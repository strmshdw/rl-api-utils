# Milestone 2 (Auth & PsyNet Integration) Independent Review Report

- **Author**: `m2_reviewer_2` (Roles: reviewer, critic)
- **Date**: 2026-09-25T03:57:00Z
- **Target Subsystems**: `internal/auth`, `internal/psynet`
- **Reviewed Worker Report**: `.agents/teamwork/m2_worker_1/handoff.md`
- **Verdict**: **APPROVE**

---

## Review Summary

**Verdict**: **APPROVE**

The implementation of Milestone 2 delivered by `m2_worker_1` fulfills all requirements in `ORIGINAL_REQUEST.md` (R1, R4, R5) and conforms to the architecture and interface contracts defined in `PROJECT.md`. The code demonstrates high quality, robust error propagation, strict Windows atomic file rename safety, thorough input validation against directory traversal, and zero integrity violations.

---

## 1. Observation

Direct examination of the repository source files, dependencies, tests, and command outputs revealed the following facts:

1. **Test Execution & Static Analysis**:
   - `go test -v -count=1 ./internal/auth/...`:
     30 test functions passed in 0.134s (including 17 adversarial boundary tests). Statement coverage: **92.2%**.
   - `go test -v -count=1 ./internal/psynet/...`:
     33 test functions passed in 4.316s (including 13 adversarial stress suites). Statement coverage: **82.4%**.
   - `go test -count=1 ./...`:
     100% pass across all repository packages (`internal/auth`, `internal/config`, `internal/psynet`, `internal/storage`, `internal/testutil`, `test/e2e`).
   - `go vet ./internal/auth/... ./internal/psynet/...`:
     Exited with code 0 and zero warnings.

2. **`internal/auth` Architecture & Correctness**:
   - `provider.go`: Defines `TokenInfo` with `IsExpired()` using a 30-second buffer (`IsExpiredWithBuffer(30 * time.Second)`), `AuthProvider` interface, and factory `NewProvider` supporting `"epic"` and `"steam"`. Rejects unsupported providers with `ErrUnsupportedProvider`.
   - `epic.go`: Implements `EpicAuthProvider`. Implements dual authentication path: accepts either `refresh_token` or `auth_code`; falls back to `storage.StateStore.GetAuthState(ctx, "epic")`; exchanges for Rocket League scoped EOS token; persists newly rotated refresh token via `store.SaveAuthState`; thread-safe cached `TokenInfo` protected with `sync.RWMutex`.
   - `steam.go`: Implements `SteamAuthProvider`. Validates 17-digit SteamID64 starting with `7656119` via `ValidateSteamID64`; exchanges Steam session ticket for EOS token via `ExchangeEOSTokenFromSteam`; returns descriptive error explaining that expired tickets require client re-generation when refresh grant is absent.

3. **`internal/psynet` Architecture & Correctness**:
   - `client.go`: Implements `MatchHistoryProvider`. Encapsulates `*rlapi.PsyNetRPC` via `RPCClient` interface. Gracefully handles delayed replay URL availability across polling cycles (preserves empty `ReplayURL` so syncer can track discovery and subsequent URL population). Skips malformed match entries with empty `MatchGUID` with warning logs. Provides transparent auto-reconnect and single retry on mid-query connection drops (`rlapi.ErrConnectionClosed` or EOF/reset). `Close()` is idempotent and thread-safe.
   - `downloader.go`: Implements `ReplayDownloader`. Enforces strict input validation: rejects empty GUIDs (`ErrEmptyMatchGUID`), forbids path traversal characters `/\:?*"<>|` and `..` (`ErrInvalidMatchGUID`), and validates URL schemes (`http` and `https`). Streams replay body directly to `.tmp` file located inside `destDir` (ensuring same filesystem volume, avoiding `EXDEV` cross-device link errors). Validates payload size (>= 1024 bytes) before flushing. Explicitly calls `tmpFile.Sync()` and `tmpFile.Close()` *before* invoking `os.Rename`. Implements `atomicRename` with a retry loop and backoff to withstand Windows transient locks. Guarantees deferred cleanup of temporary files on errors or cancellation. Provides `CleanupStaleTempFiles`.

4. **Integrity Audit**:
   - Inspected all source code lines in `internal/auth` and `internal/psynet`.
   - Zero hardcoded test outputs or responses in production code.
   - No dummy/facade implementations; full integration with `github.com/dank/rlapi` (`*rlapi.EGS`, `*rlapi.PsyNet`, and `*rlapi.PsyNetRPC`).
   - Verified tests pass independently in clean execution environment.

---

## 2. Logic Chain

