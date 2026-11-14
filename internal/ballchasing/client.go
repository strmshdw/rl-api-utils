package ballchasing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
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
	rateLimiter  *RateLimiter
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

// WithRateLimiter configures a shared context-aware RateLimiter for this client.
func WithRateLimiter(rl *RateLimiter) Option {
	return func(c *Client) {
		if rl != nil {
			c.rateLimiter = rl
		}
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
		rateLimiter:  cfg.RateLimiter,
	}

	for _, opt := range opts {
		opt(c)
	}

	if c.rateLimiter == nil {
		c.rateLimiter = NewRateLimiter(DefaultRateInterval)
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

// RateLimiter returns the client's RateLimiter so callers or background syncers
// can share one coordinator instead of doubling request rate.
func (c *Client) RateLimiter() *RateLimiter {
	if c == nil {
		return nil
	}
	return c.rateLimiter
}

func (c *Client) waitRateLimit(ctx context.Context) error {
	if c.rateLimiter == nil {
		return ctx.Err()
	}
	return c.rateLimiter.Wait(ctx)
}

// Ping verifies API key validity by querying GET / (or GET /api/).
// Returns nil on 200 OK, ErrInvalidAPIKey on 401 Unauthorized, or descriptive error.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.pingRaw(ctx)
	return err
}

// PingInfo queries Ballchasing API root and returns account metadata (name, steam_id, etc.).
func (c *Client) PingInfo(ctx context.Context) (*PingResponse, error) {
	bodyBytes, err := c.pingRaw(ctx)
	if err != nil {
		return nil, err
	}
	var p PingResponse
	if err := json.Unmarshal(bodyBytes, &p); err == nil {
		return &p, nil
	}
	return &PingResponse{}, nil
}

func (c *Client) pingRaw(ctx context.Context) ([]byte, error) {
	if err := c.waitRateLimit(ctx); err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf("%s/", strings.TrimRight(c.baseURL, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("ballchasing: create ping request: %w", err)
	}

	req.Header.Set("Authorization", c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ballchasing: ping request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	switch resp.StatusCode {
	case http.StatusOK:
		return bodyBytes, nil
	case http.StatusUnauthorized:
		return nil, ErrInvalidAPIKey
	default:
		return nil, fmt.Errorf("ballchasing: ping returned HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}
}

func (c *Client) resolveRetryAfter(headerVal string, attempt int) time.Duration {
	trimmed := strings.TrimSpace(headerVal)
	if trimmed != "" {
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
		if t, err := http.ParseTime(trimmed); err == nil {
			wait := time.Until(t)
			if wait <= 0 {
				return 0
			}
			if wait <= c.maxBackoff {
				return wait
			}
		}
	}
	return c.calculateBackoff(attempt)
}

func (c *Client) calculateBackoff(attempt int) time.Duration {
	multiplier := 1 << attempt
	if multiplier <= 0 || multiplier > 1024 {
		multiplier = 1024
	}
	backoff := c.baseBackoff * time.Duration(multiplier)
	if backoff > c.maxBackoff || backoff <= 0 {
		backoff = c.maxBackoff
	}
	half := backoff / 2
	if half <= 0 {
		return backoff
	}
	return half + time.Duration(rand.Int63n(int64(half)))
}

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
