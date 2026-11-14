package syncer

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/dank/rl-api-utils/internal/storage"
)

// Sync is an alias for RunCycle, executing a single synchronization pass.
func (s *Syncer) Sync(ctx context.Context) (*SyncStats, error) {
	return s.RunCycle(ctx)
}

// RunCycle executes a complete serialized synchronization cycle:
// 1. Recover in-flight orphan records (DOWNLOADING/UPLOADING/SYNCING -> PENDING).
// 2. Poll recent match history from PsyNet.
// 3. Upsert discovered matches into the persistent store (normalizing ReplayURL).
// 4. Download pending replays to the local replay directory.
// 5. Upload pending downloaded replays to Ballchasing.com.
// 6. Poll and sync detailed stats for uploaded / duplicate replays.
// 7. Return comprehensive cycle statistics.
func (s *Syncer) RunCycle(ctx context.Context) (*SyncStats, error) {
	s.cycleMu.Lock()
	defer s.cycleMu.Unlock()

	return s.runCycleLocked(ctx)
}

func (s *Syncer) runCycleLocked(ctx context.Context) (*SyncStats, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	stats := &SyncStats{}

	if !s.dryRun {
		if err := s.store.RecoverInFlight(ctx); err != nil {
			if isContextCanceled(ctx, err) {
				return nil, contextCancelErr(ctx, err)
			}
			return nil, fmt.Errorf("recovery failed: %w", err)
		}
	}

	discovered, err := s.provider.GetRecentMatches(ctx)
	if err != nil {
		if isContextCanceled(ctx, err) {
			return nil, contextCancelErr(ctx, err)
		}
		return nil, fmt.Errorf("polling matches failed: %w", err)
	}
	stats.DiscoveredCount = len(discovered)

	toUpsert := s.buildUpsertRecords(discovered, stats)

	if s.dryRun {
		s.logger.Info("[DRY-RUN] Discovered matches from PsyNet", slog.Int("count", len(discovered)))
		stats.PopulateAliases()
		return stats, nil
	}

	if len(toUpsert) > 0 {
		if err := s.store.UpsertDiscoveredMatches(ctx, toUpsert); err != nil {
			if isContextCanceled(ctx, err) {
				return nil, contextCancelErr(ctx, err)
			}
			return nil, fmt.Errorf("upserting discovered matches failed: %w", err)
		}
	}

	if err := s.downloadPendingReplays(ctx, stats); err != nil {
		stats.PopulateAliases()
		if isContextCanceled(ctx, err) {
			return stats, contextCancelErr(ctx, err)
		}
		return nil, err
	}

	if err := s.uploadPendingReplays(ctx, stats); err != nil {
		stats.PopulateAliases()
		if isContextCanceled(ctx, err) {
			return stats, contextCancelErr(ctx, err)
		}
		return nil, err
	}

	if err := s.pollAndSyncPendingStatsLocked(ctx); err != nil {
		if isContextCanceled(ctx, err) {
			stats.PopulateAliases()
			return stats, contextCancelErr(ctx, err)
		}
		s.logger.Warn("polling pending replay stats encountered error",
			slog.String("error", err.Error()),
		)
	}

	stats.PopulateAliases()
	return stats, nil
}

func (s *Syncer) buildUpsertRecords(discovered []DiscoveredMatch, stats *SyncStats) []*storage.MatchRecord {
	var toUpsert []*storage.MatchRecord
	for _, d := range discovered {
		guid := strings.TrimSpace(d.MatchGUID)
		if guid == "" {
			s.logger.Warn("skipping match entry with empty MatchGUID")
			continue
		}

		cleanURL := strings.TrimSpace(d.ReplayURL)
		dlStatus := storage.DownloadPending
		if cleanURL == "" {
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
			ReplayURL:            cleanURL,
			DownloadStatus:       dlStatus,
			UploadStatus:         storage.UploadPending,
		})
	}
	return toUpsert
}

