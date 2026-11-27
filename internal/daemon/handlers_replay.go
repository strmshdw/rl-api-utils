package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/domain"
	"github.com/dank/rl-api-utils/internal/storage"
)

const (
	replaySourceLocal       = "local"
	replaySourceCache       = "cache"
	replaySourceBallchasing = "ballchasing"
	replaySourceUnavailable = "unavailable"

	ballchasingCacheSubdir = "ballchasing-cache"
	replayFallbackTimeout  = 90 * time.Second
	replaySessionScanLimit = 50
)

// ReplayAvailabilityResponse is returned by GET /api/matches/{guid}/replay/status.
type ReplayAvailabilityResponse struct {
	MatchGUID             string `json:"match_guid"`
	Available             bool   `json:"available"`
	Source                string `json:"source"`
	LocalAvailable        bool   `json:"local_available"`
	BallchasingAvailable  bool   `json:"ballchasing_available"`
	BallchasingConfigured bool   `json:"ballchasing_configured"`
	BallchasingID         string `json:"ballchasing_id,omitempty"`
	BallchasingURL        string `json:"ballchasing_url,omitempty"`
	Filename              string `json:"filename"`
	SizeBytes             int64  `json:"size_bytes,omitempty"`
	DownloadURL           string `json:"download_url"`
	DownloadStatus        string `json:"download_status,omitempty"`
	UploadStatus          string `json:"upload_status,omitempty"`
	Reason                string `json:"reason,omitempty"`
	Message               string `json:"message,omitempty"`
}

type replayErrorResponse struct {
	Error             string `json:"error"`
	Code              string `json:"code"`
	RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
}

type replayDownloadCall struct {
	done chan struct{}
	size int64
	err  error
}

