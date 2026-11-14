package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/auth"
	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/storage"
)

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

func TestCLI_SyncBallchasingHistory_Flag(t *testing.T) {
	bcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/" {
			_ = json.NewEncoder(w).Encode(ballchasing.PingResponse{
				Name:    "HeroPlayer",
				SteamID: "76561198000000001",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(ballchasing.ReplayListResponse{
			Count: 0,
			List:  []ballchasing.ReplayItem{},
		})
	}))
	defer bcSrv.Close()

	runner := createTestRunner(io.Discard, io.Discard)
	runner.NewBallchasing = func(cfg ballchasing.ClientConfig) (ballchasing.ReplayUploader, error) {
		return ballchasing.NewClient(ballchasing.ClientConfig{
			BaseURL: bcSrv.URL,
			APIKey:  "dummy-key",
		})
	}

	daemonStarted := false
	runner.NewDaemon = func(s daemon.Syncer, cfg *config.Config, opts ...daemon.Option) (DaemonEngine, error) {
		daemonStarted = true
		return &mockDaemonEngine{}, nil
	}

	exitCode := runner.Run(context.Background(), []string{"--sync-ballchasing-history"})
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if daemonStarted {
		t.Error("expected daemon not to start when --sync-ballchasing-history flag is passed")
	}
}

func TestCLI_NewDownloader_EnforcesUE3Validation_And_RunnerDoesNotMutateAuthOptions(t *testing.T) {
	runner := createTestRunner(io.Discard, io.Discard)
	// Restore production NewDownloader factory to test default UE3 validation wiring
	prodRunner := NewDefaultRunner(io.Discard, io.Discard)
	dl := prodRunner.NewDownloader(5 * time.Second)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bytes.Repeat([]byte("NOT_A_VALID_UE3_REPLAY"), 80))
	}))
	defer srv.Close()

	if _, err := dl.DownloadReplay(context.Background(), "guid-ue3-check", srv.URL, t.TempDir()); err == nil {
		t.Fatal("expected NewDownloader with WithValidateReplay(true) to reject non-UE3 payload")
	}

	runner.AuthOptions = []auth.Option{auth.WithEGSClient(nil)}
	for i := 0; i < 2; i++ {
		if code := runner.Run(context.Background(), []string{"--once"}); code != 0 {
			t.Fatalf("run %d exited with %d", i, code)
		}
		if len(runner.AuthOptions) != 1 {
			t.Fatalf("run %d mutated runner.AuthOptions len to %d, want 1", i, len(runner.AuthOptions))
		}
	}
}
