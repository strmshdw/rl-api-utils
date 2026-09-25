# E2E Test Infrastructure & Methodology Specification (TEST_INFRA)

## 1. Overview & Test Philosophy

The Rocket League Replay Synchronizer Daemon (`rl-api-utils`) is a mission-critical, long-running service that operates autonomously to query match histories from Rocket League's PsyNet RPC service, download raw `.replay` binary payloads from pre-signed CDN endpoints, and synchronize them to the Ballchasing.com API.

Because the system interacts with external, proprietary, and rate-limited third-party APIs (Epic Games EOS, Rocket League PsyNet WebSocket RPC, and Ballchasing.com REST API), live credentials and production endpoints cannot be used during automated testing or CI runs. Therefore, the testing philosophy of this project is governed by four core tenets:

1. **Dual Track Autonomous Verification**:
   The E2E testing infrastructure and suites operate as a parallel track to implementation milestones. Test suites define behavioral contracts across four rigorous testing tiers (Tier 1 Feature Coverage, Tier 2 Boundary & Corner Cases, Tier 3 Pairwise Interactions, and Tier 4 Real-World Workloads) without depending on the internal private implementation details of individual modules.
2. **Hermetic & Deterministic Simulation**:
   Tests run 100% offline using high-fidelity in-memory HTTP and WebSocket mock servers (`httptest.Server`). No external network requests are made, eliminating network flakiness, latency, and reliance on upstream service availability.
3. **Idempotency & Crash-Safety Guarantees**:
   The primary failure mode of synchronization daemons is duplicate downloads or duplicate uploads caused by unexpected restarts, network drops, or rate limit throttling. All test tiers enforce strict invariants: zero duplicate downloads, zero duplicate uploads, correct recovery of in-flight states, and terminal status handling (such as HTTP 409 duplicate responses).
4. **Standard Go Tooling**:
   The entire test infrastructure compiles and executes using standard Go toolchain commands (`go test ./...`) with full race-detector support (`-race`).

---

## 2. Test Architecture & Mock Infrastructure

The testing architecture is divided into two primary directories:
- `internal/testutil/`: Reusable mock network servers and test harness utilities.
- `test/e2e/`: Four-tier test suites exercising features, boundaries, pairwise interactions, and long-running multi-cycle daemon workloads.

```
rl-api-utils/
├── internal/
│   └── testutil/
│       ├── mock_psynet.go        # Mock PsyNet HTTP auth & WebSocket RPC server
│       ├── mock_ballchasing.go   # Mock Ballchasing HTTP server (201, 409, 429, 401)
│       └── mock_cdn.go           # Mock Replay CDN server (.replay binary payloads)
└── test/
    └── e2e/
        ├── e2e_test.go           # Shared harness, test fixtures, and environment setup
        ├── tier1_feature_test.go # Tier 1: Isolated feature verification (>=5 tests/feat)
        ├── tier2_boundary_test.go# Tier 2: Boundary, corner & stress cases (>=5 tests/area)
        ├── tier3_pairwise_test.go# Tier 3: Cross-feature pairwise interactions
        └── tier4_workload_test.go# Tier 4: Multi-cycle polling, restart idempotency & soak
```

### 2.1 Mock PsyNet Server (`internal/testutil/mock_psynet.go`)
- **HTTP REST Bootstrap**:
  - Intercepts `POST /rpc/Auth/AuthPlayer/v2`.
  - Validates request headers (`PsyBuildID`, `User-Agent`, `PsyEnvironment`).
  - Verifies or accepts the authentication payload and returns a JSON `AuthPlayerResponse` containing `PerConURLv2` pointing to the mock server's local WebSocket endpoint (`/ws`), along with synthetic `PsyToken` and `SessionID`.
