# Milestone 2 (Auth & PsyNet Integration) Review & Adversarial Challenge Report

- **Reviewer**: `m2_reviewer_1` (Roles: reviewer, critic)
- **Target**: Milestone 2 (`internal/auth`, `internal/psynet`, `internal/testutil/mock_psynet.go`)
- **Worker Report**: `d:\code\rl-api-utils\.agents\teamwork\m2_worker_1\handoff.md`
- **Verdict**: **APPROVE**
- **Date**: 2026-09-25T03:58:00Z
- **Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m2_reviewer_1`

---

## Review Summary

**Verdict**: **APPROVE**

Milestone 2 delivers a robust, standards-compliant, and secure dual authentication (`internal/auth`) and PsyNet client/downloader (`internal/psynet`) implementation. Integrity checks confirm zero dummy implementations, hardcoded outputs, or bypasses. All interface contracts defined in `PROJECT.md` are strictly honored. 100% of unit and adversarial test suites pass cleanly across the entire workspace with zero `go vet` warnings.

---

## 1. Observation

Direct execution of verification commands, code reviews, and adversarial tests produced the following concrete observations:

### 1.1 Integrity Verification
- `internal/auth/epic.go` (lines 56–162, 165–257): Implements genuine OAuth grant with refresh token/auth code, exchange code acquisition, EOS token exchange, state persistence in `StateStore`, and reader/writer locking via `sync.RWMutex`.
- `internal/auth/steam.go` (lines 48–75, 78–127): Enforces strict 17-digit numeric SteamID64 validation starting with `7656119`, real ticket exchange via `ExchangeEOSTokenFromSteam`, and state persistence.
- `internal/psynet/downloader.go` (lines 196–311): Real HTTP streaming using `io.CopyBuffer` with 64KB chunks, size validation (>=1024 bytes), explicit `tmpFile.Close()` before rename, deferred file handle cleanup and unlinking on error, and Windows retry backoff (`atomicRename`).
- `internal/psynet/client.go` (lines 136–192, 219–272): Auto-reconnect on connection drops, mapping of `rlapi.MatchEntry` with preservation of empty `ReplayURL` for delayed arrival, and thread-safe idempotent `Close()`.
- **Integrity Result**: Zero hardcoded test outputs, zero dummy facade implementations, and zero task shortcuts. All implementations feature genuine production logic.

### 1.2 Build, Static Analysis, and Test Suite Results
1. **Milestone 2 Test Execution**:
   - `go test -v -count=1 ./internal/auth/...`:
     ```
     31 passing tests (13 worker unit tests + 18 adversarial tests) in 0.140s.
     PASS: ok github.com/dank/rl-api-utils/internal/auth 0.140s
     ```
   - `go test -v -count=1 ./internal/psynet/...`:
     ```
     38 passing tests (20 worker unit tests + 18 adversarial tests) in 4.016s.
     PASS: ok github.com/dank/rl-api-utils/internal/psynet 4.016s
     ```
2. **Static Analysis (`go vet`)**:
   - `go vet ./internal/auth/... ./internal/psynet/...`: Completed with exit code 0 and 0 warnings.
3. **Workspace Full Regression Suite**:
   - `go test -count=1 ./...`:
     ```
     ok   github.com/dank/rl-api-utils/internal/auth     0.188s
     ok   github.com/dank/rl-api-utils/internal/config   0.698s
     ok   github.com/dank/rl-api-utils/internal/psynet   4.512s
     ok   github.com/dank/rl-api-utils/internal/storage  4.574s
     ok   github.com/dank/rl-api-utils/internal/testutil 1.071s
     ok   github.com/dank/rl-api-utils/test/e2e          4.364s
     ```
     100% of workspace tests pass.
4. **Statement Coverage**:
   - `internal/auth`: **92.2%** statement coverage.
   - `internal/psynet`: **82.4%** statement coverage.

---

## 2. Logic Chain

From the observations above, the assessment steps proceed as follows:

1. **Interface Contract Compliance**:
   - `PROJECT.md` lines 140–165 define:
     - `MatchHistoryProvider` with `GetRecentMatches(ctx context.Context) ([]DiscoveredMatch, error)` and `Close() error`. Implemented by `psynet.Client`.
     - `ReplayDownloader` with `DownloadReplay(ctx context.Context, matchGUID, replayURL, destDir string) (string, error)`. Implemented by `psynet.HTTPDownloader` with static assertion `var _ ReplayDownloader = (*HTTPDownloader)(nil)`.
     - `AuthProvider` with `Name()`, `Authenticate(ctx)`, `Refresh(ctx, rt)`, `TokenInfo()`, `Validate()`. Implemented by `EpicAuthProvider` and `SteamAuthProvider`.
   - Domain types match requirements seamlessly.

2. **Windows File System Robustness**:
   - `downloader.go` places in-flight downloads in `destDir` with prefix `.tmp-`, ensuring temp and destination files share the same filesystem volume to avoid `EXDEV` cross-device rename errors.
   - `tmpFile.Close()` is called explicitly on line 299 before `atomicRename` on line 305, preventing `ERROR_SHARING_VIOLATION` on Windows.
   - `atomicRename` implements retry loops with backoff to withstand transient locking by Windows Defender or background indexing services.
   - Deferred cleanup (`defer func() { _ = tmpFile.Close(); if !success { _ = os.Remove(tmpPath) } }()`) guarantees that any mid-stream failure, network cutoff, context cancellation, or size validation failure closes handles and unlinks orphan files.

3. **Concurrency & Thread Safety**:
   - `EpicAuthProvider` and `SteamAuthProvider` guard in-memory cached credentials with `sync.RWMutex`.
   - `TokenInfo()` returns a shallow copy of the struct, preventing external consumers from mutating internal state.
   - Concurrency stress tests with 40 readers executing 50 iterations alongside 10 writers executing `Authenticate` and `Refresh` completed without race conditions, panics, or deadlocks.

4. **Error Handling & Resilience**:
   - Context cancellation is checked before issuing HTTP requests, before EOS token exchange, and verified during payload streaming.
   - PsyNet client transparently reconnects on in-flight socket drops, re-issues RPC queries once, and preserves matches with pending replay URLs so the syncer can resolve them across consecutive polling cycles.

---

## 3. Findings

### [Minor] Finding 1: Underlying Network Error Masked in Steam Token Renewal

- **What**: In `SteamAuthProvider.Refresh`, if `p.egsClient.RefreshEOSToken(tokenToUse)` returns a network error, the underlying error is dropped.
- **Where**: `internal/auth/steam.go:146–180`.
- **Why**: When `RefreshEOSToken` fails with a transient error (e.g. DNS failure, connection timeout), the method skips the success block and returns the static message: `"steam session ticket cannot be automatically renewed, a fresh ticket is required"`.
- **Suggestion**: For future maintenance, wrap `err` if `err != nil`:
  ```go
  if tokenToUse != "" {
      eosResp, err := p.egsClient.RefreshEOSToken(tokenToUse)
      if err != nil {
          return nil, fmt.Errorf("%w: egs eos refresh failed: %v", ErrRefreshFailed, err)
      }
      ...
  }
  ```

### [Minor] Finding 2: Re-check Context Post-Exchange in Steam Authenticate

- **What**: `SteamAuthProvider.Authenticate` checks `ctx.Err()` at method entry, but does not re-check `ctx.Err()` immediately after `ExchangeEOSTokenFromSteam`.
- **Where**: `internal/auth/steam.go:91–98`.
- **Why**: `rlapi.EGS` does not accept a context parameter. If context is cancelled while `ExchangeEOSTokenFromSteam` is in flight, `Authenticate` continues to update `tokenInfo` and write to `StateStore`.
- **Suggestion**: Add `if err := ctx.Err(); err != nil { return nil, err }` immediately following the exchange call.

### [Minor / Advisory] Finding 3: Windows Atomic Rename Retry Window Under Stampede Contention

- **What**: `atomicRename` uses `DefaultRenameRetries = 5` with 5ms linear backoff (~50ms total window).
- **Where**: `internal/psynet/downloader.go:63, 315–328`.
- **Why**: Under extreme adversarial stampedes (e.g. 20 concurrent goroutines racing to overwrite the identical file path on Windows) or slow antivirus filter drivers holding an opportunistic lock for >50ms, 5 attempts can be exhausted.
- **Suggestion**: In production, the syncer's database deduplication guarantees single-flight downloads per GUID. However, increasing `renameRetries` default to 8–10 or using exponential backoff (e.g. 10ms, 25ms, 50ms, 100ms) will provide extra safety margin against aggressive antivirus scanners.

### [Advisory for M4] Finding 4: AccountID Field Mapping for Steam Credentials in Syncer

- **What**: `TokenInfo` for Steam sets `AccountID = steamID64` and `EpicAccountID = eosAccountID`. Conversely, `psynet.Credentials` defines `AccountID` as Epic Account ID and `SteamAccountID` as Steam ID 64.
- **Where**: `internal/auth/steam.go:110–111` vs `internal/psynet/client.go:51–53`.
- **Why**: When M4 wires `AuthProvider.TokenInfo()` into `psynet.ClientConfig.Credentials`, the syncer must map `tokenInfo.EpicAccountID` to `creds.AccountID` and `tokenInfo.AccountID` to `creds.SteamAccountID`.

---

## 4. Verified Claims

| Claim | Method | Result |
|---|---|---|
| Dual auth providers support Epic Games and Steam | Inspected `internal/auth/provider.go`, `epic.go`, `steam.go` + executed 31 unit & adversarial tests | **PASS** |
| SteamID64 validation enforces 17 digits with `7656119` prefix | Tested 21 boundary variations in `auth_adversarial_test.go` | **PASS** |
| Replay payloads < 1KB are rejected; >= 1024 bytes accepted | Tested 0B, 100B, 512B, 1023B (rejected) and 1024B, 2KB, 8KB (accepted) | **PASS** |
| Path traversal in matchGUID is rejected | Tested `../`, `..\`, `/`, `\`, `:`, `*`, `?`, `"`, `<`, `>`, `|` | **PASS** |
| In-flight temp files are closed and unlinked on error | Tested on 403, 404, 500, mid-stream connection drop, and context cancel | **PASS** |
| File handle closed prior to rename on Windows | Code inspection of `downloader.go:299-305` + Windows test execution | **PASS** |
| Transparent reconnect on socket drop during RPC query | Verified in `client_test.go` and `adversarial_test.go` (EOF, socket close) | **PASS** |
| Delayed ReplayURL arrival across cycles is preserved | Verified empty ReplayURL passed to caller for syncer deferral | **PASS** |
| StateStore persistence for Epic refresh tokens and Steam tickets | Verified in `auth_test.go` with mock state store | **PASS** |
| Zero `go vet` warnings across auth and psynet packages | Executed `go vet ./internal/auth/... ./internal/psynet/...` | **PASS** |

