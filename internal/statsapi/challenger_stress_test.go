package statsapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
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

// -----------------------------------------------------------------------------
// 1. POLYMORPHIC EVENTDATA FUZZING & MALFORMED PAYLOAD RESILIENCE
// -----------------------------------------------------------------------------

func TestAdversarial_EventData_DeeplyNestedJSON(t *testing.T) {
	// Construct deeply nested JSON object (depth = 100) inside Data
	var b bytes.Buffer
	b.WriteString(`{"Event":"UpdateState","Data":{"MatchGuid":"nested-1","Playlist":13,"Extra":`)
	depth := 100
	for i := 0; i < depth; i++ {
		b.WriteString(`{"nest":`)
	}
	b.WriteString(`42`)
	for i := 0; i < depth; i++ {
		b.WriteString(`}`)
	}
	b.WriteString(`}}`)

	var msg statsapi.EventMessage
	if err := json.Unmarshal(b.Bytes(), &msg); err != nil {
		t.Fatalf("deeply nested JSON failed to unmarshal: %v", err)
	}
	if msg.Data.MatchGuid != "nested-1" {
		t.Errorf("expected MatchGuid 'nested-1', got %q", msg.Data.MatchGuid)
	}
	if msg.Data.Playlist != 13 {
		t.Errorf("expected Playlist 13, got %d", msg.Data.Playlist)
	}
}

func TestAdversarial_EventData_StringEscapedFuzzing(t *testing.T) {
	cases := []struct {
		name      string
		rawJSON   string
		expectErr bool
		checkFn   func(t *testing.T, ed *statsapi.EventData)
	}{
		{
			name:      "Valid escaped string",
			rawJSON:   `"{\"MatchGuid\":\"str-1\",\"Playlist\":11,\"WinnerTeamNum\":0}"`,
			expectErr: false,
			checkFn: func(t *testing.T, ed *statsapi.EventData) {
				if ed.MatchGuid != "str-1" || ed.Playlist != 11 || ed.WinnerTeamNum == nil || *ed.WinnerTeamNum != 0 {
					t.Errorf("unexpected fields: %+v", ed)
				}
			},
		},
		{
			name:      "Escaped string with leading/trailing whitespace inside",
			rawJSON:   `"  {\"MatchGuid\":\"str-ws\"}  "`,
			expectErr: false,
			checkFn: func(t *testing.T, ed *statsapi.EventData) {
				if ed.MatchGuid != "str-ws" {
					t.Errorf("expected MatchGuid 'str-ws', got %q", ed.MatchGuid)
				}
			},
		},
		{
			name:      "Escaped string containing empty object",
			rawJSON:   `"{}"`,
			expectErr: false,
			checkFn: func(t *testing.T, ed *statsapi.EventData) {
				if ed.MatchGuid != "" {
					t.Errorf("expected empty MatchGuid, got %q", ed.MatchGuid)
				}
			},
		},
		{
			name:      "Escaped string containing empty string",
			rawJSON:   `""`,
			expectErr: false,
			checkFn: func(t *testing.T, ed *statsapi.EventData) {
				if ed.MatchGuid != "" {
					t.Errorf("expected empty MatchGuid, got %q", ed.MatchGuid)
				}
			},
		},
		{
			name:      "Escaped string containing whitespace only",
			rawJSON:   `"   \t\n  "`,
			expectErr: false,
			checkFn: func(t *testing.T, ed *statsapi.EventData) {
				if ed.MatchGuid != "" {
					t.Errorf("expected empty EventData, got %+v", ed)
				}
			},
		},
		{
			name:      "Escaped string containing 'null'",
			rawJSON:   `"null"`,
			expectErr: false,
			checkFn: func(t *testing.T, ed *statsapi.EventData) {
				if ed.MatchGuid != "" || ed.WinnerTeamNum != nil {
					t.Errorf("expected empty EventData on null, got %+v", ed)
				}
			},
		},
		{
			name:      "Escaped string containing JSON number",
			rawJSON:   `"12345"`,
			expectErr: true,
		},
		{
			name:      "Escaped string containing JSON array",
			rawJSON:   `"[1, 2, 3]"`,
			expectErr: true,
		},
		{
			name:      "Escaped string containing boolean true",
			rawJSON:   `"true"`,
			expectErr: true,
		},
		{
			name:      "Escaped string containing truncated inner JSON",
			rawJSON:   `"{\"MatchGuid\":\"trunc"`,
			expectErr: true,
		},
		{
			name:      "Outer string with unescaped internal quotes",
			rawJSON:   `"{"MatchGuid": "bad"}"`,
			expectErr: true,
		},
		{
			name:      "Raw JSON array instead of object/string",
			rawJSON:   `[{"MatchGuid":"arr"}]`,
			expectErr: true,
		},
		{
			name:      "Raw JSON number",
			rawJSON:   `99999`,
			expectErr: true,
		},
		{
			name:      "Raw JSON boolean",
			rawJSON:   `false`,
			expectErr: true,
		},
		{
			name:      "Raw null",
			rawJSON:   `null`,
			expectErr: false,
			checkFn: func(t *testing.T, ed *statsapi.EventData) {
				if ed.WinnerTeamNum != nil {
					t.Errorf("expected nil WinnerTeamNum on raw null")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ed statsapi.EventData
			err := ed.UnmarshalJSON([]byte(tc.rawJSON))
			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected error for payload %s, got nil", tc.rawJSON)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error for payload %s: %v", tc.rawJSON, err)
				}
				if tc.checkFn != nil {
					tc.checkFn(t, &ed)
				}
			}
		})
	}
}

