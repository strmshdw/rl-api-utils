package auth

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rlapi"
)

type spyStateStore struct {
	storage.StateStore
	mu           sync.Mutex
	reads        int
	writes       int
	readKeys     []string
	writtenKeys  []string
	writtenCreds []string
}

func (s *spyStateStore) SaveAuthState(ctx context.Context, provider, refreshToken, accountID, displayName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	s.writtenKeys = append(s.writtenKeys, provider)
	s.writtenCreds = append(s.writtenCreds, refreshToken)
	return nil
}

func (s *spyStateStore) GetAuthState(ctx context.Context, provider string) (refreshToken, accountID, displayName string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	s.readKeys = append(s.readKeys, provider)
	return "stored-token", "stored-acc", "stored-name", nil
}

type mockEGSClientIsolation struct {
	mu           sync.Mutex
	refreshCalls int
}

func (m *mockEGSClientIsolation) GetAuthURL() string {
	return "https://www.epicgames.com/id/api/redirect"
}

func (m *mockEGSClientIsolation) AuthenticateWithCode(authCode string) (*rlapi.TokenResponse, error) {
	return &rlapi.TokenResponse{
		AccessToken:  "mock-access-token",
		RefreshToken: "mock-refresh-token",
		AccountID:    "mock-account-id",
		DisplayName:  "MockDisplay",
		ExpiresIn:    3600,
	}, nil
}

func (m *mockEGSClientIsolation) AuthenticateWithRefreshToken(refreshToken string) (*rlapi.TokenResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshCalls++
	return &rlapi.TokenResponse{
		AccessToken:  "mock-access-token",
		RefreshToken: "new-rotated-refresh-token",
		AccountID:    "mock-account-id",
		DisplayName:  "MockDisplay",
		ExpiresIn:    3600,
	}, nil
}

func (m *mockEGSClientIsolation) GetExchangeCode(accessToken string) (string, error) {
	return "mock-exchange-code", nil
}

func (m *mockEGSClientIsolation) ExchangeEOSToken(exchangeCode string) (*rlapi.EOSTokenResponse, error) {
	return &rlapi.EOSTokenResponse{
		AccessToken: "mock-eos-access-token",
		AccountID:   "mock-eos-account-id",
		TokenType:   "bearer",
		ExpiresIn:   3600,
	}, nil
}

func (m *mockEGSClientIsolation) ExchangeEOSTokenFromSteam(steamTicket string) (*rlapi.EOSTokenResponse, error) {
	return &rlapi.EOSTokenResponse{
		AccessToken: "mock-eos-steam-token",
		AccountID:   "mock-eos-steam-acc",
		TokenType:   "bearer",
		ExpiresIn:   3600,
	}, nil
}

func (m *mockEGSClientIsolation) RefreshEOSToken(refreshToken string) (*rlapi.EOSTokenResponse, error) {
	return &rlapi.EOSTokenResponse{
		AccessToken: "mock-refreshed-eos-token",
		ExpiresIn:   3600,
	}, nil
}

// TestNewPollingProvider_InMemoryIsolation verifies that NewPollingProvider strictly operates
// in-memory without any persistence to StateStore during normal operation.
func TestNewPollingProvider_InMemoryIsolation(t *testing.T) {
	cfg := config.PollingAuthConfig{
		Enabled:  true,
		Provider: "epic",
		Epic: config.EpicConfig{
			RefreshToken: "polling-account-refresh-token",
			AccountID:    "polling-account-id",
			DisplayName:  "PollingBot",
		},
	}

	mockEGS := &mockEGSClientIsolation{}
	provider, err := NewPollingProvider(cfg, WithEGSClient(mockEGS))
	if err != nil {
		t.Fatalf("NewPollingProvider failed: %v", err)
	}

	ctx := context.Background()

	// 1. Authenticate should succeed strictly in-memory
	token, err := provider.Authenticate(ctx)
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}
	if token.AccessToken != "mock-eos-access-token" {
		t.Errorf("token.AccessToken = %q; want mock-eos-access-token", token.AccessToken)
	}
	if token.RefreshToken != "new-rotated-refresh-token" {
		t.Errorf("token.RefreshToken = %q; want new-rotated-refresh-token", token.RefreshToken)
	}

	// Verify EpicAuthProvider's store field is nil
	epicProv, ok := provider.(*EpicAuthProvider)
	if !ok {
		t.Fatalf("expected *EpicAuthProvider, got %T", provider)
	}
	if epicProv.store != nil {
		t.Errorf("epicProv.store is NOT nil! In-memory isolation violated.")
	}

	// 2. Refresh should also operate strictly in-memory without error
	refreshedToken, err := provider.Refresh(ctx, "")
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}
	if refreshedToken.RefreshToken != "new-rotated-refresh-token" {
		t.Errorf("refreshedToken.RefreshToken = %q; want new-rotated-refresh-token", refreshedToken.RefreshToken)
	}
	if epicProv.store != nil {
		t.Errorf("epicProv.store became non-nil after Refresh!")
	}
}

