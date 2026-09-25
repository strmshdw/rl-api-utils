# Milestone 3 Review & Adversarial Challenge Report

**Reviewer**: `m3_reviewer_1`  
**Milestone**: M3 — Ballchasing Replay Uploader (`internal/ballchasing`)  
**Target Agent**: `m3_worker_1`  
**Date**: 2026-09-25T04:14:00Z  
**Verdict**: **APPROVE**

---

## 1. Observation

Direct code examination and independent test execution in the workspace confirmed the following:

### 1.1 Files Reviewed
1. `internal/ballchasing/types.go` (94 lines):
   - Sentinel errors: `ErrInvalidAPIKey`, `ErrRateLimitExhausted`, `ErrBadRequest`, `ErrNotFound`, `ErrServerError`, `ErrEmptyMatchGUID`, `ErrEmptyFilePath`, `ErrEmptyAPIKey`, `ErrEmptyFile`, `ErrFileNotFound`, `ErrInvalidVisibility`.
   - Types: `Visibility` ("public", "unlisted", "private"), `UploadResult` (`ID`, `Location`, `IsDuplicate`), `PingResponse`, `ClientConfig`.
   - Contract: `ReplayUploader` interface matching `PROJECT.md` (`UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error)` and `Ping(ctx context.Context) error`).
2. `internal/ballchasing/client.go` (623 lines):
   - Constructors: `NewClient(cfg ClientConfig, opts ...Option)`, `New(apiKey string, opts ...Option)`, and `NewHTTPBallchasingUploader`.
   - Options: `WithHTTPClient`, `WithBaseURL`, `WithVisibility`, `WithGroup`, `WithMaxRetries`, `WithTimeout`, `WithBaseBackoff`, `WithMaxBackoff`, `WithStreaming`.
   - Network implementation: Multipart form builder with `file` form part and sanitized `<matchGUID>.replay` filename.
   - Header construction: `req.Header.Set("Authorization", c.apiKey)` delivering raw API token without `"Bearer "` prefix.
   - Status code handling:
     - `201 Created`: returns `&UploadResult{ID: res.ID, Location: loc, IsDuplicate: false}, nil`.
     - `409 Conflict`: returns `&UploadResult{ID: res.ID, Location: loc, IsDuplicate: true}, nil` without error and 0 retries.
     - `429 Too Many Requests`: parses integer seconds or HTTP-date from `Retry-After` header, falls back to exponential backoff with full jitter, enforces `maxRetries` retry budget.
     - `401 Unauthorized`: returns `ErrInvalidAPIKey` on attempt 0 (0 retries).
     - `400 Bad Request`: returns error wrapping `ErrBadRequest` with extracted error message on attempt 0 (0 retries).
     - `5xx Server Error`: retries with exponential backoff up to `maxRetries`.
   - File descriptor safety: In buffered mode, file is closed immediately after reading into `bytes.Buffer` before HTTP dispatch. In streaming mode, file is closed upon attempt completion via `onceCloser`. Zero file descriptors are open during `sleepWithContext` backoff sleeps.
   - `Ping(ctx context.Context) error`: queries `GET /` with raw `Authorization` header; returns `nil` on 200 OK, `ErrInvalidAPIKey` on 401 Unauthorized.
3. `internal/ballchasing/client_test.go` (1037 lines):
   - 19 test scenarios covering 201 Created, 409 Conflict, 429 backoff retry, 429 exhaustion, 401 Unauthorized, Bearer prefix rejection, non-existent files, 0-byte files, context cancellations, Ping endpoints, visibility parameters, group parameters, input validation, Retry-After header variations, 5xx retry/recovery, 400 Bad Request immediate halt, 5MB large payload streaming, concurrent upload safety (10 goroutines), and zero-RAM streaming.

### 1.2 Independent Test & Static Analysis Execution
1. Unit tests for `internal/ballchasing`:
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   cd d:\code\rl-api-utils
   go test -v -count=1 ./internal/ballchasing/...
   ```
   **Result**: 19 of 19 test cases PASS (2.923s runtime).
2. Static analysis (`go vet`):
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   cd d:\code\rl-api-utils
   go vet ./internal/ballchasing/...
   ```
   **Result**: Exit code 0, zero warnings.
