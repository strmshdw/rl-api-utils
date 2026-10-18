package ballchasing

import (
	"encoding/json"
	"testing"
)

func TestReplayDetail_Unmarshal_FullNested(t *testing.T) {
	fixtureJSON := `{
		"id": "abc-12345-xyz",
		"link": "https://ballchasing.com/replay/abc-12345-xyz",
		"created": "2026-10-15T20:00:00Z",
		"date": "2026-10-15T19:50:00Z",
		"status": "ok",
		"rocket_league_id": "rl-guid-9999",
		"match_guid": "rl-guid-9999",
		"title": "Grand Finals Game 7",
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

	var detail ReplayDetail
	if err := json.Unmarshal([]byte(fixtureJSON), &detail); err != nil {
		t.Fatalf("failed to unmarshal ReplayDetail fixture: %v", err)
	}

	// 1. Verify Top-Level Match Metadata
	if detail.ID != "abc-12345-xyz" {
		t.Errorf("ID = %q; want abc-12345-xyz", detail.ID)
	}
	if detail.Status != "ok" {
		t.Errorf("Status = %q; want ok", detail.Status)
	}
	if !detail.Overtime {
		t.Errorf("Overtime = false; want true")
	}
	if detail.OvertimeSeconds != 60 {
		t.Errorf("OvertimeSeconds = %d; want 60", detail.OvertimeSeconds)
	}
	if detail.Uploader == nil || detail.Uploader.SteamID != "76561198000000001" {
		t.Fatalf("Uploader missing or incorrect: %+v", detail.Uploader)
	}
	if detail.MinRank == nil || detail.MinRank.Tier != 19 {
		t.Errorf("MinRank tier = %v; want 19", detail.MinRank)
	}
	if detail.MaxRank == nil || detail.MaxRank.Tier != 22 {
		t.Errorf("MaxRank tier = %v; want 22", detail.MaxRank)
	}

	// 2. Verify Team Stats & Aggregate Telemetry
	if detail.Blue.Stats == nil || detail.Blue.Stats.Ball == nil {
		t.Fatalf("Blue team stats or ball stats missing")
	}
	if detail.Blue.Stats.Ball.PossessionTime != 171.09 {
		t.Errorf("Ball possession time = %f; want 171.09", detail.Blue.Stats.Ball.PossessionTime)
	}
	if detail.Blue.Stats.Core.Goals != 3 {
		t.Errorf("Blue team goals in core = %d; want 3", detail.Blue.Stats.Core.Goals)
	}
	if detail.Blue.Stats.Boost.AmountCollected != 7703 {
		t.Errorf("Blue team boost collected = %d; want 7703", detail.Blue.Stats.Boost.AmountCollected)
	}

	// 3. Verify Player Telemetry & Mechanics
	if len(detail.Blue.Players) != 1 {
		t.Fatalf("Blue team players len = %d; want 1", len(detail.Blue.Players))
	}
	mist := detail.Blue.Players[0]
	if mist.Name != "mist" {
		t.Errorf("Player Name = %q; want mist", mist.Name)
	}
	if mist.CarName != "Octane" || mist.CarID != 23 {
		t.Errorf("Player Car = %s (%d); want Octane (23)", mist.CarName, mist.CarID)
	}
	if mist.Camera == nil || mist.Camera.FOV != 110 || mist.Camera.Height != 100 {
		t.Errorf("Player Camera incorrect: %+v", mist.Camera)
	}
	if mist.SteeringSensitivity != 1.3 {
		t.Errorf("SteeringSensitivity = %f; want 1.3", mist.SteeringSensitivity)
	}
	if mist.Rank == nil || mist.Rank.Tier != 20 || mist.Rank.MMR != 1540.5 {
		t.Errorf("Player Rank incorrect: %+v", mist.Rank)
	}

	// 4. Verify Dual Compatibility: Flat Fields Hydrated from Nested Stats
	if mist.Score != 650 {
		t.Errorf("mist.Score = %d; want 650 (hydrated from Stats.Core)", mist.Score)
	}
	if mist.Goals != 2 {
		t.Errorf("mist.Goals = %d; want 2", mist.Goals)
	}
	if mist.Saves != 4 {
		t.Errorf("mist.Saves = %d; want 4", mist.Saves)
	}
	if mist.Assists != 1 {
		t.Errorf("mist.Assists = %d; want 1", mist.Assists)
	}
	if mist.Shots != 4 {
		t.Errorf("mist.Shots = %d; want 4", mist.Shots)
	}
	if mist.Demos.Inflicted != 2 || mist.Demos.Taken != 1 {
		t.Errorf("mist.Demos = %+v; want Inflicted:2, Taken:1", mist.Demos)
	}
	if !mist.MVP {
		t.Errorf("mist.MVP = false; want true")
	}

	// 5. Verify Direct Category Pointers
	if mist.Core == nil || mist.Core.AerialHits != 14 || mist.Core.FlipResets != 1 {
		t.Errorf("mist.Core missing or incorrect: %+v", mist.Core)
	}
	if mist.Boost == nil || mist.Boost.BPM != 403.5 || mist.Boost.CountCollectedBig != 18 {
		t.Errorf("mist.Boost missing or incorrect: %+v", mist.Boost)
	}
	if mist.Movement == nil || mist.Movement.AvgSpeed != 1540.0 || mist.Movement.CountPowerslide != 28 {
		t.Errorf("mist.Movement missing or incorrect: %+v", mist.Movement)
	}
	if mist.Positioning == nil || mist.Positioning.TimeDefensiveThird != 193.27 {
		t.Errorf("mist.Positioning missing or incorrect: %+v", mist.Positioning)
	}
	if mist.Demo == nil || mist.Demo.InflictedInOffensiveHalf != 2 {
		t.Errorf("mist.Demo missing or incorrect: %+v", mist.Demo)
	}
}

func TestReplayList_Unmarshal_FlatCompatibility(t *testing.T) {
	listJSON := `{
		"count": 1,
		"list": [
			{
				"id": "flat-list-123",
				"link": "https://ballchasing.com/replay/flat-list-123",
				"created": "2026-10-15T21:00:00Z",
				"date": "2026-10-15T20:55:00Z",
				"replay_title": "NV vs SSG - Game 4",
				"playlist_id": "ranked-doubles",
				"blue": {
					"color": "blue",
					"name": "Envy",
					"goals": 4,
					"players": [
						{
							"start_time": 0,
							"end_time": 300,
							"name": "Atomic",
							"id": { "platform": "steam", "id": "76561198000000002" },
							"score": 526,
							"goals": 2,
							"assists": 1,
							"saves": 3,
							"shots": 5,
							"demos": { "inflicted": 1, "taken": 0 }
						}
					]
				},
				"orange": {
					"color": "orange",
					"name": "Spacestation",
					"goals": 1,
					"players": []
				}
			}
		]
	}`

	var resp ReplayListResponse
	if err := json.Unmarshal([]byte(listJSON), &resp); err != nil {
		t.Fatalf("failed to unmarshal ReplayListResponse: %v", err)
	}

	if resp.Count != 1 || len(resp.List) != 1 {
		t.Fatalf("resp.Count = %d, len(resp.List) = %d; want 1", resp.Count, len(resp.List))
	}

	item := resp.List[0]
	// Verify Title synchronized from replay_title
	if item.Title != "NV vs SSG - Game 4" {
		t.Errorf("item.Title = %q; want NV vs SSG - Game 4", item.Title)
	}
	if item.ReplayTitle != "NV vs SSG - Game 4" {
		t.Errorf("item.ReplayTitle = %q; want NV vs SSG - Game 4", item.ReplayTitle)
	}

	if len(item.Blue.Players) != 1 {
		t.Fatalf("len(item.Blue.Players) = %d; want 1", len(item.Blue.Players))
	}
	player := item.Blue.Players[0]
	if player.Name != "Atomic" {
		t.Errorf("player.Name = %q; want Atomic", player.Name)
	}
	if player.Score != 526 {
		t.Errorf("player.Score = %d; want 526", player.Score)
	}

	// Verify synthesized Core and Stats blocks from flat fields
	if player.Core == nil {
		t.Fatalf("player.Core is nil; expected synthesized CoreStats from flat fields")
	}
	if player.Core.Score != 526 || player.Core.Goals != 2 || player.Core.Saves != 3 {
		t.Errorf("player.Core stats mismatch: %+v", player.Core)
	}
	if player.Stats == nil || player.Stats.Core == nil {
		t.Fatalf("player.Stats or player.Stats.Core is nil")
	}
	if player.Stats.Core.Score != 526 {
		t.Errorf("player.Stats.Core.Score = %d; want 526", player.Stats.Core.Score)
	}
	if player.Demo == nil || player.Demo.Inflicted != 1 {
		t.Errorf("player.Demo mismatch: %+v", player.Demo)
	}
}

func TestReplayDetail_StatusPendingAndFailed(t *testing.T) {
	pendingJSON := `{
		"id": "pending-123",
		"status": "pending",
		"link": "https://ballchasing.com/replay/pending-123"
	}`
	var pending ReplayDetail
	if err := json.Unmarshal([]byte(pendingJSON), &pending); err != nil {
		t.Fatalf("unmarshal pending replay failed: %v", err)
	}
	if pending.Status != "pending" {
		t.Errorf("pending.Status = %q; want pending", pending.Status)
	}

	failedJSON := `{
		"id": "failed-456",
		"status": "failed",
		"link": "https://ballchasing.com/replay/failed-456"
	}`
	var failed ReplayDetail
	if err := json.Unmarshal([]byte(failedJSON), &failed); err != nil {
		t.Fatalf("unmarshal failed replay failed: %v", err)
	}
	if failed.Status != "failed" {
		t.Errorf("failed.Status = %q; want failed", failed.Status)
	}
}

func TestMapPlaylistSlugToID_NumericFallback(t *testing.T) {
	tests := []struct {
		slug string
		want int
	}{
		// Standard competitive slugs
		{"ranked-duels", 10},
		{"ranked-duel", 10},
		{"1v1", 10},
		{"10", 10},
		{"ranked-doubles", 11},
		{"ranked-double", 11},
		{"2v2", 11},
		{"11", 11},
		{"ranked-solo-standard", 12},
		{"solo-standard", 12},
		{"12", 12},
		{"ranked-standard", 13},
		{"3v3", 13},
		{"13", 13},
		{"ranked-hoops", 27},
		{"hoops", 27},
		{"27", 27},
		{"ranked-rumble", 28},
		{"rumble", 28},
		{"28", 28},
		{"ranked-dropshot", 29},
		{"dropshot", 29},
		{"29", 29},
		{"ranked-snowday", 30},
		{"snowday", 30},
		{"30", 30},
		{"tournament", 34},
		{"tournaments", 34},
		{"34", 34},

		// Casual / unranked slugs
		{"casual-standard", 3},
		{"unranked-standard", 3},
		{"3", 3},
		{"casual-doubles", 2},
		{"unranked-doubles", 2},
		{"2", 2},
		{"casual-duels", 1},
		{"casual-duel", 1},
		{"unranked-duels", 1},
		{"unranked-duel", 1},
		{"1", 1},
		{"casual-chaos", 4},
		{"unranked-chaos", 4},
		{"4", 4},

		// Numeric string fallbacks
		{"22", 22},
		{"6", 6},
		{"99", 99},
		{" 11 ", 11},

		// Non-positive, invalid, or garbage inputs
		{"0", 0},
		{"-5", 0},
		{"unknown-playlist-mode", 0},
		{"", 0},
		{"   ", 0},
	}

	for _, tt := range tests {
		got := MapPlaylistSlugToID(tt.slug)
		if got != tt.want {
			t.Errorf("MapPlaylistSlugToID(%q) = %d; want %d", tt.slug, got, tt.want)
		}
	}
}

func TestReplayDetail_Unmarshal_OmittedTopLevelTeamGoals(t *testing.T) {
	// Ballchasing GET /api/replays/{id} omits top-level blue.goals and orange.goals,
	// providing them only inside stats.core.goals and players[*].stats.core.goals.
	raw := `{
		"id": "bc-detail-no-top-goals",
		"rocket_league_id": "guid-no-top-goals",
		"playlist_id": "ranked-doubles",
		"blue": {
			"color": "blue",
			"stats": {
				"core": { "shots": 8, "goals": 4, "goals_against": 2, "saves": 5, "assists": 2, "score": 920 }
			},
			"players": [
				{
					"name": "BlueP1",
					"id": { "platform": "steam", "id": "76561198000000001" },
					"stats": { "core": { "goals": 3, "assists": 1, "saves": 3, "shots": 5, "score": 580 } }
				},
				{
					"name": "BlueP2",
					"id": { "platform": "epic", "id": "epic_b2" },
					"stats": { "core": { "goals": 1, "assists": 1, "saves": 2, "shots": 3, "score": 340 } }
				}
			]
		},
		"orange": {
			"color": "orange",
			"players": [
				{
					"name": "OrangeP1",
					"id": { "platform": "steam", "id": "76561198000000002" },
					"stats": { "core": { "goals": 2, "assists": 0, "saves": 2, "shots": 4, "score": 410 } }
				}
			]
		}
	}`

	var detail ReplayItem
	if err := json.Unmarshal([]byte(raw), &detail); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if detail.Blue.Goals != 4 {
		t.Errorf("detail.Blue.Goals = %d; want 4", detail.Blue.Goals)
	}
	if detail.Orange.Goals != 2 {
		t.Errorf("detail.Orange.Goals = %d; want 2", detail.Orange.Goals)
	}
}

