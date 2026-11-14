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
	"github.com/dank/rlapi"
)

// SteamAuthProvider implements AuthProvider for Steam platform accounts.
type SteamAuthProvider struct {
	cfg         config.SteamConfig // immutable after construction
	store       storage.StateStore
	egsClient   EGSClient
	clock       func() time.Time
	accountRole AccountRole
	generator   SteamTicketGenerator
	tokenSaver  ConfigTokenSaver

	refreshMu     sync.Mutex
	mu            sync.RWMutex
	loginKey      string
	sessionTicket string
	steamID64     string
	accountName   string
	epicAccountID string
	tokenInfo     *TokenInfo
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
	clock := options.clock
	if clock == nil {
		clock = time.Now
	}

	return &SteamAuthProvider{
		cfg:           cfg,
		store:         options.store,
		egsClient:     options.egsClient,
		clock:         clock,
		accountRole:   role,
		generator:     options.steamGen,
		tokenSaver:    options.tokenSaver,
		loginKey:      strings.TrimSpace(cfg.LoginKey),
		sessionTicket: strings.TrimSpace(cfg.SessionTicket),
		steamID64:     strings.TrimSpace(cfg.SteamID64),
		accountName:   strings.TrimSpace(cfg.AccountName),
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

// Validate checks whether configuration or runtime state contains valid Steam credentials.
func (p *SteamAuthProvider) Validate() error {
	p.mu.RLock()
	ticket := p.sessionTicket
	steamID := p.steamID64
	loginKey := p.loginKey
	p.mu.RUnlock()

	if ticket == "" {
		if strings.TrimSpace(p.cfg.Username) != "" &&
			(strings.TrimSpace(p.cfg.Password) != "" || loginKey != "") {
			return nil
		}
		return fmt.Errorf("%w: steam provider requires 'session_ticket' or credentials ('username' and 'password'/'login_key')", ErrMissingCredentials)
	}

	if steamID == "" {
		return fmt.Errorf("%w: steam provider requires 'steam_id_64'", ErrMissingCredentials)
	}

	return ValidateSteamID64(steamID)
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

// TokenInfo returns a safe copy of the current cached TokenInfo, or nil if not authenticated.
func (p *SteamAuthProvider) TokenInfo() *TokenInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.tokenInfo.clone()
}

func (p *SteamAuthProvider) validCachedToken() *TokenInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.tokenInfo != nil && !p.tokenInfo.IsExpired() {
		return p.tokenInfo.clone()
	}
	return nil
}

type steamStateSnapshot struct {
	ticket        string
	steamID       string
	accountName   string
	loginKey      string
	epicAccountID string
	eosRefresh    string
}

func (p *SteamAuthProvider) snapshotState() steamStateSnapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()

	snap := steamStateSnapshot{
		ticket:        p.sessionTicket,
		steamID:       p.steamID64,
		accountName:   p.accountName,
		loginKey:      p.loginKey,
		epicAccountID: p.epicAccountID,
	}
	if p.tokenInfo != nil {
		snap.eosRefresh = strings.TrimSpace(p.tokenInfo.RefreshToken)
		if snap.epicAccountID == "" {
			snap.epicAccountID = strings.TrimSpace(p.tokenInfo.EpicAccountID)
		}
	}
	return snap
}

func (p *SteamAuthProvider) backfillFromStore(ctx context.Context, storeKey string, snap *steamStateSnapshot) {
	if p.store == nil {
		return
	}
	storedTicket, storedSteamID, storedName, err := p.store.GetAuthState(ctx, storeKey)
	if err != nil {
		return
	}
	if snap.ticket == "" && strings.TrimSpace(storedTicket) != "" {
		snap.ticket = strings.TrimSpace(storedTicket)
	}
	if snap.steamID == "" && strings.TrimSpace(storedSteamID) != "" {
		snap.steamID = strings.TrimSpace(storedSteamID)
	}
	if snap.accountName == "" && strings.TrimSpace(storedName) != "" {
		snap.accountName = strings.TrimSpace(storedName)
	}
}

