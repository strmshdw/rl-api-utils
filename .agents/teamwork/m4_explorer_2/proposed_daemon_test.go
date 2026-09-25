package daemon_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/syncer"
)

// mockSyncer provides a controllable implementation of daemon.Syncer for testing.
type mockSyncer struct {
	mu          sync.Mutex
	cycleCount  int
	runFunc     func(ctx context.Context) (*syncer.SyncStats, error)
	cycleStarts chan struct{}
}

func newMockSyncer() *mockSyncer {
	return &mockSyncer{
		cycleStarts: make(chan struct{}, 100),
	}
}

func (m *mockSyncer) RunCycle(ctx context.Context) (*syncer.SyncStats, error) {
	m.mu.Lock()
	m.cycleCount++
	fn := m.runFunc
	m.mu.Unlock()

	select {
	case m.cycleStarts <- struct{}{}:
	default:
	}

	if fn != nil {
		return fn(ctx)
	}
	return &syncer.SyncStats{DiscoveredCount: 1, UploadedCount: 1}, nil
}

func (m *mockSyncer) getCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cycleCount
}

func (m *mockSyncer) setRunFunc(fn func(ctx context.Context) (*syncer.SyncStats, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runFunc = fn
}

func makeTestConfig(interval time.Duration, once bool) *config.Config {
	cfg := config.NewDefaultConfig()
	cfg.Sync.PollInterval = config.Duration(interval)
	cfg.Sync.Once = once
	cfg.Logging.Level = "debug"
	cfg.Logging.Format = "text"
	return cfg
}

func TestNew_Validation(t *testing.T) {
	cfg := makeTestConfig(5*time.Minute, false)
	mock := newMockSyncer()

	// Nil syncer
	if _, err := daemon.New(nil, cfg); err == nil {
		t.Fatal("expected error for nil syncer")
	}

	// Nil config
	if _, err := daemon.New(mock, nil); err == nil {
		t.Fatal("expected error for nil config")
	}

	// Valid New
	d, err := daemon.New(mock, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d == nil {
		t.Fatal("expected non-nil daemon")
	}
}

func TestDaemon_ImmediateInitialRun(t *testing.T) {
	// Interval is 1 hour; immediate run must happen right away
	cfg := makeTestConfig(1*time.Hour, false)
	mock := newMockSyncer()

	buf := &bytes.Buffer{}
	logger := daemon.NewLogger(cfg.Logging, buf)

	d, err := daemon.New(mock, cfg, daemon.WithLogger(logger))
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)

	go func() {
		stopped <- d.Start(ctx)
	}()

	// Wait for the immediate cycle to trigger
	select {
	case <-mock.cycleStarts:
		// success: immediate cycle was triggered
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for immediate cycle")
	}

	// Cancel context to shut down cleanly
	cancel()

	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("daemon did not stop on context cancellation")
	}

	if count := mock.getCount(); count != 1 {
		t.Fatalf("expected exactly 1 cycle, got %d", count)
	}
}

func TestDaemon_SingleRunOnce_Success(t *testing.T) {
	cfg := makeTestConfig(1*time.Hour, true) // Once = true
	mock := newMockSyncer()

	d, err := daemon.New(mock, cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	// Start without canceling context; should exit automatically after 1 cycle
	err = d.Start(context.Background())
	if err != nil {
		t.Fatalf("expected nil error in once mode, got: %v", err)
	}

	if count := mock.getCount(); count != 1 {
		t.Fatalf("expected exactly 1 cycle, got %d", count)
	}
}

func TestDaemon_SingleRunOnce_Error(t *testing.T) {
	cfg := makeTestConfig(1*time.Hour, true) // Once = true
	mock := newMockSyncer()
	expectedErr := errors.New("auth token expired")
	mock.setRunFunc(func(ctx context.Context) (*syncer.SyncStats, error) {
		return nil, expectedErr
	})

	d, err := daemon.New(mock, cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	err = d.Start(context.Background())
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}

	if count := mock.getCount(); count != 1 {
		t.Fatalf("expected exactly 1 cycle, got %d", count)
	}
}

func TestDaemon_TickerTriggering(t *testing.T) {
	// Rapid interval: 15ms
	cfg := makeTestConfig(15*time.Millisecond, false)
	mock := newMockSyncer()

	d, err := daemon.New(mock, cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)

	go func() {
		stopped <- d.Start(ctx)
	}()

	// Wait for at least 3 cycle triggers
	for i := 0; i < 3; i++ {
		select {
		case <-mock.cycleStarts:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for cycle %d", i+1)
		}
	}

	cancel()

	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("daemon did not stop after cancel")
	}

	if count := mock.getCount(); count < 3 {
		t.Fatalf("expected at least 3 cycles, got %d", count)
	}
}

func TestDaemon_ContextCancellationStopsLoop(t *testing.T) {
	cfg := makeTestConfig(1*time.Hour, false)
	mock := newMockSyncer()

	d, err := daemon.New(mock, cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)

	go func() {
		stopped <- d.Start(ctx)
	}()

	// Wait for immediate cycle to complete
	<-mock.cycleStarts
	time.Sleep(20 * time.Millisecond) // ensure loop is waiting on ticker

	cancel()

	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("expected clean exit (nil error), got: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("daemon did not stop on context cancellation")
	}
}

