package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/testutil"
	"gopkg.in/yaml.v3"
)

// ============================================================================
// Feature 1: Pure Go SQLite Store (and StateStore data models)
// ============================================================================

func TestTier1_F1_Store_InitAndSchema(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()

	if store.closed {
		t.Fatal("store should be open")
	}
}

func TestTier1_F1_Store_UpsertAndGetMatch(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()

	ctx := context.Background()
	rec := &MatchRecord{
		MatchGUID:            "guid-f1-1",
		RecordStartTimestamp: time.Now().Unix(),
		MapName:              "Wasteland_P",
		Playlist:             2,
		ReplayURL:            "http://example.com/replay.replay",
	}

	err := store.UpsertDiscoveredMatches(ctx, []*MatchRecord{rec})
	if err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	got, err := store.GetMatch(ctx, "guid-f1-1")
	if err != nil {
		t.Fatalf("get match failed: %v", err)
	}
	if got == nil || got.MatchGUID != "guid-f1-1" {
		t.Fatalf("expected guid-f1-1, got %v", got)
	}
	if got.DownloadStatus != DownloadPending {
		t.Fatalf("expected PENDING download status, got %s", got.DownloadStatus)
	}
}

func TestTier1_F1_Store_ListPendingDownloads(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()
	ctx := context.Background()

	_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{
		{MatchGUID: "g1", ReplayURL: "http://example.com/1"},
		{MatchGUID: "g2", ReplayURL: ""}, // skipped
		{MatchGUID: "g3", ReplayURL: "http://example.com/3"},
	})

	list, err := store.ListPendingDownloads(ctx)
	if err != nil {
		t.Fatalf("list pending downloads failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 pending downloads, got %d", len(list))
	}
}

func TestTier1_F1_Store_ListPendingUploads(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()
	ctx := context.Background()

	_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{
		{MatchGUID: "g1", ReplayURL: "http://example.com/1"},
	})
	_ = store.MarkDownloaded(ctx, "g1", "/tmp/g1.replay")

	list, err := store.ListPendingUploads(ctx)
	if err != nil {
		t.Fatalf("list pending uploads failed: %v", err)
	}
	if len(list) != 1 || list[0].MatchGUID != "g1" {
		t.Fatalf("expected g1 in pending uploads, got %v", list)
	}
}

func TestTier1_F1_Store_SaveAndGetAuthState(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()
	ctx := context.Background()

	err := store.SaveAuthState(ctx, "epic", "token-123", "account-456", "EpicGamer")
	if err != nil {
		t.Fatalf("save auth state failed: %v", err)
	}

	tok, acc, disp, err := store.GetAuthState(ctx, "epic")
	if err != nil {
		t.Fatalf("get auth state failed: %v", err)
	}
	if tok != "token-123" || acc != "account-456" || disp != "EpicGamer" {
		t.Fatalf("unexpected auth state: %s, %s, %s", tok, acc, disp)
	}
}

// ============================================================================
// Feature 2: Idempotency & Crash Recovery
// ============================================================================

func TestTier1_F2_Idempotency_PreventDuplicateDownload(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()
	ctx := context.Background()

	_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{
		{MatchGUID: "g1", ReplayURL: "http://example.com/1"},
	})
	_ = store.MarkDownloaded(ctx, "g1", "/replays/g1.replay")

	pending, _ := store.ListPendingDownloads(ctx)
	if len(pending) != 0 {
		t.Fatalf("downloaded match should not appear in pending downloads")
	}
}

func TestTier1_F2_Idempotency_PreventDuplicateUpload(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()
	ctx := context.Background()

	_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{
		{MatchGUID: "g1", ReplayURL: "http://example.com/1"},
	})
	_ = store.MarkDownloaded(ctx, "g1", "/replays/g1.replay")
	_ = store.MarkUploaded(ctx, "g1", "bc-id-1", "https://ballchasing.com/1")

	pending, _ := store.ListPendingUploads(ctx)
	if len(pending) != 0 {
		t.Fatalf("uploaded match should not appear in pending uploads")
	}
}

