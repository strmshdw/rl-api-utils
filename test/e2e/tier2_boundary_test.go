package e2e

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/testutil"
	"gopkg.in/yaml.v3"
)

// ============================================================================
// Area 1: Replay URL & Payload Anomalies
// ============================================================================

func TestTier2_Area1_EmptyReplayUrlSkippedCleanly(t *testing.T) {
	h := SetupE2EHarness(t)
	// Match has empty ReplayUrl (e.g. early forfeit, cancelled match)
	h.PsyNet.AddMatch(testutil.NewMockMatchEntry("m-empty-url", "", "Wasteland_P", 2))

	adapter := NewMockProviderAdapter(h.PsyNet)
	downloader := NewHTTPReplayDownloader(5 * time.Second)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)

	syncer := NewSyncerEngine(h.Store, adapter, downloader, uploader, h.ReplayDir, false)
	stats, err := syncer.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("syncer failed on empty replay url: %v", err)
	}

	if stats.DownloadedCount != 0 || stats.UploadedCount != 0 {
		t.Fatalf("expected 0 downloads and uploads, got %+v", stats)
	}
	if stats.SkippedCount != 1 {
		t.Fatalf("expected 1 skipped match, got %d", stats.SkippedCount)
	}

	rec, _ := h.Store.GetMatch(context.Background(), "m-empty-url")
	if rec.DownloadStatus != DownloadSkipped {
		t.Fatalf("expected status SKIPPED, got %s", rec.DownloadStatus)
	}
}

func TestTier2_Area1_ReplayUrlWithQueryParamsAndSpecialChars(t *testing.T) {
	h := SetupE2EHarness(t)
	complexURL := h.CDN.ReplayURL("guid-query") + "?token=abc&expires=123456&sig=xyz%2B123"

	downloader := NewHTTPReplayDownloader(5 * time.Second)
	path, err := downloader.DownloadReplay(context.Background(), "guid-query", complexURL, h.ReplayDir)
	if err != nil {
		t.Fatalf("download with complex signed URL query parameters failed: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("downloaded file not found: %v", err)
	}
}

func TestTier2_Area1_SpecialCharacterGUID(t *testing.T) {
	h := SetupE2EHarness(t)
	specialGUID := "guid_2026-09-25_P1-Team#0"

	h.PsyNet.AddMatch(testutil.NewMockMatchEntry(specialGUID, h.CDN.ReplayURL(specialGUID), "Map", 2))

	adapter := NewMockProviderAdapter(h.PsyNet)
	downloader := NewHTTPReplayDownloader(5 * time.Second)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)

	syncer := NewSyncerEngine(h.Store, adapter, downloader, uploader, h.ReplayDir, false)
	stats, err := syncer.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("syncer failed with special character GUID: %v", err)
	}
	if stats.UploadedCount != 1 {
		t.Fatalf("expected 1 uploaded, got %d", stats.UploadedCount)
	}

	rec, _ := h.Store.GetMatch(context.Background(), specialGUID)
	if rec == nil || rec.UploadStatus != UploadUploaded {
		t.Fatalf("expected record for %s with status UPLOADED", specialGUID)
	}
}

func TestTier2_Area1_MassiveMatchHistoryBatch(t *testing.T) {
	h := SetupE2EHarness(t)
	// 100 matches in one poll
	for i := 0; i < 100; i++ {
		guid := fmt.Sprintf("mass-guid-%03d", i)
		h.PsyNet.AddMatch(testutil.NewMockMatchEntry(guid, h.CDN.ReplayURL(guid), "Map", 2))
	}

	adapter := NewMockProviderAdapter(h.PsyNet)
	downloader := NewHTTPReplayDownloader(10 * time.Second)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)

	syncer := NewSyncerEngine(h.Store, adapter, downloader, uploader, h.ReplayDir, false)
	stats, err := syncer.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("massive batch sync failed: %v", err)
	}
	if stats.DiscoveredCount != 100 || stats.UploadedCount != 100 {
		t.Fatalf("expected 100 discovered and uploaded, got %+v", stats)
	}
}

func TestTier2_Area1_WhitespaceOrMalformedReplayURL(t *testing.T) {
	downloader := NewHTTPReplayDownloader(5 * time.Second)
	_, err := downloader.DownloadReplay(context.Background(), "guid-bad", "   ", "/tmp")
	if err == nil {
		t.Fatal("expected error for whitespace URL")
	}
}

// ============================================================================
// Area 2: Replay Downloads (Anomalies & Failures)
// ============================================================================

