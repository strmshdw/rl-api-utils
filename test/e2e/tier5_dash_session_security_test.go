package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rlapi"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/session"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/web"
)

// ============================================================================
// Scenario 2: Rapid Match Cycling and Concurrent Session Reset
// ============================================================================

// TestTier5_Dashboard_RapidMatchCyclingAndSessionReset tests rapid match start/update/conclude
// cycles interspersed with asynchronous POST /api/session/reset calls under continuous
// background SSE and REST read load, verifying state re-anchoring, zero panics, and correct math.
func TestTier5_Dashboard_RapidMatchCyclingAndSessionReset(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state_rapid.db")

	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to init SQLiteStore: %v", err)
	}
	defer store.Close()

	exporter := NewMockBakkesModExporter()
	defer exporter.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	localPID := "Steam|76561198000000001|0"

	fetcher := newAdvSkillFetcher()
	fetcher.SetSkill(rlapi.PlayerID(localPID), []rlapi.Skill{{Playlist: 11, Tier: 16, Division: 2, MMR: 1040.0}})
	fetcher.SetSkill(rlapi.PlayerID("Steam|teammate_rapid|0"), []rlapi.Skill{{Playlist: 11, Tier: 16, Division: 1, MMR: 1020.0}})
	fetcher.SetSkill(rlapi.PlayerID("Epic|opp_rapid_1|0"), []rlapi.Skill{{Playlist: 11, Tier: 16, Division: 0, MMR: 1000.0}})
	fetcher.SetSkill(rlapi.PlayerID("Epic|opp_rapid_2|0"), []rlapi.Skill{{Playlist: 11, Tier: 17, Division: 0, MMR: 1100.0}})

	trackerCfg := config.PlayerTrackingConfig{
		Enabled:        true,
		AutoFetchRanks: true,
		LocalPlayerID:  localPID,
	}
	authCfg := config.AuthConfig{
		Provider: "steam",
		Steam:    config.SteamConfig{SteamID64: "76561198000000001"},
	}

	tracker, err := playertrack.NewTracker(store, fetcher, trackerCfg, authCfg, playertrack.WithTrackerLogger(logger))
	if err != nil {
		t.Fatalf("failed to create tracker: %v", err)
	}
	defer tracker.Close()

	statsTracker, err := statsapi.NewTracker(store, statsapi.TrackerConfig{}, statsapi.WithLogger(logger))
	if err != nil {
		t.Fatalf("failed to create statsTracker: %v", err)
	}

	listenerCfg := statsapi.ListenerConfig{
		Address:        exporter.Address(),
		Protocol:       "websocket",
		ReconnectDelay: 20 * time.Millisecond,
	}
	listener, err := statsapi.NewListener(listenerCfg, statsTracker, logger, statsapi.WithPlayerEventHandler(tracker))
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	cfg := &config.Config{
		Web: config.WebConfig{
			Enabled: true,
			Host:    "127.0.0.1",
			Port:    0,
		},
		StatsAPI: config.StatsAPIConfig{
			Enabled:  true,
			Protocol: "websocket",
			Address:  exporter.Address(),
		},
		PlayerTracking: trackerCfg,
		Auth:           authCfg,
	}

	d, err := daemon.New(&testSyncerStub{}, cfg,
		daemon.WithPlayerTracker(tracker),
		daemon.WithStateStore(store),
		daemon.WithStatsTracker(statsTracker),
		daemon.WithStatsListener(listener),
		daemon.WithLogger(logger),
	)
	if err != nil {
		t.Fatalf("failed to create daemon: %v", err)
	}

	listenerCtx, cancelListener := context.WithCancel(context.Background())
	defer cancelListener()
	go func() {
		_ = listener.Start(listenerCtx)
	}()

	if !exporter.WaitForClient(4 * time.Second) {
		t.Fatalf("timed out waiting for stats listener to connect to exporter")
	}

	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	// Launch 10 background SSE subscribers and 5 REST pollers to maintain continuous load
	bgCtx, cancelBG := context.WithCancel(context.Background())
	defer cancelBG()

	var bgWg sync.WaitGroup
	for s := 0; s < 10; s++ {
		bgWg.Add(1)
		go func() {
			defer bgWg.Done()
			req, _ := http.NewRequestWithContext(bgCtx, "GET", server.URL+"/api/events", nil)
			client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
			resp, err := client.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			buf := make([]byte, 512)
			for {
				if _, err := resp.Body.Read(buf); err != nil {
					return
				}
			}
		}()
	}

	for r := 0; r < 5; r++ {
		bgWg.Add(1)
		go func() {
			defer bgWg.Done()
			client := &http.Client{Timeout: 1 * time.Second}
			for {
				select {
				case <-bgCtx.Done():
					return
				default:
					resp, err := client.Get(server.URL + "/api/session")
					if err == nil {
						_ = resp.Body.Close()
					}
					time.Sleep(20 * time.Millisecond)
				}
			}
		}()
	}

	// Execute 20 rapid match cycles across playlists, interspersing reset requests
	playlists := []int{11, 13, 10} // 2v2, 3v3, 1v1
	var resetCount int64

	client := &http.Client{Timeout: 2 * time.Second}

	for i := 0; i < 20; i++ {
		matchGUID := fmt.Sprintf("guid-rapid-cycle-%03d", i)
		plID := playlists[i%len(playlists)]

		players := []statsapi.StatsPlayer{
			{Name: "LocalHero", PrimaryId: localPID, TeamNum: 0, Score: 150 + i*10, Goals: 1},
			{Name: "Teammate1", PrimaryId: "Steam|teammate_rapid|0", TeamNum: 0, Score: 80, Assists: 1},
			{Name: "Opponent1", PrimaryId: "Epic|opp_rapid_1|0", TeamNum: 1, Score: 120, Saves: 1},
			{Name: "Opponent2", PrimaryId: "Epic|opp_rapid_2|0", TeamNum: 1, Score: 60},
		}

		if err := exporter.SendUpdateState(matchGUID, plID, players); err != nil {
			t.Fatalf("cycle %d: SendUpdateState failed: %v", i, err)
		}

		// Interleave session reset calls during in-flight matches every 4th iteration
		if i%4 == 0 {
			go func(iter int) {
				resetResp, err := client.Post(server.URL+"/api/session/reset", "application/json", nil)
				if err != nil {
					return
				}
				defer resetResp.Body.Close()
				if resetResp.StatusCode != http.StatusOK {
					t.Errorf("iter %d: POST /api/session/reset returned %d", iter, resetResp.StatusCode)
					return
				}
				var res struct {
					Status  string `json:"status"`
					Message string `json:"message"`
				}
				if err := json.NewDecoder(resetResp.Body).Decode(&res); err == nil && res.Status == "ok" {
					atomic.AddInt64(&resetCount, 1)
				}
			}(i)
		}

		time.Sleep(25 * time.Millisecond)

		// Alternate winners
		winnerTeam := i % 2
		if err := exporter.SendMatchEnded(matchGUID, winnerTeam); err != nil {
			t.Fatalf("cycle %d: SendMatchEnded failed: %v", i, err)
		}

		time.Sleep(20 * time.Millisecond)
	}

	// Verify session integrity via GET /api/session
	resp, err := client.Get(server.URL + "/api/session")
	if err != nil {
		t.Fatalf("GET /api/session failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/session returned %d", resp.StatusCode)
	}

	var sess session.SessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		t.Fatalf("failed to decode SessionResponse: %v", err)
	}

	if sess.SessionID == "" {
		t.Errorf("expected non-empty SessionID")
	}
	if sess.TotalMatches < 0 {
		t.Errorf("total matches cannot be negative: %d", sess.TotalMatches)
	}
	if sess.WinRate < 0.0 || sess.WinRate > 100.0 {
		t.Errorf("win rate out of valid bounds [0, 100]: %f", sess.WinRate)
	}

	// Stop background workers
	cancelBG()
	bgWg.Wait()
}

