package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const bulkPlayerIDFilterMax = 200

type candidateSplitEntry struct {
	summary     *PlayerSummary
	withAcc     RoleStatsAccumulator
	againstAcc  RoleStatsAccumulator
	fbTMWins    int
	fbTMLosses  int
	fbOppWins   int
	fbOppLosses int
}

// ListPlayerSummaries returns paginated players with aggregate win/loss and split statistics across all playlists.
func (s *SQLiteStore) ListPlayerSummaries(ctx context.Context, limit, offset int) ([]*PlayerSummary, error) {
	if limit == 0 {
		return []*PlayerSummary{}, nil
	}
	if limit < 0 {
		limit = -1
	}
	if offset < 0 {
		offset = 0
	}

	const query = `
	SELECT p.player_id, p.platform, p.player_name, p.ranks_json, p.notes, p.tags, p.first_seen_at, p.last_seen_at,
	       COALESCE(SUM(m.wins_as_teammate), 0) AS total_wins_teammate,
	       COALESCE(SUM(m.losses_as_teammate), 0) AS total_losses_teammate,
	       COALESCE(SUM(m.wins_as_opponent), 0) AS total_wins_opponent,
	       COALESCE(SUM(m.losses_as_opponent), 0) AS total_losses_opponent,
	       COALESCE(SUM(m.total_matches), 0) AS total_matches
	FROM (
		SELECT player_id, platform, player_name, ranks_json, notes, tags, first_seen_at, last_seen_at
		FROM players p
		ORDER BY p.last_seen_at DESC, p.player_id ASC
		LIMIT ? OFFSET ?
	) p
	LEFT JOIN player_matchups m ON p.player_id = m.player_id
	GROUP BY p.player_id
	ORDER BY p.last_seen_at DESC, p.player_id ASC;
	`
	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list player summaries: %w", err)
	}
	defer rows.Close()

	summaries := make([]*PlayerSummary, 0)
	for rows.Next() {
		sRec, scanErr := scanPlayerSummaryRow(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("failed to scan player summary row: %w", scanErr)
		}
		PopulatePlayerSummaryComputedFields(sRec)
		summaries = append(summaries, sRec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error in list player summaries: %w", err)
	}
	_ = rows.Close()

	if err := s.populateCandidateSplitStatsBulk(ctx, summaries, nil, 0, 0); err != nil {
		return nil, err
	}
	for _, sRec := range summaries {
		if sRec.WithYouStats.Games+sRec.AgainstYouStats.Games > sRec.TotalMatches {
			sRec.TotalWinsAsTeammate = sRec.WithYouStats.Wins
			sRec.TotalLossesAsTeammate = sRec.WithYouStats.Losses
			sRec.TotalWinsAsOpponent = sRec.AgainstYouStats.Wins
			sRec.TotalLossesAsOpponent = sRec.AgainstYouStats.Losses
			sRec.TotalMatches = sRec.WithYouStats.Games + sRec.AgainstYouStats.Games
		}
		PopulatePlayerSummaryComputedFields(sRec)
	}
	return summaries, nil
}

// SearchPlayerSummaries searches players matching query across player_name and player_id,
// optionally filtered by platform, ordered by last_seen_at DESC, player_id ASC.
func (s *SQLiteStore) SearchPlayerSummaries(ctx context.Context, query, platform string, limit, offset int) ([]*PlayerSummary, int, error) {
	return s.FilterPlayerSummaries(ctx, PlayerDirectoryFilter{
		Query:    query,
		Platform: platform,
		Role:     "all",
		Sort:     "last_seen",
		Order:    "desc",
		Limit:    limit,
		Offset:   offset,
	})
}

