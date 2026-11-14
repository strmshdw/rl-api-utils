package daemon_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/storage"
)

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
