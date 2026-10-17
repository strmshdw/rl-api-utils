# BRIEFING — 2026-10-17T00:15:00Z

## Mission
Implement Milestone M1: Complete Ballchasing Telemetry Ingestion, Schema Migrations, and Dual Persistence Storage Layer (SPEC-01, SPEC-02, SPEC-04, SPEC-07, SPEC-08).

## 🔒 My Identity
- Archetype: worker
- Roles: implementer, qa, specialist
- Working directory: d:\code\rl-api-utils\.agents\teamwork\m1_worker_1
- Original parent: 6e6c9567-59d2-415e-8d6e-41314a903548
- Milestone: M1 - Storage & Configuration
- Subagent Conversation ID: 2382f655-9d12-497d-8842-5d57d3fa6050
- Current Parent Conversation ID: b82f99b4-2b9f-46d1-8c45-738eb9e9a7b1
- Expansion Milestone: M1 - Stats API & Storage Schema Expansion
- M1 R2 Milestone: Milestone M1 (Requirement R2: Persistent Player State on Mid-Game Disconnect)
- M1 R2 Parent Conversation ID: f26416a7-29be-4b99-8406-d28bf983644d
- M1 R1 Milestone: Milestone 1: True In-Game MMR Conversion (R1)
- M1 R1 Parent Conversation ID: 18e4e66b-5835-4b6b-bd1e-a34092f47346
- M1 Full Spec Milestone: Milestone M1 (Telemetry Models, Schema Migrations & Storage)
- M1 Full Spec Parent: orchestrator_9 (048a592a-b159-4ea3-a5c3-e619b708dd31)

## 🔒 Key Constraints
- Exclusive write ownership:
  - go.mod, go.sum
  - internal/storage/store.go
  - internal/storage/sqlite.go
  - internal/storage/sqlite_test.go
  - internal/storage/jsonstore.go
  - internal/storage/jsonstore_test.go
  - internal/config/config.go
  - internal/config/config_test.go
  - configs/config.example.yaml
  - configs/config.example.json
  - internal/statsapi/types.go
  - internal/statsapi/listener.go
  - internal/statsapi/listener_test.go
- Expansion constraints:
  - Exclusive ownership for this task:
    - internal/statsapi/types.go
    - internal/statsapi/listener.go
    - internal/statsapi/listener_test.go
    - internal/storage/store.go
    - internal/storage/sqlite.go
    - internal/storage/sqlite_test.go
    - internal/storage/jsonstore.go
    - internal/storage/jsonstore_test.go
- M1 R2 Exclusive write ownership:
  - internal/playertrack/tracker.go
  - internal/playertrack/tracker_test.go
  - internal/session/models.go
  - internal/session/session.go
  - internal/session/session_test.go
  - web/src/types/api.ts
- M1 R1 Exclusive write ownership:
  - internal/playertrack/rank_client.go
  - internal/playertrack/rank_client_test.go
  - internal/session/session_mmr_test.go
  - internal/daemon/daemon_mmr_test.go
- M1 Full Spec Exclusive write ownership:
  - internal/ballchasing/types.go
  - internal/ballchasing/telemetry_test.go
  - internal/storage/store.go
  - internal/storage/sqlite.go
  - internal/storage/jsonstore.go
  - internal/storage/m1_storage_test.go
  - internal/auth/auth_test.go (granted by orchestrator_9 for mockStateStore interface synchronization)
- DO NOT touch other source files outside this scope. DO NOT edit any files in web/ or files owned by other milestones.
- Integrity mandate: genuine implementations only, zero hardcoding or facades.
- 100% test pass on all repository packages.

## Current Parent
- Conversation ID: 048a592a-b159-4ea3-a5c3-e619b708dd31
- Updated: 2026-10-17T00:15:00Z

## Task Summary
- **What to build**: M1 Telemetry models, schema migrations, and dual persistence storage
- **Success criteria**:
  - `internal/ballchasing`: complete telemetry domain models (core, boost, movement, positioning, demo, ball, camera, car, rank, uploader, min/max rank, overtime_seconds, status), custom UnmarshalJSON bridging nested detail and flat list responses, numeric slug fallback in MapPlaylistSlugToID.
  - `internal/storage`: match_players table & index, mmr_history table & index, player notes & tags, match stats_sync_status & stats_retry_count, safe PRAGMA table_info migrations, variadic RecordMatchResults with MIN/MAX monotonicity, replay date timestamp preservation, player rank hydration, and startup backfill migration in SQLiteStore and JSONStore.
  - Comprehensive unit and integration tests in `telemetry_test.go` and `m1_storage_test.go`.
  - 100% pass across all Go packages with zero race conditions (`go test -race ./...`).
