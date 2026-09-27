package playertrack

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/dank/rl-api-utils/internal/auth"
	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rlapi"
)

// Ensure Tracker satisfies statsapi.PlayerEventHandler at compile time.
var _ statsapi.PlayerEventHandler = (*Tracker)(nil)

const (
	defaultRankCacheTTL = 15 * time.Minute
	defaultBackoffTTL   = 60 * time.Second
)

// PlayerClassification designates a player's relationship to the local user.
type PlayerClassification string

const (
	ClassificationSelf      PlayerClassification = "self"
	ClassificationTeammate  PlayerClassification = "teammate"
	ClassificationOpponent  PlayerClassification = "opponent"
	ClassificationBot       PlayerClassification = "bot"
	ClassificationSpectator PlayerClassification = "spectator"
)

// PlayerStatsSummary holds real-time box score counters received from StatsPlayer.
type PlayerStatsSummary struct {
	Score   int `json:"score"`
	Goals   int `json:"goals"`
	Assists int `json:"assists"`
	Saves   int `json:"saves"`
	Shots   int `json:"shots"`
	Demos   int `json:"demos"`
}

// LobbyPlayer encapsulates identity, real-time match stats, skill ratings, and historical matchup records.
type LobbyPlayer struct {
	PlayerID      string                 `json:"player_id"`                // PrimaryId (e.g. "Steam|76561198...|0")
	Platform      string                 `json:"platform"`                 // "Steam", "Epic", "Unknown"
	Name          string                 `json:"name"`                     // In-game display name
	TeamNum       int                    `json:"team_num"`                 // 0 = Blue, 1 = Orange, 255 = Spectator
	IsLocal       bool                   `json:"is_local"`                 // True if this is the authenticated user
	IsBot         bool                   `json:"is_bot"`                   // True if AI bot ("Unknown|0|0")
	Stats         PlayerStatsSummary     `json:"stats"`                    // Real-time in-game box score
	CurrentRank   *PlayerPlaylistRank    `json:"current_rank,omitempty"`   // Rank for active match playlist
	Ranks         PlayerRanksSnapshot    `json:"ranks,omitempty"`          // Full snapshot across all playlists
	MatchupRecord *storage.PlayerMatchup `json:"matchup_record,omitempty"` // H2H record in this playlist
}

// DeepClone creates a deep copy of a LobbyPlayer to ensure thread safety across reads.
func (p LobbyPlayer) DeepClone() LobbyPlayer {
	clone := p
	if p.CurrentRank != nil {
		cr := *p.CurrentRank
		clone.CurrentRank = &cr
	}
	if p.Ranks != nil {
		clone.Ranks = make(PlayerRanksSnapshot, len(p.Ranks))
		for k, v := range p.Ranks {
			clone.Ranks[k] = v
		}
	}
	if p.MatchupRecord != nil {
		mr := *p.MatchupRecord
		clone.MatchupRecord = &mr
	}
	return clone
}

// ResolvedPlayer holds details of the identified local player.
type ResolvedPlayer struct {
	PrimaryID   string `json:"primary_id"`
	Platform    string `json:"platform"`
	AccountID   string `json:"account_id"`
	PlayerName  string `json:"player_name"`
	TeamNum     int    `json:"team_num"`
	MatchReason string `json:"match_reason"`
}

// CurrentMatchResponse defines the complete JSON payload returned by GET /current-match.
type CurrentMatchResponse struct {
	ActiveMatch  bool          `json:"active_match"`
	MatchEnded   bool          `json:"match_ended"`
	MatchGUID    string        `json:"match_guid"`
	PlaylistID   int           `json:"playlist_id"`
	PlaylistName string        `json:"playlist_name"`
	LocalTeam    *int          `json:"local_team,omitempty"`   // 0 = Blue, 1 = Orange, nil if unresolved/spectator
	LocalPlayer  *LobbyPlayer  `json:"local_player,omitempty"` // Dedicated reference to the local user
	Teammates    []LobbyPlayer `json:"teammates"`              // Other players on local player's team
	Opponents    []LobbyPlayer `json:"opponents"`              // Players on opposing team
	Spectators   []LobbyPlayer `json:"spectators,omitempty"`   // Spectators / unassigned
	WinnerTeam   *int          `json:"winner_team,omitempty"`  // Populated once match concludes
	Result       string        `json:"result,omitempty"`       // "victory", "defeat", or "" if in progress
	UpdatedAt    time.Time     `json:"updated_at"`
}

