package ballchasing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

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
				RateLimiter:  NewRateLimiter(time.Nanosecond),
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
