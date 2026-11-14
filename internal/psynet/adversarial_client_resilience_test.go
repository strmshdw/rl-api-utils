package psynet

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/dank/rlapi"
)

func TestAdversarial_Client_ContextCancellation_NoHangs(t *testing.T) {
	t.Run("PreCancelledContext", func(t *testing.T) {
		mockRPC := NewMockRPCClient()
		client := NewClientWithRPC(mockRPC, nil)
		defer client.Close()

		ctxCancel, cancel := context.WithCancel(context.Background())
		cancel()

		if _, err := client.GetRecentMatches(ctxCancel); !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got: %v", err)
		}
	})

	t.Run("DeadlineExceeded", func(t *testing.T) {
		mockRPC := NewMockRPCClient()
		mockRPC.mu.Lock()
		mockRPC.onQuery = func(ctx context.Context) ([]rlapi.MatchEntry, error) {
			select {
			case <-time.After(100 * time.Millisecond):
				return nil, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		mockRPC.mu.Unlock()

		client := NewClientWithRPC(mockRPC, nil)
		defer client.Close()

		ctxDeadline, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		defer cancel()

		if _, err := client.GetRecentMatches(ctxDeadline); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected context.DeadlineExceeded, got: %v", err)
		}
	})
}

func TestAdversarial_Client_ConcurrencyAndCloseSafety(t *testing.T) {
	mockRPC := NewMockRPCClient(rlapi.MatchEntry{
		ReplayUrl: "https://example.com/replay.replay",
		Match:     rlapi.Match{MatchGUID: "concur-guid", RecordStartTimestamp: 12345},
	})
	client := NewClientWithRPC(mockRPC, nil)

	const numWorkers = 20
	const opsPerWorker = 10
	var wg sync.WaitGroup

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for op := 0; op < opsPerWorker; op++ {
				_, _ = client.GetRecentMatches(context.Background())
				time.Sleep(1 * time.Millisecond)
			}
		}()
	}

	time.Sleep(5 * time.Millisecond)
	_ = client.Close()
	wg.Wait()

	if _, err := client.GetRecentMatches(context.Background()); !errors.Is(err, ErrClientClosed) {
		t.Fatalf("expected ErrClientClosed post-close, got: %v", err)
	}
	for i := 0; i < 5; i++ {
		if closeErr := client.Close(); closeErr != nil {
			t.Fatalf("idempotent Close failed on attempt %d: %v", i, closeErr)
		}
	}
}

func TestAdversarial_Client_InvalidCredentials(t *testing.T) {
	ctx := context.Background()

	t.Run("MissingCredentialsSupplierAndCreds", func(t *testing.T) {
		client, err := NewClient(ClientConfig{})
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		defer client.Close()

		if _, err = client.GetRecentMatches(ctx); !errors.Is(err, ErrMissingCredentials) {
			t.Fatalf("expected ErrMissingCredentials, got: %v", err)
		}
	})

	t.Run("UnsupportedPlatform", func(t *testing.T) {
		client, err := NewClient(ClientConfig{
			Credentials: &Credentials{Platform: "NintendoSwitch", AuthToken: "tok", AccountID: "acc"},
		})
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		defer client.Close()

		if _, err = client.GetRecentMatches(ctx); !errors.Is(err, ErrInvalidPlatform) {
			t.Fatalf("expected ErrInvalidPlatform, got: %v", err)
		}
	})

	t.Run("SteamMissingSteamAccountID", func(t *testing.T) {
		client, err := NewClient(ClientConfig{
			Credentials: &Credentials{Platform: "Steam", AuthToken: "eos-tok", AccountID: "acc-id", SteamAccountID: ""},
		})
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		defer client.Close()

		if _, err = client.GetRecentMatches(ctx); !errors.Is(err, ErrMissingSteamAccount) {
			t.Fatalf("expected ErrMissingSteamAccount, got: %v", err)
		}
	})
}

