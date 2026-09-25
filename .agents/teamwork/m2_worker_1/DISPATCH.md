# Dispatch: m2_worker_1

**Milestone**: M2 - Auth & PsyNet Integration
**Role**: Implementation Worker (internal/auth & internal/psynet)

## Objectives
Implement the complete authentication and PsyNet integration subsystems for Milestone 2:
1. `internal/auth`:
   - `provider.go`: `AuthProvider` interface, `TokenInfo` struct with expiration check
   - `epic.go`: `EpicAuthProvider` with code/refresh token exchange, EOS token exchange, and StateStore persistence
   - `steam.go`: `SteamAuthProvider` with Steam session ticket exchange and SteamID64 validation
   - `auth_test.go`: Complete unit test suite using mock EGS client
   (Reference: `d:\code\rl-api-utils\.agents\teamwork\m2_explorer_1\handoff.md`)

2. `internal/psynet`:
   - `client.go`: `MatchHistoryProvider` implementation (`GetRecentMatches`, `Close`), session lifecycle, delayed ReplayURL handling, and auto-reconnect
   - `client_test.go`: Complete unit test suite
   - `downloader.go`: `ReplayDownloader` implementation (`DownloadReplay`), streaming HTTP GET to atomic `.tmp` in destDir, size validation (>1KB), atomic `os.Rename` with Windows retry loop, temp file cleanup
   - `downloader_test.go`: Complete unit test suite against `testutil.MockCDNServer`
   (References: `d:\code\rl-api-utils\.agents\teamwork\m2_explorer_2\handoff.md` and `d:\code\rl-api-utils\.agents\teamwork\m2_explorer_3\handoff.md`)

3. Compatibility update:
   - In `internal/testutil/mock_psynet.go`, ensure `handleAuthPlayer` encodes both `"Result": resp` and flat `resp` fields for seamless compatibility with `rlapi.NewPsyNet().AuthPlayer(...)`.

4. Dependencies:
   - Ensure `go.mod` includes `github.com/dank/rlapi` and `github.com/gorilla/websocket`. Run `go mod tidy`.

5. Verification:
   - Run tests:
     ```powershell
     $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
     cd d:\code\rl-api-utils
     go test -v -count=1 ./internal/auth/...
     go test -v -count=1 ./internal/psynet/...
     go test -v -count=1 ./internal/testutil/...
     go test -count=1 ./...
     go vet ./internal/auth/... ./internal/psynet/...
     ```
   - Ensure 100% tests pass and zero vet warnings.

6. MANDATORY INTEGRITY WARNING:
   DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A forensic auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

7. Report completion to `d:\code\rl-api-utils\.agents\teamwork\m2_worker_1\handoff.md` and notify parent via `send_message`.

## 2026-09-24T20:45:24Z
You are m2_worker_1.
Your working directory is: d:\code\rl-api-utils\.agents\teamwork\m2_worker_1
Read d:\code\rl-api-utils\.agents\teamwork\ORIGINAL_REQUEST.md, d:\code\rl-api-utils\PROJECT.md, and d:\code\rl-api-utils\.agents\teamwork\m2_worker_1\DISPATCH.md.

Implement Milestone 2 (Auth & PsyNet Integration):
1. Review the explorer handoffs:
   - Auth: d:\code\rl-api-utils\.agents\teamwork\m2_explorer_1\handoff.md (source code for internal/auth/provider.go, epic.go, steam.go, auth_test.go)
   - PsyNet Client: d:\code\rl-api-utils\.agents\teamwork\m2_explorer_2\handoff.md (source code for internal/psynet/client.go, client_test.go, and mock_psynet.go update)
   - Replay Downloader: d:\code\rl-api-utils\.agents\teamwork\m2_explorer_3\handoff.md (source code for internal/psynet/downloader.go, downloader_test.go)

2. Exclusive write ownership:
   - go.mod / go.sum
   - internal/auth/provider.go
   - internal/auth/epic.go
   - internal/auth/steam.go
   - internal/auth/auth_test.go
   - internal/psynet/client.go
   - internal/psynet/client_test.go
   - internal/psynet/downloader.go
   - internal/psynet/downloader_test.go
   - internal/testutil/mock_psynet.go

3. Add dependencies to go.mod (github.com/dank/rlapi, github.com/gorilla/websocket) and run go mod tidy.

4. Implement all packages and unit test suites.

5. Execute verification commands:
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   cd d:\code\rl-api-utils
   go test -v -count=1 ./internal/auth/...
   go test -v -count=1 ./internal/psynet/...
   go test -v -count=1 ./internal/testutil/...
   go test -count=1 ./...
   go vet ./internal/auth/... ./internal/psynet/...
   Ensure 100% test pass on all tests.

6. MANDATORY INTEGRITY WARNING:
   DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

7. Write your report to d:\code\rl-api-utils\.agents\teamwork\m2_worker_1\handoff.md and notify parent via send_message.

