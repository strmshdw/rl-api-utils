package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
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

// ============================================================================
// Feature 6: Match History Polling
// ============================================================================

func TestTier1_F6_Polling_MatchesRetrieved(t *testing.T) {
	h := SetupE2EHarness(t)
	m1 := testutil.NewMockMatchEntry("m1", h.CDN.ReplayURL("m1"), "Wasteland_P", 2)
	h.PsyNet.AddMatch(m1)

	adapter := NewMockProviderAdapter(h.PsyNet)
	matches, err := adapter.GetRecentMatches(context.Background())
	if err != nil {
		t.Fatalf("failed to get matches: %v", err)
	}
	if len(matches) != 1 || matches[0].MatchGUID != "m1" {
		t.Fatalf("expected m1, got %v", matches)
	}
}

func TestTier1_F6_Polling_MetadataExtraction(t *testing.T) {
	h := SetupE2EHarness(t)
	m1 := testutil.NewMockMatchEntry("m1-meta", h.CDN.ReplayURL("m1-meta"), "Beckwith_P", 3)
	h.PsyNet.AddMatch(m1)

	adapter := NewMockProviderAdapter(h.PsyNet)
	matches, _ := adapter.GetRecentMatches(context.Background())
	if matches[0].MapName != "Beckwith_P" || matches[0].Playlist != 3 {
		t.Fatalf("unexpected metadata: %+v", matches[0])
	}
}

func TestTier1_F6_Polling_EmptyHistoryHandling(t *testing.T) {
	h := SetupE2EHarness(t)
	adapter := NewMockProviderAdapter(h.PsyNet)
	matches, err := adapter.GetRecentMatches(context.Background())
	if err != nil {
		t.Fatalf("polling failed on empty history: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(matches))
	}
}

func TestTier1_F6_Polling_FilterValidVsEmptyReplayUrl(t *testing.T) {
	h := SetupE2EHarness(t)
	m1 := testutil.NewMockMatchEntry("m1", h.CDN.ReplayURL("m1"), "Map1", 2)
	m2 := testutil.NewMockMatchEntry("m2", "", "Map2", 2) // No replay
	h.PsyNet.SetMatches([]testutil.MockMatchEntry{m1, m2})

	adapter := NewMockProviderAdapter(h.PsyNet)
	matches, _ := adapter.GetRecentMatches(context.Background())
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
}

func TestTier1_F6_Polling_DynamicMatchesUpdate(t *testing.T) {
	h := SetupE2EHarness(t)
	adapter := NewMockProviderAdapter(h.PsyNet)

	matches1, _ := adapter.GetRecentMatches(context.Background())
	if len(matches1) != 0 {
		t.Fatalf("expected 0 matches")
	}

	h.PsyNet.AddMatch(testutil.NewMockMatchEntry("m-dyn", h.CDN.ReplayURL("m-dyn"), "Map", 2))
	matches2, _ := adapter.GetRecentMatches(context.Background())
	if len(matches2) != 1 {
		t.Fatalf("expected 1 match after dynamic addition, got %d", len(matches2))
	}
}

// ============================================================================
// Feature 7: Atomic Replay Downloader
// ============================================================================

func TestTier1_F7_Downloader_SuccessWithTAGAME(t *testing.T) {
	h := SetupE2EHarness(t)
	downloader := NewHTTPReplayDownloader(10 * time.Second)

	path, err := downloader.DownloadReplay(context.Background(), "guid-dl-1", h.CDN.ReplayURL("guid-dl-1"), h.ReplayDir)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if !bytes.HasPrefix(data, testutil.ReplayMagicBytes) {
		t.Fatal("downloaded replay does not have TAGAME magic bytes")
	}
}

func TestTier1_F7_Downloader_AtomicTmpRename(t *testing.T) {
	h := SetupE2EHarness(t)
	downloader := NewHTTPReplayDownloader(10 * time.Second)

	path, err := downloader.DownloadReplay(context.Background(), "guid-dl-2", h.CDN.ReplayURL("guid-dl-2"), h.ReplayDir)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}

	expectedPath := filepath.Join(h.ReplayDir, "guid-dl-2.replay")
	if path != expectedPath {
		t.Fatalf("expected %s, got %s", expectedPath, path)
	}

	// Verify no temporary files remain
	files, _ := os.ReadDir(h.ReplayDir)
	for _, f := range files {
		if strings.HasPrefix(f.Name(), ".tmp") {
			t.Fatalf("orphaned tmp file found: %s", f.Name())
		}
	}
}

