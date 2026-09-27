package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dank/rl-api-utils/internal/config"
)

// TestChallenger2_CLI_Precedence verifies that CLI flags (--web-host, --web-port, --web-enabled)
// strictly override environment variables and configuration file values.
func TestChallenger2_CLI_Precedence(t *testing.T) {
	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "config.yaml")
	fileContent := `
ballchasing:
  api_key: "test-api-key"
auth:
  provider: "epic"
  epic:
    refresh_token: "test-primary-token"
web:
  enabled: false
  host: "10.0.0.1"
  port: 8080
`
	if err := os.WriteFile(confPath, []byte(fileContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	t.Run("CLI_Wins_Over_ConfigFile_And_Env", func(t *testing.T) {
		t.Setenv("RL_SYNC_WEB_ENABLED", "false")
		t.Setenv("RL_SYNC_WEB_HOST", "10.0.0.2")
		t.Setenv("RL_SYNC_WEB_PORT", "8081")

		var capturedCfg *config.Config
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}

		runner := createTestRunner(stdout, stderr)
		runner.LoadConfig = func(cli config.CLIFlags) (*config.Config, error) {
			cfg, err := config.Load(cli)
			if err != nil {
				return nil, err
			}
			capturedCfg = cfg
			return cfg, nil
		}

		args := []string{
			"--config=" + confPath,
			"--web-enabled=true",
			"--web-host=192.168.1.100",
			"--web-port=9090",
			"--once",
		}

		code := runner.Run(context.Background(), args)
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d. stderr: %s", code, stderr.String())
		}

		if capturedCfg == nil {
			t.Fatal("expected config to be loaded")
		}
		if capturedCfg.Web.Enabled != true {
			t.Errorf("expected Web.Enabled=true from CLI, got %v", capturedCfg.Web.Enabled)
		}
		if capturedCfg.Web.Host != "192.168.1.100" {
			t.Errorf("expected Web.Host='192.168.1.100' from CLI, got %q", capturedCfg.Web.Host)
		}
		if capturedCfg.Web.Port != 9090 {
			t.Errorf("expected Web.Port=9090 from CLI, got %d", capturedCfg.Web.Port)
		}
	})

	t.Run("CLI_Can_Disable_Web_Over_ConfigFile_And_Env", func(t *testing.T) {
		t.Setenv("RL_SYNC_WEB_ENABLED", "true")

		var capturedCfg *config.Config
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}

		runner := createTestRunner(stdout, stderr)
		runner.LoadConfig = func(cli config.CLIFlags) (*config.Config, error) {
			cfg, err := config.Load(cli)
			if err != nil {
				return nil, err
			}
			capturedCfg = cfg
			return cfg, nil
		}

		args := []string{
			"--config=" + confPath,
			"--web-enabled=false",
			"--once",
		}

		code := runner.Run(context.Background(), args)
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d. stderr: %s", code, stderr.String())
		}

		if capturedCfg == nil {
			t.Fatal("expected config to be loaded")
		}
		if capturedCfg.Web.Enabled != false {
			t.Errorf("expected Web.Enabled=false from CLI override, got %v", capturedCfg.Web.Enabled)
		}
	})

	t.Run("Env_Overrides_ConfigFile_When_CLI_Omitted", func(t *testing.T) {
		t.Setenv("RL_SYNC_WEB_ENABLED", "true")
		t.Setenv("RL_SYNC_WEB_HOST", "172.16.0.1")
		t.Setenv("RL_SYNC_WEB_PORT", "8085")

		var capturedCfg *config.Config
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}

		runner := createTestRunner(stdout, stderr)
		runner.LoadConfig = func(cli config.CLIFlags) (*config.Config, error) {
			cfg, err := config.Load(cli)
			if err != nil {
				return nil, err
			}
			capturedCfg = cfg
			return cfg, nil
		}

		args := []string{
			"--config=" + confPath,
			"--once",
		}

		code := runner.Run(context.Background(), args)
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d. stderr: %s", code, stderr.String())
		}

		if capturedCfg == nil {
			t.Fatal("expected config to be loaded")
		}
		if capturedCfg.Web.Enabled != true {
			t.Errorf("expected Web.Enabled=true from ENV, got %v", capturedCfg.Web.Enabled)
		}
		if capturedCfg.Web.Host != "172.16.0.1" {
			t.Errorf("expected Web.Host='172.16.0.1' from ENV, got %q", capturedCfg.Web.Host)
		}
		if capturedCfg.Web.Port != 8085 {
			t.Errorf("expected Web.Port=8085 from ENV, got %d", capturedCfg.Web.Port)
		}
	})
}

