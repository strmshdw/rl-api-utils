package ballchasing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// Adversarial Challenge 2: internal/ballchasing Rate Limit & Retry-After
// ============================================================================

// ----------------------------------------------------------------------------
// Challenge 1: 429 Too Many Requests - Consecutive 429 Recovery
// ----------------------------------------------------------------------------

func TestChallenge2_RateLimit_Consecutive429_Recovery(t *testing.T) {
	for _, streamMode := range []bool{false, true} {
		t.Run(fmt.Sprintf("streaming=%v", streamMode), func(t *testing.T) {
			var requestCount int32
			const consecutive429 = 3
			const expectedID = "recovered-uuid-consecutive-429"
			const expectedLoc = "https://ballchasing.com/replay/" + expectedID

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count := atomic.AddInt32(&requestCount, 1)

				if count <= consecutive429 {
					// Return 429 for the first 3 requests with Retry-After: 0 for fast test turnaround
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("Retry-After", "0")
					w.WriteHeader(http.StatusTooManyRequests)
					_ = json.NewEncoder(w).Encode(map[string]string{"error": "rate limit exceeded"})
					return
				}

				// 4th request succeeds with 201 Created
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Location", expectedLoc)
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"id":       expectedID,
					"location": expectedLoc,
				})
			}))
			defer srv.Close()

			client, err := NewClient(ClientConfig{
				BaseURL:      srv.URL,
				APIKey:       defaultTestToken,
				MaxRetries:   4, // Budget allows 4 retries (5 total requests)
				BaseBackoff:  5 * time.Millisecond,
				MaxBackoff:   50 * time.Millisecond,
				StreamUpload: streamMode,
				RateLimiter:  NewRateLimiter(time.Nanosecond),
			})
			if err != nil {
				t.Fatalf("NewClient failed: %v", err)
			}

			guid := fmt.Sprintf("guid-consec-429-%v", streamMode)
			filePath := helperCreateReplayFile(t, guid, 2048)

			res, err := client.UploadReplay(context.Background(), guid, filePath)
			if err != nil {
				t.Fatalf("expected successful recovery after %d 429s, got error: %v", consecutive429, err)
			}
			if res == nil {
				t.Fatal("expected non-nil UploadResult")
			}
			if res.ID != expectedID {
				t.Fatalf("expected ID %q, got %q", expectedID, res.ID)
			}
			if res.Location != expectedLoc {
				t.Fatalf("expected Location %q, got %q", expectedLoc, res.Location)
			}
			if res.IsDuplicate {
				t.Fatalf("expected IsDuplicate == false, got true")
			}

			// Verify exactly 4 total HTTP requests (attempt 0, 1, 2, 3)
			totalReqs := atomic.LoadInt32(&requestCount)
			if totalReqs != consecutive429+1 {
				t.Fatalf("expected exactly %d requests, got %d", consecutive429+1, totalReqs)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Challenge 1B: 429 Retry-After Header Formats & Variations
// ----------------------------------------------------------------------------

func TestChallenge2_RateLimit_RetryAfter_Formats(t *testing.T) {
	// 1. Integer Retry-After: 1 second
	t.Run("integer Retry-After: 1 respects ~1 second sleep", func(t *testing.T) {
		var requestCount int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count := atomic.AddInt32(&requestCount, 1)
			if count == 1 {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "rate limited"})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "int-1-id", "location": "loc"})
		}))
		defer srv.Close()

		client, _ := NewClient(ClientConfig{
			BaseURL:     srv.URL,
			APIKey:      defaultTestToken,
			MaxRetries:  2,
			BaseBackoff: 10 * time.Millisecond,
			RateLimiter: NewRateLimiter(time.Nanosecond),
		})

		guid := "guid-retry-int-1"
		filePath := helperCreateReplayFile(t, guid, 1024)

		start := time.Now()
		res, err := client.UploadReplay(context.Background(), guid, filePath)
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("unexpected upload error: %v", err)
		}
		if res == nil || res.ID != "int-1-id" {
			t.Fatalf("unexpected result: %+v", res)
		}
		// Must sleep approximately 1 second (allow slight clock jitter: >= 900ms)
		if elapsed < 900*time.Millisecond {
			t.Fatalf("expected sleep of ~1s for Retry-After: 1, but elapsed only %v", elapsed)
		}
		if elapsed > 3*time.Second {
			t.Fatalf("Retry-After: 1 took unreasonably long: %v", elapsed)
		}
	})

	// 2. Integer Retry-After: 0 seconds (instant retry)
	t.Run("integer Retry-After: 0 causes immediate retry", func(t *testing.T) {
		var requestCount int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count := atomic.AddInt32(&requestCount, 1)
			if count == 1 {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "rate limited"})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "int-0-id", "location": "loc"})
		}))
		defer srv.Close()

		client, _ := NewClient(ClientConfig{
			BaseURL:     srv.URL,
			APIKey:      defaultTestToken,
			MaxRetries:  2,
			BaseBackoff: 10 * time.Millisecond,
			RateLimiter: NewRateLimiter(time.Nanosecond),
		})

		guid := "guid-retry-int-0"
		filePath := helperCreateReplayFile(t, guid, 1024)

		start := time.Now()
		res, err := client.UploadReplay(context.Background(), guid, filePath)
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("unexpected upload error: %v", err)
		}
		if res == nil || res.ID != "int-0-id" {
			t.Fatalf("unexpected result: %+v", res)
		}
		if elapsed > 250*time.Millisecond {
			t.Fatalf("expected near-instant retry for Retry-After: 0, took %v", elapsed)
		}
	})

	// 3. HTTP-date string (RFC 1123 / IMF-fixdate format) in near future (~1-2s)
	t.Run("HTTP-date RFC1123 near future", func(t *testing.T) {
		var requestCount int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count := atomic.AddInt32(&requestCount, 1)
			if count == 1 {
				// HTTP-date RFC 7231 IMF-fixdate (http.TimeFormat): now + 2 seconds
				// (Since HTTP-date has 1-second resolution, +2s guarantees between 1.0s and 2.0s wait)
				retryTime := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", retryTime)
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "rate limited"})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "date-rfc1123-id", "location": "loc"})
		}))
		defer srv.Close()

		client, _ := NewClient(ClientConfig{
			BaseURL:     srv.URL,
			APIKey:      defaultTestToken,
			MaxRetries:  2,
			MaxBackoff:  10 * time.Second,
			BaseBackoff: 10 * time.Millisecond,
			RateLimiter: NewRateLimiter(time.Nanosecond),
		})

		guid := "guid-retry-rfc1123"
		filePath := helperCreateReplayFile(t, guid, 1024)

		start := time.Now()
		res, err := client.UploadReplay(context.Background(), guid, filePath)
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("unexpected upload error: %v", err)
		}
		if res == nil || res.ID != "date-rfc1123-id" {
			t.Fatalf("unexpected result: %+v", res)
		}
		// Expect ~1-2s wait (>= 900ms to allow timing variance)
		if elapsed < 900*time.Millisecond {
			t.Fatalf("expected sleep based on HTTP-date (>=900ms), but completed in %v", elapsed)
		}
	})

	// 4. HTTP-date string (RFC 850 format) in near future (~1-2s)
	t.Run("HTTP-date RFC850 near future", func(t *testing.T) {
		var requestCount int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count := atomic.AddInt32(&requestCount, 1)
			if count == 1 {
				// RFC 850 with GMT: "Monday, 02-Jan-06 15:04:05 GMT"
				retryTime := time.Now().Add(2 * time.Second).UTC().Format("Monday, 02-Jan-06 15:04:05 GMT")
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", retryTime)
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "rate limited"})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "date-rfc850-id", "location": "loc"})
		}))
		defer srv.Close()

		client, _ := NewClient(ClientConfig{
			BaseURL:     srv.URL,
			APIKey:      defaultTestToken,
			MaxRetries:  2,
			MaxBackoff:  10 * time.Second,
			BaseBackoff: 10 * time.Millisecond,
			RateLimiter: NewRateLimiter(time.Nanosecond),
		})

		guid := "guid-retry-rfc850"
		filePath := helperCreateReplayFile(t, guid, 1024)

		start := time.Now()
		res, err := client.UploadReplay(context.Background(), guid, filePath)
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("unexpected upload error: %v", err)
		}
		if res == nil || res.ID != "date-rfc850-id" {
			t.Fatalf("unexpected result: %+v", res)
		}
		if elapsed < 900*time.Millisecond {
			t.Fatalf("expected sleep based on RFC850 HTTP-date (>=900ms), completed in %v", elapsed)
		}
	})

	// 5. HTTP-date in past (immediate retry)
	t.Run("HTTP-date in past causes immediate retry", func(t *testing.T) {
		var requestCount int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count := atomic.AddInt32(&requestCount, 1)
			if count == 1 {
				// Past date
				retryTime := time.Now().Add(-60 * time.Second).UTC().Format(time.RFC1123)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", retryTime)
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "rate limited"})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "date-past-id", "location": "loc"})
		}))
		defer srv.Close()

		client, _ := NewClient(ClientConfig{
			BaseURL:     srv.URL,
			APIKey:      defaultTestToken,
			MaxRetries:  2,
			BaseBackoff: 10 * time.Millisecond,
			RateLimiter: NewRateLimiter(time.Nanosecond),
		})

		guid := "guid-retry-past"
		filePath := helperCreateReplayFile(t, guid, 1024)

		start := time.Now()
		res, err := client.UploadReplay(context.Background(), guid, filePath)
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("unexpected upload error: %v", err)
		}
		if res == nil || res.ID != "date-past-id" {
			t.Fatalf("unexpected result: %+v", res)
		}
		if elapsed > 250*time.Millisecond {
			t.Fatalf("expected immediate retry for past HTTP-date, took %v", elapsed)
		}
	})

	// 6. Absent Retry-After header falls back to exponential backoff
	t.Run("absent Retry-After header falls back to exponential backoff", func(t *testing.T) {
		var requestCount int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count := atomic.AddInt32(&requestCount, 1)
			if count == 1 {
				// No Retry-After header sent at all
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "rate limited without header"})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "absent-hdr-id", "location": "loc"})
		}))
		defer srv.Close()

		client, _ := NewClient(ClientConfig{
			BaseURL:     srv.URL,
			APIKey:      defaultTestToken,
			MaxRetries:  2,
			BaseBackoff: 40 * time.Millisecond,
			MaxBackoff:  200 * time.Millisecond,
			RateLimiter: NewRateLimiter(time.Nanosecond),
		})

		guid := "guid-retry-absent-hdr"
		filePath := helperCreateReplayFile(t, guid, 1024)

		start := time.Now()
		res, err := client.UploadReplay(context.Background(), guid, filePath)
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("unexpected upload error: %v", err)
		}
		if res == nil || res.ID != "absent-hdr-id" {
			t.Fatalf("unexpected result: %+v", res)
		}
		// Exponential backoff for attempt 0 with baseBackoff=40ms yields jittered sleep in [20ms, 40ms]
		if elapsed < 15*time.Millisecond {
			t.Fatalf("expected exponential backoff sleep (>=15ms), completed too fast: %v", elapsed)
		}
	})

	// 7. Malformed non-numeric Retry-After header
	t.Run("malformed non-numeric Retry-After falls back to exponential backoff", func(t *testing.T) {
		var requestCount int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count := atomic.AddInt32(&requestCount, 1)
			if count == 1 {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "invalid-duration-value-string")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "rate limited"})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "malformed-hdr-id", "location": "loc"})
		}))
		defer srv.Close()

		client, _ := NewClient(ClientConfig{
			BaseURL:     srv.URL,
			APIKey:      defaultTestToken,
			MaxRetries:  2,
			BaseBackoff: 30 * time.Millisecond,
			RateLimiter: NewRateLimiter(time.Nanosecond),
		})

		guid := "guid-retry-malformed"
		filePath := helperCreateReplayFile(t, guid, 1024)

		start := time.Now()
		res, err := client.UploadReplay(context.Background(), guid, filePath)
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("unexpected upload error: %v", err)
		}
		if res == nil || res.ID != "malformed-hdr-id" {
			t.Fatalf("unexpected result: %+v", res)
		}
		if elapsed < 10*time.Millisecond {
			t.Fatalf("expected exponential backoff sleep, completed in %v", elapsed)
		}
	})
}
