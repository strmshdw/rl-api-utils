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
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/testutil"
)

func TestDownloader_Success(t *testing.T) {
	cdn := testutil.NewMockCDNServer()
	defer cdn.Close()

	destDir := t.TempDir()
	guid := "test-guid-success-001"
	downloader := NewDownloader(WithTimeout(10 * time.Second))

	path, err := downloader.DownloadReplay(context.Background(), guid, cdn.ReplayURL(guid), destDir)
	if err != nil {
		t.Fatalf("DownloadReplay failed unexpectedly: %v", err)
	}

	expectedPath := filepath.Join(destDir, fmt.Sprintf("%s.replay", guid))
	if path != expectedPath {
		t.Fatalf("expected path %s, got %s", expectedPath, path)
	}

	// Verify file on disk
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read downloaded replay file: %v", err)
	}
	if len(data) < 1024 {
		t.Fatalf("expected file size >= 1024, got %d", len(data))
	}
	if !bytes.HasPrefix(data, testutil.ReplayMagicBytes) {
		t.Fatalf("downloaded file missing TAGAME magic bytes prefix, got %q", data[:10])
	}

	// Verify download count on CDN
	if cdn.GetDownloadCount(guid) != 1 {
		t.Fatalf("expected CDN download count 1, got %d", cdn.GetDownloadCount(guid))
	}

	// Verify no temporary files remain in destDir
	entries, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("failed to read destDir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), TempFilePrefix) {
			t.Fatalf("unexpected temporary file left behind: %s", entry.Name())
		}
	}
}

func TestDownloader_DestDirAutoCreation(t *testing.T) {
	cdn := testutil.NewMockCDNServer()
	defer cdn.Close()

	baseDir := t.TempDir()
	nestedDest := filepath.Join(baseDir, "nested", "level1", "level2", "replays")
	guid := "test-guid-nested"
	downloader := NewHTTPReplayDownloader(5 * time.Second)

	path, err := downloader.DownloadReplay(context.Background(), guid, cdn.ReplayURL(guid), nestedDest)
	if err != nil {
		t.Fatalf("failed downloading to non-existent nested directory: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not found in auto-created directory: %v", err)
	}
}

func TestDownloader_InputValidation(t *testing.T) {
	destDir := t.TempDir()
	downloader := NewDownloader()

	tests := []struct {
		name        string
		guid        string
		url         string
		expectedErr error
	}{
		{
			name:        "empty GUID",
			guid:        "",
			url:         "http://example.com/replay.replay",
			expectedErr: ErrEmptyMatchGUID,
		},
		{
			name:        "whitespace GUID",
			guid:        "   ",
			url:         "http://example.com/replay.replay",
			expectedErr: ErrEmptyMatchGUID,
		},
		{
			name:        "path traversal GUID dot-dot",
			guid:        "../sneaky",
			url:         "http://example.com/replay.replay",
			expectedErr: ErrInvalidMatchGUID,
		},
		{
			name:        "path traversal GUID forward slash",
			guid:        "sub/folder",
			url:         "http://example.com/replay.replay",
			expectedErr: ErrInvalidMatchGUID,
		},
		{
			name:        "path traversal GUID backward slash",
			guid:        `sub\folder`,
			url:         "http://example.com/replay.replay",
			expectedErr: ErrInvalidMatchGUID,
		},
		{
			name:        "empty replay URL",
			guid:        "guid-ok",
			url:         "",
			expectedErr: ErrEmptyReplayURL,
		},
		{
			name:        "whitespace replay URL",
			guid:        "guid-ok",
			url:         "   ",
			expectedErr: ErrEmptyReplayURL,
		},
		{
			name:        "invalid URL scheme ftp",
			guid:        "guid-ok",
			url:         "ftp://example.com/replay.replay",
			expectedErr: ErrInvalidReplayURL,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := downloader.DownloadReplay(context.Background(), tc.guid, tc.url, destDir)
			if err == nil {
				t.Fatalf("expected error for case %q, got nil", tc.name)
			}
			if !errors.Is(err, tc.expectedErr) && !strings.Contains(err.Error(), tc.expectedErr.Error()) {
				t.Fatalf("expected error matching %v, got %v", tc.expectedErr, err)
			}
		})
	}
}

func TestDownloader_RejectUnder1KB(t *testing.T) {
	cdn := testutil.NewMockCDNServer()
	defer cdn.Close()

	destDir := t.TempDir()
	downloader := NewDownloader()

	sizes := []struct {
		name string
		size int
	}{
		{"0-byte empty payload", 0},
		{"100-byte tiny payload", 100},
		{"512-byte truncated payload", 512},
		{"1023-byte just-under-threshold payload", 1023},
	}

	for _, tc := range sizes {
		t.Run(tc.name, func(t *testing.T) {
			guid := fmt.Sprintf("guid-size-%d", tc.size)
			cdn.SetReplayPayload(guid, make([]byte, tc.size))

			_, err := downloader.DownloadReplay(context.Background(), guid, cdn.ReplayURL(guid), destDir)
			if err == nil {
				t.Fatalf("expected rejection for size %d, got nil error", tc.size)
			}
			if !errors.Is(err, ErrReplayTooSmall) {
				t.Fatalf("expected ErrReplayTooSmall, got %v", err)
			}

			// Ensure neither .tmp nor .replay exists on disk
			entries, _ := os.ReadDir(destDir)
			for _, e := range entries {
				if strings.Contains(e.Name(), guid) {
					t.Fatalf("found leftover file on disk after size validation failure: %s", e.Name())
				}
			}
		})
	}
}

