package syncer

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/dank/rl-api-utils/internal/storage"
)

func TestAdversarial_InFlightRecovery_SQLiteBackend(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_recovery.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	records := []*storage.MatchRecord{
		{
			MatchGUID:            "orphan-dl-1",
			RecordStartTimestamp: 1000,
			ReplayURL:            "https://cdn.example.com/dl1.replay",
			DownloadStatus:       storage.DownloadDownloading,
			UploadStatus:         storage.UploadPending,
		},
		{
			MatchGUID:            "orphan-up-1",
			RecordStartTimestamp: 2000,
			ReplayURL:            "https://cdn.example.com/up1.replay",
			DownloadStatus:       storage.DownloadDownloaded,
			LocalFilePath:        filepath.Join(t.TempDir(), "up1.replay"),
			UploadStatus:         storage.UploadUploading,
		},
		{
			MatchGUID:            "stable-completed",
			RecordStartTimestamp: 3000,
			ReplayURL:            "https://cdn.example.com/done.replay",
			DownloadStatus:       storage.DownloadDownloaded,
			LocalFilePath:        "/tmp/done.replay",
			UploadStatus:         storage.UploadUploaded,
			BallchasingID:        "bc-done",
		},
		{
			MatchGUID:            "stable-skipped",
			RecordStartTimestamp: 4000,
			ReplayURL:            "",
			DownloadStatus:       storage.DownloadSkipped,
			UploadStatus:         storage.UploadPending,
		},
	}

	if err := store.UpsertDiscoveredMatches(ctx, records); err != nil {
		t.Fatalf("failed to seed records: %v", err)
	}
	if err := store.MarkDownloading(ctx, "orphan-dl-1"); err != nil {
		t.Fatalf("failed to mark downloading: %v", err)
	}
	if err := store.MarkUploading(ctx, "orphan-up-1"); err != nil {
		t.Fatalf("failed to mark uploading: %v", err)
	}

	syncerEngine := NewSyncerEngine(store, newMockHistoryProvider(), newMockDownloader(), newMockUploader(), t.TempDir(), false)
	stats, err := syncerEngine.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}
	if stats.DownloadedCount != 1 || stats.UploadedCount != 2 {
		t.Fatalf("unexpected recovery stats: dl=%d up=%d", stats.DownloadedCount, stats.UploadedCount)
	}

	postDl, _ := store.GetMatch(ctx, "orphan-dl-1")
	if postDl.DownloadStatus != storage.DownloadDownloaded || postDl.UploadStatus != storage.UploadUploaded {
		t.Fatalf("post-condition failed for orphan-dl-1: dl=%s, up=%s", postDl.DownloadStatus, postDl.UploadStatus)
	}
	postUp, _ := store.GetMatch(ctx, "orphan-up-1")
	if postUp.UploadStatus != storage.UploadUploaded {
		t.Fatalf("post-condition failed for orphan-up-1: up=%s", postUp.UploadStatus)
	}
	postSkipped, _ := store.GetMatch(ctx, "stable-skipped")
	if postSkipped.DownloadStatus != storage.DownloadSkipped {
		t.Fatalf("stable-skipped status clobbered: %s", postSkipped.DownloadStatus)
	}
	postDone, _ := store.GetMatch(ctx, "stable-completed")
	if postDone.UploadStatus != storage.UploadUploaded {
		t.Fatalf("stable-completed status clobbered: %s", postDone.UploadStatus)
	}
}

