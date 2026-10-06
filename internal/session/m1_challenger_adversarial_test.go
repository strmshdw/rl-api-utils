package session_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/session"
)

// TestChallenger_SessionHistory_MultiMatchSnapshotPreservation verifies that
// across multiple concluded matches with varying leavers, winners, and scores,
// SessionTracker maintains an immutable, isolated match history snapshot for every match.
func TestChallenger_SessionHistory_MultiMatchSnapshotPreservation(t *testing.T) {
	s := session.NewSessionTracker()

	type matchScenario struct {
		guid        string
		playlistID  int
		localTeam   int
		winnerTeam  *int
		expectedRes string
		blueGoals   int
		orangeGoals int
		players     []playertrack.LobbyPlayer
	}

	win0 := 0
	win1 := 1

	scenarios := []matchScenario{
		{
			// Match 1: 3v2 with 2 blue leavers, blue wins 4-1
			guid:        "multi-snap-m1",
			playlistID:  11,
			localTeam:   0,
			winnerTeam:  &win0,
			expectedRes: "victory",
			blueGoals:   4,
			orangeGoals: 1,
			players: []playertrack.LobbyPlayer{
				{
					PlayerID: "Steam|local|0", TeamNum: 0, IsLocal: true,
					Stats: playertrack.PlayerStatsSummary{Score: 150, Goals: 1},
				},
				{
					PlayerID: "Steam|tm1_leaver|0", TeamNum: 0, IsDisconnected: true,
					Stats: playertrack.PlayerStatsSummary{Score: 250, Goals: 2},
				},
				{
					PlayerID: "Steam|tm2_leaver|0", TeamNum: 0, IsDisconnected: true,
					Stats: playertrack.PlayerStatsSummary{Score: 120, Goals: 1},
				},
				{
					PlayerID: "Steam|opp1|0", TeamNum: 1,
					Stats: playertrack.PlayerStatsSummary{Score: 100, Goals: 1},
				},
			},
		},
		{
			// Match 2: Orange wins 3-0, orange leaver scored 2 goals
			guid:        "multi-snap-m2",
			playlistID:  11,
			localTeam:   0,
			winnerTeam:  &win1,
			expectedRes: "defeat",
			blueGoals:   0,
			orangeGoals: 3,
			players: []playertrack.LobbyPlayer{
				{
					PlayerID: "Steam|local|0", TeamNum: 0, IsLocal: true,
					Stats: playertrack.PlayerStatsSummary{Score: 80, Goals: 0},
				},
				{
					PlayerID: "Steam|opp2_leaver|0", TeamNum: 1, IsDisconnected: true,
					Stats: playertrack.PlayerStatsSummary{Score: 220, Goals: 2},
				},
				{
					PlayerID: "Steam|opp3|0", TeamNum: 1,
					Stats: playertrack.PlayerStatsSummary{Score: 140, Goals: 1},
				},
			},
		},
		{
			// Match 3: 0-0 Draw, no winner
			guid:        "multi-snap-m3",
			playlistID:  13,
			localTeam:   0,
			winnerTeam:  nil,
			expectedRes: "draw",
			blueGoals:   0,
			orangeGoals: 0,
			players: []playertrack.LobbyPlayer{
				{
					PlayerID: "Steam|local|0", TeamNum: 0, IsLocal: true,
					Stats: playertrack.PlayerStatsSummary{Score: 50, Goals: 0},
				},
				{
					PlayerID: "Steam|tm3_leaver|0", TeamNum: 0, IsDisconnected: true,
					Stats: playertrack.PlayerStatsSummary{Score: 60, Goals: 0},
				},
			},
		},
		{
			// Match 4: Local is on Orange team (Team 1) and wins 2-1
			guid:        "multi-snap-m4",
			playlistID:  13,
			localTeam:   1,
			winnerTeam:  &win1,
			expectedRes: "victory",
			blueGoals:   1,
			orangeGoals: 2,
			players: []playertrack.LobbyPlayer{
				{
					PlayerID: "Steam|local|0", TeamNum: 1, IsLocal: true,
					Stats: playertrack.PlayerStatsSummary{Score: 200, Goals: 2},
				},
				{
					PlayerID: "Steam|rival_leaver|0", TeamNum: 0, IsDisconnected: true,
					Stats: playertrack.PlayerStatsSummary{Score: 110, Goals: 1},
				},
			},
		},
		{
			// Match 5: Local player disconnected early! Teammate carries to victory (3-2)
			guid:        "multi-snap-m5",
			playlistID:  11,
			localTeam:   0,
			winnerTeam:  &win0,
			expectedRes: "victory",
			blueGoals:   3,
			orangeGoals: 2,
			players: []playertrack.LobbyPlayer{
				{
					PlayerID: "Steam|local|0", TeamNum: 0, IsLocal: true, IsDisconnected: true,
					Stats: playertrack.PlayerStatsSummary{Score: 100, Goals: 1},
				},
				{
					PlayerID: "Steam|carrier_tm|0", TeamNum: 0,
					Stats: playertrack.PlayerStatsSummary{Score: 300, Goals: 2},
				},
				{
					PlayerID: "Steam|opp4|0", TeamNum: 1,
					Stats: playertrack.PlayerStatsSummary{Score: 180, Goals: 2},
				},
			},
		},
	}

	for _, sc := range scenarios {
		var localP *playertrack.LobbyPlayer
		var tms, opps []playertrack.LobbyPlayer

		for _, p := range sc.players {
			if p.IsLocal {
				pCopy := p
				localP = &pCopy
			} else if p.TeamNum == sc.localTeam {
				tms = append(tms, p)
			} else {
				opps = append(opps, p)
			}
		}

		lt := sc.localTeam
		matchResp := &playertrack.CurrentMatchResponse{
			ActiveMatch: false,
			MatchEnded:  true,
			MatchGUID:   sc.guid,
			PlaylistID:  sc.playlistID,
			LocalTeam:   &lt,
			WinnerTeam:  sc.winnerTeam,
			Result:      sc.expectedRes,
			LocalPlayer: localP,
			Teammates:   tms,
			Opponents:   opps,
		}

		s.ConcludeMatch(matchResp)
	}

	summary := s.GetSessionSummary()
	if summary.TotalMatches != len(scenarios) {
		t.Fatalf("expected %d total matches, got %d", len(scenarios), summary.TotalMatches)
	}
	// Wins: M1 (victory), M4 (victory), M5 (victory) = 3 wins. M2 = defeat (1 loss). M3 = draw.
	if summary.TotalWins != 3 {
		t.Errorf("expected 3 total wins, got %d", summary.TotalWins)
	}
	if summary.TotalLosses != 1 {
		t.Errorf("expected 1 total loss, got %d", summary.TotalLosses)
	}

	// Verify each match in history is individually intact
	for i, sc := range scenarios {
		detail := summary.Matches[i]
		if detail.MatchGUID != sc.guid {
			t.Errorf("match %d: expected GUID %q, got %q", i, sc.guid, detail.MatchGUID)
		}
		if detail.BlueScore != sc.blueGoals {
			t.Errorf("match %d (%s): expected BlueScore=%d, got %d",
				i, sc.guid, sc.blueGoals, detail.BlueScore)
		}
		if detail.OrangeScore != sc.orangeGoals {
			t.Errorf("match %d (%s): expected OrangeScore=%d, got %d",
				i, sc.guid, sc.orangeGoals, detail.OrangeScore)
		}
		if detail.Result != sc.expectedRes {
			t.Errorf("match %d (%s): expected Result=%s, got %s",
				i, sc.guid, sc.expectedRes, detail.Result)
		}

		// Verify every player in scenario is represented with correct flags
		for _, expectedP := range sc.players {
			var found *session.SessionMatchPlayer
			for j := range detail.Players {
				if detail.Players[j].PlayerID == expectedP.PlayerID {
					found = &detail.Players[j]
					break
				}
			}
			if found == nil {
				t.Fatalf("match %d (%s): player %s not found in match detail", i, sc.guid, expectedP.PlayerID)
			}
			if found.IsDisconnected != expectedP.IsDisconnected {
				t.Errorf("match %d (%s): player %s expected IsDisconnected=%v, got %v",
					i, sc.guid, expectedP.PlayerID, expectedP.IsDisconnected, found.IsDisconnected)
			}
			if found.Stats.Goals != expectedP.Stats.Goals {
				t.Errorf("match %d (%s): player %s expected Goals=%d, got %d",
					i, sc.guid, expectedP.PlayerID, expectedP.Stats.Goals, found.Stats.Goals)
			}
			// Verify Won flag
			if sc.winnerTeam != nil {
				expectedWon := (expectedP.TeamNum == *sc.winnerTeam)
				if found.Won == nil {
					t.Errorf("match %d (%s): player %s expected Won=%v, got nil",
						i, sc.guid, expectedP.PlayerID, expectedWon)
				} else if *found.Won != expectedWon {
					t.Errorf("match %d (%s): player %s expected Won=%v, got %v",
						i, sc.guid, expectedP.PlayerID, expectedWon, *found.Won)
				}
			} else {
				if found.Won != nil {
					t.Errorf("match %d (%s): player %s expected Won=nil for draw, got %v",
						i, sc.guid, expectedP.PlayerID, *found.Won)
				}
			}
		}
	}

	// Deep clone mutation attack: mutate the returned summary and re-fetch to assert immutability
	for _, m := range summary.Matches {
		m.BlueScore = 999
		m.OrangeScore = 999
		for j := range m.Players {
			m.Players[j].IsDisconnected = false
			f := false
			m.Players[j].Won = &f
		}
	}

	summary2 := s.GetSessionSummary()
	for i, sc := range scenarios {
		d := summary2.Matches[i]
		if d.BlueScore == 999 || d.OrangeScore == 999 {
			t.Fatalf("SECURITY VIOLATION: mutating GetSessionSummary() corrupted internal match state for match %d", i)
		}
		if d.BlueScore != sc.blueGoals || d.OrangeScore != sc.orangeGoals {
			t.Fatalf("match %d scores were corrupted by external mutation", i)
		}
	}
}

