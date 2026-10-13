package daemon_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/session"
	"github.com/dank/rl-api-utils/internal/storage"
)

// TestChallenger_Stress_API_ConcurrentRequestsUnderLoad subjects the daemon REST APIs
// (/api/current-match, /current-match, /api/session, /api/players, /api/players/{id}, /api/session/reset)
// to heavy concurrent traffic while active match states, completions, and session resets are occurring.
// It verifies:
// 1. Thread safety and zero data races.
// 2. HTTP 200 responses with valid JSON serialization.
// 3. True in-game MMR values (~1050) across all outputs, with 0 raw TrueSkill decimals.
func TestChallenger_Stress_API_ConcurrentRequestsUnderLoad(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	// Seed 10 players into storage with true in-game MMR in ranks_json
	for i := 1; i <= 10; i++ {
		playerID := fmt.Sprintf("Steam|765611980000000%02d|0", i)
		inGameMMR := 1000.0 + float64(i*25)
		ranksJSON := fmt.Sprintf(`{"11":{"playlist_id":11,"playlist_name":"Ranked Doubles 2v2","tier":16,"division":0,"rank_name":"Champion II Division I","mmr":%.1f,"matches_played":50}}`, inGameMMR)
		player := &storage.PlayerRecord{
			PlayerID:    playerID,
			PlayerName:  fmt.Sprintf("StressPlayer%d", i),
			Platform:    "Steam",
			RanksJSON:   ranksJSON,
			FirstSeenAt: time.Now().Add(-2 * time.Hour),
			LastSeenAt:  time.Now(),
		}
		if err := store.UpsertPlayer(ctx, player); err != nil {
			t.Fatalf("failed to seed player %d: %v", i, err)
		}
	}

	sessionTracker := session.NewSessionTracker()
	cfg := config.NewDefaultConfig()
	cfg.Web.Enabled = true
	cfg.PlayerTracking.Enabled = true
	cfg.PlayerTracking.LocalPlayerID = "Steam|76561198000000001|0"

	mockFetcher := playertrack.NewMockSkillFetcher()
	pt, err := playertrack.NewTracker(store, mockFetcher, cfg.PlayerTracking, cfg.Auth)
	if err != nil {
		t.Fatalf("failed to create tracker: %v", err)
	}
	defer pt.Close()
	pt.SetMatchStateListener(sessionTracker)

	d, err := daemon.New(
		newMockSyncer(),
		cfg,
		daemon.WithStore(store),
		daemon.WithPlayerTracker(pt),
		daemon.WithSessionTracker(sessionTracker),
	)
	if err != nil {
		t.Fatalf("failed to create daemon: %v", err)
	}

	server := httptest.NewServer(d.Handler(ctx))
	defer server.Close()

	var stopSignal int32
	var reqCount int64
	var errCount int64

	// Background worker simulating continuous match progression and resets
	var bgWg sync.WaitGroup
	bgWg.Add(1)
	go func() {
		defer bgWg.Done()
		matchIdx := 0
		for atomic.LoadInt32(&stopSignal) == 0 {
			matchIdx++
			guid := fmt.Sprintf("stress-api-match-%d", matchIdx)
			localTeam := 0
			winner := matchIdx % 2

			// 1. Kickoff
			activeMatch := &playertrack.CurrentMatchResponse{
				ActiveMatch:  true,
				MatchEnded:   false,
				MatchGUID:    guid,
				PlaylistID:   11,
				PlaylistName: "Ranked Doubles 2v2",
				LocalTeam:    &localTeam,
				LocalPlayer: &playertrack.LobbyPlayer{
					PlayerID: "Steam|76561198000000001|0",
					Name:     "StressPlayer1",
					TeamNum:  0,
					IsLocal:  true,
					CurrentRank: &playertrack.PlayerPlaylistRank{
						PlaylistID: 11,
						MMR:        1050.0 + float64(matchIdx%50),
					},
				},
				Teammates: []playertrack.LobbyPlayer{
					{
						PlayerID:       "Steam|76561198000000002|0",
						Name:           "StressPlayer2",
						TeamNum:        0,
						IsDisconnected: (matchIdx%3 == 0),
						CurrentRank: &playertrack.PlayerPlaylistRank{
							PlaylistID: 11,
							MMR:        1038.0,
						},
					},
				},
				Opponents: []playertrack.LobbyPlayer{
					{
						PlayerID: "Steam|76561198000000003|0",
						Name:     "StressPlayer3",
						TeamNum:  1,
						CurrentRank: &playertrack.PlayerPlaylistRank{
							PlaylistID: 11,
							MMR:        1065.0,
						},
					},
				},
				UpdatedAt: time.Now().UTC(),
			}
			sessionTracker.RecordActiveMatch(activeMatch)
			time.Sleep(5 * time.Millisecond)

			// 2. Conclude
			concluded := activeMatch.DeepClone()
			concluded.ActiveMatch = false
			concluded.MatchEnded = true
			concluded.WinnerTeam = &winner
			concluded.Result = "victory"
			if winner != 0 {
				concluded.Result = "defeat"
			}
			concluded.LocalPlayer.CurrentRank.MMR += 9.0
			sessionTracker.ConcludeMatch(concluded)
			time.Sleep(5 * time.Millisecond)
		}
	}()

	// Spawn 15 concurrent HTTP client workers
	numWorkers := 15
	var clientWg sync.WaitGroup
	clientWg.Add(numWorkers)

	for w := 0; w < numWorkers; w++ {
		workerID := w
		go func() {
			defer clientWg.Done()
			client := &http.Client{Timeout: 5 * time.Second}

			endpoints := []string{
				"/api/current-match",
				"/current-match",
				"/api/session",
				"/api/players",
				"/api/players?query=StressPlayer",
				"/api/players?platform=Steam",
				fmt.Sprintf("/api/players/%s", url.PathEscape("Steam|76561198000000001|0")),
				fmt.Sprintf("/api/players/%s", url.PathEscape(fmt.Sprintf("Steam|765611980000000%02d|0", (workerID%10)+1))),
			}

			for i := 0; i < 25; i++ {
				ep := endpoints[rand.Intn(len(endpoints))]
				resp, err := client.Get(server.URL + ep)
				atomic.AddInt64(&reqCount, 1)
				if err != nil {
					atomic.AddInt64(&errCount, 1)
					t.Errorf("Worker %d: GET %s failed: %v", workerID, ep, err)
					continue
				}

				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					atomic.AddInt64(&errCount, 1)
					t.Errorf("Worker %d: failed to read body for %s: %v", workerID, ep, err)
					continue
				}

				if resp.StatusCode != http.StatusOK {
					atomic.AddInt64(&errCount, 1)
					t.Errorf("Worker %d: GET %s expected 200, got %d", workerID, ep, resp.StatusCode)
					continue
				}

				bodyStr := string(body)

				// Assert that no raw TrueSkill decimals like 47.5 exist in any response
				if strings.Contains(bodyStr, "47.5") {
					atomic.AddInt64(&errCount, 1)
					t.Errorf("Worker %d: GET %s contained raw rating 47.5: %s", workerID, ep, bodyStr)
				}

				// Verify JSON parseability based on endpoint
				if ep == "/api/session" {
					var sResp session.SessionResponse
					if err := json.Unmarshal(body, &sResp); err != nil {
						atomic.AddInt64(&errCount, 1)
						t.Errorf("Worker %d: invalid JSON on /api/session: %v", workerID, err)
					}
				} else if ep == "/api/current-match" || ep == "/current-match" {
					var mResp playertrack.CurrentMatchResponse
					if err := json.Unmarshal(body, &mResp); err != nil {
						atomic.AddInt64(&errCount, 1)
						t.Errorf("Worker %d: invalid JSON on %s: %v", workerID, ep, err)
					}
				}
			}

			// Also perform occasional session reset
			if workerID%5 == 0 {
				resetResp, err := client.Post(server.URL+"/api/session/reset", "application/json", nil)
				if err == nil {
					_ = resetResp.Body.Close()
				}
			}
		}()
	}

	clientWg.Wait()
	atomic.StoreInt32(&stopSignal, 1)
	bgWg.Wait()

	totalReqs := atomic.LoadInt64(&reqCount)
	totalErrs := atomic.LoadInt64(&errCount)
	t.Logf("Stress test complete: %d requests handled, %d errors encountered", totalReqs, totalErrs)

	if totalErrs > 0 {
		t.Fatalf("encountered %d errors during concurrent REST API stress test", totalErrs)
	}
}

