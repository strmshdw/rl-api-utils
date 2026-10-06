package playertrack_test

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
)

// TestChallenger_MultiMatch_DisconnectedPlayersNeverLeak empirically verifies that
// when match 1 concludes with disconnected participants across all roles (teammates,
// opponents, spectators), starting match 2 with a new GUID guarantees that NO players
// from match 1 are retained into match 2.
func TestChallenger_MultiMatch_DisconnectedPlayersNeverLeak(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()
		tracker, err := playertrack.NewTracker(
			store,
			playertrack.NewNoOpRankClient(),
			config.PlayerTrackingConfig{
				Enabled:       true,
				LocalPlayerID: "Steam|local_hero|0",
			},
			config.AuthConfig{},
		)
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer tracker.Close()

		guid1 := "match-iso-guid-1"
		playlistID := 11

		// Frame 1: Match 1 full lobby (3v3 + 1 spectator)
		playersM1F1 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalHero", "Steam|local_hero|0", 0, 150, 1, 0, 1, 2, 0),
			makePlayerWithStats("Tm1", "Steam|tm_1|0", 0, 200, 1, 1, 0, 3, 1),
			makePlayerWithStats("Tm2", "Steam|tm_2|0", 0, 180, 0, 1, 2, 1, 0),
			makePlayerWithStats("Opp1", "Steam|opp_1|0", 1, 220, 1, 0, 1, 2, 0),
			makePlayerWithStats("Opp2", "Steam|opp_2|0", 1, 190, 0, 1, 0, 2, 1),
			makePlayerWithStats("Opp3", "Steam|opp_3|0", 1, 140, 0, 0, 1, 1, 0),
			makePlayerWithStats("Caster", "Steam|spec_1|0", 255, 0, 0, 0, 0, 0, 0),
		}
		if err := tracker.OnUpdateState(ctx, guid1, playlistID, playersM1F1); err != nil {
			t.Fatalf("M1 F1 OnUpdateState failed: %v", err)
		}

		// Frame 2: Tm1, Opp2, and Caster disconnect mid-game
		playersM1F2 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalHero", "Steam|local_hero|0", 0, 180, 1, 0, 2, 3, 0),
			makePlayerWithStats("Tm2", "Steam|tm_2|0", 0, 210, 1, 1, 2, 2, 0),
			makePlayerWithStats("Opp1", "Steam|opp_1|0", 1, 250, 1, 0, 2, 3, 0),
			makePlayerWithStats("Opp3", "Steam|opp_3|0", 1, 160, 0, 1, 1, 2, 0),
		}
		if err := tracker.OnUpdateState(ctx, guid1, playlistID, playersM1F2); err != nil {
			t.Fatalf("M1 F2 OnUpdateState failed: %v", err)
		}

		snapM1 := tracker.GetCurrentMatch()
		if snapM1 == nil || !snapM1.ActiveMatch {
			t.Fatal("expected active match snapshot for match 1")
		}
		if len(snapM1.Teammates) != 2 || len(snapM1.Opponents) != 3 || len(snapM1.Spectators) != 1 {
			t.Fatalf("M1 F2: expected 2 teammates, 3 opponents, 1 spectator; got %d, %d, %d",
				len(snapM1.Teammates), len(snapM1.Opponents), len(snapM1.Spectators))
		}

		// Conclude Match 1 (Blue victory)
		winner := 0
		if err := tracker.OnMatchEnded(ctx, guid1, &winner); err != nil {
			t.Fatalf("OnMatchEnded failed: %v", err)
		}

		// Match 2: Start new match with completely different players
		guid2 := "match-iso-guid-2"
		playlist2 := 13 // 3v3 Standard
		playersM2F1 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalHero", "Steam|local_hero|0", 0, 50, 0, 0, 1, 1, 0),
			makePlayerWithStats("BrandNewTm", "Steam|new_tm|0", 0, 60, 0, 0, 0, 1, 0),
			makePlayerWithStats("BrandNewOpp", "Steam|new_opp|0", 1, 70, 0, 0, 1, 2, 0),
		}
		if err := tracker.OnUpdateState(ctx, guid2, playlist2, playersM2F1); err != nil {
			t.Fatalf("M2 F1 OnUpdateState failed: %v", err)
		}

		snapM2 := tracker.GetCurrentMatch()
		if snapM2 == nil {
			t.Fatal("expected snapshot for match 2")
		}
		if snapM2.MatchGUID != guid2 {
			t.Errorf("expected match GUID %q, got %q", guid2, snapM2.MatchGUID)
		}
		if !snapM2.ActiveMatch || snapM2.MatchEnded {
			t.Errorf("expected Match 2 to be active and not ended")
		}
		if len(snapM2.Teammates) != 1 {
			t.Fatalf("expected exactly 1 teammate in match 2, got %d", len(snapM2.Teammates))
		}
		if snapM2.Teammates[0].PlayerID != "Steam|new_tm|0" {
			t.Errorf("expected teammate 'Steam|new_tm|0', got %q", snapM2.Teammates[0].PlayerID)
		}
		if snapM2.Teammates[0].IsDisconnected {
			t.Errorf("new teammate should have IsDisconnected=false")
		}
		if len(snapM2.Opponents) != 1 {
			t.Fatalf("expected exactly 1 opponent in match 2, got %d", len(snapM2.Opponents))
		}
		if snapM2.Opponents[0].PlayerID != "Steam|new_opp|0" {
			t.Errorf("expected opponent 'Steam|new_opp|0', got %q", snapM2.Opponents[0].PlayerID)
		}
		if len(snapM2.Spectators) != 0 {
			t.Errorf("expected 0 spectators in match 2, got %d", len(snapM2.Spectators))
		}

		// Thorough audit: verify NONE of the 6 players from Match 1 exist anywhere in Match 2
		m1ForbiddenIDs := []string{
			"Steam|tm_1|0", "Steam|tm_2|0",
			"Steam|opp_1|0", "Steam|opp_2|0", "Steam|opp_3|0",
			"Steam|spec_1|0",
		}
		allM2Players := append(append(snapM2.Teammates, snapM2.Opponents...), snapM2.Spectators...)
		for _, p := range allM2Players {
			for _, forbidden := range m1ForbiddenIDs {
				if p.PlayerID == forbidden {
					t.Fatalf("SECURITY VIOLATION: player %s from Match 1 leaked into Match 2!", forbidden)
				}
			}
		}
	})
}

