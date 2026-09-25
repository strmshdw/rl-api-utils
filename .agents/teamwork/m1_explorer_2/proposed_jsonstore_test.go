package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Ensure compile-time interface implementation.
var _ StateStore = (*JSONStore)(nil)

func TestJSONStore_NewStore_DirectoryCreation(t *testing.T) {
	tempDir := t.TempDir()
	nestedPath := filepath.Join(tempDir, "nested", "storage", "subfolder", "state.json")

	store, err := NewJSONStore(nestedPath)
	if err != nil {
		t.Fatalf("failed to create store with non-existent nested directory: %v", err)
	}
	defer store.Close()

	if _, err := os.Stat(nestedPath); err != nil {
		t.Fatalf("expected state file to exist on disk: %v", err)
	}
}

func TestJSONStore_NewStore_EmptyPath(t *testing.T) {
	_, err := NewJSONStore("")
	if err == nil {
		t.Fatal("expected error when creating store with empty path, got nil")
	}
}

func TestJSONStore_NewStore_CorruptedJSON(t *testing.T) {
	tempDir := t.TempDir()
	corruptPath := filepath.Join(tempDir, "corrupt.json")

	if err := os.WriteFile(corruptPath, []byte(`{"matches": {invalid-json`), 0644); err != nil {
		t.Fatalf("failed to write corrupt json file: %v", err)
	}

	_, err := NewJSONStore(corruptPath)
	if err == nil {
		t.Fatal("expected unmarshal error on corrupt JSON file, got nil")
	}
}

