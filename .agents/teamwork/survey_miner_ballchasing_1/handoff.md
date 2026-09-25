# Specification & API Mining Report: Ballchasing.com API & PsyNet Replay Downloading

**Target**: Ballchasing.com API v2 and PsyNet Replay Binary Downloading  
**Author**: `survey_miner_ballchasing_1`  
**Date**: 2026-09-25T03:06:00Z  

---

## 1. Observation

Direct investigation of authoritative specifications, reference implementations (`Amzd/upload-match-history-to-ballchasing`, `Midnight679/kickoff-cloud-sync`, `bakkesmodorg/AutoReplayUploader`, `tom-boyes-park/ballchaser`, `deweller/rl-stat-collector-discord-bot`), and official Ballchasing documentation revealed the following interface contracts, endpoint semantics, headers, response schemas, and download mechanics:

### 1.1 Ballchasing API Endpoints & Base URL
- **Base API URL**: `https://ballchasing.com/api`
- **Replay Upload Endpoint**: `POST https://ballchasing.com/api/v2/upload`
  *(Note: In documentation and clients, occasionally referenced relative to `/api` as `/v2/upload`).*
- **Ping / Token Validation Endpoint**: `GET https://ballchasing.com/api/`
- **Replay Metadata Endpoint**: `GET https://ballchasing.com/api/replays/<id>`
- **Replay File Download Endpoint**: `GET https://ballchasing.com/api/replays/<id>/file`
- **Replay Patch Endpoint**: `PATCH https://ballchasing.com/api/replays/<id>`
- **Groups Endpoint**: `POST https://ballchasing.com/api/groups`

### 1.2 Authentication Header Specification
- **Header Name**: `Authorization`
- **Header Format**: Raw token string directly without prefix:
  ```http
  Authorization: <token>
  ```
- **Crucial Negative Observation**: Adding the `Bearer ` prefix (e.g. `Authorization: Bearer <token>`) causes Ballchasing API to reject the request with HTTP 401 Unauthorized. The API strictly expects the raw token string.
- **Verification Endpoint**: `GET https://ballchasing.com/api/`
  - Valid token returns `200 OK` with JSON: `{"chaser": true, "type": "regular", "name": "...", "steam_id": "..."}`.
  - Invalid token returns `401 Unauthorized` with JSON: `{"error": "Invalid API key."}`.

### 1.3 `POST /v2/upload` Endpoint Specification

#### Request Structure
- **HTTP Method**: `POST`
- **URL**: `https://ballchasing.com/api/v2/upload`
- **Query Parameters**:
  - `visibility` (*optional/configured*): Sets access permissions. Valid values:
    - `"public"`: Fully public; indexable and searchable in global search.
    - `"unlisted"`: Accessible only via direct URL link or parent group; hidden from public search.
    - `"private"`: Accessible only by the authenticated account that uploaded the file.
    - If omitted, defaults to the user's account preference configured at `https://ballchasing.com/upload`.
  - `group` (*optional*): ID of a replay group to which the replay should be added upon upload.
- **Headers**:
  - `Authorization: <token>`
  - `Content-Type: multipart/form-data; boundary=<boundary_string>`
  - Optional: `User-Agent: <client_name>/<version>`
- **Multipart Form Payload**:
  - Exactly one form file field named `"file"`.
  - Form field header:
    ```http
    Content-Disposition: form-data; name="file"; filename="<matchGUID>.replay"
    Content-Type: application/octet-stream
    ```
  - Form part body: Raw binary payload of the Rocket League `.replay` file.

#### Response Status Codes & Schemas

##### 1. HTTP 201 Created (Upload Succeeded)
Returned when the replay is valid, accepted, and recorded as a new replay resource on Ballchasing.
- **HTTP Headers**:
  - `Location: https://ballchasing.com/replay/<replay_id>`
  - `Content-Type: application/json`
- **Response JSON Schema**:
  ```json
  {
    "id": "e9b72942-d6b3-4f24-9bdf-f8b7f8c09a32",
    "location": "https://ballchasing.com/replay/e9b72942-d6b3-4f24-9bdf-f8b7f8c09a32"
  }
  ```

