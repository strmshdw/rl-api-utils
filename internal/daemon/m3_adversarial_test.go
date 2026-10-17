package daemon_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/session"
	"github.com/dank/rl-api-utils/internal/storage"
)

func setupChallengerDaemon(t *testing.T, isJSON bool) (*daemon.Daemon, storage.StateStore, http.Handler) {
	t.Helper()
	store := newChallengerTestStoreBackend(t, isJSON)

	cfg := &config.Config{
		Sync: config.SyncConfig{
			Once: false,
		},
		Web: config.WebConfig{
			Enabled: true,
			Port:    49125,
		},
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

func newChallengerTestStoreBackend(t *testing.T, isJSON bool) storage.StateStore {
	t.Helper()
	tempDir := t.TempDir()
	var dbPath string
	if isJSON {
		dbPath = tempDir + "/chal_store.json"
	} else {
		dbPath = tempDir + "/chal_store.db"
	}
	store, err := storage.NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

// TestM3_Adversarial_GetMatch_OrangeVictoryBug tests whether GET /api/matches/{guid}
// returns "victory" when the local player was on Orange (team 1) and Orange won.
func TestM3_Adversarial_GetMatch_OrangeVictoryBug(t *testing.T) {
	_, store, handler := setupChallengerDaemon(t, false)
	ctx := context.Background()
	localPID := "Steam|76561198000000000|0"

	_ = store.SaveAuthState(ctx, "steam", "dummy-refresh", localPID, "LocalPlayer")

	guid := "guid-orange-won-match"
	wtOrange := 1
	now := time.Now().UTC()

	// Orange won: Blue score 1, Orange score 3, WinnerTeam = 1
	err := store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
		MatchGUID:   guid,
		MapName:     "dfh_stadium",
		Playlist:    11,
		BlueScore:   1,
		OrangeScore: 3,
		WinnerTeam:  &wtOrange,
		Date:        now.Format(time.RFC3339),
		Players: []storage.ReplayPlayerStats{
			{Name: "LocalPlayer", Platform: "Steam", OnlineID: localPID, Team: 1, Score: 500, Goals: 3},
			{Name: "Opponent", Platform: "Epic", OnlineID: "opp-1", Team: 0, Score: 200, Goals: 1},
		},
	})
	if err != nil {
		t.Fatalf("SyncReplayStats failed: %v", err)
	}

	// Local player is on Orange (team 1)
	wonTrue := true
	_ = store.SaveMatchPlayers(ctx, guid, []*storage.MatchPlayerRecord{
		{MatchGUID: guid, PlayerID: localPID, TeamNum: 1, IsLocal: true, Won: &wonTrue},
	})

	// 1. Verify GET /api/matches returns this match with Result = "victory"
	reqList := httptest.NewRequest(http.MethodGet, "/api/matches?result=victory", nil)
	recList := httptest.NewRecorder()
	handler.ServeHTTP(recList, reqList)

	if recList.Code != http.StatusOK {
		t.Fatalf("GET /api/matches?result=victory status=%d", recList.Code)
	}
	var listResp daemon.MatchesResponse
	if err := json.Unmarshal(recList.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal MatchesResponse failed: %v", err)
	}
	if listResp.Total != 1 || len(listResp.Matches) != 1 {
		t.Fatalf("expected 1 match in /api/matches?result=victory, got total=%d, len=%d", listResp.Total, len(listResp.Matches))
	}
	if listResp.Matches[0].Result != "victory" {
		t.Errorf("expected list match result = victory, got %q", listResp.Matches[0].Result)
	}

	// 2. Query GET /api/matches/{guid}
	reqDetail := httptest.NewRequest(http.MethodGet, "/api/matches/"+guid, nil)
	recDetail := httptest.NewRecorder()
	handler.ServeHTTP(recDetail, reqDetail)

	if recDetail.Code != http.StatusOK {
		t.Fatalf("GET /api/matches/%s status=%d", guid, recDetail.Code)
	}
	var detailResp storage.HistoricalMatchRecord
	if err := json.Unmarshal(recDetail.Body.Bytes(), &detailResp); err != nil {
		t.Fatalf("unmarshal HistoricalMatchRecord failed: %v", err)
	}

	t.Logf("GET /api/matches (list) Result: %s", listResp.Matches[0].Result)
	t.Logf("GET /api/matches/{guid} (detail) Result: %s", detailResp.Result)

	if detailResp.Result != "victory" {
		t.Errorf("[BUG FOUND] GET /api/matches/%s returned Result = %q, expected 'victory' because local player was on Orange and Orange won 3-1! (handleGetMatch hardcodes Blue>Orange as victory and Orange>Blue as defeat)", guid, detailResp.Result)
	}
}

// TestM3_Adversarial_PlayerMatches_EdgeCases tests GET /api/players/{id}/matches:
// - Non-existent player
// - limit=0, limit=1000, offset=-5
func TestM3_Adversarial_PlayerMatches_EdgeCases(t *testing.T) {
	_, _, handler := setupChallengerDaemon(t, false)

	// Non-existent player
	t.Run("NonExistentPlayer", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/players/Steam|nonexistent|0/matches", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK with empty matches, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp daemon.PlayerMatchesResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json unmarshal failed: %v", err)
		}
		if resp.Total != 0 || len(resp.Matches) != 0 {
			t.Errorf("expected 0 matches for non-existent player, got total=%d, len=%d", resp.Total, len(resp.Matches))
		}
	})

	// Clamping limit=0 -> 1, limit=1000 -> 100, offset=-5 -> 0
	t.Run("LimitOffsetClamping", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/players/Steam|test|0/matches?limit=0&offset=-5", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}
		var resp daemon.PlayerMatchesResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Limit != 1 {
			t.Errorf("expected limit clamped to 1, got %d", resp.Limit)
		}
		if resp.Offset != 0 {
			t.Errorf("expected offset normalized to 0, got %d", resp.Offset)
		}

		req2 := httptest.NewRequest(http.MethodGet, "/api/players/Steam|test|0/matches?limit=1000", nil)
		rec2 := httptest.NewRecorder()
		handler.ServeHTTP(rec2, req2)
		var resp2 daemon.PlayerMatchesResponse
		_ = json.Unmarshal(rec2.Body.Bytes(), &resp2)
		if resp2.Limit != 100 {
			t.Errorf("expected limit clamped to 100, got %d", resp2.Limit)
		}
	})
}

