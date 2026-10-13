# Rocket League Synchronizer & Live Esports Dashboard (`rl-sync`)

`rl-sync` is a lightweight, all-in-one Go background daemon and real-time companion application for Rocket League. It combines automated replay synchronization with [Ballchasing.com](https://ballchasing.com), an embedded ACID SQLite database, real-time in-game telemetry tracking, true in-game MMR calculation, and a modern broadcast-grade React web dashboard and OBS streaming overlay.

---

## Table of Contents

- [Features](#features)
- [Architecture Overview](#architecture-overview)
- [Getting Started (Users)](#getting-started-users)
  - [Prerequisites](#prerequisites)
  - [Quick Start with Standalone Binary](#quick-start-with-standalone-binary)
  - [Configuration Setup](#configuration-setup)
  - [Enabling Rocket League Local Stats API](#enabling-rocket-league-local-stats-api)
  - [Historical Ballchasing Backfill](#historical-ballchasing-backfill)
- [Web Dashboard & Overlay](#web-dashboard--overlay)
  - [Live Match Broadcast View](#live-match-broadcast-view)
  - [Session History & Analytics](#session-history--analytics)
  - [Historical Games Archive](#historical-games-archive)
  - [Player Directory & Scouting](#player-directory--scouting)
  - [OBS Transparent Streaming Overlay](#obs-transparent-streaming-overlay)
  - [One-Click Demo / Preview UI](#one-click-demo--preview-ui)
- [CLI Flags & Options](#cli-flags--options)
- [Environment Variables](#environment-variables)
- [Developer Guide](#developer-guide)
  - [Dev Prerequisites](#dev-prerequisites)
  - [Local Setup & Initialization](#local-setup--initialization)
  - [Running the Frontend Dev Server](#running-the-frontend-dev-server)
  - [Running Tests](#running-tests)
  - [Production Build & Packaging](#production-build--packaging)
  - [Deployment & Autostart (Windows)](#deployment--autostart-windows)
- [Repository Structure](#repository-structure)

---

## Features

### 🎮 Live In-Game Match View
- **Broadcast-Grade Scoreboard**: Sleek horizontal Blue vs. Orange split featuring glowing team headers with aggregated goals and team scores.
- **True In-Game MMR**: Automatically converts raw PsyNet ratings to standard, recognizable in-game MMR values using `round((SkillRating * 20) + 100)` across all data layers.
- **Competitive Parity Pill**: Dynamically detects when teams are within 5 average MMR points and displays an **"Even Match"** badge, or highlights MMR advantage.
- **Prominent Performance Badges**: High-contrast, glowing stat pills for Score, Goals (amber glow), Assists, Saves, Shots, and Demos.
- **Disconnected Player Handling**: Preserves accrued stats and match outcomes when participants leave mid-game while displaying a distinct dimmed styling (`opacity-65`, `saturate-60`) and `DISCONNECTED` badge.
- **Zero Vertical Scrolling**: Viewport-constrained layout designed to fit standard 1080p desktop displays without scrolling.

### 📊 In-Depth Session & Historical Analysis
- **Dual Match Views**:
  - **Session History**: Tracks the current play session with win/loss progress bar, net goals, average score per match, streak tracking, and strict official MVP attribution (awarded only to the winning team's top scorer).
  - **Historical Games**: Dedicated archive of all matches stored locally with search, playlist filters, and sorting by date, MMR delta, and score.
- **Detailed Match Modal**: Inspect comprehensive box scores, individual player MMR, platforms, and head-to-head records for any completed match.

### 👥 Player Directory & Scouting Telemetry
- **Encounter Directory**: Search and filter every player you have played with or against.
- **KPI Scouting Ribbon**: Identifies your **Top Teammate Synergy** (most wins together with exact W-L) and **Frequent Rival** (most frequent opponent).
- **Direct Tracker Links**: Resolves deterministic Steam64 IDs and Epic account IDs to link directly to player profiles on [Rocket League Tracker Network](https://rocketleague.tracker.network).
- **Accurate Role Telemetry**: Displays `---` for unencountered roles rather than misleading 0% win rates.

### 🔄 Replay Sync & Ballchasing Integration
- **Automated Replay Downloader**: Polls PsyNet match history, downloads `.replay` binaries using atomic renaming to prevent corrupted files, and stores them locally.
- **Replay Metadata Parser**: Built-in pure Go `.replay` binary parser extracts header properties, box scores, player statistics, and team outcomes.
- **Live Sync Hook**: Automatically enriches local database records with parsed replay statistics upon download, keyed by `MatchGUID`.
- **Ballchasing.com Upload**: Streams replays via multipart `POST /v2/upload` with customizable visibility (`public`, `unlisted`, `private`).
- **Historical Ballchasing Backfill**: One-time sync backfills all replays ever uploaded to Ballchasing.com, strictly respecting rate limits (2 req/sec) with automatic exponential backoff on HTTP 429.

### 🛡️ Smart Session Protection & Anti-Kick
- **Local Stats API Client**: Connects locally to `ws://127.0.0.1:49124` without sending PsyNet auth tokens, preventing duplicate login errors (Error 67) and party disconnects while playing.
- **Secondary Polling Account**: Optionally configure a secondary Epic or Steam account to query player competitive ranks out-of-band so your primary account is never interrupted.
- **Buffer Warning & Toast Alerts**: Native Windows notifications alert you when the 20-match rolling queue reaches the warning threshold (default: 15 matches).
- **Auto-Sync on Game Exit**: Automatically synchronizes pending matches the moment Rocket League closes.

---

## Architecture Overview

```
                      ┌──────────────────────────────────────┐
                      │    Rocket League Game Client         │
                      │  (Local Stats API ws://127.0.0.1)    │
                      └──────────────────┬───────────────────┘
                                         │ In-game telemetry & GUIDs
                                         ▼
┌─────────────────────────┐   ┌──────────────────────────────┐   ┌──────────────────────────┐
│   Epic Games / Steam    │──▶│      rl-sync Go Daemon       │──▶│     Ballchasing.com      │
│  (Auth & PsyNet RPC)    │   │  (Replay Syncer & Tracker)   │   │  (Upload & History Sync) │
└─────────────────────────┘   └──────────────┬───────────────┘   └──────────────────────────┘
                                             │
                      ┌──────────────────────┼──────────────────────┐
                      ▼                                             ▼
       ┌──────────────────────────────┐              ┌──────────────────────────────┐
       │   Embedded SQLite Database   │              │     Embedded Web Server      │
       │   (rl-sync.db pure Go ACID)  │              │   (React SPA + SSE Events)   │
       └──────────────────────────────┘              └──────────────┬───────────────┘
                                                                    │ http://localhost:49125
                                                                    ▼
                                                     ┌──────────────────────────────┐
                                                     │  Live Esports Web Dashboard  │
                                                     │   & OBS Streaming Overlay    │
                                                     └──────────────────────────────┘
```

---

## Getting Started (Users)

### Prerequisites
- Windows 10/11 (or Linux/macOS)
- Rocket League (Epic Games or Steam)
- *(Optional)* [Ballchasing.com API key](https://ballchasing.com/upload) for uploading replays.

### Quick Start with Standalone Binary

1. Download the latest `rl-sync.exe` from releases (or locate `rl-sync.exe` in the project root).
2. Copy the example configuration template:
   ```powershell
   Copy-Item configs\config.example.yaml config.yaml
   ```
3. Edit `config.yaml` to specify your authentication and Ballchasing settings.
4. Launch the daemon:
   ```powershell
   .\rl-sync.exe -config config.yaml
   ```
5. Open your browser and navigate to:
   - **Dashboard**: [http://localhost:49125](http://localhost:49125)
   - **OBS Overlay**: [http://localhost:49125/?mode=overlay](http://localhost:49125/?mode=overlay)

---

### Configuration Setup

`rl-sync` supports layered configuration via `config.yaml` (or `config.json`), CLI flags, and environment variables.

#### Example `config.yaml`:
```yaml
auth:
  provider: "epic" # "epic" or "steam"

  epic:
    # Leave empty for interactive browser login; token will be auto-saved here.
    refresh_token: ""

  steam:
    username: ""
    password: ""
    # Or provide manual ticket:
    session_ticket: ""
    steam_id_64: ""

# Secondary account for non-intrusive rank fetching (avoids Error 67 duplicate login)
polling_auth:
  enabled: true
  provider: "epic"
  epic:
    refresh_token: "" # Secondary Epic account token

ballchasing:
  api_key: "your-ballchasing-api-key"
  visibility: "public" # "public", "unlisted", or "private"

sync:
  poll_interval: "5m"
  replay_dir: "./replays"
  db_path: "./rl-sync.db"
  keep_local_files: true

stats_api:
  enabled: true
  address: "127.0.0.1:49124"
  protocol: "websocket"
  trigger_threshold: 15
  auto_sync_on_exit: true
  enable_toast: true

web:
  enabled: true
  host: "0.0.0.0"
  port: 49125
```

---

### Enabling Rocket League Local Stats API

To allow `rl-sync` to track your in-game live matches without triggering duplicate login kicks on your primary account:

1. Locate your Rocket League configuration directory:
   - **Epic Games**: `<Epic Games Directory>\rocketleague\TAGame\Config\`
   - **Steam**: `<Steam Library>\steamapps\common\rocketleague\TAGame\Config\`
2. Open or create `TAStatsAPI.ini` and add:
   ```ini
   [TAGame.MatchStatsExporter_TA]
   Port=49123
   WebPort=49124
   PacketSendRate=30
   ```
3. Restart Rocket League. `rl-sync` will automatically connect to the local websocket on `127.0.0.1:49124`.

---

### Historical Ballchasing Backfill

To synchronize all past matches ever uploaded to your Ballchasing.com account and populate teammates, opponents, and head-to-head records:

- **Via Web UI**: Open the **Historical Games** tab on the web dashboard and click the **"Sync Ballchasing"** button. A real-time progress bar will track the backfill.
- **Via CLI**: Run `rl-sync` with the `-sync-ballchasing-history` flag:
  ```powershell
  .\rl-sync.exe -config config.yaml -sync-ballchasing-history
  ```
The sync strictly enforces a rate limit of 2 requests/second, handles pagination, and automatically pauses with exponential backoff if an HTTP 429 response is encountered.

---

## Web Dashboard & Overlay

The embedded dashboard is served on port `49125` by default (`http://localhost:49125`).

### Live Match Broadcast View
- Shows active match status, current playlist, game timer, and real-time score.
- Team rosters with individual Score, Goals, Assists, Saves, Shots, Demos, Platform, Rank, True in-game MMR, and Head-to-Head win rate.
- Column customizer allows toggling columns on/off with presets (Default, Competitive, Compact, Streamer).

### Session History & Analytics
- Live session stats: Total Wins, Losses, Win Rate %, Net Goals, Average Score, Longest Win Streak, and Highest Game Score.
- Search matches by player or playlist, filter by outcome (Victories/Defeats), and sort by date, MMR change, or score.
- Click any match card to open the **Match Detail Modal** for a complete player-by-player breakdown.

### Historical Games Archive
- Full search and filtering over your permanent local SQLite match database.
- Displays match duration, overtime status, MMR changes, and outcome badges.
- Trigger on-demand Ballchasing historical synchronization.

### Player Directory & Scouting
- Complete list of every player encountered in matches.
- Shows total encounters, teammate record, opponent rivalry record, and current competitive rank snapshot.
- Top Teammate Synergy and Frequent Rival summary ribbon.
- Click any player to view their profile, detailed playlist ranks, and launch their direct Rocket League Tracker Network profile.

### OBS Transparent Streaming Overlay
Add a browser source to OBS Studio or Streamlabs:
- **URL**: `http://localhost:49125/?mode=overlay`
- **Width**: `500`
- **Height**: `120`
- **Custom CSS**: Leave blank (background is natively transparent with backdrop blur).

### One-Click Demo / Preview UI
Want to see what the live game view or match history looks like without playing a match?
- Click the **"Preview UI"** button in the top navigation bar, or open:
  `http://localhost:49125/?preview=true`
- Simulates a live competitive match with full box scores and session history. Click **"Exit Preview"** to return to live tracking.

---

## CLI Flags & Options

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `-config` | `-c` | `""` | Path to configuration file (`config.yaml` or `config.json`). |
| `-once` | | `false` | Run a single synchronization cycle and exit. |
| `-dry-run` | | `false` | Simulate match detection without downloading files or uploading to Ballchasing. |
| `-poll-interval` | | `5m` | Interval between PsyNet polling cycles (e.g. `5m`, `1m`, `30s`). |
| `-provider` | | `""` | Override primary auth provider (`epic` or `steam`). |
| `-replay-dir` | | `""` | Directory where downloaded `.replay` files are saved. |
| `-db-path` | | `""` | Path to SQLite database or JSON state store file. |
| `-sync-ballchasing-history` | | `false` | Synchronize all historical replays from Ballchasing.com and exit. |
| `-auto-fetch-ranks` | | `true` | Automatically fetch competitive ranks for lobby players. |
| `-polling-auth` | | `false` | Enable secondary account authentication for rank retrieval. |
| `-polling-provider` | | `""` | Provider for secondary account (`epic` or `steam`). |
| `-local-player-id` | | `""` | Override local player ID (e.g. `Epic|<id>|0` or `Steam|<id>|0`). |
| `-local-player-name` | | `""` | Override local player display name. |
| `-stats-api` | | `true` | Enable Rocket League Stats API real-time event tracking. |
| `-trigger-threshold` | | `15` | Threshold of un-downloaded matches before notification. |
| `-force-sync` | | `false` | Force immediate PsyNet sync when threshold is reached. |
| `-web-enabled` | | `true` | Enable embedded web dashboard and API server. |
| `-web-host` | | `0.0.0.0` | HTTP host binding for web dashboard. |
| `-web-port` | | `49125` | HTTP port binding for web dashboard. |
| `-log-level` | | `info` | Logging verbosity (`debug`, `info`, `warn`, `error`). |
| `-log-format` | | `text` | Log format (`text` or `json`). |
| `-version` | `-v` | `false` | Display version and exit. |
| `-help` | `-h` | `false` | Display help usage and exit. |

---

## Environment Variables

Every setting can be specified via environment variables:

| Environment Variable | Description |
| :--- | :--- |
| `RL_SYNC_AUTH_PROVIDER` | `epic` or `steam` |
| `RL_SYNC_EPIC_REFRESH_TOKEN` | Epic Games OAuth refresh token |
| `RL_SYNC_EPIC_AUTH_CODE` | Epic Games one-time authorization code |
| `RL_SYNC_STEAM_USERNAME` | Steam account username |
| `RL_SYNC_STEAM_PASSWORD` | Steam account password |
| `RL_SYNC_STEAM_SESSION_TICKET` | Steam hex session ticket |
| `RL_SYNC_STEAM_ID_64` | 64-bit Steam ID |
| `RL_SYNC_POLLING_AUTH_ENABLED` | Enable secondary polling account (`true`/`false`) |
| `RL_SYNC_POLLING_AUTH_PROVIDER`| Secondary provider (`epic` or `steam`) |
| `RL_SYNC_POLLING_EPIC_REFRESH_TOKEN` | Secondary Epic account refresh token |
| `RL_SYNC_BALLCHASING_API_KEY` | Ballchasing API key |
| `RL_SYNC_BALLCHASING_VISIBILITY` | Upload visibility (`public`, `unlisted`, `private`) |
| `RL_SYNC_POLL_INTERVAL` | Polling frequency (e.g. `5m`) |
| `RL_SYNC_REPLAY_DIR` | Replay destination directory |
| `RL_SYNC_DB_PATH` | Path to SQLite persistence database |
| `RL_SYNC_STATS_API_ENABLED` | Enable Stats API tracking (`true`/`false`) |
| `RL_SYNC_STATS_API_ADDRESS` | Stats API address (`127.0.0.1:49124`) |
| `RL_SYNC_WEB_ENABLED` | Enable web dashboard (`true`/`false`) |
| `RL_SYNC_WEB_PORT` | Web dashboard port (default `49125`) |

---

## Developer Guide

### Dev Prerequisites
- **Go**: 1.24 or newer (`go version`)
- **Node.js**: v20+ and npm (`node -v`, `npm -v`)
- **Git**

---

### Local Setup & Initialization

1. **Clone the repository**:
   ```bash
   git clone https://github.com/dank/rl-api-utils.git
   cd rl-api-utils
   ```

2. **Initialize Go modules**:
   ```bash
   go mod download
   go mod verify
   ```

3. **Install Frontend Dependencies**:
   ```bash
   cd web
   npm install
   cd ..
   ```

---

### Running the Frontend Dev Server

During frontend UI development, you can run Vite with hot module replacement (HMR):

```bash
cd web
npm run dev
```

Vite will start on `http://localhost:5173`. When developing against a running `rl-sync` backend daemon on port `49125`, Vite automatically proxies `/api` requests to the daemon.

---

### Running Tests

#### Backend Go Tests
The Go test suite covers unit logic, concurrency stress, adversarial replay corruption, and e2e pipelines:

```powershell
# Run all Go tests across all 15 packages
go test -v -count=1 ./...

# Run static analysis
go vet ./...

# Run specific package tests (e.g. replay parser or session tracking)
go test -v ./internal/replay/...
go test -v ./internal/session/...
```

#### Frontend Vitest Tests
The frontend suite validates UI layout, MMR formatting, column bitmask permutations, and disconnected styling:

```powershell
# Run all frontend tests
npm --prefix web test

# Run tests in watch mode
npm --prefix web run test:watch
```

---

### Production Build & Packaging

The production build embeds the compiled React SPA assets directly into the Go executable via `//go:embed dist/*` in `internal/web/embed.go`, producing a single standalone binary without external web file dependencies.

```powershell
# 1. Compile frontend production bundle into internal/web/dist
npm --prefix web run build

# 2. Compile standalone single binary with embedded assets
go build -v -ldflags="-s -w" -o rl-sync.exe ./cmd/rl-sync

# 3. Verify compiled executable
.\rl-sync.exe -version
```

---

### Deployment & Autostart (Windows)

#### Running in Background via PowerShell
```powershell
Start-Process -FilePath ".\rl-sync.exe" -ArgumentList "-config", "config.yaml" -WindowStyle Hidden
```

#### Windows Task Scheduler (Autostart on Login)
To ensure `rl-sync` runs seamlessly in the background when logging into Windows:

```powershell
$Action = New-ScheduledTaskAction -Execute "D:\code\rl-api-utils\rl-sync.exe" -Argument "-config D:\code\rl-api-utils\config.yaml" -WorkingDirectory "D:\code\rl-api-utils"
$Trigger = New-ScheduledTaskTrigger -AtLogOn
Register-ScheduledTask -TaskName "RL-Sync-Daemon" -Action $Action -Trigger $Trigger -Description "Rocket League Replay Sync & Live Dashboard Daemon"
```

---

## Repository Structure

```
rl-api-utils/
├── cmd/
│   └── rl-sync/               # Application entry point & CLI flag parsing
├── configs/
│   ├── config.example.yaml    # Documented configuration template (YAML)
│   └── config.example.json    # Configuration template (JSON)
├── internal/
│   ├── auth/                  # Epic Games & Steam authentication adapters
│   ├── ballchasing/           # Ballchasing API client (multipart upload & backoff)
│   ├── config/                # Layered configuration loader & env bindings
│   ├── daemon/                # Daemon orchestrator, HTTP endpoints, & SSE server
│   ├── playertrack/           # Live lobby tracking, PsyNet rank client, & MMR conversion
│   ├── psynet/                # rlapi match history & atomic replay downloader
│   ├── replay/                # Pure-Go .replay binary header & property parser
│   ├── session/               # Active play session manager & match detail models
│   ├── statsapi/              # Rocket League local Stats API (TAStatsAPI) listener
│   ├── storage/               # Pure-Go ACID SQLite store (modernc.org/sqlite) & JSON store
│   ├── syncer/                # Core sync engine & Ballchasing historical backfill
│   ├── testutil/              # Hermetic mock servers (PsyNet, Ballchasing, Stats API)
│   └── web/                   # Go embedded filesystem handler (embed.FS)
│       └── dist/              # Compiled Vite production web assets
├── web/                       # React 18 + TypeScript + Vite + Tailwind CSS frontend
│   ├── src/
│   │   ├── components/
│   │   │   ├── common/        # RankBadge, H2HBadge
│   │   │   ├── historical/    # HistoricalGamesView (archive & Ballchasing backfill)
│   │   │   ├── layout/        # Header, Navbar, Preview UI toggle
│   │   │   ├── live/          # LiveGameView, RosterTable, PlayerRow, ScoreboardBanner
│   │   │   ├── overlay/       # OBSOverlayView (transparent streaming HUD)
│   │   │   ├── players/       # PlayerDirectoryView, PlayerCard, PlayerProfileModal
│   │   │   └── session/       # SessionHistoryView, MatchCard, MatchDetailModal
│   │   ├── hooks/             # useSession, useLiveMatch, useColumnConfig
│   │   ├── types/             # API data contracts & column configuration types
│   │   └── utils/             # Formatters, platforms, sampleData (demo mode)
│   ├── package.json
│   ├── vite.config.ts
│   └── tsconfig.json
├── steam/                     # Node.js Steam ticket generator utility
├── test/
│   └── e2e/                   # 5-Tier integration & concurrent stress test suites
├── rl-sync.exe                # Pre-built standalone production executable
├── go.mod                     # Go dependencies
├── go.sum
└── README.md                  # Project documentation
```

---

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for details.
