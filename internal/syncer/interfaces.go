package syncer

import (
	"context"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/psynet"
	"github.com/dank/rl-api-utils/internal/storage"
)

// DiscoveredMatch aliases psynet.DiscoveredMatch for cross-package structural identity.
type DiscoveredMatch = psynet.DiscoveredMatch

// UploadResult aliases ballchasing.UploadResult for cross-package structural identity.
type UploadResult = ballchasing.UploadResult

// SyncStats captures telemetry and metrics for a single synchronization cycle.
type SyncStats struct {
	DiscoveredCount int `json:"discovered_count"`
	DownloadedCount int `json:"downloaded_count"`
	UploadedCount   int `json:"uploaded_count"`
	DuplicateCount  int `json:"duplicate_count"`
	SkippedCount    int `json:"skipped_count"`
	FailedCount     int `json:"failed_count"`

	// Convenience alias fields matching caller conventions
	Discovered int `json:"discovered"`
	Downloaded int `json:"downloaded"`
	Uploaded   int `json:"uploaded"`
	Duplicates int `json:"duplicates"`
	Skipped    int `json:"skipped"`
	Failures   int `json:"failures"`
}

// PopulateAliases syncs convenience alias fields with their canonical *Count counterparts.
func (s *SyncStats) PopulateAliases() {
	s.Discovered = s.DiscoveredCount
	s.Downloaded = s.DownloadedCount
	s.Uploaded = s.UploadedCount
	s.Duplicates = s.DuplicateCount
	s.Skipped = s.SkippedCount
	s.Failures = s.FailedCount
}

// StateStore defines the persistence contract required by the syncer domain orchestrator.
// Satisfied by *storage.SQLiteStore and *storage.JSONStore.
type StateStore interface {
	GetMatch(ctx context.Context, matchGUID string) (*storage.MatchRecord, error)
	ListPendingDownloads(ctx context.Context) ([]*storage.MatchRecord, error)
	ListPendingUploads(ctx context.Context) ([]*storage.MatchRecord, error)
	UpsertDiscoveredMatches(ctx context.Context, matches []*storage.MatchRecord) error
	MarkDownloading(ctx context.Context, matchGUID string) error
	MarkDownloaded(ctx context.Context, matchGUID, localPath string) error
	MarkDownloadFailed(ctx context.Context, matchGUID, errMsg string) error
	MarkUploading(ctx context.Context, matchGUID string) error
	MarkUploaded(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error
	MarkDuplicate(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error
	MarkUploadFailed(ctx context.Context, matchGUID, errMsg string) error
	RecoverInFlight(ctx context.Context) error
}

// MatchHistoryProvider defines the match query contract for PsyNet or mock providers.
// Satisfied by *psynet.Client and testutil.InMemoryMatchHistoryProvider.
type MatchHistoryProvider interface {
	GetRecentMatches(ctx context.Context) ([]DiscoveredMatch, error)
	Close() error
}

// ReplayDownloader defines the replay file streaming download contract.
// Satisfied by *psynet.HTTPDownloader.
type ReplayDownloader interface {
	DownloadReplay(ctx context.Context, matchGUID, replayURL, destDir string) (localPath string, err error)
}

// ReplayUploader defines the replay upload contract for Ballchasing or mock uploaders.
// Satisfied by *ballchasing.Client.
type ReplayUploader interface {
	UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error)
}
