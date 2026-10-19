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

func TestSessionTracker_PersistenceAndContinueAcrossRestart(t *testing.T) {
	backends := []struct {
		name   string
		newStore func(t *testing.T) storage.StateStore
	}{
		{
			name: "SQLiteStore",
			newStore: func(t *testing.T) storage.StateStore {
				st, err := storage.NewSQLiteStore(t.TempDir() + "/sessions.db")
				if err != nil {
					t.Fatalf("failed to create sqlite store: %v", err)
				}
				t.Cleanup(func() { _ = st.Close() })
				return st
			},
		},
		{
			name: "JSONStore",
			newStore: func(t *testing.T) storage.StateStore {
				st, err := storage.NewJSONStore(t.TempDir() + "/sessions.json")
				if err != nil {
					t.Fatalf("failed to create json store: %v", err)
				}
				t.Cleanup(func() { _ = st.Close() })
				return st
			},
		},
	}

	for _, b := range backends {
		t.Run(b.name, func(t *testing.T) {
			store := b.newStore(t)
			baseTime := time.Date(2026, 10, 17, 18, 0, 0, 0, time.UTC)
			var mu sync.Mutex
			currTime := baseTime
			clockFn := func() time.Time {
				mu.Lock()
				defer mu.Unlock()
				return currTime
			}
			advance := func(d time.Duration) {
				mu.Lock()
				currTime = currTime.Add(d)
				mu.Unlock()
			}

			// 1. Tracker 1 plays 2 matches in playlist 11
			t1 := NewSessionTracker(
				WithStateStore(store),
				WithContinueWindow(2*time.Hour),
				WithClock(clockFn),
			)

			m1 := createMockMatch("cont-m1", 11, "victory", 1200.0, 3, 1)
			t1.RecordActiveMatch(m1)
			advance(5 * time.Minute)
			m1End := createMockMatch("cont-m1", 11, "victory", 1209.5, 3, 1)
			m1End.MatchEnded = true
			t1.ConcludeMatch(m1End)

			advance(2 * time.Minute)
			m2 := createMockMatch("cont-m2", 11, "defeat", 1209.5, 1, 2)
			t1.RecordActiveMatch(m2)
			advance(5 * time.Minute)
			m2End := createMockMatch("cont-m2", 11, "defeat", 1200.5, 1, 2)
			m2End.MatchEnded = true
			t1.ConcludeMatch(m2End)

			s1Summary := t1.GetSessionSummary()
			session1ID := s1Summary.SessionID
			if s1Summary.TotalMatches != 2 || s1Summary.TotalWins != 1 || s1Summary.TotalLosses != 1 {
				t.Fatalf("unexpected s1 summary: %+v", s1Summary)
			}

			// Verify persisted in store
			persisted1, err := store.GetSession(context.Background(), session1ID)
			if err != nil {
				t.Fatalf("expected session 1 persisted in store: %v", err)
			}
			if persisted1.TotalMatches != 2 || persisted1.TotalWins != 1 {
				t.Fatalf("unexpected persisted session 1: %+v", persisted1)
			}

			// 2. Simulate daemon restart 30 minutes later (within 2h continue window)
			advance(30 * time.Minute)
			t2 := NewSessionTracker(
				WithStateStore(store),
				WithContinueWindow(2*time.Hour),
				WithClock(clockFn),
			)

			s2Initial := t2.GetSessionSummary()
			if s2Initial.ContinuableSession == nil {
				t.Fatalf("expected ContinuableSession to be detected on t2 within 2h window")
			}
			if s2Initial.ContinuableSession.SessionID != session1ID {
				t.Fatalf("expected ContinuableSession ID %s, got %s", session1ID, s2Initial.ContinuableSession.SessionID)
			}
			if s2Initial.ContinuableSession.TotalMatches != 2 {
				t.Fatalf("expected ContinuableSession TotalMatches=2, got %d", s2Initial.ContinuableSession.TotalMatches)
			}

			// 3. Play 1 match in t2 BEFORE clicking Continue, then call ContinueSession to verify merge
			m3 := createMockMatch("cont-m3", 11, "victory", 1200.5, 4, 0)
			t2.RecordActiveMatch(m3)
			advance(5 * time.Minute)
			m3End := createMockMatch("cont-m3", 11, "victory", 1210.0, 4, 0)
			m3End.MatchEnded = true
			t2.ConcludeMatch(m3End)

			tempSession2ID := t2.GetSessionSummary().SessionID
			merged, err := t2.ContinueSession("")
			if err != nil {
				t.Fatalf("ContinueSession failed: %v", err)
			}
			if merged.SessionID != session1ID {
				t.Fatalf("expected merged SessionID %s, got %s", session1ID, merged.SessionID)
			}
			if merged.TotalMatches != 3 || merged.TotalWins != 2 || merged.TotalLosses != 1 {
				t.Fatalf("expected merged 3 matches (2W-1L), got %d (%dW-%dL)", merged.TotalMatches, merged.TotalWins, merged.TotalLosses)
			}
			p11 := merged.Playlists["11"]
			if p11 == nil {
				t.Fatalf("expected playlist 11 stats in merged session")
			}
			if p11.InitialMMR != 1200.0 || p11.CurrentMMR != 1210.0 || p11.MMRDelta != 10.0 {
				t.Fatalf("expected InitialMMR=1200, CurrentMMR=1210, MMRDelta=10, got %+v", p11)
			}

			// Verify temporary session 2 was cleaned up from store
			if _, err := store.GetSession(context.Background(), tempSession2ID); err == nil {
				t.Errorf("expected temporary session %s to be deleted after merge", tempSession2ID)
			}

			// 4. Advance 3 hours (beyond 2h continue window) and start Tracker 3
			advance(3 * time.Hour)
			t3 := NewSessionTracker(
				WithStateStore(store),
				WithContinueWindow(2*time.Hour),
				WithClock(clockFn),
			)
			s3Summary := t3.GetSessionSummary()
			if s3Summary.ContinuableSession != nil {
				t.Fatalf("expected no ContinuableSession after 3h (> 2h window), got %+v", s3Summary.ContinuableSession)
			}
			if _, err := t3.ContinueSession(session1ID); err != ErrContinueWindowExpired {
				t.Fatalf("expected ErrContinueWindowExpired when continuing after 3h, got %v", err)
			}

			// Historical session should still be retrievable via ListSessions & GetHistoricalSession
			histList, total, err := t3.ListSessions(context.Background(), 10, 0)
			if err != nil || total != 1 || len(histList) != 1 {
				t.Fatalf("expected 1 historical session in ListSessions, got total=%d, err=%v", total, err)
			}
			if histList[0].CanContinue {
				t.Errorf("expected CanContinue=false on expired historical session")
			}
		})
	}
}

