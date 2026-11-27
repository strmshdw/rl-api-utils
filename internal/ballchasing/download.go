package ballchasing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// MaxReplayDownloadBytes caps downloaded replay files to protect memory and disk (64 MiB).
	MaxReplayDownloadBytes int64 = 64 * 1024 * 1024

	downloadBufferBytes   = 64 * 1024
	downloadSniffBytes    = 512
	downloadErrorCapBytes = 64 * 1024
	downloadRenameRetries = 5
	replaySearchWindow    = 10 * time.Minute
	replayTightWindow     = 5 * time.Minute
	replayVeryTightWindow = 2 * time.Minute
	replaySearchLimit     = 25
	maxDetailVerifyCount  = 3
)

// DownloadReplayFile downloads a raw .replay binary from GET {baseURL}/replays/{id}/file
// into destPath using a per-attempt temporary file and atomic rename.
// It honors the shared RateLimiter, retries 429/5xx/network faults, caps size at 64 MiB,
// rejects empty or HTML error payloads served with HTTP 200, and cleans up temp files on failure.
func (c *Client) DownloadReplayFile(ctx context.Context, replayID, destPath string) (int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	trimmedID := strings.TrimSpace(replayID)
	if trimmedID == "" {
		return 0, fmt.Errorf("%w: replay id cannot be empty", ErrBadRequest)
	}
	trimmedDest := strings.TrimSpace(destPath)
	if trimmedDest == "" {
		return 0, ErrEmptyFilePath
	}

	destDir := filepath.Dir(trimmedDest)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return 0, fmt.Errorf("ballchasing: create download directory %s: %w", destDir, err)
	}

	endpoint := fmt.Sprintf("%s/replays/%s/file", strings.TrimRight(c.baseURL, "/"), url.PathEscape(trimmedID))

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if err := c.waitRateLimit(ctx); err != nil {
			return 0, err
		}

		written, waitDuration, retryable, err := c.doDownloadAttempt(ctx, endpoint, trimmedID, destDir, trimmedDest, attempt)
		if !retryable {
			return written, err
		}

		if attempt >= c.maxRetries {
			if err != nil {
				return 0, err
			}
			return 0, &RateLimitError{RetryAfter: waitDuration}
		}

		if sleepErr := sleepWithContext(ctx, waitDuration); sleepErr != nil {
			return 0, sleepErr
		}
	}

	return 0, &RateLimitError{RetryAfter: c.baseBackoff}
}

func (c *Client) doDownloadAttempt(
	ctx context.Context,
	endpoint, replayID, destDir, destPath string,
	attempt int,
) (int64, time.Duration, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, 0, false, fmt.Errorf("ballchasing: create download replay request: %w", err)
	}
	req.Header.Set("Authorization", c.apiKey)
	req.Header.Set("Accept", "application/octet-stream, */*")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, 0, false, ctx.Err()
		}
		if attempt < c.maxRetries {
			return 0, c.calculateBackoff(attempt), true, err
		}
		return 0, 0, false, fmt.Errorf("ballchasing: download replay request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, downloadErrorCapBytes))
		return c.handleDownloadErrorStatus(resp, replayID, bodyBytes, attempt)
	}

	ct := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if isNonReplayContentType(ct) {
		return 0, 0, false, fmt.Errorf("%w: upstream returned Content-Type %q", ErrInvalidPayload, ct)
	}

	written, err := streamReplayToAtomicFile(ctx, resp.Body, destDir, destPath)
	if err != nil {
		if ctx.Err() != nil {
			return 0, 0, false, ctx.Err()
		}
		if !errors.Is(err, ErrEmptyFile) && !errors.Is(err, ErrInvalidPayload) && attempt < c.maxRetries {
			return 0, c.calculateBackoff(attempt), true, err
		}
		return 0, 0, false, err
	}
	return written, 0, false, nil
}

