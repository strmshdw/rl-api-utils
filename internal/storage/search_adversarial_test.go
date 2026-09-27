package storage_test

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/storage"
)

// ============================================================================
// 1. SQL INJECTION ADVERSARIAL VECTORS
// ============================================================================

func TestAdversarial_SearchPlayerSummaries_SQLInjection(t *testing.T) {
	for _, storeEntry := range getSearchTestStores(t) {
		t.Run(storeEntry.name, func(t *testing.T) {
			store := storeEntry.factory(t)
			ctx := context.Background()

			// Seed legitimate test players
			baseTime := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
			legitPlayers := []*storage.PlayerRecord{
				{
					PlayerID:    "Steam|76561198000000001|0",
					Platform:    "Steam",
					PlayerName:  "LegitPlayerOne",
					FirstSeenAt: baseTime,
					LastSeenAt:  baseTime,
					RanksJSON:   "{}",
				},
				{
					PlayerID:    "Epic|00010000000000000000000000000001|0",
					Platform:    "Epic",
					PlayerName:  "LegitPlayerTwo",
					FirstSeenAt: baseTime.Add(time.Minute),
					LastSeenAt:  baseTime.Add(time.Minute),
					RanksJSON:   "{}",
				},
			}
			for _, p := range legitPlayers {
				if err := store.UpsertPlayer(ctx, p); err != nil {
					t.Fatalf("failed to seed legit player: %v", err)
				}
			}

			// Also seed a player whose literal name contains a malicious SQL string
			evilPlayer := &storage.PlayerRecord{
				PlayerID:    "Steam|76561198999999999|0",
				Platform:    "Steam",
				PlayerName:  "'; DROP TABLE players; --",
				FirstSeenAt: baseTime.Add(2 * time.Minute),
				LastSeenAt:  baseTime.Add(2 * time.Minute),
				RanksJSON:   "{}",
			}
			if err := store.UpsertPlayer(ctx, evilPlayer); err != nil {
				t.Fatalf("failed to seed evil player: %v", err)
			}

			sqlInjectionVectors := []struct {
				name        string
				query       string
				platform    string
				expectMatch bool
				matchID     string
			}{
				{
					name:        "DropTableAttempt_Query",
					query:       "'; DROP TABLE players; --",
					platform:    "",
					expectMatch: true, // Matches evilPlayer literally!
					matchID:     evilPlayer.PlayerID,
				},
				{
					name:        "AlwaysTrue_OR_Query",
					query:       "' OR '1'='1",
					platform:    "",
					expectMatch: false,
				},
				{
					name:        "AlwaysTrue_OR_Numeric_Query",
					query:       "' OR 1=1 --",
					platform:    "",
					expectMatch: false,
				},
				{
					name:        "UnionSelect_Query",
					query:       "' UNION SELECT null, null, null, null, null, null, null, null, null, null, null --",
					platform:    "",
					expectMatch: false,
				},
				{
					name:        "DeleteFrom_Query",
					query:       "'; DELETE FROM players WHERE '1'='1'; --",
					platform:    "",
					expectMatch: false,
				},
				{
					name:        "UpdateSet_Query",
					query:       "'; UPDATE players SET player_name='hacked'; --",
					platform:    "",
					expectMatch: false,
				},
				{
					name:        "DropTableAttempt_Platform",
					query:       "Legit",
					platform:    "'; DROP TABLE players; --",
					expectMatch: false,
				},
				{
					name:        "AlwaysTrue_Platform",
					query:       "Legit",
					platform:    "' OR '1'='1",
					expectMatch: false,
				},
				{
					name:        "UnionSelect_Platform",
					query:       "",
					platform:    "' UNION SELECT 'a', 'b', 'c', '{}', 1, 1 --",
					expectMatch: false,
				},
				{
					name:        "CommentTermination_Query",
					query:       "admin'--",
					platform:    "",
					expectMatch: false,
				},
				{
					name:        "StackedSubquery_Query",
					query:       "(SELECT player_name FROM players LIMIT 1)",
					platform:    "",
					expectMatch: false,
				},
			}

			for _, tc := range sqlInjectionVectors {
				t.Run(tc.name, func(t *testing.T) {
					results, total, err := store.SearchPlayerSummaries(ctx, tc.query, tc.platform, 50, 0)
					if err != nil {
						t.Fatalf("unexpected error executing injection vector %q: %v", tc.name, err)
					}

					// Verify database is completely intact
					p1, err := store.GetPlayer(ctx, legitPlayers[0].PlayerID)
					if err != nil || p1 == nil {
						t.Fatalf("database corrupted or table dropped! GetPlayer failed: %v", err)
					}
					if p1.PlayerName != "LegitPlayerOne" {
						t.Fatalf("player data tampered with! expected 'LegitPlayerOne', got %q", p1.PlayerName)
					}

					if tc.expectMatch {
						if total != 1 || len(results) != 1 {
							t.Fatalf("expected literal match for %q, got total=%d, results=%d", tc.query, total, len(results))
						}
						if results[0].PlayerID != tc.matchID {
							t.Fatalf("expected match ID %q, got %q", tc.matchID, results[0].PlayerID)
						}
					} else {
						if total != 0 || len(results) != 0 {
							t.Fatalf("expected 0 matches for non-matching injection query %q, got total=%d, results=%d", tc.query, total, len(results))
						}
					}
				})
			}
		})
	}
}