func TestAdversarial_EventData_TruncatedByteFuzzing(t *testing.T) {
	// Take valid JSON payloads and truncate them at every byte index
	validPayloads := []string{
		`{"MatchGuid":"fuzz-1","Playlist":13,"WinnerTeamNum":0,"Players":[{"Name":"P1","PrimaryId":"Steam|123|0","TeamNum":0}]}`,
		`"{\"MatchGuid\":\"fuzz-2\",\"Playlist\":2,\"WinnerTeamNum\":1,\"Players\":[]}"`,
	}

	for idx, vp := range validPayloads {
		b := []byte(vp)
		for i := 1; i < len(b)-1; i++ {
			truncated := b[:i]
			var ed statsapi.EventData
			// Must not panic on truncated byte sequences
			_ = ed.UnmarshalJSON(truncated)
		}
		t.Logf("Payload %d: successfully survived all %d truncation slices without panic", idx+1, len(b))
	}
}

func TestAdversarial_EventData_CorruptedByteFuzzing(t *testing.T) {
	// Mutate valid payload with random byte injections/replacements
	base := []byte(`{"MatchGuid":"corrupt-1","Playlist":13,"WinnerTeamNum":0,"Players":[{"Name":"Tester","PrimaryId":"Steam|1|0","TeamNum":0,"Score":100}]}`)
	r := rand.New(rand.NewSource(42))

	iterations := 2000
	for i := 0; i < iterations; i++ {
		mutated := make([]byte, len(base))
		copy(mutated, base)

		// Apply 1 to 5 random byte corruptions
		numMutations := r.Intn(5) + 1
		for m := 0; m < numMutations; m++ {
			pos := r.Intn(len(mutated))
			mutated[pos] = byte(r.Intn(256))
		}

		var ed statsapi.EventData
		// Must never panic or crash the process
		_ = ed.UnmarshalJSON(mutated)
	}
}

func TestAdversarial_EventData_HugeLobbyPayload(t *testing.T) {
	// Simulate huge lobby with 1,000 players and long strings
	players := make([]statsapi.StatsPlayer, 1000)
	longName := strings.Repeat("A", 500)
	for i := 0; i < 1000; i++ {
		players[i] = statsapi.StatsPlayer{
			Name:      fmt.Sprintf("%s-%d", longName, i),
			PrimaryId: fmt.Sprintf("Steam|76561198%09d|0", i),
			TeamNum:   i % 2,
			Score:     i * 10,
			Goals:     i,
			Assists:   i / 2,
			Saves:     i / 3,
			Shots:     i * 2,
			Demos:     i / 10,
		}
	}

	hugeMsg := statsapi.EventMessage{
		Event: "UpdateState",
		Data: statsapi.EventData{
			MatchGuid: "huge-lobby-guid",
			Playlist:  13,
			Players:   players,
			Game: &statsapi.StatsGame{
				PlaylistId:  13,
				TimeSeconds: 300,
				Overtime:    false,
			},
		},
	}

	data, err := json.Marshal(hugeMsg)
	if err != nil {
		t.Fatalf("failed to marshal huge message: %v", err)
	}

	var decoded statsapi.EventMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal huge message: %v", err)
	}

	if len(decoded.Data.Players) != 1000 {
		t.Fatalf("expected 1000 players, got %d", len(decoded.Data.Players))
	}
	if decoded.Data.Players[999].PrimaryId != "Steam|76561198000000999|0" {
		t.Errorf("unexpected player 999: %+v", decoded.Data.Players[999])
	}
}

// -----------------------------------------------------------------------------
// 2. WINNERTEAMNUM POINTER SEMANTICS RIGOROUS STRESS
// -----------------------------------------------------------------------------

