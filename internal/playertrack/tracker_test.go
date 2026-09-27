package playertrack_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/auth"
	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rlapi"
)

// ============================================================================
// Test Doubles & Helpers
// ============================================================================

type testSkillFetcher struct {
	mu           sync.Mutex
	skillsMap    map[rlapi.PlayerID][]rlapi.Skill
	delay        time.Duration
	err          error
	calls        [][]rlapi.PlayerID
	callNotifyCh chan struct{}
	doneNotifyCh chan struct{}
	enabled      bool
	closed       bool
}

func newTestSkillFetcher() *testSkillFetcher {
	return &testSkillFetcher{
		skillsMap:    make(map[rlapi.PlayerID][]rlapi.Skill),
		callNotifyCh: make(chan struct{}, 1000),
		doneNotifyCh: make(chan struct{}, 1000),
		enabled:      true,
	}
}

func (m *testSkillFetcher) GetPlayersSkills(ctx context.Context, playerIDs []rlapi.PlayerID) ([]rlapi.PlayerWithSkills, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, playertrack.ErrRankClientClosed
	}
	m.calls = append(m.calls, playerIDs)
	delay := m.delay
	errResp := m.err
	m.mu.Unlock()

	select {
	case m.callNotifyCh <- struct{}{}:
	default:
	}

	if delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}

	defer func() {
		select {
		case m.doneNotifyCh <- struct{}{}:
		default:
		}
	}()

	if errResp != nil {
		return nil, errResp
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	var res []rlapi.PlayerWithSkills
	for _, pid := range playerIDs {
		if skills, ok := m.skillsMap[pid]; ok {
			res = append(res, rlapi.PlayerWithSkills{
				PlayerID: pid,
				Skills:   skills,
			})
		}
	}
	return res, nil
}

func (m *testSkillFetcher) IsEnabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabled && !m.closed
}

func (m *testSkillFetcher) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *testSkillFetcher) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

type mockAuthProvider struct {
	providerName string
	token        *auth.TokenInfo
	authErr      error
}

func newMockAuthProvider(name string, token *auth.TokenInfo) *mockAuthProvider {
	return &mockAuthProvider{providerName: name, token: token}
}

func (m *mockAuthProvider) Name() string                                                               { return m.providerName }
func (m *mockAuthProvider) Authenticate(ctx context.Context) (*auth.TokenInfo, error)                 { return m.token, m.authErr }
func (m *mockAuthProvider) Refresh(ctx context.Context, refreshToken string) (*auth.TokenInfo, error) { return m.token, m.authErr }
func (m *mockAuthProvider) TokenInfo() *auth.TokenInfo                                                 { return m.token }
func (m *mockAuthProvider) Validate() error                                                            { return nil }

type mockFaultStore struct {
	storage.StateStore
	upsertPlayerErr  error
	recordResultsErr error
}

func (m *mockFaultStore) UpsertPlayer(ctx context.Context, player *storage.PlayerRecord) error {
	if m.upsertPlayerErr != nil {
		return m.upsertPlayerErr
	}
	return m.StateStore.UpsertPlayer(ctx, player)
}

func (m *mockFaultStore) RecordMatchResults(ctx context.Context, matchGUID string, playlistID int, outcomes []storage.PlayerOutcome) error {
	if m.recordResultsErr != nil {
		return m.recordResultsErr
	}
	return m.StateStore.RecordMatchResults(ctx, matchGUID, playlistID, outcomes)
}

func newSQLiteTestStore(t *testing.T) storage.StateStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_playertrack.db")
	s, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create test sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newJSONTestStore(t *testing.T) storage.StateStore {
	t.Helper()
	jsonPath := filepath.Join(t.TempDir(), "test_playertrack.json")
	s, err := storage.NewJSONStore(jsonPath)
	if err != nil {
		t.Fatalf("failed to create test json store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func forEachStore(t *testing.T, fn func(t *testing.T, store storage.StateStore)) {
	fixtures := []struct {
		name    string
		factory func(t *testing.T) storage.StateStore
	}{
		{"SQLite", newSQLiteTestStore},
		{"JSONStore", newJSONTestStore},
	}
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			store := f.factory(t)
			fn(t, store)
		})
	}
}

func makePlayer(name, id string, team int) statsapi.StatsPlayer {
	return statsapi.StatsPlayer{
		Name:      name,
		PrimaryId: id,
		TeamNum:   team,
		Score:     100,
		Goals:     1,
		Assists:   0,
		Saves:     1,
		Shots:     2,
		Demos:     0,
	}
}

// ============================================================================
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

