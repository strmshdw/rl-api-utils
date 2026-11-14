package daemon_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rlapi"
)

func TestDaemon_HTTP_CurrentMatch_NilTracker(t *testing.T) {
	cfg := makeTestConfig(5*time.Minute, false)
	d, err := daemon.New(newMockSyncer(), cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	rec := executeTestRequest(d, http.MethodGet, "/current-match", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("expected application/json, got %s", ct)
	}

	var resp playertrack.CurrentMatchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response JSON: %v", err)
	}

	if resp.ActiveMatch {
		t.Errorf("expected active_match = false, got true")
	}
	if resp.Teammates == nil || resp.Opponents == nil {
		t.Errorf("expected non-nil empty slices for teammates and opponents")
	}
}

// 2. No Active Match: Tracker attached, but no game in progress
func TestDaemon_HTTP_CurrentMatch_NoActiveMatch(t *testing.T) {
	store := newTestStore(t, backendSQLite)
	tracker := newTestTracker(t, store, nil)
	cfg := makeTestConfig(5*time.Minute, false)

	d, err := daemon.New(newMockSyncer(), cfg,
		daemon.WithPlayerTracker(tracker),
		daemon.WithStateStore(store),
	)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	rec := executeTestRequest(d, http.MethodGet, "/current-match", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp playertrack.CurrentMatchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}

	if resp.ActiveMatch {
		t.Errorf("expected active_match = false")
	}
	if resp.MatchGUID != "" {
		t.Errorf("expected empty match_guid, got %q", resp.MatchGUID)
	}
	if len(resp.Teammates) != 0 || len(resp.Opponents) != 0 {
		t.Errorf("expected 0 teammates and 0 opponents")
	}
}