// TestChallenger_GoalSummation_ExtremeAndEdgeCases verifies goal summation in ConcludeMatch
// under edge cases: multiple leavers, spectators with goals, and draw games.
func TestChallenger_GoalSummation_ExtremeAndEdgeCases(t *testing.T) {
	s := session.NewSessionTracker()

	win0 := 0
	localTeam := 0
	match := &playertrack.CurrentMatchResponse{
		MatchGUID:   "edge-goals-1",
		PlaylistID:  11,
		LocalTeam:   &localTeam,
		WinnerTeam:  &win0,
		Result:      "victory",
		LocalPlayer: &playertrack.LobbyPlayer{
			PlayerID: "Steam|local|0", TeamNum: 0, IsLocal: true,
			Stats: playertrack.PlayerStatsSummary{Goals: 2},
		},
		Teammates: []playertrack.LobbyPlayer{
			{
				PlayerID: "Steam|leaver1|0", TeamNum: 0, IsDisconnected: true,
				Stats: playertrack.PlayerStatsSummary{Goals: 3},
			},
			{
				PlayerID: "Steam|leaver2|0", TeamNum: 0, IsDisconnected: true,
				Stats: playertrack.PlayerStatsSummary{Goals: 1},
			},
		},
		Opponents: []playertrack.LobbyPlayer{
			{
				PlayerID: "Steam|opp_leaver|0", TeamNum: 1, IsDisconnected: true,
				Stats: playertrack.PlayerStatsSummary{Goals: 4},
			},
		},
		Spectators: []playertrack.LobbyPlayer{
			{
				PlayerID: "Steam|caster|0", TeamNum: 255,
				Stats: playertrack.PlayerStatsSummary{Goals: 99}, // Caster should NOT affect score
			},
		},
	}

	s.ConcludeMatch(match)

	sum := s.GetSessionSummary()
	if len(sum.Matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(sum.Matches))
	}
	m := sum.Matches[0]

	// Blue goals: 2 (local) + 3 (leaver1) + 1 (leaver2) = 6
	if m.BlueScore != 6 {
		t.Errorf("expected BlueScore=6, got %d", m.BlueScore)
	}
	// Orange goals: 4 (opp_leaver) = 4
	if m.OrangeScore != 4 {
		t.Errorf("expected OrangeScore=4, got %d", m.OrangeScore)
	}
}