// DeepClone creates a completely isolated copy of CurrentMatchResponse for safe concurrent reads.
func (s *CurrentMatchResponse) DeepClone() *CurrentMatchResponse {
	if s == nil {
		return nil
	}

	clone := &CurrentMatchResponse{
		ActiveMatch:  s.ActiveMatch,
		MatchEnded:   s.MatchEnded,
		MatchGUID:    s.MatchGUID,
		PlaylistID:   s.PlaylistID,
		PlaylistName: s.PlaylistName,
		Result:       s.Result,
		UpdatedAt:    s.UpdatedAt,
	}

	if s.LocalTeam != nil {
		val := *s.LocalTeam
		clone.LocalTeam = &val
	}
	if s.WinnerTeam != nil {
		val := *s.WinnerTeam
		clone.WinnerTeam = &val
	}
	if s.LocalPlayer != nil {
		lp := s.LocalPlayer.DeepClone()
		clone.LocalPlayer = &lp
	}

	clone.Teammates = make([]LobbyPlayer, len(s.Teammates))
	for i := range s.Teammates {
		clone.Teammates[i] = s.Teammates[i].DeepClone()
	}

	clone.Opponents = make([]LobbyPlayer, len(s.Opponents))
	for i := range s.Opponents {
		clone.Opponents[i] = s.Opponents[i].DeepClone()
	}

	clone.Spectators = make([]LobbyPlayer, len(s.Spectators))
	for i := range s.Spectators {
		clone.Spectators[i] = s.Spectators[i].DeepClone()
	}

	return clone
}

type cachedRankEntry struct {
	snapshot  PlayerRanksSnapshot
	fetchedAt time.Time
}

// TrackerOption configures Tracker construction.
type TrackerOption func(*Tracker)

// WithTrackerLogger injects a custom structured logger.
func WithTrackerLogger(logger *slog.Logger) TrackerOption {
	return func(t *Tracker) {
		if logger != nil {
			t.logger = logger
		}
	}
}

// WithTrackerAuthProvider injects an AuthProvider to resolve dynamic credentials.
func WithTrackerAuthProvider(provider auth.AuthProvider) TrackerOption {
	return func(t *Tracker) {
		t.authProvider = provider
	}
}

// WithRankCacheTTL configures the rank cache time-to-live.
func WithRankCacheTTL(d time.Duration) TrackerOption {
	return func(t *Tracker) {
		if d > 0 {
			t.rankCacheTTL = d
		}
	}
}

// WithBackoffTTL configures the error backoff time-to-live.
func WithBackoffTTL(d time.Duration) TrackerOption {
	return func(t *Tracker) {
		if d > 0 {
			t.backoffTTL = d
		}
	}
}

// Tracker coordinates match lifecycle events, local player resolution, ranks, and outcomes.
type Tracker struct {
	mu           sync.RWMutex
	store        storage.StateStore
	rankClient   SkillFetcher
	authProvider auth.AuthProvider
	cfg          config.PlayerTrackingConfig
	authCfg      config.AuthConfig
	logger       *slog.Logger

	// Current active match state
	currentMatch *CurrentMatchResponse

	// Observer listener for match events
	listener MatchStateListener

	// In-memory profile upsert cache: playerID -> lastSeenName (avoids 120Hz DB thrashing)
	upsertedProfiles map[string]string

	// Tier 1: In-Memory Rank Cache (TTL = 15m)
	rankCache    map[string]cachedRankEntry
	rankCacheTTL time.Duration

	// Tier 2: In-Flight RPC Deduplication
	inFlight map[string]time.Time

	// Tier 3: Negative Cache / Error Backoff (Backoff = 60s)
	failBackoff map[string]time.Time
	backoffTTL  time.Duration

	// Tier 4: In-Memory Matchup Cache (Key: "playerID:playlistID")
	matchupCache    map[string]*storage.PlayerMatchup
	matchupInFlight map[string]time.Time

	// Lifecycle & Concurrency
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	closed bool
}

// NewTracker constructs a new Tracker instance.
func NewTracker(store storage.StateStore, rankClient SkillFetcher, cfg config.PlayerTrackingConfig, authCfg config.AuthConfig, opts ...TrackerOption) (*Tracker, error) {
	if store == nil {
		return nil, errors.New("tracker: store cannot be nil")
	}
	if rankClient == nil {
		rankClient = NewNoOpRankClient()
	}

	ctx, cancel := context.WithCancel(context.Background())

	t := &Tracker{
		store:            store,
		rankClient:       rankClient,
		cfg:              cfg,
		authCfg:          authCfg,
		logger:           slog.Default().With(slog.String("component", "playertrack")),
		upsertedProfiles: make(map[string]string),
		rankCache:        make(map[string]cachedRankEntry),
		rankCacheTTL:     defaultRankCacheTTL,
		inFlight:         make(map[string]time.Time),
		failBackoff:      make(map[string]time.Time),
		backoffTTL:       defaultBackoffTTL,
		matchupCache:     make(map[string]*storage.PlayerMatchup),
		matchupInFlight:  make(map[string]time.Time),
		ctx:              ctx,
		cancel:           cancel,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(t)
		}
	}

	return t, nil
}

