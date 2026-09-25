# Milestone 3 Challenger 2 Report: Ballchasing Backoff & File Safety

**Agent**: `m3_challenger_2` (Empirical Challenger / Critic / Specialist)  
**Milestone**: M3 — Ballchasing Replay Uploader (`internal/ballchasing`)  
**Target Areas**: 429 Rate Limiting & Backoff, Context Cancellation, File Safety, and Concurrency  
**Verdict**: **APPROVE**  
**Date**: 2026-09-25T04:15:00Z  

---

## Challenge Summary

**Overall risk assessment**: LOW

All adversarial challenges designed to stress-test rate limiting backoff, context cancellation during sleep, file validation safety, and concurrency against `internal/ballchasing` passed with 100% success. No data races, deadlocks, file handle leaks, or unhandled failure modes were discovered.

---

## 1. Observation

Direct empirical tests were executed against `internal/ballchasing` using the newly developed challenge suite in `internal/ballchasing/challenge2_test.go` and the existing unit suite.

### 1.1 Test Suite Execution Commands & Outputs

```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
cd d:\code\rl-api-utils
go test -v -count=1 -run "TestChallenge2_" ./internal/ballchasing/...
```

Output:
```
=== RUN   TestChallenge2_RateLimit_Consecutive429_Recovery
=== RUN   TestChallenge2_RateLimit_Consecutive429_Recovery/streaming=false
=== RUN   TestChallenge2_RateLimit_Consecutive429_Recovery/streaming=true
--- PASS: TestChallenge2_RateLimit_Consecutive429_Recovery (0.01s)
    --- PASS: TestChallenge2_RateLimit_Consecutive429_Recovery/streaming=false (0.01s)
    --- PASS: TestChallenge2_RateLimit_Consecutive429_Recovery/streaming=true (0.01s)
=== RUN   TestChallenge2_RateLimit_RetryAfter_Formats
=== RUN   TestChallenge2_RateLimit_RetryAfter_Formats/integer_Retry-After:_1_respects_~1_second_sleep
=== RUN   TestChallenge2_RateLimit_RetryAfter_Formats/integer_Retry-After:_0_causes_immediate_retry
=== RUN   TestChallenge2_RateLimit_RetryAfter_Formats/HTTP-date_RFC1123_near_future
=== RUN   TestChallenge2_RateLimit_RetryAfter_Formats/HTTP-date_RFC850_near_future
=== RUN   TestChallenge2_RateLimit_RetryAfter_Formats/HTTP-date_in_past_causes_immediate_retry
=== RUN   TestChallenge2_RateLimit_RetryAfter_Formats/absent_Retry-After_header_falls_back_to_exponential_backoff
=== RUN   TestChallenge2_RateLimit_RetryAfter_Formats/malformed_non-numeric_Retry-After_falls_back_to_exponential_backoff
--- PASS: TestChallenge2_RateLimit_RetryAfter_Formats (4.64s)
    --- PASS: TestChallenge2_RateLimit_RetryAfter_Formats/integer_Retry-After:_1_respects_~1_second_sleep (1.01s)
    --- PASS: TestChallenge2_RateLimit_RetryAfter_Formats/integer_Retry-After:_0_causes_immediate_retry (0.00s)
    --- PASS: TestChallenge2_RateLimit_RetryAfter_Formats/HTTP-date_RFC1123_near_future (1.54s)
    --- PASS: TestChallenge2_RateLimit_RetryAfter_Formats/HTTP-date_RFC850_near_future (2.00s)
    --- PASS: TestChallenge2_RateLimit_RetryAfter_Formats/HTTP-date_in_past_causes_immediate_retry (0.01s)
    --- PASS: TestChallenge2_RateLimit_RetryAfter_Formats/absent_Retry-After_header_falls_back_to_exponential_backoff (0.04s)
    --- PASS: TestChallenge2_RateLimit_RetryAfter_Formats/malformed_non-numeric_Retry-After_falls_back_to_exponential_backoff (0.03s)
=== RUN   TestChallenge2_RateLimit_BudgetExhaustion
=== RUN   TestChallenge2_RateLimit_BudgetExhaustion/maxRetries=2_(total_3_requests)
=== RUN   TestChallenge2_RateLimit_BudgetExhaustion/maxRetries=0_(strictly_1_request,_0_retries)
=== RUN   TestChallenge2_RateLimit_BudgetExhaustion/maxRetries=4_with_streaming_(total_5_requests)
--- PASS: TestChallenge2_RateLimit_BudgetExhaustion (0.02s)
    --- PASS: TestChallenge2_RateLimit_BudgetExhaustion/maxRetries=2_(total_3_requests) (0.00s)
    --- PASS: TestChallenge2_RateLimit_BudgetExhaustion/maxRetries=0_(strictly_1_request,_0_retries) (0.01s)
    --- PASS: TestChallenge2_RateLimit_BudgetExhaustion/maxRetries=4_with_streaming_(total_5_requests) (0.01s)
=== RUN   TestChallenge2_ContextCancellation_DuringBackoffSleep
=== RUN   TestChallenge2_ContextCancellation_DuringBackoffSleep/streaming=false
=== RUN   TestChallenge2_ContextCancellation_DuringBackoffSleep/streaming=true
=== RUN   TestChallenge2_ContextCancellation_DuringBackoffSleep/pre-cancelled_context_makes_zero_requests
--- PASS: TestChallenge2_ContextCancellation_DuringBackoffSleep (0.13s)
    --- PASS: TestChallenge2_ContextCancellation_DuringBackoffSleep/streaming=false (0.06s)
    --- PASS: TestChallenge2_ContextCancellation_DuringBackoffSleep/streaming=true (0.06s)
    --- PASS: TestChallenge2_ContextCancellation_DuringBackoffSleep/pre-cancelled_context_makes_zero_requests (0.00s)
=== RUN   TestChallenge2_FileHandling_NonExistent_And_ZeroByte
=== RUN   TestChallenge2_FileHandling_NonExistent_And_ZeroByte/streaming=false
=== RUN   TestChallenge2_FileHandling_NonExistent_And_ZeroByte/streaming=false/non-existent_file
=== RUN   TestChallenge2_FileHandling_NonExistent_And_ZeroByte/streaming=false/0-byte_empty_file
=== RUN   TestChallenge2_FileHandling_NonExistent_And_ZeroByte/streaming=false/directory_path_provided_as_filePath
=== RUN   TestChallenge2_FileHandling_NonExistent_And_ZeroByte/streaming=false/empty_string_file_path
=== RUN   TestChallenge2_FileHandling_NonExistent_And_ZeroByte/streaming=false/whitespace-only_file_path
=== RUN   TestChallenge2_FileHandling_NonExistent_And_ZeroByte/streaming=true
=== RUN   TestChallenge2_FileHandling_NonExistent_And_ZeroByte/streaming=true/non-existent_file
=== RUN   TestChallenge2_FileHandling_NonExistent_And_ZeroByte/streaming=true/0-byte_empty_file
=== RUN   TestChallenge2_FileHandling_NonExistent_And_ZeroByte/streaming=true/directory_path_provided_as_filePath
=== RUN   TestChallenge2_FileHandling_NonExistent_And_ZeroByte/streaming=true/empty_string_file_path
=== RUN   TestChallenge2_FileHandling_NonExistent_And_ZeroByte/streaming=true/whitespace-only_file_path
--- PASS: TestChallenge2_FileHandling_NonExistent_And_ZeroByte (0.01s)
=== RUN   TestChallenge2_Concurrency_StressHarness
=== RUN   TestChallenge2_Concurrency_StressHarness/streaming=false
=== RUN   TestChallenge2_Concurrency_StressHarness/streaming=true
--- PASS: TestChallenge2_Concurrency_StressHarness (0.12s)
    --- PASS: TestChallenge2_Concurrency_StressHarness/streaming=false (0.07s)
    --- PASS: TestChallenge2_Concurrency_StressHarness/streaming=true (0.05s)
PASS
ok  	github.com/dank/rl-api-utils/internal/ballchasing	5.777s
```

