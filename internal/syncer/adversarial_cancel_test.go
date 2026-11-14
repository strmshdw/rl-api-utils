package syncer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/storage"
)

func TestAdversarial_ContextCancellation_MidDownload_NoPoisonAndCleanResume(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_cancel_dl.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockDl := newMockDownloader()
	downloader := &selectiveBlockingDownloader{
		inner:        mockDl,
		blockForGUID: "m-slow",
		onBlock:      cancel,
	}

	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m-fast", RecordStartTimestamp: 100, ReplayURL: "https://cdn.example.com/fast.replay"},
		DiscoveredMatch{MatchGUID: "m-slow", RecordStartTimestamp: 200, ReplayURL: "https://cdn.example.com/slow.replay"},
	)

	syncerEngine := NewSyncerEngine(store, provider, downloader, newMockUploader(), t.TempDir(), false)

	start := time.Now()
	stats, err := syncerEngine.RunCycle(ctx)
	duration := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if duration > 1*time.Second {
		t.Fatalf("cancellation took too long to exit: %v", duration)
	}

	recSlow, _ := store.GetMatch(context.Background(), "m-slow")
	if recSlow.DownloadStatus != storage.DownloadDownloading {
		t.Fatalf("match should remain DOWNLOADING at point of cancellation, got %s", recSlow.DownloadStatus)
	}
	recFast, _ := store.GetMatch(context.Background(), "m-fast")
	if recFast.DownloadStatus != storage.DownloadDownloaded {
		t.Fatalf("m-fast should be DOWNLOADED, got %s", recFast.DownloadStatus)
	}

	downloader.blockForGUID = ""
	freshCtx := context.Background()
	statsResume, err := syncerEngine.RunCycle(freshCtx)
	if err != nil {
		t.Fatalf("resume cycle failed: %v", err)
	}

	recSlowAfter, _ := store.GetMatch(freshCtx, "m-slow")
	if recSlowAfter.DownloadStatus != storage.DownloadDownloaded || recSlowAfter.UploadStatus != storage.UploadUploaded {
		t.Fatalf("m-slow failed to cleanly resume: dl=%s, up=%s", recSlowAfter.DownloadStatus, recSlowAfter.UploadStatus)
	}
	if statsResume.FailedCount != 0 {
		t.Fatalf("expected 0 failures on resume, got %d", statsResume.FailedCount)
	}
	_ = stats
}

func TestAdversarial_ContextCancellation_MidUpload_NoPoisonAndCleanResume(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_cancel_up.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	uploader := &selectiveBlockingUploader{
		inner:        newMockUploader(),
		blockForGUID: "m-up-cancel",
		onBlock:      cancel,
	}

	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m-up-cancel", ReplayURL: "https://cdn.example.com/up.replay"},
	)

	syncerEngine := NewSyncerEngine(store, provider, newMockDownloader(), uploader, t.TempDir(), false)

	start := time.Now()
	_, err = syncerEngine.RunCycle(ctx)
	duration := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if duration > 1*time.Second {
		t.Fatalf("cancellation took too long to exit: %v", duration)
	}

	rec, _ := store.GetMatch(context.Background(), "m-up-cancel")
	if rec.UploadStatus != storage.UploadUploading {
		t.Fatalf("match should remain UPLOADING at point of cancellation, got %s", rec.UploadStatus)
	}

	uploader.blockForGUID = ""
	freshCtx := context.Background()
	statsResume, err := syncerEngine.RunCycle(freshCtx)
	if err != nil {
		t.Fatalf("resume cycle failed: %v", err)
	}

	recAfter, _ := store.GetMatch(freshCtx, "m-up-cancel")
	if recAfter.UploadStatus != storage.UploadUploaded {
		t.Fatalf("match failed to recover and upload: %s", recAfter.UploadStatus)
	}
	if statsResume.UploadedCount != 1 || statsResume.FailedCount != 0 {
		t.Fatalf("unexpected resume stats: up=%d, fail=%d", statsResume.UploadedCount, statsResume.FailedCount)
	}
}

func TestAdversarial_MalformedMatches_Resilience(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_malformed.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "", ReplayURL: "https://cdn.example.com/empty.replay"},
		DiscoveredMatch{MatchGUID: "   \t\n  ", ReplayURL: "https://cdn.example.com/ws.replay"},
		DiscoveredMatch{MatchGUID: "valid-1", MapName: "Map'DROP TABLE matches;--", ReplayURL: "https://cdn.example.com/sql.replay"},
		DiscoveredMatch{MatchGUID: "valid-2", MapName: "Unicode_🏟️_日本語", ReplayURL: "https://cdn.example.com/uni.replay"},
	)

	syncerEngine := NewSyncerEngine(store, provider, newMockDownloader(), newMockUploader(), t.TempDir(), false)
	stats, err := syncerEngine.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle failed on malformed inputs: %v", err)
	}
	if stats.DownloadedCount != 2 || stats.UploadedCount != 2 {
		t.Fatalf("expected exactly 2 valid matches processed, got dl=%d, up=%d", stats.DownloadedCount, stats.UploadedCount)
	}

	rec1, err := store.GetMatch(ctx, "valid-1")
	if err != nil || rec1.MapName != "Map'DROP TABLE matches;--" {
		t.Fatalf("SQL injection string failed to store safely: %v, rec: %+v", err, rec1)
	}
	rec2, err := store.GetMatch(ctx, "valid-2")
	if err != nil || rec2.MapName != "Unicode_🏟️_日本語" {
		t.Fatalf("Unicode map name failed to store safely: %v, rec: %+v", err, rec2)
	}
}

func TestAdversarial_KeepLocalFilesFalse_CleanupOnSuccessAndDuplicate(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_cleanup.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	replayDir := t.TempDir()

	fileSuccess := filepath.Join(replayDir, "success.replay")
	if err := os.WriteFile(fileSuccess, []byte("success-data"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	fileDup := filepath.Join(replayDir, "dup.replay")
	if err := os.WriteFile(fileDup, []byte("dup-data"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "success", ReplayURL: "https://cdn.example.com/s.replay"},
		DiscoveredMatch{MatchGUID: "dup", ReplayURL: "https://cdn.example.com/d.replay"},
	)
	downloader := newMockDownloader()
	downloader.downloaded["success"] = fileSuccess
	downloader.downloaded["dup"] = fileDup

	uploader := newMockUploader()
	uploader.duplicates["dup"] = true

	s, err := New(store, provider, downloader, uploader,
		WithReplayDir(replayDir),
		WithKeepLocalFiles(false),
	)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	stats, err := s.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}
	if stats.UploadedCount != 1 || stats.DuplicateCount != 1 {
		t.Fatalf("expected 1 uploaded, 1 duplicate: %+v", stats)
	}

	if _, err := os.Stat(fileSuccess); !os.IsNotExist(err) {
		t.Fatalf("fileSuccess was not deleted: %v", err)
	}
	if _, err := os.Stat(fileDup); !os.IsNotExist(err) {
		t.Fatalf("fileDup was not deleted: %v", err)
	}
}
