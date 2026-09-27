package storage_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/storage"
)

// searchTestStoreEntry represents a named storage backend under test.
type searchTestStoreEntry struct {
	name    string
	factory func(t *testing.T) storage.StateStore
}

// getSearchTestStores provides both SQLiteStore and JSONStore backends for parameterized testing.
func getSearchTestStores(t *testing.T) []searchTestStoreEntry {
	t.Helper()
	return []searchTestStoreEntry{
		{
			name: "SQLiteStore",
			factory: func(t *testing.T) storage.StateStore {
				dbPath := filepath.Join(t.TempDir(), "search_test.db")
				s, err := storage.NewSQLiteStore(dbPath)
				if err != nil {
					t.Fatalf("failed to create sqlite store: %v", err)
				}
				t.Cleanup(func() { _ = s.Close() })
				return s
			},
		},
		{
			name: "JSONStore",
			factory: func(t *testing.T) storage.StateStore {
				jsonPath := filepath.Join(t.TempDir(), "search_test.json")
				s, err := storage.NewJSONStore(jsonPath)
				if err != nil {
					t.Fatalf("failed to create json store: %v", err)
				}
				t.Cleanup(func() { _ = s.Close() })
				return s
			},
		},
	}
}

// seedSearchTestDataset seeds a comprehensive 16-player dataset into the given StateStore.
// All timestamps use second resolution to maintain bitwise parity between SQLite and JSONStore.
func seedSearchTestDataset(t *testing.T, store storage.StateStore) {
	t.Helper()
	ctx := context.Background()

	// Discrete base time truncated to seconds (zero nanoseconds)
	baseTime := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)

	type seedPlayer struct {
		id       string
		platform string
		name     string
		offsetM  int
	}

	players := []seedPlayer{
		{"Steam|76561198000000001|0", "Steam", "SquishyMuffinz", 100},
		{"Steam|76561198000000002|0", "Steam", "GarrettG", 90},
		{"Steam|76561198000000003|0", "Steam", "Jstn", 80},
		{"Epic|00010000000000000000000000000001|0", "Epic", "Alpha54", 70},
		{"Epic|00010000000000000000000000000002|0", "Epic", "Vatira", 60},
		{"Xbox|XBox_Daniel_01|0", "Xbox", "Daniel", 50},
		{"PS4|PSN_Firstkiller_01|0", "PS4", "Firstkiller", 40},
		{"Steam|76561198000000099|0", "Steam", "pro_player_99", 30},
		{"Epic|00020000000000000000000000000099|0", "Epic", "pro-player-99", 20},
		{"Steam|76561198000000100|0", "Steam", "win_100%_king", 10},
		{"Epic|00020000000000000000000000000100|0", "Epic", "win_1000_king", 5},
		{"Steam|76561198000000101|0", "Steam", "user\\slash", 4},
		{"Steam|76561198000000102|0", "Steam", "O'Connor", 3},
		{"Steam|76561198000000103|0", "Steam", "player\"quote\"", 2},
		{"Steam|76561198000000104|0", "Steam", "TieBreaker_A", 1},
		{"Steam|76561198000000105|0", "Steam", "TieBreaker_B", 1}, // Same timestamp as 104; sorts after by ID ASC
	}

	// 1. First upsert all players with deterministic timestamps so FirstSeenAt is set to ts
	for _, p := range players {
		ts := baseTime.Add(time.Duration(p.offsetM) * time.Minute)
		err := store.UpsertPlayer(ctx, &storage.PlayerRecord{
			PlayerID:    p.id,
			Platform:    p.platform,
			PlayerName:  p.name,
			FirstSeenAt: ts,
			LastSeenAt:  ts,
			RanksJSON:   `{"11":{"tier":15}}`,
		})
		if err != nil {
			t.Fatalf("failed to initial seed player %s: %v", p.id, err)
		}
	}

	// 2. Ingest match outcomes to generate matchup aggregates
	// Match 1 (playlist 11): Squishy (teammate won)
	_ = store.RecordMatchResults(ctx, "seed-m1", 11, []storage.PlayerOutcome{
		{PlayerID: "Steam|76561198000000001|0", Platform: "Steam", PlayerName: "SquishyMuffinz", IsTeammate: true, Won: true},
	})
	// Match 2 (playlist 11): Squishy (teammate lost), GarrettG (opponent won), Alpha54 (opponent lost)
	_ = store.RecordMatchResults(ctx, "seed-m2", 11, []storage.PlayerOutcome{
		{PlayerID: "Steam|76561198000000001|0", Platform: "Steam", PlayerName: "SquishyMuffinz", IsTeammate: true, Won: false},
		{PlayerID: "Steam|76561198000000002|0", Platform: "Steam", PlayerName: "GarrettG", IsTeammate: false, Won: true},
		{PlayerID: "Epic|00010000000000000000000000000001|0", Platform: "Epic", PlayerName: "Alpha54", IsTeammate: false, Won: false},
	})
	// Match 3 (playlist 13): Jstn (teammate won x2), Vatira (opponent lost)
	_ = store.RecordMatchResults(ctx, "seed-m3", 13, []storage.PlayerOutcome{
		{PlayerID: "Steam|76561198000000003|0", Platform: "Steam", PlayerName: "Jstn", IsTeammate: true, Won: true},
		{PlayerID: "Epic|00010000000000000000000000000002|0", Platform: "Epic", PlayerName: "Vatira", IsTeammate: false, Won: false},
	})
	_ = store.RecordMatchResults(ctx, "seed-m4", 13, []storage.PlayerOutcome{
		{PlayerID: "Steam|76561198000000003|0", Platform: "Steam", PlayerName: "Jstn", IsTeammate: true, Won: true},
	})
	// Match 5 (playlist 11): Vatira (teammate won), pro_player_99 (teammate won), pro-player-99 (teammate lost)
	_ = store.RecordMatchResults(ctx, "seed-m5", 11, []storage.PlayerOutcome{
		{PlayerID: "Epic|00010000000000000000000000000002|0", Platform: "Epic", PlayerName: "Vatira", IsTeammate: true, Won: true},
		{PlayerID: "Steam|76561198000000099|0", Platform: "Steam", PlayerName: "pro_player_99", IsTeammate: true, Won: true},
		{PlayerID: "Epic|00020000000000000000000000000099|0", Platform: "Epic", PlayerName: "pro-player-99", IsTeammate: true, Won: false},
	})
	// Match 6 (playlist 11): Vatira (opponent won)
	_ = store.RecordMatchResults(ctx, "seed-m6", 11, []storage.PlayerOutcome{
		{PlayerID: "Epic|00010000000000000000000000000002|0", Platform: "Epic", PlayerName: "Vatira", IsTeammate: false, Won: true},
	})

	// 3. Set deterministic timestamps and upsert remaining players without matches
	for _, p := range players {
		ts := baseTime.Add(time.Duration(p.offsetM) * time.Minute)
		err := store.UpsertPlayer(ctx, &storage.PlayerRecord{
			PlayerID:    p.id,
			Platform:    p.platform,
			PlayerName:  p.name,
			FirstSeenAt: ts,
			LastSeenAt:  ts,
			RanksJSON:   `{"11":{"tier":15}}`,
		})
		if err != nil {
			t.Fatalf("failed to seed player %s: %v", p.id, err)
		}
	}
}

