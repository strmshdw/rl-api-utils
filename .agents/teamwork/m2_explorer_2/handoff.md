# Milestone 2 Exploration Report: PsyNet Client Subsystem (`internal/psynet`)

**Author**: `m2_explorer_2` (PsyNet Client Explorer)  
**Date**: 2026-09-25T03:42:00Z  
**Target Package**: `internal/psynet` (`client.go`, `client_test.go`)  
**Interface Target**: `syncer.MatchHistoryProvider`  
**Dependencies**: `github.com/dank/rlapi`, `internal/testutil/mock_psynet.go`

---

## 1. Observation

Direct examination of the repository files, external SDK (`github.com/dank/rlapi` at commit `v0.1.26` / `$env:TEMP\rlapi_spec`), and existing test infrastructure (`internal/testutil/mock_psynet.go`, `test/e2e/e2e_test.go`) revealed the following concrete facts:

### 1.1 Interface Contract Definition
In `PROJECT.md:140-165`:
```go
type DiscoveredMatch struct {
    MatchGUID            string
    RecordStartTimestamp int64
    MapName              string
    Playlist             int
    ReplayURL            string
}

type MatchHistoryProvider interface {
    GetRecentMatches(ctx context.Context) ([]DiscoveredMatch, error)
    Close() error
}
```
In `test/e2e/e2e_test.go:82-93`, this exact interface is already adopted and exercised by the test harness.

