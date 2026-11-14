package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
)

func TestBoundary_EnvVarBooleans(t *testing.T) {
	truthy := []string{"1", "t", "T", "true", "TRUE", "True"}
	falsy := []string{"0", "f", "F", "false", "FALSE", "False"}

	for _, val := range truthy {
		t.Run("truthy_"+val, func(t *testing.T) {
			t.Setenv("RL_SYNC_DRY_RUN", val)
			t.Setenv("RL_SYNC_ONCE", val)
			t.Setenv("RL_SYNC_KEEP_LOCAL_FILES", val)
			t.Setenv("RL_SYNC_EPIC_REFRESH_TOKEN", "valid-token")
			t.Setenv("RL_SYNC_BALLCHASING_API_KEY", "valid-key")

			cfg, err := config.Load(config.CLIFlags{})
			if err != nil {
				t.Fatalf("unexpected error for boolean %q: %v", val, err)
			}
			if !cfg.Sync.DryRun || !cfg.Sync.Once || !cfg.Sync.KeepLocalFiles {
				t.Fatalf("expected all bools to be true for %q, got DryRun=%v, Once=%v, KeepLocalFiles=%v",
					val, cfg.Sync.DryRun, cfg.Sync.Once, cfg.Sync.KeepLocalFiles)
			}
		})
	}

	for _, val := range falsy {
		t.Run("falsy_"+val, func(t *testing.T) {
			t.Setenv("RL_SYNC_DRY_RUN", val)
			t.Setenv("RL_SYNC_ONCE", val)
			t.Setenv("RL_SYNC_KEEP_LOCAL_FILES", val)
			t.Setenv("RL_SYNC_EPIC_REFRESH_TOKEN", "valid-token")
			t.Setenv("RL_SYNC_BALLCHASING_API_KEY", "valid-key")

			cfg, err := config.Load(config.CLIFlags{})
			if err != nil {
				t.Fatalf("unexpected error for boolean %q: %v", val, err)
			}
			if cfg.Sync.DryRun || cfg.Sync.Once || cfg.Sync.KeepLocalFiles {
				t.Fatalf("expected all bools to be false for %q, got DryRun=%v, Once=%v, KeepLocalFiles=%v",
					val, cfg.Sync.DryRun, cfg.Sync.Once, cfg.Sync.KeepLocalFiles)
			}
		})
	}
}

func TestBoundary_EnvVarInvalidTypes(t *testing.T) {
	invalidCases := []struct {
		name   string
		envKey string
		envVal string
	}{
		{"invalid_boolean_string", "RL_SYNC_DRY_RUN", "yes_please"},
		{"invalid_max_retries_string", "RL_SYNC_BALLCHASING_MAX_RETRIES", "three"},
		{"invalid_max_retries_float", "RL_SYNC_BALLCHASING_MAX_RETRIES", "3.5"},
		{"invalid_poll_interval_syntax", "RL_SYNC_POLL_INTERVAL", "10minutes"},
		{"invalid_ballchasing_timeout_syntax", "RL_SYNC_BALLCHASING_TIMEOUT", "invalid_time"},
		{"invalid_download_timeout_syntax", "RL_SYNC_DOWNLOAD_TIMEOUT", "bad_val"},
		{"invalid_continue_window", "RL_SYNC_CONTINUE_WINDOW", "2days"},
		{"invalid_stats_api_enabled", "RL_SYNC_STATS_API_ENABLED", "yep"},
		{"invalid_trigger_threshold", "RL_SYNC_TRIGGER_THRESHOLD", "fifteen"},
		{"invalid_force_sync_on_trigger", "RL_SYNC_FORCE_SYNC_ON_TRIGGER", "nope"},
		{"invalid_enable_toast", "RL_SYNC_ENABLE_TOAST", "sure"},
		{"invalid_http_trigger_port", "RL_SYNC_HTTP_TRIGGER_PORT", "port"},
		{"invalid_auto_sync_on_exit", "RL_SYNC_AUTO_SYNC_ON_EXIT", "nah"},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("RL_SYNC_EPIC_REFRESH_TOKEN", "valid-token")
			t.Setenv("RL_SYNC_BALLCHASING_API_KEY", "valid-key")
			t.Setenv(tc.envKey, tc.envVal)

			_, err := config.Load(config.CLIFlags{})
			if err == nil {
				t.Fatalf("expected error when setting %s=%q, got nil", tc.envKey, tc.envVal)
			}
			if !strings.Contains(err.Error(), tc.envKey) {
				t.Errorf("expected error mentioning %s, got: %v", tc.envKey, err)
			}
		})
	}
}

