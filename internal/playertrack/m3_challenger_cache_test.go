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

// TestM3_BoundedCache_FloodEviction floods rankCache and matchupCache with 2,500+ entries
// and asserts that boundRankCacheLocked, boundMatchupCacheLocked, and boundFailBackoffLocked
// strictly enforce caps and FIFO eviction without panics or leaks.
func TestM3_BoundedCache_FloodEviction(t *testing.T) {
	store, err := storage.NewJSONStore(t.TempDir() + "/test_state.json")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	tracker, err := NewTracker(store, NewNoOpRankClient(), config.PlayerTrackingConfig{}, config.AuthConfig{})
	if err != nil {
		t.Fatalf("failed to create tracker: %v", err)
	}
	defer tracker.Close()

	const floodSize = 2500

	// 1. Flood rankCache with 2500 fresh entries (within 15m TTL) with strictly increasing timestamps
	now := time.Now().UTC()
	tracker.mu.Lock()
	for i := 0; i < floodSize; i++ {
		key := fmt.Sprintf("Steam|rank_player_%05d|0", i)
		tracker.rankCache[key] = cachedRankEntry{
			snapshot: PlayerRanksSnapshot{
				"13": PlayerPlaylistRank{PlaylistID: 13, MMR: float64(1000 + i)},
			},
			// Millisecond offsets ensuring all entries are unexpired and ordered 0..2499
			fetchedAt: now.Add(-time.Duration(floodSize-i) * time.Millisecond),
		}
	}

	if len(tracker.rankCache) != floodSize {
		t.Fatalf("pre-eviction size = %d, expected %d", len(tracker.rankCache), floodSize)
	}

	// Trigger bounding
	tracker.boundRankCacheLocked()

	// Assert cap enforced
	if len(tracker.rankCache) > maxRankCacheEntries {
		t.Fatalf("rankCache size %d exceeds maxRankCacheEntries %d", len(tracker.rankCache), maxRankCacheEntries)
	}
	if len(tracker.rankCache) != maxRankCacheEntries {
		t.Fatalf("rankCache size %d != maxRankCacheEntries %d", len(tracker.rankCache), maxRankCacheEntries)
	}

	// Assert FIFO: oldest 1500 entries (0..1499) must be evicted, newest 1000 (1500..2499) preserved
	for i := 0; i < floodSize-maxRankCacheEntries; i++ {
		evictedKey := fmt.Sprintf("Steam|rank_player_%05d|0", i)
		if _, exists := tracker.rankCache[evictedKey]; exists {
			t.Fatalf("expected key %s to be evicted under FIFO, but it still exists", evictedKey)
		}
	}
	for i := floodSize - maxRankCacheEntries; i < floodSize; i++ {
		retainedKey := fmt.Sprintf("Steam|rank_player_%05d|0", i)
		if _, exists := tracker.rankCache[retainedKey]; !exists {
			t.Fatalf("expected key %s to be retained, but it was evicted", retainedKey)
		}
	}
	tracker.mu.Unlock()

	// 2. Flood matchupCache with 2500 entries
	tracker.mu.Lock()
	for i := 0; i < floodSize; i++ {
		key := fmt.Sprintf("Steam|matchup_player_%05d|0:13", i)
		tracker.matchupCache[key] = &storage.PlayerMatchup{
			PlayerID:   fmt.Sprintf("Steam|matchup_player_%05d|0", i),
			PlaylistID: 13,
		}
	}
	if len(tracker.matchupCache) != floodSize {
		t.Fatalf("pre-eviction matchup size = %d, expected %d", len(tracker.matchupCache), floodSize)
	}

	tracker.boundMatchupCacheLocked()

	if len(tracker.matchupCache) > maxMatchupCacheEntries {
		t.Fatalf("matchupCache size %d exceeds maxMatchupCacheEntries %d", len(tracker.matchupCache), maxMatchupCacheEntries)
	}
	if len(tracker.matchupCache) != maxMatchupCacheEntries {
		t.Fatalf("matchupCache size %d != maxMatchupCacheEntries %d", len(tracker.matchupCache), maxMatchupCacheEntries)
	}
	tracker.mu.Unlock()

	// 3. Flood failBackoff with 1000 fresh entries (within 60s TTL)
	const failFlood = 1000
	nowFail := time.Now().UTC()
	tracker.mu.Lock()
	for i := 0; i < failFlood; i++ {
		key := fmt.Sprintf("Steam|fail_player_%05d|0", i)
		tracker.failBackoff[key] = nowFail.Add(-time.Duration(failFlood-i) * time.Millisecond)
	}
	if len(tracker.failBackoff) != failFlood {
		t.Fatalf("pre-eviction failBackoff size = %d, expected %d", len(tracker.failBackoff), failFlood)
	}

	tracker.boundFailBackoffLocked()

	if len(tracker.failBackoff) > maxFailBackoffEntries {
		t.Fatalf("failBackoff size %d exceeds maxFailBackoffEntries %d", len(tracker.failBackoff), maxFailBackoffEntries)
	}
	if len(tracker.failBackoff) != maxFailBackoffEntries {
		t.Fatalf("failBackoff size %d != maxFailBackoffEntries %d", len(tracker.failBackoff), maxFailBackoffEntries)
	}

	// Assert FIFO on failBackoff: oldest 500 evicted (0..499), newest 500 retained (500..999)
	for i := 0; i < failFlood-maxFailBackoffEntries; i++ {
		evictedKey := fmt.Sprintf("Steam|fail_player_%05d|0", i)
		if _, exists := tracker.failBackoff[evictedKey]; exists {
			t.Fatalf("expected failBackoff key %s to be evicted under FIFO", evictedKey)
		}
	}
	for i := failFlood - maxFailBackoffEntries; i < failFlood; i++ {
		retainedKey := fmt.Sprintf("Steam|fail_player_%05d|0", i)
		if _, exists := tracker.failBackoff[retainedKey]; !exists {
			t.Fatalf("expected failBackoff key %s to be retained", retainedKey)
		}
	}
	tracker.mu.Unlock()

	// 4. Test TTL eviction: insert expired entries (older than 15m) and verify they are purged
	tracker.mu.Lock()
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("Steam|expired_%03d|0", i)
		tracker.rankCache[key] = cachedRankEntry{
			snapshot:  PlayerRanksSnapshot{},
			fetchedAt: now.Add(-30 * time.Minute), // expired
		}
	}
	tracker.boundRankCacheLocked()
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("Steam|expired_%03d|0", i)
		if _, exists := tracker.rankCache[key]; exists {
			t.Fatalf("expected expired entry %s to be purged by TTL", key)
		}
	}
	tracker.mu.Unlock()
}

