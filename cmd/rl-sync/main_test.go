package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/auth"
	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/psynet"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/syncer"
)

// mockDaemonEngine implements DaemonEngine for testing.
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
	fresh := &auth.TokenInfo{
		Provider:      m.Name(),
		AccessToken:   "mock-access-token",
		RefreshToken:  "mock-refresh-token",
		AccountID:     "mock-acc-id",
		EpicAccountID: "mock-epic-id",
		DisplayName:   "MockPlayer",
		ExpiresAt:     time.Now().Add(1 * time.Hour),
	}
	m.tokenInfo = fresh
	return fresh, nil
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
}

func (m *mockSyncerEngine) RunCycle(ctx context.Context) (*syncer.SyncStats, error) {
	m.runCount++
	return &syncer.SyncStats{DiscoveredCount: 1, UploadedCount: 1}, nil
}

// createTestRunner creates a Runner with mocked subcomponents for rapid testing.
func createTestRunner(stdout, stderr io.Writer) *Runner {
	r := NewDefaultRunner(stdout, stderr)

	r.LoadConfig = func(cli config.CLIFlags) (*config.Config, error) {
		cfg := config.NewDefaultConfig()
		cfg.Ballchasing.APIKey = "test-api-key"
		cfg.Auth.Epic.RefreshToken = "test-refresh-token"
		cfg.Sync.Once = true
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
	}

	r.NewStore = func(path string) (storage.StateStore, error) {
		return storage.NewSQLiteStore(":memory:")
	}

	r.NewAuth = func(cfg config.AuthConfig, store storage.StateStore) (auth.AuthProvider, error) {
		return &mockAuthProvider{name: "epic"}, nil
	}

	r.NewPsyNet = func(cfg psynet.ClientConfig) (psynet.MatchHistoryProvider, error) {
		return &mockMatchHistoryProvider{}, nil
	}

	r.NewDownloader = func(timeout time.Duration) psynet.ReplayDownloader {
		return &mockDownloader{}
	}

	r.NewBallchasing = func(cfg ballchasing.ClientConfig) (ballchasing.ReplayUploader, error) {
		return &mockBallchasingUploader{}, nil
	}

	r.NewSyncer = func(store storage.StateStore, p psynet.MatchHistoryProvider, d psynet.ReplayDownloader, u ballchasing.ReplayUploader, cfg syncer.Config) daemon.Syncer {
		return &mockSyncerEngine{}
	}

	r.NewDaemon = func(s daemon.Syncer, cfg *config.Config, opts ...daemon.Option) (DaemonEngine, error) {
		return &mockDaemonEngine{
			startFunc: func(ctx context.Context) error {
				_, err := s.RunCycle(ctx)
				return err
			},
		}, nil
	}

	r.NewPollingAuth = func(cfg config.PollingAuthConfig, opts ...auth.Option) (auth.AuthProvider, error) {
		return &mockAuthProvider{name: cfg.Provider}, nil
	}

	r.NewRankClient = func(cfg playertrack.RankClientConfig) (playertrack.SkillFetcher, error) {
		return playertrack.NewMockSkillFetcher(), nil
	}

	r.NewPlayerTracker = func(store storage.StateStore, rankClient playertrack.SkillFetcher, cfg config.PlayerTrackingConfig, authCfg config.AuthConfig, opts ...playertrack.TrackerOption) (*playertrack.Tracker, error) {
		return playertrack.NewTracker(store, rankClient, cfg, authCfg, opts...)
	}

	return r
}

func TestCLI_Flags_Help(t *testing.T) {
	for _, flagArg := range []string{"--help", "-h"} {
		t.Run(flagArg, func(t *testing.T) {
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}

			runner := createTestRunner(stdout, stderr)
			exitCode := runner.Run(context.Background(), []string{flagArg})

			if exitCode != 0 {
				t.Fatalf("expected exit code 0 for %s, got %d", flagArg, exitCode)
			}
			out := stdout.String() + stderr.String()
			if !strings.Contains(out, "Usage: rl-sync") || !strings.Contains(out, "-once") {
				t.Fatalf("expected usage text containing '-once', got: %s", out)
			}
		})
	}
}

