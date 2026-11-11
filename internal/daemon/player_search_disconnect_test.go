package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/syncer"
)

// abortedConnResponseWriter simulates an HTTP client aborting the TCP connection
// mid-write (e.g., Windows Winsock WSAECONNABORTED 10053 "wsasend: An established
// connection was aborted by the software in your host machine.").
type abortedConnResponseWriter struct {
	header     http.Header
	statusCode int
	writeErr   error
}

func (w *abortedConnResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *abortedConnResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}

func (w *abortedConnResponseWriter) Write(_ []byte) (int, error) {
	return 0, w.writeErr
}

type blockingCancelStore struct {
	storage.StateStore
	entered chan struct{}
	once    sync.Once
}

func (s *blockingCancelStore) FilterPlayerSummaries(ctx context.Context, filter storage.PlayerDirectoryFilter) ([]*storage.PlayerSummary, int, error) {
	s.once.Do(func() { close(s.entered) })
	<-ctx.Done()
	return nil, 0, ctx.Err()
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *syncBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}

func newPlayerSearchTestDaemon(t *testing.T, store storage.StateStore) (*Daemon, *syncBuffer) {
	t.Helper()
	var logBuf syncBuffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg := &config.Config{}
	cfg.Web.Enabled = true
	cfg.PlayerTracking.Enabled = true

	d, err := New(&noopSyncer{}, cfg, WithStateStore(store), WithLogger(logger))
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}
	return d, &logBuf
}

type noopSyncer struct{}

func (n *noopSyncer) RunCycle(_ context.Context) (*syncer.SyncStats, error) {
	return nil, nil
}