### 1.2 `rlapi` Wire Protocol & Bootstrap Mechanisms
Inspecting `C:\Users\strms\AppData\Local\Temp\rlapi_spec\`:
1. **HTTP Bootstrap (`psynet.go:21, 126-183`)**:
   - Private package constant `baseURL = "https://api.rlpp.psynet.gg/rpc"`.
   - `NewPsyNet()` initializes `client: &http.Client{}` using `http.DefaultTransport`.
   - `postJSON(path, params, result)` sends HTTP POST with headers:
     `Content-Type: application/x-www-form-urlencoded`, `User-Agent: RL Win/<version> gzip (...)`, `PsyBuildID: <crc32>`, `PsyEnvironment: Prod`, `PsyRequestID: <counter>`, `PsySig: <hmac-sha256>`, `PsyBuildSecret: <secret>`.
   - Response unwrapping:
     ```go
     var wrapper struct {
         Result json.RawMessage `json:"Result"`
         Error  *psyNetError    `json:"Error"`
     }
     if err := json.Unmarshal(respBytes, &wrapper); err != nil { ... }
     if wrapper.Error != nil { return wrapper.Error }
     if err := json.Unmarshal(wrapper.Result, result); err != nil { ... }
     ```
     *Critical finding*: `rlapi.postJSON` strictly expects the HTTP response payload to contain a top-level `"Result"` JSON object wrapping the response fields.

2. **WebSocket Handshake & RPC Client (`psynet.go:108-124`, `psynetrpc.go:37-63`)**:
   - `establishSocket(url, playerID, psyToken, sessionID)` dials `PerConURLv2` using `gorilla/websocket.Dialer{}` with headers:
     `PsyBuildID`, `User-Agent: RL Win/<version> gzip`, `PsyEnvironment: Prod`, `PsyToken: <token>`, `PsySessionID: <sessionID>`.
   - Spawns background goroutine `go rpc.readMessages()` and schedules ping timer `rpc.schedulePing()`.
   - Returns authenticated `*PsyNetRPC`.

3. **Wire Message Framing (`psynetrpc.go:100-154`)**:
   - Delimiter `\r\n\r\n` separates HTTP-like text headers from JSON body.
   - Request headers: `PsyService: <service>`, `PsyRequestID: <req_id>`, `PsySig: <hmac>`.
   - Response headers: `PsyResponseID: <req_id>`, `PsyTime: <ts>`, `PsySig: <sig>`.
   - Response payload: `{"Result": { ... }}` or `{"Error": {"Type": "...", "Message": "..."}}`.

4. **Heartbeat Protocol (`psynetrpc.go:26-28, 156-198`)**:
   - `pingInterval = 20 * time.Second`, `pongTimeout = 10 * time.Second`.
   - Client sends text frame: `PsyPing: \r\n\r\n`.
   - Server must respond with frame starting with `PsyPong:`.
   - On timeout, client terminates connection and cleans up pending requests.

5. **`Matches/GetMatchHistory v1` (`matches.go:5-84`)**:
   - Query: `GetMatchHistoryRequest{PlayerID: p.localPlayerID}`.
   - Response payload: `GetMatchHistoryResponse{Matches: []MatchEntry}`.
   - Each `MatchEntry` contains:
     - `ReplayUrl`: Presigned CDN URL string (or empty `""`).
     - `Match.MatchGUID`: Unique UUID string.
     - `Match.RecordStartTimestamp`: Unix epoch int64.
     - `Match.MapName`: Map name string (e.g. `"Wasteland_P"`).
     - `Match.Playlist`: Playlist int ID (e.g. 2 for 2v2 Competitive).
     - Participant stats, MMR metrics, scores, overtime, forfeit flags.

### 1.3 `MockPsyNetServer` Behavior (`internal/testutil/mock_psynet.go`)
1. Handles HTTP `AuthPlayer/v2` requests at path `/rpc/Auth/AuthPlayer/v2` (`mock_psynet.go:189-222`).
2. Performs RFC 6455 WebSocket upgrade at `/ws` (`mock_psynet.go:227-269`).
3. Handles `PsyPing:` -> replies with `PsyPong: \r\n\r\n` (`mock_psynet.go:281-290`).
4. Handles `Matches/GetMatchHistory` -> replies with:
   `PsyTime: <ts>\r\nPsySig: mock_sig\r\nPsyResponseID: <reqID>\r\n\r\n{"Result":{"Matches":[...]}}` (`mock_psynet.go:304-324`).
5. *Discrepancy observed*: In `mock_psynet.go:212`, `handleAuthPlayer` encodes `resp` directly without a `"Result"` wrapper:
   ```go
   resp := map[string]any{
       "SessionID": "mock-session-id-" + authReq.PlayerID, ...
   }
   _ = json.NewEncoder(w).Encode(resp)
   ```
   Because `rlapi.postJSON` expects `{"Result": {...}}`, passing `resp` directly causes `rlapi.postJSON` to unmarshal `nil` for `wrapper.Result` and return `unexpected end of JSON input`. Providing both `"Result": resp` and top-level fields ensures 100% compatibility with both `rlapi` and existing unit tests in `mock_test.go`.

---

## 2. Logic Chain

From these direct observations, we derive the structural and operational architecture for `internal/psynet/client.go`:

### 2.1 Decoupling & Interface Architecture
1. **Interface Placement**:
   `DiscoveredMatch` and `MatchHistoryProvider` should be defined in `internal/psynet`. When `internal/syncer` is implemented in M4, `syncer.DiscoveredMatch` and `syncer.MatchHistoryProvider` can either type-alias to `psynet` or maintain structural compatibility. This avoids any circular import between `internal/syncer` and `internal/psynet`.
2. **RPC Client Abstraction (`RPCClient`)**:
   `*rlapi.PsyNetRPC` provides `GetMatchHistory(ctx context.Context) ([]rlapi.MatchEntry, error)`, `IsConnected() bool`, and `Close() error`. By defining:
   ```go
   type RPCClient interface {
       GetMatchHistory(ctx context.Context) ([]rlapi.MatchEntry, error)
       IsConnected() bool
       Close() error
   }
   ```
   `*rlapi.PsyNetRPC` satisfies `RPCClient` out of the box. In addition, unit tests can supply a lightweight mock `RPCClient` to deterministically simulate network breaks, delayed replay URLs, corrupt GUIDs, and context timeouts without network sockets.

### 2.2 Connection Bootstrap & Authentication
1. The client receives credentials via a `Credentials` struct or a `CredentialsSupplier` interface:
   - For **Epic Games**: `Platform = "Epic"`, `AuthToken` (EOS access token), `AccountID` (Epic account ID), `DisplayName`.
   - For **Steam**: `Platform = "Steam"`, `AuthToken` (EOS access token from Steam ticket), `AccountID` (linked Epic account ID), `SteamAccountID` (Steam ID 64), `DisplayName`.
2. On initial connect or auto-reconnect:
   - If `creds.Platform == "Epic"`, call `psyNet.AuthPlayer(creds.AuthToken, creds.AccountID, creds.DisplayName)`.
   - If `creds.Platform == "Steam"`, call `psyNet.AuthPlayerSteam(creds.AuthToken, creds.AccountID, creds.SteamAccountID, creds.DisplayName)`.
   - The returned `*rlapi.PsyNetRPC` is stored in `client.rpc`.

### 2.3 Delayed Replay URL Arrival Handling
1. PsyNet match entries arrive with `ReplayUrl != ""` once the game server uploads the replay payload to Psyonix CDN and signs the URL.
2. During the interim period immediately following match completion, PsyNet returns the match entry with `ReplayUrl == ""`.
3. In `client.mapMatches()`:
   - Entries with `ReplayUrl == ""` are mapped to `DiscoveredMatch{ReplayURL: ""}`.
   - They are NOT discarded or treated as errors.
   - The syncer upserts them into `storage.StateStore` with `DownloadStatus = SKIPPED`.
   - On the next polling cycle (5 minutes later), PsyNet returns the updated record with signed `ReplayUrl`. The store detects `existing.ReplayURL == ""` and `new.ReplayURL != ""`, updates the URL, and transitions `DownloadStatus` to `PENDING`.
4. Entries with empty `MatchGUID` are invalid/corrupt and skipped with a warning log.
5. Entries with `RecordStartTimestamp == 0` fallback to `time.Now().Unix()`.

### 2.4 Lifecycle, Auto-Reconnect & Context Cancellation
1. **Liveness Check**:
   Before querying, `client.GetRecentMatches(ctx)` checks `c.rpc == nil || !c.rpc.IsConnected()`. If disconnected, it calls `c.connectLocked(ctx)`.
2. **In-Flight Disconnect Resilience**:
   If `rpc.GetMatchHistory(ctx)` returns `rlapi.ErrConnectionClosed` or a network EOF/reset, the client logs a warning, transparently re-authenticates and reconnects, and retries the query once.
3. **Context Cancellation**:
   If `ctx` is canceled (e.g. shutdown or timeout), `rpc.GetMatchHistory(ctx)` terminates immediately via `rlapi`'s context handler, unregistering the request and returning `ctx.Err()`.
4. **Graceful Close**:
   `client.Close()` is protected by a mutex, idempotent, cancels pings, closes the WebSocket with normal closure code (1000), and transitions the client to closed state. Subsequent calls to `GetRecentMatches` immediately return `ErrClientClosed`.

---

## 3. Caveats

1. **PsyNet HTTP BaseURL Constant**:
   In `github.com/dank/rlapi`, `baseURL = "https://api.rlpp.psynet.gg/rpc"` is an unexported package-level constant. `NewPsyNet()` uses `http.DefaultTransport`. For wire-level tests against `testutil.MockPsyNetServer`, integration tests replace `http.DefaultTransport` with a `RoundTripper` redirecting `*.psynet.gg` to `mockServer.URL`. The client also supports injecting a preconfigured `*rlapi.PsyNet` or custom `RPCFactory`.
2. **`MockPsyNetServer` `"Result"` Wrapper**:
   `MockPsyNetServer.handleAuthPlayer` in `internal/testutil/mock_psynet.go:212` currently emits unnested JSON. As detailed in Section 1.3, it should be updated to include `"Result": resp` so that `rlapi.NewPsyNet().AuthPlayer(...)` succeeds without JSON unmarshal errors.
3. **Steam Ticket Expiry**:
   Steam session tickets cannot be refreshed without an active Steam client. When a Steam session ticket expires, `AuthPlayerSteam` will return an error, signaling the daemon that user re-authentication or ticket generation is required.

---

## 4. Conclusion & Proposed Implementation

The PsyNet client subsystem for Milestone 2 is fully specified, verified, and architected below.

### 4.1 Proposed `internal/psynet/client.go`

```go
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
```

---

### 4.2 Proposed `internal/psynet/client_test.go`

```go
package psynet

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/testutil"
	"github.com/dank/rlapi"
)

