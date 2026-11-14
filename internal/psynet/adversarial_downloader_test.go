package psynet

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/testutil"
)

// TestAdversarial_Downloader_ExtremePayloads tests boundary conditions on payload sizes.
func TestAdversarial_Downloader_ExtremePayloads(t *testing.T) {
	cdn := testutil.NewMockCDNServer()
	defer cdn.Close()

	destDir := t.TempDir()
	downloader := NewDownloader(WithTimeout(15 * time.Second))

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

	acceptSizes := []struct {
		name string
		size int
	}{
		{"1024-byte exact threshold", 1024},
		{"1025-byte just-over-threshold", 1025},
		{"64KB medium replay", 64 * 1024},
		{"1MB large replay", 1024 * 1024},
		{"5MB extreme overtime replay", 5 * 1024 * 1024},
	}

	for _, tc := range acceptSizes {
		t.Run("Accept_"+tc.name, func(t *testing.T) {
			guid := fmt.Sprintf("adv-accept-%d", tc.size)
			cdn.SetReplayPayload(guid, testutil.GenerateValidReplay(guid, tc.size))

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

			data, readErr := os.ReadFile(path)
			if readErr != nil || !bytes.HasPrefix(data, testutil.ReplayMagicBytes) {
				t.Fatalf("[%s] replay missing TAGAME magic bytes prefix: %v", tc.name, readErr)
			}

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
			if httpErr.StatusCode != sc.code || httpErr.MatchGUID != guid {
				t.Fatalf("unexpected HTTPStatusError: %+v", httpErr)
			}

			entries, _ := os.ReadDir(destDir)
			for _, e := range entries {
				if strings.Contains(e.Name(), guid) {
					t.Fatalf("unexpected file on disk after HTTP %d: %s", sc.code, e.Name())
				}
			}
		})
	}
}

// TestAdversarial_Downloader_MidStreamDropsAndInterrupts tests stream drops and hangs.
func TestAdversarial_Downloader_MidStreamDropsAndInterrupts(t *testing.T) {
	destDir := t.TempDir()

	t.Run("CDN_TruncatedStream_NoOrphan", func(t *testing.T) {
		cdn := testutil.NewMockCDNServer()
		defer cdn.Close()

		guid := "adv-drop-cdn"
		cdn.SetTruncateStream(guid, true)

		downloader := NewDownloader(WithTimeout(5 * time.Second))
		if _, err := downloader.DownloadReplay(context.Background(), guid, cdn.ReplayURL(guid), destDir); err == nil {
			t.Fatal("expected error on truncated stream, got nil")
		}

		entries, _ := os.ReadDir(destDir)
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), TempFilePrefix) || strings.Contains(e.Name(), guid) {
				t.Fatalf("orphaned temp file found after stream truncation: %s", e.Name())
			}
		}
	})

	t.Run("TCP_DropAfter4KB_ContentLengthMismatch", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "102400")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(testutil.GenerateValidReplay("guid-drop-4k", 4096))
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
		if _, err := downloader.DownloadReplay(context.Background(), guid, ts.URL+"/drop", destDir); err == nil {
			t.Fatal("expected error when connection closes before Content-Length satisfied, got nil")
		}

		entries, _ := os.ReadDir(destDir)
		for _, e := range entries {
			if strings.Contains(e.Name(), guid) {
				t.Fatalf("orphaned file on disk after mid-stream drop: %s", e.Name())
			}
		}
	})

	t.Run("ServerHangs_ContextTimeout_CleanedUp", func(t *testing.T) {
		hangingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
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
		if _, err := downloader.DownloadReplay(ctxTimeout, guid, hangingServer.URL+"/hang", destDir); err == nil {
			t.Fatal("expected timeout error, got nil")
		}

		entries, _ := os.ReadDir(destDir)
		for _, e := range entries {
			if strings.Contains(e.Name(), guid) {
				t.Fatalf("orphaned file left after context timeout: %s", e.Name())
			}
		}
	})
}

// TestAdversarial_Downloader_PathTraversal_ComprehensiveMatrix verifies path sanitization.
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

// TestAdversarial_Downloader_FailedOverwritePreservesOriginal ensures failed overwrites do not damage existing replays.
func TestAdversarial_Downloader_FailedOverwritePreservesOriginal(t *testing.T) {
	cdn := testutil.NewMockCDNServer()
	defer cdn.Close()

	destDir := t.TempDir()
	downloader := NewDownloader()
	guid := "preserve-original-guid"

	originalPath, err := downloader.DownloadReplay(context.Background(), guid, cdn.ReplayURL(guid), destDir)
	if err != nil {
		t.Fatalf("initial download failed: %v", err)
	}
	origStat, err := os.Stat(originalPath)
	if err != nil || origStat.Size() < 1024 {
		t.Fatalf("failed to stat valid initial replay: %v", err)
	}

	cdn.SetReplayPayload(guid, make([]byte, 500))
	if _, err := downloader.DownloadReplay(context.Background(), guid, cdn.ReplayURL(guid), destDir); err == nil {
		t.Fatal("expected failure on truncated re-download, got nil")
	}

	currentStat, err := os.Stat(originalPath)
	if err != nil || currentStat.Size() != origStat.Size() {
		t.Fatalf("original file damaged after failed overwrite: err=%v", err)
	}
	data, err := os.ReadFile(originalPath)
	if err != nil || !bytes.HasPrefix(data, testutil.ReplayMagicBytes) {
		t.Fatal("original file payload was corrupted or modified")
	}
}

// TestAdversarial_Downloader_CleanupStaleTempFiles_EdgeCases verifies stale temp file removal.
func TestAdversarial_Downloader_CleanupStaleTempFiles_EdgeCases(t *testing.T) {
	tempDir := t.TempDir()

	for i := 0; i < 50; i++ {
		p := filepath.Join(tempDir, fmt.Sprintf("%sstale-match-%03d.replay", TempFilePrefix, i))
		_ = os.WriteFile(p, []byte("stale temp payload"), 0644)
	}
	for i := 0; i < 10; i++ {
		p := filepath.Join(tempDir, fmt.Sprintf("valid-match-%03d.replay", i))
		_ = os.WriteFile(p, testutil.GenerateValidReplay("valid", 1024), 0644)
	}
	unrelated := []string{
		"config.yaml",
		"state.json",
		"important.tmp",
		fmt.Sprintf("%snot-a-replay.txt", TempFilePrefix),
	}
	for _, u := range unrelated {
		_ = os.WriteFile(filepath.Join(tempDir, u), []byte("keep"), 0644)
	}
	_ = os.MkdirAll(filepath.Join(tempDir, fmt.Sprintf("%ssubdir.replay", TempFilePrefix)), 0755)
	_ = os.MkdirAll(filepath.Join(tempDir, "regular_folder"), 0755)

	removed, err := CleanupStaleTempFiles(tempDir)
	if err != nil || removed != 50 {
		t.Fatalf("expected 50 removed, got (%d, %v)", removed, err)
	}

	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(tempDir, fmt.Sprintf("valid-match-%03d.replay", i))); os.IsNotExist(err) {
			t.Fatalf("valid file %d was incorrectly deleted", i)
		}
	}
	for _, u := range unrelated {
		if _, err := os.Stat(filepath.Join(tempDir, u)); os.IsNotExist(err) {
			t.Fatalf("unrelated file %s was incorrectly deleted", u)
		}
	}
	if count, err := CleanupStaleTempFiles(filepath.Join(tempDir, "does-not-exist")); err != nil || count != 0 {
		t.Fatalf("expected (0, nil) on non-existent dir, got (%d, %v)", count, err)
	}
}
