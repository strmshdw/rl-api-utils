package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/dank/rl-api-utils/internal/contracts"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/storage"
)

// DefaultContinueWindow is the default maximum elapsed time since the last match
// of a previous session during which that session can be continued.
const DefaultContinueWindow = 2 * time.Hour

// Sentinel errors for session continuation.
var (
	ErrNoContinuableSession  = errors.New("no continuable session available")
	ErrContinueWindowExpired = errors.New("session continuation window has expired")
)

// EventType defines valid Server-Sent Events identifiers.
type EventType = string

const (
	// EventMatchUpdate is emitted when active match telemetry, rosters, or stats change.
	EventMatchUpdate EventType = string(contracts.EventMatchUpdate)
	// EventMatchEnded is emitted when a match concludes and final scores are recorded.
	EventMatchEnded EventType = string(contracts.EventMatchEnded)
	// EventClockUpdated is emitted when live game clock or overtime status changes.
	EventClockUpdated EventType = string(contracts.EventClockUpdated)
	// EventSessionUpdate is emitted when session-level statistics or history update.
	EventSessionUpdate EventType = string(contracts.EventSessionUpdate)
	// EventSyncProgress is emitted when background replay/Ballchasing sync starts, progresses, or completes.
	EventSyncProgress EventType = string(contracts.EventSyncProgress)
)

// MostPlayedPlaylistSummary summarizes the most-played playlist in a session.
type MostPlayedPlaylistSummary struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	NetMMR       float64 `json:"net_mmr"`
	Wins         int     `json:"wins"`
	Losses       int     `json:"losses"`
	MatchesCount int     `json:"matches_count"`
}

// DeepClone returns an isolated copy of MostPlayedPlaylistSummary.
func (m *MostPlayedPlaylistSummary) DeepClone() *MostPlayedPlaylistSummary {
	if m == nil {
		return nil
	}
	clone := *m
	return &clone
}

// SessionEvent represents an event envelope passed through internal broadcaster channels and SSE.
type SessionEvent struct {
	Event string `json:"event"`
	Data  any    `json:"data"`
}

// FormatSSE serializes the SessionEvent into standard Server-Sent Events wire format:
// event: <event_name>\n
// data: <json_payload>\n\n
func (e SessionEvent) FormatSSE() ([]byte, error) {
	payload, err := json.Marshal(e.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event data: %w", err)
	}

	var buf bytes.Buffer
	buf.WriteString("event: ")
	buf.WriteString(e.Event)
	buf.WriteString("\ndata: ")
	buf.Write(payload)
	buf.WriteString("\n\n")
	return buf.Bytes(), nil
}

// PlaylistSessionStats tracks cumulative performance and MMR progression for a single playlist.
type PlaylistSessionStats struct {
	PlaylistID    int     `json:"playlist_id"`
	PlaylistName  string  `json:"playlist_name"`
	MatchesPlayed int     `json:"matches_played"`
	Wins          int     `json:"wins"`
	Losses        int     `json:"losses"`
	WinRate       float64 `json:"win_rate"`    // Percentage (0.0 - 100.0, rounded to 1 decimal place)
	InitialMMR    float64 `json:"initial_mmr"` // Baseline MMR recorded on first encounter in session
	CurrentMMR    float64 `json:"current_mmr"` // Latest known MMR
	MMRDelta      float64 `json:"mmr_delta"`   // Net change: CurrentMMR - InitialMMR (rounded to 2 decimal places)
}

// DeepClone returns an isolated copy of PlaylistSessionStats.
func (p *PlaylistSessionStats) DeepClone() *PlaylistSessionStats {
	if p == nil {
		return nil
	}
	clone := *p
	return &clone
}

// SessionMatchPlayer captures an individual player's performance and credentials in a completed match.
type SessionMatchPlayer struct {
	PlayerID       string                         `json:"player_id"`
	Platform       string                         `json:"platform"`
	Name           string                         `json:"name"`
	TeamNum        int                            `json:"team_num"` // 0 = Blue, 1 = Orange, 255 = Spectator
	IsLocal        bool                           `json:"is_local"`
	IsBot          bool                           `json:"is_bot"`
	IsDisconnected bool                           `json:"is_disconnected,omitempty"`
	Won            *bool                          `json:"won,omitempty"`
	Stats          playertrack.PlayerStatsSummary `json:"stats"`
	RankName       string                         `json:"rank_name"`
	ShortRank      string                         `json:"short_rank,omitempty"`
	Tier           int                            `json:"tier"`
	Division       int                            `json:"division"`
	MMR            float64                        `json:"mmr"`
	MatchupRecord  *storage.PlayerMatchup         `json:"matchup_record,omitempty"`
}

// DeepClone returns an isolated copy of SessionMatchPlayer.
func (p SessionMatchPlayer) DeepClone() SessionMatchPlayer {
	clone := p
	if p.Won != nil {
		won := *p.Won
		clone.Won = &won
	}
	if p.MatchupRecord != nil {
		rec := *p.MatchupRecord
		clone.MatchupRecord = &rec
	}
	return clone
}

// SessionMatchDetail represents a completed match stored in the session history.
type SessionMatchDetail struct {
	MatchGUID       string               `json:"match_guid"`
	PlaylistID      int                  `json:"playlist_id"`
	PlaylistName    string               `json:"playlist_name"`
	StartedAt       time.Time            `json:"started_at"`
	EndedAt         time.Time            `json:"ended_at"`
	DurationSeconds int                  `json:"duration_seconds"`
	Result          string               `json:"result"` // "victory", "defeat", "draw", "unknown"
	LocalTeam       *int                 `json:"local_team,omitempty"`
	WinnerTeam      *int                 `json:"winner_team,omitempty"`
	BlueScore       int                  `json:"blue_score"`
	OrangeScore     int                  `json:"orange_score"`
	StartingMMR     float64              `json:"starting_mmr"`
	EndingMMR       float64              `json:"ending_mmr"`
	MMRChange       float64              `json:"mmr_change"` // EndingMMR - StartingMMR
	Players         []SessionMatchPlayer `json:"players"`
}

