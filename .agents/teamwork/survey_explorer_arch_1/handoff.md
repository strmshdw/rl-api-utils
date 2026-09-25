# Technical Architecture & Design Report: Rocket League Replay Synchronizer Daemon

**Target**: Daemon Architecture, Persistence Layer, Configuration, CLI, and Mock Test Harness Design  
**Author**: `survey_explorer_arch_1`  
**Date**: 2026-09-25T03:04:00Z  
**Module**: `github.com/dank/rl-api-utils` (or `rl-api-utils`)  
**Status**: Ready for Implementation  

---

## 1. Observation

Direct investigation of the project requirements, codebase references, and external API specifications revealed the following concrete technical facts:

### 1.1 Requirements & Constraints (`ORIGINAL_REQUEST.md`)
- **R1 Match Polling & Replay Synchronization**: Periodic daemon polling (default 5 minutes) via `github.com/dank/rlapi` (`Matches/GetMatchHistory v1`). Identifies matches with valid replay URLs that have not yet been downloaded, streaming `.replay` binary payloads to a configurable local directory (`ORIGINAL_REQUEST.md:16-17`).
- **R2 Ballchasing.com Replay Uploader**: Uploads `.replay` files to Ballchasing.com (`POST /v2/upload`) via multipart form data (`file` field). Supports user-configured visibility (`public`, `unlisted`, `private`), authentication via API key (`Authorization: <token>`), and handles HTTP 201 (Created), HTTP 409 (duplicate replay), and HTTP 429 (rate limiting with backoff) (`ORIGINAL_REQUEST.md:19-21`).
- **R3 Persistent State & Idempotency**: State persistence (SQLite or structured JSON store) tracking match GUIDs, timestamps, download paths, and Ballchasing upload statuses (with replay IDs). Guarantees already downloaded or uploaded matches are never re-downloaded or re-uploaded across polling cycles and restarts (`ORIGINAL_REQUEST.md:22-24`).
- **R4 Dual Authentication & Configuration**: Supports Epic Games (refresh token / auth code exchange for EOS tokens and `psyNet.AuthPlayer`) and Steam (session ticket exchanged for EOS tokens and `psyNet.AuthPlayerSteam`). Configurable via environment variables and configuration files (`ORIGINAL_REQUEST.md:25-30`).
- **R5 Verification Suite & Mock Harness**: 100% automated test pass via `go test ./...` without live credentials using mocked PsyNet RPC, mock replay downloads, mock Epic & Steam auth, mock Ballchasing endpoints (201, 409, 429, 401), and restart persistence verification (`ORIGINAL_REQUEST.md:32-39, 58-62`).

### 1.2 Upstream SDK Architecture (`github.com/dank/rlapi`)
From inspection of the authoritative source code:
- **Package exports**:
  - `EGS`: `AuthenticateWithCode`, `AuthenticateWithRefreshToken`, `GetExchangeCode`, `ExchangeEOSToken`, `ExchangeEOSTokenFromSteam` (`egs.go:87-200`).
  - `PsyNet`: `AuthPlayer`, `AuthPlayerSteam`, `SetVersion`, `GetVersion` (`psynet.go:69-106`, `auth.go:33-101`).
  - `PsyNetRPC`: `GetMatchHistory(ctx context.Context) ([]MatchEntry, error)`, `IsConnected() bool`, `Close() error`, `Events() <-chan *Event` (`matches.go:73-84`, `psynetrpc.go:65-88`).
  - `MatchEntry`: Contains `ReplayUrl string` and `Match Match` where `MatchGUID string`, `RecordStartTimestamp int64` (`matches.go:5-20`).
- **Network implementation details**:
  - `baseURL`: `const baseURL = "https://api.rlpp.psynet.gg/rpc"` (`psynet.go:21`). Note that `baseURL` is an unexported constant in `rlapi`.
  - `http.Client`: `PsyNet` and `EGS` use `&http.Client{}` which defaults to `http.DefaultTransport`. Wire-level mock testing can redirect traffic cleanly via a custom `http.RoundTripper`.
  - `WebSocket`: `establishSocket` connects to the `PerConURLv2` WebSocket URL returned by `Auth/AuthPlayer/v2` (`psynet.go:110-128`).
  - Mock test reference: `psynetrpc_test.go:19-115` provides a battle-tested `MockWSServer` implementation using `httptest.Server` and `gorilla/websocket.Upgrader`.

