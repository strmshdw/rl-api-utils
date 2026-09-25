package e2e

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/testutil"
)

// ============================================================================
// Scenario 1: Multi-Cycle Polling with Dynamic Match Progression
// ============================================================================

func TestTier4_Scenario1_MultiCyclePolling_DynamicMatchProgression(t *testing.T) {
	h := SetupE2EHarness(t)

	adapter := NewMockProviderAdapter(h.PsyNet)
	downloader := NewHTTPReplayDownloader(5 * time.Second)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 2)
	syncer := NewSyncerEngine(h.Store, adapter, downloader, uploader, h.ReplayDir, false)

	ctx := context.Background()

	// -------------------------------------------------------------
	// Cycle 1: 2 matches discovered
	// -------------------------------------------------------------
	m1 := testutil.NewMockMatchEntry("match-c1-1", h.CDN.ReplayURL("match-c1-1"), "Wasteland_P", 2)
	m2 := testutil.NewMockMatchEntry("match-c1-2", h.CDN.ReplayURL("match-c1-2"), "Stadium_P", 2)
	h.PsyNet.SetMatches([]testutil.MockMatchEntry{m1, m2})

	stats1, err := syncer.RunCycle(ctx)
	if err != nil {
		t.Fatalf("cycle 1 failed: %v", err)
	}

	if stats1.DiscoveredCount != 2 || stats1.DownloadedCount != 2 || stats1.UploadedCount != 2 {
		t.Fatalf("cycle 1 unexpected stats: %+v", stats1)
	}
	if h.CDN.GetTotalDownloads() != 2 {
		t.Fatalf("cycle 1 expected 2 CDN downloads, got %d", h.CDN.GetTotalDownloads())
	}
	if h.BC.GetUploadCount() != 2 {
		t.Fatalf("cycle 1 expected 2 Ballchasing uploads, got %d", h.BC.GetUploadCount())
	}

	// -------------------------------------------------------------
	// Cycle 2: 2 old matches + 2 new matches discovered (total 4)
	// -------------------------------------------------------------
	m3 := testutil.NewMockMatchEntry("match-c2-3", h.CDN.ReplayURL("match-c2-3"), "DFHStadium_P", 3)
	m4 := testutil.NewMockMatchEntry("match-c2-4", h.CDN.ReplayURL("match-c2-4"), "UtopiaColiseum_P", 1)
	h.PsyNet.SetMatches([]testutil.MockMatchEntry{m1, m2, m3, m4})

	stats2, err := syncer.RunCycle(ctx)
	if err != nil {
		t.Fatalf("cycle 2 failed: %v", err)
	}

	if stats2.DiscoveredCount != 4 {
		t.Fatalf("cycle 2 expected 4 discovered, got %d", stats2.DiscoveredCount)
	}
	// Zero re-downloads or re-uploads for m1 and m2; exactly 2 downloads and 2 uploads for m3 and m4
	if stats2.DownloadedCount != 2 || stats2.UploadedCount != 2 {
		t.Fatalf("cycle 2 must only download and upload new matches: %+v", stats2)
	}
	if h.CDN.GetTotalDownloads() != 4 {
		t.Fatalf("expected cumulative 4 CDN downloads, got %d", h.CDN.GetTotalDownloads())
	}
	if h.BC.GetUploadCount() != 4 {
		t.Fatalf("expected cumulative 4 Ballchasing uploads, got %d", h.BC.GetUploadCount())
	}

	// -------------------------------------------------------------
	// Cycle 3: No new matches (still m1, m2, m3, m4)
	// -------------------------------------------------------------
	stats3, err := syncer.RunCycle(ctx)
	if err != nil {
		t.Fatalf("cycle 3 failed: %v", err)
	}

	if stats3.DownloadedCount != 0 || stats3.UploadedCount != 0 {
		t.Fatalf("cycle 3 must have zero network mutations: %+v", stats3)
	}
	// Verifying zero additional requests
	if h.CDN.GetTotalDownloads() != 4 {
		t.Fatalf("expected 4 total downloads after idle cycle 3, got %d", h.CDN.GetTotalDownloads())
	}
	if h.BC.GetUploadCount() != 4 {
		t.Fatalf("expected 4 total uploads after idle cycle 3, got %d", h.BC.GetUploadCount())
	}
}

