package syncer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dank/rl-api-utils/internal/storage"
)

func TestSyncer_ConstructorValidation(t *testing.T) {
	store := newMockStore()
	provider := newMockHistoryProvider()
	downloader := newMockDownloader()
	uploader := newMockUploader()

	if _, err := New(nil, provider, downloader, uploader); !errors.Is(err, ErrNilStore) {
		t.Fatalf("expected ErrNilStore, got %v", err)
	}
	if _, err := New(store, nil, downloader, uploader); !errors.Is(err, ErrNilProvider) {
		t.Fatalf("expected ErrNilProvider, got %v", err)
	}
	if _, err := New(store, provider, nil, uploader); !errors.Is(err, ErrNilDownloader) {
		t.Fatalf("expected ErrNilDownloader, got %v", err)
	}
	if _, err := New(store, provider, downloader, nil); !errors.Is(err, ErrNilUploader) {
		t.Fatalf("expected ErrNilUploader, got %v", err)
	}

	s, err := New(store, provider, downloader, uploader,
		WithReplayDir("/tmp/replays"),
		WithDryRun(true),
		WithKeepLocalFiles(false),
	)
	if err != nil {
		t.Fatalf("unexpected New error: %v", err)
	}
	if s.replayDir != "/tmp/replays" || !s.dryRun || s.keepLocalFiles {
		t.Fatalf("options not applied correctly: %+v", s)
	}

	s2, err := NewWithConfig(store, provider, downloader, uploader, Config{
		ReplayDir:      "/tmp/cfg-replays",
		DryRun:         true,
		KeepLocalFiles: false,
	})
	if err != nil || s2.replayDir != "/tmp/cfg-replays" {
		t.Fatalf("NewWithConfig failed: %v", err)
	}

	s3 := NewSyncerEngine(store, provider, downloader, uploader, "/tmp/engine-replays", true)
	if s3 == nil || s3.replayDir != "/tmp/engine-replays" || !s3.dryRun {
		t.Fatalf("NewSyncerEngine failed")
	}
}

func TestSyncer_HappyPath_FullPipeline(t *testing.T) {
	store := newMockStore()
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m1", ReplayURL: "https://cdn.example.com/m1.replay", MapName: "DFHStadium"},
		DiscoveredMatch{MatchGUID: "m2", ReplayURL: "https://cdn.example.com/m2.replay", MapName: "Mannfield"},
	)
	downloader := newMockDownloader()
	uploader := newMockUploader()

	s, err := New(store, provider, downloader, uploader, WithReplayDir("/tmp/test-replays"))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	stats, err := s.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if stats.DiscoveredCount != 2 || stats.DownloadedCount != 2 || stats.UploadedCount != 2 || stats.FailedCount != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if stats.Discovered != 2 || stats.Downloaded != 2 || stats.Uploaded != 2 {
		t.Fatalf("alias stats mismatch: %+v", stats)
	}

	rec1, _ := store.GetMatch(context.Background(), "m1")
	if rec1.DownloadStatus != storage.DownloadDownloaded || rec1.UploadStatus != storage.UploadUploaded {
		t.Fatalf("m1 status incorrect: %+v", rec1)
	}
}

func TestSyncer_MultiCycle_Progression(t *testing.T) {
	store := newMockStore()
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m1", ReplayURL: "https://cdn.example.com/m1.replay"},
	)
	downloader := newMockDownloader()
	uploader := newMockUploader()

	s, _ := New(store, provider, downloader, uploader)

	stats1, err := s.RunCycle(context.Background())
	if err != nil || stats1.DownloadedCount != 1 || stats1.UploadedCount != 1 {
		t.Fatalf("cycle 1 failed: %v, stats: %+v", err, stats1)
	}

	provider.SetMatches([]DiscoveredMatch{
		{MatchGUID: "m1", ReplayURL: "https://cdn.example.com/m1.replay"},
		{MatchGUID: "m2", ReplayURL: "https://cdn.example.com/m2.replay"},
	})

	stats2, err := s.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("cycle 2 failed: %v", err)
	}
	if stats2.DiscoveredCount != 2 {
		t.Fatalf("expected 2 discovered in cycle 2, got %d", stats2.DiscoveredCount)
	}
	if stats2.DownloadedCount != 1 || stats2.UploadedCount != 1 {
		t.Fatalf("only new match m2 should be processed in cycle 2: %+v", stats2)
	}
}

