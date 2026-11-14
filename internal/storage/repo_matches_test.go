package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/domain"
)

func TestRepoMatches_LifecycleRetryAndPrune(t *testing.T) {
	ctx := context.Background()
	store := openTestSQLite(t)
	store.SetRetryPolicy(3, 1*time.Millisecond, 10*time.Millisecond)

	if _, err := store.GetMatch(ctx, ""); !errors.Is(err, ErrInvalidGUID) {
		t.Fatalf("empty GUID error = %v, want ErrInvalidGUID", err)
	}
	if _, err := store.GetMatch(ctx, "missing"); !errors.Is(err, ErrMatchNotFound) {
		t.Fatalf("missing GUID error = %v, want ErrMatchNotFound", err)
	}

	base := time.Now().UTC().Add(-2 * time.Hour)
	seed := []*MatchRecord{
		{MatchGUID: "m-1", RecordStartTimestamp: base.Unix(), MapName: "DFH Stadium", Playlist: 11, ReplayURL: "https://r/1"},
		{MatchGUID: "m-2", RecordStartTimestamp: base.Add(10 * time.Minute).Unix(), MapName: "Mannfield", Playlist: 13, ReplayURL: "https://r/2"},
		{MatchGUID: "m-3", RecordStartTimestamp: base.Add(20 * time.Minute).Unix(), MapName: "Utopia", Playlist: 10, ReplayURL: "https://r/3"},
	}
	if err := store.UpsertDiscoveredMatches(ctx, seed); err != nil {
		t.Fatal(err)
	}

	// Transition m-1: downloading -> downloaded -> uploading -> uploaded -> stats syncing -> synced.
	if err := store.MarkDownloading(ctx, "m-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkDownloaded(ctx, "m-1", "/replays/m-1.replay"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkUploading(ctx, "m-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkUploaded(ctx, "m-1", "bc-id-1", "https://ballchasing.com/replay/bc-id-1"); err != nil {
		t.Fatal(err)
	}
	if got, err := store.GetMatchByBallchasingID(ctx, "bc-id-1"); err != nil || got.MatchGUID != "m-1" {
		t.Fatalf("GetMatchByBallchasingID = (%v, %v)", got, err)
	}

	// Re-upserting m-1 must preserve existing download/upload progress.
	if err := store.UpsertDiscoveredMatches(ctx, []*MatchRecord{{MatchGUID: "m-1", RecordStartTimestamp: base.Unix(), MapName: "NewName", Playlist: 11}}); err != nil {
		t.Fatal(err)
	}
	gotM1, _ := store.GetMatch(ctx, "m-1")
	if gotM1.UploadStatus != UploadUploaded || gotM1.BallchasingID != "bc-id-1" {
		t.Fatalf("upsert overwrote upload progress: %+v", gotM1)
	}

	// Stats sync pending + retry >= 64 SQLite bitshift safety check.
	if _, err := store.db.ExecContext(ctx, "UPDATE matches SET stats_retry_count = 70, updated_at = ? WHERE match_guid = 'm-1';", time.Now().UTC().Add(-time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	pending, err := store.ListPendingStatsSync(ctx, 100)
	if err != nil {
		t.Fatalf("ListPendingStatsSync failed with high retry_count: %v", err)
	}
	if len(pending) != 1 || pending[0].MatchGUID != "m-1" {
		t.Fatalf("pending stats sync = %v, want [m-1]", pending)
	}

	if err := store.MarkStatsSyncFailed(ctx, "m-1", "timeout"); err != nil {
		t.Fatal(err)
	}
	if err := store.ResetMatchRetry(ctx, "m-1"); err != nil {
		t.Fatal(err)
	}
	gotM1, _ = store.GetMatch(ctx, "m-1")
	if gotM1.RetryCount != 0 || gotM1.StatsRetryCount != 0 || gotM1.StatsSyncStatus != StatsSyncPending || gotM1.UploadStatus != UploadUploaded {
		t.Fatalf("ResetMatchRetry did not reset counters/stats sync status cleanly: %+v", gotM1)
	}

	if err := store.MarkUploadFailed(ctx, "m-3", "network reset"); err != nil {
		t.Fatal(err)
	}
	if err := store.ResetMatchRetry(ctx, "m-3"); err != nil {
		t.Fatal(err)
	}
	if gotM3, _ := store.GetMatch(ctx, "m-3"); gotM3.UploadStatus != UploadPending || gotM3.RetryCount != 0 {
		t.Fatalf("ResetMatchRetry failed to re-queue failed upload m-3: %+v", gotM3)
	}

	// MarkDuplicate path.
	if err := store.MarkDuplicate(ctx, "m-2", "bc-dup", "https://ballchasing.com/replay/bc-dup"); err != nil {
		t.Fatal(err)
	}
	if gotM2, _ := store.GetMatch(ctx, "m-2"); gotM2.UploadStatus != UploadDuplicate {
		t.Fatalf("m-2 upload status = %s, want DUPLICATE", gotM2.UploadStatus)
	}

	// PruneOldestMatches deletes oldest match and cascades cleanly.
	if pruned, err := store.PruneOldestMatches(ctx, 0); err != nil || pruned != 0 {
		t.Fatalf("PruneOldestMatches(0) = (%d, %v)", pruned, err)
	}
	pruned, err := store.PruneOldestMatches(ctx, 1)
	if err != nil || pruned != 1 {
		t.Fatalf("PruneOldestMatches(1) = (%d, %v)", pruned, err)
	}
	if _, err := store.GetMatch(ctx, "m-1"); !errors.Is(err, ErrMatchNotFound) {
		t.Fatalf("m-1 should have been pruned, got %v", err)
	}
	if d := CalculateBackoff(10, time.Second, 5*time.Second); d != 5*time.Second {
		t.Fatalf("CalculateBackoff cap failed: got %v", d)
	}
}

func TestRepoMatches_SyncReplayStats_017db99AndRankHydration(t *testing.T) {
	ctx := context.Background()
	store := openTestSQLite(t)

	if err := store.UpsertPlayer(ctx, &PlayerRecord{PlayerID: "Steam|7656|0", Platform: "Steam", PlayerName: "Ace"}); err != nil {
		t.Fatal(err)
	}

	// First sync populates match + players + rank hydration.
	wt := 0
	stats1 := &ReplayMatchStats{
		MatchGUID:   "sync-1",
		MapName:     "Neo Tokyo",
		Playlist:    11,
		BlueScore:   4,
		OrangeScore: 2,
		WinnerTeam:  &wt,
		Date:        "2026-01-15T12:00:00Z",
		Players: []ReplayPlayerStats{
			{
				Name: "Ace", Platform: "Steam", OnlineID: "7656", Team: 0, IsLocal: true,
				Score: 700, Goals: 3, Assists: 1, Saves: 2, Shots: 5, Demos: 1, CarName: "Octane",
				Rank:   &domain.PlayerRank{Tier: 19, Division: 2},
				Boost:  &domain.BoostStats{BPM: 410, BCPM: 430},
				Core:   &domain.CoreStats{Score: 700, Goals: 3},
				Camera: &domain.CameraSettings{FOV: 110},
			},
			{
				Name: "Rival", Platform: "Epic", OnlineID: "epic_rival", Team: 1,
				Score: 450, Goals: 2, Assists: 0, Saves: 3, Shots: 4, Demos: 0, CarName: "Fennec",
				Rank: &domain.PlayerRank{Tier: 18, Division: 4},
			},
		},
	}
	if err := store.SyncReplayStats(ctx, stats1); err != nil {
		t.Fatalf("SyncReplayStats failed: %v", err)
	}

	// 017db99: subsequent SyncReplayStats with empty Players must NOT wipe existing players!
	emptyStats := &ReplayMatchStats{
		MatchGUID:     "sync-1",
		MapName:       "Neo Tokyo",
		Playlist:      11,
		BlueScore:     4,
		OrangeScore:   2,
		WinnerTeam:    &wt,
		BallchasingID: "bc-sync-1",
		Players:       nil,
	}
	if err := store.SyncReplayStats(ctx, emptyStats); err != nil {
		t.Fatalf("empty-players SyncReplayStats failed: %v", err)
	}

	gotMatch, err := store.GetMatch(ctx, "sync-1")
	if err != nil {
		t.Fatal(err)
	}
	var parsed ReplayMatchStats
	if err := json.Unmarshal([]byte(gotMatch.ReplayStatsJSON), &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Players) != 2 {
		t.Fatalf("017db99 violation: expected 2 preserved players, got %d", len(parsed.Players))
	}
	if parsed.BallchasingID != "bc-sync-1" {
		t.Fatalf("BallchasingID = %q, want bc-sync-1", parsed.BallchasingID)
	}

	// Verify ranks_json was hydrated on non-local opponent Epic|epic_rival|0 (local accounts stay out of players table).
	pl, err := store.GetPlayer(ctx, "Epic|epic_rival|0")
	if err != nil {
		t.Fatal(err)
	}
	tier, div, _ := ExtractPrimaryRankFromJSON(pl.RanksJSON, []int{11})
	if tier != 18 || div != 4 {
		t.Fatalf("hydrated rank = (%d, %d), want (18, 4)", tier, div)
	}
}

func TestRepoMatches_ListHistoricalMatchesBatchedFilterAndScale(t *testing.T) {
	ctx := context.Background()
	store := openTestSQLite(t)

	// Seed 60 matches (> SQLite 999 host-param limit if chunked at 500 across bigger scale too,
	// let's seed 550 matches for chunk boundary testing of 500-GUID IN clauses!).
	base := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	batch := make([]*MatchRecord, 0, 550)
	for i := 0; i < 550; i++ {
		pl := 11
		blue, orange := 3, 1
		wt := 0
		if i%3 == 0 {
			pl = 13
			blue, orange = 1, 4
			wt = 1
		}
		batch = append(batch, &MatchRecord{
			MatchGUID:            fmt.Sprintf("hist-%04d", i),
			RecordStartTimestamp: base.Add(time.Duration(i) * time.Minute).Unix(),
			MapName:              "Beckwith Park",
			Playlist:             pl,
			BlueScore:            blue,
			OrangeScore:          orange,
			WinnerTeam:           &wt,
			DownloadStatus:       DownloadDownloaded,
			UploadStatus:         UploadUploaded,
			StatsSyncStatus:      StatsSyncSynced,
		})
	}
	if err := store.UpsertDiscoveredMatches(ctx, batch); err != nil {
		t.Fatal(err)
	}
	wtOrange := 1
	if err := store.SyncReplayStats(ctx, &ReplayMatchStats{
		MatchGUID: "hist-0549", MapName: "Beckwith Park", Playlist: 13, BlueScore: 1, OrangeScore: 4, WinnerTeam: &wtOrange,
		Date: base.Add(549 * time.Minute).Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	// Tag local player on hist-0549 (orange team winning!) + search target car/name.
	if err := store.SaveMatchPlayers(ctx, "hist-0549", []*MatchPlayerRecord{
		{MatchGUID: "hist-0549", PlayerID: "Steam|me|0", TeamNum: 1, IsLocal: true, Score: 999, Goals: 5, CarName: "Dominus GT"},
	}); err != nil {
		t.Fatal(err)
	}

	// Fast SQL pagination path.
	page, total, err := store.ListHistoricalMatches(ctx, MatchFilter{PlaylistFilter: "2v2", Limit: 10, Offset: 0})
	if err != nil {
		t.Fatalf("ListHistoricalMatches fast path failed: %v", err)
	}
	if len(page) != 10 || total == 0 {
		t.Fatalf("got page=%d total=%d", len(page), total)
	}

	// In-memory search path across all 550 (> 500 chunk boundary) matching CarName "Dominus GT".
	found, foundTotal, err := store.ListHistoricalMatches(ctx, MatchFilter{Search: "dominus gt", Limit: 20})
	if err != nil {
		t.Fatalf("ListHistoricalMatches search failed: %v", err)
	}
	if foundTotal != 1 || len(found) != 1 || found[0].MatchGUID != "hist-0549" {
		t.Fatalf("search got (%d, %v), want [hist-0549]", foundTotal, found)
	}
	if found[0].Result != "victory" {
		t.Fatalf("hist-0549 local team 1 win gave result %q, want victory", found[0].Result)
	}

	// EnrichMatchStatsWithPlayers attaches player row when ReplayStatsJSON was empty.
	if found[0].Stats == nil || len(found[0].Stats.Players) != 1 || found[0].Stats.Players[0].Score != 999 {
		t.Fatalf("enriched stats missing match_players fallback: %+v", found[0].Stats)
	}

	// Sort by score descending across all matches.
	sorted, _, err := store.ListHistoricalMatches(ctx, MatchFilter{SortBy: "score", SortDir: "desc", Limit: 5})
	if err != nil || len(sorted) != 5 || sorted[0].MatchGUID != "hist-0549" {
		t.Fatalf("sort by score desc top match = %+v, err=%v", sorted, err)
	}
}
