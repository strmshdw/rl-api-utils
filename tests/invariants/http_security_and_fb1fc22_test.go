package invariants

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/gorilla/websocket"
)

type syncLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *syncLogBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}

type failingWriteRW struct {
	header http.Header
	code   int
	err    error
}

func (f *failingWriteRW) Header() http.Header {
	if f.header == nil {
		f.header = make(http.Header)
	}
	return f.header
}
func (f *failingWriteRW) WriteHeader(statusCode int) { f.code = statusCode }
func (f *failingWriteRW) Write([]byte) (int, error)  { return 0, f.err }

func newInvariantSecurityDaemon(t *testing.T, store storage.StateStore, logger *slog.Logger) *daemon.Daemon {
	t.Helper()
	cfg := config.NewDefaultConfig()
	cfg.Logging.Level = "debug"
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	d, err := daemon.New(
		noopDaemonSyncer{},
		cfg,
		daemon.WithStateStore(store),
		daemon.WithLogger(logger),
		daemon.WithWebHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})),
	)
	if err != nil {
		t.Fatalf("daemon.New: %v", err)
	}
	return d
}

func TestInvariant_LoopbackOriginCSRFProtection(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "inv_csrf.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	handler := newInvariantSecurityDaemon(t, store, nil).Handler(ctx)

	mutatingMethods := []string{http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete}
	for _, method := range mutatingMethods {
		t.Run("RejectEvilOrigin_"+method, func(t *testing.T) {
			path := "/api/session/reset"
			if method == http.MethodPatch {
				path = "/api/players/Steam|1|0"
			}
			req := httptest.NewRequest(method, path, strings.NewReader(`{}`))
			req.Header.Set("Origin", "https://evil.example")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s with evil Origin got %d, want 403 Forbidden (%s)", method, rec.Code, rec.Body.String())
			}
		})
	}

	allowedOrigins := []string{
		"",
		"http://127.0.0.1:49125",
		"http://localhost:5173",
		"http://[::1]:5173",
	}
	for _, origin := range allowedOrigins {
		name := origin
		if name == "" {
			name = "empty"
		}
		t.Run("AllowLoopbackOrEmpty_"+name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/session/reset", nil)
			if origin != "" {
				req.Header.Set("Origin", origin)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code == http.StatusForbidden {
				t.Fatalf("loopback/empty Origin %q unexpectedly rejected with 403", origin)
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("POST /api/session/reset with Origin %q got %d, want 200", origin, rec.Code)
			}
		})
	}
}

func TestInvariant_PathTraversalRejection(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "inv_traversal.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	srv := httptest.NewServer(newInvariantSecurityDaemon(t, store, nil).Handler(ctx))
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")

	vectors := []string{
		"/../etc/passwd",
		"/..%2fetc/passwd",
		"/%2e%2e/etc/passwd",
		"/api/players/%2e%2e%2fsecret",
		"/api/players/%00bad",
		"/assets/..%5csecret",
	}

	for _, vec := range vectors {
		t.Run("RawSocket_"+vec, func(t *testing.T) {
			conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
			if err != nil {
				t.Fatalf("net.Dial: %v", err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

			rawReq := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", vec, addr)
			if _, err := conn.Write([]byte(rawReq)); err != nil {
				t.Fatalf("write raw request: %v", err)
			}
			resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodGet})
			if err != nil {
				t.Fatalf("ReadResponse: %v", err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusNotFound {
				t.Fatalf("traversal vector %q returned %d, want 400 or 404", vec, resp.StatusCode)
			}
		})
	}
}

func TestInvariant_JSONBody1MiBLimit(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "inv_body_limit.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	if err := store.UpsertPlayer(ctx, &storage.PlayerRecord{
		PlayerID:    "Steam|76561198000000111|0",
		Platform:    "Steam",
		PlayerName:  "BodyLimitTarget",
		FirstSeenAt: time.Now().UTC(),
		LastSeenAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertPlayer: %v", err)
	}

	handler := newInvariantSecurityDaemon(t, store, nil).Handler(ctx)

	oversized := `{"notes":"` + strings.Repeat("A", (1<<20)+64) + `"}`
	for _, ep := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/sync/config"},
		{http.MethodPatch, "/api/players/Steam|76561198000000111|0"},
	} {
		req := httptest.NewRequest(ep.method, ep.path, strings.NewReader(oversized))
		req.Header.Set("Origin", "http://127.0.0.1:49125")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("%s %s with >1 MiB body returned %d, want 413 Request Entity Too Large (%s)",
				ep.method, ep.path, rec.Code, rec.Body.String())
		}
	}

	// Valid under-limit JSON payload succeeds.
	okReq := httptest.NewRequest(http.MethodPost, "/api/sync/config", strings.NewReader(`{"trigger_threshold":5}`))
	okReq.Header.Set("Origin", "http://127.0.0.1:49125")
	okReq.Header.Set("Content-Type", "application/json")
	okRec := httptest.NewRecorder()
	handler.ServeHTTP(okRec, okReq)
	if okRec.Code != http.StatusOK {
		t.Fatalf("valid under-limit POST /api/sync/config got %d: %s", okRec.Code, okRec.Body.String())
	}
}

