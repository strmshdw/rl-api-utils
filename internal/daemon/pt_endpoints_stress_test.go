package daemon_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
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
func TestStress_Players_EmptyStore_DualBackend(t *testing.T) {
	for _, backend := range allStorageBackends {
		t.Run(string(backend), func(t *testing.T) {
			store := newTestStore(t, backend)
			cfg := makeTestConfig(5*time.Minute, false)
			d, err := daemon.New(newMockSyncer(), cfg, daemon.WithStateStore(store))
			if err != nil {
				t.Fatalf("daemon.New failed: %v", err)
			}

			// Subtest 1: Default request
			rec := executeTestRequest(d, http.MethodGet, "/players", nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200 OK, got %d", rec.Code)
			}
			raw := strings.TrimSpace(rec.Body.String())
			if raw != "[]" {
				t.Errorf("expected '[]', got %q", raw)
			}

			// Subtest 2: Query with limit & offset on empty store
			rec2 := executeTestRequest(d, http.MethodGet, "/players?limit=10&offset=5", nil)
			if rec2.Code != http.StatusOK {
				t.Fatalf("expected 200 OK, got %d", rec2.Code)
			}
			raw2 := strings.TrimSpace(rec2.Body.String())
			if raw2 != "[]" {
				t.Errorf("expected '[]', got %q", raw2)
			}
		})
	}
}

// TestStress_Players_PaginationClamping_EdgeLimits tests all boundary conditions:
// limit=0 (clamped to 1), limit=1000 (clamped to 100), offset=-1 (clamped to 0),
// offset=1000000 (returns empty slice, not null), non-integer strings, and integer overflow.
func TestStress_Players_PaginationClamping_EdgeLimits(t *testing.T) {
	for _, backend := range allStorageBackends {
		t.Run(string(backend), func(t *testing.T) {
			store := newTestStore(t, backend)
			ctx := context.Background()
			now := time.Now().UTC()

			// Seed 30 players
			const totalPlayers = 30
			for i := 1; i <= totalPlayers; i++ {
				pid := fmt.Sprintf("Steam|765611980000000%02d|0", i)
				_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
					PlayerID:    pid,
					Platform:    "Steam",
					PlayerName:  fmt.Sprintf("Player_%02d", i),
					RanksJSON:   "{}",
					FirstSeenAt: now.Add(time.Duration(i) * time.Minute),
					LastSeenAt:  now.Add(time.Duration(i) * time.Minute),
				})
			}

			cfg := makeTestConfig(5*time.Minute, false)
			d, err := daemon.New(newMockSyncer(), cfg, daemon.WithStateStore(store))
			if err != nil {
				t.Fatalf("daemon.New failed: %v", err)
			}

			testCases := []struct {
				name          string
				query         string
				expectedCount int
				firstPlayer   string // expected first player name if non-empty
			}{
				{
					name:          "limit=0 clamped to 1",
					query:         "/players?limit=0",
					expectedCount: 1,
					firstPlayer:   "Player_30",
				},
				{
					name:          "limit=-1 clamped to 1",
					query:         "/players?limit=-1",
					expectedCount: 1,
					firstPlayer:   "Player_30",
				},
				{
					name:          "limit=-9999 clamped to 1",
					query:         "/players?limit=-9999",
					expectedCount: 1,
					firstPlayer:   "Player_30",
				},
				{
					name:          "limit=1 exactly 1",
					query:         "/players?limit=1",
					expectedCount: 1,
					firstPlayer:   "Player_30",
				},
				{
					name:          "limit=1000 clamped to 100 (all 30 returned)",
					query:         "/players?limit=1000",
					expectedCount: 30,
					firstPlayer:   "Player_30",
				},
				{
					name:          "limit=500 clamped to 100",
					query:         "/players?limit=500",
					expectedCount: 30,
					firstPlayer:   "Player_30",
				},
				{
					name:          "offset=-1 clamped to 0",
					query:         "/players?limit=5&offset=-1",
					expectedCount: 5,
					firstPlayer:   "Player_30",
				},
				{
					name:          "offset=-999 clamped to 0",
					query:         "/players?limit=5&offset=-999",
					expectedCount: 5,
					firstPlayer:   "Player_30",
				},
				{
					name:          "offset=1000000 huge offset returns empty []",
					query:         "/players?limit=10&offset=1000000",
					expectedCount: 0,
				},
				{
					name:          "offset=30 exact total returns empty []",
					query:         "/players?limit=10&offset=30",
					expectedCount: 0,
				},
				{
					name:          "offset=25 returns remaining 5",
					query:         "/players?limit=10&offset=25",
					expectedCount: 5,
					firstPlayer:   "Player_05",
				},
				{
					name:          "non-integer limit fallback to default 50",
					query:         "/players?limit=foobar",
					expectedCount: 30,
					firstPlayer:   "Player_30",
				},
				{
					name:          "non-integer offset fallback to default 0",
					query:         "/players?limit=5&offset=xyz",
					expectedCount: 5,
					firstPlayer:   "Player_30",
				},
				{
					name:          "integer overflow limit fallback to default 50",
					query:         "/players?limit=999999999999999999999999999999",
					expectedCount: 30,
					firstPlayer:   "Player_30",
				},
				{
					name:          "integer overflow offset fallback to default 0",
					query:         "/players?limit=5&offset=999999999999999999999999999999",
					expectedCount: 5,
					firstPlayer:   "Player_30",
				},
			}

			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					rec := executeTestRequest(d, http.MethodGet, tc.query, nil)
					if rec.Code != http.StatusOK {
						t.Fatalf("expected 200 OK, got %d", rec.Code)
					}

					raw := strings.TrimSpace(rec.Body.String())
					if raw == "null" {
						t.Fatalf("response body must never be null: %s", raw)
					}

					var players []*storage.PlayerSummary
					if err := json.Unmarshal(rec.Body.Bytes(), &players); err != nil {
						t.Fatalf("unmarshal error: %v", err)
					}

					if len(players) != tc.expectedCount {
						t.Errorf("expected count %d, got %d", tc.expectedCount, len(players))
					}
					if tc.expectedCount > 0 && tc.firstPlayer != "" {
						if players[0].PlayerName != tc.firstPlayer {
							t.Errorf("expected first player %q, got %q", tc.firstPlayer, players[0].PlayerName)
						}
					}
				})
			}
		})
	}
}