// Close gracefully closes the Tracker and awaits completion of any background rank queries.
func (t *Tracker) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		t.wg.Wait()
		return nil
	}
	t.closed = true
	if t.cancel != nil {
		t.cancel()
	}
	t.mu.Unlock()

	t.wg.Wait()
	return nil
}

// GetCurrentMatch returns a thread-safe deep clone of the current match snapshot.
func (t *Tracker) GetCurrentMatch() *CurrentMatchResponse {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.currentMatch == nil {
		return &CurrentMatchResponse{
			ActiveMatch: false,
			Teammates:   make([]LobbyPlayer, 0),
			Opponents:   make([]LobbyPlayer, 0),
			Spectators:  make([]LobbyPlayer, 0),
			UpdatedAt:   time.Now().UTC(),
		}
	}

	return t.currentMatch.DeepClone()
}

// OnUpdateState processes a real-time lobby state update from the Stats API Listener.
func (t *Tracker) OnUpdateState(ctx context.Context, matchGUID string, playlistID int, players []statsapi.StatsPlayer) error {
	trimmedGUID := strings.TrimSpace(matchGUID)
	if trimmedGUID == "" || len(players) == 0 {
		return nil
	}

	// 1. Resolve local player and identify team
	resolvedLocal, myTeamNum := t.resolveLocalPlayer(ctx, players)

	// 2. Prepare state update under lock
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return errors.New("tracker is closed")
	}

	now := time.Now().UTC()

	if t.currentMatch == nil || t.currentMatch.MatchGUID != trimmedGUID {
		// New match detected: initialize new snapshot
		t.currentMatch = &CurrentMatchResponse{
			ActiveMatch:  true,
			MatchEnded:   false,
			MatchGUID:    trimmedGUID,
			PlaylistID:   playlistID,
			PlaylistName: FormatPlaylist(playlistID),
			Teammates:    make([]LobbyPlayer, 0),
			Opponents:    make([]LobbyPlayer, 0),
			Spectators:   make([]LobbyPlayer, 0),
			UpdatedAt:    now,
		}
		// Reset frame upsert cache and in-flight matchup queries for new match
		t.upsertedProfiles = make(map[string]string)
		t.matchupInFlight = make(map[string]time.Time)
	} else {
		// Update existing match metadata
		if playlistID != 0 {
			t.currentMatch.PlaylistID = playlistID
			t.currentMatch.PlaylistName = FormatPlaylist(playlistID)
		}
		t.currentMatch.UpdatedAt = now
		// If match was already marked ended, keep ActiveMatch=false and MatchEnded=true
		if !t.currentMatch.MatchEnded {
			t.currentMatch.ActiveMatch = true
		}
	}

	if myTeamNum == 0 || myTeamNum == 1 {
		val := myTeamNum
		t.currentMatch.LocalTeam = &val
	} else {
		t.currentMatch.LocalTeam = nil
	}

	var teammates []LobbyPlayer
	var opponents []LobbyPlayer
	var spectators []LobbyPlayer
	var localPlayerLobby *LobbyPlayer

	var playersToUpsert []statsapi.StatsPlayer
	var playersToFetchRank []rlapi.PlayerID
	var playersToQueryMatchup []statsapi.StatsPlayer

	activePlaylistID := t.currentMatch.PlaylistID

	for _, p := range players {
		isBot := p.IsBot()
		isLocal := resolvedLocal != nil && strings.EqualFold(strings.TrimSpace(p.PrimaryId), strings.TrimSpace(resolvedLocal.PrimaryID))

		// Profile upsert check for non-bot human players
		if !isBot && strings.TrimSpace(p.PrimaryId) != "" {
			if lastSeenName, exists := t.upsertedProfiles[p.PrimaryId]; !exists || lastSeenName != p.Name {
				playersToUpsert = append(playersToUpsert, p)
				t.upsertedProfiles[p.PrimaryId] = p.Name
			}
		}

		// Asynchronous rank query check
		if !isBot && strings.TrimSpace(p.PrimaryId) != "" && t.cfg.AutoFetchRanks && t.rankClient != nil && t.rankClient.IsEnabled() {
			pid := p.PrimaryId
			shouldFetch := true

			// Tier 1: Cache check
			if entry, ok := t.rankCache[pid]; ok {
				if time.Since(entry.fetchedAt) < t.rankCacheTTL {
					shouldFetch = false
				}
			}
			// Tier 2: In-flight check
			if _, inFlight := t.inFlight[pid]; inFlight {
				shouldFetch = false
			}
			// Tier 3: Failure backoff check
			if failTime, failed := t.failBackoff[pid]; failed {
				if time.Since(failTime) < t.backoffTTL {
					shouldFetch = false
				}
			}

			if shouldFetch {
				t.inFlight[pid] = now
				playersToFetchRank = append(playersToFetchRank, rlapi.PlayerID(pid))
			}
		}

		// Parse platform
		parsed, _ := p.ParseID()
		platform := parsed.Platform

		// Extract cached ranks if available
		var ranksSnapshot PlayerRanksSnapshot
		var currentRank *PlayerPlaylistRank
		if entry, ok := t.rankCache[p.PrimaryId]; ok {
			ranksSnapshot = entry.snapshot
			if r, found := ranksSnapshot.GetRank(activePlaylistID); found {
				currentRank = &r
			}
		}

		// Extract cached matchup record if available
		var matchupRecord *storage.PlayerMatchup
		if activePlaylistID != 0 && !isBot {
			mKey := fmt.Sprintf("%s:%d", p.PrimaryId, activePlaylistID)
			if m, ok := t.matchupCache[mKey]; ok {
				matchupRecord = m
			} else if !isLocal {
				// Queue for matchup history lookup if not already in-flight
				if _, inFlight := t.matchupInFlight[mKey]; !inFlight {
					t.matchupInFlight[mKey] = now
					playersToQueryMatchup = append(playersToQueryMatchup, p)
				}
			}
		}

		lp := LobbyPlayer{
			PlayerID: p.PrimaryId,
			Platform: platform,
			Name:     p.Name,
			TeamNum:  p.TeamNum,
			IsLocal:  isLocal,
			IsBot:    isBot,
			Stats: PlayerStatsSummary{
				Score:   p.Score,
				Goals:   p.Goals,
				Assists: p.Assists,
				Saves:   p.Saves,
				Shots:   p.Shots,
				Demos:   p.Demos,
			},
			CurrentRank:   currentRank,
			Ranks:         ranksSnapshot,
			MatchupRecord: matchupRecord,
		}

		if isLocal {
			localPlayerLobby = &lp
		} else if (myTeamNum == 0 || myTeamNum == 1) && p.TeamNum == myTeamNum {
			teammates = append(teammates, lp)
		} else if (myTeamNum == 0 || myTeamNum == 1) && (p.TeamNum == 0 || p.TeamNum == 1) {
			opponents = append(opponents, lp)
		} else {
			spectators = append(spectators, lp)
		}
	}

	t.currentMatch.LocalPlayer = localPlayerLobby
	t.currentMatch.Teammates = teammates
	t.currentMatch.Opponents = opponents
	t.currentMatch.Spectators = spectators

	var matchClone *CurrentMatchResponse
	if t.currentMatch != nil {
		matchClone = t.currentMatch.DeepClone()
	}
	listener := t.listener
	t.mu.Unlock()

	if listener != nil && matchClone != nil {
		listener.OnActiveMatchUpdated(matchClone)
	}

	// 3. Perform database operations outside the lock
	for _, p := range playersToUpsert {
		parsed, _ := p.ParseID()
		rec := &storage.PlayerRecord{
			PlayerID:    p.PrimaryId,
			Platform:    parsed.Platform,
			PlayerName:  p.Name,
			RanksJSON:   "{}",
			FirstSeenAt: now,
			LastSeenAt:  now,
		}
		if err := t.store.UpsertPlayer(ctx, rec); err != nil {
			t.logger.Warn("failed to upsert player profile",
				slog.String("player_id", p.PrimaryId),
				slog.Any("error", err),
			)
		}
	}

	// 4. Warm matchup cache for players needing history lookup
	if len(playersToQueryMatchup) > 0 && activePlaylistID != 0 {
		var updatedSnapshot *CurrentMatchResponse
		for _, p := range playersToQueryMatchup {
			mKey := fmt.Sprintf("%s:%d", p.PrimaryId, activePlaylistID)
			matchup, err := t.store.GetPlayerMatchup(ctx, p.PrimaryId, activePlaylistID)

			t.mu.Lock()
			delete(t.matchupInFlight, mKey)
			if err == nil && matchup != nil {
				t.matchupCache[mKey] = matchup
				if t.currentMatch != nil && t.currentMatch.MatchGUID == trimmedGUID {
					t.updateLobbyPlayerMatchupLocked(p.PrimaryId, matchup)
					updatedSnapshot = t.currentMatch.DeepClone()
				}
			}
			t.mu.Unlock()
		}
		if updatedSnapshot != nil {
			t.mu.RLock()
			l := t.listener
			t.mu.RUnlock()
			if l != nil {
				l.OnActiveMatchUpdated(updatedSnapshot)
			}
		}
	}

	// 5. Dispatch async rank fetching
	if len(playersToFetchRank) > 0 {
		t.mu.Lock()
		if !t.closed {
			t.wg.Add(1)
			go func(pids []rlapi.PlayerID) {
				defer t.wg.Done()
				t.fetchRanksBatch(t.ctx, pids)
			}(playersToFetchRank)
		} else {
			for _, pid := range playersToFetchRank {
				delete(t.inFlight, string(pid))
			}
		}
		t.mu.Unlock()
	}

	return nil
}

