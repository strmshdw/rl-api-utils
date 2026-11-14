package statsapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/gorilla/websocket"
)

// -----------------------------------------------------------------------------
// 3. AI BOT DETECTION RIGOROUS EDGE CASES
// -----------------------------------------------------------------------------

func TestAdversarial_BotDetection_Matrix(t *testing.T) {
	cases := []struct {
		name      string
		primaryID string
		wantPlat  string
		wantAcc   string
		wantSplit int
		wantBot   bool
		wantErr   bool
	}{
		{
			name:      "Standard Unknown bot Unknown|0|0",
			primaryID: "Unknown|0|0",
			wantPlat:  "Unknown",
			wantAcc:   "0",
			wantSplit: 0,
			wantBot:   true,
		},
		{
			name:      "Case-insensitive lower unknown|0|0",
			primaryID: "unknown|0|0",
			wantPlat:  "unknown",
			wantAcc:   "0",
			wantSplit: 0,
			wantBot:   true,
		},
		{
			name:      "Case-insensitive upper UNKNOWN|0|0",
			primaryID: "UNKNOWN|0|0",
			wantPlat:  "UNKNOWN",
			wantAcc:   "0",
			wantSplit: 0,
			wantBot:   true,
		},
		{
			name:      "Case-insensitive mixed uNkNoWn|0|0",
			primaryID: "uNkNoWn|0|0",
			wantPlat:  "uNkNoWn",
			wantAcc:   "0",
			wantSplit: 0,
			wantBot:   true,
		},
		{
			name:      "Steam bot with account 0 Steam|0|0",
			primaryID: "Steam|0|0",
			wantPlat:  "Steam",
			wantAcc:   "0",
			wantSplit: 0,
			wantBot:   true,
		},
		{
			name:      "Epic bot with account 0 Epic|0|0",
			primaryID: "Epic|0|0",
			wantPlat:  "Epic",
			wantAcc:   "0",
			wantSplit: 0,
			wantBot:   true,
		},
		{
			name:      "PlayStation bot with account 0 PlayStation|0|0",
			primaryID: "PlayStation|0|0",
			wantPlat:  "PlayStation",
			wantAcc:   "0",
			wantSplit: 0,
			wantBot:   true,
		},
		{
			name:      "XboxOne bot with account 0 XboxOne|0|0",
			primaryID: "XboxOne|0|0",
			wantPlat:  "XboxOne",
			wantAcc:   "0",
			wantSplit: 0,
			wantBot:   true,
		},
		{
			name:      "Unknown platform with non-zero account Unknown|9999|0",
			primaryID: "Unknown|9999|0",
			wantPlat:  "Unknown",
			wantAcc:   "9999",
			wantSplit: 0,
			wantBot:   true, // Platform Unknown -> bot
		},
		{
			name:      "Bot with non-zero splitscreen Unknown|0|2",
			primaryID: "Unknown|0|2",
			wantPlat:  "Unknown",
			wantAcc:   "0",
			wantSplit: 2,
			wantBot:   true,
		},
		{
			name:      "Spaces around segments  Unknown | 0 | 0 ",
			primaryID: "  Unknown | 0 | 0  ",
			wantPlat:  "Unknown",
			wantAcc:   "0",
			wantSplit: 0,
			wantBot:   true,
		},
		{
			name:      "Spaces around human Steam | 76561198000000001 | 0",
			primaryID: "  Steam | 76561198000000001 | 0  ",
			wantPlat:  "Steam",
			wantAcc:   "76561198000000001",
			wantSplit: 0,
			wantBot:   false,
		},
		{
			name:      "Empty string",
			primaryID: "",
			wantErr:   true,
		},
		{
			name:      "Whitespace string",
			primaryID: "   \t\n  ",
			wantErr:   true,
		},
		{
			name:      "Missing splitscreen Steam|12345",
			primaryID: "Steam|12345",
			wantErr:   true,
		},
		{
			name:      "Missing account and splitscreen Steam",
			primaryID: "Steam",
			wantErr:   true,
		},
		{
			name:      "Empty segments ||",
			primaryID: "||",
			wantErr:   true,
		},
		{
			name:      "Four segments Steam|12345|0|extra",
			primaryID: "Steam|12345|0|extra",
			wantErr:   true,
		},
		{
			name:      "Non-numeric splitscreen Steam|12345|first",
			primaryID: "Steam|12345|first",
			wantErr:   true,
		},
		{
			name:      "Negative splitscreen Steam|12345|-1",
			primaryID: "Steam|12345|-1",
			wantPlat:  "Steam",
			wantAcc:   "12345",
			wantSplit: -1,
			wantBot:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := statsapi.ParsePrimaryID(tc.primaryID)
			p := statsapi.StatsPlayer{PrimaryId: tc.primaryID}

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got nil", tc.primaryID)
				}
				// StatsPlayer.IsBot() should safely return false on unparseable/empty IDs
				if p.IsBot() {
					t.Errorf("StatsPlayer.IsBot() should be false for malformed ID %q", tc.primaryID)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.primaryID, err)
			}
			if parsed.Platform != tc.wantPlat {
				t.Errorf("platform got %q, want %q", parsed.Platform, tc.wantPlat)
			}
			if parsed.AccountID != tc.wantAcc {
				t.Errorf("account got %q, want %q", parsed.AccountID, tc.wantAcc)
			}
			if parsed.Splitscreen != tc.wantSplit {
				t.Errorf("splitscreen got %d, want %d", parsed.Splitscreen, tc.wantSplit)
			}
			if parsed.IsBot != tc.wantBot {
				t.Errorf("isBot got %v, want %v", parsed.IsBot, tc.wantBot)
			}
			if p.IsBot() != tc.wantBot {
				t.Errorf("StatsPlayer.IsBot() got %v, want %v", p.IsBot(), tc.wantBot)
			}
		})
	}
}

