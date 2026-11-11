package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/dank/rl-api-utils/internal/session"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/syncer"
)

// MatchesResponse represents the paginated envelope returned by GET /api/matches.
type MatchesResponse struct {
	Matches        []*storage.HistoricalMatchRecord `json:"matches"`
	Total          int                              `json:"total"`
	Limit          int                              `json:"limit"`
	Offset         int                              `json:"offset"`
	LocalPlayerIDs []string                         `json:"local_player_ids,omitempty"`
}

// handleListMatches handles GET /api/matches and GET /api/historical-matches.
func (d *Daemon) handleListMatches(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	q := r.URL.Query()
	limit := 50
	if rawLimit := q.Get("limit"); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil {
			if parsed < 1 {
				limit = 1
			} else if parsed > 100 {
				limit = 100
			} else {
				limit = parsed
			}
		}
	}

	offset := 0
	if rawOffset := q.Get("offset"); rawOffset != "" {
		if parsed, err := strconv.Atoi(rawOffset); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	playlist := 0
	rawPlaylist := strings.TrimSpace(q.Get("playlist"))
	if rawPlaylist != "" {
		if parsed, err := strconv.Atoi(rawPlaylist); err == nil && parsed > 0 {
			playlist = parsed
		}
	}

	query := strings.TrimSpace(q.Get("query"))
	search := strings.TrimSpace(q.Get("search"))
	if search == "" {
		search = strings.TrimSpace(q.Get("q"))
	}
	result := strings.ToLower(strings.TrimSpace(q.Get("result")))
	status := strings.TrimSpace(q.Get("status"))
	sortBy := strings.TrimSpace(q.Get("sort_by"))
	if sortBy == "" {
		sortBy = strings.TrimSpace(q.Get("sort"))
	}
	sortDir := strings.TrimSpace(q.Get("sort_dir"))
	if sortDir == "" {
		sortDir = strings.TrimSpace(q.Get("order"))
	}
	fromUnix := storage.ParseDateFilter(q.Get("from"), false)
	toUnix := storage.ParseDateFilter(q.Get("to"), true)

	var localIDs []string
	if d.stateStore != nil {
		if ids, err := d.stateStore.GetLocalPlayerIDs(r.Context()); err == nil {
			localIDs = ids
		}
	}
	if len(localIDs) == 0 && d.sessionTracker != nil {
		localIDs = d.sessionTracker.GetLocalPlayerIDs()
	}

	if d.stateStore == nil {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(MatchesResponse{
			Matches:        make([]*storage.HistoricalMatchRecord, 0),
			Total:          0,
			Limit:          limit,
			Offset:         offset,
			LocalPlayerIDs: localIDs,
		})
		return
	}

	filter := storage.MatchFilter{
		Playlist:       playlist,
		PlaylistFilter: rawPlaylist,
		FromUnix:       fromUnix,
		ToUnix:         toUnix,
		Status:         status,
		Query:          query,
		Search:         search,
		Result:         result,
		SortBy:         sortBy,
		SortDir:        sortDir,
		Limit:          limit,
		Offset:         offset,
	}

	matches, total, err := d.stateStore.ListHistoricalMatches(r.Context(), filter)
	if err != nil {
		if isClientDisconnect(r.Context(), err) {
			d.logger.Debug("historical matches query canceled by client", slog.Any("error", err))
			return
		}
		d.logger.Error("failed to list historical matches", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to list historical matches"})
		return
	}

	if matches == nil {
		matches = make([]*storage.HistoricalMatchRecord, 0)
	}

	resp := MatchesResponse{
		Matches:        matches,
		Total:          total,
		Limit:          limit,
		Offset:         offset,
		LocalPlayerIDs: localIDs,
	}

	d.writeJSONResponse(w, r, http.StatusOK, resp, "error encoding matches response")
}

// handleGetMatch handles GET /api/matches/{guid}.
func (d *Daemon) handleGetMatch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	rawGUID := r.PathValue("guid")
	if rawGUID == "" {
		rawGUID = strings.TrimPrefix(r.URL.Path, "/api/matches/")
		rawGUID = strings.TrimPrefix(rawGUID, "/api/historical-matches/")
	}
	guid := strings.Trim(rawGUID, "/")
	if guid == "" || d.stateStore == nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "match not found"})
		return
	}

	m, err := d.stateStore.GetMatch(r.Context(), guid)
	if err == nil && m != nil {
		hist := &storage.HistoricalMatchRecord{
			MatchRecord: *m,
			Result:      "unknown",
		}
		if m.ReplayStatsJSON != "" {
			var st storage.ReplayMatchStats
			if err := json.Unmarshal([]byte(m.ReplayStatsJSON), &st); err == nil {
				hist.Stats = &st
			}
		}
		if enricher, ok := d.stateStore.(interface {
			EnrichMatchStatsWithPlayers(ctx context.Context, matchGUID string, playlist int, mr *storage.MatchRecord, st *storage.ReplayMatchStats) *storage.ReplayMatchStats
		}); ok {
			hist.Stats = enricher.EnrichMatchStatsWithPlayers(r.Context(), guid, m.Playlist, m, hist.Stats)
		}
		// Dynamically resolve local team
		localTeam := -1
		if mps, err := d.stateStore.GetMatchPlayers(r.Context(), guid); err == nil && len(mps) > 0 {
			for _, mp := range mps {
				if mp.IsLocal {
					localTeam = mp.TeamNum
					break
				}
			}
			if localTeam < 0 {
				if localIDs, err := d.stateStore.GetLocalPlayerIDs(r.Context()); err == nil && len(localIDs) > 0 {
					localSet := make(map[string]bool, len(localIDs))
					for _, id := range localIDs {
						localSet[id] = true
					}
					for _, mp := range mps {
						if localSet[mp.PlayerID] {
							localTeam = mp.TeamNum
							break
						}
					}
				}
			}
		}

		if localTeam < 0 && hist.Stats != nil && len(hist.Stats.Players) > 0 {
			if localIDs, err := d.stateStore.GetLocalPlayerIDs(r.Context()); err == nil && len(localIDs) > 0 {
				localSet := make(map[string]bool, len(localIDs))
				for _, id := range localIDs {
					localSet[id] = true
				}
				for _, p := range hist.Stats.Players {
					fullID := fmt.Sprintf("%s|%s|0", p.Platform, p.OnlineID)
					if localSet[fullID] || localSet[p.OnlineID] {
						localTeam = p.Team
						break
					}
				}
			}
		}

		// Compute hist.Result relative to local team and m.WinnerTeam, matching ListHistoricalMatches
		if localTeam == 0 {
			if m.BlueScore > m.OrangeScore {
				hist.Result = "victory"
			} else if m.OrangeScore > m.BlueScore {
				hist.Result = "defeat"
			} else if m.WinnerTeam != nil && *m.WinnerTeam != -1 {
				if *m.WinnerTeam == 0 {
					hist.Result = "victory"
				} else {
					hist.Result = "defeat"
				}
			} else if (m.BlueScore == m.OrangeScore && (m.BlueScore > 0 || m.OrangeScore > 0)) || (m.WinnerTeam != nil && *m.WinnerTeam == -1) {
				hist.Result = "draw"
			} else {
				hist.Result = "draw"
			}
		} else if localTeam == 1 {
			if m.OrangeScore > m.BlueScore {
				hist.Result = "victory"
			} else if m.BlueScore > m.OrangeScore {
				hist.Result = "defeat"
			} else if m.WinnerTeam != nil && *m.WinnerTeam != -1 {
				if *m.WinnerTeam == 1 {
					hist.Result = "victory"
				} else {
					hist.Result = "defeat"
				}
			} else if (m.BlueScore == m.OrangeScore && (m.BlueScore > 0 || m.OrangeScore > 0)) || (m.WinnerTeam != nil && *m.WinnerTeam == -1) {
				hist.Result = "draw"
			} else {
				hist.Result = "draw"
			}
		} else {
			if m.BlueScore > m.OrangeScore {
				hist.Result = "victory"
			} else if m.OrangeScore > m.BlueScore {
				hist.Result = "defeat"
			} else if m.WinnerTeam != nil && *m.WinnerTeam != -1 {
				if *m.WinnerTeam == 0 {
					hist.Result = "victory"
				} else {
					hist.Result = "defeat"
				}
			} else if (m.BlueScore == m.OrangeScore && (m.BlueScore > 0 || m.OrangeScore > 0)) || (m.WinnerTeam != nil && *m.WinnerTeam == -1) {
				hist.Result = "draw"
			} else {
				hist.Result = "draw"
			}
		}

		d.writeJSONResponse(w, r, http.StatusOK, hist, "error encoding match detail response")
		return
	}

	if err != nil && isClientDisconnect(r.Context(), err) {
		return
	}

	matches, _, err := d.stateStore.ListHistoricalMatches(r.Context(), storage.MatchFilter{
		Query: guid,
		Limit: 1,
	})
	if err != nil && isClientDisconnect(r.Context(), err) {
		return
	}
	if err != nil || len(matches) == 0 {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "match not found"})
		return
	}

	d.writeJSONResponse(w, r, http.StatusOK, matches[0], "error encoding match detail response")
}