func TestDaemon_GracefulDrainAwaitsInFlight(t *testing.T) {
	cfg := makeTestConfig(1*time.Hour, false)
	mock := newMockSyncer()

	cycleBlock := make(chan struct{})
	cycleFinished := make(chan struct{})

	mock.setRunFunc(func(ctx context.Context) (*syncer.SyncStats, error) {
		<-cycleBlock // simulate long-running in-flight sync operation
		close(cycleFinished)
		return &syncer.SyncStats{}, nil
	})

	d, err := daemon.New(mock, cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)

	go func() {
		stopped <- d.Start(ctx)
	}()

	// Wait for cycle to start
	<-mock.cycleStarts

	// Assert daemon reports in-flight status
	if !d.IsInFlight() {
		t.Fatal("expected IsInFlight to be true during active cycle")
	}

	// Cancel context while cycle is blocked in flight
	cancel()

	// Ensure Start has not exited yet (still awaiting in-flight drain)
	select {
	case <-stopped:
		t.Fatal("daemon returned before in-flight cycle completed drain")
	case <-time.After(50 * time.Millisecond):
		// Expected: Start is still waiting
	}

	// Release in-flight cycle
	close(cycleBlock)

	// Await cycle completion and clean daemon exit
	<-cycleFinished

	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for daemon to stop after drain")
	}

	if d.IsInFlight() {
		t.Fatal("expected IsInFlight to be false after drain complete")
	}
}

func TestDaemon_OverlappingCycleSkipped(t *testing.T) {
	// Rapid ticker interval
	cfg := makeTestConfig(10*time.Millisecond, false)
	mock := newMockSyncer()

	blockCh := make(chan struct{})
	mock.setRunFunc(func(ctx context.Context) (*syncer.SyncStats, error) {
		<-blockCh
		return &syncer.SyncStats{}, nil
	})

	d, err := daemon.New(mock, cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)

	go func() {
		stopped <- d.Start(ctx)
	}()

	// Wait for initial cycle to start and block
	<-mock.cycleStarts

	// Sleep 50ms while cycle is blocked — multiple ticker ticks will fire and be skipped
	time.Sleep(50 * time.Millisecond)

	// Unblock cycle
	close(blockCh)
	time.Sleep(20 * time.Millisecond)

	cancel()
	<-stopped

	// Count should be strictly 1 or at most 2, never dozens of concurrent cycles
	if count := mock.getCount(); count > 3 {
		t.Fatalf("overlapping cycles were not skipped, count: %d", count)
	}
}

func TestDaemon_CycleError_ContinuousModeContinues(t *testing.T) {
	cfg := makeTestConfig(15*time.Millisecond, false)
	mock := newMockSyncer()

	first := true
	mock.setRunFunc(func(ctx context.Context) (*syncer.SyncStats, error) {
		if first {
			first = false
			return nil, errors.New("transient network error")
		}
		return &syncer.SyncStats{DiscoveredCount: 2}, nil
	})

	d, err := daemon.New(mock, cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)

	go func() {
		stopped <- d.Start(ctx)
	}()

	// Wait for first cycle (which fails) and second cycle (which succeeds)
	<-mock.cycleStarts
	<-mock.cycleStarts

	cancel()
	err = <-stopped
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	if count := mock.getCount(); count < 2 {
		t.Fatalf("expected at least 2 cycles executed despite error, got %d", count)
	}
}

func TestNewLogger_LevelsAndFormats(t *testing.T) {
	tests := []struct {
		name       string
		level      string
		format     string
		logMsg     string
		shouldFind string
	}{
		{
			name:       "Text Info",
			level:      "info",
			format:     "text",
			logMsg:     "hello text info",
			shouldFind: "hello text info",
		},
		{
			name:       "JSON Error",
			level:      "error",
			format:     "json",
			logMsg:     "critical error occurred",
			shouldFind: `"msg":"critical error occurred"`,
		},
		{
			name:       "Debug Level Filtering",
			level:      "warn",
			format:     "text",
			logMsg:     "ignored debug",
			shouldFind: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			cfg := config.LoggingConfig{Level: tc.level, Format: tc.format}
			logger := daemon.NewLogger(cfg, buf)

			if tc.level == "warn" {
				logger.Debug(tc.logMsg)
			} else if tc.level == "error" {
				logger.Error(tc.logMsg)
			} else {
				logger.Info(tc.logMsg)
			}

			out := buf.String()
			if tc.shouldFind != "" {
				if !strings.Contains(out, tc.shouldFind) {
					t.Fatalf("expected log output to contain %q, got: %s", tc.shouldFind, out)
				}
			} else {
				if strings.Contains(out, tc.logMsg) {
					t.Fatalf("expected log %q to be filtered out at level %s, got: %s", tc.logMsg, tc.level, out)
				}
			}

			if tc.format == "json" && tc.shouldFind != "" {
				var parsed map[string]any
				if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
					t.Fatalf("failed to unmarshal JSON log: %v", err)
				}
			}
		})
	}
}
