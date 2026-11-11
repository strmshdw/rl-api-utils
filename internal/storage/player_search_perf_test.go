package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/storage"
)

func seedPlayerSearchDataset(t *testing.T, stores []storage.StateStore, matchCount int) {
	t.Helper()
	ctx := context.Background()
	baseTime := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	localID := "Steam|76561198000000000|0"

	for _, st := range stores {
		if err := st.SaveAuthState(ctx, "steam", "tok", "76561198000000000", "LocalHero"); err != nil {
			t.Fatalf("SaveAuthState failed: %v", err)
		}
	}

	for m := 0; m < matchCount; m++ {
		guid := fmt.Sprintf("perf-match-%04d", m)
		playlist := 11
		if m%3 == 0 {
			playlist = 13
		}
		matchTS := baseTime.Add(time.Duration(m) * 10 * time.Minute)
		localWon := m%2 == 0
		blueScore, orangeScore := 3, 1
		winner := 0
		if !localWon {
			blueScore, orangeScore = 1, 4
			winner = 1
		}

		// 3 unique/semi-recurring non-local players per match
		tmID := fmt.Sprintf("Steam|765611981%08d|0", (m%120)+1)
		tmName := fmt.Sprintf("Mate_%03d", (m%120)+1)
		opp1ID := fmt.Sprintf("Epic|opp_alpha_%04d|0", m*2+1)
		opp1Name := fmt.Sprintf("OppAlpha_%04d", m*2+1)
		opp2ID := fmt.Sprintf("PS4|opp_beta_%04d|0", (m%90)+1)
		opp2Name := fmt.Sprintf("OppBeta_%03d", (m%90)+1)

		replayStats := storage.ReplayMatchStats{
			MatchGUID:   guid,
			Playlist:    playlist,
			Duration:    300,
			Date:        matchTS.Format(time.RFC3339),
			BlueScore:   blueScore,
			OrangeScore: orangeScore,
			Players: []storage.ReplayPlayerStats{
				{Name: "LocalHero", Platform: "Steam", OnlineID: "76561198000000000", Team: 0, Score: 520, Goals: 2, Assists: 1, Saves: 2, Shots: 5, Demos: 1, Boost: &ballchasing.BoostStats{BPM: 410, AvgAmount: 48}},
				{Name: tmName, Platform: "Steam", OnlineID: fmt.Sprintf("765611981%08d", (m%120)+1), Team: 0, Score: 410 + (m % 90), Goals: 1, Assists: 2, Saves: 1, Shots: 4, Demos: 2, Boost: &ballchasing.BoostStats{BPM: 385, AvgAmount: 44}},
				{Name: opp1Name, Platform: "Epic", OnlineID: fmt.Sprintf("opp_alpha_%04d", m*2+1), Team: 1, Score: 330 + (m % 70), Goals: 1, Assists: 0, Saves: 3, Shots: 3, Demos: 1, Boost: &ballchasing.BoostStats{BPM: 360, AvgAmount: 41}},
				{Name: opp2Name, Platform: "PS4", OnlineID: fmt.Sprintf("opp_beta_%04d", (m%90)+1), Team: 1, Score: 290 + (m % 50), Goals: 0, Assists: 1, Saves: 2, Shots: 2, Demos: 3, Boost: &ballchasing.BoostStats{BPM: 395, AvgAmount: 46}},
			},
		}
		rawReplayJSON, _ := json.Marshal(replayStats)

		// Half the matches store per-player stats_json directly; the other half exercise fallback m.replay_stats_json extraction!
		tmStatsJSON := ""
		opp1StatsJSON := ""
		opp2StatsJSON := ""
		if m%2 == 0 {
			b1, _ := json.Marshal(replayStats.Players[1])
			b2, _ := json.Marshal(replayStats.Players[2])
			b3, _ := json.Marshal(replayStats.Players[3])
			tmStatsJSON = string(b1)
			opp1StatsJSON = string(b2)
			opp2StatsJSON = string(b3)
		}

		notLocalWon := !localWon
		mpRows := []*storage.MatchPlayerRecord{
			{MatchGUID: guid, PlayerID: localID, TeamNum: 0, IsLocal: true, Won: &localWon, Score: 520, Goals: 2, Assists: 1, Saves: 2, Shots: 5, Demos: 1},
			{MatchGUID: guid, PlayerID: tmID, TeamNum: 0, IsLocal: false, Won: &localWon, Score: 410 + (m % 90), Goals: 1, Assists: 2, Saves: 1, Shots: 4, Demos: 2, StatsJSON: tmStatsJSON},
			{MatchGUID: guid, PlayerID: opp1ID, TeamNum: 1, IsLocal: false, Won: &notLocalWon, Score: 330 + (m % 70), Goals: 1, Assists: 0, Saves: 3, Shots: 3, Demos: 1, StatsJSON: opp1StatsJSON},
			{MatchGUID: guid, PlayerID: opp2ID, TeamNum: 1, IsLocal: false, Won: &notLocalWon, Score: 290 + (m % 50), Goals: 0, Assists: 1, Saves: 2, Shots: 2, Demos: 3, StatsJSON: opp2StatsJSON},
		}

		for _, st := range stores {
			if err := st.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{
				{
					MatchGUID:            guid,
					RecordStartTimestamp: matchTS.Unix(),
					Playlist:             playlist,
					BlueScore:            blueScore,
					OrangeScore:          orangeScore,
					WinnerTeam:           &winner,
					ReplayStatsJSON:      string(rawReplayJSON),
				},
			}); err != nil {
				t.Fatalf("UpsertDiscoveredMatches failed: %v", err)
			}
			// Ensure replay_stats_json + scores are persisted on MatchRecord via SyncReplayStats as well
			if err := st.SyncReplayStats(ctx, &replayStats); err != nil {
				t.Fatalf("SyncReplayStats failed: %v", err)
			}
			for _, p := range []struct {
				id, plat, name string
				mmr            float64
			}{
				{tmID, "Steam", tmName, 1450 + float64(m%50)},
				{opp1ID, "Epic", opp1Name, 1410 + float64(m%60)},
				{opp2ID, "PS4", opp2Name, 1480 + float64(m%40)},
			} {
				if err := st.UpsertPlayer(ctx, &storage.PlayerRecord{
					PlayerID:    p.id,
					Platform:    p.plat,
					PlayerName:  p.name,
					RanksJSON:   fmt.Sprintf(`{"11":{"playlist_id":11,"tier":18,"division":2,"mmr":%.1f}}`, p.mmr),
					FirstSeenAt: baseTime,
					LastSeenAt:  matchTS,
				}); err != nil {
					t.Fatalf("UpsertPlayer failed: %v", err)
				}
			}
			if err := st.RecordMatchResults(ctx, guid, playlist, []storage.PlayerOutcome{
				{PlayerID: tmID, Platform: "Steam", PlayerName: tmName, IsTeammate: true, Won: localWon},
				{PlayerID: opp1ID, Platform: "Epic", PlayerName: opp1Name, IsTeammate: false, Won: localWon},
				{PlayerID: opp2ID, Platform: "PS4", PlayerName: opp2Name, IsTeammate: false, Won: localWon},
			}, matchTS); err != nil {
				t.Fatalf("RecordMatchResults failed: %v", err)
			}
			if err := st.SaveMatchPlayers(ctx, guid, mpRows); err != nil {
				t.Fatalf("SaveMatchPlayers failed: %v", err)
			}
		}
	}
}

