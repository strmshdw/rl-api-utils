package playertrack_test

import (
	"context"
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
