package daemon_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/session"
	"github.com/dank/rl-api-utils/internal/storage"
)

// setupWebTestDaemon constructs a Daemon configured with SQLite in-memory store,
// player tracker, and session tracker for comprehensive integration testing.
func setupWebTestDaemon(t *testing.T) (*daemon.Daemon, *session.SessionTracker, *storage.SQLiteStore) {
	t.Helper()

	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create in-memory sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	sessionTracker := session.NewSessionTracker()

	cfg := config.NewDefaultConfig()
	cfg.Web.Enabled = true
	cfg.Web.Host = "127.0.0.1"
	cfg.Web.Port = 49125
	cfg.PlayerTracking.Enabled = true

	playerTracker, err := playertrack.NewTracker(
		store,
		playertrack.NewNoOpRankClient(),
		cfg.PlayerTracking,
		cfg.Auth,
	)
	if err != nil {
		t.Fatalf("failed to create player tracker: %v", err)
	}
	t.Cleanup(func() { _ = playerTracker.Close() })

	playerTracker.SetMatchStateListener(sessionTracker)

	d, err := daemon.New(
		newMockSyncer(),
		cfg,
		daemon.WithStore(store),
		daemon.WithPlayerTracker(playerTracker),
		daemon.WithSessionTracker(sessionTracker),
	)
	if err != nil {
		t.Fatalf("failed to create daemon: %v", err)
	}

	return d, sessionTracker, store
}

// -----------------------------------------------------------------------------
// 1. Endpoints: /api/session & /api/session/reset
// -----------------------------------------------------------------------------

func TestWebIntegration_SessionEndpoints(t *testing.T) {
	d, st, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	client := server.Client()

	// 1. GET /api/session initial state
	resp, err := client.Get(server.URL + "/api/session")
	if err != nil {
		t.Fatalf("GET /api/session failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("expected application/json, got %s", ct)
	}

	var sessionResp session.SessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&sessionResp); err != nil {
		t.Fatalf("failed to decode session JSON: %v", err)
	}
	if sessionResp.SessionID == "" {
		t.Error("expected non-empty session_id")
	}
	if sessionResp.TotalMatches != 0 || sessionResp.TotalWins != 0 || sessionResp.TotalLosses != 0 {
		t.Errorf("expected 0 matches, got %+v", sessionResp)
	}

	// 2. Conclude match and verify session updates
	winner := 0
	localTeam := 0
	concludedMatch := &playertrack.CurrentMatchResponse{
		ActiveMatch: false,
		MatchGUID:   "match-guid-1",
		PlaylistID:  13,
		LocalTeam:   &localTeam,
		WinnerTeam:  &winner,
		Result:      "victory",
		LocalPlayer: &playertrack.LobbyPlayer{
			PlayerID: "Steam|76561198000000001|0",
			Name:     "TestPlayer",
			TeamNum:  0,
			Stats: playertrack.PlayerStatsSummary{
				Score: 450,
				Goals: 2,
			},
			CurrentRank: &playertrack.PlayerPlaylistRank{
				MMR: 1050.0,
			},
		},
		Teammates: []playertrack.LobbyPlayer{
			{
				PlayerID: "Steam|76561198000000001|0",
				Name:     "TestPlayer",
				TeamNum:  0,
				Stats:    playertrack.PlayerStatsSummary{Goals: 2},
			},
		},
		Opponents: []playertrack.LobbyPlayer{
			{
				PlayerID: "Epic|opp1|0",
				Name:     "Opponent1",
				TeamNum:  1,
				Stats:    playertrack.PlayerStatsSummary{Goals: 1},
			},
		},
	}
	st.ConcludeMatch(concludedMatch)

	resp2, err := client.Get(server.URL + "/api/session")
	if err != nil {
		t.Fatalf("GET /api/session failed: %v", err)
	}
	defer resp2.Body.Close()

	var sessionResp2 session.SessionResponse
	if err := json.NewDecoder(resp2.Body).Decode(&sessionResp2); err != nil {
		t.Fatalf("failed to decode updated session JSON: %v", err)
	}
	if sessionResp2.TotalMatches != 1 || sessionResp2.TotalWins != 1 {
		t.Errorf("expected 1 match and 1 win, got %+v", sessionResp2)
	}
	if len(sessionResp2.Matches) != 1 {
		t.Errorf("expected 1 match detail in history, got %d", len(sessionResp2.Matches))
	}

	// 3. POST /api/session/reset
	resetResp, err := client.Post(server.URL+"/api/session/reset", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/session/reset failed: %v", err)
	}
	defer resetResp.Body.Close()

	if resetResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for reset, got %d", resetResp.StatusCode)
	}

	var resetBody map[string]string
	if err := json.NewDecoder(resetResp.Body).Decode(&resetBody); err != nil {
		t.Fatalf("failed to decode reset response: %v", err)
	}
	if resetBody["status"] != "ok" {
		t.Errorf("expected status: ok, got %v", resetBody)
	}

	// 4. Verify GET /api/session is reset
	resp3, err := client.Get(server.URL + "/api/session")
	if err != nil {
		t.Fatalf("GET /api/session failed: %v", err)
	}
	defer resp3.Body.Close()

	var sessionResp3 session.SessionResponse
	if err := json.NewDecoder(resp3.Body).Decode(&sessionResp3); err != nil {
		t.Fatalf("failed to decode reset session JSON: %v", err)
	}
	if sessionResp3.TotalMatches != 0 {
		t.Errorf("expected 0 matches after reset, got %d", sessionResp3.TotalMatches)
	}
	if sessionResp3.SessionID == sessionResp.SessionID {
		t.Errorf("expected new session ID after reset, got same: %s", sessionResp3.SessionID)
	}

	// 5. Method Not Allowed on reset endpoint (GET)
	badMethodResp, err := client.Get(server.URL + "/api/session/reset")
	if err != nil {
		t.Fatalf("GET /api/session/reset failed: %v", err)
	}
	defer badMethodResp.Body.Close()
	if badMethodResp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 Method Not Allowed for GET /api/session/reset, got %d", badMethodResp.StatusCode)
	}
}

