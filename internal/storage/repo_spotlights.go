package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const scopedLocalOutcomeExpr = `
CASE
	WHEN m.winner_team >= 0 THEN CASE WHEN m.winner_team = COALESCE(loc.team_num, 0) THEN 1 ELSE 0 END
	WHEN m.blue_score != m.orange_score THEN
		CASE WHEN (COALESCE(loc.team_num, 0) = 0 AND m.blue_score > m.orange_score) OR (COALESCE(loc.team_num, 0) = 1 AND m.orange_score > m.blue_score) THEN 1 ELSE 0 END
	WHEN mp.won IS NOT NULL THEN
		CASE WHEN (mp.team_num = COALESCE(loc.team_num, 0) AND mp.won = 1) OR (mp.team_num != COALESCE(loc.team_num, 0) AND mp.won = 0) THEN 1 ELSE 0 END
	ELSE -1
END`

// GetPlayerSpotlights computes dataset-wide global spotlights (Best Teammate, Most Faced, Toughest Rival).
func (s *SQLiteStore) GetPlayerSpotlights(ctx context.Context) (*GlobalSpotlights, error) {
	return s.GetScopedPlayerSpotlights(ctx, "", 0, 0)
}

// GetScopedPlayerSpotlights computes spotlights optionally scoped by playlist filter and [fromUnix, toUnix] window,
// excluding local player accounts and populating role telemetry splits on winning spotlight candidates.
func (s *SQLiteStore) GetScopedPlayerSpotlights(ctx context.Context, playlist string, fromUnix, toUnix int64) (*GlobalSpotlights, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContextErr(err)
	}

	playlistIDs := ParsePlaylistFilter(playlist)
	hasDateFilter := fromUnix > 0 || toUnix > 0
	localAccounts := s.getLocalAccountIDs(ctx)
	if err := ctx.Err(); err != nil {
		return nil, wrapContextErr(err)
	}

	mpCandidates, err := s.queryMatchPlayerSpotlightCandidates(ctx, playlistIDs, fromUnix, toUnix, localAccounts)
	if err != nil {
		return nil, err
	}

	byID := make(map[string]*PlayerSummary, len(mpCandidates))
	candidates := make([]*PlayerSummary, 0, len(mpCandidates))
	for _, c := range mpCandidates {
		byID[c.PlayerID] = c
		candidates = append(candidates, c)
	}

	if !hasDateFilter {
		pmCandidates, pmErr := s.queryMatchupFallbackSpotlightCandidates(ctx, playlistIDs, localAccounts)
		if pmErr != nil {
			return nil, pmErr
		}
		for _, pm := range pmCandidates {
			existing, exists := byID[pm.PlayerID]
			if !exists {
				byID[pm.PlayerID] = pm
				candidates = append(candidates, pm)
				continue
			}
			if pm.TotalMatches > existing.TotalMatches {
				existing.TotalWinsAsTeammate = pm.TotalWinsAsTeammate
				existing.TotalLossesAsTeammate = pm.TotalLossesAsTeammate
				existing.TotalWinsAsOpponent = pm.TotalWinsAsOpponent
				existing.TotalLossesAsOpponent = pm.TotalLossesAsOpponent
				existing.TotalMatches = pm.TotalMatches
				if pm.LastSeenAt.After(existing.LastSeenAt) {
					existing.LastSeenAt = pm.LastSeenAt
				}
			}
		}
	}

	for _, c := range candidates {
		PopulatePlayerSummaryComputedFields(c)
	}

	spots := ComputeGlobalSpotlights(candidates)
	winners := uniqueSpotlightWinners(spots)
	if len(winners) > 0 {
		if err := s.populateCandidateSplitStatsBulk(ctx, winners, playlistIDs, fromUnix, toUnix); err != nil {
			return nil, err
		}
		for _, w := range winners {
			PopulatePlayerSummaryComputedFields(w)
		}
	}
	return spots, nil
}

func uniqueSpotlightWinners(spots *GlobalSpotlights) []*PlayerSummary {
	if spots == nil {
		return nil
	}
	seen := make(map[string]bool, 3)
	out := make([]*PlayerSummary, 0, 3)
	for _, item := range []*SpotlightSummary{spots.BestTeammate, spots.MostFaced, spots.ToughestRival} {
		if item != nil && item.Player != nil && !seen[item.Player.PlayerID] {
			seen[item.Player.PlayerID] = true
			out = append(out, item.Player)
		}
	}
	return out
}

