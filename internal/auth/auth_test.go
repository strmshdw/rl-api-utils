package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rlapi"
)

// mockEGS implements EGSClient for unit testing without network requests.
type mockEGS struct {
	mu sync.Mutex

	authenticateWithCodeFunc         func(authCode string) (*rlapi.TokenResponse, error)
	authenticateWithRefreshTokenFunc func(refreshToken string) (*rlapi.TokenResponse, error)
	getExchangeCodeFunc              func(accessToken string) (string, error)
	exchangeEOSTokenFunc             func(exchangeCode string) (*rlapi.EOSTokenResponse, error)
	exchangeEOSTokenFromSteamFunc    func(steamTicket string) (*rlapi.EOSTokenResponse, error)
	refreshEOSTokenFunc              func(refreshToken string) (*rlapi.EOSTokenResponse, error)

	codeCalls         []string
	refreshCalls      []string
	exchangeCodeCalls []string
	eosTokenCalls     []string
	steamCalls        []string
	refreshEOSCalls   []string
}

func (m *mockEGS) AuthenticateWithCode(authCode string) (*rlapi.TokenResponse, error) {
	m.mu.Lock()
	m.codeCalls = append(m.codeCalls, authCode)
	fn := m.authenticateWithCodeFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(authCode)
	}
	return nil, errors.New("unexpected AuthenticateWithCode call")
}

func (m *mockEGS) AuthenticateWithRefreshToken(refreshToken string) (*rlapi.TokenResponse, error) {
	m.mu.Lock()
	m.refreshCalls = append(m.refreshCalls, refreshToken)
	fn := m.authenticateWithRefreshTokenFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(refreshToken)
	}
	return nil, errors.New("unexpected AuthenticateWithRefreshToken call")
}

func (m *mockEGS) GetExchangeCode(accessToken string) (string, error) {
	m.mu.Lock()
	m.exchangeCodeCalls = append(m.exchangeCodeCalls, accessToken)
	fn := m.getExchangeCodeFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(accessToken)
	}
	return "mock-exchange-code", nil
}

func (m *mockEGS) ExchangeEOSToken(exchangeCode string) (*rlapi.EOSTokenResponse, error) {
	m.mu.Lock()
	m.eosTokenCalls = append(m.eosTokenCalls, exchangeCode)
	fn := m.exchangeEOSTokenFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(exchangeCode)
	}
	return &rlapi.EOSTokenResponse{
		AccessToken: "mock-eos-access-token",
		ExpiresIn:   3600,
	}, nil
}

func (m *mockEGS) ExchangeEOSTokenFromSteam(steamTicket string) (*rlapi.EOSTokenResponse, error) {
	m.mu.Lock()
	m.steamCalls = append(m.steamCalls, steamTicket)
	fn := m.exchangeEOSTokenFromSteamFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(steamTicket)
	}
	return &rlapi.EOSTokenResponse{
		AccessToken: "mock-steam-eos-token",
		AccountID:   "epic-account-linked",
		ExpiresIn:   3600,
	}, nil
}

func (m *mockEGS) RefreshEOSToken(refreshToken string) (*rlapi.EOSTokenResponse, error) {
	m.mu.Lock()
	m.refreshEOSCalls = append(m.refreshEOSCalls, refreshToken)
	fn := m.refreshEOSTokenFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(refreshToken)
	}
	return &rlapi.EOSTokenResponse{
		AccessToken: "mock-refreshed-eos-token",
		ExpiresIn:   3600,
	}, nil
}

// mockStateStore implements storage.StateStore in memory for testing auth persistence.
type mockStateStore struct {
	mu        sync.Mutex
	authState map[string]struct {
		refreshToken string
		accountID    string
		displayName  string
	}
}

func newMockStateStore() *mockStateStore {
	return &mockStateStore{
		authState: make(map[string]struct {
			refreshToken string
			accountID    string
			displayName  string
		}),
	}
}

func (s *mockStateStore) SaveAuthState(ctx context.Context, provider, refreshToken, accountID, displayName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authState[provider] = struct {
		refreshToken string
		accountID    string
		displayName  string
	}{refreshToken, accountID, displayName}
	return nil
}

func (s *mockStateStore) GetAuthState(ctx context.Context, provider string) (string, string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.authState[provider]
	if !ok {
		return "", "", "", storage.ErrAuthStateNotFound
	}
	return rec.refreshToken, rec.accountID, rec.displayName, nil
}

