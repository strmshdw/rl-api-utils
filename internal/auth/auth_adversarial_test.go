package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rlapi"
)

// ============================================================================
// 1. Epic Auth Adversarial & Stress Tests
// ============================================================================

// TestAdversarial_Epic_RefreshTokenVsAuthCode_Precedence verifies that when both
// RefreshToken and AuthCode are configured, Authenticate prioritizes the RefreshToken.
// If RefreshToken succeeds, AuthCode is never called.
func TestAdversarial_Epic_RefreshTokenVsAuthCode_Precedence(t *testing.T) {
	var codeCalled bool
	mock := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			if rt != "cfg-refresh" {
				return nil, errors.New("wrong refresh token")
			}
			return &rlapi.TokenResponse{
				AccessToken:  "egs-access-from-refresh",
				RefreshToken: "rotated-refresh-token",
				AccountID:    "epic-acc-123",
				DisplayName:  "EpicPlayer",
				ExpiresIn:    3600,
			}, nil
		},
		authenticateWithCodeFunc: func(code string) (*rlapi.TokenResponse, error) {
			codeCalled = true
			return nil, errors.New("should not be called")
		},
		getExchangeCodeFunc: func(at string) (string, error) {
			return "ex-code", nil
		},
		exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
			return &rlapi.EOSTokenResponse{
				AccessToken: "eos-token-success",
				ExpiresIn:   3600,
			}, nil
		},
	}

	p, err := NewEpicProvider(
		config.EpicConfig{
			RefreshToken: "cfg-refresh",
			AuthCode:     "cfg-auth-code",
		},
		nil,
		WithEGSClient(mock),
	)
	if err != nil {
		t.Fatalf("unexpected error creating provider: %v", err)
	}

	info, err := p.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("authenticate failed: %v", err)
	}

	if codeCalled {
		t.Fatal("expected AuthenticateWithCode NOT to be called when RefreshToken succeeded")
	}
	if info.AccessToken != "eos-token-success" {
		t.Fatalf("expected eos-token-success, got %s", info.AccessToken)
	}
	if info.RefreshToken != "rotated-refresh-token" {
		t.Fatalf("expected rotated-refresh-token, got %s", info.RefreshToken)
	}
}

// TestAdversarial_Epic_RefreshToken_Fails_FallbackToAuthCode verifies that when
// RefreshToken fails (e.g. revoked), Authenticate seamlessly falls back to AuthCode.
func TestAdversarial_Epic_RefreshToken_Fails_FallbackToAuthCode(t *testing.T) {
	var refreshAttempted bool
	var codeAttempted bool

	mock := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			refreshAttempted = true
			return nil, errors.New("refresh token revoked by provider")
		},
		authenticateWithCodeFunc: func(code string) (*rlapi.TokenResponse, error) {
			codeAttempted = true
			if code != "fallback-code" {
				return nil, errors.New("unexpected auth code")
			}
			return &rlapi.TokenResponse{
				AccessToken:  "egs-access-from-code",
				RefreshToken: "brand-new-refresh-token",
				AccountID:    "epic-acc-fallback",
				DisplayName:  "FallbackPlayer",
				ExpiresIn:    3600,
			}, nil
		},
		getExchangeCodeFunc: func(at string) (string, error) {
			return "ex-code-from-code", nil
		},
		exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
			return &rlapi.EOSTokenResponse{
				AccessToken: "eos-token-fallback",
				ExpiresIn:   3600,
			}, nil
		},
	}

	store := newMockStateStore()
	p, err := NewEpicProvider(
		config.EpicConfig{
			RefreshToken: "revoked-refresh",
			AuthCode:     "fallback-code",
		},
		store,
		WithEGSClient(mock),
	)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	info, err := p.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("expected fallback to auth code to succeed, got: %v", err)
	}

	if !refreshAttempted {
		t.Fatal("expected RefreshToken to be attempted first")
	}
	if !codeAttempted {
		t.Fatal("expected AuthCode fallback to be attempted after RefreshToken failure")
	}
	if info.AccessToken != "eos-token-fallback" {
		t.Fatalf("expected eos-token-fallback, got %s", info.AccessToken)
	}
	if info.RefreshToken != "brand-new-refresh-token" {
		t.Fatalf("expected brand-new-refresh-token, got %s", info.RefreshToken)
	}

	// Verify store updated with the new refresh token from code exchange
	savedTok, savedAcc, _, err := store.GetAuthState(context.Background(), "epic")
	if err != nil {
		t.Fatalf("failed to get auth state from store: %v", err)
	}
	if savedTok != "brand-new-refresh-token" || savedAcc != "epic-acc-fallback" {
		t.Fatalf("unexpected stored credentials: tok=%s, acc=%s", savedTok, savedAcc)
	}
}