// handleTriggerBallchasingSync handles POST /api/sync/ballchasing.
func (d *Daemon) handleTriggerBallchasingSync(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if !isAllowedMutatingOrigin(r) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "forbidden: invalid origin"})
		return
	}

	if d.historySyncer == nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "ballchasing historical syncer not configured"})
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
	go func() {
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
	}()

	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "started",
		"message": "Ballchasing historical sync started in background",
	})
}

// handlePruneMatches handles POST /api/matches/prune.
func (d *Daemon) handlePruneMatches(w http.ResponseWriter, r *http.Request) {
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

	var payload struct {
		Count       int `json:"count"`
		ReplayLimit int `json:"replay_limit"`
		NewLimit    int `json:"new_limit"`
	}
	if r.Body != nil {
		defer r.Body.Close()
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&payload); err != nil && !errors.Is(err, io.EOF) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
			return
		}
	}

	effectiveNewLimit := payload.ReplayLimit
	if effectiveNewLimit <= 0 && payload.NewLimit > 0 {
		effectiveNewLimit = payload.NewLimit
	}
	if effectiveNewLimit > 0 {
		d.setReplayLimit(effectiveNewLimit)
	}

	pruneCount := payload.Count
	if pruneCount <= 0 && effectiveNewLimit <= 0 {
		pruneCount = 25
	}

	pruned := 0
	if pruneCount > 0 && d.stateStore != nil {
		if pruner, ok := d.stateStore.(storage.MatchPruner); ok {
			var err error
			pruned, err = pruner.PruneOldestMatches(r.Context(), pruneCount)
			if err != nil {
				d.logger.Error("failed to prune oldest matches", slog.Any("error", err))
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to prune matches"})
				return
			}
		}
	}

	st := d.buildStatusResponse(r.Context())
	if d.sessionTracker != nil && d.sessionTracker.Broadcaster() != nil {
		summary := d.enrichSessionTelemetry(r.Context(), d.sessionTracker.GetSessionSummary())
		d.sessionTracker.Broadcaster().Broadcast(session.SessionEvent{
			Event: session.EventSessionUpdate,
			Data:  summary,
		})
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":              "ok",
		"pruned_count":        pruned,
		"replay_count":        st.ReplayCount,
		"replay_limit":        st.ReplayLimit,
		"replay_usage_pct":    st.ReplayUsagePct,
		"replay_limit_status": st.ReplayLimitStatus,
	})
}

