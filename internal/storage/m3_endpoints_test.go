package storage_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/storage"
)

func newTestStoreBackend(t *testing.T, isJSON bool) storage.StateStore {
	t.Helper()
	tempDir := t.TempDir()
	var dbPath string
	if isJSON {
		dbPath = filepath.Join(tempDir, "test_store.json")
	} else {
		dbPath = filepath.Join(tempDir, "test_store.db")
	}

	store, err := storage.NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to initialize store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

func TestM3_Storage_FilterPlayerSummaries_And_Spotlights(t *testing.T) {
	backends := []struct {
		name   string
		isJSON bool
	}{
		{"SQLite", false},
		{"JSONStore", true},
	}

	for _, b := range backends {
		t.Run(b.name, func(t *testing.T) {
			store := newTestStoreBackend(t, b.isJSON)
			ctx := context.Background()

			baseTime := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

			// Setup players:
			// P1: Best Teammate candidate (played with us twice, won both: 2-0 -> 100% win rate)
			p1 := &storage.PlayerRecord{
				PlayerID:   "Steam|76561198000000001|0",
				Platform:   "Steam",
				PlayerName: "AceTeammate",
				LastSeenAt: baseTime.Add(1 * time.Hour),
			}
			// P2: Most Faced & Toughest Rival candidate (played as opponent 3 times: 0 wins, 3 losses -> 0% win rate)
			p2 := &storage.PlayerRecord{
				PlayerID:   "Epic|rival_player_02|0",
				Platform:   "Epic",
				PlayerName: "ToughRival",
				LastSeenAt: baseTime.Add(2 * time.Hour),
			}
			// P3: Frequent player (mixed teammate 1-1, opponent 1-1: total 4 matches)
			p3 := &storage.PlayerRecord{
				PlayerID:   "Steam|76561198000000003|0",
				Platform:   "Steam",
				PlayerName: "CasualFriend",
				LastSeenAt: baseTime.Add(3 * time.Hour),
			}
			// P4: One-off player (1 match as opponent: 1-0)
			p4 := &storage.PlayerRecord{
				PlayerID:   "PSN|psn_solo_04|0",
				Platform:   "PSN",
				PlayerName: "SoloGuy",
				LastSeenAt: baseTime.Add(4 * time.Hour),
			}

			for _, p := range []*storage.PlayerRecord{p1, p2, p3, p4} {
				if err := store.UpsertPlayer(ctx, p); err != nil {
					t.Fatalf("failed to upsert player %s: %v", p.PlayerID, err)
				}
			}

			// Record match results:
			// Match 1: P1 teammate (Win), P2 opponent (Loss for P2)
			err := store.RecordMatchResults(ctx, "guid-1", 11, []storage.PlayerOutcome{
				{PlayerID: p1.PlayerID, Platform: p1.Platform, PlayerName: p1.PlayerName, IsTeammate: true, Won: true},
				{PlayerID: p2.PlayerID, Platform: p2.Platform, PlayerName: p2.PlayerName, IsTeammate: false, Won: true}, // we won vs p2
			}, baseTime.Add(10*time.Minute))
			if err != nil {
				t.Fatalf("record match 1 failed: %v", err)
			}

			// Match 2: P1 teammate (Win) -> P1 is 2-0 with us
			err = store.RecordMatchResults(ctx, "guid-2", 11, []storage.PlayerOutcome{
				{PlayerID: p1.PlayerID, Platform: p1.Platform, PlayerName: p1.PlayerName, IsTeammate: true, Won: true},
			}, baseTime.Add(20*time.Minute))
			if err != nil {
				t.Fatalf("record match 2 failed: %v", err)
			}

			// Match 3: P2 opponent (we lost vs P2 -> P2 won against us)
			err = store.RecordMatchResults(ctx, "guid-3", 11, []storage.PlayerOutcome{
				{PlayerID: p2.PlayerID, Platform: p2.Platform, PlayerName: p2.PlayerName, IsTeammate: false, Won: false}, // we lost vs p2
			}, baseTime.Add(30*time.Minute))
			if err != nil {
				t.Fatalf("record match 3 failed: %v", err)
			}

			// Match 4: P2 opponent (we lost vs P2 again) -> P2 vs us: 1 win, 2 losses (or 0-2? Total matches 3)
			err = store.RecordMatchResults(ctx, "guid-4", 11, []storage.PlayerOutcome{
				{PlayerID: p2.PlayerID, Platform: p2.Platform, PlayerName: p2.PlayerName, IsTeammate: false, Won: false},
			}, baseTime.Add(40*time.Minute))
			if err != nil {
				t.Fatalf("record match 4 failed: %v", err)
			}

			// Match 5 & 6 & 7 & 8: P3 records (4 matches total: 2 as teammate, 2 as opponent)
			_ = store.RecordMatchResults(ctx, "guid-5", 11, []storage.PlayerOutcome{
				{PlayerID: p3.PlayerID, Platform: p3.Platform, PlayerName: p3.PlayerName, IsTeammate: true, Won: true},
			}, baseTime.Add(50*time.Minute))
			_ = store.RecordMatchResults(ctx, "guid-6", 11, []storage.PlayerOutcome{
				{PlayerID: p3.PlayerID, Platform: p3.Platform, PlayerName: p3.PlayerName, IsTeammate: true, Won: false},
			}, baseTime.Add(60*time.Minute))
			_ = store.RecordMatchResults(ctx, "guid-7", 11, []storage.PlayerOutcome{
				{PlayerID: p3.PlayerID, Platform: p3.Platform, PlayerName: p3.PlayerName, IsTeammate: false, Won: true},
			}, baseTime.Add(70*time.Minute))
			_ = store.RecordMatchResults(ctx, "guid-8", 11, []storage.PlayerOutcome{
				{PlayerID: p3.PlayerID, Platform: p3.Platform, PlayerName: p3.PlayerName, IsTeammate: false, Won: false},
			}, baseTime.Add(80*time.Minute))

			// Match 9: P4 single match as opponent
			_ = store.RecordMatchResults(ctx, "guid-9", 11, []storage.PlayerOutcome{
				{PlayerID: p4.PlayerID, Platform: p4.Platform, PlayerName: p4.PlayerName, IsTeammate: false, Won: true},
			}, baseTime.Add(90*time.Minute))

			t.Run("Filter_Role_All", func(t *testing.T) {
				list, total, err := store.FilterPlayerSummaries(ctx, storage.PlayerDirectoryFilter{
					Role:  "all",
					Limit: 10,
				})
				if err != nil {
					t.Fatalf("FilterPlayerSummaries failed: %v", err)
				}
				if total != 4 || len(list) != 4 {
					t.Errorf("expected 4 players, got total=%d, len=%d", total, len(list))
				}
			})

			t.Run("Filter_Role_Frequent", func(t *testing.T) {
				// Frequent requires >= 3 total matches (P2=3, P3=4, but P1=2, P4=1)
				list, total, err := store.FilterPlayerSummaries(ctx, storage.PlayerDirectoryFilter{
					Role:  "frequent",
					Limit: 10,
				})
				if err != nil {
					t.Fatalf("FilterPlayerSummaries failed: %v", err)
				}
				if total != 2 || len(list) != 2 {
					t.Errorf("expected 2 frequent players, got total=%d, len=%d", total, len(list))
				}
				for _, p := range list {
					if p.PlayerID == p4.PlayerID || p.PlayerID == p1.PlayerID {
						t.Errorf("P4/P1 should not be in frequent role list, found %s", p.PlayerID)
					}
				}
			})

			t.Run("Filter_Role_Teammate", func(t *testing.T) {
				// Teammate requires TotalWinsAsTeammate + TotalLossesAsTeammate > 0 (P1 and P3)
				list, total, err := store.FilterPlayerSummaries(ctx, storage.PlayerDirectoryFilter{
					Role:  "teammate",
					Limit: 10,
				})
				if err != nil {
					t.Fatalf("FilterPlayerSummaries failed: %v", err)
				}
				if total != 2 || len(list) != 2 {
					t.Errorf("expected 2 teammate players, got total=%d, len=%d", total, len(list))
				}
			})

			t.Run("Filter_Role_Opponent", func(t *testing.T) {
				// Opponent requires TotalWinsAsOpponent + TotalLossesAsOpponent > 0 (P2, P3, P4)
				list, total, err := store.FilterPlayerSummaries(ctx, storage.PlayerDirectoryFilter{
					Role:  "opponent",
					Limit: 10,
				})
				if err != nil {
					t.Fatalf("FilterPlayerSummaries failed: %v", err)
				}
				if total != 3 || len(list) != 3 {
					t.Errorf("expected 3 opponent players, got total=%d, len=%d", total, len(list))
				}
			})

			t.Run("Filter_Role_Synergy", func(t *testing.T) {
				// Synergy requires teammate games >= 2 AND teammate win rate > 50%
				// P1: 2-0 (100% > 50%) -> YES
				// P3: 1-1 (50% not > 50%) -> NO
				list, total, err := store.FilterPlayerSummaries(ctx, storage.PlayerDirectoryFilter{
					Role:  "synergy",
					Limit: 10,
				})
				if err != nil {
					t.Fatalf("FilterPlayerSummaries failed: %v", err)
				}
				if total != 1 || len(list) != 1 {
					t.Fatalf("expected 1 synergy player, got total=%d, len=%d", total, len(list))
				}
				if list[0].PlayerID != p1.PlayerID {
					t.Errorf("expected synergy player %s, got %s", p1.PlayerID, list[0].PlayerID)
				}
			})

			t.Run("Sort_MatchCount_Desc", func(t *testing.T) {
				list, _, err := store.FilterPlayerSummaries(ctx, storage.PlayerDirectoryFilter{
					Sort:  "match_count",
					Order: "desc",
					Limit: 10,
				})
				if err != nil {
					t.Fatalf("FilterPlayerSummaries failed: %v", err)
				}
				if len(list) != 4 {
					t.Fatalf("expected 4 players, got %d", len(list))
				}
				if list[0].PlayerID != p3.PlayerID { // P3 has 4 matches
					t.Errorf("expected top player by match_count to be P3 (4 matches), got %s (%d)", list[0].PlayerID, list[0].TotalMatches)
				}
			})

			t.Run("Sort_Name_Asc", func(t *testing.T) {
				list, _, err := store.FilterPlayerSummaries(ctx, storage.PlayerDirectoryFilter{
					Sort:  "name",
					Order: "asc",
					Limit: 10,
				})
				if err != nil {
					t.Fatalf("FilterPlayerSummaries failed: %v", err)
				}
				if len(list) != 4 {
					t.Fatalf("expected 4 players, got %d", len(list))
				}
				// Alphabetical: AceTeammate, CasualFriend, SoloGuy, ToughRival
				if list[0].PlayerName != "AceTeammate" {
					t.Errorf("expected first alphabetical player 'AceTeammate', got '%s'", list[0].PlayerName)
				}
			})

			t.Run("GlobalSpotlights", func(t *testing.T) {
				spotlights, err := store.GetPlayerSpotlights(ctx)
				if err != nil {
					t.Fatalf("GetPlayerSpotlights failed: %v", err)
				}
				if spotlights == nil {
					t.Fatal("expected non-nil spotlights")
				}

				// Best Teammate: P1 (2-0, 100%)
				if spotlights.BestTeammate == nil || spotlights.BestTeammate.Player == nil {
					t.Fatalf("expected BestTeammate spotlight")
				}
				if spotlights.BestTeammate.Player.PlayerID != p1.PlayerID {
					t.Errorf("expected best teammate %s, got %s", p1.PlayerID, spotlights.BestTeammate.Player.PlayerID)
				}
				if spotlights.BestTeammate.Value != "100%" {
					t.Errorf("expected best teammate value 100%%, got %s", spotlights.BestTeammate.Value)
				}

				// Most Faced: P2 (3 games as opponent)
				if spotlights.MostFaced == nil || spotlights.MostFaced.Player == nil {
					t.Fatalf("expected MostFaced spotlight")
				}
				if spotlights.MostFaced.Player.PlayerID != p2.PlayerID {
					t.Errorf("expected most faced %s, got %s", p2.PlayerID, spotlights.MostFaced.Player.PlayerID)
				}

				// Toughest Rival: P2 (we won 1, lost 2 -> 33% win rate vs P2, or lowest win rate)
				if spotlights.ToughestRival == nil || spotlights.ToughestRival.Player == nil {
					t.Fatalf("expected ToughestRival spotlight")
				}
				if spotlights.ToughestRival.Player.PlayerID != p2.PlayerID {
					t.Errorf("expected toughest rival %s, got %s", p2.PlayerID, spotlights.ToughestRival.Player.PlayerID)
				}
			})

			t.Run("GetLocalPlayerIDs", func(t *testing.T) {
				// Save auth state
				_ = store.SaveAuthState(ctx, "epic", "refresh-token-1", "my-account-123", "EpicGamer")

				ids, err := store.GetLocalPlayerIDs(ctx)
				if err != nil {
					t.Fatalf("GetLocalPlayerIDs failed: %v", err)
				}
				found := false
				for _, id := range ids {
					if id == "my-account-123" {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected 'my-account-123' in local player IDs, got %v", ids)
				}
			})
		})
	}
}

func TestM3_Storage_ListHistoricalMatches_ResultFiltering(t *testing.T) {
	backends := []struct {
		name   string
		isJSON bool
	}{
		{"SQLite", false},
		{"JSONStore", true},
	}

	for _, b := range backends {
		t.Run(b.name, func(t *testing.T) {
			store := newTestStoreBackend(t, b.isJSON)
			ctx := context.Background()

			// Authenticate local player
			_ = store.SaveAuthState(ctx, "steam", "dummy-refresh", "76561198000000000", "LocalUser")

			now := time.Now().UTC()
			winnerBlue := 0
			winnerOrange := 1

			_ = store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
				MatchGUID:   "guid-m1",
				MapName:     "dfh_stadium",
				Playlist:    11,
				BlueScore:   3,
				OrangeScore: 1,
				WinnerTeam:  &winnerBlue,
				Date:        now.Add(-5 * time.Minute).Format(time.RFC3339),
			})
			_ = store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
				MatchGUID:   "guid-m2",
				MapName:     "mannfield",
				Playlist:    11,
				BlueScore:   1,
				OrangeScore: 4,
				WinnerTeam:  &winnerOrange,
				Date:        now.Add(-3 * time.Minute).Format(time.RFC3339),
			})
			_ = store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
				MatchGUID:   "guid-m3",
				MapName:     "utopia_coliseum",
				Playlist:    11,
				BlueScore:   2,
				OrangeScore: 5,
				WinnerTeam:  &winnerOrange,
				Date:        now.Add(-1 * time.Minute).Format(time.RFC3339),
			})

			// Add match players linking local player to appropriate teams
			_ = store.SaveMatchPlayers(ctx, "guid-m1", []*storage.MatchPlayerRecord{
				{MatchGUID: "guid-m1", PlayerID: "Steam|76561198000000000|0", TeamNum: 0, IsLocal: true},
			})
			_ = store.SaveMatchPlayers(ctx, "guid-m2", []*storage.MatchPlayerRecord{
				{MatchGUID: "guid-m2", PlayerID: "Steam|76561198000000000|0", TeamNum: 0, IsLocal: true},
			})
			_ = store.SaveMatchPlayers(ctx, "guid-m3", []*storage.MatchPlayerRecord{
				{MatchGUID: "guid-m3", PlayerID: "Steam|76561198000000000|0", TeamNum: 1, IsLocal: true},
			})

			// 1. Filter Result = "victory"
			victories, totalV, err := store.ListHistoricalMatches(ctx, storage.MatchFilter{
				Result: "victory",
			})
			if err != nil {
				t.Fatalf("ListHistoricalMatches(victory) failed: %v", err)
			}
			if totalV != 2 || len(victories) != 2 {
				t.Fatalf("expected 2 victories, got total=%d, len=%d", totalV, len(victories))
			}
			for _, v := range victories {
				if v.Result != "victory" {
					t.Errorf("expected match %s to have result 'victory', got '%s'", v.MatchGUID, v.Result)
				}
			}

			// 2. Filter Result = "defeat"
			defeats, totalD, err := store.ListHistoricalMatches(ctx, storage.MatchFilter{
				Result: "defeat",
			})
			if err != nil {
				t.Fatalf("ListHistoricalMatches(defeat) failed: %v", err)
			}
			if totalD != 1 || len(defeats) != 1 {
				t.Fatalf("expected 1 defeat, got total=%d, len=%d", totalD, len(defeats))
			}
			if defeats[0].MatchGUID != "guid-m2" || defeats[0].Result != "defeat" {
				t.Errorf("expected match guid-m2 defeat, got %s %s", defeats[0].MatchGUID, defeats[0].Result)
			}

			// 3. Filter Result = "all" or empty
			allMatches, totalAll, err := store.ListHistoricalMatches(ctx, storage.MatchFilter{
				Result: "all",
			})
			if err != nil {
				t.Fatalf("ListHistoricalMatches(all) failed: %v", err)
			}
			if totalAll != 3 || len(allMatches) != 3 {
				t.Fatalf("expected 3 total matches, got total=%d, len=%d", totalAll, len(allMatches))
			}
		})
	}
}

