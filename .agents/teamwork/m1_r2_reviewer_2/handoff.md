# Milestone 1 (Iteration 2) Review & Adversarial Challenge Report

**Reviewer**: `m1_r2_reviewer_2` (Roles: reviewer, critic)  
**Milestone**: M1 - Storage & Configuration (Iteration 2)  
**Date**: 2026-09-25T03:36:30Z  
**Target Subject**: Worker Handoff `d:\code\rl-api-utils\.agents\teamwork\m1_worker_2\handoff.md`  
**Verdict**: **APPROVE**  
**Overall Risk Assessment**: LOW  

---

## 1. Observation

### 1.1 Scope and Artifacts Inspected
The following files were inspected line-by-line:
1. `internal/storage/store.go` (Interface definitions, models, status enums)
2. `internal/storage/sqlite.go` (Pure Go modernc.org/sqlite implementation)
3. `internal/storage/jsonstore.go` (In-memory store with atomic JSON persistence)
4. `internal/storage/sqlite_test.go` & `internal/storage/jsonstore_test.go`
5. `internal/storage/adversarial_test.go` (Adversarial test cases including concurrency, crash recovery, context cancellation, delayed replay URL arrival, empty auth provider)
6. `internal/config/config.go` (Configuration struct, Duration type, YAML/JSON unmarshaling, env overrides, validation)
7. `internal/config/config_test.go` & `internal/config/boundary_test.go` (Boundary tests including YAML raw nanoseconds unmarshaling)

### 1.2 Verification Commands and Direct Outputs
Executed independently in PowerShell on Windows:

1. **Storage Package Test Suite**:
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   go test -v -count=1 ./internal/storage/...
   ```
   *Result*: **PASS** (17 tests, 3.255s).
   - `TestAdversarial_ContextCancellation/SQLiteStore`: PASS (`GetMatch` and `UpsertDiscoveredMatches` correctly rejected canceled context).
   - `TestAdversarial_ContextCancellation/JSONStore`: PASS (`GetMatch` and `UpsertDiscoveredMatches` correctly rejected canceled context).
   - `TestAdversarial_SkippedReplayURLArrival/SQLiteStore`: PASS (`DownloadStatus=PENDING`, `ListPendingDownloads count: 1`).
   - `TestAdversarial_SkippedReplayURLArrival/JSONStore`: PASS (`DownloadStatus=PENDING`, `ListPendingDownloads count: 1`).
   - `TestAdversarial_AuthState_EmptyProvider/SQLiteStore`: PASS (`provider cannot be empty`).
   - `TestAdversarial_AuthState_EmptyProvider/JSONStore`: PASS (`provider cannot be empty`).
   - `TestAdversarial_ConcurrencyContention_SQLite`: PASS.
   - `TestAdversarial_ConcurrencyContention_JSON`: PASS.
   - `TestAdversarial_CrashRecovery_RecoverInFlight`: PASS.
   - `TestAdversarial_Idempotency_TerminalStatesNeverClobbered`: PASS.

2. **Config Package Test Suite**:
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   go test -v -count=1 ./internal/config/...
   ```
   *Result*: **PASS** (21 tests, 0.546s).
   - `TestBug_YAMLRawNanosecondsDecoding`: PASS (parsed `300000000000` into `5m0s`).
   - `TestBoundary_ZeroAndNegativeDurationsRejectedInValidate`: PASS.
   - `TestBoundary_CaseInsensitivity_ConfigLoading`: PASS.
   - `TestBoundary_EnvVarBooleans`: PASS.
   - `TestConfig_PrecedenceHierarchy`: PASS.

3. **Static Analysis (Vet)**:
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   go vet ./internal/storage/... ./internal/config/...
   ```
   *Result*: **PASS** (zero warnings or lint errors).

4. **Full Repository & E2E Verification**:
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   go test -count=1 ./...
   ```
   *Result*: **PASS** across all packages (`internal/config`, `internal/storage`, `internal/testutil`, `test/e2e`). All 4 tiers of E2E test scenarios passed without regression.

---

