package storage_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/storage"
)

func newChallengerTestStore(t *testing.T, isJSON bool) storage.StateStore {
	t.Helper()
	tempDir := t.TempDir()
	var dbPath string
	if isJSON {
		dbPath = filepath.Join(tempDir, "challenger_store.json")
	} else {
		dbPath = filepath.Join(tempDir, "challenger_store.db")
	}

	store, err := storage.NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store (isJSON=%v): %v", isJSON, err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

// TestM3_Adversarial_EmptyDatabase tests all storage queries against an empty database.
func TestM3_Adversarial_EmptyDatabase(t *testing.T) {
	backends := []struct {
		name   string
		isJSON bool
	}{
		{"SQLite", false},
		{"JSONStore", true},
	}

	for _, b := range backends {
		t.Run(b.name, func(t *testing.T) {
			store := newChallengerTestStore(t, b.isJSON)
			ctx := context.Background()

			// 1. FilterPlayerSummaries with empty db
			summaries, total, err := store.FilterPlayerSummaries(ctx, storage.PlayerDirectoryFilter{
				Role:  "synergy",
				Limit: 50,
			})
			if err != nil {
				t.Fatalf("[%s] FilterPlayerSummaries on empty db failed: %v", b.name, err)
			}
			if total != 0 || len(summaries) != 0 {
				t.Errorf("[%s] expected 0 summaries on empty db, got %d (len=%d)", b.name, total, len(summaries))
			}

			// 2. Spotlights on empty db
			spotlights, err := store.GetPlayerSpotlights(ctx)
			if err != nil {
				t.Fatalf("[%s] GetPlayerSpotlights on empty db failed: %v", b.name, err)
			}
			if spotlights == nil {
				t.Fatalf("[%s] GetPlayerSpotlights returned nil spotlights", b.name)
			}
			if spotlights.BestTeammate == nil || spotlights.BestTeammate.Player != nil || spotlights.BestTeammate.Sub != "NOT ENOUGH GAMES YET" {
				t.Errorf("[%s] unexpected BestTeammate on empty db: %+v", b.name, spotlights.BestTeammate)
			}
			if spotlights.MostFaced == nil || spotlights.MostFaced.Player != nil || spotlights.MostFaced.Sub != "NOT ENOUGH GAMES YET" {
				t.Errorf("[%s] unexpected MostFaced on empty db: %+v", b.name, spotlights.MostFaced)
			}
			if spotlights.ToughestRival == nil || spotlights.ToughestRival.Player != nil || spotlights.ToughestRival.Sub != "NOT ENOUGH GAMES YET" {
				t.Errorf("[%s] unexpected ToughestRival on empty db: %+v", b.name, spotlights.ToughestRival)
			}

			// 3. ListHistoricalMatches on empty db
			matches, totalM, err := store.ListHistoricalMatches(ctx, storage.MatchFilter{
				Result: "victory",
			})
			if err != nil {
				t.Fatalf("[%s] ListHistoricalMatches on empty db failed: %v", b.name, err)
			}
			if totalM != 0 || len(matches) != 0 {
				t.Errorf("[%s] expected 0 matches on empty db, got %d (len=%d)", b.name, totalM, len(matches))
			}

			// 4. ListPlayerMatches on empty db
			pMatches, totalPM, err := store.ListPlayerMatches(ctx, "nonexistent", storage.PlayerMatchFilter{})
			if err != nil {
				t.Fatalf("[%s] ListPlayerMatches on empty db failed: %v", b.name, err)
			}
			if totalPM != 0 || len(pMatches) != 0 {
				t.Errorf("[%s] expected 0 player matches on empty db, got %d (len=%d)", b.name, totalPM, len(pMatches))
			}
		})
	}
}

// TestM3_Adversarial_SynergyRoleEdgeCases tests edge cases for role=synergy:
// - Player with 0 games
// - Player with 1 game (100% win rate: 1-0) -> should NOT qualify (requires >= 2 games)
// - Player with 10 games (60% win rate: 6-4) -> SHOULD qualify (>= 2 games and >= 60%)
// - Player with 10 games (50% win rate: 5-5) -> should NOT qualify (< 60%)
func TestM3_Adversarial_SynergyRoleEdgeCases(t *testing.T) {
	backends := []struct {
		name   string
		isJSON bool
	}{
		{"SQLite", false},
		{"JSONStore", true},
	}

	for _, b := range backends {
		t.Run(b.name, func(t *testing.T) {
			store := newChallengerTestStore(t, b.isJSON)
			ctx := context.Background()
			now := time.Now().UTC()

			// P0: 0 games
			p0 := &storage.PlayerRecord{
				PlayerID:   "Epic|p0_nogames|0",
				Platform:   "Epic",
				PlayerName: "ZeroGames",
				LastSeenAt: now,
			}
			// P1: 1 game, 1 win (100% win rate)
			p1 := &storage.PlayerRecord{
				PlayerID:   "Steam|76561198000000001|0",
				Platform:   "Steam",
				PlayerName: "OneGame100Pct",
				LastSeenAt: now.Add(1 * time.Minute),
			}
			// P2: 10 games, 6 wins, 4 losses (60% win rate)
			p2 := &storage.PlayerRecord{
				PlayerID:   "Steam|76561198000000002|0",
				Platform:   "Steam",
				PlayerName: "TenGames60Pct",
				LastSeenAt: now.Add(2 * time.Minute),
			}
			// P3: 10 games, 5 wins, 5 losses (50% win rate)
			p3 := &storage.PlayerRecord{
				PlayerID:   "Steam|76561198000000003|0",
				Platform:   "Steam",
				PlayerName: "TenGames50Pct",
				LastSeenAt: now.Add(3 * time.Minute),
			}

			for _, p := range []*storage.PlayerRecord{p0, p1, p2, p3} {
				if err := store.UpsertPlayer(ctx, p); err != nil {
					t.Fatalf("[%s] UpsertPlayer %s failed: %v", b.name, p.PlayerID, err)
				}
			}

			// Match for P1: 1 win as teammate
			_ = store.RecordMatchResults(ctx, "m-p1-1", 11, []storage.PlayerOutcome{
				{PlayerID: p1.PlayerID, Platform: p1.Platform, PlayerName: p1.PlayerName, IsTeammate: true, Won: true},
			}, now.Add(10*time.Minute))

			// 10 Matches for P2: 6 wins, 4 losses as teammate
			for i := 0; i < 10; i++ {
				won := (i < 6)
				_ = store.RecordMatchResults(ctx, fmt.Sprintf("m-p2-%d", i), 11, []storage.PlayerOutcome{
					{PlayerID: p2.PlayerID, Platform: p2.Platform, PlayerName: p2.PlayerName, IsTeammate: true, Won: won},
				}, now.Add(time.Duration(20+i)*time.Minute))
			}

			// 10 Matches for P3: 5 wins, 5 losses as teammate
			for i := 0; i < 10; i++ {
				won := (i < 5)
				_ = store.RecordMatchResults(ctx, fmt.Sprintf("m-p3-%d", i), 11, []storage.PlayerOutcome{
					{PlayerID: p3.PlayerID, Platform: p3.Platform, PlayerName: p3.PlayerName, IsTeammate: true, Won: won},
				}, now.Add(time.Duration(40+i)*time.Minute))
			}

			// Filter with role=synergy
			res, total, err := store.FilterPlayerSummaries(ctx, storage.PlayerDirectoryFilter{
				Role:  "synergy",
				Limit: 10,
			})
			if err != nil {
				t.Fatalf("[%s] FilterPlayerSummaries role=synergy failed: %v", b.name, err)
			}

			if total != 1 || len(res) != 1 {
				t.Fatalf("[%s] expected exactly 1 synergy player (p2), got total=%d, len=%d", b.name, total, len(res))
			}
			if res[0].PlayerID != p2.PlayerID {
				t.Errorf("[%s] expected p2 (%s), got %s", b.name, p2.PlayerID, res[0].PlayerID)
			}
		})
	}
}

// TestM3_Adversarial_SQLInjectionAttempts tests hostile inputs in all filter parameters.
func TestM3_Adversarial_SQLInjectionAttempts(t *testing.T) {
	backends := []struct {
		name   string
		isJSON bool
	}{
		{"SQLite", false},
		{"JSONStore", true},
	}

	payloads := []string{
		"'; DROP TABLE players; --",
		"' OR '1'='1",
		"admin'--",
		"1' UNION SELECT 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13 --",
		"name ASC; DROP TABLE player_matchups; --",
		"' AND 1=0 UNION ALL SELECT * FROM sqlite_master --",
		"\\000'; DROP TABLE players; --",
		"\" OR \"\"=\"",
		"` OR ``=`",
	}

	for _, b := range backends {
		t.Run(b.name, func(t *testing.T) {
			store := newChallengerTestStore(t, b.isJSON)
			ctx := context.Background()

			// Insert a test player
			p := &storage.PlayerRecord{
				PlayerID:   "Steam|76561198000000099|0",
				Platform:   "Steam",
				PlayerName: "LegitPlayer",
				LastSeenAt: time.Now().UTC(),
			}
			if err := store.UpsertPlayer(ctx, p); err != nil {
				t.Fatalf("UpsertPlayer failed: %v", err)
			}

			for idx, attack := range payloads {
				// Attack in Query
				_, _, err := store.FilterPlayerSummaries(ctx, storage.PlayerDirectoryFilter{
					Query: attack,
				})
				if err != nil {
					t.Errorf("[%s][%d] Query injection caused unexpected error: %v (input=%q)", b.name, idx, err, attack)
				}

				// Attack in Platform
				_, _, err = store.FilterPlayerSummaries(ctx, storage.PlayerDirectoryFilter{
					Platform: attack,
				})
				if err != nil {
					t.Errorf("[%s][%d] Platform injection caused unexpected error: %v (input=%q)", b.name, idx, err, attack)
				}

				// Attack in Role
				_, _, err = store.FilterPlayerSummaries(ctx, storage.PlayerDirectoryFilter{
					Role: attack,
				})
				if err != nil {
					t.Errorf("[%s][%d] Role injection caused unexpected error: %v (input=%q)", b.name, idx, err, attack)
				}

				// Attack in Sort
				_, _, err = store.FilterPlayerSummaries(ctx, storage.PlayerDirectoryFilter{
					Sort: attack,
				})
				if err != nil {
					t.Errorf("[%s][%d] Sort injection caused unexpected error: %v (input=%q)", b.name, idx, err, attack)
				}

				// Attack in Order
				_, _, err = store.FilterPlayerSummaries(ctx, storage.PlayerDirectoryFilter{
					Order: attack,
				})
				if err != nil {
					t.Errorf("[%s][%d] Order injection caused unexpected error: %v (input=%q)", b.name, idx, err, attack)
				}
			}

			// Verify table is still intact and data exists
			found, err := store.GetPlayer(ctx, p.PlayerID)
			if err != nil || found == nil {
				t.Fatalf("[%s] players table corrupted or player missing after injection suite: %v", b.name, err)
			}
		})
	}
}

// TestM3_Adversarial_SpotlightsParity_TiesAndInsufficient tests parity between SQLiteStore
// and JSONStore for spotlights when games are tied or insufficient.
func TestM3_Adversarial_SpotlightsParity_TiesAndInsufficient(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

	// Subtest 1: Insufficient games (< 2 games)
	t.Run("InsufficientGames", func(t *testing.T) {
		sqliteStore := newChallengerTestStore(t, false)
		jsonStore := newChallengerTestStore(t, true)

		// 1 game each
		p1 := &storage.PlayerRecord{PlayerID: "P1", Platform: "Steam", PlayerName: "OneGameTM", LastSeenAt: now}
		p2 := &storage.PlayerRecord{PlayerID: "P2", Platform: "Epic", PlayerName: "OneGameOpp", LastSeenAt: now}

		for _, s := range []storage.StateStore{sqliteStore, jsonStore} {
			_ = s.UpsertPlayer(ctx, p1)
			_ = s.UpsertPlayer(ctx, p2)
			_ = s.RecordMatchResults(ctx, "m1", 11, []storage.PlayerOutcome{
				{PlayerID: p1.PlayerID, Platform: p1.Platform, PlayerName: p1.PlayerName, IsTeammate: true, Won: true},
				{PlayerID: p2.PlayerID, Platform: p2.Platform, PlayerName: p2.PlayerName, IsTeammate: false, Won: true},
			}, now)
		}

		spSQLite, err := sqliteStore.GetPlayerSpotlights(ctx)
		if err != nil {
			t.Fatalf("sqlite GetPlayerSpotlights failed: %v", err)
		}
		spJSON, err := jsonStore.GetPlayerSpotlights(ctx)
		if err != nil {
			t.Fatalf("jsonStore GetPlayerSpotlights failed: %v", err)
		}

		// Both must return NOT ENOUGH GAMES YET for all three
		if spSQLite.BestTeammate.Sub != "NOT ENOUGH GAMES YET" || spJSON.BestTeammate.Sub != "NOT ENOUGH GAMES YET" {
			t.Errorf("expected NOT ENOUGH GAMES YET for BestTeammate: SQLite=%s, JSON=%s", spSQLite.BestTeammate.Sub, spJSON.BestTeammate.Sub)
		}
		if spSQLite.MostFaced.Sub != "NOT ENOUGH GAMES YET" || spJSON.MostFaced.Sub != "NOT ENOUGH GAMES YET" {
			t.Errorf("expected NOT ENOUGH GAMES YET for MostFaced: SQLite=%s, JSON=%s", spSQLite.MostFaced.Sub, spJSON.MostFaced.Sub)
		}
		if spSQLite.ToughestRival.Sub != "NOT ENOUGH GAMES YET" || spJSON.ToughestRival.Sub != "NOT ENOUGH GAMES YET" {
			t.Errorf("expected NOT ENOUGH GAMES YET for ToughestRival: SQLite=%s, JSON=%s", spSQLite.ToughestRival.Sub, spJSON.ToughestRival.Sub)
		}
	})

	// Subtest 2: Tied games with identical timestamps and identical stats
	t.Run("TiedGamesDeterminismAndParity", func(t *testing.T) {
		sqliteStore := newChallengerTestStore(t, false)
		jsonStore := newChallengerTestStore(t, true)

		// Two teammates playing in the exact same 2 matches, winning both (2-0, 100% win rate, same LastSeenAt)
		pA := &storage.PlayerRecord{PlayerID: "Epic|AAA_Player|0", Platform: "Epic", PlayerName: "AAA_Player", LastSeenAt: now}
		pB := &storage.PlayerRecord{PlayerID: "Steam|ZZZ_Player|0", Platform: "Steam", PlayerName: "ZZZ_Player", LastSeenAt: now}

		for _, s := range []storage.StateStore{sqliteStore, jsonStore} {
			_ = s.UpsertPlayer(ctx, pA)
			_ = s.UpsertPlayer(ctx, pB)
			_ = s.RecordMatchResults(ctx, "m-tie-1", 11, []storage.PlayerOutcome{
				{PlayerID: pA.PlayerID, Platform: pA.Platform, PlayerName: pA.PlayerName, IsTeammate: true, Won: true},
				{PlayerID: pB.PlayerID, Platform: pB.Platform, PlayerName: pB.PlayerName, IsTeammate: true, Won: true},
			}, now)
			_ = s.RecordMatchResults(ctx, "m-tie-2", 11, []storage.PlayerOutcome{
				{PlayerID: pA.PlayerID, Platform: pA.Platform, PlayerName: pA.PlayerName, IsTeammate: true, Won: true},
				{PlayerID: pB.PlayerID, Platform: pB.Platform, PlayerName: pB.PlayerName, IsTeammate: true, Won: true},
			}, now)
		}

		// Run JSONStore multiple times to test if map iteration causes non-deterministic results
		var jsonFirstPicked string
		for i := 0; i < 20; i++ {
			spJSON, err := jsonStore.GetPlayerSpotlights(ctx)
			if err != nil {
				t.Fatalf("jsonStore GetPlayerSpotlights failed: %v", err)
			}
			if spJSON.BestTeammate == nil || spJSON.BestTeammate.Player == nil {
				t.Fatalf("unexpected nil BestTeammate player in JSONStore")
			}
			picked := spJSON.BestTeammate.Player.PlayerID
			if i == 0 {
				jsonFirstPicked = picked
			} else if picked != jsonFirstPicked {
				t.Logf("[DISCOVERY/WARNING] JSONStore GetPlayerSpotlights is non-deterministic under ties across calls: call 0 picked %s, call %d picked %s (Go map iteration randomness)", jsonFirstPicked, i, picked)
			}
		}

		spSQLite, err := sqliteStore.GetPlayerSpotlights(ctx)
		if err != nil {
			t.Fatalf("sqlite GetPlayerSpotlights failed: %v", err)
		}
		if spSQLite.BestTeammate == nil || spSQLite.BestTeammate.Player == nil {
			t.Fatalf("unexpected nil BestTeammate player in SQLiteStore")
		}
		t.Logf("[INFO] Spotlights tie-break: SQLite picked %s, JSONStore picked %s", spSQLite.BestTeammate.Player.PlayerID, jsonFirstPicked)
	})
}

// TestM3_Adversarial_HistoricalMatchesOutcomeFiltering tests local-relative outcome filtering
// across Blue assignments, Orange assignments, and draws.
func TestM3_Adversarial_HistoricalMatchesOutcomeFiltering(t *testing.T) {
	backends := []struct {
		name   string
		isJSON bool
	}{
		{"SQLite", false},
		{"JSONStore", true},
	}

	for _, b := range backends {
		t.Run(b.name, func(t *testing.T) {
			store := newChallengerTestStore(t, b.isJSON)
			ctx := context.Background()
			now := time.Now().UTC()
			localPID := "Steam|76561198000000000|0"

			// Authenticate local player
			_ = store.SaveAuthState(ctx, "steam", "token", localPID, "LocalPlayer")

			// Match 1: Local on Blue (team 0), Blue wins 3-1 -> VICTORY
			wt0 := 0
			_ = store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
				MatchGUID:   "guid-m1-blue-win",
				Playlist:    11,
				BlueScore:   3,
				OrangeScore: 1,
				WinnerTeam:  &wt0,
				Date:        now.Add(-60 * time.Minute).Format(time.RFC3339),
			})

			// Match 2: Local on Orange (team 1), Orange wins 4-2 -> VICTORY
			wt1 := 1
			_ = store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
				MatchGUID:   "guid-m2-orange-win",
				Playlist:    11,
				BlueScore:   2,
				OrangeScore: 4,
				WinnerTeam:  &wt1,
				Date:        now.Add(-50 * time.Minute).Format(time.RFC3339),
			})

			// Match 3: Local on Orange (team 1), Blue wins 5-1 -> DEFEAT
			_ = store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
				MatchGUID:   "guid-m3-orange-loss",
				Playlist:    11,
				BlueScore:   5,
				OrangeScore: 1,
				WinnerTeam:  &wt0,
				Date:        now.Add(-40 * time.Minute).Format(time.RFC3339),
			})

			// Match 4: Local on Blue (team 0), Orange wins 2-1 -> DEFEAT
			_ = store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
				MatchGUID:   "guid-m4-blue-loss",
				Playlist:    11,
				BlueScore:   1,
				OrangeScore: 2,
				WinnerTeam:  &wt1,
				Date:        now.Add(-30 * time.Minute).Format(time.RFC3339),
			})

			// Match 5: Local on Blue (team 0), 2-2 Draw -> DRAW
			wtDraw := -1
			_ = store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
				MatchGUID:   "guid-m5-blue-draw",
				Playlist:    11,
				BlueScore:   2,
				OrangeScore: 2,
				WinnerTeam:  &wtDraw,
				Date:        now.Add(-20 * time.Minute).Format(time.RFC3339),
			})

			// Match 6: Local on Orange (team 1), 1-1 Draw -> DRAW
			_ = store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
				MatchGUID:   "guid-m6-orange-draw",
				Playlist:    11,
				BlueScore:   1,
				OrangeScore: 1,
				WinnerTeam:  &wtDraw,
				Date:        now.Add(-10 * time.Minute).Format(time.RFC3339),
			})

			// Add match_players with local player team numbers
			wonTrue := true
			wonFalse := false

			// m1: local on team 0, won
			_ = store.SaveMatchPlayers(ctx, "guid-m1-blue-win", []*storage.MatchPlayerRecord{
				{MatchGUID: "guid-m1-blue-win", PlayerID: localPID, TeamNum: 0, IsLocal: true, Won: &wonTrue},
			})
			// m2: local on team 1, won
			_ = store.SaveMatchPlayers(ctx, "guid-m2-orange-win", []*storage.MatchPlayerRecord{
				{MatchGUID: "guid-m2-orange-win", PlayerID: localPID, TeamNum: 1, IsLocal: true, Won: &wonTrue},
			})
			// m3: local on team 1, lost
			_ = store.SaveMatchPlayers(ctx, "guid-m3-orange-loss", []*storage.MatchPlayerRecord{
				{MatchGUID: "guid-m3-orange-loss", PlayerID: localPID, TeamNum: 1, IsLocal: true, Won: &wonFalse},
			})
			// m4: local on team 0, lost
			_ = store.SaveMatchPlayers(ctx, "guid-m4-blue-loss", []*storage.MatchPlayerRecord{
				{MatchGUID: "guid-m4-blue-loss", PlayerID: localPID, TeamNum: 0, IsLocal: true, Won: &wonFalse},
			})
			// m5: local on team 0, draw
			_ = store.SaveMatchPlayers(ctx, "guid-m5-blue-draw", []*storage.MatchPlayerRecord{
				{MatchGUID: "guid-m5-blue-draw", PlayerID: localPID, TeamNum: 0, IsLocal: true, Won: nil},
			})
			// m6: local on team 1, draw
			_ = store.SaveMatchPlayers(ctx, "guid-m6-orange-draw", []*storage.MatchPlayerRecord{
				{MatchGUID: "guid-m6-orange-draw", PlayerID: localPID, TeamNum: 1, IsLocal: true, Won: nil},
			})

			// 1. Filter result=victory
			vics, totalV, err := store.ListHistoricalMatches(ctx, storage.MatchFilter{Result: "victory"})
			if err != nil {
				t.Fatalf("[%s] ListHistoricalMatches(victory) failed: %v", b.name, err)
			}
			if totalV != 2 || len(vics) != 2 {
				t.Errorf("[%s] expected 2 victories (m1 & m2), got total=%d, len=%d", b.name, totalV, len(vics))
			}
			for _, m := range vics {
				if m.MatchGUID != "guid-m1-blue-win" && m.MatchGUID != "guid-m2-orange-win" {
					t.Errorf("[%s] unexpected victory match: %s", b.name, m.MatchGUID)
				}
				if m.Result != "victory" {
					t.Errorf("[%s] match %s has Result %q, want victory", b.name, m.MatchGUID, m.Result)
				}
			}

			// 2. Filter result=defeat
			defs, totalD, err := store.ListHistoricalMatches(ctx, storage.MatchFilter{Result: "defeat"})
			if err != nil {
				t.Fatalf("[%s] ListHistoricalMatches(defeat) failed: %v", b.name, err)
			}
			if totalD != 2 || len(defs) != 2 {
				t.Errorf("[%s] expected 2 defeats (m3 & m4), got total=%d, len=%d", b.name, totalD, len(defs))
			}
			for _, m := range defs {
				if m.MatchGUID != "guid-m3-orange-loss" && m.MatchGUID != "guid-m4-blue-loss" {
					t.Errorf("[%s] unexpected defeat match: %s", b.name, m.MatchGUID)
				}
				if m.Result != "defeat" {
					t.Errorf("[%s] match %s has Result %q, want defeat", b.name, m.MatchGUID, m.Result)
				}
			}

			// 3. Filter result=draw
			draws, totalDraw, err := store.ListHistoricalMatches(ctx, storage.MatchFilter{Result: "draw"})
			if err != nil {
				t.Fatalf("[%s] ListHistoricalMatches(draw) failed: %v", b.name, err)
			}
			if totalDraw != 2 || len(draws) != 2 {
				t.Errorf("[%s] expected 2 draws (m5 & m6), got total=%d, len=%d", b.name, totalDraw, len(draws))
			}
			for _, m := range draws {
				if m.MatchGUID != "guid-m5-blue-draw" && m.MatchGUID != "guid-m6-orange-draw" {
					t.Errorf("[%s] unexpected draw match: %s", b.name, m.MatchGUID)
				}
				if m.Result != "draw" {
					t.Errorf("[%s] match %s has Result %q, want draw", b.name, m.MatchGUID, m.Result)
				}
			}
		})
	}
}

