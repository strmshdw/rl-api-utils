package daemon_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
)

// TestChallenger2_ConcurrencyStress_50PlusWorkers runs 60 concurrent client workers
// across all 5 HTTP endpoints (/current-match, /players, /players/{id}, /sync, /status)
// while the daemon actively runs scheduled cycles and the player tracker ingests live matches.
func TestChallenger2_ConcurrencyStress_50PlusWorkers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	store := newTestStore(t, backendSQLite)
	fetcher := playertrack.NewMockSkillFetcher()
	tracker := newTestTracker(t, store, fetcher)

	// Pre-seed 25 players with matchup records
	now := time.Now().UTC()
	for i := 1; i <= 25; i++ {
		pid := fmt.Sprintf("Steam|765611980000000%02d|0", i)
		pname := fmt.Sprintf("Player_%02d", i)
		_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
			PlayerID:    pid,
			Platform:    "Steam",
			PlayerName:  pname,
			RanksJSON:   `{"11":{"tier":14,"division":2,"mmr":920.0}}`,
			FirstSeenAt: now,
			LastSeenAt:  now,
		})
		_ = store.RecordMatchResults(ctx, fmt.Sprintf("seed-guid-%d", i), 11, []storage.PlayerOutcome{
			{PlayerID: pid, Platform: "Steam", PlayerName: pname, IsTeammate: i%2 == 0, Won: true},
		})
	}

	cfg := makeTestConfig(15*time.Millisecond, false)
	syncerMock := newMockSyncer()

	buf := &bytes.Buffer{}
	logger := daemon.NewLogger(cfg.Logging, buf)

	d, err := daemon.New(syncerMock, cfg,
		daemon.WithPlayerTracker(tracker),
		daemon.WithStateStore(store),
		daemon.WithLogger(logger),
	)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	// Start daemon execution in background
	daemonStopped := make(chan error, 1)
	go func() {
		daemonStopped <- d.Start(ctx)
	}()

	// Background worker simulating live in-game match events
	var eventStop atomic.Bool
	var eventsWg sync.WaitGroup
	eventsWg.Add(1)
	go func() {
		defer eventsWg.Done()
		tick := 0
		for !eventStop.Load() {
			tick++
			guid := fmt.Sprintf("live-guid-%d", tick%5)
			players := []statsapi.StatsPlayer{
				{PrimaryId: "Epic|my_epic_acc_001|0", Name: "Me", TeamNum: 0, Score: 100},
				{PrimaryId: fmt.Sprintf("Steam|765611980000000%02d|0", (tick%25)+1), Name: "Tm", TeamNum: 0, Score: 150},
				{PrimaryId: fmt.Sprintf("Epic|opp_%02d|0", (tick%10)+1), Name: "Opp", TeamNum: 1, Score: 80},
			}
			_ = tracker.OnUpdateState(ctx, guid, 11, players)
			if tick%10 == 0 {
				winner := tick % 2
				_ = tracker.OnMatchEnded(ctx, guid, &winner)
			}
			time.Sleep(3 * time.Millisecond)
		}
	}()

	// Launch 60 concurrent worker goroutines (> 50 required by specification)
	const numWorkers = 60
	endpoints := []string{
		"/current-match",
		"/players",
		"/players?limit=5&offset=2",
		"/players/Steam%7C76561198000000001%7C0",
		"/players/Steam|76561198000000002|0",
		"/players/Epic%7Cnonexistent%7C0",
		"/sync",
		"/status",
	}

	var workersWg sync.WaitGroup
	var totalRequests atomic.Int64
	var errorCount atomic.Int64

	handler := d.Handler(ctx)

	for w := 0; w < numWorkers; w++ {
		workersWg.Add(1)
		go func(workerID int) {
			defer workersWg.Done()
			reqIdx := workerID
			for {
				select {
				case <-ctx.Done():
					return
				default:
					ep := endpoints[reqIdx%len(endpoints)]
					reqIdx++

					method := http.MethodGet
					if ep == "/sync" && reqIdx%2 == 0 {
						method = http.MethodPost
					}

					req, err := http.NewRequest(method, ep, nil)
					if err != nil {
						errorCount.Add(1)
						return
					}

					rec := &responseRecorderDirect{
						header: make(http.Header),
						body:   &bytes.Buffer{},
						code:   http.StatusOK,
					}

					handler.ServeHTTP(rec, req)
					totalRequests.Add(1)

					// Validate HTTP status
					switch ep {
					case "/players/Epic%7Cnonexistent%7C0":
						if rec.code != http.StatusNotFound {
							t.Errorf("worker %d: expected 404 for nonexistent player, got %d", workerID, rec.code)
							errorCount.Add(1)
						}
					default:
						if rec.code != http.StatusOK {
							t.Errorf("worker %d: unexpected status %d for %s %s: body=%s",
								workerID, rec.code, method, ep, rec.body.String())
							errorCount.Add(1)
						}
					}

					// Validate JSON payload
					if rec.body.Len() > 0 {
						var parsed any
						if err := json.Unmarshal(rec.body.Bytes(), &parsed); err != nil {
							t.Errorf("worker %d: invalid JSON for %s: %v", workerID, ep, err)
							errorCount.Add(1)
						}
					}
				}
			}
		}(w)
	}

	// Run for 1.5 seconds under intense concurrency
	time.Sleep(1500 * time.Millisecond)

	// Stop background match event generator and wait
	eventStop.Store(true)
	eventsWg.Wait()

	// Cancel context to shut down workers and daemon
	cancel()
	workersWg.Wait()

	err = <-daemonStopped
	if err != nil && !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("daemon stopped with error: %v", err)
	}

	reqs := totalRequests.Load()
	errs := errorCount.Load()
	t.Logf("Concurrency stress test completed: %d total requests across 60 workers, %d errors", reqs, errs)

	if reqs < 100 {
		t.Fatalf("expected at least 100 requests to be processed, got %d", reqs)
	}
	if errs != 0 {
		t.Fatalf("encountered %d request failures during concurrency stress test", errs)
	}
}

