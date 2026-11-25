package daemon_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/syncer"
)

type isolationSpySyncer struct {
	mu              sync.Mutex
	runCount        int
	disconnectCount int
	retriedGUIDs    []string
	cycleCh         chan int
	retryCh         chan string
	stats           *syncer.SyncStats
	blockCycle      chan struct{}
}

func newIsolationSpySyncer() *isolationSpySyncer {
	return &isolationSpySyncer{
		cycleCh: make(chan int, 32),
		retryCh: make(chan string, 16),
		stats:   &syncer.SyncStats{DiscoveredCount: 3, DownloadedCount: 2, UploadedCount: 2},
	}
}

func (s *isolationSpySyncer) RunCycle(ctx context.Context) (*syncer.SyncStats, error) {
	s.mu.Lock()
	s.runCount++
	n := s.runCount
	st := s.stats
	blk := s.blockCycle
	s.mu.Unlock()

	select {
	case s.cycleCh <- n:
	default:
	}

	if blk != nil {
		select {
		case <-blk:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return st, nil
}

func (s *isolationSpySyncer) DisconnectProvider() error {
	s.mu.Lock()
	s.disconnectCount++
	s.mu.Unlock()
	return nil
}

func (s *isolationSpySyncer) RetryMatch(ctx context.Context, matchGUID string) error {
	s.mu.Lock()
	s.retriedGUIDs = append(s.retriedGUIDs, matchGUID)
	s.mu.Unlock()
	select {
	case s.retryCh <- matchGUID:
	default:
	}
	return nil
}

func (s *isolationSpySyncer) runs() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runCount
}

func (s *isolationSpySyncer) disconnects() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.disconnectCount
}

