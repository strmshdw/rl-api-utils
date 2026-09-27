package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
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
	"github.com/dank/rl-api-utils/internal/web"
)

// Syncer represents the domain orchestrator interface required by the daemon.
// It decouples the lifecycle daemon from concrete syncer implementations.
type Syncer interface {
	RunCycle(ctx context.Context) (*syncer.SyncStats, error)
}

// PlayerDetailResponse represents the composite response for GET /players/{id}.
type PlayerDetailResponse struct {
	*storage.PlayerRecord
	Matchups []*storage.PlayerMatchup `json:"matchups"`
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
	webHandler     http.Handler
	manualTrigger  chan string

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

	// Instantiate SessionTracker if not explicitly provided via WithSessionTracker
	if d.sessionTracker == nil {
		d.sessionTracker = session.NewSessionTracker(session.WithLogger(d.logger))
	}

	// Wire playerTracker -> sessionTracker observer listener
	if d.playerTracker != nil && d.sessionTracker != nil {
		d.playerTracker.SetMatchStateListener(d.sessionTracker)
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

	// Step 4: Start unified local HTTP server if configured and not in Once mode
	if d.isHTTPEnabled() {
		host := d.httpHost()
		port := d.httpPort()
		httpAddr := fmt.Sprintf("%s:%d", host, port)

		lanIP, _ := DiscoverLANIPv4()
		banner := FormatStartupBanner(host, port, lanIP)

		handler := d.Handler(ctx)

		httpSrv := &http.Server{
			Addr:              httpAddr,
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      0, // Disabled: required for persistent SSE connections
			IdleTimeout:       60 * time.Second,
		}

		d.wg.Add(1)
		go func() {
			defer d.wg.Done()

			// Check loopback availability on Windows to catch external port conflicts
			probeLn, probeErr := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if probeErr != nil {
				d.logger.Warn("local HTTP API server stopped with error", slog.Any("error", probeErr))
				return
			}
			_ = probeLn.Close()

			d.logger.Info(banner,
				slog.String("addr", httpAddr),
				slog.String("host", host),
				slog.Int("port", port),
				slog.String("local_url", fmt.Sprintf("http://localhost:%d", port)),
				slog.String("network_url", fmt.Sprintf("http://%s:%d", lanIP, port)),
				slog.String("overlay_url", fmt.Sprintf("http://localhost:%d/?mode=overlay", port)),
			)
			if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				d.logger.Warn("local HTTP API server stopped with error", slog.Any("error", err))
			}
		}()

		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := httpSrv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
				d.logger.Warn("HTTP server shutdown encountered error", slog.Any("error", err))
			}
			if d.sessionTracker != nil && d.sessionTracker.Broadcaster() != nil {
				d.sessionTracker.Broadcaster().Close()
			}
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

// Handler constructs and returns an http.Handler wired with all active daemon routes and CORS middleware.
// Intended for unit testing via httptest.NewServer or direct httptest.ResponseRecorder execution.
func (d *Daemon) Handler(ctx context.Context) http.Handler {
	if ctx == nil {
		ctx = context.Background()
	}
	return corsMiddleware(d.setupRoutes(ctx))
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
	port := d.httpPort()
	if port <= 0 {
		return false
	}
	return d.cfg.Web.Enabled || (d.cfg.StatsAPI.Enabled && d.cfg.StatsAPI.HTTPTriggerPort > 0) || d.cfg.PlayerTracking.Enabled
}

func (d *Daemon) setupRoutes(ctx context.Context) *http.ServeMux {
	mux := http.NewServeMux()

	// Diagnostic & Health Endpoints
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"running"}`))
	})

	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if d.statsTracker != nil {
			_ = json.NewEncoder(w).Encode(d.statsTracker.Status())
		} else {
			w.Write([]byte(`{"status":"running"}`))
		}
	})

	// Stats API Trigger Endpoint (Backward compatible)
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

	// REST API v1 Endpoints (Milestone M4)
	mux.HandleFunc("GET /api/session", d.handleGetSession)
	mux.HandleFunc("/api/session/reset", d.handleResetSession)
	mux.HandleFunc("GET /api/current-match", d.handleCurrentMatch)
	mux.HandleFunc("GET /api/players", d.handleListPlayers)
	mux.HandleFunc("GET /api/players/{id...}", d.handleGetPlayer)
	mux.HandleFunc("GET /api/events", d.handleEvents)

	// Backward Compatible Player Tracking Aliases
	mux.HandleFunc("GET /current-match", d.handleCurrentMatch)
	mux.HandleFunc("GET /players", d.handleListPlayers)
	mux.HandleFunc("GET /players/{id...}", d.handleGetPlayer)

	// Embedded Static Frontend SPA (Fallback to index.html for non-API routes)
	if d.webHandler != nil {
		mux.Handle("/", d.webHandler)
	} else {
		mux.Handle("/", web.DistHandler())
	}

	return mux
}

func isPathTraversal(r *http.Request) bool {
	p := r.URL.Path
	if strings.Contains(p, "..") || strings.HasPrefix(p, "//") || strings.Contains(p, "\\") {
		return true
	}
	raw := r.URL.RawPath
	if raw != "" {
		if strings.Contains(raw, "..") || strings.HasPrefix(raw, "//") || strings.Contains(raw, "\\") {
			return true
		}
	}
	reqURI := r.RequestURI
	if strings.Contains(reqURI, "..") || strings.HasPrefix(reqURI, "//") {
		return true
	}
	lowerURI := strings.ToLower(reqURI)
	if strings.Contains(lowerURI, "%2e") || strings.Contains(lowerURI, "%5c") {
		return true
	}
	return false
}

// corsMiddleware provides standard Cross-Origin Resource Sharing headers for local development
// and handles browser preflight OPTIONS requests.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reject path traversal attempts immediately before ServeMux canonicalization redirects
		if isPathTraversal(r) {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