func TestStore_SearchPlayerSummaries_DisplayNameSubstring(t *testing.T) {
	for _, storeTest := range getSearchTestStores(t) {
		t.Run(storeTest.name, func(t *testing.T) {
			store := storeTest.factory(t)
			seedSearchTestDataset(t, store)
			ctx := context.Background()

			// 1. Exact / case-insensitive search
			res, total, err := store.SearchPlayerSummaries(ctx, "squishy", "", 10, 0)
			if err != nil {
				t.Fatalf("search failed: %v", err)
			}
			if total != 1 || len(res) != 1 || res[0].PlayerName != "SquishyMuffinz" {
				t.Errorf("expected 1 result SquishyMuffinz, got total=%d, len=%d", total, len(res))
			}

			// 2. Uppercase search against CamelCase
			res, total, err = store.SearchPlayerSummaries(ctx, "GARRETT", "", 10, 0)
			if err != nil {
				t.Fatalf("search failed: %v", err)
			}
			if total != 1 || len(res) != 1 || res[0].PlayerName != "GarrettG" {
				t.Errorf("expected 1 result GarrettG, got total=%d, len=%d", total, len(res))
			}

			// 3. Substring matching multiple players
			res, total, err = store.SearchPlayerSummaries(ctx, "king", "", 10, 0)
			if err != nil {
				t.Fatalf("search failed: %v", err)
			}
			if total != 2 || len(res) != 2 {
				t.Errorf("expected 2 results for 'king', got total=%d, len=%d", total, len(res))
			}

			// 4. Non-matching query
			res, total, err = store.SearchPlayerSummaries(ctx, "NonExistentPlayerXYZ", "", 10, 0)
			if err != nil {
				t.Fatalf("search failed: %v", err)
			}
			if total != 0 || len(res) != 0 {
				t.Errorf("expected 0 results, got total=%d, len=%d", total, len(res))
			}
		})
	}
}

