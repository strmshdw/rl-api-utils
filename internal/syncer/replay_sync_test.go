package syncer

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dank/rl-api-utils/internal/storage"
)

func TestSyncer_SyncDownloadedReplayStats_RealReplay(t *testing.T) {
	files, err := filepath.Glob("../../replays/*.replay")
	if err != nil || len(files) == 0 {
		t.Skip("no replay files found")
	}

	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	defer store.Close()

	provider := newMockHistoryProvider()
	downloader := newMockDownloader()
	uploader := newMockUploader()

	s, err := New(store, provider, downloader, uploader)
	if err != nil {
		t.Fatalf("New syncer: %v", err)
	}

	ctx := context.Background()
	matchGUID := "009D5B9611F1C224FB9A34A94F41F148"

	// Pre-create match record (e.g. from PsyNet discovery)
	err = store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{
		{
			MatchGUID: matchGUID,
			Playlist:  11,
		},
	})
	if err != nil {
		t.Fatalf("UpsertDiscoveredMatches: %v", err)
	}

	// Invoke live replay download hook
	err = s.SyncDownloadedReplayStats(ctx, matchGUID, files[0])
	if err != nil {
		t.Fatalf("SyncDownloadedReplayStats failed: %v", err)
	}

	// Verify enriched match record in storage
	rec, err := store.GetMatch(ctx, matchGUID)
	if err != nil {
		t.Fatalf("GetMatch failed: %v", err)
	}

	if rec.ReplayStatsJSON == "" {
		t.Errorf("expected non-empty ReplayStatsJSON")
	}
	if rec.BlueScore == 0 && rec.OrangeScore == 0 {
		t.Errorf("expected scores to be populated")
	}

	// Verify historical matches listing
	hist, total, err := store.ListHistoricalMatches(ctx, storage.MatchFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListHistoricalMatches: %v", err)
	}
	if total != 1 || len(hist) != 1 {
		t.Fatalf("expected 1 historical match, got total %d len %d", total, len(hist))
	}
	if hist[0].Stats == nil || len(hist[0].Stats.Players) == 0 {
		t.Errorf("expected players in historical match stats")
	}
}
