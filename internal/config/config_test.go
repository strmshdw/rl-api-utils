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
	if !cfg.Web.Enabled {
		t.Errorf("expected default web enabled true")
	}
	if cfg.Web.Host != "0.0.0.0" {
		t.Errorf("expected default web host '0.0.0.0', got %q", cfg.Web.Host)
	}
	if cfg.Web.Port != 49125 {
		t.Errorf("expected default web port 49125, got %d", cfg.Web.Port)
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

func TestConfig_Defaults_PlayerTrackingAndPollingAuth(t *testing.T) {
	cfg := config.NewDefaultConfig()

	if cfg.PollingAuth.Enabled {
		t.Errorf("expected default polling auth enabled false, got true")
	}
	if cfg.PollingAuth.Provider != "epic" {
		t.Errorf("expected default polling auth provider 'epic', got %q", cfg.PollingAuth.Provider)
	}
	if !cfg.PlayerTracking.Enabled {
		t.Errorf("expected default player tracking enabled true, got false")
	}
	if cfg.PlayerTracking.LocalPlayerID != "" {
		t.Errorf("expected default local player id empty, got %q", cfg.PlayerTracking.LocalPlayerID)
	}
	if cfg.PlayerTracking.LocalPlayerName != "" {
		t.Errorf("expected default local player name empty, got %q", cfg.PlayerTracking.LocalPlayerName)
	}
	if !cfg.PlayerTracking.AutoFetchRanks {
		t.Errorf("expected default auto fetch ranks true, got false")
	}
}

func TestConfig_PollingAuth_YAMLAndJSON(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. YAML Loading
	yamlPath := filepath.Join(tmpDir, "config.yaml")
	yamlContent := `
auth:
  provider: "epic"
  epic:
    refresh_token: "primary-token"
ballchasing:
  api_key: "bc-key"
polling_auth:
  enabled: true
  provider: "steam"
  steam:
    session_ticket: "polling-ticket"
    steam_id_64: "76561198099999999"
    account_name: "PollingBot"
player_tracking:
  enabled: true
  local_player_id: "Epic|my-epic-guid|0"
  local_player_name: "MyLocalPlayer"
  auto_fetch_ranks: false
`
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write yaml: %v", err)
	}

	cfgYAML, err := config.Load(config.CLIFlags{ConfigPath: yamlPath})
	if err != nil {
		t.Fatalf("unexpected error loading yaml: %v", err)
	}

	if !cfgYAML.PollingAuth.Enabled {
		t.Errorf("expected polling auth enabled true")
	}
	if cfgYAML.PollingAuth.Provider != "steam" {
		t.Errorf("expected polling provider steam, got %q", cfgYAML.PollingAuth.Provider)
	}
	if cfgYAML.PollingAuth.Steam.SessionTicket != "polling-ticket" {
		t.Errorf("expected polling ticket 'polling-ticket', got %q", cfgYAML.PollingAuth.Steam.SessionTicket)
	}
	if cfgYAML.PollingAuth.Steam.SteamID64 != "76561198099999999" {
		t.Errorf("expected steam id '76561198099999999', got %q", cfgYAML.PollingAuth.Steam.SteamID64)
	}
	if cfgYAML.PlayerTracking.LocalPlayerID != "Epic|my-epic-guid|0" {
		t.Errorf("expected local player id 'Epic|my-epic-guid|0', got %q", cfgYAML.PlayerTracking.LocalPlayerID)
	}
	if cfgYAML.PlayerTracking.LocalPlayerName != "MyLocalPlayer" {
		t.Errorf("expected local player name 'MyLocalPlayer', got %q", cfgYAML.PlayerTracking.LocalPlayerName)
	}
	if cfgYAML.PlayerTracking.AutoFetchRanks {
		t.Errorf("expected auto fetch ranks false")
	}

	// Test ToAuthConfig converter
	authConverted := cfgYAML.PollingAuth.ToAuthConfig()
	if authConverted.Provider != "steam" || authConverted.Steam.SteamID64 != "76561198099999999" {
		t.Errorf("ToAuthConfig mismatch: %+v", authConverted)
	}

	// 2. JSON Loading
	jsonPath := filepath.Join(tmpDir, "config.json")
	jsonContent := `{
  "auth": {
    "provider": "steam",
    "steam": {
      "session_ticket": "primary-ticket",
      "steam_id_64": "76561198011111111"
    }
  },
  "ballchasing": {
    "api_key": "bc-key"
  },
  "polling_auth": {
    "enabled": true,
    "provider": "epic",
    "epic": {
      "refresh_token": "secondary-epic-token",
      "account_id": "secondary-account-id",
      "display_name": "SecondaryPoller"
    }
  },
  "player_tracking": {
    "enabled": false,
    "local_player_id": "Steam|76561198011111111|0",
    "auto_fetch_ranks": true
  }
}`
	if err := os.WriteFile(jsonPath, []byte(jsonContent), 0644); err != nil {
		t.Fatalf("failed to write json: %v", err)
	}

	cfgJSON, err := config.Load(config.CLIFlags{ConfigPath: jsonPath})
	if err != nil {
		t.Fatalf("unexpected error loading json: %v", err)
	}

	if !cfgJSON.PollingAuth.Enabled {
		t.Errorf("expected polling auth enabled true")
	}
	if cfgJSON.PollingAuth.Provider != "epic" {
		t.Errorf("expected polling provider epic, got %q", cfgJSON.PollingAuth.Provider)
	}
	if cfgJSON.PollingAuth.Epic.RefreshToken != "secondary-epic-token" {
		t.Errorf("expected polling refresh token 'secondary-epic-token', got %q", cfgJSON.PollingAuth.Epic.RefreshToken)
	}
	if cfgJSON.PollingAuth.Epic.AccountID != "secondary-account-id" {
		t.Errorf("expected account id 'secondary-account-id', got %q", cfgJSON.PollingAuth.Epic.AccountID)
	}
	if cfgJSON.PlayerTracking.Enabled {
		t.Errorf("expected player tracking enabled false")
	}
	if cfgJSON.PlayerTracking.LocalPlayerID != "Steam|76561198011111111|0" {
		t.Errorf("expected local player id 'Steam|76561198011111111|0', got %q", cfgJSON.PlayerTracking.LocalPlayerID)
	}
}

