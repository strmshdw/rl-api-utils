package syncer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/storage"
)

func TestBallchasingHistorySyncer_Sync(t *testing.T) {
	pageCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			// Ping endpoint
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(ballchasing.PingResponse{
				Name:    "HeroLocal",
				SteamID: "76561198000000001",
			})
			return
		}

		pageCount++
		w.Header().Set("Content-Type", "application/json")

		if pageCount == 1 {
			// Page 1: 1 match
			resp := ballchasing.ReplayListResponse{
				Count: 2,
				List: []ballchasing.ReplayItem{
					{
						ID:         "replay-page1",
						MatchGUID:  "guid-page1",
						MapName:    "DFH Stadium",
						PlaylistID: "ranked-doubles",
						Blue: ballchasing.ReplayTeam{
							Goals: 3,
							Players: []ballchasing.ReplayPlayer{
								{
									Name:  "HeroLocal",
									Score: 500,
									Goals: 2,
									ID:    ballchasing.PlayerIdentity{Platform: "steam", ID: "76561198000000001"},
								},
								{
									Name:  "TeammateBob",
									Score: 350,
									Goals: 1,
									ID:    ballchasing.PlayerIdentity{Platform: "epic", ID: "epic_bob"},
								},
							},
						},
						Orange: ballchasing.ReplayTeam{
							Goals: 1,
							Players: []ballchasing.ReplayPlayer{
								{
									Name:  "OpponentAlice",
									Score: 200,
									Goals: 1,
									ID:    ballchasing.PlayerIdentity{Platform: "steam", ID: "76561198000000002"},
								},
							},
						},
					},
				},
				Next: "/replays?page=2",
			}
			json.NewEncoder(w).Encode(resp)
			return
		}

		// Page 2: 1 match, Next empty
		resp := ballchasing.ReplayListResponse{
			Count: 2,
			List: []ballchasing.ReplayItem{
				{
					ID:         "replay-page2",
					MatchGUID:  "guid-page2",
					MapName:    "Utopia Stadium",
					PlaylistID: "ranked-doubles",
					Blue: ballchasing.ReplayTeam{
						Goals: 1,
						Players: []ballchasing.ReplayPlayer{
							{
								Name:  "HeroLocal",
								Score: 250,
								Goals: 1,
								ID:    ballchasing.PlayerIdentity{Platform: "steam", ID: "76561198000000001"},
							},
						},
					},
					Orange: ballchasing.ReplayTeam{
						Goals: 2,
						Players: []ballchasing.ReplayPlayer{
							{
								Name:  "OpponentAlice",
								Score: 300,
								Goals: 2,
								ID:    ballchasing.PlayerIdentity{Platform: "steam", ID: "76561198000000002"},
							},
						},
					},
				},
			},
			Next: "",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := ballchasing.NewClient(ballchasing.ClientConfig{
		BaseURL:    server.URL,
		APIKey:     "test-key",
		MaxRetries: 2,
	})
	if err != nil {
		t.Fatalf("failed to create ballchasing client: %v", err)
	}

	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	syncer := NewBallchasingHistorySyncer(client, store, "Steam|76561198000000001|0", "HeroLocal", nil)
	syncer.SetRateInterval(10 * time.Millisecond) // Fast in unit tests

	progress, err := syncer.Sync(ctx)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if progress.SyncedMatches != 2 {
		t.Errorf("expected 2 synced matches, got %d", progress.SyncedMatches)
	}
	if progress.SyncedPlayers < 3 {
		t.Errorf("expected at least 3 unique synced players, got %d", progress.SyncedPlayers)
	}

	// Verify players populated in storage
	bob, err := store.GetPlayer(ctx, "Epic|epic_bob|0")
	if err != nil {
		t.Fatalf("GetPlayer Bob failed: %v", err)
	}
	if bob.PlayerName != "TeammateBob" {
		t.Errorf("expected TeammateBob, got %s", bob.PlayerName)
	}

	// Verify matchup records for TeammateBob (played with, won)
	bobMatchup, err := store.GetPlayerMatchup(ctx, "Epic|epic_bob|0", 11)
	if err != nil {
		t.Fatalf("GetPlayerMatchup Bob failed: %v", err)
	}
	if bobMatchup.WinsAsTeammate != 1 {
		t.Errorf("expected 1 win as teammate for Bob, got %d", bobMatchup.WinsAsTeammate)
	}

	// Verify matchup records for OpponentAlice (played against twice: 1 loss as opponent in match 1, 1 win as opponent in match 2)
	aliceMatchup, err := store.GetPlayerMatchup(ctx, "Steam|76561198000000002|0", 11)
	if err != nil {
		t.Fatalf("GetPlayerMatchup Alice failed: %v", err)
	}
	if aliceMatchup.WinsAsOpponent != 1 || aliceMatchup.LossesAsOpponent != 1 {
		t.Errorf("expected 1 win and 1 loss as opponent for Alice, got %d wins %d losses",
			aliceMatchup.WinsAsOpponent, aliceMatchup.LossesAsOpponent)
	}

	// Verify historical matches listing
	hist, total, err := store.ListHistoricalMatches(ctx, storage.MatchFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListHistoricalMatches: %v", err)
	}
	if total != 2 || len(hist) != 2 {
		t.Errorf("expected 2 historical matches, got total %d len %d", total, len(hist))
	}
}

func TestBallchasingHistorySyncer_Cancellation(t *testing.T) {
	reqCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		w.Header().Set("Content-Type", "application/json")
		// Simulate infinite pages
		resp := ballchasing.ReplayListResponse{
			Count: 100,
			List: []ballchasing.ReplayItem{
				{
					ID:         "replay-cancel",
					MatchGUID:  "guid-cancel",
					MapName:    "DFH Stadium",
					PlaylistID: "ranked-doubles",
				},
			},
			Next: "/replays?page=next",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := ballchasing.NewClient(ballchasing.ClientConfig{
		BaseURL:    server.URL,
		APIKey:     "test-key",
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	syncer := NewBallchasingHistorySyncer(client, store, "Hero", "Hero", nil)
	syncer.SetRateInterval(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after 20ms
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err = syncer.Sync(ctx)
	if err == nil {
		t.Fatalf("expected error from cancelled sync, got nil")
	}

	// Verify syncer state is no longer Running
	prog := syncer.Progress()
	if prog.Running {
		t.Errorf("expected syncer not running after cancellation, but Running=true")
	}

	// Verify another sync can be launched immediately without 'already running' error
	ctx2, cancel2 := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel2()
	_, _ = syncer.Sync(ctx2)
	prog2 := syncer.Progress()
	if prog2.Running {
		t.Errorf("expected syncer not running after completion/timeout")
	}
}

