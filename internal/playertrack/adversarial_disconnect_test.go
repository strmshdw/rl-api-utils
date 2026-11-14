package playertrack_test

import (
	"context"
	"testing"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
)

// ============================================================================
// Adversarial Disconnect Suite 1: Rapid Connect-Disconnect-Reconnect Churn
// ============================================================================

func TestAdversarial_RapidReconnect_NoDuplicateInvariants(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|local_hero|0",
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		matchGUID := "adv-rapid-reconnect-1"
		playlistID := 11

		// Baseline Frame: 2v2 lobby
		basePlayers := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalHero", "Steam|local_hero|0", 0, 100, 1, 0, 0, 1, 0),
			makePlayerWithStats("UnstableTeammate", "Steam|unstable_tm|0", 0, 150, 1, 1, 0, 2, 0),
			makePlayerWithStats("Rival1", "Steam|rival_1|0", 1, 120, 0, 1, 1, 1, 0),
			makePlayerWithStats("Rival2", "Steam|rival_2|0", 1, 140, 0, 0, 2, 1, 0),
		}
		if err := tracker.OnUpdateState(ctx, matchGUID, playlistID, basePlayers); err != nil {
			t.Fatalf("Baseline frame failed: %v", err)
		}

		// Perform 10 cycles of disconnect -> reconnect with progressive stats
		tmScore := 150
		tmGoals := 1
		for cycle := 1; cycle <= 10; cycle++ {
			// Step A: Disconnect (UnstableTeammate omitted)
			dropPlayers := []statsapi.StatsPlayer{
				makePlayerWithStats("LocalHero", "Steam|local_hero|0", 0, 100+cycle*10, 1, 0, 0, 1+cycle, 0),
				makePlayerWithStats("Rival1", "Steam|rival_1|0", 1, 120+cycle*5, 0, 1, 1, 1, 0),
				makePlayerWithStats("Rival2", "Steam|rival_2|0", 1, 140+cycle*5, 0, 0, 2, 1, 0),
			}
			if err := tracker.OnUpdateState(ctx, matchGUID, playlistID, dropPlayers); err != nil {
				t.Fatalf("Cycle %d disconnect frame failed: %v", cycle, err)
			}

			snapDrop := tracker.GetCurrentMatch()
			if len(snapDrop.Teammates) != 1 {
				t.Fatalf("Cycle %d: expected exactly 1 teammate retained on disconnect, got %d", cycle, len(snapDrop.Teammates))
			}
			retained := snapDrop.Teammates[0]
			if retained.PlayerID != "Steam|unstable_tm|0" {
				t.Fatalf("Cycle %d: expected retained teammate ID 'Steam|unstable_tm|0', got %q", cycle, retained.PlayerID)
			}
			if !retained.IsDisconnected {
				t.Fatalf("Cycle %d: expected IsDisconnected=true on disconnect step", cycle)
			}
			if retained.Stats.Score != tmScore || retained.Stats.Goals != tmGoals {
				t.Fatalf("Cycle %d: expected preserved stats (score=%d, goals=%d), got (score=%d, goals=%d)",
					cycle, tmScore, tmGoals, retained.Stats.Score, retained.Stats.Goals)
			}

			// Step B: Reconnect with increased score and shots
			tmScore += 60
			tmGoals += 1
			reconnectPlayers := []statsapi.StatsPlayer{
				makePlayerWithStats("LocalHero", "Steam|local_hero|0", 0, 100+cycle*10, 1, 0, 0, 1+cycle, 0),
				makePlayerWithStats("UnstableTeammate", "Steam|unstable_tm|0", 0, tmScore, tmGoals, 1, 0, 2+cycle, 0),
				makePlayerWithStats("Rival1", "Steam|rival_1|0", 1, 120+cycle*5, 0, 1, 1, 1, 0),
				makePlayerWithStats("Rival2", "Steam|rival_2|0", 1, 140+cycle*5, 0, 0, 2, 1, 0),
			}
			if err := tracker.OnUpdateState(ctx, matchGUID, playlistID, reconnectPlayers); err != nil {
				t.Fatalf("Cycle %d reconnect frame failed: %v", cycle, err)
			}

			snapRec := tracker.GetCurrentMatch()
			if len(snapRec.Teammates) != 1 {
				t.Fatalf("Cycle %d: expected exactly 1 teammate after reconnect (no duplicates!), got %d", cycle, len(snapRec.Teammates))
			}
			restored := snapRec.Teammates[0]
			if restored.PlayerID != "Steam|unstable_tm|0" {
				t.Fatalf("Cycle %d: expected teammate ID 'Steam|unstable_tm|0', got %q", cycle, restored.PlayerID)
			}
			if restored.IsDisconnected {
				t.Fatalf("Cycle %d: expected IsDisconnected=false on reconnect step", cycle)
			}
			if restored.Stats.Score != tmScore || restored.Stats.Goals != tmGoals {
				t.Fatalf("Cycle %d: expected updated stats after reconnect (score=%d, goals=%d), got (score=%d, goals=%d)",
					cycle, tmScore, tmGoals, restored.Stats.Score, restored.Stats.Goals)
			}
		}

		// Conclude match and verify outcome persists cleanly
		winnerBlue := 0
		if err := tracker.OnMatchEnded(ctx, matchGUID, &winnerBlue); err != nil {
			t.Fatalf("OnMatchEnded failed: %v", err)
		}

		tmRecord, err := store.GetPlayerMatchup(ctx, "Steam|unstable_tm|0", playlistID)
		if err != nil {
			t.Fatalf("GetPlayerMatchup failed: %v", err)
		}
		if tmRecord == nil || tmRecord.WinsAsTeammate != 1 || tmRecord.TotalMatches != 1 {
			t.Fatalf("Expected 1 win as teammate after rapid reconnect cycles, got %+v", tmRecord)
		}
	})
}

