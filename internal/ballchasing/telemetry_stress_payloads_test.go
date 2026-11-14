package ballchasing

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
)

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
			if err := json.Unmarshal([]byte(tc.payload), &detail); err == nil && tc.payload != "null" {
				t.Fatalf("expected error for corrupted payload %q, got nil", tc.name)
			}

			var player ReplayPlayer
			if err := json.Unmarshal([]byte(tc.payload), &player); err == nil && tc.payload != "null" {
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

func TestStress_PayloadGenerators_Truncated(t *testing.T) {
	raw := []byte(canonicalNestedFixture)
	totalLen := len(raw)

	for cut := 1; cut < totalLen; cut += 25 {
		truncated := raw[:cut]
		t.Run(fmt.Sprintf("cut_at_%d_of_%d", cut, totalLen), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic during unmarshal of truncated payload at %d: %v", cut, r)
				}
			}()

			var detail ReplayDetail
			if err := json.Unmarshal(truncated, &detail); err == nil {
				t.Fatalf("expected error for truncated payload at byte %d, got nil", cut)
			}
		})
	}
}

func TestStress_PayloadGenerators_NullFields(t *testing.T) {
	t.Run("all nullable top-level pointers null", func(t *testing.T) {
		payload := `{
			"id": "null-fields-1",
			"server": null,
			"uploader": null,
			"min_rank": null,
			"max_rank": null,
			"groups": null,
			"blue": { "color": "blue", "name": "Team Null", "goals": 0, "players": null, "stats": null },
			"orange": { "color": "orange", "name": "Team Null 2", "goals": 0, "players": null, "stats": null }
		}`
		var detail ReplayDetail
		if err := json.Unmarshal([]byte(payload), &detail); err != nil {
			t.Fatalf("unmarshal with null pointers failed: %v", err)
		}
		if detail.Server != nil || detail.Uploader != nil || detail.MinRank != nil || detail.MaxRank != nil {
			t.Fatalf("expected nil pointers")
		}
		if detail.Blue.Stats != nil || detail.Orange.Stats != nil {
			t.Fatalf("expected nil team stats")
		}
	})

	t.Run("player telemetry categories all null", func(t *testing.T) {
		payload := `{
			"name": "GhostPlayer",
			"id": { "platform": "steam", "id": "76561198000000099" },
			"camera": null,
			"rank": null,
			"stats": { "core": null, "boost": null, "movement": null, "positioning": null, "demo": null, "ball": null },
			"core": null, "boost": null, "movement": null, "positioning": null, "demo": null, "ball": null
		}`
		var player ReplayPlayer
		if err := json.Unmarshal([]byte(payload), &player); err != nil {
			t.Fatalf("unmarshal player with null stats failed: %v", err)
		}
		if player.Camera != nil || player.Rank != nil || player.Core != nil || player.Boost != nil || player.Movement != nil || player.Positioning != nil || player.Demo != nil || player.Ball != nil {
			t.Fatalf("expected category pointers to be nil")
		}
	})

	t.Run("empty object blocks", func(t *testing.T) {
		payload := `{
			"id": "empty-blocks",
			"server": {}, "uploader": {}, "min_rank": {}, "max_rank": {},
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
			t.Fatalf("expected non-nil allocated blocks for empty objects")
		}
	})
}

func TestStress_PayloadGenerators_MixedCase(t *testing.T) {
	mixedCaseJSON := `{
		"ID": "mixed-case-001", "STATUS": "ok", "MATCH_GUID": "guid-mixed-case",
		"ROCKET_LEAGUE_ID": "rl-mixed-case", "REPLAY_TITLE": "Mixed Case Cup",
		"OVERTIME": true, "OVERTIME_SECONDS": 125,
		"BLUE": {
			"COLOR": "blue", "GOALS": 5,
			"PLAYERS": [{ "NAME": "Kaydop", "SCORE": 780, "GOALS": 3, "ASSISTS": 2, "SAVES": 1, "SHOTS": 6 }]
		}
	}`

	var detail ReplayDetail
	if err := json.Unmarshal([]byte(mixedCaseJSON), &detail); err != nil {
		t.Fatalf("unmarshal mixed-case JSON failed: %v", err)
	}
	if detail.ID != "mixed-case-001" || detail.Status != "ok" || detail.Title != "Mixed Case Cup" || detail.MatchGUID != "guid-mixed-case" || detail.OvertimeSeconds != 125 {
		t.Fatalf("metadata mismatch: %+v", detail)
	}
	if len(detail.Blue.Players) != 1 {
		t.Fatalf("Blue.Players count = %d; want 1", len(detail.Blue.Players))
	}
	p := detail.Blue.Players[0]
	if p.Name != "Kaydop" || p.Score != 780 || p.Goals != 3 || p.Assists != 2 || p.Saves != 1 || p.Shots != 6 {
		t.Fatalf("player stats mismatch: %+v", p)
	}
}

func TestStress_PayloadGenerators_BoundaryValues(t *testing.T) {
	t.Run("extreme numeric ranges in replay metadata", func(t *testing.T) {
		payload := fmt.Sprintf(`{"id":"boundary-nums","duration":%d,"play_duration":%d,"overtime":true,"overtime_seconds":86400,"season":99999,"team_size":100}`, math.MaxInt32, math.MaxInt32)
		var item ReplayItem
		if err := json.Unmarshal([]byte(payload), &item); err != nil {
			t.Fatalf("unmarshal extreme numbers failed: %v", err)
		}
		if item.Duration != math.MaxInt32 || item.OvertimeSeconds != 86400 {
			t.Errorf("unexpected numeric fields: %+v", item)
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
		if p.SteeringSensitivity != 9999.99 || p.Stats.Ball.PossessionTime != 1e9 || p.Stats.Boost.PercentZeroBoost != 100.0 {
			t.Errorf("unexpected float values: %+v", p)
		}
	})

	t.Run("large strings without buffer overflow or corruption", func(t *testing.T) {
		giantTitle := strings.Repeat("EpicGameHeader", 1000)
		giantMap := strings.Repeat("Stadium_Field_Custom_", 500)
		payload := fmt.Sprintf(`{"id": "large-str", "title": %q, "map_name": %q}`, giantTitle, giantMap)
		var item ReplayItem
		if err := json.Unmarshal([]byte(payload), &item); err != nil {
			t.Fatalf("unmarshal giant string fields failed: %v", err)
		}
		if item.Title != giantTitle || item.MapName != giantMap {
			t.Errorf("giant string length mismatch")
		}
	})
}
