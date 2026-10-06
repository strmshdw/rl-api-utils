# Forensic Audit Report: Milestone M1 (Requirement R2: Persistent Player State on Disconnect)

**Work Product**: Milestone M1 changes in `internal/playertrack/tracker.go`, `internal/session/models.go`, `internal/session/session.go`, `web/src/types/api.ts`, and test suites `internal/playertrack/tracker_test.go`, `internal/session/session_test.go`.  
**Profile**: General Project  
**Integrity Mode**: Development (per `ORIGINAL_REQUEST.md ## 2026-10-06T08:30:09Z`)  
**Verdict**: CLEAN  

---

### Phase Results
- **Hardcoded Output Detection**: PASS — Zero hardcoded test outputs, strings, or canned return values detected.
- **Facade Detection**: PASS — Genuine state tracking algorithms implemented; differential retention, local player fallback, and goal aggregation operate dynamically on live frames.
- **Pre-populated Artifact Detection**: PASS — No pre-populated `.log`, result, or output verification artifacts exist in the repository.
- **Build and Run**: PASS — `go test -count=1 ./...` (all 14 packages), `go vet ./...`, `go build ./cmd/rl-sync`, `npm test`, and `npm run build` all pass cleanly with 100% success.
- **Differential Retention & Stats Preservation**: PASS — Omitted participants are retained in active match rosters with `IsDisconnected = true` and exact accumulated box score stats.
- **Pointer & Concurrency Safety**: PASS — `SessionMatchPlayer.DeepClone()` isolates the `Won *bool` pointer, preventing race conditions or mutation across session snapshots.

---

## 1. Observation

### 1.1 Direct Code Inspection
1. **`internal/playertrack/tracker.go:51-60`**:
   `LobbyPlayer` includes `IsDisconnected bool` with `json:"is_disconnected,omitempty"`:
   ```go
   type LobbyPlayer struct {
       PlayerID       string                 `json:"player_id"`
       Platform       string                 `json:"platform"`
       Name           string                 `json:"name"`
       TeamNum        int                    `json:"team_num"`
       IsLocal        bool                   `json:"is_local"`
       IsBot          bool                   `json:"is_bot"`
       IsDisconnected bool                   `json:"is_disconnected,omitempty"`
       Stats          PlayerStatsSummary     `json:"stats"`
       CurrentRank    *PlayerPlaylistRank    `json:"current_rank,omitempty"`
       Ranks          PlayerRanksSnapshot    `json:"ranks,omitempty"`
       MatchupRecord  *storage.PlayerMatchup `json:"matchup_record,omitempty"`
   }
   ```

2. **`internal/playertrack/tracker.go:330-575`**:
   `OnUpdateState` implements genuine differential participant retention:
   - Detects frame boundary: `isSameMatch := (t.currentMatch != nil && t.currentMatch.MatchGUID == trimmedGUID)`.
   - On frame update of existing match, captures previous rosters (`prevLocalPlayer`, `prevTeammates`, `prevOpponents`, `prevSpectators`).
   - If local player is omitted from current frame, falls back to `prevLocalPlayer` with `IsDisconnected = true` and preserves `myTeamNum = *t.currentMatch.LocalTeam`.
   - Populates `seenThisFrame` map (keyed by normalized player ID) for active human participants, marking them `IsDisconnected = false`.
   - For all previous human participants (`!oldP.IsBot`) absent from `seenThisFrame`, clones them via `DeepClone()`, sets `IsDisconnected = true`, and reattaches them to `teammates`, `opponents`, or `spectators`.
   - Departed bots (`oldP.IsBot == true`) are deliberately skipped to prevent bot ghost accumulation.
   - When a new match GUID arrives (`!isSameMatch`), retention is skipped, resetting state cleanly.

3. **`internal/session/models.go:70-96` & `session.go:279-310`**:
   - `SessionMatchPlayer` has `IsDisconnected bool` and `Won *bool`.
   - `SessionMatchPlayer.DeepClone()` allocates an isolated boolean copy for `clone.Won = &won`.
   - In `ConcludeMatch`, team goal totals (`blueScore`, `orangeScore`) iterate through all players including disconnected ones, accurately accumulating goals.
   - `won` status is computed dynamically from `lp.TeamNum == *match.WinnerTeam`.

4. **`web/src/types/api.ts:44-45, 91`**:
   - `SessionMatchPlayer` interfaces include `is_disconnected?: boolean;` and `won?: boolean;`.
   - `LobbyPlayer` interface includes `is_disconnected?: boolean;`.

### 1.2 Independent Verification Tool Commands & Outputs
1. **Uncached Playertrack Tests**:
   - Command: `go test -v -count=1 ./internal/playertrack/...`
   - Output:
     ```
     === RUN   TestTracker_MidGameDisconnect_Lifecycle_TeammateAndLocal
     === RUN   TestTracker_MidGameDisconnect_Lifecycle_TeammateAndLocal/SQLite
     === RUN   TestTracker_MidGameDisconnect_Lifecycle_TeammateAndLocal/JSONStore
     --- PASS: TestTracker_MidGameDisconnect_Lifecycle_TeammateAndLocal (0.03s)
     === RUN   TestTracker_MidGameDisconnect_OpponentDisconnectAndReconnect
     --- PASS: TestTracker_MidGameDisconnect_OpponentDisconnectAndReconnect (0.03s)
     === RUN   TestTracker_MidGameDisconnect_MultipleSimultaneous
     --- PASS: TestTracker_MidGameDisconnect_MultipleSimultaneous (0.03s)
     === RUN   TestTracker_MidGameDisconnect_MatchTransition_ResetsOldDisconnected
     --- PASS: TestTracker_MidGameDisconnect_MatchTransition_ResetsOldDisconnected (0.03s)
     === RUN   TestTracker_MidGameDisconnect_BotReplacement
     --- PASS: TestTracker_MidGameDisconnect_BotReplacement (0.03s)
     PASS
     ok  github.com/dank/rl-api-utils/internal/playertrack 4.184s
     ```

