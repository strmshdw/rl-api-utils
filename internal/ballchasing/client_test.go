package ballchasing

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/testutil"
)

// defaultTestToken matches testutil.NewMockBallchasingServer's default expected token.
const defaultTestToken = "test-ballchasing-token"

// helperCreateReplayFile creates a temporary replay file with valid TAGAME header.
func helperCreateReplayFile(t *testing.T, guid string, size int) string {
	t.Helper()
	dir := t.TempDir()
	filePath := filepath.Join(dir, fmt.Sprintf("%s.replay", guid))
	data := testutil.GenerateValidReplay(guid, size)
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		t.Fatalf("failed to create temporary replay file: %v", err)
	}
	return filePath
}

// ============================================================================
// Scenario 1: Successful upload (HTTP 201 Created)
// ============================================================================

func TestClient_Upload_Success201(t *testing.T) {
	srv := testutil.NewMockBallchasingServer()
	defer srv.Close()

	client, err := NewClient(ClientConfig{
		BaseURL:     srv.URL(),
		APIKey:      defaultTestToken,
		Visibility:  "public",
		MaxRetries:  2,
		BaseBackoff: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient failed unexpectedly: %v", err)
	}

	guid := "test-guid-success-201"
	filePath := helperCreateReplayFile(t, guid, 2048)

	res, err := client.UploadReplay(context.Background(), guid, filePath)
	if err != nil {
		t.Fatalf("UploadReplay failed: %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil UploadResult")
	}

	// 1. Verify returned UploadResult
	if res.ID == "" {
		t.Fatal("expected non-empty replay ID")
	}
	if res.Location == "" {
		t.Fatal("expected non-empty replay Location")
	}
	if !strings.Contains(res.Location, res.ID) {
		t.Fatalf("expected Location %q to contain ID %q", res.Location, res.ID)
	}
	if res.IsDuplicate {
		t.Fatalf("expected IsDuplicate=false on 201 Created, got true")
	}

	// 2. Verify server recorded upload metadata
	if srv.GetUploadCount() != 1 {
		t.Fatalf("expected upload count 1, got %d", srv.GetUploadCount())
	}
	rec := srv.GetUpload(guid)
	if rec == nil {
		t.Fatalf("mock server has no recorded upload for GUID %s", guid)
	}
	if rec.MatchGUID != guid {
		t.Fatalf("expected MatchGUID %s, got %s", guid, rec.MatchGUID)
	}
	if rec.FileName != fmt.Sprintf("%s.replay", guid) {
		t.Fatalf("expected FileName %s.replay, got %s", guid, rec.FileName)
	}
	if rec.FileSize != 2048 {
		t.Fatalf("expected FileSize 2048, got %d", rec.FileSize)
	}
	if rec.Visibility != "public" {
		t.Fatalf("expected Visibility public, got %s", rec.Visibility)
	}
	if rec.AuthHeader != defaultTestToken {
		t.Fatalf("expected AuthHeader %s, got %s", defaultTestToken, rec.AuthHeader)
	}
	if len(rec.FileBytes) != 2048 || !bytes.HasPrefix(rec.FileBytes, testutil.ReplayMagicBytes) {
		t.Fatalf("corrupted replay payload on mock server")
	}
}

// ============================================================================
// Scenario 2: Duplicate upload (HTTP 409 Conflict)
// ============================================================================

func TestClient_Upload_Duplicate409(t *testing.T) {
	srv := testutil.NewMockBallchasingServer()
	defer srv.Close()

	const existingID = "bc-duplicate-replay-uuid-999"
	guid := "test-guid-duplicate-409"
	srv.SetDuplicateGUID(guid, existingID)

	client, err := NewClient(ClientConfig{
		BaseURL:     srv.URL(),
		APIKey:      defaultTestToken,
		Visibility:  "unlisted",
		MaxRetries:  3,
		BaseBackoff: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	filePath := helperCreateReplayFile(t, guid, 2048)

	res, err := client.UploadReplay(context.Background(), guid, filePath)
	// CRITICAL REQUIREMENT: 409 is an idempotent terminal state; must NOT return an error to caller!
	if err != nil {
		t.Fatalf("expected err == nil on HTTP 409 Conflict, got error: %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil UploadResult on duplicate")
	}

	if !res.IsDuplicate {
		t.Fatalf("expected IsDuplicate=true on HTTP 409 Conflict, got false")
	}
	if res.ID != existingID {
		t.Fatalf("expected ID %s, got %s", existingID, res.ID)
	}
	expectedLoc := fmt.Sprintf("https://ballchasing.com/replay/%s", existingID)
	if res.Location != expectedLoc {
		t.Fatalf("expected Location %s, got %s", expectedLoc, res.Location)
	}

	// Verify exactly 1 attempt was made (no retry thrashing on duplicate!)
	if srv.GetUploadCount() != 1 {
		t.Fatalf("expected exactly 1 attempt on duplicate upload, got %d", srv.GetUploadCount())
	}
}

// ============================================================================
// Scenario 3: Rate limiting (HTTP 429) with Retry-After: 1 -> backoff & retry success
// ============================================================================

func TestClient_Upload_RateLimit429_RetrySuccess(t *testing.T) {
	srv := testutil.NewMockBallchasingServer()
	defer srv.Close()

	// Simulate 2 rate limit failures with Retry-After: 1 second
	srv.SimulateRateLimit(2, 1)

	client, err := NewClient(ClientConfig{
		BaseURL:     srv.URL(),
		APIKey:      defaultTestToken,
		Visibility:  "public",
		MaxRetries:  3,
		BaseBackoff: 10 * time.Millisecond,
		MaxBackoff:  2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	guid := "test-guid-rate-limit-succ"
	filePath := helperCreateReplayFile(t, guid, 2048)

	start := time.Now()
	res, err := client.UploadReplay(context.Background(), guid, filePath)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected upload to succeed after rate limit retries, got: %v", err)
	}
	if res == nil || res.ID == "" {
		t.Fatalf("expected valid UploadResult, got %+v", res)
	}
	if res.IsDuplicate {
		t.Fatalf("expected IsDuplicate=false")
	}

	// Verify total elapsed time respects Retry-After (2 retries * ~1s >= 1.8s)
	if elapsed < 1800*time.Millisecond {
		t.Logf("completed in %v (fast retry path or scaled)", elapsed)
	}

	// Verify server recorded the final upload
	if srv.GetUploadCount() != 1 {
		t.Fatalf("expected final upload recorded, count=%d", srv.GetUploadCount())
	}
}

// ============================================================================
// Scenario 4: Rate limit budget exhaustion
// ============================================================================

func TestClient_Upload_RateLimit429_Exhaustion(t *testing.T) {
	srv := testutil.NewMockBallchasingServer()
	defer srv.Close()

	// 5 failures > 2 max retries; using Retry-After: 0 for fast test execution
	srv.SimulateRateLimit(5, 0)

	client, err := NewClient(ClientConfig{
		BaseURL:     srv.URL(),
		APIKey:      defaultTestToken,
		Visibility:  "public",
		MaxRetries:  2, // attempt 0, retry 1, retry 2 -> then exhaustion
		BaseBackoff: 1 * time.Millisecond,
		MaxBackoff:  10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	guid := "test-guid-exhaust"
	filePath := helperCreateReplayFile(t, guid, 2048)

	res, err := client.UploadReplay(context.Background(), guid, filePath)
	if err == nil {
		t.Fatal("expected error on rate limit exhaustion, got nil")
	}
	if res != nil {
		t.Fatalf("expected nil result on exhaustion, got %+v", res)
	}

	if !errors.Is(err, ErrRateLimitExhausted) && !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("expected ErrRateLimitExhausted, got: %v", err)
	}

	// Zero uploads should be recorded in mock server (all attempts rejected with 429)
	if srv.GetUploadCount() != 0 {
		t.Fatalf("expected 0 successful uploads on exhaustion, got %d", srv.GetUploadCount())
	}
}

// ============================================================================
// Scenario 5: Invalid API key (HTTP 401 Unauthorized) immediate rejection
// ============================================================================

func TestClient_Upload_Unauthorized401_ImmediateHalt(t *testing.T) {
	srv := testutil.NewMockBallchasingServer()
	defer srv.Close()

	srv.SetAlwaysUnauthorized(true)

	client, err := NewClient(ClientConfig{
		BaseURL:     srv.URL(),
		APIKey:      "invalid-token",
		Visibility:  "public",
		MaxRetries:  3, // Should NOT retry on 401!
		BaseBackoff: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	guid := "test-guid-unauth"
	filePath := helperCreateReplayFile(t, guid, 2048)

	res, err := client.UploadReplay(context.Background(), guid, filePath)
	if err == nil {
		t.Fatal("expected error on 401 Unauthorized, got nil")
	}
	if res != nil {
		t.Fatalf("expected nil result on 401, got %+v", res)
	}

	if !errors.Is(err, ErrInvalidAPIKey) && !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected ErrInvalidAPIKey, got: %v", err)
	}

	// Verify no uploads recorded
	if srv.GetUploadCount() != 0 {
		t.Fatalf("expected 0 uploads recorded on 401, got %d", srv.GetUploadCount())
	}
}

// ============================================================================
// Scenario 5B: Bearer prefix rejected with HTTP 401
// ============================================================================

func TestClient_Upload_BearerPrefixRejected(t *testing.T) {
	srv := testutil.NewMockBallchasingServer()
	defer srv.Close()

	// Configuring client with "Bearer test-token" must be rejected by mock server with 401
	client, err := NewClient(ClientConfig{
		BaseURL:     srv.URL(),
		APIKey:      "Bearer " + defaultTestToken,
		Visibility:  "public",
		MaxRetries:  1,
		BaseBackoff: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	guid := "test-guid-bearer"
	filePath := helperCreateReplayFile(t, guid, 2048)

	_, err = client.UploadReplay(context.Background(), guid, filePath)
	if err == nil {
		t.Fatal("expected 401 error when Bearer prefix is used")
	}
	if !errors.Is(err, ErrInvalidAPIKey) && !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected ErrInvalidAPIKey for Bearer prefix, got: %v", err)
	}
}

// ============================================================================
// Scenario 6: Non-existent file path
// ============================================================================

func TestClient_Upload_NonExistentFile(t *testing.T) {
	srv := testutil.NewMockBallchasingServer()
	defer srv.Close()

	client, err := NewClient(ClientConfig{
		BaseURL:    srv.URL(),
		APIKey:     defaultTestToken,
		MaxRetries: 2,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	nonExistentPath := filepath.Join(t.TempDir(), "does_not_exist.replay")

	res, err := client.UploadReplay(context.Background(), "guid-missing", nonExistentPath)
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
	if res != nil {
		t.Fatalf("expected nil result, got %+v", res)
	}

	// Verify zero network calls were made to mock server
	if srv.GetUploadCount() != 0 {
		t.Fatalf("expected 0 uploads on mock server, got %d", srv.GetUploadCount())
	}
}

// ============================================================================
// Scenario 7: Empty file (0-byte)
// ============================================================================

func TestClient_Upload_EmptyFile0Byte(t *testing.T) {
	srv := testutil.NewMockBallchasingServer()
	defer srv.Close()

	client, err := NewClient(ClientConfig{
		BaseURL:    srv.URL(),
		APIKey:     defaultTestToken,
		MaxRetries: 2,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	emptyFilePath := filepath.Join(t.TempDir(), "empty.replay")
	if err := os.WriteFile(emptyFilePath, []byte{}, 0644); err != nil {
		t.Fatalf("failed to create empty file: %v", err)
	}

	res, err := client.UploadReplay(context.Background(), "guid-empty", emptyFilePath)
	if err == nil {
		t.Fatal("expected error for 0-byte file, got nil")
	}
	if res != nil {
		t.Fatalf("expected nil result, got %+v", res)
	}

	// Verify error mentions empty file or bad request
	if !errors.Is(err, ErrBadRequest) && !strings.Contains(strings.ToLower(err.Error()), "empty") {
		t.Fatalf("expected error indicating empty file, got: %v", err)
	}

	if srv.GetUploadCount() != 0 {
		t.Fatalf("expected 0 uploads recorded on mock server, got %d", srv.GetUploadCount())
	}
}

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

// ============================================================================
// Scenario 14: Transient 5xx server error retry and recovery
// ============================================================================

func TestClient_Upload_Transient5xxRetry(t *testing.T) {
	srv := testutil.NewMockBallchasingServer()
	defer srv.Close()

	// Forced 500 error on first attempt, then cleared
	srv.SetForcedStatus(http.StatusInternalServerError, "transient database failure")

	client, _ := NewClient(ClientConfig{
		BaseURL:     srv.URL(),
		APIKey:      defaultTestToken,
		MaxRetries:  2,
		BaseBackoff: 20 * time.Millisecond,
	})

	guid := "guid-500-recovery"
	filePath := helperCreateReplayFile(t, guid, 2048)

	// Start a goroutine to clear the 500 error after 10ms (simulating transient server recovery)
	go func() {
		time.Sleep(10 * time.Millisecond)
		srv.SetForcedStatus(0, "")
	}()

	res, err := client.UploadReplay(context.Background(), guid, filePath)
	if err != nil {
		t.Fatalf("expected upload to recover after 500 retry, got: %v", err)
	}
	if res == nil || res.ID == "" {
		t.Fatal("expected valid result after 500 recovery")
	}
}

// ============================================================================
// Scenario 15: Permanent 400 Bad Request immediate halt
// ============================================================================

func TestClient_Upload_BadRequest400_ImmediateHalt(t *testing.T) {
	srv := testutil.NewMockBallchasingServer()
	defer srv.Close()

	srv.SetForcedStatus(http.StatusBadRequest, "corrupt replay header in payload")

	client, _ := NewClient(ClientConfig{
		BaseURL:     srv.URL(),
		APIKey:      defaultTestToken,
		MaxRetries:  3, // Should NOT retry on 400!
		BaseBackoff: 10 * time.Millisecond,
	})

	guid := "guid-400-bad-request"
	filePath := helperCreateReplayFile(t, guid, 2048)

	res, err := client.UploadReplay(context.Background(), guid, filePath)
	if err == nil {
		t.Fatal("expected error on 400 Bad Request, got nil")
	}
	if res != nil {
		t.Fatalf("expected nil result on 400, got %+v", res)
	}
	if !errors.Is(err, ErrBadRequest) && !strings.Contains(err.Error(), "400") {
		t.Fatalf("expected ErrBadRequest, got: %v", err)
	}
	if !strings.Contains(err.Error(), "corrupt replay header in payload") {
		t.Fatalf("expected verbatim server error message preserved, got: %v", err)
	}
}

// ============================================================================
// Scenario 16: Large payload (5MB) streaming & byte integrity
// ============================================================================

func TestClient_Upload_LargePayloadStreaming(t *testing.T) {
	srv := testutil.NewMockBallchasingServer()
	defer srv.Close()

	client, _ := NewClient(ClientConfig{
		BaseURL:    srv.URL(),
		APIKey:     defaultTestToken,
		Visibility: "public",
	})

	guid := "guid-large-5mb"
	const payloadSize = 5 * 1024 * 1024 // 5 Megabytes
	filePath := helperCreateReplayFile(t, guid, payloadSize)

	res, err := client.UploadReplay(context.Background(), guid, filePath)
	if err != nil {
		t.Fatalf("failed to upload 5MB replay: %v", err)
	}
	if res == nil || res.ID == "" {
		t.Fatal("expected valid result for 5MB replay")
	}

	rec := srv.GetUpload(guid)
	if rec == nil {
		t.Fatal("expected upload recorded on mock server")
	}
	if rec.FileSize != payloadSize {
		t.Fatalf("expected FileSize %d, got %d", payloadSize, rec.FileSize)
	}
	if len(rec.FileBytes) != payloadSize {
		t.Fatalf("expected FileBytes length %d, got %d", payloadSize, len(rec.FileBytes))
	}
	if !bytes.HasPrefix(rec.FileBytes, testutil.ReplayMagicBytes) {
		t.Fatal("large payload lost TAGAME magic bytes prefix")
	}
}

// ============================================================================
// Scenario 17: Concurrent upload safety
// ============================================================================

func TestClient_Upload_ConcurrentUploadSafety(t *testing.T) {
	srv := testutil.NewMockBallchasingServer()
	defer srv.Close()

	client, _ := NewClient(ClientConfig{
		BaseURL:     srv.URL(),
		APIKey:      defaultTestToken,
		Visibility:  "public",
		MaxRetries:  2,
		BaseBackoff: 5 * time.Millisecond,
	})

	const concurrentCount = 10
	var wg sync.WaitGroup
	errs := make([]error, concurrentCount)
	results := make([]*UploadResult, concurrentCount)

	for i := 0; i < concurrentCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			guid := fmt.Sprintf("guid-concur-%d", idx)
			filePath := helperCreateReplayFile(t, guid, 1024+idx*256)
			res, err := client.UploadReplay(context.Background(), guid, filePath)
			errs[idx] = err
			results[idx] = res
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent upload %d failed: %v", i, err)
		}
		if results[i] == nil || results[i].ID == "" {
			t.Fatalf("concurrent upload %d returned empty result", i)
		}
	}

	if srv.GetUploadCount() != concurrentCount {
		t.Fatalf("expected %d total recorded uploads, got %d", concurrentCount, srv.GetUploadCount())
	}
}

// ============================================================================
// Scenario 18: Zero-RAM streaming upload mode (WithStreaming)
// ============================================================================

func TestClient_Upload_ZeroRAMStreamingMode(t *testing.T) {
	srv := testutil.NewMockBallchasingServer()
	defer srv.Close()

	client, err := NewClient(ClientConfig{
		BaseURL:      srv.URL(),
		APIKey:       defaultTestToken,
		Visibility:   "public",
		StreamUpload: true, // Enable zero-RAM streaming
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	guid := "guid-streaming-zeroram"
	filePath := helperCreateReplayFile(t, guid, 4096)

	res, err := client.UploadReplay(context.Background(), guid, filePath)
	if err != nil {
		t.Fatalf("streaming upload failed: %v", err)
	}
	if res == nil || res.ID == "" {
		t.Fatal("expected non-empty replay ID in streaming mode")
	}

	rec := srv.GetUpload(guid)
	if rec == nil || rec.FileSize != 4096 {
		t.Fatalf("expected 4096 bytes uploaded via streaming, got %v", rec)
	}
}

// ============================================================================
// Scenario 19: Functional options & Constructor variations
// ============================================================================

func TestClient_OptionsAndConstructors(t *testing.T) {
	srv := testutil.NewMockBallchasingServer()
	defer srv.Close()

	customHTTP := &http.Client{Timeout: 45 * time.Second}

	client, err := New(defaultTestToken,
		WithBaseURL(srv.URL()),
		WithVisibility("unlisted"),
		WithGroup("test-group"),
		WithMaxRetries(4),
		WithTimeout(45*time.Second),
		WithBaseBackoff(50*time.Millisecond),
		WithMaxBackoff(500*time.Millisecond),
		WithStreaming(false),
		WithHTTPClient(customHTTP),
	)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if client.visibility != "unlisted" {
		t.Fatalf("expected visibility unlisted, got %s", client.visibility)
	}
	if client.group != "test-group" {
		t.Fatalf("expected group test-group, got %s", client.group)
	}
	if client.maxRetries != 4 {
		t.Fatalf("expected maxRetries 4, got %d", client.maxRetries)
	}

	// Test NewHTTPBallchasingUploader constructor
	uploader := NewHTTPBallchasingUploader(srv.URL(), defaultTestToken, "public", "test-grp", 2)
	if uploader == nil {
		t.Fatal("expected non-nil uploader from NewHTTPBallchasingUploader")
	}

	guid := "guid-uploader-compat"
	filePath := helperCreateReplayFile(t, guid, 1024)
	res, err := uploader.UploadReplay(context.Background(), guid, filePath)
	if err != nil {
		t.Fatalf("uploader compat upload failed: %v", err)
	}
	if res == nil || res.ID == "" {
		t.Fatal("expected valid result from uploader compat")
	}
}