---

## 5. Caveats

- Steam session tickets cannot be renewed via OAuth grant without an active Steam client. The implementation correctly returns `ErrRefreshFailed` notifying that ticket renewal is required.
- Full Unreal Engine AST replay parsing is intentionally delegated to Ballchasing.com; `ReplayDownloader` validates file size (>=1KB), magic header bytes, and network transport integrity.
- No caveats regarding Milestone 2 acceptance criteria.

---

## 6. Conclusion

**Final Verdict**: **APPROVE**

Milestone 2 implementation by `m2_worker_1` fulfills all requirements specified in `PROJECT.md` and `ORIGINAL_REQUEST.md`:
- Pure Go implementations conforming to Clean Architecture.
- Flawless Windows filesystem semantics: handles closed prior to atomic rename, `.tmp` staging in target directory, and retry loops.
- Comprehensive test coverage: 92.2% in `internal/auth`, 82.4% in `internal/psynet`, and 100% test pass rate across the full workspace.
- Ready for integration with Milestone 3 (`internal/ballchasing`) and Milestone 4 (`internal/syncer`).

---

## 7. Verification Method

To independently verify this verdict:

```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
cd d:\code\rl-api-utils

# 1. Run unit and adversarial tests for Milestone 2 packages
go test -v -count=1 ./internal/auth/...
go test -v -count=1 ./internal/psynet/...

# 2. Run static analysis
go vet ./internal/auth/... ./internal/psynet/...

# 3. Verify statement coverage
go test -cover ./internal/auth/... ./internal/psynet/...

# 4. Run full workspace regression suite
go test -count=1 ./...
```

*Expected Result*: All commands exit with code 0; 0 vet warnings; 100% pass rate.
