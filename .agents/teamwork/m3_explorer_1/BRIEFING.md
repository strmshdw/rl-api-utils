# BRIEFING — 2026-09-25T04:05:00Z

## Mission
Investigate and design the Ballchasing client upload architecture (internal/ballchasing) including ReplayUploader interface, multipart form construction, raw Authorization header, streaming and safe handle closing, and mockable base URL.

## 🔒 My Identity
- Archetype: explorer
- Roles: Ballchasing API & Streaming Explorer (internal/ballchasing)
- Working directory: d:\code\rl-api-utils\.agents\teamwork\m3_explorer_1
- Original parent: 6e6c9567-59d2-415e-8d6e-41314a903548
- Milestone: M3 - Ballchasing Replay Uploader

## 🔒 Key Constraints
- Read-only investigation — do NOT implement in production source code directory directly (propose code in reports and proposed files)
- Strict raw token format in Authorization header: `Authorization: <token>` (no `Bearer ` prefix)
- Interface contract: `ReplayUploader` (`UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error)`)
- Multipart form field: `"file"` with filename `"<matchGUID>.replay"`
- Visibility parameter: `"public"`, `"unlisted"`, `"private"`
- Safe file reading and handle closure

## Current Parent
- Conversation ID: 6e6c9567-59d2-415e-8d6e-41314a903548
- Updated: 2026-09-25T04:05:00Z

## Investigation State
- **Explored paths**:
  - `PROJECT.md`, `ORIGINAL_REQUEST.md`, `survey_miner_ballchasing_1/handoff.md`
  - `internal/testutil/mock_ballchasing.go`, `internal/config/config.go`, `internal/psynet/downloader.go`
  - `test/e2e/e2e_test.go`, `test/e2e/tier1_feature_test.go`, `test/e2e/tier2_boundary_test.go`, `test/e2e/tier3_pairwise_test.go`
  - Peer explorer findings: `m3_explorer_2/handoff.md` (response handling and backoff)
- **Key findings**:
  - `ReplayUploader` interface (`UploadReplay` + `Ping`) satisfies clean architecture contracts.
  - Multipart form consists of a single file part `"file"` with filename `<matchGUID>.replay`.
  - Both zero-RAM streaming (`io.MultiReader` + exact `Content-Length`) and buffered (`bytes.Buffer`) modes tested and verified with `go test` and `go vet`.
  - Windows file handle safety: file handles are opened per-attempt and closed promptly via `defer closer.Close()`, ensuring no handles are held across retry backoff sleeps.
  - Authorization header: raw token string strictly without `Bearer ` prefix.
  - URL builder properly formats `?visibility=` and `&group=` query parameters, fully mock-compatible with `MockBallchasingServer`.
- **Unexplored areas**:
  - None within M3 Explorer 1 scope. Fully investigated and verified.

## Key Decisions Made
- Supported both zero-RAM streaming (`io.MultiReader` with exact `Content-Length`) and buffered in-memory upload (`bytes.Buffer`), controlled via `StreamUpload: bool` option.
- Scoped file handle lifecycle strictly within `doUploadAttempt`, guaranteeing handle closure before entering backoff sleep loops.
- Created `proposed_types.go` and `proposed_client.go` in explorer folder for turn-key implementation by worker agents.

## Artifact Index
- `.agents/teamwork/m3_explorer_1/BRIEFING.md` — persistent situational awareness
- `.agents/teamwork/m3_explorer_1/progress.md` — liveness heartbeat
- `.agents/teamwork/m3_explorer_1/proposed_types.go` — proposed types and sentinel errors
- `.agents/teamwork/m3_explorer_1/proposed_client.go` — proposed client implementation
- `.agents/teamwork/m3_explorer_1/handoff.md` — 5-component handoff report
