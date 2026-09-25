package daemon_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/syncer"
)

// TestChallenge_ImmediateStartupRun_HighInterval verifies that Daemon.Start executes
// the first cycle immediately without waiting for PollInterval (even if set to 24h).
func TestChallenge_ImmediateStartupRun_HighInterval(t *testing.T) {
	cfg := config.NewDefaultConfig()
	cfg.Sync.PollInterval = config.Duration(24 * time.Hour)
	cfg.Sync.Once = false

	cycleStarted := make(chan time.Time, 1)
	mock := &adversarialSyncer{
		runFunc: func(ctx context.Context) (*syncer.SyncStats, error) {
			select {
			case cycleStarted <- time.Now():
			default:
			}
			return &syncer.SyncStats{DiscoveredCount: 5}, nil
		},
	}

	d, err := daemon.New(mock, cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startTime := time.Now()
	stopped := make(chan error, 1)
	go func() {
		stopped <- d.Start(ctx)
	}()

	select {
	case t1 := <-cycleStarted:
		delay := t1.Sub(startTime)
		if delay > 500*time.Millisecond {
			t.Fatalf("startup cycle took too long to trigger: %v", delay)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for immediate startup cycle")
	}

	cancel()
	err = <-stopped
	if err != nil {
		t.Fatalf("Start returned unexpected error: %v", err)
	}
}

// TestChallenge_ImmediateStartupRun_InitialErrorContinuesInContinuousMode verifies
// that in continuous mode (Once=false), if the initial cycle fails with a transient error,
// the daemon does NOT crash or abort, but continues into the ticker loop and executes subsequent cycles.
func TestChallenge_ImmediateStartupRun_InitialErrorContinuesInContinuousMode(t *testing.T) {
	cfg := config.NewDefaultConfig()
	cfg.Sync.PollInterval = config.Duration(20 * time.Millisecond)
	cfg.Sync.Once = false

	var attempts int32
	cyclesCompleted := make(chan int, 5)

	mock := &adversarialSyncer{
		runFunc: func(ctx context.Context) (*syncer.SyncStats, error) {
			att := atomic.AddInt32(&attempts, 1)
			if att == 1 {
				return nil, errors.New("transient database locked")
			}
			cyclesCompleted <- int(att)
			return &syncer.SyncStats{DiscoveredCount: 1}, nil
		},
	}

	d, err := daemon.New(mock, cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() {
		stopped <- d.Start(ctx)
	}()

	// Wait for subsequent successful cycles from the ticker
	select {
	case c := <-cyclesCompleted:
		if c < 2 {
			t.Fatalf("expected cycle attempt >= 2, got %d", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("daemon halted after initial cycle error; failed to schedule subsequent cycles")
	}

	cancel()
	err = <-stopped
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	total := atomic.LoadInt32(&attempts)
	if total < 2 {
		t.Fatalf("expected at least 2 attempts, got %d", total)
	}
}

// TestChallenge_SingleRunOnce_ExitsZeroImmediately asserts that in --once mode,
// exactly 1 cycle executes and Start returns nil immediately without blocking on pollInterval.
func TestChallenge_SingleRunOnce_ExitsZeroImmediately(t *testing.T) {
	cfg := config.NewDefaultConfig()
	cfg.Sync.PollInterval = config.Duration(10 * time.Hour) // huge interval
	cfg.Sync.Once = true

	var execCount int32
	mock := &adversarialSyncer{
		runFunc: func(ctx context.Context) (*syncer.SyncStats, error) {
			atomic.AddInt32(&execCount, 1)
			return &syncer.SyncStats{DiscoveredCount: 3, UploadedCount: 3}, nil
		},
	}

	d, err := daemon.New(mock, cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	start := time.Now()
	err = d.Start(context.Background())
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected nil error on clean single-run exit, got: %v", err)
	}
	if elapsed > 1*time.Second {
		t.Fatalf("single-run mode blocked for too long: %v", elapsed)
	}
	if count := atomic.LoadInt32(&execCount); count != 1 {
		t.Fatalf("expected exactly 1 execution in single-run mode, got %d", count)
	}
}

// TestChallenge_SingleRunOnce_ErrorReturnsDirectly asserts that in --once mode,
// if the cycle encounters an error, Start immediately returns that error.
func TestChallenge_SingleRunOnce_ErrorReturnsDirectly(t *testing.T) {
	cfg := config.NewDefaultConfig()
	cfg.Sync.PollInterval = config.Duration(5 * time.Minute)
	cfg.Sync.Once = true

	expectedErr := errors.New("ballchasing api quota exceeded")
	var execCount int32
	mock := &adversarialSyncer{
		runFunc: func(ctx context.Context) (*syncer.SyncStats, error) {
			atomic.AddInt32(&execCount, 1)
			return nil, expectedErr
		},
	}

	d, err := daemon.New(mock, cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	err = d.Start(context.Background())
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}
	if count := atomic.LoadInt32(&execCount); count != 1 {
		t.Fatalf("expected exactly 1 execution in single-run mode, got %d", count)
	}
}

// TestChallenge_OverlappingCycleProtection_StressHighFrequencyTicks asserts that
// when sync cycles take significantly longer than PollInterval, overlapping ticks
// are safely skipped without deadlock, queue accumulation, or concurrent executions.
func TestChallenge_OverlappingCycleProtection_StressHighFrequencyTicks(t *testing.T) {
	// Rapid tick interval: 5ms
	cfg := config.NewDefaultConfig()
	cfg.Sync.PollInterval = config.Duration(5 * time.Millisecond)
	cfg.Sync.Once = false

	var concurrentRuns int32
	var maxConcurrency int32
	var totalStarted int32

	cycleHold := make(chan struct{})
	mock := &adversarialSyncer{
		runFunc: func(ctx context.Context) (*syncer.SyncStats, error) {
			curr := atomic.AddInt32(&concurrentRuns, 1)
			atomic.AddInt32(&totalStarted, 1)

			// Record maximum concurrent executions seen
			for {
				max := atomic.LoadInt32(&maxConcurrency)
				if curr <= max || atomic.CompareAndSwapInt32(&maxConcurrency, max, curr) {
					break
				}
			}

			// Block initial execution to let multiple ticks fire and attempt overlap
			if atomic.LoadInt32(&totalStarted) == 1 {
				select {
				case <-cycleHold:
				case <-ctx.Done():
				}
			}

			atomic.AddInt32(&concurrentRuns, -1)
			return &syncer.SyncStats{}, nil
		},
	}

	d, err := daemon.New(mock, cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() {
		stopped <- d.Start(ctx)
	}()

	// Wait for first cycle to be actively in flight
	for i := 0; i < 50; i++ {
		if d.IsInFlight() {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !d.IsInFlight() {
		t.Fatal("daemon never entered in-flight state")
	}

	// Sleep 60ms: with 5ms ticker, ~12 overlapping ticks will fire and must be dropped
	time.Sleep(60 * time.Millisecond)

	// Release first cycle
	close(cycleHold)

	// Allow one more cycle to execute cleanly
	time.Sleep(25 * time.Millisecond)

	cancel()
	err = <-stopped
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	// Concurrency must NEVER exceed 1
	if max := atomic.LoadInt32(&maxConcurrency); max > 1 {
		t.Fatalf("CONCURRENCY VIOLATION: max concurrency was %d, expected strictly 1", max)
	}

	// Total started cycles should be small (e.g. 2-5), not 15-20
	started := atomic.LoadInt32(&totalStarted)
	if started > 8 {
		t.Fatalf("overlapping cycles were not skipped properly; %d started", started)
	}
}

// TestChallenge_GracefulDrain_ActiveCycleFinishesDuringCancel asserts that if
// a context cancellation or SIGINT occurs while a cycle is actively running,
// Start() does NOT return prematurely, but awaits completion of the in-flight cycle.
func TestChallenge_GracefulDrain_ActiveCycleFinishesDuringCancel(t *testing.T) {
	cfg := config.NewDefaultConfig()
	cfg.Sync.PollInterval = config.Duration(1 * time.Hour)
	cfg.Sync.Once = false

	cycleStarted := make(chan struct{})
	cycleUnblock := make(chan struct{})
	cycleCompleted := make(chan struct{})

	mock := &adversarialSyncer{
		runFunc: func(ctx context.Context) (*syncer.SyncStats, error) {
			close(cycleStarted)
			<-cycleUnblock // simulate slow download/upload
			close(cycleCompleted)
			return &syncer.SyncStats{UploadedCount: 1}, nil
		},
	}

	d, err := daemon.New(mock, cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() {
		stopped <- d.Start(ctx)
	}()

	// Wait for the cycle to begin executing
	<-cycleStarted

	if !d.IsInFlight() {
		t.Fatal("expected daemon.IsInFlight() to be true")
	}

	// Cancel context while cycle is still blocked
	cancel()

	// Assert Start has NOT returned yet
	select {
	case <-stopped:
		t.Fatal("Start returned prematurely while cycle was in flight!")
	case <-time.After(50 * time.Millisecond):
		// Expected: Start is blocked waiting on wg.Wait()
	}

	// Now unblock the cycle
	close(cycleUnblock)

	// Cycle should complete, followed by Start returning nil
	select {
	case <-cycleCompleted:
	case <-time.After(1 * time.Second):
		t.Fatal("cycle failed to complete after unblock")
	}

	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Start did not return cleanly after cycle drain")
	}

	if d.IsInFlight() {
		t.Fatal("expected daemon.IsInFlight() to be false after drain")
	}
}

// TestChallenge_GracefulDrain_ContextCancelDuringInitialCycle verifies that
// if context cancellation occurs during the initial sync cycle, Start returns nil cleanly.
func TestChallenge_GracefulDrain_ContextCancelDuringInitialCycle(t *testing.T) {
	cfg := config.NewDefaultConfig()
	cfg.Sync.PollInterval = config.Duration(1 * time.Hour)
	cfg.Sync.Once = false

	mock := &adversarialSyncer{
		runFunc: func(ctx context.Context) (*syncer.SyncStats, error) {
			// Await context cancellation directly
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	d, err := daemon.New(mock, cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() {
		stopped <- d.Start(ctx)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("expected nil error on context cancellation in continuous mode, got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("daemon did not stop upon context cancellation")
	}
}

// TestChallenge_RapidContextCancelRaceWithTicker verifies that rapid context cancellations
// racing directly against ticker events always stop cleanly without deadlocks or leaked goroutines.
func TestChallenge_RapidContextCancelRaceWithTicker(t *testing.T) {
	cfg := config.NewDefaultConfig()
	cfg.Sync.PollInterval = config.Duration(2 * time.Millisecond)

	for iter := 0; iter < 10; iter++ {
		mock := &adversarialSyncer{
			runFunc: func(ctx context.Context) (*syncer.SyncStats, error) {
				time.Sleep(1 * time.Millisecond)
				return &syncer.SyncStats{}, nil
			},
		}

		d, err := daemon.New(mock, cfg)
		if err != nil {
			t.Fatalf("iter %d: daemon.New failed: %v", iter, err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(iter*3)*time.Millisecond)
		err = d.Start(ctx)
		cancel()

		if err != nil {
			t.Fatalf("iter %d: unexpected error on rapid cancel: %v", iter, err)
		}
	}
}

// TestChallenge_SequentialStartSafety verifies that a Daemon instance can be cleanly
// stopped and restarted sequentially without state corruption.
func TestChallenge_SequentialStartSafety(t *testing.T) {
	cfg := config.NewDefaultConfig()
	cfg.Sync.PollInterval = config.Duration(10 * time.Millisecond)

	var cycleCount int32
	mock := &adversarialSyncer{
		runFunc: func(ctx context.Context) (*syncer.SyncStats, error) {
			atomic.AddInt32(&cycleCount, 1)
			return &syncer.SyncStats{}, nil
		},
	}

	d, err := daemon.New(mock, cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	// First lifecycle
	ctx1, cancel1 := context.WithCancel(context.Background())
	stopped1 := make(chan error, 1)
	go func() {
		stopped1 <- d.Start(ctx1)
	}()
	time.Sleep(25 * time.Millisecond)
	cancel1()
	if err := <-stopped1; err != nil {
		t.Fatalf("first Start returned error: %v", err)
	}

	count1 := atomic.LoadInt32(&cycleCount)
	if count1 < 1 {
		t.Fatalf("expected at least 1 cycle in first run, got %d", count1)
	}

	// Second lifecycle on same instance
	ctx2, cancel2 := context.WithCancel(context.Background())
	stopped2 := make(chan error, 1)
	go func() {
		stopped2 <- d.Start(ctx2)
	}()
	time.Sleep(25 * time.Millisecond)
	cancel2()
	if err := <-stopped2; err != nil {
		t.Fatalf("second Start returned error: %v", err)
	}

	count2 := atomic.LoadInt32(&cycleCount)
	if count2 <= count1 {
		t.Fatalf("expected additional cycles in second run, got count1=%d, count2=%d", count1, count2)
	}
}

type adversarialSyncer struct {
	runFunc func(ctx context.Context) (*syncer.SyncStats, error)
}

func (s *adversarialSyncer) RunCycle(ctx context.Context) (*syncer.SyncStats, error) {
	if s.runFunc != nil {
		return s.runFunc(ctx)
	}
	return &syncer.SyncStats{}, nil
}