func TestDaemon_PrimaryIsolation_SkipsStartupAndScheduledTicksWhilePlaying(t *testing.T) {
	var gameRunning atomic.Bool
	gameRunning.Store(true)

	spy := newIsolationSpySyncer()
	cfg := makeTestConfig(25*time.Millisecond, false)
	cfg.Web.Enabled = false
	cfg.StatsAPI.HTTPTriggerPort = 0

	d, err := daemon.New(spy, cfg,
		daemon.WithGameProcessChecker(func() bool { return gameRunning.Load() }),
		daemon.WithStatusTickInterval(15*time.Millisecond),
		daemon.WithDisconnectGrace(30*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() { stopped <- d.Start(ctx) }()

	// Wait across startup + several 25ms poll ticks while Rocket League is active.
	time.Sleep(140 * time.Millisecond)
	if got := spy.runs(); got != 0 {
		cancel()
		<-stopped
		t.Fatalf("expected 0 primary PsyNet sync cycles while Rocket League is running, got %d", got)
	}

	// Simulate player closing Rocket League -> routine polling resumes cleanly.
	gameRunning.Store(false)
	select {
	case n := <-spy.cycleCh:
		if n != 1 {
			t.Fatalf("expected first cycle after exit to be 1, got %d", n)
		}
	case <-time.After(1 * time.Second):
		cancel()
		<-stopped
		t.Fatal("expected scheduled PsyNet sync to resume after Rocket League exited")
	}

	cancel()
	if err := <-stopped; err != nil {
		t.Fatalf("daemon stopped with error: %v", err)
	}
}

func TestDaemon_PrimaryIsolation_TransientDisconnectRespectsGraceWindow(t *testing.T) {
	var gameRunning atomic.Bool
	gameRunning.Store(true)

	store := newTestStore(t, backendSQLite)
	tracker, err := statsapi.NewTracker(store, statsapi.TrackerConfig{
		TriggerThreshold:   15,
		ForceSyncOnTrigger: false,
		EnableToast:        false,
	})
	if err != nil {
		t.Fatalf("NewTracker failed: %v", err)
	}
	defer tracker.Close()

	if _, _, err := tracker.RecordMatch(context.Background(), "pending-guid-1"); err != nil {
		t.Fatalf("RecordMatch failed: %v", err)
	}

	spy := newIsolationSpySyncer()
	cfg := makeTestConfig(1*time.Hour, false)
	cfg.Web.Enabled = false
	cfg.StatsAPI.HTTPTriggerPort = 0
	cfg.StatsAPI.AutoSyncOnExit = true

	d, err := daemon.New(spy, cfg,
		daemon.WithStateStore(store),
		daemon.WithStatsTracker(tracker),
		daemon.WithGameProcessChecker(func() bool { return gameRunning.Load() }),
		daemon.WithStatusTickInterval(15*time.Millisecond),
		daemon.WithDisconnectGrace(120*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() { stopped <- d.Start(ctx) }()

	// Wait for daemon startup while game is active.
	time.Sleep(40 * time.Millisecond)

	// Brief dip shorter than grace window (45ms < 120ms) -> must NOT trigger game_exit_sync!
	gameRunning.Store(false)
	time.Sleep(45 * time.Millisecond)
	gameRunning.Store(true)
	time.Sleep(140 * time.Millisecond)

	if got := spy.runs(); got != 0 {
		cancel()
		<-stopped
		t.Fatalf("transient disconnect under grace window must not trigger primary sync, got %d runs", got)
	}

	// Genuine exit longer than grace window -> triggers game_exit_sync once.
	gameRunning.Store(false)
	select {
	case n := <-spy.cycleCh:
		if n != 1 {
			t.Fatalf("expected 1 game_exit_sync cycle, got %d", n)
		}
	case <-time.After(1 * time.Second):
		cancel()
		<-stopped
		t.Fatal("expected game_exit_sync after genuine Rocket League exit")
	}

	cancel()
	if err := <-stopped; err != nil {
		t.Fatalf("daemon stopped with error: %v", err)
	}
}

func TestDaemon_PrimaryIsolation_ForceOverridesAndSingleMatchRetry(t *testing.T) {
	var gameRunning atomic.Bool
	gameRunning.Store(true)

	store := newTestStore(t, backendSQLite)
	tracker, err := statsapi.NewTracker(store, statsapi.TrackerConfig{
		TriggerThreshold:   2,
		ForceSyncOnTrigger: false,
		EnableToast:        false,
	})
	if err != nil {
		t.Fatalf("NewTracker failed: %v", err)
	}
	defer tracker.Close()

	// Seed a failed match for POST /api/matches/{guid}/retry
	_ = store.UpsertDiscoveredMatches(context.Background(), []*storage.MatchRecord{
		{
			MatchGUID:      "guid-retry-no-psynet",
			DownloadStatus: storage.DownloadFailed,
			UploadStatus:   storage.UploadPending,
			RetryCount:     1,
		},
	})

	spy := newIsolationSpySyncer()
	cfg := makeTestConfig(1*time.Hour, false)
	cfg.Web.Enabled = false
	cfg.StatsAPI.HTTPTriggerPort = 0
	cfg.StatsAPI.TriggerThreshold = 2
	cfg.StatsAPI.ForceSyncOnTrigger = false

	d, err := daemon.New(spy, cfg,
		daemon.WithStateStore(store),
		daemon.WithStatsTracker(tracker),
		daemon.WithGameProcessChecker(func() bool { return gameRunning.Load() }),
		daemon.WithStatusTickInterval(15*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() { stopped <- d.Start(ctx) }()
	defer func() {
		cancel()
		<-stopped
	}()

	time.Sleep(40 * time.Millisecond)

	// 1. Threshold reached while playing with ForceSyncOnTrigger=false -> MUST NOT use primary account.
	_, _, _ = tracker.RecordMatch(ctx, "pending-a")
	_, _, _ = tracker.RecordMatch(ctx, "pending-b")
	time.Sleep(80 * time.Millisecond)
	if got := spy.runs(); got != 0 {
		t.Fatalf("threshold reached with force_sync_on_trigger=false must not run primary sync, got %d", got)
	}

	// 2. POST /api/matches/{guid}/retry while playing -> uses RetryMatch (NOT primary RunCycle!).
	handler := d.Handler(ctx)
	retryReq := httptest.NewRequest(http.MethodPost, "/api/matches/guid-retry-no-psynet/retry", nil)
	retryRec := httptest.NewRecorder()
	handler.ServeHTTP(retryRec, retryReq)
	if retryRec.Code != http.StatusOK {
		t.Fatalf("POST /api/matches/.../retry returned %d: %s", retryRec.Code, retryRec.Body.String())
	}
	select {
	case guid := <-spy.retryCh:
		if guid != "guid-retry-no-psynet" {
			t.Fatalf("unexpected retried guid %q", guid)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for single-match RetryMatch")
	}
	if got := spy.runs(); got != 0 {
		t.Fatalf("POST /api/matches/{guid}/retry must not trigger primary RunCycle while playing, got %d", got)
	}

	// 3. Enabling ForceSyncOnTrigger while pending >= threshold -> MUST force-override and run primary sync!
	forceTrue := true
	tracker.UpdateConfig(2, &forceTrue, nil)
	select {
	case n := <-spy.cycleCh:
		if n != 1 {
			t.Fatalf("expected threshold force-sync to be cycle 1, got %d", n)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("expected threshold with force_sync_on_trigger=true to force primary sync while playing")
	}

	// 4. UI manual sync button (POST /api/sync/now) while playing -> MUST force-override and report status via GET /api/sync/now/status!
	nowReq := httptest.NewRequest(http.MethodPost, "/api/sync/now", nil)
	nowRec := httptest.NewRecorder()
	handler.ServeHTTP(nowRec, nowReq)
	if nowRec.Code != http.StatusOK {
		t.Fatalf("POST /api/sync/now returned %d: %s", nowRec.Code, nowRec.Body.String())
	}
	select {
	case n := <-spy.cycleCh:
		if n != 2 {
			t.Fatalf("expected manual UI sync to be cycle 2, got %d", n)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("expected POST /api/sync/now to force primary sync while playing")
	}

	// Wait until cycle finishes and verify GET /api/sync/now/status reports PsyNet counts.
	deadline := time.Now().Add(1 * time.Second)
	var status daemon.ReplaySyncStatus
	for time.Now().Before(deadline) {
		stReq := httptest.NewRequest(http.MethodGet, "/api/sync/now/status", nil)
		stRec := httptest.NewRecorder()
		handler.ServeHTTP(stRec, stReq)
		if stRec.Code != http.StatusOK {
			t.Fatalf("GET /api/sync/now/status returned %d", stRec.Code)
		}
		if err := json.Unmarshal(stRec.Body.Bytes(), &status); err != nil {
			t.Fatalf("unmarshal /api/sync/now/status: %v", err)
		}
		if !status.Running && status.Reason == "manual_ui_sync_now" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if status.Running {
		t.Fatalf("expected replay sync status to finish running")
	}
	if status.DiscoveredCount != 3 || status.DownloadedCount != 2 || status.UploadedCount != 2 {
		t.Fatalf("unexpected replay sync status counts: %+v", status)
	}
	if spy.disconnects() < 2 {
		t.Fatalf("expected DisconnectProvider to be invoked after each sync cycle, got %d", spy.disconnects())
	}
}