func TestAdversarial_HandlerError_Resilience(t *testing.T) {
	// Ensure that when PlayerEventHandler returns an error, the listener loop
	// does not break or crash and continues processing subsequent events.
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Frame 1: causes error
		msg1 := statsapi.EventMessage{Event: "UpdateState", Data: statsapi.EventData{MatchGuid: "err-1"}}
		d1, _ := json.Marshal(msg1)
		conn.WriteMessage(websocket.TextMessage, d1)

		// Frame 2: subsequent frame
		msg2 := statsapi.EventMessage{Event: "UpdateState", Data: statsapi.EventData{MatchGuid: "err-2"}}
		d2, _ := json.Marshal(msg2)
		conn.WriteMessage(websocket.TextMessage, d2)

		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	store := newMockStore()
	tracker, _ := statsapi.NewTracker(store, statsapi.TrackerConfig{TriggerThreshold: 100})
	handler := &mockPlayerHandler{returnErr: fmt.Errorf("simulated handler failure")}

	listener, err := statsapi.NewListener(statsapi.ListenerConfig{
		Address:  "ws://" + server.Listener.Addr().String(),
		Protocol: "websocket",
	}, tracker, nil, statsapi.WithPlayerEventHandler(handler))
	if err != nil {
		t.Fatalf("NewListener error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go listener.Start(ctx)

	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(handler.getUpdateStates()) >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	listener.Stop()

	updates := handler.getUpdateStates()
	if len(updates) != 2 {
		t.Fatalf("expected both frames processed despite handler error, got %d", len(updates))
	}
	if updates[0].MatchGUID != "err-1" || updates[1].MatchGUID != "err-2" {
		t.Errorf("unexpected update frames: %+v", updates)
	}
}

func TestAdversarial_Listener_InterleavedMalformedFrames(t *testing.T) {
	// Verify that sending completely malformed / non-JSON frames does not drop the connection
	// or prevent subsequent valid frames from being processed over WebSocket.
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// 1. Valid frame
		conn.WriteMessage(websocket.TextMessage, []byte(`{"Event":"UpdateState","Data":{"MatchGuid":"valid-1"}}`))

		// 2. Malformed garbage frames
		conn.WriteMessage(websocket.TextMessage, []byte(`not json at all`))
		conn.WriteMessage(websocket.TextMessage, []byte(`{"Event":"UpdateState","Data":{broken`))
		conn.WriteMessage(websocket.TextMessage, []byte(``))      // empty
		conn.WriteMessage(websocket.TextMessage, []byte(`   `))   // whitespace
		conn.WriteMessage(websocket.TextMessage, []byte(`12345`)) // number

		// 3. Second valid frame
		conn.WriteMessage(websocket.TextMessage, []byte(`{"Event":"UpdateState","Data":{"MatchGuid":"valid-2"}}`))

		time.Sleep(150 * time.Millisecond)
	}))
	defer server.Close()

	store := newMockStore()
	tracker, _ := statsapi.NewTracker(store, statsapi.TrackerConfig{TriggerThreshold: 100})
	handler := &mockPlayerHandler{}

	listener, err := statsapi.NewListener(statsapi.ListenerConfig{
		Address:  "ws://" + server.Listener.Addr().String(),
		Protocol: "websocket",
	}, tracker, nil, statsapi.WithPlayerEventHandler(handler))
	if err != nil {
		t.Fatalf("NewListener error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go listener.Start(ctx)

	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(handler.getUpdateStates()) >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	listener.Stop()

	updates := handler.getUpdateStates()
	if len(updates) != 2 {
		t.Fatalf("expected exactly 2 valid updates processed despite interleaved garbage, got %d", len(updates))
	}
	if updates[0].MatchGUID != "valid-1" || updates[1].MatchGUID != "valid-2" {
		t.Errorf("unexpected update frames: %+v", updates)
	}
}

func TestAdversarial_Listener_WinnerTeamNum_EndToEnd_Variants(t *testing.T) {
	// Deliver MatchEnded with 0, 1, nil, omitted, and unhandled team numbers over live WebSocket
	type testCase struct {
		guid     string
		jsonMsg  string
		wantNil  bool
		wantTeam int
	}

	cases := []testCase{
		{
			guid:     "win-blue-0",
			jsonMsg:  `{"Event":"MatchEnded","Data":{"MatchGuid":"win-blue-0","WinnerTeamNum":0}}`,
			wantNil:  false,
			wantTeam: 0,
		},
		{
			guid:     "win-orange-1",
			jsonMsg:  `{"Event":"MatchEnded","Data":{"MatchGuid":"win-orange-1","WinnerTeamNum":1}}`,
			wantNil:  false,
			wantTeam: 1,
		},
		{
			guid:    "win-null",
			jsonMsg: `{"Event":"MatchEnded","Data":{"MatchGuid":"win-null","WinnerTeamNum":null}}`,
			wantNil: true,
		},
		{
			guid:    "win-omitted",
			jsonMsg: `{"Event":"MatchEnded","Data":{"MatchGuid":"win-omitted"}}`,
			wantNil: true,
		},
		{
			guid:     "win-unhandled-neg1",
			jsonMsg:  `{"Event":"MatchEnded","Data":{"MatchGuid":"win-unhandled-neg1","WinnerTeamNum":-1}}`,
			wantNil:  false,
			wantTeam: -1,
		},
		{
			guid:     "win-unhandled-2",
			jsonMsg:  `{"Event":"MatchEnded","Data":{"MatchGuid":"win-unhandled-2","WinnerTeamNum":2}}`,
			wantNil:  false,
			wantTeam: 2,
		},
	}

	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for _, tc := range cases {
			conn.WriteMessage(websocket.TextMessage, []byte(tc.jsonMsg))
		}
		time.Sleep(200 * time.Millisecond)
	}))
	defer server.Close()

	store := newMockStore()
	tracker, _ := statsapi.NewTracker(store, statsapi.TrackerConfig{TriggerThreshold: 100})
	handler := &mockPlayerHandler{}

	listener, err := statsapi.NewListener(statsapi.ListenerConfig{
		Address:  "ws://" + server.Listener.Addr().String(),
		Protocol: "websocket",
	}, tracker, nil, statsapi.WithPlayerEventHandler(handler))
	if err != nil {
		t.Fatalf("NewListener error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go listener.Start(ctx)

	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(handler.getMatchEndeds()) >= len(cases) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	listener.Stop()

	endeds := handler.getMatchEndeds()
	if len(endeds) != len(cases) {
		t.Fatalf("expected %d MatchEnded events received, got %d", len(cases), len(endeds))
	}

	for i, tc := range cases {
		got := endeds[i]
		if got.MatchGUID != tc.guid {
			t.Errorf("[%d] expected GUID %s, got %s", i, tc.guid, got.MatchGUID)
		}
		if tc.wantNil {
			if got.WinnerTeamNum != nil {
				t.Errorf("[%d] expected WinnerTeamNum to be nil, got pointer to %d", i, *got.WinnerTeamNum)
			}
		} else {
			if got.WinnerTeamNum == nil {
				t.Fatalf("[%d] expected WinnerTeamNum to be non-nil", i)
			}
			if *got.WinnerTeamNum != tc.wantTeam {
				t.Errorf("[%d] expected WinnerTeamNum == %d, got %d", i, tc.wantTeam, *got.WinnerTeamNum)
			}
		}
	}
}

func TestAdversarial_Listener_StartStop_Idempotence(t *testing.T) {
	store := newMockStore()
	tracker, _ := statsapi.NewTracker(store, statsapi.TrackerConfig{TriggerThreshold: 10})

	listener, err := statsapi.NewListener(statsapi.ListenerConfig{
		Address: "127.0.0.1:49999", // Unconnected dummy port
	}, tracker, nil)
	if err != nil {
		t.Fatalf("NewListener error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go listener.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Second start should return error
	err2 := listener.Start(ctx)
	if err2 == nil {
		t.Errorf("expected error starting already running listener, got nil")
	}

	// Multiple Stop calls must be idempotent and not panic
	listener.Stop()
	listener.Stop()
	listener.Stop()

	if listener.IsConnected() {
		t.Errorf("expected IsConnected() to be false after Stop()")
	}
}
