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
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultBaseURL is the production Ballchasing v2 API base URL.
	DefaultBaseURL = "https://ballchasing.com/api"

	// DefaultVisibility is public visibility when not specified.
	DefaultVisibility = "public"

	// DefaultMaxRetries is the maximum number of retry attempts for 429 and 5xx.
	DefaultMaxRetries = 3

	// DefaultTimeout is the HTTP client timeout for uploads.
	DefaultTimeout = 60 * time.Second

	// DefaultBaseBackoff is the baseline backoff interval for rate limiting.
	DefaultBaseBackoff = 1 * time.Second

	// DefaultMaxBackoff is the maximum backoff duration ceiling.
	DefaultMaxBackoff = 30 * time.Second
)

// Client implements ReplayUploader against the Ballchasing REST API.
type Client struct {
	httpClient   *http.Client
	baseURL      string
	apiKey       string
	visibility   string
	group        string
	maxRetries   int
	baseBackoff  time.Duration
	maxBackoff   time.Duration
	streamUpload bool
}

// Compile-time assertion that Client satisfies ReplayUploader.
var _ ReplayUploader = (*Client)(nil)

// Option allows configuring optional Client settings.
type Option func(*Client)

// WithHTTPClient overrides the default HTTP client (e.g. for testing or custom proxies).
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client != nil {
			c.httpClient = client
		}
	}
}

// WithBaseURL overrides the Ballchasing API base URL.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
		if trimmed != "" {
			c.baseURL = trimmed
		}
	}
}

// WithVisibility sets the default upload visibility ("public", "unlisted", "private").
func WithVisibility(vis string) Option {
	return func(c *Client) {
		trimmed := strings.ToLower(strings.TrimSpace(vis))
		if trimmed != "" {
			c.visibility = trimmed
		}
	}
}

// WithGroup sets the default group ID for uploaded replays.
func WithGroup(group string) Option {
	return func(c *Client) {
		c.group = strings.TrimSpace(group)
	}
}

// WithMaxRetries overrides the maximum retry attempts for rate limits and server errors.
func WithMaxRetries(retries int) Option {
	return func(c *Client) {
		if retries >= 0 {
			c.maxRetries = retries
		}
	}
}

// WithTimeout overrides the HTTP client request timeout.
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		if timeout > 0 {
			c.httpClient.Timeout = timeout
		}
	}
}

// WithBaseBackoff overrides the base exponential backoff duration.
func WithBaseBackoff(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.baseBackoff = d
		}
	}
}

// WithMaxBackoff overrides the maximum backoff duration ceiling.
func WithMaxBackoff(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.maxBackoff = d
		}
	}
}

// WithStreaming toggles streaming upload mode (io.MultiReader) vs buffered upload (bytes.Buffer).
func WithStreaming(enabled bool) Option {
	return func(c *Client) {
		c.streamUpload = enabled
	}
}

// NewClient initializes a Ballchasing API client with validation and defaults.
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
		return nil, fmt.Errorf("%w: %q (must be 'public', 'unlisted', or 'private')", ErrInvalidVisibility, visibility)
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
		httpClient:   &http.Client{Timeout: timeout},
		baseURL:      baseURL,
		apiKey:       apiKey,
		visibility:   visibility,
		group:        strings.TrimSpace(cfg.Group),
		maxRetries:   maxRetries,
		baseBackoff:  baseBackoff,
		maxBackoff:   maxBackoff,
		streamUpload: cfg.StreamUpload,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c, nil
}

// New initializes a Ballchasing API client using an API key and optional functional options.
func New(apiKey string, opts ...Option) (*Client, error) {
	return NewClient(ClientConfig{APIKey: apiKey}, opts...)
}

// NewHTTPBallchasingUploader provides a drop-in compatibility constructor
// matching E2E test harness and legacy signatures.
func NewHTTPBallchasingUploader(baseURL, apiKey, visibility, group string, maxRetries int) *Client {
	c, _ := NewClient(ClientConfig{
		BaseURL:    baseURL,
		APIKey:     apiKey,
		Visibility: visibility,
		Group:      group,
		MaxRetries: maxRetries,
	})
	return c
}

