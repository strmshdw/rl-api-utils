package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func (c *Config) loadConfigFile(path string) error {
	filePath := path
	if filePath == "" {
		if envPath := strings.TrimSpace(os.Getenv("RL_SYNC_CONFIG")); envPath != "" {
			filePath = envPath
		}
	}
	if filePath == "" {
		candidates := []string{"config.yaml", "config.yml", "config.json"}
		for _, cand := range candidates {
			if _, err := os.Stat(cand); err == nil {
				filePath = cand
				break
			}
		}
		if filePath == "" {
			return nil // No default config file found; proceed with defaults + env
		}
	} else {
		if _, err := os.Stat(filePath); err != nil {
			return fmt.Errorf("specified config file not found: %w", err)
		}
	}

	c.ConfigPath = filePath

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("reading config file %s: %w", filePath, err)
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == ".json" {
		if err := json.Unmarshal(data, c); err != nil {
			return fmt.Errorf("parsing JSON config %s: %w", filePath, err)
		}
		return nil
	}

	// Default to YAML for .yaml, .yml, or unspecified extension
	if err := yaml.Unmarshal(data, c); err != nil {
		// Fallback attempt with JSON if YAML unmarshaling fails
		if jsonErr := json.Unmarshal(data, c); jsonErr == nil {
			return nil
		}
		return fmt.Errorf("parsing YAML config %s: %w", filePath, err)
	}

	return nil
}

func (c *Config) applyCLI(cli CLIFlags) {
	if cli.Once != nil {
		c.Sync.Once = *cli.Once
	}
	if cli.DryRun != nil {
		c.Sync.DryRun = *cli.DryRun
	}
	if cli.LogLevel != nil && *cli.LogLevel != "" {
		c.Logging.Level = strings.ToLower(strings.TrimSpace(*cli.LogLevel))
	}
	if cli.LogFormat != nil && *cli.LogFormat != "" {
		c.Logging.Format = strings.ToLower(strings.TrimSpace(*cli.LogFormat))
	}
	if cli.PollInterval != nil && *cli.PollInterval > 0 {
		c.Sync.PollInterval = Duration(*cli.PollInterval)
	}
	if cli.ReplayDir != nil && *cli.ReplayDir != "" {
		c.Sync.ReplayDir = *cli.ReplayDir
	}
	if cli.DBPath != nil && *cli.DBPath != "" {
		c.Sync.DBPath = *cli.DBPath
	}
	if cli.Provider != nil && *cli.Provider != "" {
		c.Auth.Provider = strings.ToLower(strings.TrimSpace(*cli.Provider))
	}
	if cli.TriggerThreshold != nil && *cli.TriggerThreshold > 0 {
		c.StatsAPI.TriggerThreshold = *cli.TriggerThreshold
	}
	if cli.ForceSyncOnTrigger != nil {
		c.StatsAPI.ForceSyncOnTrigger = *cli.ForceSyncOnTrigger
	}
	if cli.StatsAPIEnabled != nil {
		c.StatsAPI.Enabled = *cli.StatsAPIEnabled
	}
	if cli.PollingAuthEnabled != nil {
		c.PollingAuth.Enabled = *cli.PollingAuthEnabled
	}
	if cli.PollingAuthProvider != nil && *cli.PollingAuthProvider != "" {
		c.PollingAuth.Provider = strings.ToLower(strings.TrimSpace(*cli.PollingAuthProvider))
	}
	if cli.PlayerTrackingEnabled != nil {
		c.PlayerTracking.Enabled = *cli.PlayerTrackingEnabled
	}
	if cli.LocalPlayerID != nil && *cli.LocalPlayerID != "" {
		c.PlayerTracking.LocalPlayerID = strings.TrimSpace(*cli.LocalPlayerID)
	}
	if cli.LocalPlayerName != nil && *cli.LocalPlayerName != "" {
		c.PlayerTracking.LocalPlayerName = strings.TrimSpace(*cli.LocalPlayerName)
	}
	if cli.AutoFetchRanks != nil {
		c.PlayerTracking.AutoFetchRanks = *cli.AutoFetchRanks
	}
	if cli.SessionContinueWindow != nil {
		c.Session.ContinueWindow = Duration(*cli.SessionContinueWindow)
	}
	if cli.WebEnabled != nil {
		c.Web.Enabled = *cli.WebEnabled
	}
	if cli.WebHost != nil {
		c.Web.Host = strings.TrimSpace(*cli.WebHost)
	}
	if cli.WebPort != nil {
		c.Web.Port = *cli.WebPort
	}
}

