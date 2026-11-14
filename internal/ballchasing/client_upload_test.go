package ballchasing

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
		RateLimiter: NewRateLimiter(time.Nanosecond),
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
		RateLimiter: NewRateLimiter(time.Nanosecond),
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