// FilterPlayerSummaries searches, filters, and sorts players across the dataset
// using batched split-stats aggregation and full context-cancellation propagation.
func (s *SQLiteStore) FilterPlayerSummaries(ctx context.Context, filter PlayerDirectoryFilter) ([]*PlayerSummary, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, wrapContextErr(err)
	}

	cleanQuery := strings.TrimSpace(filter.Query)
	cleanPlatform := strings.TrimSpace(strings.ToLower(filter.Platform))
	playlistIDs := ParsePlaylistFilter(filter.Playlist)
	hasScopeFilter := len(playlistIDs) > 0 || filter.FromUnix > 0 || filter.ToUnix > 0

	var pWhereClauses []string
	var pWhereArgs []interface{}
	pWhereClauses = append(pWhereClauses, "NOT EXISTS (SELECT 1 FROM match_players mp_loc WHERE mp_loc.player_id = p.player_id AND mp_loc.is_local = 1)")

	if cleanPlatform != "" && cleanPlatform != "all" {
		switch cleanPlatform {
		case "playstation", "ps4", "psn", "psynet":
			pWhereClauses = append(pWhereClauses, "LOWER(p.platform) IN ('playstation', 'ps4', 'psn', 'psynet')")
		case "xbox", "xboxone":
			pWhereClauses = append(pWhereClauses, "LOWER(p.platform) IN ('xbox', 'xboxone')")
		case "switch", "nintendo":
			pWhereClauses = append(pWhereClauses, "LOWER(p.platform) IN ('switch', 'nintendo')")
		default:
			pWhereClauses = append(pWhereClauses, "LOWER(p.platform) = ?")
			pWhereArgs = append(pWhereArgs, cleanPlatform)
		}
	}

	if cleanQuery != "" {
		lowerQuery := strings.ToLower(cleanQuery)
		if strings.ContainsRune(lowerQuery, '\x00') {
			pWhereClauses = append(pWhereClauses, "(INSTR(p.player_name_lower, ?) > 0 OR INSTR(LOWER(p.player_id), ?) > 0)")
			pWhereArgs = append(pWhereArgs, lowerQuery, lowerQuery)
		} else {
			pWhereClauses = append(pWhereClauses, "(p.player_name_lower LIKE ? ESCAPE '\\' OR LOWER(p.player_id) LIKE ? ESCAPE '\\')")
			escaped := "%" + escapeLike(lowerQuery) + "%"
			pWhereArgs = append(pWhereArgs, escaped, escaped)
		}
	}

	pWhereSQL := ""
	if len(pWhereClauses) > 0 {
		pWhereSQL = " WHERE " + strings.Join(pWhereClauses, " AND ")
	}

	mJoinClauses := []string{"p.player_id = m.player_id"}
	var mJoinArgs []interface{}
	if len(playlistIDs) > 0 {
		placeholders := make([]string, len(playlistIDs))
		for i, pid := range playlistIDs {
			placeholders[i] = "?"
			mJoinArgs = append(mJoinArgs, pid)
		}
		mJoinClauses = append(mJoinClauses, fmt.Sprintf("m.playlist_id IN (%s)", strings.Join(placeholders, ",")))
	}
	if filter.FromUnix > 0 {
		mJoinClauses = append(mJoinClauses, "m.last_played_at >= ?")
		mJoinArgs = append(mJoinArgs, filter.FromUnix)
	}
	if filter.ToUnix > 0 {
		mJoinClauses = append(mJoinClauses, "m.last_played_at <= ?")
		mJoinArgs = append(mJoinArgs, filter.ToUnix)
	}

	baseSQL := fmt.Sprintf(`
	SELECT p.player_id, p.platform, p.player_name, p.ranks_json, p.notes, p.tags, p.first_seen_at, p.last_seen_at,
	       COALESCE(SUM(m.wins_as_teammate), 0) AS total_wins_teammate,
	       COALESCE(SUM(m.losses_as_teammate), 0) AS total_losses_teammate,
	       COALESCE(SUM(m.wins_as_opponent), 0) AS total_wins_opponent,
	       COALESCE(SUM(m.losses_as_opponent), 0) AS total_losses_opponent,
	       COALESCE(SUM(m.total_matches), 0) AS total_matches
	FROM players p
	LEFT JOIN player_matchups m ON %s
	%s
	GROUP BY p.player_id;
	`, strings.Join(mJoinClauses, " AND "), pWhereSQL)

	rows, err := s.db.QueryContext(ctx, baseSQL, append(mJoinArgs, pWhereArgs...)...)
	if err != nil {
		return nil, 0, wrapContextErr(fmt.Errorf("failed to query player summaries: %w", err))
	}
	defer rows.Close()

	candidates := make([]*PlayerSummary, 0)
	for rows.Next() {
		sRec, scanErr := scanPlayerSummaryRow(rows)
		if scanErr != nil {
			return nil, 0, wrapContextErr(fmt.Errorf("failed to scan player summary row: %w", scanErr))
		}
		candidates = append(candidates, sRec)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, wrapContextErr(fmt.Errorf("rows iteration error in filter player summaries: %w", err))
	}
	_ = rows.Close()

	if err := s.populateCandidateSplitStatsBulk(ctx, candidates, playlistIDs, filter.FromUnix, filter.ToUnix); err != nil {
		return nil, 0, err
	}

	matching := make([]*PlayerSummary, 0, len(candidates))
	for _, sRec := range candidates {
		withSt, againstSt := sRec.WithYouStats, sRec.AgainstYouStats
		if hasScopeFilter || withSt.Games+againstSt.Games > sRec.TotalMatches {
			sRec.TotalWinsAsTeammate = withSt.Wins
			sRec.TotalLossesAsTeammate = withSt.Losses
			sRec.TotalWinsAsOpponent = againstSt.Wins
			sRec.TotalLossesAsOpponent = againstSt.Losses
			if withSt.Games+againstSt.Games > 0 || filter.FromUnix > 0 || filter.ToUnix > 0 || withSt.Games+againstSt.Games > sRec.TotalMatches {
				sRec.TotalMatches = withSt.Games + againstSt.Games
			}
		}
		if hasScopeFilter && sRec.TotalMatches <= 0 {
			continue
		}
		if !MatchesRoleFilter(sRec, filter) {
			continue
		}
		PopulatePlayerSummaryComputedFields(sRec)
		matching = append(matching, sRec)
	}

	results, total := SortAndPaginatePlayerSummaries(matching, filter, playlistIDs)
	return results, total, nil
}