func TestStore_SearchPlayerSummaries_PlatformIDSubstring(t *testing.T) {
	for _, storeTest := range getSearchTestStores(t) {
		t.Run(storeTest.name, func(t *testing.T) {
			store := storeTest.factory(t)
			seedSearchTestDataset(t, store)
			ctx := context.Background()

			// 1. Steam ID substring matching all 10 Steam players
			res, total, err := store.SearchPlayerSummaries(ctx, "76561198", "", 20, 0)
			if err != nil {
				t.Fatalf("search failed: %v", err)
			}
			if total != 10 || len(res) != 10 {
				t.Errorf("expected 10 Steam results, got total=%d, len=%d", total, len(res))
			}

			// 2. Epic prefix matching all 4 Epic players
			res, total, err = store.SearchPlayerSummaries(ctx, "Epic|", "", 10, 0)
			if err != nil {
				t.Fatalf("search failed: %v", err)
			}
			if total != 4 || len(res) != 4 {
				t.Errorf("expected 4 Epic results, got total=%d, len=%d", total, len(res))
			}

			// 3. Xbox case-insensitive substring of ID
			res, total, err = store.SearchPlayerSummaries(ctx, "xbox_daniel", "", 10, 0)
			if err != nil {
				t.Fatalf("search failed: %v", err)
			}
			if total != 1 || len(res) != 1 || res[0].PlayerID != "Xbox|XBox_Daniel_01|0" {
				t.Errorf("expected Xbox|XBox_Daniel_01|0, got total=%d, res=%+v", total, res)
			}
		})
	}
}

func TestStore_SearchPlayerSummaries_CombinedNameAndIDMatching(t *testing.T) {
	for _, storeTest := range getSearchTestStores(t) {
		t.Run(storeTest.name, func(t *testing.T) {
			store := storeTest.factory(t)
			seedSearchTestDataset(t, store)
			ctx := context.Background()

			// Query where both name and ID match for the same player (player returned once)
			res, total, err := store.SearchPlayerSummaries(ctx, "Daniel", "", 10, 0)
			if err != nil {
				t.Fatalf("search failed: %v", err)
			}
			if total != 1 || len(res) != 1 {
				t.Errorf("expected exactly 1 result for Daniel, got total=%d, len=%d", total, len(res))
			}
			if res[0].PlayerID != "Xbox|XBox_Daniel_01|0" {
				t.Errorf("unexpected player returned: %+v", res[0])
			}
		})
	}
}

func TestStore_SearchPlayerSummaries_PlatformFilterIsolation(t *testing.T) {
	for _, storeTest := range getSearchTestStores(t) {
		t.Run(storeTest.name, func(t *testing.T) {
			store := storeTest.factory(t)
			seedSearchTestDataset(t, store)
			ctx := context.Background()

			// 1. Steam filter (case variations)
			for _, plat := range []string{"Steam", "steam", "STEAM"} {
				res, total, err := store.SearchPlayerSummaries(ctx, "", plat, 50, 0)
				if err != nil {
					t.Fatalf("platform %s search failed: %v", plat, err)
				}
				if total != 10 || len(res) != 10 {
					t.Errorf("plat=%s: expected 10 players, got total=%d, len=%d", plat, total, len(res))
				}
				for _, r := range res {
					if r.Platform != "Steam" {
						t.Errorf("plat=%s: leaked non-Steam player %+v", plat, r)
					}
				}
			}

			// 2. Epic filter
			res, total, err := store.SearchPlayerSummaries(ctx, "", "Epic", 50, 0)
			if err != nil || total != 4 || len(res) != 4 {
				t.Errorf("Epic filter: expected 4, got total=%d, len=%d, err=%v", total, len(res), err)
			}

			// 3. Universal "all" and empty filter
			for _, plat := range []string{"", "all", "ALL", "All"} {
				res, total, err := store.SearchPlayerSummaries(ctx, "", plat, 50, 0)
				if err != nil || total != 16 || len(res) != 16 {
					t.Errorf("universal plat=%s: expected 16, got total=%d, len=%d, err=%v", plat, total, len(res), err)
				}
			}

			// 4. Non-existent platform
			res, total, err = store.SearchPlayerSummaries(ctx, "", "NintendoSwitch", 50, 0)
			if err != nil || total != 0 || len(res) != 0 {
				t.Errorf("non-existent platform: expected 0, got total=%d, len=%d, err=%v", total, len(res), err)
			}
		})
	}
}

func TestStore_SearchPlayerSummaries_CombinedQueryAndPlatform(t *testing.T) {
	for _, storeTest := range getSearchTestStores(t) {
		t.Run(storeTest.name, func(t *testing.T) {
			store := storeTest.factory(t)
			seedSearchTestDataset(t, store)
			ctx := context.Background()

			// Query "pro" + Platform "Steam"
			res, total, err := store.SearchPlayerSummaries(ctx, "pro", "Steam", 10, 0)
			if err != nil || total != 1 || len(res) != 1 || res[0].PlayerID != "Steam|76561198000000099|0" {
				t.Errorf("pro+Steam: expected 1 Steam player, got total=%d, res=%+v, err=%v", total, res, err)
			}

			// Query "pro" + Platform "Epic"
			res, total, err = store.SearchPlayerSummaries(ctx, "pro", "Epic", 10, 0)
			if err != nil || total != 1 || len(res) != 1 || res[0].PlayerID != "Epic|00020000000000000000000000000099|0" {
				t.Errorf("pro+Epic: expected 1 Epic player, got total=%d, res=%+v, err=%v", total, res, err)
			}

			// Query "Squishy" + Platform "Epic" (should be empty)
			res, total, err = store.SearchPlayerSummaries(ctx, "Squishy", "Epic", 10, 0)
			if err != nil || total != 0 || len(res) != 0 {
				t.Errorf("Squishy+Epic: expected 0, got total=%d, len=%d, err=%v", total, len(res), err)
			}
		})
	}
}

