package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rlapi"
)

// mockPrompter captures prompt requests and returns configured authorization codes.
type mockPrompter struct {
	mu           sync.Mutex
	promptCalls  []promptCall
	returnCode   string
	returnErr    error
}

type promptCall struct {
	AccountLabel string
	AuthURL      string
}

func (m *mockPrompter) PromptForCode(ctx context.Context, accountLabel, authURL string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.promptCalls = append(m.promptCalls, promptCall{
		AccountLabel: accountLabel,
		AuthURL:      authURL,
	})
	if m.returnErr != nil {
		return "", m.returnErr
	}
	return m.returnCode, nil
}

// mockSteamGenerator records calls and returns simulated steam ticket results.
type mockSteamGenerator struct {
	mu           sync.Mutex
	calls        []SteamTicketOptions
	returnResult *SteamTicketResult
	returnErr    error
}

func (m *mockSteamGenerator) GenerateTicket(ctx context.Context, opts SteamTicketOptions) (*SteamTicketResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, opts)
	if m.returnErr != nil {
		return nil, m.returnErr
	}
	return m.returnResult, nil
}

// ============================================================================
// Epic Integrated Interactive Auth Tests
// ============================================================================

func TestEpic_InteractivePrompt_OnMissingTokens(t *testing.T) {
	prompter := &mockPrompter{returnCode: "prompted-auth-code"}
	var savedUpdates []config.TokenUpdates
	saver := func(u config.TokenUpdates) error {
		savedUpdates = append(savedUpdates, u)
		return nil
	}
	store := newMockStateStore()

	mock := &mockEGS{
		getAuthURLFunc: func() string {
			return "https://www.epicgames.com/id/api/redirect?client_id=xyz"
		},
		authenticateWithCodeFunc: func(code string) (*rlapi.TokenResponse, error) {
			if code != "prompted-auth-code" {
				return nil, errors.New("unexpected auth code")
			}
			return &rlapi.TokenResponse{
				AccessToken:  "egs-access-token",
				RefreshToken: "new-saved-refresh-token",
				AccountID:    "epic-user-123",
				DisplayName:  "EpicPlayer",
				ExpiresIn:    3600,
			}, nil
		},
		getExchangeCodeFunc: func(accessToken string) (string, error) {
			return "ex-code-123", nil
		},
		exchangeEOSTokenFunc: func(exchangeCode string) (*rlapi.EOSTokenResponse, error) {
			return &rlapi.EOSTokenResponse{
				AccessToken: "eos-access-123",
				AccountID:   "epic-user-123",
				ExpiresIn:   3600,
			}, nil
		},
	}

	p, err := NewEpicProvider(
		config.EpicConfig{},
		store,
		WithEGSClient(mock),
		WithCodePrompter(prompter),
		WithConfigTokenSaver(saver),
		WithAccountRole(RolePrimary),
	)
	if err != nil {
		t.Fatalf("failed to create epic provider: %v", err)
	}

	// Validate should pass because prompter is present
	if err := p.Validate(); err != nil {
		t.Fatalf("expected Validate to pass with prompter: %v", err)
	}

	info, err := p.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("unexpected authenticate error: %v", err)
	}

	// Verify prompter was called with Primary Account label and auth URL
	if len(prompter.promptCalls) != 1 {
		t.Fatalf("expected 1 prompt call, got %d", len(prompter.promptCalls))
	}
	call := prompter.promptCalls[0]
	if call.AccountLabel != "Primary Account" {
		t.Errorf("expected AccountLabel 'Primary Account', got %q", call.AccountLabel)
	}
	if !strings.Contains(call.AuthURL, "client_id=xyz") {
		t.Errorf("expected AuthURL to contain client_id=xyz, got %q", call.AuthURL)
	}

	// Verify token was saved to config
	if len(savedUpdates) != 1 {
		t.Fatalf("expected 1 config save update, got %d", len(savedUpdates))
	}
	if savedUpdates[0].Primary.EpicRefreshToken == nil || *savedUpdates[0].Primary.EpicRefreshToken != "new-saved-refresh-token" {
		t.Errorf("expected Primary.EpicRefreshToken to be saved as 'new-saved-refresh-token'")
	}

	// Verify token was saved to store under "epic"
	stored, _, _, err := store.GetAuthState(context.Background(), "epic")
	if err != nil || stored != "new-saved-refresh-token" {
		t.Errorf("expected store to contain 'new-saved-refresh-token', got %q, err: %v", stored, err)
	}

	if info.AccessToken != "eos-access-123" {
		t.Errorf("expected AccessToken 'eos-access-123', got %q", info.AccessToken)
	}
}