// GetPlayerSpotlights computes dataset-wide global spotlights (Best Teammate, Most Faced, Toughest Rival).
func (s *SQLiteStore) GetPlayerSpotlights(ctx context.Context) (*GlobalSpotlights, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContextErr(err)
	}

	const query = `
	SELECT p.player_id, p.platform, p.player_name, p.ranks_json, p.notes, p.tags, p.first_seen_at, p.last_seen_at,
	       COALESCE(SUM(m.wins_as_teammate), 0) AS total_wins_teammate,
	       COALESCE(SUM(m.losses_as_teammate), 0) AS total_losses_teammate,
	       COALESCE(SUM(m.wins_as_opponent), 0) AS total_wins_opponent,
	       COALESCE(SUM(m.losses_as_opponent), 0) AS total_losses_opponent,
	       COALESCE(SUM(m.total_matches), 0) AS total_matches
	FROM players p
	JOIN player_matchups m ON p.player_id = m.player_id
	GROUP BY p.player_id
	HAVING total_matches > 0
	ORDER BY p.player_id ASC;
	`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, wrapContextErr(fmt.Errorf("failed to query spotlight candidates: %w", err))
	}
	defer rows.Close()

	candidates := make([]*PlayerSummary, 0)
	for rows.Next() {
		sRec, scanErr := scanPlayerSummaryRow(rows)
		if scanErr != nil {
			return nil, wrapContextErr(fmt.Errorf("failed to scan spotlight candidate: %w", scanErr))
		}
		candidates = append(candidates, sRec)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapContextErr(fmt.Errorf("spotlight candidate iteration error: %w", err))
	}
	return ComputeGlobalSpotlights(candidates), nil
}

