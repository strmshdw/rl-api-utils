package playertrack_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
)

func TestTracker_Idempotency_DuplicateMatchEnded(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|local|0",
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		players := []statsapi.StatsPlayer{
			makePlayer("Local", "Steam|local|0", 0),
			makePlayer("Teammate", "Steam|tm|0", 0),
		}
		_ = tracker.OnUpdateState(ctx, "m-idem", 11, players)

		win := 0
		// Call 1
		if err := tracker.OnMatchEnded(ctx, "m-idem", &win); err != nil {
			t.Fatalf("first OnMatchEnded failed: %v", err)
		}

		// Call 2 (Duplicate)
		if err := tracker.OnMatchEnded(ctx, "m-idem", &win); err != nil {
			t.Fatalf("duplicate OnMatchEnded must return nil, got: %v", err)
		}

		// Counters must be exactly 1, not 2
		m, err := store.GetPlayerMatchup(ctx, "Steam|tm|0", 11)
		if err != nil {
			t.Fatalf("GetPlayerMatchup failed: %v", err)
		}
		if m.WinsAsTeammate != 1 {
			t.Errorf("expected 1 win_as_teammate; got %d (double counting occurred!)", m.WinsAsTeammate)
		}
	})
}

func TestTracker_NilWinnerTeamNum_NoOutcomesRecorded(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|local|0",
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		players := []statsapi.StatsPlayer{
			makePlayer("Local", "Steam|local|0", 0),
			makePlayer("Teammate", "Steam|tm|0", 0),
		}
		_ = tracker.OnUpdateState(ctx, "m-nil", 11, players)

		if err := tracker.OnMatchEnded(ctx, "m-nil", nil); err != nil {
			t.Fatalf("OnMatchEnded(nil) returned error: %v", err)
		}

		m, _ := store.GetPlayerMatchup(ctx, "Steam|tm|0", 11)
		if m.TotalMatches != 0 {
			t.Errorf("expected 0 total matches for nil winner; got %d", m.TotalMatches)
		}
	})
}

func TestTracker_EdgeCases_EmptyGUIDAndPlayers(t *testing.T) {
	store := newSQLiteTestStore(t)
	fetcher := newTestSkillFetcher()
	cfg := config.PlayerTrackingConfig{Enabled: true}
	tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
	if err != nil {
		t.Fatalf("NewTracker failed: %v", err)
	}
	defer func() { _ = tracker.Close() }()

	ctx := context.Background()

	// Empty GUID
	if err := tracker.OnUpdateState(ctx, "   ", 11, []statsapi.StatsPlayer{makePlayer("P1", "Steam|1|0", 0)}); err != nil {
		t.Errorf("expected nil for empty matchGUID; got %v", err)
	}
	win := 0
	if err := tracker.OnMatchEnded(ctx, "", &win); err != nil {
		t.Errorf("expected nil for empty matchGUID on MatchEnded; got %v", err)
	}

	// Empty player slice
	if err := tracker.OnUpdateState(ctx, "valid-guid", 11, nil); err != nil {
		t.Errorf("expected nil for empty players slice; got %v", err)
	}
}

func TestTracker_StoreFaultInjection(t *testing.T) {
	baseStore := newSQLiteTestStore(t)
	faultStore := &mockFaultStore{
		StateStore:       baseStore,
		recordResultsErr: errors.New("simulated database write failure"),
	}

	fetcher := newTestSkillFetcher()
	cfg := config.PlayerTrackingConfig{
		Enabled:       true,
		LocalPlayerID: "Steam|local|0",
	}
	tracker, err := playertrack.NewTracker(faultStore, fetcher, cfg, config.AuthConfig{})
	if err != nil {
		t.Fatalf("NewTracker failed: %v", err)
	}
	defer func() { _ = tracker.Close() }()

	ctx := context.Background()
	players := []statsapi.StatsPlayer{
		makePlayer("Local", "Steam|local|0", 0),
		makePlayer("Opp", "Steam|opp|0", 1),
	}
	_ = tracker.OnUpdateState(ctx, "m-fault", 11, players)

	win := 0
	err = tracker.OnMatchEnded(ctx, "m-fault", &win)
	if err == nil {
		t.Errorf("expected error on storage failure; got nil")
	}
	if !errors.Is(err, faultStore.recordResultsErr) {
		t.Errorf("expected %v, got %v", faultStore.recordResultsErr, err)
	}
}

// ============================================================================
// Test Suite 6: Concurrency & Race Detector Stress Testing
// ============================================================================

