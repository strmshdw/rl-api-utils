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

// ProviderDisconnector is an optional interface implemented by Syncer to immediately sever
// any idle primary-account PsyNet WebSocket connection.
type ProviderDisconnector interface {
	DisconnectProvider() error
}

// MatchRetrier is an optional interface implemented by Syncer to retry a single match
// (download/upload/stats) without logging into the primary PsyNet account.
type MatchRetrier interface {
	RetryMatch(ctx context.Context, matchGUID string) error
}

// ReplaySyncStatus describes the live or most recent PsyNet replay download/upload sync cycle.
type ReplaySyncStatus struct {
	Running         bool      `json:"running"`
	Reason          string    `json:"reason,omitempty"`
	DiscoveredCount int       `json:"discovered_count"`
	DownloadedCount int       `json:"downloaded_count"`
	UploadedCount   int       `json:"uploaded_count"`
	DuplicateCount  int       `json:"duplicate_count"`
	SkippedCount    int       `json:"skipped_count"`
	FailedCount     int       `json:"failed_count"`
	PendingCount    int       `json:"pending_count"`
	StartedAt       time.Time `json:"started_at,omitempty"`
	EndedAt         time.Time `json:"ended_at,omitempty"`
	LastError       string    `json:"last_error,omitempty"`
}

type syncTriggerReq struct {
	reason string
	force  bool
}

const (
	defaultDisconnectGrace    = 6 * time.Second
	defaultStatusTickInterval = 2 * time.Second
)

