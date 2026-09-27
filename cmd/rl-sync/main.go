package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/dank/rl-api-utils/internal/auth"
	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/psynet"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/syncer"
)

// Version is populated at link time or defaults to "dev".
var Version = "dev"

func main() {
	// Root context trapping OS termination signals (SIGINT, SIGTERM)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runner := NewDefaultRunner(os.Stdout, os.Stderr)
	code := runner.Run(ctx, os.Args[1:])
	os.Exit(code)
}

// DaemonEngine abstracts the lifecycle execution of the daemon for testing.
type DaemonEngine interface {
	Start(ctx context.Context) error
}

// Runner encapsulates CLI flag parsing, layered configuration resolution,
// component lifecycle wiring, and graceful execution.
type Runner struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	// Dependency injection hooks for unit testing
	LoadConfig       func(cli config.CLIFlags) (*config.Config, error)
	NewStore         func(path string) (storage.StateStore, error)
	NewAuth          func(cfg config.AuthConfig, store storage.StateStore) (auth.AuthProvider, error)
	AuthOptions      []auth.Option
	NewPsyNet        func(cfg psynet.ClientConfig) (psynet.MatchHistoryProvider, error)
	NewDownloader    func(timeout time.Duration) psynet.ReplayDownloader
	NewBallchasing   func(cfg ballchasing.ClientConfig) (ballchasing.ReplayUploader, error)
	NewSyncer        func(store storage.StateStore, p psynet.MatchHistoryProvider, d psynet.ReplayDownloader, u ballchasing.ReplayUploader, cfg syncer.Config) daemon.Syncer
	NewDaemon        func(s daemon.Syncer, cfg *config.Config, opts ...daemon.Option) (DaemonEngine, error)
	NewPollingAuth   func(cfg config.PollingAuthConfig, opts ...auth.Option) (auth.AuthProvider, error)
	NewRankClient    func(cfg playertrack.RankClientConfig) (playertrack.SkillFetcher, error)
	NewPlayerTracker func(store storage.StateStore, rankClient playertrack.SkillFetcher, cfg config.PlayerTrackingConfig, authCfg config.AuthConfig, opts ...playertrack.TrackerOption) (*playertrack.Tracker, error)
}

// NewDefaultRunner constructs a Runner wired with production constructors.
func NewDefaultRunner(stdout, stderr io.Writer) *Runner {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}

	r := &Runner{
		Stdin:  os.Stdin,
		Stdout: stdout,
		Stderr: stderr,
		LoadConfig: func(cli config.CLIFlags) (*config.Config, error) {
			return config.Load(cli)
		},
		NewStore: func(path string) (storage.StateStore, error) {
			return storage.NewStore(path)
		},
		NewPsyNet: func(cfg psynet.ClientConfig) (psynet.MatchHistoryProvider, error) {
			return psynet.NewClient(cfg)
		},
		NewDownloader: func(timeout time.Duration) psynet.ReplayDownloader {
			return psynet.NewDownloader(psynet.WithTimeout(timeout))
		},
		NewBallchasing: func(cfg ballchasing.ClientConfig) (ballchasing.ReplayUploader, error) {
			return ballchasing.NewClient(cfg)
		},
		NewSyncer: func(store storage.StateStore, p psynet.MatchHistoryProvider, d psynet.ReplayDownloader, u ballchasing.ReplayUploader, cfg syncer.Config) daemon.Syncer {
			s, err := syncer.NewWithConfig(store, p, d, u, cfg)
			if err != nil {
				panic(err)
			}
			return s
		},
		NewDaemon: func(s daemon.Syncer, cfg *config.Config, opts ...daemon.Option) (DaemonEngine, error) {
			return daemon.New(s, cfg, opts...)
		},
		NewPollingAuth: func(cfg config.PollingAuthConfig, opts ...auth.Option) (auth.AuthProvider, error) {
			return auth.NewPollingProvider(cfg, opts...)
		},
		NewRankClient: func(cfg playertrack.RankClientConfig) (playertrack.SkillFetcher, error) {
			return playertrack.NewRankClient(cfg)
		},
		NewPlayerTracker: func(store storage.StateStore, rankClient playertrack.SkillFetcher, cfg config.PlayerTrackingConfig, authCfg config.AuthConfig, opts ...playertrack.TrackerOption) (*playertrack.Tracker, error) {
			return playertrack.NewTracker(store, rankClient, cfg, authCfg, opts...)
		},
	}

	r.NewAuth = func(cfg config.AuthConfig, store storage.StateStore) (auth.AuthProvider, error) {
		return auth.NewProvider(cfg, store, r.AuthOptions...)
	}

	return r
}

