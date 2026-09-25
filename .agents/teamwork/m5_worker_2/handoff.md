# Handoff Report: m5_worker_2 - Ballchasing Client 5xx Error Hardening

## 1. Observation
- In `internal/ballchasing/client.go` lines 426-432, `doUploadAttempt` previously handled default status codes as:
  ```go
  default:
      if resp.StatusCode >= 500 && attempt < c.maxRetries {
          wait := c.calculateBackoff(attempt)
          return nil, wait, true, fmt.Errorf("%w: HTTP %d: %s", ErrServerError, resp.StatusCode, string(respBytes))
      }
      return nil, 0, false, fmt.Errorf("ballchasing: unexpected HTTP status %d: %s", resp.StatusCode, string(respBytes))
  ```
- When `resp.StatusCode >= 500` and `attempt >= c.maxRetries` (the final retry attempt), the condition `resp.StatusCode >= 500 && attempt < c.maxRetries` evaluated to false. The execution fell through to return `fmt.Errorf("ballchasing: unexpected HTTP status %d: %s", resp.StatusCode, string(respBytes))`, which did not wrap `ErrServerError`.
- Prior to modification, running `go test -v -count=1 ./test/e2e/... -run TestTier5_Stress_RetryBudgetExhaustion_GracefulSurfacing` logged:
  ```
  tier5_stress_test.go:695: [DEFECT-FINDING] ballchasing.Client: HTTP 5xx on final attempt (ballchasing: unexpected HTTP status 500: {"error":"internal error"}) fails to wrap ErrServerError
  ```

## 2. Logic Chain
1. `UploadReplay` loops through attempts `0 <= attempt <= c.maxRetries` and delegates each attempt to `doUploadAttempt`.
2. When the server responds with HTTP 5xx (e.g., 500 or 503) on the final attempt (`attempt == c.maxRetries`), the caller expects `errors.Is(err, ErrServerError)` to evaluate to `true`.
3. In `internal/ballchasing/client.go`, we updated `doUploadAttempt` to branch explicitly when `resp.StatusCode >= 500`:
   - If `attempt < c.maxRetries`: returns `retryable=true`, calculates exponential/jitter backoff, and wraps `ErrServerError`.
   - If `attempt >= c.maxRetries`: returns `retryable=false`, `wait=0`, and wraps `ErrServerError` with:
     ```go
     return nil, 0, false, fmt.Errorf("%w: HTTP %d (retries exhausted): %s", ErrServerError, resp.StatusCode, string(respBytes))
     ```
4. This ensures that callers using `errors.Is(err, ErrServerError)` or `errors.Is(err, ballchasing.ErrServerError)` reliably detect server errors across all attempts, including retry exhaustion.
5. In `test/e2e/tier5_stress_test.go`, the test condition `errors.Is(err, ballchasing.ErrServerError)` now evaluates to `true`, resolving the defect finding completely.

## 3. Caveats
- No caveats. The edit is strictly scoped to `internal/ballchasing/client.go` under exclusive write ownership.

## 4. Conclusion
- `internal/ballchasing/client.go` is hardened to wrap `ErrServerError` for all HTTP >= 500 responses across all attempts, including the final attempt when retries are exhausted.
- 100% test pass rate achieved across all unit and end-to-end tests (`go test -count=1 ./...`).
- Zero `go vet` warnings.

## 5. Verification Method
Execute the following verification commands from the project root `d:\code\rl-api-utils`:

```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
cd d:\code\rl-api-utils
go test -v -count=1 ./internal/ballchasing/...
go test -v -count=1 ./test/e2e/...
go test -count=1 ./...
go vet ./...
```

**Verification Results**:
- `go test -v -count=1 ./internal/ballchasing/...`: PASS (all unit tests pass)
- `go test -v -count=1 ./test/e2e/...`: PASS (all Tier 1-5 tests pass; `TestTier5_Stress_RetryBudgetExhaustion_GracefulSurfacing` passes without defect warnings)
- `go test -count=1 ./...`: PASS (10 packages ok, 100% pass)
- `go vet ./...`: PASS (exit code 0, 0 warnings)
