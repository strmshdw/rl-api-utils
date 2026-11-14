package daemon_test

import (
	"bufio"
	"context"
	"encoding/json"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/session"
	"github.com/dank/rl-api-utils/internal/storage"
)

type statusCountingStore struct {
	storage.StateStore
	mu           sync.Mutex
	calls        []storage.MatchFilter
	lastReturned int
}

func (s *statusCountingStore) ListHistoricalMatches(ctx context.Context, filter storage.MatchFilter) ([]*storage.HistoricalMatchRecord, int, error) {
	recs, total, err := s.StateStore.ListHistoricalMatches(ctx, filter)
	s.mu.Lock()
	s.calls = append(s.calls, filter)
	s.lastReturned = len(recs)
	s.mu.Unlock()
	return recs, total, err
}

func TestHardening_StatusUsesFastCountWithoutScanningMatches(t *testing.T) {
	baseStore := newTestStore(t, backendSQLite)
	ctx := context.Background()
	now := time.Now().UTC()

	var seeded []*storage.MatchRecord
	for i := 0; i < 5; i++ {
		seeded = append(seeded, &storage.MatchRecord{
			MatchGUID:      "fast-count-guid-" + string(rune('a'+i)),
			Playlist:       11,
			CreatedAt:      now.Add(-time.Duration(i) * time.Minute),
			DownloadStatus: storage.DownloadDownloaded,
			UploadStatus:   storage.UploadUploaded,
		})
	}
	if err := baseStore.UpsertDiscoveredMatches(ctx, seeded); err != nil {
		t.Fatalf("failed to seed matches: %v", err)
	}

	wrapped := &statusCountingStore{StateStore: baseStore}
	cfg := makeTestConfig(time.Hour, false)
	d, err := daemon.New(newMockSyncer(), cfg, daemon.WithStateStore(wrapped))
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	rec := executeTestRequest(d, http.MethodGet, "/status", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from /status, got %d: %s", rec.Code, rec.Body.String())
	}

	var statusResp daemon.DaemonStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &statusResp); err != nil {
		t.Fatalf("failed to decode /status JSON: %v", err)
	}
	if statusResp.ReplayCount != 5 {
		t.Fatalf("expected ReplayCount=5, got %d", statusResp.ReplayCount)
	}

	wrapped.mu.Lock()
	defer wrapped.mu.Unlock()
	if len(wrapped.calls) != 1 {
		t.Fatalf("expected exactly 1 ListHistoricalMatches count query, got %d", len(wrapped.calls))
	}
	if wrapped.calls[0].Offset != math.MaxInt {
		t.Errorf("expected Offset=math.MaxInt for pure COUNT(*) path, got %d", wrapped.calls[0].Offset)
	}
	if wrapped.lastReturned != 0 {
		t.Errorf("expected 0 scanned match rows from COUNT(*) fast path, got %d", wrapped.lastReturned)
	}
}

func TestHardening_MutatingMethods_LoopbackOriginCSRF(t *testing.T) {
	cfg := makeTestConfig(time.Hour, false)
	d, err := daemon.New(newMockSyncer(), cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}
	handler := d.Handler(context.Background())

	mutatingMethods := []string{http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete}
	rejectedOrigins := []string{
		"http://evil.com",
		"https://192.168.1.50:49125",
		"http://localhost.attacker.test",
		"null",
		"data:text/html,<script>",
	}

	for _, method := range mutatingMethods {
		for _, origin := range rejectedOrigins {
			t.Run(method+" rejects "+origin, func(t *testing.T) {
				req := httptest.NewRequest(method, "/api/session/reset", nil)
				req.Header.Set("Origin", origin)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if rec.Code != http.StatusForbidden {
					t.Fatalf("expected 403 Forbidden for %s Origin=%q, got %d (%s)", method, origin, rec.Code, rec.Body.String())
				}
			})
		}

		t.Run(method+" allows empty Origin past CSRF guard", func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/session/reset", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code == http.StatusForbidden {
				t.Fatalf("did not expect 403 Forbidden for empty Origin on %s", method)
			}
		})
	}

	loopbackOrigins := []string{
		"http://localhost:5173",
		"https://127.0.0.1:49125",
		"http://[::1]:3000",
	}
	for _, origin := range loopbackOrigins {
		t.Run("POST allows loopback "+origin, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/session/reset", nil)
			req.Header.Set("Origin", origin)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200 OK for loopback Origin %q, got %d", origin, rec.Code)
			}
		})
	}
}

func TestHardening_JSONBodyLimit_OneMiBEnforced(t *testing.T) {
	store := newTestStore(t, backendSQLite)
	ctx := context.Background()
	playerID := "Steam|76561198000000999|0"
	_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
		PlayerID:    playerID,
		PlayerName:  "BodyLimitPlayer",
		Platform:    "steam",
		FirstSeenAt: time.Now().UTC(),
		LastSeenAt:  time.Now().UTC(),
	})

	cfg := makeTestConfig(time.Hour, false)
	d, err := daemon.New(newMockSyncer(), cfg, daemon.WithStateStore(store))
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}
	handler := d.Handler(ctx)

	oversized := `{"notes":"` + strings.Repeat("a", (1<<20)+128) + `"}`
	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/matches/prune"},
		{http.MethodPost, "/api/sync/config"},
		{http.MethodPost, "/api/session/continue"},
		{http.MethodPost, "/api/session/dismiss"},
		{http.MethodPatch, "/api/players/Steam%7C76561198000000999%7C0"},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path+" rejects >1MiB", func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.path, strings.NewReader(oversized))
			req.Header.Set("Origin", "http://localhost:49125")
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("expected 413 Request Entity Too Large for %s %s, got %d (%s)", ep.method, ep.path, rec.Code, rec.Body.String())
			}
		})
	}

	reqOK := httptest.NewRequest(http.MethodPatch, "/api/players/Steam%7C76561198000000999%7C0", strings.NewReader(`{"notes":"ok"}`))
	reqOK.Header.Set("Origin", "http://localhost:49125")
	recOK := httptest.NewRecorder()
	handler.ServeHTTP(recOK, reqOK)
	if recOK.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for small valid PATCH body, got %d: %s", recOK.Code, recOK.Body.String())
	}
}

func TestHardening_SSE_ClearsWriteDeadlinePastServerWriteTimeout(t *testing.T) {
	cfg := makeTestConfig(time.Hour, false)
	st := session.NewSessionTracker()
	d, err := daemon.New(newMockSyncer(), cfg, daemon.WithSessionTracker(st))
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer ln.Close()

	// Intentionally configure an aggressive 60ms WriteTimeout to prove /api/events
	// clears the per-response write deadline via http.ResponseController.
	srv := &http.Server{
		Handler:      d.Handler(ctx),
		WriteTimeout: 60 * time.Millisecond,
	}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	reqCtx, reqCancel := context.WithTimeout(ctx, 2*time.Second)
	defer reqCancel()

	req, _ := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://"+ln.Addr().String()+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to connect to /api/events: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	waitEvent := func(target string) {
		t.Helper()
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("stream read error waiting for %q: %v", target, err)
			}
			if strings.Contains(line, target) {
				return
			}
		}
	}

	waitEvent("event: session_update")

	// Sleep well past WriteTimeout (60ms -> 150ms) and emit a second event.
	time.Sleep(150 * time.Millisecond)
	st.Broadcaster().Broadcast(session.SessionEvent{
		Event: session.EventMatchUpdate,
		Data:  map[string]any{"active_match": true},
	})

	waitEvent("event: match_update")
}
