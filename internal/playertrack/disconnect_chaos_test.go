package playertrack_test

import (
	"context"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
)

func TestAdversarial_SustainedLocalDisconnect_Across10Frames(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|local_survivor|0",
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		matchGUID := "adv-sustained-local-drop-1"
		playlistID := 11

		// Frame 1: Local player active
		f1 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalSurvivor", "Steam|local_survivor|0", 0, 100, 1, 0, 0, 1, 0),
			makePlayerWithStats("Tm1", "Steam|tm_1|0", 0, 150, 0, 1, 1, 1, 0),
			makePlayerWithStats("Opp1", "Steam|opp_1|0", 1, 120, 0, 1, 1, 1, 0),
		}
		if err := tracker.OnUpdateState(ctx, matchGUID, playlistID, f1); err != nil {
			t.Fatalf("Frame 1 failed: %v", err)
		}

		// Local player leaves in Frame 2 and NEVER returns for 10 frames
		for frame := 2; frame <= 11; frame++ {
			fFrame := []statsapi.StatsPlayer{
				// LocalSurvivor omitted!
				makePlayerWithStats("Tm1", "Steam|tm_1|0", 0, 150+frame*20, frame/3, 1, 1, 1+frame, 0),
				makePlayerWithStats("Opp1", "Steam|opp_1|0", 1, 120+frame*15, 0, 1, 1, 1+frame, 0),
			}
			if err := tracker.OnUpdateState(ctx, matchGUID, playlistID, fFrame); err != nil {
				t.Fatalf("Frame %d failed: %v", frame, err)
			}

			snap := tracker.GetCurrentMatch()
			if snap.LocalPlayer == nil {
				t.Fatalf("Frame %d: LocalPlayer became nil!", frame)
			}
			if snap.LocalPlayer.PlayerID != "Steam|local_survivor|0" {
				t.Fatalf("Frame %d: LocalPlayer corrupted: %q", frame, snap.LocalPlayer.PlayerID)
			}
			if !snap.LocalPlayer.IsDisconnected {
				t.Fatalf("Frame %d: LocalPlayer IsDisconnected should be true", frame)
			}
			if snap.LocalPlayer.Stats.Score != 100 {
				t.Fatalf("Frame %d: LocalPlayer stats corrupted: %d", frame, snap.LocalPlayer.Stats.Score)
			}
			if snap.LocalTeam == nil || *snap.LocalTeam != 0 {
				t.Fatalf("Frame %d: LocalTeam corrupted: %v", frame, snap.LocalTeam)
			}
		}

		// Conclude match: Local team won (team 0)
		winnerBlue := 0
		if err := tracker.OnMatchEnded(ctx, matchGUID, &winnerBlue); err != nil {
			t.Fatalf("OnMatchEnded failed: %v", err)
		}

		endedSnap := tracker.GetCurrentMatch()
		if endedSnap.Result != "victory" {
			t.Fatalf("Expected Result='victory', got %q", endedSnap.Result)
		}

		// Verify matchup store recorded outcomes properly despite 10-frame disconnect
		tmRec, err := store.GetPlayerMatchup(ctx, "Steam|tm_1|0", playlistID)
		if err != nil || tmRec == nil || tmRec.WinsAsTeammate != 1 {
			t.Fatalf("Expected teammate recorded 1 win, got %+v (err=%v)", tmRec, err)
		}
		oppRec, err := store.GetPlayerMatchup(ctx, "Steam|opp_1|0", playlistID)
		if err != nil || oppRec == nil || oppRec.WinsAsOpponent != 1 {
			t.Fatalf("Expected opponent recorded 1 win as opponent, got %+v (err=%v)", oppRec, err)
		}
	})
}

// ============================================================================
// Adversarial Disconnect Suite 5: Opponent Full Rage-Quit / Forfeit
// ============================================================================

func TestAdversarial_FullOpponentRageQuit_AllRetained(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|local_player|0",
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		matchGUID := "adv-ragequit-1"
		playlistID := 13 // 3v3 Standard

		// Frame 1: 3v3 full lobby (6 players)
		f1 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalPlayer", "Steam|local_player|0", 0, 300, 2, 0, 1, 3, 0),
			makePlayerWithStats("Tm1", "Steam|tm_1|0", 0, 250, 1, 1, 0, 2, 1),
			makePlayerWithStats("Tm2", "Steam|tm_2|0", 0, 200, 0, 2, 1, 1, 0),
			makePlayerWithStats("RageOpp1", "Steam|rage_opp_1|0", 1, 80, 0, 0, 1, 1, 0),
			makePlayerWithStats("RageOpp2", "Steam|rage_opp_2|0", 1, 60, 0, 0, 0, 1, 0),
			makePlayerWithStats("RageOpp3", "Steam|rage_opp_3|0", 1, 40, 0, 0, 1, 0, 0),
		}
		_ = tracker.OnUpdateState(ctx, matchGUID, playlistID, f1)

		// Frame 2: ALL 3 opponents forfeit and disconnect simultaneously!
		f2 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalPlayer", "Steam|local_player|0", 0, 300, 2, 0, 1, 3, 0),
			makePlayerWithStats("Tm1", "Steam|tm_1|0", 0, 250, 1, 1, 0, 2, 1),
			makePlayerWithStats("Tm2", "Steam|tm_2|0", 0, 200, 0, 2, 1, 1, 0),
			// All 3 opponents gone!
		}
		_ = tracker.OnUpdateState(ctx, matchGUID, playlistID, f2)

		snapF2 := tracker.GetCurrentMatch()
		if len(snapF2.Opponents) != 3 {
			t.Fatalf("Frame 2: expected all 3 opponents retained on forfeit, got %d", len(snapF2.Opponents))
		}
		for _, opp := range snapF2.Opponents {
			if !opp.IsDisconnected {
				t.Fatalf("Opponent %s should be marked IsDisconnected=true", opp.PlayerID)
			}
		}

		// Conclude match with victory for Blue
		winnerBlue := 0
		_ = tracker.OnMatchEnded(ctx, matchGUID, &winnerBlue)

		// All 3 rage-quit opponents MUST be recorded in storage as losses for them (wins for local player)
		for _, oppID := range []string{"Steam|rage_opp_1|0", "Steam|rage_opp_2|0", "Steam|rage_opp_3|0"} {
			rec, err := store.GetPlayerMatchup(ctx, oppID, playlistID)
			if err != nil || rec == nil || rec.WinsAsOpponent != 1 {
				t.Fatalf("Expected opponent %s recorded with 1 win as opponent, got %+v (err=%v)", oppID, rec, err)
			}
		}
	})
}