// TestM3_Adversarial_RetryMatch_EdgeCases tests POST /api/matches/{guid}/retry:
// - Non-existent GUID -> 404 Not Found
// - CSRF attack with forbidden Origin -> 403 Forbidden
// - URL-encoded GUID handling
func TestM3_Adversarial_RetryMatch_EdgeCases(t *testing.T) {
	_, store, handler := setupChallengerDaemon(t, false)
	ctx := context.Background()

	// Seed one failed match
	existingGUID := "guid-real-failed"
	_ = store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{
		{
			MatchGUID:      existingGUID,
			DownloadStatus: storage.DownloadFailed,
			UploadStatus:   storage.UploadFailed,
			RetryCount:     2,
		},
	})

	// 1. Non-existent GUID
	t.Run("NonExistentGUID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/matches/non-existent-guid/retry", nil)
		req.Header.Set("Origin", "http://localhost:49125")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404 Not Found for non-existent match retry, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// 2. CSRF Attack with hostile Origin
	t.Run("CSRFBlocked", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/matches/"+existingGUID+"/retry", nil)
		req.Header.Set("Origin", "https://malicious-website.com")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for CSRF Origin, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// 3. URL-encoded GUID
	t.Run("URLEncodedGUID", func(t *testing.T) {
		specialGUID := "guid:special/item_1"
		_ = store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{
			{
				MatchGUID:      specialGUID,
				DownloadStatus: storage.DownloadFailed,
				UploadStatus:   storage.UploadFailed,
				RetryCount:     1,
			},
		})

		req := httptest.NewRequest(http.MethodPost, "/api/matches/guid%3Aspecial%2Fitem_1/retry", nil)
		req.Header.Set("Origin", "http://localhost:49125")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Logf("URL encoded GUID response: %d, body: %s", rec.Code, rec.Body.String())
		}
	})
}

// TestM3_Adversarial_ConcurrencyStressUnderRace tests high-concurrency requests
// across /api/players, /api/matches, and /api/players/{id}/matches under race detector.
func TestM3_Adversarial_ConcurrencyStressUnderRace(t *testing.T) {
	_, store, handler := setupChallengerDaemon(t, false)
	ctx := context.Background()
	now := time.Now().UTC()

	// Seed players and matches
	for i := 0; i < 20; i++ {
		p := &storage.PlayerRecord{
			PlayerID:   fmt.Sprintf("Steam|765611980000000%02d|0", i),
			Platform:   "Steam",
			PlayerName: fmt.Sprintf("Player_%02d", i),
			LastSeenAt: now.Add(time.Duration(i) * time.Minute),
		}
		_ = store.UpsertPlayer(ctx, p)

		wt := i % 2
		_ = store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
			MatchGUID:   fmt.Sprintf("m-conc-%d", i),
			Playlist:    11,
			BlueScore:   2,
			OrangeScore: 1,
			WinnerTeam:  &wt,
			Date:        now.Add(time.Duration(i) * time.Minute).Format(time.RFC3339),
		})
	}

	var wg sync.WaitGroup
	concurrency := 40
	iterations := 20

	for c := 0; c < concurrency; c++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for iter := 0; iter < iterations; iter++ {
				// Query 1: GET /api/players with various roles and sorts
				roles := []string{"all", "frequent", "teammate", "opponent", "synergy"}
				sorts := []string{"match_count", "win_rate", "name", "last_seen"}
				urlP := fmt.Sprintf("/api/players?role=%s&sort=%s&limit=10", roles[(workerID+iter)%len(roles)], sorts[(workerID+iter)%len(sorts)])
				reqP := httptest.NewRequest(http.MethodGet, urlP, nil)
				recP := httptest.NewRecorder()
				handler.ServeHTTP(recP, reqP)
				if recP.Code != http.StatusOK {
					t.Errorf("worker %d GET /api/players failed: %d", workerID, recP.Code)
				}

				// Query 2: GET /api/matches
				results := []string{"all", "victory", "defeat", "draw"}
				urlM := fmt.Sprintf("/api/matches?result=%s&limit=10", results[(workerID+iter)%len(results)])
				reqM := httptest.NewRequest(http.MethodGet, urlM, nil)
				recM := httptest.NewRecorder()
				handler.ServeHTTP(recM, reqM)
				if recM.Code != http.StatusOK {
					t.Errorf("worker %d GET /api/matches failed: %d", workerID, recM.Code)
				}

				// Query 3: GET /api/players/{id}/matches
				pid := fmt.Sprintf("Steam|765611980000000%02d|0", (workerID+iter)%20)
				urlPM := fmt.Sprintf("/api/players/%s/matches?limit=5", pid)
				reqPM := httptest.NewRequest(http.MethodGet, urlPM, nil)
				recPM := httptest.NewRecorder()
				handler.ServeHTTP(recPM, reqPM)
				if recPM.Code != http.StatusOK {
					t.Errorf("worker %d GET /api/players/{id}/matches failed: %d", workerID, recPM.Code)
				}
			}
		}(c)
	}

	wg.Wait()
}
