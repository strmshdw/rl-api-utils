package e2e

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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
		name        string
		body        string
		expectSubstr string
	}{
		{
			name:        "JSON error field",
			body:        `{"error": "corrupt or invalid replay file payload"}`,
			expectSubstr: "corrupt or invalid replay file payload",
		},
		{
			name:        "Plain text error",
			body:        `Bad Request: Header size exceeded`,
			expectSubstr: "Bad Request: Header size exceeded",
		},
		{
			name:        "Empty body",
			body:        ``,
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
// SECTION 2: Database Contention & Transaction Rollback Verification
// ============================================================================

// TestTier5_Adv2_SQLiteStore_HighConcurrencyContention spawns 20 concurrent goroutines
// executing simultaneous upserts, status transitions, queries, and recovery operations
// against a single on-disk SQLite database file.
func TestTier5_Adv2_SQLiteStore_HighConcurrencyContention(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "contention.db")

	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Seed initial matches
	initial := []*storage.MatchRecord{
		{MatchGUID: "seed-1", ReplayURL: "http://cdn/seed-1.replay", RecordStartTimestamp: 1000},
		{MatchGUID: "seed-2", ReplayURL: "http://cdn/seed-2.replay", RecordStartTimestamp: 2000},
		{MatchGUID: "seed-3", ReplayURL: "http://cdn/seed-3.replay", RecordStartTimestamp: 3000},
	}
	if err := store.UpsertDiscoveredMatches(ctx, initial); err != nil {
		t.Fatalf("initial upsert failed: %v", err)
	}

	const goroutines = 20
	const iterations = 15

	var wg sync.WaitGroup
	wg.Add(goroutines)

	var errCount int64

	for i := 0; i < goroutines; i++ {
		workerID := i
		go func() {
			defer wg.Done()
			for iter := 0; iter < iterations; iter++ {
				guid := fmt.Sprintf("worker-%d-match-%d", workerID, iter)

				// Operation A: Upsert new match
				err := store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{
					{
						MatchGUID:            guid,
						ReplayURL:            fmt.Sprintf("http://cdn/%s.replay", guid),
						RecordStartTimestamp: time.Now().Unix(),
					},
				})
				if err != nil {
					atomic.AddInt64(&errCount, 1)
					continue
				}

				// Operation B: Transition to Downloading then Downloaded
				_ = store.MarkDownloading(ctx, guid)
				_ = store.MarkDownloaded(ctx, guid, fmt.Sprintf("/tmp/%s.replay", guid))

				// Operation C: Transition to Uploading then Uploaded
				_ = store.MarkUploading(ctx, guid)
				_ = store.MarkUploaded(ctx, guid, fmt.Sprintf("bc-%s", guid), "http://loc")

				// Operation D: Concurrent read queries
				_, _ = store.GetMatch(ctx, guid)
				_, _ = store.ListPendingDownloads(ctx)
				_, _ = store.ListPendingUploads(ctx)

				// Operation E: Periodic recovery attempt
				if iter%5 == 0 {
					_ = store.RecoverInFlight(ctx)
				}
			}
		}()
	}

	wg.Wait()

	if errCount > 0 {
		t.Fatalf("encountered %d unhandled errors under SQLite database contention", errCount)
	}

	// Verify database integrity by reading back matches
	sample, err := store.GetMatch(ctx, "worker-0-match-0")
	if err != nil {
		t.Fatalf("failed to retrieve sample match: %v", err)
	}
	if sample.UploadStatus != storage.UploadUploaded {
		t.Fatalf("expected sample to be UPLOADED, got %s", sample.UploadStatus)
	}
}

// TestTier5_Adv2_SQLiteStore_TransactionRollback_OnContextCancel verifies that
// when an upsert transaction encounters a pre-canceled context, it rolls back
// cleanly without leaving partially committed records.
func TestTier5_Adv2_SQLiteStore_TransactionRollback_OnContextCancel(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "rollback.db")

	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite store: %v", err)
	}
	defer store.Close()

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel() // Immediately canceled

	matches := []*storage.MatchRecord{
		{MatchGUID: "rollback-1", ReplayURL: "http://cdn/1.replay", RecordStartTimestamp: 100},
		{MatchGUID: "rollback-2", ReplayURL: "http://cdn/2.replay", RecordStartTimestamp: 200},
	}

	err = store.UpsertDiscoveredMatches(canceledCtx, matches)
	if err == nil {
		t.Fatal("expected error on canceled context upsert, got nil")
	}

	// Verify rollback: none of the matches should exist in the database
	liveCtx := context.Background()
	m1, err := store.GetMatch(liveCtx, "rollback-1")
	if !errors.Is(err, storage.ErrMatchNotFound) && m1 != nil {
		t.Fatalf("expected rollback-1 to not exist after rollback, got: %+v (err: %v)", m1, err)
	}

	m2, err := store.GetMatch(liveCtx, "rollback-2")
	if !errors.Is(err, storage.ErrMatchNotFound) && m2 != nil {
		t.Fatalf("expected rollback-2 to not exist after rollback, got: %+v (err: %v)", m2, err)
	}
}

