# Milestone 3 Handoff Report: Ballchasing Replay Uploader

**Agent**: `m3_worker_1` (Implementer / QA / Specialist)  
**Milestone**: M3 — Ballchasing Replay Uploader (`internal/ballchasing`)  
**Date**: 2026-09-25T04:10:00Z  

---

## 1. Observation

Direct inspection and execution in the workspace confirmed the following states and outputs:

### 1.1 Implemented Files
Under exclusive write ownership, three files were created and verified inside `internal/ballchasing`:
1. `internal/ballchasing/types.go` (75 lines):
   - Defined sentinel errors: `ErrInvalidAPIKey`, `ErrRateLimitExhausted`, `ErrBadRequest`, `ErrNotFound`, `ErrServerError`, `ErrEmptyMatchGUID`, `ErrEmptyFilePath`, `ErrEmptyAPIKey`, `ErrEmptyFile`, `ErrFileNotFound`, `ErrInvalidVisibility`.
   - Defined `Visibility` type alias and constants: `VisibilityPublic = "public"`, `VisibilityUnlisted = "unlisted"`, `VisibilityPrivate = "private"`.
   - Defined `UploadResult` with `ID string`, `Location string`, `IsDuplicate bool`.
   - Defined `ReplayUploader` interface with `UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error)` and `Ping(ctx context.Context) error`.
   - Defined `ClientConfig` and `PingResponse` structs.
2. `internal/ballchasing/client.go` (474 lines):
   - Constructors: `NewClient(cfg ClientConfig, opts ...Option) (*Client, error)`, `New(apiKey string, opts ...Option) (*Client, error)`, and `NewHTTPBallchasingUploader(baseURL, apiKey, visibility, group string, maxRetries int) *Client`.
   - Options: `WithHTTPClient`, `WithBaseURL`, `WithVisibility`, `WithGroup`, `WithMaxRetries`, `WithTimeout`, `WithBaseBackoff`, `WithMaxBackoff`, `WithStreaming`.
   - Endpoint: `POST <baseURL>/v2/upload?visibility=<vis>&group=<group>`.
   - Auth header: `req.Header.Set("Authorization", c.apiKey)` sending raw token strictly without `"Bearer "` prefix.
   - Multipart payload: Single part field named `"file"`, with filename `<matchGUID>.replay`.
   - Status code handling:
     - `201 Created`: unmarshals JSON response, returns `&UploadResult{ID, Location, IsDuplicate: false}, nil`.
     - `409 Conflict`: unmarshals JSON response, returns `&UploadResult{ID, Location, IsDuplicate: true}, nil` without error and without retrying.
     - `429 Too Many Requests`: parses integer seconds or HTTP-date from `Retry-After` header, falls back to exponential backoff with full jitter, obeys `maxRetries` budget, sleeps with `ctx.Done()` awareness.
     - `401 Unauthorized`: returns `ErrInvalidAPIKey` immediately (0 retries).
     - `400 Bad Request`: returns error wrapping `ErrBadRequest` with server response message immediately (0 retries).
     - `404 Not Found`: returns `ErrNotFound` immediately.
     - `5xx Server Error`: retries with exponential backoff up to `maxRetries`.
   - Windows file descriptor safety: In buffered mode, file is closed immediately after reading into memory before HTTP dispatch. In streaming mode, file is closed upon attempt completion via `onceCloser`. Zero file handles are open during backoff sleeps.
   - `Ping(ctx context.Context) error`: queries `GET /` with raw `Authorization` header; returns `nil` on 200 OK, `ErrInvalidAPIKey` on 401 Unauthorized.
3. `internal/ballchasing/client_test.go` (606 lines):
   - 19 comprehensive unit test scenarios covering all status codes, input validations, retry behaviors, zero-RAM streaming, and options.

### 1.2 Verification Command Results
Execution of verification commands:

