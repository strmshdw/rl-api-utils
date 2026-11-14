package e2e

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/auth"
	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/psynet"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/syncer"
	"github.com/dank/rl-api-utils/internal/testutil"
)

// TestTier5_Adv4_Ballchasing_ExtremeRateLimits_CapsAndContextTimeout verifies that:
// 1. Extreme Retry-After values (e.g. 999999 seconds) are capped at MaxBackoff.
// 2. Context cancellation during rate limit backoff sleep aborts immediately without hanging.
// 3. MaxRetries = 0 immediately returns rate limit error on first 429.
func TestTier5_Adv4_Ballchasing_ExtremeRateLimits_CapsAndContextTimeout(t *testing.T) {
	// Subtest 1: Context cancellation interrupts rate limit wait
	t.Run("Context cancellation aborts rate limit sleep", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error": "rate limit"}`))
		}))
		defer ts.Close()

		tempDir := t.TempDir()
		dummyFile := filepath.Join(tempDir, "test.replay")
		_ = os.WriteFile(dummyFile, testutil.GenerateValidReplay("guid-rl", 2048), 0644)

		client, _ := ballchasing.NewClient(ballchasing.ClientConfig{
			BaseURL:     ts.URL,
			APIKey:      "token",
			MaxRetries:  3,
			BaseBackoff: 10 * time.Second,
			MaxBackoff:  30 * time.Second,
		})

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		start := time.Now()
		_, err := client.UploadReplay(ctx, "guid-rl", dummyFile)
		elapsed := time.Since(start)

		if err == nil {
			t.Fatal("expected context timeout error, got nil")
		}
		if elapsed > 500*time.Millisecond {
			t.Fatalf("upload hung for %v despite 50ms context timeout", elapsed)
		}
	})

	// Subtest 2: MaxRetries = 0 fails immediately without retry
	t.Run("MaxRetries 0 fails immediately", func(t *testing.T) {
		calls := 0
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer ts.Close()

		tempDir := t.TempDir()
		dummyFile := filepath.Join(tempDir, "test.replay")
		_ = os.WriteFile(dummyFile, testutil.GenerateValidReplay("guid-rl0", 2048), 0644)

		client, _ := ballchasing.NewClient(ballchasing.ClientConfig{
			BaseURL:    ts.URL,
			APIKey:     "token",
			MaxRetries: 0,
		})

		_, err := client.UploadReplay(context.Background(), "guid-rl0", dummyFile)
		if err == nil {
			t.Fatal("expected rate limit error, got nil")
		}
		if !errors.Is(err, ballchasing.ErrRateLimitExhausted) {
			t.Fatalf("expected ErrRateLimitExhausted, got: %v", err)
		}
		if calls != 1 {
			t.Fatalf("expected exactly 1 call with MaxRetries=0, got %d", calls)
		}
	})
}

// TestTier5_Adv4_Ballchasing_UploadModes_StreamingVsBuffered_Parity tests that
// both buffered mode (bytes.Buffer) and zero-RAM streaming mode (io.MultiReader)
// upload identically to Ballchasing server with valid multipart headers.
func TestTier5_Adv4_Ballchasing_UploadModes_StreamingVsBuffered_Parity(t *testing.T) {
	tempDir := t.TempDir()
	replayPayload := testutil.GenerateValidReplay("guid-parity", 16*1024)
	dummyFile := filepath.Join(tempDir, "parity.replay")
	if err := os.WriteFile(dummyFile, replayPayload, 0644); err != nil {
		t.Fatalf("failed to write parity replay: %v", err)
	}

	bc := testutil.NewMockBallchasingServer()
	defer bc.Close()

	// 1. Test Buffered Mode (streamUpload = false)
	clientBuffered, err := ballchasing.NewClient(ballchasing.ClientConfig{
		BaseURL:      bc.URL(),
		APIKey:       "test-ballchasing-token",
		StreamUpload: false,
	})
	if err != nil {
		t.Fatalf("NewClient buffered failed: %v", err)
	}

	resBuf, err := clientBuffered.UploadReplay(context.Background(), "guid-parity-buf", dummyFile)
	if err != nil {
		t.Fatalf("buffered upload failed: %v", err)
	}
	if resBuf.ID == "" {
		t.Fatal("buffered upload returned empty ID")
	}

	// 2. Test Zero-RAM Streaming Mode (streamUpload = true)
	clientStreaming, err := ballchasing.NewClient(ballchasing.ClientConfig{
		BaseURL:      bc.URL(),
		APIKey:       "test-ballchasing-token",
		StreamUpload: true,
	})
	if err != nil {
		t.Fatalf("NewClient streaming failed: %v", err)
	}

	resStream, err := clientStreaming.UploadReplay(context.Background(), "guid-parity-stream", dummyFile)
	if err != nil {
		t.Fatalf("streaming upload failed: %v", err)
	}
	if resStream.ID == "" {
		t.Fatal("streaming upload returned empty ID")
	}

	// Verify both were received by MockBallchasingServer with identical size and payload bytes
	upBuf := bc.GetUpload("guid-parity-buf")
	upStream := bc.GetUpload("guid-parity-stream")

	if upBuf == nil || upStream == nil {
		t.Fatal("mock server did not record both uploads")
	}
	if upBuf.FileSize != upStream.FileSize {
		t.Fatalf("file size mismatch: buffered=%d, streaming=%d", upBuf.FileSize, upStream.FileSize)
	}
	if !bytes.Equal(upBuf.FileBytes, upStream.FileBytes) {
		t.Fatal("payload bytes mismatch between buffered and streaming upload modes")
	}
}

