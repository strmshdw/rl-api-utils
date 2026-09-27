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
	cfg         config.SteamConfig
	store       storage.StateStore
	egsClient   EGSClient
	clock       func() time.Time
	accountRole AccountRole
	generator   SteamTicketGenerator
	tokenSaver  ConfigTokenSaver

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

	role := options.role
	if role == "" {
		role = RolePrimary
	}

	return &SteamAuthProvider{
		cfg:         cfg,
		store:       options.store,
		egsClient:   options.egsClient,
		clock:       options.clock,
		accountRole: role,
		generator:   options.steamGen,
		tokenSaver:  options.tokenSaver,
	}, nil
}

// Name returns the provider identifier.
func (p *SteamAuthProvider) Name() string {
	return "steam"
}

// Role returns the account role (RolePrimary or RolePolling).
func (p *SteamAuthProvider) Role() AccountRole {
	return p.accountRole
}

// Validate checks whether configuration contains valid Steam credentials or login information.
func (p *SteamAuthProvider) Validate() error {
	ticket := strings.TrimSpace(p.cfg.SessionTicket)
	if ticket == "" {
		if strings.TrimSpace(p.cfg.Username) != "" &&
			(strings.TrimSpace(p.cfg.Password) != "" || strings.TrimSpace(p.cfg.LoginKey) != "") {
			return nil
		}
		return fmt.Errorf("%w: steam provider requires 'session_ticket' or credentials ('username' and 'password'/'login_key')", ErrMissingCredentials)
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
// If the session ticket is missing or expired, it automatically generates a fresh ticket
// using the configured Steam credentials (or login key).
func (p *SteamAuthProvider) Authenticate(ctx context.Context) (*TokenInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	storeKey := p.accountRole.StoreKey("steam")
	ticket := strings.TrimSpace(p.cfg.SessionTicket)
	steamID := strings.TrimSpace(p.cfg.SteamID64)
	accountName := strings.TrimSpace(p.cfg.AccountName)
	username := strings.TrimSpace(p.cfg.Username)
	password := strings.TrimSpace(p.cfg.Password)
	loginKey := strings.TrimSpace(p.cfg.LoginKey)

	// Check persistent StateStore fallback if ticket is missing
	if (ticket == "" || steamID == "") && p.store != nil {
		storedTicket, storedSteamID, storedName, err := p.store.GetAuthState(ctx, storeKey)
		if err == nil && strings.TrimSpace(storedTicket) != "" {
			ticket = strings.TrimSpace(storedTicket)
			if steamID == "" {
				steamID = strings.TrimSpace(storedSteamID)
			}
			if accountName == "" {
				accountName = strings.TrimSpace(storedName)
			}
		}
	}

	// Helper function to generate ticket and persist tokens
	generateAndSave := func() (*SteamTicketResult, error) {
		if p.generator == nil {
			return nil, fmt.Errorf("%w: steam provider requires session ticket or ticket generator", ErrMissingCredentials)
		}
		if username == "" || (password == "" && loginKey == "") {
			return nil, fmt.Errorf("%w: steam generator requires username and password or login_key", ErrMissingCredentials)
		}
		res, err := p.generator.GenerateTicket(ctx, SteamTicketOptions{
			Username:     username,
			Password:     password,
			LoginKey:     loginKey,
			AccountLabel: p.accountRole.Label(),
		})
		if err != nil {
			return nil, fmt.Errorf("generating steam ticket: %w", err)
		}
		ticket = res.SessionTicket
		steamID = res.SteamID64
		if res.AccountName != "" {
			accountName = res.AccountName
		}
		if res.LoginKey != "" {
			loginKey = res.LoginKey
			p.cfg.LoginKey = res.LoginKey
		}
		if p.tokenSaver != nil {
			_ = p.tokenSaver(BuildSteamTokenUpdate(p.accountRole, ticket, steamID, loginKey))
		}
		return res, nil
	}

	if ticket == "" || steamID == "" {
		if _, err := generateAndSave(); err != nil {
			return nil, err
		}
	}

	if err := ValidateSteamID64(steamID); err != nil {
		return nil, err
	}

	// Exchange ticket for EOS token
	eosResp, err := p.egsClient.ExchangeEOSTokenFromSteam(ticket)
	if err != nil {
		// Ticket may be expired/invalidated; attempt automated ticket generation if credentials exist
		if p.generator != nil && username != "" && (password != "" || loginKey != "") {
			if _, genErr := generateAndSave(); genErr == nil {
				eosResp, err = p.egsClient.ExchangeEOSTokenFromSteam(ticket)
			}
		}
	}

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
		_ = p.store.SaveAuthState(ctx, storeKey, ticket, steamID, accountName)
	}

	p.mu.Lock()
	p.tokenInfo = info
	p.mu.Unlock()

	return info, nil
}

// Refresh attempts to refresh the EOS session token using the EOS refresh token.
// If the EOS refresh fails, it attempts automated ticket regeneration using the stored loginKey.
func (p *SteamAuthProvider) Refresh(ctx context.Context, refreshToken string) (*TokenInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	storeKey := p.accountRole.StoreKey("steam")
	tokenToUse := strings.TrimSpace(refreshToken)
	if tokenToUse == "" {
		p.mu.RLock()
		if p.tokenInfo != nil && p.tokenInfo.RefreshToken != "" {
			tokenToUse = p.tokenInfo.RefreshToken
		}
		p.mu.RUnlock()
	}

	// 1. First attempt: refresh existing EOS token
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

	// 2. Second attempt: generate fresh ticket via steam-user generator using loginKey or password
	username := strings.TrimSpace(p.cfg.Username)
	password := strings.TrimSpace(p.cfg.Password)
	loginKey := strings.TrimSpace(p.cfg.LoginKey)

	if p.generator != nil && username != "" && (password != "" || loginKey != "") {
		res, err := p.generator.GenerateTicket(ctx, SteamTicketOptions{
			Username:     username,
			Password:     password,
			LoginKey:     loginKey,
			AccountLabel: p.accountRole.Label(),
		})
		if err == nil && res != nil && res.SessionTicket != "" {
			ticket := res.SessionTicket
			steamID := res.SteamID64
			accountName := p.cfg.AccountName
			if res.AccountName != "" {
				accountName = res.AccountName
			}
			if res.LoginKey != "" {
				loginKey = res.LoginKey
				p.cfg.LoginKey = res.LoginKey
			}

			// Auto-save to config.yaml
			if p.tokenSaver != nil {
				_ = p.tokenSaver(BuildSteamTokenUpdate(p.accountRole, ticket, steamID, loginKey))
			}

			// Exchange new ticket for EOS token
			eosResp, exErr := p.egsClient.ExchangeEOSTokenFromSteam(ticket)
			if exErr == nil && eosResp != nil {
				if p.store != nil {
					_ = p.store.SaveAuthState(ctx, storeKey, ticket, steamID, accountName)
				}

				now := p.clock()
				expiresIn := eosResp.ExpiresIn
				if expiresIn <= 0 {
					expiresIn = 3600
				}
				expiresAt := now.Add(time.Duration(expiresIn) * time.Second)

				p.mu.Lock()
				p.tokenInfo = &TokenInfo{
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
				cp := *p.tokenInfo
				p.mu.Unlock()
				return &cp, nil
			}
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
