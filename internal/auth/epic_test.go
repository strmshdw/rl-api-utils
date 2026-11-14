package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rlapi"
)

func TestEpicAuthProvider_Validate(t *testing.T) {
	p1, _ := NewEpicProvider(config.EpicConfig{}, nil)
	if err := p1.Validate(); err == nil || !errors.Is(err, ErrMissingCredentials) {
		t.Fatalf("expected ErrMissingCredentials for empty config, got %v", err)
	}

	p2, _ := NewEpicProvider(config.EpicConfig{RefreshToken: "tok"}, nil)
	if err := p2.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	p3, _ := NewEpicProvider(config.EpicConfig{AuthCode: "code"}, nil)
	if err := p3.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	p4, _ := NewEpicProvider(config.EpicConfig{RefreshToken: " \t\n ", AuthCode: " \r\n "}, nil)
	if err := p4.Validate(); err == nil || !errors.Is(err, ErrMissingCredentials) {
		t.Fatalf("expected ErrMissingCredentials for whitespace config, got %v", err)
	}
}

func TestEpicAuthProvider_Authenticate_WithRefreshToken(t *testing.T) {
	mock := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			if rt != "my-refresh-token" {
				return nil, errors.New("invalid refresh token")
			}
			return &rlapi.TokenResponse{
				AccessToken:  "egs-access-1",
				RefreshToken: "my-new-refresh-token",
				AccountID:    "epic-acc-1",
				DisplayName:  "EpicGamer",
				ExpiresIn:    3600,
			}, nil
		},
		getExchangeCodeFunc: func(at string) (string, error) {
			if at != "egs-access-1" {
				return "", errors.New("invalid access token")
			}
			return "exchange-code-1", nil
		},
		exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
			if ec != "exchange-code-1" {
				return nil, errors.New("invalid exchange code")
			}
			return &rlapi.EOSTokenResponse{
				AccessToken: "eos-token-final",
				AccountID:   "epic-acc-1",
				TokenType:   "Bearer",
				ExpiresIn:   3600,
			}, nil
		},
	}

	store := newMockStateStore()
	now := time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)

	p, err := NewEpicProvider(
		config.EpicConfig{RefreshToken: "my-refresh-token"},
		store,
		WithEGSClient(mock),
		WithClock(func() time.Time { return now }),
	)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	info, err := p.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("authenticate failed: %v", err)
	}

	if info.AccessToken != "eos-token-final" || info.AccountID != "epic-acc-1" ||
		info.EpicAccountID != "epic-acc-1" || info.DisplayName != "EpicGamer" ||
		info.RefreshToken != "my-new-refresh-token" {
		t.Fatalf("unexpected TokenInfo: %+v", info)
	}
	if !info.ExpiresAt.Equal(now.Add(3600 * time.Second)) {
		t.Fatalf("expected %v, got %v", now.Add(3600*time.Second), info.ExpiresAt)
	}

	// Verify TokenInfo inherited provider's clock (advancing clock across 5m buffer triggers expiry).
	if info.IsExpired() {
		t.Fatal("fresh token should not be expired at t=0")
	}
	now = now.Add(56 * time.Minute)
	if !info.IsExpired() {
		t.Fatal("token should be expired when provider clock is within 5m buffer")
	}

	savedToken, savedAcc, savedDisp, err := store.GetAuthState(context.Background(), "epic")
	if err != nil || savedToken != "my-new-refresh-token" || savedAcc != "epic-acc-1" || savedDisp != "EpicGamer" {
		t.Fatalf("store state mismatch: (%s, %s, %s, %v)", savedToken, savedAcc, savedDisp, err)
	}
}

