package session

import (
	"fmt"
	"math/rand"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

	expectedOverallWinRate := roundFloat((float64(totalExpectedWins)/float64(totalExpectedMatches))*100.0, 1)
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
		expectedWR := roundFloat((float64(expected.wins)/float64(expected.matches))*100.0, 1)
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
func TestChallenger_RapidResetWhileMatchesConcluding(t *testing.T) {
	s := NewSessionTracker()

	stopCh := make(chan struct{})
	var wg sync.WaitGroup

	playlists := []int{1, 2, 10, 11, 13, 27, 28, 29}
	var concludesAttempted int64
	var resetsAttempted int64
	var readsAttempted int64

	// Goroutines rapidly concluding matches
	for i := range 12 {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(workerID*37)))
			guidCounter := 0
			for {
				select {
				case <-stopCh:
					return
				default:
					guidCounter++
					guid := fmt.Sprintf("rapid-m-w%d-%d", workerID, guidCounter)
					pl := playlists[rng.Intn(len(playlists))]
					mmr := 0.0
					if pl >= 10 {
						mmr = 900.0 + float64(rng.Intn(300))
					}
					results := []string{"victory", "defeat", "draw"}
					res := results[rng.Intn(len(results))]

					// Simulate active -> conclude flow
					mActive := createChallengerMockMatch(guid, pl, "", mmr, rng.Intn(3), rng.Intn(3))
					s.RecordActiveMatch(mActive)

					if mmr > 0 {
						mmr += float64(rng.Intn(19) - 9) // -9 to +9
					}
					mEnd := createChallengerMockMatch(guid, pl, res, mmr, rng.Intn(4), rng.Intn(4))
					s.ConcludeMatch(mEnd)
					atomic.AddInt64(&concludesAttempted, 1)

					// Intermittent duplicate call to verify resilience
					if rng.Intn(4) == 0 {
						s.ConcludeMatch(mEnd)
					}
				}
			}
		}(i)
	}

	// Goroutines rapidly calling Reset
	for i := range 6 {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(workerID*73)))
			for {
				select {
				case <-stopCh:
					return
				default:
					time.Sleep(time.Duration(5+rng.Intn(15)) * time.Millisecond)
					s.Reset()
					atomic.AddInt64(&resetsAttempted, 1)
				}
			}
		}(i)
	}

	// Goroutines continuously reading session summary and validating invariants
	for i := range 8 {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for {
				select {
				case <-stopCh:
					return
				default:
					sum := s.GetSessionSummary()
					atomic.AddInt64(&readsAttempted, 1)

					// Verify mathematical invariant: totalWins + totalLosses <= totalMatches
					// (draws account for totalMatches - totalWins - totalLosses)
					if sum.TotalWins+sum.TotalLosses > sum.TotalMatches {
						t.Errorf("INVARIANT VIOLATION: TotalWins (%d) + TotalLosses (%d) > TotalMatches (%d)",
							sum.TotalWins, sum.TotalLosses, sum.TotalMatches)
					}
					if len(sum.Matches) != sum.TotalMatches {
						t.Errorf("INVARIANT VIOLATION: len(Matches) (%d) != TotalMatches (%d)",
							len(sum.Matches), sum.TotalMatches)
					}
					if sum.WinRate < 0.0 || sum.WinRate > 100.0 {
						t.Errorf("INVARIANT VIOLATION: WinRate out of range [0, 100]: %f", sum.WinRate)
					}
					for plID, ps := range sum.Playlists {
						if ps.Wins+ps.Losses > ps.MatchesPlayed {
							t.Errorf("INVARIANT VIOLATION: Playlist %s Wins (%d) + Losses (%d) > MatchesPlayed (%d)",
								plID, ps.Wins, ps.Losses, ps.MatchesPlayed)
						}
					}
				}
			}
		}(i)
	}

	// Run adversarial rapid reset concurrency for 1.5 seconds
	time.Sleep(1500 * time.Millisecond)
	close(stopCh)
	wg.Wait()

	t.Logf("Completed rapid reset stress test: %d concludes, %d resets, %d reads",
		atomic.LoadInt64(&concludesAttempted),
		atomic.LoadInt64(&resetsAttempted),
		atomic.LoadInt64(&readsAttempted),
	)

	// Post-stress validation: final Reset should cleanly leave 0 matches and 0 tallies
	finalSummary := s.Reset()
	if finalSummary.TotalMatches != 0 || finalSummary.TotalWins != 0 || finalSummary.TotalLosses != 0 {
		t.Fatalf("Post-stress Reset failed to clear state: matches=%d, wins=%d, losses=%d",
			finalSummary.TotalMatches, finalSummary.TotalWins, finalSummary.TotalLosses)
	}
	if len(finalSummary.Matches) != 0 || len(finalSummary.Playlists) != 0 {
		t.Fatalf("Post-stress Reset failed to clear slices/maps: matches=%d, playlists=%d",
			len(finalSummary.Matches), len(finalSummary.Playlists))
	}
}

