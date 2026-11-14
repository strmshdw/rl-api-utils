package storage

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
)

func (s *SQLiteStore) deduplicateConsoleAliases(ctx context.Context) error {
	type pRow struct {
		id, platform, name string
		matches            int
	}
	pRows, err := s.db.QueryContext(ctx, `
		SELECT p.player_id, p.platform, p.player_name, COALESCE(SUM(m.total_matches), 0)
		FROM players p LEFT JOIN player_matchups m ON p.player_id = m.player_id GROUP BY p.player_id;
	`)
	if err != nil {
		return err
	}
	byToken := make(map[string][]pRow)
	for pRows.Next() {
		var r pRow
		if scanErr := pRows.Scan(&r.id, &r.platform, &r.name, &r.matches); scanErr != nil {
			_ = pRows.Close()
			return scanErr
		}
		if parts := strings.Split(r.id, "|"); len(parts) >= 2 {
			if tok := strings.ToLower(strings.TrimSpace(parts[1])); tok != "" && tok != "0" {
				byToken[tok] = append(byToken[tok], r)
			}
		}
	}
	pRowsErr := pRows.Err()
	_ = pRows.Close()
	if pRowsErr != nil {
		return pRowsErr
	}
	for _, group := range byToken {
		if len(group) < 2 {
			continue
		}
		canonIdx := 0
		for i := 1; i < len(group); i++ {
			if group[i].matches > group[canonIdx].matches ||
				(group[i].matches == group[canonIdx].matches && (group[i].platform == "PS4" || group[i].platform == "XboxOne")) {
				canonIdx = i
			}
		}
		canon := group[canonIdx]
		for i, other := range group {
			if i != canonIdx && SamePlatformFamily(canon.platform, other.platform) {
				if err := s.mergeDuplicatePlayerIDs(ctx, other.id, canon.id); err != nil {
					return err
				}
			}
		}
	}

	dupRows, err := s.db.QueryContext(ctx, `
		SELECT mp1.player_id, mp2.player_id
		FROM match_players mp1
		JOIN match_players mp2
		  ON mp1.match_guid = mp2.match_guid AND mp1.team_num = mp2.team_num
		 AND mp1.player_id != mp2.player_id AND mp1.is_local = 0 AND mp2.is_local = 0
		 AND mp1.score = mp2.score AND mp1.goals = mp2.goals AND mp1.assists = mp2.assists AND mp1.saves = mp2.saves AND mp1.shots = mp2.shots
		JOIN players p1 ON mp1.player_id = p1.player_id
		JOIN players p2 ON mp2.player_id = p2.player_id
		WHERE p1.player_name_lower != '' AND p1.player_name_lower = p2.player_name_lower
		  AND (mp1.player_id LIKE 'PS4|%' OR mp1.player_id LIKE 'XboxOne|%')
		  AND (mp2.player_id LIKE 'PlayStation|%' OR mp2.player_id LIKE 'Xbox|%')
		GROUP BY mp1.player_id, mp2.player_id;
	`)
	if err != nil {
		return err
	}
	type dupPair struct{ dupID, canonID string }
	var pairs []dupPair
	for dupRows.Next() {
		var cID, dID string
		if scanErr := dupRows.Scan(&cID, &dID); scanErr != nil {
			_ = dupRows.Close()
			return scanErr
		}
		pairs = append(pairs, dupPair{dupID: dID, canonID: cID})
	}
	dupRowsErr := dupRows.Err()
	_ = dupRows.Close()
	if dupRowsErr != nil {
		return dupRowsErr
	}
	for _, pair := range pairs {
		if err := s.mergeDuplicatePlayerIDs(ctx, pair.dupID, pair.canonID); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) mergeDuplicatePlayerIDs(ctx context.Context, dupID, canonID string) error {
	if dupID == "" || canonID == "" || dupID == canonID {
		return nil
	}
	stmts := []struct {
		query string
		args  []any
	}{
		{
			query: `
		UPDATE match_players
		SET car_name = CASE WHEN match_players.car_name != '' THEN match_players.car_name ELSE COALESCE((SELECT d.car_name FROM match_players d WHERE d.match_guid = match_players.match_guid AND d.player_id = ?), '') END,
		    stats_json = CASE WHEN match_players.stats_json != '' THEN match_players.stats_json ELSE COALESCE((SELECT d.stats_json FROM match_players d WHERE d.match_guid = match_players.match_guid AND d.player_id = ?), '') END
		WHERE player_id = ?;
	`,
			args: []any{dupID, dupID, canonID},
		},
		{
			query: `DELETE FROM match_players WHERE player_id = ? AND EXISTS (SELECT 1 FROM match_players c WHERE c.match_guid = match_players.match_guid AND c.player_id = ?);`,
			args:  []any{dupID, canonID},
		},
		{query: `UPDATE match_players SET player_id = ? WHERE player_id = ?;`, args: []any{canonID, dupID}},
		{
			query: `UPDATE players SET ranks_json = CASE WHEN (players.ranks_json = '' OR players.ranks_json = '{}') THEN COALESCE((SELECT d.ranks_json FROM players d WHERE d.player_id = ?), '{}') ELSE players.ranks_json END WHERE player_id = ?;`,
			args:  []any{dupID, canonID},
		},
		{query: "DELETE FROM player_matchups WHERE player_id = ?;", args: []any{dupID}},
		{query: "DELETE FROM players WHERE player_id = ?;", args: []any{dupID}},
	}
	for _, st := range stmts {
		if _, err := s.db.ExecContext(ctx, st.query, st.args...); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) rebuildMatchupsFromMatchPlayers(ctx context.Context) error {
	var hasMatchPlayersWithWinner int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM matches m
		WHERE m.winner_team >= 0 AND EXISTS (SELECT 1 FROM match_players lp WHERE lp.match_guid = m.match_guid AND lp.is_local = 1);
	`).Scan(&hasMatchPlayersWithWinner); err != nil {
		return err
	}
	if hasMatchPlayersWithWinner <= 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	queries := []string{
		`
		INSERT INTO players (player_id, platform, player_name, player_name_lower, ranks_json, first_seen_at, last_seen_at)
		SELECT
			mp.player_id,
			CASE WHEN INSTR(mp.player_id, '|') > 1 THEN SUBSTR(mp.player_id, 1, INSTR(mp.player_id, '|') - 1) ELSE 'Unknown' END,
			CASE WHEN INSTR(mp.player_id, '|') > 1 THEN SUBSTR(mp.player_id, INSTR(mp.player_id, '|') + 1, LENGTH(mp.player_id) - INSTR(mp.player_id, '|') - 2) ELSE mp.player_id END,
			LOWER(CASE WHEN INSTR(mp.player_id, '|') > 1 THEN SUBSTR(mp.player_id, INSTR(mp.player_id, '|') + 1, LENGTH(mp.player_id) - INSTR(mp.player_id, '|') - 2) ELSE mp.player_id END),
			'{}', COALESCE(MIN(m.record_start_timestamp), 0), COALESCE(MAX(m.record_start_timestamp), 0)
		FROM match_players mp JOIN matches m ON mp.match_guid = m.match_guid
		WHERE mp.is_local = 0 AND mp.player_id != '' AND mp.player_id != '0' AND mp.player_id NOT LIKE '%|0|0'
		  AND NOT EXISTS (SELECT 1 FROM players p WHERE p.player_id = mp.player_id)
		GROUP BY mp.player_id;
	`,
		"DELETE FROM player_matchups;",
		"DELETE FROM processed_match_outcomes;",
		`
		INSERT INTO player_matchups (player_id, playlist_id, wins_as_teammate, losses_as_teammate, wins_as_opponent, losses_as_opponent, total_matches, last_played_at)
		SELECT mp.player_id, m.playlist,
			COALESCE(SUM(CASE WHEN mp.team_num = lp.team_num AND lp.team_num = m.winner_team THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN mp.team_num = lp.team_num AND lp.team_num != m.winner_team THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN mp.team_num != lp.team_num AND lp.team_num = m.winner_team THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN mp.team_num != lp.team_num AND lp.team_num != m.winner_team THEN 1 ELSE 0 END), 0),
			COUNT(*), COALESCE(MAX(m.record_start_timestamp), 0)
		FROM match_players mp
		JOIN matches m ON mp.match_guid = m.match_guid
		JOIN (SELECT match_guid, MIN(team_num) AS team_num FROM match_players WHERE is_local = 1 GROUP BY match_guid) lp ON lp.match_guid = m.match_guid
		JOIN players p ON p.player_id = mp.player_id
		WHERE m.winner_team >= 0 AND mp.is_local = 0 AND mp.player_id NOT LIKE '%|0|0'
		GROUP BY mp.player_id, m.playlist;
	`,
		`
		INSERT INTO processed_match_outcomes (match_guid, playlist_id, processed_at)
		SELECT m.match_guid, m.playlist, m.record_start_timestamp FROM matches m
		WHERE m.winner_team >= 0
		  AND EXISTS (SELECT 1 FROM match_players lp WHERE lp.match_guid = m.match_guid AND lp.is_local = 1)
		  AND EXISTS (SELECT 1 FROM match_players mp WHERE mp.match_guid = m.match_guid AND mp.is_local = 0 AND mp.player_id NOT LIKE '%|0|0')
		ON CONFLICT(match_guid) DO NOTHING;
	`,
		`
		DELETE FROM players
		WHERE NOT EXISTS (SELECT 1 FROM player_matchups pm WHERE pm.player_id = players.player_id)
		  AND NOT EXISTS (SELECT 1 FROM match_players mp WHERE mp.player_id = players.player_id);
	`,
	}
	for _, q := range queries {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) repairCorruptedRanksJSON(ctx context.Context, forceAll bool) error {
	rows, err := s.db.QueryContext(ctx, `SELECT player_id, ranks_json FROM players WHERE ranks_json != '' AND ranks_json != '{}';`)
	if err != nil {
		return err
	}
	type rankRow struct{ playerID, ranksJSON string }
	var toCheck []rankRow
	for rows.Next() {
		var r rankRow
		if scanErr := rows.Scan(&r.playerID, &r.ranksJSON); scanErr != nil {
			_ = rows.Close()
			return scanErr
		}
		toCheck = append(toCheck, r)
	}
	rowsErr := rows.Err()
	_ = rows.Close()
	if rowsErr != nil {
		return rowsErr
	}

	for _, row := range toCheck {
		var ranks map[string]map[string]interface{}
		if json.Unmarshal([]byte(row.ranksJSON), &ranks) != nil || len(ranks) == 0 {
			continue
		}
		changed := false
		for pKey, entry := range ranks {
			if entry == nil {
				continue
			}
			tier, division := 0, 0
			if tVal, ok := entry["tier"].(float64); ok {
				tier = int(tVal)
			}
			if dVal, ok := entry["division"].(float64); ok {
				division = int(dVal)
			}
			if tier < 0 || tier > 22 {
				continue
			}
			rawName, hasRankName := entry["rank_name"].(string)
			if !hasRankName && !forceAll {
				continue
			}
			effDiv := Resolve1BasedDivision(division, rawName)
			expectedBase := FormatRankName(tier, 0)
			expectedFull := FormatRankName(tier, effDiv)
			expectedShort := FormatShortRankName(tier, effDiv)

			rawBase := strings.TrimSpace(rawName)
			if idx := strings.Index(rawBase, " Div "); idx >= 0 {
				rawBase = strings.TrimSpace(rawBase[:idx])
			} else if idx := strings.Index(rawBase, " Division "); idx >= 0 {
				rawBase = strings.TrimSpace(rawBase[:idx])
			}

			if !hasRankName || rawName == "" || strings.HasPrefix(rawName, "Tier ") || rawBase != expectedBase {
				entry["rank_name"] = expectedFull
				entry["short_rank"] = expectedShort
				if _, hasPID := entry["playlist_id"]; !hasPID {
					if pid, pErr := strconv.Atoi(pKey); pErr == nil && pid > 0 {
						entry["playlist_id"] = pid
						entry["playlist_name"] = FormatPlaylistName(pid)
					}
				}
				changed = true
			} else if curShort, ok := entry["short_rank"].(string); !ok || curShort == "" || curShort != expectedShort {
				entry["short_rank"] = expectedShort
				changed = true
			}
		}
		if changed {
			updated, mErr := json.Marshal(ranks)
			if mErr != nil {
				return mErr
			}
			if _, err := s.db.ExecContext(ctx, `UPDATE players SET ranks_json = ? WHERE player_id = ?;`, string(updated), row.playerID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *SQLiteStore) backfillMatchPlayerStatsJSON(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT mp.match_guid, mp.player_id, mp.team_num, COALESCE(p.player_name, ''), m.replay_stats_json
		FROM match_players mp JOIN matches m ON mp.match_guid = m.match_guid LEFT JOIN players p ON mp.player_id = p.player_id
		WHERE (mp.stats_json = '' OR mp.stats_json = '{}') AND m.replay_stats_json LIKE '%"boost":%';
	`)
	if err != nil {
		return err
	}
	type mpBackfill struct{ guid, playerID, statsJSON string }
	var backfills []mpBackfill
	for rows.Next() {
		var guid, pid, pName, replayJSON string
		var teamNum int
		if scanErr := rows.Scan(&guid, &pid, &teamNum, &pName, &replayJSON); scanErr != nil {
			_ = rows.Close()
			return scanErr
		}
		if extracted := ExtractPlayerStatsJSONFromReplay("", replayJSON, pid, pName, teamNum); extracted != "" {
			backfills = append(backfills, mpBackfill{guid: guid, playerID: pid, statsJSON: extracted})
		}
	}
	rowsErr := rows.Err()
	_ = rows.Close()
	if rowsErr != nil {
		return rowsErr
	}
	for _, bf := range backfills {
		if _, err := s.db.ExecContext(ctx, `UPDATE match_players SET stats_json = ? WHERE match_guid = ? AND player_id = ?;`, bf.statsJSON, bf.guid, bf.playerID); err != nil {
			return err
		}
	}
	return nil
}
