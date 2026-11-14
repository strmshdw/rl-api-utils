package playertrack

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/psynet"
	"github.com/dank/rlapi"
)

// customNetError simulates a net.Error implementation.
type customNetError struct {
	msg     string
	timeout bool
	temp    bool
}

func (e *customNetError) Error() string   { return e.msg }
func (e *customNetError) Timeout() bool   { return e.timeout }
func (e *customNetError) Temporary() bool { return e.temp }

// controllableSkillRPC is a mock SkillRPCClient with programmable behavior.
type controllableSkillRPC struct {
	mu        sync.Mutex
	connected bool
	closed    bool
	callCount int
	skills    map[rlapi.PlayerID][]rlapi.Skill
	failFunc  func(call int, playerIDs []rlapi.PlayerID) error
	delay     time.Duration
}

func (c *controllableSkillRPC) GetPlayersSkills(ctx context.Context, playerIDs []rlapi.PlayerID) ([]rlapi.PlayerWithSkills, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if c.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(c.delay):
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.closed {
		return nil, rlapi.ErrConnectionClosed
	}

	c.callCount++
	if c.failFunc != nil {
		if err := c.failFunc(c.callCount, playerIDs); err != nil {
			return nil, err
		}
	}

	var results []rlapi.PlayerWithSkills
	for _, pid := range playerIDs {
		skills := c.skills[pid]
		results = append(results, rlapi.PlayerWithSkills{
			PlayerID: pid,
			Skills:   skills,
		})
	}
	return results, nil
}

func (c *controllableSkillRPC) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected && !c.closed
}

func (c *controllableSkillRPC) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.connected = false
	return nil
}

// ============================================================================
// Suite 1: Disconnect, EOF, Network Drop & Transparent Single Retry
// ============================================================================

func TestResilience_DisconnectSimulations(t *testing.T) {
	testCases := []struct {
		name          string
		dropError     error
		shouldRetry   bool
		expectedCalls int // factory creation count
	}{
		{
			name:          "WebSocket closure rlapi.ErrConnectionClosed",
			dropError:     rlapi.ErrConnectionClosed,
			shouldRetry:   true,
			expectedCalls: 2,
		},
		{
			name:          "io.EOF error",
			dropError:     io.EOF,
			shouldRetry:   true,
			expectedCalls: 2,
		},
		{
			name:          "net.OpError disconnect",
			dropError:     &net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection reset by peer")},
			shouldRetry:   true,
			expectedCalls: 2,
		},
		{
			name:          "custom net.Error",
			dropError:     &customNetError{msg: "network timeout", timeout: true},
			shouldRetry:   true,
			expectedCalls: 2,
		},
		{
			name:          "string containing connection reset",
			dropError:     errors.New("read tcp 127.0.0.1: connection reset"),
			shouldRetry:   true,
			expectedCalls: 2,
		},
		{
			name:          "string containing broken pipe",
			dropError:     errors.New("write: broken pipe"),
			shouldRetry:   true,
			expectedCalls: 2,
		},
		{
			name:          "string containing closed",
			dropError:     errors.New("use of closed network connection"),
			shouldRetry:   true,
			expectedCalls: 2,
		},
		{
			name:          "non-network application error (PsyNet 429)",
			dropError:     errors.New("PsyNet: HTTP 429 Too Many Requests"),
			shouldRetry:   false,
			expectedCalls: 1, // Must NOT attempt reconnect
		},
		{
			name:          "non-network validation error",
			dropError:     errors.New("invalid player ID format"),
			shouldRetry:   false,
			expectedCalls: 1, // Must NOT attempt reconnect
		},
	}

	testPID := rlapi.PlayerID("Epic|test-player|0")

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var factoryCount int32
			p1Skills := []rlapi.Skill{{Playlist: 11, Tier: 17, Division: 2, MMR: 1120.0}}

			client, err := NewPsyNetRankClient(RankClientConfig{
				CredentialsSupplier: psynet.StaticCredentials{
					Platform:  "Epic",
					AuthToken: "test-auth-token",
					AccountID: "test-account-id",
				},
				RPCFactory: func(ctx context.Context, creds *psynet.Credentials) (SkillRPCClient, error) {
					idx := atomic.AddInt32(&factoryCount, 1)
					rpc := &controllableSkillRPC{
						connected: true,
						skills: map[rlapi.PlayerID][]rlapi.Skill{
							testPID: p1Skills,
						},
					}
					if idx == 1 {
						// First connection fails on first call with tc.dropError
						rpc.failFunc = func(call int, playerIDs []rlapi.PlayerID) error {
							return tc.dropError
						}
					}
					return rpc, nil
				},
			})
			if err != nil {
				t.Fatalf("NewPsyNetRankClient failed: %v", err)
			}
			defer client.Close()

			res, err := client.GetPlayersSkills(context.Background(), []rlapi.PlayerID{testPID})

			if tc.shouldRetry {
				if err != nil {
					t.Fatalf("expected transparent retry to succeed, got error: %v", err)
				}
				if len(res) != 1 || res[0].Skills[0].Tier != 17 {
					t.Fatalf("expected 1 result with Tier 17, got %+v", res)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tc.dropError)
				}
			}

			actualCalls := atomic.LoadInt32(&factoryCount)
			if int(actualCalls) != tc.expectedCalls {
				t.Errorf("factory called %d times; want %d", actualCalls, tc.expectedCalls)
			}
		})
	}
}

