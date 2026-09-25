# Recommendation Report: SQLite Delayed Replay URL Transition & Empty Provider Validation

**Author**: `m1_r2_explorer_1` (Role: Explorer)  
**Target File**: `internal/storage/sqlite.go`  
**Milestone**: M1 - Storage & Configuration (Iteration 2)  
**Date**: 2026-09-25T03:29:00Z  
**Patch Artifact**: `d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_1\sqlite_fixes.patch`

---

## 1. Observation

### 1.1 Direct Defect Observations

#### Defect 1: Missing Transition from `SKIPPED` to `PENDING` in `UpsertDiscoveredMatches`
- **Location**: `internal/storage/sqlite.go:208-210`
- **Existing Code**:
  ```sql
  ON CONFLICT(match_guid) DO UPDATE SET
      replay_url = CASE WHEN matches.replay_url = '' AND excluded.replay_url != '' THEN excluded.replay_url ELSE matches.replay_url END,
      updated_at = excluded.updated_at;
  ```
- **Context in `UpsertDiscoveredMatches` (`sqlite.go:226-233`)**:
  ```go
  dlStatus := m.DownloadStatus
  if dlStatus == "" {
      if m.ReplayURL == "" {
          dlStatus = DownloadSkipped
      } else {
          dlStatus = DownloadPending
      }
  }
  ```
- **Filter in `ListPendingDownloads` (`sqlite.go:154`)**:
  ```sql
  WHERE download_status = 'PENDING' AND replay_url != ''
  ```
- **Verbatim Failure Output** (`go test -v -run TestAdversarial_SkippedReplayURLArrival ./internal/storage/...`):
  ```
  === RUN   TestAdversarial_SkippedReplayURLArrival
  === RUN   TestAdversarial_SkippedReplayURLArrival/SQLiteStore
      adversarial_test.go:1043: [SQLiteStore] Initial download status: SKIPPED
      adversarial_test.go:1062: [SQLiteStore] After ReplayURL arrival: ReplayURL="https://cdn.psynet.gg/delayed_replay.replay", DownloadStatus=SKIPPED
      adversarial_test.go:1069: [SQLiteStore] ListPendingDownloads count: 0
      adversarial_test.go:1072: [SQLiteStore] CRITICAL FINDING: Match initially SKIPPED never transitions to PENDING when ReplayURL arrives (DownloadStatus=SKIPPED, pendingCount=0)
  --- FAIL: TestAdversarial_SkippedReplayURLArrival (0.04s)
      --- FAIL: TestAdversarial_SkippedReplayURLArrival/SQLiteStore (0.02s)
  ```

