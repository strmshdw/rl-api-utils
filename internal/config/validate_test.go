package config_test

import (
	"strings"
	"testing"

	"github.com/dank/rl-api-utils/internal/config"
)

func TestConfig_ValidationFailures(t *testing.T) {
	tests := []struct {
		name          string
		modify        func(c *config.Config)
		expectedError string
	}{
		{
			name: "invalid provider",
			modify: func(c *config.Config) {
				c.Auth.Provider = "playstation"
			},
			expectedError: "invalid provider \"playstation\"",
		},
		{
			name: "steam missing ticket",
			modify: func(c *config.Config) {
				c.Auth.Provider = "steam"
				c.Auth.Steam.SessionTicket = ""
				c.Auth.Steam.SteamID64 = "76561198000000000"
			},
			expectedError: "steam provider requires 'session_ticket'",
		},
		{
			name: "steam missing steam_id_64",
			modify: func(c *config.Config) {
				c.Auth.Provider = "steam"
				c.Auth.Steam.SessionTicket = "ticket"
				c.Auth.Steam.SteamID64 = ""
			},
			expectedError: "steam provider requires 'steam_id_64'",
		},
		{
			name: "missing ballchasing api key",
			modify: func(c *config.Config) {
				c.Ballchasing.APIKey = ""
			},
			expectedError: "ballchasing: 'api_key' is required",
		},
		{
			name: "invalid visibility",
			modify: func(c *config.Config) {
				c.Ballchasing.Visibility = "secret"
			},
			expectedError: "invalid visibility \"secret\"",
		},
		{
			name: "invalid base url",
			modify: func(c *config.Config) {
				c.Ballchasing.BaseURL = "ftp://invalid-url"
			},
			expectedError: "invalid base_url",
		},
		{
			name: "non-positive timeout",
			modify: func(c *config.Config) {
				c.Ballchasing.Timeout = config.Duration(0)
			},
			expectedError: "'timeout' must be positive",
		},
		{
			name: "negative max retries",
			modify: func(c *config.Config) {
				c.Ballchasing.MaxRetries = -1
			},
			expectedError: "'max_retries' cannot be negative",
		},
		{
			name: "non-positive poll interval",
			modify: func(c *config.Config) {
				c.Sync.PollInterval = config.Duration(0)
			},
			expectedError: "'poll_interval' must be positive",
		},
		{
			name: "empty replay dir",
			modify: func(c *config.Config) {
				c.Sync.ReplayDir = ""
			},
			expectedError: "'replay_dir' cannot be empty",
		},
		{
			name: "empty db path",
			modify: func(c *config.Config) {
				c.Sync.DBPath = ""
			},
			expectedError: "'db_path' cannot be empty",
		},
		{
			name: "non-positive download timeout",
			modify: func(c *config.Config) {
				c.Sync.DownloadTimeout = config.Duration(0)
			},
			expectedError: "'download_timeout' must be positive",
		},
		{
			name: "invalid logging level",
			modify: func(c *config.Config) {
				c.Logging.Level = "superverbose"
			},
			expectedError: "invalid level \"superverbose\"",
		},
		{
			name: "invalid logging format",
			modify: func(c *config.Config) {
				c.Logging.Format = "xml"
			},
			expectedError: "invalid format \"xml\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.NewDefaultConfig()
			cfg.Auth.Epic.RefreshToken = "valid-token"
			cfg.Ballchasing.APIKey = "valid-key"

			tt.modify(cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.expectedError)
			}
			if !strings.Contains(err.Error(), tt.expectedError) {
				t.Errorf("expected error containing %q, got %q", tt.expectedError, err.Error())
			}
		})
	}
}

