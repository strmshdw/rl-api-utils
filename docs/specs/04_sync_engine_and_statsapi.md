# Sub-Spec 04: Sync Engine & Real-Time Stats API Listener

---

## 1. Match Synchronization Engine (`internal/syncer/`)

### 1.1 Responsibilities & Concurrency Control
`syncer.Engine` orchestrates three independent pipelines serialized where they mutate overlapping match records:
1. `RunCycle(ctx)` (PsyNet match history -> CDN replay download -> Ballchasing upload -> initial stats poll)
2. `PollAndSyncPendingStats(ctx)` (re-poll `SYNCED` / `DOWNLOADED` matches whose `replay_stats_status` is `pending`)
3. `RetryMatch(ctx, matchGUID)` (manual HTTP-triggered retry resetting retry counter for one match)
- **Mutex Serialization**: Guard active cycle/poll overlap via `cycleMu` (`TryLock` or mutex serialization) so concurrent manual `POST /sync`, threshold-forced syncs, and ticker cycles cannot double-download or double-upload the same `MatchGUID`.

### 1.2 Exact Cycle Workflow
1. Fetch online match history from PsyNet `MatchHistoryProvider` (single bounded RPC call).
2. Upsert new `MatchGUID` entries into `StateStore` as `PENDING` (never downgrading already `DOWNLOADED` or `SYNCED` matches!).
3. Select actionable matches:
   - `PENDING` or Retryable (`FAILED` where separate `download_retry_count < 5`), ordered newest-first.
   - If `dry_run`: log would-be downloads/uploads and emit `CycleReport` cleanly without side effects.
4. Worker pool (`max_concurrent_downloads`, default 2):
   - Always check `ctx.Err() != nil` before starting each item and before writing failure state (`3f9d1d8` — interrupted shutdowns MUST NOT mark matches `FAILED`!).
   - Download `.replay` via `ReplayDownloader`. On success set `DOWNLOADED`; notify `statsapi.Tracker.MarkDownloaded(guid)`!
   - On `404`/`410` expired CDN error, increment/cap retry state and mark `FAILED` cleanly; otherwise increment `download_retry_count`.
5. Upload `DOWNLOADED` replays (or re-upload if `ballchasing_id == ""` and `upload_retry_count < 5`) to Ballchasing:
   - Set `SYNCED`, trim `strings.TrimSpace(res.Location)` for `BallchasingURL`, set `SyncedAt = now`, populate `BallchasingID`.
   - On duplicate (`409`), mark `SYNCED` cleanly (resolving ID if present, otherwise marking `replay_stats_status = "unavailable"` if ID missing).
6. Immediate & Deferred Telemetry Hydration (`stats.go`):
   - Call `GetReplay(ctx, ballchasingID)` (using shared 2 req/sec limiter):
     - If Ballchasing `status == "ok"` or players parsed: call `Store.UpdateReplayStats` -> hydrates `match_players` and player ranks (`017db99` rule: if incoming `Players` is empty on an already-populated match, preserve existing `Players`! If `status == "ok"` with 0 players, still mark `replay_stats_status = "fetched"`).
     - Notify optional callback `onReplayStatsSynced(matchGUID, stats)` so `SessionTracker` retroactively enriches completed session matches with goals/saves/MMR deltas!
     - If Ballchasing `404` (`ErrNotFound`), set `replay_stats_status = "expired"`; if processing/`pending`, leave `pending` up to `stats_retry_count < 10` before marking `failed`.

### 1.3 Ballchasing Historical Backfill (`history_sync.go`)
- Runs on `--sync-ballchasing-history`, `POST /api/sync/ballchasing`, or `ballchasing_history_sync_interval` (default `30m`).
- Paginates `GetUploadedReplays`, skipping matches already having `replay_stats_status == "fetched"`.
- Checks `ctx.Err()` before every network call; emits `SyncProgress` snapshots feeding SSE `sync_progress` events (`syncing`, `total_found`, `processed`, `new_imported`, `errors`, `last_error`, `started_at`, `finished_at`).
- Persists match + player box scores via atomic batch operations and updates cursor state cleanly.

---

## 2. Local Rocket League `TAStatsAPI` Integration (`internal/statsapi/`)

### 2.1 Wire Listener (`listener.go`)
- Connects to Rocket League plugin (`MatchStatsExporter_TA`) over `websocket` (`ws://<addr>`) or newline-delimited `tcp`, defaulting to `127.0.0.1:49124`, `ReconnectDelay: 3s`.
- **Instant Shutdown Guarantee (`3f9d1d8`)**: Spawn connection watchdog goroutine watching `ctx.Done()`: on cancel, call `_ = conn.SetReadDeadline(time.Now())` and `_ = conn.Close()` BEFORE checking read error, guaranteeing `< 50ms` unblock on idle `ReadMessage`/`ReadBytes('\n')`.
- **Event Envelope Tolerance**: `EventData.UnmarshalJSON` supports both raw JSON objects and double-encoded JSON strings (`"{\"MatchGuid\":...}"`).
- Supported Events:
  - `TAGame.GameEvent_Soccar_TA.InitMatch` / `InitTeams` / `UpdateState` / `MatchEnded` -> dispatch to `Tracker` + optional `GameStateEventHandler` (`playertrack.Tracker`):
    - When `Data.Game != nil`: call `OnUpdateGameState(ctx, guid, playlist, players, game)`.
    - Otherwise call `OnUpdateState(ctx, guid, playlist, players)`.
    - On `MatchEnded`: call `OnMatchEnded(ctx, guid, winnerTeamNum)`.
- When transport disconnects after having been connected: invoke `onDisconnected()` callback (triggering `SessionTracker.OnGameDisconnected()`).

### 2.2 Pending Replay FIFO Tracker & Toast Notifier (`tracker.go`, `notifier.go`)
- Tracks a rolling FIFO queue bounded to `MaxHostedReplays = 20` online matches observed live that are not yet `DOWNLOADED`/`SYNCED` in `StateStore`.
- Triggers notification callback / Windows Toast when `PendingCount >= TriggerThreshold` (default 15), respecting configurable cooldown so duplicate toasts don't spam during a single match.
- When `ForceSyncOnTrigger` is true and threshold crossed, invokes injected `syncTriggerFn(ctx)` asynchronously without blocking `Listener` read loop!
- `WindowsToastNotifier`: Static PowerShell script reading XML payload from `$env:RL_SYNC_TOAST_XML` with strict XML entity escaping (`& < > " '`) and 5s `exec.CommandContext` timeout; gracefully logs on non-Windows hosts.
