package statsapi_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/gorilla/websocket"
)

// mockPlayerHandler records invocations of OnUpdateState and OnMatchEnded.
type mockPlayerHandler struct {
	mu           sync.Mutex
	updateStates []updateStateCall
	matchEndeds  []matchEndedCall
	returnErr    error
}

type updateStateCall struct {
	MatchGUID  string
	PlaylistID int
	Players    []statsapi.StatsPlayer
}

type matchEndedCall struct {
	MatchGUID     string
	WinnerTeamNum *int
}

func (m *mockPlayerHandler) OnUpdateState(ctx context.Context, matchGUID string, playlistID int, players []statsapi.StatsPlayer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateStates = append(m.updateStates, updateStateCall{
		MatchGUID:  matchGUID,
		PlaylistID: playlistID,
		Players:    players,
	})
	return m.returnErr
}

func (m *mockPlayerHandler) OnMatchEnded(ctx context.Context, matchGUID string, winnerTeamNum *int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.matchEndeds = append(m.matchEndeds, matchEndedCall{
		MatchGUID:     matchGUID,
		WinnerTeamNum: winnerTeamNum,
	})
	return m.returnErr
}

func (m *mockPlayerHandler) getUpdateStates() []updateStateCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := make([]updateStateCall, len(m.updateStates))
	copy(res, m.updateStates)
	return res
}

func (m *mockPlayerHandler) getMatchEndeds() []matchEndedCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := make([]matchEndedCall, len(m.matchEndeds))
	copy(res, m.matchEndeds)
	return res
}

