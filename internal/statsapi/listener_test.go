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

func TestEventData_UnmarshalJSON_PolymorphicObject(t *testing.T) {
	rawJSON := `{
		"Event": "UpdateState",
		"Data": {
			"MatchGuid": "guid-obj-123",
			"Playlist": 13,
			"Players": [
				{
					"Name": "RocketPro",
					"PrimaryId": "Steam|76561198000000001|0",
					"TeamNum": 0,
					"Score": 450,
					"Goals": 2,
					"Assists": 1,
					"Saves": 3,
					"Shots": 5,
					"Demos": 1
				}
			],
			"Game": {
				"PlaylistId": 13,
				"TimeSeconds": 240,
				"bOvertime": false
			}
		}
	}`

	var msg statsapi.EventMessage
	if err := json.Unmarshal([]byte(rawJSON), &msg); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}

	if msg.Event != "UpdateState" {
		t.Errorf("expected Event=UpdateState, got %s", msg.Event)
	}
	if msg.Data.MatchGuid != "guid-obj-123" {
		t.Errorf("expected MatchGuid=guid-obj-123, got %s", msg.Data.MatchGuid)
	}
	if msg.Data.Playlist != 13 {
		t.Errorf("expected Playlist=13, got %d", msg.Data.Playlist)
	}
	if msg.Data.GetPlaylist() != 13 {
		t.Errorf("expected GetPlaylist()=13, got %d", msg.Data.GetPlaylist())
	}
	if len(msg.Data.Players) != 1 {
		t.Fatalf("expected 1 player, got %d", len(msg.Data.Players))
	}
	p := msg.Data.Players[0]
	if p.Name != "RocketPro" || p.PrimaryId != "Steam|76561198000000001|0" || p.TeamNum != 0 {
		t.Errorf("unexpected player fields: %+v", p)
	}
	if p.Score != 450 || p.Goals != 2 || p.Assists != 1 || p.Saves != 3 || p.Shots != 5 || p.Demos != 1 {
		t.Errorf("unexpected player stats: %+v", p)
	}
	if msg.Data.Game == nil || msg.Data.Game.PlaylistId != 13 || msg.Data.Game.TimeSeconds != 240 || msg.Data.Game.Overtime != false {
		t.Errorf("unexpected Game fields: %+v", msg.Data.Game)
	}
	if len(msg.Data.Raw) == 0 {
		t.Errorf("expected Raw bytes to be populated")
	}
}

func TestEventData_UnmarshalJSON_PolymorphicString(t *testing.T) {
	rawJSON := `{
		"Event": "UpdateState",
		"Data": "{\"MatchGuid\":\"guid-str-456\",\"Game\":{\"PlaylistId\":2},\"Players\":[{\"Name\":\"EpicGamer\",\"PrimaryId\":\"Epic|34a02cf8f4414e29b15921876da36f9a|0\",\"TeamNum\":1}]}"
	}`

	var msg statsapi.EventMessage
	if err := json.Unmarshal([]byte(rawJSON), &msg); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}

	if msg.Event != "UpdateState" {
		t.Errorf("expected Event=UpdateState, got %s", msg.Event)
	}
	if msg.Data.MatchGuid != "guid-str-456" {
		t.Errorf("expected MatchGuid=guid-str-456, got %s", msg.Data.MatchGuid)
	}
	if msg.Data.GetPlaylist() != 2 {
		t.Errorf("expected GetPlaylist()=2 from Game.PlaylistId, got %d", msg.Data.GetPlaylist())
	}
	if len(msg.Data.Players) != 1 {
		t.Fatalf("expected 1 player, got %d", len(msg.Data.Players))
	}
	p := msg.Data.Players[0]
	if p.Name != "EpicGamer" || p.PrimaryId != "Epic|34a02cf8f4414e29b15921876da36f9a|0" || p.TeamNum != 1 {
		t.Errorf("unexpected player fields: %+v", p)
	}
}