// 3. Active Match In Progress: Complete Lobby Payload
func TestDaemon_HTTP_CurrentMatch_ActiveMatch_CompletePayload(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, backendSQLite)

	// Seed existing head-to-head records
	tmID := "Steam|76561198000000002|0"
	oppID := "Epic|epic_opp_001|0"
	_ = store.RecordMatchResults(ctx, "historical-guid-1", 11, []storage.PlayerOutcome{
		{PlayerID: tmID, Platform: "Steam", PlayerName: "Teammate1", IsTeammate: true, Won: true},
		{PlayerID: oppID, Platform: "Epic", PlayerName: "Opponent1", IsTeammate: false, Won: true},
	})

	// Setup mock rank fetcher
	mockFetcher := playertrack.NewMockSkillFetcher()
	mockFetcher.SkillsMap[rlapi.PlayerID(tmID)] = []rlapi.Skill{
		{Playlist: 11, Tier: 15, Division: 2, MMR: 980.5, MatchesPlayed: 45},
	}
	mockFetcher.SkillsMap[rlapi.PlayerID(oppID)] = []rlapi.Skill{
		{Playlist: 11, Tier: 16, Division: 0, MMR: 1050.2, MatchesPlayed: 120},
	}

	tracker := newTestTracker(t, store, mockFetcher)

	// Ingest UpdateState frame
	localID := "Epic|my_epic_acc_001|0"
	players := []statsapi.StatsPlayer{
		{PrimaryId: localID, Name: "Me", TeamNum: 0, Score: 100},
		{PrimaryId: tmID, Name: "Teammate1", TeamNum: 0, Score: 150, Goals: 1},
		{PrimaryId: oppID, Name: "Opponent1", TeamNum: 1, Score: 80, Saves: 2},
	}
	if err := tracker.OnUpdateState(ctx, "live-guid-999", 11, players); err != nil {
		t.Fatalf("OnUpdateState failed: %v", err)
	}

	// Allow async rank fetch to settle
	for i := 0; i < 50; i++ {
		match := tracker.GetCurrentMatch()
		if len(match.Teammates) > 0 && match.Teammates[0].CurrentRank != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	cfg := makeTestConfig(5*time.Minute, false)
	d, err := daemon.New(newMockSyncer(), cfg,
		daemon.WithPlayerTracker(tracker),
		daemon.WithStateStore(store),
	)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	rec := executeTestRequest(d, http.MethodGet, "/current-match", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp playertrack.CurrentMatchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}

	// Verify top-level match metadata
	if !resp.ActiveMatch {
		t.Errorf("expected active_match = true")
	}
	if resp.MatchEnded {
		t.Errorf("expected match_ended = false")
	}
	if resp.MatchGUID != "live-guid-999" {
		t.Errorf("expected match_guid 'live-guid-999', got %q", resp.MatchGUID)
	}
	if resp.PlaylistID != 11 {
		t.Errorf("expected playlist_id 11, got %d", resp.PlaylistID)
	}
	if resp.PlaylistName != "Ranked Doubles (2v2)" {
		t.Errorf("expected playlist_name 'Ranked Doubles (2v2)', got %q", resp.PlaylistName)
	}
	if resp.LocalTeam == nil || *resp.LocalTeam != 0 {
		t.Errorf("expected local_team = 0, got %v", resp.LocalTeam)
	}

	// Verify Local Player
	if resp.LocalPlayer == nil {
		t.Fatal("expected local_player to be populated")
	}
	if resp.LocalPlayer.PlayerID != localID || !resp.LocalPlayer.IsLocal {
		t.Errorf("unexpected local player details: %+v", resp.LocalPlayer)
	}

	// Verify Teammates
	if len(resp.Teammates) != 1 {
		t.Fatalf("expected 1 teammate, got %d", len(resp.Teammates))
	}
	tm := resp.Teammates[0]
	if tm.PlayerID != tmID || tm.Name != "Teammate1" || tm.TeamNum != 0 {
		t.Errorf("unexpected teammate details: %+v", tm)
	}
	if tm.Stats.Goals != 1 {
		t.Errorf("expected 1 goal, got %d", tm.Stats.Goals)
	}
	if tm.MatchupRecord == nil || tm.MatchupRecord.WinsAsTeammate != 1 {
		t.Errorf("expected 1 win as teammate in matchup record, got %+v", tm.MatchupRecord)
	}
	if tm.CurrentRank == nil || tm.CurrentRank.RankName != "Diamond III Division III" {
		t.Errorf("expected 'Diamond III Division III', got %+v", tm.CurrentRank)
	}

	// Verify Opponents
	if len(resp.Opponents) != 1 {
		t.Fatalf("expected 1 opponent, got %d", len(resp.Opponents))
	}
	opp := resp.Opponents[0]
	if opp.PlayerID != oppID || opp.Name != "Opponent1" || opp.TeamNum != 1 {
		t.Errorf("unexpected opponent details: %+v", opp)
	}
	if opp.MatchupRecord == nil || opp.MatchupRecord.WinsAsOpponent != 1 {
		t.Errorf("expected 1 win as opponent, got %+v", opp.MatchupRecord)
	}
}

// 4. Concluded Match: Result and WinnerTeam populated
func TestDaemon_HTTP_CurrentMatch_ConcludedMatch(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, backendSQLite)
	tracker := newTestTracker(t, store, nil)

	localID := "Epic|my_epic_acc_001|0"
	players := []statsapi.StatsPlayer{
		{PrimaryId: localID, Name: "Me", TeamNum: 0},
		{PrimaryId: "Epic|opp_001|0", Name: "Opp", TeamNum: 1},
	}
	_ = tracker.OnUpdateState(ctx, "guid-finish-1", 11, players)

	winner := 0
	_ = tracker.OnMatchEnded(ctx, "guid-finish-1", &winner)

	cfg := makeTestConfig(5*time.Minute, false)
	d, _ := daemon.New(newMockSyncer(), cfg, daemon.WithPlayerTracker(tracker), daemon.WithStateStore(store))

	rec := executeTestRequest(d, http.MethodGet, "/current-match", nil)
	var resp playertrack.CurrentMatchResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)

	if resp.ActiveMatch {
		t.Errorf("expected active_match = false after match end")
	}
	if !resp.MatchEnded {
		t.Errorf("expected match_ended = true")
	}
	if resp.WinnerTeam == nil || *resp.WinnerTeam != 0 {
		t.Errorf("expected winner_team = 0, got %v", resp.WinnerTeam)
	}
	if resp.Result != "victory" {
		t.Errorf("expected result 'victory', got %q", resp.Result)
	}
}

