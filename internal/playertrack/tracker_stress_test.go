package playertrack_test

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rlapi"
)

// ============================================================================
// Stress Test Doubles
// ============================================================================

type stressSkillFetcher struct {
	mu           sync.Mutex
	totalCalls   atomic.Int64
	inFlight     atomic.Int64
	maxInFlight  atomic.Int64
	delay        time.Duration
	queriedPids  map[string]int
	closed       bool
	callNotifyCh chan struct{}
}

func newStressSkillFetcher(delay time.Duration) *stressSkillFetcher {
	return &stressSkillFetcher{
		delay:        delay,
		queriedPids:  make(map[string]int),
		callNotifyCh: make(chan struct{}, 1000),
	}
}

func (s *stressSkillFetcher) GetPlayersSkills(ctx context.Context, playerIDs []rlapi.PlayerID) ([]rlapi.PlayerWithSkills, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cur := s.inFlight.Add(1)
	defer s.inFlight.Add(-1)

	// Track max concurrent in-flight RPCs
	for {
		oldMax := s.maxInFlight.Load()
		if cur <= oldMax || s.maxInFlight.CompareAndSwap(oldMax, cur) {
			break
		}
	}

	s.totalCalls.Add(1)

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, playertrack.ErrRankClientClosed
	}
	for _, pid := range playerIDs {
		s.queriedPids[string(pid)]++
	}
	s.mu.Unlock()

	select {
	case s.callNotifyCh <- struct{}{}:
	default:
	}

	if s.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(s.delay):
		}
	}

	var results []rlapi.PlayerWithSkills
	for _, pid := range playerIDs {
		results = append(results, rlapi.PlayerWithSkills{
			PlayerID: pid,
			Skills: []rlapi.Skill{
				{Playlist: 11, Tier: 16, Division: 3, MMR: 1150.0},
				{Playlist: 13, Tier: 15, Division: 2, MMR: 1080.0},
			},
		})
	}
	return results, nil
}

func (s *stressSkillFetcher) IsEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed
}

func (s *stressSkillFetcher) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// ============================================================================
// Empirical Challenge 1: Burst 120Hz/240Hz Telemetry & Concurrent JSON Readers
// ============================================================================

