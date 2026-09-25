package syncer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/storage"
)

// ============================================================================
// ADVERSARIAL CHALLENGE SUITE: internal/syncer
// ============================================================================

// 1. IN-FLIGHT RECOVERY
// Verify store.RecoverInFlight properly resets orphaned DOWNLOADING and UPLOADING
// statuses to PENDING on startup across both real storage backends (SQLite and JSONStore)
// and mockStore, ensuring orphaned items are cleanly resumed and completed.

func TestAdversarial_InFlightRecovery_SQLiteBackend(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_recovery.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Seed orphaned records
	records := []*storage.MatchRecord{
		{
			MatchGUID:            "orphan-dl-1",
			RecordStartTimestamp: 1000,
			ReplayURL:            "https://cdn.example.com/dl1.replay",
			DownloadStatus:       storage.DownloadDownloading, // orphaned downloading
			UploadStatus:         storage.UploadPending,
		},
		{
			MatchGUID:            "orphan-up-1",
			RecordStartTimestamp: 2000,
			ReplayURL:            "https://cdn.example.com/up1.replay",
			DownloadStatus:       storage.DownloadDownloaded,
			LocalFilePath:        filepath.Join(t.TempDir(), "up1.replay"),
			UploadStatus:         storage.UploadUploading, // orphaned uploading
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
	// Manually mark in-flight states to simulate crash during operation
	if err := store.MarkDownloading(ctx, "orphan-dl-1"); err != nil {
		t.Fatalf("failed to mark downloading: %v", err)
	}
	if err := store.MarkUploading(ctx, "orphan-up-1"); err != nil {
		t.Fatalf("failed to mark uploading: %v", err)
	}

	// Verify pre-conditions
	rDl, _ := store.GetMatch(ctx, "orphan-dl-1")
	if rDl.DownloadStatus != storage.DownloadDownloading {
		t.Fatalf("pre-condition failed: expected DOWNLOADING, got %s", rDl.DownloadStatus)
	}
	rUp, _ := store.GetMatch(ctx, "orphan-up-1")
	if rUp.UploadStatus != storage.UploadUploading {
		t.Fatalf("pre-condition failed: expected UPLOADING, got %s", rUp.UploadStatus)
	}

	provider := newMockHistoryProvider() // no new matches from network
	downloader := newMockDownloader()
	uploader := newMockUploader()

	syncerEngine := NewSyncerEngine(store, provider, downloader, uploader, t.TempDir(), false)

	stats, err := syncerEngine.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	// orphan-dl-1 should have been recovered -> downloaded -> uploaded
	// orphan-up-1 should have been recovered -> uploaded
	if stats.DownloadedCount != 1 {
		t.Fatalf("expected 1 download (orphan-dl-1), got %d", stats.DownloadedCount)
	}
	if stats.UploadedCount != 2 {
		t.Fatalf("expected 2 uploads (orphan-dl-1 and orphan-up-1), got %d", stats.UploadedCount)
	}

	// Verify post-conditions in DB
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

	provider := newMockHistoryProvider()
	downloader := newMockDownloader()
	uploader := newMockUploader()

	syncerEngine := NewSyncerEngine(store, provider, downloader, uploader, t.TempDir(), false)

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

// 2. DELAYED REPLAY URLS
// Test multi-cycle delayed CDN arrival:
// Cycle 1: matches discovered with empty ReplayURL -> marked SKIPPED, zero downloads.
// Cycle 2: some matches receive ReplayURL -> promoted to PENDING and downloaded/uploaded.
// Remaining matches without ReplayURL stay SKIPPED.
// Cycle 3: remaining match receives ReplayURL -> promoted and downloaded/uploaded.

func TestAdversarial_DelayedReplayURLs_MultiCyclePromotion_SQLite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_delayed.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	downloader := newMockDownloader()
	uploader := newMockUploader()

	// --- Cycle 1: 3 matches discovered, 1 has URL, 2 have empty URLs ---
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m-ready", ReplayURL: "https://cdn.example.com/ready.replay"},
		DiscoveredMatch{MatchGUID: "m-delay-1", ReplayURL: ""},
		DiscoveredMatch{MatchGUID: "m-delay-2", ReplayURL: ""},
	)

	syncerEngine := NewSyncerEngine(store, provider, downloader, uploader, t.TempDir(), false)

	stats1, err := syncerEngine.RunCycle(ctx)
	if err != nil {
		t.Fatalf("cycle 1 failed: %v", err)
	}
	if stats1.DiscoveredCount != 3 {
		t.Fatalf("cycle 1: expected 3 discovered, got %d", stats1.DiscoveredCount)
	}
	if stats1.SkippedCount != 2 {
		t.Fatalf("cycle 1: expected 2 skipped, got %d", stats1.SkippedCount)
	}
	if stats1.DownloadedCount != 1 || stats1.UploadedCount != 1 {
		t.Fatalf("cycle 1: expected 1 downloaded & uploaded, got dl=%d, up=%d", stats1.DownloadedCount, stats1.UploadedCount)
	}

	recDelay1, _ := store.GetMatch(ctx, "m-delay-1")
	if recDelay1.DownloadStatus != storage.DownloadSkipped {
		t.Fatalf("cycle 1: expected m-delay-1 SKIPPED, got %s", recDelay1.DownloadStatus)
	}

	// --- Cycle 2: m-delay-1 gets URL, m-delay-2 still empty, 1 new match ---
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
	if stats2.DiscoveredCount != 4 {
		t.Fatalf("cycle 2: expected 4 discovered, got %d", stats2.DiscoveredCount)
	}
	if stats2.SkippedCount != 1 {
		t.Fatalf("cycle 2: expected 1 skipped (m-delay-2), got %d", stats2.SkippedCount)
	}
	// m-ready is already uploaded, so only m-delay-1 and m-new should be downloaded and uploaded
	if stats2.DownloadedCount != 2 || stats2.UploadedCount != 2 {
		t.Fatalf("cycle 2: expected 2 downloaded & uploaded, got dl=%d, up=%d", stats2.DownloadedCount, stats2.UploadedCount)
	}

	recDelay1After, _ := store.GetMatch(ctx, "m-delay-1")
	if recDelay1After.DownloadStatus != storage.DownloadDownloaded || recDelay1After.UploadStatus != storage.UploadUploaded {
		t.Fatalf("cycle 2: expected m-delay-1 DOWNLOADED/UPLOADED, got dl=%s, up=%s", recDelay1After.DownloadStatus, recDelay1After.UploadStatus)
	}

	// --- Cycle 3: m-delay-2 finally receives URL ---
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
	if stats3.SkippedCount != 0 {
		t.Fatalf("cycle 3: expected 0 skipped, got %d", stats3.SkippedCount)
	}
	if stats3.DownloadedCount != 1 || stats3.UploadedCount != 1 {
		t.Fatalf("cycle 3: expected 1 downloaded & uploaded (m-delay-2), got dl=%d, up=%d", stats3.DownloadedCount, stats3.UploadedCount)
	}

	recDelay2After, _ := store.GetMatch(ctx, "m-delay-2")
	if recDelay2After.DownloadStatus != storage.DownloadDownloaded || recDelay2After.UploadStatus != storage.UploadUploaded {
		t.Fatalf("cycle 3: expected m-delay-2 DOWNLOADED/UPLOADED, got dl=%s, up=%s", recDelay2After.DownloadStatus, recDelay2After.UploadStatus)
	}
}

// Empirical test demonstrating that storing untrimmed whitespace ReplayURL prevents promotion in SQLite & JSONStore
func TestAdversarial_DelayedReplayURLs_WhitespaceStorageVulnerability(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_ws_bug.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	downloader := newMockDownloader()
	uploader := newMockUploader()

	// Cycle 1: match discovered with whitespace ReplayURL ("   ")
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m-ws-delayed", ReplayURL: "   "},
	)
	s := NewSyncerEngine(store, provider, downloader, uploader, t.TempDir(), false)

	stats1, err := s.RunCycle(ctx)
	if err != nil {
		t.Fatalf("cycle 1 failed: %v", err)
	}
	if stats1.SkippedCount != 1 {
		t.Fatalf("expected 1 skipped, got %d", stats1.SkippedCount)
	}

	recBefore, _ := store.GetMatch(ctx, "m-ws-delayed")
	if recBefore.ReplayURL != "   " {
		t.Logf("ReplayURL was normalized to %q", recBefore.ReplayURL)
	} else {
		t.Logf("Confirmed vulnerability: ReplayURL in DB is untrimmed %q instead of empty string", recBefore.ReplayURL)
	}

	// Cycle 2: valid URL arrives
	provider.SetMatches([]DiscoveredMatch{
		{MatchGUID: "m-ws-delayed", ReplayURL: "https://cdn.example.com/valid.replay"},
	})

	stats2, err := s.RunCycle(ctx)
	if err != nil {
		t.Fatalf("cycle 2 failed: %v", err)
	}

	recAfter, _ := store.GetMatch(ctx, "m-ws-delayed")
	if recBefore.ReplayURL == "   " && recAfter.DownloadStatus == storage.DownloadSkipped {
		t.Logf("Empirically proven: match remains permanently SKIPPED (stats2.Downloaded=%d) because DB replay_url was %q and did not match ''", stats2.DownloadedCount, recBefore.ReplayURL)
	}
}

