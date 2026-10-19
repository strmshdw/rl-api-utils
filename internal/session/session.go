package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/google/uuid"
)

// DefaultContinueWindow is the default maximum elapsed time since the last match
// of a previous session during which that session can be continued.
const DefaultContinueWindow = 2 * time.Hour

// Sentinel errors for session continuation.
var (
	ErrNoContinuableSession  = errors.New("no continuable session available")
	ErrContinueWindowExpired = errors.New("session continuation window has expired")
)

// Ensure SessionTracker implements playertrack.MatchStateListener at compile time.
var _ playertrack.MatchStateListener = (*SessionTracker)(nil)

// Option configures SessionTracker.
type Option func(*SessionTracker)

// WithLogger sets the structured logger for SessionTracker.
func WithLogger(logger *slog.Logger) Option {
	return func(s *SessionTracker) {
		if logger != nil {
			s.logger = logger
		}
	}
}

// WithBroadcaster injects an EventBroadcaster.
func WithBroadcaster(b *EventBroadcaster) Option {
	return func(s *SessionTracker) {
		if b != nil {
			s.broadcaster = b
		}
	}
}

// WithStateStore attaches persistent storage for session history and continuation.
func WithStateStore(store storage.StateStore) Option {
	return func(s *SessionTracker) {
		if store != nil {
			s.store = store
		}
	}
}

// WithContinueWindow configures the maximum time since the last match of a previous
// session during which that session can be continued (0 disables continuation).
func WithContinueWindow(d time.Duration) Option {
	return func(s *SessionTracker) {
		if d < 0 {
			d = 0
		}
		s.continueWindow = d
		s.continueWindowExplicit = true
	}
}

// WithClock injects a custom time source for deterministic testing.
func WithClock(nowFn func() time.Time) Option {
	return func(s *SessionTracker) {
		if nowFn != nil {
			s.nowFunc = nowFn
		}
	}
}

// SessionTracker coordinates active session statistics, playlist progression,
// match history snapshots, persistent session storage, session continuation,
// and Server-Sent Events push distribution.
type SessionTracker struct {
	mu                     sync.RWMutex
	id                     string
	startedAt              time.Time
	lastMatchAt            *time.Time
	totalWins              int
	totalLosses            int
	playlists              map[int]*PlaylistSessionStats
	matches                []*SessionMatchDetail
	completedGUIDs         map[string]struct{}
	activeMatch            *ActiveMatchRecord
	currentMatch           *playertrack.CurrentMatchResponse
	broadcaster            *EventBroadcaster
	logger                 *slog.Logger
	localPlayerIDs         []string
	store                  storage.StateStore
	continueWindow         time.Duration
	continueWindowExplicit bool
	dismissedContinueIDs   map[string]bool
	archivedMemorySessions map[string]*storage.SessionRecord
	nowFunc                func() time.Time
}

// NewSessionTracker constructs a fresh SessionTracker.
func NewSessionTracker(opts ...Option) *SessionTracker {
	nowFn := func() time.Time { return time.Now().UTC() }
	s := &SessionTracker{
		playlists:              make(map[int]*PlaylistSessionStats),
		matches:                make([]*SessionMatchDetail, 0),
		completedGUIDs:         make(map[string]struct{}),
		broadcaster:            NewEventBroadcaster(),
		logger:                 slog.Default().With(slog.String("component", "session")),
		continueWindow:         DefaultContinueWindow,
		dismissedContinueIDs:   make(map[string]bool),
		archivedMemorySessions: make(map[string]*storage.SessionRecord),
		nowFunc:                nowFn,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}

	s.id = uuid.NewString()
	s.startedAt = s.now()

	return s
}

// NewTracker is an alias for NewSessionTracker.
func NewTracker(opts ...Option) *SessionTracker {
	return NewSessionTracker(opts...)
}

func (s *SessionTracker) now() time.Time {
	if s.nowFunc != nil {
		return s.nowFunc().UTC()
	}
	return time.Now().UTC()
}

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

// SetContinueWindow updates the session continuation window duration.
func (s *SessionTracker) SetContinueWindow(d time.Duration) {
	if d < 0 {
		d = 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.continueWindow = d
}

// HasExplicitContinueWindow reports whether WithContinueWindow was explicitly supplied.
func (s *SessionTracker) HasExplicitContinueWindow() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.continueWindowExplicit
}

// ContinueWindow returns the configured session continuation window duration.
func (s *SessionTracker) ContinueWindow() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.continueWindow
}

// Broadcaster returns the underlying EventBroadcaster instance.
func (s *SessionTracker) Broadcaster() *EventBroadcaster {
	return s.broadcaster
}

// Subscribe registers a new listener channel for Server-Sent Events.
func (s *SessionTracker) Subscribe() (<-chan SessionEvent, func()) {
	return s.broadcaster.Subscribe()
}