// ============================================================================
// Scenario 2: Cold Restart Persistence & Idempotency
// ============================================================================

func TestTier4_Scenario2_ColdRestartPersistence_Idempotency(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "rl-cold-restart-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	replayDir := filepath.Join(tempDir, "replays")
	_ = os.MkdirAll(replayDir, 0755)

	psy := testutil.NewMockPsyNetServer()
	defer psy.Close()
	bc := testutil.NewMockBallchasingServer()
	defer bc.Close()
	cdn := testutil.NewMockCDNServer()
	defer cdn.Close()

	// Shared persistent store simulating disk persistence across restart
	store := NewMemoryStateStore()

	// Populate PsyNet with 3 matches
	m1 := testutil.NewMockMatchEntry("cold-m1", cdn.ReplayURL("cold-m1"), "Map1", 2)
	m2 := testutil.NewMockMatchEntry("cold-m2", cdn.ReplayURL("cold-m2"), "Map2", 2)
	m3 := testutil.NewMockMatchEntry("cold-m3", cdn.ReplayURL("cold-m3"), "Map3", 2)
	psy.SetMatches([]testutil.MockMatchEntry{m1, m2, m3})

	ctx := context.Background()

	// -------------------------------------------------------------
	// Phase 1: Daemon Instance 1 starts up, syncs 3 matches, and stops
	// -------------------------------------------------------------
	{
		adapter1 := NewMockProviderAdapter(psy)
		downloader1 := NewHTTPReplayDownloader(5 * time.Second)
		uploader1 := NewHTTPBallchasingUploader(bc.URL(), "test-ballchasing-token", "public", "", 2)
		daemon1 := NewSyncerEngine(store, adapter1, downloader1, uploader1, replayDir, false)

		stats1, err := daemon1.RunCycle(ctx)
		if err != nil {
			t.Fatalf("daemon 1 cycle failed: %v", err)
		}
		if stats1.UploadedCount != 3 {
			t.Fatalf("daemon 1 expected 3 uploads, got %d", stats1.UploadedCount)
		}
	}

	if cdn.GetTotalDownloads() != 3 || bc.GetUploadCount() != 3 {
		t.Fatalf("phase 1 count mismatch: cdn=%d, bc=%d", cdn.GetTotalDownloads(), bc.GetUploadCount())
	}

	// -------------------------------------------------------------
	// Phase 2: Daemon Instance 2 starts up (simulating restart)
	// -------------------------------------------------------------
	{
		adapter2 := NewMockProviderAdapter(psy)
		downloader2 := NewHTTPReplayDownloader(5 * time.Second)
		uploader2 := NewHTTPBallchasingUploader(bc.URL(), "test-ballchasing-token", "public", "", 2)
		daemon2 := NewSyncerEngine(store, adapter2, downloader2, uploader2, replayDir, false)

		stats2, err := daemon2.RunCycle(ctx)
		if err != nil {
			t.Fatalf("daemon 2 cycle failed: %v", err)
		}

		// Absolute zero downloads and uploads on reboot
		if stats2.DownloadedCount != 0 || stats2.UploadedCount != 0 {
			t.Fatalf("daemon 2 performed duplicate work: %+v", stats2)
		}
	}

	// Network counters must remain unchanged at exactly 3
	if cdn.GetTotalDownloads() != 3 {
		t.Fatalf("expected CDN downloads to stay at 3, got %d", cdn.GetTotalDownloads())
	}
	if bc.GetUploadCount() != 3 {
		t.Fatalf("expected Ballchasing uploads to stay at 3, got %d", bc.GetUploadCount())
	}

	// Assert store consistency
	for _, guid := range []string{"cold-m1", "cold-m2", "cold-m3"} {
		rec, _ := store.GetMatch(ctx, guid)
		if rec == nil || rec.UploadStatus != UploadUploaded || rec.DownloadStatus != DownloadDownloaded {
			t.Fatalf("match %s state inconsistency after restart: %+v", guid, rec)
		}
	}
}

// ============================================================================
// Scenario 3: Transient CDN Network Outage & Self-Healing Recovery
// ============================================================================

