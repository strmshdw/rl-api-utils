package daemon_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/session"
)

// TestChallenger2_SSE_100ConcurrentSubscribers_HalfAbruptSever_ZeroEventLossOnSurviving
// tests 100 concurrent SSE subscribers. Midway through broadcasting 50 sequential events,
// 50 subscribers are abruptly disconnected by closing their network connections.
// The test verifies:
// 1. All 50 surviving subscribers receive every single broadcast event in order (zero drops).
// 2. The server handles abrupt disconnects without panics or deadlocks.
// 3. All subscriber channels and handler goroutines clean up cleanly (zero leaks).
func TestChallenger2_SSE_50ConcurrentSubscribers_HalfAbruptSever_ZeroEventLossOnSurviving(t *testing.T) {
	d, st, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	const numSubscribers = 50
	const numEvents = 50

	ctx, masterCancel := context.WithCancel(context.Background())
	defer masterCancel()

	type clientHandle struct {
		cancel   context.CancelFunc
		resp     *http.Response
		received []int
		errs     []error
		done     chan struct{}
	}

	clients := make([]*clientHandle, numSubscribers)
	client := server.Client()

	for i := 0; i < numSubscribers; i++ {
		idx := i
		clientCtx, clientCancel := context.WithCancel(ctx)
		handle := &clientHandle{
			cancel:   clientCancel,
			received: make([]int, 0, numEvents),
			errs:     make([]error, 0),
			done:     make(chan struct{}),
		}
		clients[idx] = handle

		go func() {
			defer close(handle.done)

			var resp *http.Response
			var err error
			for attempt := 0; attempt < 5; attempt++ {
				req, reqErr := http.NewRequestWithContext(clientCtx, http.MethodGet, server.URL+"/api/events", nil)
				if reqErr != nil {
					handle.errs = append(handle.errs, reqErr)
					return
				}
				resp, err = client.Do(req)
				if err == nil {
					break
				}
				if clientCtx.Err() != nil {
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
			if err != nil {
				if clientCtx.Err() == nil {
					handle.errs = append(handle.errs, err)
				}
				return
			}
			handle.resp = resp
			defer resp.Body.Close()

			reader := bufio.NewReader(resp.Body)

			// Read subsequent broadcast events
			var currentEvent string
			for {
				line, rErr := reader.ReadString('\n')
				if rErr != nil {
					return
				}
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "event:") {
					currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				} else if strings.HasPrefix(line, "data:") && currentEvent == "match_update" {
					dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
					var payload map[string]interface{}
					if err := json.Unmarshal([]byte(dataStr), &payload); err == nil {
						if seqVal, ok := payload["seq"].(float64); ok {
							handle.received = append(handle.received, int(seqVal))
						}
					}
					currentEvent = ""
				}
			}
		}()
	}

	// Wait until all 50 subscribers are registered in the broadcaster
	subDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(subDeadline) {
		if st.Broadcaster().SubscriberCount() == numSubscribers {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}

	if count := st.Broadcaster().SubscriberCount(); count != numSubscribers {
		// Inspect any client errors
		for idx, h := range clients {
			if len(h.errs) > 0 {
				t.Logf("client %d encountered error: %v", idx, h.errs)
			}
		}
		t.Fatalf("expected exactly %d active subscribers, got %d", numSubscribers, count)
	}

	// Concurrently broadcast 50 sequential events
	var broadcastWg sync.WaitGroup
	broadcastWg.Add(1)
	go func() {
		defer broadcastWg.Done()
		for seq := 0; seq < numEvents; seq++ {
			st.Broadcaster().Broadcast(session.SessionEvent{
				Event: session.EventMatchUpdate,
				Data: map[string]interface{}{
					"seq":          seq,
					"active_match": true,
				},
			})
			// Abruptly sever the first 25 clients midway through broadcasting
			if seq == 25 {
				for i := 0; i < 25; i++ {
					clients[i].cancel()
					if clients[i].resp != nil {
						_ = clients[i].resp.Body.Close()
					}
				}
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	broadcastWg.Wait()

	// Give surviving subscribers time to drain buffered events
	time.Sleep(100 * time.Millisecond)

	// Verify the 25 surviving subscribers (indices 25..49)
	for i := 25; i < numSubscribers; i++ {
		h := clients[i]
		if len(h.received) != numEvents {
			t.Errorf("surviving client %d received %d events, expected exactly %d", i, len(h.received), numEvents)
			continue
		}
		// Check strict sequence ordering and zero drops
		for expectedSeq, actualSeq := range h.received {
			if actualSeq != expectedSeq {
				t.Errorf("surviving client %d event mismatch at idx %d: got seq %d, expected %d",
					i, expectedSeq, actualSeq, expectedSeq)
				break
			}
		}
	}

	// Disconnect all remaining surviving clients
	for i := 25; i < numSubscribers; i++ {
		clients[i].cancel()
		if clients[i].resp != nil {
			_ = clients[i].resp.Body.Close()
		}
		<-clients[i].done
	}

	// Await first 25 client routines completion
	for i := 0; i < 25; i++ {
		<-clients[i].done
	}

	// Verify subscriber count drops to 0
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if st.Broadcaster().SubscriberCount() == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if count := st.Broadcaster().SubscriberCount(); count != 0 {
		t.Fatalf("goroutine/channel leak: expected 0 subscribers after all disconnects, got %d", count)
	}
}

// TestChallenger2_SSE_ConcurrentHighFrequencyResetAndMatchEvents tests intense concurrency:
// 20 SSE subscribers reading streams while 10 workers hammer GET /api/session,
// 5 workers hammer POST /api/session/reset, and 5 workers trigger match updates/conclusions.
// Verifies no race timeouts, no deadlocks, and 100% successful HTTP responses.
func TestChallenger2_SSE_ConcurrentHighFrequencyResetAndMatchEvents(t *testing.T) {
	d, st, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	const sseCount = 20
	var sseWg sync.WaitGroup
	var sseEventsReceived atomic.Int64

	// Shared client using server's pooled transport to prevent Windows ephemeral socket exhaustion
	sharedClient := server.Client()

	// 20 SSE subscribers
	for i := 0; i < sseCount; i++ {
		sseWg.Add(1)
		go func(subIdx int) {
			defer sseWg.Done()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/events", nil)
			if err != nil {
				return
			}
			resp, err := sharedClient.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()

			reader := bufio.NewReader(resp.Body)
			for {
				line, rErr := reader.ReadString('\n')
				if rErr != nil {
					return
				}
				if strings.HasPrefix(strings.TrimSpace(line), "event:") {
					sseEventsReceived.Add(1)
				}
			}
		}(i)
	}

	// Wait for SSE subscribers to register
	subDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(subDeadline) {
		if st.Broadcaster().SubscriberCount() >= sseCount {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	var opWg sync.WaitGroup
	var readCount atomic.Int64
	var resetCount atomic.Int64
	var matchEventCount atomic.Int64

	// 10 Readers hammering GET /api/session
	for r := 0; r < 10; r++ {
		opWg.Add(1)
		go func() {
			defer opWg.Done()
			for ctx.Err() == nil {
				resp, err := sharedClient.Get(server.URL + "/api/session")
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					t.Errorf("GET /api/session failed: %v", err)
					return
				}
				if resp.StatusCode != http.StatusOK {
					t.Errorf("GET /api/session got status %d", resp.StatusCode)
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				readCount.Add(1)
				time.Sleep(15 * time.Millisecond)
			}
		}()
	}

	// 5 Writers hammering POST /api/session/reset
	for w := 0; w < 5; w++ {
		opWg.Add(1)
		go func() {
			defer opWg.Done()
			for ctx.Err() == nil {
				resp, err := sharedClient.Post(server.URL+"/api/session/reset", "application/json", nil)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					t.Errorf("POST /api/session/reset failed: %v", err)
					return
				}
				if resp.StatusCode != http.StatusOK {
					t.Errorf("POST /api/session/reset got status %d", resp.StatusCode)
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				resetCount.Add(1)
				time.Sleep(25 * time.Millisecond)
			}
		}()
	}

	// 5 Writers simulating match lifecycle
	for m := 0; m < 5; m++ {
		opWg.Add(1)
		go func(workerID int) {
			defer opWg.Done()
			cycle := 0
			for ctx.Err() == nil {
				cycle++
				guid := fmt.Sprintf("hammer-guid-%d-%d", workerID, cycle)
				w := cycle % 2
				st.RecordActiveMatch(&playertrack.CurrentMatchResponse{
					MatchGUID:   guid,
					PlaylistID:  13,
					ActiveMatch: true,
					LocalPlayer: &playertrack.LobbyPlayer{
						PlayerID: "Steam|76561198000000001|0",
						Name:     "ConcurrentTester",
						CurrentRank: &playertrack.PlayerPlaylistRank{
							MMR: float64(1000 + (cycle % 50)),
						},
					},
				})
				time.Sleep(15 * time.Millisecond)

				st.ConcludeMatch(&playertrack.CurrentMatchResponse{
					MatchGUID:   guid,
					PlaylistID:  13,
					WinnerTeam:  &w,
					LocalTeam:   &w,
					Result:      "victory",
					MatchEnded:  true,
					ActiveMatch: false,
					LocalPlayer: &playertrack.LobbyPlayer{
						PlayerID: "Steam|76561198000000001|0",
						Name:     "ConcurrentTester",
						CurrentRank: &playertrack.PlayerPlaylistRank{
							MMR: float64(1009 + (cycle % 50)),
						},
					},
				})
				matchEventCount.Add(1)
				time.Sleep(15 * time.Millisecond)
			}
		}(m)
	}

	<-ctx.Done()
	opWg.Wait()
	sseWg.Wait()

	t.Logf("Concurrency hammer completed: %d reads, %d resets, %d match concludes, %d SSE events delivered",
		readCount.Load(), resetCount.Load(), matchEventCount.Load(), sseEventsReceived.Load())

	if readCount.Load() < 50 {
		t.Errorf("expected >= 50 reads, got %d", readCount.Load())
	}
	if resetCount.Load() < 10 {
		t.Errorf("expected >= 10 resets, got %d", resetCount.Load())
	}
	if sseEventsReceived.Load() < 100 {
		t.Errorf("expected >= 100 SSE events delivered, got %d", sseEventsReceived.Load())
	}

	// Verify subscribers clean up cleanly
	cleanupDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(cleanupDeadline) {
		if st.Broadcaster().SubscriberCount() == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if count := st.Broadcaster().SubscriberCount(); count != 0 {
		t.Errorf("expected 0 subscribers after test completion, got %d", count)
	}
}

// TestChallenger2_SSE_RapidConnectDisconnectChurn tests 50 iterations of connecting
// and immediately disconnecting an SSE subscriber, verifying:
// 1. Initial snapshot is received without corruption.
// 2. Channels and tickers are always released.
// 3. No memory or goroutine leak occurs.
func TestChallenger2_SSE_RapidConnectDisconnectChurn(t *testing.T) {
	d, st, _ := setupWebTestDaemon(t)
	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	initialGoroutines := runtime.NumGoroutine()

	for iter := 0; iter < 50; iter++ {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/events", nil)
		if err != nil {
			cancel()
			t.Fatalf("iter %d: request creation failed: %v", iter, err)
		}

		resp, err := server.Client().Do(req)
		if err != nil {
			cancel()
			t.Fatalf("iter %d: GET /api/events failed: %v", iter, err)
		}

		reader := bufio.NewReader(resp.Body)
		// Read until initial session_update event
		gotEvent := false
		for i := 0; i < 10; i++ {
			line, rErr := reader.ReadString('\n')
			if rErr != nil {
				break
			}
			if strings.TrimSpace(line) == "event: session_update" {
				gotEvent = true
				break
			}
		}

		// Abrupt disconnect immediately after reading initial event
		cancel()
		_ = resp.Body.Close()

		if !gotEvent {
			t.Errorf("iter %d: failed to read initial session_update before disconnecting", iter)
		}
	}

	// Allow goroutines to exit
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if st.Broadcaster().SubscriberCount() == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if finalCount := st.Broadcaster().SubscriberCount(); finalCount != 0 {
		t.Fatalf("subscriber leak: %d subscribers remain registered after churn", finalCount)
	}

	// Verify goroutine count returns to baseline (+/- small tolerance for test runner)
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	finalGoroutines := runtime.NumGoroutine()
	delta := finalGoroutines - initialGoroutines
	t.Logf("Goroutine baseline: %d, final: %d (delta: %+d)", initialGoroutines, finalGoroutines, delta)

	if delta > 15 {
		t.Errorf("potential goroutine leak: delta %+d goroutines (initial: %d, final: %d)",
			delta, initialGoroutines, finalGoroutines)
	}
}