// responseRecorderDirect is a lightweight thread-safe response recorder for benchmarking/stressing.
type responseRecorderDirect struct {
	mu     sync.Mutex
	header http.Header
	body   *bytes.Buffer
	code   int
}

func (r *responseRecorderDirect) Header() http.Header {
	return r.header
}

func (r *responseRecorderDirect) Write(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.body.Write(b)
}

func (r *responseRecorderDirect) WriteHeader(statusCode int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.code = statusCode
}

// TestChallenger2_LifecycleDrain_RealListener_ZeroGoroutineLeak asserts that:
// 1. Daemon starts with a real TCP listener on 127.0.0.1.
// 2. Responds to concurrent HTTP requests.
// 3. Upon context cancellation, exits strictly within 2.5 seconds.
// 4. Leaves ZERO leaked goroutines from daemon or HTTP server.
func TestChallenger2_LifecycleDrain_RealListener_ZeroGoroutineLeak(t *testing.T) {
	// Baseline goroutine stabilization
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	baselineGoroutines := runtime.NumGoroutine()

	store := newTestStore(t, backendSQLite)
	tracker := newTestTracker(t, store, nil)

	// Find an available port
	testPort := 49132
	for p := 49132; p < 49150; p++ {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err == nil {
			testPort = p
			_ = ln.Close()
			break
		}
	}

	cfg := makeTestConfig(1*time.Hour, false)
	cfg.PlayerTracking.Enabled = true
	cfg.StatsAPI.HTTPTriggerPort = testPort

	syncerMock := newMockSyncer()
	d, err := daemon.New(syncerMock, cfg,
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

	// Wait for startup cycle
	select {
	case <-syncerMock.cycleStarts:
	case <-time.After(1 * time.Second):
		cancel()
		t.Fatal("timed out waiting for initial cycle")
	}

	// Poll until server is ready
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", testPort)
	client := &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: true, // Prevent client-side idle worker goroutines from polluting leak check
		},
		Timeout: 500 * time.Millisecond,
	}

	var ready bool
	for i := 0; i < 40; i++ {
		resp, err := client.Get(baseURL + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			ready = true
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !ready {
		cancel()
		t.Fatal("HTTP server failed to become ready")
	}

	// Issue active concurrent requests
	var clientWg sync.WaitGroup
	var clientErrors atomic.Int64
	for i := 0; i < 15; i++ {
		clientWg.Add(1)
		go func(id int) {
			defer clientWg.Done()
			path := "/current-match"
			if id%3 == 1 {
				path = "/players"
			} else if id%3 == 2 {
				path = "/status"
			}
			resp, err := client.Get(baseURL + path)
			if err != nil {
				clientErrors.Add(1)
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}(i)
	}
	clientWg.Wait()

	if errCount := clientErrors.Load(); errCount > 0 {
		t.Fatalf("encountered %d client request errors before shutdown", errCount)
	}

	// Trigger Graceful Drain via context cancellation
	drainStart := time.Now()
	cancel()

	// Strict verification: Daemon MUST exit within 2.5 seconds (specification requirement)
	select {
	case err := <-stopped:
		elapsed := time.Since(drainStart)
		t.Logf("Daemon shutdown completed in %v", elapsed)
		if elapsed > 2500*time.Millisecond {
			t.Fatalf("LIFECYCLE DRAIN TIMEOUT: daemon took %v to exit, exceeding 2.5s limit", elapsed)
		}
		if err != nil && !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("Start returned unexpected error: %v", err)
		}
	case <-time.After(2500 * time.Millisecond):
		t.Fatal("LIFECYCLE DRAIN DEADLINE EXCEEDED: daemon did not exit within 2.5 seconds")
	}

	// Verify listener is closed: new connection must fail
	_, err = client.Get(baseURL + "/healthz")
	if err == nil {
		t.Fatal("listener remained open after daemon shutdown")
	}

	// Verify ZERO leaked goroutines
	// Allow small settling window for runtime scheduler teardown
	var finalGoroutines int
	for attempt := 0; attempt < 10; attempt++ {
		runtime.GC()
		time.Sleep(50 * time.Millisecond)
		finalGoroutines = runtime.NumGoroutine()
		// Allow at most 1 goroutine margin for background runtime GC/sysmon
		if finalGoroutines <= baselineGoroutines+1 {
			break
		}
	}

	t.Logf("Goroutines: baseline=%d, final=%d", baselineGoroutines, finalGoroutines)
	if finalGoroutines > baselineGoroutines+2 {
		buf := make([]byte, 16384)
		n := runtime.Stack(buf, true)
		t.Fatalf("GOROUTINE LEAK DETECTED: baseline was %d, current is %d. Active stacks:\n%s",
			baselineGoroutines, finalGoroutines, string(buf[:n]))
	}
}

// TestChallenger2_PortConflictResilience_Port49125 verifies that when port 49125
// is held by an external listener, starting the daemon:
// 1. Logs a warning about the HTTP server bind error.
// 2. Does NOT crash or return fatal error.
// 3. Continues running the regular sync ticker loop normally.
func TestChallenger2_PortConflictResilience_Port49125(t *testing.T) {
	// 1. Bind port 49125 externally
	externalLn, err := net.Listen("tcp", "127.0.0.1:49125")
	if err != nil {
		t.Skipf("port 49125 unavailable on host: %v", err)
	}
	defer externalLn.Close()

	// 2. Configure daemon targeting port 49125
	cfg := makeTestConfig(20*time.Millisecond, false)
	cfg.StatsAPI.HTTPTriggerPort = 49125
	cfg.PlayerTracking.Enabled = true

	logBuf := &bytes.Buffer{}
	logger := daemon.NewLogger(cfg.Logging, logBuf)

	syncerMock := newMockSyncer()
	d, err := daemon.New(syncerMock, cfg,
		daemon.WithLogger(logger),
	)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)

	go func() {
		stopped <- d.Start(ctx)
	}()

	// 3. Verify sync cycles continue executing scheduled ticker runs
	for cycle := 1; cycle <= 3; cycle++ {
		select {
		case <-syncerMock.cycleStarts:
			t.Logf("Sync cycle %d executed successfully despite HTTP port conflict", cycle)
		case <-time.After(1500 * time.Millisecond):
			cancel()
			t.Fatalf("daemon sync loop stalled on cycle %d due to port conflict!", cycle)
		}
	}

	// 4. Cancel and await clean shutdown
	cancel()
	select {
	case err := <-stopped:
		if err != nil && !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("Start returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("daemon failed to shut down cleanly")
	}

	// 5. Verify warning was logged
	logged := logBuf.String()
	if !strings.Contains(logged, "local HTTP API server stopped with error") {
		t.Errorf("expected warning log 'local HTTP API server stopped with error', got:\n%s", logged)
	}
}

// TestChallenger2_HTTP_AdversarialInputs_And_BoundaryEdgeCases tests adversarial and
// boundary inputs across all daemon HTTP routes.
func TestChallenger2_HTTP_AdversarialInputs_And_BoundaryEdgeCases(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, backendSQLite)
	fetcher := playertrack.NewMockSkillFetcher()
	tracker := newTestTracker(t, store, fetcher)

	cfg := makeTestConfig(5*time.Minute, false)
	d, err := daemon.New(newMockSyncer(), cfg,
		daemon.WithPlayerTracker(tracker),
		daemon.WithStateStore(store),
	)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	handler := d.Handler(ctx)

	// 1. Extreme integer overflows in pagination parameters
	t.Run("Pagination_ExtremeOverflows", func(t *testing.T) {
		overflowURLs := []string{
			"/players?limit=9999999999999999999999999999999999999999999999999999999999999999999999",
			"/players?limit=-999999999999999999999999999999999999999999999999999999999999999999999",
			"/players?offset=999999999999999999999999999999999999999999999999999999999999999999999",
			"/players?offset=-99999999999999999999999999999999999999999999999999999999999999999999",
			"/players?limit=0&offset=0",
			"/players?limit=&offset=",
			"/players?limit=%20&offset=%20",
		}
		for _, u := range overflowURLs {
			req, _ := http.NewRequest(http.MethodGet, u, nil)
			rec := &responseRecorderDirect{header: make(http.Header), body: &bytes.Buffer{}, code: 200}
			handler.ServeHTTP(rec, req)
			if rec.code != http.StatusOK {
				t.Errorf("expected 200 OK for %s, got %d", u, rec.code)
			}
		}
	})

	// 2. Extremely large player ID (50KB string)
	t.Run("GetPlayer_MassiveID", func(t *testing.T) {
		hugeID := strings.Repeat("A", 50000)
		req, _ := http.NewRequest(http.MethodGet, "/players/"+hugeID, nil)
		rec := &responseRecorderDirect{header: make(http.Header), body: &bytes.Buffer{}, code: 200}
		handler.ServeHTTP(rec, req)
		if rec.code != http.StatusNotFound {
			t.Errorf("expected 404 for massive nonexistent ID, got %d", rec.code)
		}
	})

	// 3. Multi-slash player IDs
	t.Run("GetPlayer_MultiSlashID", func(t *testing.T) {
		multiSlash := "/players/Epic/SomeUser/Extra/Segments"
		req, _ := http.NewRequest(http.MethodGet, multiSlash, nil)
		rec := &responseRecorderDirect{header: make(http.Header), body: &bytes.Buffer{}, code: 200}
		handler.ServeHTTP(rec, req)
		if rec.code != http.StatusNotFound {
			t.Errorf("expected 404 for multi-slash ID, got %d", rec.code)
		}
	})

	// 4. Repeated rapid TriggerSync requests
	t.Run("Rapid_TriggerSync_Burst", func(t *testing.T) {
		for i := 0; i < 200; i++ {
			err := d.TriggerSync(ctx, fmt.Sprintf("burst-%d", i))
			if err != nil {
				t.Errorf("TriggerSync failed on iteration %d: %v", i, err)
			}
		}
	})

	// 5. Unsupported HTTP Methods on strict GET routes
	t.Run("StrictMethods", func(t *testing.T) {
		disallowedMethods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}
		for _, method := range disallowedMethods {
			req, _ := http.NewRequest(method, "/current-match", nil)
			rec := &responseRecorderDirect{header: make(http.Header), body: &bytes.Buffer{}, code: 200}
			handler.ServeHTTP(rec, req)
			if rec.code != http.StatusMethodNotAllowed {
				t.Errorf("expected 405 Method Not Allowed for %s /current-match, got %d", method, rec.code)
			}
		}
	})
}

