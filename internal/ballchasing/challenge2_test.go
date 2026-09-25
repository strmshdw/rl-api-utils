package ballchasing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/testutil"
)

// ============================================================================
// Adversarial Challenge 2: internal/ballchasing Backoff & File Safety
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

// ----------------------------------------------------------------------------
// Challenge 1C: 429 Retry Budget Exhaustion
// ----------------------------------------------------------------------------

func TestChallenge2_RateLimit_BudgetExhaustion(t *testing.T) {
	testCases := []struct {
		name         string
		maxRetries   int
		streamUpload bool
		expectedReqs int32
	}{
		{
			name:         "maxRetries=2 (total 3 requests)",
			maxRetries:   2,
			streamUpload: false,
			expectedReqs: 3,
		},
		{
			name:         "maxRetries=0 (strictly 1 request, 0 retries)",
			maxRetries:   0,
			streamUpload: false,
			expectedReqs: 1,
		},
		{
			name:         "maxRetries=4 with streaming (total 5 requests)",
			maxRetries:   4,
			streamUpload: true,
			expectedReqs: 5,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var requestCount int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&requestCount, 1)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "0") // 0s sleep for fast test execution
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "rate limit permanently exceeded"})
			}))
			defer srv.Close()

			client, err := NewClient(ClientConfig{
				BaseURL:      srv.URL,
				APIKey:       defaultTestToken,
				MaxRetries:   tc.maxRetries,
				BaseBackoff:  1 * time.Millisecond,
				MaxBackoff:   5 * time.Millisecond,
				StreamUpload: tc.streamUpload,
			})
			if err != nil {
				t.Fatalf("NewClient failed: %v", err)
			}

			guid := fmt.Sprintf("guid-exhaust-%d-%v", tc.maxRetries, tc.streamUpload)
			filePath := helperCreateReplayFile(t, guid, 1024)

			res, err := client.UploadReplay(context.Background(), guid, filePath)

			// 1. Assert result is nil
			if res != nil {
				t.Fatalf("expected nil result on exhaustion, got %+v", res)
			}

			// 2. Assert error is or wraps ErrRateLimitExhausted
			if err == nil {
				t.Fatal("expected error on rate limit exhaustion, got nil")
			}
			if !errors.Is(err, ErrRateLimitExhausted) && !strings.Contains(err.Error(), "rate limit") {
				t.Fatalf("expected ErrRateLimitExhausted, got: %v", err)
			}

			// 3. Assert exact number of HTTP requests made equals maxRetries + 1
			totalReqs := atomic.LoadInt32(&requestCount)
			if totalReqs != tc.expectedReqs {
				t.Fatalf("expected exactly %d requests for maxRetries=%d, got %d", tc.expectedReqs, tc.maxRetries, totalReqs)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Challenge 2: Context Cancellation During Backoff Sleep & Windows File Safety
// ----------------------------------------------------------------------------

func TestChallenge2_ContextCancellation_DuringBackoffSleep(t *testing.T) {
	for _, streamMode := range []bool{false, true} {
		t.Run(fmt.Sprintf("streaming=%v", streamMode), func(t *testing.T) {
			var requestCount int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&requestCount, 1)
				// Return 429 with 60s Retry-After. If client fails to cancel immediately, test will fail!
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "rate limited for 60s"})
			}))
			defer srv.Close()

			client, err := NewClient(ClientConfig{
				BaseURL:      srv.URL,
				APIKey:       defaultTestToken,
				MaxRetries:   5,
				MaxBackoff:   60 * time.Second,
				StreamUpload: streamMode,
			})
			if err != nil {
				t.Fatalf("NewClient failed: %v", err)
			}

			guid := fmt.Sprintf("guid-cancel-sleep-%v", streamMode)
			filePath := helperCreateReplayFile(t, guid, 2048)

			// Cancel context after 60ms
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
			defer cancel()

			start := time.Now()
			res, err := client.UploadReplay(ctx, guid, filePath)
			elapsed := time.Since(start)

			// 1. Must abort promptly without waiting for the 60s timer
			if elapsed > 1*time.Second {
				t.Fatalf("UploadReplay did not abort immediately on context cancellation: took %v (expected < 1s)", elapsed)
			}

			// 2. Must return error wrapping context.DeadlineExceeded or context.Canceled
			if err == nil {
				t.Fatal("expected error on cancelled context, got nil")
			}
			if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
				t.Fatalf("expected context cancellation error, got: %v", err)
			}
			if res != nil {
				t.Fatalf("expected nil result on cancellation, got %+v", res)
			}

			// 3. Exactly 1 request was attempted before sleep was interrupted
			if atomic.LoadInt32(&requestCount) != 1 {
				t.Fatalf("expected exactly 1 request before cancel, got %d", atomic.LoadInt32(&requestCount))
			}

			// 4. Windows File Handle Safety: Verify replay file is NOT locked by an open file descriptor.
			// On Windows, if a file descriptor is leaked, os.Remove or os.Rename will fail with sharing violation!
			removeErr := os.Remove(filePath)
			if removeErr != nil {
				t.Fatalf("file handle leaked during cancelled backoff sleep! os.Remove failed with: %v", removeErr)
			}
		})
	}

	t.Run("pre-cancelled context makes zero requests", func(t *testing.T) {
		var requestCount int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&requestCount, 1)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		client, _ := NewClient(ClientConfig{
			BaseURL: srv.URL,
			APIKey:  defaultTestToken,
		})

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel before call

		guid := "guid-pre-cancel"
		filePath := helperCreateReplayFile(t, guid, 1024)

		res, err := client.UploadReplay(ctx, guid, filePath)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got: %v", err)
		}
		if res != nil {
			t.Fatalf("expected nil result, got %+v", res)
		}
		if atomic.LoadInt32(&requestCount) != 0 {
			t.Fatalf("expected 0 requests for pre-cancelled context, got %d", atomic.LoadInt32(&requestCount))
		}
	})
}

