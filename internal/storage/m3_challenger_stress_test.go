package storage_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/storage"
)

func newChallengerStressStore(t *testing.T, isJSON bool) storage.StateStore {
	t.Helper()
	tempDir := t.TempDir()
	var dbPath string
	if isJSON {
		dbPath = filepath.Join(tempDir, "chal_stress.json")
	} else {
		dbPath = filepath.Join(tempDir, "chal_stress.db")
	}

	store, err := storage.NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create challenger store (isJSON=%v): %v", isJSON, err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

// TestChallenger_OutcomeClassification_WinnerTeamUnset verifies that when winner_team = -1,
// match score comparisons relative to local_team dictate victory/defeat and NEVER incorrectly resolve to draw.
func TestChallenger_OutcomeClassification_WinnerTeamUnset(t *testing.T) {
	backends := []struct {
		name   string
		isJSON bool
	}{
		{"SQLite", false},
		{"JSONStore", true},
	}

	for _, b := range backends {
		t.Run(b.name, func(t *testing.T) {
			store := newChallengerStressStore(t, b.isJSON)
			ctx := context.Background()
			now := time.Now().UTC()

			localPID := "Steam|76561198000000088|0"
			_ = store.SaveAuthState(ctx, "steam", "auth-token", localPID, "LocalTester")

			wtUnset := -1

			// Match 1: Blue 3 - Orange 1, winner_team = -1. Local on Blue (team 0).
			// MUST be VICTORY for local player.
			guidBlueWin := "m-blue-win-unset-winner"
			err := store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
				MatchGUID:   guidBlueWin,
				Playlist:    11,
				BlueScore:   3,
				OrangeScore: 1,
				WinnerTeam:  &wtUnset,
				Date:        now.Add(-30 * time.Minute).Format(time.RFC3339),
			})
			if err != nil {
				t.Fatalf("[%s] SyncReplayStats failed: %v", b.name, err)
			}
			err = store.SaveMatchPlayers(ctx, guidBlueWin, []*storage.MatchPlayerRecord{
				{MatchGUID: guidBlueWin, PlayerID: localPID, TeamNum: 0, IsLocal: true},
			})
			if err != nil {
				t.Fatalf("[%s] SaveMatchPlayers failed: %v", b.name, err)
			}

			// Match 2: Blue 1 - Orange 3, winner_team = -1. Local on Orange (team 1).
			// MUST be VICTORY for local player.
			guidOrangeWin := "m-orange-win-unset-winner"
			err = store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
				MatchGUID:   guidOrangeWin,
				Playlist:    11,
				BlueScore:   1,
				OrangeScore: 3,
				WinnerTeam:  &wtUnset,
				Date:        now.Add(-20 * time.Minute).Format(time.RFC3339),
			})
			if err != nil {
				t.Fatalf("[%s] SyncReplayStats failed: %v", b.name, err)
			}
			err = store.SaveMatchPlayers(ctx, guidOrangeWin, []*storage.MatchPlayerRecord{
				{MatchGUID: guidOrangeWin, PlayerID: localPID, TeamNum: 1, IsLocal: true},
			})
			if err != nil {
				t.Fatalf("[%s] SaveMatchPlayers failed: %v", b.name, err)
			}

			// Match 3: Blue 3 - Orange 1, winner_team = -1. Local on Orange (team 1).
			// MUST be DEFEAT for local player.
			guidOrangeDefeat := "m-orange-defeat-unset-winner"
			err = store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
				MatchGUID:   guidOrangeDefeat,
				Playlist:    11,
				BlueScore:   3,
				OrangeScore: 1,
				WinnerTeam:  &wtUnset,
				Date:        now.Add(-10 * time.Minute).Format(time.RFC3339),
			})
			if err != nil {
				t.Fatalf("[%s] SyncReplayStats failed: %v", b.name, err)
			}
			err = store.SaveMatchPlayers(ctx, guidOrangeDefeat, []*storage.MatchPlayerRecord{
				{MatchGUID: guidOrangeDefeat, PlayerID: localPID, TeamNum: 1, IsLocal: true},
			})
			if err != nil {
				t.Fatalf("[%s] SaveMatchPlayers failed: %v", b.name, err)
			}

			// Match 4: Blue 2 - Orange 2, winner_team = -1. Local on Blue.
			// MUST be DRAW for local player.
			guidDraw := "m-draw-unset-winner"
			err = store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
				MatchGUID:   guidDraw,
				Playlist:    11,
				BlueScore:   2,
				OrangeScore: 2,
				WinnerTeam:  &wtUnset,
				Date:        now.Format(time.RFC3339),
			})
			if err != nil {
				t.Fatalf("[%s] SyncReplayStats failed: %v", b.name, err)
			}
			err = store.SaveMatchPlayers(ctx, guidDraw, []*storage.MatchPlayerRecord{
				{MatchGUID: guidDraw, PlayerID: localPID, TeamNum: 0, IsLocal: true},
			})
			if err != nil {
				t.Fatalf("[%s] SaveMatchPlayers failed: %v", b.name, err)
			}

			// 1. Verify result=victory returns exactly Match 1 and Match 2
			victories, totalV, err := store.ListHistoricalMatches(ctx, storage.MatchFilter{Result: "victory"})
			if err != nil {
				t.Fatalf("[%s] ListHistoricalMatches(victory) failed: %v", b.name, err)
			}
			if totalV != 2 || len(victories) != 2 {
				t.Fatalf("[%s] expected 2 victories, got total=%d len=%d", b.name, totalV, len(victories))
			}
			for _, m := range victories {
				if m.Result != "victory" {
					t.Errorf("[%s] match %s has Result=%q, expected victory", b.name, m.MatchGUID, m.Result)
				}
			}

			// 2. Verify result=defeat returns exactly Match 3
			defeats, totalD, err := store.ListHistoricalMatches(ctx, storage.MatchFilter{Result: "defeat"})
			if err != nil {
				t.Fatalf("[%s] ListHistoricalMatches(defeat) failed: %v", b.name, err)
			}
			if totalD != 1 || len(defeats) != 1 {
				t.Fatalf("[%s] expected 1 defeat, got total=%d len=%d", b.name, totalD, len(defeats))
			}
			if defeats[0].MatchGUID != guidOrangeDefeat {
				t.Errorf("[%s] expected defeat match %s, got %s", b.name, guidOrangeDefeat, defeats[0].MatchGUID)
			}
			if defeats[0].Result != "defeat" {
				t.Errorf("[%s] match %s has Result=%q, expected defeat", b.name, defeats[0].MatchGUID, defeats[0].Result)
			}

			// 3. Verify result=draw returns exactly Match 4
			draws, totalDraw, err := store.ListHistoricalMatches(ctx, storage.MatchFilter{Result: "draw"})
			if err != nil {
				t.Fatalf("[%s] ListHistoricalMatches(draw) failed: %v", b.name, err)
			}
			if totalDraw != 1 || len(draws) != 1 {
				t.Fatalf("[%s] expected 1 draw, got total=%d len=%d", b.name, totalDraw, len(draws))
			}
			if draws[0].MatchGUID != guidDraw {
				t.Errorf("[%s] expected draw match %s, got %s", b.name, guidDraw, draws[0].MatchGUID)
			}
			if draws[0].Result != "draw" {
				t.Errorf("[%s] match %s has Result=%q, expected draw", b.name, draws[0].MatchGUID, draws[0].Result)
			}
		})
	}
}

