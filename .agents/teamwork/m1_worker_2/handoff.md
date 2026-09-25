# Milestone 1 (Iteration 2) Handoff Report: Storage & Config Remediations

**Author**: `m1_worker_2` (Roles: implementer, qa, specialist)  
**Milestone**: M1 - Storage & Configuration (Iteration 2)  
**Date**: 2026-09-25T03:32:00Z  
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m1_worker_2`  
**Status**: COMPLETE (Hard Handoff)

---

## 1. Observation

### 1.1 Initial Failing Test Suite Observations
Prior to applying changes, running tests produced the following failures:

1. **Storage Failures**:
   Command:
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   go test -v -count=1 ./internal/storage/...
   ```
   Verbatim output:
   ```
   === RUN   TestAdversarial_ContextCancellation/JSONStore
       adversarial_test.go:977: [JSONStore] GetMatch succeeded with canceled context (ignored ctx.Err())
       adversarial_test.go:986: [JSONStore] UpsertDiscoveredMatches succeeded with canceled context (ignored ctx.Err())
   --- FAIL: TestAdversarial_ContextCancellation (0.02s)
       --- PASS: TestAdversarial_ContextCancellation/SQLiteStore (0.01s)
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
   ```

2. **Config Failure**:
   Command:
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   go test -v -count=1 ./internal/config/...
   ```
   Verbatim output:
   ```
   === RUN   TestBug_YAMLRawNanosecondsDecoding
       boundary_test.go:259: BUG CONFIRMED: Duration.UnmarshalYAML failed to parse raw numeric nanoseconds: invalid duration string "300000000000": time: missing unit in duration "300000000000"
   --- FAIL: TestBug_YAMLRawNanosecondsDecoding (0.00s)
   ```

### 1.2 Modifications Applied
The following three files within exclusive write ownership were modified:

1. **`internal/storage/sqlite.go`**:
   - In `UpsertDiscoveredMatches` (line 209):
     Updated the `ON CONFLICT(match_guid) DO UPDATE SET` clause to:
     ```sql
     ON CONFLICT(match_guid) DO UPDATE SET
         download_status = CASE WHEN matches.download_status = 'SKIPPED' AND matches.replay_url = '' AND excluded.replay_url != '' THEN 'PENDING' ELSE matches.download_status END,
         replay_url = CASE WHEN matches.replay_url = '' AND excluded.replay_url != '' THEN excluded.replay_url ELSE matches.replay_url END,
         updated_at = excluded.updated_at;
     ```
   - In `SaveAuthState` (line 405):
     Added fast-fail check:
     ```go
     if provider == "" {
         return errors.New("provider cannot be empty")
     }
     ```
   - In `GetAuthState` (line 424):
     Added fast-fail check:
     ```go
     if provider == "" {
         return "", "", "", errors.New("provider cannot be empty")
     }
     ```

2. **`internal/storage/jsonstore.go`**:
   - Replaced with `d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_2\proposed_jsonstore.go`.
   - In `UpsertDiscoveredMatches`:
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
   - Added context cancellation pre-lock and post-lock validation (`if err := ctx.Err(); err != nil`) across all 14 public `StateStore` methods.

3. **`internal/config/config.go`**:
   - In `Duration.UnmarshalYAML` (lines 54-69):
     Inverted unmarshaling sequence to attempt numeric `int64` decoding prior to string decoding:
     ```go
     func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
         var n int64
         if err := value.Decode(&n); err == nil {
             *d = Duration(time.Duration(n))
             return nil
         }
         var s string
         if err := value.Decode(&s); err == nil {
             parsed, err := time.ParseDuration(s)
             if err != nil {
                 return fmt.Errorf("invalid duration string %q: %w", s, err)
             }
             *d = Duration(parsed)
             return nil
         }
         return fmt.Errorf("cannot unmarshal YAML node into Duration")
     }
     ```

---

## 2. Logic Chain

1. **Delayed Replay URL Synchronization**:
   - In Rocket League match history, matches are often announced before the signed `.replay` CDN download URL is ready. Matches discovered with `ReplayURL == ""` are initially marked `DownloadSkipped` (`SKIPPED`).
   - When a subsequent poll cycle provides `ReplayURL != ""`, the records must transition from `DownloadSkipped` to `DownloadPending` so that `ListPendingDownloads()` includes them.
   - For SQLite, using a SQL `CASE` statement ensures only matches that are currently `SKIPPED` with empty replay URLs transition to `PENDING`, leaving downloading/downloaded/failed records unchanged.
   - For JSONStore, setting `existing.DownloadStatus = DownloadPending` only when `existing.DownloadStatus == DownloadSkipped` produces identical, idempotent semantics.

2. **Context Cancellation Propagation**:
   - The `StateStore` interface accepts `ctx context.Context` on all public methods.
   - For in-memory stores like `JSONStore`, checking `ctx.Err()` before acquiring the lock and immediately after acquiring the lock ensures canceled or timed-out requests abort quickly without performing unwanted mutations or expensive temporary file disk I/O.

3. **Storage Parity (Empty Provider)**:
   - `SaveAuthState` and `GetAuthState` in `JSONStore` reject empty provider names with `errors.New("provider cannot be empty")`.
   - Applying the identical check to `SQLiteStore` ensures engine-agnostic behavioral contract parity.

4. **YAML Raw Numeric Nanosecond Decoding**:
   - In `yaml.v3`, scalar numeric nodes can be decoded into `string` as raw digit strings (e.g., `"300000000000"`).
   - If string decoding is attempted first, `time.ParseDuration("300000000000")` fails with `missing unit in duration`, and the subsequent `int64` decoding is never reached.
   - By attempting `value.Decode(&n)` first, numeric literals are parsed directly into nanoseconds, while string durations (`"5m"`) fall through to string parsing, achieving full parity with `Duration.UnmarshalJSON`.

---

## 3. Caveats

No caveats. All edge cases identified in Iteration 2 are addressed, and all tests across both packages pass cleanly without regressions.

---

## 4. Conclusion

All three Milestone 1 edge-case remediations have been successfully applied and verified:
1. SQLite delayed replay URL transition & empty provider validation are active.
2. JSONStore context cancellation across all 14 methods & delayed replay URL transition are active.
3. Config YAML numeric nanoseconds decoding is active.
4. Standard tests, boundary tests, adversarial tests, and static analysis (`go vet`) pass 100%.

---

## 5. Verification Method

### 5.1 Verification Commands
Run in PowerShell from the project root `d:\code\rl-api-utils`:

```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
cd d:\code\rl-api-utils

# Verify storage package tests
go test -v -count=1 ./internal/storage/...

# Verify config package tests
go test -v -count=1 ./internal/config/...

# Verify static analysis
go vet ./internal/storage/... ./internal/config/...

# Verify entire project test suite
go test ./...
```

### 5.2 Verification Results Obtained
- `go test -v -count=1 ./internal/storage/...`: **PASS** (all tests pass, including `TestAdversarial_ContextCancellation`, `TestAdversarial_SkippedReplayURLArrival`, `TestAdversarial_AuthState_EmptyProvider`)
- `go test -v -count=1 ./internal/config/...`: **PASS** (all tests pass, including `TestBug_YAMLRawNanosecondsDecoding`)
- `go vet ./internal/storage/... ./internal/config/...`: **PASS** (zero warnings / clean exit code 0)
- `go test ./...`: **PASS** (all packages pass)
- `go test -v -count=1 ./test/e2e/...`: **PASS** (all 4 tiers pass)

### 5.3 Invalidation Conditions
- Any test failure in `./internal/storage/...` or `./internal/config/...`.
- Any `go vet` warning on modified files.
- Any regression in E2E tests (`./test/e2e/...`).
