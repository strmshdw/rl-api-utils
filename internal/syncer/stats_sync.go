package syncer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/storage"
)

const (
	maxStatsPerCycle        = 20
	expiredStatsErrorPrefix = "expired: "
)

func isExpiredStatsError(lastError string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(lastError)), expiredStatsErrorPrefix)
}

// PollAndSyncPendingStats queries the persistent store for replays awaiting telemetry synchronization
// (either newly uploaded or recognized as duplicates) and fetches full replay details from Ballchasing.
// Serialized with RunCycle via cycleMu.
func (s *Syncer) PollAndSyncPendingStats(ctx context.Context) error {
	s.cycleMu.Lock()
	defer s.cycleMu.Unlock()

	return s.pollAndSyncPendingStatsLocked(ctx)
}

func (s *Syncer) pollAndSyncPendingStatsLocked(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.statsFetcher == nil {
		return nil
	}

	pending, err := s.store.ListPendingStatsSync(ctx, s.maxRetries)
	if err != nil {
		if isContextCanceled(ctx, err) {
			return contextCancelErr(ctx, err)
		}
		return fmt.Errorf("listing pending stats sync: %w", err)
	}

	processed := 0
	for i := len(pending) - 1; i >= 0; i-- {
		if processed >= maxStatsPerCycle {
			break
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		rec := pending[i]
		if strings.TrimSpace(rec.BallchasingID) == "" || isExpiredStatsError(rec.LastError) {
			continue
		}

		processed++
		if err := s.syncSinglePendingStats(ctx, rec); err != nil {
			if isContextCanceled(ctx, err) {
				return contextCancelErr(ctx, err)
			}
			return err
		}
	}

	return nil
}

func (s *Syncer) syncSinglePendingStats(ctx context.Context, rec *storage.MatchRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.statsFetcher == nil || rec == nil {
		return nil
	}

	bcID := strings.TrimSpace(rec.BallchasingID)
	if bcID == "" {
		return nil
	}

	if err := s.waitStatsRateLimit(ctx); err != nil {
		return err
	}

	if err := s.store.MarkStatsSyncing(ctx, rec.MatchGUID); err != nil {
		if isContextCanceled(ctx, err) {
			return contextCancelErr(ctx, err)
		}
		s.logger.Warn("failed to mark match stats syncing",
			slog.String("guid", rec.MatchGUID),
			slog.Any("error", err),
		)
		return nil
	}

	item, err := s.statsFetcher.GetReplay(ctx, bcID)
	if err != nil {
		if isContextCanceled(ctx, err) {
			return contextCancelErr(ctx, err)
		}
		if errors.Is(err, ballchasing.ErrNotFound) {
			s.logger.Warn("ballchasing replay expired or not found (HTTP 404); skipping further automatic retries",
				slog.String("guid", rec.MatchGUID),
				slog.String("ballchasing_id", bcID),
			)
			_ = s.store.MarkStatsSyncFailed(ctx, rec.MatchGUID, expiredStatsErrorPrefix+"ballchasing replay not found (HTTP 404)")
			return nil
		}
		s.logger.Error("failed to fetch replay stats from Ballchasing",
			slog.String("guid", rec.MatchGUID),
			slog.String("ballchasing_id", bcID),
			slog.Any("error", err),
		)
		_ = s.store.MarkStatsSyncFailed(ctx, rec.MatchGUID, err.Error())
		return nil
	}

	if item == nil {
		_ = s.store.MarkStatsSyncFailed(ctx, rec.MatchGUID, "empty replay response")
		return nil
	}

	status := strings.ToLower(strings.TrimSpace(item.Status))
	switch status {
	case "pending":
		s.logger.Info("replay stats still pending on Ballchasing, deferring to next cycle",
			slog.String("guid", rec.MatchGUID),
			slog.String("ballchasing_id", bcID),
		)
		if err := s.store.ResetMatchRetry(ctx, rec.MatchGUID); err != nil && isContextCanceled(ctx, err) {
			return contextCancelErr(ctx, err)
		}
		return nil

	case "failed":
		s.logger.Warn("ballchasing processing failed for replay",
			slog.String("guid", rec.MatchGUID),
			slog.String("ballchasing_id", bcID),
		)
		_ = s.store.MarkStatsSyncFailed(ctx, rec.MatchGUID, "ballchasing processing failed")
		return nil

	case "ok", "":
		if err := s.syncBallchasingReplayDetail(ctx, rec.MatchGUID, item); err != nil {
			if isContextCanceled(ctx, err) {
				return contextCancelErr(ctx, err)
			}
			s.logger.Error("failed to sync ballchasing replay detail",
				slog.String("guid", rec.MatchGUID),
				slog.String("ballchasing_id", bcID),
				slog.Any("error", err),
			)
			_ = s.store.MarkStatsSyncFailed(ctx, rec.MatchGUID, err.Error())
			return nil
		}

		if err := s.store.MarkStatsSynced(ctx, rec.MatchGUID); err != nil {
			if isContextCanceled(ctx, err) {
				return contextCancelErr(ctx, err)
			}
			s.logger.Error("failed to mark stats synced",
				slog.String("guid", rec.MatchGUID),
				slog.Any("error", err),
			)
			return nil
		}

		s.logger.Info("successfully synced post-upload replay stats",
			slog.String("guid", rec.MatchGUID),
			slog.String("ballchasing_id", bcID),
		)
	}

	return nil
}

func (s *Syncer) waitStatsRateLimit(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.fetcherManagesRate {
		return nil
	}
	if s.rateLimiter != nil {
		return s.rateLimiter.Wait(ctx)
	}
	if s.rateInterval > 0 {
		s.rateLimiter = ballchasing.NewRateLimiter(s.rateInterval)
		return s.rateLimiter.Wait(ctx)
	}
	return nil
}

func (s *Syncer) syncBallchasingReplayDetail(ctx context.Context, matchGUID string, detail *ballchasing.ReplayItem) error {
	playlistID := ballchasing.MapPlaylistSlugToID(detail.PlaylistID)
	if playlistID == 0 {
		if rec, _ := s.store.GetMatch(ctx, matchGUID); rec != nil && rec.Playlist > 0 {
			playlistID = rec.Playlist
		}
	}

	identity := s.gatherLocalPlayerIdentity(ctx, "", nil, detail.Uploader)
	res := buildBallchasingTelemetry(matchGUID, detail.ID, playlistID, detail, nil, identity)

	if err := s.store.SyncReplayStats(ctx, res.stats); err != nil {
		return fmt.Errorf("syncing replay stats: %w", err)
	}

	// Zero-player Ballchasing responses must never overwrite existing stored match players.
	if len(res.matchPlayers) > 0 {
		_ = s.store.SaveMatchPlayers(ctx, matchGUID, res.matchPlayers)
	}

	recordOutcomesIfNonEmpty(ctx, s.store, s.logger, "ballchasing detail", matchGUID, res.playlistID, res.outcomes, res.hasDate, res.matchTime)
	return nil
}
