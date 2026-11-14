package auth

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rlapi"
)

func TestNewProvider_Success(t *testing.T) {
	store := newMockStateStore()

	epicCfg := config.AuthConfig{
		Provider: "epic",
		Epic:     config.EpicConfig{RefreshToken: "dummy-refresh"},
	}
	p1, err := NewProvider(epicCfg, store)
	if err != nil {
		t.Fatalf("unexpected error creating epic provider: %v", err)
	}
	if p1.Name() != "epic" {
		t.Fatalf("expected provider name 'epic', got %s", p1.Name())
	}

	steamCfg := config.AuthConfig{
		Provider: "steam",
		Steam: config.SteamConfig{
			SessionTicket: "dummy-ticket",
			SteamID64:     "76561198000000000",
		},
	}
	p2, err := NewProvider(steamCfg, store)
	if err != nil {
		t.Fatalf("unexpected error creating steam provider: %v", err)
	}
	if p2.Name() != "steam" {
		t.Fatalf("expected provider name 'steam', got %s", p2.Name())
	}
}

func TestNewProvider_Unsupported(t *testing.T) {
	store := newMockStateStore()
	for _, bc := range []string{"", "xbox", "psn", "nintendo"} {
		_, err := NewProvider(config.AuthConfig{Provider: bc}, store)
		if err == nil || !errors.Is(err, ErrUnsupportedProvider) {
			t.Fatalf("expected ErrUnsupportedProvider for %q, got: %v", bc, err)
		}
	}
}

func TestNewPollingProvider(t *testing.T) {
	t.Run("disabled returns ErrMissingCredentials", func(t *testing.T) {
		p, err := NewPollingProvider(config.PollingAuthConfig{Enabled: false, Provider: "epic"})
		if !errors.Is(err, ErrMissingCredentials) {
			t.Fatalf("expected ErrMissingCredentials, got: %v", err)
		}
		if p != nil {
			t.Fatalf("expected nil provider, got: %v", p)
		}
	})

	t.Run("enabled epic provider created with store nil", func(t *testing.T) {
		cfg := config.PollingAuthConfig{
			Enabled:  true,
			Provider: "epic",
			Epic:     config.EpicConfig{RefreshToken: "refresh-token-123"},
		}
		mock := &mockEGS{
			authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
				return &rlapi.TokenResponse{
					AccessToken:  "access-123",
					RefreshToken: "new-refresh-123",
					ExpiresIn:    3600,
					AccountID:    "account-123",
					DisplayName:  "EpicPlayer",
				}, nil
			},
			getExchangeCodeFunc: func(at string) (string, error) { return "code-xyz", nil },
			exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
				return &rlapi.EOSTokenResponse{AccessToken: "eos-token-123", ExpiresIn: 3600}, nil
			},
		}

		p, err := NewPollingProvider(cfg, WithEGSClient(mock))
		if err != nil {
			t.Fatalf("unexpected error creating polling provider: %v", err)
		}
		if p.Name() != "epic" {
			t.Errorf("expected provider name 'epic', got %q", p.Name())
		}
		token, err := p.Authenticate(context.Background())
		if err != nil {
			t.Fatalf("unexpected auth error: %v", err)
		}
		if token.AccessToken != "eos-token-123" {
			t.Errorf("expected access token eos-token-123, got %q", token.AccessToken)
		}
	})

	t.Run("enabled steam provider created with store nil", func(t *testing.T) {
		cfg := config.PollingAuthConfig{
			Enabled:  true,
			Provider: "steam",
			Steam: config.SteamConfig{
				SessionTicket: "01020304",
				SteamID64:     "76561198000000001",
				AccountName:   "SteamBot",
			},
		}
		mock := &mockEGS{
			exchangeEOSTokenFromSteamFunc: func(ticket string) (*rlapi.EOSTokenResponse, error) {
				return &rlapi.EOSTokenResponse{
					AccessToken: "eos-steam-tok",
					AccountID:   "eos-account-id",
					ExpiresIn:   3600,
				}, nil
			},
		}

		p, err := NewPollingProvider(cfg, WithEGSClient(mock))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Name() != "steam" {
			t.Errorf("expected provider name 'steam', got %q", p.Name())
		}
		token, err := p.Authenticate(context.Background())
		if err != nil {
			t.Fatalf("unexpected auth error: %v", err)
		}
		if token.AccessToken != "eos-steam-tok" {
			t.Errorf("expected eos-steam-tok, got %q", token.AccessToken)
		}
	})
}

