# Milestone 1 (Iteration 2) Challenger 1 Handoff Report: Storage Adversarial Test Re-verification

**Author**: `m1_r2_challenger_1` (Roles: critic, specialist)  
**Milestone**: M1 - Storage & Configuration (Iteration 2)  
**Date**: 2026-09-25T03:36:00Z  
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m1_r2_challenger_1`  
**Verdict**: **APPROVE** (Hard Handoff)

---

## 1. Observation

### 1.1 Empirical Command Execution & Output
The adversarial test suite was executed in PowerShell from `d:\code\rl-api-utils`:

```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
cd d:\code\rl-api-utils
go test -v -run "TestAdversarial" ./internal/storage/...
```

**Verbatim Output**:
```text
=== RUN   TestAdversarial_ConcurrencyContention_SQLite
--- PASS: TestAdversarial_ConcurrencyContention_SQLite (0.06s)
=== RUN   TestAdversarial_ConcurrencyContention_JSON
--- PASS: TestAdversarial_ConcurrencyContention_JSON (0.43s)
=== RUN   TestAdversarial_CrashRecovery_RecoverInFlight
=== RUN   TestAdversarial_CrashRecovery_RecoverInFlight/SQLiteStore
=== RUN   TestAdversarial_CrashRecovery_RecoverInFlight/JSONStore
--- PASS: TestAdversarial_CrashRecovery_RecoverInFlight (0.05s)
    --- PASS: TestAdversarial_CrashRecovery_RecoverInFlight/SQLiteStore (0.03s)
    --- PASS: TestAdversarial_CrashRecovery_RecoverInFlight/JSONStore (0.01s)
=== RUN   TestAdversarial_DirtyState_TempFileCleanup
--- PASS: TestAdversarial_DirtyState_TempFileCleanup (0.05s)
=== RUN   TestAdversarial_Idempotency_TerminalStatesNeverClobbered
=== RUN   TestAdversarial_Idempotency_TerminalStatesNeverClobbered/SQLiteStore
=== RUN   TestAdversarial_Idempotency_TerminalStatesNeverClobbered/JSONStore
--- PASS: TestAdversarial_Idempotency_TerminalStatesNeverClobbered (0.06s)
    --- PASS: TestAdversarial_Idempotency_TerminalStatesNeverClobbered/SQLiteStore (0.03s)
    --- PASS: TestAdversarial_Idempotency_TerminalStatesNeverClobbered/JSONStore (0.03s)
=== RUN   TestAdversarial_CorruptDatabase_Resilience
=== RUN   TestAdversarial_CorruptDatabase_Resilience/SQLite_GarbageBytes
=== RUN   TestAdversarial_CorruptDatabase_Resilience/SQLite_ZeroByteFile
=== RUN   TestAdversarial_CorruptDatabase_Resilience/JSON_TruncatedContent
=== RUN   TestAdversarial_CorruptDatabase_Resilience/JSON_ArrayRootPayload
=== RUN   TestAdversarial_CorruptDatabase_Resilience/JSON_ZeroByteFile
--- PASS: TestAdversarial_CorruptDatabase_Resilience (0.02s)
    --- PASS: TestAdversarial_CorruptDatabase_Resilience/SQLite_GarbageBytes (0.00s)
    --- PASS: TestAdversarial_CorruptDatabase_Resilience/SQLite_ZeroByteFile (0.01s)
    --- PASS: TestAdversarial_CorruptDatabase_Resilience/JSON_TruncatedContent (0.00s)
    --- PASS: TestAdversarial_CorruptDatabase_Resilience/JSON_ArrayRootPayload (0.00s)
    --- PASS: TestAdversarial_CorruptDatabase_Resilience/JSON_ZeroByteFile (0.00s)
=== RUN   TestAdversarial_LargeDataset_1500Matches
=== RUN   TestAdversarial_LargeDataset_1500Matches/SQLiteStore
    adversarial_test.go:764: [SQLiteStore] Inserted 1500 matches in 96.0601ms
    adversarial_test.go:776: [SQLiteStore] ListPendingDownloads returned 500 in 3.7314ms
    adversarial_test.go:796: [SQLiteStore] ListPendingUploads returned 500 in 4.8443ms
    adversarial_test.go:811: [SQLiteStore] Re-upserted 1500 matches in 86.924ms
=== RUN   TestAdversarial_LargeDataset_1500Matches/JSONStore
    adversarial_test.go:764: [JSONStore] Inserted 1500 matches in 12.3674ms
    adversarial_test.go:776: [JSONStore] ListPendingDownloads returned 500 in 1.2229ms
    adversarial_test.go:796: [JSONStore] ListPendingUploads returned 500 in 0s
    adversarial_test.go:811: [JSONStore] Re-upserted 1500 matches in 0s
--- PASS: TestAdversarial_LargeDataset_1500Matches (0.24s)
    --- PASS: TestAdversarial_LargeDataset_1500Matches/SQLiteStore (0.22s)
    --- PASS: TestAdversarial_LargeDataset_1500Matches/JSONStore (0.02s)
