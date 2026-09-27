package storage

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Sentinel errors for the storage layer.
var (
	ErrMatchNotFound         = errors.New("match not found")
	ErrAuthStateNotFound     = errors.New("auth state not found")
	ErrInvalidGUID           = errors.New("match GUID cannot be empty")
	ErrStoreClosed           = errors.New("state store is closed")
	ErrPlayerNotFound        = errors.New("player not found")
	ErrMatchAlreadyProcessed = errors.New("match outcome already processed")
)

// DownloadStatus represents the synchronization status of a replay download.
type DownloadStatus string

const (
	DownloadPending     DownloadStatus = "PENDING"
	DownloadDownloading DownloadStatus = "DOWNLOADING"
	DownloadDownloaded  DownloadStatus = "DOWNLOADED"
	DownloadFailed      DownloadStatus = "FAILED"
	DownloadSkipped     DownloadStatus = "SKIPPED"
)

// UploadStatus represents the synchronization status of a replay upload to Ballchasing.
type UploadStatus string

const (
	UploadPending   UploadStatus = "PENDING"
	UploadUploading UploadStatus = "UPLOADING"
	UploadUploaded  UploadStatus = "UPLOADED"
	UploadDuplicate UploadStatus = "DUPLICATE"
	UploadFailed    UploadStatus = "FAILED"
)

