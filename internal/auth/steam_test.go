package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rlapi"
)

func TestSteamAuthProvider_Validate(t *testing.T) {
	p1, _ := NewSteamProvider(config.SteamConfig{SteamID64: "76561198000000000"}, nil)
	if err := p1.Validate(); err == nil || !errors.Is(err, ErrMissingCredentials) {
		t.Fatalf("expected ErrMissingCredentials for empty ticket, got: %v", err)
	}

	p2, _ := NewSteamProvider(config.SteamConfig{SessionTicket: "ticket"}, nil)
	if err := p2.Validate(); err == nil || !errors.Is(err, ErrMissingCredentials) {
		t.Fatalf("expected ErrMissingCredentials for empty steamid64, got: %v", err)
	}

	p3, _ := NewSteamProvider(config.SteamConfig{SessionTicket: "ticket", SteamID64: "12345"}, nil)
	if err := p3.Validate(); err == nil || !errors.Is(err, ErrInvalidSteamID) {
		t.Fatalf("expected ErrInvalidSteamID for short id, got: %v", err)
	}

	p4, _ := NewSteamProvider(config.SteamConfig{SessionTicket: "ticket", SteamID64: "99999999999999999"}, nil)
	if err := p4.Validate(); err == nil || !errors.Is(err, ErrInvalidSteamID) {
		t.Fatalf("expected ErrInvalidSteamID for bad prefix, got: %v", err)
	}

	p5, _ := NewSteamProvider(config.SteamConfig{SessionTicket: "ticket", SteamID64: "76561198012345678"}, nil)
	if err := p5.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestAdversarial_SteamID64_ExhaustiveBoundaries(t *testing.T) {
	validIDs := []string{
		"76561198000000000", "76561197960265728", "76561199999999999",
		"76561190000000000", "76561198012345678",
	}
	for _, id := range validIDs {
		if err := ValidateSteamID64(id); err != nil {
			t.Errorf("expected %q valid, got: %v", id, err)
		}
	}

	invalidCases := []struct{ name, id string }{
		{"empty", ""},
		{"single digit", "7"},
		{"16 digits", "7656119800000000"},
		{"18 digits", "765611980000000000"},
		{"wrong prefix", "76561208000000000"},
		{"wrong prefix 7656118", "76561188000000000"},
		{"embedded letter", "7656119800000000a"},
		{"embedded hex f", "7656119800000000f"},
		{"minus sign", "7656119-123456789"},
		{"leading space", " 7656119800000000"},
		{"trailing space", "7656119800000000 "},
		{"null byte", "7656119\x0000000000"},
		{"unicode digits", "７６５６１１９８０００００００００"},
		{"newline", "7656119\n000000000"},
	}
	for _, tc := range invalidCases {
		if err := ValidateSteamID64(tc.id); err == nil || !errors.Is(err, ErrInvalidSteamID) {
			t.Errorf("[%s] expected ErrInvalidSteamID for %q, got: %v", tc.name, tc.id, err)
		}
	}
}

func TestAdversarial_Steam_Validate_ConfigTrimsSpace(t *testing.T) {
	p1, _ := NewSteamProvider(config.SteamConfig{SessionTicket: "   ", SteamID64: "76561198000000000"}, nil)
	if err := p1.Validate(); err == nil || !errors.Is(err, ErrMissingCredentials) {
		t.Fatalf("expected ErrMissingCredentials, got: %v", err)
	}

	p2, _ := NewSteamProvider(config.SteamConfig{SessionTicket: "valid-ticket", SteamID64: "   "}, nil)
	if err := p2.Validate(); err == nil || !errors.Is(err, ErrMissingCredentials) {
		t.Fatalf("expected ErrMissingCredentials, got: %v", err)
	}

	p3, _ := NewSteamProvider(config.SteamConfig{SessionTicket: "  valid-ticket  ", SteamID64: "  76561198000000000  "}, nil)
	if err := p3.Validate(); err != nil {
		t.Fatalf("expected trimmed config to pass, got: %v", err)
	}
}

func TestSteamAuthProvider_Authenticate_Success(t *testing.T) {
	mock := &mockEGS{
		exchangeEOSTokenFromSteamFunc: func(ticket string) (*rlapi.EOSTokenResponse, error) {
			if ticket != "valid-steam-ticket" {
				return nil, errors.New("bad ticket")
			}
			return &rlapi.EOSTokenResponse{
				AccessToken: "eos-steam-access", RefreshToken: "eos-steam-refresh",
				AccountID: "epic-linked-acc-id", TokenType: "Bearer", ExpiresIn: 3600,
			}, nil
		},
	}

	store := newMockStateStore()
	fixedTime := time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)

	p, err := NewSteamProvider(
		config.SteamConfig{SessionTicket: "valid-steam-ticket", SteamID64: "76561198000000000", AccountName: "SteamGamer"},
		store, WithEGSClient(mock), WithClock(func() time.Time { return fixedTime }),
	)
	if err != nil {
		t.Fatalf("failed to create steam provider: %v", err)
	}

	info, err := p.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("steam authenticate failed: %v", err)
	}
	if info.AccessToken != "eos-steam-access" || info.AccountID != "76561198000000000" ||
		info.EpicAccountID != "epic-linked-acc-id" || info.DisplayName != "SteamGamer" {
		t.Fatalf("unexpected TokenInfo: %+v", info)
	}

	savedToken, savedAcc, savedDisp, err := store.GetAuthState(context.Background(), "steam")
	if err != nil || savedToken != "valid-steam-ticket" || savedAcc != "76561198000000000" || savedDisp != "SteamGamer" {
		t.Fatalf("store state mismatch: (%s, %s, %s, %v)", savedToken, savedAcc, savedDisp, err)
	}
}