func TestTracker_Stress_BurstTelemetry_120Hz240Hz_ConcurrentReaders(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := newSQLiteTestStore(t)
	fetcher := newStressSkillFetcher(15 * time.Millisecond) // Realistic PsyNet latency

	cfg := config.PlayerTrackingConfig{
		Enabled:        true,
		LocalPlayerID:  "Steam|local|0",
		AutoFetchRanks: true,
	}
	tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
	if err != nil {
		t.Fatalf("NewTracker failed: %v", err)
	}
	defer func() { _ = tracker.Close() }()

	const totalEvents = 10000
	const numWriters = 4
	const eventsPerWriter = totalEvents / numWriters
	const numReaders = 12

	var wg sync.WaitGroup
	var completedWrites atomic.Int64
	var completedReads atomic.Int64
	var jsonCorruptions atomic.Int64

	startBarrier := make(chan struct{})

	// 12 Concurrent Readers continuously fetching and JSON-serializing
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			<-startBarrier

			for {
				if completedWrites.Load() >= totalEvents {
					break
				}

				snap := tracker.GetCurrentMatch()
				if snap == nil {
					jsonCorruptions.Add(1)
					continue
				}

				data, err := json.Marshal(snap)
				if err != nil {
					jsonCorruptions.Add(1)
					t.Errorf("reader %d failed to marshal CurrentMatchResponse: %v", readerID, err)
					continue
				}

				// Deep integrity check: Unmarshal back and verify no corrupted fields
				var verified playertrack.CurrentMatchResponse
				if err := json.Unmarshal(data, &verified); err != nil {
					jsonCorruptions.Add(1)
					t.Errorf("reader %d unmarshal failed (data corruption): %v", readerID, err)
					continue
				}

				if verified.MatchGUID != "" && verified.MatchGUID != "burst-telemetry-guid" {
					jsonCorruptions.Add(1)
					t.Errorf("reader %d got corrupted MatchGUID: %q", readerID, verified.MatchGUID)
				}

				completedReads.Add(1)
			}
		}(r)
	}

	// 4 Concurrent Writers blasting 10,000+ OnUpdateState events
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			<-startBarrier

			for i := 0; i < eventsPerWriter; i++ {
				score := (writerID * 100000) + i
				players := []statsapi.StatsPlayer{
					{Name: "LocalUser", PrimaryId: "Steam|local|0", TeamNum: 0, Score: score, Goals: i % 5},
					{Name: "Teammate1", PrimaryId: "Steam|tm1|0", TeamNum: 0, Score: score / 2, Assists: i % 3},
					{Name: "Teammate2", PrimaryId: "Steam|tm2|0", TeamNum: 0, Score: score / 3, Saves: i % 4},
					{Name: "Opponent1", PrimaryId: "Epic|opp1|0", TeamNum: 1, Score: score / 4, Shots: i % 6},
					{Name: "Opponent2", PrimaryId: "Epic|opp2|0", TeamNum: 1, Score: score / 5, Demos: i % 2},
					{Name: "Opponent3", PrimaryId: "Epic|opp3|0", TeamNum: 1, Score: score / 6},
				}

				if err := tracker.OnUpdateState(ctx, "burst-telemetry-guid", 11, players); err != nil {
					t.Errorf("writer %d OnUpdateState failed: %v", writerID, err)
					return
				}
				completedWrites.Add(1)
			}
		}(w)
	}

	// Release all workers simultaneously
	close(startBarrier)
	wg.Wait()

	if completedWrites.Load() != totalEvents {
		t.Errorf("expected %d completed writes; got %d", totalEvents, completedWrites.Load())
	}
	if completedReads.Load() < 100 {
		t.Errorf("expected at least 100 concurrent reads during burst; got %d", completedReads.Load())
	}
	if jsonCorruptions.Load() != 0 {
		t.Fatalf("encountered %d JSON corruptions or nil snapshots under burst load!", jsonCorruptions.Load())
	}

	// Wait for any in-flight rank query to finish
	time.Sleep(100 * time.Millisecond)

	// Verify exact single-flight RPC rank retrieval:
	// All 5 non-local players were part of the initial lobby.
	// Debounce and inFlight dedup must ensure exactly 1 batch RPC was dispatched!
	totalRPCCalls := fetcher.totalCalls.Load()
	if totalRPCCalls != 1 {
		t.Errorf("EXACT SINGLE-FLIGHT VIOLATION: expected exactly 1 batch RPC call for lobby, got %d calls!", totalRPCCalls)
	}

	// Verify all 5 non-local players were included in the single-flight query
	fetcher.mu.Lock()
	for _, pid := range []string{"Steam|tm1|0", "Steam|tm2|0", "Epic|opp1|0", "Epic|opp2|0", "Epic|opp3|0"} {
		if fetcher.queriedPids[pid] != 1 {
			t.Errorf("player %s queried %d times, expected exactly 1", pid, fetcher.queriedPids[pid])
		}
	}
	fetcher.mu.Unlock()

	// Verify that the snapshot correctly reflects populated ranks
	finalSnap := tracker.GetCurrentMatch()
	if len(finalSnap.Teammates) != 2 {
		t.Fatalf("expected 2 teammates, got %d", len(finalSnap.Teammates))
	}
	if len(finalSnap.Opponents) != 3 {
		t.Fatalf("expected 3 opponents, got %d", len(finalSnap.Opponents))
	}
	for _, tm := range finalSnap.Teammates {
		if tm.CurrentRank == nil {
			t.Errorf("teammate %s missing CurrentRank after fetch", tm.PlayerID)
		} else if tm.CurrentRank.RankName != "Champion I Division IV" {
			t.Errorf("teammate %s rank mismatch: %s", tm.PlayerID, tm.CurrentRank.RankName)
		}
	}
	for _, opp := range finalSnap.Opponents {
		if opp.CurrentRank == nil {
			t.Errorf("opponent %s missing CurrentRank after fetch", opp.PlayerID)
		} else if opp.CurrentRank.RankName != "Champion I Division IV" {
			t.Errorf("opponent %s rank mismatch: %s", opp.PlayerID, opp.CurrentRank.RankName)
		}
	}
}

// ============================================================================
// Empirical Challenge 2: Deep Clone Mutation Challenge
// ============================================================================