// OnMatchEnded processes a match completion event and atomically records outcomes.
func (t *Tracker) OnMatchEnded(ctx context.Context, matchGUID string, winnerTeamNum *int) error {
	trimmedGUID := strings.TrimSpace(matchGUID)
	if trimmedGUID == "" {
		return nil
	}

	if winnerTeamNum == nil || (*winnerTeamNum != 0 && *winnerTeamNum != 1) {
		t.logger.Info("match ended without valid winner; skipping outcome compilation",
			slog.String("match_guid", trimmedGUID),
		)
		t.mu.Lock()
		var matchClone *CurrentMatchResponse
		if t.currentMatch != nil && t.currentMatch.MatchGUID == trimmedGUID {
			t.currentMatch.MatchEnded = true
			t.currentMatch.ActiveMatch = false
			t.currentMatch.WinnerTeam = winnerTeamNum
			matchClone = t.currentMatch.DeepClone()
		}
		listener := t.listener
		t.mu.Unlock()

		if listener != nil && matchClone != nil {
			listener.OnMatchConcluded(matchClone)
		}
		return nil
	}

	t.mu.Lock()
	if t.currentMatch == nil || t.currentMatch.MatchGUID != trimmedGUID {
		t.mu.Unlock()
		t.logger.Warn("match ended but no active match state cached; skipping outcome recording",
			slog.String("match_guid", trimmedGUID),
		)
		return nil
	}

	matchState := t.currentMatch
	matchState.MatchEnded = true
	matchState.ActiveMatch = false
	matchState.WinnerTeam = winnerTeamNum

	localPlayer := matchState.LocalPlayer
	localTeam := matchState.LocalTeam
	playlistID := matchState.PlaylistID

	if localPlayer == nil || localTeam == nil || (*localTeam != 0 && *localTeam != 1) {
		matchClone := matchState.DeepClone()
		listener := t.listener
		t.mu.Unlock()
		t.logger.Warn("match ended but local player not playing on a valid team; skipping matchup outcomes",
			slog.String("match_guid", trimmedGUID),
		)
		if listener != nil && matchClone != nil {
			listener.OnMatchConcluded(matchClone)
		}
		return nil
	}

	myTeamNum := *localTeam
	winner := *winnerTeamNum
	myTeamWon := (winner == myTeamNum)

	if myTeamWon {
		matchState.Result = "victory"
	} else {
		matchState.Result = "defeat"
	}

	// Snapshot all participants
	var roster []LobbyPlayer
	roster = append(roster, matchState.Teammates...)
	roster = append(roster, matchState.Opponents...)
	concludedSnapshot := matchState.DeepClone()
	listener := t.listener
	t.mu.Unlock()

	// Compile outcomes vector
	outcomes := make([]storage.PlayerOutcome, 0, len(roster))
	seen := make(map[string]bool)

	for _, p := range roster {
		if p.IsBot {
			continue // AI bots never recorded in matchups
		}
		if p.IsLocal || strings.EqualFold(strings.TrimSpace(p.PlayerID), strings.TrimSpace(localPlayer.PlayerID)) {
			continue // Self never recorded in matchups
		}
		if seen[p.PlayerID] {
			continue // Deduplicate
		}
		seen[p.PlayerID] = true

		isTeammate := (p.TeamNum == myTeamNum)
		outcomes = append(outcomes, storage.PlayerOutcome{
			PlayerID:   p.PlayerID,
			Platform:   p.Platform,
			PlayerName: p.Name,
			IsTeammate: isTeammate,
			Won:        myTeamWon,
		})
	}

	// Persist match results atomically into storage
	err := t.store.RecordMatchResults(ctx, trimmedGUID, playlistID, outcomes)
	if err != nil {
		if errors.Is(err, storage.ErrMatchAlreadyProcessed) {
			t.logger.Debug("match already processed, ignoring duplicate MatchEnded event",
				slog.String("match_guid", trimmedGUID),
			)
			if listener != nil && concludedSnapshot != nil {
				listener.OnMatchConcluded(concludedSnapshot)
			}
			return nil
		}
		t.logger.Error("failed to record match results in store",
			slog.String("match_guid", trimmedGUID),
			slog.Any("error", err),
		)
		return err
	}

	// Invalidate matchup cache for players whose records were updated
	t.mu.Lock()
	for _, o := range outcomes {
		mKey := fmt.Sprintf("%s:%d", o.PlayerID, playlistID)
		delete(t.matchupCache, mKey)
	}
	t.mu.Unlock()

	t.logger.Info("successfully recorded match results",
		slog.String("match_guid", trimmedGUID),
		slog.Int("playlist_id", playlistID),
		slog.Bool("my_team_won", myTeamWon),
		slog.Int("players_tracked", len(outcomes)),
	)

	if listener != nil && concludedSnapshot != nil {
		listener.OnMatchConcluded(concludedSnapshot)
	}

	return nil
}

