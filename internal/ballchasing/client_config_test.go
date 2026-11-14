package ballchasing

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/testutil"
)

// ============================================================================
// Scenario 8: Context cancellation during upload or backoff
// ============================================================================

func TestClient_Upload_ContextCancellation(t *testing.T) {
	srv := testutil.NewMockBallchasingServer()
	defer srv.Close()

	client, err := NewClient(ClientConfig{
		BaseURL:     srv.URL(),
		APIKey:      defaultTestToken,
		MaxRetries:  3,
		BaseBackoff: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	guid := "test-guid-cancel"
	filePath := helperCreateReplayFile(t, guid, 2048)

	// 8A. Pre-cancelled context
	t.Run("pre-cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		res, err := client.UploadReplay(ctx, guid, filePath)
		if err == nil {
			t.Fatal("expected error with pre-cancelled context, got nil")
		}
		if res != nil {
			t.Fatalf("expected nil result, got %+v", res)
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got: %v", err)
		}
		if srv.GetUploadCount() != 0 {
			t.Fatalf("expected 0 uploads on pre-cancelled context, got %d", srv.GetUploadCount())
		}
	})

	// 8B. Cancellation during backoff sleep
	t.Run("cancellation during rate limit backoff", func(t *testing.T) {
		srv.SimulateRateLimit(5, 10) // 10s Retry-After

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		start := time.Now()
		_, err := client.UploadReplay(ctx, guid, filePath)
		elapsed := time.Since(start)

		if err == nil {
			t.Fatal("expected context deadline error during backoff sleep")
		}
		if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context deadline exceeded, got: %v", err)
		}
		if elapsed > 1*time.Second {
			t.Fatalf("backoff sleep did not abort promptly on context cancellation: took %v", elapsed)
		}
	})
}

// ============================================================================
// Scenario 9: Ping method (200 OK vs 401 Unauthorized)
// ============================================================================

func TestClient_Ping(t *testing.T) {
	srv := testutil.NewMockBallchasingServer()
	defer srv.Close()

	t.Run("valid API key returns 200 OK", func(t *testing.T) {
		client, err := NewClient(ClientConfig{
			BaseURL: srv.URL(),
			APIKey:  defaultTestToken,
		})
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		if err := client.Ping(context.Background()); err != nil {
			t.Fatalf("Ping failed unexpectedly with valid token: %v", err)
		}
	})

	t.Run("invalid API key returns 401 Unauthorized", func(t *testing.T) {
		client, err := NewClient(ClientConfig{
			BaseURL: srv.URL(),
			APIKey:  "invalid-wrong-token",
		})
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		err = client.Ping(context.Background())
		if err == nil {
			t.Fatal("expected error with invalid token, got nil")
		}
		if !errors.Is(err, ErrInvalidAPIKey) && !strings.Contains(err.Error(), "401") {
			t.Fatalf("expected ErrInvalidAPIKey, got: %v", err)
		}
	})

	t.Run("Bearer prefix returns 401 Unauthorized", func(t *testing.T) {
		client, err := NewClient(ClientConfig{
			BaseURL: srv.URL(),
			APIKey:  "Bearer " + defaultTestToken,
		})
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		err = client.Ping(context.Background())
		if err == nil {
			t.Fatal("expected error when Bearer prefix is used in Ping")
		}
		if !errors.Is(err, ErrInvalidAPIKey) && !strings.Contains(err.Error(), "401") {
			t.Fatalf("expected ErrInvalidAPIKey, got: %v", err)
		}
	})

	t.Run("server error returns status error", func(t *testing.T) {
		errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("server error"))
		}))
		defer errorServer.Close()

		client, err := NewClient(ClientConfig{
			BaseURL: errorServer.URL,
			APIKey:  defaultTestToken,
		})
		if err != nil {
			t.Fatalf("NewClient failed: %v", err)
		}

		err = client.Ping(context.Background())
		if err == nil {
			t.Fatal("expected error on 500 status, got nil")
		}
	})
}

