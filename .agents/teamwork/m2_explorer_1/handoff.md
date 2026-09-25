# Milestone 2 (internal/auth) Exploration & Design Report

**Target**: `internal/auth` subsystem for Milestone 2 (Auth & PsyNet Integration)  
**Author**: `m2_explorer_1` (Role: Auth Explorer)  
**Date**: 2026-09-25T03:45:00Z  
**Status**: COMPLETE (Verified with 100% Passing Tests, 77.0% Statement Coverage)  
**Working Directory**: `d:\code\rl-api-utils\.agents\teamwork\m2_explorer_1`

---

## 1. Observation

Direct examination of project documentation, dependencies, specifications, and the existing codebase revealed the following concrete technical facts and constraints:

### 1.1 Requirements & Interface Specifications
1. **Milestone Scope (`ORIGINAL_REQUEST.md:25-30,50-52`)**:
   - R4 requirement: Support dual authentication paths:
     - Epic Games Auth: authenticate using EGS refresh tokens or authorization codes exchanged for EOS tokens (`egs.ExchangeEOSToken` and `psyNet.AuthPlayer`).
     - Steam Auth: authenticate using Steam session tickets exchanged for EOS tokens (`egs.ExchangeEOSTokenFromSteam`) and Steam PsyNet authentication (`psyNet.AuthPlayerSteam` with Steam ID 64).
     - Headless restart recovery: persist authentication state across restarts.
2. **Architecture Contract (`PROJECT.md:18,60-66,204-207`)**:
   - Layout:
     ```
     internal/auth/
     ├── provider.go
     ├── epic.go
     ├── steam.go
     └── auth_test.go
     ```
   - Decoupled Clean Architecture: syncer and daemon interface with `AuthProvider` rather than directly invoking concrete network SDK methods.
3. **StateStore Auth Methods (`internal/storage/store.go:100-103`)**:
   ```go
   SaveAuthState(ctx context.Context, provider, refreshToken, accountID, displayName string) error
   GetAuthState(ctx context.Context, provider string) (refreshToken, accountID, displayName string, err error)
   ```
   - In SQLite backend (`internal/storage/sqlite.go:404-445`), `SaveAuthState` performs an `ON CONFLICT(provider) DO UPDATE` upsert, and `GetAuthState` returns `storage.ErrAuthStateNotFound` when no record exists.
4. **Configuration Contract (`internal/config/config.go:85-104`)**:
   ```go
   type AuthConfig struct {
       Provider string      `yaml:"provider" json:"provider"` // "epic" or "steam"
       Epic     EpicConfig  `yaml:"epic" json:"epic"`
       Steam    SteamConfig `yaml:"steam" json:"steam"`
   }
   type EpicConfig struct {
       RefreshToken string `yaml:"refresh_token" json:"refresh_token"`
       AuthCode     string `yaml:"auth_code" json:"auth_code"`
       AccountID    string `yaml:"account_id" json:"account_id"`
       DisplayName  string `yaml:"display_name" json:"display_name"`
   }
   type SteamConfig struct {
       SessionTicket string `yaml:"session_ticket" json:"session_ticket"`
       SteamID64     string `yaml:"steam_id_64" json:"steam_id_64"`
       AccountName   string `yaml:"account_name" json:"account_name"`
   }
   ```
   - Validation rules in `config.go:379-393`:
     - For Epic: requires either `refresh_token` or `auth_code`.
     - For Steam: requires `session_ticket` and `steam_id_64`.
5. **Upstream SDK Behavior (`github.com/dank/rlapi`)**:
   - Authoritative source inspected at `$env:TEMP\rlapi_spec`:
     - `egs.go:94-102`:
       ```go
       func NewEGS() *EGS
       func (e *EGS) AuthenticateWithCode(authCode string) (*TokenResponse, error)
       func (e *EGS) AuthenticateWithRefreshToken(refreshToken string) (*TokenResponse, error)
       func (e *EGS) GetExchangeCode(accessToken string) (string, error)
       func (e *EGS) ExchangeEOSToken(exchangeCode string) (*EOSTokenResponse, error)
       func (e *EGS) ExchangeEOSTokenFromSteam(steamTicket string) (*EOSTokenResponse, error)
       func (e *EGS) RefreshEOSToken(refreshToken string) (*EOSTokenResponse, error)
       ```
     - `auth.go:33,69`:
       ```go
       func (p *PsyNet) AuthPlayer(authToken string, accountID string, accountName string) (*PsyNetRPC, error)
       func (p *PsyNet) AuthPlayerSteam(authToken string, epicAccountID string, steamAccountID string, accountName string) (*PsyNetRPC, error)
       ```
       - `AuthPlayer` requires: EOS access token (`authToken`), Epic Account ID (`accountID`), and display name (`accountName`).
       - `AuthPlayerSteam` requires: EOS access token (`authToken`), linked Epic Account ID (`epicAccountID`), SteamID64 (`steamAccountID`), and account name (`accountName`).
