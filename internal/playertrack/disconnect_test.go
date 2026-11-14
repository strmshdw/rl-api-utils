package playertrack_test

import (
	"context"
	"testing"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
)

func TestTracker_MidGameDisconnect_Lifecycle_TeammateAndLocal(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|local_user|0",
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		matchGUID := "m-disconnect-lifecycle-1"
		playlistID := 11 // Ranked 2v2 Doubles

		// --------------------------------------------------------------------
		// Frame 1: Full Lobby, Active Players Accumulating Stats
		// --------------------------------------------------------------------
		playersF1 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 250, 1, 1, 2, 3, 1),
			makePlayerWithStats("Teammate1", "Steam|teammate_1|0", 0, 300, 2, 0, 1, 4, 2),
			makePlayerWithStats("Rival1", "Steam|rival_1|0", 1, 180, 1, 1, 0, 2, 0),
			makePlayerWithStats("Rival2", "Steam|rival_2|0", 1, 120, 0, 1, 2, 1, 1),
		}

		if err := tracker.OnUpdateState(ctx, matchGUID, playlistID, playersF1); err != nil {
			t.Fatalf("Frame 1 OnUpdateState failed: %v", err)
		}

		snapF1 := tracker.GetCurrentMatch()
		if snapF1 == nil {
			t.Fatal("Frame 1: expected active match snapshot, got nil")
		}
		if !snapF1.ActiveMatch || snapF1.MatchEnded {
			t.Errorf("Frame 1: expected ActiveMatch=true, MatchEnded=false; got ActiveMatch=%v, MatchEnded=%v",
				snapF1.ActiveMatch, snapF1.MatchEnded)
		}
		if snapF1.LocalPlayer == nil || snapF1.LocalPlayer.PlayerID != "Steam|local_user|0" {
			t.Fatalf("Frame 1: expected LocalPlayer 'Steam|local_user|0', got %+v", snapF1.LocalPlayer)
		}
		if snapF1.LocalPlayer.IsDisconnected {
			t.Errorf("Frame 1: local player should not be disconnected")
		}
		if snapF1.LocalTeam == nil || *snapF1.LocalTeam != 0 {
			t.Errorf("Frame 1: expected LocalTeam=0, got %v", snapF1.LocalTeam)
		}
		if len(snapF1.Teammates) != 1 {
			t.Fatalf("Frame 1: expected 1 teammate, got %d", len(snapF1.Teammates))
		}
		tmF1 := snapF1.Teammates[0]
		if tmF1.PlayerID != "Steam|teammate_1|0" || tmF1.IsDisconnected {
			t.Errorf("Frame 1: unexpected teammate state: %+v", tmF1)
		}
		if tmF1.Stats.Score != 300 || tmF1.Stats.Goals != 2 || tmF1.Stats.Demos != 2 {
			t.Errorf("Frame 1: unexpected teammate stats: %+v", tmF1.Stats)
		}
		if len(snapF1.Opponents) != 2 {
			t.Fatalf("Frame 1: expected 2 opponents, got %d", len(snapF1.Opponents))
		}

		// --------------------------------------------------------------------
		// Frame 2: Teammate Leaves Early (Omitted from UpdateState)
		// --------------------------------------------------------------------
		playersF2 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 270, 1, 1, 2, 4, 1),
			// Teammate1 omitted!
			makePlayerWithStats("Rival1", "Steam|rival_1|0", 1, 200, 1, 1, 1, 3, 0),
			makePlayerWithStats("Rival2", "Steam|rival_2|0", 1, 140, 0, 1, 2, 1, 1),
		}

		if err := tracker.OnUpdateState(ctx, matchGUID, playlistID, playersF2); err != nil {
			t.Fatalf("Frame 2 OnUpdateState failed: %v", err)
		}

		snapF2 := tracker.GetCurrentMatch()
		if snapF2 == nil {
			t.Fatal("Frame 2: expected active match snapshot, got nil")
		}
		if !snapF2.ActiveMatch || snapF2.MatchEnded {
			t.Errorf("Frame 2: expected match to remain active")
		}

		// ASSERTION: Disconnected teammate MUST be retained in active match state
		if len(snapF2.Teammates) != 1 {
			t.Fatalf("Frame 2: expected disconnected teammate to be retained (len=1), got %d", len(snapF2.Teammates))
		}
		tmF2 := snapF2.Teammates[0]
		if tmF2.PlayerID != "Steam|teammate_1|0" {
			t.Errorf("Frame 2: expected retained teammate ID 'Steam|teammate_1|0', got %q", tmF2.PlayerID)
		}
		if !tmF2.IsDisconnected {
			t.Errorf("Frame 2: expected IsDisconnected=true for omitted teammate")
		}
		// ASSERTION: Accumulated stats MUST be preserved exactly
		if tmF2.Stats.Score != 300 || tmF2.Stats.Goals != 2 || tmF2.Stats.Assists != 0 ||
			tmF2.Stats.Saves != 1 || tmF2.Stats.Shots != 4 || tmF2.Stats.Demos != 2 {
			t.Errorf("Frame 2: disconnected teammate stats were modified or lost: %+v", tmF2.Stats)
		}
		// Active players in Frame 2 updated normally
		if snapF2.LocalPlayer == nil || snapF2.LocalPlayer.IsDisconnected {
			t.Errorf("Frame 2: local player should remain active and connected")
		}
		if snapF2.LocalPlayer.Stats.Score != 270 {
			t.Errorf("Frame 2: local player score not updated: got %d", snapF2.LocalPlayer.Stats.Score)
		}

		// --------------------------------------------------------------------
		// Frame 3: Disconnected Teammate Reconnects
		// --------------------------------------------------------------------
		playersF3 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 270, 1, 1, 2, 4, 1),
			// Teammate1 returns with updated score/shots
			makePlayerWithStats("Teammate1", "Steam|teammate_1|0", 0, 450, 3, 1, 2, 6, 2),
			makePlayerWithStats("Rival1", "Steam|rival_1|0", 1, 220, 1, 1, 1, 3, 0),
			makePlayerWithStats("Rival2", "Steam|rival_2|0", 1, 160, 0, 1, 2, 2, 1),
		}

		if err := tracker.OnUpdateState(ctx, matchGUID, playlistID, playersF3); err != nil {
			t.Fatalf("Frame 3 OnUpdateState failed: %v", err)
		}

		snapF3 := tracker.GetCurrentMatch()
		// ASSERTION: No duplicate entries on reconnection
		if len(snapF3.Teammates) != 1 {
			t.Fatalf("Frame 3: expected exactly 1 teammate entry after reconnect (no duplicates), got %d", len(snapF3.Teammates))
		}
		tmF3 := snapF3.Teammates[0]
		if tmF3.PlayerID != "Steam|teammate_1|0" {
			t.Errorf("Frame 3: expected teammate ID 'Steam|teammate_1|0', got %q", tmF3.PlayerID)
		}
		if tmF3.IsDisconnected {
			t.Errorf("Frame 3: expected IsDisconnected=false after reconnection")
		}
		if tmF3.Stats.Score != 450 || tmF3.Stats.Goals != 3 || tmF3.Stats.Shots != 6 {
			t.Errorf("Frame 3: teammate stats not updated after reconnect: %+v", tmF3.Stats)
		}

		// --------------------------------------------------------------------
		// Frame 4: Local Player Leaves Early (Omitted from UpdateState)
		// --------------------------------------------------------------------
		playersF4 := []statsapi.StatsPlayer{
			// LocalUser omitted!
			makePlayerWithStats("Teammate1", "Steam|teammate_1|0", 0, 450, 3, 1, 2, 6, 2),
			makePlayerWithStats("Rival1", "Steam|rival_1|0", 1, 220, 1, 1, 1, 3, 0),
			makePlayerWithStats("Rival2", "Steam|rival_2|0", 1, 160, 0, 1, 2, 2, 1),
		}

		if err := tracker.OnUpdateState(ctx, matchGUID, playlistID, playersF4); err != nil {
			t.Fatalf("Frame 4 OnUpdateState failed: %v", err)
		}

		snapF4 := tracker.GetCurrentMatch()
		// ASSERTION: Local player and local team preserved on early disconnect
		if snapF4.LocalPlayer == nil {
			t.Fatal("Frame 4: expected LocalPlayer to be retained when omitted, got nil")
		}
		if snapF4.LocalPlayer.PlayerID != "Steam|local_user|0" {
			t.Errorf("Frame 4: expected LocalPlayer ID 'Steam|local_user|0', got %q", snapF4.LocalPlayer.PlayerID)
		}
		if !snapF4.LocalPlayer.IsDisconnected {
			t.Errorf("Frame 4: expected LocalPlayer.IsDisconnected=true")
		}
		if snapF4.LocalPlayer.Stats.Score != 270 {
			t.Errorf("Frame 4: expected LocalPlayer stats preserved (Score=270), got %d", snapF4.LocalPlayer.Stats.Score)
		}
		if snapF4.LocalTeam == nil || *snapF4.LocalTeam != 0 {
			t.Errorf("Frame 4: expected LocalTeam preserved as 0, got %v", snapF4.LocalTeam)
		}

		// --------------------------------------------------------------------
		// Match Conclusion: Win/Loss Outcomes Recorded Despite Disconnects
		// --------------------------------------------------------------------
		winnerBlue := 0
		if err := tracker.OnMatchEnded(ctx, matchGUID, &winnerBlue); err != nil {
			t.Fatalf("OnMatchEnded failed: %v", err)
		}

		snapEnded := tracker.GetCurrentMatch()
		if snapEnded.ActiveMatch || !snapEnded.MatchEnded {
			t.Errorf("expected ActiveMatch=false, MatchEnded=true post-conclusion")
		}
		if snapEnded.Result != "victory" {
			t.Errorf("expected Result='victory', got %q", snapEnded.Result)
		}

		// Verify store recorded outcomes because local player/team was preserved
		tmRecord, err := store.GetPlayerMatchup(ctx, "Steam|teammate_1|0", playlistID)
		if err != nil {
			t.Fatalf("GetPlayerMatchup failed: %v", err)
		}
		if tmRecord.WinsAsTeammate != 1 || tmRecord.TotalMatches != 1 {
			t.Errorf("expected teammate recorded 1 win, got %+v", tmRecord)
		}

		rivalRecord, err := store.GetPlayerMatchup(ctx, "Steam|rival_1|0", playlistID)
		if err != nil {
			t.Fatalf("GetPlayerMatchup for rival failed: %v", err)
		}
		if rivalRecord.WinsAsOpponent != 1 || rivalRecord.TotalMatches != 1 {
			t.Errorf("expected rival recorded 1 win as opponent, got %+v", rivalRecord)
		}
	})
}