func (c *Client) handleDownloadErrorStatus(
	resp *http.Response,
	replayID string,
	bodyBytes []byte,
	attempt int,
) (int64, time.Duration, bool, error) {
	msg := extractErrorMessage(bodyBytes)
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return 0, 0, false, ErrInvalidAPIKey
	case http.StatusForbidden:
		return 0, 0, false, fmt.Errorf("%w: replay %s", ErrForbidden, replayID)
	case http.StatusNotFound:
		return 0, 0, false, fmt.Errorf("%w: replay %s", ErrNotFound, replayID)
	case http.StatusTooManyRequests:
		retryAfter := c.resolveRetryAfter(resp.Header.Get("Retry-After"), attempt)
		if attempt >= c.maxRetries {
			return 0, 0, false, &RateLimitError{RetryAfter: retryAfter, Message: msg}
		}
		return 0, retryAfter, true, &RateLimitError{RetryAfter: retryAfter, Message: msg}
	default:
		if resp.StatusCode >= 500 {
			err := fmt.Errorf("%w: HTTP %d", ErrServerError, resp.StatusCode)
			if attempt < c.maxRetries {
				return 0, c.calculateBackoff(attempt), true, err
			}
			return 0, 0, false, err
		}
		return 0, 0, false, fmt.Errorf("ballchasing: download replay HTTP %d: %s", resp.StatusCode, msg)
	}
}

func streamReplayToAtomicFile(ctx context.Context, body io.Reader, destDir, destPath string) (int64, error) {
	tmpFile, err := os.CreateTemp(destDir, ".tmp-bc-*.replay")
	if err != nil {
		return 0, fmt.Errorf("ballchasing: create temp replay file: %w", err)
	}
	tmpPath := tmpFile.Name()

	committed := false
	defer func() {
		_ = tmpFile.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()

	limited := io.LimitReader(body, MaxReplayDownloadBytes+1)
	sniff := make([]byte, downloadSniffBytes)
	n, readErr := io.ReadFull(limited, sniff)
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return 0, fmt.Errorf("ballchasing: read replay stream: %w", readErr)
	}
	if n == 0 {
		return 0, ErrEmptyFile
	}

	prefix := sniff[:n]
	if looksLikeNonReplayPayload(prefix) {
		return 0, fmt.Errorf("%w: upstream returned HTML or JSON error document", ErrInvalidPayload)
	}

	written := int64(0)
	wn, writeErr := tmpFile.Write(prefix)
	written += int64(wn)
	if writeErr != nil {
		return 0, fmt.Errorf("ballchasing: write replay temp file: %w", writeErr)
	}

	if readErr == nil {
		buf := make([]byte, downloadBufferBytes)
		copied, copyErr := io.CopyBuffer(tmpFile, limited, buf)
		written += copied
		if copyErr != nil {
			return 0, fmt.Errorf("ballchasing: stream replay payload: %w", copyErr)
		}
	}

	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if written == 0 {
		return 0, ErrEmptyFile
	}
	if written > MaxReplayDownloadBytes {
		return 0, fmt.Errorf("%w: replay exceeds max allowed size (%d bytes)", ErrInvalidPayload, MaxReplayDownloadBytes)
	}

	if err := tmpFile.Sync(); err != nil {
		return 0, fmt.Errorf("ballchasing: sync temp replay file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return 0, fmt.Errorf("ballchasing: close temp replay file: %w", err)
	}

	if err := atomicRenameWithRetry(ctx, tmpPath, destPath, downloadRenameRetries); err != nil {
		return 0, fmt.Errorf("ballchasing: commit replay file %s: %w", destPath, err)
	}
	committed = true
	return written, nil
}

func isNonReplayContentType(contentType string) bool {
	if contentType == "" {
		return false
	}
	mediaType, _, _ := strings.Cut(contentType, ";")
	mediaType = strings.TrimSpace(mediaType)
	return strings.HasPrefix(mediaType, "text/") ||
		mediaType == "application/json" ||
		strings.HasSuffix(mediaType, "+json") ||
		mediaType == "application/xhtml+xml" ||
		mediaType == "application/xml"
}

func looksLikeNonReplayPayload(prefix []byte) bool {
	trimmed := bytes.TrimPrefix(prefix, []byte{0xEF, 0xBB, 0xBF})
	trimmed = bytes.TrimLeft(trimmed, " \t\r\n")
	if len(trimmed) == 0 {
		return false
	}
	lower := bytes.ToLower(trimmed)
	for _, sig := range [][]byte{
		[]byte("<!doctype"),
		[]byte("<html"),
		[]byte("<head"),
		[]byte("<body"),
		[]byte("<title"),
		[]byte("<?xml"),
		[]byte(`{"error"`),
	} {
		if bytes.HasPrefix(lower, sig) {
			return true
		}
	}
	first := trimmed[0]
	if (first == '{' || first == '[' || first == '<') && !containsBinaryControlBytes(trimmed) {
		return true
	}
	return false
}

// containsBinaryControlBytes reports whether b contains NUL or non-whitespace ASCII control
// bytes (characteristic of binary UE3 .replay headers whose uint32 header_size always has
// trailing 0x00 bytes, unlike JSON/HTML/XML text error bodies).
func containsBinaryControlBytes(b []byte) bool {
	for _, ch := range b {
		if ch == 0 || (ch < 0x20 && ch != '\t' && ch != '\r' && ch != '\n') {
			return true
		}
	}
	return false
}

func atomicRenameWithRetry(ctx context.Context, oldPath, newPath string, maxRetries int) error {
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
			delay := time.Duration((attempt+1)*5) * time.Millisecond
			if sleepErr := sleepWithContext(ctx, delay); sleepErr != nil {
				return sleepErr
			}
		}
	}
	return err
}