// TestChallenger_MultiMatch_AbruptTransitionWithoutMatchEnded ensures that if
// a match transitions directly to a new match GUID without an intervening
// MatchEnded event, old disconnected participants are immediately purged.
func TestChallenger_MultiMatch_AbruptTransitionWithoutMatchEnded(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()
		tracker, err := playertrack.NewTracker(
			store,
			playertrack.NewNoOpRankClient(),
			config.PlayerTrackingConfig{
				Enabled:       true,
				LocalPlayerID: "Steam|local_hero|0",
			},
			config.AuthConfig{},
		)
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer tracker.Close()

		// Match 1: Player drops
		playersM1 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalHero", "Steam|local_hero|0", 0, 100, 1, 0, 0, 1, 0),
			makePlayerWithStats("DropTm", "Steam|drop_tm|0", 0, 150, 1, 0, 1, 2, 0),
		}
		_ = tracker.OnUpdateState(ctx, "abrupt-guid-1", 11, playersM1)

		playersM1Drop := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalHero", "Steam|local_hero|0", 0, 120, 1, 0, 0, 2, 0),
		}
		_ = tracker.OnUpdateState(ctx, "abrupt-guid-1", 11, playersM1Drop)

		snapM1 := tracker.GetCurrentMatch()
		if len(snapM1.Teammates) != 1 || !snapM1.Teammates[0].IsDisconnected {
			t.Fatalf("expected DropTm retained as disconnected in match 1")
		}

		// ABRUPT TRANSITION: New match GUID arrives directly without OnMatchEnded!
		playersM2 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalHero", "Steam|local_hero|0", 0, 10, 0, 0, 0, 0, 0),
			makePlayerWithStats("FreshTm", "Steam|fresh_tm|0", 0, 20, 0, 0, 0, 0, 0),
		}
		if err := tracker.OnUpdateState(ctx, "abrupt-guid-2", 11, playersM2); err != nil {
			t.Fatalf("OnUpdateState failed on abrupt transition: %v", err)
		}

		snapM2 := tracker.GetCurrentMatch()
		if snapM2.MatchGUID != "abrupt-guid-2" {
			t.Fatalf("expected MatchGUID 'abrupt-guid-2', got %q", snapM2.MatchGUID)
		}
		if len(snapM2.Teammates) != 1 {
			t.Fatalf("expected 1 teammate in match 2, got %d", len(snapM2.Teammates))
		}
		if snapM2.Teammates[0].PlayerID != "Steam|fresh_tm|0" {
			t.Errorf("expected FreshTm, got %q", snapM2.Teammates[0].PlayerID)
		}
		if snapM2.Teammates[0].IsDisconnected {
			t.Errorf("fresh teammate should not be marked disconnected")
		}
	})
}

