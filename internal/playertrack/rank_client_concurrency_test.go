package playertrack

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/psynet"
	"github.com/dank/rlapi"
)

func TestResilience_NewRankClient_GracefulFallbackGrid(t *testing.T) {
	testCases := []struct {
		name    string
		primary config.AuthConfig
		polling config.PollingAuthConfig
	}{
		{
			name:    "Polling disabled explicitly",
			primary: config.AuthConfig{Provider: "epic", Epic: config.EpicConfig{RefreshToken: "tok1"}},
			polling: config.PollingAuthConfig{Enabled: false},
		},
		{
			name:    "Collision on Epic refresh token",
			primary: config.AuthConfig{Provider: "epic", Epic: config.EpicConfig{RefreshToken: "same-tok"}},
			polling: config.PollingAuthConfig{Enabled: true, Provider: "epic", Epic: config.EpicConfig{RefreshToken: "same-tok"}},
		},
		{
			name:    "Collision on Epic account ID",
			primary: config.AuthConfig{Provider: "epic", Epic: config.EpicConfig{AccountID: "same-acc"}},
			polling: config.PollingAuthConfig{Enabled: true, Provider: "epic", Epic: config.EpicConfig{AccountID: "same-acc"}},
		},
		{
			name:    "Collision on Steam session ticket",
			primary: config.AuthConfig{Provider: "steam", Steam: config.SteamConfig{SessionTicket: "same-ticket"}},
			polling: config.PollingAuthConfig{Enabled: true, Provider: "steam", Steam: config.SteamConfig{SessionTicket: "same-ticket"}},
		},
		{
			name:    "Collision on Steam steam_id_64",
			primary: config.AuthConfig{Provider: "steam", Steam: config.SteamConfig{SteamID64: "same-steam64"}},
			polling: config.PollingAuthConfig{Enabled: true, Provider: "steam", Steam: config.SteamConfig{SteamID64: "same-steam64"}},
		},
		{
			name:    "Missing Epic credentials (no refresh token and no auth code)",
			primary: config.AuthConfig{Provider: "steam", Steam: config.SteamConfig{SteamID64: "123"}},
			polling: config.PollingAuthConfig{Enabled: true, Provider: "epic"},
		},
		{
			name:    "Missing Steam credentials (no ticket)",
			primary: config.AuthConfig{Provider: "epic", Epic: config.EpicConfig{RefreshToken: "tok1"}},
			polling: config.PollingAuthConfig{Enabled: true, Provider: "steam", Steam: config.SteamConfig{SteamID64: "123"}},
		},
		{
			name:    "Invalid provider name",
			primary: config.AuthConfig{Provider: "epic", Epic: config.EpicConfig{RefreshToken: "tok1"}},
			polling: config.PollingAuthConfig{Enabled: true, Provider: "nintendo"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewRankClient(RankClientConfig{
				PrimaryAuth: tc.primary,
				PollingAuth: tc.polling,
			})
			if err != nil {
				t.Fatalf("NewRankClient returned unexpected error: %v", err)
			}
			if client.IsEnabled() {
				t.Errorf("expected client to be disabled (NoOpRankClient)")
			}
			if _, ok := client.(*NoOpRankClient); !ok {
				t.Errorf("expected *NoOpRankClient, got %T", client)
			}

			// Calling GetPlayersSkills must immediately succeed with nil, nil
			res, err := client.GetPlayersSkills(context.Background(), []rlapi.PlayerID{"Epic|test|0"})
			if err != nil || res != nil {
				t.Errorf("fallback client returned non-(nil, nil): res=%+v, err=%v", res, err)
			}
		})
	}
}

// ============================================================================
// Suite 4: 50+ Concurrent Queries Race Conditions & Multi-Threaded Stress
// ============================================================================

// TestResilience_HighConcurrency_50PlusQueries tests 100 simultaneous goroutines
// querying GetPlayersSkills under race detection.
func TestResilience_HighConcurrency_50PlusQueries(t *testing.T) {
	const goroutines = 100
	const queriesPerGoroutine = 20

	sharedRPC := &controllableSkillRPC{
		connected: true,
		skills: map[rlapi.PlayerID][]rlapi.Skill{
			"Epic|p1|0": {{Playlist: 11, Tier: 17, Division: 3, MMR: 1150.0}},
			"Epic|p2|0": {{Playlist: 13, Tier: 16, Division: 2, MMR: 1080.0}},
			"Steam|1|0": {{Playlist: 27, Tier: 22, Division: 0, MMR: 1400.0}},
		},
	}

	client, err := NewPsyNetRankClient(RankClientConfig{
		CredentialsSupplier: psynet.StaticCredentials{
			Platform:  "Epic",
			AuthToken: "tok",
			AccountID: "acc",
		},
		RPCFactory: func(ctx context.Context, creds *psynet.Credentials) (SkillRPCClient, error) {
			return sharedRPC, nil
		},
	})
	if err != nil {
		t.Fatalf("NewPsyNetRankClient failed: %v", err)
	}
	defer client.Close()

	var wg sync.WaitGroup
	wg.Add(goroutines)

	startBarrier := make(chan struct{})

	for g := 0; g < goroutines; g++ {
		go func(gid int) {
			defer wg.Done()
			<-startBarrier

			pid := rlapi.PlayerID("Epic|p1|0")
			if gid%3 == 1 {
				pid = "Epic|p2|0"
			} else if gid%3 == 2 {
				pid = "Steam|1|0"
			}

			for q := 0; q < queriesPerGoroutine; q++ {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				res, err := client.GetPlayersSkills(ctx, []rlapi.PlayerID{pid})
				cancel()

				if err != nil {
					t.Errorf("goroutine %d query %d failed: %v", gid, q, err)
					return
				}
				if len(res) != 1 {
					t.Errorf("goroutine %d query %d: expected 1 result, got %d", gid, q, len(res))
					return
				}
			}
		}(g)
	}

	// Release all 100 goroutines simultaneously
	close(startBarrier)
	wg.Wait()

	totalExpectedCalls := goroutines * queriesPerGoroutine
	sharedRPC.mu.Lock()
	actualCalls := sharedRPC.callCount
	sharedRPC.mu.Unlock()

	if actualCalls != totalExpectedCalls {
		t.Errorf("sharedRPC handled %d calls; want %d", actualCalls, totalExpectedCalls)
	}
}