func (s *Syncer) downloadPendingReplays(ctx context.Context, stats *SyncStats) error {
	pendingDownloads, err := s.store.ListPendingDownloads(ctx)
	if err != nil {
		if isContextCanceled(ctx, err) {
			return contextCancelErr(ctx, err)
		}
		return fmt.Errorf("list pending downloads failed: %w", err)
	}
	if len(pendingDownloads) == 0 {
		return nil
	}

	if s.maxConcurrentDownloads <= 1 || len(pendingDownloads) == 1 {
		for _, rec := range pendingDownloads {
			if _, cancelErr := s.downloadOneMatch(ctx, rec, nil, stats); cancelErr != nil {
				return cancelErr
			}
		}
		return nil
	}

	workers := min(s.maxConcurrentDownloads, len(pendingDownloads))
	jobs := make(chan *storage.MatchRecord)
	var (
		wg        sync.WaitGroup
		statsMu   sync.Mutex
		cancelMu  sync.Mutex
		cancelErr error
	)
	setCancelErr := func(err error) {
		cancelMu.Lock()
		if cancelErr == nil {
			cancelErr = err
		}
		cancelMu.Unlock()
	}

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for rec := range jobs {
				if err := ctx.Err(); err != nil {
					setCancelErr(err)
					return
				}
				if _, cErr := s.downloadOneMatch(ctx, rec, &statsMu, stats); cErr != nil {
					setCancelErr(cErr)
					return
				}
			}
		}()
	}

	for _, rec := range pendingDownloads {
		select {
		case <-ctx.Done():
			setCancelErr(ctx.Err())
			close(jobs)
			wg.Wait()
			return cancelErr
		case jobs <- rec:
		}
	}
	close(jobs)
	wg.Wait()
	return cancelErr
}

func (s *Syncer) downloadOneMatch(ctx context.Context, rec *storage.MatchRecord, statsMu *sync.Mutex, stats *SyncStats) (opErr error, cancelErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cleanURL := strings.TrimSpace(rec.ReplayURL)
	if cleanURL == "" {
		withOptMutex(statsMu, func() { stats.SkippedCount++ })
		return nil, nil
	}

	if latest, err := s.store.GetMatch(ctx, rec.MatchGUID); err == nil && latest != nil && latest.DownloadStatus == storage.DownloadDownloaded {
		return nil, nil
	}

	if err := s.store.MarkDownloading(ctx, rec.MatchGUID); err != nil {
		if isContextCanceled(ctx, err) {
			return nil, contextCancelErr(ctx, err)
		}
		s.logger.Warn("failed to mark match downloading", slog.String("guid", rec.MatchGUID), slog.String("error", err.Error()))
		return err, nil
	}

	localPath, err := s.downloader.DownloadReplay(ctx, rec.MatchGUID, cleanURL, s.replayDir)
	if err != nil {
		if isContextCanceled(ctx, err) {
			return nil, contextCancelErr(ctx, err)
		}
		_ = s.store.MarkDownloadFailed(ctx, rec.MatchGUID, err.Error())
		withOptMutex(statsMu, func() { stats.FailedCount++ })
		s.logger.Error("failed to download replay", slog.String("guid", rec.MatchGUID), slog.String("error", err.Error()))
		return err, nil
	}

	if err := s.store.MarkDownloaded(ctx, rec.MatchGUID, localPath); err != nil {
		if isContextCanceled(ctx, err) {
			return nil, contextCancelErr(ctx, err)
		}
		s.logger.Error("failed to mark match downloaded", slog.String("guid", rec.MatchGUID), slog.String("error", err.Error()))
		withOptMutex(statsMu, func() { stats.FailedCount++ })
		return err, nil
	}

	withOptMutex(statsMu, func() { stats.DownloadedCount++ })
	s.logger.Info("downloaded replay successfully", slog.String("guid", rec.MatchGUID), slog.String("local_path", localPath))

	if syncErr := s.SyncDownloadedReplayStats(ctx, rec.MatchGUID, localPath); syncErr != nil {
		if isContextCanceled(ctx, syncErr) {
			return nil, contextCancelErr(ctx, syncErr)
		}
		s.logger.Warn("failed to sync replay stats for downloaded replay",
			slog.String("guid", rec.MatchGUID), slog.String("local_path", localPath), slog.Any("error", syncErr))
	}

	return nil, nil
}

