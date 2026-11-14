package daemon_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/storage"
)

func TestDaemon_HTTP_ListPlayers_DualBackend(t *testing.T) {
	for _, backend := range allStorageBackends {
		t.Run(string(backend), func(t *testing.T) {
			store := newTestStore(t, backend)
			cfg := makeTestConfig(5*time.Minute, false)
			d, err := daemon.New(newMockSyncer(), cfg, daemon.WithStateStore(store))
			if err != nil {
				t.Fatalf("daemon.New failed: %v", err)
			}

			// Subtest 1: Empty Store -> 200 OK []
			t.Run("EmptyStore", func(t *testing.T) {
				rec := executeTestRequest(d, http.MethodGet, "/players", nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("expected 200, got %d", rec.Code)
				}
				bodyStr := strings.TrimSpace(rec.Body.String())
				if bodyStr != "[]" {
					t.Errorf("expected '[]', got %q", bodyStr)
				}
			})

			// Seed 10 test players with distinct timestamps
			ctx := context.Background()
			now := time.Now().UTC()
			for i := 1; i <= 10; i++ {
				pid := fmt.Sprintf("Steam|765611980000000%02d|0", i)
				pName := fmt.Sprintf("Player_%02d", i)
				// Seed matchup for aggregation
				_ = store.RecordMatchResults(ctx, fmt.Sprintf("guid-%d", i), 11, []storage.PlayerOutcome{
					{PlayerID: pid, Platform: "Steam", PlayerName: pName, IsTeammate: i%2 == 0, Won: true},
				})
				_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
					PlayerID:    pid,
					Platform:    "Steam",
					PlayerName:  pName,
					RanksJSON:   "{}",
					FirstSeenAt: now.Add(time.Duration(i) * time.Minute),
					LastSeenAt:  now.Add(time.Duration(i) * time.Minute),
				})
			}

			// Subtest 2: Default Pagination (No params) -> returns all 10 in DESC order
			t.Run("DefaultPagination", func(t *testing.T) {
				rec := executeTestRequest(d, http.MethodGet, "/players", nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("expected 200, got %d", rec.Code)
				}
				var players []*storage.PlayerSummary
				if err := json.Unmarshal(rec.Body.Bytes(), &players); err != nil {
					t.Fatalf("decode JSON failed: %v", err)
				}
				if len(players) != 10 {
					t.Fatalf("expected 10 players, got %d", len(players))
				}
				// Verify DESC order: Player_10 was seen most recently
				if players[0].PlayerName != "Player_10" {
					t.Errorf("expected first player to be Player_10, got %s", players[0].PlayerName)
				}
				if players[9].PlayerName != "Player_01" {
					t.Errorf("expected last player to be Player_01, got %s", players[9].PlayerName)
				}
			})

			// Subtest 3: Limit and Offset Pagination Slices
			t.Run("Paging_LimitAndOffset", func(t *testing.T) {
				// Page 1: limit=3, offset=0 -> Player_10, Player_09, Player_08
				rec1 := executeTestRequest(d, http.MethodGet, "/players?limit=3&offset=0", nil)
				var page1 []*storage.PlayerSummary
				_ = json.Unmarshal(rec1.Body.Bytes(), &page1)
				if len(page1) != 3 || page1[0].PlayerName != "Player_10" || page1[2].PlayerName != "Player_08" {
					t.Errorf("unexpected page 1: %+v", page1)
				}

				// Page 2: limit=3, offset=3 -> Player_07, Player_06, Player_05
				rec2 := executeTestRequest(d, http.MethodGet, "/players?limit=3&offset=3", nil)
				var page2 []*storage.PlayerSummary
				_ = json.Unmarshal(rec2.Body.Bytes(), &page2)
				if len(page2) != 3 || page2[0].PlayerName != "Player_07" || page2[2].PlayerName != "Player_05" {
					t.Errorf("unexpected page 2: %+v", page2)
				}

				// Page 4: limit=3, offset=9 -> Player_01 (1 item)
				rec4 := executeTestRequest(d, http.MethodGet, "/players?limit=3&offset=9", nil)
				var page4 []*storage.PlayerSummary
				_ = json.Unmarshal(rec4.Body.Bytes(), &page4)
				if len(page4) != 1 || page4[0].PlayerName != "Player_01" {
					t.Errorf("unexpected page 4: %+v", page4)
				}

				// Page Beyond: offset=50 -> empty []
				recBeyond := executeTestRequest(d, http.MethodGet, "/players?limit=10&offset=50", nil)
				var pageBeyond []*storage.PlayerSummary
				_ = json.Unmarshal(recBeyond.Body.Bytes(), &pageBeyond)
				if len(pageBeyond) != 0 {
					t.Errorf("expected 0 items, got %d", len(pageBeyond))
				}
			})

			// Subtest 4: Query Param Clamping & Malformed String Recovery
			t.Run("ParamClamping_And_Fallbacks", func(t *testing.T) {
				// Negative values clamped: limit=-5, offset=-10 -> should treat as limit=1 (or default 50), offset=0
				recNeg := executeTestRequest(d, http.MethodGet, "/players?limit=-5&offset=-10", nil)
				if recNeg.Code != http.StatusOK {
					t.Errorf("expected 200 OK for negative params, got %d", recNeg.Code)
				}

				// High limit clamped to 100: limit=500
				recHigh := executeTestRequest(d, http.MethodGet, "/players?limit=500", nil)
				if recHigh.Code != http.StatusOK {
					t.Errorf("expected 200 OK for high limit, got %d", recHigh.Code)
				}

				// Malformed non-numeric values: limit=abc&offset=xyz -> fallback to default
				recAlpha := executeTestRequest(d, http.MethodGet, "/players?limit=abc&offset=xyz", nil)
				if recAlpha.Code != http.StatusOK {
					t.Errorf("expected 200 OK for alphabetic params, got %d", recAlpha.Code)
				}
				var alphaResp []*storage.PlayerSummary
				_ = json.Unmarshal(recAlpha.Body.Bytes(), &alphaResp)
				if len(alphaResp) != 10 {
					t.Errorf("expected all 10 players on fallback default, got %d", len(alphaResp))
				}
			})
		})
	}
}

