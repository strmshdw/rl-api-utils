package storage_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"rl-api-utils/internal/storage"
)

func newTestStore(t *testing.T) storage.StateStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create test sqlite store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

func TestSQLiteStore_SchemaInitialization(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "reopen_test.db")

	store1, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to open store1: %v", err)
	}
	_ = store1.Close()

	// Reopening existing file must succeed idempotently
	store2, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to reopen store2: %v", err)
	}
	defer store2.Close()

	// Querying non-existent match returns ErrMatchNotFound
	_, err = store2.GetMatch(ctx, "non-existent-guid")
	if !errors.Is(err, storage.ErrMatchNotFound) {
		t.Fatalf("expected ErrMatchNotFound, got %v", err)
	}
}

func TestSQLiteStore_CRUDAndIdempotency(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	now := time.Now().UTC().Truncate(time.Second)
	matches := []*storage.MatchRecord{
		{
			MatchGUID:            "guid-001",
			RecordStartTimestamp: now.Unix(),
			MapName:              "Park_P",
			Playlist:             11,
			ReplayURL:            "https://cdn.example.com/replays/guid-001.replay",
			DownloadStatus:       storage.DownloadPending,
			UploadStatus:         storage.UploadPending,
			CreatedAt:            now,
			UpdatedAt:            now,
		},
		{
			MatchGUID:            "guid-002",
			RecordStartTimestamp: now.Unix() + 100,
			MapName:              "Stadium_P",
			Playlist:             13,
			ReplayURL:            "", // No replay URL available
			DownloadStatus:       storage.DownloadSkipped,
			UploadStatus:         storage.UploadPending,
			CreatedAt:            now,
			UpdatedAt:            now,
		},
	}

	if err := store.UpsertDiscoveredMatches(ctx, matches); err != nil {
		t.Fatalf("failed to upsert discovered matches: %v", err)
	}

	// Verify GetMatch on guid-001
	rec1, err := store.GetMatch(ctx, "guid-001")
	if err != nil {
		t.Fatalf("failed to get match guid-001: %v", err)
	}
	if rec1.MatchGUID != "guid-001" || rec1.MapName != "Park_P" || rec1.Playlist != 11 {
		t.Errorf("unexpected match fields: %+v", rec1)
	}
	if rec1.DownloadStatus != storage.DownloadPending {
		t.Errorf("expected DownloadPending, got %s", rec1.DownloadStatus)
	}
	if rec1.DownloadedAt != nil {
		t.Errorf("expected DownloadedAt to be nil, got %v", rec1.DownloadedAt)
	}

	// Verify ListPendingDownloads returns only guid-001 (guid-002 has empty replay_url and skipped status)
	pendingDL, err := store.ListPendingDownloads(ctx)
	if err != nil {
		t.Fatalf("failed to list pending downloads: %v", err)
	}
	if len(pendingDL) != 1 {
		t.Fatalf("expected 1 pending download, got %d", len(pendingDL))
	}
	if pendingDL[0].MatchGUID != "guid-001" {
		t.Errorf("expected guid-001, got %s", pendingDL[0].MatchGUID)
	}

	// Re-upserting must preserve existing state
	matches[0].ReplayURL = "https://cdn.example.com/updated.replay"
	if err := store.UpsertDiscoveredMatches(ctx, matches); err != nil {
		t.Fatalf("failed to re-upsert matches: %v", err)
	}

	recAfterReupsert, err := store.GetMatch(ctx, "guid-001")
	if err != nil {
		t.Fatalf("failed to get match after re-upsert: %v", err)
	}
	if recAfterReupsert.DownloadStatus != storage.DownloadPending {
		t.Errorf("download status clobbered: %s", recAfterReupsert.DownloadStatus)
	}
}