##### 2. HTTP 409 Conflict (Duplicate Replay)
Returned when the replay has already been uploaded to Ballchasing.com (either by the same user previously, or by another player who participated in the same match).
- **HTTP Headers**:
  - `Content-Type: application/json`
- **Response JSON Schema**:
  ```json
  {
    "error": "duplicate replay",
    "id": "e9b72942-d6b3-4f24-9bdf-f8b7f8c09a32",
    "location": "https://ballchasing.com/replay/e9b72942-d6b3-4f24-9bdf-f8b7f8c09a32"
  }
  ```
- **Key Observation**: Even though HTTP 409 is a 4xx status code, the response body includes the `id` and `location` of the existing replay. The upload pipeline must treat 409 as a non-fatal, successful deduplication event, recording the match as uploaded/duplicate with the returned `id`.

##### 3. HTTP 429 Too Many Requests (Rate Limiting)
Returned when request frequency exceeds server rate limits or account upload quotas.
- **HTTP Headers**:
  - `Retry-After: <seconds>` (e.g. `Retry-After: 5` or `Retry-After: 60`, or HTTP-date RFC 1123).
  - Optional rate limit tracking headers: `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`.
  - `Content-Type: application/json`
- **Response JSON Schema**:
  ```json
  {
    "error": "too many requests"
  }
  ```
- **Rate Limit Tiers & Rules**:
  - Free/unsupported accounts: baseline rate limit is approximately 1 to 2 requests per second burst.
  - Patreon tiers (Gold, Diamond, Champion, GC) grant elevated rate limits and higher daily/weekly upload quotas.
  - Rate limiting occurs at two levels:
    1. Short-term burst/pacing limit (requests/sec) -> resolved by immediate backoff of several seconds.
    2. Daily/weekly upload quota exhaustion -> resolved only when quota resets.

##### 4. HTTP 401 Unauthorized (Invalid Authentication)
Returned when the `Authorization` header is missing, token is invalid, token has been revoked, or the `Bearer ` prefix was erroneously added.
- **HTTP Headers**:
  - `Content-Type: application/json`
- **Response JSON Schema**:
  ```json
  {
    "error": "Invalid API key."
  }
  ```
- **Behavior**: Unrecoverable without configuration change. Must fail immediately without retrying.

##### 5. HTTP 400 Bad Request (Malformed Request)
Returned when the `"file"` form part is missing, payload is 0 bytes, or payload is not a valid replay binary.
- **Response JSON Schema**:
  ```json
  {
    "error": "<error_message>"
  }
  ```
- **Behavior**: Permanent client error for that specific file. Do not retry.

---

### 1.4 PsyNet Replay Binary Downloading Mechanics

Inspecting `matches.go` in `github.com/dank/rlapi` and reference daemon implementations (`Amzd/upload-match-history-to-ballchasing:main.go:58-79`):

#### 1. Replay URL Acquisition
- `Matches/GetMatchHistory v1` returns `[]MatchEntry`.
- Each `MatchEntry` contains:
  ```go
  type MatchEntry struct {
      ReplayUrl string `json:"ReplayUrl"`
      Match     Match  `json:"Match"`
  }
  ```
- `Match.MatchGUID`: string UUID (e.g. `8E41C47444F0744D68DF6F853D431102`).
- `ReplayUrl`: Pre-signed HTTP/HTTPS URL provided by PsyNet / CloudFront / S3 CDN.
  Example: `http://api.rlpp.psynet.gg/Match.replay?MatchGUID=8E41C47444F0744D68DF6F853D431102&Timestamp=...&Expiration=...&Signature=...`

#### 2. Replay Download Protocol
- **HTTP Method**: Standard `GET` request to `ReplayUrl`.
- **Request Headers**: Authentication is embedded within the query parameters (signed URL). Standard headers like `User-Agent: RL Win/<gameVersion> gzip` or `User-Agent: Go-http-client/1.1` are accepted.
- **Response Validation**:
  - Must return `200 OK`.
  - HTTP 403 / 410 indicates the pre-signed URL has expired.
  - HTTP 404 indicates the replay has been purged from PsyNet storage.
  - Body length must be non-zero (valid `.replay` binary payloads are typically 500 KB - 5 MB).
