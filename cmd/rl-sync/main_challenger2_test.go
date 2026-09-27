package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dank/rl-api-utils/internal/auth"
	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/storage"
)

// TestChallenger2_CLI_FlagOverrides_Precedence tests comprehensive CLI flag overrides
// across config files, environment variables, and defaults for Player Tracking and Polling Auth.
func TestChallenger2_CLI_FlagOverrides_Precedence(t *testing.T) {
	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "config.yaml")
	fileContent := `
ballchasing:
  api_key: "test-api-key"
auth:
  provider: "epic"
  epic:
    refresh_token: "test-primary-token"
player_tracking:
  enabled: true
  auto_fetch_ranks: true
  local_player_id: "Epic|from_file_id|0"
  local_player_name: "FromFileGamer"
polling_auth:
  enabled: true
  provider: "epic"
  epic:
    refresh_token: "test-polling-token"
`
	if err := os.WriteFile(confPath, []byte(fileContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// Subtest 1: Verify CLI flags override file configuration (e.g. disabling player tracking via CLI)
	t.Run("CLI_Overrides_ConfigFile_DisablePlayerTracking", func(t *testing.T) {
		var receivedFlags config.CLIFlags
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}

		runner := createTestRunner(stdout, stderr)
		runner.LoadConfig = func(cli config.CLIFlags) (*config.Config, error) {
			receivedFlags = cli
			return config.Load(cli)
		}

		args := []string{
			"--config=" + confPath,
			"--player-tracking=false",
			"--once",
		}

		code := runner.Run(context.Background(), args)
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d. stderr: %s", code, stderr.String())
		}

		if receivedFlags.PlayerTrackingEnabled == nil || *receivedFlags.PlayerTrackingEnabled != false {
			t.Fatalf("expected CLI flag PlayerTrackingEnabled to be false, got %v", receivedFlags.PlayerTrackingEnabled)
		}
	})

	// Subtest 2: Verify CLI overrides player ID, name, auto-fetch-ranks, and polling-provider
	t.Run("CLI_Overrides_PlayerIdentity_And_PollingProvider", func(t *testing.T) {
		var capturedCfg *config.Config
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}

		runner := createTestRunner(stdout, stderr)
		origLoad := runner.LoadConfig
		runner.LoadConfig = func(cli config.CLIFlags) (*config.Config, error) {
			cfg, err := origLoad(cli)
			if err != nil {
				return nil, err
			}
			// Apply CLI overrides as done during full config.Load
			if cli.LocalPlayerID != nil {
				cfg.PlayerTracking.LocalPlayerID = *cli.LocalPlayerID
			}
			if cli.LocalPlayerName != nil {
				cfg.PlayerTracking.LocalPlayerName = *cli.LocalPlayerName
			}
			if cli.AutoFetchRanks != nil {
				cfg.PlayerTracking.AutoFetchRanks = *cli.AutoFetchRanks
			}
			if cli.PollingAuthProvider != nil {
				cfg.PollingAuth.Provider = *cli.PollingAuthProvider
			}
			capturedCfg = cfg
			return cfg, nil
		}

		args := []string{
			"--config=" + confPath,
			"--local-player-id=Steam|76561198099999999|0",
			"--local-player-name=CLIChampion",
			"--auto-fetch-ranks=false",
			"--polling-provider=steam",
			"--once",
		}

		code := runner.Run(context.Background(), args)
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d. stderr: %s", code, stderr.String())
		}

		if capturedCfg == nil {
			t.Fatal("expected config to be loaded")
		}
		if capturedCfg.PlayerTracking.LocalPlayerID != "Steam|76561198099999999|0" {
			t.Errorf("expected local player id override, got %q", capturedCfg.PlayerTracking.LocalPlayerID)
		}
		if capturedCfg.PlayerTracking.LocalPlayerName != "CLIChampion" {
			t.Errorf("expected local player name override, got %q", capturedCfg.PlayerTracking.LocalPlayerName)
		}
		if capturedCfg.PlayerTracking.AutoFetchRanks != false {
			t.Errorf("expected auto fetch ranks to be false, got %v", capturedCfg.PlayerTracking.AutoFetchRanks)
		}
		if capturedCfg.PollingAuth.Provider != "steam" {
			t.Errorf("expected polling provider override to 'steam', got %q", capturedCfg.PollingAuth.Provider)
		}
	})
}