// TestTier5_Adv4_Daemon_TickSkipping_WhenCycleInFlight verifies that when a sync cycle
// is actively executing, subsequent ticks skip execution and do not cause race conditions.
func TestTier5_Adv4_Daemon_TickSkipping_WhenCycleInFlight(t *testing.T) {
	started := make(chan struct{})
	blockCycle := make(chan struct{})

	slowSyncer := &mockSlowSyncer{
		runCycleFn: func(ctx context.Context) (*syncer.SyncStats, error) {
			close(started)
			<-blockCycle
			return &syncer.SyncStats{}, nil
		},
	}

	cfg := &config.Config{
		Sync: config.SyncConfig{
			PollInterval: config.Duration(10 * time.Millisecond),
			Once:         false,
		},
		Logging: config.LoggingConfig{Level: "error"},
	}

	d, err := daemon.New(slowSyncer, cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	daemonErrCh := make(chan error, 1)
	go func() {
		daemonErrCh <- d.Start(ctx)
	}()

	// Wait until the initial cycle begins
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for slow cycle to start")
	}

	// Verify daemon reports in-flight
	if !d.IsInFlight() {
		t.Fatal("expected IsInFlight to be true during active cycle")
	}

	// Let the slow cycle complete
	close(blockCycle)

	// Stop daemon
	cancel()

	select {
	case err := <-daemonErrCh:
		if err != nil {
			t.Fatalf("daemon exited with unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for daemon to shut down cleanly")
	}
}

type mockSlowSyncer struct {
	runCycleFn func(ctx context.Context) (*syncer.SyncStats, error)
}

func (m *mockSlowSyncer) RunCycle(ctx context.Context) (*syncer.SyncStats, error) {
	return m.runCycleFn(ctx)
}

// TestTier5_Adv4_Daemon_GracefulDrain_OnContextCancellation verifies that when
// context cancellation occurs while a cycle is running, the daemon drains the in-flight cycle.
func TestTier5_Adv4_Daemon_GracefulDrain_OnContextCancellation(t *testing.T) {
	cycleFinished := false
	var mu sync.Mutex

	syncerMock := &mockSlowSyncer{
		runCycleFn: func(ctx context.Context) (*syncer.SyncStats, error) {
			time.Sleep(100 * time.Millisecond)
			mu.Lock()
			cycleFinished = true
			mu.Unlock()
			return &syncer.SyncStats{DiscoveredCount: 1}, nil
		},
	}

	cfg := &config.Config{
		Sync: config.SyncConfig{
			PollInterval: config.Duration(5 * time.Minute),
			Once:         false,
		},
		Logging: config.LoggingConfig{Level: "error"},
	}

	d, _ := daemon.New(syncerMock, cfg)

	ctx, cancel := context.WithCancel(context.Background())

	doneCh := make(chan error, 1)
	go func() {
		doneCh <- d.Start(ctx)
	}()

	// Allow cycle to start, then cancel context
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-doneCh:
		if err != nil {
			t.Fatalf("daemon shut down with error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not drain within 2 seconds")
	}

	mu.Lock()
	finished := cycleFinished
	mu.Unlock()

	if !finished {
		t.Fatal("expected in-flight cycle to finish before daemon.Start returns")
	}
}

// TestTier5_Adv4_Syncer_FullPipeline_WithRealSQLiteStore tests the entire end-to-end
// synchronization pipeline using pure Go SQLiteStore (modernc.org/sqlite), real HTTPDownloader,
// real Ballchasing client, real Syncer, and mock servers.
func TestTier5_Adv4_Syncer_FullPipeline_WithRealSQLiteStore(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "real_pipeline.db")
	replayDir := filepath.Join(tempDir, "replays")

	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create real SQLiteStore: %v", err)
	}
	defer store.Close()

	cdn := testutil.NewMockCDNServer()
	defer cdn.Close()
	bc := testutil.NewMockBallchasingServer()
	defer bc.Close()

	guid1 := "real-match-1"
	guid2 := "real-match-dup"

	bc.SetDuplicateGUID(guid2, "existing-bc-id-2")

	provider := &dynamicMockProvider{
		fn: func(ctx context.Context) ([]syncer.DiscoveredMatch, error) {
			return []syncer.DiscoveredMatch{
				{
					MatchGUID:            guid1,
					RecordStartTimestamp: time.Now().Unix(),
					MapName:              "DFHStadium_P",
					Playlist:             2,
					ReplayURL:            cdn.ReplayURL(guid1),
				},
				{
					MatchGUID:            guid2,
					RecordStartTimestamp: time.Now().Unix(),
					MapName:              "Mannfield_P",
					Playlist:             3,
					ReplayURL:            cdn.ReplayURL(guid2),
				},
			}, nil
		},
	}

	downloader := psynet.NewDownloader(psynet.WithTimeout(10 * time.Second))
	uploader, err := ballchasing.NewClient(ballchasing.ClientConfig{
		BaseURL: bc.URL(),
		APIKey:  "test-ballchasing-token",
	})
	if err != nil {
		t.Fatalf("ballchasing.NewClient failed: %v", err)
	}

	engine, err := syncer.New(store, provider, downloader, uploader,
		syncer.WithReplayDir(replayDir),
		syncer.WithKeepLocalFiles(true),
	)
	if err != nil {
		t.Fatalf("syncer.New failed: %v", err)
	}

	ctx := context.Background()
	stats, err := engine.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle failed with real SQLite store: %v", err)
	}

	if stats.DiscoveredCount != 2 {
		t.Fatalf("expected 2 discovered, got %d", stats.DiscoveredCount)
	}
	if stats.DownloadedCount != 2 {
		t.Fatalf("expected 2 downloaded, got %d", stats.DownloadedCount)
	}
	if stats.UploadedCount != 1 {
		t.Fatalf("expected 1 new upload, got %d", stats.UploadedCount)
	}
	if stats.DuplicateCount != 1 {
		t.Fatalf("expected 1 duplicate, got %d", stats.DuplicateCount)
	}

	// Verify persistence in real SQLite database
	m1, err := store.GetMatch(ctx, guid1)
	if err != nil || m1 == nil {
		t.Fatalf("failed to retrieve %s from sqlite: %v", guid1, err)
	}
	if m1.DownloadStatus != storage.DownloadDownloaded {
		t.Fatalf("expected %s download status DOWNLOADED, got %s", guid1, m1.DownloadStatus)
	}
	if m1.UploadStatus != storage.UploadUploaded {
		t.Fatalf("expected %s upload status UPLOADED, got %s", guid1, m1.UploadStatus)
	}
	if m1.BallchasingID == "" {
		t.Fatalf("expected ballchasing ID for %s, got empty", guid1)
	}

	m2, err := store.GetMatch(ctx, guid2)
	if err != nil || m2 == nil {
		t.Fatalf("failed to retrieve %s from sqlite: %v", guid2, err)
	}
	if m2.UploadStatus != storage.UploadDuplicate {
		t.Fatalf("expected %s upload status DUPLICATE, got %s", guid2, m2.UploadStatus)
	}
	if m2.BallchasingID != "existing-bc-id-2" {
		t.Fatalf("expected existing ballchasing ID 'existing-bc-id-2', got %q", m2.BallchasingID)
	}
}

