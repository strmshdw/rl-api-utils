package daemon_test

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/session"
	"github.com/dank/rl-api-utils/internal/storage"
)

// TestChallenger2_SessionReset_AdversarialOrigins exercises POST /api/session/reset
// against unauthorized external origins (asserting 403 Forbidden) and authorized origins (asserting 200 OK).
func TestChallenger2_SessionReset_AdversarialOrigins(t *testing.T) {
	h := setupM3DaemonTest(t)

	unauthorizedOrigins := []string{
		"http://evil.com",
		"https://attacker.example.org",
		"http://evil.com:8080",
		"https://attacker.example.org:443",
		"http://localhost.evil.com",
		"http://127.0.0.1.attacker.net",
		"http://evil-localhost",
		"http://127.0.0.1.nip.io",
		"null",
		"data:text/html",
		"://malformed-origin",
		"http://evil.com?localhost",
		"http://evil.com#localhost",
	}

	for _, origin := range unauthorizedOrigins {
		t.Run("Disallowed Origin: "+origin, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/session/reset", nil)
			req.Header.Set("Origin", origin)
			rec := httptest.NewRecorder()
			h.handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("expected 403 Forbidden for unauthorized origin %q, got %d (body: %s)", origin, rec.Code, rec.Body.String())
			}

			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("failed to decode JSON error body: %v", err)
			}
			if body["error"] != "forbidden: invalid origin" {
				t.Fatalf("expected 'forbidden: invalid origin', got %q", body["error"])
			}
		})
	}

	authorizedOrigins := []string{
		"http://localhost:5173",
		"http://127.0.0.1:5173",
		"http://[::1]:5173",
		"http://localhost:49125",
		"http://127.0.0.1:49125",
	}

	for _, origin := range authorizedOrigins {
		t.Run("Allowed Origin: "+origin, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/session/reset", nil)
			req.Header.Set("Origin", origin)
			rec := httptest.NewRecorder()
			h.handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200 OK for authorized origin %q, got %d (body: %s)", origin, rec.Code, rec.Body.String())
			}

			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("failed to decode JSON success body: %v", err)
			}
			if body["status"] != "ok" || body["message"] != "session reset" {
				t.Fatalf("unexpected body response: %v", body)
			}
		})
	}

	t.Run("Native client without Origin header (curl/go test)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/session/reset", nil)
		rec := httptest.NewRecorder()
		h.handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for native client with no Origin header, got %d", rec.Code)
		}
	})
}