// TestStress_Players_DualBackend_CrossParity seeds identical player and matchup datasets
// into both SQLiteStore and JSONStore, executes identical paginated queries, and verifies
// that both backends produce 100% identical outputs.
func TestStress_Players_DualBackend_CrossParity(t *testing.T) {
	ctx := context.Background()
	sqliteStore := newTestStore(t, backendSQLite)
	jsonStore := newTestStore(t, backendJSON)

	now := time.Now().UTC().Truncate(time.Second)

	// Seed 35 players with matchup records into both stores
	const total = 35
	for i := 1; i <= total; i++ {
		pid := fmt.Sprintf("Steam|765611980000000%02d|0", i)
		pname := fmt.Sprintf("Gamer_%02d", i)
		ts := now.Add(time.Duration(i) * time.Minute)

		// Seed matchups first
		outcomes := []storage.PlayerOutcome{
			{PlayerID: pid, Platform: "Steam", PlayerName: pname, IsTeammate: i%2 == 0, Won: i%3 != 0},
		}
		_ = sqliteStore.RecordMatchResults(ctx, fmt.Sprintf("parity-guid-%d", i), 11, outcomes)
		_ = jsonStore.RecordMatchResults(ctx, fmt.Sprintf("parity-guid-%d", i), 11, outcomes)

		// Set staggered LastSeenAt after match recording so pagination sort order is deterministic
		record := &storage.PlayerRecord{
			PlayerID:    pid,
			Platform:    "Steam",
			PlayerName:  pname,
			RanksJSON:   `{"11":{"tier":15,"division":2}}`,
			FirstSeenAt: ts,
			LastSeenAt:  ts,
		}

		_ = sqliteStore.UpsertPlayer(ctx, record)
		_ = jsonStore.UpsertPlayer(ctx, record)
	}

	cfg := makeTestConfig(5*time.Minute, false)
	dSQLite, _ := daemon.New(newMockSyncer(), cfg, daemon.WithStateStore(sqliteStore))
	dJSON, _ := daemon.New(newMockSyncer(), cfg, daemon.WithStateStore(jsonStore))

	// Test 5 pagination windows
	pages := []string{
		"/players?limit=10&offset=0",
		"/players?limit=10&offset=10",
		"/players?limit=10&offset=20",
		"/players?limit=10&offset=30",
		"/players?limit=10&offset=40",
	}

	for _, pageQuery := range pages {
		t.Run(pageQuery, func(t *testing.T) {
			recSQL := executeTestRequest(dSQLite, http.MethodGet, pageQuery, nil)
			recJSON := executeTestRequest(dJSON, http.MethodGet, pageQuery, nil)

			if recSQL.Code != recJSON.Code {
				t.Fatalf("status code mismatch: SQLite=%d, JSON=%d", recSQL.Code, recJSON.Code)
			}

			var sqlList []*storage.PlayerSummary
			var jsonList []*storage.PlayerSummary

			if err := json.Unmarshal(recSQL.Body.Bytes(), &sqlList); err != nil {
				t.Fatalf("sqlite unmarshal error: %v", err)
			}
			if err := json.Unmarshal(recJSON.Body.Bytes(), &jsonList); err != nil {
				t.Fatalf("json unmarshal error: %v", err)
			}

			if len(sqlList) != len(jsonList) {
				t.Fatalf("length mismatch: SQLite=%d, JSON=%d", len(sqlList), len(jsonList))
			}

			for idx := range sqlList {
				s := sqlList[idx]
				j := jsonList[idx]

				if s.PlayerID != j.PlayerID ||
					s.Platform != j.Platform ||
					s.PlayerName != j.PlayerName ||
					s.TotalMatches != j.TotalMatches ||
					s.TotalWinsAsTeammate != j.TotalWinsAsTeammate ||
					s.TotalLossesAsTeammate != j.TotalLossesAsTeammate ||
					s.TotalWinsAsOpponent != j.TotalWinsAsOpponent ||
					s.TotalLossesAsOpponent != j.TotalLossesAsOpponent {
					t.Fatalf("cross-backend parity mismatch at index %d:\nSQLite: %+v\nJSON:   %+v", idx, s, j)
				}
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 3. GET /players/{id} STRESS TESTS (ESCAPING, 400s, 404s, PARITY)
// -----------------------------------------------------------------------------

// TestStress_GetPlayer_PercentEncodedAndRawPipes verifies that GET /players/{id} correctly
// handles percent-encoded IDs (%7C), lowercase hex (%7c), raw pipes (|), and encoded spaces (%20).
func TestStress_GetPlayer_PercentEncodedAndRawPipes(t *testing.T) {
	for _, backend := range allStorageBackends {
		t.Run(string(backend), func(t *testing.T) {
			store := newTestStore(t, backend)
			ctx := context.Background()
			now := time.Now().UTC()

			// Seed test players
			steamID := "Steam|76561198000000001|0"
			epicID := "Epic|000102030405060708090a0b0c0d0e0f|0"
			spaceID := "Steam|7656 1198 0002|0"

			_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
				PlayerID: steamID, Platform: "Steam", PlayerName: "SteamPlayer1", FirstSeenAt: now, LastSeenAt: now,
			})
			_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
				PlayerID: epicID, Platform: "Epic", PlayerName: "EpicPlayer1", FirstSeenAt: now, LastSeenAt: now,
			})
			_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
				PlayerID: spaceID, Platform: "Steam", PlayerName: "SpacedPlayer", FirstSeenAt: now, LastSeenAt: now,
			})

			cfg := makeTestConfig(5*time.Minute, false)
			d, err := daemon.New(newMockSyncer(), cfg, daemon.WithStateStore(store))
			if err != nil {
				t.Fatalf("daemon.New failed: %v", err)
			}

			cases := []struct {
				name             string
				reqPath          string
				expectedPlayerID string
				expectedName     string
			}{
				{
					name:             "Steam percent encoded uppercase %7C",
					reqPath:          "/players/Steam%7C76561198000000001%7C0",
					expectedPlayerID: steamID,
					expectedName:     "SteamPlayer1",
				},
				{
					name:             "Steam percent encoded lowercase %7c",
					reqPath:          "/players/Steam%7c76561198000000001%7c0",
					expectedPlayerID: steamID,
					expectedName:     "SteamPlayer1",
				},
				{
					name:             "Steam raw unencoded pipe",
					reqPath:          "/players/Steam|76561198000000001|0",
					expectedPlayerID: steamID,
					expectedName:     "SteamPlayer1",
				},
				{
					name:             "Epic percent encoded %7C",
					reqPath:          "/players/Epic%7C000102030405060708090a0b0c0d0e0f%7C0",
					expectedPlayerID: epicID,
					expectedName:     "EpicPlayer1",
				},
				{
					name:             "Epic raw unencoded pipe",
					reqPath:          "/players/Epic|000102030405060708090a0b0c0d0e0f|0",
					expectedPlayerID: epicID,
					expectedName:     "EpicPlayer1",
				},
				{
					name:             "Player ID with encoded space %20",
					reqPath:          "/players/Steam%7C7656%201198%200002%7C0",
					expectedPlayerID: spaceID,
					expectedName:     "SpacedPlayer",
				},
			}

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					rec := executeTestRequest(d, http.MethodGet, tc.reqPath, nil)
					if rec.Code != http.StatusOK {
						t.Fatalf("expected 200 OK for %s, got %d: %s", tc.reqPath, rec.Code, rec.Body.String())
					}

					var resp daemon.PlayerDetailResponse
					if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
						t.Fatalf("failed to decode JSON: %v", err)
					}

					if resp.PlayerID != tc.expectedPlayerID {
						t.Errorf("expected player ID %q, got %q", tc.expectedPlayerID, resp.PlayerID)
					}
					if resp.PlayerName != tc.expectedName {
						t.Errorf("expected player name %q, got %q", tc.expectedName, resp.PlayerName)
					}
					if resp.Matchups == nil {
						t.Errorf("expected non-nil matchups slice")
					}
				})
			}
		})
	}
}

