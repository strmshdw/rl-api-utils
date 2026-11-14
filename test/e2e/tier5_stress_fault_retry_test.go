package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/psynet"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/syncer"
	"github.com/dank/rl-api-utils/internal/testutil"
)

// ============================================================================
// Suite 3: Fault Injection — Abrupt Network Cutoff During Multipart Streaming
// ============================================================================

// cutoffHandler simulates an HTTP endpoint that prematurely cuts off the TCP socket
// mid-stream during multipart upload.
type cutoffHandler struct {
	mu             sync.Mutex
	attempts       int
	cutoffsAllowed int // number of attempts to drop before allowing success
	readBeforeDrop int // bytes to read before abrupt connection drop
}

func (h *cutoffHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	attempt := h.attempts
	h.attempts++
	shouldCutoff := attempt < h.cutoffsAllowed
	h.mu.Unlock()

	if shouldCutoff {
		// Read a few bytes of the incoming multipart body to simulate mid-stream transfer
		buf := make([]byte, h.readBeforeDrop)
		_, _ = io.ReadFull(r.Body, buf)

		// Abruptly hijack TCP socket and close without sending valid HTTP response
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, _ := hj.Hijack()
			if conn != nil {
				_ = conn.Close()
			}
		}
		return
	}

	// Read remaining payload and return 201 Created
	_, _ = io.ReadAll(r.Body)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", "https://ballchasing.com/replay/cutoff-recovered-id")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"id":       "cutoff-recovered-id",
		"location": "https://ballchasing.com/replay/cutoff-recovered-id",
	})
}

