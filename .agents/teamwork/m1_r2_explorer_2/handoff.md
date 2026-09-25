# Milestone 1 (Iteration 2) Explorer 2 Report: JSONStore Delayed Replay URL & Context Cancellation Fix

**Author**: `m1_r2_explorer_2` (Role: explorer)  
**Milestone**: M1 - Storage & Configuration (Iteration 2)  
**Target File**: `internal/storage/jsonstore.go`  
**Test Files**: `internal/storage/adversarial_test.go`, `internal/storage/jsonstore_test.go`  
**Date**: 2026-09-25T03:28:00Z  
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_2`  
**Artifacts Produced**:
- `d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_2\handoff.md` (This report)
- `d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_2\jsonstore.patch` (Unified diff patch)
- `d:\code\rl-api-utils\.agents\teamwork\m1_r2_explorer_2\proposed_jsonstore.go` (Full proposed file)

---

## 1. Observation

### 1.1 Direct Test Failure Reproductions
Running the adversarial test suite against the current codebase:
```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
cd d:\code\rl-api-utils
go test -v -run "TestAdversarial_(ContextCancellation|SkippedReplayURLArrival)" ./internal/storage/...
```

**Verbatim Output for JSONStore**:
```
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
```

### 1.2 Direct Source Code Inspection in `internal/storage/jsonstore.go`

1. **Defect 1: Lack of `DownloadSkipped -> DownloadPending` Transition on Delayed Replay URL Arrival**
   - Lines 310-316 in `internal/storage/jsonstore.go`:
     ```go
     existing, exists := s.matches[m.MatchGUID]
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
   - When a match is first discovered with `m.ReplayURL == ""`, lines 322-324 set `rec.DownloadStatus = DownloadSkipped`.
   - On a subsequent poll cycle when PsyNet CDN provides `m.ReplayURL != ""`, `existing.ReplayURL` is updated, but `existing.DownloadStatus` remains `DownloadSkipped`.
   - In `ListPendingDownloads` (lines 244-245):
     ```go
     if m.DownloadStatus == DownloadPending && m.ReplayURL != "" {
         results = append(results, cloneMatchRecord(m))
     }
     ```
   - Because `existing.DownloadStatus` remains `SKIPPED`, `ListPendingDownloads` ignores the match, permanently preventing download.

2. **Defect 2: Complete Absence of Context Cancellation Handling across all 14 Public Methods**
   - In `internal/storage/jsonstore.go:212-616`, exactly 14 public methods receive `ctx context.Context`, but zero methods evaluate `ctx.Err()`.
   - These 14 methods are:
     1. `GetMatch(ctx context.Context, matchGUID string)` (line 212)
     2. `ListPendingDownloads(ctx context.Context)` (line 234)
     3. `ListPendingUploads(ctx context.Context)` (line 261)
     4. `UpsertDiscoveredMatches(ctx context.Context, matches []*MatchRecord)` (line 288)
     5. `MarkDownloading(ctx context.Context, matchGUID string)` (line 352)
     6. `MarkDownloaded(ctx context.Context, matchGUID, localPath string)` (line 376)
     7. `MarkDownloadFailed(ctx context.Context, matchGUID, errMsg string)` (line 404)
     8. `MarkUploading(ctx context.Context, matchGUID string)` (line 431)
     9. `MarkUploaded(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string)` (line 455)
     10. `MarkDuplicate(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string)` (line 484)
     11. `MarkUploadFailed(ctx context.Context, matchGUID, errMsg string)` (line 513)
     12. `RecoverInFlight(ctx context.Context)` (line 540)
     13. `SaveAuthState(ctx context.Context, provider, refreshToken, accountID, displayName string)` (line 574)
     14. `GetAuthState(ctx context.Context, provider string)` (line 598)

---

## 2. Logic Chain