// TestResilience_HighConcurrency_WithConnectionDropAndReconnect tests 50 concurrent
// goroutines when the active connection drops and reconnect occurs.
func TestResilience_HighConcurrency_WithConnectionDropAndReconnect(t *testing.T) {
	const goroutines = 50

	var factoryCount int32
	firstRPC := &controllableSkillRPC{
		connected: true,
		failFunc: func(call int, playerIDs []rlapi.PlayerID) error {
			// Fail calls to simulate connection drop
			return rlapi.ErrConnectionClosed
		},
	}

	client, err := NewPsyNetRankClient(RankClientConfig{
		CredentialsSupplier: psynet.StaticCredentials{
			Platform:  "Epic",
			AuthToken: "tok",
			AccountID: "acc",
		},
		RPCFactory: func(ctx context.Context, creds *psynet.Credentials) (SkillRPCClient, error) {
			idx := atomic.AddInt32(&factoryCount, 1)
			if idx == 1 {
				return firstRPC, nil
			}
			// Each reconnect creates a distinct active RPC instance
			return &controllableSkillRPC{
				connected: true,
				skills: map[rlapi.PlayerID][]rlapi.Skill{
					"Epic|reconnect|0": {{Playlist: 11, Tier: 18, Division: 0, MMR: 1200.0}},
				},
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewPsyNetRankClient failed: %v", err)
	}
	defer client.Close()

	var wg sync.WaitGroup
	wg.Add(goroutines)
	startBarrier := make(chan struct{})

	for g := 0; g < goroutines; g++ {
		go func(gid int) {
			defer wg.Done()
			<-startBarrier

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			res, err := client.GetPlayersSkills(ctx, []rlapi.PlayerID{"Epic|reconnect|0"})
			if err != nil {
				// We record any failures during concurrent reconnect
				t.Logf("goroutine %d encountered error during reconnect: %v", gid, err)
				return
			}
			if len(res) != 1 || res[0].Skills[0].Tier != 18 {
				t.Errorf("goroutine %d: unexpected result: %+v", gid, res)
			}
		}(g)
	}

	close(startBarrier)
	wg.Wait()

	t.Logf("Concurrent reconnect test finished. Total RPC factory calls: %d", atomic.LoadInt32(&factoryCount))
}

// TestResilience_HighConcurrency_ConcurrentClose queries GetPlayersSkills from 50 goroutines
// while another goroutine concurrently calls Close(). Verifies no deadlocks or panics under -race.
func TestResilience_HighConcurrency_ConcurrentClose(t *testing.T) {
	const goroutines = 50

	mockRPC := &controllableSkillRPC{
		connected: true,
		delay:     10 * time.Millisecond,
		skills: map[rlapi.PlayerID][]rlapi.Skill{
			"Epic|close-test|0": {{Playlist: 11, Tier: 15, Division: 1}},
		},
	}

	client, err := NewPsyNetRankClient(RankClientConfig{
		CredentialsSupplier: psynet.StaticCredentials{
			Platform:  "Epic",
			AuthToken: "tok",
			AccountID: "acc",
		},
		RPCFactory: func(ctx context.Context, creds *psynet.Credentials) (SkillRPCClient, error) {
			return mockRPC, nil
		},
	})
	if err != nil {
		t.Fatalf("NewPsyNetRankClient failed: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(gid int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()

			_, err := client.GetPlayersSkills(ctx, []rlapi.PlayerID{"Epic|close-test|0"})
			// It may succeed before close, or return ErrRankClientClosed / context errors after close
			if err != nil && !errors.Is(err, ErrRankClientClosed) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("unexpected error on concurrent close: %v", err)
			}
		}(g)
	}

	// Wait slightly then close client
	time.Sleep(2 * time.Millisecond)
	_ = client.Close()

	wg.Wait()

	// Post-close call must always return ErrRankClientClosed
	_, err = client.GetPlayersSkills(context.Background(), []rlapi.PlayerID{"Epic|close-test|0"})
	if !errors.Is(err, ErrRankClientClosed) {
		t.Errorf("expected ErrRankClientClosed post-close, got: %v", err)
	}
}