func TestConfig_PollingAuth_EnvOverrides(t *testing.T) {
	t.Setenv("RL_SYNC_AUTH_PROVIDER", "steam")
	t.Setenv("RL_SYNC_STEAM_SESSION_TICKET", "primary-ticket")
	t.Setenv("RL_SYNC_STEAM_ID_64", "76561198000000001")
	t.Setenv("RL_SYNC_BALLCHASING_API_KEY", "valid-key")

	t.Setenv("RL_SYNC_POLLING_AUTH_ENABLED", "true")
	t.Setenv("RL_SYNC_POLLING_AUTH_PROVIDER", "epic")
	t.Setenv("RL_SYNC_POLLING_EPIC_REFRESH_TOKEN", "env-polling-token")
	t.Setenv("RL_SYNC_POLLING_EPIC_ACCOUNT_ID", "env-polling-account")
	t.Setenv("RL_SYNC_POLLING_EPIC_DISPLAY_NAME", "EnvPoller")
	t.Setenv("RL_SYNC_POLLING_STEAM_SESSION_TICKET", "env-polling-steam-ticket")
	t.Setenv("RL_SYNC_POLLING_STEAM_ID_64", "76561198099999999")
	t.Setenv("RL_SYNC_POLLING_STEAM_ACCOUNT_NAME", "EnvSteamPoller")

	cfg, err := config.Load(config.CLIFlags{})
	if err != nil {
		t.Fatalf("failed to load config with env vars: %v", err)
	}

	if !cfg.PollingAuth.Enabled {
		t.Errorf("expected polling auth enabled true")
	}
	if cfg.PollingAuth.Provider != "epic" {
		t.Errorf("expected polling provider epic, got %q", cfg.PollingAuth.Provider)
	}
	if cfg.PollingAuth.Epic.RefreshToken != "env-polling-token" {
		t.Errorf("expected env-polling-token, got %q", cfg.PollingAuth.Epic.RefreshToken)
	}
	if cfg.PollingAuth.Epic.AccountID != "env-polling-account" {
		t.Errorf("expected env-polling-account, got %q", cfg.PollingAuth.Epic.AccountID)
	}
	if cfg.PollingAuth.Epic.DisplayName != "EnvPoller" {
		t.Errorf("expected EnvPoller, got %q", cfg.PollingAuth.Epic.DisplayName)
	}
	if cfg.PollingAuth.Steam.SessionTicket != "env-polling-steam-ticket" {
		t.Errorf("expected env-polling-steam-ticket, got %q", cfg.PollingAuth.Steam.SessionTicket)
	}
	if cfg.PollingAuth.Steam.SteamID64 != "76561198099999999" {
		t.Errorf("expected 76561198099999999, got %q", cfg.PollingAuth.Steam.SteamID64)
	}
}

