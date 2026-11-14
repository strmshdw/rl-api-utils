package daemon_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rlapi"
)

// -----------------------------------------------------------------------------
// 1. GET /current-match STRESS TESTS
// -----------------------------------------------------------------------------

// TestStress_CurrentMatch_NilTracker asserts that GET /current-match gracefully handles
// a daemon constructed without WithPlayerTracker: returns 200 OK, active_match=false,
// empty non-null slices for teammates, opponents, and spectators.
func TestStress_CurrentMatch_NilTracker(t *testing.T) {
	cfg := makeTestConfig(5*time.Minute, false)
	d, err := daemon.New(newMockSyncer(), cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	rec := executeTestRequest(d, http.MethodGet, "/current-match", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}

	rawBody := rec.Body.String()
	// Empirical verification: arrays must be [] in raw JSON, never null
	if strings.Contains(rawBody, `"teammates":null`) {
		t.Errorf("raw JSON contains null teammates: %s", rawBody)
	}
	if strings.Contains(rawBody, `"opponents":null`) {
		t.Errorf("raw JSON contains null opponents: %s", rawBody)
	}
	if strings.Contains(rawBody, `"spectators":null`) {
		t.Errorf("raw JSON contains null spectators: %s", rawBody)
	}

	var resp playertrack.CurrentMatchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if resp.ActiveMatch {
		t.Errorf("expected active_match = false, got true")
	}
	if resp.MatchGUID != "" {
		t.Errorf("expected empty match_guid, got %q", resp.MatchGUID)
	}
	if resp.Teammates == nil || len(resp.Teammates) != 0 {
		t.Errorf("expected non-nil empty teammates slice, got %+v", resp.Teammates)
	}
	if resp.Opponents == nil || len(resp.Opponents) != 0 {
		t.Errorf("expected non-nil empty opponents slice, got %+v", resp.Opponents)
	}
	if len(resp.Spectators) != 0 {
		t.Errorf("expected empty spectators slice, got %+v", resp.Spectators)
	}
	if resp.UpdatedAt.IsZero() || time.Since(resp.UpdatedAt) > 5*time.Second {
		t.Errorf("expected recent updated_at timestamp, got %v", resp.UpdatedAt)
	}
}

// TestStress_CurrentMatch_InactiveMatch asserts that when a tracker is attached but
// no match events have been received, GET /current-match reports active_match=false
// with empty non-null collections.
func TestStress_CurrentMatch_InactiveMatch(t *testing.T) {
	for _, backend := range allStorageBackends {
		t.Run(string(backend), func(t *testing.T) {
			store := newTestStore(t, backend)
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
				t.Fatalf("expected 200 OK, got %d", rec.Code)
			}

			rawBody := rec.Body.String()
			if strings.Contains(rawBody, `"teammates":null`) ||
				strings.Contains(rawBody, `"opponents":null`) ||
				strings.Contains(rawBody, `"spectators":null`) {
				t.Fatalf("arrays must not be null in JSON: %s", rawBody)
			}

			var resp playertrack.CurrentMatchResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}

			if resp.ActiveMatch {
				t.Errorf("expected active_match = false")
			}
			if resp.MatchEnded {
				t.Errorf("expected match_ended = false")
			}
			if resp.MatchGUID != "" {
				t.Errorf("expected empty match_guid, got %q", resp.MatchGUID)
			}
			if len(resp.Teammates) != 0 || len(resp.Opponents) != 0 || len(resp.Spectators) != 0 {
				t.Errorf("expected 0 count for all player arrays")
			}
		})
	}
}

