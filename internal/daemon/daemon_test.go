package daemon_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	"github.com/dank/rl-api-utils/internal/syncer"
	"github.com/dank/rlapi"
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

func TestNew_Validation(t *testing.T) {
	cfg := makeTestConfig(5*time.Minute, false)
	mock := newMockSyncer()

	// Nil syncer
	if _, err := daemon.New(nil, cfg); err == nil {
		t.Fatal("expected error for nil syncer")
	}

	// Nil config
	if _, err := daemon.New(mock, nil); err == nil {
		t.Fatal("expected error for nil config")
	}

	// Valid New
	d, err := daemon.New(mock, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d == nil {
		t.Fatal("expected non-nil daemon")
	}
}

func TestDaemon_ImmediateInitialRun(t *testing.T) {
	// Interval is 1 hour; immediate run must happen right away
	cfg := makeTestConfig(1*time.Hour, false)
	mock := newMockSyncer()

	buf := &bytes.Buffer{}
	logger := daemon.NewLogger(cfg.Logging, buf)

	d, err := daemon.New(mock, cfg, daemon.WithLogger(logger))
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)

	go func() {
		stopped <- d.Start(ctx)
	}()

	// Wait for the immediate cycle to trigger
	select {
	case <-mock.cycleStarts:
		// success: immediate cycle was triggered
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for immediate cycle")
	}

	// Cancel context to shut down cleanly
	cancel()

	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("daemon did not stop on context cancellation")
	}

	if count := mock.getCount(); count != 1 {
		t.Fatalf("expected exactly 1 cycle, got %d", count)
	}
}

func TestDaemon_SingleRunOnce_Success(t *testing.T) {
	cfg := makeTestConfig(1*time.Hour, true) // Once = true
	mock := newMockSyncer()

	d, err := daemon.New(mock, cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	// Start without canceling context; should exit automatically after 1 cycle
	err = d.Start(context.Background())
	if err != nil {
		t.Fatalf("expected nil error in once mode, got: %v", err)
	}

	if count := mock.getCount(); count != 1 {
		t.Fatalf("expected exactly 1 cycle, got %d", count)
	}
}

func TestDaemon_SingleRunOnce_Error(t *testing.T) {
	cfg := makeTestConfig(1*time.Hour, true) // Once = true
	mock := newMockSyncer()
	expectedErr := errors.New("auth token expired")
	mock.setRunFunc(func(ctx context.Context) (*syncer.SyncStats, error) {
		return nil, expectedErr
	})

	d, err := daemon.New(mock, cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	err = d.Start(context.Background())
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}

	if count := mock.getCount(); count != 1 {
		t.Fatalf("expected exactly 1 cycle, got %d", count)
	}
}

func TestDaemon_TickerTriggering(t *testing.T) {
	// Rapid interval: 15ms
	cfg := makeTestConfig(15*time.Millisecond, false)
	mock := newMockSyncer()

	d, err := daemon.New(mock, cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)

	go func() {
		stopped <- d.Start(ctx)
	}()

	// Wait for at least 3 cycle triggers
	for i := 0; i < 3; i++ {
		select {
		case <-mock.cycleStarts:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for cycle %d", i+1)
		}
	}

	cancel()

	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("daemon did not stop after cancel")
	}

	if count := mock.getCount(); count < 3 {
		t.Fatalf("expected at least 3 cycles, got %d", count)
	}
}

func TestDaemon_ContextCancellationStopsLoop(t *testing.T) {
	cfg := makeTestConfig(1*time.Hour, false)
	mock := newMockSyncer()

	d, err := daemon.New(mock, cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)

	go func() {
		stopped <- d.Start(ctx)
	}()

	// Wait for immediate cycle to complete
	<-mock.cycleStarts
	time.Sleep(20 * time.Millisecond) // ensure loop is waiting on ticker

	cancel()

	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("expected clean exit (nil error), got: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("daemon did not stop on context cancellation")
	}
}

