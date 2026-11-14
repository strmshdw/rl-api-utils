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

func TestAdversarial_Epic_RefreshTokenVsAuthCode_Precedence(t *testing.T) {
	var codeCalled bool
	mock := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			if rt != "cfg-refresh" {
				return nil, errors.New("wrong refresh token")
			}
			return &rlapi.TokenResponse{
				AccessToken: "egs-access-from-refresh", RefreshToken: "rotated-refresh-token",
				AccountID: "epic-acc-123", DisplayName: "EpicPlayer", ExpiresIn: 3600,
			}, nil
		},
		authenticateWithCodeFunc: func(code string) (*rlapi.TokenResponse, error) {
			codeCalled = true
			return nil, errors.New("should not be called")
		},
		exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
			return &rlapi.EOSTokenResponse{AccessToken: "eos-token-success", ExpiresIn: 3600}, nil
		},
	}

	p, err := NewEpicProvider(config.EpicConfig{RefreshToken: "cfg-refresh", AuthCode: "cfg-auth-code"}, nil, WithEGSClient(mock))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info, err := p.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("authenticate failed: %v", err)
	}
	if codeCalled || info.AccessToken != "eos-token-success" || info.RefreshToken != "rotated-refresh-token" {
		t.Fatalf("unexpected result: codeCalled=%v info=%+v", codeCalled, info)
	}
}

func TestAdversarial_Epic_RefreshToken_Fails_FallbackToAuthCode(t *testing.T) {
	var refreshAttempted, codeAttempted bool
	mock := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			refreshAttempted = true
			return nil, errors.New("refresh token revoked")
		},
		authenticateWithCodeFunc: func(code string) (*rlapi.TokenResponse, error) {
			codeAttempted = true
			if code != "fallback-code" {
				return nil, errors.New("unexpected auth code")
			}
			return &rlapi.TokenResponse{
				AccessToken: "egs-access-from-code", RefreshToken: "brand-new-refresh-token",
				AccountID: "epic-acc-fallback", DisplayName: "FallbackPlayer", ExpiresIn: 3600,
			}, nil
		},
		exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
			return &rlapi.EOSTokenResponse{AccessToken: "eos-token-fallback", ExpiresIn: 3600}, nil
		},
	}

	store := newMockStateStore()
	p, _ := NewEpicProvider(config.EpicConfig{RefreshToken: "revoked-refresh", AuthCode: "fallback-code"}, store, WithEGSClient(mock))
	info, err := p.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("expected fallback to auth code to succeed, got: %v", err)
	}
	if !refreshAttempted || !codeAttempted || info.AccessToken != "eos-token-fallback" || info.RefreshToken != "brand-new-refresh-token" {
		t.Fatalf("unexpected fallback behavior: refresh=%v code=%v info=%+v", refreshAttempted, codeAttempted, info)
	}
	if savedTok, savedAcc, _, err := store.GetAuthState(context.Background(), "epic"); err != nil || savedTok != "brand-new-refresh-token" || savedAcc != "epic-acc-fallback" {
		t.Fatalf("unexpected stored credentials: tok=%s acc=%s err=%v", savedTok, savedAcc, err)
	}
}

func TestAdversarial_Epic_BothFail_ErrorWrapped(t *testing.T) {
	mock := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) { return nil, errors.New("bad rt") },
		authenticateWithCodeFunc:         func(code string) (*rlapi.TokenResponse, error) { return nil, errors.New("bad code") },
	}
	p, _ := NewEpicProvider(config.EpicConfig{RefreshToken: "bad-refresh", AuthCode: "bad-code"}, nil, WithEGSClient(mock))
	if _, err := p.Authenticate(context.Background()); err == nil || !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("expected ErrAuthFailed, got: %v", err)
	}
}

