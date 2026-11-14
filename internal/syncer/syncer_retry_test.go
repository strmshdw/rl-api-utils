package syncer

import (
	"context"
	"errors"
	"testing"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/storage"
)

func TestSyncer_CrashRecovery_ResetsInFlight(t *testing.T) {
	store := newMockStore()
	store.matches["m-crash-dl"] = &storage.MatchRecord{
		MatchGUID:      "m-crash-dl",
		ReplayURL:      "https://cdn.example.com/crash-dl.replay",
		DownloadStatus: storage.DownloadDownloading,
		UploadStatus:   storage.UploadPending,
	}
	store.matches["m-crash-up"] = &storage.MatchRecord{
		MatchGUID:      "m-crash-up",
		ReplayURL:      "https://cdn.example.com/crash-up.replay",
		DownloadStatus: storage.DownloadDownloaded,
		LocalFilePath:  "/tmp/crash-up.replay",
		UploadStatus:   storage.UploadUploading,
	}

	s, _ := New(store, newMockHistoryProvider(), newMockDownloader(), newMockUploader())

	stats, err := s.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	if store.recoverCalls != 1 {
		t.Fatalf("expected RecoverInFlight to be called once, got %d", store.recoverCalls)
	}
	if stats.DownloadedCount != 1 || stats.UploadedCount != 2 {
		t.Fatalf("expected 1 download and 2 uploads after crash recovery: %+v", stats)
	}
}

func TestSyncer_PartialDownloadFailure_ContinuesToNext(t *testing.T) {
	store := newMockStore()
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m1", ReplayURL: "https://cdn.example.com/m1.replay"},
		DiscoveredMatch{MatchGUID: "m2", ReplayURL: "https://cdn.example.com/m2.replay"},
		DiscoveredMatch{MatchGUID: "m3", ReplayURL: "https://cdn.example.com/m3.replay"},
	)
	downloader := newMockDownloader()
	downloader.failForGUIDs["m2"] = errors.New("CDN 404 Not Found")

	s, _ := New(store, provider, downloader, newMockUploader())

	stats, err := s.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}
	if stats.DownloadedCount != 2 || stats.FailedCount != 1 || stats.UploadedCount != 2 {
		t.Fatalf("unexpected stats on partial failure: %+v", stats)
	}

	rec2, _ := store.GetMatch(context.Background(), "m2")
	if rec2.DownloadStatus != storage.DownloadFailed {
		t.Fatalf("m2 should be marked FAILED, got %s", rec2.DownloadStatus)
	}
}

func TestSyncer_PartialUploadFailure_ContinuesToNext(t *testing.T) {
	store := newMockStore()
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m1", ReplayURL: "https://cdn.example.com/m1.replay"},
		DiscoveredMatch{MatchGUID: "m2", ReplayURL: "https://cdn.example.com/m2.replay"},
	)
	uploader := newMockUploader()
	uploader.failForGUIDs["m1"] = errors.New("Ballchasing 500 Internal Server Error")

	s, _ := New(store, provider, newMockDownloader(), uploader)

	stats, err := s.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}
	if stats.DownloadedCount != 2 || stats.UploadedCount != 1 || stats.FailedCount != 1 {
		t.Fatalf("unexpected stats on partial upload failure: %+v", stats)
	}

	rec1, _ := store.GetMatch(context.Background(), "m1")
	if rec1.UploadStatus != storage.UploadFailed {
		t.Fatalf("m1 should be marked FAILED, got %s", rec1.UploadStatus)
	}
	rec2, _ := store.GetMatch(context.Background(), "m2")
	if rec2.UploadStatus != storage.UploadUploaded {
		t.Fatalf("m2 should be marked UPLOADED, got %s", rec2.UploadStatus)
	}
}

func TestSyncer_RetryMatch_ResetsAndCompletesPipeline(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	store.matches["m-retry"] = &storage.MatchRecord{
		MatchGUID:      "m-retry",
		ReplayURL:      "https://cdn.example.com/m-retry.replay",
		DownloadStatus: storage.DownloadFailed,
		UploadStatus:   storage.UploadPending,
		RetryCount:     2,
		LastError:      "transient error",
	}

	dl := newMockDownloader()
	up := newMockUploader()
	fetcher := newMockStatsFetcher()
	fetcher.replays["bc-m-retry"] = &ballchasing.ReplayItem{
		ID:     "bc-m-retry",
		Status: "ok",
		Blue:   ballchasing.ReplayTeam{Goals: 3},
		Orange: ballchasing.ReplayTeam{Goals: 1},
	}

	s, err := New(store, newMockHistoryProvider(), dl, up,
		WithStatsFetcher(fetcher),
		WithReplayDir(t.TempDir()),
		WithRateInterval(0),
	)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if err := s.RetryMatch(ctx, "m-retry"); err != nil {
		t.Fatalf("RetryMatch failed: %v", err)
	}

	rec, _ := store.GetMatch(ctx, "m-retry")
	if rec.DownloadStatus != storage.DownloadDownloaded {
		t.Fatalf("expected DOWNLOADED, got %s", rec.DownloadStatus)
	}
	if rec.UploadStatus != storage.UploadUploaded {
		t.Fatalf("expected UPLOADED, got %s", rec.UploadStatus)
	}
	if rec.StatsSyncStatus != storage.StatsSyncSynced {
		t.Fatalf("expected SYNCED stats, got %s", rec.StatsSyncStatus)
	}
}

func TestSyncer_RetryMatch_CanceledDoesNotMarkFailed(t *testing.T) {
	store := newMockStore()
	store.matches["m-retry-cancel"] = &storage.MatchRecord{
		MatchGUID:      "m-retry-cancel",
		ReplayURL:      "https://cdn.example.com/cancel.replay",
		DownloadStatus: storage.DownloadFailed,
		UploadStatus:   storage.UploadPending,
	}

	dl := newMockDownloader()
	ctx, cancel := context.WithCancel(context.Background())
	blockingDl := &selectiveBlockingDownloader{
		inner:        dl,
		blockForGUID: "m-retry-cancel",
		onBlock:      cancel,
	}

	s, _ := New(store, newMockHistoryProvider(), blockingDl, newMockUploader(), WithReplayDir(t.TempDir()))

	err := s.RetryMatch(ctx, "m-retry-cancel")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	rec, _ := store.GetMatch(context.Background(), "m-retry-cancel")
	if rec.DownloadStatus == storage.DownloadFailed {
		t.Fatalf("RetryMatch cancellation must not mark match FAILED, got %s", rec.DownloadStatus)
	}
	if store.markDlFailedCalls != 0 {
		t.Fatalf("expected 0 MarkDownloadFailed calls on cancel, got %d", store.markDlFailedCalls)
	}
}