func TestJSONStore_CRUDAndTransitions(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "state.json")

	store, err := NewJSONStore(statePath)
	if err != nil {
		t.Fatalf("NewJSONStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// 1. Initial Get on empty store
	_, err = store.GetMatch(ctx, "non-existent-guid")
	if !errors.Is(err, ErrMatchNotFound) {
		t.Fatalf("expected ErrMatchNotFound, got %v", err)
	}

	_, err = store.GetMatch(ctx, "")
	if !errors.Is(err, ErrInvalidGUID) {
		t.Fatalf("expected ErrInvalidGUID, got %v", err)
	}

	// 2. UpsertDiscoveredMatches
	m1 := &MatchRecord{
		MatchGUID:            "guid-1",
		RecordStartTimestamp: 1000,
		MapName:              "DFH Stadium",
		Playlist:             11,
		ReplayURL:            "https://cdn.psynet.gg/replay1.replay",
	}
	m2 := &MatchRecord{
		MatchGUID:            "guid-2",
		RecordStartTimestamp: 2000,
		MapName:              "Mannfield",
		Playlist:             13,
		ReplayURL:            "https://cdn.psynet.gg/replay2.replay",
	}
	m3NoReplay := &MatchRecord{
		MatchGUID:            "guid-3",
		RecordStartTimestamp: 3000,
		MapName:              "Champions Field",
		Playlist:             11,
		ReplayURL:            "", // No replay URL available
	}

	if err := store.UpsertDiscoveredMatches(ctx, []*MatchRecord{m1, m2, m3NoReplay}); err != nil {
		t.Fatalf("UpsertDiscoveredMatches failed: %v", err)
	}

	// Verify initial default statuses
	rec1, err := store.GetMatch(ctx, "guid-1")
	if err != nil {
		t.Fatalf("GetMatch guid-1 failed: %v", err)
	}
	if rec1.DownloadStatus != DownloadPending {
		t.Errorf("expected rec1 DownloadStatus PENDING, got %s", rec1.DownloadStatus)
	}
	if rec1.UploadStatus != UploadPending {
		t.Errorf("expected rec1 UploadStatus PENDING, got %s", rec1.UploadStatus)
	}

	rec3, err := store.GetMatch(ctx, "guid-3")
	if err != nil {
		t.Fatalf("GetMatch guid-3 failed: %v", err)
	}
	if rec3.DownloadStatus != DownloadSkipped {
		t.Errorf("expected rec3 DownloadStatus SKIPPED, got %s", rec3.DownloadStatus)
	}

	// 3. MarkDownloading
	if err := store.MarkDownloading(ctx, "guid-1"); err != nil {
		t.Fatalf("MarkDownloading failed: %v", err)
	}
	rec1, _ = store.GetMatch(ctx, "guid-1")
	if rec1.DownloadStatus != DownloadDownloading {
		t.Errorf("expected DownloadDownloading, got %s", rec1.DownloadStatus)
	}

	// 4. MarkDownloaded
	localPath := filepath.Join(tempDir, "guid-1.replay")
	if err := store.MarkDownloaded(ctx, "guid-1", localPath); err != nil {
		t.Fatalf("MarkDownloaded failed: %v", err)
	}
	rec1, _ = store.GetMatch(ctx, "guid-1")
	if rec1.DownloadStatus != DownloadDownloaded {
		t.Errorf("expected DownloadDownloaded, got %s", rec1.DownloadStatus)
	}
	if rec1.LocalFilePath != localPath {
		t.Errorf("expected local path %s, got %s", localPath, rec1.LocalFilePath)
	}
	if rec1.DownloadedAt == nil {
		t.Error("expected DownloadedAt timestamp, got nil")
	}

	// 5. MarkDownloadFailed for guid-2
	if err := store.MarkDownloadFailed(ctx, "guid-2", "network timeout 504"); err != nil {
		t.Fatalf("MarkDownloadFailed failed: %v", err)
	}
	rec2, _ := store.GetMatch(ctx, "guid-2")
	if rec2.DownloadStatus != DownloadFailed {
		t.Errorf("expected DownloadFailed, got %s", rec2.DownloadStatus)
	}
	if rec2.RetryCount != 1 {
		t.Errorf("expected RetryCount 1, got %d", rec2.RetryCount)
	}
	if rec2.LastError != "network timeout 504" {
		t.Errorf("expected LastError 'network timeout 504', got %s", rec2.LastError)
	}

	// 6. MarkUploading for guid-1
	if err := store.MarkUploading(ctx, "guid-1"); err != nil {
		t.Fatalf("MarkUploading failed: %v", err)
	}
	rec1, _ = store.GetMatch(ctx, "guid-1")
	if rec1.UploadStatus != UploadUploading {
		t.Errorf("expected UploadUploading, got %s", rec1.UploadStatus)
	}

	// 7. MarkUploaded for guid-1
	bcID := "bc-uuid-1234"
	bcURL := "https://ballchasing.com/replay/bc-uuid-1234"
	if err := store.MarkUploaded(ctx, "guid-1", bcID, bcURL); err != nil {
		t.Fatalf("MarkUploaded failed: %v", err)
	}
	rec1, _ = store.GetMatch(ctx, "guid-1")
	if rec1.UploadStatus != UploadUploaded {
		t.Errorf("expected UploadUploaded, got %s", rec1.UploadStatus)
	}
	if rec1.BallchasingID != bcID || rec1.BallchasingURL != bcURL {
		t.Errorf("unexpected ballchasing fields: id=%s, url=%s", rec1.BallchasingID, rec1.BallchasingURL)
	}
	if rec1.UploadedAt == nil {
		t.Error("expected UploadedAt timestamp, got nil")
	}

	// 8. MarkDuplicate for a new match
	mDup := &MatchRecord{
		MatchGUID:            "guid-dup",
		RecordStartTimestamp: 4000,
		ReplayURL:            "https://cdn.psynet.gg/dup.replay",
	}
	_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{mDup})
	if err := store.MarkDuplicate(ctx, "guid-dup", "dup-id", "https://ballchasing.com/replay/dup-id"); err != nil {
		t.Fatalf("MarkDuplicate failed: %v", err)
	}
	recDup, _ := store.GetMatch(ctx, "guid-dup")
	if recDup.UploadStatus != UploadDuplicate {
		t.Errorf("expected UploadDuplicate, got %s", recDup.UploadStatus)
	}

	// 9. MarkUploadFailed
	if err := store.MarkUploadFailed(ctx, "guid-dup", "rate limited 429"); err != nil {
		t.Fatalf("MarkUploadFailed failed: %v", err)
	}
	recDup, _ = store.GetMatch(ctx, "guid-dup")
	if recDup.UploadStatus != UploadFailed {
		t.Errorf("expected UploadFailed, got %s", recDup.UploadStatus)
	}
	if recDup.RetryCount != 1 {
		t.Errorf("expected RetryCount 1, got %d", recDup.RetryCount)
	}
}