// -----------------------------------------------------------------------------
// 2. Endpoints: /api/current-match & Legacy Parity
// -----------------------------------------------------------------------------

func TestWebIntegration_CurrentMatch(t *testing.T) {
	d, _, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	client := server.Client()

	// 1. Idle match response on /api/current-match
	resp, err := client.Get(server.URL + "/api/current-match")
	if err != nil {
		t.Fatalf("GET /api/current-match failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var matchResp playertrack.CurrentMatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&matchResp); err != nil {
		t.Fatalf("failed to decode current-match JSON: %v", err)
	}
	if matchResp.ActiveMatch {
		t.Errorf("expected active_match = false, got true")
	}

	// 2. Legacy /current-match parity
	legacyResp, err := client.Get(server.URL + "/current-match")
	if err != nil {
		t.Fatalf("GET /current-match failed: %v", err)
	}
	defer legacyResp.Body.Close()

	if legacyResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for legacy route, got %d", legacyResp.StatusCode)
	}
	var legacyMatchResp playertrack.CurrentMatchResponse
	if err := json.NewDecoder(legacyResp.Body).Decode(&legacyMatchResp); err != nil {
		t.Fatalf("failed to decode legacy current-match JSON: %v", err)
	}
	if legacyMatchResp.ActiveMatch != matchResp.ActiveMatch {
		t.Errorf("legacy mismatch: expected %v, got %v", matchResp.ActiveMatch, legacyMatchResp.ActiveMatch)
	}
}

// -----------------------------------------------------------------------------
// 3. Endpoints: /api/players (Search, Platform Filter, Pagination, Parity)
// -----------------------------------------------------------------------------