// ResolveLocalPlayer runs the 4-tier local player resolution hierarchy.
// Exported for testing and diagnostic visibility.
func (t *Tracker) ResolveLocalPlayer(players []statsapi.StatsPlayer) (*ResolvedPlayer, int) {
	return t.resolveLocalPlayer(context.Background(), players)
}

func (t *Tracker) resolveLocalPlayer(ctx context.Context, players []statsapi.StatsPlayer) (*ResolvedPlayer, int) {
	var humanPlayers []statsapi.StatsPlayer
	for _, p := range players {
		if !p.IsBot() {
			humanPlayers = append(humanPlayers, p)
		}
	}
	if len(humanPlayers) == 0 {
		return nil, -1
	}

	// --- Tier 1: Configured LocalPlayerID ---
	if configuredID := strings.TrimSpace(t.cfg.LocalPlayerID); configuredID != "" {
		for _, p := range humanPlayers {
			if strings.EqualFold(strings.TrimSpace(p.PrimaryId), configuredID) {
				return t.buildResolvedPlayer(p, "Tier 1: LocalPlayerID"), p.TeamNum
			}
			parsed, err := p.ParseID()
			if err == nil && strings.EqualFold(strings.TrimSpace(parsed.AccountID), configuredID) {
				return t.buildResolvedPlayer(p, "Tier 1: LocalPlayerID (AccountID match)"), p.TeamNum
			}
			// Prefix match without splitscreen index
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(p.PrimaryId)), strings.ToLower(configuredID)+"|") {
				return t.buildResolvedPlayer(p, "Tier 1: LocalPlayerID (Prefix match)"), p.TeamNum
			}
		}
	}

	// --- Tier 2: Primary Auth Account ID ---
	authPlatform, authAccountID := t.resolveAuthCredentials(ctx)
	if authPlatform != "" && authAccountID != "" {
		for _, p := range humanPlayers {
			parsed, err := p.ParseID()
			if err == nil {
				if strings.EqualFold(parsed.Platform, authPlatform) && strings.EqualFold(parsed.AccountID, authAccountID) {
					return t.buildResolvedPlayer(p, "Tier 2: Primary Auth ID"), p.TeamNum
				}
			}
			expectedPrimaryID := fmt.Sprintf("%s|%s|0", authPlatform, authAccountID)
			if strings.EqualFold(strings.TrimSpace(p.PrimaryId), expectedPrimaryID) {
				return t.buildResolvedPlayer(p, "Tier 2: Primary Auth ID (Exact match)"), p.TeamNum
			}
		}
	}

	// --- Tier 3: Configured LocalPlayerName ---
	if configuredName := strings.TrimSpace(t.cfg.LocalPlayerName); configuredName != "" {
		for _, p := range humanPlayers {
			if strings.EqualFold(strings.TrimSpace(p.Name), configuredName) {
				return t.buildResolvedPlayer(p, "Tier 3: LocalPlayerName"), p.TeamNum
			}
		}
	}

	// --- Tier 4: Primary Auth Display Name ---
	if authDisplayName := t.resolveAuthDisplayName(ctx); authDisplayName != "" {
		for _, p := range humanPlayers {
			if strings.EqualFold(strings.TrimSpace(p.Name), authDisplayName) {
				return t.buildResolvedPlayer(p, "Tier 4: Auth Display Name"), p.TeamNum
			}
		}
	}

	return nil, -1
}

