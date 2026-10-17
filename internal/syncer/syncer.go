package syncer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/replay"
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
	store           StateStore
	provider        MatchHistoryProvider
	downloader      ReplayDownloader
	uploader        ReplayUploader
	statsFetcher    ReplayStatsFetcher
	replayDir       string
	dryRun          bool
	keepLocalFiles  bool
	logger          *slog.Logger
	localPlayerID   string
	localPlayerName string
	maxRetries      int
	rateInterval    time.Duration
	mu              sync.Mutex
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
		maxRetries:     storage.DefaultMaxRetries,
		rateInterval:   500 * time.Millisecond,
	}

	for _, opt := range opts {
		opt(s)
	}

	// Auto-detect statsFetcher from uploader if not explicitly configured
	if s.statsFetcher == nil {
		if fetcher, ok := uploader.(ReplayStatsFetcher); ok {
			s.statsFetcher = fetcher
		}
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
// 6. Poll and sync detailed stats for uploaded / duplicate replays.
// 7. Return comprehensive cycle statistics.
func (s *Syncer) RunCycle(ctx context.Context) (*SyncStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

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

		if latest, err := s.store.GetMatch(ctx, rec.MatchGUID); err == nil && latest != nil {
			if latest.DownloadStatus == storage.DownloadDownloaded {
				continue
			}
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

		// Live replay download hook: sync replay stats with captured telemetry keyed by match GUID
		if syncErr := s.SyncDownloadedReplayStats(ctx, rec.MatchGUID, localPath); syncErr != nil {
			s.logger.Warn("failed to sync replay stats for downloaded replay",
				slog.String("guid", rec.MatchGUID),
				slog.String("local_path", localPath),
				slog.Any("error", syncErr),
			)
		}
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

		if latest, err := s.store.GetMatch(ctx, rec.MatchGUID); err == nil && latest != nil {
			if latest.UploadStatus == storage.UploadUploaded || latest.UploadStatus == storage.UploadDuplicate {
				continue
			}
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

	// 6. Poll and sync detailed stats for uploaded / duplicate replays
	if err := s.PollAndSyncPendingStats(ctx); err != nil {
		if ctx.Err() != nil {
			stats.PopulateAliases()
			return stats, ctx.Err()
		}
		s.logger.Warn("polling pending replay stats encountered error",
			slog.String("error", err.Error()),
		)
	}

	// 7. Return populated cycle statistics
	stats.PopulateAliases()
	return stats, nil
}

// PollAndSyncPendingStats queries the persistent store for replays awaiting telemetry synchronization
// (either newly uploaded or recognized as duplicates) and fetches full replay details from Ballchasing.
func (s *Syncer) PollAndSyncPendingStats(ctx context.Context) error {
	if s.statsFetcher == nil {
		return nil
	}

	pending, err := s.store.ListPendingStatsSync(ctx, s.maxRetries)
	if err != nil {
		return fmt.Errorf("listing pending stats sync: %w", err)
	}

	for _, rec := range pending {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		bcID := strings.TrimSpace(rec.BallchasingID)
		if bcID == "" {
			continue
		}

		// Pacing interval to respect rate limits
		if s.rateInterval > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(s.rateInterval):
			}
		}

		if err := s.store.MarkStatsSyncing(ctx, rec.MatchGUID); err != nil {
			s.logger.Warn("failed to mark match stats syncing",
				slog.String("guid", rec.MatchGUID),
				slog.Any("error", err),
			)
			continue
		}

		item, err := s.statsFetcher.GetReplay(ctx, bcID)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			s.logger.Error("failed to fetch replay stats from Ballchasing",
				slog.String("guid", rec.MatchGUID),
				slog.String("ballchasing_id", bcID),
				slog.Any("error", err),
			)
			_ = s.store.MarkStatsSyncFailed(ctx, rec.MatchGUID, err.Error())
			continue
		}

		if item == nil {
			_ = s.store.MarkStatsSyncFailed(ctx, rec.MatchGUID, "empty replay response")
			continue
		}

		status := strings.ToLower(strings.TrimSpace(item.Status))
		switch status {
		case "pending":
			s.logger.Info("replay stats still pending on Ballchasing, deferring to next cycle",
				slog.String("guid", rec.MatchGUID),
				slog.String("ballchasing_id", bcID),
			)
			_ = s.store.ResetMatchRetry(ctx, rec.MatchGUID)
			continue

		case "failed":
			s.logger.Warn("ballchasing processing failed for replay",
				slog.String("guid", rec.MatchGUID),
				slog.String("ballchasing_id", bcID),
			)
			_ = s.store.MarkStatsSyncFailed(ctx, rec.MatchGUID, "ballchasing processing failed")
			continue

		case "ok", "":
			if err := s.syncBallchasingReplayDetail(ctx, rec.MatchGUID, item); err != nil {
				s.logger.Error("failed to sync ballchasing replay detail",
					slog.String("guid", rec.MatchGUID),
					slog.String("ballchasing_id", bcID),
					slog.Any("error", err),
				)
				_ = s.store.MarkStatsSyncFailed(ctx, rec.MatchGUID, err.Error())
				continue
			}

			if err := s.store.MarkStatsSynced(ctx, rec.MatchGUID); err != nil {
				s.logger.Error("failed to mark stats synced",
					slog.String("guid", rec.MatchGUID),
					slog.Any("error", err),
				)
				continue
			}

			s.logger.Info("successfully synced post-upload replay stats",
				slog.String("guid", rec.MatchGUID),
				slog.String("ballchasing_id", bcID),
			)
		}
	}

	return nil
}