func TestDownloader_Boundary1024Bytes(t *testing.T) {
	cdn := testutil.NewMockCDNServer()
	defer cdn.Close()

	destDir := t.TempDir()
	guid := "guid-boundary-1024"
	// Generate exactly 1024 bytes payload starting with TAGAME
	payload := testutil.GenerateValidReplay(guid, 1024)
	cdn.SetReplayPayload(guid, payload)

	downloader := NewDownloader()
	path, err := downloader.DownloadReplay(context.Background(), guid, cdn.ReplayURL(guid), destDir)
	if err != nil {
		t.Fatalf("expected exactly 1024 bytes to succeed, got error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat downloaded file: %v", err)
	}
	if info.Size() != 1024 {
		t.Fatalf("expected file size 1024, got %d", info.Size())
	}
}

func TestDownloader_HTTPStatusErrors(t *testing.T) {
	cdn := testutil.NewMockCDNServer()
	defer cdn.Close()

	destDir := t.TempDir()
	downloader := NewDownloader()

	statusCodes := []struct {
		name       string
		statusCode int
	}{
		{"403 Forbidden (Expired signed URL)", http.StatusForbidden},
		{"404 Not Found (Deleted replay)", http.StatusNotFound},
		{"410 Gone (Purged replay)", http.StatusGone},
		{"500 Internal Server Error (CDN failure)", http.StatusInternalServerError},
		{"503 Service Unavailable", http.StatusServiceUnavailable},
	}

	for _, sc := range statusCodes {
		t.Run(sc.name, func(t *testing.T) {
			guid := fmt.Sprintf("guid-status-%d", sc.statusCode)
			cdn.SetStatusCode(guid, sc.statusCode)

			_, err := downloader.DownloadReplay(context.Background(), guid, cdn.ReplayURL(guid), destDir)
			if err == nil {
				t.Fatalf("expected error for HTTP %d, got nil", sc.statusCode)
			}

			// Verify error message preserves status code
			expectedCodeStr := fmt.Sprintf("%d", sc.statusCode)
			if !strings.Contains(err.Error(), expectedCodeStr) {
				t.Fatalf("expected error string to contain status code %d, got %v", sc.statusCode, err)
			}

			// Verify typed HTTPStatusError
			var httpErr *HTTPStatusError
			if !errors.As(err, &httpErr) {
				t.Fatalf("expected error to wrap *HTTPStatusError, got %T: %v", err, err)
			}
			if httpErr.StatusCode != sc.statusCode {
				t.Fatalf("expected HTTPStatusError.StatusCode == %d, got %d", sc.statusCode, httpErr.StatusCode)
			}

			// Verify cleanup of temporary files
			entries, _ := os.ReadDir(destDir)
			for _, e := range entries {
				if strings.Contains(e.Name(), guid) {
					t.Fatalf("leftover file found on disk after HTTP error: %s", e.Name())
				}
			}
		})
	}
}

func TestDownloader_TruncatedStream_ConnectionDrop(t *testing.T) {
	cdn := testutil.NewMockCDNServer()
	defer cdn.Close()

	destDir := t.TempDir()
	guid := "guid-truncated-drop"
	cdn.SetTruncateStream(guid, true)

	downloader := NewDownloader()
	_, err := downloader.DownloadReplay(context.Background(), guid, cdn.ReplayURL(guid), destDir)
	if err == nil {
		t.Fatal("expected error on mid-stream connection drop, got nil")
	}

	// Verify temporary file was cleaned up
	entries, _ := os.ReadDir(destDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), TempFilePrefix) {
			t.Fatalf("orphaned temp file remained after connection drop: %s", e.Name())
		}
	}
}

func TestDownloader_ContextCancellation(t *testing.T) {
	cdn := testutil.NewMockCDNServer()
	defer cdn.Close()

	destDir := t.TempDir()
	guid := "guid-ctx-cancel"
	downloader := NewDownloader()

	// 1. Context already cancelled before call
	ctxCancel, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := downloader.DownloadReplay(ctxCancel, guid, cdn.ReplayURL(guid), destDir)
	if err == nil {
		t.Fatal("expected error with pre-cancelled context")
	}
	if !strings.Contains(err.Error(), "context cancelled") {
		t.Fatalf("expected context cancelled error, got %v", err)
	}

	// Verify no files on disk
	entries, _ := os.ReadDir(destDir)
	if len(entries) != 0 {
		t.Fatalf("expected empty directory after pre-cancellation, found %d entries", len(entries))
	}
}