func TestEventData_UnmarshalJSON_WinnerTeamNumVariants(t *testing.T) {
	// Case 1: WinnerTeamNum = 0 (Blue)
	json0 := `{"MatchGuid":"guid-win-0","WinnerTeamNum":0}`
	var ed0 statsapi.EventData
	if err := json.Unmarshal([]byte(json0), &ed0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ed0.WinnerTeamNum == nil {
		t.Fatal("expected WinnerTeamNum to be non-nil for 0")
	}
	if *ed0.WinnerTeamNum != 0 {
		t.Errorf("expected *WinnerTeamNum == 0, got %d", *ed0.WinnerTeamNum)
	}

	// Case 2: WinnerTeamNum = 1 (Orange)
	json1 := `{"MatchGuid":"guid-win-1","WinnerTeamNum":1}`
	var ed1 statsapi.EventData
	if err := json.Unmarshal([]byte(json1), &ed1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ed1.WinnerTeamNum == nil {
		t.Fatal("expected WinnerTeamNum to be non-nil for 1")
	}
	if *ed1.WinnerTeamNum != 1 {
		t.Errorf("expected *WinnerTeamNum == 1, got %d", *ed1.WinnerTeamNum)
	}

	// Case 3: WinnerTeamNum omitted
	jsonMissing := `{"MatchGuid":"guid-win-none"}`
	var edMissing statsapi.EventData
	if err := json.Unmarshal([]byte(jsonMissing), &edMissing); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if edMissing.WinnerTeamNum != nil {
		t.Errorf("expected WinnerTeamNum to be nil when omitted, got %v", *edMissing.WinnerTeamNum)
	}

	// Case 4: WinnerTeamNum null
	jsonNull := `{"MatchGuid":"guid-win-null","WinnerTeamNum":null}`
	var edNull statsapi.EventData
	if err := json.Unmarshal([]byte(jsonNull), &edNull); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if edNull.WinnerTeamNum != nil {
		t.Errorf("expected WinnerTeamNum to be nil when null, got %v", *edNull.WinnerTeamNum)
	}
}

func TestEventData_UnmarshalJSON_EdgeCases(t *testing.T) {
	// Empty data
	var edEmpty statsapi.EventData
	if err := edEmpty.UnmarshalJSON([]byte{}); err != nil {
		t.Errorf("expected nil error on empty bytes, got %v", err)
	}

	// Whitespace data
	var edWS statsapi.EventData
	if err := edWS.UnmarshalJSON([]byte("   \n\t")); err != nil {
		t.Errorf("expected nil error on whitespace bytes, got %v", err)
	}

	// Empty string payload ("")
	var edEmptyStr statsapi.EventData
	if err := edEmptyStr.UnmarshalJSON([]byte(`""`)); err != nil {
		t.Errorf("expected nil error on empty string payload, got %v", err)
	}

	// Malformed JSON object
	var edMalformedObj statsapi.EventData
	if err := edMalformedObj.UnmarshalJSON([]byte(`{not valid}`)); err == nil {
		t.Errorf("expected error on malformed JSON object, got nil")
	}

	// Malformed JSON string
	var edMalformedStr statsapi.EventData
	if err := edMalformedStr.UnmarshalJSON([]byte(`"not valid json"`)); err == nil {
		t.Errorf("expected error on malformed inner JSON string, got nil")
	}
}

func TestEventData_GetPlaylist(t *testing.T) {
	// Top-level Playlist set
	ed1 := &statsapi.EventData{Playlist: 13}
	if ed1.GetPlaylist() != 13 {
		t.Errorf("expected 13, got %d", ed1.GetPlaylist())
	}

	// Game.PlaylistId set
	ed2 := &statsapi.EventData{Game: &statsapi.StatsGame{PlaylistId: 2}}
	if ed2.GetPlaylist() != 2 {
		t.Errorf("expected 2, got %d", ed2.GetPlaylist())
	}

	// Top-level takes priority
	ed3 := &statsapi.EventData{Playlist: 13, Game: &statsapi.StatsGame{PlaylistId: 2}}
	if ed3.GetPlaylist() != 13 {
		t.Errorf("expected 13, got %d", ed3.GetPlaylist())
	}

	// Neither set
	ed4 := &statsapi.EventData{}
	if ed4.GetPlaylist() != 0 {
		t.Errorf("expected 0, got %d", ed4.GetPlaylist())
	}

	// Nil receiver
	var edNil *statsapi.EventData
	if edNil.GetPlaylist() != 0 {
		t.Errorf("expected 0 for nil receiver, got %d", edNil.GetPlaylist())
	}
}

func TestParsePrimaryID(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantPlat    string
		wantAccount string
		wantSplit   int
		wantBot     bool
		wantErr     bool
	}{
		{
			name:        "Steam format",
			raw:         "Steam|76561198012345678|0",
			wantPlat:    "Steam",
			wantAccount: "76561198012345678",
			wantSplit:   0,
			wantBot:     false,
			wantErr:     false,
		},
		{
			name:        "Epic format",
			raw:         "Epic|34a02cf8f4414e29b15921876da36f9a|0",
			wantPlat:    "Epic",
			wantAccount: "34a02cf8f4414e29b15921876da36f9a",
			wantSplit:   0,
			wantBot:     false,
			wantErr:     false,
		},
		{
			name:        "PlayStation format",
			raw:         "PlayStation|psn_user_xyz|0",
			wantPlat:    "PlayStation",
			wantAccount: "psn_user_xyz",
			wantSplit:   0,
			wantBot:     false,
			wantErr:     false,
		},
		{
			name:        "XboxOne format",
			raw:         "XboxOne|2533274791|0",
			wantPlat:    "XboxOne",
			wantAccount: "2533274791",
			wantSplit:   0,
			wantBot:     false,
			wantErr:     false,
		},
		{
			name:        "Splitscreen guest",
			raw:         "Steam|76561198012345678|1",
			wantPlat:    "Steam",
			wantAccount: "76561198012345678",
			wantSplit:   1,
			wantBot:     false,
			wantErr:     false,
		},
		{
			name:        "AI Bot Unknown",
			raw:         "Unknown|0|0",
			wantPlat:    "Unknown",
			wantAccount: "0",
			wantSplit:   0,
			wantBot:     true,
			wantErr:     false,
		},
		{
			name:        "AI Bot account 0",
			raw:         "Bot|0|0",
			wantPlat:    "Bot",
			wantAccount: "0",
			wantSplit:   0,
			wantBot:     true,
			wantErr:     false,
		},
		{
			name:    "Empty input",
			raw:     "",
			wantErr: true,
		},
		{
			name:    "Missing pipes",
			raw:     "Steam76561198012345678",
			wantErr: true,
		},
		{
			name:    "Too few segments",
			raw:     "Steam|76561198012345678",
			wantErr: true,
		},
		{
			name:    "Too many segments",
			raw:     "Steam|76561198012345678|0|extra",
			wantErr: true,
		},
		{
			name:    "Non-numeric splitscreen",
			raw:     "Steam|76561198012345678|guest",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := statsapi.ParsePrimaryID(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for raw=%q, got nil", tt.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for raw=%q: %v", tt.raw, err)
			}
			if res.Platform != tt.wantPlat || res.AccountID != tt.wantAccount || res.Splitscreen != tt.wantSplit || res.IsBot != tt.wantBot {
				t.Errorf("ParsePrimaryID(%q) = %+v, want Plat=%q Account=%q Split=%d Bot=%v",
					tt.raw, res, tt.wantPlat, tt.wantAccount, tt.wantSplit, tt.wantBot)
			}
		})
	}
}

func TestStatsPlayer_Helpers(t *testing.T) {
	p := statsapi.StatsPlayer{
		Name:      "Maverick",
		PrimaryId: "Unknown|0|0",
	}
	if !p.IsBot() {
		t.Errorf("expected p.IsBot()=true for Unknown|0|0")
	}

	parsed, err := p.ParseID()
	if err != nil {
		t.Fatalf("unexpected ParseID error: %v", err)
	}
	if !parsed.IsBot {
		t.Errorf("expected parsed.IsBot=true")
	}

	human := statsapi.StatsPlayer{
		Name:      "Ace",
		PrimaryId: "Steam|76561198012345678|0",
	}
	if human.IsBot() {
		t.Errorf("expected human.IsBot()=false")
	}
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
