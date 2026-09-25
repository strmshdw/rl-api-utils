package e2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/testutil"
)

// ============================================================================
// Pairwise 1: Epic Auth + Dry-Run Mode
// ============================================================================

func TestTier3_Pair1_EpicAuth_With_DryRun(t *testing.T) {
	h := SetupE2EHarness(t)

	// Epic player auth
	_ = h.Store.SaveAuthState(context.Background(), "epic", "epic-tok", "acc-1", "EpicUser")

	// Discovered matches on PsyNet
	m1 := testutil.NewMockMatchEntry("epic-dry-1", h.CDN.ReplayURL("epic-dry-1"), "Wasteland_P", 2)
	h.PsyNet.AddMatch(m1)

	adapter := NewMockProviderAdapter(h.PsyNet)
	downloader := NewHTTPReplayDownloader(5 * time.Second)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)

	// Dry run enabled
	syncer := NewSyncerEngine(h.Store, adapter, downloader, uploader, h.ReplayDir, true)
	stats, err := syncer.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("dry run failed: %v", err)
	}

	if stats.DiscoveredCount != 1 {
		t.Fatalf("expected 1 discovered match, got %d", stats.DiscoveredCount)
	}
	if stats.DownloadedCount != 0 || stats.UploadedCount != 0 {
		t.Fatalf("dry run must not download or upload: %+v", stats)
	}
	if h.BC.GetUploadCount() != 0 || h.CDN.GetTotalDownloads() != 0 {
		t.Fatal("network servers contacted during dry run")
	}

	// Verify store has not recorded any match
	rec, _ := h.Store.GetMatch(context.Background(), "epic-dry-1")
	if rec != nil {
		t.Fatalf("match should not be committed during dry run, got: %+v", rec)
	}
}

// ============================================================================
// Pairwise 2: Steam Auth + Duplicate Replay (HTTP 409)
// ============================================================================

func TestTier3_Pair2_SteamAuth_With_DuplicateReplay(t *testing.T) {
	h := SetupE2EHarness(t)

	// Steam player auth
	_ = h.Store.SaveAuthState(context.Background(), "steam", "ticket", "76561198000000000", "SteamUser")

	// Match configured to return 409 on Ballchasing
	guid := "steam-dup-match"
	h.PsyNet.AddMatch(testutil.NewMockMatchEntry(guid, h.CDN.ReplayURL(guid), "Stadium_P", 3))
	h.BC.SetDuplicateGUID(guid, "existing-ballchasing-steam-id")

	adapter := NewMockProviderAdapter(h.PsyNet)
	downloader := NewHTTPReplayDownloader(5 * time.Second)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "unlisted", "", 1)

	syncer := NewSyncerEngine(h.Store, adapter, downloader, uploader, h.ReplayDir, false)
	stats, err := syncer.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("syncer failed on duplicate: %v", err)
	}

	if stats.DuplicateCount != 1 || stats.UploadedCount != 0 {
		t.Fatalf("expected 1 duplicate, 0 new uploads, got %+v", stats)
	}

	rec, err := h.Store.GetMatch(context.Background(), guid)
	if err != nil || rec == nil {
		t.Fatalf("failed to retrieve match from store: %v", err)
	}
	if rec.UploadStatus != UploadDuplicate {
		t.Fatalf("expected status DUPLICATE, got %s", rec.UploadStatus)
	}
	if rec.BallchasingID != "existing-ballchasing-steam-id" {
		t.Fatalf("expected existing-ballchasing-steam-id, got %s", rec.BallchasingID)
	}
}

// ============================================================================
// Pairwise 3: Rate Limiting (429) + Daemon Graceful Shutdown
// ============================================================================

func TestTier3_Pair3_RateLimiting_With_GracefulShutdown(t *testing.T) {
	h := SetupE2EHarness(t)

	// Simulate high backoff rate limit
	h.BC.SimulateRateLimit(5, 10)

	guid := "rate-drain-match"
	h.PsyNet.AddMatch(testutil.NewMockMatchEntry(guid, h.CDN.ReplayURL(guid), "Map", 2))

	adapter := NewMockProviderAdapter(h.PsyNet)
	downloader := NewHTTPReplayDownloader(5 * time.Second)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 3)

	syncer := NewSyncerEngine(h.Store, adapter, downloader, uploader, h.ReplayDir, false)

	ctx, cancel := context.WithCancel(context.Background())

	// Trigger shutdown shortly after upload begins
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := syncer.RunCycle(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected context canceled or aborted sync")
	}
	// Must abort quickly without waiting out full 10s backoff
	if elapsed > 1*time.Second {
		t.Fatalf("drain took too long (%v), did not abort rate limit sleep immediately", elapsed)
	}
}

