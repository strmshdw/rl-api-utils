package playertrack

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/storage"
)

// TestChallenger2_BoundedCache_MassiveOverflow_2500PlusEntries tests bounded cache
// overflow behavior when flooded with 3,000+ entries across rankCache, matchupCache,
// and failBackoff under concurrent insertions, lookups, and invalidations.
func TestChallenger2_BoundedCache_MassiveOverflow_2500PlusEntries(t *testing.T) {
	store, err := storage.NewJSONStore(t.TempDir() + "/overflow_state.json")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	tracker, err := NewTracker(store, NewNoOpRankClient(), config.PlayerTrackingConfig{}, config.AuthConfig{})
	if err != nil {
		t.Fatalf("failed to create tracker: %v", err)
	}
	defer tracker.Close()

	const floodCount = 3000
	now := time.Now().UTC()

	// Step 1: Sequential insertion of 3000 items into rankCache and bounding check
	tracker.mu.Lock()
	for i := 0; i < floodCount; i++ {
		key := fmt.Sprintf("Steam|overflow_player_%05d|0", i)
		tracker.rankCache[key] = cachedRankEntry{
			snapshot: PlayerRanksSnapshot{
				"11": PlayerPlaylistRank{PlaylistID: 11, MMR: float64(1000 + i)},
			},
			fetchedAt: now.Add(-time.Duration(floodCount-i) * time.Millisecond),
		}
	}
	if len(tracker.rankCache) != floodCount {
		t.Fatalf("expected initial rankCache size %d, got %d", floodCount, len(tracker.rankCache))
	}

	tracker.boundRankCacheLocked()

	if len(tracker.rankCache) > maxRankCacheEntries {
		t.Fatalf("rankCache size %d exceeded maxRankCacheEntries %d", len(tracker.rankCache), maxRankCacheEntries)
	}
	if len(tracker.rankCache) != maxRankCacheEntries {
		t.Fatalf("rankCache size %d != maxRankCacheEntries %d", len(tracker.rankCache), maxRankCacheEntries)
	}

	// Verify FIFO order: items 0..(floodCount-maxRankCacheEntries-1) must be deleted
	for i := 0; i < floodCount-maxRankCacheEntries; i++ {
		evictedKey := fmt.Sprintf("Steam|overflow_player_%05d|0", i)
		if _, exists := tracker.rankCache[evictedKey]; exists {
			t.Fatalf("expected evicted key %s to not exist", evictedKey)
		}
	}
	// Items (floodCount-maxRankCacheEntries)..floodCount-1 must be retained
	for i := floodCount - maxRankCacheEntries; i < floodCount; i++ {
		retainedKey := fmt.Sprintf("Steam|overflow_player_%05d|0", i)
		if _, exists := tracker.rankCache[retainedKey]; !exists {
			t.Fatalf("expected retained key %s to exist", retainedKey)
		}
	}
	tracker.mu.Unlock()

	// Step 2: Flood matchupCache with 3000 items and bounding check
	tracker.mu.Lock()
	for i := 0; i < floodCount; i++ {
		key := fmt.Sprintf("Steam|matchup_overflow_%05d|0:11", i)
		tracker.matchupCache[key] = &storage.PlayerMatchup{
			PlayerID:   fmt.Sprintf("Steam|matchup_overflow_%05d|0", i),
			PlaylistID: 11,
		}
	}
	tracker.boundMatchupCacheLocked()
	if len(tracker.matchupCache) > maxMatchupCacheEntries {
		t.Fatalf("matchupCache size %d exceeded maxMatchupCacheEntries %d", len(tracker.matchupCache), maxMatchupCacheEntries)
	}
	if len(tracker.matchupCache) != maxMatchupCacheEntries {
		t.Fatalf("matchupCache size %d != maxMatchupCacheEntries %d", len(tracker.matchupCache), maxMatchupCacheEntries)
	}
	tracker.mu.Unlock()

	// Step 3: Flood failBackoff with 1500 items and bounding check
	const failCount = 1500
	tracker.mu.Lock()
	for i := 0; i < failCount; i++ {
		key := fmt.Sprintf("Steam|fail_overflow_%05d|0", i)
		tracker.failBackoff[key] = now.Add(-time.Duration(failCount-i) * time.Millisecond)
	}
	tracker.boundFailBackoffLocked()
	if len(tracker.failBackoff) > maxFailBackoffEntries {
		t.Fatalf("failBackoff size %d exceeded maxFailBackoffEntries %d", len(tracker.failBackoff), maxFailBackoffEntries)
	}
	if len(tracker.failBackoff) != maxFailBackoffEntries {
		t.Fatalf("failBackoff size %d != maxFailBackoffEntries %d", len(tracker.failBackoff), maxFailBackoffEntries)
	}
	tracker.mu.Unlock()

	// Step 4: High concurrency stress with race detector: 20 goroutines flooding, bounding, invalidating
	const numGoroutines = 20
	const opsPerGoroutine = 50
	var wg sync.WaitGroup

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		gid := g
		go func() {
			defer wg.Done()
			for op := 0; op < opsPerGoroutine; op++ {
				playerKey := fmt.Sprintf("Steam|concurrent_%d_%d|0", gid, op)

				tracker.mu.Lock()
				tracker.rankCache[playerKey] = cachedRankEntry{
					snapshot:  PlayerRanksSnapshot{},
					fetchedAt: time.Now().UTC(),
				}
				tracker.boundRankCacheLocked()

				tracker.matchupCache[playerKey+":11"] = &storage.PlayerMatchup{
					PlayerID:   playerKey,
					PlaylistID: 11,
				}
				tracker.boundMatchupCacheLocked()
				tracker.mu.Unlock()

				// Random invalidation
				if rand.Intn(5) == 0 {
					tracker.InvalidateRankCache(playerKey)
				}
			}
		}()
	}

	wg.Wait()

	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	if len(tracker.rankCache) > maxRankCacheEntries {
		t.Errorf("rankCache size %d exceeded cap %d under concurrent load", len(tracker.rankCache), maxRankCacheEntries)
	}
	if len(tracker.matchupCache) > maxMatchupCacheEntries {
		t.Errorf("matchupCache size %d exceeded cap %d under concurrent load", len(tracker.matchupCache), maxMatchupCacheEntries)
	}
	if len(tracker.failBackoff) > maxFailBackoffEntries {
		t.Errorf("failBackoff size %d exceeded cap %d under concurrent load", len(tracker.failBackoff), maxFailBackoffEntries)
	}
}
