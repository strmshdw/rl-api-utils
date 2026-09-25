# BRIEFING — 2026-09-25T04:22:00Z

## Mission
Explore CLI entrypoint & end-to-end component wiring for cmd/rl-sync (Milestone 4).

## 🔒 My Identity
- Archetype: explorer
- Roles: CLI & Integration Explorer
- Working directory: d:\code\rl-api-utils\.agents\teamwork\m4_explorer_3
- Original parent: 6e6c9567-59d2-415e-8d6e-41314a903548
- Milestone: M4 - Syncer, Daemon Engine & CLI

## 🔒 Key Constraints
- Read-only investigation — do NOT implement directly in codebase (propose via handoff.md)
- Write only to my folder: d:\code\rl-api-utils\.agents\teamwork\m4_explorer_3\
- Keep BRIEFING.md under ~100 lines (preserve 🔒 sections)
- Write 5-component handoff report (Observation, Logic Chain, Caveats, Conclusion, Verification Method)

## Current Parent
- Conversation ID: 6e6c9567-59d2-415e-8d6e-41314a903548
- Updated: 2026-09-25T04:22:00Z

## Investigation State
- **Explored paths**: `ORIGINAL_REQUEST.md`, `PROJECT.md`, `internal/config/*`, `internal/storage/*`, `internal/auth/*`, `internal/psynet/*`, `internal/ballchasing/*`, `test/e2e/*`, and peer explorer designs (`m4_explorer_1`, `m4_explorer_2`).
- **Key findings**:
  1. CLI Flags: `-c / --config`, `--once`, `--dry-run`, `--log-level`, `--log-format`, plus `--poll-interval`, `--replay-dir`, `--db-path`, `--provider`, `--version`, `--help`.
  2. Flag parsing with `flag.FlagSet` and `fs.Visit` ensures exact precedence: `CLI flags > Env vars (RL_SYNC_*) > Config file > Defaults`.
  3. Structured logging via `daemon.NewLogger(cfg.Logging, r.Stderr)` supporting `text` and `json` formats with `debug`, `info`, `warn`, `error` levels.
  4. Unified component wiring: `config.Load` -> `storage.NewStore` -> `auth.NewProvider` + `auth.Authenticate` -> `psynet.NewClient` + `psynet.NewDownloader` + `CleanupStaleTempFiles` -> `ballchasing.NewClient` + `bcClient.Ping` -> `syncer.New` -> `daemon.New` -> `daemon.Start(ctx)` -> graceful drain.
  5. Signal handling with `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)` (verified standard and portable on Windows Go 1.24).
  6. Pluggable `Runner` struct allows testing `main.go` logic without child processes or real network access.
- **Unexplored areas**: None. Complete end-to-end lifecycle and test suite designed.

## Key Decisions Made
- Abstract CLI execution into `Runner` struct with default production factory implementations and pluggable hooks for unit tests.
- Use `fs.Visit` to detect only explicitly provided flags, preserving non-overridden config file and environment settings.
- Implement `authSupplier` bridging `auth.AuthProvider` to `psynet.CredentialsSupplier`, supporting both Epic and Steam authentication flows and handling token expiration / renewal transparently.
- Exit code discipline: 0 on normal termination, `--once` mode completion, and clean signal trap; 1 on configuration, validation, store, auth, or ping failures.

## Artifact Index
- DISPATCH.md — Task assignment and instructions
- BRIEFING.md — Persistent working memory
- proposed_main.go — Proposed CLI entrypoint implementation (`cmd/rl-sync/main.go`)
- proposed_main_test.go — Proposed unit test suite (`cmd/rl-sync/main_test.go`)
- handoff.md — Final 5-component handoff report
