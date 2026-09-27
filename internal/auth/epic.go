package auth

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rlapi"
)

// EpicAuthProvider implements AuthProvider for Epic Games Store accounts.
type EpicAuthProvider struct {
	cfg         config.EpicConfig
	store       storage.StateStore
	egsClient   EGSClient
	clock       func() time.Time
	accountRole AccountRole
	prompter    CodePrompter
	tokenSaver  ConfigTokenSaver

	mu        sync.RWMutex
	tokenInfo *TokenInfo
}

// NewEpicProvider creates a new EpicAuthProvider with the given configuration and options.
func NewEpicProvider(cfg config.EpicConfig, store storage.StateStore, opts ...Option) (*EpicAuthProvider, error) {
	options := defaultOptions()
	options.store = store
	for _, opt := range opts {
		opt(options)
	}

	role := options.role
	if role == "" {
		role = RolePrimary
	}

	return &EpicAuthProvider{
		cfg:         cfg,
		store:       options.store,
		egsClient:   options.egsClient,
		clock:       options.clock,
		accountRole: role,
		prompter:    options.prompter,
		tokenSaver:  options.tokenSaver,
	}, nil
}

// Name returns the provider identifier.
func (p *EpicAuthProvider) Name() string {
	return "epic"
}

// Role returns the account role (RolePrimary or RolePolling).
func (p *EpicAuthProvider) Role() AccountRole {
	return p.accountRole
}

// Validate checks whether configuration contains the required Epic credentials.
// When an interactive CodePrompter is configured, missing initial credentials
// are permitted since the provider will initiate the interactive login flow.
func (p *EpicAuthProvider) Validate() error {
	if strings.TrimSpace(p.cfg.RefreshToken) != "" || strings.TrimSpace(p.cfg.AuthCode) != "" {
		return nil
	}
	if p.prompter != nil {
		return nil
	}
	return fmt.Errorf("%w: epic provider requires either 'refresh_token' or 'auth_code'", ErrMissingCredentials)
}

// Authenticate executes the full Epic Games OAuth and EOS token exchange sequence.
// If refresh tokens are missing or invalid, it triggers interactive authorization if a prompter is available.
func (p *EpicAuthProvider) Authenticate(ctx context.Context) (*TokenInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 1. Resolve credentials: check config, then check persistent StateStore fallback
	refreshToken := strings.TrimSpace(p.cfg.RefreshToken)
	authCode := strings.TrimSpace(p.cfg.AuthCode)

	storeKey := p.accountRole.StoreKey("epic")
	if refreshToken == "" && authCode == "" && p.store != nil {
		storedToken, _, _, err := p.store.GetAuthState(ctx, storeKey)
		if err == nil && strings.TrimSpace(storedToken) != "" {
			refreshToken = strings.TrimSpace(storedToken)
		}
	}

	// If no credentials found in config or store, prompt interactively if prompter is available
	if refreshToken == "" && authCode == "" {
		if p.prompter != nil {
			authURL := p.egsClient.GetAuthURL()
			code, err := p.prompter.PromptForCode(ctx, p.accountRole.Label(), authURL)
			if err != nil {
				return nil, fmt.Errorf("%w: interactive login prompt failed: %v", ErrAuthFailed, err)
			}
			authCode = strings.TrimSpace(code)
		} else {
			return nil, fmt.Errorf("%w: epic provider requires either 'refresh_token' or 'auth_code'", ErrMissingCredentials)
		}
	}

	// 2. Perform EGS OAuth token grant
	var tokenResp *rlapi.TokenResponse
	var err error

	if refreshToken != "" {
		tokenResp, err = p.egsClient.AuthenticateWithRefreshToken(refreshToken)
		if err != nil && authCode != "" {
			// Fallback to auth code if refresh token failed but auth code is present
			tokenResp, err = p.egsClient.AuthenticateWithCode(authCode)
		} else if err != nil && p.prompter != nil {
			// Stored or configured refresh token was revoked/expired; prompt interactively
			authURL := p.egsClient.GetAuthURL()
			code, promptErr := p.prompter.PromptForCode(ctx, p.accountRole.Label(), authURL)
			if promptErr == nil && strings.TrimSpace(code) != "" {
				tokenResp, err = p.egsClient.AuthenticateWithCode(strings.TrimSpace(code))
			}
		}
	} else {
		tokenResp, err = p.egsClient.AuthenticateWithCode(authCode)
	}

	if err != nil {
		return nil, fmt.Errorf("%w: epic oauth failed: %v", ErrAuthFailed, err)
	}
	if tokenResp == nil {
		return nil, fmt.Errorf("%w: received nil token response from epic", ErrAuthFailed)
	}

	// 3. Acquire short-lived exchange code
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	exchangeCode, err := p.egsClient.GetExchangeCode(tokenResp.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to obtain exchange code: %v", ErrExchangeFailed, err)
	}

	// 4. Exchange for Rocket League scoped EOS access token
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	eosResp, err := p.egsClient.ExchangeEOSToken(exchangeCode)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to exchange EOS token: %v", ErrExchangeFailed, err)
	}
	if eosResp == nil {
		return nil, fmt.Errorf("%w: received nil EOS token response", ErrExchangeFailed)
	}

	// 5. Build TokenInfo
	now := p.clock()
	expiresIn := eosResp.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	expiresAt := now.Add(time.Duration(expiresIn) * time.Second)

	accountID := tokenResp.AccountID
	if accountID == "" {
		accountID = eosResp.AccountID
	}
	displayName := tokenResp.DisplayName
	if displayName == "" {
		displayName = p.cfg.DisplayName
	}

	newRefreshToken := tokenResp.RefreshToken
	if newRefreshToken == "" {
		newRefreshToken = refreshToken
	}

	info := &TokenInfo{
		Provider:      "epic",
		AccessToken:   eosResp.AccessToken,
		RefreshToken:  newRefreshToken,
		AccountID:     accountID,
		EpicAccountID: accountID,
		DisplayName:   displayName,
		TokenType:     eosResp.TokenType,
		ExpiresAt:     expiresAt,
		RawEOS:        eosResp,
	}

	// 6. Persist to StateStore if configured using role-isolated storeKey
	if p.store != nil && newRefreshToken != "" {
		_ = p.store.SaveAuthState(ctx, storeKey, newRefreshToken, accountID, displayName)
	}

	// 7. Auto-save tokens to configuration file if configured
	if p.tokenSaver != nil && newRefreshToken != "" {
		_ = p.tokenSaver(BuildEpicTokenUpdate(p.accountRole, newRefreshToken))
	}

	p.mu.Lock()
	p.tokenInfo = info
	p.mu.Unlock()

	return info, nil
}