func TestM3_Storage_ListPlayerMatches_Filtering(t *testing.T) {
	backends := []struct {
		name   string
		isJSON bool
	}{
		{"SQLite", false},
		{"JSONStore", true},
	}

	for _, b := range backends {
		t.Run(b.name, func(t *testing.T) {
			store := newTestStoreBackend(t, b.isJSON)
			ctx := context.Background()

			targetPID := "Steam|76561198000000099|0"
			_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
				PlayerID:   targetPID,
				Platform:   "Steam",
				PlayerName: "TargetFriend",
			})

			now := time.Now().UTC()
			winnerBlue := 0
			winnerOrange := 1

			// Match 1: Teammate win
			m1 := &storage.MatchRecord{
				MatchGUID:            "guid-p1",
				RecordStartTimestamp: now.Unix() - 200,
				Playlist:             11,
				BlueScore:            4,
				OrangeScore:          2,
				WinnerTeam:           &winnerBlue,
				CreatedAt:            now.Add(-2 * time.Minute),
				UpdatedAt:            now.Add(-2 * time.Minute),
			}
			// Match 2: Opponent loss (we lost to them)
			m2 := &storage.MatchRecord{
				MatchGUID:            "guid-p2",
				RecordStartTimestamp: now.Unix() - 100,
				Playlist:             11,
				BlueScore:            1,
				OrangeScore:          3,
				WinnerTeam:           &winnerOrange,
				CreatedAt:            now.Add(-1 * time.Minute),
				UpdatedAt:            now.Add(-1 * time.Minute),
			}

			_ = store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{m1, m2})

			// In match 1: local player is Blue (0), target player is Blue (0) -> Teammate
			wonTrue := true
			_ = store.SaveMatchPlayers(ctx, "guid-p1", []*storage.MatchPlayerRecord{
				{MatchGUID: "guid-p1", PlayerID: "Steam|local|0", TeamNum: 0, IsLocal: true, Won: &wonTrue, Score: 500},
				{MatchGUID: "guid-p1", PlayerID: targetPID, TeamNum: 0, IsLocal: false, Won: &wonTrue, Score: 400, Goals: 2},
			})

			// In match 2: local player is Blue (0), target player is Orange (1) -> Opponent
			wonFalse := false
			_ = store.SaveMatchPlayers(ctx, "guid-p2", []*storage.MatchPlayerRecord{
				{MatchGUID: "guid-p2", PlayerID: "Steam|local|0", TeamNum: 0, IsLocal: true, Won: &wonFalse, Score: 300},
				{MatchGUID: "guid-p2", PlayerID: targetPID, TeamNum: 1, IsLocal: false, Won: &wonTrue, Score: 600, Goals: 3},
			})

			// Filter by role: teammate
			tmMatches, tmTotal, err := store.ListPlayerMatches(ctx, targetPID, storage.PlayerMatchFilter{
				Role: "teammate",
			})
			if err != nil {
				t.Fatalf("ListPlayerMatches(teammate) failed: %v", err)
			}
			if tmTotal != 1 || len(tmMatches) != 1 {
				t.Fatalf("expected 1 teammate match, got total=%d, len=%d", tmTotal, len(tmMatches))
			}
			if tmMatches[0].Role != "teammate" {
				t.Errorf("expected role 'teammate', got '%s'", tmMatches[0].Role)
			}

			// Filter by role: opponent
			oppMatches, oppTotal, err := store.ListPlayerMatches(ctx, targetPID, storage.PlayerMatchFilter{
				Role: "opponent",
			})
			if err != nil {
				t.Fatalf("ListPlayerMatches(opponent) failed: %v", err)
			}
			if oppTotal != 1 || len(oppMatches) != 1 {
				t.Fatalf("expected 1 opponent match, got total=%d, len=%d", oppTotal, len(oppMatches))
			}
			if oppMatches[0].Role != "opponent" {
				t.Errorf("expected role 'opponent', got '%s'", oppMatches[0].Role)
			}
		})
	}
}