3. Entire repository test suite:
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   cd d:\code\rl-api-utils
   go test -count=1 ./...
   ```
   **Result**: All packages (`auth`, `ballchasing`, `config`, `psynet`, `storage`, `testutil`, `test/e2e`) pass with 100% success.

---

## 2. Logic Chain

1. **Integrity Assessment**:
   - Inspected source code for hardcoded mock returns, fake implementations, or bypassed logic. None found.
   - Payload serialization genuinely constructs `multipart/form-data` with `file` part and streams actual bytes.
   - Response deserialization unmarshals actual server JSON for both 201 and 409 status codes.
2. **Interface & Contract Conformance**:
   - `Client` satisfies `PROJECT.md` `ReplayUploader` contract:
     `UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error)`
   - Defined `UploadResult` contains `ID`, `Location`, and `IsDuplicate` matching `PROJECT.md`.
   - Also provides `Ping(ctx context.Context) error` for early startup credentials validation.
3. **Authentication Conformance**:
   - `req.Header.Set("Authorization", c.apiKey)` passes the raw token directly.
   - Tests explicitly confirm that if `"Bearer "` is present, the mock server returns 401, which is caught and returned as `ErrInvalidAPIKey`.
4. **HTTP Status Handling & Idempotency**:
   - **201 Created**: Parsed and returned as `IsDuplicate: false`, `err == nil`.
   - **409 Conflict**: Duplicate replay is recognized as an idempotent non-error terminal condition. The client returns `UploadResult{ID: res.ID, Location: loc, IsDuplicate: true}` with `err == nil`, preventing retry thrashing and syncer errors.
   - **429 Rate Limiting**: Parses `Retry-After` header (seconds and HTTP-date); falls back to jittered exponential backoff; halts upon reaching `maxRetries` with `ErrRateLimitExhausted`.
   - **401 Unauthorized**: Halts immediately without retries, returning `ErrInvalidAPIKey`.
   - **400 Bad Request**: Halts immediately without retries, extracting server error details.
5. **Windows File Descriptor Safety**:
   - In buffered mode, `defer file.Close()` closes the OS file descriptor before the HTTP request is even dispatched.
   - In streaming mode, `safeCloser` closes the file handle immediately when `doUploadAttempt` returns.
   - Crucially, during `sleepWithContext`, `doUploadAttempt` has returned and the file is closed. No open handles remain during sleep intervals, avoiding Windows file locking and handle exhaustion issues.
6. **Adversarial & Stress-Testing Resilience**:
   - *Path Traversal*: `matchGUID` is sanitized via `filepath.Base` before setting filename.
   - *Malformed Retry-After*: Non-numeric or distant future dates safely fall back to exponential backoff.
   - *Context Cancellation*: Pre-cancelled contexts and cancellation mid-backoff abort promptly without thread hanging.
   - *Memory Safety*: Response bodies are read with `io.LimitReader(..., 1MB)`, preventing memory bloat from rogue server payloads.
   - *Concurrency*: Verified safe across concurrent goroutine execution.

---

## 3. Caveats

1. **Mock Verification**: As specified in R5, verification is performed against programmatic `httptest.Server` mocks (`testutil.MockBallchasingServer`). No network calls were made to live production Ballchasing endpoints.
2. **CGO / Race Detector**: Go race detector (`-race`) requires CGO and GCC toolchains which are not configured in this pure-Go Windows environment. However, concurrent safety was validated via `TestClient_Upload_ConcurrentUploadSafety` with 10 parallel goroutines and stateless client design.

---

## 4. Conclusion

Milestone 3 (Ballchasing Replay Uploader) is implemented with high quality, rigorous error handling, complete interface adherence, and solid Windows file handle discipline. Zero integrity violations or architectural flaws were found.

**Verdict: APPROVE**

---

## 5. Verification Method

To independently reproduce the review verification:

1. **Run Milestone 3 Unit Tests**:
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   cd d:\code\rl-api-utils
   go test -v -count=1 ./internal/ballchasing/...
   ```
   *Expected*: All 19 tests pass (exit code 0).

2. **Run Go Vet**:
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   cd d:\code\rl-api-utils
   go vet ./internal/ballchasing/...
   ```
   *Expected*: Zero warnings (exit code 0).

3. **Run Full Repository Test Suite**:
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   cd d:\code\rl-api-utils
   go test -count=1 ./...
   ```
   *Expected*: All packages pass (exit code 0).
