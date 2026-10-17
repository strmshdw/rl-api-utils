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

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/storage"
)

// --- Mock Implementations ---

type mockStore struct {
	mu                   sync.Mutex
	matches              map[string]*storage.MatchRecord
	recoverCalls         int
	upsertCalls          int
	listDlCalls          int
	listUpCalls          int
	listStatsCalls       int
	markDlFailedCalls    int
	markUpFailedCalls    int
	markStatsFailedCalls int
	failRecover          bool
	failUpsert           bool
	failListDl           bool
	failListUp           bool
	failListStats        bool
	failMarkDownloading  bool
	failMarkUploading    bool
	recordedOutcomes     map[string][]storage.PlayerOutcome
	matchTimes           map[string]time.Time
	savedPlayers         map[string][]*storage.MatchPlayerRecord
	authStates           map[string]struct{ token, accountID, displayName string }
	syncedStats          map[string]*storage.ReplayMatchStats
}

func newMockStore() *mockStore {
	return &mockStore{
		matches:          make(map[string]*storage.MatchRecord),
		recordedOutcomes: make(map[string][]storage.PlayerOutcome),
		matchTimes:       make(map[string]time.Time),
		savedPlayers:     make(map[string][]*storage.MatchPlayerRecord),
		authStates:       make(map[string]struct{ token, accountID, displayName string }),
		syncedStats:      make(map[string]*storage.ReplayMatchStats),
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
		if rec.StatsSyncStatus == storage.StatsSyncSyncing {
			rec.StatsSyncStatus = storage.StatsSyncPending
			rec.UpdatedAt = now
		}
	}
	return nil
}

func (m *mockStore) SyncReplayStats(ctx context.Context, stats *storage.ReplayMatchStats) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.syncedStats == nil {
		m.syncedStats = make(map[string]*storage.ReplayMatchStats)
	}
	m.syncedStats[stats.MatchGUID] = stats
	return nil
}

func (m *mockStore) ListPendingStatsSync(ctx context.Context, maxRetries int) ([]*storage.MatchRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listStatsCalls++
	if m.failListStats {
		return nil, errors.New("mock list stats error")
	}
	var res []*storage.MatchRecord
	for _, rec := range m.matches {
		isUploadedOrDup := rec.UploadStatus == storage.UploadUploaded || rec.UploadStatus == storage.UploadDuplicate
		isPendingOrRetryable := rec.StatsSyncStatus == storage.StatsSyncPending ||
			(rec.StatsSyncStatus == storage.StatsSyncFailed && rec.StatsRetryCount < maxRetries)
		if isUploadedOrDup && rec.BallchasingID != "" && isPendingOrRetryable {
			cp := *rec
			res = append(res, &cp)
		}
	}
	return res, nil
}

func (m *mockStore) MarkStatsSyncing(ctx context.Context, matchGUID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rec, ok := m.matches[matchGUID]; ok {
		rec.StatsSyncStatus = storage.StatsSyncSyncing
		rec.UpdatedAt = time.Now()
		return nil
	}
	return storage.ErrMatchNotFound
}

func (m *mockStore) MarkStatsSynced(ctx context.Context, matchGUID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rec, ok := m.matches[matchGUID]; ok {
		rec.StatsSyncStatus = storage.StatsSyncSynced
		rec.UpdatedAt = time.Now()
		return nil
	}
	return storage.ErrMatchNotFound
}

func (m *mockStore) MarkStatsSyncFailed(ctx context.Context, matchGUID, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.markStatsFailedCalls++
	if rec, ok := m.matches[matchGUID]; ok {
		rec.StatsSyncStatus = storage.StatsSyncFailed
		rec.LastError = errMsg
		rec.StatsRetryCount++
		rec.UpdatedAt = time.Now()
		return nil
	}
	return storage.ErrMatchNotFound
}

