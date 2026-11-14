package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rlapi"
)

func TestEpicAuthProvider_ConcurrentAuthenticate_SerializesRefresh(t *testing.T) {
	var clkMu sync.Mutex
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time {
		clkMu.Lock()
		defer clkMu.Unlock()
		return now
	}

	var oauthCalls atomic.Int32
	expectedToken := "rt-initial"

	mock := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			time.Sleep(10 * time.Millisecond) // Widen concurrency window for concurrent callers.
			n := oauthCalls.Add(1)
			if rt != expectedToken {
				return nil, fmt.Errorf("parallel or stale refresh token %q (want %q)", rt, expectedToken)
			}
			nextRT := fmt.Sprintf("rt-v%d", n+1)
			expectedToken = nextRT
			return &rlapi.TokenResponse{
				AccessToken:  fmt.Sprintf("egs-v%d", n),
				RefreshToken: nextRT,
				AccountID:    "epic-acc-serial",
				DisplayName:  "SerialUser",
				ExpiresIn:    3600,
			}, nil
		},
		getExchangeCodeFunc: func(at string) (string, error) { return "ex-" + at, nil },
		exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
			return &rlapi.EOSTokenResponse{
				AccessToken: "eos-" + ec,
				AccountID:   "epic-acc-serial",
				ExpiresIn:   3600,
			}, nil
		},
	}

	store := newMockStateStore()
	var saveCount atomic.Int32
	p, err := NewEpicProvider(
		config.EpicConfig{RefreshToken: "rt-initial"},
		store,
		WithEGSClient(mock),
		WithClock(clock),
		WithConfigTokenSaver(func(u config.TokenUpdates) error {
			saveCount.Add(1)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("NewEpicProvider failed: %v", err)
	}

	runBurst := func(wantEOS string) {
		const workers = 20
		start := make(chan struct{})
		var wg sync.WaitGroup
		errs := make(chan error, workers)
		tokens := make(chan string, workers)

		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				info, authErr := p.Authenticate(context.Background())
				if authErr != nil {
					errs <- authErr
					return
				}
				tokens <- info.AccessToken
			}()
		}
		close(start)
		wg.Wait()
		close(errs)
		close(tokens)

		for e := range errs {
			t.Fatalf("concurrent Authenticate failed: %v", e)
		}
		for tok := range tokens {
			if tok != wantEOS {
				t.Fatalf("expected %q, got %q", wantEOS, tok)
			}
		}
	}

	// Wave 1: 20 concurrent initial Authenticate calls -> exactly 1 OAuth call.
	runBurst("eos-ex-egs-v1")
	if got := oauthCalls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 OAuth call in wave 1, got %d", got)
	}
	if got := saveCount.Load(); got != 1 {
		t.Fatalf("expected exactly 1 TokenSaver call in wave 1, got %d", got)
	}

	// Advance clock into 5-minute expiry buffer so Wave 2 triggers re-authentication.
	clkMu.Lock()
	now = now.Add(56 * time.Minute)
	clkMu.Unlock()

	runBurst("eos-ex-egs-v2")
	if got := oauthCalls.Load(); got != 2 {
		t.Fatalf("expected exactly 2 total OAuth calls after expiry wave, got %d", got)
	}
	if got := saveCount.Load(); got != 2 {
		t.Fatalf("expected exactly 2 TokenSaver calls after expiry wave, got %d", got)
	}
}

func TestSteamAuthProvider_ConcurrentAuthenticate_SerializesRefresh(t *testing.T) {
	var clkMu sync.Mutex
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time {
		clkMu.Lock()
		defer clkMu.Unlock()
		return now
	}

	var genCalls atomic.Int32
	var exchangeCalls atomic.Int32

	gen := &mockSteamGenerator{
		generateFunc: func(ctx context.Context, opts SteamTicketOptions) (*SteamTicketResult, error) {
			time.Sleep(10 * time.Millisecond)
			n := genCalls.Add(1)
			return &SteamTicketResult{
				Success:       true,
				SessionTicket: fmt.Sprintf("01000000TICKET%d", n),
				SteamID64:     "76561198000000099",
				LoginKey:      fmt.Sprintf("rotated-key-%d", n),
				AccountName:   "SerialSteam",
			}, nil
		},
	}

	mock := &mockEGS{
		exchangeEOSTokenFromSteamFunc: func(ticket string) (*rlapi.EOSTokenResponse, error) {
			n := exchangeCalls.Add(1)
			return &rlapi.EOSTokenResponse{
				AccessToken:  fmt.Sprintf("steam-eos-v%d", n),
				RefreshToken: fmt.Sprintf("steam-rt-v%d", n),
				AccountID:    "linked-epic-99",
				ExpiresIn:    3600,
			}, nil
		},
	}

	p, err := NewSteamProvider(
		config.SteamConfig{Username: "serial_steam", Password: "secret"},
		newMockStateStore(),
		WithEGSClient(mock),
		WithSteamGenerator(gen),
		WithClock(clock),
	)
	if err != nil {
		t.Fatalf("NewSteamProvider failed: %v", err)
	}

	const workers = 20
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			if idx%4 == 0 {
				_ = p.Validate()
			}
			info, authErr := p.Authenticate(context.Background())
			if authErr != nil {
				errs <- authErr
				return
			}
			if info.EpicAccountID != "linked-epic-99" || info.AccessToken != "steam-eos-v1" {
				errs <- fmt.Errorf("unexpected info: %+v", info)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)

	for e := range errs {
		t.Fatalf("concurrent Steam Authenticate failed: %v", e)
	}
	if genCalls.Load() != 1 || exchangeCalls.Load() != 1 {
		t.Fatalf("expected 1 ticket gen and 1 exchange, got gen=%d exchange=%d", genCalls.Load(), exchangeCalls.Load())
	}
	if p.cfg.LoginKey != "" || p.cfg.SessionTicket != "" {
		t.Fatalf("provider must never mutate p.cfg after construction")
	}
}

