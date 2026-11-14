package ballchasing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// GetUploadedReplays queries Ballchasing API for replays uploaded by the user.
// If nextURL is non-empty, it queries nextURL directly for cursor-based pagination.
// Otherwise, it queries GET {baseURL}/replays?uploader=me&count=200.
func (c *Client) GetUploadedReplays(ctx context.Context, nextURL string) (*ReplayListResponse, error) {
	reqURL := strings.TrimSpace(nextURL)
	if reqURL == "" {
		reqURL = fmt.Sprintf("%s/replays?uploader=me&count=200", strings.TrimRight(c.baseURL, "/"))
	} else if !strings.HasPrefix(reqURL, "http://") && !strings.HasPrefix(reqURL, "https://") {
		reqURL = fmt.Sprintf("%s/%s", strings.TrimRight(c.baseURL, "/"), strings.TrimLeft(reqURL, "/"))
	}

	bodyBytes, err := c.doJSONGetWithRetry(ctx, reqURL, "create get replays request", "")
	if err != nil {
		return nil, err
	}

	var listResp ReplayListResponse
	if err := json.Unmarshal(bodyBytes, &listResp); err != nil {
		return nil, fmt.Errorf("ballchasing: decode replay list: %w", err)
	}
	return &listResp, nil
}

// GetReplay queries Ballchasing API for a single replay by its Ballchasing replay ID.
// Returns wrapped ErrNotFound on HTTP 404 so callers can inspect with errors.Is(err, ErrNotFound).
func (c *Client) GetReplay(ctx context.Context, id string) (*ReplayItem, error) {
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return nil, fmt.Errorf("ballchasing: empty replay id")
	}

	reqURL := fmt.Sprintf("%s/replays/%s", strings.TrimRight(c.baseURL, "/"), url.PathEscape(trimmedID))

	bodyBytes, err := c.doJSONGetWithRetry(ctx, reqURL, "create get replay request", trimmedID)
	if err != nil {
		return nil, err
	}

	var item ReplayItem
	if err := json.Unmarshal(bodyBytes, &item); err != nil {
		return nil, fmt.Errorf("ballchasing: decode replay: %w", err)
	}
	return &item, nil
}

func (c *Client) doJSONGetWithRetry(ctx context.Context, reqURL, reqErrPrefix, notFoundID string) ([]byte, error) {
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if err := c.waitRateLimit(ctx); err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("ballchasing: %s: %w", reqErrPrefix, err)
		}
		req.Header.Set("Authorization", c.apiKey)
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attempt < c.maxRetries {
				if sleepErr := sleepWithContext(ctx, c.calculateBackoff(attempt)); sleepErr != nil {
					return nil, sleepErr
				}
				continue
			}
			return nil, fmt.Errorf("ballchasing: request failed: %w", err)
		}

		bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("ballchasing: read response body: %w", readErr)
		}

		switch resp.StatusCode {
		case http.StatusOK:
			return bodyBytes, nil
		case http.StatusTooManyRequests:
			if attempt >= c.maxRetries {
				return nil, ErrRateLimitExhausted
			}
			retryAfter := c.resolveRetryAfter(resp.Header.Get("Retry-After"), attempt)
			if sleepErr := sleepWithContext(ctx, retryAfter); sleepErr != nil {
				return nil, sleepErr
			}
		case http.StatusUnauthorized:
			return nil, ErrInvalidAPIKey
		case http.StatusNotFound:
			if notFoundID != "" {
				return nil, fmt.Errorf("%w: replay %s", ErrNotFound, notFoundID)
			}
			msg := extractErrorMessage(bodyBytes)
			if msg == "" {
				msg = reqURL
			}
			return nil, fmt.Errorf("%w: %s", ErrNotFound, msg)
		default:
			if resp.StatusCode >= 500 {
				if attempt < c.maxRetries {
					if sleepErr := sleepWithContext(ctx, c.calculateBackoff(attempt)); sleepErr != nil {
						return nil, sleepErr
					}
					continue
				}
				return nil, fmt.Errorf("%w: HTTP %d", ErrServerError, resp.StatusCode)
			}
			return nil, fmt.Errorf("ballchasing: unexpected status HTTP %d: %s", resp.StatusCode, extractErrorMessage(bodyBytes))
		}
	}

	return nil, ErrRateLimitExhausted
}