```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
cd d:\code\rl-api-utils
go test -v -count=1 ./internal/ballchasing/...
```
Output:
```
=== RUN   TestClient_Upload_Success201
--- PASS: TestClient_Upload_Success201 (0.01s)
=== RUN   TestClient_Upload_Duplicate409
--- PASS: TestClient_Upload_Duplicate409 (0.00s)
=== RUN   TestClient_Upload_RateLimit429_RetrySuccess
--- PASS: TestClient_Upload_RateLimit429_RetrySuccess (2.01s)
=== RUN   TestClient_Upload_RateLimit429_Exhaustion
--- PASS: TestClient_Upload_RateLimit429_Exhaustion (0.00s)
=== RUN   TestClient_Upload_Unauthorized401_ImmediateHalt
--- PASS: TestClient_Upload_Unauthorized401_ImmediateHalt (0.01s)
=== RUN   TestClient_Upload_BearerPrefixRejected
--- PASS: TestClient_Upload_BearerPrefixRejected (0.00s)
=== RUN   TestClient_Upload_NonExistentFile
--- PASS: TestClient_Upload_NonExistentFile (0.00s)
=== RUN   TestClient_Upload_EmptyFile0Byte
--- PASS: TestClient_Upload_EmptyFile0Byte (0.00s)
=== RUN   TestClient_Upload_ContextCancellation
=== RUN   TestClient_Upload_ContextCancellation/pre-cancelled_context
=== RUN   TestClient_Upload_ContextCancellation/cancellation_during_rate_limit_backoff
--- PASS: TestClient_Upload_ContextCancellation (0.10s)
=== RUN   TestClient_Ping
=== RUN   TestClient_Ping/valid_API_key_returns_200_OK
=== RUN   TestClient_Ping/invalid_API_key_returns_401_Unauthorized
=== RUN   TestClient_Ping/Bearer_prefix_returns_401_Unauthorized
=== RUN   TestClient_Ping/server_error_returns_status_error
--- PASS: TestClient_Ping (0.00s)
=== RUN   TestClient_Visibility_Propagation
=== RUN   TestClient_Visibility_Propagation/public
=== RUN   TestClient_Visibility_Propagation/unlisted
=== RUN   TestClient_Visibility_Propagation/private
=== RUN   TestClient_Visibility_Propagation/invalid-vis
--- PASS: TestClient_Visibility_Propagation (0.01s)
=== RUN   TestClient_Group_Propagation
--- PASS: TestClient_Group_Propagation (0.00s)
=== RUN   TestClient_InputValidation
=== RUN   TestClient_InputValidation/empty_API_key_on_client_creation
=== RUN   TestClient_InputValidation/empty_match_GUID_on_upload
=== RUN   TestClient_InputValidation/whitespace_match_GUID_on_upload
=== RUN   TestClient_InputValidation/empty_file_path_on_upload
--- PASS: TestClient_InputValidation (0.00s)
=== RUN   TestClient_Upload_RetryAfterVariations
=== RUN   TestClient_Upload_RetryAfterVariations/Retry-After:_0_causes_immediate_retry_without_sleeping
=== RUN   TestClient_Upload_RetryAfterVariations/HTTP-date_Retry-After_falls_back_to_exponential_backoff
=== RUN   TestClient_Upload_RetryAfterVariations/malformed_non-numeric_Retry-After_falls_back_to_exponential_backoff
--- PASS: TestClient_Upload_RetryAfterVariations (0.02s)
=== RUN   TestClient_Upload_Transient5xxRetry
--- PASS: TestClient_Upload_Transient5xxRetry (0.02s)
=== RUN   TestClient_Upload_BadRequest400_ImmediateHalt
--- PASS: TestClient_Upload_BadRequest400_ImmediateHalt (0.00s)
=== RUN   TestClient_Upload_LargePayloadStreaming
--- PASS: TestClient_Upload_LargePayloadStreaming (0.03s)
=== RUN   TestClient_Upload_ConcurrentUploadSafety
--- PASS: TestClient_Upload_ConcurrentUploadSafety (0.01s)
=== RUN   TestClient_Upload_ZeroRAMStreamingMode
--- PASS: TestClient_Upload_ZeroRAMStreamingMode (0.01s)
=== RUN   TestClient_OptionsAndConstructors
--- PASS: TestClient_OptionsAndConstructors (0.01s)
PASS
ok  	github.com/dank/rl-api-utils/internal/ballchasing	3.179s
```

```powershell
go vet ./internal/ballchasing/...
```
Output:
Exit code 0, zero warnings.

