package ballchasing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// Adversarial Challenge Test Suite: internal/ballchasing response handling
// ============================================================================

// Challenge 1: 409 Conflict must return IsDuplicate: true and err == nil with strictly 0 retries.
func TestChallenge_409Conflict_ZeroRetries(t *testing.T) {
	for _, streamMode := range []bool{false, true} {
		t.Run(fmt.Sprintf("streaming=%v", streamMode), func(t *testing.T) {
			var requestCount int32
			const expectedDupID = "existing-guid-409-challenge"
			const expectedLocation = "https://ballchasing.com/replay/" + expectedDupID

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&requestCount, 1)

				if !strings.HasSuffix(r.URL.Path, "/v2/upload") {
					http.NotFound(w, r)
					return
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":    "duplicate replay",
					"id":       expectedDupID,
					"location": expectedLocation,
				})
			}))
			defer srv.Close()

			client, err := NewClient(ClientConfig{
				BaseURL:      srv.URL,
				APIKey:       "valid-test-key",
				MaxRetries:   5, // Set high retry limit to guarantee failure if client attempts to retry
				BaseBackoff:  5 * time.Millisecond,
				StreamUpload: streamMode,
			})
			if err != nil {
				t.Fatalf("NewClient failed: %v", err)
			}

			guid := "guid-409-challenge"
			filePath := helperCreateReplayFile(t, guid, 2048)

			res, err := client.UploadReplay(context.Background(), guid, filePath)

			// 1. Assert err == nil
			if err != nil {
				t.Fatalf("expected err == nil on HTTP 409 duplicate, got: %v", err)
			}

			// 2. Assert res is non-nil and IsDuplicate == true
			if res == nil {
				t.Fatal("expected non-nil UploadResult on HTTP 409 duplicate")
			}
			if !res.IsDuplicate {
				t.Fatalf("expected IsDuplicate == true, got %v", res.IsDuplicate)
			}
			if res.ID != expectedDupID {
				t.Fatalf("expected ID %q, got %q", expectedDupID, res.ID)
			}
			if res.Location != expectedLocation {
				t.Fatalf("expected Location %q, got %q", expectedLocation, res.Location)
			}

			// 3. Assert strictly 0 retries (exactly 1 HTTP request made)
			reqs := atomic.LoadInt32(&requestCount)
			if reqs != 1 {
				t.Fatalf("HTTP 409 must have strictly 0 retries (1 total request), but received %d requests", reqs)
			}
		})
	}
}

// Challenge 2: 401 Unauthorized must immediately fatally fail returning ErrInvalidAPIKey with strictly 0 retries.
func TestChallenge_401Unauthorized_ZeroRetries(t *testing.T) {
	for _, streamMode := range []bool{false, true} {
		t.Run(fmt.Sprintf("streaming=%v", streamMode), func(t *testing.T) {
			var requestCount int32

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&requestCount, 1)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error": "Invalid API key.",
				})
			}))
			defer srv.Close()

			client, err := NewClient(ClientConfig{
				BaseURL:      srv.URL,
				APIKey:       "any-key",
				MaxRetries:   5, // Set high retry limit: must NOT retry on 401
				BaseBackoff:  5 * time.Millisecond,
				StreamUpload: streamMode,
			})
			if err != nil {
				t.Fatalf("NewClient failed: %v", err)
			}

			guid := "guid-401-challenge"
			filePath := helperCreateReplayFile(t, guid, 2048)

			res, err := client.UploadReplay(context.Background(), guid, filePath)

			// 1. Assert res == nil
			if res != nil {
				t.Fatalf("expected nil result on HTTP 401 Unauthorized, got: %+v", res)
			}

			// 2. Assert fatal failure returning ErrInvalidAPIKey
			if err == nil {
				t.Fatal("expected error on HTTP 401 Unauthorized, got nil")
			}
			if !errors.Is(err, ErrInvalidAPIKey) {
				t.Fatalf("expected ErrInvalidAPIKey, got: %v", err)
			}

			// 3. Assert strictly 0 retries (exactly 1 HTTP request made)
			reqs := atomic.LoadInt32(&requestCount)
			if reqs != 1 {
				t.Fatalf("HTTP 401 must have strictly 0 retries (1 total request), but received %d requests", reqs)
			}
		})
	}
}

// Challenge 3: 400 Bad Request must immediately fail with descriptive server error text with strictly 0 retries.
func TestChallenge_400BadRequest_ZeroRetries(t *testing.T) {
	testCases := []struct {
		name         string
		responseJSON bool
		errorBody    string
		expectSubstr string
	}{
		{
			name:         "json error payload",
			responseJSON: true,
			errorBody:    `{"error": "replay CRC check failed: corrupt payload"}`,
			expectSubstr: "replay CRC check failed: corrupt payload",
		},
		{
			name:         "plaintext error payload",
			responseJSON: false,
			errorBody:    "malformed header structure",
			expectSubstr: "malformed header structure",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var requestCount int32

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&requestCount, 1)

				if tc.responseJSON {
					w.Header().Set("Content-Type", "application/json")
				} else {
					w.Header().Set("Content-Type", "text/plain")
				}
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(tc.errorBody))
			}))
			defer srv.Close()

			client, err := NewClient(ClientConfig{
				BaseURL:     srv.URL,
				APIKey:      "valid-key",
				MaxRetries:  5, // Set high retry limit: must NOT retry on 400
				BaseBackoff: 5 * time.Millisecond,
			})
			if err != nil {
				t.Fatalf("NewClient failed: %v", err)
			}

			guid := "guid-400-challenge"
			filePath := helperCreateReplayFile(t, guid, 2048)

			res, err := client.UploadReplay(context.Background(), guid, filePath)

			// 1. Assert res == nil
			if res != nil {
				t.Fatalf("expected nil result on HTTP 400 Bad Request, got: %+v", res)
			}

			// 2. Assert err wraps ErrBadRequest
			if err == nil {
				t.Fatal("expected error on HTTP 400 Bad Request, got nil")
			}
			if !errors.Is(err, ErrBadRequest) {
				t.Fatalf("expected ErrBadRequest, got: %v", err)
			}

			// 3. Assert descriptive server error text is preserved in error
			if !strings.Contains(err.Error(), tc.expectSubstr) {
				t.Fatalf("expected error string to contain %q, got: %v", tc.expectSubstr, err)
			}

			// 4. Assert strictly 0 retries (exactly 1 HTTP request made)
			reqs := atomic.LoadInt32(&requestCount)
			if reqs != 1 {
				t.Fatalf("HTTP 400 must have strictly 0 retries (1 total request), but received %d requests", reqs)
			}
		})
	}
}

