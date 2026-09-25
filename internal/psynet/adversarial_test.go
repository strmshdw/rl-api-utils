package psynet

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/testutil"
	"github.com/dank/rlapi"
)

// ============================================================================
// 1. REPLAY DOWNLOADER ADVERSARIAL STRESS SUITE
// ============================================================================

// TestAdversarial_Downloader_ExtremePayloads tests boundary conditions on payload sizes:
// 0-byte, 1-byte, 500-byte, 1023-byte (rejected <1024 bytes),
// 1024-byte boundary (accepted), 1025-byte, 1MB, 5MB valid payloads.
func TestAdversarial_Downloader_ExtremePayloads(t *testing.T) {
	cdn := testutil.NewMockCDNServer()
	defer cdn.Close()

	destDir := t.TempDir()
	downloader := NewDownloader(WithTimeout(15 * time.Second))

	// Sub-test 1: Under threshold (<1024 bytes) must be rejected with ErrReplayTooSmall
	rejectSizes := []struct {
		name string
		size int
	}{
		{"0-byte empty payload", 0},
		{"1-byte single byte", 1},
		{"128-byte chunk", 128},
		{"500-byte half-threshold", 500},
		{"1023-byte just-under-threshold", 1023},
	}

	for _, tc := range rejectSizes {
		t.Run("Reject_"+tc.name, func(t *testing.T) {
			guid := fmt.Sprintf("adv-size-%d", tc.size)
			cdn.SetReplayPayload(guid, make([]byte, tc.size))

			_, err := downloader.DownloadReplay(context.Background(), guid, cdn.ReplayURL(guid), destDir)
			if err == nil {
				t.Fatalf("[%s] expected ErrReplayTooSmall, got nil error", tc.name)
			}
			if !errors.Is(err, ErrReplayTooSmall) {
				t.Fatalf("[%s] expected error wrapping ErrReplayTooSmall, got: %v", tc.name, err)
			}

			// Invariant: zero files on disk (neither .tmp nor .replay)
			entries, readErr := os.ReadDir(destDir)
			if readErr != nil {
				t.Fatalf("[%s] failed reading destDir: %v", tc.name, readErr)
			}
			for _, e := range entries {
				if strings.Contains(e.Name(), guid) {
					t.Fatalf("[%s] orphaned file left on disk after rejection: %s", tc.name, e.Name())
				}
			}
		})
	}

	// Sub-test 2: Accept threshold (>=1024 bytes)
	acceptSizes := []struct {
		name        string
		size        int
		validHeader bool
	}{
		{"1024-byte exact threshold", 1024, true},
		{"1025-byte just-over-threshold", 1025, true},
		{"64KB medium replay", 64 * 1024, true},
		{"1MB large replay", 1024 * 1024, true},
		{"5MB extreme overtime replay", 5 * 1024 * 1024, true},
	}

	for _, tc := range acceptSizes {
		t.Run("Accept_"+tc.name, func(t *testing.T) {
			guid := fmt.Sprintf("adv-accept-%d", tc.size)
			payload := testutil.GenerateValidReplay(guid, tc.size)
			cdn.SetReplayPayload(guid, payload)

			path, err := downloader.DownloadReplay(context.Background(), guid, cdn.ReplayURL(guid), destDir)
			if err != nil {
				t.Fatalf("[%s] expected success, got error: %v", tc.name, err)
			}

			expectedPath := filepath.Join(destDir, fmt.Sprintf("%s.replay", guid))
			if path != expectedPath {
				t.Fatalf("[%s] expected path %s, got %s", tc.name, expectedPath, path)
			}

			info, statErr := os.Stat(path)
			if statErr != nil {
				t.Fatalf("[%s] downloaded file missing from disk: %v", tc.name, statErr)
			}
			if info.Size() != int64(tc.size) {
				t.Fatalf("[%s] expected size %d, got %d", tc.name, tc.size, info.Size())
			}

			// Validate TAGAME header
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("[%s] failed reading downloaded file: %v", tc.name, readErr)
			}
			if !bytes.HasPrefix(data, testutil.ReplayMagicBytes) {
				t.Fatalf("[%s] replay missing TAGAME magic bytes prefix", tc.name)
			}

			// Verify no temporary files remain
			entries, _ := os.ReadDir(destDir)
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), TempFilePrefix) {
					t.Fatalf("[%s] temporary file remained after success: %s", tc.name, e.Name())
				}
			}
		})
	}
}