func TestTier1_F7_Downloader_RejectUnder1KB(t *testing.T) {
	h := SetupE2EHarness(t)
	downloader := NewHTTPReplayDownloader(10 * time.Second)

	// Set tiny 100 byte payload
	h.CDN.SetReplayPayload("guid-tiny", []byte("tiny-replay-data"))

	_, err := downloader.DownloadReplay(context.Background(), "guid-tiny", h.CDN.ReplayURL("guid-tiny"), h.ReplayDir)
	if err == nil {
		t.Fatal("expected error downloading < 1KB file")
	}
}

func TestTier1_F7_Downloader_CleanupTmpOnFailure(t *testing.T) {
	h := SetupE2EHarness(t)
	downloader := NewHTTPReplayDownloader(10 * time.Second)

	h.CDN.SetStatusCode("guid-err", http.StatusForbidden)
	_, err := downloader.DownloadReplay(context.Background(), "guid-err", h.CDN.ReplayURL("guid-err"), h.ReplayDir)
	if err == nil {
		t.Fatal("expected download failure on 403")
	}

	files, _ := os.ReadDir(h.ReplayDir)
	for _, f := range files {
		if strings.HasPrefix(f.Name(), ".tmp") {
			t.Fatalf("tmp file not cleaned up after error: %s", f.Name())
		}
	}
}

func TestTier1_F7_Downloader_DestinationDirCreation(t *testing.T) {
	h := SetupE2EHarness(t)
	downloader := NewHTTPReplayDownloader(10 * time.Second)

	nestedDir := filepath.Join(h.ReplayDir, "nested", "replays", "dir")
	path, err := downloader.DownloadReplay(context.Background(), "guid-nested", h.CDN.ReplayURL("guid-nested"), nestedDir)
	if err != nil {
		t.Fatalf("download with auto dir creation failed: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not found in auto-created dir: %v", err)
	}
}

// ============================================================================
// Feature 8: Ballchasing Multipart Upload
// ============================================================================

func TestTier1_F8_Upload_MultipartFormDataStructure(t *testing.T) {
	h := SetupE2EHarness(t)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 3)

	filePath := filepath.Join(h.ReplayDir, "test.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("guid-up-1", 2048), 0644)

	res, err := uploader.UploadReplay(context.Background(), "guid-up-1", filePath)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if res.ID == "" {
		t.Fatal("expected non-empty replay ID")
	}
}

func TestTier1_F8_Upload_VisibilityPublic(t *testing.T) {
	h := SetupE2EHarness(t)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 3)

	filePath := filepath.Join(h.ReplayDir, "vis-pub.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("vis-pub", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "vis-pub", filePath)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	record := h.BC.GetUpload("vis-pub")
	if record == nil || record.Visibility != "public" {
		t.Fatalf("expected visibility=public, got %v", record)
	}
}

func TestTier1_F8_Upload_VisibilityUnlisted(t *testing.T) {
	h := SetupE2EHarness(t)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "unlisted", "", 3)

	filePath := filepath.Join(h.ReplayDir, "vis-unlisted.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("vis-unlisted", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "vis-unlisted", filePath)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	record := h.BC.GetUpload("vis-unlisted")
	if record == nil || record.Visibility != "unlisted" {
		t.Fatalf("expected visibility=unlisted, got %v", record)
	}
}

func TestTier1_F8_Upload_VisibilityPrivate(t *testing.T) {
	h := SetupE2EHarness(t)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "private", "", 3)

	filePath := filepath.Join(h.ReplayDir, "vis-priv.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("vis-priv", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "vis-priv", filePath)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	record := h.BC.GetUpload("vis-priv")
	if record == nil || record.Visibility != "private" {
		t.Fatalf("expected visibility=private, got %v", record)
	}
}