func (s *mockStateStore) GetMatch(ctx context.Context, matchGUID string) (*storage.MatchRecord, error) {
	return nil, storage.ErrMatchNotFound
}
func (s *mockStateStore) ListPendingDownloads(ctx context.Context) ([]*storage.MatchRecord, error) {
	return nil, nil
}
func (s *mockStateStore) ListPendingUploads(ctx context.Context) ([]*storage.MatchRecord, error) {
	return nil, nil
}
func (s *mockStateStore) UpsertDiscoveredMatches(ctx context.Context, matches []*storage.MatchRecord) error {
	return nil
}
func (s *mockStateStore) MarkDownloading(ctx context.Context, matchGUID string) error    { return nil }
func (s *mockStateStore) MarkDownloaded(ctx context.Context, matchGUID, localPath string) error {
	return nil
}
func (s *mockStateStore) MarkDownloadFailed(ctx context.Context, matchGUID, errMsg string) error {
	return nil
}
func (s *mockStateStore) MarkUploading(ctx context.Context, matchGUID string) error { return nil }
func (s *mockStateStore) MarkUploaded(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error {
	return nil
}
func (s *mockStateStore) MarkDuplicate(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error {
	return nil
}
func (s *mockStateStore) MarkUploadFailed(ctx context.Context, matchGUID, errMsg string) error {
	return nil
}
func (s *mockStateStore) RecoverInFlight(ctx context.Context) error { return nil }
func (s *mockStateStore) Close() error                              { return nil }

// ============================================================================
// Factory & Validation Tests
// ============================================================================

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
	badCases := []string{"", "xbox", "psn", "nintendo"}
	for _, bc := range badCases {
		cfg := config.AuthConfig{Provider: bc}
		_, err := NewProvider(cfg, store)
		if err == nil {
			t.Fatalf("expected error for provider %q, got nil", bc)
		}
		if !errors.Is(err, ErrUnsupportedProvider) {
			t.Fatalf("expected ErrUnsupportedProvider for %q, got: %v", bc, err)
		}
	}
}

// ============================================================================
// Epic Authentication Tests
// ============================================================================

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
	fixedTime := time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)

	p, err := NewEpicProvider(
		config.EpicConfig{RefreshToken: "my-refresh-token"},
		store,
		WithEGSClient(mock),
		WithClock(func() time.Time { return fixedTime }),
	)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	info, err := p.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("authenticate failed: %v", err)
	}

	if info.AccessToken != "eos-token-final" {
		t.Fatalf("expected eos-token-final, got %s", info.AccessToken)
	}
	if info.AccountID != "epic-acc-1" {
		t.Fatalf("expected epic-acc-1, got %s", info.AccountID)
	}
	if info.EpicAccountID != "epic-acc-1" {
		t.Fatalf("expected epic-acc-1, got %s", info.EpicAccountID)
	}
	if info.DisplayName != "EpicGamer" {
		t.Fatalf("expected EpicGamer, got %s", info.DisplayName)
	}
	if info.RefreshToken != "my-new-refresh-token" {
		t.Fatalf("expected my-new-refresh-token, got %s", info.RefreshToken)
	}
	expectedExpiry := fixedTime.Add(3600 * time.Second)
	if !info.ExpiresAt.Equal(expectedExpiry) {
		t.Fatalf("expected %v, got %v", expectedExpiry, info.ExpiresAt)
	}

	// Verify persistence in StateStore
	savedToken, savedAcc, savedDisp, err := store.GetAuthState(context.Background(), "epic")
	if err != nil {
		t.Fatalf("store state lookup failed: %v", err)
	}
	if savedToken != "my-new-refresh-token" || savedAcc != "epic-acc-1" || savedDisp != "EpicGamer" {
		t.Fatalf("store state mismatch: got (%s, %s, %s)", savedToken, savedAcc, savedDisp)
	}

	// Verify cached TokenInfo
	cached := p.TokenInfo()
	if cached == nil || cached.AccessToken != "eos-token-final" {
		t.Fatalf("cached TokenInfo incorrect: %v", cached)
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
		getExchangeCodeFunc: func(at string) (string, error) {
			return "ex-code", nil
		},
		exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
			return &rlapi.EOSTokenResponse{
				AccessToken: "eos-from-code",
				AccountID:   "code-user",
				ExpiresIn:   3600,
			}, nil
		},
	}

	p, err := NewEpicProvider(
		config.EpicConfig{AuthCode: "web-auth-code"},
		nil,
		WithEGSClient(mock),
	)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	info, err := p.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("authenticate with code failed: %v", err)
	}
	if info.AccessToken != "eos-from-code" {
		t.Fatalf("expected eos-from-code, got %s", info.AccessToken)
	}
	if info.RefreshToken != "refresh-from-code" {
		t.Fatalf("expected refresh-from-code, got %s", info.RefreshToken)
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
		getExchangeCodeFunc: func(at string) (string, error) {
			return "ex-restored", nil
		},
		exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
			return &rlapi.EOSTokenResponse{
				AccessToken: "eos-restored",
				ExpiresIn:   3600,
			}, nil
		},
	}

	p, err := NewEpicProvider(
		config.EpicConfig{},
		store,
		WithEGSClient(mock),
	)
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

