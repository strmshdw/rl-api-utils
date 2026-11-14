package statsapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/dank/rl-api-utils/internal/statsapi"
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
		name        string
		rawJSON     string
		expectNil   bool
		expectedVal int
		expectErr   bool
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
