# Technical Specification & Design Report: M1 Configuration System & Go Module Definitions

**Target**: `internal/config`, `configs/`, and `go.mod`  
**Milestone**: M1 - Storage & Configuration  
**Author**: `m1_explorer_3`  
**Date**: 2026-09-25T03:08:00Z  
**Module**: `github.com/dank/rl-api-utils`  
**Status**: Ready for Implementation  

---

## 1. Observation

Direct inspection of project documentation, existing architecture reports, and API mining outputs reveals the following concrete technical requirements and constraints:

### 1.1 Requirements & Context (`ORIGINAL_REQUEST.md`)
- **R1 Match Polling & Replay Synchronization**: Configurable local storage directory and 5-minute default polling interval (`ORIGINAL_REQUEST.md:16-17`).
- **R2 Ballchasing.com Replay Uploader**: Configurable visibility (`public`, `unlisted`, `private`) and raw token authentication (`Authorization: <token>`) (`ORIGINAL_REQUEST.md:19-21`).
- **R3 Persistent State & Idempotency**: Configurable SQLite database path or persistent state file (`ORIGINAL_REQUEST.md:22-24`).
- **R4 Dual Authentication & Configuration**: Configurable via environment variables or configuration file for authentication provider choice (`epic` or `steam`), credentials/tokens (Epic refresh token / auth code, Steam session ticket + 64-bit Steam ID), Ballchasing API key, upload visibility, polling interval, and local replay directory path. Structured logging with timestamps and descriptive error reporting (`ORIGINAL_REQUEST.md:25-30`).
- **R5 Verification Suite & Single-Run CLI**: Single-run / dry-run CLI flags (`--once`, `--dry-run`) to allow executing one sync cycle or simulation on demand (`ORIGINAL_REQUEST.md:61`).

### 1.2 Upstream SDK Requirements (`github.com/dank/rlapi`)
From inspection of `github.com/dank/rlapi` (documented in `survey_miner_rlapi_1/handoff.md:14-142`):
- **Go Version**: `github.com/dank/rlapi` specifies `go 1.24.5` (`go.mod:3`).
- **Dependencies**: `github.com/gorilla/websocket v1.5.3` (`go.mod:5`).
- **Epic Authentication**:
  - Requires `RefreshToken` or `AuthCode` exchanged via `egs.AuthenticateWithRefreshToken` or `egs.AuthenticateWithCode` (`survey_miner_rlapi_1/handoff.md:96-97`).
- **Steam Authentication**:
  - Requires `SessionTicket` (passed to `egs.ExchangeEOSTokenFromSteam`) AND `SteamID64` (passed to `psyNet.AuthPlayerSteam(authToken, epicAccountID, steamAccountID, accountName)`) (`survey_miner_rlapi_1/handoff.md:100, 141`).
  - Without `SteamID64`, `AuthPlayerSteam` cannot format the player's composite `PlayerID` (`PlatformSteam|steamAccountID|0`).

### 1.3 Ballchasing Requirements (`ballchasing.com/api`)
From inspection of Ballchasing API specs (documented in `survey_miner_ballchasing_1/handoff.md:14-58`):
- **Base URL**: `https://ballchasing.com/api` (must be configurable so integration tests can target `httptest.Server`).
- **API Key**: Raw token string passed directly in `Authorization: <token>` (without `Bearer ` prefix).
- **Visibility Parameter**: Strictly accepts `"public"`, `"unlisted"`, or `"private"`.
- **Timeouts & Retries**: Recommended HTTP timeout default 60s, retry budget default 3 retries.

### 1.4 Architecture & Persistence (`survey_explorer_arch_1/handoff.md`)
- **Pure Go SQLite Driver**: `modernc.org/sqlite` (zero CGO dependencies, enabling clean builds on Windows and Linux without gcc).
- **Configuration Hierarchy**: CLI Flags > Environment Variables (`RL_SYNC_*`) > Config File (`config.yaml` / `config.json`) > Hardcoded Defaults (`survey_explorer_arch_1/handoff.md:143-220`).
- **Structured Logging**: Built-in Go `log/slog` supporting `"text"` and `"json"` formats at levels `"debug"`, `"info"`, `"warn"`, `"error"`.