// TestChallenger_WinRateSortingParity_Comprehensive exercises multi-tier win rate ties,
// verifying exact ordering parity between SQLiteStore and JSONStore under sort=win_rate (both asc and desc).
func TestChallenger_WinRateSortingParity_Comprehensive(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 16, 15, 0, 0, 0, time.UTC)

	sqliteStore := newChallengerStressStore(t, false)
	jsonStore := newChallengerStressStore(t, true)

	// Create 5 players across varying win rates and match counts:
	// P1: 0% win rate (0 wins, 2 losses = 2 matches)
	// P2: 50% win rate (1 win, 1 loss = 2 matches)
	// P3: 50% win rate (2 wins, 2 losses = 4 matches)
	// P4: 50% win rate (5 wins, 5 losses = 10 matches)
	// P5: 100% win rate (2 wins, 0 losses = 2 matches)
	players := []*storage.PlayerRecord{
		{PlayerID: "Steam|P1_zero_rate|0", Platform: "Steam", PlayerName: "ZeroRate", LastSeenAt: now.Add(1 * time.Minute)},
		{PlayerID: "Steam|P2_fifty_two_matches|0", Platform: "Steam", PlayerName: "FiftyTwo", LastSeenAt: now.Add(2 * time.Minute)},
		{PlayerID: "Steam|P3_fifty_four_matches|0", Platform: "Steam", PlayerName: "FiftyFour", LastSeenAt: now.Add(3 * time.Minute)},
		{PlayerID: "Steam|P4_fifty_ten_matches|0", Platform: "Steam", PlayerName: "FiftyTen", LastSeenAt: now.Add(4 * time.Minute)},
		{PlayerID: "Steam|P5_hundred_rate|0", Platform: "Steam", PlayerName: "HundredRate", LastSeenAt: now.Add(5 * time.Minute)},
	}

	for _, s := range []storage.StateStore{sqliteStore, jsonStore} {
		for _, p := range players {
			_ = s.UpsertPlayer(ctx, p)
		}

		// P1: 0-2
		for i := 0; i < 2; i++ {
			_ = s.RecordMatchResults(ctx, fmt.Sprintf("m-p1-%d", i), 11, []storage.PlayerOutcome{
				{PlayerID: players[0].PlayerID, Platform: players[0].Platform, PlayerName: players[0].PlayerName, IsTeammate: true, Won: false},
			}, now)
		}

		// P2: 1-1 (2 matches)
		_ = s.RecordMatchResults(ctx, "m-p2-w", 11, []storage.PlayerOutcome{
			{PlayerID: players[1].PlayerID, Platform: players[1].Platform, PlayerName: players[1].PlayerName, IsTeammate: true, Won: true},
		}, now)
		_ = s.RecordMatchResults(ctx, "m-p2-l", 11, []storage.PlayerOutcome{
			{PlayerID: players[1].PlayerID, Platform: players[1].Platform, PlayerName: players[1].PlayerName, IsTeammate: true, Won: false},
		}, now)

		// P3: 2-2 (4 matches)
		for i := 0; i < 4; i++ {
			_ = s.RecordMatchResults(ctx, fmt.Sprintf("m-p3-%d", i), 11, []storage.PlayerOutcome{
				{PlayerID: players[2].PlayerID, Platform: players[2].Platform, PlayerName: players[2].PlayerName, IsTeammate: true, Won: i < 2},
			}, now)
		}

		// P4: 5-5 (10 matches)
		for i := 0; i < 10; i++ {
			_ = s.RecordMatchResults(ctx, fmt.Sprintf("m-p4-%d", i), 11, []storage.PlayerOutcome{
				{PlayerID: players[3].PlayerID, Platform: players[3].Platform, PlayerName: players[3].PlayerName, IsTeammate: true, Won: i < 5},
			}, now)
		}

		// P5: 2-0 (2 matches)
		for i := 0; i < 2; i++ {
			_ = s.RecordMatchResults(ctx, fmt.Sprintf("m-p5-%d", i), 11, []storage.PlayerOutcome{
				{PlayerID: players[4].PlayerID, Platform: players[4].Platform, PlayerName: players[4].PlayerName, IsTeammate: true, Won: true},
			}, now)
		}
	}

	// 1. Test sort=win_rate&order=asc
	filterAsc := storage.PlayerDirectoryFilter{
		Sort:  "win_rate",
		Order: "asc",
		Limit: 10,
	}
	sqliteAsc, _, err := sqliteStore.FilterPlayerSummaries(ctx, filterAsc)
	if err != nil {
		t.Fatalf("sqlite FilterPlayerSummaries asc failed: %v", err)
	}
	jsonAsc, _, err := jsonStore.FilterPlayerSummaries(ctx, filterAsc)
	if err != nil {
		t.Fatalf("jsonStore FilterPlayerSummaries asc failed: %v", err)
	}

	if len(sqliteAsc) != len(players) || len(jsonAsc) != len(players) {
		t.Fatalf("expected %d players, got sqlite=%d json=%d", len(players), len(sqliteAsc), len(jsonAsc))
	}

	for i := range sqliteAsc {
		if sqliteAsc[i].PlayerID != jsonAsc[i].PlayerID {
			t.Fatalf("ASC order divergence at index %d: SQLite=%s, JSONStore=%s", i, sqliteAsc[i].PlayerID, jsonAsc[i].PlayerID)
		}
	}

	// Under ASC:
	// P1 (0%) must be index 0
	// Tied 50% players (P2=2 matches, P3=4 matches, P4=10 matches) must appear in ascending total matches: P2 -> P3 -> P4
	// P5 (100%) must be index 4
	expectedAscIDs := []string{
		players[0].PlayerID, // P1: 0%
		players[1].PlayerID, // P2: 50%, 2 matches
		players[2].PlayerID, // P3: 50%, 4 matches
		players[3].PlayerID, // P4: 50%, 10 matches
		players[4].PlayerID, // P5: 100%
	}
	for i, expectedID := range expectedAscIDs {
		if sqliteAsc[i].PlayerID != expectedID {
			t.Errorf("ASC index %d: expected %s, got %s", i, expectedID, sqliteAsc[i].PlayerID)
		}
	}

	// 2. Test sort=win_rate&order=desc
	filterDesc := storage.PlayerDirectoryFilter{
		Sort:  "win_rate",
		Order: "desc",
		Limit: 10,
	}
	sqliteDesc, _, err := sqliteStore.FilterPlayerSummaries(ctx, filterDesc)
	if err != nil {
		t.Fatalf("sqlite FilterPlayerSummaries desc failed: %v", err)
	}
	jsonDesc, _, err := jsonStore.FilterPlayerSummaries(ctx, filterDesc)
	if err != nil {
		t.Fatalf("jsonStore FilterPlayerSummaries desc failed: %v", err)
	}

	for i := range sqliteDesc {
		if sqliteDesc[i].PlayerID != jsonDesc[i].PlayerID {
			t.Fatalf("DESC order divergence at index %d: SQLite=%s, JSONStore=%s", i, sqliteDesc[i].PlayerID, jsonDesc[i].PlayerID)
		}
	}

	// Under DESC:
	// P5 (100%) must be index 0
	// Tied 50% players must appear in descending total matches: P4 (10) -> P3 (4) -> P2 (2)
	// P1 (0%) must be index 4
	expectedDescIDs := []string{
		players[4].PlayerID, // P5: 100%
		players[3].PlayerID, // P4: 50%, 10 matches
		players[2].PlayerID, // P3: 50%, 4 matches
		players[1].PlayerID, // P2: 50%, 2 matches
		players[0].PlayerID, // P1: 0%
	}
	for i, expectedID := range expectedDescIDs {
		if sqliteDesc[i].PlayerID != expectedID {
			t.Errorf("DESC index %d: expected %s, got %s", i, expectedID, sqliteDesc[i].PlayerID)
		}
	}
}