// ComputePlayerSplitStats computes WITH YOU (teammate) and AGAINST YOU (opponent, local win/loss perspective)
// Core + Ballchasing Mechanics stats for a player, optionally scoped to playlistIDs and [fromUnix, toUnix].
func (s *SQLiteStore) ComputePlayerSplitStats(
	ctx context.Context,
	playerID, playerName string,
	playlistIDs []int,
	fromUnix, toUnix int64,
	fbTMWins, fbTMLosses, fbOppWins, fbOppLosses int,
) (*PlayerRoleStatsSummary, *PlayerRoleStatsSummary) {
	resolvedID := s.resolveCanonicalPlayerID(ctx, playerID)
	if resolvedID == "" {
		resolvedID = strings.TrimSpace(playerID)
	}
	isLocalTarget := s.isLocalPlayerID(ctx, resolvedID)
	if isLocalTarget {
		if primaryID := s.primaryLocalPlayerID(ctx); primaryID != "" {
			resolvedID = primaryID
		}
	}

	if fromUnix > 0 || toUnix > 0 {
		var hasAny int
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM match_players WHERE player_id = ? LIMIT 1;", resolvedID).Scan(&hasAny); err == nil && hasAny > 0 {
			fbTMWins, fbTMLosses, fbOppWins, fbOppLosses = 0, 0, 0, 0
		}
	}

	var whereClauses []string
	var args []interface{}
	if isLocalTarget {
		whereClauses = append(whereClauses, "(mp.player_id = ? OR mp.is_local = 1)")
	} else {
		whereClauses = append(whereClauses, "mp.player_id = ?")
	}
	args = append(args, resolvedID)
	whereClauses, args = appendSplitScopeFilters(whereClauses, args, playlistIDs, fromUnix, toUnix)

	querySQL := fmt.Sprintf(`
	SELECT mp.player_id, mp.team_num, mp.is_local, mp.won, mp.score, mp.goals, mp.assists, mp.saves, mp.shots, mp.demos, mp.stats_json,
	       m.blue_score, m.orange_score, m.winner_team, m.replay_stats_json,
	       COALESCE((SELECT lp.team_num FROM match_players lp WHERE lp.match_guid = m.match_guid AND lp.is_local = 1 LIMIT 1), -1) AS local_team
	FROM match_players mp
	JOIN matches m ON mp.match_guid = m.match_guid
	WHERE %s
	ORDER BY m.record_start_timestamp DESC;
	`, strings.Join(whereClauses, " AND "))

	var withAcc, againstAcc RoleStatsAccumulator
	rows, err := s.db.QueryContext(ctx, querySQL, args...)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var (
				mpPlayerID, statsJSON                   string
				teamNum, isLocal, localTeam             int
				score, goals, assists, saves, shots, dm int
				wonVal, blueScore, orangeSc, winnerTeam sql.NullInt64
				replayStats                             sql.NullString
			)
			if rows.Scan(
				&mpPlayerID, &teamNum, &isLocal, &wonVal, &score, &goals, &assists, &saves, &shots, &dm, &statsJSON,
				&blueScore, &orangeSc, &winnerTeam, &replayStats, &localTeam,
			) != nil {
				continue
			}

			isTeammate, localWon, isDraw := classifyLocalOutcomeAndRole(teamNum, isLocal, localTeam, wonVal, blueScore, orangeSc, winnerTeam)
			lookupID := resolvedID
			if strings.TrimSpace(mpPlayerID) != "" {
				lookupID = strings.TrimSpace(mpPlayerID)
			}
			resolvedStatsJSON := ExtractPlayerStatsJSONFromReplay(statsJSON, replayStats.String, lookupID, playerName, teamNum)
			if isTeammate {
				withAcc.AddMatchOutcome(localWon, isDraw)
				withAcc.AddPlayerBoxAndTelemetry(score, goals, assists, saves, shots, dm, resolvedStatsJSON)
			} else {
				againstAcc.AddMatchOutcome(localWon, isDraw)
				againstAcc.AddPlayerBoxAndTelemetry(score, goals, assists, saves, shots, dm, resolvedStatsJSON)
			}
		}
	}
	return withAcc.Finalize(fbTMWins, fbTMLosses), againstAcc.Finalize(fbOppWins, fbOppLosses)
}