func TestSyncer_DelayedReplayURL_TwoCycles(t *testing.T) {
	store := newMockStore()
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m-delayed", ReplayURL: ""},
	)
	downloader := newMockDownloader()
	uploader := newMockUploader()

	s, _ := New(store, provider, downloader, uploader)

	stats1, err := s.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("cycle 1 failed: %v", err)
	}
	if stats1.SkippedCount != 1 || stats1.DownloadedCount != 0 {
		t.Fatalf("expected 1 skipped, 0 downloaded in cycle 1: %+v", stats1)
	}

	rec, _ := store.GetMatch(context.Background(), "m-delayed")
	if rec.DownloadStatus != storage.DownloadSkipped {
		t.Fatalf("expected SKIPPED status, got %s", rec.DownloadStatus)
	}

	provider.SetMatches([]DiscoveredMatch{
		{MatchGUID: "m-delayed", ReplayURL: "https://cdn.example.com/m-delayed.replay"},
	})

	stats2, err := s.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("cycle 2 failed: %v", err)
	}
	if stats2.DownloadedCount != 1 || stats2.UploadedCount != 1 {
		t.Fatalf("delayed match should be downloaded and uploaded in cycle 2: %+v", stats2)
	}
}

func TestSyncer_DelayedReplayURL_WhitespaceNormalized(t *testing.T) {
	store := newMockStore()
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m-ws", ReplayURL: "   \t  "},
	)
	downloader := newMockDownloader()
	uploader := newMockUploader()

	s, _ := New(store, provider, downloader, uploader)

	stats1, err := s.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("cycle 1 failed: %v", err)
	}
	if stats1.SkippedCount != 1 || stats1.DownloadedCount != 0 {
		t.Fatalf("expected 1 skipped in cycle 1, got %+v", stats1)
	}
	recBefore, _ := store.GetMatch(context.Background(), "m-ws")
	if recBefore.ReplayURL != "" {
		t.Fatalf("expected whitespace ReplayURL normalized to empty string, got %q", recBefore.ReplayURL)
	}

	provider.SetMatches([]DiscoveredMatch{
		{MatchGUID: "m-ws", ReplayURL: "  https://cdn.example.com/m-ws.replay  "},
	})

	stats2, err := s.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("cycle 2 failed: %v", err)
	}
	if stats2.DownloadedCount != 1 || stats2.UploadedCount != 1 {
		t.Fatalf("expected trimmed URL promotion on cycle 2, got %+v", stats2)
	}
	recAfter, _ := store.GetMatch(context.Background(), "m-ws")
	if recAfter.ReplayURL != "https://cdn.example.com/m-ws.replay" {
		t.Fatalf("expected normalized URL saved, got %q", recAfter.ReplayURL)
	}
}

func TestSyncer_DuplicateHandling_HTTP409(t *testing.T) {
	store := newMockStore()
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m-dup", ReplayURL: "https://cdn.example.com/m-dup.replay"},
	)
	downloader := newMockDownloader()
	uploader := newMockUploader()
	uploader.duplicates["m-dup"] = true

	s, _ := New(store, provider, downloader, uploader)

	stats, err := s.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}
	if stats.DuplicateCount != 1 || stats.UploadedCount != 0 || stats.FailedCount != 0 {
		t.Fatalf("expected 1 duplicate, 0 uploaded, 0 failed: %+v", stats)
	}

	rec, _ := store.GetMatch(context.Background(), "m-dup")
	if rec.UploadStatus != storage.UploadDuplicate {
		t.Fatalf("expected DUPLICATE upload status, got %s", rec.UploadStatus)
	}
}