func TestConfig_PollingAuth_AuthCodeAliases(t *testing.T) {
	// 1. Test RL_SYNC_POLLING_EPIC_AUTH_CODE
	t.Run("auth_code", func(t *testing.T) {
		t.Setenv("RL_SYNC_AUTH_PROVIDER", "steam")
		t.Setenv("RL_SYNC_STEAM_SESSION_TICKET", "ticket")
		t.Setenv("RL_SYNC_STEAM_ID_64", "76561198000000001")
		t.Setenv("RL_SYNC_BALLCHASING_API_KEY", "key")
		t.Setenv("RL_SYNC_POLLING_AUTH_ENABLED", "true")
		t.Setenv("RL_SYNC_POLLING_AUTH_PROVIDER", "epic")
		t.Setenv("RL_SYNC_POLLING_EPIC_AUTH_CODE", "code-alias-1")

		cfg, err := config.Load(config.CLIFlags{})
		if err != nil {
			t.Fatalf("failed to load: %v", err)
		}
		if cfg.PollingAuth.Epic.AuthCode != "code-alias-1" {
			t.Errorf("expected 'code-alias-1', got %q", cfg.PollingAuth.Epic.AuthCode)
		}
	})

	// 2. Test RL_SYNC_POLLING_EPIC_AUTHORIZATION_CODE
	t.Run("authorization_code", func(t *testing.T) {
		t.Setenv("RL_SYNC_AUTH_PROVIDER", "steam")
		t.Setenv("RL_SYNC_STEAM_SESSION_TICKET", "ticket")
		t.Setenv("RL_SYNC_STEAM_ID_64", "76561198000000001")
		t.Setenv("RL_SYNC_BALLCHASING_API_KEY", "key")
		t.Setenv("RL_SYNC_POLLING_AUTH_ENABLED", "true")
		t.Setenv("RL_SYNC_POLLING_AUTH_PROVIDER", "epic")
		t.Setenv("RL_SYNC_POLLING_EPIC_AUTHORIZATION_CODE", "code-alias-2")

		cfg, err := config.Load(config.CLIFlags{})
		if err != nil {
			t.Fatalf("failed to load: %v", err)
		}
		if cfg.PollingAuth.Epic.AuthCode != "code-alias-2" {
			t.Errorf("expected 'code-alias-2', got %q", cfg.PollingAuth.Epic.AuthCode)
		}
	})
}