// mockCloseErrRankClient wraps MockSkillFetcher to simulate a Close error.
type mockCloseErrRankClient struct {
	*playertrack.MockSkillFetcher
	closeErr error
}

func (m *mockCloseErrRankClient) Close() error {
	_ = m.MockSkillFetcher.Close()
	return m.closeErr
}

// TestChallenger2_CLI_FallbackToNoOpRankClient_Stress tests all failure modes
// for rank client initialization and verifies seamless fallback to NoOpRankClient.
func TestChallenger2_CLI_FallbackToNoOpRankClient_Stress(t *testing.T) {
	// Scenario 1: Polling auth disabled -> verify NoOpRankClient used
	t.Run("PollingAuth_Disabled_UsesNoOpRankClient", func(t *testing.T) {
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		runner := createTestRunner(stdout, stderr)

		var passedSkillFetcher playertrack.SkillFetcher
		runner.LoadConfig = func(cli config.CLIFlags) (*config.Config, error) {
			cfg := config.NewDefaultConfig()
			cfg.Ballchasing.APIKey = "k"
			cfg.Auth.Epic.RefreshToken = "r"
			cfg.Sync.Once = true
			cfg.PlayerTracking.Enabled = true
			cfg.PollingAuth.Enabled = false
			return cfg, nil
		}
		runner.NewPlayerTracker = func(store storage.StateStore, rankClient playertrack.SkillFetcher, cfg config.PlayerTrackingConfig, authCfg config.AuthConfig, opts ...playertrack.TrackerOption) (*playertrack.Tracker, error) {
			passedSkillFetcher = rankClient
			return playertrack.NewTracker(store, rankClient, cfg, authCfg, opts...)
		}

		code := runner.Run(context.Background(), []string{"--once"})
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if _, ok := passedSkillFetcher.(*playertrack.NoOpRankClient); !ok {
			t.Fatalf("expected *playertrack.NoOpRankClient, got %T", passedSkillFetcher)
		}
	})

	// Scenario 2: Polling auth enabled, but NewPollingAuth returns error
	// Must log warning, fall back to NoOpRankClient, and NOT return exit code 1
	t.Run("PollingAuth_ProviderInitError_FallsBackToNoOpRankClient", func(t *testing.T) {
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		runner := createTestRunner(stdout, stderr)

		var passedSkillFetcher playertrack.SkillFetcher
		runner.LoadConfig = func(cli config.CLIFlags) (*config.Config, error) {
			cfg := config.NewDefaultConfig()
			cfg.Ballchasing.APIKey = "k"
			cfg.Auth.Epic.RefreshToken = "r"
			cfg.Sync.Once = true
			cfg.PlayerTracking.Enabled = true
			cfg.PollingAuth.Enabled = true
			return cfg, nil
		}
		runner.NewPollingAuth = func(cfg config.PollingAuthConfig, opts ...auth.Option) (auth.AuthProvider, error) {
			return nil, errors.New("eos token exchange connection refused")
		}
		runner.NewPlayerTracker = func(store storage.StateStore, rankClient playertrack.SkillFetcher, cfg config.PlayerTrackingConfig, authCfg config.AuthConfig, opts ...playertrack.TrackerOption) (*playertrack.Tracker, error) {
			passedSkillFetcher = rankClient
			return playertrack.NewTracker(store, rankClient, cfg, authCfg, opts...)
		}

		code := runner.Run(context.Background(), []string{"--once"})
		if code != 0 {
			t.Fatalf("expected exit code 0 on graceful degradation, got %d. stderr: %s", code, stderr.String())
		}
		if _, ok := passedSkillFetcher.(*playertrack.NoOpRankClient); !ok {
			t.Fatalf("expected *playertrack.NoOpRankClient, got %T", passedSkillFetcher)
		}
		if !strings.Contains(stderr.String(), "failed to initialize polling auth provider; falling back to NoOpRankClient") {
			t.Errorf("expected warning in log, got: %s", stderr.String())
		}
	})

	// Scenario 3: Polling auth provider succeeds, but NewRankClient returns error
	// Must log warning, fall back to NoOpRankClient, and NOT return exit code 1
	t.Run("RankClient_InitError_FallsBackToNoOpRankClient", func(t *testing.T) {
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		runner := createTestRunner(stdout, stderr)

		var passedSkillFetcher playertrack.SkillFetcher
		runner.LoadConfig = func(cli config.CLIFlags) (*config.Config, error) {
			cfg := config.NewDefaultConfig()
			cfg.Ballchasing.APIKey = "k"
			cfg.Auth.Epic.RefreshToken = "r"
			cfg.Sync.Once = true
			cfg.PlayerTracking.Enabled = true
			cfg.PollingAuth.Enabled = true
			return cfg, nil
		}
		runner.NewPollingAuth = func(cfg config.PollingAuthConfig, opts ...auth.Option) (auth.AuthProvider, error) {
			return &mockAuthProvider{name: "epic"}, nil
		}
		runner.NewRankClient = func(cfg playertrack.RankClientConfig) (playertrack.SkillFetcher, error) {
			return nil, errors.New("psynet rpc handshake timeout")
		}
		runner.NewPlayerTracker = func(store storage.StateStore, rankClient playertrack.SkillFetcher, cfg config.PlayerTrackingConfig, authCfg config.AuthConfig, opts ...playertrack.TrackerOption) (*playertrack.Tracker, error) {
			passedSkillFetcher = rankClient
			return playertrack.NewTracker(store, rankClient, cfg, authCfg, opts...)
		}

		code := runner.Run(context.Background(), []string{"--once"})
		if code != 0 {
			t.Fatalf("expected exit code 0 on graceful degradation, got %d. stderr: %s", code, stderr.String())
		}
		if _, ok := passedSkillFetcher.(*playertrack.NoOpRankClient); !ok {
			t.Fatalf("expected *playertrack.NoOpRankClient, got %T", passedSkillFetcher)
		}
		if !strings.Contains(stderr.String(), "failed to initialize rank client; falling back to NoOpRankClient") {
			t.Errorf("expected warning in log, got: %s", stderr.String())
		}
	})

	// Scenario 3b: Polling auth provider Authenticate fails
	// Must log warning, fall back to NoOpRankClient, and NOT return exit code 1
	t.Run("PollingAuth_AuthenticateError_FallsBackToNoOpRankClient", func(t *testing.T) {
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		runner := createTestRunner(stdout, stderr)

		var passedSkillFetcher playertrack.SkillFetcher
		runner.LoadConfig = func(cli config.CLIFlags) (*config.Config, error) {
			cfg := config.NewDefaultConfig()
			cfg.Ballchasing.APIKey = "k"
			cfg.Auth.Epic.RefreshToken = "r"
			cfg.Sync.Once = true
			cfg.PlayerTracking.Enabled = true
			cfg.PollingAuth.Enabled = true
			return cfg, nil
		}
		runner.NewPollingAuth = func(cfg config.PollingAuthConfig, opts ...auth.Option) (auth.AuthProvider, error) {
			return &mockAuthProvider{name: "epic", authErr: errors.New("user cancelled login prompt")}, nil
		}
		runner.NewPlayerTracker = func(store storage.StateStore, rankClient playertrack.SkillFetcher, cfg config.PlayerTrackingConfig, authCfg config.AuthConfig, opts ...playertrack.TrackerOption) (*playertrack.Tracker, error) {
			passedSkillFetcher = rankClient
			return playertrack.NewTracker(store, rankClient, cfg, authCfg, opts...)
		}

		code := runner.Run(context.Background(), []string{"--once"})
		if code != 0 {
			t.Fatalf("expected exit code 0 on graceful degradation, got %d. stderr: %s", code, stderr.String())
		}
		if _, ok := passedSkillFetcher.(*playertrack.NoOpRankClient); !ok {
			t.Fatalf("expected *playertrack.NoOpRankClient, got %T", passedSkillFetcher)
		}
		if !strings.Contains(stderr.String(), "polling account authentication failed; falling back to NoOpRankClient") {
			t.Errorf("expected warning in log, got: %s", stderr.String())
		}
	})

	// Scenario 4: Error during rank client close does not panic runner defer chain
	t.Run("RankClient_CloseError_DoesNotCrash", func(t *testing.T) {
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		runner := createTestRunner(stdout, stderr)

		runner.LoadConfig = func(cli config.CLIFlags) (*config.Config, error) {
			cfg := config.NewDefaultConfig()
			cfg.Ballchasing.APIKey = "k"
			cfg.Auth.Epic.RefreshToken = "r"
			cfg.Sync.Once = true
			cfg.PlayerTracking.Enabled = true
			cfg.PollingAuth.Enabled = true
			return cfg, nil
		}
		runner.NewPollingAuth = func(cfg config.PollingAuthConfig, opts ...auth.Option) (auth.AuthProvider, error) {
			return &mockAuthProvider{name: "epic"}, nil
		}
		runner.NewRankClient = func(cfg playertrack.RankClientConfig) (playertrack.SkillFetcher, error) {
			return &mockCloseErrRankClient{
				MockSkillFetcher: playertrack.NewMockSkillFetcher(),
				closeErr:         errors.New("network socket close error"),
			}, nil
		}

		code := runner.Run(context.Background(), []string{"--once"})
		if code != 0 {
			t.Fatalf("expected clean exit code 0, got %d", code)
		}
		if !strings.Contains(stderr.String(), "error closing rank client") {
			t.Errorf("expected warning log about closing rank client, got: %s", stderr.String())
		}
	})
}

