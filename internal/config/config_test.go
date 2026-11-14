package config_test

import (
	"encoding/json"
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
	if cfg.Web.Host != "127.0.0.1" {
		t.Errorf("expected default web host '127.0.0.1', got %q", cfg.Web.Host)
	}
	if cfg.Web.Port != 49125 {
		t.Errorf("expected default web port 49125, got %d", cfg.Web.Port)
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

func TestConfig_SessionContinueWindow(t *testing.T) {
	t.Run("default_is_2h", func(t *testing.T) {
		cfg := config.NewDefaultConfig()
		if cfg.EffectiveSessionContinueWindow() != 2*time.Hour {
			t.Fatalf("expected default continue window 2h, got %v", cfg.EffectiveSessionContinueWindow())
		}
	})

	t.Run("env_and_cli_overrides", func(t *testing.T) {
		t.Setenv("RL_SYNC_EPIC_REFRESH_TOKEN", "token")
		t.Setenv("RL_SYNC_BALLCHASING_API_KEY", "key")
		t.Setenv("RL_SYNC_SESSION_CONTINUE_WINDOW", "90m")

		cfgEnv, err := config.Load(config.CLIFlags{})
		if err != nil {
			t.Fatalf("unexpected error loading env override: %v", err)
		}
		if cfgEnv.EffectiveSessionContinueWindow() != 90*time.Minute {
			t.Fatalf("expected env continue window 90m, got %v", cfgEnv.EffectiveSessionContinueWindow())
		}

		cliWin := 45 * time.Minute
		cfgCLI, err := config.Load(config.CLIFlags{
			SessionContinueWindow: &cliWin,
		})
		if err != nil {
			t.Fatalf("unexpected error loading CLI override: %v", err)
		}
		if cfgCLI.EffectiveSessionContinueWindow() != 45*time.Minute {
			t.Fatalf("expected CLI continue window 45m, got %v", cfgCLI.EffectiveSessionContinueWindow())
		}

		zeroWin := time.Duration(0)
		cfgZero, err := config.Load(config.CLIFlags{
			SessionContinueWindow: &zeroWin,
		})
		if err != nil {
			t.Fatalf("unexpected error loading 0s CLI override: %v", err)
		}
		if cfgZero.EffectiveSessionContinueWindow() != 0 {
			t.Fatalf("expected 0 continue window when disabled, got %v", cfgZero.EffectiveSessionContinueWindow())
		}
	})

	t.Run("negative_continue_window_rejected", func(t *testing.T) {
		cfg := config.NewDefaultConfig()
		cfg.Auth.Epic.RefreshToken = "token"
		cfg.Ballchasing.APIKey = "key"
		cfg.Session.ContinueWindow = config.Duration(-10 * time.Minute)
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), "session: 'continue_window' cannot be negative") {
			t.Fatalf("expected negative continue_window validation error, got: %v", err)
		}
	})
}