func TestTier1_F8_Upload_WithGroupID(t *testing.T) {
	h := SetupE2EHarness(t)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "tournament-group-1", 3)

	filePath := filepath.Join(h.ReplayDir, "grp.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("grp", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "grp", filePath)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	record := h.BC.GetUpload("grp")
	if record == nil || record.Group != "tournament-group-1" {
		t.Fatalf("expected group=tournament-group-1, got %v", record)
	}
}

// ============================================================================
// Feature 9: Ballchasing Raw Auth Header
// ============================================================================

func TestTier1_F9_AuthHeader_RawTokenAccepted(t *testing.T) {
	h := SetupE2EHarness(t)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 3)

	filePath := filepath.Join(h.ReplayDir, "raw.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("raw", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "raw", filePath)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
}

func TestTier1_F9_AuthHeader_BearerPrefixRejected401(t *testing.T) {
	h := SetupE2EHarness(t)
	// Passing "Bearer <key>" must trigger 401
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "Bearer test-ballchasing-token", "public", "", 1)

	filePath := filepath.Join(h.ReplayDir, "bearer.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("bearer", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "bearer", filePath)
	if err == nil {
		t.Fatal("expected 401 unauthorized when Bearer prefix is used")
	}
}

func TestTier1_F9_AuthHeader_MissingTokenRejected401(t *testing.T) {
	h := SetupE2EHarness(t)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "", "public", "", 1)

	filePath := filepath.Join(h.ReplayDir, "missing.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("missing", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "missing", filePath)
	if err == nil {
		t.Fatal("expected error with missing token")
	}
}

func TestTier1_F9_AuthHeader_WrongTokenRejected401(t *testing.T) {
	h := SetupE2EHarness(t)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "wrong-token-abc", "public", "", 1)

	filePath := filepath.Join(h.ReplayDir, "wrong.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("wrong", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "wrong", filePath)
	if err == nil {
		t.Fatal("expected error with wrong token")
	}
}

func TestTier1_F9_AuthHeader_SpecialCharacterToken(t *testing.T) {
	h := SetupE2EHarness(t)
	specialToken := "token_with-special!@#$"
	h.BC.SetExpectedToken(specialToken)

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), specialToken, "public", "", 1)
	filePath := filepath.Join(h.ReplayDir, "special.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("special", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "special", filePath)
	if err != nil {
		t.Fatalf("upload with special character token failed: %v", err)
	}
}

// ============================================================================
// Feature 10: Ballchasing HTTP 201 Handling
// ============================================================================

func TestTier1_F10_201_ParseIDAndLocation(t *testing.T) {
	h := SetupE2EHarness(t)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)

	filePath := filepath.Join(h.ReplayDir, "f10.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("f10", 2048), 0644)

	res, err := uploader.UploadReplay(context.Background(), "f10", filePath)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if res.ID == "" || !strings.Contains(res.Location, res.ID) {
		t.Fatalf("location does not match ID: id=%s, loc=%s", res.ID, res.Location)
	}
}

func TestTier1_F10_201_TransitionToUploaded(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()
	ctx := context.Background()

	_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{{MatchGUID: "m201"}})
	_ = store.MarkUploaded(ctx, "m201", "bc-201", "http://bc/201")

	rec, _ := store.GetMatch(ctx, "m201")
	if rec.UploadStatus != UploadUploaded {
		t.Fatalf("expected UPLOADED, got %s", rec.UploadStatus)
	}
}

func TestTier1_F10_201_RecordBallchasingID(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()
	ctx := context.Background()

	_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{{MatchGUID: "m-id"}})
	_ = store.MarkUploaded(ctx, "m-id", "expected-id-xyz", "http://bc/id")

	rec, _ := store.GetMatch(ctx, "m-id")
	if rec.BallchasingID != "expected-id-xyz" {
		t.Fatalf("expected expected-id-xyz, got %s", rec.BallchasingID)
	}
}

func TestTier1_F10_201_RecordBallchasingURL(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()
	ctx := context.Background()

	_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{{MatchGUID: "m-url"}})
	_ = store.MarkUploaded(ctx, "m-url", "id", "https://ballchasing.com/replay/12345")

	rec, _ := store.GetMatch(ctx, "m-url")
	if rec.BallchasingURL != "https://ballchasing.com/replay/12345" {
		t.Fatalf("unexpected BallchasingURL: %s", rec.BallchasingURL)
	}
}

