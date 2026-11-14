package playertrack

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/auth"
	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/psynet"
	"github.com/dank/rlapi"
)

type testSkillRPC struct {
	mu           sync.Mutex
	skills       map[rlapi.PlayerID][]rlapi.Skill
	connected    bool
	closed       bool
	callCount    int
	failNextWith error
}

func (r *testSkillRPC) GetPlayersSkills(ctx context.Context, playerIDs []rlapi.PlayerID) ([]rlapi.PlayerWithSkills, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.callCount++
	if r.failNextWith != nil {
		err := r.failNextWith
		r.failNextWith = nil
		return nil, err
	}

	var results []rlapi.PlayerWithSkills
	for _, pid := range playerIDs {
		skills := r.skills[pid]
		results = append(results, rlapi.PlayerWithSkills{
			PlayerID: pid,
			Skills:   skills,
		})
	}
	return results, nil
}

func (r *testSkillRPC) IsConnected() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.connected && !r.closed
}

func (r *testSkillRPC) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	r.connected = false
	return nil
}

func TestPsyNetRankClient_EmptyPlayerIDs_NoNetworkCall(t *testing.T) {
	var rpcCreated int32
	client, err := NewPsyNetRankClient(RankClientConfig{
		CredentialsSupplier: psynet.StaticCredentials{
			Platform:  "Epic",
			AuthToken: "tok",
			AccountID: "acc",
		},
		RPCFactory: func(ctx context.Context, creds *psynet.Credentials) (SkillRPCClient, error) {
			atomic.AddInt32(&rpcCreated, 1)
			return &testSkillRPC{connected: true}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewPsyNetRankClient failed: %v", err)
	}
	defer client.Close()

	res, err := client.GetPlayersSkills(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != nil {
		t.Errorf("expected nil results for empty playerIDs")
	}
	if atomic.LoadInt32(&rpcCreated) != 0 {
		t.Errorf("expected no RPC connection created for empty request")
	}
}

func TestPsyNetRankClient_TransparentReconnectOnDrop(t *testing.T) {
	var factoryCalls int32
	firstRPC := &testSkillRPC{
		connected:    true,
		failNextWith: rlapi.ErrConnectionClosed, // First call fails with dropped connection
	}
	secondRPC := &testSkillRPC{
		connected: true,
		skills: map[rlapi.PlayerID][]rlapi.Skill{
			"Epic|reconnect-player|0": {
				{Playlist: 11, Tier: 17, Division: 3, MMR: 1150.0},
			},
		},
	}

	client, err := NewPsyNetRankClient(RankClientConfig{
		CredentialsSupplier: psynet.StaticCredentials{
			Platform:  "Epic",
			AuthToken: "tok",
			AccountID: "acc",
		},
		RPCFactory: func(ctx context.Context, creds *psynet.Credentials) (SkillRPCClient, error) {
			count := atomic.AddInt32(&factoryCalls, 1)
			if count == 1 {
				return firstRPC, nil
			}
			return secondRPC, nil
		},
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	res, err := client.GetPlayersSkills(context.Background(), []rlapi.PlayerID{"Epic|reconnect-player|0"})
	if err != nil {
		t.Fatalf("expected transparent reconnect to succeed, got: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 result, got %d", len(res))
	}
	if res[0].Skills[0].Tier != 17 {
		t.Errorf("expected Tier 17, got %d", res[0].Skills[0].Tier)
	}
	if atomic.LoadInt32(&factoryCalls) != 2 {
		t.Errorf("expected 2 factory calls (initial + reconnect), got %d", atomic.LoadInt32(&factoryCalls))
	}
}

func TestPsyNetRankClient_CloseAndClosedErrors(t *testing.T) {
	mockRPC := &testSkillRPC{connected: true}
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
		t.Fatalf("failed to create client: %v", err)
	}

	if !client.IsEnabled() {
		t.Errorf("expected client to be enabled")
	}

	// Trigger connection
	_, err = client.GetPlayersSkills(context.Background(), []rlapi.PlayerID{"Epic|p1|0"})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	// Close client
	if err := client.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if client.IsEnabled() {
		t.Errorf("expected client to be disabled after Close()")
	}
	if !mockRPC.closed {
		t.Errorf("expected underlying RPC to be closed")
	}

	// Double close should succeed cleanly
	if err := client.Close(); err != nil {
		t.Errorf("double close failed: %v", err)
	}

	// Queries after close return ErrRankClientClosed
	_, err = client.GetPlayersSkills(context.Background(), []rlapi.PlayerID{"Epic|p1|0"})
	if !errors.Is(err, ErrRankClientClosed) {
		t.Errorf("expected ErrRankClientClosed, got: %v", err)
	}
}

func TestPsyNetRankClient_RuntimeCollisionDetection(t *testing.T) {
	primaryAcct := "primary-account-guid"
	client, err := NewPsyNetRankClient(RankClientConfig{
		PrimaryAuth: config.AuthConfig{
			Provider: "epic",
			Epic:     config.EpicConfig{AccountID: primaryAcct},
		},
		CredentialsSupplier: psynet.StaticCredentials{
			Platform:  "Epic",
			AuthToken: "tok",
			AccountID: primaryAcct, // Collision!
		},
		RPCFactory: func(ctx context.Context, creds *psynet.Credentials) (SkillRPCClient, error) {
			return &testSkillRPC{connected: true}, nil
		},
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	_, err = client.GetPlayersSkills(context.Background(), []rlapi.PlayerID{"Epic|p1|0"})
	if err == nil {
		t.Fatalf("expected collision error, got nil")
	}
	if !errors.Is(err, ErrCredentialCollision) && !strings.Contains(err.Error(), "conflict") {
		t.Errorf("expected collision error, got: %v", err)
	}
}

func TestPsyNetRankClient_Concurrency(t *testing.T) {
	mockRPC := &testSkillRPC{
		connected: true,
		skills: map[rlapi.PlayerID][]rlapi.Skill{
			"Epic|concurrent|0": {
				{Playlist: 11, Tier: 16, Division: 2, MMR: 1080.0},
			},
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
		t.Fatalf("failed to create client: %v", err)
	}

	const goroutines = 30
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, _ = client.GetPlayersSkills(ctx, []rlapi.PlayerID{"Epic|concurrent|0"})
		}()
	}

	// Close asynchronously after slight delay
	go func() {
		time.Sleep(10 * time.Millisecond)
		_ = client.Close()
	}()

	wg.Wait()
}

func TestAuthProviderSupplier(t *testing.T) {
	t.Run("nil provider returns ErrMissingCredentials", func(t *testing.T) {
		supplier := NewAuthProviderSupplier(nil)
		_, err := supplier.GetCredentials(context.Background())
		if !errors.Is(err, ErrMissingCredentials) {
			t.Errorf("expected ErrMissingCredentials, got %v", err)
		}
	})

	t.Run("valid mock AuthProvider creates credentials", func(t *testing.T) {
		mockAuth := &mockAuthProvider{
			name: "epic",
			tokenInfo: &auth.TokenInfo{
				AccessToken:   "test-access-token",
				EpicAccountID: "epic-eos-id",
				AccountID:     "epic-account-id",
				DisplayName:   "PollerName",
				ExpiresAt:     time.Now().Add(1 * time.Hour),
			},
		}

		supplier := NewAuthProviderSupplier(mockAuth)
		creds, err := supplier.GetCredentials(context.Background())
		if err != nil {
			t.Fatalf("GetCredentials failed: %v", err)
		}
		if creds.Platform != "Epic" {
			t.Errorf("creds.Platform = %q; want Epic", creds.Platform)
		}
		if creds.AuthToken != "test-access-token" {
			t.Errorf("creds.AuthToken = %q", creds.AuthToken)
		}
		if creds.AccountID != "epic-eos-id" {
			t.Errorf("creds.AccountID = %q; want epic-eos-id", creds.AccountID)
		}
		if creds.DisplayName != "PollerName" {
			t.Errorf("creds.DisplayName = %q; want PollerName", creds.DisplayName)
		}
	})

	t.Run("steam mock AuthProvider sets SteamAccountID", func(t *testing.T) {
		mockAuth := &mockAuthProvider{
			name: "steam",
			tokenInfo: &auth.TokenInfo{
				AccessToken: "steam-eos-token",
				AccountID:   "76561198000000001",
				DisplayName: "SteamPoller",
				ExpiresAt:   time.Now().Add(1 * time.Hour),
			},
		}

		supplier := NewAuthProviderSupplier(mockAuth)
		creds, err := supplier.GetCredentials(context.Background())
		if err != nil {
			t.Fatalf("GetCredentials failed: %v", err)
		}
		if creds.Platform != "Steam" {
			t.Errorf("creds.Platform = %q; want Steam", creds.Platform)
		}
		if creds.SteamAccountID != "76561198000000001" {
			t.Errorf("creds.SteamAccountID = %q; want 76561198000000001", creds.SteamAccountID)
		}
	})
}

type mockAuthProvider struct {
	name      string
	tokenInfo *auth.TokenInfo
}

func (m *mockAuthProvider) Name() string { return m.name }
func (m *mockAuthProvider) Authenticate(ctx context.Context) (*auth.TokenInfo, error) {
	return m.tokenInfo, nil
}
func (m *mockAuthProvider) Refresh(ctx context.Context, refreshToken string) (*auth.TokenInfo, error) {
	return m.tokenInfo, nil
}
func (m *mockAuthProvider) TokenInfo() *auth.TokenInfo { return m.tokenInfo }
func (m *mockAuthProvider) Validate() error            { return nil }

// ============================================================================
// Category 10: Milestone 1 True In-Game MMR Conversion (R1) Tests
// ============================================================================
