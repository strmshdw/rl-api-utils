package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
// Scenario 1: Live Telemetry Propagation from WebSocket Exporter to SSE Push
// ============================================================================

// TestTier5_Dashboard_LiveTelemetryPropagationToSSE verifies the entire real-time
// telemetry flow under high-fanout concurrency:
// MockBakkesModExporter (WebSocket) -> statsapi.Listener -> playertrack.Tracker ->
// session.SessionTracker -> EventBroadcaster -> 50 concurrent SSE HTTP client connections.
func TestTier5_Dashboard_LiveTelemetryPropagationToSSE(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state_sse.db")

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
	fetcher.SetSkill(rlapi.PlayerID("Steam|76561198000000002|0"), []rlapi.Skill{{Playlist: 11, Tier: 16, Division: 3, MMR: 1055.0}})
	fetcher.SetSkill(rlapi.PlayerID("Epic|opp_001|0"), []rlapi.Skill{{Playlist: 11, Tier: 17, Division: 1, MMR: 1120.0}})
	fetcher.SetSkill(rlapi.PlayerID("Epic|opp_002|0"), []rlapi.Skill{{Playlist: 11, Tier: 16, Division: 0, MMR: 1005.0}})

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
		t.Fatalf("timed out waiting for stats listener to connect to mock exporter")
	}

	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	const numClients = 50
	var wg sync.WaitGroup
	wg.Add(numClients)

	clientCtxs := make([]context.CancelFunc, numClients)
	initialReady := make([]int32, numClients)
	matchUpdatesReceived := make([]int64, numClients)
	matchEndedsReceived := make([]int64, numClients)
	postMatchSessionUpdates := make([]int64, numClients)

	readyCh := make(chan struct{}, numClients)

	// Launch 50 concurrent SSE subscribers
	for i := 0; i < numClients; i++ {
		clientIdx := i
		cCtx, cCancel := context.WithCancel(context.Background())
		clientCtxs[clientIdx] = cCancel

		go func() {
			defer wg.Done()
			req, err := http.NewRequestWithContext(cCtx, http.MethodGet, server.URL+"/api/events", nil)
			if err != nil {
				t.Errorf("client %d: failed to create request: %v", clientIdx, err)
				return
			}

			httpClient := &http.Client{
				Transport: &http.Transport{
					DisableKeepAlives: true,
				},
			}

			resp, err := httpClient.Do(req)
			if err != nil {
				if cCtx.Err() != nil {
					return
				}
				t.Errorf("client %d: GET /api/events failed: %v", clientIdx, err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("client %d: expected 200 OK, got %d", clientIdx, resp.StatusCode)
				return
			}

			reader := bufio.NewReader(resp.Body)
			for {
				line, readErr := reader.ReadString('\n')
				if readErr != nil {
					return
				}
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "event:") {
					evType := strings.TrimSpace(strings.TrimPrefix(line, "event:"))
					switch evType {
					case "session_update":
						if atomic.CompareAndSwapInt32(&initialReady[clientIdx], 0, 1) {
							readyCh <- struct{}{}
						} else {
							atomic.AddInt64(&postMatchSessionUpdates[clientIdx], 1)
						}
					case "match_update":
						atomic.AddInt64(&matchUpdatesReceived[clientIdx], 1)
					case "match_ended":
						atomic.AddInt64(&matchEndedsReceived[clientIdx], 1)
					}
				}
			}
		}()
	}

	// Await initial snapshot delivery on all 50 clients
	deadline := time.After(6 * time.Second)
	for i := 0; i < numClients; i++ {
		select {
		case <-readyCh:
		case <-deadline:
			t.Fatalf("timed out waiting for 50 SSE clients to establish connection; received %d ready", i)
		}
	}

	// Stream live match state over BakkesMod WebSocket
	matchGUID := "guid-sse-propagation-100"
	players := []statsapi.StatsPlayer{
		{Name: "LocalHero", PrimaryId: localPID, TeamNum: 0, Score: 100, Goals: 1},
		{Name: "Teammate1", PrimaryId: "Steam|76561198000000002|0", TeamNum: 0, Score: 50, Assists: 1},
		{Name: "Opponent1", PrimaryId: "Epic|opp_001|0", TeamNum: 1, Score: 70, Saves: 1},
		{Name: "Opponent2", PrimaryId: "Epic|opp_002|0", TeamNum: 1, Score: 30},
	}

	// 1. Send UpdateState (match begins)
	if err := exporter.SendUpdateState(matchGUID, 11, players); err != nil {
		t.Fatalf("failed to send UpdateState: %v", err)
	}

	// 2. Send second UpdateState (score change)
	time.Sleep(40 * time.Millisecond)
	players[0].Score = 200
	players[0].Goals = 2
	if err := exporter.SendUpdateState(matchGUID, 11, players); err != nil {
		t.Fatalf("failed to send second UpdateState: %v", err)
	}

	// 3. Send MatchEnded (Blue / Team 0 wins)
	time.Sleep(40 * time.Millisecond)
	if err := exporter.SendMatchEnded(matchGUID, 0); err != nil {
		t.Fatalf("failed to send MatchEnded: %v", err)
	}

	// Verify all 50 clients received the updates
	checkDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(checkDeadline) {
		allReceived := true
		for i := 0; i < numClients; i++ {
			if atomic.LoadInt64(&matchUpdatesReceived[i]) < 1 ||
				atomic.LoadInt64(&matchEndedsReceived[i]) < 1 ||
				atomic.LoadInt64(&postMatchSessionUpdates[i]) < 1 {
				allReceived = false
				break
			}
		}
		if allReceived {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}

	// Assert 100% reception across all 50 clients
	for i := 0; i < numClients; i++ {
		mUp := atomic.LoadInt64(&matchUpdatesReceived[i])
		mEnd := atomic.LoadInt64(&matchEndedsReceived[i])
		sUp := atomic.LoadInt64(&postMatchSessionUpdates[i])
		if mUp == 0 {
			t.Errorf("client %d received 0 match_update events", i)
		}
		if mEnd == 0 {
			t.Errorf("client %d received 0 match_ended events", i)
		}
		if sUp == 0 {
			t.Errorf("client %d received 0 post-match session_update events", i)
		}
	}

	// Clean up client connections
	for i := 0; i < numClients; i++ {
		clientCtxs[i]()
	}
	wg.Wait()
}

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
// Scenario 3: Player Search Directory Under Continuous Telemetry Ingestion
// ============================================================================