- **Interface contracts**: PROJECT.md § Architecture & Database Schemas
- **Code layout**: internal/ballchasing, internal/storage

## Key Decisions Made
- Custom `UnmarshalJSON` on `ReplayPlayer` and `ReplayItem`: bridges nested detail responses (`stats.core`, `stats.demo`, etc.) with flat fields (`score`, `goals`, etc.) bidirectionally so existing downstream syncer/storage code never suffers zero-stat bugs.
- `MapPlaylistSlugToID`: added `strconv.Atoi(slug)` with `id > 0` fallback in the default branch.
- Safe PRAGMA-based schema migration in SQLite: inspects columns via `PRAGMA table_info` before executing `ALTER TABLE ADD COLUMN` to prevent duplicate column errors on modernc.org/sqlite.
- Dual persistence parity: both SQLiteStore and JSONStore implement full match_players and mmr_history indexing, notes/tags, and stats sync lifecycle.
- Variadic `matchTime ...time.Time` on `RecordMatchResults`: enables historical replay syncing while preserving backward compatibility for live events.
- Monotonic `MIN`/`MAX` timestamp updates: `first_seen_at = MIN(...)`, `last_seen_at = MAX(...)`, `last_played_at = MAX(...)` prevent historical replay syncs from overwriting newer activity.

## Artifact Index
- internal/ballchasing/types.go — full telemetry structs, custom UnmarshalJSON, numeric slug fallback
- internal/ballchasing/telemetry_test.go — comprehensive unmarshaling and slug mapping tests (100% pass)
- internal/storage/store.go — MatchPlayerRecord, MMRHistoryRecord, notes/tags, stats sync status, ParseReplayDate, wrapContextErr
- internal/storage/sqlite.go — DDL, safe PRAGMA migrations, startup backfill, timestamp monotonicity, rank hydration, subquery pagination, CRUD
- internal/storage/jsonstore.go — in-memory indexing, compact JSON serialization, startup backfill, timestamp monotonicity, rank hydration, CRUD
- internal/storage/m1_storage_test.go — 8 comprehensive test suites for migrations, backfill, timestamp, and parity
- internal/auth/auth_test.go — updated mockStateStore to embed storage.StateStore and match RecordMatchResults signature
- internal/playertrack/tracker_test.go — updated mockFaultStore.RecordMatchResults signature with variadic matchTime

## Change Tracker
- **Files modified**:
  - `internal/ballchasing/types.go`: Complete telemetry structs, custom UnmarshalJSON bridging nested/flat, numeric slug fallback.
  - `internal/ballchasing/telemetry_test.go`: 4 test suites covering full nested, flat compatibility, pending/failed status, numeric slug fallback.
  - `internal/storage/store.go`: Data models, DDL structs, ParseReplayDate, wrapContextErr, StateStore interface extensions.
  - `internal/storage/sqlite.go`: DDL, PRAGMA migrations, startup backfill, timestamp monotonicity, rank hydration, subquery pagination, CRUD.
  - `internal/storage/jsonstore.go`: Dual persistence parity, compact JSON serialization, backfill, monotonicity, rank hydration, CRUD.
  - `internal/storage/m1_storage_test.go`: 8 test suites validating all M1 specifications across SQLiteStore and JSONStore.
  - `internal/auth/auth_test.go`: Embedded storage.StateStore in mockStateStore, updated RecordMatchResults signature.
  - `internal/playertrack/tracker_test.go`: Updated mockFaultStore.RecordMatchResults signature with variadic matchTime.
- **Build status**: PASS (`go vet ./...` 100% clean, `go test -race ./internal/ballchasing/...` PASS, `go test -race ./internal/storage/...` PASS in 88.8s)
- **Pending issues**: None

## Quality Status
- **Build/test result**: PASS. All unit and integration tests in `internal/ballchasing` and `internal/storage` pass 100% under `-race` with zero race conditions.
- **Lint status**: Clean (zero `go vet` violations across all repo packages).
- **Tests added/modified**: `telemetry_test.go` (4 suites), `m1_storage_test.go` (8 suites), `auth_test.go` and `tracker_test.go` interface compatibility.