// Run executes the command line entrypoint given arguments and returns the process exit code.
// Exit code 0 denotes successful execution (including clean termination or --once completion).
// Exit code 1 denotes fatal configuration, initialization, authentication, or unrecoverable error.
func (r *Runner) Run(ctx context.Context, args []string) int {
	// 1. Setup FlagSet
	fs := flag.NewFlagSet("rl-sync", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var (
		configPath   string
		configShort  string
		once         bool
		dryRun       bool
		logLevel     string
		logFormat    string
		pollInterval time.Duration
		replayDir    string
		dbPath       string
		provider              string
		triggerThreshold      int
		forceSyncOnTrigger    bool
		statsAPIEnabled       bool
		playerTrackingEnabled bool
		localPlayerID         string
		localPlayerName       string
		autoFetchRanks        bool
		pollingAuthEnabled    bool
		pollingAuthProvider   string
		webHost               string
		webPort               int
		webEnabled            bool
		showVersion           bool
		showVersionV          bool
		showHelp              bool
		showHelpH             bool
	)

	fs.StringVar(&configPath, "config", "", "Path to configuration file (YAML or JSON)")
	fs.StringVar(&configShort, "c", "", "Path to configuration file (shorthand)")
	fs.BoolVar(&once, "once", false, "Execute a single synchronization cycle and exit")
	fs.BoolVar(&dryRun, "dry-run", false, "Simulate sync cycle without downloading or uploading replays")
	fs.StringVar(&logLevel, "log-level", "", "Logging level (debug, info, warn, error)")
	fs.StringVar(&logFormat, "log-format", "", "Logging format (text, json)")
	fs.DurationVar(&pollInterval, "poll-interval", 0, "Polling interval (e.g. 5m, 1m, 30s)")
	fs.StringVar(&replayDir, "replay-dir", "", "Directory to store downloaded replays")
	fs.StringVar(&dbPath, "db-path", "", "Path to SQLite database or JSON state store")
	fs.StringVar(&provider, "provider", "", "Authentication provider override ('epic' or 'steam')")
	fs.IntVar(&triggerThreshold, "trigger-threshold", 0, "Threshold of un-downloaded matches to fire notification/trigger (default 15)")
	fs.BoolVar(&forceSyncOnTrigger, "force-sync", false, "Force immediate PsyNet sync when trigger threshold is reached")
	fs.BoolVar(&statsAPIEnabled, "stats-api", true, "Enable Rocket League Stats API event tracking")
	fs.BoolVar(&playerTrackingEnabled, "player-tracking", true, "Enable player tracking and live lobby analysis")
	fs.StringVar(&localPlayerID, "local-player-id", "", "Override local player ID (e.g. 'Epic|<id>|0' or 'Steam|<id>|0')")
	fs.StringVar(&localPlayerName, "local-player-name", "", "Override local player display name")
	fs.BoolVar(&autoFetchRanks, "auto-fetch-ranks", true, "Automatically fetch competitive ranks via secondary account")
	fs.BoolVar(&pollingAuthEnabled, "polling-auth", false, "Enable secondary account authentication for rank retrieval")
	fs.StringVar(&pollingAuthProvider, "polling-provider", "", "Authentication provider for secondary account ('epic' or 'steam')")
	fs.StringVar(&webHost, "web-host", "", "HTTP host binding for web dashboard (default '0.0.0.0')")
	fs.IntVar(&webPort, "web-port", 0, "HTTP port binding for web dashboard (default 49125)")
	fs.BoolVar(&webEnabled, "web-enabled", true, "Enable embedded web dashboard and API server")
	fs.BoolVar(&showVersion, "version", false, "Display application version and exit")
	fs.BoolVar(&showVersionV, "v", false, "Display application version (shorthand)")
	fs.BoolVar(&showHelp, "help", false, "Display usage help and exit")
	fs.BoolVar(&showHelpH, "h", false, "Display usage help (shorthand)")

	fs.Usage = func() {
		fmt.Fprintf(r.Stdout, "Usage: rl-sync [flags]\n\nFlags:\n")
		fs.SetOutput(r.Stdout)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.Usage()
			return 0
		}
		fmt.Fprintf(r.Stderr, "%v\n", err)
		return 1
	}

	if showHelp || showHelpH {
		fs.Usage()
		return 0
	}

	if showVersion || showVersionV {
		fmt.Fprintf(r.Stdout, "rl-sync %s\n", Version)
		return 0
	}

	// 2. Build CLIFlags respecting explicit CLI invocation (CLI > Env > File > Defaults)
	cli := config.CLIFlags{}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "config":
			cli.ConfigPath = configPath
		case "c":
			if cli.ConfigPath == "" {
				cli.ConfigPath = configShort
			}
		case "once":
			cli.Once = &once
		case "dry-run":
			cli.DryRun = &dryRun
		case "log-level":
			cli.LogLevel = &logLevel
		case "log-format":
			cli.LogFormat = &logFormat
		case "poll-interval":
			cli.PollInterval = &pollInterval
		case "replay-dir":
			cli.ReplayDir = &replayDir
		case "db-path":
			cli.DBPath = &dbPath
		case "provider":
			cli.Provider = &provider
		case "trigger-threshold":
			cli.TriggerThreshold = &triggerThreshold
		case "force-sync":
			cli.ForceSyncOnTrigger = &forceSyncOnTrigger
		case "stats-api":
			cli.StatsAPIEnabled = &statsAPIEnabled
		case "player-tracking":
			cli.PlayerTrackingEnabled = &playerTrackingEnabled
		case "local-player-id":
			cli.LocalPlayerID = &localPlayerID
		case "local-player-name":
			cli.LocalPlayerName = &localPlayerName
		case "auto-fetch-ranks":
			cli.AutoFetchRanks = &autoFetchRanks
		case "polling-auth":
			cli.PollingAuthEnabled = &pollingAuthEnabled
		case "polling-provider":
			cli.PollingAuthProvider = &pollingAuthProvider
		case "web-host":
			cli.WebHost = &webHost
		case "web-port":
			cli.WebPort = &webPort
		case "web-enabled":
			cli.WebEnabled = &webEnabled
		}
	})

	// 3. Load & Validate Configuration
	cfg, err := r.LoadConfig(cli)
	if err != nil {
		fmt.Fprintf(r.Stderr, "Configuration error: %v\n", err)
		return 1
	}

	// 4. Initialize Structured Logger
	logger := daemon.NewLogger(cfg.Logging, r.Stderr)
	slog.SetDefault(logger)

	logger.Info("configuration loaded successfully",
		slog.String("auth_provider", cfg.Auth.Provider),
		slog.String("db_path", cfg.Sync.DBPath),
		slog.String("replay_dir", cfg.Sync.ReplayDir),
		slog.Duration("poll_interval", cfg.Sync.PollInterval.Duration()),
		slog.Bool("dry_run", cfg.Sync.DryRun),
		slog.Bool("once", cfg.Sync.Once),
		slog.String("log_level", cfg.Logging.Level),
		slog.String("log_format", cfg.Logging.Format),
		slog.Bool("web_enabled", cfg.Web.Enabled),
		slog.String("web_host", cfg.Web.Host),
		slog.Int("web_port", cfg.Web.Port),
	)

	// 5. Initialize Storage Backend (SQLite or JSON fallback)
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

	// 6. Initialize Auth Provider
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
		return 1
	}

	if err := authProvider.Validate(); err != nil {
		logger.Error("invalid authentication credentials", slog.Any("error", err))
		return 1
	}

	// Verify credentials immediately via initial authentication handshake
	logger.Info("authenticating credentials", slog.String("provider", authProvider.Name()))
	tokenInfo, err := authProvider.Authenticate(ctx)
	if err != nil {
		logger.Error("initial authentication failed",
			slog.String("provider", authProvider.Name()),
			slog.Any("error", err),
		)
		return 1
	}
	logger.Info("authenticated successfully",
		slog.String("provider", authProvider.Name()),
		slog.String("account_id", tokenInfo.AccountID),
		slog.String("display_name", tokenInfo.DisplayName),
	)

	// 7. Initialize PsyNet Client & Downloader
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

	// Remove stale temporary (.tmp-*) files from previous abnormal terminations
	if cleaned, cleanErr := psynet.CleanupStaleTempFiles(cfg.Sync.ReplayDir); cleanErr == nil && cleaned > 0 {
		logger.Info("cleaned stale temporary replay files", slog.Int("count", cleaned))
	}

	// 8. Initialize Ballchasing Client & Ping API Key
	bcClient, err := r.NewBallchasing(ballchasing.ClientConfig{
		BaseURL:      cfg.Ballchasing.BaseURL,
		APIKey:       cfg.Ballchasing.APIKey,
		Visibility:   cfg.Ballchasing.Visibility,
		MaxRetries:   cfg.Ballchasing.MaxRetries,
		Timeout:      cfg.Ballchasing.Timeout.Duration(),
		StreamUpload: false,
	})
	if err != nil {
		logger.Error("failed to initialize ballchasing client", slog.Any("error", err))
		return 1
	}

	logger.Info("verifying ballchasing api key")
	if err := bcClient.Ping(ctx); err != nil {
		logger.Error("ballchasing api key verification failed",
			slog.Any("error", err),
		)
		return 1
	}
	logger.Info("ballchasing api key verified successfully")

	// 9. Initialize Syncer Domain Engine
	syncerConfig := syncer.Config{
		ReplayDir:      cfg.Sync.ReplayDir,
		DryRun:         cfg.Sync.DryRun,
		KeepLocalFiles: cfg.Sync.KeepLocalFiles,
		Logger:         logger,
	}
	syncerEngine := r.NewSyncer(store, psyClient, downloader, bcClient, syncerConfig)

	// 10. Initialize Player Tracking Subsystem
	var playerTracker *playertrack.Tracker
	if cfg.PlayerTracking.Enabled {
		logger.Info("initializing player tracking subsystem",
			slog.Bool("auto_fetch_ranks", cfg.PlayerTracking.AutoFetchRanks),
			slog.String("local_player_id", cfg.PlayerTracking.LocalPlayerID),
			slog.String("local_player_name", cfg.PlayerTracking.LocalPlayerName),
			slog.Bool("polling_auth_enabled", cfg.PollingAuth.Enabled),
		)

		var rankClient playertrack.SkillFetcher = playertrack.NewNoOpRankClient()
		if cfg.PollingAuth.Enabled {
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
			} else {
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
				} else {
					rankClient = rc
				}
			}
		}

		defer func() {
			if closeErr := rankClient.Close(); closeErr != nil {
				logger.Warn("error closing rank client", slog.Any("error", closeErr))
			}
		}()

		var ptErr error
		playerTracker, ptErr = r.NewPlayerTracker(
			store,
			rankClient,
			cfg.PlayerTracking,
			cfg.Auth,
			playertrack.WithTrackerLogger(logger),
			playertrack.WithTrackerAuthProvider(authProvider),
		)
		if ptErr != nil {
			logger.Error("failed to initialize player tracker", slog.Any("error", ptErr))
			return 1
		}

		defer func() {
			logger.Info("closing player tracker")
			if closeErr := playerTracker.Close(); closeErr != nil {
				logger.Warn("error closing player tracker", slog.Any("error", closeErr))
			}
		}()
	}

	// 11. Initialize Daemon Engine
	var daemonOpts []daemon.Option
	daemonOpts = append(daemonOpts, daemon.WithLogger(logger))

	if playerTracker != nil {
		daemonOpts = append(daemonOpts,
			daemon.WithPlayerTracker(playerTracker),
			daemon.WithStateStore(store),
		)
	}

	if cfg.StatsAPI.Enabled && !cfg.Sync.Once {
		trackerCfg := statsapi.TrackerConfig{
			TriggerThreshold:   cfg.StatsAPI.TriggerThreshold,
			ForceSyncOnTrigger: cfg.StatsAPI.ForceSyncOnTrigger,
			EnableToast:        cfg.StatsAPI.EnableToast,
		}
		tracker, trackerErr := statsapi.NewTracker(store, trackerCfg, statsapi.WithLogger(logger))
		if trackerErr != nil {
			logger.Warn("failed to initialize stats tracker", slog.Any("error", trackerErr))
		} else {
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
			} else {
				if playerTracker != nil {
					listener.SetPlayerEventHandler(playerTracker)
				}
				daemonOpts = append(daemonOpts,
					daemon.WithStatsTracker(tracker),
					daemon.WithStatsListener(listener),
				)
			}
		}
	}

	daemonEngine, err := r.NewDaemon(syncerEngine, cfg, daemonOpts...)
	if err != nil {
		logger.Error("failed to initialize daemon engine", slog.Any("error", err))
		return 1
	}

	if cfg.Web.Enabled && !cfg.Sync.Once {
		lanIP, _ := daemon.DiscoverLANIPv4()
		banner := daemon.FormatStartupBanner(cfg.Web.Host, cfg.Web.Port, lanIP)
		logger.Info(banner)
	}

	// 11. Run Daemon and await completion / OS signal trap
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

// authSupplier bridges auth.AuthProvider into psynet.CredentialsSupplier.
type authSupplier struct {
	provider auth.AuthProvider
}

func (s *authSupplier) GetCredentials(ctx context.Context) (*psynet.Credentials, error) {
	token := s.provider.TokenInfo()
	if token == nil || token.IsExpired() {
		var err error
		token, err = s.provider.Authenticate(ctx)
		if err != nil {
			return nil, fmt.Errorf("authenticating with %s: %w", s.provider.Name(), err)
		}
	}

	platform := "Epic"
	if strings.EqualFold(s.provider.Name(), "steam") {
		platform = "Steam"
	}

	creds := &psynet.Credentials{
		Platform:    platform,
		AuthToken:   token.AccessToken,
		AccountID:   token.EpicAccountID,
		DisplayName: token.DisplayName,
	}
	if platform == "Steam" {
		creds.SteamAccountID = token.AccountID // in steam auth, AccountID is SteamID64
	}
	if creds.AccountID == "" {
		creds.AccountID = token.AccountID
	}

	return creds, nil
}
