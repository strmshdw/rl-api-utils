package replay

const (
	maxHeaderLen        = 32 * 1024 * 1024
	maxPropertyDataSize = 16 * 1024 * 1024
	maxBytePropDataSize = 1024 * 1024
	maxFStringLen       = 65536
	maxArrayCount       = 1000
	maxPropertyDepth    = 8

	netVersionEngineThreshold   = 868
	netVersionLicenseeThreshold = 18
	minOvertimeHeuristicSeconds = 315.0
)

// ReplayPlayerStats contains per-player statistics extracted from a .replay file.
type ReplayPlayerStats struct {
	Name     string `json:"name"`
	Platform string `json:"platform"`
	OnlineID string `json:"online_id"`
	Team     int    `json:"team"` // 0 = Blue, 1 = Orange
	Score    int    `json:"score"`
	Goals    int    `json:"goals"`
	Assists  int    `json:"assists"`
	Saves    int    `json:"saves"`
	Shots    int    `json:"shots"`
	Demos    int    `json:"demos,omitempty"`
	IsBot    bool   `json:"is_bot"`
}

// ReplayMetadata contains game-level metadata and box scores extracted from a .replay file.
type ReplayMetadata struct {
	MatchGUID    string              `json:"match_guid"`
	MapName      string              `json:"map_name"`
	Team0Score   int                 `json:"team0_score"` // Blue
	Team1Score   int                 `json:"team1_score"` // Orange
	WinnerTeam   *int                `json:"winner_team,omitempty"`
	TeamSize     int                 `json:"team_size"`
	TotalSeconds float32             `json:"total_seconds"`
	Forfeit      bool                `json:"forfeit"`
	Overtime     bool                `json:"overtime"`
	Date         string              `json:"date"`
	Players      []ReplayPlayerStats `json:"players"`
	LocalTeam    *int                `json:"local_team,omitempty"`
	PlayerName   string              `json:"player_name,omitempty"` // Local player name in replay
}

type propertyValue struct {
	typeName string
	intVal   int
	strVal   string
	floatVal float32
	boolVal  bool
	int64Val int64
	subProps []map[string]*propertyValue
}
