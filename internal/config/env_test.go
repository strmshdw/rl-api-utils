package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
)

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