func TestInvariant_fb1fc22_DisconnectSuppressionAndPlayerDisconnect(t *testing.T) {
	t.Run("Daemon_HTTPClientDisconnectLogsDebugNotWarnOrError", func(t *testing.T) {
		store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "inv_fb1fc22_http.db"))
		if err != nil {
			t.Fatalf("NewSQLiteStore: %v", err)
		}
		defer store.Close()

		var logBuf syncLogBuffer
		logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		d := newInvariantSecurityDaemon(t, store, logger)

		reqCtx, cancel := context.WithCancel(context.Background())
		cancel() // simulate aborted HTTP client connection
		req := httptest.NewRequestWithContext(reqCtx, http.MethodGet, "/api/players", nil)
		rw := &failingWriteRW{err: io.EOF}
		d.Handler(context.Background()).ServeHTTP(rw, req)

		logs := logBuf.String()
		if strings.Contains(logs, "level=WARN") || strings.Contains(logs, "level=ERROR") {
			t.Fatalf("fb1fc22 regression: client disconnect logged WARN/ERROR:\n%s", logs)
		}
		if !strings.Contains(logs, "level=DEBUG") {
			t.Fatalf("expected client disconnect handling to log at DEBUG level, got:\n%s", logs)
		}
	})

	t.Run("StatsAPI_WSDisconnectLogsDebugNotWarnOrError", func(t *testing.T) {
		store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "inv_fb1fc22_ws.db"))
		if err != nil {
			t.Fatalf("NewSQLiteStore: %v", err)
		}
		defer store.Close()

		var logBuf syncLogBuffer
		logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		closedCh := make(chan struct{}, 4)
		wsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			_ = conn.Close() // abrupt EOF disconnect
			select {
			case closedCh <- struct{}{}:
			default:
			}
		}))
		defer wsSrv.Close()

		tracker, err := statsapi.NewTracker(store, statsapi.TrackerConfig{}, statsapi.WithLogger(logger))
		if err != nil {
			t.Fatalf("statsapi.NewTracker: %v", err)
		}
		wsAddr := strings.TrimPrefix(wsSrv.URL, "http://")
		listener, err := statsapi.NewListener(statsapi.ListenerConfig{
			Address:        wsAddr,
			Protocol:       "websocket",
			ReconnectDelay: 40 * time.Millisecond,
		}, tracker, logger)
		if err != nil {
			t.Fatalf("statsapi.NewListener: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- listener.Start(ctx) }()

		select {
		case <-closedCh:
		case <-time.After(3 * time.Second):
			cancel()
			t.Fatalf("timed out waiting for StatsAPI WS disconnect")
		}
		time.Sleep(60 * time.Millisecond)
		cancel()
		<-done

		logs := logBuf.String()
		if strings.Contains(logs, "level=WARN") || strings.Contains(logs, "level=ERROR") {
			t.Fatalf("fb1fc22 regression: WS disconnect logged WARN/ERROR:\n%s", logs)
		}
		if !strings.Contains(logs, "level=DEBUG") || !strings.Contains(logs, "Stats API connection closed") {
			t.Fatalf("expected WS disconnect debug log, got:\n%s", logs)
		}
	})

	t.Run("PlayerTrack_MidMatchDisconnectPreservesStats", func(t *testing.T) {
		ctx := context.Background()
		store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "inv_fb1fc22_pt.db"))
		if err != nil {
			t.Fatalf("NewSQLiteStore: %v", err)
		}
		defer store.Close()

		localPID := "Steam|76561198000000100|0"
		tmPID := "Epic|teammate-drop-200|0"
		oppPID := "Steam|76561198000000300|0"

		tracker, err := playertrack.NewTracker(
			store,
			&playertrack.NoOpRankClient{},
			config.PlayerTrackingConfig{Enabled: true, LocalPlayerID: localPID},
			config.AuthConfig{Provider: "steam", Steam: config.SteamConfig{SteamID64: "76561198000000100"}},
		)
		if err != nil {
			t.Fatalf("playertrack.NewTracker: %v", err)
		}
		defer tracker.Close()

		guid := "m-disconnect-keep"
		if err := tracker.OnUpdateState(ctx, guid, 11, []statsapi.StatsPlayer{
			{Name: "Local", PrimaryId: localPID, TeamNum: 0, Score: 400, Goals: 2},
			{Name: "DroppingTM", PrimaryId: tmPID, TeamNum: 0, Score: 320, Goals: 1, Assists: 1},
			{Name: "Opp", PrimaryId: oppPID, TeamNum: 1, Score: 210, Saves: 2},
		}); err != nil {
			t.Fatalf("frame 1 OnUpdateState: %v", err)
		}
		// Frame 2: teammate disconnected (omitted from UpdateState)
		if err := tracker.OnUpdateState(ctx, guid, 11, []statsapi.StatsPlayer{
			{Name: "Local", PrimaryId: localPID, TeamNum: 0, Score: 520, Goals: 3},
			{Name: "Opp", PrimaryId: oppPID, TeamNum: 1, Score: 250, Saves: 2},
		}); err != nil {
			t.Fatalf("frame 2 OnUpdateState: %v", err)
		}

		snap := tracker.GetCurrentMatch()
		if len(snap.Teammates) != 1 || !snap.Teammates[0].IsDisconnected {
			t.Fatalf("expected disconnected teammate retained in live match snapshot, got %+v", snap.Teammates)
		}
		if snap.Teammates[0].Stats.Score != 320 || snap.Teammates[0].Stats.Goals != 1 {
			t.Fatalf("expected disconnected teammate stats preserved, got %+v", snap.Teammates[0].Stats)
		}

		win := 0
		if err := tracker.OnMatchEnded(ctx, guid, &win); err != nil {
			t.Fatalf("OnMatchEnded: %v", err)
		}
		matchup, err := store.GetPlayerMatchup(ctx, tmPID, 11)
		if err != nil || matchup.WinsAsTeammate != 1 {
			t.Fatalf("expected disconnected teammate matchup win persisted, got %+v (err=%v)", matchup, err)
		}
	})
}