// TestM3_BoundedCache_ConcurrentAccess tests concurrent insertion, eviction, and
// InvalidateRankCache calls across 30 goroutines under the race detector.
func TestM3_BoundedCache_ConcurrentAccess(t *testing.T) {
	store, err := storage.NewJSONStore(t.TempDir() + "/test_state.json")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	tracker, err := NewTracker(store, NewNoOpRankClient(), config.PlayerTrackingConfig{}, config.AuthConfig{})
	if err != nil {
		t.Fatalf("failed to create tracker: %v", err)
	}
	defer tracker.Close()

	const numWorkers = 30
	const iterationsPerWorker = 100

	var wg sync.WaitGroup
	startCh := make(chan struct{})

	// 10 workers inserting into rankCache & triggering boundRankCacheLocked
	for w := 0; w < 10; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			<-startCh
			for i := 0; i < iterationsPerWorker; i++ {
				key := fmt.Sprintf("Steam|worker_%d_player_%d|0", workerID, i)
				tracker.mu.Lock()
				tracker.rankCache[key] = cachedRankEntry{
					snapshot:  PlayerRanksSnapshot{},
					fetchedAt: time.Now().UTC(),
				}
				tracker.boundRankCacheLocked()
				tracker.mu.Unlock()
			}
		}(w)
	}

	// 10 workers calling InvalidateRankCache
	for w := 0; w < 10; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			<-startCh
			for i := 0; i < iterationsPerWorker; i++ {
				if i%20 == 0 {
					// Full flush
					tracker.InvalidateRankCache("")
				} else {
					// Specific target
					target := fmt.Sprintf("Steam|worker_%d_player_%d|0", rand.Intn(10), rand.Intn(iterationsPerWorker))
					tracker.InvalidateRankCache(target)
				}
			}
		}(w)
	}

	// 10 workers inserting into matchupCache & triggering boundMatchupCacheLocked
	for w := 0; w < 10; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			<-startCh
			for i := 0; i < iterationsPerWorker; i++ {
				key := fmt.Sprintf("Steam|matchup_w%d_p%d|0:13", workerID, i)
				tracker.mu.Lock()
				tracker.matchupCache[key] = &storage.PlayerMatchup{
					PlayerID:   fmt.Sprintf("Steam|matchup_w%d_p%d|0", workerID, i),
					PlaylistID: 13,
				}
				tracker.boundMatchupCacheLocked()
				tracker.mu.Unlock()
			}
		}(w)
	}

	close(startCh)
	wg.Wait()

	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if len(tracker.rankCache) > maxRankCacheEntries {
		t.Errorf("rankCache size %d exceeded max %d", len(tracker.rankCache), maxRankCacheEntries)
	}
	if len(tracker.matchupCache) > maxMatchupCacheEntries {
		t.Errorf("matchupCache size %d exceeded max %d", len(tracker.matchupCache), maxMatchupCacheEntries)
	}
}