func (s *Syncer) uploadPendingReplays(ctx context.Context, stats *SyncStats) error {
	pendingUploads, err := s.store.ListPendingUploads(ctx)
	if err != nil {
		if isContextCanceled(ctx, err) {
			return contextCancelErr(ctx, err)
		}
		return fmt.Errorf("list pending uploads failed: %w", err)
	}

	for _, rec := range pendingUploads {
		if _, cancelErr := s.uploadOneMatch(ctx, rec, stats); cancelErr != nil {
			return cancelErr
		}
	}

	return nil
}

func (s *Syncer) uploadOneMatch(ctx context.Context, rec *storage.MatchRecord, stats *SyncStats) (opErr error, cancelErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	localPath := strings.TrimSpace(rec.LocalFilePath)
	if localPath == "" {
		s.logger.Warn("skipping pending upload with empty local file path", slog.String("guid", rec.MatchGUID))
		return nil, nil
	}

	if latest, err := s.store.GetMatch(ctx, rec.MatchGUID); err == nil && latest != nil {
		if latest.UploadStatus == storage.UploadUploaded || latest.UploadStatus == storage.UploadDuplicate {
			return nil, nil
		}
	}

	if err := s.store.MarkUploading(ctx, rec.MatchGUID); err != nil {
		if isContextCanceled(ctx, err) {
			return nil, contextCancelErr(ctx, err)
		}
		s.logger.Warn("failed to mark match uploading", slog.String("guid", rec.MatchGUID), slog.String("error", err.Error()))
		return err, nil
	}

	res, err := s.uploader.UploadReplay(ctx, rec.MatchGUID, localPath)
	if err != nil {
		if isContextCanceled(ctx, err) {
			return nil, contextCancelErr(ctx, err)
		}
		_ = s.store.MarkUploadFailed(ctx, rec.MatchGUID, err.Error())
		stats.FailedCount++
		s.logger.Error("failed to upload replay", slog.String("guid", rec.MatchGUID), slog.String("error", err.Error()))
		return err, nil
	}

	if res != nil && res.IsDuplicate {
		if err := s.store.MarkDuplicate(ctx, rec.MatchGUID, res.ID, res.Location); err != nil {
			if isContextCanceled(ctx, err) {
				return nil, contextCancelErr(ctx, err)
			}
			s.logger.Error("failed to mark match duplicate", slog.String("guid", rec.MatchGUID), slog.String("error", err.Error()))
			stats.FailedCount++
			return err, nil
		}
		stats.DuplicateCount++
		s.logger.Info("replay already uploaded (duplicate marked)", slog.String("guid", rec.MatchGUID), slog.String("ballchasing_id", res.ID))
	} else {
		id, loc := "", ""
		if res != nil {
			id, loc = res.ID, res.Location
		}
		if err := s.store.MarkUploaded(ctx, rec.MatchGUID, id, loc); err != nil {
			if isContextCanceled(ctx, err) {
				return nil, contextCancelErr(ctx, err)
			}
			s.logger.Error("failed to mark match uploaded", slog.String("guid", rec.MatchGUID), slog.String("error", err.Error()))
			stats.FailedCount++
			return err, nil
		}
		stats.UploadedCount++
		s.logger.Info("uploaded replay successfully", slog.String("guid", rec.MatchGUID), slog.String("ballchasing_id", id))
	}

	if !s.keepLocalFiles && localPath != "" {
		_ = os.Remove(localPath)
	}
	return nil, nil
}

func withOptMutex(mu *sync.Mutex, fn func()) {
	if mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	fn()
}