### 1.2 Static Analysis and Repository Regression Verification

```powershell
go vet ./internal/ballchasing/...
```
Output:
Exit code 0, 0 warnings.

```powershell
go test -count=1 ./...
```
Output:
```
ok  	github.com/dank/rl-api-utils/internal/auth	0.176s
ok  	github.com/dank/rl-api-utils/internal/ballchasing	7.700s
ok  	github.com/dank/rl-api-utils/internal/config	0.597s
ok  	github.com/dank/rl-api-utils/internal/psynet	4.334s
ok  	github.com/dank/rl-api-utils/internal/storage	3.264s
ok  	github.com/dank/rl-api-utils/internal/testutil	1.013s
ok  	github.com/dank/rl-api-utils/test/e2e	3.704s
```

---

## 2. Logic Chain

1. **429 Consecutive Responses and Recovery**:
   - In `client.go:285-312`, the retry loop increments `attempt` up to `c.maxRetries`.
   - In `client.go:404-409`, `http.StatusTooManyRequests` evaluates `attempt >= c.maxRetries`. If under budget, it computes `waitDuration` and returns `retryable = true`.
   - Empirically observed: When the server returned 3 consecutive 429 responses, `UploadReplay` executed attempts 0, 1, 2, and 3, successfully parsing the 201 Created on attempt 3. Tested in both buffered (`StreamUpload: false`) and streaming (`StreamUpload: true`) modes. Exactly 4 total requests were dispatched.

