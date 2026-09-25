package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"gopkg.in/yaml.v3"
)

// ============================================================================
// 1. Malformed Syntax & Corrupted Configuration Files
// ============================================================================

func TestBoundary_MalformedYAML(t *testing.T) {
	malformedCases := []struct {
		name    string
		content string
	}{
		{
			name:    "unbalanced_curly_bracket",
			content: "auth:\n  provider: {epic\n",
		},
		{
			name:    "invalid_tab_indentation",
			content: "auth:\n\tprovider: epic\n",
		},
		{
			name:    "colon_without_space",
			content: "auth:provider:epic\n",
		},
		{
			name:    "unterminated_quoted_string",
			content: "auth:\n  provider: \"epic\n",
		},
		{
			name:    "random_binary_garbage",
			content: "\x00\x01\x02\xFF\xFE\xFD\x80\x90broken_binary_yaml",
		},
	}

	for _, tc := range malformedCases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			path := filepath.Join(tmpDir, "config.yaml")
			if err := os.WriteFile(path, []byte(tc.content), 0644); err != nil {
				t.Fatalf("failed to write test file: %v", err)
			}

			_, err := config.Load(config.CLIFlags{ConfigPath: path})
			if err == nil {
				t.Fatalf("expected error loading malformed YAML (%s), got nil", tc.name)
			}
		})
	}
}

func TestBoundary_MalformedJSON(t *testing.T) {
	malformedCases := []struct {
		name    string
		content string
	}{
		{
			name:    "truncated_json",
			content: `{"auth": {"provider": "epic"`,
		},
		{
			name:    "trailing_comma",
			content: `{"auth": {"provider": "epic",}}`,
		},
		{
			name:    "unquoted_keys",
			content: `{auth: {provider: "epic"}}`,
		},
		{
			name:    "single_quotes",
			content: `{'auth': {'provider': 'epic'}}`,
		},
		{
			name:    "empty_json_file",
			content: ``,
		},
		{
			name:    "json_root_is_primitive",
			content: `"just a string"`,
		},
		{
			name:    "json_root_is_array",
			content: `["item1", "item2"]`,
		},
		{
			name:    "corrupt_binary_with_json_ext",
			content: "\xDE\xAD\xBE\xEF\x00\x11\x22\x33\x44\x55",
		},
	}

	for _, tc := range malformedCases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			path := filepath.Join(tmpDir, "config.json")
			if err := os.WriteFile(path, []byte(tc.content), 0644); err != nil {
				t.Fatalf("failed to write test file: %v", err)
			}

			_, err := config.Load(config.CLIFlags{ConfigPath: path})
			if err == nil {
				t.Fatalf("expected error loading malformed JSON (%s), got nil", tc.name)
			}
		})
	}
}

// ============================================================================
// 2. Boundary Duration Strings & Raw Nanoseconds
// ============================================================================

func TestBoundary_DurationStrings(t *testing.T) {
	type durationWrapper struct {
		D config.Duration `json:"duration" yaml:"duration"`
	}

	testCases := []struct {
		name        string
		inputJSON   string
		inputYAML   string
		expectParse bool
		expectDur   time.Duration
	}{
		{
			name:        "zero_duration",
			inputJSON:   `{"duration": "0s"}`,
			inputYAML:   "duration: \"0s\"\n",
			expectParse: true,
			expectDur:   0,
		},
		{
			name:        "negative_duration",
			inputJSON:   `{"duration": "-5m"}`,
			inputYAML:   "duration: \"-5m\"\n",
			expectParse: true,
			expectDur:   -5 * time.Minute,
		},
		{
			name:        "extreme_large_duration_100000h",
			inputJSON:   `{"duration": "100000h"}`,
			inputYAML:   "duration: \"100000h\"\n",
			expectParse: true,
			expectDur:   100000 * time.Hour,
		},
		{
			name:        "raw_nanoseconds_string_with_unit",
			inputJSON:   `{"duration": "300000000000ns"}`,
			inputYAML:   "duration: \"300000000000ns\"\n",
			expectParse: true,
			expectDur:   5 * time.Minute,
		},
		{
			name:        "invalid_string_abc",
			inputJSON:   `{"duration": "abc"}`,
			inputYAML:   "duration: \"abc\"\n",
			expectParse: false,
		},
		{
			name:        "invalid_string_5months",
			inputJSON:   `{"duration": "5months"}`,
			inputYAML:   "duration: \"5months\"\n",
			expectParse: false,
		},
		{
			name:        "invalid_string_raw_number_no_units",
			inputJSON:   `{"duration": "100"}`,
			inputYAML:   "duration: \"100\"\n",
			expectParse: false,
		},
		{
			name:        "invalid_type_boolean",
			inputJSON:   `{"duration": true}`,
			inputYAML:   "duration: true\n",
			expectParse: false,
		},
		{
			name:        "invalid_type_array",
			inputJSON:   `{"duration": [1, 2]}`,
			inputYAML:   "duration:\n  - 1\n  - 2\n",
			expectParse: false,
		},
	}

	for _, tc := range testCases {
		t.Run("JSON_"+tc.name, func(t *testing.T) {
			var w durationWrapper
			err := json.Unmarshal([]byte(tc.inputJSON), &w)
			if tc.expectParse {
				if err != nil {
					t.Fatalf("expected successful JSON unmarshal, got: %v", err)
				}
				if w.D.Duration() != tc.expectDur {
					t.Fatalf("expected duration %v, got %v", tc.expectDur, w.D.Duration())
				}
			} else {
				if err == nil {
					t.Fatalf("expected JSON unmarshal error for %s, got nil", tc.inputJSON)
				}
			}
		})

		t.Run("YAML_"+tc.name, func(t *testing.T) {
			var w durationWrapper
			err := yaml.Unmarshal([]byte(tc.inputYAML), &w)
			if tc.expectParse {
				if err != nil {
					t.Fatalf("expected successful YAML unmarshal, got: %v", err)
				}
				if w.D.Duration() != tc.expectDur {
					t.Fatalf("expected duration %v, got %v", tc.expectDur, w.D.Duration())
				}
			} else {
				if err == nil {
					t.Fatalf("expected YAML unmarshal error for %s, got nil", tc.inputYAML)
				}
			}
		})
	}
}