// ----------------------------------------------------------------------------
// Challenge 3: Non-Existent and 0-Byte File Handling
// ----------------------------------------------------------------------------

func TestChallenge2_FileHandling_NonExistent_And_ZeroByte(t *testing.T) {
	for _, streamMode := range []bool{false, true} {
		t.Run(fmt.Sprintf("streaming=%v", streamMode), func(t *testing.T) {
			var requestCount int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&requestCount, 1)
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			client, err := NewClient(ClientConfig{
				BaseURL:      srv.URL,
				APIKey:       defaultTestToken,
				MaxRetries:   3,
				StreamUpload: streamMode,
			})
			if err != nil {
				t.Fatalf("NewClient failed: %v", err)
			}

			// Case 1: Non-existent file path
			t.Run("non-existent file", func(t *testing.T) {
				nonExistent := filepath.Join(t.TempDir(), "non_existent_file.replay")
				res, err := client.UploadReplay(context.Background(), "guid-ne", nonExistent)
				if res != nil {
					t.Fatalf("expected nil result, got %+v", res)
				}
				if err == nil {
					t.Fatal("expected error for non-existent file, got nil")
				}
				if !errors.Is(err, ErrFileNotFound) {
					t.Fatalf("expected ErrFileNotFound, got: %v", err)
				}
				if atomic.LoadInt32(&requestCount) != 0 {
					t.Fatalf("expected 0 HTTP requests for non-existent file, got %d", atomic.LoadInt32(&requestCount))
				}
			})

			// Case 2: 0-byte empty file
			t.Run("0-byte empty file", func(t *testing.T) {
				emptyFile := filepath.Join(t.TempDir(), "empty_0byte.replay")
				if err := os.WriteFile(emptyFile, []byte{}, 0644); err != nil {
					t.Fatalf("failed to create 0-byte file: %v", err)
				}

				res, err := client.UploadReplay(context.Background(), "guid-0byte", emptyFile)
				if res != nil {
					t.Fatalf("expected nil result, got %+v", res)
				}
				if err == nil {
					t.Fatal("expected error for 0-byte file, got nil")
				}
				if !errors.Is(err, ErrEmptyFile) {
					t.Fatalf("expected ErrEmptyFile, got: %v", err)
				}
				if !errors.Is(err, ErrBadRequest) {
					t.Fatalf("expected ErrBadRequest wrapper for 0-byte file, got: %v", err)
				}
				if atomic.LoadInt32(&requestCount) != 0 {
					t.Fatalf("expected 0 HTTP requests for 0-byte file, got %d", atomic.LoadInt32(&requestCount))
				}
			})

			// Case 3: Directory path provided as filePath
			t.Run("directory path provided as filePath", func(t *testing.T) {
				dirPath := t.TempDir()
				res, err := client.UploadReplay(context.Background(), "guid-dir", dirPath)
				if res != nil {
					t.Fatalf("expected nil result, got %+v", res)
				}
				if err == nil {
					t.Fatal("expected error for directory path, got nil")
				}
				if !errors.Is(err, ErrBadRequest) {
					t.Fatalf("expected ErrBadRequest for directory path, got: %v", err)
				}
				if atomic.LoadInt32(&requestCount) != 0 {
					t.Fatalf("expected 0 HTTP requests for directory path, got %d", atomic.LoadInt32(&requestCount))
				}
			})

			// Case 4: Empty file path string
			t.Run("empty string file path", func(t *testing.T) {
				res, err := client.UploadReplay(context.Background(), "guid-empty-str", "")
				if res != nil {
					t.Fatalf("expected nil result, got %+v", res)
				}
				if err == nil {
					t.Fatal("expected error for empty file path, got nil")
				}
				if !errors.Is(err, ErrEmptyFilePath) {
					t.Fatalf("expected ErrEmptyFilePath, got: %v", err)
				}
				if atomic.LoadInt32(&requestCount) != 0 {
					t.Fatalf("expected 0 HTTP requests for empty file path, got %d", atomic.LoadInt32(&requestCount))
				}
			})

			// Case 5: Whitespace-only file path string
			t.Run("whitespace-only file path", func(t *testing.T) {
				res, err := client.UploadReplay(context.Background(), "guid-ws-path", "   \t\n  ")
				if res != nil {
					t.Fatalf("expected nil result, got %+v", res)
				}
				if err == nil {
					t.Fatal("expected error for whitespace file path, got nil")
				}
				if !errors.Is(err, ErrEmptyFilePath) {
					t.Fatalf("expected ErrEmptyFilePath, got: %v", err)
				}
				if atomic.LoadInt32(&requestCount) != 0 {
					t.Fatalf("expected 0 HTTP requests for whitespace file path, got %d", atomic.LoadInt32(&requestCount))
				}
			})
		})
	}
}

