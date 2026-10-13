package ballchasing

import (
	"context"
	"errors"
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
type ReplayItem struct {
	ID           string     `json:"id"`
	Link         string     `json:"link"`
	Created      string     `json:"created"`
	Date         string     `json:"date"`
	MatchGUID    string     `json:"match_guid"`
	MapCode      string     `json:"map_code"`
	MapName      string     `json:"map_name"`
	PlaylistID   string     `json:"playlist_id"`
	PlaylistName string     `json:"playlist_name"`
	Duration     int        `json:"duration"`
	Overtime     bool       `json:"overtime"`
	Blue         ReplayTeam `json:"blue"`
	Orange       ReplayTeam `json:"orange"`
}

// ReplayTeam represents a team in a Ballchasing replay.
type ReplayTeam struct {
	Color   string         `json:"color"`
	Name    string         `json:"name"`
	Goals   int            `json:"goals"`
	Players []ReplayPlayer `json:"players"`
}

// ReplayPlayer represents a player in a Ballchasing replay team.
type ReplayPlayer struct {
	Name    string         `json:"name"`
	ID      PlayerIdentity `json:"id"`
	Score   int            `json:"score"`
	Goals   int            `json:"goals"`
	Assists int            `json:"assists"`
	Saves   int            `json:"saves"`
	Shots   int            `json:"shots"`
	Demos   PlayerDemos    `json:"demos"`
}

// PlayerIdentity contains player platform and platform account ID.
type PlayerIdentity struct {
	Platform string `json:"platform"`
	ID       string `json:"id"`
}

// PlayerDemos contains demolition statistics for a player.
type PlayerDemos struct {
	Inflicted int `json:"inflicted"`
	Taken     int `json:"taken"`
}

// MapPlaylistSlugToID maps Ballchasing playlist slugs or string IDs to numeric Rocket League playlist IDs.
func MapPlaylistSlugToID(slug string) int {
	slug = strings.TrimSpace(strings.ToLower(slug))
	switch slug {
	case "ranked-duels", "ranked-duel", "1v1", "10":
		return 10
	case "ranked-doubles", "ranked-double", "2v2", "11":
		return 11
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
	case "casual-standard", "3":
		return 3
	case "casual-doubles", "2":
		return 2
	case "casual-duels", "casual-duel", "1":
		return 1
	case "casual-chaos", "4":
		return 4
	default:
		// Attempt numeric parse if already an integer
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