func TestSteamAuthProvider_Authenticate_Failures(t *testing.T) {
	mockErr := &mockEGS{
		exchangeEOSTokenFromSteamFunc: func(ticket string) (*rlapi.EOSTokenResponse, error) {
			return nil, errors.New("connection reset by peer")
		},
	}
	p1, _ := NewSteamProvider(config.SteamConfig{SessionTicket: "ticket", SteamID64: "76561198000000000"}, nil, WithEGSClient(mockErr))
	if _, err := p1.Authenticate(context.Background()); err == nil || !errors.Is(err, ErrExchangeFailed) {
		t.Fatalf("expected ErrExchangeFailed, got: %v", err)
	}

	mockNil := &mockEGS{
		exchangeEOSTokenFromSteamFunc: func(ticket string) (*rlapi.EOSTokenResponse, error) { return nil, nil },
	}
	p2, _ := NewSteamProvider(config.SteamConfig{SessionTicket: "ticket", SteamID64: "76561198000000000"}, nil, WithEGSClient(mockNil))
	if _, err := p2.Authenticate(context.Background()); err == nil || !errors.Is(err, ErrExchangeFailed) {
		t.Fatalf("expected ErrExchangeFailed on nil response, got: %v", err)
	}
}

func TestSteamAuthProvider_Refresh(t *testing.T) {
	mockSuccess := &mockEGS{
		refreshEOSTokenFunc: func(rt string) (*rlapi.EOSTokenResponse, error) {
			return &rlapi.EOSTokenResponse{AccessToken: "refreshed-steam-eos", ExpiresIn: 3600}, nil
		},
	}
	p1, _ := NewSteamProvider(config.SteamConfig{SessionTicket: "ticket", SteamID64: "76561198000000000"}, nil, WithEGSClient(mockSuccess))
	refreshed, err := p1.Refresh(context.Background(), "eos-refresh-token")
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if refreshed.AccessToken != "refreshed-steam-eos" {
		t.Fatalf("expected refreshed-steam-eos, got %s", refreshed.AccessToken)
	}

	p2, _ := NewSteamProvider(config.SteamConfig{SessionTicket: "ticket", SteamID64: "76561198000000000"}, nil, WithEGSClient(&mockEGS{}))
	_, err = p2.Refresh(context.Background(), "")
	if err == nil || !errors.Is(err, ErrRefreshFailed) {
		t.Fatalf("expected ErrRefreshFailed, got: %v", err)
	}
	if !strings.Contains(err.Error(), "steam session ticket cannot be automatically renewed, a fresh ticket is required") {
		t.Fatalf("expected informative message, got: %v", err)
	}
}