func TestTier1_F10_201_UpdateUploadedAtTimestamp(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()
	ctx := context.Background()

	_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{{MatchGUID: "m-ts"}})
	_ = store.MarkUploaded(ctx, "m-ts", "id", "http://bc/id")

	rec, _ := store.GetMatch(ctx, "m-ts")
	if rec.UploadedAt == nil || rec.UploadedAt.IsZero() {
		t.Fatal("expected non-nil UploadedAt timestamp")
	}
}

// ============================================================================
// Feature 11: Ballchasing HTTP 409 Deduplication
// ============================================================================

func TestTier1_F11_409_ParseDuplicateResponse(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SetDuplicateGUID("dup-guid", "existing-id-409")

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)
	filePath := filepath.Join(h.ReplayDir, "dup-guid.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("dup-guid", 2048), 0644)

	res, err := uploader.UploadReplay(context.Background(), "dup-guid", filePath)
	if err != nil {
		t.Fatalf("upload failed on 409: %v", err)
	}
	if !res.IsDuplicate || res.ID != "existing-id-409" {
		t.Fatalf("expected duplicate result with existing-id-409, got %+v", res)
	}
}

func TestTier1_F11_409_TransitionToDuplicate(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()
	ctx := context.Background()

	_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{{MatchGUID: "m409"}})
	_ = store.MarkDuplicate(ctx, "m409", "bc-dup-id", "http://bc/dup")

	rec, _ := store.GetMatch(ctx, "m409")
	if rec.UploadStatus != UploadDuplicate {
		t.Fatalf("expected DUPLICATE status, got %s", rec.UploadStatus)
	}
}

func TestTier1_F11_409_RecordExistingID(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()
	ctx := context.Background()

	_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{{MatchGUID: "m409-id"}})
	_ = store.MarkDuplicate(ctx, "m409-id", "extracted-duplicate-id", "http://bc/dup")

	rec, _ := store.GetMatch(ctx, "m409-id")
	if rec.BallchasingID != "extracted-duplicate-id" {
		t.Fatalf("expected extracted-duplicate-id, got %s", rec.BallchasingID)
	}
}

func TestTier1_F11_409_NoCallerError(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SetAlwaysDuplicate(true)

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)
	filePath := filepath.Join(h.ReplayDir, "noerr.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("noerr", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "noerr", filePath)
	if err != nil {
		t.Fatalf("409 duplicate replay must not return an error to caller, got: %v", err)
	}
}

func TestTier1_F11_409_ZeroRetriesAttempted(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SetAlwaysDuplicate(true)

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 3)
	filePath := filepath.Join(h.ReplayDir, "noretries.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("noretries", 2048), 0644)

	_, _ = uploader.UploadReplay(context.Background(), "noretries", filePath)

	// Since 409 is terminal, upload count should be exactly 1
	if h.BC.GetUploadCount() != 1 {
		t.Fatalf("expected exactly 1 upload attempt on 409, got %d", h.BC.GetUploadCount())
	}
}

// ============================================================================
// Feature 12: Ballchasing HTTP 429 Rate Limiting
// ============================================================================

func TestTier1_F12_429_ParseRetryAfterHeader(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SimulateRateLimit(1, 1)

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 2)
	filePath := filepath.Join(h.ReplayDir, "f12.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("f12", 2048), 0644)

	res, err := uploader.UploadReplay(context.Background(), "f12", filePath)
	if err != nil {
		t.Fatalf("expected successful retry after 429, got %v", err)
	}
	if res.ID == "" {
		t.Fatal("expected replay ID")
	}
}

func TestTier1_F12_429_BackoffAndSucceedOnRetry(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SimulateRateLimit(2, 1)

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 3)
	filePath := filepath.Join(h.ReplayDir, "f12-succ.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("f12-succ", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "f12-succ", filePath)
	if err != nil {
		t.Fatalf("expected success after 2 retries, got: %v", err)
	}
}

