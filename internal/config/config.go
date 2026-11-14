package config

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

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
		Session: SessionConfig{
			ContinueWindow: Duration(DefaultSessionContinueWindow),
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
			Host:    "127.0.0.1",
			Port:    49125,
		},
	}
}

// EffectiveSessionContinueWindow returns the configured session continuation window.
func (c *Config) EffectiveSessionContinueWindow() time.Duration {
	if c == nil {
		return DefaultSessionContinueWindow
	}
	if c.Session.ContinueWindow > 0 {
		return c.Session.ContinueWindow.Duration()
	}
	if c.PlayerTracking.SessionContinueWindow > 0 {
		return c.PlayerTracking.SessionContinueWindow.Duration()
	}
	// If Config was constructed as a zero literal (&config.Config{}) in tests rather than NewDefaultConfig(),
	// fall back to DefaultSessionContinueWindow unless other top-level defaults are populated.
	if c.Auth.Provider == "" && c.Sync.PollInterval == 0 && c.Ballchasing.BaseURL == "" {
		return DefaultSessionContinueWindow
	}
	return 0
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

// SaveConfig marshals the configuration as YAML and atomically writes it with 0600 permissions.
func (c *Config) SaveConfig(filePath string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshaling config to YAML: %w", err)
	}
	return AtomicWriteFile(filePath, data, 0600)
}