// TestSteamAuthProvider_Refresh_PreservesEpicAccountID verifies that EpicAccountID established
// during initial Steam->EOS exchange is preserved across subsequent RefreshEOSToken calls (even
// when EOS returns empty AccountID) and ticket-regeneration fallbacks, without mutating cfg.
func TestSteamAuthProvider_Refresh_PreservesEpicAccountID(t *testing.T) {
	now := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	store := newMockStateStore()
	genCount := 0
	gen := &mockSteamGenerator{
		generateFunc: func(ctx context.Context, opts SteamTicketOptions) (*SteamTicketResult, error) {
			genCount++
			return &SteamTicketResult{
				Success:       true,
				SessionTicket: "01000000REGEN",
				SteamID64:     "76561198000000077",
				LoginKey:      "rotated-login-key",
				AccountName:   "SteamAce",
			}, nil
		},
	}

	refreshShouldFail := false
	mock := &mockEGS{
		exchangeEOSTokenFromSteamFunc: func(ticket string) (*rlapi.EOSTokenResponse, error) {
			if ticket == "01000000REGEN" {
				// Simulate EOS exchange returning empty AccountID on subsequent ticket exchange
				return &rlapi.EOSTokenResponse{
					AccessToken: "eos-after-regen", RefreshToken: "eos-rt-3", ExpiresIn: 3600,
				}, nil
			}
			return &rlapi.EOSTokenResponse{
				AccessToken: "eos-initial", RefreshToken: "eos-rt-1", AccountID: "epic-linked-777", ExpiresIn: 3600,
			}, nil
		},
		refreshEOSTokenFunc: func(rt string) (*rlapi.EOSTokenResponse, error) {
			if refreshShouldFail {
				return nil, errors.New("eos refresh expired")
			}
			return &rlapi.EOSTokenResponse{
				AccessToken: "eos-refreshed", RefreshToken: "", AccountID: "", ExpiresIn: 3600,
			}, nil
		},
	}

	origCfg := config.SteamConfig{
		Username: "steam_ace", LoginKey: "initial-login-key",
		SessionTicket: "01000000INIT", SteamID64: "76561198000000077", AccountName: "SteamAce",
	}
	p, err := NewSteamProvider(origCfg, store, WithEGSClient(mock), WithSteamGenerator(gen), WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewSteamProvider failed: %v", err)
	}

	t1, err := p.Authenticate(context.Background())
	if err != nil || t1.EpicAccountID != "epic-linked-777" {
		t.Fatalf("expected initial EpicAccountID 'epic-linked-777', got %+v (err=%v)", t1, err)
	}

	// 1. RefreshEOSToken returns empty AccountID and empty RefreshToken -> preserves both!
	t2, err := p.Refresh(context.Background(), "")
	if err != nil {
		t.Fatalf("refresh 1 failed: %v", err)
	}
	if t2.EpicAccountID != "epic-linked-777" || t2.RefreshToken != "eos-rt-1" || t2.AccountID != "76561198000000077" {
		t.Fatalf("EpicAccountID/RefreshToken lost on EOS refresh: %+v", t2)
	}

	// 2. RefreshEOSToken fails -> ticket regeneration fallback preserves EpicAccountID and updates internal loginKey only.
	refreshShouldFail = true
	t3, err := p.Refresh(context.Background(), "")
	if err != nil {
		t.Fatalf("refresh 2 (ticket regen) failed: %v", err)
	}
	if genCount != 1 || t3.EpicAccountID != "epic-linked-777" || t3.AccessToken != "eos-after-regen" {
		t.Fatalf("unexpected state after regen: gen=%d info=%+v", genCount, t3)
	}
	if p.cfg.LoginKey != "initial-login-key" {
		t.Fatalf("p.cfg.LoginKey must remain immutable, got %q", p.cfg.LoginKey)
	}
	savedTicket, savedSID, savedName, err := store.GetAuthState(context.Background(), "steam")
	if err != nil || savedTicket != "01000000REGEN" || savedSID != "76561198000000077" || savedName != "SteamAce" {
		t.Fatalf("store state mismatch after regen: (%q, %q, %q, %v)", savedTicket, savedSID, savedName, err)
	}
}

