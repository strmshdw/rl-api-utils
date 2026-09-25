# Dispatch: m5_worker_2

**Milestone**: M5 - Final Milestone & Hardening
**Role**: Adversarial Hardening Worker

## Objectives
Harden `internal/ballchasing/client.go`:
In `doUploadAttempt` (around line 427-432):
Ensure that HTTP 5xx responses always wrap `ErrServerError`, even on the final attempt when retries are exhausted (`attempt >= c.maxRetries`), so that `errors.Is(err, ErrServerError)` holds true.

Specifically:
```go
	default:
		if resp.StatusCode >= 500 {
			if attempt < c.maxRetries {
				wait := c.calculateBackoff(attempt)
				return nil, wait, true, fmt.Errorf("%w: HTTP %d: %s", ErrServerError, resp.StatusCode, string(respBytes))
			}
			return nil, 0, false, fmt.Errorf("%w: HTTP %d (retries exhausted): %s", ErrServerError, resp.StatusCode, string(respBytes))
		}
		return nil, 0, false, fmt.Errorf("ballchasing: unexpected HTTP status %d: %s", resp.StatusCode, string(respBytes))
```

## Exclusive Write Ownership
- `internal/ballchasing/client.go`

## Verification Commands
```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
cd d:\code\rl-api-utils
go test -v -count=1 ./internal/ballchasing/...
go test -v -count=1 ./test/e2e/...
go test -count=1 ./...
go vet ./...
```
Ensure 100% test pass on all tests and zero `go vet` warnings.

Write your report to `d:\code\rl-api-utils\.agents\teamwork\m5_worker_2\handoff.md` and notify parent via `send_message`.

## 2026-09-25T05:01:10Z
You are m5_worker_2.
Your working directory is: d:\code\rl-api-utils\.agents\teamwork\m5_worker_2
Read d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md, d:\code\rl-api-utils\PROJECT.md, and d:\code\rl-api-utils\.agents\teamwork\m5_worker_2\DISPATCH.md.

Harden internal/ballchasing/client.go:
In doUploadAttempt around line 427-432, ensure HTTP >= 500 status codes wrap ErrServerError on all attempts, including the final attempt when retries are exhausted.

Exclusive write ownership:
internal/ballchasing/client.go

Verification commands:
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
cd d:\code\rl-api-utils
go test -v -count=1 ./internal/ballchasing/...
go test -v -count=1 ./test/e2e/...
go test -count=1 ./...
go vet ./...
Ensure 100% test pass on all tests and zero vet warnings.