func (t *Tracker) buildResolvedPlayer(p statsapi.StatsPlayer, reason string) *ResolvedPlayer {
	parsed, _ := p.ParseID()
	return &ResolvedPlayer{
		PrimaryID:   p.PrimaryId,
		Platform:    parsed.Platform,
		AccountID:   parsed.AccountID,
		PlayerName:  p.Name,
		TeamNum:     p.TeamNum,
		MatchReason: reason,
	}
}

func (t *Tracker) resolveAuthCredentials(ctx context.Context) (platform, accountID string) {
	provider := strings.ToLower(strings.TrimSpace(t.authCfg.Provider))
	switch provider {
	case "epic":
		platform = "Epic"
		accountID = strings.TrimSpace(t.authCfg.Epic.AccountID)
		if accountID == "" && t.authProvider != nil {
			if tok := t.authProvider.TokenInfo(); tok != nil {
				accountID = strings.TrimSpace(tok.EpicAccountID)
				if accountID == "" {
					accountID = strings.TrimSpace(tok.AccountID)
				}
			}
		}
		if accountID == "" && t.store != nil {
			_, acct, _, _ := t.store.GetAuthState(ctx, "epic")
			accountID = strings.TrimSpace(acct)
		}
	case "steam":
		platform = "Steam"
		accountID = strings.TrimSpace(t.authCfg.Steam.SteamID64)
		if accountID == "" && t.authProvider != nil {
			if tok := t.authProvider.TokenInfo(); tok != nil {
				accountID = strings.TrimSpace(tok.AccountID)
			}
		}
		if accountID == "" && t.store != nil {
			_, acct, _, _ := t.store.GetAuthState(ctx, "steam")
			accountID = strings.TrimSpace(acct)
		}
	}
	return platform, accountID
}