```powershell
go test -count=1 ./...
```
Output:
```
ok  	github.com/dank/rl-api-utils/internal/auth	0.239s
ok  	github.com/dank/rl-api-utils/internal/ballchasing	3.312s
ok  	github.com/dank/rl-api-utils/internal/config	0.604s
ok  	github.com/dank/rl-api-utils/internal/psynet	4.449s
ok  	github.com/dank/rl-api-utils/internal/storage	3.295s
ok  	github.com/dank/rl-api-utils/internal/testutil	1.002s
ok  	github.com/dank/rl-api-utils/test/e2e	3.785s
```

---

## 2. Logic Chain

1. **Contract Adherence**:
   - `PROJECT.md` specifies `ReplayUploader` requiring `UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error)`. `Client` satisfies this interface directly.
   - `DISPATCH.md` required adding `Ping(ctx context.Context) error`, `UploadResult`, `Visibility` types, and sentinel errors. All were placed in `internal/ballchasing/types.go` and implemented in `internal/ballchasing/client.go`.
2. **Duplicate Deduplication Logic**:
   - When a replay already exists on Ballchasing.com, HTTP 409 is returned with the existing replay ID and URL. The client parses these and returns `UploadResult{ID: id, Location: loc, IsDuplicate: true}` with `err == nil`. This satisfies idempotency by preventing error propagation or retry thrashing.
3. **Throttling & Backoff Logic**:
   - When rate-limited (HTTP 429), `Retry-After` is parsed. If integer seconds, that duration is respected; if HTTP-date within `maxBackoff`, the interval is respected; otherwise, exponential backoff with random jitter `[backoff/2, backoff]` is applied.
   - Backoff sleeps honor `ctx.Done()`. If the context is cancelled, the method aborts promptly without hanging.
   - When `attempt >= maxRetries`, the loop halts and returns `ErrRateLimitExhausted`.
4. **Fatal vs Transient Error Classification**:
   - HTTP 401 returns `ErrInvalidAPIKey` on attempt 0 (no retries).
   - HTTP 400 returns `ErrBadRequest` on attempt 0 (no retries).
   - HTTP 5xx returns `ErrServerError` and retries within the retry budget.
5. **Windows File Handle Discipline**:
   - In buffered mode, `os.Open` is deferred closed and read into `bytes.Buffer` before making any HTTP network calls.
   - In streaming mode, file descriptor lifetime is managed via `onceCloser`, closing immediately when the attempt terminates.
   - During backoff sleep intervals, no file descriptors remain open, preventing file lock errors on Windows.

---

## 3. Caveats

1. **Streaming vs Buffered Mode**: Default is buffered mode (`StreamUpload: false`), which is recommended for typical Rocket League replays (<5MB) and guarantees immediate file descriptor release before network I/O. For high-volume or constrained memory environments, `StreamUpload: true` can be enabled via `WithStreaming(true)`. Both modes were verified to pass.
2. **Strict Raw Authorization**: As required by Ballchasing API and verified by test scenarios, the API key must not have `"Bearer "` prepended. If a user enters `"Bearer <key>"`, the client preserves it verbatim, allowing the Ballchasing API / mock server to reject it with 401.

---

## 4. Conclusion

Milestone 3 (Ballchasing Replay Uploader) is 100% complete and fully verified:
- `internal/ballchasing/types.go` implements all required interfaces, types, constants, and sentinel errors.
- `internal/ballchasing/client.go` implements the upload client, multipart form construction, raw token authorization, 201/409/429/401/400/5xx handling, Windows file descriptor safety, and Ping endpoint.
- `internal/ballchasing/client_test.go` provides 19 unit test scenarios passing with 100% success against `testutil.MockBallchasingServer`.
- All verification commands passed with zero errors and zero vet warnings across the entire repository.

---

## 5. Verification Method

To independently verify the implementation:

1. **Run Unit Tests for internal/ballchasing**:
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   cd d:\code\rl-api-utils
   go test -v -count=1 ./internal/ballchasing/...
   ```
   *Expected*: 19 tests pass, exit code 0.

2. **Run Static Analysis (go vet)**:
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   cd d:\code\rl-api-utils
   go vet ./internal/ballchasing/...
   ```
   *Expected*: Zero warnings, exit code 0.

3. **Run Entire Repository Test Suite**:
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   cd d:\code\rl-api-utils
   go test -count=1 ./...
   ```
   *Expected*: All packages (`auth`, `ballchasing`, `config`, `psynet`, `storage`, `testutil`, `test/e2e`) pass with 100% success.
