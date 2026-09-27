package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/psynet"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/syncer"
	"github.com/dank/rl-api-utils/internal/testutil"
	"github.com/dank/rl-api-utils/internal/web"
)

// staticMatchProvider provides a fixed slice of matches for syncer testing.
type staticMatchProvider struct {
	mu      sync.Mutex
	matches []psynet.DiscoveredMatch
	calls   int
}

func newStaticMatchProvider(matches []psynet.DiscoveredMatch) *staticMatchProvider {
	return &staticMatchProvider{matches: matches}
}

func (p *staticMatchProvider) GetRecentMatches(ctx context.Context) ([]psynet.DiscoveredMatch, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	res := make([]psynet.DiscoveredMatch, len(p.matches))
	copy(res, p.matches)
	return res, nil
}

func (p *staticMatchProvider) Close() error {
	return nil
}

// ============================================================================
// Suite 1: High Concurrency Stress — Multiple Concurrent Syncers on Shared SQLite DB
// ============================================================================

func TestTier5_Stress_ConcurrentSyncers_SharedSQLite(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "shared_concurrent.db")
	replayDir := filepath.Join(tempDir, "replays")
	if err := os.MkdirAll(replayDir, 0755); err != nil {
		t.Fatalf("failed to create replay dir: %v", err)
	}

	// 1. Initialize SQLite schema once
	initStore, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to initialize sqlite store: %v", err)
	}
	if err := initStore.Close(); err != nil {
		t.Fatalf("failed to close init store: %v", err)
	}

	// 2. Setup Mock CDN and Ballchasing servers
	cdn := testutil.NewMockCDNServer()
	defer cdn.Close()

	bc := testutil.NewMockBallchasingServer()
	defer bc.Close()

	// 3. Prepare 24 matches across the system
	const matchCount = 24
	matches := make([]psynet.DiscoveredMatch, matchCount)
	for i := 0; i < matchCount; i++ {
		guid := fmt.Sprintf("stress-match-%03d", i+1)
		cdnURL := cdn.ReplayURL(guid)
		cdn.SetReplayPayload(guid, testutil.GenerateValidReplay(guid, 2048))
		matches[i] = psynet.DiscoveredMatch{
			MatchGUID:            guid,
			RecordStartTimestamp: time.Now().Unix() - int64((matchCount-i)*60),
			MapName:              "DFHStadium_P",
			Playlist:             2,
			ReplayURL:            cdnURL,
		}
	}

	// 4. Spin up 6 concurrent syncer instances, each having its OWN *storage.SQLiteStore
	// connection to the shared on-disk database file.
	const workerCount = 6
	type workerInstance struct {
		id     int
		store  *storage.SQLiteStore
		engine *syncer.Syncer
	}

	workers := make([]*workerInstance, workerCount)
	for i := 0; i < workerCount; i++ {
		store, err := storage.NewSQLiteStore(dbPath)
		if err != nil {
			t.Fatalf("worker %d failed to open sqlite store: %v", i, err)
		}
		defer store.Close()

		provider := newStaticMatchProvider(matches)
		downloader := psynet.NewDownloader(psynet.WithTimeout(10 * time.Second))
		uploader, err := ballchasing.NewClient(ballchasing.ClientConfig{
			BaseURL:     bc.URL(),
			APIKey:      "test-ballchasing-token",
			Visibility:  "public",
			MaxRetries:  3,
			BaseBackoff: 10 * time.Millisecond,
			Timeout:     10 * time.Second,
		})
		if err != nil {
			t.Fatalf("worker %d failed to create ballchasing client: %v", i, err)
		}

		engine, err := syncer.NewWithConfig(store, provider, downloader, uploader, syncer.Config{
			ReplayDir:      replayDir,
			DryRun:         false,
			KeepLocalFiles: true,
		})
		if err != nil {
			t.Fatalf("worker %d failed to create syncer engine: %v", i, err)
		}

		workers[i] = &workerInstance{
			id:     i,
			store:  store,
			engine: engine,
		}
	}

	// Background contention generator: runs RecoverInFlight and ListPendingDownloads concurrently
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	bgStore, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to open background sqlite store: %v", err)
	}
	defer bgStore.Close()

	stopBg := make(chan struct{})
	bgErrCh := make(chan error, 1)
	go func() {
		defer close(bgErrCh)
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopBg:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := bgStore.RecoverInFlight(ctx); err != nil && !errors.Is(err, context.Canceled) {
					// Check if lock error was handled or surfaced
					if !strings.Contains(err.Error(), "locked") && !strings.Contains(err.Error(), "busy") {
						bgErrCh <- fmt.Errorf("unexpected background recovery error: %w", err)
						return
					}
				}
				_, _ = bgStore.ListPendingDownloads(ctx)
			}
		}
	}()

	// 5. Synchronized Start Barrier for all workers
	var startWg sync.WaitGroup
	startWg.Add(1)

	var doneWg sync.WaitGroup
	workerErrors := make([]error, workerCount)

	for i := 0; i < workerCount; i++ {
		doneWg.Add(1)
		go func(idx int) {
			defer doneWg.Done()
			startWg.Wait() // align execution start

			// Each worker runs 2 full sync cycles under concurrent load
			for cycle := 0; cycle < 2; cycle++ {
				_, err := workers[idx].engine.RunCycle(ctx)
				if err != nil {
					workerErrors[idx] = fmt.Errorf("worker %d cycle %d failed: %w", idx, cycle, err)
					return
				}
			}
		}(i)
	}

	// Trigger simultaneous execution
	startWg.Done()
	doneWg.Wait()
	close(stopBg)

	// Check background errors
	if bgErr := <-bgErrCh; bgErr != nil {
		t.Fatalf("background contention error: %v", bgErr)
	}

	// Check worker errors
	for i, wErr := range workerErrors {
		if wErr != nil {
			t.Fatalf("worker error on concurrent DB access: %v (worker %d)", wErr, i)
		}
	}

	// 6. Post-Run Database Integrity & Completeness Verification
	verifyStore, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to open verify store: %v", err)
	}
	defer verifyStore.Close()

	// Ensure all in-flight items are settled
	if err := verifyStore.RecoverInFlight(ctx); err != nil {
		t.Fatalf("post-run recovery failed: %v", err)
	}

	// Verify all 24 matches exist and reached terminal success state
	for i := 0; i < matchCount; i++ {
		guid := fmt.Sprintf("stress-match-%03d", i+1)
		rec, err := verifyStore.GetMatch(ctx, guid)
		if err != nil {
			t.Fatalf("missing match record %s after concurrent execution: %v", guid, err)
		}

		if rec.DownloadStatus != storage.DownloadDownloaded {
			t.Errorf("match %s expected DownloadDownloaded, got %s", guid, rec.DownloadStatus)
		}
		if rec.UploadStatus != storage.UploadUploaded && rec.UploadStatus != storage.UploadDuplicate {
			t.Errorf("match %s expected UploadUploaded or UploadDuplicate, got %s", guid, rec.UploadStatus)
		}
		if rec.BallchasingID == "" {
			t.Errorf("match %s has empty BallchasingID", guid)
		}
	}

	// Verify no pending items remain in the database
	pendingDl, err := verifyStore.ListPendingDownloads(ctx)
	if err != nil {
		t.Fatalf("failed to list pending downloads: %v", err)
	}
	if len(pendingDl) != 0 {
		t.Errorf("expected 0 pending downloads remaining, got %d", len(pendingDl))
	}

	pendingUp, err := verifyStore.ListPendingUploads(ctx)
	if err != nil {
		t.Fatalf("failed to list pending uploads: %v", err)
	}
	if len(pendingUp) != 0 {
		t.Errorf("expected 0 pending uploads remaining, got %d", len(pendingUp))
	}

	t.Logf("Concurrent DB stress completed successfully: %d matches fully processed by %d workers without deadlocks or corruptions", matchCount, workerCount)
}

