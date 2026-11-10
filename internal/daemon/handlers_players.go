package daemon

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/storage"
)

// PlayerSearchResponse represents the paginated envelope returned by GET /api/players.
type PlayerSearchResponse struct {
	Players    []*storage.PlayerSummary `json:"players"`
	Total      int                      `json:"total"`
	Limit      int                      `json:"limit"`
	Offset     int                      `json:"offset"`
	Spotlights *storage.GlobalSpotlights `json:"spotlights,omitempty"`
}

// PlayerMatchesResponse represents the paginated envelope returned by GET /api/players/{id}/matches.
type PlayerMatchesResponse struct {
	Matches []*storage.PlayerMatchDetail `json:"matches"`
	Total   int                          `json:"total"`
	Limit   int                          `json:"limit"`
	Offset  int                          `json:"offset"`
}

// handleCurrentMatch handles GET /current-match and GET /api/current-match.
func (d *Daemon) handleCurrentMatch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var resp *playertrack.CurrentMatchResponse
	if d.playerTracker != nil {
		resp = d.playerTracker.GetCurrentMatch()
	}

	if resp == nil {
		resp = &playertrack.CurrentMatchResponse{
			ActiveMatch: false,
			Teammates:   make([]playertrack.LobbyPlayer, 0),
			Opponents:   make([]playertrack.LobbyPlayer, 0),
			Spectators:  make([]playertrack.LobbyPlayer, 0),
			UpdatedAt:   time.Now().UTC(),
		}
	} else {
		if resp.Teammates == nil {
			resp.Teammates = make([]playertrack.LobbyPlayer, 0)
		}
		if resp.Opponents == nil {
			resp.Opponents = make([]playertrack.LobbyPlayer, 0)
		}
		if resp.Spectators == nil {
			resp.Spectators = make([]playertrack.LobbyPlayer, 0)
		}
	}

	if len(resp.LocalPlayerIDs) == 0 {
		if d.playerTracker != nil {
			resp.LocalPlayerIDs = d.playerTracker.GetLocalPlayerIDs(r.Context())
		}
		if len(resp.LocalPlayerIDs) == 0 && d.sessionTracker != nil {
			resp.LocalPlayerIDs = d.sessionTracker.GetLocalPlayerIDs()
		}
		if len(resp.LocalPlayerIDs) == 0 && d.stateStore != nil {
			if ids, err := d.stateStore.GetLocalPlayerIDs(r.Context()); err == nil {
				resp.LocalPlayerIDs = ids
			}
		}
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		d.logger.Warn("error encoding current-match response", slog.Any("error", err))
	}
}

// handleListPlayers handles GET /players and GET /api/players.
func (d *Daemon) handleListPlayers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	isAPIRoute := strings.HasPrefix(r.URL.Path, "/api/")

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
		if parsed, err := strconv.Atoi(rawOffset); err == nil {
			if parsed < 0 {
				offset = 0
			} else {
				offset = parsed
			}
		}
	}

	if d.stateStore == nil {
		w.WriteHeader(http.StatusOK)
		if isAPIRoute {
			_ = json.NewEncoder(w).Encode(PlayerSearchResponse{
				Players: make([]*storage.PlayerSummary, 0),
				Total:   0,
				Limit:   limit,
				Offset:  offset,
			})
		} else {
			w.Write([]byte("[]\n"))
		}
		return
	}

	q := r.URL.Query()
	query := strings.TrimSpace(q.Get("query"))
	if query == "" {
		query = strings.TrimSpace(q.Get("q"))
	}
	if query == "" {
		query = strings.TrimSpace(q.Get("search"))
	}
	platform := strings.TrimSpace(q.Get("platform"))
	role := strings.TrimSpace(q.Get("role"))
	playlist := strings.TrimSpace(q.Get("playlist"))
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

	minMatches := 2
	if rawMinMatches := strings.TrimSpace(q.Get("min_matches")); rawMinMatches != "" {
		if parsed, err := strconv.Atoi(rawMinMatches); err == nil && parsed > 0 {
			minMatches = parsed
		}
	}
	minSynergyGames := 1
	if rawMinSynGames := strings.TrimSpace(q.Get("min_synergy_games")); rawMinSynGames != "" {
		if parsed, err := strconv.Atoi(rawMinSynGames); err == nil && parsed > 0 {
			minSynergyGames = parsed
		}
	}
	minSynergyWR := 50.0
	if rawMinSynWR := strings.TrimSpace(q.Get("min_synergy_wr")); rawMinSynWR != "" {
		if parsed, err := strconv.ParseFloat(rawMinSynWR, 64); err == nil && parsed > 0 {
			minSynergyWR = parsed
		}
	}

	filter := storage.PlayerDirectoryFilter{
		Query:           query,
		Platform:        platform,
		Role:            role,
		Playlist:        playlist,
		FromUnix:        fromUnix,
		ToUnix:          toUnix,
		MinMatches:      minMatches,
		MinSynergyGames: minSynergyGames,
		MinSynergyWR:    minSynergyWR,
		Sort:            sortBy,
		Order:           sortDir,
		Limit:           limit,
		Offset:          offset,
	}

	summaries, total, err := d.stateStore.FilterPlayerSummaries(r.Context(), filter)
	if err != nil {
		d.logger.Error("failed to query player summaries", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to query players"})
		return
	}

	if summaries == nil {
		summaries = make([]*storage.PlayerSummary, 0)
	}

	var spotlights *storage.GlobalSpotlights
	if isAPIRoute {
		if sp, spErr := d.stateStore.GetPlayerSpotlights(r.Context()); spErr == nil {
			spotlights = sp
		}
	}

	w.WriteHeader(http.StatusOK)
	if isAPIRoute {
		if err := json.NewEncoder(w).Encode(PlayerSearchResponse{
			Players:    summaries,
			Total:      total,
			Limit:      limit,
			Offset:     offset,
			Spotlights: spotlights,
		}); err != nil {
			d.logger.Warn("error encoding player search response", slog.Any("error", err))
		}
	} else {
		if err := json.NewEncoder(w).Encode(summaries); err != nil {
			d.logger.Warn("error encoding player summaries response", slog.Any("error", err))
		}
	}
}