func TestAdversarial_InFlightRecovery_JSONBackend(t *testing.T) {
	jsonPath := filepath.Join(t.TempDir(), "test_recovery.json")
	store, err := storage.NewJSONStore(jsonPath)
	if err != nil {
		t.Fatalf("failed to create json store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	records := []*storage.MatchRecord{
		{
			MatchGUID:            "json-orphan-dl",
			RecordStartTimestamp: 100,
			ReplayURL:            "https://cdn.example.com/jdl.replay",
			DownloadStatus:       storage.DownloadPending,
			UploadStatus:         storage.UploadPending,
		},
		{
			MatchGUID:            "json-orphan-up",
			RecordStartTimestamp: 200,
			ReplayURL:            "https://cdn.example.com/jup.replay",
			DownloadStatus:       storage.DownloadDownloaded,
			LocalFilePath:        filepath.Join(t.TempDir(), "jup.replay"),
			UploadStatus:         storage.UploadPending,
		},
	}
	if err := store.UpsertDiscoveredMatches(ctx, records); err != nil {
		t.Fatalf("failed to seed: %v", err)
	}
	_ = store.MarkDownloading(ctx, "json-orphan-dl")
	_ = store.MarkUploading(ctx, "json-orphan-up")

	syncerEngine := NewSyncerEngine(store, newMockHistoryProvider(), newMockDownloader(), newMockUploader(), t.TempDir(), false)
	stats, err := syncerEngine.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}
	if stats.DownloadedCount != 1 || stats.UploadedCount != 2 {
		t.Fatalf("unexpected recovery stats: dl=%d, up=%d", stats.DownloadedCount, stats.UploadedCount)
	}

	rec1, _ := store.GetMatch(ctx, "json-orphan-dl")
	if rec1.DownloadStatus != storage.DownloadDownloaded || rec1.UploadStatus != storage.UploadUploaded {
		t.Fatalf("recovery failed for json-orphan-dl: dl=%s, up=%s", rec1.DownloadStatus, rec1.UploadStatus)
	}
}

func TestAdversarial_DelayedReplayURLs_MultiCyclePromotion_SQLite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_delayed.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m-ready", ReplayURL: "https://cdn.example.com/ready.replay"},
		DiscoveredMatch{MatchGUID: "m-delay-1", ReplayURL: ""},
		DiscoveredMatch{MatchGUID: "m-delay-2", ReplayURL: ""},
	)
	syncerEngine := NewSyncerEngine(store, provider, newMockDownloader(), newMockUploader(), t.TempDir(), false)

	stats1, err := syncerEngine.RunCycle(ctx)
	if err != nil {
		t.Fatalf("cycle 1 failed: %v", err)
	}
	if stats1.DiscoveredCount != 3 || stats1.SkippedCount != 2 || stats1.DownloadedCount != 1 || stats1.UploadedCount != 1 {
		t.Fatalf("cycle 1 unexpected stats: %+v", stats1)
	}
	if recDelay1, _ := store.GetMatch(ctx, "m-delay-1"); recDelay1.DownloadStatus != storage.DownloadSkipped {
		t.Fatalf("cycle 1: expected m-delay-1 SKIPPED, got %s", recDelay1.DownloadStatus)
	}

	provider.SetMatches([]DiscoveredMatch{
		{MatchGUID: "m-ready", ReplayURL: "https://cdn.example.com/ready.replay"},
		{MatchGUID: "m-delay-1", ReplayURL: "https://cdn.example.com/delay1.replay"},
		{MatchGUID: "m-delay-2", ReplayURL: ""},
		{MatchGUID: "m-new", ReplayURL: "https://cdn.example.com/new.replay"},
	})

	stats2, err := syncerEngine.RunCycle(ctx)
	if err != nil {
		t.Fatalf("cycle 2 failed: %v", err)
	}
	if stats2.DiscoveredCount != 4 || stats2.SkippedCount != 1 || stats2.DownloadedCount != 2 || stats2.UploadedCount != 2 {
		t.Fatalf("cycle 2 unexpected stats: %+v", stats2)
	}
	recDelay1After, _ := store.GetMatch(ctx, "m-delay-1")
	if recDelay1After.DownloadStatus != storage.DownloadDownloaded || recDelay1After.UploadStatus != storage.UploadUploaded {
		t.Fatalf("cycle 2: expected m-delay-1 DOWNLOADED/UPLOADED, got dl=%s, up=%s", recDelay1After.DownloadStatus, recDelay1After.UploadStatus)
	}

	provider.SetMatches([]DiscoveredMatch{
		{MatchGUID: "m-ready", ReplayURL: "https://cdn.example.com/ready.replay"},
		{MatchGUID: "m-delay-1", ReplayURL: "https://cdn.example.com/delay1.replay"},
		{MatchGUID: "m-delay-2", ReplayURL: "https://cdn.example.com/delay2.replay"},
		{MatchGUID: "m-new", ReplayURL: "https://cdn.example.com/new.replay"},
	})

	stats3, err := syncerEngine.RunCycle(ctx)
	if err != nil {
		t.Fatalf("cycle 3 failed: %v", err)
	}
	if stats3.SkippedCount != 0 || stats3.DownloadedCount != 1 || stats3.UploadedCount != 1 {
		t.Fatalf("cycle 3 unexpected stats: %+v", stats3)
	}
}