func TestStore_SearchPlayerSummaries_Pagination(t *testing.T) {
	for _, storeTest := range getSearchTestStores(t) {
		t.Run(storeTest.name, func(t *testing.T) {
			store := storeTest.factory(t)
			seedSearchTestDataset(t, store)
			ctx := context.Background()

			// Baseline: Full scan
			fullList, fullTotal, err := store.SearchPlayerSummaries(ctx, "", "", -1, 0)
			if err != nil || fullTotal != 16 || len(fullList) != 16 {
				t.Fatalf("full scan failed: total=%d, len=%d, err=%v", fullTotal, len(fullList), err)
			}

			// Page 1: limit 5, offset 0
			p1, t1, err := store.SearchPlayerSummaries(ctx, "", "", 5, 0)
			if err != nil || t1 != 16 || len(p1) != 5 {
				t.Fatalf("page 1 failed: total=%d, len=%d, err=%v", t1, len(p1), err)
			}

			// Page 2: limit 5, offset 5
			p2, t2, err := store.SearchPlayerSummaries(ctx, "", "", 5, 5)
			if err != nil || t2 != 16 || len(p2) != 5 {
				t.Fatalf("page 2 failed: total=%d, len=%d, err=%v", t2, len(p2), err)
			}

			// Page 3: limit 5, offset 10
			p3, t3, err := store.SearchPlayerSummaries(ctx, "", "", 5, 10)
			if err != nil || t3 != 16 || len(p3) != 5 {
				t.Fatalf("page 3 failed: total=%d, len=%d, err=%v", t3, len(p3), err)
			}

			// Page 4: limit 5, offset 15 (partial final page)
			p4, t4, err := store.SearchPlayerSummaries(ctx, "", "", 5, 15)
			if err != nil || t4 != 16 || len(p4) != 1 {
				t.Fatalf("page 4 failed: total=%d, len=%d, err=%v", t4, len(p4), err)
			}

			// Page 5: limit 5, offset 20 (beyond total)
			p5, t5, err := store.SearchPlayerSummaries(ctx, "", "", 5, 20)
			if err != nil || t5 != 16 || len(p5) != 0 {
				t.Fatalf("page 5 failed: total=%d, len=%d, err=%v", t5, len(p5), err)
			}

			// Verify continuity: concatenated pages must equal full scan
			allPages := append(append(append(p1, p2...), p3...), p4...)
			for i := 0; i < 16; i++ {
				if allPages[i].PlayerID != fullList[i].PlayerID {
					t.Errorf("pagination sequence mismatch at index %d: page=%s, full=%s",
						i, allPages[i].PlayerID, fullList[i].PlayerID)
				}
			}
		})
	}
}

func TestStore_SearchPlayerSummaries_TotalCountAccuracy(t *testing.T) {
	for _, storeTest := range getSearchTestStores(t) {
		t.Run(storeTest.name, func(t *testing.T) {
			store := storeTest.factory(t)
			seedSearchTestDataset(t, store)
			ctx := context.Background()

			// Query "king" matches 2 players
			tests := []struct {
				limit  int
				offset int
				expLen int
			}{
				{1, 0, 1},
				{1, 1, 1},
				{1, 2, 0},
				{0, 0, 0},
				{-1, 0, 2},
				{50, 0, 2},
			}

			for _, tt := range tests {
				res, total, err := store.SearchPlayerSummaries(ctx, "king", "", tt.limit, tt.offset)
				if err != nil {
					t.Fatalf("search failed for limit=%d, offset=%d: %v", tt.limit, tt.offset, err)
				}
				if total != 2 {
					t.Errorf("limit=%d, offset=%d: expected total=2, got %d", tt.limit, tt.offset, total)
				}
				if len(res) != tt.expLen {
					t.Errorf("limit=%d, offset=%d: expected len=%d, got %d", tt.limit, tt.offset, tt.expLen, len(res))
				}
			}
		})
	}
}

