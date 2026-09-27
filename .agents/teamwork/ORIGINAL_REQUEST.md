# Original User Request

## 2026-09-25T02:56:38Z

An automated Rocket League daemon written in Go that interfaces with Rocket League's internal PsyNet API via github.com/dank/rlapi to poll match history every 5 minutes, download new .replay files, and automatically upload them to ballchasing.com.

Working directory: d:/code/rl-api-utils
Integrity mode: development

References:
- Rocket League Go SDK: https://github.com/dank/rlapi
- Ballchasing API Documentation: https://ballchasing.com/doc/api

## Requirements

### R1. Match Polling & Replay Synchronization
A Go daemon that periodically (defaulting to every 5 minutes) queries the authenticated player's Rocket League match history using github.com/dank/rlapi (Matches/GetMatchHistory v1). It must identify matches with valid replay URLs that have not yet been downloaded, and download the .replay binary payloads to a configurable local storage directory.

### R2. Ballchasing.com Replay Uploader
An integration that automatically uploads downloaded .replay files to the ballchasing.com API (POST /v2/upload) using multipart form data (file field). The uploader must support user-configured visibility (public, unlisted, private), authenticate via an API key (Authorization: <token>), and gracefully handle HTTP 201 (Created), HTTP 409 (duplicate replay), and HTTP 429 (rate limiting with backoff).

### R3. Persistent State & Idempotency
The utility must persist processing state (e.g. SQLite database or structured JSON state store) tracking match GUIDs, timestamps, download paths, and ballchasing upload statuses (including returned replay IDs). On startup and across polling cycles, already downloaded or uploaded matches must not be re-downloaded or re-uploaded.

### R4. Dual Authentication (Epic Games & Steam) & Configuration
The service must support both Epic Games and Steam authentication paths using rlapi:
- Epic Games Auth: Authenticate using EGS refresh tokens or authorization codes exchanged for EOS tokens (egs.ExchangeEOSToken and psyNet.AuthPlayer).
- Steam Auth: Authenticate using Steam session tickets exchanged for EOS tokens (egs.ExchangeEOSTokenFromSteam) and Steam PsyNet authentication (psyNet.AuthPlayerSteam with Steam ID 64).
- Configuration: Configurable via environment variables or configuration file for authentication provider choice, credentials/tokens, Ballchasing API key, upload visibility, polling interval, and local replay directory path.
- Logging: Structured logging with timestamps and descriptive error reporting.

### R5. Automated Verification Test Suite & Mock Harness
An automated programmatic test harness independent of live Rocket League or Ballchasing credentials that:
- Mocks PsyNet RPC GetMatchHistory responses simulating new match entries across consecutive polling cycles
- Simulates replay file downloads from mock URLs
- Mocks both Epic Games and Steam authentication exchanges
- Mocks Ballchasing API endpoints testing 201 (success), 409 (duplicate), 429 (rate limit backoff), and 401 (unauthorized error handling)
- Verifies state persistence across simulated daemon restarts to guarantee idempotency

## Acceptance Criteria

### Core Synchronization & Upload
- [ ] Daemon queries match history on schedule and successfully downloads .replay files for newly identified matches.
- [ ] Uploads .replay files to ballchasing.com via multipart/form-data with configured Authorization header and visibility.
- [ ] Correctly processes HTTP 201 Created responses and records Ballchasing replay IDs in local state.
- [ ] Handles HTTP 409 Conflict without error or retry thrashing, marking the replay as already uploaded/duplicate.
- [ ] Handles HTTP 429 Rate Limit responses by backing off and retrying as appropriate.

### Authentication & Provider Flexibility
- [ ] Supports Epic Games authentication flow (refresh token / exchange code).
- [ ] Supports Steam authentication flow (Steam session ticket and Steam ID 64).
- [ ] Configuration dynamically selects the active authentication provider and loads appropriate credentials.

### State Management & Reliability
- [ ] Previously processed matches are skipped on subsequent polling cycles without re-downloading or duplicate uploads.
- [ ] State persists across application restarts, ensuring clean recovery and no redundant API requests.

### Verification & Test Suite
- [ ] Automated test suite runs via standard Go tooling (go test ./...) and passes with 100% success.
- [ ] Test harness exercises the full pipeline (authentication -> polling -> download -> upload -> state save) using mocked PsyNet and Ballchasing servers for both auth providers.
- [ ] A CLI single-run / dry-run flag is provided to allow executing one sync cycle on demand.

