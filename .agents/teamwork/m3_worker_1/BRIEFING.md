# BRIEFING — 2026-09-26T01:16:00Z

## Mission
Implement Milestone M3 (Player Tracker Engine & Lifecycle): `internal/playertrack/tracker.go` and `internal/playertrack/tracker_test.go` with 100% test pass across all packages, race-safe concurrent access, and zero vet warnings.

## 🔒 My Identity
- Archetype: worker
- Roles: implementer, qa, specialist
- Working directory: d:\code\rl-api-utils\.agents\teamwork\m3_worker_1
- Original parent: 6e6c9567-59d2-415e-8d6e-41314a903548
- Milestone: M3 - Ballchasing Replay Uploader
- Current Milestone: M3 - Player Tracker Engine & Lifecycle
- Parent: b82f99b4-2b9f-46d1-8c45-738eb9e9a7b1

## 🔒 Key Constraints
- Exclusive write ownership: `internal/ballchasing/types.go`, `internal/ballchasing/client.go`, `internal/ballchasing/client_test.go`.
- Do NOT modify any files outside `internal/ballchasing`.
- DO NOT CHEAT: genuine logic, real state and HTTP multipart mechanics, zero hardcoded test fixtures or facade implementations.
- Raw Authorization header: strictly without `Bearer ` prefix (`Authorization: <token>`).
- 201 Created -> `UploadResult{ID, Location, IsDuplicate: false}`, nil error.
- 409 Conflict -> `UploadResult{ID, Location, IsDuplicate: true}`, nil error, 0 retries.
- 429 Too Many Requests -> parse `Retry-After` (integer seconds & HTTP-date), exponential backoff with retry budget, context-aware sleep.
- 401 Unauthorized -> immediate fatal `ErrInvalidAPIKey` (0 retries).
- 400 Bad Request -> immediate descriptive error (0 retries).
- 5xx Server Error -> transient retry within budget.
- Windows file descriptor safety: file handles closed immediately inside each attempt.
- M3 Constraints:
  - Implement `internal/playertrack/tracker.go` and `internal/playertrack/tracker_test.go`.
  - Satisfy `statsapi.PlayerEventHandler` interface (`OnUpdateState`, `OnMatchEnded`).
  - 4-Tier Local Player Resolution hierarchy:
    - Tier 1: `cfg.PlayerTracking.LocalPlayerID` (Exact PrimaryId, AccountID match, or prefix match).
    - Tier 2: Primary auth ID (`Epic|<id>|0` or `Steam|<id>|0`).
    - Tier 3: `cfg.PlayerTracking.LocalPlayerName` (case-insensitive name match).
    - Tier 4: Primary auth display name (`Epic.DisplayName` or `Steam.AccountName`).
    - Rejection: Never resolve an AI bot (`p.IsBot()`) as local player.
  - Team resolution (`myTeamNum = localPlayer.TeamNum`) and Teammate vs Opponent classification.
  - Profile upsert on `OnUpdateState` into `store.UpsertPlayer` with in-memory caching to avoid 120Hz SQLite lock contention.
  - In-memory current match snapshot (`sync.RWMutex`) with `GetCurrentMatch() *CurrentMatchResponse` returning deep clones.
  - Asynchronous rank retrieval via `SkillFetcher.GetPlayersSkills` when `AutoFetchRanks` is enabled:
    - 4-tier debounce against 120Hz flooding (15m rank cache TTL, in-flight set, 60s failure backoff, matchup cache).
    - Async DB update via `store.UpdatePlayerRanks` (fallback `UpsertPlayer` if `ErrPlayerNotFound`).
  - Match outcome compilation on `OnMatchEnded`:
    - `myTeamWon = (*winnerTeamNum == myTeamNum)`.
    - Compiles `[]storage.PlayerOutcome` with `outcome.Won = myTeamWon`.
    - Calls `store.RecordMatchResults(ctx, matchGUID, playlistID, outcomes)`.
    - Idempotent: returns `nil` on `storage.ErrMatchAlreadyProcessed`.
    - Safe handling for nil `winnerTeamNum` and unresolved local player.
  - Safe lifecycle with `Close()`.
  - Full test suite in `tracker_test.go` with dual-backend storage tests (`SQLiteStore` and `JSONStore`), full matrix of tiers, bot exclusion, 120Hz debounce, idempotency, and concurrency race stress test.

## Current Parent
- Conversation ID: b82f99b4-2b9f-46d1-8c45-738eb9e9a7b1
- Updated: 2026-09-26T01:16:00Z

## Task Summary
- **What to build**: Production-grade Player Tracker Engine & Lifecycle in `internal/playertrack/tracker.go` and comprehensive unit test suite in `internal/playertrack/tracker_test.go`.
- **Success criteria**: 100% test pass on `go test -v -count=1 ./internal/playertrack/...`, `go test -count=1 ./...` across all 12 packages, and zero warnings on `go vet ./...`.
- **Interface contracts**: `PROJECT.md` § Interface Contracts (statsapi, storage, playertrack).
- **Code layout**: `internal/playertrack/tracker.go`, `internal/playertrack/tracker_test.go`.

## Key Decisions Made
- Implemented `playertrack.Tracker` satisfying `statsapi.PlayerEventHandler` with 4-tier local player resolution.
- Guaranteed complete isolation and race freedom via `DeepClone()` on `CurrentMatchResponse` and `LobbyPlayer`.
- Implemented 4-tier debounce engine for high-frequency 120Hz ticks: profile upsert cache, 15m in-memory rank cache, in-flight RPC map, 60s failure backoff, and in-flight matchup queries.
- Verified dual-backend storage parity (`SQLiteStore` and `JSONStore`) across matchup outcomes, win/loss increments, and idempotency.

## Artifact Index
- `internal/playertrack/tracker.go` — Tracker engine implementation.
- `internal/playertrack/tracker_test.go` — Comprehensive test suite.
- `.agents/teamwork/m3_worker_1/handoff.md` — Final handoff report.

## Change Tracker
- **Files modified**:
  - `internal/playertrack/tracker.go`: Implemented Tracker engine, local player resolution, snapshot cloning, debounce caching, and outcome compilation.
  - `internal/playertrack/tracker_test.go`: Added test suite with dual backends, all tiers, bot exclusion, debounce, idempotency, and concurrency stress testing.
- **Build status**: PASS (100% pass across all 12 packages; zero `go vet` warnings).
- **Pending issues**: None.

## Quality Status
- **Build/test result**: PASS (`go test -count=1 ./...` 12/12 packages passed).
- **Lint status**: 0 warnings on `go vet ./...`.
- **Tests added/modified**: 11 test suites added in `internal/playertrack/tracker_test.go`.

## Loaded Skills
- None requested by orchestrator