// TestTracker_MidGameDisconnect_OpponentDisconnectAndReconnect asserts that
// opposing players who disconnect mid-game are preserved in the Opponents slice
// with stats intact, marked IsDisconnected=true, and restored cleanly upon return.
func TestTracker_MidGameDisconnect_OpponentDisconnectAndReconnect(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|local_user|0",
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		matchGUID := "m-opp-disconnect-1"
		playlistID := 11

		// Frame 1: Full lobby
		playersF1 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 100, 0, 0, 1, 1, 0),
			makePlayerWithStats("Opponent1", "Steam|opp_1|0", 1, 200, 1, 0, 1, 2, 0),
			makePlayerWithStats("Opponent2", "Steam|opp_2|0", 1, 250, 1, 1, 0, 3, 1),
		}
		_ = tracker.OnUpdateState(ctx, matchGUID, playlistID, playersF1)

		// Frame 2: Opponent2 disconnects
		playersF2 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 120, 0, 0, 1, 2, 0),
			makePlayerWithStats("Opponent1", "Steam|opp_1|0", 1, 220, 1, 0, 2, 2, 0),
			// Opponent2 omitted
		}
		_ = tracker.OnUpdateState(ctx, matchGUID, playlistID, playersF2)

		snapF2 := tracker.GetCurrentMatch()
		if len(snapF2.Opponents) != 2 {
			t.Fatalf("expected 2 opponents retained, got %d", len(snapF2.Opponents))
		}

		var opp1, opp2 *playertrack.LobbyPlayer
		for i := range snapF2.Opponents {
			if snapF2.Opponents[i].PlayerID == "Steam|opp_1|0" {
				opp1 = &snapF2.Opponents[i]
			} else if snapF2.Opponents[i].PlayerID == "Steam|opp_2|0" {
				opp2 = &snapF2.Opponents[i]
			}
		}
		if opp1 == nil || opp2 == nil {
			t.Fatalf("expected both opponents present in snapshot")
		}
		if opp1.IsDisconnected {
			t.Errorf("opp1 should not be disconnected")
		}
		if !opp2.IsDisconnected {
			t.Errorf("opp2 must be marked IsDisconnected=true")
		}
		if opp2.Stats.Score != 250 || opp2.Stats.Goals != 1 || opp2.Stats.Demos != 1 {
			t.Errorf("opp2 stats were lost: %+v", opp2.Stats)
		}

		// Frame 3: Opponent2 reconnects with updated stats
		playersF3 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 150, 0, 0, 2, 2, 0),
			makePlayerWithStats("Opponent1", "Steam|opp_1|0", 1, 250, 1, 0, 2, 3, 0),
			makePlayerWithStats("Opponent2", "Steam|opp_2|0", 1, 380, 2, 1, 1, 5, 2),
		}
		_ = tracker.OnUpdateState(ctx, matchGUID, playlistID, playersF3)

		snapF3 := tracker.GetCurrentMatch()
		if len(snapF3.Opponents) != 2 {
			t.Fatalf("expected 2 opponents after reconnect (no duplicates), got %d", len(snapF3.Opponents))
		}
		for _, o := range snapF3.Opponents {
			if o.PlayerID == "Steam|opp_2|0" {
				if o.IsDisconnected {
					t.Errorf("opp2 should have IsDisconnected=false after reconnect")
				}
				if o.Stats.Score != 380 || o.Stats.Goals != 2 || o.Stats.Demos != 2 {
					t.Errorf("opp2 stats not updated: %+v", o.Stats)
				}
			}
		}
	})
}

