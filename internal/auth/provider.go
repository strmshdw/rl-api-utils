package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rlapi"
)

// defaultExpiryBuffer is the proactive safety window applied by IsExpired so tokens
// are refreshed before hitting upstream expiration mid-request.
const defaultExpiryBuffer = 5 * time.Minute

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

	nowFn func() time.Time
}

// SetClock injects a custom clock function used by IsExpired and IsExpiredWithBuffer.
func (t *TokenInfo) SetClock(fn func() time.Time) {
	if t != nil {
		t.nowFn = fn
	}
}

func (t *TokenInfo) now() time.Time {
	if t != nil && t.nowFn != nil {
		return t.nowFn()
	}
	return time.Now()
}

func (t *TokenInfo) clone() *TokenInfo {
	if t == nil {
		return nil
	}
	cp := *t
	return &cp
}

// IsExpired checks if the access token is expired, applying the default 5-minute safety buffer.
func (t *TokenInfo) IsExpired() bool {
	return t.IsExpiredWithBuffer(defaultExpiryBuffer)
}

// IsExpiredWithBuffer checks if the access token expires within the given buffer duration.
func (t *TokenInfo) IsExpiredWithBuffer(buffer time.Duration) bool {
	if t == nil || t.AccessToken == "" {
		return true
	}
	if t.ExpiresAt.IsZero() {
		return false
	}
	return t.now().Add(buffer).After(t.ExpiresAt)
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
	GetAuthURL() string
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
	egsClient  EGSClient
	store      storage.StateStore
	clock      func() time.Time
	role       AccountRole
	prompter   CodePrompter
	steamGen   SteamTicketGenerator
	tokenSaver ConfigTokenSaver
	logger     *slog.Logger
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

// WithAccountRole sets the account role (RolePrimary or RolePolling).
func WithAccountRole(role AccountRole) Option {
	return func(o *providerOptions) {
		o.role = role
	}
}

// WithCodePrompter sets a custom prompter for Epic authorization codes.
func WithCodePrompter(prompter CodePrompter) Option {
	return func(o *providerOptions) {
		o.prompter = prompter
	}
}

// WithSteamGenerator sets a custom Steam session ticket generator.
func WithSteamGenerator(generator SteamTicketGenerator) Option {
	return func(o *providerOptions) {
		o.steamGen = generator
	}
}

// WithConfigTokenSaver sets a callback to save updated tokens to the configuration file.
func WithConfigTokenSaver(saver ConfigTokenSaver) Option {
	return func(o *providerOptions) {
		o.tokenSaver = saver
	}
}

// WithLogger sets the structured logger used for non-fatal auth persistence diagnostics.
func WithLogger(logger *slog.Logger) Option {
	return func(o *providerOptions) {
		if logger != nil {
			o.logger = logger
		}
	}
}

func defaultOptions() *providerOptions {
	return &providerOptions{
		egsClient:  rlapi.NewEGS(),
		clock:      time.Now,
		logger:     slog.Default(),
		role:       RolePrimary,
		prompter:   nil,
		steamGen:   NewNodeSteamTicketGenerator(""),
		tokenSaver: NewConfigTokenSaver(""),
	}
}

func computeTokenExpiry(now time.Time, expiresIn int) time.Time {
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	return now.Add(time.Duration(expiresIn) * time.Second)
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

// NewPollingProvider creates an AuthProvider dedicated to secondary polling.
// It sets RolePolling so tokens are persisted under distinct keys ("epic_polling", "steam_polling")
// guaranteeing complete isolation from primary account tokens.
func NewPollingProvider(cfg config.PollingAuthConfig, opts ...Option) (AuthProvider, error) {
	if !cfg.Enabled {
		return nil, ErrMissingCredentials
	}
	authCfg := config.AuthConfig{
		Provider: cfg.Provider,
		Epic:     cfg.Epic,
		Steam:    cfg.Steam,
	}
	// Prepend WithAccountRole(RolePolling) so caller options can still customize it.
	combinedOpts := append([]Option{WithAccountRole(RolePolling)}, opts...)
	return NewProvider(authCfg, nil, combinedOpts...)
}

func logPersistError(logger *slog.Logger, msg string, err error) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn(msg, "error", err)
}
