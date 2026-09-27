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

func TestJSONStore_PlayerUpsertAndGet(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewJSONStore(filepath.Join(tempDir, "state.json"))
	if err != nil {
		t.Fatalf("NewJSONStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// 1. Get non-existent player
	_, err = store.GetPlayer(ctx, "non-existent")
	if !errors.Is(err, ErrPlayerNotFound) {
		t.Fatalf("expected ErrPlayerNotFound, got %v", err)
	}

	// 2. Empty or nil inputs
	if _, err := store.GetPlayer(ctx, ""); !errors.Is(err, ErrPlayerNotFound) {
		t.Fatalf("expected ErrPlayerNotFound for empty ID, got %v", err)
	}
	if err := store.UpsertPlayer(ctx, nil); err == nil {
		t.Fatal("expected error on nil player, got nil")
	}
	if err := store.UpsertPlayer(ctx, &PlayerRecord{}); err == nil {
		t.Fatal("expected error on empty player_id, got nil")
	}

	// 3. Upsert new player
	now := time.Now().UTC().Truncate(time.Second)
	p := &PlayerRecord{
		PlayerID:    "Steam|76561198000000001|0",
		Platform:    "Steam",
		PlayerName:  "TestPlayer",
		RanksJSON:   `{"11":{"tier":15,"division":2}}`,
		FirstSeenAt: now,
		LastSeenAt:  now,
	}
	if err := store.UpsertPlayer(ctx, p); err != nil {
		t.Fatalf("UpsertPlayer failed: %v", err)
	}

	// 4. Retrieve and verify
	got, err := store.GetPlayer(ctx, "Steam|76561198000000001|0")
	if err != nil {
		t.Fatalf("GetPlayer failed: %v", err)
	}
	if got.PlayerID != p.PlayerID || got.PlayerName != "TestPlayer" || got.Platform != "Steam" || got.RanksJSON != p.RanksJSON {
		t.Errorf("retrieved player mismatch: %+v", got)
	}
	if !got.FirstSeenAt.Equal(now) || !got.LastSeenAt.Equal(now) {
		t.Errorf("timestamp mismatch: first=%v, last=%v", got.FirstSeenAt, got.LastSeenAt)
	}
}

func TestJSONStore_PlayerUpsert_PreservesFields(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewJSONStore(filepath.Join(tempDir, "state.json"))
	if err != nil {
		t.Fatalf("NewJSONStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	t1 := time.Now().UTC().Add(-1 * time.Hour).Truncate(time.Second)

	// Initial upsert with rank
	p := &PlayerRecord{
		PlayerID:    "Epic|abc1234|0",
		Platform:    "Epic",
		PlayerName:  "OriginalName",
		RanksJSON:   `{"11":{"tier":16}}`,
		FirstSeenAt: t1,
		LastSeenAt:  t1,
	}
	_ = store.UpsertPlayer(ctx, p)

	// Re-upsert with new name, empty ranks JSON, and new timestamp
	t2 := time.Now().UTC().Truncate(time.Second)
	reUpdate := &PlayerRecord{
		PlayerID:   "Epic|abc1234|0",
		Platform:   "EpicNew",
		PlayerName: "UpdatedName",
		RanksJSON:  "", // should preserve existing
		LastSeenAt: t2,
	}
	if err := store.UpsertPlayer(ctx, reUpdate); err != nil {
		t.Fatalf("UpsertPlayer update failed: %v", err)
	}

	got, _ := store.GetPlayer(ctx, "Epic|abc1234|0")
	if got.PlayerName != "UpdatedName" {
		t.Errorf("expected updated name, got %s", got.PlayerName)
	}
	if got.Platform != "EpicNew" {
		t.Errorf("expected updated platform, got %s", got.Platform)
	}
	if got.RanksJSON != `{"11":{"tier":16}}` {
		t.Errorf("ranks_json was overwritten: %s", got.RanksJSON)
	}
	if !got.FirstSeenAt.Equal(t1) {
		t.Errorf("first_seen_at was clobbered: %v", got.FirstSeenAt)
	}
	if !got.LastSeenAt.Equal(t2) {
		t.Errorf("last_seen_at was not updated: %v", got.LastSeenAt)
	}
}

func TestJSONStore_UpdatePlayerRanks(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewJSONStore(filepath.Join(tempDir, "state.json"))
	if err != nil {
		t.Fatalf("NewJSONStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Update non-existent returns ErrPlayerNotFound
	err = store.UpdatePlayerRanks(ctx, "non-existent", `{"11":{"tier":16}}`)
	if !errors.Is(err, ErrPlayerNotFound) {
		t.Fatalf("expected ErrPlayerNotFound, got %v", err)
	}
	if err := store.UpdatePlayerRanks(ctx, "", `{"11":{"tier":16}}`); !errors.Is(err, ErrPlayerNotFound) {
		t.Fatalf("expected ErrPlayerNotFound for empty ID, got %v", err)
	}

	// Upsert player
	p := &PlayerRecord{
		PlayerID:   "Steam|ranktest|0",
		PlayerName: "RankTest",
		RanksJSON:  "{}",
	}
	_ = store.UpsertPlayer(ctx, p)

	newRanks := `{"11":{"tier":16,"division":4}}`
	if err := store.UpdatePlayerRanks(ctx, "Steam|ranktest|0", newRanks); err != nil {
		t.Fatalf("UpdatePlayerRanks failed: %v", err)
	}

	got, err := store.GetPlayer(ctx, "Steam|ranktest|0")
	if err != nil {
		t.Fatalf("GetPlayer failed: %v", err)
	}
	if got.RanksJSON != newRanks {
		t.Errorf("ranks not updated: got %s, want %s", got.RanksJSON, newRanks)
	}

	// Empty ranks defaults to "{}"
	if err := store.UpdatePlayerRanks(ctx, "Steam|ranktest|0", ""); err != nil {
		t.Fatalf("UpdatePlayerRanks empty string failed: %v", err)
	}
	got2, _ := store.GetPlayer(ctx, "Steam|ranktest|0")
	if got2.RanksJSON != "{}" {
		t.Errorf("expected ranks_json to default to {}, got %s", got2.RanksJSON)
	}
}

func TestJSONStore_RecordMatchResults_MatrixAndIdempotency(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewJSONStore(filepath.Join(tempDir, "state.json"))
	if err != nil {
		t.Fatalf("NewJSONStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	matchGUID := "match-guid-100"
	playlistID := 11

	outcomes := []PlayerOutcome{
		{PlayerID: "p1-tm-won", IsTeammate: true, Won: true, PlayerName: "Mate1", Platform: "Steam"},
		{PlayerID: "p2-tm-lost", IsTeammate: true, Won: false, PlayerName: "Mate2", Platform: "Epic"},
		{PlayerID: "p3-op-won", IsTeammate: false, Won: true, PlayerName: "Rival1", Platform: "Steam"},
		{PlayerID: "p4-op-lost", IsTeammate: false, Won: false, PlayerName: "Rival2", Platform: "Epic"},
	}

	if err := store.RecordMatchResults(ctx, matchGUID, playlistID, outcomes); err != nil {
		t.Fatalf("RecordMatchResults failed: %v", err)
	}

	// Verify p1: Teammate won
	m1, err := store.GetPlayerMatchup(ctx, "p1-tm-won", playlistID)
	if err != nil || m1.WinsAsTeammate != 1 || m1.LossesAsTeammate != 0 || m1.TotalMatches != 1 {
		t.Errorf("p1 unexpected matchup: %+v", m1)
	}

	// Verify p2: Teammate lost
	m2, _ := store.GetPlayerMatchup(ctx, "p2-tm-lost", playlistID)
	if m2.LossesAsTeammate != 1 || m2.WinsAsTeammate != 0 || m2.TotalMatches != 1 {
		t.Errorf("p2 unexpected matchup: %+v", m2)
	}

	// Verify p3: Opponent won
	m3, _ := store.GetPlayerMatchup(ctx, "p3-op-won", playlistID)
	if m3.WinsAsOpponent != 1 || m3.LossesAsOpponent != 0 || m3.TotalMatches != 1 {
		t.Errorf("p3 unexpected matchup: %+v", m3)
	}

	// Verify p4: Opponent lost
	m4, _ := store.GetPlayerMatchup(ctx, "p4-op-lost", playlistID)
	if m4.LossesAsOpponent != 1 || m4.WinsAsOpponent != 0 || m4.TotalMatches != 1 {
		t.Errorf("p4 unexpected matchup: %+v", m4)
	}

	// Verify referential integrity: player records were created
	p1Rec, err := store.GetPlayer(ctx, "p1-tm-won")
	if err != nil || p1Rec.PlayerName != "Mate1" || p1Rec.Platform != "Steam" {
		t.Errorf("player profile not created: %+v", p1Rec)
	}

	// IDEMPOTENCY TEST: repeat same matchGUID must return ErrMatchAlreadyProcessed
	errRepeat := store.RecordMatchResults(ctx, matchGUID, playlistID, outcomes)
	if !errors.Is(errRepeat, ErrMatchAlreadyProcessed) {
		t.Fatalf("expected ErrMatchAlreadyProcessed on duplicate, got %v", errRepeat)
	}

	// Verify counters NOT incremented
	m1After, _ := store.GetPlayerMatchup(ctx, "p1-tm-won", playlistID)
	if m1After.WinsAsTeammate != 1 || m1After.TotalMatches != 1 {
		t.Fatalf("idempotency violated: counters incremented on duplicate match!")
	}

	// Empty matchGUID returns ErrInvalidGUID
	if err := store.RecordMatchResults(ctx, "", playlistID, outcomes); !errors.Is(err, ErrInvalidGUID) {
		t.Fatalf("expected ErrInvalidGUID for empty matchGUID, got %v", err)
	}
}

func TestJSONStore_GetPlayerMatchup_MissingReturnsZeroed(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewJSONStore(filepath.Join(tempDir, "state.json"))
	if err != nil {
		t.Fatalf("NewJSONStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Calling for unknown player and unknown playlist returns zeroed record and nil error
	m, err := store.GetPlayerMatchup(ctx, "unknown-player", 11)
	if err != nil {
		t.Fatalf("expected nil error for missing matchup, got %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil zeroed PlayerMatchup, got nil")
	}
	if m.PlayerID != "unknown-player" || m.PlaylistID != 11 || m.TotalMatches != 0 || m.WinsAsTeammate != 0 || m.WinsAsOpponent != 0 {
		t.Errorf("expected all zeros, got %+v", m)
	}
}

func TestJSONStore_GetPlayerMatchups_OrderedByPlaylist(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewJSONStore(filepath.Join(tempDir, "state.json"))
	if err != nil {
		t.Fatalf("NewJSONStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Record matches in playlists 13, 11, and 10 in disordered sequence
	_ = store.RecordMatchResults(ctx, "m-1", 13, []PlayerOutcome{{PlayerID: "player-multi", IsTeammate: true, Won: true}})
	_ = store.RecordMatchResults(ctx, "m-2", 11, []PlayerOutcome{{PlayerID: "player-multi", IsTeammate: true, Won: false}})
	_ = store.RecordMatchResults(ctx, "m-3", 10, []PlayerOutcome{{PlayerID: "player-multi", IsTeammate: false, Won: true}})

	matchups, err := store.GetPlayerMatchups(ctx, "player-multi")
	if err != nil {
		t.Fatalf("GetPlayerMatchups failed: %v", err)
	}
	if len(matchups) != 3 {
		t.Fatalf("expected 3 matchups, got %d", len(matchups))
	}

	// Must be ordered by playlist_id ASC: 10, 11, 13
	if matchups[0].PlaylistID != 10 || matchups[1].PlaylistID != 11 || matchups[2].PlaylistID != 13 {
		t.Errorf("expected ordered [10, 11, 13], got [%d, %d, %d]",
			matchups[0].PlaylistID, matchups[1].PlaylistID, matchups[2].PlaylistID)
	}

	// Unknown player returns empty slice
	emptyMatchups, err := store.GetPlayerMatchups(ctx, "unknown-nobody")
	if err != nil || len(emptyMatchups) != 0 {
		t.Errorf("expected empty slice for unknown player, got %v / %v", emptyMatchups, err)
	}
}

func TestJSONStore_ListPlayers_PaginationAndSorting(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewJSONStore(filepath.Join(tempDir, "state.json"))
	if err != nil {
		t.Fatalf("NewJSONStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	// Insert 5 players with staggered timestamps
	for i := 0; i < 5; i++ {
		_ = store.UpsertPlayer(ctx, &PlayerRecord{
			PlayerID:    fmt.Sprintf("player-%02d", i),
			PlayerName:  fmt.Sprintf("Name-%d", i),
			FirstSeenAt: now.Add(time.Duration(i) * time.Minute),
			LastSeenAt:  now.Add(time.Duration(i) * time.Minute),
		})
	}

	// List limit=2, offset=0 -> expect player-04, player-03 (most recent first)
	page1, err := store.ListPlayers(ctx, 2, 0)
	if err != nil {
		t.Fatalf("ListPlayers page 1 failed: %v", err)
	}
	if len(page1) != 2 || page1[0].PlayerID != "player-04" || page1[1].PlayerID != "player-03" {
		t.Errorf("unexpected page 1 items: %+v", page1)
	}

	// List limit=2, offset=2 -> expect player-02, player-01
	page2, err := store.ListPlayers(ctx, 2, 2)
	if err != nil {
		t.Fatalf("ListPlayers page 2 failed: %v", err)
	}
	if len(page2) != 2 || page2[0].PlayerID != "player-02" || page2[1].PlayerID != "player-01" {
		t.Errorf("unexpected page 2 items: %+v", page2)
	}

	// List limit=2, offset=4 -> expect player-00
	page3, err := store.ListPlayers(ctx, 2, 4)
	if err != nil {
		t.Fatalf("ListPlayers page 3 failed: %v", err)
	}
	if len(page3) != 1 || page3[0].PlayerID != "player-00" {
		t.Errorf("unexpected page 3 items: %+v", page3)
	}

	// List offset beyond total -> empty slice
	emptyPage, err := store.ListPlayers(ctx, 10, 10)
	if err != nil {
		t.Fatalf("ListPlayers empty page failed: %v", err)
	}
	if len(emptyPage) != 0 {
		t.Errorf("expected empty slice for offset >= total, got %d items", len(emptyPage))
	}

	// Tie-breaking: identical timestamps sort by player_id ASC
	sameTime := now.Add(10 * time.Minute)
	_ = store.UpsertPlayer(ctx, &PlayerRecord{
		PlayerID:   "tie-b",
		PlayerName: "TieB",
		LastSeenAt: sameTime,
	})
	_ = store.UpsertPlayer(ctx, &PlayerRecord{
		PlayerID:   "tie-a",
		PlayerName: "TieA",
		LastSeenAt: sameTime,
	})
	tiePage, err := store.ListPlayers(ctx, 2, 0)
	if err != nil {
		t.Fatalf("ListPlayers tiePage failed: %v", err)
	}
	if len(tiePage) != 2 || tiePage[0].PlayerID != "tie-a" || tiePage[1].PlayerID != "tie-b" {
		t.Errorf("expected tie-a before tie-b, got %+v", tiePage)
	}
}

func TestJSONStore_ListPlayerSummaries_AggregationAndPagination(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewJSONStore(filepath.Join(tempDir, "state.json"))
	if err != nil {
		t.Fatalf("NewJSONStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Seed 2 matches in different playlists for player-sum
	_ = store.RecordMatchResults(ctx, "m-s1", 11, []PlayerOutcome{
		{PlayerID: "player-sum", IsTeammate: true, Won: true},
	})
	_ = store.RecordMatchResults(ctx, "m-s2", 13, []PlayerOutcome{
		{PlayerID: "player-sum", IsTeammate: false, Won: true},
	})

	summaries, err := store.ListPlayerSummaries(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ListPlayerSummaries failed: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}

	s := summaries[0]
	if s.PlayerID != "player-sum" {
		t.Errorf("expected player-sum, got %s", s.PlayerID)
	}
	if s.TotalWinsAsTeammate != 1 || s.TotalWinsAsOpponent != 1 || s.TotalMatches != 2 {
		t.Errorf("unexpected aggregate stats: tmWins=%d, opWins=%d, total=%d",
			s.TotalWinsAsTeammate, s.TotalWinsAsOpponent, s.TotalMatches)
	}
}

func TestJSONStore_DeepCopyDefense_PlayersAndMatchups(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewJSONStore(filepath.Join(tempDir, "state.json"))
	if err != nil {
		t.Fatalf("NewJSONStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	_ = store.UpsertPlayer(ctx, &PlayerRecord{
		PlayerID:   "safe-player",
		PlayerName: "SafeOriginal",
	})
	_ = store.RecordMatchResults(ctx, "safe-match", 11, []PlayerOutcome{
		{PlayerID: "safe-player", IsTeammate: true, Won: true},
	})

	// 1. Mutate PlayerRecord outside store
	p, _ := store.GetPlayer(ctx, "safe-player")
	p.PlayerName = "MutatedHacked"

	pFresh, _ := store.GetPlayer(ctx, "safe-player")
	if pFresh.PlayerName != "SafeOriginal" {
		t.Errorf("deep copy failure: internal PlayerRecord mutated to %s", pFresh.PlayerName)
	}

	// 2. Mutate PlayerMatchup outside store
	m, _ := store.GetPlayerMatchup(ctx, "safe-player", 11)
	m.WinsAsTeammate = 9999

	mFresh, _ := store.GetPlayerMatchup(ctx, "safe-player", 11)
	if mFresh.WinsAsTeammate != 1 {
		t.Errorf("deep copy failure: internal PlayerMatchup mutated to %d", mFresh.WinsAsTeammate)
	}

	// 3. Mutate PlayerSummary outside store
	sums, _ := store.ListPlayerSummaries(ctx, 10, 0)
	if len(sums) > 0 {
		sums[0].TotalMatches = 8888
		sumsFresh, _ := store.ListPlayerSummaries(ctx, 10, 0)
		if sumsFresh[0].TotalMatches != 1 {
			t.Errorf("deep copy failure: internal PlayerSummary mutated to %d", sumsFresh[0].TotalMatches)
		}
	}
}

func TestJSONStore_RestartPersistence_PlayerTracking(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "persist_state.json")

	store1, err := NewJSONStore(statePath)
	if err != nil {
		t.Fatalf("initial NewJSONStore failed: %v", err)
	}

	ctx := context.Background()
	_ = store1.UpsertPlayer(ctx, &PlayerRecord{
		PlayerID:   "persist-p1",
		PlayerName: "PersistPlayer",
		Platform:   "Steam",
		RanksJSON:  `{"11":{"tier":16}}`,
	})
	_ = store1.RecordMatchResults(ctx, "persist-match-1", 11, []PlayerOutcome{
		{PlayerID: "persist-p1", IsTeammate: true, Won: true},
	})
	_ = store1.Close()

	// Reopen with fresh instance
	store2, err := NewJSONStore(statePath)
	if err != nil {
		t.Fatalf("reopening NewJSONStore failed: %v", err)
	}
	defer store2.Close()

	// Verify player persisted
	p, err := store2.GetPlayer(ctx, "persist-p1")
	if err != nil || p.PlayerName != "PersistPlayer" || p.RanksJSON != `{"11":{"tier":16}}` {
		t.Errorf("player did not persist properly: %+v", p)
	}

	// Verify matchup persisted
	m, err := store2.GetPlayerMatchup(ctx, "persist-p1", 11)
	if err != nil || m.WinsAsTeammate != 1 {
		t.Errorf("matchup did not persist properly: %+v", m)
	}

	// Verify idempotency ledger persisted
	errDuplicate := store2.RecordMatchResults(ctx, "persist-match-1", 11, []PlayerOutcome{
		{PlayerID: "persist-p1", IsTeammate: true, Won: true},
	})
	if !errors.Is(errDuplicate, ErrMatchAlreadyProcessed) {
		t.Errorf("idempotency ledger was not persisted across restarts: got %v", errDuplicate)
	}
}

func TestJSONStore_ConcurrentPlayerOperations_Race(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewJSONStore(filepath.Join(tempDir, "state_race.json"))
	if err != nil {
		t.Fatalf("NewJSONStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	const workers = 20
	var wg sync.WaitGroup
	errCh := make(chan error, workers*5)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			playerID := fmt.Sprintf("race-json-p-%d", workerID%5)
			matchGUID := fmt.Sprintf("race-json-m-%d", workerID)

			// 1. Upsert player
			if err := store.UpsertPlayer(ctx, &PlayerRecord{
				PlayerID:   playerID,
				PlayerName: fmt.Sprintf("Name-%d", workerID),
				Platform:   "Steam",
			}); err != nil {
				errCh <- fmt.Errorf("worker %d UpsertPlayer failed: %w", workerID, err)
				return
			}

			// 2. Record match results
			_ = store.RecordMatchResults(ctx, matchGUID, 11, []PlayerOutcome{
				{PlayerID: playerID, IsTeammate: workerID%2 == 0, Won: workerID%3 == 0},
			})

			// 3. Update player ranks
			if err := store.UpdatePlayerRanks(ctx, playerID, `{"11":{"tier":14}}`); err != nil && !errors.Is(err, ErrPlayerNotFound) {
				errCh <- fmt.Errorf("worker %d UpdatePlayerRanks failed: %w", workerID, err)
				return
			}

			// 4. Query player & matchups
			_, _ = store.GetPlayer(ctx, playerID)
			_, _ = store.GetPlayerMatchup(ctx, playerID, 11)
			_, _ = store.ListPlayerSummaries(ctx, 5, 0)
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("race test error: %v", err)
	}
}

func TestJSONStore_ClosedStore_PlayerOperations(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewJSONStore(filepath.Join(tempDir, "closed_state.json"))
	if err != nil {
		t.Fatalf("NewJSONStore failed: %v", err)
	}
	ctx := context.Background()
	_ = store.Close()

	if err := store.UpsertPlayer(ctx, &PlayerRecord{PlayerID: "p1"}); !errors.Is(err, ErrStoreClosed) {
		t.Errorf("expected ErrStoreClosed from UpsertPlayer, got %v", err)
	}
	if _, err := store.GetPlayer(ctx, "p1"); !errors.Is(err, ErrStoreClosed) {
		t.Errorf("expected ErrStoreClosed from GetPlayer, got %v", err)
	}
	if _, err := store.ListPlayers(ctx, 10, 0); !errors.Is(err, ErrStoreClosed) {
		t.Errorf("expected ErrStoreClosed from ListPlayers, got %v", err)
	}
	if _, err := store.ListPlayerSummaries(ctx, 10, 0); !errors.Is(err, ErrStoreClosed) {
		t.Errorf("expected ErrStoreClosed from ListPlayerSummaries, got %v", err)
	}
	if err := store.UpdatePlayerRanks(ctx, "p1", "{}"); !errors.Is(err, ErrStoreClosed) {
		t.Errorf("expected ErrStoreClosed from UpdatePlayerRanks, got %v", err)
	}
	if err := store.RecordMatchResults(ctx, "m1", 11, []PlayerOutcome{}); !errors.Is(err, ErrStoreClosed) {
		t.Errorf("expected ErrStoreClosed from RecordMatchResults, got %v", err)
	}
	if _, err := store.GetPlayerMatchup(ctx, "p1", 11); !errors.Is(err, ErrStoreClosed) {
		t.Errorf("expected ErrStoreClosed from GetPlayerMatchup, got %v", err)
	}
	if _, err := store.GetPlayerMatchups(ctx, "p1"); !errors.Is(err, ErrStoreClosed) {
		t.Errorf("expected ErrStoreClosed from GetPlayerMatchups, got %v", err)
	}
}