## 2026-09-26T00:19:44Z

Expand the Rocket League daemon in d:\code\rl-api-utils to track all players encountered in matches via the local Rocket League Stats API, maintaining per-playlist historical records of wins and losses when playing alongside them as teammates versus against them as opponents. Integrate a secondary non-playing PsyNet polling account to asynchronously fetch and store all players' competitive ranks, divisions, and MMR in real time without causing duplicate login kicks (Error 67) to the primary player account. Expose the live lobby state and historical matchup data via local HTTP endpoints.

Working directory: d:/code/rl-api-utils
Integrity mode: development

References:
- Player Tracking Plan: c:\Users\strms\.gemini\antigravity\brain\11baea32-4a41-4d49-b959-d518322eea18\player_tracking_plan.md
- Rocket League Go SDK: github.com/dank/rlapi
- Local Stats API Exporter: MatchStatsExporter_TA (WebSocket ws://127.0.0.1:49124 / TCP 49123)

## Requirements

### R1. Stats API Ingestion of Lobby Players & Match Results
Expand the local Stats API listener (internal/statsapi) to decode full player lobby data and match results from MatchStatsExporter_TA:
- Parse Players in UpdateState events including player names, primary IDs (e.g. Steam|765611...|0 or Epic|<account_id>|0), team numbers (0 for Blue, 1 for Orange), and in-game stats.
- Parse WinnerTeamNum in MatchEnded events.
- Forward parsed match events to the player tracking subsystem.

### R2. Teammate vs. Opponent Matchup Tracking & Persistent Storage
Track player encounter history per playlist in persistent storage (internal/storage - SQLite and JSON store parity):
- Schema support for players (player ID, platform, name, ranks JSON, first seen, last seen) and player_matchups (player ID, playlist ID, wins as teammate, losses as teammate, wins as opponent, losses as opponent, total matches, last played timestamp).
- Implement tracking logic that resolves the local player's identity and team number, determines whether other players are teammates or opponents, and automatically records the outcome on match end:
  - If teammate and my team won: increment wins_as_teammate.
  - If teammate and my team lost: increment losses_as_teammate.
  - If opponent and my team won: increment wins_as_opponent.
  - If opponent and my team lost: increment losses_as_opponent.
- Ensure idempotency so that duplicate match events for the same match GUID do not double-count results.

### R3. Secondary Non-Playing Account & PsyNet Rank Retrieval
Support a dedicated secondary polling account (polling_auth) to fetch competitive skill data without triggering duplicate login kicks on the main account:
- Configuration for polling_auth (Epic Games or Steam provider, credentials, enabled flag) and player_tracking (enabled, local player ID / name overrides, auto fetch ranks) with environment variable overrides.
- Provide a skill-fetching client that connects to PsyNet with the polling account credentials and queries Skills/GetPlayersSkills v1 for all lobby players.
- Parse and format tiers, divisions, and MMR into human-readable representations (e.g., Tier 16, Div 3 -> "Champion II Division IV") and persist the latest rank snapshot in the database.
- Graceful degradation: if polling_auth is not configured or disabled, player matchup tracking must still function seamlessly using local Stats API data.

### R4. Local Web API Query Endpoints
Expose player tracking and current match analytics via the built-in HTTP server (internal/daemon on port 49125):
- GET /current-match: Returns active match details, list of players in the lobby categorized as teammates or opponents, their current ranks/MMR, and historical head-to-head records with/against each player in the current playlist.
- GET /players: Paginated list of all encountered players with summary records.
- GET /players/{id}: Detailed matchup record for a specific player broken down across all playlists.

### R5. Automated Testing & Backward Compatibility
- Maintain 100% test pass rate for all existing replay syncing, match history polling, and Stats API pending replay warning features.
- Provide automated unit and integration tests covering:
  - Storage operations for players and player_matchups in SQLite and JSON stores.
  - Player classification (teammate vs. opponent) and win/loss resolution on match end.
  - Mocked PsyNet Skills/GetPlayersSkills response decoding, tier conversion, and error handling.
  - HTTP handlers for /current-match, /players, and /players/{id}.

## Acceptance Criteria

### Stats API & Player Detection
- [ ] Ingests UpdateState events and accurately decodes player lists, team numbers, and primary IDs.
- [ ] Ingests MatchEnded events and extracts WinnerTeamNum.
- [ ] Accurately determines local player identity and team affiliation.

### Matchup Record Storage & Tracking
- [ ] Stores player profiles and per-playlist head-to-head records in SQLite (with matching JSON store support).
- [ ] Correctly increments wins_as_teammate / losses_as_teammate for players on the same team.
- [ ] Correctly increments wins_as_opponent / losses_as_opponent for players on the opposing team.
- [ ] Prevents double-counting match results for repeated match GUID events.

### Rank Retrieval via Secondary Polling Account
- [ ] polling_auth can be configured independently from the primary auth in config.yaml and via environment variables.
- [ ] Connects secondary account to PsyNet and queries Skills/GetPlayersSkills v1 for lobby players in batch.
- [ ] Successfully converts raw tier and division IDs to human-readable rank names and MMR values.
- [ ] Player tracking continues operating normally without errors if polling_auth is disabled or fails to authenticate.

### Local HTTP Endpoints
- [ ] GET /current-match returns valid JSON with current lobby players, team classifications, ranks, and historical record.
- [ ] GET /players returns paginated player records.
- [ ] GET /players/{id} returns complete per-playlist breakdown for the requested player ID.

### Verification & Regression
- [ ] All existing 385+ tests and newly added tests pass cleanly (go test ./...).
- [ ] Code builds without errors (go build ./cmd/rl-sync).

## 2026-09-26T03:23:29Z

A real-time Rocket League play session web dashboard exposed on the local network (0.0.0.0:49125) that tracks live game stats via the local Stats API, provides configurable stat column displays, tracks cumulative wins/losses and MMR changes (Δ) per playlist, displays session match history with detailed drill-down, and provides a searchable player tracker directory, built with an extensible React + TypeScript frontend embedded directly into the single Go executable (rl-sync.exe).

Working directory: d:/code/rl-api-utils
Integrity mode: development

References:
- Implementation Plan: c:\Users\strms\.gemini\antigravity\brain\11baea32-4a41-4d49-b959-d518322eea18\session_dashboard_plan.md
- Stats API Exporter: MatchStatsExporter_TA (WebSocket ws://127.0.0.1:49124 / TCP 49123)
- Rocket League Go SDK: github.com/dank/rlapi

## Requirements

### R1. Session Tracking Subsystem & Playlist MMR Analytics
Create a thread-safe session tracking engine (internal/session) that manages active play session telemetry:
- Track session start time, total matches, total wins, total losses, and overall win rate.
- For each playlist encountered during the session, record: matches played, wins, losses, win rate %, initial starting MMR, current MMR, and net MMR change (Δ).
- Maintain a chronological list of all matches completed during the session, capturing full roster snapshots (players, teams, in-game stats: score, goals, assists, saves, shots, demos, ranks, MMR, and historical head-to-head records) and final scores.
- Support session reset (POST /api/session/reset) to restart session counters on demand without daemon restart.
- Provide a thread-safe event broadcaster supporting Server-Sent Events (SSE) to push live updates (match_update, match_ended, session_update) to connected clients.

### R2. Searchable Player Directory in Storage Layer
Expand the persistent storage layer (internal/storage - SQLite and JSON store parity) with search capabilities:
- Implement SearchPlayerSummaries(ctx context.Context, query, platform string, limit, offset int) ([]*PlayerSummary, int, error) in StateStore.
- Support case-insensitive substring search matching player display names and platform IDs (Steam|<id>|0, Epic|<id>|0).
- Support optional platform filtering (All, Steam, Epic, etc.) and pagination (limit, offset), returning matching records and total count.

### R3. Extensible Modern Web Frontend (React + TypeScript + Vite)
Build a responsive, modern single-page dashboard in web/ using React 19, TypeScript, Vite, Tailwind CSS, and Lucide Icons:
- Live Game View: When in a match, display real-time match details (active playlist name, live score banner with Blue vs Orange team styling, player rosters, ranks, MMR, and H2H records). When not in a match, display an idle status banner and session match history.
- Configurable Stats Display: Provide an interactive column customizer modal allowing users to toggle which player stats/columns are displayed (Score, Goals, Assists, Saves, Shots, Demos, MMR, Rank, H2H, Platform). Selections must immediately update the live roster and match tables and persist in localStorage across reloads.
- Session History & Drill-Down: Render a list of all matches completed this session; clicking any match opens a detailed modal with complete rosters, individual box scores, and results.
- Searchable Player Directory: Dedicated view featuring a debounced search input, platform filter pills, player summary cards with H2H records (teammate win rate vs opponent win rate), and a drill-down modal showing all playlist matchups for a clicked player.
- Extensible Architecture: Provide modular components with clean boundaries, including a dedicated streaming overlay mode (/?mode=overlay) with a transparent background suitable for OBS/Streamlabs browser sources.

### R4. Local Network Exposure, REST/SSE APIs & Embedded Single-Binary Delivery
Serve the web application directly from the Go daemon on port 49125:
- Local Network Binding: Default HTTP server bind host to 0.0.0.0 (configurable via web.host / --web-host / RL_SYNC_WEB_HOST), allowing access from phones, tablets, or secondary PCs on the local network (http://<lan-ip>:49125/). Detect and log local IPv4 network addresses on startup.
- Embedded Static Assets: Embed the production build output (web/dist) into the Go binary using //go:embed dist/* in internal/web, serving the SPA with client-side routing fallback to index.html. End users must be able to run rl-sync.exe as a single standalone executable without Node.js installed.
- REST & SSE Endpoints:
  - GET /api/session: Current session summary, playlist stats, and match history list.
  - POST /api/session/reset: Reset active session counters.
  - GET /api/current-match: Real-time match state (compatible with existing /current-match).
  - GET /api/players: Paginated player directory with search and platform filters.
  - GET /api/players/{id}: Detailed player profile with all playlist matchups.
  - GET /api/events: Server-Sent Events (SSE) stream for live push notifications.
  - CORS middleware enabled for local Vite development (localhost:5173).

### R5. Automated Testing & Backward Compatibility
- Maintain 100% test pass rate for all existing replay syncing, match history polling, Ballchasing uploader, and player tracking features across all existing Go packages.
- Automated tests covering:
  - Frontend type checks and build (npm run build succeeds).
  - Session tracker logic (match updates, completion, playlist W/L, MMR deltas, concurrency safety).
  - Storage player search in both SQLite and JSONStore (name substring matching, platform filtering, pagination).
  - HTTP endpoints, SSE event streams, and static asset serving in internal/daemon.

## Acceptance Criteria

### Session & Analytics
- [ ] Session tracker correctly computes total wins, total losses, and win rate.
- [ ] Tracks per-playlist wins, losses, initial MMR, current MMR, and net MMR change (Δ).
- [ ] Preserves full roster and box score snapshots for all completed session matches.
- [ ] POST /api/session/reset resets session counters without crashing or disconnecting active streams.

### Storage Search
- [ ] SearchPlayerSummaries finds players matching name substrings or platform IDs in SQLite and JSONStore.
- [ ] Platform filtering correctly isolates Steam vs Epic vs all players.
- [ ] Pagination (limit, offset) and total count queries return accurate slices.

### Frontend Dashboard
- [ ] React application compiles cleanly via npm run build without TypeScript or lint errors.
- [ ] Displays live scoreboard, team rosters, MMR, ranks, and H2H records during matches.
- [ ] Column customizer allows toggling stat columns (Score, Goals, Assists, Saves, Shots, Demos, MMR, Rank, H2H, Platform) and persists choices in localStorage.
- [ ] When outside a match, displays session match list; clicking a match reveals full match details and player scores.
- [ ] Player directory allows debounced searching and shows full per-playlist breakdown when a player is selected.
- [ ] Streaming overlay mode (/?mode=overlay) renders a compact transparent view for OBS.

### Server & Network Delivery
- [ ] Server binds to 0.0.0.0:49125 by default and logs accessible LAN URL(s) on startup.
- [ ] Embedded static assets are served from rl-sync.exe without requiring Node.js at runtime.
- [ ] Client-side routing falls back to index.html on direct deep-link reloads.
- [ ] Server-Sent Events (/api/events) push live updates to the frontend immediately upon Stats API events.

### Verification & Regression
- [ ] All existing 385+ tests and newly added tests pass cleanly (go test ./...).
- [ ] Single executable rl-sync.exe builds cleanly with embedded frontend.
