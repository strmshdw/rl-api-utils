package playertrack

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/dank/rl-api-utils/internal/domain"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rlapi"
)

// InvalidateRankCache purges cached rank, in-flight, and failure backoff states.
// If keys is empty or contains a single empty string, all cached rank entries are flushed.
func (t *Tracker) InvalidateRankCache(keys ...string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(keys) == 0 || (len(keys) == 1 && strings.TrimSpace(keys[0]) == "") {
		t.rankCache = make(map[string]cachedRankEntry)
		t.inFlight = make(map[string]time.Time)
		t.failBackoff = make(map[string]time.Time)
		return
	}
	for _, k := range keys {
		pid := strings.TrimSpace(k)
		if pid == "" {
			continue
		}
		delete(t.rankCache, pid)
		delete(t.inFlight, pid)
		delete(t.failBackoff, pid)
	}
}

// FetchPlayerRanksUncached queries PsyNet directly for the given player IDs, bypassing the in-memory rankCache.
func (t *Tracker) FetchPlayerRanksUncached(ctx context.Context, playerIDs []rlapi.PlayerID) (map[string]PlayerRanksSnapshot, error) {
	if len(playerIDs) == 0 {
		return make(map[string]PlayerRanksSnapshot), nil
	}
	t.mu.RLock()
	rc := t.rankClient
	closed := t.closed
	t.mu.RUnlock()
	if closed || rc == nil || !rc.IsEnabled() {
		return nil, ErrRankClientClosed
	}

	skillsList, err := rc.GetPlayersSkills(ctx, playerIDs)
	if err != nil {
		return nil, err
	}

	out := make(map[string]PlayerRanksSnapshot, len(skillsList))
	for _, entry := range skillsList {
		pid := strings.TrimSpace(string(entry.PlayerID))
		if pid == "" {
			continue
		}
		out[pid] = BuildRanksSnapshot(entry.Skills)
	}
	return out, nil
}

func evictExpiredAndOldest[V any](m map[string]V, maxEntries int, ttl time.Duration, timestampOf func(V) time.Time) {
	if len(m) <= maxEntries {
		return
	}
	now := time.Now().UTC()
	for k, v := range m {
		if now.Sub(timestampOf(v)) >= ttl {
			delete(m, k)
		}
	}
	for len(m) > maxEntries {
		var oldestKey string
		var oldestTime time.Time
		for k, v := range m {
			ts := timestampOf(v)
			if oldestKey == "" || ts.Before(oldestTime) {
				oldestKey = k
				oldestTime = ts
			}
		}
		if oldestKey == "" {
			break
		}
		delete(m, oldestKey)
	}
}

func (t *Tracker) boundRankCacheLocked() {
	evictExpiredAndOldest(t.rankCache, maxRankCacheEntries, t.rankCacheTTL, func(e cachedRankEntry) time.Time {
		return e.fetchedAt
	})
}

func (t *Tracker) boundFailBackoffLocked() {
	evictExpiredAndOldest(t.failBackoff, maxFailBackoffEntries, t.backoffTTL, func(ts time.Time) time.Time {
		return ts
	})
}

func (t *Tracker) boundMatchupCacheLocked() {
	if len(t.matchupCache) <= maxMatchupCacheEntries {
		return
	}
	toEvict := len(t.matchupCache) - maxMatchupCacheEntries
	count := 0
	for k := range t.matchupCache {
		delete(t.matchupCache, k)
		count++
		if count >= toEvict {
			break
		}
	}
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
			t.boundFailBackoffLocked()
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

	type pendingPersist struct {
		playerID  string
		ranksJSON string
	}
	var toPersist []pendingPersist

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
		t.boundRankCacheLocked()

		if t.currentMatch != nil {
			t.updateLobbyPlayerRanksLocked(idStr, snapshot)
		}

		if ranksJSON, serErr := SerializeRanksJSON(skills); serErr == nil {
			toPersist = append(toPersist, pendingPersist{playerID: idStr, ranksJSON: ranksJSON})
		}
	}

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

func (t *Tracker) persistPlayerRanks(ctx context.Context, playerID, ranksJSON string) {
	dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	err := t.store.UpdatePlayerRanks(dbCtx, playerID, ranksJSON)
	if err == nil {
		return
	}
	if errors.Is(err, context.Canceled) || dbCtx.Err() != nil {
		return
	}
	if errors.Is(err, storage.ErrPlayerNotFound) {
		parsed, _ := domain.ParsePrimaryID(playerID)
		now := time.Now().UTC()
		upsertErr := t.store.UpsertPlayer(dbCtx, &storage.PlayerRecord{
			PlayerID:    playerID,
			Platform:    parsed.Platform,
			PlayerName:  "",
			RanksJSON:   ranksJSON,
			FirstSeenAt: now,
			LastSeenAt:  now,
		})
		if upsertErr != nil && !errors.Is(upsertErr, context.Canceled) && dbCtx.Err() == nil {
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