// syncBallchasingReplayDetail ingests parsed Ballchasing telemetry into the store, sets up match players,
// and records matchup outcomes using the actual match timestamp.
func (s *Syncer) syncBallchasingReplayDetail(ctx context.Context, matchGUID string, detail *ballchasing.ReplayItem) error {
	playlistID := ballchasing.MapPlaylistSlugToID(detail.PlaylistID)
	if playlistID == 0 {
		if rec, _ := s.store.GetMatch(ctx, matchGUID); rec != nil && rec.Playlist > 0 {
			playlistID = rec.Playlist
		}
	}

	var winnerTeam *int
	if detail.Blue.Goals > detail.Orange.Goals {
		w := 0
		winnerTeam = &w
	} else if detail.Orange.Goals > detail.Blue.Goals {
		w := 1
		winnerTeam = &w
	}

	dateStr := detail.Date
	if dateStr == "" {
		dateStr = detail.Created
	}
	matchTime, hasDate := storage.ParseReplayDate(dateStr)

	type replayPlayerEntry struct {
		team int
		p    ballchasing.ReplayPlayer
	}
	var allPlayers []replayPlayerEntry
	for _, p := range detail.Blue.Players {
		allPlayers = append(allPlayers, replayPlayerEntry{team: 0, p: p})
	}
	for _, p := range detail.Orange.Players {
		allPlayers = append(allPlayers, replayPlayerEntry{team: 1, p: p})
	}

	var (
		candidates   []ReplayPlayerCandidate
		playerStats  []storage.ReplayPlayerStats
		matchPlayers []*storage.MatchPlayerRecord
	)

	for _, entry := range allPlayers {
		plat := normalizePlatform(entry.p.ID.Platform)
		onlineID := entry.p.ID.ID
		canonicalID := onlineID
		if onlineID != "" && !strings.Contains(canonicalID, "|") {
			canonicalID = fmt.Sprintf("%s|%s|0", plat, onlineID)
		}

		demos := entry.p.Demos.Inflicted
		if demos == 0 && entry.p.Stats != nil && entry.p.Stats.Demo != nil {
			demos = entry.p.Stats.Demo.Inflicted
		}

		candidates = append(candidates, ReplayPlayerCandidate{
			Name:        entry.p.Name,
			Platform:    plat,
			OnlineID:    onlineID,
			CanonicalID: canonicalID,
			Team:        entry.team,
			IsBot:       false,
		})

		playerStats = append(playerStats, storage.ReplayPlayerStats{
			Name:     entry.p.Name,
			Platform: plat,
			OnlineID: onlineID,
			Team:     entry.team,
			Score:    entry.p.Score,
			Goals:    entry.p.Goals,
			Assists:  entry.p.Assists,
			Saves:    entry.p.Saves,
			Shots:    entry.p.Shots,
			Demos:    demos,
			IsBot:    false,
		})
	}

	identity := s.gatherLocalPlayerIdentity(ctx, "", nil, detail.Uploader)
	localIdx, localTeam, found := ResolveLocalPlayer(candidates, identity)

	for i, entry := range allPlayers {
		c := candidates[i]
		playerID := c.CanonicalID
		if playerID == "" {
			playerID = c.Name
		}

		isLocal := (found && i == localIdx) || (localTeam != nil && c.Team == *localTeam && strings.EqualFold(c.Name, identity.ConfigLocalPlayerName))

		var won *bool
		if winnerTeam != nil {
			w := (c.Team == *winnerTeam)
			won = &w
		}

		matchPlayers = append(matchPlayers, &storage.MatchPlayerRecord{
			MatchGUID: matchGUID,
			PlayerID:  playerID,
			TeamNum:   c.Team,
			IsLocal:   isLocal,
			Won:       won,
			Score:     entry.p.Score,
			Goals:     entry.p.Goals,
			Assists:   entry.p.Assists,
			Saves:     entry.p.Saves,
			Shots:     entry.p.Shots,
			Demos:     playerStats[i].Demos,
		})
	}

	bcLink := detail.Link
	if bcLink == "" {
		bcLink = fmt.Sprintf("https://ballchasing.com/replay/%s", detail.ID)
	}

	st := &storage.ReplayMatchStats{
		MatchGUID:      matchGUID,
		MapName:        detail.MapName,
		Playlist:       playlistID,
		BlueScore:      detail.Blue.Goals,
		OrangeScore:    detail.Orange.Goals,
		WinnerTeam:     winnerTeam,
		Duration:       detail.Duration,
		Overtime:       detail.Overtime,
		Date:           dateStr,
		Players:        playerStats,
		BallchasingID:  detail.ID,
		BallchasingURL: bcLink,
	}

	if err := s.store.SyncReplayStats(ctx, st); err != nil {
		return fmt.Errorf("syncing replay stats: %w", err)
	}

	if len(matchPlayers) > 0 {
		_ = s.store.SaveMatchPlayers(ctx, matchGUID, matchPlayers)
	}

	if localTeam != nil && winnerTeam != nil {
		var outcomes []storage.PlayerOutcome
		for i, c := range candidates {
			if c.IsBot {
				continue
			}
			if found && i == localIdx {
				continue // Skip self
			}
			if c.CanonicalID == "" {
				continue
			}

			isTeammate := (c.Team == *localTeam)
			won := (c.Team == *winnerTeam)

			outcomes = append(outcomes, storage.PlayerOutcome{
				PlayerID:   c.CanonicalID,
				Platform:   c.Platform,
				PlayerName: c.Name,
				IsTeammate: isTeammate,
				Won:        won,
			})
		}

		if len(outcomes) > 0 {
			var matchTimes []time.Time
			if hasDate {
				matchTimes = []time.Time{matchTime}
			}
			if err := s.store.RecordMatchResults(ctx, matchGUID, playlistID, outcomes, matchTimes...); err != nil && !errors.Is(err, storage.ErrMatchAlreadyProcessed) {
				s.logger.Warn("failed to record match results from ballchasing detail",
					slog.String("guid", matchGUID),
					slog.Any("error", err),
				)
			}
		}
	}

	return nil
}

