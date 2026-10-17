package ballchasing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// ============================================================================
// Empirical Stress Test Generator & Adversarial Verification Suite
// Milestone M1: internal/ballchasing Telemetry Unmarshaling
// ============================================================================

const canonicalNestedFixture = `{
	"id": "full-fixture-abc-123",
	"link": "https://ballchasing.com/replay/full-fixture-abc-123",
	"created": "2026-10-15T20:00:00Z",
	"date": "2026-10-15T19:50:00Z",
	"status": "ok",
	"rocket_league_id": "rl-guid-canonical",
	"match_guid": "rl-guid-canonical",
	"title": "Challenger Finals",
	"map_code": "stadium_p",
	"map_name": "DFH Stadium",
	"match_type": "online",
	"team_size": 3,
	"playlist_id": "ranked-standard",
	"playlist_name": "Ranked Standard 3v3",
	"duration": 360,
	"play_duration": 360,
	"overtime": true,
	"overtime_seconds": 60,
	"season": 22,
	"visibility": "public",
	"server": { "name": "USEast-123456" },
	"uploader": {
		"steam_id": "76561198000000001",
		"name": "ChallengerOne",
		"profile_url": "https://steamcommunity.com/id/challengerone",
		"avatar": "https://avatars.steamstatic.com/sample.jpg"
	},
	"min_rank": { "id": "grand-champion-1", "tier": 19, "division": 1, "name": "Grand Champion I" },
	"max_rank": { "id": "supersonic-legend", "tier": 22, "division": 1, "name": "Supersonic Legend" },
	"blue": {
		"color": "blue",
		"name": "Team Liquid",
		"goals": 3,
		"stats": {
			"ball": { "possession_time": 171.09, "time_in_side": 170.01 },
			"core": { "shots": 12, "goals": 3, "saves": 8, "assists": 2, "score": 1500 },
			"boost": { "amount_collected": 7703, "amount_stolen": 1420, "bpm": 1250.5 },
			"movement": { "avg_speed": 1540.2, "total_distance": 45000.5 },
			"positioning": { "avg_distance_to_ball": 2865.4, "time_defensive_third": 240.5 },
			"demo": { "inflicted": 4, "taken": 1 }
		},
		"players": [
			{
				"start_time": 0,
				"end_time": 360,
				"name": "mist",
				"id": { "platform": "steam", "id": "76561198245238200" },
				"car_id": 23,
				"car_name": "Octane",
				"camera": {
					"fov": 110,
					"height": 100,
					"pitch": -4,
					"distance": 270,
					"stiffness": 0.45,
					"swivel_speed": 4.5,
					"transition_speed": 1.2
				},
				"steering_sensitivity": 1.3,
				"rank": {
					"id": "grand-champion-2",
					"tier": 20,
					"division": 3,
					"name": "Grand Champion II",
					"mmr": 1540.5
				},
				"mvp": true,
				"stats": {
					"core": {
						"shots": 4,
						"shots_against": 3,
						"goals": 2,
						"goals_against": 1,
						"saves": 4,
						"assists": 1,
						"score": 650,
						"mvp": true,
						"shooting_percentage": 50.0,
						"epic_saves": 2,
						"clears": 7,
						"centers": 5,
						"aerial_hits": 14,
						"first_touches": 3,
						"flip_resets": 1,
						"crossbar_hits": 1,
						"bicycle_hits": 0,
						"juggle_hits": 0
					},
					"boost": {
						"bpm": 403.5,
						"bcpm": 395.2,
						"avg_amount": 54.2,
						"amount_collected": 2850,
						"amount_stolen": 450,
						"amount_collected_big": 1800,
						"amount_stolen_big": 300,
						"amount_collected_small": 1050,
						"amount_stolen_small": 150,
						"count_collected_big": 18,
						"count_stolen_big": 3,
						"count_collected_small": 87,
						"count_stolen_small": 12,
						"amount_overfill": 250,
						"amount_overfill_stolen": 50,
						"amount_used_while_supersonic": 320,
						"time_zero_boost": 12.5,
						"percent_zero_boost": 3.47,
						"time_full_boost": 45.0,
						"percent_full_boost": 12.5,
						"time_boost_0_25": 40.0,
						"time_boost_25_50": 60.0,
						"time_boost_50_75": 120.0,
						"time_boost_75_100": 140.0,
						"percent_boost_0_25": 11.1,
						"percent_boost_25_50": 16.7,
						"percent_boost_50_75": 33.3,
						"percent_boost_75_100": 38.9
					},
					"movement": {
						"avg_speed": 1540.0,
						"total_distance": 18500.0,
						"time_supersonic_speed": 75.0,
						"time_boost_speed": 110.0,
						"time_slow_speed": 175.0,
						"time_ground": 220.0,
						"time_low_air": 110.0,
						"time_high_air": 30.0,
						"time_powerslide": 14.5,
						"count_powerslide": 28,
						"avg_powerslide_duration": 0.52,
						"avg_speed_percentage": 67.5,
						"percent_slow_speed": 48.6,
						"percent_boost_speed": 30.6,
						"percent_supersonic_speed": 20.8,
						"percent_ground": 61.1,
						"percent_low_air": 30.6,
						"percent_high_air": 8.3
					},
					"positioning": {
						"avg_distance_to_ball": 2865.0,
						"avg_distance_to_ball_possession": 2100.0,
						"avg_distance_to_ball_no_possession": 3200.0,
						"avg_distance_to_mates": 2400.0,
						"time_defensive_third": 193.27,
						"time_neutral_third": 95.0,
						"time_offensive_third": 71.73,
						"time_defensive_half": 230.0,
						"time_offensive_half": 130.0,
						"time_behind_ball": 280.0,
						"time_infront_ball": 80.0,
						"time_most_back": 120.0,
						"time_most_forward": 110.0,
						"goals_against_while_last_defender": 0,
						"time_closest_to_ball": 130.0,
						"time_farthest_from_ball": 90.0,
						"percent_defensive_third": 53.7,
						"percent_offensive_third": 19.9,
						"percent_neutral_third": 26.4,
						"percent_defensive_half": 63.9,
						"percent_offensive_half": 36.1,
						"percent_behind_ball": 77.8,
						"percent_infront_ball": 22.2,
						"percent_most_back": 33.3,
						"percent_most_forward": 30.6,
						"percent_closest_to_ball": 36.1,
						"percent_farthest_from_ball": 25.0
					},
					"demo": {
						"inflicted": 2,
						"inflicted_in_defensive_half": 0,
						"inflicted_in_offensive_half": 2,
						"taken": 1,
						"taken_in_defensive_half": 1,
						"taken_in_offensive_half": 0
					}
				}
			}
		]
	},
	"orange": {
		"color": "orange",
		"name": "G2 Esports",
		"goals": 2,
		"players": []
	}
}`