// TestChallenger2_CLI_PortValidation tests port boundary conditions (0, -1, 70000, 65536, etc.).
func TestChallenger2_CLI_PortValidation(t *testing.T) {
	testCases := []struct {
		name        string
		args        []string
		expectError bool
		errSnippet  string
	}{
		{
			name:        "port_0_rejected_when_enabled",
			args:        []string{"--web-port=0"},
			expectError: true,
			errSnippet:  "web: 'port' must be between 1 and 65535 (got 0)",
		},
		{
			name:        "port_negative_1_rejected",
			args:        []string{"--web-port=-1"},
			expectError: true,
			errSnippet:  "web: 'port' must be between 1 and 65535 (got -1)",
		},
		{
			name:        "port_70000_rejected",
			args:        []string{"--web-port=70000"},
			expectError: true,
			errSnippet:  "web: 'port' must be between 1 and 65535 (got 70000)",
		},
		{
			name:        "port_65536_boundary_rejected",
			args:        []string{"--web-port=65536"},
			expectError: true,
			errSnippet:  "web: 'port' must be between 1 and 65535 (got 65536)",
		},
		{
			name:        "port_1_boundary_allowed",
			args:        []string{"--web-port=1"},
			expectError: false,
		},
		{
			name:        "port_65535_boundary_allowed",
			args:        []string{"--web-port=65535"},
			expectError: false,
		},
		{
			name:        "empty_host_rejected",
			args:        []string{"--web-host="},
			expectError: true,
			errSnippet:  "web: 'host' cannot be empty",
		},
		{
			name:        "whitespace_host_rejected",
			args:        []string{"--web-host=   "},
			expectError: true,
			errSnippet:  "web: 'host' cannot be empty",
		},
		{
			name:        "port_0_allowed_when_web_disabled",
			args:        []string{"--web-enabled=false", "--web-port=0"},
			expectError: false,
		},
		{
			name:        "port_negative_rejected_even_when_web_disabled",
			args:        []string{"--web-enabled=false", "--web-port=-1"},
			expectError: true,
			errSnippet:  "web: 'port' must be between 0 and 65535 (got -1)",
		},
		{
			name:        "port_70000_rejected_even_when_web_disabled",
			args:        []string{"--web-enabled=false", "--web-port=70000"},
			expectError: true,
			errSnippet:  "web: 'port' must be between 0 and 65535 (got 70000)",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}
			runner := createTestRunner(stdout, stderr)
			runner.LoadConfig = func(cli config.CLIFlags) (*config.Config, error) {
				cfg := config.NewDefaultConfig()
				cfg.Ballchasing.APIKey = "k"
				cfg.Auth.Epic.RefreshToken = "r"
				cfg.Sync.Once = true

				if cli.WebEnabled != nil {
					cfg.Web.Enabled = *cli.WebEnabled
				}
				if cli.WebHost != nil {
					cfg.Web.Host = strings.TrimSpace(*cli.WebHost)
				}
				if cli.WebPort != nil {
					cfg.Web.Port = *cli.WebPort
				}

				if err := cfg.Validate(); err != nil {
					return nil, err
				}
				return cfg, nil
			}

			args := append([]string{"--once"}, tc.args...)
			code := runner.Run(context.Background(), args)

			if tc.expectError {
				if code == 0 {
					t.Errorf("expected non-zero exit code for %v, got 0", tc.args)
				}
				if tc.errSnippet != "" && !strings.Contains(stderr.String(), tc.errSnippet) {
					t.Errorf("expected stderr to contain %q, got: %s", tc.errSnippet, stderr.String())
				}
			} else {
				if code != 0 {
					t.Errorf("expected exit code 0 for %v, got %d. stderr: %s", tc.args, code, stderr.String())
				}
			}
		})
	}
}

// TestChallenger2_CLI_InvalidFlagSyntax tests non-numeric port syntax rejection.
func TestChallenger2_CLI_InvalidFlagSyntax(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	runner := createTestRunner(stdout, stderr)

	code := runner.Run(context.Background(), []string{"--web-port=invalid_port_string"})
	if code == 0 {
		t.Fatal("expected exit code 1 for non-numeric --web-port, got 0")
	}
	if !strings.Contains(stderr.String(), "invalid value") && !strings.Contains(stderr.String(), "parse error") {
		t.Errorf("expected syntax error in stderr, got: %s", stderr.String())
	}
}
