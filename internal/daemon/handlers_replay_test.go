package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/session"
	"github.com/dank/rl-api-utils/internal/storage"
)

type stubReplayFetcher struct {
	downloadFn func(ctx context.Context, replayID, destPath string) (int64, error)
	findFn     func(ctx context.Context, q ballchasing.ReplaySearchQuery) (*ballchasing.ReplayItem, error)
	dlCalls    atomic.Int32
	findCalls  atomic.Int32
}

func (s *stubReplayFetcher) DownloadReplayFile(ctx context.Context, replayID, destPath string) (int64, error) {
	s.dlCalls.Add(1)
	if s.downloadFn != nil {
		return s.downloadFn(ctx, replayID, destPath)
	}
	return 0, ballchasing.ErrNotFound
}

func (s *stubReplayFetcher) FindReplayByMetadata(ctx context.Context, q ballchasing.ReplaySearchQuery) (*ballchasing.ReplayItem, error) {
	s.findCalls.Add(1)
	if s.findFn != nil {
		return s.findFn(ctx, q)
	}
	return nil, ballchasing.ErrNotFound
}

func setupReplayTestDaemon(t *testing.T, fetcher ReplayFetcher) (*Daemon, *storage.SQLiteStore, string) {
	t.Helper()
	tmpDir := t.TempDir()
	replayDir := filepath.Join(tmpDir, "replays")
	if err := os.MkdirAll(replayDir, 0o755); err != nil {
		t.Fatalf("mkdir replayDir: %v", err)
	}
	store, err := storage.NewSQLiteStore(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := config.NewDefaultConfig()
	cfg.Sync.ReplayDir = replayDir
	opts := []Option{WithStateStore(store)}
	if fetcher != nil {
		opts = append(opts, WithBallchasingClient(fetcher))
	}
	d, err := New(&dummySyncer{}, cfg, opts...)
	if err != nil {
		t.Fatalf("New daemon: %v", err)
	}
	return d, store, replayDir
}

func TestDaemon_ReplayStatusAndServe_LocalAndCache(t *testing.T) {
	ctx := context.Background()
	fetcher := &stubReplayFetcher{}
	d, store, replayDir := setupReplayTestDaemon(t, fetcher)
	h := d.Handler(ctx)

	localPayload := []byte("LOCAL_REPLAY_BINARY_DATA_001")
	localFile := filepath.Join(replayDir, "2026_11_27_001_2v2_WIN.replay")
	if err := os.WriteFile(localFile, localPayload, 0o644); err != nil {
		t.Fatalf("write local replay: %v", err)
	}

	guid := "match-local-001"
	if err := store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{{
		MatchGUID:            guid,
		RecordStartTimestamp: time.Now().UTC().Unix(),
		MapName:              "DFH Stadium",
		Playlist:             11,
		DownloadStatus:       storage.DownloadDownloaded,
		LocalFilePath:        localFile,
		UploadStatus:         storage.UploadUploaded,
		BallchasingID:        "bc-local-1",
		BallchasingURL:       "https://ballchasing.com/replay/bc-local-1",
	}}); err != nil {
		t.Fatalf("upsert match: %v", err)
	}
	_ = store.UpdateMatchReplayFile(ctx, guid, localFile, "2026_11_27_001_2v2_WIN.replay", "2v2")

	// 1. Status endpoint reports local source, size, and zero Ballchasing calls
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/matches/"+guid+"/replay/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d: %s", rec.Code, rec.Body.String())
	}
	var st ReplayAvailabilityResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if !st.Available || st.Source != "local" || !st.LocalAvailable || !st.BallchasingAvailable || !st.BallchasingConfigured {
		t.Errorf("unexpected status payload: %+v", st)
	}
	if st.SizeBytes != int64(len(localPayload)) {
		t.Errorf("size_bytes = %d, want %d", st.SizeBytes, len(localPayload))
	}
	if st.Filename != "2026_11_27_001_2v2_WIN.replay" {
		t.Errorf("filename = %q", st.Filename)
	}
	if fetcher.dlCalls.Load() != 0 || fetcher.findCalls.Load() != 0 {
		t.Errorf("expected zero Ballchasing calls for status check")
	}

	// 2. GET /replay serves local file inline with exposed CORS headers
	recGet := httptest.NewRecorder()
	h.ServeHTTP(recGet, httptest.NewRequest(http.MethodGet, "/api/matches/"+guid+"/replay", nil))
	if recGet.Code != http.StatusOK {
		t.Fatalf("GET replay = %d: %s", recGet.Code, recGet.Body.String())
	}
	if !bytes.Equal(recGet.Body.Bytes(), localPayload) {
		t.Fatalf("body mismatch")
	}
	if got := recGet.Header().Get("X-Replay-Source"); got != "local" {
		t.Errorf("X-Replay-Source = %q, want local", got)
	}
	if got := recGet.Header().Get("Cache-Control"); got != "private, max-age=3600" {
		t.Errorf("Cache-Control (local) = %q, want private, max-age=3600", got)
	}
	if got := recGet.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := recGet.Header().Get("Content-Disposition"); !strings.HasPrefix(got, `inline; filename="2026_11_27_001_2v2_WIN.replay"`) {
		t.Errorf("Content-Disposition = %q", got)
	}
	expose := recGet.Header().Get("Access-Control-Expose-Headers")
	for _, hdr := range []string{"X-Replay-Source", "X-Replay-Filename", "X-Replay-Ballchasing-ID", "Content-Length", "Retry-After"} {
		if !strings.Contains(expose, hdr) {
			t.Errorf("Access-Control-Expose-Headers = %q missing %s", expose, hdr)
		}
	}

	// 3. ?download=1 switches to attachment disposition
	recDL := httptest.NewRecorder()
	h.ServeHTTP(recDL, httptest.NewRequest(http.MethodGet, "/api/matches/"+guid+"/replay?download=1", nil))
	if got := recDL.Header().Get("Content-Disposition"); !strings.HasPrefix(got, `attachment; filename="2026_11_27_001_2v2_WIN.replay"`) {
		t.Errorf("Content-Disposition (?download=1) = %q", got)
	}

	// 4. HEAD returns headers with empty body
	recHead := httptest.NewRecorder()
	h.ServeHTTP(recHead, httptest.NewRequest(http.MethodHead, "/api/matches/"+guid+"/replay", nil))
	if recHead.Code != http.StatusOK || recHead.Body.Len() != 0 {
		t.Errorf("HEAD replay code=%d len=%d", recHead.Code, recHead.Body.Len())
	}

	// 5. Existing GET /api/matches/{guid} still routes properly
	recMatch := httptest.NewRecorder()
	h.ServeHTTP(recMatch, httptest.NewRequest(http.MethodGet, "/api/matches/"+guid, nil))
	if recMatch.Code != http.StatusOK {
		t.Fatalf("GET /api/matches/{guid} = %d", recMatch.Code)
	}
}

