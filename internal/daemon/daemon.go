package daemon

import (
	"context"
	"errors"
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
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/session"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/syncer"
)

// Syncer represents the domain orchestrator interface required by the daemon.
// It decouples the lifecycle daemon from concrete syncer implementations.
type Syncer interface {
	RunCycle(ctx context.Context) (*syncer.SyncStats, error)
}

// StatsPoller is an optional interface implemented by Syncer to poll pending Ballchasing stats
// independently of full PsyNet discovery cycles (safe to run while Rocket League is active).
type StatsPoller interface {
	PollAndSyncPendingStats(ctx context.Context) error
}

// Daemon coordinates the periodic execution of the synchronization engine.
// It manages startup, immediate initial sync, ticker loop execution, single-run mode,
// OS signal trapping (SIGINT/SIGTERM), Rocket League Stats API event tracking,
// and graceful drain of in-flight cycles.
type Daemon struct {
	syncer         Syncer
	cfg            *config.Config
	logger         *slog.Logger
	statsTracker   *statsapi.Tracker
	statsListener  *statsapi.Listener
	playerTracker  *playertrack.Tracker
	sessionTracker *session.SessionTracker
	stateStore     storage.StateStore
	historySyncer  *syncer.BallchasingHistorySyncer
	webHandler     http.Handler
	manualTrigger  chan string

	mu            sync.Mutex
	ctx           context.Context
	replayLimit   int
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

// WithPlayerTracker attaches a PlayerTracker instance to the Daemon.
func WithPlayerTracker(tracker *playertrack.Tracker) Option {
	return func(d *Daemon) {
		d.playerTracker = tracker
	}
}

// WithSessionTracker attaches a SessionTracker instance to the Daemon.
func WithSessionTracker(tracker *session.SessionTracker) Option {
	return func(d *Daemon) {
		d.sessionTracker = tracker
	}
}

// WithWebHandler attaches a custom web handler for static asset serving (useful for testing).
func WithWebHandler(handler http.Handler) Option {
	return func(d *Daemon) {
		d.webHandler = handler
	}
}

// SessionTracker returns the underlying SessionTracker instance.
func (d *Daemon) SessionTracker() *session.SessionTracker {
	return d.sessionTracker
}

// WithStateStore attaches a storage.StateStore backend to the Daemon for serving player queries.
func WithStateStore(store storage.StateStore) Option {
	return func(d *Daemon) {
		d.stateStore = store
	}
}

// WithStore attaches persistent storage to the Daemon (alias for WithStateStore).
func WithStore(store storage.StateStore) Option {
	return WithStateStore(store)
}

// WithHistorySyncer attaches a BallchasingHistorySyncer for on-demand historical imports.
func WithHistorySyncer(s *syncer.BallchasingHistorySyncer) Option {
	return func(d *Daemon) {
		d.historySyncer = s
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

	replayLimit := cfg.Sync.ReplayLimit
	if replayLimit <= 0 {
		replayLimit = 200
	}

	d := &Daemon{
		syncer:        s,
		cfg:           cfg,
		manualTrigger: make(chan string, 16),
		replayLimit:   replayLimit,
	}

	for _, opt := range opts {
		opt(d)
	}

	if d.logger == nil {
		d.logger = NewLogger(cfg.Logging, os.Stderr)
	}

	if d.sessionTracker == nil {
		d.sessionTracker = session.NewSessionTracker(
			session.WithLogger(d.logger),
			session.WithStateStore(d.stateStore),
			session.WithContinueWindow(d.cfg.EffectiveSessionContinueWindow()),
		)
	} else {
		if d.stateStore != nil {
			d.sessionTracker.SetStateStore(d.stateStore)
		}
		if !d.sessionTracker.HasExplicitContinueWindow() {
			d.sessionTracker.SetContinueWindow(d.cfg.EffectiveSessionContinueWindow())
		}
	}

	if d.playerTracker != nil && d.sessionTracker != nil {
		d.playerTracker.SetMatchStateListener(d.sessionTracker)
	}

	if d.statsTracker != nil {
		d.statsTracker.SetSyncTrigger(d.TriggerSync)
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

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	d.mu.Lock()
	d.ctx = ctx
	d.mu.Unlock()

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

	if d.cfg.Sync.Once {
		d.logger.Info("single-run mode completed successfully")
		return nil
	}

	d.startStatsListener(ctx)
	d.startHTTPServer(ctx)

	return d.runEventLoop(ctx, pollInterval)
}

func (d *Daemon) daemonContext() context.Context {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ctx != nil {
		return d.ctx
	}
	return context.Background()
}

func (d *Daemon) getReplayLimit() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.replayLimit > 0 {
		return d.replayLimit
	}
	if d.cfg != nil && d.cfg.Sync.ReplayLimit > 0 {
		return d.cfg.Sync.ReplayLimit
	}
	return 200
}

func (d *Daemon) setReplayLimit(limit int) {
	if limit <= 0 {
		return
	}
	d.mu.Lock()
	d.replayLimit = limit
	if d.cfg != nil {
		d.cfg.Sync.ReplayLimit = limit
	}
	d.mu.Unlock()
}

func (d *Daemon) httpHost() string {
	if d.cfg.Web.Host != "" {
		return d.cfg.Web.Host
	}
	return "127.0.0.1"
}

func (d *Daemon) httpPort() int {
	if d.cfg.Web.Port > 0 && d.cfg.Web.Port != 49125 {
		return d.cfg.Web.Port
	}
	if d.cfg.StatsAPI.HTTPTriggerPort > 0 {
		return d.cfg.StatsAPI.HTTPTriggerPort
	}
	if d.cfg.Web.Port > 0 {
		return d.cfg.Web.Port
	}
	return 49125
}

func (d *Daemon) isHTTPEnabled() bool {
	if d.cfg.Sync.Once {
		return false
	}
	if d.httpPort() <= 0 {
		return false
	}
	return d.cfg.Web.Enabled || (d.cfg.StatsAPI.Enabled && d.cfg.StatsAPI.HTTPTriggerPort > 0) || d.cfg.PlayerTracking.Enabled
}