func TestSQLiteStore_DownloadTransitions(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	now := time.Now().UTC().Truncate(time.Second)
	m := &storage.MatchRecord{
		MatchGUID:            "guid-dl-test",
		RecordStartTimestamp: now.Unix(),
		MapName:              "Utopia_P",
		Playlist:             11,
		ReplayURL:            "https://cdn.example.com/guid-dl-test.replay",
		DownloadStatus:       storage.DownloadPending,
		UploadStatus:         storage.UploadPending,
		CreatedAt:            now,
		UpdatedAt:            now,
	}

	if err := store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{m}); err != nil {
		t.Fatalf("failed to upsert match: %v", err)
	}

	// 1. MarkDownloading
	if err := store.MarkDownloading(ctx, "guid-dl-test"); err != nil {
		t.Fatalf("failed to mark downloading: %v", err)
	}
	rec, _ := store.GetMatch(ctx, "guid-dl-test")
	if rec.DownloadStatus != storage.DownloadDownloading {
		t.Fatalf("expected DOWNLOADING, got %s", rec.DownloadStatus)
	}

	// 2. MarkDownloaded
	localPath := "/replays/guid-dl-test.replay"
	if err := store.MarkDownloaded(ctx, "guid-dl-test", localPath); err != nil {
		t.Fatalf("failed to mark downloaded: %v", err)
	}
	rec, _ = store.GetMatch(ctx, "guid-dl-test")
	if rec.DownloadStatus != storage.DownloadDownloaded {
		t.Fatalf("expected DOWNLOADED, got %s", rec.DownloadStatus)
	}
	if rec.LocalFilePath != localPath {
		t.Errorf("expected local path %s, got %s", localPath, rec.LocalFilePath)
	}
	if rec.DownloadedAt == nil {
		t.Errorf("expected downloaded_at to be non-nil")
	}

	// 3. MarkDownloadFailed on non-existent returns error
	if err := store.MarkDownloadFailed(ctx, "guid-fake", "network timeout"); !errors.Is(err, storage.ErrMatchNotFound) {
		t.Errorf("expected ErrMatchNotFound, got %v", err)
	}

	// 4. MarkDownloadFailed on real match
	if err := store.MarkDownloadFailed(ctx, "guid-dl-test", "hash mismatch"); err != nil {
		t.Fatalf("failed to mark download failed: %v", err)
	}
	rec, _ = store.GetMatch(ctx, "guid-dl-test")
	if rec.DownloadStatus != storage.DownloadFailed {
		t.Fatalf("expected FAILED, got %s", rec.DownloadStatus)
	}
	if rec.RetryCount != 1 || rec.LastError != "hash mismatch" {
		t.Errorf("unexpected retry count %d or last error %s", rec.RetryCount, rec.LastError)
	}
}