// TestChallenger_SSEBroadcast_PayloadAndJsonSerialization verifies that
// EventMatchUpdate pushed over SSE correctly includes `is_disconnected: true`
// when serialized to JSON, ensuring frontend contract compatibility.
func TestChallenger_SSEBroadcast_PayloadAndJsonSerialization(t *testing.T) {
	s := session.NewSessionTracker()
	ch, unsubscribe := s.Subscribe()
	defer unsubscribe()

	localTeam := 0
	match := &playertrack.CurrentMatchResponse{
		ActiveMatch:  true,
		MatchEnded:   false,
		MatchGUID:    "sse-json-guid",
		PlaylistID:   11,
		PlaylistName: "Ranked Doubles",
		LocalTeam:    &localTeam,
		LocalPlayer: &playertrack.LobbyPlayer{
			PlayerID: "Steam|local|0", Name: "LocalHero", TeamNum: 0, IsLocal: true,
			Stats: playertrack.PlayerStatsSummary{Score: 100, Goals: 1},
		},
		Teammates: []playertrack.LobbyPlayer{
			{
				PlayerID:       "Steam|leaver|0",
				Name:           "LeaverTm",
				TeamNum:        0,
				IsDisconnected: true,
				Stats: playertrack.PlayerStatsSummary{
					Score: 240,
					Goals: 2,
					Demos: 1,
				},
			},
		},
	}

	s.RecordActiveMatch(match)

	select {
	case evt := <-ch:
		if evt.Event != session.EventMatchUpdate {
			t.Fatalf("expected EventMatchUpdate, got %s", evt.Event)
		}

		// Serialize to JSON
		payloadJSON, err := json.Marshal(evt.Data)
		if err != nil {
			t.Fatalf("failed to marshal SSE event data: %v", err)
		}

		// Inspect JSON structure
		var parsed map[string]interface{}
		if err := json.Unmarshal(payloadJSON, &parsed); err != nil {
			t.Fatalf("failed to unmarshal SSE payload: %v", err)
		}

		teammates, ok := parsed["teammates"].([]interface{})
		if !ok || len(teammates) != 1 {
			t.Fatalf("expected 1 teammate in JSON payload, got %v", parsed["teammates"])
		}

		tmMap, ok := teammates[0].(map[string]interface{})
		if !ok {
			t.Fatalf("expected teammate map, got %T", teammates[0])
		}

		isDisc, ok := tmMap["is_disconnected"].(bool)
		if !ok || !isDisc {
			t.Errorf("expected 'is_disconnected: true' in JSON, got %v", tmMap["is_disconnected"])
		}

		statsMap, ok := tmMap["stats"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected stats map, got %T", tmMap["stats"])
		}
		if int(statsMap["score"].(float64)) != 240 || int(statsMap["goals"].(float64)) != 2 {
			t.Errorf("unexpected stats in JSON: %+v", statsMap)
		}

	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for SSE EventMatchUpdate")
	}
}

