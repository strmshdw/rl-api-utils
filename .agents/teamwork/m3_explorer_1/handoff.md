# Investigation Report: Ballchasing Client Upload Architecture

**Role**: `m3_explorer_1` (Ballchasing API & Streaming Explorer)  
**Milestone**: M3 — Ballchasing Replay Uploader (`internal/ballchasing`)  
**Date**: 2026-09-25T04:05:00Z  

---

## 1. Observation

Direct investigation of the project contracts (`PROJECT.md`), requirements (`ORIGINAL_REQUEST.md`), survey mining specifications (`survey_miner_ballchasing_1/handoff.md`), mock test harness (`internal/testutil/mock_ballchasing.go`), existing E2E test suites (`test/e2e/tier1_feature_test.go`, `tier2_boundary_test.go`, `tier3_pairwise_test.go`), and configuration (`internal/config/config.go`) revealed the following concrete architectural facts, constraints, and contracts:

### 1.1 Interface Contracts
- **`PROJECT.md` (lines 168–182)** defines the domain interface contract for `internal/ballchasing` ↔ `internal/syncer`:
  ```go
  package syncer

  import "context"

  type UploadResult struct {
      ID          string
      Location    string
      IsDuplicate bool
  }

  type ReplayUploader interface {
      UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error)
  }
  ```
- **`DISPATCH.md` (peer and milestone requirements)** specifies an additional `Ping(ctx context.Context) error` method on the Ballchasing client querying `GET /` to validate credentials prior to sync cycle execution.
- In Go clean architecture, `internal/ballchasing` defines the concrete `UploadResult` and `ReplayUploader` types, and `*Client` implements both `UploadReplay` and `Ping`. Any consumer (including `syncer.ReplayUploader`) is satisfied via Go's structural typing.

### 1.2 Multipart Form Construction
- **Replay Upload Endpoint**: `POST <baseURL>/v2/upload`
- **Query Parameters**:
  - `visibility`: valid values `"public"`, `"unlisted"`, `"private"`. Observed in `testutil/mock_ballchasing.go:261-267`:
    ```go
    visibility := r.URL.Query().Get("visibility")
    if visibility != "" && visibility != "public" && visibility != "unlisted" && visibility != "private" {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusBadRequest)
        _ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("invalid visibility: %s", visibility)})
        return
    }
    ```
  - `group`: optional replay group identifier (e.g. `?group=weekly-scrims`). Observed in `testutil/mock_ballchasing.go:268` and `test/e2e/tier3_pairwise_test.go:329`.
  - Query parameters must be properly URL-encoded using `url.Values`.
- **Multipart Form Payload**:
  - Exactly one form field named `"file"`.
  - Header:
    ```http
    Content-Disposition: form-data; name="file"; filename="<matchGUID>.replay"
    Content-Type: application/octet-stream
    ```
  - Observed in `testutil/mock_ballchasing.go:280-286`:
    ```go
    file, header, err := r.FormFile("file")
    if err != nil {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusBadRequest)
        _ = json.NewEncoder(w).Encode(map[string]string{"error": "missing 'file' form part"})
        return
    }
    ```
  - The filename submitted in the form header must be `"<matchGUID>.replay"`. If `matchGUID` already has the `.replay` suffix, it must be normalized to prevent `foo.replay.replay`. Furthermore, `filepath.Base` should be used to eliminate path traversal characters from form headers.

### 1.3 Raw Authorization Header
- **Header Name**: `Authorization`
- **Format**: Raw token string directly without `Bearer ` prefix:
  ```http
  Authorization: <token>
  ```
- **Strict Prohibition of Bearer Prefix**:
  - Observed in `testutil/mock_ballchasing.go:198, 234`:
    ```go
    if m.alwaysUnauthorized || auth == "" || strings.HasPrefix(auth, "Bearer ") || (m.expectedToken != "" && auth != m.expectedToken) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusUnauthorized)
        _ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid API key."})
        return
    }
    ```
  - Observed in `test/e2e/tier1_feature_test.go:780-792` (`TestTier1_F9_AuthHeader_BearerPrefixRejected401`): passing `"Bearer <token>"` triggers HTTP 401 Unauthorized.
  - Setting `req.Header.Set("Authorization", c.apiKey)` directly guarantees that the configured token is sent verbatim without prepending `Bearer `.