// ----------------------------------------------------------------------------
// Suite 1: Corrupted & Malformed JSON Payloads
// ----------------------------------------------------------------------------

func TestStress_PayloadGenerators_Corrupted(t *testing.T) {
	corruptedCases := []struct {
		name    string
		payload string
	}{
		{"empty string", ""},
		{"whitespace only", "   \t\r\n   "},
		{"unclosed root object", "{"},
		{"unclosed root array", "["},
		{"trailing comma in object", `{"id": "123",}`},
		{"trailing comma in array", `{"blue": {"players": [,]}}`},
		{"unquoted key", `{id: "123"}`},
		{"single quotes", `{'id': '123'}`},
		{"missing colon", `{"id" "123"}`},
		{"double colon", `{"id":: "123"}`},
		{"invalid escape in string", `{"id": "abc\x123"}`},
		{"raw newline in string", "{\"id\": \"abc\n123\"}"},
		{"naked string literal", `"just a string"`},
		{"naked number", `12345`},
		{"naked boolean", `true`},
		{"naked null", `null`},
		{"invalid token sequence", `{{}}`},
		{"random binary garbage", string([]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0xFF})},
		{"invalid UTF-8 syntax tokens", string([]byte{0xFF, 0xFE, '{', '"', 'i', 'd', '"', ':', ' ', '"', '1', '"', '}'})},
	}

	for _, tc := range corruptedCases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic during unmarshal of corrupted payload %q: %v", tc.name, r)
				}
			}()

			var detail ReplayDetail
			err := json.Unmarshal([]byte(tc.payload), &detail)
			if err == nil && tc.payload != "null" {
				t.Fatalf("expected error for corrupted payload %q, got nil", tc.name)
			}

			var player ReplayPlayer
			errPlayer := json.Unmarshal([]byte(tc.payload), &player)
			if errPlayer == nil && tc.payload != "null" {
				t.Fatalf("expected error for corrupted player payload %q, got nil", tc.name)
			}
		})
	}

	t.Run("non-UTF8 bytes in string literal does not panic", func(t *testing.T) {
		payload := "{\"id\": \"\xFF\xFE\xFD\"}"
		var detail ReplayDetail
		if err := json.Unmarshal([]byte(payload), &detail); err != nil {
			t.Logf("unmarshal returned error for non-UTF8 string: %v", err)
		} else if len(detail.ID) == 0 {
			t.Errorf("expected non-empty ID string")
		}
	})
}

// ----------------------------------------------------------------------------
// Suite 2: Truncated JSON Payloads (Prefix Sweep)
// ----------------------------------------------------------------------------

