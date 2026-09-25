# BRIEFING — 2026-09-25T03:42:00Z

## Mission
Investigate and design the Replay Downloader subsystem for Milestone 2 (`internal/psynet/downloader.go`) and tests (`internal/psynet/downloader_test.go`).

## 🔒 My Identity
- Archetype: explorer
- Roles: explorer, synthesizer
- Working directory: d:\code\rl-api-utils\.agents\teamwork\m2_explorer_3
- Original parent: 6e6c9567-59d2-415e-8d6e-41314a903548
- Milestone: M2 - Auth & PsyNet Integration

## 🔒 Key Constraints
- Read-only investigation — do NOT implement directly into `internal/psynet` (write proposed code and analysis in working directory)
- Must implement `ReplayDownloader` interface (`DownloadReplay(ctx, matchGUID, replayURL, destDir) (localPath, error)`)
- HTTP GET streaming to unique temporary file (`.tmp`) in `destDir`
- Minimum file size validation (>1KB), non-empty validation
- Flush and sync to disk before validation and rename
- Atomic rename (`os.Rename` with Windows retry loop)
- Cleanup on context cancellation or error
- Custom HTTP client configuration (timeout, transport, user-agent)
- Unit test design (`downloader_test.go`) utilizing `testutil.MockCDNServer`
- Output report and proposed code to `d:\code\rl-api-utils\.agents\teamwork\m2_explorer_3\handoff.md` and notify parent via `send_message`

## Current Parent
- Conversation ID: 6e6c9567-59d2-415e-8d6e-41314a903548
- Updated: 2026-09-25T03:38:00Z

## Investigation State
- **Explored paths**:
  - `ORIGINAL_REQUEST.md`, `PROJECT.md`, `DISPATCH.md`
  - `survey_miner_ballchasing_1/handoff.md`
  - `internal/testutil/mock_cdn.go`
  - `test/e2e/e2e_test.go`, `test/e2e/tier1_feature_test.go`, `test/e2e/tier2_boundary_test.go`
  - `internal/storage/jsonstore.go`
- **Key findings**:
  - Exact `ReplayDownloader` interface: `DownloadReplay(ctx context.Context, matchGUID, replayURL, destDir string) (localPath string, err error)`.
  - Windows file handle semantics: `tmpFile.Close()` MUST precede `os.Rename()` to prevent `ERROR_SHARING_VIOLATION`.
  - Windows retry loop: Linear backoff across 5 attempts withstands transient file locks from antivirus scanners and indexers.
  - Co-located staging: Writing `.tmp` file directly inside `destDir` guarantees same-filesystem volume (avoids `EXDEV` cross-device rename errors).
  - Robust deferred cleanup: Guarantees zero orphaned `.tmp` files on any error, mid-stream disconnect, or context cancellation.
  - Validation: Enforcing non-empty and minimum size (>1KB) rejects truncated streams, empty bodies, and error responses.
  - Path traversal protection: Validating and sanitizing `matchGUID` prevents arbitrary file write attacks.
  - Typed `HTTPStatusError` preserves status code for `errors.As` and error string formatting.
- **Unexplored areas**: None. Design, proposed code, and unit tests are complete and verified.

## Key Decisions Made
- Authored production-ready `proposed_downloader.go` and comprehensive `proposed_downloader_test.go`.
- Verified test suite with Go 1.24.1 (12 test cases, 100% passing, clean `go vet`).
- Authored 5-component `handoff.md`.

## Artifact Index
- `handoff.md` — Final investigation report and proposed code
- `proposed_downloader.go` — Proposed code for `internal/psynet/downloader.go`
- `proposed_downloader_test.go` — Proposed tests for `internal/psynet/downloader_test.go`
- `progress.md` — Liveness and progress tracker
- `BRIEFING.md` — Working memory and situational awareness