func TestSessionTracker_AutoRotateOnIdleAndGameDisconnect(t *testing.T) {
	store, err := storage.NewSQLiteStore(t.TempDir() + "/rotate.db")
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	currTime := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clockFn := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return currTime
	}
	advance := func(d time.Duration) {
		mu.Lock()
		currTime = currTime.Add(d)
		mu.Unlock()
	}

	st := NewSessionTracker(
		WithStateStore(store),
		WithContinueWindow(1*time.Hour),
		WithClock(clockFn),
	)

	// Play 1 match
	m1 := createMockMatch("rot-m1", 13, "victory", 1300.0, 2, 1)
	st.RecordActiveMatch(m1)

	// Mid-match disconnect should NOT rotate the session while activeMatch != nil
	st.OnGameDisconnected()
	if st.GetSessionSummary().ActiveMatch == nil {
		t.Fatalf("expected ActiveMatch to remain intact on mid-match disconnect")
	}

	advance(5 * time.Minute)
	m1.MatchEnded = true
	st.ConcludeMatch(m1)
	firstSessionID := st.GetSessionSummary().SessionID

	// Now disconnect between matches: should archive firstSessionID and rotate to a new session with ContinuableSession
	advance(10 * time.Minute)
	st.OnGameDisconnected()

	afterDisc := st.GetSessionSummary()
	if afterDisc.SessionID == firstSessionID {
		t.Fatalf("expected new session ID after game disconnect")
	}
	if afterDisc.TotalMatches != 0 {
		t.Fatalf("expected 0 matches in new session after disconnect, got %d", afterDisc.TotalMatches)
	}
	if afterDisc.ContinuableSession == nil || afterDisc.ContinuableSession.SessionID != firstSessionID {
		t.Fatalf("expected ContinuableSession pointing to %s, got %+v", firstSessionID, afterDisc.ContinuableSession)
	}

	// Dismiss continuable session
	afterDismiss := st.DismissContinuableSession("")
	if afterDismiss.ContinuableSession != nil {
		t.Fatalf("expected ContinuableSession to be nil after dismiss")
	}

	// Simulate daemon restart: dismissed state must persist in StateStore
	advance(5 * time.Minute)
	stRestarted := NewSessionTracker(
		WithStateStore(store),
		WithContinueWindow(1*time.Hour),
		WithClock(clockFn),
	)
	restartedSummary := stRestarted.GetSessionSummary()
	if restartedSummary.ContinuableSession != nil {
		t.Fatalf("expected dismissed ContinuableSession to remain nil after daemon restart, got %+v", restartedSummary.ContinuableSession)
	}

	// Explicit ContinueSession by ID from Session History should still succeed while within window
	continued, err := stRestarted.ContinueSession(firstSessionID)
	if err != nil {
		t.Fatalf("expected explicit ContinueSession(%s) to succeed after dismiss, got %v", firstSessionID, err)
	}
	if continued.SessionID != firstSessionID || continued.TotalMatches != 1 {
		t.Fatalf("unexpected continued session: %+v", continued)
	}
}

