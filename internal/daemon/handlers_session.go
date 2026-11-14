package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/dank/rl-api-utils/internal/domain"
	"github.com/dank/rl-api-utils/internal/session"
	"github.com/dank/rl-api-utils/internal/storage"
)

// SessionListResponse represents the paginated response for GET /api/sessions.
type SessionListResponse struct {
	Sessions []*session.SessionResponse `json:"sessions"`
	Total    int                        `json:"total"`
	Limit    int                        `json:"limit"`
	Offset   int                        `json:"offset"`
}

// sessionActionRequest is the optional JSON body for POST /api/session/continue and POST /api/session/dismiss.
type sessionActionRequest struct {
	SessionID string `json:"session_id"`
}

func (d *Daemon) enrichSessionTelemetry(ctx context.Context, summary *session.SessionResponse) *session.SessionResponse {
	if summary == nil {
		return nil
	}
	st := d.buildStatusResponse(ctx)
	summary.StatsAPIReady = st.StatsAPIReady
	summary.RankAPIReady = st.RankAPIReady
	summary.MMRPollerState = st.MMRPollerState
	summary.GameConnected = st.GameConnected
	summary.PendingCount = st.PendingCount
	summary.TriggerThreshold = st.TriggerThreshold
	summary.ForceSyncOnTrigger = st.ForceSyncOnTrigger
	summary.EnableToast = st.EnableToast
	summary.AutoSyncOnExit = st.AutoSyncOnExit
	summary.MaxHostedReplays = st.MaxHostedReplays
	summary.ReplayCount = st.ReplayCount
	summary.ReplayLimit = st.ReplayLimit
	summary.ReplayUsagePct = st.ReplayUsagePct
	summary.ReplayLimitStatus = st.ReplayLimitStatus
	return summary
}

// handleGetSession handles GET /api/session, returning current session telemetry,
// playlist progression, match history list, and any continuable previous session.
func (d *Daemon) handleGetSession(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	playlist := strings.TrimSpace(q.Get("playlist"))
	fromUnix := storage.ParseDateFilter(q.Get("from"), false)
	toUnix := storage.ParseDateFilter(q.Get("to"), true)

	tracker := d.sessionTracker
	if tracker == nil {
		tracker = session.NewSessionTracker()
	}
	summary := session.FilterSessionSummary(tracker.GetSessionSummary(), playlist, fromUnix, toUnix, d.stateStore)
	summary = d.enrichSessionTelemetry(r.Context(), summary)
	d.writeJSONResponse(w, r, http.StatusOK, summary, "error encoding session summary")
}

// handleResetSession handles POST /api/session/reset, resetting active session counters
// and broadcasting session_update to connected SSE clients.
func (d *Daemon) handleResetSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	if !isAllowedMutatingOrigin(r) {
		writeJSONError(w, http.StatusForbidden, "forbidden: invalid origin")
		return
	}

	if d.sessionTracker != nil {
		d.sessionTracker.Reset()
	}

	d.writeJSONResponse(w, r, http.StatusOK, map[string]string{
		"status":  "ok",
		"message": "session reset",
	}, "error encoding session reset response")
}

// handleContinueSession handles POST /api/session/continue, resuming a previous session
// (and merging any matches already played in the current session) if within the continuation window.
func (d *Daemon) handleContinueSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	if !isAllowedMutatingOrigin(r) {
		writeJSONError(w, http.StatusForbidden, "forbidden: invalid origin")
		return
	}

	if d.sessionTracker == nil {
		writeJSONError(w, http.StatusConflict, session.ErrNoContinuableSession.Error())
		return
	}

	var reqBody sessionActionRequest
	if !decodeJSONBody(w, r, &reqBody, true) {
		return
	}

	summary, err := d.sessionTracker.ContinueSession(reqBody.SessionID)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, storage.ErrSessionNotFound):
			status = http.StatusNotFound
		case errors.Is(err, session.ErrNoContinuableSession), errors.Is(err, session.ErrContinueWindowExpired):
			status = http.StatusConflict
		}
		writeJSONError(w, status, err.Error())
		return
	}

	summary = d.enrichSessionTelemetry(r.Context(), summary)
	d.writeJSONResponse(w, r, http.StatusOK, summary, "error encoding continued session summary")
}

// handleDismissContinueSession handles POST /api/session/dismiss, dismissing the continuation
// prompt for the current session.
func (d *Daemon) handleDismissContinueSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	if !isAllowedMutatingOrigin(r) {
		writeJSONError(w, http.StatusForbidden, "forbidden: invalid origin")
		return
	}

	var reqBody sessionActionRequest
	if !decodeJSONBody(w, r, &reqBody, true) {
		return
	}

	if d.sessionTracker == nil {
		d.writeJSONResponse(w, r, http.StatusOK,
			d.enrichSessionTelemetry(r.Context(), session.NewSessionTracker().GetSessionSummary()),
			"error encoding dismissed session summary")
		return
	}

	summary := d.enrichSessionTelemetry(r.Context(), d.sessionTracker.DismissContinuableSession(reqBody.SessionID))
	d.writeJSONResponse(w, r, http.StatusOK, summary, "error encoding dismissed session summary")
}

// handleListSessions handles GET /api/sessions, returning a paginated list of persisted sessions.
func (d *Daemon) handleListSessions(w http.ResponseWriter, r *http.Request) {
	limit, offset := parseLimitOffset(r.URL.Query(), 50, 200)

	if d.sessionTracker == nil {
		d.writeJSONResponse(w, r, http.StatusOK, SessionListResponse{
			Sessions: []*session.SessionResponse{},
			Total:    0,
			Limit:    limit,
			Offset:   offset,
		}, "error encoding empty session list")
		return
	}

	sessions, total, err := d.sessionTracker.ListSessions(r.Context(), limit, offset)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(SessionListResponse{
		Sessions: sessions,
		Total:    total,
		Limit:    limit,
		Offset:   offset,
	})
}

// handleGetHistoricalSession handles GET /api/sessions/{id}, returning the full details of a specific session.
func (d *Daemon) handleGetHistoricalSession(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimSpace(r.PathValue("id"))
	if sessionID == "" {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	cleanID, err := domain.SanitizePathSegment(sessionID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid session id")
		return
	}

	if d.sessionTracker == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}

	resp, err := d.sessionTracker.GetHistoricalSession(r.Context(), cleanID)
	if err != nil {
		if errors.Is(err, storage.ErrSessionNotFound) {
			writeJSONError(w, http.StatusNotFound, "session not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp = d.enrichSessionTelemetry(r.Context(), resp)
	d.writeJSONResponse(w, r, http.StatusOK, resp, "error encoding historical session")
}
