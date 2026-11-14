package storage

import (
	"context"
	"sync"
	"time"
)

// JSONStore implements StateStore using an atomic JSON snapshot file backed by an isolated in-memory SQLite engine.
type JSONStore struct {
	mu       sync.RWMutex
	filePath string
	inner    *SQLiteStore
	closed   bool
}

// NewJSONStore opens or initializes a JSON-persisted state store at filePath.
func NewJSONStore(filePath string) (*JSONStore, error) {
	return loadJSONStoreEngine(filePath)
}

func (s *JSONStore) saveLocked() error {
	payload, err := dumpSQLiteToPayload(context.Background(), s.inner)
	if err != nil {
		return err
	}
	return writePayloadAtomically(s.filePath, payload)
}

// SetRetryPolicy overrides retry scheduling parameters for failed transfers.
func (s *JSONStore) SetRetryPolicy(maxRetries int, baseBackoff, maxBackoff time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inner != nil {
		s.inner.SetRetryPolicy(maxRetries, baseBackoff, maxBackoff)
	}
}

// Close closes the backing engine idempotently.
func (s *JSONStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.inner != nil {
		return s.inner.Close()
	}
	return nil
}

func (s *JSONStore) GetMatch(ctx context.Context, matchGUID string) (*MatchRecord, error) {
	return withRead(s, func() (*MatchRecord, error) { return s.inner.GetMatch(ctx, matchGUID) })
}

func (s *JSONStore) GetMatchByBallchasingID(ctx context.Context, ballchasingID string) (*MatchRecord, error) {
	return withRead(s, func() (*MatchRecord, error) { return s.inner.GetMatchByBallchasingID(ctx, ballchasingID) })
}

func (s *JSONStore) ListPendingDownloads(ctx context.Context) ([]*MatchRecord, error) {
	return withRead(s, func() ([]*MatchRecord, error) { return s.inner.ListPendingDownloads(ctx) })
}

func (s *JSONStore) ListPendingUploads(ctx context.Context) ([]*MatchRecord, error) {
	return withRead(s, func() ([]*MatchRecord, error) { return s.inner.ListPendingUploads(ctx) })
}

func (s *JSONStore) ListPendingStatsSync(ctx context.Context, maxRetries int) ([]*MatchRecord, error) {
	return withRead(s, func() ([]*MatchRecord, error) { return s.inner.ListPendingStatsSync(ctx, maxRetries) })
}

func (s *JSONStore) UpsertDiscoveredMatches(ctx context.Context, matches []*MatchRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrStoreClosed
	}
	if err := s.inner.UpsertDiscoveredMatches(ctx, matches); err != nil {
		return err
	}
	if len(matches) == 0 {
		return nil
	}
	return s.saveLocked()
}

func (s *JSONStore) MarkDownloading(ctx context.Context, matchGUID string) error {
	return s.withWrite(func() error { return s.inner.MarkDownloading(ctx, matchGUID) })
}

func (s *JSONStore) MarkDownloaded(ctx context.Context, matchGUID, localPath string) error {
	return s.withWrite(func() error { return s.inner.MarkDownloaded(ctx, matchGUID, localPath) })
}

func (s *JSONStore) MarkDownloadFailed(ctx context.Context, matchGUID, errMsg string) error {
	return s.withWrite(func() error { return s.inner.MarkDownloadFailed(ctx, matchGUID, errMsg) })
}

func (s *JSONStore) MarkUploading(ctx context.Context, matchGUID string) error {
	return s.withWrite(func() error { return s.inner.MarkUploading(ctx, matchGUID) })
}

