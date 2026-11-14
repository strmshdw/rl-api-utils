package ballchasing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestRateLimiter_Default500msAndIntervalPacing(t *testing.T) {
	rlDefault := NewRateLimiter(0)
	if rlDefault.Interval() != DefaultRateInterval || DefaultRateInterval != 500*time.Millisecond {
		t.Fatalf("expected default interval 500ms, got %v", rlDefault.Interval())
	}
	if rlNeg := NewRateLimiter(-10 * time.Millisecond); rlNeg.Interval() != DefaultRateInterval {
		t.Fatalf("expected negative interval to default to 500ms, got %v", rlNeg.Interval())
	}

	const interval = 50 * time.Millisecond
	rl := NewRateLimiter(interval)
	ctx := context.Background()

	// First call returns immediately; 2nd and 3rd pace by at least 2*interval.
	start := time.Now()
	for i := 0; i < 3; i++ {
		if err := rl.Wait(ctx); err != nil {
			t.Fatalf("Wait #%d failed: %v", i, err)
		}
	}
	elapsed := time.Since(start)
	if elapsed < 80*time.Millisecond {
		t.Fatalf("expected 3 paced Wait calls to span >= 80ms (~2*50ms), took %v", elapsed)
	}
}

func TestRateLimiter_ContextCancellationDoesNotBlockOthers(t *testing.T) {
	rl := NewRateLimiter(500 * time.Millisecond)

	// Consume the immediate slot
	if err := rl.Wait(context.Background()); err != nil {
		t.Fatalf("initial Wait failed: %v", err)
	}

	// Second caller waits with a short 25ms context timeout — must wake cleanly without holding any lock
	ctxShort, cancelShort := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancelShort()

	start := time.Now()
	err := rl.Wait(ctxShort)
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got: %v", err)
	}
	if elapsed > 180*time.Millisecond {
		t.Fatalf("cancelled Wait slept too long: %v", elapsed)
	}

	// Pre-cancelled context must fail immediately
	ctxPre, cancelPre := context.WithCancel(context.Background())
	cancelPre()
	if err := rl.Wait(ctxPre); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled for pre-cancelled ctx, got: %v", err)
	}
}

func TestRateLimiter_SharedAcrossMultipleClients(t *testing.T) {
	var timestampsMu sync.Mutex
	var timestamps []time.Time

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timestampsMu.Lock()
		timestamps = append(timestamps, time.Now())
		timestampsMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PingResponse{Chaser: true, Name: "sync"})
	}))
	defer srv.Close()

	shared := NewRateLimiter(60 * time.Millisecond)
	c1, err := NewClient(ClientConfig{
		BaseURL:     srv.URL,
		APIKey:      "key-1",
		RateLimiter: shared,
	})
	if err != nil {
		t.Fatalf("NewClient c1 failed: %v", err)
	}

	c2, err := NewClient(ClientConfig{
		BaseURL: srv.URL,
		APIKey:  "key-2",
	}, WithRateLimiter(c1.RateLimiter()))
	if err != nil {
		t.Fatalf("NewClient c2 failed: %v", err)
	}
	if c1.RateLimiter() != c2.RateLimiter() {
		t.Fatal("expected c1 and c2 to share identical *RateLimiter instance")
	}

	var wg sync.WaitGroup
	wg.Add(4)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_ = c1.Ping(context.Background())
		}()
		go func() {
			defer wg.Done()
			_, _ = c2.GetUploadedReplays(context.Background(), "")
		}()
	}
	wg.Wait()

	timestampsMu.Lock()
	defer timestampsMu.Unlock()
	if len(timestamps) != 4 {
		t.Fatalf("expected 4 requests total, got %d", len(timestamps))
	}
	earliest := timestamps[0]
	latest := timestamps[0]
	for _, ts := range timestamps {
		if ts.Before(earliest) {
			earliest = ts
		}
		if ts.After(latest) {
			latest = ts
		}
	}
	if span := latest.Sub(earliest); span < 140*time.Millisecond {
		t.Fatalf("expected shared limiter to space 4 concurrent calls across c1 & c2 by >= 140ms (~3*60ms), got %v", span)
	}
}