// TestNewPollingProvider_SteamInMemoryIsolation verifies Steam polling provider store is nil.
func TestNewPollingProvider_SteamInMemoryIsolation(t *testing.T) {
	cfg := config.PollingAuthConfig{
		Enabled:  true,
		Provider: "steam",
		Steam: config.SteamConfig{
			SessionTicket: "valid-steam-session-ticket",
			SteamID64:     "76561198000000002",
		},
	}

	mockEGS := &mockEGSClientIsolation{}
	provider, err := NewPollingProvider(cfg, WithEGSClient(mockEGS))
	if err != nil {
		t.Fatalf("NewPollingProvider failed: %v", err)
	}

	ctx := context.Background()
	token, err := provider.Authenticate(ctx)
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}
	if token.AccessToken != "mock-eos-steam-token" {
		t.Errorf("token.AccessToken = %q; want mock-eos-steam-token", token.AccessToken)
	}

	steamProv, ok := provider.(*SteamAuthProvider)
	if !ok {
		t.Fatalf("expected *SteamAuthProvider, got %T", provider)
	}
	if steamProv.store != nil {
		t.Errorf("steamProv.store is NOT nil! Steam in-memory isolation violated.")
	}
}

// TestNewPollingProvider_AdversarialOptionOverride checks whether passing WithStateStore
// in opts can bypass the store=nil design constraint.
func TestNewPollingProvider_AdversarialOptionOverride(t *testing.T) {
	cfg := config.PollingAuthConfig{
		Enabled:  true,
		Provider: "epic",
		Epic: config.EpicConfig{
			RefreshToken: "polling-tok",
		},
	}

	spy := &spyStateStore{}
	mockEGS := &mockEGSClientIsolation{}

	// Adversarial test: Caller maliciously or accidentally passes WithStateStore
	provider, err := NewPollingProvider(cfg, WithEGSClient(mockEGS), WithStateStore(spy))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	epicProv, ok := provider.(*EpicAuthProvider)
	if !ok {
		t.Fatalf("expected *EpicAuthProvider, got %T", provider)
	}

	// We observe whether NewPollingProvider sanitized the opts or let WithStateStore leak through
	if epicProv.store != nil {
		t.Logf("ADVERSARIAL OBSERVATION: Passing WithStateStore in opts allows store injection into polling provider (store is %T)", epicProv.store)
		_ = epicProv.store.SaveAuthState(context.Background(), "epic", "poll-tok", "poll-id", "poll-name")
		if spy.writes > 0 {
			t.Logf("ADVERSARIAL FINDING: Injected store received writes! NewPollingProvider does not sanitize WithStateStore from opts.")
		}
	} else {
		t.Logf("CONFIRMED: Polling provider successfully sanitized or ignored WithStateStore option.")
	}
}

// TestNewPollingProvider_DisabledErrors verifies disabled provider returns ErrMissingCredentials.
func TestNewPollingProvider_DisabledErrors(t *testing.T) {
	cfg := config.PollingAuthConfig{
		Enabled: false,
	}
	_, err := NewPollingProvider(cfg)
	if !errors.Is(err, ErrMissingCredentials) {
		t.Errorf("expected ErrMissingCredentials, got %v", err)
	}
}