func TestTier4_Scenario3_TransientCDNOutage_SelfHealingRecovery(t *testing.T) {
	h := SetupE2EHarness(t)

	guid := "outage-match"
	h.PsyNet.AddMatch(testutil.NewMockMatchEntry(guid, h.CDN.ReplayURL(guid), "Map", 2))

	adapter := NewMockProviderAdapter(h.PsyNet)
	downloader := NewHTTPReplayDownloader(5 * time.Second)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 2)
	syncer := NewSyncerEngine(h.Store, adapter, downloader, uploader, h.ReplayDir, false)

	ctx := context.Background()

	// -------------------------------------------------------------
	// Cycle 1: CDN is experiencing an outage (HTTP 500)
	// -------------------------------------------------------------
	h.CDN.SetStatusCode(guid, http.StatusInternalServerError)

	stats1, err := syncer.RunCycle(ctx)
	if err != nil {
		t.Fatalf("cycle 1 should absorb match failure, returned error: %v", err)
	}
	if stats1.DownloadedCount != 0 || stats1.UploadedCount != 0 {
		t.Fatalf("cycle 1 should not have downloaded or uploaded: %+v", stats1)
	}

	rec, _ := h.Store.GetMatch(ctx, guid)
	if rec.DownloadStatus != DownloadFailed {
		t.Fatalf("expected status FAILED after CDN outage, got %s", rec.DownloadStatus)
	}

	// -------------------------------------------------------------
	// Cycle 2: CDN recovers (status 200 OK)
	// -------------------------------------------------------------
	h.CDN.SetStatusCode(guid, http.StatusOK)

	// In real daemon, failed downloads transition back to PENDING or are retried
	_ = h.Store.RecoverInFlight(ctx)
	// Reset failed download to pending for retry
	rec.DownloadStatus = DownloadPending
	_ = h.Store.UpsertDiscoveredMatches(ctx, []*MatchRecord{rec})

	stats2, err := syncer.RunCycle(ctx)
	if err != nil {
		t.Fatalf("cycle 2 recovery failed: %v", err)
	}

	if stats2.DownloadedCount != 1 || stats2.UploadedCount != 1 {
		t.Fatalf("cycle 2 should have retried and succeeded: %+v", stats2)
	}

	recAfter, _ := h.Store.GetMatch(ctx, guid)
	if recAfter.UploadStatus != UploadUploaded || recAfter.DownloadStatus != DownloadDownloaded {
		t.Fatalf("expected match state to be completely recovered: %+v", recAfter)
	}
}

// ============================================================================
// Scenario 4: Ballchasing Burst Rate-Limiting & Self-Healing
// ============================================================================

func TestTier4_Scenario4_BallchasingBurstRateLimiting_SelfHealing(t *testing.T) {
	h := SetupE2EHarness(t)

	// Batch of 4 matches
	for i := 1; i <= 4; i++ {
		guid := fmt.Sprintf("burst-m%d", i)
		h.PsyNet.AddMatch(testutil.NewMockMatchEntry(guid, h.CDN.ReplayURL(guid), "Map", 2))
	}

	// Mock server will return 429 for the first 2 upload attempts, then succeed
	h.BC.SimulateRateLimit(2, 1)

	adapter := NewMockProviderAdapter(h.PsyNet)
	downloader := NewHTTPReplayDownloader(5 * time.Second)
	// Set maxRetries = 3 so the 2 rate limits are absorbed and resolved
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 3)
	syncer := NewSyncerEngine(h.Store, adapter, downloader, uploader, h.ReplayDir, false)

	ctx := context.Background()
	stats, err := syncer.RunCycle(ctx)
	if err != nil {
		t.Fatalf("burst upload with rate limiting failed: %v", err)
	}

	if stats.DownloadedCount != 4 {
		t.Fatalf("expected 4 downloaded, got %d", stats.DownloadedCount)
	}
	if stats.UploadedCount != 4 {
		t.Fatalf("expected all 4 uploads to succeed after backoff, got %d", stats.UploadedCount)
	}

	// Verify all 4 matches are marked UPLOADED in the store
	for i := 1; i <= 4; i++ {
		guid := fmt.Sprintf("burst-m%d", i)
		rec, _ := h.Store.GetMatch(ctx, guid)
		if rec == nil || rec.UploadStatus != UploadUploaded {
			t.Fatalf("match %s not marked uploaded: %+v", guid, rec)
		}
	}
}

