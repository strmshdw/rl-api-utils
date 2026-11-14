package main

import (
	"bytes"
	"context"
	"io"
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
