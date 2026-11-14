package syncer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/storage"
)

// BallchasingHistorySyncProgress tracks the status and metrics of a historical sync operation.
type BallchasingHistorySyncProgress struct {
	Running       bool      `json:"running"`
	TotalReplays  int       `json:"total_replays"`
	SyncedMatches int       `json:"synced_matches"`
	SyncedPlayers int       `json:"synced_players"`
	StartedAt     time.Time `json:"started_at"`
	EndedAt       time.Time `json:"ended_at,omitempty"`
	LastError     string    `json:"last_error,omitempty"`
}

// BallchasingHistorySyncer coordinates historical replay imports from Ballchasing.com.
type BallchasingHistorySyncer struct {
	mu              sync.RWMutex
	client          *ballchasing.Client
	store           storage.StateStore
	localPlayerID   string
	localPlayerName string
	logger          *slog.Logger
	rateInterval    time.Duration
	rateLimiter     *ballchasing.RateLimiter
	progress        BallchasingHistorySyncProgress
}

// NewBallchasingHistorySyncer creates a new BallchasingHistorySyncer sharing client's rate limiter when available.
func NewBallchasingHistorySyncer(
	client *ballchasing.Client,
	store storage.StateStore,
	localPlayerID string,
	localPlayerName string,
	logger *slog.Logger,
) *BallchasingHistorySyncer {
	if logger == nil {
		logger = slog.Default()
	}
	var rl *ballchasing.RateLimiter
	if client != nil {
		rl = client.RateLimiter()
	}
	return &BallchasingHistorySyncer{
		client:          client,
		store:           store,
		localPlayerID:   strings.TrimSpace(localPlayerID),
		localPlayerName: strings.TrimSpace(localPlayerName),
		logger:          logger.With(slog.String("component", "ballchasing-history-sync")),
		rateInterval:    ballchasing.DefaultRateInterval,
		rateLimiter:     rl,
	}
}

// SetRateInterval overrides the inter-request rate limit spacing.
func (b *BallchasingHistorySyncer) SetRateInterval(d time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rateInterval = d
}

// SetRateLimiter configures an optional shared Ballchasing rate limiter across sync components.
func (b *BallchasingHistorySyncer) SetRateLimiter(rl *ballchasing.RateLimiter) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rateLimiter = rl
}

// RateLimiter returns the configured rate limiter.
func (b *BallchasingHistorySyncer) RateLimiter() *ballchasing.RateLimiter {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.rateLimiter
}

// Progress returns a snapshot of the current or most recent sync operation.
func (b *BallchasingHistorySyncer) Progress() BallchasingHistorySyncProgress {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.progress
}

func (b *BallchasingHistorySyncer) setProgressMetrics(totalReplays, syncedMatches, syncedPlayers int) {
	b.mu.Lock()
	b.progress.TotalReplays = totalReplays
	b.progress.SyncedMatches = syncedMatches
	b.progress.SyncedPlayers = syncedPlayers
	b.mu.Unlock()
}

func (b *BallchasingHistorySyncer) recordError(err error) {
	if err == nil {
		return
	}
	b.mu.Lock()
	b.progress.LastError = err.Error()
	b.mu.Unlock()
}

func (b *BallchasingHistorySyncer) waitRateLimit(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.RLock()
	rl := b.rateLimiter
	interval := b.rateInterval
	var clientRL *ballchasing.RateLimiter
	if b.client != nil {
		clientRL = b.client.RateLimiter()
	}
	b.mu.RUnlock()

	if rl != nil {
		if clientRL != nil && rl == clientRL {
			// b.client already enforces this exact RateLimiter inside GetUploadedReplays and GetReplay.
			return nil
		}
		return rl.Wait(ctx)
	}
	if interval > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
	return nil
}

