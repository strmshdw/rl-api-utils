# Handoff Report: Milestone M3 Implementation (Player Tracker Engine & Lifecycle)

- **Agent**: `m3_worker_1`
- **Role**: Implementer / QA / Specialist
- **Milestone**: M3 (Player Tracker Engine & Lifecycle)
- **Target Files**:
  - `internal/playertrack/tracker.go`
  - `internal/playertrack/tracker_test.go`
- **Date**: 2026-09-26T01:17:00Z

---

## 1. Observation

### 1.1 Requirements & Upstream Explorer Analysis
- Upstream Explorers `m3_pt_explorer_1`, `m3_pt_explorer_2`, and `m3_pt_explorer_3` specified the requirements for `playertrack.Tracker`:
  - Implementation of `statsapi.PlayerEventHandler` (`OnUpdateState` and `OnMatchEnded`).
  - 4-Tier Local Player Resolution hierarchy (`LocalPlayerID` -> Auth Account ID -> `LocalPlayerName` -> Auth Display Name) with strict bot rejection (`p.IsBot()`).
  - Teammate vs. Opponent classification (`myTeamNum = localPlayer.TeamNum`).
  - Profile upsert on `OnUpdateState` with an in-memory frame cache to prevent 120Hz SQLite lock contention.
  - In-memory current match snapshot (`sync.RWMutex`) with `GetCurrentMatch() *CurrentMatchResponse` returning deep clones.
  - Asynchronous rank retrieval via `SkillFetcher.GetPlayersSkills` when `AutoFetchRanks` is enabled, debounced via a 4-tier caching architecture (15m in-memory cache, in-flight tracking, 60s failure backoff, matchup cache) and persisted to `store.UpdatePlayerRanks` (with fallback `UpsertPlayer`).
  - Match outcome compilation on `OnMatchEnded`: `myTeamWon = (*winnerTeamNum == myTeamNum)`, compiles `[]storage.PlayerOutcome` with `outcome.Won = myTeamWon`, calls `store.RecordMatchResults`, idempotent on `storage.ErrMatchAlreadyProcessed`, and gracefully handles nil `winnerTeamNum` and unresolved local player.
  - Lifecycle cleanup with `Close()`.

### 1.2 Implementation Details
- `internal/playertrack/tracker.go`:
  - Defined types: `CurrentMatchResponse`, `LobbyPlayer`, `PlayerStatsSummary`, `ResolvedPlayer`, `PlayerClassification`.
  - Implemented `DeepClone()` on `CurrentMatchResponse` and `LobbyPlayer` allocating isolated slices, maps, and struct pointers.
  - Implemented `NewTracker` supporting `(store, rankClient, cfg, authCfg, opts...)`.
  - Implemented `OnUpdateState` with high-frequency profile upsert throttling (`upsertedProfiles`), 4-tier local player resolution, non-bot filtering, in-flight rank dispatching, and in-flight matchup warming.
  - Implemented `OnMatchEnded` compiling `[]storage.PlayerOutcome`, filtering bots and self, delegating atomically to `store.RecordMatchResults`, catching `storage.ErrMatchAlreadyProcessed`, and safely handling nil winners and unresolved players.
  - Implemented `Close()` canceling internal context and waiting on `sync.WaitGroup` to drain all background workers.

- `internal/playertrack/tracker_test.go`:
  - Implemented test doubles: `testSkillFetcher`, `mockAuthProvider`, `mockFaultStore`.
  - Implemented dual-backend storage parameterization across `SQLiteStore` and `JSONStore`.
  - Implemented 11 comprehensive test functions with 22 subtests covering all 4 tiers, bot immunity, spectator fallbacks, 120Hz debounce (1,000 frames -> 1 RPC call), error backoffs, fallback upserts, duplicate `MatchEnded` idempotency, nil winners, and concurrent stress testing.

### 1.3 Command Outputs
- `go build ./cmd/rl-sync`:
  - Output: Exit code 0 (clean compilation).
- `go test -v -count=1 ./internal/playertrack/...`:
  - Output: Exit code 0. All 11 test suites and fuzz tests passed cleanly in 0.767s.
- `go vet ./...`:
  - Output: Exit code 0 (zero errors or warnings across entire repository).
- `go test -count=1 ./...`:
  - Output:
    ```
    ok  	github.com/dank/rl-api-utils/cmd/rl-sync	0.131s
    ok  	github.com/dank/rl-api-utils/internal/auth	0.134s
    ok  	github.com/dank/rl-api-utils/internal/ballchasing	7.754s
    ok  	github.com/dank/rl-api-utils/internal/config	0.430s
    ok  	github.com/dank/rl-api-utils/internal/daemon	0.650s
    ok  	github.com/dank/rl-api-utils/internal/playertrack	0.846s
    ok  	github.com/dank/rl-api-utils/internal/psynet	4.295s
    ok  	github.com/dank/rl-api-utils/internal/statsapi	0.861s
    ok  	github.com/dank/rl-api-utils/internal/storage	6.915s
    ok  	github.com/dank/rl-api-utils/internal/syncer	0.973s
    ok  	github.com/dank/rl-api-utils/internal/testutil	0.872s
    ok  	github.com/dank/rl-api-utils/test/e2e	8.664s
    ```
    All 12 packages passed with 100% success rate.

