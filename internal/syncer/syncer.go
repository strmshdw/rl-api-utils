package syncer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/dank/rl-api-utils/internal/storage"
)

// Sentinel errors returned by the syncer domain package.
var (
	ErrNilStore      = errors.New("syncer: state store cannot be nil")
	ErrNilProvider   = errors.New("syncer: match history provider cannot be nil")
	ErrNilDownloader = errors.New("syncer: replay downloader cannot be nil")
	ErrNilUploader   = errors.New("syncer: replay uploader cannot be nil")
)

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

// Config holds configuration parameters for the syncer orchestrator.
type Config struct {
	ReplayDir      string
	DryRun         bool
	KeepLocalFiles bool
	Logger         *slog.Logger
}

// Syncer is the domain orchestrator that coordinates match discovery,
// replay downloading, Ballchasing uploads, and state persistence.
type Syncer struct {
	store          StateStore
	provider       MatchHistoryProvider
	downloader     ReplayDownloader
	uploader       ReplayUploader
	replayDir      string
	dryRun         bool
	keepLocalFiles bool
	logger         *slog.Logger
}

// New creates a new Syncer instance with functional options.
func New(store StateStore, provider MatchHistoryProvider, downloader ReplayDownloader, uploader ReplayUploader, opts ...Option) (*Syncer, error) {
	if store == nil {
		return nil, ErrNilStore
	}
	if provider == nil {
		return nil, ErrNilProvider
	}
	if downloader == nil {
		return nil, ErrNilDownloader
	}
	if uploader == nil {
		return nil, ErrNilUploader
	}

	s := &Syncer{
		store:          store,
		provider:       provider,
		downloader:     downloader,
		uploader:       uploader,
		replayDir:      "./replays",
		dryRun:         false,
		keepLocalFiles: true,
		logger:         slog.Default(),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s, nil
}

// NewWithConfig creates a new Syncer instance with a Config struct.
func NewWithConfig(store StateStore, provider MatchHistoryProvider, downloader ReplayDownloader, uploader ReplayUploader, cfg Config) (*Syncer, error) {
	return New(store, provider, downloader, uploader,
		WithReplayDir(cfg.ReplayDir),
		WithDryRun(cfg.DryRun),
		WithKeepLocalFiles(cfg.KeepLocalFiles),
		WithLogger(cfg.Logger),
	)
}

// NewSyncerEngine creates a new Syncer matching E2E test harness conventions.
func NewSyncerEngine(store StateStore, provider MatchHistoryProvider, downloader ReplayDownloader, uploader ReplayUploader, replayDir string, dryRun bool) *Syncer {
	s, err := New(store, provider, downloader, uploader,
		WithReplayDir(replayDir),
		WithDryRun(dryRun),
	)
	if err != nil {
		panic(err)
	}
	return s
}

// Sync is an alias for RunCycle, executing a single synchronization pass.
func (s *Syncer) Sync(ctx context.Context) (*SyncStats, error) {
	return s.RunCycle(ctx)
}

// RunCycle executes a complete synchronization cycle:
// 1. Recover in-flight orphan records (DOWNLOADING/UPLOADING -> PENDING).
// 2. Poll recent match history from PsyNet.
// 3. Upsert discovered matches into the persistent store.
// 4. Download pending replays to the local replay directory.
// 5. Upload pending downloaded replays to Ballchasing.com.
// 6. Return comprehensive cycle statistics.
func (s *Syncer) RunCycle(ctx context.Context) (*SyncStats, error) {
	stats := &SyncStats{}

	// 1. Recover in-flight items from previous crashes (skipped in dry-run to keep store read-only)
	if !s.dryRun {
		if err := s.store.RecoverInFlight(ctx); err != nil {
			return nil, fmt.Errorf("recovery failed: %w", err)
		}
	}

	// 2. Poll matches from PsyNet
	discovered, err := s.provider.GetRecentMatches(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("polling matches failed: %w", err)
	}
	stats.DiscoveredCount = len(discovered)

	// 3. Process discovered matches
	var toUpsert []*storage.MatchRecord
	for _, d := range discovered {
		guid := strings.TrimSpace(d.MatchGUID)
		if guid == "" {
			s.logger.Warn("skipping match entry with empty MatchGUID")
			continue
		}

		dlStatus := storage.DownloadPending
		if strings.TrimSpace(d.ReplayURL) == "" {
			dlStatus = storage.DownloadSkipped
			stats.SkippedCount++
			s.logger.Debug("match discovered without replay URL (delayed CDN arrival)",
				slog.String("guid", guid),
				slog.Int("playlist", d.Playlist),
				slog.String("map", d.MapName),
			)
		}

		toUpsert = append(toUpsert, &storage.MatchRecord{
			MatchGUID:            guid,
			RecordStartTimestamp: d.RecordStartTimestamp,
			MapName:              d.MapName,
			Playlist:             d.Playlist,
			ReplayURL:            d.ReplayURL,
			DownloadStatus:       dlStatus,
			UploadStatus:         storage.UploadPending,
		})
	}

	if s.dryRun {
		s.logger.Info("[DRY-RUN] Discovered matches from PsyNet", slog.Int("count", len(discovered)))
		stats.PopulateAliases()
		return stats, nil
	}

	if len(toUpsert) > 0 {
		if err := s.store.UpsertDiscoveredMatches(ctx, toUpsert); err != nil {
			return nil, fmt.Errorf("upserting discovered matches failed: %w", err)
		}
	}

	// 4. Download pending replays
	pendingDownloads, err := s.store.ListPendingDownloads(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pending downloads failed: %w", err)
	}

	for _, rec := range pendingDownloads {
		select {
		case <-ctx.Done():
			stats.PopulateAliases()
			return stats, ctx.Err()
		default:
		}

		if strings.TrimSpace(rec.ReplayURL) == "" {
			stats.SkippedCount++
			continue
		}

		if err := s.store.MarkDownloading(ctx, rec.MatchGUID); err != nil {
			s.logger.Warn("failed to mark match downloading",
				slog.String("guid", rec.MatchGUID),
				slog.String("error", err.Error()),
			)
			continue
		}

		localPath, err := s.downloader.DownloadReplay(ctx, rec.MatchGUID, rec.ReplayURL, s.replayDir)
		if err != nil {
			if ctx.Err() != nil {
				stats.PopulateAliases()
				return stats, ctx.Err()
			}
			_ = s.store.MarkDownloadFailed(ctx, rec.MatchGUID, err.Error())
			stats.FailedCount++
			s.logger.Error("failed to download replay",
				slog.String("guid", rec.MatchGUID),
				slog.String("error", err.Error()),
			)
			continue
		}

		if err := s.store.MarkDownloaded(ctx, rec.MatchGUID, localPath); err != nil {
			s.logger.Error("failed to mark match downloaded",
				slog.String("guid", rec.MatchGUID),
				slog.String("error", err.Error()),
			)
			stats.FailedCount++
			continue
		}

		stats.DownloadedCount++
		s.logger.Info("downloaded replay successfully",
			slog.String("guid", rec.MatchGUID),
			slog.String("local_path", localPath),
		)
	}

	// 5. Upload pending replays
	pendingUploads, err := s.store.ListPendingUploads(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pending uploads failed: %w", err)
	}

	for _, rec := range pendingUploads {
		select {
		case <-ctx.Done():
			stats.PopulateAliases()
			return stats, ctx.Err()
		default:
		}

		if strings.TrimSpace(rec.LocalFilePath) == "" {
			s.logger.Warn("skipping pending upload with empty local file path",
				slog.String("guid", rec.MatchGUID),
			)
			continue
		}

		if err := s.store.MarkUploading(ctx, rec.MatchGUID); err != nil {
			s.logger.Warn("failed to mark match uploading",
				slog.String("guid", rec.MatchGUID),
				slog.String("error", err.Error()),
			)
			continue
		}

		res, err := s.uploader.UploadReplay(ctx, rec.MatchGUID, rec.LocalFilePath)
		if err != nil {
			if ctx.Err() != nil {
				stats.PopulateAliases()
				return stats, ctx.Err()
			}
			_ = s.store.MarkUploadFailed(ctx, rec.MatchGUID, err.Error())
			stats.FailedCount++
			s.logger.Error("failed to upload replay",
				slog.String("guid", rec.MatchGUID),
				slog.String("error", err.Error()),
			)
			continue
		}

		if res != nil && res.IsDuplicate {
			if err := s.store.MarkDuplicate(ctx, rec.MatchGUID, res.ID, res.Location); err != nil {
				s.logger.Error("failed to mark match duplicate",
					slog.String("guid", rec.MatchGUID),
					slog.String("error", err.Error()),
				)
				stats.FailedCount++
				continue
			}
			stats.DuplicateCount++
			s.logger.Info("replay already uploaded (duplicate marked)",
				slog.String("guid", rec.MatchGUID),
				slog.String("ballchasing_id", res.ID),
			)
		} else {
			id := ""
			loc := ""
			if res != nil {
				id = res.ID
				loc = res.Location
			}
			if err := s.store.MarkUploaded(ctx, rec.MatchGUID, id, loc); err != nil {
				s.logger.Error("failed to mark match uploaded",
					slog.String("guid", rec.MatchGUID),
					slog.String("error", err.Error()),
				)
				stats.FailedCount++
				continue
			}
			stats.UploadedCount++
			s.logger.Info("uploaded replay successfully",
				slog.String("guid", rec.MatchGUID),
				slog.String("ballchasing_id", id),
			)
		}

		// Clean up local file if KeepLocalFiles is false
		if !s.keepLocalFiles && rec.LocalFilePath != "" {
			_ = os.Remove(rec.LocalFilePath)
		}
	}

	// 6. Return populated cycle statistics
	stats.PopulateAliases()
	return stats, nil
}