func TestDaemon_GracefulDrainAwaitsInFlight(t *testing.T) {
	cfg := makeTestConfig(1*time.Hour, false)
	mock := newMockSyncer()

	cycleBlock := make(chan struct{})
	cycleFinished := make(chan struct{})

	mock.setRunFunc(func(ctx context.Context) (*syncer.SyncStats, error) {
		<-cycleBlock // simulate long-running in-flight sync operation
		close(cycleFinished)
		return &syncer.SyncStats{}, nil
	})

	d, err := daemon.New(mock, cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)

	go func() {
		stopped <- d.Start(ctx)
	}()

	// Wait for cycle to start
	<-mock.cycleStarts

	// Assert daemon reports in-flight status
	if !d.IsInFlight() {
		t.Fatal("expected IsInFlight to be true during active cycle")
	}

	// Cancel context while cycle is blocked in flight
	cancel()

	// Ensure Start has not exited yet (still awaiting in-flight drain)
	select {
	case <-stopped:
		t.Fatal("daemon returned before in-flight cycle completed drain")
	case <-time.After(50 * time.Millisecond):
		// Expected: Start is still waiting
	}

	// Release in-flight cycle
	close(cycleBlock)

	// Await cycle completion and clean daemon exit
	<-cycleFinished

	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for daemon to stop after drain")
	}

	if d.IsInFlight() {
		t.Fatal("expected IsInFlight to be false after drain complete")
	}
}

func TestDaemon_OverlappingCycleSkipped(t *testing.T) {
	// Rapid ticker interval
	cfg := makeTestConfig(10*time.Millisecond, false)
	mock := newMockSyncer()

	blockCh := make(chan struct{})
	mock.setRunFunc(func(ctx context.Context) (*syncer.SyncStats, error) {
		<-blockCh
		return &syncer.SyncStats{}, nil
	})

	d, err := daemon.New(mock, cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)

	go func() {
		stopped <- d.Start(ctx)
	}()

	// Wait for initial cycle to start and block
	<-mock.cycleStarts

	// Sleep 50ms while cycle is blocked — multiple ticker ticks will fire and be skipped
	time.Sleep(50 * time.Millisecond)

	// Unblock cycle
	close(blockCh)
	time.Sleep(20 * time.Millisecond)

	cancel()
	<-stopped

	// Count should be strictly 1 or at most 2, never dozens of concurrent cycles
	if count := mock.getCount(); count > 3 {
		t.Fatalf("overlapping cycles were not skipped, count: %d", count)
	}
}

func TestDaemon_CycleError_ContinuousModeContinues(t *testing.T) {
	cfg := makeTestConfig(15*time.Millisecond, false)
	mock := newMockSyncer()

	first := true
	mock.setRunFunc(func(ctx context.Context) (*syncer.SyncStats, error) {
		if first {
			first = false
			return nil, errors.New("transient network error")
		}
		return &syncer.SyncStats{DiscoveredCount: 2}, nil
	})

	d, err := daemon.New(mock, cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)

	go func() {
		stopped <- d.Start(ctx)
	}()

	// Wait for first cycle (which fails) and second cycle (which succeeds)
	<-mock.cycleStarts
	<-mock.cycleStarts

	cancel()
	err = <-stopped
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	if count := mock.getCount(); count < 2 {
		t.Fatalf("expected at least 2 cycles executed despite error, got %d", count)
	}
}

func TestNewLogger_LevelsAndFormats(t *testing.T) {
	tests := []struct {
		name       string
		level      string
		format     string
		logMsg     string
		shouldFind string
	}{
		{
			name:       "Text Info",
			level:      "info",
			format:     "text",
			logMsg:     "hello text info",
			shouldFind: "hello text info",
		},
		{
			name:       "JSON Error",
			level:      "error",
			format:     "json",
			logMsg:     "critical error occurred",
			shouldFind: `"msg":"critical error occurred"`,
		},
		{
			name:       "Debug Level Filtering",
			level:      "warn",
			format:     "text",
			logMsg:     "ignored debug",
			shouldFind: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			cfg := config.LoggingConfig{Level: tc.level, Format: tc.format}
			logger := daemon.NewLogger(cfg, buf)

			if tc.level == "warn" {
				logger.Debug(tc.logMsg)
			} else if tc.level == "error" {
				logger.Error(tc.logMsg)
			} else {
				logger.Info(tc.logMsg)
			}

			out := buf.String()
			if tc.shouldFind != "" {
				if !strings.Contains(out, tc.shouldFind) {
					t.Fatalf("expected log output to contain %q, got: %s", tc.shouldFind, out)
				}
			} else {
				if strings.Contains(out, tc.logMsg) {
					t.Fatalf("expected log %q to be filtered out at level %s, got: %s", tc.logMsg, tc.level, out)
				}
			}

			if tc.format == "json" && tc.shouldFind != "" {
				var parsed map[string]any
				if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
					t.Fatalf("failed to unmarshal JSON log: %v", err)
				}
			}
		})
	}
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
func TestDaemon_HTTP_CurrentMatch_NilTracker(t *testing.T) {
	cfg := makeTestConfig(5*time.Minute, false)
	d, err := daemon.New(newMockSyncer(), cfg)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	rec := executeTestRequest(d, http.MethodGet, "/current-match", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("expected application/json, got %s", ct)
	}

	var resp playertrack.CurrentMatchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response JSON: %v", err)
	}

	if resp.ActiveMatch {
		t.Errorf("expected active_match = false, got true")
	}
	if resp.Teammates == nil || resp.Opponents == nil {
		t.Errorf("expected non-nil empty slices for teammates and opponents")
	}
}

