package ballchasing

import (
	"context"
	"sync"
	"time"
)

// DefaultRateInterval enforces a maximum sustained throughput of 2 requests/sec (1 req / 500ms).
const DefaultRateInterval = 500 * time.Millisecond

// RateLimiter serializes request pacing across one or more Ballchasing clients
// without holding locks while waiting and while honoring context cancellation.
type RateLimiter struct {
	mu          sync.Mutex
	interval    time.Duration
	nextAllowed time.Time
}

// NewRateLimiter creates a RateLimiter enforcing minimum spacing interval between requests.
// If interval <= 0, DefaultRateInterval (500ms) is used.
func NewRateLimiter(interval time.Duration) *RateLimiter {
	if interval <= 0 {
		interval = DefaultRateInterval
	}
	return &RateLimiter{interval: interval}
}

// Interval returns the configured minimum inter-request interval.
func (r *RateLimiter) Interval() time.Duration {
	if r == nil {
		return DefaultRateInterval
	}
	return r.interval
}

// Wait blocks until the caller's request slot arrives or ctx is cancelled.
// It reserves a pacing slot under a brief mutex lock and releases the lock before sleeping.
func (r *RateLimiter) Wait(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || r.interval <= 0 {
		return nil
	}

	r.mu.Lock()
	now := time.Now()
	if !r.nextAllowed.After(now) {
		r.nextAllowed = now.Add(r.interval)
		r.mu.Unlock()
		return nil
	}
	waitUntil := r.nextAllowed
	slotEnd := waitUntil.Add(r.interval)
	r.nextAllowed = slotEnd
	r.mu.Unlock()

	delay := time.Until(waitUntil)
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		r.mu.Lock()
		if r.nextAllowed.Equal(slotEnd) {
			r.nextAllowed = waitUntil
		}
		r.mu.Unlock()
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
