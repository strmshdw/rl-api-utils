package ballchasing

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Sentinel errors returned by the Ballchasing client.
var (
	// ErrInvalidAPIKey is returned when Ballchasing responds with HTTP 401 Unauthorized.
	ErrInvalidAPIKey = errors.New("ballchasing: invalid or unauthorized API key (HTTP 401)")

	// ErrRateLimitExhausted is returned when HTTP 429 retries exceed the retry budget.
	ErrRateLimitExhausted = errors.New("ballchasing: rate limit retries exhausted (HTTP 429)")

	// ErrBadRequest is returned when Ballchasing rejects an upload with HTTP 400 Bad Request.
	ErrBadRequest = errors.New("ballchasing: bad request (HTTP 400)")

	// ErrNotFound is returned when an endpoint or resource is not found (HTTP 404).
	ErrNotFound = errors.New("ballchasing: resource not found (HTTP 404)")

	// ErrServerError is returned when Ballchasing returns an unrecoverable 5xx server error.
	ErrServerError = errors.New("ballchasing: server error (HTTP 5xx)")

	// ErrEmptyMatchGUID is returned when matchGUID is empty or whitespace.
	ErrEmptyMatchGUID = errors.New("ballchasing: match GUID cannot be empty")

	// ErrEmptyFilePath is returned when replay file path is empty or whitespace.
	ErrEmptyFilePath = errors.New("ballchasing: replay file path cannot be empty")

	// ErrEmptyAPIKey is returned when the configured API key is empty.
	ErrEmptyAPIKey = errors.New("ballchasing: API key cannot be empty")

	// ErrEmptyFile is returned when the replay file is 0 bytes.
	ErrEmptyFile = errors.New("ballchasing: empty replay file (0 bytes)")

	// ErrFileNotFound is returned when the specified replay file does not exist on disk.
	ErrFileNotFound = errors.New("ballchasing: replay file not found")

	// ErrInvalidVisibility is returned when an unsupported visibility string is configured.
	ErrInvalidVisibility = errors.New("ballchasing: invalid visibility (must be 'public', 'unlisted', or 'private')")
)

// Visibility represents the upload visibility setting on Ballchasing.com.
type Visibility = string

const (
	// VisibilityPublic makes the uploaded replay publicly visible and searchable on ballchasing.com.
	VisibilityPublic Visibility = "public"

	// VisibilityUnlisted allows anyone with the direct replay URL to view it, but hides it from public search.
	VisibilityUnlisted Visibility = "unlisted"

	// VisibilityPrivate restricts replay visibility strictly to the uploader's account.
	VisibilityPrivate Visibility = "private"
)

// UploadResult captures the outcome of a replay upload.
// Satisfies the PROJECT.md interface contract for internal/ballchasing -> internal/syncer.
type UploadResult struct {
	ID          string `json:"id"`
	Location    string `json:"location"`
	IsDuplicate bool   `json:"is_duplicate"`
}

