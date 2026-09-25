# Dispatch: m4_challenger_1

**Milestone**: M4 - Syncer, Daemon Engine & CLI
**Role**: Syncer Pipeline Challenger (internal/syncer)

## Objectives
Adversarially challenge `internal/syncer`:
1. In-flight recovery: verify `store.RecoverInFlight` properly resets orphaned `DOWNLOADING` and `UPLOADING` statuses to `PENDING` on startup.
2. Delayed Replay URLs: simulate discovery cycle with empty ReplayURL (marked `SKIPPED`), followed by cycle 2 with valid ReplayURL (promoted to `PENDING` and subsequently downloaded).
3. Ballchasing 409 Duplicate: verify duplicate replay returns `DUPLICATE` status, increments `DuplicateCount`, does not trigger download/upload failure, and proceeds cleanly.
4. Dry-run guarantee: assert that in dry-run mode, zero database mutations occur (no records created/updated), and zero downloads/uploads are executed.
5. Context cancellation: test cancellation mid-download and mid-upload, verifying prompt exit and proper error return.

Verification commands:
```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
cd d:\code\rl-api-utils
go test -v -count=1 ./internal/syncer/...
```

Provide your verdict (APPROVE or CHALLENGE_FAILED) in `d:\code\rl-api-utils\.agents\teamwork\m4_challenger_1\handoff.md` and notify parent via `send_message`.

## 2026-09-25T04:33:58Z
Received dispatch:
Adversarially challenge internal/syncer:
1. In-flight recovery: verify store.RecoverInFlight resets orphaned DOWNLOADING and UPLOADING statuses to PENDING on startup.
2. Delayed Replay URLs: verify transition from SKIPPED to PENDING when replay URL arrives in cycle 2.
3. Ballchasing 409 Duplicate: verify duplicate replay returns DUPLICATE status without treating as error.
4. Dry-run guarantee: verify zero DB mutations and zero network uploads in dry-run mode.
5. Context cancellation: test cancellation mid-download and mid-upload, verifying prompt exit and proper error return.
6. Run tests:
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   cd d:\code\rl-api-utils
   go test -v -count=1 ./internal/syncer/...
7. Provide your verdict (APPROVE or CHALLENGE_FAILED) in d:\code\rl-api-utils\.agents\teamwork\m4_challenger_1\handoff.md and notify parent via send_message.
