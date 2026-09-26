package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/syncer"
)

// Syncer represents the domain orchestrator interface required by the daemon.
// It decouples the lifecycle daemon from concrete syncer implementations.
type Syncer interface {
	RunCycle(ctx context.Context) (*syncer.SyncStats, error)
}

// Daemon coordinates the periodic execution of the synchronization engine.
// It manages startup, immediate initial sync, ticker loop execution, single-run mode,
// OS signal trapping (SIGINT/SIGTERM), Rocket League Stats API event tracking,
// and graceful drain of in-flight cycles.
type Daemon struct {
	syncer        Syncer
	cfg           *config.Config
	logger        *slog.Logger
	statsTracker  *statsapi.Tracker
	statsListener *statsapi.Listener
	manualTrigger chan string

	mu            sync.Mutex
	inFlight      bool
	wg            sync.WaitGroup
	prevConnected bool
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

// WithStatsTracker attaches a Stats API match evaluator and trigger tracker.
func WithStatsTracker(tracker *statsapi.Tracker) Option {
	return func(d *Daemon) {
		d.statsTracker = tracker
	}
}

// WithStatsListener attaches a Stats API event listener.
func WithStatsListener(listener *statsapi.Listener) Option {
	return func(d *Daemon) {
		d.statsListener = listener
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
		syncer:        s,
		cfg:           cfg,
		manualTrigger: make(chan string, 16),
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

// TriggerSync requests an immediate synchronization cycle on demand.
func (d *Daemon) TriggerSync(ctx context.Context, reason string) error {
	d.logger.Info("triggering synchronization cycle on demand", slog.String("reason", reason))
	select {
	case d.manualTrigger <- reason:
		return nil
	default:
		d.logger.Warn("sync trigger already queued; ignoring redundant request", slog.String("reason", reason))
		return nil
	}
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
		slog.Bool("stats_api_enabled", d.cfg.StatsAPI.Enabled),
		slog.Int("trigger_threshold", d.cfg.StatsAPI.TriggerThreshold),
		slog.Bool("force_sync_on_trigger", d.cfg.StatsAPI.ForceSyncOnTrigger),
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

	// Step 3: Start Stats API background listener if configured
	if d.statsListener != nil && d.cfg.StatsAPI.Enabled {
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			if err := d.statsListener.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
				d.logger.Warn("Stats API listener stopped with error", slog.Any("error", err))
			}
		}()
	}

	// Step 4: Start local HTTP trigger endpoint if configured
	if d.cfg.StatsAPI.Enabled && d.cfg.StatsAPI.HTTPTriggerPort > 0 {
		httpAddr := fmt.Sprintf("127.0.0.1:%d", d.cfg.StatsAPI.HTTPTriggerPort)
		mux := http.NewServeMux()
		mux.HandleFunc("/sync", func(w http.ResponseWriter, r *http.Request) {
			d.logger.Info("received manual trigger request via local HTTP endpoint", slog.String("remote", r.RemoteAddr))
			if err := d.TriggerSync(ctx, "manual_http_request"); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(fmt.Sprintf(`{"error":%q}`, err.Error())))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"sync_triggered"}`))
		})
		mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if d.statsTracker != nil {
				json.NewEncoder(w).Encode(d.statsTracker.Status())
			} else {
				w.Write([]byte(`{"status":"running"}`))
			}
		})

		httpSrv := &http.Server{
			Addr:    httpAddr,
			Handler: mux,
		}

		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			d.logger.Info("local HTTP trigger endpoint ready", slog.String("url", fmt.Sprintf("http://%s/sync", httpAddr)))
			if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				d.logger.Warn("local HTTP trigger server stopped", slog.Any("error", err))
			}
		}()

		go func() {
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			httpSrv.Shutdown(shutdownCtx)
		}()
	}

	// Step 5: Ticker and event loop
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	statusTicker := time.NewTicker(2 * time.Second)
	defer statusTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			d.logger.Info("shutdown signal received; awaiting in-flight drain",
				slog.Any("reason", ctx.Err()),
			)
			d.wg.Wait()
			d.logger.Info("graceful drain complete; daemon stopped")
			return nil

		case reason := <-d.manualTrigger:
			d.logger.Info("executing on-demand sync cycle", slog.String("trigger_reason", reason))
			if err := d.executeCycle(ctx); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return nil
				}
				d.logger.Error("on-demand sync cycle failed", slog.Any("error", err))
			}

		case <-statusTicker.C:
			// Monitor game connection state for AutoSyncOnExit
			if d.statsListener != nil {
				connected := d.statsListener.IsConnected()
				if d.prevConnected && !connected {
					// Game client just terminated or disconnected
					if d.cfg.StatsAPI.AutoSyncOnExit && d.statsTracker != nil && d.statsTracker.PendingCount() > 0 {
						d.logger.Info("Rocket League disconnected; auto-syncing pending replays upon game exit",
							slog.Int("pending_matches", d.statsTracker.PendingCount()),
						)
						select {
						case d.manualTrigger <- "game_exit_sync":
						default:
						}
					}
				}
				d.prevConnected = connected
			}

		case <-ticker.C:
			// Routine scheduled tick
			if d.statsListener != nil && d.statsListener.IsConnected() {
				// While game is running, avoid routine 5m PsyNet polling to prevent duplicate login kicks
				d.logger.Debug("Rocket League is currently running; skipping routine 5m PsyNet polling to prevent duplicate login kicks (Stats API tracking active)")
				continue
			}

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
// recording execution duration, logging structured metrics, and reconciling pending matches.
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

	// Reconcile pending matches in stats tracker after successful cycle
	if d.statsTracker != nil {
		d.statsTracker.RefreshPending(ctx)
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