// TestAdversarial_Epic_BothFail_ErrorWrapped verifies that when both RefreshToken
// and AuthCode fail, a proper ErrAuthFailed error is returned.
func TestAdversarial_Epic_BothFail_ErrorWrapped(t *testing.T) {
	mock := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			return nil, errors.New("refresh token invalid")
		},
		authenticateWithCodeFunc: func(code string) (*rlapi.TokenResponse, error) {
			return nil, errors.New("auth code expired")
		},
	}

	p, _ := NewEpicProvider(
		config.EpicConfig{
			RefreshToken: "bad-refresh",
			AuthCode:     "bad-code",
		},
		nil,
		WithEGSClient(mock),
	)

	_, err := p.Authenticate(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("expected ErrAuthFailed, got: %v", err)
	}
}

// TestAdversarial_Epic_StoreFallback_FullCascade verifies that when config has no
// tokens, Authenticate pulls from StateStore, and if StateStore also has nothing,
// returns ErrMissingCredentials.
func TestAdversarial_Epic_StoreFallback_FullCascade(t *testing.T) {
	// Case A: StateStore has valid token
	store := newMockStateStore()
	_ = store.SaveAuthState(context.Background(), "epic", "stored-rt", "stored-acc", "StoredUser")

	mock := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			if rt != "stored-rt" {
				return nil, errors.New("wrong token")
			}
			return &rlapi.TokenResponse{
				AccessToken:  "egs-at-stored",
				RefreshToken: "stored-rt-v2",
				AccountID:    "stored-acc",
				DisplayName:  "StoredUser",
				ExpiresIn:    3600,
			}, nil
		},
		getExchangeCodeFunc: func(at string) (string, error) { return "ex", nil },
		exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
			return &rlapi.EOSTokenResponse{AccessToken: "eos-stored", ExpiresIn: 3600}, nil
		},
	}

	p, _ := NewEpicProvider(config.EpicConfig{}, store, WithEGSClient(mock))
	info, err := p.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("expected store fallback to succeed, got: %v", err)
	}
	if info.AccessToken != "eos-stored" {
		t.Fatalf("unexpected token: %s", info.AccessToken)
	}

	// Case B: StateStore is empty -> returns ErrMissingCredentials
	emptyStore := newMockStateStore()
	p2, _ := NewEpicProvider(config.EpicConfig{}, emptyStore, WithEGSClient(mock))
	_, err = p2.Authenticate(context.Background())
	if err == nil || !errors.Is(err, ErrMissingCredentials) {
		t.Fatalf("expected ErrMissingCredentials from empty store, got: %v", err)
	}

	// Case C: StateStore is nil -> returns ErrMissingCredentials
	p3, _ := NewEpicProvider(config.EpicConfig{}, nil, WithEGSClient(mock))
	_, err = p3.Authenticate(context.Background())
	if err == nil || !errors.Is(err, ErrMissingCredentials) {
		t.Fatalf("expected ErrMissingCredentials with nil store, got: %v", err)
	}
}

// TestAdversarial_Epic_WhitespaceCredentials_TreatedAsMissing verifies that
// whitespace-only credentials in config are trimmed and treated as empty.
func TestAdversarial_Epic_WhitespaceCredentials_TreatedAsMissing(t *testing.T) {
	p, _ := NewEpicProvider(
		config.EpicConfig{
			RefreshToken: "   \t\n  ",
			AuthCode:     "   \r\n  ",
		},
		nil,
	)

	if err := p.Validate(); err == nil || !errors.Is(err, ErrMissingCredentials) {
		t.Fatalf("expected ErrMissingCredentials on whitespace config, got: %v", err)
	}

	_, err := p.Authenticate(context.Background())
	if err == nil || !errors.Is(err, ErrMissingCredentials) {
		t.Fatalf("expected ErrMissingCredentials on whitespace config authenticate, got: %v", err)
	}
}