func TestConfig_PlayerTracking_EnvOverridesAndAliases(t *testing.T) {
	t.Run("canonical_names", func(t *testing.T) {
		t.Setenv("RL_SYNC_EPIC_REFRESH_TOKEN", "valid-token")
		t.Setenv("RL_SYNC_BALLCHASING_API_KEY", "valid-key")
		t.Setenv("RL_SYNC_PLAYER_TRACKING_ENABLED", "false")
		t.Setenv("RL_SYNC_PLAYER_TRACKING_LOCAL_PLAYER_ID", "Epic|canon-id|0")
		t.Setenv("RL_SYNC_PLAYER_TRACKING_LOCAL_PLAYER_NAME", "CanonName")
		t.Setenv("RL_SYNC_PLAYER_TRACKING_AUTO_FETCH_RANKS", "false")

		cfg, err := config.Load(config.CLIFlags{})
		if err != nil {
			t.Fatalf("failed to load: %v", err)
		}
		if cfg.PlayerTracking.Enabled {
			t.Errorf("expected player tracking enabled false")
		}
		if cfg.PlayerTracking.LocalPlayerID != "Epic|canon-id|0" {
			t.Errorf("expected 'Epic|canon-id|0', got %q", cfg.PlayerTracking.LocalPlayerID)
		}
		if cfg.PlayerTracking.LocalPlayerName != "CanonName" {
			t.Errorf("expected 'CanonName', got %q", cfg.PlayerTracking.LocalPlayerName)
		}
		if cfg.PlayerTracking.AutoFetchRanks {
			t.Errorf("expected auto fetch ranks false")
		}
	})

	t.Run("dispatch_shorthand_aliases", func(t *testing.T) {
		t.Setenv("RL_SYNC_EPIC_REFRESH_TOKEN", "valid-token")
		t.Setenv("RL_SYNC_BALLCHASING_API_KEY", "valid-key")
		t.Setenv("RL_SYNC_PLAYER_TRACKING_ENABLED", "true")
		t.Setenv("RL_SYNC_LOCAL_PLAYER_ID", "Steam|76561198000000001|0")
		t.Setenv("RL_SYNC_LOCAL_PLAYER_NAME", "ShortName")
		t.Setenv("RL_SYNC_AUTO_FETCH_RANKS", "false")

		cfg, err := config.Load(config.CLIFlags{})
		if err != nil {
			t.Fatalf("failed to load: %v", err)
		}
		if cfg.PlayerTracking.LocalPlayerID != "Steam|76561198000000001|0" {
			t.Errorf("expected 'Steam|76561198000000001|0', got %q", cfg.PlayerTracking.LocalPlayerID)
		}
		if cfg.PlayerTracking.LocalPlayerName != "ShortName" {
			t.Errorf("expected 'ShortName', got %q", cfg.PlayerTracking.LocalPlayerName)
		}
		if cfg.PlayerTracking.AutoFetchRanks {
			t.Errorf("expected auto fetch ranks false")
		}
	})
}

func TestConfig_PollingAuth_Validation(t *testing.T) {
	tests := []struct {
		name          string
		modify        func(c *config.Config)
		expectErr     bool
		expectedError string
	}{
		{
			name: "disabled_with_no_credentials_passes",
			modify: func(c *config.Config) {
				c.PollingAuth.Enabled = false
				c.PollingAuth.Epic.RefreshToken = ""
			},
			expectErr: false,
		},
		{
			name: "disabled_with_invalid_provider_fails",
			modify: func(c *config.Config) {
				c.PollingAuth.Enabled = false
				c.PollingAuth.Provider = "nintendo"
			},
			expectErr:     true,
			expectedError: "polling_auth: invalid provider \"nintendo\"",
		},
		{
			name: "enabled_epic_missing_credentials",
			modify: func(c *config.Config) {
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "epic"
				c.PollingAuth.Epic.RefreshToken = ""
				c.PollingAuth.Epic.AuthCode = ""
			},
			expectErr: false,
		},
		{
			name: "enabled_epic_whitespace_credentials",
			modify: func(c *config.Config) {
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "epic"
				c.PollingAuth.Epic.RefreshToken = "   "
				c.PollingAuth.Epic.AuthCode = "   "
			},
			expectErr: false,
		},
		{
			name: "enabled_epic_valid_refresh_token",
			modify: func(c *config.Config) {
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "epic"
				c.PollingAuth.Epic.RefreshToken = "secondary-refresh-token"
			},
			expectErr: false,
		},
		{
			name: "enabled_epic_valid_auth_code",
			modify: func(c *config.Config) {
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "epic"
				c.PollingAuth.Epic.AuthCode = "secondary-auth-code"
			},
			expectErr: false,
		},
		{
			name: "enabled_steam_missing_ticket",
			modify: func(c *config.Config) {
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "steam"
				c.PollingAuth.Steam.SessionTicket = ""
				c.PollingAuth.Steam.SteamID64 = "76561198099999999"
			},
			expectErr:     true,
			expectedError: "polling_auth: steam provider requires 'session_ticket'",
		},
		{
			name: "enabled_steam_missing_steam_id_64",
			modify: func(c *config.Config) {
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "steam"
				c.PollingAuth.Steam.SessionTicket = "ticket"
				c.PollingAuth.Steam.SteamID64 = ""
			},
			expectErr:     true,
			expectedError: "polling_auth: steam provider requires 'steam_id_64'",
		},
		{
			name: "enabled_steam_valid",
			modify: func(c *config.Config) {
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "steam"
				c.PollingAuth.Steam.SessionTicket = "ticket"
				c.PollingAuth.Steam.SteamID64 = "76561198099999999"
			},
			expectErr: false,
		},
		{
			name: "enabled_invalid_provider",
			modify: func(c *config.Config) {
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "playstation"
			},
			expectErr:     true,
			expectedError: "polling_auth: invalid provider \"playstation\"",
		},
		{
			name: "enabled_provider_normalization_uppercase_with_whitespace",
			modify: func(c *config.Config) {
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "  EPIC  "
				c.PollingAuth.Epic.RefreshToken = "valid-token"
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.NewDefaultConfig()
			cfg.Auth.Epic.RefreshToken = "primary-valid-token"
			cfg.Ballchasing.APIKey = "valid-key"

			tt.modify(cfg)

			err := cfg.Validate()
			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.expectedError)
				}
				if !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("expected error containing %q, got %q", tt.expectedError, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("expected validation success, got error: %v", err)
				}
			}
		})
	}
}

