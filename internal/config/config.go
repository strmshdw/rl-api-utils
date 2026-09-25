package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration wraps time.Duration to enable seamless JSON and YAML unmarshaling
// from human-readable strings like "5m", "30s", "1h" as well as integer nanoseconds.
type Duration time.Duration

func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

func (d Duration) String() string {
	return time.Duration(d).String()
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch val := v.(type) {
	case float64:
		*d = Duration(time.Duration(val))
		return nil
	case string:
		parsed, err := time.ParseDuration(val)
		if err != nil {
			return fmt.Errorf("invalid duration string %q: %w", val, err)
		}
		*d = Duration(parsed)
		return nil
	default:
		return fmt.Errorf("invalid duration type: %T", val)
	}
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var n int64
	if err := value.Decode(&n); err == nil {
		*d = Duration(time.Duration(n))
		return nil
	}
	var s string
	if err := value.Decode(&s); err == nil {
		parsed, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("invalid duration string %q: %w", s, err)
		}
		*d = Duration(parsed)
		return nil
	}
	return fmt.Errorf("cannot unmarshal YAML node into Duration")
}

func (d Duration) MarshalYAML() (interface{}, error) {
	return time.Duration(d).String(), nil
}

// Config represents the complete daemon configuration.
type Config struct {
	Auth        AuthConfig        `yaml:"auth" json:"auth"`
	Ballchasing BallchasingConfig `yaml:"ballchasing" json:"ballchasing"`
	Sync        SyncConfig        `yaml:"sync" json:"sync"`
	Logging     LoggingConfig     `yaml:"logging" json:"logging"`
}

// AuthConfig configures authentication for Rocket League PsyNet RPC.
type AuthConfig struct {
	Provider string      `yaml:"provider" json:"provider"` // "epic" or "steam"
	Epic     EpicConfig  `yaml:"epic" json:"epic"`
	Steam    SteamConfig `yaml:"steam" json:"steam"`
}

// EpicConfig holds Epic Games Store OAuth credentials.
type EpicConfig struct {
	RefreshToken string `yaml:"refresh_token" json:"refresh_token"`
	AuthCode     string `yaml:"auth_code" json:"auth_code"`
	AccountID    string `yaml:"account_id" json:"account_id"`
	DisplayName  string `yaml:"display_name" json:"display_name"`
}

// SteamConfig holds Steam session ticket authentication credentials.
type SteamConfig struct {
	SessionTicket string `yaml:"session_ticket" json:"session_ticket"`
	SteamID64     string `yaml:"steam_id_64" json:"steam_id_64"`
	AccountName   string `yaml:"account_name" json:"account_name"`
}

// BallchasingConfig configures replay uploads to ballchasing.com.
type BallchasingConfig struct {
	APIKey     string   `yaml:"api_key" json:"api_key"`
	Visibility string   `yaml:"visibility" json:"visibility"` // "public", "unlisted", "private"
	BaseURL    string   `yaml:"base_url" json:"base_url"`     // default: "https://ballchasing.com/api"
	Timeout    Duration `yaml:"timeout" json:"timeout"`       // default: 60s
	MaxRetries int      `yaml:"max_retries" json:"max_retries"`// default: 3
}

// SyncConfig configures synchronization polling and local persistence.
type SyncConfig struct {
	PollInterval    Duration `yaml:"poll_interval" json:"poll_interval"`       // default: 5m
	ReplayDir       string   `yaml:"replay_dir" json:"replay_dir"`             // default: "./replays"
	DBPath          string   `yaml:"db_path" json:"db_path"`                   // default: "./rl-sync.db"
	KeepLocalFiles  bool     `yaml:"keep_local_files" json:"keep_local_files"` // default: true
	DownloadTimeout Duration `yaml:"download_timeout" json:"download_timeout"` // default: 30s
	DryRun          bool     `yaml:"dry_run" json:"dry_run"`                   // default: false
	Once            bool     `yaml:"once" json:"once"`                         // default: false
}

// LoggingConfig configures structured logging output.
type LoggingConfig struct {
	Level  string `yaml:"level" json:"level"`   // "debug", "info", "warn", "error"
	Format string `yaml:"format" json:"format"` // "text", "json"
}

// CLIFlags captures optional command-line flag overrides.
type CLIFlags struct {
	ConfigPath   string
	Once         *bool
	DryRun       *bool
	LogLevel     *string
	LogFormat    *string
	PollInterval *time.Duration
	ReplayDir    *string
	DBPath       *string
	Provider     *string
}