func (m *mockStore) ResetMatchRetry(ctx context.Context, matchGUID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rec, ok := m.matches[matchGUID]; ok {
		rec.RetryCount = 0
		rec.StatsRetryCount = 0
		if rec.DownloadStatus == storage.DownloadFailed {
			rec.DownloadStatus = storage.DownloadPending
		}
		if rec.UploadStatus == storage.UploadFailed {
			rec.UploadStatus = storage.UploadPending
		}
		if rec.StatsSyncStatus == storage.StatsSyncFailed || rec.StatsSyncStatus == storage.StatsSyncSyncing {
			rec.StatsSyncStatus = storage.StatsSyncPending
		}
		rec.LastError = ""
		rec.UpdatedAt = time.Now()
		return nil
	}
	return storage.ErrMatchNotFound
}

func (m *mockStore) RecordMatchResults(ctx context.Context, matchGUID string, playlistID int, outcomes []storage.PlayerOutcome, matchTime ...time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.recordedOutcomes == nil {
		m.recordedOutcomes = make(map[string][]storage.PlayerOutcome)
	}
	if _, ok := m.recordedOutcomes[matchGUID]; ok {
		return storage.ErrMatchAlreadyProcessed
	}
	m.recordedOutcomes[matchGUID] = outcomes
	if len(matchTime) > 0 {
		if m.matchTimes == nil {
			m.matchTimes = make(map[string]time.Time)
		}
		m.matchTimes[matchGUID] = matchTime[0]
	}
	return nil
}

func (m *mockStore) SaveMatchPlayers(ctx context.Context, matchGUID string, players []*storage.MatchPlayerRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.savedPlayers == nil {
		m.savedPlayers = make(map[string][]*storage.MatchPlayerRecord)
	}
	m.savedPlayers[matchGUID] = players
	return nil
}

