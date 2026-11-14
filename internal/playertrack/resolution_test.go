package playertrack_test

import (
	"context"
	"testing"

	"github.com/dank/rl-api-utils/internal/auth"
	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/statsapi"
)

// Test Suite 1: 4-Tier Local Player Resolution Tests
// ============================================================================

func TestTracker_LocalPlayerResolution_All4Tiers(t *testing.T) {
	ctx := context.Background()

	t.Run("Tier1_ExactPrimaryID", func(t *testing.T) {
		store := newSQLiteTestStore(t)
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|76561198000000001|0",
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		players := []statsapi.StatsPlayer{
			makePlayer("Player1", "Steam|76561198000000001|0", 0),
			makePlayer("Player2", "Steam|76561198000000002|0", 1),
		}

		if err := tracker.OnUpdateState(ctx, "m1", 11, players); err != nil {
			t.Fatalf("OnUpdateState failed: %v", err)
		}

		match := tracker.GetCurrentMatch()
		if match == nil || match.LocalPlayer == nil {
			t.Fatalf("expected LocalPlayer resolved; got nil")
		}
		if match.LocalPlayer.PlayerID != "Steam|76561198000000001|0" {
			t.Errorf("expected LocalPlayer ID %q, got %q", "Steam|76561198000000001|0", match.LocalPlayer.PlayerID)
		}
		if match.LocalTeam == nil || *match.LocalTeam != 0 {
			t.Errorf("expected LocalTeam 0, got %v", match.LocalTeam)
		}
	})

	t.Run("Tier1_AccountIDOnly_And_CaseInsensitive", func(t *testing.T) {
		store := newSQLiteTestStore(t)
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "  34a02cf8f4414e29b15921876da36f9a  ",
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		players := []statsapi.StatsPlayer{
			makePlayer("EpicUser", "Epic|34A02CF8F4414E29B15921876DA36F9A|0", 1),
			makePlayer("Opponent", "Steam|76561198000000002|0", 0),
		}

		if err := tracker.OnUpdateState(ctx, "m2", 11, players); err != nil {
			t.Fatalf("OnUpdateState failed: %v", err)
		}

		match := tracker.GetCurrentMatch()
		if match == nil || match.LocalPlayer == nil {
			t.Fatalf("expected LocalPlayer resolved; got nil")
		}
		if match.LocalTeam == nil || *match.LocalTeam != 1 {
			t.Errorf("expected LocalTeam 1, got %v", match.LocalTeam)
		}
	})

	t.Run("Tier1_PrefixMatchWithoutSplitscreen", func(t *testing.T) {
		store := newSQLiteTestStore(t)
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|76561198000000001",
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		players := []statsapi.StatsPlayer{
			makePlayer("PrefixUser", "Steam|76561198000000001|0", 0),
			makePlayer("Opponent", "Steam|76561198000000002|0", 1),
		}

		if err := tracker.OnUpdateState(ctx, "m-prefix", 11, players); err != nil {
			t.Fatalf("OnUpdateState failed: %v", err)
		}

		match := tracker.GetCurrentMatch()
		if match.LocalPlayer == nil || match.LocalPlayer.PlayerID != "Steam|76561198000000001|0" {
			t.Errorf("expected prefix match resolved; got %+v", match.LocalPlayer)
		}
	})

	t.Run("Tier2_PrimaryAuthEpicAccountID", func(t *testing.T) {
		store := newSQLiteTestStore(t)
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{Enabled: true}
		authCfg := config.AuthConfig{
			Provider: "epic",
			Epic: config.EpicConfig{
				AccountID: "epic-auth-uuid-1234",
			},
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, authCfg)
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		players := []statsapi.StatsPlayer{
			makePlayer("P1", "Epic|epic-auth-uuid-1234|0", 0),
			makePlayer("P2", "Steam|76561198000000002|0", 1),
		}

		if err := tracker.OnUpdateState(ctx, "m3", 11, players); err != nil {
			t.Fatalf("OnUpdateState failed: %v", err)
		}

		match := tracker.GetCurrentMatch()
		if match == nil || match.LocalPlayer == nil {
			t.Fatalf("expected LocalPlayer resolved via Tier 2; got nil")
		}
		if match.LocalPlayer.Name != "P1" {
			t.Errorf("expected LocalPlayer P1, got %q", match.LocalPlayer.Name)
		}
	})

	t.Run("Tier2_PrimaryAuthSteamID64", func(t *testing.T) {
		store := newSQLiteTestStore(t)
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{Enabled: true}
		authCfg := config.AuthConfig{
			Provider: "steam",
			Steam: config.SteamConfig{
				SteamID64: "76561198099999999",
			},
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, authCfg)
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		players := []statsapi.StatsPlayer{
			makePlayer("SteamP", "Steam|76561198099999999|0", 1),
			makePlayer("P2", "Steam|76561198000000002|0", 0),
		}

		if err := tracker.OnUpdateState(ctx, "m4", 11, players); err != nil {
			t.Fatalf("OnUpdateState failed: %v", err)
		}

		match := tracker.GetCurrentMatch()
		if match == nil || match.LocalPlayer == nil {
			t.Fatalf("expected LocalPlayer resolved via Tier 2; got nil")
		}
		if match.LocalTeam == nil || *match.LocalTeam != 1 {
			t.Errorf("expected LocalTeam 1, got %v", match.LocalTeam)
		}
	})

	t.Run("Tier2_DynamicTokenInfoFallback", func(t *testing.T) {
		store := newSQLiteTestStore(t)
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{Enabled: true}
		authCfg := config.AuthConfig{Provider: "epic"}
		mockAuth := newMockAuthProvider("epic", &auth.TokenInfo{
			EpicAccountID: "dynamic-epic-uuid",
		})
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, authCfg,
			playertrack.WithTrackerAuthProvider(mockAuth),
		)
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		players := []statsapi.StatsPlayer{
			makePlayer("DynUser", "Epic|dynamic-epic-uuid|0", 0),
			makePlayer("Opponent", "Steam|76561198000000002|0", 1),
		}

		if err := tracker.OnUpdateState(ctx, "m-dyn", 11, players); err != nil {
			t.Fatalf("OnUpdateState failed: %v", err)
		}

		match := tracker.GetCurrentMatch()
		if match.LocalPlayer == nil || match.LocalPlayer.Name != "DynUser" {
			t.Errorf("expected dynamic TokenInfo resolution; got %+v", match.LocalPlayer)
		}
	})

	t.Run("Tier2_StoreGetAuthStateFallback", func(t *testing.T) {
		store := newSQLiteTestStore(t)
		_ = store.SaveAuthState(ctx, "epic", "refresh", "stored-epic-uuid", "StoredName")

		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{Enabled: true}
		authCfg := config.AuthConfig{Provider: "epic"} // Empty AccountID in config
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, authCfg)
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		players := []statsapi.StatsPlayer{
			makePlayer("StoredUser", "Epic|stored-epic-uuid|0", 1),
			makePlayer("Opponent", "Steam|76561198000000002|0", 0),
		}

		if err := tracker.OnUpdateState(ctx, "m-store-auth", 11, players); err != nil {
			t.Fatalf("OnUpdateState failed: %v", err)
		}

		match := tracker.GetCurrentMatch()
		if match.LocalPlayer == nil || match.LocalPlayer.Name != "StoredUser" {
			t.Errorf("expected store GetAuthState fallback resolution; got %+v", match.LocalPlayer)
		}
	})

	t.Run("Tier3_ConfigLocalPlayerName", func(t *testing.T) {
		store := newSQLiteTestStore(t)
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:         true,
			LocalPlayerName: "  RocketStriker  ",
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		players := []statsapi.StatsPlayer{
			makePlayer("RocketStriker", "Epic|unknown-id|0", 0),
			makePlayer("P2", "Steam|76561198000000002|0", 1),
		}

		if err := tracker.OnUpdateState(ctx, "m5", 11, players); err != nil {
			t.Fatalf("OnUpdateState failed: %v", err)
		}

		match := tracker.GetCurrentMatch()
		if match == nil || match.LocalPlayer == nil {
			t.Fatalf("expected LocalPlayer resolved via Tier 3; got nil")
		}
		if match.LocalPlayer.Name != "RocketStriker" {
			t.Errorf("expected LocalPlayer RocketStriker, got %q", match.LocalPlayer.Name)
		}
	})

	t.Run("Tier4_AuthDisplayName", func(t *testing.T) {
		store := newSQLiteTestStore(t)
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{Enabled: true}
		authCfg := config.AuthConfig{
			Provider: "epic",
			Epic: config.EpicConfig{
				DisplayName: "LegendaryGamer",
			},
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, authCfg)
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		players := []statsapi.StatsPlayer{
			makePlayer("LegendaryGamer", "Epic|some-id|0", 1),
			makePlayer("P2", "Steam|76561198000000002|0", 0),
		}

		if err := tracker.OnUpdateState(ctx, "m6", 11, players); err != nil {
			t.Fatalf("OnUpdateState failed: %v", err)
		}

		match := tracker.GetCurrentMatch()
		if match == nil || match.LocalPlayer == nil {
			t.Fatalf("expected LocalPlayer resolved via Tier 4; got nil")
		}
		if match.LocalTeam == nil || *match.LocalTeam != 1 {
			t.Errorf("expected LocalTeam 1, got %v", match.LocalTeam)
		}
	})

	t.Run("Precedence_Tier1_Overrides_Tier2_And_Tier3", func(t *testing.T) {
		store := newSQLiteTestStore(t)
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:         true,
			LocalPlayerID:   "Steam|111|0",
			LocalPlayerName: "NameOfPlayer222",
		}
		authCfg := config.AuthConfig{
			Provider: "epic",
			Epic:     config.EpicConfig{AccountID: "222"},
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, authCfg)
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		players := []statsapi.StatsPlayer{
			makePlayer("NameOfPlayer111", "Steam|111|0", 0),
			makePlayer("NameOfPlayer222", "Epic|222|0", 1),
		}

		if err := tracker.OnUpdateState(ctx, "m-prec", 11, players); err != nil {
			t.Fatalf("OnUpdateState failed: %v", err)
		}

		match := tracker.GetCurrentMatch()
		if match.LocalPlayer.PlayerID != "Steam|111|0" {
			t.Errorf("expected Tier 1 to win precedence; got %q", match.LocalPlayer.PlayerID)
		}
		if *match.LocalTeam != 0 {
			t.Errorf("expected LocalTeam 0, got %d", *match.LocalTeam)
		}
	})

	t.Run("BotImmunity_BotNeverSelectedAsLocalPlayer", func(t *testing.T) {
		store := newSQLiteTestStore(t)
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:         true,
			LocalPlayerName: "Saltie",
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		players := []statsapi.StatsPlayer{
			makePlayer("Saltie", "Unknown|0|0", 0), // AI Bot
			makePlayer("Human1", "Steam|999|0", 1),
		}

		if err := tracker.OnUpdateState(ctx, "m-bot", 11, players); err != nil {
			t.Fatalf("OnUpdateState failed: %v", err)
		}

		match := tracker.GetCurrentMatch()
		if match.LocalPlayer != nil {
			t.Fatalf("bot was erroneously selected as LocalPlayer: %+v", match.LocalPlayer)
		}
	})

	t.Run("MissingLocalPlayer_SpectatorFallback", func(t *testing.T) {
		store := newSQLiteTestStore(t)
		fetcher := newTestSkillFetcher()
		cfg := config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|NonExistent|0",
		}
		tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
		if err != nil {
			t.Fatalf("NewTracker failed: %v", err)
		}
		defer func() { _ = tracker.Close() }()

		players := []statsapi.StatsPlayer{
			makePlayer("P1", "Steam|1|0", 0),
			makePlayer("P2", "Steam|2|0", 1),
		}

		if err := tracker.OnUpdateState(ctx, "m-spec", 11, players); err != nil {
			t.Fatalf("OnUpdateState failed: %v", err)
		}

		match := tracker.GetCurrentMatch()
		if match.LocalPlayer != nil {
			t.Errorf("expected nil LocalPlayer; got %+v", match.LocalPlayer)
		}
		if match.LocalTeam != nil {
			t.Errorf("expected nil LocalTeam; got %v", match.LocalTeam)
		}

		winner := 0
		if err := tracker.OnMatchEnded(ctx, "m-spec", &winner); err != nil {
			t.Errorf("OnMatchEnded returned error for unresolved local player: %v", err)
		}
	})
}

// ============================================================================
// Test Suite 2: Lobby Player Classification & Bot Exclusion Tests
// ============================================================================