func TestAdversarial_DelayedReplayURLs_WhitespaceTrimmedAndPromoted(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_ws_fix.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m-ws-delayed", ReplayURL: "   "},
	)
	s := NewSyncerEngine(store, provider, newMockDownloader(), newMockUploader(), t.TempDir(), false)

	stats1, err := s.RunCycle(ctx)
	if err != nil {
		t.Fatalf("cycle 1 failed: %v", err)
	}
	if stats1.SkippedCount != 1 {
		t.Fatalf("expected 1 skipped, got %d", stats1.SkippedCount)
	}
	recBefore, _ := store.GetMatch(ctx, "m-ws-delayed")
	if recBefore.ReplayURL != "" {
		t.Fatalf("expected whitespace ReplayURL trimmed to '', got %q", recBefore.ReplayURL)
	}

	provider.SetMatches([]DiscoveredMatch{
		{MatchGUID: "m-ws-delayed", ReplayURL: "  https://cdn.example.com/valid.replay  "},
	})

	stats2, err := s.RunCycle(ctx)
	if err != nil {
		t.Fatalf("cycle 2 failed: %v", err)
	}
	if stats2.DownloadedCount != 1 || stats2.UploadedCount != 1 {
		t.Fatalf("expected trimmed delayed URL promoted & completed, got %+v", stats2)
	}
	recAfter, _ := store.GetMatch(ctx, "m-ws-delayed")
	if recAfter.DownloadStatus != storage.DownloadDownloaded || recAfter.ReplayURL != "https://cdn.example.com/valid.replay" {
		t.Fatalf("unexpected match state after promotion: %+v", recAfter)
	}
}

func TestAdversarial_Ballchasing409_DuplicateHandling_MixedBatch(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_409.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	const totalMatches = 20
	var discovered []DiscoveredMatch
	duplicates := make(map[string]bool)
	uploader := newMockUploader()

	for i := 0; i < totalMatches; i++ {
		guid := fmt.Sprintf("guid-%02d", i)
		discovered = append(discovered, DiscoveredMatch{
			MatchGUID:            guid,
			RecordStartTimestamp: int64(1000 + i),
			ReplayURL:            fmt.Sprintf("https://cdn.example.com/%s.replay", guid),
		})
		if i%2 == 0 {
			duplicates[guid] = true
			uploader.duplicates[guid] = true
		}
	}

	provider := newMockHistoryProvider(discovered...)
	syncerEngine := NewSyncerEngine(store, provider, newMockDownloader(), uploader, t.TempDir(), false)

	stats, err := syncerEngine.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	expectedDuplicates := totalMatches / 2
	expectedFresh := totalMatches - expectedDuplicates
	if stats.DiscoveredCount != totalMatches || stats.DownloadedCount != totalMatches ||
		stats.DuplicateCount != expectedDuplicates || stats.UploadedCount != expectedFresh || stats.FailedCount != 0 {
		t.Fatalf("unexpected mixed batch stats: %+v", stats)
	}

	for i := 0; i < totalMatches; i++ {
		guid := fmt.Sprintf("guid-%02d", i)
		rec, err := store.GetMatch(ctx, guid)
		if err != nil {
			t.Fatalf("failed to get match %s: %v", guid, err)
		}
		if duplicates[guid] {
			if rec.UploadStatus != storage.UploadDuplicate || rec.BallchasingID != fmt.Sprintf("bc-%s", guid) {
				t.Fatalf("match %s expected DUPLICATE, got %+v", guid, rec)
			}
		} else if rec.UploadStatus != storage.UploadUploaded {
			t.Fatalf("match %s expected UPLOADED, got %s", guid, rec.UploadStatus)
		}
	}

	statsCycle2, err := syncerEngine.RunCycle(ctx)
	if err != nil {
		t.Fatalf("cycle 2 failed: %v", err)
	}
	if statsCycle2.DownloadedCount != 0 || statsCycle2.UploadedCount != 0 || statsCycle2.DuplicateCount != 0 {
		t.Fatalf("cycle 2 was not idempotent: %+v", statsCycle2)
	}
}