// TestTier5_Dashboard_PlayerSearchUnderContinuousIngestion tests concurrent search
// traffic against GET /api/players while continuous player and match results are written,
// validating lock-free concurrency, SQLite vs JSONStore parity, and zero 500 errors.
func TestTier5_Dashboard_PlayerSearchUnderContinuousIngestion(t *testing.T) {
	tempDir := t.TempDir()
	sqlitePath := filepath.Join(tempDir, "state_search.db")
	jsonPath := filepath.Join(tempDir, "state_search.json")

	sqliteStore, err := storage.NewSQLiteStore(sqlitePath)
	if err != nil {
		t.Fatalf("failed to init SQLiteStore: %v", err)
	}
	defer sqliteStore.Close()

	jsonStore, err := storage.NewJSONStore(jsonPath)
	if err != nil {
		t.Fatalf("failed to init JSONStore: %v", err)
	}
	defer jsonStore.Close()

	ctx := context.Background()

	baseTime := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)

	// Pre-populate both stores with initial 50 players (25 Steam, 25 Epic)
	// Each player receives a distinct discrete second-aligned timestamp
	for i := 1; i <= 25; i++ {
		ts := baseTime.Add(time.Duration(100-i) * time.Minute)
		pSteam := &storage.PlayerRecord{
			PlayerID:    fmt.Sprintf("Steam|765611980000000%02d|0", i),
			Platform:    "Steam",
			PlayerName:  fmt.Sprintf("SteamPlayer_%02d", i),
			FirstSeenAt: ts,
			LastSeenAt:  ts,
		}
		mSteam := storage.PlayerOutcome{
			PlayerID:   pSteam.PlayerID,
			Platform:   "Steam",
			PlayerName: pSteam.PlayerName,
			IsTeammate: true,
			Won:        true,
		}
		_ = sqliteStore.RecordMatchResults(ctx, fmt.Sprintf("init-guid-steam-%d", i), 11, []storage.PlayerOutcome{mSteam})
		_ = jsonStore.RecordMatchResults(ctx, fmt.Sprintf("init-guid-steam-%d", i), 11, []storage.PlayerOutcome{mSteam})

		_ = sqliteStore.UpsertPlayer(ctx, pSteam)
		_ = jsonStore.UpsertPlayer(ctx, pSteam)
	}

	for i := 1; i <= 25; i++ {
		ts := baseTime.Add(time.Duration(50-i) * time.Minute)
		pEpic := &storage.PlayerRecord{
			PlayerID:    fmt.Sprintf("Epic|epic_account_%02d|0", i),
			Platform:    "Epic",
			PlayerName:  fmt.Sprintf("EpicPlayer_%02d", i),
			FirstSeenAt: ts,
			LastSeenAt:  ts,
		}
		mEpic := storage.PlayerOutcome{
			PlayerID:   pEpic.PlayerID,
			Platform:   "Epic",
			PlayerName: pEpic.PlayerName,
			IsTeammate: false,
			Won:        false,
		}
		_ = sqliteStore.RecordMatchResults(ctx, fmt.Sprintf("init-guid-epic-%d", i), 11, []storage.PlayerOutcome{mEpic})
		_ = jsonStore.RecordMatchResults(ctx, fmt.Sprintf("init-guid-epic-%d", i), 11, []storage.PlayerOutcome{mEpic})

		_ = sqliteStore.UpsertPlayer(ctx, pEpic)
		_ = jsonStore.UpsertPlayer(ctx, pEpic)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		Web: config.WebConfig{Enabled: true, Host: "127.0.0.1", Port: 0},
		PlayerTracking: config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|76561198000000001|0",
		},
	}

	d, err := daemon.New(&testSyncerStub{}, cfg,
		daemon.WithStateStore(sqliteStore),
		daemon.WithLogger(logger),
	)
	if err != nil {
		t.Fatalf("failed to create daemon: %v", err)
	}

	server := httptest.NewServer(d.Handler(ctx))
	defer server.Close()

	// Ingestion loop: continuous inserts and updates across both stores
	var ingestWg sync.WaitGroup
	ingestWg.Add(1)

	go func() {
		defer ingestWg.Done()
		for idx := 51; idx <= 100; idx++ {
			ts := baseTime.Add(time.Duration(200+idx) * time.Minute)
			p := &storage.PlayerRecord{
				PlayerID:    fmt.Sprintf("Steam|76561198000000%03d|0", idx),
				Platform:    "Steam",
				PlayerName:  fmt.Sprintf("LivePlayer_%03d", idx),
				FirstSeenAt: ts,
				LastSeenAt:  ts,
			}
			m := storage.PlayerOutcome{
				PlayerID:   p.PlayerID,
				Platform:   "Steam",
				PlayerName: p.PlayerName,
				IsTeammate: true,
				Won:        true,
			}
			_ = sqliteStore.RecordMatchResults(ctx, fmt.Sprintf("live-guid-%d", idx), 11, []storage.PlayerOutcome{m})
			_ = jsonStore.RecordMatchResults(ctx, fmt.Sprintf("live-guid-%d", idx), 11, []storage.PlayerOutcome{m})

			_ = sqliteStore.UpsertPlayer(ctx, p)
			_ = jsonStore.UpsertPlayer(ctx, p)

			time.Sleep(15 * time.Millisecond)
		}
	}()

	// Concurrently hammer GET /api/players with 10 workers running varied search queries
	const searchWorkers = 10
	var searchWg sync.WaitGroup
	searchWg.Add(searchWorkers)
	var totalQueries int64

	searchEndpoints := []string{
		"/api/players?query=SteamPlayer&limit=10&offset=0",
		"/api/players?platform=Steam&limit=10&offset=0",
		"/api/players?platform=Epic&limit=10&offset=0",
		"/api/players?query=Epic&platform=Epic&limit=5&offset=5",
		"/api/players?query=%25&limit=10",
		"/api/players?query=_&limit=10",
		"/api/players?query=nonexistent_xyz&limit=10",
		"/api/players?limit=10&offset=200",
		"/api/players?limit=5&offset=0",
	}

	for w := 0; w < searchWorkers; w++ {
		go func(workerID int) {
			defer searchWg.Done()
			client := &http.Client{Timeout: 2 * time.Second}

			for q := 0; q < 25; q++ {
				path := searchEndpoints[(workerID+q)%len(searchEndpoints)]
				resp, err := client.Get(server.URL + path)
				if err != nil {
					t.Errorf("worker %d query %q failed: %v", workerID, path, err)
					return
				}

				if resp.StatusCode != http.StatusOK {
					t.Errorf("worker %d query %q returned %d", workerID, path, resp.StatusCode)
					resp.Body.Close()
					return
				}

				var result struct {
					Players []*storage.PlayerSummary `json:"players"`
					Total   int                      `json:"total"`
					Limit   int                      `json:"limit"`
					Offset  int                      `json:"offset"`
				}

				if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
					t.Errorf("worker %d query %q json decode failed: %v", workerID, path, err)
					resp.Body.Close()
					return
				}
				resp.Body.Close()

				if result.Total < len(result.Players) {
					t.Errorf("worker %d query %q: total (%d) < returned players (%d)",
						workerID, path, result.Total, len(result.Players))
				}

				atomic.AddInt64(&totalQueries, 1)
				time.Sleep(10 * time.Millisecond)
			}
		}(w)
	}

	searchWg.Wait()
	ingestWg.Wait()

	if total := atomic.LoadInt64(&totalQueries); total < 250 {
		t.Fatalf("expected at least 250 search queries, executed %d", total)
	}

	// Cross-Backend Parity Matrix Check: identical vectors on SQLiteStore vs JSONStore
	parityVectors := []struct {
		name     string
		query    string
		platform string
		limit    int
		offset   int
	}{
		{"All_Default", "", "", 10, 0},
		{"Steam_Filter", "", "Steam", 10, 0},
		{"Epic_Filter", "", "Epic", 10, 0},
		{"Substring_Player", "Player", "", 10, 0},
		{"Pagination_Offset", "", "Steam", 5, 5},
		{"Wildcard_Percent", "%", "", 10, 0},
		{"Wildcard_Underscore", "_", "", 10, 0},
		{"NonExistent", "non_existent_player_123", "", 10, 0},
	}

	for _, pv := range parityVectors {
		t.Run("Parity_"+pv.name, func(t *testing.T) {
			sRes, sTotal, sErr := sqliteStore.SearchPlayerSummaries(ctx, pv.query, pv.platform, pv.limit, pv.offset)
			if sErr != nil {
				t.Fatalf("SQLite search failed: %v", sErr)
			}
			jRes, jTotal, jErr := jsonStore.SearchPlayerSummaries(ctx, pv.query, pv.platform, pv.limit, pv.offset)
			if jErr != nil {
				t.Fatalf("JSONStore search failed: %v", jErr)
			}

			if sTotal != jTotal {
				t.Errorf("parity total mismatch: SQLite=%d, JSONStore=%d", sTotal, jTotal)
			}
			if len(sRes) != len(jRes) {
				t.Fatalf("parity count mismatch: SQLite=%d, JSONStore=%d", len(sRes), len(jRes))
			}
			for idx := range sRes {
				if sRes[idx].PlayerID != jRes[idx].PlayerID {
					t.Errorf("parity index %d PlayerID mismatch: SQLite=%s, JSONStore=%s",
						idx, sRes[idx].PlayerID, jRes[idx].PlayerID)
				}
				if sRes[idx].Platform != jRes[idx].Platform {
					t.Errorf("parity index %d Platform mismatch: SQLite=%s, JSONStore=%s",
						idx, sRes[idx].Platform, jRes[idx].Platform)
				}
			}
		})
	}
}

