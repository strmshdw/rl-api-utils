package session

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/dank/rl-api-utils/internal/domain"
	"github.com/dank/rl-api-utils/internal/storage"
)

// SetStateStore attaches a persistent StateStore if one is not already configured,
// and flushes any sessions already recorded in memory to the store.
func (s *SessionTracker) SetStateStore(store storage.StateStore) {
	if store == nil {
		return
	}
	s.mu.Lock()
	var toPersist []*storage.SessionRecord
	if s.store == nil {
		s.store = store
		if len(s.matches) > 0 {
			_ = s.buildSessionRecordLocked(nil)
		}
		for _, rec := range s.archivedMemorySessions {
			if rec != nil {
				toPersist = append(toPersist, rec)
			}
		}
	}
	s.mu.Unlock()

	for _, rec := range toPersist {
		s.saveRecordToStore(rec)
	}
}

// RestoreFromStore hydrates recent persisted session records and continuation dismissal flags
// from the configured StateStore into memory without overwriting the active session epoch.
func (s *SessionTracker) RestoreFromStore(ctx context.Context) error {
	s.mu.RLock()
	store := s.store
	s.mu.RUnlock()
	if store == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	recs, _, err := store.ListSessions(ctx, 25, 0)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.archivedMemorySessions == nil {
		s.archivedMemorySessions = make(map[string]*storage.SessionRecord)
	}
	if s.dismissedContinueIDs == nil {
		s.dismissedContinueIDs = make(map[string]bool)
	}
	for _, rec := range recs {
		if rec == nil || strings.TrimSpace(rec.SessionID) == "" {
			continue
		}
		if _, exists := s.archivedMemorySessions[rec.SessionID]; !exists {
			recCopy := *rec
			s.archivedMemorySessions[rec.SessionID] = &recCopy
		}
		if rec.Dismissed {
			s.dismissedContinueIDs[rec.SessionID] = true
		}
	}
	return nil
}

// buildSessionRecordLocked constructs a storage.SessionRecord snapshot for the current session.
// Caller must hold s.mu. Returns nil if the current session has 0 completed matches.
func (s *SessionTracker) buildSessionRecordLocked(endedAt *time.Time) *storage.SessionRecord {
	totalMatches := len(s.matches)
	if totalMatches == 0 {
		return nil
	}

	winRate := domain.ComputeWinRate(s.totalWins, totalMatches)

	playlistMap := make(map[string]*PlaylistSessionStats, len(s.playlists))
	for k, v := range s.playlists {
		playlistMap[strconv.Itoa(k)] = v.DeepClone()
	}
	playlistsBytes, err := json.Marshal(playlistMap)
	if err != nil {
		playlistsBytes = []byte("{}")
	}

	matchesClone := make([]*SessionMatchDetail, len(s.matches))
	var latestMatchEnd time.Time
	for i, m := range s.matches {
		matchesClone[i] = m.DeepClone()
		if m.EndedAt.After(latestMatchEnd) {
			latestMatchEnd = m.EndedAt
		}
	}
	matchesBytes, err := json.Marshal(matchesClone)
	if err != nil {
		matchesBytes = []byte("[]")
	}

	var lastMatchPtr *time.Time
	if s.lastMatchAt != nil && !s.lastMatchAt.IsZero() {
		t := s.lastMatchAt.UTC()
		lastMatchPtr = &t
	} else if !latestMatchEnd.IsZero() {
		t := latestMatchEnd.UTC()
		lastMatchPtr = &t
	}

	var endedPtr *time.Time
	if endedAt != nil && !endedAt.IsZero() {
		t := endedAt.UTC()
		endedPtr = &t
	}

	rec := &storage.SessionRecord{
		SessionID:      s.id,
		StartedAt:      s.startedAt.UTC(),
		EndedAt:        endedPtr,
		LastMatchAt:    lastMatchPtr,
		UpdatedAt:      s.now(),
		TotalMatches:   totalMatches,
		TotalWins:      s.totalWins,
		TotalLosses:    s.totalLosses,
		WinRate:        winRate,
		PlaylistsJSON:  string(playlistsBytes),
		MatchesJSON:    string(matchesBytes),
		LocalPlayerIDs: s.getLocalPlayerIDsLocked(),
		Dismissed:      s.dismissedContinueIDs != nil && s.dismissedContinueIDs[s.id],
	}

	if s.archivedMemorySessions == nil {
		s.archivedMemorySessions = make(map[string]*storage.SessionRecord)
	}
	s.archivedMemorySessions[rec.SessionID] = rec
	return rec
}