// TestM3_Remediation_StorageParityAndOutcomes verifies the 3 storage remediations:
// 1. Outcome classification prioritizing score comparisons when winner_team is unset (-1)
// 2. Exact sort=win_rate&order=asc parity between SQLiteStore and JSONStore
// 3. Spotlight determinism with tied rates, game counts, and timestamps
func TestM3_Remediation_StorageParityAndOutcomes(t *testing.T) {
	backends := []struct {
		name   string
		isJSON bool
	}{
		{"SQLite", false},
		{"JSONStore", true},
	}

	for _, b := range backends {
		t.Run(b.name+"_OutcomeScorePrioritization", func(t *testing.T) {
			store := newTestStoreBackend(t, b.isJSON)
			ctx := context.Background()
			localPID := "Steam|76561198000000001|0"
			_ = store.SaveAuthState(ctx, "steam", "test-token", localPID, "Hero")

			now := time.Now().UTC()

			// m1: Blue wins 4-1, Local on Blue -> VICTORY
			_ = store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
				MatchGUID:   "rem-m1-blue-win",
				Playlist:    11,
				BlueScore:   4,
				OrangeScore: 1,
				Date:        now.Add(-40 * time.Minute).Format(time.RFC3339),
			})
			_ = store.SaveMatchPlayers(ctx, "rem-m1-blue-win", []*storage.MatchPlayerRecord{
				{MatchGUID: "rem-m1-blue-win", PlayerID: localPID, TeamNum: 0, IsLocal: true},
			})

			// m2: Orange wins 4-1, Local on Orange -> VICTORY
			_ = store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
				MatchGUID:   "rem-m2-orange-win",
				Playlist:    11,
				BlueScore:   1,
				OrangeScore: 4,
				Date:        now.Add(-30 * time.Minute).Format(time.RFC3339),
			})
			_ = store.SaveMatchPlayers(ctx, "rem-m2-orange-win", []*storage.MatchPlayerRecord{
				{MatchGUID: "rem-m2-orange-win", PlayerID: localPID, TeamNum: 1, IsLocal: true},
			})

			// m3: Orange wins 4-1, Local on Blue -> DEFEAT
			_ = store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
				MatchGUID:   "rem-m3-blue-loss",
				Playlist:    11,
				BlueScore:   1,
				OrangeScore: 4,
				Date:        now.Add(-20 * time.Minute).Format(time.RFC3339),
			})
			_ = store.SaveMatchPlayers(ctx, "rem-m3-blue-loss", []*storage.MatchPlayerRecord{
				{MatchGUID: "rem-m3-blue-loss", PlayerID: localPID, TeamNum: 0, IsLocal: true},
			})

			// m4: Tied 2-2, Local on Blue -> DRAW
			_ = store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
				MatchGUID:   "rem-m4-draw",
				Playlist:    11,
				BlueScore:   2,
				OrangeScore: 2,
				Date:        now.Add(-10 * time.Minute).Format(time.RFC3339),
			})
			_ = store.SaveMatchPlayers(ctx, "rem-m4-draw", []*storage.MatchPlayerRecord{
				{MatchGUID: "rem-m4-draw", PlayerID: localPID, TeamNum: 0, IsLocal: true},
			})

			// Verify result=victory filter
			vics, totalV, err := store.ListHistoricalMatches(ctx, storage.MatchFilter{Result: "victory"})
			if err != nil {
				t.Fatalf("[%s] ListHistoricalMatches(victory) failed: %v", b.name, err)
			}
			if totalV != 2 || len(vics) != 2 {
				t.Errorf("[%s] expected 2 victories, got total=%d, len=%d", b.name, totalV, len(vics))
			}

			// Verify result=defeat filter
			defs, totalD, err := store.ListHistoricalMatches(ctx, storage.MatchFilter{Result: "defeat"})
			if err != nil {
				t.Fatalf("[%s] ListHistoricalMatches(defeat) failed: %v", b.name, err)
			}
			if totalD != 1 || len(defs) != 1 {
				t.Errorf("[%s] expected 1 defeat, got total=%d, len=%d", b.name, totalD, len(defs))
			}

			// Verify result=draw filter
			draws, totalDraw, err := store.ListHistoricalMatches(ctx, storage.MatchFilter{Result: "draw"})
			if err != nil {
				t.Fatalf("[%s] ListHistoricalMatches(draw) failed: %v", b.name, err)
			}
			if totalDraw != 1 || len(draws) != 1 {
				t.Errorf("[%s] expected 1 draw, got total=%d, len=%d", b.name, totalDraw, len(draws))
			}
		})

		t.Run(b.name+"_WinRateSortParityAndSpotlightDeterminism", func(t *testing.T) {
			store := newTestStoreBackend(t, b.isJSON)
			ctx := context.Background()
			now := time.Now().UTC()

			// Two players with 50% win rate:
			// Player A: 1 win, 1 loss -> 2 matches
			pA := &storage.PlayerRecord{
				PlayerID:   "Steam|pA_two_matches|0",
				PlayerName: "PlayerFew",
				Platform:   "Steam",
				LastSeenAt: now,
			}
			_ = store.UpsertPlayer(ctx, pA)
			_ = store.RecordMatchResults(ctx, "m-sort-1", 11, []storage.PlayerOutcome{
				{PlayerID: pA.PlayerID, Platform: pA.Platform, PlayerName: pA.PlayerName, IsTeammate: true, Won: true},
			}, now)
			_ = store.RecordMatchResults(ctx, "m-sort-2", 11, []storage.PlayerOutcome{
				{PlayerID: pA.PlayerID, Platform: pA.Platform, PlayerName: pA.PlayerName, IsTeammate: true, Won: false},
			}, now)

			// Player B: 5 wins, 5 losses -> 10 matches
			pB := &storage.PlayerRecord{
				PlayerID:   "Steam|pB_ten_matches|0",
				PlayerName: "PlayerMany",
				Platform:   "Steam",
				LastSeenAt: now,
			}
			_ = store.UpsertPlayer(ctx, pB)
			for i := 0; i < 5; i++ {
				_ = store.RecordMatchResults(ctx, "m-sort-b-win-"+string(rune('a'+i)), 11, []storage.PlayerOutcome{
					{PlayerID: pB.PlayerID, Platform: pB.Platform, PlayerName: pB.PlayerName, IsTeammate: true, Won: true},
				}, now)
				_ = store.RecordMatchResults(ctx, "m-sort-b-loss-"+string(rune('a'+i)), 11, []storage.PlayerOutcome{
					{PlayerID: pB.PlayerID, Platform: pB.Platform, PlayerName: pB.PlayerName, IsTeammate: true, Won: false},
				}, now)
			}

			// When sort=win_rate&order=asc: tied rates break with total_matches ASC (pA first)
			ascList, _, err := store.FilterPlayerSummaries(ctx, storage.PlayerDirectoryFilter{
				Sort:  "win_rate",
				Order: "asc",
				Limit: 10,
			})
			if err != nil {
				t.Fatalf("[%s] FilterPlayerSummaries asc failed: %v", b.name, err)
			}
			if len(ascList) < 2 {
				t.Fatalf("[%s] expected at least 2 players, got %d", b.name, len(ascList))
			}
			if ascList[0].PlayerID != pA.PlayerID {
				t.Errorf("[%s] expected pA with fewer matches first under order=asc, got %s", b.name, ascList[0].PlayerID)
			}

			// When sort=win_rate&order=desc: tied rates break with total_matches DESC (pB first)
			descList, _, err := store.FilterPlayerSummaries(ctx, storage.PlayerDirectoryFilter{
				Sort:  "win_rate",
				Order: "desc",
				Limit: 10,
			})
			if err != nil {
				t.Fatalf("[%s] FilterPlayerSummaries desc failed: %v", b.name, err)
			}
			if len(descList) < 2 {
				t.Fatalf("[%s] expected at least 2 players, got %d", b.name, len(descList))
			}
			if descList[0].PlayerID != pB.PlayerID {
				t.Errorf("[%s] expected pB with more matches first under order=desc, got %s", b.name, descList[0].PlayerID)
			}

			// Verify spotlight determinism over repeated calls
			for i := 0; i < 10; i++ {
				sp, err := store.GetPlayerSpotlights(ctx)
				if err != nil {
					t.Fatalf("[%s] GetPlayerSpotlights failed: %v", b.name, err)
				}
				if sp.BestTeammate == nil || sp.BestTeammate.Player == nil {
					t.Fatalf("[%s] expected BestTeammate", b.name)
				}
				// Player B has 10 matches (50%) vs Player A 2 matches (50%), so Player B wins on game count
				if sp.BestTeammate.Player.PlayerID != pB.PlayerID {
					t.Errorf("[%s] expected BestTeammate to be pB, got %s", b.name, sp.BestTeammate.Player.PlayerID)
				}
			}
		})
	}
}