func TestBoundary_JSON_NumericNanoseconds(t *testing.T) {
	type durationWrapper struct {
		D config.Duration `json:"duration"`
	}

	rawJSON := []byte(`{"duration": 300000000000}`)
	var w durationWrapper
	if err := json.Unmarshal(rawJSON, &w); err != nil {
		t.Fatalf("expected JSON numeric unmarshaling to succeed, got: %v", err)
	}
	if w.D.Duration() != 5*time.Minute {
		t.Fatalf("expected 5m, got %v", w.D.Duration())
	}
}

// TestBug_YAMLRawNanosecondsDecoding empirically reproduces the bug in Duration.UnmarshalYAML.
// When an unquoted numeric literal (raw nanoseconds, e.g. 300000000000) is present in YAML,
// Duration.UnmarshalYAML currently calls value.Decode(&s) which decodes the integer scalar into string "300000000000",
// causing time.ParseDuration to fail with "missing unit in duration" and immediately return an error.
// The intended fallback `var n int64; value.Decode(&n)` at line 64 of internal/config/config.go is dead code.
func TestBug_YAMLRawNanosecondsDecoding(t *testing.T) {
	type durationWrapper struct {
		D config.Duration `yaml:"duration"`
	}

	rawYAML := []byte("duration: 300000000000\n")
	var w durationWrapper
	err := yaml.Unmarshal(rawYAML, &w)
	if err != nil {
		t.Errorf("BUG CONFIRMED: Duration.UnmarshalYAML failed to parse raw numeric nanoseconds: %v", err)
	} else if w.D.Duration() != 5*time.Minute {
		t.Errorf("expected 5m, got %v", w.D.Duration())
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

// ============================================================================
// 3. Case Insensitivity of Enum Fields
// ============================================================================

func TestBoundary_CaseInsensitivity_ConfigLoading(t *testing.T) {
	testEnums := []struct {
		name       string
		provider   string
		visibility string
		level      string
		format     string
		expectProv string
		expectVis  string
		expectLev  string
		expectFmt  string
	}{
		{
			name:       "all_caps",
			provider:   "EPIC",
			visibility: "PUBLIC",
			level:      "DEBUG",
			format:     "TEXT",
			expectProv: "epic",
			expectVis:  "public",
			expectLev:  "debug",
			expectFmt:  "text",
		},
		{
			name:       "mixed_case_1",
			provider:   "sTeAm",
			visibility: "UnLiStEd",
			level:      "InFo",
			format:     "JsOn",
			expectProv: "steam",
			expectVis:  "unlisted",
			expectLev:  "info",
			expectFmt:  "json",
		},
		{
			name:       "mixed_case_2",
			provider:   "Epic",
			visibility: "pRiVaTe",
			level:      "WaRn",
			format:     "JSON",
			expectProv: "epic",
			expectVis:  "private",
			expectLev:  "warn",
			expectFmt:  "json",
		},
		{
			name:       "with_whitespace",
			provider:   "  steam  ",
			visibility: "  public  ",
			level:      "  ERROR  ",
			format:     "  text  ",
			expectProv: "steam",
			expectVis:  "public",
			expectLev:  "error",
			expectFmt:  "text",
		},
	}

	for _, tc := range testEnums {
		t.Run("YAML_"+tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			path := filepath.Join(tmpDir, "config.yaml")
			content := "auth:\n  provider: \"" + tc.provider + "\"\n" +
				"  epic:\n    refresh_token: \"test-tok\"\n" +
				"  steam:\n    session_ticket: \"test-tkt\"\n    steam_id_64: \"1234567890\"\n" +
				"ballchasing:\n  api_key: \"test-key\"\n  visibility: \"" + tc.visibility + "\"\n" +
				"logging:\n  level: \"" + tc.level + "\"\n  format: \"" + tc.format + "\"\n"

			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				t.Fatalf("failed to write test file: %v", err)
			}

			cfg, err := config.Load(config.CLIFlags{ConfigPath: path})
			if err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}

			if cfg.Auth.Provider != tc.expectProv {
				t.Errorf("provider: expected %q, got %q", tc.expectProv, cfg.Auth.Provider)
			}
			if cfg.Ballchasing.Visibility != tc.expectVis {
				t.Errorf("visibility: expected %q, got %q", tc.expectVis, cfg.Ballchasing.Visibility)
			}
			if cfg.Logging.Level != tc.expectLev {
				t.Errorf("logging level: expected %q, got %q", tc.expectLev, cfg.Logging.Level)
			}
			if cfg.Logging.Format != tc.expectFmt {
				t.Errorf("logging format: expected %q, got %q", tc.expectFmt, cfg.Logging.Format)
			}
		})

		t.Run("JSON_"+tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			path := filepath.Join(tmpDir, "config.json")
			jsonPayload := map[string]interface{}{
				"auth": map[string]interface{}{
					"provider": tc.provider,
					"epic": map[string]string{
						"refresh_token": "test-tok",
					},
					"steam": map[string]string{
						"session_ticket": "test-tkt",
						"steam_id_64":    "1234567890",
					},
				},
				"ballchasing": map[string]interface{}{
					"api_key":    "test-key",
					"visibility": tc.visibility,
				},
				"logging": map[string]interface{}{
					"level":  tc.level,
					"format": tc.format,
				},
			}
			bytes, err := json.Marshal(jsonPayload)
			if err != nil {
				t.Fatalf("marshal error: %v", err)
			}
			if err := os.WriteFile(path, bytes, 0644); err != nil {
				t.Fatalf("write file error: %v", err)
			}

			cfg, err := config.Load(config.CLIFlags{ConfigPath: path})
			if err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}

			if cfg.Auth.Provider != tc.expectProv {
				t.Errorf("provider: expected %q, got %q", tc.expectProv, cfg.Auth.Provider)
			}
			if cfg.Ballchasing.Visibility != tc.expectVis {
				t.Errorf("visibility: expected %q, got %q", tc.expectVis, cfg.Ballchasing.Visibility)
			}
			if cfg.Logging.Level != tc.expectLev {
				t.Errorf("logging level: expected %q, got %q", tc.expectLev, cfg.Logging.Level)
			}
			if cfg.Logging.Format != tc.expectFmt {
				t.Errorf("logging format: expected %q, got %q", tc.expectFmt, cfg.Logging.Format)
			}
		})
	}
}