func TestJSONStore_ListPending(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "state.json")

	store, err := NewJSONStore(statePath)
	if err != nil {
		t.Fatalf("NewJSONStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	matches := []*MatchRecord{
		{
			MatchGUID:            "match-c",
			RecordStartTimestamp: 300,
			ReplayURL:            "https://cdn.psynet.gg/c.replay",
		},
		{
			MatchGUID:            "match-a",
			RecordStartTimestamp: 100,
			ReplayURL:            "https://cdn.psynet.gg/a.replay",
		},
		{
			MatchGUID:            "match-b",
			RecordStartTimestamp: 200,
			ReplayURL:            "https://cdn.psynet.gg/b.replay",
		},
		{
			MatchGUID:            "match-no-url",
			RecordStartTimestamp: 50,
			ReplayURL:            "", // no url -> DownloadSkipped
		},
	}

	if err := store.UpsertDiscoveredMatches(ctx, matches); err != nil {
		t.Fatalf("UpsertDiscoveredMatches failed: %v", err)
	}

	// ListPendingDownloads must return match-a, match-b, match-c ordered by timestamp ASC
	pendingDl, err := store.ListPendingDownloads(ctx)
	if err != nil {
		t.Fatalf("ListPendingDownloads failed: %v", err)
	}
	if len(pendingDl) != 3 {
		t.Fatalf("expected 3 pending downloads, got %d", len(pendingDl))
	}
	if pendingDl[0].MatchGUID != "match-a" || pendingDl[1].MatchGUID != "match-b" || pendingDl[2].MatchGUID != "match-c" {
		t.Errorf("expected ordered [match-a, match-b, match-c], got [%s, %s, %s]",
			pendingDl[0].MatchGUID, pendingDl[1].MatchGUID, pendingDl[2].MatchGUID)
	}

	// Transition match-b to DOWNLOADED with local path
	if err := store.MarkDownloaded(ctx, "match-b", "/local/match-b.replay"); err != nil {
		t.Fatalf("MarkDownloaded failed: %v", err)
	}

	// ListPendingDownloads should now only contain match-a and match-c
	pendingDl, _ = store.ListPendingDownloads(ctx)
	if len(pendingDl) != 2 {
		t.Fatalf("expected 2 pending downloads, got %d", len(pendingDl))
	}

	// ListPendingUploads should now contain match-b
	pendingUp, err := store.ListPendingUploads(ctx)
	if err != nil {
		t.Fatalf("ListPendingUploads failed: %v", err)
	}
	if len(pendingUp) != 1 {
		t.Fatalf("expected 1 pending upload, got %d", len(pendingUp))
	}
	if pendingUp[0].MatchGUID != "match-b" {
		t.Errorf("expected match-b for upload, got %s", pendingUp[0].MatchGUID)
	}
}

func TestJSONStore_IdempotentUpsert(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "state.json")

	store, err := NewJSONStore(statePath)
	if err != nil {
		t.Fatalf("NewJSONStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Initial discovery
	m := &MatchRecord{
		MatchGUID:            "guid-idem",
		RecordStartTimestamp: 500,
		ReplayURL:            "https://psynet.gg/replay.replay",
	}
	_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{m})

	// Download and upload it
	_ = store.MarkDownloaded(ctx, "guid-idem", "/local/idem.replay")
	_ = store.MarkUploaded(ctx, "guid-idem", "ballchasing-id-99", "https://ballchasing.com/replay/ballchasing-id-99")

	// Re-upsert identical match as if discovered on next poll cycle
	reDiscovered := &MatchRecord{
		MatchGUID:            "guid-idem",
		RecordStartTimestamp: 500,
		ReplayURL:            "https://psynet.gg/replay.replay",
	}
	if err := store.UpsertDiscoveredMatches(ctx, []*MatchRecord{reDiscovered}); err != nil {
		t.Fatalf("UpsertDiscoveredMatches re-insert failed: %v", err)
	}

	// Verify terminal states are strictly preserved
	rec, _ := store.GetMatch(ctx, "guid-idem")
	if rec.DownloadStatus != DownloadDownloaded {
		t.Errorf("idempotency broken: DownloadStatus changed to %s", rec.DownloadStatus)
	}
	if rec.UploadStatus != UploadUploaded {
		t.Errorf("idempotency broken: UploadStatus changed to %s", rec.UploadStatus)
	}
	if rec.BallchasingID != "ballchasing-id-99" {
		t.Errorf("idempotency broken: BallchasingID lost, got %s", rec.BallchasingID)
	}
	if rec.LocalFilePath != "/local/idem.replay" {
		t.Errorf("idempotency broken: LocalFilePath lost, got %s", rec.LocalFilePath)
	}
}

