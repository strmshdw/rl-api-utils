package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/syncer"
)

type dummySyncer struct{}

func (d *dummySyncer) RunCycle(ctx context.Context) (*syncer.SyncStats, error) {
	return &syncer.SyncStats{}, nil
}

func TestDaemon_MatchesEndpoints(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_matches.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Seed matches
	stats1 := &storage.ReplayMatchStats{
		MatchGUID:   "guid-alpha",
		MapName:     "DFH Stadium",
		Playlist:    11,
		BlueScore:   3,
		OrangeScore: 2,
		Date:        "2026-10-12T12:00:00Z",
		Players: []storage.ReplayPlayerStats{
			{Name: "PlayerOne", Platform: "Epic", OnlineID: "p1", Team: 0, Score: 500, Goals: 2},
			{Name: "PlayerTwo", Platform: "Steam", OnlineID: "p2", Team: 1, Score: 300, Goals: 1},
		},
	}
	if err := store.SyncReplayStats(ctx, stats1); err != nil {
		t.Fatalf("failed to sync replay stats: %v", err)
	}

	stats2 := &storage.ReplayMatchStats{
		MatchGUID:   "guid-beta",
		MapName:     "Utopia Coliseum",
		Playlist:    13,
		BlueScore:   1,
		OrangeScore: 4,
		Date:        "2026-10-12T13:00:00Z",
		Players: []storage.ReplayPlayerStats{
			{Name: "PlayerOne", Platform: "Epic", OnlineID: "p1", Team: 1, Score: 400, Goals: 3},
		},
	}
	if err := store.SyncReplayStats(ctx, stats2); err != nil {
		t.Fatalf("failed to sync replay stats 2: %v", err)
	}

	cfg := &config.Config{
		Sync: config.SyncConfig{
			Once: false,
		},
	}

	d, err := New(&dummySyncer{}, cfg, WithStateStore(store))
	if err != nil {
		t.Fatalf("failed to create daemon: %v", err)
	}

	handler := d.Handler(ctx)

	// 1. List all matches
	req := httptest.NewRequest(http.MethodGet, "/api/matches", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/matches status = %d, want 200", rec.Code)
	}

	var resp MatchesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal MatchesResponse failed: %v", err)
	}
	if resp.Total != 2 {
		t.Errorf("expected total 2, got %d", resp.Total)
	}
	if len(resp.Matches) != 2 {
		t.Errorf("expected 2 matches, got %d", len(resp.Matches))
	}

	// 2. Filter by playlist
	req = httptest.NewRequest(http.MethodGet, "/api/matches?playlist=11", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal MatchesResponse failed: %v", err)
	}
	if resp.Total != 1 || len(resp.Matches) != 1 {
		t.Errorf("expected 1 match for playlist 11, got %d (total %d)", len(resp.Matches), resp.Total)
	}

	// 3. Search query
	req = httptest.NewRequest(http.MethodGet, "/api/matches?query=Utopia", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal MatchesResponse failed: %v", err)
	}
	if resp.Total != 1 || resp.Matches[0].Stats.MapName != "Utopia Coliseum" {
		t.Errorf("expected Utopia match, got %+v", resp.Matches)
	}

	// 4. Get specific match
	req = httptest.NewRequest(http.MethodGet, "/api/matches/guid-alpha", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/matches/guid-alpha status = %d, want 200", rec.Code)
	}
	var matchRec storage.HistoricalMatchRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &matchRec); err != nil {
		t.Fatalf("unmarshal HistoricalMatchRecord failed: %v", err)
	}
	if matchRec.MatchGUID != "guid-alpha" {
		t.Errorf("expected match guid-alpha, got %s", matchRec.MatchGUID)
	}

	// 5. Get non-existent match
	req = httptest.NewRequest(http.MethodGet, "/api/matches/non-existent", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-existent match, got %d", rec.Code)
	}
}