// TestStress_GetPlayer_MalformedPercentEscapes_Returns400 verifies that malformed percent
// escapes (e.g., %, %2, %ZZ, %G1, trailing %) return HTTP 400 Bad Request.
// Tested both via in-memory handler execution and over actual TCP wire connection.
func TestStress_GetPlayer_MalformedPercentEscapes_Returns400(t *testing.T) {
	store := newTestStore(t, backendSQLite)
	cfg := makeTestConfig(5*time.Minute, false)
	d, err := daemon.New(newMockSyncer(), cfg, daemon.WithStateStore(store))
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	malformedIDs := []string{
		"%",
		"%2",
		"%ZZ",
		"%G1",
		"Steam%",
		"Steam%7",
		"Steam%2Z",
		"Steam%ZZ%7C0",
	}

	handler := d.Handler(context.Background())

	for _, malformed := range malformedIDs {
		t.Run("Handler_"+malformed, func(t *testing.T) {
			// Construct request directly with malformed path without triggering url.Parse errors
			req, err := http.NewRequest(http.MethodGet, "http://localhost", nil)
			if err != nil {
				t.Fatalf("failed to construct request: %v", err)
			}
			req.URL.Path = "/players/" + malformed

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 Bad Request for %q, got %d: %s", malformed, rec.Code, rec.Body.String())
			}

			var errResp map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
				t.Fatalf("failed to decode error JSON: %v", err)
			}
			if errResp["error"] == "" {
				t.Errorf("expected non-empty error message in response")
			}
		})
	}

	// Live TCP Wire Verification: spin up httptest.Server and send raw HTTP bytes over net.Dial
	srv := httptest.NewServer(handler)
	defer srv.Close()

	srvURL, _ := url.Parse(srv.URL)
	hostPort := srvURL.Host

	for _, malformed := range malformedIDs {
		t.Run("WireTCP_"+malformed, func(t *testing.T) {
			conn, err := net.DialTimeout("tcp", hostPort, 1*time.Second)
			if err != nil {
				t.Fatalf("failed to connect to server: %v", err)
			}
			defer conn.Close()

			rawRequest := fmt.Sprintf("GET /players/%s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", malformed, hostPort)
			if _, err := io.WriteString(conn, rawRequest); err != nil {
				t.Fatalf("failed to write raw request: %v", err)
			}

			respReader := bufio.NewReader(conn)
			resp, err := http.ReadResponse(respReader, nil)
			if err != nil {
				t.Fatalf("failed to read response: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				bodyBytes, _ := io.ReadAll(resp.Body)
				t.Fatalf("expected 400 Bad Request on wire for %q, got %d: %s", malformed, resp.StatusCode, string(bodyBytes))
			}
		})
	}
}

