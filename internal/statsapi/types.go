package statsapi

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/dank/rl-api-utils/internal/contracts"
	"github.com/dank/rl-api-utils/internal/domain"
)

// EventMessage represents a JSON message broadcast by Rocket League's MatchStatsExporter_TA.
type EventMessage struct {
	Event string    `json:"Event"`
	Data  EventData `json:"Data"`
}

// EventData contains event-specific payload fields broadcast by the Rocket League Stats API.
type EventData struct {
	MatchGuid     string          `json:"MatchGuid,omitempty"`
	Playlist      int             `json:"Playlist,omitempty"`
	WinnerTeamNum *int            `json:"WinnerTeamNum,omitempty"`
	Players       []StatsPlayer   `json:"Players,omitempty"`
	Game          *StatsGame      `json:"Game,omitempty"`
	Raw           json.RawMessage `json:"-"`
}

// StatsGame holds global match parameters from UpdateState.
type StatsGame struct {
	PlaylistId    int  `json:"PlaylistId,omitempty"`
	TimeSeconds   int  `json:"TimeSeconds,omitempty"`
	TimeRemaining int  `json:"time_remaining,omitempty"`
	Overtime      bool `json:"bOvertime,omitempty"`
	IsOvertime    bool `json:"is_overtime,omitempty"`
}

// GetTimeRemaining returns the match time remaining in seconds.
func (g *StatsGame) GetTimeRemaining() int {
	if g == nil {
		return 0
	}
	if g.TimeRemaining != 0 {
		return g.TimeRemaining
	}
	return g.TimeSeconds
}

// GetIsOvertime returns whether the match is in overtime.
func (g *StatsGame) GetIsOvertime() bool {
	if g == nil {
		return false
	}
	return g.Overtime || g.IsOvertime
}

// StatsPlayer represents an individual player within the lobby.
type StatsPlayer struct {
	Name      string `json:"Name"`
	PrimaryId string `json:"PrimaryId"` // e.g. "Steam|76561198...|0", "Epic|<id>|0", "Unknown|0|0"
	TeamNum   int    `json:"TeamNum"`   // 0 = Blue, 1 = Orange
	Score     int    `json:"Score"`
	Goals     int    `json:"Goals"`
	Assists   int    `json:"Assists"`
	Saves     int    `json:"Saves"`
	Shots     int    `json:"Shots"`
	Demos     int    `json:"Demos"`
}

// ParsedPlayerID contains the dissected components of a Stats API PrimaryId.
type ParsedPlayerID struct {
	Platform    string
	AccountID   string
	Splitscreen int
	Raw         string
	IsBot       bool
}

// ParsePrimaryID parses a composite player identifier in the format <Platform>|<AccountID>|<SplitscreenIndex>.
// For example: "Steam|76561198012345678|0" or "Epic|34a02cf8f4414e29b15921876da36f9a|0".
// AI bots are indicated by platform "Unknown" or account ID "0" (e.g. "Unknown|0|0").
func ParsePrimaryID(raw string) (ParsedPlayerID, error) {
	p, err := domain.ParsePrimaryID(raw)
	return ParsedPlayerID{
		Platform:    p.Platform,
		AccountID:   p.AccountID,
		Splitscreen: p.Splitscreen,
		Raw:         p.Raw,
		IsBot:       p.IsBot,
	}, err
}

// ParseID parses this player's PrimaryId into structured components.
func (p StatsPlayer) ParseID() (ParsedPlayerID, error) {
	return ParsePrimaryID(p.PrimaryId)
}

// IsBot returns true if this player represents an AI bot.
func (p StatsPlayer) IsBot() bool {
	return domain.IsBotID(p.PrimaryId)
}

// GetPlaylist returns the playlist ID, resolving from Game.PlaylistId if top-level Playlist is 0.
func (ed *EventData) GetPlaylist() int {
	if ed == nil {
		return 0
	}
	if ed.Playlist != 0 {
		return ed.Playlist
	}
	if ed.Game != nil && ed.Game.PlaylistId != 0 {
		return ed.Game.PlaylistId
	}
	return 0
}

// UnmarshalJSON unmarshals event data while tolerating varied payloads:
// both nested JSON objects ({"MatchGuid": "..."}) and JSON-encoded strings ("{\"MatchGuid\": \"...\"}").
func (ed *EventData) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil
	}

	target := trimmed
	// If payload is wrapped in quotes, it is a JSON-encoded string
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return fmt.Errorf("failed to unmarshal string-encoded EventData: %w", err)
		}
		target = bytes.TrimSpace([]byte(s))
		if len(target) == 0 {
			return nil
		}
	}

	type Alias EventData
	var a Alias
	if err := json.Unmarshal(target, &a); err != nil {
		return err
	}
	*ed = EventData(a)
	ed.Raw = make([]byte, len(target))
	copy(ed.Raw, target)
	return nil
}

// MaxHostedReplays is Rocket League's server-side rolling FIFO cap of 20 online match replays.
// Any un-downloaded replays beyond 20 games are permanently overwritten on PsyNet.
const MaxHostedReplays = 20

// PendingMatch tracks an online match detected via Stats API that has not yet been downloaded/persisted.
type PendingMatch = contracts.PendingMatch

// Status represents the current operational status of the Stats API tracking module.
type Status = contracts.StatsAPIStatus