### 1.3 Ballchasing.com API Specification (`ballchasing.com/doc/api`)
From inspection of the official Ballchasing API documentation:
- **Endpoint**: `POST https://ballchasing.com/api/v2/upload?visibility={public|unlisted|private}`.
- **Headers**: `Authorization: <token>`, `Content-Type: multipart/form-data; boundary=...`.
- **Form Body**: Single part named `file` with `filename="<guid>.replay"`, binary stream.
- **Status Responses**:
  - **201 Created**: `{"id": "0b4ce8c0-68fa-4a93-8525-cae068c67eee", "location": "https://ballchasing.com/replay/0b4ce8c0-68fa-4a93-8525-cae068c67eee"}`.
  - **409 Conflict**: `{"id": "0b4ce8c0-68fa-4a93-8525-cae068c67eee", "location": "...", "error": "duplicate replay"}`. Both 201 and 409 return the replay ID and are idempotent terminal success states.
  - **429 Too Many Requests**: Returns rate limit error. Requires backoff before retry.
  - **401 Unauthorized**: `{"error": "missing API key"}`.

### 1.4 Persistence & Driver Evaluation
- **Pure Go SQLite (`modernc.org/sqlite`)**:
  - Port of SQLite 3 directly translated to pure Go using `ccgo`.
  - **Zero CGO**: Compiles with `CGO_ENABLED=0`, eliminating external GCC/MinGW dependencies.
  - Registers standard `database/sql` driver name `"sqlite"`.
  - Full ACID transaction support, write-ahead logging (WAL), crash-safe, fast indexing on `match_guid`.
- **CGO-based SQLite (`github.com/mattn/go-sqlite3`)**:
  - Requires GCC/MinGW on Windows and `CGO_ENABLED=1`. Fails to compile on systems without C build tools installed.
- **Structured JSON Store**:
  - Useful as an optional fallback; can be implemented behind the same `StateStore` interface using atomic write-to-temp and `os.Rename`.

---

## 2. Logic Chain

From these direct observations, we trace the architectural reasoning from requirements to concrete module and interface designs:

### 2.1 Module Layout & Clean Architecture Invariants
To satisfy R1, R2, R3, R4, and R5 simultaneously while preventing coupling between business logic and third-party network libraries:
1. **Separation of Concerns**: The core synchronization orchestrator (`syncer`) must not depend directly on concrete `*rlapi.PsyNetRPC` or `*http.Client`. Instead, it must depend on clean interfaces:
   - `MatchHistoryProvider`: Provides match history records.
   - `ReplayDownloader`: Streams binary replay payloads.
   - `ReplayUploader`: Uploads replays to Ballchasing.
   - `StateStore`: Manages persistence and idempotency tracking.
2. **Project Layout**: Standard Go layout conforming to Go standards:
   ```
   rl-api-utils/
   ├── cmd/
   │   └── rl-sync/
   │       └── main.go               # Thin CLI entry point: flags, signals, dependency injection
   ├── internal/
   │   ├── config/
   │   │   ├── config.go             # Typed configuration struct, validation, YAML/JSON, env binding
   │   │   └── config_test.go
   │   ├── daemon/
   │   │   ├── daemon.go             # Lifecycle manager, ticker loop, graceful shutdown, drain
   │   │   └── daemon_test.go
   │   ├── syncer/
   │   │   ├── syncer.go             # Core sync loop: poll -> diff -> download -> upload -> commit
   │   │   ├── interfaces.go         # Domain interfaces (MatchHistoryProvider, ReplayDownloader, etc.)
   │   │   └── syncer_test.go
   │   ├── storage/
   │   │   ├── store.go              # StateStore interface & MatchRecord data model
   │   │   ├── sqlite.go             # modernc.org/sqlite pure Go implementation
   │   │   ├── sqlite_test.go
   │   │   ├── jsonstore.go          # Atomic JSON file fallback implementation
   │   │   └── jsonstore_test.go
   │   ├── auth/
   │   │   ├── provider.go           # AuthProvider interface & token persistence
   │   │   ├── epic.go               # Epic Games auth flow (refresh token/auth code -> EOS -> PsyNet)
   │   │   ├── steam.go              # Steam auth flow (ticket + SteamID64 -> EOS -> PsyNet)
   │   │   └── auth_test.go
   │   ├── psynet/
   │   │   ├── client.go             # rlapi adapter implementing MatchHistoryProvider
   │   │   ├── downloader.go         # HTTP replay streaming with atomic temp file & integrity validation
   │   │   └── downloader_test.go
   │   ├── ballchasing/
   │   │   ├── client.go             # Ballchasing API client implementing ReplayUploader
   │   │   ├── types.go              # Upload request/response structs
   │   │   └── client_test.go
   │   └── testutil/                 # Reusable test harness & mock servers
   │       ├── mock_psynet.go        # Mock PsyNet HTTP & WebSocket RPC server
   │       ├── mock_ballchasing.go   # Mock Ballchasing HTTP server (201, 409, 429, 401)
   │       └── mock_cdn.go           # Mock replay file CDN server
   ├── configs/
   │   ├── config.example.yaml       # Annotated YAML configuration template
   │   └── config.example.json       # JSON configuration template
   ├── go.mod
   ├── go.sum
   └── README.md
   ```