// FindReplayByMetadata searches Ballchasing replays using a bounded timestamp window.
// It queries uploader=me first; if no replay is found and PlayerNames is populated, it runs a
// bounded secondary player-name query so teammate/opponent-uploaded replays can also be resolved.
func (c *Client) FindReplayByMetadata(ctx context.Context, q ReplaySearchQuery) (*ReplayItem, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	params := buildReplaySearchParams(q, "me", "")
	item, err := c.searchReplayCandidates(ctx, q, params, 3)
	if err == nil || !errors.Is(err, ErrNotFound) {
		return item, err
	}

	playerName := firstNonEmptyPlayerName(q.PlayerNames)
	if playerName == "" || (q.Timestamp.IsZero() && normalizeGUID(q.MatchGUID) == "") {
		return nil, err
	}

	pubParams := buildReplaySearchParams(q, "", playerName)
	return c.searchReplayCandidates(ctx, q, pubParams, 5)
}

func buildReplaySearchParams(q ReplaySearchQuery, uploader, playerName string) url.Values {
	params := url.Values{}
	if uploader != "" {
		params.Set("uploader", uploader)
	}
	if playerName != "" {
		params.Set("player-name", playerName)
	}
	params.Set("count", fmt.Sprintf("%d", replaySearchLimit))
	if !q.Timestamp.IsZero() {
		after := q.Timestamp.UTC().Add(-replaySearchWindow).Format(time.RFC3339)
		before := q.Timestamp.UTC().Add(replaySearchWindow).Format(time.RFC3339)
		params.Set("replay-date-after", after)
		params.Set("replay-date-before", before)
	}
	return params
}

func firstNonEmptyPlayerName(names []string) string {
	for _, n := range names {
		if trimmed := strings.TrimSpace(n); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (c *Client) searchReplayCandidates(
	ctx context.Context,
	q ReplaySearchQuery,
	params url.Values,
	minFallbackScore int,
) (*ReplayItem, error) {
	reqURL := fmt.Sprintf("%s/replays?%s", strings.TrimRight(c.baseURL, "/"), params.Encode())
	bodyBytes, err := c.doJSONGetWithRetry(ctx, reqURL, "create search replays request", "")
	if err != nil {
		return nil, err
	}

	var listResp ReplayListResponse
	if err := json.Unmarshal(bodyBytes, &listResp); err != nil {
		return nil, fmt.Errorf("ballchasing: decode replay search list: %w", err)
	}
	if len(listResp.List) == 0 {
		return nil, fmt.Errorf("%w: no matching replays found", ErrNotFound)
	}

	normTarget := normalizeGUID(q.MatchGUID)
	for i := range listResp.List {
		item := &listResp.List[i]
		if normTarget != "" && replayItemMatchesGUID(item, normTarget) {
			return item, nil
		}
	}

	type scoredCandidate struct {
		item     ReplayItem
		score    int
		timeDiff time.Duration
	}
	var candidates []scoredCandidate
	for _, item := range listResp.List {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		if normTarget != "" && strings.TrimSpace(item.MatchGUID) != "" && !replayItemMatchesGUID(&item, normTarget) {
			continue
		}
		sc, diff, ok := scoreReplayCandidate(item, q)
		if !ok {
			continue
		}
		candidates = append(candidates, scoredCandidate{item: item, score: sc, timeDiff: diff})
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("%w: no matching replays found", ErrNotFound)
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].timeDiff < candidates[j].timeDiff
	})

	verifyLimit := len(candidates)
	if verifyLimit > maxDetailVerifyCount {
		verifyLimit = maxDetailVerifyCount
	}

	var fallbackDetail *ReplayItem
	for i := 0; i < verifyLimit; i++ {
		cand := candidates[i]
		if normTarget != "" && strings.TrimSpace(cand.item.MatchGUID) == "" {
			detail, detailErr := c.GetReplay(ctx, cand.item.ID)
			if detailErr == nil && detail != nil {
				if replayItemMatchesGUID(detail, normTarget) {
					return detail, nil
				}
				if strings.TrimSpace(detail.MatchGUID) != "" {
					// Confirmed different MatchGUID — disqualify candidate.
					continue
				}
				if fallbackDetail == nil && cand.score >= minFallbackScore {
					fallbackDetail = detail
				}
				continue
			}
		}
		if fallbackDetail == nil && cand.score >= minFallbackScore {
			cp := cand.item
			fallbackDetail = &cp
		}
	}

	if fallbackDetail != nil {
		return fallbackDetail, nil
	}
	return nil, fmt.Errorf("%w: no confident replay match found", ErrNotFound)
}

