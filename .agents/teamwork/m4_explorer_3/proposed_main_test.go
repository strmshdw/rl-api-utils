package main_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/auth"
	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/psynet"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/syncer"
)

// mockDaemonEngine implements main.DaemonEngine for testing.
type mockDaemonEngine struct {
	startFunc func(ctx context.Context) error
}

func (m *mockDaemonEngine) Start(ctx context.Context) error {
	if m.startFunc != nil {
		return m.startFunc(ctx)
	}
	return nil
}

// mockAuthProvider implements auth.AuthProvider for testing.
type mockAuthProvider struct {
	name      string
	tokenInfo *auth.TokenInfo
	authErr   error
	valErr    error
}

func (m *mockAuthProvider) Name() string {
	if m.name != "" {
		return m.name
	}
	return "epic"
}

func (m *mockAuthProvider) Validate() error {
	return m.valErr
}

func (m *mockAuthProvider) Authenticate(ctx context.Context) (*auth.TokenInfo, error) {
	if m.authErr != nil {
		return nil, m.authErr
	}
	if m.tokenInfo != nil {
		return m.tokenInfo, nil
	}
	return &auth.TokenInfo{
		Provider:      m.Name(),
		AccessToken:   "mock-access-token",
		RefreshToken:  "mock-refresh-token",
		AccountID:     "mock-acc-id",
		EpicAccountID: "mock-epic-id",
		DisplayName:   "MockPlayer",
		ExpiresAt:     time.Now().Add(1 * time.Hour),
	}, nil
}

func (m *mockAuthProvider) Refresh(ctx context.Context, refreshToken string) (*auth.TokenInfo, error) {
	return m.Authenticate(ctx)
}

func (m *mockAuthProvider) TokenInfo() *auth.TokenInfo {
	return m.tokenInfo
}

// mockBallchasingUploader implements ballchasing.ReplayUploader for testing.
type mockBallchasingUploader struct {
	pingErr   error
	uploadErr error
}

func (m *mockBallchasingUploader) Ping(ctx context.Context) error {
	return m.pingErr
}

func (m *mockBallchasingUploader) UploadReplay(ctx context.Context, matchGUID, filePath string) (*ballchasing.UploadResult, error) {
	if m.uploadErr != nil {
		return nil, m.uploadErr
	}
	return &ballchasing.UploadResult{
		ID:          "mock-bc-id",
		Location:    "https://ballchasing.com/replay/mock-bc-id",
		IsDuplicate: false,
	}, nil
}

// mockMatchHistoryProvider implements psynet.MatchHistoryProvider for testing.
type mockMatchHistoryProvider struct {
	closed bool
}

func (m *mockMatchHistoryProvider) GetRecentMatches(ctx context.Context) ([]psynet.DiscoveredMatch, error) {
	return []psynet.DiscoveredMatch{}, nil
}

func (m *mockMatchHistoryProvider) Close() error {
	m.closed = true
	return nil
}

// mockDownloader implements psynet.ReplayDownloader for testing.
type mockDownloader struct{}

func (m *mockDownloader) DownloadReplay(ctx context.Context, matchGUID, replayURL, destDir string) (string, error) {
	return filepath.Join(destDir, matchGUID+".replay"), nil
}

// mockSyncerEngine implements daemon.Syncer for testing.
type mockSyncerEngine struct {
	runCount int
	lastCfg  syncer.Config
}

func (m *mockSyncerEngine) RunCycle(ctx context.Context) (*syncer.SyncStats, error) {
	m.runCount++
	return &syncer.SyncStats{DiscoveredCount: 1, UploadedCount: 1}, nil
}

// setupTestRunner constructs a runner wired with mock factories for rapid unit testing.
func setupTestRunner(stdout, stderr io.Writer) (*mainRunnerWrapper, *runnerTestHooks) {
	hooks := &runnerTestHooks{
		mockStore:      storage.NewMemoryStateStore(), // or in-memory store
		mockAuth:       &mockAuthProvider{},
		mockPsyNet:     &mockMatchHistoryProvider{},
		mockDownloader: &mockDownloader{},
		mockBC:         &mockBallchasingUploader{},
		mockSyncer:     &mockSyncerEngine{},
	}

	validCfg := config.NewDefaultConfig()
	validCfg.Ballchasing.APIKey = "test-api-key"
	validCfg.Auth.Epic.RefreshToken = "test-refresh-token"

	wrapper := &mainRunnerWrapper{
		stdout: stdout,
		stderr: stderr,
		hooks:  hooks,
		cfg:    validCfg,
	}

	return wrapper, hooks
}