func TestAdversarial_Epic_StoreFallback_FullCascade(t *testing.T) {
	store := newMockStateStore()
	_ = store.SaveAuthState(context.Background(), "epic", "stored-rt", "stored-acc", "StoredUser")
	mock := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			if rt != "stored-rt" {
				return nil, errors.New("wrong token")
			}
			return &rlapi.TokenResponse{AccessToken: "egs-at", RefreshToken: "stored-rt-v2", AccountID: "stored-acc", ExpiresIn: 3600}, nil
		},
		exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
			return &rlapi.EOSTokenResponse{AccessToken: "eos-stored", ExpiresIn: 3600}, nil
		},
	}

	p, _ := NewEpicProvider(config.EpicConfig{}, store, WithEGSClient(mock))
	if info, err := p.Authenticate(context.Background()); err != nil || info.AccessToken != "eos-stored" {
		t.Fatalf("expected store fallback to succeed, got info=%+v err=%v", info, err)
	}

	p2, _ := NewEpicProvider(config.EpicConfig{}, newMockStateStore(), WithEGSClient(mock))
	if _, err := p2.Authenticate(context.Background()); err == nil || !errors.Is(err, ErrMissingCredentials) {
		t.Fatalf("expected ErrMissingCredentials from empty store, got: %v", err)
	}

	p3, _ := NewEpicProvider(config.EpicConfig{}, nil, WithEGSClient(mock))
	if _, err := p3.Authenticate(context.Background()); err == nil || !errors.Is(err, ErrMissingCredentials) {
		t.Fatalf("expected ErrMissingCredentials with nil store, got: %v", err)
	}
}

func TestAdversarial_Epic_Refresh_FallbackCascade(t *testing.T) {
	var usedToken string
	mock := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			usedToken = rt
			return &rlapi.TokenResponse{AccessToken: "at-" + rt, RefreshToken: rt + "-next", ExpiresIn: 3600}, nil
		},
		exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
			return &rlapi.EOSTokenResponse{AccessToken: "eos", ExpiresIn: 3600}, nil
		},
	}

	store := newMockStateStore()
	_ = store.SaveAuthState(context.Background(), "epic", "store-token", "acc", "user")
	p1, _ := NewEpicProvider(config.EpicConfig{RefreshToken: "cfg-token"}, store, WithEGSClient(mock))
	p1.mu.Lock()
	p1.tokenInfo = &TokenInfo{RefreshToken: "cached-token"}
	p1.mu.Unlock()

	if _, err := p1.Refresh(context.Background(), "explicit-arg-token"); err != nil || usedToken != "explicit-arg-token" {
		t.Fatalf("level 1 failed: used=%q err=%v", usedToken, err)
	}
	if _, err := p1.Refresh(context.Background(), ""); err != nil || usedToken != "explicit-arg-token-next" {
		t.Fatalf("level 2 failed: used=%q err=%v", usedToken, err)
	}

	store3 := newMockStateStore()
	_ = store3.SaveAuthState(context.Background(), "epic", "store-token", "acc", "user")
	p2, _ := NewEpicProvider(config.EpicConfig{RefreshToken: "cfg-token"}, store3, WithEGSClient(mock))
	if _, err := p2.Refresh(context.Background(), ""); err != nil || usedToken != "store-token" {
		t.Fatalf("level 3 failed: used=%q err=%v", usedToken, err)
	}

	p3, _ := NewEpicProvider(config.EpicConfig{RefreshToken: "cfg-token"}, newMockStateStore(), WithEGSClient(mock))
	if _, err := p3.Refresh(context.Background(), ""); err != nil || usedToken != "cfg-token" {
		t.Fatalf("level 4 failed: used=%q err=%v", usedToken, err)
	}

	p4, _ := NewEpicProvider(config.EpicConfig{}, newMockStateStore(), WithEGSClient(mock))
	if _, err := p4.Refresh(context.Background(), ""); !errors.Is(err, ErrMissingCredentials) {
		t.Fatalf("level 5 expected ErrMissingCredentials, got: %v", err)
	}
}