func TestDaemon_ReplayDownloadFromBallchasing_CachesSafelyAndSingleflights(t *testing.T) {
	ctx := context.Background()
	bcPayload := []byte("BALLCHASING_REPLAY_BINARY_PAYLOAD_XYZ")
	gate := make(chan struct{})
	started := make(chan struct{}, 1)

	fetcher := &stubReplayFetcher{
		downloadFn: func(ctx context.Context, replayID, destPath string) (int64, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			<-gate
			if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
				return 0, err
			}
			if err := os.WriteFile(destPath, bcPayload, 0o644); err != nil {
				return 0, err
			}
			return int64(len(bcPayload)), nil
		},
	}

	d, store, replayDir := setupReplayTestDaemon(t, fetcher)
	h := d.Handler(ctx)

	guid := "match-bc-remote-001"
	if err := store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{{
		MatchGUID:            guid,
		RecordStartTimestamp: time.Now().UTC().Unix(),
		Playlist:             11,
		DownloadStatus:       storage.DownloadSkipped,
		UploadStatus:         storage.UploadUploaded,
		BallchasingID:        "bc-uuid-777",
		BallchasingURL:       "https://ballchasing.com/replay/bc-uuid-777",
	}}); err != nil {
		t.Fatalf("upsert match: %v", err)
	}

	// HEAD must not trigger download and must not be cached as stale ballchasing source
	recHead := httptest.NewRecorder()
	h.ServeHTTP(recHead, httptest.NewRequest(http.MethodHead, "/api/matches/"+guid+"/replay", nil))
	if recHead.Code != http.StatusOK || recHead.Header().Get("X-Replay-Source") != "ballchasing" {
		t.Fatalf("HEAD remote replay code=%d source=%q", recHead.Code, recHead.Header().Get("X-Replay-Source"))
	}
	if cc := recHead.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Errorf("HEAD remote Cache-Control = %q, want private, no-store", cc)
	}
	if fetcher.dlCalls.Load() != 0 {
		t.Fatalf("HEAD erroneously triggered Ballchasing download")
	}

	// 6 concurrent GET requests must deduplicate into 1 Ballchasing download
	const workers = 6
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/matches/"+guid+"/replay", nil))
			if rec.Code != http.StatusOK {
				errs <- fmt.Errorf("status %d: %s", rec.Code, rec.Body.String())
				return
			}
			if !bytes.Equal(rec.Body.Bytes(), bcPayload) {
				errs <- errors.New("payload mismatch")
				return
			}
			src := rec.Header().Get("X-Replay-Source")
			if src != "ballchasing" && src != "cache" {
				errs <- fmt.Errorf("unexpected source %q", src)
				return
			}
			if src == "ballchasing" && rec.Header().Get("Cache-Control") != "private, no-store" {
				errs <- fmt.Errorf("fresh ballchasing response Cache-Control = %q, want private, no-store", rec.Header().Get("Cache-Control"))
			}
		}()
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for download start")
	}
	time.Sleep(30 * time.Millisecond)
	close(gate)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent request failed: %v", err)
	}

	if calls := fetcher.dlCalls.Load(); calls != 1 {
		t.Fatalf("expected exactly 1 singleflight Ballchasing download, got %d", calls)
	}

	// Subsequent request serves directly from cache without hitting Ballchasing
	recCached := httptest.NewRecorder()
	h.ServeHTTP(recCached, httptest.NewRequest(http.MethodGet, "/api/matches/"+guid+"/replay", nil))
	if recCached.Code != http.StatusOK || recCached.Header().Get("X-Replay-Source") != "cache" {
		t.Fatalf("cached fetch code=%d source=%q", recCached.Code, recCached.Header().Get("X-Replay-Source"))
	}
	if cc := recCached.Header().Get("Cache-Control"); cc != "private, max-age=3600" {
		t.Errorf("cached fetch Cache-Control = %q, want private, max-age=3600", cc)
	}
	if fetcher.dlCalls.Load() != 1 {
		t.Fatalf("cached fetch should not call Ballchasing again")
	}

	// Verify cache lives strictly under ballchasing-cache/ and DB sync state is UNTOUCHED
	cachePath := filepath.Join(replayDir, "ballchasing-cache", "bc-uuid-777.replay")
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("expected cache file at %s: %v", cachePath, err)
	}
	recAfter, err := store.GetMatch(ctx, guid)
	if err != nil {
		t.Fatalf("GetMatch after cache: %v", err)
	}
	if recAfter.DownloadStatus != storage.DownloadSkipped || recAfter.LocalFilePath != "" || recAfter.UploadStatus != storage.UploadUploaded {
		t.Errorf("viewer cache mutated syncer invariants: dl=%s path=%q up=%s", recAfter.DownloadStatus, recAfter.LocalFilePath, recAfter.UploadStatus)
	}
}