func (s *JSONStore) MarkUploaded(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error {
	return s.withWrite(func() error { return s.inner.MarkUploaded(ctx, matchGUID, ballchasingID, ballchasingURL) })
}

func (s *JSONStore) MarkDuplicate(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error {
	return s.withWrite(func() error { return s.inner.MarkDuplicate(ctx, matchGUID, ballchasingID, ballchasingURL) })
}

func (s *JSONStore) MarkUploadFailed(ctx context.Context, matchGUID, errMsg string) error {
	return s.withWrite(func() error { return s.inner.MarkUploadFailed(ctx, matchGUID, errMsg) })
}

func (s *JSONStore) MarkStatsSyncing(ctx context.Context, matchGUID string) error {
	return s.withWrite(func() error { return s.inner.MarkStatsSyncing(ctx, matchGUID) })
}

func (s *JSONStore) MarkStatsSynced(ctx context.Context, matchGUID string) error {
	return s.withWrite(func() error { return s.inner.MarkStatsSynced(ctx, matchGUID) })
}

func (s *JSONStore) MarkStatsSyncFailed(ctx context.Context, matchGUID, errMsg string) error {
	return s.withWrite(func() error { return s.inner.MarkStatsSyncFailed(ctx, matchGUID, errMsg) })
}

func (s *JSONStore) ResetMatchRetry(ctx context.Context, matchGUID string) error {
	return s.withWrite(func() error { return s.inner.ResetMatchRetry(ctx, matchGUID) })
}

func (s *JSONStore) RecoverInFlight(ctx context.Context) error {
	return s.withWrite(func() error { return s.inner.RecoverInFlight(ctx) })
}

func (s *JSONStore) RepairHistoricalData(ctx context.Context) error {
	return s.withWrite(func() error { return s.inner.RepairHistoricalData(ctx) })
}

func (s *JSONStore) SaveAuthState(ctx context.Context, provider, refreshToken, accountID, displayName string) error {
	return s.withWrite(func() error { return s.inner.SaveAuthState(ctx, provider, refreshToken, accountID, displayName) })
}

func (s *JSONStore) GetAuthState(ctx context.Context, provider string) (string, string, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return "", "", "", ErrStoreClosed
	}
	return s.inner.GetAuthState(ctx, provider)
}

func (s *JSONStore) UpsertPlayer(ctx context.Context, player *PlayerRecord) error {
	return s.withWrite(func() error { return s.inner.UpsertPlayer(ctx, player) })
}

func (s *JSONStore) GetPlayer(ctx context.Context, playerID string) (*PlayerRecord, error) {
	return withRead(s, func() (*PlayerRecord, error) { return s.inner.GetPlayer(ctx, playerID) })
}

func (s *JSONStore) GetLocalPlayerProfile(ctx context.Context, playerID string) (*PlayerRecord, []*PlayerMatchup, error) {
	return withRead2(s, func() (*PlayerRecord, []*PlayerMatchup, error) { return s.inner.GetLocalPlayerProfile(ctx, playerID) })
}

func (s *JSONStore) ListPlayers(ctx context.Context, limit, offset int) ([]*PlayerRecord, error) {
	return withRead(s, func() ([]*PlayerRecord, error) { return s.inner.ListPlayers(ctx, limit, offset) })
}

func (s *JSONStore) ListPlayerSummaries(ctx context.Context, limit, offset int) ([]*PlayerSummary, error) {
	return withRead(s, func() ([]*PlayerSummary, error) { return s.inner.ListPlayerSummaries(ctx, limit, offset) })
}

func (s *JSONStore) SearchPlayerSummaries(ctx context.Context, query, platform string, limit, offset int) ([]*PlayerSummary, int, error) {
	return withRead2(s, func() ([]*PlayerSummary, int, error) {
		return s.inner.SearchPlayerSummaries(ctx, query, platform, limit, offset)
	})
}

func (s *JSONStore) FilterPlayerSummaries(ctx context.Context, filter PlayerDirectoryFilter) ([]*PlayerSummary, int, error) {
	return withRead2(s, func() ([]*PlayerSummary, int, error) { return s.inner.FilterPlayerSummaries(ctx, filter) })
}

func (s *JSONStore) GetPlayerSpotlights(ctx context.Context) (*GlobalSpotlights, error) {
	return withRead(s, func() (*GlobalSpotlights, error) { return s.inner.GetPlayerSpotlights(ctx) })
}

func (s *JSONStore) GetLocalPlayerIDs(ctx context.Context) ([]string, error) {
	return withRead(s, func() ([]string, error) { return s.inner.GetLocalPlayerIDs(ctx) })
}

func (s *JSONStore) UpdatePlayerRanks(ctx context.Context, playerID string, ranksJSON string) error {
	return s.withWrite(func() error { return s.inner.UpdatePlayerRanks(ctx, playerID, ranksJSON) })
}

func (s *JSONStore) RecordMatchResults(ctx context.Context, matchGUID string, playlistID int, outcomes []PlayerOutcome, matchTime ...time.Time) error {
	return s.withWrite(func() error { return s.inner.RecordMatchResults(ctx, matchGUID, playlistID, outcomes, matchTime...) })
}

func (s *JSONStore) GetPlayerMatchup(ctx context.Context, playerID string, playlistID int) (*PlayerMatchup, error) {
	return withRead(s, func() (*PlayerMatchup, error) { return s.inner.GetPlayerMatchup(ctx, playerID, playlistID) })
}

func (s *JSONStore) GetPlayerMatchups(ctx context.Context, playerID string) ([]*PlayerMatchup, error) {
	return withRead(s, func() ([]*PlayerMatchup, error) { return s.inner.GetPlayerMatchups(ctx, playerID) })
}

func (s *JSONStore) SyncReplayStats(ctx context.Context, stats *ReplayMatchStats) error {
	return s.withWrite(func() error { return s.inner.SyncReplayStats(ctx, stats) })
}

func (s *JSONStore) ListHistoricalMatches(ctx context.Context, filter MatchFilter) ([]*HistoricalMatchRecord, int, error) {
	return withRead2(s, func() ([]*HistoricalMatchRecord, int, error) { return s.inner.ListHistoricalMatches(ctx, filter) })
}

func (s *JSONStore) SaveMatchPlayers(ctx context.Context, matchGUID string, players []*MatchPlayerRecord) error {
	return s.withWrite(func() error { return s.inner.SaveMatchPlayers(ctx, matchGUID, players) })
}

func (s *JSONStore) GetMatchPlayers(ctx context.Context, matchGUID string) ([]*MatchPlayerRecord, error) {
	return withRead(s, func() ([]*MatchPlayerRecord, error) { return s.inner.GetMatchPlayers(ctx, matchGUID) })
}

func (s *JSONStore) ListPlayerMatches(ctx context.Context, playerID string, filter PlayerMatchFilter) ([]*PlayerMatchDetail, int, error) {
	return withRead2(s, func() ([]*PlayerMatchDetail, int, error) { return s.inner.ListPlayerMatches(ctx, playerID, filter) })
}

func (s *JSONStore) RecordMMR(ctx context.Context, record *MMRHistoryRecord) error {
	return s.withWrite(func() error { return s.inner.RecordMMR(ctx, record) })
}

func (s *JSONStore) ListMMRHistory(ctx context.Context, playlistID int, limit int) ([]*MMRHistoryRecord, error) {
	return withRead(s, func() ([]*MMRHistoryRecord, error) { return s.inner.ListMMRHistory(ctx, playlistID, limit) })
}

func (s *JSONStore) UpdatePlayerNotesAndTags(ctx context.Context, playerID string, notes string, tags []string) error {
	return s.withWrite(func() error { return s.inner.UpdatePlayerNotesAndTags(ctx, playerID, notes, tags) })
}

func (s *JSONStore) SaveSession(ctx context.Context, session *SessionRecord) error {
	return s.withWrite(func() error { return s.inner.SaveSession(ctx, session) })
}

func (s *JSONStore) GetSession(ctx context.Context, sessionID string) (*SessionRecord, error) {
	return withRead(s, func() (*SessionRecord, error) { return s.inner.GetSession(ctx, sessionID) })
}

func (s *JSONStore) ListSessions(ctx context.Context, limit, offset int) ([]*SessionRecord, int, error) {
	return withRead2(s, func() ([]*SessionRecord, int, error) { return s.inner.ListSessions(ctx, limit, offset) })
}

func (s *JSONStore) DeleteSession(ctx context.Context, sessionID string) error {
	return s.withWrite(func() error { return s.inner.DeleteSession(ctx, sessionID) })
}

func (s *JSONStore) PruneOldestMatches(ctx context.Context, count int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, ErrStoreClosed
	}
	pruned, err := s.inner.PruneOldestMatches(ctx, count)
	if err != nil {
		return pruned, err
	}
	if pruned > 0 {
		if saveErr := s.saveLocked(); saveErr != nil {
			return pruned, saveErr
		}
	}
	return pruned, nil
}

func (s *JSONStore) ComputePlayerSplitStats(
	ctx context.Context,
	playerID, playerName string,
	playlistIDs []int,
	fromUnix, toUnix int64,
	fbTMWins, fbTMLosses, fbOppWins, fbOppLosses int,
) (*PlayerRoleStatsSummary, *PlayerRoleStatsSummary) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return &PlayerRoleStatsSummary{}, &PlayerRoleStatsSummary{}
	}
	return s.inner.ComputePlayerSplitStats(ctx, playerID, playerName, playlistIDs, fromUnix, toUnix, fbTMWins, fbTMLosses, fbOppWins, fbOppLosses)
}

func (s *JSONStore) EnrichMatchStatsWithPlayers(ctx context.Context, matchGUID string, playlist int, mr *MatchRecord, st *ReplayMatchStats) *ReplayMatchStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return st
	}
	return s.inner.EnrichMatchStatsWithPlayers(ctx, matchGUID, playlist, mr, st)
}