2. **Uncached Session Tracker Tests**:
   - Command: `go test -v -count=1 ./internal/session/...`
   - Output:
     ```
     === RUN   TestSessionTracker_MidGameDisconnect_ActiveMatchObserverPropagation
     --- PASS: TestSessionTracker_MidGameDisconnect_ActiveMatchObserverPropagation (0.02s)
     === RUN   TestSessionTracker_MidGameDisconnect_SSEBroadcast
     --- PASS: TestSessionTracker_MidGameDisconnect_SSEBroadcast (0.02s)
     === RUN   TestSessionTracker_MidGameDisconnect_ConcludeMatchSnapshotPreservation
     --- PASS: TestSessionTracker_MidGameDisconnect_ConcludeMatchSnapshotPreservation (0.00s)
     === RUN   TestSessionTracker_DeepClone_PreservesDisconnect
     --- PASS: TestSessionTracker_DeepClone_PreservesDisconnect (0.00s)
     PASS
     ok  github.com/dank/rl-api-utils/internal/session 5.203s
     ```

3. **Repository-Wide Go Tests (All 14 Packages)**:
   - Command: `go test -count=1 ./...`
   - Result: Exit code 0 across all 14 packages:
     ```
     ok  github.com/dank/rl-api-utils/cmd/rl-sync        0.446s
     ok  github.com/dank/rl-api-utils/internal/auth       2.483s
     ok  github.com/dank/rl-api-utils/internal/ballchasing 8.971s
     ok  github.com/dank/rl-api-utils/internal/config     0.982s
     ok  github.com/dank/rl-api-utils/internal/daemon     16.626s
     ok  github.com/dank/rl-api-utils/internal/playertrack 11.107s
     ok  github.com/dank/rl-api-utils/internal/psynet     5.140s
     ok  github.com/dank/rl-api-utils/internal/session    5.827s
     ok  github.com/dank/rl-api-utils/internal/statsapi   1.178s
     ok  github.com/dank/rl-api-utils/internal/storage    37.757s
     ok  github.com/dank/rl-api-utils/internal/syncer     1.677s
     ok  github.com/dank/rl-api-utils/internal/testutil   1.418s
     ok  github.com/dank/rl-api-utils/internal/web        0.820s
     ok  github.com/dank/rl-api-utils/test/e2e            29.810s
     ```

4. **Static Analysis & Compilation**:
   - Command: `go vet ./...` -> Clean (0 warnings, 0 errors).
   - Command: `go build ./cmd/rl-sync` -> Clean exit code 0.

5. **Frontend Tests & Build**:
   - Command: `npm --prefix web test` -> 9 test files passed, 112 tests passed (0 failures).
   - Command: `npm --prefix web run build` -> `tsc -b && vite build` completed cleanly, bundling into `internal/web/dist`.

---

## 2. Logic Chain

1. **Integrity Mode & Ground Truth**:
   - `ORIGINAL_REQUEST.md` specifies `Integrity mode: development`.
   - Under this mode, hardcoded test results, facade implementations, and fabricated verification outputs are strictly prohibited.
2. **Implementation Verification**:
   - Inspection of `internal/playertrack/tracker.go` confirms that player departure retention is implemented with dynamic state comparison across successive `OnUpdateState` invocations using `seenThisFrame` and deep cloning of previous participant vectors.
   - No hardcoded player IDs, test GUIDs, or constant return mocks exist in production logic.
   - The retention mechanism properly discriminates AI bots from human players, avoids leaking across match boundaries, and correctly deduplicates returning players.
3. **Behavioral Corroboration**:
   - Running the test suites independently with `-count=1` confirmed that all assertions execute genuinely against live SQLite and JSONStore backends.
   - All tests pass with zero flakiness or side effects.

---

## 3. Caveats

- **No caveats.** The scope of Milestone M1 is strictly Requirement R2 (Persistent Player State on Disconnect). Milestone M2 will handle persistent outcome logging into storage (`player_matchups`). All M1 deliverables and interface contracts are cleanly satisfied.

---

## 4. Conclusion

The work product for Milestone M1 is verified as **CLEAN**. There are zero integrity violations, no facade methods, no fabricated test outputs, and no regressions across any of the project's subsystems.

---

## 5. Verification Method

To independently re-verify:
```powershell
go test -v -count=1 -run TestTracker_MidGameDisconnect ./internal/playertrack/...
go test -v -count=1 -run "TestSessionTracker_MidGameDisconnect|TestSessionTracker_DeepClone_PreservesDisconnect" ./internal/session/...
go test -count=1 ./...
go vet ./...
go build ./cmd/rl-sync
npm --prefix web test
npm --prefix web run build
```
Invalidation condition: Any failure in retention tests or missing `is_disconnected` fields in API responses.