func TestConfig_DuplicateCredentials_Error67Prevention(t *testing.T) {
	tests := []struct {
		name          string
		modify        func(c *config.Config)
		expectErr     bool
		expectedError string
	}{
		{
			name: "epic_matching_refresh_tokens_triggers_error67_protection",
			modify: func(c *config.Config) {
				c.Auth.Provider = "epic"
				c.Auth.Epic.RefreshToken = "duplicate-token"
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "epic"
				c.PollingAuth.Epic.RefreshToken = "duplicate-token"
			},
			expectErr:     true,
			expectedError: "polling_auth: credentials conflict with primary auth (same account causes PsyNet Error 67 kick)",
		},
		{
			name: "epic_matching_account_ids_triggers_error67_protection",
			modify: func(c *config.Config) {
				c.Auth.Provider = "epic"
				c.Auth.Epic.RefreshToken = "token-1"
				c.Auth.Epic.AccountID = "acct-12345"
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "epic"
				c.PollingAuth.Epic.RefreshToken = "token-2"
				c.PollingAuth.Epic.AccountID = "acct-12345"
			},
			expectErr:     true,
			expectedError: "polling_auth: credentials conflict with primary auth (same account causes PsyNet Error 67 kick)",
		},
		{
			name: "epic_matching_auth_codes_triggers_error67_protection",
			modify: func(c *config.Config) {
				c.Auth.Provider = "epic"
				c.Auth.Epic.AuthCode = "code-123"
				c.Auth.Epic.RefreshToken = ""
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "epic"
				c.PollingAuth.Epic.AuthCode = "code-123"
				c.PollingAuth.Epic.RefreshToken = ""
			},
			expectErr:     true,
			expectedError: "polling_auth: credentials conflict with primary auth (same account causes PsyNet Error 67 kick)",
		},
		{
			name: "steam_matching_session_tickets_triggers_error67_protection",
			modify: func(c *config.Config) {
				c.Auth.Provider = "steam"
				c.Auth.Steam.SessionTicket = "ticket-same"
				c.Auth.Steam.SteamID64 = "76561198000000001"
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "steam"
				c.PollingAuth.Steam.SessionTicket = "ticket-same"
				c.PollingAuth.Steam.SteamID64 = "76561198000000002"
			},
			expectErr:     true,
			expectedError: "polling_auth: credentials conflict with primary auth (same account causes PsyNet Error 67 kick)",
		},
		{
			name: "steam_matching_steam_id_64_triggers_error67_protection",
			modify: func(c *config.Config) {
				c.Auth.Provider = "steam"
				c.Auth.Steam.SessionTicket = "ticket-primary"
				c.Auth.Steam.SteamID64 = "76561198000000001"
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "steam"
				c.PollingAuth.Steam.SessionTicket = "ticket-secondary"
				c.PollingAuth.Steam.SteamID64 = "76561198000000001"
			},
			expectErr:     true,
			expectedError: "polling_auth: credentials conflict with primary auth (same account causes PsyNet Error 67 kick)",
		},
		{
			name: "different_providers_epic_and_steam_passes",
			modify: func(c *config.Config) {
				c.Auth.Provider = "epic"
				c.Auth.Epic.RefreshToken = "token-1"
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "steam"
				c.PollingAuth.Steam.SessionTicket = "ticket-1"
				c.PollingAuth.Steam.SteamID64 = "76561198099999999"
			},
			expectErr: false,
		},
		{
			name: "epic_different_tokens_passes",
			modify: func(c *config.Config) {
				c.Auth.Provider = "epic"
				c.Auth.Epic.RefreshToken = "primary-token"
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "epic"
				c.PollingAuth.Epic.RefreshToken = "secondary-token"
			},
			expectErr: false,
		},
		{
			name: "same_tokens_but_polling_disabled_passes",
			modify: func(c *config.Config) {
				c.Auth.Provider = "epic"
				c.Auth.Epic.RefreshToken = "same-token"
				c.PollingAuth.Enabled = false
				c.PollingAuth.Provider = "epic"
				c.PollingAuth.Epic.RefreshToken = "same-token"
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.NewDefaultConfig()
			cfg.Ballchasing.APIKey = "valid-key"

			tt.modify(cfg)

			err := cfg.Validate()
			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.expectedError)
				}
				if !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("expected error containing %q, got %q", tt.expectedError, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("expected validation success, got error: %v", err)
				}
			}
		})
	}
}

