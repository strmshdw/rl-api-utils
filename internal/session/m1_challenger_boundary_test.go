package session_test

import (
	"testing"

	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/session"
)

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
