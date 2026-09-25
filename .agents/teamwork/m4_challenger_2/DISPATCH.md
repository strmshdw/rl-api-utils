# Dispatch: m4_challenger_2

**Milestone**: M4 - Syncer, Daemon Engine & CLI
**Role**: Daemon & CLI Challenger (internal/daemon, cmd/rl-sync)

## Objectives
Adversarially challenge `internal/daemon` and `cmd/rl-sync`:
1. Immediate startup run: verify `Daemon.Start(ctx)` runs the first cycle immediately without sleeping for `cfg.Sync.PollInterval`.
2. Single-run mode (`--once`): verify execution of exactly 1 cycle and clean exit (exit code 0).
3. Overlapping cycle protection: verify that if a cycle takes longer than `cfg.Sync.PollInterval`, overlapping executions are skipped and do not deadlock or run concurrently.
4. Graceful shutdown drain: trigger context cancellation while a cycle is actively running, asserting the in-flight cycle completes before `Start()` returns.
5. CLI flag precedence: verify CLI flags override environment variables and configuration file values.
6. Error exit codes: verify exit code 1 when configuration or authentication validation fails.

Verification commands:
```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
cd d:\code\rl-api-utils
go test -v -count=1 ./internal/daemon/...
go test -v -count=1 ./cmd/rl-sync/...
```

Provide your verdict (APPROVE or CHALLENGE_FAILED) in `d:\code\rl-api-utils\.agents\teamwork\m4_challenger_2\handoff.md` and notify parent via `send_message`.