## 2. Logic Chain

1. **Integrity Verification**:
   - Source code inspection revealed **zero integrity violations**:
     - No hardcoded test GUIDs, timestamps, or expected outputs in production logic.
     - Both `SQLiteStore` and `JSONStore` provide genuine, complete implementations of all 14 `StateStore` interface methods.
     - Pure Go `modernc.org/sqlite` is used without CGO shortcuts.
     - `JSONStore` employs real atomic staging via temporary files (`os.CreateTemp`), `Sync()`, and `os.Rename` with retry mechanisms for Windows file lock tolerance.
     - Verification was independently executed and reproduced.

2. **Remediation 1: Delayed Replay URL Arrival (SQLiteStore & JSONStore)**:
   - *Problem*: In Rocket League PsyNet match discovery, a match may initially appear before the CDN replay URL is provisioned. Initial discovery records `ReplayURL = ""` and `DownloadStatus = SKIPPED`. In subsequent cycles, when `ReplayURL` becomes available, the record must transition to `PENDING` without overwriting other statuses or metadata.
   - *SQLite Implementation* (`sqlite.go:208-211`):
     ```sql
     ON CONFLICT(match_guid) DO UPDATE SET
         download_status = CASE WHEN matches.download_status = 'SKIPPED' AND matches.replay_url = '' AND excluded.replay_url != '' THEN 'PENDING' ELSE matches.download_status END,
         replay_url = CASE WHEN matches.replay_url = '' AND excluded.replay_url != '' THEN excluded.replay_url ELSE matches.replay_url END,
         updated_at = excluded.updated_at;
     ```
   - *JSONStore Implementation* (`jsonstore.go:329-337`):
     ```go
     if existing.ReplayURL == "" && m.ReplayURL != "" {
         existing.ReplayURL = m.ReplayURL
         if existing.DownloadStatus == DownloadSkipped {
             existing.DownloadStatus = DownloadPending
         }
         existing.UpdatedAt = now
         modified = true
     }
     ```
   - *Assessment*: Both engines implement strictly equivalent, robust semantics. Terminal states (`DOWNLOADED`, `UPLOADED`, `DUPLICATE`) and in-flight states (`DOWNLOADING`, `UPLOADING`) are protected from being overwritten, while delayed replay URLs cleanly trigger the transition from `SKIPPED` to `PENDING`.

3. **Remediation 2: Context Cancellation Propagation in JSONStore**:
   - *Problem*: Previously, `JSONStore` ignored `ctx.Err()` in public methods, continuing execution even when caller requests were timed out or canceled.
   - *Implementation* (`jsonstore.go`): Every one of the 14 `StateStore` interface methods now checks `if err := ctx.Err(); err != nil { return ..., err }`. Mutation methods check before acquiring the lock and immediately after acquiring the lock.
   - *Assessment*: Prevents lock starvation, unnecessary in-memory mutations, and unneeded disk I/O when operations are abandoned or timed out.

4. **Remediation 3: Empty Auth Provider Guard (SQLiteStore)**:
   - *Problem*: Behavioral divergence where `JSONStore` rejected empty provider names with `"provider cannot be empty"`, while `SQLiteStore` permitted insertion of empty provider strings.
   - *Implementation* (`sqlite.go:405-407, 428-430`): Added explicit guards returning `errors.New("provider cannot be empty")` in `SaveAuthState` and `GetAuthState`.
   - *Assessment*: Complete behavioral parity across both storage implementations.

5. **Remediation 4: YAML Raw Nanoseconds Parsing (Config Duration)**:
   - *Problem*: In `Duration.UnmarshalYAML`, attempting `value.Decode(&s)` first resulted in YAML numeric scalar nodes (e.g. `300000000000`) being decoded as string `"300000000000"`, which failed `time.ParseDuration` with `"missing unit in duration"`.
   - *Implementation* (`config.go:54-70`): Inverted unmarshaling sequence to attempt `value.Decode(&n)` (`int64`) first, falling back to string duration parsing (`time.ParseDuration`).
   - *Assessment*: Directly resolves the bug, enabling support for both human-readable duration strings (`"5m"`, `"30s"`) and raw numeric nanoseconds across both YAML and JSON formats.

