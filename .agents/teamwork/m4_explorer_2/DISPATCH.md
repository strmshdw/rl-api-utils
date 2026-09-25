# Dispatch: m4_explorer_2

**Milestone**: M4 - Syncer, Daemon Engine & CLI
**Role**: Daemon Lifecycle Explorer (internal/daemon)

## Scope
Investigate and design `internal/daemon`:
- Daemon Lifecycle (`daemon.go`):
  - Immediate execution of first sync cycle on start.
  - Ticker loop running every `cfg.Sync.PollInterval` (default 5m).
  - Context cancellation & signal trapping: listen for `SIGINT` / `SIGTERM` via `signal.NotifyContext`.
  - Graceful drain: if shutdown signal arrives while a sync cycle is in flight, let the active cycle finish cleanly before exiting.
  - Single-run mode (`Once: bool`): executes exactly 1 sync cycle and exits cleanly.
  - Structured logging with Go standard library `log/slog`:
    - Support log levels: `DEBUG`, `INFO`, `WARN`, `ERROR`.
    - Support log formats: `text` and `json`.
- Unit test strategy (`daemon_test.go`) testing ticker intervals, context cancellation, graceful shutdown, single-run mode, and error propagation.

Write your report and proposed code to `d:\code\rl-api-utils\.agents\teamwork\m4_explorer_2\handoff.md`.

## 2026-09-25T04:17:32Z
You are m4_explorer_2.
Your working directory is: d:\code\rl-api-utils\.agents\teamwork\m4_explorer_2
Read d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md, d:\code\rl-api-utils\PROJECT.md, and d:\code\rl-api-utils\.agents\teamwork\m4_explorer_2\DISPATCH.md.

Explore the daemon lifecycle subsystem for Milestone 4 (internal/daemon):
- Daemon Lifecycle (daemon.go):
  - Daemon struct wrapping syncer.Syncer and config.Config.
  - Start(ctx context.Context) error:
    - Runs immediate initial sync cycle on startup.
    - If Once: true (single-run mode), returns after the first cycle.
    - Otherwise, runs ticker loop at cfg.Sync.PollInterval (default 5m).
    - Traps context cancellation gracefully: if a sync cycle is in flight when context cancels, allows cycle to complete or cleanly drain before returning.
  - Logging integration: slog with configurable level and format.
- Unit test strategy (daemon_test.go): testing immediate execution, ticker loop, single-run mode, context cancellation, and error handling.
Write your detailed report and proposed Go code to d:\code\rl-api-utils\.agents\teamwork\m4_explorer_2\handoff.md and notify parent via send_message.

