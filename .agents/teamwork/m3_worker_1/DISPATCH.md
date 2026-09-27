# Dispatch: m3_worker_1

## 2026-09-26T01:15:00Z
- **Role**: Milestone M3 Implementation Worker
- **Milestone**: M3 (Player Tracker Engine & Lifecycle)
- **Target Files**:
  - `internal/playertrack/tracker.go` (Implementation)
  - `internal/playertrack/tracker_test.go` (Unit & Race Tests)
- **Inputs**:
  - `d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md` (specifically `## 2026-09-26T00:19:44Z`)
  - `c:\Users\strms\.gemini\antigravity\brain\11baea32-4a41-4d49-b959-d518322eea18\player_tracking_plan.md`
  - `d:\code\rl-api-utils\.agents\teamwork\orchestrator_3\PROJECT.md`
  - `d:\code\rl-api-utils\.agents\teamwork\m3_pt_explorer_1\analysis.md`
  - `d:\code\rl-api-utils\.agents\teamwork\m3_pt_explorer_2\analysis.md`
  - `d:\code\rl-api-utils\.agents\teamwork\m3_pt_explorer_3\analysis.md`

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Core Requirements to Implement:
1. `playertrack.Tracker`:
   - Implements `statsapi.PlayerEventHandler` (`OnUpdateState`, `OnMatchEnded`).
   - 4-Tier Local Player Resolution hierarchy:
     - Tier 1: Config `LocalPlayerID` (`PrimaryId` or `AccountID` match).
     - Tier 2: Primary auth ID (`Epic|<id>|0` or `Steam|<id>|0`).
     - Tier 3: Config `LocalPlayerName` (case-insensitive name match).
     - Tier 4: Primary auth display name (`Epic.DisplayName` or `Steam.AccountName`).
     - Rejects any player where `player.IsBot() == true`.
   - Team resolution (`myTeamNum = localPlayer.TeamNum`) and Teammate vs. Opponent classification.
   - Non-bot human player upsert to `store.UpsertPlayer`, with in-memory caching to avoid 120Hz SQLite thrashing.
   - In-memory current match snapshot (`sync.RWMutex`) with `GetCurrentMatch() *CurrentMatchResponse` returning deep clones.
   - Asynchronous rank retrieval via `SkillFetcher.GetPlayersSkills` when `AutoFetchRanks` is enabled:
     - Debounce with in-memory TTL cache and in-flight tracking (no redundant PsyNet RPC calls during 120Hz events).
     - Asynchronous DB update via `store.UpdatePlayerRanks` (with fallback `UpsertPlayer` if `ErrPlayerNotFound`).
   - Match outcome compilation on `OnMatchEnded`:
     - `myTeamWon = (*winnerTeamNum == myTeamNum)`.
     - Compiles `[]storage.PlayerOutcome` setting `outcome.Won = myTeamWon`.
     - Calls `store.RecordMatchResults(ctx, matchGUID, playlistID, outcomes)`.
     - Idempotent: returns `nil` on `storage.ErrMatchAlreadyProcessed`.
     - Safe fallback on nil `winnerTeamNum` or unresolved local player.
   - Clean shutdown with `Tracker.Close()`.
2. `playertrack.Tracker` Unit Tests (`tracker_test.go`):
   - Dual-backend parameterization testing both `SQLiteStore` and `JSONStore`.
   - Test double setup (`TestSkillFetcher` with synchronization channels).
   - Test all 4 resolution tiers and fallbacks.
   - Test bot exclusion (`Unknown|0|0`).
   - Test 120Hz debounce (1,000 `OnUpdateState` calls -> 1 rank RPC call).
   - Test `OnMatchEnded` outcome calculation (teammate win/loss, opponent win/loss).
   - Test duplicate `OnMatchEnded` idempotency (`ErrMatchAlreadyProcessed` -> zero counter drift).
   - Heavy concurrency stress test with Go race detector (`go test -race ./internal/playertrack/...`).

Verification Commands:
- `go build ./cmd/rl-sync`
- `go test -v -count=1 ./internal/playertrack/...`
- `go test -v -race -count=1 ./internal/playertrack/...`
- `go test -count=1 ./...` (all 12 packages must pass 100%)
- `go vet ./...`

Write complete handoff report to `d:\code\rl-api-utils\.agents\teamwork\m3_worker_1\handoff.md`.
Send message to parent when done.
