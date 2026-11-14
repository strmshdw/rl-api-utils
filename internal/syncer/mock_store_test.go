package syncer

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/dank/rl-api-utils/internal/storage"
)

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
	cp := *stats
	if prev, ok := m.syncedStats[stats.MatchGUID]; ok && len(stats.Players) == 0 && len(prev.Players) > 0 {
		cp.Players = prev.Players
	}
	m.syncedStats[stats.MatchGUID] = &cp
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