// 2. No Active Match: Tracker attached, but no game in progress
func TestDaemon_HTTP_CurrentMatch_NoActiveMatch(t *testing.T) {
	store := newTestStore(t, backendSQLite)
	tracker := newTestTracker(t, store, nil)
	cfg := makeTestConfig(5*time.Minute, false)

	d, err := daemon.New(newMockSyncer(), cfg,
		daemon.WithPlayerTracker(tracker),
		daemon.WithStateStore(store),
	)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	rec := executeTestRequest(d, http.MethodGet, "/current-match", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp playertrack.CurrentMatchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}

	if resp.ActiveMatch {
		t.Errorf("expected active_match = false")
	}
	if resp.MatchGUID != "" {
		t.Errorf("expected empty match_guid, got %q", resp.MatchGUID)
	}
	if len(resp.Teammates) != 0 || len(resp.Opponents) != 0 {
		t.Errorf("expected 0 teammates and 0 opponents")
	}
}

// 3. Active Match In Progress: Complete Lobby Payload
func TestDaemon_HTTP_CurrentMatch_ActiveMatch_CompletePayload(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, backendSQLite)

	// Seed existing head-to-head records
	tmID := "Steam|76561198000000002|0"
	oppID := "Epic|epic_opp_001|0"
	_ = store.RecordMatchResults(ctx, "historical-guid-1", 11, []storage.PlayerOutcome{
		{PlayerID: tmID, Platform: "Steam", PlayerName: "Teammate1", IsTeammate: true, Won: true},
		{PlayerID: oppID, Platform: "Epic", PlayerName: "Opponent1", IsTeammate: false, Won: true},
	})

	// Setup mock rank fetcher
	mockFetcher := playertrack.NewMockSkillFetcher()
	mockFetcher.SkillsMap[rlapi.PlayerID(tmID)] = []rlapi.Skill{
		{Playlist: 11, Tier: 15, Division: 2, MMR: 980.5, MatchesPlayed: 45},
	}
	mockFetcher.SkillsMap[rlapi.PlayerID(oppID)] = []rlapi.Skill{
		{Playlist: 11, Tier: 16, Division: 0, MMR: 1050.2, MatchesPlayed: 120},
	}

	tracker := newTestTracker(t, store, mockFetcher)

	// Ingest UpdateState frame
	localID := "Epic|my_epic_acc_001|0"
	players := []statsapi.StatsPlayer{
		{PrimaryId: localID, Name: "Me", TeamNum: 0, Score: 100},
		{PrimaryId: tmID, Name: "Teammate1", TeamNum: 0, Score: 150, Goals: 1},
		{PrimaryId: oppID, Name: "Opponent1", TeamNum: 1, Score: 80, Saves: 2},
	}
	if err := tracker.OnUpdateState(ctx, "live-guid-999", 11, players); err != nil {
		t.Fatalf("OnUpdateState failed: %v", err)
	}

	// Allow async rank fetch to settle
	for i := 0; i < 50; i++ {
		match := tracker.GetCurrentMatch()
		if len(match.Teammates) > 0 && match.Teammates[0].CurrentRank != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	cfg := makeTestConfig(5*time.Minute, false)
	d, err := daemon.New(newMockSyncer(), cfg,
		daemon.WithPlayerTracker(tracker),
		daemon.WithStateStore(store),
	)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	rec := executeTestRequest(d, http.MethodGet, "/current-match", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp playertrack.CurrentMatchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}

	// Verify top-level match metadata
	if !resp.ActiveMatch {
		t.Errorf("expected active_match = true")
	}
	if resp.MatchEnded {
		t.Errorf("expected match_ended = false")
	}
	if resp.MatchGUID != "live-guid-999" {
		t.Errorf("expected match_guid 'live-guid-999', got %q", resp.MatchGUID)
	}
	if resp.PlaylistID != 11 {
		t.Errorf("expected playlist_id 11, got %d", resp.PlaylistID)
	}
	if resp.PlaylistName != "Ranked Doubles (2v2)" {
		t.Errorf("expected playlist_name 'Ranked Doubles (2v2)', got %q", resp.PlaylistName)
	}
	if resp.LocalTeam == nil || *resp.LocalTeam != 0 {
		t.Errorf("expected local_team = 0, got %v", resp.LocalTeam)
	}

	// Verify Local Player
	if resp.LocalPlayer == nil {
		t.Fatal("expected local_player to be populated")
	}
	if resp.LocalPlayer.PlayerID != localID || !resp.LocalPlayer.IsLocal {
		t.Errorf("unexpected local player details: %+v", resp.LocalPlayer)
	}

	// Verify Teammates
	if len(resp.Teammates) != 1 {
		t.Fatalf("expected 1 teammate, got %d", len(resp.Teammates))
	}
	tm := resp.Teammates[0]
	if tm.PlayerID != tmID || tm.Name != "Teammate1" || tm.TeamNum != 0 {
		t.Errorf("unexpected teammate details: %+v", tm)
	}
	if tm.Stats.Goals != 1 {
		t.Errorf("expected 1 goal, got %d", tm.Stats.Goals)
	}
	if tm.MatchupRecord == nil || tm.MatchupRecord.WinsAsTeammate != 1 {
		t.Errorf("expected 1 win as teammate in matchup record, got %+v", tm.MatchupRecord)
	}
	if tm.CurrentRank == nil || tm.CurrentRank.RankName != "Diamond III Division III" {
		t.Errorf("expected 'Diamond III Division III', got %+v", tm.CurrentRank)
	}

	// Verify Opponents
	if len(resp.Opponents) != 1 {
		t.Fatalf("expected 1 opponent, got %d", len(resp.Opponents))
	}
	opp := resp.Opponents[0]
	if opp.PlayerID != oppID || opp.Name != "Opponent1" || opp.TeamNum != 1 {
		t.Errorf("unexpected opponent details: %+v", opp)
	}
	if opp.MatchupRecord == nil || opp.MatchupRecord.WinsAsOpponent != 1 {
		t.Errorf("expected 1 win as opponent, got %+v", opp.MatchupRecord)
	}
}

