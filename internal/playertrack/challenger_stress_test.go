package playertrack_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
)

// ============================================================================
// Empirical Challenger Stress Suite: Resolution, Lifecycle, Concurrency & Parity
// ============================================================================

// TestChallenger_ResolutionTiers_AdversarialStress tests all 4 resolution tiers
// against casing variations, leading/trailing whitespace, splitscreen indices,
// and adversarial player names (Unicode, SQL-injection, symbols, empty).
func TestChallenger_ResolutionTiers_AdversarialStress(t *testing.T) {
	ctx := context.Background()

	t.Run("Tier1_CasingVariationsAndWhitespace", func(t *testing.T) {
		testCases := []struct {
			name        string
			configID    string
			statsID     string
			expectMatch bool
		}{
			{
				name:        "LowercaseSteamPlatformInConfig",
				configID:    "steam|76561198000000001|0",
				statsID:     "Steam|76561198000000001|0",
				expectMatch: true,
			},
			{
				name:        "UppercaseSteamPlatformInConfig",
				configID:    "STEAM|76561198000000001|0",
				statsID:     "Steam|76561198000000001|0",
				expectMatch: true,
			},
			{
				name:        "HexCasingDifference_ConfigLower_StatsUpper",
				configID:    "epic|34a02cf8f4414e29b15921876da36f9a|0",
				statsID:     "Epic|34A02CF8F4414E29B15921876DA36F9A|0",
				expectMatch: true,
			},
			{
				name:        "HexCasingDifference_ConfigUpper_StatsLower",
				configID:    "Epic|34A02CF8F4414E29B15921876DA36F9A|0",
				statsID:     "epic|34a02cf8f4414e29b15921876da36f9a|0",
				expectMatch: true,
			},
			{
				name:        "WhitespaceInConfigID_LeadingAndTrailing",
				configID:    "  \tSteam|76561198000000001|0 \n ",
				statsID:     "Steam|76561198000000001|0",
				expectMatch: true,
			},
			{
				name:        "WhitespaceInStatsPlayerPrimaryId",
				configID:    "Steam|76561198000000001|0",
				statsID:     "  Steam|76561198000000001|0  ",
				expectMatch: true,
			},
			{
				name:        "AccountIDOnly_WithWhitespace",
				configID:    "  34a02cf8f4414e29b15921876da36f9a  ",
				statsID:     "Epic|34A02CF8F4414E29B15921876DA36F9A|0",
				expectMatch: true,
			},
			{
				name:        "SplitscreenGuest_ConfigHasBase_StatsHasIndex1",
				configID:    "Steam|76561198000000001",
				statsID:     "Steam|76561198000000001|1",
				expectMatch: true,
			},
			{
				name:        "SplitscreenGuest_ConfigHasAccountID_StatsHasIndex1",
				configID:    "76561198000000001",
				statsID:     "Steam|76561198000000001|1",
				expectMatch: true,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				store := newSQLiteTestStore(t)
				fetcher := newTestSkillFetcher()
				cfg := config.PlayerTrackingConfig{
					Enabled:       true,
					LocalPlayerID: tc.configID,
				}
				tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
				if err != nil {
					t.Fatalf("NewTracker failed: %v", err)
				}
				defer func() { _ = tracker.Close() }()

				players := []statsapi.StatsPlayer{
					makePlayer("TestPlayer", tc.statsID, 0),
					makePlayer("Opponent", "Steam|99999999999999999|0", 1),
				}

				if err := tracker.OnUpdateState(ctx, "guid-"+tc.name, 11, players); err != nil {
					t.Fatalf("OnUpdateState failed: %v", err)
				}

				match := tracker.GetCurrentMatch()
				if tc.expectMatch {
					if match.LocalPlayer == nil {
						t.Fatalf("expected LocalPlayer resolved for %s; got nil", tc.name)
					}
					if match.LocalTeam == nil || *match.LocalTeam != 0 {
						t.Errorf("expected LocalTeam 0, got %v", match.LocalTeam)
					}
				} else {
					if match.LocalPlayer != nil {
						t.Fatalf("expected LocalPlayer nil for %s; got %+v", tc.name, match.LocalPlayer)
					}
				}
			})
		}
	})

	t.Run("Tier2_PrimaryAuth_CasingAndSplitscreen", func(t *testing.T) {
		store := newSQLiteTestStore(t)
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{Enabled: true}
		authCfg := config.AuthConfig{
			Provider: "EPIC", // Uppercase provider
			Epic: config.EpicConfig{
				AccountID: "  ABCDEF0123456789ABCDEF0123456789  ", // Uppercase hex with whitespace
			},
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, authCfg)
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		// Stats has lowercase hex and splitscreen index 1
		players := []statsapi.StatsPlayer{
			makePlayer("GuestUser", "Epic|abcdef0123456789abcdef0123456789|1", 1),
			makePlayer("Rival", "Steam|76561198000000002|0", 0),
		}

		if err := tracker.OnUpdateState(ctx, "m-auth-case", 11, players); err != nil {
			t.Fatalf("OnUpdateState failed: %v", err)
		}

		match := tracker.GetCurrentMatch()
		if match.LocalPlayer == nil {
			t.Fatalf("Tier 2 failed to resolve lowercase hex with splitscreen index 1")
		}
		if match.LocalTeam == nil || *match.LocalTeam != 1 {
			t.Errorf("expected LocalTeam 1, got %v", match.LocalTeam)
		}
	})

	t.Run("Tier3_AdversarialPlayerNames", func(t *testing.T) {
		adversarialNames := []string{
			"🔥⚽ [PRO] Ŝtřîkêř ⚽🔥",
			"' OR '1'='1'; DROP TABLE players; --",
			"<script>alert('xss')</script>",
			"Player|With|Pipes",
			"NameWith\nNewlineAnd\tTabs",
			"   WhitespaceSurroundedName   ",
			"NormalNameWithTrailingSpace ",
			"null",
			"undefined",
			"Unknown",
			strings.Repeat("LongName_", 50), // 450 chars
		}

		for idx, advName := range adversarialNames {
			t.Run(fmt.Sprintf("AdvName_%d", idx), func(t *testing.T) {
				store := newSQLiteTestStore(t)
				fetcher := newTestSkillFetcher()
				cfg := config.PlayerTrackingConfig{
					Enabled:         true,
					LocalPlayerName: advName,
				}
				tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
				if err != nil {
					t.Fatalf("NewTracker failed: %v", err)
				}
				defer func() { _ = tracker.Close() }()

				players := []statsapi.StatsPlayer{
					makePlayer(advName, fmt.Sprintf("Steam|adv_%d|0", idx), 0),
					makePlayer("OtherPlayer", "Steam|other|0", 1),
				}

				if err := tracker.OnUpdateState(ctx, fmt.Sprintf("m-adv-%d", idx), 11, players); err != nil {
					t.Fatalf("OnUpdateState failed for %q: %v", advName, err)
				}

				match := tracker.GetCurrentMatch()
				if match.LocalPlayer == nil {
					t.Fatalf("expected LocalPlayer resolved for adversarial name %q", advName)
				}
				if match.LocalPlayer.Name != advName {
					t.Errorf("expected LocalPlayer name %q, got %q", advName, match.LocalPlayer.Name)
				}
			})
		}
	})

	t.Run("Tier4_AuthDisplayName_CasingAndWhitespace", func(t *testing.T) {
		store := newSQLiteTestStore(t)
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{Enabled: true}
		authCfg := config.AuthConfig{
			Provider: "steam",
			Steam: config.SteamConfig{
				AccountName: "  CrAzY_StRiKeR  ",
			},
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, authCfg)
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		players := []statsapi.StatsPlayer{
			makePlayer("crazy_striker", "Steam|some_id|0", 0),
			makePlayer("Opponent", "Steam|opp_id|0", 1),
		}

		if err := tracker.OnUpdateState(ctx, "m-auth-name", 11, players); err != nil {
			t.Fatalf("OnUpdateState failed: %v", err)
		}

		match := tracker.GetCurrentMatch()
		if match.LocalPlayer == nil || match.LocalPlayer.Name != "crazy_striker" {
			t.Fatalf("expected Tier 4 case-insensitive match for crazy_striker; got %+v", match.LocalPlayer)
		}
	})
}