// ============================================================================
// Suite 2: Rapid Start/Stop Cycles of Daemon Without Leaks or Deadlocks
// ============================================================================

type mockStressSyncer struct {
	cycleCount int64
	sleepDur   time.Duration
}

func (m *mockStressSyncer) RunCycle(ctx context.Context) (*syncer.SyncStats, error) {
	atomic.AddInt64(&m.cycleCount, 1)
	if m.sleepDur > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(m.sleepDur):
		}
	}
	return &syncer.SyncStats{DiscoveredCount: 1}, nil
}

func TestTier5_Stress_Daemon_RapidStartStopCycles(t *testing.T) {
	// Baseline goroutine count
	runtime.GC()
	baselineGoroutines := runtime.NumGoroutine()

	syncerMock := &mockStressSyncer{sleepDur: 2 * time.Millisecond}
	cfg := config.NewDefaultConfig()
	cfg.Sync.PollInterval = config.Duration(10 * time.Millisecond)
	cfg.Sync.Once = false

	const iterations = 50
	t.Logf("Running %d rapid start/stop cycles of daemon engine...", iterations)

	for i := 0; i < iterations; i++ {
		d, err := daemon.New(syncerMock, cfg)
		if err != nil {
			t.Fatalf("iteration %d: failed to create daemon: %v", i, err)
		}

		// Create contexts with varied cancellation timings
		var ctx context.Context
		var cancel context.CancelFunc

		switch i % 5 {
		case 0:
			// Pre-cancelled context
			ctx, cancel = context.WithCancel(context.Background())
			cancel()
		case 1:
			// Cancelled immediately after 1ms
			ctx, cancel = context.WithTimeout(context.Background(), 1*time.Millisecond)
			defer cancel()
		case 2:
			// Cancelled during tick wait (8ms)
			ctx, cancel = context.WithTimeout(context.Background(), 8*time.Millisecond)
			defer cancel()
		case 3:
			// Cancelled mid-cycle after 15ms
			ctx, cancel = context.WithTimeout(context.Background(), 15*time.Millisecond)
			defer cancel()
		case 4:
			// Single-run mode with clean exit
			onceCfg := *cfg
			onceCfg.Sync.Once = true
			dOnce, _ := daemon.New(syncerMock, &onceCfg)
			startErr := dOnce.Start(context.Background())
			if startErr != nil {
				t.Fatalf("iteration %d: Once mode failed: %v", i, startErr)
			}
			continue
		}

		doneCh := make(chan error, 1)
		go func() {
			doneCh <- d.Start(ctx)
		}()

		select {
		case err := <-doneCh:
			if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("iteration %d: unexpected daemon error: %v", i, err)
			}
		case <-time.After(1 * time.Second):
			t.Fatalf("iteration %d: DEADLOCK DETECTED! Daemon failed to stop within 1s", i)
		}

		// In-flight state must be cleanly cleared
		if d.IsInFlight() {
			t.Fatalf("iteration %d: daemon inFlight state remained true after shutdown", i)
		}
	}

	// Goroutine leak audit: allow up to 100ms for system runtimes to settle
	time.Sleep(100 * time.Millisecond)
	runtime.GC()
	finalGoroutines := runtime.NumGoroutine()
	leakDelta := finalGoroutines - baselineGoroutines

	if leakDelta > 5 {
		t.Fatalf("GOROUTINE LEAK DETECTED: baseline=%d, final=%d (delta=%d leaked goroutines)",
			baselineGoroutines, finalGoroutines, leakDelta)
	}

	t.Logf("Rapid start/stop test passed: %d iterations completed cleanly, final goroutines=%d (delta=%d)",
		iterations, finalGoroutines, leakDelta)
}

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