// ============================================================================
// 4. Environment Variable Overrides & Edge Cases
// ============================================================================

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
		{
			name:   "invalid_boolean_string",
			envKey: "RL_SYNC_DRY_RUN",
			envVal: "yes_please",
		},
		{
			name:   "invalid_max_retries_string",
			envKey: "RL_SYNC_BALLCHASING_MAX_RETRIES",
			envVal: "three",
		},
		{
			name:   "invalid_max_retries_float",
			envKey: "RL_SYNC_BALLCHASING_MAX_RETRIES",
			envVal: "3.5",
		},
		{
			name:   "invalid_poll_interval_syntax",
			envKey: "RL_SYNC_POLL_INTERVAL",
			envVal: "10minutes",
		},
		{
			name:   "invalid_ballchasing_timeout_syntax",
			envKey: "RL_SYNC_BALLCHASING_TIMEOUT",
			envVal: "invalid_time",
		},
		{
			name:   "invalid_download_timeout_syntax",
			envKey: "RL_SYNC_DOWNLOAD_TIMEOUT",
			envVal: "bad_val",
		},
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

func TestBoundary_WhitespaceOnlyFields(t *testing.T) {
	// Fields containing only whitespace should be detected as empty and fail validation
	cases := []struct {
		name   string
		modify func(c *config.Config)
		errMsg string
	}{
		{
			name: "whitespace_epic_credentials",
			modify: func(c *config.Config) {
				c.Auth.Provider = "epic"
				c.Auth.Epic.RefreshToken = "   "
				c.Auth.Epic.AuthCode = "   "
			},
			errMsg: "epic provider requires either 'refresh_token' or 'auth_code'",
		},
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

// ============================================================================
// 5. Multi-Error Aggregation with errors.Join
// ============================================================================

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
