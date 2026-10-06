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
	"github.com/google/uuid"
)

// Helper to construct a mock CurrentMatchResponse for testing.
func createMockMatch(guid string, playlistID int, result string, localMMR float64, localGoals, oppGoals int) *playertrack.CurrentMatchResponse {
	localTeam := 0
	winnerTeam := 0
	if result == "defeat" {
		winnerTeam = 1
	}

	var currentRank *playertrack.PlayerPlaylistRank
	if localMMR > 0 {
		currentRank = &playertrack.PlayerPlaylistRank{
			PlaylistID: playlistID,
			RankName:   "Champion I Division II",
			Tier:       13,
			Division:   1,
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
				Goals:   localGoals,
				Score:   localGoals * 100,
				Shots:   localGoals + 2,
				Saves:   1,
				Assists: 1,
				Demos:   0,
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

func TestSessionTracker_InitialState(t *testing.T) {
	s := NewSessionTracker()
	summary := s.GetSessionSummary()

	if summary.TotalMatches != 0 {
		t.Errorf("expected TotalMatches 0, got %d", summary.TotalMatches)
	}
	if summary.TotalWins != 0 {
		t.Errorf("expected TotalWins 0, got %d", summary.TotalWins)
	}
	if summary.TotalLosses != 0 {
		t.Errorf("expected TotalLosses 0, got %d", summary.TotalLosses)
	}
	if summary.WinRate != 0.0 {
		t.Errorf("expected WinRate 0.0, got %f", summary.WinRate)
	}
	if summary.Playlists == nil || len(summary.Playlists) != 0 {
		t.Errorf("expected empty non-nil playlists map, got %v", summary.Playlists)
	}
	if summary.Matches == nil || len(summary.Matches) != 0 {
		t.Errorf("expected empty non-nil matches slice, got %v", summary.Matches)
	}
	if summary.ActiveMatch != nil {
		t.Errorf("expected nil ActiveMatch, got %v", summary.ActiveMatch)
	}
	if _, err := uuid.Parse(summary.SessionID); err != nil {
		t.Errorf("expected valid UUID for SessionID, got %s: %v", summary.SessionID, err)
	}
	if time.Since(summary.StartedAt) > 2*time.Second {
		t.Errorf("expected StartedAt to be recent, got %v", summary.StartedAt)
	}
	if summary.Uptime == "" {
		t.Error("expected non-empty uptime string")
	}
}

func TestSessionTracker_SingleMatchRecording(t *testing.T) {
	s := NewSessionTracker()

	match := createMockMatch("guid-active-1", 13, "", 1045.5, 2, 1)
	s.RecordActiveMatch(match)

	summary := s.GetSessionSummary()
	if summary.ActiveMatch == nil {
		t.Fatal("expected ActiveMatch to be populated, got nil")
	}
	if summary.ActiveMatch.MatchGUID != "guid-active-1" {
		t.Errorf("expected GUID 'guid-active-1', got %s", summary.ActiveMatch.MatchGUID)
	}

	pStats, ok := summary.Playlists["13"]
	if !ok {
		t.Fatal("expected playlist '13' in playlists map")
	}
	if pStats.InitialMMR != 1045.5 {
		t.Errorf("expected InitialMMR 1045.5, got %f", pStats.InitialMMR)
	}
	if pStats.CurrentMMR != 1045.5 {
		t.Errorf("expected CurrentMMR 1045.5, got %f", pStats.CurrentMMR)
	}
	if pStats.MMRDelta != 0.0 {
		t.Errorf("expected MMRDelta 0.0, got %f", pStats.MMRDelta)
	}
	if pStats.MatchesPlayed != 0 {
		t.Errorf("expected MatchesPlayed 0 before conclusion, got %d", pStats.MatchesPlayed)
	}
}

func TestSessionTracker_VictoryDefeatWinRateMath(t *testing.T) {
	s := NewSessionTracker()

	// 1. Victory
	m1 := createMockMatch("m1", 11, "victory", 1000.0, 3, 1)
	s.RecordActiveMatch(m1)
	s.ConcludeMatch(m1)

	s1 := s.GetSessionSummary()
	if s1.TotalMatches != 1 || s1.TotalWins != 1 || s1.TotalLosses != 0 {
		t.Fatalf("M1: expected 1 match, 1 win, 0 loss, got %d/%d/%d", s1.TotalMatches, s1.TotalWins, s1.TotalLosses)
	}
	if s1.WinRate != 100.0 {
		t.Errorf("M1: expected win rate 100.0%%, got %f", s1.WinRate)
	}

	// 2. Defeat
	m2 := createMockMatch("m2", 11, "defeat", 991.0, 1, 2)
	s.RecordActiveMatch(m2)
	s.ConcludeMatch(m2)

	s2 := s.GetSessionSummary()
	if s2.TotalMatches != 2 || s2.TotalWins != 1 || s2.TotalLosses != 1 {
		t.Fatalf("M2: expected 2 matches, 1 win, 1 loss, got %d/%d/%d", s2.TotalMatches, s2.TotalWins, s2.TotalLosses)
	}
	if s2.WinRate != 50.0 {
		t.Errorf("M2: expected win rate 50.0%%, got %f", s2.WinRate)
	}

	// 3. Victory -> 2 wins out of 3 matches = 66.666...% -> 66.7%
	m3 := createMockMatch("m3", 11, "victory", 1000.5, 4, 2)
	s.RecordActiveMatch(m3)
	s.ConcludeMatch(m3)

	s3 := s.GetSessionSummary()
	if s3.TotalMatches != 3 || s3.TotalWins != 2 || s3.TotalLosses != 1 {
		t.Fatalf("M3: expected 3 matches, 2 wins, 1 loss, got %d/%d/%d", s3.TotalMatches, s3.TotalWins, s3.TotalLosses)
	}
	if s3.WinRate != 66.7 {
		t.Errorf("M3: expected win rate 66.7%%, got %f", s3.WinRate)
	}

	// 4. Draw / Unresolved -> 4 matches total, 2 wins -> 50.0%
	m4 := createMockMatch("m4", 11, "draw", 1000.5, 2, 2)
	m4.WinnerTeam = nil
	s.RecordActiveMatch(m4)
	s.ConcludeMatch(m4)

	s4 := s.GetSessionSummary()
	if s4.TotalMatches != 4 || s4.TotalWins != 2 || s4.TotalLosses != 1 {
		t.Fatalf("M4: expected 4 matches, 2 wins, 1 loss, got %d/%d/%d", s4.TotalMatches, s4.TotalWins, s4.TotalLosses)
	}
	if s4.WinRate != 50.0 {
		t.Errorf("M4: expected win rate 50.0%%, got %f", s4.WinRate)
	}
}

func TestSessionTracker_MMRDeltaProgression(t *testing.T) {
	s := NewSessionTracker()

	// Initial match starts at 1000.0 MMR
	m1Start := createMockMatch("m1", 11, "", 1000.0, 0, 0)
	s.RecordActiveMatch(m1Start)

	// Match 1 ends with 1009.25 MMR (+9.25)
	m1End := createMockMatch("m1", 11, "victory", 1009.25, 3, 1)
	s.ConcludeMatch(m1End)

	s1 := s.GetSessionSummary()
	p1 := s1.Playlists["11"]
	if p1.InitialMMR != 1000.0 {
		t.Errorf("P1 InitialMMR expected 1000.0, got %f", p1.InitialMMR)
	}
	if p1.CurrentMMR != 1009.25 {
		t.Errorf("P1 CurrentMMR expected 1009.25, got %f", p1.CurrentMMR)
	}
	if p1.MMRDelta != 9.25 {
		t.Errorf("P1 MMRDelta expected 9.25, got %f", p1.MMRDelta)
	}
	if len(s1.Matches) != 1 || s1.Matches[0].MMRChange != 9.25 {
		t.Errorf("M1 MMRChange expected 9.25, got %f", s1.Matches[0].MMRChange)
	}

	// Match 2 starts with 1009.25, ends with 1001.50 (-7.75)
	m2Start := createMockMatch("m2", 11, "", 1009.25, 0, 0)
	s.RecordActiveMatch(m2Start)
	m2End := createMockMatch("m2", 11, "defeat", 1001.50, 1, 3)
	s.ConcludeMatch(m2End)

	s2 := s.GetSessionSummary()
	p2 := s2.Playlists["11"]
	if p2.InitialMMR != 1000.0 {
		t.Errorf("P2 InitialMMR expected 1000.0, got %f", p2.InitialMMR)
	}
	if p2.CurrentMMR != 1001.50 {
		t.Errorf("P2 CurrentMMR expected 1001.50, got %f", p2.CurrentMMR)
	}
	// Net delta: 1001.50 - 1000.0 = +1.50
	if p2.MMRDelta != 1.50 {
		t.Errorf("P2 MMRDelta expected 1.50, got %f", p2.MMRDelta)
	}
	if len(s2.Matches) != 2 || s2.Matches[1].MMRChange != -7.75 {
		t.Errorf("M2 MMRChange expected -7.75, got %f", s2.Matches[1].MMRChange)
	}

	// Match 3 starts with 1001.50, ends with 1012.80 (+11.30)
	m3Start := createMockMatch("m3", 11, "", 1001.50, 0, 0)
	s.RecordActiveMatch(m3Start)
	m3End := createMockMatch("m3", 11, "victory", 1012.80, 5, 2)
	s.ConcludeMatch(m3End)

	s3 := s.GetSessionSummary()
	p3 := s3.Playlists["11"]
	// Net delta: 1012.80 - 1000.0 = +12.80
	if p3.MMRDelta != 12.80 {
		t.Errorf("P3 MMRDelta expected 12.80, got %f", p3.MMRDelta)
	}
	if len(s3.Matches) != 3 || s3.Matches[2].MMRChange != 11.30 {
		t.Errorf("M3 MMRChange expected 11.30, got %f", s3.Matches[2].MMRChange)
	}
}

func TestSessionTracker_CasualPlaylistHandling(t *testing.T) {
	s := NewSessionTracker()

	// Casual 2v2 (playlist 2) with 0.0 MMR
	m := createMockMatch("casual-1", 2, "victory", 0.0, 2, 1)
	s.RecordActiveMatch(m)
	s.ConcludeMatch(m)

	summary := s.GetSessionSummary()
	p := summary.Playlists["2"]
	if p == nil {
		t.Fatal("expected playlist '2' to be tracked")
	}

	if p.InitialMMR != 0.0 || p.CurrentMMR != 0.0 || p.MMRDelta != 0.0 {
		t.Errorf("expected 0.0 MMR values for casual, got initial=%f, curr=%f, delta=%f",
			p.InitialMMR, p.CurrentMMR, p.MMRDelta)
	}
	if p.MatchesPlayed != 1 || p.Wins != 1 || p.WinRate != 100.0 {
		t.Errorf("expected casual W/L to track properly, got played=%d, wins=%d, rate=%f",
			p.MatchesPlayed, p.Wins, p.WinRate)
	}

	matchDetail := summary.Matches[0]
	if matchDetail.StartingMMR != 0.0 || matchDetail.EndingMMR != 0.0 || matchDetail.MMRChange != 0.0 {
		t.Errorf("expected 0.0 MMR on match detail, got start=%f, end=%f, change=%f",
			matchDetail.StartingMMR, matchDetail.EndingMMR, matchDetail.MMRChange)
	}
}

func TestSessionTracker_Idempotency(t *testing.T) {
	s := NewSessionTracker()

	m := createMockMatch("guid-dup", 13, "victory", 1050.0, 3, 1)
	s.RecordActiveMatch(m)
	s.ConcludeMatch(m)
	s.ConcludeMatch(m) // Duplicate invocation

	summary := s.GetSessionSummary()
	if summary.TotalMatches != 1 {
		t.Errorf("expected 1 match after duplicate conclude, got %d", summary.TotalMatches)
	}
	if summary.TotalWins != 1 {
		t.Errorf("expected 1 win after duplicate conclude, got %d", summary.TotalWins)
	}
	if len(summary.Matches) != 1 {
		t.Errorf("expected 1 match in history, got %d", len(summary.Matches))
	}
	if summary.Playlists["13"].MatchesPlayed != 1 {
		t.Errorf("expected 1 playlist match played, got %d", summary.Playlists["13"].MatchesPlayed)
	}
}

func TestSessionTracker_DeepClone_Immunity(t *testing.T) {
	s := NewSessionTracker()

	m := createMockMatch("m-clone", 13, "victory", 1050.0, 3, 1)
	s.RecordActiveMatch(m)
	s.ConcludeMatch(m)

	s1 := s.GetSessionSummary()
	// Mutate returned copy
	s1.TotalWins = 9999
	s1.Playlists["13"].Wins = 9999
	s1.Playlists["13"].PlaylistName = "Mutated"
	s1.Matches[0].Result = "mutated"
	s1.Matches[0].Players[0].Name = "Hacker"

	s2 := s.GetSessionSummary()
	if s2.TotalWins == 9999 {
		t.Error("mutation of s1.TotalWins affected internal tracker state")
	}
	if s2.Playlists["13"].Wins == 9999 {
		t.Error("mutation of s1.Playlists affected internal tracker state")
	}
	if s2.Playlists["13"].PlaylistName == "Mutated" {
		t.Error("mutation of s1.Playlists name affected internal tracker state")
	}
	if s2.Matches[0].Result == "mutated" {
		t.Error("mutation of s1.Matches[0].Result affected internal tracker state")
	}
	if s2.Matches[0].Players[0].Name == "Hacker" {
		t.Error("mutation of s1.Matches[0].Players[0].Name affected internal tracker state")
	}
}

func TestSessionTracker_Reset_ClearsStateAndPreservesSSE(t *testing.T) {
	s := NewSessionTracker()

	// 1. Subscribe client
	subCh, cancel := s.Subscribe()
	defer cancel()

	// 2. Play 2 matches
	m1 := createMockMatch("m1", 13, "victory", 1000.0, 3, 1)
	s.RecordActiveMatch(m1)
	s.ConcludeMatch(m1)

	m2 := createMockMatch("m2", 13, "defeat", 991.0, 0, 2)
	s.RecordActiveMatch(m2)
	s.ConcludeMatch(m2)

	// Drain events generated by m1 and m2
drainLoop:
	for {
		select {
		case <-subCh:
		default:
			break drainLoop
		}
	}

	oldID := s.GetSessionSummary().SessionID

	// 3. Reset session
	resetSummary := s.Reset()

	if resetSummary.SessionID == oldID {
		t.Errorf("expected new session UUID after reset, got unchanged %s", resetSummary.SessionID)
	}
	if resetSummary.TotalMatches != 0 || resetSummary.TotalWins != 0 || resetSummary.TotalLosses != 0 {
		t.Errorf("expected reset counters, got matches=%d, wins=%d, losses=%d",
			resetSummary.TotalMatches, resetSummary.TotalWins, resetSummary.TotalLosses)
	}
	if len(resetSummary.Matches) != 0 {
		t.Errorf("expected 0 matches after reset, got %d", len(resetSummary.Matches))
	}
	if len(resetSummary.Playlists) != 0 {
		t.Errorf("expected 0 playlists after reset, got %d", len(resetSummary.Playlists))
	}

	// 4. Verify subscriber receives EventSessionUpdate
	select {
	case event := <-subCh:
		if event.Event != EventSessionUpdate {
			t.Errorf("expected EventSessionUpdate, got %s", event.Event)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("subscriber did not receive session_update after Reset")
	}

	// 5. Play another match after reset and verify subscriber receives updates
	m3 := createMockMatch("m3", 13, "victory", 1000.0, 2, 0)
	s.RecordActiveMatch(m3)
	select {
	case event := <-subCh:
		if event.Event != EventMatchUpdate {
			t.Errorf("expected EventMatchUpdate post-reset, got %s", event.Event)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("subscriber did not receive match_update post-reset")
	}

	s.ConcludeMatch(m3)
	select {
	case event := <-subCh:
		if event.Event != EventMatchEnded {
			t.Errorf("expected EventMatchEnded post-reset, got %s", event.Event)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("subscriber did not receive match_ended post-reset")
	}
}

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
func TestSessionTracker_MidGameDisconnect_ActiveMatchObserverPropagation(t *testing.T) {
	s := NewSessionTracker()

	tempDir := t.TempDir()
	store, err := storage.NewSQLiteStore(tempDir + "/test_session_disc.db")
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	cfg := config.PlayerTrackingConfig{
		Enabled:       true,
		LocalPlayerID: "Steam|local_session_user|0",
	}

	pt, err := playertrack.NewTracker(
		store,
		playertrack.NewNoOpRankClient(),
		cfg,
		config.AuthConfig{},
		playertrack.WithMatchStateListener(s),
	)
	if err != nil {
		t.Fatalf("failed to create playertrack.Tracker: %v", err)
	}
	defer pt.Close()

	ctx := context.Background()
	matchGUID := "session-disc-guid-1"
	playlistID := 11

	// Frame 1: Full lobby with active players accumulating stats
	playersF1 := []statsapi.StatsPlayer{
		{
			Name:      "LocalUser",
			PrimaryId: "Steam|local_session_user|0",
			TeamNum:   0,
			Score:     200,
			Goals:     1,
			Assists:   0,
			Saves:     1,
			Shots:     3,
			Demos:     0,
		},
		{
			Name:      "Teammate1",
			PrimaryId: "Steam|session_tm_1|0",
			TeamNum:   0,
			Score:     350,
			Goals:     2,
			Assists:   1,
			Saves:     2,
			Shots:     4,
			Demos:     1,
		},
		{
			Name:      "Opponent1",
			PrimaryId: "Steam|session_opp_1|0",
			TeamNum:   1,
			Score:     180,
			Goals:     1,
			Assists:   0,
			Saves:     1,
			Shots:     2,
			Demos:     0,
		},
	}

	if err := pt.OnUpdateState(ctx, matchGUID, playlistID, playersF1); err != nil {
		t.Fatalf("Frame 1 OnUpdateState failed: %v", err)
	}

	// Verify SessionTracker ActiveMatch in Frame 1
	summaryF1 := s.GetSessionSummary()
	if summaryF1.ActiveMatch == nil {
		t.Fatal("Frame 1: expected SessionTracker to hold ActiveMatch")
	}
	if len(summaryF1.ActiveMatch.Teammates) != 1 {
		t.Fatalf("Frame 1: expected 1 teammate in session active match, got %d", len(summaryF1.ActiveMatch.Teammates))
	}
	if summaryF1.ActiveMatch.Teammates[0].IsDisconnected {
		t.Errorf("Frame 1: teammate should not be disconnected")
	}

	// Frame 2: Teammate leaves early (omitted from UpdateState)
	playersF2 := []statsapi.StatsPlayer{
		{
			Name:      "LocalUser",
			PrimaryId: "Steam|local_session_user|0",
			TeamNum:   0,
			Score:     240,
			Goals:     1,
			Assists:   0,
			Saves:     2,
			Shots:     4,
			Demos:     0,
		},
		// Teammate1 omitted!
		{
			Name:      "Opponent1",
			PrimaryId: "Steam|session_opp_1|0",
			TeamNum:   1,
			Score:     200,
			Goals:     1,
			Assists:   0,
			Saves:     2,
			Shots:     3,
			Demos:     0,
		},
	}

	if err := pt.OnUpdateState(ctx, matchGUID, playlistID, playersF2); err != nil {
		t.Fatalf("Frame 2 OnUpdateState failed: %v", err)
	}

	// ASSERTION: SessionTracker.GetSessionSummary().ActiveMatch retains disconnected player and stats
	summaryF2 := s.GetSessionSummary()
	if summaryF2.ActiveMatch == nil {
		t.Fatal("Frame 2: expected SessionTracker to retain ActiveMatch")
	}
	if len(summaryF2.ActiveMatch.Teammates) != 1 {
		t.Fatalf("Frame 2: expected 1 retained teammate in session active match, got %d", len(summaryF2.ActiveMatch.Teammates))
	}
	tmF2 := summaryF2.ActiveMatch.Teammates[0]
	if tmF2.PlayerID != "Steam|session_tm_1|0" {
		t.Errorf("Frame 2: expected teammate ID 'Steam|session_tm_1|0', got %q", tmF2.PlayerID)
	}
	if !tmF2.IsDisconnected {
		t.Errorf("Frame 2: expected IsDisconnected=true in session active match for omitted teammate")
	}
	if tmF2.Stats.Score != 350 || tmF2.Stats.Goals != 2 || tmF2.Stats.Assists != 1 || tmF2.Stats.Demos != 1 {
		t.Errorf("Frame 2: session active match teammate stats were not preserved: %+v", tmF2.Stats)
	}

	// Frame 3: Disconnected teammate reconnects -> stats update, no duplicates
	playersF3 := []statsapi.StatsPlayer{
		{
			Name:      "LocalUser",
			PrimaryId: "Steam|local_session_user|0",
			TeamNum:   0,
			Score:     240,
			Goals:     1,
			Assists:   0,
			Saves:     2,
			Shots:     4,
			Demos:     0,
		},
		{
			Name:      "Teammate1",
			PrimaryId: "Steam|session_tm_1|0",
			TeamNum:   0,
			Score:     480,
			Goals:     3,
			Assists:   1,
			Saves:     3,
			Shots:     6,
			Demos:     1,
		},
		{
			Name:      "Opponent1",
			PrimaryId: "Steam|session_opp_1|0",
			TeamNum:   1,
			Score:     210,
			Goals:     1,
			Assists:   0,
			Saves:     2,
			Shots:     3,
			Demos:     0,
		},
	}

	if err := pt.OnUpdateState(ctx, matchGUID, playlistID, playersF3); err != nil {
		t.Fatalf("Frame 3 OnUpdateState failed: %v", err)
	}

	summaryF3 := s.GetSessionSummary()
	if len(summaryF3.ActiveMatch.Teammates) != 1 {
		t.Fatalf("Frame 3: expected exactly 1 teammate in session active match after reconnect, got %d", len(summaryF3.ActiveMatch.Teammates))
	}
	tmF3 := summaryF3.ActiveMatch.Teammates[0]
	if tmF3.IsDisconnected {
		t.Errorf("Frame 3: expected IsDisconnected=false after reconnection")
	}
	if tmF3.Stats.Score != 480 || tmF3.Stats.Goals != 3 {
		t.Errorf("Frame 3: teammate stats not updated after reconnect: %+v", tmF3.Stats)
	}

	// Frame 4: Local player leaves early -> local player and local team preserved
	playersF4 := []statsapi.StatsPlayer{
		// LocalUser omitted!
		{
			Name:      "Teammate1",
			PrimaryId: "Steam|session_tm_1|0",
			TeamNum:   0,
			Score:     480,
			Goals:     3,
			Assists:   1,
			Saves:     3,
			Shots:     6,
			Demos:     1,
		},
		{
			Name:      "Opponent1",
			PrimaryId: "Steam|session_opp_1|0",
			TeamNum:   1,
			Score:     210,
			Goals:     1,
			Assists:   0,
			Saves:     2,
			Shots:     3,
			Demos:     0,
		},
	}

	if err := pt.OnUpdateState(ctx, matchGUID, playlistID, playersF4); err != nil {
		t.Fatalf("Frame 4 OnUpdateState failed: %v", err)
	}

	summaryF4 := s.GetSessionSummary()
	if summaryF4.ActiveMatch.LocalPlayer == nil {
		t.Fatal("Frame 4: expected LocalPlayer to be retained in session active match, got nil")
	}
	if summaryF4.ActiveMatch.LocalPlayer.PlayerID != "Steam|local_session_user|0" {
		t.Errorf("Frame 4: expected LocalPlayer 'Steam|local_session_user|0', got %q", summaryF4.ActiveMatch.LocalPlayer.PlayerID)
	}
	if !summaryF4.ActiveMatch.LocalPlayer.IsDisconnected {
		t.Errorf("Frame 4: expected LocalPlayer.IsDisconnected=true in session active match")
	}
	if summaryF4.ActiveMatch.LocalTeam == nil || *summaryF4.ActiveMatch.LocalTeam != 0 {
		t.Errorf("Frame 4: expected LocalTeam preserved as 0, got %v", summaryF4.ActiveMatch.LocalTeam)
	}
}

// TestSessionTracker_MidGameDisconnect_SSEBroadcast verifies that the Server-Sent
// Events broadcaster pushes an EventMatchUpdate carrying the retained disconnected player.
func TestSessionTracker_MidGameDisconnect_SSEBroadcast(t *testing.T) {
	s := NewSessionTracker()

	tempDir := t.TempDir()
	store, err := storage.NewSQLiteStore(tempDir + "/test_session_sse.db")
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	cfg := config.PlayerTrackingConfig{
		Enabled:       true,
		LocalPlayerID: "Steam|local_user|0",
	}

	pt, err := playertrack.NewTracker(
		store,
		playertrack.NewNoOpRankClient(),
		cfg,
		config.AuthConfig{},
		playertrack.WithMatchStateListener(s),
	)
	if err != nil {
		t.Fatalf("failed to create playertrack.Tracker: %v", err)
	}
	defer pt.Close()

	ch, unsubscribe := s.Subscribe()
	defer unsubscribe()

	ctx := context.Background()
	matchGUID := "session-sse-guid-1"

	// Frame 1: Full lobby
	playersF1 := []statsapi.StatsPlayer{
		{Name: "LocalUser", PrimaryId: "Steam|local_user|0", TeamNum: 0, Score: 100, Goals: 1},
		{Name: "Teammate1", PrimaryId: "Steam|tm_1|0", TeamNum: 0, Score: 200, Goals: 1},
	}
	_ = pt.OnUpdateState(ctx, matchGUID, 11, playersF1)

	// Consume Frame 1 SSE event(s)
	select {
	case event := <-ch:
		if event.Event != EventMatchUpdate {
			t.Errorf("expected EventMatchUpdate, got %s", event.Event)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for Frame 1 SSE event")
	}
	// Drain any additional Frame 1 enrichments (e.g. matchup cache warming)
	for len(ch) > 0 {
		<-ch
	}

	// Frame 2: Teammate disconnects
	playersF2 := []statsapi.StatsPlayer{
		{Name: "LocalUser", PrimaryId: "Steam|local_user|0", TeamNum: 0, Score: 120, Goals: 1},
	}
	_ = pt.OnUpdateState(ctx, matchGUID, 11, playersF2)

	// Consume Frame 2 SSE event and verify payload
	select {
	case event := <-ch:
		if event.Event != EventMatchUpdate {
			t.Errorf("expected EventMatchUpdate, got %s", event.Event)
		}
		matchPayload, ok := event.Data.(*playertrack.CurrentMatchResponse)
		if !ok {
			t.Fatalf("expected event data to be *playertrack.CurrentMatchResponse, got %T", event.Data)
		}
		if len(matchPayload.Teammates) != 1 {
			t.Fatalf("expected 1 teammate in SSE payload, got %d", len(matchPayload.Teammates))
		}
		if !matchPayload.Teammates[0].IsDisconnected {
			t.Errorf("expected SSE payload teammate to have IsDisconnected=true")
		}
		if matchPayload.Teammates[0].Stats.Score != 200 || matchPayload.Teammates[0].Stats.Goals != 1 {
			t.Errorf("expected SSE payload teammate stats preserved, got %+v", matchPayload.Teammates[0].Stats)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for Frame 2 SSE event")
	}
}

// TestSessionTracker_MidGameDisconnect_ConcludeMatchSnapshotPreservation verifies that
// when a match finishes with a disconnected participant:
// 1. ConcludeMatch preserves the disconnected player in SessionMatchDetail.Players.
// 2. The player's IsDisconnected flag and accumulated stats are preserved.
// 3. Team goal tallies (BlueScore/OrangeScore) aggregate goals from the disconnected player.
func TestSessionTracker_MidGameDisconnect_ConcludeMatchSnapshotPreservation(t *testing.T) {
	s := NewSessionTracker()

	localTeam := 0
	winnerTeam := 0

	// Mock match snapshot where teammate disconnected after scoring 2 goals
	match := &playertrack.CurrentMatchResponse{
		ActiveMatch:  false,
		MatchEnded:   true,
		MatchGUID:    "conclude-disc-1",
		PlaylistID:   11,
		PlaylistName: "Ranked Doubles",
		LocalTeam:    &localTeam,
		WinnerTeam:   &winnerTeam,
		Result:       "victory",
		LocalPlayer: &playertrack.LobbyPlayer{
			PlayerID: "Steam|local_hero|0",
			Name:     "LocalHero",
			TeamNum:  0,
			IsLocal:  true,
			Stats: playertrack.PlayerStatsSummary{
				Score: 150,
				Goals: 1, // 1 goal by local
			},
		},
		Teammates: []playertrack.LobbyPlayer{
			{
				PlayerID:       "Steam|leaver_tm|0",
				Name:           "LeaverTm",
				TeamNum:        0,
				IsLocal:        false,
				IsDisconnected: true, // Left early
				Stats: playertrack.PlayerStatsSummary{
					Score: 250,
					Goals: 2, // 2 goals scored before leaving
				},
			},
		},
		Opponents: []playertrack.LobbyPlayer{
			{
				PlayerID: "Steam|rival|0",
				Name:     "Rival",
				TeamNum:  1,
				Stats: playertrack.PlayerStatsSummary{
					Score: 100,
					Goals: 1,
				},
			},
		},
	}

	// First set match as active so ConcludeMatch can transition cleanly
	activeMatch := match.DeepClone()
	activeMatch.ActiveMatch = true
	activeMatch.MatchEnded = false
	s.RecordActiveMatch(activeMatch)

	// Conclude match
	s.ConcludeMatch(match)

	summary := s.GetSessionSummary()
	if summary.TotalMatches != 1 || summary.TotalWins != 1 {
		t.Fatalf("expected 1 match and 1 win, got %d matches, %d wins", summary.TotalMatches, summary.TotalWins)
	}
	if len(summary.Matches) != 1 {
		t.Fatalf("expected 1 match detail in history, got %d", len(summary.Matches))
	}

	detail := summary.Matches[0]
	// Blue team scored 1 (Local) + 2 (LeaverTm) = 3 total goals
	if detail.BlueScore != 3 {
		t.Errorf("expected BlueScore=3 (including disconnected player's 2 goals), got %d", detail.BlueScore)
	}
	if detail.OrangeScore != 1 {
		t.Errorf("expected OrangeScore=1, got %d", detail.OrangeScore)
	}

	// Verify players roster contains disconnected teammate
	var leaverFound bool
	for _, p := range detail.Players {
		if p.PlayerID == "Steam|leaver_tm|0" {
			leaverFound = true
			if !p.IsDisconnected {
				t.Errorf("expected SessionMatchPlayer.IsDisconnected=true for early leaver")
			}
			if p.Won == nil || !*p.Won {
				t.Errorf("expected SessionMatchPlayer.Won=true for winning leaver")
			}
			if p.Stats.Goals != 2 || p.Stats.Score != 250 {
				t.Errorf("expected leaver stats preserved in match detail, got %+v", p.Stats)
			}
		}
	}
	if !leaverFound {
		t.Errorf("disconnected teammate not found in completed match detail players")
	}
}

// TestSessionTracker_DeepClone_PreservesDisconnect verifies that DeepClone preserves
// IsDisconnected and clones Won pointer safely.
func TestSessionTracker_DeepClone_PreservesDisconnect(t *testing.T) {
	won := true
	p := SessionMatchPlayer{
		PlayerID:       "Steam|clone_p|0",
		Name:           "ClonePlayer",
		TeamNum:        0,
		IsDisconnected: true,
		Won:            &won,
	}

	clone := p.DeepClone()
	if !clone.IsDisconnected {
		t.Errorf("expected clone to have IsDisconnected=true")
	}
	if clone.Won == nil || !*clone.Won {
		t.Errorf("expected clone to have Won=true")
	}

	// Mutate original Won
	newWon := false
	p.Won = &newWon

	if !*clone.Won {
		t.Errorf("clone Won pointer was not isolated from original mutation")
	}
}

