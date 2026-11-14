package ballchasing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

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

	endpoint := c.buildUploadURL()

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if err := c.waitRateLimit(ctx); err != nil {
			return nil, err
		}

		result, waitDuration, retryable, err := c.doUploadAttempt(ctx, endpoint, trimmedGUID, trimmedPath, attempt)
		if !retryable {
			return result, err
		}

		if attempt >= c.maxRetries {
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("%w: after %d attempts", ErrRateLimitExhausted, attempt+1)
		}

		// File handle is closed inside doUploadAttempt before sleeping.
		if err := sleepWithContext(ctx, waitDuration); err != nil {
			return nil, err
		}
	}

	return nil, fmt.Errorf("%w: max retries reached", ErrRateLimitExhausted)
}

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
			return nil, c.calculateBackoff(attempt), true, err
		}
		return nil, 0, false, fmt.Errorf("ballchasing: upload request failed: %w", err)
	}
	defer resp.Body.Close()

	// Limit response reading to 1MB to guard against malformed responses.
	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, 0, false, fmt.Errorf("ballchasing: reading response body: %w", err)
	}

	return c.handleUploadResponse(resp, respBytes, attempt)
}

func (c *Client) handleUploadResponse(resp *http.Response, respBytes []byte, attempt int) (*UploadResult, time.Duration, bool, error) {
	switch resp.StatusCode {
	case http.StatusCreated:
		return parseUploadSuccess(resp.Header.Get("Location"), respBytes, false, "decoding 201 response")
	case http.StatusConflict:
		return parseUploadSuccess(resp.Header.Get("Location"), respBytes, true, "decoding 409 response")
	case http.StatusTooManyRequests:
		if attempt >= c.maxRetries {
			return nil, 0, false, fmt.Errorf("%w: HTTP 429 Too Many Requests (attempt %d/%d)", ErrRateLimitExhausted, attempt, c.maxRetries)
		}
		return nil, c.resolveRetryAfter(resp.Header.Get("Retry-After"), attempt), true, nil
	case http.StatusUnauthorized:
		return nil, 0, false, ErrInvalidAPIKey
	case http.StatusBadRequest:
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
				return nil, c.calculateBackoff(attempt), true, fmt.Errorf("%w: HTTP %d: %s", ErrServerError, resp.StatusCode, string(respBytes))
			}
			return nil, 0, false, fmt.Errorf("%w: HTTP %d (retries exhausted): %s", ErrServerError, resp.StatusCode, string(respBytes))
		}
		return nil, 0, false, fmt.Errorf("ballchasing: unexpected HTTP status %d: %s", resp.StatusCode, string(respBytes))
	}
}

func parseUploadSuccess(headerLoc string, respBytes []byte, isDup bool, errCtx string) (*UploadResult, time.Duration, bool, error) {
	var res rawUploadResponse
	if err := json.Unmarshal(respBytes, &res); err != nil {
		return nil, 0, false, fmt.Errorf("ballchasing: %s: %w", errCtx, err)
	}
	loc := res.Location
	if loc == "" {
		loc = headerLoc
	}
	if loc == "" && res.ID != "" {
		loc = fmt.Sprintf("https://ballchasing.com/replay/%s", res.ID)
	}
	return &UploadResult{
		ID:          res.ID,
		Location:    loc,
		IsDuplicate: isDup,
	}, 0, false, nil
}

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

	baseGUID := filepath.Base(matchGUID)
	filename := fmt.Sprintf("%s.replay", strings.TrimSuffix(baseGUID, ".replay"))

	if c.streamUpload {
		var headerBuf bytes.Buffer
		mw := multipart.NewWriter(&headerBuf)
		if _, err := mw.CreateFormFile("file", filename); err != nil {
			_ = file.Close()
			return nil, nil, fmt.Errorf("ballchasing: create form file: %w", err)
		}
		footer := fmt.Sprintf("\r\n--%s--\r\n", mw.Boundary())
		contentLength := int64(headerBuf.Len()) + fileInfo.Size() + int64(len(footer))

		safeCloser := &onceCloser{closer: file}
		bodyWrapper := &readCloserWrapper{
			Reader: io.MultiReader(&headerBuf, file, strings.NewReader(footer)),
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
