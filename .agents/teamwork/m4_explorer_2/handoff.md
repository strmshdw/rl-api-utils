# Handoff Report: Daemon Lifecycle Subsystem (internal/daemon)

**Agent**: `m4_explorer_2`  
**Milestone**: M4 - Syncer, Daemon Engine & CLI  
**Scope**: Daemon lifecycle engine, immediate startup execution, ticker loop, single-run mode, signal trapping & graceful drain, structured logging (`log/slog`), and unit test suite.  
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m4_explorer_2`  

---

## 1. Observation

### 1.1 Requirements & Architectural Contracts
- **Original User Request (`ORIGINAL_REQUEST.md:5-7, 29-30`)**:
  - *"An automated Rocket League daemon written in Go that interfaces with Rocket League's internal PsyNet API via github.com/dank/rlapi to poll match history every 5 minutes, download new .replay files, and automatically upload them to ballchasing.com."*
  - *"Configuration: Configurable via environment variables or configuration file for authentication provider choice, credentials/tokens, Ballchasing API key, upload visibility, polling interval, and local replay directory path."*
  - *"Logging: Structured logging with timestamps and descriptive error reporting."*
  - *"A CLI single-run / dry-run flag is provided to allow executing one sync cycle on demand."* (`ORIGINAL_REQUEST.md:61`)
- **System Architecture (`PROJECT.md:10-21`)**:
  ```
  [cmd/rl-sync] (CLI: flags, signals, entry point)
         │
         ▼
  [internal/daemon] (Lifecycle: 5m ticker, immediate startup run, OS signal trap, graceful drain)
         │
         ▼
  [internal/syncer] (Domain Orchestrator: poll -> diff -> download -> upload -> persist)
  ```
- **Feature Inventory (`PROJECT.md:47-49`)**:
  - **Feature 15**: *Daemon Engine & Lifecycle: Ticker loop (default 5m), immediate initial run, OS signal trapping (SIGINT/SIGTERM), graceful drain*
  - **Feature 16**: *CLI Interface & Modes: Flags `--config`, `--once` (single-run), `--dry-run` (simulation mode), `--log-level`, `--log-format`*
  - **Feature 17**: *Structured Logging: JSON and text structured logging via Go `log/slog` with timestamps and contextual attributes*
- **Existing Config Contract (`internal/config/config.go:116-130, 158-169`)**:
  - `SyncConfig` defines `PollInterval Duration` (default 5m), `DryRun bool` (default false), and `Once bool` (default false).
  - `LoggingConfig` defines `Level string` ("debug", "info", "warn", "error") and `Format string` ("text", "json").
  - `NewDefaultConfig()` defaults: `PollInterval: 5m`, `Once: false`, `DryRun: false`, `Logging.Level: "info"`, `Logging.Format: "text"`.
- **E2E Test Specifications (`test/e2e/tier1_feature_test.go:1257-1335, 1410-1460`)**:
  - `TestTier1_F15_Daemon_ImmediateInitialRun`: verifies immediate cycle run on startup without delay.
  - `TestTier1_F15_Daemon_TickerTriggering`: verifies recurring ticker triggers.
  - `TestTier1_F15_Daemon_ContextCancellationStopsLoop`: verifies loop stops promptly on `ctx.Done()`.
  - `TestTier1_F15_Daemon_GracefulDrainAwaitsInFlight`: verifies in-flight work completes before drain exit.
  - `TestTier1_F16_CLI_OnceModeSinglePass`: verifies single cycle execution in `--once` mode.
  - `TestTier1_F16_CLI_LogLevelAndFormatFlags` & `TestTier1_F17_Log_*`: verifies level parsing and text/json output.

---

## 2. Logic Chain

1. **Clean Architecture Boundary**:
   - `internal/daemon` acts as the execution coordinator between the operating system / CLI (`cmd/rl-sync`) and the domain engine (`internal/syncer`).
   - Coupling `Daemon` directly to concrete `*syncer.Syncer` makes unit testing clumsy and dependent on storage/network implementations.
   - Therefore, `Daemon` accepts an interface `Syncer` defining `RunCycle(ctx context.Context) (*syncer.SyncStats, error)`. Any type implementing this contract can be driven by `Daemon`.

2. **Immediate Startup Cycle**:
   - Daemons polling at long intervals (e.g. 5 minutes) must not force users to wait 5 minutes before the first sync occurs.
   - `d.Start(ctx)` invokes `d.executeCycle(ctx)` synchronously before setting up `time.NewTicker`.
   - If `cfg.Sync.Once == true`, `Start(ctx)` returns immediately after this first cycle. If the cycle succeeds, it returns `nil`. If it fails, it returns the error so CLI `--once` can exit with a non-zero exit code.
   - In continuous daemon mode (`cfg.Sync.Once == false`), if the initial cycle encounters an error (e.g. temporary network glitch at system boot), the daemon logs an error and proceeds to the ticker loop rather than abruptly crashing.

3. **Ticker Loop Execution & Overlap Prevention**:
   - A `time.Ticker` runs at `cfg.Sync.PollInterval` (defaulting to 5 minutes if unconfigured).
   - If a synchronization cycle takes longer than `PollInterval` (e.g. large file downloads on a slow link), overlapping concurrent runs on the same SQLite state store could cause write contention or race conditions.
   - The daemon maintains `inFlight bool` protected by `sync.Mutex`. When the ticker fires, if `inFlight` is true, the daemon emits a structured warning (`"previous sync cycle still in progress; skipping tick"`) and skips the tick.

4. **Graceful Drain & Signal Trapping**:
   - `Start(ctx)` wraps incoming `ctx` with `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)`. This cleanly handles both external context cancellation (e.g. in tests) and OS signals (`SIGINT`, `SIGTERM`).
   - Each active cycle increments `d.wg.Add(1)` and decrements `d.wg.Done()` in a `defer` block.
   - When `<-ctx.Done()` triggers, `Start` logs the shutdown event and invokes `d.wg.Wait()`.
   - Because cycles are tracked via `d.wg`, `Start(ctx)` will **never** return while a cycle is still running. Deferred cleanups in `main.go` (such as `store.Close()`) are guaranteed to execute only after all in-flight sync work has fully drained.

5. **Structured Logging Integration (`log/slog`)**:
   - Helper `NewLogger(cfg config.LoggingConfig, w ...io.Writer) *slog.Logger` handles level parsing (`debug`, `info`, `warn`, `error`) and format dispatch (`text`, `json`).
   - Logging throughout the daemon lifecycle records:
     - Daemon startup parameters: `poll_interval`, `once`, `dry_run`.
     - Cycle startup and execution duration (`duration`, `duration_ms`).
     - Cycle completion statistics (`discovered`, `downloaded`, `uploaded`, `duplicates`, `skipped`, `failed`).
     - Overlapping cycle skips and graceful drain progress.

---

## 3. Caveats

- **Syncer Dependency Ordering**: `internal/syncer` and `internal/daemon` are both in M4. `daemon.go` imports `github.com/dank/rl-api-utils/internal/syncer` for `syncer.SyncStats`. If `internal/syncer` is compiled in parallel, both workers must adhere to the `SyncStats` definition documented in `PROJECT.md:46` and `test/e2e/e2e_test.go:550`.
- **Drain Timeout**: In the proposed design, in-flight cycles drain until completion or until the underlying HTTP context aborts. If PsyNet or Ballchasing hangs indefinitely, the client-level timeouts configured in `cfg.Sync.DownloadTimeout` and `cfg.Ballchasing.Timeout` bound individual HTTP operations.
- **Windows Signal Handling**: On Windows, `os.Interrupt` is fully supported; `syscall.SIGTERM` is handled by Go runtime signal emulation. This design is cross-platform.

---

## 4. Conclusion & Proposed Code

The daemon lifecycle subsystem is fully specified with clear boundaries, concurrency safety, clean drain semantics, and structured logging.

### Proposed File: `internal/daemon/daemon.go`
*(Also written to `.agents/teamwork/m4_explorer_2/proposed_daemon.go`)*

```go
package daemon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/syncer"
)