// Daemon coordinates the periodic execution of the synchronization engine.
// It manages startup, immediate initial sync, ticker loop execution, single-run mode,
// OS signal trapping (SIGINT/SIGTERM), Rocket League Stats API event tracking,
// and graceful drain of in-flight cycles.
type Daemon struct {
	syncer             Syncer
	cfg                *config.Config
	logger             *slog.Logger
	statsTracker       *statsapi.Tracker
	statsListener      *statsapi.Listener
	playerTracker      *playertrack.Tracker
	sessionTracker     *session.SessionTracker
	stateStore         storage.StateStore
	historySyncer      *syncer.BallchasingHistorySyncer
	webHandler         http.Handler
	manualTrigger      chan syncTriggerReq
	gameProcessRunning func() bool
	disconnectGrace    time.Duration
	disconnectGraceSet bool
	statusTickInterval time.Duration

	mu                    sync.Mutex
	ctx                   context.Context
	replayLimit           int
	inFlight              bool
	pendingManualTriggers int
	replaySync            ReplaySyncStatus
	statsPollInFlight     bool
	historySyncRunning    bool
	wg                    sync.WaitGroup
	prevGameActive        bool
	disconnectedSince     time.Time
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

// WithGameProcessChecker injects a host process check (e.g. IsRocketLeagueRunning) so the daemon
// avoids primary-account PsyNet syncs whenever Rocket League is running, even if Stats API is disconnected.
func WithGameProcessChecker(fn func() bool) Option {
	return func(d *Daemon) {
		d.gameProcessRunning = fn
	}
}

// WithDisconnectGrace sets the confirmation window before treating a Stats API drop as game exit.
func WithDisconnectGrace(grace time.Duration) Option {
	return func(d *Daemon) {
		if grace < 0 {
			grace = 0
		}
		d.disconnectGrace = grace
		d.disconnectGraceSet = true
	}
}

// WithStatusTickInterval overrides the game-status polling cadence (primarily for unit tests).
func WithStatusTickInterval(interval time.Duration) Option {
	return func(d *Daemon) {
		if interval > 0 {
			d.statusTickInterval = interval
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

	replayLimit := cfg.Sync.ReplayLimit
	if replayLimit <= 0 {
		replayLimit = 200
	}

	d := &Daemon{
		syncer:             s,
		cfg:                cfg,
		manualTrigger:      make(chan syncTriggerReq, 16),
		replayLimit:        replayLimit,
		disconnectGrace:    defaultDisconnectGrace,
		statusTickInterval: defaultStatusTickInterval,
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

	if d.sessionTracker != nil {
		d.sessionTracker.SetSummaryEnricher(func(sum *session.SessionResponse) *session.SessionResponse {
			return d.enrichSessionTelemetry(d.daemonContext(), sum)
		})
	}

	if d.playerTracker != nil && d.sessionTracker != nil {
		d.playerTracker.SetMatchStateListener(d.sessionTracker)
	}

	if d.statsTracker != nil {
		d.statsTracker.SetSyncTrigger(d.triggerThresholdSync)
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

// TriggerSync requests an immediate forced synchronization cycle on demand (manual override).
func (d *Daemon) TriggerSync(ctx context.Context, reason string) error {
	return d.enqueueSync(reason, true)
}

func (d *Daemon) triggerThresholdSync(ctx context.Context, reason string) error {
	if !d.isForceSyncOnTriggerEnabled() {
		d.logger.Info("threshold sync skipped because force_sync_on_trigger is disabled", slog.String("reason", reason))
		return nil
	}
	return d.enqueueSync(reason, true)
}

func (d *Daemon) enqueueSync(reason string, force bool) error {
	d.logger.Info("triggering synchronization cycle on demand",
		slog.String("reason", reason),
		slog.Bool("force_override", force),
	)
	req := syncTriggerReq{reason: reason, force: force}
	d.mu.Lock()
	select {
	case d.manualTrigger <- req:
		if force {
			d.pendingManualTriggers++
			if !d.inFlight {
				d.replaySync = ReplaySyncStatus{
					Running:   true,
					Reason:    reason,
					StartedAt: time.Now().UTC(),
				}
			}
		}
		d.mu.Unlock()
		return nil
	default:
		d.mu.Unlock()
		d.logger.Warn("sync trigger already queued; ignoring redundant request", slog.String("reason", reason))
		return nil
	}
}

func (d *Daemon) isForceSyncOnTriggerEnabled() bool {
	if d.statsTracker != nil {
		return d.statsTracker.Status().ForceSyncOnTrigger
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cfg != nil && d.cfg.StatsAPI.ForceSyncOnTrigger
}

func (d *Daemon) isAutoSyncOnExitEnabled() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cfg != nil && d.cfg.StatsAPI.AutoSyncOnExit
}

// isGameActive reports whether Rocket League is currently running or connected via Stats API.
func (d *Daemon) isGameActive() bool {
	if d.statsListener != nil && d.statsListener.IsConnected() {
		return true
	}
	if d.statsTracker != nil && d.statsTracker.IsGameConnected() {
		return true
	}
	if d.gameProcessRunning != nil && d.gameProcessRunning() {
		return true
	}
	return false
}

func (d *Daemon) disconnectPrimaryProvider() {
	if disc, ok := d.syncer.(ProviderDisconnector); ok && disc != nil {
		_ = disc.DisconnectProvider()
	}
}

// ReplaySyncStatusSnapshot returns the live or most recent PsyNet replay sync status.
func (d *Daemon) ReplaySyncStatusSnapshot() ReplaySyncStatus {
	d.mu.Lock()
	snap := d.replaySync
	snap.Running = d.inFlight || d.pendingManualTriggers > 0
	d.mu.Unlock()
	if d.statsTracker != nil {
		snap.PendingCount = d.statsTracker.PendingCount()
	}
	return snap
}

// Start begins daemon execution.
// It executes an initial sync cycle immediately (unless Rocket League is already active in daemon mode),
// then returns if Once mode is enabled, or enters a ticker loop running at cfg.Sync.PollInterval.
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

	if !d.cfg.Sync.Once {
		d.startStatsListener(ctx)
		d.startHTTPServer(ctx)
		d.prevGameActive = d.isGameActive()
	}

	initialReason := "startup"
	initialForce := d.cfg.Sync.Once
	if d.cfg.Sync.Once {
		initialReason = "cli_once"
	}

	if err := d.executeCycle(ctx, initialForce, initialReason); err != nil {
		if d.cfg.Sync.Once {
			d.logger.Error("single-run mode sync cycle failed", slog.Any("error", err))
			return err
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			d.logger.Info("daemon stopped during initial cycle", slog.Any("reason", err))
			if d.statsListener != nil {
				d.statsListener.Stop()
			}
			if d.statsTracker != nil {
				d.statsTracker.Close()
			}
			if d.sessionTracker != nil && d.sessionTracker.Broadcaster() != nil {
				d.sessionTracker.Broadcaster().Close()
			}
			d.wg.Wait()
			return nil
		}
		d.logger.Error("initial sync cycle failed; continuing to ticker schedule", slog.Any("error", err))
	}

	if d.cfg.Sync.Once {
		d.logger.Info("single-run mode completed successfully")
		return nil
	}

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
	return "0.0.0.0"
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