// TestStress_GetPlayer_NotFound_And_EmptyPath asserts 404 Not Found for nonexistent IDs,
// empty path, and whitespace-only IDs.
func TestStress_GetPlayer_NotFound_And_EmptyPath(t *testing.T) {
	for _, backend := range allStorageBackends {
		t.Run(string(backend), func(t *testing.T) {
			store := newTestStore(t, backend)
			cfg := makeTestConfig(5*time.Minute, false)
			d, err := daemon.New(newMockSyncer(), cfg, daemon.WithStateStore(store))
			if err != nil {
				t.Fatalf("daemon.New failed: %v", err)
			}

			cases := []struct {
				name    string
				path    string
				expCode int
			}{
				{
					name:    "Nonexistent Steam ID",
					path:    "/players/Steam%7C99999999999999999%7C0",
					expCode: http.StatusNotFound,
				},
				{
					name:    "Nonexistent Epic ID",
					path:    "/players/Epic%7Cnonexistent_acc_guid%7C0",
					expCode: http.StatusNotFound,
				},
				{
					name:    "Empty ID after slash",
					path:    "/players/",
					expCode: http.StatusNotFound,
				},
				{
					name:    "Multiple slashes cleaned by ServeMux (301 Redirect)",
					path:    "/players///",
					expCode: http.StatusMovedPermanently,
				},
				{
					name:    "Encoded slash (%2F) unescapes to slash then trimmed to empty",
					path:    "/players/%2F",
					expCode: http.StatusNotFound,
				},
				{
					name:    "Whitespace only encoded (%20)",
					path:    "/players/%20",
					expCode: http.StatusNotFound,
				},
				{
					name:    "Tab character encoded (%09)",
					path:    "/players/%09",
					expCode: http.StatusNotFound,
				},
			}

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					rec := executeTestRequest(d, http.MethodGet, tc.path, nil)
					if tc.path == "/players///" {
						if rec.Code != http.StatusMovedPermanently && rec.Code != http.StatusTemporaryRedirect {
							t.Fatalf("expected status 301 or 307 for %s, got %d: %s", tc.path, rec.Code, rec.Body.String())
						}
					} else if rec.Code != tc.expCode {
						t.Fatalf("expected status %d for %s, got %d: %s", tc.expCode, tc.path, rec.Code, rec.Body.String())
					}
					if tc.expCode == http.StatusNotFound {
						var errResp map[string]string
						_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
						if errResp["error"] != "player not found" {
							t.Errorf("expected error 'player not found', got %q", errResp["error"])
						}
					} else if tc.expCode == http.StatusMovedPermanently || rec.Code == http.StatusTemporaryRedirect {
						loc := rec.Header().Get("Location")
						if loc != "/players/" {
							t.Errorf("expected redirect location '/players/', got %q", loc)
						}
					}
				})
			}
		})
	}
}

