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
	"github.com/dank/rl-api-utils/internal/psynet"
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
	Stdout io.Writer
	Stderr io.Writer

	// Dependency injection hooks for unit testing
	LoadConfig     func(cli config.CLIFlags) (*config.Config, error)
	NewStore       func(path string) (storage.StateStore, error)
	NewAuth        func(cfg config.AuthConfig, store storage.StateStore) (auth.AuthProvider, error)
	NewPsyNet      func(cfg psynet.ClientConfig) (psynet.MatchHistoryProvider, error)
	NewDownloader  func(timeout time.Duration) psynet.ReplayDownloader
	NewBallchasing func(cfg ballchasing.ClientConfig) (ballchasing.ReplayUploader, error)
	NewSyncer      func(store storage.StateStore, p psynet.MatchHistoryProvider, d psynet.ReplayDownloader, u ballchasing.ReplayUploader, cfg syncer.Config) daemon.Syncer
	NewDaemon      func(s daemon.Syncer, cfg *config.Config, opts ...daemon.Option) (DaemonEngine, error)
}

// NewDefaultRunner constructs a Runner wired with production constructors.
func NewDefaultRunner(stdout, stderr io.Writer) *Runner {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}

	return &Runner{
		Stdout: stdout,
		Stderr: stderr,
		LoadConfig: func(cli config.CLIFlags) (*config.Config, error) {
			return config.Load(cli)
		},
		NewStore: func(path string) (storage.StateStore, error) {
			return storage.NewStore(path)
		},
		NewAuth: func(cfg config.AuthConfig, store storage.StateStore) (auth.AuthProvider, error) {
			return auth.NewProvider(cfg, store)
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
			return syncer.New(store, p, d, u, cfg)
		},
		NewDaemon: func(s daemon.Syncer, cfg *config.Config, opts ...daemon.Option) (DaemonEngine, error) {
			return daemon.New(s, cfg, opts...)
		},
	}
}

// Run executes the command line entrypoint given arguments and returns the process exit code.
// Exit code 0 denotes successful execution (including clean termination or --once completion).
// Exit code 1 denotes fatal configuration, initialization, authentication, or unrecoverable error.
func (r *Runner) Run(ctx context.Context, args []string) int {
	// 1. Setup FlagSet
	fs := flag.NewFlagSet("rl-sync", flag.ContinueOnError)
	fs.SetOutput(r.Stderr)

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
		provider     string
		showVersion  bool
		showVersionV bool
		showHelp     bool
		showHelpH    bool
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
	fs.BoolVar(&showVersion, "version", false, "Display application version and exit")
	fs.BoolVar(&showVersionV, "v", false, "Display application version (shorthand)")
	fs.BoolVar(&showHelp, "help", false, "Display usage help and exit")
	fs.BoolVar(&showHelpH, "h", false, "Display usage help (shorthand)")

	fs.Usage = func() {
		fmt.Fprintf(r.Stderr, "Usage: rl-sync [flags]\n\nFlags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}

	if showHelp || showHelpH {
		fs.SetOutput(r.Stdout)
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
		ReplayDir: cfg.Sync.ReplayDir,
		DryRun:    cfg.Sync.DryRun,
		Logger:    logger,
	}
	syncerEngine := r.NewSyncer(store, psyClient, downloader, bcClient, syncerConfig)

	// 10. Initialize Daemon Engine
	daemonEngine, err := r.NewDaemon(syncerEngine, cfg, daemon.WithLogger(logger))
	if err != nil {
		logger.Error("failed to initialize daemon engine", slog.Any("error", err))
		return 1
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
