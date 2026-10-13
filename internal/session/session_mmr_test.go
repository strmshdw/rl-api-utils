package session

import (
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/playertrack"
)

// Helper to construct a CurrentMatchResponse with explicit in-game MMR values.
func createInGameMMRMockMatch(
	guid string,
	playlistID int,
	result string,
	localMMR float64,
	localGoals, oppGoals int,
) *playertrack.CurrentMatchResponse {
	localTeam := 0
	winnerTeam := 0
	if result == "defeat" {
		winnerTeam = 1
	}

	var currentRank *playertrack.PlayerPlaylistRank
	if localMMR > 0 {
		currentRank = &playertrack.PlayerPlaylistRank{
			PlaylistID: playlistID,
			RankName:   "Champion II Division I",
			Tier:       16,
			Division:   0,
			MMR:        localMMR,
		}
	}

	return &playertrack.CurrentMatchResponse{
		ActiveMatch:  true,
		MatchEnded:   false,
		MatchGUID:    guid,
		PlaylistID:   playlistID,
		PlaylistName: playertrack.FormatPlaylist(playlistID),
		LocalTeam:    &localTeam,
		WinnerTeam:   &winnerTeam,
		Result:       result,
		LocalPlayer: &playertrack.LobbyPlayer{
			PlayerID:    "Epic|local_hero_1|0",
			Platform:    "Epic",
			Name:        "LocalHero",
			TeamNum:     0,
			IsLocal:     true,
			CurrentRank: currentRank,
			Stats: playertrack.PlayerStatsSummary{
				Goals: localGoals,
				Score: localGoals * 100,
				Shots: localGoals + 2,
				Saves: 1,
			},
		},
		Teammates: []playertrack.LobbyPlayer{
			{
				PlayerID: "Steam|teammate_1|0",
				Platform: "Steam",
				Name:     "GoodTeammate",
				TeamNum:  0,
				Stats: playertrack.PlayerStatsSummary{
					Goals: 1,
					Score: 150,
				},
			},
		},
		Opponents: []playertrack.LobbyPlayer{
			{
				PlayerID: "Steam|opp_1|0",
				Platform: "Steam",
				Name:     "OpponentOne",
				TeamNum:  1,
				Stats: playertrack.PlayerStatsSummary{
					Goals: oppGoals,
					Score: oppGoals * 100,
				},
			},
		},
		UpdatedAt: time.Now().UTC(),
	}
}