type matchBallchasingLinker interface {
	UpdateMatchBallchasingLink(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error
}

func writeReplayError(w http.ResponseWriter, status int, code, msg string, retryAfterSec int) {
	if retryAfterSec > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfterSec))
	}
	payload, err := json.Marshal(replayErrorResponse{
		Error:             msg,
		Code:              code,
		RetryAfterSeconds: retryAfterSec,
	})
	if err != nil {
		payload = []byte(`{"error":"internal server error","code":"internal_error"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(payload, '\n'))
}

func parseReplayRouteGUID(r *http.Request) (string, bool) {
	rawGUID := r.PathValue("guid")
	if rawGUID == "" {
		p := strings.TrimPrefix(r.URL.Path, "/api/matches/")
		p = strings.TrimSuffix(p, "/replay/status")
		rawGUID = strings.TrimSuffix(p, "/replay")
	}
	trimmed := strings.Trim(rawGUID, "/")
	if strings.TrimSpace(trimmed) == "" {
		return "", false
	}
	clean, err := domain.SanitizePathSegment(trimmed)
	if err != nil {
		return "", false
	}
	return clean, true
}

func (d *Daemon) handleGetMatchReplayStatus(w http.ResponseWriter, r *http.Request) {
	guid, ok := parseReplayRouteGUID(r)
	if !ok {
		writeReplayError(w, http.StatusBadRequest, "invalid_match_guid", "invalid match guid", 0)
		return
	}

	rec, sessMatch, err := d.resolveMatchOrSession(r.Context(), guid)
	if err != nil {
		if isClientDisconnect(r.Context(), err) {
			return
		}
		if errors.Is(err, storage.ErrInvalidGUID) {
			writeReplayError(w, http.StatusBadRequest, "invalid_match_guid", "invalid match guid", 0)
			return
		}
		if errors.Is(err, storage.ErrMatchNotFound) {
			writeReplayError(w, http.StatusNotFound, "match_not_found", "match not found", 0)
			return
		}
		d.logger.Error("failed to resolve match for replay status", slog.String("guid", guid), slog.Any("error", err))
		writeReplayError(w, http.StatusInternalServerError, "storage_error", "failed to query match record", 0)
		return
	}

	canonicalGUID := resolveCanonicalMatchGUID(guid, rec, sessMatch)
	bcID, bcURL := d.resolveBallchasingInfo(rec, canonicalGUID)
	bcConfigured := d.ballchasingClient != nil

	localPath, localSource, sizeBytes := d.findLocalReplayFile(rec, canonicalGUID, bcID)

	if localPath == "" && bcID == "" && isTruthyQuery(r.URL.Query().Get("lookup")) && bcConfigured {
		found, findErr := d.lookupBallchasingFallback(r.Context(), canonicalGUID, rec, sessMatch)
		if findErr != nil {
			if isClientDisconnect(r.Context(), findErr) {
				return
			}
			if !errors.Is(findErr, ballchasing.ErrNotFound) {
				d.writeBallchasingLookupError(w, findErr, rec == nil && sessMatch != nil)
				return
			}
		} else if found != nil {
			bcID = strings.TrimSpace(found.ID)
			bcURL = storage.NormalizeBallchasingURL(bcID, found.Link)
			localPath, localSource, sizeBytes = d.findLocalReplayFile(rec, canonicalGUID, bcID)
		}
	}

	filename := resolveReplayFilename(rec, canonicalGUID)
	resp := ReplayAvailabilityResponse{
		MatchGUID:             canonicalGUID,
		BallchasingConfigured: bcConfigured,
		BallchasingID:         bcID,
		BallchasingURL:        bcURL,
		Filename:              filename,
		DownloadURL:           fmt.Sprintf("/api/matches/%s/replay", url.PathEscape(canonicalGUID)),
	}
	if rec != nil {
		resp.DownloadStatus = string(rec.DownloadStatus)
		resp.UploadStatus = string(rec.UploadStatus)
	}

	if localPath != "" {
		resp.Available = true
		resp.Source = localSource
		resp.LocalAvailable = true
		resp.SizeBytes = sizeBytes
		resp.BallchasingAvailable = bcID != "" && bcConfigured
	} else if bcID != "" {
		if bcConfigured {
			resp.Available = true
			resp.Source = replaySourceBallchasing
			resp.BallchasingAvailable = true
		} else {
			resp.Available = false
			resp.Source = replaySourceUnavailable
			resp.Reason = "missing_ballchasing_key"
			resp.Message = "Local replay file is unavailable and no Ballchasing API key is configured."
		}
	} else if rec == nil && sessMatch != nil {
		resp.Available = false
		resp.Source = replaySourceUnavailable
		resp.Reason = "not_synced_yet"
		resp.Message = "This session match has not been synced to local replay storage yet."
	} else if !bcConfigured && rec != nil && (rec.UploadStatus == storage.UploadUploaded || rec.UploadStatus == storage.UploadDuplicate) {
		resp.Available = false
		resp.Source = replaySourceUnavailable
		resp.Reason = "missing_ballchasing_key"
		resp.Message = "Local replay file is unavailable and no Ballchasing API key is configured."
	} else {
		resp.Available = false
		resp.Source = replaySourceUnavailable
		resp.Reason = "no_replay_source"
		resp.Message = "No local replay file or Ballchasing replay is linked to this match."
	}

	d.writeJSONResponse(w, r, http.StatusOK, resp, "error encoding replay status response")
}

func (d *Daemon) handleGetMatchReplay(w http.ResponseWriter, r *http.Request) {
	guid, ok := parseReplayRouteGUID(r)
	if !ok {
		writeReplayError(w, http.StatusBadRequest, "invalid_match_guid", "invalid match guid", 0)
		return
	}

	rec, sessMatch, err := d.resolveMatchOrSession(r.Context(), guid)
	if err != nil {
		if isClientDisconnect(r.Context(), err) {
			return
		}
		if errors.Is(err, storage.ErrInvalidGUID) {
			writeReplayError(w, http.StatusBadRequest, "invalid_match_guid", "invalid match guid", 0)
			return
		}
		if errors.Is(err, storage.ErrMatchNotFound) {
			writeReplayError(w, http.StatusNotFound, "match_not_found", "match not found", 0)
			return
		}
		d.logger.Error("failed to resolve match for replay download", slog.String("guid", guid), slog.Any("error", err))
		writeReplayError(w, http.StatusInternalServerError, "storage_error", "failed to query match record", 0)
		return
	}

	canonicalGUID := resolveCanonicalMatchGUID(guid, rec, sessMatch)
	filename := resolveReplayFilename(rec, canonicalGUID)
	asAttachment := isTruthyQuery(r.URL.Query().Get("download"))
	bcID, _ := d.resolveBallchasingInfo(rec, canonicalGUID)

	// 1. Local disk or viewer cache hit (instant, zero network).
	if localPath, source, _ := d.findLocalReplayFile(rec, canonicalGUID, bcID); localPath != "" {
		d.serveReplayFile(w, r, localPath, source, filename, canonicalGUID, bcID, asAttachment)
		return
	}

	// HEAD never triggers an external Ballchasing download.
	if r.Method == http.MethodHead {
		if bcID != "" {
			if d.ballchasingClient == nil {
				writeReplayError(w, http.StatusServiceUnavailable, "missing_ballchasing_api_key", "Ballchasing API key is not configured.", 0)
				return
			}
			setReplayResponseHeaders(w, replaySourceBallchasing, filename, canonicalGUID, bcID, asAttachment)
			w.WriteHeader(http.StatusOK)
			return
		}
		if rec == nil && sessMatch != nil {
			writeReplayError(w, http.StatusNotFound, "replay_not_synced", "Match replay has not been synced yet.", 0)
			return
		}
		writeReplayError(w, http.StatusNotFound, "replay_unavailable", "No local or Ballchasing replay available for this match.", 0)
		return
	}

	// 2. Need Ballchasing API for download (or fallback metadata lookup).
	if d.ballchasingClient == nil {
		if bcID != "" || (rec != nil && (rec.UploadStatus == storage.UploadUploaded || rec.UploadStatus == storage.UploadDuplicate)) {
			writeReplayError(w, http.StatusServiceUnavailable, "missing_ballchasing_api_key", "Ballchasing API key is not configured; cannot download replay from Ballchasing.", 0)
			return
		}
		if rec == nil && sessMatch != nil {
			writeReplayError(w, http.StatusNotFound, "replay_not_synced", "Match replay has not been synced locally and Ballchasing API is not configured.", 0)
			return
		}
		writeReplayError(w, http.StatusNotFound, "replay_unavailable", "No local replay file found and Ballchasing API is not configured.", 0)
		return
	}

	// 3. Fallback metadata search on Ballchasing if ballchasing_id is not cached yet.
	if bcID == "" {
		found, findErr := d.lookupBallchasingFallback(r.Context(), canonicalGUID, rec, sessMatch)
		if findErr != nil {
			if isClientDisconnect(r.Context(), findErr) {
				return
			}
			d.writeBallchasingLookupError(w, findErr, rec == nil && sessMatch != nil)
			return
		}
		if found == nil || strings.TrimSpace(found.ID) == "" {
			if rec == nil && sessMatch != nil {
				writeReplayError(w, http.StatusNotFound, "replay_not_synced", "Match has not been synced locally or uploaded to Ballchasing yet.", 0)
				return
			}
			writeReplayError(w, http.StatusNotFound, "replay_unavailable", "No matching replay found on Ballchasing.", 0)
			return
		}
		bcID = strings.TrimSpace(found.ID)
		if cachedPath, source, _ := d.findLocalReplayFile(rec, canonicalGUID, bcID); cachedPath != "" {
			d.serveReplayFile(w, r, cachedPath, source, filename, canonicalGUID, bcID, asAttachment)
			return
		}
	}

	// 4. Download from Ballchasing into isolated viewer cache with singleflight deduplication.
	cachePath := d.ballchasingCachePath(bcID, canonicalGUID)
	if _, dlErr := d.downloadBallchasingReplaySingleflight(r.Context(), bcID, cachePath); dlErr != nil {
		if isClientDisconnect(r.Context(), dlErr) {
			return
		}
		d.writeBallchasingDownloadError(w, dlErr)
		return
	}
	if rec == nil {
		d.mirrorSessionCacheAlias(cachePath, canonicalGUID)
	}

	d.serveReplayFile(w, r, cachePath, replaySourceBallchasing, filename, canonicalGUID, bcID, asAttachment)
}

func (d *Daemon) downloadBallchasingReplaySingleflight(ctx context.Context, bcID, cachePath string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	d.replayFlightMu.Lock()
	if d.replayInFlight == nil {
		d.replayInFlight = make(map[string]*replayDownloadCall)
	}
	if existing, ok := d.replayInFlight[bcID]; ok {
		d.replayFlightMu.Unlock()
		select {
		case <-existing.done:
			return existing.size, existing.err
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}

	call := &replayDownloadCall{done: make(chan struct{})}
	d.replayInFlight[bcID] = call
	d.replayFlightMu.Unlock()

	timeout := replayFallbackTimeout
	if d.cfg != nil && d.cfg.Ballchasing.Timeout.Duration() > timeout {
		timeout = d.cfg.Ballchasing.Timeout.Duration()
	}

	go func() {
		dlCtx, cancel := context.WithTimeout(d.daemonContext(), timeout)
		defer cancel()

		size, err := d.ballchasingClient.DownloadReplayFile(dlCtx, bcID, cachePath)
		call.size = size
		call.err = err

		// Delete before closing done so immediate retries after failure never attach to a stale call.
		d.replayFlightMu.Lock()
		delete(d.replayInFlight, bcID)
		d.replayFlightMu.Unlock()

		close(call.done)
	}()

	select {
	case <-call.done:
		return call.size, call.err
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func (d *Daemon) serveReplayFile(
	w http.ResponseWriter,
	r *http.Request,
	filePath, source, filename, matchGUID, bcID string,
	asAttachment bool,
) {
	f, err := os.Open(filePath)
	if err != nil {
		d.logger.Warn("failed to open replay file for serving", slog.String("path", filePath), slog.Any("error", err))
		writeReplayError(w, http.StatusNotFound, "replay_unavailable", "replay file is no longer available on disk", 0)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || info.IsDir() || info.Size() <= 0 {
		writeReplayError(w, http.StatusNotFound, "replay_unavailable", "replay file is empty or unreadable", 0)
		return
	}

	setReplayResponseHeaders(w, source, filename, matchGUID, bcID, asAttachment)
	http.ServeContent(w, r, filename, info.ModTime(), f)
}

func setReplayResponseHeaders(w http.ResponseWriter, source, filename, matchGUID, bcID string, asAttachment bool) {
	disposition := "inline"
	if asAttachment {
		disposition = "attachment"
	}
	safeName := sanitizeHeaderFilename(filename)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, safeName))
	if source == replaySourceBallchasing {
		w.Header().Set("Cache-Control", "private, no-store")
	} else {
		w.Header().Set("Cache-Control", "private, max-age=3600")
	}
	w.Header().Set("X-Replay-Source", source)
	w.Header().Set("X-Replay-Filename", safeName)
	if matchGUID != "" {
		w.Header().Set("X-Replay-Match-GUID", matchGUID)
	}
	if bcID != "" {
		w.Header().Set("X-Replay-Ballchasing-ID", bcID)
	}
}

func (d *Daemon) writeBallchasingLookupError(w http.ResponseWriter, err error, isSessionOnly bool) {
	switch {
	case errors.Is(err, ballchasing.ErrInvalidAPIKey):
		writeReplayError(w, http.StatusBadGateway, "ballchasing_auth_failed", "Ballchasing rejected the configured API key (HTTP 401).", 0)
	case errors.Is(err, ballchasing.ErrForbidden):
		writeReplayError(w, http.StatusForbidden, "ballchasing_forbidden", "Ballchasing denied access while searching replays (HTTP 403).", 0)
	case errors.Is(err, ballchasing.ErrRateLimitExhausted):
		retrySec := extractRetryAfterSeconds(err)
		writeReplayError(w, http.StatusTooManyRequests, "ballchasing_rate_limited", "Ballchasing API rate limit reached; please retry shortly.", retrySec)
	case errors.Is(err, ballchasing.ErrNotFound):
		if isSessionOnly {
			writeReplayError(w, http.StatusNotFound, "replay_not_synced", "Match has not been synced locally or uploaded to Ballchasing yet.", 0)
			return
		}
		writeReplayError(w, http.StatusNotFound, "replay_unavailable", "No local replay file or matching Ballchasing replay was found.", 0)
	default:
		d.logger.Warn("ballchasing metadata lookup failed", slog.Any("error", err))
		writeReplayError(w, http.StatusBadGateway, "ballchasing_upstream_error", "Failed to search Ballchasing for replay metadata.", 0)
	}
}

func (d *Daemon) writeBallchasingDownloadError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ballchasing.ErrInvalidAPIKey):
		writeReplayError(w, http.StatusBadGateway, "ballchasing_auth_failed", "Ballchasing rejected the configured API key (HTTP 401).", 0)
	case errors.Is(err, ballchasing.ErrForbidden):
		writeReplayError(w, http.StatusForbidden, "ballchasing_forbidden", "Ballchasing denied access to this replay (HTTP 403).", 0)
	case errors.Is(err, ballchasing.ErrNotFound):
		writeReplayError(w, http.StatusNotFound, "ballchasing_not_found", "Replay was not found on Ballchasing (HTTP 404).", 0)
	case errors.Is(err, ballchasing.ErrRateLimitExhausted):
		retrySec := extractRetryAfterSeconds(err)
		writeReplayError(w, http.StatusTooManyRequests, "ballchasing_rate_limited", "Ballchasing API rate limit reached; please retry shortly.", retrySec)
	default:
		d.logger.Warn("ballchasing replay binary download failed", slog.Any("error", err))
		writeReplayError(w, http.StatusBadGateway, "ballchasing_upstream_error", "Failed to download replay binary from Ballchasing.", 0)
	}
}

func extractRetryAfterSeconds(err error) int {
	var rlErr *ballchasing.RateLimitError
	if errors.As(err, &rlErr) && rlErr != nil && rlErr.RetryAfter > 0 {
		secs := int(math.Ceil(rlErr.RetryAfter.Seconds()))
		if secs > 0 {
			return secs
		}
	}
	return 5
}