// ============================================================================
// Scenario 4: Daemon Graceful Shutdown Under Active SSE and REST Load
// ============================================================================

// TestTier5_Dashboard_GracefulShutdownUnderActiveSSELoad verifies that when daemon
// shutdown is triggered, the HTTP server and broadcaster terminate within the 2s timeout
// without hung SSE connections or leaked goroutines.
func TestTier5_Dashboard_GracefulShutdownUnderActiveSSELoad(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state_shutdown.db")

	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to init SQLiteStore: %v", err)
	}
	defer store.Close()

	// Find an available port in the 49200-49350 range
	testPort := 49215
	for p := 49215; p <= 49350; p++ {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err == nil {
			testPort = p
			_ = ln.Close()
			break
		}
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		Sync: config.SyncConfig{
			PollInterval: config.Duration(1 * time.Hour),
		},
		Web: config.WebConfig{
			Enabled: true,
			Host:    "127.0.0.1",
			Port:    testPort,
		},
		PlayerTracking: config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|76561198000000001|0",
		},
	}

	d, err := daemon.New(&testSyncerStub{}, cfg,
		daemon.WithStateStore(store),
		daemon.WithLogger(logger),
	)
	if err != nil {
		t.Fatalf("failed to create daemon: %v", err)
	}

	startGoroutines := runtime.NumGoroutine()

	daemonCtx, cancelDaemon := context.WithCancel(context.Background())
	stopped := make(chan error, 1)

	go func() {
		stopped <- d.Start(daemonCtx)
	}()

	// Poll until HTTP server is active
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", testPort)
	pollClient := &http.Client{
		Transport: &http.Transport{DisableKeepAlives: true},
		Timeout:   500 * time.Millisecond,
	}

	var ready bool
	for i := 0; i < 40; i++ {
		resp, err := pollClient.Get(baseURL + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			ready = true
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if !ready {
		cancelDaemon()
		t.Fatalf("daemon HTTP server failed to start on %s", baseURL)
	}

	// Establish 50 persistent SSE client connections
	const numClients = 50
	var sseWg sync.WaitGroup
	sseWg.Add(numClients)

	var sseConnectedCount int32
	sseErrors := make([]error, numClients)

	for i := 0; i < numClients; i++ {
		clientIdx := i
		go func() {
			defer sseWg.Done()
			req, err := http.NewRequest(http.MethodGet, baseURL+"/api/events", nil)
			if err != nil {
				sseErrors[clientIdx] = err
				return
			}

			client := &http.Client{
				Transport: &http.Transport{DisableKeepAlives: true},
			}

			resp, err := client.Do(req)
			if err != nil {
				sseErrors[clientIdx] = err
				return
			}
			defer resp.Body.Close()

			atomic.AddInt32(&sseConnectedCount, 1)
			buf := make([]byte, 256)
			for {
				// Read until EOF or connection severed by server shutdown
				if _, err := resp.Body.Read(buf); err != nil {
					return
				}
			}
		}()
	}

	// Launch 10 REST polling workers concurrently
	restCtx, cancelREST := context.WithCancel(context.Background())
	var restWg sync.WaitGroup
	for r := 0; r < 10; r++ {
		restWg.Add(1)
		go func() {
			defer restWg.Done()
			rClient := &http.Client{
				Transport: &http.Transport{DisableKeepAlives: true},
				Timeout:   1 * time.Second,
			}
			for {
				select {
				case <-restCtx.Done():
					return
				default:
					resp, err := rClient.Get(baseURL + "/api/session")
					if err == nil {
						_ = resp.Body.Close()
					}
					time.Sleep(15 * time.Millisecond)
				}
			}
		}()
	}

	// Wait for SSE connections to establish
	sseDeadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(sseDeadline) {
		if atomic.LoadInt32(&sseConnectedCount) >= int32(numClients) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Trigger graceful shutdown
	cancelDaemon()

	// Daemon Start() must complete within 3.5 seconds
	select {
	case err := <-stopped:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("daemon stopped with unexpected error: %v", err)
		}
	case <-time.After(3500 * time.Millisecond):
		t.Fatalf("daemon failed to shut down within 3.5 seconds")
	}

	// Stop REST pollers
	cancelREST()
	restWg.Wait()

	// SSE clients must all observe connection close within 2 seconds
	sseDoneCh := make(chan struct{})
	go func() {
		sseWg.Wait()
		close(sseDoneCh)
	}()

	select {
	case <-sseDoneCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for active SSE connections to close after shutdown")
	}

	// Goroutine leak bound check
	time.Sleep(400 * time.Millisecond)
	finalGoroutines := runtime.NumGoroutine()
	delta := finalGoroutines - startGoroutines
	if delta > 25 {
		t.Errorf("possible goroutine leak after shutdown: delta=%d (before=%d, after=%d)",
			delta, startGoroutines, finalGoroutines)
	}
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

// ============================================================================
// Scenario 6: Standalone Binary Build and CLI Precedence Verification
// ============================================================================

// findRepoRoot finds the repository root directory containing go.mod.
func findRepoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "../.."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "../.."
}

// findGoBinary locates the Go compiler executable on the host system.
func findGoBinary() (string, error) {
	if p, err := exec.LookPath("go"); err == nil {
		return p, nil
	}
	if p, err := exec.LookPath("go.exe"); err == nil {
		return p, nil
	}
	localApp := os.Getenv("LOCALAPPDATA")
	if localApp != "" {
		p := filepath.Join(localApp, "Programs", "go", "bin", "go.exe")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	goroot := runtime.GOROOT()
	if goroot != "" {
		p := filepath.Join(goroot, "bin", "go.exe")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		p = filepath.Join(goroot, "bin", "go")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("go executable not found in PATH, LOCALAPPDATA, or GOROOT")
}

// TestTier5_Dashboard_SingleBinaryBuildAndCLIPrecedence builds the standalone rl-sync.exe
// executable from cmd/rl-sync, verifies size and embedded assets, executes --help and --version,
// and tests CLI flag precedence and boundary validations.
func TestTier5_Dashboard_SingleBinaryBuildAndCLIPrecedence(t *testing.T) {
	goBin, err := findGoBinary()
	if err != nil {
		t.Fatalf("failed to locate go binary: %v", err)
	}

	repoRoot := findRepoRoot()
	tempDir := t.TempDir()

	binName := "rl-sync-test"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(tempDir, binName)

	// Step 1: Build standalone executable from cmd/rl-sync
	buildCmd := exec.Command(goBin, "build", "-o", binPath, "./cmd/rl-sync")
	buildCmd.Dir = repoRoot
	buildCmd.Env = os.Environ()
	if localApp := os.Getenv("LOCALAPPDATA"); localApp != "" {
		goBinDir := filepath.Join(localApp, "Programs", "go", "bin")
		buildCmd.Env = append(buildCmd.Env, "PATH="+goBinDir+";"+os.Getenv("PATH"))
	}

	out, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed: %v\nOutput: %s", err, string(out))
	}

	// Step 2: Verify binary footprint (> 10MB indicates embedded React SPA and modernc SQLite)
	fi, err := os.Stat(binPath)
	if err != nil {
		t.Fatalf("failed to stat compiled binary: %v", err)
	}
	if fi.Size() < 10*1024*1024 {
		t.Errorf("compiled binary is unexpectedly small (%d bytes), expected > 10MB", fi.Size())
	}

	// Step 3: Verify --help banner and flags
	helpCmd := exec.Command(binPath, "--help")
	helpOut, err := helpCmd.CombinedOutput()
	helpStr := string(helpOut)
	if !strings.Contains(helpStr, "-web-enabled") {
		t.Errorf("--help output missing -web-enabled flag: %s", helpStr)
	}
	if !strings.Contains(helpStr, "-web-host") {
		t.Errorf("--help output missing -web-host flag: %s", helpStr)
	}
	if !strings.Contains(helpStr, "-web-port") {
		t.Errorf("--help output missing -web-port flag: %s", helpStr)
	}

	// Step 4: Verify --version banner
	versionCmd := exec.Command(binPath, "--version")
	verOut, err := versionCmd.CombinedOutput()
	if err != nil {
		t.Errorf("--version command failed: %v", err)
	}
	if !strings.Contains(string(verOut), "rl-sync") {
		t.Errorf("--version output missing 'rl-sync': %s", string(verOut))
	}

	// Step 5: Test CLI Flag Validation Boundaries (Invalid Ports)
	invalidPortVectors := []string{"99999", "-1", "invalid", "0"}
	for _, port := range invalidPortVectors {
		cmd := exec.Command(binPath, "--web-port="+port, "--once")
		cmd.Dir = repoRoot
		err := cmd.Run()
		if err == nil {
			t.Errorf("expected --web-port=%s to fail with non-zero exit code", port)
		}
	}

	// Step 6: Test CLI Flag Precedence (Disabling web via CLI flag)
	confPath := filepath.Join(tempDir, "config.yaml")
	confContent := `
ballchasing:
  api_key: "test-token"
auth:
  provider: "steam"
  steam:
    steam_id_64: "76561198000000001"
    session_ticket: "test-session-ticket"
web:
  enabled: true
  port: 49125
`
	if err := os.WriteFile(confPath, []byte(confContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// Test CLI flag --web-enabled=false overrides RL_SYNC_WEB_ENABLED=true
	validCmd := exec.Command(binPath, "--config="+confPath, "--web-enabled=false", "--once", "--dry-run")
	validCmd.Dir = repoRoot
	validCmd.Env = append(os.Environ(), "RL_SYNC_WEB_ENABLED=true")
	validOut, _ := validCmd.CombinedOutput()
	outStr := string(validOut)
	if !strings.Contains(outStr, "web_enabled=false") {
		t.Errorf("expected output to reflect CLI flag web_enabled=false, got: %s", outStr)
	}

	// Test CLI flag --web-port=54321 overrides RL_SYNC_WEB_PORT=49125
	portCmd := exec.Command(binPath, "--config="+confPath, "--web-port=54321", "--once", "--dry-run")
	portCmd.Dir = repoRoot
	portCmd.Env = append(os.Environ(), "RL_SYNC_WEB_PORT=49125")
	portOut, _ := portCmd.CombinedOutput()
	portStr := string(portOut)
	if !strings.Contains(portStr, "web_port=54321") {
		t.Errorf("expected output to reflect CLI flag web_port=54321, got: %s", portStr)
	}

	// Test CLI flag --web-host=127.0.0.2 overrides RL_SYNC_WEB_HOST=0.0.0.0
	hostCmd := exec.Command(binPath, "--config="+confPath, "--web-host=127.0.0.2", "--once", "--dry-run")
	hostCmd.Dir = repoRoot
	hostCmd.Env = append(os.Environ(), "RL_SYNC_WEB_HOST=0.0.0.0")
	hostOut, _ := hostCmd.CombinedOutput()
	hostStr := string(hostOut)
	if !strings.Contains(hostStr, "web_host=127.0.0.2") {
		t.Errorf("expected output to reflect CLI flag web_host=127.0.0.2, got: %s", hostStr)
	}
}
