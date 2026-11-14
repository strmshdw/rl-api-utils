package daemon

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/dank/rl-api-utils/internal/storage"
)

// PlayerSearchResponse represents the paginated envelope returned by GET /api/players.
type PlayerSearchResponse struct {
	Players    []*storage.PlayerSummary  `json:"players"`
	Total      int                       `json:"total"`
	Limit      int                       `json:"limit"`
	Offset     int                       `json:"offset"`
	Spotlights *storage.GlobalSpotlights `json:"spotlights,omitempty"`
}

// PlayerMatchesResponse represents the paginated envelope returned by GET /api/players/{id}/matches.
type PlayerMatchesResponse struct {
	Matches []*storage.PlayerMatchDetail `json:"matches"`
	Total   int                          `json:"total"`
	Limit   int                          `json:"limit"`
	Offset  int                          `json:"offset"`
}

func firstQueryParam(q url.Values, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(q.Get(k)); v != "" {
			return v
		}
	}
	return ""
}

func (d *Daemon) resolvePlayerRouteID(w http.ResponseWriter, r *http.Request, rawID string, strictUnescape bool) (string, bool) {
	rawID = strings.Trim(rawID, "/")
	if rawID == "" {
		writeJSONError(w, http.StatusNotFound, "player not found")
		return "", false
	}

	playerID, err := url.PathUnescape(rawID)
	if err != nil {
		if strictUnescape {
			writeJSONError(w, http.StatusBadRequest, "invalid player id")
			return "", false
		}
		playerID = rawID
	}

	playerID = strings.TrimSpace(playerID)
	if playerID == "" {
		writeJSONError(w, http.StatusNotFound, "player not found")
		return "", false
	}

	if d.cfg != nil && strings.TrimSpace(d.cfg.PlayerTracking.LocalPlayerID) != "" {
		if strings.EqualFold(playerID, "me") || strings.EqualFold(playerID, "you") {
			playerID = strings.TrimSpace(d.cfg.PlayerTracking.LocalPlayerID)
		}
	}
	return playerID, true
}

// handleListPlayers handles GET /players and GET /api/players.
func (d *Daemon) handleListPlayers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	isAPIRoute := strings.HasPrefix(r.URL.Path, "/api/")
	q := r.URL.Query()
	limit, offset := parseLimitOffset(q, 50, 100)

	if d.stateStore == nil {
		if isAPIRoute {
			d.writeJSONResponse(w, r, http.StatusOK, PlayerSearchResponse{
				Players: make([]*storage.PlayerSummary, 0),
				Total:   0,
				Limit:   limit,
				Offset:  offset,
			}, "error encoding empty player search response")
		} else {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("[]\n"))
		}
		return
	}

	minMatches := 2
	if rawMin := strings.TrimSpace(q.Get("min_matches")); rawMin != "" {
		if parsed, err := strconv.Atoi(rawMin); err == nil && parsed > 0 {
			minMatches = parsed
		}
	}
	minSynergyGames := 1
	if rawMinSyn := strings.TrimSpace(q.Get("min_synergy_games")); rawMinSyn != "" {
		if parsed, err := strconv.Atoi(rawMinSyn); err == nil && parsed > 0 {
			minSynergyGames = parsed
		}
	}
	minSynergyWR := 50.0
	if rawMinWR := strings.TrimSpace(q.Get("min_synergy_wr")); rawMinWR != "" {
		if parsed, err := strconv.ParseFloat(rawMinWR, 64); err == nil && parsed > 0 {
			minSynergyWR = parsed
		}
	}

	filter := storage.PlayerDirectoryFilter{
		Query:           firstQueryParam(q, "query", "q", "search"),
		Platform:        strings.TrimSpace(q.Get("platform")),
		Role:            strings.TrimSpace(q.Get("role")),
		Playlist:        strings.TrimSpace(q.Get("playlist")),
		FromUnix:        storage.ParseDateFilter(q.Get("from"), false),
		ToUnix:          storage.ParseDateFilter(q.Get("to"), true),
		MinMatches:      minMatches,
		MinSynergyGames: minSynergyGames,
		MinSynergyWR:    minSynergyWR,
		Sort:            firstQueryParam(q, "sort_by", "sort"),
		Order:           firstQueryParam(q, "sort_dir", "order"),
		Limit:           limit,
		Offset:          offset,
	}

	summaries, total, err := d.stateStore.FilterPlayerSummaries(r.Context(), filter)
	if err != nil {
		if isClientDisconnect(r.Context(), err) {
			d.logger.Debug("player search canceled by client", slog.Any("error", err))
			return
		}
		d.logger.Error("failed to query player summaries", slog.Any("error", err))
		writeJSONError(w, http.StatusInternalServerError, "failed to query players")
		return
	}

	if summaries == nil {
		summaries = make([]*storage.PlayerSummary, 0)
	}
	if isClientDisconnect(r.Context(), nil) {
		return
	}

	var spotlights *storage.GlobalSpotlights
	if isAPIRoute {
		sp, spErr := d.stateStore.GetPlayerSpotlights(r.Context())
		if spErr != nil && isClientDisconnect(r.Context(), spErr) {
			d.logger.Debug("player spotlights canceled by client", slog.Any("error", spErr))
			return
		}
		if spErr == nil {
			spotlights = sp
		}
	}

	if isAPIRoute {
		d.writeJSONResponse(w, r, http.StatusOK, PlayerSearchResponse{
			Players:    summaries,
			Total:      total,
			Limit:      limit,
			Offset:     offset,
			Spotlights: spotlights,
		}, "error encoding player search response")
		return
	}
	d.writeJSONResponse(w, r, http.StatusOK, summaries, "error encoding player summaries response")
}