func TestDownloader_AtomicOverwrite(t *testing.T) {
	cdn := testutil.NewMockCDNServer()
	defer cdn.Close()

	destDir := t.TempDir()
	guid := "guid-overwrite"
	downloader := NewDownloader()

	// Initial download (4KB)
	path1, err := downloader.DownloadReplay(context.Background(), guid, cdn.ReplayURL(guid), destDir)
	if err != nil {
		t.Fatalf("initial download failed: %v", err)
	}

	// Update CDN payload to 8KB
	largerPayload := testutil.GenerateValidReplay(guid, 8192)
	cdn.SetReplayPayload(guid, largerPayload)

	// Re-download identical GUID (simulating update or re-fetch)
	path2, err := downloader.DownloadReplay(context.Background(), guid, cdn.ReplayURL(guid), destDir)
	if err != nil {
		t.Fatalf("re-download overwrite failed: %v", err)
	}
	if path1 != path2 {
		t.Fatalf("expected identical path %s, got %s", path1, path2)
	}

	info, err := os.Stat(path2)
	if err != nil {
		t.Fatalf("failed to stat overwritten file: %v", err)
	}
	if info.Size() != 8192 {
		t.Fatalf("expected overwritten file size 8192, got %d", info.Size())
	}
}

func TestDownloader_CustomOptions(t *testing.T) {
	var receivedUA string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(testutil.GenerateValidReplay("guid-custom", 2048))
	}))
	defer ts.Close()

	destDir := t.TempDir()
	customUA := "CustomRLAgent/3.14"
	downloader := NewDownloader(
		WithUserAgent(customUA),
		WithTimeout(15*time.Second),
		WithMinSize(2048),
		WithRenameRetries(3),
		WithBufferSize(32*1024),
	)

	_, err := downloader.DownloadReplay(context.Background(), "guid-custom", ts.URL+"/replay", destDir)
	if err != nil {
		t.Fatalf("download with custom options failed: %v", err)
	}

	if receivedUA != customUA {
		t.Fatalf("expected User-Agent %q, got %q", customUA, receivedUA)
	}
}

func TestDownloader_ConcurrentDownloads(t *testing.T) {
	cdn := testutil.NewMockCDNServer()
	defer cdn.Close()

	destDir := t.TempDir()
	downloader := NewDownloader()

	const count = 10
	var wg sync.WaitGroup
	errs := make([]error, count)

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			guid := fmt.Sprintf("guid-concur-%d", idx)
			_, err := downloader.DownloadReplay(context.Background(), guid, cdn.ReplayURL(guid), destDir)
			errs[idx] = err
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent download %d failed: %v", i, err)
		}
	}

	// Verify all 10 files exist and no .tmp files remain
	entries, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("failed reading destDir: %v", err)
	}
	if len(entries) != count {
		t.Fatalf("expected %d files on disk, found %d", count, len(entries))
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), TempFilePrefix) {
			t.Fatalf("unexpected temporary file found after concurrent test: %s", e.Name())
		}
	}
}

func TestDownloader_CleanupStaleTempFiles(t *testing.T) {
	destDir := t.TempDir()

	// Create 3 fake stale temp files
	stale1 := filepath.Join(destDir, fmt.Sprintf("%sstale-1.replay", TempFilePrefix))
	stale2 := filepath.Join(destDir, fmt.Sprintf("%sstale-2.replay", TempFilePrefix))
	stale3 := filepath.Join(destDir, fmt.Sprintf("%sstale-3.replay", TempFilePrefix))
	// Create 1 valid replay file
	validFile := filepath.Join(destDir, "valid-match.replay")
	// Create 1 irrelevant file
	otherFile := filepath.Join(destDir, "config.yaml")

	_ = os.WriteFile(stale1, []byte("stale"), 0644)
	_ = os.WriteFile(stale2, []byte("stale"), 0644)
	_ = os.WriteFile(stale3, []byte("stale"), 0644)
	_ = os.WriteFile(validFile, []byte("valid"), 0644)
	_ = os.WriteFile(otherFile, []byte("yaml"), 0644)

	removed, err := CleanupStaleTempFiles(destDir)
	if err != nil {
		t.Fatalf("CleanupStaleTempFiles failed: %v", err)
	}
	if removed != 3 {
		t.Fatalf("expected 3 stale files removed, got %d", removed)
	}

	// Verify valid and other files are still present
	if _, err := os.Stat(validFile); os.IsNotExist(err) {
		t.Fatal("valid file was incorrectly deleted")
	}
	if _, err := os.Stat(otherFile); os.IsNotExist(err) {
		t.Fatal("irrelevant file was incorrectly deleted")
	}
	if _, err := os.Stat(stale1); !os.IsNotExist(err) {
		t.Fatal("stale1 file was not deleted")
	}
}