1. **Delayed Replay URL Lifecycle in PsyNet**:
   - Immediately after a Rocket League match ends, PsyNet RPC `Matches/GetMatchHistory v1` exposes match metadata before its CDN has finalized uploading and signing the `.replay` payload (`ReplayUrl == ""`).
   - The initial ingestion correctly stores this match with `DownloadStatus = DownloadSkipped` (`SKIPPED`).
   - On the subsequent poll (e.g., 5 minutes later), PsyNet returns the same match GUID with `ReplayUrl != ""`.
   - In `JSONStore.UpsertDiscoveredMatches`, updating `existing.ReplayURL` without updating `existing.DownloadStatus` leaves the match permanently in `SKIPPED`.
   - `ListPendingDownloads()` filters on `DownloadStatus == DownloadPending && ReplayURL != ""`. Therefore, the match will never be queued for download, violating Requirement R1.
   - **Remediation**: Check `if existing.DownloadStatus == DownloadSkipped { existing.DownloadStatus = DownloadPending }`. If the match was in another status (e.g. already downloading or failed), it will not be clobbered, preserving idempotency.

2. **Context Propagation and Responsiveness**:
   - The `StateStore` interface enforces `ctx context.Context` on all operational methods to support daemon lifecycle management (e.g., graceful shutdown, per-cycle sync timeouts, cancellations).
   - In `JSONStore`, ignoring `ctx.Err()` allows canceled goroutines to proceed, acquiring mutex locks and executing blocking disk I/O (`saveLocked()` writing temporary files and calling `Sync()`).
   - Checking `if err := ctx.Err(); err != nil` at the entrance of every method returns `ctx.Err()` (such as `context.Canceled` or `context.DeadlineExceeded`) immediately, preventing lock acquisition.
   - In mutating methods, re-checking `if err := ctx.Err(); err != nil { return err }` immediately after `s.mu.Lock()` guarantees that if a context was canceled while waiting in the mutex queue (e.g., during a prior goroutine's disk write), it will immediately release the lock without modifying state or initiating unnecessary disk I/O.

---

## 3. Caveats

- **Scope Separation**: Explorer 1 (`m1_r2_explorer_1`) investigates the SQLite counterpart (`internal/storage/sqlite.go`), while Explorer 3 (`m1_r2_explorer_3`) investigates `internal/config/config.go`. This report exclusively targets `internal/storage/jsonstore.go`.
- **RWMutex Non-Cancellable Wait**: Standard Go `sync.RWMutex` does not provide a cancellable lock acquisition primitive (`LockContext(ctx)`). Therefore, performing a pre-lock check and a post-lock check offers optimal protection against lock contention with expired contexts.
- **Nil Context**: In standard Go, passing a `nil` context is prohibited by contract. Direct calls to `ctx.Err()` will panic on `nil` interface, which is standard Go idiom (identical to standard library `database/sql` and `net/http`).

---

## 4. Conclusion & Actionable Recommendation

### 4.1 Recommended Changes in `internal/storage/jsonstore.go`

#### A. Fix `UpsertDiscoveredMatches` (Lines 310-316):
```go
		existing, exists := s.matches[m.MatchGUID]
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

#### B. Add Context Cancellation Checks to All 14 Public Methods:

1. **`GetMatch`**:
   ```go
   func (s *JSONStore) GetMatch(ctx context.Context, matchGUID string) (*MatchRecord, error) {
   	if err := ctx.Err(); err != nil {
   		return nil, err
   	}
   	if matchGUID == "" {
   		return nil, ErrInvalidGUID
   	}
   ```

2. **`ListPendingDownloads`**:
   ```go
   func (s *JSONStore) ListPendingDownloads(ctx context.Context) ([]*MatchRecord, error) {
   	if err := ctx.Err(); err != nil {
   		return nil, err
   	}
   	s.mu.RLock()
   	defer s.mu.RUnlock()
   ```

3. **`ListPendingUploads`**:
   ```go
   func (s *JSONStore) ListPendingUploads(ctx context.Context) ([]*MatchRecord, error) {
   	if err := ctx.Err(); err != nil {
   		return nil, err
   	}
   	s.mu.RLock()
   	defer s.mu.RUnlock()
   ```

4. **`UpsertDiscoveredMatches`**:
   ```go
   func (s *JSONStore) UpsertDiscoveredMatches(ctx context.Context, matches []*MatchRecord) error {
   	if err := ctx.Err(); err != nil {
   		return err
   	}
   	if len(matches) == 0 {
   		return nil
   	}

   	s.mu.Lock()
   	defer s.mu.Unlock()

   	if err := ctx.Err(); err != nil {
   		return err
   	}

   	if s.closed {
   		return ErrStoreClosed
   	}
   ```

5. **`MarkDownloading`**:
   ```go
   func (s *JSONStore) MarkDownloading(ctx context.Context, matchGUID string) error {
   	if err := ctx.Err(); err != nil {
   		return err
   	}
   	if matchGUID == "" {
   		return ErrInvalidGUID
   	}

   	s.mu.Lock()
   	defer s.mu.Unlock()

   	if err := ctx.Err(); err != nil {
   		return err
   	}
   ```

6. **`MarkDownloaded`**:
   ```go
   func (s *JSONStore) MarkDownloaded(ctx context.Context, matchGUID, localPath string) error {
   	if err := ctx.Err(); err != nil {
   		return err
   	}
   	if matchGUID == "" {
   		return ErrInvalidGUID
   	}

   	s.mu.Lock()
   	defer s.mu.Unlock()

   	if err := ctx.Err(); err != nil {
   		return err
   	}
   ```

7. **`MarkDownloadFailed`**:
   ```go
   func (s *JSONStore) MarkDownloadFailed(ctx context.Context, matchGUID, errMsg string) error {
   	if err := ctx.Err(); err != nil {
   		return err
   	}
   	if matchGUID == "" {
   		return ErrInvalidGUID
   	}

   	s.mu.Lock()
   	defer s.mu.Unlock()

   	if err := ctx.Err(); err != nil {
   		return err
   	}
   ```

8. **`MarkUploading`**:
   ```go
   func (s *JSONStore) MarkUploading(ctx context.Context, matchGUID string) error {
   	if err := ctx.Err(); err != nil {
   		return err
   	}
   	if matchGUID == "" {
   		return ErrInvalidGUID
   	}

   	s.mu.Lock()
   	defer s.mu.Unlock()

   	if err := ctx.Err(); err != nil {
   		return err
   	}
   ```

9. **`MarkUploaded`**:
   ```go
   func (s *JSONStore) MarkUploaded(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error {
   	if err := ctx.Err(); err != nil {
   		return err
   	}
   	if matchGUID == "" {
   		return ErrInvalidGUID
   	}

   	s.mu.Lock()
   	defer s.mu.Unlock()

   	if err := ctx.Err(); err != nil {
   		return err
   	}
   ```

10. **`MarkDuplicate`**:
    ```go
    func (s *JSONStore) MarkDuplicate(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error {
    	if err := ctx.Err(); err != nil {
    		return err
    	}
    	if matchGUID == "" {
    		return ErrInvalidGUID
    	}

    	s.mu.Lock()
    	defer s.mu.Unlock()

    	if err := ctx.Err(); err != nil {
    		return err
    	}
    ```

11. **`MarkUploadFailed`**:
    ```go
    func (s *JSONStore) MarkUploadFailed(ctx context.Context, matchGUID, errMsg string) error {
    	if err := ctx.Err(); err != nil {
    		return err
    	}
    	if matchGUID == "" {
    		return ErrInvalidGUID
    	}

    	s.mu.Lock()
    	defer s.mu.Unlock()

    	if err := ctx.Err(); err != nil {
    		return err
    	}
    ```

12. **`RecoverInFlight`**:
    ```go
    func (s *JSONStore) RecoverInFlight(ctx context.Context) error {
    	if err := ctx.Err(); err != nil {
    		return err
    	}

    	s.mu.Lock()
    	defer s.mu.Unlock()

    	if err := ctx.Err(); err != nil {
    		return err
    	}
    ```

13. **`SaveAuthState`**:
    ```go
    func (s *JSONStore) SaveAuthState(ctx context.Context, provider, refreshToken, accountID, displayName string) error {
    	if err := ctx.Err(); err != nil {
    		return err
    	}
    	if provider == "" {
    		return errors.New("provider cannot be empty")
    	}

    	s.mu.Lock()
    	defer s.mu.Unlock()

    	if err := ctx.Err(); err != nil {
    		return err
    	}
    ```

14. **`GetAuthState`**:
    ```go
    func (s *JSONStore) GetAuthState(ctx context.Context, provider string) (refreshToken, accountID, displayName string, err error) {
    	if err := ctx.Err(); err != nil {
    		return "", "", "", err
    	}
    	if provider == "" {
    		return "", "", "", errors.New("provider cannot be empty")
    	}

    	s.mu.RLock()
    	defer s.mu.RUnlock()
    ```

### 4.2 Comprehensive Unit Test Recommendation for `internal/storage/jsonstore_test.go`

Add this test suite to verify context cancellation across all 14 methods:
```go
func TestJSONStore_ContextCancellation_AllMethods(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewJSONStore(filepath.Join(tempDir, "ctx_all.json"))
	if err != nil {
		t.Fatalf("NewJSONStore failed: %v", err)
	}
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-canceled context

	if _, err := store.GetMatch(ctx, "test"); !errors.Is(err, context.Canceled) {
		t.Errorf("GetMatch expected context.Canceled, got %v", err)
	}
	if _, err := store.ListPendingDownloads(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("ListPendingDownloads expected context.Canceled, got %v", err)
	}
	if _, err := store.ListPendingUploads(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("ListPendingUploads expected context.Canceled, got %v", err)
	}
	if err := store.UpsertDiscoveredMatches(ctx, []*MatchRecord{{MatchGUID: "m1"}}); !errors.Is(err, context.Canceled) {
		t.Errorf("UpsertDiscoveredMatches expected context.Canceled, got %v", err)
	}
	if err := store.MarkDownloading(ctx, "test"); !errors.Is(err, context.Canceled) {
		t.Errorf("MarkDownloading expected context.Canceled, got %v", err)
	}
	if err := store.MarkDownloaded(ctx, "test", "/path"); !errors.Is(err, context.Canceled) {
		t.Errorf("MarkDownloaded expected context.Canceled, got %v", err)
	}
	if err := store.MarkDownloadFailed(ctx, "test", "err"); !errors.Is(err, context.Canceled) {
		t.Errorf("MarkDownloadFailed expected context.Canceled, got %v", err)
	}
	if err := store.MarkUploading(ctx, "test"); !errors.Is(err, context.Canceled) {
		t.Errorf("MarkUploading expected context.Canceled, got %v", err)
	}
	if err := store.MarkUploaded(ctx, "test", "id", "url"); !errors.Is(err, context.Canceled) {
		t.Errorf("MarkUploaded expected context.Canceled, got %v", err)
	}
	if err := store.MarkDuplicate(ctx, "test", "id", "url"); !errors.Is(err, context.Canceled) {
		t.Errorf("MarkDuplicate expected context.Canceled, got %v", err)
	}
	if err := store.MarkUploadFailed(ctx, "test", "err"); !errors.Is(err, context.Canceled) {
		t.Errorf("MarkUploadFailed expected context.Canceled, got %v", err)
	}
	if err := store.RecoverInFlight(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("RecoverInFlight expected context.Canceled, got %v", err)
	}
	if err := store.SaveAuthState(ctx, "epic", "token", "acc", "name"); !errors.Is(err, context.Canceled) {
		t.Errorf("SaveAuthState expected context.Canceled, got %v", err)
	}
	if _, _, _, err := store.GetAuthState(ctx, "epic"); !errors.Is(err, context.Canceled) {
		t.Errorf("GetAuthState expected context.Canceled, got %v", err)
	}
}
```

---

## 5. Verification Method

Once `m1_worker_1` applies the changes:

```powershell
$env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
cd d:\code\rl-api-utils

# 1. Run all JSONStore tests
go test -v -count=1 -run "TestJSONStore" ./internal/storage/...

# 2. Verify Adversarial Context Cancellation passes on JSONStore
go test -v -count=1 -run "TestAdversarial_ContextCancellation" ./internal/storage/...

# 3. Verify Adversarial Skipped Replay URL Arrival passes on JSONStore
go test -v -count=1 -run "TestAdversarial_SkippedReplayURLArrival" ./internal/storage/...
```

**Invalidation Conditions**:
- If `TestAdversarial_ContextCancellation/JSONStore` fails with `ignored ctx.Err()`.
- If `TestAdversarial_SkippedReplayURLArrival/JSONStore` reports `Match initially SKIPPED never transitions to PENDING when ReplayURL arrives`.
- If any existing JSONStore unit test in `internal/storage/jsonstore_test.go` regresses.
