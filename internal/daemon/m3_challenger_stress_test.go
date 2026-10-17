package daemon_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/session"
	"github.com/dank/rl-api-utils/internal/storage"
)

func newDaemonChallengerInstance(t *testing.T, isJSON bool) (*daemon.Daemon, storage.StateStore, http.Handler) {
	t.Helper()
	tempDir := t.TempDir()
	var dbPath string
	if isJSON {
		dbPath = filepath.Join(tempDir, "daemon_chal.json")
	} else {
		dbPath = filepath.Join(tempDir, "daemon_chal.db")
	}

	store, err := storage.NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store (isJSON=%v): %v", isJSON, err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	cfg := &config.Config{
		Sync: config.SyncConfig{Once: false},
		Web:  config.WebConfig{Enabled: true, Port: 49125},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	buf := &bytes.Buffer{}
	logger := daemon.NewLogger(cfg.Logging, buf)
	sess := session.NewSessionTracker(session.WithLogger(logger))
	fetcher := playertrack.NewMockSkillFetcher()
	tracker := newTestTracker(t, store, fetcher)
	syncerMock := newMockSyncer()

	d, err := daemon.New(syncerMock, cfg,
		daemon.WithStateStore(store),
		daemon.WithPlayerTracker(tracker),
		daemon.WithSessionTracker(sess),
		daemon.WithLogger(logger),
	)
	if err != nil {
		t.Fatalf("failed to create daemon: %v", err)
	}

	ctx := context.Background()
	handler := d.Handler(ctx)
	return d, store, handler
}

