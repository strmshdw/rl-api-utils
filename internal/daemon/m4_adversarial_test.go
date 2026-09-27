package daemon_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/session"
	"github.com/dank/rl-api-utils/internal/statsapi"
)

// -----------------------------------------------------------------------------
// Adversarial Test 1: 50 Concurrent SSE Subscribers & Abrupt Disconnections
// -----------------------------------------------------------------------------

func TestM4_Adversarial_SSE_50ClientsAndAbruptDisconnect(t *testing.T) {
	d, st, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	const numClients = 50
	var wg sync.WaitGroup
	wg.Add(numClients)

	clientCtxs := make([]context.CancelFunc, numClients)
	eventsReceived := make([]int64, numClients)
	initialSnapshotReceived := make([]int32, numClients)
	readyChan := make(chan struct{}, numClients)

	// Launch 50 concurrent SSE subscribers
	for i := 0; i < numClients; i++ {
		clientIdx := i
		ctx, cancel := context.WithCancel(context.Background())
		clientCtxs[clientIdx] = cancel

		go func() {
			defer wg.Done()

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/events", nil)
			if err != nil {
				t.Errorf("client %d: request creation failed: %v", clientIdx, err)
				return
			}

			tr := &http.Transport{
				DisableKeepAlives: true,
			}
			httpClient := &http.Client{Transport: tr}

			resp, err := httpClient.Do(req)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				t.Errorf("client %d: GET /api/events failed: %v", clientIdx, err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("client %d: expected 200, got %d", clientIdx, resp.StatusCode)
				return
			}

			reader := bufio.NewReader(resp.Body)
			readyNotified := false

			for {
				line, readErr := reader.ReadString('\n')
				if readErr != nil {
					return
				}
				line = strings.TrimSpace(line)

				if strings.HasPrefix(line, "event:") {
					evType := strings.TrimSpace(strings.TrimPrefix(line, "event:"))
					if evType == "session_update" && atomic.LoadInt32(&initialSnapshotReceived[clientIdx]) == 0 {
						atomic.StoreInt32(&initialSnapshotReceived[clientIdx], 1)
						if !readyNotified {
							readyChan <- struct{}{}
							readyNotified = true
						}
					}
					atomic.AddInt64(&eventsReceived[clientIdx], 1)
				}
			}
		}()
	}

	// Await all 50 clients receiving initial snapshot
	timeout := time.After(5 * time.Second)
	readyCount := 0
	for readyCount < numClients {
		select {
		case <-readyChan:
			readyCount++
		case <-timeout:
			t.Fatalf("timed out waiting for 50 clients to connect, only %d connected", readyCount)
		}
	}

	// Verify broadcaster has 50 subscribers active
	if subCount := st.Broadcaster().SubscriberCount(); subCount < numClients {
		t.Fatalf("expected at least %d subscribers, got %d", numClients, subCount)
	}

	// Concurrently broadcast events while abruptly disconnecting half of the clients
	var broadcastWg sync.WaitGroup
	broadcastWg.Add(1)

	go func() {
		defer broadcastWg.Done()
		for b := 0; b < 20; b++ {
			st.Broadcaster().Broadcast(session.SessionEvent{
				Event: session.EventMatchUpdate,
				Data: map[string]interface{}{
					"round":        b,
					"active_match": true,
					"score":        b * 100,
				},
			})
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// Abruptly terminate connections for the first 25 clients midway through broadcasting
	time.Sleep(50 * time.Millisecond)
	for i := 0; i < numClients/2; i++ {
		clientCtxs[i]() // abrupt socket teardown
	}

	// Wait for broadcast to finish
	broadcastWg.Wait()

	// Send another batch of events to ensure server didn't panic or deadlock on severed channels
	for b := 0; b < 10; b++ {
		st.Broadcaster().Broadcast(session.SessionEvent{
			Event: session.EventSessionUpdate,
			Data: map[string]interface{}{
				"round":         b + 100,
				"total_matches": b,
			},
		})
		time.Sleep(5 * time.Millisecond)
	}

	// Verify surviving clients continued receiving events
	for i := numClients / 2; i < numClients; i++ {
		received := atomic.LoadInt64(&eventsReceived[i])
		if received < 10 {
			t.Errorf("surviving client %d received only %d events, expected >= 10", i, received)
		}
	}

	// Disconnect remaining clients
	for i := numClients / 2; i < numClients; i++ {
		clientCtxs[i]()
	}

	// Wait for all client goroutines to exit
	wg.Wait()

	// Verify subscriber count drops back to 0
	cleanupDeadline := time.Now().Add(2 * time.Second)
	var finalSubCount int
	for time.Now().Before(cleanupDeadline) {
		finalSubCount = st.Broadcaster().SubscriberCount()
		if finalSubCount == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if finalSubCount != 0 {
		t.Fatalf("goroutine/subscriber leak: expected 0 subscribers after all disconnects, got %d", finalSubCount)
	}
}

// -----------------------------------------------------------------------------
// Adversarial Test 2: SSE Initial Snapshot With Active Match
// -----------------------------------------------------------------------------

func TestM4_Adversarial_SSE_InitialSnapshot_WithActiveMatch(t *testing.T) {
	d, st, store := setupWebTestDaemon(t)
	_ = d
	_ = store

	// Pre-seed an active match through player tracker observer pipeline
	cfg := config.NewDefaultConfig()
	playerTracker, err := playertrack.NewTracker(
		store,
		playertrack.NewNoOpRankClient(),
		cfg.PlayerTracking,
		cfg.Auth,
	)
	if err != nil {
		t.Fatalf("failed to create player tracker: %v", err)
	}
	defer playerTracker.Close()
	playerTracker.SetMatchStateListener(st)

	players := []statsapi.StatsPlayer{
		{Name: "Hero", PrimaryId: "Steam|76561198000000001|0", TeamNum: 0},
		{Name: "Villain", PrimaryId: "Epic|villain_id|0", TeamNum: 1},
	}
	if err := playerTracker.OnUpdateState(context.Background(), "guid-active-test", 13, players); err != nil {
		t.Fatalf("OnUpdateState failed: %v", err)
	}

	dWithTracker, err := daemon.New(
		newMockSyncer(),
		cfg,
		daemon.WithStore(store),
		daemon.WithPlayerTracker(playerTracker),
		daemon.WithSessionTracker(st),
	)
	if err != nil {
		t.Fatalf("failed to construct daemon: %v", err)
	}

	server := httptest.NewServer(dWithTracker.Handler(context.Background()))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/events", nil)
	if err != nil {
		t.Fatalf("request creation failed: %v", err)
	}

	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /api/events failed: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	receivedEvents := make(map[string]string)

	for len(receivedEvents) < 2 {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			break
		}
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "event:") {
			evType := strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			for {
				dataLine, dErr := reader.ReadString('\n')
				if dErr != nil {
					break
				}
				dataLine = strings.TrimSpace(dataLine)
				if strings.HasPrefix(dataLine, "data:") {
					receivedEvents[evType] = strings.TrimSpace(strings.TrimPrefix(dataLine, "data:"))
					break
				}
			}
		}
	}

	if _, ok := receivedEvents["session_update"]; !ok {
		t.Error("expected initial snapshot to contain session_update")
	}
	if _, ok := receivedEvents["match_update"]; !ok {
		t.Error("expected initial snapshot to contain match_update when active match exists in playerTracker")
	}
}

// TestM4_Adversarial_SSE_ActiveMatchSessionFallbackBug demonstrates that if playerTracker is idle
// (ActiveMatch: false) but sessionTracker has an active match recorded, sse.go ignores summary.ActiveMatch
// due to the 'else if' branch and fails to emit the initial match_update snapshot to SSE clients.
func TestM4_Adversarial_SSE_ActiveMatchSessionFallbackBug(t *testing.T) {
	d, st, store := setupWebTestDaemon(t)
	_ = store

	// Pre-seed active match directly on sessionTracker while playerTracker remains idle
	activeMatch := &playertrack.CurrentMatchResponse{
		ActiveMatch: true,
		MatchGUID:   "session-only-guid-123",
		PlaylistID:  13,
		LocalPlayer: &playertrack.LobbyPlayer{
			PlayerID: "Steam|76561198000000001|0",
			Name:     "SessionHero",
		},
	}
	st.RecordActiveMatch(activeMatch)

	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/events", nil)
	if err != nil {
		t.Fatalf("request creation failed: %v", err)
	}

	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /api/events failed: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	receivedMatchUpdate := false

	// Read lines until timeout
	readDeadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(readDeadline) {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			break
		}
		if strings.TrimSpace(line) == "event: match_update" {
			receivedMatchUpdate = true
			break
		}
	}

	if !receivedMatchUpdate {
		t.Errorf("expected initial SSE snapshot to contain match_update when playerTracker is idle and summary.ActiveMatch exists")
	}
}

