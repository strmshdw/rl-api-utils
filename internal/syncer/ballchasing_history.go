package syncer

import (
	"context"
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

// BallchasingHistorySyncer coordinates the historical replay import from Ballchasing.com.
type BallchasingHistorySyncer struct {
	mu              sync.RWMutex
	client          *ballchasing.Client
	store           storage.StateStore
	localPlayerID   string
	localPlayerName string
	logger          *slog.Logger
	rateInterval    time.Duration // Default 500ms (2 req/sec) to strictly respect rate limits
	progress        BallchasingHistorySyncProgress
}

// NewBallchasingHistorySyncer creates a new BallchasingHistorySyncer.
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
	return &BallchasingHistorySyncer{
		client:          client,
		store:           store,
		localPlayerID:   strings.TrimSpace(localPlayerID),
		localPlayerName: strings.TrimSpace(localPlayerName),
		logger:          logger.With(slog.String("component", "ballchasing-history-sync")),
		rateInterval:    500 * time.Millisecond, // Strict 2 requests/sec rate limit compliance
	}
}

// SetRateInterval overrides the inter-request rate limit spacing.
func (b *BallchasingHistorySyncer) SetRateInterval(d time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rateInterval = d
}

// Progress returns a snapshot of the current or most recent sync operation.
func (b *BallchasingHistorySyncer) Progress() BallchasingHistorySyncProgress {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.progress
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
		slog.Duration("rate_interval", b.rateInterval),
	)

	// Auto-detect local player identity from Ballchasing API ping if not provided
	localPID := b.localPlayerID
	localPName := b.localPlayerName
	if localPID == "" || localPName == "" {
		if ping, err := b.client.PingInfo(ctx); err == nil && ping != nil {
			if localPName == "" && ping.Name != "" {
				localPName = ping.Name
			}
			if localPID == "" && ping.SteamID != "" {
				localPID = fmt.Sprintf("Steam|%s|0", ping.SteamID)
			}
		}
	}

	nextURL := ""
	totalReplays := 0
	syncedMatches := 0
	syncedPlayers := 0
	uniquePlayers := make(map[string]bool)

	for {
		select {
		case <-ctx.Done():
			b.mu.Lock()
			b.progress.LastError = ctx.Err().Error()
			b.mu.Unlock()
			return nil, ctx.Err()
		default:
		}

		res, err := b.client.GetUploadedReplays(ctx, nextURL)
		if err != nil {
			b.logger.Error("failed to fetch uploaded replays from Ballchasing", slog.Any("error", err))
			b.mu.Lock()
			b.progress.LastError = err.Error()
			b.mu.Unlock()
			return nil, fmt.Errorf("fetching uploaded replays: %w", err)
		}

		if res.Count > totalReplays {
			totalReplays = res.Count
		}

		for _, listItem := range res.List {
			select {
			case <-ctx.Done():
				b.mu.Lock()
				b.progress.LastError = ctx.Err().Error()
				b.mu.Unlock()
				return nil, ctx.Err()
			default:
			}

			replayID := strings.TrimSpace(listItem.ID)
			if replayID == "" {
				continue
			}

			// Strictly respect rate limits before each individual replay query
			b.mu.RLock()
			interval := b.rateInterval
			b.mu.RUnlock()
			if interval > 0 {
				select {
				case <-ctx.Done():
					b.mu.Lock()
					b.progress.LastError = ctx.Err().Error()
					b.mu.Unlock()
					return nil, ctx.Err()
				case <-time.After(interval):
				}
			}

			// Fetch full replay detail individually from Ballchasing to obtain MatchGUID and full rosters
			detail, err := b.client.GetReplay(ctx, replayID)
			if err != nil {
				b.logger.Warn("failed to fetch individual replay detail from Ballchasing, falling back to list item",
					slog.String("id", replayID),
					slog.Any("error", err),
				)
				detail = &listItem
			}

			guid := strings.TrimSpace(detail.MatchGUID)
			if guid == "" {
				guid = strings.TrimSpace(listItem.MatchGUID)
			}
			if guid == "" {
				guid = replayID
			}
			if guid == "" {
				continue
			}

			playlistID := ballchasing.MapPlaylistSlugToID(detail.PlaylistID)

			var winnerTeam *int
			if detail.Blue.Goals > detail.Orange.Goals {
				w := 0
				winnerTeam = &w
			} else if detail.Orange.Goals > detail.Blue.Goals {
				w := 1
				winnerTeam = &w
			}

			var playerStats []storage.ReplayPlayerStats
			var matchOutcomes []storage.PlayerOutcome
			var localTeam *int

			type replayPlayerEntry struct {
				team int
				p    ballchasing.ReplayPlayer
			}
			var allReplayPlayers []replayPlayerEntry

			for _, p := range detail.Blue.Players {
				allReplayPlayers = append(allReplayPlayers, replayPlayerEntry{team: 0, p: p})
			}
			for _, p := range detail.Orange.Players {
				allReplayPlayers = append(allReplayPlayers, replayPlayerEntry{team: 1, p: p})
			}

			for _, entry := range allReplayPlayers {
				plat := normalizePlatform(entry.p.ID.Platform)
				onlineID := entry.p.ID.ID
				canonicalID := onlineID
				if onlineID != "" && !strings.Contains(canonicalID, "|") {
					canonicalID = fmt.Sprintf("%s|%s|0", plat, onlineID)
				}

				// Check if local player
				if localTeam == nil {
					if localPID != "" && (canonicalID == localPID || onlineID == localPID) {
						t := entry.team
						localTeam = &t
					} else if localPName != "" && strings.EqualFold(entry.p.Name, localPName) {
						t := entry.team
						localTeam = &t
					}
				}

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
					Demos:    entry.p.Demos.Inflicted,
				})

				if canonicalID != "" && !uniquePlayers[canonicalID] {
					uniquePlayers[canonicalID] = true
					syncedPlayers++
				}
			}

			// Second pass: if local team identified, build head-to-head outcomes
			if localTeam != nil && winnerTeam != nil {
				for _, entry := range allReplayPlayers {
					plat := normalizePlatform(entry.p.ID.Platform)
					onlineID := entry.p.ID.ID
					canonicalID := onlineID
					if onlineID != "" && !strings.Contains(canonicalID, "|") {
						canonicalID = fmt.Sprintf("%s|%s|0", plat, onlineID)
					}
					if canonicalID == "" {
						continue
					}

					// Skip local player themselves in head-to-head records
					if (localPID != "" && canonicalID == localPID) || (localPName != "" && strings.EqualFold(entry.p.Name, localPName)) {
						continue
					}

					isTeammate := (entry.team == *localTeam)
					won := (entry.team == *winnerTeam)
					matchOutcomes = append(matchOutcomes, storage.PlayerOutcome{
						PlayerID:   canonicalID,
						Platform:   plat,
						PlayerName: entry.p.Name,
						IsTeammate: isTeammate,
						Won:        won,
					})
				}
			}

			// Sync into storage
			dateStr := detail.Date
			if dateStr == "" {
				dateStr = detail.Created
			}

			bcLink := detail.Link
			if bcLink == "" {
				bcLink = listItem.Link
			}
			if bcLink == "" {
				bcLink = fmt.Sprintf("https://ballchasing.com/replay/%s", replayID)
			}

			st := &storage.ReplayMatchStats{
				MatchGUID:      guid,
				MapName:        detail.MapName,
				Playlist:       playlistID,
				BlueScore:      detail.Blue.Goals,
				OrangeScore:    detail.Orange.Goals,
				WinnerTeam:     winnerTeam,
				Duration:       detail.Duration,
				Date:           dateStr,
				Players:        playerStats,
				BallchasingID:  replayID,
				BallchasingURL: bcLink,
			}

			if err := b.store.SyncReplayStats(ctx, st); err != nil {
				b.logger.Warn("failed to sync match record from Ballchasing",
					slog.String("guid", guid),
					slog.Any("error", err),
				)
				continue
			}

			// Record match outcomes for player matchups if local team was present
			if len(matchOutcomes) > 0 {
				_ = b.store.RecordMatchResults(ctx, guid, playlistID, matchOutcomes)
			}

			syncedMatches++
			b.mu.Lock()
			b.progress.TotalReplays = totalReplays
			b.progress.SyncedMatches = syncedMatches
			b.progress.SyncedPlayers = syncedPlayers
			b.mu.Unlock()
		}

		b.mu.Lock()
		b.progress.TotalReplays = totalReplays
		b.progress.SyncedMatches = syncedMatches
		b.progress.SyncedPlayers = syncedPlayers
		b.mu.Unlock()

		if res.Next == "" || len(res.List) == 0 {
			b.logger.Info("reached end of uploaded replays from Ballchasing",
				slog.Int("total_replays", totalReplays),
				slog.Int("synced_matches", syncedMatches),
				slog.Int("synced_players", syncedPlayers),
			)
			break
		}

		nextURL = res.Next

		// Strictly respect rate limits between page fetches
		b.mu.RLock()
		interval := b.rateInterval
		b.mu.RUnlock()
		if interval > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(interval):
			}
		}
	}

	snap := b.Progress()
	return &snap, nil
}

func normalizePlatform(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "epic":
		return "Epic"
	case "steam":
		return "Steam"
	case "ps4", "ps5", "playstation":
		return "PlayStation"
	case "xbox", "xboxone", "xboxseries":
		return "Xbox"
	case "switch", "nintendo":
		return "Switch"
	default:
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			return "Unknown"
		}
		return strings.ToUpper(trimmed[:1]) + strings.ToLower(trimmed[1:])
	}
}