// TestChallenger2_CLI_BackwardCompatibility_PlayerTrackingDisabled verifies that
// when player tracking is disabled, rl-sync operates cleanly without regressions.
func TestChallenger2_CLI_BackwardCompatibility_PlayerTrackingDisabled(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	runner := createTestRunner(stdout, stderr)

	var trackerConstructed bool
	var pollingAuthConstructed bool
	var rankClientConstructed bool
	var daemonWiredWithTracker bool

	runner.LoadConfig = func(cli config.CLIFlags) (*config.Config, error) {
		cfg := config.NewDefaultConfig()
		cfg.Ballchasing.APIKey = "k"
		cfg.Auth.Epic.RefreshToken = "r"
		cfg.Sync.Once = true
		cfg.PlayerTracking.Enabled = false // Disabled
		cfg.StatsAPI.Enabled = true
		return cfg, nil
	}

	runner.NewPlayerTracker = func(store storage.StateStore, rankClient playertrack.SkillFetcher, cfg config.PlayerTrackingConfig, authCfg config.AuthConfig, opts ...playertrack.TrackerOption) (*playertrack.Tracker, error) {
		trackerConstructed = true
		return nil, nil
	}
	runner.NewPollingAuth = func(cfg config.PollingAuthConfig, opts ...auth.Option) (auth.AuthProvider, error) {
		pollingAuthConstructed = true
		return nil, nil
	}
	runner.NewRankClient = func(cfg playertrack.RankClientConfig) (playertrack.SkillFetcher, error) {
		rankClientConstructed = true
		return nil, nil
	}
	runner.NewDaemon = func(s daemon.Syncer, cfg *config.Config, opts ...daemon.Option) (DaemonEngine, error) {
		d, err := daemon.New(s, cfg, opts...)
		if err != nil {
			return nil, err
		}
		// If handler handles /current-match, response active_match must be false
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/current-match", nil)
		d.Handler(context.Background()).ServeHTTP(rec, req)
		if strings.Contains(rec.Body.String(), `"active_match":true`) {
			daemonWiredWithTracker = true
		}
		return &mockDaemonEngine{}, nil
	}

	code := runner.Run(context.Background(), []string{"--once"})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d. stderr: %s", code, stderr.String())
	}

	if trackerConstructed {
		t.Error("PlayerTracker was constructed even though PlayerTracking.Enabled = false")
	}
	if pollingAuthConstructed {
		t.Error("PollingAuth was constructed even though PlayerTracking.Enabled = false")
	}
	if rankClientConstructed {
		t.Error("RankClient was constructed even though PlayerTracking.Enabled = false")
	}
	if daemonWiredWithTracker {
		t.Error("Daemon was wired with active player tracker even though disabled")
	}
}