// TestAdversarial_Downloader_HTTPStatusCodes verifies that all non-200 HTTP status codes
// return a typed HTTPStatusError and leave zero disk footprint.
func TestAdversarial_Downloader_HTTPStatusCodes(t *testing.T) {
	cdn := testutil.NewMockCDNServer()
	defer cdn.Close()

	destDir := t.TempDir()
	downloader := NewDownloader()

	statusCodes := []struct {
		code int
		desc string
	}{
		{http.StatusBadRequest, "400 Bad Request"},
		{http.StatusUnauthorized, "401 Unauthorized"},
		{http.StatusForbidden, "403 Forbidden (Expired signed URL)"},
		{http.StatusNotFound, "404 Not Found (Deleted replay)"},
		{http.StatusGone, "410 Gone (Purged replay)"},
		{http.StatusTooManyRequests, "429 Too Many Requests (Rate limited)"},
		{http.StatusInternalServerError, "500 Internal Server Error (CDN crash)"},
		{http.StatusBadGateway, "502 Bad Gateway"},
		{http.StatusServiceUnavailable, "503 Service Unavailable (CDN maintenance)"},
		{http.StatusGatewayTimeout, "504 Gateway Timeout"},
	}

	for _, sc := range statusCodes {
		t.Run(sc.desc, func(t *testing.T) {
			guid := fmt.Sprintf("status-%d", sc.code)
			cdn.SetStatusCode(guid, sc.code)

			_, err := downloader.DownloadReplay(context.Background(), guid, cdn.ReplayURL(guid), destDir)
			if err == nil {
				t.Fatalf("expected error for HTTP %d, got nil", sc.code)
			}

			var httpErr *HTTPStatusError
			if !errors.As(err, &httpErr) {
				t.Fatalf("expected error to wrap *HTTPStatusError, got %T: %v", err, err)
			}
			if httpErr.StatusCode != sc.code {
				t.Fatalf("expected status code %d, got %d", sc.code, httpErr.StatusCode)
			}
			if httpErr.MatchGUID != guid {
				t.Fatalf("expected match GUID %s in HTTPStatusError, got %s", guid, httpErr.MatchGUID)
			}

			// Invariant: zero files created on disk
			entries, _ := os.ReadDir(destDir)
			for _, e := range entries {
				if strings.Contains(e.Name(), guid) {
					t.Fatalf("unexpected file on disk after HTTP %d: %s", sc.code, e.Name())
				}
			}
		})
	}
}

// TestAdversarial_Downloader_MidStreamDropsAndInterrupts tests network drops at various stream positions:
// 1. Truncation at 64 bytes (before minSize)
// 2. Truncation after 4KB (mid-stream drop after threshold)
// 3. Server hang / timeout during body stream
func TestAdversarial_Downloader_MidStreamDropsAndInterrupts(t *testing.T) {
	destDir := t.TempDir()

	// Scenario 1: Mock CDN with stream truncation
	t.Run("CDN_TruncatedStream_NoOrphan", func(t *testing.T) {
		cdn := testutil.NewMockCDNServer()
		defer cdn.Close()

		guid := "adv-drop-cdn"
		cdn.SetTruncateStream(guid, true)

		downloader := NewDownloader(WithTimeout(5 * time.Second))
		_, err := downloader.DownloadReplay(context.Background(), guid, cdn.ReplayURL(guid), destDir)
		if err == nil {
			t.Fatal("expected error on truncated stream, got nil")
		}

		// Verify zero temporary files remaining
		entries, _ := os.ReadDir(destDir)
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), TempFilePrefix) || strings.Contains(e.Name(), guid) {
				t.Fatalf("orphaned temp file found after stream truncation: %s", e.Name())
			}
		}
	})

	// Scenario 2: Custom TCP server dropping connection after 4KB (over 1024 bytes, but EOF before Content-Length)
	t.Run("TCP_DropAfter4KB_ContentLengthMismatch", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Advertise 100KB content length
			w.Header().Set("Content-Length", "102400")
			w.WriteHeader(http.StatusOK)

			// Write 4KB valid TAGAME replay bytes
			chunk := testutil.GenerateValidReplay("guid-drop-4k", 4096)
			_, _ = w.Write(chunk)

			// Abruptly hijack and close TCP connection
			if hj, ok := w.(http.Hijacker); ok {
				conn, _, _ := hj.Hijack()
				if conn != nil {
					_ = conn.Close()
				}
			}
		}))
		defer ts.Close()

		downloader := NewDownloader(WithTimeout(5 * time.Second))
		guid := "guid-drop-4k"
		_, err := downloader.DownloadReplay(context.Background(), guid, ts.URL+"/drop", destDir)
		if err == nil {
			t.Fatal("expected error when connection closes before Content-Length satisfied, got nil")
		}

		// Verify cleanup
		entries, _ := os.ReadDir(destDir)
		for _, e := range entries {
			if strings.Contains(e.Name(), guid) {
				t.Fatalf("orphaned file on disk after mid-stream drop: %s", e.Name())
			}
		}
	})

	// Scenario 3: Server hangs indefinitely; client context timeout fires
	t.Run("ServerHangs_ContextTimeout_CleanedUp", func(t *testing.T) {
		hangingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			// Flush headers then sleep indefinitely
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(3 * time.Second)
		}))
		defer hangingServer.Close()

		ctxTimeout, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		downloader := NewDownloader(WithTimeout(10 * time.Second))
		guid := "guid-hanging-timeout"
		_, err := downloader.DownloadReplay(ctxTimeout, guid, hangingServer.URL+"/hang", destDir)
		if err == nil {
			t.Fatal("expected timeout error, got nil")
		}

		// Verify no files on disk
		entries, _ := os.ReadDir(destDir)
		for _, e := range entries {
			if strings.Contains(e.Name(), guid) {
				t.Fatalf("orphaned file left after context timeout: %s", e.Name())
			}
		}
	})
}