- **WebSocket RPC Framing**:
  - Upgrades connection via `gorilla/websocket.Upgrader`.
  - Implements the text-delimited PsyNet wire protocol:
    ```http
    <HeaderKey>: <HeaderValue>\r\n
    \r\n
    <JSON_PAYLOAD>
    ```
  - Handles `PsyPing:\r\n\r\n` heartbeat frames and immediately responds with `PsyPong:\r\n\r\n`.
  - Handles RPC requests for `PsyService: Matches/GetMatchHistory v1`, parsing `PsyRequestID` and replying with:
    ```http
    PsyTime: <unix_ts>\r\nPsySig: <mock_sig>\r\nPsyResponseID: <req_id>\r\n\r\n{"Result":{"Matches":[...]}}
    ```
- **Dynamic Match Injection**:
  - Provides thread-safe methods (`SetMatches`, `AddMatch`, `ClearMatches`, `GetRequestCount`) allowing test suites to inject new matches dynamically between polling cycles to simulate games completing over time.

### 2.2 Mock Ballchasing Server (`internal/testutil/mock_ballchasing.go`)
- **Endpoint**: `POST /v2/upload`.
- **Authentication**:
  - Inspects `Authorization` header.
  - Strictly requires the raw API key without `Bearer ` prefix (e.g. `Authorization: test-api-key`).
  - If header is missing or contains `Bearer `, immediately returns `HTTP 401 Unauthorized` with `{"error": "Invalid API key."}`.
- **Multipart Form Validation**:
  - Parses `multipart/form-data` verifying the presence of the form field `"file"`.
  - Reads and measures uploaded payload bytes.
  - Validates query parameter `visibility` (`public`, `unlisted`, `private`).
- **Programmable Responses**:
  - **201 Created**: Returns JSON `{"id": "<uuid>", "location": "https://ballchasing.com/replay/<uuid>"}`.
  - **409 Conflict**: Returns JSON `{"error": "duplicate replay", "id": "<existing_uuid>", "location": "https://ballchasing.com/replay/<existing_uuid>"}`.
  - **429 Too Many Requests**: Returns JSON `{"error": "too many requests"}` with `Retry-After: <seconds>` header. Supports configurable rate-limit policies (e.g. fail first N requests, then succeed).
  - **401 Unauthorized**: Simulates invalid or revoked API credentials.
  - **400 Bad Request**: Simulates corrupt replay payloads or missing form files.
  - **500 / 503 Internal Error**: Simulates transient gateway or upstream outages.
- **Inspection Metrics**:
  - Records all received uploads, headers, query parameters, file contents, and call counts for programmatic verification in assertions.

### 2.3 Mock Replay CDN Server (`internal/testutil/mock_cdn.go`)
- **Endpoint**: `GET /replays/{guid}.replay`.
- **Payload Generation**:
  - Serves valid Rocket League `.replay` binary payloads.
  - Generates binary payload prefixed with the standard Rocket League header magic bytes (`TAGAME\x00\x00\x00\x00`) followed by deterministic or configured byte streams (default 4KB to 1MB).
- **Fault Injection**:
  - Configurable to return `404 Not Found` (purged replays).
  - Configurable to return `403 Forbidden` / `410 Gone` (expired signed URLs).
  - Configurable to truncate streams prematurely (connection abort simulation).
  - Configurable to return 0-byte or corrupted payloads.
  - Records download access counts per match GUID.

---

## 3. Four-Tier Test Suite Specification

### Tier 1: Feature Coverage (>=5 Test Cases Per Feature)

Tier 1 exercises every functional unit and requirement in isolation with at least 5 distinct test cases per feature:

| Feature # | Feature Name | Test Cases (Minimum 5 Each) |
|---|---|---|
| **F1** | Pure Go SQLite Store | 1. Initialize schema & indexes<br>2. Upsert new matches into store<br>3. Query match by GUID<br>4. List pending downloads filter<br>5. List pending uploads filter<br>6. Save & retrieve auth tokens |
| **F2** | Idempotency & Crash Recovery | 1. Prevent duplicate download transition<br>2. Prevent duplicate upload transition<br>3. Recover in-flight DOWNLOADING to PENDING<br>4. Recover in-flight UPLOADING to PENDING<br>5. Safe re-insertion of already processed GUIDs |
| **F3** | Layered Configuration | 1. Default config population<br>2. Load YAML config file<br>3. Load JSON config file<br>4. Environment variable overrides (`RL_SYNC_*`)<br>5. CLI flag precedence over file/env<br>6. Validation of required fields & ranges |
| **F4** | Epic Games Authentication | 1. Authenticate with valid refresh token<br>2. Exchange auth code for tokens<br>3. Exchange EOS token for Rocket League scope<br>4. Error handling on expired refresh token<br>5. Error handling on invalid authorization code |
| **F5** | Steam Authentication | 1. Exchange Steam session ticket for EOS token<br>2. Authenticate Steam player with SteamID64<br>3. Rejection of malformed session ticket<br>4. Unlinked Steam account handling<br>5. Persistence of Steam linked credentials |
| **F6** | Match History Polling | 1. Successful GetMatchHistory returning entries<br>2. Parsing match metadata (timestamps, map, playlist)<br>3. Extracting player stats & MMR skills<br>4. Handling empty match history list<br>5. Filtering matches with valid vs empty ReplayUrl |
| **F7** | Atomic Replay Downloader | 1. Download valid replay to target directory<br>2. Atomic write via `.tmp` staging and rename<br>3. Validation of replay size (>1KB)<br>4. Validation of `TAGAME` header magic bytes<br>5. Immediate removal of `.tmp` file on download error |
| **F8** | Ballchasing Multipart Upload | 1. Multipart form encoding with `file` part<br>2. Upload with `visibility=public`<br>3. Upload with `visibility=unlisted`<br>4. Upload with `visibility=private`<br>5. Upload with optional `group` ID parameter |
| **F9** | Ballchasing Raw Auth Header | 1. Standard raw token header (`Authorization: <key>`)<br>2. Rejection of `Bearer <key>` with 401<br>3. Rejection of empty/missing authorization<br>4. Dynamic token update verification<br>5. Special character token handling |
| **F10** | Ballchasing HTTP 201 Handling | 1. Parse JSON response `id` and `location`<br>2. Transition status to `UPLOADED`<br>3. Record `ballchasing_id` in database<br>4. Record `ballchasing_url` in database<br>5. Update `uploaded_at` timestamp |
| **F11** | Ballchasing HTTP 409 Deduplication | 1. Parse duplicate replay JSON response<br>2. Extract existing replay `id` and `location`<br>3. Transition status to `DUPLICATE`<br>4. Ensure no error is returned to caller<br>5. Verify no retry is attempted |
| **F12** | Ballchasing HTTP 429 Rate Limiting | 1. Parse `Retry-After` header (seconds)<br>2. Execute backoff delay before retry<br>3. Successful upload on retry after 429<br>4. Exponential backoff when `Retry-After` is missing<br>5. Retry budget exhaustion after max attempts |
| **F13** | Ballchasing HTTP 401 & Permanent Errors | 1. Detect 401 Unauthorized and abort retries immediately<br>2. Mark status as permanent failure<br>3. Detect 400 Bad Request and abort retries<br>4. Descriptive error message propagation<br>5. Prevent daemon crash on permanent upload error |
| **F14** | Syncer Domain Orchestrator | 1. Diff new matches against state store<br>2. Trigger download only for undownloaded matches<br>3. Trigger upload only for downloaded unuploaded matches<br>4. Batch processing of multiple matches<br>5. Context cancellation aborts in-flight sync loop |
| **F15** | Daemon Engine & Lifecycle | 1. Immediate initial sync run on startup<br>2. Scheduled tick execution (5m ticker)<br>3. OS signal trapping (SIGINT / SIGTERM)<br>4. Graceful drain of in-flight sync cycle<br>5. Clean resource teardown on shutdown |
| **F16** | CLI Interface & Execution Modes | 1. `--once` flag executes exactly one cycle and exits<br>2. `--dry-run` flag identifies diffs without downloading/uploading<br>3. Combined `--once` and `--dry-run`<br>4. `--config` flag loads custom file path<br>5. `--log-level` and `--log-format` flag handling |
| **F17** | Structured Logging | 1. Text log format output via `log/slog`<br>2. JSON log format output via `log/slog`<br>3. Contextual attributes (match_guid, replay_url, status)<br>4. Log level filtering (DEBUG, INFO, WARN, ERROR)<br>5. Sensitive token redaction in logs |