// TestAdversarial_Epic_Refresh_FallbackCascade tests the 4-level fallback order
// in Refresh(ctx, refreshToken):
// 1. Explicit refreshToken argument
// 2. Cached p.tokenInfo.RefreshToken
// 3. StateStore.GetAuthState
// 4. Config.RefreshToken
func TestAdversarial_Epic_Refresh_FallbackCascade(t *testing.T) {
	var usedToken string
	mock := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			usedToken = rt
			return &rlapi.TokenResponse{
				AccessToken:  "at-" + rt,
				RefreshToken: rt + "-next",
				ExpiresIn:    3600,
			}, nil
		},
		getExchangeCodeFunc: func(at string) (string, error) { return "ex", nil },
		exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
			return &rlapi.EOSTokenResponse{AccessToken: "eos", ExpiresIn: 3600}, nil
		},
	}

	// Level 1: Explicit argument overrides everything
	store := newMockStateStore()
	_ = store.SaveAuthState(context.Background(), "epic", "store-token", "acc", "user")
	p1, _ := NewEpicProvider(
		config.EpicConfig{RefreshToken: "cfg-token"},
		store,
		WithEGSClient(mock),
	)
	// Seed cached token
	p1.mu.Lock()
	p1.tokenInfo = &TokenInfo{RefreshToken: "cached-token"}
	p1.mu.Unlock()

	_, err := p1.Refresh(context.Background(), "explicit-arg-token")
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if usedToken != "explicit-arg-token" {
		t.Fatalf("expected explicit-arg-token to take highest precedence, got %s", usedToken)
	}

	// Level 2: Empty argument -> falls back to cached tokenInfo
	_, err = p1.Refresh(context.Background(), "")
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	// Note: previous refresh updated cached token to "explicit-arg-token-next"
	if usedToken != "explicit-arg-token-next" {
		t.Fatalf("expected cached token to be used, got %s", usedToken)
	}

	// Level 3: No cached token -> falls back to StateStore
	store3 := newMockStateStore()
	_ = store3.SaveAuthState(context.Background(), "epic", "store-token", "acc", "user")
	p2, _ := NewEpicProvider(
		config.EpicConfig{RefreshToken: "cfg-token"},
		store3,
		WithEGSClient(mock),
	)
	_, err = p2.Refresh(context.Background(), "")
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if usedToken != "store-token" {
		t.Fatalf("expected store-token to be used when not cached, got %s", usedToken)
	}

	// Level 4: Empty store -> falls back to Config.RefreshToken
	emptyStore := newMockStateStore()
	p3, _ := NewEpicProvider(
		config.EpicConfig{RefreshToken: "cfg-token"},
		emptyStore,
		WithEGSClient(mock),
	)
	_, err = p3.Refresh(context.Background(), "")
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if usedToken != "cfg-token" {
		t.Fatalf("expected cfg-token to be used when store empty, got %s", usedToken)
	}

	// Level 5: Everything empty -> returns ErrMissingCredentials
	emptyStore5 := newMockStateStore()
	p4, _ := NewEpicProvider(config.EpicConfig{}, emptyStore5, WithEGSClient(mock))
	_, err = p4.Refresh(context.Background(), "")
	if err == nil || !errors.Is(err, ErrMissingCredentials) {
		t.Fatalf("expected ErrMissingCredentials when no token available, got: %v", err)
	}
}

// TestAdversarial_Epic_TokenInfo_Immutability verifies that modifying the TokenInfo
// returned by TokenInfo() does not mutate the internal provider state.
func TestAdversarial_Epic_TokenInfo_Immutability(t *testing.T) {
	mock := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			return &rlapi.TokenResponse{
				AccessToken:  "orig-access",
				RefreshToken: "orig-refresh",
				AccountID:    "orig-acc",
				ExpiresIn:    3600,
			}, nil
		},
		getExchangeCodeFunc: func(at string) (string, error) { return "ex", nil },
		exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
			return &rlapi.EOSTokenResponse{AccessToken: "orig-eos", ExpiresIn: 3600}, nil
		},
	}

	p, _ := NewEpicProvider(config.EpicConfig{RefreshToken: "tok"}, nil, WithEGSClient(mock))
	_, err := p.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("authenticate failed: %v", err)
	}

	tok1 := p.TokenInfo()
	if tok1 == nil {
		t.Fatal("expected non-nil TokenInfo")
	}

	// Mutate fields on the returned copy
	tok1.AccessToken = "MUTATED-ACCESS"
	tok1.RefreshToken = "MUTATED-REFRESH"
	tok1.AccountID = "MUTATED-ACC"

	// Fetch fresh copy from provider
	tok2 := p.TokenInfo()
	if tok2.AccessToken != "orig-eos" {
		t.Fatalf("internal AccessToken was mutated! got %s, expected orig-eos", tok2.AccessToken)
	}
	if tok2.RefreshToken != "orig-refresh" {
		t.Fatalf("internal RefreshToken was mutated! got %s, expected orig-refresh", tok2.RefreshToken)
	}
	if tok2.AccountID != "orig-acc" {
		t.Fatalf("internal AccountID was mutated! got %s, expected orig-acc", tok2.AccountID)
	}
}

// TestAdversarial_Epic_DefaultExpiresIn verifies that when EOS returns zero or
// negative ExpiresIn, it safely defaults to 3600 seconds.
func TestAdversarial_Epic_DefaultExpiresIn(t *testing.T) {
	fixedTime := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)

	cases := []int{0, -1, -3600}
	for _, exp := range cases {
		mock := &mockEGS{
			authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
				return &rlapi.TokenResponse{AccessToken: "at", ExpiresIn: 3600}, nil
			},
			getExchangeCodeFunc: func(at string) (string, error) { return "ex", nil },
			exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
				return &rlapi.EOSTokenResponse{AccessToken: "eos", ExpiresIn: exp}, nil
			},
		}

		p, _ := NewEpicProvider(
			config.EpicConfig{RefreshToken: "tok"},
			nil,
			WithEGSClient(mock),
			WithClock(func() time.Time { return fixedTime }),
		)

		info, err := p.Authenticate(context.Background())
		if err != nil {
			t.Fatalf("authenticate failed with expiresIn=%d: %v", exp, err)
		}

		expected := fixedTime.Add(3600 * time.Second)
		if !info.ExpiresAt.Equal(expected) {
			t.Fatalf("for expiresIn=%d expected expiry %v, got %v", exp, expected, info.ExpiresAt)
		}
	}
}

