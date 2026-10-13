package session_test

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/session"
)

// Helper to create a CurrentMatchResponse for testing.
func makeMockMatch(
	guid string,
	playlistID int,
	result string,
	localMMR float64,
	localGoals, oppGoals int,
	ended bool,
) *playertrack.CurrentMatchResponse {
	localTeam := 0
	var winnerTeam *int
	if ended {
		w := 0
		if result == "defeat" {
			w = 1
		} else if result == "draw" {
			winnerTeam = nil
		} else {
			winnerTeam = &w
		}
	}

	var currentRank *playertrack.PlayerPlaylistRank
	var ranksSnapshot playertrack.PlayerRanksSnapshot
	if localMMR > 0 {
		currentRank = &playertrack.PlayerPlaylistRank{
			PlaylistID: playlistID,
			RankName:   playertrack.FormatPlaylist(playlistID),
			Tier:       16,
			Division:   1,
			MMR:        localMMR,
		}
		ranksSnapshot = playertrack.PlayerRanksSnapshot{
			fmt.Sprintf("%d", playlistID): *currentRank,
		}
	}

	return &playertrack.CurrentMatchResponse{
		ActiveMatch:  !ended,
		MatchEnded:   ended,
		MatchGUID:    guid,
		PlaylistID:   playlistID,
		PlaylistName: playertrack.FormatPlaylist(playlistID),
		LocalTeam:    &localTeam,
		WinnerTeam:   winnerTeam,
		Result:       result,
		LocalPlayer: &playertrack.LobbyPlayer{
			PlayerID:    "Epic|local_challenger|0",
			Platform:    "Epic",
			Name:        "LocalChallenger",
			TeamNum:     0,
			IsLocal:     true,
			CurrentRank: currentRank,
			Ranks:       ranksSnapshot,
			Stats: playertrack.PlayerStatsSummary{
				Goals: localGoals,
				Score: localGoals * 100,
				Shots: localGoals + 1,
				Saves: 1,
			},
		},
		Teammates: []playertrack.LobbyPlayer{
			{
				PlayerID: "Steam|teammate_challenger|0",
				Platform: "Steam",
				Name:     "TeammateChallenger",
				TeamNum:  0,
				CurrentRank: &playertrack.PlayerPlaylistRank{
					PlaylistID: playlistID,
					Tier:       15,
					Division:   3,
					MMR:        1040.0,
				},
				Stats: playertrack.PlayerStatsSummary{
					Goals: 0,
					Score: 120,
				},
			},
		},
		Opponents: []playertrack.LobbyPlayer{
			{
				PlayerID: "Steam|opponent_challenger|0",
				Platform: "Steam",
				Name:     "OpponentChallenger",
				TeamNum:  1,
				CurrentRank: &playertrack.PlayerPlaylistRank{
					PlaylistID: playlistID,
					Tier:       16,
					Division:   0,
					MMR:        1052.0,
				},
				Stats: playertrack.PlayerStatsSummary{
					Goals: oppGoals,
					Score: oppGoals * 100,
				},
			},
		},
		UpdatedAt: time.Now().UTC(),
	}
}