func TestDaemon_BallchasingSyncEndpoints(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{
		Sync: config.SyncConfig{Once: false},
	}

	// Test endpoints without history syncer
	dNoSync, err := New(&dummySyncer{}, cfg)
	if err != nil {
		t.Fatalf("New daemon failed: %v", err)
	}
	hNoSync := dNoSync.Handler(ctx)

	// Status when not configured
	req := httptest.NewRequest(http.MethodGet, "/api/sync/ballchasing/status", nil)
	rec := httptest.NewRecorder()
	hNoSync.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for status, got %d", rec.Code)
	}
	var prog syncer.BallchasingHistorySyncProgress
	if err := json.Unmarshal(rec.Body.Bytes(), &prog); err != nil {
		t.Fatalf("unmarshal progress failed: %v", err)
	}
	if prog.Running {
		t.Errorf("expected running to be false")
	}

	// POST trigger when not configured -> 400
	req = httptest.NewRequest(http.MethodPost, "/api/sync/ballchasing", nil)
	rec = httptest.NewRecorder()
	hNoSync.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when history syncer not configured, got %d", rec.Code)
	}

	// Now configure mock ballchasing client and history syncer
	bcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/" {
			_ = json.NewEncoder(w).Encode(ballchasing.PingResponse{Name: "Tester"})
			return
		}
		_ = json.NewEncoder(w).Encode(ballchasing.ReplayListResponse{
			Count: 0,
			List:  []ballchasing.ReplayItem{},
			Next:  "",
		})
	}))
	defer bcSrv.Close()

	bcClient, err := ballchasing.NewClient(ballchasing.ClientConfig{
		BaseURL: bcSrv.URL,
		APIKey:  "dummy-key",
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	tempDir := t.TempDir()
	store, _ := storage.NewSQLiteStore(filepath.Join(tempDir, "test.db"))
	defer store.Close()

	historySyncer := syncer.NewBallchasingHistorySyncer(bcClient, store, "Epic|test|0", "Tester", nil)
	historySyncer.SetRateInterval(time.Millisecond)

	dWithSync, err := New(&dummySyncer{}, cfg, WithHistorySyncer(historySyncer), WithStateStore(store))
	if err != nil {
		t.Fatalf("New daemon failed: %v", err)
	}
	hWithSync := dWithSync.Handler(ctx)

	// POST trigger -> 202 Accepted
	req = httptest.NewRequest(http.MethodPost, "/api/sync/ballchasing", nil)
	rec = httptest.NewRecorder()
	hWithSync.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d: %s", rec.Code, rec.Body.String())
	}

	// Check status
	time.Sleep(50 * time.Millisecond)
	req = httptest.NewRequest(http.MethodGet, "/api/sync/ballchasing/status", nil)
	rec = httptest.NewRecorder()
	hWithSync.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// TestDaemon_GetMatch_LocalRelativeOutcome verifies that GET /api/matches/{guid}
// dynamically resolves the local player's team and evaluates outcome relative to local team.
func TestDaemon_GetMatch_LocalRelativeOutcome(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_getmatch_outcome.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	localPID := "Steam|76561198000000099|0"
	_ = store.SaveAuthState(ctx, "steam", "test-token", localPID, "LocalHero")

	// Match 1: Local on Orange (team 1), Orange wins 3-1 -> should be VICTORY for local player
	guidOrangeWin := "guid-orange-win-test"
	_ = store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
		MatchGUID:   guidOrangeWin,
		MapName:     "Mannfield",
		Playlist:    11,
		BlueScore:   1,
		OrangeScore: 3,
		Date:        time.Now().UTC().Format(time.RFC3339),
	})
	wonTrue := true
	_ = store.SaveMatchPlayers(ctx, guidOrangeWin, []*storage.MatchPlayerRecord{
		{MatchGUID: guidOrangeWin, PlayerID: localPID, TeamNum: 1, IsLocal: true, Won: &wonTrue, Score: 450},
		{MatchGUID: guidOrangeWin, PlayerID: "Steam|opponent|0", TeamNum: 0, IsLocal: false, Score: 300},
	})

	// Match 2: Local on Blue (team 0), Orange wins 3-1 -> should be DEFEAT for local player
	guidBlueLoss := "guid-blue-loss-test"
	_ = store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
		MatchGUID:   guidBlueLoss,
		MapName:     "Champions Field",
		Playlist:    11,
		BlueScore:   1,
		OrangeScore: 3,
		Date:        time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339),
	})
	wonFalse := false
	_ = store.SaveMatchPlayers(ctx, guidBlueLoss, []*storage.MatchPlayerRecord{
		{MatchGUID: guidBlueLoss, PlayerID: localPID, TeamNum: 0, IsLocal: true, Won: &wonFalse, Score: 200},
		{MatchGUID: guidBlueLoss, PlayerID: "Steam|opponent|0", TeamNum: 1, IsLocal: false, Score: 500},
	})

	cfg := &config.Config{}
	d, err := New(&dummySyncer{}, cfg, WithStateStore(store))
	if err != nil {
		t.Fatalf("failed to create daemon: %v", err)
	}
	handler := d.Handler(ctx)

	// Test Match 1: Orange Win with Local on Orange -> Victory
	req1 := httptest.NewRequest(http.MethodGet, "/api/matches/"+guidOrangeWin, nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("GET /api/matches/%s failed: %d", guidOrangeWin, rec1.Code)
	}
	var res1 storage.HistoricalMatchRecord
	if err := json.Unmarshal(rec1.Body.Bytes(), &res1); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if res1.Result != "victory" {
		t.Errorf("expected Result 'victory' for local Orange win, got %q", res1.Result)
	}

	// Test Match 2: Orange Win with Local on Blue -> Defeat
	req2 := httptest.NewRequest(http.MethodGet, "/api/matches/"+guidBlueLoss, nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("GET /api/matches/%s failed: %d", guidBlueLoss, rec2.Code)
	}
	var res2 storage.HistoricalMatchRecord
	if err := json.Unmarshal(rec2.Body.Bytes(), &res2); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if res2.Result != "defeat" {
		t.Errorf("expected Result 'defeat' for local Blue loss, got %q", res2.Result)
	}
}