// Validate verifies all configuration fields adhere to semantic constraints.
// It accumulates all validation errors using errors.Join.
func (c *Config) Validate() error {
	var errs []error

	c.validateAuth(&errs)
	c.validatePollingAuth(&errs)
	c.validatePlayerTrackingAndSession(&errs)
	c.validateBallchasing(&errs)
	c.validateSync(&errs)
	c.validateLogging(&errs)
	c.validateWeb(&errs)

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func validateSteamCredentials(section string, steam SteamConfig) error {
	if strings.TrimSpace(steam.SessionTicket) == "" {
		if strings.TrimSpace(steam.Username) == "" ||
			(strings.TrimSpace(steam.Password) == "" && strings.TrimSpace(steam.LoginKey) == "") {
			return fmt.Errorf("%s: steam provider requires 'session_ticket' and 'steam_id_64' (or 'username' and 'password'/'login_key')", section)
		}
		return nil
	}
	if strings.TrimSpace(steam.SteamID64) == "" {
		return fmt.Errorf("%s: steam provider requires 'steam_id_64' when 'session_ticket' is provided", section)
	}
	return nil
}

func (c *Config) validateAuth(errs *[]error) {
	c.Auth.Provider = strings.ToLower(strings.TrimSpace(c.Auth.Provider))
	switch c.Auth.Provider {
	case "epic":
		// RefreshToken/AuthCode can be provided, or prompted interactively on startup.
	case "steam":
		if err := validateSteamCredentials("auth", c.Auth.Steam); err != nil {
			*errs = append(*errs, err)
		}
	default:
		*errs = append(*errs, fmt.Errorf("auth: invalid provider %q (must be 'epic' or 'steam')", c.Auth.Provider))
	}
}

func (c *Config) validatePollingAuth(errs *[]error) {
	if c.PollingAuth.Enabled {
		c.PollingAuth.Provider = strings.ToLower(strings.TrimSpace(c.PollingAuth.Provider))
		switch c.PollingAuth.Provider {
		case "epic":
			// RefreshToken/AuthCode can be provided, or prompted interactively on startup.
		case "steam":
			if err := validateSteamCredentials("polling_auth", c.PollingAuth.Steam); err != nil {
				*errs = append(*errs, err)
			}
		default:
			*errs = append(*errs, fmt.Errorf("polling_auth: invalid provider %q (must be 'epic' or 'steam')", c.PollingAuth.Provider))
		}
		c.validateAuthConflict(errs)
		return
	}

	if c.PollingAuth.Provider != "" {
		c.PollingAuth.Provider = strings.ToLower(strings.TrimSpace(c.PollingAuth.Provider))
		if c.PollingAuth.Provider != "epic" && c.PollingAuth.Provider != "steam" {
			*errs = append(*errs, fmt.Errorf("polling_auth: invalid provider %q (must be 'epic' or 'steam')", c.PollingAuth.Provider))
		}
	}
}

// validateAuthConflict prevents using identical credentials for primary auth and secondary
// polling auth, which triggers PsyNet Error 67 duplicate login kicks.
func (c *Config) validateAuthConflict(errs *[]error) {
	if c.Auth.Provider != c.PollingAuth.Provider {
		return
	}
	switch c.PollingAuth.Provider {
	case "epic":
		primaryRefresh := strings.TrimSpace(c.Auth.Epic.RefreshToken)
		pollingRefresh := strings.TrimSpace(c.PollingAuth.Epic.RefreshToken)
		primaryCode := strings.TrimSpace(c.Auth.Epic.AuthCode)
		pollingCode := strings.TrimSpace(c.PollingAuth.Epic.AuthCode)
		primaryAcct := strings.TrimSpace(c.Auth.Epic.AccountID)
		pollingAcct := strings.TrimSpace(c.PollingAuth.Epic.AccountID)

		if (primaryRefresh != "" && primaryRefresh == pollingRefresh) ||
			(primaryCode != "" && primaryCode == pollingCode) ||
			(primaryAcct != "" && primaryAcct == pollingAcct) {
			*errs = append(*errs, errors.New("polling_auth: credentials conflict with primary auth (same account causes PsyNet Error 67 kick)"))
		}
	case "steam":
		primaryTicket := strings.TrimSpace(c.Auth.Steam.SessionTicket)
		pollingTicket := strings.TrimSpace(c.PollingAuth.Steam.SessionTicket)
		primarySteamID := strings.TrimSpace(c.Auth.Steam.SteamID64)
		pollingSteamID := strings.TrimSpace(c.PollingAuth.Steam.SteamID64)
		primaryUser := strings.ToLower(strings.TrimSpace(c.Auth.Steam.Username))
		pollingUser := strings.ToLower(strings.TrimSpace(c.PollingAuth.Steam.Username))

		if (primaryTicket != "" && primaryTicket == pollingTicket) ||
			(primarySteamID != "" && primarySteamID == pollingSteamID) ||
			(primaryUser != "" && primaryUser == pollingUser) {
			*errs = append(*errs, errors.New("polling_auth: credentials conflict with primary auth (same account causes PsyNet Error 67 kick)"))
		}
	}
}

func (c *Config) validatePlayerTrackingAndSession(errs *[]error) {
	c.PlayerTracking.LocalPlayerID = strings.TrimSpace(c.PlayerTracking.LocalPlayerID)
	c.PlayerTracking.LocalPlayerName = strings.TrimSpace(c.PlayerTracking.LocalPlayerName)
	if c.PlayerTracking.SessionContinueWindow != 0 && c.Session.ContinueWindow == Duration(DefaultSessionContinueWindow) {
		c.Session.ContinueWindow = c.PlayerTracking.SessionContinueWindow
	}
	if c.Session.ContinueWindow.Duration() < 0 {
		*errs = append(*errs, errors.New("session: 'continue_window' cannot be negative"))
	}
}

func (c *Config) validateBallchasing(errs *[]error) {
	if strings.TrimSpace(c.Ballchasing.APIKey) == "" {
		*errs = append(*errs, errors.New("ballchasing: 'api_key' is required"))
	}
	c.Ballchasing.Visibility = strings.ToLower(strings.TrimSpace(c.Ballchasing.Visibility))
	switch c.Ballchasing.Visibility {
	case "public", "unlisted", "private":
	default:
		*errs = append(*errs, fmt.Errorf("ballchasing: invalid visibility %q (must be 'public', 'unlisted', or 'private')", c.Ballchasing.Visibility))
	}
	if strings.TrimSpace(c.Ballchasing.BaseURL) == "" {
		*errs = append(*errs, errors.New("ballchasing: 'base_url' cannot be empty"))
	} else {
		u, err := url.Parse(c.Ballchasing.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			*errs = append(*errs, fmt.Errorf("ballchasing: invalid base_url %q (must be http or https)", c.Ballchasing.BaseURL))
		}
	}
	if c.Ballchasing.Timeout.Duration() <= 0 {
		*errs = append(*errs, errors.New("ballchasing: 'timeout' must be positive"))
	}
	if c.Ballchasing.MaxRetries < 0 {
		*errs = append(*errs, errors.New("ballchasing: 'max_retries' cannot be negative"))
	}
}

func (c *Config) validateSync(errs *[]error) {
	if c.Sync.PollInterval.Duration() <= 0 {
		*errs = append(*errs, errors.New("sync: 'poll_interval' must be positive"))
	}
	if strings.TrimSpace(c.Sync.ReplayDir) == "" {
		*errs = append(*errs, errors.New("sync: 'replay_dir' cannot be empty"))
	}
	if strings.TrimSpace(c.Sync.DBPath) == "" {
		*errs = append(*errs, errors.New("sync: 'db_path' cannot be empty"))
	}
	if c.Sync.DownloadTimeout.Duration() <= 0 {
		*errs = append(*errs, errors.New("sync: 'download_timeout' must be positive"))
	}
}

func (c *Config) validateLogging(errs *[]error) {
	c.Logging.Level = strings.ToLower(strings.TrimSpace(c.Logging.Level))
	switch c.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		*errs = append(*errs, fmt.Errorf("logging: invalid level %q (must be 'debug', 'info', 'warn', or 'error')", c.Logging.Level))
	}
	c.Logging.Format = strings.ToLower(strings.TrimSpace(c.Logging.Format))
	switch c.Logging.Format {
	case "text", "json":
	default:
		*errs = append(*errs, fmt.Errorf("logging: invalid format %q (must be 'text' or 'json')", c.Logging.Format))
	}
}

func (c *Config) validateWeb(errs *[]error) {
	if c.Web.Enabled {
		c.Web.Host = strings.TrimSpace(c.Web.Host)
		if c.Web.Host == "" {
			*errs = append(*errs, errors.New("web: 'host' cannot be empty"))
		}
		if c.Web.Port < 1 || c.Web.Port > 65535 {
			*errs = append(*errs, fmt.Errorf("web: 'port' must be between 1 and 65535 (got %d)", c.Web.Port))
		}
		return
	}
	if c.Web.Port < 0 || c.Web.Port > 65535 {
		*errs = append(*errs, fmt.Errorf("web: 'port' must be between 0 and 65535 (got %d)", c.Web.Port))
	}
}