// 4. Concluded Match: Result and WinnerTeam populated
func TestDaemon_HTTP_CurrentMatch_ConcludedMatch(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, backendSQLite)
	tracker := newTestTracker(t, store, nil)

	localID := "Epic|my_epic_acc_001|0"
	players := []statsapi.StatsPlayer{
		{PrimaryId: localID, Name: "Me", TeamNum: 0},
		{PrimaryId: "Epic|opp_001|0", Name: "Opp", TeamNum: 1},
	}
	_ = tracker.OnUpdateState(ctx, "guid-finish-1", 11, players)

	winner := 0
	_ = tracker.OnMatchEnded(ctx, "guid-finish-1", &winner)

	cfg := makeTestConfig(5*time.Minute, false)
	d, _ := daemon.New(newMockSyncer(), cfg, daemon.WithPlayerTracker(tracker), daemon.WithStateStore(store))

	rec := executeTestRequest(d, http.MethodGet, "/current-match", nil)
	var resp playertrack.CurrentMatchResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)

	if resp.ActiveMatch {
		t.Errorf("expected active_match = false after match end")
	}
	if !resp.MatchEnded {
		t.Errorf("expected match_ended = true")
	}
	if resp.WinnerTeam == nil || *resp.WinnerTeam != 0 {
		t.Errorf("expected winner_team = 0, got %v", resp.WinnerTeam)
	}
	if resp.Result != "victory" {
		t.Errorf("expected result 'victory', got %q", resp.Result)
	}
}