// TestChallenger_MultiMatch_DisconnectedPlayerRejoinsNextMatchAsOpponent verifies
// that if a player who disconnected in match 1 queues into match 2 as an opponent,
// their state is cleanly reset: classified as Opponent, IsDisconnected=false, and
// new stats assigned.
func TestChallenger_MultiMatch_DisconnectedPlayerRejoinsNextMatchAsOpponent(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()
		tracker, err := playertrack.NewTracker(
			store,
			playertrack.NewNoOpRankClient(),
			config.PlayerTrackingConfig{
				Enabled:       true,
				LocalPlayerID: "Steam|local_hero|0",
			},
			config.AuthConfig{},
		)
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer tracker.Close()

		// Match 1: PlayerX is teammate on Team 0
		playersM1F1 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalHero", "Steam|local_hero|0", 0, 100, 0, 0, 1, 1, 0),
			makePlayerWithStats("PlayerX", "Steam|player_x|0", 0, 200, 1, 1, 0, 2, 0),
		}
		_ = tracker.OnUpdateState(ctx, "guid-swap-1", 11, playersM1F1)

		// Frame 2: PlayerX disconnects
		playersM1F2 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalHero", "Steam|local_hero|0", 0, 120, 0, 0, 1, 2, 0),
		}
		_ = tracker.OnUpdateState(ctx, "guid-swap-1", 11, playersM1F2)

		snapM1 := tracker.GetCurrentMatch()
		if len(snapM1.Teammates) != 1 || !snapM1.Teammates[0].IsDisconnected {
			t.Fatalf("expected PlayerX retained as disconnected teammate in match 1")
		}

		// Conclude match 1
		win := 0
		_ = tracker.OnMatchEnded(ctx, "guid-swap-1", &win)

		// Match 2: PlayerX joins as an OPPONENT on Team 1!
		playersM2 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalHero", "Steam|local_hero|0", 0, 30, 0, 0, 0, 1, 0),
			makePlayerWithStats("PlayerX", "Steam|player_x|0", 1, 90, 1, 0, 0, 1, 0),
		}
		if err := tracker.OnUpdateState(ctx, "guid-swap-2", 11, playersM2); err != nil {
			t.Fatalf("OnUpdateState failed: %v", err)
		}

		snapM2 := tracker.GetCurrentMatch()
		if len(snapM2.Teammates) != 0 {
			t.Fatalf("expected 0 teammates in match 2, got %d", len(snapM2.Teammates))
		}
		if len(snapM2.Opponents) != 1 {
			t.Fatalf("expected 1 opponent in match 2, got %d", len(snapM2.Opponents))
		}
		opp := snapM2.Opponents[0]
		if opp.PlayerID != "Steam|player_x|0" {
			t.Errorf("expected PlayerX in opponents, got %q", opp.PlayerID)
		}
		if opp.IsDisconnected {
			t.Errorf("PlayerX should NOT be marked disconnected in match 2")
		}
		if opp.Stats.Score != 90 || opp.Stats.Goals != 1 {
			t.Errorf("PlayerX should have fresh match 2 stats, got %+v", opp.Stats)
		}
	})
}

// TestChallenger_MultiMatch_RapidSequencingChaos tests 15 consecutive matches
// with varied churn, verifying zero leaks and state integrity at each step.
func TestChallenger_MultiMatch_RapidSequencingChaos(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()
		tracker, err := playertrack.NewTracker(
			store,
			playertrack.NewNoOpRankClient(),
			config.PlayerTrackingConfig{
				Enabled:       true,
				LocalPlayerID: "Steam|local_user|0",
			},
			config.AuthConfig{},
		)
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer tracker.Close()

		rng := rand.New(rand.NewSource(42))

		for matchIdx := 0; matchIdx < 15; matchIdx++ {
			guid := fmt.Sprintf("chaos-match-%d", matchIdx)
			playlistID := 11

			// 2v2: LocalUser + Tm vs Opp1 + Opp2
			tmID := fmt.Sprintf("Steam|tm_m%d|0", matchIdx)
			opp1ID := fmt.Sprintf("Steam|opp1_m%d|0", matchIdx)
			opp2ID := fmt.Sprintf("Steam|opp2_m%d|0", matchIdx)

			// Frame 1: Full
			f1 := []statsapi.StatsPlayer{
				makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 100+matchIdx, 0, 0, 1, 1, 0),
				makePlayerWithStats("Tm", tmID, 0, 150+matchIdx, 1, 0, 0, 2, 0),
				makePlayerWithStats("Opp1", opp1ID, 1, 120+matchIdx, 0, 1, 1, 1, 0),
				makePlayerWithStats("Opp2", opp2ID, 1, 130+matchIdx, 1, 0, 0, 1, 0),
			}
			if err := tracker.OnUpdateState(ctx, guid, playlistID, f1); err != nil {
				t.Fatalf("match %d F1 failed: %v", matchIdx, err)
			}

			// Frame 2: Teammate or Opponent drops
			dropTm := rng.Intn(2) == 1
			f2 := []statsapi.StatsPlayer{
				makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 120+matchIdx, 0, 0, 1, 2, 0),
			}
			if !dropTm {
				f2 = append(f2, makePlayerWithStats("Tm", tmID, 0, 160+matchIdx, 1, 0, 0, 2, 0))
			}
			// Opp2 always drops
			f2 = append(f2, makePlayerWithStats("Opp1", opp1ID, 1, 140+matchIdx, 0, 1, 1, 2, 0))

			if err := tracker.OnUpdateState(ctx, guid, playlistID, f2); err != nil {
				t.Fatalf("match %d F2 failed: %v", matchIdx, err)
			}

			snap := tracker.GetCurrentMatch()
			if snap == nil || snap.MatchGUID != guid {
				t.Fatalf("match %d: invalid match snapshot GUID: %v", matchIdx, snap)
			}
			if len(snap.Teammates) != 1 {
				t.Fatalf("match %d: expected 1 teammate, got %d", matchIdx, len(snap.Teammates))
			}
			if len(snap.Opponents) != 2 {
				t.Fatalf("match %d: expected 2 opponents, got %d", matchIdx, len(snap.Opponents))
			}

			// Verify Opp2 was retained as disconnected
			var opp2Found bool
			for _, o := range snap.Opponents {
				if o.PlayerID == opp2ID {
					opp2Found = true
					if !o.IsDisconnected {
						t.Errorf("match %d: expected Opp2 to be IsDisconnected=true", matchIdx)
					}
				}
			}
			if !opp2Found {
				t.Fatalf("match %d: Opp2 was not retained", matchIdx)
			}

			// Conclude match
			winner := rng.Intn(2)
			if err := tracker.OnMatchEnded(ctx, guid, &winner); err != nil {
				t.Fatalf("match %d: OnMatchEnded failed: %v", matchIdx, err)
			}
		}
	})
}