// ============================================================================
// 2. COMPLEX WILDCARD ESCAPING COMBINATIONS
// ============================================================================

func TestAdversarial_SearchPlayerSummaries_ComplexWildcards(t *testing.T) {
	for _, storeEntry := range getSearchTestStores(t) {
		t.Run(storeEntry.name, func(t *testing.T) {
			store := storeEntry.factory(t)
			ctx := context.Background()

			baseTime := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
			seeded := []*storage.PlayerRecord{
				{PlayerID: "Steam|W1|0", Platform: "Steam", PlayerName: "wild_%_player", FirstSeenAt: baseTime, LastSeenAt: baseTime, RanksJSON: "{}"},
				{PlayerID: "Steam|W2|0", Platform: "Steam", PlayerName: "wild_1_player", FirstSeenAt: baseTime, LastSeenAt: baseTime, RanksJSON: "{}"},
				{PlayerID: "Steam|W3|0", Platform: "Steam", PlayerName: "wild_a_player", FirstSeenAt: baseTime, LastSeenAt: baseTime, RanksJSON: "{}"},
				{PlayerID: "Steam|W4|0", Platform: "Steam", PlayerName: "wild_%_%_player", FirstSeenAt: baseTime, LastSeenAt: baseTime, RanksJSON: "{}"},
				{PlayerID: "Steam|W5|0", Platform: "Steam", PlayerName: `wild_%_%_\%_boss`, FirstSeenAt: baseTime, LastSeenAt: baseTime, RanksJSON: "{}"},
				{PlayerID: "Steam|W6|0", Platform: "Steam", PlayerName: `slash\backslash`, FirstSeenAt: baseTime, LastSeenAt: baseTime, RanksJSON: "{}"},
				{PlayerID: "Steam|W7|0", Platform: "Steam", PlayerName: `double\\slash`, FirstSeenAt: baseTime, LastSeenAt: baseTime, RanksJSON: "{}"},
				{PlayerID: "Steam|W8|0", Platform: "Steam", PlayerName: "100%_pure", FirstSeenAt: baseTime, LastSeenAt: baseTime, RanksJSON: "{}"},
				{PlayerID: "Steam|W9|0", Platform: "Steam", PlayerName: "1000_pure", FirstSeenAt: baseTime, LastSeenAt: baseTime, RanksJSON: "{}"},
				{PlayerID: "Steam|W10|0", Platform: "Steam", PlayerName: "[bracket%_test]", FirstSeenAt: baseTime, LastSeenAt: baseTime, RanksJSON: "{}"},
				{PlayerID: "Steam|W11|0", Platform: "Steam", PlayerName: "test_underscore", FirstSeenAt: baseTime, LastSeenAt: baseTime, RanksJSON: "{}"},
				{PlayerID: "Steam|W12|0", Platform: "Steam", PlayerName: "testXunderscore", FirstSeenAt: baseTime, LastSeenAt: baseTime, RanksJSON: "{}"},
			}
			for _, p := range seeded {
				if err := store.UpsertPlayer(ctx, p); err != nil {
					t.Fatalf("failed to seed wildcard player: %v", err)
				}
			}

			testCases := []struct {
				name          string
				query         string
				expectedCount int
				expectedIDs   []string
			}{
				{
					name:          "LiteralPercent_OnlyMatchingPlayersWithPercent",
					query:         "%",
					expectedCount: 5, // W1, W4, W5, W8, W10
					expectedIDs:   []string{"Steam|W1|0", "Steam|W4|0", "Steam|W5|0", "Steam|W8|0", "Steam|W10|0"},
				},
				{
					name:          "LiteralUnderscore_DoesNotMatchArbitraryChar",
					query:         "test_underscore",
					expectedCount: 1, // W11 only, NOT W12 (testXunderscore)
					expectedIDs:   []string{"Steam|W11|0"},
				},
				{
					name:          "SingleBackslash",
					query:         `\`,
					expectedCount: 3, // W5, W6, W7
					expectedIDs:   []string{"Steam|W5|0", "Steam|W6|0", "Steam|W7|0"},
				},
				{
					name:          "DoubleBackslash",
					query:         `\\`,
					expectedCount: 1, // W7 only
					expectedIDs:   []string{"Steam|W7|0"},
				},
				{
					name:          "ComplexSequence_PercentUnderscorePercentUnderscoreBackslashPercent",
					query:         `%_%_\%`,
					expectedCount: 1, // W5 only
					expectedIDs:   []string{"Steam|W5|0"},
				},
				{
					name:          "ExactPrefixWithPercent",
					query:         "100%",
					expectedCount: 1, // W8 only, NOT W9 (1000_pure)
					expectedIDs:   []string{"Steam|W8|0"},
				},
				{
					name:          "ExactPrefixWithNumber",
					query:         "1000",
					expectedCount: 1, // W9 only, NOT W8 (100%_pure)
					expectedIDs:   []string{"Steam|W9|0"},
				},
				{
					name:          "MultipleConsecutivePercents",
					query:         "%%",
					expectedCount: 0, // None have "%%" consecutively
					expectedIDs:   []string{},
				},
				{
					name:          "PercentUnderscorePercent",
					query:         "%_%",
					expectedCount: 2, // W4, W5
					expectedIDs:   []string{"Steam|W4|0", "Steam|W5|0"},
				},
				{
					name:          "BracketLiteral",
					query:         "[bracket",
					expectedCount: 1, // W10
					expectedIDs:   []string{"Steam|W10|0"},
				},
			}

			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					results, total, err := store.SearchPlayerSummaries(ctx, tc.query, "", 50, 0)
					if err != nil {
						t.Fatalf("unexpected search error: %v", err)
					}
					if total != tc.expectedCount {
						t.Fatalf("query %q: expected total %d, got %d", tc.query, tc.expectedCount, total)
					}
					if len(results) != tc.expectedCount {
						t.Fatalf("query %q: expected %d results, got %d", tc.query, tc.expectedCount, len(results))
					}

					// Verify expected IDs are present
					matchedIDs := make(map[string]bool)
					for _, r := range results {
						matchedIDs[r.PlayerID] = true
					}
					for _, expID := range tc.expectedIDs {
						if !matchedIDs[expID] {
							t.Errorf("query %q: expected player ID %s not found in results", tc.query, expID)
						}
					}
				})
			}
		})
	}
}

// ============================================================================
// 3. UNICODE, EMOJI, CONTROL CHARACTERS, AND NULL BYTES
// ============================================================================

func TestAdversarial_SearchPlayerSummaries_UnicodeEmojiAndControlChars(t *testing.T) {
	for _, storeEntry := range getSearchTestStores(t) {
		t.Run(storeEntry.name, func(t *testing.T) {
			store := storeEntry.factory(t)
			ctx := context.Background()

			baseTime := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
			seeded := []*storage.PlayerRecord{
				{PlayerID: "Steam|U_JP|0", Platform: "Steam", PlayerName: "山田太郎_プロ", FirstSeenAt: baseTime, LastSeenAt: baseTime, RanksJSON: "{}"},
				{PlayerID: "Steam|U_RU|0", Platform: "Steam", PlayerName: "Иван_Петров", FirstSeenAt: baseTime, LastSeenAt: baseTime, RanksJSON: "{}"},
				{PlayerID: "Steam|U_AR|0", Platform: "Steam", PlayerName: "أحمد_علي_بطل", FirstSeenAt: baseTime, LastSeenAt: baseTime, RanksJSON: "{}"},
				{PlayerID: "Steam|U_EMOJI_1|0", Platform: "Steam", PlayerName: "Rocket🚀Champion🔥", FirstSeenAt: baseTime, LastSeenAt: baseTime, RanksJSON: "{}"},
				{PlayerID: "Steam|U_EMOJI_2|0", Platform: "Steam", PlayerName: "Gamer🎮Zone👾", FirstSeenAt: baseTime, LastSeenAt: baseTime, RanksJSON: "{}"},
				{PlayerID: "Steam|U_ZWJ|0", Platform: "Steam", PlayerName: "Family👨‍👩‍👧‍👦Squad", FirstSeenAt: baseTime, LastSeenAt: baseTime, RanksJSON: "{}"},
				{PlayerID: "Steam|U_CTRL|0", Platform: "Steam", PlayerName: "ctrl\x01\x02\x1f_user", FirstSeenAt: baseTime, LastSeenAt: baseTime, RanksJSON: "{}"},
				{PlayerID: "Steam|U_DEL|0", Platform: "Steam", PlayerName: "del\x7f_user", FirstSeenAt: baseTime, LastSeenAt: baseTime, RanksJSON: "{}"},
				{PlayerID: "Steam|U_NULL|0", Platform: "Steam", PlayerName: "null\x00byte_user", FirstSeenAt: baseTime, LastSeenAt: baseTime, RanksJSON: "{}"},
				{PlayerID: "Steam|U_NULL_2|0", Platform: "Steam", PlayerName: "null_prefix_user", FirstSeenAt: baseTime, LastSeenAt: baseTime, RanksJSON: "{}"},
				{PlayerID: "Steam|U_MULTI_WS|0", Platform: "Steam", PlayerName: "tab\tnewline\nuser", FirstSeenAt: baseTime, LastSeenAt: baseTime, RanksJSON: "{}"},
			}
			for _, p := range seeded {
				if err := store.UpsertPlayer(ctx, p); err != nil {
					t.Fatalf("failed to seed unicode/control player: %v", err)
				}
			}

			testCases := []struct {
				name          string
				query         string
				expectedCount int
				expectedID    string
			}{
				{name: "Japanese_Substring", query: "太郎", expectedCount: 1, expectedID: "Steam|U_JP|0"},
				{name: "Arabic_Substring", query: "أحمد", expectedCount: 1, expectedID: "Steam|U_AR|0"},
				{name: "Cyrillic_Case_Insensitive", query: "иван", expectedCount: 1, expectedID: "Steam|U_RU|0"},
				{name: "Cyrillic_Exact_Case", query: "Иван", expectedCount: 1, expectedID: "Steam|U_RU|0"},
				{name: "Cyrillic_SecondWord_ExactCase", query: "Петров", expectedCount: 1, expectedID: "Steam|U_RU|0"},
				{name: "Cyrillic_SecondWord_LowerCase", query: "петров", expectedCount: 1, expectedID: "Steam|U_RU|0"},
				{name: "Single_Emoji", query: "🚀", expectedCount: 1, expectedID: "Steam|U_EMOJI_1|0"},
				{name: "Second_Emoji", query: "🔥", expectedCount: 1, expectedID: "Steam|U_EMOJI_1|0"},
				{name: "Multiple_Emoji", query: "🎮Zone👾", expectedCount: 1, expectedID: "Steam|U_EMOJI_2|0"},
				{name: "ZWJ_Complex_Emoji", query: "👨‍👩‍👧‍👦", expectedCount: 1, expectedID: "Steam|U_ZWJ|0"},
				{name: "Control_Characters", query: "\x01\x02", expectedCount: 1, expectedID: "Steam|U_CTRL|0"},
				{name: "DEL_Character", query: "\x7f", expectedCount: 1, expectedID: "Steam|U_DEL|0"},
				{name: "Null_Byte", query: "\x00", expectedCount: 1, expectedID: "Steam|U_NULL|0"},
				{name: "Embedded_Null_Byte_Substr", query: "null\x00byte", expectedCount: 1, expectedID: "Steam|U_NULL|0"},
				{name: "Tab_Newline_Literal", query: "tab\tnewline", expectedCount: 1, expectedID: "Steam|U_MULTI_WS|0"},
			}

			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					results, total, err := store.SearchPlayerSummaries(ctx, tc.query, "", 50, 0)
					if err != nil {
						t.Fatalf("search error for query %q: %v", tc.query, err)
					}
					if total != tc.expectedCount {
						t.Fatalf("query %q: expected total %d, got %d", tc.query, tc.expectedCount, total)
					}
					if len(results) != tc.expectedCount {
						t.Fatalf("query %q: expected %d results, got %d", tc.query, tc.expectedCount, len(results))
					}
					if results[0].PlayerID != tc.expectedID {
						t.Fatalf("query %q: expected match ID %s, got %s", tc.query, tc.expectedID, results[0].PlayerID)
					}
				})
			}
		})
	}
}

// ============================================================================
// 4. EXTREME PAGINATION STRESS TESTS
// ============================================================================

func TestAdversarial_SearchPlayerSummaries_ExtremePagination(t *testing.T) {
	for _, storeEntry := range getSearchTestStores(t) {
		t.Run(storeEntry.name, func(t *testing.T) {
			store := storeEntry.factory(t)
			ctx := context.Background()

			baseTime := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
			const numPlayers = 20
			for i := 0; i < numPlayers; i++ {
				p := &storage.PlayerRecord{
					PlayerID:    fmt.Sprintf("Steam|PAG%02d|0", i),
					Platform:    "Steam",
					PlayerName:  fmt.Sprintf("PaginationPlayer%02d", i),
					FirstSeenAt: baseTime.Add(time.Duration(i) * time.Minute),
					LastSeenAt:  baseTime.Add(time.Duration(i) * time.Minute),
					RanksJSON:   "{}",
				}
				if err := store.UpsertPlayer(ctx, p); err != nil {
					t.Fatalf("failed to seed pagination player: %v", err)
				}
			}

			paginationCases := []struct {
				name          string
				limit         int
				offset        int
				expectedTotal int
				expectedLen   int
				firstExpected string // Expected ID of results[0] if len > 0
			}{
				{
					name:          "LimitNegative999_OffsetZero_ReturnsAll",
					limit:         -999,
					offset:        0,
					expectedTotal: 20,
					expectedLen:   20,
					firstExpected: "Steam|PAG19|0", // Highest LastSeenAt
				},
				{
					name:          "LimitNegative1_OffsetZero_ReturnsAll",
					limit:         -1,
					offset:        0,
					expectedTotal: 20,
					expectedLen:   20,
					firstExpected: "Steam|PAG19|0",
				},
				{
					name:          "LimitHuge_OffsetZero_ReturnsAll",
					limit:         100000,
					offset:        0,
					expectedTotal: 20,
					expectedLen:   20,
					firstExpected: "Steam|PAG19|0",
				},
				{
					name:          "LimitHuge_Offset5_ReturnsRemaining",
					limit:         100000,
					offset:        5,
					expectedTotal: 20,
					expectedLen:   15,
					firstExpected: "Steam|PAG14|0",
				},
				{
					name:          "LimitZero_OffsetZero_ReturnsEmptyAccurateTotal",
					limit:         0,
					offset:        0,
					expectedTotal: 20,
					expectedLen:   0,
				},
				{
					name:          "LimitZero_Offset5_ReturnsEmptyAccurateTotal",
					limit:         0,
					offset:        5,
					expectedTotal: 20,
					expectedLen:   0,
				},
				{
					name:          "OffsetNegative5_NormalizesToZero",
					limit:         5,
					offset:        -5,
					expectedTotal: 20,
					expectedLen:   5,
					firstExpected: "Steam|PAG19|0",
				},
				{
					name:          "OffsetNegative999_NormalizesToZero",
					limit:         5,
					offset:        -999,
					expectedTotal: 20,
					expectedLen:   5,
					firstExpected: "Steam|PAG19|0",
				},
				{
					name:          "OffsetBeyondTotal_ReturnsEmptyAccurateTotal",
					limit:         10,
					offset:        1000000,
					expectedTotal: 20,
					expectedLen:   0,
				},
				{
					name:          "LimitNegative_OffsetBeyondTotal_ReturnsEmptyAccurateTotal",
					limit:         -999,
					offset:        1000000,
					expectedTotal: 20,
					expectedLen:   0,
				},
				{
					name:          "OffsetExactlyTotal_ReturnsEmptyAccurateTotal",
					limit:         10,
					offset:        20,
					expectedTotal: 20,
					expectedLen:   0,
				},
				{
					name:          "MaxInt32Limit_OffsetZero",
					limit:         math.MaxInt32,
					offset:        0,
					expectedTotal: 20,
					expectedLen:   20,
					firstExpected: "Steam|PAG19|0",
				},
				{
					name:          "MaxInt32Offset_ReturnsEmpty",
					limit:         10,
					offset:        math.MaxInt32,
					expectedTotal: 20,
					expectedLen:   0,
				},
			}

			for _, tc := range paginationCases {
				t.Run(tc.name, func(t *testing.T) {
					results, total, err := store.SearchPlayerSummaries(ctx, "", "", tc.limit, tc.offset)
					if err != nil {
						t.Fatalf("unexpected pagination search error: %v", err)
					}
					if results == nil {
						t.Fatalf("SearchPlayerSummaries must return non-nil slice on empty results, got nil")
					}
					if total != tc.expectedTotal {
						t.Fatalf("expected total %d, got %d", tc.expectedTotal, total)
					}
					if len(results) != tc.expectedLen {
						t.Fatalf("expected result len %d, got %d", tc.expectedLen, len(results))
					}
					if tc.expectedLen > 0 {
						if results[0].PlayerID != tc.firstExpected {
							t.Fatalf("expected first ID %s, got %s", tc.firstExpected, results[0].PlayerID)
						}
					}
				})
			}
		})
	}
}

// ============================================================================
// 5. LARGE DATASETS WITH IDENTICAL NAMES AND TIMESTAMPS (PARITY & DETERMINISM)
// ============================================================================

func TestAdversarial_SearchPlayerSummaries_LargeDatasetIdenticalNamesAndTimestamps(t *testing.T) {
	const datasetSize = 1000
	const sharedName = "CloneTrooper"
	sharedTime := time.Date(2026, 9, 26, 15, 30, 0, 0, time.UTC)

	// Pre-generate seed records
	seedRecords := make([]*storage.PlayerRecord, datasetSize)
	for i := 0; i < datasetSize; i++ {
		seedRecords[i] = &storage.PlayerRecord{
			PlayerID:    fmt.Sprintf("Steam|CLONE_%04d|0", i),
			Platform:    "Steam",
			PlayerName:  sharedName,
			FirstSeenAt: sharedTime,
			LastSeenAt:  sharedTime,
			RanksJSON:   `{"11":{"tier":16,"division":3,"mmr":1250.5}}`,
		}
	}

	// Instantiate both SQLiteStore and JSONStore
	stores := getSearchTestStores(t)
	var sqliteStore, jsonStore storage.StateStore
	for _, s := range stores {
		if s.name == "SQLiteStore" {
			sqliteStore = s.factory(t)
		} else if s.name == "JSONStore" {
			jsonStore = s.factory(t)
		}
	}

	ctx := context.Background()

	// Seed both stores identically
	for _, p := range seedRecords {
		if err := sqliteStore.UpsertPlayer(ctx, p); err != nil {
			t.Fatalf("failed to seed sqlite: %v", err)
		}
		if err := jsonStore.UpsertPlayer(ctx, p); err != nil {
			t.Fatalf("failed to seed json: %v", err)
		}
	}

	// Add matchup statistics to a subset of players in both stores
	matchOutcomes := []storage.PlayerOutcome{
		{PlayerID: "Steam|CLONE_0042|0", Platform: "Steam", PlayerName: sharedName, IsTeammate: true, Won: true},
		{PlayerID: "Steam|CLONE_0042|0", Platform: "Steam", PlayerName: sharedName, IsTeammate: false, Won: false},
		{PlayerID: "Steam|CLONE_0500|0", Platform: "Steam", PlayerName: sharedName, IsTeammate: false, Won: true},
	}
	if err := sqliteStore.RecordMatchResults(ctx, "adv-match-1", 11, matchOutcomes); err != nil {
		t.Fatalf("failed to record match in sqlite: %v", err)
	}
	if err := jsonStore.RecordMatchResults(ctx, "adv-match-1", 11, matchOutcomes); err != nil {
		t.Fatalf("failed to record match in json: %v", err)
	}

	// Reset LastSeenAt to sharedTime for the matchup players so all 1,000 have the exact same timestamp
	for _, pid := range []string{"Steam|CLONE_0042|0", "Steam|CLONE_0500|0"} {
		p := &storage.PlayerRecord{
			PlayerID:    pid,
			Platform:    "Steam",
			PlayerName:  sharedName,
			FirstSeenAt: sharedTime,
			LastSeenAt:  sharedTime,
			RanksJSON:   `{"11":{"tier":16,"division":3,"mmr":1250.5}}`,
		}
		if err := sqliteStore.UpsertPlayer(ctx, p); err != nil {
			t.Fatalf("failed to reset last seen in sqlite: %v", err)
		}
		if err := jsonStore.UpsertPlayer(ctx, p); err != nil {
			t.Fatalf("failed to reset last seen in json: %v", err)
		}
	}

	// 1. Verify total count
	_, sqTotal, sqErr := sqliteStore.SearchPlayerSummaries(ctx, sharedName, "Steam", 10, 0)
	_, jsTotal, jsErr := jsonStore.SearchPlayerSummaries(ctx, sharedName, "Steam", 10, 0)

	if sqErr != nil || jsErr != nil {
		t.Fatalf("search error: sqlite=%v, json=%v", sqErr, jsErr)
	}
	if sqTotal != datasetSize || jsTotal != datasetSize {
		t.Fatalf("total mismatch: sqlite=%d, json=%d, expected=%d", sqTotal, jsTotal, datasetSize)
	}

	// 2. Paginate through all 1,000 records in batches of 50
	const pageSize = 50
	const totalPages = datasetSize / pageSize

	var allSQLiteIDs []string
	var allJSONIDs []string

	for page := 0; page < totalPages; page++ {
		offset := page * pageSize

		startSq := time.Now()
		pagedSq, totSq, errSq := sqliteStore.SearchPlayerSummaries(ctx, sharedName, "Steam", pageSize, offset)
		sqDuration := time.Since(startSq)

		startJs := time.Now()
		pagedJs, totJs, errJs := jsonStore.SearchPlayerSummaries(ctx, sharedName, "Steam", pageSize, offset)
		jsDuration := time.Since(startJs)

		if errSq != nil || errJs != nil {
			t.Fatalf("page %d search error: sq=%v, js=%v", page, errSq, errJs)
		}
		if totSq != datasetSize || totJs != datasetSize {
			t.Fatalf("page %d total mismatch: sq=%d, js=%d", page, totSq, totJs)
		}
		if len(pagedSq) != pageSize || len(pagedJs) != pageSize {
			t.Fatalf("page %d len mismatch: sq=%d, js=%d, expected=%d", page, len(pagedSq), len(pagedJs), pageSize)
		}

		// Ensure latency is reasonable (< 50ms per page)
		if sqDuration > 200*time.Millisecond || jsDuration > 200*time.Millisecond {
			t.Logf("Notice: Page %d latency: SQLite=%v, JSON=%v", page, sqDuration, jsDuration)
		}

		for i := 0; i < pageSize; i++ {
			sqItem := pagedSq[i]
			jsItem := pagedJs[i]

			allSQLiteIDs = append(allSQLiteIDs, sqItem.PlayerID)
			allJSONIDs = append(allJSONIDs, jsItem.PlayerID)

			// Strict Parity Comparison of every single summary field
			if sqItem.PlayerID != jsItem.PlayerID {
				t.Fatalf("page %d item %d: PlayerID mismatch: sq=%s, js=%s", page, i, sqItem.PlayerID, jsItem.PlayerID)
			}
			if sqItem.PlayerName != jsItem.PlayerName {
				t.Fatalf("page %d item %d: PlayerName mismatch: sq=%s, js=%s", page, i, sqItem.PlayerName, jsItem.PlayerName)
			}
			if sqItem.Platform != jsItem.Platform {
				t.Fatalf("page %d item %d: Platform mismatch: sq=%s, js=%s", page, i, sqItem.Platform, jsItem.Platform)
			}
			if sqItem.RanksJSON != jsItem.RanksJSON {
				t.Fatalf("page %d item %d: RanksJSON mismatch: sq=%s, js=%s", page, i, sqItem.RanksJSON, jsItem.RanksJSON)
			}
			if sqItem.LastSeenAt.Unix() != jsItem.LastSeenAt.Unix() {
				t.Fatalf("page %d item %d: LastSeenAt mismatch: sq=%v, js=%v", page, i, sqItem.LastSeenAt, jsItem.LastSeenAt)
			}
			if sqItem.TotalWinsAsTeammate != jsItem.TotalWinsAsTeammate ||
				sqItem.TotalLossesAsTeammate != jsItem.TotalLossesAsTeammate ||
				sqItem.TotalWinsAsOpponent != jsItem.TotalWinsAsOpponent ||
				sqItem.TotalLossesAsOpponent != jsItem.TotalLossesAsOpponent ||
				sqItem.TotalMatches != jsItem.TotalMatches {
				t.Fatalf("page %d item %d (%s): matchup aggregates mismatch: sq=%+v, js=%+v", page, i, sqItem.PlayerID, sqItem, jsItem)
			}
		}
	}

	// 3. Verify Deterministic Ordering & Completeness across all 1,000 players
	if len(allSQLiteIDs) != datasetSize || len(allJSONIDs) != datasetSize {
		t.Fatalf("total stream size mismatch: sq=%d, js=%d, expected=%d", len(allSQLiteIDs), len(allJSONIDs), datasetSize)
	}

	seenSq := make(map[string]bool)
	seenJs := make(map[string]bool)

	for i := 0; i < datasetSize; i++ {
		expectedID := fmt.Sprintf("Steam|CLONE_%04d|0", i)
		if allSQLiteIDs[i] != expectedID {
			t.Fatalf("sqlite order mismatch at index %d: expected %s, got %s", i, expectedID, allSQLiteIDs[i])
		}
		if allJSONIDs[i] != expectedID {
			t.Fatalf("jsonstore order mismatch at index %d: expected %s, got %s", i, expectedID, allJSONIDs[i])
		}
		seenSq[allSQLiteIDs[i]] = true
		seenJs[allJSONIDs[i]] = true
	}

	if len(seenSq) != datasetSize || len(seenJs) != datasetSize {
		t.Fatalf("duplicates detected in pagination stream! seenSq=%d, seenJs=%d", len(seenSq), len(seenJs))
	}
}

// ============================================================================
// 6. HIGH-CONTENTION CONCURRENT READ/WRITE STRESS TEST
// ============================================================================

func TestAdversarial_SearchPlayerSummaries_ConcurrentReadWriteStress(t *testing.T) {
	for _, storeEntry := range getSearchTestStores(t) {
		t.Run(storeEntry.name, func(t *testing.T) {
			store := storeEntry.factory(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			baseTime := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
			// Seed initial 50 players
			for i := 0; i < 50; i++ {
				_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
					PlayerID:    fmt.Sprintf("Steam|RACE_%03d|0", i),
					Platform:    "Steam",
					PlayerName:  fmt.Sprintf("RacePlayer_%03d", i),
					FirstSeenAt: baseTime,
					LastSeenAt:  baseTime,
					RanksJSON:   "{}",
				})
			}

			var wg sync.WaitGroup
			errCh := make(chan error, 100)

			// 10 concurrent readers with adversarial queries
			adversarialQueries := []string{
				"'; DROP TABLE players; --",
				"' OR '1'='1",
				"%",
				"_",
				`\`,
				`%_%_\\%`,
				"RacePlayer",
				"RACE_",
				"NonExistent",
				"",
			}

			for w := 0; w < 10; w++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					for i := 0; ctx.Err() == nil && i < 100; i++ {
						q := adversarialQueries[i%len(adversarialQueries)]
						limit := (i % 20) - 5 // mixture of negative, zero, positive limits
						offset := (i % 30) - 5
						_, _, err := store.SearchPlayerSummaries(ctx, q, "", limit, offset)
						if err != nil && !strings.Contains(err.Error(), "context canceled") {
							select {
							case errCh <- fmt.Errorf("reader %d query %q error: %w", workerID, q, err):
							default:
							}
							return
						}
					}
				}(w)
			}

			// 5 concurrent writers upserting players and match results
			for w := 0; w < 5; w++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					for i := 0; ctx.Err() == nil && i < 50; i++ {
						pid := fmt.Sprintf("Steam|RACE_%03d|0", (workerID*50+i)%60)
						err := store.UpsertPlayer(ctx, &storage.PlayerRecord{
							PlayerID:    pid,
							Platform:    "Steam",
							PlayerName:  fmt.Sprintf("RacePlayer_%03d", workerID),
							FirstSeenAt: baseTime,
							LastSeenAt:  time.Now().UTC(),
							RanksJSON:   "{}",
						})
						if err != nil && !strings.Contains(err.Error(), "context canceled") {
							select {
							case errCh <- fmt.Errorf("writer %d upsert error: %w", workerID, err):
							default:
							}
							return
						}

						// Record match
						_ = store.RecordMatchResults(ctx, fmt.Sprintf("m-%d-%d", workerID, i), 11, []storage.PlayerOutcome{
							{PlayerID: pid, Platform: "Steam", PlayerName: "Race", IsTeammate: true, Won: i%2 == 0},
						})
					}
				}(w)
			}

			wg.Wait()
			close(errCh)

			for err := range errCh {
				t.Fatalf("concurrency stress failure: %v", err)
			}
		})
	}
}