// TestAdversarial_Downloader_PathTraversal_ComprehensiveMatrix tests that all forms
// of directory traversal, absolute paths, drive letters, and invalid characters are rejected.
func TestAdversarial_Downloader_PathTraversal_ComprehensiveMatrix(t *testing.T) {
	destDir := t.TempDir()
	downloader := NewDownloader()
	validURL := "http://example.com/replay.replay"

	traversalGUIDs := []struct {
		name string
		guid string
	}{
		{"dot-dot slash", "../sneaky"},
		{"dot-dot backslash", `..\sneaky`},
		{"deep posix traversal", "../../../../etc/passwd"},
		{"deep windows traversal", `..\..\..\windows\system32\calc`},
		{"embedded posix traversal", "abc/../def"},
		{"embedded windows traversal", `abc\..\def`},
		{"absolute posix path", "/tmp/owned"},
		{"absolute windows path", `C:\Windows\System32\malicious`},
		{"drive letter relative", `D:file`},
		{"windows volume separator", "C:test"},
		{"wildcard asterisk", "match*guid"},
		{"wildcard question", "match?guid"},
		{"angle brackets", "match<1>"},
		{"pipe symbol", "match|1"},
		{"quote symbol", `match"1`},
		{"colon symbol", "match:1"},
		{"exact double dot", ".."},
		{"triple dot", "..."},
		{"quadruple dot", "...."},
		{"empty string", ""},
		{"spaces only", "     "},
		{"tabs and newlines", "\t\r\n"},
	}

	for _, tc := range traversalGUIDs {
		t.Run(tc.name, func(t *testing.T) {
			_, err := downloader.DownloadReplay(context.Background(), tc.guid, validURL, destDir)
			if err == nil {
				t.Fatalf("[%s] expected path traversal error for guid %q, got nil", tc.name, tc.guid)
			}
			if !errors.Is(err, ErrInvalidMatchGUID) && !errors.Is(err, ErrEmptyMatchGUID) {
				t.Fatalf("[%s] expected ErrInvalidMatchGUID or ErrEmptyMatchGUID, got: %v", tc.name, err)
			}
		})
	}
}

// TestAdversarial_Downloader_ConcurrencyAndContention stresses the downloader under:
// 1. High concurrent downloads of distinct GUIDs (30 goroutines)
// 2. Controlled concurrent downloads of identical GUID (5 goroutines with backoff)
// 3. Stampede contention invariants (file validity and temp cleanup)
func TestAdversarial_Downloader_ConcurrencyAndContention(t *testing.T) {
	cdn := testutil.NewMockCDNServer()
	defer cdn.Close()

	destDir := t.TempDir()
	downloader := NewDownloader(WithTimeout(10 * time.Second))

	// Part 1: 30 concurrent distinct GUID downloads into the same destDir
	t.Run("ConcurrentDistinctGUIDs_30Workers", func(t *testing.T) {
		const numWorkers = 30
		var wg sync.WaitGroup
		errCh := make(chan error, numWorkers)

		for i := 0; i < numWorkers; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				guid := fmt.Sprintf("distinct-concur-%03d", idx)
				path, err := downloader.DownloadReplay(context.Background(), guid, cdn.ReplayURL(guid), destDir)
				if err != nil {
					errCh <- fmt.Errorf("worker %d failed: %w", idx, err)
					return
				}
				if !strings.HasSuffix(path, ".replay") {
					errCh <- fmt.Errorf("worker %d unexpected path: %s", idx, path)
				}
			}(i)
		}

		wg.Wait()
		close(errCh)

		for err := range errCh {
			t.Fatalf("concurrent download error: %v", err)
		}

		// Verify all 30 files exist and 0 temp files remain
		entries, err := os.ReadDir(destDir)
		if err != nil {
			t.Fatalf("failed reading destDir: %v", err)
		}
		if len(entries) != numWorkers {
			t.Fatalf("expected %d files on disk, found %d", numWorkers, len(entries))
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), TempFilePrefix) {
				t.Fatalf("stray temp file found: %s", e.Name())
			}
		}
	})

	// Part 2: Concurrent downloads of the SAME GUID (controlled concurrency with retries)
	t.Run("ConcurrentSameGUID_ControlledContention", func(t *testing.T) {
		controlledDir := t.TempDir()
		sameGUID := "controlled-identical-guid"
		controlledDownloader := NewDownloader(WithRenameRetries(15))
		const numWorkers = 5

		var wg sync.WaitGroup
		errCh := make(chan error, numWorkers)

		for i := 0; i < numWorkers; i++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				path, err := controlledDownloader.DownloadReplay(context.Background(), sameGUID, cdn.ReplayURL(sameGUID), controlledDir)
				if err != nil {
					// On Windows, if extreme contention causes retry exhaustion, record it
					errCh <- err
					return
				}
				expected := filepath.Join(controlledDir, fmt.Sprintf("%s.replay", sameGUID))
				if path != expected {
					errCh <- fmt.Errorf("worker %d expected %s, got %s", workerID, expected, path)
				}
			}(i)
		}

		wg.Wait()
		close(errCh)

		// At least one worker MUST have succeeded, and the destination file MUST exist
		finalPath := filepath.Join(controlledDir, fmt.Sprintf("%s.replay", sameGUID))
		if _, statErr := os.Stat(finalPath); statErr != nil {
			t.Fatalf("expected final file %s to exist: %v", finalPath, statErr)
		}

		// Invariant: ZERO orphaned temp files remain
		entries, _ := os.ReadDir(controlledDir)
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), TempFilePrefix) {
				t.Fatalf("orphaned temp file found after controlled contention: %s", e.Name())
			}
		}
	})

	// Part 3: Stampede invariant testing (20 workers)
	t.Run("ConcurrentSameGUID_StampedeInvariants", func(t *testing.T) {
		stampedeDir := t.TempDir()
		sameGUID := "stampede-invariants-guid"
		const numStampede = 20

		var wg sync.WaitGroup
		var successCount atomic.Int32
		var failCount atomic.Int32

		for i := 0; i < numStampede; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := downloader.DownloadReplay(context.Background(), sameGUID, cdn.ReplayURL(sameGUID), stampedeDir)
				if err == nil {
					successCount.Add(1)
				} else {
					failCount.Add(1)
				}
			}()
		}

		wg.Wait()

		// Invariant 1: At least one download succeeded and created the file
		if successCount.Load() == 0 {
			t.Fatal("expected at least one worker to succeed during stampede")
		}

		// Invariant 2: Final file exists and contains valid replay data
		finalPath := filepath.Join(stampedeDir, fmt.Sprintf("%s.replay", sameGUID))
		data, err := os.ReadFile(finalPath)
		if err != nil {
			t.Fatalf("failed to read final stampede file: %v", err)
		}
		if !bytes.HasPrefix(data, testutil.ReplayMagicBytes) {
			t.Fatal("final stampede file corrupted; missing TAGAME magic bytes")
		}

		// Invariant 3: ALL temporary files from both winning and losing workers MUST be removed
		entries, err := os.ReadDir(stampedeDir)
		if err != nil {
			t.Fatalf("failed reading stampedeDir: %v", err)
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), TempFilePrefix) {
				t.Fatalf("orphaned temp file leaked by losing stampede worker: %s", e.Name())
			}
		}
		if len(entries) != 1 {
			t.Fatalf("expected exactly 1 final file in stampedeDir, found %d", len(entries))
		}
	})
}