func TestSessionTracker_AutoRotateAfterAbandonedMatchAndSSEBroadcast(t *testing.T) {
	store, err := storage.NewSQLiteStore(t.TempDir() + "/abandoned.db")
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	currTime := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clockFn := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return currTime
	}
	advance := func(d time.Duration) {
		mu.Lock()
		currTime = currTime.Add(d)
		mu.Unlock()
	}

	st := NewSessionTracker(
		WithStateStore(store),
		WithContinueWindow(1*time.Hour),
		WithClock(clockFn),
	)

	// 1. Conclude Match 1 in Playlist 11
	m1 := createMockMatch("m1", 11, "victory", 1200.0, 3, 1)
	st.RecordActiveMatch(m1)
	advance(5 * time.Minute)
	m1.MatchEnded = true
	st.ConcludeMatch(m1)
	s1ID := st.GetSessionSummary().SessionID

	// 2. Enter Match 2 in Playlist 13 and abandon it mid-game (no ConcludeMatch)
	advance(2 * time.Minute)
	m2Abandoned := createMockMatch("m2-abandoned", 13, "", 1350.0, 0, 1)
	st.RecordActiveMatch(m2Abandoned)

	// 3. Subscribe to SSE and advance 2 hours (> 1h continueWindow)
	ch, cancel := st.Subscribe()
	defer cancel()
	advance(2 * time.Hour)

	// 4. Start Match 3 in Playlist 11 -> must auto-rotate despite m2-abandoned having left activeMatch != nil,
	// must NOT seed m2-abandoned's Playlist 13 into the new session, and must broadcast EventSessionUpdate!
	m3 := createMockMatch("m3", 11, "", 1210.0, 1, 0)
	st.RecordActiveMatch(m3)

	afterM3 := st.GetSessionSummary()
	if afterM3.SessionID == s1ID {
		t.Fatalf("expected auto-rotation to a new session after 2h idle following abandoned match")
	}
	if afterM3.TotalMatches != 0 || afterM3.TotalWins != 0 {
		t.Fatalf("expected new session to have 0 completed matches, got %d", afterM3.TotalMatches)
	}
	if _, hasP13 := afterM3.Playlists["13"]; hasP13 {
		t.Fatalf("expected abandoned match playlist 13 not to leak into new rotated session")
	}

	var gotSessionUpdate, gotMatchUpdate bool
	for range 2 {
		select {
		case ev := <-ch:
			if ev.Event == EventSessionUpdate {
				gotSessionUpdate = true
			}
			if ev.Event == EventMatchUpdate {
				gotMatchUpdate = true
			}
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("timed out waiting for SSE events on auto-rotate (gotSessionUpdate=%v, gotMatchUpdate=%v)", gotSessionUpdate, gotMatchUpdate)
		}
	}
	if !gotSessionUpdate || !gotMatchUpdate {
		t.Fatalf("expected both EventSessionUpdate and EventMatchUpdate, got session=%v match=%v", gotSessionUpdate, gotMatchUpdate)
	}

	// 5. Conclude Match 3, then start Match 4 and quit Rocket League mid-Match-4 (OnGameDisconnected)
	advance(5 * time.Minute)
	m3.MatchEnded = true
	m3.Result = "victory"
	st.ConcludeMatch(m3)
	s2ID := st.GetSessionSummary().SessionID

	advance(1 * time.Minute)
	m4Abandoned := createMockMatch("m4-abandoned", 13, "", 1350.0, 0, 0)
	st.RecordActiveMatch(m4Abandoned)

	advance(2 * time.Minute)
	st.OnGameDisconnected()

	afterQuit := st.GetSessionSummary()
	if afterQuit.SessionID == s2ID {
		t.Fatalf("expected OnGameDisconnected to rotate completed session even when a subsequent match was abandoned on exit")
	}
	if afterQuit.ActiveMatch != nil {
		t.Fatalf("expected ActiveMatch to be nil after OnGameDisconnected rotation")
	}
	if afterQuit.ContinuableSession == nil || afterQuit.ContinuableSession.SessionID != s2ID {
		t.Fatalf("expected ContinuableSession to offer %s, got %+v", s2ID, afterQuit.ContinuableSession)
	}
}