// ============================================================================
// Pairwise 4: Multi-Match Batch with Mixed Outcomes (201, 409, 429, Skipped)
// ============================================================================

func TestTier3_Pair4_MultiMatchBatch_MixedOutcomes(t *testing.T) {
	h := SetupE2EHarness(t)

	m1 := testutil.NewMockMatchEntry("m1-succ", h.CDN.ReplayURL("m1-succ"), "Map1", 2)
	m2 := testutil.NewMockMatchEntry("m2-dup", h.CDN.ReplayURL("m2-dup"), "Map2", 2)
	m3 := testutil.NewMockMatchEntry("m3-skip", "", "Map3", 2) // empty URL -> skipped
	m4 := testutil.NewMockMatchEntry("m4-retry", h.CDN.ReplayURL("m4-retry"), "Map4", 2)

	h.PsyNet.SetMatches([]testutil.MockMatchEntry{m1, m2, m3, m4})
	h.BC.SetDuplicateGUID("m2-dup", "dup-id-2")
	h.BC.SimulateRateLimit(1, 1) // m1 or m4 hits 429 once, retries and succeeds

	adapter := NewMockProviderAdapter(h.PsyNet)
	downloader := NewHTTPReplayDownloader(5 * time.Second)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 3)

	syncer := NewSyncerEngine(h.Store, adapter, downloader, uploader, h.ReplayDir, false)
	stats, err := syncer.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("mixed batch failed: %v", err)
	}

	if stats.DiscoveredCount != 4 {
		t.Fatalf("expected 4 discovered, got %d", stats.DiscoveredCount)
	}
	if stats.DownloadedCount != 3 {
		t.Fatalf("expected 3 downloaded (m1, m2, m4), got %d", stats.DownloadedCount)
	}
	if stats.DuplicateCount != 1 {
		t.Fatalf("expected 1 duplicate (m2), got %d", stats.DuplicateCount)
	}
	if stats.UploadedCount != 2 {
		t.Fatalf("expected 2 newly uploaded (m1, m4), got %d", stats.UploadedCount)
	}
	if stats.SkippedCount != 1 {
		t.Fatalf("expected 1 skipped (m3), got %d", stats.SkippedCount)
	}

	// Verify statuses in store
	r1, _ := h.Store.GetMatch(context.Background(), "m1-succ")
	if r1.UploadStatus != UploadUploaded {
		t.Fatalf("m1 expected UPLOADED, got %s", r1.UploadStatus)
	}

	r2, _ := h.Store.GetMatch(context.Background(), "m2-dup")
	if r2.UploadStatus != UploadDuplicate || r2.BallchasingID != "dup-id-2" {
		t.Fatalf("m2 expected DUPLICATE with dup-id-2, got %+v", r2)
	}

	r3, _ := h.Store.GetMatch(context.Background(), "m3-skip")
	if r3.DownloadStatus != DownloadSkipped {
		t.Fatalf("m3 expected SKIPPED, got %s", r3.DownloadStatus)
	}

	r4, _ := h.Store.GetMatch(context.Background(), "m4-retry")
	if r4.UploadStatus != UploadUploaded {
		t.Fatalf("m4 expected UPLOADED after retry, got %s", r4.UploadStatus)
	}
}

// ============================================================================
// Pairwise 5: Crash Mid-Download + Startup Recovery
// ============================================================================

func TestTier3_Pair5_CrashMidDownload_StartupRecovery(t *testing.T) {
	h := SetupE2EHarness(t)

	guid := "crash-dl-match"
	_ = h.Store.UpsertDiscoveredMatches(context.Background(), []*MatchRecord{
		{MatchGUID: guid, ReplayURL: h.CDN.ReplayURL(guid)},
	})

	// Simulate crash during download
	_ = h.Store.MarkDownloading(context.Background(), guid)

	recBefore, _ := h.Store.GetMatch(context.Background(), guid)
	if recBefore.DownloadStatus != DownloadDownloading {
		t.Fatalf("expected DOWNLOADING before recovery")
	}

	// RecoverInFlight resets state
	_ = h.Store.RecoverInFlight(context.Background())

	recAfter, _ := h.Store.GetMatch(context.Background(), guid)
	if recAfter.DownloadStatus != DownloadPending {
		t.Fatalf("expected PENDING after recovery, got %s", recAfter.DownloadStatus)
	}

	// Next cycle runs normally
	h.PsyNet.AddMatch(testutil.NewMockMatchEntry(guid, h.CDN.ReplayURL(guid), "Map", 2))
	adapter := NewMockProviderAdapter(h.PsyNet)
	downloader := NewHTTPReplayDownloader(5 * time.Second)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)

	syncer := NewSyncerEngine(h.Store, adapter, downloader, uploader, h.ReplayDir, false)
	stats, err := syncer.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("syncer failed after recovery: %v", err)
	}
	if stats.DownloadedCount != 1 || stats.UploadedCount != 1 {
		t.Fatalf("expected 1 download and upload after recovery, got %+v", stats)
	}
}