// TestChallenger2_Concurrent_SessionReset_Matches_Players_UnderLoad subjects
// /api/session/reset, /api/matches, and /api/players to rapid concurrent requests
// under the race detector.
func TestChallenger2_Concurrent_SessionReset_Matches_Players_UnderLoad(t *testing.T) {
	h := setupM3DaemonTest(t)

	// Seed 20 historical matches and players
	now := time.Now().UTC()
	team0 := 0
	won := true
	for i := 1; i <= 20; i++ {
		guid := fmt.Sprintf("guid-stress-%03d", i)
		pID := fmt.Sprintf("Steam|76561198000000%03d|0", i)

		_ = h.store.UpsertPlayer(h.ctx, &storage.PlayerRecord{
			PlayerID:    pID,
			PlayerName:  fmt.Sprintf("StressPlayer%d", i),
			Platform:    "steam",
			FirstSeenAt: now.Add(-time.Duration(i) * time.Hour),
			LastSeenAt:  now,
		})

		_ = h.store.UpsertDiscoveredMatches(h.ctx, []*storage.MatchRecord{
			{
				MatchGUID:            guid,
				RecordStartTimestamp: now.Add(-time.Duration(i) * time.Minute).Unix(),
				Playlist:             13,
				BlueScore:            3,
				OrangeScore:          1,
				WinnerTeam:           &team0,
				DownloadStatus:       storage.DownloadDownloaded,
				UploadStatus:         storage.UploadUploaded,
				StatsSyncStatus:      storage.StatsSyncSynced,
			},
		})

		_ = h.store.SaveMatchPlayers(h.ctx, guid, []*storage.MatchPlayerRecord{
			{
				MatchGUID: guid,
				PlayerID:  pID,
				TeamNum:   0,
				IsLocal:   (i == 1),
				Won:       &won,
				Score:     450,
				Goals:     2,
			},
		})
	}

	server := httptest.NewServer(h.handler)
	defer server.Close()

	var stopCh = make(chan struct{})
	var wg sync.WaitGroup
	var reqCount int64
	var errCount int64

	const numWorkers = 24
	const iterationsPerWorker = 30

	// Worker routines
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		workerID := w
		go func() {
			defer wg.Done()
			client := &http.Client{Timeout: 5 * time.Second}

			endpoints := []struct {
				method string
				path   string
				origin string
			}{
				{method: "POST", path: "/api/session/reset", origin: "http://localhost:5173"},
				{method: "GET", path: "/api/matches?limit=10&result=victory", origin: ""},
				{method: "GET", path: "/api/matches?limit=10&result=defeat", origin: ""},
				{method: "GET", path: "/api/matches?playlist=13", origin: ""},
				{method: "GET", path: "/api/players?limit=10&role=all", origin: ""},
				{method: "GET", path: "/api/players?limit=10&role=teammate", origin: ""},
				{method: "GET", path: "/api/players?sort=win_rate&order=desc", origin: ""},
				{method: "GET", path: "/api/session", origin: ""},
				{method: "GET", path: "/api/current-match", origin: ""},
			}

			for i := 0; i < iterationsPerWorker; i++ {
				select {
				case <-stopCh:
					return
				default:
				}

				ep := endpoints[rand.Intn(len(endpoints))]
				req, err := http.NewRequest(ep.method, server.URL+ep.path, nil)
				if err != nil {
					atomic.AddInt64(&errCount, 1)
					continue
				}
				if ep.origin != "" {
					req.Header.Set("Origin", ep.origin)
				}

				resp, err := client.Do(req)
				atomic.AddInt64(&reqCount, 1)
				if err != nil {
					atomic.AddInt64(&errCount, 1)
					t.Errorf("worker %d %s %s failed: %v", workerID, ep.method, ep.path, err)
					continue
				}

				if resp.StatusCode != http.StatusOK {
					atomic.AddInt64(&errCount, 1)
					t.Errorf("worker %d %s %s expected 200, got %d", workerID, ep.method, ep.path, resp.StatusCode)
				}
				_ = resp.Body.Close()
			}
		}()
	}

	// Concurrently simulate live match events in background
	bgDone := make(chan struct{})
	go func() {
		defer close(bgDone)
		for i := 0; i < 50; i++ {
			select {
			case <-stopCh:
				return
			default:
				h.sess.RecordActiveMatch(&playertrack.CurrentMatchResponse{
					ActiveMatch:   true,
					MatchGUID:     fmt.Sprintf("live-guid-%d", i),
					PlaylistID:    13,
					TimeRemaining: 300 - i*5,
					IsOvertime:    false,
					LocalTeam:     &team0,
					UpdatedAt:     time.Now().UTC(),
				})
				time.Sleep(3 * time.Millisecond)
			}
		}
	}()

	wg.Wait()
	close(stopCh)
	<-bgDone

	totalReqs := atomic.LoadInt64(&reqCount)
	totalErrs := atomic.LoadInt64(&errCount)
	t.Logf("Concurrent API Stress Test complete: %d requests handled, %d errors", totalReqs, totalErrs)

	if totalErrs > 0 {
		t.Fatalf("encountered %d errors during concurrent API stress test", totalErrs)
	}
}

// TestChallenger2_LiveGameClock_And_MatchConclusions_Concurrency verifies thread safety
// and state consistency when live game clock updates arrive concurrently with match conclusions.
func TestChallenger2_LiveGameClock_And_MatchConclusions_Concurrency(t *testing.T) {
	tracker := session.NewSessionTracker()
	defer tracker.Broadcaster().Close()

	subCh, cancelSub := tracker.Subscribe()
	defer cancelSub()

	var wg sync.WaitGroup
	const numEvents = 200

	team0 := 0
	team1 := 1

	// Goroutine 1: Pumping rapid live game clock updates
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numEvents; i++ {
			tracker.OnActiveMatchUpdated(&playertrack.CurrentMatchResponse{
				ActiveMatch:   true,
				MatchGUID:     "clock-concurrency-guid",
				PlaylistID:    11,
				TimeRemaining: 300 - i,
				IsOvertime:    (i >= 300),
				LocalTeam:     &team0,
				UpdatedAt:     time.Now().UTC(),
			})
			time.Sleep(50 * time.Microsecond)
		}
	}()

	// Goroutine 2: Pumping match conclusions
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			winner := team0
			if i%2 == 1 {
				winner = team1
			}
			tracker.ConcludeMatch(&playertrack.CurrentMatchResponse{
				ActiveMatch:   false,
				MatchEnded:    true,
				MatchGUID:     fmt.Sprintf("concluded-guid-%d", i),
				PlaylistID:    11,
				WinnerTeam:    &winner,
				Result:        "victory",
				TimeRemaining: 0,
				IsOvertime:    false,
				LocalTeam:     &team0,
				UpdatedAt:     time.Now().UTC(),
			})
			time.Sleep(500 * time.Microsecond)
		}
	}()

	// Goroutine 3: Concurrent reader querying GetSessionSummary
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			summary := tracker.GetSessionSummary()
			if summary == nil {
				t.Errorf("GetSessionSummary returned nil")
			}
			time.Sleep(200 * time.Microsecond)
		}
	}()

	// Consumer reading SSE channel to ensure no deadlocks or buffer blocks
	consumeDone := make(chan struct{})
	go func() {
		defer close(consumeDone)
		for {
			select {
			case _, ok := <-subCh:
				if !ok {
					return
				}
			case <-time.After(500 * time.Millisecond):
				return
			}
		}
	}()

	wg.Wait()
	<-consumeDone
}
