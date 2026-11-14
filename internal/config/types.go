package config

import (
	"encoding/json"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration wraps time.Duration to enable seamless JSON and YAML unmarshaling
// from human-readable strings like "5m", "30s", "1h" as well as integer nanoseconds.
type Duration time.Duration

// Duration returns the underlying time.Duration value.
func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

// String formats the duration using time.Duration.String.
func (d Duration) String() string {
	return time.Duration(d).String()
}

// UnmarshalJSON parses a JSON string duration (e.g. "5m") or numeric nanoseconds.
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

// MarshalJSON encodes the duration as a human-readable JSON string.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// UnmarshalYAML parses a YAML numeric nanosecond scalar or duration string.
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

// MarshalYAML encodes the duration as a human-readable YAML string.
func (d Duration) MarshalYAML() (interface{}, error) {
	return time.Duration(d).String(), nil
}

// DefaultSessionContinueWindow is the default maximum elapsed time since the last
// match of a previous session during which that session can be continued.
const DefaultSessionContinueWindow = 2 * time.Hour

// Config represents the complete daemon configuration.
type Config struct {
	ConfigPath     string               `yaml:"-" json:"-"`
	Auth           AuthConfig           `yaml:"auth" json:"auth"`
	PollingAuth    PollingAuthConfig    `yaml:"polling_auth" json:"polling_auth"`
	PlayerTracking PlayerTrackingConfig `yaml:"player_tracking" json:"player_tracking"`
	Session        SessionConfig        `yaml:"session" json:"session"`
	Ballchasing    BallchasingConfig    `yaml:"ballchasing" json:"ballchasing"`
	Sync           SyncConfig           `yaml:"sync" json:"sync"`
	StatsAPI       StatsAPIConfig       `yaml:"stats_api" json:"stats_api"`
	Logging        LoggingConfig        `yaml:"logging" json:"logging"`
	Web            WebConfig            `yaml:"web" json:"web"`
}

// SessionConfig configures play session tracking, persistence, and continuation.
type SessionConfig struct {
	ContinueWindow    Duration `yaml:"continue_window" json:"continue_window"` // default: 2h (0 disables continuation)
	continueWindowSet bool
}

// UnmarshalYAML decodes SessionConfig while recording whether continue_window was explicitly provided.
func (s *SessionConfig) UnmarshalYAML(value *yaml.Node) error {
	var raw struct {
		ContinueWindow *Duration `yaml:"continue_window"`
	}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	if raw.ContinueWindow != nil {
		s.ContinueWindow = *raw.ContinueWindow
		s.continueWindowSet = true
	}
	return nil
}

// UnmarshalJSON decodes SessionConfig while recording whether continue_window was explicitly provided.
func (s *SessionConfig) UnmarshalJSON(b []byte) error {
	var raw struct {
		ContinueWindow *Duration `json:"continue_window"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if raw.ContinueWindow != nil {
		s.ContinueWindow = *raw.ContinueWindow
		s.continueWindowSet = true
	}
	return nil
}

// WebConfig configures the embedded web dashboard and HTTP server.
type WebConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"` // default: true
	Host    string `yaml:"host" json:"host"`       // default: "127.0.0.1"
	Port    int    `yaml:"port" json:"port"`       // default: 49125
}

// StatsAPIConfig configures connection to Rocket League's local Stats API (MatchStatsExporter_TA).
type StatsAPIConfig struct {
	Enabled            bool   `yaml:"enabled" json:"enabled"`                             // default: true
	Address            string `yaml:"address" json:"address"`                             // default: "127.0.0.1:49124"
	Protocol           string `yaml:"protocol" json:"protocol"`                           // "websocket" or "tcp", default: "websocket"
	TriggerThreshold   int    `yaml:"trigger_threshold" json:"trigger_threshold"`         // default: 15
	ForceSyncOnTrigger bool   `yaml:"force_sync_on_trigger" json:"force_sync_on_trigger"` // default: false
	EnableToast        bool   `yaml:"enable_toast" json:"enable_toast"`                   // default: true
	HTTPTriggerPort    int    `yaml:"http_trigger_port" json:"http_trigger_port"`         // default: 49125 (0 to disable)
	AutoSyncOnExit     bool   `yaml:"auto_sync_on_exit" json:"auto_sync_on_exit"`         // default: true
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
	Enabled               bool     `yaml:"enabled" json:"enabled"`                                                     // default: true
	LocalPlayerID         string   `yaml:"local_player_id" json:"local_player_id"`                                     // optional override (e.g. "Epic|<id>|0" or "Steam|<id>|0")
	LocalPlayerName       string   `yaml:"local_player_name" json:"local_player_name"`                                 // optional override for player display name
	AutoFetchRanks        bool     `yaml:"auto_fetch_ranks" json:"auto_fetch_ranks"`                                   // default: true
	SessionContinueWindow Duration `yaml:"session_continue_window,omitempty" json:"session_continue_window,omitempty"` // optional fallback alias for session.continue_window
}

// BallchasingConfig configures replay uploads to ballchasing.com.
type BallchasingConfig struct {
	APIKey     string   `yaml:"api_key" json:"api_key"`
	Visibility string   `yaml:"visibility" json:"visibility"`   // "public", "unlisted", "private"
	BaseURL    string   `yaml:"base_url" json:"base_url"`       // default: "https://ballchasing.com/api"
	Timeout    Duration `yaml:"timeout" json:"timeout"`         // default: 60s
	MaxRetries int      `yaml:"max_retries" json:"max_retries"` // default: 3
}

// SyncConfig configures synchronization polling and local persistence.
type SyncConfig struct {
	PollInterval    Duration `yaml:"poll_interval" json:"poll_interval"`                   // default: 5m
	ReplayDir       string   `yaml:"replay_dir" json:"replay_dir"`                         // default: "./replays"
	DBPath          string   `yaml:"db_path" json:"db_path"`                               // default: "./rl-sync.db"
	KeepLocalFiles  bool     `yaml:"keep_local_files" json:"keep_local_files"`             // default: true
	DownloadTimeout Duration `yaml:"download_timeout" json:"download_timeout"`             // default: 30s
	ReplayLimit     int      `yaml:"replay_limit,omitempty" json:"replay_limit,omitempty"` // default: 200 if 0
	DryRun          bool     `yaml:"dry_run" json:"dry_run"`                               // default: false
	Once            bool     `yaml:"once" json:"once"`                                     // default: false
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
	SessionContinueWindow *time.Duration
	WebEnabled            *bool
	WebHost               *string
	WebPort               *int
}