// TestDaemon_SessionReset_CSRFOriginValidation verifies that POST /api/session/reset
// blocks unauthorized external origins and permits allowed local origins.
func TestDaemon_SessionReset_CSRFOriginValidation(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{}
	d, err := New(&dummySyncer{}, cfg)
	if err != nil {
		t.Fatalf("failed to create daemon: %v", err)
	}
	handler := d.Handler(ctx)

	// Disallowed origin should return 403 Forbidden
	reqDisallowed := httptest.NewRequest(http.MethodPost, "/api/session/reset", nil)
	reqDisallowed.Header.Set("Origin", "http://evil-attacker.com")
	recDisallowed := httptest.NewRecorder()
	handler.ServeHTTP(recDisallowed, reqDisallowed)
	if recDisallowed.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for disallowed Origin on session reset, got %d", recDisallowed.Code)
	}

	// Allowed origin should return 200 OK
	reqAllowed := httptest.NewRequest(http.MethodPost, "/api/session/reset", nil)
	reqAllowed.Header.Set("Origin", "http://localhost:5173")
	recAllowed := httptest.NewRecorder()
	handler.ServeHTTP(recAllowed, reqAllowed)
	if recAllowed.Code != http.StatusOK {
		t.Errorf("expected 200 OK for allowed Origin on session reset, got %d", recAllowed.Code)
	}
}

func TestDaemon_GetMatch_NoSubstringFallback_And_SyncConfigPATCH_CORS(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "test_precision.db"))
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	_ = store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
		MatchGUID: "guid-exact-001",
		MapName:   "Mannfield",
		Playlist:  11,
		Date:      "2026-10-12T12:00:00Z",
		Players: []storage.ReplayPlayerStats{
			{Name: "PlayerOne", Platform: "Epic", OnlineID: "p1", Team: 0, Score: 500},
		},
	})

	cfg := config.NewDefaultConfig()
	d, err := New(&dummySyncer{}, cfg, WithStateStore(store))
	if err != nil {
		t.Fatalf("New daemon failed: %v", err)
	}
	h := d.Handler(ctx)

	// Substring player/map query to /api/matches/{guid} must return 404, not a fallback match
	reqNotFound := httptest.NewRequest(http.MethodGet, "/api/matches/PlayerOne", nil)
	recNotFound := httptest.NewRecorder()
	h.ServeHTTP(recNotFound, reqNotFound)
	if recNotFound.Code != http.StatusNotFound {
		t.Fatalf("GET /api/matches/PlayerOne = %d, want 404", recNotFound.Code)
	}

	// PATCH /api/sync/config must succeed
	patchBody := strings.NewReader(`{"trigger_threshold":7}`)
	reqPatch := httptest.NewRequest(http.MethodPatch, "/api/sync/config", patchBody)
	reqPatch.Header.Set("Content-Type", "application/json")
	recPatch := httptest.NewRecorder()
	h.ServeHTTP(recPatch, reqPatch)
	if recPatch.Code != http.StatusOK {
		t.Fatalf("PATCH /api/sync/config = %d: %s", recPatch.Code, recPatch.Body.String())
	}

	// OPTIONS preflight must advertise PATCH, PUT, DELETE
	reqOpts := httptest.NewRequest(http.MethodOptions, "/api/sync/config", nil)
	recOpts := httptest.NewRecorder()
	h.ServeHTTP(recOpts, reqOpts)
	if recOpts.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS /api/sync/config = %d, want 204", recOpts.Code)
	}
	allowMethods := recOpts.Header().Get("Access-Control-Allow-Methods")
	for _, m := range []string{"PATCH", "PUT", "DELETE"} {
		if !strings.Contains(allowMethods, m) {
			t.Errorf("Access-Control-Allow-Methods = %q, missing %s", allowMethods, m)
		}
	}
}

