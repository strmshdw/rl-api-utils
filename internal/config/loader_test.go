package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
)

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

func TestConfig_MissingExplicitConfigFile(t *testing.T) {
	_, err := config.Load(config.CLIFlags{
		ConfigPath: "/nonexistent/path/to/config.yaml",
	})
	if err == nil {
		t.Fatal("expected error loading non-existent explicit config file, got nil")
	}
}

func TestConfig_Load_RLSyncConfigEnvAndAutoDiscovery(t *testing.T) {
	t.Run("rl_sync_config_env", func(t *testing.T) {
		tmpDir := t.TempDir()
		envPath := filepath.Join(tmpDir, "custom.yaml")
		if err := os.WriteFile(envPath, []byte("ballchasing:\n  api_key: env-file-key\n"), 0644); err != nil {
			t.Fatalf("failed to write yaml: %v", err)
		}
		t.Setenv("RL_SYNC_CONFIG", envPath)

		cfg, err := config.Load(config.CLIFlags{})
		if err != nil {
			t.Fatalf("expected RL_SYNC_CONFIG load to succeed, got: %v", err)
		}
		if cfg.ConfigPath != envPath {
			t.Errorf("expected ConfigPath %q, got %q", envPath, cfg.ConfigPath)
		}
		if cfg.Ballchasing.APIKey != "env-file-key" {
			t.Errorf("expected APIKey 'env-file-key', got %q", cfg.Ballchasing.APIKey)
		}
	})

	t.Run("rl_sync_config_env_missing_fails_fast", func(t *testing.T) {
		t.Setenv("RL_SYNC_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))
		if _, err := config.Load(config.CLIFlags{}); err == nil {
			t.Fatal("expected missing RL_SYNC_CONFIG file to fail fast")
		}
	})

	t.Run("cwd_auto_discovery_yml", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Chdir(tmpDir)
		if err := os.WriteFile("config.yml", []byte("ballchasing:\n  api_key: cwd-yml-key\n"), 0644); err != nil {
			t.Fatalf("failed to write config.yml: %v", err)
		}

		cfg, err := config.Load(config.CLIFlags{})
		if err != nil {
			t.Fatalf("expected auto-discovered config.yml to load, got: %v", err)
		}
		if cfg.ConfigPath != "config.yml" {
			t.Errorf("expected ConfigPath 'config.yml', got %q", cfg.ConfigPath)
		}
		if cfg.Ballchasing.APIKey != "cwd-yml-key" {
			t.Errorf("expected APIKey 'cwd-yml-key', got %q", cfg.Ballchasing.APIKey)
		}
	})
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