// Syncer represents the domain orchestrator interface required by the daemon.
// It decouples the lifecycle daemon from concrete syncer implementations.
type Syncer interface {
	RunCycle(ctx context.Context) (*syncer.SyncStats, error)
}

// Daemon coordinates the periodic execution of the synchronization engine.
// It manages startup, immediate initial sync, ticker loop execution, single-run mode,
// OS signal trapping (SIGINT/SIGTERM), and graceful drain of in-flight cycles.
type Daemon struct {
	syncer Syncer
	cfg    *config.Config
	logger *slog.Logger

	mu       sync.Mutex
	inFlight bool
	wg       sync.WaitGroup
}

// Option allows customizing Daemon configuration.
type Option func(*Daemon)

// WithLogger configures a custom slog.Logger for the Daemon.
func WithLogger(logger *slog.Logger) Option {
	return func(d *Daemon) {
		if logger != nil {
			d.logger = logger
		}
	}
}

// New constructs a new Daemon instance.
// Returns an error if syncer or cfg is nil.
func New(s Syncer, cfg *config.Config, opts ...Option) (*Daemon, error) {
	if s == nil {
		return nil, errors.New("daemon: syncer cannot be nil")
	}
	if cfg == nil {
		return nil, errors.New("daemon: config cannot be nil")
	}

	d := &Daemon{
		syncer: s,
		cfg:    cfg,
	}

	for _, opt := range opts {
		opt(d)
	}

	if d.logger == nil {
		d.logger = NewLogger(cfg.Logging, os.Stderr)
	}

	return d, nil
}