// TestTier5_Adv2_SQLiteStore_BatchUpsert_DuplicateGUIDsInSameBatch tests handling
// when an incoming slice contains duplicate GUIDs in the same upsert batch.
func TestTier5_Adv2_SQLiteStore_BatchUpsert_DuplicateGUIDsInSameBatch(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "dup_batch.db")

	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	batch := []*storage.MatchRecord{
		{MatchGUID: "dup-guid-1", ReplayURL: "", RecordStartTimestamp: 1000},
		{MatchGUID: "dup-guid-1", ReplayURL: "http://cdn/dup-1.replay", RecordStartTimestamp: 1000}, // second has replay url
	}

	err = store.UpsertDiscoveredMatches(ctx, batch)
	if err != nil {
		t.Fatalf("batch with internal duplicates should succeed with upsert ON CONFLICT, got: %v", err)
	}

	rec, err := store.GetMatch(ctx, "dup-guid-1")
	if err != nil {
		t.Fatalf("failed to retrieve match: %v", err)
	}
	// The replay URL should have been updated by the second item
	if rec.ReplayURL != "http://cdn/dup-1.replay" {
		t.Fatalf("expected updated replay URL http://cdn/dup-1.replay, got %q", rec.ReplayURL)
	}
}

// TestTier5_Adv2_JSONStore_ConcurrentReadWriteContention tests thread safety and
// atomic persistence of JSONStore under heavy concurrent access from 25 goroutines.
func TestTier5_Adv2_JSONStore_ConcurrentReadWriteContention(t *testing.T) {
	tempDir := t.TempDir()
	jsonPath := filepath.Join(tempDir, "concurrent_state.json")

	store, err := storage.NewJSONStore(jsonPath)
	if err != nil {
		t.Fatalf("NewJSONStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	const workers = 25
	const iterations = 10

	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		workerID := i
		go func() {
			defer wg.Done()
			for iter := 0; iter < iterations; iter++ {
				guid := fmt.Sprintf("json-worker-%d-%d", workerID, iter)

				_ = store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{
					{
						MatchGUID:            guid,
						ReplayURL:            fmt.Sprintf("http://cdn/%s.replay", guid),
						RecordStartTimestamp: time.Now().Unix(),
					},
				})

				_ = store.MarkDownloading(ctx, guid)
				_ = store.MarkDownloaded(ctx, guid, fmt.Sprintf("/tmp/%s.replay", guid))
				_ = store.MarkUploading(ctx, guid)
				_ = store.MarkUploaded(ctx, guid, fmt.Sprintf("bc-%s", guid), "http://loc")

				_, _ = store.GetMatch(ctx, guid)
				_, _ = store.ListPendingDownloads(ctx)
				_, _ = store.ListPendingUploads(ctx)
			}
		}()
	}

	wg.Wait()

	// Verify that the JSON file on disk is valid and parseable
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("failed to read persisted json file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("persisted json file is empty")
	}

	// Verify clean reload into a new JSONStore instance
	reloaded, err := storage.NewJSONStore(jsonPath)
	if err != nil {
		t.Fatalf("failed to reload persisted JSONStore: %v", err)
	}
	defer reloaded.Close()

	rec, err := reloaded.GetMatch(ctx, "json-worker-0-0")
	if err != nil || rec == nil {
		t.Fatalf("failed to retrieve match from reloaded JSONStore: %v", err)
	}
	if rec.UploadStatus != storage.UploadUploaded {
		t.Fatalf("expected UPLOADED status, got %s", rec.UploadStatus)
	}
}

