package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/dank/rl-api-utils/internal/session"
	"github.com/dank/rl-api-utils/internal/syncer"
)

func (d *Daemon) handleSync(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !isAllowedMutatingOrigin(r) {
			writeJSONError(w, http.StatusForbidden, "forbidden: invalid origin")
			return
		}
		d.logger.Info("received manual trigger request via local HTTP endpoint", slog.String("remote", r.RemoteAddr))
		if err := d.TriggerSync(ctx, "manual_http_request"); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"error":%q}`, err.Error())))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"sync_triggered"}`))
	}
}

// handleTriggerBallchasingSync handles POST /api/sync/ballchasing.
func (d *Daemon) handleTriggerBallchasingSync(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if !isAllowedMutatingOrigin(r) {
		writeJSONError(w, http.StatusForbidden, "forbidden: invalid origin")
		return
	}

	if d.historySyncer == nil {
		writeJSONError(w, http.StatusBadRequest, "ballchasing historical syncer not configured")
		return
	}

	prog := d.historySyncer.Progress()
	if prog.Running {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":   "in_progress",
			"message":  "Ballchasing historical sync is already running",
			"progress": prog,
		})
		return
	}

	syncCtx := d.daemonContext()
	d.wg.Add(1)
	go d.runBackgroundHistorySync(syncCtx)

	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "started",
		"message": "Ballchasing historical sync started in background",
	})
}

func (d *Daemon) runBackgroundHistorySync(syncCtx context.Context) {
	defer d.wg.Done()
	if d.sessionTracker != nil && d.sessionTracker.Broadcaster() != nil {
		d.sessionTracker.Broadcaster().Broadcast(session.SessionEvent{
			Event: session.EventSyncProgress,
			Data: map[string]any{
				"status":   "started",
				"running":  true,
				"progress": d.historySyncer.Progress(),
			},
		})
	}
	if poller, ok := d.syncer.(StatsPoller); ok && poller != nil {
		_ = poller.PollAndSyncPendingStats(syncCtx)
	}
	if syncCtx.Err() != nil {
		return
	}
	res, err := d.historySyncer.Sync(syncCtx)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || syncCtx.Err() != nil {
			return
		}
		d.logger.Error("background ballchasing historical sync failed", slog.Any("error", err))
	}
	if syncCtx.Err() != nil {
		return
	}
	if d.sessionTracker != nil && d.sessionTracker.Broadcaster() != nil {
		statusStr := "completed"
		if err != nil {
			statusStr = "failed"
		}
		d.sessionTracker.Broadcaster().Broadcast(session.SessionEvent{
			Event: session.EventSyncProgress,
			Data: map[string]any{
				"status":   statusStr,
				"running":  false,
				"progress": res,
			},
		})
		summary := d.enrichSessionTelemetry(syncCtx, d.sessionTracker.GetSessionSummary())
		d.sessionTracker.Broadcaster().Broadcast(session.SessionEvent{
			Event: session.EventSessionUpdate,
			Data:  summary,
		})
	}
}

// handleBallchasingSyncStatus handles GET /api/sync/ballchasing/status.
func (d *Daemon) handleBallchasingSyncStatus(w http.ResponseWriter, r *http.Request) {
	if d.historySyncer == nil {
		d.writeJSONResponse(w, r, http.StatusOK, syncer.BallchasingHistorySyncProgress{
			Running: false,
		}, "error encoding sync status response")
		return
	}

	prog := d.historySyncer.Progress()
	d.writeJSONResponse(w, r, http.StatusOK, prog, "error encoding sync status response")
}