func TestSessionTracker_MultiPlaylistMultiMatchMergeAndCarryOverWindow(t *testing.T) {
	store, err := storage.NewJSONStore(t.TempDir() + "/merge.json")
	if err != nil {
		t.Fatalf("failed to create json store: %v", err)
	}
	defer store.Close()

	currTime := time.Date(2026, 10, 17, 16, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clockFn := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return currTime
	}
	advance := func(d time.Duration) {
		mu.Lock()
		currTime = currTime.Add(d)
		mu.Unlock()
	}

	t1 := NewSessionTracker(
		WithStateStore(store),
		WithContinueWindow(1*time.Hour),
		WithClock(clockFn),
	)

	// Session 1: 1 match in Playlist 11 (1000 -> 1010) and 1 match in Playlist 13 (1100 -> 1090)
	s1m1 := createMockMatch("s1-m1", 11, "victory", 1000.0, 3, 1)
	t1.RecordActiveMatch(s1m1)
	advance(5 * time.Minute)
	s1m1End := createMockMatch("s1-m1", 11, "victory", 1010.0, 3, 1)
	s1m1End.MatchEnded = true
	t1.ConcludeMatch(s1m1End)

	advance(2 * time.Minute)
	s1m2 := createMockMatch("s1-m2", 13, "defeat", 1100.0, 1, 2)
	t1.RecordActiveMatch(s1m2)
	advance(5 * time.Minute)
	s1m2End := createMockMatch("s1-m2", 13, "defeat", 1090.0, 1, 2)
	s1m2End.MatchEnded = true
	t1.ConcludeMatch(s1m2End)

	session1ID := t1.GetSessionSummary().SessionID

	// Restart 45 minutes after Session 1's last match (within 1h window)
	advance(45 * time.Minute)
	t2 := NewSessionTracker(
		WithStateStore(store),
		WithContinueWindow(1*time.Hour),
		WithClock(clockFn),
	)

	// Play Match 1 in Session 2 (Playlist 11) where StartingMMR was 0 (rank fetch arrived only at match end = 1020.0)
	s2m1 := createMockMatch("s2-m1", 11, "victory", 0.0, 2, 0)
	t2.RecordActiveMatch(s2m1)
	advance(6 * time.Minute)
	s2m1End := createMockMatch("s2-m1", 11, "victory", 1020.0, 2, 0)
	s2m1End.MatchEnded = true
	t2.ConcludeMatch(s2m1End)

	// Play Match 2 in Session 2 (Playlist 13: 1090 -> 1105) + advance past 1h total since Session 1 ended
	// (45m + 6m + 15m = 66m > 60m since Session 1's LastMatchAt, but Session 2's first match started at +45m <= 60m!)
	advance(9 * time.Minute)
	s2m2 := createMockMatch("s2-m2", 13, "victory", 1090.0, 4, 2)
	t2.RecordActiveMatch(s2m2)
	advance(6 * time.Minute)
	s2m2End := createMockMatch("s2-m2", 13, "victory", 1105.0, 4, 2)
	s2m2End.MatchEnded = true
	t2.ConcludeMatch(s2m2End)

	// Verify CanContinue is true in both GetSessionSummary().ContinuableSession and ListSessions
	preMerge := t2.GetSessionSummary()
	if preMerge.ContinuableSession == nil || preMerge.ContinuableSession.SessionID != session1ID {
		t.Fatalf("expected ContinuableSession %s via carry-over window, got %+v", session1ID, preMerge.ContinuableSession)
	}
	if !preMerge.ContinuableSession.CanContinue {
		t.Fatalf("expected ContinuableSession.CanContinue=true via carry-over window")
	}

	listed, _, err := t2.ListSessions(context.Background(), 10, 0)
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	var foundS1 *SessionResponse
	for _, item := range listed {
		if item.SessionID == session1ID {
			foundS1 = item
			break
		}
	}
	if foundS1 == nil || !foundS1.CanContinue {
		t.Fatalf("expected Session 1 in ListSessions to have CanContinue=true via carry-over window, got %+v", foundS1)
	}

	// Merge Session 2 (2 matches across playlists 11 & 13) into Session 1
	merged, err := t2.ContinueSession("")
	if err != nil {
		t.Fatalf("ContinueSession failed: %v", err)
	}
	if merged.TotalMatches != 4 || merged.TotalWins != 3 || merged.TotalLosses != 1 {
		t.Fatalf("expected 4 matches (3W-1L), got %d (%dW-%dL)", merged.TotalMatches, merged.TotalWins, merged.TotalLosses)
	}
	if merged.Playlists["11"].InitialMMR != 1000.0 || merged.Playlists["11"].CurrentMMR != 1020.0 || merged.Playlists["11"].MMRDelta != 20.0 {
		t.Fatalf("unexpected Playlist 11 stats after merge: %+v", merged.Playlists["11"])
	}
	if merged.Playlists["13"].InitialMMR != 1100.0 || merged.Playlists["13"].CurrentMMR != 1105.0 || merged.Playlists["13"].MMRDelta != 5.0 {
		t.Fatalf("unexpected Playlist 13 stats after merge: %+v", merged.Playlists["13"])
	}

	// Verify s2-m1 had its StartingMMR (1010.0) and MMRChange (+10.0) backfilled from Session 1's CurrentMMR
	var mergedS2M1 *SessionMatchDetail
	for _, m := range merged.Matches {
		if m.MatchGUID == "s2-m1" {
			mergedS2M1 = m
			break
		}
	}
	if mergedS2M1 == nil {
		t.Fatalf("expected s2-m1 in merged matches")
	}
	if mergedS2M1.StartingMMR != 1010.0 || mergedS2M1.MMRChange != 10.0 {
		t.Fatalf("expected s2-m1 StartingMMR=1010.0 and MMRChange=10.0 after backfill, got start=%f change=%f",
			mergedS2M1.StartingMMR, mergedS2M1.MMRChange)
	}
}

