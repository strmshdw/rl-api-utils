package playertrack

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/dank/rl-api-utils/internal/auth"
	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/psynet"
	"github.com/dank/rlapi"
)

const defaultRankRPCTimeout = 30 * time.Second

// Sentinel errors for rank retrieval.
var (
	ErrRankClientClosed    = errors.New("rank client is closed")
	ErrRankClientDisabled  = errors.New("rank client is disabled")
	ErrCredentialCollision = errors.New("polling auth credentials conflict with primary auth credentials (risk of Error 67)")
	ErrMissingCredentials  = errors.New("missing polling authentication credentials")
)

// SkillFetcher provides an abstraction for querying player competitive skill metrics.
type SkillFetcher interface {
	GetPlayersSkills(ctx context.Context, playerIDs []rlapi.PlayerID) ([]rlapi.PlayerWithSkills, error)
	IsEnabled() bool
	Close() error
}

// SkillRPCClient abstracts WebSocket RPC methods required for rank queries.
// Satisfied directly by *rlapi.PsyNetRPC.
type SkillRPCClient interface {
	GetPlayersSkills(ctx context.Context, playerIDs []rlapi.PlayerID) ([]rlapi.PlayerWithSkills, error)
	IsConnected() bool
	Close() error
}

// SkillRPCFactory instantiates a SkillRPCClient from credentials (used for tests and mock injection).
type SkillRPCFactory func(ctx context.Context, creds *psynet.Credentials) (SkillRPCClient, error)

// RankClientConfig configures RankClient construction.
type RankClientConfig struct {
	PrimaryAuth         config.AuthConfig
	PollingAuth         config.PollingAuthConfig
	CredentialsSupplier psynet.CredentialsSupplier
	AuthProvider        auth.AuthProvider
	Logger              *slog.Logger
	PsyNet              *rlapi.PsyNet
	RPCFactory          SkillRPCFactory
}

// AuthProviderSupplier bridges auth.AuthProvider into psynet.CredentialsSupplier.
type AuthProviderSupplier struct {
	provider auth.AuthProvider
}

// NewAuthProviderSupplier creates an AuthProviderSupplier wrapping an AuthProvider.
func NewAuthProviderSupplier(provider auth.AuthProvider) *AuthProviderSupplier {
	return &AuthProviderSupplier{provider: provider}
}

// GetCredentials resolves fresh authentication tokens from the underlying AuthProvider.
func (s *AuthProviderSupplier) GetCredentials(ctx context.Context) (*psynet.Credentials, error) {
	if s.provider == nil {
		return nil, ErrMissingCredentials
	}

	token := s.provider.TokenInfo()
	if token == nil || token.IsExpired() {
		var err error
		token, err = s.provider.Authenticate(ctx)
		if err != nil {
			return nil, fmt.Errorf("authenticating polling account (%s): %w", s.provider.Name(), err)
		}
	}

	platform := "Epic"
	if strings.EqualFold(s.provider.Name(), "steam") {
		platform = "Steam"
	}

	creds := &psynet.Credentials{
		Platform:    platform,
		AuthToken:   token.AccessToken,
		AccountID:   token.EpicAccountID,
		DisplayName: token.DisplayName,
	}
	if platform == "Steam" {
		creds.SteamAccountID = token.AccountID
	}
	if creds.AccountID == "" {
		creds.AccountID = token.AccountID
	}
	return creds, nil
}

// NoOpRankClient is an offline fallback that satisfies SkillFetcher without performing network operations.
type NoOpRankClient struct{}

// NewNoOpRankClient returns an instance of NoOpRankClient.
func NewNoOpRankClient() *NoOpRankClient {
	return &NoOpRankClient{}
}

// GetPlayersSkills returns nil, nil immediately without performing any network operations.
func (n *NoOpRankClient) GetPlayersSkills(ctx context.Context, playerIDs []rlapi.PlayerID) ([]rlapi.PlayerWithSkills, error) {
	return nil, nil
}

// IsEnabled always returns false for NoOpRankClient.
func (n *NoOpRankClient) IsEnabled() bool {
	return false
}

// Close is a no-op returning nil.
func (n *NoOpRankClient) Close() error {
	return nil
}

