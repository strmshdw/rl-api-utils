package syncer

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dank/rl-api-utils/internal/ballchasing"
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
	_ = s.DisconnectProvider()
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

	parsedInfo, syncErr := s.syncDownloadedReplayStatsWithMeta(ctx, rec.MatchGUID, localPath)
	if syncErr != nil {
		if isContextCanceled(ctx, syncErr) {
			return nil, contextCancelErr(ctx, syncErr)
		}
		s.logger.Warn("failed to sync replay stats for downloaded replay",
			slog.String("guid", rec.MatchGUID), slog.String("local_path", localPath), slog.Any("error", syncErr))
	}

	finalPath, orgErr := s.organizeDownloadedReplay(ctx, rec, localPath, parsedInfo)
	if orgErr != nil {
		if isContextCanceled(ctx, orgErr) {
			return nil, contextCancelErr(ctx, orgErr)
		}
		s.logger.Warn("failed to organize/rename downloaded replay; retaining original path",
			slog.String("guid", rec.MatchGUID), slog.String("local_path", localPath), slog.Any("error", orgErr))
		finalPath = localPath
	}

	if err := s.store.MarkDownloaded(ctx, rec.MatchGUID, finalPath); err != nil {
		if isContextCanceled(ctx, err) {
			return nil, contextCancelErr(ctx, err)
		}
		s.logger.Error("failed to mark match downloaded", slog.String("guid", rec.MatchGUID), slog.String("error", err.Error()))
		withOptMutex(statsMu, func() { stats.FailedCount++ })
		return err, nil
	}

	withOptMutex(statsMu, func() { stats.DownloadedCount++ })
	s.logger.Info("downloaded replay successfully", slog.String("guid", rec.MatchGUID), slog.String("local_path", finalPath))

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
		rec = latest
		if strings.TrimSpace(latest.LocalFilePath) != "" {
			localPath = strings.TrimSpace(latest.LocalFilePath)
		}
	}

	// If localPath was renamed on disk by a concurrent worker, resolve the canonical ReplayName sibling path.
	if strings.TrimSpace(rec.ReplayName) != "" {
		if _, statErr := os.Stat(localPath); os.IsNotExist(statErr) {
			for _, cand := range []string{
				filepath.Join(filepath.Dir(localPath), rec.ReplayName),
				filepath.Join(s.replayDir, rec.ReplayName),
			} {
				if _, cErr := os.Stat(cand); cErr == nil {
					localPath = cand
					break
				}
			}
		}
	}

	// Lazily organize/rename any previously downloaded pending replay file that lacks a canonical name.
	if strings.TrimSpace(rec.ReplayName) == "" {
		if _, statErr := os.Stat(localPath); statErr == nil {
			if renamedPath, orgErr := s.organizeDownloadedReplay(ctx, rec, localPath, nil); orgErr == nil && renamedPath != "" {
				localPath = renamedPath
				if refreshed, getErr := s.store.GetMatch(ctx, rec.MatchGUID); getErr == nil && refreshed != nil {
					rec = refreshed
				}
			} else if isContextCanceled(ctx, orgErr) {
				return nil, contextCancelErr(ctx, orgErr)
			}
		}
	}

	if err := s.store.MarkUploading(ctx, rec.MatchGUID); err != nil {
		if isContextCanceled(ctx, err) {
			return nil, contextCancelErr(ctx, err)
		}
		s.logger.Warn("failed to mark match uploading", slog.String("guid", rec.MatchGUID), slog.String("error", err.Error()))
		return err, nil
	}

	groupInfo, grpErr := s.resolveOrCreateUploadGroup(ctx, rec)
	if grpErr != nil {
		if isContextCanceled(ctx, grpErr) {
			return nil, contextCancelErr(ctx, grpErr)
		}
		_ = s.store.MarkUploadFailed(ctx, rec.MatchGUID, grpErr.Error())
		stats.FailedCount++
		s.logger.Error("failed to resolve/create ballchasing group for replay upload",
			slog.String("guid", rec.MatchGUID), slog.String("error", grpErr.Error()))
		return grpErr, nil
	}

	uploadFilename := strings.TrimSpace(rec.ReplayName)
	groupID := ""
	if groupInfo != nil {
		groupID = groupInfo.GroupID
	}

	var (
		res *UploadResult
		err error
	)
	if optsUploader, ok := s.uploader.(ReplayUploaderWithOptions); ok && (uploadFilename != "" || groupID != "") {
		res, err = optsUploader.UploadReplayWithOptions(ctx, rec.MatchGUID, localPath, ballchasing.UploadOptions{
			Filename: uploadFilename,
			GroupID:  groupID,
		})
	} else {
		res, err = s.uploader.UploadReplay(ctx, rec.MatchGUID, localPath)
	}
	if err != nil {
		if isContextCanceled(ctx, err) {
			return nil, contextCancelErr(ctx, err)
		}
		_ = s.store.MarkUploadFailed(ctx, rec.MatchGUID, err.Error())
		stats.FailedCount++
		s.logger.Error("failed to upload replay", slog.String("guid", rec.MatchGUID), slog.String("error", err.Error()))
		return err, nil
	}

	// On 409 Conflict (duplicate), Ballchasing ignores the upload ?group= parameter on the existing replay;
	// issue a best-effort PATCH to assign the existing replay to the resolved group and canonical title.
	if res != nil && res.IsDuplicate && strings.TrimSpace(res.ID) != "" {
		s.patchUploadedReplayMetadata(ctx, rec.MatchGUID, res.ID, uploadFilename, groupID)
	}
	if groupInfo != nil {
		if orgStore, ok := s.store.(ReplayOrganizationStore); ok {
			_ = orgStore.UpdateMatchGrouping(ctx, rec.MatchGUID, groupInfo.SessionID, groupInfo.GameMode, groupInfo.GroupID)
		}
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