### 1.4 Base URL Configuration & Mockability
- **Production Base URL**: `https://ballchasing.com/api` (as configured in `internal/config/config.go:153`).
- **Endpoint Routing in Mock Server** (`internal/testutil/mock_ballchasing.go:176-186`):
  - Ping endpoint matches: `GET /`, `GET /api`, or `GET /api/`.
  - Upload endpoint matches: `POST` with `strings.HasSuffix(r.URL.Path, "/v2/upload")`.
- When `baseURL` is trimmed of trailing slashes:
  - Production `https://ballchasing.com/api` -> upload path `/api/v2/upload`, ping path `/api/`.
  - Mock server `http://127.0.0.1:port` -> upload path `/v2/upload`, ping path `/`.
  Both URLs are fully compatible with both the mock test server and the production Ballchasing API.

### 1.5 File Streaming, Memory Footprint & Windows Handle Safety
- **Replay File Sizes**:
  - Rocket League binary `.replay` files range from ~500 KB (short 1v1 match) to 5.0 MB (long overtime 3v3 match).
  - In `test/e2e/tier2_boundary_test.go:218`, the 5MB boundary condition is tested:
    ```go
    largeData := testutil.GenerateValidReplay("large-guid", 5*1024*1024)
    ```
- **Windows File Locking Mechanics**:
  - On Windows, open file descriptors lock the underlying file. If a file handle remains open during an HTTP backoff sleep (e.g. `Retry-After: 60`), the file is held locked for 60 seconds, preventing cleanup, rotation, or inspection.
  - If a file handle is not closed on error or context cancellation, OS file descriptors leak.
  - In `internal/psynet/downloader.go:297-302`, the critical Windows requirement is noted: file handles must always be closed before renaming or entering waiting states.

---

## 2. Logic Chain

From the observations above, we establish the step-by-step reasoning supporting the Ballchasing client upload architecture:

### 2.1 File Streaming vs. In-Memory Buffering Evaluation

Three distinct approaches were analyzed for constructing the multipart request:

1. **Approach 1: In-Memory `bytes.Buffer` (Buffered)**
   - *Mechanics*: Reads file from disk into a `bytes.Buffer`, writes multipart headers, copies file bytes, closes writer, then constructs `http.NewRequestWithContext`.
   - *Advantages*:
     - File handle is closed *immediately* after reading into the buffer, before network I/O starts. Zero open file handles during transmission.
     - Known exact `Content-Length`. Automatically supported by `bytes.Reader`.
     - 100% immune to goroutine leaks, pipe deadlocks, or pipe stalls.
     - Replays are bounded (500KB to 5MB). In a daemon uploading sequentially, peak RSS increase is negligible (~5MB).
   - *Disadvantages*: Allocates a 5MB buffer in RAM per upload.

2. **Approach 2: `io.Pipe` with Goroutine**
   - *Mechanics*: Spawns a background goroutine writing multipart headers and copying `file` into `io.PipeWriter`, while `http.Client` reads from `io.PipeReader`.
   - *Advantages*: Streams chunk by chunk without 5MB memory allocation.
   - *Disadvantages*:
     - Spawns an unmonitored goroutine per upload attempt. If context is cancelled or the HTTP request fails midway, the goroutine must be aborted via `pw.CloseWithError(err)`, risking goroutine leaks.
     - Without pre-calculated length, Go's `http.Client` defaults to `Transfer-Encoding: chunked`. Many reverse proxies and API gateways (including Cloudflare and older NGINX instances fronting REST APIs) reject chunked multipart uploads with HTTP 411 Length Required.
     - Rebuilding the pipe on 429 retries requires spawning new goroutines per attempt.

3. **Approach 3: Zero-Allocation `io.MultiReader` with Exact `Content-Length` (Optimal Streaming)**
   - *Mechanics*:
     - Pre-computes multipart header bytes (~140 bytes) via a small buffer.
     - Computes the closing boundary footer string (~40 bytes): `\r\n--<boundary>--\r\n`.
     - Queries `fileInfo.Size()` to compute exact `Content-Length = len(header) + fileInfo.Size() + len(footer)`.
     - Chains readers: `io.MultiReader(headerReader, file, footerReader)`.
     - Sets `req.ContentLength = totalLength`.
     - Sets `req.Body = &readCloserWrapper{Reader: combined, closer: &onceCloser{closer: file}}`.
   - *Advantages*:
     - **True zero-RAM file streaming**: The file is read directly from disk in standard 32KB kernel/TCP buffers.
     - **Exact `Content-Length` header**: Disables chunked encoding; 100% compliant with all HTTP/1.1 proxies.
     - **Zero background goroutines**: No concurrency, no channel deadlocks, no goroutine leaks.
     - **Safe handle closing**: `onceCloser` guarantees `file.Close()` is called as soon as transmission finishes or upon any error.