// TestChallenger_ConcludeMatch_IdempotencyAndNilSafety verifies strict nil safety
// and idempotency for ConcludeMatch.
func TestChallenger_ConcludeMatch_IdempotencyAndNilSafety(t *testing.T) {
	s := session.NewSessionTracker()

	// Should not panic on nil or empty GUID
	s.ConcludeMatch(nil)
	s.ConcludeMatch(&playertrack.CurrentMatchResponse{})
	s.ConcludeMatch(&playertrack.CurrentMatchResponse{MatchGUID: "   "})

	win0 := 0
	local0 := 0
	validMatch := &playertrack.CurrentMatchResponse{
		MatchGUID:   "idem-conclude-1",
		PlaylistID:  11,
		WinnerTeam:  &win0,
		LocalTeam:   &local0,
		Result:      "victory",
		LocalPlayer: &playertrack.LobbyPlayer{PlayerID: "Steam|p|0", TeamNum: 0, Stats: playertrack.PlayerStatsSummary{Goals: 1}},
	}

	s.ConcludeMatch(validMatch)
	s.ConcludeMatch(validMatch) // Duplicate call

	sum := s.GetSessionSummary()
	if sum.TotalMatches != 1 {
		t.Errorf("expected exactly 1 match after duplicate ConcludeMatch, got %d", sum.TotalMatches)
	}
	if sum.TotalWins != 1 {
		t.Errorf("expected exactly 1 win after duplicate ConcludeMatch, got %d", sum.TotalWins)
	}
}
