package ballchasing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"testing"
)

func TestStress_EdgeCases_PlayerCounts(t *testing.T) {
	t.Run("replay with 0 players on both teams", func(t *testing.T) {
		payload := `{"id":"empty-teams","status":"ok","blue":{"color":"blue","goals":0,"players":[]},"orange":{"color":"orange","goals":0,"players":[]}}`
		var detail ReplayDetail
		if err := json.Unmarshal([]byte(payload), &detail); err != nil {
			t.Fatalf("unmarshal 0 players replay failed: %v", err)
		}
		if len(detail.Blue.Players) != 0 || len(detail.Orange.Players) != 0 {
			t.Fatalf("expected 0 players on both teams")
		}
	})

	t.Run("replay with 8 players (4v4 Chaos mode)", func(t *testing.T) {
		generatePlayers := func(prefix string, count int) string {
			var b bytes.Buffer
			b.WriteString("[")
			for i := 0; i < count; i++ {
				if i > 0 {
					b.WriteString(",")
				}
				fmt.Fprintf(&b, `{
					"name": "%s_Player_%d",
					"id": { "platform": "steam", "id": "76561198000000%s%d" },
					"car_id": %d, "car_name": "Fennec",
					"score": %d, "goals": %d, "assists": 1, "saves": 2, "shots": 3,
					"demos": { "inflicted": %d, "taken": 1 },
					"stats": {
						"core": { "score": %d, "goals": %d, "assists": 1, "saves": 2, "shots": 3 },
						"boost": { "bpm": 350.0, "amount_collected": 2000 },
						"movement": { "avg_speed": 1400.0 },
						"positioning": { "avg_distance_to_ball": 2500.0 },
						"demo": { "inflicted": %d, "taken": 1 }
					}
				}`, prefix, i, prefix, i, 42+i, 400+i*50, i, i, 400+i*50, i, i)
			}
			b.WriteString("]")
			return b.String()
		}

		payload := fmt.Sprintf(`{
			"id": "chaos-4v4", "match_type": "online", "team_size": 4, "playlist_id": "casual-chaos",
			"blue": { "color": "blue", "goals": 4, "players": %s },
			"orange": { "color": "orange", "goals": 2, "players": %s }
		}`, generatePlayers("blue", 4), generatePlayers("orange", 4))

		var detail ReplayDetail
		if err := json.Unmarshal([]byte(payload), &detail); err != nil {
			t.Fatalf("unmarshal 8 players 4v4 replay failed: %v", err)
		}
		if len(detail.Blue.Players) != 4 || len(detail.Orange.Players) != 4 {
			t.Fatalf("expected 4 players per team")
		}
		for i, p := range detail.Blue.Players {
			if p.Name != fmt.Sprintf("blue_Player_%d", i) || p.Score != 400+i*50 {
				t.Errorf("blue player %d mismatch: %+v", i, p)
			}
			if p.Core == nil || p.Boost == nil || p.Movement == nil || p.Positioning == nil || p.Demo == nil {
				t.Errorf("blue player %d has nil telemetry category pointer", i)
			}
		}
	})
}

func TestStress_EdgeCases_OvertimeAndDuration(t *testing.T) {
	testCases := []struct {
		name            string
		duration        int
		overtime        bool
		overtimeSeconds int
	}{
		{"1 hour overtime", 3600 + 300, true, 3600},
		{"2 hours overtime", 7200 + 300, true, 7200},
		{"zero duration zero overtime", 0, false, 0},
		{"overtime false with 0 overtime_seconds", 300, false, 0},
		{"overtime true with 0 overtime_seconds", 300, true, 0},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			payload := fmt.Sprintf(`{"id":"ot-test","duration":%d,"overtime":%v,"overtime_seconds":%d}`, tc.duration, tc.overtime, tc.overtimeSeconds)
			var item ReplayItem
			if err := json.Unmarshal([]byte(payload), &item); err != nil {
				t.Fatalf("unmarshal overtime payload failed: %v", err)
			}
			if item.Duration != tc.duration || item.Overtime != tc.overtime || item.OvertimeSeconds != tc.overtimeSeconds {
				t.Errorf("unexpected overtime fields: %+v", item)
			}
		})
	}
}