func TestAdversarial_TokenInfo_Immutability_And_DefaultExpiry(t *testing.T) {
	mock := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			return &rlapi.TokenResponse{AccessToken: "orig-access", RefreshToken: "orig-refresh", AccountID: "orig-acc", ExpiresIn: 3600}, nil
		},
		exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
			return &rlapi.EOSTokenResponse{AccessToken: "orig-eos", ExpiresIn: 3600}, nil
		},
		exchangeEOSTokenFromSteamFunc: func(ticket string) (*rlapi.EOSTokenResponse, error) {
			return &rlapi.EOSTokenResponse{AccessToken: "orig-steam-eos", RefreshToken: "orig-steam-refresh", AccountID: "orig-steam-acc", ExpiresIn: 3600}, nil
		},
	}

	pEpic, _ := NewEpicProvider(config.EpicConfig{RefreshToken: "tok"}, nil, WithEGSClient(mock))
	epicAuthRet, err := pEpic.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("epic authenticate failed: %v", err)
	}
	epicAuthRet.AccessToken = "MUTATED"
	tokEpic := pEpic.TokenInfo()
	tokEpic.RefreshToken = "MUTATED"
	if got := pEpic.TokenInfo(); got.AccessToken != "orig-eos" || got.RefreshToken != "orig-refresh" {
		t.Fatalf("epic TokenInfo mutated internally: %+v", got)
	}

	pSteam, _ := NewSteamProvider(config.SteamConfig{SessionTicket: "ticket", SteamID64: "76561198000000000"}, nil, WithEGSClient(mock))
	steamAuthRet, err := pSteam.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("steam authenticate failed: %v", err)
	}
	steamAuthRet.AccessToken = "CORRUPTED"
	if got := pSteam.TokenInfo(); got.AccessToken != "orig-steam-eos" || got.RefreshToken != "orig-steam-refresh" {
		t.Fatalf("steam TokenInfo mutated internally: %+v", got)
	}

	fixedTime := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	for _, exp := range []int{0, -1, -3600} {
		mExp := &mockEGS{
			authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
				return &rlapi.TokenResponse{AccessToken: "at", ExpiresIn: 3600}, nil
			},
			exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
				return &rlapi.EOSTokenResponse{AccessToken: "eos", ExpiresIn: exp}, nil
			},
		}
		p, _ := NewEpicProvider(config.EpicConfig{RefreshToken: "tok"}, nil, WithEGSClient(mExp), WithClock(func() time.Time { return fixedTime }))
		info, err := p.Authenticate(context.Background())
		if err != nil || !info.ExpiresAt.Equal(fixedTime.Add(3600*time.Second)) {
			t.Fatalf("for expiresIn=%d got info=%+v err=%v", exp, info, err)
		}
	}
}

func TestAdversarial_ContextCancellation_AllStages(t *testing.T) {
	ctxPre, cancelPre := context.WithCancel(context.Background())
	cancelPre()

	pEpic, _ := NewEpicProvider(config.EpicConfig{RefreshToken: "tok"}, nil, WithEGSClient(&mockEGS{}))
	if _, err := pEpic.Authenticate(ctxPre); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled pre-auth, got: %v", err)
	}
	if _, err := pEpic.Refresh(ctxPre, "tok"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled on Refresh, got: %v", err)
	}

	ctxStep3, cancelStep3 := context.WithCancel(context.Background())
	mStep3 := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			cancelStep3()
			return &rlapi.TokenResponse{AccessToken: "step2"}, nil
		},
	}
	pStep3, _ := NewEpicProvider(config.EpicConfig{RefreshToken: "tok"}, nil, WithEGSClient(mStep3))
	if _, err := pStep3.Authenticate(ctxStep3); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled before GetExchangeCode, got: %v", err)
	}

	ctxStep4, cancelStep4 := context.WithCancel(context.Background())
	mStep4 := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			return &rlapi.TokenResponse{AccessToken: "step2"}, nil
		},
		getExchangeCodeFunc: func(at string) (string, error) {
			cancelStep4()
			return "step3", nil
		},
	}
	pStep4, _ := NewEpicProvider(config.EpicConfig{RefreshToken: "tok"}, nil, WithEGSClient(mStep4))
	if _, err := pStep4.Authenticate(ctxStep4); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled before ExchangeEOSToken, got: %v", err)
	}

	pSteam, _ := NewSteamProvider(config.SteamConfig{SessionTicket: "ticket", SteamID64: "76561198000000000"}, nil)
	if _, err := pSteam.Authenticate(ctxPre); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled on Steam Authenticate, got: %v", err)
	}
	if _, err := pSteam.Refresh(ctxPre, "eos-token"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled on Steam Refresh, got: %v", err)
	}

	ctxDead, cancelDead := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Minute))
	defer cancelDead()
	if _, err := pEpic.Authenticate(ctxDead); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got: %v", err)
	}
}

func TestAdversarial_Steam_Refresh_SurfacesUnderlyingError(t *testing.T) {
	mock := &mockEGS{
		refreshEOSTokenFunc: func(rt string) (*rlapi.EOSTokenResponse, error) {
			return nil, errors.New("dial tcp 127.0.0.1:443: i/o timeout")
		},
	}
	p, _ := NewSteamProvider(config.SteamConfig{SessionTicket: "ticket", SteamID64: "76561198000000000"}, nil, WithEGSClient(mock))
	_, err := p.Refresh(context.Background(), "existing-eos-refresh-token")
	if err == nil || !errors.Is(err, ErrRefreshFailed) || !strings.Contains(err.Error(), "i/o timeout") {
		t.Fatalf("expected ErrRefreshFailed wrapping i/o timeout, got: %v", err)
	}
}
