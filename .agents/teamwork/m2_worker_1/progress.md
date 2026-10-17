# Progress — M2 Syncer, Post-Upload Polling, Retry Queue & Parser

Last visited: 2026-10-17T02:24:00Z

## Status
- Current Step: Verification & Handoff
- Completed:
  - Phase 1: Replay Parser Fixes (`internal/replay/parser.go`, `internal/replay/parser_test.go`) — RFC3339 date normalization, overtime extraction with telemetry fallback, demolition extraction, verified across all 20 real replays.
  - Phase 2: Storage Retry Queue & Backoff (`internal/storage/store.go`, `internal/storage/sqlite.go`, `internal/storage/jsonstore.go`, `internal/storage/m2_retry_test.go`) — Bounded retry queue, exponential backoff, duplicate handling in `ListPendingStatsSync`, download/stats retry resets.
  - Phase 3: Syncer Post-Upload Telemetry Polling & Local Player Resolution (`internal/syncer/interfaces.go`, `internal/syncer/syncer.go`, `internal/syncer/syncer_test.go`) — Step 6 `PollAndSyncPendingStats` in `RunCycle`, 6-tier `ResolveLocalPlayer` with bot immunity, matchup outcome recording with actual match timestamp, match player records.
- In Progress:
  - Full repo verification (`go test -race ./...`).
- Next Steps:
  - Prepare 5-component `handoff.md`.
  - Send completion message to `orchestrator_9`.