func TestStore_SearchPlayerSummaries_ZeroAndNegativeLimit(t *testing.T) {
	for _, storeTest := range getSearchTestStores(t) {
		t.Run(storeTest.name, func(t *testing.T) {
			store := storeTest.factory(t)
			seedSearchTestDataset(t, store)
			ctx := context.Background()

			// Limit 0: count-only query
			res, total, err := store.SearchPlayerSummaries(ctx, "", "", 0, 0)
			if err != nil || total != 16 || len(res) != 0 || res == nil {
				t.Errorf("limit=0: expected total=16, non-nil empty slice, got len=%d, total=%d, err=%v", len(res), total, err)
			}

			// Negative limit: fetch all
			res, total, err = store.SearchPlayerSummaries(ctx, "", "", -1, 0)
			if err != nil || total != 16 || len(res) != 16 {
				t.Errorf("limit=-1: expected total=16, len=16, got total=%d, len=%d, err=%v", total, len(res), err)
			}

			// Negative limit with offset
			res, total, err = store.SearchPlayerSummaries(ctx, "", "", -1, 10)
			if err != nil || total != 16 || len(res) != 6 {
				t.Errorf("limit=-1, offset=10: expected len=6, total=16, got len=%d, total=%d, err=%v", len(res), total, err)
			}
		})
	}
}

func TestStore_SearchPlayerSummaries_OffsetNormalization(t *testing.T) {
	for _, storeTest := range getSearchTestStores(t) {
		t.Run(storeTest.name, func(t *testing.T) {
			store := storeTest.factory(t)
			seedSearchTestDataset(t, store)
			ctx := context.Background()

			// Negative offset: normalized to 0
			res, total, err := store.SearchPlayerSummaries(ctx, "", "", 5, -10)
			if err != nil || total != 16 || len(res) != 5 {
				t.Errorf("negative offset: expected len=5, total=16, got len=%d, total=%d, err=%v", len(res), total, err)
			}

			// Out-of-bounds offset
			res, total, err = store.SearchPlayerSummaries(ctx, "", "", 5, 9999)
			if err != nil || total != 16 || len(res) != 0 {
				t.Errorf("extreme offset: expected len=0, total=16, got len=%d, total=%d, err=%v", len(res), total, err)
			}
		})
	}
}

func TestStore_SearchPlayerSummaries_WildcardLiteralMatching(t *testing.T) {
	for _, storeTest := range getSearchTestStores(t) {
		t.Run(storeTest.name, func(t *testing.T) {
			store := storeTest.factory(t)
			seedSearchTestDataset(t, store)
			ctx := context.Background()

			// 1. Literal underscore matching: "pro_player" must match "pro_player_99" and NOT "pro-player-99"
			res, total, err := store.SearchPlayerSummaries(ctx, "pro_player", "", 10, 0)
			if err != nil {
				t.Fatalf("search failed: %v", err)
			}
			if total != 1 || len(res) != 1 || res[0].PlayerID != "Steam|76561198000000099|0" {
				t.Errorf("literal underscore failed: expected pro_player_99, got total=%d, res=%+v", total, res)
			}

			// 2. Literal percent matching: "100%" must match "win_100%_king" and NOT "win_1000_king"
			res, total, err = store.SearchPlayerSummaries(ctx, "100%", "", 10, 0)
			if err != nil {
				t.Fatalf("search failed: %v", err)
			}
			if total != 1 || len(res) != 1 || res[0].PlayerID != "Steam|76561198000000100|0" {
				t.Errorf("literal percent failed: expected win_100%%_king, got total=%d, res=%+v", total, res)
			}

			// 3. Literal backslash matching: "user\slash"
			res, total, err = store.SearchPlayerSummaries(ctx, "user\\slash", "", 10, 0)
			if err != nil {
				t.Fatalf("search failed: %v", err)
			}
			if total != 1 || len(res) != 1 || res[0].PlayerID != "Steam|76561198000000101|0" {
				t.Errorf("literal backslash failed: expected user\\slash, got total=%d, res=%+v", total, res)
			}

			// 4. Single quote in query (SQL injection / syntax resilience)
			res, total, err = store.SearchPlayerSummaries(ctx, "O'Connor", "", 10, 0)
			if err != nil {
				t.Fatalf("single quote search failed: %v", err)
			}
			if total != 1 || len(res) != 1 || res[0].PlayerName != "O'Connor" {
				t.Errorf("single quote search failed: expected O'Connor, got total=%d, res=%+v", total, res)
			}

			// 5. Double quote in query
			res, total, err = store.SearchPlayerSummaries(ctx, "player\"quote\"", "", 10, 0)
			if err != nil {
				t.Fatalf("double quote search failed: %v", err)
			}
			if total != 1 || len(res) != 1 || res[0].PlayerName != "player\"quote\"" {
				t.Errorf("double quote search failed: expected player\"quote\", got total=%d, res=%+v", total, res)
			}
		})
	}
}

func TestStore_SearchPlayerSummaries_EmptyAndWhitespaceQueries(t *testing.T) {
	for _, storeTest := range getSearchTestStores(t) {
		t.Run(storeTest.name, func(t *testing.T) {
			store := storeTest.factory(t)
			seedSearchTestDataset(t, store)
			ctx := context.Background()

			for _, q := range []string{"", "   ", "\t\n  \r\n"} {
				res, total, err := store.SearchPlayerSummaries(ctx, q, "", 50, 0)
				if err != nil {
					t.Fatalf("query %q failed: %v", q, err)
				}
				if total != 16 || len(res) != 16 {
					t.Errorf("query %q: expected 16 results, got total=%d, len=%d", q, total, len(res))
				}
			}
		})
	}
}