// TestTracker_MidGameDisconnect_MultipleSimultaneous verifies that simultaneous
// drops across both teams retain all disconnected players correctly.
func TestTracker_MidGameDisconnect_MultipleSimultaneous(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|local_user|0",
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		matchGUID := "m-multi-drop-1"
		playlistID := 13 // 3v3 Standard

		// Frame 1: 3v3 full lobby (6 players)
		playersF1 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 200, 1, 0, 1, 2, 0),
			makePlayerWithStats("Tm1", "Steam|tm_1|0", 0, 150, 0, 1, 1, 1, 0),
			makePlayerWithStats("Tm2", "Steam|tm_2|0", 0, 180, 1, 0, 0, 2, 1),
			makePlayerWithStats("Opp1", "Steam|opp_1|0", 1, 220, 1, 0, 1, 3, 0),
			makePlayerWithStats("Opp2", "Steam|opp_2|0", 1, 140, 0, 1, 1, 1, 0),
			makePlayerWithStats("Opp3", "Steam|opp_3|0", 1, 160, 0, 0, 2, 1, 0),
		}
		_ = tracker.OnUpdateState(ctx, matchGUID, playlistID, playersF1)

		// Frame 2: Both Tm2 and Opp3 disconnect simultaneously
		playersF2 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 220, 1, 0, 1, 3, 0),
			makePlayerWithStats("Tm1", "Steam|tm_1|0", 0, 160, 0, 1, 1, 2, 0),
			// Tm2 omitted!
			makePlayerWithStats("Opp1", "Steam|opp_1|0", 1, 240, 1, 0, 1, 3, 0),
			makePlayerWithStats("Opp2", "Steam|opp_2|0", 1, 150, 0, 1, 1, 1, 0),
			// Opp3 omitted!
		}
		_ = tracker.OnUpdateState(ctx, matchGUID, playlistID, playersF2)

		snapF2 := tracker.GetCurrentMatch()
		if len(snapF2.Teammates) != 2 {
			t.Fatalf("expected 2 teammates retained, got %d", len(snapF2.Teammates))
		}
		if len(snapF2.Opponents) != 3 {
			t.Fatalf("expected 3 opponents retained, got %d", len(snapF2.Opponents))
		}

		for _, tm := range snapF2.Teammates {
			if tm.PlayerID == "Steam|tm_2|0" {
				if !tm.IsDisconnected {
					t.Errorf("Tm2 must be marked IsDisconnected=true")
				}
				if tm.Stats.Score != 180 || tm.Stats.Goals != 1 {
					t.Errorf("Tm2 stats lost: %+v", tm.Stats)
				}
			}
		}

		for _, opp := range snapF2.Opponents {
			if opp.PlayerID == "Steam|opp_3|0" {
				if !opp.IsDisconnected {
					t.Errorf("Opp3 must be marked IsDisconnected=true")
				}
				if opp.Stats.Score != 160 || opp.Stats.Saves != 2 {
					t.Errorf("Opp3 stats lost: %+v", opp.Stats)
				}
			}
		}
	})
}