func (m *mockStore) GetAuthState(ctx context.Context, provider string) (refreshToken, accountID, displayName string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.authStates != nil {
		if st, ok := m.authStates[provider]; ok {
			return st.token, st.accountID, st.displayName, nil
		}
	}
	return "", "", "", nil
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

type mockStatsFetcher struct {
	mu        sync.Mutex
	replays   map[string]*ballchasing.ReplayItem
	failForID map[string]error
	calls     map[string]int
}

func newMockStatsFetcher() *mockStatsFetcher {
	return &mockStatsFetcher{
		replays:   make(map[string]*ballchasing.ReplayItem),
		failForID: make(map[string]error),
		calls:     make(map[string]int),
	}
}

func (f *mockStatsFetcher) GetReplay(ctx context.Context, id string) (*ballchasing.ReplayItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[id]++
	if err, ok := f.failForID[id]; ok {
		return nil, err
	}
	if item, ok := f.replays[id]; ok {
		return item, nil
	}
	return nil, errors.New("mock replay not found")
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

func TestSyncer_PostUploadStatsPolling_Success(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	store.matches["m101"] = &storage.MatchRecord{
		MatchGUID:       "m101",
		Playlist:        11,
		UploadStatus:    storage.UploadUploaded,
		BallchasingID:   "bc-101",
		BallchasingURL:  "https://ballchasing.com/replay/bc-101",
		StatsSyncStatus: storage.StatsSyncPending,
	}

	fetcher := newMockStatsFetcher()
	fetcher.replays["bc-101"] = &ballchasing.ReplayItem{
		ID:         "bc-101",
		Status:     "ok",
		PlaylistID: "ranked-doubles",
		Duration:   300,
		Overtime:   false,
		Date:       "2026-10-16T18:00:00Z",
		Blue: ballchasing.ReplayTeam{
			Goals: 3,
			Players: []ballchasing.ReplayPlayer{
				{
					Name: "LocalHero",
					ID:   ballchasing.PlayerIdentity{Platform: "epic", ID: "epic-hero-id"},
					Score: 500, Goals: 2, Assists: 1, Saves: 1, Shots: 4,
					Demos: ballchasing.PlayerDemos{Inflicted: 1},
				},
				{
					Name: "Teammate1",
					ID:   ballchasing.PlayerIdentity{Platform: "steam", ID: "76561198000000002"},
					Score: 350, Goals: 1, Assists: 2, Saves: 2, Shots: 2,
					Demos: ballchasing.PlayerDemos{Inflicted: 0},
				},
			},
		},
		Orange: ballchasing.ReplayTeam{
			Goals: 1,
			Players: []ballchasing.ReplayPlayer{
				{
					Name: "Opponent1",
					ID:   ballchasing.PlayerIdentity{Platform: "steam", ID: "76561198000000003"},
					Score: 400, Goals: 1, Assists: 0, Saves: 3, Shots: 3,
					Demos: ballchasing.PlayerDemos{Inflicted: 2},
				},
				{
					Name: "Opponent2",
					ID:   ballchasing.PlayerIdentity{Platform: "xbox", ID: "xbox-opp-id"},
					Score: 250, Goals: 0, Assists: 1, Saves: 1, Shots: 1,
					Demos: ballchasing.PlayerDemos{Inflicted: 0},
				},
			},
		},
	}

	provider := newMockHistoryProvider()
	downloader := newMockDownloader()
	uploader := newMockUploader()

	s, err := New(store, provider, downloader, uploader,
		WithStatsFetcher(fetcher),
		WithLocalPlayerName("LocalHero"),
		WithRateInterval(0),
	)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	stats, err := s.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}
	if stats == nil {
		t.Fatalf("expected stats return")
	}

	rec := store.matches["m101"]
	if rec.StatsSyncStatus != storage.StatsSyncSynced {
		t.Errorf("expected StatsSyncSynced, got %v", rec.StatsSyncStatus)
	}

	// Verify synced stats
	st, ok := store.syncedStats["m101"]
	if !ok {
		t.Fatalf("expected syncedStats for m101")
	}
	if st.BlueScore != 3 || st.OrangeScore != 1 || *st.WinnerTeam != 0 {
		t.Errorf("unexpected scores: %+v", st)
	}

	// Verify saved players
	players, ok := store.savedPlayers["m101"]
	if !ok || len(players) != 4 {
		t.Fatalf("expected 4 saved players, got %d", len(players))
	}

	var localPlayerFound bool
	for _, p := range players {
		if p.IsLocal {
			localPlayerFound = true
			if p.Won == nil || !*p.Won {
				t.Errorf("expected local player won=true")
			}
			if p.Goals != 2 || p.Demos != 1 {
				t.Errorf("unexpected stats for local player: %+v", p)
			}
		}
	}
	if !localPlayerFound {
		t.Errorf("expected local player to be flagged in saved players")
	}

	// Verify recorded outcomes
	outcomes, ok := store.recordedOutcomes["m101"]
	if !ok || len(outcomes) != 3 {
		t.Fatalf("expected 3 recorded outcomes (excluding local), got %d", len(outcomes))
	}

	matchTime, ok := store.matchTimes["m101"]
	if !ok {
		t.Fatalf("expected matchTime to be preserved")
	}
	expectedTime, _ := storage.ParseReplayDate("2026-10-16T18:00:00Z")
	if !matchTime.Equal(expectedTime) {
		t.Errorf("expected matchTime %v, got %v", expectedTime, matchTime)
	}
}

func TestSyncer_PostUploadStatsPolling_Duplicate(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	store.matches["m-dup"] = &storage.MatchRecord{
		MatchGUID:       "m-dup",
		Playlist:        11,
		UploadStatus:    storage.UploadDuplicate,
		BallchasingID:   "bc-dup",
		BallchasingURL:  "https://ballchasing.com/replay/bc-dup",
		StatsSyncStatus: storage.StatsSyncPending,
	}

	fetcher := newMockStatsFetcher()
	fetcher.replays["bc-dup"] = &ballchasing.ReplayItem{
		ID:     "bc-dup",
		Status: "ok",
		Date:   "2026-10-16T17:00:00Z",
		Blue: ballchasing.ReplayTeam{
			Goals: 1,
			Players: []ballchasing.ReplayPlayer{
				{Name: "P1", ID: ballchasing.PlayerIdentity{Platform: "epic", ID: "id1"}},
			},
		},
		Orange: ballchasing.ReplayTeam{
			Goals: 0,
			Players: []ballchasing.ReplayPlayer{
				{Name: "P2", ID: ballchasing.PlayerIdentity{Platform: "steam", ID: "id2"}},
			},
		},
	}

	s, err := New(store, newMockHistoryProvider(), newMockDownloader(), newMockUploader(),
		WithStatsFetcher(fetcher),
		WithRateInterval(0),
	)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	_, err = s.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	rec := store.matches["m-dup"]
	if rec.StatsSyncStatus != storage.StatsSyncSynced {
		t.Errorf("expected duplicate replay to transition to StatsSyncSynced, got %v", rec.StatsSyncStatus)
	}
}

func TestSyncer_PostUploadStatsPolling_StatusPending(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	store.matches["m-pend"] = &storage.MatchRecord{
		MatchGUID:       "m-pend",
		Playlist:        11,
		UploadStatus:    storage.UploadUploaded,
		BallchasingID:   "bc-pend",
		BallchasingURL:  "https://ballchasing.com/replay/bc-pend",
		StatsSyncStatus: storage.StatsSyncPending,
	}

	fetcher := newMockStatsFetcher()
	fetcher.replays["bc-pend"] = &ballchasing.ReplayItem{
		ID:     "bc-pend",
		Status: "pending",
	}

	s, _ := New(store, newMockHistoryProvider(), newMockDownloader(), newMockUploader(),
		WithStatsFetcher(fetcher),
		WithRateInterval(0),
	)

	// Cycle 1: Ballchasing status is pending
	_, err := s.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	rec := store.matches["m-pend"]
	if rec.StatsSyncStatus != storage.StatsSyncPending {
		t.Errorf("expected match to remain StatsSyncPending, got %v", rec.StatsSyncStatus)
	}
	if rec.StatsRetryCount != 0 {
		t.Errorf("expected stats retry count 0 for pending status, got %d", rec.StatsRetryCount)
	}

	// Cycle 2: Ballchasing status is now ok
	fetcher.replays["bc-pend"].Status = "ok"
	_, err = s.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle cycle 2 failed: %v", err)
	}

	rec = store.matches["m-pend"]
	if rec.StatsSyncStatus != storage.StatsSyncSynced {
		t.Errorf("expected match to transition to StatsSyncSynced, got %v", rec.StatsSyncStatus)
	}
}