// TestAdversarial_Downloader_FailedOverwritePreservesOriginal verifies that if an existing
// valid replay file exists on disk, and a subsequent download fails (e.g. truncated payload),
// the original valid file on disk is NOT corrupted or deleted.
func TestAdversarial_Downloader_FailedOverwritePreservesOriginal(t *testing.T) {
	cdn := testutil.NewMockCDNServer()
	defer cdn.Close()

	destDir := t.TempDir()
	downloader := NewDownloader()
	guid := "preserve-original-guid"

	// 1. Initial valid download (4KB)
	originalPath, err := downloader.DownloadReplay(context.Background(), guid, cdn.ReplayURL(guid), destDir)
	if err != nil {
		t.Fatalf("initial download failed: %v", err)
	}
	origStat, err := os.Stat(originalPath)
	if err != nil {
		t.Fatalf("failed to stat initial replay: %v", err)
	}
	if origStat.Size() < 1024 {
		t.Fatalf("initial size unexpectedly small: %d", origStat.Size())
	}

	// 2. Configure CDN to return a 500-byte truncated payload for the same GUID
	cdn.SetReplayPayload(guid, make([]byte, 500))

	// 3. Re-download attempt must fail
	_, err = downloader.DownloadReplay(context.Background(), guid, cdn.ReplayURL(guid), destDir)
	if err == nil {
		t.Fatal("expected failure on truncated re-download, got nil")
	}

	// 4. Invariant: The original file on disk MUST still be intact and undamaged!
	currentStat, err := os.Stat(originalPath)
	if err != nil {
		t.Fatalf("original file was deleted by failed re-download: %v", err)
	}
	if currentStat.Size() != origStat.Size() {
		t.Fatalf("original file was corrupted! expected size %d, got %d", origStat.Size(), currentStat.Size())
	}

	data, err := os.ReadFile(originalPath)
	if err != nil || !bytes.HasPrefix(data, testutil.ReplayMagicBytes) {
		t.Fatal("original file payload was corrupted or modified")
	}
}