// PsyNetRankClient executes PsyNet Skills/GetPlayersSkills v1 RPC queries
// over an isolated WebSocket connection using a dedicated non-playing secondary account.
type PsyNetRankClient struct {
	mu         sync.Mutex
	cfg        RankClientConfig
	supplier   psynet.CredentialsSupplier
	psyNet     *rlapi.PsyNet
	rpc        SkillRPCClient
	rpcFactory SkillRPCFactory
	logger     *slog.Logger
	closed     bool
}

// NewPsyNetRankClient constructs a direct PsyNetRankClient instance.
func NewPsyNetRankClient(cfg RankClientConfig) (*PsyNetRankClient, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	supplier := cfg.CredentialsSupplier
	if supplier == nil {
		if cfg.AuthProvider != nil {
			supplier = NewAuthProviderSupplier(cfg.AuthProvider)
		} else {
			return nil, ErrMissingCredentials
		}
	}

	psy := cfg.PsyNet
	if psy == nil {
		psy = rlapi.NewPsyNet()
		psy.SetLogger(logger)
	}

	return &PsyNetRankClient{
		cfg:        cfg,
		supplier:   supplier,
		psyNet:     psy,
		rpcFactory: cfg.RPCFactory,
		logger:     logger,
	}, nil
}

// NewRankClient creates a SkillFetcher based on system configuration.
// Enforces Error 67 collision detection and graceful fallback to NoOpRankClient.
func NewRankClient(cfg RankClientConfig) (SkillFetcher, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	if !cfg.PollingAuth.Enabled {
		logger.Info("polling_auth is disabled; using NoOpRankClient (live rank queries disabled)")
		return NewNoOpRankClient(), nil
	}

	if err := CheckCredentialCollision(cfg.PrimaryAuth, cfg.PollingAuth); err != nil {
		logger.Warn("CRITICAL: polling auth credential collision detected; falling back to NoOpRankClient to prevent kicking game client with Error 67",
			slog.Any("error", err),
			slog.String("primary_provider", cfg.PrimaryAuth.Provider),
			slog.String("polling_provider", cfg.PollingAuth.Provider),
		)
		return NewNoOpRankClient(), nil
	}

	supplier := cfg.CredentialsSupplier
	if supplier == nil {
		authProvider := cfg.AuthProvider
		if authProvider == nil {
			var ok bool
			authProvider, ok = buildIsolatedPollingProvider(cfg.PollingAuth, logger)
			if !ok {
				return NewNoOpRankClient(), nil
			}
		}
		supplier = NewAuthProviderSupplier(authProvider)
	}

	client, err := NewPsyNetRankClient(RankClientConfig{
		PrimaryAuth:         cfg.PrimaryAuth,
		PollingAuth:         cfg.PollingAuth,
		CredentialsSupplier: supplier,
		Logger:              logger,
		PsyNet:              cfg.PsyNet,
		RPCFactory:          cfg.RPCFactory,
	})
	if err != nil {
		logger.Warn("failed to initialize PsyNetRankClient; falling back to NoOpRankClient", slog.Any("error", err))
		return NewNoOpRankClient(), nil
	}
	return client, nil
}

func buildIsolatedPollingProvider(polling config.PollingAuthConfig, logger *slog.Logger) (auth.AuthProvider, bool) {
	providerName := strings.ToLower(strings.TrimSpace(polling.Provider))
	authCfg := config.AuthConfig{
		Provider: providerName,
		Epic:     polling.Epic,
		Steam:    polling.Steam,
	}

	switch providerName {
	case "epic":
		if strings.TrimSpace(authCfg.Epic.RefreshToken) == "" && strings.TrimSpace(authCfg.Epic.AuthCode) == "" {
			logger.Warn("polling_auth enabled for epic but missing credentials; falling back to NoOpRankClient")
			return nil, false
		}
	case "steam":
		if strings.TrimSpace(authCfg.Steam.SessionTicket) == "" || strings.TrimSpace(authCfg.Steam.SteamID64) == "" {
			logger.Warn("polling_auth enabled for steam but missing credentials; falling back to NoOpRankClient")
			return nil, false
		}
	default:
		logger.Warn("polling_auth has invalid or unsupported provider; falling back to NoOpRankClient",
			slog.String("provider", polling.Provider),
		)
		return nil, false
	}

	// Pass store: nil for complete isolation from primary auth state.
	authProvider, err := auth.NewProvider(authCfg, nil)
	if err != nil {
		logger.Warn("failed to instantiate polling auth provider; falling back to NoOpRankClient", slog.Any("error", err))
		return nil, false
	}
	return authProvider, true
}