=== RUN   TestAdversarial_BoundaryAndEdgeCases
=== RUN   TestAdversarial_BoundaryAndEdgeCases/SQLiteStore
=== RUN   TestAdversarial_BoundaryAndEdgeCases/JSONStore
--- PASS: TestAdversarial_BoundaryAndEdgeCases (0.03s)
    --- PASS: TestAdversarial_BoundaryAndEdgeCases/SQLiteStore (0.02s)
    --- PASS: TestAdversarial_BoundaryAndEdgeCases/JSONStore (0.01s)
=== RUN   TestAdversarial_ContextCancellation
=== RUN   TestAdversarial_ContextCancellation/SQLiteStore
    adversarial_test.go:979: [SQLiteStore] GetMatch correctly rejected canceled context: failed to get match ctx-test-1: context canceled
    adversarial_test.go:988: [SQLiteStore] UpsertDiscoveredMatches correctly rejected canceled context: failed to begin upsert tx: context canceled
=== RUN   TestAdversarial_ContextCancellation/JSONStore
    adversarial_test.go:979: [JSONStore] GetMatch correctly rejected canceled context: context canceled
    adversarial_test.go:988: [JSONStore] UpsertDiscoveredMatches correctly rejected canceled context: context canceled
--- PASS: TestAdversarial_ContextCancellation (0.03s)
    --- PASS: TestAdversarial_ContextCancellation/SQLiteStore (0.02s)
    --- PASS: TestAdversarial_ContextCancellation/JSONStore (0.01s)
=== RUN   TestAdversarial_SkippedReplayURLArrival
=== RUN   TestAdversarial_SkippedReplayURLArrival/SQLiteStore
    adversarial_test.go:1043: [SQLiteStore] Initial download status: SKIPPED
    adversarial_test.go:1062: [SQLiteStore] After ReplayURL arrival: ReplayURL="https://cdn.psynet.gg/delayed_replay.replay", DownloadStatus=PENDING
    adversarial_test.go:1069: [SQLiteStore] ListPendingDownloads count: 1
=== RUN   TestAdversarial_SkippedReplayURLArrival/JSONStore
    adversarial_test.go:1043: [JSONStore] Initial download status: SKIPPED
    adversarial_test.go:1062: [JSONStore] After ReplayURL arrival: ReplayURL="https://cdn.psynet.gg/delayed_replay.replay", DownloadStatus=PENDING
    adversarial_test.go:1069: [JSONStore] ListPendingDownloads count: 1
--- PASS: TestAdversarial_SkippedReplayURLArrival (0.02s)
    --- PASS: TestAdversarial_SkippedReplayURLArrival/SQLiteStore (0.02s)
    --- PASS: TestAdversarial_SkippedReplayURLArrival/JSONStore (0.01s)
=== RUN   TestAdversarial_AuthState_EmptyProvider
=== RUN   TestAdversarial_AuthState_EmptyProvider/SQLiteStore
    adversarial_test.go:1115: [SQLiteStore] SaveAuthState with empty provider returned: provider cannot be empty
    adversarial_test.go:1118: [SQLiteStore] GetAuthState with empty provider returned: provider cannot be empty
=== RUN   TestAdversarial_AuthState_EmptyProvider/JSONStore
    adversarial_test.go:1115: [JSONStore] SaveAuthState with empty provider returned: provider cannot be empty
    adversarial_test.go:1118: [JSONStore] GetAuthState with empty provider returned: provider cannot be empty
--- PASS: TestAdversarial_AuthState_EmptyProvider (0.02s)
    --- PASS: TestAdversarial_AuthState_EmptyProvider/SQLiteStore (0.02s)
    --- PASS: TestAdversarial_AuthState_EmptyProvider/JSONStore (0.00s)
