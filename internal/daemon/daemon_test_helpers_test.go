package daemon_test

import (
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/syncer"
)

// mockSyncer provides a controllable implementation of daemon.Syncer for testing.
type mockSyncer struct {
	mu          sync.Mutex
	cycleCount  int
	runFunc     func(ctx context.Context) (*syncer.SyncStats, error)
	cycleStarts chan struct{}
}

func newMockSyncer() *mockSyncer {
	return &mockSyncer{
		cycleStarts: make(chan struct{}, 100),
	}
}

func (m *mockSyncer) RunCycle(ctx context.Context) (*syncer.SyncStats, error) {
	m.mu.Lock()
	m.cycleCount++
	fn := m.runFunc
	m.mu.Unlock()

	select {
	case m.cycleStarts <- struct{}{}:
	default:
	}

	if fn != nil {
		return fn(ctx)
	}
	return &syncer.SyncStats{DiscoveredCount: 1, UploadedCount: 1}, nil
}

func (m *mockSyncer) getCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cycleCount
}

func (m *mockSyncer) setRunFunc(fn func(ctx context.Context) (*syncer.SyncStats, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runFunc = fn
}

func makeTestConfig(interval time.Duration, once bool) *config.Config {
	cfg := config.NewDefaultConfig()
	cfg.Sync.PollInterval = config.Duration(interval)
	cfg.Sync.Once = once
	cfg.Logging.Level = "debug"
	cfg.Logging.Format = "text"
	return cfg
}

// storageBackend defines parameterized test backends
type storageBackend string

const (
	backendSQLite storageBackend = "sqlite"
	backendJSON   storageBackend = "json"
)

var allStorageBackends = []storageBackend{backendSQLite, backendJSON}

// newTestStore instantiates a clean, isolated StateStore for the specified backend.
func newTestStore(t *testing.T, backend storageBackend) storage.StateStore {
	t.Helper()
	tmpDir := t.TempDir()

	switch backend {
	case backendSQLite:
		dbPath := filepath.Join(tmpDir, "test_daemon.db")
		store, err := storage.NewSQLiteStore(dbPath)
		if err != nil {
			t.Fatalf("failed to create sqlite store: %v", err)
		}
		t.Cleanup(func() { _ = store.Close() })
		return store

	case backendJSON:
		jsonPath := filepath.Join(tmpDir, "test_daemon.json")
		store, err := storage.NewJSONStore(jsonPath)
		if err != nil {
			t.Fatalf("failed to create json store: %v", err)
		}
		t.Cleanup(func() { _ = store.Close() })
		return store

	default:
		t.Fatalf("unknown storage backend: %s", backend)
		return nil
	}
}

// newTestTracker instantiates a real playertrack.Tracker with mock rank fetcher.
func newTestTracker(t *testing.T, store storage.StateStore, fetcher playertrack.SkillFetcher) *playertrack.Tracker {
	t.Helper()
	if fetcher == nil {
		fetcher = playertrack.NewMockSkillFetcher()
	}

	cfg := config.PlayerTrackingConfig{
		Enabled:        true,
		AutoFetchRanks: true,
	}
	authCfg := config.AuthConfig{
		Provider: "epic",
		Epic: config.EpicConfig{
			AccountID: "my_epic_acc_001",
		},
	}

	tracker, err := playertrack.NewTracker(store, fetcher, cfg, authCfg)
	if err != nil {
		t.Fatalf("failed to create test tracker: %v", err)
	}
	t.Cleanup(func() { _ = tracker.Close() })
	return tracker
}

// executeTestRequest executes an HTTP request against the daemon's handler.
func executeTestRequest(d *daemon.Daemon, method, path string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	rec := httptest.NewRecorder()
	d.Handler(context.Background()).ServeHTTP(rec, req)
	return rec
}

// 1. Nil Tracker: Daemon initialized without WithPlayerTracker

func executeTestRequestWithOrigin(d *daemon.Daemon, method, path, origin string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	d.Handler(context.Background()).ServeHTTP(rec, req)
	return rec
}

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