func TestSteam_AutomatedGeneration_And_SilentReauth(t *testing.T) {
	t.Run("automated generation on missing ticket", func(t *testing.T) {
		gen := &mockSteamGenerator{
			returnResult: &SteamTicketResult{
				Success: true, SessionTicket: "01000000AABBCCDD",
				SteamID64: "76561198000000001", LoginKey: "persisted-login-key-1", AccountName: "SteamGamer",
			},
		}
		var savedUpdates []config.TokenUpdates
		store := newMockStateStore()
		mock := &mockEGS{
			exchangeEOSTokenFromSteamFunc: func(ticket string) (*rlapi.EOSTokenResponse, error) {
				if ticket != "01000000AABBCCDD" {
					return nil, errors.New("wrong ticket")
				}
				return &rlapi.EOSTokenResponse{AccessToken: "eos-steam-access", RefreshToken: "eos-rt", AccountID: "epic-linked", ExpiresIn: 3600}, nil
			},
		}

		p, _ := NewSteamProvider(
			config.SteamConfig{Username: "gamer_john", Password: "secret"}, store,
			WithEGSClient(mock), WithSteamGenerator(gen),
			WithConfigTokenSaver(func(u config.TokenUpdates) error { savedUpdates = append(savedUpdates, u); return nil }),
			WithAccountRole(RolePrimary),
		)
		if err := p.Validate(); err != nil {
			t.Fatalf("Validate failed: %v", err)
		}
		info, err := p.Authenticate(context.Background())
		if err != nil || info.AccessToken != "eos-steam-access" {
			t.Fatalf("Authenticate failed: info=%+v err=%v", info, err)
		}
		if len(gen.calls) != 1 || gen.calls[0].AccountLabel != "Primary Account" {
			t.Fatalf("unexpected generator calls: %+v", gen.calls)
		}
		if len(savedUpdates) != 1 || *savedUpdates[0].Primary.SteamLoginKey != "persisted-login-key-1" {
			t.Fatalf("expected Primary Steam updates saved: %+v", savedUpdates)
		}
	})

	t.Run("silent background reauth using loginKey", func(t *testing.T) {
		gen := &mockSteamGenerator{
			returnResult: &SteamTicketResult{
				Success: true, SessionTicket: "01000000RENEWEDTICKET",
				SteamID64: "76561198000000002", LoginKey: "updated-rotated-login-key", AccountName: "SilentGamer",
			},
		}
		var savedUpdates []config.TokenUpdates
		mock := &mockEGS{
			refreshEOSTokenFunc: func(rt string) (*rlapi.EOSTokenResponse, error) { return nil, errors.New("HTTP 401") },
			exchangeEOSTokenFromSteamFunc: func(ticket string) (*rlapi.EOSTokenResponse, error) {
				if ticket != "01000000RENEWEDTICKET" {
					return nil, errors.New("invalid renewed ticket")
				}
				return &rlapi.EOSTokenResponse{AccessToken: "fresh-eos-access", RefreshToken: "fresh-rt", AccountID: "epic-id", ExpiresIn: 3600}, nil
			},
		}

		p, _ := NewSteamProvider(
			config.SteamConfig{Username: "silent_user", LoginKey: "cached-login-key-123", SteamID64: "76561198000000002", SessionTicket: "01000000OLDTICKET"},
			nil, WithEGSClient(mock), WithSteamGenerator(gen),
			WithConfigTokenSaver(func(u config.TokenUpdates) error { savedUpdates = append(savedUpdates, u); return nil }),
			WithAccountRole(RolePolling),
		)
		info, err := p.Refresh(context.Background(), "expired-eos-refresh-token")
		if err != nil || info.AccessToken != "fresh-eos-access" {
			t.Fatalf("Refresh failed: info=%+v err=%v", info, err)
		}
		if len(gen.calls) != 1 || gen.calls[0].LoginKey != "cached-login-key-123" || gen.calls[0].AccountLabel != "Secondary Polling Account" {
			t.Fatalf("unexpected generator call: %+v", gen.calls)
		}
		if len(savedUpdates) != 1 || *savedUpdates[0].Polling.SteamLoginKey != "updated-rotated-login-key" {
			t.Fatalf("expected Polling loginKey saved: %+v", savedUpdates)
		}
	})
}

func TestNodeSteamTicketGenerator_Execution_ResolvesScriptPath(t *testing.T) {
	gen := NewNodeSteamTicketGenerator("")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := gen.GenerateTicket(ctx, SteamTicketOptions{
		Username: "test_nonexistent_user", Password: "test_invalid_pass", AccountLabel: "Primary Account",
	})
	if err == nil {
		t.Fatal("expected error with dummy credentials, got nil")
	}
	if strings.Contains(err.Error(), "Cannot find module") || strings.Contains(err.Error(), "not found") {
		t.Fatalf("script resolution failed: %v", err)
	}
}

func TestNodeSteamTicketGenerator_RequiresExplicitUsername(t *testing.T) {
	t.Setenv("STEAM_USERNAME", "")
	t.Setenv("USERNAME", "WindowsOSUserShouldBeIgnored")

	gen := NewNodeSteamTicketGenerator("")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := gen.GenerateTicket(ctx, SteamTicketOptions{
		Password: "some-password",
	})
	if err == nil {
		t.Fatal("expected error when Steam username is unset")
	}
	if !strings.Contains(err.Error(), "Steam username is required") {
		t.Fatalf("expected missing username error (not Windows USERNAME fallback), got: %v", err)
	}
}