// TestChallenger_Stress_MultiPlaylistTransitions_WithOvertime stress-tests a complex
// multi-match sequence across Doubles (11), Standard (13), and Duel (10), including
// overtime matches, back-to-back switching, cumulative playlist deltas, and session reset.
func TestChallenger_Stress_MultiPlaylistTransitions_WithOvertime(t *testing.T) {
	s := session.NewSessionTracker()

	type stepDef struct {
		name         string
		guid         string
		playlistID   int
		startMMR     float64
		endMMR       float64
		localGoals   int
		oppGoals     int
		result       string
		isOvertime   bool
	}

	sequence := []stepDef{
		{
			name:       "Match 1: Ranked Doubles (11) - Clean Victory",
			guid:       "stress-m1-doubles",
			playlistID: 11,
			startMMR:   1050.0,
			endMMR:     1059.2, // +9.2 MMR
			localGoals: 3,
			oppGoals:   1,
			result:     "victory",
		},
		{
			name:       "Match 2: Ranked Duel 1v1 (10) - Overtime Sudden Death Victory",
			guid:       "stress-m2-duel",
			playlistID: 10,
			startMMR:   920.0,
			endMMR:     930.5, // +10.5 MMR
			localGoals: 5,     // 4-4 regulation, 5th in OT
			oppGoals:   4,
			result:     "victory",
			isOvertime: true,
		},
		{
			name:       "Match 3: Ranked Standard 3v3 (13) - Tough Defeat",
			guid:       "stress-m3-standard",
			playlistID: 13,
			startMMR:   1120.0,
			endMMR:     1110.8, // -9.2 MMR
			localGoals: 1,
			oppGoals:   3,
			result:     "defeat",
		},
		{
			name:       "Match 4: Ranked Doubles (11) - Return to Doubles, Defeat",
			guid:       "stress-m4-doubles",
			playlistID: 11,
			startMMR:   1059.2,
			endMMR:     1049.7, // -9.5 MMR (net P11 delta: 1049.7 - 1050.0 = -0.3)
			localGoals: 2,
			oppGoals:   4,
			result:     "defeat",
		},
		{
			name:       "Match 5: Ranked Duel 1v1 (10) - Return to Duel, Defeat",
			guid:       "stress-m5-duel",
			playlistID: 10,
			startMMR:   930.5,
			endMMR:     921.0, // -9.5 MMR (net P10 delta: 921.0 - 920.0 = +1.0)
			localGoals: 2,
			oppGoals:   5,
			result:     "defeat",
		},
	}

	for idx, step := range sequence {
		t.Run(step.name, func(t *testing.T) {
			// 1. Kickoff
			mStart := makeMockMatch(step.guid, step.playlistID, "", step.startMMR, 0, 0, false)
			s.RecordActiveMatch(mStart)

			midSnap := s.GetSessionSummary()
			pMid := midSnap.Playlists[fmt.Sprintf("%d", step.playlistID)]
			if pMid == nil {
				t.Fatalf("[%s] playlist %d not found in session mid-match", step.name, step.playlistID)
			}
			if pMid.CurrentMMR != step.startMMR {
				t.Errorf("[%s] mid-match CurrentMMR expected %f, got %f", step.name, step.startMMR, pMid.CurrentMMR)
			}

			// 2. Mid-game goal progression
			mGoal := makeMockMatch(step.guid, step.playlistID, "", step.startMMR, step.localGoals, step.oppGoals, false)
			s.RecordActiveMatch(mGoal)

			// 3. Conclude
			mEnd := makeMockMatch(step.guid, step.playlistID, step.result, step.endMMR, step.localGoals, step.oppGoals, true)
			s.ConcludeMatch(mEnd)

			afterSnap := s.GetSessionSummary()
			if afterSnap.TotalMatches != idx+1 {
				t.Errorf("[%s] TotalMatches expected %d, got %d", step.name, idx+1, afterSnap.TotalMatches)
			}
		})
	}

	// Verify final session metrics
	summary := s.GetSessionSummary()
	if summary.TotalMatches != 5 {
		t.Fatalf("expected 5 total matches, got %d", summary.TotalMatches)
	}
	if summary.TotalWins != 2 || summary.TotalLosses != 3 {
		t.Fatalf("expected 2 wins, 3 losses, got %d wins, %d losses", summary.TotalWins, summary.TotalLosses)
	}
	if summary.WinRate != 40.0 {
		t.Errorf("expected win rate 40.0%%, got %.1f%%", summary.WinRate)
	}

	// Verify Playlist 11 (Doubles)
	p11 := summary.Playlists["11"]
	if p11 == nil {
		t.Fatal("missing playlist 11")
	}
	if p11.MatchesPlayed != 2 || p11.Wins != 1 || p11.Losses != 1 {
		t.Errorf("P11 matches: expected 2 (1W/1L), got %d (%dW/%dL)", p11.MatchesPlayed, p11.Wins, p11.Losses)
	}
	if p11.InitialMMR != 1050.0 {
		t.Errorf("P11 InitialMMR expected 1050.0, got %f", p11.InitialMMR)
	}
	if p11.CurrentMMR != 1049.7 {
		t.Errorf("P11 CurrentMMR expected 1049.7, got %f", p11.CurrentMMR)
	}
	if p11.MMRDelta != -0.3 {
		t.Errorf("P11 MMRDelta expected -0.3, got %f", p11.MMRDelta)
	}

	// Verify Playlist 10 (Duel 1v1)
	p10 := summary.Playlists["10"]
	if p10 == nil {
		t.Fatal("missing playlist 10")
	}
	if p10.MatchesPlayed != 2 || p10.Wins != 1 || p10.Losses != 1 {
		t.Errorf("P10 matches: expected 2 (1W/1L), got %d (%dW/%dL)", p10.MatchesPlayed, p10.Wins, p10.Losses)
	}
	if p10.InitialMMR != 920.0 {
		t.Errorf("P10 InitialMMR expected 920.0, got %f", p10.InitialMMR)
	}
	if p10.CurrentMMR != 921.0 {
		t.Errorf("P10 CurrentMMR expected 921.0, got %f", p10.CurrentMMR)
	}
	if p10.MMRDelta != 1.0 {
		t.Errorf("P10 MMRDelta expected +1.0, got %f", p10.MMRDelta)
	}

	// Verify Playlist 13 (Standard 3v3)
	p13 := summary.Playlists["13"]
	if p13 == nil {
		t.Fatal("missing playlist 13")
	}
	if p13.MatchesPlayed != 1 || p13.Wins != 0 || p13.Losses != 1 {
		t.Errorf("P13 matches: expected 1 (0W/1L), got %d (%dW/%dL)", p13.MatchesPlayed, p13.Wins, p13.Losses)
	}
	if p13.InitialMMR != 1120.0 || p13.CurrentMMR != 1110.8 || p13.MMRDelta != -9.2 {
		t.Errorf("P13 MMR expected init=1120.0, curr=1110.8, delta=-9.2, got %+v", p13)
	}

	// Verify individual match histories
	if len(summary.Matches) != 5 {
		t.Fatalf("expected 5 match details, got %d", len(summary.Matches))
	}

	// Match 1: Starting 1050, Ending 1059.2, Change +9.2
	m1 := summary.Matches[0]
	if m1.StartingMMR != 1050.0 || m1.EndingMMR != 1059.2 || m1.MMRChange != 9.2 {
		t.Errorf("m1 MMR mismatch: start=%f end=%f change=%f", m1.StartingMMR, m1.EndingMMR, m1.MMRChange)
	}
	if m1.BlueScore != 3 || m1.OrangeScore != 1 {
		t.Errorf("m1 scores mismatch: blue=%d orange=%d", m1.BlueScore, m1.OrangeScore)
	}

	// Match 4: Starting 1059.2, Ending 1049.7, Change -9.5
	m4 := summary.Matches[3]
	if m4.StartingMMR != 1059.2 || m4.EndingMMR != 1049.7 || m4.MMRChange != -9.5 {
		t.Errorf("m4 MMR mismatch: start=%f end=%f change=%f", m4.StartingMMR, m4.EndingMMR, m4.MMRChange)
	}

	// Test Session Reset
	resetSummary := s.Reset()
	if resetSummary.TotalMatches != 0 || resetSummary.TotalWins != 0 || resetSummary.TotalLosses != 0 {
		t.Errorf("expected Reset to clear totals, got %+v", resetSummary)
	}
	if len(resetSummary.Playlists) != 0 || len(resetSummary.Matches) != 0 {
		t.Errorf("expected Reset to clear playlists and matches, got %d playlists, %d matches",
			len(resetSummary.Playlists), len(resetSummary.Matches))
	}
}