// NewLogger instantiates a structured slog.Logger conforming to LoggingConfig.
// It writes to the provided io.Writer (defaults to os.Stderr if omitted or nil).
func NewLogger(cfg config.LoggingConfig, w ...io.Writer) *slog.Logger {
	var out io.Writer = os.Stderr
	if len(w) > 0 && w[0] != nil {
		out = w[0]
	}

	var level slog.Level
	switch strings.ToLower(strings.TrimSpace(cfg.Level)) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}

	var handler slog.Handler
	switch strings.ToLower(strings.TrimSpace(cfg.Format)) {
	case "json":
		handler = slog.NewJSONHandler(out, opts)
	default:
		handler = slog.NewTextHandler(out, opts)
	}

	return slog.New(handler)
}

// IsInFlight reports whether a sync cycle is actively running.
func (d *Daemon) IsInFlight() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.inFlight
}

// Start begins daemon execution.
// It executes an initial sync cycle immediately, then returns if Once mode is enabled,
// or enters a ticker loop running at cfg.Sync.PollInterval.
// Context cancellation and OS signals (SIGINT, SIGTERM) trigger a graceful shutdown
// that awaits in-flight cycle drain before returning.
func (d *Daemon) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	// Trap OS termination signals (SIGINT, SIGTERM) combined with incoming context.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	pollInterval := d.cfg.Sync.PollInterval.Duration()
	if pollInterval <= 0 {
		pollInterval = 5 * time.Minute
	}

	d.logger.Info("starting replay synchronizer daemon",
		slog.Duration("poll_interval", pollInterval),
		slog.Bool("once", d.cfg.Sync.Once),
		slog.Bool("dry_run", d.cfg.Sync.DryRun),
	)

	// Step 1: Immediate execution on startup
	if err := d.executeCycle(ctx); err != nil {
		if d.cfg.Sync.Once {
			d.logger.Error("single-run mode sync cycle failed", slog.Any("error", err))
			return err
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			d.logger.Info("daemon stopped during initial cycle", slog.Any("reason", err))
			return nil
		}
		d.logger.Error("initial sync cycle failed; continuing to ticker schedule", slog.Any("error", err))
	}

	// Step 2: Exit early if single-run mode
	if d.cfg.Sync.Once {
		d.logger.Info("single-run mode completed successfully")
		return nil
	}

	// Step 3: Ticker loop
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			d.logger.Info("shutdown signal received; awaiting in-flight drain",
				slog.Any("reason", ctx.Err()),
			)
			d.wg.Wait()
			d.logger.Info("graceful drain complete; daemon stopped")
			return nil

		case <-ticker.C:
			if err := d.executeCycle(ctx); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					d.logger.Info("daemon stopped during scheduled cycle", slog.Any("reason", err))
					return nil
				}
				d.logger.Error("scheduled sync cycle failed", slog.Any("error", err))
			}
		}
	}
}