// TestStress_GetPlayer_DualBackend_FullParity verifies that detailed player records
// with multi-playlist matchup histories are returned identically across SQLite and JSONStore.
func TestStress_GetPlayer_DualBackend_FullParity(t *testing.T) {
	ctx := context.Background()
	sqliteStore := newTestStore(t, backendSQLite)
	jsonStore := newTestStore(t, backendJSON)

	now := time.Now().UTC().Truncate(time.Second)
	targetID := "Steam|76561198099887766|0"

	record := &storage.PlayerRecord{
		PlayerID:    targetID,
		Platform:    "Steam",
		PlayerName:  "ParityMaster",
		RanksJSON:   `{"10":{"tier":12,"division":1},"11":{"tier":16,"division":2},"13":{"tier":17,"division":0}}`,
		FirstSeenAt: now.Add(-10 * time.Hour),
		LastSeenAt:  now,
	}

	_ = sqliteStore.UpsertPlayer(ctx, record)
	_ = jsonStore.UpsertPlayer(ctx, record)

	// Seed matchups across 3 playlists: 10 (1v1), 11 (2v2), 13 (3v3)
	matchupsToSeed := []struct {
		playlist   int
		isTeammate bool
		won        bool
		count      int
	}{
		{playlist: 10, isTeammate: false, won: true, count: 4},
		{playlist: 10, isTeammate: false, won: false, count: 2},
		{playlist: 11, isTeammate: true, won: true, count: 8},
		{playlist: 11, isTeammate: true, won: false, count: 3},
		{playlist: 11, isTeammate: false, won: true, count: 2},
		{playlist: 13, isTeammate: true, won: false, count: 5},
	}

	for i, m := range matchupsToSeed {
		for c := 0; c < m.count; c++ {
			guid := fmt.Sprintf("guid-m-%d-%d", i, c)
			outcomes := []storage.PlayerOutcome{
				{PlayerID: targetID, Platform: "Steam", PlayerName: "ParityMaster", IsTeammate: m.isTeammate, Won: m.won},
			}
			_ = sqliteStore.RecordMatchResults(ctx, guid, m.playlist, outcomes)
			_ = jsonStore.RecordMatchResults(ctx, guid, m.playlist, outcomes)
		}
	}

	cfg := makeTestConfig(5*time.Minute, false)
	dSQL, _ := daemon.New(newMockSyncer(), cfg, daemon.WithStateStore(sqliteStore))
	dJSON, _ := daemon.New(newMockSyncer(), cfg, daemon.WithStateStore(jsonStore))

	reqPath := "/players/Steam%7C76561198099887766%7C0"
	recSQL := executeTestRequest(dSQL, http.MethodGet, reqPath, nil)
	recJSON := executeTestRequest(dJSON, http.MethodGet, reqPath, nil)

	if recSQL.Code != http.StatusOK || recJSON.Code != http.StatusOK {
		t.Fatalf("expected 200 OK: SQLite=%d, JSON=%d", recSQL.Code, recJSON.Code)
	}

	var respSQL daemon.PlayerDetailResponse
	var respJSON daemon.PlayerDetailResponse

	if err := json.Unmarshal(recSQL.Body.Bytes(), &respSQL); err != nil {
		t.Fatalf("failed to decode SQL: %v", err)
	}
	if err := json.Unmarshal(recJSON.Body.Bytes(), &respJSON); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}

	// Compare player record fields
	if respSQL.PlayerID != respJSON.PlayerID ||
		respSQL.Platform != respJSON.Platform ||
		respSQL.PlayerName != respJSON.PlayerName ||
		respSQL.RanksJSON != respJSON.RanksJSON {
		t.Fatalf("player record mismatch:\nSQL:  %+v\nJSON: %+v", respSQL.PlayerRecord, respJSON.PlayerRecord)
	}

	// Compare matchups
	if len(respSQL.Matchups) != len(respJSON.Matchups) {
		t.Fatalf("matchup count mismatch: SQL=%d, JSON=%d", len(respSQL.Matchups), len(respJSON.Matchups))
	}
	if len(respSQL.Matchups) != 3 {
		t.Fatalf("expected 3 playlist matchups, got %d", len(respSQL.Matchups))
	}

	for i := 0; i < len(respSQL.Matchups); i++ {
		sm := respSQL.Matchups[i]
		jm := respJSON.Matchups[i]

		if sm.PlaylistID != jm.PlaylistID ||
			sm.WinsAsTeammate != jm.WinsAsTeammate ||
			sm.LossesAsTeammate != jm.LossesAsTeammate ||
			sm.WinsAsOpponent != jm.WinsAsOpponent ||
			sm.LossesAsOpponent != jm.LossesAsOpponent ||
			sm.TotalMatches != jm.TotalMatches {
			t.Fatalf("matchup mismatch at index %d:\nSQL:  %+v\nJSON: %+v", i, sm, jm)
		}
	}
}

