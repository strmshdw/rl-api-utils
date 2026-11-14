package syncer

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/psynet"
	"github.com/dank/rl-api-utils/internal/storage"
)

// Sentinel errors returned by the syncer domain package.
var (
	ErrNilStore      = errors.New("syncer: state store cannot be nil")
	ErrNilProvider   = errors.New("syncer: match history provider cannot be nil")
	ErrNilDownloader = errors.New("syncer: replay downloader cannot be nil")
	ErrNilUploader   = errors.New("syncer: replay uploader cannot be nil")
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

	// Convenience alias fields matching caller conventions.
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
	SyncReplayStats(ctx context.Context, stats *storage.ReplayMatchStats) error

	ListPendingStatsSync(ctx context.Context, maxRetries int) ([]*storage.MatchRecord, error)
	MarkStatsSyncing(ctx context.Context, matchGUID string) error
	MarkStatsSynced(ctx context.Context, matchGUID string) error
	MarkStatsSyncFailed(ctx context.Context, matchGUID, errMsg string) error
	ResetMatchRetry(ctx context.Context, matchGUID string) error
	RecordMatchResults(ctx context.Context, matchGUID string, playlistID int, outcomes []storage.PlayerOutcome, matchTime ...time.Time) error
	SaveMatchPlayers(ctx context.Context, matchGUID string, players []*storage.MatchPlayerRecord) error
	GetAuthState(ctx context.Context, provider string) (refreshToken, accountID, displayName string, err error)
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

// ReplayStatsFetcher defines the capability to retrieve detailed replay telemetry.
// Satisfied by *ballchasing.Client.
type ReplayStatsFetcher interface {
	GetReplay(ctx context.Context, id string) (*ballchasing.ReplayItem, error)
}

type rateLimiterProvider interface {
	RateLimiter() *ballchasing.RateLimiter
}

// Config holds configuration parameters for the syncer orchestrator.
type Config struct {
	ReplayDir       string
	DryRun          bool
	KeepLocalFiles  bool
	LocalPlayerID   string
	LocalPlayerName string
	Logger          *slog.Logger
}

// Option allows configuring optional Syncer settings.
type Option func(*Syncer)

// WithReplayDir configures the directory where replay files are downloaded.
func WithReplayDir(dir string) Option {
	return func(s *Syncer) {
		trimmed := strings.TrimSpace(dir)
		if trimmed != "" {
			s.replayDir = trimmed
		}
	}
}

// WithDryRun configures whether the syncer operates in simulation mode.
func WithDryRun(dryRun bool) Option {
	return func(s *Syncer) {
		s.dryRun = dryRun
	}
}

// WithLogger configures a custom structured logger.
func WithLogger(logger *slog.Logger) Option {
	return func(s *Syncer) {
		if logger != nil {
			s.logger = logger
		}
	}
}

// WithKeepLocalFiles configures whether local .replay files are kept after upload.
func WithKeepLocalFiles(keep bool) Option {
	return func(s *Syncer) {
		s.keepLocalFiles = keep
	}
}

// WithStatsFetcher configures a custom replay stats fetcher for Ballchasing API queries.
func WithStatsFetcher(fetcher ReplayStatsFetcher) Option {
	return func(s *Syncer) {
		s.statsFetcher = fetcher
	}
}

// WithLocalPlayerID configures the local player ID for multi-identity player resolution.
func WithLocalPlayerID(id string) Option {
	return func(s *Syncer) {
		s.localPlayerID = strings.TrimSpace(id)
	}
}

// WithLocalPlayerName configures the local player name for multi-identity player resolution.
func WithLocalPlayerName(name string) Option {
	return func(s *Syncer) {
		s.localPlayerName = strings.TrimSpace(name)
	}
}

// WithMaxRetries configures the maximum retries for sync operations.
func WithMaxRetries(retries int) Option {
	return func(s *Syncer) {
		if retries > 0 {
			s.maxRetries = retries
		}
	}
}

// WithRateInterval configures the pacing interval between external stats queries.
func WithRateInterval(interval time.Duration) Option {
	return func(s *Syncer) {
		s.rateInterval = interval
	}
}

// WithRateLimiter configures a shared Ballchasing rate limiter across sync components.
func WithRateLimiter(rl *ballchasing.RateLimiter) Option {
	return func(s *Syncer) {
		s.rateLimiter = rl
	}
}

// WithMaxConcurrentDownloads configures the download worker pool concurrency (defaults to 1).
func WithMaxConcurrentDownloads(n int) Option {
	return func(s *Syncer) {
		if n > 0 {
			s.maxConcurrentDownloads = n
		}
	}
}