// ============================================================================
// Scenario 10: Visibility parameter propagation
// ============================================================================

func TestClient_Visibility_Propagation(t *testing.T) {
	srv := testutil.NewMockBallchasingServer()
	defer srv.Close()

	tests := []struct {
		visibility   string
		expectConfig bool
	}{
		{"public", true},
		{"unlisted", true},
		{"private", true},
		{"invalid-vis", false},
	}

	for _, tc := range tests {
		t.Run(tc.visibility, func(t *testing.T) {
			client, err := NewClient(ClientConfig{
				BaseURL:     srv.URL(),
				APIKey:      defaultTestToken,
				Visibility:  tc.visibility,
				BaseBackoff: 10 * time.Millisecond,
			})

			if !tc.expectConfig {
				if err == nil {
					t.Fatalf("expected error for invalid visibility %q, got nil", tc.visibility)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected NewClient error for visibility %q: %v", tc.visibility, err)
			}

			guid := fmt.Sprintf("guid-vis-%s", tc.visibility)
			filePath := helperCreateReplayFile(t, guid, 2048)

			res, err := client.UploadReplay(context.Background(), guid, filePath)
			if err != nil {
				t.Fatalf("upload failed for visibility %q: %v", tc.visibility, err)
			}
			if res == nil {
				t.Fatal("expected non-nil result")
			}

			rec := srv.GetUpload(guid)
			if rec == nil {
				t.Fatalf("expected upload record on mock server for guid %s", guid)
			}
			if rec.Visibility != tc.visibility {
				t.Fatalf("expected recorded visibility %q, got %q", tc.visibility, rec.Visibility)
			}
		})
	}
}

// ============================================================================
// Scenario 11: Group query parameter propagation
// ============================================================================

func TestClient_Group_Propagation(t *testing.T) {
	srv := testutil.NewMockBallchasingServer()
	defer srv.Close()

	const groupID = "scrims-week-42"
	client, err := NewClient(ClientConfig{
		BaseURL:    srv.URL(),
		APIKey:     defaultTestToken,
		Visibility: "public",
		Group:      groupID,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	guid := "test-guid-group"
	filePath := helperCreateReplayFile(t, guid, 2048)

	res, err := client.UploadReplay(context.Background(), guid, filePath)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil result")
	}

	rec := srv.GetUpload(guid)
	if rec == nil || rec.Group != groupID {
		t.Fatalf("expected group %q, got %v", groupID, rec)
	}
}

// ============================================================================
// Scenario 12: Input validation edge cases
// ============================================================================

func TestClient_InputValidation(t *testing.T) {
	srv := testutil.NewMockBallchasingServer()
	defer srv.Close()

	t.Run("empty API key on client creation", func(t *testing.T) {
		_, err := NewClient(ClientConfig{
			BaseURL: srv.URL(),
			APIKey:  "",
		})
		if err == nil {
			t.Fatal("expected error with empty API key, got nil")
		}
		if !errors.Is(err, ErrEmptyAPIKey) {
			t.Fatalf("expected ErrEmptyAPIKey, got: %v", err)
		}
	})

	t.Run("empty match GUID on upload", func(t *testing.T) {
		client, _ := NewClient(ClientConfig{
			BaseURL: srv.URL(),
			APIKey:  defaultTestToken,
		})
		filePath := helperCreateReplayFile(t, "guid-val", 2048)

		_, err := client.UploadReplay(context.Background(), "", filePath)
		if err == nil {
			t.Fatal("expected error for empty matchGUID")
		}
		if !errors.Is(err, ErrEmptyMatchGUID) {
			t.Fatalf("expected ErrEmptyMatchGUID, got: %v", err)
		}
	})

	t.Run("whitespace match GUID on upload", func(t *testing.T) {
		client, _ := NewClient(ClientConfig{
			BaseURL: srv.URL(),
			APIKey:  defaultTestToken,
		})
		filePath := helperCreateReplayFile(t, "guid-ws", 2048)

		_, err := client.UploadReplay(context.Background(), "   ", filePath)
		if err == nil {
			t.Fatal("expected error for whitespace matchGUID")
		}
		if !errors.Is(err, ErrEmptyMatchGUID) {
			t.Fatalf("expected ErrEmptyMatchGUID, got: %v", err)
		}
	})

	t.Run("empty file path on upload", func(t *testing.T) {
		client, _ := NewClient(ClientConfig{
			BaseURL: srv.URL(),
			APIKey:  defaultTestToken,
		})

		_, err := client.UploadReplay(context.Background(), "guid-ok", "")
		if err == nil {
			t.Fatal("expected error for empty file path")
		}
		if !errors.Is(err, ErrEmptyFilePath) {
			t.Fatalf("expected ErrEmptyFilePath, got: %v", err)
		}
	})
}

// ============================================================================
// Scenario 13: Retry-After Header variations (zero, date, non-numeric)
// ============================================================================

func TestClient_Upload_RetryAfterVariations(t *testing.T) {
	srv := testutil.NewMockBallchasingServer()
	defer srv.Close()

	t.Run("Retry-After: 0 causes immediate retry without sleeping", func(t *testing.T) {
		srv.SimulateRateLimit(1, 0)
		client, _ := NewClient(ClientConfig{
			BaseURL:     srv.URL(),
			APIKey:      defaultTestToken,
			MaxRetries:  2,
			BaseBackoff: 10 * time.Millisecond,
			RateLimiter: NewRateLimiter(time.Nanosecond),
		})
		guid := "guid-zero-retry"
		filePath := helperCreateReplayFile(t, guid, 2048)

		start := time.Now()
		res, err := client.UploadReplay(context.Background(), guid, filePath)
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("upload failed: %v", err)
		}
		if res == nil || res.ID == "" {
			t.Fatal("expected replay ID")
		}
		if elapsed > 500*time.Millisecond {
			t.Fatalf("Retry-After: 0 took too long (%v), expected near-instant retry", elapsed)
		}
	})

	t.Run("HTTP-date Retry-After falls back to exponential backoff", func(t *testing.T) {
		srv.SimulateRateLimit(1, 1)
		srv.SetCustomRetryAfterHeader("Wed, 21 Oct 2026 07:28:00 GMT")

		client, _ := NewClient(ClientConfig{
			BaseURL:     srv.URL(),
			APIKey:      defaultTestToken,
			MaxRetries:  2,
			BaseBackoff: 10 * time.Millisecond,
			MaxBackoff:  50 * time.Millisecond,
			RateLimiter: NewRateLimiter(time.Nanosecond),
		})
		guid := "guid-date-retry"
		filePath := helperCreateReplayFile(t, guid, 2048)

		res, err := client.UploadReplay(context.Background(), guid, filePath)
		if err != nil {
			t.Fatalf("upload failed on date fallback: %v", err)
		}
		if res == nil || res.ID == "" {
			t.Fatal("expected replay ID")
		}
	})

	t.Run("malformed non-numeric Retry-After falls back to exponential backoff", func(t *testing.T) {
		srv.SimulateRateLimit(1, 1)
		srv.SetCustomRetryAfterHeader("unparseable-garbage")

		client, _ := NewClient(ClientConfig{
			BaseURL:     srv.URL(),
			APIKey:      defaultTestToken,
			MaxRetries:  2,
			BaseBackoff: 10 * time.Millisecond,
			MaxBackoff:  50 * time.Millisecond,
			RateLimiter: NewRateLimiter(time.Nanosecond),
		})
		guid := "guid-garbage-retry"
		filePath := helperCreateReplayFile(t, guid, 2048)

		res, err := client.UploadReplay(context.Background(), guid, filePath)
		if err != nil {
			t.Fatalf("upload failed on garbage fallback: %v", err)
		}
		if res == nil || res.ID == "" {
			t.Fatal("expected replay ID")
		}
	})
}
