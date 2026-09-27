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
	ConfigPath     string               `yaml:"-" json:"-"`
	Auth           AuthConfig           `yaml:"auth" json:"auth"`
	PollingAuth    PollingAuthConfig    `yaml:"polling_auth" json:"polling_auth"`
	PlayerTracking PlayerTrackingConfig `yaml:"player_tracking" json:"player_tracking"`
	Ballchasing    BallchasingConfig    `yaml:"ballchasing" json:"ballchasing"`
	Sync           SyncConfig           `yaml:"sync" json:"sync"`
	StatsAPI       StatsAPIConfig       `yaml:"stats_api" json:"stats_api"`
	Logging        LoggingConfig        `yaml:"logging" json:"logging"`
	Web            WebConfig            `yaml:"web" json:"web"`
}

// WebConfig configures the embedded web dashboard and HTTP server.
type WebConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"` // default: true
	Host    string `yaml:"host" json:"host"`       // default: "0.0.0.0"
	Port    int    `yaml:"port" json:"port"`       // default: 49125
}

// StatsAPIConfig configures connection to Rocket League's local Stats API (MatchStatsExporter_TA).
type StatsAPIConfig struct {
	Enabled            bool   `yaml:"enabled" json:"enabled"`                                 // default: true
	Address            string `yaml:"address" json:"address"`                                 // default: "127.0.0.1:49124"
	Protocol           string `yaml:"protocol" json:"protocol"`                               // "websocket" or "tcp", default: "websocket"
	TriggerThreshold   int    `yaml:"trigger_threshold" json:"trigger_threshold"`             // default: 15
	ForceSyncOnTrigger bool   `yaml:"force_sync_on_trigger" json:"force_sync_on_trigger"`     // default: false
	EnableToast        bool   `yaml:"enable_toast" json:"enable_toast"`                       // default: true
	HTTPTriggerPort    int    `yaml:"http_trigger_port" json:"http_trigger_port"`             // default: 49125 (0 to disable)
	AutoSyncOnExit     bool   `yaml:"auto_sync_on_exit" json:"auto_sync_on_exit"`             // default: true
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
	Username      string `yaml:"username" json:"username"`
	Password      string `yaml:"password" json:"password"`
	SessionTicket string `yaml:"session_ticket" json:"session_ticket"`
	SteamID64     string `yaml:"steam_id_64" json:"steam_id_64"`
	AccountName   string `yaml:"account_name" json:"account_name"`
	LoginKey      string `yaml:"login_key" json:"login_key"`
}

// PollingAuthConfig configures authentication for the secondary non-playing PsyNet account
// used to fetch player ranks and MMR without triggering Error 67 duplicate login kicks.
type PollingAuthConfig struct {
	Enabled  bool        `yaml:"enabled" json:"enabled"`
	Provider string      `yaml:"provider" json:"provider"` // "epic" or "steam"
	Epic     EpicConfig  `yaml:"epic" json:"epic"`
	Steam    SteamConfig `yaml:"steam" json:"steam"`
}

// ToAuthConfig converts a PollingAuthConfig into a standard AuthConfig
// suitable for passing directly into auth.NewProvider.
func (p PollingAuthConfig) ToAuthConfig() AuthConfig {
	return AuthConfig{
		Provider: p.Provider,
		Epic:     p.Epic,
		Steam:    p.Steam,
	}
}

// PlayerTrackingConfig configures in-game player tracking and rank retrieval.
type PlayerTrackingConfig struct {
	Enabled         bool   `yaml:"enabled" json:"enabled"`                     // default: true
	LocalPlayerID   string `yaml:"local_player_id" json:"local_player_id"`     // optional override (e.g. "Epic|<id>|0" or "Steam|<id>|0")
	LocalPlayerName string `yaml:"local_player_name" json:"local_player_name"` // optional override for player display name
	AutoFetchRanks  bool   `yaml:"auto_fetch_ranks" json:"auto_fetch_ranks"`   // default: true
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
	ConfigPath            string
	Once                  *bool
	DryRun                *bool
	LogLevel              *string
	LogFormat             *string
	PollInterval          *time.Duration
	ReplayDir             *string
	DBPath                *string
	Provider              *string
	TriggerThreshold      *int
	ForceSyncOnTrigger    *bool
	StatsAPIEnabled       *bool
	PollingAuthEnabled    *bool
	PollingAuthProvider   *string
	PlayerTrackingEnabled *bool
	LocalPlayerID         *string
	LocalPlayerName       *string
	AutoFetchRanks        *bool
	WebEnabled            *bool
	WebHost               *string
	WebPort               *int
}

