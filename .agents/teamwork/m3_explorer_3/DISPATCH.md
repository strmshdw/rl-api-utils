# Dispatch: m3_explorer_3

**Milestone**: M3 - Ballchasing Replay Uploader
**Role**: Test Architecture & Edge Case Explorer (internal/ballchasing)

## Scope
Investigate and design the comprehensive unit and integration test suite for `internal/ballchasing`:
- Mock Server Integration:
  - Utilize `internal/testutil/mock_ballchasing.go` (`NewMockBallchasingServer`).
  - Test matrix:
    1. Successful upload (201 Created) -> verify ID, Location, IsDuplicate=false.
    2. Duplicate upload (409 Conflict) -> verify ID, Location, IsDuplicate=true, err==nil.
    3. Rate limiting (429 Too Many Requests) with `Retry-After: 1` -> verify automatic backoff and retry success.
    4. Rate limit budget exhaustion -> verify error returned after max retries.
    5. Invalid API key (401 Unauthorized) -> verify immediate error without retries.
    6. Non-existent file path -> verify file read error.
    7. Empty file (0-byte) -> verify error.
    8. Context cancellation during upload or backoff -> verify immediate `ctx.Err()`.
    9. Ping method (200 OK vs 401 Unauthorized).
    10. Custom visibility parameter propagation.


## 2026-09-25T03:58:19Z
You are m3_explorer_3.
Working directory: d:\code\rl-api-utils\.agents\teamwork\m3_explorer_3
Read d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md, d:\code\rl-api-utils\PROJECT.md, and d:\code\rl-api-utils\.agents\teamwork\m3_explorer_3\DISPATCH.md.
Also review d:\code\rl-api-utils\internal\testutil\mock_ballchasing.go and survey findings in d:\code\rl-api-utils\.agents\teamwork\survey_miner_ballchasing_1\handoff.md.

Explore test architecture and edge cases for internal/ballchasing:
- Comprehensive test matrix using testutil.NewMockBallchasingServer:
  - 201 Created, 409 Conflict (IsDuplicate=true), 429 backoff and retry success, 429 retry budget exhaustion, 401 Unauthorized immediate rejection, missing/empty file, context cancellation, Ping method, visibility settings.
Write your report and proposed client_test.go to d:\code\rl-api-utils\.agents\teamwork\m3_explorer_3\handoff.md and notify parent via send_message.
