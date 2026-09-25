# Dispatch: m4_explorer_1

**Milestone**: M4 - Syncer, Daemon Engine & CLI
**Role**: Syncer Domain Orchestrator Explorer (internal/syncer)

## Scope
Investigate and design `internal/syncer`:
- Dependencies: `internal/storage`, `internal/psynet`, `internal/ballchasing`, `internal/config`.
- Interface definitions (`interfaces.go`):
  - Interface contracts for `StateStore`, `MatchHistoryProvider`, `ReplayDownloader`, `ReplayUploader`.
- Core Sync Pipeline (`syncer.go`):
  1. `store.RecoverInFlight(ctx)` (resets orphaned `DOWNLOADING` and `UPLOADING` records to `PENDING`).
  2. Query `historyProvider.GetRecentMatches(ctx)` -> slice of `DiscoveredMatch`.
  3. Upsert discovered matches into `store.UpsertDiscoveredMatches(ctx, ...)`:
     - Empty `ReplayURL` -> `DownloadStatus: SKIPPED`.
     - Non-empty `ReplayURL` -> `DownloadStatus: PENDING`.
  4. Query `store.ListPendingDownloads(ctx)`:
     - If dry-run: log and count, do not download.
     - Mark `DOWNLOADING`.
     - Download replay via `downloader.DownloadReplay`.
     - On success: mark `DOWNLOADED`.
     - On failure: mark `FAILED`.
  5. Query `store.ListPendingUploads(ctx)`:
     - If dry-run: log and count, do not upload.
     - Mark `UPLOADING`.
     - Upload replay via `uploader.UploadReplay`.
     - If duplicate: mark `DUPLICATE` (err is nil).
     - Else: mark `UPLOADED`.
     - On failure: mark `FAILED`.
  6. Return `SyncStats` (Discovered, Downloaded, Uploaded, Duplicates, Failures).
- Unit test strategy (`syncer_test.go`) covering multi-cycle progression, delayed replay URLs, duplicate handling, dry-run, crash recovery, and context cancellation.

Write your report and proposed code to `d:\code\rl-api-utils\.agents\teamwork\m4_explorer_1\handoff.md`.

## 2026-09-25T04:18:00Z
You are m4_explorer_1.
Your working directory is: d:\code\rl-api-utils\.agents\teamwork\m4_explorer_1
Read d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md, d:\code\rl-api-utils\PROJECT.md, and d:\code\rl-api-utils\.agents\teamwork\m4_explorer_1\DISPATCH.md.

Explore the sync orchestration subsystem for Milestone 4 (internal/syncer):
- Interfaces (interfaces.go): StateStore, MatchHistoryProvider, ReplayDownloader, ReplayUploader, and supporting types (DiscoveredMatch, UploadResult, SyncStats).
- Core Sync Pipeline (syncer.go):
  1. store.RecoverInFlight(ctx) (resets orphaned DOWNLOADING / UPLOADING records).
  2. Query historyProvider.GetRecentMatches(ctx) -> slice of DiscoveredMatch.
  3. Upsert discovered matches into store.UpsertDiscoveredMatches(ctx, ...):
     - Empty ReplayURL -> DownloadStatus: SKIPPED.
     - Non-empty ReplayURL -> DownloadStatus: PENDING.
  4. Query store.ListPendingDownloads(ctx):
     - If dry-run: log and count, do not download.
     - Mark DOWNLOADING -> downloader.DownloadReplay -> Mark DOWNLOADED / FAILED.
  5. Query store.ListPendingUploads(ctx):
     - If dry-run: log and count, do not upload.
     - Mark UPLOADING -> uploader.UploadReplay -> Mark UPLOADED / DUPLICATE / FAILED.
  6. Return SyncStats (Discovered, Downloaded, Uploaded, Duplicates, Failures).
- Unit test strategy (syncer_test.go) with mock implementations for all interfaces.
Write your detailed report and proposed Go code to d:\code\rl-api-utils\.agents\teamwork\m4_explorer_1\handoff.md and notify parent via send_message.