- **File System & Temporary File Mechanics**:
  - Replays should be downloaded to a configurable directory (e.g. `./replays/`).
  - To prevent corrupted or partially-downloaded files from remaining on disk during network interruptions, downloads must use atomic write staging:
    1. Create temporary file in the destination directory:
       `filepath.Join(replayDir, fmt.Sprintf(".tmp-%s-%d.replay", matchGUID, time.Now().UnixNano()))`
    2. Stream `resp.Body` into the temporary file via `io.Copy(tmpFile, resp.Body)`.
    3. Flush buffers to disk: `tmpFile.Sync()`.
    4. Close the file descriptor: `tmpFile.Close()`.
    5. Validate downloaded size > 0 (and matches `Content-Length` header if >= 0).
    6. Atomically rename temporary file to final target:
       `os.Rename(tmpPath, filepath.Join(replayDir, fmt.Sprintf("%s.replay", matchGUID)))`.
    7. Deferred cleanup: If any step fails, immediately delete the temporary file (`os.Remove(tmpPath)`).

---

## 2. Logic Chain

From the direct observations above, we establish the step-by-step logic chain governing the Ballchasing uploader, rate limiting backoff strategy, duplicate handling, and replay download synchronization:

### 2.1 Replay Download Pipeline
1. **Match Filtering**: When `GetMatchHistory` returns match entries:
   - Check if `matchEntry.ReplayUrl == ""`. If empty, no replay binary is hosted by PsyNet (e.g. early forfeit, cancelled match); mark match as `skipped_no_replay` in state store.
   - Check if `MatchGUID` is already recorded in persistent state as downloaded or uploaded. If present, skip download.
2. **Streaming Download**:
   - Issue `http.Get(matchEntry.ReplayUrl)` with timeout context (e.g. 30–60s).
   - If HTTP status != 200, return `DownloadError{StatusCode: resp.StatusCode, MatchGUID: guid}`.
   - Stream directly to a `.tmp` file in the configured replay directory using `io.Copy`.
   - On completion, verify `bytesWritten > 1024` (sanity check for minimum valid replay header).
   - Atomically rename `.tmp` file to `<replayDir>/<matchGUID>.replay`.
   - Record in persistent state: `status = "downloaded"`, `file_path = "<path>"`, `timestamp = now()`.

### 2.2 Ballchasing Upload Pipeline
1. **Multipart Request Construction**:
   - Create buffer or streaming pipe.
   - Initialize `multipart.NewWriter`.
   - Call `writer.CreateFormFile("file", filepath.Base(replayPath))` setting `"file"` as field name.
   - Copy file contents from disk into part.
   - Close writer to write terminating boundary.
   - Build request: `POST https://ballchasing.com/api/v2/upload?visibility=<visibility>`.
   - Set headers:
     - `Authorization: <token>`
     - `Content-Type: writer.FormDataContentType()`
2. **Response Classification & Deduplication**:
   - Case **201 Created**:
     - Parse JSON into `UploadResult{ID: string, Location: string}`.
     - Record in persistent state: `status = "uploaded"`, `ballchasing_id = result.ID`, `ballchasing_url = result.Location`.
   - Case **409 Conflict**:
     - Parse JSON into `UploadResult{ID: string, Location: string}`.
     - Extract `id` and `location` of the existing replay.
     - Record in persistent state: `status = "duplicate"`, `ballchasing_id = result.ID`, `ballchasing_url = result.Location`.
     - **Do NOT retry or report error.** Deduplication is a normal, successful terminal state.
   - Case **429 Too Many Requests**:
     - Enter Rate Limit Backoff Algorithm (Section 2.3).
   - Case **401 Unauthorized**:
     - Halt upload operations immediately. Return permanent authentication error; alert user to check API key.
   - Case **400 Bad Request** / **422 Unprocessable**:
     - Record in persistent state: `status = "failed_permanent"`. Do not retry this file.
   - Case **5xx Server Error** or **Network Failure**:
     - Treat as transient; retry with exponential backoff or defer to next polling cycle.