// -----------------------------------------------------------------------------
// Adversarial Test 3: Concurrent REST Mutation & Read Stress Hammer
// -----------------------------------------------------------------------------

func TestM4_Adversarial_ConcurrentRESTHammerWhileSSEActive(t *testing.T) {
	d, st, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Maintain 10 background SSE streams
	const sseWorkers = 10
	var sseWg sync.WaitGroup
	sseWg.Add(sseWorkers)

	for i := 0; i < sseWorkers; i++ {
		go func() {
			defer sseWg.Done()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/events", nil)
			if err != nil {
				return
			}
			resp, err := server.Client().Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body)
		}()
	}

	// Hammer GET /api/session and POST /api/session/reset concurrently
	const readerCount = 15
	const writerCount = 5
	var opWg sync.WaitGroup
	opWg.Add(readerCount + writerCount)

	var totalReads int64
	var totalResets int64

	// Readers hammering GET /api/session
	for r := 0; r < readerCount; r++ {
		go func() {
			defer opWg.Done()
			client := &http.Client{Timeout: 1 * time.Second}
			for ctx.Err() == nil {
				resp, err := client.Get(server.URL + "/api/session")
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					t.Errorf("GET /api/session failed: %v", err)
					return
				}
				if resp.StatusCode != http.StatusOK {
					t.Errorf("GET /api/session unexpected status: %d", resp.StatusCode)
				}
				var summary session.SessionResponse
				if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
					t.Errorf("failed to decode session summary: %v", err)
				}
				_ = resp.Body.Close()
				atomic.AddInt64(&totalReads, 1)
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}

	// Writers hammering POST /api/session/reset
	for w := 0; w < writerCount; w++ {
		go func() {
			defer opWg.Done()
			client := &http.Client{Timeout: 1 * time.Second}
			for ctx.Err() == nil {
				resp, err := client.Post(server.URL+"/api/session/reset", "application/json", nil)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					t.Errorf("POST /api/session/reset failed: %v", err)
					return
				}
				if resp.StatusCode != http.StatusOK {
					t.Errorf("POST /api/session/reset unexpected status: %d", resp.StatusCode)
				}
				var res map[string]string
				if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
					t.Errorf("failed to decode reset response: %v", err)
				} else if res["status"] != "ok" {
					t.Errorf("expected status ok, got %v", res)
				}
				_ = resp.Body.Close()
				atomic.AddInt64(&totalResets, 1)
				time.Sleep(15 * time.Millisecond)
			}
		}()
	}

	// Concurrently simulate live match concluded events
	go func() {
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		mIndex := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				mIndex++
				w := mIndex % 2
				st.ConcludeMatch(&playertrack.CurrentMatchResponse{
					MatchGUID:  fmt.Sprintf("guid-%d", mIndex),
					PlaylistID: 13,
					WinnerTeam: &w,
					LocalTeam:  &w,
					Result:     "victory",
				})
			}
		}
	}()

	<-ctx.Done()
	opWg.Wait()
	sseWg.Wait()

	reads := atomic.LoadInt64(&totalReads)
	resets := atomic.LoadInt64(&totalResets)
	if reads < 30 {
		t.Errorf("expected at least 30 reads during stress test, got %d", reads)
	}
	if resets < 5 {
		t.Errorf("expected at least 5 resets during stress test, got %d", resets)
	}
}

