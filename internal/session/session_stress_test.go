package session

import (
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/domain"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/storage"
)

// Helper to create adversarial mock matches.
func createChallengerMockMatch(
	guid string,
	playlistID int,
	result string,
	localMMR float64,
	localGoals, oppGoals int,
) *playertrack.CurrentMatchResponse {
	localTeam := 0
	var winnerTeam *int
	if result == "victory" {
		w := 0
		winnerTeam = &w
	} else if result == "defeat" {
		w := 1
		winnerTeam = &w
	} // if draw, winnerTeam is nil

	var currentRank *playertrack.PlayerPlaylistRank
	if localMMR > 0 {
		currentRank = &playertrack.PlayerPlaylistRank{
			PlaylistID: playlistID,
			RankName:   "Grand Champion I Division III",
			Tier:       16,
			Division:   2,
			MMR:        localMMR,
		}
	}

	mockMatchup := &storage.PlayerMatchup{
		PlayerID:         "Steam|tm_1|0",
		PlaylistID:       playlistID,
		WinsAsTeammate:   15,
		LossesAsTeammate: 5,
		WinsAsOpponent:   8,
		LossesAsOpponent: 12,
		TotalMatches:     40,
	}

	return &playertrack.CurrentMatchResponse{
		ActiveMatch:  true,
		MatchEnded:   false,
		MatchGUID:    guid,
		PlaylistID:   playlistID,
		PlaylistName: playertrack.FormatPlaylist(playlistID),
		LocalTeam:    &localTeam,
		WinnerTeam:   winnerTeam,
		Result:       result,
		LocalPlayer: &playertrack.LobbyPlayer{
			PlayerID:    "Epic|challenger_local|0",
			Platform:    "Epic",
			Name:        "ChallengerLocal",
			TeamNum:     0,
			IsLocal:     true,
			CurrentRank: currentRank,
			Stats: playertrack.PlayerStatsSummary{
				Goals:   localGoals,
				Score:   localGoals*100 + 50,
				Shots:   localGoals + 3,
				Saves:   2,
				Assists: 1,
				Demos:   1,
			},
		},
		Teammates: []playertrack.LobbyPlayer{
			{
				PlayerID:      "Steam|tm_1|0",
				Platform:      "Steam",
				Name:          "GoodTeammate",
				TeamNum:       0,
				MatchupRecord: mockMatchup,
				Stats: playertrack.PlayerStatsSummary{
					Goals: 1,
					Score: 120,
				},
			},
		},
		Opponents: []playertrack.LobbyPlayer{
			{
				PlayerID: "Steam|opp_adversary|0",
				Platform: "Steam",
				Name:     "Adversary",
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

// ---------------------------------------------------------------------------
// 1. Alternating wins, losses, and draws across 20 distinct playlists
// ---------------------------------------------------------------------------
func TestChallenger_TwentyPlaylists_AlternatingOutcomes(t *testing.T) {
	s := NewSessionTracker()

	const numPlaylists = 20
	const cyclesPerPlaylist = 5 // 5 of each (win, loss, draw) = 15 matches per playlist -> 300 matches total
	playlists := make([]int, numPlaylists)
	for i := range numPlaylists {
		playlists[i] = 100 + i // Distinct playlist IDs 100..119
	}

	results := []string{"victory", "defeat", "draw"}

	type playlistTracker struct {
		wins    int
		losses  int
		draws   int
		matches int
	}
	expectedPlaylistStats := make(map[int]*playlistTracker)
	for _, p := range playlists {
		expectedPlaylistStats[p] = &playlistTracker{}
	}

	totalExpectedWins := 0
	totalExpectedLosses := 0
	totalExpectedMatches := 0

	matchIdx := 0
	for cycle := range cyclesPerPlaylist {
		for _, pl := range playlists {
			for _, res := range results {
				matchIdx++
				guid := fmt.Sprintf("guid-20pl-c%d-p%d-m%d", cycle, pl, matchIdx)
				localGoals := 2
				oppGoals := 1
				if res == "defeat" {
					localGoals = 0
					oppGoals = 2
				} else if res == "draw" {
					localGoals = 1
					oppGoals = 1
				}

				m := createChallengerMockMatch(guid, pl, res, 1200.0+float64(matchIdx), localGoals, oppGoals)
				s.RecordActiveMatch(m)
				s.ConcludeMatch(m)

				pt := expectedPlaylistStats[pl]
				pt.matches++
				totalExpectedMatches++
				switch res {
				case "victory":
					pt.wins++
					totalExpectedWins++
				case "defeat":
					pt.losses++
					totalExpectedLosses++
				case "draw":
					pt.draws++
				}
			}
		}
	}

	summary := s.GetSessionSummary()

	// Assert overall session totals
	if summary.TotalMatches != totalExpectedMatches {
		t.Fatalf("TotalMatches mismatch: expected %d, got %d", totalExpectedMatches, summary.TotalMatches)
	}
	if summary.TotalWins != totalExpectedWins {
		t.Fatalf("TotalWins mismatch: expected %d, got %d", totalExpectedWins, summary.TotalWins)
	}
	if summary.TotalLosses != totalExpectedLosses {
		t.Fatalf("TotalLosses mismatch: expected %d, got %d", totalExpectedLosses, summary.TotalLosses)
	}

	expectedOverallWinRate := domain.RoundFloat((float64(totalExpectedWins)/float64(totalExpectedMatches))*100.0, 1)
	if summary.WinRate != expectedOverallWinRate {
		t.Errorf("WinRate mismatch: expected %f, got %f", expectedOverallWinRate, summary.WinRate)
	}

	if len(summary.Playlists) != numPlaylists {
		t.Fatalf("Playlists map count mismatch: expected %d, got %d", numPlaylists, len(summary.Playlists))
	}
	if len(summary.Matches) != totalExpectedMatches {
		t.Fatalf("Matches history count mismatch: expected %d, got %d", totalExpectedMatches, len(summary.Matches))
	}

	// Assert per-playlist math
	for _, pl := range playlists {
		plKey := strconv.Itoa(pl)
		ps, ok := summary.Playlists[plKey]
		if !ok {
			t.Fatalf("Missing playlist %s in summary.Playlists", plKey)
		}
		expected := expectedPlaylistStats[pl]
		if ps.MatchesPlayed != expected.matches {
			t.Errorf("Playlist %d MatchesPlayed: expected %d, got %d", pl, expected.matches, ps.MatchesPlayed)
		}
		if ps.Wins != expected.wins {
			t.Errorf("Playlist %d Wins: expected %d, got %d", pl, expected.wins, ps.Wins)
		}
		if ps.Losses != expected.losses {
			t.Errorf("Playlist %d Losses: expected %d, got %d", pl, expected.losses, ps.Losses)
		}
		expectedWR := domain.RoundFloat((float64(expected.wins)/float64(expected.matches))*100.0, 1)
		if ps.WinRate != expectedWR {
			t.Errorf("Playlist %d WinRate: expected %f, got %f", pl, expectedWR, ps.WinRate)
		}
	}
}

// ---------------------------------------------------------------------------
// 2. Mixed competitive (valid MMR) and casual (0.0 MMR) playlists
// ---------------------------------------------------------------------------
func TestChallenger_MixedCompetitiveAndCasualMMR(t *testing.T) {
	s := NewSessionTracker()

	// Competitive playlist 11 (Doubles)
	compPL := 11
	// Casual playlist 2 (Doubles Casual)
	casualPL := 2

	// Match 1: Competitive win, MMR: 1000.0 -> 1009.75 (+9.75)
	m1Start := createChallengerMockMatch("comp-1", compPL, "", 1000.0, 0, 0)
	s.RecordActiveMatch(m1Start)
	m1End := createChallengerMockMatch("comp-1", compPL, "victory", 1009.75, 3, 1)
	s.ConcludeMatch(m1End)

	// Match 2: Casual win, MMR: 0.0 -> 0.0
	m2Start := createChallengerMockMatch("cas-1", casualPL, "", 0.0, 0, 0)
	s.RecordActiveMatch(m2Start)
	m2End := createChallengerMockMatch("cas-1", casualPL, "victory", 0.0, 4, 0)
	s.ConcludeMatch(m2End)

	// Match 3: Competitive loss, MMR: 1009.75 -> 1001.25 (-8.50)
	m3Start := createChallengerMockMatch("comp-2", compPL, "", 1009.75, 0, 0)
	s.RecordActiveMatch(m3Start)
	m3End := createChallengerMockMatch("comp-2", compPL, "defeat", 1001.25, 1, 2)
	s.ConcludeMatch(m3End)

	// Match 4: Casual loss, MMR: 0.0 -> 0.0
	m4Start := createChallengerMockMatch("cas-2", casualPL, "", 0.0, 0, 0)
	s.RecordActiveMatch(m4Start)
	m4End := createChallengerMockMatch("cas-2", casualPL, "defeat", 0.0, 2, 3)
	s.ConcludeMatch(m4End)

	// Match 5: Competitive match where MMR was initially 0.0 at match start, but fetched before match conclusion!
	m5Start := createChallengerMockMatch("comp-late-mmr", compPL, "", 0.0, 0, 0)
	s.RecordActiveMatch(m5Start)
	// Rank fetched mid-match:
	m5Mid := createChallengerMockMatch("comp-late-mmr", compPL, "", 1001.25, 1, 0)
	s.RecordActiveMatch(m5Mid)
	// Match concludes with +10.25 MMR -> 1011.50
	m5End := createChallengerMockMatch("comp-late-mmr", compPL, "victory", 1011.50, 2, 0)
	s.ConcludeMatch(m5End)

	summary := s.GetSessionSummary()

	// Verify Competitive Playlist 11
	pComp := summary.Playlists[strconv.Itoa(compPL)]
	if pComp == nil {
		t.Fatal("Missing competitive playlist 11")
	}
	if pComp.MatchesPlayed != 3 || pComp.Wins != 2 || pComp.Losses != 1 {
		t.Errorf("Comp PL: expected 3 played, 2 wins, 1 loss, got %d/%d/%d",
			pComp.MatchesPlayed, pComp.Wins, pComp.Losses)
	}
	if pComp.InitialMMR != 1000.0 {
		t.Errorf("Comp PL InitialMMR: expected 1000.0, got %f", pComp.InitialMMR)
	}
	if pComp.CurrentMMR != 1011.50 {
		t.Errorf("Comp PL CurrentMMR: expected 1011.50, got %f", pComp.CurrentMMR)
	}
	// Net Delta: 1011.50 - 1000.0 = +11.50
	if pComp.MMRDelta != 11.50 {
		t.Errorf("Comp PL MMRDelta: expected 11.50, got %f", pComp.MMRDelta)
	}

	// Verify Casual Playlist 2
	pCas := summary.Playlists[strconv.Itoa(casualPL)]
	if pCas == nil {
		t.Fatal("Missing casual playlist 2")
	}
	if pCas.MatchesPlayed != 2 || pCas.Wins != 1 || pCas.Losses != 1 {
		t.Errorf("Casual PL: expected 2 played, 1 win, 1 loss, got %d/%d/%d",
			pCas.MatchesPlayed, pCas.Wins, pCas.Losses)
	}
	if pCas.InitialMMR != 0.0 || pCas.CurrentMMR != 0.0 || pCas.MMRDelta != 0.0 {
		t.Errorf("Casual PL MMR should strictly be 0.0, got initial=%f, current=%f, delta=%f",
			pCas.InitialMMR, pCas.CurrentMMR, pCas.MMRDelta)
	}
	if pCas.WinRate != 50.0 {
		t.Errorf("Casual PL WinRate: expected 50.0, got %f", pCas.WinRate)
	}

	// Verify Match History records
	if len(summary.Matches) != 5 {
		t.Fatalf("Expected 5 matches in history, got %d", len(summary.Matches))
	}
	// Match 1: comp +9.75
	if summary.Matches[0].MMRChange != 9.75 {
		t.Errorf("Match 1 MMRChange expected 9.75, got %f", summary.Matches[0].MMRChange)
	}
	// Match 2: casual 0.0
	if summary.Matches[1].MMRChange != 0.0 || summary.Matches[1].StartingMMR != 0.0 || summary.Matches[1].EndingMMR != 0.0 {
		t.Errorf("Match 2 Casual MMR fields expected 0.0, got start=%f, end=%f, change=%f",
			summary.Matches[1].StartingMMR, summary.Matches[1].EndingMMR, summary.Matches[1].MMRChange)
	}
	// Match 3: comp -8.50
	if summary.Matches[2].MMRChange != -8.50 {
		t.Errorf("Match 3 MMRChange expected -8.50, got %f", summary.Matches[2].MMRChange)
	}
	// Match 4: casual 0.0
	if summary.Matches[3].MMRChange != 0.0 {
		t.Errorf("Match 4 Casual MMRChange expected 0.0, got %f", summary.Matches[3].MMRChange)
	}
	// Match 5: comp +10.25 (from 1001.25 to 1011.50)
	if summary.Matches[4].MMRChange != 10.25 {
		t.Errorf("Match 5 MMRChange expected 10.25, got %f", summary.Matches[4].MMRChange)
	}
}

// ---------------------------------------------------------------------------
// 3. Repeated duplicate match GUIDs verifying strict idempotency without score double-counting
// ---------------------------------------------------------------------------
func TestChallenger_DuplicateMatchGUIDs_StrictIdempotency(t *testing.T) {
	s := NewSessionTracker()

	guid := "dup-attack-guid-999"
	mActive := createChallengerMockMatch(guid, 13, "", 1100.0, 1, 0)
	mConcluded := createChallengerMockMatch(guid, 13, "victory", 1109.50, 4, 1)

	// Step 1: Initial record & conclude
	s.RecordActiveMatch(mActive)
	s.ConcludeMatch(mConcluded)

	// Step 2: Spam duplicate calls: RecordActiveMatch and ConcludeMatch
	for i := range 20 {
		if i%2 == 0 {
			s.RecordActiveMatch(mActive)
		}
		s.ConcludeMatch(mConcluded)
	}

	summary := s.GetSessionSummary()

	// Strict assertions: NO double counting
	if summary.TotalMatches != 1 {
		t.Fatalf("TotalMatches double-counted! Expected 1, got %d", summary.TotalMatches)
	}
	if summary.TotalWins != 1 {
		t.Fatalf("TotalWins double-counted! Expected 1, got %d", summary.TotalWins)
	}
	if summary.TotalLosses != 0 {
		t.Fatalf("TotalLosses corrupted! Expected 0, got %d", summary.TotalLosses)
	}
	if summary.WinRate != 100.0 {
		t.Errorf("WinRate expected 100.0, got %f", summary.WinRate)
	}
	if len(summary.Matches) != 1 {
		t.Fatalf("Matches history duplicated! Expected 1 entry, got %d", len(summary.Matches))
	}
	p13 := summary.Playlists["13"]
	if p13 == nil {
		t.Fatal("Missing playlist 13")
	}
	if p13.MatchesPlayed != 1 || p13.Wins != 1 || p13.Losses != 0 {
		t.Fatalf("Playlist 13 double-counted! Expected played=1, wins=1, got played=%d, wins=%d",
			p13.MatchesPlayed, p13.Wins)
	}
	if p13.InitialMMR != 1100.0 || p13.CurrentMMR != 1109.50 || p13.MMRDelta != 9.50 {
		t.Fatalf("Playlist 13 MMR corrupted by duplicate events: init=%f, curr=%f, delta=%f",
			p13.InitialMMR, p13.CurrentMMR, p13.MMRDelta)
	}

	// Box score verification: Blue score should be 4+1 = 5, not duplicated
	matchDetail := summary.Matches[0]
	if matchDetail.BlueScore != 5 {
		t.Errorf("BlueScore double counted! Expected 5, got %d", matchDetail.BlueScore)
	}
	if matchDetail.OrangeScore != 1 {
		t.Errorf("OrangeScore double counted! Expected 1, got %d", matchDetail.OrangeScore)
	}
}

// ---------------------------------------------------------------------------
// 3b. Concurrent duplicate conclusion flood
// ---------------------------------------------------------------------------
func TestChallenger_ConcurrentDuplicateFlood(t *testing.T) {
	s := NewSessionTracker()

	const numGoroutines = 50
	guid := "flood-dup-guid"
	m := createChallengerMockMatch(guid, 13, "victory", 1050.0, 3, 1)
	s.RecordActiveMatch(m)

	var wg sync.WaitGroup
	startBarrier := make(chan struct{})

	for range numGoroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startBarrier
			s.ConcludeMatch(m)
		}()
	}

	// Release all goroutines simultaneously
	close(startBarrier)
	wg.Wait()

	summary := s.GetSessionSummary()
	if summary.TotalMatches != 1 {
		t.Fatalf("Concurrent duplicate conclusion broke idempotency! Expected 1 match, got %d", summary.TotalMatches)
	}
	if summary.TotalWins != 1 {
		t.Fatalf("Expected exactly 1 win, got %d", summary.TotalWins)
	}
	if len(summary.Matches) != 1 {
		t.Fatalf("Expected exactly 1 match detail, got %d", len(summary.Matches))
	}
}

// ---------------------------------------------------------------------------
// 4. Rapid Reset calls while matches are concluding concurrently
// ---------------------------------------------------------------------------