func seedLargePlayerDirectory(t *testing.T, store storage.StateStore, count int) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	ranksJSON := `{"11":{"playlist_id":11,"playlist_name":"Ranked Doubles (2v2)","tier":19,"division":2,"rank_name":"Grand Champion I Division II","mmr":1515.5},"13":{"playlist_id":13,"playlist_name":"Ranked Standard (3v3)","tier":18,"division":4,"rank_name":"Champion III Division IV","mmr":1420.0}}`
	for i := 0; i < count; i++ {
		pid := fmt.Sprintf("Steam|76561198%09d|0", i+1)
		if err := store.UpsertPlayer(ctx, &storage.PlayerRecord{
			PlayerID:    pid,
			Platform:    "Steam",
			PlayerName:  fmt.Sprintf("RocketPlayer_%04d_LongDisplayNameForBufferFlush", i+1),
			RanksJSON:   ranksJSON,
			Notes:       "Aggressive first-man挑戰者, strong backboard reads and boost steals.",
			Tags:        []string{"aggro", "2v2-grinder", "fast-aerial"},
			FirstSeenAt: now.Add(-24 * time.Hour),
			LastSeenAt:  now.Add(-time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatalf("UpsertPlayer %d failed: %v", i, err)
		}
		if err := store.RecordMatchResults(ctx, fmt.Sprintf("search-disc-guid-%d", i), 11, []storage.PlayerOutcome{
			{
				PlayerID:   pid,
				Platform:   "Steam",
				PlayerName: fmt.Sprintf("RocketPlayer_%04d_LongDisplayNameForBufferFlush", i+1),
				IsTeammate: i%2 == 0,
				Won:        i%3 != 0,
			},
		}, now.Add(-time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("RecordMatchResults %d failed: %v", i, err)
		}
	}
}

// TestHandleListPlayers_SuppressesWarnOnWindowsWSASendConnAborted reproduces the exact
// production warning:
// level=WARN msg="error encoding player search response" error="write tcp 192.168.50.27:49125->192.168.50.27:55122: wsasend: An established connection was aborted by the software in your host machine."
func TestHandleListPlayers_SuppressesWarnOnWindowsWSASendConnAborted(t *testing.T) {
	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer store.Close()
	seedLargePlayerDirectory(t, store, 10)

	d, logBuf := newPlayerSearchTestDaemon(t, store)

	disconnectErrors := []struct {
		name string
		err  error
	}{
		{
			name: "windows_wsasend_WSAECONNABORTED_10053",
			err: &net.OpError{
				Op:     "write",
				Net:    "tcp",
				Source: &net.TCPAddr{IP: net.ParseIP("192.168.50.27"), Port: 49125},
				Addr:   &net.TCPAddr{IP: net.ParseIP("192.168.50.27"), Port: 55122},
				Err:    os.NewSyscallError("wsasend", syscall.Errno(10053)),
			},
		},
		{
			name: "windows_wsasend_message_string",
			err:  errors.New("write tcp 192.168.50.27:49125->192.168.50.27:55122: wsasend: An established connection was aborted by the software in your host machine."),
		},
		{
			name: "windows_wsasend_WSAECONNRESET_10054",
			err:  os.NewSyscallError("wsasend", syscall.Errno(10054)),
		},
		{
			name: "posix_broken_pipe",
			err:  os.NewSyscallError("write", syscall.EPIPE),
		},
		{
			name: "posix_conn_reset",
			err:  os.NewSyscallError("write", syscall.ECONNRESET),
		},
	}

	for _, tc := range disconnectErrors {
		t.Run(tc.name, func(t *testing.T) {
			for _, path := range []string{"/api/players?limit=24&offset=0", "/players?limit=24&offset=0"} {
				logBuf.Reset()
				req := httptest.NewRequest(http.MethodGet, path, nil)
				rw := &abortedConnResponseWriter{writeErr: tc.err}
				d.handleListPlayers(rw, req)

				logs := logBuf.String()
				if strings.Contains(logs, "error encoding player search response") ||
					strings.Contains(logs, "error encoding player summaries response") ||
					strings.Contains(logs, "level=WARN") ||
					strings.Contains(logs, "level=ERROR") {
					t.Fatalf("expected zero WARN/ERROR logs on client-aborted socket (%s, %s), got:\n%s", tc.name, path, logs)
				}
			}
		})
	}
}

// TestHandleListPlayers_SuppressesErrorLogWhenRequestContextCanceledMidQuery verifies that
// aborting an in-flight GET /api/players fetch while SQLite is querying does not log ERROR/WARN.
func TestHandleListPlayers_SuppressesErrorLogWhenRequestContextCanceledMidQuery(t *testing.T) {
	baseStore, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer baseStore.Close()

	store := &blockingCancelStore{
		StateStore: baseStore,
		entered:    make(chan struct{}),
	}
	d, logBuf := newPlayerSearchTestDaemon(t, store)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/players?query=squishy&limit=24", nil)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		d.Handler(context.Background()).ServeHTTP(rec, req)
	}()

	<-store.entered
	cancel()
	<-done

	logs := logBuf.String()
	if strings.Contains(logs, "level=ERROR") || strings.Contains(logs, "level=WARN") {
		t.Fatalf("expected zero WARN/ERROR logs when client cancels GET /api/players mid-query, got:\n%s", logs)
	}
}

// TestHandleListPlayers_RealTCPClientAbortOnLargeResponse verifies over a real TCP socket
// that aborting a large (>32KB) GET /api/players response produces zero WARN/ERROR logs.
func TestHandleListPlayers_RealTCPClientAbortOnLargeResponse(t *testing.T) {
	store, err := storage.NewSQLiteStore(t.TempDir() + "/tcp_abort.db")
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer store.Close()
	seedLargePlayerDirectory(t, store, 80)

	d, logBuf := newPlayerSearchTestDaemon(t, store)
	srv := httptest.NewServer(d.Handler(context.Background()))
	defer srv.Close()

	// Fire rapid aborted TCP requests to simulate browser AbortController / RST on Windows/Unix
	for i := 0; i < 15; i++ {
		conn, dialErr := net.DialTimeout("tcp", srv.Listener.Addr().String(), 2*time.Second)
		if dialErr != nil {
			t.Fatalf("net.Dial failed: %v", dialErr)
		}
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			// SetLinger(0) forces TCP RST on Close(), reproducing Winsock WSAECONNABORTED / ECONNRESET
			_ = tcpConn.SetLinger(0)
		}
		_, _ = fmt.Fprintf(conn, "GET /api/players?limit=100&offset=0&query=Rocket HTTP/1.1\r\nHost: %s\r\n\r\n", srv.Listener.Addr().String())
		if i%2 == 0 {
			time.Sleep(time.Duration(i) * time.Millisecond)
		}
		_ = conn.Close()
	}

	// Give server handler goroutines a brief moment to finish writing to closed sockets
	time.Sleep(100 * time.Millisecond)

	logs := logBuf.String()
	if strings.Contains(logs, "error encoding player search response") ||
		strings.Contains(logs, "level=WARN") ||
		strings.Contains(logs, "level=ERROR") {
		t.Fatalf("expected zero WARN/ERROR logs on real TCP RST during /api/players, got:\n%s", logs)
	}
}

// TestWriteJSONResponse_LogsWarnOnGenuineJSONMarshalFailure ensures genuine encoding bugs
// (such as NaN floats) still emit the expected WARN log rather than being silently swallowed.
func TestWriteJSONResponse_LogsWarnOnGenuineJSONMarshalFailure(t *testing.T) {
	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer store.Close()

	d, logBuf := newPlayerSearchTestDaemon(t, store)
	req := httptest.NewRequest(http.MethodGet, "/api/players", nil)
	rw := &abortedConnResponseWriter{writeErr: errors.New("disk quota exceeded in custom response sink")}

	d.handleListPlayers(rw, req)
	logs := logBuf.String()
	if !strings.Contains(logs, "error encoding player search response") {
		t.Fatalf("expected non-disconnect write failure to still log warning, got:\n%s", logs)
	}

	logBuf.Reset()
	rec := httptest.NewRecorder()
	d.writeJSONResponse(rec, req, http.StatusOK, map[string]float64{"bad": math.NaN()}, "error encoding player search response")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected HTTP 500 on json.Marshal failure, got %d", rec.Code)
	}
	if !strings.Contains(logBuf.String(), "error encoding player search response") {
		t.Fatalf("expected json.Marshal failure to log warning, got:\n%s", logBuf.String())
	}
}
