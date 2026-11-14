package e2e

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
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
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
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
