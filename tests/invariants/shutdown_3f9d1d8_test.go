package invariants

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/psynet"
	"github.com/dank/rl-api-utils/internal/session"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/syncer"
	"github.com/gorilla/websocket"
)

func allocateLoopbackPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate ephemeral port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func TestInvariant_GracefulShutdownUnder2sWithSSEAndWS(t *testing.T) {
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	baselineGoroutines := runtime.NumGoroutine()

	tempDir := t.TempDir()
	store, err := storage.NewSQLiteStore(filepath.Join(tempDir, "inv_shutdown.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	// Mock WebSocket server simulating BakkesMod StatsAPI exporter.
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	wsStopCh := make(chan struct{})
	var wsMu sync.Mutex
	var wsConns []*websocket.Conn

	wsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		wsMu.Lock()
		wsConns = append(wsConns, conn)
		wsMu.Unlock()
		defer conn.Close()

		readDone := make(chan struct{})
		go func() {
			defer close(readDone)
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()

		select {
		case <-readDone:
		case <-wsStopCh:
		}
	}))
	defer func() {
		close(wsStopCh)
		wsMu.Lock()
		for _, c := range wsConns {
			_ = c.Close()
		}
		wsMu.Unlock()
		wsSrv.Close()
	}()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	statsTracker, err := statsapi.NewTracker(store, statsapi.TrackerConfig{}, statsapi.WithLogger(logger))
	if err != nil {
		t.Fatalf("statsapi.NewTracker: %v", err)
	}
	wsAddr := strings.TrimPrefix(wsSrv.URL, "http://")
	statsListener, err := statsapi.NewListener(statsapi.ListenerConfig{
		Address:        wsAddr,
		Protocol:       "websocket",
		ReconnectDelay: 100 * time.Millisecond,
	}, statsTracker, logger)
	if err != nil {
		t.Fatalf("statsapi.NewListener: %v", err)
	}

	pt, err := playertrack.NewTracker(
		store,
		&playertrack.NoOpRankClient{},
		config.PlayerTrackingConfig{Enabled: true},
		config.AuthConfig{Provider: "epic"},
		playertrack.WithTrackerLogger(logger),
	)
	if err != nil {
		t.Fatalf("playertrack.NewTracker: %v", err)
	}
	defer pt.Close()

	sessTracker := session.NewSessionTracker(session.WithStateStore(store), session.WithLogger(logger))
	port := allocateLoopbackPort(t)

	cfg := config.NewDefaultConfig()
	cfg.Sync.PollInterval = config.Duration(time.Hour)
	cfg.StatsAPI.Enabled = true
	cfg.StatsAPI.Address = wsAddr
	cfg.StatsAPI.Protocol = "websocket"
	cfg.StatsAPI.HTTPTriggerPort = port
	cfg.Web.Enabled = true
	cfg.Web.Host = "127.0.0.1"
	cfg.Web.Port = port
	cfg.Logging.Level = "error"

	d, err := daemon.New(
		noopDaemonSyncer{},
		cfg,
		daemon.WithStateStore(store),
		daemon.WithStatsTracker(statsTracker),
		daemon.WithStatsListener(statsListener),
		daemon.WithPlayerTracker(pt),
		daemon.WithSessionTracker(sessTracker),
		daemon.WithLogger(logger),
		daemon.WithWebHandler(http.NotFoundHandler()),
	)
	if err != nil {
		t.Fatalf("daemon.New: %v", err)
	}

	daemonCtx, cancelDaemon := context.WithCancel(context.Background())
	daemonErrCh := make(chan error, 1)
	go func() {
		daemonErrCh <- d.Start(daemonCtx)
	}()

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	httpClient := &http.Client{Timeout: 500 * time.Millisecond}

	// Wait for HTTP server and WebSocket StatsAPI listener to become ready.
	readyDeadline := time.Now().Add(4 * time.Second)
	var httpReady bool
	for time.Now().Before(readyDeadline) {
		resp, err := httpClient.Get(baseURL + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK && statsListener.IsConnected() {
				httpReady = true
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !httpReady {
		cancelDaemon()
		t.Fatalf("daemon HTTP+WS failed to become ready within deadline (wsConnected=%v)", statsListener.IsConnected())
	}

	// Connect an active SSE client and verify initial snapshot frame.
	sseResp, err := http.Get(baseURL + "/api/events")
	if err != nil {
		cancelDaemon()
		t.Fatalf("connect SSE /api/events: %v", err)
	}
	defer sseResp.Body.Close()

	reader := bufio.NewReader(sseResp.Body)
	gotEvent := false
	for i := 0; i < 10; i++ {
		line, err := reader.ReadString('\n')
		if err != nil {
			cancelDaemon()
			t.Fatalf("read initial SSE frame: %v", err)
		}
		if strings.HasPrefix(line, "data:") {
			gotEvent = true
			break
		}
	}
	if !gotEvent {
		cancelDaemon()
		t.Fatalf("expected initial SSE data frame before shutdown")
	}

	sseClosedCh := make(chan error, 1)
	go func() {
		buf := make([]byte, 256)
		for {
			if _, err := reader.Read(buf); err != nil {
				sseClosedCh <- err
				return
			}
		}
	}()

	// Trigger graceful shutdown and enforce strict < 2s completion bound.
	shutdownStart := time.Now()
	cancelDaemon()

	select {
	case err := <-daemonErrCh:
		elapsed := time.Since(shutdownStart)
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("daemon exited with unexpected error: %v", err)
		}
		if elapsed >= 2*time.Second {
			t.Fatalf("graceful shutdown took %v, exceeded strict < 2s invariant", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("daemon hung on shutdown (> 2s) with active SSE + WS")
	}

	select {
	case <-sseClosedCh:
	case <-time.After(1500 * time.Millisecond):
		t.Fatalf("SSE stream did not close promptly after daemon shutdown")
	}

	// Close mock WS connections and ensure no background goroutines remain leaked.
	time.Sleep(150 * time.Millisecond)
	runtime.GC()
	afterGoroutines := runtime.NumGoroutine()
	if afterGoroutines > baselineGoroutines+10 {
		t.Fatalf("possible goroutine leak after graceful shutdown: baseline=%d after=%d", baselineGoroutines, afterGoroutines)
	}
}

type cancelTestProvider struct{ matches []psynet.DiscoveredMatch }

func (p *cancelTestProvider) GetRecentMatches(context.Context) ([]psynet.DiscoveredMatch, error) {
	return append([]psynet.DiscoveredMatch(nil), p.matches...), nil
}
func (p *cancelTestProvider) Close() error { return nil }

type blockingCancelDownloader struct {
	blocking atomic.Bool
	started  chan struct{}
	once     sync.Once
}

func (d *blockingCancelDownloader) DownloadReplay(ctx context.Context, matchGUID, _ string, destDir string) (string, error) {
	if d.blocking.Load() {
		d.once.Do(func() { close(d.started) })
		<-ctx.Done()
		return "", ctx.Err()
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	outPath := filepath.Join(destDir, matchGUID+".replay")
	payload := append([]byte("00000000TAGAME"), make([]byte, 1200)...)
	return outPath, os.WriteFile(outPath, payload, 0o644)
}

type blockingCancelUploader struct {
	blocking atomic.Bool
	started  chan struct{}
	once     sync.Once
}

func (u *blockingCancelUploader) UploadReplay(ctx context.Context, matchGUID, _ string) (*ballchasing.UploadResult, error) {
	if u.blocking.Load() {
		u.once.Do(func() { close(u.started) })
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &ballchasing.UploadResult{ID: "bc-" + matchGUID, Location: "https://ballchasing.com/replay/bc-" + matchGUID}, nil
}

func runCancelledCycle(t *testing.T, s *syncer.Syncer, started <-chan struct{}) {
	t.Helper()
	cycleCtx, cancelCycle := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := s.RunCycle(cycleCtx)
		errCh <- err
	}()
	<-started
	cancelCycle()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled from RunCycle, got %v", err)
	}
}

func TestInvariant_3f9d1d8_CancelledSyncCyclePreservesMatchForRecovery(t *testing.T) {
	bgCtx := context.Background()

	t.Run("CancelMidDownload_PreservesRetryAndRecovers", func(t *testing.T) {
		tempDir := t.TempDir()
		store, err := storage.NewSQLiteStore(filepath.Join(tempDir, "inv_3f9d1d8_dl.db"))
		if err != nil {
			t.Fatalf("NewSQLiteStore: %v", err)
		}
		defer store.Close()

		guid := "m-cancel-dl-1"
		provider := &cancelTestProvider{matches: []psynet.DiscoveredMatch{{MatchGUID: guid, ReplayURL: "https://example.invalid/r1.replay", Playlist: 11, RecordStartTimestamp: time.Now().UTC().Unix()}}}
		dl := &blockingCancelDownloader{started: make(chan struct{})}
		dl.blocking.Store(true)
		ul := &blockingCancelUploader{started: make(chan struct{})}

		s, err := syncer.New(store, provider, dl, ul, syncer.WithReplayDir(filepath.Join(tempDir, "replays")), syncer.WithRateInterval(0))
		if err != nil {
			t.Fatalf("syncer.New: %v", err)
		}

		runCancelledCycle(t, s, dl.started)

		rec, err := store.GetMatch(bgCtx, guid)
		if err != nil {
			t.Fatalf("GetMatch after canceled download: %v", err)
		}
		if rec.DownloadStatus == storage.DownloadFailed || rec.RetryCount != 0 || rec.LastError != "" {
			t.Fatalf("3f9d1d8 regression: status=%q retry=%d lastErr=%q", rec.DownloadStatus, rec.RetryCount, rec.LastError)
		}

		dl.blocking.Store(false)
		stats, err := s.RunCycle(bgCtx)
		if err != nil || stats.DownloadedCount != 1 || stats.UploadedCount != 1 {
			t.Fatalf("recovery RunCycle got stats=%+v err=%v", stats, err)
		}
		if after, err := store.GetMatch(bgCtx, guid); err != nil || after.UploadStatus != storage.UploadUploaded {
			t.Fatalf("expected UPLOADED after recovery, got %+v (err=%v)", after, err)
		}
	})

	t.Run("CancelMidUpload_PreservesRetryAndRecovers", func(t *testing.T) {
		tempDir := t.TempDir()
		store, err := storage.NewSQLiteStore(filepath.Join(tempDir, "inv_3f9d1d8_ul.db"))
		if err != nil {
			t.Fatalf("NewSQLiteStore: %v", err)
		}
		defer store.Close()

		guid := "m-cancel-ul-1"
		replayDir := filepath.Join(tempDir, "replays")
		_ = os.MkdirAll(replayDir, 0o755)
		replayPath := filepath.Join(replayDir, guid+".replay")
		if err := os.WriteFile(replayPath, append([]byte("00000000TAGAME"), make([]byte, 1200)...), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := store.UpsertDiscoveredMatches(bgCtx, []*storage.MatchRecord{{
			MatchGUID:            guid,
			RecordStartTimestamp: time.Now().UTC().Unix(),
			DownloadStatus:       storage.DownloadDownloaded,
			LocalFilePath:        replayPath,
			UploadStatus:         storage.UploadPending,
			StatsSyncStatus:      storage.StatsSyncPending,
		}}); err != nil {
			t.Fatalf("seed downloaded match: %v", err)
		}

		ul := &blockingCancelUploader{started: make(chan struct{})}
		ul.blocking.Store(true)
		s, err := syncer.New(store, &noopHistoryProvider{}, &blockingCancelDownloader{started: make(chan struct{})}, ul, syncer.WithReplayDir(replayDir), syncer.WithRateInterval(0))
		if err != nil {
			t.Fatalf("syncer.New: %v", err)
		}

		runCancelledCycle(t, s, ul.started)

		rec, err := store.GetMatch(bgCtx, guid)
		if err != nil {
			t.Fatalf("GetMatch after canceled upload: %v", err)
		}
		if rec.UploadStatus == storage.UploadFailed || rec.RetryCount != 0 || rec.LastError != "" {
			t.Fatalf("3f9d1d8 regression: status=%q retry=%d lastErr=%q", rec.UploadStatus, rec.RetryCount, rec.LastError)
		}

		ul.blocking.Store(false)
		stats, err := s.RunCycle(bgCtx)
		if err != nil || stats.UploadedCount != 1 {
			t.Fatalf("recovery RunCycle got stats=%+v err=%v", stats, err)
		}
		if after, err := store.GetMatch(bgCtx, guid); err != nil || after.UploadStatus != storage.UploadUploaded || after.BallchasingID == "" {
			t.Fatalf("expected UPLOADED with BallchasingID after recovery, got %+v (err=%v)", after, err)
		}
	})
}