// TestSessionTracker_TrueInGameMMR_ScaleAndDelta asserts that match start/end MMR calculations
// and session MMR deltas operate accurately on the true in-game MMR scale (~1050 instead of 47.5).
func TestSessionTracker_TrueInGameMMR_ScaleAndDelta(t *testing.T) {
	s := NewSessionTracker()

	// -------------------------------------------------------------------------
	// Match 1: Ranked 2v2 (Playlist 11) - Victory (+9.0 MMR)
	// Starting MMR: 1050.0 (converted from raw 47.5)
	// Ending MMR:   1059.0 (converted from raw 47.95)
	// -------------------------------------------------------------------------
	m1Start := createInGameMMRMockMatch("match-guid-1", 11, "", 1050.0, 0, 0)
	s.RecordActiveMatch(m1Start)

	// Verify active match baseline in session state
	mid1 := s.GetSessionSummary()
	p1Mid := mid1.Playlists["11"]
	if p1Mid == nil {
		t.Fatal("expected playlist '11' to be tracked upon active match recording")
	}
	if p1Mid.InitialMMR != 1050.0 {
		t.Errorf("expected InitialMMR 1050.0 on in-game scale, got %f", p1Mid.InitialMMR)
	}
	if p1Mid.CurrentMMR != 1050.0 {
		t.Errorf("expected CurrentMMR 1050.0 on in-game scale, got %f", p1Mid.CurrentMMR)
	}
	if p1Mid.MMRDelta != 0.0 {
		t.Errorf("expected initial MMRDelta 0.0, got %f", p1Mid.MMRDelta)
	}

	// Conclude Match 1 with victory at 1059.0
	m1End := createInGameMMRMockMatch("match-guid-1", 11, "victory", 1059.0, 3, 1)
	s.ConcludeMatch(m1End)

	s1 := s.GetSessionSummary()
	if s1.TotalMatches != 1 || s1.TotalWins != 1 {
		t.Fatalf("expected 1 match and 1 win, got %d matches, %d wins", s1.TotalMatches, s1.TotalWins)
	}

	p1 := s1.Playlists["11"]
	if p1.InitialMMR != 1050.0 {
		t.Errorf("P1 InitialMMR expected 1050.0, got %f", p1.InitialMMR)
	}
	if p1.CurrentMMR != 1059.0 {
		t.Errorf("P1 CurrentMMR expected 1059.0, got %f", p1.CurrentMMR)
	}
	if p1.MMRDelta != 9.0 {
		t.Errorf("P1 MMRDelta expected +9.0, got %f", p1.MMRDelta)
	}

	if len(s1.Matches) != 1 {
		t.Fatalf("expected 1 match history entry, got %d", len(s1.Matches))
	}
	match1 := s1.Matches[0]
	if match1.StartingMMR != 1050.0 {
		t.Errorf("Match 1 StartingMMR expected 1050.0, got %f", match1.StartingMMR)
	}
	if match1.EndingMMR != 1059.0 {
		t.Errorf("Match 1 EndingMMR expected 1059.0, got %f", match1.EndingMMR)
	}
	if match1.MMRChange != 9.0 {
		t.Errorf("Match 1 MMRChange expected +9.0, got %f", match1.MMRChange)
	}

	// -------------------------------------------------------------------------
	// Match 2: Ranked 2v2 (Playlist 11) - Defeat (-9.5 MMR)
	// Starting MMR: 1059.0
	// Ending MMR:   1049.5
	// -------------------------------------------------------------------------
	m2Start := createInGameMMRMockMatch("match-guid-2", 11, "", 1059.0, 0, 0)
	s.RecordActiveMatch(m2Start)

	m2End := createInGameMMRMockMatch("match-guid-2", 11, "defeat", 1049.5, 1, 2)
	s.ConcludeMatch(m2End)

	s2 := s.GetSessionSummary()
	p2 := s2.Playlists["11"]
	if p2.InitialMMR != 1050.0 {
		t.Errorf("P2 InitialMMR expected 1050.0 (session baseline), got %f", p2.InitialMMR)
	}
	if p2.CurrentMMR != 1049.5 {
		t.Errorf("P2 CurrentMMR expected 1049.5, got %f", p2.CurrentMMR)
	}
	// Net session delta: 1049.5 - 1050.0 = -0.5
	if p2.MMRDelta != -0.5 {
		t.Errorf("P2 MMRDelta expected -0.5, got %f", p2.MMRDelta)
	}

	match2 := s2.Matches[1]
	if match2.StartingMMR != 1059.0 {
		t.Errorf("Match 2 StartingMMR expected 1059.0, got %f", match2.StartingMMR)
	}
	if match2.EndingMMR != 1049.5 {
		t.Errorf("Match 2 EndingMMR expected 1049.5, got %f", match2.EndingMMR)
	}
	if match2.MMRChange != -9.5 {
		t.Errorf("Match 2 MMRChange expected -9.5, got %f", match2.MMRChange)
	}

	// -------------------------------------------------------------------------
	// Match 3: Ranked 3v3 (Playlist 13) - Multi-playlist isolation
	// Starting MMR: 1120.0
	// Ending MMR:   1129.2 (+9.2)
	// -------------------------------------------------------------------------
	m3Start := createInGameMMRMockMatch("match-guid-3", 13, "", 1120.0, 0, 0)
	s.RecordActiveMatch(m3Start)

	m3End := createInGameMMRMockMatch("match-guid-3", 13, "victory", 1129.2, 4, 2)
	s.ConcludeMatch(m3End)

	s3 := s.GetSessionSummary()
	p3 := s3.Playlists["13"]
	if p3 == nil {
		t.Fatal("expected playlist '13' to exist")
	}
	if p3.InitialMMR != 1120.0 || p3.CurrentMMR != 1129.2 || p3.MMRDelta != 9.2 {
		t.Errorf("Playlist 13 expected initial=1120.0, curr=1129.2, delta=9.2, got %+v", p3)
	}

	// Playlist 11 must remain isolated and unchanged
	p1Preserved := s3.Playlists["11"]
	if p1Preserved.InitialMMR != 1050.0 || p1Preserved.CurrentMMR != 1049.5 || p1Preserved.MMRDelta != -0.5 {
		t.Errorf("Playlist 11 mutated unexpectedly: %+v", p1Preserved)
	}
}