func TestDaemon_ReplayFallbackLookup_AndSessionOnlyMatch(t *testing.T) {
	ctx := context.Background()
	bcPayload := []byte("SESSION_MATCH_FROM_BALLCHASING")
	fetcher := &stubReplayFetcher{
		findFn: func(ctx context.Context, q ballchasing.ReplaySearchQuery) (*ballchasing.ReplayItem, error) {
			if strings.EqualFold(q.MatchGUID, "sess-only-guid-1") {
				return &ballchasing.ReplayItem{
					ID:   "bc-discovered-sess",
					Link: "https://ballchasing.com/replay/bc-discovered-sess",
				}, nil
			}
			if strings.EqualFold(q.MatchGUID, "db-missing-bc-id") {
				return &ballchasing.ReplayItem{
					ID:   "bc-discovered-db",
					Link: "https://ballchasing.com/replay/bc-discovered-db",
				}, nil
			}
			return nil, ballchasing.ErrNotFound
		},
		downloadFn: func(ctx context.Context, replayID, destPath string) (int64, error) {
			if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
				return 0, err
			}
			if err := os.WriteFile(destPath, bcPayload, 0o644); err != nil {
				return 0, err
			}
			return int64(len(bcPayload)), nil
		},
	}

	d, store, _ := setupReplayTestDaemon(t, fetcher)
	h := d.Handler(ctx)

	// Seed session-only match (not in matches table)
	sessMatchesJSON, _ := json.Marshal([]*session.SessionMatchDetail{{
		MatchGUID:   "sess-only-guid-1",
		PlaylistID:  11,
		StartedAt:   time.Now().UTC().Add(-5 * time.Minute),
		BlueScore:   4,
		OrangeScore: 1,
	}})
	if err := store.SaveSession(ctx, &storage.SessionRecord{
		SessionID:    "sess-1",
		StartedAt:    time.Now().UTC().Add(-10 * time.Minute),
		TotalMatches: 1,
		MatchesJSON:  string(sessMatchesJSON),
	}); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	// Status without ?lookup=true returns 200 with reason=not_synced_yet (zero remote calls)
	recSt := httptest.NewRecorder()
	h.ServeHTTP(recSt, httptest.NewRequest(http.MethodGet, "/api/matches/sess-only-guid-1/replay/status", nil))
	if recSt.Code != http.StatusOK {
		t.Fatalf("session match status = %d: %s", recSt.Code, recSt.Body.String())
	}
	var st ReplayAvailabilityResponse
	_ = json.Unmarshal(recSt.Body.Bytes(), &st)
	if st.Available || st.Reason != "not_synced_yet" {
		t.Errorf("unexpected session-only status: %+v", st)
	}
	if fetcher.findCalls.Load() != 0 {
		t.Errorf("default status should not call Ballchasing lookup")
	}

	// GET /replay for session-only match runs fallback lookup and downloads replay
	recGet := httptest.NewRecorder()
	h.ServeHTTP(recGet, httptest.NewRequest(http.MethodGet, "/api/matches/sess-only-guid-1/replay", nil))
	if recGet.Code != http.StatusOK || !bytes.Equal(recGet.Body.Bytes(), bcPayload) {
		t.Fatalf("session-only replay download failed: code=%d body=%s", recGet.Code, recGet.Body.String())
	}
	if recGet.Header().Get("X-Replay-Ballchasing-ID") != "bc-discovered-sess" {
		t.Errorf("X-Replay-Ballchasing-ID = %q", recGet.Header().Get("X-Replay-Ballchasing-ID"))
	}

	// Seed DB match without BallchasingID and verify ?lookup=true persists link non-destructively
	if err := store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{{
		MatchGUID:            "db-missing-bc-id",
		RecordStartTimestamp: time.Now().UTC().Unix(),
		Playlist:             11,
		DownloadStatus:       storage.DownloadSkipped,
		UploadStatus:         storage.UploadPending,
	}}); err != nil {
		t.Fatalf("upsert db match: %v", err)
	}

	recLookup := httptest.NewRecorder()
	h.ServeHTTP(recLookup, httptest.NewRequest(http.MethodGet, "/api/matches/db-missing-bc-id/replay/status?lookup=true", nil))
	if recLookup.Code != http.StatusOK {
		t.Fatalf("lookup status = %d", recLookup.Code)
	}
	var stLookup ReplayAvailabilityResponse
	_ = json.Unmarshal(recLookup.Body.Bytes(), &stLookup)
	if !stLookup.Available || stLookup.Source != "ballchasing" || stLookup.BallchasingID != "bc-discovered-db" {
		t.Errorf("unexpected lookup status: %+v", stLookup)
	}
	updated, err := store.GetMatch(ctx, "db-missing-bc-id")
	if err != nil {
		t.Fatalf("GetMatch: %v", err)
	}
	if updated.BallchasingID != "bc-discovered-db" || updated.UploadStatus != storage.UploadPending || updated.DownloadStatus != storage.DownloadSkipped {
		t.Errorf("expected non-destructive link persist, got %+v", updated)
	}
}

