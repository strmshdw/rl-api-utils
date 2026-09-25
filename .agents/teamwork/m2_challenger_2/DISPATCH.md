# Dispatch: m2_challenger_2

**Milestone**: M2 - Auth & PsyNet Integration
**Role**: PsyNet Challenger (internal/psynet)

## Scope
Adversarially challenge and stress-test `internal/psynet`:
- Test scenarios:
  - `ReplayDownloader`:
    - Extreme payloads: 0-byte, 500-byte (rejected <1024 bytes), exactly 1024 bytes (accepted), valid TAGAME binary.
    - HTTP status codes: 403 expired pre-signed URL, 404 deleted replay, 500 server error, 503 unavailable.
    - Mid-stream network interruption / dropped connections: ensure `.tmp` file is immediately cleaned up and no orphan remains.
    - Path traversal: matchGUID with `../` or `..\` rejected.
    - Windows file contention: concurrent downloads into the same `destDir`.
  - `MatchHistoryProvider`:
    - Matches with empty `ReplayURL` correctly preserved as `SKIPPED` candidate.
    - Malformed match GUIDs skipped.
    - Connection drop mid-query transparently reconnected.
    - Canceled context terminates immediately without hanging.
- Run tests and provide verdict (APPROVE or CHALLENGE_FAILED) in `d:\code\rl-api-utils\.agents\teamwork\m2_challenger_2\handoff.md` and notify parent via `send_message`.