func TestDaemon_BallchasingSync_ConcurrentRequests_OnlyOneStarts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	unblockBC := make(chan struct{})
	var unblockOnce sync.Once
	unblock := func() { unblockOnce.Do(func() { close(unblockBC) }) }
	enteredBC := make(chan struct{})
	var bcCalls int32

	bcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&bcCalls, 1) == 1 {
			close(enteredBC)
		}
		<-unblockBC
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ballchasing.ReplayListResponse{})
	}))
	defer bcSrv.Close()
	defer unblock()

	bcClient, err := ballchasing.NewClient(ballchasing.ClientConfig{BaseURL: bcSrv.URL, APIKey: "dummy-key"})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "test_sync_race.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer store.Close()

	hs := syncer.NewBallchasingHistorySyncer(bcClient, store, "Epic|test|0", "Tester", nil)
	hs.SetRateInterval(time.Millisecond)

	d, err := New(&dummySyncer{}, config.NewDefaultConfig(), WithHistorySyncer(hs), WithStateStore(store))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	d.mu.Lock()
	d.ctx = ctx
	d.mu.Unlock()
	h := d.Handler(ctx)

	const n = 10
	codes := make([]int, n)
	var wg sync.WaitGroup
	startGate := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-startGate
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/sync/ballchasing", nil))
			codes[idx] = rec.Code
		}(i)
	}
	close(startGate)
	wg.Wait()

	var accepted, conflict int
	for _, code := range codes {
		switch code {
		case http.StatusAccepted:
			accepted++
		case http.StatusConflict:
			conflict++
		default:
			t.Fatalf("unexpected status code %d", code)
		}
	}
	if accepted != 1 || conflict != n-1 {
		t.Fatalf("expected 1x202 + %dx409, got %dx202 + %dx409", n-1, accepted, conflict)
	}

	select {
	case <-enteredBC:
	case <-time.After(2 * time.Second):
		t.Fatal("background history sync never reached Ballchasing client")
	}
	unblock()
	cancel()
	d.wg.Wait()
}

type invalidGUIDOverrideStore struct {
	storage.StateStore
	overrideErr error
}

func (s *invalidGUIDOverrideStore) GetMatch(ctx context.Context, matchGUID string) (*storage.MatchRecord, error) {
	if s.overrideErr != nil {
		return nil, s.overrideErr
	}
	return s.StateStore.GetMatch(ctx, matchGUID)
}

