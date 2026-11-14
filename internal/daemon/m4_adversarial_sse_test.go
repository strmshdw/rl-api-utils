package daemon_test

import (
	"bufio"
	"context"
	"encoding/json"
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

func TestM4_SSE_InitialAndConcludedSessionUpdatesAreEnriched(t *testing.T) {
	d, st, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/events", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /api/events failed: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	readSessionEvent := func() session.SessionResponse {
		for {
			line, readErr := reader.ReadString('\n')
			if readErr != nil {
				t.Fatalf("SSE read failed before session_update: %v", readErr)
			}
			if strings.TrimSpace(line) != "event: session_update" {
				continue
			}
			for {
				dataLine, dErr := reader.ReadString('\n')
				if dErr != nil {
					t.Fatalf("SSE data read failed: %v", dErr)
				}
				dataLine = strings.TrimSpace(dataLine)
				if strings.HasPrefix(dataLine, "data:") {
					raw := strings.TrimSpace(strings.TrimPrefix(dataLine, "data:"))
					var out session.SessionResponse
					if uErr := json.Unmarshal([]byte(raw), &out); uErr != nil {
						t.Fatalf("unmarshal session_update failed: %v", uErr)
					}
					return out
				}
			}
		}
	}

	initSum := readSessionEvent()
	if initSum.MaxHostedReplays != 20 || initSum.TriggerThreshold <= 0 {
		t.Fatalf("expected enriched initial SSE session_update (max_hosted_replays=20, trigger_threshold>0), got %+v", initSum)
	}

	st.OnMatchConcluded(&playertrack.CurrentMatchResponse{
		ActiveMatch:  true,
		MatchEnded:   true,
		MatchGUID:    "sse-enriched-conclude-1",
		PlaylistID:   11,
		PlaylistName: "Ranked Doubles (2v2)",
		Result:       "victory",
	})

	nextSum := readSessionEvent()
	if nextSum.TotalMatches != 1 || nextSum.MaxHostedReplays != 20 || nextSum.TriggerThreshold <= 0 {
		t.Fatalf("expected enriched concluded SSE session_update, got %+v", nextSum)
	}
}

// -----------------------------------------------------------------------------
// Adversarial Test 3: Concurrent REST Mutation & Read Stress Hammer
// -----------------------------------------------------------------------------