// TestChallenger_SpotlightDeterminism_50Iterations runs 50 iterations on SQLiteStore and JSONStore
// with 3 candidate players having strictly equal stats, match counts, and timestamps.
// Asserts that BOTH stores deterministically select the candidate with lexicographically lowest PlayerID
// on 100% of iterations with ZERO jitter.
func TestChallenger_SpotlightDeterminism_50Iterations(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 16, 16, 0, 0, 0, time.UTC)

	sqliteStore := newChallengerStressStore(t, false)
	jsonStore := newChallengerStressStore(t, true)

	// 3 candidate teammates with identical stats: 2 wins, 0 losses, same timestamp
	c1 := &storage.PlayerRecord{PlayerID: "Steam|CCC_Player|0", Platform: "Steam", PlayerName: "Charlie", LastSeenAt: now}
	c2 := &storage.PlayerRecord{PlayerID: "Steam|AAA_Player|0", Platform: "Steam", PlayerName: "Alice", LastSeenAt: now}
	c3 := &storage.PlayerRecord{PlayerID: "Steam|BBB_Player|0", Platform: "Steam", PlayerName: "Bob", LastSeenAt: now}

	// 3 candidate opponents with identical stats: 2 losses against us, 0 wins (0% win rate), same timestamp
	r1 := &storage.PlayerRecord{PlayerID: "Epic|ZZZ_Rival|0", Platform: "Epic", PlayerName: "Zack", LastSeenAt: now}
	r2 := &storage.PlayerRecord{PlayerID: "Epic|MMM_Rival|0", Platform: "Epic", PlayerName: "Mike", LastSeenAt: now}

	for _, s := range []storage.StateStore{sqliteStore, jsonStore} {
		_ = s.UpsertPlayer(ctx, c1)
		_ = s.UpsertPlayer(ctx, c2)
		_ = s.UpsertPlayer(ctx, c3)
		_ = s.UpsertPlayer(ctx, r1)
		_ = s.UpsertPlayer(ctx, r2)

		for i := 0; i < 2; i++ {
			_ = s.RecordMatchResults(ctx, fmt.Sprintf("m-tm-%d", i), 11, []storage.PlayerOutcome{
				{PlayerID: c1.PlayerID, Platform: c1.Platform, PlayerName: c1.PlayerName, IsTeammate: true, Won: true},
				{PlayerID: c2.PlayerID, Platform: c2.Platform, PlayerName: c2.PlayerName, IsTeammate: true, Won: true},
				{PlayerID: c3.PlayerID, Platform: c3.Platform, PlayerName: c3.PlayerName, IsTeammate: true, Won: true},
				{PlayerID: r1.PlayerID, Platform: r1.Platform, PlayerName: r1.PlayerName, IsTeammate: false, Won: true},
				{PlayerID: r2.PlayerID, Platform: r2.Platform, PlayerName: r2.PlayerName, IsTeammate: false, Won: true},
			}, now)
		}
	}

	expectedTeammate := "Steam|AAA_Player|0"
	expectedRival := "Epic|MMM_Rival|0"

	iterations := 50

	// Test SQLiteStore
	for iter := 0; iter < iterations; iter++ {
		sp, err := sqliteStore.GetPlayerSpotlights(ctx)
		if err != nil {
			t.Fatalf("sqlite GetPlayerSpotlights iter %d failed: %v", iter, err)
		}
		if sp.BestTeammate == nil || sp.BestTeammate.Player == nil || sp.BestTeammate.Player.PlayerID != expectedTeammate {
			t.Fatalf("[SQLite] iter %d: expected BestTeammate %s, got %v", iter, expectedTeammate, sp.BestTeammate)
		}
		if sp.ToughestRival == nil || sp.ToughestRival.Player == nil || sp.ToughestRival.Player.PlayerID != expectedRival {
			t.Fatalf("[SQLite] iter %d: expected ToughestRival %s, got %v", iter, expectedRival, sp.ToughestRival)
		}
	}

	// Test JSONStore
	for iter := 0; iter < iterations; iter++ {
		sp, err := jsonStore.GetPlayerSpotlights(ctx)
		if err != nil {
			t.Fatalf("jsonStore GetPlayerSpotlights iter %d failed: %v", iter, err)
		}
		if sp.BestTeammate == nil || sp.BestTeammate.Player == nil || sp.BestTeammate.Player.PlayerID != expectedTeammate {
			t.Fatalf("[JSONStore] iter %d: expected BestTeammate %s, got %v", iter, expectedTeammate, sp.BestTeammate)
		}
		if sp.ToughestRival == nil || sp.ToughestRival.Player == nil || sp.ToughestRival.Player.PlayerID != expectedRival {
			t.Fatalf("[JSONStore] iter %d: expected ToughestRival %s, got %v", iter, expectedRival, sp.ToughestRival)
		}
	}
}