// TestAdversarial_Downloader_CleanupStaleTempFiles_EdgeCases tests CleanupStaleTempFiles
// against mixed directories with valid replays, subdirectories, unrelated files, and non-existent dirs.
func TestAdversarial_Downloader_CleanupStaleTempFiles_EdgeCases(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Create 50 stale temp files (.tmp-*)
	for i := 0; i < 50; i++ {
		p := filepath.Join(tempDir, fmt.Sprintf("%sstale-match-%03d.replay", TempFilePrefix, i))
		_ = os.WriteFile(p, []byte("stale temp payload"), 0644)
	}

	// 2. Create 10 valid .replay files (must NOT be touched)
	for i := 0; i < 10; i++ {
		p := filepath.Join(tempDir, fmt.Sprintf("valid-match-%03d.replay", i))
		_ = os.WriteFile(p, testutil.GenerateValidReplay("valid", 1024), 0644)
	}

	// 3. Create unrelated files (.tmp, config.yaml, state.json, .tmp-foo.txt)
	unrelated := []string{
		"config.yaml",
		"state.json",
		"important.tmp",
		fmt.Sprintf("%snot-a-replay.txt", TempFilePrefix),
	}
	for _, u := range unrelated {
		_ = os.WriteFile(filepath.Join(tempDir, u), []byte("keep"), 0644)
	}

	// 4. Create subdirectories (including one with .tmp- prefix)
	_ = os.MkdirAll(filepath.Join(tempDir, fmt.Sprintf("%ssubdir.replay", TempFilePrefix)), 0755)
	_ = os.MkdirAll(filepath.Join(tempDir, "regular_folder"), 0755)

	// Execute cleanup
	removed, err := CleanupStaleTempFiles(tempDir)
	if err != nil {
		t.Fatalf("CleanupStaleTempFiles failed: %v", err)
	}
	if removed != 50 {
		t.Fatalf("expected exactly 50 stale temp files removed, got %d", removed)
	}

	// Verify all 10 valid files survived
	for i := 0; i < 10; i++ {
		p := filepath.Join(tempDir, fmt.Sprintf("valid-match-%03d.replay", i))
		if _, statErr := os.Stat(p); os.IsNotExist(statErr) {
			t.Fatalf("valid file was incorrectly deleted: %s", p)
		}
	}

	// Verify unrelated files survived
	for _, u := range unrelated {
		p := filepath.Join(tempDir, u)
		if _, statErr := os.Stat(p); os.IsNotExist(statErr) {
			t.Fatalf("unrelated file was incorrectly deleted: %s", p)
		}
	}

	// Verify non-existent directory returns (0, nil)
	nonExistent := filepath.Join(tempDir, "does-not-exist")
	count, err := CleanupStaleTempFiles(nonExistent)
	if err != nil || count != 0 {
		t.Fatalf("expected (0, nil) on non-existent dir, got (%d, %v)", count, err)
	}
}

// ============================================================================
// 2. MATCH HISTORY PROVIDER ADVERSARIAL STRESS SUITE
// ============================================================================

// TestAdversarial_Client_DelayedReplayURL_Progression verifies that matches with empty
// ReplayUrl are properly preserved in DiscoveredMatch across multiple polling cycles,
// allowing the syncer orchestrator to record them as SKIPPED until the CDN URL is populated.
func TestAdversarial_Client_DelayedReplayURL_Progression(t *testing.T) {
	mockRPC := NewMockRPCClient()
	client := NewClientWithRPC(mockRPC, nil)
	defer client.Close()

	ctx := context.Background()

	now := time.Now().Unix()
	mockRPC.mu.Lock()
	mockRPC.matches = []rlapi.MatchEntry{
		{
			ReplayUrl: "", // delayed
			Match:     rlapi.Match{MatchGUID: "guid-delayed-1", RecordStartTimestamp: now, MapName: "Wasteland_P", Playlist: 2},
		},
		{
			ReplayUrl: "", // delayed
			Match:     rlapi.Match{MatchGUID: "guid-delayed-2", RecordStartTimestamp: now + 60, MapName: "Stadium_P", Playlist: 3},
		},
		{
			ReplayUrl: "https://cdn.example.com/3.replay",
			Match:     rlapi.Match{MatchGUID: "guid-ready-3", RecordStartTimestamp: now + 120, MapName: "Utopia_P", Playlist: 2},
		},
		{
			ReplayUrl: "https://cdn.example.com/4.replay",
			Match:     rlapi.Match{MatchGUID: "guid-ready-4", RecordStartTimestamp: now + 180, MapName: "DFHStadium_P", Playlist: 1},
		},
	}
	mockRPC.mu.Unlock()

	cycle1, err := client.GetRecentMatches(ctx)
	if err != nil {
		t.Fatalf("cycle 1 GetRecentMatches failed: %v", err)
	}
	if len(cycle1) != 4 {
		t.Fatalf("expected 4 matches in cycle 1, got %d", len(cycle1))
	}

	// Verify matches 1 and 2 have empty ReplayURL preserved
	if cycle1[0].MatchGUID != "guid-delayed-1" || cycle1[0].ReplayURL != "" {
		t.Errorf("match 1 expected empty ReplayURL, got %q", cycle1[0].ReplayURL)
	}
	if cycle1[1].MatchGUID != "guid-delayed-2" || cycle1[1].ReplayURL != "" {
		t.Errorf("match 2 expected empty ReplayURL, got %q", cycle1[1].ReplayURL)
	}
	if cycle1[2].MatchGUID != "guid-ready-3" || cycle1[2].ReplayURL == "" {
		t.Errorf("match 3 expected valid ReplayURL, got %q", cycle1[2].ReplayURL)
	}

	// Cycle 2 (simulating 5 minutes later):
	mockRPC.mu.Lock()
	mockRPC.matches = []rlapi.MatchEntry{
		{
			ReplayUrl: "https://cdn.example.com/delayed-1.replay?token=xyz",
			Match:     rlapi.Match{MatchGUID: "guid-delayed-1", RecordStartTimestamp: now, MapName: "Wasteland_P", Playlist: 2},
		},
		{
			ReplayUrl: "", // still delayed
			Match:     rlapi.Match{MatchGUID: "guid-delayed-2", RecordStartTimestamp: now + 60, MapName: "Stadium_P", Playlist: 3},
		},
		{
			ReplayUrl: "https://cdn.example.com/3.replay",
			Match:     rlapi.Match{MatchGUID: "guid-ready-3", RecordStartTimestamp: now + 120, MapName: "Utopia_P", Playlist: 2},
		},
		{
			ReplayUrl: "https://cdn.example.com/4.replay",
			Match:     rlapi.Match{MatchGUID: "guid-ready-4", RecordStartTimestamp: now + 180, MapName: "DFHStadium_P", Playlist: 1},
		},
		{
			ReplayUrl: "", // new match, delayed
			Match:     rlapi.Match{MatchGUID: "guid-new-5", RecordStartTimestamp: now + 240, MapName: "NeoTokyo_P", Playlist: 2},
		},
	}
	mockRPC.mu.Unlock()

	cycle2, err := client.GetRecentMatches(ctx)
	if err != nil {
		t.Fatalf("cycle 2 GetRecentMatches failed: %v", err)
	}
	if len(cycle2) != 5 {
		t.Fatalf("expected 5 matches in cycle 2, got %d", len(cycle2))
	}

	// Verify match 1 now has its updated URL
	if cycle2[0].MatchGUID != "guid-delayed-1" || cycle2[0].ReplayURL != "https://cdn.example.com/delayed-1.replay?token=xyz" {
		t.Errorf("cycle 2: match 1 expected populated URL, got %q", cycle2[0].ReplayURL)
	}
	// Verify match 2 is still empty
	if cycle2[1].MatchGUID != "guid-delayed-2" || cycle2[1].ReplayURL != "" {
		t.Errorf("cycle 2: match 2 expected empty URL, got %q", cycle2[1].ReplayURL)
	}
	// Verify match 5 is present with empty URL
	if cycle2[4].MatchGUID != "guid-new-5" || cycle2[4].ReplayURL != "" {
		t.Errorf("cycle 2: match 5 expected empty URL, got %q", cycle2[4].ReplayURL)
	}
}