func (t *Tracker) resolveAuthDisplayName(ctx context.Context) string {
	provider := strings.ToLower(strings.TrimSpace(t.authCfg.Provider))
	switch provider {
	case "epic":
		if d := strings.TrimSpace(t.authCfg.Epic.DisplayName); d != "" {
			return d
		}
		if t.authProvider != nil {
			if tok := t.authProvider.TokenInfo(); tok != nil && strings.TrimSpace(tok.DisplayName) != "" {
				return strings.TrimSpace(tok.DisplayName)
			}
		}
		if t.store != nil {
			_, _, d, _ := t.store.GetAuthState(ctx, "epic")
			return strings.TrimSpace(d)
		}
	case "steam":
		if d := strings.TrimSpace(t.authCfg.Steam.AccountName); d != "" {
			return d
		}
		if t.authProvider != nil {
			if tok := t.authProvider.TokenInfo(); tok != nil && strings.TrimSpace(tok.DisplayName) != "" {
				return strings.TrimSpace(tok.DisplayName)
			}
		}
		if t.store != nil {
			_, _, d, _ := t.store.GetAuthState(ctx, "steam")
			return strings.TrimSpace(d)
		}
	}
	return ""
}

func (t *Tracker) fetchRanksBatch(ctx context.Context, playerIDs []rlapi.PlayerID) {
	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	results, err := t.rankClient.GetPlayersSkills(fetchCtx, playerIDs)
	now := time.Now().UTC()

	t.mu.Lock()
	if err != nil {
		if !t.closed {
			t.logger.Warn("async rank retrieval failed",
				slog.Int("player_count", len(playerIDs)),
				slog.Any("error", err),
			)
			for _, pid := range playerIDs {
				idStr := string(pid)
				delete(t.inFlight, idStr)
				t.failBackoff[idStr] = now
			}
		} else {
			for _, pid := range playerIDs {
				delete(t.inFlight, string(pid))
			}
		}
		t.mu.Unlock()
		return
	}

	skillMap := make(map[string][]rlapi.Skill, len(results))
	for _, res := range results {
		skillMap[string(res.PlayerID)] = res.Skills
	}

	var toPersist []struct {
		playerID  string
		ranksJSON string
	}

	for _, pid := range playerIDs {
		idStr := string(pid)
		delete(t.inFlight, idStr)
		delete(t.failBackoff, idStr)

		skills, found := skillMap[idStr]
		if !found {
			skills = []rlapi.Skill{}
		}

		snapshot := BuildRanksSnapshot(skills)
		t.rankCache[idStr] = cachedRankEntry{
			snapshot:  snapshot,
			fetchedAt: now,
		}

		if t.currentMatch != nil {
			t.updateLobbyPlayerRanksLocked(idStr, snapshot)
		}

		ranksJSON, serErr := SerializeRanksJSON(skills)
		if serErr == nil {
			toPersist = append(toPersist, struct {
				playerID  string
				ranksJSON string
			}{playerID: idStr, ranksJSON: ranksJSON})
		}
	}

	// Asynchronously persist to database, guarding t.wg.Add under t.mu.Lock()
	if !t.closed {
		for _, item := range toPersist {
			t.wg.Add(1)
			go func(pID, rJSON string) {
				defer t.wg.Done()
				t.persistPlayerRanks(t.ctx, pID, rJSON)
			}(item.playerID, item.ranksJSON)
		}
	}

	var rankSnapshot *CurrentMatchResponse
	if t.currentMatch != nil && len(playerIDs) > 0 {
		rankSnapshot = t.currentMatch.DeepClone()
	}
	listener := t.listener
	t.mu.Unlock()

	if listener != nil && rankSnapshot != nil {
		listener.OnActiveMatchUpdated(rankSnapshot)
	}
}