// MatchRecord represents the persistent state of a single Rocket League match.
type MatchRecord struct {
	MatchGUID            string         `json:"match_guid"`
	RecordStartTimestamp int64          `json:"record_start_timestamp"`
	MapName              string         `json:"map_name"`
	Playlist             int            `json:"playlist"`
	ReplayURL            string         `json:"replay_url"`
	DownloadStatus       DownloadStatus `json:"download_status"`
	LocalFilePath        string         `json:"local_file_path"`
	DownloadedAt         *time.Time     `json:"downloaded_at,omitempty"`
	UploadStatus         UploadStatus   `json:"upload_status"`
	BallchasingID        string         `json:"ballchasing_id"`
	BallchasingURL       string         `json:"ballchasing_url"`
	UploadedAt           *time.Time     `json:"uploaded_at,omitempty"`
	RetryCount           int            `json:"retry_count"`
	LastError            string         `json:"last_error"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
}

// AuthRecord represents persisted authentication tokens and metadata for an auth provider.
type AuthRecord struct {
	Provider     string    `json:"provider"`
	RefreshToken string    `json:"refresh_token"`
	AccountID    string    `json:"account_id"`
	DisplayName  string    `json:"display_name"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// PlayerRecord represents the persistent profile of an encountered player.
type PlayerRecord struct {
	PlayerID    string    `json:"player_id"`
	Platform    string    `json:"platform"`
	PlayerName  string    `json:"player_name"`
	RanksJSON   string    `json:"ranks_json"`
	FirstSeenAt time.Time `json:"first_seen_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
}

// PlayerMatchup represents historical head-to-head records with/against a player in a specific playlist.
type PlayerMatchup struct {
	PlayerID         string    `json:"player_id"`
	PlaylistID       int       `json:"playlist_id"`
	WinsAsTeammate   int       `json:"wins_as_teammate"`
	LossesAsTeammate int       `json:"losses_as_teammate"`
	WinsAsOpponent   int       `json:"wins_as_opponent"`
	LossesAsOpponent int       `json:"losses_as_opponent"`
	TotalMatches     int       `json:"total_matches"`
	LastPlayedAt     time.Time `json:"last_played_at"`
}

// PlayerSummary embeds PlayerRecord with aggregate matchup totals across all playlists.
type PlayerSummary struct {
	PlayerRecord
	TotalWinsAsTeammate   int `json:"total_wins_as_teammate"`
	TotalLossesAsTeammate int `json:"total_losses_as_teammate"`
	TotalWinsAsOpponent   int `json:"total_wins_as_opponent"`
	TotalLossesAsOpponent int `json:"total_losses_as_opponent"`
	TotalMatches          int `json:"total_matches"`
}

// PlayerOutcome specifies the result for a single player in a concluded match.
type PlayerOutcome struct {
	PlayerID   string `json:"player_id"`
	Platform   string `json:"platform,omitempty"`
	PlayerName string `json:"player_name,omitempty"`
	IsTeammate bool   `json:"is_teammate"`
	Won        bool   `json:"won"`
}

// StateStore defines the persistence interface required by the syncer and daemon.
type StateStore interface {
	// GetMatch retrieves a single match record by its GUID.
	// Returns ErrMatchNotFound if the record does not exist.
	GetMatch(ctx context.Context, matchGUID string) (*MatchRecord, error)

	// ListPendingDownloads returns all matches ready for replay downloading
	// (download_status = PENDING and non-empty replay_url), ordered by timestamp ASC.
	ListPendingDownloads(ctx context.Context) ([]*MatchRecord, error)

	// ListPendingUploads returns all matches ready for upload
	// (download_status = DOWNLOADED and upload_status = PENDING and non-empty local path).
	ListPendingUploads(ctx context.Context) ([]*MatchRecord, error)

	// UpsertDiscoveredMatches inserts newly discovered matches without clobbering
	// existing download/upload statuses or metadata.
	UpsertDiscoveredMatches(ctx context.Context, matches []*MatchRecord) error

	// Status transition methods
	MarkDownloading(ctx context.Context, matchGUID string) error
	MarkDownloaded(ctx context.Context, matchGUID, localPath string) error
	MarkDownloadFailed(ctx context.Context, matchGUID, errMsg string) error
	MarkUploading(ctx context.Context, matchGUID string) error
	MarkUploaded(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error
	MarkDuplicate(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error
	MarkUploadFailed(ctx context.Context, matchGUID, errMsg string) error

	// RecoverInFlight resets in-flight transitions (DOWNLOADING -> PENDING, UPLOADING -> PENDING)
	// after an abnormal daemon termination or crash.
	RecoverInFlight(ctx context.Context) error

	// Auth token persistence
	SaveAuthState(ctx context.Context, provider, refreshToken, accountID, displayName string) error
	GetAuthState(ctx context.Context, provider string) (refreshToken, accountID, displayName string, err error)

	// Close closes the underlying store connection.
	Close() error

	// --- Player Tracking Extensions (Milestone M1) ---

	// UpsertPlayer inserts a new player or updates an existing player's platform, name, and last_seen_at.
	// If the player already exists, first_seen_at is preserved and ranks_json is preserved unless updated.
	UpsertPlayer(ctx context.Context, player *PlayerRecord) error

	// GetPlayer retrieves a single player profile by PlayerID.
	// Returns ErrPlayerNotFound if the player does not exist.
	GetPlayer(ctx context.Context, playerID string) (*PlayerRecord, error)

	// ListPlayers returns a paginated list of players ordered by last_seen_at DESC, player_id ASC.
	ListPlayers(ctx context.Context, limit, offset int) ([]*PlayerRecord, error)

	// ListPlayerSummaries returns paginated players with aggregated win/loss statistics across all playlists,
	// ordered by last_seen_at DESC, player_id ASC.
	ListPlayerSummaries(ctx context.Context, limit, offset int) ([]*PlayerSummary, error)

	// SearchPlayerSummaries searches players matching a query string across player_name and player_id
	// (case-insensitive substring match), optionally filtered by platform (case-insensitive; "all" or "" means
	// all platforms), returning paginated PlayerSummary records and the total count of matching records across all pages.
	// Results are ordered by last_seen_at DESC, player_id ASC.
	SearchPlayerSummaries(ctx context.Context, query, platform string, limit, offset int) ([]*PlayerSummary, int, error)

	// UpdatePlayerRanks updates the ranks_json payload and last_seen_at for a player.
	// Returns ErrPlayerNotFound if the player does not exist.
	UpdatePlayerRanks(ctx context.Context, playerID string, ranksJSON string) error

	// RecordMatchResults atomically records match outcomes for all players in a match and registers
	// the match GUID in processed_match_outcomes. If the match was already processed, it returns
	// ErrMatchAlreadyProcessed and performs no updates to player or matchup records.
	RecordMatchResults(ctx context.Context, matchGUID string, playlistID int, outcomes []PlayerOutcome) error

	// GetPlayerMatchup retrieves the head-to-head record for a player in a specific playlist.
	// If no record exists for that playlist, returns a zeroed PlayerMatchup (TotalMatches=0) and nil error.
	GetPlayerMatchup(ctx context.Context, playerID string, playlistID int) (*PlayerMatchup, error)

	// GetPlayerMatchups retrieves all playlist matchups for a player ordered by playlist_id ASC.
	GetPlayerMatchups(ctx context.Context, playerID string) ([]*PlayerMatchup, error)
}

// NewStore initializes a StateStore backend based on the database path or URI.
// If dbPath ends in ".json", a JSONStore is returned; otherwise SQLiteStore is used.
func NewStore(dbPath string) (StateStore, error) {
	if strings.HasSuffix(strings.ToLower(dbPath), ".json") {
		return NewJSONStore(dbPath)
	}
	return NewSQLiteStore(dbPath)
}