// Challenge 4: Bearer prefix: assert that passing Bearer <key> is rejected by the server as 401.
func TestChallenge_BearerPrefix_RejectedAs401(t *testing.T) {
	var capturedAuthHeaders []string
	var requestCount int32

	// Server strictly requires raw token and rejects any "Bearer " prefix with 401 Unauthorized
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		auth := r.Header.Get("Authorization")
		capturedAuthHeaders = append(capturedAuthHeaders, auth)

		if strings.HasPrefix(auth, "Bearer ") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "Bearer token not supported. Provide raw API key.",
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"chaser": true})
	}))
	defer srv.Close()

	const rawToken = "super-secret-token-123"
	bearerToken := "Bearer " + rawToken

	client, err := NewClient(ClientConfig{
		BaseURL:    srv.URL,
		APIKey:     bearerToken,
		MaxRetries: 3,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	// 1. Test UploadReplay with Bearer prefix
	guid := "guid-bearer-challenge"
	filePath := helperCreateReplayFile(t, guid, 2048)

	res, err := client.UploadReplay(context.Background(), guid, filePath)
	if err == nil {
		t.Fatal("expected UploadReplay to fail when Bearer prefix is used")
	}
	if !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("expected ErrInvalidAPIKey on Bearer rejection, got: %v", err)
	}
	if res != nil {
		t.Fatalf("expected nil result on Bearer rejection, got: %+v", res)
	}

	// 2. Test Ping with Bearer prefix
	err = client.Ping(context.Background())
	if err == nil {
		t.Fatal("expected Ping to fail when Bearer prefix is used")
	}
	if !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("expected ErrInvalidAPIKey on Bearer Ping, got: %v", err)
	}

	// 3. Verify server received the verbatim "Bearer " header (proving client did not mutate it)
	if len(capturedAuthHeaders) != 2 {
		t.Fatalf("expected exactly 2 requests (1 upload, 1 ping), got %d", len(capturedAuthHeaders))
	}
	for i, h := range capturedAuthHeaders {
		if h != bearerToken {
			t.Fatalf("request %d: expected Authorization header %q, got %q", i, bearerToken, h)
		}
	}
}

// Challenge 5: Ping(ctx): assert 200 OK returns nil error, 401 returns ErrInvalidAPIKey.
func TestChallenge_Ping_ResponseHandling(t *testing.T) {
	t.Run("200 OK returns nil error", func(t *testing.T) {
		var pingCount int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&pingCount, 1)
			if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"chaser":   true,
				"type":     "regular",
				"name":     "EmpiricalChallenger",
				"steam_id": "76561198000000001",
			})
		}))
		defer srv.Close()

		client, err := NewClient(ClientConfig{
			BaseURL: srv.URL,
			APIKey:  "valid-api-key",
		})
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		err = client.Ping(context.Background())
		if err != nil {
			t.Fatalf("expected nil error on 200 OK Ping, got: %v", err)
		}
		if atomic.LoadInt32(&pingCount) != 1 {
			t.Fatalf("expected exactly 1 ping request, got %d", pingCount)
		}
	})

	t.Run("401 Unauthorized returns ErrInvalidAPIKey", func(t *testing.T) {
		var pingCount int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&pingCount, 1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "Invalid API key.",
			})
		}))
		defer srv.Close()

		client, err := NewClient(ClientConfig{
			BaseURL: srv.URL,
			APIKey:  "invalid-api-key",
		})
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		err = client.Ping(context.Background())
		if err == nil {
			t.Fatal("expected error on 401 Ping, got nil")
		}
		if !errors.Is(err, ErrInvalidAPIKey) {
			t.Fatalf("expected ErrInvalidAPIKey, got: %v", err)
		}
		if atomic.LoadInt32(&pingCount) != 1 {
			t.Fatalf("expected exactly 1 ping request, got %d", pingCount)
		}
	})

	t.Run("non-401 non-200 returns descriptive status error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("service under maintenance"))
		}))
		defer srv.Close()

		client, err := NewClient(ClientConfig{
			BaseURL: srv.URL,
			APIKey:  "any-key",
		})
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		err = client.Ping(context.Background())
		if err == nil {
			t.Fatal("expected error on 503 Ping, got nil")
		}
		if errors.Is(err, ErrInvalidAPIKey) {
			t.Fatal("503 error should not be mapped to ErrInvalidAPIKey")
		}
		if !strings.Contains(err.Error(), "503") || !strings.Contains(err.Error(), "service under maintenance") {
			t.Fatalf("expected error to contain 503 and maintenance message, got: %v", err)
		}
	})
}
