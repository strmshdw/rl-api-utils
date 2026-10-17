package playertrack_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/session"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rlapi"
)

type dynamicSkillFetcher struct {
	mu        sync.Mutex
	skillsMap map[rlapi.PlayerID][]rlapi.Skill
	enabled   bool
}

func newDynamicSkillFetcher() *dynamicSkillFetcher {
	return &dynamicSkillFetcher{
		skillsMap: make(map[rlapi.PlayerID][]rlapi.Skill),
		enabled:   true,
	}
}

func (d *dynamicSkillFetcher) setPlayerRating(playerID string, playlistID int, rating float64, tier, div int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	pid := rlapi.PlayerID(playerID)
	d.skillsMap[pid] = []rlapi.Skill{
		{
			Playlist:      playlistID,
			MMR:           rating,
			Tier:          tier,
			Division:      div,
			MatchesPlayed: 50,
		},
	}
}

func (d *dynamicSkillFetcher) GetPlayersSkills(ctx context.Context, playerIDs []rlapi.PlayerID) ([]rlapi.PlayerWithSkills, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	res := make([]rlapi.PlayerWithSkills, 0, len(playerIDs))
	for _, id := range playerIDs {
		skills := d.skillsMap[id]
		res = append(res, rlapi.PlayerWithSkills{
			PlayerID: id,
			Skills:   skills,
		})
	}
	return res, nil
}

func (d *dynamicSkillFetcher) IsEnabled() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.enabled
}

func (d *dynamicSkillFetcher) Close() error {
	return nil
}