func (s *SessionTracker) saveRecordToStore(rec *storage.SessionRecord) {
	if rec == nil {
		return
	}
	s.mu.RLock()
	store := s.store
	logger := s.logger
	s.mu.RUnlock()
	if store != nil {
		if err := store.SaveSession(context.Background(), rec); err != nil && logger != nil {
			logger.Warn("failed to persist session record", slog.String("session_id", rec.SessionID), slog.Any("error", err))
		}
	}
}

func (s *SessionTracker) lookupSessionRecord(sessionID string) (*storage.SessionRecord, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, storage.ErrSessionNotFound
	}
	s.mu.RLock()
	store := s.store
	memRec := s.archivedMemorySessions[sessionID]
	s.mu.RUnlock()

	if store != nil {
		if rec, err := store.GetSession(context.Background(), sessionID); err == nil && rec != nil {
			return rec, nil
		} else if err != nil && !errors.Is(err, storage.ErrSessionNotFound) {
			return nil, err
		}
	}
	if memRec != nil {
		return memRec, nil
	}
	return nil, storage.ErrSessionNotFound
}

// ListHistoricalSessions is an alias for ListSessions.
func (s *SessionTracker) ListHistoricalSessions(ctx context.Context, limit, offset int) ([]*SessionResponse, int, error) {
	return s.ListSessions(ctx, limit, offset)
}

// ListSessions returns a paginated list of historical and current persisted sessions.
func (s *SessionTracker) ListSessions(ctx context.Context, limit, offset int) ([]*SessionResponse, int, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	s.mu.RLock()
	store := s.store
	currentID := s.id
	cw := s.continueWindow
	now := s.now()
	var currentFirstMatch time.Time
	if len(s.matches) > 0 {
		currentFirstMatch = s.matches[0].StartedAt
	}
	memTelemetry := s.cloneReplayTelemetryLocked()
	memRecords := make([]*storage.SessionRecord, 0, len(s.archivedMemorySessions))
	for _, r := range s.archivedMemorySessions {
		if r != nil && r.TotalMatches > 0 {
			memRecords = append(memRecords, r)
		}
	}
	s.mu.RUnlock()

	var pageRecs []*storage.SessionRecord
	var total int

	if store != nil {
		recs, count, err := store.ListSessions(ctx, limit, offset)
		if err != nil {
			return nil, 0, err
		}
		pageRecs = recs
		total = count
	} else {
		sortSessionRecordsDesc(memRecords)
		total = len(memRecords)
		if offset >= total {
			return []*SessionResponse{}, total, nil
		}
		end := offset + limit
		if end > total {
			end = total
		}
		pageRecs = memRecords[offset:end]
	}

	out := make([]*SessionResponse, 0, len(pageRecs))
	var allMatches []*SessionMatchDetail
	for _, r := range pageRecs {
		resp, err := decodeSessionResponse(r, cw, currentID, now, currentFirstMatch)
		if err != nil {
			continue
		}
		out = append(out, resp)
		allMatches = append(allMatches, resp.Matches...)
	}

	var histByGUID map[string]*storage.HistoricalMatchRecord
	var cachedLocalIDs []string
	if store != nil {
		if len(allMatches) > 0 {
			histByGUID = batchLoadHistoricalMatches(ctx, store, allMatches)
		}
		cachedLocalIDs, _ = store.GetLocalPlayerIDs(ctx)
	}

	for _, resp := range out {
		resp.MostPlayedPlaylist, resp.ForStats, resp.AgainstStats = computeSessionDerivedStatsWithCache(
			resp.Playlists, resp.Matches, resp.LocalPlayerIDs, store, cachedLocalIDs, histByGUID, memTelemetry,
		)
	}
	return out, total, nil
}

// GetHistoricalSession retrieves a specific session by ID (either the active session or a persisted past session).
func (s *SessionTracker) GetHistoricalSession(ctx context.Context, sessionID string) (*SessionResponse, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, storage.ErrSessionNotFound
	}

	s.mu.RLock()
	currentID := s.id
	cw := s.continueWindow
	now := s.now()
	var currentFirstMatch time.Time
	if len(s.matches) > 0 {
		currentFirstMatch = s.matches[0].StartedAt
	}
	store := s.store
	memRec := s.archivedMemorySessions[sessionID]
	s.mu.RUnlock()

	if sessionID == currentID {
		return s.GetSessionSummary(), nil
	}

	if store != nil {
		rec, err := store.GetSession(ctx, sessionID)
		if err == nil && rec != nil {
			return s.sessionResponseFromRecord(rec, cw, currentID, now, currentFirstMatch)
		}
		if err != nil && !errors.Is(err, storage.ErrSessionNotFound) {
			return nil, err
		}
	}
	if memRec != nil {
		return s.sessionResponseFromRecord(memRec, cw, currentID, now, currentFirstMatch)
	}
	return nil, storage.ErrSessionNotFound
}