// SyncDownloadedReplayStats parses the downloaded replay file and merges the replay statistics
// with telemetry in the local store using matchGUID as key, sets up match players, and records
// matchup results using the actual replay timestamp.
func (s *Syncer) SyncDownloadedReplayStats(ctx context.Context, matchGUID, localPath string) error {
	meta, err := replay.ParseReplayFile(localPath)
	if err != nil {
		return fmt.Errorf("parsing replay file: %w", err)
	}

	matchTime, hasDate := storage.ParseReplayDate(meta.Date)

	existing, _ := s.store.GetMatch(ctx, matchGUID)
	playlist := 0
	if existing != nil {
		playlist = existing.Playlist
	}

	var (
		candidates   []ReplayPlayerCandidate
		replayStats  []storage.ReplayPlayerStats
		matchPlayers []*storage.MatchPlayerRecord
	)

	for _, p := range meta.Players {
		plat := normalizePlatform(p.Platform)
		canonicalID := p.OnlineID
		if canonicalID != "" && !strings.Contains(canonicalID, "|") {
			canonicalID = fmt.Sprintf("%s|%s|0", plat, canonicalID)
		}

		candidates = append(candidates, ReplayPlayerCandidate{
			Name:        p.Name,
			Platform:    plat,
			OnlineID:    p.OnlineID,
			CanonicalID: canonicalID,
			Team:        p.Team,
			IsBot:       p.IsBot,
		})

		replayStats = append(replayStats, storage.ReplayPlayerStats{
			Name:     p.Name,
			Platform: plat,
			OnlineID: p.OnlineID,
			Team:     p.Team,
			Score:    p.Score,
			Goals:    p.Goals,
			Assists:  p.Assists,
			Saves:    p.Saves,
			Shots:    p.Shots,
			Demos:    p.Demos,
			IsBot:    p.IsBot,
		})
	}

	identity := s.gatherLocalPlayerIdentity(ctx, meta.PlayerName, meta.LocalTeam, nil)
	localIdx, localTeam, found := ResolveLocalPlayer(candidates, identity)

	for i, p := range meta.Players {
		plat := normalizePlatform(p.Platform)
		canonicalID := p.OnlineID
		if canonicalID != "" && !strings.Contains(canonicalID, "|") {
			canonicalID = fmt.Sprintf("%s|%s|0", plat, canonicalID)
		}
		if canonicalID == "" {
			canonicalID = p.Name
		}

		isLocal := (found && i == localIdx) || (localTeam != nil && p.Team == *localTeam && strings.EqualFold(p.Name, meta.PlayerName))

		var won *bool
		if meta.WinnerTeam != nil {
			w := (p.Team == *meta.WinnerTeam)
			won = &w
		}

		matchPlayers = append(matchPlayers, &storage.MatchPlayerRecord{
			MatchGUID: matchGUID,
			PlayerID:  canonicalID,
			TeamNum:   p.Team,
			IsLocal:   isLocal,
			Won:       won,
			Score:     p.Score,
			Goals:     p.Goals,
			Assists:   p.Assists,
			Saves:     p.Saves,
			Shots:     p.Shots,
			Demos:     p.Demos,
		})
	}

	st := &storage.ReplayMatchStats{
		MatchGUID:   matchGUID,
		MapName:     meta.MapName,
		Playlist:    playlist,
		BlueScore:   meta.Team0Score,
		OrangeScore: meta.Team1Score,
		WinnerTeam:  meta.WinnerTeam,
		Duration:    int(meta.TotalSeconds),
		Overtime:    meta.Overtime,
		Date:        meta.Date,
		Players:     replayStats,
	}

	if err := s.store.SyncReplayStats(ctx, st); err != nil {
		return fmt.Errorf("storing synced replay stats: %w", err)
	}

	if len(matchPlayers) > 0 {
		_ = s.store.SaveMatchPlayers(ctx, matchGUID, matchPlayers)
	}

	if localTeam != nil && meta.WinnerTeam != nil {
		var outcomes []storage.PlayerOutcome
		for i, c := range candidates {
			if c.IsBot {
				continue
			}
			if found && i == localIdx {
				continue // Skip self
			}
			if c.CanonicalID == "" {
				continue
			}

			isTeammate := (c.Team == *localTeam)
			won := (c.Team == *meta.WinnerTeam)

			outcomes = append(outcomes, storage.PlayerOutcome{
				PlayerID:   c.CanonicalID,
				Platform:   c.Platform,
				PlayerName: c.Name,
				IsTeammate: isTeammate,
				Won:        won,
			})
		}

		if len(outcomes) > 0 {
			var matchTimes []time.Time
			if hasDate {
				matchTimes = []time.Time{matchTime}
			}
			if err := s.store.RecordMatchResults(ctx, matchGUID, playlist, outcomes, matchTimes...); err != nil && !errors.Is(err, storage.ErrMatchAlreadyProcessed) {
				s.logger.Warn("failed to record match results for downloaded replay",
					slog.String("guid", matchGUID),
					slog.Any("error", err),
				)
			}
		}
	}

	s.logger.Info("synced replay stats into local store",
		slog.String("guid", matchGUID),
		slog.Int("players", len(replayStats)),
		slog.Int("blue_score", meta.Team0Score),
		slog.Int("orange_score", meta.Team1Score),
	)

	return nil
}