func TestSessionTracker_SetStateStoreFlushesInMemorySessions(t *testing.T) {
	st := NewSessionTracker()
	m1 := createMockMatch("flush-m1", 11, "victory", 1000.0, 2, 1)
	st.RecordActiveMatch(m1)
	m1.MatchEnded = true
	st.ConcludeMatch(m1)
	firstID := st.GetSessionSummary().SessionID

	st.Reset()

	m2 := createMockMatch("flush-m2", 13, "defeat", 1100.0, 1, 3)
	st.RecordActiveMatch(m2)
	m2.MatchEnded = true
	st.ConcludeMatch(m2)
	secondID := st.GetSessionSummary().SessionID

	store, err := storage.NewSQLiteStore(t.TempDir() + "/late_attach.db")
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	st.SetStateStore(store)

	sessions, total, err := st.ListSessions(context.Background(), 10, 0)
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if total != 2 || len(sessions) != 2 {
		t.Fatalf("expected 2 flushed sessions in store after SetStateStore, got total=%d len=%d", total, len(sessions))
	}
	seen := map[string]bool{sessions[0].SessionID: true, sessions[1].SessionID: true}
	if !seen[firstID] || !seen[secondID] {
		t.Fatalf("expected both %s and %s in store, got %v", firstID, secondID, seen)
	}
}



