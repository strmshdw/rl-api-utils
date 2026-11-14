package session

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
)

func TestSessionTracker_Reset_DuringActiveMatch(t *testing.T) {
	s := NewSessionTracker()

	// 1. Record active match in Playlist 13 with LocalPlayer MMR 1050.0
	m := createMockMatch("active-m", 13, "", 1050.0, 1, 0)
	s.RecordActiveMatch(m)

	// 2. Call Reset while match is in progress
	s.Reset()

	summaryMid := s.GetSessionSummary()
	pMid := summaryMid.Playlists["13"]
	if pMid == nil {
		t.Fatal("expected playlist '13' to be re-anchored on active match reset")
	}
	if pMid.InitialMMR != 1050.0 || pMid.CurrentMMR != 1050.0 || pMid.MMRDelta != 0.0 {
		t.Errorf("expected re-anchored MMR 1050.0, got initial=%f, current=%f, delta=%f",
			pMid.InitialMMR, pMid.CurrentMMR, pMid.MMRDelta)
	}
	if pMid.MatchesPlayed != 0 {
		t.Errorf("expected 0 matches played mid-match, got %d", pMid.MatchesPlayed)
	}

	// 3. Conclude match with ending MMR 1059.0 (victory)
	mEnd := createMockMatch("active-m", 13, "victory", 1059.0, 3, 1)
	s.ConcludeMatch(mEnd)

	summaryFinal := s.GetSessionSummary()
	pFinal := summaryFinal.Playlists["13"]
	if pFinal.MatchesPlayed != 1 || pFinal.Wins != 1 {
		t.Errorf("expected 1 match, 1 win, got %d played, %d wins", pFinal.MatchesPlayed, pFinal.Wins)
	}
	if pFinal.MMRDelta != 9.0 {
		t.Errorf("expected MMRDelta 9.0 relative to reset baseline, got %f", pFinal.MMRDelta)
	}
	if summaryFinal.TotalMatches != 1 || summaryFinal.WinRate != 100.0 {
		t.Errorf("expected 1 total match, 100%% win rate, got %d matches, %f%%",
			summaryFinal.TotalMatches, summaryFinal.WinRate)
	}
}

func TestSessionTracker_ObserverIntegration(t *testing.T) {
	// Verify compile-time assertion
	var _ playertrack.MatchStateListener = (*SessionTracker)(nil)

	s := NewSessionTracker()

	// Use temporary SQLite store to construct playertrack.Tracker
	tempDir := t.TempDir()
	store, err := storage.NewSQLiteStore(tempDir + "/test_observer.db")
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	pt, err := playertrack.NewTracker(
		store,
		playertrack.NewNoOpRankClient(),
		config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Epic|local_hero_1|0",
		},
		config.AuthConfig{},
		playertrack.WithMatchStateListener(s),
	)
	if err != nil {
		t.Fatalf("failed to create playertrack.Tracker: %v", err)
	}
	defer pt.Close()

	ctx := context.Background()

	// 1. Send UpdateState to playertrack.Tracker
	players := []statsapi.StatsPlayer{
		{
			Name:      "LocalHero",
			PrimaryId: "Epic|local_hero_1|0",
			TeamNum:   0,
			Score:     100,
			Goals:     1,
		},
		{
			Name:      "Opponent",
			PrimaryId: "Steam|opp_1|0",
			TeamNum:   1,
			Score:     50,
			Goals:     0,
		},
	}
	if err := pt.OnUpdateState(ctx, "obs-guid-1", 11, players); err != nil {
		t.Fatalf("OnUpdateState failed: %v", err)
	}

	// Verify session tracker received active match
	summary := s.GetSessionSummary()
	if summary.ActiveMatch == nil {
		t.Fatal("expected SessionTracker to receive active match from observer notification")
	}
	if summary.ActiveMatch.MatchGUID != "obs-guid-1" {
		t.Errorf("expected GUID 'obs-guid-1', got %s", summary.ActiveMatch.MatchGUID)
	}

	// 2. Conclude match in playertrack.Tracker
	winnerBlue := 0
	if err := pt.OnMatchEnded(ctx, "obs-guid-1", &winnerBlue); err != nil {
		t.Fatalf("OnMatchEnded failed: %v", err)
	}

	// Verify session tracker recorded match conclusion
	summaryAfter := s.GetSessionSummary()
	if summaryAfter.ActiveMatch != nil {
		t.Error("expected ActiveMatch to be cleared after match conclusion")
	}
	if summaryAfter.TotalMatches != 1 || summaryAfter.TotalWins != 1 {
		t.Errorf("expected 1 match and 1 win in session tracker, got %d matches, %d wins",
			summaryAfter.TotalMatches, summaryAfter.TotalWins)
	}
}

