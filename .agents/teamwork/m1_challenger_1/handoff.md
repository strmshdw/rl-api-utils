# Milestone 1 (Storage & Configuration) Challenger Handoff Report

**Author**: `m1_challenger_1` (Roles: critic, specialist)  
**Milestone**: M1 - Storage & Configuration  
**Target**: `internal/storage` (`sqlite.go`, `jsonstore.go`, `store.go`)  
**Date**: 2026-09-25T03:24:00Z  
**Verdict**: **CHALLENGE_FAILED** (Actionable Bug Findings Discovered)  
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m1_challenger_1`

---

## 1. Observation

### 1.1 Verbatim Tool Commands and Test Failures
Stress harness was authored in `internal/storage/adversarial_test.go` and executed against the implementation:

```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
cd d:\code\rl-api-utils
go test -v -run "TestAdversarial" ./internal/storage/...
```

**Verbatim Output**:
```
=== RUN   TestAdversarial_ConcurrencyContention_SQLite
--- PASS: TestAdversarial_ConcurrencyContention_SQLite (0.08s)
=== RUN   TestAdversarial_ConcurrencyContention_JSON
--- PASS: TestAdversarial_ConcurrencyContention_JSON (0.45s)
=== RUN   TestAdversarial_CrashRecovery_RecoverInFlight
=== RUN   TestAdversarial_CrashRecovery_RecoverInFlight/SQLiteStore
=== RUN   TestAdversarial_CrashRecovery_RecoverInFlight/JSONStore
--- PASS: TestAdversarial_CrashRecovery_RecoverInFlight (0.03s)
    --- PASS: TestAdversarial_CrashRecovery_RecoverInFlight/SQLiteStore (0.03s)
    --- PASS: TestAdversarial_CrashRecovery_RecoverInFlight/JSONStore (0.01s)
=== RUN   TestAdversarial_DirtyState_TempFileCleanup
--- PASS: TestAdversarial_DirtyState_TempFileCleanup (0.04s)
=== RUN   TestAdversarial_Idempotency_TerminalStatesNeverClobbered
=== RUN   TestAdversarial_Idempotency_TerminalStatesNeverClobbered/SQLiteStore
=== RUN   TestAdversarial_Idempotency_TerminalStatesNeverClobbered/JSONStore
--- PASS: TestAdversarial_Idempotency_TerminalStatesNeverClobbered (0.05s)
    --- PASS: TestAdversarial_Idempotency_TerminalStatesNeverClobbered/SQLiteStore (0.02s)
    --- PASS: TestAdversarial_Idempotency_TerminalStatesNeverClobbered/JSONStore (0.03s)
=== RUN   TestAdversarial_CorruptDatabase_Resilience
=== RUN   TestAdversarial_CorruptDatabase_Resilience/SQLite_GarbageBytes
=== RUN   TestAdversarial_CorruptDatabase_Resilience/SQLite_ZeroByteFile
=== RUN   TestAdversarial_CorruptDatabase_Resilience/JSON_TruncatedContent
=== RUN   TestAdversarial_CorruptDatabase_Resilience/JSON_ArrayRootPayload
=== RUN   TestAdversarial_CorruptDatabase_Resilience/JSON_ZeroByteFile
--- PASS: TestAdversarial_CorruptDatabase_Resilience (0.03s)
    --- PASS: TestAdversarial_CorruptDatabase_Resilience/SQLite_GarbageBytes (0.00s)
    --- PASS: TestAdversarial_CorruptDatabase_Resilience/SQLite_ZeroByteFile (0.02s)
    --- PASS: TestAdversarial_CorruptDatabase_Resilience/JSON_TruncatedContent (0.00s)
    --- PASS: TestAdversarial_CorruptDatabase_Resilience/JSON_ArrayRootPayload (0.00s)
    --- PASS: TestAdversarial_CorruptDatabase_Resilience/JSON_ZeroByteFile (0.00s)
=== RUN   TestAdversarial_LargeDataset_1500Matches
=== RUN   TestAdversarial_LargeDataset_1500Matches/SQLiteStore
    adversarial_test.go:764: [SQLiteStore] Inserted 1500 matches in 46.0056ms
    adversarial_test.go:776: [SQLiteStore] ListPendingDownloads returned 500 in 5.2646ms
    adversarial_test.go:796: [SQLiteStore] ListPendingUploads returned 500 in 3.2605ms
    adversarial_test.go:811: [SQLiteStore] Re-upserted 1500 matches in 36.8243ms