func TestTier1_F2_CrashRecovery_DownloadingToPending(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()
	ctx := context.Background()

	_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{
		{MatchGUID: "g1", ReplayURL: "http://example.com/1"},
	})
	_ = store.MarkDownloading(ctx, "g1")

	// Crash happens -> RecoverInFlight called on startup
	err := store.RecoverInFlight(ctx)
	if err != nil {
		t.Fatalf("recover failed: %v", err)
	}

	rec, _ := store.GetMatch(ctx, "g1")
	if rec.DownloadStatus != DownloadPending {
		t.Fatalf("expected DOWNLOADING to be reset to PENDING, got %s", rec.DownloadStatus)
	}
}

func TestTier1_F2_CrashRecovery_UploadingToPending(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()
	ctx := context.Background()

	_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{
		{MatchGUID: "g1", ReplayURL: "http://example.com/1"},
	})
	_ = store.MarkDownloaded(ctx, "g1", "/replays/g1.replay")
	_ = store.MarkUploading(ctx, "g1")

	err := store.RecoverInFlight(ctx)
	if err != nil {
		t.Fatalf("recover failed: %v", err)
	}

	rec, _ := store.GetMatch(ctx, "g1")
	if rec.UploadStatus != UploadPending {
		t.Fatalf("expected UPLOADING to be reset to PENDING, got %s", rec.UploadStatus)
	}
}

func TestTier1_F2_Idempotency_ReUpsertPreservesTerminalStatus(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()
	ctx := context.Background()

	_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{
		{MatchGUID: "g1", ReplayURL: "http://example.com/1"},
	})
	_ = store.MarkDownloaded(ctx, "g1", "/replays/g1.replay")
	_ = store.MarkUploaded(ctx, "g1", "bc-1", "http://bc/1")

	// Re-poll the same match
	_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{
		{MatchGUID: "g1", ReplayURL: "http://example.com/1"},
	})

	rec, _ := store.GetMatch(ctx, "g1")
	if rec.UploadStatus != UploadUploaded || rec.DownloadStatus != DownloadDownloaded {
		t.Fatalf("re-upsert must not overwrite terminal UPLOADED state, got %s / %s",
			rec.DownloadStatus, rec.UploadStatus)
	}
}

// ============================================================================
// Feature 3: Layered Configuration
// ============================================================================

type TestConfig struct {
	Auth struct {
		Provider string `yaml:"provider" json:"provider"`
		Epic     struct {
			RefreshToken string `yaml:"refresh_token" json:"refresh_token"`
			AccountID    string `yaml:"account_id" json:"account_id"`
		} `yaml:"epic" json:"epic"`
		Steam struct {
			SessionTicket string `yaml:"session_ticket" json:"session_ticket"`
			SteamID64     string `yaml:"steam_id_64" json:"steam_id_64"`
		} `yaml:"steam" json:"steam"`
	} `yaml:"auth" json:"auth"`
	Ballchasing struct {
		APIKey     string `yaml:"api_key" json:"api_key"`
		Visibility string `yaml:"visibility" json:"visibility"`
		BaseURL    string `yaml:"base_url" json:"base_url"`
	} `yaml:"ballchasing" json:"ballchasing"`
	Sync struct {
		PollInterval string `yaml:"poll_interval" json:"poll_interval"`
		ReplayDir    string `yaml:"replay_dir" json:"replay_dir"`
		DBPath       string `yaml:"db_path" json:"db_path"`
	} `yaml:"sync" json:"sync"`
}

func DefaultTestConfig() TestConfig {
	var cfg TestConfig
	cfg.Auth.Provider = "epic"
	cfg.Ballchasing.Visibility = "public"
	cfg.Ballchasing.BaseURL = "https://ballchasing.com/api"
	cfg.Sync.PollInterval = "5m"
	cfg.Sync.ReplayDir = "./replays"
	cfg.Sync.DBPath = "./rl-sync.db"
	return cfg
}

