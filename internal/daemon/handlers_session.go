package daemon

import (
	"encoding/json"
	"net/http"

	"github.com/dank/rl-api-utils/internal/session"
)

// handleGetSession handles GET /api/session, returning current session telemetry,
// playlist progression, and match history list.
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

	if d.sessionTracker != nil {
		d.sessionTracker.Reset()
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "session reset",
	})
}
