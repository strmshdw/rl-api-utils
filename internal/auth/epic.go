package auth

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rlapi"
)

// EpicAuthProvider implements AuthProvider for Epic Games Store accounts.
type EpicAuthProvider struct {
	cfg         config.EpicConfig // immutable after construction
	store       storage.StateStore
	egsClient   EGSClient
	clock       func() time.Time
	accountRole AccountRole
	prompter    CodePrompter
	tokenSaver  ConfigTokenSaver
	logger      *slog.Logger

	refreshMu sync.Mutex
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
	clock := options.clock
	if clock == nil {
		clock = time.Now
	}

	return &EpicAuthProvider{
		cfg:         cfg,
		store:       options.store,
		egsClient:   options.egsClient,
		clock:       clock,
		accountRole: role,
		prompter:    options.prompter,
		tokenSaver:  options.tokenSaver,
		logger:      options.logger,
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

// TokenInfo returns a safe copy of the current cached TokenInfo, or nil if not authenticated.
func (p *EpicAuthProvider) TokenInfo() *TokenInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.tokenInfo.clone()
}

func (p *EpicAuthProvider) validCachedToken() *TokenInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.tokenInfo != nil && !p.tokenInfo.IsExpired() {
		return p.tokenInfo.clone()
	}
	return nil
}

// Authenticate executes the full Epic Games OAuth and EOS token exchange sequence.
// Concurrent callers are serialized via refreshMu with double-checked locking so parallel
// callers reuse the refreshed token instead of triggering competing refresh token rotations.
func (p *EpicAuthProvider) Authenticate(ctx context.Context) (*TokenInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cached := p.validCachedToken(); cached != nil {
		return cached, nil
	}

	p.refreshMu.Lock()
	defer p.refreshMu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cached := p.validCachedToken(); cached != nil {
		return cached, nil
	}

	return p.authenticateLocked(ctx)
}

func (p *EpicAuthProvider) authenticateLocked(ctx context.Context) (*TokenInfo, error) {
	storeKey := p.accountRole.StoreKey("epic")
	refreshCandidates := p.collectRefreshCandidates(ctx, storeKey)
	authCode := strings.TrimSpace(p.cfg.AuthCode)

	var tokenResp *rlapi.TokenResponse
	var usedRefreshToken string
	var oauthErr error

	// 1. Prefer refresh tokens (cached -> StateStore -> config) even when a stale auth_code is present.
	for _, rt := range refreshCandidates {
		resp, err := p.egsClient.AuthenticateWithRefreshToken(rt)
		if err == nil && resp != nil {
			tokenResp = resp
			usedRefreshToken = rt
			oauthErr = nil
			break
		}
		if err != nil {
			oauthErr = err
		}
	}

	// 2. Fallback to one-time authorization code if refresh tokens were absent or rejected.
	if tokenResp == nil && authCode != "" {
		resp, err := p.egsClient.AuthenticateWithCode(authCode)
		if err == nil && resp != nil {
			tokenResp = resp
			oauthErr = nil
		} else if err != nil {
			oauthErr = err
		}
	}

	// 3. Fallback to interactive CodePrompter if available.
	if tokenResp == nil {
		if p.prompter != nil {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			authURL := p.egsClient.GetAuthURL()
			code, promptErr := p.prompter.PromptForCode(ctx, p.accountRole.Label(), authURL)
			if promptErr != nil {
				return nil, fmt.Errorf("%w: interactive login prompt failed: %w", ErrAuthFailed, promptErr)
			}
			trimmedCode := strings.TrimSpace(code)
			if trimmedCode == "" {
				return nil, fmt.Errorf("%w: authorization code cannot be empty", ErrAuthFailed)
			}
			resp, err := p.egsClient.AuthenticateWithCode(trimmedCode)
			if err != nil {
				return nil, fmt.Errorf("%w: epic oauth failed: %v", ErrAuthFailed, err)
			}
			tokenResp = resp
			oauthErr = nil
		} else if len(refreshCandidates) == 0 && authCode == "" {
			return nil, fmt.Errorf("%w: epic provider requires either 'refresh_token' or 'auth_code'", ErrMissingCredentials)
		}
	}

	if oauthErr != nil {
		return nil, fmt.Errorf("%w: epic oauth failed: %v", ErrAuthFailed, oauthErr)
	}
	if tokenResp == nil {
		return nil, fmt.Errorf("%w: received nil token response from epic", ErrAuthFailed)
	}

	return p.exchangeAndPersistLocked(ctx, storeKey, usedRefreshToken, tokenResp)
}