func TestTracker_ClassificationAndBotExclusion(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()
		fetcher := newTestSkillFetcher()
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

		players := []statsapi.StatsPlayer{
			makePlayer("LocalUser", "Steam|local|0", 0),
			makePlayer("Teammate1", "Steam|tm1|0", 0),
			makePlayer("BotBlue", "Unknown|0|0", 0),
			makePlayer("Opponent1", "Epic|opp1|0", 1),
			makePlayer("BotOrange", "Unknown|0|0", 1),
			makePlayer("Spectator1", "Steam|spec1|0", 255),
		}

		if err := tracker.OnUpdateState(ctx, "m-class", 11, players); err != nil {
			t.Fatalf("OnUpdateState failed: %v", err)
		}

		match := tracker.GetCurrentMatch()
		if len(match.Teammates) != 2 { // Teammate1 (human) + BotBlue (bot)
			t.Fatalf("expected 2 teammates on Blue; got %d", len(match.Teammates))
		}
		if len(match.Opponents) != 2 { // Opponent1 (human) + BotOrange (bot)
			t.Fatalf("expected 2 opponents on Orange; got %d", len(match.Opponents))
		}
		if len(match.Spectators) != 1 || match.Spectators[0].PlayerID != "Steam|spec1|0" {
			t.Errorf("expected 1 spectator spec1; got %+v", match.Spectators)
		}

		// Verify bots are excluded from storage.UpsertPlayer
		if _, err := store.GetPlayer(ctx, "Unknown|0|0"); !errors.Is(err, storage.ErrPlayerNotFound) {
			t.Errorf("expected ErrPlayerNotFound for bot; got err = %v", err)
		}
		// Verify human players are stored
		if p, err := store.GetPlayer(ctx, "Steam|tm1|0"); err != nil || p == nil {
			t.Errorf("expected human teammate in store: %v", err)
		}
		if p, err := store.GetPlayer(ctx, "Epic|opp1|0"); err != nil || p == nil {
			t.Errorf("expected human opponent in store: %v", err)
		}

		// Verify bots excluded from rank queries
		time.Sleep(20 * time.Millisecond) // Let background worker dispatch
		for _, call := range fetcher.calls {
			for _, pid := range call {
				if string(pid) == "Unknown|0|0" {
					t.Errorf("bot ID Unknown|0|0 was dispatched for rank query!")
				}
			}
		}

		// Conclude match: Blue (team 0) wins
		winTeam := 0
		if err := tracker.OnMatchEnded(ctx, "m-class", &winTeam); err != nil {
			t.Fatalf("OnMatchEnded failed: %v", err)
		}

		// Verify matchup increments in database:
		// Teammate1: wins_as_teammate = 1, losses_as_teammate = 0
		tmMatchup, err := store.GetPlayerMatchup(ctx, "Steam|tm1|0", 11)
		if err != nil {
			t.Fatalf("GetPlayerMatchup teammate failed: %v", err)
		}
		if tmMatchup.WinsAsTeammate != 1 || tmMatchup.LossesAsTeammate != 0 {
			t.Errorf("teammate matchup mismatch: %+v", tmMatchup)
		}

		// Opponent1: wins_as_opponent = 1, losses_as_opponent = 0
		oppMatchup, err := store.GetPlayerMatchup(ctx, "Epic|opp1|0", 11)
		if err != nil {
			t.Fatalf("GetPlayerMatchup opponent failed: %v", err)
		}
		if oppMatchup.WinsAsOpponent != 1 || oppMatchup.LossesAsOpponent != 0 {
			t.Errorf("opponent matchup mismatch: %+v", oppMatchup)
		}

		// Spectator1: excluded from outcomes
		specMatchup, err := store.GetPlayerMatchup(ctx, "Steam|spec1|0", 11)
		if err != nil {
			t.Fatalf("GetPlayerMatchup spectator failed: %v", err)
		}
		if specMatchup.TotalMatches != 0 {
			t.Errorf("spectator should have 0 total matches; got %d", specMatchup.TotalMatches)
		}
	})
}

func TestTracker_SplitscreenTeammate(t *testing.T) {
	forEachStore(t, func(t *testing.T, store storage.StateStore) {
		ctx := context.Background()
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
			makePlayer("MainPlayer", "Steam|76561198000000001|0", 0),
			makePlayer("GuestPlayer", "Steam|76561198000000001|1", 0), // Splitscreen index 1
			makePlayer("Rival", "Epic|rival|0", 1),
		}

		if err := tracker.OnUpdateState(ctx, "m-split", 11, players); err != nil {
			t.Fatalf("OnUpdateState failed: %v", err)
		}

		match := tracker.GetCurrentMatch()
		if len(match.Teammates) != 1 || match.Teammates[0].PlayerID != "Steam|76561198000000001|1" {
			t.Fatalf("splitscreen guest not categorized as teammate: %+v", match.Teammates)
		}

		win := 0
		if err := tracker.OnMatchEnded(ctx, "m-split", &win); err != nil {
			t.Fatalf("OnMatchEnded failed: %v", err)
		}

		guestMatchup, err := store.GetPlayerMatchup(ctx, "Steam|76561198000000001|1", 11)
		if err != nil {
			t.Fatalf("GetPlayerMatchup guest failed: %v", err)
		}
		if guestMatchup.WinsAsTeammate != 1 {
			t.Errorf("splitscreen teammate should have 1 win_as_teammate; got %d", guestMatchup.WinsAsTeammate)
		}
	})
}