func replayItemMatchesGUID(item *ReplayItem, normTarget string) bool {
	if item == nil || normTarget == "" {
		return false
	}
	return normalizeGUID(item.MatchGUID) == normTarget ||
		normalizeGUID(item.RocketLeagueID) == normTarget ||
		normalizeGUID(item.ID) == normTarget
}

func scoreReplayCandidate(item ReplayItem, q ReplaySearchQuery) (int, time.Duration, bool) {
	score := 0
	timeDiff := replaySearchWindow

	if !q.Timestamp.IsZero() {
		itemTime, ok := parseBallchasingTimestamp(item.Date, item.Created)
		if ok {
			diff := itemTime.Sub(q.Timestamp.UTC())
			if diff < 0 {
				diff = -diff
			}
			if diff > replaySearchWindow {
				return 0, diff, false
			}
			timeDiff = diff
			switch {
			case diff <= replayVeryTightWindow:
				score += 3
			case diff <= replayTightWindow:
				score += 2
			default:
				score++
			}
		}
	}

	if q.PlaylistID > 0 {
		if itemPID := MapPlaylistSlugToID(item.PlaylistID); itemPID > 0 {
			if itemPID != q.PlaylistID {
				return 0, timeDiff, false
			}
			score += 2
		}
	}

	blueGoals, orangeGoals := replayItemGoals(item)
	if q.BlueScore > 0 || q.OrangeScore > 0 {
		if blueGoals != q.BlueScore || orangeGoals != q.OrangeScore {
			return 0, timeDiff, false
		}
		score += 3
	}

	if q.MapName != "" {
		wantMap := strings.ToLower(strings.TrimSpace(q.MapName))
		if strings.ToLower(strings.TrimSpace(item.MapName)) == wantMap ||
			strings.ToLower(strings.TrimSpace(item.MapCode)) == wantMap {
			score++
		}
	}

	if len(q.PlayerNames) > 0 {
		wanted := make(map[string]struct{}, len(q.PlayerNames))
		for _, name := range q.PlayerNames {
			if n := strings.ToLower(strings.TrimSpace(name)); n != "" {
				wanted[n] = struct{}{}
			}
		}
		matchedPlayers := 0
		for _, p := range item.Blue.Players {
			if _, ok := wanted[strings.ToLower(strings.TrimSpace(p.Name))]; ok {
				matchedPlayers++
			}
		}
		for _, p := range item.Orange.Players {
			if _, ok := wanted[strings.ToLower(strings.TrimSpace(p.Name))]; ok {
				matchedPlayers++
			}
		}
		score += matchedPlayers * 2
	}

	if score <= 0 {
		return 0, timeDiff, false
	}
	return score, timeDiff, true
}

func replayItemGoals(item ReplayItem) (int, int) {
	blue, orange := item.Blue.Goals, item.Orange.Goals
	if blue == 0 && orange == 0 {
		for _, p := range item.Blue.Players {
			blue += p.Goals
		}
		for _, p := range item.Orange.Players {
			orange += p.Goals
		}
	}
	return blue, orange
}

func parseBallchasingTimestamp(values ...string) (time.Time, bool) {
	for _, raw := range values {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05"} {
			if t, err := time.Parse(layout, trimmed); err == nil {
				return t.UTC(), true
			}
		}
	}
	return time.Time{}, false
}

func normalizeGUID(guid string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(guid), "-", ""))
}