=== RUN   TestAdversarial_LargeDataset_1500Matches/JSONStore
    adversarial_test.go:764: [JSONStore] Inserted 1500 matches in 8.8199ms
    adversarial_test.go:776: [JSONStore] ListPendingDownloads returned 500 in 0s
    adversarial_test.go:796: [JSONStore] ListPendingUploads returned 500 in 0s
    adversarial_test.go:811: [JSONStore] Re-upserted 1500 matches in 516.5µs
--- PASS: TestAdversarial_LargeDataset_1500Matches (0.13s)
    --- PASS: TestAdversarial_LargeDataset_1500Matches/SQLiteStore (0.11s)
    --- PASS: TestAdversarial_LargeDataset_1500Matches/JSONStore (0.01s)
=== RUN   TestAdversarial_BoundaryAndEdgeCases
=== RUN   TestAdversarial_BoundaryAndEdgeCases/SQLiteStore
=== RUN   TestAdversarial_BoundaryAndEdgeCases/JSONStore
--- PASS: TestAdversarial_BoundaryAndEdgeCases (0.02s)
    --- PASS: TestAdversarial_BoundaryAndEdgeCases/SQLiteStore (0.01s)
    --- PASS: TestAdversarial_BoundaryAndEdgeCases/JSONStore (0.01s)
=== RUN   TestAdversarial_ContextCancellation
=== RUN   TestAdversarial_ContextCancellation/SQLiteStore
    adversarial_test.go:979: [SQLiteStore] GetMatch correctly rejected canceled context: failed to get match ctx-test-1: context canceled
    adversarial_test.go:988: [SQLiteStore] UpsertDiscoveredMatches correctly rejected canceled context: failed to begin upsert tx: context canceled
=== RUN   TestAdversarial_ContextCancellation/JSONStore
    adversarial_test.go:977: [JSONStore] GetMatch succeeded with canceled context (ignored ctx.Err())
    adversarial_test.go:986: [JSONStore] UpsertDiscoveredMatches succeeded with canceled context (ignored ctx.Err())
--- FAIL: TestAdversarial_ContextCancellation (0.02s)
    --- PASS: TestAdversarial_ContextCancellation/SQLiteStore (0.02s)
    --- FAIL: TestAdversarial_ContextCancellation/JSONStore (0.01s)
=== RUN   TestAdversarial_SkippedReplayURLArrival
=== RUN   TestAdversarial_SkippedReplayURLArrival/SQLiteStore
    adversarial_test.go:1043: [SQLiteStore] Initial download status: SKIPPED
    adversarial_test.go:1062: [SQLiteStore] After ReplayURL arrival: ReplayURL="https://cdn.psynet.gg/delayed_replay.replay", DownloadStatus=SKIPPED
    adversarial_test.go:1069: [SQLiteStore] ListPendingDownloads count: 0
    adversarial_test.go:1072: [SQLiteStore] CRITICAL FINDING: Match initially SKIPPED never transitions to PENDING when ReplayURL arrives (DownloadStatus=SKIPPED, pendingCount=0)
=== RUN   TestAdversarial_SkippedReplayURLArrival/JSONStore
    adversarial_test.go:1043: [JSONStore] Initial download status: SKIPPED
    adversarial_test.go:1062: [JSONStore] After ReplayURL arrival: ReplayURL="https://cdn.psynet.gg/delayed_replay.replay", DownloadStatus=SKIPPED
    adversarial_test.go:1069: [JSONStore] ListPendingDownloads count: 0
    adversarial_test.go:1072: [JSONStore] CRITICAL FINDING: Match initially SKIPPED never transitions to PENDING when ReplayURL arrives (DownloadStatus=SKIPPED, pendingCount=0)
--- FAIL: TestAdversarial_SkippedReplayURLArrival (0.02s)
    --- FAIL: TestAdversarial_SkippedReplayURLArrival/SQLiteStore (0.01s)
    --- FAIL: TestAdversarial_SkippedReplayURLArrival/JSONStore (0.01s)