func TestListener_PlayerEventHandler_WebSocket(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// 1. Send UpdateState (object Data)
		updateMsg := statsapi.EventMessage{
			Event: "UpdateState",
			Data: statsapi.EventData{
				MatchGuid: "ws-match-101",
				Playlist:  13,
				Players: []statsapi.StatsPlayer{
					{Name: "Alice", PrimaryId: "Steam|1111|0", TeamNum: 0},
					{Name: "Bob", PrimaryId: "Epic|2222|0", TeamNum: 1},
				},
			},
		}
		data1, _ := json.Marshal(updateMsg)
		conn.WriteMessage(websocket.TextMessage, data1)

		// 2. Send UpdateState (string Data)
		strData := `{"Event":"UpdateState","Data":"{\"MatchGuid\":\"ws-match-102\",\"Playlist\":2,\"Players\":[{\"Name\":\"Charlie\",\"PrimaryId\":\"Steam|3333|0\",\"TeamNum\":0}]}"}`
		conn.WriteMessage(websocket.TextMessage, []byte(strData))

		// 3. Send MatchEnded with WinnerTeamNum = 0
		win0 := 0
		endedMsg := statsapi.EventMessage{
			Event: "MatchEnded",
			Data: statsapi.EventData{
				MatchGuid:     "ws-match-101",
				WinnerTeamNum: &win0,
			},
		}
		data3, _ := json.Marshal(endedMsg)
		conn.WriteMessage(websocket.TextMessage, data3)

		time.Sleep(150 * time.Millisecond)
	}))
	defer server.Close()

	wsURL := "ws://" + server.Listener.Addr().String()

	store := newMockStore()
	tracker, _ := statsapi.NewTracker(store, statsapi.TrackerConfig{TriggerThreshold: 10})
	handler := &mockPlayerHandler{}

	listener, err := statsapi.NewListener(statsapi.ListenerConfig{
		Address:        wsURL,
		Protocol:       "websocket",
		ReconnectDelay: 50 * time.Millisecond,
	}, tracker, nil, statsapi.WithPlayerEventHandler(handler))
	if err != nil {
		t.Fatalf("NewListener error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go listener.Start(ctx)

	// Poll until events arrive in handler
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(handler.getUpdateStates()) >= 2 && len(handler.getMatchEndeds()) >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	listener.Stop()

	updates := handler.getUpdateStates()
	if len(updates) != 2 {
		t.Fatalf("expected 2 UpdateState events received, got %d", len(updates))
	}
	if updates[0].MatchGUID != "ws-match-101" || updates[0].PlaylistID != 13 || len(updates[0].Players) != 2 {
		t.Errorf("unexpected first update: %+v", updates[0])
	}
	if updates[1].MatchGUID != "ws-match-102" || updates[1].PlaylistID != 2 || len(updates[1].Players) != 1 {
		t.Errorf("unexpected second update: %+v", updates[1])
	}

	endeds := handler.getMatchEndeds()
	if len(endeds) != 1 {
		t.Fatalf("expected 1 MatchEnded event received, got %d", len(endeds))
	}
	if endeds[0].MatchGUID != "ws-match-101" || endeds[0].WinnerTeamNum == nil || *endeds[0].WinnerTeamNum != 0 {
		t.Errorf("unexpected MatchEnded: %+v", endeds[0])
	}

	// Verify tracker also recorded ws-match-101 on MatchEnded
	if tracker.PendingCount() != 1 {
		t.Errorf("expected tracker.PendingCount()=1, got %d", tracker.PendingCount())
	}
}

func TestListener_PlayerEventHandler_TCP(t *testing.T) {
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

		// Write UpdateState line
		updateMsg := statsapi.EventMessage{
			Event: "UpdateState",
			Data: statsapi.EventData{
				MatchGuid: "tcp-match-201",
				Playlist:  11,
				Players: []statsapi.StatsPlayer{
					{Name: "P1", PrimaryId: "Epic|abc|0", TeamNum: 0},
				},
			},
		}
		data1, _ := json.Marshal(updateMsg)
		conn.Write(append(data1, '\n'))

		// Write MatchEnded line with WinnerTeamNum = 1
		win1 := 1
		endedMsg := statsapi.EventMessage{
			Event: "MatchEnded",
			Data: statsapi.EventData{
				MatchGuid:     "tcp-match-201",
				WinnerTeamNum: &win1,
			},
		}
		data2, _ := json.Marshal(endedMsg)
		conn.Write(append(data2, '\n'))

		time.Sleep(100 * time.Millisecond)
	}()

	store := newMockStore()
	tracker, _ := statsapi.NewTracker(store, statsapi.TrackerConfig{TriggerThreshold: 10})
	handler := &mockPlayerHandler{}

	listener, err := statsapi.NewListener(statsapi.ListenerConfig{
		Address:        ln.Addr().String(),
		Protocol:       "tcp",
		ReconnectDelay: 50 * time.Millisecond,
	}, tracker, nil, statsapi.WithPlayerEventHandler(handler))
	if err != nil {
		t.Fatalf("NewListener error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go listener.Start(ctx)

	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(handler.getUpdateStates()) >= 1 && len(handler.getMatchEndeds()) >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	listener.Stop()

	updates := handler.getUpdateStates()
	if len(updates) != 1 || updates[0].MatchGUID != "tcp-match-201" || updates[0].PlaylistID != 11 {
		t.Errorf("unexpected TCP update: %+v", updates)
	}

	endeds := handler.getMatchEndeds()
	if len(endeds) != 1 || endeds[0].WinnerTeamNum == nil || *endeds[0].WinnerTeamNum != 1 {
		t.Errorf("unexpected TCP match ended: %+v", endeds)
	}
}

func TestListener_PlayerEventHandler_NilSafe(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		msg := statsapi.EventMessage{
			Event: "UpdateState",
			Data:  statsapi.EventData{MatchGuid: "ws-nil-1"},
		}
		data, _ := json.Marshal(msg)
		conn.WriteMessage(websocket.TextMessage, data)
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	store := newMockStore()
	tracker, _ := statsapi.NewTracker(store, statsapi.TrackerConfig{TriggerThreshold: 10})

	// Start listener without handler option
	listener, err := statsapi.NewListener(statsapi.ListenerConfig{
		Address:        "ws://" + server.Listener.Addr().String(),
		Protocol:       "websocket",
		ReconnectDelay: 50 * time.Millisecond,
	}, tracker, nil)
	if err != nil {
		t.Fatalf("NewListener error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	go listener.Start(ctx)
	time.Sleep(200 * time.Millisecond)
	listener.Stop()
	// Pass if no panic occurred
}

func TestListener_SetPlayerEventHandler_Dynamic(t *testing.T) {
	store := newMockStore()
	tracker, _ := statsapi.NewTracker(store, statsapi.TrackerConfig{TriggerThreshold: 10})

	listener, err := statsapi.NewListener(statsapi.ListenerConfig{
		Address: "127.0.0.1:49124",
	}, tracker, nil)
	if err != nil {
		t.Fatalf("NewListener error: %v", err)
	}

	if listener.PlayerEventHandler() != nil {
		t.Errorf("expected initial PlayerEventHandler to be nil")
	}

	handler := &mockPlayerHandler{}
	listener.SetPlayerEventHandler(handler)

	if listener.PlayerEventHandler() != handler {
		t.Errorf("expected PlayerEventHandler to match configured handler")
	}
}