### 2.3 Rate Limiting & Backoff Algorithm
1. **Initial Rate Pacing**:
   - Outbound requests to Ballchasing should be rate-limited locally (e.g. using `golang.org/x/time/rate.NewLimiter(rate.Limit(1.5), 1)`) to avoid hitting Ballchasing's baseline 2 req/sec threshold during batch uploads.
2. **Handling 429 Responses**:
   - When HTTP 429 is encountered:
     - Inspect `resp.Header.Get("Retry-After")`.
     - If header is present and parses as an integer `N`: wait `time.Duration(N) * time.Second + jitter`.
     - If header is absent or unparseable: apply exponential backoff:
       `wait = baseBackoff * (2 ^ attempt) + randJitter`
       where `baseBackoff = 2 * time.Second`, `maxAttempts = 4`.
     - After wait expires, rebuild the request body (since request bodies cannot be reused) and retry.
   - If `attempt >= maxAttempts`, abort the upload for this cycle, leave the state as `pending_upload`, and defer to the next 5-minute polling cycle.

---

## 3. Features Discovered

| # | Category | Feature | Description | Inputs | Outputs | Error Behavior | Discovered Via |
|---|----------|---------|-------------|--------|---------|----------------|----------------|
| 1 | Ballchasing Upload | `POST /v2/upload` | Uploads a `.replay` file via multipart/form-data to Ballchasing.com | Multipart form with `file` part; query param `visibility` (`public`, `unlisted`, `private`); header `Authorization: <token>` | HTTP 201 Created with JSON `{"id": "...", "location": "..."}` | 400 Bad Request, 401 Unauthorized, 409 Conflict, 429 Too Many Requests | Official API doc, `ballchaser/client.py:246`, `kickoff-cloud-sync/internal/uploader/ballchasing.go:121` |
| 2 | Ballchasing Upload | `visibility` Query Parameter | Sets visibility permissions of uploaded replay | Query param: `?visibility=public`, `?visibility=unlisted`, or `?visibility=private` | Replay visibility configured accordingly on Ballchasing | If invalid value supplied, server defaults or returns 400 Bad Request | `AutoReplayUploader/Uploader/Ballchasing.cpp:81`, `ballchaser/client.py:246` |
| 3 | Ballchasing Upload | `group` Query Parameter | Assigns uploaded replay directly into a designated replay group | Query param: `?group=<group_id>` | Replay placed inside group | If group does not exist, returns 400 / 404 | `ballchaser/client.py:246`, `Midnight679/kickoff-cloud-sync` |
| 4 | Ballchasing Auth | Raw Token Authorization Header | Authenticates requests using raw API token without "Bearer" prefix | Header `Authorization: <token>` | Grants access to API endpoints | HTTP 401 Unauthorized with JSON `{"error": "Invalid API key."}` if missing or prefixed with "Bearer " | Official API doc, `deweller/lib/ballchasingApi.js:28`, `kickoff-cloud-sync:ballchasing.go:138` |
| 5 | Ballchasing Auth | Ping Endpoint (`GET /`) | Verifies validity of Ballchasing API token and checks service reachability | Header `Authorization: <token>` | HTTP 200 OK with user account metadata (`chaser`, `type`, `steam_id`, `name`) | HTTP 401 Unauthorized if invalid token | `AutoReplayUploader/Ballchasing.cpp:131`, `ballchaser/client.py:126`, `kickoff-cloud-sync:ballchasing.go:81` |
| 6 | Ballchasing Deduplication | HTTP 409 Conflict Handling | Handles duplicate replay uploads gracefully, extracting existing replay ID | Duplicate `.replay` binary payload | HTTP 409 Conflict with JSON `{"error": "duplicate replay", "id": "...", "location": "..."}` | N/A - treated as non-fatal deduplication success | `Midnight679/kickoff-cloud-sync:ballchasing.go:149`, `ballchaser/tests:test_client.py:293` |
| 7 | Ballchasing Rate Limit | HTTP 429 Rate Limiting & Backoff | Detects rate limit errors, reads `Retry-After` header, and applies backoff | Exceeding burst limit or quota | HTTP 429 Too Many Requests with JSON `{"error": "too many requests"}` and `Retry-After: <sec>` header | If retries exhausted, defer upload to next polling cycle | `ballchaser/client.py:44`, `kickoff-cloud-sync:ballchasing.go:44` |
| 8 | Ballchasing Metadata | `GET /replays/<id>` | Retrieves parsed replay metadata and processing status | Replay ID string, `Authorization` header | HTTP 200 OK with JSON details (`status`: `"pending"`, `"ok"`, `"failed"`, player stats, scores) | HTTP 404 Not Found | `Midnight679/kickoff-cloud-sync:ballchasing.go:178`, `ballchaser/client.py:139` |
| 9 | PsyNet Replay Download | Signed Replay URL Streaming | Downloads raw binary `.replay` from pre-signed CDN URL returned by PsyNet `GetMatchHistory` | HTTP GET to `MatchEntry.ReplayUrl` | Binary `.replay` stream written to local file | HTTP 403/410 if expired, HTTP 404 if deleted | `github.com/dank/rlapi/matches.go:20-25`, `Amzd/upload-match-history-to-ballchasing:main.go:58-79` |
| 10 | PsyNet Replay Download | Atomic Temp File Staging | Writes downloading replay to a hidden temporary file before renaming to avoid partial files | Temporary file path `.tmp-<guid>-<ts>.replay` in target directory | Atomically committed `<guid>.replay` | Cleans up temporary file upon error or connection abort | Standard Go streaming pattern, `Amzd:main.go:68-76` |
| 11 | PsyNet Replay Download | Empty Replay URL Detection | Detects matches in history where `ReplayUrl == ""` and skips download | `MatchEntry` with `ReplayUrl: ""` | State marked as `skipped_no_replay` | Prevents nil/empty HTTP GET panics | `github.com/dank/rlapi/matches.go:20-25` |