func TestCLI_Flags_Version(t *testing.T) {
	for _, flagArg := range []string{"--version", "-v"} {
		t.Run(flagArg, func(t *testing.T) {
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}

			runner := createTestRunner(stdout, stderr)
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

func TestCLI_Flags_UnknownFlag(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	runner := createTestRunner(stdout, stderr)
	exitCode := runner.Run(context.Background(), []string{"--unknown-flag-xyz"})

	if exitCode != 1 {
		t.Fatalf("expected exit code 1 for unknown flag, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined") {
		t.Fatalf("expected flag error in stderr, got: %s", stderr.String())
	}
}

func TestCLI_Flags_ConfigShorthand(t *testing.T) {
	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "custom.yaml")
	_ = os.WriteFile(confPath, []byte("ballchasing:\n  api_key: custom-shorthand-key\n"), 0644)

	var loadedPath string
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	runner := createTestRunner(stdout, stderr)
	runner.LoadConfig = func(cli config.CLIFlags) (*config.Config, error) {
		loadedPath = cli.ConfigPath
		cfg := config.NewDefaultConfig()
		cfg.Ballchasing.APIKey = "custom-shorthand-key"
		cfg.Auth.Epic.RefreshToken = "token"
		cfg.Sync.Once = true
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

func TestCLI_Flags_Precedence_AllFlags(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	var receivedFlags config.CLIFlags
	runner := createTestRunner(stdout, stderr)
	runner.LoadConfig = func(cli config.CLIFlags) (*config.Config, error) {
		receivedFlags = cli
		cfg := config.NewDefaultConfig()
		cfg.Ballchasing.APIKey = "key"
		cfg.Auth.Epic.RefreshToken = "token"
		cfg.Sync.Once = true
		return cfg, nil
	}

	args := []string{
		"--config=/tmp/cfg.yaml",
		"--dry-run",
		"--once",
		"--log-level=debug",
		"--log-format=json",
		"--poll-interval=10s",
		"--replay-dir=/tmp/replays",
		"--db-path=/tmp/test.db",
		"--provider=steam",
	}

	exitCode := runner.Run(context.Background(), args)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d. stderr: %s", exitCode, stderr.String())
	}

	if receivedFlags.ConfigPath != "/tmp/cfg.yaml" {
		t.Errorf("ConfigPath mismatch: %s", receivedFlags.ConfigPath)
	}
	if receivedFlags.DryRun == nil || !*receivedFlags.DryRun {
		t.Errorf("DryRun not set to true")
	}
	if receivedFlags.Once == nil || !*receivedFlags.Once {
		t.Errorf("Once not set to true")
	}
	if receivedFlags.LogLevel == nil || *receivedFlags.LogLevel != "debug" {
		t.Errorf("LogLevel mismatch")
	}
	if receivedFlags.LogFormat == nil || *receivedFlags.LogFormat != "json" {
		t.Errorf("LogFormat mismatch")
	}
	if receivedFlags.PollInterval == nil || *receivedFlags.PollInterval != 10*time.Second {
		t.Errorf("PollInterval mismatch")
	}
	if receivedFlags.ReplayDir == nil || *receivedFlags.ReplayDir != "/tmp/replays" {
		t.Errorf("ReplayDir mismatch")
	}
	if receivedFlags.DBPath == nil || *receivedFlags.DBPath != "/tmp/test.db" {
		t.Errorf("DBPath mismatch")
	}
	if receivedFlags.Provider == nil || *receivedFlags.Provider != "steam" {
		t.Errorf("Provider mismatch")
	}
}

func TestCLI_Run_ConfigLoadError(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	runner := createTestRunner(stdout, stderr)
	runner.LoadConfig = func(cli config.CLIFlags) (*config.Config, error) {
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

func TestCLI_Run_StoreInitError(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	runner := createTestRunner(stdout, stderr)
	runner.NewStore = func(path string) (storage.StateStore, error) {
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

func TestCLI_Run_AuthValidateError(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	runner := createTestRunner(stdout, stderr)
	runner.NewAuth = func(cfg config.AuthConfig, store storage.StateStore) (auth.AuthProvider, error) {
		return &mockAuthProvider{valErr: errors.New("missing refresh token")}, nil
	}

	exitCode := runner.Run(context.Background(), []string{"--once"})
	if exitCode != 1 {
		t.Fatalf("expected exit code 1 on auth validation error, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "invalid authentication credentials") {
		t.Fatalf("expected auth validation error in stderr, got: %s", stderr.String())
	}
}

func TestCLI_Run_AuthAuthenticateError(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	runner := createTestRunner(stdout, stderr)
	runner.NewAuth = func(cfg config.AuthConfig, store storage.StateStore) (auth.AuthProvider, error) {
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

func TestCLI_Run_PsyNetInitError(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	runner := createTestRunner(stdout, stderr)
	runner.NewPsyNet = func(cfg psynet.ClientConfig) (psynet.MatchHistoryProvider, error) {
		return nil, errors.New("cannot create psynet client")
	}

	exitCode := runner.Run(context.Background(), []string{"--once"})
	if exitCode != 1 {
		t.Fatalf("expected exit code 1 on psynet error, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "failed to initialize psynet client") {
		t.Fatalf("expected psynet client error in stderr, got: %s", stderr.String())
	}
}

func TestCLI_Run_BallchasingInitError(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	runner := createTestRunner(stdout, stderr)
	runner.NewBallchasing = func(cfg ballchasing.ClientConfig) (ballchasing.ReplayUploader, error) {
		return nil, errors.New("invalid visibility option")
	}

	exitCode := runner.Run(context.Background(), []string{"--once"})
	if exitCode != 1 {
		t.Fatalf("expected exit code 1 on ballchasing client error, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "failed to initialize ballchasing client") {
		t.Fatalf("expected ballchasing client error in stderr, got: %s", stderr.String())
	}
}

func TestCLI_Run_BallchasingPingFailure(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	runner := createTestRunner(stdout, stderr)
	runner.NewBallchasing = func(cfg ballchasing.ClientConfig) (ballchasing.ReplayUploader, error) {
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

func TestCLI_Run_DryRun_Propagation(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	var receivedSyncerCfg syncer.Config
	runner := createTestRunner(stdout, stderr)
	runner.NewSyncer = func(store storage.StateStore, p psynet.MatchHistoryProvider, d psynet.ReplayDownloader, u ballchasing.ReplayUploader, cfg syncer.Config) daemon.Syncer {
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

func TestCLI_Run_OnceMode_Success(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	mockSyncer := &mockSyncerEngine{}
	runner := createTestRunner(stdout, stderr)
	runner.NewSyncer = func(store storage.StateStore, p psynet.MatchHistoryProvider, d psynet.ReplayDownloader, u ballchasing.ReplayUploader, cfg syncer.Config) daemon.Syncer {
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

func TestCLI_Run_ContextCancellation(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-canceled context

	runner := createTestRunner(stdout, stderr)
	runner.NewDaemon = func(s daemon.Syncer, cfg *config.Config, opts ...daemon.Option) (DaemonEngine, error) {
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

func TestCLI_Run_DaemonError(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	runner := createTestRunner(stdout, stderr)
	runner.NewDaemon = func(s daemon.Syncer, cfg *config.Config, opts ...daemon.Option) (DaemonEngine, error) {
		return &mockDaemonEngine{
			startFunc: func(c context.Context) error {
				return errors.New("daemon fatal failure")
			},
		}, nil
	}

	exitCode := runner.Run(context.Background(), []string{})
	if exitCode != 1 {
		t.Fatalf("expected exit code 1 on daemon error, got %d", exitCode)
	}
}

func TestCLI_Run_RealDaemonIntegration(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	runner := createTestRunner(stdout, stderr)
	// Wire the real daemon constructor
	runner.NewDaemon = func(s daemon.Syncer, cfg *config.Config, opts ...daemon.Option) (DaemonEngine, error) {
		return daemon.New(s, cfg, opts...)
	}

	exitCode := runner.Run(context.Background(), []string{"--once"})
	if exitCode != 0 {
		t.Fatalf("expected exit code 0 with real daemon in once mode, got %d, stderr: %s", exitCode, stderr.String())
	}
}

func TestCLI_AuthSupplier_EpicAndSteam(t *testing.T) {
	ctx := context.Background()

	// 1. Epic provider
	epicProvider := &mockAuthProvider{
		name: "epic",
		tokenInfo: &auth.TokenInfo{
			Provider:      "epic",
			AccessToken:   "epic-access-tok",
			AccountID:     "epic-acc-1",
			EpicAccountID: "epic-acc-1",
			DisplayName:   "EpicGamer",
			ExpiresAt:     time.Now().Add(10 * time.Minute),
		},
	}
	supplier := &authSupplier{provider: epicProvider}
	creds, err := supplier.GetCredentials(ctx)
	if err != nil {
		t.Fatalf("unexpected GetCredentials error: %v", err)
	}
	if creds.Platform != "Epic" || creds.AuthToken != "epic-access-tok" || creds.AccountID != "epic-acc-1" {
		t.Fatalf("unexpected epic creds: %+v", creds)
	}

	// 2. Steam provider
	steamProvider := &mockAuthProvider{
		name: "steam",
		tokenInfo: &auth.TokenInfo{
			Provider:      "steam",
			AccessToken:   "steam-eos-tok",
			AccountID:     "76561198000000000",
			EpicAccountID: "steam-eos-acc",
			DisplayName:   "SteamGamer",
			ExpiresAt:     time.Now().Add(10 * time.Minute),
		},
	}
	supplierSteam := &authSupplier{provider: steamProvider}
	credsSteam, err := supplierSteam.GetCredentials(ctx)
	if err != nil {
		t.Fatalf("unexpected GetCredentials error: %v", err)
	}
	if credsSteam.Platform != "Steam" || credsSteam.SteamAccountID != "76561198000000000" {
		t.Fatalf("unexpected steam creds: %+v", credsSteam)
	}

	// 3. Expired token triggers re-authentication
	expiredProvider := &mockAuthProvider{
		name: "epic",
		tokenInfo: &auth.TokenInfo{
			Provider:    "epic",
			AccessToken: "expired-token",
			ExpiresAt:   time.Now().Add(-10 * time.Minute),
		},
	}
	supplierExpired := &authSupplier{provider: expiredProvider}
	credsExpired, err := supplierExpired.GetCredentials(ctx)
	if err != nil {
		t.Fatalf("unexpected GetCredentials error on expired: %v", err)
	}
	if credsExpired.AuthToken != "mock-access-token" {
		t.Fatalf("expected fresh token, got %s", credsExpired.AuthToken)
	}
}

func TestCLI_NewDefaultRunner_Constructors(t *testing.T) {
	r := NewDefaultRunner(nil, nil)
	if r.Stdout == nil || r.Stderr == nil {
		t.Fatal("expected stdout and stderr to default to standard streams")
	}
	if r.LoadConfig == nil || r.NewStore == nil || r.NewAuth == nil || r.NewPsyNet == nil ||
		r.NewDownloader == nil || r.NewBallchasing == nil || r.NewSyncer == nil || r.NewDaemon == nil ||
		r.NewPollingAuth == nil || r.NewRankClient == nil || r.NewPlayerTracker == nil {
		t.Fatal("expected all factory functions to be non-nil")
	}
}

func TestCLI_PlayerTracking_Flags_Precedence(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	var receivedFlags config.CLIFlags
	runner := createTestRunner(stdout, stderr)
	runner.LoadConfig = func(cli config.CLIFlags) (*config.Config, error) {
		receivedFlags = cli
		cfg := config.NewDefaultConfig()
		cfg.Ballchasing.APIKey = "key"
		cfg.Auth.Epic.RefreshToken = "token"
		cfg.Sync.Once = true
		return cfg, nil
	}

	args := []string{
		"--once",
		"--player-tracking=true",
		"--local-player-id=Steam|76561198000000000|0",
		"--local-player-name=CustomGamer",
		"--auto-fetch-ranks=false",
		"--polling-auth=true",
		"--polling-provider=epic",
	}

	exitCode := runner.Run(context.Background(), args)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d. stderr: %s", exitCode, stderr.String())
	}

	if receivedFlags.PlayerTrackingEnabled == nil || !*receivedFlags.PlayerTrackingEnabled {
		t.Errorf("PlayerTrackingEnabled flag not parsed")
	}
	if receivedFlags.LocalPlayerID == nil || *receivedFlags.LocalPlayerID != "Steam|76561198000000000|0" {
		t.Errorf("LocalPlayerID mismatch: %v", receivedFlags.LocalPlayerID)
	}
	if receivedFlags.LocalPlayerName == nil || *receivedFlags.LocalPlayerName != "CustomGamer" {
		t.Errorf("LocalPlayerName mismatch: %v", receivedFlags.LocalPlayerName)
	}
	if receivedFlags.AutoFetchRanks == nil || *receivedFlags.AutoFetchRanks != false {
		t.Errorf("AutoFetchRanks mismatch: %v", receivedFlags.AutoFetchRanks)
	}
	if receivedFlags.PollingAuthEnabled == nil || !*receivedFlags.PollingAuthEnabled {
		t.Errorf("PollingAuthEnabled flag not parsed")
	}
	if receivedFlags.PollingAuthProvider == nil || *receivedFlags.PollingAuthProvider != "epic" {
		t.Errorf("PollingAuthProvider mismatch: %v", receivedFlags.PollingAuthProvider)
	}
}

func TestCLI_Runner_PlayerTracking_And_PollingAuth_FullyEnabled(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	runner := createTestRunner(stdout, stderr)

	var pollingAuthInvoked bool
	var rankClientInvoked bool
	var trackerInvoked bool
	var daemonOptsCount int

	runner.LoadConfig = func(cli config.CLIFlags) (*config.Config, error) {
		cfg := config.NewDefaultConfig()
		cfg.Ballchasing.APIKey = "test-api-key"
		cfg.Auth.Epic.RefreshToken = "test-refresh-token"
		cfg.Sync.Once = true
		cfg.PlayerTracking.Enabled = true
		cfg.PollingAuth.Enabled = true
		cfg.PollingAuth.Provider = "epic"
		cfg.PollingAuth.Epic.RefreshToken = "polling-refresh-token"
		return cfg, nil
	}

	runner.NewPollingAuth = func(cfg config.PollingAuthConfig, opts ...auth.Option) (auth.AuthProvider, error) {
		pollingAuthInvoked = true
		if cfg.Provider != "epic" {
			t.Errorf("expected epic polling provider, got %s", cfg.Provider)
		}
		return &mockAuthProvider{name: "epic"}, nil
	}

	runner.NewRankClient = func(cfg playertrack.RankClientConfig) (playertrack.SkillFetcher, error) {
		rankClientInvoked = true
		return playertrack.NewMockSkillFetcher(), nil
	}

	runner.NewPlayerTracker = func(store storage.StateStore, rankClient playertrack.SkillFetcher, cfg config.PlayerTrackingConfig, authCfg config.AuthConfig, opts ...playertrack.TrackerOption) (*playertrack.Tracker, error) {
		trackerInvoked = true
		return playertrack.NewTracker(store, rankClient, cfg, authCfg, opts...)
	}

	runner.NewDaemon = func(s daemon.Syncer, cfg *config.Config, opts ...daemon.Option) (DaemonEngine, error) {
		daemonOptsCount = len(opts)
		return &mockDaemonEngine{}, nil
	}

	exitCode := runner.Run(context.Background(), []string{"--once"})
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d. stderr: %s", exitCode, stderr.String())
	}

	if !pollingAuthInvoked {
		t.Error("expected NewPollingAuth to be called")
	}
	if !rankClientInvoked {
		t.Error("expected NewRankClient to be called")
	}
	if !trackerInvoked {
		t.Error("expected NewPlayerTracker to be called")
	}
	// Verify daemon options received WithPlayerTracker and WithStateStore
	if daemonOptsCount < 2 {
		t.Errorf("expected at least 2 daemon options wired, got %d", daemonOptsCount)
	}
}

func TestCLI_Runner_PollingAuth_Disabled_FallbackToNoOp(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	runner := createTestRunner(stdout, stderr)

	var pollingAuthInvoked bool
	var rankClientPassed playertrack.SkillFetcher

	runner.LoadConfig = func(cli config.CLIFlags) (*config.Config, error) {
		cfg := config.NewDefaultConfig()
		cfg.Ballchasing.APIKey = "test-key"
		cfg.Auth.Epic.RefreshToken = "test-token"
		cfg.Sync.Once = true
		cfg.PlayerTracking.Enabled = true
		cfg.PollingAuth.Enabled = false // Polling disabled
		return cfg, nil
	}

	runner.NewPollingAuth = func(cfg config.PollingAuthConfig, opts ...auth.Option) (auth.AuthProvider, error) {
		pollingAuthInvoked = true
		return nil, errors.New("should not be called")
	}

	runner.NewPlayerTracker = func(store storage.StateStore, rankClient playertrack.SkillFetcher, cfg config.PlayerTrackingConfig, authCfg config.AuthConfig, opts ...playertrack.TrackerOption) (*playertrack.Tracker, error) {
		rankClientPassed = rankClient
		return playertrack.NewTracker(store, rankClient, cfg, authCfg, opts...)
	}

	exitCode := runner.Run(context.Background(), []string{"--once"})
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}

	if pollingAuthInvoked {
		t.Error("NewPollingAuth should NOT be invoked when PollingAuth.Enabled = false")
	}
	if rankClientPassed == nil {
		t.Fatal("rankClient was nil")
	}
	if _, isNoOp := rankClientPassed.(*playertrack.NoOpRankClient); !isNoOp {
		t.Errorf("expected NoOpRankClient fallback, got %T", rankClientPassed)
	}
}

func TestCLI_Runner_PollingAuth_Error_GracefulDegradation(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	runner := createTestRunner(stdout, stderr)

	var trackerInitialized bool

	runner.LoadConfig = func(cli config.CLIFlags) (*config.Config, error) {
		cfg := config.NewDefaultConfig()
		cfg.Ballchasing.APIKey = "test-key"
		cfg.Auth.Epic.RefreshToken = "test-token"
		cfg.Sync.Once = true
		cfg.PlayerTracking.Enabled = true
		cfg.PollingAuth.Enabled = true
		return cfg, nil
	}

	// Polling auth fails (e.g. invalid credentials or network error)
	runner.NewPollingAuth = func(cfg config.PollingAuthConfig, opts ...auth.Option) (auth.AuthProvider, error) {
		return nil, errors.New("polling auth service unreachable")
	}

	runner.NewPlayerTracker = func(store storage.StateStore, rankClient playertrack.SkillFetcher, cfg config.PlayerTrackingConfig, authCfg config.AuthConfig, opts ...playertrack.TrackerOption) (*playertrack.Tracker, error) {
		trackerInitialized = true
		// Verify fallback to NoOpRankClient
		if _, ok := rankClient.(*playertrack.NoOpRankClient); !ok {
			t.Errorf("expected NoOpRankClient, got %T", rankClient)
		}
		return playertrack.NewTracker(store, rankClient, cfg, authCfg, opts...)
	}

	// Must NOT return exit code 1!
	exitCode := runner.Run(context.Background(), []string{"--once"})
	if exitCode != 0 {
		t.Fatalf("expected exit code 0 on graceful degradation, got %d. stderr: %s", exitCode, stderr.String())
	}

	if !trackerInitialized {
		t.Error("expected PlayerTracker to initialize despite polling auth error")
	}
}

func TestCLI_Runner_PlayerTracking_Disabled_BackwardCompatibility(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	runner := createTestRunner(stdout, stderr)

	var trackerInvoked bool
	var daemonWiredWithTracker bool

	runner.LoadConfig = func(cli config.CLIFlags) (*config.Config, error) {
		cfg := config.NewDefaultConfig()
		cfg.Ballchasing.APIKey = "test-key"
		cfg.Auth.Epic.RefreshToken = "test-token"
		cfg.Sync.Once = true
		cfg.PlayerTracking.Enabled = false // Disabled
		return cfg, nil
	}

	runner.NewPlayerTracker = func(store storage.StateStore, rankClient playertrack.SkillFetcher, cfg config.PlayerTrackingConfig, authCfg config.AuthConfig, opts ...playertrack.TrackerOption) (*playertrack.Tracker, error) {
		trackerInvoked = true
		return nil, nil
	}

	runner.NewDaemon = func(s daemon.Syncer, cfg *config.Config, opts ...daemon.Option) (DaemonEngine, error) {
		d, err := daemon.New(s, cfg, opts...)
		if err != nil {
			return nil, err
		}
		// If handler is invoked, /current-match will report inactive
		rec := httptest.NewRecorder()
		d.Handler(context.Background()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/current-match", nil))
		if strings.Contains(rec.Body.String(), `"active_match":true`) {
			daemonWiredWithTracker = true
		}
		return &mockDaemonEngine{}, nil
	}

	exitCode := runner.Run(context.Background(), []string{"--once"})
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}

	if trackerInvoked {
		t.Error("NewPlayerTracker should NOT be called when PlayerTracking.Enabled = false")
	}
	if daemonWiredWithTracker {
		t.Error("daemon should not have active player tracking wired")
	}
}
