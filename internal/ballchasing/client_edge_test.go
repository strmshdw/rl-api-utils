package ballchasing

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/testutil"
)

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
		RateLimiter: NewRateLimiter(time.Nanosecond),
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
		RateLimiter: NewRateLimiter(time.Nanosecond),
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
