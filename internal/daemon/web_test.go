package daemon_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
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
	cfg.Web.Host = "0.0.0.0"
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

func TestWebIntegration_StaticAndSPAFallback(t *testing.T) {
	d, _, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	client := server.Client()

	// 1. Root / serves index.html
	respRoot, err := client.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	defer respRoot.Body.Close()

	if respRoot.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for /, got %d", respRoot.StatusCode)
	}
	if ct := respRoot.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html, got %s", ct)
	}
	bodyRoot, _ := io.ReadAll(respRoot.Body)
	if !strings.Contains(string(bodyRoot), `<div id="root"></div>`) {
		t.Errorf("expected index.html body to contain root div, got: %s", string(bodyRoot))
	}

	// 2. /index.html serves index.html with Cache-Control: no-cache
	respIndex, err := client.Get(server.URL + "/index.html")
	if err != nil {
		t.Fatalf("GET /index.html failed: %v", err)
	}
	defer respIndex.Body.Close()
	if respIndex.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for /index.html, got %d", respIndex.StatusCode)
	}
	if cc := respIndex.Header.Get("Cache-Control"); !strings.Contains(cc, "no-cache") {
		t.Errorf("expected no-cache Cache-Control for index.html, got %s", cc)
	}

	// 3. Static JS/CSS asset serving
	respJS, err := client.Get(server.URL + "/assets/index-ev8_Pgz-.js")
	if err != nil {
		t.Fatalf("GET asset JS failed: %v", err)
	}
	defer respJS.Body.Close()
	if respJS.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for static JS asset, got %d", respJS.StatusCode)
	}
	if ct := respJS.Header.Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("expected javascript Content-Type, got %s", ct)
	}

	// 4. SPA Fallback: /session-history
	respHistory, err := client.Get(server.URL + "/session-history")
	if err != nil {
		t.Fatalf("GET /session-history failed: %v", err)
	}
	defer respHistory.Body.Close()
	if respHistory.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for SPA fallback /session-history, got %d", respHistory.StatusCode)
	}
	bodyHistory, _ := io.ReadAll(respHistory.Body)
	if !strings.Contains(string(bodyHistory), `<div id="root"></div>`) {
		t.Errorf("expected index.html fallback for /session-history, got: %s", string(bodyHistory))
	}

	// 5. SPA Fallback: /overlay & /?mode=overlay
	respOverlay, err := client.Get(server.URL + "/overlay")
	if err != nil {
		t.Fatalf("GET /overlay failed: %v", err)
	}
	defer respOverlay.Body.Close()
	if respOverlay.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for /overlay, got %d", respOverlay.StatusCode)
	}

	respModeOverlay, err := client.Get(server.URL + "/?mode=overlay")
	if err != nil {
		t.Fatalf("GET /?mode=overlay failed: %v", err)
	}
	defer respModeOverlay.Body.Close()
	if respModeOverlay.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for /?mode=overlay, got %d", respModeOverlay.StatusCode)
	}
}

// -----------------------------------------------------------------------------
// 5. SSE Stream Delivery: /api/events (Snapshot, Push, Keepalive, Disconnect)
// -----------------------------------------------------------------------------

func TestWebIntegration_SSE_Stream(t *testing.T) {
	d, st, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/events", nil)
	if err != nil {
		t.Fatalf("creating SSE request failed: %v", err)
	}

	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /api/events failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for SSE, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("expected text/event-stream, got %s", ct)
	}

	reader := bufio.NewReader(resp.Body)

	lineChan := make(chan string, 100)
	errChan := make(chan error, 1)
	go func() {
		for {
			line, readErr := reader.ReadString('\n')
			if readErr != nil {
				select {
				case errChan <- readErr:
				default:
				}
				return
			}
			lineChan <- line
		}
	}()

	readEventWithTimeout := func(desc string, timeout time.Duration) (string, string) {
		var evType, data string
		timer := time.NewTimer(timeout)
		defer timer.Stop()

		for {
			select {
			case line := <-lineChan:
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "event:") {
					evType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				} else if strings.HasPrefix(line, "data:") {
					data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				} else if line == "" && evType != "" {
					return evType, data
				}
			case err := <-errChan:
				t.Fatalf("reading SSE stream for %s failed: %v", desc, err)
				return "", ""
			case <-timer.C:
				cancel()
				_ = resp.Body.Close()
				t.Fatalf("timed out after %v waiting for SSE %s", timeout, desc)
				return "", ""
			}
		}
	}

	// 1. Initial snapshot must be event: session_update
	initialEventType, initialData := readEventWithTimeout("initial snapshot (session_update)", 2*time.Second)

	if initialEventType != "session_update" {
		t.Errorf("expected initial event to be session_update, got %s", initialEventType)
	}
	if !strings.Contains(initialData, "session_id") {
		t.Errorf("expected initial data to contain session_id, got: %s", initialData)
	}

	// 2. Broadcast event distribution
	// Ensure the SSE subscriber is registered before broadcasting to prevent racing
	subDeadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(subDeadline) {
		if st.Broadcaster().SubscriberCount() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if count := st.Broadcaster().SubscriberCount(); count == 0 {
		t.Fatalf("timed out waiting for SSE subscriber registration: subscriber count is 0")
	}

	matchEvent := session.SessionEvent{
		Event: session.EventMatchUpdate,
		Data: map[string]interface{}{
			"active_match": true,
			"playlist_id":  13,
		},
	}
	st.Broadcaster().Broadcast(matchEvent)

	broadcastType, broadcastData := readEventWithTimeout("broadcast event (match_update)", 2*time.Second)

	if broadcastType != "match_update" {
		t.Errorf("expected broadcast event to be match_update, got %s", broadcastType)
	}
	if !strings.Contains(broadcastData, `"active_match":true`) {
		t.Errorf("expected broadcast data to contain active_match, got %s", broadcastData)
	}

	// 3. Client disconnection cleanup
	cancel()
	_ = resp.Body.Close()

	// Wait for subscriber channel to be removed
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if st.Broadcaster().SubscriberCount() == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if count := st.Broadcaster().SubscriberCount(); count != 0 {
		t.Errorf("expected 0 subscribers after client disconnect, got %d", count)
	}
}

