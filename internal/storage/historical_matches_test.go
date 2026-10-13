package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestStore_SyncReplayStats_And_HistoricalMatches(t *testing.T) {
	for _, storeType := range []string{"sqlite", "json"} {
		t.Run(storeType, func(t *testing.T) {
			ctx := context.Background()
			var store StateStore
			var err error

			if storeType == "sqlite" {
				store, err = NewSQLiteStore(":memory:")
			} else {
				tmpDir := t.TempDir()
				store, err = NewJSONStore(filepath.Join(tmpDir, "test.json"))
			}
			if err != nil {
				t.Fatalf("failed to init store: %v", err)
			}
			defer store.Close()

			now := time.Now().UTC()
			winner := 0
			stats1 := &ReplayMatchStats{
				MatchGUID:   "guid-hist-1",
				MapName:     "Stadium_P",
				Playlist:    11,
				BlueScore:   3,
				OrangeScore: 1,
				WinnerTeam:  &winner,
				Duration:    300,
				Date:        now.Format(time.RFC3339),
				Players: []ReplayPlayerStats{
					{
						Name:     "LocalPlayer",
						Platform: "Epic",
						OnlineID: "local_id_1",
						Team:     0,
						Score:    450,
						Goals:    2,
						Assists:  1,
						Saves:    1,
						Shots:    3,
					},
					{
						Name:     "Opponent1",
						Platform: "Steam",
						OnlineID: "76561198000000001",
						Team:     1,
						Score:    300,
						Goals:    1,
						Assists:  0,
						Saves:    2,
						Shots:    2,
					},
				},
				BallchasingID:  "bc-id-1",
				BallchasingURL: "https://ballchasing.com/replay/bc-id-1",
			}

			if err := store.SyncReplayStats(ctx, stats1); err != nil {
				t.Fatalf("SyncReplayStats failed: %v", err)
			}

			// Verify player was inserted
			p, err := store.GetPlayer(ctx, "Epic|local_id_1|0")
			if err != nil {
				t.Fatalf("GetPlayer failed: %v", err)
			}
			if p.PlayerName != "LocalPlayer" {
				t.Errorf("expected LocalPlayer, got %s", p.PlayerName)
			}

			// Add second match with Orange win
			winnerOrange := 1
			stats2 := &ReplayMatchStats{
				MatchGUID:   "guid-hist-2",
				MapName:     "UtopiaStadium_P",
				Playlist:    13,
				BlueScore:   2,
				OrangeScore: 4,
				WinnerTeam:  &winnerOrange,
				Duration:    320,
				Date:        now.Add(-1 * time.Hour).Format(time.RFC3339),
				Players: []ReplayPlayerStats{
					{
						Name:     "LocalPlayer",
						Platform: "Epic",
						OnlineID: "local_id_1",
						Team:     0,
						Score:    280,
						Goals:    1,
					},
				},
			}

			if err := store.SyncReplayStats(ctx, stats2); err != nil {
				t.Fatalf("SyncReplayStats match 2 failed: %v", err)
			}

			// Query all historical matches
			matches, total, err := store.ListHistoricalMatches(ctx, MatchFilter{Limit: 10})
			if err != nil {
				t.Fatalf("ListHistoricalMatches failed: %v", err)
			}
			if total != 2 {
				t.Errorf("expected total 2, got %d", total)
			}
			if len(matches) != 2 {
				t.Fatalf("expected 2 matches, got %d", len(matches))
			}

			// Verify ordering: newest first (guid-hist-1, then guid-hist-2)
			if matches[0].MatchGUID != "guid-hist-1" {
				t.Errorf("expected guid-hist-1 first, got %s", matches[0].MatchGUID)
			}
			if matches[0].BlueScore != 3 || matches[0].OrangeScore != 1 {
				t.Errorf("expected score 3-1, got %d-%d", matches[0].BlueScore, matches[0].OrangeScore)
			}
			if matches[0].Result != "victory" {
				t.Errorf("expected result victory, got %s", matches[0].Result)
			}
			if matches[0].BallchasingID != "bc-id-1" {
				t.Errorf("expected bc-id-1, got %s", matches[0].BallchasingID)
			}
			if matches[0].Stats == nil || len(matches[0].Stats.Players) != 2 {
				t.Errorf("expected 2 players in stats, got %v", matches[0].Stats)
			}

			// Filter by Playlist 13
			p13Matches, p13Total, err := store.ListHistoricalMatches(ctx, MatchFilter{Playlist: 13, Limit: 10})
			if err != nil {
				t.Fatalf("ListHistoricalMatches playlist 13 failed: %v", err)
			}
			if p13Total != 1 || len(p13Matches) != 1 || p13Matches[0].MatchGUID != "guid-hist-2" {
				t.Errorf("expected only guid-hist-2 for playlist 13, got total %d", p13Total)
			}

			// Search by query "Utopia"
			searchMatches, searchTotal, err := store.ListHistoricalMatches(ctx, MatchFilter{Query: "Utopia", Limit: 10})
			if err != nil {
				t.Fatalf("search failed: %v", err)
			}
			if searchTotal != 1 || searchMatches[0].MatchGUID != "guid-hist-2" {
				t.Errorf("expected Utopia search match, got %d", searchTotal)
			}

			// Search by query "Opponent1" (found in replay_stats_json)
			oppMatches, oppTotal, err := store.ListHistoricalMatches(ctx, MatchFilter{Query: "Opponent1", Limit: 10})
			if err != nil {
				t.Fatalf("search by player failed: %v", err)
			}
			if oppTotal != 1 || oppMatches[0].MatchGUID != "guid-hist-1" {
				t.Errorf("expected Opponent1 match, got %d", oppTotal)
			}
		})
	}
}

func TestStore_SyncReplayStats_EdgeCases(t *testing.T) {
	ctx := context.Background()
	store, _ := NewSQLiteStore(":memory:")
	defer store.Close()

	// Nil stats
	if err := store.SyncReplayStats(ctx, nil); err != ErrInvalidGUID {
		t.Errorf("expected ErrInvalidGUID on nil stats, got %v", err)
	}

	// Empty GUID
	if err := store.SyncReplayStats(ctx, &ReplayMatchStats{MatchGUID: "   "}); err != ErrInvalidGUID {
		t.Errorf("expected ErrInvalidGUID on empty GUID, got %v", err)
	}

	// Extreme pagination
	matches, total, err := store.ListHistoricalMatches(ctx, MatchFilter{Limit: -5, Offset: -10})
	if err != nil {
		t.Fatalf("expected no error on negative pagination, got %v", err)
	}
	if total != 0 || len(matches) != 0 {
		t.Errorf("expected empty on empty store, got total %d len %d", total, len(matches))
	}
}
