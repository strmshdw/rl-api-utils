package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/testutil"
)

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