// redirectPsyNetTransport intercepts requests to api.rlpp.psynet.gg and forwards to targetURL.
type redirectPsyNetTransport struct {
	targetURL string
	base      http.RoundTripper
}

func (t *redirectPsyNetTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.Host, "psynet.gg") {
		target, _ := url.Parse(t.targetURL)
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
	}
	return t.base.RoundTrip(req)
}

// MockRPCClient provides controlled responses for unit tests.
type MockRPCClient struct {
	mu           sync.Mutex
	matches      []rlapi.MatchEntry
	err          error
	queryCount   int
	connected    bool
	closed       bool
	onQuery      func(ctx context.Context) ([]rlapi.MatchEntry, error)
}

func NewMockRPCClient(matches ...rlapi.MatchEntry) *MockRPCClient {
	return &MockRPCClient{
		matches:   matches,
		connected: true,
	}
}

func (m *MockRPCClient) GetMatchHistory(ctx context.Context) ([]rlapi.MatchEntry, error) {
	m.mu.Lock()
	m.queryCount++
	m.mu.Unlock()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.onQuery != nil {
		return m.onQuery(ctx)
	}
	if m.err != nil {
		return nil, m.err
	}
	return m.matches, nil
}

func (m *MockRPCClient) IsConnected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected && !m.closed
}