func TestDaemon_ReplayErrorMappings(t *testing.T) {
	ctx := context.Background()

	t.Run("missing API key returns 503", func(t *testing.T) {
		d, store, _ := setupReplayTestDaemon(t, nil)
		_ = store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{{
			MatchGUID:            "m-nokey",
			RecordStartTimestamp: time.Now().UTC().Unix(),
			BallchasingID:        "bc-nokey",
			UploadStatus:         storage.UploadUploaded,
		}})
		h := d.Handler(ctx)

		recSt := httptest.NewRecorder()
		h.ServeHTTP(recSt, httptest.NewRequest(http.MethodGet, "/api/matches/m-nokey/replay/status", nil))
		var st ReplayAvailabilityResponse
		_ = json.Unmarshal(recSt.Body.Bytes(), &st)
		if st.Available || st.Reason != "missing_ballchasing_key" || st.BallchasingConfigured {
			t.Errorf("unexpected no-key status: %+v", st)
		}

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/matches/m-nokey/replay", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("got %d, want 503", rec.Code)
		}
		var errResp replayErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
		if errResp.Code != "missing_ballchasing_api_key" {
			t.Errorf("code = %q", errResp.Code)
		}
	})

	cases := []struct {
		name       string
		dlErr      error
		wantStatus int
		wantCode   string
		wantRetry  int
	}{
		{"401 invalid key", ballchasing.ErrInvalidAPIKey, http.StatusBadGateway, "ballchasing_auth_failed", 0},
		{"403 forbidden", ballchasing.ErrForbidden, http.StatusForbidden, "ballchasing_forbidden", 0},
		{"404 not found", ballchasing.ErrNotFound, http.StatusNotFound, "ballchasing_not_found", 0},
		{"429 rate limited", &ballchasing.RateLimitError{RetryAfter: 9 * time.Second}, http.StatusTooManyRequests, "ballchasing_rate_limited", 9},
		{"500 upstream", ballchasing.ErrServerError, http.StatusBadGateway, "ballchasing_upstream_error", 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fetcher := &stubReplayFetcher{
				downloadFn: func(ctx context.Context, replayID, destPath string) (int64, error) {
					return 0, tc.dlErr
				},
			}
			d, store, _ := setupReplayTestDaemon(t, fetcher)
			_ = store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{{
				MatchGUID:            "m-err",
				RecordStartTimestamp: time.Now().UTC().Unix(),
				BallchasingID:        "bc-err",
			}})
			rec := httptest.NewRecorder()
			d.Handler(ctx).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/matches/m-err/replay", nil))
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			var errResp replayErrorResponse
			_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
			if errResp.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", errResp.Code, tc.wantCode)
			}
			if tc.wantRetry > 0 {
				if errResp.RetryAfterSeconds != tc.wantRetry {
					t.Errorf("retry_after_seconds = %d, want %d", errResp.RetryAfterSeconds, tc.wantRetry)
				}
				if hdr := rec.Header().Get("Retry-After"); hdr != fmt.Sprintf("%d", tc.wantRetry) {
					t.Errorf("Retry-After header = %q, want %d", hdr, tc.wantRetry)
				}
			}
		})
	}

	t.Run("unknown match returns 404 and blank guid returns 400", func(t *testing.T) {
		d, _, _ := setupReplayTestDaemon(t, nil)
		h := d.Handler(ctx)

		rec404 := httptest.NewRecorder()
		h.ServeHTTP(rec404, httptest.NewRequest(http.MethodGet, "/api/matches/does-not-exist/replay/status", nil))
		if rec404.Code != http.StatusNotFound {
			t.Errorf("unknown match status = %d, want 404", rec404.Code)
		}

		rec400 := httptest.NewRecorder()
		h.ServeHTTP(rec400, httptest.NewRequest(http.MethodGet, "/api/matches/%20%20/replay", nil))
		if rec400.Code != http.StatusBadRequest {
			t.Errorf("blank guid replay = %d, want 400", rec400.Code)
		}
	})
}