func TestAdversarial_WinnerTeamNum_PointerSemantics(t *testing.T) {
	tests := []struct {
		name          string
		rawJSON       string
		expectNil     bool
		expectedVal   int
		expectErr     bool
	}{
		{
			name:        "Blue team win (0)",
			rawJSON:     `{"WinnerTeamNum": 0}`,
			expectNil:   false,
			expectedVal: 0,
		},
		{
			name:        "Orange team win (1)",
			rawJSON:     `{"WinnerTeamNum": 1}`,
			expectNil:   false,
			expectedVal: 1,
		},
		{
			name:      "Explicit null (match in progress / no winner / draw)",
			rawJSON:   `{"WinnerTeamNum": null}`,
			expectNil: true,
		},
		{
			name:      "Omitted field",
			rawJSON:   `{"MatchGuid": "guid-no-winner"}`,
			expectNil: true,
		},
		{
			name:        "Negative team num (-1, forfeit/draw/cancelled)",
			rawJSON:     `{"WinnerTeamNum": -1}`,
			expectNil:   false,
			expectedVal: -1,
		},
		{
			name:        "Unhandled positive team num (2, spectator/extra team)",
			rawJSON:     `{"WinnerTeamNum": 2}`,
			expectNil:   false,
			expectedVal: 2,
		},
		{
			name:        "Arbitrary large team num (255)",
			rawJSON:     `{"WinnerTeamNum": 255}`,
			expectNil:   false,
			expectedVal: 255,
		},
		{
			name:      "String quoted team num (\"0\") - type mismatch",
			rawJSON:   `{"WinnerTeamNum": "0"}`,
			expectErr: true,
		},
		{
			name:      "Floating point team num (0.5) - non-integer",
			rawJSON:   `{"WinnerTeamNum": 0.5}`,
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var ed statsapi.EventData
			err := ed.UnmarshalJSON([]byte(tc.rawJSON))
			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected error for %s, got nil", tc.rawJSON)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected unmarshal error: %v", err)
			}

			if tc.expectNil {
				if ed.WinnerTeamNum != nil {
					t.Fatalf("expected WinnerTeamNum to be nil, got pointer to %d", *ed.WinnerTeamNum)
				}
			} else {
				if ed.WinnerTeamNum == nil {
					t.Fatalf("expected WinnerTeamNum to be non-nil")
				}
				if *ed.WinnerTeamNum != tc.expectedVal {
					t.Fatalf("expected *WinnerTeamNum == %d, got %d", tc.expectedVal, *ed.WinnerTeamNum)
				}
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 3. AI BOT DETECTION RIGOROUS EDGE CASES
// -----------------------------------------------------------------------------

func TestAdversarial_BotDetection_Matrix(t *testing.T) {
	cases := []struct {
		name        string
		primaryID   string
		wantPlat    string
		wantAcc     string
		wantSplit   int
		wantBot     bool
		wantErr     bool
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
		conn.WriteMessage(websocket.TextMessage, []byte(``)) // empty
		conn.WriteMessage(websocket.TextMessage, []byte(`   `)) // whitespace
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
			guid:     "win-null",
			jsonMsg:  `{"Event":"MatchEnded","Data":{"MatchGuid":"win-null","WinnerTeamNum":null}}`,
			wantNil:  true,
		},
		{
			guid:     "win-omitted",
			jsonMsg:  `{"Event":"MatchEnded","Data":{"MatchGuid":"win-omitted"}}`,
			wantNil:  true,
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

func TestAdversarial_EventData_RawPayloadIntegrity(t *testing.T) {
	// Verify ed.Raw preservation for both object and string modes
	objPayload := `{"Event":"UpdateState","Data":{"MatchGuid":"raw-obj","Playlist":1}}`
	var msgObj statsapi.EventMessage
	if err := json.Unmarshal([]byte(objPayload), &msgObj); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if !bytes.Contains(msgObj.Data.Raw, []byte(`"MatchGuid":"raw-obj"`)) {
		t.Errorf("expected Raw bytes to contain MatchGuid, got %s", string(msgObj.Data.Raw))
	}

	strPayload := `{"Event":"UpdateState","Data":"{\"MatchGuid\":\"raw-str\",\"Playlist\":2}"}`
	var msgStr statsapi.EventMessage
	if err := json.Unmarshal([]byte(strPayload), &msgStr); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if !bytes.Contains(msgStr.Data.Raw, []byte(`"MatchGuid":"raw-str"`)) {
		t.Errorf("expected Raw bytes to contain unescaped JSON, got %s", string(msgStr.Data.Raw))
	}
}
