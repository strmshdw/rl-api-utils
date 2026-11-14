package psynet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dank/rl-api-utils/internal/replay"
)

// Sentinel errors for replay downloading.
var (
	// ErrEmptyMatchGUID is returned when an empty or whitespace match GUID is provided.
	ErrEmptyMatchGUID = errors.New("psynet: match GUID cannot be empty")

	// ErrInvalidMatchGUID is returned when a match GUID contains path traversal or illegal characters.
	ErrInvalidMatchGUID = errors.New("psynet: invalid match GUID (path traversal characters forbidden)")

	// ErrEmptyReplayURL is returned when an empty or whitespace replay URL is provided.
	ErrEmptyReplayURL = errors.New("psynet: empty replay URL")

	// ErrInvalidReplayURL is returned when the replay URL scheme is not http or https.
	ErrInvalidReplayURL = errors.New("psynet: invalid replay URL scheme (must be http or https)")

	// ErrReplayTooSmall is returned when downloaded bytes are less than the minimum size threshold (1024 bytes).
	ErrReplayTooSmall = errors.New("psynet: downloaded replay payload too small")
)

// HTTPStatusError represents an unexpected HTTP response code from the CDN server.
type HTTPStatusError struct {
	StatusCode int
	MatchGUID  string
	URL        string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("psynet: replay download for match %s returned HTTP %d from %s", e.MatchGUID, e.StatusCode, e.URL)
}

// ReplayDownloader defines the contract for downloading Rocket League replay binary files.
// Matches the PROJECT.md interface contract for internal/psynet -> internal/syncer.
type ReplayDownloader interface {
	DownloadReplay(ctx context.Context, matchGUID, replayURL, destDir string) (localPath string, err error)
}

const (
	// DefaultDownloadTimeout is the default per-request HTTP timeout.
	DefaultDownloadTimeout = 30 * time.Second

	// DefaultMinReplaySize is the minimum valid size in bytes (1KB).
	// Valid Rocket League replays typically range from 500KB to 5MB.
	DefaultMinReplaySize = 1024

	// DefaultUserAgent is sent on outgoing HTTP GET requests to PsyNet CDN.
	DefaultUserAgent = "rl-sync/1.0 (Rocket League Replay Syncer)"

	// DefaultRenameRetries is the number of atomic rename attempts on Windows.
	DefaultRenameRetries = 5

	// TempFilePrefix is the prefix used for in-flight download temporary files.
	TempFilePrefix = ".tmp-"

	// ReplayFileExtension is the canonical extension for Rocket League replays.
	ReplayFileExtension = ".replay"

	// DefaultMaxRetries is the default retry count for transient CDN errors (0 preserves single-shot behavior).
	DefaultMaxRetries = 0

	// DefaultRetryBackoff is the base delay between download retry attempts.
	DefaultRetryBackoff = 50 * time.Millisecond

	// DefaultMaxDownloadBytes bounds the maximum downloaded replay size to protect disk/memory (64 MiB).
	DefaultMaxDownloadBytes = 64 * 1024 * 1024
)

// DownloaderConfig holds configurable options for HTTPDownloader.
type DownloaderConfig struct {
	Client         *http.Client
	Timeout        time.Duration
	UserAgent      string
	MinSize        int64
	MaxSize        int64
	MaxRetries     int
	RetryBackoff   time.Duration
	RenameRetries  int
	BufferSize     int
	ValidateReplay bool
}

// DownloaderOption configures an HTTPDownloader instance.
type DownloaderOption func(*DownloaderConfig)

// WithHTTPClient provides a custom *http.Client.
func WithHTTPClient(client *http.Client) DownloaderOption {
	return func(c *DownloaderConfig) {
		if client != nil {
			c.Client = client
		}
	}
}

// WithTimeout sets the overall request timeout.
func WithTimeout(timeout time.Duration) DownloaderOption {
	return func(c *DownloaderConfig) {
		if timeout > 0 {
			c.Timeout = timeout
		}
	}
}

// WithUserAgent sets a custom User-Agent header.
func WithUserAgent(ua string) DownloaderOption {
	return func(c *DownloaderConfig) {
		c.UserAgent = ua
	}
}

// WithMinSize sets the minimum payload size in bytes.
func WithMinSize(minSize int64) DownloaderOption {
	return func(c *DownloaderConfig) {
		if minSize > 0 {
			c.MinSize = minSize
		}
	}
}

// WithMaxRetries sets the maximum number of retry attempts for transient network/429/5xx errors.
// HTTP 404, 410, and other non-retryable 4xx status codes always fail immediately without retrying.
func WithMaxRetries(retries int) DownloaderOption {
	return func(c *DownloaderConfig) {
		if retries >= 0 {
			c.MaxRetries = retries
		}
	}
}

// WithRenameRetries sets the max rename attempts for Windows transient lock retry.
func WithRenameRetries(retries int) DownloaderOption {
	return func(c *DownloaderConfig) {
		if retries > 0 {
			c.RenameRetries = retries
		}
	}
}