func (t *Tracker) updateLobbyPlayerRanksLocked(playerID string, snapshot PlayerRanksSnapshot) {
	if t.currentMatch == nil {
		return
	}

	pid := t.currentMatch.PlaylistID
	var currentRank *PlayerPlaylistRank
	if r, ok := snapshot.GetRank(pid); ok {
		currentRank = &r
	}

	updatePlayer := func(p *LobbyPlayer) {
		if p != nil && strings.EqualFold(strings.TrimSpace(p.PlayerID), strings.TrimSpace(playerID)) {
			p.Ranks = snapshot
			p.CurrentRank = currentRank
		}
	}

	if t.currentMatch.LocalPlayer != nil {
		updatePlayer(t.currentMatch.LocalPlayer)
	}
	for i := range t.currentMatch.Teammates {
		updatePlayer(&t.currentMatch.Teammates[i])
	}
	for i := range t.currentMatch.Opponents {
		updatePlayer(&t.currentMatch.Opponents[i])
	}
	for i := range t.currentMatch.Spectators {
		updatePlayer(&t.currentMatch.Spectators[i])
	}
}

func (t *Tracker) updateLobbyPlayerMatchupLocked(playerID string, matchup *storage.PlayerMatchup) {
	if t.currentMatch == nil {
		return
	}

	updatePlayer := func(p *LobbyPlayer) {
		if p != nil && strings.EqualFold(strings.TrimSpace(p.PlayerID), strings.TrimSpace(playerID)) {
			p.MatchupRecord = matchup
		}
	}

	if t.currentMatch.LocalPlayer != nil {
		updatePlayer(t.currentMatch.LocalPlayer)
	}
	for i := range t.currentMatch.Teammates {
		updatePlayer(&t.currentMatch.Teammates[i])
	}
	for i := range t.currentMatch.Opponents {
		updatePlayer(&t.currentMatch.Opponents[i])
	}
	for i := range t.currentMatch.Spectators {
		updatePlayer(&t.currentMatch.Spectators[i])
	}
}

func (t *Tracker) persistPlayerRanks(ctx context.Context, playerID, ranksJSON string) {
	dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	err := t.store.UpdatePlayerRanks(dbCtx, playerID, ranksJSON)
	if err != nil {
		if errors.Is(err, context.Canceled) || dbCtx.Err() != nil {
			return
		}
		if errors.Is(err, storage.ErrPlayerNotFound) {
			parsed, _ := statsapi.ParsePrimaryID(playerID)
			now := time.Now().UTC()
			upsertErr := t.store.UpsertPlayer(dbCtx, &storage.PlayerRecord{
				PlayerID:    playerID,
				Platform:    parsed.Platform,
				PlayerName:  "",
				RanksJSON:   ranksJSON,
				FirstSeenAt: now,
				LastSeenAt:  now,
			})
			if upsertErr != nil {
				if errors.Is(upsertErr, context.Canceled) || dbCtx.Err() != nil {
					return
				}
				t.logger.Warn("failed fallback UpsertPlayer after UpdatePlayerRanks",
					slog.String("player_id", playerID),
					slog.Any("error", upsertErr),
				)
			}
			return
		}
		t.logger.Warn("failed to persist player ranks to database",
			slog.String("player_id", playerID),
			slog.Any("error", err),
		)
	}
}