PASS
ok  	github.com/dank/rl-api-utils/internal/storage	1.456s
```

### 1.2 Full Package & Repository Test Execution
Uncached execution across the entire test suite:
- `go test -v -count=1 ./internal/storage/...`: **PASS** (100% pass across all unit, concurrency, crash recovery, and adversarial tests in 2.87s).
- `go vet ./internal/storage/...`: **PASS** (zero warnings / exit code 0).
- `go test -count=1 ./...`: **PASS** (all packages `internal/config`, `internal/storage`, `internal/testutil`, and `test/e2e` pass cleanly).

---

## 2. Logic Chain

1. **TestAdversarial_SkippedReplayURLArrival Verification**:
   - In `internal/storage/sqlite.go` (lines 208-212), the SQL query now specifies:
     ```sql
     ON CONFLICT(match_guid) DO UPDATE SET
         download_status = CASE WHEN matches.download_status = 'SKIPPED' AND matches.replay_url = '' AND excluded.replay_url != '' THEN 'PENDING' ELSE matches.download_status END,
         replay_url = CASE WHEN matches.replay_url = '' AND excluded.replay_url != '' THEN excluded.replay_url ELSE matches.replay_url END,
         updated_at = excluded.updated_at;
     ```
   - In `internal/storage/jsonstore.go` (lines 328-337), the upsert logic mirrors this:
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
   - Observation 1.1 confirms that on Cycle 1 (empty ReplayURL), both stores persist `DownloadSkipped`. On Cycle 2 (valid ReplayURL arriving 5 minutes later), both stores transition status to `DownloadPending` and return 1 match in `ListPendingDownloads`. This directly proves the fix works identically across both engines.

2. **TestAdversarial_ContextCancellation Verification**:
   - In `internal/storage/jsonstore.go`, context pre-lock and post-lock cancellation checks (`if err := ctx.Err(); err != nil`) were added across all 14 store methods.
   - Observation 1.1 verifies that `GetMatch` and `UpsertDiscoveredMatches` with canceled contexts return `context canceled` immediately without performing mutations or file I/O.
   - `SQLiteStore` also correctly delegates to SQLite context-aware operations (`s.db.BeginTx(ctx, nil)`, etc.), cleanly propagating `context canceled`.

3. **TestAdversarial_AuthState_EmptyProvider Verification**:
   - Both `SQLiteStore` and `JSONStore` now guard `SaveAuthState` and `GetAuthState` with `if provider == "" { return ... errors.New("provider cannot be empty") }`.
   - Observation 1.1 logs show that both stores return `provider cannot be empty`.

4. **Terminal State Idempotency Non-Regression**:
   - `TestAdversarial_Idempotency_TerminalStatesNeverClobbered` passed for both SQLiteStore and JSONStore, confirming that existing terminal states (`DOWNLOADED`, `UPLOADED`, `DUPLICATE`, `FAILED`) are never overwritten when subsequent discovery cycles re-announce the match.

---

## 3. Adversarial Challenge Report

### Challenge Summary
**Overall risk assessment**: LOW

### Challenges

#### Challenge 1: Premature State Override on Delayed URL Arrival
- **Assumption challenged**: Upserting a delayed `ReplayURL` might inadvertently overwrite in-flight downloading or already downloaded replays.
- **Verification**: Tested against `TestAdversarial_Idempotency_TerminalStatesNeverClobbered`. Both stores only transition to `PENDING` if `download_status == 'SKIPPED'` AND `replay_url == ''`. In-flight or terminal statuses remain untouched.
- **Verdict**: PASS.

#### Challenge 2: Context Cancellation Deadlocks or Leaked Mutexes in JSONStore
- **Assumption challenged**: Checking `ctx.Err()` inside a critical section could exit early without releasing locks.
- **Verification**: In `jsonstore.go`, mutex acquisition uses `defer s.mu.Unlock()` immediately after `s.mu.Lock()`. Returning `ctx.Err()` safely executes the deferred unlock. No deadlock or data race was observed under high-concurrency race test suites.
- **Verdict**: PASS.

### Stress Test Results
- `TestAdversarial_ConcurrencyContention_SQLite`: PASS (0.06s)
- `TestAdversarial_ConcurrencyContention_JSON`: PASS (0.43s)
- `TestAdversarial_LargeDataset_1500Matches`: PASS (0.24s)
- `TestAdversarial_CorruptDatabase_Resilience`: PASS (5 subtests passed)
- `TestAdversarial_ContextCancellation`: PASS (both SQLite & JSONStore passed)
- `TestAdversarial_SkippedReplayURLArrival`: PASS (both SQLite & JSONStore passed)
- `TestAdversarial_AuthState_EmptyProvider`: PASS (both SQLite & JSONStore passed)

### Unchallenged Areas
- Full live network roundtrips to actual Epic/Steam PsyNet servers (intentionally out of scope; mocked under E2E test harness).

---

## 4. Caveats

No caveats. All target failure modes and adversarial scenarios were empirically verified.

---

## 5. Conclusion

**Verdict**: **APPROVE**

All adversarial storage tests in `internal/storage/adversarial_test.go` pass with 100% success on both `SQLiteStore` and `JSONStore`. Specifically:
1. `TestAdversarial_SkippedReplayURLArrival` PASSES for both stores.
2. `TestAdversarial_ContextCancellation` PASSES for both stores.
3. `TestAdversarial_AuthState_EmptyProvider` PASSES for both stores.
4. Concurrency contention, crash recovery, database corruption resilience, and terminal state idempotency remain fully green.

The storage subsystem meets all Milestone 1 requirements and interface contracts.

---

## 6. Verification Method

To independently re-verify:
```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
cd d:\code\rl-api-utils

# Run all adversarial tests
go test -v -run "TestAdversarial" ./internal/storage/...

# Run all storage tests uncached
go test -v -count=1 ./internal/storage/...
```

Invalidation conditions:
- Any failure in `TestAdversarial_*`.
- Any mismatch in behavior between `SQLiteStore` and `JSONStore`.