func TestAdversarial_Concurrency_Epic_TokenInfoAndAuthenticate(t *testing.T) {
	var count int
	var mu sync.Mutex

	mock := &mockEGS{
		authenticateWithRefreshTokenFunc: func(rt string) (*rlapi.TokenResponse, error) {
			mu.Lock()
			count++
			c := count
			mu.Unlock()
			return &rlapi.TokenResponse{
				AccessToken: fmt.Sprintf("egs-token-%d", c), RefreshToken: fmt.Sprintf("refresh-token-%d", c),
				AccountID: "epic-acc-concurrent", ExpiresIn: 3600,
			}, nil
		},
		getExchangeCodeFunc: func(at string) (string, error) { return "ex-" + at, nil },
		exchangeEOSTokenFunc: func(ec string) (*rlapi.EOSTokenResponse, error) {
			return &rlapi.EOSTokenResponse{AccessToken: "eos-" + ec, ExpiresIn: 3600}, nil
		},
	}

	p, _ := NewEpicProvider(config.EpicConfig{RefreshToken: "init-token"}, newMockStateStore(), WithEGSClient(mock))
	if _, err := p.Authenticate(context.Background()); err != nil {
		t.Fatalf("initial authenticate failed: %v", err)
	}

	const readers, writers, iterations = 30, 8, 40
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if info := p.TokenInfo(); info != nil {
					_ = info.Valid()
					_ = info.IsExpired()
					if !strings.HasPrefix(info.AccessToken, "eos-ex-egs-token-") {
						t.Errorf("corrupted AccessToken read: %q", info.AccessToken)
						return
					}
				}
				time.Sleep(10 * time.Microsecond)
			}
		}()
	}

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				if idx%2 == 0 {
					_, _ = p.Authenticate(ctx)
				} else {
					_, _ = p.Refresh(ctx, "")
				}
				time.Sleep(50 * time.Microsecond)
			}
		}(i)
	}

	wg.Wait()
}

func TestAdversarial_Concurrency_Steam_TokenInfoAndAuthenticate(t *testing.T) {
	var count int
	var mu sync.Mutex

	mock := &mockEGS{
		exchangeEOSTokenFromSteamFunc: func(ticket string) (*rlapi.EOSTokenResponse, error) {
			mu.Lock()
			count++
			c := count
			mu.Unlock()
			return &rlapi.EOSTokenResponse{
				AccessToken: fmt.Sprintf("steam-eos-%d", c), RefreshToken: fmt.Sprintf("steam-refresh-%d", c),
				AccountID: "steam-epic-acc", ExpiresIn: 3600,
			}, nil
		},
		refreshEOSTokenFunc: func(rt string) (*rlapi.EOSTokenResponse, error) {
			mu.Lock()
			count++
			c := count
			mu.Unlock()
			return &rlapi.EOSTokenResponse{
				AccessToken: fmt.Sprintf("steam-eos-refreshed-%d", c), RefreshToken: fmt.Sprintf("steam-refresh-%d", c),
				ExpiresIn: 3600,
			}, nil
		},
	}

	p, _ := NewSteamProvider(
		config.SteamConfig{SessionTicket: "valid-ticket", SteamID64: "76561198000000000"},
		nil, WithEGSClient(mock),
	)
	if _, err := p.Authenticate(context.Background()); err != nil {
		t.Fatalf("initial authenticate failed: %v", err)
	}

	const readers, writers, iterations = 30, 8, 40
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if info := p.TokenInfo(); info != nil {
					_ = info.Valid()
					_ = info.IsExpired()
					if !strings.HasPrefix(info.AccessToken, "steam-eos-") || info.EpicAccountID != "steam-epic-acc" {
						t.Errorf("corrupted steam TokenInfo read: %+v", info)
						return
					}
				}
				time.Sleep(10 * time.Microsecond)
			}
		}()
	}

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				if idx%2 == 0 {
					_, _ = p.Authenticate(ctx)
				} else {
					_, _ = p.Refresh(ctx, "")
				}
				time.Sleep(50 * time.Microsecond)
			}
		}(i)
	}

	wg.Wait()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatal("stress test timed out")
	}
}