func (s *SQLiteStore) populateCandidateSplitStatsBulk(
	ctx context.Context,
	candidates []*PlayerSummary,
	playlistIDs []int,
	fromUnix, toUnix int64,
) error {
	if err := ctx.Err(); err != nil {
		return wrapContextErr(err)
	}
	if len(candidates) == 0 {
		return nil
	}

	localAccounts := s.getLocalAccountIDs(ctx)
	if err := ctx.Err(); err != nil {
		return wrapContextErr(err)
	}

	accByPlayerID := make(map[string][]*candidateSplitEntry, len(candidates))
	nonLocalIDs := make([]string, 0, len(candidates))
	for _, sRec := range candidates {
		if sRec == nil {
			continue
		}
		if sRec.PlayerID == "" || strings.TrimSpace(sRec.PlayerID) != sRec.PlayerID || isCandidateLocalAccount(sRec.PlayerID, localAccounts) {
			if err := ctx.Err(); err != nil {
				return wrapContextErr(err)
			}
			sRec.WithYouStats, sRec.AgainstYouStats = s.ComputePlayerSplitStats(
				ctx, sRec.PlayerID, sRec.PlayerName, playlistIDs, fromUnix, toUnix,
				sRec.TotalWinsAsTeammate, sRec.TotalLossesAsTeammate,
				sRec.TotalWinsAsOpponent, sRec.TotalLossesAsOpponent,
			)
			if err := ctx.Err(); err != nil {
				return wrapContextErr(err)
			}
			continue
		}
		if _, exists := accByPlayerID[sRec.PlayerID]; !exists {
			nonLocalIDs = append(nonLocalIDs, sRec.PlayerID)
		}
		accByPlayerID[sRec.PlayerID] = append(accByPlayerID[sRec.PlayerID], &candidateSplitEntry{
			summary:     sRec,
			fbTMWins:    sRec.TotalWinsAsTeammate,
			fbTMLosses:  sRec.TotalLossesAsTeammate,
			fbOppWins:   sRec.TotalWinsAsOpponent,
			fbOppLosses: sRec.TotalLossesAsOpponent,
		})
	}
	if len(nonLocalIDs) == 0 {
		return nil
	}

	if fromUnix > 0 || toUnix > 0 {
		if err := s.clearScopedFallbackIfMatchedInDB(ctx, nonLocalIDs, accByPlayerID); err != nil {
			return err
		}
	}

	var whereClauses []string
	var args []interface{}
	if len(nonLocalIDs) <= bulkPlayerIDFilterMax {
		placeholders := make([]string, len(nonLocalIDs))
		for i, pid := range nonLocalIDs {
			placeholders[i] = "?"
			args = append(args, pid)
		}
		whereClauses = append(whereClauses, fmt.Sprintf("mp.player_id IN (%s)", strings.Join(placeholders, ",")))
	}
	whereClauses, args = appendSplitScopeFilters(whereClauses, args, playlistIDs, fromUnix, toUnix)

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}
	querySQL := fmt.Sprintf(`
	SELECT mp.player_id, mp.team_num, mp.is_local, mp.won, mp.score, mp.goals, mp.assists, mp.saves, mp.shots, mp.demos, mp.stats_json,
	       m.match_guid, m.blue_score, m.orange_score, m.winner_team,
	       CASE WHEN TRIM(mp.stats_json) = '' OR TRIM(mp.stats_json) = '{}' THEN m.replay_stats_json ELSE '' END AS fallback_replay_stats,
	       COALESCE((SELECT lp.team_num FROM match_players lp WHERE lp.match_guid = m.match_guid AND lp.is_local = 1 LIMIT 1), -1) AS local_team
	FROM match_players mp
	JOIN matches m ON mp.match_guid = m.match_guid
	%s
	ORDER BY m.record_start_timestamp DESC;
	`, whereSQL)

	rows, err := s.db.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return wrapContextErr(fmt.Errorf("failed to query bulk player split stats: %w", err))
	}
	defer rows.Close()

	parsedReplayByGUID := make(map[string]*ReplayMatchStats)
	invalidReplayByGUID := make(map[string]bool)
	for rowIdx := 0; rows.Next(); rowIdx++ {
		if rowIdx&63 == 0 {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return wrapContextErr(ctxErr)
			}
		}
		var (
			mpPlayerID, statsJSON, matchGUID           string
			teamNum, isLocal, localTeam                int
			score, goals, assists, saves, shots, demos int
			wonVal, blueScore, orangeScore, winnerTeam sql.NullInt64
			fallbackReplayStats                        sql.NullString
		)
		if scanErr := rows.Scan(
			&mpPlayerID, &teamNum, &isLocal, &wonVal, &score, &goals, &assists, &saves, &shots, &demos, &statsJSON,
			&matchGUID, &blueScore, &orangeScore, &winnerTeam, &fallbackReplayStats, &localTeam,
		); scanErr != nil {
			return wrapContextErr(fmt.Errorf("failed to scan bulk player split stats row: %w", scanErr))
		}
		entries, ok := accByPlayerID[mpPlayerID]
		if !ok {
			continue
		}

		isTeammate, localWon, isDraw := classifyLocalOutcomeAndRole(teamNum, isLocal, localTeam, wonVal, blueScore, orangeScore, winnerTeam)
		lookupID := strings.TrimSpace(mpPlayerID)
		trimmedMPStats := strings.TrimSpace(statsJSON)
		hasDirectStats := trimmedMPStats != "" && trimmedMPStats != "{}"

		var cachedReplay *ReplayMatchStats
		if !hasDirectStats {
			if fb := strings.TrimSpace(fallbackReplayStats.String); fb != "" && fb != "{}" {
				if cached, exists := parsedReplayByGUID[matchGUID]; exists {
					cachedReplay = cached
				} else if !invalidReplayByGUID[matchGUID] {
					var parsed ReplayMatchStats
					if json.Unmarshal([]byte(fb), &parsed) == nil {
						cachedReplay = &parsed
						parsedReplayByGUID[matchGUID] = cachedReplay
					} else {
						invalidReplayByGUID[matchGUID] = true
					}
				}
			}
		}

		for _, entry := range entries {
			resolvedStatsJSON := ""
			if hasDirectStats {
				resolvedStatsJSON = trimmedMPStats
			} else if cachedReplay != nil {
				idForLookup := lookupID
				if idForLookup == "" {
					idForLookup = entry.summary.PlayerID
				}
				resolvedStatsJSON = ExtractPlayerStatsFromParsedReplay(cachedReplay, idForLookup, entry.summary.PlayerName, teamNum)
			}
			if isTeammate {
				entry.withAcc.AddMatchOutcome(localWon, isDraw)
				entry.withAcc.AddPlayerBoxAndTelemetry(score, goals, assists, saves, shots, demos, resolvedStatsJSON)
			} else {
				entry.againstAcc.AddMatchOutcome(localWon, isDraw)
				entry.againstAcc.AddPlayerBoxAndTelemetry(score, goals, assists, saves, shots, demos, resolvedStatsJSON)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return wrapContextErr(fmt.Errorf("bulk player split stats iteration error: %w", err))
	}

	for _, entries := range accByPlayerID {
		for _, entry := range entries {
			entry.summary.WithYouStats = entry.withAcc.Finalize(entry.fbTMWins, entry.fbTMLosses)
			entry.summary.AgainstYouStats = entry.againstAcc.Finalize(entry.fbOppWins, entry.fbOppLosses)
		}
	}
	return nil
}