6. **Existing Test Suite Expectations (`test/e2e/tier1_feature_test.go:482-495`)**:
   - SteamID64 validation: strictly 17-digit numeric string, standard format starting with `7656119`.
   - Store persistence: `SaveAuthState(ctx, "epic", ...)` and `SaveAuthState(ctx, "steam", ...)`.

---

## 2. Logic Chain

From the direct observations above, the architectural requirements and interactions are synthesized step-by-step:

### 2.1 The Need for `TokenInfo` Abstraction
1. `rlapi.PsyNet.AuthPlayer` and `rlapi.PsyNet.AuthPlayerSteam` expect distinct sets of credentials:
   - Epic expects: `(eosAccessToken, epicAccountID, displayName)`.
   - Steam expects: `(eosAccessToken, linkedEpicAccountID, steamID64, accountName)`.
2. To allow `internal/psynet` to remain platform-agnostic, `TokenInfo` must encapsulate:
   - `Provider`: `"epic"` or `"steam"`
   - `AccessToken`: EOS token (common token passed to `AuthPlayer`/`AuthPlayerSteam`)
   - `RefreshToken`: EGS or EOS refresh token for renewals
   - `AccountID`: Platform primary identifier (Epic Account ID or SteamID64)
   - `EpicAccountID`: Epic Games Account ID (equal to `AccountID` for Epic; the linked Epic Account ID for Steam)
   - `DisplayName`: Account / persona name
   - `ExpiresAt`: Absolute timestamp calculated from `ExpiresIn`
3. Proactive expiration checking: `TokenInfo.IsExpired()` with a default 30-second buffer allows downstream workers/syncer to refresh before tokens expire mid-operation.

### 2.2 Complete Mockability via `EGSClient` Interface
1. In `rlapi`, `EGS` uses an internal unexported `*http.Client`.
2. If `internal/auth` bound directly to concrete `*rlapi.EGS`, unit testing would require spinning up an HTTP server or hijacking global `http.DefaultTransport`.
3. By defining the `EGSClient` interface:
   ```go
   type EGSClient interface {
       AuthenticateWithCode(authCode string) (*rlapi.TokenResponse, error)
       AuthenticateWithRefreshToken(refreshToken string) (*rlapi.TokenResponse, error)
       GetExchangeCode(accessToken string) (string, error)
       ExchangeEOSToken(exchangeCode string) (*rlapi.EOSTokenResponse, error)
       ExchangeEOSTokenFromSteam(steamTicket string) (*rlapi.EOSTokenResponse, error)
       RefreshEOSToken(refreshToken string) (*rlapi.EOSTokenResponse, error)
   }
   ```
   `*rlapi.EGS` satisfies this interface **implicitly and automatically** without any wrapper or adapter code.
4. Unit tests can inject a `mockEGS` directly, testing every failure permutation (OAuth error, exchange code error, EOS error, network error, context cancellation) in under 1 millisecond.

### 2.3 Headless Recovery via `StateStore`
1. For Epic authentication, initial login may be done via a one-time web `auth_code`.
2. Once exchanged, Epic returns a long-lived `refresh_token`.
3. If the daemon restarts, `p.cfg.RefreshToken` and `p.cfg.AuthCode` may be empty in configuration files.
4. `EpicAuthProvider.Authenticate` checks `store.GetAuthState(ctx, "epic")` as a fallback. If a persisted refresh token is found, it automatically restores the session without manual user intervention.
5. On every successful authentication or refresh, `store.SaveAuthState` is called with the newest refresh token.

### 2.4 Ephemeral Steam Session Ticket Semantics
1. Steam session tickets are one-time or ephemeral; if expired, they cannot be refreshed via standard OAuth refresh grants unless EOS issued an EOS refresh token.
2. In `SteamAuthProvider.Refresh`:
   - If an EOS refresh token is available, call `egsClient.RefreshEOSToken`.
   - If no EOS refresh token is available or refresh fails, return a clear error: `steam session ticket cannot be automatically renewed, a fresh ticket is required`.