func TestEpicAuthProvider_Authenticate_WithAuthCode(t *testing.T) {
	mock := &mockEGS{
		authenticateWithCodeFunc: func(code string) (*rlapi.TokenResponse, error) {
			if code != "web-auth-code" {
				return nil, errors.New("bad code")
			}
			return &rlapi.TokenResponse{
				AccessToken:  "egs-access-code",
				RefreshToken: "refresh-from-code",
				AccountID:    "code-user",
				DisplayName:  "CodeGamer",
				ExpiresIn:    3600,
			}, nil
		},
		getExchangeCodeFunc: func(at string) (string, error) { return "ex-code", nil },
		exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
			return &rlapi.EOSTokenResponse{AccessToken: "eos-from-code", AccountID: "code-user", ExpiresIn: 3600}, nil
		},
	}

	p, err := NewEpicProvider(config.EpicConfig{AuthCode: "web-auth-code"}, nil, WithEGSClient(mock))
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	info, err := p.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("authenticate with code failed: %v", err)
	}
	if info.AccessToken != "eos-from-code" || info.RefreshToken != "refresh-from-code" {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestEpicAuthProvider_Authenticate_RestoreFromStateStore(t *testing.T) {
	store := newMockStateStore()
	_ = store.SaveAuthState(context.Background(), "epic", "restored-token", "stored-acc", "StoredPlayer")

	mock := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			if rt != "restored-token" {
				return nil, errors.New("unexpected refresh token")
			}
			return &rlapi.TokenResponse{
				AccessToken:  "egs-restored",
				RefreshToken: "restored-token-v2",
				AccountID:    "stored-acc",
				DisplayName:  "StoredPlayer",
				ExpiresIn:    3600,
			}, nil
		},
		getExchangeCodeFunc: func(at string) (string, error) { return "ex-restored", nil },
		exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
			return &rlapi.EOSTokenResponse{AccessToken: "eos-restored", ExpiresIn: 3600}, nil
		},
	}

	p, err := NewEpicProvider(config.EpicConfig{}, store, WithEGSClient(mock))
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	info, err := p.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("authenticate failed to restore from store: %v", err)
	}
	if info.AccessToken != "eos-restored" {
		t.Fatalf("unexpected token: %s", info.AccessToken)
	}
}

// TestEpicAuthProvider_StateStorePreferredOverStaleAuthCode verifies that when cfg.RefreshToken
// is empty, StateStore refresh_token is consulted and preferred EVEN IF a stale one-time auth_code
// remains in config, falling back to auth_code or prompter only if StateStore fails.
func TestEpicAuthProvider_StateStorePreferredOverStaleAuthCode(t *testing.T) {
	store := newMockStateStore()
	_ = store.SaveAuthState(context.Background(), "epic", "rotated-refresh-from-run1", "epic-acc", "Player")

	var savedUpdates []config.TokenUpdates
	saver := func(u config.TokenUpdates) error {
		savedUpdates = append(savedUpdates, u)
		return nil
	}
	prompter := &mockPrompter{returnCode: "should-not-prompt"}

	mock := &mockEGS{
		authenticateWithCodeFunc: func(code string) (*rlapi.TokenResponse, error) {
			t.Fatalf("consumed auth_code %q should NOT be called when StateStore has valid refresh_token", code)
			return nil, errors.New("consumed auth code")
		},
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			if rt != "rotated-refresh-from-run1" {
				return nil, errors.New("unexpected refresh token: " + rt)
			}
			return &rlapi.TokenResponse{
				AccessToken:  "fresh-egs-token",
				RefreshToken: "rotated-refresh-from-run2",
				AccountID:    "epic-acc",
				DisplayName:  "Player",
				ExpiresIn:    3600,
			}, nil
		},
		getExchangeCodeFunc: func(at string) (string, error) { return "ex-run2", nil },
		exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
			return &rlapi.EOSTokenResponse{AccessToken: "eos-run2", AccountID: "epic-acc", ExpiresIn: 3600}, nil
		},
	}

	p, err := NewEpicProvider(
		config.EpicConfig{AuthCode: "consumed-code-from-run1"},
		store,
		WithEGSClient(mock),
		WithCodePrompter(prompter),
		WithConfigTokenSaver(saver),
	)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	info, err := p.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("expected StateStore refresh token to succeed over stale auth_code, got: %v", err)
	}
	if info.AccessToken != "eos-run2" || info.RefreshToken != "rotated-refresh-from-run2" {
		t.Fatalf("unexpected TokenInfo: %+v", info)
	}
	if len(prompter.promptCalls) != 0 {
		t.Fatalf("prompter should not be called when StateStore token succeeds")
	}

	// Verify both StateStore and ConfigTokenSaver received the new rotated token.
	storedRT, _, _, err := store.GetAuthState(context.Background(), "epic")
	if err != nil || storedRT != "rotated-refresh-from-run2" {
		t.Fatalf("expected updated store refresh token, got %q (err=%v)", storedRT, err)
	}
	if len(savedUpdates) != 1 || savedUpdates[0].Primary.EpicRefreshToken == nil ||
		*savedUpdates[0].Primary.EpicRefreshToken != "rotated-refresh-from-run2" {
		t.Fatalf("expected TokenSaver update with rotated refresh token")
	}
}

