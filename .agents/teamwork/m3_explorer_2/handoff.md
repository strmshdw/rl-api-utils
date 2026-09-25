# M3 Investigation Report: Response Handling, Duplicate Detection & Rate Limiting Backoff Engine

**Role**: `m3_explorer_2` (Explorer)  
**Milestone**: M3 — Ballchasing Replay Uploader (`internal/ballchasing`)  
**Date**: 2026-09-25T04:02:00Z  

---

## 1. Observation

Direct investigation of the project specifications (`PROJECT.md`), mining survey findings (`survey_miner_ballchasing_1/handoff.md`), mock implementation (`internal/testutil/mock_ballchasing.go`), existing E2E test harness (`test/e2e/e2e_test.go`), and test specifications (`test/e2e/tier1_feature_test.go`) revealed the following concrete contracts, schemas, and runtime behaviors:

### 1.1 Interface and Result Models
- In `PROJECT.md` (lines 168–182):
  ```go
  type UploadResult struct {
      ID          string
      Location    string
      IsDuplicate bool
  }

  type ReplayUploader interface {
      UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error)
  }
  ```
- In `DISPATCH.md`:
  `Ping(ctx)` method querying `GET /` to validate API key before starting synchronization.

### 1.2 Status Code Semantics & Response Payloads

#### HTTP 201 Created (Success)
- Observed in `internal/testutil/mock_ballchasing.go` (lines 350–355):
  - HTTP Header: `Location: https://ballchasing.com/replay/<id>`
  - Body JSON: `{"id": "<id>", "location": "https://ballchasing.com/replay/<id>"}`
- Result expectation:
  `UploadResult{ ID: "<id>", Location: "<location>", IsDuplicate: false }`, `err == nil`.

#### HTTP 409 Conflict (Idempotent Duplicate)
- Observed in `internal/testutil/mock_ballchasing.go` (lines 322–329):
  - HTTP Header: `Content-Type: application/json`
  - Body JSON: `{"error": "duplicate replay", "id": "<id>", "location": "https://ballchasing.com/replay/<id>"}`
- Observed in `test/e2e/tier1_feature_test.go` (lines 927–971):
  - `res.IsDuplicate == true`
  - `res.ID == "existing-id-409"`
  - `err == nil` (must NOT return error to caller: `t.Fatalf("409 duplicate replay must not return an error to caller, got: %v", err)`)
  - Upload attempt count must be exactly 1 (`t.Fatalf("expected exactly 1 upload attempt on 409, got %d", h.BC.GetUploadCount())` in `TestTier1_F11_409_ZeroRetriesAttempted`).

#### HTTP 429 Too Many Requests (Rate Limiting)
- Observed in `internal/testutil/mock_ballchasing.go` (lines 244–256):
  - Header: `Retry-After: <seconds>` or custom HTTP-date string
  - Body JSON: `{"error": "too many requests"}`
- Observed in `test/e2e/tier1_feature_test.go` (lines 994–1066):
  - `TestTier1_F12_429_ParseRetryAfterHeader`: integer `Retry-After: 1` parsed, backoff applied, retry succeeds.
  - `TestTier1_F12_429_BackoffAndSucceedOnRetry`: succeeds after 2 consecutive 429s.
  - `TestTier1_F12_429_ExhaustionErrorAfterMaxRetries`: fails when 429 count (5) exceeds `maxRetries` (2).
  - `TestTier1_F12_429_NonIntegerRetryAfterFallback`: header `"Wed, 21 Oct 2026 07:28:00 GMT"` falls back to exponential backoff and succeeds without hanging.
  - `TestTier1_F12_429_ZeroRetryAfterImmediateRetry`: `Retry-After: 0` causes immediate retry without sleeping.

#### HTTP 401 Unauthorized (Permanent Authentication Failure)
- Observed in `internal/testutil/mock_ballchasing.go` (lines 198–202, 234–239):
  - Returned if `Authorization` header is empty, incorrect token, or prefixed with `"Bearer "`.
  - Body JSON: `{"error": "Invalid API key."}`
- Observed in `test/e2e/tier1_feature_test.go` (lines 1072–1084):
  - Fails immediately on first attempt without retrying (`TestTier1_F13_401_ImmediateHaltNoRetry`).
  - Must return `ErrInvalidAPIKey`.