// TestStress_CurrentMatch_ActiveMatch_CompleteRosterAndMatchups validates an active match
// with a fully populated lobby (local player, multiple teammates, multiple opponents, spectators),
// pre-seeded historical head-to-head records across playlists, and mock PsyNet skills.
func TestStress_CurrentMatch_ActiveMatch_CompleteRosterAndMatchups(t *testing.T) {
	for _, backend := range allStorageBackends {
		t.Run(string(backend), func(t *testing.T) {
			ctx := context.Background()
			store := newTestStore(t, backend)

			localID := "Epic|my_epic_acc_001|0"
			tm1ID := "Steam|76561198000000001|0"
			tm2ID := "Steam|76561198000000002|0"
			opp1ID := "Epic|opp_alpha_001|0"
			opp2ID := "Epic|opp_beta_002|0"
			opp3ID := "Steam|76561198000000003|0"
			specID := "Steam|76561198000000099|0"

			// Seed historical matchup records
			// tm1: 5 wins, 2 losses as teammate, 1 win as opponent in playlist 13 (3v3)
			for i := 0; i < 5; i++ {
				_ = store.RecordMatchResults(ctx, fmt.Sprintf("hist-w-tm-%d", i), 13, []storage.PlayerOutcome{
					{PlayerID: tm1ID, Platform: "Steam", PlayerName: "Teammate_Alpha", IsTeammate: true, Won: true},
				})
			}
			for i := 0; i < 2; i++ {
				_ = store.RecordMatchResults(ctx, fmt.Sprintf("hist-l-tm-%d", i), 13, []storage.PlayerOutcome{
					{PlayerID: tm1ID, Platform: "Steam", PlayerName: "Teammate_Alpha", IsTeammate: true, Won: false},
				})
			}
			_ = store.RecordMatchResults(ctx, "hist-w-opp-0", 13, []storage.PlayerOutcome{
				{PlayerID: tm1ID, Platform: "Steam", PlayerName: "Teammate_Alpha", IsTeammate: false, Won: true},
			})

			// opp1: 3 wins, 4 losses as opponent in playlist 13
			for i := 0; i < 3; i++ {
				_ = store.RecordMatchResults(ctx, fmt.Sprintf("hist-opp1-w-%d", i), 13, []storage.PlayerOutcome{
					{PlayerID: opp1ID, Platform: "Epic", PlayerName: "Opponent_Alpha", IsTeammate: false, Won: true},
				})
			}
			for i := 0; i < 4; i++ {
				_ = store.RecordMatchResults(ctx, fmt.Sprintf("hist-opp1-l-%d", i), 13, []storage.PlayerOutcome{
					{PlayerID: opp1ID, Platform: "Epic", PlayerName: "Opponent_Alpha", IsTeammate: false, Won: false},
				})
			}

			// Mock PsyNet rank fetcher
			mockFetcher := playertrack.NewMockSkillFetcher()
			// tm1: Tier 18 Div 1 (Grand Champion I Division II)
			mockFetcher.SkillsMap[rlapi.PlayerID(tm1ID)] = []rlapi.Skill{
				{Playlist: 13, Tier: 18, Division: 1, MMR: 1435.5, MatchesPlayed: 85},
			}
			// tm2: Tier 16 Div 3 (Champion II Division IV)
			mockFetcher.SkillsMap[rlapi.PlayerID(tm2ID)] = []rlapi.Skill{
				{Playlist: 13, Tier: 16, Division: 3, MMR: 1210.0, MatchesPlayed: 40},
			}
			// opp1: Tier 19 Div 2 (Grand Champion II Division III)
			mockFetcher.SkillsMap[rlapi.PlayerID(opp1ID)] = []rlapi.Skill{
				{Playlist: 13, Tier: 19, Division: 2, MMR: 1580.2, MatchesPlayed: 150},
			}
			// opp2: Tier 17 Div 0 (Champion III Division I)
			mockFetcher.SkillsMap[rlapi.PlayerID(opp2ID)] = []rlapi.Skill{
				{Playlist: 13, Tier: 17, Division: 0, MMR: 1300.0, MatchesPlayed: 60},
			}

			tracker := newTestTracker(t, store, mockFetcher)

			// Ingest active 3v3 match with 7 lobby participants
			players := []statsapi.StatsPlayer{
				{PrimaryId: localID, Name: "Me", TeamNum: 0, Score: 240, Goals: 1, Shots: 3},
				{PrimaryId: tm1ID, Name: "Teammate_Alpha", TeamNum: 0, Score: 310, Goals: 2, Assists: 1},
				{PrimaryId: tm2ID, Name: "Teammate_Beta", TeamNum: 0, Score: 180, Saves: 3},
				{PrimaryId: opp1ID, Name: "Opponent_Alpha", TeamNum: 1, Score: 290, Goals: 1, Saves: 2},
				{PrimaryId: opp2ID, Name: "Opponent_Beta", TeamNum: 1, Score: 150, Shots: 2},
				{PrimaryId: opp3ID, Name: "Opponent_Gamma", TeamNum: 1, Score: 110, Saves: 1},
				{PrimaryId: specID, Name: "Spectator_Z", TeamNum: 255, Score: 0},
			}

			matchGUID := "match-stress-3v3-live"
			if err := tracker.OnUpdateState(ctx, matchGUID, 13, players); err != nil {
				t.Fatalf("OnUpdateState failed: %v", err)
			}

			// Await async rank fetcher resolution
			for i := 0; i < 50; i++ {
				cm := tracker.GetCurrentMatch()
				if len(cm.Teammates) == 2 && cm.Teammates[0].CurrentRank != nil && cm.Teammates[1].CurrentRank != nil {
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
				t.Fatalf("expected 200 OK, got %d", rec.Code)
			}

			var resp playertrack.CurrentMatchResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode JSON: %v", err)
			}

			// Verify Top-Level Match Metadata
			if !resp.ActiveMatch {
				t.Errorf("expected active_match = true")
			}
			if resp.MatchEnded {
				t.Errorf("expected match_ended = false")
			}
			if resp.MatchGUID != matchGUID {
				t.Errorf("expected match_guid %q, got %q", matchGUID, resp.MatchGUID)
			}
			if resp.PlaylistID != 13 {
				t.Errorf("expected playlist_id 13, got %d", resp.PlaylistID)
			}
			if resp.PlaylistName != "Ranked Standard (3v3)" {
				t.Errorf("expected playlist_name 'Ranked Standard (3v3)', got %q", resp.PlaylistName)
			}
			if resp.LocalTeam == nil || *resp.LocalTeam != 0 {
				t.Errorf("expected local_team = 0, got %v", resp.LocalTeam)
			}

			// Verify Local Player
			if resp.LocalPlayer == nil {
				t.Fatal("expected local_player to be populated")
			}
			if resp.LocalPlayer.PlayerID != localID || !resp.LocalPlayer.IsLocal || resp.LocalPlayer.TeamNum != 0 {
				t.Errorf("unexpected local player: %+v", resp.LocalPlayer)
			}
			if resp.LocalPlayer.Stats.Goals != 1 || resp.LocalPlayer.Stats.Score != 240 {
				t.Errorf("unexpected local player stats: %+v", resp.LocalPlayer.Stats)
			}

			// Verify Teammates (Count = 2)
			if len(resp.Teammates) != 2 {
				t.Fatalf("expected 2 teammates, got %d", len(resp.Teammates))
			}
			// Find tm1
			var foundTM1 *playertrack.LobbyPlayer
			for i := range resp.Teammates {
				if resp.Teammates[i].PlayerID == tm1ID {
					foundTM1 = &resp.Teammates[i]
					break
				}
			}
			if foundTM1 == nil {
				t.Fatalf("teammate %s not found", tm1ID)
			}
			if foundTM1.Stats.Goals != 2 || foundTM1.Stats.Assists != 1 {
				t.Errorf("unexpected tm1 stats: %+v", foundTM1.Stats)
			}
			if foundTM1.MatchupRecord == nil {
				t.Fatal("expected tm1 matchup record to be populated")
			}
			if foundTM1.MatchupRecord.WinsAsTeammate != 5 || foundTM1.MatchupRecord.LossesAsTeammate != 2 {
				t.Errorf("unexpected tm1 matchup record: %+v", foundTM1.MatchupRecord)
			}
			if foundTM1.CurrentRank == nil || foundTM1.CurrentRank.RankName != "Champion III Division II" {
				t.Errorf("expected tm1 rank 'Champion III Division II', got %+v", foundTM1.CurrentRank)
			}

			// Verify Opponents (Count = 3)
			if len(resp.Opponents) != 3 {
				t.Fatalf("expected 3 opponents, got %d", len(resp.Opponents))
			}
			// Find opp1
			var foundOpp1 *playertrack.LobbyPlayer
			for i := range resp.Opponents {
				if resp.Opponents[i].PlayerID == opp1ID {
					foundOpp1 = &resp.Opponents[i]
					break
				}
			}
			if foundOpp1 == nil {
				t.Fatalf("opponent %s not found", opp1ID)
			}
			if foundOpp1.MatchupRecord == nil || foundOpp1.MatchupRecord.WinsAsOpponent != 3 || foundOpp1.MatchupRecord.LossesAsOpponent != 4 {
				t.Errorf("unexpected opp1 matchup record: %+v", foundOpp1.MatchupRecord)
			}
			if foundOpp1.CurrentRank == nil || foundOpp1.CurrentRank.RankName != "Grand Champion I Division III" {
				t.Errorf("expected opp1 rank 'Grand Champion I Division III', got %+v", foundOpp1.CurrentRank)
			}

			// Verify Spectators (Count = 1)
			if len(resp.Spectators) != 1 {
				t.Fatalf("expected 1 spectator, got %d", len(resp.Spectators))
			}
			if resp.Spectators[0].PlayerID != specID {
				t.Errorf("expected spectator %s, got %s", specID, resp.Spectators[0].PlayerID)
			}

			// Transition match to ended
			winner := 0
			if err := tracker.OnMatchEnded(ctx, matchGUID, &winner); err != nil {
				t.Fatalf("OnMatchEnded failed: %v", err)
			}

			recEnded := executeTestRequest(d, http.MethodGet, "/current-match", nil)
			if recEnded.Code != http.StatusOK {
				t.Fatalf("expected 200 OK after match ended, got %d", recEnded.Code)
			}
			var respEnded playertrack.CurrentMatchResponse
			if err := json.Unmarshal(recEnded.Body.Bytes(), &respEnded); err != nil {
				t.Fatalf("failed to decode ended response: %v", err)
			}
			if respEnded.ActiveMatch {
				t.Errorf("expected active_match = false after conclusion")
			}
			if !respEnded.MatchEnded {
				t.Errorf("expected match_ended = true after conclusion")
			}
			if respEnded.WinnerTeam == nil || *respEnded.WinnerTeam != 0 {
				t.Errorf("expected winner_team = 0, got %v", respEnded.WinnerTeam)
			}
			if respEnded.Result != "victory" {
				t.Errorf("expected result 'victory', got %q", respEnded.Result)
			}
		})
	}
}

