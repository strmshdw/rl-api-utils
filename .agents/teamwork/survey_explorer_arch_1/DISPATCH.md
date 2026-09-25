# Dispatch for Survey Explorer: Architecture, Persistence, Config & Test Harness

**Target**: Technical architecture, persistence layer, configuration, CLI, and mock test harness design.
**Original Requirements**: d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md
**Working Directory**: d:\code\rl-api-utils\.agents\teamwork\survey_explorer_arch_1

## Objectives
1. Read `d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md` thoroughly.
2. Architecture & Module Design:
   - Idiomatic Go project layout for `rl-api-utils`
   - Daemon polling loop (ticker, context cancellation, graceful shutdown on SIGINT/SIGTERM)
   - CLI flags: single-run (`--once` / `--dry-run`), config path, log level
   - Configuration schema: YAML/JSON or env vars (auth provider selection, tokens, ballchasing key, visibility, polling interval, replay dir, db path)
3. State Persistence & Idempotency:
   - Database schema (SQLite / pure Go sqlite like modernc.org/sqlite or structured JSON store)
   - Tracking fields: match GUID, match timestamp, download status & local path, ballchasing upload status & replay ID, error/retry counts, last updated timestamp
   - Transactional safety and idempotency invariants: no duplicate downloads, no duplicate uploads
4. Mock Test Harness Architecture:
   - Standalone mock PsyNet HTTP/RPC server
   - Mock Ballchasing HTTP server
   - Simulating multi-cycle polling (new matches appearing on cycle 2)
   - Testing restarts with persisted state
   - Verification of 100% test pass with `go test ./...`
5. Output your full report to `d:\code\rl-api-utils\.agents\teamwork\survey_explorer_arch_1\handoff.md`.

## 2026-09-25T02:57:56Z
Investigate and propose the daemon architecture, persistence layer, configuration, CLI, and mock test harness design:
- Go module layout and daemon lifecycle (polling ticker every 5m default, context cancellation, graceful shutdown)
- CLI flags: single-run / dry-run mode, config path, logging
- Configuration structure: env vars and config file support for auth provider, credentials, ballchasing key, visibility, polling interval, paths
- Persistence & Idempotency: schema design (SQLite / pure Go sqlite or JSON store) tracking match GUID, timestamp, download path, upload status, ballchasing replay ID. Guaranteeing no duplicate downloads and no duplicate uploads
- Mock test harness design: mock PsyNet server, mock Ballchasing server, multi-cycle polling tests, restart persistence verification, 100% test pass with go test ./...
Write your detailed report to d:\code\rl-api-utils\.agents\teamwork\survey_explorer_arch_1\handoff.md. Use send_message to report completion to parent.