### 2.5 Validation & Thread Safety
1. Input validation occurs before any network calls:
   - Epic: checks that either `RefreshToken` or `AuthCode` exists (or store has state).
   - Steam: checks `SessionTicket != ""` and `ValidateSteamID64(id)` (17 digits, numeric, prefix `7656119`).
2. Concurrent access: `tokenInfo` is protected by `sync.RWMutex` to guarantee safe concurrent reads (`TokenInfo()`) and writes (`Authenticate()`, `Refresh()`).

---

## 3. Caveats

1. **Go Version in Upstream rlapi**:
   - `github.com/dank/rlapi` specifies `go 1.24.5` in its `go.mod`. In an environment running `go 1.24.1` with `GOTOOLCHAIN=local`, `go` may complain about version mismatch if not managed. In standard Go toolchains with `GOTOOLCHAIN=auto` (the default), Go seamlessly switches toolchains. The implementer should ensure `go.mod` in `rl-api-utils` is maintained at `go 1.24.1` with standard toolchain handling.
2. **Steam Session Ticket Lifetime**:
   - Steam session tickets are issued by Steam client / Steamworks API. If a ticket expires and cannot be refreshed via EOS, manual re-acquisition of a fresh session ticket by the user is required.
3. **StateStore Failure Resilience**:
   - If `StateStore.SaveAuthState` fails (e.g. disk full), `Authenticate()` should log a warning but NOT abort the authentication if in-memory tokens were successfully received from Epic/Steam.

---

## 4. Conclusion & Proposed Code Implementation

The proposed design for `internal/auth` is complete, thoroughly tested, and ready for immediate implementation.

### 4.1 Proposed `internal/auth/provider.go`
```go
package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dank/rlapi"
	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/storage"
)

// Sentinel errors for the authentication layer.
var (
	ErrMissingCredentials  = errors.New("missing authentication credentials")
	ErrUnsupportedProvider = errors.New("unsupported authentication provider")
	ErrTokenExpired        = errors.New("authentication token has expired")
	ErrAuthFailed          = errors.New("authentication failed")
	ErrRefreshFailed       = errors.New("token refresh failed")
	ErrNotAuthenticated    = errors.New("provider has not been authenticated")
	ErrExchangeFailed      = errors.New("token exchange failed")
	ErrInvalidSteamID      = errors.New("invalid steamid64 format")
)

// TokenInfo holds normalized credentials and tokens resulting from successful authentication.
type TokenInfo struct {
	Provider      string                  `json:"provider"`
	AccessToken   string                  `json:"access_token"`
	RefreshToken  string                  `json:"refresh_token"`
	AccountID     string                  `json:"account_id"`
	EpicAccountID string                  `json:"epic_account_id"`
	DisplayName   string                  `json:"display_name"`
	TokenType     string                  `json:"token_type"`
	ExpiresAt     time.Time               `json:"expires_at"`
	RawEOS        *rlapi.EOSTokenResponse `json:"raw_eos,omitempty"`
}

// IsExpired checks if the access token is expired, applying a default 30-second safety buffer.
func (t *TokenInfo) IsExpired() bool {
	return t.IsExpiredWithBuffer(30 * time.Second)
}

// IsExpiredWithBuffer checks if the access token expires within the given buffer duration.
func (t *TokenInfo) IsExpiredWithBuffer(buffer time.Duration) bool {
	if t == nil || t.AccessToken == "" {
		return true
	}
	if t.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().Add(buffer).After(t.ExpiresAt)
}

// Valid returns true if the token is non-nil, has an access token, and is not expired.
func (t *TokenInfo) Valid() bool {
	return t != nil && t.AccessToken != "" && !t.IsExpired()
}

// AuthProvider defines the domain contract for authentication providers.
type AuthProvider interface {
	Name() string
	Authenticate(ctx context.Context) (*TokenInfo, error)
	Refresh(ctx context.Context, refreshToken string) (*TokenInfo, error)
	TokenInfo() *TokenInfo
	Validate() error
}

// EGSClient defines the interface required from the underlying Epic Games Store client.
// rlapi.NewEGS() satisfies this interface.
type EGSClient interface {
	AuthenticateWithCode(authCode string) (*rlapi.TokenResponse, error)
	AuthenticateWithRefreshToken(refreshToken string) (*rlapi.TokenResponse, error)
	GetExchangeCode(accessToken string) (string, error)
	ExchangeEOSToken(exchangeCode string) (*rlapi.EOSTokenResponse, error)
	ExchangeEOSTokenFromSteam(steamTicket string) (*rlapi.EOSTokenResponse, error)
	RefreshEOSToken(refreshToken string) (*rlapi.EOSTokenResponse, error)
}

// Option configures an AuthProvider.
type Option func(*providerOptions)

type providerOptions struct {
	egsClient EGSClient
	store     storage.StateStore
	clock     func() time.Time
}

// WithEGSClient sets a custom EGS client (for testing or custom transports).
func WithEGSClient(client EGSClient) Option {
	return func(o *providerOptions) {
		o.egsClient = client
	}
}

// WithStateStore injects a StateStore for persisting and restoring auth tokens.
func WithStateStore(store storage.StateStore) Option {
	return func(o *providerOptions) {
		o.store = store
	}
}

// WithClock sets a custom clock function for testing expiration logic.
func WithClock(clock func() time.Time) Option {
	return func(o *providerOptions) {
		o.clock = clock
	}
}

func defaultOptions() *providerOptions {
	return &providerOptions{
		egsClient: rlapi.NewEGS(),
		clock:     time.Now,
	}
}

// NewProvider is a factory that instantiates an AuthProvider based on AuthConfig.Provider.
func NewProvider(cfg config.AuthConfig, store storage.StateStore, opts ...Option) (AuthProvider, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	switch provider {
	case "epic":
		return NewEpicProvider(cfg.Epic, store, opts...)
	case "steam":
		return NewSteamProvider(cfg.Steam, store, opts...)
	case "":
		return nil, fmt.Errorf("%w: provider cannot be empty", ErrUnsupportedProvider)
	default:
		return nil, fmt.Errorf("%w: %q (must be 'epic' or 'steam')", ErrUnsupportedProvider, cfg.Provider)
	}
}
```