func TestSQLiteStore_UploadTransitionsAndDuplicate(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	now := time.Now().UTC().Truncate(time.Second)
	m := &storage.MatchRecord{
		MatchGUID:            "guid-up-test",
		RecordStartTimestamp: now.Unix(),
		MapName:              "DFH_P",
		Playlist:             11,
		ReplayURL:            "https://cdn.example.com/guid-up-test.replay",
		DownloadStatus:       storage.DownloadDownloaded,
		LocalFilePath:        "/replays/guid-up-test.replay",
		UploadStatus:         storage.UploadPending,
		CreatedAt:            now,
		UpdatedAt:            now,
	}

	if err := store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{m}); err != nil {
		t.Fatalf("failed to upsert match: %v", err)
	}

	// Verify ListPendingUploads includes this downloaded match
	pendingUps, err := store.ListPendingUploads(ctx)
	if err != nil {
		t.Fatalf("failed to list pending uploads: %v", err)
	}
	if len(pendingUps) != 1 || pendingUps[0].MatchGUID != "guid-up-test" {
		t.Fatalf("expected 1 pending upload for guid-up-test, got %+v", pendingUps)
	}

	// 1. MarkUploading
	if err := store.MarkUploading(ctx, "guid-up-test"); err != nil {
		t.Fatalf("failed to mark uploading: %v", err)
	}
	rec, _ := store.GetMatch(ctx, "guid-up-test")
	if rec.UploadStatus != storage.UploadUploading {
		t.Fatalf("expected UPLOADING, got %s", rec.UploadStatus)
	}

	// 2. MarkDuplicate (HTTP 409 Conflict)
	ballchasingID := "bc-dup-uuid-123"
	ballchasingURL := "https://ballchasing.com/replay/bc-dup-uuid-123"
	if err := store.MarkDuplicate(ctx, "guid-up-test", ballchasingID, ballchasingURL); err != nil {
		t.Fatalf("failed to mark duplicate: %v", err)
	}

	rec, _ = store.GetMatch(ctx, "guid-up-test")
	if rec.UploadStatus != storage.UploadDuplicate {
		t.Fatalf("expected DUPLICATE, got %s", rec.UploadStatus)
	}
	if rec.BallchasingID != ballchasingID || rec.BallchasingURL != ballchasingURL {
		t.Errorf("ballchasing metadata mismatch: %s / %s", rec.BallchasingID, rec.BallchasingURL)
	}

	// Pending uploads should now be empty
	pendingUps, err = store.ListPendingUploads(ctx)
	if err != nil {
		t.Fatalf("failed to list pending uploads: %v", err)
	}
	if len(pendingUps) != 0 {
		t.Errorf("expected 0 pending uploads after duplicate mark, got %d", len(pendingUps))
	}

	// Re-upserting must NOT reset DUPLICATE status back to PENDING
	if err := store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{m}); err != nil {
		t.Fatalf("failed to re-upsert: %v", err)
	}
	recAfter, _ := store.GetMatch(ctx, "guid-up-test")
	if recAfter.UploadStatus != storage.UploadDuplicate {
		t.Errorf("DUPLICATE status was clobbered by re-upsert: %s", recAfter.UploadStatus)
	}
}

func TestSQLiteStore_CrashRecovery(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	now := time.Now().UTC().Truncate(time.Second)
	matches := []*storage.MatchRecord{
		{
			MatchGUID:            "crash-1",
			RecordStartTimestamp: now.Unix(),
			DownloadStatus:       storage.DownloadDownloading, // In-flight download
			UploadStatus:         storage.UploadPending,
			CreatedAt:            now,
			UpdatedAt:            now,
		},
		{
			MatchGUID:            "crash-2",
			RecordStartTimestamp: now.Unix() + 10,
			DownloadStatus:       storage.DownloadDownloaded,
			LocalFilePath:        "/replays/crash-2.replay",
			UploadStatus:         storage.UploadUploading, // In-flight upload
			CreatedAt:            now,
			UpdatedAt:            now,
		},
		{
			MatchGUID:            "done-3",
			RecordStartTimestamp: now.Unix() + 20,
			DownloadStatus:       storage.DownloadDownloaded,
			LocalFilePath:        "/replays/done-3.replay",
			UploadStatus:         storage.UploadUploaded,
			CreatedAt:            now,
			UpdatedAt:            now,
		},
	}

	if err := store.UpsertDiscoveredMatches(ctx, matches); err != nil {
		t.Fatalf("failed to seed crash matches: %v", err)
	}

	// Execute RecoverInFlight
	if err := store.RecoverInFlight(ctx); err != nil {
		t.Fatalf("failed to recover in-flight states: %v", err)
	}

	// Check crash-1: DOWNLOADING -> PENDING
	r1, _ := store.GetMatch(ctx, "crash-1")
	if r1.DownloadStatus != storage.DownloadPending {
		t.Errorf("expected crash-1 download status PENDING, got %s", r1.DownloadStatus)
	}

	// Check crash-2: UPLOADING -> PENDING
	r2, _ := store.GetMatch(ctx, "crash-2")
	if r2.UploadStatus != storage.UploadPending {
		t.Errorf("expected crash-2 upload status PENDING, got %s", r2.UploadStatus)
	}
	if r2.DownloadStatus != storage.DownloadDownloaded {
		t.Errorf("crash-2 download status altered: %s", r2.DownloadStatus)
	}

	// Check done-3: UPLOADED unchanged
	r3, _ := store.GetMatch(ctx, "done-3")
	if r3.UploadStatus != storage.UploadUploaded {
		t.Errorf("done-3 upload status altered: %s", r3.UploadStatus)
	}
}