### 2.2 Daemon Lifecycle & Polling Engine
1. **Ticker Loop**:
   - `time.NewTicker(cfg.Sync.PollInterval)` (default 5 minutes).
   - On startup, execute an immediate sync cycle (`syncOnce(ctx)`), then wait for subsequent ticks via `select { case <-ticker.C: ... }`.
2. **Context Propagation & Signal Handling**:
   - `ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`.
   - On signal receipt, `ctx.Done()` is signalled.
   - `daemon.Stop()` stops the ticker immediately to prevent starting new cycles.
3. **Graceful Drain**:
   - In-flight synchronization is protected by a mutex or `sync.WaitGroup`.
   - If a sync cycle is currently active when shutdown is requested:
     - The daemon waits up to a configurable shutdown timeout (e.g. 30 seconds) for in-flight file downloads and uploads to finish and commit their state to SQLite.
     - Ongoing HTTP requests inherit the parent context with timeout, ensuring clean closure without leaking goroutines or leaving corrupted files.
   - Resources closed in defer chain: `rpc.Close()`, `db.Close()`.

### 2.3 CLI Flags & Execution Modes
Standard library `flag` package (or `pflag`) options:
- `--config <path>` / `-c <path>`: Path to YAML or JSON config file (default `"config.yaml"`).
- `--once`: Single-run mode. Executes exactly one synchronization pass, outputs a structured summary, and exits with code 0 (or 1 on unrecoverable failure).
- `--dry-run`: Simulation mode. Discovers matches, calculates diff against local database, logs matches that would be downloaded and uploaded, but skips disk writes, Ballchasing uploads, and state commits. Can be combined with `--once` or daemon mode.
- `--log-level`: Logging verbosity (`debug`, `info`, `warn`, `error`). Configures `log/slog`.
- `--log-format`: Log output format (`text` or `json`).
- `--version`: Prints binary version, commit SHA, and build timestamp.

### 2.4 Configuration Hierarchy & Schema
Configuration adheres to strict priority order:
**CLI Flags > Environment Variables > Config File > Hardcoded Defaults**