// TestChallenger_Disconnect_LocalPlayerDropAndReconnection verifies full lifecycle
// of local player disconnecting and reconnecting.
func TestChallenger_Disconnect_LocalPlayerDropAndReconnection(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()
		tracker, err := playertrack.NewTracker(
			store,
			playertrack.NewNoOpRankClient(),
			config.PlayerTrackingConfig{
				Enabled:       true,
				LocalPlayerID: "Steam|local_user|0",
			},
			config.AuthConfig{},
		)
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer tracker.Close()

		guid := "local-reconnect-guid"
		playlistID := 11

		// Frame 1: Local + Teammate + Opponent
		f1 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 100, 1, 0, 1, 2, 0),
			makePlayerWithStats("Tm1", "Steam|tm_1|0", 0, 200, 1, 0, 0, 2, 0),
			makePlayerWithStats("Opp1", "Steam|opp_1|0", 1, 150, 0, 1, 1, 1, 0),
		}
		_ = tracker.OnUpdateState(ctx, guid, playlistID, f1)

		// Frame 2: LocalUser drops (omitted)
		f2 := []statsapi.StatsPlayer{
			makePlayerWithStats("Tm1", "Steam|tm_1|0", 0, 220, 1, 0, 1, 3, 0),
			makePlayerWithStats("Opp1", "Steam|opp_1|0", 1, 170, 0, 1, 1, 2, 0),
		}
		_ = tracker.OnUpdateState(ctx, guid, playlistID, f2)

		snapF2 := tracker.GetCurrentMatch()
		if snapF2.LocalPlayer == nil {
			t.Fatal("expected LocalPlayer retained")
		}
		if !snapF2.LocalPlayer.IsDisconnected {
			t.Errorf("expected LocalPlayer.IsDisconnected=true")
		}
		if snapF2.LocalTeam == nil || *snapF2.LocalTeam != 0 {
			t.Errorf("expected LocalTeam preserved as 0")
		}

		// Frame 3: LocalUser reconnects with updated stats
		f3 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 160, 2, 0, 1, 3, 0),
			makePlayerWithStats("Tm1", "Steam|tm_1|0", 0, 240, 1, 0, 1, 3, 0),
			makePlayerWithStats("Opp1", "Steam|opp_1|0", 1, 180, 0, 1, 1, 2, 0),
		}
		_ = tracker.OnUpdateState(ctx, guid, playlistID, f3)

		snapF3 := tracker.GetCurrentMatch()
		if snapF3.LocalPlayer == nil {
			t.Fatal("expected LocalPlayer present")
		}
		if snapF3.LocalPlayer.IsDisconnected {
			t.Errorf("expected LocalPlayer.IsDisconnected=false after reconnect")
		}
		if snapF3.LocalPlayer.Stats.Score != 160 || snapF3.LocalPlayer.Stats.Goals != 2 {
			t.Errorf("expected updated stats for LocalPlayer: %+v", snapF3.LocalPlayer.Stats)
		}
		if len(snapF3.Teammates) != 1 {
			t.Errorf("expected exactly 1 teammate, got %d", len(snapF3.Teammates))
		}
	})
}