// ============================================================================
// Suite 5: Raw Socket Adversarial Security & Static Boundary Penetration
// ============================================================================

// TestTier5_Adversarial_RawSocketPathTraversalAndBoundary bypasses client-side URL
// normalization by transmitting un-sanitized raw HTTP/1.1 bytes across direct TCP
// sockets to probe daemon static handlers and path traversal guards.
func TestTier5_Adversarial_RawSocketPathTraversalAndBoundary(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state_raw_sec.db")

	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to init SQLiteStore: %v", err)
	}
	defer store.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		Web: config.WebConfig{Enabled: true, Host: "127.0.0.1", Port: 0},
		PlayerTracking: config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|76561198000000001|0",
		},
	}

	d, err := daemon.New(&testSyncerStub{}, cfg,
		daemon.WithStateStore(store),
		daemon.WithWebHandler(web.DistHandler()),
		daemon.WithLogger(logger),
	)
	if err != nil {
		t.Fatalf("failed to create daemon: %v", err)
	}

	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	serverAddr := server.Listener.Addr().String()

	// Helper to send raw HTTP/1.1 request bytes over raw TCP connection
	sendRawRequest := func(method, rawPath string) (*http.Response, string, error) {
		conn, dialErr := net.DialTimeout("tcp", serverAddr, 2*time.Second)
		if dialErr != nil {
			return nil, "", fmt.Errorf("tcp dial failed: %w", dialErr)
		}
		defer conn.Close()

		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

		rawReq := fmt.Sprintf("%s %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", method, rawPath, serverAddr)
		if _, writeErr := conn.Write([]byte(rawReq)); writeErr != nil {
			return nil, "", fmt.Errorf("conn write failed: %w", writeErr)
		}

		reader := bufio.NewReader(conn)
		resp, readErr := http.ReadResponse(reader, nil)
		if readErr != nil {
			return nil, "", fmt.Errorf("read response failed: %w", readErr)
		}
		defer resp.Body.Close()

		bodyBytes, _ := io.ReadAll(resp.Body)
		return resp, string(bodyBytes), nil
	}

	// 1. Mandatory 14 Path Traversal Variants via Raw Sockets
	traversalVectors := []string{
		"//dist/..",
		"/..",
		"/../../etc/passwd",
		"/..%2f..%2fetc/passwd",
		"/%2e%2e/%2e%2e/windows/win.ini",
		"/%2e%2e",
		"/assets/../index.html",
		"/assets/%2e%2e/dist/index.html",
		"/assets/..%2findex.html",
		"/api/../index.html",
		"/api/%2e%2e/index.html",
		"/\\..\\windows\\win.ini",
		"/..\\..\\windows\\system.ini",
		"/..%5c..%5cwindows%5cwin.ini",
	}

	for _, vec := range traversalVectors {
		t.Run("RawSocket_Traversal_"+vec, func(t *testing.T) {
			resp, body, err := sendRawRequest(http.MethodGet, vec)
			if err != nil {
				t.Fatalf("raw request failed for %q: %v", vec, err)
			}

			// Must be rejected with 400 or 404
			if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusNotFound {
				t.Errorf("vector %q returned status %d, expected 400 or 404", vec, resp.StatusCode)
			}

			// Traversal must NEVER return SPA index.html or leak system files
			if strings.Contains(body, "<div id=\"root\">") || strings.Contains(body, "rl-sync-web") {
				t.Errorf("vector %q leaked SPA index.html via raw TCP socket!", vec)
			}
			if strings.Contains(body, "[extensions]") || strings.Contains(body, "root:") || strings.Contains(body, "[boot loader]") {
				t.Errorf("vector %q leaked host filesystem contents via raw TCP socket!", vec)
			}
		})
	}

	// 1b. Extended Path Traversal Vectors
	extendedVectors := []string{
		"//..//windows//win.ini",
		"/./../../etc/shadow",
		"/%2e%2e%2f",
		"/assets/..",
		"/..;",
	}

	for _, vec := range extendedVectors {
		t.Run("RawSocket_ExtendedTraversal_"+vec, func(t *testing.T) {
			resp, body, err := sendRawRequest(http.MethodGet, vec)
			if err != nil {
				t.Fatalf("raw request failed for %q: %v", vec, err)
			}

			// Extended traversals must be rejected with 400 or 404
			if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusNotFound {
				t.Errorf("vector %q returned status %d, expected 400 or 404", vec, resp.StatusCode)
			}

			if strings.Contains(body, "<div id=\"root\">") || strings.Contains(body, "rl-sync-web") {
				t.Errorf("vector %q leaked SPA index.html!", vec)
			}
			if strings.Contains(body, "[extensions]") || strings.Contains(body, "root:") {
				t.Errorf("vector %q leaked host filesystem contents!", vec)
			}
		})
	}

	// 1c. Double-Encoding Exploratory Probe: /%252e%252e/
	// Demonstrates that even though double-URL-encoded %252e decodes to %2e, it never traverses
	// out of the sandbox or leaks host filesystem files.
	t.Run("RawSocket_DoubleEncoding_Probe", func(t *testing.T) {
		_, body, err := sendRawRequest(http.MethodGet, "/%252e%252e/")
		if err != nil {
			t.Fatalf("raw request failed: %v", err)
		}
		if strings.Contains(body, "[extensions]") || strings.Contains(body, "root:") || strings.Contains(body, "[boot loader]") {
			t.Errorf("double-encoded traversal leaked host files!")
		}
	})

	// 2. Strict /api Guard: Raw requests to /api and /api/* must NEVER fall back to index.html
	apiVectors := []string{
		"/api",
		"/api/",
		"/api/unknown",
		"/api/unknown/nested",
		"/api/v1/invalid",
		"/api/session/extra",
	}

	for _, vec := range apiVectors {
		t.Run("RawSocket_APIGuard_"+vec, func(t *testing.T) {
			resp, body, err := sendRawRequest(http.MethodGet, vec)
			if err != nil {
				t.Fatalf("raw request failed for %q: %v", vec, err)
			}

			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("API guard vector %q returned status %d, expected 404", vec, resp.StatusCode)
			}
			if strings.Contains(body, "<div id=\"root\">") || strings.Contains(body, "<html") {
				t.Errorf("API guard vector %q erroneously fell back to index.html!", vec)
			}
		})
	}

	// 3. Static HTTP Method Boundary Enforcement
	methodVectors := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/index.html"},
		{http.MethodPut, "/index.html"},
		{http.MethodDelete, "/session"},
		{http.MethodPatch, "/"},
	}

	for _, mv := range methodVectors {
		t.Run("RawSocket_Method_"+mv.method+"_"+mv.path, func(t *testing.T) {
			resp, _, err := sendRawRequest(mv.method, mv.path)
			if err != nil {
				t.Fatalf("raw request failed for %s %s: %v", mv.method, mv.path, err)
			}

			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf("%s %s returned status %d, expected 405 Method Not Allowed", mv.method, mv.path, resp.StatusCode)
			}
		})
	}

	// 4. Valid SPA Route Fallbacks over Raw Socket
	validSPAVectors := []string{
		"/",
		"/session",
		"/dashboard",
		"/overlay",
	}

	for _, vec := range validSPAVectors {
		t.Run("RawSocket_ValidSPA_"+vec, func(t *testing.T) {
			resp, body, err := sendRawRequest(http.MethodGet, vec)
			if err != nil {
				t.Fatalf("raw request failed for %q: %v", vec, err)
			}

			if resp.StatusCode != http.StatusOK {
				t.Errorf("valid SPA vector %q returned status %d, expected 200 OK", vec, resp.StatusCode)
			}
			if !strings.Contains(body, "<html") && !strings.Contains(body, "<!doctype html>") {
				t.Errorf("valid SPA vector %q did not return HTML content", vec)
			}
			ct := resp.Header.Get("Content-Type")
			if !strings.Contains(ct, "text/html") {
				t.Errorf("valid SPA vector %q had Content-Type %q, expected text/html", vec, ct)
			}
		})
	}
}