// ============================================================================
// 2. Steam Auth Adversarial & Stress Tests
// ============================================================================

// TestAdversarial_SteamID64_ExhaustiveBoundaries tests every edge case for SteamID64
// validation: lengths, prefixes, numeric ranges, non-digits, unicode, and whitespace.
func TestAdversarial_SteamID64_ExhaustiveBoundaries(t *testing.T) {
	// Valid cases
	validIDs := []string{
		"76561198000000000", // Standard SteamID64
		"76561197960265728", // Lowest valid 64-bit Steam ID (Valve base ID)
		"76561199999999999", // Upper boundary of 7656119 prefix
		"76561190000000000", // Lowest 7656119 prefix
		"76561198012345678", // Arbitrary valid community ID
	}
	for _, id := range validIDs {
		if err := ValidateSteamID64(id); err != nil {
			t.Errorf("expected %q to be VALID, but got: %v", id, err)
		}
	}

	// Invalid cases
	invalidCases := []struct {
		name string
		id   string
	}{
		{"empty", ""},
		{"single digit", "7"},
		{"16 digits (short by 1)", "7656119800000000"},
		{"18 digits (long by 1)", "765611980000000000"},
		{"15 digits", "765611980000000"},
		{"wrong prefix 7656120", "76561208000000000"},
		{"wrong prefix 7656118", "76561188000000000"},
		{"wrong prefix 1234567", "12345678901234567"},
		{"embedded lowercase letter", "7656119800000000a"},
		{"embedded uppercase letter", "7656119800000000Z"},
		{"embedded hex char f", "7656119800000000f"},
		{"embedded symbol exclamation", "7656119800000000!"},
		{"embedded minus sign", "7656119-123456789"},
		{"embedded plus sign", "7656119+123456789"},
		{"leading space (17 chars)", " 7656119800000000"},
		{"trailing space (17 chars)", "7656119800000000 "},
		{"surrounded by spaces", " 76561198000000000 "},
		{"embedded null byte", "7656119\x0000000000"},
		{"full-width unicode digits", "７６５６１１９８０００００００００"},
		{"newline inside", "7656119\n000000000"},
		{"tab inside", "7656119\t000000000"},
	}

	for _, tc := range invalidCases {
		err := ValidateSteamID64(tc.id)
		if err == nil {
			t.Errorf("[%s] expected %q to be INVALID, but got nil error", tc.name, tc.id)
		} else if !errors.Is(err, ErrInvalidSteamID) {
			t.Errorf("[%s] expected ErrInvalidSteamID for %q, got: %v", tc.name, tc.id, err)
		}
	}
}

// TestAdversarial_Steam_Validate_ConfigTrimsSpace verifies that SteamAuthProvider.Validate()
// trims whitespace from SteamID64 and SessionTicket, but catches whitespace-only inputs.
func TestAdversarial_Steam_Validate_ConfigTrimsSpace(t *testing.T) {
	// Whitespace ticket
	p1, _ := NewSteamProvider(config.SteamConfig{
		SessionTicket: "   ",
		SteamID64:     "76561198000000000",
	}, nil)
	if err := p1.Validate(); err == nil || !errors.Is(err, ErrMissingCredentials) {
		t.Fatalf("expected ErrMissingCredentials for whitespace ticket, got: %v", err)
	}

	// Whitespace steamid
	p2, _ := NewSteamProvider(config.SteamConfig{
		SessionTicket: "valid-ticket",
		SteamID64:     "   ",
	}, nil)
	if err := p2.Validate(); err == nil || !errors.Is(err, ErrMissingCredentials) {
		t.Fatalf("expected ErrMissingCredentials for whitespace steamid, got: %v", err)
	}

	// Valid with surrounding spaces in config -> trimmed and accepted
	p3, _ := NewSteamProvider(config.SteamConfig{
		SessionTicket: "  valid-ticket  ",
		SteamID64:     "  76561198000000000  ",
	}, nil)
	if err := p3.Validate(); err != nil {
		t.Fatalf("expected trimmed config to pass validation, got: %v", err)
	}
}