// ----------------------------------------------------------------------------
// Challenge 4: Concurrency Stress Harness (Same Client Instance)
// ----------------------------------------------------------------------------

func TestChallenge2_Concurrency_StressHarness(t *testing.T) {
	for _, streamMode := range []bool{false, true} {
		t.Run(fmt.Sprintf("streaming=%v", streamMode), func(t *testing.T) {
			srv := testutil.NewMockBallchasingServer()
			defer srv.Close()

			// Configure client with shared instance
			client, err := NewClient(ClientConfig{
				BaseURL:      srv.URL(),
				APIKey:       defaultTestToken,
				Visibility:   "public",
				MaxRetries:   3,
				BaseBackoff:  2 * time.Millisecond,
				MaxBackoff:   20 * time.Millisecond,
				StreamUpload: streamMode,
			})
			if err != nil {
				t.Fatalf("NewClient failed: %v", err)
			}

			// Pre-configure duplicates in mock server
			const dupCount = 10
			for i := 0; i < dupCount; i++ {
				dupGUID := fmt.Sprintf("concur-dup-%d", i)
				existingID := fmt.Sprintf("existing-concur-id-%d", i)
				srv.SetDuplicateGUID(dupGUID, existingID)
			}

			// 50 total goroutines:
			// - 20 fresh uploads -> 201 Created
			// - 10 duplicate uploads -> 409 Conflict (IsDuplicate: true, err == nil)
			// - 10 transient 429 uploads -> retry and succeed
			// - 5 0-byte file uploads -> ErrEmptyFile
			// - 5 non-existent file uploads -> ErrFileNotFound
			const totalGoroutines = 50
			type resultRecord struct {
				index    int
				category string
				guid     string
				res      *UploadResult
				err      error
			}

			results := make([]resultRecord, totalGoroutines)
			var wg sync.WaitGroup
			startGate := make(chan struct{}) // Barrier to release all goroutines simultaneously

			for i := 0; i < totalGoroutines; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					<-startGate // Wait for simultaneous release

					var cat, guid, filePath string

					switch {
					case idx < 20:
						// Normal fresh uploads (201 Created)
						cat = "fresh"
						guid = fmt.Sprintf("concur-fresh-%v-%d", streamMode, idx)
						filePath = helperCreateReplayFile(t, guid, 1024+(idx*128))

					case idx < 30:
						// Duplicate uploads (409 Conflict)
						dupIdx := idx - 20
						cat = "duplicate"
						guid = fmt.Sprintf("concur-dup-%d", dupIdx)
						filePath = helperCreateReplayFile(t, guid, 1024)

					case idx < 40:
						// Rate-limited uploads
						cat = "ratelimit"
						guid = fmt.Sprintf("concur-ratelimit-%v-%d", streamMode, idx)
						filePath = helperCreateReplayFile(t, guid, 1024)

					case idx < 45:
						// 0-byte file
						cat = "0byte"
						guid = fmt.Sprintf("concur-0byte-%d", idx)
						filePath = filepath.Join(t.TempDir(), fmt.Sprintf("%s.replay", guid))
						_ = os.WriteFile(filePath, []byte{}, 0644)

					default:
						// Non-existent file
						cat = "nonexistent"
						guid = fmt.Sprintf("concur-missing-%d", idx)
						filePath = filepath.Join(t.TempDir(), fmt.Sprintf("%s.replay", guid))
					}

					res, err := client.UploadReplay(context.Background(), guid, filePath)
					results[idx] = resultRecord{
						index:    idx,
						category: cat,
						guid:     guid,
						res:      res,
						err:      err,
					}
				}(i)
			}

			// Release all goroutines simultaneously
			close(startGate)
			wg.Wait()

			// Verify results for all 50 concurrent goroutines
			for _, r := range results {
				switch r.category {
				case "fresh":
					if r.err != nil {
						t.Errorf("[fresh %d] unexpected error: %v", r.index, r.err)
					}
					if r.res == nil || r.res.ID == "" {
						t.Errorf("[fresh %d] expected valid UploadResult, got: %+v", r.index, r.res)
					} else if r.res.IsDuplicate {
						t.Errorf("[fresh %d] expected IsDuplicate == false", r.index)
					}

				case "duplicate":
					if r.err != nil {
						t.Errorf("[duplicate %d] expected err == nil on 409, got: %v", r.index, r.err)
					}
					if r.res == nil {
						t.Errorf("[duplicate %d] expected non-nil result", r.index)
					} else {
						if !r.res.IsDuplicate {
							t.Errorf("[duplicate %d] expected IsDuplicate == true", r.index)
						}
						dupIdx := r.index - 20
						expectedID := fmt.Sprintf("existing-concur-id-%d", dupIdx)
						if r.res.ID != expectedID {
							t.Errorf("[duplicate %d] expected ID %s, got %s", r.index, expectedID, r.res.ID)
						}
					}

				case "ratelimit":
					if r.err != nil {
						t.Errorf("[ratelimit %d] unexpected error: %v", r.index, r.err)
					}
					if r.res == nil || r.res.ID == "" {
						t.Errorf("[ratelimit %d] expected valid UploadResult, got %+v", r.index, r.res)
					}

				case "0byte":
					if r.err == nil {
						t.Errorf("[0byte %d] expected error for 0-byte file, got nil", r.index)
					}
					if !errors.Is(r.err, ErrEmptyFile) {
						t.Errorf("[0byte %d] expected ErrEmptyFile, got: %v", r.index, r.err)
					}
					if r.res != nil {
						t.Errorf("[0byte %d] expected nil result, got %+v", r.index, r.res)
					}

				case "nonexistent":
					if r.err == nil {
						t.Errorf("[nonexistent %d] expected error for missing file, got nil", r.index)
					}
					if !errors.Is(r.err, ErrFileNotFound) {
						t.Errorf("[nonexistent %d] expected ErrFileNotFound, got: %v", r.index, r.err)
					}
					if r.res != nil {
						t.Errorf("[nonexistent %d] expected nil result, got %+v", r.index, r.res)
					}
				}
			}

			// Verify payload integrity for uploaded replays:
			// Each fresh upload recorded on mock server must match its matchGUID and TAGAME magic bytes
			for i := 0; i < 20; i++ {
				guid := fmt.Sprintf("concur-fresh-%v-%d", streamMode, i)
				rec := srv.GetUpload(guid)
				if rec == nil {
					t.Errorf("mock server missing upload record for %s", guid)
					continue
				}
				expectedSize := int64(1024 + (i * 128))
				if rec.FileSize != expectedSize {
					t.Errorf("[%s] expected size %d, got %d", guid, expectedSize, rec.FileSize)
				}
				if !bytes.HasPrefix(rec.FileBytes, testutil.ReplayMagicBytes) {
					t.Errorf("[%s] payload corrupted, missing TAGAME prefix", guid)
				}
			}
		})
	}
}