### 1.5 Critical Go Unmarshaling Limitation
In Go's standard library `encoding/json`, `time.Duration` fields cannot be unmarshaled from human-readable strings (e.g. `"5m"`, `"30s"`) without custom unmarshaling logic (`json: cannot unmarshal string into Go struct field ... of type time.Duration`). While `gopkg.in/yaml.v3` supports string duration parsing natively, parity between YAML and JSON requires a custom `Duration` type with `UnmarshalJSON` and `UnmarshalYAML` implementations.

---

## 2. Logic Chain

From these direct observations, we trace the step-by-step reasoning that establishes the concrete module and configuration architecture:

```
[R1, R2, R3, R4, R5] 
         │
         ▼
[Zero CGO & Go 1.24+ Toolchain] ───> go.mod definition with modernc.org/sqlite & gopkg.in/yaml.v3
         │
         ▼
[Dual Auth (Epic/Steam) + Ballchasing] ───> Strongly-typed Config structs with json/yaml tags
         │
         ▼
[Human-Friendly Durations in JSON/YAML] ───> Custom Duration type supporting "5m", "30s" & ns
         │
         ▼
[Configuration Hierarchy] ───> Defaults -> File (YAML/JSON) -> Env (RL_SYNC_*) -> CLI Flags
         │
         ▼
[Validation Engine] ───> Fail-fast semantic checks with multi-error aggregation (errors.Join)
         │
         ▼
[Operator Ergonomics] ───> Rich config.example.yaml and config.example.json templates
         │
         ▼
[Test Suite Strategy] ───> Unit tests in config_test.go covering full hierarchy, table tests & errors
```

### 2.1 Go Module Definition (`go.mod`)
1. **Module Name**: `github.com/dank/rl-api-utils` aligns with the project repository and imports `github.com/dank/rlapi`.
2. **Go Version**: `go 1.24.0` (matching upstream `github.com/dank/rlapi` and supporting `errors.Join`, `log/slog`, `sync/atomic`).
3. **Core Dependencies**:
   - `github.com/dank/rlapi`: Core Rocket League PsyNet RPC SDK.
   - `modernc.org/sqlite`: Pure Go SQLite driver, zero CGO.
   - `github.com/gorilla/websocket v1.5.3`: WebSocket framing for PsyNet RPC.
   - `gopkg.in/yaml.v3 v3.0.1`: De facto standard YAML v3 unmarshaler and emitter.

### 2.2 Typed Configuration Structures (`internal/config/config.go`)
The configuration is grouped into logical, domain-driven sub-structs:
- `Config`: Root container.
- `AuthConfig`: Contains `Provider` (`"epic"` | `"steam"`), `EpicConfig`, and `SteamConfig`.
- `EpicConfig`: `RefreshToken`, `AuthCode`, `AccountID`, `DisplayName`.
- `SteamConfig`: `SessionTicket`, `SteamID64`, `AccountName`.
- `BallchasingConfig`: `APIKey`, `Visibility`, `BaseURL`, `Timeout`, `MaxRetries`.
- `SyncConfig`: `PollInterval`, `ReplayDir`, `DBPath`, `KeepLocalFiles`, `DownloadTimeout`, `DryRun`, `Once`.
- `LoggingConfig`: `Level`, `Format`.

### 2.3 Precedence & Layering Mechanics
The configuration loading sequence executes four sequential phases:
1. **Phase 1: Defaults Initialization**
   - Populate `Config` with immutable baseline defaults:
     - Provider: `"epic"`
     - Ballchasing BaseURL: `"https://ballchasing.com/api"`, Visibility: `"public"`, Timeout: `60s`, MaxRetries: `3`
     - Sync PollInterval: `5m`, ReplayDir: `"./replays"`, DBPath: `"./rl-sync.db"`, KeepLocalFiles: `true`, DownloadTimeout: `30s`, DryRun: `false`, Once: `false`
     - Logging Level: `"info"`, Format: `"text"`