func TestDaemon_ReplayStatusLookup_SurfacesUpstreamErrors(t *testing.T) {
	ctx := context.Background()

	errorCases := []struct {
		name       string
		findErr    error
		wantStatus int
		wantCode   string
		wantRetry  int
	}{
		{"401 invalid API key", ballchasing.ErrInvalidAPIKey, http.StatusBadGateway, "ballchasing_auth_failed", 0},
		{"403 forbidden", ballchasing.ErrForbidden, http.StatusForbidden, "ballchasing_forbidden", 0},
		{"429 rate limited", &ballchasing.RateLimitError{RetryAfter: 11 * time.Second}, http.StatusTooManyRequests, "ballchasing_rate_limited", 11},
		{"5xx upstream error", ballchasing.ErrServerError, http.StatusBadGateway, "ballchasing_upstream_error", 0},
		{"network error", errors.New("dial tcp timeout"), http.StatusBadGateway, "ballchasing_upstream_error", 0},
	}

	for _, tc := range errorCases {
		t.Run(tc.name, func(t *testing.T) {
			fetcher := &stubReplayFetcher{
				findFn: func(ctx context.Context, q ballchasing.ReplaySearchQuery) (*ballchasing.ReplayItem, error) {
					return nil, tc.findErr
				},
			}
			d, store, _ := setupReplayTestDaemon(t, fetcher)
			_ = store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{{
				MatchGUID:            "m-lookup-err",
				RecordStartTimestamp: time.Now().UTC().Unix(),
				DownloadStatus:       storage.DownloadSkipped,
			}})

			rec := httptest.NewRecorder()
			d.Handler(ctx).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/matches/m-lookup-err/replay/status?lookup=true", nil))
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			var errResp replayErrorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			if errResp.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", errResp.Code, tc.wantCode)
			}
			if tc.wantRetry > 0 {
				if errResp.RetryAfterSeconds != tc.wantRetry {
					t.Errorf("retry_after_seconds = %d, want %d", errResp.RetryAfterSeconds, tc.wantRetry)
				}
				if hdr := rec.Header().Get("Retry-After"); hdr != fmt.Sprintf("%d", tc.wantRetry) {
					t.Errorf("Retry-After = %q, want %d", hdr, tc.wantRetry)
				}
			}
		})
	}

	t.Run("clean ErrNotFound returns 200 no_replay_source", func(t *testing.T) {
		fetcher := &stubReplayFetcher{
			findFn: func(ctx context.Context, q ballchasing.ReplaySearchQuery) (*ballchasing.ReplayItem, error) {
				return nil, ballchasing.ErrNotFound
			},
		}
		d, store, _ := setupReplayTestDaemon(t, fetcher)
		_ = store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{{
			MatchGUID:            "m-lookup-miss",
			RecordStartTimestamp: time.Now().UTC().Unix(),
			DownloadStatus:       storage.DownloadSkipped,
		}})

		rec := httptest.NewRecorder()
		d.Handler(ctx).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/matches/m-lookup-miss/replay/status?lookup=true", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
		}
		var st ReplayAvailabilityResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
			t.Fatalf("unmarshal status: %v", err)
		}
		if st.Available || st.Source != "unavailable" || st.Reason != "no_replay_source" {
			t.Errorf("unexpected clean-miss status: %+v", st)
		}
	})
}

