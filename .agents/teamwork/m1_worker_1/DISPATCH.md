# Task Assignment: M1 Worker 1 — Stats API & Storage Schema Implementation

**Agent Identity**: `m1_worker_1`
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m1_worker_1`
**Authoritative Request**: `d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md` (section ## 2026-09-26T00:19:44Z)
**Scope Document**: `d:\code\rl-api-utils\.agents\teamwork\orchestrator_3\SCOPE.md`
**Project Reference**: `d:\code\rl-api-utils\.agents\teamwork\orchestrator_3\PROJECT.md`
**Explorer Inputs**:
- `d:\code\rl-api-utils\.agents\teamwork\m1_pt_explorer_1\analysis.md`
- `d:\code\rl-api-utils\.agents\teamwork\m1_pt_explorer_2\analysis.md`
- `d:\code\rl-api-utils\.agents\teamwork\m1_pt_explorer_3\analysis.md`

## Files Owned Exclusively
- `internal/statsapi/types.go`
- `internal/statsapi/listener.go`
- `internal/statsapi/listener_test.go`
- `internal/storage/store.go`
- `internal/storage/sqlite.go`
- `internal/storage/sqlite_test.go`
- `internal/storage/jsonstore.go`
- `internal/storage/jsonstore_test.go`

## Mandatory Integrity Warning
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

## Objectives
1. Read all three Explorer analysis reports thoroughly.
2. In `internal/statsapi`:
   - Implement `EventData.UnmarshalJSON` with polymorphic support for both raw string and object envelopes.
   - Add `StatsPlayer`, `StatsGame`, `WinnerTeamNum *int`, `Playlist int`, `ParsePrimaryID`, and `StatsPlayer.IsBot()`.
   - Add `PlayerEventHandler` interface, `WithPlayerEventHandler`, and `SetPlayerEventHandler` to `Listener` while keeping `NewListener` backward compatible.
   - Dispatch `UpdateState` and `MatchEnded` in `handleRawMessage` while preserving `tracker.RecordMatch`.
   - Write comprehensive tests in `internal/statsapi/listener_test.go`.
3. In `internal/storage`:
   - In `store.go`: add `PlayerRecord`, `PlayerMatchup`, `PlayerSummary`, `PlayerOutcome`, sentinel errors, and 8 new methods to `StateStore`.
   - In `sqlite.go`: update `schemaDDL` with `players`, `player_matchups`, and `processed_match_outcomes` (atomic deduplication). Implement all 8 methods with transaction safety, foreign key compliance, and deterministic sorting.
   - In `jsonstore.go`: update `jsonStatePayload` and `JSONStore`, implement deep cloning, RWMutex locking, sorting/pagination, and atomic disk persistence. Full parity with SQLite.
   - In `sqlite_test.go` and `jsonstore_test.go`: add comprehensive test suites for all 8 methods, transaction rollbacks, idempotency, foreign key cascading, and concurrency under `-race`.
4. Run verification commands:
   - `go build ./cmd/rl-sync`
   - `go test -v -count=1 ./internal/statsapi/...`
   - `go test -v -count=1 ./internal/storage/...`
   - `go test -v -race -count=1 ./internal/storage/...`
   - `go test ./...` (ensure 100% pass across all repository packages)
5. Document all changes, verification commands, and pass results in `d:\code\rl-api-utils\.agents\teamwork\m1_worker_1\handoff.md`.
