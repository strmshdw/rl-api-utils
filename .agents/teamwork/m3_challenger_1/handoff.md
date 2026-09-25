# Adversarial Challenge Report: Ballchasing Response Handling

**Agent**: `m3_challenger_1` (Empirical Challenger / Critic / Specialist)  
**Milestone**: M3 — Ballchasing Replay Uploader (`internal/ballchasing`)  
**Verdict**: **APPROVE**  
**Date**: 2026-09-25T04:13:00Z  

---

## 1. Observation

Direct examination and empirical test execution were conducted against `internal/ballchasing/client.go`, `internal/ballchasing/types.go`, and the dedicated adversarial challenge suite `internal/ballchasing/challenge_test.go`.

### 1.1 Source Code Observations
1. **409 Conflict Handling (`client.go:385-403`)**:
   ```go
   case http.StatusConflict:
       // 409 Conflict: Deduplication event. Returns existing ID and Location without error!
       var res rawUploadResponse
       if err := json.Unmarshal(respBytes, &res); err != nil {
           return nil, 0, false, fmt.Errorf("ballchasing: decoding 409 response: %w", err)
       }
       loc := res.Location
       if loc == "" {
           loc = resp.Header.Get("Location")
       }
       if loc == "" && res.ID != "" {
           loc = fmt.Sprintf("https://ballchasing.com/replay/%s", res.ID)
       }
       return &UploadResult{
           ID:          res.ID,
           Location:    loc,
           IsDuplicate: true,
       }, 0, false, nil
   ```
   Observation: The 3rd return argument (`retryable`) is `false`, and `err` is `nil`. In `UploadReplay` (`client.go:294-296`), `if !retryable { return result, err }` terminates immediately on attempt 0 without sleeping or retrying.

2. **401 Unauthorized Handling (`client.go:412-414`)**:
   ```go
   case http.StatusUnauthorized:
       // 401 Unauthorized: Immediate fatal rejection without retries
       return nil, 0, false, ErrInvalidAPIKey
   ```
   Observation: `retryable` is `false`, and `ErrInvalidAPIKey` is returned immediately.

3. **400 Bad Request Handling (`client.go:415-422`)**:
   ```go
   case http.StatusBadRequest:
       // 400 Bad Request: Non-retryable client error
       errMsg := extractErrorMessage(respBytes)
       if errMsg == "" {
           errMsg = "bad request"
       }
       return nil, 0, false, fmt.Errorf("%w: HTTP 400 Bad Request: %s", ErrBadRequest, errMsg)
   ```
   Observation: `retryable` is `false`, returned error wraps `ErrBadRequest` via `%w`, and descriptive message from the response body is preserved.

4. **Raw Authorization Header & Bearer Rejection (`client.go:226`, `client.go:481`, `client.go:506`)**:
   - `req.Header.Set("Authorization", c.apiKey)` passes the configured token verbatim without adding a `"Bearer "` prefix.
   - When a user configures `"Bearer <key>"`, it is forwarded verbatim to Ballchasing (or `testutil.MockBallchasingServer`), which rejects it with HTTP 401.

5. **Ping Method (`client.go:218-245`)**:
   - Queries `GET /` with raw `Authorization` header.
   - Status 200 returns `nil`.
   - Status 401 returns `ErrInvalidAPIKey`.
   - Other status codes return descriptive error.

### 1.2 Verification Commands & Empirical Results
Executed empirical test suite specifically measuring HTTP request counts and responses:

```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
cd d:\code\rl-api-utils
go test -v -count=1 -run "TestChallenge" ./internal/ballchasing/...
```
Output:
```
=== RUN   TestChallenge_409Conflict_ZeroRetries
=== RUN   TestChallenge_409Conflict_ZeroRetries/streaming=false
=== RUN   TestChallenge_409Conflict_ZeroRetries/streaming=true
--- PASS: TestChallenge_409Conflict_ZeroRetries (0.02s)
    --- PASS: TestChallenge_409Conflict_ZeroRetries/streaming=false (0.01s)
    --- PASS: TestChallenge_409Conflict_ZeroRetries/streaming=true (0.00s)
=== RUN   TestChallenge_401Unauthorized_ZeroRetries
=== RUN   TestChallenge_401Unauthorized_ZeroRetries/streaming=false
=== RUN   TestChallenge_401Unauthorized_ZeroRetries/streaming=true
--- PASS: TestChallenge_401Unauthorized_ZeroRetries (0.01s)
    --- PASS: TestChallenge_401Unauthorized_ZeroRetries/streaming=false (0.01s)
    --- PASS: TestChallenge_401Unauthorized_ZeroRetries/streaming=true (0.00s)
=== RUN   TestChallenge_400BadRequest_ZeroRetries
=== RUN   TestChallenge_400BadRequest_ZeroRetries/json_error_payload
=== RUN   TestChallenge_400BadRequest_ZeroRetries/plaintext_error_payload
--- PASS: TestChallenge_400BadRequest_ZeroRetries (0.01s)
    --- PASS: TestChallenge_400BadRequest_ZeroRetries/json_error_payload (0.01s)
    --- PASS: TestChallenge_400BadRequest_ZeroRetries/plaintext_error_payload (0.00s)
=== RUN   TestChallenge_BearerPrefix_RejectedAs401
--- PASS: TestChallenge_BearerPrefix_RejectedAs401 (0.01s)
=== RUN   TestChallenge_Ping_ResponseHandling
=== RUN   TestChallenge_Ping_ResponseHandling/200_OK_returns_nil_error
=== RUN   TestChallenge_Ping_ResponseHandling/401_Unauthorized_returns_ErrInvalidAPIKey
=== RUN   TestChallenge_Ping_ResponseHandling/non-401_non-200_returns_descriptive_status_error
--- PASS: TestChallenge_Ping_ResponseHandling (0.00s)
    --- PASS: TestChallenge_Ping_ResponseHandling/200_OK_returns_nil_error (0.00s)
    --- PASS: TestChallenge_Ping_ResponseHandling/401_Unauthorized_returns_ErrInvalidAPIKey (0.00s)
    --- PASS: TestChallenge_Ping_ResponseHandling/non-401_non-200_returns_descriptive_status_error (0.00s)
PASS
ok  	github.com/dank/rl-api-utils/internal/ballchasing	1.034s
```