func TestTokenInfo_IsExpired(t *testing.T) {
	var nilInfo *TokenInfo
	if !nilInfo.IsExpired() || nilInfo.Valid() {
		t.Fatal("nil TokenInfo must be expired and invalid")
	}

	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	clk := func() time.Time { return base }

	expiredInfo := &TokenInfo{AccessToken: "tok", ExpiresAt: base.Add(-10 * time.Minute), nowFn: clk}
	if !expiredInfo.IsExpired() || expiredInfo.Valid() {
		t.Fatal("past token should be expired and invalid")
	}

	expiringSoon := &TokenInfo{AccessToken: "tok", ExpiresAt: base.Add(4 * time.Minute), nowFn: clk}
	if !expiringSoon.IsExpired() || expiringSoon.Valid() {
		t.Fatal("token expiring within 5m buffer should be treated as expired")
	}

	freshInfo := &TokenInfo{AccessToken: "tok", ExpiresAt: base.Add(1 * time.Hour), nowFn: clk}
	if freshInfo.IsExpired() || !freshInfo.Valid() {
		t.Fatal("token expiring in 1 hour should be valid and not expired")
	}
}

func TestTokenInfo_IsExpired_CustomClockAnd5mBuffer(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	tok := &TokenInfo{
		AccessToken: "clock-tok",
		ExpiresAt:   now.Add(10 * time.Minute),
	}
	tok.SetClock(func() time.Time { return now })

	// At t=0: expires in 10m (> 5m buffer) -> valid
	if tok.IsExpired() || !tok.Valid() {
		t.Fatal("expected token valid at t=0 with 10m remaining")
	}

	// Advance clock to t=+4m59s (5m01s remaining > 5m buffer) -> still valid
	now = now.Add(4*time.Minute + 59*time.Second)
	if tok.IsExpired() {
		t.Fatal("expected token not expired with 5m01s remaining")
	}

	// Advance clock by 2s to t=+5m01s (4m59s remaining < 5m buffer) -> expired!
	now = now.Add(2 * time.Second)
	if !tok.IsExpired() || tok.Valid() {
		t.Fatal("expected token expired inside 5m buffer via custom clock")
	}
}

func TestAdversarial_TokenInfo_IsExpired_Boundaries(t *testing.T) {
	base := time.Date(2026, 10, 1, 15, 0, 0, 0, time.UTC)
	clk := func() time.Time { return base }

	emptyTok := &TokenInfo{AccessToken: "", ExpiresAt: base.Add(1 * time.Hour), nowFn: clk}
	if !emptyTok.IsExpired() || emptyTok.Valid() {
		t.Fatal("empty AccessToken must be expired and invalid")
	}

	zeroTok := &TokenInfo{AccessToken: "infinite", ExpiresAt: time.Time{}, nowFn: clk}
	if zeroTok.IsExpired() || !zeroTok.Valid() {
		t.Fatal("zero ExpiresAt with AccessToken must not be expired")
	}

	exact5m := &TokenInfo{AccessToken: "exact", ExpiresAt: base.Add(5 * time.Minute), nowFn: clk}
	if exact5m.IsExpired() {
		t.Fatal("token with exactly 5m0s remaining (not strictly After) should not be expired")
	}

	inside5m := &TokenInfo{AccessToken: "soon", ExpiresAt: base.Add(4*time.Minute + 59*time.Second), nowFn: clk}
	if !inside5m.IsExpired() || inside5m.Valid() {
		t.Fatal("token expiring in 4m59s must be expired under 5m buffer")
	}

	outside5m := &TokenInfo{AccessToken: "fresh", ExpiresAt: base.Add(5*time.Minute + 1*time.Second), nowFn: clk}
	if outside5m.IsExpired() || !outside5m.Valid() {
		t.Fatal("token expiring in 5m01s must be valid")
	}

	customTok := &TokenInfo{AccessToken: "custom", ExpiresAt: base.Add(15 * time.Second), nowFn: clk}
	if customTok.IsExpiredWithBuffer(10 * time.Second) {
		t.Fatal("15s remaining should not expire with 10s buffer")
	}
	if !customTok.IsExpiredWithBuffer(20 * time.Second) {
		t.Fatal("15s remaining must expire with 20s buffer")
	}
	if customTok.IsExpiredWithBuffer(0) {
		t.Fatal("15s remaining should not expire with 0s buffer")
	}
	if customTok.IsExpiredWithBuffer(-30 * time.Second) {
		t.Fatal("future token with negative buffer should not expire")
	}
}

