package playertrack

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/dank/rl-api-utils/internal/auth"
	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
)

// Ensure Tracker satisfies statsapi.PlayerEventHandler and statsapi.GameStateEventHandler at compile time.
var (
	_ statsapi.PlayerEventHandler    = (*Tracker)(nil)
	_ statsapi.GameStateEventHandler = (*Tracker)(nil)
)

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

	// Tier 4: In-Memory Matchup & Scouting Cache (Key: "playerID:playlistID" and per-player maps)
	matchupCache            map[string]*storage.PlayerMatchup
	matchupsByPlaylistCache map[string]map[string]*storage.PlayerMatchup
	scoutingCache           map[string]cachedScoutingEntry
	matchupInFlight         map[string]time.Time

	// Post-Match MMR Poller State ("idle", "polling", "synced")
	mmrPollerState        string
	lastMMRSyncLatencyMs  int64
	postMatchPollSchedule []time.Duration
	pollerCancel          context.CancelFunc

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
		store:                 store,
		rankClient:            rankClient,
		cfg:                   cfg,
		authCfg:               authCfg,
		logger:                slog.Default().With(slog.String("component", "playertrack")),
		upsertedProfiles:      make(map[string]string),
		rankCache:             make(map[string]cachedRankEntry),
		rankCacheTTL:          defaultRankCacheTTL,
		inFlight:              make(map[string]time.Time),
		failBackoff:           make(map[string]time.Time),
		backoffTTL:            defaultBackoffTTL,
		matchupCache:            make(map[string]*storage.PlayerMatchup),
		matchupsByPlaylistCache: make(map[string]map[string]*storage.PlayerMatchup),
		scoutingCache:           make(map[string]cachedScoutingEntry),
		matchupInFlight:         make(map[string]time.Time),
		mmrPollerState:        "idle",
		postMatchPollSchedule: append([]time.Duration(nil), defaultPostMatchPollSchedule...),
		ctx:                   ctx,
		cancel:                cancel,
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
	if t.pollerCancel != nil {
		t.pollerCancel()
	}
	if t.cancel != nil {
		t.cancel()
	}
	t.mu.Unlock()

	t.wg.Wait()
	return nil
}

// MMRPollerState returns the current post-match MMR poller state ("idle", "polling", or "synced").
func (t *Tracker) MMRPollerState() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.mmrPollerState == "" {
		return "idle"
	}
	return t.mmrPollerState
}

// LastMMRSyncLatencyMs returns the latency in milliseconds of the most recent post-match MMR sync.
func (t *Tracker) LastMMRSyncLatencyMs() int64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.lastMMRSyncLatencyMs
}

// IsRankAPIReady reports whether the underlying RankClient is enabled and ready for queries.
func (t *Tracker) IsRankAPIReady() bool {
	t.mu.RLock()
	rc := t.rankClient
	closed := t.closed
	t.mu.RUnlock()
	if closed || rc == nil {
		return false
	}
	return rc.IsEnabled()
}

// GetCurrentMatch returns a thread-safe deep clone of the current match snapshot.
func (t *Tracker) GetCurrentMatch() *CurrentMatchResponse {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.currentMatch == nil {
		return &CurrentMatchResponse{
			ActiveMatch:    false,
			LocalPlayerIDs: t.getLocalPlayerIDsLocked(),
			Teammates:      make([]LobbyPlayer, 0),
			Opponents:      make([]LobbyPlayer, 0),
			Spectators:     make([]LobbyPlayer, 0),
			UpdatedAt:      time.Now().UTC(),
		}
	}

	clone := t.currentMatch.DeepClone()
	if len(clone.LocalPlayerIDs) == 0 {
		clone.LocalPlayerIDs = t.getLocalPlayerIDsLocked()
	}
	return clone
}

// GetLocalPlayerIDs returns all authenticated and discovered local player identifiers.
func (t *Tracker) GetLocalPlayerIDs(ctx context.Context) []string {
	t.mu.RLock()
	ids := t.getLocalPlayerIDsLocked()
	t.mu.RUnlock()

	if t.store != nil {
		if storeIDs, err := t.store.GetLocalPlayerIDs(ctx); err == nil {
			seen := make(map[string]bool, len(ids)+len(storeIDs))
			for _, id := range ids {
				seen[id] = true
			}
			for _, sid := range storeIDs {
				sid = strings.TrimSpace(sid)
				if sid != "" && !seen[sid] {
					seen[sid] = true
					ids = append(ids, sid)
				}
			}
		}
	}
	return ids
}

type cachedScoutingEntry struct {
	notes  string
	tags   []string
	loaded bool
}

// UpdatePlayerScouting updates in-memory scouting notes and tags for a player and broadcasts
// an active match update immediately if that player is in the current live lobby.
func (t *Tracker) UpdatePlayerScouting(playerID string, notes string, tags []string) {
	trimmed := strings.TrimSpace(playerID)
	if trimmed == "" {
		return
	}
	var copiedTags []string
	if len(tags) > 0 {
		copiedTags = append([]string(nil), tags...)
	}

	t.mu.Lock()
	normKey := trimmed
	for existingKey := range t.scoutingCache {
		if strings.EqualFold(existingKey, trimmed) {
			normKey = existingKey
			break
		}
	}
	t.scoutingCache[normKey] = cachedScoutingEntry{
		notes:  notes,
		tags:   copiedTags,
		loaded: true,
	}

	var updatedSnapshot *CurrentMatchResponse
	var listener MatchStateListener
	if t.currentMatch != nil {
		matched := false
		visitMatchPlayers(t.currentMatch, trimmed, func(p *LobbyPlayer) {
			p.Notes = notes
			if len(copiedTags) > 0 {
				p.Tags = append([]string(nil), copiedTags...)
			} else {
				p.Tags = nil
			}
			matched = true
		})
		if matched {
			updatedSnapshot = t.currentMatch.DeepClone()
			listener = t.listener
		}
	}
	t.mu.Unlock()

	if listener != nil && updatedSnapshot != nil {
		listener.OnActiveMatchUpdated(updatedSnapshot)
	}
}