func TestClient_RequestTimeout_BoundsHangingRPCAndAuth(t *testing.T) {
	t.Run("DefaultRequestTimeoutIs30s", func(t *testing.T) {
		c, err := NewClient(ClientConfig{})
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		defer c.Close()
		if c.requestTimeout != DefaultRequestTimeout || DefaultRequestTimeout != 30*time.Second {
			t.Fatalf("expected default RequestTimeout 30s, got %v", c.requestTimeout)
		}
	})

	t.Run("ShortRequestTimeoutAbortsHangingGetMatchHistory", func(t *testing.T) {
		mockRPC := NewMockRPCClient()
		mockRPC.mu.Lock()
		mockRPC.onQuery = func(ctx context.Context) ([]rlapi.MatchEntry, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		mockRPC.mu.Unlock()

		c, err := NewClient(ClientConfig{
			RequestTimeout: 40 * time.Millisecond,
			Credentials:    &Credentials{Platform: "Epic", AuthToken: "tok", AccountID: "acc"},
			RPCFactory: func(ctx context.Context, creds *Credentials) (RPCClient, error) {
				return mockRPC, nil
			},
		})
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		defer c.Close()

		start := time.Now()
		_, err = c.GetRecentMatches(context.Background())
		elapsed := time.Since(start)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected DeadlineExceeded, got: %v", err)
		}
		if elapsed > 300*time.Millisecond {
			t.Fatalf("GetRecentMatches blocked too long (%v) past 40ms RequestTimeout", elapsed)
		}
	})

	t.Run("ShortRequestTimeoutAbortsHangingAuthPlayer", func(t *testing.T) {
		hangingAuth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(3 * time.Second):
			}
		}))
		defer hangingAuth.Close()

		origTransport := http.DefaultTransport
		http.DefaultTransport = &redirectPsyNetTransport{
			targetURL: hangingAuth.URL,
			base:      origTransport,
		}
		defer func() { http.DefaultTransport = origTransport }()

		c, err := NewClient(ClientConfig{
			RequestTimeout: 40 * time.Millisecond,
			Credentials:    &Credentials{Platform: "Epic", AuthToken: "tok", AccountID: "acc", DisplayName: "Test"},
		})
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}
		defer c.Close()

		start := time.Now()
		_, err = c.GetRecentMatches(context.Background())
		elapsed := time.Since(start)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected context.DeadlineExceeded from hanging auth handshake, got: %v", err)
		}
		if elapsed > 350*time.Millisecond {
			t.Fatalf("hanging AuthPlayer took too long (%v) past 40ms RequestTimeout", elapsed)
		}
	})

	t.Run("CloseCancelsInFlightConnectWithoutDeadlock", func(t *testing.T) {
		entered := make(chan struct{})
		release := make(chan struct{})
		defer close(release)

		c, err := NewClient(ClientConfig{
			RequestTimeout: 5 * time.Second,
			Credentials:    &Credentials{Platform: "Epic", AuthToken: "tok", AccountID: "acc"},
			RPCFactory: func(ctx context.Context, creds *Credentials) (RPCClient, error) {
				close(entered)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-release:
					return NewMockRPCClient(), nil
				}
			},
		})
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		errCh := make(chan error, 1)
		go func() {
			_, gErr := c.GetRecentMatches(context.Background())
			errCh <- gErr
		}()

		<-entered
		start := time.Now()
		if err := c.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		select {
		case gotErr := <-errCh:
			if !errors.Is(gotErr, ErrClientClosed) {
				t.Fatalf("expected ErrClientClosed when Close aborts connect, got: %v", gotErr)
			}
			if time.Since(start) > 300*time.Millisecond {
				t.Fatalf("Close took too long to abort connect: %v", time.Since(start))
			}
		case <-time.After(1 * time.Second):
			t.Fatal("Close deadlocked waiting on in-flight connect")
		}
	})
}