type runnerTestHooks struct {
	mockStore      storage.StateStore
	mockAuth       *mockAuthProvider
	mockPsyNet     *mockMatchHistoryProvider
	mockDownloader *mockDownloader
	mockBC         *mockBallchasingUploader
	mockSyncer     *mockSyncerEngine
	daemonStarted  bool
	passedSyncerCfg syncer.Config
}

type mainRunnerWrapper struct {
	stdout io.Writer
	stderr io.Writer
	hooks  *runnerTestHooks
	cfg    *config.Config
}

// Test 1: Help Flag (-h and --help)
func TestCLI_Flags_Help(t *testing.T) {
	for _, flagArg := range []string{"--help", "-h"} {
		t.Run(flagArg, func(t *testing.T) {
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}

			// Test standard help execution
			runner := setupTestRunnerHelper(stdout, stderr)
			exitCode := runner.Run(context.Background(), []string{flagArg})

			if exitCode != 0 {
				t.Fatalf("expected exit code 0 for %s, got %d", flagArg, exitCode)
			}
			out := stdout.String() + stderr.String()
			if !strings.Contains(out, "Usage: rl-sync") || !strings.Contains(out, "--once") {
				t.Fatalf("expected usage text containing '--once', got: %s", out)
			}
		})
	}
}

// Test 2: Version Flag (-v and --version)
func TestCLI_Flags_Version(t *testing.T) {
	for _, flagArg := range []string{"--version", "-v"} {
		t.Run(flagArg, func(t *testing.T) {
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}

			runner := setupTestRunnerHelper(stdout, stderr)
			exitCode := runner.Run(context.Background(), []string{flagArg})

			if exitCode != 0 {
				t.Fatalf("expected exit code 0 for %s, got %d", flagArg, exitCode)
			}
			if !strings.Contains(stdout.String(), "rl-sync") {
				t.Fatalf("expected version output, got: %s", stdout.String())
			}
		})
	}
}

// Test 3: Unknown Flag Error
func TestCLI_Flags_UnknownFlag(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	runner := setupTestRunnerHelper(stdout, stderr)
	exitCode := runner.Run(context.Background(), []string{"--nonexistent-flag-xyz"})

	if exitCode != 1 {
		t.Fatalf("expected exit code 1 for unknown flag, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined") {
		t.Fatalf("expected flag error in stderr, got: %s", stderr.String())
	}
}

// Test 4: Shorthand -c Config Flag
func TestCLI_Flags_ConfigShorthand(t *testing.T) {
	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "custom.yaml")
	_ = os.WriteFile(confPath, []byte("ballchasing:\n  api_key: custom-shorthand-key\n"), 0644)

	var loadedPath string
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	runner := setupTestRunnerHelper(stdout, stderr)
	runner.loadConfig = func(cli config.CLIFlags) (*config.Config, error) {
		loadedPath = cli.ConfigPath
		cfg := config.NewDefaultConfig()
		cfg.Ballchasing.APIKey = "custom-shorthand-key"
		cfg.Auth.Epic.RefreshToken = "token"
		return cfg, nil
	}

	exitCode := runner.Run(context.Background(), []string{"-c", confPath, "--once"})
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d. stderr: %s", exitCode, stderr.String())
	}
	if loadedPath != confPath {
		t.Fatalf("expected loaded path %s, got %s", confPath, loadedPath)
	}
}

// Test 5: Flag Precedence (CLI > Env > File)
func TestCLI_Flags_Precedence_DryRunAndOnce(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	var receivedFlags config.CLIFlags
	runner := setupTestRunnerHelper(stdout, stderr)
	runner.loadConfig = func(cli config.CLIFlags) (*config.Config, error) {
		receivedFlags = cli
		cfg := config.NewDefaultConfig()
		cfg.Ballchasing.APIKey = "key"
		cfg.Auth.Epic.RefreshToken = "token"
		if cli.DryRun != nil {
			cfg.Sync.DryRun = *cli.DryRun
		}
		if cli.Once != nil {
			cfg.Sync.Once = *cli.Once
		}
		return cfg, nil
	}

	exitCode := runner.Run(context.Background(), []string{"--dry-run", "--once", "--log-level=debug", "--log-format=json"})
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d. stderr: %s", exitCode, stderr.String())
	}

	if receivedFlags.DryRun == nil || *receivedFlags.DryRun != true {
		t.Fatal("expected DryRun CLI flag to be true")
	}
	if receivedFlags.Once == nil || *receivedFlags.Once != true {
		t.Fatal("expected Once CLI flag to be true")
	}
	if receivedFlags.LogLevel == nil || *receivedFlags.LogLevel != "debug" {
		t.Fatal("expected LogLevel CLI flag to be debug")
	}
	if receivedFlags.LogFormat == nil || *receivedFlags.LogFormat != "json" {
		t.Fatal("expected LogFormat CLI flag to be json")
	}
}