---

### Tier 2: Boundary & Corner Cases (>=5 Test Cases Per Area)

Tier 2 exposes the system to extreme conditions, boundary limits, and malformed inputs:

1. **Replay URL & Payload Anomalies**:
   - `ReplayUrl == ""` (match ended early or forfeited; skipped cleanly without download attempt).
   - `ReplayUrl` with spaces, special characters, or query parameters.
   - 0-byte replay file returned by CDN (rejected by size validation, `.tmp` deleted).
   - Replay file smaller than 1KB (rejected by size validation).
   - Replay file without `TAGAME` header (detected as invalid replay binary).
   - CDN returns HTTP 403 Forbidden / 410 Gone (expired pre-signed URL; marked FAILED).
   - Network connection dropped halfway through download (premature EOF; `.tmp` deleted, remains PENDING).

2. **Ballchasing API Boundary Conditions**:
   - HTTP 429 with non-integer `Retry-After` (fallback to exponential backoff).
   - HTTP 429 with `Retry-After: 0` (immediate retry).
   - HTTP 429 repeatedly returned until retry limit exhausted (deferred to next cycle).
   - Replay file larger than 10MB (streamed upload without memory exhaustion).
   - Malformed JSON returned on 201 or 409 (graceful parsing error).
   - Server returns 502/503/504 Bad Gateway (treated as transient, retried with backoff).

3. **Storage & Concurrency Boundaries**:
   - Match GUID with 128 characters or special unicode characters.
   - Massive batch of 1,000 matches returned in a single history poll.
   - Replay destination directory does not exist (`os.MkdirAll` creates it automatically).
   - Read-only directory permissions (returns clear file system error).
   - Concurrent calls to state store methods (verified data-race free via `-race`).

4. **Configuration Boundaries**:
   - Empty configuration file (all defaults applied safely).
   - Invalid poll interval (`0s` or `-5m` rejected by validation).
   - Invalid visibility string (`"super-secret"` rejected by validation).
   - Corrupt YAML / JSON syntax (returns clear syntax error).
   - Non-existent config file path (returns clear not-found error).

---

### Tier 3: Cross-Feature Interactions (Pairwise Combinations)

Tier 3 validates emergent behaviors when two or more distinct features interact:

1. **Epic Auth + Dry-Run Mode**:
   - Authenticates via Epic Games OAuth mock.
   - Polls match history from mock PsyNet server.
   - Computes diff against SQLite database.
   - Verifies zero files created in replay directory and zero upload requests sent to Ballchasing.
2. **Steam Auth + Duplicate Replay (HTTP 409)**:
   - Authenticates via Steam session ticket mock.
   - Polls match history and downloads replay from CDN.
   - Uploads to Ballchasing which returns HTTP 409 Conflict with existing replay ID.
   - Verifies status is committed to SQLite as `DUPLICATE` with the existing Ballchasing ID, without reporting an error.
3. **HTTP 429 Rate Limiting + Daemon Graceful Shutdown**:
   - Daemon encounters HTTP 429 during upload and begins backoff sleep.
   - OS interrupt signal (SIGINT) is received during the sleep.
   - Verifies backoff sleep terminates promptly, in-flight state is preserved in SQLite, and daemon drains and exits cleanly within timeout.
4. **Multi-Match Batch with Mixed Outcomes**:
   - Single polling cycle receives 4 matches:
     - Match 1: normal replay -> downloads & uploads (201 Created).
     - Match 2: already on Ballchasing -> downloads & uploads (409 Conflict -> DUPLICATE).
     - Match 3: rate limited -> receives 429, retries, succeeds (201 Created).
     - Match 4: cancelled match -> empty `ReplayUrl` -> skipped.
   - Verifies all 4 matches end in their correct respective states in SQLite.
5. **Crash During DOWNLOADING + Startup Recovery**:
   - Match is marked `DOWNLOADING` in SQLite, and process is aborted (simulated crash).
   - New daemon instance starts up against the same database.
   - Startup recovery query resets `DOWNLOADING` to `PENDING`.
   - Polling resumes and match is downloaded and uploaded successfully.
