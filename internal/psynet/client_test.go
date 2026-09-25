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
	mu         sync.Mutex
	matches    []rlapi.MatchEntry
	err        error
	queryCount int
	connected  bool
	closed     bool
	onQuery    func(ctx context.Context) ([]rlapi.MatchEntry, error)
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
