package psynet

import (
	"context"
	"errors"
	"log/slog"
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

// DefaultRequestTimeout is the default upper bound for PsyNet auth handshakes and RPC calls.
const DefaultRequestTimeout = 30 * time.Second

// ClientConfig configures the PsyNet client.
type ClientConfig struct {
	Credentials         *Credentials
	CredentialsSupplier CredentialsSupplier
	GameVersion         string
	FeatureSet          string
	Logger              *slog.Logger
	PsyNet              *rlapi.PsyNet
	RPCFactory          RPCFactory
	RequestTimeout      time.Duration
}