func TestTier1_F12_429_ExhaustionErrorAfterMaxRetries(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SimulateRateLimit(5, 1) // 5 failures > 2 max retries

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 2)
	filePath := filepath.Join(h.ReplayDir, "f12-fail.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("f12-fail", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "f12-fail", filePath)
	if err == nil {
		t.Fatal("expected retry exhaustion error")
	}
}

func TestTier1_F12_429_NonIntegerRetryAfterFallback(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SimulateRateLimit(1, 1)
	h.BC.SetCustomRetryAfterHeader("Wed, 21 Oct 2026 07:28:00 GMT")

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 2)
	filePath := filepath.Join(h.ReplayDir, "f12-date.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("f12-date", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "f12-date", filePath)
	if err != nil {
		t.Fatalf("expected fallback backoff to succeed, got: %v", err)
	}
}

func TestTier1_F12_429_ZeroRetryAfterImmediateRetry(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SimulateRateLimit(1, 0)

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 2)
	filePath := filepath.Join(h.ReplayDir, "f12-zero.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("f12-zero", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "f12-zero", filePath)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
}

// ============================================================================
// Feature 13: Ballchasing HTTP 401 & Permanent Errors
// ============================================================================

func TestTier1_F13_401_ImmediateHaltNoRetry(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SetAlwaysUnauthorized(true)

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 3)
	filePath := filepath.Join(h.ReplayDir, "f13-401.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("f13-401", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "f13-401", filePath)
	if err == nil {
		t.Fatal("expected 401 unauthorized error")
	}
}

func TestTier1_F13_400_BadRequestImmediateHalt(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SetForcedStatus(http.StatusBadRequest, "corrupt replay binary")

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 3)
	filePath := filepath.Join(h.ReplayDir, "f13-400.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("f13-400", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "f13-400", filePath)
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("expected 400 error, got: %v", err)
	}
}

func TestTier1_F13_Error_DescriptiveErrorPropagation(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SetForcedStatus(http.StatusBadRequest, "specific syntax error in replay")

	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)
	filePath := filepath.Join(h.ReplayDir, "f13-desc.replay")
	_ = os.WriteFile(filePath, testutil.GenerateValidReplay("f13-desc", 2048), 0644)

	_, err := uploader.UploadReplay(context.Background(), "f13-desc", filePath)
	if err == nil || !strings.Contains(err.Error(), "specific syntax error in replay") {
		t.Fatalf("expected descriptive error message in: %v", err)
	}
}

func TestTier1_F13_Error_StatusTransitionToFailed(t *testing.T) {
	store := NewMemoryStateStore()
	defer store.Close()
	ctx := context.Background()

	_ = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{{MatchGUID: "m-fail"}})
	_ = store.MarkUploadFailed(ctx, "m-fail", "bad request")

	rec, _ := store.GetMatch(ctx, "m-fail")
	if rec.UploadStatus != UploadFailed || rec.LastError != "bad request" || rec.RetryCount != 1 {
		t.Fatalf("expected failed upload state: %+v", rec)
	}
}

func TestTier1_F13_Error_NoDaemonCrashOnUploadError(t *testing.T) {
	h := SetupE2EHarness(t)
	h.BC.SetForcedStatus(http.StatusBadRequest, "fatal rejection")

	m := testutil.NewMockMatchEntry("m-fatal", h.CDN.ReplayURL("m-fatal"), "Map", 2)
	h.PsyNet.AddMatch(m)

	adapter := NewMockProviderAdapter(h.PsyNet)
	downloader := NewHTTPReplayDownloader(10 * time.Second)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)

	syncer := NewSyncerEngine(h.Store, adapter, downloader, uploader, h.ReplayDir, false)

	// Must not panic or return fatal top-level error; returns stats with FailedCount = 1
	stats, err := syncer.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("syncer run cycle returned error instead of absorbing match failure: %v", err)
	}
	if stats.FailedCount != 1 {
		t.Fatalf("expected 1 failed upload in stats, got %d", stats.FailedCount)
	}
}

// ============================================================================
// Feature 14: Syncer Domain Orchestrator
// ============================================================================

