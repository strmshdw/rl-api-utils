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