// TestM3_Adversarial_SortWinRateParity tests whether SQLiteStore and JSONStore have identical
// sort order when sort=win_rate&order=asc with tied win rates and different total matches.
func TestM3_Adversarial_SortWinRateParity(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

	sqliteStore := newChallengerTestStore(t, false)
	jsonStore := newChallengerTestStore(t, true)

	// Two players with identical win rate:
	// P1: 1 win, 1 loss = 50% win rate, total 2 matches
	// P2: 5 wins, 5 losses = 50% win rate, total 10 matches
	p1 := &storage.PlayerRecord{PlayerID: "Steam|p1_few_matches|0", Platform: "Steam", PlayerName: "PlayerFew", LastSeenAt: now}
	p2 := &storage.PlayerRecord{PlayerID: "Steam|p2_many_matches|0", Platform: "Steam", PlayerName: "PlayerMany", LastSeenAt: now}

	for _, s := range []storage.StateStore{sqliteStore, jsonStore} {
		_ = s.UpsertPlayer(ctx, p1)
		_ = s.UpsertPlayer(ctx, p2)

		// P1: 1 win, 1 loss
		_ = s.RecordMatchResults(ctx, "m-p1-1", 11, []storage.PlayerOutcome{
			{PlayerID: p1.PlayerID, Platform: p1.Platform, PlayerName: p1.PlayerName, IsTeammate: true, Won: true},
		}, now)
		_ = s.RecordMatchResults(ctx, "m-p1-2", 11, []storage.PlayerOutcome{
			{PlayerID: p1.PlayerID, Platform: p1.Platform, PlayerName: p1.PlayerName, IsTeammate: true, Won: false},
		}, now)

		// P2: 5 wins, 5 losses
		for i := 0; i < 10; i++ {
			_ = s.RecordMatchResults(ctx, fmt.Sprintf("m-p2-%d", i), 11, []storage.PlayerOutcome{
				{PlayerID: p2.PlayerID, Platform: p2.Platform, PlayerName: p2.PlayerName, IsTeammate: true, Won: i < 5},
			}, now)
		}
	}

	// Filter with sort=win_rate, order=asc
	filterAsc := storage.PlayerDirectoryFilter{
		Sort:  "win_rate",
		Order: "asc",
		Limit: 10,
	}

	resSQLite, _, err := sqliteStore.FilterPlayerSummaries(ctx, filterAsc)
	if err != nil {
		t.Fatalf("sqlite FilterPlayerSummaries failed: %v", err)
	}
	resJSON, _, err := jsonStore.FilterPlayerSummaries(ctx, filterAsc)
	if err != nil {
		t.Fatalf("jsonStore FilterPlayerSummaries failed: %v", err)
	}

	if len(resSQLite) != 2 || len(resJSON) != 2 {
		t.Fatalf("expected 2 results each, got sqlite=%d, json=%d", len(resSQLite), len(resJSON))
	}

	t.Logf("sort=win_rate&order=asc results:")
	t.Logf("SQLiteStore: [0]=%s (matches=%d), [1]=%s (matches=%d)",
		resSQLite[0].PlayerID, resSQLite[0].TotalMatches, resSQLite[1].PlayerID, resSQLite[1].TotalMatches)
	t.Logf("JSONStore:   [0]=%s (matches=%d), [1]=%s (matches=%d)",
		resJSON[0].PlayerID, resJSON[0].TotalMatches, resJSON[1].PlayerID, resJSON[1].TotalMatches)

	if resSQLite[0].PlayerID != resJSON[0].PlayerID {
		t.Errorf("[PARITY GAP FOUND] When sort=win_rate&order=asc and rates are tied: SQLiteStore put %s first (fewer matches), JSONStore put %s first (more matches)!",
			resSQLite[0].PlayerID, resJSON[0].PlayerID)
	}
}