2. **Phase 2: Configuration File (YAML or JSON)**
   - If `--config <path>` is explicitly provided: file must exist; return `os.ErrNotExist` if not found.
   - If no flag is given: probe default paths `./config.yaml`, `./config.yml`, `./config.json`. If none exist, proceed without error.
   - Detect format by file extension or payload sniffing; unmarshal on top of current struct state.
3. **Phase 3: Environment Variable Overrides (`RL_SYNC_*`)**
   - Iterate over mapped `RL_SYNC_*` environment variables. If present, parse and overwrite the corresponding struct field.
   - Handle string, boolean (`strconv.ParseBool`), integer (`strconv.Atoi`), and duration (`time.ParseDuration`) parsing.
4. **Phase 4: CLI Flag Overrides**
   - CLI flags passed via `CLIFlags` struct (e.g. `--once`, `--dry-run`, `--log-level`, `--poll-interval`, `--provider`, etc.).
   - Explicitly specified flags override all preceding layers.
5. **Phase 5: Validation**
   - Run semantic checks. If any fail, accumulate into `[]error` and return aggregated error via `errors.Join`.

### 2.4 Concrete Validation Invariants
The validation routine enforces the following invariants:
1. `Auth.Provider`: Must be `"epic"` or `"steam"` (case-insensitive).
2. If `Auth.Provider == "epic"`:
   - Must have `Auth.Epic.RefreshToken != ""` OR `Auth.Epic.AuthCode != ""`.
3. If `Auth.Provider == "steam"`:
   - Must have `Auth.Steam.SessionTicket != ""` AND `Auth.Steam.SteamID64 != ""`.
4. `Ballchasing.APIKey`: Must not be empty.
5. `Ballchasing.Visibility`: Must be `"public"`, `"unlisted"`, or `"private"`.
6. `Ballchasing.BaseURL`: Must be a valid absolute HTTP/HTTPS URL.
7. `Ballchasing.Timeout`: Must be > 0.
8. `Ballchasing.MaxRetries`: Must be >= 0.
9. `Sync.PollInterval`: Must be > 0.
10. `Sync.ReplayDir`: Must not be empty.
11. `Sync.DBPath`: Must not be empty.
12. `Sync.DownloadTimeout`: Must be > 0.
13. `Logging.Level`: Must be one of `"debug"`, `"info"`, `"warn"`, `"error"`.
14. `Logging.Format`: Must be one of `"text"`, `"json"`.

---

## 3. Recommended Code Architecture

### 3.1 `go.mod` Specification
```gomod
module github.com/dank/rl-api-utils

go 1.24.0

require (
	github.com/dank/rlapi v0.0.0-20241001000000-000000000000
	github.com/gorilla/websocket v1.5.3
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.36.0
)
```

### 3.2 `internal/config/config.go`
```go
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
	var s string
	if err := value.Decode(&s); err == nil {
		parsed, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("invalid duration string %q: %w", s, err)
		}
		*d = Duration(parsed)
		return nil
	}
	var n int64
	if err := value.Decode(&n); err == nil {
		*d = Duration(time.Duration(n))
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
```

---

## 4. Configuration File Templates