// -----------------------------------------------------------------------------
// Adversarial Test 4: Boundary & Method Check on /api/session/reset
// -----------------------------------------------------------------------------

func TestM4_Adversarial_Boundary_SessionResetMethods(t *testing.T) {
	d, _, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	client := server.Client()

	// 1. POST succeeds
	resp, err := client.Post(server.URL+"/api/session/reset", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/session/reset failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for POST /api/session/reset, got %d", resp.StatusCode)
	}

	// 2. Disallowed methods must return 405 Method Not Allowed
	disallowed := []string{
		http.MethodGet,
		http.MethodPut,
		http.MethodDelete,
		http.MethodPatch,
		http.MethodHead,
	}

	for _, m := range disallowed {
		t.Run("Method_"+m, func(t *testing.T) {
			req, err := http.NewRequest(m, server.URL+"/api/session/reset", nil)
			if err != nil {
				t.Fatalf("request creation failed: %v", err)
			}
			res, err := client.Do(req)
			if err != nil {
				t.Fatalf("%s /api/session/reset failed: %v", m, err)
			}
			defer res.Body.Close()

			if res.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf("expected 405 Method Not Allowed for %s, got %d", m, res.StatusCode)
			}

			allow := res.Header.Get("Allow")
			if !strings.Contains(allow, "POST") {
				t.Errorf("expected Allow header to contain POST, got %q", allow)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Adversarial Test 5: API Invalid Routes 404 Guard
// -----------------------------------------------------------------------------

func TestM4_Adversarial_Boundary_InvalidAPIRoutes(t *testing.T) {
	d, _, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	client := server.Client()

	routes := []struct {
		name string
		path string
	}{
		{name: "invalid route under /api/", path: "/api/invalid_route"},
		{name: "nonexistent endpoint", path: "/api/does_not_exist"},
		{name: "invalid session subpath", path: "/api/session/unknown"},
		{name: "invalid players subpath", path: "/api/players/../invalid"},
		{name: "invalid events subpath", path: "/api/events/sub"},
		{name: "exact api root", path: "/api"},
	}

	for _, tc := range routes {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := client.Get(server.URL + tc.path)
			if err != nil {
				t.Fatalf("GET %s failed: %v", tc.path, err)
			}
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)
			bodyStr := string(body)

			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("expected 404 Not Found for %s, got HTTP %d", tc.path, resp.StatusCode)
			}
			if strings.Contains(bodyStr, "<div id=\"root\"></div>") {
				t.Errorf("SECURITY/ROUTING BUG: route %s fell back to SPA index.html instead of returning 404!", tc.path)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Adversarial Test 6: Static Assets Path Traversal Boundary & Security
// -----------------------------------------------------------------------------

func TestM4_Adversarial_Security_PathTraversalStaticAssets(t *testing.T) {
	d, _, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	traversalPaths := []struct {
		name string
		path string
	}{
		{name: "parent traversal to etc passwd", path: "/../../etc/passwd"},
		{name: "deep parent traversal to win.ini", path: "/../../../windows/win.ini"},
		{name: "double slash dist parent", path: "//dist/.."},
		{name: "assets parent traversal", path: "/assets/../../secret.txt"},
		{name: "assets relative to index", path: "/assets/../index.html"},
		{name: "url encoded slash traversal", path: "/..%2f..%2fetc/passwd"},
		{name: "url encoded dot traversal", path: "/%2e%2e/%2e%2e/etc/passwd"},
	}

	for _, tc := range traversalPaths {
		t.Run(tc.name, func(t *testing.T) {
			// Test 1: Using client without auto-redirect to inspect raw immediate response
			tr := &http.Transport{
				DialContext: (&net.Dialer{}).DialContext,
			}
			noRedirectClient := &http.Client{
				Transport: tr,
				CheckRedirect: func(req *http.Request, via []*http.Request) error {
					return http.ErrUseLastResponse
				},
			}

			req, err := http.NewRequest(http.MethodGet, server.URL+tc.path, nil)
			if err != nil {
				t.Fatalf("request creation failed: %v", err)
			}
			rawResp, err := noRedirectClient.Do(req)
			if err != nil {
				t.Fatalf("GET %s failed: %v", tc.path, err)
			}
			defer rawResp.Body.Close()
			rawBody, _ := io.ReadAll(rawResp.Body)
			rawBodyStr := string(rawBody)

			// Security Assertion 1: Must never escape embedded directory
			if strings.Contains(rawBodyStr, "root:x:") || strings.Contains(rawBodyStr, "[fonts]") || strings.Contains(rawBodyStr, "[extensions]") {
				t.Fatalf("CRITICAL SECURITY VULNERABILITY: %s leaked host filesystem file!", tc.path)
			}

			// Test 2: Standard client following redirects
			stdClient := server.Client()
			stdResp, err := stdClient.Get(server.URL + tc.path)
			if err != nil {
				t.Fatalf("standard GET %s failed: %v", tc.path, err)
			}
			defer stdResp.Body.Close()
			stdBody, _ := io.ReadAll(stdResp.Body)
			stdBodyStr := string(stdBody)

			if strings.Contains(stdBodyStr, "root:x:") || strings.Contains(stdBodyStr, "[fonts]") {
				t.Fatalf("CRITICAL SECURITY VULNERABILITY: followed %s leaked host filesystem file!", tc.path)
			}

			// Boundary Check: Requirement states:
			// "Verify path traversal attempts on static assets (e.g. /../../etc/passwd or //dist/..) return 400 or 404 and cannot escape the embedded directory."
			// If raw response is not 400 or 404, or if followed response returns 200 index.html, flag as finding.
			if rawResp.StatusCode != http.StatusBadRequest && rawResp.StatusCode != http.StatusNotFound {
				t.Errorf("Path traversal %s: raw response was HTTP %d (Location: %q), expected 400 or 404",
					tc.path, rawResp.StatusCode, rawResp.Header.Get("Location"))
			}
			if stdResp.StatusCode == http.StatusOK && strings.Contains(stdBodyStr, "<div id=\"root\"></div>") {
				t.Errorf("Path traversal %s: standard client received HTTP 200 with index.html SPA fallback, expected 400 or 404 rejection", tc.path)
			}
		})
	}
}