// TestM3_RapidSuccessiveMatchConclusions_InvalidateRankCache_NonZeroMMRDelta
// tests that multiple rapid matches concluding in sequence properly invalidate
// the local player's rank cache and compute accurate non-zero MMR deltas.
func TestM3_RapidSuccessiveMatchConclusions_InvalidateRankCache_NonZeroMMRDelta(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store, err := storage.NewJSONStore(t.TempDir() + "/test_state.json")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	skillFetcher := newDynamicSkillFetcher()
	localPID := "Steam|76561198000000001|0"
	const activePlaylist = 13

	// Initial MMR: rating 45.0 -> round(45*20 + 100) = 1000
	skillFetcher.setPlayerRating(localPID, activePlaylist, 45.0, 16, 2)

	trackingCfg := config.PlayerTrackingConfig{
		Enabled:        true,
		AutoFetchRanks: true,
		LocalPlayerID:  localPID,
	}
	authCfg := config.AuthConfig{
		Provider: "steam",
		Steam: config.SteamConfig{
			SteamID64: "76561198000000001",
		},
	}

	tracker, err := playertrack.NewTracker(store, skillFetcher, trackingCfg, authCfg)
	if err != nil {
		t.Fatalf("failed to create tracker: %v", err)
	}
	defer tracker.Close()

	sessionTracker := session.NewSessionTracker()
	tracker.SetMatchStateListener(sessionTracker)

	// Step 1: Match 1 Starts
	match1GUID := "rapid-match-001"
	playersMatch1 := []statsapi.StatsPlayer{
		{Name: "Hero", PrimaryId: localPID, TeamNum: 0},
		{Name: "Opponent1", PrimaryId: "Epic|opp1|0", TeamNum: 1},
	}
	game1 := &statsapi.StatsGame{PlaylistId: activePlaylist, TimeRemaining: 300}
	if err := tracker.OnUpdateGameState(ctx, match1GUID, activePlaylist, playersMatch1, game1); err != nil {
		t.Fatalf("match 1 OnUpdateGameState failed: %v", err)
	}

	// Allow rank fetch to populate
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		m := tracker.GetCurrentMatch()
		if m != nil && m.LocalPlayer != nil && m.LocalPlayer.CurrentRank != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	m1Before := tracker.GetCurrentMatch()
	if m1Before == nil || m1Before.LocalPlayer == nil || m1Before.LocalPlayer.CurrentRank == nil {
		t.Fatal("expected local player rank to be hydrated for match 1")
	}
	if m1Before.LocalPlayer.CurrentRank.MMR != 1000 {
		t.Fatalf("expected initial MMR 1000, got %v", m1Before.LocalPlayer.CurrentRank.MMR)
	}

	// Match 1 ends: Local player WON, rating goes up to 45.75 (1015 MMR)
	skillFetcher.setPlayerRating(localPID, activePlaylist, 45.75, 16, 3)
	winner0 := 0
	if err := tracker.OnMatchEnded(ctx, match1GUID, &winner0); err != nil {
		t.Fatalf("match 1 OnMatchEnded failed: %v", err)
	}

	// Verify SessionTracker recorded non-zero MMR delta for Match 1
	summary1 := sessionTracker.GetSessionSummary()
	if len(summary1.Matches) != 1 {
		t.Fatalf("expected 1 completed match, got %d", len(summary1.Matches))
	}
	concludedM1 := summary1.Matches[0]
	if concludedM1.StartingMMR != 1000 {
		t.Errorf("match 1: expected StartingMMR 1000, got %v", concludedM1.StartingMMR)
	}
	if concludedM1.EndingMMR != 1015 {
		t.Errorf("match 1: expected EndingMMR 1015, got %v", concludedM1.EndingMMR)
	}
	if concludedM1.MMRChange != 15.00 {
		t.Errorf("match 1: expected non-zero MMRChange +15.00, got %v", concludedM1.MMRChange)
	}

	// Step 2: Rapid Match 2 Starts immediately
	match2GUID := "rapid-match-002"
	playersMatch2 := []statsapi.StatsPlayer{
		{Name: "Hero", PrimaryId: localPID, TeamNum: 0},
		{Name: "Opponent2", PrimaryId: "Epic|opp2|0", TeamNum: 1},
	}
	game2 := &statsapi.StatsGame{PlaylistId: activePlaylist, TimeRemaining: 300}
	if err := tracker.OnUpdateGameState(ctx, match2GUID, activePlaylist, playersMatch2, game2); err != nil {
		t.Fatalf("match 2 OnUpdateGameState failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Match 2 ends: Local player LOST, rating goes down to 45.10 (1002 MMR)
	skillFetcher.setPlayerRating(localPID, activePlaylist, 45.10, 16, 2)
	winner1 := 1
	if err := tracker.OnMatchEnded(ctx, match2GUID, &winner1); err != nil {
		t.Fatalf("match 2 OnMatchEnded failed: %v", err)
	}

	// Verify SessionTracker recorded non-zero MMR delta for Match 2
	summary2 := sessionTracker.GetSessionSummary()
	if len(summary2.Matches) != 2 {
		t.Fatalf("expected 2 completed matches, got %d", len(summary2.Matches))
	}
	concludedM2 := summary2.Matches[1]
	if concludedM2.StartingMMR != 1015 {
		t.Errorf("match 2: expected StartingMMR 1015, got %v", concludedM2.StartingMMR)
	}
	if concludedM2.EndingMMR != 1002 {
		t.Errorf("match 2: expected EndingMMR 1002, got %v", concludedM2.EndingMMR)
	}
	if concludedM2.MMRChange != -13.00 {
		t.Errorf("match 2: expected non-zero MMRChange -13.00, got %v", concludedM2.MMRChange)
	}

	// Step 3: Rapid Match 3 Starts immediately
	match3GUID := "rapid-match-003"
	playersMatch3 := []statsapi.StatsPlayer{
		{Name: "Hero", PrimaryId: localPID, TeamNum: 0},
		{Name: "Opponent3", PrimaryId: "Epic|opp3|0", TeamNum: 1},
	}
	game3 := &statsapi.StatsGame{PlaylistId: activePlaylist, TimeRemaining: 300}
	if err := tracker.OnUpdateGameState(ctx, match3GUID, activePlaylist, playersMatch3, game3); err != nil {
		t.Fatalf("match 3 OnUpdateGameState failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Match 3 ends: Local player WON, rating goes to 46.00 (1020 MMR)
	skillFetcher.setPlayerRating(localPID, activePlaylist, 46.00, 16, 3)
	if err := tracker.OnMatchEnded(ctx, match3GUID, &winner0); err != nil {
		t.Fatalf("match 3 OnMatchEnded failed: %v", err)
	}

	// Verify SessionTracker recorded non-zero MMR delta for Match 3
	summary3 := sessionTracker.GetSessionSummary()
	if len(summary3.Matches) != 3 {
		t.Fatalf("expected 3 completed matches, got %d", len(summary3.Matches))
	}
	concludedM3 := summary3.Matches[2]
	if concludedM3.StartingMMR != 1002 {
		t.Errorf("match 3: expected StartingMMR 1002, got %v", concludedM3.StartingMMR)
	}
	if concludedM3.EndingMMR != 1020 {
		t.Errorf("match 3: expected EndingMMR 1020, got %v", concludedM3.EndingMMR)
	}
	if concludedM3.MMRChange != 18.00 {
		t.Errorf("match 3: expected non-zero MMRChange +18.00, got %v", concludedM3.MMRChange)
	}

	// Check playlist stats MMR delta
	plStats, ok := summary3.Playlists["13"]
	if !ok {
		t.Fatal("expected playlist stats for playlist 13")
	}
	if plStats.InitialMMR != 1000 {
		t.Errorf("expected playlist InitialMMR 1000, got %v", plStats.InitialMMR)
	}
	if plStats.CurrentMMR != 1020 {
		t.Errorf("expected playlist CurrentMMR 1020, got %v", plStats.CurrentMMR)
	}
	if plStats.MMRDelta != 20.00 {
		t.Errorf("expected playlist MMRDelta +20.00, got %v", plStats.MMRDelta)
	}
}
