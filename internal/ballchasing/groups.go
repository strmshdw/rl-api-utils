package ballchasing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	defaultPlayerIdentification = "by-id"
	defaultTeamIdentification   = "by-distinct-players"
)

// CreateGroup creates a new replay group via POST /api/groups on Ballchasing.com.
// Respects the client's shared RateLimiter and retries on HTTP 429 and 5xx errors.
func (c *Client) CreateGroup(ctx context.Context, req CreateGroupRequest) (*CreateGroupResponse, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, fmt.Errorf("%w: group name cannot be empty", ErrBadRequest)
	}
	req.Parent = strings.TrimSpace(req.Parent)
	if strings.TrimSpace(req.PlayerIdentification) == "" {
		req.PlayerIdentification = defaultPlayerIdentification
	}
	if strings.TrimSpace(req.TeamIdentification) == "" {
		req.TeamIdentification = defaultTeamIdentification
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("ballchasing: marshal create group request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/groups", strings.TrimRight(c.baseURL, "/"))
	bodyBytes, headerLoc, statusCode, err := c.doJSONMutateWithRetry(ctx, http.MethodPost, endpoint, payload, "create group")
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusCreated && statusCode != http.StatusOK {
		return nil, fmt.Errorf("ballchasing: create group unexpected HTTP %d: %s", statusCode, extractErrorMessage(bodyBytes))
	}

	var parsed CreateGroupResponse
	if err := json.Unmarshal(bodyBytes, &parsed); err != nil {
		return nil, fmt.Errorf("ballchasing: decode create group response: %w", err)
	}
	parsed.ID = strings.TrimSpace(parsed.ID)
	if parsed.ID == "" {
		return nil, fmt.Errorf("ballchasing: create group response missing group id")
	}
	if parsed.Location == "" {
		parsed.Location = strings.TrimSpace(headerLoc)
	}
	if parsed.Location == "" {
		parsed.Location = fmt.Sprintf("https://ballchasing.com/group/%s", parsed.ID)
	}
	return &parsed, nil
}

// PatchReplay updates metadata (such as title, group assignment, or visibility) for an uploaded
// replay via PATCH /api/replays/{id}. Respects rate limits and retries on HTTP 429/5xx.
func (c *Client) PatchReplay(ctx context.Context, replayID string, req PatchReplayRequest) error {
	trimmedID := strings.TrimSpace(replayID)
	if trimmedID == "" {
		return fmt.Errorf("%w: replay id cannot be empty", ErrBadRequest)
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Group = strings.TrimSpace(req.Group)
	req.Visibility = strings.TrimSpace(req.Visibility)
	if req.Title == "" && req.Group == "" && req.Visibility == "" {
		return nil
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("ballchasing: marshal patch replay request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/replays/%s", strings.TrimRight(c.baseURL, "/"), url.PathEscape(trimmedID))
	bodyBytes, _, statusCode, err := c.doJSONMutateWithRetry(ctx, http.MethodPatch, endpoint, payload, "patch replay")
	if err != nil {
		return err
	}
	if statusCode != http.StatusNoContent && statusCode != http.StatusOK {
		return fmt.Errorf("ballchasing: patch replay unexpected HTTP %d: %s", statusCode, extractErrorMessage(bodyBytes))
	}
	return nil
}

func (c *Client) doJSONMutateWithRetry(ctx context.Context, method, endpoint string, payload []byte, opName string) ([]byte, string, int, error) {
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if err := c.waitRateLimit(ctx); err != nil {
			return nil, "", 0, err
		}

		req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(payload))
		if err != nil {
			return nil, "", 0, fmt.Errorf("ballchasing: %s request: %w", opName, err)
		}
		req.Header.Set("Authorization", c.apiKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, "", 0, ctx.Err()
			}
			if attempt < c.maxRetries {
				if sleepErr := sleepWithContext(ctx, c.calculateBackoff(attempt)); sleepErr != nil {
					return nil, "", 0, sleepErr
				}
				continue
			}
			return nil, "", 0, fmt.Errorf("ballchasing: %s failed: %w", opName, err)
		}

		bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		headerLoc := resp.Header.Get("Location")
		statusCode := resp.StatusCode
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, "", statusCode, fmt.Errorf("ballchasing: %s read body: %w", opName, readErr)
		}

		switch statusCode {
		case http.StatusOK, http.StatusCreated, http.StatusNoContent:
			return bodyBytes, headerLoc, statusCode, nil
		case http.StatusTooManyRequests:
			if attempt >= c.maxRetries {
				return nil, "", statusCode, ErrRateLimitExhausted
			}
			retryAfter := c.resolveRetryAfter(resp.Header.Get("Retry-After"), attempt)
			if sleepErr := sleepWithContext(ctx, retryAfter); sleepErr != nil {
				return nil, "", statusCode, sleepErr
			}
		case http.StatusUnauthorized:
			return nil, "", statusCode, ErrInvalidAPIKey
		case http.StatusBadRequest:
			msg := extractErrorMessage(bodyBytes)
			if msg == "" {
				msg = "bad request"
			}
			return nil, "", statusCode, fmt.Errorf("%w: %s", ErrBadRequest, msg)
		case http.StatusNotFound:
			msg := extractErrorMessage(bodyBytes)
			if msg == "" {
				msg = endpoint
			}
			return nil, "", statusCode, fmt.Errorf("%w: %s", ErrNotFound, msg)
		default:
			if statusCode >= 500 {
				if attempt < c.maxRetries {
					if sleepErr := sleepWithContext(ctx, c.calculateBackoff(attempt)); sleepErr != nil {
						return nil, "", statusCode, sleepErr
					}
					continue
				}
				return nil, "", statusCode, fmt.Errorf("%w: HTTP %d: %s", ErrServerError, statusCode, extractErrorMessage(bodyBytes))
			}
			return nil, "", statusCode, fmt.Errorf("ballchasing: %s HTTP %d: %s", opName, statusCode, extractErrorMessage(bodyBytes))
		}
	}

	return nil, "", 0, ErrRateLimitExhausted
}
