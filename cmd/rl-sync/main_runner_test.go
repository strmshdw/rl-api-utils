package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
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