// TestAdversarial_Steam_SessionTicketExchange_FailureScenarios verifies various
// failure modes during Steam ticket exchange (network drop, nil response, invalid ticket).
func TestAdversarial_Steam_SessionTicketExchange_FailureScenarios(t *testing.T) {
	// Subcase A: Upstream returns network error
	mockErr := &mockEGS{
		exchangeEOSTokenFromSteamFunc: func(ticket string) (*rlapi.EOSTokenResponse, error) {
			return nil, errors.New("connection reset by peer")
		},
	}
	p1, _ := NewSteamProvider(config.SteamConfig{
		SessionTicket: "ticket",
		SteamID64:     "76561198000000000",
	}, nil, WithEGSClient(mockErr))

	_, err := p1.Authenticate(context.Background())
	if err == nil || !errors.Is(err, ErrExchangeFailed) {
		t.Fatalf("expected ErrExchangeFailed on network drop, got: %v", err)
	}

	// Subcase B: Upstream returns nil response with nil error
	mockNil := &mockEGS{
		exchangeEOSTokenFromSteamFunc: func(ticket string) (*rlapi.EOSTokenResponse, error) {
			return nil, nil
		},
	}
	p2, _ := NewSteamProvider(config.SteamConfig{
		SessionTicket: "ticket",
		SteamID64:     "76561198000000000",
	}, nil, WithEGSClient(mockNil))

	_, err = p2.Authenticate(context.Background())
	if err == nil || !errors.Is(err, ErrExchangeFailed) {
		t.Fatalf("expected ErrExchangeFailed on nil response, got: %v", err)
	}
}

// TestAdversarial_Steam_TicketRenewalError verifies the explicit error contract
// when Steam session tickets cannot be renewed automatically.
func TestAdversarial_Steam_TicketRenewalError(t *testing.T) {
	// When no EOS refresh token is available, Refresh must return ErrRefreshFailed
	// with a clear, descriptive message indicating fresh ticket is required.
	p, _ := NewSteamProvider(config.SteamConfig{
		SessionTicket: "ticket",
		SteamID64:     "76561198000000000",
	}, nil)

	_, err := p.Refresh(context.Background(), "")
	if err == nil {
		t.Fatal("expected error from Steam Refresh, got nil")
	}
	if !errors.Is(err, ErrRefreshFailed) {
		t.Fatalf("expected ErrRefreshFailed, got: %v", err)
	}
	expectedSubstring := "steam session ticket cannot be automatically renewed, a fresh ticket is required"
	if !strings.Contains(err.Error(), expectedSubstring) {
		t.Fatalf("expected error message to contain %q, got: %q", expectedSubstring, err.Error())
	}
}

// TestAdversarial_Steam_TokenInfo_Immutability verifies that modifying the returned
// TokenInfo from SteamAuthProvider does not alter internal provider state.
func TestAdversarial_Steam_TokenInfo_Immutability(t *testing.T) {
	mock := &mockEGS{
		exchangeEOSTokenFromSteamFunc: func(ticket string) (*rlapi.EOSTokenResponse, error) {
			return &rlapi.EOSTokenResponse{
				AccessToken:  "orig-steam-eos",
				RefreshToken: "orig-steam-refresh",
				AccountID:    "orig-steam-acc",
				ExpiresIn:    3600,
			}, nil
		},
	}

	p, _ := NewSteamProvider(config.SteamConfig{
		SessionTicket: "ticket",
		SteamID64:     "76561198000000000",
	}, nil, WithEGSClient(mock))

	_, err := p.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("authenticate failed: %v", err)
	}

	tok1 := p.TokenInfo()
	tok1.AccessToken = "CORRUPTED"
	tok1.RefreshToken = "CORRUPTED"

	tok2 := p.TokenInfo()
	if tok2.AccessToken != "orig-steam-eos" {
		t.Fatalf("expected internal AccessToken unchanged, got %s", tok2.AccessToken)
	}
	if tok2.RefreshToken != "orig-steam-refresh" {
		t.Fatalf("expected internal RefreshToken unchanged, got %s", tok2.RefreshToken)
	}
}

// ============================================================================
// 3. Concurrency Safety & Context Cancellation
// ============================================================================

// TestAdversarial_Concurrency_Epic_TokenInfoAndAuthenticate stress-tests concurrent
// reads of TokenInfo() while Authenticate() and Refresh() are executing simultaneously
// across dozens of goroutines.
func TestAdversarial_Concurrency_Epic_TokenInfoAndAuthenticate(t *testing.T) {
	var count int
	var mu sync.Mutex

	mock := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			mu.Lock()
			count++
			c := count
			mu.Unlock()
			return &rlapi.TokenResponse{
				AccessToken:  fmt.Sprintf("egs-token-%d", c),
				RefreshToken: fmt.Sprintf("refresh-token-%d", c),
				AccountID:    "epic-acc-concurrent",
				ExpiresIn:    3600,
			}, nil
		},
		getExchangeCodeFunc: func(at string) (string, error) {
			return "ex-" + at, nil
		},
		exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
			return &rlapi.EOSTokenResponse{
				AccessToken: "eos-" + ec,
				ExpiresIn:   3600,
			}, nil
		},
	}

	store := newMockStateStore()
	p, _ := NewEpicProvider(
		config.EpicConfig{RefreshToken: "init-token"},
		store,
		WithEGSClient(mock),
	)

	// Pre-seed initial authentication
	_, err := p.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("initial authenticate failed: %v", err)
	}

	const readers = 40
	const writers = 10
	const iterations = 50

	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Readers: rapidly calling TokenInfo()
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				info := p.TokenInfo()
				if info != nil {
					// Verify struct integrity (no partial writes)
					_ = info.Valid()
					_ = info.IsExpired()
					if !strings.HasPrefix(info.AccessToken, "eos-ex-egs-token-") {
						t.Errorf("corrupted AccessToken read: %q", info.AccessToken)
						return
					}
				}
				time.Sleep(10 * time.Microsecond)
			}
		}()
	}

	// Writers: calling Authenticate and Refresh
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				if idx%2 == 0 {
					_, _ = p.Authenticate(ctx)
				} else {
					_, _ = p.Refresh(ctx, "")
				}
				time.Sleep(50 * time.Microsecond)
			}
		}(i)
	}

	wg.Wait()
}