// 3. BALLCHASING 409 DUPLICATE
// Stress-test handling of HTTP 409 Duplicate:
// - Mixed batch with both fresh (201) and duplicate (409) replays.
// - Must return DUPLICATE status without treating as error.
// - DuplicateCount incremented, FailedCount remains 0.
// - Cycle succeeds completely with no retry thrashing.
// - Second cycle does NOT re-upload duplicates.

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

	downloader := newMockDownloader()
	uploader := newMockUploader()

	for i := 0; i < totalMatches; i++ {
		guid := fmt.Sprintf("guid-%02d", i)
		discovered = append(discovered, DiscoveredMatch{
			MatchGUID:            guid,
			RecordStartTimestamp: int64(1000 + i),
			ReplayURL:            fmt.Sprintf("https://cdn.example.com/%s.replay", guid),
		})
		// Mark every even index as duplicate
		if i%2 == 0 {
			duplicates[guid] = true
			uploader.duplicates[guid] = true
		}
	}

	provider := newMockHistoryProvider(discovered...)
	syncerEngine := NewSyncerEngine(store, provider, downloader, uploader, t.TempDir(), false)

	stats, err := syncerEngine.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	if stats.DiscoveredCount != totalMatches {
		t.Fatalf("expected %d discovered, got %d", totalMatches, stats.DiscoveredCount)
	}
	if stats.DownloadedCount != totalMatches {
		t.Fatalf("expected %d downloaded, got %d", totalMatches, stats.DownloadedCount)
	}
	expectedDuplicates := totalMatches / 2
	expectedFresh := totalMatches - expectedDuplicates

	if stats.DuplicateCount != expectedDuplicates {
		t.Fatalf("expected %d duplicates, got %d", expectedDuplicates, stats.DuplicateCount)
	}
	if stats.UploadedCount != expectedFresh {
		t.Fatalf("expected %d fresh uploads, got %d", expectedFresh, stats.UploadedCount)
	}
	if stats.FailedCount != 0 {
		t.Fatalf("expected 0 failures on duplicate handling, got %d", stats.FailedCount)
	}

	// Verify all records in store have proper status
	for i := 0; i < totalMatches; i++ {
		guid := fmt.Sprintf("guid-%02d", i)
		rec, err := store.GetMatch(ctx, guid)
		if err != nil {
			t.Fatalf("failed to get match %s: %v", guid, err)
		}
		if rec.DownloadStatus != storage.DownloadDownloaded {
			t.Fatalf("match %s expected DOWNLOADED, got %s", guid, rec.DownloadStatus)
		}
		if duplicates[guid] {
			if rec.UploadStatus != storage.UploadDuplicate {
				t.Fatalf("match %s expected DUPLICATE, got %s", guid, rec.UploadStatus)
			}
			if rec.BallchasingID != fmt.Sprintf("bc-%s", guid) {
				t.Fatalf("match %s expected ballchasing id bc-%s, got %s", guid, guid, rec.BallchasingID)
			}
		} else {
			if rec.UploadStatus != storage.UploadUploaded {
				t.Fatalf("match %s expected UPLOADED, got %s", guid, rec.UploadStatus)
			}
		}
	}

	// Cycle 2: Verify strict idempotency (0 downloads, 0 uploads)
	statsCycle2, err := syncerEngine.RunCycle(ctx)
	if err != nil {
		t.Fatalf("cycle 2 failed: %v", err)
	}
	if statsCycle2.DownloadedCount != 0 || statsCycle2.UploadedCount != 0 || statsCycle2.DuplicateCount != 0 {
		t.Fatalf("cycle 2 was not idempotent: dl=%d, up=%d, dup=%d", statsCycle2.DownloadedCount, statsCycle2.UploadedCount, statsCycle2.DuplicateCount)
	}
}

