package playertrack_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rlapi"
)

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
		for _, call := range fetcher.Calls() {
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
