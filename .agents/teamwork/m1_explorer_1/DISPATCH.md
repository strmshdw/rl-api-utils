# Dispatch for M1 Explorer 1: SQLite Storage Engine & Idempotency State Machine

**Milestone**: M1 - Storage & Configuration
**Role**: Explorer 1 (`teamwork_preview_explorer`)
**Original Request**: `d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md`
**Project Specification**: `d:\code\rl-api-utils\PROJECT.md`
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m1_explorer_1`

## Objectives
1. Read `ORIGINAL_REQUEST.md` and `PROJECT.md` (specifically the Interface Contracts for `internal/storage` and the schema).
2. Deep dive into the implementation strategy for `internal/storage`:
   - Using pure Go SQLite (`modernc.org/sqlite`) with zero CGO dependencies.
   - Exact SQL table schema for `matches` and `auth_state` tables.
   - Database connection management (WAL mode, busy timeout, single writer or connection pooling).
   - Method implementations for `StateStore` interface:
     - `GetMatch(ctx, guid)`
     - `ListPendingDownloads(ctx)`
     - `ListPendingUploads(ctx)`
     - `UpsertDiscoveredMatches(ctx, matches)`
     - Status transition methods (`MarkDownloading`, `MarkDownloaded`, `MarkDownloadFailed`, `MarkUploading`, `MarkUploaded`, `MarkDuplicate`, `MarkUploadFailed`)
     - `RecoverInFlight(ctx)`: resets in-flight states on startup
     - `SaveAuthState(ctx, provider, token, accountID, displayName)` / `GetAuthState(ctx, provider)`
   - Design unit tests for SQLite store (`sqlite_test.go`): verifying concurrency, transactions, state transitions, duplicate marking, crash recovery.
3. Output comprehensive findings and recommended implementation code structures in `d:\code\rl-api-utils\.agents\teamwork\m1_explorer_1\handoff.md`.

## 2026-09-25T03:04:06Z
You are m1_explorer_1.
Working directory: d:\code\rl-api-utils\.agents\teamwork\m1_explorer_1
Read d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md, d:\code\rl-api-utils\PROJECT.md, and d:\code\rl-api-utils\.agents\teamwork\m1_explorer_1\DISPATCH.md.

Explore the pure Go SQLite storage engine for Milestone 1 (internal/storage):
- Driver: modernc.org/sqlite (zero CGO)
- SQL Schema: matches and auth_state tables with indexing
- Connection management: WAL mode, busy timeout, transactions
- Implementation details for StateStore interface methods (GetMatch, ListPendingDownloads, ListPendingUploads, UpsertDiscoveredMatches, MarkDownloading, MarkDownloaded, MarkDownloadFailed, MarkUploading, MarkUploaded, MarkDuplicate, MarkUploadFailed, RecoverInFlight, SaveAuthState, GetAuthState)
- Unit test strategy in sqlite_test.go covering CRUD, duplicate marking, crash recovery, and concurrency safety.
Write your report to d:\code\rl-api-utils\.agents\teamwork\m1_explorer_1\handoff.md and notify parent via send_message.