// buildSessionRecordLocked constructs a storage.SessionRecord snapshot for the current session.
// Caller must hold s.mu. Returns nil if the current session has 0 completed matches.
func (s *SessionTracker) buildSessionRecordLocked(endedAt *time.Time) *storage.SessionRecord {
	totalMatches := len(s.matches)
	if totalMatches == 0 {
		return nil
	}

	var winRate float64
	if totalMatches > 0 {
		winRate = roundFloat((float64(s.totalWins)/float64(totalMatches))*100.0, 1)
	}

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

// resetEpochLocked initializes a fresh session epoch in memory. Caller must hold s.mu.
func (s *SessionTracker) resetEpochLocked(now time.Time) {
	s.id = uuid.NewString()
	s.startedAt = now
	s.lastMatchAt = nil
	s.totalWins = 0
	s.totalLosses = 0
	s.playlists = make(map[int]*PlaylistSessionStats)
	s.matches = make([]*SessionMatchDetail, 0)
	s.completedGUIDs = make(map[string]struct{})

	// If currently in an active match, re-anchor baseline MMR for active playlist
	if s.activeMatch != nil && s.currentMatch != nil && s.currentMatch.LocalPlayer != nil {
		if cr := s.currentMatch.LocalPlayer.CurrentRank; cr != nil && cr.MMR > 0 {
			s.activeMatch.StartingMMR = cr.MMR
			s.playlists[s.activeMatch.PlaylistID] = &PlaylistSessionStats{
				PlaylistID:   s.activeMatch.PlaylistID,
				PlaylistName: playertrack.FormatPlaylist(s.activeMatch.PlaylistID),
				InitialMMR:   cr.MMR,
				CurrentMMR:   cr.MMR,
				MMRDelta:     0.0,
			}
		}
	}
}

// checkAutoRotateLocked checks if the current session has been idle longer than continueWindow
// since its last match ended. If so, it finalizes the current session and rotates to a new epoch.
// Caller must hold s.mu.
func (s *SessionTracker) checkAutoRotateLocked(now time.Time) *storage.SessionRecord {
	if len(s.matches) == 0 || s.continueWindow <= 0 || s.lastMatchAt == nil || s.lastMatchAt.IsZero() {
		return nil
	}
	if now.Sub(*s.lastMatchAt) <= s.continueWindow {
		return nil
	}
	endedAt := *s.lastMatchAt
	rec := s.buildSessionRecordLocked(&endedAt)
	s.resetEpochLocked(now)
	return rec
}

// Reset re-initializes session statistics and starts a new session epoch
// without disconnecting active SSE clients. Any completed matches in the previous
// session are archived to persistent storage first.
func (s *SessionTracker) Reset() *SessionResponse {
	s.mu.Lock()
	now := s.now()
	archivedRec := s.buildSessionRecordLocked(&now)
	s.resetEpochLocked(now)
	s.mu.Unlock()

	s.saveRecordToStore(archivedRec)

	summary := s.GetSessionSummary()
	if s.broadcaster != nil {
		s.broadcaster.Broadcast(SessionEvent{Event: EventSessionUpdate, Data: summary})
	}
	return summary
}

// OnGameDisconnected notifies SessionTracker that the Rocket League client disconnected.
// If the current session has completed matches, it clears any unfinished active match,
// archives the current session, and rotates to a new session epoch so that reopening
// Rocket League offers to continue the previous session if within the continuation window.
func (s *SessionTracker) OnGameDisconnected() {
	s.mu.Lock()
	if len(s.matches) == 0 {
		s.mu.Unlock()
		return
	}
	s.activeMatch = nil
	s.currentMatch = nil
	now := s.now()
	endedAt := now
	if s.lastMatchAt != nil && !s.lastMatchAt.IsZero() {
		endedAt = *s.lastMatchAt
	}
	archivedRec := s.buildSessionRecordLocked(&endedAt)
	s.resetEpochLocked(now)
	s.mu.Unlock()

	s.saveRecordToStore(archivedRec)

	summary := s.GetSessionSummary()
	if s.broadcaster != nil {
		s.broadcaster.Broadcast(SessionEvent{Event: EventSessionUpdate, Data: summary})
	}
}

// DismissContinuableSession dismisses the continuation banner for a specific previous session
// (or the currently offered continuable session if sessionID is empty), persisting the dismissal
// to StateStore so it survives daemon restarts.
func (s *SessionTracker) DismissContinuableSession(sessionID string) *SessionResponse {
	targetID := strings.TrimSpace(sessionID)
	if targetID == "" {
		currentSummary := s.GetSessionSummary()
		if currentSummary.ContinuableSession != nil {
			targetID = currentSummary.ContinuableSession.SessionID
		}
	}

	var recToPersist *storage.SessionRecord
	if targetID != "" {
		if rec, err := s.lookupSessionRecord(targetID); err == nil && rec != nil {
			recCopy := *rec
			recCopy.Dismissed = true
			recCopy.UpdatedAt = s.now()
			recToPersist = &recCopy
		}
	}

	s.mu.Lock()
	if s.dismissedContinueIDs == nil {
		s.dismissedContinueIDs = make(map[string]bool)
	}
	if targetID != "" {
		s.dismissedContinueIDs[targetID] = true
		if memRec, ok := s.archivedMemorySessions[targetID]; ok && memRec != nil {
			memRec.Dismissed = true
			memRec.UpdatedAt = s.now()
			if recToPersist == nil {
				recCopy := *memRec
				recToPersist = &recCopy
			}
		}
		if recToPersist != nil {
			if s.archivedMemorySessions == nil {
				s.archivedMemorySessions = make(map[string]*storage.SessionRecord)
			}
			s.archivedMemorySessions[targetID] = recToPersist
		}
	}
	s.mu.Unlock()

	if recToPersist != nil {
		s.saveRecordToStore(recToPersist)
	}

	summary := s.GetSessionSummary()
	if s.broadcaster != nil {
		s.broadcaster.Broadcast(SessionEvent{Event: EventSessionUpdate, Data: summary})
	}
	return summary
}

// ContinueSession resumes a previous session (or merges the current session's matches into it)
// if the target session's last game played is within the configured continuation window.
// If targetSessionID is empty, the most recent eligible previous session is continued.
func (s *SessionTracker) ContinueSession(targetSessionID string) (*SessionResponse, error) {
	targetSessionID = strings.TrimSpace(targetSessionID)

	s.mu.RLock()
	currentID := s.id
	cw := s.continueWindow
	now := s.now()
	s.mu.RUnlock()

	if targetSessionID != "" && targetSessionID == currentID {
		return s.GetSessionSummary(), nil
	}

	var targetRec *storage.SessionRecord
	if targetSessionID != "" {
		rec, err := s.lookupSessionRecord(targetSessionID)
		if err != nil {
			return nil, err
		}
		targetRec = rec
	} else {
		rec := s.findContinuableRecord(currentID, cw, now, true)
		if rec == nil {
			// Check if there is any previous session that is simply expired so we return the right error
			if s.hasAnyPreviousSession(currentID) && cw <= 0 {
				return nil, ErrContinueWindowExpired
			}
			if s.hasExpiredPreviousSession(currentID, cw, now) {
				return nil, ErrContinueWindowExpired
			}
			return nil, ErrNoContinuableSession
		}
		targetRec = rec
	}

	if targetRec.TotalMatches <= 0 {
		return nil, ErrNoContinuableSession
	}

	// Verify continuation window constraint
	refTime := targetRec.StartedAt
	if targetRec.LastMatchAt != nil && !targetRec.LastMatchAt.IsZero() {
		refTime = *targetRec.LastMatchAt
	} else if targetRec.EndedAt != nil && !targetRec.EndedAt.IsZero() {
		refTime = *targetRec.EndedAt
	}

	s.mu.Lock()
	var currentFirstMatchStart time.Time
	if len(s.matches) > 0 {
		currentFirstMatchStart = s.matches[0].StartedAt
	}

	withinWindow := cw > 0 && now.Sub(refTime) <= cw
	if !withinWindow && cw > 0 && !currentFirstMatchStart.IsZero() && currentFirstMatchStart.After(refTime) {
		if currentFirstMatchStart.Sub(refTime) <= cw {
			withinWindow = true
		}
	}
	if !withinWindow {
		s.mu.Unlock()
		return nil, ErrContinueWindowExpired
	}

	var prevMatches []*SessionMatchDetail
	if strings.TrimSpace(targetRec.MatchesJSON) != "" {
		_ = json.Unmarshal([]byte(targetRec.MatchesJSON), &prevMatches)
	}
	var prevPlaylists map[string]*PlaylistSessionStats
	if strings.TrimSpace(targetRec.PlaylistsJSON) != "" {
		_ = json.Unmarshal([]byte(targetRec.PlaylistsJSON), &prevPlaylists)
	}

	oldSessionID := s.id
	hadCurrentMatches := len(s.matches) > 0

	// Combine prevMatches and current s.matches without duplicates, ordered chronologically
	combinedMatches := make([]*SessionMatchDetail, 0, len(prevMatches)+len(s.matches))
	seenGUIDs := make(map[string]struct{}, len(prevMatches)+len(s.matches))
	for _, m := range prevMatches {
		if m == nil || strings.TrimSpace(m.MatchGUID) == "" {
			continue
		}
		g := strings.TrimSpace(m.MatchGUID)
		if _, exists := seenGUIDs[g]; exists {
			continue
		}
		seenGUIDs[g] = struct{}{}
		combinedMatches = append(combinedMatches, m.DeepClone())
	}
	for _, m := range s.matches {
		if m == nil || strings.TrimSpace(m.MatchGUID) == "" {
			continue
		}
		g := strings.TrimSpace(m.MatchGUID)
		if _, exists := seenGUIDs[g]; exists {
			continue
		}
		seenGUIDs[g] = struct{}{}
		combinedMatches = append(combinedMatches, m.DeepClone())
	}

	sort.SliceStable(combinedMatches, func(i, j int) bool {
		if !combinedMatches[i].EndedAt.Equal(combinedMatches[j].EndedAt) {
			return combinedMatches[i].EndedAt.Before(combinedMatches[j].EndedAt)
		}
		return combinedMatches[i].StartedAt.Before(combinedMatches[j].StartedAt)
	})

	// Seed initial MMR baselines from prevPlaylists
	newPlaylists := make(map[int]*PlaylistSessionStats)
	for kStr, ps := range prevPlaylists {
		if ps == nil {
			continue
		}
		pid := ps.PlaylistID
		if pid == 0 {
			pid, _ = strconv.Atoi(kStr)
		}
		if pid != 0 && ps.InitialMMR > 0 {
			pName := ps.PlaylistName
			if pName == "" {
				pName = playertrack.FormatPlaylist(pid)
			}
			newPlaylists[pid] = &PlaylistSessionStats{
				PlaylistID:   pid,
				PlaylistName: pName,
				InitialMMR:   ps.InitialMMR,
				CurrentMMR:   ps.InitialMMR,
			}
		}
	}

	var totalWins, totalLosses int
	var latestEnd time.Time
	for _, m := range combinedMatches {
		if m.Result == "victory" {
			totalWins++
		} else if m.Result == "defeat" {
			totalLosses++
		}
		if m.EndedAt.After(latestEnd) {
			latestEnd = m.EndedAt
		}
		if m.PlaylistID != 0 {
			st, ok := newPlaylists[m.PlaylistID]
			pName := m.PlaylistName
			if pName == "" {
				pName = playertrack.FormatPlaylist(m.PlaylistID)
			}
			if !ok {
				st = &PlaylistSessionStats{
					PlaylistID:   m.PlaylistID,
					PlaylistName: pName,
					InitialMMR:   m.StartingMMR,
					CurrentMMR:   m.StartingMMR,
				}
				newPlaylists[m.PlaylistID] = st
			} else {
				if m.StartingMMR == 0 && st.CurrentMMR > 0 {
					m.StartingMMR = st.CurrentMMR
				}
				if st.InitialMMR == 0 && m.StartingMMR > 0 {
					st.InitialMMR = m.StartingMMR
				}
			}
			if m.StartingMMR > 0 && m.EndingMMR > 0 {
				m.MMRChange = roundFloat(m.EndingMMR-m.StartingMMR, 2)
			}
			st.MatchesPlayed++
			if m.Result == "victory" {
				st.Wins++
			} else if m.Result == "defeat" {
				st.Losses++
			}
			if st.MatchesPlayed > 0 {
				st.WinRate = roundFloat((float64(st.Wins)/float64(st.MatchesPlayed))*100.0, 1)
			}
			effMMR := m.EndingMMR
			if effMMR <= 0 {
				effMMR = m.StartingMMR
			}
			if effMMR > 0 {
				st.CurrentMMR = effMMR
				if st.InitialMMR == 0 {
					st.InitialMMR = effMMR
				}
			}
			if st.InitialMMR > 0 && st.CurrentMMR > 0 {
				st.MMRDelta = roundFloat(st.CurrentMMR-st.InitialMMR, 2)
			}
		}
	}

	// Ensure any active in-progress match is reflected in newPlaylists
	if s.activeMatch != nil && s.activeMatch.PlaylistID != 0 {
		pid := s.activeMatch.PlaylistID
		st, ok := newPlaylists[pid]
		pName := playertrack.FormatPlaylist(pid)
		if !ok {
			st = &PlaylistSessionStats{
				PlaylistID:   pid,
				PlaylistName: pName,
				InitialMMR:   s.activeMatch.StartingMMR,
				CurrentMMR:   s.activeMatch.StartingMMR,
			}
			newPlaylists[pid] = st
		} else if s.activeMatch.StartingMMR > 0 {
			st.CurrentMMR = s.activeMatch.StartingMMR
			if st.InitialMMR == 0 {
				st.InitialMMR = s.activeMatch.StartingMMR
			}
			if st.InitialMMR > 0 {
				st.MMRDelta = roundFloat(st.CurrentMMR-st.InitialMMR, 2)
			}
		}
	}

	s.id = targetRec.SessionID
	if !targetRec.StartedAt.IsZero() && (targetRec.StartedAt.Before(s.startedAt) || !hadCurrentMatches) {
		s.startedAt = targetRec.StartedAt.UTC()
	}
	if !latestEnd.IsZero() {
		t := latestEnd.UTC()
		s.lastMatchAt = &t
	} else if targetRec.LastMatchAt != nil && !targetRec.LastMatchAt.IsZero() {
		t := targetRec.LastMatchAt.UTC()
		s.lastMatchAt = &t
	}
	s.totalWins = totalWins
	s.totalLosses = totalLosses
	s.playlists = newPlaylists
	s.matches = combinedMatches
	s.completedGUIDs = seenGUIDs
	if len(s.localPlayerIDs) == 0 && len(targetRec.LocalPlayerIDs) > 0 {
		s.localPlayerIDs = make([]string, len(targetRec.LocalPlayerIDs))
		copy(s.localPlayerIDs, targetRec.LocalPlayerIDs)
	}
	delete(s.dismissedContinueIDs, s.id)
	if hadCurrentMatches && oldSessionID != s.id {
		delete(s.archivedMemorySessions, oldSessionID)
	}

	updatedRec := s.buildSessionRecordLocked(nil)
	store := s.store
	s.mu.Unlock()

	if hadCurrentMatches && oldSessionID != targetRec.SessionID && store != nil {
		_ = store.DeleteSession(context.Background(), oldSessionID)
	}
	s.saveRecordToStore(updatedRec)

	summary := s.GetSessionSummary()
	if s.broadcaster != nil {
		s.broadcaster.Broadcast(SessionEvent{Event: EventSessionUpdate, Data: summary})
	}
	return summary, nil
}

// RecordActiveMatch processes an updated match snapshot from playertrack.
func (s *SessionTracker) RecordActiveMatch(match *playertrack.CurrentMatchResponse) {
	if match == nil || strings.TrimSpace(match.MatchGUID) == "" || match.MatchEnded {
		return
	}

	s.mu.Lock()
	guid := strings.TrimSpace(match.MatchGUID)

	// Idempotency: skip if already concluded or match has ended
	if _, completed := s.completedGUIDs[guid]; completed || match.MatchEnded {
		s.mu.Unlock()
		return
	}

	var rotatedRec *storage.SessionRecord
	if s.activeMatch == nil || s.activeMatch.MatchGUID != guid {
		if s.activeMatch != nil {
			s.activeMatch = nil
			s.currentMatch = nil
		}
		rotatedRec = s.checkAutoRotateLocked(s.now())
	}

	s.currentMatch = match.DeepClone()

	// New match detected: initialize active match tracking
	if s.activeMatch == nil || s.activeMatch.MatchGUID != guid {
		var startingMMR float64
		if match.LocalPlayer != nil && match.LocalPlayer.CurrentRank != nil {
			startingMMR = match.LocalPlayer.CurrentRank.MMR
		}

		s.activeMatch = &ActiveMatchRecord{
			MatchGUID:   guid,
			PlaylistID:  match.PlaylistID,
			StartedAt:   s.now(),
			StartingMMR: startingMMR,
		}

		if match.PlaylistID != 0 {
			stats, exists := s.playlists[match.PlaylistID]
			pName := match.PlaylistName
			if pName == "" {
				pName = playertrack.FormatPlaylist(match.PlaylistID)
			}

			if !exists {
				stats = &PlaylistSessionStats{
					PlaylistID:   match.PlaylistID,
					PlaylistName: pName,
					InitialMMR:   startingMMR,
					CurrentMMR:   startingMMR,
				}
				s.playlists[match.PlaylistID] = stats
			} else if stats.InitialMMR == 0 && startingMMR > 0 {
				stats.InitialMMR = startingMMR
				stats.CurrentMMR = startingMMR
			} else if startingMMR > 0 {
				stats.CurrentMMR = startingMMR
				if stats.InitialMMR > 0 {
					stats.MMRDelta = roundFloat(stats.CurrentMMR-stats.InitialMMR, 2)
				}
			}
		}
	} else {
		// Existing match: update MMR if it was initially zero and is now available
		if match.LocalPlayer != nil && match.LocalPlayer.CurrentRank != nil {
			currMMR := match.LocalPlayer.CurrentRank.MMR
			if currMMR > 0 {
				if s.activeMatch.StartingMMR == 0 {
					s.activeMatch.StartingMMR = currMMR
				}
				if stats, ok := s.playlists[match.PlaylistID]; ok {
					if stats.InitialMMR == 0 {
						stats.InitialMMR = currMMR
					}
					stats.CurrentMMR = currMMR
					if stats.InitialMMR > 0 {
						stats.MMRDelta = roundFloat(stats.CurrentMMR-stats.InitialMMR, 2)
					}
				}
			}
		}
	}

	matchClone := s.currentMatch.DeepClone()
	s.mu.Unlock()

	if rotatedRec != nil {
		s.saveRecordToStore(rotatedRec)
		if s.broadcaster != nil {
			summary := s.GetSessionSummary()
			s.broadcaster.Broadcast(SessionEvent{Event: EventSessionUpdate, Data: summary})
		}
	}

	if s.broadcaster != nil && matchClone != nil {
		s.broadcaster.Broadcast(SessionEvent{Event: EventMatchUpdate, Data: matchClone})
	}
}

// ConcludeMatch records the final match outcome and updates session tallies.
func (s *SessionTracker) ConcludeMatch(match *playertrack.CurrentMatchResponse) {
	if match == nil || strings.TrimSpace(match.MatchGUID) == "" {
		return
	}

	s.mu.Lock()
	guid := strings.TrimSpace(match.MatchGUID)

	// Idempotency: skip if already concluded
	if _, processed := s.completedGUIDs[guid]; processed {
		s.mu.Unlock()
		return
	}

	now := s.now()
	var rotatedRec *storage.SessionRecord
	if s.activeMatch == nil || s.activeMatch.MatchGUID != guid {
		rotatedRec = s.checkAutoRotateLocked(now)
	}

	s.completedGUIDs[guid] = struct{}{}

	endedAt := now
	startedAt := endedAt
	var startingMMR float64

	if s.activeMatch != nil && s.activeMatch.MatchGUID == guid {
		startedAt = s.activeMatch.StartedAt
		startingMMR = s.activeMatch.StartingMMR
		s.activeMatch = nil
		s.currentMatch = nil
	}

	duration := int(endedAt.Sub(startedAt).Seconds())
	if duration < 0 {
		duration = 0
	}

	// Calculate BlueScore & OrangeScore from player box scores
	var blueScore, orangeScore int
	allPlayers := make([]playertrack.LobbyPlayer, 0, len(match.Teammates)+len(match.Opponents)+len(match.Spectators)+1)
	if match.LocalPlayer != nil {
		allPlayers = append(allPlayers, *match.LocalPlayer)
	}
	allPlayers = append(allPlayers, match.Teammates...)
	allPlayers = append(allPlayers, match.Opponents...)
	allPlayers = append(allPlayers, match.Spectators...)

	matchPlayers := make([]SessionMatchPlayer, 0, len(allPlayers))
	seenPlayerIDs := make(map[string]bool)

	for _, lp := range allPlayers {
		pid := strings.TrimSpace(lp.PlayerID)
		if pid != "" {
			if seenPlayerIDs[pid] {
				continue
			}
			seenPlayerIDs[pid] = true
		}

		if lp.TeamNum == 0 {
			blueScore += lp.Stats.Goals
		} else if lp.TeamNum == 1 {
			orangeScore += lp.Stats.Goals
		}

		var rankName string
		var tier, div int
		var mmr float64
		if lp.CurrentRank != nil {
			rankName = lp.CurrentRank.RankName
			tier = lp.CurrentRank.Tier
			div = lp.CurrentRank.Division
			mmr = lp.CurrentRank.MMR
		}

		var won *bool
		if match.WinnerTeam != nil && (lp.TeamNum == 0 || lp.TeamNum == 1) {
			w := (lp.TeamNum == *match.WinnerTeam)
			won = &w
		}

		matchPlayers = append(matchPlayers, SessionMatchPlayer{
			PlayerID:       lp.PlayerID,
			Platform:       lp.Platform,
			Name:           lp.Name,
			TeamNum:        lp.TeamNum,
			IsLocal:        lp.IsLocal,
			IsBot:          lp.IsBot,
			IsDisconnected: lp.IsDisconnected,
			Won:            won,
			Stats:          lp.Stats,
			RankName:       rankName,
			Tier:           tier,
			Division:       div,
			MMR:            mmr,
			MatchupRecord:  lp.MatchupRecord,
		})
	}

	// Resolve result and ending MMR
	result := strings.ToLower(strings.TrimSpace(match.Result))
	if result == "" {
		if match.WinnerTeam != nil && match.LocalTeam != nil {
			if *match.WinnerTeam == -1 {
				result = "draw"
			} else if *match.WinnerTeam == *match.LocalTeam {
				result = "victory"
			} else {
				result = "defeat"
			}
		} else if match.LocalTeam != nil {
			if *match.LocalTeam == 0 {
				if blueScore > orangeScore {
					result = "victory"
				} else if orangeScore > blueScore {
					result = "defeat"
				} else if blueScore == orangeScore && (blueScore > 0 || orangeScore > 0) {
					result = "draw"
				} else {
					result = "unknown"
				}
			} else if *match.LocalTeam == 1 {
				if orangeScore > blueScore {
					result = "victory"
				} else if blueScore > orangeScore {
					result = "defeat"
				} else if blueScore == orangeScore && (blueScore > 0 || orangeScore > 0) {
					result = "draw"
				} else {
					result = "unknown"
				}
			}
		} else {
			if blueScore > orangeScore {
				result = "victory"
			} else if orangeScore > blueScore {
				result = "defeat"
			} else if blueScore == orangeScore && (blueScore > 0 || orangeScore > 0) {
				result = "draw"
			} else {
				result = "draw"
			}
		}
	}

	var endingMMR float64
	if match.LocalPlayer != nil && match.LocalPlayer.CurrentRank != nil {
		endingMMR = match.LocalPlayer.CurrentRank.MMR
	}

	var mmrChange float64
	if startingMMR > 0 && endingMMR > 0 {
		mmrChange = roundFloat(endingMMR-startingMMR, 2)
	}

	// Update session totals if valid victory or defeat
	if result == "victory" {
		s.totalWins++
	} else if result == "defeat" {
		s.totalLosses++
	}

	// Update playlist totals
	if match.PlaylistID != 0 {
		stats, ok := s.playlists[match.PlaylistID]
		pName := match.PlaylistName
		if pName == "" {
			pName = playertrack.FormatPlaylist(match.PlaylistID)
		}

		if !ok {
			stats = &PlaylistSessionStats{
				PlaylistID:   match.PlaylistID,
				PlaylistName: pName,
				InitialMMR:   startingMMR,
			}
			s.playlists[match.PlaylistID] = stats
		}

		stats.MatchesPlayed++
		if result == "victory" {
			stats.Wins++
		} else if result == "defeat" {
			stats.Losses++
		}
		if stats.MatchesPlayed > 0 {
			stats.WinRate = roundFloat((float64(stats.Wins)/float64(stats.MatchesPlayed))*100.0, 1)
		}
		if endingMMR > 0 {
			stats.CurrentMMR = endingMMR
			if stats.InitialMMR == 0 {
				stats.InitialMMR = endingMMR
			}
		}
		if stats.InitialMMR > 0 && stats.CurrentMMR > 0 {
			stats.MMRDelta = roundFloat(stats.CurrentMMR-stats.InitialMMR, 2)
		}
	}

	pName := match.PlaylistName
	if pName == "" && match.PlaylistID != 0 {
		pName = playertrack.FormatPlaylist(match.PlaylistID)
	}

	detail := &SessionMatchDetail{
		MatchGUID:       guid,
		PlaylistID:      match.PlaylistID,
		PlaylistName:    pName,
		StartedAt:       startedAt,
		EndedAt:         endedAt,
		DurationSeconds: duration,
		Result:          result,
		LocalTeam:       match.LocalTeam,
		WinnerTeam:      match.WinnerTeam,
		BlueScore:       blueScore,
		OrangeScore:     orangeScore,
		StartingMMR:     startingMMR,
		EndingMMR:       endingMMR,
		MMRChange:       mmrChange,
		Players:         matchPlayers,
	}

	s.matches = append(s.matches, detail)
	s.lastMatchAt = &endedAt
	detailClone := detail.DeepClone()
	currentRec := s.buildSessionRecordLocked(nil)
	s.mu.Unlock()

	if rotatedRec != nil {
		s.saveRecordToStore(rotatedRec)
	}
	s.saveRecordToStore(currentRec)

	summary := s.GetSessionSummary()

	// Broadcast notifications outside the mutex lock
	if s.broadcaster != nil {
		s.broadcaster.Broadcast(SessionEvent{Event: EventMatchEnded, Data: detailClone})
		s.broadcaster.Broadcast(SessionEvent{Event: EventSessionUpdate, Data: summary})
	}
}

// GetSessionSummary returns a thread-safe deep clone of the current session state,
// enriched with ContinuableSession if a previous session is within the continuation window.
func (s *SessionTracker) GetSessionSummary() *SessionResponse {
	s.mu.RLock()
	id := s.id
	startedAt := s.startedAt
	var lastMatchAt *time.Time
	if s.lastMatchAt != nil && !s.lastMatchAt.IsZero() {
		t := *s.lastMatchAt
		lastMatchAt = &t
	}
	totalMatches := len(s.matches)
	var winRate float64
	if totalMatches > 0 {
		winRate = roundFloat((float64(s.totalWins)/float64(totalMatches))*100.0, 1)
	}

	playlistClones := make(map[string]*PlaylistSessionStats, len(s.playlists))
	for k, v := range s.playlists {
		playlistClones[strconv.Itoa(k)] = v.DeepClone()
	}

	matchClones := make([]*SessionMatchDetail, len(s.matches))
	for i, m := range s.matches {
		matchClones[i] = m.DeepClone()
	}

	var activeClone *playertrack.CurrentMatchResponse
	if s.activeMatch != nil && s.currentMatch != nil {
		activeClone = s.currentMatch.DeepClone()
	}

	now := s.now()
	uptimeDuration := now.Sub(startedAt)
	if uptimeDuration < 0 {
		uptimeDuration = 0
	}
	hours := int(uptimeDuration.Hours())
	mins := int(uptimeDuration.Minutes()) % 60
	uptimeStr := fmt.Sprintf("%dh %02dm", hours, mins)

	cw := s.continueWindow
	localIDs := s.getLocalPlayerIDsLocked()
	totalWins := s.totalWins
	totalLosses := s.totalLosses
	var currentFirstMatch time.Time
	if len(s.matches) > 0 {
		currentFirstMatch = s.matches[0].StartedAt
	}
	s.mu.RUnlock()

	var continuableResp *SessionResponse
	if cw > 0 {
		if contRec := s.findContinuableRecord(id, cw, now, false); contRec != nil {
			if resp, err := s.sessionResponseFromRecord(contRec, cw, id, now, currentFirstMatch); err == nil {
				resp.ContinuableSession = nil
				continuableResp = resp
			}
		}
	}

	return &SessionResponse{
		SessionID:             id,
		StartedAt:             startedAt,
		LastMatchAt:           lastMatchAt,
		Uptime:                uptimeStr,
		TotalMatches:          totalMatches,
		TotalWins:             totalWins,
		TotalLosses:           totalLosses,
		WinRate:               winRate,
		Playlists:             playlistClones,
		Matches:               matchClones,
		ActiveMatch:           activeClone,
		LocalPlayerIDs:        localIDs,
		ContinuableSession:    continuableResp,
		ContinueWindowSeconds: int(cw.Seconds()),
	}
}

func (s *SessionTracker) sessionResponseFromRecord(rec *storage.SessionRecord, cw time.Duration, currentSessionID string, now, currentFirstMatch time.Time) (*SessionResponse, error) {
	resp, err := SessionResponseFromRecord(rec, cw, currentSessionID, now)
	if err != nil {
		return nil, err
	}
	if !resp.CanContinue && rec.SessionID != currentSessionID && rec.TotalMatches > 0 && cw > 0 && !currentFirstMatch.IsZero() {
		refTime := rec.StartedAt
		if rec.LastMatchAt != nil && !rec.LastMatchAt.IsZero() {
			refTime = *rec.LastMatchAt
		} else if rec.EndedAt != nil && !rec.EndedAt.IsZero() {
			refTime = *rec.EndedAt
		}
		if currentFirstMatch.After(refTime) && currentFirstMatch.Sub(refTime) <= cw {
			resp.CanContinue = true
		}
	}
	return resp, nil
}

func (s *SessionTracker) collectCandidateRecords(excludeSessionID string) []*storage.SessionRecord {
	s.mu.RLock()
	store := s.store
	memMap := make(map[string]*storage.SessionRecord, len(s.archivedMemorySessions))
	for k, v := range s.archivedMemorySessions {
		if k != excludeSessionID && v != nil {
			memMap[k] = v
		}
	}
	s.mu.RUnlock()

	if store != nil {
		if recs, _, err := store.ListSessions(context.Background(), 25, 0); err == nil {
			for _, r := range recs {
				if r != nil && r.SessionID != excludeSessionID {
					memMap[r.SessionID] = r
				}
			}
		}
	}

	candidates := make([]*storage.SessionRecord, 0, len(memMap))
	for _, r := range memMap {
		if r.TotalMatches > 0 {
			candidates = append(candidates, r)
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		ti := candidates[i].StartedAt
		if candidates[i].LastMatchAt != nil && !candidates[i].LastMatchAt.IsZero() {
			ti = *candidates[i].LastMatchAt
		}
		tj := candidates[j].StartedAt
		if candidates[j].LastMatchAt != nil && !candidates[j].LastMatchAt.IsZero() {
			tj = *candidates[j].LastMatchAt
		}
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return candidates[i].StartedAt.After(candidates[j].StartedAt)
	})
	return candidates
}

func (s *SessionTracker) findContinuableRecord(currentSessionID string, cw time.Duration, now time.Time, ignoreDismissed bool) *storage.SessionRecord {
	if cw <= 0 {
		return nil
	}
	candidates := s.collectCandidateRecords(currentSessionID)

	s.mu.RLock()
	dismissed := make(map[string]bool, len(s.dismissedContinueIDs))
	for k, v := range s.dismissedContinueIDs {
		dismissed[k] = v
	}
	var currentFirstMatch time.Time
	if len(s.matches) > 0 {
		currentFirstMatch = s.matches[0].StartedAt
	}
	s.mu.RUnlock()

	for _, rec := range candidates {
		if !ignoreDismissed && (dismissed[rec.SessionID] || rec.Dismissed) {
			continue
		}
		refTime := rec.StartedAt
		if rec.LastMatchAt != nil && !rec.LastMatchAt.IsZero() {
			refTime = *rec.LastMatchAt
		} else if rec.EndedAt != nil && !rec.EndedAt.IsZero() {
			refTime = *rec.EndedAt
		}
		if now.Sub(refTime) <= cw {
			return rec
		}
		if !currentFirstMatch.IsZero() && currentFirstMatch.After(refTime) && currentFirstMatch.Sub(refTime) <= cw {
			return rec
		}
	}
	return nil
}

func (s *SessionTracker) hasAnyPreviousSession(currentSessionID string) bool {
	return len(s.collectCandidateRecords(currentSessionID)) > 0
}

func (s *SessionTracker) hasExpiredPreviousSession(currentSessionID string, cw time.Duration, now time.Time) bool {
	candidates := s.collectCandidateRecords(currentSessionID)
	s.mu.RLock()
	var currentFirstMatch time.Time
	if len(s.matches) > 0 {
		currentFirstMatch = s.matches[0].StartedAt
	}
	s.mu.RUnlock()

	for _, rec := range candidates {
		refTime := rec.StartedAt
		if rec.LastMatchAt != nil && !rec.LastMatchAt.IsZero() {
			refTime = *rec.LastMatchAt
		} else if rec.EndedAt != nil && !rec.EndedAt.IsZero() {
			refTime = *rec.EndedAt
		}
		carryValid := cw > 0 && !currentFirstMatch.IsZero() && currentFirstMatch.After(refTime) && currentFirstMatch.Sub(refTime) <= cw
		if cw <= 0 || (now.Sub(refTime) > cw && !carryValid) {
			return true
		}
	}
	return false
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
	memRecords := make([]*storage.SessionRecord, 0, len(s.archivedMemorySessions))
	for _, r := range s.archivedMemorySessions {
		if r != nil && r.TotalMatches > 0 {
			memRecords = append(memRecords, r)
		}
	}
	s.mu.RUnlock()

	if store != nil {
		recs, total, err := store.ListSessions(ctx, limit, offset)
		if err != nil {
			return nil, 0, err
		}
		out := make([]*SessionResponse, 0, len(recs))
		for _, r := range recs {
			if resp, convErr := s.sessionResponseFromRecord(r, cw, currentID, now, currentFirstMatch); convErr == nil {
				out = append(out, resp)
			}
		}
		return out, total, nil
	}

	sort.Slice(memRecords, func(i, j int) bool {
		ti := memRecords[i].StartedAt
		if memRecords[i].LastMatchAt != nil && !memRecords[i].LastMatchAt.IsZero() {
			ti = *memRecords[i].LastMatchAt
		}
		tj := memRecords[j].StartedAt
		if memRecords[j].LastMatchAt != nil && !memRecords[j].LastMatchAt.IsZero() {
			tj = *memRecords[j].LastMatchAt
		}
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return memRecords[i].StartedAt.After(memRecords[j].StartedAt)
	})

	total := len(memRecords)
	if offset >= total {
		return []*SessionResponse{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	out := make([]*SessionResponse, 0, end-offset)
	for _, r := range memRecords[offset:end] {
		if resp, err := s.sessionResponseFromRecord(r, cw, currentID, now, currentFirstMatch); err == nil {
			out = append(out, resp)
		}
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

	uptimeDur := endRef.Sub(rec.StartedAt)
	if uptimeDur < 0 {
		uptimeDur = 0
	}
	hours := int(uptimeDur.Hours())
	mins := int(uptimeDur.Minutes()) % 60
	uptimeStr := fmt.Sprintf("%dh %02dm", hours, mins)

	refTime := rec.StartedAt
	if rec.LastMatchAt != nil && !rec.LastMatchAt.IsZero() {
		refTime = *rec.LastMatchAt
	} else if rec.EndedAt != nil && !rec.EndedAt.IsZero() {
		refTime = *rec.EndedAt
	}

	canContinue := rec.SessionID != currentSessionID &&
		rec.TotalMatches > 0 &&
		continueWindow > 0 &&
		now.Sub(refTime) <= continueWindow

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

// SetLocalPlayerIDs sets the local player IDs known to the daemon.
func (s *SessionTracker) SetLocalPlayerIDs(ids []string) {
	s.mu.Lock()
	s.localPlayerIDs = make([]string, len(ids))
	copy(s.localPlayerIDs, ids)
	rec := s.buildSessionRecordLocked(nil)
	s.mu.Unlock()
	s.saveRecordToStore(rec)
}

// GetLocalPlayerIDs returns the currently known local player IDs.
func (s *SessionTracker) GetLocalPlayerIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getLocalPlayerIDsLocked()
}

func (s *SessionTracker) getLocalPlayerIDsLocked() []string {
	if s.localPlayerIDs == nil {
		return []string{}
	}
	res := make([]string, len(s.localPlayerIDs))
	copy(res, s.localPlayerIDs)
	return res
}

// UpdateMatchEndingMMR updates the ending MMR and recalculated MMR delta for a concluded match.
func (s *SessionTracker) UpdateMatchEndingMMR(matchGUID string, endingMMR float64) {
	if strings.TrimSpace(matchGUID) == "" || endingMMR <= 0 {
		return
	}

	s.mu.Lock()
	var targetMatch *SessionMatchDetail
	for _, m := range s.matches {
		if m.MatchGUID == matchGUID {
			targetMatch = m
			break
		}
	}

	if targetMatch == nil {
		s.mu.Unlock()
		return
	}

	targetMatch.EndingMMR = endingMMR
	if targetMatch.StartingMMR > 0 {
		targetMatch.MMRChange = roundFloat(endingMMR-targetMatch.StartingMMR, 2)
	}

	if stats, ok := s.playlists[targetMatch.PlaylistID]; ok {
		stats.CurrentMMR = endingMMR
		if stats.InitialMMR > 0 {
			stats.MMRDelta = roundFloat(endingMMR-stats.InitialMMR, 2)
		}
	}
	rec := s.buildSessionRecordLocked(nil)
	s.mu.Unlock()

	s.saveRecordToStore(rec)

	summary := s.GetSessionSummary()
	if s.broadcaster != nil {
		s.broadcaster.Broadcast(SessionEvent{Event: EventSessionUpdate, Data: summary})
	}
}

// OnActiveMatchUpdated implements playertrack.MatchStateListener.
func (s *SessionTracker) OnActiveMatchUpdated(match *playertrack.CurrentMatchResponse) {
	s.RecordActiveMatch(match)
}

// OnMatchConcluded implements playertrack.MatchStateListener.
func (s *SessionTracker) OnMatchConcluded(match *playertrack.CurrentMatchResponse) {
	s.ConcludeMatch(match)
}

func roundFloat(val float64, precision int) float64 {
	ratio := math.Pow(10, float64(precision))
	return math.Round(val*ratio) / ratio
}

