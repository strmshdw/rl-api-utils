# Sub-Spec 02: Configuration & SQLite Storage Architecture

---

## 1. Configuration Resolution & Precedence

Load order (lowest to highest priority):
1. Built-in defaults (`DefaultConfig()`)
2. Auto-discovered or `-config`/`-c` / `RL_SYNC_CONFIG` file (`.yaml`, `.yml`, or `.json` — search CWD `config.yaml`, `config.yml`, `config.json` unless explicit `-config` fails fast if missing)
3. Environment variables (`RL_SYNC_*` — 58 variable checks across all sections)
4. CLI flags (22 config flags + `-version`/`-v`/`-help`/`-h`)

### 1.1 Exact Sections & Struct Layout
- `auth` (`AuthConfig`): `provider` (`"epic"` | `"steam"`), `epic` (`refresh_token`, `auth_code`, `account_id`, `display_name`), `steam` (`username`, `password`, `session_ticket`, `steam_id_64`, `account_name`, `login_key`).
- `polling_auth` (`PollingAuthConfig`): `enabled` (`false`), `provider` (`"epic"`), `epic` (`EpicConfig`), `steam` (`SteamConfig`).
- `player_tracking` (`PlayerTrackingConfig`): `enabled` (`true`), `local_player_id`, `local_player_name`, `auto_fetch_ranks` (`true`), `session_continue_window` (optional Duration override).
- `session` (`SessionConfig`): `continue_window` (`2h`, `0s` disables continuation).
- `ballchasing` (`BallchasingConfig`): `api_key`, `visibility` (`"private"` | `"unlisted"` | `"public"`), `base_url` (`"https://ballchasing.com/api"`), `timeout` (`60s`), `max_retries` (`3`).
- `sync` (`SyncConfig`): `poll_interval` (`5m`), `replay_dir` (`"./replays"`), `db_path` (`"./rl-sync.db"`), `keep_local_files` (`true`), `download_timeout` (`30s`), `replay_limit` (`200` default when `<= 0`), `dry_run` (`false`), `once` (`false`).
- `stats_api` (`StatsAPIConfig`): `enabled` (`true`), `address` (`"127.0.0.1:49124"`), `protocol` (`"websocket"` | `"tcp"`), `trigger_threshold` (`15`), `force_sync_on_trigger` (`false`), `enable_toast` (`true`), `http_trigger_port` (`49125`), `auto_sync_on_exit` (`true`).
- `web` (`WebConfig`): `enabled` (`true`), `host` (`"127.0.0.1"`), `port` (`49125`).
- `logging` (`LoggingConfig`): `level` (`"info"`), `format` (`"text"` | `"json"`).