**Architectural Decision**: The proposed `Client` supports **both** mechanisms via the `StreamUpload: bool` configuration option:
- When `StreamUpload == true`: Uses Approach 3 (`io.MultiReader` zero-allocation streaming with exact `Content-Length`).
- When `StreamUpload == false` (default): Uses Approach 1 (`bytes.Buffer` buffered upload), where the file handle is closed immediately before the HTTP request is even dispatched.
Both modes were implemented, tested, and verified to pass with `go test` and `go vet`.

### 2.2 Safe Handle Closing Across Retries
- In both streaming and buffered modes, file opening is strictly scoped inside `doUploadAttempt`:
  ```go
  req, closer, err := c.createMultipartRequest(ctx, endpoint, matchGUID, filePath)
  if err != nil {
      return nil, 0, false, err
  }
  if closer != nil {
      defer closer.Close()
  }
  ```
- When `doUploadAttempt` returns (whether succeeding with 201/409, failing with 401/400, or receiving a 429 to be retried):
  - In buffered mode: `file` was already closed before `httpClient.Do(req)` was called.
  - In streaming mode: `closer.Close()` runs before `doUploadAttempt` exits.
- When `sleepWithContext(ctx, waitDuration)` executes:
  - **Zero file handles are held open**.
  - On Windows, no file locking errors can occur during rate limit backoff sleeps.
  - When the retry executes, a fresh file handle is cleanly opened.

### 2.3 Strict Authorization & URL Construction
- `req.Header.Set("Authorization", c.apiKey)` sets the raw key. No `"Bearer "` prefix is prepended.
- URL builder uses `url.Values` to construct:
  - `?visibility=<public|unlisted|private>`
  - `&group=<groupID>` (optional)
- Base URL is trimmed of trailing slashes, ensuring consistent behavior for both `https://ballchasing.com/api` and `http://127.0.0.1:port`.

---

## 3. Caveats

1. **Replay AST Parsing**:
   - The Ballchasing client validates that the file exists, is not a directory, and is non-empty (`>0` bytes). Deeper Rocket League replay binary AST parsing and integrity validation are performed server-side by Ballchasing's ingestion worker.
2. **Account Upload Quotas vs Technical Rate Limits**:
   - Technical rate limits (burst > 2 req/sec) are resolved via short exponential backoff.
   - Account quota exhaustion (daily/weekly limits) returns persistent 429s. After `maxRetries` (default 3) are exhausted, the client returns `ErrRateLimitExhausted`, enabling the domain syncer to defer the match to the next polling cycle without crashing.
3. **No Bearer Prefix Workarounds**:
   - If an API key configured by the user begins with `Bearer `, the client deliberately does not strip it. Instead, Ballchasing API's 401 rejection is preserved as expected by E2E test `TestTier1_F9_AuthHeader_BearerPrefixRejected401`.

---

## 4. Conclusion

1. **Architecture Ready**: The client architecture for `internal/ballchasing` is fully designed and verified:
   - Interface `ReplayUploader` satisfies `PROJECT.md` and provides `Ping(ctx)` for startup health checks.
   - Multipart form construction formats the single `"file"` field as `<matchGUID>.replay` with query parameters `?visibility=` and `&group=`.
   - Supports both **Zero-RAM streaming** (`io.MultiReader` with exact `Content-Length`) and **Buffered mode** (`bytes.Buffer`).
   - Strict `Authorization: <token>` raw header implementation.
   - Robust Windows file handle safety: zero handle leaks during backoff sleeps.
   - Full mockability against `testutil.MockBallchasingServer`.
2. **Proposed Code Artifacts**:
   - Full proposed type definitions: `d:\code\rl-api-utils\.agents\teamwork\m3_explorer_1\proposed_types.go`.
   - Full proposed client implementation: `d:\code\rl-api-utils\.agents\teamwork\m3_explorer_1\proposed_client.go`.
   - Verified clean compilation with `go vet` and passed unit tests with `go test` (both streaming and buffered modes).

