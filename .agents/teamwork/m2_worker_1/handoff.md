# Milestone 2 Handoff Report: Polling Auth & PsyNet Rank Client Implementation

**Agent**: `m2_worker_1`  
**Date**: 2026-09-26T01:00:00Z  
**Target Milestone**: M2 (Polling Auth & PsyNet Rank Client)  
**Parent Agent**: `parent` (`b82f99b4-2b9f-46d1-8c45-738eb9e9a7b1`)

---

## 1. Observation

1. **Baseline Invariants & Integrity**:
   - Initial repository test run via `go test -count=1 ./...` passed across all packages (`cmd/rl-sync`, `internal/auth`, `internal/ballchasing`, `internal/config`, `internal/daemon`, `internal/psynet`, `internal/statsapi`, `internal/storage`, `internal/syncer`, `internal/testutil`, `test/e2e`).
   - Prior to M2, `internal/playertrack` did not exist.
2. **StateStore Persistence & Collision Hazards**:
   - Direct inspection of `internal/auth/epic.go` and `internal/auth/steam.go` revealed that `SaveAuthState(ctx, "epic", ...)` and `SaveAuthState(ctx, "steam", ...)` use static provider keys (`"epic"` / `"steam"`).
   - If an AuthProvider configured with `polling_auth` were given the shared `storage.StateStore`, any token refresh or session renewal on the secondary polling account would overwrite the primary player's stored tokens in SQLite and JSONStore (`ON CONFLICT(provider) DO UPDATE SET...`).
   - PsyNet enforces strict session exclusivity per account ID. Authenticating to PsyNet using the active game account results in immediate game termination with `Error 67 ("Connection to server timed out")`, inflicting matchmaking bans and MMR penalties.
3. **PsyNet Skills RPC Capabilities**:
   - In `github.com/dank/rlapi v0.1.26` (`C:\Users\strms\go\pkg\mod\github.com\dank\rlapi@v0.1.26\skills.go` lines 113–124):
     `func (p *PsyNetRPC) GetPlayersSkills(ctx context.Context, playerIDs []PlayerID) ([]PlayerWithSkills, error)`
     sends synchronous `Skills/GetPlayersSkills v1` requests over WebSocket and returns `[]PlayerWithSkills`.
   - In `psynetrpc.go`: `*rlapi.PsyNetRPC` provides `IsConnected() bool` and `Close() error`.
4. **Canonical Ranks & Formatting**:
   - PsyNet uses 23 0-indexed tiers (0 to 22) and 4 0-indexed divisions (0 to 3).
   - Tier 0 is `Unranked` and Tier 22 is `Supersonic Legend`. Neither displays a division under official Rocket League taxonomy.
   - Tiers 1–21 display `<TierName> Division <I|II|III|IV>`.
   - In standard 0-indexed PsyNet tiers, Tier 16 is Champion I and Tier 17 is Champion II.
5. **Modified and Created Source Code**:
   - `internal/config/config.go`: Added `PollingAuthConfig` (with `ToAuthConfig()`), `PlayerTrackingConfig`, root `Config` fields, `CLIFlags` fields, defaults in `NewDefaultConfig()`, environment variable overrides (`RL_SYNC_POLLING_*`, `RL_SYNC_PLAYER_TRACKING_*`, `RL_SYNC_LOCAL_*`), aliases, and anti-collision validation in `Validate()`.
   - `internal/config/config_test.go`: Added 9 unit test suites covering defaults, YAML/JSON loading, env overrides, aliases, validation, anti-collision Error 67 guard, and CLI flags.
   - `internal/auth/provider.go`: Implemented `NewPollingProvider(cfg config.PollingAuthConfig, opts ...Option)` enforcing `store = nil` for strictly in-memory token lifecycle.
   - `internal/auth/auth_test.go`: Added `TestNewPollingProvider` testing disabled and enabled epic/steam polling providers.
   - `internal/playertrack/rank_client.go`: Implemented `SkillFetcher`, `SkillRPCClient`, `SkillRPCFactory`, `PsyNetRankClient` (with transparent reconnect and single retry), `NoOpRankClient`, `CheckCredentialCollision`, `FormatRank` (canonical 23 tiers & 4 divisions, Unranked & SSL division suppression), `FormatPlaylist`, `SerializeRanksJSON`, `ParseRanksJSON`, and `MockSkillFetcher`.
   - `internal/playertrack/rank_client_test.go`: Implemented 20 unit test suites covering all tiers, divisions, Unranked/SSL suppression, out-of-bounds, playlist mapping, serialization roundtrip, mock fetcher, offline degradation, reconnect, concurrency, and auth supplier.
   - `configs/config.example.yaml`: Added `polling_auth` and `player_tracking` configuration templates.

---

## 2. Logic Chain

