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

	"github.com/dank/rl-api-utils/internal/session"
	"github.com/dank/rl-api-utils/internal/storage"
)

// MatchesResponse represents the paginated envelope returned by GET /api/matches.
type MatchesResponse struct {
	Matches        []*storage.HistoricalMatchRecord `json:"matches"`
	Total          int                              `json:"total"`
	Limit          int                              `json:"limit"`
	Offset         int                              `json:"offset"`
	LocalPlayerIDs []string                         `json:"local_player_ids,omitempty"`
}

// MMRHistoryResponse represents the payload returned by GET /api/mmr-history.
type MMRHistoryResponse struct {
	PlaylistID int                         `json:"playlist_id"`
	History    []*storage.MMRHistoryRecord `json:"history"`
	Total      int                         `json:"total"`
}

type matchStatsEnricher interface {
	EnrichMatchStatsWithPlayers(ctx context.Context, matchGUID string, playlist int, mr *storage.MatchRecord, st *storage.ReplayMatchStats) *storage.ReplayMatchStats
}

func parseLimitOffset(q url.Values, defaultLimit, maxLimit int) (int, int) {
	limit := defaultLimit
	if rawLimit := q.Get("limit"); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil {
			if parsed < 1 {
				limit = 1
			} else if parsed > maxLimit {
				limit = maxLimit
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
	return limit, offset
}

func computeMatchOutcome(m *storage.MatchRecord, localTeam int) string {
	team := 0
	if localTeam == 1 {
		team = 1
	}
	localScore, oppScore := m.BlueScore, m.OrangeScore
	if team == 1 {
		localScore, oppScore = m.OrangeScore, m.BlueScore
	}
	if localScore > oppScore {
		return "victory"
	}
	if oppScore > localScore {
		return "defeat"
	}
	if m.WinnerTeam != nil && *m.WinnerTeam != -1 {
		if *m.WinnerTeam == team {
			return "victory"
		}
		return "defeat"
	}
	return "draw"
}

func (d *Daemon) resolveLocalPlayerIDs(ctx context.Context) []string {
	var localIDs []string
	if d.stateStore != nil {
		if ids, err := d.stateStore.GetLocalPlayerIDs(ctx); err == nil {
			localIDs = ids
		}
	}
	if len(localIDs) == 0 && d.sessionTracker != nil {
		localIDs = d.sessionTracker.GetLocalPlayerIDs()
	}
	return localIDs
}

func (d *Daemon) resolveMatchLocalTeam(ctx context.Context, guid string, stats *storage.ReplayMatchStats) int {
	var localSet map[string]bool
	loadLocalSet := func() map[string]bool {
		if localSet != nil {
			return localSet
		}
		localIDs, err := d.stateStore.GetLocalPlayerIDs(ctx)
		if err != nil || len(localIDs) == 0 {
			localSet = map[string]bool{}
			return localSet
		}
		localSet = make(map[string]bool, len(localIDs))
		for _, id := range localIDs {
			localSet[id] = true
		}
		return localSet
	}

	if mps, err := d.stateStore.GetMatchPlayers(ctx, guid); err == nil && len(mps) > 0 {
		for _, mp := range mps {
			if mp.IsLocal {
				return mp.TeamNum
			}
		}
		if set := loadLocalSet(); len(set) > 0 {
			for _, mp := range mps {
				if set[mp.PlayerID] {
					return mp.TeamNum
				}
			}
		}
	}

	if stats != nil && len(stats.Players) > 0 {
		if set := loadLocalSet(); len(set) > 0 {
			for _, p := range stats.Players {
				fullID := fmt.Sprintf("%s|%s|0", p.Platform, p.OnlineID)
				if set[fullID] || set[p.OnlineID] {
					return p.Team
				}
			}
		}
	}
	return -1
}

// handleListMatches handles GET /api/matches and GET /api/historical-matches.
func (d *Daemon) handleListMatches(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, offset := parseLimitOffset(q, 50, 100)

	playlist := 0
	rawPlaylist := strings.TrimSpace(q.Get("playlist"))
	if rawPlaylist != "" {
		if parsed, err := strconv.Atoi(rawPlaylist); err == nil && parsed > 0 {
			playlist = parsed
		}
	}

	search := strings.TrimSpace(q.Get("search"))
	if search == "" {
		search = strings.TrimSpace(q.Get("q"))
	}
	sortBy := strings.TrimSpace(q.Get("sort_by"))
	if sortBy == "" {
		sortBy = strings.TrimSpace(q.Get("sort"))
	}
	sortDir := strings.TrimSpace(q.Get("sort_dir"))
	if sortDir == "" {
		sortDir = strings.TrimSpace(q.Get("order"))
	}

	localIDs := d.resolveLocalPlayerIDs(r.Context())
	if d.stateStore == nil {
		d.writeJSONResponse(w, r, http.StatusOK, MatchesResponse{
			Matches:        make([]*storage.HistoricalMatchRecord, 0),
			Total:          0,
			Limit:          limit,
			Offset:         offset,
			LocalPlayerIDs: localIDs,
		}, "error encoding empty matches response")
		return
	}

	filter := storage.MatchFilter{
		Playlist:       playlist,
		PlaylistFilter: rawPlaylist,
		FromUnix:       storage.ParseDateFilter(q.Get("from"), false),
		ToUnix:         storage.ParseDateFilter(q.Get("to"), true),
		Status:         strings.TrimSpace(q.Get("status")),
		Query:          strings.TrimSpace(q.Get("query")),
		Search:         search,
		Result:         strings.ToLower(strings.TrimSpace(q.Get("result"))),
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
		writeJSONError(w, http.StatusInternalServerError, "failed to list historical matches")
		return
	}

	if matches == nil {
		matches = make([]*storage.HistoricalMatchRecord, 0)
	}

	d.writeJSONResponse(w, r, http.StatusOK, MatchesResponse{
		Matches:        matches,
		Total:          total,
		Limit:          limit,
		Offset:         offset,
		LocalPlayerIDs: localIDs,
	}, "error encoding matches response")
}

// handleGetMatch handles GET /api/matches/{guid}.
func (d *Daemon) handleGetMatch(w http.ResponseWriter, r *http.Request) {
	rawGUID := r.PathValue("guid")
	if rawGUID == "" {
		rawGUID = strings.TrimPrefix(r.URL.Path, "/api/matches/")
		rawGUID = strings.TrimPrefix(rawGUID, "/api/historical-matches/")
	}
	guid := strings.Trim(rawGUID, "/")
	if guid == "" || d.stateStore == nil {
		writeJSONError(w, http.StatusNotFound, "match not found")
		return
	}
	if strings.TrimSpace(guid) == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid match guid")
		return
	}

	m, err := d.stateStore.GetMatch(r.Context(), guid)
	if err != nil {
		if isClientDisconnect(r.Context(), err) {
			return
		}
		if errors.Is(err, storage.ErrInvalidGUID) {
			writeJSONError(w, http.StatusBadRequest, "invalid match guid")
			return
		}
		if errors.Is(err, storage.ErrMatchNotFound) {
			writeJSONError(w, http.StatusNotFound, "match not found")
			return
		}
		d.logger.Error("failed to retrieve match", slog.String("guid", guid), slog.Any("error", err))
		writeJSONError(w, http.StatusInternalServerError, "failed to retrieve match")
		return
	}
	if m == nil {
		writeJSONError(w, http.StatusNotFound, "match not found")
		return
	}

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
	if enricher, ok := d.stateStore.(matchStatsEnricher); ok {
		hist.Stats = enricher.EnrichMatchStatsWithPlayers(r.Context(), guid, m.Playlist, m, hist.Stats)
	}
	localTeam := d.resolveMatchLocalTeam(r.Context(), guid, hist.Stats)
	hist.Result = computeMatchOutcome(m, localTeam)

	d.writeJSONResponse(w, r, http.StatusOK, hist, "error encoding match detail response")
}

// handlePruneMatches handles POST /api/matches/prune.
func (d *Daemon) handlePruneMatches(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	if !isAllowedMutatingOrigin(r) {
		writeJSONError(w, http.StatusForbidden, "forbidden: invalid origin")
		return
	}

	var payload struct {
		Count       int `json:"count"`
		ReplayLimit int `json:"replay_limit"`
		NewLimit    int `json:"new_limit"`
	}
	if !decodeJSONBody(w, r, &payload, true) {
		return
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
				writeJSONError(w, http.StatusInternalServerError, "failed to prune matches")
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

	d.writeJSONResponse(w, r, http.StatusOK, map[string]any{
		"status":              "ok",
		"pruned_count":        pruned,
		"replay_count":        st.ReplayCount,
		"replay_limit":        st.ReplayLimit,
		"replay_usage_pct":    st.ReplayUsagePct,
		"replay_limit_status": st.ReplayLimitStatus,
	}, "error encoding prune response")
}

// handleRetryMatch handles POST /api/matches/{guid}/retry.
func (d *Daemon) handleRetryMatch(w http.ResponseWriter, r *http.Request) {
	if !isAllowedMutatingOrigin(r) {
		writeJSONError(w, http.StatusForbidden, "forbidden: invalid origin")
		return
	}

	rawGUID := r.PathValue("guid")
	if rawGUID == "" {
		p := strings.TrimPrefix(r.URL.Path, "/api/matches/")
		rawGUID = strings.TrimSuffix(p, "/retry")
	}
	guid, err := url.PathUnescape(strings.Trim(rawGUID, "/"))
	if err != nil || guid == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid match guid")
		return
	}

	if d.stateStore == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	if err := d.stateStore.ResetMatchRetry(r.Context(), guid); err != nil {
		if errors.Is(err, storage.ErrMatchNotFound) {
			writeJSONError(w, http.StatusNotFound, "match not found")
			return
		}
		if errors.Is(err, storage.ErrInvalidGUID) {
			writeJSONError(w, http.StatusBadRequest, "invalid match guid")
			return
		}
		d.logger.Error("failed to reset match retry", slog.String("guid", guid), slog.Any("error", err))
		writeJSONError(w, http.StatusInternalServerError, "failed to reset match retry")
		return
	}

	_ = d.TriggerSync(r.Context(), "retry_match_"+guid)

	d.writeJSONResponse(w, r, http.StatusOK, map[string]string{
		"status":     "ok",
		"message":    "match retry queued",
		"match_guid": guid,
	}, "error encoding match retry response")
}

// handleListMMRHistory handles GET /api/mmr-history.
func (d *Daemon) handleListMMRHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	playlistID := 0
	if rawPlaylist := q.Get("playlist_id"); rawPlaylist != "" {
		if p, err := strconv.Atoi(rawPlaylist); err == nil && p > 0 {
			playlistID = p
		}
	} else if rawP := q.Get("playlist"); rawP != "" {
		if p, err := strconv.Atoi(rawP); err == nil && p > 0 {
			playlistID = p
		}
	}

	limit, _ := parseLimitOffset(q, 50, 200)

	order := strings.ToLower(strings.TrimSpace(q.Get("order")))
	if order == "" {
		order = "asc"
	}

	if d.stateStore == nil {
		d.writeJSONResponse(w, r, http.StatusOK, MMRHistoryResponse{
			PlaylistID: playlistID,
			History:    make([]*storage.MMRHistoryRecord, 0),
			Total:      0,
		}, "error encoding empty mmr history response")
		return
	}

	recs, err := d.stateStore.ListMMRHistory(r.Context(), playlistID, limit)
	if err != nil {
		if isClientDisconnect(r.Context(), err) {
			d.logger.Debug("mmr history query canceled by client", slog.Any("error", err))
			return
		}
		d.logger.Error("failed to query mmr history", slog.Any("error", err))
		writeJSONError(w, http.StatusInternalServerError, "failed to query mmr history")
		return
	}

	if recs == nil {
		recs = make([]*storage.MMRHistoryRecord, 0)
	}

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