func TestStress_PayloadGenerators_Truncated(t *testing.T) {
	raw := []byte(canonicalNestedFixture)
	totalLen := len(raw)

	// Sample cut points across the entire payload
	step := 25
	for cut := 1; cut < totalLen; cut += step {
		truncated := raw[:cut]
		t.Run(fmt.Sprintf("cut_at_%d_of_%d", cut, totalLen), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic during unmarshal of truncated payload at %d: %v", cut, r)
				}
			}()

			var detail ReplayDetail
			err := json.Unmarshal(truncated, &detail)
			if err == nil {
				t.Fatalf("expected error for truncated payload at byte %d, got nil", cut)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Suite 3: Null Fields & Missing Blocks
// ----------------------------------------------------------------------------

func TestStress_PayloadGenerators_NullFields(t *testing.T) {
	t.Run("all nullable top-level pointers null", func(t *testing.T) {
		payload := `{
			"id": "null-fields-1",
			"server": null,
			"uploader": null,
			"min_rank": null,
			"max_rank": null,
			"groups": null,
			"blue": {
				"color": "blue",
				"name": "Team Null",
				"goals": 0,
				"players": null,
				"stats": null
			},
			"orange": {
				"color": "orange",
				"name": "Team Null 2",
				"goals": 0,
				"players": null,
				"stats": null
			}
		}`
		var detail ReplayDetail
		if err := json.Unmarshal([]byte(payload), &detail); err != nil {
			t.Fatalf("unmarshal with null pointers failed: %v", err)
		}
		if detail.Server != nil || detail.Uploader != nil || detail.MinRank != nil || detail.MaxRank != nil {
			t.Fatalf("expected nil pointers, got: server=%v, uploader=%v, min_rank=%v, max_rank=%v",
				detail.Server, detail.Uploader, detail.MinRank, detail.MaxRank)
		}
		if detail.Blue.Stats != nil || detail.Orange.Stats != nil {
			t.Fatalf("expected nil team stats, got: blue=%v, orange=%v", detail.Blue.Stats, detail.Orange.Stats)
		}
	})

	t.Run("player telemetry categories all null", func(t *testing.T) {
		payload := `{
			"name": "GhostPlayer",
			"id": { "platform": "steam", "id": "76561198000000099" },
			"camera": null,
			"rank": null,
			"stats": {
				"core": null,
				"boost": null,
				"movement": null,
				"positioning": null,
				"demo": null,
				"ball": null
			},
			"core": null,
			"boost": null,
			"movement": null,
			"positioning": null,
			"demo": null,
			"ball": null
		}`
		var player ReplayPlayer
		if err := json.Unmarshal([]byte(payload), &player); err != nil {
			t.Fatalf("unmarshal player with null stats failed: %v", err)
		}
		if player.Camera != nil || player.Rank != nil {
			t.Fatalf("expected camera and rank to be nil, got: camera=%v, rank=%v", player.Camera, player.Rank)
		}
		if player.Core != nil || player.Boost != nil || player.Movement != nil || player.Positioning != nil || player.Demo != nil || player.Ball != nil {
			t.Fatalf("expected category pointers to be nil when stats categories are null")
		}
	})

	t.Run("empty object blocks", func(t *testing.T) {
		payload := `{
			"id": "empty-blocks",
			"server": {},
			"uploader": {},
			"min_rank": {},
			"max_rank": {},
			"blue": { "players": [{ "id": {}, "camera": {}, "rank": {}, "stats": {} }] }
		}`
		var detail ReplayDetail
		if err := json.Unmarshal([]byte(payload), &detail); err != nil {
			t.Fatalf("unmarshal with empty blocks failed: %v", err)
		}
		if len(detail.Blue.Players) != 1 {
			t.Fatalf("len(Blue.Players) = %d; want 1", len(detail.Blue.Players))
		}
		p := detail.Blue.Players[0]
		if p.Camera == nil || p.Rank == nil || p.Stats == nil {
			t.Fatalf("expected non-nil allocated blocks for empty objects, got: camera=%v, rank=%v, stats=%v",
				p.Camera, p.Rank, p.Stats)
		}
	})
}

// ----------------------------------------------------------------------------
// Suite 4: Mixed-Case Key Payloads
// ----------------------------------------------------------------------------

func TestStress_PayloadGenerators_MixedCase(t *testing.T) {
	mixedCaseJSON := `{
		"ID": "mixed-case-001",
		"STATUS": "ok",
		"MATCH_GUID": "guid-mixed-case",
		"ROCKET_LEAGUE_ID": "rl-mixed-case",
		"REPLAY_TITLE": "Mixed Case Cup",
		"OVERTIME": true,
		"OVERTIME_SECONDS": 125,
		"BLUE": {
			"COLOR": "blue",
			"GOALS": 5,
			"PLAYERS": [
				{
					"NAME": "Kaydop",
					"SCORE": 780,
					"GOALS": 3,
					"ASSISTS": 2,
					"SAVES": 1,
					"SHOTS": 6
				}
			]
		}
	}`

	var detail ReplayDetail
	if err := json.Unmarshal([]byte(mixedCaseJSON), &detail); err != nil {
		t.Fatalf("unmarshal mixed-case JSON failed: %v", err)
	}
	if detail.ID != "mixed-case-001" {
		t.Errorf("detail.ID = %q; want mixed-case-001", detail.ID)
	}
	if detail.Status != "ok" {
		t.Errorf("detail.Status = %q; want ok", detail.Status)
	}
	if detail.Title != "Mixed Case Cup" {
		t.Errorf("detail.Title = %q; want Mixed Case Cup", detail.Title)
	}
	if detail.MatchGUID != "guid-mixed-case" {
		t.Errorf("detail.MatchGUID = %q; want guid-mixed-case", detail.MatchGUID)
	}
	if detail.OvertimeSeconds != 125 {
		t.Errorf("detail.OvertimeSeconds = %d; want 125", detail.OvertimeSeconds)
	}
	if len(detail.Blue.Players) != 1 {
		t.Fatalf("Blue.Players count = %d; want 1", len(detail.Blue.Players))
	}
	p := detail.Blue.Players[0]
	if p.Name != "Kaydop" {
		t.Errorf("player Name = %q; want Kaydop", p.Name)
	}
	if p.Score != 780 || p.Goals != 3 || p.Assists != 2 || p.Saves != 1 || p.Shots != 6 {
		t.Errorf("player stats mismatch: score=%d, goals=%d, assists=%d, saves=%d, shots=%d",
			p.Score, p.Goals, p.Assists, p.Saves, p.Shots)
	}
}

// ----------------------------------------------------------------------------
// Suite 5: Boundary Values & Extreme Numbers
// ----------------------------------------------------------------------------

func TestStress_PayloadGenerators_BoundaryValues(t *testing.T) {
	t.Run("extreme numeric ranges in replay metadata", func(t *testing.T) {
		payload := fmt.Sprintf(`{
			"id": "boundary-nums",
			"duration": %d,
			"play_duration": %d,
			"overtime": true,
			"overtime_seconds": %d,
			"season": %d,
			"team_size": %d
		}`, math.MaxInt32, math.MaxInt32, 86400, 99999, 100)

		var item ReplayItem
		if err := json.Unmarshal([]byte(payload), &item); err != nil {
			t.Fatalf("unmarshal extreme numbers failed: %v", err)
		}
		if item.Duration != math.MaxInt32 {
			t.Errorf("item.Duration = %d; want %d", item.Duration, math.MaxInt32)
		}
		if item.OvertimeSeconds != 86400 {
			t.Errorf("item.OvertimeSeconds = %d; want 86400", item.OvertimeSeconds)
		}
	})

	t.Run("extreme floating point telemetry values", func(t *testing.T) {
		payload := `{
			"name": "FloatStressPlayer",
			"steering_sensitivity": 9999.99,
			"stats": {
				"ball": { "possession_time": 1e9, "time_in_side": 0.0000001 },
				"boost": { "bpm": 99999.5, "avg_amount": 0.0, "percent_zero_boost": 100.0 },
				"movement": { "avg_speed": 2300.0, "total_distance": 9999999.9 },
				"positioning": { "avg_distance_to_ball": 1e6 }
			}
		}`
		var p ReplayPlayer
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			t.Fatalf("unmarshal float stress player failed: %v", err)
		}
		if p.SteeringSensitivity != 9999.99 {
			t.Errorf("p.SteeringSensitivity = %f; want 9999.99", p.SteeringSensitivity)
		}
		if p.Stats.Ball.PossessionTime != 1e9 {
			t.Errorf("PossessionTime = %f; want 1e9", p.Stats.Ball.PossessionTime)
		}
		if p.Stats.Boost.PercentZeroBoost != 100.0 {
			t.Errorf("PercentZeroBoost = %f; want 100.0", p.Stats.Boost.PercentZeroBoost)
		}
	})

	t.Run("large strings without buffer overflow or corruption", func(t *testing.T) {
		giantTitle := strings.Repeat("EpicGameHeader", 1000) // 14,000 chars
		giantMap := strings.Repeat("Stadium_Field_Custom_", 500)
		payload := fmt.Sprintf(`{"id": "large-str", "title": %q, "map_name": %q}`, giantTitle, giantMap)

		var item ReplayItem
		if err := json.Unmarshal([]byte(payload), &item); err != nil {
			t.Fatalf("unmarshal giant string fields failed: %v", err)
		}
		if item.Title != giantTitle {
			t.Errorf("giant title length mismatch: got %d, want %d", len(item.Title), len(giantTitle))
		}
		if item.MapName != giantMap {
			t.Errorf("giant map length mismatch: got %d, want %d", len(item.MapName), len(giantMap))
		}
	})
}

// ----------------------------------------------------------------------------
// Suite 6: Edge Cases — Player Counts (0 Players and 8 Players)
// ----------------------------------------------------------------------------

func TestStress_EdgeCases_PlayerCounts(t *testing.T) {
	t.Run("replay with 0 players on both teams", func(t *testing.T) {
		payload := `{
			"id": "empty-teams",
			"status": "ok",
			"blue": { "color": "blue", "goals": 0, "players": [] },
			"orange": { "color": "orange", "goals": 0, "players": [] }
		}`
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
					"car_id": %d,
					"car_name": "Fennec",
					"score": %d,
					"goals": %d,
					"assists": %d,
					"saves": %d,
					"shots": %d,
					"demos": { "inflicted": %d, "taken": %d },
					"stats": {
						"core": { "score": %d, "goals": %d, "assists": %d, "saves": %d, "shots": %d },
						"boost": { "bpm": 350.0, "amount_collected": 2000 },
						"movement": { "avg_speed": 1400.0 },
						"positioning": { "avg_distance_to_ball": 2500.0 },
						"demo": { "inflicted": %d, "taken": %d }
					}
				}`, prefix, i, prefix, i, 42+i, 400+i*50, i, 1, 2, 3, i, 1, 400+i*50, i, 1, 2, 3, i, 1)
			}
			b.WriteString("]")
			return b.String()
		}

		payload := fmt.Sprintf(`{
			"id": "chaos-4v4",
			"match_type": "online",
			"team_size": 4,
			"playlist_id": "casual-chaos",
			"blue": { "color": "blue", "goals": 4, "players": %s },
			"orange": { "color": "orange", "goals": 2, "players": %s }
		}`, generatePlayers("blue", 4), generatePlayers("orange", 4))

		var detail ReplayDetail
		if err := json.Unmarshal([]byte(payload), &detail); err != nil {
			t.Fatalf("unmarshal 8 players 4v4 replay failed: %v", err)
		}
		if len(detail.Blue.Players) != 4 {
			t.Fatalf("len(Blue.Players) = %d; want 4", len(detail.Blue.Players))
		}
		if len(detail.Orange.Players) != 4 {
			t.Fatalf("len(Orange.Players) = %d; want 4", len(detail.Orange.Players))
		}

		// Verify every player has populated stats and distinct identities
		for i, p := range detail.Blue.Players {
			expectedName := fmt.Sprintf("blue_Player_%d", i)
			if p.Name != expectedName {
				t.Errorf("blue player %d name = %q; want %q", i, p.Name, expectedName)
			}
			if p.Score != 400+i*50 {
				t.Errorf("blue player %d score = %d; want %d", i, p.Score, 400+i*50)
			}
			if p.Core == nil || p.Boost == nil || p.Movement == nil || p.Positioning == nil || p.Demo == nil {
				t.Errorf("blue player %d has nil telemetry category pointer", i)
			}
		}
	})
}

// ----------------------------------------------------------------------------
// Suite 7: Edge Cases — Overtime > 1 Hour & Extreme Durations
// ----------------------------------------------------------------------------

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
		{"overtime true with 0 overtime_seconds (golden goal instant)", 300, true, 0},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			payload := fmt.Sprintf(`{
				"id": "ot-test",
				"duration": %d,
				"overtime": %v,
				"overtime_seconds": %d
			}`, tc.duration, tc.overtime, tc.overtimeSeconds)

			var item ReplayItem
			if err := json.Unmarshal([]byte(payload), &item); err != nil {
				t.Fatalf("unmarshal overtime payload failed: %v", err)
			}
			if item.Duration != tc.duration {
				t.Errorf("Duration = %d; want %d", item.Duration, tc.duration)
			}
			if item.Overtime != tc.overtime {
				t.Errorf("Overtime = %v; want %v", item.Overtime, tc.overtime)
			}
			if item.OvertimeSeconds != tc.overtimeSeconds {
				t.Errorf("OvertimeSeconds = %d; want %d", item.OvertimeSeconds, tc.overtimeSeconds)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Suite 8: Edge Cases — Unknown Cars & Missing Camera Settings
// ----------------------------------------------------------------------------

func TestStress_EdgeCases_CarsAndCamera(t *testing.T) {
	t.Run("unknown future car ID and custom car name", func(t *testing.T) {
		payload := `{
			"name": "SciFiDriver",
			"car_id": 999999,
			"car_name": "CyberpunkHovercar2077",
			"id": { "platform": "steam", "id": "76561198000000001" }
		}`
		var p ReplayPlayer
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			t.Fatalf("unmarshal unknown car failed: %v", err)
		}
		if p.CarID != 999999 {
			t.Errorf("CarID = %d; want 999999", p.CarID)
		}
		if p.CarName != "CyberpunkHovercar2077" {
			t.Errorf("CarName = %q; want CyberpunkHovercar2077", p.CarName)
		}
	})

	t.Run("missing camera settings entirely", func(t *testing.T) {
		payload := `{
			"name": "NoCameraPlayer",
			"id": { "platform": "epic", "id": "epic-no-cam" }
		}`
		var p ReplayPlayer
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			t.Fatalf("unmarshal missing camera failed: %v", err)
		}
		if p.Camera != nil {
			t.Fatalf("expected Camera to be nil, got: %+v", p.Camera)
		}
	})

	t.Run("extreme camera setting values", func(t *testing.T) {
		payload := `{
			"name": "WeirdCamPlayer",
			"camera": {
				"fov": 360,
				"height": -50,
				"pitch": -90,
				"distance": 1000,
				"stiffness": 10.0,
				"swivel_speed": 100.0,
				"transition_speed": 50.0
			}
		}`
		var p ReplayPlayer
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			t.Fatalf("unmarshal extreme camera failed: %v", err)
		}
		if p.Camera == nil {
			t.Fatal("expected non-nil Camera")
		}
		if p.Camera.FOV != 360 || p.Camera.Height != -50 || p.Camera.Pitch != -90 || p.Camera.Distance != 1000 {
			t.Errorf("camera values mismatch: %+v", p.Camera)
		}
	})
}

// ----------------------------------------------------------------------------
// Suite 9: Edge Cases — Negative Stats
// ----------------------------------------------------------------------------

func TestStress_EdgeCases_NegativeStats(t *testing.T) {
	payload := `{
		"name": "NegativeStatPlayer",
		"score": -50,
		"goals": -1,
		"assists": -1,
		"saves": -1,
		"shots": -2,
		"demos": { "inflicted": -1, "taken": -1 },
		"stats": {
			"core": {
				"score": -50,
				"goals": -1,
				"assists": -1,
				"saves": -1,
				"shots": -2
			},
			"demo": {
				"inflicted": -1,
				"taken": -1
			}
		}
	}`

	var p ReplayPlayer
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		t.Fatalf("unmarshal negative stats failed: %v", err)
	}
	if p.Score != -50 {
		t.Errorf("p.Score = %d; want -50", p.Score)
	}
	if p.Goals != -1 || p.Assists != -1 || p.Saves != -1 || p.Shots != -2 {
		t.Errorf("negative stats mismatch: goals=%d, assists=%d, saves=%d, shots=%d",
			p.Goals, p.Assists, p.Saves, p.Shots)
	}
	if p.Core == nil || p.Core.Score != -50 {
		t.Errorf("p.Core score mismatch: %+v", p.Core)
	}
	if p.Demo == nil || p.Demo.Inflicted != -1 {
		t.Errorf("p.Demo mismatch: %+v", p.Demo)
	}
}

// ----------------------------------------------------------------------------
// Suite 10: Edge Cases — Missing & Empty Platform IDs
// ----------------------------------------------------------------------------

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
			if p.ID.Platform != tc.expPlat {
				t.Errorf("Platform = %q; want %q", p.ID.Platform, tc.expPlat)
			}
			if p.ID.ID != tc.expID {
				t.Errorf("ID = %q; want %q", p.ID.ID, tc.expID)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Suite 11: Edge Cases — Extreme Numeric Slugs (MapPlaylistSlugToID)
// ----------------------------------------------------------------------------

func TestStress_EdgeCases_ExtremeNumericSlugs(t *testing.T) {
	tests := []struct {
		slug string
		want int
	}{
		// Standard competitive mappings
		{"ranked-duels", 10},
		{"ranked-doubles", 11},
		{"ranked-solo-standard", 12},
		{"ranked-standard", 13},
		{"ranked-hoops", 27},
		{"ranked-rumble", 28},
		{"ranked-dropshot", 29},
		{"ranked-snowday", 30},
		{"tournament", 34},

		// Standard casual mappings
		{"casual-standard", 3},
		{"casual-doubles", 2},
		{"casual-duels", 1},
		{"casual-chaos", 4},

		// Extreme positive integer strings
		{"100", 100},
		{"999999", 999999},
		{strconv.Itoa(math.MaxInt32), math.MaxInt32},
		{"00013", 13}, // Leading zeroes should parse cleanly as decimal 13
		{"  22  ", 22}, // Leading/trailing whitespace trimmed

		// BigInt / Int64 overflow (must gracefully return 0 without panicking)
		{"999999999999999999999999999999999999999999999999999999999999999999999999", 0},
		{"9223372036854775807", func() int {
			if id, err := strconv.Atoi("9223372036854775807"); err == nil && id > 0 {
				return id
			}
			return 0
		}()},

		// Zero and negative numbers (must return 0)
		{"0", 0},
		{"-0", 0},
		{"0000", 0},
		{"-1", 0},
		{"-999999", 0},
		{strconv.Itoa(math.MinInt32), 0},

		// Non-numeric, garbage, hex, floats
		{"0x10", 0},
		{"0b101", 0},
		{"1e5", 0},
		{"3.14", 0},
		{"NaN", 0},
		{"Infinity", 0},
		{"-Infinity", 0},
		{"", 0},
		{"   ", 0},
		{"unknown-future-slug", 0},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("slug_%s", tt.slug), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("MapPlaylistSlugToID panicked for slug %q: %v", tt.slug, r)
				}
			}()

			got := MapPlaylistSlugToID(tt.slug)
			if got != tt.want {
				t.Errorf("MapPlaylistSlugToID(%q) = %d; want %d", tt.slug, got, tt.want)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Suite 12: Bidirectional Conversion Stability (Round-Trip Idempotency)
// ----------------------------------------------------------------------------

func TestStress_Bidirectional_RoundTripStability(t *testing.T) {
	t.Run("nested detail payload round trip idempotency", func(t *testing.T) {
		var d1 ReplayDetail
		if err := json.Unmarshal([]byte(canonicalNestedFixture), &d1); err != nil {
			t.Fatalf("first unmarshal failed: %v", err)
		}

		// Marshal d1 back to JSON
		m1, err := json.Marshal(d1)
		if err != nil {
			t.Fatalf("first marshal failed: %v", err)
		}

		// Unmarshal into d2
		var d2 ReplayDetail
		if err := json.Unmarshal(m1, &d2); err != nil {
			t.Fatalf("second unmarshal failed: %v", err)
		}

		// Marshal d2 back to JSON
		m2, err := json.Marshal(d2)
		if err != nil {
			t.Fatalf("second marshal failed: %v", err)
		}

		// Assert m1 and m2 are identical (idempotent round trip)
		if !bytes.Equal(m1, m2) {
			t.Fatalf("bidirectional round trip serialization is NOT idempotent!\nFirst:\n%s\nSecond:\n%s", string(m1), string(m2))
		}

		// Assert key fields preserved in d2
		if d2.ID != d1.ID || d2.Title != d1.Title || d2.Duration != d1.Duration || d2.OvertimeSeconds != d1.OvertimeSeconds {
			t.Errorf("metadata altered after round-trip: d1=%+v, d2=%+v", d1, d2)
		}

		p1 := d1.Blue.Players[0]
		p2 := d2.Blue.Players[0]
		if p2.Score != p1.Score || p2.Goals != p1.Goals || p2.Assists != p1.Assists {
			t.Errorf("player stats altered after round-trip: p1.Score=%d, p2.Score=%d", p1.Score, p2.Score)
		}
		if p2.Core == nil || p2.Core.AerialHits != 14 {
			t.Errorf("player Core category pointer lost or altered after round trip: %+v", p2.Core)
		}
	})

	t.Run("flat list payload round trip and Core synthesis preservation", func(t *testing.T) {
		flatJSON := `{
			"id": "flat-roundtrip-1",
			"replay_title": "Game Flat",
			"blue": {
				"players": [
					{
						"name": "FlatStar",
						"score": 620,
						"goals": 2,
						"assists": 1,
						"saves": 3,
						"shots": 4,
						"demos": { "inflicted": 1, "taken": 0 }
					}
				]
			}
		}`

		var r1 ReplayDetail
		if err := json.Unmarshal([]byte(flatJSON), &r1); err != nil {
			t.Fatalf("unmarshal flat JSON failed: %v", err)
		}

		if r1.Title != "Game Flat" {
			t.Errorf("r1.Title = %q; want Game Flat", r1.Title)
		}
		p1 := r1.Blue.Players[0]
		if p1.Core == nil || p1.Core.Score != 620 {
			t.Fatalf("synthesized Core missing on p1: %+v", p1.Core)
		}

		// Round trip
		marshaled, err := json.Marshal(r1)
		if err != nil {
			t.Fatalf("marshal r1 failed: %v", err)
		}

		var r2 ReplayDetail
		if err := json.Unmarshal(marshaled, &r2); err != nil {
			t.Fatalf("unmarshal marshaled flat failed: %v", err)
		}

		p2 := r2.Blue.Players[0]
		if p2.Score != 620 || p2.Goals != 2 || p2.Saves != 3 {
			t.Errorf("p2 box score corrupted: %+v", p2)
		}
		if p2.Core == nil || p2.Core.Score != 620 {
			t.Errorf("p2 Core synthesized pointer lost after round trip: %+v", p2.Core)
		}
	})
}

// ----------------------------------------------------------------------------
// Suite 13: Zero-Field Clobbering Verification
// ----------------------------------------------------------------------------

func TestStress_ZeroFieldClobbering(t *testing.T) {
	t.Run("legitimate 0-score player with non-zero nested core stats", func(t *testing.T) {
		payload := `{
			"name": "HydratedPlayer",
			"score": 0,
			"goals": 0,
			"assists": 0,
			"saves": 0,
			"shots": 0,
			"stats": {
				"core": {
					"score": 450,
					"goals": 1,
					"assists": 2,
					"saves": 3,
					"shots": 4,
					"mvp": true
				}
			}
		}`

		var p ReplayPlayer
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}

		// Flat fields must be hydrated from Stats.Core when flat fields are 0
		if p.Score != 450 || p.Goals != 1 || p.Assists != 2 || p.Saves != 3 || p.Shots != 4 || !p.MVP {
			t.Errorf("flat fields were not hydrated from nested Stats.Core: score=%d, goals=%d, assists=%d, saves=%d, shots=%d, mvp=%v",
				p.Score, p.Goals, p.Assists, p.Saves, p.Shots, p.MVP)
		}
	})

	t.Run("legitimate 0-score player with 0 nested core stats", func(t *testing.T) {
		payload := `{
			"name": "AFKPlayer",
			"score": 0,
			"goals": 0,
			"assists": 0,
			"saves": 0,
			"shots": 0,
			"stats": {
				"core": {
					"score": 0,
					"goals": 0,
					"assists": 0,
					"saves": 0,
					"shots": 0,
					"mvp": false
				}
			}
		}`

		var p ReplayPlayer
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}

		// Must remain 0 without panicking, and Core pointer must be populated
		if p.Score != 0 || p.Goals != 0 || p.Assists != 0 || p.Saves != 0 || p.Shots != 0 {
			t.Errorf("zero stats clobbered into non-zero: %+v", p)
		}
		if p.Core == nil {
			t.Fatal("expected non-nil p.Core pointer pointing to Stats.Core")
		}
		if p.Core.Score != 0 {
			t.Errorf("p.Core.Score = %d; want 0", p.Core.Score)
		}
	})

	t.Run("partial zero and non-zero fields preserved accurately", func(t *testing.T) {
		payload := `{
			"name": "DefenderOnly",
			"score": 150,
			"goals": 0,
			"assists": 0,
			"saves": 3,
			"shots": 0,
			"demos": { "inflicted": 0, "taken": 2 }
		}`

		var p ReplayPlayer
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}

		if p.Score != 150 || p.Goals != 0 || p.Assists != 0 || p.Saves != 3 || p.Shots != 0 {
			t.Errorf("partial stats mismatch: score=%d, goals=%d, assists=%d, saves=%d, shots=%d",
				p.Score, p.Goals, p.Assists, p.Saves, p.Shots)
		}
		if p.Core == nil || p.Core.Score != 150 || p.Core.Saves != 3 || p.Core.Goals != 0 {
			t.Errorf("synthesized Core mismatch: %+v", p.Core)
		}
		if p.Demo == nil || p.Demo.Inflicted != 0 || p.Demo.Taken != 2 {
			t.Errorf("synthesized Demo mismatch: %+v", p.Demo)
		}
	})
}

// ----------------------------------------------------------------------------
// Suite 14: Memory Leak & High-Allocation Stability
// ----------------------------------------------------------------------------

func TestStress_MemoryAndAllocation_NoLeaks(t *testing.T) {
	raw := []byte(canonicalNestedFixture)

	// Warm-up run
	for i := 0; i < 100; i++ {
		var d ReplayDetail
		_ = json.Unmarshal(raw, &d)
	}

	runtime.GC()
	var mStart runtime.MemStats
	runtime.ReadMemStats(&mStart)

	const iterations = 5000
	for i := 0; i < iterations; i++ {
		var d ReplayDetail
		if err := json.Unmarshal(raw, &d); err != nil {
			t.Fatalf("unmarshal error during iteration %d: %v", i, err)
		}
		if d.Blue.Players[0].Score != 650 {
			t.Fatalf("data corruption in iteration %d", i)
		}
	}

	runtime.GC()
	var mEnd runtime.MemStats
	runtime.ReadMemStats(&mEnd)

	// Verify no runaway retained heap growth (heap inuse should not explode)
	heapGrowth := int64(mEnd.HeapInuse) - int64(mStart.HeapInuse)
	// Allow up to 32MB tolerance for GC generational pools
	if heapGrowth > 32*1024*1024 {
		t.Fatalf("suspected memory leak: heap inuse grew by %d bytes across %d unmarshals", heapGrowth, iterations)
	}
}

// ----------------------------------------------------------------------------
// Suite 15: Concurrency & Race Detector Stress Harness
// ----------------------------------------------------------------------------

func TestStress_Concurrency_RaceDetection(t *testing.T) {
	rawNested := []byte(canonicalNestedFixture)
	flatPayload := []byte(`{
		"id": "concurrent-flat",
		"replay_title": "Concur Match",
		"blue": {
			"players": [
				{ "name": "ConcurP1", "score": 300, "goals": 1 }
			]
		}
	}`)

	const numGoroutines = 100
	const perGoroutineIterations = 50

	var wg sync.WaitGroup
	startBarrier := make(chan struct{})

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			<-startBarrier

			r := rand.New(rand.NewSource(int64(gid)))

			for i := 0; i < perGoroutineIterations; i++ {
				var d ReplayDetail

				if r.Intn(2) == 0 {
					if err := json.Unmarshal(rawNested, &d); err != nil {
						t.Errorf("goroutine %d unmarshal nested failed: %v", gid, err)
						return
					}
					if d.ID != "full-fixture-abc-123" || d.Blue.Players[0].Score != 650 {
						t.Errorf("goroutine %d data mismatch on nested payload", gid)
						return
					}
				} else {
					if err := json.Unmarshal(flatPayload, &d); err != nil {
						t.Errorf("goroutine %d unmarshal flat failed: %v", gid, err)
						return
					}
					if d.Title != "Concur Match" || d.Blue.Players[0].Score != 300 {
						t.Errorf("goroutine %d data mismatch on flat payload", gid)
						return
					}
				}

				// Also test slug mapping concurrently
				slug := fmt.Sprintf("%d", r.Intn(50))
				_ = MapPlaylistSlugToID(slug)
			}
		}(g)
	}

	// Release all goroutines simultaneously
	close(startBarrier)
	wg.Wait()
}
