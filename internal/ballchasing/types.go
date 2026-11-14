package ballchasing

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/dank/rl-api-utils/internal/domain"
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

// BallStats aliases domain.BallStats as canonical SSoT.
type BallStats = domain.BallStats

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

// CoreStats aliases domain.CoreStats as canonical SSoT.
type CoreStats = domain.CoreStats

// BoostStats aliases domain.BoostStats as canonical SSoT.
type BoostStats = domain.BoostStats

// MovementStats aliases domain.MovementStats as canonical SSoT.
type MovementStats = domain.MovementStats

// PositioningStats aliases domain.PositioningStats as canonical SSoT.
type PositioningStats = domain.PositioningStats

// DemoStats aliases domain.DemoStats as canonical SSoT.
type DemoStats = domain.DemoStats

// CameraSettings aliases domain.CameraSettings as canonical SSoT.
type CameraSettings = domain.CameraSettings

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

// PlayerRank aliases domain.PlayerRank as canonical SSoT.
type PlayerRank = domain.PlayerRank

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

// MapPlaylistSlugToID maps Ballchasing playlist slugs or string IDs to numeric Rocket League playlist IDs.
func MapPlaylistSlugToID(slug string) int {
	if id := domain.MapPlaylistSlugToID(slug); id > 0 {
		return id
	}
	normalized := strings.TrimSpace(strings.ToLower(slug))
	switch normalized {
	case "ranked-duel", "1v1":
		return 10
	case "ranked-double", "2v2":
		return 11
	case "solo-standard":
		return 12
	case "3v3":
		return 13
	case "ranked-snowday":
		return 30
	case "casual-duels", "unranked-duel", "unranked-duels":
		return 1
	default:
		// Attempt numeric parse if already an integer string (e.g. "22", "100")
		if id, err := strconv.Atoi(normalized); err == nil && id > 0 {
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
	RateLimiter  *RateLimiter
}