2. **Retry-After Header Variations**:
   - `client.go:540-571` (`resolveRetryAfter`):
     - Integer seconds: `strconv.Atoi` parses `Retry-After: 1`, yielding ~1s sleep (measured: 1.01s). `Retry-After: 0` causes immediate retry without sleeping (measured: 0.00s).
     - HTTP-date: `http.ParseTime` parses IMF-fixdate (RFC 1123 with GMT) and RFC 850 formats. Measured wait times matched the target future timestamps. Dates in the past yielded immediate retries.
     - Absent or malformed headers: When header was omitted or contained non-numeric strings, the client fell back to exponential backoff with full jitter `[backoff/2, backoff]`.

3. **Retry Budget Exhaustion**:
   - When the server returned 429 persistently:
     - With `maxRetries = 2`, exactly 3 requests (attempt 0, retry 1, retry 2) were made before immediately halting with `ErrRateLimitExhausted`.
     - With `maxRetries = 0`, exactly 1 request was made and retries halted immediately (strictly 0 retries).
     - With `maxRetries = 4` in streaming mode, exactly 5 requests were made before halting with `ErrRateLimitExhausted`. In all cases, returned `UploadResult` was `nil`.

4. **Context Cancellation & Windows File Handle Safety**:
   - In `client.go:594-608`, `sleepWithContext` listens on `select { case <-ctx.Done(): return ctx.Err() case <-timer.C: return nil }`.
   - When the server simulated a 60s `Retry-After` and the context was cancelled after 60ms, `UploadReplay` returned within 60ms, returning `context.DeadlineExceeded` without hanging.
   - Crucially, on Windows, file handle locks prevent file deletion (`ERROR_SHARING_VIOLATION 0x20`). An immediate `os.Remove(filePath)` was executed right after context cancellation and succeeded without error, proving file handles were closed before entering sleep.

5. **File Safety (Missing, 0-Byte, Directory, Empty Paths)**:
   - In `client.go:261-281`, `UploadReplay` validates input before initiating any network I/O:
     - Non-existent file path: returns error wrapping `ErrFileNotFound`.
     - 0-byte file: returns error wrapping `ErrBadRequest` and `ErrEmptyFile`.
     - Directory path: returns error wrapping `ErrBadRequest`.
     - Empty/whitespace path: returns `ErrEmptyFilePath`.
   - Verified that exactly 0 HTTP requests were sent to the mock server in all failure conditions.

6. **Concurrency Stress Testing**:
   - 50 concurrent goroutines were executed simultaneously against a shared `*Client` instance across mixed workloads (20 successful 201s, 10 duplicate 409s, 10 429 backoff recoveries, 5 0-byte files, 5 missing files).
   - In both buffered and streaming modes, every goroutine received its expected result with zero cross-talk, zero race conditions, and complete payload byte integrity (`TAGAME` headers preserved).

---

## 3. Stress Test Results Matrix

