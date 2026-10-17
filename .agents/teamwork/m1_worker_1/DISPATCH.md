# Dispatch: M1 Worker

**Role**: Implementation Worker (Milestone M1)
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m1_worker_1`
**Parent**: `orchestrator_9`
**Requirements Document**: `d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md` (timestamp 2026-10-16T23:46:37Z)
**Project Document**: `d:\code\rl-api-utils\.agents\teamwork\orchestrator_9\PROJECT.md`

## Input Reports to Read:
1. `d:\code\rl-api-utils\.agents\teamwork\m1_explorer_telemetry_1\handoff.md` and `telemetry_analysis.md`
2. `d:\code\rl-api-utils\.agents\teamwork\m1_explorer_storage_1\handoff.md` and `storage_analysis.md`
3. `d:\code\rl-api-utils\.agents\teamwork\m1_explorer_migration_1\handoff.md` and `migration_analysis.md`

## Owned Files:
- `internal/ballchasing/types.go`
- `internal/ballchasing/telemetry_test.go` (new)
- `internal/storage/store.go`
- `internal/storage/sqlite.go`
- `internal/storage/jsonstore.go`
- `internal/storage/m1_storage_test.go` (new)

## Mission
Implement all Milestone M1 features based on the 3 explorer designs:
1. **`internal/ballchasing`**:
   - Complete telemetry structs (`core`, `boost`, `movement`, `positioning`, `demo`, `ball`, `camera`, `car`, `rank`, `uploader`, `min_rank`/`max_rank`, `overtime_seconds`, and replay `status`).
   - Implement custom `UnmarshalJSON` for `ReplayItem` and `ReplayPlayer` to bridge nested detail and flat responses seamlessly without zero-stat bugs.
   - Update `MapPlaylistSlugToID` with `strconv.Atoi(slug)` fallback for positive integers.
   - Add unit tests in `telemetry_test.go`.
2. **`internal/storage` Models & DDL**:
   - In `store.go`: define `MatchPlayerRecord`, `MMRHistoryRecord`, add `notes` and `tags` to `PlayerRecord`, add `stats_sync_status` and `stats_retry_count` to `MatchRecord`. Add `matchTime ...time.Time` variadic to `RecordMatchResults`.
   - In `sqlite.go`:
     - Add `match_players` and `mmr_history` DDL and indexes to `schemaDDL`.
     - In `migratePlayersTable`: safely add `notes` and `tags` columns using `PRAGMA table_info`.
     - In `migrateMatchesTable`: safely add `stats_sync_status` and `stats_retry_count` columns using `PRAGMA table_info`.
     - In `RecordMatchResults`: use `matchTime` if provided (or `time.Now()`), update `ON CONFLICT` to use `first_seen_at = MIN(...)`, `last_seen_at = MAX(...)`, `last_played_at = MAX(...)`.
     - In `SyncReplayStats`: preserve replay date (`stats.Date`) instead of `time.Now()` when updating player records; hydrate player ranks into `players.ranks_json`.
     - Implement startup backfill migration `migrateMatchPlayers` that scans matches with `replay_stats_json != ''` and inserts into `match_players` if not already present.
     - Implement CRUD/queries for `match_players` and `mmr_history`.
   - In `jsonstore.go`:
     - Maintain strict dual persistence parity for `match_players`, `mmr_history`, notes/tags, and sync state.
     - Implement equivalent backfill and timestamp monotonicity.
3. **Verification**:
   - Run `go vet ./...` and ensure zero warnings.
   - Run `go test -race ./internal/ballchasing/...` and `go test -race ./internal/storage/...`.
   - Run `go test ./...` across the repository to ensure zero regressions.

DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Write your completion report to `handoff.md` and notify `orchestrator_9`.

## 2026-10-17T00:10:11Z
You are M1 Implementation Worker.
Your working directory is d:\code\rl-api-utils\.agents\teamwork\m1_worker_1.
Read:
- d:\code\rl-api-utils\.agents\teamwork\m1_worker_1\DISPATCH.md
- d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md (specifically timestamp 2026-10-16T23:46:37Z)
- d:\code\rl-api-utils\.agents\teamwork\orchestrator_9\PROJECT.md
- d:\code\rl-api-utils\.agents\teamwork\m1_explorer_telemetry_1\handoff.md and telemetry_analysis.md
- d:\code\rl-api-utils\.agents\teamwork\m1_explorer_storage_1\handoff.md and storage_analysis.md
- d:\code\rl-api-utils\.agents\teamwork\m1_explorer_migration_1\handoff.md and migration_analysis.md

Your exclusive write ownership files:
- internal/ballchasing/types.go
- internal/ballchasing/telemetry_test.go
- internal/storage/store.go
- internal/storage/sqlite.go
- internal/storage/jsonstore.go
- internal/storage/m1_storage_test.go

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Implement:
1. Complete Ballchasing telemetry types in internal/ballchasing/types.go with custom UnmarshalJSON bridging nested detail (GET /api/replays/{id}) and flat responses, and numeric slug fallback in MapPlaylistSlugToID. Add comprehensive tests in telemetry_test.go.
2. Models and DDL in internal/storage: match_players table & index, mmr_history table & index, player notes & tags, match stats_sync_status & stats_retry_count, safe PRAGMA-based migrations, variadic RecordMatchResults timestamp preservation with MIN/MAX monotonicity, replay date preservation, player rank hydration, and startup backfill migration in both SQLiteStore and JSONStore.
3. Verify with go vet ./..., go test -race ./internal/ballchasing/..., go test -race ./internal/storage/..., and go test ./... across the repo.

When done, write handoff.md in your working directory and message orchestrator_9.

## 2026-10-17T00:13:58Z
[Message from orchestrator_9]:
**Context**: Permission granted for mockStateStore in internal/auth/auth_test.go
**Content**: Write ownership is hereby granted for `internal/auth/auth_test.go` to update `mockStateStore` (e.g. embedding `storage.StateStore` or updating its `RecordMatchResults` method signature with `matchTime ...time.Time`) so that all tests compile and pass cleanly across all packages.
**Action**: Proceed with updating `internal/auth/auth_test.go` and complete the Milestone M1 implementation.

## 2026-10-17T00:49:47Z
[Message from orchestrator_9]:
**Context**: Permission granted for mockFaultStore in internal/playertrack/tracker_test.go
**Content**: Write ownership is hereby granted for `internal/playertrack/tracker_test.go` to update `mockFaultStore.RecordMatchResults` method signature with `matchTime ...time.Time` so that the mock matches the `storage.StateStore` interface and `go vet ./...` passes cleanly across the entire repository.
**Action**: Update `tracker_test.go`, complete repository verification, and write your handoff report.