### 4.1 `configs/config.example.yaml`
```yaml
# ==============================================================================
# Rocket League Replay Synchronizer Daemon Configuration Template
# ==============================================================================

# Authentication Settings
# Supported providers: "epic" or "steam"
auth:
  provider: "epic" # Override with RL_SYNC_AUTH_PROVIDER

  # Epic Games Authentication
  # Required if provider is "epic". Provide either refresh_token OR auth_code.
  epic:
    # Long-term OAuth refresh token (recommended for daemon mode)
    refresh_token: "your-epic-refresh-token" # Override with RL_SYNC_EPIC_REFRESH_TOKEN
    
    # One-time login exchange code (used for initial bootstrap)
    auth_code: ""                           # Override with RL_SYNC_EPIC_AUTH_CODE
    
    # Optional account identifiers
    account_id: ""                          # Override with RL_SYNC_EPIC_ACCOUNT_ID
    display_name: ""                        # Override with RL_SYNC_EPIC_DISPLAY_NAME

  # Steam Authentication
  # Required if provider is "steam". Both session_ticket and steam_id_64 are required.
  steam:
    # Hex-encoded Steam session ticket obtained from Steamworks API
    session_ticket: ""                      # Override with RL_SYNC_STEAM_SESSION_TICKET
    
    # 64-bit Steam ID (e.g. 76561198000000000)
    steam_id_64: ""                         # Override with RL_SYNC_STEAM_ID_64
    
    # Optional Steam account username
    account_name: ""                        # Override with RL_SYNC_STEAM_ACCOUNT_NAME

# Ballchasing.com Upload Settings
ballchasing:
  # User upload API key from https://ballchasing.com/upload
  api_key: "your-ballchasing-api-key"      # Override with RL_SYNC_BALLCHASING_API_KEY
  
  # Upload visibility: "public", "unlisted", or "private"
  visibility: "public"                     # Override with RL_SYNC_BALLCHASING_VISIBILITY
  
  # Base API URL (defaults to production; change for mock testing)
  base_url: "https://ballchasing.com/api"   # Override with RL_SYNC_BALLCHASING_BASE_URL
  
  # HTTP request timeout for multipart uploads
  timeout: "60s"                           # Override with RL_SYNC_BALLCHASING_TIMEOUT
  
  # Maximum retry attempts on HTTP 429 rate limit or network error
  max_retries: 3                           # Override with RL_SYNC_BALLCHASING_MAX_RETRIES

# Synchronization Engine Settings
sync:
  # Polling interval between PsyNet match history checks
  poll_interval: "5m"                      # Override with RL_SYNC_POLL_INTERVAL
  
  # Local filesystem directory where downloaded .replay files are stored
  replay_dir: "./replays"                  # Override with RL_SYNC_REPLAY_DIR
  
  # Path to SQLite persistence database
  db_path: "./rl-sync.db"                  # Override with RL_SYNC_DB_PATH
  
  # Whether to retain .replay files on disk after successful upload
  keep_local_files: true                   # Override with RL_SYNC_KEEP_LOCAL_FILES
  
  # HTTP download timeout for streaming replay binary payload
  download_timeout: "30s"                  # Override with RL_SYNC_DOWNLOAD_TIMEOUT
  
  # Dry-run mode: discover and diff matches, but do not download or upload
  dry_run: false                           # Override with RL_SYNC_DRY_RUN or --dry-run
  
  # Once mode: run exactly one polling pass and then terminate
  once: false                              # Override with RL_SYNC_ONCE or --once

# Structured Logging Settings
logging:
  # Minimum log level: "debug", "info", "warn", or "error"
  level: "info"                            # Override with RL_SYNC_LOG_LEVEL or --log-level
  
  # Log format: "text" (human-friendly console) or "json" (structured production)
  format: "text"                           # Override with RL_SYNC_LOG_FORMAT or --log-format
```

### 4.2 `configs/config.example.json`
```json
{
  "auth": {
    "provider": "epic",
    "epic": {
      "refresh_token": "your-epic-refresh-token",
      "auth_code": "",
      "account_id": "",
      "display_name": ""
    },
    "steam": {
      "session_ticket": "",
      "steam_id_64": "",
      "account_name": ""
    }
  },
  "ballchasing": {
    "api_key": "your-ballchasing-api-key",
    "visibility": "public",
    "base_url": "https://ballchasing.com/api",
    "timeout": "60s",
    "max_retries": 3
  },
  "sync": {
    "poll_interval": "5m",
    "replay_dir": "./replays",
    "db_path": "./rl-sync.db",
    "keep_local_files": true,
    "download_timeout": "30s",
    "dry_run": false,
    "once": false
  },
  "logging": {
    "level": "info",
    "format": "text"
  }
}
```

---

## 5. Unit Test Strategy (`internal/config/config_test.go`)

The unit test suite validates every layer of the precedence hierarchy, file unmarshaling, duration handling, environment overrides, CLI overrides, and semantic validation rules:

