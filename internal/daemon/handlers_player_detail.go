package daemon

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/dank/rl-api-utils/internal/storage"
)

// PlayerDetailResponse represents the composite response for GET /players/{id}.
type PlayerDetailResponse struct {
	*storage.PlayerRecord
	EncountersCount int                             `json:"encounters_count"`
	TeammateWins    int                             `json:"teammate_wins"`
	TeammateLosses  int                             `json:"teammate_losses"`
	OpponentWins    int                             `json:"opponent_wins"`
	OpponentLosses  int                             `json:"opponent_losses"`
	RelationshipTag string                          `json:"relationship_tag,omitempty"`
	WithYouStats    *storage.PlayerRoleStatsSummary `json:"with_you_stats,omitempty"`
	AgainstYouStats *storage.PlayerRoleStatsSummary `json:"against_you_stats,omitempty"`
	Matchups        []*storage.PlayerMatchup        `json:"matchups"`
	RecentMatches   []*storage.PlayerMatchDetail    `json:"recent_matches"`
}

type localProfileProvider interface {
	GetLocalPlayerProfile(ctx context.Context, playerID string) (*storage.PlayerRecord, []*storage.PlayerMatchup, error)
}

type splitStatsProvider interface {
	ComputePlayerSplitStats(ctx context.Context, playerID, playerName string, playlistIDs []int, fromUnix, toUnix int64, fbTMWins, fbTMLosses, fbOppWins, fbOppLosses int) (*storage.PlayerRoleStatsSummary, *storage.PlayerRoleStatsSummary)
}

func (d *Daemon) loadPlayerProfile(ctx context.Context, playerID string) (*storage.PlayerRecord, string, []*storage.PlayerMatchup, bool, error) {
	player, err := d.stateStore.GetPlayer(ctx, playerID)
	if err != nil {
		if !errors.Is(err, storage.ErrPlayerNotFound) {
			return nil, playerID, nil, false, err
		}
		if lpProvider, ok := d.stateStore.(localProfileProvider); ok {
			if lp, lMatchups, lErr := lpProvider.GetLocalPlayerProfile(ctx, playerID); lErr == nil && lp != nil {
				return lp, lp.PlayerID, lMatchups, true, nil
			}
		}
		return nil, playerID, nil, false, storage.ErrPlayerNotFound
	}
	if player == nil {
		return nil, playerID, nil, false, storage.ErrPlayerNotFound
	}

	var localMatchups []*storage.PlayerMatchup
	isLocalPlayer := false
	if localIDs, lErr := d.stateStore.GetLocalPlayerIDs(ctx); lErr == nil {
		for _, lid := range localIDs {
			if strings.EqualFold(lid, player.PlayerID) || strings.EqualFold(lid, playerID) {
				isLocalPlayer = true
				if lpProvider, ok := d.stateStore.(localProfileProvider); ok {
					if _, lMatchups, mErr := lpProvider.GetLocalPlayerProfile(ctx, player.PlayerID); mErr == nil {
						localMatchups = lMatchups
					}
				}
				break
			}
		}
	}

	return player, playerID, localMatchups, isLocalPlayer, nil
}

// handleGetPlayer handles GET /players/{id} and GET /api/players/{id}.
func (d *Daemon) handleGetPlayer(w http.ResponseWriter, r *http.Request) {
	rawID := r.PathValue("id")
	if rawID == "" {
		rawID = strings.TrimPrefix(r.URL.Path, "/api/players/")
		rawID = strings.TrimPrefix(rawID, "/players/")
	}
	playerID, ok := d.resolvePlayerRouteID(w, r, rawID, true)
	if !ok {
		return
	}
	if d.stateStore == nil {
		writeJSONError(w, http.StatusNotFound, "player not found")
		return
	}

	player, resolvedID, localMatchups, isLocalPlayer, err := d.loadPlayerProfile(r.Context(), playerID)
	if err != nil {
		if errors.Is(err, storage.ErrPlayerNotFound) {
			writeJSONError(w, http.StatusNotFound, "player not found")
			return
		}
		if isClientDisconnect(r.Context(), err) {
			d.logger.Debug("player detail query canceled by client", slog.String("player_id", playerID), slog.Any("error", err))
			return
		}
		d.logger.Error("failed to query player", slog.String("player_id", playerID), slog.Any("error", err))
		writeJSONError(w, http.StatusInternalServerError, "failed to query player")
		return
	}
	playerID = resolvedID

	if isLocalPlayer && d.cfg != nil {
		if cfgID := strings.TrimSpace(d.cfg.PlayerTracking.LocalPlayerID); cfgID != "" {
			player.PlayerID = cfgID
			playerID = cfgID
			if strings.Contains(cfgID, "|") {
				if parts := strings.Split(cfgID, "|"); strings.TrimSpace(parts[0]) != "" {
					player.Platform = strings.TrimSpace(parts[0])
				}
			}
		}
		if cfgName := strings.TrimSpace(d.cfg.PlayerTracking.LocalPlayerName); cfgName != "" && (player.PlayerName == "" || strings.EqualFold(player.PlayerName, "You")) {
			player.PlayerName = cfgName
		}
	}

	q := r.URL.Query()
	playlist := strings.TrimSpace(q.Get("playlist"))
	fromUnix := storage.ParseDateFilter(q.Get("from"), false)
	toUnix := storage.ParseDateFilter(q.Get("to"), true)
	hasDateFilter := fromUnix > 0 || toUnix > 0
	hasPlaylistFilter := playlist != "" && strings.ToLower(playlist) != "all"

	matchups := localMatchups
	if !isLocalPlayer {
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
	filteredMatchups := filterPlayerMatchups(matchups, pids, hasPlaylistFilter)

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
	if provider, ok := d.stateStore.(splitStatsProvider); ok {
		withYouStats, againstYouStats = provider.ComputePlayerSplitStats(
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
	relTag := storage.ComputeRelationshipTag(withYouStats.Wins, withYouStats.Losses, againstYouStats.Wins, againstYouStats.Losses)
	if isLocalPlayer {
		relTag = "YOU"
	}

	d.writeJSONResponse(w, r, http.StatusOK, PlayerDetailResponse{
		PlayerRecord:    &playerCopy,
		EncountersCount: encountersCount,
		TeammateWins:    withYouStats.Wins,
		TeammateLosses:  withYouStats.Losses,
		OpponentWins:    againstYouStats.Wins,
		OpponentLosses:  againstYouStats.Losses,
		RelationshipTag: relTag,
		WithYouStats:    withYouStats,
		AgainstYouStats: againstYouStats,
		Matchups:        filteredMatchups,
		RecentMatches:   recentMatches,
	}, "error encoding player detail response")
}

func filterPlayerMatchups(matchups []*storage.PlayerMatchup, pids []int, hasPlaylistFilter bool) []*storage.PlayerMatchup {
	if !hasPlaylistFilter || len(pids) == 0 {
		return matchups
	}
	pidSet := make(map[int]bool, len(pids))
	for _, id := range pids {
		pidSet[id] = true
	}
	out := make([]*storage.PlayerMatchup, 0, len(matchups))
	for _, mu := range matchups {
		if pidSet[mu.PlaylistID] {
			out = append(out, mu)
		}
	}
	return out
}