// handleGetSyncConfig handles GET /api/sync/config, returning the Rocket League 20-game
// hosted replay queue configuration and current pending count.
func (d *Daemon) handleGetSyncConfig(w http.ResponseWriter, r *http.Request) {
	st := d.buildStatusResponse(r.Context())
	d.writeJSONResponse(w, r, http.StatusOK, map[string]any{
		"pending_count":         st.PendingCount,
		"trigger_threshold":     st.TriggerThreshold,
		"force_sync_on_trigger": st.ForceSyncOnTrigger,
		"enable_toast":          st.EnableToast,
		"auto_sync_on_exit":     st.AutoSyncOnExit,
		"max_hosted_replays":    st.MaxHostedReplays,
		"game_connected":        st.GameConnected,
	}, "error encoding sync config response")
}

// handleUpdateSyncConfig handles POST /api/sync/config, updating the configurable game threshold
// for notifications (1..20) and automatic replay sync/download behavior.
func (d *Daemon) handleUpdateSyncConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPatch {
		w.Header().Set("Allow", "POST, PATCH")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	if !isAllowedMutatingOrigin(r) {
		writeJSONError(w, http.StatusForbidden, "forbidden: invalid origin")
		return
	}

	var payload struct {
		TriggerThreshold   *int  `json:"trigger_threshold"`
		ForceSyncOnTrigger *bool `json:"force_sync_on_trigger"`
		EnableToast        *bool `json:"enable_toast"`
		AutoSyncOnExit     *bool `json:"auto_sync_on_exit"`
	}
	if !decodeJSONBody(w, r, &payload, true) {
		return
	}

	threshold := 0
	if payload.TriggerThreshold != nil {
		t := *payload.TriggerThreshold
		if t < 1 {
			t = 1
		} else if t > 20 {
			t = 20
		}
		threshold = t
	}

	d.mu.Lock()
	if d.cfg != nil {
		if threshold > 0 {
			d.cfg.StatsAPI.TriggerThreshold = threshold
		}
		if payload.ForceSyncOnTrigger != nil {
			d.cfg.StatsAPI.ForceSyncOnTrigger = *payload.ForceSyncOnTrigger
		}
		if payload.EnableToast != nil {
			d.cfg.StatsAPI.EnableToast = *payload.EnableToast
		}
		if payload.AutoSyncOnExit != nil {
			d.cfg.StatsAPI.AutoSyncOnExit = *payload.AutoSyncOnExit
		}
	}
	d.mu.Unlock()

	if d.statsTracker != nil {
		d.statsTracker.UpdateConfig(threshold, payload.ForceSyncOnTrigger, payload.EnableToast)
	}

	st := d.buildStatusResponse(r.Context())
	if d.sessionTracker != nil && d.sessionTracker.Broadcaster() != nil {
		summary := d.enrichSessionTelemetry(r.Context(), d.sessionTracker.GetSessionSummary())
		d.sessionTracker.Broadcaster().Broadcast(session.SessionEvent{
			Event: session.EventSessionUpdate,
			Data:  summary,
		})
	}

	d.writeJSONResponse(w, r, http.StatusOK, map[string]any{
		"status":                "ok",
		"pending_count":         st.PendingCount,
		"trigger_threshold":     st.TriggerThreshold,
		"force_sync_on_trigger": st.ForceSyncOnTrigger,
		"enable_toast":          st.EnableToast,
		"auto_sync_on_exit":     st.AutoSyncOnExit,
		"max_hosted_replays":    st.MaxHostedReplays,
	}, "error encoding updated sync config response")
}

// handleManualSyncNow handles POST /api/sync/now, immediately triggering a replay download & sync pass
// from Rocket League's 20-game hosted replay buffer so no replays are lost.
func (d *Daemon) handleManualSyncNow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	if !isAllowedMutatingOrigin(r) {
		writeJSONError(w, http.StatusForbidden, "forbidden: invalid origin")
		return
	}

	if err := d.TriggerSync(d.daemonContext(), "manual_ui_sync_now"); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	st := d.buildStatusResponse(r.Context())
	d.writeJSONResponse(w, r, http.StatusOK, map[string]any{
		"status":             "sync_triggered",
		"pending_count":      st.PendingCount,
		"max_hosted_replays": st.MaxHostedReplays,
	}, "error encoding manual sync response")
}