// Authenticate exchanges a Steam session ticket for a Rocket League scoped EOS token.
// Concurrent callers are serialized via refreshMu with double-checked locking so parallel
// callers reuse the freshly authenticated token without triggering duplicate Steam logons.
func (p *SteamAuthProvider) Authenticate(ctx context.Context) (*TokenInfo, error) {
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

func (p *SteamAuthProvider) authenticateLocked(ctx context.Context) (*TokenInfo, error) {
	storeKey := p.accountRole.StoreKey("steam")
	snap := p.snapshotState()
	username := strings.TrimSpace(p.cfg.Username)
	password := strings.TrimSpace(p.cfg.Password)

	if snap.ticket == "" || snap.steamID == "" {
		p.backfillFromStore(ctx, storeKey, &snap)
	}

	if snap.ticket == "" || snap.steamID == "" {
		if err := p.generateTicketLocked(ctx, username, password, &snap); err != nil {
			return nil, err
		}
	}

	if err := ValidateSteamID64(snap.steamID); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	eosResp, err := p.egsClient.ExchangeEOSTokenFromSteam(snap.ticket)
	if err != nil {
		if p.generator != nil && username != "" && (password != "" || snap.loginKey != "") {
			if genErr := p.generateTicketLocked(ctx, username, password, &snap); genErr == nil {
				eosResp, err = p.egsClient.ExchangeEOSTokenFromSteam(snap.ticket)
			}
		}
	}

	if err != nil {
		return nil, fmt.Errorf("%w: steam ticket exchange failed: %v", ErrExchangeFailed, err)
	}
	if eosResp == nil {
		return nil, fmt.Errorf("%w: received nil EOS token response from steam ticket exchange", ErrExchangeFailed)
	}

	return p.commitSessionLocked(ctx, storeKey, snap, eosResp, eosResp.RefreshToken), nil
}

// Refresh attempts to refresh the EOS session token using the EOS refresh token.
// If EOS refresh fails, it regenerates a fresh Steam session ticket using the stored loginKey or password.
// Preserves EpicAccountID across all subsequent refresh paths.
func (p *SteamAuthProvider) Refresh(ctx context.Context, refreshToken string) (*TokenInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	p.refreshMu.Lock()
	defer p.refreshMu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	storeKey := p.accountRole.StoreKey("steam")
	snap := p.snapshotState()
	if snap.steamID == "" || snap.accountName == "" || snap.ticket == "" {
		p.backfillFromStore(ctx, storeKey, &snap)
	}

	tokenToUse := strings.TrimSpace(refreshToken)
	if tokenToUse == "" {
		tokenToUse = snap.eosRefresh
	}

	var lastErr error
	// 1. First attempt: refresh existing EOS token while preserving linked EpicAccountID.
	if tokenToUse != "" {
		eosResp, err := p.egsClient.RefreshEOSToken(tokenToUse)
		if err == nil && eosResp != nil {
			nextRefresh := eosResp.RefreshToken
			if nextRefresh == "" {
				nextRefresh = tokenToUse
			}
			return p.commitSessionLocked(ctx, storeKey, snap, eosResp, nextRefresh), nil
		}
		lastErr = err
	}

	// 2. Second attempt: generate fresh Steam session ticket via generator using loginKey or password.
	username := strings.TrimSpace(p.cfg.Username)
	password := strings.TrimSpace(p.cfg.Password)
	if p.generator != nil && username != "" && (password != "" || snap.loginKey != "") {
		if genErr := p.generateTicketLocked(ctx, username, password, &snap); genErr == nil && snap.ticket != "" {
			eosResp, exErr := p.egsClient.ExchangeEOSTokenFromSteam(snap.ticket)
			if exErr == nil && eosResp != nil {
				return p.commitSessionLocked(ctx, storeKey, snap, eosResp, eosResp.RefreshToken), nil
			}
			lastErr = exErr
		} else if genErr != nil {
			lastErr = genErr
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("%w: steam session ticket cannot be automatically renewed, a fresh ticket is required: %v", ErrRefreshFailed, lastErr)
	}
	return nil, fmt.Errorf("%w: steam session ticket cannot be automatically renewed, a fresh ticket is required", ErrRefreshFailed)
}

func (p *SteamAuthProvider) generateTicketLocked(
	ctx context.Context,
	username string,
	password string,
	snap *steamStateSnapshot,
) error {
	if p.generator == nil {
		return fmt.Errorf("%w: steam provider requires session ticket or ticket generator", ErrMissingCredentials)
	}
	if username == "" || (password == "" && snap.loginKey == "") {
		return fmt.Errorf("%w: steam generator requires username and password or login_key", ErrMissingCredentials)
	}

	res, err := p.generator.GenerateTicket(ctx, SteamTicketOptions{
		Username:     username,
		Password:     password,
		LoginKey:     snap.loginKey,
		AccountLabel: p.accountRole.Label(),
	})
	if err != nil {
		return fmt.Errorf("generating steam ticket: %w", err)
	}
	if res == nil || strings.TrimSpace(res.SessionTicket) == "" {
		return fmt.Errorf("%w: steam generator returned empty session ticket", ErrAuthFailed)
	}

	snap.ticket = strings.TrimSpace(res.SessionTicket)
	if sid := strings.TrimSpace(res.SteamID64); sid != "" {
		snap.steamID = sid
	}
	if name := strings.TrimSpace(res.AccountName); name != "" {
		snap.accountName = name
	}
	if key := strings.TrimSpace(res.LoginKey); key != "" {
		snap.loginKey = key
	}

	p.mu.Lock()
	p.sessionTicket = snap.ticket
	if snap.steamID != "" {
		p.steamID64 = snap.steamID
	}
	if snap.accountName != "" {
		p.accountName = snap.accountName
	}
	if snap.loginKey != "" {
		p.loginKey = snap.loginKey
	}
	p.mu.Unlock()

	if p.tokenSaver != nil {
		_ = p.tokenSaver(BuildSteamTokenUpdate(p.accountRole, snap.ticket, snap.steamID, snap.loginKey))
	}
	return nil
}

func (p *SteamAuthProvider) commitSessionLocked(
	ctx context.Context,
	storeKey string,
	snap steamStateSnapshot,
	eosResp *rlapi.EOSTokenResponse,
	refreshToken string,
) *TokenInfo {
	epicID := snap.epicAccountID
	if id := strings.TrimSpace(eosResp.AccountID); id != "" {
		epicID = id
	}

	now := p.clock()
	info := &TokenInfo{
		Provider:      "steam",
		AccessToken:   eosResp.AccessToken,
		RefreshToken:  refreshToken,
		AccountID:     snap.steamID,
		EpicAccountID: epicID,
		DisplayName:   snap.accountName,
		TokenType:     eosResp.TokenType,
		ExpiresAt:     computeTokenExpiry(now, eosResp.ExpiresIn),
		RawEOS:        eosResp,
		nowFn:         p.clock,
	}

	if p.store != nil && snap.ticket != "" && snap.steamID != "" {
		_ = p.store.SaveAuthState(ctx, storeKey, snap.ticket, snap.steamID, snap.accountName)
	}

	p.mu.Lock()
	p.sessionTicket = snap.ticket
	p.steamID64 = snap.steamID
	p.accountName = snap.accountName
	p.loginKey = snap.loginKey
	p.epicAccountID = epicID
	p.tokenInfo = info
	cp := info.clone()
	p.mu.Unlock()

	return cp
}