// handleGetPlayer handles GET /players/{id} and GET /api/players/{id}.
func (d *Daemon) handleGetPlayer(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	rawID := r.PathValue("id")
	if rawID == "" {
		rawID = strings.TrimPrefix(r.URL.Path, "/api/players/")
		rawID = strings.TrimPrefix(rawID, "/players/")
	}
	rawID = strings.Trim(rawID, "/")

	if rawID == "" {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "player not found"})
		return
	}

	playerID, err := url.PathUnescape(rawID)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid player id"})
		return
	}
	playerID = strings.TrimSpace(playerID)
	if playerID == "" {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "player not found"})
		return
	}

	if d.stateStore == nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "player not found"})
		return
	}

	var (
		player        *storage.PlayerRecord
		localMatchups []*storage.PlayerMatchup
		isLocalPlayer bool
	)
	player, err = d.stateStore.GetPlayer(r.Context(), playerID)
	if err != nil {
		if errors.Is(err, storage.ErrPlayerNotFound) {
			if sqlStore, ok := d.stateStore.(*storage.SQLiteStore); ok {
				if lp, lMatchups, lErr := sqlStore.GetLocalPlayerProfile(r.Context(), playerID); lErr == nil && lp != nil {
					player = lp
					localMatchups = lMatchups
					playerID = lp.PlayerID
					isLocalPlayer = true
				}
			} else if jsonStore, ok := d.stateStore.(*storage.JSONStore); ok {
				if lp, lMatchups, lErr := jsonStore.GetLocalPlayerProfile(r.Context(), playerID); lErr == nil && lp != nil {
					player = lp
					localMatchups = lMatchups
					playerID = lp.PlayerID
					isLocalPlayer = true
				}
			}
			if player == nil {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "player not found"})
				return
			}
		} else {
			d.logger.Error("failed to query player", slog.String("player_id", playerID), slog.Any("error", err))
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to query player"})
			return
		}
	}
	if player == nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "player not found"})
		return
	}
	if !isLocalPlayer {
		if localIDs, lErr := d.stateStore.GetLocalPlayerIDs(r.Context()); lErr == nil {
			for _, lid := range localIDs {
				if strings.EqualFold(lid, player.PlayerID) || strings.EqualFold(lid, playerID) {
					isLocalPlayer = true
					if sqlStore, ok := d.stateStore.(*storage.SQLiteStore); ok {
						if _, lMatchups, mErr := sqlStore.GetLocalPlayerProfile(r.Context(), player.PlayerID); mErr == nil {
							localMatchups = lMatchups
						}
					} else if jsonStore, ok := d.stateStore.(*storage.JSONStore); ok {
						if _, lMatchups, mErr := jsonStore.GetLocalPlayerProfile(r.Context(), player.PlayerID); mErr == nil {
							localMatchups = lMatchups
						}
					}
					break
				}
			}
		}
	}

	q := r.URL.Query()
	playlist := strings.TrimSpace(q.Get("playlist"))
	fromUnix := storage.ParseDateFilter(q.Get("from"), false)
	toUnix := storage.ParseDateFilter(q.Get("to"), true)
	hasDateFilter := fromUnix > 0 || toUnix > 0
	hasPlaylistFilter := playlist != "" && strings.ToLower(playlist) != "all"

	var matchups []*storage.PlayerMatchup
	if isLocalPlayer {
		matchups = localMatchups
	} else {
		matchups, err = d.stateStore.GetPlayerMatchups(r.Context(), playerID)
		if err != nil {
			d.logger.Warn("failed to query player matchups; using empty slice",
				slog.String("player_id", playerID),
				slog.Any("error", err),
			)
			matchups = make([]*storage.PlayerMatchup, 0)
		}
	}
	if matchups == nil {
		matchups = make([]*storage.PlayerMatchup, 0)
	}

	pids := storage.ParsePlaylistFilter(playlist)
	filteredMatchups := matchups
	if hasPlaylistFilter && len(pids) > 0 {
		pidSet := make(map[int]bool, len(pids))
		for _, id := range pids {
			pidSet[id] = true
		}
		filteredMatchups = make([]*storage.PlayerMatchup, 0, len(matchups))
		for _, mu := range matchups {
			if pidSet[mu.PlaylistID] {
				filteredMatchups = append(filteredMatchups, mu)
			}
		}
	}

	var fbTmWins, fbTmLosses, fbOppWins, fbOppLosses, encountersCount int
	if !hasDateFilter {
		for _, mu := range filteredMatchups {
			fbTmWins += mu.WinsAsTeammate
			fbTmLosses += mu.LossesAsTeammate
			fbOppWins += mu.WinsAsOpponent
			fbOppLosses += mu.LossesAsOpponent
			encountersCount += mu.TotalMatches
		}
	}

	var withYouStats, againstYouStats *storage.PlayerRoleStatsSummary
	if sqlStore, ok := d.stateStore.(*storage.SQLiteStore); ok {
		withYouStats, againstYouStats = sqlStore.ComputePlayerSplitStats(
			r.Context(), playerID, player.PlayerName, pids, fromUnix, toUnix,
			fbTmWins, fbTmLosses, fbOppWins, fbOppLosses,
		)
	} else if jsonStore, ok := d.stateStore.(*storage.JSONStore); ok {
		withYouStats, againstYouStats = jsonStore.ComputePlayerSplitStats(
			r.Context(), playerID, player.PlayerName, pids, fromUnix, toUnix,
			fbTmWins, fbTmLosses, fbOppWins, fbOppLosses,
		)
	}
	if withYouStats == nil {
		withYouStats = &storage.PlayerRoleStatsSummary{}
	}
	if againstYouStats == nil {
		againstYouStats = &storage.PlayerRoleStatsSummary{}
	}

	tmWins := withYouStats.Wins
	tmLosses := withYouStats.Losses
	oppWins := againstYouStats.Wins
	oppLosses := againstYouStats.Losses
	totalFromSplit := withYouStats.Games + againstYouStats.Games
	if hasDateFilter || totalFromSplit > encountersCount {
		encountersCount = totalFromSplit
	}

	recentMatches, _, err := d.stateStore.ListPlayerMatches(r.Context(), playerID, storage.PlayerMatchFilter{
		PlaylistFilter:   playlist,
		FromUnix:         fromUnix,
		ToUnix:           toUnix,
		LocalPerspective: true,
		Limit:            5,
	})
	if err != nil {
		d.logger.Warn("failed to query player recent matches; using empty slice",
			slog.String("player_id", playerID),
			slog.Any("error", err),
		)
		recentMatches = make([]*storage.PlayerMatchDetail, 0)
	}
	if recentMatches == nil {
		recentMatches = make([]*storage.PlayerMatchDetail, 0)
	}

	playerCopy := *player

	relTag := storage.ComputeRelationshipTag(tmWins, tmLosses, oppWins, oppLosses)
	if isLocalPlayer {
		relTag = "YOU"
	}

	resp := PlayerDetailResponse{
		PlayerRecord:    &playerCopy,
		EncountersCount: encountersCount,
		TeammateWins:    tmWins,
		TeammateLosses:  tmLosses,
		OpponentWins:    oppWins,
		OpponentLosses:  oppLosses,
		RelationshipTag: relTag,
		WithYouStats:    withYouStats,
		AgainstYouStats: againstYouStats,
		Matchups:        filteredMatchups,
		RecentMatches:   recentMatches,
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		d.logger.Warn("error encoding player detail response", slog.Any("error", err))
	}
}

