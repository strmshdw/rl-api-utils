# Dispatch: m4_explorer_3

**Milestone**: M4 - Syncer, Daemon Engine & CLI
**Role**: CLI & Integration Explorer (cmd/rl-sync)

## Scope
Investigate and design `cmd/rl-sync`:
- CLI Entrypoint (`main.go`):
  - Command-line flags using `flag.FlagSet` or standard `flag`:
    - `--config`, `-c`: path to config file (default checks `config.yaml`, `config.json`).
    - `--once`: single-run mode (default false).
    - `--dry-run`: simulation mode (default false).
    - `--log-level`: debug, info, warn, error (default info).
    - `--log-format`: text, json (default text).
  - Configuration loading precedence: CLI flags > Env vars (`RL_SYNC_*`) > Config file > Defaults.
  - Component wiring:
    1. Initialize `config.Config`.
    2. Initialize `log/slog` logger based on log-level and log-format.
    3. Initialize `storage.StateStore` (pure Go SQLite or JSON fallback based on config/path).
    4. Initialize `auth.AuthProvider` (Epic or Steam).
    5. Initialize `psynet.Client` (`MatchHistoryProvider`) and `psynet.HTTPDownloader` (`ReplayDownloader`).
    6. Initialize `ballchasing.Client` (`ReplayUploader`).
    7. Call `ballchasing.Ping(ctx)` to validate API key.
    8. Initialize `syncer.Syncer` and `daemon.Daemon`.
    9. Run daemon with root `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`.
    10. Close all resources in deferred cleanup (`store.Close()`, `psynetClient.Close()`).
  - Exit code discipline: 0 on normal termination/`--once`, 1 on fatal config/auth errors.
- Test strategy (`cmd/rl-sync/main_test.go` or package tests) verifying CLI flags, help text, dry-run flag propagation, and startup validation.

Write your report and proposed code to `d:\code\rl-api-utils\.agents\teamwork\m4_explorer_3\handoff.md`.

## 2026-09-25T04:17:32Z
Explore CLI entrypoint & component wiring for Milestone 4 (cmd/rl-sync):
- CLI flags & options:
  - --config / -c (path to config file)
  - --once (single-run mode)
  - --dry-run (simulation mode)
  - --log-level (debug, info, warn, error)
  - --log-format (text, json)
- End-to-end component wiring in main.go:
  1. Load config (CLI flag > env vars > file > defaults).
  2. Setup slog logger.
  3. Initialize storage (SQLite or JSON store).
  4. Initialize auth (Epic or Steam).
  5. Initialize psynet client (RPC history provider) & psynet downloader.
  6. Initialize ballchasing client & ping API key.
  7. Initialize syncer & daemon.
  8. signal.NotifyContext for SIGINT/SIGTERM.
  9. Run daemon and handle graceful exit.
- Unit test strategy (main_test.go or flag test) for CLI flags, defaults, and validation.
Write your detailed report and proposed Go code to d:\code\rl-api-utils\.agents\teamwork\m4_explorer_3\handoff.md and notify parent via send_message.