// WithBufferSize sets the streaming copy buffer size in bytes (default 64KB).
func WithBufferSize(size int) DownloaderOption {
	return func(c *DownloaderConfig) {
		if size > 0 {
			c.BufferSize = size
		}
	}
}

// WithValidateReplay enables strict Rocket League UE3 header/CRC parsing via replay.ParseReplay
// before committing the downloaded file to its final path.
func WithValidateReplay(validate bool) DownloaderOption {
	return func(c *DownloaderConfig) {
		c.ValidateReplay = validate
	}
}

// HTTPDownloader implements ReplayDownloader with atomic disk streaming,
// minimum size validation, optional replay header validation, Windows retry loops,
// and robust deferred cleanup.
type HTTPDownloader struct {
	client         *http.Client
	userAgent      string
	minSize        int64
	maxSize        int64
	maxRetries     int
	retryBackoff   time.Duration
	renameRetries  int
	bufferSize     int
	validateReplay bool
}

// Compile-time assertion that HTTPDownloader satisfies ReplayDownloader.
var _ ReplayDownloader = (*HTTPDownloader)(nil)

// NewDownloader creates a new HTTPDownloader with the provided options.
func NewDownloader(opts ...DownloaderOption) *HTTPDownloader {
	cfg := DownloaderConfig{
		Timeout:       DefaultDownloadTimeout,
		UserAgent:     DefaultUserAgent,
		MinSize:       DefaultMinReplaySize,
		MaxSize:       DefaultMaxDownloadBytes,
		MaxRetries:    DefaultMaxRetries,
		RetryBackoff:  DefaultRetryBackoff,
		RenameRetries: DefaultRenameRetries,
		BufferSize:    64 * 1024, // 64 KB streaming buffer
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	client := cfg.Client
	if client == nil {
		transport := &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConns:          10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
		client = &http.Client{
			Transport: transport,
			Timeout:   cfg.Timeout,
		}
	}

	maxSize := cfg.MaxSize
	if maxSize <= 0 {
		maxSize = DefaultMaxDownloadBytes
	}
	retryBackoff := cfg.RetryBackoff
	if retryBackoff <= 0 {
		retryBackoff = DefaultRetryBackoff
	}

	return &HTTPDownloader{
		client:         client,
		userAgent:      cfg.UserAgent,
		minSize:        cfg.MinSize,
		maxSize:        maxSize,
		maxRetries:     cfg.MaxRetries,
		retryBackoff:   retryBackoff,
		renameRetries:  cfg.RenameRetries,
		bufferSize:     cfg.BufferSize,
		validateReplay: cfg.ValidateReplay,
	}
}

// NewHTTPReplayDownloader provides a compatibility constructor matching E2E harness conventions.
func NewHTTPReplayDownloader(timeout time.Duration) *HTTPDownloader {
	return NewDownloader(WithTimeout(timeout))
}

// DownloadReplay downloads a replay binary from replayURL, streams it to a temporary file
// in destDir, validates minimum size (and optionally replay.ParseReplay), flushes to disk,
// and atomically renames the file to <destDir>/<matchGUID>.replay with a Windows retry loop.
func (d *HTTPDownloader) DownloadReplay(ctx context.Context, matchGUID, replayURL, destDir string) (string, error) {
	trimmedGUID, trimmedURL, cleanDestDir, err := validateDownloadArgs(ctx, matchGUID, replayURL, destDir)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(cleanDestDir, 0755); err != nil {
		return "", fmt.Errorf("psynet: failed to create destination directory %s: %w", cleanDestDir, err)
	}

	totalAttempts := d.maxRetries + 1
	if totalAttempts < 1 {
		totalAttempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < totalAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("psynet: context cancelled before attempt: %w", err)
		}

		finalPath, retryable, err := d.downloadAttempt(ctx, trimmedGUID, trimmedURL, cleanDestDir)
		if err == nil {
			return finalPath, nil
		}
		lastErr = err

		if !retryable || attempt == totalAttempts-1 || ctx.Err() != nil {
			return "", lastErr
		}

		backoff := d.retryBackoff * time.Duration(1<<uint(attempt))
		if sleepErr := sleepWithContext(ctx, backoff); sleepErr != nil {
			return "", fmt.Errorf("psynet: download retry cancelled: %w", sleepErr)
		}
	}

	return "", lastErr
}

func validateDownloadArgs(ctx context.Context, matchGUID, replayURL, destDir string) (string, string, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", "", "", fmt.Errorf("psynet: context cancelled prior to download: %w", err)
	}

	trimmedGUID := strings.TrimSpace(matchGUID)
	if trimmedGUID == "" {
		return "", "", "", ErrEmptyMatchGUID
	}
	if strings.ContainsAny(trimmedGUID, `/\:?*"<>|`) || strings.Contains(trimmedGUID, "..") {
		return "", "", "", fmt.Errorf("%w: %q", ErrInvalidMatchGUID, matchGUID)
	}

	trimmedURL := strings.TrimSpace(replayURL)
	if trimmedURL == "" {
		return "", "", "", ErrEmptyReplayURL
	}
	parsedURL, err := url.Parse(trimmedURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		return "", "", "", fmt.Errorf("%w: %q", ErrInvalidReplayURL, replayURL)
	}

	if destDir == "" {
		destDir = "."
	}
	return trimmedGUID, trimmedURL, destDir, nil
}