func TestDaemon_SingleflightPostFailureImmediateRetry(t *testing.T) {
	ctx := context.Background()
	bcPayload := []byte("RECOVERED_REPLAY_BYTES")
	var attempt atomic.Int32

	fetcher := &stubReplayFetcher{
		downloadFn: func(ctx context.Context, replayID, destPath string) (int64, error) {
			if attempt.Add(1) == 1 {
				return 0, ballchasing.ErrServerError
			}
			if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
				return 0, err
			}
			if err := os.WriteFile(destPath, bcPayload, 0o644); err != nil {
				return 0, err
			}
			return int64(len(bcPayload)), nil
		},
	}

	d, store, _ := setupReplayTestDaemon(t, fetcher)
	_ = store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{{
		MatchGUID:            "m-sf-retry",
		RecordStartTimestamp: time.Now().UTC().Unix(),
		BallchasingID:        "bc-sf-retry",
	}})
	h := d.Handler(ctx)

	// 1st attempt fails; singleflight entry MUST already be evicted before caller wakes.
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/api/matches/m-sf-retry/replay", nil))
	if rec1.Code != http.StatusBadGateway {
		t.Fatalf("first attempt status = %d, want 502", rec1.Code)
	}

	// Immediate retry must NOT attach to the stale failed call.
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/api/matches/m-sf-retry/replay", nil))
	if rec2.Code != http.StatusOK || !bytes.Equal(rec2.Body.Bytes(), bcPayload) {
		t.Fatalf("immediate retry failed: status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	if calls := fetcher.dlCalls.Load(); calls != 2 {
		t.Fatalf("expected 2 distinct download calls, got %d", calls)
	}
}