---

## 5. Verification Method

To independently verify the architecture and proposed implementation:

1. **Inspect Proposed Code Files**:
   - View `d:\code\rl-api-utils\.agents\teamwork\m3_explorer_1\proposed_types.go`
   - View `d:\code\rl-api-utils\.agents\teamwork\m3_explorer_1\proposed_client.go`

2. **Verify Static Analysis (`go vet`)**:
   Run in PowerShell:
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   $tmp = New-Item -ItemType Directory -Path "$env:TEMP\bc_vet_$([guid]::NewGuid())"
   Copy-Item "d:\code\rl-api-utils\.agents\teamwork\m3_explorer_1\proposed_types.go" "$tmp\types.go"
   Copy-Item "d:\code\rl-api-utils\.agents\teamwork\m3_explorer_1\proposed_client.go" "$tmp\client.go"
   Set-Location $tmp
   go mod init testpkg
   go vet .
   Set-Location "d:\code\rl-api-utils"
   Remove-Item -Recurse -Force $tmp
   ```
   *Expected Result*: Exit code 0, zero warnings.

3. **Verify Both Streaming and Buffered Modes with Unit Tests**:
   Run in PowerShell:
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   $tmp = New-Item -ItemType Directory -Path "$env:TEMP\bc_unit_$([guid]::NewGuid())"
   Copy-Item "d:\code\rl-api-utils\.agents\teamwork\m3_explorer_1\proposed_types.go" "$tmp\types.go"
   Copy-Item "d:\code\rl-api-utils\.agents\teamwork\m3_explorer_1\proposed_client.go" "$tmp\client.go"
   @'
   package ballchasing

   import (
       "context"
       "net/http"
       "net/http/httptest"
       "os"
       "path/filepath"
       "testing"
       "time"
   )

   func TestClient_StreamingAndBuffered(t *testing.T) {
       for _, stream := range []bool{false, true} {
           server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
               if r.Header.Get("Authorization") != "valid-token" {
                   w.WriteHeader(http.StatusUnauthorized)
                   return
               }
               if r.URL.Path == "/" {
                   w.WriteHeader(http.StatusOK)
                   return
               }
               if r.URL.Path == "/v2/upload" {
                   if r.URL.Query().Get("visibility") != "public" {
                       w.WriteHeader(http.StatusBadRequest)
                       return
                   }
                   f, h, err := r.FormFile("file")
                   if err != nil || h.Filename != "match1.replay" {
                       w.WriteHeader(http.StatusBadRequest)
                       return
                   }
                   f.Close()
                   w.WriteHeader(http.StatusCreated)
                   w.Write([]byte(`{"id":"bc-101","location":"https://ballchasing.com/replay/bc-101"}`))
               }
           }))
           defer server.Close()

           client, err := NewClient(ClientConfig{
               BaseURL:      server.URL,
               APIKey:       "valid-token",
               Visibility:   "public",
               StreamUpload: stream,
           })
           if err != nil {
               t.Fatalf("NewClient failed: %v", err)
           }

           if err := client.Ping(context.Background()); err != nil {
               t.Fatalf("Ping failed: %v", err)
           }

           tmpFile := filepath.Join(t.TempDir(), "test.replay")
           _ = os.WriteFile(tmpFile, []byte("TAGAME sample replay data"), 0644)

           res, err := client.UploadReplay(context.Background(), "match1", tmpFile)
           if err != nil {
               t.Fatalf("UploadReplay failed: %v", err)
           }
           if res.ID != "bc-101" || res.IsDuplicate {
               t.Fatalf("unexpected res: %+v", res)
           }
       }
   }
   '@ | Out-File -FilePath "$tmp\client_test.go" -Encoding utf8
   Set-Location $tmp
   go mod init testpkg
   go test -v .
   Set-Location "d:\code\rl-api-utils"
   Remove-Item -Recurse -Force $tmp
   ```
   *Expected Result*: Both streaming and buffered test runs pass with `PASS` and exit code 0.

4. **Verify Existing Workspace Tests Unaffected**:
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   cd d:\code\rl-api-utils
   go test -count=1 ./internal/...
   ```
   *Expected Result*: All 5 existing packages pass with `ok`.