func (b *BallchasingHistorySyncer) resolveBaseIdentity(ctx context.Context) LocalPlayerIdentity {
	id := LocalPlayerIdentity{
		ConfigLocalPlayerID:   b.localPlayerID,
		ConfigLocalPlayerName: b.localPlayerName,
	}
	if b.client != nil {
		if ping, err := b.client.PingInfo(ctx); err == nil && ping != nil {
			id.PingSteamID = strings.TrimSpace(ping.SteamID)
			id.PingName = strings.TrimSpace(ping.Name)
			if id.ConfigLocalPlayerName == "" && id.PingName != "" {
				id.ConfigLocalPlayerName = id.PingName
			}
			if id.ConfigLocalPlayerID == "" && id.PingSteamID != "" {
				id.ConfigLocalPlayerID = fmt.Sprintf("Steam|%s|0", id.PingSteamID)
			}
		}
	}
	populateAuthIdentity(ctx, b.store, &id)
	return id
}

func (b *BallchasingHistorySyncer) lookupExistingMatch(ctx context.Context, replayID string, listItem ballchasing.ReplayItem) (*storage.MatchRecord, bool) {
	if b.store == nil {
		return nil, false
	}
	var existing *storage.MatchRecord
	if candidateGUID := strings.TrimSpace(listItem.MatchGUID); candidateGUID != "" {
		if ex, err := b.store.GetMatch(ctx, candidateGUID); err == nil && ex != nil {
			existing = ex
		}
	}
	if existing == nil {
		if ex, err := b.store.GetMatch(ctx, replayID); err == nil && ex != nil && ex.BallchasingID == replayID {
			existing = ex
		}
	}
	if existing == nil || existing.BallchasingID == "" {
		return existing, false
	}

	hasCompleteTelemetry := strings.Contains(existing.ReplayStatsJSON, `"boost":`) ||
		(existing.StatsSyncStatus == storage.StatsSyncSynced &&
			existing.SyncedAt != nil &&
			len(listItem.Blue.Players) == 0 && len(listItem.Orange.Players) == 0)

	complete := (existing.BlueScore > 0 || existing.OrangeScore > 0 || existing.StatsSyncStatus == storage.StatsSyncSynced) && hasCompleteTelemetry
	return existing, complete
}