6. **Crash During UPLOADING + Startup Recovery**:
   - Match is marked `UPLOADING` in SQLite, and process is aborted.
   - New daemon instance starts up against the same database.
   - Startup recovery query resets `UPLOADING` to `PENDING`.
   - Polling resumes and match is uploaded successfully without re-downloading.
7. **Single-Run (`--once`) + Custom Visibility & Group**:
   - CLI executed with `--once`, `--visibility private`, and custom group ID.
   - Runs single sync pass, applies custom query parameters to Ballchasing upload, commits to SQLite, and terminates with exit code 0.
8. **JSON Store Fallback + Atomic Staging**:
   - Daemon configured with structured JSON state store instead of SQLite.
   - Full pipeline executes: poll, download, upload, atomic JSON save.
   - Verified that JSON state matches SQLite data model semantics.

---

### Tier 4: Real-World Application Workload Scenarios

Tier 4 tests full, multi-stage realistic workloads simulating hours or days of live operations:

1. **Multi-Cycle Polling with Dynamic Match Progression**:
   - **Cycle 1**: Mock PsyNet returns Match A and Match B. Both are downloaded from CDN and uploaded to Ballchasing.
     *Assertion*: CDN served 2 files; Ballchasing received 2 uploads; SQLite records 2 `UPLOADED` records.
   - **Cycle 2**: Mock PsyNet returns Match A, Match B, Match C, and Match D.
     *Assertion*: CDN served exactly 2 additional files (C and D); Ballchasing received exactly 2 additional uploads; zero re-downloads and zero re-uploads for A and B. Total uploads: 4.
   - **Cycle 3**: Mock PsyNet returns Match A, Match B, Match C, and Match D (no new matches).
     *Assertion*: CDN served 0 additional files; Ballchasing received 0 additional uploads. Total uploads remain 4.

2. **Cold Restart Persistence & Idempotency**:
   - **Phase 1**: Daemon Instance 1 initializes SQLite file on disk. Polls 3 matches, downloads all 3, uploads all 3 to Ballchasing. Daemon Instance 1 is cleanly stopped.
   - **Phase 2**: Daemon Instance 2 initializes pointing to the same SQLite file on disk. Polls the exact same 3 matches from PsyNet.
   - *Assertion*: Zero requests sent to CDN; zero requests sent to Ballchasing; all 3 matches immediately recognized as already processed; zero duplicate uploads.

3. **Transient CDN Network Outage & Recovery**:
   - **Cycle 1**: CDN is configured to fail (HTTP 500 / connection dropped). Match download fails and is recorded as `FAILED`.
   - **Cycle 2**: CDN is restored to healthy state. Syncer retries failed download, streams file successfully, and completes Ballchasing upload.

4. **Ballchasing Burst Rate-Limiting & Self-Healing**:
   - Batch of 5 matches queued for upload. Mock Ballchasing server enforces a strict rate limit: returns HTTP 429 with `Retry-After: 1` on every 2nd request.
   - The synchronizer transparently honors the backoff, retries, and successfully uploads all 5 replays without dropping any match or aborting the daemon.

5. **Extended Multi-Cycle Soak Simulation**:
   - 10 consecutive simulated polling cycles with dynamic match creation, varied playlists, duplicate matches, and intermittent network hiccups.
   - Verifies zero memory leaks, zero file descriptor leaks, zero orphaned `.tmp` files, and database integrity across all cycles.

---

## 4. Test Execution & Coverage Verification

All tests are executable via standard Go commands without external services:

```bash
# Run all tests (unit, integration, and E2E)
go test -v ./...

# Run all E2E tests specifically
go test -v ./test/e2e/...

# Run with race detector enabled
go test -v -race ./test/e2e/...

# Run specific tiers
go test -v -run "TestTier1" ./test/e2e/...
go test -v -run "TestTier2" ./test/e2e/...
go test -v -run "TestTier3" ./test/e2e/...
go test -v -run "TestTier4" ./test/e2e/...
```