func TestTier5_Stress_FaultInjection_AbruptNetworkCutoffStreaming(t *testing.T) {
	tempDir := t.TempDir()
	replayFile := filepath.Join(tempDir, "sample.replay")
	if err := os.WriteFile(replayFile, testutil.GenerateValidReplay("sample", 8192), 0644); err != nil {
		t.Fatalf("failed to create sample replay: %v", err)
	}

	// ------------------------------------------------------------------------
	// Sub-test A: Permanent Network Cutoff with Zero-RAM Streaming (StreamUpload=true)
	// ------------------------------------------------------------------------
	t.Run("PermanentCutoff_StreamingUpload", func(t *testing.T) {
		handler := &cutoffHandler{
			cutoffsAllowed: 999, // cutoff every attempt
			readBeforeDrop: 64,
		}
		srv := httptest.NewServer(handler)
		defer srv.Close()

		client, err := ballchasing.NewClient(ballchasing.ClientConfig{
			BaseURL:      srv.URL,
			APIKey:       "test-token",
			Visibility:   "public",
			MaxRetries:   3,
			BaseBackoff:  5 * time.Millisecond,
			Timeout:      2 * time.Second,
			StreamUpload: true, // test zero-RAM streaming
		})
		if err != nil {
			t.Fatalf("failed to create client: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		res, err := client.UploadReplay(ctx, "sample", replayFile)
		if err == nil {
			t.Fatalf("expected error from permanent cutoff, got success: %+v", res)
		}

		// Verify 4 attempts (1 initial + 3 retries)
		handler.mu.Lock()
		attemptsMade := handler.attempts
		handler.mu.Unlock()

		if attemptsMade != 4 {
			t.Errorf("expected exactly 4 upload attempts, got %d", attemptsMade)
		}

		// Verify file descriptor is NOT leaked or held open
		// On Windows, if open file descriptor remained, os.Remove or os.OpenFile with write would fail
		f, fErr := os.OpenFile(replayFile, os.O_RDWR, 0644)
		if fErr != nil {
			t.Fatalf("FILE DESCRIPTOR LEAK DETECTED: file is still locked by process: %v", fErr)
		}
		_ = f.Close()
	})

	// ------------------------------------------------------------------------
	// Sub-test B: Permanent Network Cutoff with Buffered Upload (StreamUpload=false)
	// ------------------------------------------------------------------------
	t.Run("PermanentCutoff_BufferedUpload", func(t *testing.T) {
		handler := &cutoffHandler{
			cutoffsAllowed: 999,
			readBeforeDrop: 64,
		}
		srv := httptest.NewServer(handler)
		defer srv.Close()

		client, err := ballchasing.NewClient(ballchasing.ClientConfig{
			BaseURL:      srv.URL,
			APIKey:       "test-token",
			Visibility:   "public",
			MaxRetries:   3,
			BaseBackoff:  5 * time.Millisecond,
			Timeout:      2 * time.Second,
			StreamUpload: false,
		})
		if err != nil {
			t.Fatalf("failed to create client: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, err = client.UploadReplay(ctx, "sample", replayFile)
		if err == nil {
			t.Fatalf("expected error from permanent cutoff in buffered mode")
		}

		handler.mu.Lock()
		attemptsMade := handler.attempts
		handler.mu.Unlock()

		if attemptsMade != 4 {
			t.Errorf("expected exactly 4 attempts in buffered mode, got %d", attemptsMade)
		}
	})

	// ------------------------------------------------------------------------
	// Sub-test C: Transient Network Cutoff with Self-Healing Recovery
	// ------------------------------------------------------------------------
	t.Run("TransientCutoff_SelfHealingRecovery", func(t *testing.T) {
		handler := &cutoffHandler{
			cutoffsAllowed: 2, // drop first 2 attempts, succeed on 3rd attempt
			readBeforeDrop: 64,
		}
		srv := httptest.NewServer(handler)
		defer srv.Close()

		client, err := ballchasing.NewClient(ballchasing.ClientConfig{
			BaseURL:      srv.URL,
			APIKey:       "test-token",
			Visibility:   "public",
			MaxRetries:   3,
			BaseBackoff:  5 * time.Millisecond,
			Timeout:      2 * time.Second,
			StreamUpload: true,
		})
		if err != nil {
			t.Fatalf("failed to create client: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		res, err := client.UploadReplay(ctx, "sample", replayFile)
		if err != nil {
			t.Fatalf("expected self-healing upload recovery, got error: %v", err)
		}

		if res == nil || res.ID != "cutoff-recovered-id" {
			t.Fatalf("unexpected upload result: %+v", res)
		}

		handler.mu.Lock()
		attemptsMade := handler.attempts
		handler.mu.Unlock()

		if attemptsMade != 3 {
			t.Errorf("expected 3 attempts before success, got %d", attemptsMade)
		}
	})

	// ------------------------------------------------------------------------
	// Sub-test D: Mid-Stream Context Cancellation Aborts Gracefully
	// ------------------------------------------------------------------------
	t.Run("ContextCancellation_MidStream", func(t *testing.T) {
		// Server that hangs without responding
		hangingSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			buf := make([]byte, 32)
			_, _ = io.ReadFull(r.Body, buf)
			time.Sleep(500 * time.Millisecond) // block
		}))
		defer hangingSrv.Close()

		client, err := ballchasing.NewClient(ballchasing.ClientConfig{
			BaseURL:      hangingSrv.URL,
			APIKey:       "test-token",
			Visibility:   "public",
			MaxRetries:   3,
			BaseBackoff:  10 * time.Millisecond,
			StreamUpload: true,
		})
		if err != nil {
			t.Fatalf("failed to create client: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()

		start := time.Now()
		_, err = client.UploadReplay(ctx, "sample", replayFile)
		elapsed := time.Since(start)

		if err == nil {
			t.Fatalf("expected context cancellation error")
		}
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "context") {
			t.Errorf("expected context error, got: %v", err)
		}

		// Must return immediately upon context expiration, not block for full server timeout
		if elapsed > 200*time.Millisecond {
			t.Errorf("upload took too long to abort on cancelled context: %v", elapsed)
		}
	})
}

// ============================================================================
// Suite 4: Exhaustion of Retry Budgets and Graceful Error Surfacing
// ============================================================================

func TestTier5_Stress_RetryBudgetExhaustion_GracefulSurfacing(t *testing.T) {
	tempDir := t.TempDir()
	replayDir := filepath.Join(tempDir, "replays")
	dbPath := filepath.Join(tempDir, "retry_exhaustion.db")
	_ = os.MkdirAll(replayDir, 0755)

	// Create sample replay files
	createSampleFile := func(guid string) string {
		p := filepath.Join(replayDir, fmt.Sprintf("%s.replay", guid))
		_ = os.WriteFile(p, testutil.GenerateValidReplay(guid, 2048), 0644)
		return p
	}

	// ------------------------------------------------------------------------
	// Sub-test A: HTTP 429 Rate Limit Budget Exhaustion (Client Level)
	// ------------------------------------------------------------------------
	t.Run("RateLimit429_BudgetExhaustion", func(t *testing.T) {
		reqCount := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqCount++
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
		}))
		defer srv.Close()

		client, _ := ballchasing.NewClient(ballchasing.ClientConfig{
			BaseURL:     srv.URL,
			APIKey:      "token",
			Visibility:  "public",
			MaxRetries:  2, // 1 + 2 = 3 attempts total
			BaseBackoff: 2 * time.Millisecond,
		})

		filePath := createSampleFile("m-429-exhaust")
		_, err := client.UploadReplay(context.Background(), "m-429-exhaust", filePath)
		if err == nil {
			t.Fatalf("expected error from 429 exhaustion")
		}

		if !errors.Is(err, ballchasing.ErrRateLimitExhausted) {
			t.Errorf("expected ErrRateLimitExhausted, got: %v", err)
		}
		if reqCount != 3 {
			t.Errorf("expected exactly 3 attempts (1 initial + 2 retries), got %d", reqCount)
		}
	})

	// ------------------------------------------------------------------------
	// Sub-test B: HTTP 500 Server Error Budget Exhaustion (Client Level)
	// ------------------------------------------------------------------------
	t.Run("ServerError500_BudgetExhaustion", func(t *testing.T) {
		reqCount := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqCount++
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "internal error"})
		}))
		defer srv.Close()

		client, _ := ballchasing.NewClient(ballchasing.ClientConfig{
			BaseURL:     srv.URL,
			APIKey:      "token",
			Visibility:  "public",
			MaxRetries:  2,
			BaseBackoff: 2 * time.Millisecond,
		})

		filePath := createSampleFile("m-500-exhaust")
		_, err := client.UploadReplay(context.Background(), "m-500-exhaust", filePath)
		if err == nil {
			t.Fatalf("expected error from 500 exhaustion")
		}

		if !errors.Is(err, ballchasing.ErrServerError) && !strings.Contains(err.Error(), "500") {
			t.Errorf("expected ErrServerError or HTTP 500 error, got: %v", err)
		}
		if !errors.Is(err, ballchasing.ErrServerError) {
			t.Logf("[DEFECT-FINDING] ballchasing.Client: HTTP 5xx on final attempt (%v) fails to wrap ErrServerError", err)
		}
		if reqCount != 3 {
			t.Errorf("expected exactly 3 attempts, got %d", reqCount)
		}
	})

	// ------------------------------------------------------------------------
	// Sub-test C: Full Syncer Cycle with Mixed Outcomes and SQLite Persistence
	// ------------------------------------------------------------------------
	t.Run("Syncer_MixedBatch_GracefulErrorSurfacing", func(t *testing.T) {
		store, err := storage.NewSQLiteStore(dbPath)
		if err != nil {
			t.Fatalf("failed to create sqlite store: %v", err)
		}
		defer store.Close()

		cdn := testutil.NewMockCDNServer()
		defer cdn.Close()

		// Configure mock Ballchasing server with diverse response policies per match GUID
		bcHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				w.WriteHeader(http.StatusOK)
				return
			}

			// Parse multipart form to extract filename
			_ = r.ParseMultipartForm(10 << 20)
			file, header, fErr := r.FormFile("file")
			if fErr != nil {
				http.Error(w, "missing file", http.StatusBadRequest)
				return
			}
			_ = file.Close()

			guid := strings.TrimSuffix(header.Filename, ".replay")

			switch guid {
			case "match-healthy":
				// HTTP 201 Created
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Location", "https://ballchasing.com/replay/healthy-id")
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"id":       "healthy-id",
					"location": "https://ballchasing.com/replay/healthy-id",
				})

			case "match-rate-limited":
				// HTTP 429 Too Many Requests
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "rate limited"})

			case "match-server-error":
				// HTTP 503 Service Unavailable
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "service unavailable"})

			case "match-duplicate":
				// HTTP 409 Conflict (Duplicate replay)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Location", "https://ballchasing.com/replay/dup-id")
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"id":       "dup-id",
					"location": "https://ballchasing.com/replay/dup-id",
				})

			default:
				w.WriteHeader(http.StatusOK)
			}
		})

		bcSrv := httptest.NewServer(bcHandler)
		defer bcSrv.Close()

		// 4 matches in batch
		batch := []psynet.DiscoveredMatch{
			{MatchGUID: "match-healthy", ReplayURL: cdn.ReplayURL("match-healthy"), MapName: "DFHStadium_P", Playlist: 2, RecordStartTimestamp: 100},
			{MatchGUID: "match-rate-limited", ReplayURL: cdn.ReplayURL("match-rate-limited"), MapName: "DFHStadium_P", Playlist: 2, RecordStartTimestamp: 200},
			{MatchGUID: "match-server-error", ReplayURL: cdn.ReplayURL("match-server-error"), MapName: "DFHStadium_P", Playlist: 2, RecordStartTimestamp: 300},
			{MatchGUID: "match-duplicate", ReplayURL: cdn.ReplayURL("match-duplicate"), MapName: "DFHStadium_P", Playlist: 2, RecordStartTimestamp: 400},
		}

		provider := newStaticMatchProvider(batch)
		downloader := psynet.NewDownloader(psynet.WithTimeout(5 * time.Second))
		uploader, err := ballchasing.NewClient(ballchasing.ClientConfig{
			BaseURL:     bcSrv.URL,
			APIKey:      "token",
			Visibility:  "public",
			MaxRetries:  2,
			BaseBackoff: 2 * time.Millisecond,
		})
		if err != nil {
			t.Fatalf("failed to create uploader: %v", err)
		}

		engine, err := syncer.NewWithConfig(store, provider, downloader, uploader, syncer.Config{
			ReplayDir:      replayDir,
			DryRun:         false,
			KeepLocalFiles: true,
		})
		if err != nil {
			t.Fatalf("failed to create syncer: %v", err)
		}

		// RunCycle should NOT crash or fail fatally despite errors on individual items
		stats, err := engine.RunCycle(context.Background())
		if err != nil {
			t.Fatalf("syncer RunCycle failed fatally: %v", err)
		}

		// Validate cycle statistics
		if stats.DiscoveredCount != 4 {
			t.Errorf("expected 4 discovered, got %d", stats.DiscoveredCount)
		}
		if stats.DownloadedCount != 4 {
			t.Errorf("expected 4 downloaded, got %d", stats.DownloadedCount)
		}
		if stats.UploadedCount != 1 {
			t.Errorf("expected 1 uploaded, got %d", stats.UploadedCount)
		}
		if stats.DuplicateCount != 1 {
			t.Errorf("expected 1 duplicate, got %d", stats.DuplicateCount)
		}
		if stats.FailedCount != 2 {
			t.Errorf("expected 2 failed, got %d", stats.FailedCount)
		}

		// Verify persisted SQLite states
		// 1. Healthy match
		recH, err := store.GetMatch(context.Background(), "match-healthy")
		if err != nil || recH.UploadStatus != storage.UploadUploaded || recH.BallchasingID != "healthy-id" {
			t.Errorf("unexpected healthy match state: %+v, err: %v", recH, err)
		}

		// 2. Rate-limited match
		recRL, err := store.GetMatch(context.Background(), "match-rate-limited")
		if err != nil || recRL.UploadStatus != storage.UploadFailed || recRL.RetryCount != 1 {
			t.Errorf("unexpected rate-limited match state: %+v, err: %v", recRL, err)
		}
		if !strings.Contains(recRL.LastError, "429") && !strings.Contains(recRL.LastError, "rate limit") {
			t.Errorf("expected rate limit in last_error, got: %q", recRL.LastError)
		}

		// 3. Server error match
		recSE, err := store.GetMatch(context.Background(), "match-server-error")
		if err != nil || recSE.UploadStatus != storage.UploadFailed || recSE.RetryCount != 1 {
			t.Errorf("unexpected server-error match state: %+v, err: %v", recSE, err)
		}
		if !strings.Contains(recSE.LastError, "503") && !strings.Contains(recSE.LastError, "service unavailable") {
			t.Errorf("expected 503 error in last_error, got: %q", recSE.LastError)
		}

		// 4. Duplicate match
		recDup, err := store.GetMatch(context.Background(), "match-duplicate")
		if err != nil || recDup.UploadStatus != storage.UploadDuplicate || recDup.BallchasingID != "dup-id" {
			t.Errorf("unexpected duplicate match state: %+v, err: %v", recDup, err)
		}

		// --------------------------------------------------------------------
		// Second Cycle: Idempotency verification
		// --------------------------------------------------------------------
		stats2, err := engine.RunCycle(context.Background())
		if err != nil {
			t.Fatalf("second RunCycle failed: %v", err)
		}

		// The 2 successful items (healthy and duplicate) must NOT be re-downloaded or re-uploaded!
		if stats2.DownloadedCount != 0 {
			t.Errorf("expected 0 downloads on second cycle, got %d", stats2.DownloadedCount)
		}
		if stats2.UploadedCount != 0 {
			t.Errorf("expected 0 uploads on second cycle, got %d", stats2.UploadedCount)
		}

		t.Logf("Retry budget exhaustion and error surfacing test completed successfully: %+v", stats)
	})
}