func TestSyncer_PostUploadStatsPolling_StatusFailed(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	store.matches["m-fail"] = &storage.MatchRecord{
		MatchGUID:       "m-fail",
		Playlist:        11,
		UploadStatus:    storage.UploadUploaded,
		BallchasingID:   "bc-fail",
		BallchasingURL:  "https://ballchasing.com/replay/bc-fail",
		StatsSyncStatus: storage.StatsSyncPending,
	}

	fetcher := newMockStatsFetcher()
	fetcher.replays["bc-fail"] = &ballchasing.ReplayItem{
		ID:     "bc-fail",
		Status: "failed",
	}

	s, _ := New(store, newMockHistoryProvider(), newMockDownloader(), newMockUploader(),
		WithStatsFetcher(fetcher),
		WithRateInterval(0),
	)

	_, err := s.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	rec := store.matches["m-fail"]
	if rec.StatsSyncStatus != storage.StatsSyncFailed {
		t.Errorf("expected StatsSyncFailed, got %v", rec.StatsSyncStatus)
	}
	if rec.StatsRetryCount != 1 {
		t.Errorf("expected StatsRetryCount=1, got %d", rec.StatsRetryCount)
	}
}

func TestSyncer_PostUploadStatsPolling_RetryBounded(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	store.matches["m-err"] = &storage.MatchRecord{
		MatchGUID:       "m-err",
		Playlist:        11,
		UploadStatus:    storage.UploadUploaded,
		BallchasingID:   "bc-err",
		StatsSyncStatus: storage.StatsSyncPending,
	}

	fetcher := newMockStatsFetcher()
	fetcher.failForID["bc-err"] = errors.New("500 internal server error")

	s, _ := New(store, newMockHistoryProvider(), newMockDownloader(), newMockUploader(),
		WithStatsFetcher(fetcher),
		WithMaxRetries(2),
		WithRateInterval(0),
	)

	// Attempt 1: fails
	_, _ = s.RunCycle(ctx)
	rec := store.matches["m-err"]
	if rec.StatsSyncStatus != storage.StatsSyncFailed || rec.StatsRetryCount != 1 {
		t.Fatalf("expected StatsSyncFailed with count 1, got status %v, count %d", rec.StatsSyncStatus, rec.StatsRetryCount)
	}

	// Attempt 2: still retryable (< 2)
	_, _ = s.RunCycle(ctx)
	if rec.StatsRetryCount != 2 {
		t.Fatalf("expected StatsRetryCount=2, got %d", rec.StatsRetryCount)
	}

	// Attempt 3: reached maxRetries (2), should be excluded from ListPendingStatsSync
	callsBefore := fetcher.calls["bc-err"]
	_, _ = s.RunCycle(ctx)
	if fetcher.calls["bc-err"] != callsBefore {
		t.Errorf("expected no additional calls after reaching maxRetries")
	}
}