// ============================================================================
// Pairwise 6: Crash Mid-Upload + Startup Recovery
// ============================================================================

func TestTier3_Pair6_CrashMidUpload_StartupRecovery(t *testing.T) {
	h := SetupE2EHarness(t)

	guid := "crash-up-match"
	filePath := filepath.Join(h.ReplayDir, guid+".replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay(guid, 2048), 0644)

	_ = h.Store.UpsertDiscoveredMatches(context.Background(), []*MatchRecord{
		{MatchGUID: guid, ReplayURL: h.CDN.ReplayURL(guid)},
	})
	_ = h.Store.MarkDownloaded(context.Background(), guid, filePath)

	// Simulate crash during upload
	_ = h.Store.MarkUploading(context.Background(), guid)

	_ = h.Store.RecoverInFlight(context.Background())

	rec, _ := h.Store.GetMatch(context.Background(), guid)
	if rec.UploadStatus != UploadPending || rec.DownloadStatus != DownloadDownloaded {
		t.Fatalf("expected Downloaded + Pending Upload, got %s / %s", rec.DownloadStatus, rec.UploadStatus)
	}

	// Resuming cycle completes upload without re-downloading
	h.PsyNet.AddMatch(testutil.NewMockMatchEntry(guid, h.CDN.ReplayURL(guid), "Map", 2))
	adapter := NewMockProviderAdapter(h.PsyNet)
	downloader := NewHTTPReplayDownloader(5 * time.Second)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)

	syncer := NewSyncerEngine(h.Store, adapter, downloader, uploader, h.ReplayDir, false)
	stats, err := syncer.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("syncer resume failed: %v", err)
	}

	if stats.DownloadedCount != 0 {
		t.Fatalf("must not re-download already downloaded file")
	}
	if stats.UploadedCount != 1 {
		t.Fatalf("expected 1 upload, got %d", stats.UploadedCount)
	}
}

// ============================================================================
// Pairwise 7: Single-Run (--once) with Database Commits
// ============================================================================

func TestTier3_Pair7_SingleRun_OnceMode_WithStoreCommit(t *testing.T) {
	h := SetupE2EHarness(t)

	guid := "single-run-match"
	h.PsyNet.AddMatch(testutil.NewMockMatchEntry(guid, h.CDN.ReplayURL(guid), "Map", 2))

	adapter := NewMockProviderAdapter(h.PsyNet)
	downloader := NewHTTPReplayDownloader(5 * time.Second)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)

	syncer := NewSyncerEngine(h.Store, adapter, downloader, uploader, h.ReplayDir, false)

	// Single run execution
	stats, err := syncer.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("single run cycle failed: %v", err)
	}
	if stats.UploadedCount != 1 {
		t.Fatalf("expected 1 upload, got %d", stats.UploadedCount)
	}

	// Store must have committed state
	rec, err := h.Store.GetMatch(context.Background(), guid)
	if err != nil || rec == nil || rec.UploadStatus != UploadUploaded {
		t.Fatalf("match was not committed on single-run completion: %+v", rec)
	}
}

// ============================================================================
// Pairwise 8: Custom Visibility + Group ID Uploads
// ============================================================================

func TestTier3_Pair8_CustomVisibility_With_GroupID(t *testing.T) {
	h := SetupE2EHarness(t)

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "private", "weekly-scrims", 1)

	guid := "vis-grp-match"
	filePath := filepath.Join(h.ReplayDir, guid+".replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay(guid, 2048), 0644)

	res, err := uploader.UploadReplay(context.Background(), guid, filePath)
	if err != nil {
		t.Fatalf("upload with custom visibility and group failed: %v", err)
	}
	if res.ID == "" {
		t.Fatal("expected ID")
	}

	recorded := h.BC.GetUpload(guid)
	if recorded == nil || recorded.Visibility != "private" || recorded.Group != "weekly-scrims" {
		t.Fatalf("upload parameters not matched: %+v", recorded)
	}
}