// DeepClone returns an isolated copy of SessionMatchDetail.
func (m *SessionMatchDetail) DeepClone() *SessionMatchDetail {
	if m == nil {
		return nil
	}
	clone := *m
	if m.LocalTeam != nil {
		lt := *m.LocalTeam
		clone.LocalTeam = &lt
	}
	if m.WinnerTeam != nil {
		wt := *m.WinnerTeam
		clone.WinnerTeam = &wt
	}
	if m.Players != nil {
		clone.Players = make([]SessionMatchPlayer, len(m.Players))
		for i, p := range m.Players {
			clone.Players[i] = p.DeepClone()
		}
	}
	return &clone
}

// ActiveMatchRecord tracks transient metadata for a match currently in progress.
type ActiveMatchRecord struct {
	MatchGUID   string
	PlaylistID  int
	StartedAt   time.Time
	StartingMMR float64
}

// SessionResponse represents the full payload returned by GET /api/session.
type SessionResponse struct {
	SessionID             string                            `json:"session_id"`
	StartedAt             time.Time                         `json:"started_at"`
	EndedAt               *time.Time                        `json:"ended_at,omitempty"`
	LastMatchAt           *time.Time                        `json:"last_match_at,omitempty"`
	Uptime                string                            `json:"uptime"`
	TotalMatches          int                               `json:"total_matches"`
	TotalWins             int                               `json:"total_wins"`
	TotalLosses           int                               `json:"total_losses"`
	WinRate               float64                           `json:"win_rate"` // Percentage (0.0 - 100.0, rounded to 1 decimal place)
	Playlists             map[string]*PlaylistSessionStats  `json:"playlists"`
	MostPlayedPlaylist    *MostPlayedPlaylistSummary        `json:"most_played_playlist,omitempty"`
	ForStats              *storage.PlayerRoleStatsSummary   `json:"for_stats,omitempty"`
	AgainstStats          *storage.PlayerRoleStatsSummary   `json:"against_stats,omitempty"`
	Matches               []*SessionMatchDetail             `json:"matches"`
	ActiveMatch           *playertrack.CurrentMatchResponse `json:"active_match,omitempty"`
	LocalPlayerIDs        []string                          `json:"local_player_ids,omitempty"`
	ContinuableSession    *SessionResponse                  `json:"continuable_session,omitempty"`
	ContinueWindowSeconds int                               `json:"continue_window_seconds,omitempty"`
	CanContinue           bool                              `json:"can_continue,omitempty"`
	StatsAPIReady         bool                              `json:"stats_api_ready"`
	RankAPIReady          bool                              `json:"rank_api_ready"`
	MMRPollerState        string                            `json:"mmr_poller_state,omitempty"`
	GameConnected         bool                              `json:"game_connected"`
	PendingCount          int                               `json:"pending_count"`
	TriggerThreshold      int                               `json:"trigger_threshold"`
	ForceSyncOnTrigger    bool                              `json:"force_sync_on_trigger"`
	EnableToast           bool                              `json:"enable_toast"`
	AutoSyncOnExit        bool                              `json:"auto_sync_on_exit"`
	MaxHostedReplays      int                               `json:"max_hosted_replays"`
	ReplayCount           int                               `json:"replay_count"`
	ReplayLimit           int                               `json:"replay_limit"`
	ReplayUsagePct        float64                           `json:"replay_usage_pct"`
	ReplayLimitStatus     string                            `json:"replay_limit_status,omitempty"`
}

// DeepClone creates a fully isolated copy of SessionResponse for thread-safe concurrent reads.
func (r *SessionResponse) DeepClone() *SessionResponse {
	if r == nil {
		return nil
	}
	clone := *r
	if r.EndedAt != nil {
		t := *r.EndedAt
		clone.EndedAt = &t
	}
	if r.LastMatchAt != nil {
		t := *r.LastMatchAt
		clone.LastMatchAt = &t
	}
	if r.Playlists != nil {
		clone.Playlists = make(map[string]*PlaylistSessionStats, len(r.Playlists))
		for k, v := range r.Playlists {
			clone.Playlists[k] = v.DeepClone()
		}
	}
	if r.MostPlayedPlaylist != nil {
		clone.MostPlayedPlaylist = r.MostPlayedPlaylist.DeepClone()
	}
	if r.ForStats != nil {
		fs := *r.ForStats
		clone.ForStats = &fs
	}
	if r.AgainstStats != nil {
		as := *r.AgainstStats
		clone.AgainstStats = &as
	}
	if r.Matches != nil {
		clone.Matches = make([]*SessionMatchDetail, len(r.Matches))
		for i, m := range r.Matches {
			clone.Matches[i] = m.DeepClone()
		}
	}
	if r.ActiveMatch != nil {
		clone.ActiveMatch = r.ActiveMatch.DeepClone()
	}
	if r.LocalPlayerIDs != nil {
		clone.LocalPlayerIDs = make([]string, len(r.LocalPlayerIDs))
		copy(clone.LocalPlayerIDs, r.LocalPlayerIDs)
	}
	if r.ContinuableSession != nil {
		clone.ContinuableSession = r.ContinuableSession.DeepClone()
	}
	return &clone
}

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

// WithNowFunc is an alias for WithClock for flexible configuration naming.
func WithNowFunc(nowFn func() time.Time) Option {
	return WithClock(nowFn)
}