// Sync executes a full historical synchronization cycle across all uploaded replays.
func (b *BallchasingHistorySyncer) Sync(ctx context.Context) (*BallchasingHistorySyncProgress, error) {
	b.mu.Lock()
	if b.progress.Running {
		b.mu.Unlock()
		return nil, fmt.Errorf("ballchasing historical sync is already running")
	}
	b.progress = BallchasingHistorySyncProgress{
		Running:   true,
		StartedAt: time.Now().UTC(),
	}
	interval := b.rateInterval
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		b.progress.Running = false
		b.progress.EndedAt = time.Now().UTC()
		b.mu.Unlock()
	}()

	b.logger.Info("starting historical Ballchasing sync",
		slog.String("local_player_id", b.localPlayerID),
		slog.String("local_player_name", b.localPlayerName),
		slog.Duration("rate_interval", interval),
	)

	baseIdentity := b.resolveBaseIdentity(ctx)

	nextURL := ""
	totalReplays := 0
	syncedMatches := 0
	syncedPlayers := 0
	uniquePlayers := make(map[string]bool)

	for {
		if err := ctx.Err(); err != nil {
			b.recordError(err)
			return nil, err
		}

		if nextURL != "" {
			if err := b.waitRateLimit(ctx); err != nil {
				b.recordError(err)
				return nil, err
			}
		}

		res, err := b.client.GetUploadedReplays(ctx, nextURL)
		if err != nil {
			if isContextCanceled(ctx, err) {
				cErr := contextCancelErr(ctx, err)
				b.recordError(cErr)
				return nil, cErr
			}
			b.logger.Error("failed to fetch uploaded replays from Ballchasing", slog.Any("error", err))
			b.recordError(err)
			return nil, fmt.Errorf("fetching uploaded replays: %w", err)
		}

		if res.Count > totalReplays {
			totalReplays = res.Count
		}

		for _, listItem := range res.List {
			if err := ctx.Err(); err != nil {
				b.recordError(err)
				return nil, err
			}

			replayID := strings.TrimSpace(listItem.ID)
			if replayID == "" {
				continue
			}

			existingMatch, alreadyComplete := b.lookupExistingMatch(ctx, replayID, listItem)
			if alreadyComplete {
				syncedMatches++
				b.setProgressMetrics(totalReplays, syncedMatches, syncedPlayers)
				continue
			}

			if err := b.waitRateLimit(ctx); err != nil {
				b.recordError(err)
				return nil, err
			}

			detail, err := b.client.GetReplay(ctx, replayID)
			if err != nil {
				if isContextCanceled(ctx, err) {
					cErr := contextCancelErr(ctx, err)
					b.recordError(cErr)
					return nil, cErr
				}
				if errors.Is(err, ballchasing.ErrNotFound) {
					b.logger.Warn("ballchasing replay expired or not found (404), skipping",
						slog.String("id", replayID),
					)
					continue
				}
				b.logger.Warn("failed to fetch individual replay detail from Ballchasing, falling back to list item",
					slog.String("id", replayID),
					slog.Any("error", err),
				)
				detail = &listItem
			}

			guid := resolveHistoryMatchGUID(existingMatch, detail, listItem, replayID)
			if guid == "" {
				continue
			}

			playlistID := ballchasing.MapPlaylistSlugToID(detail.PlaylistID)
			if playlistID == 0 {
				playlistID = ballchasing.MapPlaylistSlugToID(listItem.PlaylistID)
			}

			identity := baseIdentity
			if detail.Uploader != nil {
				identity.UploaderSteamID = strings.TrimSpace(detail.Uploader.SteamID)
				identity.UploaderName = strings.TrimSpace(detail.Uploader.Name)
			} else if listItem.Uploader != nil {
				identity.UploaderSteamID = strings.TrimSpace(listItem.Uploader.SteamID)
				identity.UploaderName = strings.TrimSpace(listItem.Uploader.Name)
			}

			tel := buildBallchasingTelemetry(guid, replayID, playlistID, detail, &listItem, identity)
			for _, c := range tel.candidates {
				if c.CanonicalID != "" && !uniquePlayers[c.CanonicalID] {
					uniquePlayers[c.CanonicalID] = true
					syncedPlayers++
				}
			}

			if err := b.store.SyncReplayStats(ctx, tel.stats); err != nil {
				if isContextCanceled(ctx, err) {
					cErr := contextCancelErr(ctx, err)
					b.recordError(cErr)
					return nil, cErr
				}
				b.logger.Warn("failed to sync match record from Ballchasing",
					slog.String("guid", guid),
					slog.Any("error", err),
				)
				continue
			}

			if tel.stats.MatchGUID != "" && tel.stats.MatchGUID != guid {
				guid = tel.stats.MatchGUID
				for _, mp := range tel.matchPlayers {
					mp.MatchGUID = guid
				}
			}

			if len(tel.matchPlayers) > 0 {
				_ = b.store.SaveMatchPlayers(ctx, guid, tel.matchPlayers)
			}
			recordOutcomesIfNonEmpty(ctx, b.store, b.logger, "ballchasing history", guid, playlistID, tel.outcomes, tel.hasDate, tel.matchTime)

			syncedMatches++
			b.setProgressMetrics(totalReplays, syncedMatches, syncedPlayers)
		}

		b.setProgressMetrics(totalReplays, syncedMatches, syncedPlayers)

		if res.Next == "" || len(res.List) == 0 {
			b.logger.Info("reached end of uploaded replays from Ballchasing",
				slog.Int("total_replays", totalReplays),
				slog.Int("synced_matches", syncedMatches),
				slog.Int("synced_players", syncedPlayers),
			)
			break
		}

		nextURL = res.Next
	}

	snap := b.Progress()
	return &snap, nil
}

func resolveHistoryMatchGUID(existing *storage.MatchRecord, detail *ballchasing.ReplayItem, listItem ballchasing.ReplayItem, replayID string) string {
	candidates := []string{
		"",
		detail.MatchGUID,
		listItem.MatchGUID,
		replayID,
	}
	if existing != nil {
		candidates[0] = existing.MatchGUID
	}
	for _, g := range candidates {
		if trimmed := strings.TrimSpace(g); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