func TestAdversarial_DryRun_ZeroMutationsWithPreSeededDirtyState(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_dryrun.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	seeds := []*storage.MatchRecord{
		{
			MatchGUID:            "dirty-pending-dl",
			RecordStartTimestamp: 100,
			ReplayURL:            "https://cdn.example.com/pdl.replay",
			DownloadStatus:       storage.DownloadPending,
			UploadStatus:         storage.UploadPending,
		},
		{
			MatchGUID:            "dirty-orphan-dl",
			RecordStartTimestamp: 200,
			ReplayURL:            "https://cdn.example.com/odl.replay",
			DownloadStatus:       storage.DownloadDownloading,
			UploadStatus:         storage.UploadPending,
		},
		{
			MatchGUID:            "dirty-pending-up",
			RecordStartTimestamp: 300,
			ReplayURL:            "https://cdn.example.com/pup.replay",
			DownloadStatus:       storage.DownloadDownloaded,
			LocalFilePath:        filepath.Join(t.TempDir(), "pup.replay"),
			UploadStatus:         storage.UploadPending,
		},
	}
	if err := store.UpsertDiscoveredMatches(ctx, seeds); err != nil {
		t.Fatalf("failed to seed dirty state: %v", err)
	}
	_ = store.MarkDownloading(ctx, "dirty-orphan-dl")

	recOdlBefore, _ := store.GetMatch(ctx, "dirty-orphan-dl")
	recPdlBefore, _ := store.GetMatch(ctx, "dirty-pending-dl")
	recPupBefore, _ := store.GetMatch(ctx, "dirty-pending-up")

	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "new-match-1", ReplayURL: "https://cdn.example.com/new1.replay"},
		DiscoveredMatch{MatchGUID: "new-match-2", ReplayURL: ""},
		DiscoveredMatch{MatchGUID: "dirty-pending-dl", ReplayURL: "https://cdn.example.com/pdl.replay"},
	)
	downloader := newMockDownloader()
	uploader := newMockUploader()

	syncerEngine := NewSyncerEngine(store, provider, downloader, uploader, t.TempDir(), true)
	stats, err := syncerEngine.RunCycle(ctx)
	if err != nil {
		t.Fatalf("dry-run RunCycle failed: %v", err)
	}
	if stats.DiscoveredCount != 3 || stats.DownloadedCount != 0 || stats.UploadedCount != 0 || stats.DuplicateCount != 0 {
		t.Fatalf("dry run stats mismatch: %+v", stats)
	}
	if downloader.calls != 0 || uploader.calls != 0 {
		t.Fatalf("downloader or uploader called in dry run: dl=%d up=%d", downloader.calls, uploader.calls)
	}
	if _, errNew1 := store.GetMatch(ctx, "new-match-1"); !errors.Is(errNew1, storage.ErrMatchNotFound) {
		t.Fatalf("new-match-1 was written to DB in dry run: %v", errNew1)
	}
	if _, errNew2 := store.GetMatch(ctx, "new-match-2"); !errors.Is(errNew2, storage.ErrMatchNotFound) {
		t.Fatalf("new-match-2 was written to DB in dry run: %v", errNew2)
	}

	recOdlAfter, _ := store.GetMatch(ctx, "dirty-orphan-dl")
	if recOdlAfter.DownloadStatus != recOdlBefore.DownloadStatus || recOdlAfter.UpdatedAt != recOdlBefore.UpdatedAt {
		t.Fatalf("orphaned record mutated in dry run: %s -> %s", recOdlBefore.DownloadStatus, recOdlAfter.DownloadStatus)
	}
	recPdlAfter, _ := store.GetMatch(ctx, "dirty-pending-dl")
	if recPdlAfter.DownloadStatus != recPdlBefore.DownloadStatus {
		t.Fatalf("pending dl record mutated in dry run: %s", recPdlAfter.DownloadStatus)
	}
	recPupAfter, _ := store.GetMatch(ctx, "dirty-pending-up")
	if recPupAfter.UploadStatus != recPupBefore.UploadStatus {
		t.Fatalf("pending up record mutated in dry run: %s", recPupAfter.UploadStatus)
	}
}