// TestChallenger_Stress_API_LegacyDeserializationMigration tests that legacy database
// records containing raw TrueSkill decimal ratings (e.g. 47.5, 30.46, 81.86)
// are dynamically migrated on API retrieval without altering the database schema.
func TestChallenger_Stress_API_LegacyDeserializationMigration(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	// Seed legacy records with raw decimal ratings (e.g. 47.5 -> 1050, 30.4646 -> 709, 81.8645 -> 1737)
	legacyPlayers := []struct {
		id        string
		name      string
		rawRating float64
		expMMR    float64
	}{
		{id: "Steam|legacy_player_1|0", name: "Legacy1", rawRating: 47.5, expMMR: 1050.0},
		{id: "Steam|legacy_player_2|0", name: "Legacy2", rawRating: 30.4646, expMMR: 709.0},
		{id: "Epic|legacy_player_3|0", name: "Legacy3", rawRating: 81.8645, expMMR: 1737.0},
	}

	for _, lp := range legacyPlayers {
		ranksJSON := fmt.Sprintf(`{"11":{"tier":16,"division":2,"mmr":%f}}`, lp.rawRating)
		record := &storage.PlayerRecord{
			PlayerID:    lp.id,
			PlayerName:  lp.name,
			Platform:    strings.Split(lp.id, "|")[0],
			RanksJSON:   ranksJSON,
			FirstSeenAt: time.Now().Add(-24 * time.Hour),
			LastSeenAt:  time.Now(),
		}
		if err := store.UpsertPlayer(ctx, record); err != nil {
			t.Fatalf("failed to upsert legacy player: %v", err)
		}
	}

	cfg := config.NewDefaultConfig()
	cfg.Web.Enabled = true

	d, err := daemon.New(
		newMockSyncer(),
		cfg,
		daemon.WithStore(store),
	)
	if err != nil {
		t.Fatalf("failed to create daemon: %v", err)
	}

	server := httptest.NewServer(d.Handler(ctx))
	defer server.Close()
	client := server.Client()

	// 1. Check GET /api/players
	respList, err := client.Get(server.URL + "/api/players")
	if err != nil {
		t.Fatalf("GET /api/players failed: %v", err)
	}
	defer respList.Body.Close()

	var searchResp daemon.PlayerSearchResponse
	if err := json.NewDecoder(respList.Body).Decode(&searchResp); err != nil {
		t.Fatalf("failed to decode searchResp: %v", err)
	}
	if len(searchResp.Players) != 3 {
		t.Fatalf("expected 3 legacy players, got %d", len(searchResp.Players))
	}

	// 2. Query each player via GET /api/players/{id} and verify rank migration
	for _, lp := range legacyPlayers {
		escapedID := url.PathEscape(lp.id)
		respDetail, err := client.Get(fmt.Sprintf("%s/api/players/%s", server.URL, escapedID))
		if err != nil {
			t.Fatalf("GET /api/players/%s failed: %v", escapedID, err)
		}
		defer respDetail.Body.Close()

		if respDetail.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for %s, got %d", lp.id, respDetail.StatusCode)
		}

		var detailResp daemon.PlayerDetailResponse
		if err := json.NewDecoder(respDetail.Body).Decode(&detailResp); err != nil {
			t.Fatalf("failed to decode detailResp for %s: %v", lp.id, err)
		}

		// Parse the returned ranksJSON
		parsedRanks, err := playertrack.ParseRanksJSON(detailResp.PlayerRecord.RanksJSON)
		if err != nil {
			t.Fatalf("failed to parse ranks JSON for %s: %v", lp.id, err)
		}

		rank11, ok := parsedRanks["11"]
		if !ok {
			t.Fatalf("missing playlist 11 in parsed ranks for %s", lp.id)
		}

		if rank11.MMR != lp.expMMR {
			t.Errorf("player %s: expected MMR migrated to %f, got %f", lp.id, lp.expMMR, rank11.MMR)
		}
	}
}

