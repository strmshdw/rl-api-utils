package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
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

// TestChallenge_CLI_Precedence_LayeredHierarchy tests the 4-layer configuration hierarchy:
// CLI Flags > Environment Variables > Configuration File > Defaults.
func TestChallenge_CLI_Precedence_LayeredHierarchy(t *testing.T) {
	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "precedence.yaml")

	// 1. Layer 2: Configuration file content
	yamlContent := `
auth:
  provider: epic
  epic:
    refresh_token: file-epic-token
ballchasing:
  api_key: file-bc-key
sync:
  poll_interval: 10m
  replay_dir: /file/replays
  db_path: /file/db.sqlite
  dry_run: false
  once: false
logging:
  level: error
  format: text
`
	if err := os.WriteFile(confPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config file: %v", err)
	}

	// 2. Layer 3: Environment variables (overrides file, overridden by CLI)
	t.Setenv("RL_SYNC_POLL_INTERVAL", "3m")
	t.Setenv("RL_SYNC_REPLAY_DIR", "/env/replays")
	t.Setenv("RL_SYNC_DB_PATH", "/env/db.sqlite")
	t.Setenv("RL_SYNC_AUTH_PROVIDER", "steam")
	t.Setenv("RL_SYNC_LOG_LEVEL", "warn")
	t.Setenv("RL_SYNC_LOG_FORMAT", "json")
	t.Setenv("RL_SYNC_DRY_RUN", "true")

	// 3. Layer 4: CLI flags (highest priority)
	// We override poll-interval, replay-dir, provider, and log-level on CLI.
	// We leave db-path, log-format, and dry-run to come from ENV.
	// We leave ballchasing api_key to come from FILE.
	cliArgs := []string{
		"--config=" + confPath,
		"--poll-interval=45s",
		"--replay-dir=/cli/replays",
		"--provider=epic",
		"--log-level=debug",
		"--once",
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	runner := createTestRunner(stdout, stderr)

	var resolvedCfg *config.Config
	runner.LoadConfig = func(cli config.CLIFlags) (*config.Config, error) {
		cfg, err := config.Load(cli)
		if err != nil {
			return nil, err
		}
		resolvedCfg = cfg
		return cfg, nil
	}

	exitCode := runner.Run(context.Background(), cliArgs)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d. stderr: %s", exitCode, stderr.String())
	}

	if resolvedCfg == nil {
		t.Fatal("resolvedCfg was not populated")
	}

	// Assert CLI overrides both ENV and FILE
	if resolvedCfg.Sync.PollInterval.Duration() != 45*time.Second {
		t.Errorf("expected PollInterval 45s (from CLI), got %v", resolvedCfg.Sync.PollInterval.Duration())
	}
	if resolvedCfg.Sync.ReplayDir != "/cli/replays" {
		t.Errorf("expected ReplayDir /cli/replays (from CLI), got %s", resolvedCfg.Sync.ReplayDir)
	}
	if resolvedCfg.Auth.Provider != "epic" {
		t.Errorf("expected Provider epic (from CLI), got %s", resolvedCfg.Auth.Provider)
	}
	if resolvedCfg.Logging.Level != "debug" {
		t.Errorf("expected LogLevel debug (from CLI), got %s", resolvedCfg.Logging.Level)
	}
	if !resolvedCfg.Sync.Once {
		t.Errorf("expected Once=true (from CLI)")
	}

	// Assert ENV overrides FILE when flag is omitted from CLI
	if resolvedCfg.Sync.DBPath != "/env/db.sqlite" {
		t.Errorf("expected DBPath /env/db.sqlite (from ENV), got %s", resolvedCfg.Sync.DBPath)
	}
	if resolvedCfg.Logging.Format != "json" {
		t.Errorf("expected LogFormat json (from ENV), got %s", resolvedCfg.Logging.Format)
	}
	if !resolvedCfg.Sync.DryRun {
		t.Errorf("expected DryRun=true (from ENV)")
	}

	// Assert FILE provides values when neither CLI nor ENV sets them
	if resolvedCfg.Ballchasing.APIKey != "file-bc-key" {
		t.Errorf("expected Ballchasing.APIKey file-bc-key (from FILE), got %s", resolvedCfg.Ballchasing.APIKey)
	}
	if resolvedCfg.Auth.Epic.RefreshToken != "file-epic-token" {
		t.Errorf("expected Epic.RefreshToken file-epic-token (from FILE), got %s", resolvedCfg.Auth.Epic.RefreshToken)
	}
}