// Ping verifies API key validity by querying GET / (or GET /api/).
// Returns nil on 200 OK, ErrInvalidAPIKey on 401 Unauthorized, or descriptive error.
func (c *Client) Ping(ctx context.Context) error {
	endpoint := fmt.Sprintf("%s/", strings.TrimRight(c.baseURL, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("ballchasing: create ping request: %w", err)
	}

	// Strictly raw token without "Bearer " prefix
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

// rawUploadResponse deserializes both 201 Created and 409 Conflict responses.
type rawUploadResponse struct {
	Error    string `json:"error"`
	ID       string `json:"id"`
	Location string `json:"location"`
}

// UploadReplay uploads a Rocket League .replay file to Ballchasing.com.
// Satisfies ReplayUploader contract. Handles 201, 409 (duplicate), 429 (backoff), 401, 400.
func (c *Client) UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error) {
	trimmedGUID := strings.TrimSpace(matchGUID)
	if trimmedGUID == "" {
		return nil, ErrEmptyMatchGUID
	}

	trimmedPath := strings.TrimSpace(filePath)
	if trimmedPath == "" {
		return nil, ErrEmptyFilePath
	}

	// Validate file existence and accessibility
	fileInfo, err := os.Stat(trimmedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrFileNotFound, trimmedPath)
		}
		return nil, fmt.Errorf("ballchasing: stat replay file: %w", err)
	}
	if fileInfo.IsDir() {
		return nil, fmt.Errorf("%w: path is a directory: %s", ErrBadRequest, trimmedPath)
	}
	if fileInfo.Size() == 0 {
		return nil, fmt.Errorf("%w: %w", ErrBadRequest, ErrEmptyFile)
	}

	// Construct destination URL with query parameters
	endpoint := c.buildUploadURL()

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		// Verify context before starting attempt
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		result, waitDuration, retryable, err := c.doUploadAttempt(ctx, endpoint, trimmedGUID, trimmedPath, attempt)
		if !retryable {
			return result, err
		}

		// If maxRetries reached, return terminal rate limit error
		if attempt >= c.maxRetries {
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("%w: after %d attempts", ErrRateLimitExhausted, attempt+1)
		}

		// Context-aware sleep before next retry.
		// Note: The file handle from doUploadAttempt is ALREADY closed, so no file handles
		// are locked or leaked while sleeping.
		if err := sleepWithContext(ctx, waitDuration); err != nil {
			return nil, err
		}
	}

	return nil, fmt.Errorf("%w: max retries reached", ErrRateLimitExhausted)
}

// buildUploadURL constructs the target upload URL including query parameters.
func (c *Client) buildUploadURL() string {
	baseURL := strings.TrimRight(c.baseURL, "/")
	endpoint := fmt.Sprintf("%s/v2/upload", baseURL)

	params := url.Values{}
	if c.visibility != "" {
		params.Set("visibility", c.visibility)
	}
	if c.group != "" {
		params.Set("group", c.group)
	}

	if q := params.Encode(); q != "" {
		endpoint += "?" + q
	}
	return endpoint
}

// doUploadAttempt performs a single HTTP upload attempt.
// File handles are opened inside and guaranteed closed upon function exit.
func (c *Client) doUploadAttempt(ctx context.Context, endpoint, matchGUID, filePath string, attempt int) (*UploadResult, time.Duration, bool, error) {
	req, closer, err := c.createMultipartRequest(ctx, endpoint, matchGUID, filePath)
	if err != nil {
		return nil, 0, false, err
	}
	if closer != nil {
		defer closer.Close()
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, false, ctx.Err()
		}
		if attempt < c.maxRetries {
			wait := c.calculateBackoff(attempt)
			return nil, wait, true, err
		}
		return nil, 0, false, fmt.Errorf("ballchasing: upload request failed: %w", err)
	}
	defer resp.Body.Close()

	// Limit response reading to 1MB to avoid memory exhaustion from malformed server responses
	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, 0, false, fmt.Errorf("ballchasing: reading response body: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusCreated:
		var res rawUploadResponse
		if err := json.Unmarshal(respBytes, &res); err != nil {
			return nil, 0, false, fmt.Errorf("ballchasing: decoding 201 response: %w", err)
		}
		loc := res.Location
		if loc == "" {
			loc = resp.Header.Get("Location")
		}
		if loc == "" && res.ID != "" {
			loc = fmt.Sprintf("https://ballchasing.com/replay/%s", res.ID)
		}
		return &UploadResult{
			ID:          res.ID,
			Location:    loc,
			IsDuplicate: false,
		}, 0, false, nil

	case http.StatusConflict:
		// 409 Conflict: Deduplication event. Returns existing ID and Location without error!
		var res rawUploadResponse
		if err := json.Unmarshal(respBytes, &res); err != nil {
			return nil, 0, false, fmt.Errorf("ballchasing: decoding 409 response: %w", err)
		}
		loc := res.Location
		if loc == "" {
			loc = resp.Header.Get("Location")
		}
		if loc == "" && res.ID != "" {
			loc = fmt.Sprintf("https://ballchasing.com/replay/%s", res.ID)
		}
		return &UploadResult{
			ID:          res.ID,
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
		// 401 Unauthorized: Immediate fatal rejection without retries
		return nil, 0, false, ErrInvalidAPIKey

	case http.StatusBadRequest:
		// 400 Bad Request: Non-retryable client error
		errMsg := extractErrorMessage(respBytes)
		if errMsg == "" {
			errMsg = "bad request"
		}
		return nil, 0, false, fmt.Errorf("%w: HTTP 400 Bad Request: %s", ErrBadRequest, errMsg)

	case http.StatusNotFound:
		return nil, 0, false, fmt.Errorf("%w: %s", ErrNotFound, string(respBytes))

	default:
		if resp.StatusCode >= 500 {
			if attempt < c.maxRetries {
				wait := c.calculateBackoff(attempt)
				return nil, wait, true, fmt.Errorf("%w: HTTP %d: %s", ErrServerError, resp.StatusCode, string(respBytes))
			}
			return nil, 0, false, fmt.Errorf("%w: HTTP %d (retries exhausted): %s", ErrServerError, resp.StatusCode, string(respBytes))
		}
		return nil, 0, false, fmt.Errorf("ballchasing: unexpected HTTP status %d: %s", resp.StatusCode, string(respBytes))
	}
}