func TestEpicAuthProvider_Authenticate_Errors(t *testing.T) {
	// 1. EGS OAuth failure
	mockFailOAuth := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			return nil, errors.New("invalid credentials")
		},
	}
	p1, _ := NewEpicProvider(config.EpicConfig{RefreshToken: "bad"}, nil, WithEGSClient(mockFailOAuth))
	_, err := p1.Authenticate(context.Background())
	if err == nil || !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("expected ErrAuthFailed, got: %v", err)
	}

	// 2. Exchange code failure
	mockFailExchange := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			return &rlapi.TokenResponse{AccessToken: "ok"}, nil
		},
		getExchangeCodeFunc: func(at string) (string, error) {
			return "", errors.New("exchange rate limit")
		},
	}
	p2, _ := NewEpicProvider(config.EpicConfig{RefreshToken: "ok"}, nil, WithEGSClient(mockFailExchange))
	_, err = p2.Authenticate(context.Background())
	if err == nil || !errors.Is(err, ErrExchangeFailed) {
		t.Fatalf("expected ErrExchangeFailed, got: %v", err)
	}

	// 3. Context cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p3, _ := NewEpicProvider(config.EpicConfig{RefreshToken: "ok"}, nil, WithEGSClient(mockFailExchange))
	_, err = p3.Authenticate(ctx)
	if !errors.Is(err, context.Canceled) {
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
		getExchangeCodeFunc: func(at string) (string, error) {
			return "fresh-code", nil
		},
		exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
			return &rlapi.EOSTokenResponse{
				AccessToken: "fresh-eos-token",
				ExpiresIn:   3600,
			}, nil
		},
	}

	p, _ := NewEpicProvider(config.EpicConfig{RefreshToken: "old-tok"}, nil, WithEGSClient(mock))
	refreshed, err := p.Refresh(context.Background(), "")
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if refreshed.AccessToken != "fresh-eos-token" {
		t.Fatalf("expected fresh-eos-token, got %s", refreshed.AccessToken)
	}
	if refreshed.RefreshToken != "fresh-refresh-token" {
		t.Fatalf("expected fresh-refresh-token, got %s", refreshed.RefreshToken)
	}
}

// ============================================================================
// Steam Authentication Tests
// ============================================================================

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

func TestSteamAuthProvider_Authenticate_Success(t *testing.T) {
	mock := &mockEGS{
		exchangeEOSTokenFromSteamFunc: func(ticket string) (*rlapi.EOSTokenResponse, error) {
			if ticket != "valid-steam-ticket" {
				return nil, errors.New("bad ticket")
			}
			return &rlapi.EOSTokenResponse{
				AccessToken:  "eos-steam-access",
				RefreshToken: "eos-steam-refresh",
				AccountID:    "epic-linked-acc-id",
				TokenType:    "Bearer",
				ExpiresIn:    3600,
			}, nil
		},
	}

	store := newMockStateStore()
	fixedTime := time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)

	p, err := NewSteamProvider(
		config.SteamConfig{
			SessionTicket: "valid-steam-ticket",
			SteamID64:     "76561198000000000",
			AccountName:   "SteamGamer",
		},
		store,
		WithEGSClient(mock),
		WithClock(func() time.Time { return fixedTime }),
	)
	if err != nil {
		t.Fatalf("failed to create steam provider: %v", err)
	}

	info, err := p.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("steam authenticate failed: %v", err)
	}

	if info.AccessToken != "eos-steam-access" {
		t.Fatalf("expected eos-steam-access, got %s", info.AccessToken)
	}
	if info.AccountID != "76561198000000000" {
		t.Fatalf("expected steamid 76561198000000000, got %s", info.AccountID)
	}
	if info.EpicAccountID != "epic-linked-acc-id" {
		t.Fatalf("expected linked epic acc id, got %s", info.EpicAccountID)
	}
	if info.DisplayName != "SteamGamer" {
		t.Fatalf("expected SteamGamer, got %s", info.DisplayName)
	}

	// Verify persistence in StateStore
	savedToken, savedAcc, savedDisp, err := store.GetAuthState(context.Background(), "steam")
	if err != nil {
		t.Fatalf("store lookup failed: %v", err)
	}
	if savedToken != "valid-steam-ticket" || savedAcc != "76561198000000000" || savedDisp != "SteamGamer" {
		t.Fatalf("store state mismatch: got (%s, %s, %s)", savedToken, savedAcc, savedDisp)
	}
}

