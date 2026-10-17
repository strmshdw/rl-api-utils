package statsapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/gorilla/websocket"
)

type mockGameStateHandler struct {
	mu           sync.Mutex
	states       []receivedGameState
	updateStates []string
	matchEndeds  []string
}

type receivedGameState struct {
	MatchGUID     string
	PlaylistID    int
	TimeRemaining int
	IsOvertime    bool
	PlayersCount  int
}

func (m *mockGameStateHandler) OnUpdateState(ctx context.Context, matchGUID string, playlistID int, players []statsapi.StatsPlayer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateStates = append(m.updateStates, matchGUID)
	return nil
}

func (m *mockGameStateHandler) OnMatchEnded(ctx context.Context, matchGUID string, winnerTeamNum *int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.matchEndeds = append(m.matchEndeds, matchGUID)
	return nil
}

func (m *mockGameStateHandler) OnUpdateGameState(ctx context.Context, matchGUID string, playlistID int, players []statsapi.StatsPlayer, game *statsapi.StatsGame) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	timeRemaining := 0
	isOvertime := false
	if game != nil {
		timeRemaining = game.GetTimeRemaining()
		isOvertime = game.GetIsOvertime()
	}
	m.states = append(m.states, receivedGameState{
		MatchGUID:     matchGUID,
		PlaylistID:    playlistID,
		TimeRemaining: timeRemaining,
		IsOvertime:    isOvertime,
		PlayersCount:  len(players),
	})
	return nil
}

func (m *mockGameStateHandler) getStatesCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.states)
}

func (m *mockGameStateHandler) getLastState() (receivedGameState, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.states) == 0 {
		return receivedGameState{}, false
	}
	return m.states[len(m.states)-1], true
}

// TestM3_LiveClock_RapidWebSocketBursts tests ingestion of 600 rapid clock update frames
// under concurrent WebSocket streaming with negative times, overtime toggles, and zero-second play.
func TestM3_LiveClock_RapidWebSocketBursts(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	const burstFrames = 600
	readyToSend := make(chan struct{})
	doneSending := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade error: %v", err)
			return
		}
		defer conn.Close()

		<-readyToSend

		for i := 0; i < burstFrames; i++ {
			var timeRemaining int
			var isOT bool

			switch {
			case i < 50:
				// Kickoff countdown / uninitialized negative clock
				timeRemaining = -1 * ((i % 5) + 1)
				isOT = false
			case i < 300:
				// Normal regulation counting down from 300 to 50
				timeRemaining = 300 - i
				isOT = false
			case i < 350:
				// Zero-second buzzer beater play
				timeRemaining = 0
				isOT = false
			case i < 400:
				// Sudden overtime transition
				timeRemaining = i - 350
				isOT = true
			case i < 500:
				// Overtime toggle oscillation (e.g. glitching spectator state)
				timeRemaining = i - 350
				isOT = (i%2 == 0)
			default:
				// Final overtime resolution
				timeRemaining = 120
				isOT = true
			}

			payload := map[string]interface{}{
				"Event": "UpdateState",
				"Data": map[string]interface{}{
					"MatchGuid": "burst-clock-match",
					"Playlist":  13,
					"Players": []map[string]interface{}{
						{
							"Name":      "ClockRunner",
							"PrimaryId": "Steam|76561198000000001|0",
							"TeamNum":   0,
							"Score":     i * 10,
						},
					},
					"Game": map[string]interface{}{
						"PlaylistId":     13,
						"time_remaining": timeRemaining,
						"is_overtime":    isOT,
					},
				},
			}

			data, _ := json.Marshal(payload)
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				t.Logf("write error at frame %d: %v", i, err)
				break
			}
		}

		close(doneSending)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	store := newMockStore()
	tracker, _ := statsapi.NewTracker(store, statsapi.TrackerConfig{TriggerThreshold: 10})
	handler := &mockGameStateHandler{}

	listener, err := statsapi.NewListener(
		statsapi.ListenerConfig{
			Address:        wsURL,
			Protocol:       "websocket",
			ReconnectDelay: 100 * time.Millisecond,
		},
		tracker,
		nil,
		statsapi.WithPlayerEventHandler(handler),
	)
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go listener.Start(ctx)
	defer listener.Stop()

	// Wait for connection
	time.Sleep(100 * time.Millisecond)

	// Release burst
	close(readyToSend)

	select {
	case <-doneSending:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for burst frames to send")
	}

	// Allow listener loop to drain
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if handler.getStatesCount() >= burstFrames {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	count := handler.getStatesCount()
	if count < burstFrames {
		t.Fatalf("expected at least %d ingested frames, got %d", burstFrames, count)
	}

	last, ok := handler.getLastState()
	if !ok {
		t.Fatal("expected at least one state recorded")
	}
	if last.TimeRemaining != 120 {
		t.Errorf("expected final time_remaining=120, got %d", last.TimeRemaining)
	}
	if !last.IsOvertime {
		t.Errorf("expected final is_overtime=true, got %v", last.IsOvertime)
	}
}