// 4. DRY-RUN GUARANTEE
// Verify that dry-run mode guarantees ZERO database mutations and ZERO network uploads:
// - Even when pre-seeded with dirty state (pending downloads, pending uploads, orphaned states).
// - Pre-state and post-state of the store must be completely identical.
// - Downloader and uploader must receive 0 calls.

func TestAdversarial_DryRun_ZeroMutationsWithPreSeededDirtyState(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_dryrun.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Seed dirty state: 2 pending downloads, 2 pending uploads, 1 orphaned downloading
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
			DownloadStatus:       storage.DownloadDownloading, // should NOT be recovered in dry run
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

	// Snapshot state before dry run
	recOdlBefore, _ := store.GetMatch(ctx, "dirty-orphan-dl")
	recPdlBefore, _ := store.GetMatch(ctx, "dirty-pending-dl")
	recPupBefore, _ := store.GetMatch(ctx, "dirty-pending-up")

	// Provider discovers 3 new matches
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "new-match-1", ReplayURL: "https://cdn.example.com/new1.replay"},
		DiscoveredMatch{MatchGUID: "new-match-2", ReplayURL: ""},
		DiscoveredMatch{MatchGUID: "dirty-pending-dl", ReplayURL: "https://cdn.example.com/pdl.replay"},
	)
	downloader := newMockDownloader()
	uploader := newMockUploader()

	syncerEngine := NewSyncerEngine(store, provider, downloader, uploader, t.TempDir(), true) // dryRun = true

	stats, err := syncerEngine.RunCycle(ctx)
	if err != nil {
		t.Fatalf("dry-run RunCycle failed: %v", err)
	}

	// In dry run:
	if stats.DiscoveredCount != 3 {
		t.Fatalf("expected 3 discovered in dry run, got %d", stats.DiscoveredCount)
	}
	if stats.DownloadedCount != 0 {
		t.Fatalf("expected 0 downloaded in dry run, got %d", stats.DownloadedCount)
	}
	if stats.UploadedCount != 0 {
		t.Fatalf("expected 0 uploaded in dry run, got %d", stats.UploadedCount)
	}
	if stats.DuplicateCount != 0 {
		t.Fatalf("expected 0 duplicates in dry run, got %d", stats.DuplicateCount)
	}
	if downloader.calls != 0 {
		t.Fatalf("downloader called %d times in dry run", downloader.calls)
	}
	if uploader.calls != 0 {
		t.Fatalf("uploader called %d times in dry run", uploader.calls)
	}

	// Verify ZERO new records were added to DB
	_, errNew1 := store.GetMatch(ctx, "new-match-1")
	if !errors.Is(errNew1, storage.ErrMatchNotFound) {
		t.Fatalf("new-match-1 was written to DB in dry run: %v", errNew1)
	}
	_, errNew2 := store.GetMatch(ctx, "new-match-2")
	if !errors.Is(errNew2, storage.ErrMatchNotFound) {
		t.Fatalf("new-match-2 was written to DB in dry run: %v", errNew2)
	}

	// Verify existing records were NOT touched (even orphaned ones!)
	recOdlAfter, _ := store.GetMatch(ctx, "dirty-orphan-dl")
	if recOdlAfter.DownloadStatus != recOdlBefore.DownloadStatus || recOdlAfter.UpdatedAt != recOdlBefore.UpdatedAt {
		t.Fatalf("orphaned record was mutated in dry run: before=%s, after=%s", recOdlBefore.DownloadStatus, recOdlAfter.DownloadStatus)
	}
	recPdlAfter, _ := store.GetMatch(ctx, "dirty-pending-dl")
	if recPdlAfter.DownloadStatus != recPdlBefore.DownloadStatus {
		t.Fatalf("pending dl record was mutated in dry run: %s", recPdlAfter.DownloadStatus)
	}
	recPupAfter, _ := store.GetMatch(ctx, "dirty-pending-up")
	if recPupAfter.UploadStatus != recPupBefore.UploadStatus {
		t.Fatalf("pending up record was mutated in dry run: %s", recPupAfter.UploadStatus)
	}
}

