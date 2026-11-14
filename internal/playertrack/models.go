package playertrack

import (
	"log/slog"
	"time"

	"github.com/dank/rl-api-utils/internal/auth"
	"github.com/dank/rl-api-utils/internal/contracts"
	"github.com/dank/rl-api-utils/internal/storage"
)

const (
	defaultRankCacheTTL = 15 * time.Minute
	defaultBackoffTTL   = 60 * time.Second

	maxRankCacheEntries    = 1000
	maxMatchupCacheEntries = 1000
	maxFailBackoffEntries  = 500
)

var defaultPostMatchPollSchedule = []time.Duration{
	2 * time.Second,
	5 * time.Second,
	9 * time.Second,
	15 * time.Second,
	25 * time.Second,
}

// PlayerClassification designates a player's relationship to the local user.
type PlayerClassification = contracts.PlayerClassification

const (
	ClassificationSelf      = contracts.ClassificationSelf
	ClassificationTeammate  = contracts.ClassificationTeammate
	ClassificationOpponent  = contracts.ClassificationOpponent
	ClassificationBot       = contracts.ClassificationBot
	ClassificationSpectator = contracts.ClassificationSpectator
)

// PlayerStatsSummary holds real-time box score counters received from StatsPlayer.
type PlayerStatsSummary = contracts.PlayerStatsSummary

// LobbyPlayer encapsulates identity, real-time match stats, skill ratings, and historical matchup records.
type LobbyPlayer struct {
	PlayerID       string                 `json:"player_id"`                 // PrimaryId (e.g. "Steam|76561198...|0")
	Platform       string                 `json:"platform"`                  // "Steam", "Epic", "Unknown"
	Name           string                 `json:"name"`                      // In-game display name
	TeamNum        int                    `json:"team_num"`                  // 0 = Blue, 1 = Orange, 255 = Spectator
	IsLocal        bool                   `json:"is_local"`                  // True if this is the authenticated user
	IsBot          bool                   `json:"is_bot"`                    // True if AI bot ("Unknown|0|0")
	IsDisconnected bool                   `json:"is_disconnected,omitempty"` // True if player left the active game early
	Stats          PlayerStatsSummary     `json:"stats"`                     // Real-time in-game box score
	CurrentRank    *PlayerPlaylistRank    `json:"current_rank,omitempty"`    // Rank for active match playlist
	Ranks          PlayerRanksSnapshot    `json:"ranks,omitempty"`           // Full snapshot across all playlists
	MatchupRecord  *storage.PlayerMatchup `json:"matchup_record,omitempty"`  // H2H record in this playlist
}

// DeepClone creates a deep copy of a LobbyPlayer to ensure thread safety across reads.
func (p LobbyPlayer) DeepClone() LobbyPlayer {
	clone := p
	if p.CurrentRank != nil {
		cr := *p.CurrentRank
		clone.CurrentRank = &cr
	}
	if p.Ranks != nil {
		clone.Ranks = make(PlayerRanksSnapshot, len(p.Ranks))
		for k, v := range p.Ranks {
			clone.Ranks[k] = v
		}
	}
	if p.MatchupRecord != nil {
		mr := *p.MatchupRecord
		clone.MatchupRecord = &mr
	}
	return clone
}

// ResolvedPlayer holds details of the identified local player.
type ResolvedPlayer struct {
	PrimaryID   string `json:"primary_id"`
	Platform    string `json:"platform"`
	AccountID   string `json:"account_id"`
	PlayerName  string `json:"player_name"`
	TeamNum     int    `json:"team_num"`
	MatchReason string `json:"match_reason"`
}