// TestChallenger_ConcurrentDuplicateMatchEnded tests high concurrency duplicate
// MatchEnded events and verifies ZERO counter drift across both SQLite and JSONStore.
func TestChallenger_ConcurrentDuplicateMatchEnded(t *testing.T) {
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

		matchGUID := "concurrent-dup-match-001"
		playlistID := 11

		players := []statsapi.StatsPlayer{
			makePlayer("LocalHero", "Steam|local_hero|0", 0),
			makePlayer("TeammateA", "Steam|team_a|0", 0),
			makePlayer("TeammateB", "Steam|team_b|0", 0),
			makePlayer("Opponent1", "Epic|opp_1|0", 1),
			makePlayer("Opponent2", "Epic|opp_2|0", 1),
			makePlayer("Opponent3", "Epic|opp_3|0", 1),
		}

		if err := tracker.OnUpdateState(ctx, matchGUID, playlistID, players); err != nil {
			t.Fatalf("OnUpdateState failed: %v", err)
		}

		// Concurrently invoke OnMatchEnded from 80 goroutines
		const concurrency = 80
		var wg sync.WaitGroup
		winner := 0 // Local team wins
		errChan := make(chan error, concurrency)

		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := tracker.OnMatchEnded(ctx, matchGUID, &winner); err != nil {
					errChan <- err
				}
			}()
		}

		wg.Wait()
		close(errChan)

		for e := range errChan {
			t.Errorf("concurrent OnMatchEnded returned unexpected error: %v", e)
		}

		// Verify zero counter drift for ALL teammates and opponents
		// Each teammate must have WinsAsTeammate == 1, TotalMatches == 1
		for _, tmID := range []string{"Steam|team_a|0", "Steam|team_b|0"} {
			m, err := store.GetPlayerMatchup(ctx, tmID, playlistID)
			if err != nil {
				t.Fatalf("GetPlayerMatchup(%s) failed: %v", tmID, err)
			}
			if m.WinsAsTeammate != 1 {
				t.Errorf("drift detected for %s: WinsAsTeammate = %d, expected 1", tmID, m.WinsAsTeammate)
			}
			if m.LossesAsTeammate != 0 {
				t.Errorf("drift detected for %s: LossesAsTeammate = %d, expected 0", tmID, m.LossesAsTeammate)
			}
			if m.TotalMatches != 1 {
				t.Errorf("drift detected for %s: TotalMatches = %d, expected 1", tmID, m.TotalMatches)
			}
		}

		// Each opponent must have WinsAsOpponent == 1 (local won), LossesAsOpponent == 0, TotalMatches == 1
		for _, oppID := range []string{"Epic|opp_1|0", "Epic|opp_2|0", "Epic|opp_3|0"} {
			m, err := store.GetPlayerMatchup(ctx, oppID, playlistID)
			if err != nil {
				t.Fatalf("GetPlayerMatchup(%s) failed: %v", oppID, err)
			}
			if m.WinsAsOpponent != 1 {
				t.Errorf("drift detected for %s: WinsAsOpponent = %d, expected 1", oppID, m.WinsAsOpponent)
			}
			if m.LossesAsOpponent != 0 {
				t.Errorf("drift detected for %s: LossesAsOpponent = %d, expected 0", oppID, m.LossesAsOpponent)
			}
			if m.TotalMatches != 1 {
				t.Errorf("drift detected for %s: TotalMatches = %d, expected 1", oppID, m.TotalMatches)
			}
		}

		// Verify LocalPlayer itself was NEVER recorded in player_matchups
		localMatchup, err := store.GetPlayerMatchup(ctx, "Steam|local_hero|0", playlistID)
		if err != nil {
			t.Fatalf("GetPlayerMatchup(local) failed: %v", err)
		}
		if localMatchup.TotalMatches != 0 {
			t.Errorf("local player self-recorded in matchups: TotalMatches = %d", localMatchup.TotalMatches)
		}
	})
}