// TestResilience_PersistentDropDoesNotLoopInfinitely verifies that if the reconnected
// client also fails with a network drop, the client halts after exactly one retry.
func TestResilience_PersistentDropDoesNotLoopInfinitely(t *testing.T) {
	var factoryCount int32
	client, err := NewPsyNetRankClient(RankClientConfig{
		CredentialsSupplier: psynet.StaticCredentials{
			Platform:  "Epic",
			AuthToken: "tok",
			AccountID: "acc",
		},
		RPCFactory: func(ctx context.Context, creds *psynet.Credentials) (SkillRPCClient, error) {
			atomic.AddInt32(&factoryCount, 1)
			return &controllableSkillRPC{
				connected: true,
				failFunc: func(call int, playerIDs []rlapi.PlayerID) error {
					return io.EOF // Always drops
				},
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewPsyNetRankClient failed: %v", err)
	}
	defer client.Close()

	_, err = client.GetPlayersSkills(context.Background(), []rlapi.PlayerID{"Epic|p1|0"})
	if err == nil {
		t.Fatalf("expected error from persistent drop, got nil")
	}

	// Should attempt initial call (factoryCount=1), drop, reconnect (factoryCount=2), and then fail without looping
	calls := atomic.LoadInt32(&factoryCount)
	if calls != 2 {
		t.Errorf("factory called %d times; want exactly 2 (initial + single retry)", calls)
	}
}

// TestResilience_ReconnectFailureHandling verifies behavior when the initial query drops
// and the reconnect attempt itself fails.
func TestResilience_ReconnectFailureHandling(t *testing.T) {
	var factoryCount int32
	reconnectErr := errors.New("auth server unreachable")

	client, err := NewPsyNetRankClient(RankClientConfig{
		CredentialsSupplier: psynet.StaticCredentials{
			Platform:  "Epic",
			AuthToken: "tok",
			AccountID: "acc",
		},
		RPCFactory: func(ctx context.Context, creds *psynet.Credentials) (SkillRPCClient, error) {
			c := atomic.AddInt32(&factoryCount, 1)
			if c == 1 {
				return &controllableSkillRPC{
					connected: true,
					failFunc: func(call int, playerIDs []rlapi.PlayerID) error {
						return io.EOF
					},
				}, nil
			}
			return nil, reconnectErr
		},
	})
	if err != nil {
		t.Fatalf("NewPsyNetRankClient failed: %v", err)
	}
	defer client.Close()

	_, err = client.GetPlayersSkills(context.Background(), []rlapi.PlayerID{"Epic|p1|0"})
	if err == nil {
		t.Fatalf("expected error when reconnect fails, got nil")
	}
	if !errors.Is(err, reconnectErr) && !errors.Is(err, io.EOF) {
		t.Errorf("error %v does not wrap either reconnectErr or io.EOF", err)
	}
}

// ============================================================================
// Suite 2: Context Cancellations & Timeouts
// ============================================================================

func TestResilience_PreCancelledContext(t *testing.T) {
	var factoryCalls int32
	client, err := NewPsyNetRankClient(RankClientConfig{
		CredentialsSupplier: psynet.StaticCredentials{
			Platform:  "Epic",
			AuthToken: "tok",
			AccountID: "acc",
		},
		RPCFactory: func(ctx context.Context, creds *psynet.Credentials) (SkillRPCClient, error) {
			atomic.AddInt32(&factoryCalls, 1)
			return &controllableSkillRPC{connected: true}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewPsyNetRankClient failed: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before call

	_, err = client.GetPlayersSkills(ctx, []rlapi.PlayerID{"Epic|p1|0"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestResilience_TimeoutDuringInitialCallDoesNotRetry(t *testing.T) {
	var factoryCalls int32
	client, err := NewPsyNetRankClient(RankClientConfig{
		CredentialsSupplier: psynet.StaticCredentials{
			Platform:  "Epic",
			AuthToken: "tok",
			AccountID: "acc",
		},
		RPCFactory: func(ctx context.Context, creds *psynet.Credentials) (SkillRPCClient, error) {
			atomic.AddInt32(&factoryCalls, 1)
			return &controllableSkillRPC{
				connected: true,
				delay:     200 * time.Millisecond, // Exceeds deadline
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewPsyNetRankClient failed: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = client.GetPlayersSkills(ctx, []rlapi.PlayerID{"Epic|p1|0"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got: %v", err)
	}

	// Because context was cancelled, it should NOT trigger a reconnect retry
	if atomic.LoadInt32(&factoryCalls) != 1 {
		t.Errorf("expected 1 factory call, got %d", atomic.LoadInt32(&factoryCalls))
	}
}

// ============================================================================
// Suite 3: NoOpRankClient Offline Fallback Verification & Zero Latency
// ============================================================================

func TestResilience_NoOpRankClient_NeverPanicsAndZeroLatency(t *testing.T) {
	client := NewNoOpRankClient()
	if client.IsEnabled() {
		t.Errorf("NoOpRankClient.IsEnabled() must be false")
	}

	// 1. Edge input scenarios
	scenarios := [][]rlapi.PlayerID{
		nil,
		{},
		{"Epic|test|0"},
		make([]rlapi.PlayerID, 5000), // Large batch
	}

	for idx, playerIDs := range scenarios {
		t.Run(fmt.Sprintf("Scenario_%d", idx), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("NoOpRankClient panicked on input len %d: %v", len(playerIDs), r)
				}
			}()

			res, err := client.GetPlayersSkills(context.Background(), playerIDs)
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			if res != nil {
				t.Fatalf("expected nil results, got %+v", res)
			}
		})
	}

	// 2. Cancelled context must still be safe and return nil, nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := client.GetPlayersSkills(ctx, []rlapi.PlayerID{"Epic|test|0"})
	if err != nil || res != nil {
		t.Errorf("expected nil, nil on cancelled context, got %v, %v", res, err)
	}

	// 3. Zero latency benchmark: 50,000 calls must finish within 100 milliseconds
	start := time.Now()
	const iterations = 50000
	for i := 0; i < iterations; i++ {
		_, _ = client.GetPlayersSkills(context.Background(), []rlapi.PlayerID{"Epic|bench|0"})
	}
	elapsed := time.Since(start)
	if elapsed > 100*time.Millisecond {
		t.Errorf("50,000 NoOpRankClient calls took %v; expected near-zero latency (< 100ms)", elapsed)
	}
	t.Logf("50,000 NoOp calls executed in %v (%.2f ns/call)", elapsed, float64(elapsed.Nanoseconds())/iterations)

	// 4. Close is clean
	if err := client.Close(); err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

// TestResilience_NewRankClient_GracefulFallbackGrid tests all configuration states
// where NewRankClient must cleanly degrade to NoOpRankClient without throwing an error.
