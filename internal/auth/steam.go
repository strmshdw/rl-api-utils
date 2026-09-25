package auth

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/storage"
)

// SteamAuthProvider implements AuthProvider for Steam platform accounts.
type SteamAuthProvider struct {
	cfg       config.SteamConfig
	store     storage.StateStore
	egsClient EGSClient
	clock     func() time.Time

	mu        sync.RWMutex
	tokenInfo *TokenInfo
}

// NewSteamProvider creates a new SteamAuthProvider with the given configuration and options.
func NewSteamProvider(cfg config.SteamConfig, store storage.StateStore, opts ...Option) (*SteamAuthProvider, error) {
	options := defaultOptions()
	options.store = store
	for _, opt := range opts {
		opt(options)
	}

	return &SteamAuthProvider{
		cfg:       cfg,
		store:     options.store,
		egsClient: options.egsClient,
		clock:     options.clock,
	}, nil
}

// Name returns the provider identifier.
func (p *SteamAuthProvider) Name() string {
	return "steam"
}

// Validate checks whether configuration contains valid Steam credentials and identifier.
func (p *SteamAuthProvider) Validate() error {
	ticket := strings.TrimSpace(p.cfg.SessionTicket)
	if ticket == "" {
		return fmt.Errorf("%w: steam provider requires 'session_ticket'", ErrMissingCredentials)
	}

	steamID := strings.TrimSpace(p.cfg.SteamID64)
	if steamID == "" {
		return fmt.Errorf("%w: steam provider requires 'steam_id_64'", ErrMissingCredentials)
	}

	if err := ValidateSteamID64(steamID); err != nil {
		return err
	}

	return nil
}

// ValidateSteamID64 validates that the given ID is a valid 17-digit SteamID64 starting with 7656119.
func ValidateSteamID64(id string) error {
	if len(id) != 17 || !strings.HasPrefix(id, "7656119") {
		return fmt.Errorf("%w: must be a 17-digit number starting with '7656119', got %q", ErrInvalidSteamID, id)
	}
	if _, err := strconv.ParseUint(id, 10, 64); err != nil {
		return fmt.Errorf("%w: not a valid unsigned integer: %v", ErrInvalidSteamID, err)
	}
	return nil
}

// Authenticate exchanges a Steam session ticket for a Rocket League scoped EOS token.
func (p *SteamAuthProvider) Authenticate(ctx context.Context) (*TokenInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := p.Validate(); err != nil {
		return nil, err
	}

	ticket := strings.TrimSpace(p.cfg.SessionTicket)
	steamID := strings.TrimSpace(p.cfg.SteamID64)
	accountName := strings.TrimSpace(p.cfg.AccountName)

	eosResp, err := p.egsClient.ExchangeEOSTokenFromSteam(ticket)
	if err != nil {
		return nil, fmt.Errorf("%w: steam ticket exchange failed: %v", ErrExchangeFailed, err)
	}
	if eosResp == nil {
		return nil, fmt.Errorf("%w: received nil EOS token response from steam ticket exchange", ErrExchangeFailed)
	}

	now := p.clock()
	expiresIn := eosResp.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	expiresAt := now.Add(time.Duration(expiresIn) * time.Second)

	info := &TokenInfo{
		Provider:      "steam",
		AccessToken:   eosResp.AccessToken,
		RefreshToken:  eosResp.RefreshToken,
		AccountID:     steamID,
		EpicAccountID: eosResp.AccountID,
		DisplayName:   accountName,
		TokenType:     eosResp.TokenType,
		ExpiresAt:     expiresAt,
		RawEOS:        eosResp,
	}

	if p.store != nil {
		_ = p.store.SaveAuthState(ctx, "steam", ticket, steamID, accountName)
	}

	p.mu.Lock()
	p.tokenInfo = info
	p.mu.Unlock()

	return info, nil
}

// Refresh attempts to refresh the EOS session token using the EOS refresh token.
func (p *SteamAuthProvider) Refresh(ctx context.Context, refreshToken string) (*TokenInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	tokenToUse := strings.TrimSpace(refreshToken)
	if tokenToUse == "" {
		p.mu.RLock()
		if p.tokenInfo != nil && p.tokenInfo.RefreshToken != "" {
			tokenToUse = p.tokenInfo.RefreshToken
		}
		p.mu.RUnlock()
	}

	if tokenToUse != "" {
		eosResp, err := p.egsClient.RefreshEOSToken(tokenToUse)
		if err == nil && eosResp != nil {
			now := p.clock()
			expiresIn := eosResp.ExpiresIn
			if expiresIn <= 0 {
				expiresIn = 3600
			}
			expiresAt := now.Add(time.Duration(expiresIn) * time.Second)

			p.mu.Lock()
			if p.tokenInfo != nil {
				p.tokenInfo.AccessToken = eosResp.AccessToken
				if eosResp.RefreshToken != "" {
					p.tokenInfo.RefreshToken = eosResp.RefreshToken
				}
				p.tokenInfo.ExpiresAt = expiresAt
				p.tokenInfo.RawEOS = eosResp
			} else {
				p.tokenInfo = &TokenInfo{
					Provider:     "steam",
					AccessToken:  eosResp.AccessToken,
					RefreshToken: eosResp.RefreshToken,
					AccountID:    p.cfg.SteamID64,
					DisplayName:  p.cfg.AccountName,
					TokenType:    eosResp.TokenType,
					ExpiresAt:    expiresAt,
					RawEOS:       eosResp,
				}
			}
			cp := *p.tokenInfo
			p.mu.Unlock()
			return &cp, nil
		}
	}

	return nil, fmt.Errorf("%w: steam session ticket cannot be automatically renewed, a fresh ticket is required", ErrRefreshFailed)
}

// TokenInfo returns the current cached TokenInfo, or nil if not authenticated.
func (p *SteamAuthProvider) TokenInfo() *TokenInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.tokenInfo == nil {
		return nil
	}
	cp := *p.tokenInfo
	return &cp
}