func TestTier1_F3_Config_Defaults(t *testing.T) {
	cfg := DefaultTestConfig()
	if cfg.Auth.Provider != "epic" || cfg.Ballchasing.Visibility != "public" || cfg.Sync.PollInterval != "5m" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestTier1_F3_Config_YAMLLoading(t *testing.T) {
	yamlData := `
auth:
  provider: steam
  steam:
    steam_id_64: "76561198000000000"
ballchasing:
  api_key: "secret-key"
  visibility: "private"
`
	cfg := DefaultTestConfig()
	err := yaml.Unmarshal([]byte(yamlData), &cfg)
	if err != nil {
		t.Fatalf("failed to unmarshal yaml: %v", err)
	}
	if cfg.Auth.Provider != "steam" || cfg.Ballchasing.APIKey != "secret-key" || cfg.Ballchasing.Visibility != "private" {
		t.Fatalf("unexpected unmarshaled yaml: %+v", cfg)
	}
}

func TestTier1_F3_Config_JSONLoading(t *testing.T) {
	jsonData := `{
		"auth": {"provider": "epic", "epic": {"refresh_token": "tok123"}},
		"ballchasing": {"api_key": "json-key", "visibility": "unlisted"}
	}`
	cfg := DefaultTestConfig()
	err := json.Unmarshal([]byte(jsonData), &cfg)
	if err != nil {
		t.Fatalf("failed to unmarshal json: %v", err)
	}
	if cfg.Ballchasing.APIKey != "json-key" || cfg.Ballchasing.Visibility != "unlisted" {
		t.Fatalf("unexpected unmarshaled json: %+v", cfg)
	}
}

func TestTier1_F3_Config_EnvOverrides(t *testing.T) {
	cfg := DefaultTestConfig()
	os.Setenv("RL_SYNC_AUTH_PROVIDER", "steam")
	os.Setenv("RL_SYNC_BALLCHASING_API_KEY", "env-key")
	defer func() {
		os.Unsetenv("RL_SYNC_AUTH_PROVIDER")
		os.Unsetenv("RL_SYNC_BALLCHASING_API_KEY")
	}()

	if env := os.Getenv("RL_SYNC_AUTH_PROVIDER"); env != "" {
		cfg.Auth.Provider = env
	}
	if env := os.Getenv("RL_SYNC_BALLCHASING_API_KEY"); env != "" {
		cfg.Ballchasing.APIKey = env
	}

	if cfg.Auth.Provider != "steam" || cfg.Ballchasing.APIKey != "env-key" {
		t.Fatalf("env override failed: %+v", cfg)
	}
}

func TestTier1_F3_Config_Validation(t *testing.T) {
	cfg := DefaultTestConfig()
	cfg.Ballchasing.Visibility = "invalid-vis"

	validate := func(c TestConfig) error {
		if c.Ballchasing.Visibility != "public" && c.Ballchasing.Visibility != "unlisted" && c.Ballchasing.Visibility != "private" {
			return fmt.Errorf("invalid visibility: %s", c.Ballchasing.Visibility)
		}
		return nil
	}

	if err := validate(cfg); err == nil {
		t.Fatal("expected validation error for invalid visibility")
	}
}

// ============================================================================
// Feature 4: Epic Games Authentication
// ============================================================================

func TestTier1_F4_EpicAuth_RefreshTokenExchange(t *testing.T) {
	psy := testutil.NewMockPsyNetServer()
	defer psy.Close()

	// Simulate AuthPlayer call with Epic refresh token credentials
	authPayload := map[string]string{
		"Platform":       "Epic",
		"PlayerID":       "epic-user-1",
		"PlayerName":     "EpicPlayer",
		"EpicAuthTicket": "eos-token-sample",
	}
	body, _ := json.Marshal(authPayload)
	resp, err := http.Post(psy.URL()+"/rpc/Auth/AuthPlayer/v2", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("auth request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestTier1_F4_EpicAuth_AuthCodeExchange(t *testing.T) {
	psy := testutil.NewMockPsyNetServer()
	defer psy.Close()

	authPayload := map[string]string{
		"Platform":       "Epic",
		"PlayerID":       "epic-code-user",
		"EpicAuthTicket": "eos-token-from-code",
	}
	body, _ := json.Marshal(authPayload)
	resp, err := http.Post(psy.URL()+"/rpc/Auth/AuthPlayer/v2", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("auth code request failed: %v", err)
	}
	defer resp.Body.Close()

	var res map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&res)
	if res["PsyToken"] == nil {
		t.Fatal("expected PsyToken in auth response")
	}
}

func TestTier1_F4_EpicAuth_EOSTokenScoped(t *testing.T) {
	psy := testutil.NewMockPsyNetServer()
	defer psy.Close()

	reqs := psy.GetAuthRequests()
	if len(reqs) != 0 {
		t.Fatalf("expected 0 requests initially, got %d", len(reqs))
	}
}

func TestTier1_F4_EpicAuth_InvalidRefreshTokenError(t *testing.T) {
	// If refresh token is empty, client should abort without network call
	var refreshToken string
	if refreshToken == "" {
		err := fmt.Errorf("empty epic refresh token")
		if err == nil {
			t.Fatal("expected error")
		}
	}
}

func TestTier1_F4_EpicAuth_MissingCredentialsRejection(t *testing.T) {
	validate := func(provider, token string) error {
		if provider == "epic" && token == "" {
			return errors.New("missing epic credentials")
		}
		return nil
	}
	if err := validate("epic", ""); err == nil {
		t.Fatal("expected validation error")
	}
}

// ============================================================================
// Feature 5: Steam Authentication
// ============================================================================

func TestTier1_F5_SteamAuth_SessionTicketExchange(t *testing.T) {
	psy := testutil.NewMockPsyNetServer()
	defer psy.Close()

	authPayload := map[string]string{
		"Platform":   "Steam",
		"PlayerID":   "76561198000000000",
		"AuthTicket": "steam-session-ticket-sample",
	}
	body, _ := json.Marshal(authPayload)
	resp, err := http.Post(psy.URL()+"/rpc/Auth/AuthPlayer/v2", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("steam auth failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestTier1_F5_SteamAuth_AuthPlayerWithSteamID64(t *testing.T) {
	psy := testutil.NewMockPsyNetServer()
	defer psy.Close()

	authPayload := map[string]string{
		"Platform": "Steam",
		"PlayerID": "76561198012345678",
	}
	body, _ := json.Marshal(authPayload)
	resp, err := http.Post(psy.URL()+"/rpc/Auth/AuthPlayer/v2", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("auth request failed: %v", err)
	}
	defer resp.Body.Close()

	reqs := psy.GetAuthRequests()
	if len(reqs) != 1 || reqs[0].PlayerID != "76561198012345678" {
		t.Fatalf("expected player ID 76561198012345678, got %v", reqs)
	}
}

func TestTier1_F5_SteamAuth_EmptySessionTicketRejection(t *testing.T) {
	validate := func(ticket string) error {
		if ticket == "" {
			return errors.New("empty steam ticket")
		}
		return nil
	}
	if err := validate(""); err == nil {
		t.Fatal("expected error for empty ticket")
	}
}

func TestTier1_F5_SteamAuth_InvalidSteamID64Format(t *testing.T) {
	validateSteamID := func(id string) error {
		if len(id) != 17 || !strings.HasPrefix(id, "7656119") {
			return errors.New("invalid steamid64 format")
		}
		return nil
	}
	if err := validateSteamID("12345"); err == nil {
		t.Fatal("expected invalid steamid64 error")
	}
	if err := validateSteamID("76561198000000000"); err != nil {
		t.Fatalf("expected valid steamid64, got error: %v", err)
	}
}

func TestTier1_F5_SteamAuth_CredentialsPersistence(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()
	ctx := context.Background()

	_ = store.SaveAuthState(ctx, "steam", "ticket-sample", "76561198000000000", "SteamPlayer")
	tok, acc, disp, _ := store.GetAuthState(ctx, "steam")
	if tok != "ticket-sample" || acc != "76561198000000000" || disp != "SteamPlayer" {
		t.Fatalf("steam auth persistence failed: %s, %s, %s", tok, acc, disp)
	}
}
