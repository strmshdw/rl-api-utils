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
	Players []*storage.PlayerSummary `json:"players"`
	Total   int                      `json:"total"`
	Limit   int                      `json:"limit"`
	Offset  int                      `json:"offset"`
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

	query := strings.TrimSpace(r.URL.Query().Get("query"))
	platform := strings.TrimSpace(r.URL.Query().Get("platform"))

	summaries, total, err := d.stateStore.SearchPlayerSummaries(r.Context(), query, platform, limit, offset)
	if err != nil {
		d.logger.Error("failed to query player summaries", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to query players"})
		return
	}

	if summaries == nil {
		summaries = make([]*storage.PlayerSummary, 0)
	}

	w.WriteHeader(http.StatusOK)
	if isAPIRoute {
		if err := json.NewEncoder(w).Encode(PlayerSearchResponse{
			Players: summaries,
			Total:   total,
			Limit:   limit,
			Offset:  offset,
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

	player, err := d.stateStore.GetPlayer(r.Context(), playerID)
	if err != nil {
		if errors.Is(err, storage.ErrPlayerNotFound) {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "player not found"})
			return
		}
		d.logger.Error("failed to query player", slog.String("player_id", playerID), slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to query player"})
		return
	}
	if player == nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "player not found"})
		return
	}

	matchups, err := d.stateStore.GetPlayerMatchups(r.Context(), playerID)
	if err != nil {
		d.logger.Warn("failed to query player matchups; using empty slice",
			slog.String("player_id", playerID),
			slog.Any("error", err),
		)
		matchups = make([]*storage.PlayerMatchup, 0)
	}
	if matchups == nil {
		matchups = make([]*storage.PlayerMatchup, 0)
	}

	resp := PlayerDetailResponse{
		PlayerRecord: player,
		Matchups:     matchups,
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		d.logger.Warn("error encoding player detail response", slog.Any("error", err))
	}
}
