package psynet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rlapi"
)

// TestAdversarial_Client_DelayedReplayURL_Progression verifies matches with empty ReplayUrl
// are preserved until the CDN URL populates in subsequent polling cycles.
func TestAdversarial_Client_DelayedReplayURL_Progression(t *testing.T) {
	mockRPC := NewMockRPCClient()
	client := NewClientWithRPC(mockRPC, nil)
	defer client.Close()

	ctx := context.Background()
	now := time.Now().Unix()

	mockRPC.mu.Lock()
	mockRPC.matches = []rlapi.MatchEntry{
		{
			ReplayUrl: "",
			Match:     rlapi.Match{MatchGUID: "guid-delayed-1", RecordStartTimestamp: now, MapName: "Wasteland_P", Playlist: 2},
		},
		{
			ReplayUrl: "",
			Match:     rlapi.Match{MatchGUID: "guid-delayed-2", RecordStartTimestamp: now + 60, MapName: "Stadium_P", Playlist: 3},
		},
		{
			ReplayUrl: "https://cdn.example.com/3.replay",
			Match:     rlapi.Match{MatchGUID: "guid-ready-3", RecordStartTimestamp: now + 120, MapName: "Utopia_P", Playlist: 2},
		},
		{
			ReplayUrl: "https://cdn.example.com/4.replay",
			Match:     rlapi.Match{MatchGUID: "guid-ready-4", RecordStartTimestamp: now + 180, MapName: "DFHStadium_P", Playlist: 1},
		},
	}
	mockRPC.mu.Unlock()

	cycle1, err := client.GetRecentMatches(ctx)
	if err != nil || len(cycle1) != 4 {
		t.Fatalf("cycle 1 expected 4 matches, got len=%d err=%v", len(cycle1), err)
	}
	if cycle1[0].ReplayURL != "" || cycle1[1].ReplayURL != "" || cycle1[2].ReplayURL == "" {
		t.Fatalf("unexpected cycle 1 ReplayURLs: %+v", cycle1)
	}

	mockRPC.mu.Lock()
	mockRPC.matches = []rlapi.MatchEntry{
		{
			ReplayUrl: "https://cdn.example.com/delayed-1.replay?token=xyz",
			Match:     rlapi.Match{MatchGUID: "guid-delayed-1", RecordStartTimestamp: now, MapName: "Wasteland_P", Playlist: 2},
		},
		{
			ReplayUrl: "",
			Match:     rlapi.Match{MatchGUID: "guid-delayed-2", RecordStartTimestamp: now + 60, MapName: "Stadium_P", Playlist: 3},
		},
		{
			ReplayUrl: "https://cdn.example.com/3.replay",
			Match:     rlapi.Match{MatchGUID: "guid-ready-3", RecordStartTimestamp: now + 120, MapName: "Utopia_P", Playlist: 2},
		},
		{
			ReplayUrl: "https://cdn.example.com/4.replay",
			Match:     rlapi.Match{MatchGUID: "guid-ready-4", RecordStartTimestamp: now + 180, MapName: "DFHStadium_P", Playlist: 1},
		},
		{
			ReplayUrl: "",
			Match:     rlapi.Match{MatchGUID: "guid-new-5", RecordStartTimestamp: now + 240, MapName: "NeoTokyo_P", Playlist: 2},
		},
	}
	mockRPC.mu.Unlock()

	cycle2, err := client.GetRecentMatches(ctx)
	if err != nil || len(cycle2) != 5 {
		t.Fatalf("cycle 2 expected 5 matches, got len=%d err=%v", len(cycle2), err)
	}
	if cycle2[0].ReplayURL != "https://cdn.example.com/delayed-1.replay?token=xyz" {
		t.Errorf("cycle 2: match 1 expected populated URL, got %q", cycle2[0].ReplayURL)
	}
	if cycle2[1].ReplayURL != "" || cycle2[4].ReplayURL != "" {
		t.Errorf("cycle 2: expected delayed/new matches to keep empty URL")
	}
}

// TestAdversarial_Client_MalformedMetadata_Filtering verifies empty GUID filtering and zero-timestamp fallback.
func TestAdversarial_Client_MalformedMetadata_Filtering(t *testing.T) {
	mockRPC := NewMockRPCClient()
	client := NewClientWithRPC(mockRPC, nil)
	defer client.Close()

	ctx := context.Background()

	mockRPC.mu.Lock()
	mockRPC.matches = []rlapi.MatchEntry{
		{ReplayUrl: "https://example.com/1.replay", Match: rlapi.Match{MatchGUID: ""}},
		{ReplayUrl: "https://example.com/2.replay", Match: rlapi.Match{MatchGUID: "    "}},
		{
			ReplayUrl: "https://example.com/3.replay",
			Match:     rlapi.Match{MatchGUID: "valid-guid-001", RecordStartTimestamp: 0, MapName: "Field_P", Playlist: 11},
		},
		{
			ReplayUrl: "https://example.com/4.replay",
			Match:     rlapi.Match{MatchGUID: "valid-guid-002", RecordStartTimestamp: 1700000000, MapName: "", Playlist: 0},
		},
	}
	mockRPC.mu.Unlock()

	matches, err := client.GetRecentMatches(ctx)
	if err != nil || len(matches) != 2 {
		t.Fatalf("expected 2 valid matches after filtering, got %d (err=%v)", len(matches), err)
	}
	if matches[0].MatchGUID != "valid-guid-001" || matches[0].RecordStartTimestamp <= 0 {
		t.Errorf("unexpected first match: %+v", matches[0])
	}
	if matches[1].MatchGUID != "valid-guid-002" {
		t.Errorf("unexpected second match: %+v", matches[1])
	}

	mockRPC.mu.Lock()
	mockRPC.matches = []rlapi.MatchEntry{}
	mockRPC.mu.Unlock()

	emptyMatches, err := client.GetRecentMatches(ctx)
	if err != nil || len(emptyMatches) != 0 {
		t.Fatalf("expected (0, nil) on empty list, got (%d, %v)", len(emptyMatches), err)
	}
}