### 4.2 Proposed `internal/auth/epic.go`
```go
package auth

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dank/rlapi"
	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/storage"
)

// EpicAuthProvider implements AuthProvider for Epic Games Store accounts.
type EpicAuthProvider struct {
	cfg       config.EpicConfig
	store     storage.StateStore
	egsClient EGSClient
	clock     func() time.Time

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

	return &EpicAuthProvider{
		cfg:       cfg,
		store:     options.store,
		egsClient: options.egsClient,
		clock:     options.clock,
	}, nil
}

// Name returns the provider identifier.
func (p *EpicAuthProvider) Name() string {
	return "epic"
}

// Validate checks whether configuration contains the required Epic credentials.
func (p *EpicAuthProvider) Validate() error {
	if strings.TrimSpace(p.cfg.RefreshToken) == "" && strings.TrimSpace(p.cfg.AuthCode) == "" {
		return fmt.Errorf("%w: epic provider requires either 'refresh_token' or 'auth_code'", ErrMissingCredentials)
	}
	return nil
}

// Authenticate executes the full Epic Games OAuth and EOS token exchange sequence.
func (p *EpicAuthProvider) Authenticate(ctx context.Context) (*TokenInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 1. Resolve credentials: check config, then check persistent StateStore fallback
	refreshToken := strings.TrimSpace(p.cfg.RefreshToken)
	authCode := strings.TrimSpace(p.cfg.AuthCode)

	if refreshToken == "" && authCode == "" && p.store != nil {
		storedToken, _, _, err := p.store.GetAuthState(ctx, "epic")
		if err == nil && strings.TrimSpace(storedToken) != "" {
			refreshToken = strings.TrimSpace(storedToken)
		}
	}

	if refreshToken == "" && authCode == "" {
		return nil, fmt.Errorf("%w: epic provider requires either 'refresh_token' or 'auth_code'", ErrMissingCredentials)
	}

	// 2. Perform EGS OAuth token grant
	var tokenResp *rlapi.TokenResponse
	var err error

	if refreshToken != "" {
		tokenResp, err = p.egsClient.AuthenticateWithRefreshToken(refreshToken)
		if err != nil && authCode != "" {
			// Fallback to auth code if refresh token failed but auth code is present
			tokenResp, err = p.egsClient.AuthenticateWithCode(authCode)
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

	// 6. Persist to StateStore if configured
	if p.store != nil && newRefreshToken != "" {
		_ = p.store.SaveAuthState(ctx, "epic", newRefreshToken, accountID, displayName)
	}

	p.mu.Lock()
	p.tokenInfo = info
	p.mu.Unlock()

	return info, nil
}

// Refresh renews the EOS session token using the provided or cached refresh token.
func (p *EpicAuthProvider) Refresh(ctx context.Context, refreshToken string) (*TokenInfo, error) {
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
	if tokenToUse == "" && p.store != nil {
		storedToken, _, _, err := p.store.GetAuthState(ctx, "epic")
		if err == nil && strings.TrimSpace(storedToken) != "" {
			tokenToUse = strings.TrimSpace(storedToken)
		}
	}
	if tokenToUse == "" {
		tokenToUse = strings.TrimSpace(p.cfg.RefreshToken)
	}

	if tokenToUse == "" {
		return nil, fmt.Errorf("%w: no refresh token available for epic session renewal", ErrMissingCredentials)
	}

	// 1. Authenticate with refresh token
	tokenResp, err := p.egsClient.AuthenticateWithRefreshToken(tokenToUse)
	if err != nil {
		return nil, fmt.Errorf("%w: egs refresh failed: %v", ErrRefreshFailed, err)
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
		_ = p.store.SaveAuthState(ctx, "epic", newRefreshToken, accountID, displayName)
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
```