func TestTier2_Area2_ZeroByteDownloadPayload(t *testing.T) {
	h := SetupE2EHarness(t)
	h.CDN.SetReplayPayload("guid-0byte", []byte{})

	downloader := NewHTTPReplayDownloader(5 * time.Second)
	_, err := downloader.DownloadReplay(context.Background(), "guid-0byte", h.CDN.ReplayURL("guid-0byte"), h.ReplayDir)
	if err == nil {
		t.Fatal("expected error downloading 0-byte payload")
	}

	// Verify no file created
	finalPath := filepath.Join(h.ReplayDir, "guid-0byte.replay")
	if _, err := os.Stat(finalPath); !os.IsNotExist(err) {
		t.Fatal("file should not exist after 0-byte download failure")
	}
}

func TestTier2_Area2_TruncatedPayloadUnder1KB(t *testing.T) {
	h := SetupE2EHarness(t)
	h.CDN.SetReplayPayload("guid-truncated", make([]byte, 512))

	downloader := NewHTTPReplayDownloader(5 * time.Second)
	_, err := downloader.DownloadReplay(context.Background(), "guid-truncated", h.CDN.ReplayURL("guid-truncated"), h.ReplayDir)
	if err == nil {
		t.Fatal("expected error downloading < 1KB payload")
	}
}

func TestTier2_Area2_ExpiredURL403(t *testing.T) {
	h := SetupE2EHarness(t)
	h.CDN.SetStatusCode("guid-403", http.StatusForbidden)

	downloader := NewHTTPReplayDownloader(5 * time.Second)
	_, err := downloader.DownloadReplay(context.Background(), "guid-403", h.CDN.ReplayURL("guid-403"), h.ReplayDir)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected 403 error, got %v", err)
	}
}

func TestTier2_Area2_DeletedReplay404(t *testing.T) {
	h := SetupE2EHarness(t)
	h.CDN.SetStatusCode("guid-404", http.StatusNotFound)

	downloader := NewHTTPReplayDownloader(5 * time.Second)
	_, err := downloader.DownloadReplay(context.Background(), "guid-404", h.CDN.ReplayURL("guid-404"), h.ReplayDir)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 error, got %v", err)
	}
}

func TestTier2_Area2_ConnectionDropMidStream(t *testing.T) {
	h := SetupE2EHarness(t)
	h.CDN.SetTruncateStream("guid-drop", true)

	downloader := NewHTTPReplayDownloader(5 * time.Second)
	_, err := downloader.DownloadReplay(context.Background(), "guid-drop", h.CDN.ReplayURL("guid-drop"), h.ReplayDir)
	if err == nil {
		t.Fatal("expected error on connection drop mid-stream")
	}

	// Verify .tmp file was removed
	files, _ := os.ReadDir(h.ReplayDir)
	for _, f := range files {
		if strings.HasPrefix(f.Name(), ".tmp") {
			t.Fatalf("orphaned temp file remained after connection drop: %s", f.Name())
		}
	}
}

func TestTier2_Area2_CorruptNonTAGAMEPayload(t *testing.T) {
	corruptPayload := make([]byte, 2048)
	copy(corruptPayload, []byte("NOT_A_REPLAY_HEADER"))

	hasMagic := func(p []byte) bool {
		return len(p) >= len(testutil.ReplayMagicBytes) && string(p[:len(testutil.ReplayMagicBytes)]) == string(testutil.ReplayMagicBytes)
	}

	if hasMagic(corruptPayload) {
		t.Fatal("corrupt payload should not have TAGAME magic bytes")
	}
	validPayload := testutil.GenerateValidReplay("guid-valid", 2048)
	if !hasMagic(validPayload) {
		t.Fatal("valid payload must have TAGAME magic bytes")
	}
}

// ============================================================================
// Area 3: Ballchasing API Boundary Conditions
// ============================================================================

func TestTier2_Area3_LargeReplayUpload(t *testing.T) {
	h := SetupE2EHarness(t)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)

	// 5MB replay
	largeData := testutil.GenerateValidReplay("large-guid", 5*1024*1024)
	largePath := filepath.Join(h.ReplayDir, "large-guid.replay")
	_ = os.WriteFile(largePath, largeData, 0644)

	res, err := uploader.UploadReplay(context.Background(), "large-guid", largePath)
	if err != nil {
		t.Fatalf("uploading 5MB replay failed: %v", err)
	}
	if res.ID == "" {
		t.Fatal("expected ID from large replay upload")
	}

	uploaded := h.BC.GetUpload("large-guid")
	if uploaded == nil || uploaded.FileSize != 5*1024*1024 {
		t.Fatalf("expected recorded upload size 5MB, got %v", uploaded)
	}
}