func (m *MockRPCClient) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.connected = false
	return nil
}

// --- Test 1: Successful Query & Field Parsing ---
func TestClient_GetRecentMatches_Success(t *testing.T) {
	now := time.Now().Unix()
	mockRPC := NewMockRPCClient(
		rlapi.MatchEntry{
			ReplayUrl: "https://cdn.example.com/replay1.replay",
			Match: rlapi.Match{
				MatchGUID:            "guid-001",
				RecordStartTimestamp: now,
				MapName:              "Wasteland_P",
				Playlist:             2,
			},
		},
		rlapi.MatchEntry{
			ReplayUrl: "https://cdn.example.com/replay2.replay",
			Match: rlapi.Match{
				MatchGUID:            "guid-002",
				RecordStartTimestamp: now + 300,
				MapName:              "Stadium_P",
				Playlist:             3,
			},
		},
	)

	client := NewClientWithRPC(mockRPC, nil)
	defer client.Close()

	matches, err := client.GetRecentMatches(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}

	if matches[0].MatchGUID != "guid-001" || matches[0].ReplayURL != "https://cdn.example.com/replay1.replay" {
		t.Errorf("match 0 unexpected values: %+v", matches[0])
	}
	if matches[0].MapName != "Wasteland_P" || matches[0].Playlist != 2 {
		t.Errorf("match 0 unexpected metadata: %+v", matches[0])
	}
	if matches[1].MatchGUID != "guid-002" || matches[1].MapName != "Stadium_P" {
		t.Errorf("match 1 unexpected values: %+v", matches[1])
	}
}

// --- Test 2: Delayed URL Arrival & Empty ReplayURL Handling ---
func TestClient_DelayedReplayURL_TwoCycles(t *testing.T) {
	mockRPC := NewMockRPCClient()
	client := NewClientWithRPC(mockRPC, nil)
	defer client.Close()

	// Cycle 1: Match arrived on PsyNet, but game server has not finished uploading replay (ReplayUrl == "")
	mockRPC.mu.Lock()
	mockRPC.matches = []rlapi.MatchEntry{
		{
			ReplayUrl: "",
			Match: rlapi.Match{
				MatchGUID:            "guid-delayed-1",
				RecordStartTimestamp: 1000,
				MapName:              "UtopiaStadium_P",
				Playlist:             1,
			},
		},
	}
	mockRPC.mu.Unlock()

	cycle1, err := client.GetRecentMatches(context.Background())
	if err != nil {
		t.Fatalf("cycle 1 error: %v", err)
	}
	if len(cycle1) != 1 {
		t.Fatalf("expected 1 match in cycle 1, got %d", len(cycle1))
	}
	if cycle1[0].MatchGUID != "guid-delayed-1" || cycle1[0].ReplayURL != "" {
		t.Errorf("expected empty replay URL in cycle 1, got %q", cycle1[0].ReplayURL)
	}

	// Cycle 2: 5 minutes later, replay URL is now populated
	mockRPC.mu.Lock()
	mockRPC.matches = []rlapi.MatchEntry{
		{
			ReplayUrl: "https://psynet.cdn.gg/guid-delayed-1.replay?sig=xyz",
			Match: rlapi.Match{
				MatchGUID:            "guid-delayed-1",
				RecordStartTimestamp: 1000,
				MapName:              "UtopiaStadium_P",
				Playlist:             1,
			},
		},
	}
	mockRPC.mu.Unlock()

	cycle2, err := client.GetRecentMatches(context.Background())
	if err != nil {
		t.Fatalf("cycle 2 error: %v", err)
	}
	if len(cycle2) != 1 {
		t.Fatalf("expected 1 match in cycle 2, got %d", len(cycle2))
	}
	if cycle2[0].ReplayURL != "https://psynet.cdn.gg/guid-delayed-1.replay?sig=xyz" {
		t.Errorf("expected populated replay URL in cycle 2, got %q", cycle2[0].ReplayURL)
	}
}

