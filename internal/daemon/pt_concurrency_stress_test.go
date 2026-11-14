package daemon_test

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
)

func TestStress_FullHTTPAPI_HighConcurrency(t *testing.T) {
	store := newTestStore(t, backendSQLite)
	ctx := context.Background()
	now := time.Now().UTC()

	// Seed 50 players in SQLite
	const seedCount = 50
	for i := 1; i <= seedCount; i++ {
		pid := fmt.Sprintf("Steam|765611980000000%02d|0", i)
		_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
			PlayerID:    pid,
			Platform:    "Steam",
			PlayerName:  fmt.Sprintf("ConcurrentPlayer_%02d", i),
			RanksJSON:   `{"11":{"tier":14,"division":0}}`,
			FirstSeenAt: now,
			LastSeenAt:  now,
		})
		_ = store.RecordMatchResults(ctx, fmt.Sprintf("conc-guid-%d", i), 11, []storage.PlayerOutcome{
			{PlayerID: pid, Platform: "Steam", PlayerName: fmt.Sprintf("ConcurrentPlayer_%02d", i), IsTeammate: true, Won: true},
		})
	}

	tracker := newTestTracker(t, store, nil)
	// Seed an active match in tracker
	_ = tracker.OnUpdateState(ctx, "live-conc-guid", 11, []statsapi.StatsPlayer{
		{PrimaryId: "Epic|my_epic_acc_001|0", Name: "Me", TeamNum: 0},
		{PrimaryId: "Steam|76561198000000001|0", Name: "ConcurrentPlayer_01", TeamNum: 1},
	})

	cfg := makeTestConfig(5*time.Minute, false)
	d, err := daemon.New(newMockSyncer(), cfg,
		daemon.WithPlayerTracker(tracker),
		daemon.WithStateStore(store),
	)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	srv := httptest.NewServer(d.Handler(ctx))
	defer srv.Close()

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
		},
	}

	const numWorkers = 50
	const requestsPerWorker = 100
	var wg sync.WaitGroup
	var totalRequests int64
	var errCount int64

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for r := 0; r < requestsPerWorker; r++ {
				endpointChoice := r % 4
				var targetURL string
				var expectedStatus int

				switch endpointChoice {
				case 0:
					// GET /current-match
					targetURL = srv.URL + "/current-match"
					expectedStatus = http.StatusOK
				case 1:
					// GET /players with randomized limit and offset
					limit := rand.Intn(150) - 10  // [-10, 140]
					offset := rand.Intn(100) - 10 // [-10, 90]
					targetURL = fmt.Sprintf("%s/players?limit=%d&offset=%d", srv.URL, limit, offset)
					expectedStatus = http.StatusOK
				case 2:
					// GET /players/{id} with percent encoded ID
					targetIdx := (r % seedCount) + 1
					targetURL = fmt.Sprintf("%s/players/Steam%%7C765611980000000%02d%%7C0", srv.URL, targetIdx)
					expectedStatus = http.StatusOK
				case 3:
					// GET /players/{id} with nonexistent ID -> 404
					targetURL = srv.URL + "/players/Steam%7Cnonexistent_id%7C0"
					expectedStatus = http.StatusNotFound
				}

				resp, err := client.Get(targetURL)
				if err != nil {
					atomic.AddInt64(&errCount, 1)
					continue
				}

				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()

				if resp.StatusCode != expectedStatus {
					atomic.AddInt64(&errCount, 1)
					t.Errorf("worker %d req %d: url %s expected %d, got %d: %s",
						workerID, r, targetURL, expectedStatus, resp.StatusCode, string(body))
					return
				}
				atomic.AddInt64(&totalRequests, 1)
			}
		}(w)
	}

	wg.Wait()

	if atomic.LoadInt64(&errCount) > 0 {
		t.Fatalf("encountered %d request errors during high concurrency stress", errCount)
	}
	expectedTotal := int64(numWorkers * requestsPerWorker)
	if totalRequests != expectedTotal {
		t.Fatalf("expected %d total requests, processed %d", expectedTotal, totalRequests)
	}
}
