package psynet

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/dank/rlapi"
)

// Sentinel errors returned by the psynet client.
var (
	ErrClientClosed        = errors.New("psynet client is closed")
	ErrNotConnected        = errors.New("psynet client is not connected")
	ErrMissingCredentials  = errors.New("missing authentication credentials")
	ErrInvalidPlatform     = errors.New("unsupported or missing auth platform")
	ErrMissingSteamAccount = errors.New("steam authentication requires SteamAccountID")
)

// DiscoveredMatch represents a match metadata item extracted from PsyNet match history.
type DiscoveredMatch struct {
	MatchGUID            string
	RecordStartTimestamp int64
	MapName              string
	Playlist             int
	ReplayURL            string
}

// MatchHistoryProvider defines the interface required by the syncer orchestrator.
type MatchHistoryProvider interface {
	GetRecentMatches(ctx context.Context) ([]DiscoveredMatch, error)
	Close() error
}

// RPCClient abstracts the underlying WebSocket RPC connection (satisfied by *rlapi.PsyNetRPC).
type RPCClient interface {
	GetMatchHistory(ctx context.Context) ([]rlapi.MatchEntry, error)
	IsConnected() bool
	Close() error
}

// Credentials holds resolved authentication tokens for PsyNet bootstrapping.
type Credentials struct {
	Platform       string // "Epic" or "Steam"
	AuthToken      string // EOS Access Token
	AccountID      string // Epic Account ID
	DisplayName    string // Account Name / Display Name
	SteamAccountID string // Steam ID 64 (only if Platform == "Steam")
}

// CredentialsSupplier dynamically supplies fresh authentication credentials.
type CredentialsSupplier interface {
	GetCredentials(ctx context.Context) (*Credentials, error)
}

// StaticCredentials implements CredentialsSupplier for fixed credentials.
type StaticCredentials Credentials

func (s StaticCredentials) GetCredentials(ctx context.Context) (*Credentials, error) {
	c := Credentials(s)
	return &c, nil
}

// RPCFactory is an optional factory function for creating RPCClient instances (used in tests).
type RPCFactory func(ctx context.Context, creds *Credentials) (RPCClient, error)

// ClientConfig configures the PsyNet client.
type ClientConfig struct {
	Credentials         *Credentials
	CredentialsSupplier CredentialsSupplier
	GameVersion         string
	FeatureSet          string
	Logger              *slog.Logger
	PsyNet              *rlapi.PsyNet
	RPCFactory          RPCFactory
}

// Client implements MatchHistoryProvider interfacing with PsyNet via rlapi.
type Client struct {
	mu     sync.Mutex
	cfg    ClientConfig
	psyNet *rlapi.PsyNet
	rpc    RPCClient
	closed bool
	logger *slog.Logger
}

// NewClient creates a new PsyNet Client.
func NewClient(cfg ClientConfig) (*Client, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	psy := cfg.PsyNet
	if psy == nil {
		psy = rlapi.NewPsyNet()
		if cfg.GameVersion != "" || cfg.FeatureSet != "" {
			gv, fs := psy.GetVersion()
			if cfg.GameVersion != "" {
				gv = cfg.GameVersion
			}
			if cfg.FeatureSet != "" {
				fs = cfg.FeatureSet
			}
			psy.SetVersion(gv, fs)
		}
		psy.SetLogger(logger)
	}

	return &Client{
		cfg:    cfg,
		psyNet: psy,
		logger: logger,
	}, nil
}

// NewClientWithRPC creates a Client wrapping an existing RPCClient instance (primarily for tests).
func NewClientWithRPC(rpc RPCClient, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		rpc:    rpc,
		logger: logger,
	}
}