#### HTTP 400 Bad Request & Permanent Errors
- Observed in `test/e2e/tier1_feature_test.go` (lines 1086–1111):
  - `TestTier1_F13_400_BadRequestImmediateHalt`: fails immediately without retry.
  - `TestTier1_F13_Error_DescriptiveErrorPropagation`: error contains verbatim error text returned by server (e.g. `"corrupt replay binary"` or `"specific syntax error in replay"`).

### 1.3 Ping / API Key Verification (`GET /`)
- Observed in `internal/testutil/mock_ballchasing.go` (lines 176–213):
  - Method: `GET`
  - Path: `/`, `/api`, or `/api/`
  - Header: `Authorization: <token>` (raw token, no `Bearer `)
  - Returns `200 OK` with JSON `{"chaser": true, "type": "regular", "name": "MockUser", "steam_id": "76561198000000000"}` on success.
  - Returns `401 Unauthorized` with JSON `{"error": "Invalid API key."}` on failure.

---

## 2. Logic Chain

From the direct observations, the system behavior follows a rigorous causal logic chain:

### 2.1 HTTP 201 vs HTTP 409 Resolution
1. **Observation**: Ballchasing API returns HTTP 201 when a replay is newly ingested, and HTTP 409 when the replay has already been processed (either by the same user or an opponent in the match). Both responses include `id` and `location`.
2. **Logic Step**: In replay synchronization, finding that a match is already on Ballchasing.com is a completely valid and desired terminal state. It is not an error condition. Returning an error would cause the syncer to mark the match as failed or attempt re-uploads.
3. **Deduplication Rule**:
   - HTTP 201 -> `UploadResult{ID: res.ID, Location: res.Location, IsDuplicate: false}`, `err = nil`.
   - HTTP 409 -> `UploadResult{ID: res.ID, Location: res.Location, IsDuplicate: true}`, `err = nil`.
   - Both HTTP 201 and 409 are terminal states: `attempt` counter halts immediately with **0 retries**.

### 2.2 Rate Limiting & Backoff Engine (HTTP 429)
1. **Observation**: Ballchasing rate-limits burst uploads (>2 req/sec) and periodic quotas, signaling throttling with HTTP 429 and `Retry-After`.
2. **Logic Step**:
   - Parse `Retry-After` header:
     - Case A: Integer string (e.g. `"5"`, `"1"`): `waitDuration = time.Duration(secs) * time.Second`. If `secs == 0`, `waitDuration = 0`.
     - Case B: HTTP-date string (RFC 1123 / RFC 850 / ANSI C via `http.ParseTime`): `waitDuration = time.Until(t)`. If the date is excessively in the future (`> maxBackoff`, e.g. test scenario with date weeks away) or in the past, discard and fall back to exponential backoff.
     - Case C: Absent, empty, or unparseable: apply exponential backoff `baseBackoff * (2 ^ attempt)`.
   - Apply Jitter: Add uniform random jitter to prevent synchronized retry spikes from multiple clients.
   - Retry Budget Check:
     - Check `if attempt >= maxRetries`: halt retry loop and return `ErrRateLimitExhausted`.
     - If `maxRetries == 0`: attempt 0 fails immediately without sleeping.
   - Context Cancellation:
     - Sleep using a timer and `select` against `ctx.Done()`. If context expires or is cancelled (e.g. daemon shutdown or per-upload timeout), abort immediately returning `ctx.Err()`.

### 2.3 Permanent Rejection & Fatal Authentication (HTTP 401 / 400)
1. **Observation**: HTTP 401 indicates invalid or revoked credentials (or illegal `Bearer ` prefix). HTTP 400 indicates invalid multipart payload or corrupt file.
2. **Logic Step**:
   - HTTP 401 will never succeed with retries without configuration changes. Retrying will only waste network and quota. Therefore, on 401, immediately abort all retries and return `ErrInvalidAPIKey`.
   - HTTP 400 is an invalid client request. Abort retries and return descriptive error wrapping `ErrBadRequest`.

### 2.4 Ping Verification (`Ping(ctx)`)
1. **Observation**: `GET /` verifies the API key and returns 200 OK or 401 Unauthorized.
2. **Logic Step**:
   - Before the synchronizer starts polling match history or downloading replays, calling `Ping(ctx)` guarantees the Ballchasing API key is valid.
   - If `Ping` returns `ErrInvalidAPIKey`, daemon startup can fail fast, saving CPU and bandwidth before any downloads commence.