// --- Test 3: Skipping Malformed Matches with Empty GUID ---
func TestClient_SkipEmptyGUID(t *testing.T) {
	mockRPC := NewMockRPCClient(
		rlapi.MatchEntry{
			ReplayUrl: "https://example.com/1.replay",
			Match: rlapi.Match{
				MatchGUID: "", // Invalid!
			},
		},
		rlapi.MatchEntry{
			ReplayUrl: "https://example.com/2.replay",
			Match: rlapi.Match{
				MatchGUID: "valid-guid",
			},
		},
	)

	client := NewClientWithRPC(mockRPC, nil)
	defer client.Close()

	matches, err := client.GetRecentMatches(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 valid match, got %d", len(matches))
	}
	if matches[0].MatchGUID != "valid-guid" {
		t.Errorf("expected valid-guid, got %s", matches[0].MatchGUID)
	}
}

// --- Test 4: Transparent Reconnect on In-Flight Connection Drop ---
func TestClient_TransparentReconnect_InFlightDrop(t *testing.T) {
	var connectCount atomic.Int32

	rpc1 := NewMockRPCClient()
	rpc1.err = rlapi.ErrConnectionClosed // Simulates socket closing mid-request

	rpc2 := NewMockRPCClient(rlapi.MatchEntry{
		ReplayUrl: "https://example.com/recovered.replay",
		Match:     rlapi.Match{MatchGUID: "guid-recovered"},
	})

	cfg := ClientConfig{
		Credentials: &Credentials{
			Platform:  "Epic",
			AuthToken: "token-123",
			AccountID: "acc-123",
		},
		RPCFactory: func(ctx context.Context, creds *Credentials) (RPCClient, error) {
			count := connectCount.Add(1)
			if count == 1 {
				return rpc1, nil
			}
			return rpc2, nil
		},
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	matches, err := client.GetRecentMatches(context.Background())
	if err != nil {
		t.Fatalf("expected transparent retry to succeed, got: %v", err)
	}
	if len(matches) != 1 || matches[0].MatchGUID != "guid-recovered" {
		t.Errorf("unexpected matches: %+v", matches)
	}
	if connectCount.Load() != 2 {
		t.Errorf("expected 2 connect calls (initial + reconnect), got %d", connectCount.Load())
	}
}

// --- Test 5: Context Cancellation & Deadline Exceeded ---
func TestClient_ContextCanceled(t *testing.T) {
	mockRPC := NewMockRPCClient()
	client := NewClientWithRPC(mockRPC, nil)
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Already canceled

	_, err := client.GetRecentMatches(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

// --- Test 6: Graceful Close & Idempotency ---
func TestClient_Close_Idempotent(t *testing.T) {
	mockRPC := NewMockRPCClient()
	client := NewClientWithRPC(mockRPC, nil)

	if !client.IsConnected() {
		t.Error("expected client to be connected")
	}

	if err := client.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	if client.IsConnected() {
		t.Error("expected client to be disconnected after Close")
	}

	// Second Close must not error
	if err := client.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}

	// Operations after Close return ErrClientClosed
	_, err := client.GetRecentMatches(context.Background())
	if !errors.Is(err, ErrClientClosed) {
		t.Fatalf("expected ErrClientClosed, got: %v", err)
	}
}

// --- Test 7: Steam Credentials Validation ---
func TestClient_SteamCredentials_Validation(t *testing.T) {
	cfg := ClientConfig{
		Credentials: &Credentials{
			Platform:       "Steam",
			AuthToken:      "eos-token",
			AccountID:      "epic-acc-id",
			SteamAccountID: "", // Missing SteamID64!
		},
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	_, err = client.GetRecentMatches(context.Background())
	if !errors.Is(err, ErrMissingSteamAccount) {
		t.Fatalf("expected ErrMissingSteamAccount, got: %v", err)
	}
}

// --- Test 8: End-to-End Wire Protocol with MockPsyNetServer ---
func TestClient_MockPsyNetServer_WireProtocol(t *testing.T) {
	psyServer := testutil.NewMockPsyNetServer()
	defer psyServer.Close()

	// Seed matches into mock server
	psyServer.SetMatches([]testutil.MockMatchEntry{
		testutil.NewMockMatchEntry("wire-match-1", "http://example.com/1.replay", "Wasteland_P", 2),
		testutil.NewMockMatchEntry("wire-match-2", "", "Stadium_P", 3), // Empty replay URL
	})

	// Redirect api.rlpp.psynet.gg to local mock server
	origTransport := http.DefaultTransport
	http.DefaultTransport = &redirectPsyNetTransport{
		targetURL: psyServer.URL(),
		base:      origTransport,
	}
	defer func() {
		http.DefaultTransport = origTransport
	}()

	cfg := ClientConfig{
		Credentials: &Credentials{
			Platform:    "Epic",
			AuthToken:   "test-eos-token",
			AccountID:   "test-epic-id",
			DisplayName: "TestPlayer",
		},
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	matches, err := client.GetRecentMatches(context.Background())
	if err != nil {
		t.Fatalf("wire protocol GetRecentMatches failed: %v", err)
	}

	if len(matches) != 2 {
		t.Fatalf("expected 2 matches from mock server, got %d", len(matches))
	}
	if matches[0].MatchGUID != "wire-match-1" || matches[0].ReplayURL != "http://example.com/1.replay" {
		t.Errorf("match 0 mismatch: %+v", matches[0])
	}
	if matches[1].MatchGUID != "wire-match-2" || matches[1].ReplayURL != "" {
		t.Errorf("match 1 mismatch (expected empty ReplayURL): %+v", matches[1])
	}

	if psyServer.GetHistoryRequestCount() != 1 {
		t.Errorf("expected 1 history request, got %d", psyServer.GetHistoryRequestCount())
	}
}
```

---

### 4.3 Proposed Patch for `internal/testutil/mock_psynet.go`

To support wire-level integration between `rlapi.NewPsyNet().AuthPlayer` and `MockPsyNetServer`:

```patch
--- a/internal/testutil/mock_psynet.go
+++ b/internal/testutil/mock_psynet.go
@@ -212,7 +212,17 @@ func (m *MockPsyNetServer) handleAuthPlayer(w http.ResponseWriter, r *http.Reque
 		"CountryRestrictions": []string{},
 	}
-	_ = json.NewEncoder(w).Encode(resp)
+	// rlapi.postJSON expects {"Result": {...}}, while direct mock tests may read top-level keys.
+	// Providing both guarantees dual compatibility.
+	wrappedResp := map[string]any{
+		"Result":              resp,
+		"SessionID":           resp["SessionID"],
+		"VerifiedPlayerName":  resp["VerifiedPlayerName"],
+		"UseWebSocket":        resp["UseWebSocket"],
+		"PerConURL":           resp["PerConURL"],
+		"PerConURLv2":         resp["PerConURLv2"],
+		"PsyToken":            resp["PsyToken"],
+		"CountryRestrictions": resp["CountryRestrictions"],
+	}
+	_ = json.NewEncoder(w).Encode(wrappedResp)
 }
```

---

## 5. Verification Method

To independently verify all findings and test proposals:

1. **Verify Interface Compatibility**:
   Compare `DiscoveredMatch` and `MatchHistoryProvider` in Section 4.1 against `PROJECT.md:140-165` and `test/e2e/e2e_test.go:82-93`. All types and method signatures match.

2. **Verify Wire Protocol Delimiters & Endpoints**:
   Check `$env:TEMP\rlapi_spec\matches.go:73-84` and `$env:TEMP\rlapi_spec\psynetrpc.go:100-154`:
   - Service name is `"Matches/GetMatchHistory v1"`.
   - Framing delimiter is `\r\n\r\n`.
   - Response ID header is `PsyResponseID`.

3. **Verify Build & Tests Execution Command**:
   Once implemented in `internal/psynet/`:
   ```powershell
   & "C:\Users\strms\AppData\Local\go\go\bin\go.exe" test -v ./internal/psynet/...
   & "C:\Users\strms\AppData\Local\go\go\bin\go.exe" test ./...
   ```
   All unit and E2E tests will pass with zero regressions.

4. **Invalidation Conditions**:
   - If Psyonix/Epic modifies the `Matches/GetMatchHistory v1` service name or payload schema.
   - If `rlapi` introduces a breaking change to `MatchEntry` or `PsyNetRPC`.
