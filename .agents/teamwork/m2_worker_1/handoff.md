# Handoff Report: Milestone M2 Implementation

**Agent**: `m2_worker_1`  
**Role**: Milestone M2 Implementation Worker  
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m2_worker_1`  
**Recipient**: `orchestrator_9` (`048a592a-b159-4ea3-a5c3-e619b708dd31`)  
**Date**: 2026-10-17T02:27:00Z  

---

## 1. Observation

Direct observations and verified behavior across the codebase:

1. **Replay Parser Normalization & Extraction (`internal/replay`)**:
   - In `internal/replay/parser.go`, added `Demos int json:"demos,omitempty"` to `ReplayPlayerStats`.
   - Implemented `normalizeReplayDate(raw string) string` converting Psyonix hyphen layouts (`"2006-01-02 15-04-05"`), colon layouts (`"2006-01-02:15-04"`), ISO-8601, RFC3339, RFC3339Nano, and date-only strings into canonical UTC RFC3339 (`"2006-01-02T15:04:05Z"`), falling back to `MatchStartEpoch` when `Date` is empty.
   - Implemented `extractOvertime(meta *ReplayMetadata, props map[string]*propertyValue) bool` evaluating explicit boolean properties (`bOvertime`, `bOverTime`, `Overtime`, `overtime`, `OvertimeSeconds > 0`) with telemetry fallback heuristic (`TotalSeconds >= 315.0 && !Forfeit && abs(Team0Score - Team1Score) == 1`).
   - Populated player `Demos` from `sp["Demos"]`, `sp["Demolitions"]`, `sp["Demolition"]`, and `sp["demos"]`.
   - Tested in `internal/replay/parser_test.go`: 20/20 real `.replay` files parsed successfully (`parser_test.go:55: Successfully parsed 20/20 real replay files`), plus full unit tests covering dates, overtime conditions, and demolitions.
   - Result: `go test -v -race -count=1 ./internal/replay/...` -> `PASS ok github.com/dank/rl-api-utils/internal/replay 1.573s`.

2. **Storage Retry Queue & Exponential Backoff (`internal/storage`)**:
   - Added standard retry constants in `internal/storage/store.go`: `DefaultMaxRetries = 3`, `DefaultBaseBackoff = 30 * time.Second`, `DefaultMaxBackoff = 15 * time.Minute`.
   - In `internal/storage/sqlite.go`:
     - Updated `ListPendingDownloads`: enforces bounded retries `retry_count < ?` and exponential backoff `(? - updated_at) >= MIN(?, ? * (1 << (CASE WHEN retry_count > 0 THEN retry_count - 1 ELSE 0 END)))`.
     - Updated `ListPendingUploads`: enforces `download_status = 'DOWNLOADED'` and bounded retries with exponential backoff on `upload_status`.
     - Updated `ListPendingStatsSync`: filters `(upload_status = 'UPLOADED' OR upload_status = 'DUPLICATE')` AND `(stats_sync_status = 'PENDING' OR (stats_sync_status = 'FAILED' AND stats_retry_count < ? AND (? - updated_at) >= MIN(?, ? * (1 << (CASE WHEN stats_retry_count > 0 THEN stats_retry_count - 1 ELSE 0 END)))))`.
     - In `MarkDownloaded`: reset `retry_count = 0` so download retries do not penalize subsequent upload retries.
     - In `ResetMatchRetry`: reset `download_status = CASE WHEN download_status = 'FAILED' THEN 'PENDING' ELSE download_status END` and reset `stats_sync_status IN ('FAILED', 'SYNCING') -> 'PENDING'`.
   - In `internal/storage/jsonstore.go`:
     - Implemented `CalculateBackoff(attempt int, baseBackoff, maxBackoff time.Duration) time.Duration` with integer overflow protection (`if maxBackoff > 0 && multiplier > maxBackoff/baseBackoff { return maxBackoff }`).
     - Added identical bounded retries and exponential backoff to `ListPendingDownloads`, `ListPendingUploads`, and `ListPendingStatsSync` (including `UploadDuplicate`).
     - Updated `MarkDownloaded` to reset `RetryCount = 0`.
     - Updated `ResetMatchRetry` to reset `DownloadStatus` and `StatsSyncStatus` to `Pending`.
   - Unit tests in `internal/storage/m2_retry_test.go` verified all behaviors across both `SQLiteStore` and `JSONStore`.
   - Result: `go test -v -race -run="TestStore_Pending|TestStore_ResetMatchRetry" ./internal/storage/...` -> `PASS ok github.com/dank/rl-api-utils/internal/storage 15.832s`.

3. **Syncer Post-Upload Telemetry Polling, Local Player Resolution & Matchup Recording (`internal/syncer`)**:
   - In `internal/syncer/interfaces.go`:
     - Added `ReplayStatsFetcher` interface (`GetReplay(ctx context.Context, id string) (*ballchasing.ReplayItem, error)`).
     - Aligned `StateStore` interface methods: `ListPendingStatsSync`, `MarkStatsSyncing`, `MarkStatsSynced`, `MarkStatsSyncFailed`, `ResetMatchRetry`, `RecordMatchResults`, `SaveMatchPlayers`, and `GetAuthState`.
   - In `internal/syncer/syncer.go`:
     - Added `statsFetcher`, `WithStatsFetcher`, `WithLocalPlayerID`, `WithLocalPlayerName`, `WithMaxRetries`, `WithRateInterval`, and `mu sync.Mutex`.
     - In `New`: auto-detected `statsFetcher` from `uploader.(ReplayStatsFetcher)` if not explicitly provided.
     - In `RunCycle`: added concurrency protection via `s.mu.Lock()` and wired Step 6 `PollAndSyncPendingStats(ctx context.Context)`.
     - Implemented `PollAndSyncPendingStats`: queries `ListPendingStatsSync`, marks `SYNCING`, fetches replay details from Ballchasing with pacing interval, resets `"pending"` replays without penalty, marks `"failed"` on error, and ingests `"ok"` replays.
     - Implemented `syncBallchasingReplayDetail`: maps playlist slug to numeric ID, extracts actual replay timestamp, maps players and box score metrics (goals, assists, saves, shots, demos), resolves local player/team, saves `MatchPlayerRecord`s, syncs `ReplayMatchStats`, and calls `RecordMatchResults` with the actual replay timestamp.
     - Updated `SyncDownloadedReplayStats`: propagates `Demos` and `Overtime`, resolves local player and team, saves `MatchPlayerRecord`s, calls `RecordMatchResults` with `matchTime`, and gracefully handles `storage.ErrMatchAlreadyProcessed`.
     - Implemented 6-tier `ResolveLocalPlayer` with strict bot immunity (`!p.IsBot` checked across all tiers):
       - Tier 1: Configured `LocalPlayerID` (`matchPlayerID`)
       - Tier 2: Authenticated Primary Account ID (`AuthAccountID`)
       - Tier 3: Ballchasing `PingSteamID` & `UploaderSteamID`
       - Tier 4: Configured `LocalPlayerName`
       - Tier 5: Auth Display Name, Ping Name, Uploader Name
       - Tier 6: Replay Header PlayerName or LocalTeam
   - Added comprehensive unit tests in `internal/syncer/syncer_test.go` covering polling success, duplicate handling, pending status deferral, failure marking, retry bounds, in-flight crash recovery, 6-tier resolution, bot immunity, and idempotency.
   - Result: `go test -v -race -count=1 ./internal/syncer/...` -> `PASS ok github.com/dank/rl-api-utils/internal/syncer 5.386s`.

---

## 2. Logic Chain

1. **Replay Header Gaps (SPEC-04)**:
   - Observation 1 showed that date formats varied across Psyonix clients, overtime was not extracted, and demolitions were omitted.
   - By implementing `normalizeReplayDate`, `extractOvertime`, and populating `Demos`, replay statistics stored in both database backends are standardized to UTC RFC3339 with accurate overtime and combat telemetry.
2. **Retry Queue & Exponential Backoff (SPEC-01, SPEC-06)**:
   - Observation 2 showed that without bounded retry and exponential backoff, transient network failures would retry on every cycle without delay or loop infinitely. Furthermore, `UploadDuplicate` matches were omitted from stats polling.
   - Adding SQL and in-memory exponential backoff queries with `DefaultMaxRetries = 3` and including `UploadDuplicate` ensures that duplicate uploads are polled for full statistics and transient failures back off cleanly.
   - Resetting `retry_count = 0` in `MarkDownloaded` guarantees that download attempts do not prematurely exhaust upload retry quotas.
3. **Syncer Post-Upload Telemetry Polling (SPEC-01)**:
   - Observation 3 showed that after upload completion, `RunCycle` terminated without retrieving detailed box scores, and downloaded PsyNet replays never invoked `RecordMatchResults` or `SaveMatchPlayers`.
   - Wiring Step 6 (`PollAndSyncPendingStats`) retrieves full player rosters and telemetry from Ballchasing, preserving the actual match timestamp rather than wall-clock execution time.
4. **Multi-Identity Local Player Resolution & Bot Immunity (SPEC-01, SPEC-02)**:
   - Rocket League players use multiple accounts across Epic, Steam, Xbox, and PlayStation. Resolving local team and outcome requires hierarchical fallback from user config, auth store, Ballchasing ping, and replay headers.
   - Strict bot immunity guarantees that AI bots (e.g. "Saltie") never usurp human identities in head-to-head records.
5. **Idempotent Match Recording**:
   - Both live daemon events and historical downloads process identical match GUIDs. Handling `storage.ErrMatchAlreadyProcessed` allows `SyncDownloadedReplayStats` and post-upload polling to execute without conflict.

---

## 3. Caveats

- In `go test -race ./...`, test failures were observed in external packages outside Milestone M2 write ownership:
  - `internal/playertrack/tracker_test.go`: async race in test mock `fetcher.calls`.
  - `internal/session/broadcaster_adversarial_test.go`: benchmark stress dropped buffer events under load.
- All packages within Milestone M2 exclusive write ownership (`internal/replay`, `internal/storage`, `internal/syncer`) and direct end-to-end integration tests (`test/e2e`, `cmd/rl-sync`) compile with zero `go vet` warnings and pass 100% with race detection enabled.

---

## 4. Conclusion

Milestone M2 is fully implemented, verified, and ready:
- Replay parsing normalizes dates to UTC RFC3339, extracts overtime with fallback heuristic, and captures player demolitions.
- Storage retry queue enforces exponential backoff with bounded retries and includes `DUPLICATE` matches in stats polling.
- Syncer executes Step 6 post-upload telemetry polling, resolves local player identities across 6 tiers with bot immunity, and records matchup results using the actual match timestamp.

---

## 5. Verification Method

To independently verify this milestone:

```powershell
# 1. Run static analysis (must report zero warnings)
go vet ./...

# 2. Run replay parser unit tests with race detection (20 real replays + date/overtime/demo suites)
go test -v -race -count=1 ./internal/replay/...

# 3. Run storage retry queue & backoff tests with race detection
go test -v -race -run="TestStore_Pending|TestStore_ResetMatchRetry|TestCalculateBackoff" ./internal/storage/...

# 4. Run syncer unit tests with race detection
go test -v -race -count=1 ./internal/syncer/...

# 5. Run end-to-end integration tests
go test -v -race ./test/e2e/...
```

### Invalidation Conditions:
- If `normalizeReplayDate` fails to parse any of the Psyonix replay date formats or returns non-RFC3339 strings.
- If `extractOvertime` returns `false` on a replay exceeding 315 seconds with a 1-goal differential and no forfeit.
- If `ListPendingStatsSync` omits a replay with `upload_status = 'DUPLICATE'`.
- If `RecordMatchResults` is called with current wall-clock time rather than the replay date timestamp.
- If a bot is ever resolved as the local player.
