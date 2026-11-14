package playertrack

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/psynet"
	"github.com/dank/rlapi"
)

func TestMockSkillFetcher_BatchSuccess(t *testing.T) {
	ctx := context.Background()
	mock := NewMockSkillFetcher()

	p1 := rlapi.PlayerID("Epic|account1|0")
	p2 := rlapi.PlayerID("Steam|76561198000000001|0")

	mock.SkillsMap[p1] = []rlapi.Skill{
		{Playlist: 11, Tier: 17, Division: 3, MMR: 1150.5, MatchesPlayed: 100},
	}
	mock.SkillsMap[p2] = []rlapi.Skill{
		{Playlist: 11, Tier: 16, Division: 1, MMR: 1060.0, MatchesPlayed: 50},
	}

	results, err := mock.GetPlayersSkills(ctx, []rlapi.PlayerID{p1, p2})
	if err != nil {
		t.Fatalf("GetPlayersSkills failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 RPC call, got %d", len(mock.Calls))
	}

	if results[0].PlayerID != p1 {
		t.Errorf("results[0].PlayerID = %q; want %q", results[0].PlayerID, p1)
	}
	if len(results[0].Skills) != 1 || results[0].Skills[0].Tier != 17 {
		t.Errorf("results[0].Skills mismatch: %+v", results[0].Skills)
	}
}