func TestBoundary_EnvVarEmptyStringVsUnset(t *testing.T) {
	t.Setenv("RL_SYNC_AUTH_PROVIDER", "")
	t.Setenv("RL_SYNC_BALLCHASING_VISIBILITY", "")
	t.Setenv("RL_SYNC_REPLAY_DIR", "")
	t.Setenv("RL_SYNC_DB_PATH", "")
	t.Setenv("RL_SYNC_LOG_LEVEL", "")
	t.Setenv("RL_SYNC_LOG_FORMAT", "")
	t.Setenv("RL_SYNC_EPIC_REFRESH_TOKEN", "valid-token")
	t.Setenv("RL_SYNC_BALLCHASING_API_KEY", "valid-key")

	cfg, err := config.Load(config.CLIFlags{})
	if err != nil {
		t.Fatalf("unexpected error when env vars are empty: %v", err)
	}

	if cfg.Auth.Provider != "epic" {
		t.Errorf("expected default provider 'epic', got %q", cfg.Auth.Provider)
	}
	if cfg.Ballchasing.Visibility != "public" {
		t.Errorf("expected default visibility 'public', got %q", cfg.Ballchasing.Visibility)
	}
	if cfg.Sync.ReplayDir != "./replays" {
		t.Errorf("expected default replay dir './replays', got %q", cfg.Sync.ReplayDir)
	}
	if cfg.Sync.DBPath != "./rl-sync.db" {
		t.Errorf("expected default db path './rl-sync.db', got %q", cfg.Sync.DBPath)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("expected default logging level 'info', got %q", cfg.Logging.Level)
	}
	if cfg.Logging.Format != "text" {
		t.Errorf("expected default logging format 'text', got %q", cfg.Logging.Format)
	}
}

