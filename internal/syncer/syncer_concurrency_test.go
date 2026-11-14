package syncer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/storage"
)

type overlapTrackingProvider struct {
	inner         *mockHistoryProvider
	activeCounter *atomic.Int32
	overlapSeen   *atomic.Bool
	delay         time.Duration
}

func (p *overlapTrackingProvider) GetRecentMatches(ctx context.Context) ([]DiscoveredMatch, error) {
	if p.activeCounter.Add(1) > 1 {
		p.overlapSeen.Store(true)
	}
	defer p.activeCounter.Add(-1)
	time.Sleep(p.delay)
	return p.inner.GetRecentMatches(ctx)
}

func (p *overlapTrackingProvider) Close() error { return p.inner.Close() }

type overlapTrackingFetcher struct {
	inner         *mockStatsFetcher
	activeCounter *atomic.Int32
	overlapSeen   *atomic.Bool
	delay         time.Duration
}

func (f *overlapTrackingFetcher) GetReplay(ctx context.Context, id string) (*ballchasing.ReplayItem, error) {
	if f.activeCounter.Add(1) > 1 {
		f.overlapSeen.Store(true)
	}
	defer f.activeCounter.Add(-1)
	time.Sleep(f.delay)
	return f.inner.GetReplay(ctx, id)
}

func TestSyncer_CycleMu_SerializesRunCycleAndPollStatsAndRetry(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	store.matches["m-pending-stats"] = &storage.MatchRecord{
		MatchGUID:       "m-pending-stats",
		Playlist:        11,
		UploadStatus:    storage.UploadUploaded,
		BallchasingID:   "bc-pend",
		StatsSyncStatus: storage.StatsSyncPending,
	}
	store.matches["m-retry-concurrent"] = &storage.MatchRecord{
		MatchGUID:      "m-retry-concurrent",
		ReplayURL:      "https://cdn.example.com/retry.replay",
		DownloadStatus: storage.DownloadFailed,
		UploadStatus:   storage.UploadPending,
	}

	var active atomic.Int32
	var overlap atomic.Bool

	provider := &overlapTrackingProvider{
		inner:         newMockHistoryProvider(DiscoveredMatch{MatchGUID: "m-cycle", ReplayURL: "https://cdn.example.com/cycle.replay"}),
		activeCounter: &active,
		overlapSeen:   &overlap,
		delay:         15 * time.Millisecond,
	}
	fetcher := newMockStatsFetcher()
	fetcher.replays["bc-pend"] = &ballchasing.ReplayItem{ID: "bc-pend", Status: "ok"}
	fetcher.replays["bc-m-cycle"] = &ballchasing.ReplayItem{ID: "bc-m-cycle", Status: "ok"}
	fetcher.replays["bc-m-retry-concurrent"] = &ballchasing.ReplayItem{ID: "bc-m-retry-concurrent", Status: "ok"}

	trackingFetcher := &overlapTrackingFetcher{
		inner:         fetcher,
		activeCounter: &active,
		overlapSeen:   &overlap,
		delay:         15 * time.Millisecond,
	}

	s, err := New(store, provider, newMockDownloader(), newMockUploader(),
		WithStatsFetcher(trackingFetcher),
		WithReplayDir(t.TempDir()),
		WithRateInterval(0),
	)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		if _, err := s.RunCycle(ctx); err != nil {
			t.Errorf("RunCycle error: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		if err := s.PollAndSyncPendingStats(ctx); err != nil {
			t.Errorf("PollAndSyncPendingStats error: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		if err := s.RetryMatch(ctx, "m-retry-concurrent"); err != nil {
			t.Errorf("RetryMatch error: %v", err)
		}
	}()
	wg.Wait()

	if overlap.Load() {
		t.Fatalf("cycleMu failed to serialize RunCycle, PollAndSyncPendingStats, and RetryMatch")
	}
}

func TestSyncer_ContextCancellation_DuringDownload(t *testing.T) {
	store := newMockStore()
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m-cancel", ReplayURL: "https://cdn.example.com/cancel.replay"},
	)
	downloader := newMockDownloader()
	downloader.blockChan = make(chan struct{})
	downloader.startedChan = make(chan struct{})

	s, _ := New(store, provider, downloader, newMockUploader())
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		<-downloader.startedChan
		cancel()
	}()

	_, err := s.RunCycle(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if store.markDlFailedCalls != 0 {
		t.Fatalf("expected 0 MarkDownloadFailed calls on cancel, got %d", store.markDlFailedCalls)
	}
	if rec := store.matches["m-cancel"]; rec.DownloadStatus != storage.DownloadDownloading {
		t.Fatalf("expected match to remain DOWNLOADING on cancel, got %s", rec.DownloadStatus)
	}
}

