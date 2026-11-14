package main

import (
	"context"
	"log/slog"

	"github.com/dank/rl-api-utils/internal/auth"
	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/syncer"
)

func (r *Runner) initPrimaryAuth(ctx context.Context, cfg *config.Config, store storage.StateStore, logger *slog.Logger) (auth.AuthProvider, error) {
	tokenSaver := auth.NewConfigTokenSaver(cfg.ConfigPath)
	prompter := auth.NewTerminalPrompter(r.Stdin, r.Stdout)
	steamGen := auth.NewNodeSteamTicketGenerator("")

	r.AuthOptions = append(r.AuthOptions,
		auth.WithAccountRole(auth.RolePrimary),
		auth.WithCodePrompter(prompter),
		auth.WithSteamGenerator(steamGen),
		auth.WithConfigTokenSaver(tokenSaver),
	)

	authProvider, err := r.NewAuth(cfg.Auth, store)
	if err != nil {
		logger.Error("failed to initialize auth provider",
			slog.String("provider", cfg.Auth.Provider),
			slog.Any("error", err),
		)
		return nil, err
	}

	if err := authProvider.Validate(); err != nil {
		logger.Error("invalid authentication credentials", slog.Any("error", err))
		return nil, err
	}

	logger.Info("authenticating credentials", slog.String("provider", authProvider.Name()))
	tokenInfo, err := authProvider.Authenticate(ctx)
	if err != nil {
		logger.Error("initial authentication failed",
			slog.String("provider", authProvider.Name()),
			slog.Any("error", err),
		)
		return nil, err
	}
	logger.Info("authenticated successfully",
		slog.String("provider", authProvider.Name()),
		slog.String("account_id", tokenInfo.AccountID),
		slog.String("display_name", tokenInfo.DisplayName),
	)
	return authProvider, nil
}

func runBallchasingHistorySync(ctx context.Context, bcClient ballchasing.ReplayUploader, store storage.StateStore, cfg *config.Config, logger *slog.Logger) int {
	logger.Info("running standalone Ballchasing historical synchronization")
	concreteBC, ok := bcClient.(*ballchasing.Client)
	if !ok || concreteBC == nil {
		logger.Error("ballchasing client does not support historical synchronization")
		return 1
	}
	hs := syncer.NewBallchasingHistorySyncer(
		concreteBC,
		store,
		cfg.PlayerTracking.LocalPlayerID,
		cfg.PlayerTracking.LocalPlayerName,
		logger,
	)
	prog, err := hs.Sync(ctx)
	if err != nil {
		logger.Error("historical ballchasing synchronization failed", slog.Any("error", err))
		return 1
	}
	logger.Info("historical ballchasing synchronization completed successfully",
		slog.Int("total_replays", prog.TotalReplays),
		slog.Int("synced_matches", prog.SyncedMatches),
		slog.Int("synced_players", prog.SyncedPlayers),
	)
	return 0
}

func (r *Runner) initPlayerTracker(ctx context.Context, cfg *config.Config, store storage.StateStore, authProvider auth.AuthProvider, logger *slog.Logger) (*playertrack.Tracker, func(), error) {
	if !cfg.PlayerTracking.Enabled {
		return nil, func() {}, nil
	}

	logger.Info("initializing player tracking subsystem",
		slog.Bool("auto_fetch_ranks", cfg.PlayerTracking.AutoFetchRanks),
		slog.String("local_player_id", cfg.PlayerTracking.LocalPlayerID),
		slog.String("local_player_name", cfg.PlayerTracking.LocalPlayerName),
		slog.Bool("polling_auth_enabled", cfg.PollingAuth.Enabled),
	)

	var rankClient playertrack.SkillFetcher = playertrack.NewNoOpRankClient()
	if cfg.PollingAuth.Enabled {
		rankClient = r.initPollingRankClient(ctx, cfg, store, logger)
	}

	playerTracker, err := r.NewPlayerTracker(
		store,
		rankClient,
		cfg.PlayerTracking,
		cfg.Auth,
		playertrack.WithTrackerLogger(logger),
		playertrack.WithTrackerAuthProvider(authProvider),
	)
	if err != nil {
		if closeErr := rankClient.Close(); closeErr != nil {
			logger.Warn("error closing rank client", slog.Any("error", closeErr))
		}
		logger.Error("failed to initialize player tracker", slog.Any("error", err))
		return nil, func() {}, err
	}

	cleanup := func() {
		logger.Info("closing player tracker")
		if closeErr := playerTracker.Close(); closeErr != nil {
			logger.Warn("error closing player tracker", slog.Any("error", closeErr))
		}
		if closeErr := rankClient.Close(); closeErr != nil {
			logger.Warn("error closing rank client", slog.Any("error", closeErr))
		}
	}
	return playerTracker, cleanup, nil
}