// TestSessionTracker_TrueInGameMMR_LateRankArrival asserts that if rank data arrives
// asynchronously after match kickoff, StartingMMR and InitialMMR are backfilled to true in-game MMR.
func TestSessionTracker_TrueInGameMMR_LateRankArrival(t *testing.T) {
	s := NewSessionTracker()

	// 1. Kickoff frame: Ranks have not arrived yet (CurrentRank is nil)
	mStart := createInGameMMRMockMatch("late-guid-1", 11, "", 0.0, 0, 0)
	s.RecordActiveMatch(mStart)

	mid1 := s.GetSessionSummary()
	pMid1 := mid1.Playlists["11"]
	if pMid1 == nil {
		t.Fatal("expected playlist '11' initialized")
	}
	if pMid1.InitialMMR != 0.0 {
		t.Errorf("expected InitialMMR 0.0 before rank arrives, got %f", pMid1.InitialMMR)
	}

	// 2. Rank arrives asynchronously: LocalPlayer.CurrentRank.MMR = 1050.0
	mRankArrived := createInGameMMRMockMatch("late-guid-1", 11, "", 1050.0, 1, 0)
	s.RecordActiveMatch(mRankArrived)

	mid2 := s.GetSessionSummary()
	pMid2 := mid2.Playlists["11"]
	if pMid2.InitialMMR != 1050.0 {
		t.Errorf("expected InitialMMR backfilled to 1050.0, got %f", pMid2.InitialMMR)
	}
	if pMid2.CurrentMMR != 1050.0 {
		t.Errorf("expected CurrentMMR backfilled to 1050.0, got %f", pMid2.CurrentMMR)
	}
	if pMid2.MMRDelta != 0.0 {
		t.Errorf("expected MMRDelta 0.0, got %f", pMid2.MMRDelta)
	}

	// 3. Conclude match at 1059.0
	mEnd := createInGameMMRMockMatch("late-guid-1", 11, "victory", 1059.0, 3, 1)
	s.ConcludeMatch(mEnd)

	finalSummary := s.GetSessionSummary()
	pFinal := finalSummary.Playlists["11"]
	if pFinal.MMRDelta != 9.0 {
		t.Errorf("expected MMRDelta 9.0 after late arrival, got %f", pFinal.MMRDelta)
	}

	matchDetail := finalSummary.Matches[0]
	if matchDetail.StartingMMR != 1050.0 {
		t.Errorf("expected match StartingMMR 1050.0, got %f", matchDetail.StartingMMR)
	}
	if matchDetail.EndingMMR != 1059.0 {
		t.Errorf("expected match EndingMMR 1059.0, got %f", matchDetail.EndingMMR)
	}
	if matchDetail.MMRChange != 9.0 {
		t.Errorf("expected match MMRChange 9.0, got %f", matchDetail.MMRChange)
	}
}

