package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"gopkg.in/yaml.v3"
)

func TestConfig_Defaults(t *testing.T) {
	cfg := config.NewDefaultConfig()

	if cfg.Auth.Provider != "epic" {
		t.Errorf("expected default provider 'epic', got %q", cfg.Auth.Provider)
	}
	if cfg.Ballchasing.Visibility != "public" {
		t.Errorf("expected default visibility 'public', got %q", cfg.Ballchasing.Visibility)
	}
	if cfg.Ballchasing.BaseURL != "https://ballchasing.com/api" {
		t.Errorf("expected default base URL 'https://ballchasing.com/api', got %q", cfg.Ballchasing.BaseURL)
	}
	if cfg.Ballchasing.Timeout.Duration() != 60*time.Second {
		t.Errorf("expected default timeout 60s, got %v", cfg.Ballchasing.Timeout)
	}
	if cfg.Ballchasing.MaxRetries != 3 {
		t.Errorf("expected default max retries 3, got %d", cfg.Ballchasing.MaxRetries)
	}
	if cfg.Sync.PollInterval.Duration() != 5*time.Minute {
		t.Errorf("expected default poll interval 5m, got %v", cfg.Sync.PollInterval)
	}
	if cfg.Sync.ReplayDir != "./replays" {
		t.Errorf("expected default replay dir './replays', got %q", cfg.Sync.ReplayDir)
	}
	if cfg.Sync.DBPath != "./rl-sync.db" {
		t.Errorf("expected default DB path './rl-sync.db', got %q", cfg.Sync.DBPath)
	}
	if !cfg.Sync.KeepLocalFiles {
		t.Errorf("expected default keep local files true")
	}
	if cfg.Logging.Level != "info" || cfg.Logging.Format != "text" {
		t.Errorf("expected default logging info/text, got %s/%s", cfg.Logging.Level, cfg.Logging.Format)
	}
}

func TestConfig_LoadYAML(t *testing.T) {
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "config.yaml")

	content := `
auth:
  provider: "epic"
  epic:
    refresh_token: "test-epic-token"
ballchasing:
  api_key: "test-ballchasing-key"
  visibility: "unlisted"
  timeout: "45s"
sync:
  poll_interval: "2m"
  replay_dir: "/tmp/custom-replays"
  db_path: "/tmp/custom.db"
logging:
  level: "debug"
  format: "json"
`
	if err := os.WriteFile(yamlPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write yaml: %v", err)
	}

	cfg, err := config.Load(config.CLIFlags{ConfigPath: yamlPath})
	if err != nil {
		t.Fatalf("unexpected error loading yaml config: %v", err)
	}

	if cfg.Auth.Epic.RefreshToken != "test-epic-token" {
		t.Errorf("expected refresh token 'test-epic-token', got %q", cfg.Auth.Epic.RefreshToken)
	}
	if cfg.Ballchasing.Visibility != "unlisted" {
		t.Errorf("expected visibility 'unlisted', got %q", cfg.Ballchasing.Visibility)
	}
	if cfg.Ballchasing.Timeout.Duration() != 45*time.Second {
		t.Errorf("expected timeout 45s, got %v", cfg.Ballchasing.Timeout)
	}
	if cfg.Sync.PollInterval.Duration() != 2*time.Minute {
		t.Errorf("expected poll interval 2m, got %v", cfg.Sync.PollInterval)
	}
	if cfg.Sync.ReplayDir != "/tmp/custom-replays" {
		t.Errorf("expected replay dir '/tmp/custom-replays', got %q", cfg.Sync.ReplayDir)
	}
	if cfg.Logging.Level != "debug" || cfg.Logging.Format != "json" {
		t.Errorf("expected debug/json, got %s/%s", cfg.Logging.Level, cfg.Logging.Format)
	}
}

func TestConfig_LoadJSON(t *testing.T) {
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, "config.json")

	content := `{
  "auth": {
    "provider": "steam",
    "steam": {
      "session_ticket": "deadbeef1234",
      "steam_id_64": "76561198000000001"
    }
  },
  "ballchasing": {
    "api_key": "steam-user-key",
    "visibility": "private"
  },
  "sync": {
    "poll_interval": "10m"
  }
}`
	if err := os.WriteFile(jsonPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write json: %v", err)
	}

	cfg, err := config.Load(config.CLIFlags{ConfigPath: jsonPath})
	if err != nil {
		t.Fatalf("unexpected error loading json config: %v", err)
	}

	if cfg.Auth.Provider != "steam" {
		t.Errorf("expected steam provider, got %q", cfg.Auth.Provider)
	}
	if cfg.Auth.Steam.SessionTicket != "deadbeef1234" {
		t.Errorf("expected session ticket 'deadbeef1234', got %q", cfg.Auth.Steam.SessionTicket)
	}
	if cfg.Auth.Steam.SteamID64 != "76561198000000001" {
		t.Errorf("expected steam ID '76561198000000001', got %q", cfg.Auth.Steam.SteamID64)
	}
	if cfg.Ballchasing.Visibility != "private" {
		t.Errorf("expected visibility 'private', got %q", cfg.Ballchasing.Visibility)
	}
	if cfg.Sync.PollInterval.Duration() != 10*time.Minute {
		t.Errorf("expected poll interval 10m, got %v", cfg.Sync.PollInterval)
	}
}

