package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/storage"
)

// EventType defines valid Server-Sent Events identifiers.
type EventType = string

const (
	// EventMatchUpdate is emitted when active match telemetry, rosters, or stats change.
	EventMatchUpdate EventType = "match_update"
	// EventMatchEnded is emitted when a match concludes and final scores are recorded.
	EventMatchEnded EventType = "match_ended"
	// EventSessionUpdate is emitted when session-level statistics or history update.
	EventSessionUpdate EventType = "session_update"
)

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
	PlayerID      string                         `json:"player_id"`
	Platform      string                         `json:"platform"`
	Name          string                         `json:"name"`
	TeamNum       int                            `json:"team_num"` // 0 = Blue, 1 = Orange, 255 = Spectator
	IsLocal       bool                           `json:"is_local"`
	IsBot         bool                           `json:"is_bot"`
	Stats         playertrack.PlayerStatsSummary `json:"stats"`
	RankName      string                         `json:"rank_name"`
	Tier          int                            `json:"tier"`
	Division      int                            `json:"division"`
	MMR           float64                        `json:"mmr"`
	MatchupRecord *storage.PlayerMatchup         `json:"matchup_record,omitempty"`
}

// DeepClone returns an isolated copy of SessionMatchPlayer.
func (p SessionMatchPlayer) DeepClone() SessionMatchPlayer {
	clone := p
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
	SessionID    string                           `json:"session_id"`
	StartedAt    time.Time                        `json:"started_at"`
	Uptime       string                           `json:"uptime"`
	TotalMatches int                              `json:"total_matches"`
	TotalWins    int                              `json:"total_wins"`
	TotalLosses  int                              `json:"total_losses"`
	WinRate      float64                          `json:"win_rate"` // Percentage (0.0 - 100.0, rounded to 1 decimal place)
	Playlists    map[string]*PlaylistSessionStats `json:"playlists"`
	Matches      []*SessionMatchDetail            `json:"matches"`
	ActiveMatch  *playertrack.CurrentMatchResponse `json:"active_match,omitempty"`
}

// DeepClone creates a fully isolated copy of SessionResponse for thread-safe concurrent reads.
func (r *SessionResponse) DeepClone() *SessionResponse {
	if r == nil {
		return nil
	}
	clone := *r
	if r.Playlists != nil {
		clone.Playlists = make(map[string]*PlaylistSessionStats, len(r.Playlists))
		for k, v := range r.Playlists {
			clone.Playlists[k] = v.DeepClone()
		}
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
	return &clone
}