`go vet` check:
```powershell
go vet ./internal/ballchasing/...
```
Output: Exit code 0, 0 warnings.

Full regression test across entire workspace:
```powershell
go test -count=1 ./...
```
Output:
- `internal/auth`: PASS
- `internal/ballchasing`: PASS (24 tests total)
- `internal/config`: PASS
- `internal/psynet`: PASS
- `internal/storage`: PASS
- `internal/testutil`: PASS
- `test/e2e`: PASS (All Tiers 1-4 pass 100%)

---

## 2. Logic Chain

1. **409 Conflict Invariant**:
   - `TestChallenge_409Conflict_ZeroRetries` configured the client with `MaxRetries: 5`. An atomic request counter tracked inbound HTTP requests to `httptest.Server`.
   - Result: `requestCount == 1`, `res.IsDuplicate == true`, `res.ID == "existing-guid-409-challenge"`, `res.Location == "https://ballchasing.com/replay/existing-guid-409-challenge"`, and `err == nil`. Tested in both buffered and streaming modes. This confirms deduplication is strictly non-retryable and treated as successful idempotent completion.

2. **401 Unauthorized Invariant**:
   - `TestChallenge_401Unauthorized_ZeroRetries` configured the client with `MaxRetries: 5`.
   - Result: `requestCount == 1`, `res == nil`, `errors.Is(err, ErrInvalidAPIKey) == true`. Tested in both buffered and streaming modes. This confirms unauthorized requests immediately halt without retry thrashing.

3. **400 Bad Request Invariant**:
   - `TestChallenge_400BadRequest_ZeroRetries` configured the client with `MaxRetries: 5` and tested both JSON error envelopes (`{"error": "replay CRC check failed: corrupt payload"}`) and raw text error payloads (`malformed header structure`).
   - Result: `requestCount == 1`, `res == nil`, `errors.Is(err, ErrBadRequest) == true`, and verbatim error text was present in `err.Error()`. This confirms permanent client bad requests halt immediately and preserve error diagnostics.

4. **Bearer Prefix Rejection Invariant**:
   - `TestChallenge_BearerPrefix_RejectedAs401` configured the client with `APIKey: "Bearer super-secret-token-123"`.
   - Result: Both `UploadReplay` and `Ping` sent verbatim `"Bearer super-secret-token-123"` in the `Authorization` header. The server rejected the request with 401 Unauthorized. The client returned `ErrInvalidAPIKey` for both operations.

5. **Ping(ctx) Invariant**:
   - `TestChallenge_Ping_ResponseHandling` asserted:
     - 200 OK returns `err == nil` and makes exactly 1 request.
     - 401 Unauthorized returns `errors.Is(err, ErrInvalidAPIKey) == true` and makes exactly 1 request.
     - 503 Service Unavailable returns non-nil error wrapping the status code and text without falsely classifying as `ErrInvalidAPIKey`.

---

## 3. Caveats

- **Network-level transport drops**: Client handles TCP connection resets or timeouts via `attempt < maxRetries` exponential backoff; this is standard transient recovery. Fatal HTTP statuses (400, 401, 409) are non-retryable at the HTTP protocol layer as verified.
- **No other caveats.**

---

## 4. Conclusion

The implementation of `internal/ballchasing` under M3 satisfies all adversarial challenges and edge-case requirements:
- **Verdict**: **APPROVE**
- All 5 challenge scenarios passed empirically with strict verification of zero retry attempts (`requestCount == 1`), expected error sentinel unwrapping, payload integrity, and header conformance.
- Zero regressions across the workspace.

---

## 5. Verification Method

To independently reproduce the adversarial challenge results:

1. **Run Adversarial Challenge Tests**:
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   cd d:\code\rl-api-utils
   go test -v -count=1 -run "TestChallenge" ./internal/ballchasing/...
   ```
   *Expected Result*: All 5 challenge test functions (and all subtests) PASS, exit code 0.

2. **Run Package Tests**:
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   cd d:\code\rl-api-utils
   go test -v -count=1 ./internal/ballchasing/...
   ```
   *Expected Result*: 24 test functions PASS, exit code 0.

3. **Run Full Workspace Suite**:
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   cd d:\code\rl-api-utils
   go test -count=1 ./...
   ```
   *Expected Result*: All packages pass with 100% success.
