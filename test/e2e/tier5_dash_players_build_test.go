package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/storage"
)

// ============================================================================
// Scenario 3: Player Search Directory Under Continuous Telemetry Ingestion
// ============================================================================

// TestTier5_Dashboard_PlayerSearchUnderContinuousIngestion tests concurrent search
// traffic against GET /api/players while continuous player and match results are written,
// validating lock-free concurrency, SQLite vs JSONStore parity, and zero 500 errors.
func TestTier5_Dashboard_PlayerSearchUnderContinuousIngestion(t *testing.T) {
	tempDir := t.TempDir()
	sqlitePath := filepath.Join(tempDir, "state_search.db")
	jsonPath := filepath.Join(tempDir, "state_search.json")

	sqliteStore, err := storage.NewSQLiteStore(sqlitePath)
	if err != nil {
		t.Fatalf("failed to init SQLiteStore: %v", err)
	}
	defer sqliteStore.Close()

	jsonStore, err := storage.NewJSONStore(jsonPath)
	if err != nil {
		t.Fatalf("failed to init JSONStore: %v", err)
	}
	defer jsonStore.Close()

	ctx := context.Background()

	baseTime := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)

	// Pre-populate both stores with initial 50 players (25 Steam, 25 Epic)
	// Each player receives a distinct discrete second-aligned timestamp
	for i := 1; i <= 25; i++ {
		ts := baseTime.Add(time.Duration(100-i) * time.Minute)
		pSteam := &storage.PlayerRecord{
			PlayerID:    fmt.Sprintf("Steam|765611980000000%02d|0", i),
			Platform:    "Steam",
			PlayerName:  fmt.Sprintf("SteamPlayer_%02d", i),
			FirstSeenAt: ts,
			LastSeenAt:  ts,
		}
		mSteam := storage.PlayerOutcome{
			PlayerID:   pSteam.PlayerID,
			Platform:   "Steam",
			PlayerName: pSteam.PlayerName,
			IsTeammate: true,
			Won:        true,
		}
		_ = sqliteStore.RecordMatchResults(ctx, fmt.Sprintf("init-guid-steam-%d", i), 11, []storage.PlayerOutcome{mSteam}, ts)
		_ = jsonStore.RecordMatchResults(ctx, fmt.Sprintf("init-guid-steam-%d", i), 11, []storage.PlayerOutcome{mSteam}, ts)

		_ = sqliteStore.UpsertPlayer(ctx, pSteam)
		_ = jsonStore.UpsertPlayer(ctx, pSteam)
	}

	for i := 1; i <= 25; i++ {
		ts := baseTime.Add(time.Duration(50-i) * time.Minute)
		pEpic := &storage.PlayerRecord{
			PlayerID:    fmt.Sprintf("Epic|epic_account_%02d|0", i),
			Platform:    "Epic",
			PlayerName:  fmt.Sprintf("EpicPlayer_%02d", i),
			FirstSeenAt: ts,
			LastSeenAt:  ts,
		}
		mEpic := storage.PlayerOutcome{
			PlayerID:   pEpic.PlayerID,
			Platform:   "Epic",
			PlayerName: pEpic.PlayerName,
			IsTeammate: false,
			Won:        false,
		}
		_ = sqliteStore.RecordMatchResults(ctx, fmt.Sprintf("init-guid-epic-%d", i), 11, []storage.PlayerOutcome{mEpic}, ts)
		_ = jsonStore.RecordMatchResults(ctx, fmt.Sprintf("init-guid-epic-%d", i), 11, []storage.PlayerOutcome{mEpic}, ts)

		_ = sqliteStore.UpsertPlayer(ctx, pEpic)
		_ = jsonStore.UpsertPlayer(ctx, pEpic)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		Web: config.WebConfig{Enabled: true, Host: "127.0.0.1", Port: 0},
		PlayerTracking: config.PlayerTrackingConfig{
			Enabled:       true,
			LocalPlayerID: "Steam|76561198000000001|0",
		},
	}

	d, err := daemon.New(&testSyncerStub{}, cfg,
		daemon.WithStateStore(sqliteStore),
		daemon.WithLogger(logger),
	)
	if err != nil {
		t.Fatalf("failed to create daemon: %v", err)
	}

	server := httptest.NewServer(d.Handler(ctx))
	defer server.Close()

	// Ingestion loop: continuous inserts and updates across both stores
	var ingestWg sync.WaitGroup
	ingestWg.Add(1)

	go func() {
		defer ingestWg.Done()
		for idx := 51; idx <= 100; idx++ {
			ts := baseTime.Add(time.Duration(200+idx) * time.Minute)
			p := &storage.PlayerRecord{
				PlayerID:    fmt.Sprintf("Steam|76561198000000%03d|0", idx),
				Platform:    "Steam",
				PlayerName:  fmt.Sprintf("LivePlayer_%03d", idx),
				FirstSeenAt: ts,
				LastSeenAt:  ts,
			}
			m := storage.PlayerOutcome{
				PlayerID:   p.PlayerID,
				Platform:   "Steam",
				PlayerName: p.PlayerName,
				IsTeammate: true,
				Won:        true,
			}
			_ = sqliteStore.RecordMatchResults(ctx, fmt.Sprintf("live-guid-%d", idx), 11, []storage.PlayerOutcome{m}, ts)
			_ = jsonStore.RecordMatchResults(ctx, fmt.Sprintf("live-guid-%d", idx), 11, []storage.PlayerOutcome{m}, ts)

			_ = sqliteStore.UpsertPlayer(ctx, p)
			_ = jsonStore.UpsertPlayer(ctx, p)

			time.Sleep(15 * time.Millisecond)
		}
	}()

	// Concurrently hammer GET /api/players with 10 workers running varied search queries
	const searchWorkers = 10
	var searchWg sync.WaitGroup
	searchWg.Add(searchWorkers)
	var totalQueries int64

	searchEndpoints := []string{
		"/api/players?query=SteamPlayer&limit=10&offset=0",
		"/api/players?platform=Steam&limit=10&offset=0",
		"/api/players?platform=Epic&limit=10&offset=0",
		"/api/players?query=Epic&platform=Epic&limit=5&offset=5",
		"/api/players?query=%25&limit=10",
		"/api/players?query=_&limit=10",
		"/api/players?query=nonexistent_xyz&limit=10",
		"/api/players?limit=10&offset=200",
		"/api/players?limit=5&offset=0",
	}

	for w := 0; w < searchWorkers; w++ {
		go func(workerID int) {
			defer searchWg.Done()
			client := &http.Client{Timeout: 15 * time.Second}

			for q := 0; q < 25; q++ {
				path := searchEndpoints[(workerID+q)%len(searchEndpoints)]
				resp, err := client.Get(server.URL + path)
				if err != nil {
					t.Errorf("worker %d query %q failed: %v", workerID, path, err)
					return
				}

				if resp.StatusCode != http.StatusOK {
					t.Errorf("worker %d query %q returned %d", workerID, path, resp.StatusCode)
					resp.Body.Close()
					return
				}

				var result struct {
					Players []*storage.PlayerSummary `json:"players"`
					Total   int                      `json:"total"`
					Limit   int                      `json:"limit"`
					Offset  int                      `json:"offset"`
				}

				if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
					t.Errorf("worker %d query %q json decode failed: %v", workerID, path, err)
					resp.Body.Close()
					return
				}
				resp.Body.Close()

				if result.Total < len(result.Players) {
					t.Errorf("worker %d query %q: total (%d) < returned players (%d)",
						workerID, path, result.Total, len(result.Players))
				}

				atomic.AddInt64(&totalQueries, 1)
				time.Sleep(10 * time.Millisecond)
			}
		}(w)
	}

	searchWg.Wait()
	ingestWg.Wait()

	if total := atomic.LoadInt64(&totalQueries); total < 250 {
		t.Fatalf("expected at least 250 search queries, executed %d", total)
	}

	// Cross-Backend Parity Matrix Check: identical vectors on SQLiteStore vs JSONStore
	parityVectors := []struct {
		name     string
		query    string
		platform string
		limit    int
		offset   int
	}{
		{"All_Default", "", "", 10, 0},
		{"Steam_Filter", "", "Steam", 10, 0},
		{"Epic_Filter", "", "Epic", 10, 0},
		{"Substring_Player", "Player", "", 10, 0},
		{"Pagination_Offset", "", "Steam", 5, 5},
		{"Wildcard_Percent", "%", "", 10, 0},
		{"Wildcard_Underscore", "_", "", 10, 0},
		{"NonExistent", "non_existent_player_123", "", 10, 0},
	}

	for _, pv := range parityVectors {
		t.Run("Parity_"+pv.name, func(t *testing.T) {
			sRes, sTotal, sErr := sqliteStore.SearchPlayerSummaries(ctx, pv.query, pv.platform, pv.limit, pv.offset)
			if sErr != nil {
				t.Fatalf("SQLite search failed: %v", sErr)
			}
			jRes, jTotal, jErr := jsonStore.SearchPlayerSummaries(ctx, pv.query, pv.platform, pv.limit, pv.offset)
			if jErr != nil {
				t.Fatalf("JSONStore search failed: %v", jErr)
			}

			if sTotal != jTotal {
				t.Errorf("parity total mismatch: SQLite=%d, JSONStore=%d", sTotal, jTotal)
			}
			if len(sRes) != len(jRes) {
				t.Fatalf("parity count mismatch: SQLite=%d, JSONStore=%d", len(sRes), len(jRes))
			}
			for idx := range sRes {
				if sRes[idx].PlayerID != jRes[idx].PlayerID {
					t.Errorf("parity index %d PlayerID mismatch: SQLite=%s, JSONStore=%s",
						idx, sRes[idx].PlayerID, jRes[idx].PlayerID)
				}
				if sRes[idx].Platform != jRes[idx].Platform {
					t.Errorf("parity index %d Platform mismatch: SQLite=%s, JSONStore=%s",
						idx, sRes[idx].Platform, jRes[idx].Platform)
				}
			}
		})
	}
}