```go
package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
)

func TestConfig_Defaults(t *testing.T) {
	cfg := config.NewDefaultConfig()

	if cfg.Auth.Provider != "epic" {
		t.Errorf("expected default provider 'epic', got %q", cfg.Auth.Provider)
	}
	if cfg.Ballchasing.Visibility != "public" {
		t.Errorf("expected default visibility 'public', got %q", cfg.Ballchasing.Visibility)
	}
	if cfg.Ballchasing.BaseURL != "https://ballchasing.com/api" {
		t.Errorf("expected default base URL 'https://ballchasing.com/api', got %q", cfg.Ballchasing.BaseURL)
	}
	if cfg.Ballchasing.Timeout.Duration() != 60*time.Second {
		t.Errorf("expected default timeout 60s, got %v", cfg.Ballchasing.Timeout)
	}
	if cfg.Ballchasing.MaxRetries != 3 {
		t.Errorf("expected default max retries 3, got %d", cfg.Ballchasing.MaxRetries)
	}
	if cfg.Sync.PollInterval.Duration() != 5*time.Minute {
		t.Errorf("expected default poll interval 5m, got %v", cfg.Sync.PollInterval)
	}
	if cfg.Sync.ReplayDir != "./replays" {
		t.Errorf("expected default replay dir './replays', got %q", cfg.Sync.ReplayDir)
	}
	if cfg.Sync.DBPath != "./rl-sync.db" {
		t.Errorf("expected default DB path './rl-sync.db', got %q", cfg.Sync.DBPath)
	}
	if !cfg.Sync.KeepLocalFiles {
		t.Errorf("expected default keep local files true")
	}
	if cfg.Logging.Level != "info" || cfg.Logging.Format != "text" {
		t.Errorf("expected default logging info/text, got %s/%s", cfg.Logging.Level, cfg.Logging.Format)
	}
}

func TestConfig_LoadYAML(t *testing.T) {
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "config.yaml")

	content := `
auth:
  provider: "epic"
  epic:
    refresh_token: "test-epic-token"
ballchasing:
  api_key: "test-ballchasing-key"
  visibility: "unlisted"
  timeout: "45s"
sync:
  poll_interval: "2m"
  replay_dir: "/tmp/custom-replays"
  db_path: "/tmp/custom.db"
logging:
  level: "debug"
  format: "json"