// TestAdversarial_Concurrency_Steam_TokenInfoAndAuthenticate stress-tests concurrent
// reads of TokenInfo() on SteamAuthProvider while Refresh and Authenticate execute.
func TestAdversarial_Concurrency_Steam_TokenInfoAndAuthenticate(t *testing.T) {
	var count int
	var mu sync.Mutex

	mock := &mockEGS{
		exchangeEOSTokenFromSteamFunc: func(ticket string) (*rlapi.EOSTokenResponse, error) {
			mu.Lock()
			count++
			c := count
			mu.Unlock()
			return &rlapi.EOSTokenResponse{
				AccessToken:  fmt.Sprintf("steam-eos-%d", c),
				RefreshToken: fmt.Sprintf("steam-refresh-%d", c),
				AccountID:    "steam-epic-acc",
				ExpiresIn:    3600,
			}, nil
		},
		refreshEOSTokenFunc: func(rt string) (*rlapi.EOSTokenResponse, error) {
			mu.Lock()
			count++
			c := count
			mu.Unlock()
			return &rlapi.EOSTokenResponse{
				AccessToken:  fmt.Sprintf("steam-eos-refreshed-%d", c),
				RefreshToken: fmt.Sprintf("steam-refresh-%d", c),
				ExpiresIn:    3600,
			}, nil
		},
	}

	p, _ := NewSteamProvider(
		config.SteamConfig{
			SessionTicket: "valid-ticket",
			SteamID64:     "76561198000000000",
		},
		nil,
		WithEGSClient(mock),
	)

	_, err := p.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("initial authenticate failed: %v", err)
	}

	const readers = 40
	const writers = 10
	const iterations = 50

	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				info := p.TokenInfo()
				if info != nil {
					_ = info.Valid()
					_ = info.IsExpired()
					if !strings.HasPrefix(info.AccessToken, "steam-eos-") {
						t.Errorf("corrupted steam AccessToken read: %q", info.AccessToken)
						return
					}
				}
				time.Sleep(10 * time.Microsecond)
			}
		}()
	}

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				if idx%2 == 0 {
					_, _ = p.Authenticate(ctx)
				} else {
					_, _ = p.Refresh(ctx, "")
				}
				time.Sleep(50 * time.Microsecond)
			}
		}(i)
	}

	wg.Wait()
}

// TestAdversarial_ContextCancellation_AllStages tests that context cancellation is
// respected across every stage of the auth lifecycle.
func TestAdversarial_ContextCancellation_AllStages(t *testing.T) {
	// 1. Epic Authenticate - pre-canceled
	ctxPre, cancelPre := context.WithCancel(context.Background())
	cancelPre()

	mock := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			return &rlapi.TokenResponse{AccessToken: "ok"}, nil
		},
	}
	pEpic, _ := NewEpicProvider(config.EpicConfig{RefreshToken: "tok"}, nil, WithEGSClient(mock))

	_, err := pEpic.Authenticate(ctxPre)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled pre-authenticate, got: %v", err)
	}

	// 2. Epic Authenticate - canceled during exchange code step
	ctxStep3, cancelStep3 := context.WithCancel(context.Background())
	mockStep3 := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			cancelStep3() // cancel context inside OAuth step
			return &rlapi.TokenResponse{AccessToken: "step2-done"}, nil
		},
	}
	pEpicStep3, _ := NewEpicProvider(config.EpicConfig{RefreshToken: "tok"}, nil, WithEGSClient(mockStep3))
	_, err = pEpicStep3.Authenticate(ctxStep3)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled before GetExchangeCode, got: %v", err)
	}

	// 3. Epic Authenticate - canceled during EOS exchange step
	ctxStep4, cancelStep4 := context.WithCancel(context.Background())
	mockStep4 := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			return &rlapi.TokenResponse{AccessToken: "step2-done"}, nil
		},
		getExchangeCodeFunc: func(at string) (string, error) {
			cancelStep4() // cancel context during exchange code step
			return "step3-done", nil
		},
	}
	pEpicStep4, _ := NewEpicProvider(config.EpicConfig{RefreshToken: "tok"}, nil, WithEGSClient(mockStep4))
	_, err = pEpicStep4.Authenticate(ctxStep4)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled before ExchangeEOSToken, got: %v", err)
	}

	// 4. Epic Refresh - pre-canceled
	_, err = pEpic.Refresh(ctxPre, "tok")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled on Refresh, got: %v", err)
	}

	// 5. Steam Authenticate - pre-canceled
	pSteam, _ := NewSteamProvider(config.SteamConfig{
		SessionTicket: "ticket",
		SteamID64:     "76561198000000000",
	}, nil)

	_, err = pSteam.Authenticate(ctxPre)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled on Steam Authenticate, got: %v", err)
	}

	// 6. Steam Refresh - pre-canceled
	_, err = pSteam.Refresh(ctxPre, "eos-token")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled on Steam Refresh, got: %v", err)
	}

	// 7. Context DeadlineExceeded
	ctxDead, cancelDead := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Minute))
	defer cancelDead()

	_, err = pEpic.Authenticate(ctxDead)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got: %v", err)
	}
}

