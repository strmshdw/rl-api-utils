# DISPATCH: Milestone M2 Implementation Worker

**Agent**: `m2_worker_1`  
**Role**: Milestone M2 Implementation Worker  
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m2_worker_1`  
**Parent**: `orchestrator_9`  
**Timestamp**: 2026-10-17T02:00:00Z  

## Mission
Implement Milestone M2: Syncer Post-Upload Telemetry Polling, Multi-Identity Local Player Resolution, PsyNet Replay Matchup Recording, Bounded Retry Queue with Exponential Backoff, and Replay Parser RFC3339 Date Normalization / Overtime / Demolition Extraction (SPEC-01, SPEC-04, SPEC-06).

## Mandatory Reference Documents
Read these files thoroughly before writing code:
1. `d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md` (specifically timestamp 2026-10-16T23:46:37Z)
2. `d:\code\rl-api-utils\.agents\teamwork\orchestrator_9\PROJECT.md`
3. `d:\code\rl-api-utils\.agents\teamwork\m2_exp_syncer_1\handoff.md` and `syncer_analysis.md`
4. `d:\code\rl-api-utils\.agents\teamwork\m2_exp_retry_1\handoff.md` and `retry_analysis.md`
5. `d:\code\rl-api-utils\.agents\teamwork\m2_exp_parser_1\handoff.md` and `parser_analysis.md`

## Exclusive Write Ownership Files
You have exclusive write ownership of:
- `internal/syncer/*` (`syncer.go`, `interfaces.go`, `ballchasing_history.go`, `syncer_test.go`, etc.)
- `internal/replay/*` (`parser.go`, `replay.go`, `parser_test.go`, etc.)
- `internal/storage/sqlite.go` (only for `ListPendingDownloads`, `ListPendingUploads`, `ListPendingStatsSync`, `MarkDownloaded`, `ResetMatchRetry`)
- `internal/storage/jsonstore.go` (only for `ListPendingDownloads`, `ListPendingUploads`, `ListPendingStatsSync`, `MarkDownloaded`, `ResetMatchRetry`, `CalculateBackoff`)
- `internal/storage/store.go` (only for interface methods / helper definitions if needed)
- `internal/storage/m2_retry_test.go` (new unit tests for retry queue and backoff)

## MANDATORY INTEGRITY WARNING
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

## Detailed Tasks to Implement

### 1. Replay Parser Fixes (`internal/replay`)
- In `internal/replay/parser.go`:
  - Add `Demos int json:"demos,omitempty"` to `ReplayPlayerStats`.
  - Implement `normalizeReplayDate(raw string) string` supporting layouts (`"2006-01-02 15-04-05"`, `"2006-01-02:15-04"`, `"2006-01-02 15:04:05"`, `"2006-01-02T15:04:05"`, `time.RFC3339`, `time.RFC3339Nano`, `"2006-01-02"`), converting to UTC RFC3339.
  - Fall back to `MatchStartEpoch` when `Date` is empty or missing.
  - Implement `extractOvertime(meta *ReplayMetadata, props map[string]*propertyValue) bool`: check explicit properties (`bOvertime`, `bOverTime`, `Overtime`, `overtime`, `OvertimeSeconds > 0`), and fallback heuristic (`TotalSeconds >= 315.0 && !Forfeit && abs(Team0Score - Team1Score) == 1`).
  - In `populateMetadata()`: assign `meta.Overtime = extractOvertime(...)`, normalize `meta.Date = normalizeReplayDate(...)`, and parse player `Demos` from `sp["Demos"]` / `sp["Demolitions"]`.
- In `internal/replay/parser_test.go`:
  - Add comprehensive unit tests verifying date normalization, overtime extraction, demolition extraction, and test all 20 real replays in `replays/*.replay`.

### 2. Storage Retry Queue & Backoff (`internal/storage`)
- In `internal/storage/sqlite.go`:
  - Update `ListPendingDownloads`: query `(download_status = 'PENDING' OR (download_status = 'FAILED' AND retry_count < ? AND (? - updated_at) >= MIN(?, ? * (1 << (CASE WHEN retry_count > 0 THEN retry_count - 1 ELSE 0 END)))))`. Default `maxRetries = 3`, `baseBackoff = 30s`, `maxBackoff = 15m`.
  - Update `ListPendingUploads`: query `download_status = 'DOWNLOADED'` AND `(upload_status = 'PENDING' OR (upload_status = 'FAILED' AND retry_count < ? AND (? - updated_at) >= MIN(?, ? * (1 << (CASE WHEN retry_count > 0 THEN retry_count - 1 ELSE 0 END)))))`.
  - Update `ListPendingStatsSync`: include `(upload_status = 'UPLOADED' OR upload_status = 'DUPLICATE')` AND `(stats_sync_status = 'PENDING' OR (stats_sync_status = 'FAILED' AND stats_retry_count < ? AND (? - updated_at) >= MIN(?, ? * (1 << (CASE WHEN stats_retry_count > 0 THEN stats_retry_count - 1 ELSE 0 END)))))`.
  - In `MarkDownloaded`: reset `retry_count = 0` so download retries don't starve upload retries.
  - In `ResetMatchRetry`: also reset `download_status = CASE WHEN download_status = 'FAILED' THEN 'PENDING' ELSE download_status END`.
- In `internal/storage/jsonstore.go`:
  - Implement `CalculateBackoff(attempt int, baseBackoff, maxBackoff time.Duration) time.Duration`.
  - Update `ListPendingDownloads`, `ListPendingUploads`, and `ListPendingStatsSync` with identical bounded retry and backoff logic.
  - Update `MarkDownloaded` to reset `RetryCount = 0`.
  - Update `ResetMatchRetry` to reset `DownloadStatus = DownloadPending` if `DownloadFailed`.
- Add unit tests in `internal/storage/m2_retry_test.go` covering backoff calculation and retry filtering.

### 3. Syncer Post-Upload Polling, Multi-Identity Resolution & Matchups (`internal/syncer`)
- In `internal/syncer/interfaces.go`:
  - Add `ReplayStatsFetcher` interface: `GetReplay(ctx context.Context, id string) (*ballchasing.ReplayItem, error)`.
  - Ensure `StateStore` interface includes `ListPendingStatsSync`, `MarkStatsSyncing`, `MarkStatsSynced`, `MarkStatsSyncFailed`, `RecordMatchResults`, `SaveMatchPlayers`, and `GetAuthState`.
- In `internal/syncer/syncer.go`:
  - Implement 6-tier local player resolution (`ResolveLocalPlayer`) with strict bot immunity (`!p.IsBot`).
  - Wire Step 6 `PollAndSyncPendingStats(ctx context.Context)` into `Syncer.RunCycle`:
    - Poll matches returned by `store.ListPendingStatsSync(ctx)`.
    - Fetch replay via `ReplayStatsFetcher.GetReplay(ctx, m.BallchasingID)`.
    - If status == `"ok"`:
      - Call `store.MarkStatsSynced(ctx, m.MatchGUID, m.BallchasingID)`.
      - Upsert player rank snapshots if present.
      - Save `match_players` and sync replay stats.
      - Call `store.RecordMatchResults` passing actual match time parsed via `storage.ParseReplayDate`.
    - If status == `"pending"`: leave as pending for subsequent cycle.
    - If status == `"failed"`: call `store.MarkStatsSyncFailed(ctx, m.MatchGUID, errStr)`.
  - Update `Syncer.SyncDownloadedReplayStats`:
    - Propagate `Demos: p.Demos` to `storage.ReplayPlayerStats` and `Overtime: meta.Overtime` to `storage.ReplayMatchStats`.
    - Resolve local player & local team.
    - Call `store.RecordMatchResults` with `matchTime` from `storage.ParseReplayDate(meta.Date)`.
    - Call `store.SaveMatchPlayers`.
    - Gracefully ignore `storage.ErrMatchAlreadyProcessed`.
- Add unit tests in `internal/syncer/syncer_test.go` verifying the polling loop, retry logic, and local player resolution.

## Verification
Worker must execute:
- `go vet ./...`
- `go test -v -race ./internal/replay/...`
- `go test -v -race ./internal/storage/...`
- `go test -v -race ./internal/syncer/...`
- `go test -race ./...`

Write complete handoff report to `d:\code\rl-api-utils\.agents\teamwork\m2_worker_1\handoff.md` and notify `orchestrator_9`.