// TestAdversarial_Client_MalformedMetadata_Filtering tests edge cases in PsyNet responses:
// empty GUIDs, whitespace GUIDs, zero timestamps, and empty response lists.
func TestAdversarial_Client_MalformedMetadata_Filtering(t *testing.T) {
	mockRPC := NewMockRPCClient()
	client := NewClientWithRPC(mockRPC, nil)
	defer client.Close()

	ctx := context.Background()

	// Case 1: Mixed malformed and valid entries
	mockRPC.mu.Lock()
	mockRPC.matches = []rlapi.MatchEntry{
		{
			ReplayUrl: "https://example.com/1.replay",
			Match:     rlapi.Match{MatchGUID: ""}, // empty GUID -> MUST BE SKIPPED
		},
		{
			ReplayUrl: "https://example.com/2.replay",
			Match:     rlapi.Match{MatchGUID: "    "}, // whitespace GUID -> MUST BE SKIPPED
		},
		{
			ReplayUrl: "https://example.com/3.replay",
			Match: rlapi.Match{
				MatchGUID:            "valid-guid-001",
				RecordStartTimestamp: 0, // zero timestamp -> MUST DEFAULT TO > 0
				MapName:              "Field_P",
				Playlist:             11,
			},
		},
		{
			ReplayUrl: "https://example.com/4.replay",
			Match: rlapi.Match{
				MatchGUID:            "valid-guid-002",
				RecordStartTimestamp: 1700000000,
				MapName:              "", // empty map name -> preserved
				Playlist:             0,
			},
		},
	}
	mockRPC.mu.Unlock()

	matches, err := client.GetRecentMatches(ctx)
	if err != nil {
		t.Fatalf("GetRecentMatches failed: %v", err)
	}

	// Exactly 2 valid matches should survive
	if len(matches) != 2 {
		t.Fatalf("expected 2 valid matches after filtering, got %d", len(matches))
	}
	if matches[0].MatchGUID != "valid-guid-001" {
		t.Errorf("expected valid-guid-001, got %s", matches[0].MatchGUID)
	}
	if matches[0].RecordStartTimestamp <= 0 {
		t.Errorf("expected zero timestamp to default to positive Unix timestamp, got %d", matches[0].RecordStartTimestamp)
	}
	if matches[1].MatchGUID != "valid-guid-002" {
		t.Errorf("expected valid-guid-002, got %s", matches[1].MatchGUID)
	}

	// Case 2: Completely empty match list from PsyNet
	mockRPC.mu.Lock()
	mockRPC.matches = []rlapi.MatchEntry{}
	mockRPC.mu.Unlock()

	emptyMatches, err := client.GetRecentMatches(ctx)
	if err != nil {
		t.Fatalf("expected success on empty match list, got: %v", err)
	}
	if len(emptyMatches) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(emptyMatches))
	}
}