// ============================================================================
// Adversarial Disconnect Suite 2: Splitscreen Players Independent Tracking
// ============================================================================

func TestAdversarial_Splitscreen_IndependentDisconnectAndLocalFallback(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|76561198000000001|0", // Primary player configured as local
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		matchGUID := "adv-splitscreen-1"
		playlistID := 11

		// Frame 1: Local primary (splitscreen 0), splitscreen guest (splitscreen 1), and 2 opponents
		playersF1 := []statsapi.StatsPlayer{
			makePlayerWithStats("PrimaryUser", "Steam|76561198000000001|0", 0, 200, 1, 0, 1, 2, 0),
			makePlayerWithStats("GuestUser(1)", "Steam|76561198000000001|1", 0, 150, 0, 1, 0, 1, 0),
			makePlayerWithStats("Rival1", "Steam|rival_1|0", 1, 180, 1, 0, 1, 2, 0),
			makePlayerWithStats("Rival2", "Steam|rival_2|0", 1, 110, 0, 1, 0, 1, 0),
		}
		if err := tracker.OnUpdateState(ctx, matchGUID, playlistID, playersF1); err != nil {
			t.Fatalf("Frame 1 failed: %v", err)
		}

		snapF1 := tracker.GetCurrentMatch()
		if snapF1.LocalPlayer == nil || snapF1.LocalPlayer.PlayerID != "Steam|76561198000000001|0" {
			t.Fatalf("Frame 1: expected LocalPlayer 'Steam|76561198000000001|0', got %+v", snapF1.LocalPlayer)
		}
		if len(snapF1.Teammates) != 1 || snapF1.Teammates[0].PlayerID != "Steam|76561198000000001|1" {
			t.Fatalf("Frame 1: expected splitscreen guest in Teammates, got %+v", snapF1.Teammates)
		}

		// Frame 2: Guest user leaves early; Primary user remains active
		playersF2 := []statsapi.StatsPlayer{
			makePlayerWithStats("PrimaryUser", "Steam|76561198000000001|0", 0, 240, 1, 0, 2, 3, 0),
			// GuestUser(1) omitted!
			makePlayerWithStats("Rival1", "Steam|rival_1|0", 1, 200, 1, 0, 1, 2, 0),
			makePlayerWithStats("Rival2", "Steam|rival_2|0", 1, 130, 0, 1, 0, 1, 0),
		}
		if err := tracker.OnUpdateState(ctx, matchGUID, playlistID, playersF2); err != nil {
			t.Fatalf("Frame 2 failed: %v", err)
		}

		snapF2 := tracker.GetCurrentMatch()
		if snapF2.LocalPlayer.IsDisconnected {
			t.Errorf("Frame 2: primary local player should remain connected")
		}
		if len(snapF2.Teammates) != 1 {
			t.Fatalf("Frame 2: expected 1 retained guest teammate, got %d", len(snapF2.Teammates))
		}
		guestF2 := snapF2.Teammates[0]
		if guestF2.PlayerID != "Steam|76561198000000001|1" {
			t.Errorf("Frame 2: expected guest teammate 'Steam|76561198000000001|1', got %q", guestF2.PlayerID)
		}
		if !guestF2.IsDisconnected {
			t.Errorf("Frame 2: expected guest teammate IsDisconnected=true")
		}
		if guestF2.Stats.Score != 150 {
			t.Errorf("Frame 2: expected guest stats preserved (Score=150), got %d", guestF2.Stats.Score)
		}

		// Frame 3: Primary user ALSO leaves, guest rejoins
		playersF3 := []statsapi.StatsPlayer{
			// PrimaryUser omitted!
			makePlayerWithStats("GuestUser(1)", "Steam|76561198000000001|1", 0, 260, 1, 1, 1, 3, 0),
			makePlayerWithStats("Rival1", "Steam|rival_1|0", 1, 220, 1, 0, 1, 2, 0),
			makePlayerWithStats("Rival2", "Steam|rival_2|0", 1, 140, 0, 1, 0, 1, 0),
		}
		if err := tracker.OnUpdateState(ctx, matchGUID, playlistID, playersF3); err != nil {
			t.Fatalf("Frame 3 failed: %v", err)
		}

		snapF3 := tracker.GetCurrentMatch()
		// Primary local player should be retained as disconnected
		if snapF3.LocalPlayer == nil || snapF3.LocalPlayer.PlayerID != "Steam|76561198000000001|0" {
			t.Fatalf("Frame 3: expected retained local player 'Steam|76561198000000001|0', got %+v", snapF3.LocalPlayer)
		}
		if !snapF3.LocalPlayer.IsDisconnected {
			t.Errorf("Frame 3: expected LocalPlayer.IsDisconnected=true")
		}
		if snapF3.LocalPlayer.Stats.Score != 240 {
			t.Errorf("Frame 3: expected retained local player score 240, got %d", snapF3.LocalPlayer.Stats.Score)
		}
		// Guest teammate should now be reconnected and active
		if len(snapF3.Teammates) != 1 {
			t.Fatalf("Frame 3: expected 1 guest teammate, got %d", len(snapF3.Teammates))
		}
		guestF3 := snapF3.Teammates[0]
		if guestF3.PlayerID != "Steam|76561198000000001|1" {
			t.Errorf("Frame 3: expected guest teammate 'Steam|76561198000000001|1', got %q", guestF3.PlayerID)
		}
		if guestF3.IsDisconnected {
			t.Errorf("Frame 3: expected guest teammate IsDisconnected=false after rejoining")
		}
		if guestF3.Stats.Score != 260 {
			t.Errorf("Frame 3: expected guest stats updated to 260, got %d", guestF3.Stats.Score)
		}
	})
}