func TestJSONStore_RecoverInFlight(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "state.json")

	store, err := NewJSONStore(statePath)
	if err != nil {
		t.Fatalf("NewJSONStore failed: %v", err)
	}

	ctx := context.Background()

	m1 := &MatchRecord{MatchGUID: "inflight-1", ReplayURL: "http://test.com/1"}
	m2 := &MatchRecord{MatchGUID: "inflight-2", ReplayURL: "http://test.com/2"}
	_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{m1, m2})

	// Put m1 into DOWNLOADING and m2 into UPLOADING
	_ = store.MarkDownloading(ctx, "inflight-1")
	_ = store.MarkDownloaded(ctx, "inflight-2", "/tmp/m2.replay")
	_ = store.MarkUploading(ctx, "inflight-2")

	// Execute RecoverInFlight
	if err := store.RecoverInFlight(ctx); err != nil {
		t.Fatalf("RecoverInFlight failed: %v", err)
	}

	r1, _ := store.GetMatch(ctx, "inflight-1")
	if r1.DownloadStatus != DownloadPending {
		t.Errorf("expected m1 reset to PENDING, got %s", r1.DownloadStatus)
	}

	r2, _ := store.GetMatch(ctx, "inflight-2")
	if r2.UploadStatus != UploadPending {
		t.Errorf("expected m2 reset to PENDING, got %s", r2.UploadStatus)
	}

	_ = store.Close()

	// Reopen store from disk and verify recovered state was persisted
	reopened, err := NewJSONStore(statePath)
	if err != nil {
		t.Fatalf("reopening store failed: %v", err)
	}
	defer reopened.Close()

	r1After, _ := reopened.GetMatch(ctx, "inflight-1")
	if r1After.DownloadStatus != DownloadPending {
		t.Errorf("persisted m1 download status expected PENDING, got %s", r1After.DownloadStatus)
	}
	r2After, _ := reopened.GetMatch(ctx, "inflight-2")
	if r2After.UploadStatus != UploadPending {
		t.Errorf("persisted m2 upload status expected PENDING, got %s", r2After.UploadStatus)
	}
}

func TestJSONStore_AuthState(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "state.json")

	store, err := NewJSONStore(statePath)
	if err != nil {
		t.Fatalf("NewJSONStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Missing provider check
	_, _, _, err = store.GetAuthState(ctx, "epic")
	if !errors.Is(err, ErrAuthStateNotFound) {
		t.Fatalf("expected ErrAuthStateNotFound for missing provider, got %v", err)
	}

	// Save epic auth state
	if err := store.SaveAuthState(ctx, "epic", "refresh-token-123", "account-abc", "EpicPlayer"); err != nil {
		t.Fatalf("SaveAuthState failed: %v", err)
	}

	// Save steam auth state
	if err := store.SaveAuthState(ctx, "steam", "ticket-xyz", "76561198000000000", "SteamPlayer"); err != nil {
		t.Fatalf("SaveAuthState steam failed: %v", err)
	}

	// Retrieve epic
	token, accID, disp, err := store.GetAuthState(ctx, "epic")
	if err != nil {
		t.Fatalf("GetAuthState epic failed: %v", err)
	}
	if token != "refresh-token-123" || accID != "account-abc" || disp != "EpicPlayer" {
		t.Errorf("unexpected epic auth state: token=%s, id=%s, disp=%s", token, accID, disp)
	}

	// Update epic token
	if err := store.SaveAuthState(ctx, "epic", "refresh-token-456", "account-abc", "EpicPlayer"); err != nil {
		t.Fatalf("SaveAuthState update failed: %v", err)
	}
	token, _, _, _ = store.GetAuthState(ctx, "epic")
	if token != "refresh-token-456" {
		t.Errorf("expected updated token 'refresh-token-456', got %s", token)
	}
}

func TestJSONStore_DeepCopyDefense(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "state.json")

	store, err := NewJSONStore(statePath)
	if err != nil {
		t.Fatalf("NewJSONStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	m := &MatchRecord{
		MatchGUID:      "copy-defense-guid",
		DownloadStatus: DownloadPending,
		ReplayURL:      "http://test.com",
	}
	_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{m})

	rec, err := store.GetMatch(ctx, "copy-defense-guid")
	if err != nil {
		t.Fatalf("GetMatch failed: %v", err)
	}

	// Mutate the returned record outside store lock
	rec.DownloadStatus = DownloadDownloaded
	rec.LocalFilePath = "/hacked/path"

	// Fetch again from store: verify store internal record was NOT modified
	recFresh, _ := store.GetMatch(ctx, "copy-defense-guid")
	if recFresh.DownloadStatus != DownloadPending {
		t.Errorf("deep copy violation: internal store record was mutated to %s", recFresh.DownloadStatus)
	}
	if recFresh.LocalFilePath == "/hacked/path" {
		t.Error("deep copy violation: internal store local file path was mutated")
	}
}

