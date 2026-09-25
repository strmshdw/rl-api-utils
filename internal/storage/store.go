package storage

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Sentinel errors for the storage layer.
var (
	ErrMatchNotFound     = errors.New("match not found")
	ErrAuthStateNotFound = errors.New("auth state not found")
	ErrInvalidGUID       = errors.New("match GUID cannot be empty")
	ErrStoreClosed       = errors.New("state store is closed")
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
}

// NewStore initializes a StateStore backend based on the database path or URI.
// If dbPath ends in ".json", a JSONStore is returned; otherwise SQLiteStore is used.
func NewStore(dbPath string) (StateStore, error) {
	if strings.HasSuffix(strings.ToLower(dbPath), ".json") {
		return NewJSONStore(dbPath)
	}
	return NewSQLiteStore(dbPath)
}