// TestTracker_MidGameDisconnect_MatchTransition_ResetsOldDisconnected ensures that
// disconnected players from match N do NOT leak into match N+1.
func TestTracker_MidGameDisconnect_MatchTransition_ResetsOldDisconnected(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|local_user|0",
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		// Match 1: Player drops and is retained
		playersM1 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 100, 1, 0, 0, 1, 0),
			makePlayerWithStats("OldTeammate", "Steam|old_tm|0", 0, 200, 1, 0, 1, 2, 0),
			makePlayerWithStats("Rival", "Steam|rival|0", 1, 150, 0, 1, 1, 1, 0),
		}
		_ = tracker.OnUpdateState(ctx, "guid-match-1", 11, playersM1)

		playersM1Drop := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 120, 1, 0, 0, 2, 0),
			makePlayerWithStats("Rival", "Steam|rival|0", 1, 150, 0, 1, 1, 1, 0),
		}
		_ = tracker.OnUpdateState(ctx, "guid-match-1", 11, playersM1Drop)

		snapM1 := tracker.GetCurrentMatch()
		if len(snapM1.Teammates) != 1 || !snapM1.Teammates[0].IsDisconnected {
			t.Fatalf("expected OldTeammate retained as disconnected in match 1")
		}

		// Match 2: Completely new match GUID with new players
		playersM2 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 50, 0, 0, 0, 1, 0),
			makePlayerWithStats("NewTeammate", "Steam|new_tm|0", 0, 80, 0, 1, 0, 1, 0),
			makePlayerWithStats("NewRival", "Steam|new_rival|0", 1, 90, 1, 0, 0, 2, 0),
		}
		_ = tracker.OnUpdateState(ctx, "guid-match-2", 11, playersM2)

		snapM2 := tracker.GetCurrentMatch()
		if snapM2.MatchGUID != "guid-match-2" {
			t.Errorf("expected MatchGUID 'guid-match-2', got %q", snapM2.MatchGUID)
		}
		if len(snapM2.Teammates) != 1 {
			t.Fatalf("expected exactly 1 teammate in match 2, got %d", len(snapM2.Teammates))
		}
		if snapM2.Teammates[0].PlayerID != "Steam|new_tm|0" {
			t.Errorf("expected NewTeammate in match 2, got %q", snapM2.Teammates[0].PlayerID)
		}
		// Confirm OldTeammate is nowhere in match 2
		for _, tm := range snapM2.Teammates {
			if tm.PlayerID == "Steam|old_tm|0" {
				t.Errorf("old disconnected teammate leaked into match 2!")
			}
		}
	})
}

// TestTracker_MidGameDisconnect_BotReplacement verifies casual match bot backfill:
// When a human player disconnects, they are retained as disconnected, while an AI
// bot joining in their place is added as an active participant.
