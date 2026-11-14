package session

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

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