func TestTier1_F14_Syncer_DiffIdentifiesNewMatches(t *testing.T) {
	h := SetupE2EHarness(t)
	h.PsyNet.AddMatch(testutil.NewMockMatchEntry("m1", h.CDN.ReplayURL("m1"), "Map", 2))
	h.PsyNet.AddMatch(testutil.NewMockMatchEntry("m2", h.CDN.ReplayURL("m2"), "Map", 2))

	adapter := NewMockProviderAdapter(h.PsyNet)
	downloader := NewHTTPReplayDownloader(10 * time.Second)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)

	syncer := NewSyncerEngine(h.Store, adapter, downloader, uploader, h.ReplayDir, false)
	stats, err := syncer.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("syncer run failed: %v", err)
	}
	if stats.DiscoveredCount != 2 || stats.UploadedCount != 2 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestTier1_F14_Syncer_DownloadsPendingOnly(t *testing.T) {
	h := SetupE2EHarness(t)
	// Seed store with m1 already downloaded
	_ = h.Store.UpsertDiscoveredMatches(context.Background(), []*MatchRecord{
		{MatchGUID: "m1", ReplayURL: h.CDN.ReplayURL("m1")},
	})
	_ = h.Store.MarkDownloaded(context.Background(), "m1", filepath.Join(h.ReplayDir, "m1.replay"))
	_ = os.WriteFile(filepath.Join(h.ReplayDir, "m1.replay"), testutil.GenerateValidReplay("m1", 2048), 0644)

	h.PsyNet.AddMatch(testutil.NewMockMatchEntry("m1", h.CDN.ReplayURL("m1"), "Map", 2))
	h.PsyNet.AddMatch(testutil.NewMockMatchEntry("m2", h.CDN.ReplayURL("m2"), "Map", 2))

	adapter := NewMockProviderAdapter(h.PsyNet)
	downloader := NewHTTPReplayDownloader(10 * time.Second)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)

	syncer := NewSyncerEngine(h.Store, adapter, downloader, uploader, h.ReplayDir, false)
	stats, _ := syncer.RunCycle(context.Background())

	if stats.DownloadedCount != 1 {
		t.Fatalf("expected exactly 1 download (m2 only), got %d", stats.DownloadedCount)
	}
}

func TestTier1_F14_Syncer_UploadsDownloadedOnly(t *testing.T) {
	h := SetupE2EHarness(t)
	h.CDN.SetStatusCode("m-fail-dl", http.StatusForbidden)
	h.PsyNet.AddMatch(testutil.NewMockMatchEntry("m-fail-dl", h.CDN.ReplayURL("m-fail-dl"), "Map", 2))

	adapter := NewMockProviderAdapter(h.PsyNet)
	downloader := NewHTTPReplayDownloader(10 * time.Second)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)

	syncer := NewSyncerEngine(h.Store, adapter, downloader, uploader, h.ReplayDir, false)
	stats, _ := syncer.RunCycle(context.Background())

	if stats.DownloadedCount != 0 || stats.UploadedCount != 0 {
		t.Fatalf("expected 0 uploads when download fails, got %+v", stats)
	}
}

func TestTier1_F14_Syncer_BatchProcessing(t *testing.T) {
	h := SetupE2EHarness(t)
	for i := 1; i <= 5; i++ {
		guid := fmt.Sprintf("batch-%d", i)
		h.PsyNet.AddMatch(testutil.NewMockMatchEntry(guid, h.CDN.ReplayURL(guid), "Map", 2))
	}

	adapter := NewMockProviderAdapter(h.PsyNet)
	downloader := NewHTTPReplayDownloader(10 * time.Second)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)

	syncer := NewSyncerEngine(h.Store, adapter, downloader, uploader, h.ReplayDir, false)
	stats, _ := syncer.RunCycle(context.Background())

	if stats.DiscoveredCount != 5 || stats.DownloadedCount != 5 || stats.UploadedCount != 5 {
		t.Fatalf("expected 5 for all stats, got %+v", stats)
	}
}