func TestTracker_RaceStress_120HzUpdates_And_Reads(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := newSQLiteTestStore(t)
	fetcher := newTestSkillFetcher()
	fetcher.delay = 5 * time.Millisecond // Simulate network latency

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

	const writers = 6
	const readers = 6
	const iterations = 300

	var wg sync.WaitGroup
	var completedReads atomic.Int64
	var completedWrites atomic.Int64

	// Concurrent 120Hz writers
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				score := i + (workerID * 10)
				players := []statsapi.StatsPlayer{
					{Name: "Local", PrimaryId: "Steam|local|0", TeamNum: 0, Score: score},
					{Name: fmt.Sprintf("Teammate_%d", workerID), PrimaryId: fmt.Sprintf("Steam|tm_%d|0", workerID), TeamNum: 0, Score: score / 2},
					{Name: fmt.Sprintf("Opponent_%d", workerID), PrimaryId: fmt.Sprintf("Epic|opp_%d|0", workerID), TeamNum: 1, Score: score / 3},
				}
				_ = tracker.OnUpdateState(ctx, "race-match-guid", 11, players)
				completedWrites.Add(1)
			}
		}(w)
	}

	// Concurrent HTTP snapshot readers
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				snap := tracker.GetCurrentMatch()
				if snap != nil {
					// Verify serialization safety under race detector
					_, _ = json.Marshal(snap)
				}
				completedReads.Add(1)
			}
		}()
	}

	wg.Wait()

	if completedWrites.Load() != writers*iterations {
		t.Errorf("expected %d writes, got %d", writers*iterations, completedWrites.Load())
	}
	if completedReads.Load() != readers*iterations {
		t.Errorf("expected %d reads, got %d", readers*iterations, completedReads.Load())
	}

	// Final match end after heavy concurrency
	winner := 0
	if err := tracker.OnMatchEnded(ctx, "race-match-guid", &winner); err != nil {
		t.Fatalf("final OnMatchEnded failed: %v", err)
	}
}

func TestTracker_ConcurrentCloseAndRankFetch_NoWaitGroupMisuse(t *testing.T) {
	const iterations = 30
	for iter := 0; iter < iterations; iter++ {
		store := newSQLiteTestStore(t)
		fetcher := newTestSkillFetcher()
		fetcher.delay = 2 * time.Millisecond

		cfg := config.PlayerTrackingConfig{
			Enabled:        true,
			LocalPlayerID:  "Steam|local|0",
			AutoFetchRanks: true,
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}

		ctx := context.Background()
		var wg sync.WaitGroup

		// Goroutine 1: Rapid OnUpdateState calls generating new player rank queries
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				players := []statsapi.StatsPlayer{
					{Name: "Local", PrimaryId: "Steam|local|0", TeamNum: 0},
					{Name: fmt.Sprintf("Opp_%d_%d", iter, i), PrimaryId: fmt.Sprintf("Steam|opp_%d_%d|0", iter, i), TeamNum: 1},
				}
				_ = tracker.OnUpdateState(ctx, fmt.Sprintf("m-%d-%d", iter, i), 11, players)
				time.Sleep(200 * time.Microsecond)
			}
		}()

		// Goroutine 2: Concurrent Close()
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(1 * time.Millisecond)
			_ = tracker.Close()
		}()

		// Goroutine 3: Concurrent second Close() to verify idempotent drain
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(2 * time.Millisecond)
			_ = tracker.Close()
		}()

		wg.Wait()

		// Verify that after Close() completed, tracker rejects new updates
		players := []statsapi.StatsPlayer{
			{Name: "Local", PrimaryId: "Steam|local|0", TeamNum: 0},
		}
		err = tracker.OnUpdateState(ctx, "post-close-match", 11, players)
		if err == nil {
			t.Errorf("expected error on OnUpdateState after Close(), got nil")
		}
	}
}

// ============================================================================
// Test Suite 4: Mid-Game Disconnect & Player State Retention (Requirement R2)
// ============================================================================

// TestTracker_MidGameDisconnect_Lifecycle_TeammateAndLocal covers the primary 4-frame lifecycle:
// Frame 1: Full lobby with active players accumulating stats.
// Frame 2: Teammate leaves early -> retained in active state with stats preserved and IsDisconnected=true.
// Frame 3: Disconnected teammate reconnects -> stats updated, IsDisconnected=false, NO duplicate entries.
// Frame 4: Local player leaves early -> local player and local team preserved, stats retained.
// Conclude: Match ends -> outcomes recorded in store because local player and team were preserved.
