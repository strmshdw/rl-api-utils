package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type envStringBinding struct {
	key       string
	dst       *string
	transform func(string) string
}

type envBoolBinding struct {
	key string
	dst *bool
}

type envIntBinding struct {
	key string
	dst *int
}

type envDurationBinding struct {
	key string
	dst *Duration
}

func identityString(v string) string {
	return v
}

func lowerTrimString(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func (c *Config) applyEnv() error {
	strBindings := []envStringBinding{
		// Primary auth
		{"RL_SYNC_AUTH_PROVIDER", &c.Auth.Provider, lowerTrimString},
		{"RL_SYNC_EPIC_REFRESH_TOKEN", &c.Auth.Epic.RefreshToken, identityString},
		{"RL_SYNC_EPIC_AUTH_CODE", &c.Auth.Epic.AuthCode, identityString},
		{"RL_SYNC_EPIC_ACCOUNT_ID", &c.Auth.Epic.AccountID, identityString},
		{"RL_SYNC_EPIC_DISPLAY_NAME", &c.Auth.Epic.DisplayName, identityString},
		{"RL_SYNC_STEAM_USERNAME", &c.Auth.Steam.Username, identityString},
		{"RL_SYNC_STEAM_PASSWORD", &c.Auth.Steam.Password, identityString},
		{"RL_SYNC_STEAM_LOGIN_KEY", &c.Auth.Steam.LoginKey, identityString},
		{"RL_SYNC_STEAM_SESSION_TICKET", &c.Auth.Steam.SessionTicket, identityString},
		{"RL_SYNC_STEAM_ID_64", &c.Auth.Steam.SteamID64, identityString},
		{"RL_SYNC_STEAM_ACCOUNT_NAME", &c.Auth.Steam.AccountName, identityString},

		// Polling auth strings
		{"RL_SYNC_POLLING_AUTH_PROVIDER", &c.PollingAuth.Provider, lowerTrimString},
		{"RL_SYNC_POLLING_EPIC_REFRESH_TOKEN", &c.PollingAuth.Epic.RefreshToken, identityString},
		{"RL_SYNC_POLLING_EPIC_AUTH_CODE", &c.PollingAuth.Epic.AuthCode, identityString},
		{"RL_SYNC_POLLING_EPIC_AUTHORIZATION_CODE", &c.PollingAuth.Epic.AuthCode, identityString},
		{"RL_SYNC_POLLING_EPIC_ACCOUNT_ID", &c.PollingAuth.Epic.AccountID, identityString},
		{"RL_SYNC_POLLING_EPIC_DISPLAY_NAME", &c.PollingAuth.Epic.DisplayName, identityString},
		{"RL_SYNC_POLLING_STEAM_USERNAME", &c.PollingAuth.Steam.Username, identityString},
		{"RL_SYNC_POLLING_STEAM_PASSWORD", &c.PollingAuth.Steam.Password, identityString},
		{"RL_SYNC_POLLING_STEAM_LOGIN_KEY", &c.PollingAuth.Steam.LoginKey, identityString},
		{"RL_SYNC_POLLING_STEAM_SESSION_TICKET", &c.PollingAuth.Steam.SessionTicket, identityString},
		{"RL_SYNC_POLLING_STEAM_ID_64", &c.PollingAuth.Steam.SteamID64, identityString},
		{"RL_SYNC_POLLING_STEAM_ACCOUNT_NAME", &c.PollingAuth.Steam.AccountName, identityString},

		// Player tracking strings (canonical then shorthand aliases)
		{"RL_SYNC_PLAYER_TRACKING_LOCAL_PLAYER_ID", &c.PlayerTracking.LocalPlayerID, strings.TrimSpace},
		{"RL_SYNC_LOCAL_PLAYER_ID", &c.PlayerTracking.LocalPlayerID, strings.TrimSpace},
		{"RL_SYNC_PLAYER_TRACKING_LOCAL_PLAYER_NAME", &c.PlayerTracking.LocalPlayerName, strings.TrimSpace},
		{"RL_SYNC_LOCAL_PLAYER_NAME", &c.PlayerTracking.LocalPlayerName, strings.TrimSpace},

		// Ballchasing, Sync, Stats API, Logging, and Web strings
		{"RL_SYNC_BALLCHASING_API_KEY", &c.Ballchasing.APIKey, identityString},
		{"RL_SYNC_BALLCHASING_VISIBILITY", &c.Ballchasing.Visibility, lowerTrimString},
		{"RL_SYNC_BALLCHASING_BASE_URL", &c.Ballchasing.BaseURL, identityString},
		{"RL_SYNC_REPLAY_DIR", &c.Sync.ReplayDir, identityString},
		{"RL_SYNC_DB_PATH", &c.Sync.DBPath, identityString},
		{"RL_SYNC_STATS_API_ADDRESS", &c.StatsAPI.Address, strings.TrimSpace},
		{"RL_SYNC_STATS_API_PROTOCOL", &c.StatsAPI.Protocol, lowerTrimString},
		{"RL_SYNC_LOG_LEVEL", &c.Logging.Level, lowerTrimString},
		{"RL_SYNC_LOG_FORMAT", &c.Logging.Format, lowerTrimString},
		{"RL_SYNC_WEB_HOST", &c.Web.Host, strings.TrimSpace},
	}
	for _, b := range strBindings {
		if val := os.Getenv(b.key); val != "" {
			*b.dst = b.transform(val)
		}
	}

	boolBindings := []envBoolBinding{
		{"RL_SYNC_POLLING_AUTH_ENABLED", &c.PollingAuth.Enabled},
		{"RL_SYNC_PLAYER_TRACKING_ENABLED", &c.PlayerTracking.Enabled},
		{"RL_SYNC_PLAYER_TRACKING_AUTO_FETCH_RANKS", &c.PlayerTracking.AutoFetchRanks},
		{"RL_SYNC_AUTO_FETCH_RANKS", &c.PlayerTracking.AutoFetchRanks},
		{"RL_SYNC_KEEP_LOCAL_FILES", &c.Sync.KeepLocalFiles},
		{"RL_SYNC_DRY_RUN", &c.Sync.DryRun},
		{"RL_SYNC_ONCE", &c.Sync.Once},
		{"RL_SYNC_STATS_API_ENABLED", &c.StatsAPI.Enabled},
		{"RL_SYNC_FORCE_SYNC_ON_TRIGGER", &c.StatsAPI.ForceSyncOnTrigger},
		{"RL_SYNC_ENABLE_TOAST", &c.StatsAPI.EnableToast},
		{"RL_SYNC_AUTO_SYNC_ON_EXIT", &c.StatsAPI.AutoSyncOnExit},
		{"RL_SYNC_WEB_ENABLED", &c.Web.Enabled},
	}
	for _, b := range boolBindings {
		if val := os.Getenv(b.key); val != "" {
			parsed, err := strconv.ParseBool(val)
			if err != nil {
				return fmt.Errorf("invalid %s %q: %w", b.key, val, err)
			}
			*b.dst = parsed
		}
	}

	intBindings := []envIntBinding{
		{"RL_SYNC_BALLCHASING_MAX_RETRIES", &c.Ballchasing.MaxRetries},
		{"RL_SYNC_TRIGGER_THRESHOLD", &c.StatsAPI.TriggerThreshold},
		{"RL_SYNC_HTTP_TRIGGER_PORT", &c.StatsAPI.HTTPTriggerPort},
		{"RL_SYNC_WEB_PORT", &c.Web.Port},
	}
	for _, b := range intBindings {
		if val := os.Getenv(b.key); val != "" {
			parsed, err := strconv.Atoi(val)
			if err != nil {
				return fmt.Errorf("invalid %s %q: %w", b.key, val, err)
			}
			*b.dst = parsed
		}
	}

	durBindings := []envDurationBinding{
		{"RL_SYNC_SESSION_CONTINUE_WINDOW", &c.Session.ContinueWindow},
		{"RL_SYNC_CONTINUE_WINDOW", &c.Session.ContinueWindow},
		{"RL_SYNC_BALLCHASING_TIMEOUT", &c.Ballchasing.Timeout},
		{"RL_SYNC_POLL_INTERVAL", &c.Sync.PollInterval},
		{"RL_SYNC_DOWNLOAD_TIMEOUT", &c.Sync.DownloadTimeout},
	}
	for _, b := range durBindings {
		if val := os.Getenv(b.key); val != "" {
			parsed, err := time.ParseDuration(val)
			if err != nil {
				return fmt.Errorf("invalid %s %q: %w", b.key, val, err)
			}
			*b.dst = Duration(parsed)
			if b.dst == &c.Session.ContinueWindow {
				c.Session.continueWindowSet = true
			}
		}
	}

	return nil
}