func (s *SQLiteStore) queryMatchPlayerSpotlightCandidates(
	ctx context.Context,
	playlistIDs []int,
	fromUnix, toUnix int64,
	localAccounts map[string]bool,
) ([]*PlayerSummary, error) {
	whereClauses := []string{
		"mp.is_local = 0",
		"NOT EXISTS (SELECT 1 FROM match_players mp_loc WHERE mp_loc.player_id = p.player_id AND mp_loc.is_local = 1)",
	}
	var args []interface{}
	whereClauses, args = appendSplitScopeFilters(whereClauses, args, playlistIDs, fromUnix, toUnix)

	query := fmt.Sprintf(`
	SELECT p.player_id, p.platform, p.player_name, p.ranks_json, p.notes, p.tags, p.first_seen_at,
	       CASE WHEN COALESCE(MAX(m.record_start_timestamp), 0) > 0 THEN MAX(m.record_start_timestamp) ELSE p.last_seen_at END AS scoped_last_seen,
	       COALESCE(SUM(CASE WHEN mp.team_num = COALESCE(loc.team_num, 0) AND (%s) = 1 THEN 1 ELSE 0 END), 0) AS total_wins_teammate,
	       COALESCE(SUM(CASE WHEN mp.team_num = COALESCE(loc.team_num, 0) AND (%s) = 0 THEN 1 ELSE 0 END), 0) AS total_losses_teammate,
	       COALESCE(SUM(CASE WHEN mp.team_num != COALESCE(loc.team_num, 0) AND (%s) = 1 THEN 1 ELSE 0 END), 0) AS total_wins_opponent,
	       COALESCE(SUM(CASE WHEN mp.team_num != COALESCE(loc.team_num, 0) AND (%s) = 0 THEN 1 ELSE 0 END), 0) AS total_losses_opponent,
	       COUNT(*) AS total_matches
	FROM match_players mp
	JOIN matches m ON m.match_guid = mp.match_guid
	LEFT JOIN (
		SELECT match_guid, MIN(team_num) AS team_num
		FROM match_players
		WHERE is_local = 1
		GROUP BY match_guid
	) loc ON loc.match_guid = m.match_guid
	JOIN players p ON p.player_id = mp.player_id
	WHERE %s
	GROUP BY p.player_id
	HAVING total_matches > 0
	ORDER BY p.player_id ASC;
	`, scopedLocalOutcomeExpr, scopedLocalOutcomeExpr, scopedLocalOutcomeExpr, scopedLocalOutcomeExpr, strings.Join(whereClauses, " AND "))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapContextErr(fmt.Errorf("failed to query scoped spotlight candidates: %w", err))
	}
	defer rows.Close()

	candidates := make([]*PlayerSummary, 0)
	for rows.Next() {
		sRec, scanErr := scanPlayerSummaryRow(rows)
		if scanErr != nil {
			return nil, wrapContextErr(fmt.Errorf("failed to scan scoped spotlight candidate: %w", scanErr))
		}
		if isCandidateLocalAccount(sRec.PlayerID, localAccounts) {
			continue
		}
		candidates = append(candidates, sRec)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapContextErr(fmt.Errorf("scoped spotlight candidate iteration error: %w", err))
	}
	return candidates, nil
}