// createMultipartRequest constructs the HTTP request with multipart/form-data payload.
// Supports both zero-RAM streaming (io.MultiReader) and buffered writer (bytes.Buffer).
func (c *Client) createMultipartRequest(ctx context.Context, endpoint, matchGUID, filePath string) (*http.Request, io.Closer, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("ballchasing: open replay file: %w", err)
	}

	fileInfo, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, fmt.Errorf("ballchasing: stat replay file: %w", err)
	}

	// Sanitize filename to avoid path traversal
	baseGUID := filepath.Base(matchGUID)
	filename := fmt.Sprintf("%s.replay", strings.TrimSuffix(baseGUID, ".replay"))

	if c.streamUpload {
		// ZERO-RAM STREAMING: io.MultiReader with exact Content-Length
		var headerBuf bytes.Buffer
		mw := multipart.NewWriter(&headerBuf)
		_, err = mw.CreateFormFile("file", filename)
		if err != nil {
			_ = file.Close()
			return nil, nil, fmt.Errorf("ballchasing: create form file: %w", err)
		}
		boundary := mw.Boundary()
		footer := fmt.Sprintf("\r\n--%s--\r\n", boundary)
		contentLength := int64(headerBuf.Len()) + fileInfo.Size() + int64(len(footer))

		safeCloser := &onceCloser{closer: file}
		combinedReader := io.MultiReader(&headerBuf, file, strings.NewReader(footer))
		bodyWrapper := &readCloserWrapper{
			Reader: combinedReader,
			closer: safeCloser,
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bodyWrapper)
		if err != nil {
			_ = safeCloser.Close()
			return nil, nil, fmt.Errorf("ballchasing: create http request: %w", err)
		}

		req.ContentLength = contentLength
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("Authorization", c.apiKey)
		return req, safeCloser, nil
	}

	// BUFFERED WRITER: bytes.Buffer
	// File handle is closed immediately after reading into buffer!
	defer file.Close()
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return nil, nil, fmt.Errorf("ballchasing: create form file: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, nil, fmt.Errorf("ballchasing: read replay file: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, nil, fmt.Errorf("ballchasing: close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return nil, nil, fmt.Errorf("ballchasing: create http request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", c.apiKey)
	return req, nil, nil
}

// onceCloser guarantees Close is executed exactly once.
type onceCloser struct {
	closer io.Closer
	once   sync.Once
	err    error
}

func (o *onceCloser) Close() error {
	o.once.Do(func() {
		if o.closer != nil {
			o.err = o.closer.Close()
		}
	})
	return o.err
}

// readCloserWrapper binds an io.Reader and an io.Closer together into an io.ReadCloser.
type readCloserWrapper struct {
	io.Reader
	closer io.Closer
}

func (w *readCloserWrapper) Close() error {
	if w.closer != nil {
		return w.closer.Close()
	}
	return nil
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

// sleepWithContext pauses execution for d duration while listening for context cancellation.
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

// extractErrorMessage attempts to parse an "error" string from JSON response bytes.
func extractErrorMessage(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var data map[string]any
	if err := json.Unmarshal(body, &data); err == nil {
		if msg, ok := data["error"].(string); ok && msg != "" {
			return msg
		}
	}
	return string(body)
}