// handleGetPlayerMatches handles GET /players/{id}/matches and GET /api/players/{id}/matches.
func (d *Daemon) handleGetPlayerMatches(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	rawID := r.PathValue("id")
	if rawID == "" {
		p := strings.TrimPrefix(r.URL.Path, "/api/players/")
		p = strings.TrimPrefix(p, "/players/")
		rawID = strings.TrimSuffix(p, "/matches")
	}
	rawID = strings.Trim(rawID, "/")

	if rawID == "" {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "player not found"})
		return
	}

	playerID, err := url.PathUnescape(rawID)
	if err != nil {
		playerID = rawID
	}
	playerID = strings.TrimSpace(playerID)
	if playerID == "" {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "player not found"})
		return
	}

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
		if parsed, err := strconv.Atoi(rawOffset); err == nil {
			if parsed < 0 {
				offset = 0
			} else {
				offset = parsed
			}
		}
	}

	var playlist int
	rawPlaylist := strings.TrimSpace(q.Get("playlist"))
	if rawPlaylist != "" {
		if parsed, err := strconv.Atoi(rawPlaylist); err == nil {
			playlist = parsed
		}
	}

	role := strings.TrimSpace(q.Get("role"))
	result := strings.TrimSpace(q.Get("result"))
	status := strings.TrimSpace(q.Get("status"))
	search := strings.TrimSpace(q.Get("search"))
	if search == "" {
		search = strings.TrimSpace(q.Get("query"))
	}
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
	fromUnix := storage.ParseDateFilter(q.Get("from"), false)
	toUnix := storage.ParseDateFilter(q.Get("to"), true)

	if d.stateStore == nil {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(PlayerMatchesResponse{
			Matches: make([]*storage.PlayerMatchDetail, 0),
			Total:   0,
			Limit:   limit,
			Offset:  offset,
		})
		return
	}

	filter := storage.PlayerMatchFilter{
		Role:             role,
		Playlist:         playlist,
		PlaylistFilter:   rawPlaylist,
		FromUnix:         fromUnix,
		ToUnix:           toUnix,
		Status:           status,
		Result:           result,
		Search:           search,
		SortBy:           sortBy,
		SortDir:          sortDir,
		LocalPerspective: true,
		Limit:            limit,
		Offset:           offset,
	}

	matches, total, err := d.stateStore.ListPlayerMatches(r.Context(), playerID, filter)
	if err != nil {
		d.logger.Error("failed to query player matches", slog.String("player_id", playerID), slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to query player matches"})
		return
	}

	if matches == nil {
		matches = make([]*storage.PlayerMatchDetail, 0)
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(PlayerMatchesResponse{
		Matches: matches,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
	}); err != nil {
		d.logger.Warn("error encoding player matches response", slog.Any("error", err))
	}
}

// handlePatchPlayer handles PATCH /players/{id...} and PATCH /api/players/{id...}.
func (d *Daemon) handlePatchPlayer(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if !isAllowedMutatingOrigin(r) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "forbidden: invalid origin"})
		return
	}

	rawID := r.PathValue("id")
	if rawID == "" {
		rawID = strings.TrimPrefix(r.URL.Path, "/api/players/")
		rawID = strings.TrimPrefix(rawID, "/players/")
	}
	rawID = strings.Trim(rawID, "/")

	if rawID == "" {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "player not found"})
		return
	}

	playerID, err := url.PathUnescape(rawID)
	if err != nil {
		playerID = rawID
	}
	playerID = strings.TrimSpace(playerID)
	if playerID == "" {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "player not found"})
		return
	}

	if d.stateStore == nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "player not found"})
		return
	}

	existing, err := d.stateStore.GetPlayer(r.Context(), playerID)
	if err != nil {
		if errors.Is(err, storage.ErrPlayerNotFound) {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "player not found"})
			return
		}
		d.logger.Error("failed to query player for update", slog.String("player_id", playerID), slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to query player"})
		return
	}

	var payload struct {
		Notes *string   `json:"notes"`
		Tags  *[]string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
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
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "player not found"})
			return
		}
		d.logger.Error("failed to update player notes/tags", slog.String("player_id", playerID), slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to update player"})
		return
	}

	updated, err := d.stateStore.GetPlayer(r.Context(), playerID)
	if err != nil {
		updated = existing
		updated.Notes = newNotes
		updated.Tags = newTags
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"player": updated,
	}); err != nil {
		d.logger.Warn("error encoding patch player response", slog.Any("error", err))
	}
}
