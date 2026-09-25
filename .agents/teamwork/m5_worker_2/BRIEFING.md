# BRIEFING — 2026-09-25T05:04:30Z

## Mission
Harden internal/ballchasing/client.go to ensure HTTP >= 500 status codes wrap ErrServerError on all attempts (including final attempt when retries are exhausted).

## 🔒 My Identity
- Archetype: m5_worker_2
- Roles: implementer, qa, specialist
- Working directory: d:\code\rl-api-utils\.agents\teamwork\m5_worker_2
- Original parent: 6e6c9567-59d2-415e-8d6e-41314a903548
- Milestone: M5 - Final Milestone & Hardening

## 🔒 Key Constraints
- Exclusive write ownership: internal/ballchasing/client.go
- Ensure HTTP >= 500 status codes wrap ErrServerError on all attempts, including the final attempt when retries are exhausted.
- 100% pass on go test -v -count=1 ./internal/ballchasing/..., go test -v -count=1 ./test/e2e/..., go test -count=1 ./...
- Zero go vet warnings.
- DO NOT CHEAT: genuine logic only, no hardcoded strings or test bypasses.

## Current Parent
- Conversation ID: 6e6c9567-59d2-415e-8d6e-41314a903548
- Updated: 2026-09-25T05:01:10Z

## Task Summary
- **What to build**: Update doUploadAttempt in internal/ballchasing/client.go to wrap ErrServerError for HTTP status >= 500 on all attempts, even when retries are exhausted.
- **Success criteria**: All tests pass, errors.Is(err, ErrServerError) holds true for 5xx errors on exhausted retries, go vet passes cleanly.
- **Interface contracts**: PROJECT.md § ReplayUploader
- **Code layout**: PROJECT.md § Code Layout

## Key Decisions Made
- Modified `doUploadAttempt` in `internal/ballchasing/client.go` to branch on `resp.StatusCode >= 500`: when `attempt < c.maxRetries`, calculate backoff and return `retryable=true` wrapping `ErrServerError`; when `attempt >= c.maxRetries`, return `retryable=false` with 0 wait duration, wrapping `ErrServerError` with `fmt.Errorf("%w: HTTP %d (retries exhausted): %s", ErrServerError, resp.StatusCode, string(respBytes))`.
- Strictly adhered to exclusive write ownership (`internal/ballchasing/client.go`).

## Artifact Index
- DISPATCH.md — Assignment instructions
- BRIEFING.md — Persistent context & state
- progress.md — Liveness & task execution progress
- handoff.md — Final handoff report

## Change Tracker
- **Files modified**: `internal/ballchasing/client.go` (wrapped ErrServerError on 5xx exhausted retries)
- **Build status**: PASS (100% tests passing across ballchasing, e2e, and entire repo)
- **Pending issues**: None

## Quality Status
- **Build/test result**: PASS (go test ./...: 100% pass)
- **Lint status**: PASS (go vet ./...: 0 warnings)
- **Tests added/modified**: Verified via existing `test/e2e/tier5_stress_test.go` ServerError500_BudgetExhaustion subtest

## Loaded Skills
None.