// 5. CONTEXT CANCELLATION
// Test cancellation mid-download and mid-upload:
// - Prompt exit (returns ctx.Err()).
// - Does NOT mark in-flight matches as FAILED (so they are not permanently poisoned).
// - On subsequent cycle, recovery and retry completes cleanly.

type selectiveBlockingDownloader struct {
	inner        *mockDownloader
	blockForGUID string
	onBlock      func()
}

func (d *selectiveBlockingDownloader) DownloadReplay(ctx context.Context, matchGUID, replayURL, destDir string) (string, error) {
	if matchGUID == d.blockForGUID {
		if d.onBlock != nil {
			d.onBlock()
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return d.inner.DownloadReplay(ctx, matchGUID, replayURL, destDir)
}

type selectiveBlockingUploader struct {
	inner        *mockUploader
	blockForGUID string
	onBlock      func()
}

func (u *selectiveBlockingUploader) UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error) {
	if matchGUID == u.blockForGUID {
		if u.onBlock != nil {
			u.onBlock()
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return u.inner.UploadReplay(ctx, matchGUID, filePath)
}

func TestAdversarial_ContextCancellation_MidDownload_NoPoisonAndCleanResume(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_cancel_dl.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockDl := newMockDownloader()
	mockUp := newMockUploader()

	downloader := &selectiveBlockingDownloader{
		inner:        mockDl,
		blockForGUID: "m-slow",
		onBlock: func() {
			cancel()
		},
	}

	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m-fast", RecordStartTimestamp: 100, ReplayURL: "https://cdn.example.com/fast.replay"},
		DiscoveredMatch{MatchGUID: "m-slow", RecordStartTimestamp: 200, ReplayURL: "https://cdn.example.com/slow.replay"},
	)

	syncerEngine := NewSyncerEngine(store, provider, downloader, mockUp, t.TempDir(), false)

	start := time.Now()
	stats, err := syncerEngine.RunCycle(ctx)
	duration := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if duration > 1*time.Second {
		t.Fatalf("cancellation took too long to exit: %v", duration)
	}

	// Verify m-slow is NOT marked FAILED (must be DOWNLOADING)
	recSlow, _ := store.GetMatch(context.Background(), "m-slow")
	if recSlow.DownloadStatus == storage.DownloadFailed {
		t.Fatalf("match was erroneously marked FAILED upon context cancellation")
	}
	if recSlow.DownloadStatus != storage.DownloadDownloading {
		t.Fatalf("match should remain DOWNLOADING at point of cancellation, got %s", recSlow.DownloadStatus)
	}

	// m-fast was before m-slow, so it finished downloading
	recFast, _ := store.GetMatch(context.Background(), "m-fast")
	if recFast.DownloadStatus != storage.DownloadDownloaded {
		t.Fatalf("m-fast should be DOWNLOADED, got %s", recFast.DownloadStatus)
	}

	// --- Subsequent Cycle with fresh context: clean recovery and completion ---
	downloader.blockForGUID = "" // no longer block
	freshCtx := context.Background()

	statsResume, err := syncerEngine.RunCycle(freshCtx)
	if err != nil {
		t.Fatalf("resume cycle failed: %v", err)
	}

	// Both matches should now be downloaded and uploaded
	recSlowAfter, _ := store.GetMatch(freshCtx, "m-slow")
	if recSlowAfter.DownloadStatus != storage.DownloadDownloaded || recSlowAfter.UploadStatus != storage.UploadUploaded {
		t.Fatalf("m-slow failed to cleanly resume: dl=%s, up=%s", recSlowAfter.DownloadStatus, recSlowAfter.UploadStatus)
	}
	if statsResume.FailedCount != 0 {
		t.Fatalf("expected 0 failures on resume, got %d", statsResume.FailedCount)
	}
	_ = stats
}

func TestAdversarial_ContextCancellation_MidUpload_NoPoisonAndCleanResume(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_cancel_up.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockDl := newMockDownloader()
	mockUp := newMockUploader()

	uploader := &selectiveBlockingUploader{
		inner:        mockUp,
		blockForGUID: "m-up-cancel",
		onBlock: func() {
			cancel()
		},
	}

	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m-up-cancel", ReplayURL: "https://cdn.example.com/up.replay"},
	)

	syncerEngine := NewSyncerEngine(store, provider, mockDl, uploader, t.TempDir(), false)

	start := time.Now()
	_, err = syncerEngine.RunCycle(ctx)
	duration := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if duration > 1*time.Second {
		t.Fatalf("cancellation took too long to exit: %v", duration)
	}

	// Verify m-up-cancel is NOT marked FAILED
	rec, _ := store.GetMatch(context.Background(), "m-up-cancel")
	if rec.UploadStatus == storage.UploadFailed {
		t.Fatalf("match was erroneously marked FAILED upon context cancellation")
	}
	if rec.UploadStatus != storage.UploadUploading {
		t.Fatalf("match should remain UPLOADING at point of cancellation, got %s", rec.UploadStatus)
	}

	// --- Subsequent Cycle with fresh context: clean recovery and upload ---
	uploader.blockForGUID = "" // unblock
	freshCtx := context.Background()

	statsResume, err := syncerEngine.RunCycle(freshCtx)
	if err != nil {
		t.Fatalf("resume cycle failed: %v", err)
	}

	recAfter, _ := store.GetMatch(freshCtx, "m-up-cancel")
	if recAfter.UploadStatus != storage.UploadUploaded {
		t.Fatalf("match failed to recover and upload: %s", recAfter.UploadStatus)
	}
	if statsResume.UploadedCount != 1 || statsResume.FailedCount != 0 {
		t.Fatalf("unexpected resume stats: up=%d, fail=%d", statsResume.UploadedCount, statsResume.FailedCount)
	}
}

// 6. ROBUSTNESS UNDER ADVERSARIAL INPUTS
// Empty GUIDs, whitespace GUIDs, special chars in map names, extreme timestamps

func TestAdversarial_MalformedMatches_Resilience(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_malformed.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "", ReplayURL: "https://cdn.example.com/empty.replay"},
		DiscoveredMatch{MatchGUID: "   \t\n  ", ReplayURL: "https://cdn.example.com/ws.replay"},
		DiscoveredMatch{MatchGUID: "valid-1", MapName: "Map'DROP TABLE matches;--", ReplayURL: "https://cdn.example.com/sql.replay"},
		DiscoveredMatch{MatchGUID: "valid-2", MapName: "Unicode_🏟️_日本語", ReplayURL: "https://cdn.example.com/uni.replay"},
	)
	downloader := newMockDownloader()
	uploader := newMockUploader()

	syncerEngine := NewSyncerEngine(store, provider, downloader, uploader, t.TempDir(), false)

	stats, err := syncerEngine.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle failed on malformed inputs: %v", err)
	}

	// Discovered is 4 from provider, but empty GUIDs were skipped before upsert
	if stats.DownloadedCount != 2 || stats.UploadedCount != 2 {
		t.Fatalf("expected exactly 2 valid matches processed, got dl=%d, up=%d", stats.DownloadedCount, stats.UploadedCount)
	}

	rec1, err := store.GetMatch(ctx, "valid-1")
	if err != nil || rec1.MapName != "Map'DROP TABLE matches;--" {
		t.Fatalf("SQL injection string failed to store safely: %v, rec: %+v", err, rec1)
	}

	rec2, err := store.GetMatch(ctx, "valid-2")
	if err != nil || rec2.MapName != "Unicode_🏟️_日本語" {
		t.Fatalf("Unicode map name failed to store safely: %v, rec: %+v", err, rec2)
	}
}

