package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/syncer"
)

// MatchesResponse represents the paginated envelope returned by GET /api/matches.
type MatchesResponse struct {
	Matches []*storage.HistoricalMatchRecord `json:"matches"`
	Total   int                              `json:"total"`
	Limit   int                              `json:"limit"`
	Offset  int                              `json:"offset"`
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

	if d.stateStore == nil {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(MatchesResponse{
			Matches: make([]*storage.HistoricalMatchRecord, 0),
			Total:   0,
			Limit:   limit,
			Offset:  offset,
		})
		return
	}

	filter := storage.MatchFilter{
		Playlist: playlist,
		Query:    query,
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
		Matches: matches,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
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
		if m.BlueScore > m.OrangeScore {
			hist.Result = "victory"
		} else if m.OrangeScore > m.BlueScore {
			hist.Result = "defeat"
		} else if m.BlueScore == m.OrangeScore && (m.BlueScore > 0 || m.OrangeScore > 0) {
			hist.Result = "draw"
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