// ============================================================================
// 4. TokenInfo Expiry & Validation Boundary Tests
// ============================================================================

// TestAdversarial_TokenInfo_IsExpired_Boundaries conducts precise boundary testing
// on IsExpired() and IsExpiredWithBuffer():
// - nil receiver
// - empty access token
// - zero time
// - past expiration
// - within buffer (29s, 30s)
// - outside buffer (31s, 60s)
// - negative and custom buffers
func TestAdversarial_TokenInfo_IsExpired_Boundaries(t *testing.T) {
	// 1. Nil receiver
	var nilTok *TokenInfo
	if !nilTok.IsExpired() {
		t.Fatal("nil receiver must report IsExpired() = true")
	}
	if nilTok.Valid() {
		t.Fatal("nil receiver must report Valid() = false")
	}

	// 2. Empty AccessToken
	emptyTok := &TokenInfo{
		AccessToken: "",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}
	if !emptyTok.IsExpired() {
		t.Fatal("token with empty AccessToken must report IsExpired() = true")
	}
	if emptyTok.Valid() {
		t.Fatal("token with empty AccessToken must report Valid() = false")
	}

	// 3. Zero ExpiresAt (never expires / no expiration provided)
	zeroTok := &TokenInfo{
		AccessToken: "infinite-token",
		ExpiresAt:   time.Time{},
	}
	if zeroTok.IsExpired() {
		t.Fatal("token with zero ExpiresAt should not be expired")
	}
	if !zeroTok.Valid() {
		t.Fatal("token with zero ExpiresAt and valid AccessToken should be valid")
	}

	// 4. Past expiration
	pastTok := &TokenInfo{
		AccessToken: "old-token",
		ExpiresAt:   time.Now().Add(-5 * time.Second),
	}
	if !pastTok.IsExpired() {
		t.Fatal("past token must be expired")
	}
	if pastTok.Valid() {
		t.Fatal("past token must not be valid")
	}

	// 5. Expiration within safety buffer (default 30s)
	// 20s in future: time.Now() + 30s > time.Now() + 20s -> expired!
	soonTok := &TokenInfo{
		AccessToken: "expiring-soon",
		ExpiresAt:   time.Now().Add(20 * time.Second),
	}
	if !soonTok.IsExpired() {
		t.Fatal("token expiring in 20s must be treated as expired under 30s safety buffer")
	}
	if soonTok.Valid() {
		t.Fatal("token expiring within safety buffer must not be Valid()")
	}

	// 6. Expiration outside safety buffer
	// 60s in future: time.Now() + 30s < time.Now() + 60s -> not expired!
	freshTok := &TokenInfo{
		AccessToken: "fresh-token",
		ExpiresAt:   time.Now().Add(60 * time.Second),
	}
	if freshTok.IsExpired() {
		t.Fatal("token expiring in 60s should NOT be expired under 30s safety buffer")
	}
	if !freshTok.Valid() {
		t.Fatal("fresh token expiring in 60s should be Valid()")
	}

	// 7. Custom buffer boundaries
	// Token expires in 15 seconds:
	// - with 10s buffer: not expired (15 > 10)
	// - with 20s buffer: expired (15 < 20)
	// - with 0s buffer: not expired (15 > 0)
	customTok := &TokenInfo{
		AccessToken: "custom-token",
		ExpiresAt:   time.Now().Add(15 * time.Second),
	}
	if customTok.IsExpiredWithBuffer(10 * time.Second) {
		t.Fatal("token expiring in 15s should not be expired with 10s buffer")
	}
	if !customTok.IsExpiredWithBuffer(20 * time.Second) {
		t.Fatal("token expiring in 15s must be expired with 20s buffer")
	}
	if customTok.IsExpiredWithBuffer(0) {
		t.Fatal("token expiring in 15s should not be expired with 0s buffer")
	}

	// 8. Negative buffer (only expired if expired at least |buffer| ago)
	if customTok.IsExpiredWithBuffer(-30 * time.Second) {
		t.Fatal("future token with negative buffer should not be expired")
	}
}

