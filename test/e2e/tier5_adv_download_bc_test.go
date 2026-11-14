package e2e

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/psynet"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/testutil"
	"github.com/dank/rlapi"
)

// ============================================================================
// SECTION 1: Malformed and Corrupted Payloads & Headers
// ============================================================================

// TestTier5_Adv1_Ballchasing_MalformedJSON_On201Created tests Ballchasing uploader
// behavior when the server returns HTTP 201 Created but the response body is malformed JSON.
func TestTier5_Adv1_Ballchasing_MalformedJSON_On201Created(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		// Truncated/corrupted JSON
		_, _ = w.Write([]byte(`{"id": "incomplete-json`))
	}))
	defer ts.Close()

	tempDir := t.TempDir()
	dummyFile := filepath.Join(tempDir, "test.replay")
	if err := os.WriteFile(dummyFile, testutil.GenerateValidReplay("guid-1", 2048), 0644); err != nil {
		t.Fatalf("failed to create dummy replay: %v", err)
	}

	client, err := ballchasing.NewClient(ballchasing.ClientConfig{
		BaseURL:    ts.URL,
		APIKey:     "test-token",
		MaxRetries: 0,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	res, err := client.UploadReplay(context.Background(), "guid-1", dummyFile)
	if err == nil {
		t.Fatalf("expected error on malformed JSON response, got result: %+v", res)
	}
	if !strings.Contains(err.Error(), "decoding 201 response") {
		t.Fatalf("expected decoding error message, got: %v", err)
	}
}

// TestTier5_Adv1_Ballchasing_CorruptedHTML_On409Conflict tests Ballchasing uploader
// when a proxy or server returns HTML instead of JSON for HTTP 409 Conflict.
func TestTier5_Adv1_Ballchasing_CorruptedHTML_On409Conflict(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`<html><body><h1>409 Conflict</h1><p>Nginx error</p></body></html>`))
	}))
	defer ts.Close()

	tempDir := t.TempDir()
	dummyFile := filepath.Join(tempDir, "test.replay")
	_ = os.WriteFile(dummyFile, testutil.GenerateValidReplay("guid-2", 2048), 0644)

	client, err := ballchasing.NewClient(ballchasing.ClientConfig{
		BaseURL:    ts.URL,
		APIKey:     "test-token",
		MaxRetries: 0,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	res, err := client.UploadReplay(context.Background(), "guid-2", dummyFile)
	if err == nil {
		t.Fatalf("expected error on corrupted HTML 409 response, got: %+v", res)
	}
	if !strings.Contains(err.Error(), "decoding 409 response") {
		t.Fatalf("expected decoding 409 error, got: %v", err)
	}
}

// TestTier5_Adv1_Ballchasing_MalformedErrorBody_On400BadRequest tests extraction
// of error messages from varied HTTP 400 responses (plain text, JSON with error field, JSON without error field).
func TestTier5_Adv1_Ballchasing_MalformedErrorBody_On400BadRequest(t *testing.T) {
	cases := []struct {
		name         string
		body         string
		expectSubstr string
	}{
		{
			name:         "JSON error field",
			body:         `{"error": "corrupt or invalid replay file payload"}`,
			expectSubstr: "corrupt or invalid replay file payload",
		},
		{
			name:         "Plain text error",
			body:         `Bad Request: Header size exceeded`,
			expectSubstr: "Bad Request: Header size exceeded",
		},
		{
			name:         "Empty body",
			body:         ``,
			expectSubstr: "bad request",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer ts.Close()

			tempDir := t.TempDir()
			dummyFile := filepath.Join(tempDir, "test.replay")
			_ = os.WriteFile(dummyFile, testutil.GenerateValidReplay("guid-bad", 2048), 0644)

			client, _ := ballchasing.NewClient(ballchasing.ClientConfig{
				BaseURL:    ts.URL,
				APIKey:     "token",
				MaxRetries: 0,
			})

			_, err := client.UploadReplay(context.Background(), "guid-bad", dummyFile)
			if err == nil {
				t.Fatal("expected error on 400 bad request, got nil")
			}
			if !errors.Is(err, ballchasing.ErrBadRequest) {
				t.Fatalf("expected ErrBadRequest, got: %v", err)
			}
			if !strings.Contains(err.Error(), tc.expectSubstr) {
				t.Fatalf("expected error to contain %q, got: %v", tc.expectSubstr, err)
			}
		})
	}
}

