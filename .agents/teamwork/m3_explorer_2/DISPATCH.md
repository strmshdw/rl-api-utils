# Dispatch: m3_explorer_2

**Milestone**: M3 - Ballchasing Replay Uploader
**Role**: Response Handling & Rate Limiting Explorer (internal/ballchasing)

## Scope
Investigate and design the response parsing, duplicate detection, and backoff engine:
- Status Code Handling:
  - HTTP 201 Created: Parse JSON `{ "id": "...", "location": "..." }`, return `UploadResult{ID, Location, IsDuplicate: false}`.
  - HTTP 409 Conflict: Parse JSON `{ "error": "...", "id": "...", "location": "..." }`, return `UploadResult{ID, Location, IsDuplicate: true}`, error is `nil` (idempotent duplicate, not a failure!).
  - HTTP 429 Too Many Requests: Parse `Retry-After` header (seconds as int or RFC1123 date), implement exponential backoff with jitter and retry budget (default 3 retries).
  - HTTP 401 Unauthorized: Immediate fatal error `ErrInvalidAPIKey`.
  - HTTP 400 Bad Request / 404 / 500: Clear error propagation.
- Context cancellation: ensure backoff sleeps respect `ctx.Done()`.
- Ping / API Key verification: `Ping(ctx)` method querying `GET /` to validate API key before starting synchronization.
- Write your findings, proposed algorithms, and design to `d:\code\rl-api-utils\.agents\teamwork\m3_explorer_2\handoff.md`.

## 2026-09-25T03:58:19Z
You are m3_explorer_2.
Working directory: d:\code\rl-api-utils\.agents\teamwork\m3_explorer_2
Read d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md, d:\code\rl-api-utils\PROJECT.md, and d:\code\rl-api-utils\.agents\teamwork\m3_explorer_2\DISPATCH.md.
Also review survey findings in d:\code\rl-api-utils\.agents\teamwork\survey_miner_ballchasing_1\handoff.md.

Explore response handling, duplicate detection, and backoff for internal/ballchasing:
- HTTP 201 Created -> UploadResult{ID, Location, IsDuplicate: false}
- HTTP 409 Conflict -> UploadResult{ID, Location, IsDuplicate: true}, err == nil (idempotent duplicate!)
- HTTP 429 Too Many Requests -> parse Retry-After, exponential backoff with retry budget, context-aware sleep
- HTTP 401 Unauthorized -> immediate fatal ErrInvalidAPIKey
- Ping(ctx) API key verification method
Write your report and proposed code to d:\code\rl-api-utils\.agents\teamwork\m3_explorer_2\handoff.md and notify parent via send_message.