// TestSQLiteStore_FilterPlayerSummaries_HonorsMidQueryContextCancellation verifies that if a client
// aborts an in-flight /api/players request mid-evaluation, FilterPlayerSummaries returns context.Canceled
// instead of silently swallowing query errors inside ComputePlayerSplitStats and returning partial data.
func TestSQLiteStore_FilterPlayerSummaries_HonorsMidQueryContextCancellation(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cancel_perf.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer store.Close()

	seedPlayerSearchDataset(t, []storage.StateStore{store}, 120)

	// Immediately-canceled and tight-deadline contexts must return context error, never partial results
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, _, err := store.FilterPlayerSummaries(ctx, storage.PlayerDirectoryFilter{Limit: 24})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got res=%d err=%v", len(res), err)
	}

	// Concurrent cancel race during active FilterPlayerSummaries calls
	for i := 0; i < 10; i++ {
		rCtx, rCancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			out, total, fErr := store.FilterPlayerSummaries(rCtx, storage.PlayerDirectoryFilter{
				Sort:  "vs_pts",
				Order: "desc",
				Limit: 24,
			})
			if fErr != nil {
				if !errors.Is(fErr, context.Canceled) {
					t.Errorf("unexpected non-cancel error: %v", fErr)
				}
				return
			}
			// If it succeeded before cancel took effect, results must be complete (never zeroed/corrupt)
			if total == 0 || len(out) == 0 {
				t.Errorf("expected complete results when err==nil, got len=%d total=%d", len(out), total)
			}
		}()
		time.Sleep(time.Duration(i%3) * time.Millisecond)
		rCancel()
		<-done
	}
}