// ============================================================================
// Adversarial Disconnect Suite 6: Chaos Fuzz & Race Stress
// ============================================================================

func TestAdversarial_ChaosFuzz_DropsAndReconnects(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|local_chaos|0",
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		matchGUID := "adv-chaos-fuzz-1"
		playlistID := 13
		rng := rand.New(rand.NewSource(1337))

		allPlayerPool := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalChaos", "Steam|local_chaos|0", 0, 100, 0, 0, 0, 0, 0),
			makePlayerWithStats("TeammateA", "Steam|tm_a|0", 0, 100, 0, 0, 0, 0, 0),
			makePlayerWithStats("TeammateB", "Steam|tm_b|0", 0, 100, 0, 0, 0, 0, 0),
			makePlayerWithStats("OpponentX", "Steam|opp_x|0", 1, 100, 0, 0, 0, 0, 0),
			makePlayerWithStats("OpponentY", "Steam|opp_y|0", 1, 100, 0, 0, 0, 0, 0),
			makePlayerWithStats("OpponentZ", "Steam|opp_z|0", 1, 100, 0, 0, 0, 0, 0),
		}

		// Initial frame establishes lobby
		_ = tracker.OnUpdateState(ctx, matchGUID, playlistID, allPlayerPool)

		// 30 iterations of random presence and stat progression
		for iter := 0; iter < 30; iter++ {
			var framePlayers []statsapi.StatsPlayer
			for _, p := range allPlayerPool {
				// 70% chance player is present this frame
				if rng.Float64() < 0.70 {
					p.Score += rng.Intn(50)
					p.Goals += rng.Intn(2)
					framePlayers = append(framePlayers, p)
				}
			}

			// Ensure at least 1 player in frame so OnUpdateState processes
			if len(framePlayers) == 0 {
				framePlayers = append(framePlayers, allPlayerPool[0])
			}

			if err := tracker.OnUpdateState(ctx, matchGUID, playlistID, framePlayers); err != nil {
				t.Fatalf("Chaos iter %d failed: %v", iter, err)
			}

			snap := tracker.GetCurrentMatch()

			// INVARIANT 1: Total teammates + opponents + local player must never exceed original pool (no duplicates!)
			seenIDs := make(map[string]bool)
			if snap.LocalPlayer != nil {
				seenIDs[snap.LocalPlayer.PlayerID] = true
			}
			for _, tm := range snap.Teammates {
				if seenIDs[tm.PlayerID] {
					t.Fatalf("Chaos iter %d: duplicate teammate %s detected!", iter, tm.PlayerID)
				}
				seenIDs[tm.PlayerID] = true
			}
			for _, opp := range snap.Opponents {
				if seenIDs[opp.PlayerID] {
					t.Fatalf("Chaos iter %d: duplicate opponent %s detected!", iter, opp.PlayerID)
				}
				seenIDs[opp.PlayerID] = true
			}

			// INVARIANT 2: Count of human players must equal 6 (all players retained or active)
			if len(seenIDs) != 6 {
				t.Fatalf("Chaos iter %d: expected all 6 players accounted for, got %d", iter, len(seenIDs))
			}
		}
	})
}

func TestAdversarial_ConcurrentUpdateAndReadStress(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|local_stress|0",
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		matchGUID := "adv-concurrent-stress-1"
		playlistID := 11

		var wg sync.WaitGroup
		stopCh := make(chan struct{})

		// Goroutine 1: Rapid Updates alternating drops and reconnects
		wg.Add(1)
		go func() {
			defer wg.Done()
			rng := rand.New(rand.NewSource(42))
			for i := 0; ; i++ {
				select {
				case <-stopCh:
					return
				default:
				}
				var players []statsapi.StatsPlayer
				players = append(players, makePlayerWithStats("LocalStress", "Steam|local_stress|0", 0, 100+i, 1, 0, 0, 1, 0))
				if rng.Intn(2) == 0 {
					players = append(players, makePlayerWithStats("Tm1", "Steam|tm_1|0", 0, 150+i, 0, 1, 1, 1, 0))
				}
				if rng.Intn(2) == 0 {
					players = append(players, makePlayerWithStats("Opp1", "Steam|opp_1|0", 1, 120+i, 0, 1, 1, 1, 0))
				}
				_ = tracker.OnUpdateState(ctx, matchGUID, playlistID, players)
				time.Sleep(time.Millisecond)
			}
		}()

		// Goroutines 2-4: Concurrent readers calling GetCurrentMatch
		for r := 0; r < 3; r++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stopCh:
						return
					default:
					}
					snap := tracker.GetCurrentMatch()
					if snap != nil && snap.LocalPlayer != nil {
						_ = snap.LocalPlayer.IsDisconnected
					}
					time.Sleep(500 * time.Microsecond)
				}
			}()
		}

		time.Sleep(200 * time.Millisecond)
		close(stopCh)
		wg.Wait()
	})
}