func TestSyncer_DryRun_NoNetworkOrDBMutations(t *testing.T) {
	store := newMockStore()
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m-dry", ReplayURL: "https://cdn.example.com/m-dry.replay"},
	)
	downloader := newMockDownloader()
	uploader := newMockUploader()

	s, _ := New(store, provider, downloader, uploader, WithDryRun(true))

	stats, err := s.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("dry run failed: %v", err)
	}
	if stats.DiscoveredCount != 1 || stats.DownloadedCount != 0 || stats.UploadedCount != 0 {
		t.Fatalf("dry run stats mismatch: %+v", stats)
	}
	if downloader.calls != 0 || uploader.calls != 0 {
		t.Fatalf("downloader or uploader called in dry run: dl=%d, up=%d", downloader.calls, uploader.calls)
	}

	if _, err = store.GetMatch(context.Background(), "m-dry"); !errors.Is(err, storage.ErrMatchNotFound) {
		t.Fatalf("dry run must not insert match into store, got %v", err)
	}
}

func TestSyncer_ProviderError_AbortsCycle(t *testing.T) {
	store := newMockStore()
	provider := newMockHistoryProvider()
	provider.err = errors.New("psynet rpc connection failed")
	s, _ := New(store, provider, newMockDownloader(), newMockUploader())

	_, err := s.RunCycle(context.Background())
	if err == nil || !strings.Contains(err.Error(), "polling matches failed") {
		t.Fatalf("expected polling error, got: %v", err)
	}
}

func TestSyncer_KeepLocalFilesFalse(t *testing.T) {
	store := newMockStore()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "m-rm.replay")
	if err := os.WriteFile(filePath, []byte("test-data"), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m-rm", ReplayURL: "https://cdn.example.com/rm.replay"},
	)
	downloader := newMockDownloader()
	downloader.downloaded["m-rm"] = filePath

	s, err := New(store, provider, downloader, newMockUploader(),
		WithReplayDir(tmpDir),
		WithKeepLocalFiles(false),
	)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	stats, err := s.RunCycle(context.Background())
	if err != nil || stats.UploadedCount != 1 {
		t.Fatalf("expected 1 upload, got err=%v stats=%+v", err, stats)
	}

	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("expected file %s to be deleted when KeepLocalFiles=false, stat err: %v", filePath, err)
	}
}

func TestSyncer_StoreErrors(t *testing.T) {
	store := newMockStore()
	store.failRecover = true
	provider := newMockHistoryProvider()
	s, _ := New(store, provider, newMockDownloader(), newMockUploader())

	_, err := s.RunCycle(context.Background())
	if err == nil || !strings.Contains(err.Error(), "recovery failed") {
		t.Fatalf("expected recovery failure, got: %v", err)
	}

	store.failRecover = false
	store.failUpsert = true
	provider.SetMatches([]DiscoveredMatch{{MatchGUID: "m1", ReplayURL: "https://example.com/1"}})

	_, err = s.RunCycle(context.Background())
	if err == nil || !strings.Contains(err.Error(), "upserting discovered matches failed") {
		t.Fatalf("expected upsert failure, got: %v", err)
	}

	store.failUpsert = false
	store.failListDl = true
	_, err = s.RunCycle(context.Background())
	if err == nil || !strings.Contains(err.Error(), "list pending downloads failed") {
		t.Fatalf("expected list downloads failure, got: %v", err)
	}

	store.failListDl = false
	store.failListUp = true
	_, err = s.RunCycle(context.Background())
	if err == nil || !strings.Contains(err.Error(), "list pending uploads failed") {
		t.Fatalf("expected list uploads failure, got: %v", err)
	}
}