#### Struct Definition
```go
package config

import "time"

type Config struct {
    Auth        AuthConfig        `yaml:"auth" json:"auth"`
    Ballchasing BallchasingConfig `yaml:"ballchasing" json:"ballchasing"`
    Sync        SyncConfig        `yaml:"sync" json:"sync"`
    Logging     LoggingConfig     `yaml:"logging" json:"logging"`
}

type AuthConfig struct {
    Provider string      `yaml:"provider" json:"provider"` // "epic" or "steam"
    Epic     EpicConfig  `yaml:"epic" json:"epic"`
    Steam    SteamConfig `yaml:"steam" json:"steam"`
}

type EpicConfig struct {
    RefreshToken string `yaml:"refresh_token" json:"refresh_token"`
    AuthCode     string `yaml:"auth_code" json:"auth_code"`
    AccountID    string `yaml:"account_id" json:"account_id"`
    DisplayName  string `yaml:"display_name" json:"display_name"`
}

type SteamConfig struct {
    SessionTicket string `yaml:"session_ticket" json:"session_ticket"`
    SteamID64     string `yaml:"steam_id_64" json:"steam_id_64"`
    AccountName   string `yaml:"account_name" json:"account_name"`
}

type BallchasingConfig struct {
    APIKey     string        `yaml:"api_key" json:"api_key"`
    Visibility string        `yaml:"visibility" json:"visibility"` // "public", "unlisted", "private"
    BaseURL    string        `yaml:"base_url" json:"base_url"`     // default: "https://ballchasing.com/api"
    Timeout    time.Duration `yaml:"timeout" json:"timeout"`       // default: 60s
    MaxRetries int           `yaml:"max_retries" json:"max_retries"`// default: 3
}

type SyncConfig struct {
    PollInterval    time.Duration `yaml:"poll_interval" json:"poll_interval"`       // default: 5m
    ReplayDir       string        `yaml:"replay_dir" json:"replay_dir"`             // default: "./replays"
    DBPath          string        `yaml:"db_path" json:"db_path"`                   // default: "./rl-sync.db"
    KeepLocalFiles  bool          `yaml:"keep_local_files" json:"keep_local_files"` // default: true
    DownloadTimeout time.Duration `yaml:"download_timeout" json:"download_timeout"` // default: 30s
}

type LoggingConfig struct {
    Level  string `yaml:"level" json:"level"`   // "debug", "info", "warn", "error"
    Format string `yaml:"format" json:"format"` // "text", "json"
}
```

#### Environment Variable Mappings
| Environment Variable | Config Mapping | Default Value |
|----------------------|----------------|---------------|
| `RL_SYNC_AUTH_PROVIDER` | `Auth.Provider` | `"epic"` |
| `RL_SYNC_EPIC_REFRESH_TOKEN` | `Auth.Epic.RefreshToken` | `""` |
| `RL_SYNC_EPIC_AUTH_CODE` | `Auth.Epic.AuthCode` | `""` |
| `RL_SYNC_EPIC_ACCOUNT_ID` | `Auth.Epic.AccountID` | `""` |
| `RL_SYNC_EPIC_DISPLAY_NAME` | `Auth.Epic.DisplayName` | `""` |
| `RL_SYNC_STEAM_SESSION_TICKET` | `Auth.Steam.SessionTicket` | `""` |
| `RL_SYNC_STEAM_ID_64` | `Auth.Steam.SteamID64` | `""` |
| `RL_SYNC_STEAM_ACCOUNT_NAME` | `Auth.Steam.AccountName` | `""` |
| `RL_SYNC_BALLCHASING_API_KEY` | `Ballchasing.APIKey` | `""` |
| `RL_SYNC_BALLCHASING_VISIBILITY` | `Ballchasing.Visibility` | `"public"` |
| `RL_SYNC_BALLCHASING_BASE_URL` | `Ballchasing.BaseURL` | `"https://ballchasing.com/api"` |
| `RL_SYNC_POLL_INTERVAL` | `Sync.PollInterval` | `5m` |
| `RL_SYNC_REPLAY_DIR` | `Sync.ReplayDir` | `"./replays"` |
| `RL_SYNC_DB_PATH` | `Sync.DBPath` | `"./rl-sync.db"` |
| `RL_SYNC_KEEP_LOCAL_FILES` | `Sync.KeepLocalFiles` | `true` |
| `RL_SYNC_LOG_LEVEL` | `Logging.Level` | `"info"` |
| `RL_SYNC_LOG_FORMAT` | `Logging.Format` | `"text"` |

### 2.5 State Persistence & Idempotency Engine
1. **Engine Selection**: Pure Go SQLite (`modernc.org/sqlite`).
   - Rationale: Fully cross-platform, zero CGO, zero gcc requirement on Windows or in CI, full ACID transactional semantics, file-based persistence.
