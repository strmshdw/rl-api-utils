package daemon

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/session"
)

// handleEvents handles GET /api/events, establishing a persistent Server-Sent Events stream.
func (d *Daemon) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// 1. Mandatory SSE Headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Prevent proxy buffering (Nginx, Caddy)

	// 2. Disable write deadline for persistent streaming
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})

	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	if d.sessionTracker == nil {
		<-r.Context().Done()
		return
	}

	// 3. Subscribe to session events BEFORE sending initial snapshots.
	// Subscribing prior to transmitting initial snapshots ensures that any events
	// broadcast while snapshots are formatted or flushed are enqueued in eventCh
	// (defaultSubscriberBufferSize = 64) and not dropped due to zero active subscribers.
	eventCh, cancel := d.sessionTracker.Subscribe()
	defer cancel()

	// 4. Initial Snapshot: session_update
	summary := d.sessionTracker.GetSessionSummary()
	if summary != nil {
		sessionEv := session.SessionEvent{
			Event: session.EventSessionUpdate,
			Data:  summary,
		}
		if wireBytes, err := sessionEv.FormatSSE(); err == nil {
			if _, err := w.Write(wireBytes); err != nil {
				return
			}
			flusher.Flush()
		}
	}

	// 5. Initial Snapshot: match_update if active match exists
	var currentMatch *playertrack.CurrentMatchResponse
	if d.playerTracker != nil {
		currentMatch = d.playerTracker.GetCurrentMatch()
	}
	if (currentMatch == nil || !currentMatch.ActiveMatch) && summary != nil && summary.ActiveMatch != nil {
		currentMatch = summary.ActiveMatch
	}

	if currentMatch != nil && currentMatch.ActiveMatch {
		matchEv := session.SessionEvent{
			Event: session.EventMatchUpdate,
			Data:  currentMatch,
		}
		if wireBytes, err := matchEv.FormatSSE(); err == nil {
			if _, err := w.Write(wireBytes); err != nil {
				return
			}
			flusher.Flush()
		}
	}

	// 6. 15-second keepalive ping ticker
	keepaliveTicker := time.NewTicker(15 * time.Second)
	defer keepaliveTicker.Stop()

	keepaliveBytes := []byte(": keepalive\n\n")

	for {
		select {
		case <-r.Context().Done():
			// Client disconnected (tab closed, page navigated)
			return

		case <-keepaliveTicker.C:
			// Send standard SSE keepalive comment
			if _, err := w.Write(keepaliveBytes); err != nil {
				return
			}
			flusher.Flush()

		case ev, ok := <-eventCh:
			if !ok {
				// Event broadcaster closed
				return
			}
			wireBytes, err := ev.FormatSSE()
			if err != nil {
				d.logger.Warn("failed to format SSE event", slog.Any("error", err))
				continue
			}
			if _, err := w.Write(wireBytes); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
