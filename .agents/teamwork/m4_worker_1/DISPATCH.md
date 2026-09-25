# Dispatch: m4_worker_1

**Milestone**: M4 - Syncer, Daemon Engine & CLI
**Role**: Implementation Worker (internal/syncer, internal/daemon, cmd/rl-sync)

## Objectives
Implement the complete sync orchestration pipeline, daemon lifecycle engine, and CLI entrypoint for Milestone 4:
1. `internal/syncer`:
   - `interfaces.go`: Type aliases for `DiscoveredMatch` and `UploadResult`, `StateStore`, `MatchHistoryProvider`, `ReplayDownloader`, `ReplayUploader`, `SyncStats`.
   - `syncer.go`: `Syncer` struct, `New` constructor, `RunCycle(ctx)` implementing the 6-stage lifecycle (in-flight recovery, match discovery, upserting discovered matches, downloading replays, uploading replays to Ballchasing, returning `SyncStats`), dry-run simulation mode (`dryRun = true`), error handling and context cancellation.
   - `syncer_test.go`: Comprehensive unit tests with mock stores and clients.
2. `internal/daemon`:
   - `daemon.go`: `Daemon` struct, `New` constructor, `NewLogger` structured logger (`log/slog`), `Start(ctx)` running immediate initial cycle, recurring ticker loop at `cfg.Sync.PollInterval`, `--once` single-run mode, graceful drain of in-flight cycles before exit.
   - `daemon_test.go`: Unit tests for immediate execution, ticker loop, single-run mode, context cancellation, and logging options.
3. `cmd/rl-sync`:
   - `main.go`: CLI entrypoint with flag parsing (`--config / -c`, `--once`, `--dry-run`, `--log-level`, `--log-format`, etc.), config loading precedence, component wiring (config, logger, storage, auth, psynet client/downloader, ballchasing client with pre-flight ping, syncer, daemon), root `signal.NotifyContext(SIGINT, SIGTERM)`, deferred resource cleanup, exit codes (0 on normal/once/interrupt, 1 on error).
   - `main_test.go`: Unit tests for CLI flags, defaults, and validation.

## Inputs & Explorer Proposals
- Syncer: `d:\code\rl-api-utils\.agents\teamwork\m4_explorer_1\handoff.md`
- Daemon: `d:\code\rl-api-utils\.agents\teamwork\m4_explorer_2\proposed_daemon.go` and `proposed_daemon_test.go`
- CLI Entrypoint: `d:\code\rl-api-utils\.agents\teamwork\m4_explorer_3\proposed_main.go` and `proposed_main_test.go`

## Exclusive Write Ownership
- `internal/syncer/interfaces.go`
- `internal/syncer/syncer.go`
- `internal/syncer/syncer_test.go`
- `internal/daemon/daemon.go`
- `internal/daemon/daemon_test.go`
- `cmd/rl-sync/main.go`
- `cmd/rl-sync/main_test.go`

## Verification Commands
```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
cd d:\code\rl-api-utils
go test -v -count=1 ./internal/syncer/...
go test -v -count=1 ./internal/daemon/...
go test -v -count=1 ./cmd/rl-sync/...
go test -count=1 ./...
go vet ./internal/syncer/... ./internal/daemon/... ./cmd/rl-sync/...
```

Ensure 100% test pass and zero `go vet` warnings across the entire repository.

## 2026-09-25T04:23:47Z
You are m4_worker_1.
Your working directory is: d:\code\rl-api-utils\.agents\teamwork\m4_worker_1
Read d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md, d:\code\rl-api-utils\PROJECT.md, and d:\code\rl-api-utils\.agents\teamwork\m4_worker_1\DISPATCH.md.

Implement Milestone 4 (Syncer, Daemon Engine & CLI):
1. Review the explorer handoffs and proposals:
   - Syncer: d:\code\rl-api-utils\.agents\teamwork\m4_explorer_1\handoff.md
   - Daemon: d:\code\rl-api-utils\.agents\teamwork\m4_explorer_2\proposed_daemon.go and proposed_daemon_test.go
   - CLI Entrypoint: d:\code\rl-api-utils\.agents\teamwork\m4_explorer_3\proposed_main.go and proposed_main_test.go
2. Exclusive write ownership:
   - internal/syncer/interfaces.go
   - internal/syncer/syncer.go
   - internal/syncer/syncer_test.go
   - internal/daemon/daemon.go
   - internal/daemon/daemon_test.go
   - cmd/rl-sync/main.go
   - cmd/rl-sync/main_test.go
3. Implement the packages, ensuring clean integration across storage, auth, psynet, ballchasing, syncer, daemon, and cmd/rl-sync.
4. Execute verification commands: Ensure 100% test pass and zero vet warnings.
5. Mandatory Integrity Warning: Do not cheat, no facade/dummy code.
6. Write report to handoff.md and notify parent via send_message.