---

## 4. Edge Cases

| # | Feature | Input / Condition | Observed / Documented Behavior |
|---|---------|-------------------|--------------------------------|
| 1 | Ballchasing Upload Auth | `Authorization: Bearer <token>` | Returns HTTP 401 Unauthorized. Ballchasing strictly forbids the `Bearer ` prefix and requires `Authorization: <token>`. |
| 2 | Ballchasing Upload Duplicate | Uploading a replay already present on Ballchasing | Returns HTTP 409 Conflict. Response body is valid JSON containing the existing replay's `id` and `location`. Client extracts `id`, marks local status as `duplicate`, and avoids retrying. |
| 3 | Ballchasing Rate Limit | Bursting > 2 requests/second | Returns HTTP 429 Too Many Requests. Header `Retry-After: <seconds>` specifies cooldown. Client sleeps `Retry-After` seconds or performs exponential backoff. |
| 4 | Ballchasing Quota Exhaustion | Daily/weekly upload limit reached | Returns HTTP 429 with repeated failures even after backoff. Client must terminate retry loop after max attempts (e.g. 4) and defer until subsequent polling cycle. |
| 5 | Ballchasing Malformed Upload | Missing `"file"` multipart part or empty 0-byte file | Returns HTTP 400 Bad Request with error description. Client must mark upload as permanently failed and avoid infinite retry loops. |
| 6 | Ballchasing Visibility | Custom visibility string | Supports `"public"`, `"unlisted"`, `"private"`. Passing any other string triggers 400 Bad Request or server defaults. |
| 7 | PsyNet Replay Download | `MatchEntry.ReplayUrl` is empty string `""` | Happens when match was cancelled, player forfeited too early, or replay was purged. Client must detect `ReplayUrl == ""`, log debug info, mark state as `skipped_no_replay`, and bypass HTTP GET. |
| 8 | PsyNet Replay Download | Signed URL has expired (HTTP 403 / 410) | CDN returns HTTP 403 Forbidden or 410 Gone. Client catches non-200 status code, aborts stream, deletes temporary file, and records `download_failed`. |
| 9 | PsyNet Replay Download | Connection drop / network interrupt during download | `io.Copy` returns premature EOF or network timeout error. Deferred cleanup removes incomplete `.tmp` file. Match state is not marked downloaded, enabling clean retry on next polling cycle. |
| 10 | PsyNet Replay Download | Zero-byte or truncated response payload | Response returns HTTP 200 but body has 0 bytes or < 1024 bytes. Validation check fails; temporary file is deleted; error returned. |
| 11 | Local Storage Directory | Replay directory does not exist | `os.MkdirAll(replayDir, 0755)` ensures directory exists before writing temp files. |
| 12 | Ballchasing Replay Size | Large replay file (> 5 MB, multi-overtime) | Streaming upload via `io.Copy` into multipart writer ensures memory footprint remains bounded without out-of-memory errors. |