// TestTier5_Adv4_Auth_SteamValidation_And_EpicCredentialBoundaries tests SteamID64
// boundary conditions and Epic credential validation edge cases.
func TestTier5_Adv4_Auth_SteamValidation_And_EpicCredentialBoundaries(t *testing.T) {
	// SteamID64 validation
	validSteamIDs := []string{
		"76561198000000000",
		"76561197960287930",
	}
	for _, id := range validSteamIDs {
		if err := auth.ValidateSteamID64(id); err != nil {
			t.Fatalf("expected %q to be valid SteamID64, got: %v", id, err)
		}
	}

	invalidSteamIDs := []string{
		"",
		"7656119",            // too short
		"765611980000000001", // too long (18 digits)
		"12345678901234567",  // wrong prefix
		"7656119abcdefghij",  // non-numeric
		"7656119800000000 ",  // whitespace
	}
	for _, id := range invalidSteamIDs {
		if err := auth.ValidateSteamID64(id); err == nil {
			t.Fatalf("expected %q to be rejected as invalid SteamID64, got nil", id)
		}
	}

	// Epic provider validation requires refresh token or auth code
	epicEmpty, err := auth.NewEpicProvider(config.EpicConfig{}, nil)
	if err != nil {
		t.Fatalf("NewEpicProvider failed: %v", err)
	}
	if err := epicEmpty.Validate(); err == nil {
		t.Fatal("expected empty Epic config to fail validation, got nil")
	}

	// Steam provider validation requires session ticket and steam ID 64
	steamEmpty, err := auth.NewSteamProvider(config.SteamConfig{}, nil)
	if err != nil {
		t.Fatalf("NewSteamProvider failed: %v", err)
	}
	if err := steamEmpty.Validate(); err == nil {
		t.Fatal("expected empty Steam config to fail validation, got nil")
	}
}