func TestConfig_EnvVarInvalidTypes_PollingAndPlayerTracking(t *testing.T) {
	invalidCases := []struct {
		envKey string
		envVal string
	}{
		{"RL_SYNC_POLLING_AUTH_ENABLED", "not-a-bool"},
		{"RL_SYNC_PLAYER_TRACKING_ENABLED", "invalid"},
		{"RL_SYNC_AUTO_FETCH_RANKS", "unknown"},
		{"RL_SYNC_PLAYER_TRACKING_AUTO_FETCH_RANKS", "yes"},
	}

	for _, tc := range invalidCases {
		t.Run(tc.envKey, func(t *testing.T) {
			t.Setenv(tc.envKey, tc.envVal)
			_, err := config.Load(config.CLIFlags{})
			if err == nil {
				t.Fatalf("expected error loading invalid bool for %s=%s, got nil", tc.envKey, tc.envVal)
			}
			if !strings.Contains(err.Error(), tc.envKey) {
				t.Errorf("expected error containing %q, got %q", tc.envKey, err.Error())
			}
		})
	}
}

func TestConfig_CLIOverrides_PollingAndPlayerTracking(t *testing.T) {
	pollingEnabled := true
	pollingProvider := "steam"
	trackingEnabled := false
	localID := "Epic|cli-guid|0"
	localName := "CLIName"
	autoFetch := false

	t.Setenv("RL_SYNC_POLLING_STEAM_SESSION_TICKET", "cli-ticket")
	t.Setenv("RL_SYNC_POLLING_STEAM_ID_64", "76561198000000002")
	t.Setenv("RL_SYNC_EPIC_REFRESH_TOKEN", "cli-primary-token")
	t.Setenv("RL_SYNC_BALLCHASING_API_KEY", "cli-bc-key")

	cfg, err := config.Load(config.CLIFlags{
		PollingAuthEnabled:    &pollingEnabled,
		PollingAuthProvider:   &pollingProvider,
		PlayerTrackingEnabled: &trackingEnabled,
		LocalPlayerID:         &localID,
		LocalPlayerName:       &localName,
		AutoFetchRanks:        &autoFetch,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.PollingAuth.Enabled {
		t.Errorf("expected polling auth enabled true")
	}
	if cfg.PollingAuth.Provider != "steam" {
		t.Errorf("expected polling provider steam, got %q", cfg.PollingAuth.Provider)
	}
	if cfg.PlayerTracking.Enabled {
		t.Errorf("expected player tracking enabled false")
	}
	if cfg.PlayerTracking.LocalPlayerID != "Epic|cli-guid|0" {
		t.Errorf("expected local player id 'Epic|cli-guid|0', got %q", cfg.PlayerTracking.LocalPlayerID)
	}
	if cfg.PlayerTracking.LocalPlayerName != "CLIName" {
		t.Errorf("expected local player name 'CLIName', got %q", cfg.PlayerTracking.LocalPlayerName)
	}
	if cfg.PlayerTracking.AutoFetchRanks {
		t.Errorf("expected auto fetch ranks false")
	}
}

func TestConfig_Web_YAMLAndJSON(t *testing.T) {
	yamlContent := `
auth:
  provider: "epic"
  epic:
    refresh_token: "test-epic-token"
ballchasing:
  api_key: "test-ballchasing-key"
web:
  enabled: false
  host: "127.0.0.1"
  port: 8080
`
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write test yaml: %v", err)
	}

	cfg, err := config.Load(config.CLIFlags{ConfigPath: yamlPath})
	if err != nil {
		t.Fatalf("unexpected error loading yaml: %v", err)
	}
	if cfg.Web.Enabled {
		t.Errorf("expected web enabled false from YAML, got true")
	}
	if cfg.Web.Host != "127.0.0.1" {
		t.Errorf("expected web host '127.0.0.1', got %q", cfg.Web.Host)
	}
	if cfg.Web.Port != 8080 {
		t.Errorf("expected web port 8080, got %d", cfg.Web.Port)
	}

	// JSON unmarshaling parity
	jsonContent := `{"auth":{"provider":"epic","epic":{"refresh_token":"tok"}},"ballchasing":{"api_key":"bc"},"web":{"enabled":true,"host":"192.168.1.100","port":49200}}`
	jsonPath := filepath.Join(tmpDir, "config.json")
	if err := os.WriteFile(jsonPath, []byte(jsonContent), 0o600); err != nil {
		t.Fatalf("failed to write test json: %v", err)
	}

	cfgJSON, err := config.Load(config.CLIFlags{ConfigPath: jsonPath})
	if err != nil {
		t.Fatalf("unexpected error loading json: %v", err)
	}
	if !cfgJSON.Web.Enabled {
		t.Errorf("expected web enabled true from JSON")
	}
	if cfgJSON.Web.Host != "192.168.1.100" {
		t.Errorf("expected web host '192.168.1.100', got %q", cfgJSON.Web.Host)
	}
	if cfgJSON.Web.Port != 49200 {
		t.Errorf("expected web port 49200, got %d", cfgJSON.Web.Port)
	}
}

func TestConfig_Web_EnvOverrides(t *testing.T) {
	t.Setenv("RL_SYNC_EPIC_REFRESH_TOKEN", "token")
	t.Setenv("RL_SYNC_BALLCHASING_API_KEY", "key")
	t.Setenv("RL_SYNC_WEB_ENABLED", "false")
	t.Setenv("RL_SYNC_WEB_HOST", "10.0.0.50")
	t.Setenv("RL_SYNC_WEB_PORT", "49300")

	cfg, err := config.Load(config.CLIFlags{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Web.Enabled {
		t.Errorf("expected web enabled false via env")
	}
	if cfg.Web.Host != "10.0.0.50" {
		t.Errorf("expected web host '10.0.0.50', got %q", cfg.Web.Host)
	}
	if cfg.Web.Port != 49300 {
		t.Errorf("expected web port 49300, got %d", cfg.Web.Port)
	}
}

func TestConfig_Web_EnvInvalid(t *testing.T) {
	t.Setenv("RL_SYNC_EPIC_REFRESH_TOKEN", "token")
	t.Setenv("RL_SYNC_BALLCHASING_API_KEY", "key")

	t.Run("invalid_enabled", func(t *testing.T) {
		t.Setenv("RL_SYNC_WEB_ENABLED", "not-a-bool")
		_, err := config.Load(config.CLIFlags{})
		if err == nil {
			t.Fatal("expected error for invalid RL_SYNC_WEB_ENABLED")
		}
		if !strings.Contains(err.Error(), "RL_SYNC_WEB_ENABLED") {
			t.Errorf("expected error message mentioning RL_SYNC_WEB_ENABLED, got: %v", err)
		}
	})

	t.Run("invalid_port", func(t *testing.T) {
		t.Setenv("RL_SYNC_WEB_ENABLED", "true")
		t.Setenv("RL_SYNC_WEB_PORT", "invalid-port")
		_, err := config.Load(config.CLIFlags{})
		if err == nil {
			t.Fatal("expected error for invalid RL_SYNC_WEB_PORT")
		}
		if !strings.Contains(err.Error(), "RL_SYNC_WEB_PORT") {
			t.Errorf("expected error message mentioning RL_SYNC_WEB_PORT, got: %v", err)
		}
	})
}

func TestConfig_Web_Validation(t *testing.T) {
	tests := []struct {
		name        string
		enabled     bool
		host        string
		port        int
		expectValid bool
		errSubstr   string
	}{
		{
			name:        "valid default config",
			enabled:     true,
			host:        "0.0.0.0",
			port:        49125,
			expectValid: true,
		},
		{
			name:        "empty host when enabled",
			enabled:     true,
			host:        "",
			port:        49125,
			expectValid: false,
			errSubstr:   "web: 'host' cannot be empty",
		},
		{
			name:        "whitespace host when enabled",
			enabled:     true,
			host:        "   ",
			port:        49125,
			expectValid: false,
			errSubstr:   "web: 'host' cannot be empty",
		},
		{
			name:        "port zero when enabled",
			enabled:     true,
			host:        "0.0.0.0",
			port:        0,
			expectValid: false,
			errSubstr:   "web: 'port' must be between 1 and 65535",
		},
		{
			name:        "negative port when enabled",
			enabled:     true,
			host:        "0.0.0.0",
			port:        -1,
			expectValid: false,
			errSubstr:   "web: 'port' must be between 1 and 65535",
		},
		{
			name:        "port too high when enabled",
			enabled:     true,
			host:        "0.0.0.0",
			port:        65536,
			expectValid: false,
			errSubstr:   "web: 'port' must be between 1 and 65535",
		},
		{
			name:        "port 1 boundary when enabled",
			enabled:     true,
			host:        "127.0.0.1",
			port:        1,
			expectValid: true,
		},
		{
			name:        "port 65535 boundary when enabled",
			enabled:     true,
			host:        "127.0.0.1",
			port:        65535,
			expectValid: true,
		},
		{
			name:        "disabled with empty host and port zero is valid",
			enabled:     false,
			host:        "",
			port:        0,
			expectValid: true,
		},
		{
			name:        "disabled with negative port is invalid",
			enabled:     false,
			host:        "",
			port:        -1,
			expectValid: false,
			errSubstr:   "web: 'port' must be between 0 and 65535",
		},
		{
			name:        "disabled with port 70000 is invalid",
			enabled:     false,
			host:        "",
			port:        70000,
			expectValid: false,
			errSubstr:   "web: 'port' must be between 0 and 65535",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.NewDefaultConfig()
			cfg.Auth.Epic.RefreshToken = "valid-token"
			cfg.Ballchasing.APIKey = "valid-key"
			cfg.Web.Enabled = tc.enabled
			cfg.Web.Host = tc.host
			cfg.Web.Port = tc.port

			err := cfg.Validate()
			if tc.expectValid {
				if err != nil {
					t.Fatalf("expected valid config, got error: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tc.errSubstr) {
					t.Errorf("expected error containing %q, got: %v", tc.errSubstr, err)
				}
			}
		})
	}
}

func TestConfig_Web_CLIOverrides(t *testing.T) {
	t.Setenv("RL_SYNC_EPIC_REFRESH_TOKEN", "token")
	t.Setenv("RL_SYNC_BALLCHASING_API_KEY", "key")
	t.Setenv("RL_SYNC_WEB_HOST", "env-host")
	t.Setenv("RL_SYNC_WEB_PORT", "49100")
	t.Setenv("RL_SYNC_WEB_ENABLED", "true")

	webEnabled := false
	webHost := "cli-host"
	webPort := 49199

	cfg, err := config.Load(config.CLIFlags{
		WebEnabled: &webEnabled,
		WebHost:    &webHost,
		WebPort:    &webPort,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Web.Enabled {
		t.Errorf("expected CLI to override web enabled to false")
	}
	if cfg.Web.Host != "cli-host" {
		t.Errorf("expected CLI web host 'cli-host', got %q", cfg.Web.Host)
	}
	if cfg.Web.Port != 49199 {
		t.Errorf("expected CLI web port 49199, got %d", cfg.Web.Port)
	}
}
