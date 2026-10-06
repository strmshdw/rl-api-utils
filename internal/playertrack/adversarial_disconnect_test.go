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