// Refresh renews the EOS session token using the provided, cached, stored, or configured refresh token.
// All refresh calls are serialized via refreshMu so parallel refreshes cannot invalidate each other.
func (p *EpicAuthProvider) Refresh(ctx context.Context, refreshToken string) (*TokenInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	p.refreshMu.Lock()
	defer p.refreshMu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	storeKey := p.accountRole.StoreKey("epic")
	tokenToUse := p.resolveTokenForRefresh(ctx, storeKey, refreshToken)

	var tokenResp *rlapi.TokenResponse
	var err error

	if tokenToUse != "" {
		tokenResp, err = p.egsClient.AuthenticateWithRefreshToken(tokenToUse)
	}

	if tokenToUse == "" || err != nil {
		if p.prompter != nil {
			authURL := p.egsClient.GetAuthURL()
			code, promptErr := p.prompter.PromptForCode(ctx, p.accountRole.Label(), authURL)
			if promptErr != nil {
				if err != nil {
					return nil, fmt.Errorf("%w: egs refresh failed: %v", ErrRefreshFailed, err)
				}
				return nil, fmt.Errorf("%w: interactive prompt failed: %w", ErrRefreshFailed, promptErr)
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

	return p.exchangeAndPersistLocked(ctx, storeKey, tokenToUse, tokenResp)
}

func (p *EpicAuthProvider) collectRefreshCandidates(ctx context.Context, storeKey string) []string {
	var candidates []string
	seen := make(map[string]struct{}, 3)
	add := func(tok string) {
		trimmed := strings.TrimSpace(tok)
		if trimmed == "" {
			return
		}
		if _, exists := seen[trimmed]; exists {
			return
		}
		seen[trimmed] = struct{}{}
		candidates = append(candidates, trimmed)
	}

	p.mu.RLock()
	if p.tokenInfo != nil {
		add(p.tokenInfo.RefreshToken)
	}
	p.mu.RUnlock()

	if p.store != nil {
		if storedToken, _, _, err := p.store.GetAuthState(ctx, storeKey); err == nil {
			add(storedToken)
		}
	}

	add(p.cfg.RefreshToken)
	return candidates
}

func (p *EpicAuthProvider) resolveTokenForRefresh(ctx context.Context, storeKey, explicit string) string {
	if trimmed := strings.TrimSpace(explicit); trimmed != "" {
		return trimmed
	}

	p.mu.RLock()
	var cached string
	if p.tokenInfo != nil {
		cached = strings.TrimSpace(p.tokenInfo.RefreshToken)
	}
	p.mu.RUnlock()
	if cached != "" {
		return cached
	}

	if p.store != nil {
		if storedToken, _, _, err := p.store.GetAuthState(ctx, storeKey); err == nil {
			if trimmed := strings.TrimSpace(storedToken); trimmed != "" {
				return trimmed
			}
		}
	}

	return strings.TrimSpace(p.cfg.RefreshToken)
}

func (p *EpicAuthProvider) exchangeAndPersistLocked(
	ctx context.Context,
	storeKey string,
	usedRefreshToken string,
	tokenResp *rlapi.TokenResponse,
) (*TokenInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	exchangeCode, err := p.egsClient.GetExchangeCode(tokenResp.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to obtain exchange code: %v", ErrExchangeFailed, err)
	}

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

	now := p.clock()
	expiresAt := computeTokenExpiry(now, eosResp.ExpiresIn)

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
		newRefreshToken = usedRefreshToken
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
		nowFn:         p.clock,
	}

	// Persist under refreshMu so StateStore, ConfigTokenSaver, and cached TokenInfo stay consistent.
	if p.store != nil && newRefreshToken != "" {
		if err := p.store.SaveAuthState(ctx, storeKey, newRefreshToken, accountID, displayName); err != nil {
			logPersistError(p.logger, "epic auth state persistence failed", err)
		}
	}
	if p.tokenSaver != nil && newRefreshToken != "" {
		if err := p.tokenSaver(BuildEpicTokenUpdate(p.accountRole, newRefreshToken)); err != nil {
			logPersistError(p.logger, "epic config token save failed", err)
		}
	}

	p.mu.Lock()
	p.tokenInfo = info
	cp := info.clone()
	p.mu.Unlock()

	return cp, nil
}
