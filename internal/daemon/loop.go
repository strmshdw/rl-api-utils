package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/dank/rl-api-utils/internal/session"
)

func (d *Daemon) startStatsListener(ctx context.Context) {
	if d.statsListener == nil || !d.cfg.StatsAPI.Enabled {
		return
	}
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		if err := d.statsListener.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
			d.logger.Warn("Stats API listener stopped with error", slog.Any("error", err))
		}
	}()
}

func (d *Daemon) startHTTPServer(ctx context.Context) {
	if !d.isHTTPEnabled() {
		return
	}
	host := d.httpHost()
	port := d.httpPort()
	httpAddr := fmt.Sprintf("%s:%d", host, port)

	lanIP, _ := DiscoverLANIPv4()
	banner := FormatStartupBanner(host, port, lanIP)
	handler := d.Handler(ctx)

	httpSrv := &http.Server{
		Addr:              httpAddr,
		Handler:           handler,
		BaseContext:       func(_ net.Listener) context.Context { return ctx },
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
		if d.statsListener != nil {
			d.statsListener.Stop()
		}
		if d.sessionTracker != nil && d.sessionTracker.Broadcaster() != nil {
			d.sessionTracker.Broadcaster().Close()
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			if !errors.Is(err, context.Canceled) {
				d.logger.Warn("HTTP server shutdown encountered error", slog.Any("error", err))
			}
			_ = httpSrv.Close()
		}
	}()
}

func (d *Daemon) runEventLoop(ctx context.Context, pollInterval time.Duration) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	statusTicker := time.NewTicker(2 * time.Second)
	defer statusTicker.Stop()
	statsPollTicks := 0

	drainAndStop := func() error {
		d.logger.Info("shutdown signal received; awaiting in-flight drain",
			slog.Any("reason", ctx.Err()),
		)
		if d.statsListener != nil {
			d.statsListener.Stop()
		}
		if d.sessionTracker != nil && d.sessionTracker.Broadcaster() != nil {
			d.sessionTracker.Broadcaster().Close()
		}
		d.wg.Wait()
		d.logger.Info("graceful drain complete; daemon stopped")
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return drainAndStop()
		default:
		}

		select {
		case <-ctx.Done():
			return drainAndStop()

		case reason := <-d.manualTrigger:
			d.logger.Info("executing on-demand sync cycle", slog.String("trigger_reason", reason))
			if err := d.executeCycle(ctx); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
					return drainAndStop()
				}
				d.logger.Error("on-demand sync cycle failed", slog.Any("error", err))
			}

		case <-statusTicker.C:
			d.handleGameStatusTick()
			statsPollTicks++
			if statsPollTicks >= 5 {
				statsPollTicks = 0
				if poller, ok := d.syncer.(StatsPoller); ok {
					if err := d.executeStatsPoll(ctx, poller); err != nil {
						if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
							return drainAndStop()
						}
					}
				}
			}

		case <-ticker.C:
			if d.statsListener != nil && d.statsListener.IsConnected() {
				d.logger.Debug("Rocket League is currently running; skipping routine 5m PsyNet polling to prevent duplicate login kicks (Stats API tracking active)")
				continue
			}
			if err := d.executeCycle(ctx); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
					d.logger.Info("daemon stopped during scheduled cycle", slog.Any("reason", err))
					return drainAndStop()
				}
				d.logger.Error("scheduled sync cycle failed", slog.Any("error", err))
			}
		}
	}
}

func (d *Daemon) handleGameStatusTick() {
	if d.statsListener == nil {
		return
	}
	connected := d.statsListener.IsConnected()
	if d.prevConnected && !connected {
		if d.sessionTracker != nil {
			d.sessionTracker.OnGameDisconnected()
		}
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

func (d *Daemon) executeStatsPoll(ctx context.Context, poller StatsPoller) error {
	d.mu.Lock()
	if d.inFlight {
		d.mu.Unlock()
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

	err := poller.PollAndSyncPendingStats(ctx)
	if err == nil && d.sessionTracker != nil && d.sessionTracker.Broadcaster() != nil {
		summary := d.enrichSessionTelemetry(ctx, d.sessionTracker.GetSessionSummary())
		d.sessionTracker.Broadcaster().Broadcast(session.SessionEvent{
			Event: session.EventSessionUpdate,
			Data:  summary,
		})
	}
	return err
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

	if d.statsTracker != nil {
		d.statsTracker.RefreshPending(ctx)
	}

	if d.sessionTracker != nil && d.sessionTracker.Broadcaster() != nil {
		summary := d.enrichSessionTelemetry(ctx, d.sessionTracker.GetSessionSummary())
		d.sessionTracker.Broadcaster().Broadcast(session.SessionEvent{
			Event: session.EventSessionUpdate,
			Data:  summary,
		})
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