2. **Database Schema (`matches` table)**:
   ```sql
   CREATE TABLE IF NOT EXISTS matches (
       match_guid TEXT PRIMARY KEY,
       record_start_timestamp INTEGER NOT NULL,
       map_name TEXT NOT NULL DEFAULT '',
       playlist INTEGER NOT NULL DEFAULT 0,
       replay_url TEXT NOT NULL DEFAULT '',
       download_status TEXT NOT NULL DEFAULT 'PENDING',  -- PENDING, DOWNLOADING, DOWNLOADED, FAILED
       local_file_path TEXT NOT NULL DEFAULT '',
       downloaded_at INTEGER NULL,                      -- Unix epoch seconds
       upload_status TEXT NOT NULL DEFAULT 'PENDING',    -- PENDING, UPLOADING, UPLOADED, DUPLICATE, FAILED
       ballchasing_id TEXT NOT NULL DEFAULT '',
       ballchasing_url TEXT NOT NULL DEFAULT '',
       uploaded_at INTEGER NULL,                        -- Unix epoch seconds
       retry_count INTEGER NOT NULL DEFAULT 0,
       last_error TEXT NOT NULL DEFAULT '',
       created_at INTEGER NOT NULL,                     -- Unix epoch seconds
       updated_at INTEGER NOT NULL                      -- Unix epoch seconds
   );

   CREATE INDEX IF NOT EXISTS idx_matches_download_status ON matches(download_status);
   CREATE INDEX IF NOT EXISTS idx_matches_upload_status ON matches(upload_status);
   CREATE INDEX IF NOT EXISTS idx_matches_record_ts ON matches(record_start_timestamp DESC);

   -- Token store for headless restart recovery
   CREATE TABLE IF NOT EXISTS auth_state (
       provider TEXT PRIMARY KEY,
       refresh_token TEXT NOT NULL,
       account_id TEXT NOT NULL DEFAULT '',
       display_name TEXT NOT NULL DEFAULT '',
       updated_at INTEGER NOT NULL
   );
   ```

3. **State Machine Transitions**:
   ```
   [Match Discovered from PsyNet]
               │
               ▼
       [Insert PENDING]
               │
      Is ReplayUrl != "" ?
         ├── NO  ──> [Mark Skipped / No Replay Available]
         └── YES
               │
               ▼
       [Check Local Store]
      Already DOWNLOADED & File Exists?
         ├── YES ──> (Skip Download)
         └── NO
               │
               ▼
       [Status: DOWNLOADING]
       Download to <guid>.replay.tmp
       Validate magic bytes ('TAGAME') & size > 0
       Atomic rename to <guid>.replay
               │
               ▼
       [Status: DOWNLOADED]
               │
               ▼
       [Check Upload Store]
      Already UPLOADED or DUPLICATE?
         ├── YES ──> (Skip Upload)
         └── NO
               │
               ▼
       [Status: UPLOADING]
       Multipart POST to /v2/upload
               │
         ┌─────┴───────────────────────┬────────────────────────┐
         ▼                             ▼                        ▼
     HTTP 201                      HTTP 409                 HTTP 429 / 5xx
    (Created)                     (Duplicate)             (Rate Limit / Net Err)
         │                             │                        │
         ▼                             ▼                        ▼
  [Status: UPLOADED]           [Status: DUPLICATE]       [Status: PENDING]
  Record Replay ID             Record Replay ID          Increment retry_count
  Terminal Success             Terminal Success          Backoff & Retry next cycle
   ```

4. **Idempotency Invariant Guarantees**:
   - **Invariant 1: No Duplicate Downloads**:
     Before downloading, query `SELECT download_status, local_file_path FROM matches WHERE match_guid = ?`. If `download_status = 'DOWNLOADED'` AND `os.Stat(local_file_path)` confirms the file exists and is non-empty, download is immediately skipped.
   - **Invariant 2: Atomic File Writing**:
     Replay files are streamed to `<guid>.replay.tmp`. Only after the HTTP body is fully read and flushed is it renamed to `<guid>.replay` via atomic `os.Rename`. If a crash occurs during download, the `.tmp` file is cleaned up on reboot and the match remains `PENDING`.
   - **Invariant 3: No Duplicate Uploads**:
     Before uploading, query `SELECT upload_status FROM matches WHERE match_guid = ?`. If `upload_status IN ('UPLOADED', 'DUPLICATE')`, upload is immediately skipped.
   - **Invariant 4: Duplicate Reconciliation (409 Conflict)**:
     If Ballchasing returns HTTP 409 Conflict, Ballchasing responds with the existing replay's `id` and `location`. The daemon extracts this ID, marks `upload_status = 'DUPLICATE'`, records `ballchasing_id`, and marks the operation complete. It is never retried.
   - **Invariant 5: Crash & Restart Recovery**:
     During startup initialization, the storage layer executes a cleanup query:
     `UPDATE matches SET download_status = 'PENDING' WHERE download_status = 'DOWNLOADING'`
     `UPDATE matches SET upload_status = 'PENDING' WHERE upload_status = 'UPLOADING'`
     This ensures interrupted tasks are safely resumed without duplicate operations.