// TestSessionTracker_TrueInGameMMR_RosterAndDisconnectedSnapshots asserts that all players
// (teammates, opponents, spectators, and early leavers) have their MMR stored on the in-game scale.
func TestSessionTracker_TrueInGameMMR_RosterAndDisconnectedSnapshots(t *testing.T) {
	s := NewSessionTracker()

	winner := 0
	localTeam := 0
	match := &playertrack.CurrentMatchResponse{
		ActiveMatch:  false,
		MatchEnded:   true,
		MatchGUID:    "roster-guid-1",
		PlaylistID:   11,
		PlaylistName: "Ranked Doubles 2v2",
		LocalTeam:    &localTeam,
		WinnerTeam:   &winner,
		Result:       "victory",
		LocalPlayer: &playertrack.LobbyPlayer{
			PlayerID: "Epic|local_1|0",
			Name:     "LocalPlayer",
			TeamNum:  0,
			IsLocal:  true,
			CurrentRank: &playertrack.PlayerPlaylistRank{
				RankName: "Champion II Division I",
				Tier:     16,
				Division: 0,
				MMR:      1050.0,
			},
			Stats: playertrack.PlayerStatsSummary{Goals: 2, Score: 400},
		},
		Teammates: []playertrack.LobbyPlayer{
			{
				PlayerID:       "Steam|leaver_tm|0",
				Name:           "LeaverTeammate",
				TeamNum:        0,
				IsDisconnected: true, // Left early
				CurrentRank: &playertrack.PlayerPlaylistRank{
					RankName: "Champion I Division IV",
					Tier:     15,
					Division: 3,
					MMR:      1038.0,
				},
				Stats: playertrack.PlayerStatsSummary{Goals: 1, Score: 200},
			},
		},
		Opponents: []playertrack.LobbyPlayer{
			{
				PlayerID: "Steam|opp_1|0",
				Name:     "OpponentOne",
				TeamNum:  1,
				CurrentRank: &playertrack.PlayerPlaylistRank{
					RankName: "Champion II Division II",
					Tier:     16,
					Division: 1,
					MMR:      1062.5,
				},
				Stats: playertrack.PlayerStatsSummary{Goals: 1, Score: 250},
			},
		},
	}

	// Record active then conclude
	active := match.DeepClone()
	active.ActiveMatch = true
	active.MatchEnded = false
	s.RecordActiveMatch(active)
	s.ConcludeMatch(match)

	summary := s.GetSessionSummary()
	if len(summary.Matches) != 1 {
		t.Fatalf("expected 1 match detail, got %d", len(summary.Matches))
	}

	detail := summary.Matches[0]
	var foundLocal, foundLeaver, foundOpp bool
	for _, p := range detail.Players {
		switch p.PlayerID {
		case "Epic|local_1|0":
			foundLocal = true
			if p.MMR != 1050.0 {
				t.Errorf("LocalPlayer MMR expected 1050.0, got %f", p.MMR)
			}
		case "Steam|leaver_tm|0":
			foundLeaver = true
			if p.MMR != 1038.0 {
				t.Errorf("Disconnected teammate MMR expected 1038.0, got %f", p.MMR)
			}
			if !p.IsDisconnected {
				t.Error("Disconnected teammate missing IsDisconnected flag")
			}
		case "Steam|opp_1|0":
			foundOpp = true
			if p.MMR != 1062.5 {
				t.Errorf("Opponent MMR expected 1062.5, got %f", p.MMR)
			}
		}
	}

	if !foundLocal || !foundLeaver || !foundOpp {
		t.Errorf("missing players in snapshot: local=%v leaver=%v opp=%v", foundLocal, foundLeaver, foundOpp)
	}
}

// TestSessionTracker_TrueInGameMMR_CasualZeroGuard asserts that unranked and casual playlists
// (where MMR is 0.0) never produce false MMR gains or evaluate to 100.0 MMR.
func TestSessionTracker_TrueInGameMMR_CasualZeroGuard(t *testing.T) {
	s := NewSessionTracker()

	// Playlist 2 = Casual 2v2 with MMR 0.0
	mStart := createInGameMMRMockMatch("casual-guid-1", 2, "", 0.0, 0, 0)
	s.RecordActiveMatch(mStart)

	mEnd := createInGameMMRMockMatch("casual-guid-1", 2, "victory", 0.0, 4, 1)
	s.ConcludeMatch(mEnd)

	summary := s.GetSessionSummary()
	pCasual := summary.Playlists["2"]
	if pCasual == nil {
		t.Fatal("expected casual playlist 2 to be tracked")
	}

	if pCasual.InitialMMR != 0.0 || pCasual.CurrentMMR != 0.0 || pCasual.MMRDelta != 0.0 {
		t.Errorf("casual playlist produced non-zero MMR values: initial=%f, curr=%f, delta=%f",
			pCasual.InitialMMR, pCasual.CurrentMMR, pCasual.MMRDelta)
	}

	detail := summary.Matches[0]
	if detail.StartingMMR != 0.0 || detail.EndingMMR != 0.0 || detail.MMRChange != 0.0 {
		t.Errorf("casual match produced non-zero MMR change: start=%f, end=%f, change=%f",
			detail.StartingMMR, detail.EndingMMR, detail.MMRChange)
	}
}