// TestChallenger_Stress_LateRankArrival_RaceConditions subjects the late rank arrival
// backfill mechanism to rapid concurrent interleaved events:
// - Kickoff arrives with 0.0 MMR
// - Mid-game score updates arrive
// - Rank arrival fires concurrently in a separate goroutine
// - Match concludes either concurrently or immediately following
// Assert that StartingMMR is backfilled properly to the in-game scale (~1050.0),
// EndingMMR reflects in-game scale, and MMRChange is accurate (never producing raw decimal differences).
func TestChallenger_Stress_LateRankArrival_RaceConditions(t *testing.T) {
	for iter := 0; iter < 30; iter++ {
		s := session.NewSessionTracker()
		guid := fmt.Sprintf("late-race-guid-%d", iter)
		playlistID := 11
		inGameStartMMR := 1050.0 + float64(iter)
		inGameEndMMR := inGameStartMMR + 9.0

		// 1. Kickoff frame with MMR = 0.0 (unresolved rank)
		mKickoff := makeMockMatch(guid, playlistID, "", 0.0, 0, 0, false)
		s.RecordActiveMatch(mKickoff)

		var wg sync.WaitGroup
		wg.Add(3)

		// Goroutine A: Multiple mid-game goal updates
		go func() {
			defer wg.Done()
			for g := 1; g <= 3; g++ {
				mUpdate := makeMockMatch(guid, playlistID, "", 0.0, g, 0, false)
				s.RecordActiveMatch(mUpdate)
				time.Sleep(time.Duration(rand.Intn(3)) * time.Millisecond)
			}
		}()

		// Goroutine B: Late rank arrival (setting true in-game MMR)
		go func() {
			defer wg.Done()
			time.Sleep(time.Duration(rand.Intn(4)+1) * time.Millisecond)
			mRank := makeMockMatch(guid, playlistID, "", inGameStartMMR, 2, 0, false)
			s.RecordActiveMatch(mRank)
		}()

		// Goroutine C: Conclude match
		go func() {
			defer wg.Done()
			time.Sleep(time.Duration(rand.Intn(5)+4) * time.Millisecond)
			mEnd := makeMockMatch(guid, playlistID, "victory", inGameEndMMR, 3, 1, true)
			s.ConcludeMatch(mEnd)
		}()

		wg.Wait()

		// Verify final match state
		summary := s.GetSessionSummary()
		if len(summary.Matches) != 1 {
			t.Fatalf("iteration %d: expected 1 match, got %d", iter, len(summary.Matches))
		}

		matchDetail := summary.Matches[0]
		// EndingMMR must always be inGameEndMMR
		if matchDetail.EndingMMR != inGameEndMMR {
			t.Errorf("iteration %d: EndingMMR expected %f, got %f", iter, inGameEndMMR, matchDetail.EndingMMR)
		}

		// StartingMMR must be either inGameStartMMR (if rank arrived before conclude)
		// or 0.0 (if conclude won the race before rank arrival).
		// Crucially, it must NEVER be a raw TrueSkill rating like 47.5!
		if matchDetail.StartingMMR > 0 && matchDetail.StartingMMR != inGameStartMMR {
			t.Errorf("iteration %d: corrupted StartingMMR: %f (expected %f or 0.0)",
				iter, matchDetail.StartingMMR, inGameStartMMR)
		}

		// If StartingMMR was backfilled, MMRChange must be exactly +9.0
		if matchDetail.StartingMMR == inGameStartMMR {
			if matchDetail.MMRChange != 9.0 {
				t.Errorf("iteration %d: MMRChange expected 9.0, got %f", iter, matchDetail.MMRChange)
			}
		}
	}
}