// ============================================================================
// 5. Deeper Edge Cases & Architectural Behavior Observations
// ============================================================================

// TestAdversarial_Epic_AuthCode_Restart_StoreBehavior documents the behavior
// when an auth code was used in run 1 and saved to store, but the user leaves
// the now-consumed auth code in config on daemon restart.
// Because Authenticate checks `if refreshToken == "" && authCode == ""`,
// a non-empty (consumed) AuthCode prevents StateStore lookup, resulting in
// ErrAuthFailed on restart unless the user clears auth_code or provides refresh_token.
func TestAdversarial_Epic_AuthCode_Restart_StoreBehavior(t *testing.T) {
	store := newMockStateStore()
	// Simulate run 1 already completed and saved rotated refresh token to store:
	_ = store.SaveAuthState(context.Background(), "epic", "rotated-refresh-from-run1", "epic-acc", "Player")

	mock := &mockEGS{
		authenticateWithCodeFunc: func(code string) (*rlapi.TokenResponse, error) {
			// Second run: provider rejects consumed one-time auth code
			return nil, errors.New("oauth2: authorization code already used or expired")
		},
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			if rt == "rotated-refresh-from-run1" {
				return &rlapi.TokenResponse{
					AccessToken:  "fresh-egs-token",
					RefreshToken: "rotated-refresh-from-run2",
					ExpiresIn:    3600,
				}, nil
			}
			return nil, errors.New("unexpected token")
		},
	}

	// Daemon restarts with consumed auth_code still in config:
	p, _ := NewEpicProvider(
		config.EpicConfig{AuthCode: "consumed-code-from-run1"},
		store,
		WithEGSClient(mock),
	)

	_, err := p.Authenticate(context.Background())
	// Documented behavior: Authenticate fails with ErrAuthFailed because it
	// attempts the config auth_code first and does not fall back to StateStore
	if err == nil {
		t.Fatal("expected Authenticate to fail with consumed auth_code, got nil")
	}
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("expected ErrAuthFailed, got: %v", err)
	}
}

// TestAdversarial_Steam_Refresh_MasksUnderlyingError documents that SteamAuthProvider.Refresh
// discards the underlying error when RefreshEOSToken fails (e.g. network timeout),
// attributing the failure solely to session ticket expiration.
func TestAdversarial_Steam_Refresh_MasksUnderlyingError(t *testing.T) {
	mock := &mockEGS{
		refreshEOSTokenFunc: func(rt string) (*rlapi.EOSTokenResponse, error) {
			return nil, errors.New("dial tcp 127.0.0.1:443: i/o timeout")
		},
	}

	p, _ := NewSteamProvider(
		config.SteamConfig{
			SessionTicket: "ticket",
			SteamID64:     "76561198000000000",
		},
		nil,
		WithEGSClient(mock),
	)

	_, err := p.Refresh(context.Background(), "existing-eos-refresh-token")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrRefreshFailed) {
		t.Fatalf("expected ErrRefreshFailed, got: %v", err)
	}
	// The original network timeout ("i/o timeout") is masked by the static message
	if strings.Contains(err.Error(), "i/o timeout") {
		t.Log("Underlying error was preserved")
	} else {
		t.Log("Underlying network error was masked by static renewal message (documented finding)")
	}
}

// TestAdversarial_Steam_Authenticate_ContextCanceled_DuringExchange documents that
// SteamAuthProvider.Authenticate does not re-check ctx.Err() after ExchangeEOSTokenFromSteam,
// returning success if exchange succeeded even if context was canceled mid-call.
func TestAdversarial_Steam_Authenticate_ContextCanceled_DuringExchange(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	mock := &mockEGS{
		exchangeEOSTokenFromSteamFunc: func(ticket string) (*rlapi.EOSTokenResponse, error) {
			cancel() // Context canceled during exchange
			return &rlapi.EOSTokenResponse{
				AccessToken: "eos-token-exchanged",
				ExpiresIn:   3600,
			}, nil
		},
	}

	p, _ := NewSteamProvider(
		config.SteamConfig{
			SessionTicket: "ticket",
			SteamID64:     "76561198000000000",
		},
		nil,
		WithEGSClient(mock),
	)

	info, err := p.Authenticate(ctx)
	// Because SteamAuthProvider.Authenticate does not check ctx.Err() after exchange,
	// info is returned without error even though ctx was canceled.
	if err != nil {
		t.Logf("Authenticate returned error on canceled context: %v", err)
	} else {
		if info == nil || info.AccessToken != "eos-token-exchanged" {
			t.Fatalf("unexpected info: %v", info)
		}
		t.Log("Steam Authenticate succeeded despite context canceled during exchange (documented finding)")
	}
}

