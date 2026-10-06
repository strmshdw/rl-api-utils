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

func makePlayerWithStats(name, id string, team int, score, goals, assists, saves, shots, demos int) statsapi.StatsPlayer {
	return statsapi.StatsPlayer{
		Name:      name,
		PrimaryId: id,
		TeamNum:   team,
		Score:     score,
		Goals:     goals,
		Assists:   assists,
		Saves:     saves,
		Shots:     shots,
		Demos:     demos,
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

// ============================================================================
// Test Suite 4: Mid-Game Disconnect & Player State Retention (Requirement R2)
// ============================================================================

// TestTracker_MidGameDisconnect_Lifecycle_TeammateAndLocal covers the primary 4-frame lifecycle:
// Frame 1: Full lobby with active players accumulating stats.
// Frame 2: Teammate leaves early -> retained in active state with stats preserved and IsDisconnected=true.
// Frame 3: Disconnected teammate reconnects -> stats updated, IsDisconnected=false, NO duplicate entries.
// Frame 4: Local player leaves early -> local player and local team preserved, stats retained.
// Conclude: Match ends -> outcomes recorded in store because local player and team were preserved.
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
func TestTracker_MidGameDisconnect_BotReplacement(t *testing.T) {
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

		matchGUID := "m-bot-replace-1"
		playlistID := 1 // Casual 3v3

		// Frame 1: Human teammate active
		playersF1 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 100, 1, 0, 0, 1, 0),
			makePlayerWithStats("HumanTm", "Steam|human_tm|0", 0, 180, 1, 1, 0, 2, 1),
			makePlayerWithStats("Opponent", "Steam|opp|0", 1, 150, 0, 1, 1, 1, 0),
		}
		_ = tracker.OnUpdateState(ctx, matchGUID, playlistID, playersF1)

		// Frame 2: HumanTm disconnects, AI bot "Tex" replaces them on Team 0
		playersF2 := []statsapi.StatsPlayer{
			makePlayerWithStats("LocalUser", "Steam|local_user|0", 0, 120, 1, 0, 1, 2, 0),
			makePlayerWithStats("Tex", "Unknown|0|0", 0, 20, 0, 0, 0, 1, 0), // AI Bot
			makePlayerWithStats("Opponent", "Steam|opp|0", 1, 160, 0, 1, 1, 1, 0),
		}
		_ = tracker.OnUpdateState(ctx, matchGUID, playlistID, playersF2)

		snapF2 := tracker.GetCurrentMatch()
		if len(snapF2.Teammates) != 2 {
			t.Fatalf("expected 2 teammates (retained human + bot), got %d", len(snapF2.Teammates))
		}

		var humanFound, botFound bool
		for _, tm := range snapF2.Teammates {
			if tm.PlayerID == "Steam|human_tm|0" {
				humanFound = true
				if !tm.IsDisconnected {
					t.Errorf("expected human teammate to be marked IsDisconnected=true")
				}
				if tm.Stats.Score != 180 || tm.Stats.Goals != 1 {
					t.Errorf("human teammate stats were modified: %+v", tm.Stats)
				}
			}
			if tm.IsBot {
				botFound = true
				if tm.IsDisconnected {
					t.Errorf("AI bot should be active, not disconnected")
				}
				if tm.Name != "Tex" {
					t.Errorf("expected bot name 'Tex', got %q", tm.Name)
				}
			}
		}
		if !humanFound {
			t.Errorf("retained human teammate not found in teammates slice")
		}
		if !botFound {
			t.Errorf("AI bot teammate not found in teammates slice")
		}
	})
}