func TestStore_SearchPlayerSummaries_MatchupAggregation(t *testing.T) {
	for _, storeTest := range getSearchTestStores(t) {
		t.Run(storeTest.name, func(t *testing.T) {
			store := storeTest.factory(t)
			seedSearchTestDataset(t, store)
			ctx := context.Background()

			// 1. Player with zero matchups (Daniel)
			res, total, err := store.SearchPlayerSummaries(ctx, "Daniel", "", 10, 0)
			if err != nil || total != 1 || len(res) != 1 {
				t.Fatalf("search Daniel failed: total=%d, len=%d, err=%v", total, len(res), err)
			}
			dan := res[0]
			if dan.TotalMatches != 0 || dan.TotalWinsAsTeammate != 0 || dan.TotalLossesAsTeammate != 0 ||
				dan.TotalWinsAsOpponent != 0 || dan.TotalLossesAsOpponent != 0 {
				t.Errorf("Daniel expected 0 aggregate matches, got %+v", dan)
			}

			// 2. Player with single playlist matches (SquishyMuffinz)
			res, _, _ = store.SearchPlayerSummaries(ctx, "Squishy", "", 10, 0)
			sq := res[0]
			if sq.TotalWinsAsTeammate != 1 || sq.TotalLossesAsTeammate != 1 || sq.TotalMatches != 2 {
				t.Errorf("Squishy unexpected aggregate: tmW=%d, tmL=%d, total=%d",
					sq.TotalWinsAsTeammate, sq.TotalLossesAsTeammate, sq.TotalMatches)
			}

			// 3. Player with multi-playlist matches (Vatira)
			res, _, _ = store.SearchPlayerSummaries(ctx, "Vatira", "", 10, 0)
			vat := res[0]
			if vat.TotalWinsAsTeammate != 1 || vat.TotalLossesAsTeammate != 0 ||
				vat.TotalWinsAsOpponent != 1 || vat.TotalLossesAsOpponent != 1 || vat.TotalMatches != 3 {
				t.Errorf("Vatira unexpected aggregate: tmW=%d, tmL=%d, opW=%d, opL=%d, total=%d",
					vat.TotalWinsAsTeammate, vat.TotalLossesAsTeammate, vat.TotalWinsAsOpponent, vat.TotalLossesAsOpponent, vat.TotalMatches)
			}
		})
	}
}

func TestStore_SearchPlayerSummaries_DeterministicOrdering(t *testing.T) {
	for _, storeTest := range getSearchTestStores(t) {
		t.Run(storeTest.name, func(t *testing.T) {
			store := storeTest.factory(t)
			seedSearchTestDataset(t, store)
			ctx := context.Background()

			// 1. Primary order verification (last_seen_at DESC)
			res, total, err := store.SearchPlayerSummaries(ctx, "", "", -1, 0)
			if err != nil || total != 16 {
				t.Fatalf("full search failed: %v", err)
			}
			for i := 0; i < len(res)-1; i++ {
				if res[i].LastSeenAt.Before(res[i+1].LastSeenAt) {
					t.Fatalf("ordering violated at index %d: %s (%v) before %s (%v)",
						i, res[i].PlayerID, res[i].LastSeenAt, res[i+1].PlayerID, res[i+1].LastSeenAt)
				}
			}

			// 2. Secondary tie-breaker verification (player_id ASC)
			resTie, totalTie, err := store.SearchPlayerSummaries(ctx, "TieBreaker", "", 10, 0)
			if err != nil || totalTie != 2 || len(resTie) != 2 {
				t.Fatalf("TieBreaker search failed: total=%d, len=%d, err=%v", totalTie, len(resTie), err)
			}
			if resTie[0].PlayerID != "Steam|76561198000000104|0" || resTie[1].PlayerID != "Steam|76561198000000105|0" {
				t.Errorf("tie-breaker violated: expected [104, 105], got [%s, %s]",
					resTie[0].PlayerID, resTie[1].PlayerID)
			}
		})
	}
}

func TestStore_SearchPlayerSummaries_ContextCancellation(t *testing.T) {
	for _, storeTest := range getSearchTestStores(t) {
		t.Run(storeTest.name, func(t *testing.T) {
			store := storeTest.factory(t)
			seedSearchTestDataset(t, store)

			ctx, cancel := context.WithCancel(context.Background())
			cancel() // Cancel context immediately before calling

			res, total, err := store.SearchPlayerSummaries(ctx, "", "", 10, 0)
			if err == nil {
				t.Fatal("expected error on cancelled context, got nil")
			}
			if !errors.Is(err, context.Canceled) {
				t.Errorf("expected context.Canceled, got %v", err)
			}
			if res != nil || total != 0 {
				t.Errorf("expected nil results and 0 total, got len=%d, total=%d", len(res), total)
			}
		})
	}
}