### 2.6 Mock Test Harness Architecture
To achieve 100% test pass with `go test ./...` in any isolated environment:
1. **Mock PsyNet Server (`testutil.MockPsyNetServer`)**:
   - An `httptest.Server` implementing:
     - `POST /rpc/Auth/AuthPlayer/v2`: Validates HMAC request signatures and headers (`PsyBuildID`, `User-Agent`, `PsyEnvironment`). Responds with `AuthPlayerResponse` returning a WebSocket URL pointing to its own `/ws` endpoint.
     - WebSocket `/ws` endpoint: Upgrades connection using `gorilla/websocket.Upgrader`. Handles `PsyPing:` frames by responding with `PsyPong:\r\n\r\n`. Handles `Matches/GetMatchHistory v1` frames by replying with the pre-configured slice of `MatchEntry` objects.
     - Dynamic match injection: `server.SetMatches(matches []rlapi.MatchEntry)` allows test suites to mutate the match list between polling ticks to simulate new matches appearing dynamically.
2. **Mock Ballchasing Server (`testutil.MockBallchasingServer`)**:
   - An `httptest.Server` implementing:
     - `POST /v2/upload`:
       - Checks `Authorization: <expected_api_key>` (returns 401 if missing/invalid).
       - Validates `visibility` query parameter (`public`, `unlisted`, `private`).
       - Parses multipart form data with form field `file`.
       - Returns status based on test configuration:
         - Success: HTTP 201 Created with JSON `{"id": "<uuid>", "location": "https://ballchasing.com/replay/<uuid>"}`.
         - Duplicate: HTTP 409 Conflict with JSON `{"id": "<uuid>", "location": "...", "error": "duplicate replay"}`.
         - Rate Limit: HTTP 429 with header `Retry-After: 1` on call N, followed by 201 on call N+1.
         - Server Error: HTTP 500.
       - Records uploaded payloads and invocation counts for programmatic assertion.
3. **Mock Replay CDN Server (`testutil.MockCDNServer`)**:
   - An `httptest.Server` that serves dummy `.replay` binary payloads (e.g. 512 bytes with Rocket League replay header bytes `TAGAME`).
4. **Automated Verification Test Matrix**:
   - `TestDaemon_FullSyncPipeline_Epic`: Complete pipeline from Epic auth -> match history -> CDN download -> Ballchasing upload -> SQLite persistence.
   - `TestDaemon_FullSyncPipeline_Steam`: Complete pipeline using Steam ticket exchange.
   - `TestDaemon_MultiCyclePolling`: Cycle 1 receives Match A (assert 1 download, 1 upload). Cycle 2 receives Match A + Match B (assert 0 re-downloads for A, 0 re-uploads for A, exactly 1 download for B, 1 upload for B). Total uploaded = 2.
   - `TestDaemon_RestartPersistence`: Run cycle 1 with Match A on Daemon 1. Stop Daemon 1. Instantiate Daemon 2 pointing to the same SQLite file. Run cycle 2 on Daemon 2. Assert zero new downloads and zero new uploads.
   - `TestDaemon_BallchasingDuplicate409`: Verify HTTP 409 Conflict sets status `DUPLICATE` and stores replay ID without failing the cycle.
   - `TestDaemon_BallchasingRateLimit429`: Verify HTTP 429 triggers exponential backoff or honors `Retry-After` header and succeeds on retry.
   - `TestDaemon_DryRunMode`: Verify `--dry-run` executes diffing logic without creating files or invoking the Ballchasing upload endpoint.
   - `TestDaemon_GracefulShutdown`: Verify SIGINT context cancellation stops ticker, awaits in-flight sync, and closes SQLite without deadlock.

---

## 3. Caveats

