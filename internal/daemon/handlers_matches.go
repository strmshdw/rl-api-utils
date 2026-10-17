package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

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

	limit := 50
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
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
	if rawOffset := r.URL.Query().Get("offset"); rawOffset != "" {
		if parsed, err := strconv.Atoi(rawOffset); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	playlist := 0
	if rawPlaylist := r.URL.Query().Get("playlist"); rawPlaylist != "" {
		if parsed, err := strconv.Atoi(rawPlaylist); err == nil && parsed > 0 {
			playlist = parsed
		}
	}

	query := strings.TrimSpace(r.URL.Query().Get("query"))
	result := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("result")))

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
		Playlist: playlist,
		Query:    query,
		Result:   result,
		Limit:    limit,
		Offset:   offset,
	}

	matches, total, err := d.stateStore.ListHistoricalMatches(r.Context(), filter)
	if err != nil {
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

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		d.logger.Warn("error encoding matches response", slog.Any("error", err))
	}
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

		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(hist); err != nil {
			d.logger.Warn("error encoding match detail response", slog.Any("error", err))
		}
		return
	}

	matches, _, err := d.stateStore.ListHistoricalMatches(r.Context(), storage.MatchFilter{
		Query: guid,
		Limit: 1,
	})
	if err != nil || len(matches) == 0 {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "match not found"})
		return
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(matches[0]); err != nil {
		d.logger.Warn("error encoding match detail response", slog.Any("error", err))
	}
}

// handleTriggerBallchasingSync handles POST /api/sync/ballchasing.
func (d *Daemon) handleTriggerBallchasingSync(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

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

	go func() {
		_, err := d.historySyncer.Sync(context.Background())
		if err != nil {
			d.logger.Error("background ballchasing historical sync failed", slog.Any("error", err))
		}
	}()

	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "started",
		"message": "Ballchasing historical sync started in background",
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
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(prog); err != nil {
		d.logger.Warn("error encoding sync status response", slog.Any("error", err))
	}
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

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(MMRHistoryResponse{
		PlaylistID: playlistID,
		History:    recs,
		Total:      len(recs),
	})
}