---

## 3. Adversarial Review & Stress-Testing

### Challenge 1: Terminal State Clobbering Resistance
- **Attack Scenario**: PsyNet returns a previously processed match with altered fields or empty replay URL on subsequent polling passes.
- **Result**: PASS. Both SQLite `ON CONFLICT` and JSONStore map checks verify that non-empty `ReplayURL` only updates when the existing `ReplayURL` was empty, and only transitions status if current status is `SKIPPED`. Records marked `DOWNLOADED`, `UPLOADED`, `DUPLICATE`, or `FAILED` are untouched.

### Challenge 2: Concurrent Multi-Goroutine Contention
- **Attack Scenario**: High contention with simultaneous `UpsertDiscoveredMatches`, `ListPendingDownloads`, `MarkDownloading`, `MarkDownloaded`, and `RecoverInFlight` calls across 50 concurrent goroutines.
- **Result**: PASS.
  - SQLite: Configured with `db.SetMaxOpenConns(1)` and `PRAGMA busy_timeout = 5000;`, serializing access without `database is locked` panics or deadlocks.
  - JSONStore: Protected by `sync.RWMutex` with defensive pointer cloning (`cloneMatchRecord`), preventing memory race conditions.

### Challenge 3: Windows File-Lock Race on Atomic Renames
- **Attack Scenario**: Background processes (Windows Defender, search indexer) momentarily lock the state store JSON file during atomic replacement.
- **Result**: PASS. `JSONStore.saveLocked()` utilizes `atomicRename` with exponential sleep retries (up to 5 attempts), preventing transient file lock errors on Windows.

### Challenge 4: Context Cancellation Boundary
- **Attack Scenario**: Client initiates an upsert or status transition with an already canceled or expired context.
- **Result**: PASS. Both engines immediately return `context.Canceled` or `context.DeadlineExceeded` without mutating state or performing disk I/O.

---

## 4. Caveats

1. **Advisory Finding in E2E Test Suite (Outside M1)**:
   Running `go vet ./...` across the entire repository flags:
   `test\e2e\tier1_feature_test.go:462:8: using resp before checking for errors`
   - *Impact*: Low/Advisory. The issue is in `test/e2e` (E2E Track), not within M1 (`internal/storage` or `internal/config`). `go vet` passes cleanly on all M1 packages (`internal/storage/...` and `internal/config/...`).
   - *Recommendation*: The E2E track / orchestrator can patch line 461 in `tier1_feature_test.go` to verify `err == nil` before invoking `defer resp.Body.Close()`.

---

## 5. Conclusion

**Verdict**: **APPROVE**

All Milestone 1 Iteration 2 requirements and remediations have been successfully verified:
- Correctness, completeness, and interface conformance of `internal/storage` and `internal/config` are verified.
- Edge cases (delayed replay URL arrival, context cancellation, empty provider validation, YAML numeric duration unmarshaling) are properly implemented and thoroughly tested.
- Zero integrity violations detected.
- 100% of tests across `internal/storage`, `internal/config`, and `test/e2e` pass cleanly.
- M1 is ready for downstream milestone integration (M2 Auth/PsyNet and M3 Ballchasing).

---

## 6. Verification Method

To independently reproduce this review's findings, run the following commands in PowerShell from the project root `d:\code\rl-api-utils`:

```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
cd d:\code\rl-api-utils

# 1. Verify storage package tests
go test -v -count=1 ./internal/storage/...

# 2. Verify config package tests
go test -v -count=1 ./internal/config/...

# 3. Verify static analysis on M1 packages
go vet ./internal/storage/... ./internal/config/...

# 4. Verify all repository tests
go test -count=1 ./...
```

**Invalidation Conditions**:
- Any test failure in `./internal/storage/...` or `./internal/config/...`.
- Any `go vet` error in `./internal/storage/...` or `./internal/config/...`.
- Any regression in E2E tests (`./test/e2e/...`).
