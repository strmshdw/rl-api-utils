package session

import (
	"context"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
)

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