func TestSyncer_PostUploadStatsPolling_InFlightRecovery(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	store.matches["m-recov"] = &storage.MatchRecord{
		MatchGUID:       "m-recov",
		Playlist:        11,
		UploadStatus:    storage.UploadUploaded,
		BallchasingID:   "bc-recov",
		StatsSyncStatus: storage.StatsSyncSyncing, // left in-flight by crash
	}

	fetcher := newMockStatsFetcher()
	fetcher.replays["bc-recov"] = &ballchasing.ReplayItem{
		ID:     "bc-recov",
		Status: "ok",
		Date:   "2026-10-16T18:00:00Z",
	}

	s, _ := New(store, newMockHistoryProvider(), newMockDownloader(), newMockUploader(),
		WithStatsFetcher(fetcher),
		WithRateInterval(0),
	)

	_, err := s.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	rec := store.matches["m-recov"]
	if rec.StatsSyncStatus != storage.StatsSyncSynced {
		t.Errorf("expected recovered in-flight record to transition to StatsSyncSynced, got %v", rec.StatsSyncStatus)
	}
}

func TestSyncer_ResolveLocalPlayer_AllTiers(t *testing.T) {
	roster := []ReplayPlayerCandidate{
		{Name: "PlayerTier1", Platform: "Epic", OnlineID: "epic-p1", CanonicalID: "Epic|epic-p1|0", Team: 0},
		{Name: "PlayerTier2", Platform: "Steam", OnlineID: "76561198000000002", CanonicalID: "Steam|76561198000000002|0", Team: 1},
		{Name: "PlayerTier3", Platform: "Steam", OnlineID: "76561198000000003", CanonicalID: "Steam|76561198000000003|0", Team: 0},
		{Name: "PlayerTier4", Platform: "Xbox", OnlineID: "xbox-p4", CanonicalID: "Xbox|xbox-p4|0", Team: 1},
		{Name: "PlayerTier5", Platform: "PlayStation", OnlineID: "ps-p5", CanonicalID: "PlayStation|ps-p5|0", Team: 0},
		{Name: "PlayerTier6", Platform: "Switch", OnlineID: "sw-p6", CanonicalID: "Switch|sw-p6|0", Team: 1},
	}

	// Tier 1: Config LocalPlayerID
	idx, team, found := ResolveLocalPlayer(roster, LocalPlayerIdentity{
		ConfigLocalPlayerID: "Epic|epic-p1|0",
	})
	if !found || idx != 0 || *team != 0 {
		t.Errorf("Tier 1 resolution failed: idx=%d, team=%v, found=%v", idx, team, found)
	}

	// Tier 2: Auth Primary Account ID
	idx, team, found = ResolveLocalPlayer(roster, LocalPlayerIdentity{
		AuthAccountID: "76561198000000002",
	})
	if !found || idx != 1 || *team != 1 {
		t.Errorf("Tier 2 resolution failed: idx=%d, team=%v, found=%v", idx, team, found)
	}

	// Tier 3: Ping / Uploader SteamID
	idx, team, found = ResolveLocalPlayer(roster, LocalPlayerIdentity{
		PingSteamID: "76561198000000003",
	})
	if !found || idx != 2 || *team != 0 {
		t.Errorf("Tier 3 resolution failed: idx=%d, team=%v, found=%v", idx, team, found)
	}

	// Tier 4: Config LocalPlayerName
	idx, team, found = ResolveLocalPlayer(roster, LocalPlayerIdentity{
		ConfigLocalPlayerName: "PlayerTier4",
	})
	if !found || idx != 3 || *team != 1 {
		t.Errorf("Tier 4 resolution failed: idx=%d, team=%v, found=%v", idx, team, found)
	}

	// Tier 5: Auth Display Name
	idx, team, found = ResolveLocalPlayer(roster, LocalPlayerIdentity{
		AuthDisplayName: "PlayerTier5",
	})
	if !found || idx != 4 || *team != 0 {
		t.Errorf("Tier 5 resolution failed: idx=%d, team=%v, found=%v", idx, team, found)
	}

	// Tier 6: Replay Header PlayerName
	idx, team, found = ResolveLocalPlayer(roster, LocalPlayerIdentity{
		ReplayPlayerName: "PlayerTier6",
	})
	if !found || idx != 5 || *team != 1 {
		t.Errorf("Tier 6 resolution failed: idx=%d, team=%v, found=%v", idx, team, found)
	}

	// Tier 6 Fallback: ReplayLocalTeam known without player
	localTeamNum := 0
	idx, team, found = ResolveLocalPlayer(roster, LocalPlayerIdentity{
		ReplayLocalTeam: &localTeamNum,
	})
	if !found || idx != -1 || *team != 0 {
		t.Errorf("Tier 6 team fallback failed: idx=%d, team=%v, found=%v", idx, team, found)
	}

	// No match
	idx, team, found = ResolveLocalPlayer(roster, LocalPlayerIdentity{
		ConfigLocalPlayerName: "NoSuchPlayer",
	})
	if found || idx != -1 || team != nil {
		t.Errorf("expected no match, got idx=%d, team=%v, found=%v", idx, team, found)
	}
}