func TestConfig_EnvOverrides_RemainingSections(t *testing.T) {
	t.Setenv("RL_SYNC_AUTH_PROVIDER", "epic")
	t.Setenv("RL_SYNC_EPIC_REFRESH_TOKEN", "epic-refresh")
	t.Setenv("RL_SYNC_EPIC_AUTH_CODE", "epic-code")
	t.Setenv("RL_SYNC_EPIC_ACCOUNT_ID", "epic-acct")
	t.Setenv("RL_SYNC_EPIC_DISPLAY_NAME", "EpicUser")
	t.Setenv("RL_SYNC_STEAM_USERNAME", "steam-user")
	t.Setenv("RL_SYNC_STEAM_PASSWORD", "steam-pass")
	t.Setenv("RL_SYNC_STEAM_LOGIN_KEY", "steam-key")
	t.Setenv("RL_SYNC_STEAM_ACCOUNT_NAME", "SteamAcct")
	t.Setenv("RL_SYNC_POLLING_STEAM_USERNAME", "poll-user")
	t.Setenv("RL_SYNC_POLLING_STEAM_PASSWORD", "poll-pass")
	t.Setenv("RL_SYNC_POLLING_STEAM_LOGIN_KEY", "poll-key")
	t.Setenv("RL_SYNC_CONTINUE_WINDOW", "3h")
	t.Setenv("RL_SYNC_BALLCHASING_API_KEY", "bc-key")
	t.Setenv("RL_SYNC_BALLCHASING_BASE_URL", "https://example.test/api")
	t.Setenv("RL_SYNC_BALLCHASING_TIMEOUT", "45s")
	t.Setenv("RL_SYNC_BALLCHASING_MAX_RETRIES", "7")
	t.Setenv("RL_SYNC_REPLAY_DIR", "/data/replays")
	t.Setenv("RL_SYNC_DB_PATH", "/data/state.db")
	t.Setenv("RL_SYNC_DOWNLOAD_TIMEOUT", "12s")
	t.Setenv("RL_SYNC_STATS_API_ENABLED", "false")
	t.Setenv("RL_SYNC_STATS_API_ADDRESS", "127.0.0.1:49999")
	t.Setenv("RL_SYNC_STATS_API_PROTOCOL", "TCP")
	t.Setenv("RL_SYNC_TRIGGER_THRESHOLD", "25")
	t.Setenv("RL_SYNC_FORCE_SYNC_ON_TRIGGER", "true")
	t.Setenv("RL_SYNC_ENABLE_TOAST", "false")
	t.Setenv("RL_SYNC_HTTP_TRIGGER_PORT", "48000")
	t.Setenv("RL_SYNC_AUTO_SYNC_ON_EXIT", "false")
	t.Setenv("RL_SYNC_LOG_LEVEL", "WARN")
	t.Setenv("RL_SYNC_LOG_FORMAT", "JSON")

	cfg, err := config.Load(config.CLIFlags{})
	if err != nil {
		t.Fatalf("unexpected error loading env overrides: %v", err)
	}

	if cfg.Auth.Epic.AuthCode != "epic-code" || cfg.Auth.Epic.AccountID != "epic-acct" || cfg.Auth.Epic.DisplayName != "EpicUser" {
		t.Errorf("unexpected epic auth fields: %+v", cfg.Auth.Epic)
	}
	if cfg.Auth.Steam.Username != "steam-user" || cfg.Auth.Steam.Password != "steam-pass" || cfg.Auth.Steam.LoginKey != "steam-key" || cfg.Auth.Steam.AccountName != "SteamAcct" {
		t.Errorf("unexpected steam auth fields: %+v", cfg.Auth.Steam)
	}
	if cfg.PollingAuth.Steam.Username != "poll-user" || cfg.PollingAuth.Steam.Password != "poll-pass" || cfg.PollingAuth.Steam.LoginKey != "poll-key" {
		t.Errorf("unexpected polling steam auth fields: %+v", cfg.PollingAuth.Steam)
	}
	if cfg.Session.ContinueWindow.Duration() != 3*time.Hour {
		t.Errorf("expected continue window 3h, got %v", cfg.Session.ContinueWindow)
	}
	if cfg.Ballchasing.BaseURL != "https://example.test/api" || cfg.Ballchasing.Timeout.Duration() != 45*time.Second || cfg.Ballchasing.MaxRetries != 7 {
		t.Errorf("unexpected ballchasing fields: %+v", cfg.Ballchasing)
	}
	if cfg.Sync.ReplayDir != "/data/replays" || cfg.Sync.DBPath != "/data/state.db" || cfg.Sync.DownloadTimeout.Duration() != 12*time.Second {
		t.Errorf("unexpected sync fields: %+v", cfg.Sync)
	}
	if cfg.StatsAPI.Enabled || cfg.StatsAPI.Address != "127.0.0.1:49999" || cfg.StatsAPI.Protocol != "tcp" ||
		cfg.StatsAPI.TriggerThreshold != 25 || !cfg.StatsAPI.ForceSyncOnTrigger ||
		cfg.StatsAPI.EnableToast || cfg.StatsAPI.HTTPTriggerPort != 48000 || cfg.StatsAPI.AutoSyncOnExit {
		t.Errorf("unexpected stats_api fields: %+v", cfg.StatsAPI)
	}
	if cfg.Logging.Level != "warn" || cfg.Logging.Format != "json" {
		t.Errorf("unexpected logging fields: %+v", cfg.Logging)
	}
}