// NewDefaultConfig returns a Config populated with baseline default settings.
func NewDefaultConfig() *Config {
	return &Config{
		Auth: AuthConfig{
			Provider: "epic",
		},
		Ballchasing: BallchasingConfig{
			Visibility: "public",
			BaseURL:    "https://ballchasing.com/api",
			Timeout:    Duration(60 * time.Second),
			MaxRetries: 3,
		},
		Sync: SyncConfig{
			PollInterval:    Duration(5 * time.Minute),
			ReplayDir:       "./replays",
			DBPath:          "./rl-sync.db",
			KeepLocalFiles:  true,
			DownloadTimeout: Duration(30 * time.Second),
			DryRun:          false,
			Once:            false,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "text",
		},
	}
}

// Load compiles a configuration from Defaults, Config File, Environment Variables, and CLI Flags.
func Load(cli CLIFlags) (*Config, error) {
	cfg := NewDefaultConfig()

	// 1. Resolve and Load Configuration File
	if err := cfg.loadConfigFile(cli.ConfigPath); err != nil {
		return nil, err
	}

	// 2. Apply Environment Variables (RL_SYNC_*)
	if err := cfg.applyEnv(); err != nil {
		return nil, err
	}

	// 3. Apply CLI Overrides
	cfg.applyCLI(cli)

	// 4. Validate Final Invariants
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) loadConfigFile(path string) error {
	filePath := path
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

func (c *Config) applyEnv() error {
	getEnv := os.Getenv

	if val := getEnv("RL_SYNC_AUTH_PROVIDER"); val != "" {
		c.Auth.Provider = strings.ToLower(strings.TrimSpace(val))
	}
	if val := getEnv("RL_SYNC_EPIC_REFRESH_TOKEN"); val != "" {
		c.Auth.Epic.RefreshToken = val
	}
	if val := getEnv("RL_SYNC_EPIC_AUTH_CODE"); val != "" {
		c.Auth.Epic.AuthCode = val
	}
	if val := getEnv("RL_SYNC_EPIC_ACCOUNT_ID"); val != "" {
		c.Auth.Epic.AccountID = val
	}
	if val := getEnv("RL_SYNC_EPIC_DISPLAY_NAME"); val != "" {
		c.Auth.Epic.DisplayName = val
	}
	if val := getEnv("RL_SYNC_STEAM_SESSION_TICKET"); val != "" {
		c.Auth.Steam.SessionTicket = val
	}
	if val := getEnv("RL_SYNC_STEAM_ID_64"); val != "" {
		c.Auth.Steam.SteamID64 = val
	}
	if val := getEnv("RL_SYNC_STEAM_ACCOUNT_NAME"); val != "" {
		c.Auth.Steam.AccountName = val
	}

	if val := getEnv("RL_SYNC_BALLCHASING_API_KEY"); val != "" {
		c.Ballchasing.APIKey = val
	}
	if val := getEnv("RL_SYNC_BALLCHASING_VISIBILITY"); val != "" {
		c.Ballchasing.Visibility = strings.ToLower(strings.TrimSpace(val))
	}
	if val := getEnv("RL_SYNC_BALLCHASING_BASE_URL"); val != "" {
		c.Ballchasing.BaseURL = val
	}
	if val := getEnv("RL_SYNC_BALLCHASING_TIMEOUT"); val != "" {
		d, err := time.ParseDuration(val)
		if err != nil {
			return fmt.Errorf("invalid RL_SYNC_BALLCHASING_TIMEOUT %q: %w", val, err)
		}
		c.Ballchasing.Timeout = Duration(d)
	}
	if val := getEnv("RL_SYNC_BALLCHASING_MAX_RETRIES"); val != "" {
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("invalid RL_SYNC_BALLCHASING_MAX_RETRIES %q: %w", val, err)
		}
		c.Ballchasing.MaxRetries = n
	}

	if val := getEnv("RL_SYNC_POLL_INTERVAL"); val != "" {
		d, err := time.ParseDuration(val)
		if err != nil {
			return fmt.Errorf("invalid RL_SYNC_POLL_INTERVAL %q: %w", val, err)
		}
		c.Sync.PollInterval = Duration(d)
	}
	if val := getEnv("RL_SYNC_REPLAY_DIR"); val != "" {
		c.Sync.ReplayDir = val
	}
	if val := getEnv("RL_SYNC_DB_PATH"); val != "" {
		c.Sync.DBPath = val
	}
	if val := getEnv("RL_SYNC_KEEP_LOCAL_FILES"); val != "" {
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid RL_SYNC_KEEP_LOCAL_FILES %q: %w", val, err)
		}
		c.Sync.KeepLocalFiles = b
	}
	if val := getEnv("RL_SYNC_DOWNLOAD_TIMEOUT"); val != "" {
		d, err := time.ParseDuration(val)
		if err != nil {
			return fmt.Errorf("invalid RL_SYNC_DOWNLOAD_TIMEOUT %q: %w", val, err)
		}
		c.Sync.DownloadTimeout = Duration(d)
	}
	if val := getEnv("RL_SYNC_DRY_RUN"); val != "" {
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid RL_SYNC_DRY_RUN %q: %w", val, err)
		}
		c.Sync.DryRun = b
	}
	if val := getEnv("RL_SYNC_ONCE"); val != "" {
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid RL_SYNC_ONCE %q: %w", val, err)
		}
		c.Sync.Once = b
	}

	if val := getEnv("RL_SYNC_LOG_LEVEL"); val != "" {
		c.Logging.Level = strings.ToLower(strings.TrimSpace(val))
	}
	if val := getEnv("RL_SYNC_LOG_FORMAT"); val != "" {
		c.Logging.Format = strings.ToLower(strings.TrimSpace(val))
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
}