func TestInvariant_PlayersEndpointShapeParity(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "inv_players_shape.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	if err := store.UpsertPlayer(ctx, &storage.PlayerRecord{
		PlayerID:    "Steam|76561198000000555|0",
		Platform:    "Steam",
		PlayerName:  "ShapeCheckPlayer",
		FirstSeenAt: now,
		LastSeenAt:  now,
	}); err != nil {
		t.Fatalf("UpsertPlayer: %v", err)
	}

	handler := newInvariantSecurityDaemon(t, store, nil).Handler(ctx)

	// GET /players MUST return a bare JSON array `[...]`.
	recBare := httptest.NewRequest(http.MethodGet, "/players?limit=10", nil)
	resBare := httptest.NewRecorder()
	handler.ServeHTTP(resBare, recBare)
	if resBare.Code != http.StatusOK {
		t.Fatalf("GET /players = %d: %s", resBare.Code, resBare.Body.String())
	}
	var bareArr []map[string]any
	if err := json.Unmarshal(resBare.Body.Bytes(), &bareArr); err != nil {
		t.Fatalf("GET /players did not return bare JSON array: %v (body=%s)", err, resBare.Body.String())
	}
	if len(bareArr) != 1 {
		t.Fatalf("GET /players len = %d, want 1", len(bareArr))
	}

	// GET /api/players MUST return paginated object `{players: [...], total, limit, offset}`.
	recPaged := httptest.NewRequest(http.MethodGet, "/api/players?limit=10&offset=0", nil)
	resPaged := httptest.NewRecorder()
	handler.ServeHTTP(resPaged, recPaged)
	if resPaged.Code != http.StatusOK {
		t.Fatalf("GET /api/players = %d: %s", resPaged.Code, resPaged.Body.String())
	}
	var paged daemon.PlayerSearchResponse
	if err := json.Unmarshal(resPaged.Body.Bytes(), &paged); err != nil {
		t.Fatalf("GET /api/players did not return paginated object: %v (body=%s)", err, resPaged.Body.String())
	}
	if paged.Total != 1 || paged.Limit != 10 || paged.Offset != 0 || len(paged.Players) != 1 {
		t.Fatalf("GET /api/players unexpected payload: %+v", paged)
	}
}