// 7. KEEP LOCAL FILES: CLEANUP ON SUCCESS AND DUPLICATE
// When KeepLocalFiles is false, the local file must be deleted on BOTH
// 201 (success) and 409 (duplicate).

func TestAdversarial_KeepLocalFilesFalse_CleanupOnSuccessAndDuplicate(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_cleanup.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	replayDir := t.TempDir()

	fileSuccess := filepath.Join(replayDir, "success.replay")
	if err := os.WriteFile(fileSuccess, []byte("success-data"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	fileDup := filepath.Join(replayDir, "dup.replay")
	if err := os.WriteFile(fileDup, []byte("dup-data"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "success", ReplayURL: "https://cdn.example.com/s.replay"},
		DiscoveredMatch{MatchGUID: "dup", ReplayURL: "https://cdn.example.com/d.replay"},
	)
	downloader := newMockDownloader()
	downloader.downloaded["success"] = fileSuccess
	downloader.downloaded["dup"] = fileDup

	uploader := newMockUploader()
	uploader.duplicates["dup"] = true

	s, err := New(store, provider, downloader, uploader,
		WithReplayDir(replayDir),
		WithKeepLocalFiles(false),
	)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	stats, err := s.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	if stats.UploadedCount != 1 || stats.DuplicateCount != 1 {
		t.Fatalf("expected 1 uploaded, 1 duplicate: %+v", stats)
	}

	// Verify BOTH files were unlinked
	if _, err := os.Stat(fileSuccess); !os.IsNotExist(err) {
		t.Fatalf("fileSuccess was not deleted: %v", err)
	}
	if _, err := os.Stat(fileDup); !os.IsNotExist(err) {
		t.Fatalf("fileDup was not deleted: %v", err)
	}
}