// TestAdversarial_Client_ConnectionDrop_TransparentReconnect stresses auto-reconnection:
// 1. Initial query encounters dropped socket -> transparent reconnect succeeds and returns matches
// 2. Query fails with drop, and reconnect ALSO fails -> returns clean descriptive error
// 3. Repeated drops over multiple consecutive query cycles
func TestAdversarial_Client_ConnectionDrop_TransparentReconnect(t *testing.T) {
	ctx := context.Background()

	// Scenario 1: Drop on query, transparent reconnect succeeds
	t.Run("Drop_TransparentReconnectSuccess", func(t *testing.T) {
		var connectCalls atomic.Int32

		rpcDrop := NewMockRPCClient()
		rpcDrop.mu.Lock()
		rpcDrop.err = rlapi.ErrConnectionClosed
		rpcDrop.mu.Unlock()

		rpcRecovered := NewMockRPCClient(rlapi.MatchEntry{
			ReplayUrl: "https://example.com/recovered.replay",
			Match:     rlapi.Match{MatchGUID: "guid-rec-001", RecordStartTimestamp: 1000},
		})

		cfg := ClientConfig{
			Credentials: &Credentials{
				Platform:  "Epic",
				AuthToken: "token-1",
				AccountID: "acc-1",
			},
			RPCFactory: func(ctx context.Context, creds *Credentials) (RPCClient, error) {
				call := connectCalls.Add(1)
				if call == 1 {
					return rpcDrop, nil
				}
				return rpcRecovered, nil
			},
		}

		client, err := NewClient(cfg)
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		defer client.Close()

		matches, err := client.GetRecentMatches(ctx)
		if err != nil {
			t.Fatalf("expected transparent retry to succeed, got: %v", err)
		}
		if len(matches) != 1 || matches[0].MatchGUID != "guid-rec-001" {
			t.Fatalf("unexpected matches: %+v", matches)
		}
		if connectCalls.Load() != 2 {
			t.Fatalf("expected exactly 2 connect calls (initial + reconnect), got %d", connectCalls.Load())
		}
	})

	// Scenario 2: Network error (EOF) on query, transparent reconnect succeeds
	t.Run("EOF_TransparentReconnectSuccess", func(t *testing.T) {
		var connectCalls atomic.Int32

		rpcEOF := NewMockRPCClient()
		rpcEOF.mu.Lock()
		rpcEOF.err = io.EOF
		rpcEOF.mu.Unlock()

		rpcRecovered := NewMockRPCClient(rlapi.MatchEntry{
			ReplayUrl: "https://example.com/recovered-eof.replay",
			Match:     rlapi.Match{MatchGUID: "guid-eof-recovered", RecordStartTimestamp: 2000},
		})

		cfg := ClientConfig{
			Credentials: &Credentials{Platform: "Epic", AuthToken: "tok", AccountID: "acc"},
			RPCFactory: func(ctx context.Context, creds *Credentials) (RPCClient, error) {
				call := connectCalls.Add(1)
				if call == 1 {
					return rpcEOF, nil
				}
				return rpcRecovered, nil
			},
		}

		client, err := NewClient(cfg)
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		defer client.Close()

		matches, err := client.GetRecentMatches(ctx)
		if err != nil {
			t.Fatalf("expected EOF reconnect to succeed, got: %v", err)
		}
		if len(matches) != 1 || matches[0].MatchGUID != "guid-eof-recovered" {
			t.Fatalf("unexpected matches: %+v", matches)
		}
	})

	// Scenario 3: Drop on query, but reconnect ALSO fails
	t.Run("Drop_ReconnectAlsoFails", func(t *testing.T) {
		var connectCalls atomic.Int32

		rpcDrop := NewMockRPCClient()
		rpcDrop.mu.Lock()
		rpcDrop.err = rlapi.ErrConnectionClosed
		rpcDrop.mu.Unlock()

		cfg := ClientConfig{
			Credentials: &Credentials{Platform: "Epic", AuthToken: "tok", AccountID: "acc"},
			RPCFactory: func(ctx context.Context, creds *Credentials) (RPCClient, error) {
				call := connectCalls.Add(1)
				if call == 1 {
					return rpcDrop, nil
				}
				return nil, errors.New("psynet auth server unreachable")
			},
		}

		client, err := NewClient(cfg)
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		defer client.Close()

		_, err = client.GetRecentMatches(ctx)
		if err == nil {
			t.Fatal("expected error when reconnect fails, got nil")
		}
		if !strings.Contains(err.Error(), "reconnect failed") {
			t.Fatalf("expected error mentioning reconnect failed, got: %v", err)
		}
	})

	// Scenario 4: Consecutive drops across multiple polling cycles
	t.Run("ConsecutiveDropsAcrossCycles", func(t *testing.T) {
		var rpcCycleCount atomic.Int32

		cfg := ClientConfig{
			Credentials: &Credentials{Platform: "Epic", AuthToken: "tok", AccountID: "acc"},
			RPCFactory: func(ctx context.Context, creds *Credentials) (RPCClient, error) {
				cycle := rpcCycleCount.Add(1)
				rpc := NewMockRPCClient(rlapi.MatchEntry{
					ReplayUrl: fmt.Sprintf("https://example.com/%d.replay", cycle),
					Match:     rlapi.Match{MatchGUID: fmt.Sprintf("guid-cycle-%d", cycle)},
				})
				// For odd calls, simulate drop on first query
				if cycle%2 == 1 {
					var dropped atomic.Bool
					rpc.onQuery = func(c context.Context) ([]rlapi.MatchEntry, error) {
						if !dropped.Swap(true) {
							return nil, rlapi.ErrConnectionClosed
						}
						return rpc.matches, nil
					}
				}
				return rpc, nil
			},
		}

		client, err := NewClient(cfg)
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		defer client.Close()

		// Run 3 polling cycles
		for cycle := 1; cycle <= 3; cycle++ {
			matches, err := client.GetRecentMatches(ctx)
			if err != nil {
				t.Fatalf("cycle %d failed: %v", cycle, err)
			}
			if len(matches) == 0 {
				t.Fatalf("cycle %d returned empty matches", cycle)
			}
		}
	})
}