func (r *Runner) initPollingRankClient(ctx context.Context, cfg *config.Config, store storage.StateStore, logger *slog.Logger) playertrack.SkillFetcher {
	tokenSaver := auth.NewConfigTokenSaver(cfg.ConfigPath)
	prompter := auth.NewTerminalPrompter(r.Stdin, r.Stdout)
	steamGen := auth.NewNodeSteamTicketGenerator("")

	pollingAuth, authErr := r.NewPollingAuth(cfg.PollingAuth,
		auth.WithAccountRole(auth.RolePolling),
		auth.WithStateStore(store),
		auth.WithCodePrompter(prompter),
		auth.WithSteamGenerator(steamGen),
		auth.WithConfigTokenSaver(tokenSaver),
	)
	if authErr != nil {
		logger.Warn("failed to initialize polling auth provider; falling back to NoOpRankClient",
			slog.Any("error", authErr),
		)
		return playertrack.NewNoOpRankClient()
	}

	logger.Info("authenticating polling account credentials",
		slog.String("provider", pollingAuth.Name()),
		slog.String("account_type", auth.RolePolling.Label()),
	)
	pollToken, pollErr := pollingAuth.Authenticate(ctx)
	if pollErr != nil {
		logger.Warn("polling account authentication failed; falling back to NoOpRankClient",
			slog.String("provider", pollingAuth.Name()),
			slog.Any("error", pollErr),
		)
		return playertrack.NewNoOpRankClient()
	}

	logger.Info("polling account authenticated successfully",
		slog.String("provider", pollingAuth.Name()),
		slog.String("account_id", pollToken.AccountID),
		slog.String("display_name", pollToken.DisplayName),
	)
	rc, rcErr := r.NewRankClient(playertrack.RankClientConfig{
		PrimaryAuth:  cfg.Auth,
		PollingAuth:  cfg.PollingAuth,
		AuthProvider: pollingAuth,
		Logger:       logger,
	})
	if rcErr != nil {
		logger.Warn("failed to initialize rank client; falling back to NoOpRankClient",
			slog.Any("error", rcErr),
		)
		return playertrack.NewNoOpRankClient()
	}
	return rc
}

func buildStatsDaemonOptions(cfg *config.Config, store storage.StateStore, playerTracker *playertrack.Tracker, logger *slog.Logger) []daemon.Option {
	if !cfg.StatsAPI.Enabled || cfg.Sync.Once {
		return nil
	}

	trackerCfg := statsapi.TrackerConfig{
		TriggerThreshold:   cfg.StatsAPI.TriggerThreshold,
		ForceSyncOnTrigger: cfg.StatsAPI.ForceSyncOnTrigger,
		EnableToast:        cfg.StatsAPI.EnableToast,
	}
	tracker, trackerErr := statsapi.NewTracker(store, trackerCfg, statsapi.WithLogger(logger))
	if trackerErr != nil {
		logger.Warn("failed to initialize stats tracker", slog.Any("error", trackerErr))
		return nil
	}

	listenerCfg := statsapi.ListenerConfig{
		Address:  cfg.StatsAPI.Address,
		Protocol: cfg.StatsAPI.Protocol,
	}
	var listenerOpts []statsapi.ListenerOption
	if playerTracker != nil {
		listenerOpts = append(listenerOpts, statsapi.WithPlayerEventHandler(playerTracker))
	}
	listener, listenerErr := statsapi.NewListener(listenerCfg, tracker, logger, listenerOpts...)
	if listenerErr != nil {
		logger.Warn("failed to initialize stats listener", slog.Any("error", listenerErr))
		return nil
	}
	if playerTracker != nil {
		listener.SetPlayerEventHandler(playerTracker)
	}

	return []daemon.Option{
		daemon.WithStatsTracker(tracker),
		daemon.WithStatsListener(listener),
	}
}