// 5. Method Not Allowed: POST /current-match
func TestDaemon_HTTP_CurrentMatch_MethodNotAllowed(t *testing.T) {
	cfg := makeTestConfig(5*time.Minute, false)
	d, _ := daemon.New(newMockSyncer(), cfg)

	rec := executeTestRequest(d, http.MethodPost, "/current-match", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 Method Not Allowed, got %d", rec.Code)
	}
}

func TestDaemon_HTTP_ListPlayers_DualBackend(t *testing.T) {
	for _, backend := range allStorageBackends {
		t.Run(string(backend), func(t *testing.T) {
			store := newTestStore(t, backend)
			cfg := makeTestConfig(5*time.Minute, false)
			d, err := daemon.New(newMockSyncer(), cfg, daemon.WithStateStore(store))
			if err != nil {
				t.Fatalf("daemon.New failed: %v", err)
			}

			// Subtest 1: Empty Store -> 200 OK []
			t.Run("EmptyStore", func(t *testing.T) {
				rec := executeTestRequest(d, http.MethodGet, "/players", nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("expected 200, got %d", rec.Code)
				}
				bodyStr := strings.TrimSpace(rec.Body.String())
				if bodyStr != "[]" {
					t.Errorf("expected '[]', got %q", bodyStr)
				}
			})

			// Seed 10 test players with distinct timestamps
			ctx := context.Background()
			now := time.Now().UTC()
			for i := 1; i <= 10; i++ {
				pid := fmt.Sprintf("Steam|765611980000000%02d|0", i)
				pName := fmt.Sprintf("Player_%02d", i)
				// Seed matchup for aggregation
				_ = store.RecordMatchResults(ctx, fmt.Sprintf("guid-%d", i), 11, []storage.PlayerOutcome{
					{PlayerID: pid, Platform: "Steam", PlayerName: pName, IsTeammate: i%2 == 0, Won: true},
				})
				_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
					PlayerID:    pid,
					Platform:    "Steam",
					PlayerName:  pName,
					RanksJSON:   "{}",
					FirstSeenAt: now.Add(time.Duration(i) * time.Minute),
					LastSeenAt:  now.Add(time.Duration(i) * time.Minute),
				})
			}

			// Subtest 2: Default Pagination (No params) -> returns all 10 in DESC order
			t.Run("DefaultPagination", func(t *testing.T) {
				rec := executeTestRequest(d, http.MethodGet, "/players", nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("expected 200, got %d", rec.Code)
				}
				var players []*storage.PlayerSummary
				if err := json.Unmarshal(rec.Body.Bytes(), &players); err != nil {
					t.Fatalf("decode JSON failed: %v", err)
				}
				if len(players) != 10 {
					t.Fatalf("expected 10 players, got %d", len(players))
				}
				// Verify DESC order: Player_10 was seen most recently
				if players[0].PlayerName != "Player_10" {
					t.Errorf("expected first player to be Player_10, got %s", players[0].PlayerName)
				}
				if players[9].PlayerName != "Player_01" {
					t.Errorf("expected last player to be Player_01, got %s", players[9].PlayerName)
				}
			})

			// Subtest 3: Limit and Offset Pagination Slices
			t.Run("Paging_LimitAndOffset", func(t *testing.T) {
				// Page 1: limit=3, offset=0 -> Player_10, Player_09, Player_08
				rec1 := executeTestRequest(d, http.MethodGet, "/players?limit=3&offset=0", nil)
				var page1 []*storage.PlayerSummary
				_ = json.Unmarshal(rec1.Body.Bytes(), &page1)
				if len(page1) != 3 || page1[0].PlayerName != "Player_10" || page1[2].PlayerName != "Player_08" {
					t.Errorf("unexpected page 1: %+v", page1)
				}

				// Page 2: limit=3, offset=3 -> Player_07, Player_06, Player_05
				rec2 := executeTestRequest(d, http.MethodGet, "/players?limit=3&offset=3", nil)
				var page2 []*storage.PlayerSummary
				_ = json.Unmarshal(rec2.Body.Bytes(), &page2)
				if len(page2) != 3 || page2[0].PlayerName != "Player_07" || page2[2].PlayerName != "Player_05" {
					t.Errorf("unexpected page 2: %+v", page2)
				}

				// Page 4: limit=3, offset=9 -> Player_01 (1 item)
				rec4 := executeTestRequest(d, http.MethodGet, "/players?limit=3&offset=9", nil)
				var page4 []*storage.PlayerSummary
				_ = json.Unmarshal(rec4.Body.Bytes(), &page4)
				if len(page4) != 1 || page4[0].PlayerName != "Player_01" {
					t.Errorf("unexpected page 4: %+v", page4)
				}

				// Page Beyond: offset=50 -> empty []
				recBeyond := executeTestRequest(d, http.MethodGet, "/players?limit=10&offset=50", nil)
				var pageBeyond []*storage.PlayerSummary
				_ = json.Unmarshal(recBeyond.Body.Bytes(), &pageBeyond)
				if len(pageBeyond) != 0 {
					t.Errorf("expected 0 items, got %d", len(pageBeyond))
				}
			})

			// Subtest 4: Query Param Clamping & Malformed String Recovery
			t.Run("ParamClamping_And_Fallbacks", func(t *testing.T) {
				// Negative values clamped: limit=-5, offset=-10 -> should treat as limit=1 (or default 50), offset=0
				recNeg := executeTestRequest(d, http.MethodGet, "/players?limit=-5&offset=-10", nil)
				if recNeg.Code != http.StatusOK {
					t.Errorf("expected 200 OK for negative params, got %d", recNeg.Code)
				}

				// High limit clamped to 100: limit=500
				recHigh := executeTestRequest(d, http.MethodGet, "/players?limit=500", nil)
				if recHigh.Code != http.StatusOK {
					t.Errorf("expected 200 OK for high limit, got %d", recHigh.Code)
				}

				// Malformed non-numeric values: limit=abc&offset=xyz -> fallback to default
				recAlpha := executeTestRequest(d, http.MethodGet, "/players?limit=abc&offset=xyz", nil)
				if recAlpha.Code != http.StatusOK {
					t.Errorf("expected 200 OK for alphabetic params, got %d", recAlpha.Code)
				}
				var alphaResp []*storage.PlayerSummary
				_ = json.Unmarshal(recAlpha.Body.Bytes(), &alphaResp)
				if len(alphaResp) != 10 {
					t.Errorf("expected all 10 players on fallback default, got %d", len(alphaResp))
				}
			})
		})
	}
}