// -----------------------------------------------------------------------------
// 4. LIVE HTTP SERVER FULL SYSTEM CONCURRENT STRESS TEST
// -----------------------------------------------------------------------------

// TestStress_FullHTTPAPI_HighConcurrency exercises all query endpoints concurrently
// under load across 50 worker goroutines over live HTTP connections.
func TestStress_FullHTTPAPI_HighConcurrency(t *testing.T) {
	store := newTestStore(t, backendSQLite)
	ctx := context.Background()
	now := time.Now().UTC()

	// Seed 50 players in SQLite
	const seedCount = 50
	for i := 1; i <= seedCount; i++ {
		pid := fmt.Sprintf("Steam|765611980000000%02d|0", i)
		_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
			PlayerID:    pid,
			Platform:    "Steam",
			PlayerName:  fmt.Sprintf("ConcurrentPlayer_%02d", i),
			RanksJSON:   `{"11":{"tier":14,"division":0}}`,
			FirstSeenAt: now,
			LastSeenAt:  now,
		})
		_ = store.RecordMatchResults(ctx, fmt.Sprintf("conc-guid-%d", i), 11, []storage.PlayerOutcome{
			{PlayerID: pid, Platform: "Steam", PlayerName: fmt.Sprintf("ConcurrentPlayer_%02d", i), IsTeammate: true, Won: true},
		})
	}

	tracker := newTestTracker(t, store, nil)
	// Seed an active match in tracker
	_ = tracker.OnUpdateState(ctx, "live-conc-guid", 11, []statsapi.StatsPlayer{
		{PrimaryId: "Epic|my_epic_acc_001|0", Name: "Me", TeamNum: 0},
		{PrimaryId: "Steam|76561198000000001|0", Name: "ConcurrentPlayer_01", TeamNum: 1},
	})

	cfg := makeTestConfig(5*time.Minute, false)
	d, err := daemon.New(newMockSyncer(), cfg,
		daemon.WithPlayerTracker(tracker),
		daemon.WithStateStore(store),
	)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	srv := httptest.NewServer(d.Handler(ctx))
	defer srv.Close()

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
		},
	}

	const numWorkers = 50
	const requestsPerWorker = 100
	var wg sync.WaitGroup
	var totalRequests int64
	var errCount int64

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for r := 0; r < requestsPerWorker; r++ {
				endpointChoice := r % 4
				var targetURL string
				var expectedStatus int

				switch endpointChoice {
				case 0:
					// GET /current-match
					targetURL = srv.URL + "/current-match"
					expectedStatus = http.StatusOK
				case 1:
					// GET /players with randomized limit and offset
					limit := rand.Intn(150) - 10   // [-10, 140]
					offset := rand.Intn(100) - 10 // [-10, 90]
					targetURL = fmt.Sprintf("%s/players?limit=%d&offset=%d", srv.URL, limit, offset)
					expectedStatus = http.StatusOK
				case 2:
					// GET /players/{id} with percent encoded ID
					targetIdx := (r % seedCount) + 1
					targetURL = fmt.Sprintf("%s/players/Steam%%7C765611980000000%02d%%7C0", srv.URL, targetIdx)
					expectedStatus = http.StatusOK
				case 3:
					// GET /players/{id} with nonexistent ID -> 404
					targetURL = srv.URL + "/players/Steam%7Cnonexistent_id%7C0"
					expectedStatus = http.StatusNotFound
				}

				resp, err := client.Get(targetURL)
				if err != nil {
					atomic.AddInt64(&errCount, 1)
					continue
				}

				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()

				if resp.StatusCode != expectedStatus {
					atomic.AddInt64(&errCount, 1)
					t.Errorf("worker %d req %d: url %s expected %d, got %d: %s",
						workerID, r, targetURL, expectedStatus, resp.StatusCode, string(body))
					return
				}
				atomic.AddInt64(&totalRequests, 1)
			}
		}(w)
	}

	wg.Wait()

	if atomic.LoadInt64(&errCount) > 0 {
		t.Fatalf("encountered %d request errors during high concurrency stress", errCount)
	}
	expectedTotal := int64(numWorkers * requestsPerWorker)
	if totalRequests != expectedTotal {
		t.Fatalf("expected %d total requests, processed %d", expectedTotal, totalRequests)
	}
}