=== RUN   TestAdversarial_AuthState_EmptyProvider
=== RUN   TestAdversarial_AuthState_EmptyProvider/SQLiteStore
    adversarial_test.go:1115: [SQLiteStore] SaveAuthState with empty provider returned: <nil>
    adversarial_test.go:1118: [SQLiteStore] GetAuthState with empty provider returned: <nil>
=== RUN   TestAdversarial_AuthState_EmptyProvider/JSONStore
    adversarial_test.go:1115: [JSONStore] SaveAuthState with empty provider returned: provider cannot be empty
    adversarial_test.go:1118: [JSONStore] GetAuthState with empty provider returned: provider cannot be empty
--- PASS: TestAdversarial_AuthState_EmptyProvider (0.02s)
    --- PASS: TestAdversarial_AuthState_EmptyProvider/SQLiteStore (0.01s)
    --- PASS: TestAdversarial_AuthState_EmptyProvider/JSONStore (0.00s)
FAIL
FAIL	github.com/dank/rl-api-utils/internal/storage	1.471s
```

### 1.2 Direct Source Code Evidence

1. **Bug 1: Missing Status Transition from SKIPPED to PENDING on ReplayURL Arrival**
   - In `internal/storage/sqlite.go:208-210`:
     ```sql
     ON CONFLICT(match_guid) DO UPDATE SET
         replay_url = CASE WHEN matches.replay_url = '' AND excluded.replay_url != '' THEN excluded.replay_url ELSE matches.replay_url END,
         updated_at = excluded.updated_at;
     ```
     `download_status` is NOT included in the update. If a match had `download_status = 'SKIPPED'`, it remains `'SKIPPED'`.
   - In `internal/storage/jsonstore.go:310-316`:
     ```go
     if exists {
         // Idempotency guarantee: preserve existing progress and statuses
         if existing.ReplayURL == "" && m.ReplayURL != "" {
             existing.ReplayURL = m.ReplayURL
             existing.UpdatedAt = now
             modified = true
         }
         continue
     }
     ```
     `existing.DownloadStatus` is NOT updated from `DownloadSkipped` to `DownloadPending`.

2. **Bug 2: Complete Absence of Context Cancellation Handling in `JSONStore`**
   - In `internal/storage/jsonstore.go:212-616`:
     Every method signature receives `ctx context.Context`, but zero methods check `ctx.Err()`. When a canceled context is passed, `JSONStore` proceeds with locks and file writes. In contrast, `SQLiteStore` respects `ctx` via `database/sql`.

3. **Inconsistency: Empty Provider Handling in `SaveAuthState`**
   - In `internal/storage/jsonstore.go:575-577`:
     `if provider == "" { return errors.New("provider cannot be empty") }`
   - In `internal/storage/sqlite.go:403-419`:
     No check for `provider == ""`. SQLite stores `provider = ""` as valid primary key.

---

## 2. Logic Chain

1. **Delayed Replay URLs are Inevitable in PsyNet**:
   - Rocket League match history queries immediately after a game often return the match record before PsyNet's CDN has finished ingesting and signing the `.replay` payload (`ReplayUrl == ""`).
   - On Cycle 1, the store marks `DownloadStatus = DownloadSkipped`.
   - On Cycle 2 (5 minutes later), PsyNet returns the match WITH `ReplayUrl != ""`.
   - Because both `SQLiteStore` and `JSONStore` only update `replay_url` and leave `download_status = SKIPPED`:
   - `ListPendingDownloads` filters on `WHERE download_status = 'PENDING' AND replay_url != ''`.
   - **Conclusion**: The match will **NEVER be returned by `ListPendingDownloads`** and will **NEVER be downloaded**, permanently violating Requirement R1 in `ORIGINAL_REQUEST.md`.

2. **Context Propagation Violation in `JSONStore`**:
   - Clean Architecture and Go idioms require context awareness across all I/O and synchronization boundaries.
   - If the daemon initiates a graceful shutdown or an operation times out, callers cancel `ctx`.
   - `JSONStore` ignores `ctx.Err()`, creating un-cancellable blocking disk I/O on large stores.

---

## 3. Adversarial Challenge & Stress Report

### Overall Risk Assessment: HIGH

| Dimension | Scenario | Expected Behavior | Actual Behavior | Result |
|---|---|---|---|---|
| **Contention** | 20 goroutines, 400 ops, mixed RW (SQLite) | Zero deadlocks / errors | Zero errors, 0.08s | **PASS** |
| **Contention** | 15 goroutines, 225 ops, mixed RW (JSON) | Zero race conditions | Valid JSON preserved, 0.45s | **PASS** |
| **Crash Recovery** | 200 matches across 5 statuses (`RecoverInFlight`) | DOWNLOADING/UPLOADING reset to PENDING; others untouched | Exact status reset on both stores | **PASS** |
| **Dirty State** | 50 stale `.tmp` files in dir on JSONStore init | Stale `.tmp` deleted, real files preserved | 50 `.tmp` removed, log preserved | **PASS** |
| **Idempotency** | 10 cycles re-upserting DOWNLOADED/UPLOADED/DUPLICATE | Progress/terminal states never overwritten | 100% preserved on both stores | **PASS** |
| **Resilience** | 1KB garbage DB, 0-byte DB, corrupted JSON | Clean error or auto-init | Clean error / auto-init | **PASS** |
| **Scale** | 1500 matches inserted, queried, re-upserted | Pending counts exact (500), sorted ASC | SQLite: 46ms insert, 5.2ms query; JSON: 8.8ms insert, 0ms query | **PASS** |
| **Lifecycle** | ReplayURL arrives after initial SKIPPED status | Status transitions SKIPPED -> PENDING | Status remains SKIPPED; pendingCount=0 | **FAIL (CRITICAL)** |
| **Context** | Canceled context passed to store methods | Returns `context.Canceled` | JSONStore returns `nil` (ignored) | **FAIL (HIGH)** |
| **Boundary** | SaveAuthState with empty provider `""` | Rejects with error | SQLite allows, JSON rejects | **INCONSISTENT (LOW)** |

---

## 4. Caveats

- Implementation was otherwise exceptionally robust: crash recovery, atomic file replacements, WAL concurrency, and terminal state idempotency were 100% verified.
- The two failures are clean, localized logic bugs that do not require any architectural redesign.

---

## 5. Conclusion & Actionable Remediation

**Verdict**: **CHALLENGE_FAILED**

The implementation failed the adversarial challenge due to the delayed replay URL lifecycle bug (Bug 1) and lack of context handling in `JSONStore` (Bug 2).

### Actionable Remediation for `m1_worker_1`:

#### Remediation 1: Fix `internal/storage/sqlite.go` (`UpsertDiscoveredMatches`)
Line 208-210:
```sql
ON CONFLICT(match_guid) DO UPDATE SET
    download_status = CASE WHEN matches.download_status = 'SKIPPED' AND matches.replay_url = '' AND excluded.replay_url != '' THEN 'PENDING' ELSE matches.download_status END,
    replay_url = CASE WHEN matches.replay_url = '' AND excluded.replay_url != '' THEN excluded.replay_url ELSE matches.replay_url END,
    updated_at = excluded.updated_at;