func TestConfig_PollingAuth_Validation(t *testing.T) {
	tests := []struct {
		name          string
		modify        func(c *config.Config)
		expectErr     bool
		expectedError string
	}{
		{
			name: "disabled_with_no_credentials_passes",
			modify: func(c *config.Config) {
				c.PollingAuth.Enabled = false
				c.PollingAuth.Epic.RefreshToken = ""
			},
			expectErr: false,
		},
		{
			name: "disabled_with_invalid_provider_fails",
			modify: func(c *config.Config) {
				c.PollingAuth.Enabled = false
				c.PollingAuth.Provider = "nintendo"
			},
			expectErr:     true,
			expectedError: "polling_auth: invalid provider \"nintendo\"",
		},
		{
			name: "enabled_epic_missing_credentials",
			modify: func(c *config.Config) {
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "epic"
				c.PollingAuth.Epic.RefreshToken = ""
				c.PollingAuth.Epic.AuthCode = ""
			},
			expectErr: false,
		},
		{
			name: "enabled_epic_whitespace_credentials",
			modify: func(c *config.Config) {
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "epic"
				c.PollingAuth.Epic.RefreshToken = "   "
				c.PollingAuth.Epic.AuthCode = "   "
			},
			expectErr: false,
		},
		{
			name: "enabled_epic_valid_refresh_token",
			modify: func(c *config.Config) {
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "epic"
				c.PollingAuth.Epic.RefreshToken = "secondary-refresh-token"
			},
			expectErr: false,
		},
		{
			name: "enabled_epic_valid_auth_code",
			modify: func(c *config.Config) {
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "epic"
				c.PollingAuth.Epic.AuthCode = "secondary-auth-code"
			},
			expectErr: false,
		},
		{
			name: "enabled_steam_missing_ticket",
			modify: func(c *config.Config) {
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "steam"
				c.PollingAuth.Steam.SessionTicket = ""
				c.PollingAuth.Steam.SteamID64 = "76561198099999999"
			},
			expectErr:     true,
			expectedError: "polling_auth: steam provider requires 'session_ticket'",
		},
		{
			name: "enabled_steam_missing_steam_id_64",
			modify: func(c *config.Config) {
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "steam"
				c.PollingAuth.Steam.SessionTicket = "ticket"
				c.PollingAuth.Steam.SteamID64 = ""
			},
			expectErr:     true,
			expectedError: "polling_auth: steam provider requires 'steam_id_64'",
		},
		{
			name: "enabled_steam_valid",
			modify: func(c *config.Config) {
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "steam"
				c.PollingAuth.Steam.SessionTicket = "ticket"
				c.PollingAuth.Steam.SteamID64 = "76561198099999999"
			},
			expectErr: false,
		},
		{
			name: "enabled_invalid_provider",
			modify: func(c *config.Config) {
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "playstation"
			},
			expectErr:     true,
			expectedError: "polling_auth: invalid provider \"playstation\"",
		},
		{
			name: "enabled_provider_normalization_uppercase_with_whitespace",
			modify: func(c *config.Config) {
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "  EPIC  "
				c.PollingAuth.Epic.RefreshToken = "valid-token"
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.NewDefaultConfig()
			cfg.Auth.Epic.RefreshToken = "primary-valid-token"
			cfg.Ballchasing.APIKey = "valid-key"

			tt.modify(cfg)

			err := cfg.Validate()
			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.expectedError)
				}
				if !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("expected error containing %q, got %q", tt.expectedError, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("expected validation success, got error: %v", err)
				}
			}
		})
	}
}