// gatherLocalPlayerIdentity aggregates configured identity, active auth states, and replay metadata.
func (s *Syncer) gatherLocalPlayerIdentity(ctx context.Context, replayPlayerName string, replayLocalTeam *int, uploader *ballchasing.UploaderInfo) LocalPlayerIdentity {
	id := LocalPlayerIdentity{
		ConfigLocalPlayerID:   s.localPlayerID,
		ConfigLocalPlayerName: s.localPlayerName,
		ReplayPlayerName:      replayPlayerName,
		ReplayLocalTeam:       replayLocalTeam,
	}

	if uploader != nil {
		id.UploaderSteamID = strings.TrimSpace(uploader.SteamID)
		id.UploaderName = strings.TrimSpace(uploader.Name)
	}

	if s.store != nil {
		if _, accID, disp, err := s.store.GetAuthState(ctx, "epic"); err == nil {
			if accID != "" && id.AuthAccountID == "" {
				id.AuthAccountID = accID
				id.AuthPlatform = "epic"
			}
			if disp != "" && id.AuthDisplayName == "" {
				id.AuthDisplayName = disp
			}
		}
		if _, accID, disp, err := s.store.GetAuthState(ctx, "steam"); err == nil {
			if accID != "" && id.AuthAccountID == "" {
				id.AuthAccountID = accID
				id.AuthPlatform = "steam"
			}
			if disp != "" && id.AuthDisplayName == "" {
				id.AuthDisplayName = disp
			}
		}
	}

	return id
}