func TestSyncer_ContextCancellation_DuringUpload(t *testing.T) {
	store := newMockStore()
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m-cancel-up", ReplayURL: "https://cdn.example.com/cancel-up.replay"},
	)
	uploader := newMockUploader()
	uploader.blockChan = make(chan struct{})
	uploader.startedChan = make(chan struct{})

	s, _ := New(store, provider, newMockDownloader(), uploader)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		<-uploader.startedChan
		cancel()
	}()

	_, err := s.RunCycle(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if store.markUpFailedCalls != 0 {
		t.Fatalf("expected 0 MarkUploadFailed calls on cancel, got %d", store.markUpFailedCalls)
	}
	if rec := store.matches["m-cancel-up"]; rec.UploadStatus != storage.UploadUploading {
		t.Fatalf("expected match to remain UPLOADING on cancel, got %s", rec.UploadStatus)
	}
}

func TestSyncer_ContextCancellation_DuringStatsPoll_NoPoison(t *testing.T) {
	store := newMockStore()
	store.matches["m-stats-cancel"] = &storage.MatchRecord{
		MatchGUID:       "m-stats-cancel",
		Playlist:        11,
		UploadStatus:    storage.UploadUploaded,
		BallchasingID:   "bc-stats-cancel",
		StatsSyncStatus: storage.StatsSyncPending,
	}

	fetcher := newMockStatsFetcher()
	fetcher.blockChan = make(chan struct{})
	fetcher.startedChan = make(chan struct{})
	fetcher.replays["bc-stats-cancel"] = &ballchasing.ReplayItem{ID: "bc-stats-cancel", Status: "ok"}

	s, _ := New(store, newMockHistoryProvider(), newMockDownloader(), newMockUploader(),
		WithStatsFetcher(fetcher),
		WithRateInterval(0),
	)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-fetcher.startedChan
		cancel()
	}()

	err := s.PollAndSyncPendingStats(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if store.markStatsFailedCalls != 0 {
		t.Fatalf("expected 0 MarkStatsSyncFailed calls on cancel, got %d", store.markStatsFailedCalls)
	}
	if store.matches["m-stats-cancel"].StatsSyncStatus != storage.StatsSyncSyncing {
		t.Fatalf("expected StatsSyncSyncing (not FAILED), got %s", store.matches["m-stats-cancel"].StatsSyncStatus)
	}

	// Next full cycle recovers SYNCING -> PENDING and finishes stats sync.
	close(fetcher.blockChan)
	fetcher.blockChan = nil
	if _, err := s.RunCycle(context.Background()); err != nil {
		t.Fatalf("recovery RunCycle failed: %v", err)
	}
	if store.matches["m-stats-cancel"].StatsSyncStatus != storage.StatsSyncSynced {
		t.Fatalf("expected SYNCED after recovery, got %s", store.matches["m-stats-cancel"].StatsSyncStatus)
	}
}

func TestSyncer_WorkerPool_ConcurrentDownloads(t *testing.T) {
	store := newMockStore()
	var matches []DiscoveredMatch
	for i := 0; i < 6; i++ {
		guid := fmt.Sprintf("m-pool-%d", i)
		matches = append(matches, DiscoveredMatch{
			MatchGUID: guid,
			ReplayURL: "https://cdn.example.com/" + guid + ".replay",
		})
	}
	provider := newMockHistoryProvider(matches...)
	dl := newMockDownloader()
	up := newMockUploader()

	s, err := New(store, provider, dl, up, WithMaxConcurrentDownloads(3))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	stats, err := s.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}
	if stats.DownloadedCount != 6 || stats.UploadedCount != 6 || stats.FailedCount != 0 {
		t.Fatalf("unexpected worker pool stats: %+v", stats)
	}
}