### 1.2 Environment Overrides (Preserved 100%)
Include every legacy `RL_SYNC_*` variable:
- Auth (primary): `RL_SYNC_AUTH_PROVIDER`, `RL_SYNC_EPIC_REFRESH_TOKEN`, `RL_SYNC_EPIC_AUTH_CODE`, `RL_SYNC_EPIC_ACCOUNT_ID`, `RL_SYNC_EPIC_DISPLAY_NAME`, `RL_SYNC_STEAM_USERNAME`, `RL_SYNC_STEAM_PASSWORD`, `RL_SYNC_STEAM_SESSION_TICKET`, `RL_SYNC_STEAM_ID_64`, `RL_SYNC_STEAM_ACCOUNT_NAME`, `RL_SYNC_STEAM_LOGIN_KEY`
- Polling auth: `RL_SYNC_POLLING_AUTH_ENABLED`, `RL_SYNC_POLLING_AUTH_PROVIDER`, `RL_SYNC_POLLING_EPIC_REFRESH_TOKEN`, `RL_SYNC_POLLING_EPIC_AUTH_CODE`, `RL_SYNC_POLLING_EPIC_AUTHORIZATION_CODE`, `RL_SYNC_POLLING_EPIC_ACCOUNT_ID`, `RL_SYNC_POLLING_EPIC_DISPLAY_NAME`, `RL_SYNC_POLLING_STEAM_USERNAME`, `RL_SYNC_POLLING_STEAM_PASSWORD`, `RL_SYNC_POLLING_STEAM_SESSION_TICKET`, `RL_SYNC_POLLING_STEAM_ID_64`, `RL_SYNC_POLLING_STEAM_ACCOUNT_NAME`, `RL_SYNC_POLLING_STEAM_LOGIN_KEY`
- Player tracking & session: `RL_SYNC_PLAYER_TRACKING_ENABLED`, `RL_SYNC_LOCAL_PLAYER_ID`, `RL_SYNC_PLAYER_TRACKING_LOCAL_PLAYER_ID`, `RL_SYNC_LOCAL_PLAYER_NAME`, `RL_SYNC_PLAYER_TRACKING_LOCAL_PLAYER_NAME`, `RL_SYNC_AUTO_FETCH_RANKS`, `RL_SYNC_PLAYER_TRACKING_AUTO_FETCH_RANKS`, `RL_SYNC_SESSION_CONTINUE_WINDOW`, `RL_SYNC_CONTINUE_WINDOW`
- Ballchasing & Sync: `RL_SYNC_BALLCHASING_API_KEY`, `RL_SYNC_BALLCHASING_VISIBILITY`, `RL_SYNC_BALLCHASING_BASE_URL`, `RL_SYNC_BALLCHASING_TIMEOUT`, `RL_SYNC_BALLCHASING_MAX_RETRIES`, `RL_SYNC_POLL_INTERVAL`, `RL_SYNC_REPLAY_DIR`, `RL_SYNC_DB_PATH`, `RL_SYNC_KEEP_LOCAL_FILES`, `RL_SYNC_DOWNLOAD_TIMEOUT`, `RL_SYNC_DRY_RUN`, `RL_SYNC_ONCE`
- Stats API, Web & Logging: `RL_SYNC_STATS_API_ENABLED`, `RL_SYNC_STATS_API_ADDRESS`, `RL_SYNC_STATS_API_PROTOCOL`, `RL_SYNC_TRIGGER_THRESHOLD`, `RL_SYNC_FORCE_SYNC_ON_TRIGGER`, `RL_SYNC_ENABLE_TOAST`, `RL_SYNC_HTTP_TRIGGER_PORT`, `RL_SYNC_AUTO_SYNC_ON_EXIT`, `RL_SYNC_WEB_ENABLED`, `RL_SYNC_WEB_HOST`, `RL_SYNC_WEB_PORT`, `RL_SYNC_LOG_LEVEL`, `RL_SYNC_LOG_FORMAT`

### 1.3 Comment-Preserving Atomic Persistence (`yaml_update.go` & `atomic.go`)
- `AtomicWriteFile(path, data, perm)`: write temp file in same directory, `Sync()`, close, `Rename` over target (plus Windows retry backoff for AV locks), clean up temp on failure.
- `UpdateYAMLAuthTokens(configPath, section, updates)`: unmarshals into `yaml.Node` AST, locates/creates `section` (`"auth"` or `"polling_auth"`), updates/inserts string scalar nodes without deleting user comments, clears `auth_code` once token is exchanged if configured, writes atomically with `0600`.
- Validation (`cfg.Validate()`): rejects invalid `provider`, invalid `visibility` (`public`, `unlisted`, `private`), invalid `protocol` (`websocket`, `tcp`), invalid `log.level`/`log.format`, invalid `web.port` (`<1 || >65535` when enabled), negative durations, and enforces auth requirements only when non-dry-run sync needs credentials.

---

## 2. SQLite Storage Architecture (`internal/storage/`)