// ============================================================================
// Scenario 6: Standalone Binary Build and CLI Precedence Verification
// ============================================================================

// findRepoRoot finds the repository root directory containing go.mod.
func findRepoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "../.."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "../.."
}

// findGoBinary locates the Go compiler executable on the host system.
func findGoBinary() (string, error) {
	if p, err := exec.LookPath("go"); err == nil {
		return p, nil
	}
	if p, err := exec.LookPath("go.exe"); err == nil {
		return p, nil
	}
	localApp := os.Getenv("LOCALAPPDATA")
	if localApp != "" {
		p := filepath.Join(localApp, "Programs", "go", "bin", "go.exe")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	goroot := runtime.GOROOT()
	if goroot != "" {
		p := filepath.Join(goroot, "bin", "go.exe")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		p = filepath.Join(goroot, "bin", "go")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("go executable not found in PATH, LOCALAPPDATA, or GOROOT")
}

// TestTier5_Dashboard_SingleBinaryBuildAndCLIPrecedence builds the standalone rl-sync.exe
// executable from cmd/rl-sync, verifies size and embedded assets, executes --help and --version,
// and tests CLI flag precedence and boundary validations.
func TestTier5_Dashboard_SingleBinaryBuildAndCLIPrecedence(t *testing.T) {
	goBin, err := findGoBinary()
	if err != nil {
		t.Fatalf("failed to locate go binary: %v", err)
	}

	repoRoot := findRepoRoot()
	tempDir := t.TempDir()

	binName := "rl-sync-test"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(tempDir, binName)

	// Step 1: Build standalone executable from cmd/rl-sync
	buildCmd := exec.Command(goBin, "build", "-o", binPath, "./cmd/rl-sync")
	buildCmd.Dir = repoRoot
	buildCmd.Env = os.Environ()
	if localApp := os.Getenv("LOCALAPPDATA"); localApp != "" {
		goBinDir := filepath.Join(localApp, "Programs", "go", "bin")
		buildCmd.Env = append(buildCmd.Env, "PATH="+goBinDir+";"+os.Getenv("PATH"))
	}

	out, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed: %v\nOutput: %s", err, string(out))
	}

	// Step 2: Verify binary footprint (> 10MB indicates embedded React SPA and modernc SQLite)
	fi, err := os.Stat(binPath)
	if err != nil {
		t.Fatalf("failed to stat compiled binary: %v", err)
	}
	if fi.Size() < 10*1024*1024 {
		t.Errorf("compiled binary is unexpectedly small (%d bytes), expected > 10MB", fi.Size())
	}

	// Step 3: Verify --help banner and flags
	helpCmd := exec.Command(binPath, "--help")
	helpOut, err := helpCmd.CombinedOutput()
	helpStr := string(helpOut)
	if !strings.Contains(helpStr, "-web-enabled") {
		t.Errorf("--help output missing -web-enabled flag: %s", helpStr)
	}
	if !strings.Contains(helpStr, "-web-host") {
		t.Errorf("--help output missing -web-host flag: %s", helpStr)
	}
	if !strings.Contains(helpStr, "-web-port") {
		t.Errorf("--help output missing -web-port flag: %s", helpStr)
	}

	// Step 4: Verify --version banner
	versionCmd := exec.Command(binPath, "--version")
	verOut, err := versionCmd.CombinedOutput()
	if err != nil {
		t.Errorf("--version command failed: %v", err)
	}
	if !strings.Contains(string(verOut), "rl-sync") {
		t.Errorf("--version output missing 'rl-sync': %s", string(verOut))
	}

	// Step 5: Test CLI Flag Validation Boundaries (Invalid Ports)
	invalidPortVectors := []string{"99999", "-1", "invalid", "0"}
	for _, port := range invalidPortVectors {
		cmd := exec.Command(binPath, "--web-port="+port, "--once")
		cmd.Dir = repoRoot
		err := cmd.Run()
		if err == nil {
			t.Errorf("expected --web-port=%s to fail with non-zero exit code", port)
		}
	}

	// Step 6: Test CLI Flag Precedence (Disabling web via CLI flag)
	confPath := filepath.Join(tempDir, "config.yaml")
	confContent := `
ballchasing:
  api_key: "test-token"
auth:
  provider: "steam"
  steam:
    steam_id_64: "76561198000000001"
    session_ticket: "test-session-ticket"
web:
  enabled: true
  port: 49125
`
	if err := os.WriteFile(confPath, []byte(confContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// Test CLI flag --web-enabled=false overrides RL_SYNC_WEB_ENABLED=true
	validCmd := exec.Command(binPath, "--config="+confPath, "--web-enabled=false", "--once", "--dry-run")
	validCmd.Dir = repoRoot
	validCmd.Env = append(os.Environ(), "RL_SYNC_WEB_ENABLED=true")
	validOut, _ := validCmd.CombinedOutput()
	outStr := string(validOut)
	if !strings.Contains(outStr, "web_enabled=false") {
		t.Errorf("expected output to reflect CLI flag web_enabled=false, got: %s", outStr)
	}

	// Test CLI flag --web-port=54321 overrides RL_SYNC_WEB_PORT=49125
	portCmd := exec.Command(binPath, "--config="+confPath, "--web-port=54321", "--once", "--dry-run")
	portCmd.Dir = repoRoot
	portCmd.Env = append(os.Environ(), "RL_SYNC_WEB_PORT=49125")
	portOut, _ := portCmd.CombinedOutput()
	portStr := string(portOut)
	if !strings.Contains(portStr, "web_port=54321") {
		t.Errorf("expected output to reflect CLI flag web_port=54321, got: %s", portStr)
	}

	// Test CLI flag --web-host=127.0.0.2 overrides RL_SYNC_WEB_HOST=127.0.0.1
	hostCmd := exec.Command(binPath, "--config="+confPath, "--web-host=127.0.0.2", "--once", "--dry-run")
	hostCmd.Dir = repoRoot
	hostCmd.Env = append(os.Environ(), "RL_SYNC_WEB_HOST=127.0.0.1")
	hostOut, _ := hostCmd.CombinedOutput()
	hostStr := string(hostOut)
	if !strings.Contains(hostStr, "web_host=127.0.0.2") {
		t.Errorf("expected output to reflect CLI flag web_host=127.0.0.2, got: %s", hostStr)
	}
}