func TestEpic_InteractivePrompt_SecondaryPollingAccount(t *testing.T) {
	prompter := &mockPrompter{returnCode: "polling-auth-code"}
	var savedUpdates []config.TokenUpdates
	saver := func(u config.TokenUpdates) error {
		savedUpdates = append(savedUpdates, u)
		return nil
	}
	store := newMockStateStore()

	mock := &mockEGS{
		getAuthURLFunc: func() string {
			return "https://www.epicgames.com/id/api/redirect"
		},
		authenticateWithCodeFunc: func(code string) (*rlapi.TokenResponse, error) {
			return &rlapi.TokenResponse{
				AccessToken:  "polling-egs-token",
				RefreshToken: "polling-refresh-token",
				AccountID:    "polling-account-id",
				DisplayName:  "PollingAlt",
				ExpiresIn:    3600,
			}, nil
		},
		getExchangeCodeFunc: func(accessToken string) (string, error) {
			return "ex-code-polling", nil
		},
		exchangeEOSTokenFunc: func(exchangeCode string) (*rlapi.EOSTokenResponse, error) {
			return &rlapi.EOSTokenResponse{
				AccessToken: "eos-polling-token",
				ExpiresIn:   3600,
			}, nil
		},
	}

	p, err := NewEpicProvider(
		config.EpicConfig{},
		store,
		WithEGSClient(mock),
		WithCodePrompter(prompter),
		WithConfigTokenSaver(saver),
		WithAccountRole(RolePolling),
	)
	if err != nil {
		t.Fatalf("failed to create epic polling provider: %v", err)
	}

	info, err := p.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("unexpected authenticate error: %v", err)
	}

	// Check Secondary Polling Account label
	if len(prompter.promptCalls) != 1 {
		t.Fatalf("expected 1 prompt call, got %d", len(prompter.promptCalls))
	}
	if prompter.promptCalls[0].AccountLabel != "Secondary Polling Account" {
		t.Errorf("expected AccountLabel 'Secondary Polling Account', got %q", prompter.promptCalls[0].AccountLabel)
	}

	// Verify token was saved to Polling field in config
	if len(savedUpdates) != 1 {
		t.Fatalf("expected 1 config save update, got %d", len(savedUpdates))
	}
	if savedUpdates[0].Polling.EpicRefreshToken == nil || *savedUpdates[0].Polling.EpicRefreshToken != "polling-refresh-token" {
		t.Errorf("expected Polling.EpicRefreshToken to be saved")
	}

	// Verify token was saved to store under "epic_polling"
	stored, _, _, err := store.GetAuthState(context.Background(), "epic_polling")
	if err != nil || stored != "polling-refresh-token" {
		t.Errorf("expected store key 'epic_polling' to contain token, got %q, err: %v", stored, err)
	}

	// Verify store key "epic" remains untouched
	_, _, _, err = store.GetAuthState(context.Background(), "epic")
	if !errors.Is(err, storage.ErrAuthStateNotFound) {
		t.Errorf("expected primary 'epic' key to not exist in store, got err: %v", err)
	}

	if info.AccessToken != "eos-polling-token" {
		t.Errorf("expected AccessToken 'eos-polling-token', got %q", info.AccessToken)
	}
}