func TestWebIntegration_PlayerDirectory(t *testing.T) {
	d, _, store := setupWebTestDaemon(t)
	ctx := context.Background()

	// Seed 3 players
	p1 := &storage.PlayerRecord{
		PlayerID:    "Steam|76561198000000001|0",
		PlayerName:  "AlphaStrike",
		Platform:    "Steam",
		FirstSeenAt: time.Now().Add(-1 * time.Hour),
		LastSeenAt:  time.Now(),
	}
	p2 := &storage.PlayerRecord{
		PlayerID:    "Epic|beta-account-id|0",
		PlayerName:  "BetaBravo",
		Platform:    "Epic",
		FirstSeenAt: time.Now().Add(-2 * time.Hour),
		LastSeenAt:  time.Now(),
	}
	p3 := &storage.PlayerRecord{
		PlayerID:    "Steam|76561198000000002|0",
		PlayerName:  "CharlieDelta",
		Platform:    "Steam",
		FirstSeenAt: time.Now().Add(-3 * time.Hour),
		LastSeenAt:  time.Now(),
	}

	if err := store.UpsertPlayer(ctx, p1); err != nil {
		t.Fatalf("failed to upsert p1: %v", err)
	}
	if err := store.UpsertPlayer(ctx, p2); err != nil {
		t.Fatalf("failed to upsert p2: %v", err)
	}
	if err := store.UpsertPlayer(ctx, p3); err != nil {
		t.Fatalf("failed to upsert p3: %v", err)
	}

	outcome := []storage.PlayerOutcome{
		{
			PlayerID:   "Steam|76561198000000001|0",
			Platform:   "Steam",
			PlayerName: "AlphaStrike",
			IsTeammate: true,
			Won:        true,
		},
	}
	if err := store.RecordMatchResults(ctx, "guid-seed-1", 13, outcome); err != nil {
		t.Fatalf("failed to record matchup: %v", err)
	}

	server := httptest.NewServer(d.Handler(ctx))
	defer server.Close()
	client := server.Client()

	// 1. GET /api/players (All)
	resp, err := client.Get(server.URL + "/api/players")
	if err != nil {
		t.Fatalf("GET /api/players failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var searchResp daemon.PlayerSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		t.Fatalf("failed to decode /api/players JSON: %v", err)
	}
	if searchResp.Total != 3 || len(searchResp.Players) != 3 {
		t.Errorf("expected 3 players, got total=%d, len=%d", searchResp.Total, len(searchResp.Players))
	}

	// 2. Query filter: ?query=alpha
	respQ, err := client.Get(server.URL + "/api/players?query=alpha")
	if err != nil {
		t.Fatalf("GET /api/players?query=alpha failed: %v", err)
	}
	defer respQ.Body.Close()

	var searchRespQ daemon.PlayerSearchResponse
	if err := json.NewDecoder(respQ.Body).Decode(&searchRespQ); err != nil {
		t.Fatalf("failed to decode searchRespQ: %v", err)
	}
	if searchRespQ.Total != 1 || len(searchRespQ.Players) != 1 || searchRespQ.Players[0].PlayerName != "AlphaStrike" {
		t.Errorf("expected AlphaStrike, got %+v", searchRespQ)
	}

	// 3. Platform filter: ?platform=Epic
	respP, err := client.Get(server.URL + "/api/players?platform=Epic")
	if err != nil {
		t.Fatalf("GET /api/players?platform=Epic failed: %v", err)
	}
	defer respP.Body.Close()

	var searchRespP daemon.PlayerSearchResponse
	if err := json.NewDecoder(respP.Body).Decode(&searchRespP); err != nil {
		t.Fatalf("failed to decode searchRespP: %v", err)
	}
	if searchRespP.Total != 1 || len(searchRespP.Players) != 1 || searchRespP.Players[0].PlayerName != "BetaBravo" {
		t.Errorf("expected BetaBravo, got %+v", searchRespP)
	}

	// 4. Pagination: ?limit=1&offset=1
	respPag, err := client.Get(server.URL + "/api/players?limit=1&offset=1")
	if err != nil {
		t.Fatalf("GET /api/players?limit=1&offset=1 failed: %v", err)
	}
	defer respPag.Body.Close()

	var searchRespPag daemon.PlayerSearchResponse
	if err := json.NewDecoder(respPag.Body).Decode(&searchRespPag); err != nil {
		t.Fatalf("failed to decode searchRespPag: %v", err)
	}
	if len(searchRespPag.Players) != 1 || searchRespPag.Total != 3 || searchRespPag.Offset != 1 {
		t.Errorf("unexpected pagination response: %+v", searchRespPag)
	}

	// 5. GET /api/players/{id}
	respDetail, err := client.Get(server.URL + "/api/players/Steam%7C76561198000000001%7C0")
	if err != nil {
		t.Fatalf("GET player detail failed: %v", err)
	}
	defer respDetail.Body.Close()

	if respDetail.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for player detail, got %d", respDetail.StatusCode)
	}

	var detail daemon.PlayerDetailResponse
	if err := json.NewDecoder(respDetail.Body).Decode(&detail); err != nil {
		t.Fatalf("failed to decode player detail: %v", err)
	}
	if detail.PlayerID != "Steam|76561198000000001|0" || len(detail.Matchups) != 1 {
		t.Errorf("unexpected player detail: %+v", detail)
	}

	// 6. Legacy /players returns array
	legacyResp, err := client.Get(server.URL + "/players")
	if err != nil {
		t.Fatalf("GET /players failed: %v", err)
	}
	defer legacyResp.Body.Close()

	var legacyArray []*storage.PlayerSummary
	if err := json.NewDecoder(legacyResp.Body).Decode(&legacyArray); err != nil {
		t.Fatalf("failed to decode legacy players array: %v", err)
	}
	if len(legacyArray) != 3 {
		t.Errorf("expected 3 players in legacy array, got %d", len(legacyArray))
	}
}

// -----------------------------------------------------------------------------
// 4. Static Asset Serving & SPA Fallback (/session-history, /overlay, etc.)
// -----------------------------------------------------------------------------