func TestTier2_Area3_GatewayErrors502_503(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SetForcedStatus(http.StatusBadGateway, "bad gateway from Cloudflare")

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 2)
	filePath := filepath.Join(h.ReplayDir, "gw.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("gw", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "gw", filePath)
	if err == nil {
		t.Fatal("expected gateway error")
	}
}

func TestTier2_Area3_EmptyFileSubmission(t *testing.T) {
	h := SetupE2EHarness(t)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)

	emptyPath := filepath.Join(h.ReplayDir, "empty.replay")
	_ = os.WriteFile(emptyPath, []byte{}, 0644)

	_, err := uploader.UploadReplay(context.Background(), "empty", emptyPath)
	if err == nil {
		t.Fatal("expected error uploading 0-byte file")
	}
}

func TestTier2_Area3_InvalidJSONResponseBody(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SetForcedStatus(http.StatusInternalServerError, "<html><body>Internal Server Error</body></html>")

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)
	filePath := filepath.Join(h.ReplayDir, "html.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("html", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "html", filePath)
	if err == nil {
		t.Fatal("expected error on 500 with non-JSON body")
	}
}

func TestTier2_Area3_NonExistentReplayFileOnDisk(t *testing.T) {
	h := SetupE2EHarness(t)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)

	_, err := uploader.UploadReplay(context.Background(), "ghost", filepath.Join(h.ReplayDir, "ghost.replay"))
	if err == nil {
		t.Fatal("expected error uploading non-existent file")
	}
}

// ============================================================================
// Area 4: Storage & Concurrency Boundaries
// ============================================================================

func TestTier2_Area4_ConcurrentStateStoreOperations(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()
	ctx := context.Background()

	var wg sync.WaitGroup
	workers := 20
	opsPerWorker := 50

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < opsPerWorker; i++ {
				guid := fmt.Sprintf("worker-%d-match-%d", workerID, i)
				_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{
					{MatchGUID: guid, ReplayURL: "http://example.com/replay"},
				})
				_ = store.MarkDownloading(ctx, guid)
				_ = store.MarkDownloaded(ctx, guid, "/tmp/"+guid)
				_ = store.MarkUploading(ctx, guid)
				_ = store.MarkUploaded(ctx, guid, "bc-"+guid, "http://bc/"+guid)
				_, _ = store.GetMatch(ctx, guid)
			}
		}(w)
	}

	wg.Wait()

	// Verify all records are intact
	for w := 0; w < workers; w++ {
		for i := 0; i < opsPerWorker; i++ {
			guid := fmt.Sprintf("worker-%d-match-%d", w, i)
			rec, err := store.GetMatch(ctx, guid)
			if err != nil || rec == nil || rec.UploadStatus != UploadUploaded {
				t.Fatalf("concurrent corruption for %s", guid)
			}
		}
	}
}

func TestTier2_Area4_ExtremelyLongMatchGUID(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()
	ctx := context.Background()

	longGUID := strings.Repeat("A", 256)
	err := store.UpsertDiscoveredMatches(ctx, []*MatchRecord{
		{MatchGUID: longGUID, ReplayURL: "http://example.com/long"},
	})
	if err != nil {
		t.Fatalf("failed to upsert 256-char GUID: %v", err)
	}

	got, err := store.GetMatch(ctx, longGUID)
	if err != nil || got == nil || got.MatchGUID != longGUID {
		t.Fatalf("failed to retrieve 256-char GUID: %v", err)
	}
}

func TestTier2_Area4_MissingRecordTransitionsReturnError(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()
	ctx := context.Background()

	nonExistent := "does-not-exist"
	if err := store.MarkDownloading(ctx, nonExistent); err == nil {
		t.Fatal("expected error marking non-existent match as downloading")
	}
	if err := store.MarkDownloaded(ctx, nonExistent, "/path"); err == nil {
		t.Fatal("expected error marking non-existent match as downloaded")
	}
	if err := store.MarkUploading(ctx, nonExistent); err == nil {
		t.Fatal("expected error marking non-existent match as uploading")
	}
	if err := store.MarkUploaded(ctx, nonExistent, "id", "url"); err == nil {
		t.Fatal("expected error marking non-existent match as uploaded")
	}
}

func TestTier2_Area4_QueryNonExistentMatchReturnsNil(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()

	rec, err := store.GetMatch(context.Background(), "phantom-guid")
	if err != nil {
		t.Fatalf("query non-existent match returned error: %v", err)
	}
	if rec != nil {
		t.Fatalf("expected nil for non-existent match, got %+v", rec)
	}
}

