# Rocket League Replay Synchronizer (`rl-sync`)

An automated background daemon written in Go that interfaces with Rocket League's internal PsyNet API via [`github.com/dank/rlapi`](https://github.com/dank/rlapi) to poll match history every 5 minutes, download new `.replay` binary payloads, and automatically upload them to [Ballchasing.com](https://ballchasing.com).

---

## Features

- **Match History Polling**: Periodically queries PsyNet RPC (`Matches/GetMatchHistory v1`) for recent matches.
- **Atomic Replay Downloads**: Downloads `.replay` files from PsyNet signed URLs with size validation and atomic file renaming to prevent corrupt files.
- **Ballchasing.com Upload**: Streams replay files via multipart `POST /v2/upload` with customizable visibility (`public`, `unlisted`, `private`).
- **Resilient Error Handling**:
  - Handles **HTTP 201 Created** and tracks the assigned Ballchasing replay ID.
  - Treats **HTTP 409 Conflict** (duplicate replay) as non-fatal, avoiding duplicate upload attempts.
  - Respects **HTTP 429 Too Many Requests** using `Retry-After` headers and exponential backoff with jitter.
- **Dual Authentication**: Supports both **Epic Games** (refresh token / exchange code) and **Steam** (session ticket + Steam ID 64).
- **Persistent State**: ACID-compliant storage using embedded pure-Go SQLite (`modernc.org/sqlite`) or JSON state files to ensure zero duplicate downloads or uploads across daemon restarts.
- **Lifecycle Management**: Graceful OS signal handling (`SIGINT`, `SIGTERM`), immediate startup execution, single-cycle mode (`--once`), and simulation mode (`--dry-run`).

---

## How to Run

You can run `rl-sync` either using the pre-compiled standalone binary (`rl-sync.exe`) or from source using `go`.

### Option A: Using the Standalone Binary (Recommended)

A pre-built binary `rl-sync.exe` is located in the project root:

```powershell
# 1. View all flags and options
.\rl-sync.exe --help

# 2. Run a dry-run test (queries matches without downloading or uploading)
.\rl-sync.exe -config config.yaml --once --dry-run

# 3. Run a single sync pass and exit
.\rl-sync.exe -config config.yaml --once

# 4. Run the daemon continuously (polls every 5 minutes)
.\rl-sync.exe -config config.yaml
```

---

### Option B: Running from Source (`go run`)

If you have Go installed:

```powershell
# Run single cycle
go run ./cmd/rl-sync -config config.yaml -once

# Run continuous daemon
go run ./cmd/rl-sync -config config.yaml
```

---

## CLI Flags & Options

| Flag | Short | Default | Description |
| :--- | :--- | :--- | :--- |
| `-config` | `-c` | `""` | Path to configuration file (YAML or JSON). |
| `-once` | | `false` | Execute a single sync cycle and exit immediately. |
| `-dry-run` | | `false` | Simulate match detection without downloading files or uploading to Ballchasing. |
| `-poll-interval` | | `5m` | Interval between polling cycles (e.g., `5m`, `1m`, `30s`). |
| `-provider` | | `""` | Override authentication provider (`epic` or `steam`). |
| `-replay-dir` | | `""` | Path to local directory where `.replay` files are saved. |
| `-db-path` | | `""` | Path to SQLite database file or JSON state store file. |
| `-log-level` | | `info` | Logging verbosity (`debug`, `info`, `warn`, `error`). |
| `-log-format` | | `text` | Log output format (`text`, `json`). |
| `-version` | `-v` | `false` | Display application version and exit. |
| `-help` | `-h` | `false` | Display help usage and exit. |

---

## Configuration

Copy the example configuration file to get started:

```powershell
Copy-Item configs\config.example.yaml config.yaml
```

Edit `config.yaml` to specify your authentication and API settings:

```yaml
auth:
  provider: "epic" # "epic" or "steam"

  epic:
    refresh_token: "your-epic-refresh-token"
    # Or bootstrap with a one-time code:
    # auth_code: "your-exchange-code"

  steam:
    session_ticket: "your-hex-session-ticket"
    steam_id_64: "76561198000000000"

ballchasing:
  api_key: "your-ballchasing-api-key"
  visibility: "public" # "public", "unlisted", or "private"
  timeout: "60s"
  max_retries: 3

sync:
  poll_interval: "5m"
  replay_dir: "./replays"
  db_path: "./data/rl-sync.db" # SQLite store
```

### Generating a Steam Session Ticket (`steam/`)

If using Steam authentication (`provider: "steam"`), you can generate a valid Rocket League session ticket using the script provided in the [`steam/`](file:///d:/code/rl-api-utils/steam) folder:

1. **Navigate to the `steam` folder and install dependencies**:
   ```powershell
   cd steam
   npm install
   ```

2. **Set your Steam credentials as environment variables**:
   ```powershell
   $env:USERNAME = "STEAM_USERNAME"
   $env:PASSWORD = "STEAM_PASSWORD"
   ```

3. **Run the ticket generator**:
   ```powershell
   npm run start
   ```

4. **Enter Steam Guard code**:
   When prompted, paste the Steam Guard code received from your email or mobile app into the terminal.

5. **Copy the session ticket**:
   Copy the generated hex ticket from the terminal output and paste it into your `config.yaml` under `auth.steam.session_ticket` (or set `$env:RL_SYNC_STEAM_SESSION_TICKET`). Also ensure your 64-bit Steam ID is configured under `auth.steam.steam_id_64`.


### Environment Variable Overrides

Any configuration field can also be supplied via environment variables:

| Variable | Description |
| :--- | :--- |
| `RL_SYNC_AUTH_PROVIDER` | `epic` or `steam` |
| `RL_SYNC_EPIC_REFRESH_TOKEN` | Epic Games OAuth refresh token |
| `RL_SYNC_EPIC_AUTH_CODE` | Epic Games one-time authorization code |
| `RL_SYNC_STEAM_SESSION_TICKET` | Steam authentication ticket (hex encoded) |
| `RL_SYNC_STEAM_ID_64` | 64-bit Steam ID |
| `RL_SYNC_BALLCHASING_API_KEY` | Ballchasing upload API token |
| `RL_SYNC_BALLCHASING_VISIBILITY`| Upload visibility (`public`, `unlisted`, `private`) |
| `RL_SYNC_POLL_INTERVAL` | Polling frequency (e.g., `5m`) |
| `RL_SYNC_REPLAY_DIR` | Replay destination directory |
| `RL_SYNC_DB_PATH` | Path to persistence database |

---

## Building from Source

To compile the binary yourself:

```powershell
go build -v -o rl-sync.exe ./cmd/rl-sync
```

---

## Running Tests

The project includes unit, integration, boundary, and adversarial test suites:

```powershell
# Run all 376 tests across all 10 packages
go test -v -count=1 ./...

# Run static analysis
go vet ./...

# Run specific E2E test tier
go test -v ./test/e2e -run TestTier1
```

---

## Repository Structure

```
rl-api-utils/
├── cmd/
│   └── rl-sync/          # Application entry point & CLI flag parsing
├── configs/
│   ├── config.example.yaml # Example YAML configuration
│   └── config.example.json # Example JSON configuration
├── internal/
│   ├── auth/             # Epic Games & Steam authentication adapters
│   ├── ballchasing/      # Ballchasing API client (multipart upload & backoff)
│   ├── config/           # Layered configuration loader
│   ├── daemon/           # Periodic runner & signal handling
│   ├── psynet/           # rlapi match history & atomic replay downloader
│   ├── storage/          # SQLite (modernc.org/sqlite) & JSON persistence
│   ├── syncer/           # Core sync loop orchestrator
│   └── testutil/         # Hermetic mock servers (PsyNet, Ballchasing, CDN)
├── test/
│   └── e2e/              # 5-Tier integration & adversarial test suites
├── steam/                # Steam session ticket generation utility (Node.js)
│   ├── package.json
│   └── steam-ticket.js
├── rl-sync.exe           # Standalone executable
├── go.mod
├── go.sum
└── PROJECT.md            # Technical specifications and architecture
```
