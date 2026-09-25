# Dispatch: m3_challenger_2

**Milestone**: M3 - Ballchasing Replay Uploader
**Role**: Backoff & Resilience Challenger (internal/ballchasing)

## Scope
Adversarially challenge and stress-test `internal/ballchasing` backoff and file safety:
- Scenarios:
  - 429 Too Many Requests: test multiple consecutive 429 responses, verify integer `Retry-After: 1` vs HTTP-date string vs absent header, and verify failure when retries exceed budget.
  - Context cancellation during backoff sleep: ensure immediate return of `ctx.Err()` without waiting for timer to expire.
  - Non-existent and 0-byte file handling.
  - Concurrency: multiple goroutines uploading concurrently with the same client instance.

## 2026-09-25T04:10:09Z
You are m3_challenger_2.
Working directory: d:\code\rl-api-utils\.agents\teamwork\m3_challenger_2
Read d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md, d:\code\rl-api-utils\PROJECT.md, and d:\code\rl-api-utils\.agents\teamwork\m3_challenger_2\DISPATCH.md.
Worker report is at d:\code\rl-api-utils\.agents\teamwork\m3_worker_1\handoff.md.

Adversarially challenge internal/ballchasing backoff and file safety:
1. 429 Too Many Requests: test multiple consecutive 429 responses, verify integer Retry-After: 1 vs HTTP-date string vs absent header, and verify failure when retries exceed budget.
2. Context cancellation during backoff sleep: ensure immediate return of ctx.Err() without waiting for timer to expire.
3. Non-existent and 0-byte file handling.
4. Concurrency: multiple goroutines uploading concurrently with the same client instance.
5. Run tests and provide your verdict (APPROVE or CHALLENGE_FAILED) in d:\code\rl-api-utils\.agents\teamwork\m3_challenger_2\handoff.md and notify parent via send_message.