// TestM3_StatsGame_GetTimeRemaining_AdversarialBoundaries systematically tests
// edge cases and priority order between TimeRemaining and TimeSeconds.
func TestM3_StatsGame_GetTimeRemaining_AdversarialBoundaries(t *testing.T) {
	tests := []struct {
		name         string
		game         *statsapi.StatsGame
		expectedTime int
		expectedOT   bool
	}{
		{
			name:         "Nil game pointer returns 0 and false",
			game:         nil,
			expectedTime: 0,
			expectedOT:   false,
		},
		{
			name: "time_remaining takes precedence over TimeSeconds when non-zero",
			game: &statsapi.StatsGame{
				TimeRemaining: 150,
				TimeSeconds:   300,
				IsOvertime:    false,
			},
			expectedTime: 150,
			expectedOT:   false,
		},
		{
			name: "negative time_remaining (countdown/uninitialized) preserved as negative",
			game: &statsapi.StatsGame{
				TimeRemaining: -5,
				TimeSeconds:   300,
				IsOvertime:    false,
			},
			expectedTime: -5,
			expectedOT:   false,
		},
		{
			name: "TimeSeconds used as fallback when time_remaining is 0",
			game: &statsapi.StatsGame{
				TimeRemaining: 0,
				TimeSeconds:   240,
				Overtime:      true,
			},
			expectedTime: 240,
			expectedOT:   true,
		},
		{
			name: "Both 0 returns 0",
			game: &statsapi.StatsGame{
				TimeRemaining: 0,
				TimeSeconds:   0,
				IsOvertime:    false,
			},
			expectedTime: 0,
			expectedOT:   false,
		},
		{
			name: "Overtime field (bOvertime) true maps to GetIsOvertime=true",
			game: &statsapi.StatsGame{
				Overtime:   true,
				IsOvertime: false,
			},
			expectedTime: 0,
			expectedOT:   true,
		},
		{
			name: "IsOvertime field (is_overtime) true maps to GetIsOvertime=true",
			game: &statsapi.StatsGame{
				Overtime:   false,
				IsOvertime: true,
			},
			expectedTime: 0,
			expectedOT:   true,
		},
		{
			name: "Both overtime false maps to GetIsOvertime=false",
			game: &statsapi.StatsGame{
				Overtime:   false,
				IsOvertime: false,
			},
			expectedTime: 0,
			expectedOT:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotTime := tc.game.GetTimeRemaining()
			if gotTime != tc.expectedTime {
				t.Errorf("GetTimeRemaining() = %d, expected %d", gotTime, tc.expectedTime)
			}
			gotOT := tc.game.GetIsOvertime()
			if gotOT != tc.expectedOT {
				t.Errorf("GetIsOvertime() = %v, expected %v", gotOT, tc.expectedOT)
			}
		})
	}
}

// TestM3_LiveClock_RapidToggles_ConcurrentStatsAPI ensures no data races under concurrent
// WebSocket connection drops and rapid reconnects during high-frequency clock updates.
func TestM3_LiveClock_RapidToggles_ConcurrentStatsAPI(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	var clientCount int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		id := atomic.AddInt64(&clientCount, 1)

		for i := 0; i < 50; i++ {
			msg := fmt.Sprintf(`{"Event":"UpdateState","Data":{"MatchGuid":"toggle-guid-%d","Playlist":13,"Game":{"time_remaining":%d,"is_overtime":%v}}}`,
				id, 300-i, i%2 == 0)
			if err := conn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
				return
			}
			time.Sleep(1 * time.Millisecond)
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	store := newMockStore()
	tracker, _ := statsapi.NewTracker(store, statsapi.TrackerConfig{TriggerThreshold: 10})
	handler := &mockGameStateHandler{}

	listener, err := statsapi.NewListener(
		statsapi.ListenerConfig{
			Address:        wsURL,
			Protocol:       "websocket",
			ReconnectDelay: 50 * time.Millisecond,
		},
		tracker,
		nil,
		statsapi.WithPlayerEventHandler(handler),
	)
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go listener.Start(ctx)
	defer listener.Stop()

	time.Sleep(200 * time.Millisecond)
}