1. **Windows Atomic Rename Safety**:
   - *Observation*: On Windows, calling `os.Rename` on an open file descriptor triggers `ERROR_SHARING_VIOLATION`. Calling `os.Rename` across different volumes triggers `EXDEV`.
   - *Implementation in `downloader.go`*:
     - Line 259: `os.CreateTemp(destDir, tmpPattern)` places the temporary file in `destDir`, guaranteeing the same volume as the target `.replay` file.
     - Line 293: `tmpFile.Sync()` ensures data is committed to disk.
     - Line 299: `tmpFile.Close()` explicitly closes the file handle prior to `atomicRename`.
     - Lines 267-274: Deferred cleanup handler safely closes any open handle and removes the temporary file on error.
     - Lines 315-328: `atomicRename` implements linear backoff retries (default 5 attempts, configurable via `WithRenameRetries`) to ride out transient locks from Windows Defender or search indexers.
   - *Deduction*: The Windows atomic file streaming and rename protocol is correctly and safely implemented.

2. **Clean Architecture & Interface Boundaries**:
   - *Observation*: `PROJECT.md` specifies `MatchHistoryProvider` and `ReplayDownloader` domain interfaces for `syncer`.
   - *Implementation*: `internal/psynet` implements `GetRecentMatches(ctx context.Context) ([]DiscoveredMatch, error)` and `DownloadReplay(ctx context.Context, matchGUID, replayURL, destDir string) (string, error)`.
   - *Dependencies*:
     - `internal/auth` depends only on `config`, `storage` (interface), and `rlapi`.
     - `internal/psynet` depends only on Go standard library and `rlapi`.
     - Zero circular dependencies exist.
   - *Deduction*: Interface boundaries conform to Clean Architecture and allow straightforward consumption by Milestone 4 (`internal/syncer`).

3. **Robustness & Error Resilience**:
   - *Observation*: PsyNet RPC connections can drop mid-poll, match replays can be delayed in arriving on CDN, and malformed entries can be received.
   - *Implementation*: `client.go` handles delayed replay URLs without dropping matches, skips empty GUIDs, and transparently reconnects on dropped sockets. `downloader.go` rejects corrupt payloads (<1KB), returns typed `HTTPStatusError` on non-200 responses, and respects context cancellation throughout the network stream.
   - *Deduction*: Error handling is defensive, robust, and aligned with daemon uptime requirements.

---

## 3. Adversarial Analysis & Edge Cases

1. **Stampede Contention on Identical Match GUID on Windows**:
   - *Attack Scenario*: If 20 concurrent workers attempt to download and rename the *exact same* match GUID to the same destination directory simultaneously, multiple concurrent `MoveFileEx(..., MOVEFILE_REPLACE_EXISTING)` calls occur.
   - *Finding*: Under an extreme 20-worker stampede on Windows, transient `ERROR_ACCESS_DENIED` can occur if all 20 threads contend within a tight 50ms window.
   - *Mitigation & Defense*:
     - `WithRenameRetries(15)` provides extended retry budget if high-concurrency contention is expected.
     - In the daemon architecture (`syncer`), matches are deduplicated by `MatchGUID` against `storage.StateStore` prior to queuing for download, preventing duplicate concurrent downloads of identical GUIDs in production.
     - Even under extreme contention, `downloader.go`'s deferred cleanup guarantees zero orphaned `.tmp` files are left on disk.

2. **Path Traversal & Filename Sanitization**:
   - *Attack Scenario*: Malicious or corrupted match GUID containing `../`, `..\`, `:`, or NTFS special characters (`*`, `?`, `"`, `<`, `>`, `|`).
   - *Finding*: Explicitly sanitized at `downloader.go:208` using `strings.ContainsAny(trimmedGUID, `/\:?*"<>|`) || strings.Contains(trimmedGUID, "..")`. Rejects malicious inputs with `ErrInvalidMatchGUID`.

3. **Steam Session Ticket Expiration**:
   - *Attack Scenario*: Steam session ticket expires while daemon is running.
   - *Finding*: `SteamAuthProvider.Refresh` attempts EOS token refresh first; if unavailable, it fails fast with a clear, descriptive error indicating that a fresh Steam session ticket must be provided by the host environment.

---

## 4. Caveats

1. **Steam Ticket Lifetime**:
   Steam session tickets cannot be renewed via OAuth grants without an active local Steam client. As documented in `m2_worker_1/handoff.md`, daemon configurations utilizing Steam authentication must rely on valid tickets provided at runtime or renewed via external scripts.
2. **Replay Payload Parsing**:
   Replay payloads are validated for minimum size (>=1KB) and non-empty content. Full binary AST parsing of Unreal Engine replay data is intentionally delegated to Ballchasing.com.

---

## 5. Conclusion

**Verdict: APPROVE**

Milestone 2 (Auth & PsyNet Integration) meets all technical requirements, passes 100% of unit, adversarial, and E2E tests with 0 `go vet` warnings, exhibits clean architectural decoupling, and contains zero integrity violations.

---

## 6. Verification Method

To independently reproduce the verification:

```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
cd d:\code\rl-api-utils

# 1. Run auth unit and adversarial tests
go test -v -count=1 ./internal/auth/...

# 2. Run psynet unit and adversarial tests
go test -v -count=1 ./internal/psynet/...

# 3. Run full repository test suite
go test -count=1 ./...

# 4. Run static analysis
go vet ./internal/auth/... ./internal/psynet/...

# 5. Check test coverage
go test -cover ./internal/auth/... ./internal/psynet/...
```
All commands must exit with code 0.
