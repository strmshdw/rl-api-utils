package statsapi_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/gorilla/websocket"
)

// -----------------------------------------------------------------------------
// 4. HIGH TICK-RATE TELEMETRY BURST (120Hz UpdateState) STRESS TEST
// -----------------------------------------------------------------------------

// highRateHandler records high-throughput events atomically
type highRateHandler struct {
	updateCount   int64
	matchEndCount int64
	lastUpdate    atomic.Pointer[statsapi.StatsPlayer]
	errors        int64
}

func (h *highRateHandler) OnUpdateState(ctx context.Context, matchGUID string, playlistID int, players []statsapi.StatsPlayer) error {
	atomic.AddInt64(&h.updateCount, 1)
	if len(players) > 0 {
		h.lastUpdate.Store(&players[0])
	}
	return nil
}

func (h *highRateHandler) OnMatchEnded(ctx context.Context, matchGUID string, winnerTeamNum *int) error {
	atomic.AddInt64(&h.matchEndCount, 1)
	return nil
}

func TestAdversarial_HighRateBurst_120Hz_WebSocket(t *testing.T) {
	// Simulate 120Hz telemetry burst over WebSocket:
	// 120 frames per second. We test a burst of 600 frames (5 full seconds of 120Hz telemetry)
	// sent as rapidly as possible to stress reader queue and lock contention.

	totalFrames := 600
	upgrader := websocket.Upgrader{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Stream 600 UpdateState frames in rapid succession (simulating 120Hz burst)
		for i := 0; i < totalFrames; i++ {
			msg := statsapi.EventMessage{
				Event: "UpdateState",
				Data: statsapi.EventData{
					MatchGuid: "burst-120hz-match",
					Playlist:  13,
					Players: []statsapi.StatsPlayer{
						{Name: "Blue1", PrimaryId: "Steam|101|0", TeamNum: 0, Score: i * 10, Goals: i % 5},
						{Name: "Blue2", PrimaryId: "Steam|102|0", TeamNum: 0, Score: i * 8},
						{Name: "Blue3", PrimaryId: "Steam|103|0", TeamNum: 0, Score: i * 6},
						{Name: "Orange1", PrimaryId: "Epic|201|0", TeamNum: 1, Score: i * 9},
						{Name: "Orange2", PrimaryId: "Epic|202|0", TeamNum: 1, Score: i * 7},
						{Name: "Orange3", PrimaryId: "Unknown|0|0", TeamNum: 1, Score: i * 2},
					},
					Game: &statsapi.StatsGame{
						PlaylistId:  13,
						TimeSeconds: 300 - (i / 120),
						Overtime:    false,
					},
				},
			}
			data, _ := json.Marshal(msg)
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		}

		// Finish match with MatchEnded
		win0 := 0
		endedMsg := statsapi.EventMessage{
			Event: "MatchEnded",
			Data: statsapi.EventData{
				MatchGuid:     "burst-120hz-match",
				WinnerTeamNum: &win0,
			},
		}
		data, _ := json.Marshal(endedMsg)
		conn.WriteMessage(websocket.TextMessage, data)
	}))
	defer server.Close()

	store := newMockStore()
	tracker, _ := statsapi.NewTracker(store, statsapi.TrackerConfig{TriggerThreshold: 1000})
	handler := &highRateHandler{}

	wsURL := "ws://" + server.Listener.Addr().String()
	listener, err := statsapi.NewListener(statsapi.ListenerConfig{
		Address:        wsURL,
		Protocol:       "websocket",
		ReconnectDelay: 50 * time.Millisecond,
	}, tracker, nil, statsapi.WithPlayerEventHandler(handler))
	if err != nil {
		t.Fatalf("NewListener error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	go listener.Start(ctx)

	// Poll until all frames arrive or timeout
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&handler.updateCount) >= int64(totalFrames) && atomic.LoadInt64(&handler.matchEndCount) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	elapsed := time.Since(start)
	listener.Stop()

	receivedUpdates := atomic.LoadInt64(&handler.updateCount)
	receivedEnds := atomic.LoadInt64(&handler.matchEndCount)

	t.Logf("WebSocket Burst Result: received %d / %d updates and %d / 1 match ends in %v (rate: %.1f msg/sec)",
		receivedUpdates, totalFrames, receivedEnds, elapsed, float64(receivedUpdates)/elapsed.Seconds())

	if receivedUpdates != int64(totalFrames) {
		t.Fatalf("expected all %d UpdateState frames to be processed, got %d", totalFrames, receivedUpdates)
	}
	if receivedEnds != 1 {
		t.Fatalf("expected 1 MatchEnded event, got %d", receivedEnds)
	}
}