// 5. Method Not Allowed: POST /current-match
func TestDaemon_HTTP_CurrentMatch_MethodNotAllowed(t *testing.T) {
	cfg := makeTestConfig(5*time.Minute, false)
	d, _ := daemon.New(newMockSyncer(), cfg)

	rec := executeTestRequest(d, http.MethodPost, "/current-match", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 Method Not Allowed, got %d", rec.Code)
	}
}

func TestDaemon_HTTP_ConcurrentAccessAndRaceSafety(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	store := newTestStore(t, backendSQLite)
	fetcher := playertrack.NewMockSkillFetcher()
	tracker := newTestTracker(t, store, fetcher)

	cfg := makeTestConfig(10*time.Millisecond, false)
	d, err := daemon.New(newMockSyncer(), cfg,
		daemon.WithPlayerTracker(tracker),
		daemon.WithStateStore(store),
	)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	// Pre-seed baseline players
	for i := 1; i <= 5; i++ {
		pid := fmt.Sprintf("Steam|765611980000000%d|0", i)
		_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
			PlayerID:    pid,
			Platform:    "Steam",
			PlayerName:  fmt.Sprintf("Player_%d", i),
			RanksJSON:   "{}",
			FirstSeenAt: time.Now().UTC(),
			LastSeenAt:  time.Now().UTC(),
		})
	}

	var wg sync.WaitGroup

	// Worker Group 1: 10 Concurrent HTTP Readers
	endpoints := []string{
		"/current-match",
		"/players",
		"/players?limit=2&offset=1",
		"/players/Steam%7C7656119800000001%7C0",
		"/players/Steam%7Cnonexistent%7C0",
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					path := endpoints[workerID%len(endpoints)]
					rec := executeTestRequest(d, http.MethodGet, path, nil)
					if rec.Code != http.StatusOK && rec.Code != http.StatusNotFound {
						t.Errorf("worker %d: unexpected status %d for %s", workerID, rec.Code, path)
					}
					// Verify valid JSON
					var dummy interface{}
					if err := json.Unmarshal(rec.Body.Bytes(), &dummy); err != nil {
						t.Errorf("worker %d: JSON parse error on %s: %v", workerID, path, err)
					}
				}
			}
		}(i)
	}

	// Worker Group 2: 3 Concurrent In-Game Lobby Updaters
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(updaterID int) {
			defer wg.Done()
			tick := 0
			for {
				select {
				case <-ctx.Done():
					return
				default:
					tick++
					guid := fmt.Sprintf("race-guid-%d", updaterID)
					players := []statsapi.StatsPlayer{
						{PrimaryId: "Epic|my_epic_acc_001|0", Name: "Me", TeamNum: 0},
						{PrimaryId: fmt.Sprintf("Steam|765611980000000%d|0", (tick%5)+1), Name: "Tm", TeamNum: 0},
						{PrimaryId: fmt.Sprintf("Epic|opp_%d|0", tick%10), Name: "Opp", TeamNum: 1},
					}
					_ = tracker.OnUpdateState(ctx, guid, 11, players)
					time.Sleep(2 * time.Millisecond)
				}
			}
		}(i)
	}

	// Worker Group 3: 2 Concurrent Match Completion Updaters
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(enderID int) {
			defer wg.Done()
			tick := 0
			for {
				select {
				case <-ctx.Done():
					return
				default:
					tick++
					guid := fmt.Sprintf("race-guid-%d", enderID)
					winner := tick % 2
					_ = tracker.OnMatchEnded(ctx, guid, &winner)
					time.Sleep(5 * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()
}
