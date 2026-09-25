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
)

// DownloaderConfig holds configurable options for HTTPDownloader.
type DownloaderConfig struct {
	Client        *http.Client
	Timeout       time.Duration
	UserAgent     string
	MinSize       int64
	RenameRetries int
	BufferSize    int
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

// HTTPDownloader implements ReplayDownloader with atomic disk streaming,
// minimum size validation, Windows retry loops, and robust deferred cleanup.
type HTTPDownloader struct {
	client        *http.Client
	userAgent     string
	minSize       int64
	renameRetries int
	bufferSize    int
}

// Compile-time assertion that HTTPDownloader satisfies ReplayDownloader.
var _ ReplayDownloader = (*HTTPDownloader)(nil)

// NewDownloader creates a new HTTPDownloader with the provided options.
func NewDownloader(opts ...DownloaderOption) *HTTPDownloader {
	cfg := DownloaderConfig{
		Timeout:       DefaultDownloadTimeout,
		UserAgent:     DefaultUserAgent,
		MinSize:       DefaultMinReplaySize,
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

	return &HTTPDownloader{
		client:        client,
		userAgent:     cfg.UserAgent,
		minSize:       cfg.MinSize,
		renameRetries: cfg.RenameRetries,
		bufferSize:    cfg.BufferSize,
	}
}

// NewHTTPReplayDownloader provides a compatibility constructor matching E2E harness conventions.
func NewHTTPReplayDownloader(timeout time.Duration) *HTTPDownloader {
	return NewDownloader(WithTimeout(timeout))
}

// DownloadReplay downloads a replay binary from replayURL, streams it to a temporary file
// in destDir, validates non-empty and minimum size (>1KB), flushes to disk, and atomically
// renames the file to <destDir>/<matchGUID>.replay with a Windows retry loop.
func (d *HTTPDownloader) DownloadReplay(ctx context.Context, matchGUID, replayURL, destDir string) (string, error) {
	// 1. Validate Context
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("psynet: context cancelled prior to download: %w", err)
	}

	// 2. Validate matchGUID
	trimmedGUID := strings.TrimSpace(matchGUID)
	if trimmedGUID == "" {
		return "", ErrEmptyMatchGUID
	}
	// Sanitize against path traversal attacks (e.g. "../", "..\", path separators)
	if strings.ContainsAny(trimmedGUID, `/\:?*"<>|`) || strings.Contains(trimmedGUID, "..") {
		return "", fmt.Errorf("%w: %q", ErrInvalidMatchGUID, matchGUID)
	}

	// 3. Validate replayURL
	trimmedURL := strings.TrimSpace(replayURL)
	if trimmedURL == "" {
		return "", ErrEmptyReplayURL
	}
	parsedURL, err := url.Parse(trimmedURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		return "", fmt.Errorf("%w: %q", ErrInvalidReplayURL, replayURL)
	}

	// 4. Ensure destination directory exists
	if destDir == "" {
		destDir = "."
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("psynet: failed to create destination directory %s: %w", destDir, err)
	}

	// 5. Construct HTTP Request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, trimmedURL, nil)
	if err != nil {
		return "", fmt.Errorf("psynet: failed to create http request: %w", err)
	}
	if d.userAgent != "" {
		req.Header.Set("User-Agent", d.userAgent)
	}

	// 6. Execute HTTP GET
	resp, err := d.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("psynet: http get failed: %w", err)
	}
	defer resp.Body.Close()

	// 7. Validate HTTP Status Code
	if resp.StatusCode != http.StatusOK {
		return "", &HTTPStatusError{
			StatusCode: resp.StatusCode,
			MatchGUID:  trimmedGUID,
			URL:        trimmedURL,
		}
	}

	// 8. Create Unique Temporary File in destDir
	// Putting temp file in the SAME directory ensures it resides on the same filesystem volume,
	// which is required for atomic os.Rename (avoids cross-device link errors EXDEV).
	tmpPattern := fmt.Sprintf("%s%s-*.replay", TempFilePrefix, trimmedGUID)
	tmpFile, err := os.CreateTemp(destDir, tmpPattern)
	if err != nil {
		return "", fmt.Errorf("psynet: failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// 9. Deferred Cleanup Handler
	success := false
	defer func() {
		_ = tmpFile.Close()
		if !success {
			_ = os.Remove(tmpPath)
		}
	}()

	// 10. Stream Payload to Disk Buffer
	buf := make([]byte, d.bufferSize)
	written, err := io.CopyBuffer(tmpFile, resp.Body, buf)
	if err != nil {
		return "", fmt.Errorf("psynet: streaming replay payload failed: %w", err)
	}

	// 11. Validate Context Cancellation During Stream
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("psynet: context cancelled during download: %w", err)
	}

	// 12. Validate Minimum Size & Non-Empty Content (>1KB)
	if written < d.minSize {
		return "", fmt.Errorf("%w: downloaded %d bytes, minimum required is %d", ErrReplayTooSmall, written, d.minSize)
	}

	// 13. Flush and Sync to Disk
	if err := tmpFile.Sync(); err != nil {
		return "", fmt.Errorf("psynet: failed to sync temp file to disk: %w", err)
	}

	// 14. Close File Descriptor Before Rename (CRITICAL ON WINDOWS!)
	// On Windows, renaming an open file descriptor triggers ERROR_SHARING_VIOLATION.
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("psynet: failed to close temp file: %w", err)
	}

	// 15. Atomic Rename to Final Destination with Windows Retry Loop
	finalPath := filepath.Join(destDir, fmt.Sprintf("%s%s", trimmedGUID, ReplayFileExtension))
	if err := atomicRename(tmpPath, finalPath, d.renameRetries); err != nil {
		return "", fmt.Errorf("psynet: atomic rename failed: %w", err)
	}

	success = true
	return finalPath, nil
}

// atomicRename attempts atomic os.Rename with retries and backoff to withstand
// transient Windows file locks caused by antivirus scanners, search indexers, or filter drivers.
func atomicRename(oldPath, newPath string, maxRetries int) error {
	var err error
	for attempt := 0; attempt < maxRetries; attempt++ {
		err = os.Rename(oldPath, newPath)
		if err == nil {
			return nil
		}
		if attempt < maxRetries-1 {
			// Linear backoff: 5ms, 10ms, 15ms, 20ms...
			time.Sleep(time.Duration((attempt+1)*5) * time.Millisecond)
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