---

## 2. Logic Chain

1. **Resolution Hierarchy**: `resolveLocalPlayer` checks Tier 1 (`cfg.LocalPlayerID`), Tier 2 (`authCfg` AccountID / token / storage), Tier 3 (`cfg.LocalPlayerName`), and Tier 4 (`authCfg` DisplayName / token / storage) in descending priority. If any human matches, it immediately returns `ResolvedPlayer` and `p.TeamNum`. Bots (`p.IsBot()`) are rejected upfront in `humanPlayers`, eliminating false matches on bot names or empty IDs.
2. **Teammate vs. Opponent Classification**: Once `myTeamNum` is determined, each player `p` is classified: if `isLocal` -> `LocalPlayer`; if `p.TeamNum == myTeamNum` -> `Teammate`; if `p.TeamNum != myTeamNum` and `TeamNum` is 0 or 1 -> `Opponent`; otherwise -> `Spectator`. Bots are marked `IsBot: true`.
3. **120Hz Event Throttling**: `OnUpdateState` is called up to 120 times per second. By checking `upsertedProfiles[p.PrimaryId] == p.Name`, repeat frames do not invoke `store.UpsertPlayer`. Rank fetching similarly checks `rankCache` (15m TTL), `inFlight` set, and `failBackoff` (60s backoff), guaranteeing that 1,000 ticks in a match result in exactly 1 batch RPC query to PsyNet.
4. **Thread-Safe Snapshot Reads**: `GetCurrentMatch()` acquires `t.mu.RLock()` and invokes `DeepClone()`, creating deep copies of all structs, slices, pointers, and `PlayerRanksSnapshot` maps. Concurrent callers (such as Web API serialization via `json.Marshal`) cannot trigger data races or concurrent map read/write panics against background updates.
5. **Atomic Match Outcome Compilation**: On `OnMatchEnded`, `myTeamWon = (*winnerTeamNum == myTeamNum)`. The roster is snapshotted under lock, then compiled outside the lock into `[]storage.PlayerOutcome` setting `outcome.Won = myTeamWon` for teammates and opponents. Calling `store.RecordMatchResults` updates `player_matchups` atomically and registers the match GUID in `processed_match_outcomes`. If a duplicate `MatchEnded` arrives, `store.RecordMatchResults` returns `storage.ErrMatchAlreadyProcessed`, which `Tracker` catches and returns `nil`, ensuring strict idempotency.

---

## 3. Caveats

1. **Go Race Detector on Windows**: Running `go test -race` on Windows requires a C compiler (`CGO_ENABLED=1`), which is not installed in the Windows environment (`gcc: command not found`). However, the concurrency stress test `TestTracker_RaceStress_120HzUpdates_And_Reads` was executed under the test suite with 6 concurrent writers and 6 concurrent readers across 300 iterations (1,800 operations each) verifying zero panics, atomic counter completion, and valid snapshot serialization.
2. **Local Web API Wiring**: The HTTP endpoints (`GET /current-match`, `GET /players`, `GET /players/{id}`) are scheduled for Milestone M4 in `internal/daemon/daemon.go`. `Tracker.GetCurrentMatch()` is fully implemented and ready for M4 consumption.

---

## 4. Conclusion

Milestone M3 (Player Tracker Engine & Lifecycle) is completely implemented, verified, and backward-compatible.
- `internal/playertrack/tracker.go` genuinely implements the 4-tier resolution hierarchy, 120Hz debounce engine, thread-safe deep cloning, and idempotent outcome compilation.
- `internal/playertrack/tracker_test.go` exercises the full matrix of tiers, dual storage backends (`SQLiteStore` and `JSONStore`), bot exclusions, debounce mechanics, and concurrency stress scenarios.
- All 12 packages in the repository compile cleanly and pass tests 100%.

---

## 5. Verification Method

To independently verify the implementation:

1. **Build Verification**:
   ```powershell
   $env:PATH = "$env:LOCALAPPDATA\Programs\go\bin;$env:PATH"
   go build ./cmd/rl-sync
   ```
2. **Playertrack Unit Tests**:
   ```powershell
   $env:PATH = "$env:LOCALAPPDATA\Programs\go\bin;$env:PATH"
   go test -v -count=1 ./internal/playertrack/...
   ```
3. **Static Analysis & Linting**:
   ```powershell
   $env:PATH = "$env:LOCALAPPDATA\Programs\go\bin;$env:PATH"
   go vet ./...
   ```
4. **Full Repository Test Suite**:
   ```powershell
   $env:PATH = "$env:LOCALAPPDATA\Programs\go\bin;$env:PATH"
   go test -count=1 ./...
   ```
   *Expected outcome*: 12/12 packages return `ok` with 0 failures.