`
	if err := os.WriteFile(yamlPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write yaml: %v", err)
	}

	cfg, err := config.Load(config.CLIFlags{ConfigPath: yamlPath})
	if err != nil {
		t.Fatalf("unexpected error loading yaml config: %v", err)
	}

	if cfg.Auth.Epic.RefreshToken != "test-epic-token" {
		t.Errorf("expected refresh token 'test-epic-token', got %q", cfg.Auth.Epic.RefreshToken)
	}
	if cfg.Ballchasing.Visibility != "unlisted" {
		t.Errorf("expected visibility 'unlisted', got %q", cfg.Ballchasing.Visibility)
	}
	if cfg.Ballchasing.Timeout.Duration() != 45*time.Second {
		t.Errorf("expected timeout 45s, got %v", cfg.Ballchasing.Timeout)
	}
	if cfg.Sync.PollInterval.Duration() != 2*time.Minute {
		t.Errorf("expected poll interval 2m, got %v", cfg.Sync.PollInterval)
	}
	if cfg.Sync.ReplayDir != "/tmp/custom-replays" {
		t.Errorf("expected replay dir '/tmp/custom-replays', got %q", cfg.Sync.ReplayDir)
	}
	if cfg.Logging.Level != "debug" || cfg.Logging.Format != "json" {
		t.Errorf("expected debug/json, got %s/%s", cfg.Logging.Level, cfg.Logging.Format)
	}
}

func TestConfig_LoadJSON(t *testing.T) {
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, "config.json")

	content := `{
  "auth": {
    "provider": "steam",
    "steam": {
      "session_ticket": "deadbeef1234",
      "steam_id_64": "76561198000000001"
    }
  },
  "ballchasing": {
    "api_key": "steam-user-key",
    "visibility": "private"
  },
  "sync": {
    "poll_interval": "10m"
  }
}`
	if err := os.WriteFile(jsonPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write json: %v", err)
	}

	cfg, err := config.Load(config.CLIFlags{ConfigPath: jsonPath})
	if err != nil {
		t.Fatalf("unexpected error loading json config: %v", err)
	}

	if cfg.Auth.Provider != "steam" {
		t.Errorf("expected steam provider, got %q", cfg.Auth.Provider)
	}
	if cfg.Auth.Steam.SessionTicket != "deadbeef1234" {
		t.Errorf("expected session ticket 'deadbeef1234', got %q", cfg.Auth.Steam.SessionTicket)
	}
	if cfg.Auth.Steam.SteamID64 != "76561198000000001" {
		t.Errorf("expected steam ID '76561198000000001', got %q", cfg.Auth.Steam.SteamID64)
	}
	if cfg.Ballchasing.Visibility != "private" {
		t.Errorf("expected visibility 'private', got %q", cfg.Ballchasing.Visibility)
	}
	if cfg.Sync.PollInterval.Duration() != 10*time.Minute {
		t.Errorf("expected poll interval 10m, got %v", cfg.Sync.PollInterval)
	}
}

func TestConfig_EnvOverrides(t *testing.T) {
	t.Setenv("RL_SYNC_AUTH_PROVIDER", "steam")
	t.Setenv("RL_SYNC_STEAM_SESSION_TICKET", "env-steam-ticket")
	t.Setenv("RL_SYNC_STEAM_ID_64", "76561198000000002")
	t.Setenv("RL_SYNC_BALLCHASING_API_KEY", "env-api-key")
	t.Setenv("RL_SYNC_BALLCHASING_VISIBILITY", "unlisted")
	t.Setenv("RL_SYNC_POLL_INTERVAL", "15m")
	t.Setenv("RL_SYNC_KEEP_LOCAL_FILES", "false")
	t.Setenv("RL_SYNC_DRY_RUN", "true")

	cfg, err := config.Load(config.CLIFlags{})
	if err != nil {
		t.Fatalf("failed to load config with env vars: %v", err)
	}

	if cfg.Auth.Provider != "steam" {
		t.Errorf("expected provider steam, got %q", cfg.Auth.Provider)
	}
	if cfg.Auth.Steam.SessionTicket != "env-steam-ticket" {
		t.Errorf("expected session ticket 'env-steam-ticket', got %q", cfg.Auth.Steam.SessionTicket)
	}
	if cfg.Ballchasing.APIKey != "env-api-key" {
		t.Errorf("expected ballchasing api key 'env-api-key', got %q", cfg.Ballchasing.APIKey)
	}
	if cfg.Sync.PollInterval.Duration() != 15*time.Minute {
		t.Errorf("expected poll interval 15m, got %v", cfg.Sync.PollInterval)
	}
	if cfg.Sync.KeepLocalFiles != false {
		t.Errorf("expected keep_local_files false, got %v", cfg.Sync.KeepLocalFiles)
	}
	if cfg.Sync.DryRun != true {
		t.Errorf("expected dry_run true, got %v", cfg.Sync.DryRun)
	}
}

func TestConfig_PrecedenceHierarchy(t *testing.T) {
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "config.yaml")

	// Config file sets poll_interval to 10m
	yamlContent := `
auth:
  provider: "epic"
  epic:
    refresh_token: "file-token"
ballchasing:
  api_key: "file-key"
sync:
  poll_interval: "10m"