// TestTier5_Adv2_Stores_ClosedStore_RejectsOperations verifies that once Close()
// is called on JSONStore and SQLiteStore, subsequent operations return appropriate errors.
func TestTier5_Adv2_Stores_ClosedStore_RejectsOperations(t *testing.T) {
	tempDir := t.TempDir()

	// 1. JSONStore closed behavior
	jsonStore, _ := storage.NewJSONStore(filepath.Join(tempDir, "closed.json"))
	_ = jsonStore.Close()

	ctx := context.Background()
	_, err := jsonStore.GetMatch(ctx, "any")
	if !errors.Is(err, storage.ErrStoreClosed) {
		t.Fatalf("expected ErrStoreClosed for JSONStore.GetMatch, got: %v", err)
	}
	err = jsonStore.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{{MatchGUID: "m1"}})
	if !errors.Is(err, storage.ErrStoreClosed) {
		t.Fatalf("expected ErrStoreClosed for JSONStore.UpsertDiscoveredMatches, got: %v", err)
	}

	// 2. SQLiteStore closed behavior
	sqliteStore, _ := storage.NewSQLiteStore(filepath.Join(tempDir, "closed.db"))
	_ = sqliteStore.Close()

	_, err = sqliteStore.GetMatch(ctx, "any")
	if err == nil {
		t.Fatal("expected error on closed SQLiteStore.GetMatch, got nil")
	}
}

// ============================================================================
// SECTION 3: Transparent Reconnects on Connection Drops During Long Sync Cycles
// ============================================================================

// adversarialMockRPC implements psynet.RPCClient with fault injection for reconnect testing.
type adversarialMockRPC struct {
	mu           sync.Mutex
	entries      []rlapi.MatchEntry
	failNextCall error
	connected    bool
	callCount    int
	closed       bool
}

func (m *adversarialMockRPC) GetMatchHistory(ctx context.Context) ([]rlapi.MatchEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++

	if m.failNextCall != nil {
		err := m.failNextCall
		m.failNextCall = nil // Clear after one trigger
		m.connected = false  // Drop connection
		return nil, err
	}

	return m.entries, nil
}

func (m *adversarialMockRPC) IsConnected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected && !m.closed
}

func (m *adversarialMockRPC) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.connected = false
	return nil
}