// GetPlayersSkills retrieves skill data across all playlists for the requested player IDs.
func (c *PsyNetRankClient) GetPlayersSkills(ctx context.Context, playerIDs []rlapi.PlayerID) ([]rlapi.PlayerWithSkills, error) {
	if len(playerIDs) == 0 {
		return nil, nil
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrRankClientClosed
	}
	if c.rpc == nil || !c.rpc.IsConnected() {
		if err := c.connectLocked(ctx); err != nil {
			c.mu.Unlock()
			return nil, fmt.Errorf("failed to connect to psynet: %w", err)
		}
	}
	activeRPC := c.rpc
	c.mu.Unlock()

	results, err := callSkillRPCWithTimeout(ctx, activeRPC, playerIDs)
	if err == nil {
		return results, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if errors.Is(err, rlapi.ErrConnectionClosed) || isNetworkOrEOF(err) {
		c.logger.Warn("psynet connection dropped during rank query; attempting transparent reconnect", slog.Any("err", err))

		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return nil, ErrRankClientClosed
		}
		if recErr := c.connectLocked(ctx); recErr != nil {
			c.mu.Unlock()
			return nil, fmt.Errorf("rank query failed (%v) and reconnect failed: %w", err, recErr)
		}
		retryRPC := c.rpc
		c.mu.Unlock()

		results, err = callSkillRPCWithTimeout(ctx, retryRPC, playerIDs)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("rank query failed after reconnect: %w", err)
		}
		return results, nil
	}

	return nil, fmt.Errorf("psynet GetPlayersSkills failed: %w", err)
}

func callSkillRPCWithTimeout(ctx context.Context, rpc SkillRPCClient, playerIDs []rlapi.PlayerID) ([]rlapi.PlayerWithSkills, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, defaultRankRPCTimeout)
	defer cancel()
	return rpc.GetPlayersSkills(rpcCtx, playerIDs)
}

// IsEnabled returns true if the client is active and not closed.
func (c *PsyNetRankClient) IsEnabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closed
}

// Close gracefully closes the underlying WebSocket RPC connection.
func (c *PsyNetRankClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	var err error
	if c.rpc != nil {
		err = c.rpc.Close()
		c.rpc = nil
	}
	return err
}

func (c *PsyNetRankClient) connectLocked(ctx context.Context) error {
	if c.closed {
		return ErrRankClientClosed
	}

	if c.rpc != nil {
		_ = c.rpc.Close()
		c.rpc = nil
	}

	creds, err := c.supplier.GetCredentials(ctx)
	if err != nil {
		return fmt.Errorf("resolving polling credentials: %w", err)
	}

	primaryID := c.cfg.PrimaryAuth.Epic.AccountID
	if strings.EqualFold(c.cfg.PrimaryAuth.Provider, "steam") {
		primaryID = c.cfg.PrimaryAuth.Steam.SteamID64
	}
	if primaryID != "" && creds.AccountID == primaryID {
		return ErrCredentialCollision
	}

	if c.rpcFactory != nil {
		rpc, err := c.rpcFactory(ctx, creds)
		if err != nil {
			return err
		}
		c.rpc = rpc
		return nil
	}

	var rpc *rlapi.PsyNetRPC
	platform := strings.ToLower(strings.TrimSpace(creds.Platform))
	switch platform {
	case "epic", "":
		rpc, err = c.psyNet.AuthPlayer(creds.AuthToken, creds.AccountID, creds.DisplayName)
		if err != nil {
			return fmt.Errorf("epic auth player failed: %w", err)
		}
	case "steam":
		if creds.SteamAccountID == "" {
			return errors.New("steam authentication requires SteamAccountID")
		}
		rpc, err = c.psyNet.AuthPlayerSteam(creds.AuthToken, creds.AccountID, creds.SteamAccountID, creds.DisplayName)
		if err != nil {
			return fmt.Errorf("steam auth player failed: %w", err)
		}
	default:
		return fmt.Errorf("unsupported platform %q", creds.Platform)
	}

	c.rpc = rpc
	c.logger.Info("connected secondary polling account to psynet rpc",
		slog.String("platform", creds.Platform),
		slog.String("account_id", creds.AccountID),
	)
	return nil
}

func isNetworkOrEOF(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "EOF") ||
		strings.Contains(msg, "closed") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe")
}