// TestChallenger_Daemon_OrangeVictory_And_UnsetWinner verifies:
// 1. When local player is Orange and Orange wins, both GET /api/matches and GET /api/matches/{guid}
//    return Result = "victory".
// 2. When winner_team = -1 but scores differ (e.g. 3-1), both GET /api/matches and GET /api/matches/{guid}
//    evaluate score relative to local player's team and return "victory" or "defeat" (NEVER "draw").
// 3. Tests both SQLite and JSONStore daemon backends.
func TestChallenger_Daemon_OrangeVictory_And_UnsetWinner(t *testing.T) {
	backends := []struct {
		name   string
		isJSON bool
	}{
		{"SQLite", false},
		{"JSONStore", true},
	}

	for _, b := range backends {
		t.Run(b.name, func(t *testing.T) {
			_, store, handler := newDaemonChallengerInstance(t, b.isJSON)
			ctx := context.Background()
			now := time.Now().UTC()

			localPID := "Steam|76561198000000077|0"
			_ = store.SaveAuthState(ctx, "steam", "chal-token", localPID, "OrangeChampion")

			// Case 1: Orange wins 3-1, winner_team = 1, local on Orange (team 1)
			guidOrangeExplicit := "guid-orange-explicit-win"
			wt1 := 1
			err := store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
				MatchGUID:   guidOrangeExplicit,
				MapName:     "dfh_stadium",
				Playlist:    11,
				BlueScore:   1,
				OrangeScore: 3,
				WinnerTeam:  &wt1,
				Date:        now.Add(-20 * time.Minute).Format(time.RFC3339),
			})
			if err != nil {
				t.Fatalf("[%s] SyncReplayStats failed: %v", b.name, err)
			}
			wonTrue := true
			_ = store.SaveMatchPlayers(ctx, guidOrangeExplicit, []*storage.MatchPlayerRecord{
				{MatchGUID: guidOrangeExplicit, PlayerID: localPID, TeamNum: 1, IsLocal: true, Won: &wonTrue},
			})

			// Case 2: Orange wins 3-1, winner_team = -1 (UNSET), local on Orange (team 1)
			guidOrangeUnset := "guid-orange-unset-winner"
			wtUnset := -1
			err = store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
				MatchGUID:   guidOrangeUnset,
				MapName:     "mannfield",
				Playlist:    11,
				BlueScore:   1,
				OrangeScore: 3,
				WinnerTeam:  &wtUnset,
				Date:        now.Add(-10 * time.Minute).Format(time.RFC3339),
			})
			if err != nil {
				t.Fatalf("[%s] SyncReplayStats failed: %v", b.name, err)
			}
			_ = store.SaveMatchPlayers(ctx, guidOrangeUnset, []*storage.MatchPlayerRecord{
				{MatchGUID: guidOrangeUnset, PlayerID: localPID, TeamNum: 1, IsLocal: true},
			})

			// Case 3: Blue wins 4-0, winner_team = -1 (UNSET), local on Orange (team 1) -> DEFEAT
			guidOrangeDefeatUnset := "guid-orange-defeat-unset"
			err = store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
				MatchGUID:   guidOrangeDefeatUnset,
				MapName:     "champions_field",
				Playlist:    11,
				BlueScore:   4,
				OrangeScore: 0,
				WinnerTeam:  &wtUnset,
				Date:        now.Format(time.RFC3339),
			})
			if err != nil {
				t.Fatalf("[%s] SyncReplayStats failed: %v", b.name, err)
			}
			_ = store.SaveMatchPlayers(ctx, guidOrangeDefeatUnset, []*storage.MatchPlayerRecord{
				{MatchGUID: guidOrangeDefeatUnset, PlayerID: localPID, TeamNum: 1, IsLocal: true},
			})

			// 1. Test GET /api/matches?result=victory
			reqListVic := httptest.NewRequest(http.MethodGet, "/api/matches?result=victory", nil)
			recListVic := httptest.NewRecorder()
			handler.ServeHTTP(recListVic, reqListVic)

			if recListVic.Code != http.StatusOK {
				t.Fatalf("[%s] GET /api/matches?result=victory failed: %d", b.name, recListVic.Code)
			}
			var listVicResp daemon.MatchesResponse
			if err := json.Unmarshal(recListVic.Body.Bytes(), &listVicResp); err != nil {
				t.Fatalf("[%s] unmarshal list victories failed: %v", b.name, err)
			}
			if listVicResp.Total != 2 || len(listVicResp.Matches) != 2 {
				t.Fatalf("[%s] expected 2 victories (explicit + unset), got total=%d len=%d", b.name, listVicResp.Total, len(listVicResp.Matches))
			}
			for _, m := range listVicResp.Matches {
				if m.Result != "victory" {
					t.Errorf("[%s] list match %s expected result=victory, got %q", b.name, m.MatchGUID, m.Result)
				}
			}

			// 2. Test GET /api/matches/{guid} for Case 1 (Explicit Orange Victory)
			reqDet1 := httptest.NewRequest(http.MethodGet, "/api/matches/"+guidOrangeExplicit, nil)
			recDet1 := httptest.NewRecorder()
			handler.ServeHTTP(recDet1, reqDet1)
			if recDet1.Code != http.StatusOK {
				t.Fatalf("[%s] GET /api/matches/%s failed: %d", b.name, guidOrangeExplicit, recDet1.Code)
			}
			var det1 storage.HistoricalMatchRecord
			_ = json.Unmarshal(recDet1.Body.Bytes(), &det1)
			if det1.Result != "victory" {
				t.Errorf("[%s] GET /api/matches/%s returned Result = %q, expected 'victory'", b.name, guidOrangeExplicit, det1.Result)
			}

			// 3. Test GET /api/matches/{guid} for Case 2 (Orange Win with winner_team = -1)
			reqDet2 := httptest.NewRequest(http.MethodGet, "/api/matches/"+guidOrangeUnset, nil)
			recDet2 := httptest.NewRecorder()
			handler.ServeHTTP(recDet2, reqDet2)
			if recDet2.Code != http.StatusOK {
				t.Fatalf("[%s] GET /api/matches/%s failed: %d", b.name, guidOrangeUnset, recDet2.Code)
			}
			var det2 storage.HistoricalMatchRecord
			_ = json.Unmarshal(recDet2.Body.Bytes(), &det2)
			if det2.Result != "victory" {
				t.Errorf("[%s] GET /api/matches/%s returned Result = %q, expected 'victory' (NOT draw)", b.name, guidOrangeUnset, det2.Result)
			}

			// 4. Test GET /api/matches/{guid} for Case 3 (Orange Defeat with winner_team = -1)
			reqDet3 := httptest.NewRequest(http.MethodGet, "/api/matches/"+guidOrangeDefeatUnset, nil)
			recDet3 := httptest.NewRecorder()
			handler.ServeHTTP(recDet3, reqDet3)
			if recDet3.Code != http.StatusOK {
				t.Fatalf("[%s] GET /api/matches/%s failed: %d", b.name, guidOrangeDefeatUnset, recDet3.Code)
			}
			var det3 storage.HistoricalMatchRecord
			_ = json.Unmarshal(recDet3.Body.Bytes(), &det3)
			if det3.Result != "defeat" {
				t.Errorf("[%s] GET /api/matches/%s returned Result = %q, expected 'defeat' (NOT draw)", b.name, guidOrangeDefeatUnset, det3.Result)
			}
		})
	}
}