func TestConfig_DuplicateCredentials_Error67Prevention(t *testing.T) {
	tests := []struct {
		name          string
		modify        func(c *config.Config)
		expectErr     bool
		expectedError string
	}{
		{
			name: "epic_matching_refresh_tokens_triggers_error67_protection",
			modify: func(c *config.Config) {
				c.Auth.Provider = "epic"
				c.Auth.Epic.RefreshToken = "duplicate-token"
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "epic"
				c.PollingAuth.Epic.RefreshToken = "duplicate-token"
			},
			expectErr:     true,
			expectedError: "polling_auth: credentials conflict with primary auth (same account causes PsyNet Error 67 kick)",
		},
		{
			name: "epic_matching_account_ids_triggers_error67_protection",
			modify: func(c *config.Config) {
				c.Auth.Provider = "epic"
				c.Auth.Epic.RefreshToken = "token-1"
				c.Auth.Epic.AccountID = "acct-12345"
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "epic"
				c.PollingAuth.Epic.RefreshToken = "token-2"
				c.PollingAuth.Epic.AccountID = "acct-12345"
			},
			expectErr:     true,
			expectedError: "polling_auth: credentials conflict with primary auth (same account causes PsyNet Error 67 kick)",
		},
		{
			name: "epic_matching_auth_codes_triggers_error67_protection",
			modify: func(c *config.Config) {
				c.Auth.Provider = "epic"
				c.Auth.Epic.AuthCode = "code-123"
				c.Auth.Epic.RefreshToken = ""
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "epic"
				c.PollingAuth.Epic.AuthCode = "code-123"
				c.PollingAuth.Epic.RefreshToken = ""
			},
			expectErr:     true,
			expectedError: "polling_auth: credentials conflict with primary auth (same account causes PsyNet Error 67 kick)",
		},
		{
			name: "steam_matching_session_tickets_triggers_error67_protection",
			modify: func(c *config.Config) {
				c.Auth.Provider = "steam"
				c.Auth.Steam.SessionTicket = "ticket-same"
				c.Auth.Steam.SteamID64 = "76561198000000001"
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "steam"
				c.PollingAuth.Steam.SessionTicket = "ticket-same"
				c.PollingAuth.Steam.SteamID64 = "76561198000000002"
			},
			expectErr:     true,
			expectedError: "polling_auth: credentials conflict with primary auth (same account causes PsyNet Error 67 kick)",
		},
		{
			name: "steam_matching_steam_id_64_triggers_error67_protection",
			modify: func(c *config.Config) {
				c.Auth.Provider = "steam"
				c.Auth.Steam.SessionTicket = "ticket-primary"
				c.Auth.Steam.SteamID64 = "76561198000000001"
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "steam"
				c.PollingAuth.Steam.SessionTicket = "ticket-secondary"
				c.PollingAuth.Steam.SteamID64 = "76561198000000001"
			},
			expectErr:     true,
			expectedError: "polling_auth: credentials conflict with primary auth (same account causes PsyNet Error 67 kick)",
		},
		{
			name: "different_providers_epic_and_steam_passes",
			modify: func(c *config.Config) {
				c.Auth.Provider = "epic"
				c.Auth.Epic.RefreshToken = "token-1"
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "steam"
				c.PollingAuth.Steam.SessionTicket = "ticket-1"
				c.PollingAuth.Steam.SteamID64 = "76561198099999999"
			},
			expectErr: false,
		},
		{
			name: "epic_different_tokens_passes",
			modify: func(c *config.Config) {
				c.Auth.Provider = "epic"
				c.Auth.Epic.RefreshToken = "primary-token"
				c.PollingAuth.Enabled = true
				c.PollingAuth.Provider = "epic"
				c.PollingAuth.Epic.RefreshToken = "secondary-token"
			},
			expectErr: false,
		},
		{
			name: "same_tokens_but_polling_disabled_passes",
			modify: func(c *config.Config) {
				c.Auth.Provider = "epic"
				c.Auth.Epic.RefreshToken = "same-token"
				c.PollingAuth.Enabled = false
				c.PollingAuth.Provider = "epic"
				c.PollingAuth.Epic.RefreshToken = "same-token"
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.NewDefaultConfig()
			cfg.Ballchasing.APIKey = "valid-key"

			tt.modify(cfg)

			err := cfg.Validate()
			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.expectedError)
				}
				if !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("expected error containing %q, got %q", tt.expectedError, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("expected validation success, got error: %v", err)
				}
			}
		})
	}
}