func TestEpicAuthProvider_Authenticate_Errors(t *testing.T) {
	mockFailOAuth := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			return nil, errors.New("invalid credentials")
		},
	}
	p1, _ := NewEpicProvider(config.EpicConfig{RefreshToken: "bad"}, nil, WithEGSClient(mockFailOAuth))
	if _, err := p1.Authenticate(context.Background()); err == nil || !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("expected ErrAuthFailed, got: %v", err)
	}

	mockFailExchange := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			return &rlapi.TokenResponse{AccessToken: "ok"}, nil
		},
		getExchangeCodeFunc: func(at string) (string, error) {
			return "", errors.New("exchange rate limit")
		},
	}
	p2, _ := NewEpicProvider(config.EpicConfig{RefreshToken: "ok"}, nil, WithEGSClient(mockFailExchange))
	if _, err := p2.Authenticate(context.Background()); err == nil || !errors.Is(err, ErrExchangeFailed) {
		t.Fatalf("expected ErrExchangeFailed, got: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p3, _ := NewEpicProvider(config.EpicConfig{RefreshToken: "ok"}, nil, WithEGSClient(mockFailExchange))
	if _, err := p3.Authenticate(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

func TestEpicAuthProvider_Refresh(t *testing.T) {
	mock := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			return &rlapi.TokenResponse{
				AccessToken:  "fresh-egs-token",
				RefreshToken: "fresh-refresh-token",
				AccountID:    "acc-1",
				DisplayName:  "Gamer",
				ExpiresIn:    3600,
			}, nil
		},
		getExchangeCodeFunc: func(at string) (string, error) { return "fresh-code", nil },
		exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
			return &rlapi.EOSTokenResponse{AccessToken: "fresh-eos-token", ExpiresIn: 3600}, nil
		},
	}

	p, _ := NewEpicProvider(config.EpicConfig{RefreshToken: "old-tok"}, nil, WithEGSClient(mock))
	refreshed, err := p.Refresh(context.Background(), "")
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if refreshed.AccessToken != "fresh-eos-token" || refreshed.RefreshToken != "fresh-refresh-token" {
		t.Fatalf("unexpected refresh result: %+v", refreshed)
	}
}