// ============================================================================
// Test Suite 3: Match Lifecycle Transitions & Outcomes
// ============================================================================

func TestTracker_Lifecycle_WinAndLoss(t *testing.T) {
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

		// Game 1: Victory (Team 0 wins, Local is Team 0)
		playersG1 := []statsapi.StatsPlayer{
			makePlayer("LocalUser", "Steam|local|0", 0),
			makePlayer("Buddy", "Steam|buddy|0", 0),
			makePlayer("Rival", "Steam|rival|0", 1),
		}
		_ = tracker.OnUpdateState(ctx, "m-g1", 11, playersG1)
		win0 := 0
		_ = tracker.OnMatchEnded(ctx, "m-g1", &win0)

		// Inspect post-match snapshot
		snapG1 := tracker.GetCurrentMatch()
		if snapG1.ActiveMatch != false {
			t.Errorf("expected ActiveMatch=false after match end; got true")
		}
		if snapG1.MatchEnded != true {
			t.Errorf("expected MatchEnded=true after match end; got false")
		}
		if snapG1.Result != "victory" {
			t.Errorf("expected Result='victory'; got %q", snapG1.Result)
		}

		// Game 2: Defeat (Team 1 wins, Local is Team 0)
		playersG2 := []statsapi.StatsPlayer{
			makePlayer("LocalUser", "Steam|local|0", 0),
			makePlayer("Buddy", "Steam|buddy|0", 0),
			makePlayer("Rival", "Steam|rival|0", 1),
		}
		_ = tracker.OnUpdateState(ctx, "m-g2", 11, playersG2)

		snapG2Active := tracker.GetCurrentMatch()
		if snapG2Active.ActiveMatch != true || snapG2Active.MatchEnded != false {
			t.Errorf("new match should reset ActiveMatch=true, MatchEnded=false")
		}

		win1 := 1
		_ = tracker.OnMatchEnded(ctx, "m-g2", &win1)

		snapG2End := tracker.GetCurrentMatch()
		if snapG2End.Result != "defeat" {
			t.Errorf("expected Result='defeat'; got %q", snapG2End.Result)
		}

		// Verify aggregate matchups
		buddy, _ := store.GetPlayerMatchup(ctx, "Steam|buddy|0", 11)
		if buddy.WinsAsTeammate != 1 || buddy.LossesAsTeammate != 1 || buddy.TotalMatches != 2 {
			t.Errorf("unexpected buddy record: %+v", buddy)
		}

		rival, _ := store.GetPlayerMatchup(ctx, "Steam|rival|0", 11)
		if rival.WinsAsOpponent != 1 || rival.LossesAsOpponent != 1 || rival.TotalMatches != 2 {
			t.Errorf("unexpected rival record: %+v", rival)
		}
	})
}

func TestTracker_PlaylistIsolation(t *testing.T) {
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

		// Play match in playlist 11 (2v2)
		players := []statsapi.StatsPlayer{
			makePlayer("Local", "Steam|local|0", 0),
			makePlayer("Opponent", "Steam|opp|0", 1),
		}
		_ = tracker.OnUpdateState(ctx, "m-pl11", 11, players)
		win0 := 0
		_ = tracker.OnMatchEnded(ctx, "m-pl11", &win0)

		// Play match in playlist 13 (3v3)
		_ = tracker.OnUpdateState(ctx, "m-pl13", 13, players)
		win1 := 1
		_ = tracker.OnMatchEnded(ctx, "m-pl13", &win1)

		m11, _ := store.GetPlayerMatchup(ctx, "Steam|opp|0", 11)
		if m11.WinsAsOpponent != 1 || m11.LossesAsOpponent != 0 || m11.TotalMatches != 1 {
			t.Errorf("playlist 11 record mismatch: %+v", m11)
		}

		m13, _ := store.GetPlayerMatchup(ctx, "Steam|opp|0", 13)
		if m13.WinsAsOpponent != 0 || m13.LossesAsOpponent != 1 || m13.TotalMatches != 1 {
			t.Errorf("playlist 13 record mismatch: %+v", m13)
		}
	})
}