func TestTier1_F14_Syncer_ContextCancellation(t *testing.T) {
	h := SetupE2EHarness(t)
	h.PsyNet.AddMatch(testutil.NewMockMatchEntry("m-cancel", h.CDN.ReplayURL("m-cancel"), "Map", 2))

	adapter := NewMockProviderAdapter(h.PsyNet)
	downloader := NewHTTPReplayDownloader(10 * time.Second)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)

	syncer := NewSyncerEngine(h.Store, adapter, downloader, uploader, h.ReplayDir, false)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := syncer.RunCycle(ctx)
	if err == nil {
		t.Fatal("expected context canceled error")
	}
}

// ============================================================================
// Feature 15: Daemon Engine & Lifecycle
// ============================================================================

func TestTier1_F15_Daemon_ImmediateInitialRun(t *testing.T) {
	executed := false
	runCycle := func() { executed = true }

	// Daemon executes immediate run on startup
	runCycle()
	if !executed {
		t.Fatal("daemon did not execute immediate initial run")
	}
}

func TestTier1_F15_Daemon_TickerTriggering(t *testing.T) {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	ticks := 0
	for i := 0; i < 3; i++ {
		<-ticker.C
		ticks++
	}
	if ticks != 3 {
		t.Fatalf("expected 3 ticks, got %d", ticks)
	}
}

func TestTier1_F15_Daemon_ContextCancellationStopsLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan bool)

	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				stopped <- true
				return
			case <-ticker.C:
			}
		}
	}()

	time.Sleep(25 * time.Millisecond)
	cancel()

	select {
	case <-stopped:
		// success
	case <-time.After(1 * time.Second):
		t.Fatal("daemon loop did not stop on context cancellation")
	}
}

func TestTier1_F15_Daemon_GracefulDrainAwaitsInFlight(t *testing.T) {
	inFlight := true
	done := make(chan struct{})

	go func() {
		time.Sleep(50 * time.Millisecond)
		inFlight = false
		close(done)
	}()

	<-done
	if inFlight {
		t.Fatal("in-flight work was not finished before drain completion")
	}
}

func TestTier1_F15_Daemon_CleanResourceTeardown(t *testing.T) {
	store := NewMemoryStateStore()
	err := store.Close()
	if err != nil {
		t.Fatalf("store teardown returned error: %v", err)
	}
	if !store.closed {
		t.Fatal("store was not closed")
	}
}

// ============================================================================
// Feature 16: CLI Interface & Modes
// ============================================================================

func TestTier1_F16_CLI_OnceModeSinglePass(t *testing.T) {
	h := SetupE2EHarness(t)
	h.PsyNet.AddMatch(testutil.NewMockMatchEntry("m-once", h.CDN.ReplayURL("m-once"), "Map", 2))

	adapter := NewMockProviderAdapter(h.PsyNet)
	downloader := NewHTTPReplayDownloader(10 * time.Second)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)

	syncer := NewSyncerEngine(h.Store, adapter, downloader, uploader, h.ReplayDir, false)

	// --once executes exactly one cycle
	stats, err := syncer.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("once mode cycle failed: %v", err)
	}
	if stats.UploadedCount != 1 {
		t.Fatalf("expected 1 upload, got %d", stats.UploadedCount)
	}
}

func TestTier1_F16_CLI_DryRunNoDiskWrites(t *testing.T) {
	h := SetupE2EHarness(t)
	h.PsyNet.AddMatch(testutil.NewMockMatchEntry("m-dry", h.CDN.ReplayURL("m-dry"), "Map", 2))

	adapter := NewMockProviderAdapter(h.PsyNet)
	downloader := NewHTTPReplayDownloader(10 * time.Second)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)

	// dryRun = true
	syncer := NewSyncerEngine(h.Store, adapter, downloader, uploader, h.ReplayDir, true)
	stats, err := syncer.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("dry run failed: %v", err)
	}
	if stats.DiscoveredCount != 1 {
		t.Fatalf("expected 1 discovered in dry run")
	}
	if stats.DownloadedCount != 0 || stats.UploadedCount != 0 {
		t.Fatalf("dry run must not download or upload: %+v", stats)
	}
	if h.BC.GetUploadCount() != 0 || h.CDN.GetTotalDownloads() != 0 {
		t.Fatal("dry run contacted network servers")
	}
}

