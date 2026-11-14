package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
)

func TestConfig_Web_Validation(t *testing.T) {
	tests := []struct {
		name        string
		enabled     bool
		host        string
		port        int
		expectValid bool
		errSubstr   string
	}{
		{
			name:        "valid default config",
			enabled:     true,
			host:        "127.0.0.1",
			port:        49125,
			expectValid: true,
		},
		{
			name:        "empty host when enabled",
			enabled:     true,
			host:        "",
			port:        49125,
			expectValid: false,
			errSubstr:   "web: 'host' cannot be empty",
		},
		{
			name:        "whitespace host when enabled",
			enabled:     true,
			host:        "   ",
			port:        49125,
			expectValid: false,
			errSubstr:   "web: 'host' cannot be empty",
		},
		{
			name:        "port zero when enabled",
			enabled:     true,
			host:        "127.0.0.1",
			port:        0,
			expectValid: false,
			errSubstr:   "web: 'port' must be between 1 and 65535",
		},
		{
			name:        "negative port when enabled",
			enabled:     true,
			host:        "127.0.0.1",
			port:        -1,
			expectValid: false,
			errSubstr:   "web: 'port' must be between 1 and 65535",
		},
		{
			name:        "port too high when enabled",
			enabled:     true,
			host:        "127.0.0.1",
			port:        65536,
			expectValid: false,
			errSubstr:   "web: 'port' must be between 1 and 65535",
		},
		{
			name:        "port 1 boundary when enabled",
			enabled:     true,
			host:        "127.0.0.1",
			port:        1,
			expectValid: true,
		},
		{
			name:        "port 65535 boundary when enabled",
			enabled:     true,
			host:        "127.0.0.1",
			port:        65535,
			expectValid: true,
		},
		{
			name:        "disabled with empty host and port zero is valid",
			enabled:     false,
			host:        "",
			port:        0,
			expectValid: true,
		},
		{
			name:        "disabled with negative port is invalid",
			enabled:     false,
			host:        "",
			port:        -1,
			expectValid: false,
			errSubstr:   "web: 'port' must be between 0 and 65535",
		},
		{
			name:        "disabled with port 70000 is invalid",
			enabled:     false,
			host:        "",
			port:        70000,
			expectValid: false,
			errSubstr:   "web: 'port' must be between 0 and 65535",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.NewDefaultConfig()
			cfg.Auth.Epic.RefreshToken = "valid-token"
			cfg.Ballchasing.APIKey = "valid-key"
			cfg.Web.Enabled = tc.enabled
			cfg.Web.Host = tc.host
			cfg.Web.Port = tc.port

			err := cfg.Validate()
			if tc.expectValid {
				if err != nil {
					t.Fatalf("expected valid config, got error: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tc.errSubstr) {
					t.Errorf("expected error containing %q, got: %v", tc.errSubstr, err)
				}
			}
		})
	}
}

func TestBoundary_ZeroAndNegativeDurationsRejectedInValidate(t *testing.T) {
	cases := []struct {
		name   string
		modify func(c *config.Config)
		errMsg string
	}{
		{
			name: "zero_ballchasing_timeout",
			modify: func(c *config.Config) {
				c.Ballchasing.Timeout = config.Duration(0)
			},
			errMsg: "'timeout' must be positive",
		},
		{
			name: "negative_ballchasing_timeout",
			modify: func(c *config.Config) {
				c.Ballchasing.Timeout = config.Duration(-5 * time.Minute)
			},
			errMsg: "'timeout' must be positive",
		},
		{
			name: "zero_poll_interval",
			modify: func(c *config.Config) {
				c.Sync.PollInterval = config.Duration(0)
			},
			errMsg: "'poll_interval' must be positive",
		},
		{
			name: "negative_poll_interval",
			modify: func(c *config.Config) {
				c.Sync.PollInterval = config.Duration(-1 * time.Second)
			},
			errMsg: "'poll_interval' must be positive",
		},
		{
			name: "zero_download_timeout",
			modify: func(c *config.Config) {
				c.Sync.DownloadTimeout = config.Duration(0)
			},
			errMsg: "'download_timeout' must be positive",
		},
		{
			name: "negative_download_timeout",
			modify: func(c *config.Config) {
				c.Sync.DownloadTimeout = config.Duration(-30 * time.Second)
			},
			errMsg: "'download_timeout' must be positive",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.NewDefaultConfig()
			cfg.Auth.Epic.RefreshToken = "valid"
			cfg.Ballchasing.APIKey = "valid"
			tc.modify(cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatalf("expected validation error containing %q, got nil", tc.errMsg)
			}
			if !strings.Contains(err.Error(), tc.errMsg) {
				t.Fatalf("expected error containing %q, got %q", tc.errMsg, err.Error())
			}
		})
	}
}

