package daemon_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
)

func TestDaemon_HTTPServer_Lifecycle_StartAndDrain(t *testing.T) {
	store := newTestStore(t, backendSQLite)
	tracker := newTestTracker(t, store, nil)

	cfg := makeTestConfig(1*time.Hour, false)
	cfg.PlayerTracking.Enabled = true
	// Use an ephemeral or dedicated test port to avoid conflicting with active daemons
	cfg.StatsAPI.HTTPTriggerPort = 49128

	syncer := newMockSyncer()
	d, err := daemon.New(syncer, cfg,
		daemon.WithPlayerTracker(tracker),
		daemon.WithStateStore(store),
	)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)

	go func() {
		stopped <- d.Start(ctx)
	}()

	// Wait for immediate startup cycle
	select {
	case <-syncer.cycleStarts:
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for initial cycle")
	}

	// Verify server is listening and accepts HTTP requests
	client := &http.Client{Timeout: 1 * time.Second}
	var resp *http.Response
	for i := 0; i < 40; i++ {
		resp, err = client.Get("http://127.0.0.1:49128/current-match")
		if err == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err != nil {
		cancel()
		t.Fatalf("failed to query live HTTP server: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp.StatusCode)
	}

	// Cancel context to initiate graceful drain
	cancel()

	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Start returned error on shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("daemon failed to shut down cleanly within 3s drain deadline")
	}

	// Confirm port is released: new connection should fail immediately
	_, err = client.Get("http://127.0.0.1:49128/current-match")
	if err == nil {
		t.Fatal("expected connection to fail after server shutdown")
	}
}

func TestDaemon_HTTPServer_PortConflictResilience(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:49129")
	if err != nil {
		t.Skip("port 49129 unavailable for test")
	}
	defer ln.Close()

	cfg := makeTestConfig(20*time.Millisecond, false)
	cfg.StatsAPI.HTTPTriggerPort = 49129 // Collides with ln

	syncer := newMockSyncer()
	d, err := daemon.New(syncer, cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)

	go func() {
		stopped <- d.Start(ctx)
	}()

	// Verify that daemon continues executing scheduled ticker cycles despite HTTP bind failure
	for i := 0; i < 2; i++ {
		select {
		case <-syncer.cycleStarts:
		case <-time.After(1 * time.Second):
			t.Fatal("daemon halted sync loop due to HTTP port conflict!")
		}
	}

	cancel()
	if err := <-stopped; err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
}

func TestDaemon_HTTPServer_OnceMode_NeverStarted(t *testing.T) {
	cfg := makeTestConfig(1*time.Hour, true) // Once = true
	cfg.StatsAPI.HTTPTriggerPort = 49130

	d, _ := daemon.New(newMockSyncer(), cfg)

	err := d.Start(context.Background())
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Verify HTTP server was never started
	client := &http.Client{Timeout: 100 * time.Millisecond}
	_, err = client.Get("http://127.0.0.1:49130/status")
	if err == nil {
		t.Fatal("HTTP server should not start in Once mode")
	}
}

func TestDaemon_ShutdownWithActiveSSEClient(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	cfg := makeTestConfig(10*time.Second, false)
	cfg.Web.Enabled = true
	cfg.Web.Host = "127.0.0.1"
	cfg.Web.Port = port

	var logBuf syncLogBuffer
	logger := daemon.NewLogger(config.LoggingConfig{Level: "info", Format: "text"}, &logBuf)

	d, err := daemon.New(newMockSyncer(), cfg, daemon.WithLogger(logger))
	if err != nil {
		t.Fatalf("failed to create daemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	doneCh := make(chan error, 1)
	go func() {
		doneCh <- d.Start(ctx)
	}()

	// Wait for HTTP server to come up
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
	}

	// Open a persistent SSE stream to /api/events (just like an open browser dashboard tab)
	sseResp, err := http.Get(baseURL + "/api/events")
	if err != nil {
		t.Fatalf("failed to open /api/events SSE stream: %v", err)
	}
	defer sseResp.Body.Close()

	// Trigger daemon shutdown while the SSE client is still connected
	shutdownStart := time.Now()
	cancel()

	select {
	case err := <-doneCh:
		if err != nil {
			t.Fatalf("daemon.Start returned error on shutdown: %v", err)
		}
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("daemon failed to drain within 1.5s while SSE stream was active")
	}

	elapsed := time.Since(shutdownStart)
	if elapsed >= 1500*time.Millisecond {
		t.Errorf("expected immediate SSE drain (<1.5s), took %v", elapsed)
	}
	if strings.Contains(logBuf.String(), "HTTP server shutdown encountered error") {
		t.Errorf("expected zero HTTP server shutdown errors, got logs:\n%s", logBuf.String())
	}
}

func TestDaemon_StatsListenerConnectedIdle_DrainsImmediatelyOnShutdown(t *testing.T) {
	// Start an idle TCP listener simulating Rocket League Stats API connected with zero games played
	statsLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start mock stats TCP server: %v", err)
	}
	defer statsLn.Close()

	acceptedCh := make(chan net.Conn, 1)
	go func() {
		conn, acceptErr := statsLn.Accept()
		if acceptErr == nil {
			acceptedCh <- conn
		}
	}()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free HTTP port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	cfg := makeTestConfig(10*time.Second, false)
	cfg.Web.Enabled = true
	cfg.Web.Host = "127.0.0.1"
	cfg.Web.Port = port
	cfg.StatsAPI.Enabled = true
	cfg.StatsAPI.Address = statsLn.Addr().String()
	cfg.StatsAPI.Protocol = "tcp"

	var logBuf syncLogBuffer
	logger := daemon.NewLogger(config.LoggingConfig{Level: "info", Format: "text"}, &logBuf)

	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	tracker, err := statsapi.NewTracker(store, statsapi.TrackerConfig{TriggerThreshold: 15}, statsapi.WithLogger(logger))
	if err != nil {
		t.Fatalf("failed to create stats tracker: %v", err)
	}
	listener, err := statsapi.NewListener(statsapi.ListenerConfig{
		Address:  cfg.StatsAPI.Address,
		Protocol: "tcp",
	}, tracker, logger)
	if err != nil {
		t.Fatalf("failed to create stats listener: %v", err)
	}

	d, err := daemon.New(
		newMockSyncer(),
		cfg,
		daemon.WithLogger(logger),
		daemon.WithStateStore(store),
		daemon.WithStatsTracker(tracker),
		daemon.WithStatsListener(listener),
	)
	if err != nil {
		t.Fatalf("failed to create daemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	doneCh := make(chan error, 1)
	go func() {
		doneCh <- d.Start(ctx)
	}()

	// Wait for Stats API listener to connect and enter blocking read with zero messages sent
	var serverConn net.Conn
	select {
	case serverConn = <-acceptedCh:
		defer serverConn.Close()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for statsListener to connect to idle Stats API server")
	}

	// Wait for listener.IsConnected() to be true
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) && !listener.IsConnected() {
		time.Sleep(10 * time.Millisecond)
	}

	// Cancel context (simulating Ctrl+C while connected with no games played)
	shutdownStart := time.Now()
	cancel()

	select {
	case err := <-doneCh:
		if err != nil {
			t.Fatalf("daemon.Start returned error on shutdown: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("daemon hung on awaiting in-flight drain when Stats API listener was idle")
	}

	if elapsed := time.Since(shutdownStart); elapsed >= 1*time.Second {
		t.Errorf("expected immediate drain (<1s), took %v", elapsed)
	}
}