func TestEpic_InteractivePrompt_OnExpiredRefreshTokenDuringAuthenticate(t *testing.T) {
	prompter := &mockPrompter{returnCode: "recovered-auth-code"}
	var savedUpdates []config.TokenUpdates
	saver := func(u config.TokenUpdates) error {
		savedUpdates = append(savedUpdates, u)
		return nil
	}

	mock := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			return nil, errors.New("HTTP 400: refresh_token_invalid")
		},
		authenticateWithCodeFunc: func(code string) (*rlapi.TokenResponse, error) {
			if code != "recovered-auth-code" {
				return nil, errors.New("wrong code")
			}
			return &rlapi.TokenResponse{
				AccessToken:  "new-access",
				RefreshToken: "new-refresh-token",
				AccountID:    "acc-recovered",
				ExpiresIn:    3600,
			}, nil
		},
		getExchangeCodeFunc: func(at string) (string, error) {
			return "ex-code", nil
		},
		exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
			return &rlapi.EOSTokenResponse{
				AccessToken: "eos-recovered",
				ExpiresIn:   3600,
			}, nil
		},
	}

	p, err := NewEpicProvider(
		config.EpicConfig{RefreshToken: "expired-configured-token"},
		nil,
		WithEGSClient(mock),
		WithCodePrompter(prompter),
		WithConfigTokenSaver(saver),
	)
	if err != nil {
		t.Fatalf("failed to create epic provider: %v", err)
	}

	info, err := p.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("authenticate failed: %v", err)
	}

	if len(prompter.promptCalls) != 1 {
		t.Fatalf("expected fallback prompt when refresh token failed")
	}
	if info.AccessToken != "eos-recovered" {
		t.Errorf("expected 'eos-recovered', got %q", info.AccessToken)
	}
	if len(savedUpdates) != 1 || *savedUpdates[0].Primary.EpicRefreshToken != "new-refresh-token" {
		t.Errorf("expected new refresh token to be auto-saved")
	}
}

// ============================================================================
// Steam Automated Generation & Silent Background Reauth Tests
// ============================================================================

func TestSteam_AutomatedGeneration_OnMissingTicket(t *testing.T) {
	generator := &mockSteamGenerator{
		returnResult: &SteamTicketResult{
			Success:       true,
			SessionTicket: "01000000AABBCCDD",
			SteamID64:     "76561198000000001",
			LoginKey:      "persisted-login-key-1",
			AccountName:   "SteamGamer",
		},
	}
	var savedUpdates []config.TokenUpdates
	saver := func(u config.TokenUpdates) error {
		savedUpdates = append(savedUpdates, u)
		return nil
	}
	store := newMockStateStore()

	mock := &mockEGS{
		exchangeEOSTokenFromSteamFunc: func(steamTicket string) (*rlapi.EOSTokenResponse, error) {
			if steamTicket != "01000000AABBCCDD" {
				return nil, errors.New("wrong session ticket")
			}
			return &rlapi.EOSTokenResponse{
				AccessToken:  "eos-steam-access",
				RefreshToken: "eos-steam-refresh",
				AccountID:    "epic-linked-account",
				ExpiresIn:    3600,
			}, nil
		},
	}

	p, err := NewSteamProvider(
		config.SteamConfig{
			Username: "gamer_john",
			Password: "supersecretpassword",
		},
		store,
		WithEGSClient(mock),
		WithSteamGenerator(generator),
		WithConfigTokenSaver(saver),
		WithAccountRole(RolePrimary),
	)
	if err != nil {
		t.Fatalf("failed to create steam provider: %v", err)
	}

	// Validate should pass because username and password are provided
	if err := p.Validate(); err != nil {
		t.Fatalf("expected Validate to pass with credentials: %v", err)
	}

	info, err := p.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("unexpected authenticate error: %v", err)
	}

	// Check generator was invoked
	if len(generator.calls) != 1 {
		t.Fatalf("expected 1 generator call, got %d", len(generator.calls))
	}
	call := generator.calls[0]
	if call.Username != "gamer_john" || call.Password != "supersecretpassword" {
		t.Errorf("wrong credentials passed to generator: %+v", call)
	}
	if call.AccountLabel != "Primary Account" {
		t.Errorf("expected AccountLabel 'Primary Account', got %q", call.AccountLabel)
	}

	// Verify updates saved to config: ticket, steamID, loginKey
	if len(savedUpdates) != 1 {
		t.Fatalf("expected 1 config update, got %d", len(savedUpdates))
	}
	up := savedUpdates[0].Primary
	if up.SteamSessionTicket == nil || *up.SteamSessionTicket != "01000000AABBCCDD" {
		t.Errorf("ticket not saved correctly")
	}
	if up.SteamID64 == nil || *up.SteamID64 != "76561198000000001" {
		t.Errorf("steamID not saved correctly")
	}
	if up.SteamLoginKey == nil || *up.SteamLoginKey != "persisted-login-key-1" {
		t.Errorf("loginKey not saved correctly")
	}

	// Verify store has auth state under "steam"
	storedTicket, storedSteamID, storedName, err := store.GetAuthState(context.Background(), "steam")
	if err != nil || storedTicket != "01000000AABBCCDD" || storedSteamID != "76561198000000001" || storedName != "SteamGamer" {
		t.Errorf("store did not record steam state correctly: ticket=%q, steamID=%q, name=%q, err=%v",
			storedTicket, storedSteamID, storedName, err)
	}

	if info.AccessToken != "eos-steam-access" {
		t.Errorf("expected AccessToken 'eos-steam-access', got %q", info.AccessToken)
	}
}