// GetRecentMatches retrieves the recent match history from PsyNet.
// If the connection is down, it attempts to connect or reconnect automatically.
func (c *Client) GetRecentMatches(ctx context.Context) ([]DiscoveredMatch, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrClientClosed
	}

	// 1. Ensure connected
	if c.rpc == nil || !c.rpc.IsConnected() {
		if err := c.connectLocked(ctx); err != nil {
			c.mu.Unlock()
			return nil, fmt.Errorf("failed to connect to psynet: %w", err)
		}
	}
	activeRPC := c.rpc
	c.mu.Unlock()

	// 2. Query match history
	entries, err := activeRPC.GetMatchHistory(ctx)
	if err != nil {
		// Check for context cancellation
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Check for connection drop; attempt transparent reconnect and single retry
		if errors.Is(err, rlapi.ErrConnectionClosed) || isNetworkOrEOF(err) {
			c.logger.Warn("psynet connection dropped during match query; attempting transparent reconnect", slog.Any("err", err))

			c.mu.Lock()
			if c.closed {
				c.mu.Unlock()
				return nil, ErrClientClosed
			}
			if recErr := c.connectLocked(ctx); recErr != nil {
				c.mu.Unlock()
				return nil, fmt.Errorf("query failed (%v) and reconnect failed: %w", err, recErr)
			}
			retryRPC := c.rpc
			c.mu.Unlock()

			// Retry query once
			entries, err = retryRPC.GetMatchHistory(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, fmt.Errorf("query failed after reconnect: %w", err)
			}
		} else {
			return nil, fmt.Errorf("psynet GetMatchHistory failed: %w", err)
		}
	}

	// 3. Map entries to DiscoveredMatch
	return c.mapMatches(entries), nil
}

// Close gracefully closes the PsyNet WebSocket connection and stops keep-alive pings.
func (c *Client) Close() error {
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

// IsConnected returns whether the underlying WebSocket RPC connection is currently active.
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rpc != nil && c.rpc.IsConnected() && !c.closed
}

func (c *Client) connectLocked(ctx context.Context) error {
	if c.closed {
		return ErrClientClosed
	}

	// Close old socket if any lingering
	if c.rpc != nil {
		_ = c.rpc.Close()
		c.rpc = nil
	}

	creds, err := c.resolveCredentials(ctx)
	if err != nil {
		return err
	}

	// If a custom RPCFactory is provided (e.g. for testing), use it
	if c.cfg.RPCFactory != nil {
		rpc, err := c.cfg.RPCFactory(ctx, creds)
		if err != nil {
			return err
		}
		c.rpc = rpc
		return nil
	}

	// Use real rlapi.PsyNet
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
			return ErrMissingSteamAccount
		}
		rpc, err = c.psyNet.AuthPlayerSteam(creds.AuthToken, creds.AccountID, creds.SteamAccountID, creds.DisplayName)
		if err != nil {
			return fmt.Errorf("steam auth player failed: %w", err)
		}
	default:
		return fmt.Errorf("%w: %q", ErrInvalidPlatform, creds.Platform)
	}

	c.rpc = rpc
	c.logger.Info("connected to psynet websocket rpc",
		slog.String("platform", creds.Platform),
		slog.String("account_id", creds.AccountID),
	)
	return nil
}

func (c *Client) resolveCredentials(ctx context.Context) (*Credentials, error) {
	if c.cfg.CredentialsSupplier != nil {
		creds, err := c.cfg.CredentialsSupplier.GetCredentials(ctx)
		if err != nil {
			return nil, fmt.Errorf("resolving credentials: %w", err)
		}
		if creds != nil {
			return creds, nil
		}
	}
	if c.cfg.Credentials != nil {
		return c.cfg.Credentials, nil
	}
	return nil, ErrMissingCredentials
}

func (c *Client) mapMatches(entries []rlapi.MatchEntry) []DiscoveredMatch {
	results := make([]DiscoveredMatch, 0, len(entries))
	for _, entry := range entries {
		guid := strings.TrimSpace(entry.Match.MatchGUID)
		if guid == "" {
			c.logger.Warn("skipping match entry with empty MatchGUID")
			continue
		}

		timestamp := entry.Match.RecordStartTimestamp
		if timestamp == 0 {
			c.logger.Debug("match has zero RecordStartTimestamp; using current timestamp", slog.String("guid", guid))
			timestamp = time.Now().Unix()
		}

		replayURL := strings.TrimSpace(entry.ReplayUrl)
		if replayURL == "" {
			c.logger.Debug("match discovered with pending/empty replay url (delayed url arrival)",
				slog.String("guid", guid),
				slog.Int("playlist", entry.Match.Playlist),
				slog.String("map", entry.Match.MapName),
			)
		}

		results = append(results, DiscoveredMatch{
			MatchGUID:            guid,
			RecordStartTimestamp: timestamp,
			MapName:              entry.Match.MapName,
			Playlist:             entry.Match.Playlist,
			ReplayURL:            replayURL,
		})
	}
	return results
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
