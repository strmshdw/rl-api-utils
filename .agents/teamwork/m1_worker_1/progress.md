# Progress: m1_worker_1 (Milestone M1: Storage Telemetry & Persistence)

Last visited: 2026-10-17T00:48:00Z

## Current Status
- [x] Read DISPATCH.md, ORIGINAL_REQUEST.md, PROJECT.md, and explorer handoff reports
- [x] Initialized BRIEFING.md
- [x] Task 1: Complete Ballchasing telemetry types in `internal/ballchasing/types.go` with custom `UnmarshalJSON` (flat + nested detail support) and numeric slug fallback in `MapPlaylistSlugToID`.
- [x] Task 2: Comprehensive telemetry unit tests in `internal/ballchasing/telemetry_test.go` (100% pass under race detector).
- [x] Task 3: Data models and DDL in `internal/storage/store.go` and `sqlite.go` (`match_players`, `mmr_history`, player notes/tags, match stats sync status & retries, safe PRAGMA migrations, startup backfill migration, variadic `RecordMatchResults`).
- [x] Task 4: Complete parity implementation in `internal/storage/jsonstore.go`.
- [x] Task 5: Comprehensive unit tests in `internal/storage/m1_storage_test.go` (8 test suites covering all M1 requirements).
- [x] Task 6: Updated `internal/auth/auth_test.go` `mockStateStore` signature for compilation parity.
- [x] Task 7: Resolved concurrency stress test timeouts under `-race` via JSON compact serialization and SQL subquery pagination with `wrapContextErr`.
- [ ] Task 8: Final verification pass across repository (`go vet ./...`, `go test -race ./internal/ballchasing/...`, `go test -race ./internal/storage/...`, `go test ./...`).
- [ ] Task 9: Write handoff.md and report to parent orchestrator.