// TestChallenger_NilWinnerAbortedAndSpectators investigates lifecycle edge cases:
// 1. nil winnerTeamNum
// 2. Aborted / unassigned matches
// 3. Other players who are spectators
// 4. Local player when local player is a spectator (TeamNum == 255)
func TestChallenger_NilWinnerAbortedAndSpectators(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()

		t.Run("NilWinnerTeamNum_ZeroOutcomes", func(t *testing.T) {
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

			players := []statsapi.StatsPlayer{
				makePlayer("LocalUser", "Steam|local_user|0", 0),
				makePlayer("Teammate", "Steam|tm_nil|0", 0),
				makePlayer("Opponent", "Epic|opp_nil|0", 1),
			}

			guid := "m-nil-winner-001"
			_ = tracker.OnUpdateState(ctx, guid, 11, players)

			if err := tracker.OnMatchEnded(ctx, guid, nil); err != nil {
				t.Fatalf("OnMatchEnded(nil) failed: %v", err)
			}

			snap := tracker.GetCurrentMatch()
			if snap.ActiveMatch != false {
				t.Errorf("expected ActiveMatch=false; got true")
			}
			if snap.MatchEnded != true {
				t.Errorf("expected MatchEnded=true; got false")
			}
			if snap.WinnerTeam != nil {
				t.Errorf("expected WinnerTeam=nil; got %v", snap.WinnerTeam)
			}
			if snap.Result != "" {
				t.Errorf("expected empty Result; got %q", snap.Result)
			}

			// Verify no matchups were written
			m1, _ := store.GetPlayerMatchup(ctx, "Steam|tm_nil|0", 11)
			if m1.TotalMatches != 0 {
				t.Errorf("expected 0 total matches for teammate on nil winner; got %d", m1.TotalMatches)
			}
			m2, _ := store.GetPlayerMatchup(ctx, "Epic|opp_nil|0", 11)
			if m2.TotalMatches != 0 {
				t.Errorf("expected 0 total matches for opponent on nil winner; got %d", m2.TotalMatches)
			}
		})

		t.Run("SpectatorPlayerInLobby_ExcludedFromOutcomes", func(t *testing.T) {
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

			players := []statsapi.StatsPlayer{
				makePlayer("LocalUser", "Steam|local_user|0", 0),
				makePlayer("Teammate", "Steam|tm_spec_test|0", 0),
				makePlayer("Opponent", "Epic|opp_spec_test|0", 1),
				makePlayer("SpectatorOnly", "Steam|spectator_player|0", 255),
			}

			guid := "m-spectator-lobby-001"
			_ = tracker.OnUpdateState(ctx, guid, 11, players)

			winner := 0
			if err := tracker.OnMatchEnded(ctx, guid, &winner); err != nil {
				t.Fatalf("OnMatchEnded failed: %v", err)
			}

			// Spectator must NOT be recorded in matchups
			specMatchup, _ := store.GetPlayerMatchup(ctx, "Steam|spectator_player|0", 11)
			if specMatchup.TotalMatches != 0 {
				t.Errorf("spectator player was recorded in matchups! TotalMatches = %d", specMatchup.TotalMatches)
			}
		})

		t.Run("AbortedMatch_WinnerTeamNumInvalid_255", func(t *testing.T) {
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

			players := []statsapi.StatsPlayer{
				makePlayer("LocalUser", "Steam|local_user|0", 0),
				makePlayer("Teammate", "Steam|tm_abort|0", 0),
				makePlayer("Opponent", "Epic|opp_abort|0", 1),
			}

			guid := "m-aborted-255-001"
			_ = tracker.OnUpdateState(ctx, guid, 11, players)

			// In aborted match, Stats API might send WinnerTeamNum = 255 (no winner)
			winner255 := 255
			if err := tracker.OnMatchEnded(ctx, guid, &winner255); err != nil {
				t.Fatalf("OnMatchEnded failed: %v", err)
			}

			// In an aborted match with no winner, no player should receive a loss or win
			tmM, _ := store.GetPlayerMatchup(ctx, "Steam|tm_abort|0", 11)
			oppM, _ := store.GetPlayerMatchup(ctx, "Epic|opp_abort|0", 11)
			if tmM.TotalMatches > 0 || oppM.TotalMatches > 0 {
				t.Errorf("CORRUPTED OUTCOME RECORDED: WinnerTeamNum 255 (aborted) resulted in match outcome recorded! tmM=%+v, oppM=%+v", tmM, oppM)
			}
		})

		t.Run("LocalPlayerAsSpectator_NoCorruptedOutcomes", func(t *testing.T) {
			fetcher := newTestSkillFetcher()
			cfg := config.PlayerTrackingConfig{
				Enabled:       true,
				LocalPlayerID: "Steam|local_spectator|0",
			}
			tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
			if err != nil {
				t.Fatalf("NewTracker failed: %v", err)
			}
			defer func() { _ = tracker.Close() }()

			// Local player is spectator (TeamNum 255)
			players := []statsapi.StatsPlayer{
				makePlayer("LocalSpectator", "Steam|local_spectator|0", 255),
				makePlayer("BluePlayer", "Steam|blue_hero|0", 0),
				makePlayer("OrangePlayer", "Epic|orange_hero|0", 1),
			}

			guid := "m-local-is-spectator-001"
			if err := tracker.OnUpdateState(ctx, guid, 11, players); err != nil {
				t.Fatalf("OnUpdateState failed: %v", err)
			}

			snap := tracker.GetCurrentMatch()
			// LocalTeam contract says: nil if unresolved/spectator
			if snap.LocalTeam != nil && *snap.LocalTeam == 255 {
				t.Logf("OBSERVATION: LocalTeam was populated with 255 instead of nil for spectator")
			}

			winner := 0
			if err := tracker.OnMatchEnded(ctx, guid, &winner); err != nil {
				t.Fatalf("OnMatchEnded failed: %v", err)
			}

			// EMPIRICAL CHECK: Did the system corrupt player_matchups for blue_hero and orange_hero?
			blueMatchup, _ := store.GetPlayerMatchup(ctx, "Steam|blue_hero|0", 11)
			orangeMatchup, _ := store.GetPlayerMatchup(ctx, "Epic|orange_hero|0", 11)

			if blueMatchup.TotalMatches > 0 || orangeMatchup.TotalMatches > 0 {
				t.Errorf("CORRUPTED OUTCOME RECORDED: Local player was a spectator (TeamNum 255), but match results were written to player_matchups! blueMatchup=%+v, orangeMatchup=%+v", blueMatchup, orangeMatchup)
			}
		})
	})
}