```

#### Remediation 2: Fix `internal/storage/jsonstore.go` (`UpsertDiscoveredMatches`)
Line 310-316:
```go
if exists {
    // Idempotency guarantee: preserve existing progress and statuses
    if existing.ReplayURL == "" && m.ReplayURL != "" {
        existing.ReplayURL = m.ReplayURL
        if existing.DownloadStatus == DownloadSkipped {
            existing.DownloadStatus = DownloadPending
        }
        existing.UpdatedAt = now
        modified = true
    }
    continue
}
```

#### Remediation 3: Add Context Cancellation Checks to `internal/storage/jsonstore.go`
Add to public methods:
```go
if err := ctx.Err(); err != nil {
    return nil, err // or return err
}
```

#### Remediation 4: Validate `provider != ""` in `internal/storage/sqlite.go`
In `SaveAuthState` and `GetAuthState`:
```go
if provider == "" {
    return errors.New("provider cannot be empty")
}
```

---

## 6. Verification Method

```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
cd d:\code\rl-api-utils

# Run all storage tests including adversarial harness
go test -v -count=1 ./internal/storage/...
```

**Invalidation Conditions**:
- If `TestAdversarial_SkippedReplayURLArrival` or `TestAdversarial_ContextCancellation` continues to fail after worker remediation.