// TestSQLiteStore_FilterPlayerSummaries_BulkSplitStatsParityAndLatency verifies 100% parity
// with JSONStore across filters and sort modes (including opponent_wr and vs_pts sent by the UI)
// and ensures 500+ players complete rapidly without N+1 table-scan stalls.
func TestSQLiteStore_FilterPlayerSummaries_BulkSplitStatsParityAndLatency(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "parity_perf.db")
	sqlStore, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer sqlStore.Close()

	jsonPath := filepath.Join(t.TempDir(), "parity_perf.json")
	jsonStore, err := storage.NewJSONStore(jsonPath)
	if err != nil {
		t.Fatalf("NewJSONStore failed: %v", err)
	}
	defer jsonStore.Close()

	// 200 matches -> ~410 distinct players
	seedPlayerSearchDataset(t, []storage.StateStore{sqlStore, jsonStore}, 200)

	ctx := context.Background()
	filters := []storage.PlayerDirectoryFilter{
		{Role: "all", Sort: "last_seen", Order: "desc", Limit: 24, Offset: 0},
		{Role: "teammate", Sort: "with_pts", Order: "desc", Limit: 24, Offset: 0},
		{Role: "opponent", Sort: "vs_pts", Order: "desc", Limit: 24, Offset: 0},
		{Role: "opponent", Sort: "opponent_wr", Order: "asc", Limit: 24, Offset: 0},
		{Query: "OppBeta", Platform: "playstation", Sort: "match_count", Order: "desc", Limit: 20, Offset: 0},
		{Playlist: "2v2", FromUnix: time.Date(2026, 10, 1, 18, 0, 0, 0, time.UTC).Unix(), Sort: "mmr", Order: "desc", Limit: 25, Offset: 0},
	}

	for idx, f := range filters {
		start := time.Now()
		sqlRes, sqlTotal, err := sqlStore.FilterPlayerSummaries(ctx, f)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("filter[%d] sqlite failed: %v", idx, err)
		}
		if elapsed > 300*time.Millisecond {
			t.Fatalf("filter[%d] (%+v) took %v (> 300ms N+1 regression budget)", idx, f, elapsed)
		}

		jsonRes, jsonTotal, err := jsonStore.FilterPlayerSummaries(ctx, f)
		if err != nil {
			t.Fatalf("filter[%d] json failed: %v", idx, err)
		}

		if sqlTotal != jsonTotal {
			t.Fatalf("filter[%d] total mismatch: sqlite=%d json=%d", idx, sqlTotal, jsonTotal)
		}
		if len(sqlRes) != len(jsonRes) {
			t.Fatalf("filter[%d] len mismatch: sqlite=%d json=%d", idx, len(sqlRes), len(jsonRes))
		}
		for i := range sqlRes {
			gs, gj := sqlRes[i], jsonRes[i]
			if gs.PlayerID != gj.PlayerID ||
				gs.TotalMatches != gj.TotalMatches ||
				gs.TotalWinsAsTeammate != gj.TotalWinsAsTeammate ||
				gs.TotalLossesAsTeammate != gj.TotalLossesAsTeammate ||
				gs.TotalWinsAsOpponent != gj.TotalWinsAsOpponent ||
				gs.TotalLossesAsOpponent != gj.TotalLossesAsOpponent {
				t.Fatalf("filter[%d] row[%d] mismatch:\nsqlite=%+v\njson  =%+v", idx, i, gs, gj)
			}
			if (gs.WithYouStats != nil) != (gj.WithYouStats != nil) ||
				(gs.WithYouStats != nil && (*gs.WithYouStats != *gj.WithYouStats)) {
				t.Fatalf("filter[%d] row[%d] (%s) WithYouStats mismatch:\nsqlite=%+v\njson  =%+v", idx, i, gs.PlayerID, gs.WithYouStats, gj.WithYouStats)
			}
			if (gs.AgainstYouStats != nil) != (gj.AgainstYouStats != nil) ||
				(gs.AgainstYouStats != nil && (*gs.AgainstYouStats != *gj.AgainstYouStats)) {
				t.Fatalf("filter[%d] row[%d] (%s) AgainstYouStats mismatch:\nsqlite=%+v\njson  =%+v", idx, i, gs.PlayerID, gs.AgainstYouStats, gj.AgainstYouStats)
			}
		}
	}

	// Verify vs_pts and opponent_wr actually sort monotonically across the returned slice!
	vsPtsRes, _, err := sqlStore.FilterPlayerSummaries(ctx, storage.PlayerDirectoryFilter{
		Role:  "opponent",
		Sort:  "vs_pts",
		Order: "desc",
		Limit: 50,
	})
	if err != nil || len(vsPtsRes) < 2 {
		t.Fatalf("vs_pts query failed: len=%d err=%v", len(vsPtsRes), err)
	}
	for i := 1; i < len(vsPtsRes); i++ {
		prev := vsPtsRes[i-1].AgainstYouStats.AvgScore
		curr := vsPtsRes[i].AgainstYouStats.AvgScore
		if prev < curr {
			t.Fatalf("vs_pts desc sort order violated at %d: prev=%.1f < curr=%.1f", i, prev, curr)
		}
	}

	oppWRRes, _, err := sqlStore.FilterPlayerSummaries(ctx, storage.PlayerDirectoryFilter{
		Role:  "opponent",
		Sort:  "opponent_wr",
		Order: "desc",
		Limit: 50,
	})
	if err != nil || len(oppWRRes) < 2 {
		t.Fatalf("opponent_wr query failed: len=%d err=%v", len(oppWRRes), err)
	}
	rateOf := func(p *storage.PlayerSummary) float64 {
		g := p.TotalWinsAsOpponent + p.TotalLossesAsOpponent
		if g == 0 {
			return 0
		}
		return float64(p.TotalWinsAsOpponent) / float64(g)
	}
	for i := 1; i < len(oppWRRes); i++ {
		if rateOf(oppWRRes[i-1]) < rateOf(oppWRRes[i]) {
			t.Fatalf("opponent_wr desc sort order violated at %d: prev=%.3f < curr=%.3f", i, rateOf(oppWRRes[i-1]), rateOf(oppWRRes[i]))
		}
	}
}