func TestStore_SearchPlayerSummaries_ClosedStore(t *testing.T) {
	for _, storeTest := range getSearchTestStores(t) {
		t.Run(storeTest.name, func(t *testing.T) {
			store := storeTest.factory(t)
			seedSearchTestDataset(t, store)

			if err := store.Close(); err != nil {
				t.Fatalf("failed to close store: %v", err)
			}

			ctx := context.Background()
			_, _, err := store.SearchPlayerSummaries(ctx, "", "", 10, 0)
			if err == nil {
				t.Fatal("expected error calling SearchPlayerSummaries on closed store, got nil")
			}
		})
	}
}

func TestStore_SearchPlayerSummaries_DeepCopyDefense(t *testing.T) {
	for _, storeTest := range getSearchTestStores(t) {
		t.Run(storeTest.name, func(t *testing.T) {
			store := storeTest.factory(t)
			seedSearchTestDataset(t, store)
			ctx := context.Background()

			res, total, err := store.SearchPlayerSummaries(ctx, "Squishy", "", 1, 0)
			if err != nil || total != 1 || len(res) != 1 {
				t.Fatalf("initial query failed: %v", err)
			}

			// Mutate returned struct
			res[0].PlayerName = "MutatedName"
			res[0].TotalMatches = 9999

			// Re-query: internal store state must remain untouched
			res2, total2, err := store.SearchPlayerSummaries(ctx, "Squishy", "", 1, 0)
			if err != nil || total2 != 1 || len(res2) != 1 {
				t.Fatalf("secondary query failed: %v", err)
			}
			if res2[0].PlayerName != "SquishyMuffinz" || res2[0].TotalMatches != 2 {
				t.Errorf("deep copy failure: internal record mutated to name=%s, matches=%d",
					res2[0].PlayerName, res2[0].TotalMatches)
			}
		})
	}
}

func TestStore_SearchPlayerSummaries_ConcurrencyUnderRace(t *testing.T) {
	for _, storeTest := range getSearchTestStores(t) {
		t.Run(storeTest.name, func(t *testing.T) {
			store := storeTest.factory(t)
			seedSearchTestDataset(t, store)
			ctx := context.Background()

			const (
				numReaders = 20
				numWriters = 5
			)
			var wg sync.WaitGroup
			errCh := make(chan error, (numReaders+numWriters)*10)

			// Readers
			for i := 0; i < numReaders; i++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					queries := []string{"", "squishy", "king", "pro", "Daniel", "76561198"}
					platforms := []string{"", "all", "Steam", "Epic"}
					for j := 0; j < 10; j++ {
						q := queries[(id+j)%len(queries)]
						p := platforms[(id+j)%len(platforms)]
						res, total, err := store.SearchPlayerSummaries(ctx, q, p, 5, j%3)
						if err != nil {
							errCh <- fmt.Errorf("reader %d query failed: %w", id, err)
							return
						}
						if total < 0 || len(res) < 0 {
							errCh <- fmt.Errorf("reader %d returned negative count", id)
							return
						}
					}
				}(i)
			}

			// Writers
			for i := 0; i < numWriters; i++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					for j := 0; j < 5; j++ {
						pid := fmt.Sprintf("Steam|race_%d_%d|0", id, j)
						_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
							PlayerID:   pid,
							Platform:   "Steam",
							PlayerName: fmt.Sprintf("RacePlayer_%d", id),
						})
						_ = store.RecordMatchResults(ctx, fmt.Sprintf("race-m-%d-%d", id, j), 11, []storage.PlayerOutcome{
							{PlayerID: pid, IsTeammate: true, Won: true},
						})
					}
				}(i)
			}

			wg.Wait()
			close(errCh)

			for err := range errCh {
				t.Errorf("concurrency race error: %v", err)
			}
		})
	}
}

