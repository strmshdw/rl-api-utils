package syncer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/storage"
)

// --- Mock Implementations ---

type mockStore struct {
	mu                  sync.Mutex
	matches             map[string]*storage.MatchRecord
	recoverCalls        int
	upsertCalls         int
	listDlCalls         int
	listUpCalls         int
	markDlFailedCalls   int
	markUpFailedCalls   int
	failRecover         bool
	failUpsert          bool
	failListDl          bool
	failListUp          bool
	failMarkDownloading bool
	failMarkUploading   bool
}

func newMockStore() *mockStore {
	return &mockStore{
		matches: make(map[string]*storage.MatchRecord),
	}
}

func (m *mockStore) GetMatch(ctx context.Context, matchGUID string) (*storage.MatchRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.matches[matchGUID]
	if !ok {
		return nil, storage.ErrMatchNotFound
	}
	cp := *rec
	return &cp, nil
}

func (m *mockStore) ListPendingDownloads(ctx context.Context) ([]*storage.MatchRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listDlCalls++
	if m.failListDl {
		return nil, errors.New("mock list dl error")
	}
	var res []*storage.MatchRecord
	for _, rec := range m.matches {
		if rec.DownloadStatus == storage.DownloadPending && rec.ReplayURL != "" {
			cp := *rec
			res = append(res, &cp)
		}
	}
	return res, nil
}

func (m *mockStore) ListPendingUploads(ctx context.Context) ([]*storage.MatchRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listUpCalls++
	if m.failListUp {
		return nil, errors.New("mock list up error")
	}
	var res []*storage.MatchRecord
	for _, rec := range m.matches {
		if rec.DownloadStatus == storage.DownloadDownloaded && rec.UploadStatus == storage.UploadPending && rec.LocalFilePath != "" {
			cp := *rec
			res = append(res, &cp)
		}
	}
	return res, nil
}

func (m *mockStore) UpsertDiscoveredMatches(ctx context.Context, matches []*storage.MatchRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.upsertCalls++
	if m.failUpsert {
		return errors.New("mock upsert error")
	}
	now := time.Now()
	for _, rec := range matches {
		if existing, ok := m.matches[rec.MatchGUID]; ok {
			if existing.ReplayURL == "" && rec.ReplayURL != "" {
				existing.ReplayURL = rec.ReplayURL
				if existing.DownloadStatus == storage.DownloadSkipped {
					existing.DownloadStatus = storage.DownloadPending
				}
				existing.UpdatedAt = now
			}
			continue
		}
		cp := *rec
		if cp.CreatedAt.IsZero() {
			cp.CreatedAt = now
		}
		cp.UpdatedAt = now
		m.matches[rec.MatchGUID] = &cp
	}
	return nil
}

func (m *mockStore) MarkDownloading(ctx context.Context, matchGUID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failMarkDownloading {
		return errors.New("mock mark downloading error")
	}
	if rec, ok := m.matches[matchGUID]; ok {
		rec.DownloadStatus = storage.DownloadDownloading
		rec.UpdatedAt = time.Now()
		return nil
	}
	return storage.ErrMatchNotFound
}

func (m *mockStore) MarkDownloaded(ctx context.Context, matchGUID, localPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rec, ok := m.matches[matchGUID]; ok {
		now := time.Now()
		rec.DownloadStatus = storage.DownloadDownloaded
		rec.LocalFilePath = localPath
		rec.DownloadedAt = &now
		rec.UpdatedAt = now
		return nil
	}
	return storage.ErrMatchNotFound
}

func (m *mockStore) MarkDownloadFailed(ctx context.Context, matchGUID, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.markDlFailedCalls++
	if rec, ok := m.matches[matchGUID]; ok {
		rec.DownloadStatus = storage.DownloadFailed
		rec.LastError = errMsg
		rec.RetryCount++
		rec.UpdatedAt = time.Now()
		return nil
	}
	return storage.ErrMatchNotFound
}

func (m *mockStore) MarkUploading(ctx context.Context, matchGUID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failMarkUploading {
		return errors.New("mock mark uploading error")
	}
	if rec, ok := m.matches[matchGUID]; ok {
		rec.UploadStatus = storage.UploadUploading
		rec.UpdatedAt = time.Now()
		return nil
	}
	return storage.ErrMatchNotFound
}