// TestAdversarial_Client_ContextCancellation_NoHangs tests that context cancellation
// terminates immediately without hanging or leaking goroutines.
func TestAdversarial_Client_ContextCancellation_NoHangs(t *testing.T) {
	// Case 1: Pre-cancelled context
	t.Run("PreCancelledContext", func(t *testing.T) {
		mockRPC := NewMockRPCClient()
		client := NewClientWithRPC(mockRPC, nil)
		defer client.Close()

		ctxCancel, cancel := context.WithCancel(context.Background())
		cancel() // immediately cancelled

		_, err := client.GetRecentMatches(ctxCancel)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got: %v", err)
		}
	})

	// Case 2: Short deadline context (5ms)
	t.Run("DeadlineExceeded", func(t *testing.T) {
		mockRPC := NewMockRPCClient()
		mockRPC.mu.Lock()
		mockRPC.onQuery = func(ctx context.Context) ([]rlapi.MatchEntry, error) {
			select {
			case <-time.After(100 * time.Millisecond):
				return nil, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		mockRPC.mu.Unlock()

		client := NewClientWithRPC(mockRPC, nil)
		defer client.Close()

		ctxDeadline, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		defer cancel()

		_, err := client.GetRecentMatches(ctxDeadline)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected context.DeadlineExceeded, got: %v", err)
		}
	})
}

// TestAdversarial_Client_ConcurrencyAndCloseSafety tests thread safety under concurrent
// GetRecentMatches calls while Close() is invoked.
func TestAdversarial_Client_ConcurrencyAndCloseSafety(t *testing.T) {
	mockRPC := NewMockRPCClient(rlapi.MatchEntry{
		ReplayUrl: "https://example.com/replay.replay",
		Match:     rlapi.Match{MatchGUID: "concur-guid", RecordStartTimestamp: 12345},
	})

	client := NewClientWithRPC(mockRPC, nil)

	const numWorkers = 20
	const opsPerWorker = 10
	var wg sync.WaitGroup

	// Launch concurrent readers
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for op := 0; op < opsPerWorker; op++ {
				_, err := client.GetRecentMatches(context.Background())
				if err != nil && !errors.Is(err, ErrClientClosed) {
					// Other network errors are acceptable if closed mid-flight, but no panic or memory corruption
					_ = err
				}
				time.Sleep(1 * time.Millisecond)
			}
		}(w)
	}

	// Mid-way close
	time.Sleep(5 * time.Millisecond)
	_ = client.Close()

	wg.Wait()

	// Post-close calls must return ErrClientClosed
	_, err := client.GetRecentMatches(context.Background())
	if !errors.Is(err, ErrClientClosed) {
		t.Fatalf("expected ErrClientClosed post-close, got: %v", err)
	}

	// Multiple Close calls are safe and idempotent
	for i := 0; i < 5; i++ {
		if closeErr := client.Close(); closeErr != nil {
			t.Fatalf("idempotent Close failed on attempt %d: %v", i, closeErr)
		}
	}
}

// TestAdversarial_Client_InvalidCredentials verifies defensive guards against malformed credentials.
func TestAdversarial_Client_InvalidCredentials(t *testing.T) {
	ctx := context.Background()

	t.Run("MissingCredentialsSupplierAndCreds", func(t *testing.T) {
		client, err := NewClient(ClientConfig{})
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		defer client.Close()

		_, err = client.GetRecentMatches(ctx)
		if !errors.Is(err, ErrMissingCredentials) {
			t.Fatalf("expected ErrMissingCredentials, got: %v", err)
		}
	})

	t.Run("UnsupportedPlatform", func(t *testing.T) {
		client, err := NewClient(ClientConfig{
			Credentials: &Credentials{
				Platform:  "NintendoSwitch",
				AuthToken: "tok",
				AccountID: "acc",
			},
		})
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		defer client.Close()

		_, err = client.GetRecentMatches(ctx)
		if !errors.Is(err, ErrInvalidPlatform) {
			t.Fatalf("expected ErrInvalidPlatform, got: %v", err)
		}
	})

	t.Run("SteamMissingSteamAccountID", func(t *testing.T) {
		client, err := NewClient(ClientConfig{
			Credentials: &Credentials{
				Platform:       "Steam",
				AuthToken:      "eos-tok",
				AccountID:      "acc-id",
				SteamAccountID: "", // empty
			},
		})
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		defer client.Close()

		_, err = client.GetRecentMatches(ctx)
		if !errors.Is(err, ErrMissingSteamAccount) {
			t.Fatalf("expected ErrMissingSteamAccount, got: %v", err)
		}
	})
}
