package daemon

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

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

// handleGetSession handles GET /api/session, returning current session telemetry,
// playlist progression, match history list, and any continuable previous session.
func (d *Daemon) handleGetSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if d.sessionTracker == nil {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(session.NewSessionTracker().GetSessionSummary())
		return
	}

	summary := d.sessionTracker.GetSessionSummary()
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(summary); err != nil {
		d.logger.Warn("error encoding session summary", "error", err)
	}
}

// handleResetSession handles POST /api/session/reset, resetting active session counters
// and broadcasting session_update to connected SSE clients.
func (d *Daemon) handleResetSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if !isAllowedMutatingOrigin(r) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "forbidden: invalid origin"})
		return
	}

	if d.sessionTracker != nil {
		d.sessionTracker.Reset()
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "session reset",
	})
}

// handleContinueSession handles POST /api/session/continue, resuming a previous session
// (and merging any matches already played in the current session) if within the continuation window.
func (d *Daemon) handleContinueSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if !isAllowedMutatingOrigin(r) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "forbidden: invalid origin"})
		return
	}

	if d.sessionTracker == nil {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": session.ErrNoContinuableSession.Error()})
		return
	}

	var reqBody sessionActionRequest
	if r.Body != nil {
		defer r.Body.Close()
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&reqBody); err != nil && !errors.Is(err, io.EOF) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
			return
		}
	}

	summary, err := d.sessionTracker.ContinueSession(reqBody.SessionID)
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrSessionNotFound):
			w.WriteHeader(http.StatusNotFound)
		case errors.Is(err, session.ErrNoContinuableSession), errors.Is(err, session.ErrContinueWindowExpired):
			w.WriteHeader(http.StatusConflict)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(summary); err != nil {
		d.logger.Warn("error encoding continued session summary", "error", err)
	}
}

// handleDismissContinueSession handles POST /api/session/dismiss, dismissing the continuation
// prompt for the current session.
func (d *Daemon) handleDismissContinueSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if !isAllowedMutatingOrigin(r) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "forbidden: invalid origin"})
		return
	}

	var reqBody sessionActionRequest
	if r.Body != nil {
		defer r.Body.Close()
		_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&reqBody)
	}

	if d.sessionTracker == nil {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(session.NewSessionTracker().GetSessionSummary())
		return
	}

	summary := d.sessionTracker.DismissContinuableSession(reqBody.SessionID)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(summary)
}

// handleListSessions handles GET /api/sessions, returning a paginated list of persisted sessions.
func (d *Daemon) handleListSessions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	limit := 50
	offset := 0
	q := r.URL.Query()
	if lStr := strings.TrimSpace(q.Get("limit")); lStr != "" {
		if v, err := strconv.Atoi(lStr); err == nil && v > 0 {
			limit = v
			if limit > 200 {
				limit = 200
			}
		}
	}
	if oStr := strings.TrimSpace(q.Get("offset")); oStr != "" {
		if v, err := strconv.Atoi(oStr); err == nil && v >= 0 {
			offset = v
		}
	}

	if d.sessionTracker == nil {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(SessionListResponse{
			Sessions: []*session.SessionResponse{},
			Total:    0,
			Limit:    limit,
			Offset:   offset,
		})
		return
	}

	sessions, total, err := d.sessionTracker.ListSessions(r.Context(), limit, offset)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

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
	w.Header().Set("Content-Type", "application/json")

	sessionID := strings.TrimSpace(r.PathValue("id"))
	if sessionID == "" {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "session not found"})
		return
	}

	if d.sessionTracker == nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "session not found"})
		return
	}

	resp, err := d.sessionTracker.GetHistoricalSession(r.Context(), sessionID)
	if err != nil {
		if errors.Is(err, storage.ErrSessionNotFound) {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "session not found"})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