func (s *SQLiteStore) clearScopedFallbackIfMatchedInDB(ctx context.Context, nonLocalIDs []string, accByPlayerID map[string][]*candidateSplitEntry) error {
	var (
		mpCheckSQL  = "SELECT DISTINCT player_id FROM match_players;"
		mpCheckArgs []interface{}
	)
	if len(nonLocalIDs) <= bulkPlayerIDFilterMax {
		placeholders := make([]string, len(nonLocalIDs))
		mpCheckArgs = make([]interface{}, len(nonLocalIDs))
		for i, pid := range nonLocalIDs {
			placeholders[i] = "?"
			mpCheckArgs[i] = pid
		}
		mpCheckSQL = fmt.Sprintf("SELECT DISTINCT player_id FROM match_players WHERE player_id IN (%s);", strings.Join(placeholders, ","))
	}
	mpRows, err := s.db.QueryContext(ctx, mpCheckSQL, mpCheckArgs...)
	if err != nil {
		return wrapContextErr(fmt.Errorf("failed to query match_players presence: %w", err))
	}
	defer mpRows.Close()
	for mpRows.Next() {
		var pid string
		if scanErr := mpRows.Scan(&pid); scanErr != nil {
			return wrapContextErr(fmt.Errorf("failed to scan match_players presence: %w", scanErr))
		}
		for _, entry := range accByPlayerID[pid] {
			entry.fbTMWins, entry.fbTMLosses, entry.fbOppWins, entry.fbOppLosses = 0, 0, 0, 0
		}
	}
	return wrapContextErr(mpRows.Err())
}