func TestConfig_EnvOverrides(t *testing.T) {
	t.Setenv("RL_SYNC_AUTH_PROVIDER", "steam")
	t.Setenv("RL_SYNC_STEAM_SESSION_TICKET", "env-steam-ticket")
	t.Setenv("RL_SYNC_STEAM_ID_64", "76561198000000002")
	t.Setenv("RL_SYNC_BALLCHASING_API_KEY", "env-api-key")
	t.Setenv("RL_SYNC_BALLCHASING_VISIBILITY", "unlisted")
	t.Setenv("RL_SYNC_POLL_INTERVAL", "15m")
	t.Setenv("RL_SYNC_KEEP_LOCAL_FILES", "false")
	t.Setenv("RL_SYNC_DRY_RUN", "true")

	cfg, err := config.Load(config.CLIFlags{})
	if err != nil {
		t.Fatalf("failed to load config with env vars: %v", err)
	}

	if cfg.Auth.Provider != "steam" {
		t.Errorf("expected provider steam, got %q", cfg.Auth.Provider)
	}
	if cfg.Auth.Steam.SessionTicket != "env-steam-ticket" {
		t.Errorf("expected session ticket 'env-steam-ticket', got %q", cfg.Auth.Steam.SessionTicket)
	}
	if cfg.Ballchasing.APIKey != "env-api-key" {
		t.Errorf("expected ballchasing api key 'env-api-key', got %q", cfg.Ballchasing.APIKey)
	}
	if cfg.Sync.PollInterval.Duration() != 15*time.Minute {
		t.Errorf("expected poll interval 15m, got %v", cfg.Sync.PollInterval)
	}
	if cfg.Sync.KeepLocalFiles != false {
		t.Errorf("expected keep_local_files false, got %v", cfg.Sync.KeepLocalFiles)
	}
	if cfg.Sync.DryRun != true {
		t.Errorf("expected dry_run true, got %v", cfg.Sync.DryRun)
	}
}

func TestConfig_PrecedenceHierarchy(t *testing.T) {
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "config.yaml")

	// Config file sets poll_interval to 10m
	yamlContent := `
auth:
  provider: "epic"
  epic:
    refresh_token: "file-token"
ballchasing:
  api_key: "file-key"
sync:
  poll_interval: "10m"
`
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write yaml: %v", err)
	}

	// 1. Base test: YAML overrides default 5m -> 10m
	cfg, err := config.Load(config.CLIFlags{ConfigPath: yamlPath})
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if cfg.Sync.PollInterval.Duration() != 10*time.Minute {
		t.Fatalf("expected 10m from file, got %v", cfg.Sync.PollInterval)
	}

	// 2. Env var overrides YAML file (10m -> 15m)
	t.Setenv("RL_SYNC_POLL_INTERVAL", "15m")
	cfg, err = config.Load(config.CLIFlags{ConfigPath: yamlPath})
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if cfg.Sync.PollInterval.Duration() != 15*time.Minute {
		t.Fatalf("expected 15m from env, got %v", cfg.Sync.PollInterval)
	}

	// 3. CLI flag overrides Env var (15m -> 20m)
	cliPoll := 20 * time.Minute
	cfg, err = config.Load(config.CLIFlags{
		ConfigPath:   yamlPath,
		PollInterval: &cliPoll,
	})
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if cfg.Sync.PollInterval.Duration() != 20*time.Minute {
		t.Fatalf("expected 20m from CLI flag, got %v", cfg.Sync.PollInterval)
	}
}