1. **StateStore Isolation**:
   - Because `auth.NewProvider` accepts `storage.StateStore` and invokes `p.store.SaveAuthState(ctx, "epic", ...)` on token refresh (Observation 2), passing `nil` to `NewProvider` inside `NewPollingProvider` guarantees that secondary polling credentials remain strictly in-memory.
   - This ensures that token rotation on the secondary account will never overwrite or corrupt the primary playing account's saved tokens in SQLite or JSONStore.
2. **Error 67 Prevention**:
   - To guard against PsyNet Error 67 session exclusivity kicks (Observation 2), `Validate()` in `internal/config/config.go` and `CheckCredentialCollision` in `internal/playertrack/rank_client.go` compare primary vs. polling credentials.
   - If both accounts use the same provider and matching credentials (`account_id`, `refresh_token`, `auth_code`, `session_ticket`, or `steam_id_64`), `Validate()` returns a configuration error, and `NewRankClient` gracefully falls back to `NoOpRankClient`, ensuring the daemon never connects with colliding credentials.
3. **Decoupled Graceful Offline Degradation**:
   - If `polling_auth` is disabled (`enabled: false`), unconfigured, or fails to authenticate, `NewRankClient` returns `NoOpRankClient` without returning a fatal error.
   - Downstream subsystems (e.g. `Tracker` in M3 and Web API in M4) invoke `GetPlayersSkills(ctx, playerIDs)`, which returns `(nil, nil)` immediately with zero latency and zero network overhead. Player matchup tracking continues operating normally based on local Stats API telemetry.
4. **Canonical Ranking & Deserialization Parity**:
   - `FormatRank` handles all 23 tiers (0–22) and 4 divisions (0–3) per official PsyNet taxonomy (Observation 4), suppressing division text on Unranked (0) and Supersonic Legend (22), and handling invalid divisions or out-of-bounds tiers gracefully.
   - `SerializeRanksJSON` outputs standard JSON maps keyed by string playlist ID, and outputs `"{}"` when empty/nil.
   - `ParseRanksJSON` safely parses `"{}"` or empty strings into an empty non-nil `PlayerRanksSnapshot` and backfills legacy or partial payloads.

---

## 3. Caveats

- Live WebSocket connections to production PsyNet (`api.rlpp.psynet.gg`) require real Epic or Steam accounts; automated test execution exercises the complete RPC query, auto-reconnect, and error-handling code paths using `SkillRPCFactory` and test doubles without requiring live credentials.
- No other caveats.

---

## 4. Conclusion

Milestone 2 objectives are 100% completed and fully verified:
- `internal/config` provides `PollingAuthConfig`, `PlayerTrackingConfig`, env variable overrides, aliases, and static anti-collision protection.
- `internal/auth` provides `NewPollingProvider` with guaranteed in-memory state isolation.
- `internal/playertrack` provides `SkillFetcher`, `PsyNetRankClient`, `NoOpRankClient`, canonical 23-tier & 4-division rank formatting (`FormatRank`), playlist mapping (`FormatPlaylist`), and `ranks_json` serialization (`SerializeRanksJSON`, `ParseRanksJSON`).
- All 12 packages in the repository compile cleanly and pass 100% of all unit and integration tests with zero `go vet` warnings.

---

## 5. Verification Method

To independently verify the implementation, execute the following commands in `d:\code\rl-api-utils`:

1. **Compile Application**:
   ```powershell
   go build ./cmd/rl-sync
   ```
   *Expected*: Exits with code 0 and zero errors.

2. **Run Config Package Tests**:
   ```powershell
   go test -v -count=1 ./internal/config/...
   ```
   *Expected*: All tests pass (including `TestConfig_Defaults_PlayerTrackingAndPollingAuth`, `TestConfig_PollingAuth_YAMLAndJSON`, `TestConfig_PollingAuth_EnvOverrides`, `TestConfig_DuplicateCredentials_Error67Prevention`, `TestConfig_CLIOverrides_PollingAndPlayerTracking`).

3. **Run Auth Package Tests**:
   ```powershell
   go test -v -count=1 ./internal/auth/...
   ```
   *Expected*: All tests pass (including `TestNewPollingProvider`).

4. **Run PlayerTrack Package Tests**:
   ```powershell
   go test -v -count=1 ./internal/playertrack/...
   ```
   *Expected*: All 20 tests pass (including `TestFormatRank_All23Tiers`, `TestFormatRank_UnrankedEdgeCases`, `TestFormatRank_SSLEdgeCases`, `TestFormatPlaylist_CanonicalPlaylists`, `TestSerializeRanksJSON_ValidSkills`, `TestParseRanksJSON_RoundTrip`, `TestParseRanksJSON_LegacyOrPartialJSON`, `TestPsyNetRankClient_TransparentReconnectOnDrop`, `TestPsyNetRankClient_Concurrency`, `TestNewRankClient_Factory`).

5. **Run Full Test Suite Across All Packages**:
   ```powershell
   go test -count=1 ./...
   ```
   *Expected*: 100% pass across all 12 packages.

6. **Run Go Vet**:
   ```powershell
   go vet ./...
   ```
   *Expected*: Exits with code 0 and zero warnings.