// ---------------------------------------------------------------------------
// 5. Direct mutation attacks on returned GetSessionSummary pointers and maps
// ---------------------------------------------------------------------------
func TestChallenger_DirectMutationAttacks(t *testing.T) {
	s := NewSessionTracker()

	// Play a match to populate state
	mActive := createChallengerMockMatch("mut-guid-1", 11, "", 1050.0, 2, 1)
	s.RecordActiveMatch(mActive)
	mConcluded := createChallengerMockMatch("mut-guid-1", 11, "victory", 1059.25, 4, 2)
	s.ConcludeMatch(mConcluded)

	// Also start a second active match
	mActive2 := createChallengerMockMatch("mut-guid-2", 13, "", 1200.0, 1, 0)
	s.RecordActiveMatch(mActive2)

	// Fetch snapshot 1
	snap1 := s.GetSessionSummary()

	// Execute adversarial mutations directly on snap1:
	// 1. Primitive fields
	snap1.SessionID = "MALICIOUS_UUID"
	snap1.TotalMatches = -9999
	snap1.TotalWins = -9999
	snap1.TotalLosses = -9999
	snap1.WinRate = -666.6

	// 2. Playlists map
	if p11, ok := snap1.Playlists["11"]; ok {
		p11.PlaylistName = "MUTATED_PLAYLIST"
		p11.MatchesPlayed = 8888
		p11.Wins = 8888
		p11.Losses = 8888
		p11.WinRate = 999.9
		p11.InitialMMR = 9999.0
		p11.CurrentMMR = 9999.0
		p11.MMRDelta = 9999.0
	}
	delete(snap1.Playlists, "11")
	snap1.Playlists["evil_key"] = &PlaylistSessionStats{PlaylistName: "EVIL"}

	// 3. Matches slice & internal match pointers
	if len(snap1.Matches) > 0 {
		m := snap1.Matches[0]
		m.MatchGUID = "CORRUPTED_GUID"
		m.PlaylistName = "CORRUPTED_NAME"
		m.Result = "CORRUPTED_RESULT"
		m.BlueScore = 999
		m.OrangeScore = 999
		m.StartingMMR = 9999.9
		m.EndingMMR = 9999.9
		m.MMRChange = 9999.9

		if m.LocalTeam != nil {
			*m.LocalTeam = 888
		}
		if m.WinnerTeam != nil {
			*m.WinnerTeam = 888
		}
		if len(m.Players) > 0 {
			m.Players[0].Name = "HACKED_PLAYER"
			m.Players[0].MMR = 99999.0
			if m.Players[0].MatchupRecord != nil {
				m.Players[0].MatchupRecord.WinsAsTeammate = 99999
			}
			m.Players = nil // Erase players slice on snap1
		}
	}
	snap1.Matches = nil // Erase matches slice on snap1

	// 4. ActiveMatch pointers and slices
	if snap1.ActiveMatch != nil {
		snap1.ActiveMatch.MatchGUID = "PWNED_ACTIVE_GUID"
		snap1.ActiveMatch.PlaylistName = "PWNED_PLAYLIST"
		if snap1.ActiveMatch.LocalTeam != nil {
			*snap1.ActiveMatch.LocalTeam = 777
		}
		if snap1.ActiveMatch.LocalPlayer != nil {
			snap1.ActiveMatch.LocalPlayer.Name = "PWNED_LOCAL_NAME"
			if snap1.ActiveMatch.LocalPlayer.CurrentRank != nil {
				snap1.ActiveMatch.LocalPlayer.CurrentRank.MMR = 88888.0
			}
		}
		snap1.ActiveMatch.Teammates = nil
		snap1.ActiveMatch.Opponents = nil
	}

	// Fetch snapshot 2 from session tracker: MUST be completely pristine and unaffected!
	snap2 := s.GetSessionSummary()

	if snap2.SessionID == "MALICIOUS_UUID" {
		t.Error("CRITICAL: SessionID was corrupted by external mutation!")
	}
	if snap2.TotalMatches != 1 {
		t.Errorf("CRITICAL: TotalMatches was corrupted! Expected 1, got %d", snap2.TotalMatches)
	}
	if snap2.TotalWins != 1 {
		t.Errorf("CRITICAL: TotalWins was corrupted! Expected 1, got %d", snap2.TotalWins)
	}
	if snap2.TotalLosses != 0 {
		t.Errorf("CRITICAL: TotalLosses was corrupted! Expected 0, got %d", snap2.TotalLosses)
	}
	if snap2.WinRate != 100.0 {
		t.Errorf("CRITICAL: WinRate was corrupted! Expected 100.0, got %f", snap2.WinRate)
	}

	// Verify Playlists map immunity
	if _, evilExists := snap2.Playlists["evil_key"]; evilExists {
		t.Error("CRITICAL: External key injection corrupted internal playlists map!")
	}
	p11Clean, ok := snap2.Playlists["11"]
	if !ok {
		t.Fatal("CRITICAL: External delete removed playlist '11' from internal tracker!")
	}
	if p11Clean.PlaylistName == "MUTATED_PLAYLIST" {
		t.Error("CRITICAL: PlaylistName was corrupted by mutation!")
	}
	if p11Clean.Wins != 1 || p11Clean.MatchesPlayed != 1 {
		t.Errorf("CRITICAL: Playlist tallies corrupted! Expected played=1, wins=1, got played=%d, wins=%d",
			p11Clean.MatchesPlayed, p11Clean.Wins)
	}
	if p11Clean.MMRDelta != 9.25 {
		t.Errorf("CRITICAL: Playlist MMRDelta corrupted! Expected 9.25, got %f", p11Clean.MMRDelta)
	}

	// Verify Matches history immunity
	if len(snap2.Matches) != 1 {
		t.Fatalf("CRITICAL: Matches history corrupted! Expected 1, got %d", len(snap2.Matches))
	}
	mClean := snap2.Matches[0]
	if mClean.MatchGUID != "mut-guid-1" {
		t.Errorf("CRITICAL: MatchGUID corrupted! Expected 'mut-guid-1', got %s", mClean.MatchGUID)
	}
	if mClean.Result != "victory" {
		t.Errorf("CRITICAL: Result corrupted! Expected 'victory', got %s", mClean.Result)
	}
	if mClean.BlueScore != 5 {
		t.Errorf("CRITICAL: BlueScore corrupted! Expected 5, got %d", mClean.BlueScore)
	}
	if mClean.LocalTeam == nil || *mClean.LocalTeam != 0 {
		t.Errorf("CRITICAL: LocalTeam pointer corrupted! Expected 0, got %v", mClean.LocalTeam)
	}
	if mClean.WinnerTeam == nil || *mClean.WinnerTeam != 0 {
		t.Errorf("CRITICAL: WinnerTeam pointer corrupted! Expected 0, got %v", mClean.WinnerTeam)
	}
	if len(mClean.Players) == 0 {
		t.Fatal("CRITICAL: Players slice was deleted by external mutation!")
	}
	if mClean.Players[0].Name == "HACKED_PLAYER" {
		t.Error("CRITICAL: Player name was corrupted by external mutation!")
	}
	if mClean.Players[0].MMR == 99999.0 {
		t.Error("CRITICAL: Player MMR was corrupted by external mutation!")
	}

	// Verify ActiveMatch immunity
	if snap2.ActiveMatch == nil {
		t.Fatal("CRITICAL: ActiveMatch was nil on snap2")
	}
	if snap2.ActiveMatch.MatchGUID != "mut-guid-2" {
		t.Errorf("CRITICAL: ActiveMatch GUID corrupted! Expected 'mut-guid-2', got %s", snap2.ActiveMatch.MatchGUID)
	}
	if snap2.ActiveMatch.LocalTeam == nil || *snap2.ActiveMatch.LocalTeam != 0 {
		t.Errorf("CRITICAL: ActiveMatch LocalTeam pointer corrupted! Expected 0, got %v", snap2.ActiveMatch.LocalTeam)
	}
	if snap2.ActiveMatch.LocalPlayer == nil || snap2.ActiveMatch.LocalPlayer.Name != "ChallengerLocal" {
		t.Errorf("CRITICAL: ActiveMatch LocalPlayer corrupted! Expected 'ChallengerLocal', got %v",
			snap2.ActiveMatch.LocalPlayer)
	}
	if len(snap2.ActiveMatch.Teammates) == 0 {
		t.Error("CRITICAL: ActiveMatch Teammates slice was deleted by external mutation!")
	}
}