func (m *mockStore) MarkUploaded(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rec, ok := m.matches[matchGUID]; ok {
		now := time.Now()
		rec.UploadStatus = storage.UploadUploaded
		rec.BallchasingID = ballchasingID
		rec.BallchasingURL = ballchasingURL
		rec.UploadedAt = &now
		rec.UpdatedAt = now
		return nil
	}
	return storage.ErrMatchNotFound
}

func (m *mockStore) MarkDuplicate(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rec, ok := m.matches[matchGUID]; ok {
		now := time.Now()
		rec.UploadStatus = storage.UploadDuplicate
		rec.BallchasingID = ballchasingID
		rec.BallchasingURL = ballchasingURL
		rec.UploadedAt = &now
		rec.UpdatedAt = now
		return nil
	}
	return storage.ErrMatchNotFound
}

func (m *mockStore) MarkUploadFailed(ctx context.Context, matchGUID, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.markUpFailedCalls++
	if rec, ok := m.matches[matchGUID]; ok {
		rec.UploadStatus = storage.UploadFailed
		rec.LastError = errMsg
		rec.RetryCount++
		rec.UpdatedAt = time.Now()
		return nil
	}
	return storage.ErrMatchNotFound
}

func (m *mockStore) RecoverInFlight(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recoverCalls++
	if m.failRecover {
		return errors.New("mock recover error")
	}
	now := time.Now()
	for _, rec := range m.matches {
		if rec.DownloadStatus == storage.DownloadDownloading {
			rec.DownloadStatus = storage.DownloadPending
			rec.UpdatedAt = now
		}
		if rec.UploadStatus == storage.UploadUploading {
			rec.UploadStatus = storage.UploadPending
			rec.UpdatedAt = now
		}
	}
	return nil
}

type mockHistoryProvider struct {
	mu      sync.Mutex
	matches []DiscoveredMatch
	calls   int
	err     error
	closed  bool
}

func newMockHistoryProvider(matches ...DiscoveredMatch) *mockHistoryProvider {
	return &mockHistoryProvider{matches: matches}
}

func (p *mockHistoryProvider) GetRecentMatches(ctx context.Context) ([]DiscoveredMatch, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	res := make([]DiscoveredMatch, len(p.matches))
	copy(res, p.matches)
	return res, nil
}

func (p *mockHistoryProvider) SetMatches(matches []DiscoveredMatch) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.matches = matches
}

func (p *mockHistoryProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

type mockDownloader struct {
	mu           sync.Mutex
	downloaded   map[string]string // guid -> localPath
	failForGUIDs map[string]error
	calls        int
	blockChan    chan struct{}
}

func newMockDownloader() *mockDownloader {
	return &mockDownloader{
		downloaded:   make(map[string]string),
		failForGUIDs: make(map[string]error),
	}
}

func (d *mockDownloader) DownloadReplay(ctx context.Context, matchGUID, replayURL, destDir string) (string, error) {
	d.mu.Lock()
	d.calls++
	block := d.blockChan
	if err, ok := d.failForGUIDs[matchGUID]; ok {
		d.mu.Unlock()
		return "", err
	}
	d.mu.Unlock()

	if block != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-block:
		}
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	p := filepath.Join(destDir, fmt.Sprintf("%s.replay", matchGUID))
	d.mu.Lock()
	d.downloaded[matchGUID] = p
	d.mu.Unlock()
	return p, nil
}

type mockUploader struct {
	mu           sync.Mutex
	uploaded     map[string]string // guid -> filePath
	failForGUIDs map[string]error
	duplicates   map[string]bool
	calls        int
	blockChan    chan struct{}
}

func newMockUploader() *mockUploader {
	return &mockUploader{
		uploaded:     make(map[string]string),
		failForGUIDs: make(map[string]error),
		duplicates:   make(map[string]bool),
	}
}

func (u *mockUploader) UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error) {
	u.mu.Lock()
	u.calls++
	block := u.blockChan
	if err, ok := u.failForGUIDs[matchGUID]; ok {
		u.mu.Unlock()
		return nil, err
	}
	isDup := u.duplicates[matchGUID]
	u.mu.Unlock()

	if block != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-block:
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	u.mu.Lock()
	u.uploaded[matchGUID] = filePath
	u.mu.Unlock()

	return &UploadResult{
		ID:          fmt.Sprintf("bc-%s", matchGUID),
		Location:    fmt.Sprintf("https://ballchasing.com/replay/bc-%s", matchGUID),
		IsDuplicate: isDup,
	}, nil
}

