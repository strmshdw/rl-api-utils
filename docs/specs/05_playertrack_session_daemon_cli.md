# Sub-Spec 05: Player Tracking, Session Engine, Daemon & CLI

---

## 1. Player Tracking & Rank Intelligence (`internal/playertrack/`)

### 1.1 Rank Client & Cache Architecture
- `SkillFetcher` wraps `rlapi.PsyNet` via `AuthProviderSupplier` (polling secondary account). Returns `[]PlayerWithSkills` formatted into `PlayerRanksSnapshot` (string key = playlist ID) with canonical `RankName` and `ShortRank`.
- **Safe Caching (`cache.go`)**:
  - Rank cache bounded at `maxRankCacheEntries = 1000` with `rank_cache_ttl` (default `15m`) + oldest-entry eviction when full.
  - Per-player failure backoff cache bounded at `maxFailBackoffEntries = 500` with `60s` TTL (`defaultBackoffTTL`) so invalid/private profiles don't hammer PsyNet on every game tick.
- `NoOpRankClient`: returned cleanly when `auto_fetch_ranks == false` or `polling_auth.enabled == false`.

### 1.2 Live Match State Machine (`tracker.go`)
- `OnUpdateState` / `OnUpdateGameState`:
  - Upserts observed human players into `players` table (preserving notes & existing rank data when new ranks haven't arrived yet).
  - Resolves local player priority (`3744274` & `Error 67` rules):
    1. Explicit `local_player_id` config override.
    2. Authenticated primary user (`auth_state` rows where `LOWER(provider) NOT LIKE '%_polling'`) matched by platform+accountID or Epic/Steam cross-ID.
    3. Most frequent `is_local = 1` ID in historical `match_players`.
    4. `local_player_name` display name match if present in lobby.
  - Classifies each player vs local user's `TeamNum`: `self`, `teammate`, `opponent`, `bot` (`Unknown|0|0`), or `spectator` (`TeamNum == 255` or `-1`).
  - Tracks mid-game disconnects (`IsDisconnected = true` if seen earlier in the match and absent in subsequent ticks; cleared if reconnected).
  - Returns defensive deep clones (`MatchSnapshot.DeepClone()`) to callers so concurrent HTTP/SSE readers never race or mutate internal maps.
- `OnMatchEnded`:
  - Atomically records H2H `player_matchups` (`with` / `against`, `win` / `loss`) via `processed_match_outcomes` idempotency guard.
  - Records post-match MMR change into `mmr_history` when rank snapshot refreshes after match end.
  - Emits match/session events to registered listener (`session.SessionTracker`).

---

## 2. Session Engine & SSE Broadcaster (`internal/session/`)

- `EventBroadcaster`: fan-out hub with per-subscriber buffered channels (`defaultSubscriberBufferSize = 64`), slow-client drop (never blocks emitters), and explicit `Close()` terminating all active SSE loops cleanly on daemon shutdown (`3f9d1d8`).
- `SessionTracker`:
  - Tracks current session `session_id` (UUID), `started_at`, `last_match_at`, per-playlist `InitialMMR`, `CurrentMMR`, `MMRDelta`, `MatchesPlayed`, `Wins`, `Losses`, `WinRate`, ordered `Matches` (`SessionMatchDetail`), and active live match clock (`SecondsRemaining`, `IsOvertime`).
  - **Continuation Window**: `EffectiveSessionContinueWindow()` (default `2h`, `0s` disables). When a match starts/ends after `now - last_match_at > continueWindow`, auto-rotates & archives previous session to SQLite `sessions`.
  - **Game Disconnect**: `OnGameDisconnected()` clears incomplete active match and archives non-empty sessions so reopening Rocket League within `continueWindow` surfaces `continuable_session`.
  - **Manual Control**: `ContinueSession()` merges archived continuable session chronologically, recalculating baselines/deltas; `DismissContinuableSession(id)` persists `dismissed=1`; `Reset()` archives and opens fresh epoch.
  - **Telemetry Enrichment**: `AttachReplayTelemetry(guid, stats)` updates matching `SessionMatchDetail` with goals/assists/saves/shots/demos/MVP and broadcasts `session_update`.

---

## 3. HTTP Daemon, Security & 30 Exact Routes (`internal/daemon/`)

### 3.1 Server Config & Security Guards
- `http.Server{Addr, Handler, ReadHeaderTimeout: 5s, ReadTimeout: 10s, WriteTimeout: 0, IdleTimeout: 60s, BaseContext: ..., ConnState: ...}` bound as a single unified HTTP server via `net.Listen` + `httpSrv.Serve(ln)` (eliminating port-probe TOCTOU) — `WriteTimeout: 0` avoids cutting persistent SSE streams. `httpPort()` resolves `Web.Port` when explicitly configured away from default `49125`, falling back to `StatsAPI.HTTPTriggerPort` and then `49125`.
- **Origin CSRF Check** (`middleware.go`): State-mutating requests (`POST`, `PATCH`, `PUT`, `DELETE`) MUST originate from loopback (`localhost`, `127.0.0.1`, `[::1]`) when an `Origin` header is present; empty `Origin` (non-browser CLI `curl`/tests) allowed. `Access-Control-Allow-Methods` advertises `GET, POST, PATCH, PUT, DELETE, OPTIONS`.
- **Path & Body Guards**: Limit JSON request bodies to `1 MiB`; clean & validate `{id}`/`{guid}` against `..`/`/` traversal; **marshal JSON to bytes before calling `w.WriteHeader(status)`** (`fb1fc22`); suppress `IsDisconnectError(err)` writes to `Debug`.
- **Shutdown**: Cancels root context, invokes `RegisterOnShutdown` (`broadcaster.Close()` + `50ms` read-deadline release for idle `StateNew` TCP preconnect sockets), closes StatsAPI listener and `statsTracker.Close()`, awaits WaitGroup, and calls `httpSrv.Shutdown(2s)` (`3f9d1d8`).

### 3.2 Complete 30-Route Contract (Preserving Every Alias)
| Method | Route(s) | Purpose |
| :--- | :--- | :--- |
| `GET` | `/healthz` | Liveness `{"status":"running"}` |
| `GET` | `/status` | `DaemonStatusResponse` (syncing, stats_api, auth, player_tracking, db/replay counts via fast `COUNT(*)`) |
| `POST` | `/sync` | Trigger manual sync cycle (`200` / `202` or error) |
| `GET` | `/api/session`, `/api/session/state` | `SessionResponse` (current + `continuable_session`) |
| `POST` | `/api/session/reset`, `/api/session/continue`, `/api/session/dismiss` | Session lifecycle mutations + broadcast |
| `GET` | `/api/sessions` | Paginated archived session summaries |
| `GET` | `/api/sessions/{id...}` | Single historical session detail |
| `GET` | `/api/current-match`, `/current-match` | Active `CurrentMatchResponse` (`active_match: false` when idle) |
| `GET` | `/api/players` | Paginated `PlayerSearchResponse` (`q`, `sort`, `limit`, `offset`, `playlist`, `date_range`, `from`, `to`, plus unconditional `spotlights`) |
| `GET` | `/players` | **Bare `[]PlayerSummary` JSON array** (legacy compat!) |
| `GET` | `/api/players/{id}/matches`, `/players/{id}/matches` | Player's match history with batched stats & `split_stats` |
| `GET` | `/api/players/{id...}`, `/players/{id...}` | `PlayerDetailResponse` (`player`, `matchups`, `matchup`, `recent_matches`, `split_stats`) |
| `PATCH` | `/api/players/{id...}`, `/players/{id...}` | Update player `notes` |
| `GET` | `/api/matches`, `/api/historical-matches` | Filtered `MatchesResponse` (`limit`, `offset`, `status`, `playlist`, `search`, `q`, `date_range`, `from`, `to`) |
| `POST` | `/api/matches/prune` | Prune matches older than `days` / by `status` |
| `POST` | `/api/matches/{guid}/retry` | Reset retry counter & trigger/mark retry |
| `GET` | `/api/matches/{guid...}` | Full `HistoricalMatchRecord` (`MatchRecord` including `local_file_path` + enriched `stats` & local-relative `result`; `400` on invalid GUID, `404` on not found, `500` on store error) |
| `GET` | `/api/mmr-history` | `MMRHistoryResponse` (`player_id`, `playlist_id`, `limit`) |
| `POST` | `/api/sync/ballchasing`, `/api/sync/now` | Trigger historical Ballchasing backfill or immediate sync |
| `GET` | `/api/sync/ballchasing/status` | `BallchasingSyncStatus` |
| `GET`, `POST`, `PATCH` | `/api/sync/config` | Read or live-update sync/player_tracking runtime settings |
| `GET` | `/api/events` | SSE stream (initial `session_update` + optional `match_update` snapshots, followed by `match_update`, `match_ended`, `clock_updated`, `session_update`, `sync_progress` + `: keepalive` every 15s) |
| `GET` | `/` | `web.DistHandler()` SPA static assets (`Cache-Control: immutable` on `/assets/*`, `no-cache` on `index.html`) |

---

## 4. CLI Entrypoint (`cmd/rl-sync/main.go`)

Parses exact 22 config flags + `-version`/`-v` + `-help`/`-h`, validates credential collision when polling auth enabled, runs single cycle (`-once`), history backfill (`-sync-ballchasing-history`), or full daemon until `SIGINT`/`SIGTERM`, returning exit code `0` or `1`.