// TestChallenger_BotImmunity verifies bots are never recorded in players or player_matchups
// across multiple bot ID variations and match outcomes.
func TestChallenger_BotImmunity(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:        true,
			LocalPlayerID:  "Steam|local_human|0",
			AutoFetchRanks: true,
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		botIDs := []string{
			"Unknown|0|0",
			"unknown|0|0",
			"UNKNOWN|0|0",
			"Unknown|0|1",
			"Steam|0|0",
			"Epic|0|0",
			"Unknown|bot_maverick|0",
		}

		var players []statsapi.StatsPlayer
		players = append(players, makePlayer("LocalHuman", "Steam|local_human|0", 0))
		players = append(players, makePlayer("HumanTeammate", "Steam|human_tm|0", 0))
		players = append(players, makePlayer("HumanOpponent", "Epic|human_opp|0", 1))

		for idx, bid := range botIDs {
			team := idx % 2 // distribute across blue and orange
			players = append(players, makePlayer(fmt.Sprintf("Bot_%d", idx), bid, team))
		}

		guid := "m-bot-immunity-001"
		if err := tracker.OnUpdateState(ctx, guid, 11, players); err != nil {
			t.Fatalf("OnUpdateState failed: %v", err)
		}

		winner := 0
		if err := tracker.OnMatchEnded(ctx, guid, &winner); err != nil {
			t.Fatalf("OnMatchEnded failed: %v", err)
		}

		// 1. Verify NO bot ID is in players table
		for _, bid := range botIDs {
			p, err := store.GetPlayer(ctx, bid)
			if !errors.Is(err, storage.ErrPlayerNotFound) {
				t.Errorf("bot ID %q found in players table: record = %+v, err = %v", bid, p, err)
			}
		}

		// 2. Verify NO bot ID is in player_matchups
		for _, bid := range botIDs {
			m, _ := store.GetPlayerMatchup(ctx, bid, 11)
			if m != nil && m.TotalMatches != 0 {
				t.Errorf("bot ID %q has non-zero matchups recorded: %+v", bid, m)
			}
		}

		// 3. Verify ListPlayers contains ONLY the 2 non-local human players
		listed, err := store.ListPlayers(ctx, 100, 0)
		if err != nil {
			t.Fatalf("ListPlayers failed: %v", err)
		}
		for _, lp := range listed {
			for _, bid := range botIDs {
				if strings.EqualFold(lp.PlayerID, bid) {
					t.Errorf("bot ID %q returned in ListPlayers!", lp.PlayerID)
				}
			}
		}

		// 4. Verify human players WERE correctly recorded
		tmM, _ := store.GetPlayerMatchup(ctx, "Steam|human_tm|0", 11)
		if tmM.WinsAsTeammate != 1 || tmM.TotalMatches != 1 {
			t.Errorf("human teammate matchup missing or incorrect: %+v", tmM)
		}

		oppM, _ := store.GetPlayerMatchup(ctx, "Epic|human_opp|0", 11)
		if oppM.WinsAsOpponent != 1 || oppM.TotalMatches != 1 {
			t.Errorf("human opponent matchup missing or incorrect: %+v", oppM)
		}
	})
}