func TestAccountRole_And_TerminalPrompter(t *testing.T) {
	if RolePrimary.Label() != "Primary Account" || RolePrimary.StoreKey("epic") != "epic" {
		t.Fatalf("unexpected primary role mapping")
	}
	if RolePolling.Label() != "Secondary Polling Account" || RolePolling.StoreKey("steam") != "steam_polling" {
		t.Fatalf("unexpected polling role mapping")
	}

	var out bytes.Buffer
	prompter := NewTerminalPrompter(strings.NewReader("  my-code-123  \n"), &out)
	code, err := prompter.PromptForCode(context.Background(), RolePrimary.Label(), "https://auth.example")
	if err != nil {
		t.Fatalf("PromptForCode failed: %v", err)
	}
	if code != "my-code-123" {
		t.Fatalf("expected trimmed code 'my-code-123', got %q", code)
	}
	if !strings.Contains(out.String(), "https://auth.example") {
		t.Fatalf("expected auth URL in prompt banner")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := prompter.PromptForCode(ctx, RolePrimary.Label(), "https://auth.example"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestConfigTokenSaver_And_UpdateBuilders(t *testing.T) {
	noop := NewConfigTokenSaver("")
	if err := noop(config.TokenUpdates{}); err != nil {
		t.Fatalf("expected nil from no-op token saver, got: %v", err)
	}

	epPrim := BuildEpicTokenUpdate(RolePrimary, "rt-prim")
	if epPrim.Primary.EpicRefreshToken == nil || *epPrim.Primary.EpicRefreshToken != "rt-prim" || epPrim.Polling.EpicRefreshToken != nil {
		t.Fatalf("unexpected primary epic update: %+v", epPrim)
	}
	epPoll := BuildEpicTokenUpdate(RolePolling, "rt-poll")
	if epPoll.Polling.EpicRefreshToken == nil || *epPoll.Polling.EpicRefreshToken != "rt-poll" || epPoll.Primary.EpicRefreshToken != nil {
		t.Fatalf("unexpected polling epic update: %+v", epPoll)
	}

	stPrim := BuildSteamTokenUpdate(RolePrimary, "t1", "76561198000000001", "")
	if stPrim.Primary.SteamSessionTicket == nil || *stPrim.Primary.SteamSessionTicket != "t1" ||
		stPrim.Primary.SteamID64 == nil || *stPrim.Primary.SteamID64 != "76561198000000001" ||
		stPrim.Primary.SteamLoginKey != nil {
		t.Fatalf("unexpected primary steam update: %+v", stPrim)
	}

	stPoll := BuildSteamTokenUpdate(RolePolling, "", "", "lk-poll")
	if stPoll.Polling.SteamSessionTicket != nil || stPoll.Polling.SteamID64 != nil ||
		stPoll.Polling.SteamLoginKey == nil || *stPoll.Polling.SteamLoginKey != "lk-poll" {
		t.Fatalf("unexpected polling steam update: %+v", stPoll)
	}
}