---

## 3. Proposed Code & Architecture

Below is the concrete proposed implementation for `internal/ballchasing/types.go` and the core response/backoff engine in `internal/ballchasing/client.go`.

### 3.1 Data Types and Sentinel Errors (`internal/ballchasing/types.go`)

```go
package ballchasing

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Sentinel errors returned by the Ballchasing client.
var (
	// ErrInvalidAPIKey is returned when Ballchasing responds with HTTP 401 Unauthorized.
	ErrInvalidAPIKey = errors.New("ballchasing: invalid or unauthorized API key (HTTP 401)")

	// ErrRateLimitExhausted is returned when HTTP 429 retries exceed the retry budget.
	ErrRateLimitExhausted = errors.New("ballchasing: rate limit retries exhausted (HTTP 429)")

	// ErrBadRequest is returned when Ballchasing rejects an upload with HTTP 400 Bad Request.
	ErrBadRequest = errors.New("ballchasing: bad request (HTTP 400)")

	// ErrNotFound is returned when an endpoint or resource is not found (HTTP 404).
	ErrNotFound = errors.New("ballchasing: resource not found (HTTP 404)")

	// ErrServerError is returned when Ballchasing returns an unrecoverable 5xx server error.
	ErrServerError = errors.New("ballchasing: server error (HTTP 5xx)")

	// ErrEmptyMatchGUID is returned when matchGUID is empty or whitespace.
	ErrEmptyMatchGUID = errors.New("ballchasing: match GUID cannot be empty")

	// ErrEmptyFilePath is returned when replay file path is empty.
	ErrEmptyFilePath = errors.New("ballchasing: replay file path cannot be empty")

	// ErrEmptyAPIKey is returned when the configured API key is empty.
	ErrEmptyAPIKey = errors.New("ballchasing: API key cannot be empty")
)

// UploadResult captures the outcome of a replay upload.
type UploadResult struct {
	ID          string `json:"id"`
	Location    string `json:"location"`
	IsDuplicate bool   `json:"is_duplicate"`
}

// PingResponse represents the account metadata returned by GET / (ping).
type PingResponse struct {
	Chaser  bool   `json:"chaser"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	SteamID string `json:"steam_id"`
}

// ReplayUploader defines the contract for uploading Rocket League replays to Ballchasing.com.
type ReplayUploader interface {
	UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error)
	Ping(ctx context.Context) error
}

// ClientConfig holds configuration settings for the Ballchasing client.
type ClientConfig struct {
	BaseURL     string
	APIKey      string
	Visibility  string // "public", "unlisted", "private"
	Group       string
	MaxRetries  int
	Timeout     time.Duration
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
}
```

### 3.2 Response Handling & Backoff Implementation (`internal/ballchasing/client.go`)

```go
package ballchasing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultBaseURL     = "https://ballchasing.com/api"
	DefaultVisibility  = "public"
	DefaultMaxRetries  = 3
	DefaultTimeout     = 60 * time.Second
	DefaultBaseBackoff = 1 * time.Second
	DefaultMaxBackoff  = 30 * time.Second
)

// Client implements ReplayUploader against the Ballchasing REST API.
type Client struct {
	httpClient  *http.Client
	baseURL     string
	apiKey      string
	visibility  string
	group       string
	maxRetries  int
	baseBackoff time.Duration
	maxBackoff  time.Duration
}

// Option allows configuring optional Client settings.
type Option func(*Client)

func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client != nil {
			c.httpClient = client
		}
	}
}

func WithGroup(group string) Option {
	return func(c *Client) {
		c.group = group
	}
}

func WithBaseBackoff(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.baseBackoff = d
		}
	}
}

func WithMaxBackoff(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.maxBackoff = d
		}
	}
}