func TestDaemon_HTTP_ListPlayers_NilStore(t *testing.T) {
	cfg := makeTestConfig(5*time.Minute, false)
	d, _ := daemon.New(newMockSyncer(), cfg) // stateStore is nil

	rec := executeTestRequest(d, http.MethodGet, "/players", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("expected '[]' on nil store, got %q", rec.Body.String())
	}
}

func TestDaemon_HTTP_ListPlayers_MethodNotAllowed(t *testing.T) {
	cfg := makeTestConfig(5*time.Minute, false)
	d, _ := daemon.New(newMockSyncer(), cfg)

	rec := executeTestRequest(d, http.MethodPost, "/players", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 Method Not Allowed, got %d", rec.Code)
	}
}

func TestDaemon_HTTP_GetPlayer_DualBackend(t *testing.T) {
	for _, backend := range allStorageBackends {
		t.Run(string(backend), func(t *testing.T) {
			store := newTestStore(t, backend)
			cfg := makeTestConfig(5*time.Minute, false)
			d, err := daemon.New(newMockSyncer(), cfg, daemon.WithStateStore(store))
			if err != nil {
				t.Fatalf("daemon.New failed: %v", err)
			}

			ctx := context.Background()
			targetSteamID := "Steam|76561198012345678|0"
			now := time.Now().UTC()

			// Seed player profile
			_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
				PlayerID:    targetSteamID,
				Platform:    "Steam",
				PlayerName:  "TargetPlayer",
				RanksJSON:   `{"11":{"tier":16,"division":1}}`,
				FirstSeenAt: now,
				LastSeenAt:  now,
			})

			// Seed 3 per-playlist matchups: Playlist 10 (1v1), 11 (2v2), 13 (3v3)
			_ = store.RecordMatchResults(ctx, "m1", 10, []storage.PlayerOutcome{
				{PlayerID: targetSteamID, Platform: "Steam", PlayerName: "TargetPlayer", IsTeammate: false, Won: true},
			})
			_ = store.RecordMatchResults(ctx, "m2", 11, []storage.PlayerOutcome{
				{PlayerID: targetSteamID, Platform: "Steam", PlayerName: "TargetPlayer", IsTeammate: true, Won: true},
				{PlayerID: targetSteamID, Platform: "Steam", PlayerName: "TargetPlayer", IsTeammate: true, Won: false},
			})
			_ = store.RecordMatchResults(ctx, "m3", 13, []storage.PlayerOutcome{
				{PlayerID: targetSteamID, Platform: "Steam", PlayerName: "TargetPlayer", IsTeammate: false, Won: false},
			})

			// Subtest 1: URL Unescaping of Pipe (%7C)
			t.Run("URLUnescaping_PipeEncoded", func(t *testing.T) {
				escapedPath := "/players/Steam%7C76561198012345678%7C0"
				rec := executeTestRequest(d, http.MethodGet, escapedPath, nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
				}

				var payload struct {
					*storage.PlayerRecord
					Matchups []*storage.PlayerMatchup `json:"matchups"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
					t.Fatalf("failed to decode JSON: %v", err)
				}

				if payload.PlayerID != targetSteamID {
					t.Errorf("expected player_id %q, got %q", targetSteamID, payload.PlayerID)
				}
				if payload.PlayerName != "TargetPlayer" {
					t.Errorf("expected player_name 'TargetPlayer', got %q", payload.PlayerName)
				}
				if len(payload.Matchups) != 3 {
					t.Fatalf("expected 3 playlist matchups, got %d", len(payload.Matchups))
				}
				// Verify ordering by playlist_id ASC (10, 11, 13)
				if payload.Matchups[0].PlaylistID != 10 || payload.Matchups[1].PlaylistID != 11 || payload.Matchups[2].PlaylistID != 13 {
					t.Errorf("matchups not sorted by playlist_id ASC: %+v", payload.Matchups)
				}
			})

			// Subtest 2: Raw Pipe Request (Unencoded)
			t.Run("RawPipe_Request", func(t *testing.T) {
				rawPath := "/players/Steam|76561198012345678|0"
				rec := executeTestRequest(d, http.MethodGet, rawPath, nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("expected 200 OK, got %d", rec.Code)
				}
			})

			// Subtest 3: Player With Zero Matchups
			t.Run("ZeroMatchups_EmptyArray", func(t *testing.T) {
				zeroID := "Epic|zero_matchups_user|0"
				_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
					PlayerID:    zeroID,
					Platform:    "Epic",
					PlayerName:  "ZeroUser",
					FirstSeenAt: now,
					LastSeenAt:  now,
				})

				rec := executeTestRequest(d, http.MethodGet, "/players/Epic%7Czero_matchups_user%7C0", nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("expected 200 OK, got %d", rec.Code)
				}

				var payload struct {
					*storage.PlayerRecord
					Matchups []*storage.PlayerMatchup `json:"matchups"`
				}
				_ = json.Unmarshal(rec.Body.Bytes(), &payload)

				if payload.PlayerID != zeroID {
					t.Errorf("expected %q, got %q", zeroID, payload.PlayerID)
				}
				if payload.Matchups == nil || len(payload.Matchups) != 0 {
					t.Errorf("expected empty non-nil slice [], got %+v", payload.Matchups)
				}
			})

			// Subtest 4: Nonexistent Player -> 404 Not Found
			t.Run("NotFound_404", func(t *testing.T) {
				rec := executeTestRequest(d, http.MethodGet, "/players/Steam%7Cnonexistent_id%7C0", nil)
				if rec.Code != http.StatusNotFound {
					t.Fatalf("expected 404 Not Found, got %d", rec.Code)
				}
				if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
					t.Errorf("expected application/json, got %s", ct)
				}

				var errResp map[string]string
				_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
				if errResp["error"] != "player not found" {
					t.Errorf("expected error 'player not found', got %q", errResp["error"])
				}
			})

			// Subtest 5: Malformed URL Escaping -> 400 Bad Request
			t.Run("MalformedURLEscaping_400", func(t *testing.T) {
				// %7 is an incomplete hex escape set directly on Path
				req, _ := http.NewRequest(http.MethodGet, "http://localhost", nil)
				req.URL.Path = "/players/Steam%7"
				rec := httptest.NewRecorder()
				d.Handler(context.Background()).ServeHTTP(rec, req)

				if rec.Code != http.StatusBadRequest {
					t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
				}
				var errResp map[string]string
				_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
				if errResp["error"] == "" {
					t.Errorf("expected non-empty error message")
				}
			})
		})
	}
}

func TestDaemon_HTTP_GetPlayer_NilStore(t *testing.T) {
	cfg := makeTestConfig(5*time.Minute, false)
	d, _ := daemon.New(newMockSyncer(), cfg) // Nil store

	rec := executeTestRequest(d, http.MethodGet, "/players/Steam%7C123%7C0", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestDaemon_HTTP_GetPlayer_MethodNotAllowed(t *testing.T) {
	cfg := makeTestConfig(5*time.Minute, false)
	d, _ := daemon.New(newMockSyncer(), cfg)

	rec := executeTestRequest(d, http.MethodDelete, "/players/Steam%7C123%7C0", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 Method Not Allowed, got %d", rec.Code)
	}
}