// TestChallenger_Stress_DisconnectedPlayers_MMRRetention tests that human leavers
// in both active match telemetry and completed match history preserve their in-game MMR
// without reset or dropping to 0.0, and their stats, goals, and win/loss outcomes are accurately logged.
func TestChallenger_Stress_DisconnectedPlayers_MMRRetention(t *testing.T) {
	s := session.NewSessionTracker()

	winnerTeam := 0
	localTeam := 0

	// 3v3 match with 2 early leavers: 1 teammate leaver, 1 opponent leaver
	match := &playertrack.CurrentMatchResponse{
		ActiveMatch:  false,
		MatchEnded:   true,
		MatchGUID:    "leaver-stress-guid-1",
		PlaylistID:   13, // Ranked Standard 3v3
		PlaylistName: "Ranked Standard 3v3",
		LocalTeam:    &localTeam,
		WinnerTeam:   &winnerTeam,
		Result:       "victory",
		LocalPlayer: &playertrack.LobbyPlayer{
			PlayerID: "Epic|local_captain|0",
			Name:     "LocalCaptain",
			TeamNum:  0,
			IsLocal:  true,
			CurrentRank: &playertrack.PlayerPlaylistRank{
				PlaylistID: 13,
				RankName:   "Grand Champion I Division II",
				Tier:       19,
				Division:   1,
				MMR:        1462.0, // In-game MMR
			},
			Stats: playertrack.PlayerStatsSummary{Goals: 2, Score: 500, Shots: 4, Saves: 2},
		},
		Teammates: []playertrack.LobbyPlayer{
			{
				PlayerID:       "Steam|leaver_tm_1|0",
				Name:           "RageQuitTeammate",
				TeamNum:        0,
				IsDisconnected: true, // Left after 2 minutes
				CurrentRank: &playertrack.PlayerPlaylistRank{
					PlaylistID: 13,
					RankName:   "Grand Champion I Division I",
					Tier:       19,
					Division:   0,
					MMR:        1445.0, // In-game MMR
				},
				Stats: playertrack.PlayerStatsSummary{Goals: 2, Score: 350, Shots: 3, Saves: 1},
			},
			{
				PlayerID: "Epic|staying_tm_2|0",
				Name:     "LoyalTeammate",
				TeamNum:  0,
				CurrentRank: &playertrack.PlayerPlaylistRank{
					PlaylistID: 13,
					RankName:   "Grand Champion I Division III",
					Tier:       19,
					Division:   2,
					MMR:        1478.0,
				},
				Stats: playertrack.PlayerStatsSummary{Goals: 1, Score: 300, Shots: 2, Saves: 3},
			},
		},
		Opponents: []playertrack.LobbyPlayer{
			{
				PlayerID:       "Steam|leaver_opp_1|0",
				Name:           "TiltedOpponent",
				TeamNum:        1,
				IsDisconnected: true, // Left after conceding 4th goal
				CurrentRank: &playertrack.PlayerPlaylistRank{
					PlaylistID: 13,
					RankName:   "Grand Champion I Division I",
					Tier:       19,
					Division:   0,
					MMR:        1440.0,
				},
				Stats: playertrack.PlayerStatsSummary{Goals: 1, Score: 200, Shots: 2, Saves: 0},
			},
			{
				PlayerID: "Steam|staying_opp_2|0",
				Name:     "OpponentTwo",
				TeamNum:  1,
				CurrentRank: &playertrack.PlayerPlaylistRank{
					PlaylistID: 13,
					RankName:   "Grand Champion I Division II",
					Tier:       19,
					Division:   1,
					MMR:        1455.0,
				},
				Stats: playertrack.PlayerStatsSummary{Goals: 2, Score: 400, Shots: 5, Saves: 1},
			},
			{
				PlayerID: "Epic|staying_opp_3|0",
				Name:     "OpponentThree",
				TeamNum:  1,
				CurrentRank: &playertrack.PlayerPlaylistRank{
					PlaylistID: 13,
					RankName:   "Champion III Division IV",
					Tier:       18,
					Division:   3,
					MMR:        1425.0,
				},
				Stats: playertrack.PlayerStatsSummary{Goals: 0, Score: 180, Shots: 1, Saves: 2},
			},
		},
	}

	// 1. Record active match state first
	activeMatch := match.DeepClone()
	activeMatch.ActiveMatch = true
	activeMatch.MatchEnded = false
	s.RecordActiveMatch(activeMatch)

	// 2. Conclude match
	s.ConcludeMatch(match)

	summary := s.GetSessionSummary()
	if len(summary.Matches) != 1 {
		t.Fatalf("expected 1 match history entry, got %d", len(summary.Matches))
	}

	detail := summary.Matches[0]

	// Total Blue Goals: Local(2) + LeaverTm(2) + LoyalTm(1) = 5
	// Total Orange Goals: LeaverOpp(1) + Opp2(2) + Opp3(0) = 3
	if detail.BlueScore != 5 {
		t.Errorf("expected BlueScore 5 (including leaver goals), got %d", detail.BlueScore)
	}
	if detail.OrangeScore != 3 {
		t.Errorf("expected OrangeScore 3 (including leaver goals), got %d", detail.OrangeScore)
	}

	// Assert player entries
	if len(detail.Players) != 6 {
		t.Fatalf("expected 6 players in match history snapshot, got %d", len(detail.Players))
	}

	playerMap := make(map[string]session.SessionMatchPlayer)
	for _, p := range detail.Players {
		playerMap[p.PlayerID] = p
	}

	// Teammate leaver checks
	tmLeaver, ok := playerMap["Steam|leaver_tm_1|0"]
	if !ok {
		t.Fatal("missing disconnected teammate in players snapshot")
	}
	if !tmLeaver.IsDisconnected {
		t.Error("expected IsDisconnected=true for teammate leaver")
	}
	if tmLeaver.MMR != 1445.0 {
		t.Errorf("expected teammate leaver MMR 1445.0 on in-game scale, got %f", tmLeaver.MMR)
	}
	if tmLeaver.Won == nil || !*tmLeaver.Won {
		t.Errorf("expected teammate leaver Won=true (blue team won), got %v", tmLeaver.Won)
	}
	if tmLeaver.Stats.Goals != 2 {
		t.Errorf("expected teammate leaver goals 2, got %d", tmLeaver.Stats.Goals)
	}

	// Opponent leaver checks
	oppLeaver, ok := playerMap["Steam|leaver_opp_1|0"]
	if !ok {
		t.Fatal("missing disconnected opponent in players snapshot")
	}
	if !oppLeaver.IsDisconnected {
		t.Error("expected IsDisconnected=true for opponent leaver")
	}
	if oppLeaver.MMR != 1440.0 {
		t.Errorf("expected opponent leaver MMR 1440.0 on in-game scale, got %f", oppLeaver.MMR)
	}
	if oppLeaver.Won == nil || *oppLeaver.Won {
		t.Errorf("expected opponent leaver Won=false (orange team lost), got %v", oppLeaver.Won)
	}
	if oppLeaver.Stats.Goals != 1 {
		t.Errorf("expected opponent leaver goals 1, got %d", oppLeaver.Stats.Goals)
	}
}