// handleGetPlayerMatches handles GET /players/{id}/matches and GET /api/players/{id}/matches.
func (d *Daemon) handleGetPlayerMatches(w http.ResponseWriter, r *http.Request) {
	rawID := r.PathValue("id")
	if rawID == "" {
		p := strings.TrimPrefix(r.URL.Path, "/api/players/")
		p = strings.TrimPrefix(p, "/players/")
		rawID = strings.TrimSuffix(p, "/matches")
	}
	playerID, ok := d.resolvePlayerRouteID(w, r, rawID, false)
	if !ok {
		return
	}

	q := r.URL.Query()
	limit, offset := parseLimitOffset(q, 50, 100)

	var playlist int
	rawPlaylist := strings.TrimSpace(q.Get("playlist"))
	if rawPlaylist != "" {
		if parsed, err := strconv.Atoi(rawPlaylist); err == nil {
			playlist = parsed
		}
	}

	if d.stateStore == nil {
		d.writeJSONResponse(w, r, http.StatusOK, PlayerMatchesResponse{
			Matches: make([]*storage.PlayerMatchDetail, 0),
			Total:   0,
			Limit:   limit,
			Offset:  offset,
		}, "error encoding empty player matches response")
		return
	}

	filter := storage.PlayerMatchFilter{
		Role:             strings.TrimSpace(q.Get("role")),
		Playlist:         playlist,
		PlaylistFilter:   rawPlaylist,
		FromUnix:         storage.ParseDateFilter(q.Get("from"), false),
		ToUnix:           storage.ParseDateFilter(q.Get("to"), true),
		Status:           strings.TrimSpace(q.Get("status")),
		Result:           strings.TrimSpace(q.Get("result")),
		Search:           firstQueryParam(q, "search", "query", "q"),
		SortBy:           firstQueryParam(q, "sort_by", "sort"),
		SortDir:          firstQueryParam(q, "sort_dir", "order"),
		LocalPerspective: true,
		Limit:            limit,
		Offset:           offset,
	}

	matches, total, err := d.stateStore.ListPlayerMatches(r.Context(), playerID, filter)
	if err != nil {
		if isClientDisconnect(r.Context(), err) {
			d.logger.Debug("player matches query canceled by client", slog.String("player_id", playerID), slog.Any("error", err))
			return
		}
		d.logger.Error("failed to query player matches", slog.String("player_id", playerID), slog.Any("error", err))
		writeJSONError(w, http.StatusInternalServerError, "failed to query player matches")
		return
	}

	if matches == nil {
		matches = make([]*storage.PlayerMatchDetail, 0)
	}

	d.writeJSONResponse(w, r, http.StatusOK, PlayerMatchesResponse{
		Matches: matches,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
	}, "error encoding player matches response")
}

// handlePatchPlayer handles PATCH /players/{id...} and PATCH /api/players/{id...}.
func (d *Daemon) handlePatchPlayer(w http.ResponseWriter, r *http.Request) {
	if !isAllowedMutatingOrigin(r) {
		writeJSONError(w, http.StatusForbidden, "forbidden: invalid origin")
		return
	}

	rawID := r.PathValue("id")
	if rawID == "" {
		rawID = strings.TrimPrefix(r.URL.Path, "/api/players/")
		rawID = strings.TrimPrefix(rawID, "/players/")
	}
	playerID, ok := d.resolvePlayerRouteID(w, r, rawID, false)
	if !ok {
		return
	}

	if d.stateStore == nil {
		writeJSONError(w, http.StatusNotFound, "player not found")
		return
	}

	existing, err := d.stateStore.GetPlayer(r.Context(), playerID)
	if err != nil {
		if errors.Is(err, storage.ErrPlayerNotFound) {
			writeJSONError(w, http.StatusNotFound, "player not found")
			return
		}
		d.logger.Error("failed to query player for update", slog.String("player_id", playerID), slog.Any("error", err))
		writeJSONError(w, http.StatusInternalServerError, "failed to query player")
		return
	}

	var payload struct {
		Notes *string   `json:"notes"`
		Tags  *[]string `json:"tags"`
	}
	if !decodeJSONBody(w, r, &payload, false) {
		return
	}

	newNotes := existing.Notes
	if payload.Notes != nil {
		newNotes = *payload.Notes
	}
	newTags := existing.Tags
	if payload.Tags != nil {
		newTags = *payload.Tags
	}
	if newTags == nil {
		newTags = []string{}
	}

	if err := d.stateStore.UpdatePlayerNotesAndTags(r.Context(), playerID, newNotes, newTags); err != nil {
		if errors.Is(err, storage.ErrPlayerNotFound) {
			writeJSONError(w, http.StatusNotFound, "player not found")
			return
		}
		d.logger.Error("failed to update player notes/tags", slog.String("player_id", playerID), slog.Any("error", err))
		writeJSONError(w, http.StatusInternalServerError, "failed to update player")
		return
	}

	updated, err := d.stateStore.GetPlayer(r.Context(), playerID)
	if err != nil {
		updated = existing
		updated.Notes = newNotes
		updated.Tags = newTags
	}

	d.writeJSONResponse(w, r, http.StatusOK, map[string]any{
		"status": "ok",
		"player": updated,
	}, "error encoding patch player response")
}