// SessionResponseFromRecord reconstructs a SessionResponse from a persisted storage.SessionRecord.
func SessionResponseFromRecord(rec *storage.SessionRecord, continueWindow time.Duration, currentSessionID string, now time.Time) (*SessionResponse, error) {
	resp, err := decodeSessionResponse(rec, continueWindow, currentSessionID, now, time.Time{})
	if err != nil {
		return nil, err
	}
	resp.MostPlayedPlaylist, resp.ForStats, resp.AgainstStats = computeSessionDerivedStats(
		resp.Playlists, resp.Matches, resp.LocalPlayerIDs, nil,
	)
	return resp, nil
}

func (s *SessionTracker) sessionResponseFromRecord(rec *storage.SessionRecord, cw time.Duration, currentSessionID string, now, currentFirstMatch time.Time) (*SessionResponse, error) {
	resp, err := decodeSessionResponse(rec, cw, currentSessionID, now, currentFirstMatch)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	store := s.store
	memTelemetry := s.cloneReplayTelemetryLocked()
	s.mu.RUnlock()

	resp.MostPlayedPlaylist, resp.ForStats, resp.AgainstStats = computeSessionDerivedStatsWithCache(
		resp.Playlists, resp.Matches, resp.LocalPlayerIDs, store, nil, nil, memTelemetry,
	)
	return resp, nil
}

func decodeSessionResponse(rec *storage.SessionRecord, continueWindow time.Duration, currentSessionID string, now, currentFirstMatch time.Time) (*SessionResponse, error) {
	if rec == nil {
		return nil, storage.ErrSessionNotFound
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	playlists := make(map[string]*PlaylistSessionStats)
	if strings.TrimSpace(rec.PlaylistsJSON) != "" {
		_ = json.Unmarshal([]byte(rec.PlaylistsJSON), &playlists)
	}
	if playlists == nil {
		playlists = make(map[string]*PlaylistSessionStats)
	}

	matches := make([]*SessionMatchDetail, 0)
	if strings.TrimSpace(rec.MatchesJSON) != "" {
		_ = json.Unmarshal([]byte(rec.MatchesJSON), &matches)
	}
	if matches == nil {
		matches = make([]*SessionMatchDetail, 0)
	}

	endRef := now
	if rec.EndedAt != nil && !rec.EndedAt.IsZero() {
		endRef = *rec.EndedAt
	} else if rec.SessionID != currentSessionID && rec.LastMatchAt != nil && !rec.LastMatchAt.IsZero() {
		endRef = *rec.LastMatchAt
	}
	uptimeStr := formatUptime(endRef.Sub(rec.StartedAt))

	refTime := recordRefTime(rec)
	canContinue := rec.SessionID != currentSessionID &&
		rec.TotalMatches > 0 &&
		isWithinContinueWindow(refTime, now, currentFirstMatch, continueWindow)

	localIDs := rec.LocalPlayerIDs
	if localIDs == nil {
		localIDs = []string{}
	}

	var endedCopy *time.Time
	if rec.EndedAt != nil && !rec.EndedAt.IsZero() {
		t := rec.EndedAt.UTC()
		endedCopy = &t
	}
	var lastMatchCopy *time.Time
	if rec.LastMatchAt != nil && !rec.LastMatchAt.IsZero() {
		t := rec.LastMatchAt.UTC()
		lastMatchCopy = &t
	}

	return &SessionResponse{
		SessionID:             rec.SessionID,
		StartedAt:             rec.StartedAt.UTC(),
		EndedAt:               endedCopy,
		LastMatchAt:           lastMatchCopy,
		Uptime:                uptimeStr,
		TotalMatches:          rec.TotalMatches,
		TotalWins:             rec.TotalWins,
		TotalLosses:           rec.TotalLosses,
		WinRate:               rec.WinRate,
		Playlists:             playlists,
		Matches:               matches,
		LocalPlayerIDs:        localIDs,
		ContinueWindowSeconds: int(continueWindow.Seconds()),
		CanContinue:           canContinue,
	}, nil
}