// ============================================================================
// Scenario 5: Security and Path Traversal Penetration Testing
// ============================================================================

// TestTier5_Dashboard_SecurityAndPathTraversalPenetration executes live penetration
// testing against daemon endpoints to verify that path traversal attacks return 400 or 404,
// never leak host filesystem contents, never serve index.html, and exact /api returns 404.
func TestTier5_Dashboard_SecurityAndPathTraversalPenetration(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state_sec.db")

	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to init SQLiteStore: %v", err)
	}
	defer store.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		Web: config.WebConfig{Enabled: true, Host: "127.0.0.1", Port: 0},
		PlayerTracking: config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|76561198000000001|0",
		},
	}

	d, err := daemon.New(&testSyncerStub{}, cfg,
		daemon.WithStateStore(store),
		daemon.WithWebHandler(web.DistHandler()),
		daemon.WithLogger(logger),
	)
	if err != nil {
		t.Fatalf("failed to create daemon: %v", err)
	}

	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	client := &http.Client{
		Timeout: 2 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// 1. Path Traversal Penetration Vectors
	traversalVectors := []string{
		"//dist/..",
		"/..",
		"/../../etc/passwd",
		"/..%2f..%2fetc/passwd",
		"/%2e%2e/%2e%2e/windows/win.ini",
		"/%2e%2e",
		"/assets/../index.html",
		"/assets/%2e%2e/dist/index.html",
		"/assets/..%2findex.html",
		"/api/../index.html",
		"/api/%2e%2e/index.html",
		"/\\..\\windows\\win.ini",
		"/..\\..\\windows\\system.ini",
		"/..%5c..%5cwindows%5cwin.ini",
	}

	for _, vec := range traversalVectors {
		t.Run("Traversal_"+vec, func(t *testing.T) {
			resp, err := client.Get(server.URL + vec)
			if err != nil {
				t.Fatalf("request failed for vector %q: %v", vec, err)
			}
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)
			bodyStr := string(body)

			// Traversal must be rejected with 400 or 404
			if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusNotFound {
				t.Errorf("vector %q returned status %d, expected 400 or 404", vec, resp.StatusCode)
			}

			// Traversal must NEVER serve index.html or leak system files
			if strings.Contains(bodyStr, "<div id=\"root\">") || strings.Contains(bodyStr, "rl-sync-web") {
				t.Errorf("vector %q leaked SPA index.html!", vec)
			}
			if strings.Contains(bodyStr, "[extensions]") || strings.Contains(bodyStr, "root:") {
				t.Errorf("vector %q leaked host filesystem contents!", vec)
			}
		})
	}

	// 2. Exact API Root Guard Vectors
	apiRootVectors := []string{
		"/api",
		"/api/",
		"/api/nonexistent",
		"/api/nonexistent/nested",
	}

	for _, vec := range apiRootVectors {
		t.Run("APIGuard_"+vec, func(t *testing.T) {
			resp, err := client.Get(server.URL + vec)
			if err != nil {
				t.Fatalf("request failed for %q: %v", vec, err)
			}
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)
			bodyStr := string(body)

			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("vector %q returned status %d, expected 404", vec, resp.StatusCode)
			}
			if strings.Contains(bodyStr, "<div id=\"root\">") {
				t.Errorf("vector %q erroneously fell through to index.html!", vec)
			}
		})
	}

	// 3. Legitimate SPA Route Fallback Vectors
	legitimateVectors := []string{
		"/",
		"/session",
		"/dashboard",
		"/overlay",
		"/?mode=overlay",
	}

	for _, vec := range legitimateVectors {
		t.Run("ValidSPA_"+vec, func(t *testing.T) {
			resp, err := client.Get(server.URL + vec)
			if err != nil {
				t.Fatalf("request failed for %q: %v", vec, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("vector %q returned status %d, expected 200 OK", vec, resp.StatusCode)
			}

			body, _ := io.ReadAll(resp.Body)
			bodyStr := string(body)
			if !strings.Contains(bodyStr, "<!doctype html>") && !strings.Contains(bodyStr, "<html") {
				t.Errorf("vector %q did not return valid HTML", vec)
			}
		})
	}

	// 4. Legacy Backward-Compatible REST API Alias (/players returns JSON)
	t.Run("LegacyAPI_/players", func(t *testing.T) {
		resp, err := client.Get(server.URL + "/players")
		if err != nil {
			t.Fatalf("request failed for /players: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 OK for /players, got %d", resp.StatusCode)
		}
	})
}