func TestDaemon_GetMatch_InvalidGUID400_And_StoreError500(t *testing.T) {
	ctx := context.Background()
	baseStore, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "test_getmatch_errs.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer baseStore.Close()

	// 1. Whitespace-only GUID in URL path -> 400 Bad Request
	dWS, err := New(&dummySyncer{}, config.NewDefaultConfig(), WithStateStore(baseStore))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	recWS := httptest.NewRecorder()
	dWS.Handler(ctx).ServeHTTP(recWS, httptest.NewRequest(http.MethodGet, "/api/matches/%20%20", nil))
	if recWS.Code != http.StatusBadRequest {
		t.Fatalf("GET /api/matches/%%20%%20 = %d, want 400", recWS.Code)
	}

	// 2. Store returning storage.ErrInvalidGUID -> 400 Bad Request
	dInvalid, err := New(&dummySyncer{}, config.NewDefaultConfig(), WithStateStore(&invalidGUIDOverrideStore{
		StateStore:  baseStore,
		overrideErr: storage.ErrInvalidGUID,
	}))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	recInvalid := httptest.NewRecorder()
	dInvalid.Handler(ctx).ServeHTTP(recInvalid, httptest.NewRequest(http.MethodGet, "/api/matches/bad-guid", nil))
	if recInvalid.Code != http.StatusBadRequest {
		t.Fatalf("GET /api/matches/bad-guid (ErrInvalidGUID) = %d, want 400", recInvalid.Code)
	}

	// 3. Unexpected store I/O error -> 500 Internal Server Error (never masked as 404)
	dBoom, err := New(&dummySyncer{}, config.NewDefaultConfig(), WithStateStore(&invalidGUIDOverrideStore{
		StateStore:  baseStore,
		overrideErr: errors.New("sqlite disk I/O error"),
	}))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	recBoom := httptest.NewRecorder()
	dBoom.Handler(ctx).ServeHTTP(recBoom, httptest.NewRequest(http.MethodGet, "/api/matches/guid-exact-001", nil))
	if recBoom.Code != http.StatusInternalServerError {
		t.Fatalf("GET /api/matches/guid-exact-001 (store error) = %d, want 500", recBoom.Code)
	}
}

type blockingStatsPollerSyncer struct {
	cycleMu     sync.Mutex
	cycles      atomic.Int32
	cycleSignal chan int32
	pollStarted chan struct{}
	unblockPoll chan struct{}
}

func (s *blockingStatsPollerSyncer) RunCycle(ctx context.Context) (*syncer.SyncStats, error) {
	s.cycleMu.Lock()
	defer s.cycleMu.Unlock()
	n := s.cycles.Add(1)
	select {
	case s.cycleSignal <- n:
	default:
	}
	return &syncer.SyncStats{DiscoveredCount: 1}, nil
}

func (s *blockingStatsPollerSyncer) PollAndSyncPendingStats(ctx context.Context) error {
	s.cycleMu.Lock()
	defer s.cycleMu.Unlock()
	select {
	case <-s.pollStarted:
	default:
		close(s.pollStarted)
	}
	select {
	case <-s.unblockPoll:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestDaemon_AsyncStatsPoll_DoesNotDropManualTriggerSyncCycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockSync := &blockingStatsPollerSyncer{
		cycleSignal: make(chan int32, 8),
		pollStarted: make(chan struct{}),
		unblockPoll: make(chan struct{}),
	}

	cfg := config.NewDefaultConfig()
	cfg.Web.Enabled = false
	cfg.StatsAPI.Enabled = false
	cfg.PlayerTracking.Enabled = false
	cfg.Sync.PollInterval = config.Duration(time.Hour)

	d, err := New(mockSync, cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	doneCh := make(chan error, 1)
	go func() {
		doneCh <- d.Start(ctx)
	}()

	// Wait for initial cycle (#1) to complete fully (d.inFlight == false)
	select {
	case n := <-mockSync.cycleSignal:
		if n != 1 {
			t.Fatalf("expected initial cycle 1, got %d", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for initial sync cycle")
	}
	for d.IsInFlight() {
		time.Sleep(time.Millisecond)
	}

	// Start async stats poll and wait until it holds cycleMu
	d.startAsyncStatsPoll(ctx, mockSync)
	select {
	case <-mockSync.pollStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for async stats poll to start")
	}

	// Trigger manual sync while statsPollInFlight is active
	if err := d.TriggerSync(ctx, "manual_during_stats_poll"); err != nil {
		t.Fatalf("TriggerSync failed: %v", err)
	}
	// Allow runEventLoop to dequeue manualTrigger before unblocking stats poll
	time.Sleep(25 * time.Millisecond)
	close(mockSync.unblockPoll)

	select {
	case n := <-mockSync.cycleSignal:
		if n != 2 {
			t.Fatalf("expected manual sync cycle 2 after stats poll completed, got %d", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("manual sync trigger was dropped while async stats poll was in flight")
	}

	cancel()
	select {
	case err := <-doneCh:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("daemon failed to stop cleanly")
	}
}