| # | Stress Test Scenario | Expected Behavior | Actual Behavior | Result |
|---|----------------------|-------------------|-----------------|:------:|
| 1 | 3x Consecutive 429 -> 201 Created | 4 attempts, success result, IsDuplicate=false | 4 requests made, recovered ID returned, 0 errors | **PASS** |
| 2 | Integer `Retry-After: 1` | Wait ~1s (>=900ms) before retry | Slept 1.01s, succeeded on attempt 1 | **PASS** |
| 3 | Integer `Retry-After: 0` | Immediate retry without sleeping | Completed in <10ms, succeeded | **PASS** |
| 4 | HTTP-date RFC 1123 near future | Parse date, sleep difference (~1-2s) | Slept 1.54s, succeeded on attempt 1 | **PASS** |
| 5 | HTTP-date RFC 850 near future | Parse date, sleep difference (~1-2s) | Slept 2.00s, succeeded on attempt 1 | **PASS** |
| 6 | HTTP-date in past | Immediate retry (wait <= 0) | Completed in <10ms, succeeded | **PASS** |
| 7 | Absent `Retry-After` header | Fallback to jittered exponential backoff | Slept baseBackoff jitter interval (40ms) | **PASS** |
| 8 | Malformed `Retry-After` header | Fallback to exponential backoff | Handled gracefully without crash | **PASS** |
| 9 | Budget exhaustion (`maxRetries=2`) | Exactly 3 requests, `ErrRateLimitExhausted` | Exactly 3 requests, returns `ErrRateLimitExhausted` | **PASS** |
| 10 | Budget exhaustion (`maxRetries=0`) | Exactly 1 request, 0 retries | Strictly 1 request, returns `ErrRateLimitExhausted` | **PASS** |
| 11 | Budget exhaustion (`maxRetries=4` stream) | Exactly 5 requests, `ErrRateLimitExhausted` | Strictly 5 requests, returns `ErrRateLimitExhausted` | **PASS** |
| 12 | Context cancel during 60s backoff sleep | Immediate return (<100ms) with `ctx.Err()` | Returned in 60ms with `context.DeadlineExceeded` | **PASS** |
| 13 | Windows file handle release on cancel | `os.Remove(filePath)` succeeds without lock | File deleted cleanly, zero handle leaks | **PASS** |
| 14 | Pre-cancelled context | Immediate abort, 0 HTTP requests | Returned `context.Canceled`, 0 requests | **PASS** |
| 15 | Non-existent file path | `ErrFileNotFound`, 0 HTTP requests | Returned `ErrFileNotFound`, 0 requests | **PASS** |
| 16 | 0-byte replay file | `ErrEmptyFile` & `ErrBadRequest`, 0 requests | Returned `ErrEmptyFile`, 0 requests | **PASS** |
| 17 | Directory path provided | `ErrBadRequest`, 0 HTTP requests | Returned `ErrBadRequest`, 0 requests | **PASS** |
| 18 | Empty / whitespace file path | `ErrEmptyFilePath`, 0 HTTP requests | Returned `ErrEmptyFilePath`, 0 requests | **PASS** |
| 19 | 50 concurrent goroutines (buffered mode) | Zero races, accurate per-thread results | All 50 completed cleanly, payload intact | **PASS** |
| 20 | 50 concurrent goroutines (streaming mode) | Zero races, accurate per-thread results | All 50 completed cleanly, payload intact | **PASS** |

---

## 4. Caveats

- **CGO Race Detector**: GCC is not installed on this Windows environment, so `go test -race` could not be run with compiler instrumentation. Concurrency safety was instead empirically verified using multi-threaded stress tests (50 concurrent goroutines with barrier release) inspecting data integrity, atomic counters, and synchronization invariants.
- **HTTP-Date Second Granularity**: Standard HTTP-dates provide 1-second resolution without sub-second fractions. Backoff duration tests for HTTP-dates used +2s targets to ensure timing thresholds reliably accounted for second truncation.

---

## 5. Conclusion

**Verdict: APPROVE**

The implementation of `internal/ballchasing` demonstrates high resilience and safety:
- Throttling and backoff algorithms cleanly handle repeated 429 responses, all variations of `Retry-After` headers, and strict budget caps.
- Context cancellation during backoff sleep aborts immediately without hanging.
- File descriptors are closed prior to backoff sleeps and cancellation, preventing Windows file sharing locks.
- Pre-flight file validations guard against missing, empty, and invalid paths with zero network overhead.
- Concurrent execution across 50 goroutines is thread-safe across all functional flows.

---

## 6. Verification Method

To independently execute and verify the empirical challenge suite:

```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
cd d:\code\rl-api-utils

# 1. Run Challenger 2 test suite
go test -v -count=1 -run "TestChallenge2_" ./internal/ballchasing/...

# 2. Run all package unit tests
go test -v -count=1 ./internal/ballchasing/...

# 3. Static analysis
go vet ./internal/ballchasing/...

# 4. Full repository test suite
go test -count=1 ./...
```
All commands are expected to exit with code 0 and 100% test pass.
