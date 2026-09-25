package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rlapi"
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
