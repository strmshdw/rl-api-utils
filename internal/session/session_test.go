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