func TestEpic_InteractivePrompt_Flows(t *testing.T) {
	t.Run("primary prompt on missing tokens", func(t *testing.T) {
		prompter := &mockPrompter{returnCode: "prompted-auth-code"}
		var savedUpdates []config.TokenUpdates
		store := newMockStateStore()

		mock := &mockEGS{
			getAuthURLFunc: func() string { return "https://www.epicgames.com/id/api/redirect?client_id=xyz" },
			authenticateWithCodeFunc: func(code string) (*rlapi.TokenResponse, error) {
				if code != "prompted-auth-code" {
					return nil, errors.New("unexpected auth code")
				}
				return &rlapi.TokenResponse{
					AccessToken: "egs-access", RefreshToken: "new-saved-refresh",
					AccountID: "epic-user-123", DisplayName: "EpicPlayer", ExpiresIn: 3600,
				}, nil
			},
			exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
				return &rlapi.EOSTokenResponse{AccessToken: "eos-access-123", AccountID: "epic-user-123", ExpiresIn: 3600}, nil
			},
		}

		p, _ := NewEpicProvider(
			config.EpicConfig{}, store,
			WithEGSClient(mock), WithCodePrompter(prompter),
			WithConfigTokenSaver(func(u config.TokenUpdates) error { savedUpdates = append(savedUpdates, u); return nil }),
			WithAccountRole(RolePrimary),
		)
		if err := p.Validate(); err != nil {
			t.Fatalf("expected Validate to pass with prompter: %v", err)
		}
		info, err := p.Authenticate(context.Background())
		if err != nil {
			t.Fatalf("unexpected authenticate error: %v", err)
		}
		if len(prompter.promptCalls) != 1 || prompter.promptCalls[0].AccountLabel != "Primary Account" ||
			!strings.Contains(prompter.promptCalls[0].AuthURL, "client_id=xyz") {
			t.Fatalf("unexpected prompt call: %+v", prompter.promptCalls)
		}
		if len(savedUpdates) != 1 || *savedUpdates[0].Primary.EpicRefreshToken != "new-saved-refresh" {
			t.Fatalf("expected Primary.EpicRefreshToken saved")
		}
		if stored, _, _, err := store.GetAuthState(context.Background(), "epic"); err != nil || stored != "new-saved-refresh" {
			t.Fatalf("store mismatch: %q, %v", stored, err)
		}
		if info.AccessToken != "eos-access-123" {
			t.Fatalf("expected eos-access-123, got %q", info.AccessToken)
		}
	})

	t.Run("secondary polling account isolation", func(t *testing.T) {
		prompter := &mockPrompter{returnCode: "polling-auth-code"}
		var savedUpdates []config.TokenUpdates
		store := newMockStateStore()

		mock := &mockEGS{
			authenticateWithCodeFunc: func(code string) (*rlapi.TokenResponse, error) {
				return &rlapi.TokenResponse{
					AccessToken: "poll-egs", RefreshToken: "polling-refresh-token",
					AccountID: "polling-account-id", DisplayName: "PollingAlt", ExpiresIn: 3600,
				}, nil
			},
			exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
				return &rlapi.EOSTokenResponse{AccessToken: "eos-polling-token", ExpiresIn: 3600}, nil
			},
		}

		p, _ := NewEpicProvider(
			config.EpicConfig{}, store,
			WithEGSClient(mock), WithCodePrompter(prompter),
			WithConfigTokenSaver(func(u config.TokenUpdates) error { savedUpdates = append(savedUpdates, u); return nil }),
			WithAccountRole(RolePolling),
		)
		info, err := p.Authenticate(context.Background())
		if err != nil {
			t.Fatalf("unexpected authenticate error: %v", err)
		}
		if len(prompter.promptCalls) != 1 || prompter.promptCalls[0].AccountLabel != "Secondary Polling Account" {
			t.Fatalf("unexpected prompt call: %+v", prompter.promptCalls)
		}
		if len(savedUpdates) != 1 || *savedUpdates[0].Polling.EpicRefreshToken != "polling-refresh-token" {
			t.Fatalf("expected Polling.EpicRefreshToken saved")
		}
		if stored, _, _, err := store.GetAuthState(context.Background(), "epic_polling"); err != nil || stored != "polling-refresh-token" {
			t.Fatalf("expected epic_polling store entry, got %q, %v", stored, err)
		}
		if _, _, _, err := store.GetAuthState(context.Background(), "epic"); !errors.Is(err, storage.ErrAuthStateNotFound) {
			t.Fatalf("expected primary epic store key untouched, got err=%v", err)
		}
		if info.AccessToken != "eos-polling-token" {
			t.Fatalf("expected eos-polling-token, got %q", info.AccessToken)
		}
	})

	t.Run("expired refresh token triggers prompt fallback", func(t *testing.T) {
		prompter := &mockPrompter{returnCode: "recovered-auth-code"}
		var savedUpdates []config.TokenUpdates

		mock := &mockEGS{
			authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
				return nil, errors.New("HTTP 400: refresh_token_invalid")
			},
			authenticateWithCodeFunc: func(code string) (*rlapi.TokenResponse, error) {
				return &rlapi.TokenResponse{
					AccessToken: "new-access", RefreshToken: "new-refresh-token",
					AccountID: "acc-recovered", ExpiresIn: 3600,
				}, nil
			},
			exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
				return &rlapi.EOSTokenResponse{AccessToken: "eos-recovered", ExpiresIn: 3600}, nil
			},
		}

		p, _ := NewEpicProvider(
			config.EpicConfig{RefreshToken: "expired-configured-token"}, nil,
			WithEGSClient(mock), WithCodePrompter(prompter),
			WithConfigTokenSaver(func(u config.TokenUpdates) error { savedUpdates = append(savedUpdates, u); return nil }),
		)
		info, err := p.Authenticate(context.Background())
		if err != nil {
			t.Fatalf("authenticate failed: %v", err)
		}
		if len(prompter.promptCalls) != 1 || info.AccessToken != "eos-recovered" ||
			len(savedUpdates) != 1 || *savedUpdates[0].Primary.EpicRefreshToken != "new-refresh-token" {
			t.Fatalf("unexpected recovery state: info=%+v updates=%+v", info, savedUpdates)
		}
	})
}