func TestCrossStore_SearchPlayerSummaries_ParityMatrix(t *testing.T) {
	ctx := context.Background()

	// Initialize fresh instances of both SQLiteStore and JSONStore
	sqlPath := filepath.Join(t.TempDir(), "parity.db")
	sqlStore, err := storage.NewSQLiteStore(sqlPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer sqlStore.Close()

	jsonPath := filepath.Join(t.TempDir(), "parity.json")
	jsonStore, err := storage.NewJSONStore(jsonPath)
	if err != nil {
		t.Fatalf("failed to create json store: %v", err)
	}
	defer jsonStore.Close()

	// Seed identical data
	seedSearchTestDataset(t, sqlStore)
	seedSearchTestDataset(t, jsonStore)

	type testVector struct {
		query    string
		platform string
		limit    int
		offset   int
	}

	vectors := []testVector{
		{"", "", 10, 0},
		{"", "all", 10, 0},
		{"", "ALL", 10, 0},
		{"   ", "", 10, 0},
		{"squishy", "", 10, 0},
		{"SQUISHY", "", 10, 0},
		{"sQuIsHy", "", 10, 0},
		{"pro_player", "", 10, 0},
		{"pro-player", "", 10, 0},
		{"100%", "", 10, 0},
		{"1000", "", 10, 0},
		{"user\\slash", "", 10, 0},
		{"O'Connor", "", 10, 0},
		{"player\"quote\"", "", 10, 0},
		{"76561198", "", 10, 0},
		{"Epic|", "", 10, 0},
		{"TieBreaker", "", 10, 0},
		{"NonExistent", "", 10, 0},
		{"", "Steam", 20, 0},
		{"", "steam", 20, 0},
		{"", "Epic", 20, 0},
		{"", "Xbox", 20, 0},
		{"", "PS4", 20, 0},
		{"", "NintendoSwitch", 20, 0},
		{"pro", "Steam", 10, 0},
		{"pro", "Epic", 10, 0},
		{"pro", "all", 10, 0},
		{"", "", 5, 0},
		{"", "", 5, 5},
		{"", "", 5, 10},
		{"", "", 5, 15},
		{"", "", 5, 20},
		{"", "", 0, 0},
		{"", "", -1, 0},
		{"", "", -1, 5},
		{"", "", 5, -5},
	}

	for idx, v := range vectors {
		testName := fmt.Sprintf("Vector_%02d_Q[%s]_P[%s]_L[%d]_O[%d]", idx, v.query, v.platform, v.limit, v.offset)
		t.Run(testName, func(t *testing.T) {
			sqlRes, sqlTotal, sqlErr := sqlStore.SearchPlayerSummaries(ctx, v.query, v.platform, v.limit, v.offset)
			jsonRes, jsonTotal, jsonErr := jsonStore.SearchPlayerSummaries(ctx, v.query, v.platform, v.limit, v.offset)

			if (sqlErr == nil) != (jsonErr == nil) {
				t.Fatalf("error parity divergence: sqlErr=%v, jsonErr=%v", sqlErr, jsonErr)
			}
			if sqlTotal != jsonTotal {
				t.Fatalf("total count parity divergence: sqlTotal=%d, jsonTotal=%d", sqlTotal, jsonTotal)
			}
			if len(sqlRes) != len(jsonRes) {
				t.Fatalf("results length parity divergence: len(sql)=%d, len(json)=%d", len(sqlRes), len(jsonRes))
			}

			for i := 0; i < len(sqlRes); i++ {
				s := sqlRes[i]
				j := jsonRes[i]

				if s.PlayerID != j.PlayerID {
					t.Fatalf("item[%d] PlayerID mismatch: sql=%s, json=%s", i, s.PlayerID, j.PlayerID)
				}
				if s.Platform != j.Platform {
					t.Errorf("item[%d] Platform mismatch: sql=%s, json=%s", i, s.Platform, j.Platform)
				}
				if s.PlayerName != j.PlayerName {
					t.Errorf("item[%d] PlayerName mismatch: sql=%s, json=%s", i, s.PlayerName, j.PlayerName)
				}
				if s.RanksJSON != j.RanksJSON {
					t.Errorf("item[%d] RanksJSON mismatch: sql=%s, json=%s", i, s.RanksJSON, j.RanksJSON)
				}
				if s.FirstSeenAt.Unix() != j.FirstSeenAt.Unix() {
					t.Errorf("item[%d] FirstSeenAt mismatch: sql=%v, json=%v", i, s.FirstSeenAt, j.FirstSeenAt)
				}
				if s.LastSeenAt.Unix() != j.LastSeenAt.Unix() {
					t.Errorf("item[%d] LastSeenAt mismatch: sql=%v, json=%v", i, s.LastSeenAt, j.LastSeenAt)
				}
				if s.TotalWinsAsTeammate != j.TotalWinsAsTeammate {
					t.Errorf("item[%d] TotalWinsAsTeammate mismatch: sql=%d, json=%d", i, s.TotalWinsAsTeammate, j.TotalWinsAsTeammate)
				}
				if s.TotalLossesAsTeammate != j.TotalLossesAsTeammate {
					t.Errorf("item[%d] TotalLossesAsTeammate mismatch: sql=%d, json=%d", i, s.TotalLossesAsTeammate, j.TotalLossesAsTeammate)
				}
				if s.TotalWinsAsOpponent != j.TotalWinsAsOpponent {
					t.Errorf("item[%d] TotalWinsAsOpponent mismatch: sql=%d, json=%d", i, s.TotalWinsAsOpponent, j.TotalWinsAsOpponent)
				}
				if s.TotalLossesAsOpponent != j.TotalLossesAsOpponent {
					t.Errorf("item[%d] TotalLossesAsOpponent mismatch: sql=%d, json=%d", i, s.TotalLossesAsOpponent, j.TotalLossesAsOpponent)
				}
				if s.TotalMatches != j.TotalMatches {
					t.Errorf("item[%d] TotalMatches mismatch: sql=%d, json=%d", i, s.TotalMatches, j.TotalMatches)
				}
			}
		})
	}
}