`
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write yaml: %v", err)
	}

	// 1. Base test: YAML overrides default 5m -> 10m
	cfg, err := config.Load(config.CLIFlags{ConfigPath: yamlPath})
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if cfg.Sync.PollInterval.Duration() != 10*time.Minute {
		t.Fatalf("expected 10m from file, got %v", cfg.Sync.PollInterval)
	}

	// 2. Env var overrides YAML file (10m -> 15m)
	t.Setenv("RL_SYNC_POLL_INTERVAL", "15m")
	cfg, err = config.Load(config.CLIFlags{ConfigPath: yamlPath})
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if cfg.Sync.PollInterval.Duration() != 15*time.Minute {
		t.Fatalf("expected 15m from env, got %v", cfg.Sync.PollInterval)
	}

	// 3. CLI flag overrides Env var (15m -> 20m)
	cliPoll := 20 * time.Minute
	cfg, err = config.Load(config.CLIFlags{
		ConfigPath:   yamlPath,
		PollInterval: &cliPoll,
	})
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if cfg.Sync.PollInterval.Duration() != 20*time.Minute {
		t.Fatalf("expected 20m from CLI flag, got %v", cfg.Sync.PollInterval)
	}
}

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
			name: "epic missing both credentials",
			modify: func(c *config.Config) {
				c.Auth.Provider = "epic"
				c.Auth.Epic.RefreshToken = ""
				c.Auth.Epic.AuthCode = ""
			},
			expectedError: "epic provider requires either 'refresh_token' or 'auth_code'",
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

func TestConfig_MissingExplicitConfigFile(t *testing.T) {
	_, err := config.Load(config.CLIFlags{
		ConfigPath: "/nonexistent/path/to/config.yaml",
	})
	if err == nil {
		t.Fatal("expected error loading non-existent explicit config file, got nil")
	}
}
```

---

## 6. Caveats

1. **Duration unmarshaling in standard Go JSON**: Standard `encoding/json` cannot parse `"5m"` directly into `time.Duration`. Implementing the custom `Duration` wrapper type is mandatory to support human-friendly durations across both YAML and JSON templates without forcing nanosecond integers.
2. **SteamID64 requirement**: Steam authentication requires both `session_ticket` AND `steam_id_64`. The PsyNet RPC client requires the SteamID64 to construct the composite `PlayerID`. If a user only provides a session ticket without their SteamID64, the daemon cannot authenticate.
3. **Environment variable scoping**: All environment variables use prefix `RL_SYNC_` to prevent namespace collisions with other tools.
4. **Zero CGO**: Pure Go SQLite (`modernc.org/sqlite`) must be used exclusively to guarantee builds succeed without GCC or MinGW toolchains on Windows.

---

## 7. Conclusion

The configuration subsystem (`internal/config`) is architecturally sound and cleanly decouples all runtime settings from application logic.
Key implementation takeaways:
- **`go.mod`**: Module `github.com/dank/rl-api-utils` with Go 1.24+, `github.com/dank/rlapi`, `modernc.org/sqlite`, `github.com/gorilla/websocket`, and `gopkg.in/yaml.v3`.
- **Precedence Hierarchy**: CLI flags > Env vars (`RL_SYNC_*`) > Config file (`.yaml` / `.json`) > Hardcoded defaults.
- **Custom `Duration`**: Enables human-readable `"5m"`, `"30s"` strings across both YAML and JSON formats.
- **Validation**: Enforces provider presence (`"epic"` or `"steam"`), credentials presence, visibility enum (`"public"`, `"unlisted"`, `"private"`), positive intervals, and accumulates all errors using `errors.Join`.
- **Templates**: Annotated `configs/config.example.yaml` and `configs/config.example.json` templates provide immediate guidance to operators.

---

## 8. Verification Method

To independently verify this specification once implemented in Milestone 1:

1. **Verify Go Module & Dependencies**:
   ```bash
   cd d:\code\rl-api-utils
   go mod verify
   go mod tidy
   ```
   *Expected outcome*: `go.mod` and `go.sum` are valid, dependencies download cleanly without CGO compiler errors.

2. **Execute Configuration Unit Tests**:
   ```bash
   go test -v -race ./internal/config/...
   ```
   *Expected outcome*: 100% test pass covering defaults, YAML loading, JSON loading, environment variable overrides, CLI flag overrides, multi-error validation, and custom duration parsing.

3. **Invalidation Conditions**:
   - If `json.Unmarshal` fails on `"poll_interval": "5m"` due to standard `time.Duration` type without the custom `Duration` wrapper.
   - If Steam authentication is attempted without validating the presence of `steam_id_64`.
   - If an invalid visibility string (e.g. `"hidden"`) is accepted without error.
   - If CLI flags fail to override environment variables or configuration files.