// TestChallenger_CrossStoreParity_FullMatrix runs identical sequences through
// both SQLiteStore and JSONStore and asserts strict parity across all outputs.
func TestChallenger_CrossStoreParity_FullMatrix(t *testing.T) {
	ctx := context.Background()

	sqliteStore := newSQLiteTestStore(t)
	jsonStore := newJSONTestStore(t)

	stores := []struct {
		name  string
		store storage.StateStore
	}{
		{"SQLite", sqliteStore},
		{"JSONStore", jsonStore},
	}

	for _, s := range stores {
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|parity_hero|0",
		}
		tracker, err := playertrack.NewTracker(s.store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("[%s] NewTracker failed: %v", s.name, err)
		}

		// Match 1: Playlist 11 (2v2), Win
		p1 := []statsapi.StatsPlayer{
			makePlayer("ParityHero", "Steam|parity_hero|0", 0),
			makePlayer("Teammate1", "Steam|tm_1|0", 0),
			makePlayer("Opponent1", "Epic|opp_1|0", 1),
			makePlayer("Opponent2", "Epic|opp_2|0", 1),
		}
		_ = tracker.OnUpdateState(ctx, "parity-m1", 11, p1)
		win0 := 0
		_ = tracker.OnMatchEnded(ctx, "parity-m1", &win0)

		// Match 2: Playlist 11 (2v2), Loss (Opponent1 is now Teammate! Swap sides)
		p2 := []statsapi.StatsPlayer{
			makePlayer("ParityHero", "Steam|parity_hero|0", 0),
			makePlayer("Opponent1", "Epic|opp_1|0", 0), // Now teammate
			makePlayer("Teammate1", "Steam|tm_1|0", 1), // Now opponent
			makePlayer("Opponent3", "Epic|opp_3|0", 1),
		}
		_ = tracker.OnUpdateState(ctx, "parity-m2", 11, p2)
		win1 := 1 // Opponents win -> Local loss
		_ = tracker.OnMatchEnded(ctx, "parity-m2", &win1)

		// Match 3: Playlist 13 (3v3), Win
		p3 := []statsapi.StatsPlayer{
			makePlayer("ParityHero", "Steam|parity_hero|0", 0),
			makePlayer("Teammate1", "Steam|tm_1|0", 0),
			makePlayer("Opponent1", "Epic|opp_1|0", 1),
		}
		_ = tracker.OnUpdateState(ctx, "parity-m3", 13, p3)
		_ = tracker.OnMatchEnded(ctx, "parity-m3", &win0)

		_ = tracker.Close()
	}

	// Compare outputs from both stores
	// 1. Players in ListPlayers
	sqlPlayers, err := sqliteStore.ListPlayers(ctx, 100, 0)
	if err != nil {
		t.Fatalf("sqlite ListPlayers failed: %v", err)
	}
	jsonPlayers, err := jsonStore.ListPlayers(ctx, 100, 0)
	if err != nil {
		t.Fatalf("json ListPlayers failed: %v", err)
	}

	if len(sqlPlayers) != len(jsonPlayers) {
		t.Fatalf("ListPlayers count mismatch: sqlite=%d, json=%d", len(sqlPlayers), len(jsonPlayers))
	}

	// 2. Compare Matchups for all players across playlists 11 and 13
	playersToCheck := []string{"Steam|tm_1|0", "Epic|opp_1|0", "Epic|opp_2|0", "Epic|opp_3|0"}
	playlistsToCheck := []int{11, 13}

	for _, pid := range playersToCheck {
		for _, pl := range playlistsToCheck {
			sqlM, errSQL := sqliteStore.GetPlayerMatchup(ctx, pid, pl)
			jsonM, errJSON := jsonStore.GetPlayerMatchup(ctx, pid, pl)

			if (errSQL != nil) != (errJSON != nil) {
				t.Errorf("[%s pl:%d] error mismatch: sqlErr=%v, jsonErr=%v", pid, pl, errSQL, errJSON)
				continue
			}

			if sqlM.WinsAsTeammate != jsonM.WinsAsTeammate ||
				sqlM.LossesAsTeammate != jsonM.LossesAsTeammate ||
				sqlM.WinsAsOpponent != jsonM.WinsAsOpponent ||
				sqlM.LossesAsOpponent != jsonM.LossesAsOpponent ||
				sqlM.TotalMatches != jsonM.TotalMatches {
				t.Errorf("[%s pl:%d] matchup parity mismatch:\nSQL:  %+v\nJSON: %+v", pid, pl, sqlM, jsonM)
			}
		}

		// Compare GetPlayerMatchups (all playlists)
		sqlAll, _ := sqliteStore.GetPlayerMatchups(ctx, pid)
		jsonAll, _ := jsonStore.GetPlayerMatchups(ctx, pid)
		if len(sqlAll) != len(jsonAll) {
			t.Errorf("[%s] GetPlayerMatchups count mismatch: sql=%d, json=%d", pid, len(sqlAll), len(jsonAll))
		}
	}

	// 3. Compare ListPlayerSummaries
	sqlSummaries, err := sqliteStore.ListPlayerSummaries(ctx, 100, 0)
	if err != nil {
		t.Fatalf("sqlite ListPlayerSummaries failed: %v", err)
	}
	jsonSummaries, err := jsonStore.ListPlayerSummaries(ctx, 100, 0)
	if err != nil {
		t.Fatalf("json ListPlayerSummaries failed: %v", err)
	}

	if len(sqlSummaries) != len(jsonSummaries) {
		t.Fatalf("ListPlayerSummaries count mismatch: sql=%d, json=%d", len(sqlSummaries), len(jsonSummaries))
	}

	summaryMapJSON := make(map[string]*storage.PlayerSummary)
	for _, s := range jsonSummaries {
		summaryMapJSON[s.PlayerID] = s
	}

	for _, sqlS := range sqlSummaries {
		jsonS, ok := summaryMapJSON[sqlS.PlayerID]
		if !ok {
			t.Errorf("player %s found in sqlite summaries but missing in json summaries", sqlS.PlayerID)
			continue
		}
		if sqlS.TotalWinsAsTeammate != jsonS.TotalWinsAsTeammate ||
			sqlS.TotalLossesAsTeammate != jsonS.TotalLossesAsTeammate ||
			sqlS.TotalWinsAsOpponent != jsonS.TotalWinsAsOpponent ||
			sqlS.TotalLossesAsOpponent != jsonS.TotalLossesAsOpponent ||
			sqlS.TotalMatches != jsonS.TotalMatches {
			t.Errorf("[%s] summary parity mismatch:\nSQL:  %+v\nJSON: %+v", sqlS.PlayerID, sqlS, jsonS)
		}
	}
}