// CurrentMatchResponse defines the complete JSON payload returned by GET /current-match.
type CurrentMatchResponse struct {
	ActiveMatch    bool          `json:"active_match"`
	MatchEnded     bool          `json:"match_ended"`
	MatchGUID      string        `json:"match_guid"`
	PlaylistID     int           `json:"playlist_id"`
	PlaylistName   string        `json:"playlist_name"`
	TimeRemaining  int           `json:"time_remaining"`             // Ingested match time remaining in seconds (SPEC-09)
	IsOvertime     bool          `json:"is_overtime"`                // True if match in overtime (SPEC-09)
	LocalTeam      *int          `json:"local_team,omitempty"`       // 0 = Blue, 1 = Orange, nil if unresolved/spectator
	LocalPlayer    *LobbyPlayer  `json:"local_player,omitempty"`     // Dedicated reference to the local user
	LocalPlayerIDs []string      `json:"local_player_ids,omitempty"` // Authenticated/known local user IDs (SPEC-03)
	Teammates      []LobbyPlayer `json:"teammates"`                  // Other players on local player's team
	Opponents      []LobbyPlayer `json:"opponents"`                  // Players on opposing team
	Spectators     []LobbyPlayer `json:"spectators,omitempty"`       // Spectators / unassigned
	WinnerTeam     *int          `json:"winner_team,omitempty"`      // Populated once match concludes
	Result         string        `json:"result,omitempty"`           // "victory", "defeat", or "" if in progress
	UpdatedAt      time.Time     `json:"updated_at"`
}

// DeepClone creates a completely isolated copy of CurrentMatchResponse for safe concurrent reads.
func (s *CurrentMatchResponse) DeepClone() *CurrentMatchResponse {
	if s == nil {
		return nil
	}

	clone := &CurrentMatchResponse{
		ActiveMatch:   s.ActiveMatch,
		MatchEnded:    s.MatchEnded,
		MatchGUID:     s.MatchGUID,
		PlaylistID:    s.PlaylistID,
		PlaylistName:  s.PlaylistName,
		TimeRemaining: s.TimeRemaining,
		IsOvertime:    s.IsOvertime,
		Result:        s.Result,
		UpdatedAt:     s.UpdatedAt,
	}

	if s.LocalTeam != nil {
		val := *s.LocalTeam
		clone.LocalTeam = &val
	}
	if s.WinnerTeam != nil {
		val := *s.WinnerTeam
		clone.WinnerTeam = &val
	}
	if s.LocalPlayer != nil {
		lp := s.LocalPlayer.DeepClone()
		clone.LocalPlayer = &lp
	}
	if len(s.LocalPlayerIDs) > 0 {
		clone.LocalPlayerIDs = append([]string(nil), s.LocalPlayerIDs...)
	}

	clone.Teammates = cloneLobbySlice(s.Teammates)
	clone.Opponents = cloneLobbySlice(s.Opponents)
	clone.Spectators = cloneLobbySlice(s.Spectators)

	return clone
}

func cloneLobbySlice(players []LobbyPlayer) []LobbyPlayer {
	out := make([]LobbyPlayer, len(players))
	for i := range players {
		out[i] = players[i].DeepClone()
	}
	return out
}

type cachedRankEntry struct {
	snapshot  PlayerRanksSnapshot
	fetchedAt time.Time
}

// TrackerOption configures Tracker construction.
type TrackerOption func(*Tracker)

// WithTrackerLogger injects a custom structured logger.
func WithTrackerLogger(logger *slog.Logger) TrackerOption {
	return func(t *Tracker) {
		if logger != nil {
			t.logger = logger
		}
	}
}

// WithTrackerAuthProvider injects an AuthProvider to resolve dynamic credentials.
func WithTrackerAuthProvider(provider auth.AuthProvider) TrackerOption {
	return func(t *Tracker) {
		t.authProvider = provider
	}
}

// WithRankCacheTTL configures the rank cache time-to-live.
func WithRankCacheTTL(d time.Duration) TrackerOption {
	return func(t *Tracker) {
		if d > 0 {
			t.rankCacheTTL = d
		}
	}
}

// WithBackoffTTL configures the error backoff time-to-live.
func WithBackoffTTL(d time.Duration) TrackerOption {
	return func(t *Tracker) {
		if d > 0 {
			t.backoffTTL = d
		}
	}
}

// WithPostMatchPollSchedule configures the delayed post-match MMR reconciliation backoff schedule.
func WithPostMatchPollSchedule(schedule []time.Duration) TrackerOption {
	return func(t *Tracker) {
		if len(schedule) > 0 {
			cp := make([]time.Duration, len(schedule))
			copy(cp, schedule)
			t.postMatchPollSchedule = cp
		}
	}
}