// --- Unit Tests ---

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

	// Test NewWithConfig
	s2, err := NewWithConfig(store, provider, downloader, uploader, Config{
		ReplayDir:      "/tmp/cfg-replays",
		DryRun:         true,
		KeepLocalFiles: false,
	})
	if err != nil || s2.replayDir != "/tmp/cfg-replays" {
		t.Fatalf("NewWithConfig failed: %v", err)
	}

	// Test NewSyncerEngine
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

	stats, err := s.Sync(context.Background()) // also tests Sync alias
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

	// Cycle 1
	stats1, err := s.RunCycle(context.Background())
	if err != nil || stats1.DownloadedCount != 1 || stats1.UploadedCount != 1 {
		t.Fatalf("cycle 1 failed: %v, stats: %+v", err, stats1)
	}

	// Cycle 2: add m2, keep m1
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
	// Cycle 1: ReplayURL is empty (delayed CDN generation)
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

	// Cycle 2: CDN populates ReplayURL
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

	if stats.DiscoveredCount != 1 {
		t.Fatalf("expected 1 discovered in dry run, got %d", stats.DiscoveredCount)
	}
	if stats.DownloadedCount != 0 || stats.UploadedCount != 0 {
		t.Fatalf("dry run must not download or upload: %+v", stats)
	}
	if downloader.calls != 0 || uploader.calls != 0 {
		t.Fatalf("downloader or uploader called in dry run: dl=%d, up=%d", downloader.calls, uploader.calls)
	}

	// Verify store was not mutated
	_, err = store.GetMatch(context.Background(), "m-dry")
	if !errors.Is(err, storage.ErrMatchNotFound) {
		t.Fatalf("dry run must not insert match into store, got %v", err)
	}
}

func TestSyncer_CrashRecovery_ResetsInFlight(t *testing.T) {
	store := newMockStore()
	// Pre-seed orphaned in-flight states from simulated crash
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

	provider := newMockHistoryProvider() // no new discovered matches
	downloader := newMockDownloader()
	uploader := newMockUploader()

	s, _ := New(store, provider, downloader, uploader)

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

func TestSyncer_ContextCancellation_DuringDownload(t *testing.T) {
	store := newMockStore()
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m-cancel", ReplayURL: "https://cdn.example.com/cancel.replay"},
	)
	downloader := newMockDownloader()
	downloader.blockChan = make(chan struct{})
	uploader := newMockUploader()

	s, _ := New(store, provider, downloader, uploader)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := s.RunCycle(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestSyncer_ContextCancellation_DuringUpload(t *testing.T) {
	store := newMockStore()
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m-cancel-up", ReplayURL: "https://cdn.example.com/cancel-up.replay"},
	)
	downloader := newMockDownloader()
	uploader := newMockUploader()
	uploader.blockChan = make(chan struct{})

	s, _ := New(store, provider, downloader, uploader)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := s.RunCycle(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
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
	uploader := newMockUploader()

	s, _ := New(store, provider, downloader, uploader)

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
	downloader := newMockDownloader()
	uploader := newMockUploader()
	uploader.failForGUIDs["m1"] = errors.New("Ballchasing 500 Internal Server Error")

	s, _ := New(store, provider, downloader, uploader)

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

func TestSyncer_ProviderError_AbortsCycle(t *testing.T) {
	store := newMockStore()
	provider := newMockHistoryProvider()
	provider.err = errors.New("psynet rpc connection failed")
	downloader := newMockDownloader()
	uploader := newMockUploader()

	s, _ := New(store, provider, downloader, uploader)

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

	uploader := newMockUploader()

	s, err := New(store, provider, downloader, uploader,
		WithReplayDir(tmpDir),
		WithKeepLocalFiles(false),
	)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	stats, err := s.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}
	if stats.UploadedCount != 1 {
		t.Fatalf("expected 1 upload, got %d", stats.UploadedCount)
	}

	// Verify file was unlinked
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("expected file %s to be deleted when KeepLocalFiles=false, stat err: %v", filePath, err)
	}
}

func TestSyncer_StoreErrors(t *testing.T) {
	store := newMockStore()
	store.failRecover = true
	provider := newMockHistoryProvider()
	downloader := newMockDownloader()
	uploader := newMockUploader()

	s, _ := New(store, provider, downloader, uploader)

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