// TestChallenger_Stress_ExtremeMMR_BoundaryValues asserts that extreme rating values
// (SSL pro at 2600 MMR, casual unranked 0.0 MMR, Bronze I at 150 MMR) are safely handled
// without numerical distortion, division by zero, or negative MMR leaks.
func TestChallenger_Stress_ExtremeMMR_BoundaryValues(t *testing.T) {
	s := session.NewSessionTracker()

	cases := []struct {
		name       string
		guid       string
		playlistID int
		startMMR   float64
		endMMR     float64
		expStart   float64
		expEnd     float64
		expChange  float64
	}{
		{
			name:       "Casual Playlist (MMR 0.0)",
			guid:       "boundary-casual",
			playlistID: 2,
			startMMR:   0.0,
			endMMR:     0.0,
			expStart:   0.0,
			expEnd:     0.0,
			expChange:  0.0,
		},
		{
			name:       "Bronze I Lowest Ranked (MMR 150.0)",
			guid:       "boundary-bronze",
			playlistID: 11,
			startMMR:   150.0,
			endMMR:     162.5,
			expStart:   150.0,
			expEnd:     162.5,
			expChange:  12.5,
		},
		{
			name:       "Pro Player SSL (MMR 2600.0)",
			guid:       "boundary-pro-ssl",
			playlistID: 11,
			startMMR:   2600.0,
			endMMR:     2608.8,
			expStart:   2600.0,
			expEnd:     2608.8,
			expChange:  8.8,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mStart := makeMockMatch(tc.guid, tc.playlistID, "", tc.startMMR, 0, 0, false)
			s.RecordActiveMatch(mStart)

			mEnd := makeMockMatch(tc.guid, tc.playlistID, "victory", tc.endMMR, 2, 1, true)
			s.ConcludeMatch(mEnd)

			summary := s.GetSessionSummary()
			var matchFound *session.SessionMatchDetail
			for _, m := range summary.Matches {
				if m.MatchGUID == tc.guid {
					matchFound = m
					break
				}
			}

			if matchFound == nil {
				t.Fatalf("match %s not found", tc.guid)
			}
			if matchFound.StartingMMR != tc.expStart {
				t.Errorf("StartingMMR expected %f, got %f", tc.expStart, matchFound.StartingMMR)
			}
			if matchFound.EndingMMR != tc.expEnd {
				t.Errorf("EndingMMR expected %f, got %f", tc.expEnd, matchFound.EndingMMR)
			}
			if matchFound.MMRChange != tc.expChange {
				t.Errorf("MMRChange expected %f, got %f", tc.expChange, matchFound.MMRChange)
			}
		})
	}
}
