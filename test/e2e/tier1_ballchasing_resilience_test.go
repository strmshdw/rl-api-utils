package e2e

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/testutil"
)

// ============================================================================
// Feature 11: Ballchasing HTTP 409 Deduplication
// ============================================================================

func TestTier1_F11_409_ParseDuplicateResponse(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SetDuplicateGUID("dup-guid", "existing-id-409")

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)
	filePath := filepath.Join(h.ReplayDir, "dup-guid.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("dup-guid", 2048), 0644)

	res, err := uploader.UploadReplay(context.Background(), "dup-guid", filePath)
	if err != nil {
		t.Fatalf("upload failed on 409: %v", err)
	}
	if !res.IsDuplicate || res.ID != "existing-id-409" {
		t.Fatalf("expected duplicate result with existing-id-409, got %+v", res)
	}
}

func TestTier1_F11_409_TransitionToDuplicate(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()
	ctx := context.Background()

	_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{{MatchGUID: "m409"}})
	_ = store.MarkDuplicate(ctx, "m409", "bc-dup-id", "http://bc/dup")

	rec, _ := store.GetMatch(ctx, "m409")
	if rec.UploadStatus != UploadDuplicate {
		t.Fatalf("expected DUPLICATE status, got %s", rec.UploadStatus)
	}
}

func TestTier1_F11_409_RecordExistingID(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()
	ctx := context.Background()

	_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{{MatchGUID: "m409-id"}})
	_ = store.MarkDuplicate(ctx, "m409-id", "extracted-duplicate-id", "http://bc/dup")

	rec, _ := store.GetMatch(ctx, "m409-id")
	if rec.BallchasingID != "extracted-duplicate-id" {
		t.Fatalf("expected extracted-duplicate-id, got %s", rec.BallchasingID)
	}
}

func TestTier1_F11_409_NoCallerError(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SetAlwaysDuplicate(true)

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)
	filePath := filepath.Join(h.ReplayDir, "noerr.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("noerr", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "noerr", filePath)
	if err != nil {
		t.Fatalf("409 duplicate replay must not return an error to caller, got: %v", err)
	}
}

func TestTier1_F11_409_ZeroRetriesAttempted(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SetAlwaysDuplicate(true)

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 3)
	filePath := filepath.Join(h.ReplayDir, "noretries.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("noretries", 2048), 0644)

	_, _ = uploader.UploadReplay(context.Background(), "noretries", filePath)

	// Since 409 is terminal, upload count should be exactly 1
	if h.BC.GetUploadCount() != 1 {
		t.Fatalf("expected exactly 1 upload attempt on 409, got %d", h.BC.GetUploadCount())
	}
}

// ============================================================================
// Feature 12: Ballchasing HTTP 429 Rate Limiting
// ============================================================================

func TestTier1_F12_429_ParseRetryAfterHeader(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SimulateRateLimit(1, 1)

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 2)
	filePath := filepath.Join(h.ReplayDir, "f12.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("f12", 2048), 0644)

	res, err := uploader.UploadReplay(context.Background(), "f12", filePath)
	if err != nil {
		t.Fatalf("expected successful retry after 429, got %v", err)
	}
	if res.ID == "" {
		t.Fatal("expected replay ID")
	}
}

func TestTier1_F12_429_BackoffAndSucceedOnRetry(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SimulateRateLimit(2, 1)

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 3)
	filePath := filepath.Join(h.ReplayDir, "f12-succ.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("f12-succ", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "f12-succ", filePath)
	if err != nil {
		t.Fatalf("expected success after 2 retries, got: %v", err)
	}
}

func TestTier1_F12_429_ExhaustionErrorAfterMaxRetries(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SimulateRateLimit(5, 1) // 5 failures > 2 max retries

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 2)
	filePath := filepath.Join(h.ReplayDir, "f12-fail.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("f12-fail", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "f12-fail", filePath)
	if err == nil {
		t.Fatal("expected retry exhaustion error")
	}
}

func TestTier1_F12_429_NonIntegerRetryAfterFallback(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SimulateRateLimit(1, 1)
	h.BC.SetCustomRetryAfterHeader("Wed, 21 Oct 2026 07:28:00 GMT")

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 2)
	filePath := filepath.Join(h.ReplayDir, "f12-date.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("f12-date", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "f12-date", filePath)
	if err != nil {
		t.Fatalf("expected fallback backoff to succeed, got: %v", err)
	}
}

func TestTier1_F12_429_ZeroRetryAfterImmediateRetry(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SimulateRateLimit(1, 0)

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 2)
	filePath := filepath.Join(h.ReplayDir, "f12-zero.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("f12-zero", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "f12-zero", filePath)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
}

// ============================================================================
// Feature 13: Ballchasing HTTP 401 & Permanent Errors
// ============================================================================

func TestTier1_F13_401_ImmediateHaltNoRetry(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SetAlwaysUnauthorized(true)

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 3)
	filePath := filepath.Join(h.ReplayDir, "f13-401.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("f13-401", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "f13-401", filePath)
	if err == nil {
		t.Fatal("expected 401 unauthorized error")
	}
}

func TestTier1_F13_400_BadRequestImmediateHalt(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SetForcedStatus(http.StatusBadRequest, "corrupt replay binary")

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 3)
	filePath := filepath.Join(h.ReplayDir, "f13-400.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("f13-400", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "f13-400", filePath)
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("expected 400 error, got: %v", err)
	}
}

func TestTier1_F13_Error_DescriptiveErrorPropagation(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SetForcedStatus(http.StatusBadRequest, "specific syntax error in replay")

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)
	filePath := filepath.Join(h.ReplayDir, "f13-desc.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("f13-desc", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "f13-desc", filePath)
	if err == nil || !strings.Contains(err.Error(), "specific syntax error in replay") {
		t.Fatalf("expected descriptive error message in: %v", err)
	}
}

func TestTier1_F13_Error_StatusTransitionToFailed(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()
	ctx := context.Background()

	_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{{MatchGUID: "m-fail"}})
	_ = store.MarkUploadFailed(ctx, "m-fail", "bad request")

	rec, _ := store.GetMatch(ctx, "m-fail")
	if rec.UploadStatus != UploadFailed || rec.LastError != "bad request" || rec.RetryCount != 1 {
		t.Fatalf("expected failed upload state: %+v", rec)
	}
}

func TestTier1_F13_Error_NoDaemonCrashOnUploadError(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SetForcedStatus(http.StatusBadRequest, "fatal rejection")

	m := testutil.NewMockMatchEntry("m-fatal", h.CDN.ReplayURL("m-fatal"), "Map", 2)
	h.PsyNet.AddMatch(m)

	adapter := NewMockProviderAdapter(h.PsyNet)
	downloader := NewHTTPReplayDownloader(10 * time.Second)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)

	syncer := NewSyncerEngine(h.Store, adapter, downloader, uploader, h.ReplayDir, false)

	// Must not panic or return fatal top-level error; returns stats with FailedCount = 1
	stats, err := syncer.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("syncer run cycle returned error instead of absorbing match failure: %v", err)
	}
	if stats.FailedCount != 1 {
		t.Fatalf("expected 1 failed upload in stats, got %d", stats.FailedCount)
	}
}