func TestSyncer_ResolveLocalPlayer_BotImmunity(t *testing.T) {
	roster := []ReplayPlayerCandidate{
		{Name: "Saltie", Platform: "Unknown", OnlineID: "bot-1", CanonicalID: "bot-1", Team: 0, IsBot: true},
		{Name: "RealPlayer", Platform: "Epic", OnlineID: "real-id", CanonicalID: "Epic|real-id|0", Team: 1, IsBot: false},
	}

	// Config name conflicts with bot name
	idx, team, found := ResolveLocalPlayer(roster, LocalPlayerIdentity{
		ConfigLocalPlayerName: "Saltie",
	})
	if found || idx != -1 || team != nil {
		t.Errorf("bot immunity violated: bot was resolved as local player! idx=%d, team=%v", idx, team)
	}

	// Config ID conflicts with bot ID
	idx, team, found = ResolveLocalPlayer(roster, LocalPlayerIdentity{
		ConfigLocalPlayerID: "bot-1",
	})
	if found || idx != -1 || team != nil {
		t.Errorf("bot immunity violated for ID: bot was resolved as local player!")
	}

	// Real player match succeeds
	idx, team, found = ResolveLocalPlayer(roster, LocalPlayerIdentity{
		ConfigLocalPlayerName: "RealPlayer",
	})
	if !found || idx != 1 || *team != 1 {
		t.Errorf("failed to resolve legitimate human player")
	}
}

func TestSyncer_SyncDownloadedReplayStats_Idempotent(t *testing.T) {
	files, err := filepath.Glob("../../replays/*.replay")
	if err != nil || len(files) == 0 {
		t.Skip("no replay files found")
	}

	store := newMockStore()
	guid := "test-idempotent-guid"
	store.matches[guid] = &storage.MatchRecord{
		MatchGUID: guid,
		Playlist:  11,
	}

	s, err := New(store, newMockHistoryProvider(), newMockDownloader(), newMockUploader())
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	ctx := context.Background()
	// Call 1
	if err := s.SyncDownloadedReplayStats(ctx, guid, files[0]); err != nil {
		t.Fatalf("first call failed: %v", err)
	}

	// Call 2: RecordMatchResults will return ErrMatchAlreadyProcessed
	if err := s.SyncDownloadedReplayStats(ctx, guid, files[0]); err != nil {
		t.Fatalf("second call should ignore ErrMatchAlreadyProcessed, but failed with: %v", err)
	}
}
