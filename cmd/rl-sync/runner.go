package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/psynet"
	"github.com/dank/rl-api-utils/internal/syncer"
)

// Run executes the command line entrypoint given arguments and returns the process exit code.
// Exit code 0 denotes successful execution (including clean termination or --once completion).
// Exit code 1 denotes fatal configuration, initialization, authentication, or unrecoverable error.
func (r *Runner) Run(ctx context.Context, args []string) int {
	opts, exitCode, shouldExit := parseCLIFlags(args, r.Stdout, r.Stderr)
	if shouldExit {
		return exitCode
	}

	cfg, err := r.LoadConfig(opts.flags)
	if err != nil {
		fmt.Fprintf(r.Stderr, "Configuration error: %v\n", err)
		return 1
	}

	logger := daemon.NewLogger(cfg.Logging, r.Stderr)
	slog.SetDefault(logger)

	logger.Info("configuration loaded successfully",
		slog.String("auth_provider", cfg.Auth.Provider),
		slog.String("db_path", cfg.Sync.DBPath),
		slog.String("replay_dir", cfg.Sync.ReplayDir),
		slog.Duration("poll_interval", cfg.Sync.PollInterval.Duration()),
		slog.Duration("session_continue_window", cfg.EffectiveSessionContinueWindow()),
		slog.Bool("dry_run", cfg.Sync.DryRun),
		slog.Bool("once", cfg.Sync.Once),
		slog.String("log_level", cfg.Logging.Level),
		slog.String("log_format", cfg.Logging.Format),
		slog.Bool("web_enabled", cfg.Web.Enabled),
		slog.String("web_host", cfg.Web.Host),
		slog.Int("web_port", cfg.Web.Port),
	)

	store, err := r.NewStore(cfg.Sync.DBPath)
	if err != nil {
		logger.Error("failed to initialize persistent store",
			slog.String("db_path", cfg.Sync.DBPath),
			slog.Any("error", err),
		)
		return 1
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			logger.Warn("error closing persistent store", slog.Any("error", closeErr))
		}
	}()

	authProvider, err := r.initPrimaryAuth(ctx, cfg, store, logger)
	if err != nil {
		return 1
	}

	psyClient, err := r.NewPsyNet(psynet.ClientConfig{
		CredentialsSupplier: &authSupplier{provider: authProvider},
		Logger:              logger,
	})
	if err != nil {
		logger.Error("failed to initialize psynet client", slog.Any("error", err))
		return 1
	}
	defer func() {
		if closeErr := psyClient.Close(); closeErr != nil {
			logger.Warn("error closing psynet client", slog.Any("error", closeErr))
		}
	}()

	downloader := r.NewDownloader(cfg.Sync.DownloadTimeout.Duration())
	if cleaned, cleanErr := psynet.CleanupStaleTempFiles(cfg.Sync.ReplayDir); cleanErr == nil && cleaned > 0 {
		logger.Info("cleaned stale temporary replay files", slog.Int("count", cleaned))
	}

	bcClient, err := r.NewBallchasing(ballchasing.ClientConfig{
		BaseURL:      cfg.Ballchasing.BaseURL,
		APIKey:       cfg.Ballchasing.APIKey,
		Visibility:   cfg.Ballchasing.Visibility,
		MaxRetries:   cfg.Ballchasing.MaxRetries,
		Timeout:      cfg.Ballchasing.Timeout.Duration(),
		StreamUpload: true,
	})
	if err != nil {
		logger.Error("failed to initialize ballchasing client", slog.Any("error", err))
		return 1
	}

	logger.Info("verifying ballchasing api key")
	if err := bcClient.Ping(ctx); err != nil {
		logger.Error("ballchasing api key verification failed", slog.Any("error", err))
		return 1
	}
	logger.Info("ballchasing api key verified successfully")

	if opts.syncBallchasingHistory {
		return runBallchasingHistorySync(ctx, bcClient, store, cfg, logger)
	}

	syncerConfig := syncer.Config{
		ReplayDir:       cfg.Sync.ReplayDir,
		DryRun:          cfg.Sync.DryRun,
		KeepLocalFiles:  cfg.Sync.KeepLocalFiles,
		LocalPlayerID:   cfg.PlayerTracking.LocalPlayerID,
		LocalPlayerName: cfg.PlayerTracking.LocalPlayerName,
		Logger:          logger,
	}
	syncerEngine := r.NewSyncer(store, psyClient, downloader, bcClient, syncerConfig)

	playerTracker, ptCleanup, err := r.initPlayerTracker(ctx, cfg, store, authProvider, logger)
	if err != nil {
		return 1
	}
	defer ptCleanup()

	daemonOpts := []daemon.Option{daemon.WithLogger(logger)}
	if store != nil {
		daemonOpts = append(daemonOpts, daemon.WithStateStore(store))
	}
	if concreteBC, ok := bcClient.(*ballchasing.Client); ok && concreteBC != nil {
		historySyncer := syncer.NewBallchasingHistorySyncer(
			concreteBC,
			store,
			cfg.PlayerTracking.LocalPlayerID,
			cfg.PlayerTracking.LocalPlayerName,
			logger,
		)
		daemonOpts = append(daemonOpts, daemon.WithHistorySyncer(historySyncer))
	}
	if playerTracker != nil {
		daemonOpts = append(daemonOpts, daemon.WithPlayerTracker(playerTracker))
	}
	daemonOpts = append(daemonOpts, buildStatsDaemonOptions(cfg, store, playerTracker, logger)...)

	daemonEngine, err := r.NewDaemon(syncerEngine, cfg, daemonOpts...)
	if err != nil {
		logger.Error("failed to initialize daemon engine", slog.Any("error", err))
		return 1
	}

	if err := daemonEngine.Start(ctx); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			logger.Info("daemon stopped due to context cancellation")
			return 0
		}
		logger.Error("daemon terminated with fatal error", slog.Any("error", err))
		return 1
	}

	logger.Info("daemon shutdown gracefully")
	return 0
}