// ---------------------------------------------------------------------------
// 6. Adversarial Corner-Case: Concluding non-active match does NOT corrupt active match
// ---------------------------------------------------------------------------
func TestChallenger_OutOfOrderConclusion_ActiveMatchPreservation(t *testing.T) {
	s := NewSessionTracker()

	// 1. Start active match M1
	m1 := createChallengerMockMatch("active-m1", 11, "", 1050.0, 1, 0)
	s.RecordActiveMatch(m1)

	// 2. An out-of-order conclusion arrives for a PREVIOUS match M0 (different GUID)
	m0 := createChallengerMockMatch("prev-m0", 13, "victory", 1200.0, 3, 2)
	s.ConcludeMatch(m0)

	// 3. Verify that M1 is STILL active in session summary!
	summary := s.GetSessionSummary()
	if summary.ActiveMatch == nil {
		t.Fatalf("VULNERABILITY DETECTED: Concluding non-matching GUID cleared active match state!")
	}
	if summary.ActiveMatch.MatchGUID != "active-m1" {
		t.Errorf("ActiveMatch GUID corrupted: expected 'active-m1', got %s", summary.ActiveMatch.MatchGUID)
	}

	// 4. Now conclude M1
	m1End := createChallengerMockMatch("active-m1", 11, "victory", 1059.0, 3, 1)
	s.ConcludeMatch(m1End)

	summaryAfter := s.GetSessionSummary()
	if summaryAfter.ActiveMatch != nil {
		t.Errorf("ActiveMatch should be nil after M1 concludes, got %v", summaryAfter.ActiveMatch)
	}
	if summaryAfter.TotalMatches != 2 {
		t.Errorf("Expected 2 completed matches, got %d", summaryAfter.TotalMatches)
	}
}

// ---------------------------------------------------------------------------
// 7. Adversarial Corner-Case: Delayed RecordActiveMatch after ConcludeMatch
// ---------------------------------------------------------------------------
func TestChallenger_DelayedActiveMatchAfterConclusion(t *testing.T) {
	s := NewSessionTracker()

	m := createChallengerMockMatch("guid-ordered", 11, "victory", 1050.0, 3, 1)
	s.RecordActiveMatch(m)
	s.ConcludeMatch(m)

	// Verify match concluded cleanly
	s1 := s.GetSessionSummary()
	if s1.ActiveMatch != nil {
		t.Fatal("ActiveMatch should be nil after conclusion")
	}

	// Now a delayed/replayed RecordActiveMatch arrives for the same GUID:
	s.RecordActiveMatch(m)

	// Check if the concluded match was resurrected as active:
	s2 := s.GetSessionSummary()
	if s2.ActiveMatch != nil && s2.ActiveMatch.MatchGUID == "guid-ordered" {
		t.Errorf("VULNERABILITY DETECTED: RecordActiveMatch revived already-concluded match GUID as active!")
	}
}