// ============================================================================
// Adversarial Disconnect Suite 3: Casual Bot Replacement Churn
// ============================================================================

func TestAdversarial_CasualBotChurn_DepartedBotsNeverRetained(t *testing.T) {
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

		matchGUID := "adv-bot-churn-1"
		playlistID := 1 // Casual 3v3

		// Frame 1: Full human 2v2 (Local + HumanTeammate vs 2 Opponents)
		f1 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 100, 1, 0, 0, 1, 0),
			makePlayerWithStats("HumanTm", "Steam|human_tm|0", 0, 200, 1, 1, 0, 2, 1),
			makePlayerWithStats("Opponent1", "Steam|opp_1|0", 1, 150, 0, 1, 1, 1, 0),
			makePlayerWithStats("Opponent2", "Steam|opp_2|0", 1, 180, 1, 0, 1, 2, 0),
		}
		_ = tracker.OnUpdateState(ctx, matchGUID, playlistID, f1)

		// Frame 2: HumanTm disconnects, replaced by Bot "Tex"
		f2 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 120, 1, 0, 1, 2, 0),
			makePlayerWithStats("Tex", "Unknown|0|0", 0, 30, 0, 0, 0, 1, 0),
			makePlayerWithStats("Opponent1", "Steam|opp_1|0", 1, 160, 0, 1, 1, 1, 0),
			makePlayerWithStats("Opponent2", "Steam|opp_2|0", 1, 190, 1, 0, 1, 2, 0),
		}
		_ = tracker.OnUpdateState(ctx, matchGUID, playlistID, f2)

		snapF2 := tracker.GetCurrentMatch()
		if len(snapF2.Teammates) != 2 {
			t.Fatalf("Frame 2: expected 2 teammates (retained human + active bot), got %d", len(snapF2.Teammates))
		}

		// Frame 3: Bot "Tex" departs! Replaced by Bot "Foamer"
		f3 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 140, 1, 0, 1, 2, 0),
			makePlayerWithStats("Foamer", "Unknown|0|0", 0, 10, 0, 0, 0, 0, 0),
			makePlayerWithStats("Opponent1", "Steam|opp_1|0", 1, 170, 0, 1, 1, 1, 0),
			makePlayerWithStats("Opponent2", "Steam|opp_2|0", 1, 200, 1, 0, 1, 2, 0),
		}
		_ = tracker.OnUpdateState(ctx, matchGUID, playlistID, f3)

		snapF3 := tracker.GetCurrentMatch()
		// ASSERTION: Only HumanTm (disconnected) and Foamer (active bot) should be in teammates.
		// "Tex" MUST NOT be retained!
		if len(snapF3.Teammates) != 2 {
			t.Fatalf("Frame 3: expected exactly 2 teammates (human + 1 current bot), got %d", len(snapF3.Teammates))
		}
		for _, tm := range snapF3.Teammates {
			if tm.IsBot && tm.Name == "Tex" {
				t.Fatalf("CRITICAL BUG: Departed bot 'Tex' was retained as a ghost!")
			}
		}

		// Frame 4: Bot "Foamer" departs! Replaced by a new human "HumanReplacement"
		f4 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 160, 1, 0, 1, 3, 0),
			makePlayerWithStats("HumanReplacement", "Steam|human_rep|0", 0, 50, 0, 1, 0, 1, 0),
			makePlayerWithStats("Opponent1", "Steam|opp_1|0", 1, 180, 0, 1, 1, 1, 0),
			makePlayerWithStats("Opponent2", "Steam|opp_2|0", 1, 210, 1, 0, 1, 2, 0),
		}
		_ = tracker.OnUpdateState(ctx, matchGUID, playlistID, f4)

		snapF4 := tracker.GetCurrentMatch()
		// ASSERTION: Teammates must have HumanTm (disconnected) + HumanReplacement (active). Zero bots!
		if len(snapF4.Teammates) != 2 {
			t.Fatalf("Frame 4: expected exactly 2 teammates, got %d", len(snapF4.Teammates))
		}
		for _, tm := range snapF4.Teammates {
			if tm.IsBot {
				t.Fatalf("CRITICAL BUG: Bot ghost found in Frame 4: %+v", tm)
			}
		}

		// Conclude match: Assert both humans recorded, zero bots recorded in store
		winnerBlue := 0
		if err := tracker.OnMatchEnded(ctx, matchGUID, &winnerBlue); err != nil {
			t.Fatalf("OnMatchEnded failed: %v", err)
		}

		origRecord, err := store.GetPlayerMatchup(ctx, "Steam|human_tm|0", playlistID)
		if err != nil || origRecord == nil || origRecord.WinsAsTeammate != 1 {
			t.Fatalf("Expected original disconnected human recorded 1 win, got %+v (err=%v)", origRecord, err)
		}

		repRecord, err := store.GetPlayerMatchup(ctx, "Steam|human_rep|0", playlistID)
		if err != nil || repRecord == nil || repRecord.WinsAsTeammate != 1 {
			t.Fatalf("Expected replacement human recorded 1 win, got %+v (err=%v)", repRecord, err)
		}

		// Verify bots never recorded
		botRecord, _ := store.GetPlayerMatchup(ctx, "Unknown|0|0", playlistID)
		if botRecord != nil && botRecord.TotalMatches > 0 {
			t.Fatalf("CRITICAL BUG: Bot was recorded in player matchups store: %+v", botRecord)
		}
	})
}

// ============================================================================
// Adversarial Disconnect Suite 4: Sustained Multi-Frame Local Player Disconnect
// ============================================================================
