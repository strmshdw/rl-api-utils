package session

import (
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/google/uuid"
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

// SessionTracker coordinates active session statistics, playlist progression,
// match history snapshots, and Server-Sent Events push distribution.
type SessionTracker struct {
	mu             sync.RWMutex
	id             string
	startedAt      time.Time
	totalWins      int
	totalLosses    int
	playlists      map[int]*PlaylistSessionStats
	matches        []*SessionMatchDetail
	completedGUIDs map[string]struct{}
	activeMatch    *ActiveMatchRecord
	currentMatch   *playertrack.CurrentMatchResponse
	broadcaster    *EventBroadcaster
	logger         *slog.Logger
}

// NewSessionTracker constructs a fresh SessionTracker.
func NewSessionTracker(opts ...Option) *SessionTracker {
	s := &SessionTracker{
		id:             uuid.NewString(),
		startedAt:      time.Now().UTC(),
		playlists:      make(map[int]*PlaylistSessionStats),
		matches:        make([]*SessionMatchDetail, 0),
		completedGUIDs: make(map[string]struct{}),
		broadcaster:    NewEventBroadcaster(),
		logger:         slog.Default().With(slog.String("component", "session")),
	}

	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}

	return s
}

// NewTracker is an alias for NewSessionTracker.
func NewTracker(opts ...Option) *SessionTracker {
	return NewSessionTracker(opts...)
}

// Broadcaster returns the underlying EventBroadcaster instance.
func (s *SessionTracker) Broadcaster() *EventBroadcaster {
	return s.broadcaster
}

// Subscribe registers a new listener channel for Server-Sent Events.
func (s *SessionTracker) Subscribe() (<-chan SessionEvent, func()) {
	return s.broadcaster.Subscribe()
}

// Reset re-initializes session statistics and starts a new session epoch
// without disconnecting active SSE clients.
func (s *SessionTracker) Reset() *SessionResponse {
	s.mu.Lock()
	s.id = uuid.NewString()
	s.startedAt = time.Now().UTC()
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
	s.mu.Unlock()

	summary := s.GetSessionSummary()
	if s.broadcaster != nil {
		s.broadcaster.Broadcast(SessionEvent{Event: EventSessionUpdate, Data: summary})
	}
	return summary
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
			StartedAt:   time.Now().UTC(),
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
	s.completedGUIDs[guid] = struct{}{}

	endedAt := time.Now().UTC()
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

		matchPlayers = append(matchPlayers, SessionMatchPlayer{
			PlayerID:      lp.PlayerID,
			Platform:      lp.Platform,
			Name:          lp.Name,
			TeamNum:       lp.TeamNum,
			IsLocal:       lp.IsLocal,
			IsBot:         lp.IsBot,
			Stats:         lp.Stats,
			RankName:      rankName,
			Tier:          tier,
			Division:      div,
			MMR:           mmr,
			MatchupRecord: lp.MatchupRecord,
		})
	}

	// Resolve result and ending MMR
	result := strings.ToLower(strings.TrimSpace(match.Result))
	if result == "" {
		if match.WinnerTeam != nil && match.LocalTeam != nil {
			if *match.WinnerTeam == *match.LocalTeam {
				result = "victory"
			} else {
				result = "defeat"
			}
		} else {
			result = "draw"
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
	detailClone := detail.DeepClone()
	s.mu.Unlock()

	summary := s.GetSessionSummary()

	// Broadcast notifications outside the mutex lock
	if s.broadcaster != nil {
		s.broadcaster.Broadcast(SessionEvent{Event: EventMatchEnded, Data: detailClone})
		s.broadcaster.Broadcast(SessionEvent{Event: EventSessionUpdate, Data: summary})
	}
}

// GetSessionSummary returns a thread-safe deep clone of the current session state.
func (s *SessionTracker) GetSessionSummary() *SessionResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()

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

	uptimeDuration := time.Since(s.startedAt)
	hours := int(uptimeDuration.Hours())
	mins := int(uptimeDuration.Minutes()) % 60
	uptimeStr := fmt.Sprintf("%dh %02dm", hours, mins)

	return &SessionResponse{
		SessionID:    s.id,
		StartedAt:    s.startedAt,
		Uptime:       uptimeStr,
		TotalMatches: totalMatches,
		TotalWins:    s.totalWins,
		TotalLosses:  s.totalLosses,
		WinRate:      winRate,
		Playlists:    playlistClones,
		Matches:      matchClones,
		ActiveMatch:  activeClone,
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