### 2.1 Decomposition Rules (Eliminating the 5,642-LOC Monolith)
Replace monolithic `sqlite.go` + duplicated `jsonstore.go` with focused repositories sharing one serialized write/read DB handle:
- `store.go` (< 300 LOC): `StateStore` (all 44 methods preserved), `MatchPruner`, model types, errors (`ErrMatchNotFound`, `ErrPlayerNotFound`, `ErrSessionNotFound`).
- `db.go` (< 250 LOC): `SQLiteStore` struct, `modernc.org/sqlite` opener with pragmas (`MaxOpenConns(1)`, `WAL`, `busy_timeout=5000`, `synchronous=NORMAL`, `foreign_keys=ON`), `Close()`.
- `migrations.go` (< 350 LOC): `schemaDDL` (exact 8 tables + 21 indexes), `v1 -> v4` column/index upgrades, idempotent rank-repair pass, local account deduplication.
- `matches_repo.go` (< 500 LOC): match CRUD, `ResetSyncingToPending` (honoring `017db99` — NEVER reset `SYNCED` matches with 0 players), pending retries, `ListHistoricalMatches` (**batched** `WHERE match_guid IN (...)` player load — zero N+1!), replay stats upsert preserving existing `Players` when incoming count is 0 (`017db99`), `PruneMatches`.
- `players_repo.go` (< 500 LOC): player upsert, matchup updates, atomic `RecordMatchResults`, local account resolution (`3744274` — exclude `LOWER(provider) LIKE '%_polling'`, count `is_local=1`), MMR history, `SearchPlayers` (batched subquery/CTE eliminating N+1), `GetPlayerDossier` + `ListPlayerMatches` (batched!).
- `stats_repo.go` (< 400 LOC): `GetPlayerRoleStats` (grouped SQL + `@perf-isolation` accumulator parity tested against `tests/parity/split_stats_parity_test.go`), `GetGlobalSpotlights`.
- `sessions_repo.go` (< 250 LOC): session CRUD storing `started_at`, `ended_at`, `last_match_at`, `updated_at` in Unix nanoseconds; auto-detect `<1e12` seconds / `<1e15` ms / ns on read.
- `legacy_json_import.go` (< 250 LOC): If `NewStore(path)` receives `.json`, open/migrate `<path>.db` (or `<stem>.db`) and import existing JSON state once idempotently if DB is empty, maintaining 100% `NewStore(jsonPath)` backward compatibility without maintaining two separate 3,800-LOC storage engines!

### 2.2 Authoritative SQLite Schema (8 Tables, 21 Indexes)
1. `matches`: `match_guid PK`, `match_time`, `game_mode`, `playlist_id`, `player_names`, `replay_url`, `local_path`, `ballchasing_id`, `ballchasing_url`, `status`, `error_message`, `retry_count`, `replay_stats_json`, `replay_stats_status`, `synced_at`, `created_at`, `updated_at`, `download_retry_count`, `upload_retry_count`, `stats_retry_count`.
2. `auth_state`: `provider PK`, `epic_account_id`, `access_token`, `refresh_token`, `expires_at`, `display_name`, `account_id`, `updated_at`.
3. `players`: `player_id PK`, `platform`, `account_id`, `name`, `ranks_json`, `last_seen_at`, `notes`, `updated_at`.
4. `player_matchups`: `(player_id, playlist_id)` PK, `games_with`, `wins_with`, `losses_with`, `games_against`, `wins_against`, `losses_against`, `last_played_at`, `updated_at`.
5. `processed_match_outcomes`: `match_guid PK`, `playlist_id`, `outcome`, `processed_at`.
6. `match_players`: `(match_guid, player_id)` PK, `platform`, `player_name`, `team_num`, `is_local`, `score`, `goals`, `assists`, `saves`, `shots`, `demos`, `mvp`, `playlist_id`, `match_time`, `outcome`, `created_at`, `stats_json`.
7. `mmr_history`: `id AUTOINCREMENT PK`, `player_id`, `playlist_id`, `match_guid`, `mmr`, `tier`, `division`, `recorded_at`, `UNIQUE(player_id, playlist_id, match_guid)`.
8. `sessions`: `session_id PK`, `started_at`, `ended_at`, `last_match_at`, `updated_at`, `total_matches`, `total_wins`, `total_losses`, `win_rate`, `playlists_json`, `matches_json`, `local_player_ids`, `dismissed`.
Plus all 21 indexes on `(status)`, `(match_time DESC)`, `(replay_stats_status)`, `(ballchasing_id)`, `players(name COLLATE NOCASE)`, `match_players(player_id, match_time DESC)`, `mmr_history(player_id, playlist_id, recorded_at DESC)`, `sessions(updated_at DESC)`.