func (d *HTTPDownloader) downloadAttempt(ctx context.Context, trimmedGUID, trimmedURL, destDir string) (string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, trimmedURL, nil)
	if err != nil {
		return "", false, fmt.Errorf("psynet: failed to create http request: %w", err)
	}
	if d.userAgent != "" {
		req.Header.Set("User-Agent", d.userAgent)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return "", ctx.Err() == nil, fmt.Errorf("psynet: http get failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		statusErr := &HTTPStatusError{
			StatusCode: resp.StatusCode,
			MatchGUID:  trimmedGUID,
			URL:        trimmedURL,
		}
		return "", isRetryableDownloadStatus(resp.StatusCode), statusErr
	}

	tmpPattern := fmt.Sprintf("%s%s-*.replay", TempFilePrefix, trimmedGUID)
	tmpFile, err := os.CreateTemp(destDir, tmpPattern)
	if err != nil {
		return "", false, fmt.Errorf("psynet: failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	success := false
	defer func() {
		_ = tmpFile.Close()
		if !success {
			_ = os.Remove(tmpPath)
		}
	}()

	buf := make([]byte, d.bufferSize)
	limitedBody := io.LimitReader(resp.Body, d.maxSize+1)
	written, err := io.CopyBuffer(tmpFile, limitedBody, buf)
	if err != nil {
		return "", ctx.Err() == nil, fmt.Errorf("psynet: streaming replay payload failed: %w", err)
	}
	if written > d.maxSize {
		return "", false, fmt.Errorf("psynet: replay payload exceeds max size limit (%d bytes)", d.maxSize)
	}

	if err := ctx.Err(); err != nil {
		return "", false, fmt.Errorf("psynet: context cancelled during download: %w", err)
	}

	if written < d.minSize {
		return "", false, fmt.Errorf("%w: downloaded %d bytes, minimum required is %d", ErrReplayTooSmall, written, d.minSize)
	}

	if d.validateReplay {
		if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
			return "", false, fmt.Errorf("psynet: failed to rewind temp replay file: %w", err)
		}
		if _, err := replay.ParseReplay(tmpFile); err != nil {
			return "", false, fmt.Errorf("psynet: replay validation failed: %w", err)
		}
	}

	if err := tmpFile.Sync(); err != nil {
		return "", false, fmt.Errorf("psynet: failed to sync temp file to disk: %w", err)
	}

	// Close descriptor before rename to avoid Windows ERROR_SHARING_VIOLATION.
	if err := tmpFile.Close(); err != nil {
		return "", false, fmt.Errorf("psynet: failed to close temp file: %w", err)
	}

	finalPath := filepath.Join(destDir, fmt.Sprintf("%s%s", trimmedGUID, ReplayFileExtension))
	if err := atomicRename(ctx, tmpPath, finalPath, d.renameRetries); err != nil {
		return "", false, fmt.Errorf("psynet: atomic rename failed: %w", err)
	}

	success = true
	return finalPath, false, nil
}

func isRetryableDownloadStatus(statusCode int) bool {
	if statusCode == http.StatusNotFound || statusCode == http.StatusGone {
		return false
	}
	if statusCode == http.StatusTooManyRequests {
		return true
	}
	return statusCode >= 500 && statusCode <= 599
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
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

// atomicRename attempts atomic os.Rename with retries and context-aware backoff
// to withstand transient Windows file locks caused by AV scanners or indexers.
func atomicRename(ctx context.Context, oldPath, newPath string, maxRetries int) error {
	if maxRetries <= 0 {
		maxRetries = 1
	}
	var err error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		err = os.Rename(oldPath, newPath)
		if err == nil {
			return nil
		}
		if attempt < maxRetries-1 {
			if sleepErr := sleepWithContext(ctx, time.Duration((attempt+1)*5)*time.Millisecond); sleepErr != nil {
				return sleepErr
			}
		}
	}
	return fmt.Errorf("failed to rename %s to %s after %d attempts: %w", oldPath, newPath, maxRetries, err)
}

// CleanupStaleTempFiles scans destDir for leftover temporary files (.tmp-*) from crashed runs.
func CleanupStaleTempFiles(destDir string) (int, error) {
	entries, err := os.ReadDir(destDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("psynet: failed to read directory %s: %w", destDir, err)
	}

	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), TempFilePrefix) && strings.HasSuffix(entry.Name(), ReplayFileExtension) {
			target := filepath.Join(destDir, entry.Name())
			if err := os.Remove(target); err == nil {
				removed++
			}
		}
	}
	return removed, nil
}