func TestSessionTracker_ConcurrencyStress(t *testing.T) {
	s := NewSessionTracker()

	stopCh := make(chan struct{})
	var wg sync.WaitGroup

	playlists := []int{11, 13, 27}

	// 15 Goroutines recording active matches
	for i := range 15 {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(workerID)))
			for {
				select {
				case <-stopCh:
					return
				default:
					pl := playlists[rng.Intn(len(playlists))]
					guid := fmt.Sprintf("stress-guid-%d", rng.Intn(20))
					mmr := 1000.0 + float64(rng.Intn(200))
					m := createMockMatch(guid, pl, "", mmr, rng.Intn(5), rng.Intn(5))
					s.RecordActiveMatch(m)
					time.Sleep(time.Duration(rng.Intn(2)) * time.Millisecond)
				}
			}
		}(i)
	}

	// 10 Goroutines concluding matches
	for i := range 10 {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(workerID+100)))
			results := []string{"victory", "defeat", "draw"}
			for {
				select {
				case <-stopCh:
					return
				default:
					pl := playlists[rng.Intn(len(playlists))]
					guid := fmt.Sprintf("stress-guid-%d", rng.Intn(20))
					res := results[rng.Intn(len(results))]
					mmr := 1000.0 + float64(rng.Intn(200))
					m := createMockMatch(guid, pl, res, mmr, rng.Intn(5), rng.Intn(5))
					s.ConcludeMatch(m)
					time.Sleep(time.Duration(rng.Intn(3)) * time.Millisecond)
				}
			}
		}(i)
	}

	// 5 Goroutines calling Reset
	for i := range 5 {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(workerID+200)))
			for {
				select {
				case <-stopCh:
					return
				default:
					time.Sleep(time.Duration(50+rng.Intn(50)) * time.Millisecond)
					s.Reset()
				}
			}
		}(i)
	}

	// 10 Goroutines reading session summary
	for i := range 10 {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for {
				select {
				case <-stopCh:
					return
				default:
					summary := s.GetSessionSummary()
					// Read all nested structures to test deep clone and concurrent access
					_ = summary.SessionID
					_ = summary.TotalWins
					_ = summary.WinRate
					for _, p := range summary.Playlists {
						_ = p.PlaylistName
						_ = p.MMRDelta
					}
					for _, m := range summary.Matches {
						_ = m.MatchGUID
						_ = len(m.Players)
					}
					time.Sleep(1 * time.Millisecond)
				}
			}
		}(i)
	}

	// 10 Goroutines subscribing, reading, and unsubscribing
	for i := range 10 {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for {
				select {
				case <-stopCh:
					return
				default:
					ch, cancel := s.Subscribe()
					for range 3 {
						select {
						case <-ch:
						case <-time.After(5 * time.Millisecond):
						}
					}
					cancel()
					time.Sleep(1 * time.Millisecond)
				}
			}
		}(i)
	}

	// Run stress test for 1.5 seconds
	time.Sleep(1500 * time.Millisecond)
	close(stopCh)
	wg.Wait()
}

// ============================================================================
// Test Suite: Mid-Game Disconnect Integration & Snapshots (Requirement R2)
// ============================================================================

// TestSessionTracker_MidGameDisconnect_ActiveMatchObserverPropagation asserts that
// when a player disconnects in playertrack.Tracker, the active match state retained
// in SessionTracker.GetSessionSummary().ActiveMatch preserves the player, their stats,
// and sets IsDisconnected=true across all 4 lifecycle frames.