// ReplayPlayerCandidate represents a player candidate in a match roster for identity resolution.
type ReplayPlayerCandidate struct {
	Name        string
	Platform    string
	OnlineID    string
	CanonicalID string
	Team        int
	IsBot       bool
}

// LocalPlayerIdentity aggregates all known identities of the local user.
type LocalPlayerIdentity struct {
	ConfigLocalPlayerID   string
	ConfigLocalPlayerName string
	AuthAccountID         string
	AuthDisplayName       string
	AuthPlatform          string
	PingSteamID           string
	PingName              string
	UploaderSteamID       string
	UploaderName          string
	ReplayPlayerName      string
	ReplayLocalTeam       *int
}

// ResolveLocalPlayer resolves the local player index and team from a player roster.
// Returns (localPlayerIndex, localTeamNum, found).
// If an individual player cannot be resolved, but team is known from header, returns (-1, &team, true).
// Strict bot immunity: any candidate with IsBot == true is NEVER matched as the local player.
func ResolveLocalPlayer(players []ReplayPlayerCandidate, id LocalPlayerIdentity) (int, *int, bool) {
	if len(players) == 0 {
		return -1, nil, false
	}

	// Tier 1: Configured LocalPlayerID
	if cleanID := strings.TrimSpace(id.ConfigLocalPlayerID); cleanID != "" {
		for i, p := range players {
			if p.IsBot {
				continue
			}
			if matchPlayerID(p, cleanID) {
				team := p.Team
				return i, &team, true
			}
		}
	}

	// Tier 2: Authenticated Primary Account ID
	if authID := strings.TrimSpace(id.AuthAccountID); authID != "" {
		for i, p := range players {
			if p.IsBot {
				continue
			}
			if strings.EqualFold(p.OnlineID, authID) || matchPlayerID(p, authID) {
				team := p.Team
				return i, &team, true
			}
		}
	}

	// Tier 3: Ballchasing Ping SteamID & Uploader SteamID
	for _, steamID := range []string{id.PingSteamID, id.UploaderSteamID} {
		clean := strings.TrimSpace(steamID)
		if clean == "" {
			continue
		}
		for i, p := range players {
			if p.IsBot {
				continue
			}
			if strings.EqualFold(p.Platform, "Steam") && (p.OnlineID == clean || matchPlayerID(p, clean)) {
				team := p.Team
				return i, &team, true
			}
		}
	}

	// Tier 4: Configured LocalPlayerName
	if cfgName := strings.TrimSpace(id.ConfigLocalPlayerName); cfgName != "" {
		for i, p := range players {
			if p.IsBot {
				continue
			}
			if strings.EqualFold(p.Name, cfgName) {
				team := p.Team
				return i, &team, true
			}
		}
	}

	// Tier 5: Auth Display Name, Ping Name, Uploader Name
	for _, name := range []string{id.AuthDisplayName, id.PingName, id.UploaderName} {
		clean := strings.TrimSpace(name)
		if clean == "" {
			continue
		}
		for i, p := range players {
			if p.IsBot {
				continue
			}
			if strings.EqualFold(p.Name, clean) {
				team := p.Team
				return i, &team, true
			}
		}
	}

	// Tier 6: Replay Header Tags (Downloaded .replay files)
	if id.ReplayPlayerName != "" {
		clean := strings.TrimSpace(id.ReplayPlayerName)
		for i, p := range players {
			if p.IsBot {
				continue
			}
			if strings.EqualFold(p.Name, clean) {
				team := p.Team
				return i, &team, true
			}
		}
	}
	if id.ReplayLocalTeam != nil {
		t := *id.ReplayLocalTeam
		return -1, &t, true
	}

	return -1, nil, false
}

// matchPlayerID compares player IDs across canonical formats ("Platform|OnlineID|0")
// and raw online IDs.
func matchPlayerID(p ReplayPlayerCandidate, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	if strings.EqualFold(p.CanonicalID, target) || strings.EqualFold(p.OnlineID, target) {
		return true
	}
	// Handle prefix without splitscreen suffix (e.g. "Steam|12345" matching "Steam|12345|0")
	if strings.HasPrefix(strings.ToLower(p.CanonicalID), strings.ToLower(target)+"|") {
		return true
	}
	// If target has platform prefix, check if online ID matches
	parts := strings.Split(target, "|")
	if len(parts) >= 2 {
		targetOnlineID := parts[1]
		if strings.EqualFold(p.OnlineID, targetOnlineID) {
			return true
		}
	}
	return false
}