// TestChallenge_CLI_ExitCodes_Matrix thoroughly validates exit codes across all exit scenarios:
// - Exit code 0 on clean exits (help, version, once completion, context cancellation)
// - Exit code 1 on all validation, authentication, and runtime errors.
func TestChallenge_CLI_ExitCodes_Matrix(t *testing.T) {
	cases := []struct {
		name         string
		args         []string
		setupRunner  func(r *Runner)
		ctxSetup     func() context.Context
		expectedCode int
		errSubstring string
	}{
		{
			name:         "Help flag --help returns 0",
			args:         []string{"--help"},
			expectedCode: 0,
		},
		{
			name:         "Help shorthand -h returns 0",
			args:         []string{"-h"},
			expectedCode: 0,
		},
		{
			name:         "Version flag --version returns 0",
			args:         []string{"--version"},
			expectedCode: 0,
		},
		{
			name:         "Version shorthand -v returns 0",
			args:         []string{"-v"},
			expectedCode: 0,
		},
		{
			name:         "Clean --once single-run mode returns 0",
			args:         []string{"--once"},
			expectedCode: 0,
		},
		{
			name: "Clean exit on context cancellation returns 0",
			args: []string{},
			ctxSetup: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			setupRunner: func(r *Runner) {
				r.NewDaemon = func(s daemon.Syncer, cfg *config.Config, opts ...daemon.Option) (DaemonEngine, error) {
					return &mockDaemonEngine{
						startFunc: func(c context.Context) error {
							return c.Err()
						},
					}, nil
				}
			},
			expectedCode: 0,
		},
		{
			name:         "Unknown flag returns 1",
			args:         []string{"--non-existent-flag"},
			expectedCode: 1,
			errSubstring: "flag provided but not defined",
		},
		{
			name: "Config file not found returns 1",
			args: []string{"--config=/path/that/does/not/exist.yaml"},
			setupRunner: func(r *Runner) {
				r.LoadConfig = func(cli config.CLIFlags) (*config.Config, error) {
					return nil, errors.New("specified config file not found: stat /path/that/does/not/exist.yaml: no such file or directory")
				}
			},
			expectedCode: 1,
			errSubstring: "Configuration error",
		},
		{
			name: "Malformed config file returns 1",
			args: []string{"--config=/dummy.yaml"},
			setupRunner: func(r *Runner) {
				r.LoadConfig = func(cli config.CLIFlags) (*config.Config, error) {
					return nil, errors.New("parsing YAML config: yaml: unmarshal errors")
				}
			},
			expectedCode: 1,
			errSubstring: "Configuration error",
		},
		{
			name: "Config validation failure (missing credentials) returns 1",
			args: []string{"--once"},
			setupRunner: func(r *Runner) {
				r.LoadConfig = func(cli config.CLIFlags) (*config.Config, error) {
					return nil, errors.New("validation failed: auth: epic requires refresh_token or auth_code")
				}
			},
			expectedCode: 1,
			errSubstring: "Configuration error",
		},
		{
			name: "Store creation error returns 1",
			args: []string{"--once"},
			setupRunner: func(r *Runner) {
				r.NewStore = func(path string) (storage.StateStore, error) {
					return nil, errors.New("unable to open database file: permission denied")
				}
			},
			expectedCode: 1,
			errSubstring: "failed to initialize persistent store",
		},
		{
			name: "Auth provider creation error returns 1",
			args: []string{"--once"},
			setupRunner: func(r *Runner) {
				r.NewAuth = func(cfg config.AuthConfig, store storage.StateStore) (auth.AuthProvider, error) {
					return nil, errors.New("unsupported auth provider: nintendo")
				}
			},
			expectedCode: 1,
			errSubstring: "failed to initialize auth provider",
		},
		{
			name: "Auth credentials validation failure returns 1",
			args: []string{"--once"},
			setupRunner: func(r *Runner) {
				r.NewAuth = func(cfg config.AuthConfig, store storage.StateStore) (auth.AuthProvider, error) {
					return &mockAuthProvider{valErr: errors.New("epic auth requires refresh_token")}, nil
				}
			},
			expectedCode: 1,
			errSubstring: "invalid authentication credentials",
		},
		{
			name: "Initial authentication handshake failure returns 1",
			args: []string{"--once"},
			setupRunner: func(r *Runner) {
				r.NewAuth = func(cfg config.AuthConfig, store storage.StateStore) (auth.AuthProvider, error) {
					return &mockAuthProvider{authErr: errors.New("HTTP 401: invalid refresh token")}, nil
				}
			},
			expectedCode: 1,
			errSubstring: "initial authentication failed",
		},
		{
			name: "Ballchasing client construction error returns 1",
			args: []string{"--once"},
			setupRunner: func(r *Runner) {
				r.NewBallchasing = func(cfg ballchasing.ClientConfig) (ballchasing.ReplayUploader, error) {
					return nil, errors.New("ballchasing api key cannot be empty")
				}
			},
			expectedCode: 1,
			errSubstring: "failed to initialize ballchasing client",
		},
		{
			name: "Ballchasing ping failure (bad API key) returns 1",
			args: []string{"--once"},
			setupRunner: func(r *Runner) {
				r.NewBallchasing = func(cfg ballchasing.ClientConfig) (ballchasing.ReplayUploader, error) {
					return &mockBallchasingUploader{pingErr: errors.New("HTTP 401 Unauthorized: invalid API key")}, nil
				}
			},
			expectedCode: 1,
			errSubstring: "ballchasing api key verification failed",
		},
		{
			name: "PsyNet client construction failure returns 1",
			args: []string{"--once"},
			setupRunner: func(r *Runner) {
				r.NewPsyNet = func(cfg psynet.ClientConfig) (psynet.MatchHistoryProvider, error) {
					return nil, errors.New("failed to connect to psynet websocket")
				}
			},
			expectedCode: 1,
			errSubstring: "failed to initialize psynet client",
		},
		{
			name: "Daemon engine construction failure returns 1",
			args: []string{"--once"},
			setupRunner: func(r *Runner) {
				r.NewDaemon = func(s daemon.Syncer, cfg *config.Config, opts ...daemon.Option) (DaemonEngine, error) {
					return nil, errors.New("daemon: nil config")
				}
			},
			expectedCode: 1,
			errSubstring: "failed to initialize daemon engine",
		},
		{
			name: "Daemon runtime failure returns 1",
			args: []string{"--once"},
			setupRunner: func(r *Runner) {
				r.NewDaemon = func(s daemon.Syncer, cfg *config.Config, opts ...daemon.Option) (DaemonEngine, error) {
					return &mockDaemonEngine{
						startFunc: func(ctx context.Context) error {
							return errors.New("panic during cycle execution")
						},
					}, nil
				}
			},
			expectedCode: 1,
			errSubstring: "daemon terminated with fatal error",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}
			runner := createTestRunner(stdout, stderr)

			if tc.setupRunner != nil {
				tc.setupRunner(runner)
			}

			ctx := context.Background()
			if tc.ctxSetup != nil {
				ctx = tc.ctxSetup()
			}

			code := runner.Run(ctx, tc.args)
			if code != tc.expectedCode {
				t.Fatalf("expected exit code %d, got %d. stderr: %s", tc.expectedCode, code, stderr.String())
			}

			if tc.errSubstring != "" {
				combined := stderr.String() + stdout.String()
				if !strings.Contains(combined, tc.errSubstring) {
					t.Fatalf("expected output to contain %q, got: %s", tc.errSubstring, combined)
				}
			}
		})
	}
}

// TestChallenge_CLI_EndToEnd_RealIntegration verifies the complete wiring of cmd/rl-sync
// with real in-memory SQLite store, real daemon engine, real syncer, and mocked network clients.
func TestChallenge_CLI_EndToEnd_RealIntegration(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	runner := createTestRunner(stdout, stderr)

	// Wire real syncer constructor
	runner.NewSyncer = func(store storage.StateStore, p psynet.MatchHistoryProvider, d psynet.ReplayDownloader, u ballchasing.ReplayUploader, cfg syncer.Config) daemon.Syncer {
		s, err := syncer.NewWithConfig(store, p, d, u, cfg)
		if err != nil {
			t.Fatalf("syncer.NewWithConfig failed: %v", err)
		}
		return s
	}

	// Wire real daemon constructor
	runner.NewDaemon = func(s daemon.Syncer, cfg *config.Config, opts ...daemon.Option) (DaemonEngine, error) {
		return daemon.New(s, cfg, opts...)
	}

	// Execute with --once and --dry-run
	code := runner.Run(context.Background(), []string{"--once", "--dry-run", "--log-level=debug"})
	if code != 0 {
		t.Fatalf("expected exit code 0 for real integrated runner, got %d. stderr: %s", code, stderr.String())
	}
}