#### Defect 2: Missing Empty Provider Validation in `SaveAuthState` and `GetAuthState`
- **Location**: `internal/storage/sqlite.go:403-436`
- **Existing Code**:
  ```go
  func (s *SQLiteStore) SaveAuthState(ctx context.Context, provider, refreshToken, accountID, displayName string) error {
      now := time.Now().UTC().Unix()
      const query = `
      INSERT INTO auth_state (provider, refresh_token, account_id, display_name, updated_at)
  ...
  func (s *SQLiteStore) GetAuthState(ctx context.Context, provider string) (refreshToken, accountID, displayName string, err error) {
      const query = `
      SELECT refresh_token, account_id, display_name
      FROM auth_state
      WHERE provider = ?;
  ...
  ```
- **Comparison with `JSONStore` (`internal/storage/jsonstore.go:575, 599`)**:
  ```go
  if provider == "" {
      return errors.New("provider cannot be empty")
  }
  ```
- **Verbatim Discrepancy Log** (`go test -v -run TestAdversarial_AuthState_EmptyProvider ./internal/storage/...`):
  ```
  [SQLiteStore] SaveAuthState with empty provider returned: <nil>
  [SQLiteStore] GetAuthState with empty provider returned: <nil>
  [JSONStore] SaveAuthState with empty provider returned: provider cannot be empty
  [JSONStore] GetAuthState with empty provider returned: provider cannot be empty
  ```

---

## 2. Logic Chain

1. **PsyNet Replay Signing Latency (Domain Context)**:
   - When Rocket League games finish, the PsyNet match history endpoint (`Matches/GetMatchHistory v1`) exposes match metadata immediately, but the CDN signed `.replay` download URL is often generated asynchronously seconds to minutes later (`ReplayUrl == ""`).
   - On the initial polling cycle (Cycle 1), `UpsertDiscoveredMatches` assigns `download_status = 'SKIPPED'` because `ReplayURL == ""` (lines 228–232).

2. **The Orphaned Replay Defect**:
   - On Cycle 2 (5 minutes later), PsyNet returns the same match with `ReplayURL != ""` populated.
   - `UpsertDiscoveredMatches` executes an `INSERT ... ON CONFLICT(match_guid) DO UPDATE SET`.
   - In the unpatched code, only `replay_url` and `updated_at` were updated; `download_status` was not modified and remained `'SKIPPED'`.
   - `ListPendingDownloads` queries `WHERE download_status = 'PENDING' AND replay_url != ''`.
   - Because `download_status` remained `'SKIPPED'`, the match was permanently excluded from `ListPendingDownloads()`, meaning the `.replay` payload was never downloaded, directly violating Requirement R1 of `ORIGINAL_REQUEST.md`.

3. **Analysis of the Proposed SQL CASE Transition**:
   - The proposed update clause is:
     ```sql
     ON CONFLICT(match_guid) DO UPDATE SET
         download_status = CASE WHEN matches.download_status = 'SKIPPED' AND matches.replay_url = '' AND excluded.replay_url != '' THEN 'PENDING' ELSE matches.download_status END,
         replay_url = CASE WHEN matches.replay_url = '' AND excluded.replay_url != '' THEN excluded.replay_url ELSE matches.replay_url END,
         updated_at = excluded.updated_at;
     ```
   - **Condition Evaluation**:
     1. `matches.download_status = 'SKIPPED'` ensures that records already in progress (`'DOWNLOADING'`), completed (`'DOWNLOADED'`), or failed (`'FAILED'`) are never touched.
     2. `matches.replay_url = ''` ensures that if a record had already been associated with a replay URL, its status is not altered.
     3. `excluded.replay_url != ''` ensures that if the subsequent cycle still lacks a replay URL, the record remains `'SKIPPED'`.
     4. Only when all three conditions are met does `download_status` transition to `'PENDING'`.
     5. If re-upserted on Cycle 3 (with `download_status = 'PENDING'` and `replay_url != ''`), `matches.download_status = 'SKIPPED'` is false, so it remains `'PENDING'`, guaranteeing complete idempotency.

4. **Analysis of Empty Provider Validation**:
   - `StateStore` interface contract mandates consistent behavior regardless of storage engine.
   - `JSONStore` rejects empty provider strings with `errors.New("provider cannot be empty")`.
   - `SQLiteStore` should perform an identical fast-fail check at method entry before acquiring connections or executing queries:
     - In `SaveAuthState`: `if provider == "" { return errors.New("provider cannot be empty") }`
     - In `GetAuthState`: `if provider == "" { return "", "", "", errors.New("provider cannot be empty") }`

---

## 3. Caveats

- **No Schema Changes Required**: The existing SQLite schema (`matches` and `auth_state` tables) already supports these transitions without needing migrations or alter table statements.
- **Scope Isolation**: This analysis is strictly focused on `internal/storage/sqlite.go`. Peer components (`internal/storage/jsonstore.go` and `internal/config/config.go`) are explored by `m1_r2_explorer_2` and `m1_r2_explorer_3`.
- **Read-Only Explorer Discipline**: `internal/storage/sqlite.go` was temporarily tested in-memory/disk to confirm test suite green state, and then immediately reverted back to its original state so that no unauthorized modifications remain in the tree. Implementation is delegated to `m1_worker_1`.

---

## 4. Conclusion & Proposed Code Changes

### Recommended Code Modifications

#### Modification 1: `internal/storage/sqlite.go:208-210` (`UpsertDiscoveredMatches`)

**Before**:
```sql
	ON CONFLICT(match_guid) DO UPDATE SET
	    replay_url = CASE WHEN matches.replay_url = '' AND excluded.replay_url != '' THEN excluded.replay_url ELSE matches.replay_url END,
	    updated_at = excluded.updated_at;
```

**After**:
```sql
	ON CONFLICT(match_guid) DO UPDATE SET
	    download_status = CASE WHEN matches.download_status = 'SKIPPED' AND matches.replay_url = '' AND excluded.replay_url != '' THEN 'PENDING' ELSE matches.download_status END,
	    replay_url = CASE WHEN matches.replay_url = '' AND excluded.replay_url != '' THEN excluded.replay_url ELSE matches.replay_url END,
	    updated_at = excluded.updated_at;
```

#### Modification 2: `internal/storage/sqlite.go:403` (`SaveAuthState`)

**Before**:
```go
// SaveAuthState upserts authentication tokens and metadata for a provider.
func (s *SQLiteStore) SaveAuthState(ctx context.Context, provider, refreshToken, accountID, displayName string) error {
	now := time.Now().UTC().Unix()
```

**After**:
```go
// SaveAuthState upserts authentication tokens and metadata for a provider.
func (s *SQLiteStore) SaveAuthState(ctx context.Context, provider, refreshToken, accountID, displayName string) error {
	if provider == "" {
		return errors.New("provider cannot be empty")
	}

	now := time.Now().UTC().Unix()
```

#### Modification 3: `internal/storage/sqlite.go:422` (`GetAuthState`)

**Before**:
```go
// GetAuthState retrieves persisted authentication metadata for a given provider.
func (s *SQLiteStore) GetAuthState(ctx context.Context, provider string) (refreshToken, accountID, displayName string, err error) {
	const query = `
```

**After**:
```go
// GetAuthState retrieves persisted authentication metadata for a given provider.
func (s *SQLiteStore) GetAuthState(ctx context.Context, provider string) (refreshToken, accountID, displayName string, err error) {
	if provider == "" {
		return "", "", "", errors.New("provider cannot be empty")
	}

	const query = `
```

---

## 5. Verification Method

### 5.1 Verification Commands
To independently verify the fix once applied by `m1_worker_1`:

```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
cd d:\code\rl-api-utils

# 1. Verify standard SQLite unit test suite (9 tests)
go test -v -count=1 -run "TestSQLiteStore" ./internal/storage/...

# 2. Verify delayed replay URL transition fix
go test -v -count=1 -run "TestAdversarial_SkippedReplayURLArrival/SQLiteStore" ./internal/storage/...

# 3. Verify empty provider validation parity
go test -v -count=1 -run "TestAdversarial_AuthState_EmptyProvider/SQLiteStore" ./internal/storage/...

# 4. Verify all adversarial SQLite subtests
go test -v -count=1 -run "/SQLite" ./internal/storage/...
```

### 5.2 Empirical Verification Results Obtained During Investigation
When the changes were applied, the following test outputs were confirmed:
- `TestSQLiteStore_*`: 9 passed out of 9 (0.582s).
- `TestAdversarial_SkippedReplayURLArrival/SQLiteStore`:
  ```
  === RUN   TestAdversarial_SkippedReplayURLArrival
  === RUN   TestAdversarial_SkippedReplayURLArrival/SQLiteStore
      adversarial_test.go:1043: [SQLiteStore] Initial download status: SKIPPED
      adversarial_test.go:1062: [SQLiteStore] After ReplayURL arrival: ReplayURL="https://cdn.psynet.gg/delayed_replay.replay", DownloadStatus=PENDING
      adversarial_test.go:1069: [SQLiteStore] ListPendingDownloads count: 1
  --- PASS: TestAdversarial_SkippedReplayURLArrival (0.01s)
      --- PASS: TestAdversarial_SkippedReplayURLArrival/SQLiteStore (0.01s)
  PASS
  ```
- `TestAdversarial_AuthState_EmptyProvider/SQLiteStore`:
  ```
  === RUN   TestAdversarial_AuthState_EmptyProvider/SQLiteStore
      adversarial_test.go:1115: [SQLiteStore] SaveAuthState with empty provider returned: provider cannot be empty
      adversarial_test.go:1118: [SQLiteStore] GetAuthState with empty provider returned: provider cannot be empty
  --- PASS: TestAdversarial_AuthState_EmptyProvider/SQLiteStore (0.01s)
  ```
- `TestAdversarial_ConcurrencyContention_SQLite`: PASS
- `TestAdversarial_CrashRecovery_RecoverInFlight/SQLiteStore`: PASS
- `TestAdversarial_Idempotency_TerminalStatesNeverClobbered/SQLiteStore`: PASS
- `TestAdversarial_LargeDataset_1500Matches/SQLiteStore`: PASS (1500 records inserted in 60.8ms, queried in 3.6ms)
- `TestAdversarial_BoundaryAndEdgeCases/SQLiteStore`: PASS
- `TestAdversarial_ContextCancellation/SQLiteStore`: PASS

### 5.3 Invalidation Conditions
- If applying the patch causes `TestSQLiteStore_CRUDAndIdempotency` or `TestSQLiteStore_DownloadTransitions` to fail.
- If `TestAdversarial_SkippedReplayURLArrival/SQLiteStore` does not show `DownloadStatus=PENDING` and `ListPendingDownloads count: 1` after Cycle 2.
- If `SaveAuthState(ctx, "", ...)` does not return `"provider cannot be empty"`.