// TestChallenger_Daemon_ConcurrentEndpointsRace stresses GET /api/players, GET /api/matches,
// and GET /api/matches/{guid} under high concurrency with -race on both SQLite and JSONStore.
func TestChallenger_Daemon_ConcurrentEndpointsRace(t *testing.T) {
	backends := []struct {
		name   string
		isJSON bool
	}{
		{"SQLite", false},
		{"JSONStore", true},
	}

	for _, b := range backends {
		t.Run(b.name, func(t *testing.T) {
			_, store, handler := newDaemonChallengerInstance(t, b.isJSON)
			ctx := context.Background()
			now := time.Now().UTC()

			localPID := "Steam|76561198000000011|0"
			_ = store.SaveAuthState(ctx, "steam", "auth-token", localPID, "ConcurrencyTester")

			for i := 0; i < 15; i++ {
				p := &storage.PlayerRecord{
					PlayerID:   fmt.Sprintf("Steam|p_conc_%02d|0", i),
					Platform:   "Steam",
					PlayerName: fmt.Sprintf("ConcPlayer_%02d", i),
					LastSeenAt: now.Add(time.Duration(i) * time.Minute),
				}
				_ = store.UpsertPlayer(ctx, p)

				wt := i % 2
				guid := fmt.Sprintf("guid-race-%02d", i)
				_ = store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
					MatchGUID:   guid,
					Playlist:    11,
					BlueScore:   i % 4,
					OrangeScore: (i + 1) % 4,
					WinnerTeam:  &wt,
					Date:        now.Add(time.Duration(i) * time.Minute).Format(time.RFC3339),
				})
				_ = store.SaveMatchPlayers(ctx, guid, []*storage.MatchPlayerRecord{
					{MatchGUID: guid, PlayerID: localPID, TeamNum: i % 2, IsLocal: true},
					{MatchGUID: guid, PlayerID: p.PlayerID, TeamNum: (i + 1) % 2, IsLocal: false},
				})
			}

			var wg sync.WaitGroup
			concurrency := 20
			iterations := 15

			for c := 0; c < concurrency; c++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					for iter := 0; iter < iterations; iter++ {
						// 1. GET /api/players with sorting & filtering
						sorts := []string{"win_rate", "match_count", "name", "last_seen"}
						orders := []string{"asc", "desc"}
						urlPlayers := fmt.Sprintf("/api/players?sort=%s&order=%s&limit=5", sorts[(workerID+iter)%len(sorts)], orders[(workerID+iter)%len(orders)])
						reqP := httptest.NewRequest(http.MethodGet, urlPlayers, nil)
						recP := httptest.NewRecorder()
						handler.ServeHTTP(recP, reqP)
						if recP.Code != http.StatusOK {
							t.Errorf("[%s] GET %s status=%d", b.name, urlPlayers, recP.Code)
						}

						// 2. GET /api/matches with result filter
						results := []string{"all", "victory", "defeat"}
						urlMatches := fmt.Sprintf("/api/matches?result=%s&limit=5", results[(workerID+iter)%len(results)])
						reqM := httptest.NewRequest(http.MethodGet, urlMatches, nil)
						recM := httptest.NewRecorder()
						handler.ServeHTTP(recM, reqM)
						if recM.Code != http.StatusOK {
							t.Errorf("[%s] GET %s status=%d", b.name, urlMatches, recM.Code)
						}

						// 3. GET /api/matches/{guid}
						guid := fmt.Sprintf("guid-race-%02d", (workerID+iter)%15)
						urlDet := "/api/matches/" + guid
						reqD := httptest.NewRequest(http.MethodGet, urlDet, nil)
						recD := httptest.NewRecorder()
						handler.ServeHTTP(recD, reqD)
						if recD.Code != http.StatusOK {
							t.Errorf("[%s] GET %s status=%d", b.name, urlDet, recD.Code)
						}
					}
				}(c)
			}

			wg.Wait()
		})
	}
}