func TestAdversarial_HighRateBurst_120Hz_TCP(t *testing.T) {
	// Simulate 120Hz burst over TCP connection with newline-delimited JSON
	totalFrames := 600

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on TCP: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		for i := 0; i < totalFrames; i++ {
			msg := statsapi.EventMessage{
				Event: "UpdateState",
				Data: statsapi.EventData{
					MatchGuid: "tcp-burst-match",
					Playlist:  11,
					Players: []statsapi.StatsPlayer{
						{Name: "P1", PrimaryId: "Epic|1|0", TeamNum: 0, Score: i},
						{Name: "P2", PrimaryId: "Epic|2|0", TeamNum: 1, Score: i * 2},
					},
				},
			}
			data, _ := json.Marshal(msg)
			conn.Write(append(data, '\n'))
		}

		win1 := 1
		ended := statsapi.EventMessage{
			Event: "MatchEnded",
			Data: statsapi.EventData{
				MatchGuid:     "tcp-burst-match",
				WinnerTeamNum: &win1,
			},
		}
		data, _ := json.Marshal(ended)
		conn.Write(append(data, '\n'))
	}()

	store := newMockStore()
	tracker, _ := statsapi.NewTracker(store, statsapi.TrackerConfig{TriggerThreshold: 1000})
	handler := &highRateHandler{}

	listener, err := statsapi.NewListener(statsapi.ListenerConfig{
		Address:        ln.Addr().String(),
		Protocol:       "tcp",
		ReconnectDelay: 50 * time.Millisecond,
	}, tracker, nil, statsapi.WithPlayerEventHandler(handler))
	if err != nil {
		t.Fatalf("NewListener error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go listener.Start(ctx)

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&handler.updateCount) >= int64(totalFrames) && atomic.LoadInt64(&handler.matchEndCount) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	listener.Stop()

	received := atomic.LoadInt64(&handler.updateCount)
	if received != int64(totalFrames) {
		t.Fatalf("TCP Burst: expected %d frames, got %d", totalFrames, received)
	}
	if atomic.LoadInt64(&handler.matchEndCount) != 1 {
		t.Fatalf("TCP Burst: expected 1 MatchEnded, got %d", atomic.LoadInt64(&handler.matchEndCount))
	}
}

func TestAdversarial_Concurrent_SetHandler_DuringBurst(t *testing.T) {
	// Concurrently invoke SetPlayerEventHandler while streaming frames
	totalFrames := 400
	upgrader := websocket.Upgrader{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for i := 0; i < totalFrames; i++ {
			msg := statsapi.EventMessage{
				Event: "UpdateState",
				Data: statsapi.EventData{
					MatchGuid: "race-handler-match",
					Playlist:  13,
					Players: []statsapi.StatsPlayer{
						{Name: "Racer", PrimaryId: "Steam|999|0", TeamNum: 0},
					},
				},
			}
			data, _ := json.Marshal(msg)
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	store := newMockStore()
	tracker, _ := statsapi.NewTracker(store, statsapi.TrackerConfig{TriggerThreshold: 1000})

	listener, err := statsapi.NewListener(statsapi.ListenerConfig{
		Address:  "ws://" + server.Listener.Addr().String(),
		Protocol: "websocket",
	}, tracker, nil)
	if err != nil {
		t.Fatalf("NewListener error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go listener.Start(ctx)

	// Concurrently swap handlers
	h1 := &highRateHandler{}
	h2 := &highRateHandler{}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			listener.SetPlayerEventHandler(h1)
			time.Sleep(2 * time.Millisecond)
			listener.SetPlayerEventHandler(h2)
			time.Sleep(2 * time.Millisecond)
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = listener.PlayerEventHandler()
			time.Sleep(2 * time.Millisecond)
		}
	}()

	wg.Wait()
	listener.Stop()

	totalProcessed := atomic.LoadInt64(&h1.updateCount) + atomic.LoadInt64(&h2.updateCount)
	t.Logf("Concurrent handler swap: h1=%d, h2=%d, total=%d / %d",
		atomic.LoadInt64(&h1.updateCount), atomic.LoadInt64(&h2.updateCount), totalProcessed, totalFrames)
}