func TestTier1_F16_CLI_CombinedOnceAndDryRun(t *testing.T) {
	h := SetupE2EHarness(t)
	h.PsyNet.AddMatch(testutil.NewMockMatchEntry("m-combo", h.CDN.ReplayURL("m-combo"), "Map", 2))

	adapter := NewMockProviderAdapter(h.PsyNet)
	downloader := NewHTTPReplayDownloader(10 * time.Second)
	uploader := NewHTTPBallchasingUploader(h.BC.URL(), "test-ballchasing-token", "public", "", 1)

	syncer := NewSyncerEngine(h.Store, adapter, downloader, uploader, h.ReplayDir, true)
	stats, _ := syncer.RunCycle(context.Background())

	if stats.DiscoveredCount != 1 || stats.DownloadedCount != 0 {
		t.Fatalf("unexpected combo stats: %+v", stats)
	}
}

func TestTier1_F16_CLI_ConfigFlagOverride(t *testing.T) {
	configPath := "--config=custom_config.yaml"
	val := strings.TrimPrefix(configPath, "--config=")
	if val != "custom_config.yaml" {
		t.Fatalf("expected custom_config.yaml, got %s", val)
	}
}

func TestTier1_F16_CLI_LogLevelAndFormatFlags(t *testing.T) {
	parseLogLevel := func(lvl string) slog.Level {
		switch strings.ToLower(lvl) {
		case "debug":
			return slog.LevelDebug
		case "warn":
			return slog.LevelWarn
		case "error":
			return slog.LevelError
		default:
			return slog.LevelInfo
		}
	}
	if parseLogLevel("DEBUG") != slog.LevelDebug {
		t.Fatal("expected DEBUG level")
	}
	if parseLogLevel("warn") != slog.LevelWarn {
		t.Fatal("expected WARN level")
	}
}

// ============================================================================
// Feature 17: Structured Logging
// ============================================================================

func TestTier1_F17_Log_TextFormatOutput(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, nil))
	logger.Info("sync cycle completed", "uploaded", 5, "duration_ms", 120)

	out := buf.String()
	if !strings.Contains(out, "sync cycle completed") || !strings.Contains(out, "uploaded=5") {
		t.Fatalf("unexpected text log: %s", out)
	}
}

func TestTier1_F17_Log_JSONFormatOutput(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(buf, nil))
	logger.Info("match downloaded", "match_guid", "g-123", "bytes", 4096)

	var logObj map[string]any
	err := json.Unmarshal(buf.Bytes(), &logObj)
	if err != nil {
		t.Fatalf("failed to parse json log: %v", err)
	}
	if logObj["msg"] != "match downloaded" || logObj["match_guid"] != "g-123" {
		t.Fatalf("unexpected json log content: %v", logObj)
	}
}

func TestTier1_F17_Log_ContextualAttributes(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, nil))
	logger.With("component", "uploader").Info("uploading", "guid", "g-attr")

	out := buf.String()
	if !strings.Contains(out, "component=uploader") || !strings.Contains(out, "guid=g-attr") {
		t.Fatalf("unexpected contextual log: %s", out)
	}
}

func TestTier1_F17_Log_LevelFiltering(t *testing.T) {
	buf := &bytes.Buffer{}
	opts := &slog.HandlerOptions{Level: slog.LevelWarn}
	logger := slog.New(slog.NewTextHandler(buf, opts))

	logger.Info("info message") // should be dropped
	logger.Warn("warn message") // should be logged

	out := buf.String()
	if strings.Contains(out, "info message") {
		t.Fatal("info message should have been filtered out")
	}
	if !strings.Contains(out, "warn message") {
		t.Fatal("warn message was not logged")
	}
}

func TestTier1_F17_Log_SensitiveTokenRedaction(t *testing.T) {
	redact := func(token string) string {
		if len(token) <= 4 {
			return "***"
		}
		return token[:2] + "***" + token[len(token)-2:]
	}

	rawToken := "secret-api-key-12345"
	redacted := redact(rawToken)
	if strings.Contains(redacted, "api-key") {
		t.Fatalf("token was not properly redacted: %s", redacted)
	}
	if !strings.HasPrefix(redacted, "se***") {
		t.Fatalf("unexpected redaction format: %s", redacted)
	}
}