---

## 5. Caveats

1. **Upload Quota vs Rate Limit**: Ballchasing enforces both technical rate limits (e.g. 2 req/sec) and account upload quotas (daily/weekly replay count limits). A 429 resulting from quota exhaustion cannot be resolved by short-term exponential backoff; the daemon must recognize persistent 429s after max retry attempts (e.g. 4 attempts) and defer until the next polling cycle.
2. **CDN Pre-Signed URL Lifespan**: Replay download URLs provided by PsyNet are pre-signed and time-limited. Once retrieved by `GetMatchHistory`, downloads should be executed immediately within the same sync cycle rather than queued for later download.
3. **No Direct Live Ballchasing Account Probing**: Because live API tokens are private to users, probing was conducted against authoritative client source code, mock harnesses, and official documentation specifications rather than making unauthenticated live mutating upload calls to production Ballchasing.com.

---

## 6. Conclusion

1. **Ballchasing Client Contract**:
   - The Ballchasing client must POST to `https://ballchasing.com/api/v2/upload?visibility=<visibility>`.
   - The Authorization header must be set to `<token>` (raw token, no `Bearer` prefix).
   - The replay binary must be packaged as a multipart form part named `"file"`.
   - Responses with HTTP 201 Created and HTTP 409 Conflict must both be decoded as successful operations yielding `UploadResult{ID, Location}`.
   - HTTP 429 responses must be handled using `Retry-After` header parsing and exponential backoff with jitter, capped at a maximum attempt count.
   - HTTP 401 must trigger an immediate authentication failure.
2. **Replay Downloader Contract**:
   - Replay downloads from PsyNet `ReplayUrl` must stream directly to a temporary file in the local storage directory and atomically rename upon completion.
   - Validations must verify HTTP 200 and minimum byte size (> 1024 bytes).
   - Empty `ReplayUrl` fields must be safely handled without issuing network requests.
3. **Test Harness & Mocking Strategy**:
   - Both Ballchasing upload and PsyNet replay download endpoints can be cleanly mocked using standard `httptest.Server` instances verifying multipart form parts, Authorization headers, and reproducing 201, 409, 429, 401, and 400 responses.

---

## 7. Verification Method

To independently verify the interface specifications and behaviors identified in this report:

1. **Inspect Reference Codebases**:
   - Ballchasing multipart upload and raw Authorization header:
     `Midnight679/kickoff-cloud-sync/internal/uploader/ballchasing.go:121-160`
     `bakkesmodorg/AutoReplayUploader/Uploader/Ballchasing.cpp:78-95`
     `deweller/rl-stat-collector-discord-bot/lib/ballchasingApi.js:28-40`
   - Ballchasing 201 Created & 409 Conflict payload structures:
     `tom-boyes-park/ballchaser/tests/ballchaser/test_client.py:285-305`
   - PsyNet ReplayUrl handling:
     `github.com/dank/rlapi/matches.go:20-25`
     `Amzd/upload-match-history-to-ballchasing/main.go:58-79`
2. **Programmatic Verification via Unit Tests**:
   - Implement an HTTP test server (`httptest.NewServer`) mimicking Ballchasing's `/v2/upload` endpoint:
     - Verify multipart form boundary parsing and `"file"` form part.
     - Assert `req.Header.Get("Authorization") == "<test_token>"` (no `Bearer`).
     - Emit 201 Created with `{"id": "test-id", "location": "https://ballchasing.com/replay/test-id"}`.
     - Emit 409 Conflict with `{"error": "duplicate replay", "id": "test-id", "location": "https://ballchasing.com/replay/test-id"}`.
     - Emit 429 Too Many Requests with `Retry-After: 1`.
     - Emit 401 Unauthorized with `{"error": "Invalid API key."}`.
   - Run tests with `go test -v ./...`.
