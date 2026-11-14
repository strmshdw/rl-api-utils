package e2e

import (
	"context"
	"errors"
	"fmt"
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