// executeCycle runs a single synchronization cycle, tracking in-flight status,
// recording execution duration, and logging structured metrics.
func (d *Daemon) executeCycle(ctx context.Context) error {
	d.mu.Lock()
	if d.inFlight {
		d.mu.Unlock()
		d.logger.Warn("previous sync cycle still in progress; skipping tick")
		return nil
	}
	d.inFlight = true
	d.wg.Add(1)
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		d.inFlight = false
		d.wg.Done()
		d.mu.Unlock()
	}()

	start := time.Now()
	d.logger.Debug("sync cycle starting")

	stats, err := d.syncer.RunCycle(ctx)
	duration := time.Since(start)

	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			d.logger.Warn("sync cycle aborted due to context cancellation",
				slog.Duration("duration", duration),
				slog.Int64("duration_ms", duration.Milliseconds()),
			)
			return err
		}
		d.logger.Error("sync cycle encountered error",
			slog.Any("error", err),
			slog.Duration("duration", duration),
			slog.Int64("duration_ms", duration.Milliseconds()),
		)
		return err
	}

	if stats != nil {
		d.logger.Info("sync cycle completed",
			slog.Int("discovered", stats.DiscoveredCount),
			slog.Int("downloaded", stats.DownloadedCount),
			slog.Int("uploaded", stats.UploadedCount),
			slog.Int("duplicates", stats.DuplicateCount),
			slog.Int("skipped", stats.SkippedCount),
			slog.Int("failed", stats.FailedCount),
			slog.Duration("duration", duration),
			slog.Int64("duration_ms", duration.Milliseconds()),
		)
	} else {
		d.logger.Info("sync cycle completed",
			slog.Duration("duration", duration),
			slog.Int64("duration_ms", duration.Milliseconds()),
		)
	}

	return nil
}
```

---

### Proposed File: `internal/daemon/daemon_test.go`
*(Also written to `.agents/teamwork/m4_explorer_2/proposed_daemon_test.go`)*

```go
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
```

---

## 5. Verification Method

Once Milestone 4 workers implement `internal/syncer` and `internal/daemon`:

1. **Unit Test Execution**:
   ```powershell
   go test -v -race ./internal/daemon/...
   ```
   - Must pass 100% of unit tests without data races (`-race`).
   - Execution time should be under 5 seconds.

2. **Full Test Suite & E2E Validation**:
   ```powershell
   go test ./internal/...
   go test -v ./test/e2e -run "TestTier1_F15_Daemon|TestTier1_F16_CLI|TestTier1_F17_Log"
   ```
   - Validates that Features 15, 16, and 17 pass without regressions.

3. **Invalidation Conditions**:
   - If `d.Start(ctx)` returns before an active sync cycle finishes during shutdown, `TestDaemon_GracefulDrainAwaitsInFlight` will fail.
   - If `--once` does not exit automatically without context cancellation, `TestDaemon_SingleRunOnce_Success` will hang/timeout.
   - If overlapping sync cycles execute concurrently on slow operations, `TestDaemon_OverlappingCycleSkipped` will fail with count > 3.
   - If logging level or format fails to parse, `TestNewLogger_LevelsAndFormats` will fail.
