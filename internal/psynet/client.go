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

// Client implements MatchHistoryProvider interfacing with PsyNet via rlapi.
type Client struct {
	mu             sync.Mutex
	cfg            ClientConfig
	psyNet         *rlapi.PsyNet
	rpc            RPCClient
	closed         bool
	logger         *slog.Logger
	requestTimeout time.Duration
	baseCtx        context.Context
	baseCancel     context.CancelFunc
}

// NewClient creates a new PsyNet Client.
func NewClient(cfg ClientConfig) (*Client, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = DefaultRequestTimeout
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

	baseCtx, baseCancel := context.WithCancel(context.Background())

	return &Client{
		cfg:            cfg,
		psyNet:         psy,
		logger:         logger,
		requestTimeout: timeout,
		baseCtx:        baseCtx,
		baseCancel:     baseCancel,
	}, nil
}

// NewClientWithRPC creates a Client wrapping an existing RPCClient instance (primarily for tests).
func NewClientWithRPC(rpc RPCClient, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	baseCtx, baseCancel := context.WithCancel(context.Background())
	return &Client{
		rpc:            rpc,
		logger:         logger,
		requestTimeout: DefaultRequestTimeout,
		baseCtx:        baseCtx,
		baseCancel:     baseCancel,
	}
}

// GetRecentMatches retrieves the recent match history from PsyNet.
// If the connection is down, it attempts to connect or reconnect automatically.
func (c *Client) GetRecentMatches(ctx context.Context) ([]DiscoveredMatch, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrClientClosed
	}

	// 1. Ensure connected
	if c.rpc == nil || !c.rpc.IsConnected() {
		if err := c.connectLocked(ctx); err != nil {
			c.mu.Unlock()
			if errors.Is(err, ErrClientClosed) {
				return nil, ErrClientClosed
			}
			return nil, fmt.Errorf("failed to connect to psynet: %w", err)
		}
	}
	activeRPC := c.rpc
	c.mu.Unlock()

	// 2. Query match history with bounded timeout
	queryCtx, cancelQuery := c.withRequestTimeout(ctx)
	entries, err := activeRPC.GetMatchHistory(queryCtx)
	queryCtxErr := queryCtx.Err()
	cancelQuery()
	if err != nil {
		if mapped := c.checkInterrupted(ctx, queryCtxErr); mapped != nil {
			return nil, mapped
		}

		// Check for connection drop; attempt transparent reconnect and single retry
		if errors.Is(err, rlapi.ErrConnectionClosed) || isNetworkOrEOF(err) {
			c.logger.Warn("psynet connection dropped during match query; attempting transparent reconnect", slog.Any("err", err))

			c.mu.Lock()
			if c.isClosingLocked() {
				c.mu.Unlock()
				return nil, ErrClientClosed
			}
			if recErr := c.connectLocked(ctx); recErr != nil {
				c.mu.Unlock()
				if errors.Is(recErr, ErrClientClosed) {
					return nil, ErrClientClosed
				}
				return nil, fmt.Errorf("query failed (%v) and reconnect failed: %w", err, recErr)
			}
			retryRPC := c.rpc
			c.mu.Unlock()

			// Retry query once with fresh bounded timeout
			retryCtx, cancelRetry := c.withRequestTimeout(ctx)
			entries, err = retryRPC.GetMatchHistory(retryCtx)
			retryCtxErr := retryCtx.Err()
			cancelRetry()
			if err != nil {
				if mapped := c.checkInterrupted(ctx, retryCtxErr); mapped != nil {
					return nil, mapped
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

// Close gracefully closes the PsyNet WebSocket connection and cancels in-flight handshakes.
func (c *Client) Close() error {
	if c.baseCancel != nil {
		c.baseCancel()
	}

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

func (c *Client) isClosingLocked() bool {
	return c.closed || (c.baseCtx != nil && c.baseCtx.Err() != nil)
}

func (c *Client) isClosed() bool {
	if c.baseCtx != nil && c.baseCtx.Err() != nil {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *Client) withRequestTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	timeout := c.requestTimeout
	if timeout <= 0 {
		timeout = DefaultRequestTimeout
	}
	ctx, cancelTimeout := context.WithTimeout(parent, timeout)
	if c.baseCtx == nil {
		return ctx, cancelTimeout
	}
	stopClose := context.AfterFunc(c.baseCtx, cancelTimeout)
	return ctx, func() {
		stopClose()
		cancelTimeout()
	}
}

func (c *Client) checkInterrupted(parentCtx context.Context, opCtxErr error) error {
	if parentCtx != nil && parentCtx.Err() != nil {
		return parentCtx.Err()
	}
	if c.isClosed() {
		return ErrClientClosed
	}
	if opCtxErr != nil {
		return opCtxErr
	}
	return nil
}

func (c *Client) connectLocked(parentCtx context.Context) error {
	if c.isClosingLocked() {
		return ErrClientClosed
	}

	// Close old socket if any lingering
	if c.rpc != nil {
		_ = c.rpc.Close()
		c.rpc = nil
	}

	ctx, cancel := c.withRequestTimeout(parentCtx)
	defer cancel()

	creds, err := c.resolveCredentials(ctx)
	if err != nil {
		if c.isClosingLocked() {
			return ErrClientClosed
		}
		return err
	}

	// If a custom RPCFactory is provided (e.g. for testing), use it
	if c.cfg.RPCFactory != nil {
		rpc, err := c.cfg.RPCFactory(ctx, creds)
		if err != nil {
			if c.isClosingLocked() {
				return ErrClientClosed
			}
			return err
		}
		if c.isClosingLocked() {
			if rpc != nil {
				_ = rpc.Close()
			}
			return ErrClientClosed
		}
		c.rpc = rpc
		return nil
	}

	// Use real rlapi.PsyNet inside cancel-aware wrapper so synchronous auth never hangs
	platform := strings.ToLower(strings.TrimSpace(creds.Platform))
	var authFn func() (*rlapi.PsyNetRPC, error)
	var authLabel string

	switch platform {
	case "epic", "":
		authLabel = "epic"
		authFn = func() (*rlapi.PsyNetRPC, error) {
			return c.psyNet.AuthPlayer(creds.AuthToken, creds.AccountID, creds.DisplayName)
		}
	case "steam":
		if creds.SteamAccountID == "" {
			return ErrMissingSteamAccount
		}
		authLabel = "steam"
		authFn = func() (*rlapi.PsyNetRPC, error) {
			return c.psyNet.AuthPlayerSteam(creds.AuthToken, creds.AccountID, creds.SteamAccountID, creds.DisplayName)
		}
	default:
		return fmt.Errorf("%w: %q", ErrInvalidPlatform, creds.Platform)
	}

	rpc, err := authWithContext(ctx, authFn)
	if err != nil {
		if c.isClosingLocked() {
			return ErrClientClosed
		}
		return fmt.Errorf("%s auth player failed: %w", authLabel, err)
	}

	if c.isClosingLocked() {
		if rpc != nil {
			_ = rpc.Close()
		}
		return ErrClientClosed
	}

	c.rpc = rpc
	c.logger.Info("connected to psynet websocket rpc",
		slog.String("platform", creds.Platform),
		slog.String("account_id", creds.AccountID),
	)
	return nil
}

type authResult struct {
	rpc *rlapi.PsyNetRPC
	err error
}

// authWithContext bounds synchronous rlapi auth handshakes with ctx and ensures
// any late-completing PsyNetRPC connection is closed immediately on abort.
func authWithContext(ctx context.Context, authFn func() (*rlapi.PsyNetRPC, error)) (*rlapi.PsyNetRPC, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var mu sync.Mutex
	aborted := false
	ch := make(chan authResult, 1)

	go func() {
		rpc, err := authFn()
		mu.Lock()
		defer mu.Unlock()
		if aborted {
			if rpc != nil {
				_ = rpc.Close()
			}
			return
		}
		ch <- authResult{rpc: rpc, err: err}
	}()

	select {
	case res := <-ch:
		return res.rpc, res.err
	case <-ctx.Done():
		mu.Lock()
		aborted = true
		mu.Unlock()
		select {
		case res := <-ch:
			if res.rpc != nil {
				_ = res.rpc.Close()
			}
		default:
		}
		return nil, ctx.Err()
	}
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
