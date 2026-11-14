package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// TestBug_YAMLRawNanosecondsDecoding verifies unquoted numeric nanosecond literals in YAML decode properly.
func TestBug_YAMLRawNanosecondsDecoding(t *testing.T) {
	type durationWrapper struct {
		D config.Duration `yaml:"duration"`
	}

	rawYAML := []byte("duration: 300000000000\n")
	var w durationWrapper
	err := yaml.Unmarshal(rawYAML, &w)
	if err != nil {
		t.Errorf("Duration.UnmarshalYAML failed to parse raw numeric nanoseconds: %v", err)
	} else if w.D.Duration() != 5*time.Minute {
		t.Errorf("expected 5m, got %v", w.D.Duration())
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