// Validate verifies all configuration fields adhere to semantic constraints.
// It accumulates all validation errors using errors.Join.
func (c *Config) Validate() error {
	var errs []error

	// Normalize provider
	c.Auth.Provider = strings.ToLower(strings.TrimSpace(c.Auth.Provider))
	switch c.Auth.Provider {
	case "epic":
		if strings.TrimSpace(c.Auth.Epic.RefreshToken) == "" && strings.TrimSpace(c.Auth.Epic.AuthCode) == "" {
			errs = append(errs, errors.New("auth: epic provider requires either 'refresh_token' or 'auth_code'"))
		}
	case "steam":
		if strings.TrimSpace(c.Auth.Steam.SessionTicket) == "" {
			errs = append(errs, errors.New("auth: steam provider requires 'session_ticket'"))
		}
		if strings.TrimSpace(c.Auth.Steam.SteamID64) == "" {
			errs = append(errs, errors.New("auth: steam provider requires 'steam_id_64'"))
		}
	default:
		errs = append(errs, fmt.Errorf("auth: invalid provider %q (must be 'epic' or 'steam')", c.Auth.Provider))
	}

	// Validate Ballchasing
	if strings.TrimSpace(c.Ballchasing.APIKey) == "" {
		errs = append(errs, errors.New("ballchasing: 'api_key' is required"))
	}
	c.Ballchasing.Visibility = strings.ToLower(strings.TrimSpace(c.Ballchasing.Visibility))
	switch c.Ballchasing.Visibility {
	case "public", "unlisted", "private":
		// valid
	default:
		errs = append(errs, fmt.Errorf("ballchasing: invalid visibility %q (must be 'public', 'unlisted', or 'private')", c.Ballchasing.Visibility))
	}
	if strings.TrimSpace(c.Ballchasing.BaseURL) == "" {
		errs = append(errs, errors.New("ballchasing: 'base_url' cannot be empty"))
	} else {
		u, err := url.Parse(c.Ballchasing.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			errs = append(errs, fmt.Errorf("ballchasing: invalid base_url %q (must be http or https)", c.Ballchasing.BaseURL))
		}
	}
	if c.Ballchasing.Timeout.Duration() <= 0 {
		errs = append(errs, errors.New("ballchasing: 'timeout' must be positive"))
	}
	if c.Ballchasing.MaxRetries < 0 {
		errs = append(errs, errors.New("ballchasing: 'max_retries' cannot be negative"))
	}

	// Validate Sync
	if c.Sync.PollInterval.Duration() <= 0 {
		errs = append(errs, errors.New("sync: 'poll_interval' must be positive"))
	}
	if strings.TrimSpace(c.Sync.ReplayDir) == "" {
		errs = append(errs, errors.New("sync: 'replay_dir' cannot be empty"))
	}
	if strings.TrimSpace(c.Sync.DBPath) == "" {
		errs = append(errs, errors.New("sync: 'db_path' cannot be empty"))
	}
	if c.Sync.DownloadTimeout.Duration() <= 0 {
		errs = append(errs, errors.New("sync: 'download_timeout' must be positive"))
	}

	// Validate Logging
	c.Logging.Level = strings.ToLower(strings.TrimSpace(c.Logging.Level))
	switch c.Logging.Level {
	case "debug", "info", "warn", "error":
		// valid
	default:
		errs = append(errs, fmt.Errorf("logging: invalid level %q (must be 'debug', 'info', 'warn', or 'error')", c.Logging.Level))
	}
	c.Logging.Format = strings.ToLower(strings.TrimSpace(c.Logging.Format))
	switch c.Logging.Format {
	case "text", "json":
		// valid
	default:
		errs = append(errs, fmt.Errorf("logging: invalid format %q (must be 'text' or 'json')", c.Logging.Format))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