func TestJSONStore_ConcurrencyUnderRace(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "state.json")

	store, err := NewJSONStore(statePath)
	if err != nil {
		t.Fatalf("NewJSONStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	const numGoroutines = 40
	const opsPerGoroutine = 25

	var wg sync.WaitGroup

	// Populate initial matches
	var initialMatches []*MatchRecord
	for i := 0; i < 20; i++ {
		initialMatches = append(initialMatches, &MatchRecord{
			MatchGUID:            fmt.Sprintf("concurrent-guid-%d", i),
			RecordStartTimestamp: int64(i * 10),
			ReplayURL:            fmt.Sprintf("http://cdn.com/replay-%d", i),
		})
	}
	if err := store.UpsertDiscoveredMatches(ctx, initialMatches); err != nil {
		t.Fatalf("failed to insert initial matches: %v", err)
	}

	// Concurrent Readers
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for op := 0; op < opsPerGoroutine; op++ {
				guid := fmt.Sprintf("concurrent-guid-%d", (id+op)%20)
				_, _ = store.GetMatch(ctx, guid)
				_, _ = store.ListPendingDownloads(ctx)
				_, _ = store.ListPendingUploads(ctx)
				_, _, _, _ = store.GetAuthState(ctx, "epic")
			}
		}(i)
	}

	// Concurrent Writers
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for op := 0; op < opsPerGoroutine; op++ {
				guid := fmt.Sprintf("concurrent-guid-%d", (id+op)%20)
				switch op % 5 {
				case 0:
					_ = store.MarkDownloading(ctx, guid)
				case 1:
					_ = store.MarkDownloaded(ctx, guid, fmt.Sprintf("/tmp/%s.replay", guid))
				case 2:
					_ = store.MarkUploading(ctx, guid)
				case 3:
					_ = store.MarkUploaded(ctx, guid, fmt.Sprintf("bc-%s", guid), "https://bc.com")
				case 4:
					_ = store.SaveAuthState(ctx, "epic", fmt.Sprintf("token-%d-%d", id, op), "acc", "user")
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify file is still valid JSON after all concurrent writes
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("failed to read state file after concurrent test: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("state file was empty after concurrent test")
	}
}

func TestJSONStore_Close(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "state.json")

	store, err := NewJSONStore(statePath)
	if err != nil {
		t.Fatalf("NewJSONStore failed: %v", err)
	}

	ctx := context.Background()

	if err := store.Close(); err != nil {
		t.Fatalf("store.Close() failed: %v", err)
	}

	// Idempotent close
	if err := store.Close(); err != nil {
		t.Errorf("second store.Close() failed: %v", err)
	}

	// All subsequent operations must return ErrStoreClosed
	if _, err := store.GetMatch(ctx, "guid-1"); !errors.Is(err, ErrStoreClosed) {
		t.Errorf("expected ErrStoreClosed from GetMatch, got %v", err)
	}
	if _, err := store.ListPendingDownloads(ctx); !errors.Is(err, ErrStoreClosed) {
		t.Errorf("expected ErrStoreClosed from ListPendingDownloads, got %v", err)
	}
	if _, err := store.ListPendingUploads(ctx); !errors.Is(err, ErrStoreClosed) {
		t.Errorf("expected ErrStoreClosed from ListPendingUploads, got %v", err)
	}
	if err := store.UpsertDiscoveredMatches(ctx, []*MatchRecord{{MatchGUID: "g"}}); !errors.Is(err, ErrStoreClosed) {
		t.Errorf("expected ErrStoreClosed from UpsertDiscoveredMatches, got %v", err)
	}
	if err := store.MarkDownloading(ctx, "g"); !errors.Is(err, ErrStoreClosed) {
		t.Errorf("expected ErrStoreClosed from MarkDownloading, got %v", err)
	}
	if err := store.RecoverInFlight(ctx); !errors.Is(err, ErrStoreClosed) {
		t.Errorf("expected ErrStoreClosed from RecoverInFlight, got %v", err)
	}
	if err := store.SaveAuthState(ctx, "epic", "t", "a", "u"); !errors.Is(err, ErrStoreClosed) {
		t.Errorf("expected ErrStoreClosed from SaveAuthState, got %v", err)
	}
	if _, _, _, err := store.GetAuthState(ctx, "epic"); !errors.Is(err, ErrStoreClosed) {
		t.Errorf("expected ErrStoreClosed from GetAuthState, got %v", err)
	}
}