// -----------------------------------------------------------------------------
// 6. CORS Headers: Simple Requests & Preflight OPTIONS
// -----------------------------------------------------------------------------

func TestWebIntegration_CORS(t *testing.T) {
	d, _, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	client := server.Client()

	endpoints := []string{
		"/api/session",
		"/api/current-match",
		"/api/players",
	}

	for _, ep := range endpoints {
		t.Run("GET_"+ep, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, server.URL+ep, nil)
			req.Header.Set("Origin", "http://localhost:5173")
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if acao := resp.Header.Get("Access-Control-Allow-Origin"); acao != "*" && acao != "http://localhost:5173" {
				t.Errorf("expected Access-Control-Allow-Origin, got %q", acao)
			}
		})
	}

	// OPTIONS Preflight
	t.Run("OPTIONS_Preflight_Reset", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodOptions, server.URL+"/api/session/reset", nil)
		req.Header.Set("Origin", "http://localhost:5173")
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "Content-Type")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("OPTIONS request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
			t.Errorf("expected 204 or 200 for OPTIONS, got %d", resp.StatusCode)
		}
		if acao := resp.Header.Get("Access-Control-Allow-Origin"); acao == "" {
			t.Error("missing Access-Control-Allow-Origin on preflight")
		}
		if acam := resp.Header.Get("Access-Control-Allow-Methods"); !strings.Contains(acam, "POST") {
			t.Errorf("expected POST in Access-Control-Allow-Methods, got %s", acam)
		}
		if acah := resp.Header.Get("Access-Control-Allow-Headers"); !strings.Contains(acah, "Content-Type") {
			t.Errorf("expected Content-Type in Access-Control-Allow-Headers, got %s", acah)
		}
	})
}

// -----------------------------------------------------------------------------
// 7. Network LAN Discovery & Banner Formatting Unit Tests
// -----------------------------------------------------------------------------

func TestNetwork_LAN_Discovery_Unit(t *testing.T) {
	// Test banner formatting with active LAN IP
	banner := daemon.FormatStartupBanner("0.0.0.0", 49125, "192.168.1.150")
	expected := "Web Dashboard:\n  Local:   http://localhost:49125\n  Network: http://192.168.1.150:49125\n  Overlay: http://localhost:49125/?mode=overlay"
	if banner != expected {
		t.Errorf("banner mismatch:\nexpected:\n%s\ngot:\n%s", expected, banner)
	}

	// Test banner with loopback bind
	bannerLoopback := daemon.FormatStartupBanner("127.0.0.1", 49125, "192.168.1.150")
	if !strings.Contains(bannerLoopback, "disabled") {
		t.Errorf("expected loopback banner to mention disabled, got:\n%s", bannerLoopback)
	}

	// Test banner with unavailable network
	bannerNoLAN := daemon.FormatStartupBanner("0.0.0.0", 49125, "")
	if !strings.Contains(bannerNoLAN, "unavailable") {
		t.Errorf("expected no-LAN banner to mention unavailable, got:\n%s", bannerNoLAN)
	}

	// Test real host discovery does not crash
	ip, err := daemon.DiscoverLANIPv4()
	if err == nil {
		if net.ParseIP(ip) == nil {
			t.Errorf("discovered invalid IP: %s", ip)
		}
	}
}

// -----------------------------------------------------------------------------
// 8. API Route 404 Guard & Security Verification
// -----------------------------------------------------------------------------

func TestWebIntegration_Api404Guard(t *testing.T) {
	d, _, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	client := server.Client()

	guardedPaths := []string{
		"/api",
		"/api/",
		"/api/invalid",
		"/api/nonexistent",
		"/api/session/unknown",
		"/api/players/../invalid",
	}

	for _, p := range guardedPaths {
		t.Run(p, func(t *testing.T) {
			resp, err := client.Get(server.URL + p)
			if err != nil {
				t.Fatalf("GET %s failed: %v", p, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("expected 404 Not Found for %s, got HTTP %d", p, resp.StatusCode)
			}
			body, _ := io.ReadAll(resp.Body)
			if strings.Contains(string(body), "<div id=\"root\"></div>") {
				t.Errorf("route %s erroneously fell back to SPA index.html", p)
			}
		})
	}
}