func TestConfig_ValidationFailures(t *testing.T) {
	tests := []struct {
		name          string
		modify        func(c *config.Config)
		expectedError string
	}{
		{
			name: "invalid provider",
			modify: func(c *config.Config) {
				c.Auth.Provider = "playstation"
			},
			expectedError: "invalid provider \"playstation\"",
		},
		{
			name: "epic missing both credentials",
			modify: func(c *config.Config) {
				c.Auth.Provider = "epic"
				c.Auth.Epic.RefreshToken = ""
				c.Auth.Epic.AuthCode = ""
			},
			expectedError: "epic provider requires either 'refresh_token' or 'auth_code'",
		},
		{
			name: "steam missing ticket",
			modify: func(c *config.Config) {
				c.Auth.Provider = "steam"
				c.Auth.Steam.SessionTicket = ""
				c.Auth.Steam.SteamID64 = "76561198000000000"
			},
			expectedError: "steam provider requires 'session_ticket'",
		},
		{
			name: "steam missing steam_id_64",
			modify: func(c *config.Config) {
				c.Auth.Provider = "steam"
				c.Auth.Steam.SessionTicket = "ticket"
				c.Auth.Steam.SteamID64 = ""
			},
			expectedError: "steam provider requires 'steam_id_64'",
		},
		{
			name: "missing ballchasing api key",
			modify: func(c *config.Config) {
				c.Ballchasing.APIKey = ""
			},
			expectedError: "ballchasing: 'api_key' is required",
		},
		{
			name: "invalid visibility",
			modify: func(c *config.Config) {
				c.Ballchasing.Visibility = "secret"
			},
			expectedError: "invalid visibility \"secret\"",
		},
		{
			name: "invalid base url",
			modify: func(c *config.Config) {
				c.Ballchasing.BaseURL = "ftp://invalid-url"
			},
			expectedError: "invalid base_url",
		},
		{
			name: "non-positive timeout",
			modify: func(c *config.Config) {
				c.Ballchasing.Timeout = config.Duration(0)
			},
			expectedError: "'timeout' must be positive",
		},
		{
			name: "negative max retries",
			modify: func(c *config.Config) {
				c.Ballchasing.MaxRetries = -1
			},
			expectedError: "'max_retries' cannot be negative",
		},
		{
			name: "non-positive poll interval",
			modify: func(c *config.Config) {
				c.Sync.PollInterval = config.Duration(0)
			},
			expectedError: "'poll_interval' must be positive",
		},
		{
			name: "empty replay dir",
			modify: func(c *config.Config) {
				c.Sync.ReplayDir = ""
			},
			expectedError: "'replay_dir' cannot be empty",
		},
		{
			name: "empty db path",
			modify: func(c *config.Config) {
				c.Sync.DBPath = ""
			},
			expectedError: "'db_path' cannot be empty",
		},
		{
			name: "non-positive download timeout",
			modify: func(c *config.Config) {
				c.Sync.DownloadTimeout = config.Duration(0)
			},
			expectedError: "'download_timeout' must be positive",
		},
		{
			name: "invalid logging level",
			modify: func(c *config.Config) {
				c.Logging.Level = "superverbose"
			},
			expectedError: "invalid level \"superverbose\"",
		},
		{
			name: "invalid logging format",
			modify: func(c *config.Config) {
				c.Logging.Format = "xml"
			},
			expectedError: "invalid format \"xml\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.NewDefaultConfig()
			cfg.Auth.Epic.RefreshToken = "valid-token"
			cfg.Ballchasing.APIKey = "valid-key"

			tt.modify(cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.expectedError)
			}
			if !strings.Contains(err.Error(), tt.expectedError) {
				t.Errorf("expected error containing %q, got %q", tt.expectedError, err.Error())
			}
		})
	}
}

func TestConfig_DurationCustomType(t *testing.T) {
	// JSON marshaling and unmarshaling
	type testStruct struct {
		D config.Duration `json:"duration" yaml:"duration"`
	}

	ts := testStruct{D: config.Duration(5 * time.Minute)}
	data, err := json.Marshal(ts)
	if err != nil {
		t.Fatalf("failed to marshal duration to JSON: %v", err)
	}

	var parsed testStruct
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal duration from JSON string: %v", err)
	}
	if parsed.D.Duration() != 5*time.Minute {
		t.Errorf("expected 5m, got %v", parsed.D)
	}

	// JSON unmarshaling from numeric nanoseconds
	numJSON := []byte(`{"duration": 300000000000}`)
	var parsedNum testStruct
	if err := json.Unmarshal(numJSON, &parsedNum); err != nil {
		t.Fatalf("failed to unmarshal duration from numeric JSON: %v", err)
	}
	if parsedNum.D.Duration() != 5*time.Minute {
		t.Errorf("expected 5m from numeric, got %v", parsedNum.D)
	}

	// YAML marshaling and unmarshaling
	yamlData, err := yaml.Marshal(ts)
	if err != nil {
		t.Fatalf("failed to marshal duration to YAML: %v", err)
	}
	var parsedYAML testStruct
	if err := yaml.Unmarshal(yamlData, &parsedYAML); err != nil {
		t.Fatalf("failed to unmarshal duration from YAML: %v", err)
	}
	if parsedYAML.D.Duration() != 5*time.Minute {
		t.Errorf("expected 5m from YAML, got %v", parsedYAML.D)
	}

	// Invalid duration string
	invalidJSON := []byte(`{"duration": "invalid"}`)
	if err := json.Unmarshal(invalidJSON, &parsed); err == nil {
		t.Error("expected error unmarshaling invalid duration string, got nil")
	}
}

func TestConfig_MissingExplicitConfigFile(t *testing.T) {
	_, err := config.Load(config.CLIFlags{
		ConfigPath: "/nonexistent/path/to/config.yaml",
	})
	if err == nil {
		t.Fatal("expected error loading non-existent explicit config file, got nil")
	}
}
