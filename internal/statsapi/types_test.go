package statsapi_test

import (
	"encoding/json"
	"testing"

	"github.com/dank/rl-api-utils/internal/statsapi"
)

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