// handleBallchasingSyncStatus handles GET /api/sync/ballchasing/status.
func (d *Daemon) handleBallchasingSyncStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if d.historySyncer == nil {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(syncer.BallchasingHistorySyncProgress{
			Running: false,
		})
		return
	}

	prog := d.historySyncer.Progress()
	d.writeJSONResponse(w, r, http.StatusOK, prog, "error encoding sync status response")
}

// handleRetryMatch handles POST /api/matches/{guid}/retry.
func (d *Daemon) handleRetryMatch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if !isAllowedMutatingOrigin(r) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "forbidden: invalid origin"})
		return
	}

	rawGUID := r.PathValue("guid")
	if rawGUID == "" {
		p := strings.TrimPrefix(r.URL.Path, "/api/matches/")
		rawGUID = strings.TrimSuffix(p, "/retry")
	}
	guid, err := url.PathUnescape(strings.Trim(rawGUID, "/"))
	if err != nil || guid == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid match guid"})
		return
	}

	if d.stateStore == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "storage unavailable"})
		return
	}

	if err := d.stateStore.ResetMatchRetry(r.Context(), guid); err != nil {
		if errors.Is(err, storage.ErrMatchNotFound) {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "match not found"})
			return
		}
		if errors.Is(err, storage.ErrInvalidGUID) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid match guid"})
			return
		}
		d.logger.Error("failed to reset match retry", slog.String("guid", guid), slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to reset match retry"})
		return
	}

	_ = d.TriggerSync(r.Context(), "retry_match_"+guid)

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":     "ok",
		"message":    "match retry queued",
		"match_guid": guid,
	})
}

// MMRHistoryResponse represents the payload returned by GET /api/mmr-history.
type MMRHistoryResponse struct {
	PlaylistID int                         `json:"playlist_id"`
	History    []*storage.MMRHistoryRecord `json:"history"`
	Total      int                         `json:"total"`
}