// PingResponse represents the account metadata returned by GET / (ping).
type PingResponse struct {
	Chaser  bool   `json:"chaser"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	SteamID string `json:"steam_id"`
}

// ReplayUploader defines the contract for uploading Rocket League replays to Ballchasing.com.
// Matches the PROJECT.md interface contract for internal/ballchasing -> internal/syncer.
type ReplayUploader interface {
	UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error)
	Ping(ctx context.Context) error
}

// ReplayListResponse represents the paginated response from GET /api/replays.
type ReplayListResponse struct {
	Count int          `json:"count"`
	List  []ReplayItem `json:"list"`
	Next  string       `json:"next"`
}

// ReplayItem represents a single match/replay record returned by Ballchasing.com.
// Satisfies both paginated list responses (GET /api/replays) and full detail responses (GET /api/replays/{id}).
type ReplayItem struct {
	ID              string        `json:"id"`
	Link            string        `json:"link"`
	Created         string        `json:"created"`
	Date            string        `json:"date"`
	DateHasTimezone bool          `json:"date_has_timezone,omitempty"`
	Status          string        `json:"status,omitempty"` // "ok", "pending", "failed"
	RocketLeagueID  string        `json:"rocket_league_id,omitempty"`
	MatchGUID       string        `json:"match_guid,omitempty"`
	Title           string        `json:"title,omitempty"`
	ReplayTitle     string        `json:"replay_title,omitempty"` // Returned in GET /api/replays list endpoint
	MapCode         string        `json:"map_code,omitempty"`
	MapName         string        `json:"map_name,omitempty"`
	MatchType       string        `json:"match_type,omitempty"`
	TeamSize        int           `json:"team_size,omitempty"`
	PlaylistID      string        `json:"playlist_id,omitempty"`
	PlaylistName    string        `json:"playlist_name,omitempty"`
	Duration        int           `json:"duration,omitempty"`
	PlayDuration    int           `json:"play_duration,omitempty"`
	Overtime        bool          `json:"overtime"`
	OvertimeSeconds int           `json:"overtime_seconds,omitempty"`
	Season          int           `json:"season,omitempty"`
	SeasonType      string        `json:"season_type,omitempty"`
	Visibility      string        `json:"visibility,omitempty"`
	Server          *ServerInfo   `json:"server,omitempty"`
	Uploader        *UploaderInfo `json:"uploader,omitempty"`
	Blue            ReplayTeam    `json:"blue"`
	Orange          ReplayTeam    `json:"orange"`
	MinRank         *RankInfo     `json:"min_rank,omitempty"`
	MaxRank         *RankInfo     `json:"max_rank,omitempty"`
	Groups          []GroupInfo   `json:"groups,omitempty"`
}

// ReplayDetail aliases ReplayItem to satisfy the PROJECT.md interface contract.
type ReplayDetail = ReplayItem

// ReplayTeam represents a team in a Ballchasing replay.
type ReplayTeam struct {
	Color   string         `json:"color"`
	Name    string         `json:"name"`
	Goals   int            `json:"goals"`
	Players []ReplayPlayer `json:"players"`
	Stats   *TeamStats     `json:"stats,omitempty"`
}

// TeamStats contains aggregate team-level telemetry across all categories.
type TeamStats struct {
	Ball        *BallStats        `json:"ball,omitempty"`
	Core        *CoreStats        `json:"core,omitempty"`
	Boost       *BoostStats       `json:"boost,omitempty"`
	Movement    *MovementStats    `json:"movement,omitempty"`
	Positioning *PositioningStats `json:"positioning,omitempty"`
	Demo        *DemoStats        `json:"demo,omitempty"`
}

// BallStats measures ball possession and territorial occupation.
type BallStats struct {
	PossessionTime float64 `json:"possession_time"` // Time in seconds team held ball possession
	TimeInSide     float64 `json:"time_in_side"`     // Time in seconds ball stayed in team's defensive half
}

// ReplayPlayer represents a player in a Ballchasing replay team.
// Supports both flat list representation and comprehensive nested telemetry.
type ReplayPlayer struct {
	StartTime           float64         `json:"start_time,omitempty"`
	EndTime             float64         `json:"end_time,omitempty"`
	Name                string          `json:"name"`
	ID                  PlayerIdentity  `json:"id"`
	CarID               int             `json:"car_id,omitempty"`
	CarName             string          `json:"car_name,omitempty"`
	Camera              *CameraSettings `json:"camera,omitempty"`
	SteeringSensitivity float64         `json:"steering_sensitivity,omitempty"`
	Rank                *PlayerRank     `json:"rank,omitempty"`
	MVP                 bool            `json:"mvp,omitempty"`

	// Flat box score fields (hydrated automatically from Stats.Core / Stats.Demo if nested)
	Score   int         `json:"score"`
	Goals   int         `json:"goals"`
	Assists int         `json:"assists"`
	Saves   int         `json:"saves"`
	Shots   int         `json:"shots"`
	Demos   PlayerDemos `json:"demos"`

	// Nested telemetry container as returned by GET /api/replays/{id}
	Stats *PlayerTelemetry `json:"stats,omitempty"`

	// Category pointers directly on ReplayPlayer (PROJECT.md contract compatibility)
	Core        *CoreStats        `json:"core,omitempty"`
	Boost       *BoostStats       `json:"boost,omitempty"`
	Movement    *MovementStats    `json:"movement,omitempty"`
	Positioning *PositioningStats `json:"positioning,omitempty"`
	Demo        *DemoStats        `json:"demo,omitempty"`
	Ball        *BallStats        `json:"ball,omitempty"`
}

// PlayerTelemetry encapsulates the player statistics block.
type PlayerTelemetry struct {
	Core        *CoreStats        `json:"core,omitempty"`
	Boost       *BoostStats       `json:"boost,omitempty"`
	Movement    *MovementStats    `json:"movement,omitempty"`
	Positioning *PositioningStats `json:"positioning,omitempty"`
	Demo        *DemoStats        `json:"demo,omitempty"`
	Ball        *BallStats        `json:"ball,omitempty"`
}

// CoreStats represents traditional and advanced mechanics counters.
type CoreStats struct {
	Shots              int     `json:"shots"`
	ShotsAgainst       int     `json:"shots_against"`
	Goals              int     `json:"goals"`
	GoalsAgainst       int     `json:"goals_against"`
	Saves              int     `json:"saves"`
	Assists            int     `json:"assists"`
	Score              int     `json:"score"`
	MVP                bool    `json:"mvp"`
	ShootingPercentage float64 `json:"shooting_percentage"`

	// Extended in-game mechanics
	EpicSaves    int `json:"epic_saves"`
	Clears       int `json:"clears"`
	Centers      int `json:"centers"`
	AerialHits   int `json:"aerial_hits"`
	FirstTouches int `json:"first_touches"`
	FlipResets   int `json:"flip_resets"`
	CrossbarHits int `json:"crossbar_hits"`
	BicycleHits  int `json:"bicycle_hits"`
	JuggleHits   int `json:"juggle_hits"`
}

// BoostStats captures boost economy, steals, and distribution metrics.
type BoostStats struct {
	BPM                       float64 `json:"bpm"`
	BCPM                      float64 `json:"bcpm"`
	AvgAmount                 float64 `json:"avg_amount"`
	AmountCollected           int     `json:"amount_collected"`
	AmountStolen              int     `json:"amount_stolen"`
	AmountCollectedBig        int     `json:"amount_collected_big"`
	AmountStolenBig           int     `json:"amount_stolen_big"`
	AmountCollectedSmall      int     `json:"amount_collected_small"`
	AmountStolenSmall         int     `json:"amount_stolen_small"`
	CountCollectedBig         int     `json:"count_collected_big"`
	CountStolenBig            int     `json:"count_stolen_big"`
	CountCollectedSmall       int     `json:"count_collected_small"`
	CountStolenSmall          int     `json:"count_stolen_small"`
	AmountOverfill            int     `json:"amount_overfill"`
	AmountOverfillStolen      int     `json:"amount_overfill_stolen"`
	AmountUsedWhileSupersonic int     `json:"amount_used_while_supersonic"`
	TimeZeroBoost             float64 `json:"time_zero_boost"`
	PercentZeroBoost          float64 `json:"percent_zero_boost"`
	TimeFullBoost             float64 `json:"time_full_boost"`
	PercentFullBoost          float64 `json:"percent_full_boost"`
	TimeBoost0To25            float64 `json:"time_boost_0_25"`
	TimeBoost25To50           float64 `json:"time_boost_25_50"`
	TimeBoost50To75           float64 `json:"time_boost_50_75"`
	TimeBoost75To100          float64 `json:"time_boost_75_100"`
	PercentBoost0To25         float64 `json:"percent_boost_0_25"`
	PercentBoost25To50        float64 `json:"percent_boost_25_50"`
	PercentBoost50To75        float64 `json:"percent_boost_50_75"`
	PercentBoost75To100       float64 `json:"percent_boost_75_100"`
}

// MovementStats measures speed, distance, elevation, and powerslide mechanics.
type MovementStats struct {
	AvgSpeed              float64 `json:"avg_speed"`
	TotalDistance         float64 `json:"total_distance"`
	TimeSupersonicSpeed   float64 `json:"time_supersonic_speed"`
	TimeBoostSpeed        float64 `json:"time_boost_speed"`
	TimeSlowSpeed         float64 `json:"time_slow_speed"`
	TimeGround            float64 `json:"time_ground"`
	TimeLowAir            float64 `json:"time_low_air"`
	TimeHighAir           float64 `json:"time_high_air"`
	TimePowerslide        float64 `json:"time_powerslide"`
	CountPowerslide       int     `json:"count_powerslide"`
	AvgPowerslideDuration float64 `json:"avg_powerslide_duration"`
	AvgSpeedPercentage    float64 `json:"avg_speed_percentage"`
	PercentSlowSpeed      float64 `json:"percent_slow_speed"`
	PercentBoostSpeed     float64 `json:"percent_boost_speed"`
	PercentSupersonic     float64 `json:"percent_supersonic_speed"`
	PercentGround         float64 `json:"percent_ground"`
	PercentLowAir         float64 `json:"percent_low_air"`
	PercentHighAir        float64 `json:"percent_high_air"`
}

// PositioningStats quantifies field positioning, spacing, and third/half allocations.
type PositioningStats struct {
	AvgDistanceToBall             float64 `json:"avg_distance_to_ball"`
	AvgDistanceToBallPossession   float64 `json:"avg_distance_to_ball_possession"`
	AvgDistanceToBallNoPossession float64 `json:"avg_distance_to_ball_no_possession"`
	AvgDistanceToMates            float64 `json:"avg_distance_to_mates"`
	TimeDefensiveThird            float64 `json:"time_defensive_third"`
	TimeNeutralThird              float64 `json:"time_neutral_third"`
	TimeOffensiveThird            float64 `json:"time_offensive_third"`
	TimeDefensiveHalf             float64 `json:"time_defensive_half"`
	TimeOffensiveHalf             float64 `json:"time_offensive_half"`
	TimeBehindBall                float64 `json:"time_behind_ball"`
	TimeInFrontOfBall             float64 `json:"time_infront_ball"`
	TimeMostBack                  float64 `json:"time_most_back"`
	TimeMostForward               float64 `json:"time_most_forward"`
	GoalsAgainstWhileLastDefender int     `json:"goals_against_while_last_defender"`
	TimeClosestToBall             float64 `json:"time_closest_to_ball"`
	TimeFarthestFromBall          float64 `json:"time_farthest_from_ball"`
	PercentDefensiveThird         float64 `json:"percent_defensive_third"`
	PercentOffensiveThird         float64 `json:"percent_offensive_third"`
	PercentNeutralThird           float64 `json:"percent_neutral_third"`
	PercentDefensiveHalf          float64 `json:"percent_defensive_half"`
	PercentOffensiveHalf          float64 `json:"percent_offensive_half"`
	PercentBehindBall             float64 `json:"percent_behind_ball"`
	PercentInFrontOfBall          float64 `json:"percent_infront_ball"`
	PercentMostBack               float64 `json:"percent_most_back"`
	PercentMostForward            float64 `json:"percent_most_forward"`
	PercentClosestToBall          float64 `json:"percent_closest_to_ball"`
	PercentFarthestFromBall       float64 `json:"percent_farthest_from_ball"`
}

// DemoStats details inflicted and taken demolitions with field-half splits.
type DemoStats struct {
	Inflicted                int `json:"inflicted"`
	InflictedInDefensiveHalf int `json:"inflicted_in_defensive_half"`
	InflictedInOffensiveHalf int `json:"inflicted_in_offensive_half"`
	Taken                    int `json:"taken"`
	TakenInDefensiveHalf     int `json:"taken_in_defensive_half"`
	TakenInOffensiveHalf     int `json:"taken_in_offensive_half"`
}

// CameraSettings stores player camera configuration settings.
type CameraSettings struct {
	FOV             int     `json:"fov"`
	Height          int     `json:"height"`
	Pitch           int     `json:"pitch"`
	Distance        int     `json:"distance"`
	Stiffness       float64 `json:"stiffness"`
	SwivelSpeed     float64 `json:"swivel_speed"`
	TransitionSpeed float64 `json:"transition_speed"`
}

// PlayerIdentity contains player platform and platform account ID.
type PlayerIdentity struct {
	Platform string `json:"platform"`
	ID       string `json:"id"`
}

// PlayerID aliases PlayerIdentity for PROJECT.md contract compatibility.
type PlayerID = PlayerIdentity

// PlayerDemos contains demolition statistics for a player.
type PlayerDemos struct {
	Inflicted int `json:"inflicted"`
	Taken     int `json:"taken"`
}

// PlayerRank represents rank and tier information.
type PlayerRank struct {
	ID       string  `json:"id,omitempty"`
	Tier     int     `json:"tier,omitempty"`
	Division int     `json:"division,omitempty"`
	Name     string  `json:"name,omitempty"`
	MMR      float64 `json:"mmr,omitempty"`
}

// RankInfo aliases PlayerRank for min_rank / max_rank contract compatibility.
type RankInfo = PlayerRank

// UploaderInfo represents account metadata of the replay uploader.
type UploaderInfo struct {
	SteamID    string `json:"steam_id"`
	Name       string `json:"name"`
	ProfileURL string `json:"profile_url,omitempty"`
	Avatar     string `json:"avatar,omitempty"`
}

// ServerInfo represents game server details.
type ServerInfo struct {
	Name string `json:"name,omitempty"`
}

// GroupInfo represents a Ballchasing replay group assignment.
type GroupInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Link string `json:"link"`
}

// UnmarshalJSON bridges flat list and nested detail formats seamlessly.
func (p *ReplayPlayer) UnmarshalJSON(data []byte) error {
	type Alias ReplayPlayer
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(p),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// 1. If nested Stats is present (Detail response), bridge to top-level pointers & flat fields
	if p.Stats != nil {
		if p.Stats.Core != nil {
			if p.Core == nil {
				p.Core = p.Stats.Core
			}
			if p.Score == 0 {
				p.Score = p.Stats.Core.Score
			}
			if p.Goals == 0 {
				p.Goals = p.Stats.Core.Goals
			}
			if p.Assists == 0 {
				p.Assists = p.Stats.Core.Assists
			}
			if p.Saves == 0 {
				p.Saves = p.Stats.Core.Saves
			}
			if p.Shots == 0 {
				p.Shots = p.Stats.Core.Shots
			}
			if !p.MVP && p.Stats.Core.MVP {
				p.MVP = true
			}
		}
		if p.Stats.Boost != nil && p.Boost == nil {
			p.Boost = p.Stats.Boost
		}
		if p.Stats.Movement != nil && p.Movement == nil {
			p.Movement = p.Stats.Movement
		}
		if p.Stats.Positioning != nil && p.Positioning == nil {
			p.Positioning = p.Stats.Positioning
		}
		if p.Stats.Demo != nil {
			if p.Demo == nil {
				p.Demo = p.Stats.Demo
			}
			if p.Demos.Inflicted == 0 {
				p.Demos.Inflicted = p.Stats.Demo.Inflicted
			}
			if p.Demos.Taken == 0 {
				p.Demos.Taken = p.Stats.Demo.Taken
			}
		}
		if p.Stats.Ball != nil && p.Ball == nil {
			p.Ball = p.Stats.Ball
		}
	} else {
		// 2. If nested Stats is missing, check if direct category pointers or flat fields exist
		hasPointers := p.Core != nil || p.Boost != nil || p.Movement != nil || p.Positioning != nil || p.Demo != nil || p.Ball != nil
		if hasPointers {
			p.Stats = &PlayerTelemetry{
				Core:        p.Core,
				Boost:       p.Boost,
				Movement:    p.Movement,
				Positioning: p.Positioning,
				Demo:        p.Demo,
				Ball:        p.Ball,
			}
		} else if p.Score > 0 || p.Goals > 0 || p.Assists > 0 || p.Saves > 0 || p.Shots > 0 {
			// Synthesize Core from flat fields so callers accessing p.Stats.Core or p.Core never get nil
			p.Core = &CoreStats{
				Score:   p.Score,
				Goals:   p.Goals,
				Assists: p.Assists,
				Saves:   p.Saves,
				Shots:   p.Shots,
				MVP:     p.MVP,
			}
			p.Stats = &PlayerTelemetry{Core: p.Core}
		}
		if p.Demo == nil && (p.Demos.Inflicted > 0 || p.Demos.Taken > 0) {
			p.Demo = &DemoStats{
				Inflicted: p.Demos.Inflicted,
				Taken:     p.Demos.Taken,
			}
			if p.Stats == nil {
				p.Stats = &PlayerTelemetry{}
			}
			p.Stats.Demo = p.Demo
		}
	}

	return nil
}

// UnmarshalJSON implements json.Unmarshaler for ReplayTeam, hydrating top-level
// Goals from nested stats.core.goals or the sum of player goals when top-level
// goals is omitted (as in Ballchasing's single replay detail endpoint).
func (t *ReplayTeam) UnmarshalJSON(data []byte) error {
	type Alias ReplayTeam
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(t),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if t.Goals == 0 && t.Stats != nil && t.Stats.Core != nil && t.Stats.Core.Goals > 0 {
		t.Goals = t.Stats.Core.Goals
	}
	if t.Goals == 0 && len(t.Players) > 0 {
		sum := 0
		for _, p := range t.Players {
			sum += p.Goals
		}
		if sum > 0 {
			t.Goals = sum
		}
	}
	return nil
}

// UnmarshalJSON bridges field differences between list and detail replay objects.
func (r *ReplayItem) UnmarshalJSON(data []byte) error {
	type Alias ReplayItem
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(r),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Synchronize Title and ReplayTitle across list and detail schemas
	if r.Title == "" && r.ReplayTitle != "" {
		r.Title = r.ReplayTitle
	} else if r.ReplayTitle == "" && r.Title != "" {
		r.ReplayTitle = r.Title
	}

	// Synchronize RocketLeagueID and MatchGUID
	if r.MatchGUID == "" && r.RocketLeagueID != "" {
		r.MatchGUID = r.RocketLeagueID
	} else if r.RocketLeagueID == "" && r.MatchGUID != "" {
		r.RocketLeagueID = r.MatchGUID
	}

	// Cross-check team goals from opponent's goals_against if still zero
	if r.Blue.Goals == 0 && r.Orange.Stats != nil && r.Orange.Stats.Core != nil && r.Orange.Stats.Core.GoalsAgainst > 0 {
		r.Blue.Goals = r.Orange.Stats.Core.GoalsAgainst
	}
	if r.Orange.Goals == 0 && r.Blue.Stats != nil && r.Blue.Stats.Core != nil && r.Blue.Stats.Core.GoalsAgainst > 0 {
		r.Orange.Goals = r.Blue.Stats.Core.GoalsAgainst
	}

	return nil
}

// MapPlaylistSlugToID maps Ballchasing playlist slugs or string IDs to numeric Rocket League playlist IDs.
func MapPlaylistSlugToID(slug string) int {
	slug = strings.TrimSpace(strings.ToLower(slug))
	switch slug {
	case "ranked-duels", "ranked-duel", "1v1", "10":
		return 10
	case "ranked-doubles", "ranked-double", "2v2", "11":
		return 11
	case "ranked-solo-standard", "solo-standard", "12":
		return 12
	case "ranked-standard", "3v3", "13":
		return 13
	case "ranked-hoops", "hoops", "27":
		return 27
	case "ranked-rumble", "rumble", "28":
		return 28
	case "ranked-dropshot", "dropshot", "29":
		return 29
	case "ranked-snowday", "snowday", "30":
		return 30
	case "tournament", "tournaments", "34":
		return 34
	case "casual-standard", "unranked-standard", "3":
		return 3
	case "casual-doubles", "unranked-doubles", "2":
		return 2
	case "casual-duels", "casual-duel", "unranked-duels", "unranked-duel", "1":
		return 1
	case "casual-chaos", "unranked-chaos", "4":
		return 4
	default:
		// Attempt numeric parse if already an integer string (e.g. "11", "22")
		if id, err := strconv.Atoi(slug); err == nil && id > 0 {
			return id
		}
		return 0
	}
}

// ClientConfig holds configuration settings for the Ballchasing client.
type ClientConfig struct {
	BaseURL      string
	APIKey       string
	Visibility   string // "public", "unlisted", "private"
	Group        string // optional group id
	MaxRetries   int
	Timeout      time.Duration
	BaseBackoff  time.Duration
	MaxBackoff   time.Duration
	StreamUpload bool // true enables zero-RAM streaming via io.MultiReader with exact Content-Length
}
