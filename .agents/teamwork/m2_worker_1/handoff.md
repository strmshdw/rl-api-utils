# Milestone 2 (Auth & PsyNet Integration) Implementation Handoff Report

- **Author**: `m2_worker_1` (Roles: implementer, qa, specialist)
- **Date**: 2026-09-24T20:52:00Z
- **Target Subsystems**: `internal/auth`, `internal/psynet`, `internal/testutil/mock_psynet.go`, `go.mod`
- **Status**: COMPLETE (100% Passing Tests, 0 `go vet` Warnings)
- **Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m2_worker_1`

---

## 1. Observation

Direct inspection of requirements, dependencies, existing code, and execution outputs revealed the following facts:

1. **Interface & Architectural Contracts**:
   - `ORIGINAL_REQUEST.md` lines 25–30 (R4) and lines 16–18 (R1): Requires Epic Games auth (code and refresh token exchange) and Steam auth (session ticket exchange and SteamID64 validation); periodic polling of match history; download of `.replay` files to local storage.
   - `PROJECT.md` lines 140–165: Specifies the domain interfaces:
     ```go
     type DiscoveredMatch struct {
         MatchGUID            string
         RecordStartTimestamp int64
         MapName              string
         Playlist             int
         ReplayURL            string
     }
     type MatchHistoryProvider interface {
         GetRecentMatches(ctx context.Context) ([]DiscoveredMatch, error)
         Close() error
     }
     type ReplayDownloader interface {
         DownloadReplay(ctx context.Context, matchGUID, replayURL, destDir string) (localPath string, err error)
     }
     ```
   - `internal/storage/store.go` lines 100–103: `SaveAuthState` and `GetAuthState` interfaces for persistent credentials storage.

2. **Upstream SDK Behavior (`github.com/dank/rlapi`)**:
   - `EGS` (`github.com/dank/rlapi`): Methods `AuthenticateWithCode`, `AuthenticateWithRefreshToken`, `GetExchangeCode`, `ExchangeEOSToken`, `ExchangeEOSTokenFromSteam`, and `RefreshEOSToken`.
   - `PsyNet` (`github.com/dank/rlapi`): `AuthPlayer(authToken, accountID, accountName)` and `AuthPlayerSteam(authToken, epicAccountID, steamAccountID, accountName)` establish authenticated WebSocket RPC connection (`*PsyNetRPC`).
   - `rlapi.postJSON`: Expects responses to contain `{"Result": {...}}`. In `internal/testutil/mock_psynet.go:212`, `handleAuthPlayer` originally emitted unnested JSON. Wrapping the response with `"Result": resp` alongside top-level keys maintains dual compatibility with both `rlapi` and direct mock unit tests.

3. **Windows File System Semantics**:
   - `os.Rename` on Windows returns `ERROR_SHARING_VIOLATION` if the file descriptor is still open. Thus `tmpFile.Sync()` and `tmpFile.Close()` must strictly precede `os.Rename`.
   - Transient file locks by Windows Defender or search indexers necessitate a retry loop with linear backoff (5 attempts).
   - Staging the `.tmp` file in the same directory (`destDir`) avoids cross-volume `EXDEV` link errors.

4. **Test Suite Verification Results**:
   - `go test -v -count=1 ./internal/auth/...`: 13 unit tests passed in 0.086s.
   - `go test -v -count=1 ./internal/psynet/...`: 20 unit tests passed in 0.793s.
   - `go test -v -count=1 ./internal/testutil/...`: 3 unit tests passed in 0.929s.
   - `go test -count=1 ./...`: All packages passed 100%.
   - `go vet ./internal/auth/... ./internal/psynet/...`: Completed with 0 warnings or errors.

---

## 2. Logic Chain

From the observations above, the implementation decisions were derived step-by-step:

1. **`internal/auth` Implementation**:
   - `provider.go`: Defines `TokenInfo` (with thread-safe helper `IsExpired()` using a 30s buffer), `AuthProvider` interface, `EGSClient` interface (matched by `*rlapi.EGS`), and constructor `NewProvider` which dispatches to Epic or Steam providers based on `cfg.Provider`.
   - `epic.go`: Implements `EpicAuthProvider`. Authenticates via refresh token or auth code; falls back to `StateStore.GetAuthState` if no tokens are configured; exchanges for EOS token; persists newest refresh token via `StateStore.SaveAuthState`; protects in-memory `TokenInfo` with `sync.RWMutex`.
   - `steam.go`: Implements `SteamAuthProvider`. Validates SteamID64 (strictly 17 digits starting with `7656119`); exchanges Steam session ticket for EOS token via `ExchangeEOSTokenFromSteam`; records state to `StateStore`; supports EOS token refresh or returns a descriptive error when session ticket renewal is required.
   - `auth_test.go`: 13 comprehensive unit tests exercising successful auth with refresh token/code/store restore, error conditions (OAuth failure, exchange rate limit, context cancellation), Steam ticket validation, and token expiry calculations without network dependencies.

2. **`internal/psynet` Implementation**:
   - `client.go`: Implements `MatchHistoryProvider`. Encapsulates `RPCClient` (`*rlapi.PsyNetRPC`). Manages connection lifecycle; provides auto-reconnect and single-retry upon in-flight network drops; maps matches preserving empty `ReplayURL` so syncer can handle delayed replay URL arrivals across polling cycles; filters out empty/malformed `MatchGUID`s; ensures thread-safe and idempotent `Close()`.
   - `client_test.go`: 8 unit tests covering normal retrieval, delayed replay URL arrival across cycles, malformed GUID skipping, transparent in-flight reconnect, context cancellation, idempotent close, Steam credentials validation, and end-to-end wire protocol against `MockPsyNetServer`.
   - `downloader.go`: Implements `ReplayDownloader`. Enforces strict parameter validation (empty GUID/URL, path traversal prevention); creates `destDir` if missing; streams GET payload to `.tmp` file directly inside `destDir`; validates minimum size (>=1024 bytes); flushes via `Sync()`; explicitly closes handle before Windows atomic rename; applies 5-attempt retry loop with linear backoff; guarantees deferred cleanup of `.tmp` file on any failure or cancellation; provides `CleanupStaleTempFiles`.
   - `downloader_test.go`: 12 unit tests covering successful download with TAGAME verification, auto-creation of nested directories, input validation, 1KB rejection, 1024-byte boundary, HTTP status errors (403, 404, 410, 500, 503), mid-stream connection drops, context cancellation, atomic overwrite, custom headers/buffers, concurrent downloads, and stale temp file cleanup.

3. **Compatibility Update in `internal/testutil/mock_psynet.go`**:
   - Updated `handleAuthPlayer` to encode `{"Result": resp, "SessionID": ..., ...}` ensuring `rlapi.postJSON` receives the `"Result"` wrapper while preserving existing flat fields for other tests.

4. **Dependency Resolution**:
   - Added `github.com/dank/rlapi` v0.1.26 and `github.com/gorilla/websocket` v1.5.3 to `go.mod` and ran `go mod tidy`.

---

## 3. Caveats

1. **Steam Ticket Lifetime**:
   - Steam session tickets are ephemeral and cannot be refreshed by OAuth grant without an active Steam client. When a ticket expires and has no EOS refresh grant, `SteamAuthProvider.Refresh` returns `ErrRefreshFailed` with an explicit message that a fresh ticket is required.
2. **Replay Payload Validation**:
   - `ReplayDownloader` validates size (>=1KB) and non-empty content. Full binary AST decoding of Unreal Engine structures is intentionally delegated to Ballchasing.com.
3. **No Unrelated Refactoring**:
   - All edits were strictly constrained to the assigned files within exclusive write ownership.

---

## 4. Conclusion

Milestone 2 (Auth & PsyNet Integration) is completely implemented, verified, and adheres to Clean Architecture:
- `internal/auth`: Full dual provider implementation (Epic Games and Steam) with `StateStore` persistence and EGS mockability.
- `internal/psynet`: Full PsyNet client with auto-reconnect, delayed replay URL handling, and atomic `.tmp` disk streaming replay downloader.
- `internal/testutil/mock_psynet.go`: Dual compatibility update.
- `go.mod` & `go.sum`: Dependencies tidied and validated.
- 100% of unit tests pass with zero `go vet` warnings across all affected packages.

---

## 5. Verification Method

To independently verify the implementation:

1. **Run Unit Tests for Milestone 2 Packages**:
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   cd d:\code\rl-api-utils
   go test -v -count=1 ./internal/auth/...
   go test -v -count=1 ./internal/psynet/...
   go test -v -count=1 ./internal/testutil/...
   ```
   *Expected Result*: All 13 tests in `internal/auth`, 20 tests in `internal/psynet`, and 3 tests in `internal/testutil` pass cleanly.

2. **Run Full Workspace Test Suite**:
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   cd d:\code\rl-api-utils
   go test -count=1 ./...
   ```
   *Expected Result*: Exit code 0, all packages report `ok`.

3. **Run Static Analysis (go vet)**:
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   cd d:\code\rl-api-utils
   go vet ./internal/auth/... ./internal/psynet/...
   ```
   *Expected Result*: Exit code 0 with zero warnings.

4. **Verify Test Coverage**:
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   cd d:\code\rl-api-utils
   go test -cover ./internal/auth/... ./internal/psynet/...
   ```
   *Expected Result*: `internal/auth` ~77.0%, `internal/psynet` ~75.8% coverage.