// handleListMMRHistory handles GET /api/mmr-history.
func (d *Daemon) handleListMMRHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	playlistID := 0
	if rawPlaylist := r.URL.Query().Get("playlist_id"); rawPlaylist != "" {
		if p, err := strconv.Atoi(rawPlaylist); err == nil && p > 0 {
			playlistID = p
		}
	} else if rawP := r.URL.Query().Get("playlist"); rawP != "" {
		if p, err := strconv.Atoi(rawP); err == nil && p > 0 {
			playlistID = p
		}
	}

	limit := 50
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		if p, err := strconv.Atoi(rawLimit); err == nil {
			if p < 1 {
				limit = 1
			} else if p > 200 {
				limit = 200
			} else {
				limit = p
			}
		}
	}

	order := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("order")))
	if order == "" {
		order = "asc" // default chronological for charts
	}

	if d.stateStore == nil {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(MMRHistoryResponse{
			PlaylistID: playlistID,
			History:    make([]*storage.MMRHistoryRecord, 0),
			Total:      0,
		})
		return
	}

	recs, err := d.stateStore.ListMMRHistory(r.Context(), playlistID, limit)
	if err != nil {
		if isClientDisconnect(r.Context(), err) {
			d.logger.Debug("mmr history query canceled by client", slog.Any("error", err))
			return
		}
		d.logger.Error("failed to query mmr history", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to query mmr history"})
		return
	}

	if recs == nil {
		recs = make([]*storage.MMRHistoryRecord, 0)
	}

	// ListMMRHistory in storage returns descending order (most recent first).
	// If order is asc (default), reverse to chronological order for charts.
	if order == "asc" && len(recs) > 1 {
		for i, j := 0, len(recs)-1; i < j; i, j = i+1, j-1 {
			recs[i], recs[j] = recs[j], recs[i]
		}
	}

	d.writeJSONResponse(w, r, http.StatusOK, MMRHistoryResponse{
		PlaylistID: playlistID,
		History:    recs,
		Total:      len(recs),
	}, "error encoding mmr history response")
}

// handleGetSyncConfig handles GET /api/sync/config, returning the Rocket League 20-game
// hosted replay queue configuration and current pending count.
func (d *Daemon) handleGetSyncConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	st := d.buildStatusResponse(r.Context())
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"pending_count":         st.PendingCount,
		"trigger_threshold":     st.TriggerThreshold,
		"force_sync_on_trigger": st.ForceSyncOnTrigger,
		"enable_toast":          st.EnableToast,
		"auto_sync_on_exit":     st.AutoSyncOnExit,
		"max_hosted_replays":    st.MaxHostedReplays,
		"game_connected":        st.GameConnected,
	})
}

// handleUpdateSyncConfig handles POST /api/sync/config, updating the configurable game threshold
// for notifications (1..20) and automatic replay sync/download behavior.
func (d *Daemon) handleUpdateSyncConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPatch {
		w.Header().Set("Allow", "POST, PATCH")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	if !isAllowedMutatingOrigin(r) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "forbidden: invalid origin"})
		return
	}

	var payload struct {
		TriggerThreshold   *int  `json:"trigger_threshold"`
		ForceSyncOnTrigger *bool `json:"force_sync_on_trigger"`
		EnableToast        *bool `json:"enable_toast"`
		AutoSyncOnExit     *bool `json:"auto_sync_on_exit"`
	}
	if r.Body != nil {
		defer r.Body.Close()
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&payload); err != nil && !errors.Is(err, io.EOF) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
			return
		}
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

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":                "ok",
		"pending_count":         st.PendingCount,
		"trigger_threshold":     st.TriggerThreshold,
		"force_sync_on_trigger": st.ForceSyncOnTrigger,
		"enable_toast":          st.EnableToast,
		"auto_sync_on_exit":     st.AutoSyncOnExit,
		"max_hosted_replays":    st.MaxHostedReplays,
	})
}

// handleManualSyncNow handles POST /api/sync/now, immediately triggering a replay download & sync pass
// from Rocket League's 20-game hosted replay buffer so no replays are lost.
func (d *Daemon) handleManualSyncNow(w http.ResponseWriter, r *http.Request) {
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

	if err := d.TriggerSync(d.daemonContext(), "manual_ui_sync_now"); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	st := d.buildStatusResponse(r.Context())
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":             "sync_triggered",
		"pending_count":      st.PendingCount,
		"max_hosted_replays": st.MaxHostedReplays,
	})
}