// TestAdversarial_Client_ConnectionDrop_TransparentReconnect stresses auto-reconnection.
func TestAdversarial_Client_ConnectionDrop_TransparentReconnect(t *testing.T) {
	ctx := context.Background()

	t.Run("Drop_TransparentReconnectSuccess", func(t *testing.T) {
		var connectCalls atomic.Int32
		rpcDrop := NewMockRPCClient()
		rpcDrop.err = rlapi.ErrConnectionClosed

		rpcRecovered := NewMockRPCClient(rlapi.MatchEntry{
			ReplayUrl: "https://example.com/recovered.replay",
			Match:     rlapi.Match{MatchGUID: "guid-rec-001", RecordStartTimestamp: 1000},
		})

		cfg := ClientConfig{
			Credentials: &Credentials{Platform: "Epic", AuthToken: "token-1", AccountID: "acc-1"},
			RPCFactory: func(ctx context.Context, creds *Credentials) (RPCClient, error) {
				if connectCalls.Add(1) == 1 {
					return rpcDrop, nil
				}
				return rpcRecovered, nil
			},
		}

		client, err := NewClient(cfg)
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		defer client.Close()

		matches, err := client.GetRecentMatches(ctx)
		if err != nil || len(matches) != 1 || matches[0].MatchGUID != "guid-rec-001" {
			t.Fatalf("expected transparent retry success, got len=%d err=%v", len(matches), err)
		}
		if connectCalls.Load() != 2 {
			t.Fatalf("expected 2 connect calls, got %d", connectCalls.Load())
		}
	})

	t.Run("EOF_TransparentReconnectSuccess", func(t *testing.T) {
		var connectCalls atomic.Int32
		rpcEOF := NewMockRPCClient()
		rpcEOF.err = io.EOF
		rpcRecovered := NewMockRPCClient(rlapi.MatchEntry{
			ReplayUrl: "https://example.com/recovered-eof.replay",
			Match:     rlapi.Match{MatchGUID: "guid-eof-recovered", RecordStartTimestamp: 2000},
		})

		cfg := ClientConfig{
			Credentials: &Credentials{Platform: "Epic", AuthToken: "tok", AccountID: "acc"},
			RPCFactory: func(ctx context.Context, creds *Credentials) (RPCClient, error) {
				if connectCalls.Add(1) == 1 {
					return rpcEOF, nil
				}
				return rpcRecovered, nil
			},
		}

		client, err := NewClient(cfg)
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		defer client.Close()

		matches, err := client.GetRecentMatches(ctx)
		if err != nil || len(matches) != 1 || matches[0].MatchGUID != "guid-eof-recovered" {
			t.Fatalf("expected EOF reconnect success, got len=%d err=%v", len(matches), err)
		}
	})

	t.Run("Drop_ReconnectAlsoFails", func(t *testing.T) {
		var connectCalls atomic.Int32
		rpcDrop := NewMockRPCClient()
		rpcDrop.err = rlapi.ErrConnectionClosed

		cfg := ClientConfig{
			Credentials: &Credentials{Platform: "Epic", AuthToken: "tok", AccountID: "acc"},
			RPCFactory: func(ctx context.Context, creds *Credentials) (RPCClient, error) {
				if connectCalls.Add(1) == 1 {
					return rpcDrop, nil
				}
				return nil, errors.New("psynet auth server unreachable")
			},
		}

		client, err := NewClient(cfg)
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		defer client.Close()

		_, err = client.GetRecentMatches(ctx)
		if err == nil || !strings.Contains(err.Error(), "reconnect failed") {
			t.Fatalf("expected reconnect failure error, got: %v", err)
		}
	})

	t.Run("ConsecutiveDropsAcrossCycles", func(t *testing.T) {
		var rpcCycleCount atomic.Int32
		cfg := ClientConfig{
			Credentials: &Credentials{Platform: "Epic", AuthToken: "tok", AccountID: "acc"},
			RPCFactory: func(ctx context.Context, creds *Credentials) (RPCClient, error) {
				cycle := rpcCycleCount.Add(1)
				rpc := NewMockRPCClient(rlapi.MatchEntry{
					ReplayUrl: fmt.Sprintf("https://example.com/%d.replay", cycle),
					Match:     rlapi.Match{MatchGUID: fmt.Sprintf("guid-cycle-%d", cycle)},
				})
				if cycle%2 == 1 {
					var dropped atomic.Bool
					rpc.onQuery = func(c context.Context) ([]rlapi.MatchEntry, error) {
						if !dropped.Swap(true) {
							return nil, rlapi.ErrConnectionClosed
						}
						return rpc.matches, nil
					}
				}
				return rpc, nil
			},
		}

		client, err := NewClient(cfg)
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		defer client.Close()

		for cycle := 1; cycle <= 3; cycle++ {
			matches, err := client.GetRecentMatches(ctx)
			if err != nil || len(matches) == 0 {
				t.Fatalf("cycle %d failed: len=%d err=%v", cycle, len(matches), err)
			}
		}
	})
}