func TestBoundary_WhitespaceOnlyFields(t *testing.T) {
	// Fields containing only whitespace should be detected as empty and fail validation
	cases := []struct {
		name   string
		modify func(c *config.Config)
		errMsg string
	}{
		{
			name: "whitespace_steam_ticket",
			modify: func(c *config.Config) {
				c.Auth.Provider = "steam"
				c.Auth.Steam.SessionTicket = "   "
				c.Auth.Steam.SteamID64 = "76561198000000000"
			},
			errMsg: "steam provider requires 'session_ticket'",
		},
		{
			name: "whitespace_steam_id_64",
			modify: func(c *config.Config) {
				c.Auth.Provider = "steam"
				c.Auth.Steam.SessionTicket = "ticket"
				c.Auth.Steam.SteamID64 = "   "
			},
			errMsg: "steam provider requires 'steam_id_64'",
		},
		{
			name: "whitespace_ballchasing_api_key",
			modify: func(c *config.Config) {
				c.Ballchasing.APIKey = "   "
			},
			errMsg: "ballchasing: 'api_key' is required",
		},
		{
			name: "whitespace_replay_dir",
			modify: func(c *config.Config) {
				c.Sync.ReplayDir = "   "
			},
			errMsg: "sync: 'replay_dir' cannot be empty",
		},
		{
			name: "whitespace_db_path",
			modify: func(c *config.Config) {
				c.Sync.DBPath = "   "
			},
			errMsg: "sync: 'db_path' cannot be empty",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.NewDefaultConfig()
			cfg.Auth.Epic.RefreshToken = "valid"
			cfg.Ballchasing.APIKey = "valid"
			tc.modify(cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatalf("expected validation error for whitespace field, got nil")
			}
			if !strings.Contains(err.Error(), tc.errMsg) {
				t.Fatalf("expected error containing %q, got %q", tc.errMsg, err.Error())
			}
		})
	}
}

func TestBoundary_MultiErrorAggregation_5InvalidFields(t *testing.T) {
	cfg := config.NewDefaultConfig()

	// Intentionally invalidate exactly 5 distinct fields across different sections:
	// 1. Auth: invalid provider
	cfg.Auth.Provider = "playstation"
	// 2. Ballchasing: empty api_key
	cfg.Ballchasing.APIKey = ""
	// 3. Ballchasing: invalid visibility
	cfg.Ballchasing.Visibility = "secret"
	// 4. Sync: empty replay_dir
	cfg.Sync.ReplayDir = ""
	// 5. Logging: invalid format
	cfg.Logging.Format = "xml"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected multi-error validation failure, got nil")
	}

	// Verify unwrap returns slice via Go's errors.Join contract: interface{ Unwrap() []error }
	type multiErrUnwrapper interface {
		Unwrap() []error
	}

	unwrapper, ok := err.(multiErrUnwrapper)
	if !ok {
		t.Fatalf("expected error returned by errors.Join to implement Unwrap() []error, got type %T", err)
	}

	joinedErrors := unwrapper.Unwrap()
	if len(joinedErrors) != 5 {
		t.Fatalf("expected exactly 5 aggregated errors, got %d: %v", len(joinedErrors), joinedErrors)
	}

	expectedSnippets := []string{
		"invalid provider \"playstation\"",
		"'api_key' is required",
		"invalid visibility \"secret\"",
		"'replay_dir' cannot be empty",
		"invalid format \"xml\"",
	}

	for _, snippet := range expectedSnippets {
		found := false
		for _, e := range joinedErrors {
			if strings.Contains(e.Error(), snippet) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected error containing snippet %q in joined errors, but was not found. All errors: %v", snippet, joinedErrors)
		}
	}
}

func TestBoundary_MultiErrorAggregation_AllFieldsInvalid(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{
			Provider: "nintendo",
		},
		Ballchasing: config.BallchasingConfig{
			APIKey:     "",
			Visibility: "unknown",
			BaseURL:    "ftp://invalid",
			Timeout:    config.Duration(-1 * time.Second),
			MaxRetries: -5,
		},
		Sync: config.SyncConfig{
			PollInterval:    config.Duration(0),
			ReplayDir:       "",
			DBPath:          "",
			DownloadTimeout: config.Duration(-10 * time.Second),
		},
		Logging: config.LoggingConfig{
			Level:  "insane",
			Format: "yaml_format",
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}

	type multiErrUnwrapper interface {
		Unwrap() []error
	}

	unwrapper, ok := err.(multiErrUnwrapper)
	if !ok {
		t.Fatalf("expected error returned by errors.Join to implement Unwrap() []error, got type %T", err)
	}

	joined := unwrapper.Unwrap()
	if len(joined) < 11 {
		t.Fatalf("expected at least 11 errors joined, got %d: %v", len(joined), joined)
	}
}
