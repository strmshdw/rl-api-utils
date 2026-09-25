package ballchasing

import (
	"context"
	"errors"
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

	// ErrFileNotFound is returned when the specified replay file does not exist on disk.
	ErrFileNotFound = errors.New("ballchasing: replay file not found")
)

// UploadResult captures the outcome of a replay upload.
// Directly satisfies the syncer.UploadResult contract.
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
