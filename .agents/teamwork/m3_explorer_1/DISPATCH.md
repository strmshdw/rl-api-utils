# Dispatch: m3_explorer_1

**Milestone**: M3 - Ballchasing Replay Uploader
**Role**: Ballchasing API & Streaming Explorer (internal/ballchasing)

## Scope
Investigate and design the Ballchasing client upload architecture:
- Interface: `ReplayUploader` (`UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error)`)
- Multipart form construction:
  - Streaming file payload to avoid loading entire 5MB replays into RAM if possible, or buffered multipart writer.
  - Form field `"file"` with filename `"<matchGUID>.replay"`.
  - Configured `visibility` parameter (`"public"`, `"unlisted"`, `"private"`).
- Authentication:
  - Header: `Authorization: <token>` (raw token strictly without `Bearer ` prefix).
- Base URL configuration (default `https://ballchasing.com/api`, mockable for testing).
- File reading safety: ensure file handle is always closed promptly.
- Write your findings, proposed types, and design to `d:\code\rl-api-utils\.agents\teamwork\m3_explorer_1\handoff.md`.

## 2026-09-25T03:58:19Z
You are m3_explorer_1.
Working directory: d:\code\rl-api-utils\.agents\teamwork\m3_explorer_1
Read d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md, d:\code\rl-api-utils\PROJECT.md, and d:\code\rl-api-utils\.agents\teamwork\m3_explorer_1\DISPATCH.md.
Also review the survey findings in d:\code\rl-api-utils\.agents\teamwork\survey_miner_ballchasing_1\handoff.md.

Explore the Ballchasing client upload architecture (internal/ballchasing):
- Interface: ReplayUploader (UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error))
- Multipart form construction with "file" part and "visibility" parameter
- Raw Authorization header: Authorization: <token> (WITHOUT Bearer)
- Configurable base URL for testing
- File streaming and safe handle closing
Write your report and proposed code to d:\code\rl-api-utils\.agents\teamwork\m3_explorer_1\handoff.md and notify parent via send_message.