// Refresh renews the EOS session token using the provided or cached refresh token.
// If the refresh token is expired or revoked, it initiates interactive prompt if a prompter is available.
func (p *EpicAuthProvider) Refresh(ctx context.Context, refreshToken string) (*TokenInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	storeKey := p.accountRole.StoreKey("epic")
	tokenToUse := strings.TrimSpace(refreshToken)
	if tokenToUse == "" {
		p.mu.RLock()
		if p.tokenInfo != nil && p.tokenInfo.RefreshToken != "" {
			tokenToUse = p.tokenInfo.RefreshToken
		}
		p.mu.RUnlock()
	}
	if tokenToUse == "" && p.store != nil {
		storedToken, _, _, err := p.store.GetAuthState(ctx, storeKey)
		if err == nil && strings.TrimSpace(storedToken) != "" {
			tokenToUse = strings.TrimSpace(storedToken)
		}
	}
	if tokenToUse == "" {
		tokenToUse = strings.TrimSpace(p.cfg.RefreshToken)
	}

	var tokenResp *rlapi.TokenResponse
	var err error

	if tokenToUse != "" {
		// 1. Authenticate with refresh token
		tokenResp, err = p.egsClient.AuthenticateWithRefreshToken(tokenToUse)
	}

	if tokenToUse == "" || err != nil {
		// If refresh failed or was empty, attempt interactive prompt if prompter is present
		if p.prompter != nil {
			authURL := p.egsClient.GetAuthURL()
			code, promptErr := p.prompter.PromptForCode(ctx, p.accountRole.Label(), authURL)
			if promptErr != nil {
				if err != nil {
					return nil, fmt.Errorf("%w: egs refresh failed: %v", ErrRefreshFailed, err)
				}
				return nil, fmt.Errorf("%w: interactive prompt failed: %v", ErrRefreshFailed, promptErr)
			}
			tokenResp, err = p.egsClient.AuthenticateWithCode(strings.TrimSpace(code))
			if err != nil {
				return nil, fmt.Errorf("%w: egs re-auth with code failed: %v", ErrRefreshFailed, err)
			}
		} else {
			if tokenToUse == "" {
				return nil, fmt.Errorf("%w: no refresh token available for epic session renewal", ErrMissingCredentials)
			}
			return nil, fmt.Errorf("%w: egs refresh failed: %v", ErrRefreshFailed, err)
		}
	}

	if tokenResp == nil {
		return nil, fmt.Errorf("%w: received nil token response during refresh", ErrRefreshFailed)
	}

	// 2. Obtain exchange code
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	exchangeCode, err := p.egsClient.GetExchangeCode(tokenResp.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to obtain exchange code during refresh: %v", ErrExchangeFailed, err)
	}

	// 3. Exchange for fresh EOS token
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	eosResp, err := p.egsClient.ExchangeEOSToken(exchangeCode)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to exchange EOS token during refresh: %v", ErrExchangeFailed, err)
	}
	if eosResp == nil {
		return nil, fmt.Errorf("%w: received nil EOS token response during refresh", ErrExchangeFailed)
	}

	now := p.clock()
	expiresIn := eosResp.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	expiresAt := now.Add(time.Duration(expiresIn) * time.Second)

	accountID := tokenResp.AccountID
	if accountID == "" {
		accountID = eosResp.AccountID
	}
	displayName := tokenResp.DisplayName
	if displayName == "" {
		displayName = p.cfg.DisplayName
	}
	newRefreshToken := tokenResp.RefreshToken
	if newRefreshToken == "" {
		newRefreshToken = tokenToUse
	}

	info := &TokenInfo{
		Provider:      "epic",
		AccessToken:   eosResp.AccessToken,
		RefreshToken:  newRefreshToken,
		AccountID:     accountID,
		EpicAccountID: accountID,
		DisplayName:   displayName,
		TokenType:     eosResp.TokenType,
		ExpiresAt:     expiresAt,
		RawEOS:        eosResp,
	}

	if p.store != nil && newRefreshToken != "" {
		_ = p.store.SaveAuthState(ctx, storeKey, newRefreshToken, accountID, displayName)
	}

	if p.tokenSaver != nil && newRefreshToken != "" {
		_ = p.tokenSaver(BuildEpicTokenUpdate(p.accountRole, newRefreshToken))
	}

	p.mu.Lock()
	p.tokenInfo = info
	p.mu.Unlock()

	return info, nil
}

// TokenInfo returns the current cached TokenInfo, or nil if not authenticated.
func (p *EpicAuthProvider) TokenInfo() *TokenInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.tokenInfo == nil {
		return nil
	}
	cp := *p.tokenInfo
	return &cp
}