// ============================================================================
// Test Suite 4: Debounce & Rank Fetching Engine
// ============================================================================

func TestTracker_Debounce_1000UpdateState_SingleRPC(t *testing.T) {
	ctx := context.Background()
	store := newSQLiteTestStore(t)
	fetcher := newTestSkillFetcher()
	fetcher.skillsMap["Steam|p1|0"] = []rlapi.Skill{
		{Playlist: 11, Tier: 16, Division: 3, MMR: 1150.5},
	}

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

	players := []statsapi.StatsPlayer{
		makePlayer("Local", "Steam|local|0", 0),
		makePlayer("P1", "Steam|p1|0", 1),
	}

	// Hammer 1,000 OnUpdateState events (simulating high-frequency 120Hz frames)
	for i := 0; i < 1000; i++ {
		players[0].Score = i
		if err := tracker.OnUpdateState(ctx, "m-debounce", 11, players); err != nil {
			t.Fatalf("OnUpdateState %d failed: %v", i, err)
		}
	}

	// Await rank fetch completion
	select {
	case <-fetcher.doneNotifyCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for rank fetch completion")
	}

	// Must be exactly 1 batch call to PsyNet RPC!
	if count := fetcher.CallCount(); count != 1 {
		t.Errorf("expected exactly 1 RPC call to PsyNet; got %d calls (debounce failure!)", count)
	}

	// Verify ranks successfully populated in snapshot
	snap := tracker.GetCurrentMatch()
	if len(snap.Opponents) != 1 || snap.Opponents[0].CurrentRank == nil {
		t.Fatalf("expected CurrentRank populated in snapshot: %+v", snap.Opponents)
	}
	if snap.Opponents[0].CurrentRank.RankName != "Champion I Division IV" {
		t.Errorf("expected rank 'Champion I Division IV'; got %q", snap.Opponents[0].CurrentRank.RankName)
	}
}

func TestTracker_RankFetch_ErrorBackoff(t *testing.T) {
	ctx := context.Background()
	store := newSQLiteTestStore(t)
	fetcher := newTestSkillFetcher()
	fetcher.err = errors.New("psynet 500 internal server error")

	cfg := config.PlayerTrackingConfig{
		Enabled:        true,
		LocalPlayerID:  "Steam|local|0",
		AutoFetchRanks: true,
	}
	tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{},
		playertrack.WithBackoffTTL(60*time.Second),
	)
	if err != nil {
		t.Fatalf("NewTracker failed: %v", err)
	}
	defer func() { _ = tracker.Close() }()

	players := []statsapi.StatsPlayer{
		makePlayer("Local", "Steam|local|0", 0),
		makePlayer("Rival", "Steam|rival|0", 1),
	}

	// First tick triggers query that fails
	_ = tracker.OnUpdateState(ctx, "m-err", 11, players)

	select {
	case <-fetcher.doneNotifyCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for initial rank fetch")
	}

	// Call 500 subsequent ticks within backoff window
	for i := 0; i < 500; i++ {
		_ = tracker.OnUpdateState(ctx, "m-err", 11, players)
	}

	// RPC call count must remain 1 (negative cache backoff suppresses retries)
	if count := fetcher.CallCount(); count != 1 {
		t.Errorf("failure backoff failed to suppress retries; got %d calls", count)
	}
}

func TestTracker_RankPersist_FallbackUpsertPlayer(t *testing.T) {
	ctx := context.Background()
	store := newSQLiteTestStore(t)
	fetcher := newTestSkillFetcher()
	fetcher.skillsMap["Steam|fresh|0"] = []rlapi.Skill{
		{Playlist: 11, Tier: 17, Division: 0, MMR: 1200.0},
	}

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

	players := []statsapi.StatsPlayer{
		makePlayer("Local", "Steam|local|0", 0),
		makePlayer("FreshPlayer", "Steam|fresh|0", 1),
	}

	if err := tracker.OnUpdateState(ctx, "m-fallback", 11, players); err != nil {
		t.Fatalf("OnUpdateState failed: %v", err)
	}

	select {
	case <-fetcher.doneNotifyCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for rank fetch")
	}

	// Allow async DB persist to complete
	time.Sleep(100 * time.Millisecond)

	rec, err := store.GetPlayer(ctx, "Steam|fresh|0")
	if err != nil {
		t.Fatalf("GetPlayer failed: %v", err)
	}
	if rec.RanksJSON == "" || rec.RanksJSON == "{}" {
		t.Errorf("expected ranks_json populated in store; got %q", rec.RanksJSON)
	}
}

// ============================================================================
// Test Suite 5: Idempotency & Edge Cases
// ============================================================================

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

