package session

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dank/rl-api-utils/internal/contracts"
	"github.com/dank/rl-api-utils/internal/domain"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/google/uuid"
)

// Ensure SessionTracker implements playertrack.MatchStateListener and PostMatchMMRReconciler at compile time.
var (
	_ playertrack.MatchStateListener     = (*SessionTracker)(nil)
	_ playertrack.PostMatchMMRReconciler = (*SessionTracker)(nil)
)

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
	replayTelemetry        map[string]*storage.ReplayMatchStats
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
	summaryEnricher        func(*SessionResponse) *SessionResponse
}

// NewSessionTracker constructs a fresh SessionTracker.
func NewSessionTracker(opts ...Option) *SessionTracker {
	s := &SessionTracker{
		playlists:              make(map[int]*PlaylistSessionStats),
		matches:                make([]*SessionMatchDetail, 0),
		completedGUIDs:         make(map[string]struct{}),
		replayTelemetry:        make(map[string]*storage.ReplayMatchStats),
		broadcaster:            NewEventBroadcaster(),
		logger:                 slog.Default().With(slog.String("component", "session")),
		continueWindow:         DefaultContinueWindow,
		dismissedContinueIDs:   make(map[string]bool),
		archivedMemorySessions: make(map[string]*storage.SessionRecord),
		nowFunc:                func() time.Time { return time.Now().UTC() },
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

	// If currently in an active match, re-anchor baseline MMR for active playlist.
	if s.activeMatch != nil && s.currentMatch != nil && s.currentMatch.LocalPlayer != nil {
		if cr := s.currentMatch.LocalPlayer.CurrentRank; cr != nil && cr.MMR > 0 {
			s.activeMatch.StartingMMR = cr.MMR
			s.playlists[s.activeMatch.PlaylistID] = &PlaylistSessionStats{
				PlaylistID:   s.activeMatch.PlaylistID,
				PlaylistName: domain.FormatPlaylist(s.activeMatch.PlaylistID),
				InitialMMR:   cr.MMR,
				CurrentMMR:   cr.MMR,
				MMRDelta:     0.0,
			}
		}
	}
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
	return s.broadcastSummaryUpdate()
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
	s.broadcastSummaryUpdate()
}

// RecordActiveMatch processes an updated match snapshot from playertrack.
func (s *SessionTracker) RecordActiveMatch(match *playertrack.CurrentMatchResponse) {
	if match == nil || strings.TrimSpace(match.MatchGUID) == "" || match.MatchEnded {
		return
	}

	s.mu.Lock()
	guid := strings.TrimSpace(match.MatchGUID)
	if _, completed := s.completedGUIDs[guid]; completed {
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

	var currMMR float64
	if match.LocalPlayer != nil && match.LocalPlayer.CurrentRank != nil {
		currMMR = match.LocalPlayer.CurrentRank.MMR
	}

	if s.activeMatch == nil || s.activeMatch.MatchGUID != guid {
		s.activeMatch = &ActiveMatchRecord{
			MatchGUID:   guid,
			PlaylistID:  match.PlaylistID,
			StartedAt:   s.now(),
			StartingMMR: currMMR,
		}
		if match.PlaylistID != 0 {
			stats := ensurePlaylistStatsLocked(s.playlists, match.PlaylistID, match.PlaylistName, currMMR, currMMR)
			if currMMR > 0 {
				applyPlaylistCurrentMMRLocked(stats, currMMR)
			}
		}
	} else if currMMR > 0 {
		if s.activeMatch.StartingMMR == 0 {
			s.activeMatch.StartingMMR = currMMR
		}
		if stats, ok := s.playlists[match.PlaylistID]; ok {
			applyPlaylistCurrentMMRLocked(stats, currMMR)
		}
	}

	matchClone := s.currentMatch.DeepClone()
	s.mu.Unlock()

	if rotatedRec != nil {
		s.saveRecordToStore(rotatedRec)
		s.broadcastSummaryUpdate()
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

	matchPlayers, blueScore, orangeScore := concludeMatchPlayers(match)
	result := resolveMatchResult(match, blueScore, orangeScore)

	var endingMMR float64
	if match.LocalPlayer != nil && match.LocalPlayer.CurrentRank != nil {
		endingMMR = match.LocalPlayer.CurrentRank.MMR
	}
	mmrChange := domain.ComputeMMRDelta(startingMMR, endingMMR)

	if result == "victory" {
		s.totalWins++
	} else if result == "defeat" {
		s.totalLosses++
	}

	if match.PlaylistID != 0 {
		stats := ensurePlaylistStatsLocked(s.playlists, match.PlaylistID, match.PlaylistName, startingMMR, 0)
		recordPlaylistOutcomeLocked(stats, result, endingMMR)
	}

	detail := &SessionMatchDetail{
		MatchGUID:       guid,
		PlaylistID:      match.PlaylistID,
		PlaylistName:    resolvePlaylistName(match.PlaylistID, match.PlaylistName),
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

	summary := s.enrichedSummary()
	if s.broadcaster != nil {
		s.broadcaster.Broadcast(SessionEvent{Event: EventMatchEnded, Data: detailClone})
		s.broadcaster.Broadcast(SessionEvent{Event: EventSessionUpdate, Data: summary})
	}
}

// OnClockUpdated updates live match clock state and emits an EventClockUpdated SSE frame.
func (s *SessionTracker) OnClockUpdated(clock *contracts.MatchClock) {
	if clock == nil {
		return
	}
	s.mu.Lock()
	if s.currentMatch != nil {
		s.currentMatch.TimeRemaining = clock.SecondsRemaining
		s.currentMatch.IsOvertime = clock.IsOvertime
	}
	broadcaster := s.broadcaster
	s.mu.Unlock()

	if broadcaster != nil {
		broadcaster.Broadcast(SessionEvent{
			Event: EventClockUpdated,
			Data: contracts.MatchClock{
				SecondsRemaining: clock.SecondsRemaining,
				IsOvertime:       clock.IsOvertime,
			},
		})
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
	winRate := domain.ComputeWinRate(s.totalWins, totalMatches)

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
	uptimeStr := formatUptime(now.Sub(startedAt))
	cw := s.continueWindow
	localIDs := s.getLocalPlayerIDsLocked()
	store := s.store
	totalWins := s.totalWins
	totalLosses := s.totalLosses
	var currentFirstMatch time.Time
	if len(s.matches) > 0 {
		currentFirstMatch = s.matches[0].StartedAt
	}
	memTelemetry := s.cloneReplayTelemetryLocked()
	s.mu.RUnlock()

	var continuableResp *SessionResponse
	if cw > 0 {
		if contRec := s.findContinuableRecord(id, cw, now, false); contRec != nil {
			if resp, err := decodeSessionResponse(contRec, cw, id, now, currentFirstMatch); err == nil {
				continuableResp = resp
			}
		}
	}

	var histByGUID map[string]*storage.HistoricalMatchRecord
	var cachedLocalIDs []string
	if store != nil {
		allMatches := make([]*SessionMatchDetail, 0, len(matchClones))
		allMatches = append(allMatches, matchClones...)
		if continuableResp != nil {
			allMatches = append(allMatches, continuableResp.Matches...)
		}
		if len(allMatches) > 0 {
			histByGUID = batchLoadHistoricalMatches(context.Background(), store, allMatches)
		}
		cachedLocalIDs, _ = store.GetLocalPlayerIDs(context.Background())
	}

	if continuableResp != nil {
		continuableResp.MostPlayedPlaylist, continuableResp.ForStats, continuableResp.AgainstStats = computeSessionDerivedStatsWithCache(
			continuableResp.Playlists, continuableResp.Matches, continuableResp.LocalPlayerIDs, store, cachedLocalIDs, histByGUID, memTelemetry,
		)
	}

	mostPlayed, forStats, againstStats := computeSessionDerivedStatsWithCache(
		playlistClones, matchClones, localIDs, store, cachedLocalIDs, histByGUID, memTelemetry,
	)

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
		MostPlayedPlaylist:    mostPlayed,
		ForStats:              forStats,
		AgainstStats:          againstStats,
		Matches:               matchClones,
		ActiveMatch:           activeClone,
		LocalPlayerIDs:        localIDs,
		ContinuableSession:    continuableResp,
		ContinueWindowSeconds: int(cw.Seconds()),
	}
}

// OnActiveMatchUpdated implements playertrack.MatchStateListener.
func (s *SessionTracker) OnActiveMatchUpdated(match *playertrack.CurrentMatchResponse) {
	s.RecordActiveMatch(match)
}

// OnMatchStateUpdated is an alias for RecordActiveMatch.
func (s *SessionTracker) OnMatchStateUpdated(match *playertrack.CurrentMatchResponse) {
	s.RecordActiveMatch(match)
}

// OnMatchConcluded implements playertrack.MatchStateListener.
func (s *SessionTracker) OnMatchConcluded(match *playertrack.CurrentMatchResponse) {
	s.ConcludeMatch(match)
}

// OnMatchEnded is an alias for ConcludeMatch.
func (s *SessionTracker) OnMatchEnded(match *playertrack.CurrentMatchResponse) {
	s.ConcludeMatch(match)
}