// TestStress_CurrentMatch_RapidQueryPolling_ConcurrentUpdates stresses GET /current-match
// with 50 concurrent query goroutines (10,000 total queries) while match states and outcomes
// are mutating rapidly. Verifies zero data races, zero panics, and consistent 200 OK responses.
func TestStress_CurrentMatch_RapidQueryPolling_ConcurrentUpdates(t *testing.T) {
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stateMutations int32
	// Background mutator: rapidly updates match state
	go func() {
		localID := "Epic|my_epic_acc_001|0"
		for i := 0; ; i++ {
			select {
			case <-ctx.Done():
				return
			default:
				guid := fmt.Sprintf("rapid-poll-guid-%d", i%10)
				players := []statsapi.StatsPlayer{
					{PrimaryId: localID, Name: "Me", TeamNum: 0, Score: rand.Intn(500)},
					{PrimaryId: fmt.Sprintf("Steam|765611980000000%02d|0", i%5), Name: fmt.Sprintf("P_%d", i%5), TeamNum: i % 2, Score: rand.Intn(400)},
				}
				_ = tracker.OnUpdateState(ctx, guid, 11, players)
				if i%3 == 0 {
					winner := rand.Intn(2)
					_ = tracker.OnMatchEnded(ctx, guid, &winner)
				}
				atomic.AddInt32(&stateMutations, 1)
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()

	const numWorkers = 50
	const queriesPerWorker = 200
	var wg sync.WaitGroup
	var successCount int64

	handler := d.Handler(ctx)

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for q := 0; q < queriesPerWorker; q++ {
				req := httptest.NewRequest(http.MethodGet, "/current-match", nil)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)

				if rec.Code != http.StatusOK {
					t.Errorf("worker received non-200 status: %d", rec.Code)
					return
				}

				var resp playertrack.CurrentMatchResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
					t.Errorf("worker failed to unmarshal JSON: %v", err)
					return
				}
				atomic.AddInt64(&successCount, 1)
			}
		}()
	}

	wg.Wait()
	cancel()

	expectedTotal := int64(numWorkers * queriesPerWorker)
	if successCount != expectedTotal {
		t.Fatalf("expected %d successful responses, got %d", expectedTotal, successCount)
	}
	if atomic.LoadInt32(&stateMutations) == 0 {
		t.Errorf("expected state mutations during stress test")
	}
}

// -----------------------------------------------------------------------------
// 2. GET /players STRESS TESTS (DUAL BACKEND & PAGINATION CLAMPING)
// -----------------------------------------------------------------------------

// TestStress_Players_EmptyStore_DualBackend verifies that GET /players on an empty store
// returns 200 OK and strictly empty array [] (never null or error) across both backends.