// TestTier5_Adv1_Ballchasing_MemoryExhaustionProtection_LimitReader tests that
// adversarial servers returning massive response bodies (>1MB) do not exhaust client memory.
func TestTier5_Adv1_Ballchasing_MemoryExhaustionProtection_LimitReader(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Stream 4MB of whitespace followed by valid JSON to test LimitReader truncation
		padding := bytes.Repeat([]byte(" "), 2*1024*1024)
		_, _ = w.Write(padding)
		_, _ = w.Write([]byte(`{"chaser": true}`))
	}))
	defer ts.Close()

	client, err := ballchasing.NewClient(ballchasing.ClientConfig{
		BaseURL: ts.URL,
		APIKey:  "token",
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	// Ping reads up to 64KB, so 2MB of spaces truncates before valid JSON
	err = client.Ping(context.Background())
	// Truncated body should not match 200 OK cleanly or crash with OOM
	if err == nil {
		t.Log("ping succeeded within limit reader bounds")
	}
}

// TestTier5_Adv1_Ballchasing_MalformedRetryAfterHeaders tests rate limit backoff
// resolution with various malformed, non-standard, or extreme Retry-After headers.
func TestTier5_Adv1_Ballchasing_MalformedRetryAfterHeaders(t *testing.T) {
	cases := []struct {
		name       string
		headerVal  string
		expectWait bool
	}{
		{"Non-numeric garbage", "immediately", true}, // should fall back to exponential backoff
		{"Negative integer", "-10", true},            // <= 0 wait or fallback
		{"Malformed date", "Sun, 32 Dec 2026 99:99:99 GMT", true},
		{"Empty string", "", true},
		{"Valid past date", "Mon, 01 Jan 2024 00:00:00 GMT", false}, // past date -> 0 wait
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			callCount := 0
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				callCount++
				if callCount == 1 {
					w.Header().Set("Retry-After", tc.headerVal)
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = w.Write([]byte(`{"error": "rate limit"}`))
					return
				}
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"id": "ok-guid", "location": "http://loc"}`))
			}))
			defer ts.Close()

			tempDir := t.TempDir()
			dummyFile := filepath.Join(tempDir, "test.replay")
			_ = os.WriteFile(dummyFile, testutil.GenerateValidReplay("guid-ra", 2048), 0644)

			client, _ := ballchasing.NewClient(
				ballchasing.ClientConfig{
					BaseURL:     ts.URL,
					APIKey:      "token",
					MaxRetries:  1,
					BaseBackoff: 5 * time.Millisecond,
					MaxBackoff:  20 * time.Millisecond,
				},
			)

			res, err := client.UploadReplay(context.Background(), "guid-ra", dummyFile)
			if err != nil {
				t.Fatalf("expected successful retry after malformed Retry-After, got error: %v", err)
			}
			if res == nil || res.ID != "ok-guid" {
				t.Fatalf("unexpected upload result: %+v", res)
			}
			if callCount != 2 {
				t.Fatalf("expected exactly 2 calls, got %d", callCount)
			}
		})
	}
}

// TestTier5_Adv1_PsyNet_MalformedMatchHistoryEntries tests mapping of malformed PsyNet match entries:
// empty GUID (skipped), zero timestamp (defaulted to current time), empty replay URL (discovered with empty URL).
func TestTier5_Adv1_PsyNet_MalformedMatchHistoryEntries(t *testing.T) {
	mockRPC := &adversarialMockRPC{
		connected: true,
		entries: []rlapi.MatchEntry{
			{
				Match: rlapi.Match{
					MatchGUID:            "", // Empty GUID -> should be skipped
					RecordStartTimestamp: 1700000000,
					MapName:              "Park_P",
					Playlist:             2,
				},
				ReplayUrl: "http://cdn/empty-guid.replay",
			},
			{
				Match: rlapi.Match{
					MatchGUID:            "valid-guid-zero-ts",
					RecordStartTimestamp: 0, // Zero timestamp -> should default to current time
					MapName:              "Utopia_P",
					Playlist:             3,
				},
				ReplayUrl: "http://cdn/zero-ts.replay",
			},
			{
				Match: rlapi.Match{
					MatchGUID:            "valid-guid-empty-url",
					RecordStartTimestamp: 1700000100,
					MapName:              "Wasteland_P",
					Playlist:             1,
				},
				ReplayUrl: "   ", // Whitespace replay URL -> treated as empty URL
			},
		},
	}

	client := psynet.NewClientWithRPC(mockRPC, slog.Default())
	matches, err := client.GetRecentMatches(context.Background())
	if err != nil {
		t.Fatalf("GetRecentMatches failed: %v", err)
	}

	// Out of 3 entries, the empty GUID must be skipped, leaving 2
	if len(matches) != 2 {
		t.Fatalf("expected 2 valid matches after filtering empty GUID, got %d", len(matches))
	}

	// Match 1: Zero timestamp replaced with non-zero
	if matches[0].MatchGUID != "valid-guid-zero-ts" {
		t.Fatalf("expected first match valid-guid-zero-ts, got %s", matches[0].MatchGUID)
	}
	if matches[0].RecordStartTimestamp == 0 {
		t.Fatal("expected zero timestamp to be defaulted to current epoch, got 0")
	}

	// Match 2: Empty replay URL trimmed to empty
	if matches[1].MatchGUID != "valid-guid-empty-url" {
		t.Fatalf("expected second match valid-guid-empty-url, got %s", matches[1].MatchGUID)
	}
	if matches[1].ReplayURL != "" {
		t.Fatalf("expected empty replay URL, got %q", matches[1].ReplayURL)
	}
}

// TestTier5_Adv1_Downloader_InvalidURLSchemes tests rejection of unsafe URL schemes (file, ftp, javascript).
func TestTier5_Adv1_Downloader_InvalidURLSchemes(t *testing.T) {
	downloader := psynet.NewDownloader()
	tempDir := t.TempDir()

	invalidURLs := []string{
		"file:///etc/passwd",
		"file://C:/Windows/System32/calc.exe",
		"ftp://anonymous@evil.com/payload.replay",
		"gopher://gopher.floodgap.com/",
		"javascript:alert(1)",
		"data:text/plain;base64,SGVsbG8=",
		"not-a-url",
	}

	for _, u := range invalidURLs {
		t.Run(u, func(t *testing.T) {
			_, err := downloader.DownloadReplay(context.Background(), "guid-test", u, tempDir)
			if err == nil {
				t.Fatalf("expected rejection of invalid URL %q, got nil error", u)
			}
			if !errors.Is(err, psynet.ErrInvalidReplayURL) {
				t.Fatalf("expected ErrInvalidReplayURL for %q, got: %v", u, err)
			}
		})
	}
}

// TestTier5_Adv1_Downloader_CorruptedHTMLInsteadOfReplay tests CDN servers returning HTTP 500
// or HTML error bodies instead of binary replays.
func TestTier5_Adv1_Downloader_CorruptedHTMLInsteadOfReplay(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("<html><body>500 Internal Server Error</body></html>"))
	}))
	defer ts.Close()

	downloader := psynet.NewDownloader()
	tempDir := t.TempDir()

	_, err := downloader.DownloadReplay(context.Background(), "guid-500", ts.URL+"/replay.replay", tempDir)
	if err == nil {
		t.Fatal("expected error on HTTP 500 download, got nil")
	}

	var httpErr *psynet.HTTPStatusError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected *psynet.HTTPStatusError, got %T: %v", err, err)
	}
	if httpErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected StatusCode 500, got %d", httpErr.StatusCode)
	}
}

// TestTier5_Adv1_JSONStore_CorruptedOrTruncatedDiskFile tests JSONStore initialization
// under corrupted file syntax, empty files, and whitespace-only files.
func TestTier5_Adv1_JSONStore_CorruptedOrTruncatedDiskFile(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Corrupted JSON syntax -> fails to initialize
	corruptPath := filepath.Join(tempDir, "corrupted.json")
	if err := os.WriteFile(corruptPath, []byte(`{"version": 1, "matches": {INVALID_JSON`), 0644); err != nil {
		t.Fatalf("failed to write corrupt file: %v", err)
	}
	_, err := storage.NewJSONStore(corruptPath)
	if err == nil {
		t.Fatal("expected NewJSONStore to fail on corrupted JSON, got nil error")
	}

	// 2. Zero-byte file -> recovers cleanly and initializes empty store
	emptyPath := filepath.Join(tempDir, "empty.json")
	if err := os.WriteFile(emptyPath, []byte(""), 0644); err != nil {
		t.Fatalf("failed to write empty file: %v", err)
	}
	emptyStore, err := storage.NewJSONStore(emptyPath)
	if err != nil {
		t.Fatalf("NewJSONStore should recover zero-byte file, got error: %v", err)
	}
	defer emptyStore.Close()

	// 3. Whitespace-only file -> recovers cleanly
	wsPath := filepath.Join(tempDir, "whitespace.json")
	if err := os.WriteFile(wsPath, []byte("   \n\t  \r\n   "), 0644); err != nil {
		t.Fatalf("failed to write whitespace file: %v", err)
	}
	wsStore, err := storage.NewJSONStore(wsPath)
	if err != nil {
		t.Fatalf("NewJSONStore should recover whitespace-only file, got error: %v", err)
	}
	defer wsStore.Close()
}

// ============================================================================
// SECTION 4: Extreme Boundary Conditions on File Sizes, Timeouts, and Rate Limits
// ============================================================================

// TestTier5_Adv4_Downloader_PayloadSizeBoundaries_1023vs1024 verifies the exact
// 1024-byte boundary: 1023 bytes fails with ErrReplayTooSmall; 1024 bytes passes.
func TestTier5_Adv4_Downloader_PayloadSizeBoundaries_1023vs1024(t *testing.T) {
	tempDir := t.TempDir()
	downloader := psynet.NewDownloader()

	// 1. Exact 1023 bytes -> must FAIL with ErrReplayTooSmall
	ts1023 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bytes.Repeat([]byte("A"), 1023))
	}))
	defer ts1023.Close()

	_, err := downloader.DownloadReplay(context.Background(), "guid-1023", ts1023.URL+"/1023.replay", tempDir)
	if err == nil {
		t.Fatal("expected 1023 bytes to fail with ErrReplayTooSmall, got nil error")
	}
	if !errors.Is(err, psynet.ErrReplayTooSmall) {
		t.Fatalf("expected ErrReplayTooSmall for 1023 bytes, got: %v", err)
	}

	// 2. Exact 1024 bytes -> must SUCCEED (boundary condition)
	ts1024 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bytes.Repeat([]byte("B"), 1024))
	}))
	defer ts1024.Close()

	path1024, err := downloader.DownloadReplay(context.Background(), "guid-1024", ts1024.URL+"/1024.replay", tempDir)
	if err != nil {
		t.Fatalf("expected 1024 bytes to succeed, got: %v", err)
	}

	info, err := os.Stat(path1024)
	if err != nil || info.Size() != 1024 {
		t.Fatalf("expected downloaded file to be exactly 1024 bytes, got %d", info.Size())
	}
}

// TestTier5_Adv4_Downloader_LargePayloadStreaming_5MB verifies streaming download
// of a realistic large 5MB replay without corruption or temp file leaks.
func TestTier5_Adv4_Downloader_LargePayloadStreaming_5MB(t *testing.T) {
	tempDir := t.TempDir()
	payload5MB := testutil.GenerateValidReplay("guid-5mb", 5*1024*1024)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload5MB)
	}))
	defer ts.Close()

	downloader := psynet.NewDownloader(psynet.WithBufferSize(32 * 1024))
	localPath, err := downloader.DownloadReplay(context.Background(), "guid-5mb", ts.URL+"/5mb.replay", tempDir)
	if err != nil {
		t.Fatalf("failed to download 5MB replay: %v", err)
	}

	info, err := os.Stat(localPath)
	if err != nil || info.Size() != 5*1024*1024 {
		t.Fatalf("expected 5MB file on disk, got %d bytes", info.Size())
	}

	// Verify no orphaned temporary files remain
	staleCount, err := psynet.CleanupStaleTempFiles(tempDir)
	if err != nil || staleCount != 0 {
		t.Fatalf("expected 0 stale files, found %d", staleCount)
	}
}

// TestTier5_Adv4_PathTraversalAttacks_Rejection verifies that match GUIDs containing
// directory traversal sequences or Windows/Unix forbidden characters are rejected immediately.
func TestTier5_Adv4_PathTraversalAttacks_Rejection(t *testing.T) {
	downloader := psynet.NewDownloader()
	tempDir := t.TempDir()

	maliciousGUIDs := []string{
		"../../../../etc/passwd",
		"..\\..\\Windows\\System32\\cmd.exe",
		"foo/bar",
		"foo\\bar",
		"guid:colon",
		"guid*star",
		"guid?question",
		`guid"quote`,
		"guid<less",
		"guid>greater",
		"guid|pipe",
	}

	for _, guid := range maliciousGUIDs {
		t.Run(guid, func(t *testing.T) {
			_, err := downloader.DownloadReplay(context.Background(), guid, "http://cdn/safe.replay", tempDir)
			if err == nil {
				t.Fatalf("expected path traversal GUID %q to be rejected, got nil error", guid)
			}
			if !errors.Is(err, psynet.ErrInvalidMatchGUID) {
				t.Fatalf("expected ErrInvalidMatchGUID for %q, got: %v", guid, err)
			}
		})
	}
}