func TestStress_EdgeCases_CarsAndCamera(t *testing.T) {
	t.Run("unknown future car ID and custom car name", func(t *testing.T) {
		payload := `{"name":"SciFiDriver","car_id":999999,"car_name":"CyberpunkHovercar2077","id":{"platform":"steam","id":"76561198000000001"}}`
		var p ReplayPlayer
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			t.Fatalf("unmarshal unknown car failed: %v", err)
		}
		if p.CarID != 999999 || p.CarName != "CyberpunkHovercar2077" {
			t.Errorf("unexpected car values: %+v", p)
		}
	})

	t.Run("missing camera settings entirely", func(t *testing.T) {
		var p ReplayPlayer
		if err := json.Unmarshal([]byte(`{"name":"NoCameraPlayer","id":{"platform":"epic","id":"epic-no-cam"}}`), &p); err != nil {
			t.Fatalf("unmarshal missing camera failed: %v", err)
		}
		if p.Camera != nil {
			t.Fatalf("expected Camera to be nil")
		}
	})

	t.Run("extreme camera setting values", func(t *testing.T) {
		payload := `{"name":"WeirdCamPlayer","camera":{"fov":360,"height":-50,"pitch":-90,"distance":1000,"stiffness":10.0,"swivel_speed":100.0,"transition_speed":50.0}}`
		var p ReplayPlayer
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			t.Fatalf("unmarshal extreme camera failed: %v", err)
		}
		if p.Camera == nil || p.Camera.FOV != 360 || p.Camera.Height != -50 || p.Camera.Pitch != -90 || p.Camera.Distance != 1000 {
			t.Errorf("camera values mismatch: %+v", p.Camera)
		}
	})
}

func TestStress_EdgeCases_NegativeStats(t *testing.T) {
	payload := `{
		"name": "NegativeStatPlayer",
		"score": -50, "goals": -1, "assists": -1, "saves": -1, "shots": -2,
		"demos": { "inflicted": -1, "taken": -1 },
		"stats": {
			"core": { "score": -50, "goals": -1, "assists": -1, "saves": -1, "shots": -2 },
			"demo": { "inflicted": -1, "taken": -1 }
		}
	}`
	var p ReplayPlayer
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		t.Fatalf("unmarshal negative stats failed: %v", err)
	}
	if p.Score != -50 || p.Goals != -1 || p.Assists != -1 || p.Saves != -1 || p.Shots != -2 {
		t.Errorf("negative stats mismatch: %+v", p)
	}
	if p.Core == nil || p.Core.Score != -50 || p.Demo == nil || p.Demo.Inflicted != -1 {
		t.Errorf("nested negative stats mismatch")
	}
}

func TestStress_EdgeCases_MissingPlatformIDs(t *testing.T) {
	testCases := []struct {
		name     string
		jsonPart string
		expPlat  string
		expID    string
	}{
		{"both empty", `{"name": "AI Bot 1", "id": {"platform": "", "id": ""}}`, "", ""},
		{"missing platform", `{"name": "Player", "id": {"id": "12345"}}`, "", "12345"},
		{"missing id string", `{"name": "Player", "id": {"platform": "steam"}}`, "steam", ""},
		{"omitted id field completely", `{"name": "AI Bot 2"}`, "", ""},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var p ReplayPlayer
			if err := json.Unmarshal([]byte(tc.jsonPart), &p); err != nil {
				t.Fatalf("unmarshal player failed: %v", err)
			}
			if p.ID.Platform != tc.expPlat || p.ID.ID != tc.expID {
				t.Errorf("ID mismatch: got %+v, want (%q,%q)", p.ID, tc.expPlat, tc.expID)
			}
		})
	}
}

func TestStress_EdgeCases_ExtremeNumericSlugs(t *testing.T) {
	tests := []struct {
		slug string
		want int
	}{
		{"ranked-duels", 10}, {"ranked-doubles", 11}, {"ranked-solo-standard", 12},
		{"ranked-standard", 13}, {"ranked-hoops", 27}, {"ranked-rumble", 28},
		{"ranked-dropshot", 29}, {"ranked-snowday", 30}, {"tournament", 34},
		{"casual-standard", 3}, {"casual-doubles", 2}, {"casual-duels", 1}, {"casual-chaos", 4},
		{"100", 100}, {"999999", 999999}, {strconv.Itoa(math.MaxInt32), math.MaxInt32},
		{"00013", 13}, {"  22  ", 22},
		{"999999999999999999999999999999999999999999999999999999999999999999999999", 0},
		{"9223372036854775807", func() int {
			if id, err := strconv.Atoi("9223372036854775807"); err == nil && id > 0 {
				return id
			}
			return 0
		}()},
		{"0", 0}, {"-0", 0}, {"0000", 0}, {"-1", 0}, {"-999999", 0}, {strconv.Itoa(math.MinInt32), 0},
		{"0x10", 0}, {"0b101", 0}, {"1e5", 0}, {"3.14", 0}, {"NaN", 0},
		{"Infinity", 0}, {"-Infinity", 0}, {"", 0}, {"   ", 0}, {"unknown-future-slug", 0},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("slug_%s", tt.slug), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("MapPlaylistSlugToID panicked for %q: %v", tt.slug, r)
				}
			}()
			if got := MapPlaylistSlugToID(tt.slug); got != tt.want {
				t.Errorf("MapPlaylistSlugToID(%q) = %d; want %d", tt.slug, got, tt.want)
			}
		})
	}
}