func TestSteamAuthProvider_Authenticate_Failure(t *testing.T) {
	mock := &mockEGS{
		exchangeEOSTokenFromSteamFunc: func(ticket string) (*rlapi.EOSTokenResponse, error) {
			return nil, errors.New("steam ticket expired")
		},
	}

	p, _ := NewSteamProvider(
		config.SteamConfig{
			SessionTicket: "expired-ticket",
			SteamID64:     "76561198000000000",
		},
		nil,
		WithEGSClient(mock),
	)

	_, err := p.Authenticate(context.Background())
	if err == nil || !errors.Is(err, ErrExchangeFailed) {
		t.Fatalf("expected ErrExchangeFailed, got: %v", err)
	}
}

func TestSteamAuthProvider_Refresh(t *testing.T) {
	// Case 1: Refresh succeeds via EOS refresh token
	mockSuccess := &mockEGS{
		refreshEOSTokenFunc: func(rt string) (*rlapi.EOSTokenResponse, error) {
			return &rlapi.EOSTokenResponse{
				AccessToken: "refreshed-steam-eos",
				ExpiresIn:   3600,
			}, nil
		},
	}
	p1, _ := NewSteamProvider(
		config.SteamConfig{
			SessionTicket: "ticket",
			SteamID64:     "76561198000000000",
		},
		nil,
		WithEGSClient(mockSuccess),
	)
	refreshed, err := p1.Refresh(context.Background(), "eos-refresh-token")
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if refreshed.AccessToken != "refreshed-steam-eos" {
		t.Fatalf("expected refreshed-steam-eos, got %s", refreshed.AccessToken)
	}

	// Case 2: No refresh token available -> informs caller that fresh ticket is required
	p2, _ := NewSteamProvider(
		config.SteamConfig{
			SessionTicket: "ticket",
			SteamID64:     "76561198000000000",
		},
		nil,
		WithEGSClient(&mockEGS{}),
	)
	_, err = p2.Refresh(context.Background(), "")
	if err == nil || !errors.Is(err, ErrRefreshFailed) {
		t.Fatalf("expected ErrRefreshFailed, got: %v", err)
	}
	if !strings.Contains(err.Error(), "steam session ticket cannot be automatically renewed") {
		t.Fatalf("expected informative message about steam ticket renewal, got: %v", err)
	}
}

// ============================================================================
// TokenInfo Helper Tests
// ============================================================================

func TestTokenInfo_IsExpired(t *testing.T) {
	var nilInfo *TokenInfo
	if !nilInfo.IsExpired() {
		t.Fatal("nil TokenInfo should be expired")
	}
	if nilInfo.Valid() {
		t.Fatal("nil TokenInfo should not be valid")
	}

	now := time.Now()
	expiredInfo := &TokenInfo{
		AccessToken: "some-tok",
		ExpiresAt:   now.Add(-10 * time.Minute),
	}
	if !expiredInfo.IsExpired() {
		t.Fatal("past token should be expired")
	}
	if expiredInfo.Valid() {
		t.Fatal("past token should not be valid")
	}

	expiringSoon := &TokenInfo{
		AccessToken: "some-tok",
		ExpiresAt:   now.Add(10 * time.Second),
	}
	if !expiringSoon.IsExpired() {
		t.Fatal("token expiring within 30s buffer should be treated as expired")
	}

	freshInfo := &TokenInfo{
		AccessToken: "some-tok",
		ExpiresAt:   now.Add(1 * time.Hour),
	}
	if freshInfo.IsExpired() {
		t.Fatal("token expiring in 1 hour should not be expired")
	}
	if !freshInfo.Valid() {
		t.Fatal("fresh token should be valid")
	}
}