func appendSplitScopeFilters(whereClauses []string, args []interface{}, playlistIDs []int, fromUnix, toUnix int64) ([]string, []interface{}) {
	if len(playlistIDs) > 0 {
		placeholders := make([]string, len(playlistIDs))
		for i, pid := range playlistIDs {
			placeholders[i] = "?"
			args = append(args, pid)
		}
		whereClauses = append(whereClauses, fmt.Sprintf("m.playlist IN (%s)", strings.Join(placeholders, ",")))
	}
	if fromUnix > 0 {
		whereClauses = append(whereClauses, "m.record_start_timestamp >= ?")
		args = append(args, fromUnix)
	}
	if toUnix > 0 {
		whereClauses = append(whereClauses, "m.record_start_timestamp <= ?")
		args = append(args, toUnix)
	}
	return whereClauses, args
}

func classifyLocalOutcomeAndRole(teamNum, isLocal, localTeam int, wonVal, blueScore, orangeScore, winnerTeam sql.NullInt64) (isTeammate, localWon, isDraw bool) {
	if isLocal == 1 {
		localTeam = teamNum
	}
	if localTeam < 0 {
		localTeam = 0
	}
	isTeammate = teamNum == localTeam

	switch {
	case winnerTeam.Valid && winnerTeam.Int64 >= 0:
		localWon = int(winnerTeam.Int64) == localTeam
	case blueScore.Int64 != orangeScore.Int64:
		localWon = (localTeam == 0 && blueScore.Int64 > orangeScore.Int64) ||
			(localTeam == 1 && orangeScore.Int64 > blueScore.Int64)
	case wonVal.Valid:
		if isTeammate {
			localWon = wonVal.Int64 == 1
		} else {
			localWon = wonVal.Int64 == 0
		}
	default:
		isDraw = true
	}
	return isTeammate, localWon, isDraw
}

func isCandidateLocalAccount(playerID string, localAccounts map[string]bool) bool {
	trimmed := strings.TrimSpace(playerID)
	if trimmed == "" {
		return false
	}
	if strings.EqualFold(trimmed, "me") || strings.EqualFold(trimmed, "you") {
		return true
	}
	lower := strings.ToLower(trimmed)
	if localAccounts[lower] {
		return true
	}
	if strings.Contains(lower, "|") {
		parts := strings.Split(lower, "|")
		if len(parts) >= 2 && strings.TrimSpace(parts[1]) != "" && localAccounts[strings.TrimSpace(parts[1])] {
			return true
		}
	}
	return false
}

func scanPlayerSummaryRow(scanner rowScanner) (*PlayerSummary, error) {
	var (
		sRec      PlayerSummary
		notes     sql.NullString
		tagsStr   sql.NullString
		firstSeen int64
		lastSeen  int64
	)
	if err := scanner.Scan(
		&sRec.PlayerID, &sRec.Platform, &sRec.PlayerName, &sRec.RanksJSON, &notes, &tagsStr, &firstSeen, &lastSeen,
		&sRec.TotalWinsAsTeammate, &sRec.TotalLossesAsTeammate,
		&sRec.TotalWinsAsOpponent, &sRec.TotalLossesAsOpponent, &sRec.TotalMatches,
	); err != nil {
		return nil, err
	}
	sRec.Notes = notes.String
	sRec.Tags = parseTags(tagsStr.String)
	sRec.FirstSeenAt = time.Unix(firstSeen, 0).UTC()
	sRec.LastSeenAt = time.Unix(lastSeen, 0).UTC()
	return &sRec, nil
}