### 4.3 Proposed `internal/auth/steam.go`
```go
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
```

### 4.4 Proposed `internal/auth/auth_test.go`
```go
package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dank/rlapi"
	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/storage"
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
```

---

## 5. Verification Method

To independently verify the implementation and all claims in this report:

1. **Unit Test Verification in Verification Directory**:
   Run the full unit test suite with coverage in the standalone verification workspace:
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   $env:GOTOOLCHAIN = "local"
   cd C:\Users\strms\AppData\Local\Temp\auth_verify
   go test -v -count=1 -cover ./...
   ```
   **Expected Result**:
   ```
   === RUN   TestNewProvider_Success
   --- PASS: TestNewProvider_Success (0.00s)
   === RUN   TestNewProvider_Unsupported
   --- PASS: TestNewProvider_Unsupported (0.00s)
   === RUN   TestEpicAuthProvider_Validate
   --- PASS: TestEpicAuthProvider_Validate (0.00s)
   === RUN   TestEpicAuthProvider_Authenticate_WithRefreshToken
   --- PASS: TestEpicAuthProvider_Authenticate_WithRefreshToken (0.00s)
   === RUN   TestEpicAuthProvider_Authenticate_WithAuthCode
   --- PASS: TestEpicAuthProvider_Authenticate_WithAuthCode (0.00s)
   === RUN   TestEpicAuthProvider_Authenticate_RestoreFromStateStore
   --- PASS: TestEpicAuthProvider_Authenticate_RestoreFromStateStore (0.00s)
   === RUN   TestEpicAuthProvider_Authenticate_Errors
   --- PASS: TestEpicAuthProvider_Authenticate_Errors (0.00s)
   === RUN   TestEpicAuthProvider_Refresh
   --- PASS: TestEpicAuthProvider_Refresh (0.00s)
   === RUN   TestSteamAuthProvider_Validate
   --- PASS: TestSteamAuthProvider_Validate (0.00s)
   === RUN   TestSteamAuthProvider_Authenticate_Success
   --- PASS: TestSteamAuthProvider_Authenticate_Success (0.00s)
   === RUN   TestSteamAuthProvider_Authenticate_Failure
   --- PASS: TestSteamAuthProvider_Authenticate_Failure (0.00s)
   === RUN   TestSteamAuthProvider_Refresh
   --- PASS: TestSteamAuthProvider_Refresh (0.00s)
   === RUN   TestTokenInfo_IsExpired
   --- PASS: TestTokenInfo_IsExpired (0.00s)
   PASS
   coverage: 77.0% of statements
   ok  	github.com/dank/rl-api-utils/internal/auth	0.139s
   ```
2. **Code Lint & Vet Verification**:
   ```powershell
   $env:Path = "C:\Users\strms\AppData\Local\go\go\bin;$env:Path"
   $env:GOTOOLCHAIN = "local"
   cd C:\Users\strms\AppData\Local\Temp\auth_verify
   go vet ./...
   ```
   **Expected Result**: Exit code 0 with zero warnings or errors.
3. **Invalidation Conditions**:
   - Upstream changes to `rlapi.EGS` signatures (`AuthenticateWithCode`, `AuthenticateWithRefreshToken`, `GetExchangeCode`, `ExchangeEOSToken`, `ExchangeEOSTokenFromSteam`, `RefreshEOSToken`).
   - Breaking modifications to `StateStore.SaveAuthState` / `GetAuthState` signatures in `internal/storage/store.go`.
