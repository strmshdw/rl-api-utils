package ballchasing

import (
	"encoding/json"
	"testing"
)

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
