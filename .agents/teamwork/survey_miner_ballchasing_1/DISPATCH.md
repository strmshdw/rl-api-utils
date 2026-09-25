# Dispatch for Survey Spec Miner: Ballchasing API & Replay Downloading

**Target**: Specification & API mining for Ballchasing.com API and .replay binary downloading.
**Original Requirements**: d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md
**Working Directory**: d:\code\rl-api-utils\.agents\teamwork\survey_miner_ballchasing_1

## 2026-09-25T02:57:56Z

You are survey_miner_ballchasing_1.
Your working directory is: d:\code\rl-api-utils\.agents\teamwork\survey_miner_ballchasing_1
Read d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md and d:\code\rl-api-utils\.agents\teamwork\survey_miner_ballchasing_1\DISPATCH.md.

Perform comprehensive specification and API mining on Ballchasing.com API and replay downloading:
- POST /v2/upload endpoint specs, multipart/form-data requirements, file field, visibility options (public, unlisted, private), Authorization header
- Status codes & schemas: HTTP 201 Created (id, location), HTTP 409 Conflict (handling duplicate replay), HTTP 429 Too Many Requests (rate limiting, Retry-After header, backoff strategy), HTTP 401 Unauthorized
- Replay downloading mechanics from PsyNet URLs (HTTP GET, file streaming, validation, temp files)
Write your detailed report to d:\code\rl-api-utils\.agents\teamwork\survey_miner_ballchasing_1\handoff.md. Use send_message to report completion to parent.