func (s *SQLiteStore) queryMatchupFallbackSpotlightCandidates(
	ctx context.Context,
	playlistIDs []int,
	localAccounts map[string]bool,
) ([]*PlayerSummary, error) {
	whereClauses := []string{
		"NOT EXISTS (SELECT 1 FROM match_players mp_loc WHERE mp_loc.player_id = p.player_id AND mp_loc.is_local = 1)",
	}
	var args []interface{}
	lastSeenExpr := "p.last_seen_at"
	if len(playlistIDs) > 0 {
		placeholders := make([]string, len(playlistIDs))
		for i, pid := range playlistIDs {
			placeholders[i] = "?"
			args = append(args, pid)
		}
		whereClauses = append(whereClauses, fmt.Sprintf("pm.playlist_id IN (%s)", strings.Join(placeholders, ",")))
		lastSeenExpr = "CASE WHEN COALESCE(MAX(pm.last_played_at), 0) > 0 THEN MAX(pm.last_played_at) ELSE p.last_seen_at END"
	}

	query := fmt.Sprintf(`
	SELECT p.player_id, p.platform, p.player_name, p.ranks_json, p.notes, p.tags, p.first_seen_at, %s,
	       COALESCE(SUM(pm.wins_as_teammate), 0) AS total_wins_teammate,
	       COALESCE(SUM(pm.losses_as_teammate), 0) AS total_losses_teammate,
	       COALESCE(SUM(pm.wins_as_opponent), 0) AS total_wins_opponent,
	       COALESCE(SUM(pm.losses_as_opponent), 0) AS total_losses_opponent,
	       COALESCE(SUM(pm.total_matches), 0) AS total_matches
	FROM players p
	JOIN player_matchups pm ON p.player_id = pm.player_id
	WHERE %s
	GROUP BY p.player_id
	HAVING total_matches > 0
	ORDER BY p.player_id ASC;
	`, lastSeenExpr, strings.Join(whereClauses, " AND "))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapContextErr(fmt.Errorf("failed to query spotlight fallback candidates: %w", err))
	}
	defer rows.Close()

	candidates := make([]*PlayerSummary, 0)
	for rows.Next() {
		sRec, scanErr := scanPlayerSummaryRow(rows)
		if scanErr != nil {
			return nil, wrapContextErr(fmt.Errorf("failed to scan spotlight fallback candidate: %w", scanErr))
		}
		if isCandidateLocalAccount(sRec.PlayerID, localAccounts) {
			continue
		}
		candidates = append(candidates, sRec)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapContextErr(fmt.Errorf("spotlight fallback candidate iteration error: %w", err))
	}
	return candidates, nil
}

// GetScopedPlayerMatchups computes per-playlist matchup breakdowns for a player within [fromUnix, toUnix].
// When fromUnix == 0 && toUnix == 0, reconciles with GetPlayerMatchups to preserve live-only player_matchups rows.
func (s *SQLiteStore) GetScopedPlayerMatchups(ctx context.Context, playerID string, fromUnix, toUnix int64) ([]*PlayerMatchup, error) {
	if strings.TrimSpace(playerID) == "" {
		return nil, errors.New("player_id cannot be empty")
	}
	if err := ctx.Err(); err != nil {
		return nil, wrapContextErr(err)
	}

	resolvedID := s.resolveCanonicalPlayerID(ctx, playerID)
	if strings.TrimSpace(resolvedID) == "" {
		resolvedID = strings.TrimSpace(playerID)
	}

	if s.isLocalPlayerID(ctx, resolvedID) {
		if primaryID := s.primaryLocalPlayerID(ctx); primaryID != "" {
			resolvedID = primaryID
		}
		return s.queryScopedLocalPlayerMatchups(ctx, resolvedID, fromUnix, toUnix)
	}

	whereClauses := []string{
		"mp.player_id = ?",
		"mp.is_local = 0",
		"m.playlist > 0",
	}
	args := []interface{}{resolvedID}
	whereClauses, args = appendSplitScopeFilters(whereClauses, args, nil, fromUnix, toUnix)

	query := fmt.Sprintf(`
	SELECT
		mp.player_id,
		m.playlist,
		COALESCE(SUM(CASE WHEN mp.team_num = COALESCE(loc.team_num, 0) AND (%s) = 1 THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN mp.team_num = COALESCE(loc.team_num, 0) AND (%s) = 0 THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN mp.team_num != COALESCE(loc.team_num, 0) AND (%s) = 1 THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN mp.team_num != COALESCE(loc.team_num, 0) AND (%s) = 0 THEN 1 ELSE 0 END), 0),
		COUNT(*),
		COALESCE(MAX(m.record_start_timestamp), 0)
	FROM match_players mp
	JOIN matches m ON m.match_guid = mp.match_guid
	LEFT JOIN (
		SELECT match_guid, MIN(team_num) AS team_num
		FROM match_players
		WHERE is_local = 1
		GROUP BY match_guid
	) loc ON loc.match_guid = m.match_guid
	WHERE %s
	GROUP BY m.playlist;
	`, scopedLocalOutcomeExpr, scopedLocalOutcomeExpr, scopedLocalOutcomeExpr, scopedLocalOutcomeExpr, strings.Join(whereClauses, " AND "))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapContextErr(fmt.Errorf("failed to query scoped player matchups for %s: %w", resolvedID, err))
	}
	defer rows.Close()

	byPlaylist := make(map[int]*PlayerMatchup)
	for rows.Next() {
		m, scanErr := scanPlayerMatchupRow(rows)
		if scanErr != nil {
			return nil, wrapContextErr(fmt.Errorf("failed to scan scoped player matchup row: %w", scanErr))
		}
		m.PlayerID = resolvedID
		byPlaylist[m.PlaylistID] = m
	}
	if err := rows.Err(); err != nil {
		return nil, wrapContextErr(fmt.Errorf("scoped player matchups iteration error: %w", err))
	}
	_ = rows.Close()

	if fromUnix <= 0 && toUnix <= 0 {
		if pmList, pmErr := s.GetPlayerMatchups(ctx, resolvedID); pmErr == nil {
			for _, pm := range pmList {
				if pm == nil || pm.PlaylistID <= 0 || pm.TotalMatches <= 0 {
					continue
				}
				existing, exists := byPlaylist[pm.PlaylistID]
				if !exists {
					cp := *pm
					byPlaylist[pm.PlaylistID] = &cp
					continue
				}
				if pm.TotalMatches > existing.TotalMatches {
					cp := *pm
					if existing.LastPlayedAt.After(cp.LastPlayedAt) {
						cp.LastPlayedAt = existing.LastPlayedAt
					}
					byPlaylist[pm.PlaylistID] = &cp
				} else if existing.LastPlayedAt.IsZero() && !pm.LastPlayedAt.IsZero() {
					existing.LastPlayedAt = pm.LastPlayedAt
				}
			}
		}
	}

	out := make([]*PlayerMatchup, 0, len(byPlaylist))
	for _, m := range byPlaylist {
		if m != nil && m.TotalMatches > 0 {
			out = append(out, m)
		}
	}
	sortPlayerMatchupsByEncounters(out)
	return out, nil
}