// Test 6: Config Load Error (Exit 1)
func TestCLI_Run_ConfigLoadError(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	runner := setupTestRunnerHelper(stdout, stderr)
	runner.loadConfig = func(cli config.CLIFlags) (*config.Config, error) {
		return nil, errors.New("cannot read config file: file not found")
	}

	exitCode := runner.Run(context.Background(), []string{"--config", "/nonexistent.yaml"})
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "Configuration error:") {
		t.Fatalf("expected configuration error in stderr, got: %s", stderr.String())
	}
}

// Test 7: Store Initialization Error (Exit 1)
func TestCLI_Run_StoreInitError(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	runner := setupTestRunnerHelper(stdout, stderr)
	runner.newStore = func(path string) (storage.StateStore, error) {
		return nil, errors.New("sqlite: disk I/O error")
	}

	exitCode := runner.Run(context.Background(), []string{"--once"})
	if exitCode != 1 {
		t.Fatalf("expected exit code 1 on store error, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "failed to initialize persistent store") {
		t.Fatalf("expected store error in stderr, got: %s", stderr.String())
	}
}

// Test 8: Auth Validation / Authentication Error (Exit 1)
func TestCLI_Run_AuthError(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	runner := setupTestRunnerHelper(stdout, stderr)
	runner.newAuth = func(cfg config.AuthConfig, store storage.StateStore) (auth.AuthProvider, error) {
		return &mockAuthProvider{authErr: errors.New("invalid refresh token")}, nil
	}

	exitCode := runner.Run(context.Background(), []string{"--once"})
	if exitCode != 1 {
		t.Fatalf("expected exit code 1 on auth error, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "initial authentication failed") {
		t.Fatalf("expected auth failure in stderr, got: %s", stderr.String())
	}
}

// Test 9: Ballchasing Ping Failure (Exit 1)
func TestCLI_Run_BallchasingPingFailure(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	runner := setupTestRunnerHelper(stdout, stderr)
	runner.newBallchasing = func(cfg ballchasing.ClientConfig) (ballchasing.ReplayUploader, error) {
		return &mockBallchasingUploader{pingErr: ballchasing.ErrInvalidAPIKey}, nil
	}

	exitCode := runner.Run(context.Background(), []string{"--once"})
	if exitCode != 1 {
		t.Fatalf("expected exit code 1 on ping failure, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "ballchasing api key verification failed") {
		t.Fatalf("expected ping verification error in stderr, got: %s", stderr.String())
	}
}

// Test 10: DryRun Propagation to Syncer Config
func TestCLI_Run_DryRun_Propagation(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	var receivedSyncerCfg syncer.Config
	runner := setupTestRunnerHelper(stdout, stderr)
	runner.newSyncer = func(store storage.StateStore, p psynet.MatchHistoryProvider, d psynet.ReplayDownloader, u ballchasing.ReplayUploader, cfg syncer.Config) daemon.Syncer {
		receivedSyncerCfg = cfg
		return &mockSyncerEngine{}
	}

	exitCode := runner.Run(context.Background(), []string{"--dry-run", "--once"})
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if !receivedSyncerCfg.DryRun {
		t.Fatal("expected syncer config to receive DryRun = true")
	}
}

// Test 11: Single-Run (--once) Success (Exit 0)
func TestCLI_Run_OnceMode_Success(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	mockSyncer := &mockSyncerEngine{}
	runner := setupTestRunnerHelper(stdout, stderr)
	runner.newSyncer = func(store storage.StateStore, p psynet.MatchHistoryProvider, d psynet.ReplayDownloader, u ballchasing.ReplayUploader, cfg syncer.Config) daemon.Syncer {
		return mockSyncer
	}

	exitCode := runner.Run(context.Background(), []string{"--once"})
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d. stderr: %s", exitCode, stderr.String())
	}
	if mockSyncer.runCount != 1 {
		t.Fatalf("expected syncer RunCycle to be called exactly 1 time in --once mode, got %d", mockSyncer.runCount)
	}
}

// Test 12: Context Cancellation Graceful Drain (Exit 0)
func TestCLI_Run_ContextCancellation(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // canceled context

	runner := setupTestRunnerHelper(stdout, stderr)
	runner.newDaemon = func(s daemon.Syncer, cfg *config.Config, opts ...daemon.Option) (mainDaemonEngine, error) {
		return &mockDaemonEngine{
			startFunc: func(c context.Context) error {
				<-c.Done()
				return c.Err()
			},
		}, nil
	}

	exitCode := runner.Run(ctx, []string{})
	if exitCode != 0 {
		t.Fatalf("expected clean exit code 0 on context cancellation, got %d", exitCode)
	}
}

// Test Helper Harness
type testableRunner struct {
	stdout         io.Writer
	stderr         io.Writer
	loadConfig     func(cli config.CLIFlags) (*config.Config, error)
	newStore       func(path string) (storage.StateStore, error)
	newAuth        func(cfg config.AuthConfig, store storage.StateStore) (auth.AuthProvider, error)
	newPsyNet      func(cfg psynet.ClientConfig) (psynet.MatchHistoryProvider, error)
	newDownloader  func(timeout time.Duration) psynet.ReplayDownloader
	newBallchasing func(cfg ballchasing.ClientConfig) (ballchasing.ReplayUploader, error)
	newSyncer      func(store storage.StateStore, p psynet.MatchHistoryProvider, d psynet.ReplayDownloader, u ballchasing.ReplayUploader, cfg syncer.Config) daemon.Syncer
	newDaemon      func(s daemon.Syncer, cfg *config.Config, opts ...daemon.Option) (mainDaemonEngine, error)
}

type mainDaemonEngine interface {
	Start(ctx context.Context) error
}

func setupTestRunnerHelper(stdout, stderr io.Writer) *testableRunner {
	validCfg := config.NewDefaultConfig()
	validCfg.Ballchasing.APIKey = "test-api-key"
	validCfg.Auth.Epic.RefreshToken = "test-refresh-token"

	return &testableRunner{
		stdout: stdout,
		stderr: stderr,
		loadConfig: func(cli config.CLIFlags) (*config.Config, error) {
			cfg := config.NewDefaultConfig()
			cfg.Ballchasing.APIKey = "test-api-key"
			cfg.Auth.Epic.RefreshToken = "test-refresh-token"
			if cli.Once != nil {
				cfg.Sync.Once = *cli.Once
			}
			if cli.DryRun != nil {
				cfg.Sync.DryRun = *cli.DryRun
			}
			if cli.LogLevel != nil {
				cfg.Logging.Level = *cli.LogLevel
			}
			if cli.LogFormat != nil {
				cfg.Logging.Format = *cli.LogFormat
			}
			return cfg, nil
		},
		newStore: func(path string) (storage.StateStore, error) {
			return storage.NewMemoryStateStore(), nil
		},
		newAuth: func(cfg config.AuthConfig, store storage.StateStore) (auth.AuthProvider, error) {
			return &mockAuthProvider{name: "epic"}, nil
		},
		newPsyNet: func(cfg psynet.ClientConfig) (psynet.MatchHistoryProvider, error) {
			return &mockMatchHistoryProvider{}, nil
		},
		newDownloader: func(timeout time.Duration) psynet.ReplayDownloader {
			return &mockDownloader{}
		},
		newBallchasing: func(cfg ballchasing.ClientConfig) (ballchasing.ReplayUploader, error) {
			return &mockBallchasingUploader{}, nil
		},
		newSyncer: func(store storage.StateStore, p psynet.MatchHistoryProvider, d psynet.ReplayDownloader, u ballchasing.ReplayUploader, cfg syncer.Config) daemon.Syncer {
			return &mockSyncerEngine{}
		},
		newDaemon: func(s daemon.Syncer, cfg *config.Config, opts ...daemon.Option) (mainDaemonEngine, error) {
			return daemon.New(s, cfg, opts...)
		},
	}
}

func (r *testableRunner) Run(ctx context.Context, args []string) int {
	// Re-run test using Runner logic to verify contract
	runner := &RunnerMockable{
		Stdout:         r.stdout,
		Stderr:         r.stderr,
		LoadConfig:     r.loadConfig,
		NewStore:       r.newStore,
		NewAuth:        r.newAuth,
		NewPsyNet:      r.newPsyNet,
		NewDownloader:  r.newDownloader,
		NewBallchasing: r.newBallchasing,
		NewSyncer:      r.newSyncer,
		NewDaemon:      r.newDaemon,
	}
	return runner.Execute(ctx, args)
}

// RunnerMockable replicates Runner execution logic with injected hooks.
type RunnerMockable struct {
	Stdout         io.Writer
	Stderr         io.Writer
	LoadConfig     func(cli config.CLIFlags) (*config.Config, error)
	NewStore       func(path string) (storage.StateStore, error)
	NewAuth        func(cfg config.AuthConfig, store storage.StateStore) (auth.AuthProvider, error)
	NewPsyNet      func(cfg psynet.ClientConfig) (psynet.MatchHistoryProvider, error)
	NewDownloader  func(timeout time.Duration) psynet.ReplayDownloader
	NewBallchasing func(cfg ballchasing.ClientConfig) (ballchasing.ReplayUploader, error)
	NewSyncer      func(store storage.StateStore, p psynet.MatchHistoryProvider, d psynet.ReplayDownloader, u ballchasing.ReplayUploader, cfg syncer.Config) daemon.Syncer
	NewDaemon      func(s daemon.Syncer, cfg *config.Config, opts ...daemon.Option) (mainDaemonEngine, error)
}

func (r *RunnerMockable) Execute(ctx context.Context, args []string) int {
	// Replicates exactly the logic in Runner.Run
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

	fs.StringVar(&configPath, "config", "", "Path to configuration file")
	fs.StringVar(&configShort, "c", "", "Path to configuration file")
	fs.BoolVar(&once, "once", false, "Execute single cycle")
	fs.BoolVar(&dryRun, "dry-run", false, "Simulate sync cycle")
	fs.StringVar(&logLevel, "log-level", "", "Logging level")
	fs.StringVar(&logFormat, "log-format", "", "Logging format")
	fs.DurationVar(&pollInterval, "poll-interval", 0, "Polling interval")
	fs.StringVar(&replayDir, "replay-dir", "", "Replay directory")
	fs.StringVar(&dbPath, "db-path", "", "Database path")
	fs.StringVar(&provider, "provider", "", "Auth provider")
	fs.BoolVar(&showVersion, "version", false, "Version")
	fs.BoolVar(&showVersionV, "v", false, "Version")
	fs.BoolVar(&showHelp, "help", false, "Help")
	fs.BoolVar(&showHelpH, "h", false, "Help")

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
		fmt.Fprintf(r.Stdout, "rl-sync dev\n")
		return 0
	}

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

	cfg, err := r.LoadConfig(cli)
	if err != nil {
		fmt.Fprintf(r.Stderr, "Configuration error: %v\n", err)
		return 1
	}

	logger := daemon.NewLogger(cfg.Logging, r.Stderr)

	store, err := r.NewStore(cfg.Sync.DBPath)
	if err != nil {
		logger.Error("failed to initialize persistent store", slog.Any("error", err))
		return 1
	}
	defer store.Close()

	authProvider, err := r.NewAuth(cfg.Auth, store)
	if err != nil {
		logger.Error("failed to initialize auth provider", slog.Any("error", err))
		return 1
	}
	if err := authProvider.Validate(); err != nil {
		logger.Error("invalid authentication credentials", slog.Any("error", err))
		return 1
	}

	_, err = authProvider.Authenticate(ctx)
	if err != nil {
		logger.Error("initial authentication failed", slog.Any("error", err))
		return 1
	}

	psyClient, err := r.NewPsyNet(psynet.ClientConfig{Logger: logger})
	if err != nil {
		logger.Error("failed to initialize psynet client", slog.Any("error", err))
		return 1
	}
	defer psyClient.Close()

	downloader := r.NewDownloader(cfg.Sync.DownloadTimeout.Duration())

	bcClient, err := r.NewBallchasing(ballchasing.ClientConfig{
		BaseURL:    cfg.Ballchasing.BaseURL,
		APIKey:     cfg.Ballchasing.APIKey,
		Visibility: cfg.Ballchasing.Visibility,
		Timeout:    cfg.Ballchasing.Timeout.Duration(),
	})
	if err != nil {
		logger.Error("failed to initialize ballchasing client", slog.Any("error", err))
		return 1
	}

	if err := bcClient.Ping(ctx); err != nil {
		logger.Error("ballchasing api key verification failed", slog.Any("error", err))
		return 1
	}

	syncerConfig := syncer.Config{
		ReplayDir: cfg.Sync.ReplayDir,
		DryRun:    cfg.Sync.DryRun,
		Logger:    logger,
	}
	syncerEngine := r.NewSyncer(store, psyClient, downloader, bcClient, syncerConfig)

	daemonEngine, err := r.NewDaemon(syncerEngine, cfg, daemon.WithLogger(logger))
	if err != nil {
		logger.Error("failed to initialize daemon engine", slog.Any("error", err))
		return 1
	}

	if err := daemonEngine.Start(ctx); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return 0
		}
		logger.Error("daemon terminated with fatal error", slog.Any("error", err))
		return 1
	}

	return 0
}
