# Dispatch: m2_explorer_3

**Milestone**: M2 - Auth & PsyNet Integration
**Role**: Replay Downloader Explorer (internal/psynet)
**Scope**:
Investigate and design `internal/psynet/downloader.go` implementing `syncer.ReplayDownloader`:
- Interface contract from PROJECT.md:
  ```go
  type ReplayDownloader interface {
      DownloadReplay(ctx context.Context, matchGUID, replayURL, destDir string) (localPath string, err error)
  }
  ```
- Implementation requirements:
  - HTTP GET streaming download of signed `ReplayURL`
  - Atomic download pattern: stream to unique temporary file `.tmp` in `destDir` (e.g. `destDir/<matchGUID>.replay.tmp`)
  - Flush and sync to disk
  - File validation: check minimum file size (>1KB) and non-empty content
  - Atomic rename (`os.Rename` with Windows retry loop) to final path `destDir/<matchGUID>.replay`
  - Clean up temporary files on error or context cancellation
  - Custom HTTP client with configurable timeout and user-agent
- Unit test strategy (`downloader_test.go`) using `testutil.NewMockCDNServer` testing successful downloads, corrupt/truncated files, 404/500 errors, context cancellation, and temp file cleanup.

Output report: `d:\code\rl-api-utils\.agents\teamwork\m2_explorer_3\handoff.md`

## 2026-09-25T03:37:53Z
You are m2_explorer_3.
Working directory: d:\code\rl-api-utils\.agents\teamwork\m2_explorer_3
Read d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md, d:\code\rl-api-utils\PROJECT.md, and d:\code\rl-api-utils\.agents\teamwork\m2_explorer_3\DISPATCH.md.
Also review d:\code\rl-api-utils\.agents\teamwork\survey_miner_ballchasing_1\handoff.md and d:\code\rl-api-utils\internal\testutil\mock_cdn.go.

Explore the Replay Downloader subsystem for Milestone 2 (internal/psynet/downloader.go):
- Interface: ReplayDownloader (DownloadReplay(ctx, matchGUID, replayURL, destDir) (localPath, error))
- HTTP GET streaming to temporary file (.tmp) in destDir
- Minimum file size validation (>1KB), non-empty validation
- Atomic rename (os.Rename with Windows retry loop)
- Cleanup on context cancellation or error
- Custom HTTP client configuration
- Unit test design (downloader_test.go) utilizing testutil.MockCDNServer
Write your report and proposed code to d:\code\rl-api-utils\.agents\teamwork\m2_explorer_3\handoff.md and notify parent via send_message.