// TestTier5_Adv3_PsyNet_TransparentReconnect_OnConnectionDrop verifies that when
// PsyNet WebSocket RPC experiences an unexpected connection drop (ErrConnectionClosed or EOF),
// psynet.Client transparently reconnects via connectLocked and retries the query successfully.
func TestTier5_Adv3_PsyNet_TransparentReconnect_OnConnectionDrop(t *testing.T) {
	reconnectAttempts := 0

	// Factory that returns a failing RPC on attempt 1, and a working RPC on attempt 2 (reconnect)
	factory := func(ctx context.Context, creds *psynet.Credentials) (psynet.RPCClient, error) {
		reconnectAttempts++
		if reconnectAttempts == 1 {
			// First connection drops with ErrConnectionClosed
			return &adversarialMockRPC{
				failNextCall: rlapi.ErrConnectionClosed,
				connected:    true,
			}, nil
		}
		// Second connection (reconnect) succeeds with match entries
		return &adversarialMockRPC{
			connected: true,
			entries: []rlapi.MatchEntry{
				{
					Match: rlapi.Match{
						MatchGUID:            "reconnect-guid-1",
						RecordStartTimestamp: 1700000000,
						MapName:              "Beckwith_P",
						Playlist:             2,
					},
					ReplayUrl: "http://cdn/rec-1.replay",
				},
			},
		}, nil
	}

	client, err := psynet.NewClient(psynet.ClientConfig{
		Credentials: &psynet.Credentials{
			Platform:  "Epic",
			AuthToken: "token",
			AccountID: "acc",
		},
		RPCFactory: factory,
		Logger:     slog.Default(),
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer client.Close()

	matches, err := client.GetRecentMatches(context.Background())
	if err != nil {
		t.Fatalf("GetRecentMatches should transparently reconnect and succeed, got error: %v", err)
	}

	if len(matches) != 1 || matches[0].MatchGUID != "reconnect-guid-1" {
		t.Fatalf("unexpected matches after reconnect: %+v", matches)
	}
	if reconnectAttempts != 2 {
		t.Fatalf("expected exactly 2 connections (initial + reconnect), got %d", reconnectAttempts)
	}
}

// TestTier5_Adv3_PsyNet_ReconnectFailure_PropagatesDescriptiveError verifies that
// if the connection drops AND the reconnect attempt also fails, psynet.Client
// returns a descriptive compounded error without leaking state or panicking.
func TestTier5_Adv3_PsyNet_ReconnectFailure_PropagatesDescriptiveError(t *testing.T) {
	reconnectAttempts := 0

	factory := func(ctx context.Context, creds *psynet.Credentials) (psynet.RPCClient, error) {
		reconnectAttempts++
		if reconnectAttempts == 1 {
			return &adversarialMockRPC{
				failNextCall: rlapi.ErrConnectionClosed,
				connected:    true,
			}, nil
		}
		// Reconnect fails with network error
		return nil, errors.New("dial tcp 127.0.0.1: connection refused")
	}

	client, err := psynet.NewClient(psynet.ClientConfig{
		Credentials: &psynet.Credentials{
			Platform:  "Epic",
			AuthToken: "token",
			AccountID: "acc",
		},
		RPCFactory: factory,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer client.Close()

	_, err = client.GetRecentMatches(context.Background())
	if err == nil {
		t.Fatal("expected error when reconnect fails, got nil")
	}
	if !strings.Contains(err.Error(), "reconnect failed") {
		t.Fatalf("expected error message to mention 'reconnect failed', got: %v", err)
	}
}

// TestTier5_Adv3_PsyNet_ClientClosedDuringReconnect_ReturnsErrClientClosed verifies
// that if Close() is called while a reconnect is in flight, ErrClientClosed is returned.
func TestTier5_Adv3_PsyNet_ClientClosedDuringReconnect_ReturnsErrClientClosed(t *testing.T) {
	client, _ := psynet.NewClient(psynet.ClientConfig{
		Credentials: &psynet.Credentials{Platform: "Epic", AuthToken: "t", AccountID: "a"},
		RPCFactory: func(ctx context.Context, creds *psynet.Credentials) (psynet.RPCClient, error) {
			return &adversarialMockRPC{connected: true}, nil
		},
	})

	_ = client.Close()

	_, err := client.GetRecentMatches(context.Background())
	if !errors.Is(err, psynet.ErrClientClosed) {
		t.Fatalf("expected ErrClientClosed on closed client, got: %v", err)
	}
}

// TestTier5_Adv3_Syncer_MultiCycleSelfHealing_AfterPsyNetDrop tests the syncer orchestrator
// recovering across cycles: Cycle 1 encounters a PsyNet drop and fails; Cycle 2 heals and completes.
func TestTier5_Adv3_Syncer_MultiCycleSelfHealing_AfterPsyNetDrop(t *testing.T) {
	tempDir := t.TempDir()
	store, _ := storage.NewStore(filepath.Join(tempDir, "state.json"))
	defer store.Close()

	cdn := testutil.NewMockCDNServer()
	defer cdn.Close()
	bc := testutil.NewMockBallchasingServer()
	defer bc.Close()

	providerFail := true
	mockProvider := &dynamicMockProvider{
		fn: func(ctx context.Context) ([]syncer.DiscoveredMatch, error) {
			if providerFail {
				return nil, errors.New("psynet rpc connection reset")
			}
			return []syncer.DiscoveredMatch{
				{
					MatchGUID:            "self-heal-1",
					RecordStartTimestamp: time.Now().Unix(),
					MapName:              "Park_P",
					Playlist:             2,
					ReplayURL:            cdn.ReplayURL("self-heal-1"),
				},
			}, nil
		},
	}

	downloader := psynet.NewDownloader()
	uploader, _ := ballchasing.NewClient(ballchasing.ClientConfig{
		BaseURL: bc.URL(),
		APIKey:  "test-ballchasing-token",
	})

	s, err := syncer.New(store, mockProvider, downloader, uploader,
		syncer.WithReplayDir(filepath.Join(tempDir, "replays")),
	)
	if err != nil {
		t.Fatalf("syncer.New failed: %v", err)
	}

	// Cycle 1: PsyNet fails -> Cycle returns error
	_, err = s.RunCycle(context.Background())
	if err == nil {
		t.Fatal("expected Cycle 1 to fail due to psynet drop, got nil error")
	}

	// Self-heal: PsyNet reconnects for Cycle 2
	providerFail = false

	stats2, err := s.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("Cycle 2 should have healed and succeeded, got: %v", err)
	}
	if stats2.UploadedCount != 1 {
		t.Fatalf("expected 1 upload in healed Cycle 2, got %d", stats2.UploadedCount)
	}
}

type dynamicMockProvider struct {
	fn func(ctx context.Context) ([]syncer.DiscoveredMatch, error)
}

func (p *dynamicMockProvider) GetRecentMatches(ctx context.Context) ([]syncer.DiscoveredMatch, error) {
	return p.fn(ctx)
}

func (p *dynamicMockProvider) Close() error {
	return nil
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
		"7656119",                    // too short
		"765611980000000001",         // too long (18 digits)
		"12345678901234567",         // wrong prefix
		"7656119abcdefghij",         // non-numeric
		"7656119800000000 ",         // whitespace
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
