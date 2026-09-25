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