// NewDefaultConfig returns a Config populated with baseline default settings.
func NewDefaultConfig() *Config {
	return &Config{
		Auth: AuthConfig{
			Provider: "epic",
		},
		PollingAuth: PollingAuthConfig{
			Enabled:  false,
			Provider: "epic",
		},
		PlayerTracking: PlayerTrackingConfig{
			Enabled:         true,
			LocalPlayerID:   "",
			LocalPlayerName: "",
			AutoFetchRanks:  true,
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
		StatsAPI: StatsAPIConfig{
			Enabled:            true,
			Address:            "127.0.0.1:49124",
			Protocol:           "websocket",
			TriggerThreshold:   15,
			ForceSyncOnTrigger: false,
			EnableToast:        true,
			HTTPTriggerPort:    49125,
			AutoSyncOnExit:     true,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "text",
		},
		Web: WebConfig{
			Enabled: true,
			Host:    "0.0.0.0",
			Port:    49125,
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
	if val := getEnv("RL_SYNC_STEAM_USERNAME"); val != "" {
		c.Auth.Steam.Username = val
	}
	if val := getEnv("RL_SYNC_STEAM_PASSWORD"); val != "" {
		c.Auth.Steam.Password = val
	}
	if val := getEnv("RL_SYNC_STEAM_LOGIN_KEY"); val != "" {
		c.Auth.Steam.LoginKey = val
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

	// Polling Auth environment overrides
	if val := getEnv("RL_SYNC_POLLING_AUTH_ENABLED"); val != "" {
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid RL_SYNC_POLLING_AUTH_ENABLED %q: %w", val, err)
		}
		c.PollingAuth.Enabled = b
	}
	if val := getEnv("RL_SYNC_POLLING_AUTH_PROVIDER"); val != "" {
		c.PollingAuth.Provider = strings.ToLower(strings.TrimSpace(val))
	}
	if val := getEnv("RL_SYNC_POLLING_EPIC_REFRESH_TOKEN"); val != "" {
		c.PollingAuth.Epic.RefreshToken = val
	}
	if val := getEnv("RL_SYNC_POLLING_EPIC_AUTH_CODE"); val != "" {
		c.PollingAuth.Epic.AuthCode = val
	}
	if val := getEnv("RL_SYNC_POLLING_EPIC_AUTHORIZATION_CODE"); val != "" {
		c.PollingAuth.Epic.AuthCode = val
	}
	if val := getEnv("RL_SYNC_POLLING_EPIC_ACCOUNT_ID"); val != "" {
		c.PollingAuth.Epic.AccountID = val
	}
	if val := getEnv("RL_SYNC_POLLING_EPIC_DISPLAY_NAME"); val != "" {
		c.PollingAuth.Epic.DisplayName = val
	}
	if val := getEnv("RL_SYNC_POLLING_STEAM_USERNAME"); val != "" {
		c.PollingAuth.Steam.Username = val
	}
	if val := getEnv("RL_SYNC_POLLING_STEAM_PASSWORD"); val != "" {
		c.PollingAuth.Steam.Password = val
	}
	if val := getEnv("RL_SYNC_POLLING_STEAM_LOGIN_KEY"); val != "" {
		c.PollingAuth.Steam.LoginKey = val
	}
	if val := getEnv("RL_SYNC_POLLING_STEAM_SESSION_TICKET"); val != "" {
		c.PollingAuth.Steam.SessionTicket = val
	}
	if val := getEnv("RL_SYNC_POLLING_STEAM_ID_64"); val != "" {
		c.PollingAuth.Steam.SteamID64 = val
	}
	if val := getEnv("RL_SYNC_POLLING_STEAM_ACCOUNT_NAME"); val != "" {
		c.PollingAuth.Steam.AccountName = val
	}

	// Player Tracking environment overrides
	if val := getEnv("RL_SYNC_PLAYER_TRACKING_ENABLED"); val != "" {
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid RL_SYNC_PLAYER_TRACKING_ENABLED %q: %w", val, err)
		}
		c.PlayerTracking.Enabled = b
	}
	if val := getEnv("RL_SYNC_PLAYER_TRACKING_LOCAL_PLAYER_ID"); val != "" {
		c.PlayerTracking.LocalPlayerID = strings.TrimSpace(val)
	}
	if val := getEnv("RL_SYNC_LOCAL_PLAYER_ID"); val != "" {
		c.PlayerTracking.LocalPlayerID = strings.TrimSpace(val)
	}
	if val := getEnv("RL_SYNC_PLAYER_TRACKING_LOCAL_PLAYER_NAME"); val != "" {
		c.PlayerTracking.LocalPlayerName = strings.TrimSpace(val)
	}
	if val := getEnv("RL_SYNC_LOCAL_PLAYER_NAME"); val != "" {
		c.PlayerTracking.LocalPlayerName = strings.TrimSpace(val)
	}
	if val := getEnv("RL_SYNC_PLAYER_TRACKING_AUTO_FETCH_RANKS"); val != "" {
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid RL_SYNC_PLAYER_TRACKING_AUTO_FETCH_RANKS %q: %w", val, err)
		}
		c.PlayerTracking.AutoFetchRanks = b
	}
	if val := getEnv("RL_SYNC_AUTO_FETCH_RANKS"); val != "" {
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid RL_SYNC_AUTO_FETCH_RANKS %q: %w", val, err)
		}
		c.PlayerTracking.AutoFetchRanks = b
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
	if val := getEnv("RL_SYNC_STATS_API_ENABLED"); val != "" {
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid RL_SYNC_STATS_API_ENABLED %q: %w", val, err)
		}
		c.StatsAPI.Enabled = b
	}
	if val := getEnv("RL_SYNC_STATS_API_ADDRESS"); val != "" {
		c.StatsAPI.Address = strings.TrimSpace(val)
	}
	if val := getEnv("RL_SYNC_STATS_API_PROTOCOL"); val != "" {
		c.StatsAPI.Protocol = strings.ToLower(strings.TrimSpace(val))
	}
	if val := getEnv("RL_SYNC_TRIGGER_THRESHOLD"); val != "" {
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("invalid RL_SYNC_TRIGGER_THRESHOLD %q: %w", val, err)
		}
		c.StatsAPI.TriggerThreshold = n
	}
	if val := getEnv("RL_SYNC_FORCE_SYNC_ON_TRIGGER"); val != "" {
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid RL_SYNC_FORCE_SYNC_ON_TRIGGER %q: %w", val, err)
		}
		c.StatsAPI.ForceSyncOnTrigger = b
	}
	if val := getEnv("RL_SYNC_ENABLE_TOAST"); val != "" {
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid RL_SYNC_ENABLE_TOAST %q: %w", val, err)
		}
		c.StatsAPI.EnableToast = b
	}
	if val := getEnv("RL_SYNC_HTTP_TRIGGER_PORT"); val != "" {
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("invalid RL_SYNC_HTTP_TRIGGER_PORT %q: %w", val, err)
		}
		c.StatsAPI.HTTPTriggerPort = n
	}
	if val := getEnv("RL_SYNC_AUTO_SYNC_ON_EXIT"); val != "" {
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid RL_SYNC_AUTO_SYNC_ON_EXIT %q: %w", val, err)
		}
		c.StatsAPI.AutoSyncOnExit = b
	}

	if val := getEnv("RL_SYNC_LOG_LEVEL"); val != "" {
		c.Logging.Level = strings.ToLower(strings.TrimSpace(val))
	}
	if val := getEnv("RL_SYNC_LOG_FORMAT"); val != "" {
		c.Logging.Format = strings.ToLower(strings.TrimSpace(val))
	}

	// Web environment overrides
	if val := getEnv("RL_SYNC_WEB_ENABLED"); val != "" {
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid RL_SYNC_WEB_ENABLED %q: %w", val, err)
		}
		c.Web.Enabled = b
	}
	if val := getEnv("RL_SYNC_WEB_HOST"); val != "" {
		c.Web.Host = strings.TrimSpace(val)
	}
	if val := getEnv("RL_SYNC_WEB_PORT"); val != "" {
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("invalid RL_SYNC_WEB_PORT %q: %w", val, err)
		}
		c.Web.Port = n
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

	// Normalize provider
	c.Auth.Provider = strings.ToLower(strings.TrimSpace(c.Auth.Provider))
	switch c.Auth.Provider {
	case "epic":
		// Valid: RefreshToken/AuthCode can be provided, or prompt interactively on startup
	case "steam":
		if strings.TrimSpace(c.Auth.Steam.SessionTicket) == "" {
			if strings.TrimSpace(c.Auth.Steam.Username) == "" ||
				(strings.TrimSpace(c.Auth.Steam.Password) == "" && strings.TrimSpace(c.Auth.Steam.LoginKey) == "") {
				errs = append(errs, errors.New("auth: steam provider requires 'session_ticket' and 'steam_id_64' (or 'username' and 'password'/'login_key')"))
			}
		} else if strings.TrimSpace(c.Auth.Steam.SteamID64) == "" {
			errs = append(errs, errors.New("auth: steam provider requires 'steam_id_64' when 'session_ticket' is provided"))
		}
	default:
		errs = append(errs, fmt.Errorf("auth: invalid provider %q (must be 'epic' or 'steam')", c.Auth.Provider))
	}

	// Validate PollingAuth (enforced when PollingAuth is enabled)
	if c.PollingAuth.Enabled {
		c.PollingAuth.Provider = strings.ToLower(strings.TrimSpace(c.PollingAuth.Provider))
		switch c.PollingAuth.Provider {
		case "epic":
			// Valid: RefreshToken/AuthCode can be provided, or prompt interactively on startup
		case "steam":
			if strings.TrimSpace(c.PollingAuth.Steam.SessionTicket) == "" {
				if strings.TrimSpace(c.PollingAuth.Steam.Username) == "" ||
					(strings.TrimSpace(c.PollingAuth.Steam.Password) == "" && strings.TrimSpace(c.PollingAuth.Steam.LoginKey) == "") {
					errs = append(errs, errors.New("polling_auth: steam provider requires 'session_ticket' and 'steam_id_64' (or 'username' and 'password'/'login_key')"))
				}
			} else if strings.TrimSpace(c.PollingAuth.Steam.SteamID64) == "" {
				errs = append(errs, errors.New("polling_auth: steam provider requires 'steam_id_64' when 'session_ticket' is provided"))
			}
		default:
			errs = append(errs, fmt.Errorf("polling_auth: invalid provider %q (must be 'epic' or 'steam')", c.PollingAuth.Provider))
		}

		// Anti-collision / Duplicate Credential Check:
		// Using the same credentials for both primary auth and secondary polling auth
		// causes PsyNet Error 67 kicks on the active game client.
		if c.Auth.Provider == c.PollingAuth.Provider {
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
					errs = append(errs, errors.New("polling_auth: credentials conflict with primary auth (same account causes PsyNet Error 67 kick)"))
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
					errs = append(errs, errors.New("polling_auth: credentials conflict with primary auth (same account causes PsyNet Error 67 kick)"))
				}
			}
		}
	} else if c.PollingAuth.Provider != "" {
		c.PollingAuth.Provider = strings.ToLower(strings.TrimSpace(c.PollingAuth.Provider))
		if c.PollingAuth.Provider != "epic" && c.PollingAuth.Provider != "steam" {
			errs = append(errs, fmt.Errorf("polling_auth: invalid provider %q (must be 'epic' or 'steam')", c.PollingAuth.Provider))
		}
	}

	// Normalize PlayerTracking
	c.PlayerTracking.LocalPlayerID = strings.TrimSpace(c.PlayerTracking.LocalPlayerID)
	c.PlayerTracking.LocalPlayerName = strings.TrimSpace(c.PlayerTracking.LocalPlayerName)

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

	// Validate Web
	if c.Web.Enabled {
		c.Web.Host = strings.TrimSpace(c.Web.Host)
		if c.Web.Host == "" {
			errs = append(errs, errors.New("web: 'host' cannot be empty"))
		}
		if c.Web.Port < 1 || c.Web.Port > 65535 {
			errs = append(errs, fmt.Errorf("web: 'port' must be between 1 and 65535 (got %d)", c.Web.Port))
		}
	} else if c.Web.Port < 0 || c.Web.Port > 65535 {
		errs = append(errs, fmt.Errorf("web: 'port' must be between 0 and 65535 (got %d)", c.Web.Port))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