// NewClient initializes a Ballchasing API client.
func NewClient(cfg ClientConfig, opts ...Option) (*Client, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, ErrEmptyAPIKey
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	visibility := strings.ToLower(strings.TrimSpace(cfg.Visibility))
	if visibility == "" {
		visibility = DefaultVisibility
	}
	if visibility != "public" && visibility != "unlisted" && visibility != "private" {
		return nil, fmt.Errorf("ballchasing: invalid visibility %q (must be 'public', 'unlisted', or 'private')", visibility)
	}

	maxRetries := cfg.MaxRetries
	if maxRetries < 0 {
		maxRetries = DefaultMaxRetries
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	baseBackoff := cfg.BaseBackoff
	if baseBackoff <= 0 {
		baseBackoff = DefaultBaseBackoff
	}

	maxBackoff := cfg.MaxBackoff
	if maxBackoff <= 0 {
		maxBackoff = DefaultMaxBackoff
	}

	c := &Client{
		httpClient:  &http.Client{Timeout: timeout},
		baseURL:     baseURL,
		apiKey:      apiKey,
		visibility:  visibility,
		group:       cfg.Group,
		maxRetries:  maxRetries,
		baseBackoff: baseBackoff,
		maxBackoff:  maxBackoff,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c, nil
}

// Ping verifies API key validity by querying GET / (or GET /api/).
func (c *Client) Ping(ctx context.Context) error {
	endpoint := fmt.Sprintf("%s/", strings.TrimRight(c.baseURL, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("ballchasing: create ping request: %w", err)
	}

	req.Header.Set("Authorization", c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ballchasing: ping request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized:
		return ErrInvalidAPIKey
	default:
		return fmt.Errorf("ballchasing: ping returned HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}
}

// rawUploadResponse handles deserialization of both 201 and 409 responses.
type rawUploadResponse struct {
	Error    string `json:"error"`
	ID       string `json:"id"`
	Location string `json:"location"`
}

// UploadReplay streams a .replay file to Ballchasing POST /v2/upload.
func (c *Client) UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error) {
	if strings.TrimSpace(matchGUID) == "" {
		return nil, ErrEmptyMatchGUID
	}
	if strings.TrimSpace(filePath) == "" {
		return nil, ErrEmptyFilePath
	}

	// Validate file existence and accessibility
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("ballchasing: replay file stat failed: %w", err)
	}
	if fileInfo.Size() == 0 {
		return nil, fmt.Errorf("%w: empty replay file (0 bytes)", ErrBadRequest)
	}

	endpoint := fmt.Sprintf("%s/v2/upload?visibility=%s", c.baseURL, c.visibility)
	if c.group != "" {
		endpoint += "&group=" + c.group
	}

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		// Respect context cancellation before every attempt
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		result, waitDuration, retryable, err := c.doUploadAttempt(ctx, endpoint, matchGUID, filePath, attempt)
		if !retryable {
			return result, err
		}

		// Reached retry budget limit
		if attempt >= c.maxRetries {
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("%w: after %d attempts", ErrRateLimitExhausted, attempt+1)
		}

		// Context-aware sleep before next retry
		if err := sleepWithContext(ctx, waitDuration); err != nil {
			return nil, err
		}
	}

	return nil, fmt.Errorf("%w: max retries reached", ErrRateLimitExhausted)
}

func (c *Client) doUploadAttempt(ctx context.Context, endpoint, matchGUID, filePath string, attempt int) (*UploadResult, time.Duration, bool, error) {
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return nil, 0, false, fmt.Errorf("ballchasing: read replay file: %w", err)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	filename := fmt.Sprintf("%s.replay", matchGUID)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, 0, false, fmt.Errorf("ballchasing: create form file part: %w", err)
	}
	if _, err := part.Write(fileData); err != nil {
		return nil, 0, false, fmt.Errorf("ballchasing: write payload: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, 0, false, fmt.Errorf("ballchasing: close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return nil, 0, false, fmt.Errorf("ballchasing: create request: %w", err)
	}

	// Strictly raw token without "Bearer " prefix
	req.Header.Set("Authorization", c.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Transient network transport error: back off and retry
		wait := c.calculateBackoff(attempt)
		return nil, wait, true, err
	}
	// Drain and close immediately per iteration to prevent connection pool leaks
	respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusCreated:
		var raw rawUploadResponse
		_ = json.Unmarshal(respBytes, &raw)
		loc := raw.Location
		if loc == "" {
			loc = resp.Header.Get("Location")
		}
		if loc == "" && raw.ID != "" {
			loc = fmt.Sprintf("https://ballchasing.com/replay/%s", raw.ID)
		}
		return &UploadResult{
			ID:          raw.ID,
			Location:    loc,
			IsDuplicate: false,
		}, 0, false, nil

	case http.StatusConflict:
		// Idempotent duplicate: non-fatal, return result with err == nil
		var raw rawUploadResponse
		_ = json.Unmarshal(respBytes, &raw)
		loc := raw.Location
		if loc == "" && raw.ID != "" {
			loc = fmt.Sprintf("https://ballchasing.com/replay/%s", raw.ID)
		}
		return &UploadResult{
			ID:          raw.ID,
			Location:    loc,
			IsDuplicate: true,
		}, 0, false, nil

	case http.StatusTooManyRequests:
		if attempt >= c.maxRetries {
			return nil, 0, false, fmt.Errorf("%w: HTTP 429 Too Many Requests (attempt %d/%d)", ErrRateLimitExhausted, attempt, c.maxRetries)
		}
		waitDuration := c.resolveRetryAfter(resp.Header.Get("Retry-After"), attempt)
		return nil, waitDuration, true, nil

	case http.StatusUnauthorized:
		// Permanent auth error: halt immediately
		return nil, 0, false, ErrInvalidAPIKey

	case http.StatusBadRequest:
		// Permanent client rejection: halt immediately
		return nil, 0, false, fmt.Errorf("%w: HTTP 400: %s", ErrBadRequest, string(respBytes))

	case http.StatusNotFound:
		return nil, 0, false, fmt.Errorf("%w: HTTP 404: %s", ErrNotFound, string(respBytes))

	default:
		if resp.StatusCode >= 500 {
			// Transient server error: retryable
			wait := c.calculateBackoff(attempt)
			return nil, wait, true, fmt.Errorf("%w: HTTP %d: %s", ErrServerError, resp.StatusCode, string(respBytes))
		}
		return nil, 0, false, fmt.Errorf("ballchasing: unexpected HTTP status %d: %s", resp.StatusCode, string(respBytes))
	}
}

// resolveRetryAfter inspects the Retry-After header and returns the wait duration.
func (c *Client) resolveRetryAfter(headerVal string, attempt int) time.Duration {
	trimmed := strings.TrimSpace(headerVal)
	if trimmed != "" {
		// 1. Try integer seconds
		if secs, err := strconv.Atoi(trimmed); err == nil {
			if secs <= 0 {
				return 0
			}
			wait := time.Duration(secs) * time.Second
			if wait > c.maxBackoff {
				return c.maxBackoff
			}
			return wait
		}

		// 2. Try HTTP-date (RFC 1123, RFC 850, ANSIC)
		if t, err := http.ParseTime(trimmed); err == nil {
			wait := time.Until(t)
			if wait <= 0 {
				return 0
			}
			// If date is within a reasonable range (<= maxBackoff), use it;
			// otherwise fall back to exponential backoff to avoid hanging tests or callers
			if wait <= c.maxBackoff {
				return wait
			}
		}
	}

	// 3. Fallback: exponential backoff with full jitter
	return c.calculateBackoff(attempt)
}

// calculateBackoff computes exponential backoff with full jitter:
// wait = (baseBackoff * 2^attempt) / 2 + random_jitter
func (c *Client) calculateBackoff(attempt int) time.Duration {
	multiplier := 1 << attempt
	if multiplier <= 0 || multiplier > 1024 {
		multiplier = 1024
	}
	backoff := c.baseBackoff * time.Duration(multiplier)
	if backoff > c.maxBackoff || backoff <= 0 {
		backoff = c.maxBackoff
	}

	// Full jitter over the interval [backoff/2, backoff]
	half := backoff / 2
	if half <= 0 {
		return backoff
	}
	jitter := time.Duration(rand.Int63n(int64(half)))
	return half + jitter
}

// sleepWithContext pauses execution for d duration unless ctx is cancelled.
func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
```

---

## 4. Caveats

1. **HTTP Date in `Retry-After` Header**:
   As observed in `TestTier1_F12_429_NonIntegerRetryAfterFallback`, mock or real servers can pass future HTTP dates (e.g. `"Wed, 21 Oct 2026 07:28:00 GMT"`). If `time.Until(t)` is blindly used, the client could block for days or weeks. Capping at `maxBackoff` and falling back to standard exponential backoff ensures daemon liveness while maintaining test suite speed and correctness.
2. **Context Deadline vs Per-Request Timeout**:
   The `UploadReplay` method must use the passed `ctx` across both HTTP requests and sleep intervals. Setting a per-upload context with deadline prevents a single stalled retry loop from blocking the entire 5-minute daemon polling cycle.
3. **Response Body Resource Leaks**:
   In Go, omitting `resp.Body.Close()` within a loop leaks file descriptors and exhausts connection pools. In `doUploadAttempt`, `resp.Body.Close()` is guaranteed to execute immediately after reading the response on every attempt.
4. **Re-reading Request Body on Retries**:
   Go HTTP request bodies are drained streams (`io.Reader`). Attempting to reuse an `*http.Request` on retry will send a 0-byte body. Re-reading or re-streaming the multipart payload per attempt inside the retry loop is mandatory.

---

## 5. Conclusion

1. **HTTP 201 & HTTP 409 Resolution**:
   - Both status codes are terminal and return `UploadResult` with extracted `ID` and `Location`.
   - HTTP 201 sets `IsDuplicate: false`.
   - HTTP 409 sets `IsDuplicate: true` with `err == nil`. Retries on 409 are strictly disabled (attempt count = 1).
2. **HTTP 429 Rate Limiting Engine**:
   - Parses integer seconds, HTTP-date, and applies exponential backoff with jitter on fallback.
   - Enforces configurable retry budget (`maxRetries`, default 3).
   - Sleep intervals strictly honor `ctx.Done()`.
3. **HTTP 401 Fatal Failure**:
   - Terminates immediately on attempt 0 returning sentinel `ErrInvalidAPIKey`.
4. **HTTP 400 / 404 / 5xx Error Propagation**:
   - 400 and 404 fail immediately with descriptive errors.
   - 5xx transient server errors retry using exponential backoff within the retry budget.
5. **Ping Method (`Ping(ctx)`)**:
   - Validates the Ballchasing API key via `GET /` before daemon synchronization loops start, failing fast if the key is invalid.

---

## 6. Verification Method

To independently verify the response handling, duplicate detection, and backoff engine:

### 6.1 Unit Test Suite (`internal/ballchasing/client_test.go`)
1. **HTTP 201 Success**:
   - Spin up `mock_ballchasing.go` server.
   - Call `UploadReplay` with test replay file.
   - Assert `err == nil`, `res.ID != ""`, `res.IsDuplicate == false`.
2. **HTTP 409 Duplicate**:
   - Call `h.BC.SetAlwaysDuplicate(true)` or `SetDuplicateGUID`.
   - Call `UploadReplay`.
   - Assert `err == nil`, `res.IsDuplicate == true`, `res.ID == "<existing_id>"`.
   - Assert `h.BC.GetUploadCount() == 1`.
3. **HTTP 429 Retry & Exhaustion**:
   - Test with `h.BC.SimulateRateLimit(2, 1)`. Assert retry succeeds and `UploadCount == 3`.
   - Test with `h.BC.SimulateRateLimit(5, 1)` and `maxRetries = 2`. Assert error wraps `ErrRateLimitExhausted`.
   - Test with `SetCustomRetryAfterHeader("Wed, 21 Oct 2026 07:28:00 GMT")`. Assert fallback backoff succeeds.
   - Test with `Retry-After: 0`. Assert immediate retry.
4. **HTTP 401 Fatal Failure**:
   - Call `h.BC.SetAlwaysUnauthorized(true)`.
   - Assert `errors.Is(err, ErrInvalidAPIKey) == true`.
   - Assert `h.BC.GetUploadCount() == 1` (zero retries).
5. **Context Cancellation**:
   - Cancel context during backoff or pass an already cancelled context.
   - Assert `errors.Is(err, context.Canceled) == true`.
6. **Ping Verification**:
   - Call `Ping(ctx)` with valid token -> assert `nil`.
   - Call `Ping(ctx)` with invalid token -> assert `errors.Is(err, ErrInvalidAPIKey)`.

### 6.2 Test Command
Run existing and proposed unit & E2E tests:
```powershell
go test -v -run "TestTier1_F10|TestTier1_F11|TestTier1_F12|TestTier1_F13" ./test/e2e
go test -v ./internal/ballchasing/...
```