// TestChallenger_Stress_SSE_ConcurrentDistributionUnderLoad verifies that multiple concurrent
// SSE subscribers on GET /api/events receive true in-game MMR events during rapid match lifecycle changes
// without deadlock, channel blocking, or data corruption.
func TestChallenger_Stress_SSE_ConcurrentDistributionUnderLoad(t *testing.T) {
	sessionTracker := session.NewSessionTracker()
	cfg := config.NewDefaultConfig()
	cfg.Web.Enabled = true

	d, err := daemon.New(
		newMockSyncer(),
		cfg,
		daemon.WithSessionTracker(sessionTracker),
	)
	if err != nil {
		t.Fatalf("failed to create daemon: %v", err)
	}

	server := httptest.NewServer(d.Handler(context.Background()))
	defer server.Close()

	numSubscribers := 8
	var subWg sync.WaitGroup
	subWg.Add(numSubscribers)

	clientCtx, cancelClients := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelClients()

	receivedCount := int64(0)

	for i := 0; i < numSubscribers; i++ {
		go func(subID int) {
			defer subWg.Done()
			req, err := http.NewRequestWithContext(clientCtx, http.MethodGet, server.URL+"/api/events", nil)
			if err != nil {
				return
			}

			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()

			reader := bufio.NewReader(resp.Body)
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if strings.Contains(line, "1050") || strings.Contains(line, "1059") {
					atomic.AddInt64(&receivedCount, 1)
				}
			}
		}(i)
	}

	// Give clients time to connect
	time.Sleep(100 * time.Millisecond)

	// Broadcast 15 match updates with in-game MMR
	localTeam := 0
	winner := 0
	for m := 1; m <= 15; m++ {
		match := &playertrack.CurrentMatchResponse{
			ActiveMatch:  false,
			MatchEnded:   true,
			MatchGUID:    fmt.Sprintf("sse-stress-match-%d", m),
			PlaylistID:   11,
			PlaylistName: "Ranked Doubles 2v2",
			LocalTeam:    &localTeam,
			WinnerTeam:   &winner,
			Result:       "victory",
			LocalPlayer: &playertrack.LobbyPlayer{
				PlayerID: "Epic|sse_hero|0",
				Name:     "SSEHero",
				TeamNum:  0,
				IsLocal:  true,
				CurrentRank: &playertrack.PlayerPlaylistRank{
					PlaylistID: 11,
					MMR:        1059.0, // In-game MMR
				},
			},
		}
		sessionTracker.ConcludeMatch(match)
		time.Sleep(10 * time.Millisecond)
	}

	// Wait briefly then cancel client connections
	time.Sleep(200 * time.Millisecond)
	cancelClients()
	subWg.Wait()

	totalReceived := atomic.LoadInt64(&receivedCount)
	t.Logf("SSE stress complete: %d in-game MMR events observed by subscribers", totalReceived)
	if totalReceived == 0 {
		t.Error("expected SSE subscribers to receive events containing in-game MMR 1050 or 1059")
	}
}