func TestSteam_SilentBackgroundReauth_UsingLoginKey(t *testing.T) {
	generator := &mockSteamGenerator{
		returnResult: &SteamTicketResult{
			Success:       true,
			SessionTicket: "01000000RENEWEDTICKET",
			SteamID64:     "76561198000000002",
			LoginKey:      "updated-rotated-login-key",
			AccountName:   "SilentGamer",
		},
	}
	var savedUpdates []config.TokenUpdates
	saver := func(u config.TokenUpdates) error {
		savedUpdates = append(savedUpdates, u)
		return nil
	}

	mock := &mockEGS{
		// Simulate EOS token refresh failing (EOS refresh expired after hours/days)
		refreshEOSTokenFunc: func(rt string) (*rlapi.EOSTokenResponse, error) {
			return nil, errors.New("HTTP 401: eos_token_expired")
		},
		// Simulate exchange succeeding with new session ticket
		exchangeEOSTokenFromSteamFunc: func(ticket string) (*rlapi.EOSTokenResponse, error) {
			if ticket != "01000000RENEWEDTICKET" {
				return nil, errors.New("invalid renewed ticket")
			}
			return &rlapi.EOSTokenResponse{
				AccessToken:  "fresh-eos-access",
				RefreshToken: "fresh-eos-refresh",
				AccountID:    "epic-linked-id",
				ExpiresIn:    3600,
			}, nil
		},
	}

	p, err := NewSteamProvider(
		config.SteamConfig{
			Username:      "silent_user",
			LoginKey:      "cached-login-key-123",
			SteamID64:     "76561198000000002",
			SessionTicket: "01000000OLDTICKET",
		},
		nil,
		WithEGSClient(mock),
		WithSteamGenerator(generator),
		WithConfigTokenSaver(saver),
		WithAccountRole(RolePolling),
	)
	if err != nil {
		t.Fatalf("failed to create steam provider: %v", err)
	}

	// Trigger Refresh: EOS refresh fails -> background generator runs with loginKey
	info, err := p.Refresh(context.Background(), "expired-eos-refresh-token")
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	// Verify generator was called with loginKey
	if len(generator.calls) != 1 {
		t.Fatalf("expected 1 generator call, got %d", len(generator.calls))
	}
	call := generator.calls[0]
	if call.LoginKey != "cached-login-key-123" {
		t.Errorf("expected LoginKey 'cached-login-key-123', got %q", call.LoginKey)
	}
	if call.AccountLabel != "Secondary Polling Account" {
		t.Errorf("expected AccountLabel 'Secondary Polling Account', got %q", call.AccountLabel)
	}

	// Verify renewed ticket and new loginKey were saved to config under Polling
	if len(savedUpdates) != 1 {
		t.Fatalf("expected 1 config update, got %d", len(savedUpdates))
	}
	up := savedUpdates[0].Polling
	if up.SteamSessionTicket == nil || *up.SteamSessionTicket != "01000000RENEWEDTICKET" {
		t.Errorf("expected renewed ticket to be saved to config")
	}
	if up.SteamLoginKey == nil || *up.SteamLoginKey != "updated-rotated-login-key" {
		t.Errorf("expected updated login key to be saved to config")
	}

	if info.AccessToken != "fresh-eos-access" {
		t.Errorf("expected AccessToken 'fresh-eos-access', got %q", info.AccessToken)
	}
}