func TestDaemon_HTTP_ListPlayers_NilStore(t *testing.T) {
	cfg := makeTestConfig(5*time.Minute, false)
	d, _ := daemon.New(newMockSyncer(), cfg) // stateStore is nil

	rec := executeTestRequest(d, http.MethodGet, "/players", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("expected '[]' on nil store, got %q", rec.Body.String())
	}
}

func TestDaemon_HTTP_ListPlayers_MethodNotAllowed(t *testing.T) {
	cfg := makeTestConfig(5*time.Minute, false)
	d, _ := daemon.New(newMockSyncer(), cfg)

	rec := executeTestRequest(d, http.MethodPost, "/players", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 Method Not Allowed, got %d", rec.Code)
	}
}

func TestDaemon_HTTP_GetPlayer_DualBackend(t *testing.T) {
	for _, backend := range allStorageBackends {
		t.Run(string(backend), func(t *testing.T) {
			store := newTestStore(t, backend)
			cfg := makeTestConfig(5*time.Minute, false)
			d, err := daemon.New(newMockSyncer(), cfg, daemon.WithStateStore(store))
			if err != nil {
				t.Fatalf("daemon.New failed: %v", err)
			}

			ctx := context.Background()
			targetSteamID := "Steam|76561198012345678|0"
			now := time.Now().UTC()

			// Seed player profile
			_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
				PlayerID:    targetSteamID,
				Platform:    "Steam",
				PlayerName:  "TargetPlayer",
				RanksJSON:   `{"11":{"tier":16,"division":1}}`,
				FirstSeenAt: now,
				LastSeenAt:  now,
			})

			// Seed 3 per-playlist matchups: Playlist 10 (1v1), 11 (2v2), 13 (3v3)
			_ = store.RecordMatchResults(ctx, "m1", 10, []storage.PlayerOutcome{
				{PlayerID: targetSteamID, Platform: "Steam", PlayerName: "TargetPlayer", IsTeammate: false, Won: true},
			})
			_ = store.RecordMatchResults(ctx, "m2", 11, []storage.PlayerOutcome{
				{PlayerID: targetSteamID, Platform: "Steam", PlayerName: "TargetPlayer", IsTeammate: true, Won: true},
				{PlayerID: targetSteamID, Platform: "Steam", PlayerName: "TargetPlayer", IsTeammate: true, Won: false},
			})
			_ = store.RecordMatchResults(ctx, "m3", 13, []storage.PlayerOutcome{
				{PlayerID: targetSteamID, Platform: "Steam", PlayerName: "TargetPlayer", IsTeammate: false, Won: false},
			})

			// Subtest 1: URL Unescaping of Pipe (%7C)
			t.Run("URLUnescaping_PipeEncoded", func(t *testing.T) {
				escapedPath := "/players/Steam%7C76561198012345678%7C0"
				rec := executeTestRequest(d, http.MethodGet, escapedPath, nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
				}

				var payload struct {
					*storage.PlayerRecord
					Matchups []*storage.PlayerMatchup `json:"matchups"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
					t.Fatalf("failed to decode JSON: %v", err)
				}

				if payload.PlayerID != targetSteamID {
					t.Errorf("expected player_id %q, got %q", targetSteamID, payload.PlayerID)
				}
				if payload.PlayerName != "TargetPlayer" {
					t.Errorf("expected player_name 'TargetPlayer', got %q", payload.PlayerName)
				}
				if len(payload.Matchups) != 3 {
					t.Fatalf("expected 3 playlist matchups, got %d", len(payload.Matchups))
				}
				// Verify ordering by playlist_id ASC (10, 11, 13)
				if payload.Matchups[0].PlaylistID != 10 || payload.Matchups[1].PlaylistID != 11 || payload.Matchups[2].PlaylistID != 13 {
					t.Errorf("matchups not sorted by playlist_id ASC: %+v", payload.Matchups)
				}
			})

			// Subtest 2: Raw Pipe Request (Unencoded)
			t.Run("RawPipe_Request", func(t *testing.T) {
				rawPath := "/players/Steam|76561198012345678|0"
				rec := executeTestRequest(d, http.MethodGet, rawPath, nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("expected 200 OK, got %d", rec.Code)
				}
			})

			// Subtest 3: Player With Zero Matchups
			t.Run("ZeroMatchups_EmptyArray", func(t *testing.T) {
				zeroID := "Epic|zero_matchups_user|0"
				_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
					PlayerID:    zeroID,
					Platform:    "Epic",
					PlayerName:  "ZeroUser",
					FirstSeenAt: now,
					LastSeenAt:  now,
				})

				rec := executeTestRequest(d, http.MethodGet, "/players/Epic%7Czero_matchups_user%7C0", nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("expected 200 OK, got %d", rec.Code)
				}

				var payload struct {
					*storage.PlayerRecord
					Matchups []*storage.PlayerMatchup `json:"matchups"`
				}
				_ = json.Unmarshal(rec.Body.Bytes(), &payload)

				if payload.PlayerID != zeroID {
					t.Errorf("expected %q, got %q", zeroID, payload.PlayerID)
				}
				if payload.Matchups == nil || len(payload.Matchups) != 0 {
					t.Errorf("expected empty non-nil slice [], got %+v", payload.Matchups)
				}
			})

			// Subtest 4: Nonexistent Player -> 404 Not Found
			t.Run("NotFound_404", func(t *testing.T) {
				rec := executeTestRequest(d, http.MethodGet, "/players/Steam%7Cnonexistent_id%7C0", nil)
				if rec.Code != http.StatusNotFound {
					t.Fatalf("expected 404 Not Found, got %d", rec.Code)
				}
				if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
					t.Errorf("expected application/json, got %s", ct)
				}

				var errResp map[string]string
				_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
				if errResp["error"] != "player not found" {
					t.Errorf("expected error 'player not found', got %q", errResp["error"])
				}
			})

			// Subtest 5: Malformed URL Escaping -> 400 Bad Request
			t.Run("MalformedURLEscaping_400", func(t *testing.T) {
				// %7 is an incomplete hex escape set directly on Path
				req, _ := http.NewRequest(http.MethodGet, "http://localhost", nil)
				req.URL.Path = "/players/Steam%7"
				rec := httptest.NewRecorder()
				d.Handler(context.Background()).ServeHTTP(rec, req)

				if rec.Code != http.StatusBadRequest {
					t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
				}
				var errResp map[string]string
				_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
				if errResp["error"] == "" {
					t.Errorf("expected non-empty error message")
				}
			})
		})
	}
}

func TestDaemon_HTTP_GetPlayer_NilStore(t *testing.T) {
	cfg := makeTestConfig(5*time.Minute, false)
	d, _ := daemon.New(newMockSyncer(), cfg) // Nil store

	rec := executeTestRequest(d, http.MethodGet, "/players/Steam%7C123%7C0", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestDaemon_HTTP_GetPlayer_MethodNotAllowed(t *testing.T) {
	cfg := makeTestConfig(5*time.Minute, false)
	d, _ := daemon.New(newMockSyncer(), cfg)

	rec := executeTestRequest(d, http.MethodDelete, "/players/Steam%7C123%7C0", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 Method Not Allowed, got %d", rec.Code)
	}
}

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

func TestDaemon_HTTP_ConcurrentAccessAndRaceSafety(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	store := newTestStore(t, backendSQLite)
	fetcher := playertrack.NewMockSkillFetcher()
	tracker := newTestTracker(t, store, fetcher)

	cfg := makeTestConfig(10*time.Millisecond, false)
	d, err := daemon.New(newMockSyncer(), cfg,
		daemon.WithPlayerTracker(tracker),
		daemon.WithStateStore(store),
	)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	// Pre-seed baseline players
	for i := 1; i <= 5; i++ {
		pid := fmt.Sprintf("Steam|765611980000000%d|0", i)
		_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
			PlayerID:    pid,
			Platform:    "Steam",
			PlayerName:  fmt.Sprintf("Player_%d", i),
			RanksJSON:   "{}",
			FirstSeenAt: time.Now().UTC(),
			LastSeenAt:  time.Now().UTC(),
		})
	}

	var wg sync.WaitGroup

	// Worker Group 1: 10 Concurrent HTTP Readers
	endpoints := []string{
		"/current-match",
		"/players",
		"/players?limit=2&offset=1",
		"/players/Steam%7C7656119800000001%7C0",
		"/players/Steam%7Cnonexistent%7C0",
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					path := endpoints[workerID%len(endpoints)]
					rec := executeTestRequest(d, http.MethodGet, path, nil)
					if rec.Code != http.StatusOK && rec.Code != http.StatusNotFound {
						t.Errorf("worker %d: unexpected status %d for %s", workerID, rec.Code, path)
					}
					// Verify valid JSON
					var dummy interface{}
					if err := json.Unmarshal(rec.Body.Bytes(), &dummy); err != nil {
						t.Errorf("worker %d: JSON parse error on %s: %v", workerID, path, err)
					}
				}
			}
		}(i)
	}

	// Worker Group 2: 3 Concurrent In-Game Lobby Updaters
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(updaterID int) {
			defer wg.Done()
			tick := 0
			for {
				select {
				case <-ctx.Done():
					return
				default:
					tick++
					guid := fmt.Sprintf("race-guid-%d", updaterID)
					players := []statsapi.StatsPlayer{
						{PrimaryId: "Epic|my_epic_acc_001|0", Name: "Me", TeamNum: 0},
						{PrimaryId: fmt.Sprintf("Steam|765611980000000%d|0", (tick%5)+1), Name: "Tm", TeamNum: 0},
						{PrimaryId: fmt.Sprintf("Epic|opp_%d|0", tick%10), Name: "Opp", TeamNum: 1},
					}
					_ = tracker.OnUpdateState(ctx, guid, 11, players)
					time.Sleep(2 * time.Millisecond)
				}
			}
		}(i)
	}

	// Worker Group 3: 2 Concurrent Match Completion Updaters
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(enderID int) {
			defer wg.Done()
			tick := 0
			for {
				select {
				case <-ctx.Done():
					return
				default:
					tick++
					guid := fmt.Sprintf("race-guid-%d", enderID)
					winner := tick % 2
					_ = tracker.OnMatchEnded(ctx, guid, &winner)
					time.Sleep(5 * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()
}