func TestMockSkillFetcher_EmptyPlayerList(t *testing.T) {
	ctx := context.Background()
	mock := NewMockSkillFetcher()

	results, err := mock.GetPlayersSkills(ctx, []rlapi.PlayerID{})
	if err != nil {
		t.Fatalf("GetPlayersSkills with empty list failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestMockSkillFetcher_UnrankedPlayer(t *testing.T) {
	ctx := context.Background()
	mock := NewMockSkillFetcher()

	p := rlapi.PlayerID("Epic|unrankedPlayer|0")
	mock.SkillsMap[p] = []rlapi.Skill{
		{Playlist: 11, Tier: 0, Division: 0, MMR: 600.0, MatchesPlayed: 0},
	}

	results, err := mock.GetPlayersSkills(ctx, []rlapi.PlayerID{p})
	if err != nil {
		t.Fatalf("GetPlayersSkills failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	rankName := FormatRank(results[0].Skills[0].Tier, results[0].Skills[0].Division)
	if rankName != "Unranked" {
		t.Errorf("rankName = %q; want 'Unranked'", rankName)
	}
}

func TestMockSkillFetcher_SSLRankedPlayer(t *testing.T) {
	ctx := context.Background()
	mock := NewMockSkillFetcher()

	p := rlapi.PlayerID("Epic|sslPlayer|0")
	mock.SkillsMap[p] = []rlapi.Skill{
		{Playlist: 11, Tier: 22, Division: 0, MMR: 1850.0, MatchesPlayed: 300},
	}

	results, err := mock.GetPlayersSkills(ctx, []rlapi.PlayerID{p})
	if err != nil {
		t.Fatalf("GetPlayersSkills failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	rankName := FormatRank(results[0].Skills[0].Tier, results[0].Skills[0].Division)
	if rankName != "Supersonic Legend" {
		t.Errorf("rankName = %q; want 'Supersonic Legend'", rankName)
	}
}

func TestMockSkillFetcher_RpcError(t *testing.T) {
	ctx := context.Background()
	mock := NewMockSkillFetcher()
	mock.Err = errors.New("PsyNet RPC: Session expired")

	p := rlapi.PlayerID("Epic|test|0")
	_, err := mock.GetPlayersSkills(ctx, []rlapi.PlayerID{p})
	if err == nil {
		t.Fatalf("expected RPC error, got nil")
	}
	if !strings.Contains(err.Error(), "Session expired") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMockSkillFetcher_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mock := NewMockSkillFetcher()
	p := rlapi.PlayerID("Epic|test|0")
	_, err := mock.GetPlayersSkills(ctx, []rlapi.PlayerID{p})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}
}

func TestMockSkillFetcher_Close(t *testing.T) {
	mock := NewMockSkillFetcher()
	if !mock.IsEnabled() {
		t.Errorf("expected mock to be enabled initially")
	}
	if err := mock.Close(); err != nil {
		t.Errorf("unexpected error on Close: %v", err)
	}
	if mock.IsEnabled() {
		t.Errorf("expected mock to be disabled after Close")
	}
	_, err := mock.GetPlayersSkills(context.Background(), []rlapi.PlayerID{"Epic|test|0"})
	if !errors.Is(err, ErrRankClientClosed) {
		t.Errorf("expected ErrRankClientClosed, got: %v", err)
	}
}

// ============================================================================
// Category 8: Offline Fallback, Degradation & Collision Checks
// ============================================================================

func TestNoOpRankClient(t *testing.T) {
	client := NewNoOpRankClient()
	if client.IsEnabled() {
		t.Errorf("NoOpRankClient.IsEnabled() = true; want false")
	}

	ctx := context.Background()
	results, err := client.GetPlayersSkills(ctx, []rlapi.PlayerID{"Epic|test|0"})
	if err != nil {
		t.Errorf("NoOpRankClient returned error: %v", err)
	}
	if results != nil {
		t.Errorf("NoOpRankClient returned non-nil results: %+v", results)
	}
	if err := client.Close(); err != nil {
		t.Errorf("NoOpRankClient.Close() returned error: %v", err)
	}
}

func TestCheckCredentialCollision(t *testing.T) {
	tests := []struct {
		name      string
		primary   config.AuthConfig
		polling   config.PollingAuthConfig
		expectErr bool
	}{
		{
			name: "different_providers_epic_and_steam",
			primary: config.AuthConfig{
				Provider: "epic",
				Epic:     config.EpicConfig{RefreshToken: "tok1", AccountID: "acc1"},
			},
			polling: config.PollingAuthConfig{
				Enabled:  true,
				Provider: "steam",
				Steam:    config.SteamConfig{SessionTicket: "ticket1", SteamID64: "76561198000000001"},
			},
			expectErr: false,
		},
		{
			name: "epic_matching_refresh_token",
			primary: config.AuthConfig{
				Provider: "epic",
				Epic:     config.EpicConfig{RefreshToken: "dup-token"},
			},
			polling: config.PollingAuthConfig{
				Enabled:  true,
				Provider: "epic",
				Epic:     config.EpicConfig{RefreshToken: "dup-token"},
			},
			expectErr: true,
		},
		{
			name: "epic_matching_account_id",
			primary: config.AuthConfig{
				Provider: "epic",
				Epic:     config.EpicConfig{RefreshToken: "tok1", AccountID: "same-acct"},
			},
			polling: config.PollingAuthConfig{
				Enabled:  true,
				Provider: "epic",
				Epic:     config.EpicConfig{RefreshToken: "tok2", AccountID: "same-acct"},
			},
			expectErr: true,
		},
		{
			name: "epic_matching_auth_code",
			primary: config.AuthConfig{
				Provider: "epic",
				Epic:     config.EpicConfig{AuthCode: "same-code"},
			},
			polling: config.PollingAuthConfig{
				Enabled:  true,
				Provider: "epic",
				Epic:     config.EpicConfig{AuthCode: "same-code"},
			},
			expectErr: true,
		},
		{
			name: "steam_matching_session_ticket",
			primary: config.AuthConfig{
				Provider: "steam",
				Steam:    config.SteamConfig{SessionTicket: "same-ticket", SteamID64: "111"},
			},
			polling: config.PollingAuthConfig{
				Enabled:  true,
				Provider: "steam",
				Steam:    config.SteamConfig{SessionTicket: "same-ticket", SteamID64: "222"},
			},
			expectErr: true,
		},
		{
			name: "steam_matching_steam_id_64",
			primary: config.AuthConfig{
				Provider: "steam",
				Steam:    config.SteamConfig{SessionTicket: "ticket1", SteamID64: "same-steam-id"},
			},
			polling: config.PollingAuthConfig{
				Enabled:  true,
				Provider: "steam",
				Steam:    config.SteamConfig{SessionTicket: "ticket2", SteamID64: "same-steam-id"},
			},
			expectErr: true,
		},
		{
			name: "epic_distinct_tokens",
			primary: config.AuthConfig{
				Provider: "epic",
				Epic:     config.EpicConfig{RefreshToken: "tok1", AccountID: "acc1"},
			},
			polling: config.PollingAuthConfig{
				Enabled:  true,
				Provider: "epic",
				Epic:     config.EpicConfig{RefreshToken: "tok2", AccountID: "acc2"},
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckCredentialCollision(tt.primary, tt.polling)
			if tt.expectErr && err == nil {
				t.Fatalf("expected collision error, got nil")
			}
			if !tt.expectErr && err != nil {
				t.Fatalf("expected no collision, got: %v", err)
			}
			if tt.expectErr && !errors.Is(err, ErrCredentialCollision) {
				t.Errorf("expected ErrCredentialCollision, got: %v", err)
			}
		})
	}
}

func TestNewRankClient_Factory(t *testing.T) {
	t.Run("disabled returns NoOpRankClient", func(t *testing.T) {
		client, err := NewRankClient(RankClientConfig{
			PollingAuth: config.PollingAuthConfig{Enabled: false},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client.IsEnabled() {
			t.Errorf("expected disabled NoOpRankClient")
		}
		if _, ok := client.(*NoOpRankClient); !ok {
			t.Errorf("expected *NoOpRankClient type, got %T", client)
		}
	})

	t.Run("credential collision returns NoOpRankClient", func(t *testing.T) {
		client, err := NewRankClient(RankClientConfig{
			PrimaryAuth: config.AuthConfig{
				Provider: "epic",
				Epic:     config.EpicConfig{RefreshToken: "duplicate"},
			},
			PollingAuth: config.PollingAuthConfig{
				Enabled:  true,
				Provider: "epic",
				Epic:     config.EpicConfig{RefreshToken: "duplicate"},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client.IsEnabled() {
			t.Errorf("expected disabled NoOpRankClient on collision")
		}
	})

	t.Run("missing credentials returns NoOpRankClient", func(t *testing.T) {
		client, err := NewRankClient(RankClientConfig{
			PrimaryAuth: config.AuthConfig{
				Provider: "steam",
				Steam:    config.SteamConfig{SessionTicket: "t1", SteamID64: "1"},
			},
			PollingAuth: config.PollingAuthConfig{
				Enabled:  true,
				Provider: "epic",
				// No RefreshToken or AuthCode
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client.IsEnabled() {
			t.Errorf("expected disabled NoOpRankClient on missing credentials")
		}
	})

	t.Run("valid polling config returns active client with RPCFactory", func(t *testing.T) {
		mockRPC := &testSkillRPC{connected: true}
		client, err := NewRankClient(RankClientConfig{
			PrimaryAuth: config.AuthConfig{
				Provider: "steam",
				Steam:    config.SteamConfig{SessionTicket: "t1", SteamID64: "1"},
			},
			PollingAuth: config.PollingAuthConfig{
				Enabled:  true,
				Provider: "epic",
				Epic:     config.EpicConfig{RefreshToken: "secondary-token"},
			},
			CredentialsSupplier: psynet.StaticCredentials{
				Platform:  "Epic",
				AuthToken: "eos-token",
				AccountID: "sec-acc",
			},
			RPCFactory: func(ctx context.Context, creds *psynet.Credentials) (SkillRPCClient, error) {
				return mockRPC, nil
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !client.IsEnabled() {
			t.Errorf("expected active client")
		}

		res, err := client.GetPlayersSkills(context.Background(), []rlapi.PlayerID{"Epic|test|0"})
		if err != nil {
			t.Fatalf("GetPlayersSkills failed: %v", err)
		}
		if len(res) != 1 {
			t.Errorf("expected 1 result, got %d", len(res))
		}
		_ = client.Close()
	})
}

// ============================================================================
// Category 9: PsyNetRankClient Connection Lifecycle & Reconnect Tests
// ============================================================================
