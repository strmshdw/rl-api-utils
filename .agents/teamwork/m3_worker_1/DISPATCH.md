# Dispatch: m3_worker_1

**Milestone**: M3 - Ballchasing Replay Uploader
**Role**: Implementation Worker (internal/ballchasing)

## Objectives
Implement the complete Ballchasing client subsystem for Milestone 3:
1. `internal/ballchasing/types.go`:
   - `UploadResult` (`ID`, `Location`, `IsDuplicate`).
   - `ReplayUploader` interface (`UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error)`).
   - `Client` configuration types (`ClientConfig`, `Visibility`, options).
   - Sentinel errors (`ErrInvalidAPIKey`, `ErrRateLimitExhausted`, `ErrEmptyFilePath`, `ErrEmptyMatchGUID`, `ErrInvalidVisibility`, `ErrEmptyFile`).
   (Reference: `d:\code\rl-api-utils\.agents\teamwork\m3_explorer_1\proposed_types.go` and `d:\code\rl-api-utils\.agents\teamwork\m3_explorer_2\handoff.md`).

2. `internal/ballchasing/client.go`:
   - `NewClient(apiKey string, opts ...Option) *Client`.
   - `UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error)`.
   - `Ping(ctx context.Context) error`.
   - Multipart form construction with file part name `"file"`, filename `<matchGUID>.replay`, and query parameter `visibility`.
   - Raw Authorization header: `Authorization: <apiKey>` (strictly WITHOUT `Bearer ` prefix).
   - Status code handling:
     - 201 Created -> `UploadResult{ID: id, Location: loc, IsDuplicate: false}`, nil error.
     - 409 Conflict -> `UploadResult{ID: id, Location: loc, IsDuplicate: true}`, nil error (idempotent, 0 retries).
     - 429 Too Many Requests -> parse `Retry-After` (integer seconds & HTTP-date), exponential backoff with retry budget (default 3 retries), context-aware sleep.
     - 401 Unauthorized -> immediate fatal `ErrInvalidAPIKey` (0 retries).
     - 400 Bad Request -> immediate descriptive error (0 retries).
     - 5xx Server Error -> transient retry within budget.
   - Windows file descriptor safety: file handle closed immediately after reading inside each attempt; no open handles during backoff sleep.
   (Reference: `d:\code\rl-api-utils\.agents\teamwork\m3_explorer_1\proposed_client.go` and `d:\code\rl-api-utils\.agents\teamwork\m3_explorer_2\handoff.md`).

3. `internal/ballchasing/client_test.go`:
   - Comprehensive unit test suite utilizing `testutil.NewMockBallchasingServer`.
   (Reference: `d:\code\rl-api-utils\.agents\teamwork\m3_explorer_3\proposed_client_test.go`).

4. Exclusive write ownership:
   - `internal/ballchasing/types.go`
   - `internal/ballchasing/client.go`
   - `internal/ballchasing/client_test.go`
   Do NOT modify any files outside `internal/ballchasing`.

5. Verification:
   - Run tests:
     ```powershell
     $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
     cd d:\code\rl-api-utils
     go test -v -count=1 ./internal/ballchasing/...
     go test -count=1 ./...
     go vet ./internal/ballchasing/...
     ```
   - Ensure 100% test pass on all tests and zero vet warnings.

6. MANDATORY INTEGRITY WARNING:
   DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

7. Report completion to `d:\code\rl-api-utils\.agents\teamwork\m3_worker_1\handoff.md` and notify parent via `send_message`.

## 2026-09-25T04:06:28Z
Implement Milestone 3 (Ballchasing Replay Uploader):
1. Review the explorer handoffs and proposals.
2. Exclusive write ownership: internal/ballchasing/types.go, internal/ballchasing/client.go, internal/ballchasing/client_test.go.
3. Implement types.go, client.go, and client_test.go.
4. Execute verification commands (go test, go vet).
5. Mandatory Integrity Warning.
6. Write handoff.md and notify parent via send_message.