1. **`rlapi` Unexported `baseURL` Constant**:
   In `github.com/dank/rlapi`, `const baseURL = "https://api.rlpp.psynet.gg/rpc"` is private to package `rlapi`. 
   - *Design Mitigation*: We decouple the synchronizer via the `MatchHistoryProvider` interface (`internal/syncer/interfaces.go`). For unit and multi-cycle daemon tests, tests use mock implementations of `MatchHistoryProvider`. For full wire-level integration tests of the `rlapi` adapter itself, tests override `http.DefaultTransport` with a custom `http.RoundTripper` that redirects requests destined for `api.rlpp.psynet.gg` to the local `MockPsyNetServer`.
2. **Steam Session Ticket Expiration**:
   Unlike Epic refresh tokens which can be persisted and renewed indefinitely, Steam session tickets have limited lifetimes and require a running Steam client or external ticket generator to refresh.
   - *Design Mitigation*: The daemon persists the Epic Account ID and linked EOS token when using Steam, and returns a clean, structured log message when the Steam session ticket expires rather than panicking.
3. **Ballchasing Rate Limits**:
   Free/standard Ballchasing API keys are limited to 2 requests/second and 500 requests/hour.
   - *Design Mitigation*: The Ballchasing client incorporates an internal token bucket rate limiter (e.g. 1 request per second) and exponential backoff on HTTP 429 to prevent hitting tier limits when syncing large batches of replays.
4. **Pure Go SQLite Driver Compilation**:
   `modernc.org/sqlite` is 100% CGO-free, but adds ~15MB to the binary size due to the translated SQLite C-to-Go runtime. This is an intentional and highly desirable tradeoff because it guarantees zero-dependency native builds on Windows, Linux, and macOS without requiring MinGW or GCC.

---

## 4. Conclusion

The technical architecture for the `rl-api-utils` Rocket League daemon is fully specified:
1. **Clean Architecture**: Interfaces isolate orchestration from network protocols (`MatchHistoryProvider`, `ReplayDownloader`, `ReplayUploader`, `StateStore`).
2. **Robust Lifecycle**: Dual-mode CLI (Daemon vs `--once` vs `--dry-run`), polling ticker with 5m default, OS signal trap (`SIGINT`, `SIGTERM`), and graceful drain before shutdown.
3. **Flexible Configuration**: Layered hierarchy supporting YAML/JSON configuration files, environment variables (`RL_SYNC_*`), and CLI flags with sensible defaults.
4. **Idempotency & Persistence**: Pure Go SQLite (`modernc.org/sqlite`) state engine tracking match GUID, download path, upload status, and Ballchasing replay ID. Atomic `.tmp` file downloads, HTTP 409 duplicate reconciliation, and startup crash recovery guarantee zero duplicate downloads and zero duplicate uploads.
5. **Hermetic Mock Harness**: Self-contained mock PsyNet HTTP/WebSocket, mock Ballchasing, and mock CDN test servers enabling 100% test pass with `go test ./...` in seconds with zero network dependencies.

---

## 5. Verification Method

To independently verify the architectural specifications and design documents:

1. **Verify Upstream SDK Dependencies & Types**:
   Inspect the upstream `github.com/dank/rlapi` codebase:
   - `matches.go`: Verify `MatchEntry`, `Match`, and `GetMatchHistory` signatures.
   - `auth.go`: Verify `AuthPlayer` and `AuthPlayerSteam` parameters.
   - `egs.go`: Verify `ExchangeEOSToken` and `ExchangeEOSTokenFromSteam`.
   - `psynetrpc_test.go`: Verify `MockWSServer` implementation using `gorilla/websocket`.
2. **Verify Ballchasing API Specifications**:
   Inspect `https://ballchasing.com/doc/api` or reference document:
   - Confirm `POST /v2/upload` with query parameter `visibility`.
   - Confirm HTTP 201 Created returns `{"id": "...", "location": "..."}`.
   - Confirm HTTP 409 Conflict returns `{"id": "...", "location": "...", "error": "duplicate replay"}`.
   - Confirm HTTP 429 Too Many Requests rate limiting behavior.
3. **Verify Implementation Plan**:
   Once implemented in subsequent milestones:
   - Run `go test -v -race ./...` to verify 100% pass across all unit, integration, multi-cycle, and restart persistence tests.
   - Build binary with `go build -o bin/rl-sync.exe ./cmd/rl-sync` and test `--help`, `--dry-run`, and `--once` execution flags.