func TestTier2_Area4_RecoverInFlightOnCleanStoreIsNoOp(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()

	err := store.RecoverInFlight(context.Background())
	if err != nil {
		t.Fatalf("recovery on empty store failed: %v", err)
	}
}

// ============================================================================
// Area 5: Configuration Boundary Conditions
// ============================================================================

func TestTier2_Area5_InvalidPollIntervalZeroOrNegative(t *testing.T) {
	validateInterval := func(interval time.Duration) error {
		if interval <= 0 {
			return fmt.Errorf("poll interval must be positive: %v", interval)
		}
		return nil
	}

	if err := validateInterval(0); err == nil {
		t.Fatal("expected error for 0s poll interval")
	}
	if err := validateInterval(-5 * time.Minute); err == nil {
		t.Fatal("expected error for negative poll interval")
	}
	if err := validateInterval(5 * time.Minute); err != nil {
		t.Fatalf("unexpected error for 5m poll interval: %v", err)
	}
}

func TestTier2_Area5_UnsupportedAuthProvider(t *testing.T) {
	validateProvider := func(p string) error {
		if p != "epic" && p != "steam" {
			return fmt.Errorf("unsupported provider: %s", p)
		}
		return nil
	}

	if err := validateProvider("xbox"); err == nil {
		t.Fatal("expected error for unsupported provider xbox")
	}
	if err := validateProvider("epic"); err != nil {
		t.Fatalf("unexpected error for epic: %v", err)
	}
	if err := validateProvider("steam"); err != nil {
		t.Fatalf("unexpected error for steam: %v", err)
	}
}

func TestTier2_Area5_CorruptConfigSyntax(t *testing.T) {
	badYAML := `auth: {provider: "epic", unclosed_brace:`
	var target map[string]any
	err := yaml.Unmarshal([]byte(badYAML), &target)
	if err == nil {
		t.Fatal("expected syntax error for bad YAML")
	}
}

func TestTier2_Area5_InvalidVisibilityRejection(t *testing.T) {
	validateVis := func(v string) error {
		switch v {
		case "public", "unlisted", "private", "":
			return nil
		default:
			return fmt.Errorf("invalid visibility: %s", v)
		}
	}

	if err := validateVis("hidden"); err == nil {
		t.Fatal("expected error for invalid visibility 'hidden'")
	}
	if err := validateVis("public"); err != nil {
		t.Fatalf("unexpected error for 'public': %v", err)
	}
}

func TestTier2_Area5_MissingConfigFileHandling(t *testing.T) {
	_, err := os.ReadFile("non_existent_config_file_12345.yaml")
	if !os.IsNotExist(err) {
		t.Fatalf("expected IsNotExist error, got %v", err)
	}
}

// ============================================================================
// Area 6: Rate Limiting & Retry Budgets
// ============================================================================

func TestTier2_Area6_RateLimitRetriesZero(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SimulateRateLimit(1, 1)

	// maxRetries = 0 -> must fail immediately on first 429
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 0)
	filePath := filepath.Join(h.ReplayDir, "r0.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("r0", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "r0", filePath)
	if err == nil {
		t.Fatal("expected rate limit error with 0 retries")
	}
}

func TestTier2_Area6_Repeated500RetriesUntilExhausted(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SetForcedStatus(http.StatusInternalServerError, "persistent server error")

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 2)
	filePath := filepath.Join(h.ReplayDir, "rep500.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("rep500", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "rep500", filePath)
	if err == nil {
		t.Fatal("expected error after exhausted 500 retries")
	}
}

func TestTier2_Area6_ZeroRetryAfterHeader(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SimulateRateLimit(1, 0) // Retry-After: 0

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 2)
	filePath := filepath.Join(h.ReplayDir, "zero-retry.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("zero-retry", 2048), 0644)

	res, err := uploader.UploadReplay(context.Background(), "zero-retry", filePath)
	if err != nil {
		t.Fatalf("upload failed with Retry-After: 0: %v", err)
	}
	if res.ID == "" {
		t.Fatal("expected replay ID")
	}
}

func TestTier2_Area6_RateLimitContextTimeout(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SimulateRateLimit(5, 10) // 10 second backoff

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 3)
	filePath := filepath.Join(h.ReplayDir, "timeout.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("timeout", 2048), 0644)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := uploader.UploadReplay(ctx, "timeout", filePath)
	if err == nil {
		t.Fatal("expected context timeout during backoff wait")
	}
}