func TestSQLiteStore_AuthState(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	// Provider not found
	_, _, _, err := store.GetAuthState(ctx, "epic")
	if !errors.Is(err, storage.ErrAuthStateNotFound) {
		t.Fatalf("expected ErrAuthStateNotFound, got %v", err)
	}

	// Save epic auth state
	err = store.SaveAuthState(ctx, "epic", "token_v1", "epic_acc_123", "RocketChamp")
	if err != nil {
		t.Fatalf("failed to save auth state: %v", err)
	}

	token, accID, name, err := store.GetAuthState(ctx, "epic")
	if err != nil {
		t.Fatalf("failed to get auth state: %v", err)
	}
	if token != "token_v1" || accID != "epic_acc_123" || name != "RocketChamp" {
		t.Errorf("unexpected auth state: %s, %s, %s", token, accID, name)
	}

	// Update epic auth state (new refresh token)
	err = store.SaveAuthState(ctx, "epic", "token_v2", "epic_acc_123", "RocketChampPro")
	if err != nil {
		t.Fatalf("failed to update auth state: %v", err)
	}

	token, accID, name, err = store.GetAuthState(ctx, "epic")
	if err != nil {
		t.Fatalf("failed to get updated auth state: %v", err)
	}
	if token != "token_v2" || name != "RocketChampPro" {
		t.Errorf("auth update not reflected: token=%s, name=%s", token, name)
	}
}

func TestSQLiteStore_Concurrency(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	// Seed some initial matches
	now := time.Now().UTC().Unix()
	var initial []*storage.MatchRecord
	for i := 0; i < 20; i++ {
		initial = append(initial, &storage.MatchRecord{
			MatchGUID:            fmt.Sprintf("conc-seed-%02d", i),
			RecordStartTimestamp: now + int64(i),
			ReplayURL:            fmt.Sprintf("https://cdn.example.com/%02d.replay", i),
			DownloadStatus:       storage.DownloadPending,
			UploadStatus:         storage.UploadPending,
		})
	}
	if err := store.UpsertDiscoveredMatches(ctx, initial); err != nil {
		t.Fatalf("failed to seed concurrent records: %v", err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	// Run 20 concurrent goroutines performing mixed reads, transitions, and upserts
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			guid := fmt.Sprintf("conc-seed-%02d", workerID)

			// 1. Read
			_, err := store.GetMatch(ctx, guid)
			if err != nil {
				errCh <- fmt.Errorf("worker %d GetMatch failed: %w", workerID, err)
				return
			}

			// 2. Mark downloading
			if err := store.MarkDownloading(ctx, guid); err != nil {
				errCh <- fmt.Errorf("worker %d MarkDownloading failed: %w", workerID, err)
				return
			}

			// 3. Mark downloaded
			if err := store.MarkDownloaded(ctx, guid, fmt.Sprintf("/replays/%s.replay", guid)); err != nil {
				errCh <- fmt.Errorf("worker %d MarkDownloaded failed: %w", workerID, err)
				return
			}

			// 4. Mark uploading
			if err := store.MarkUploading(ctx, guid); err != nil {
				errCh <- fmt.Errorf("worker %d MarkUploading failed: %w", workerID, err)
				return
			}

			// 5. Mark uploaded
			if err := store.MarkUploaded(ctx, guid, fmt.Sprintf("bc-%d", workerID), "https://ballchasing.com"); err != nil {
				errCh <- fmt.Errorf("worker %d MarkUploaded failed: %w", workerID, err)
				return
			}

			// 6. List pending
			_, _ = store.ListPendingDownloads(ctx)
			_, _ = store.ListPendingUploads(ctx)
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrency error: %v", err)
	}
}