func (s *SQLiteStore) queryScopedLocalPlayerMatchups(ctx context.Context, canonicalID string, fromUnix, toUnix int64) ([]*PlayerMatchup, error) {
	whereClauses := []string{
		"(mp.is_local = 1 OR mp.player_id = ?)",
		"m.playlist > 0",
	}
	args := []interface{}{canonicalID}
	whereClauses, args = appendSplitScopeFilters(whereClauses, args, nil, fromUnix, toUnix)

	query := fmt.Sprintf(`
	SELECT m.playlist, m.record_start_timestamp, m.blue_score, m.orange_score, m.winner_team, mp.team_num, mp.won
	FROM match_players mp
	JOIN matches m ON mp.match_guid = m.match_guid
	WHERE %s
	ORDER BY m.record_start_timestamp DESC;
	`, strings.Join(whereClauses, " AND "))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapContextErr(fmt.Errorf("failed to query local scoped matchups: %w", err))
	}
	defer rows.Close()

	byPlaylist := make(map[int]*PlayerMatchup)
	for rows.Next() {
		var (
			playlist, teamNum               int
			startTS                         int64
			blueScore, orangeSc, winnerTeam sql.NullInt64
			wonVal                          sql.NullInt64
		)
		if scanErr := rows.Scan(&playlist, &startTS, &blueScore, &orangeSc, &winnerTeam, &teamNum, &wonVal); scanErr != nil {
			return nil, wrapContextErr(scanErr)
		}
		if playlist <= 0 {
			continue
		}
		_, localWon, isDraw := classifyLocalOutcomeAndRole(teamNum, 1, teamNum, wonVal, blueScore, orangeSc, winnerTeam)
		if isDraw {
			continue
		}
		mu, ok := byPlaylist[playlist]
		if !ok {
			mu = &PlayerMatchup{PlayerID: canonicalID, PlaylistID: playlist}
			byPlaylist[playlist] = mu
		}
		mu.TotalMatches++
		if localWon {
			mu.WinsAsTeammate++
		} else {
			mu.LossesAsTeammate++
		}
		if startTS > 0 {
			ts := time.Unix(startTS, 0).UTC()
			if ts.After(mu.LastPlayedAt) {
				mu.LastPlayedAt = ts
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, wrapContextErr(err)
	}

	out := make([]*PlayerMatchup, 0, len(byPlaylist))
	for _, mu := range byPlaylist {
		if mu.TotalMatches > 0 {
			out = append(out, mu)
		}
	}
	sortPlayerMatchupsByEncounters(out)
	return out, nil
}

func sortPlayerMatchupsByEncounters(items []*PlayerMatchup) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].TotalMatches != items[j].TotalMatches {
			return items[i].TotalMatches > items[j].TotalMatches
		}
		return items[i].PlaylistID < items[j].PlaylistID
	})
}