// ============================================================================
// Scenario 5: Extended Multi-Cycle Soak Simulation (10 Cycles)
// ============================================================================

func TestTier4_Scenario5_ExtendedMultiCycle_SoakSimulation(t *testing.T) {
	h := SetupE2EHarness(t)

	adapter := NewMockProviderAdapter(h.PsyNet)
	downloader := NewHTTPReplayDownloader(5 * time.Second)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 2)
	syncer := NewSyncerEngine(h.Store, adapter, downloader, uploader, h.ReplayDir, false)

	ctx := context.Background()

	totalExpectedUploads := 0
	totalExpectedDuplicates := 0
	totalExpectedSkips := 0

	// Run 10 simulated consecutive polling cycles
	for cycle := 1; cycle <= 10; cycle++ {
		switch cycle {
		case 1:
			// Normal 2 matches
			m1 := testutil.NewMockMatchEntry("soak-c1-1", h.CDN.ReplayURL("soak-c1-1"), "Wasteland_P", 2)
			m2 := testutil.NewMockMatchEntry("soak-c1-2", h.CDN.ReplayURL("soak-c1-2"), "Stadium_P", 3)
			h.PsyNet.AddMatch(m1)
			h.PsyNet.AddMatch(m2)
			totalExpectedUploads += 2

		case 2:
			// Idle cycle (no new matches)

		case 3:
			// 1 new match + 1 duplicate match (409)
			m3 := testutil.NewMockMatchEntry("soak-c3-3", h.CDN.ReplayURL("soak-c3-3"), "DFH_P", 1)
			m4 := testutil.NewMockMatchEntry("soak-c3-4-dup", h.CDN.ReplayURL("soak-c3-4-dup"), "NeoTokyo_P", 2)
			h.PsyNet.AddMatch(m3)
			h.PsyNet.AddMatch(m4)
			h.BC.SetDuplicateGUID("soak-c3-4-dup", "bc-dup-c3-4")
			totalExpectedUploads += 1
			totalExpectedDuplicates += 1

		case 4:
			// 1 match with empty ReplayUrl
			m5 := testutil.NewMockMatchEntry("soak-c4-5-skip", "", "Aquadome_P", 2)
			h.PsyNet.AddMatch(m5)
			totalExpectedSkips += 1

		case 5:
			// Idle cycle

		case 6:
			// 2 new matches with intermittent 429
			m6 := testutil.NewMockMatchEntry("soak-c6-6", h.CDN.ReplayURL("soak-c6-6"), "ChampionsField_P", 2)
			m7 := testutil.NewMockMatchEntry("soak-c6-7", h.CDN.ReplayURL("soak-c6-7"), "RivalsArena_P", 2)
			h.PsyNet.AddMatch(m6)
			h.PsyNet.AddMatch(m7)
			h.BC.SimulateRateLimit(1, 1)
			totalExpectedUploads += 2

		default:
			// Cycles 7-10: steady state verification
		}

		stats, err := syncer.RunCycle(ctx)
		if err != nil {
			t.Fatalf("soak cycle %d failed: %v", cycle, err)
		}

		if stats == nil {
			t.Fatalf("soak cycle %d returned nil stats", cycle)
		}
	}

	// Verify final aggregate state
	if h.BC.GetUploadCount() != (totalExpectedUploads + totalExpectedDuplicates) {
		t.Fatalf("expected total Ballchasing operations %d, got %d",
			totalExpectedUploads+totalExpectedDuplicates, h.BC.GetUploadCount())
	}

	// Verify no orphaned temporary files in replay dir
	files, err := os.ReadDir(h.ReplayDir)
	if err != nil {
		t.Fatalf("failed to read replay dir: %v", err)
	}
	for _, f := range files {
		if strings.HasPrefix(f.Name(), ".tmp") {
			t.Fatalf("soak test leaked temporary file: %s", f.Name())
		}
	}
}
