package storage

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

type repairedMatchCandidate struct {
	guid          string
	startTS       int64
	playlist      int
	ballchasingID string
	blueScore     int
	orangeScore   int
	winnerTeam    int
	statsJSON     string
}

// RepairHistoricalData runs full historical match, matchup, and ranks_json repair on the SQLite database.
func (s *SQLiteStore) RepairHistoricalData(ctx context.Context) error {
	if err := s.repairHistoricalMatchesAndOutcomes(ctx); err != nil {
		return err
	}
	s.repairCorruptedRanksJSON(ctx, true)
	return nil
}

func (s *SQLiteStore) repairHistoricalMatchesAndOutcomes(ctx context.Context) error {
	// 1. Normalize Ballchasing links and preserve SYNCED 0-player replays (017db99).
	_, _ = s.db.ExecContext(ctx, `
		UPDATE matches
		SET ballchasing_url = 'https://ballchasing.com/replay/' || ballchasing_id
		WHERE ballchasing_id != '' AND (ballchasing_url LIKE '%/api/replays/%' OR ballchasing_url = '');
	`)
	_, _ = s.db.ExecContext(ctx, `
		UPDATE matches
		SET upload_status = 'UPLOADED'
		WHERE ballchasing_id != '' AND (upload_status = '' OR upload_status = 'PENDING' OR upload_status = 'FAILED');
	`)
	_, _ = s.db.ExecContext(ctx, `
		UPDATE matches
		SET stats_sync_status = 'SYNCED', stats_retry_count = 0
		WHERE ballchasing_id != ''
		  AND stats_sync_status = 'PENDING'
		  AND synced_at IS NOT NULL
		  AND synced_at > 0
		  AND replay_stats_json LIKE '%"ballchasing_id":%'
		  AND (replay_stats_json LIKE '%"players":null%' OR replay_stats_json LIKE '%"players":[]%' OR replay_stats_json LIKE '%"boost":%');
	`)

	// 2. Repair 0-0 match scores derived from player goals and backfill matchups.
	if err := s.repairZeroScoreMatches(ctx); err != nil {
		return err
	}

	// 3. Remove local accounts from players directory tables.
	localAccounts := s.getLocalAccountIDs(ctx)
	for accLower := range localAccounts {
		_, _ = s.db.ExecContext(ctx, "UPDATE match_players SET is_local = 1 WHERE LOWER(player_id) = ?;", accLower)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM player_matchups WHERE LOWER(player_id) = ?;", accLower)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM players WHERE LOWER(player_id) = ?;", accLower)
	}

	// 4. One-time v4 migration: repair timestamps/players, deduplicate aliases, rebuild matchups, mark boost-less non-empty replays PENDING.
	var userVersion int
	_ = s.db.QueryRowContext(ctx, "PRAGMA user_version;").Scan(&userVersion)
	if userVersion < 4 {
		s.backfillMissingReplayPlayersAndTimestamps(ctx, localAccounts)
		s.deduplicateConsoleAliases(ctx)
		s.rebuildMatchupsFromMatchPlayers(ctx)

		_, _ = s.db.ExecContext(ctx, `
			UPDATE matches
			SET stats_sync_status = 'PENDING', stats_retry_count = 0
			WHERE ballchasing_id != ''
			  AND stats_sync_status = 'SYNCED'
			  AND replay_stats_json LIKE '%"players":[{%'
			  AND replay_stats_json NOT LIKE '%"boost":%';
		`)
		_, _ = s.db.ExecContext(ctx, "PRAGMA user_version = 4;")
	}

	// 5. Repair wall-clock player timestamps using match start timestamps.
	_, _ = s.db.ExecContext(ctx, `
		UPDATE players
		SET first_seen_at = (
				SELECT MIN(m.record_start_timestamp)
				FROM match_players mp JOIN matches m ON mp.match_guid = m.match_guid
				WHERE mp.player_id = players.player_id AND m.record_start_timestamp > 0
			),
			last_seen_at = (
				SELECT MAX(m.record_start_timestamp)
				FROM match_players mp JOIN matches m ON mp.match_guid = m.match_guid
				WHERE mp.player_id = players.player_id AND m.record_start_timestamp > 0
			)
		WHERE EXISTS (
			SELECT 1 FROM match_players mp JOIN matches m ON mp.match_guid = m.match_guid
			WHERE mp.player_id = players.player_id AND m.record_start_timestamp > 0
			GROUP BY mp.player_id
			HAVING players.last_seen_at > MAX(m.record_start_timestamp) + 3600
		);
	`)

	// 6. Repair corrupted legacy ranks_json values & backfill missing match_players.stats_json.
	s.repairCorruptedRanksJSON(ctx, false)
	s.backfillMatchPlayerStatsJSON(ctx)
	return nil
}

func (s *SQLiteStore) repairZeroScoreMatches(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT match_guid, record_start_timestamp, playlist, ballchasing_id, replay_stats_json
		FROM matches
		WHERE blue_score = 0 AND orange_score = 0 AND replay_stats_json != '';
	`)
	if err != nil {
		return err
	}
	type rawCand struct {
		guid, ballchasingID, statsJSON string
		startTS                        int64
		playlist                       int
	}
	var cands []rawCand
	for rows.Next() {
		var c rawCand
		if rows.Scan(&c.guid, &c.startTS, &c.playlist, &c.ballchasingID, &c.statsJSON) == nil {
			cands = append(cands, c)
		}
	}
	_ = rows.Close()

	var toRepair []repairedMatchCandidate
	for _, c := range cands {
		var st ReplayMatchStats
		if json.Unmarshal([]byte(c.statsJSON), &st) != nil {
			continue
		}
		if len(st.Players) == 0 {
			st.Players = s.loadReplayPlayersFromMatchPlayers(ctx, c.guid)
			if len(st.Players) == 0 {
				continue
			}
		}
		bGoals, oGoals := 0, 0
		for _, p := range st.Players {
			if p.Team == 0 {
				bGoals += p.Goals
			} else if p.Team == 1 {
				oGoals += p.Goals
			}
		}
		if bGoals == 0 && oGoals == 0 {
			continue
		}
		st.BlueScore, st.OrangeScore = bGoals, oGoals
		winner := -1
		if bGoals > oGoals {
			winner = 0
			st.WinnerTeam = &winner
		} else if oGoals > bGoals {
			winner = 1
			st.WinnerTeam = &winner
		}
		st.BallchasingURL = NormalizeBallchasingURL(st.BallchasingID, st.BallchasingURL)
		updatedJSON, mErr := json.Marshal(&st)
		if mErr != nil {
			continue
		}
		toRepair = append(toRepair, repairedMatchCandidate{
			guid: c.guid, startTS: c.startTS, playlist: c.playlist, ballchasingID: c.ballchasingID,
			blueScore: bGoals, orangeScore: oGoals, winnerTeam: winner, statsJSON: string(updatedJSON),
		})
	}
	if len(toRepair) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmtMatch, err := tx.PrepareContext(ctx, `UPDATE matches SET blue_score = ?, orange_score = ?, winner_team = ?, replay_stats_json = ? WHERE match_guid = ?;`)
	if err != nil {
		return err
	}
	defer stmtMatch.Close()

	stmtMP, err := tx.PrepareContext(ctx, `UPDATE match_players SET won = CASE WHEN team_num = ? THEN 1 ELSE 0 END WHERE match_guid = ?;`)
	if err != nil {
		return err
	}
	defer stmtMP.Close()

	for _, r := range toRepair {
		_, _ = stmtMatch.ExecContext(ctx, r.blueScore, r.orangeScore, r.winnerTeam, r.statsJSON, r.guid)
		if r.winnerTeam >= 0 {
			_, _ = stmtMP.ExecContext(ctx, r.winnerTeam, r.guid)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	s.backfillOutcomesForRepairedMatches(ctx, toRepair)
	return nil
}

func (s *SQLiteStore) loadReplayPlayersFromMatchPlayers(ctx context.Context, guid string) []ReplayPlayerStats {
	mpRows, err := s.db.QueryContext(ctx, `
		SELECT mp.player_id, mp.team_num, mp.is_local, mp.score, mp.goals, mp.assists, mp.saves, mp.shots, mp.demos, mp.car_name, COALESCE(p.player_name, ''), COALESCE(p.platform, '')
		FROM match_players mp LEFT JOIN players p ON mp.player_id = p.player_id
		WHERE mp.match_guid = ? ORDER BY mp.team_num ASC, mp.score DESC;
	`, guid)
	if err != nil {
		return nil
	}
	defer mpRows.Close()

	var out []ReplayPlayerStats
	for mpRows.Next() {
		var (
			pid, carName, pName, plat        string
			team, isLoc, sc, g, a, sv, sh, d int
		)
		if mpRows.Scan(&pid, &team, &isLoc, &sc, &g, &a, &sv, &sh, &d, &carName, &pName, &plat) == nil {
			onlineID := pid
			if parts := strings.Split(pid, "|"); len(parts) >= 2 {
				if plat == "" && parts[0] != "" {
					plat = parts[0]
				}
				onlineID = parts[1]
			}
			if pName == "" {
				pName = onlineID
			}
			out = append(out, ReplayPlayerStats{
				Name: pName, Platform: plat, OnlineID: onlineID, Team: team, IsLocal: isLoc == 1,
				Score: sc, Goals: g, Assists: a, Saves: sv, Shots: sh, Demos: d, CarName: carName,
			})
		}
	}
	return out
}

func (s *SQLiteStore) backfillOutcomesForRepairedMatches(ctx context.Context, repaired []repairedMatchCandidate) {
	for _, r := range repaired {
		if r.winnerTeam < 0 || r.playlist <= 0 || r.ballchasingID == "" {
			continue
		}
		mpRows, err := s.db.QueryContext(ctx, `
			SELECT mp.player_id, mp.team_num, mp.is_local, COALESCE(p.platform, ''), COALESCE(p.player_name, '')
			FROM match_players mp LEFT JOIN players p ON mp.player_id = p.player_id
			WHERE mp.match_guid = ?;
		`, r.guid)
		if err != nil {
			continue
		}
		type mpInfo struct {
			playerID, platform, name string
			teamNum                  int
			isLocal                  bool
		}
		var participants []mpInfo
		localTeam := -1
		for mpRows.Next() {
			var info mpInfo
			var isLoc int
			if mpRows.Scan(&info.playerID, &info.teamNum, &isLoc, &info.platform, &info.name) == nil {
				info.isLocal = (isLoc == 1)
				if info.isLocal && localTeam == -1 {
					localTeam = info.teamNum
				}
				participants = append(participants, info)
			}
		}
		_ = mpRows.Close()
		if localTeam < 0 {
			continue
		}
		var outcomes []PlayerOutcome
		for _, p := range participants {
			if p.isLocal || strings.HasSuffix(p.playerID, "|0|0") {
				continue
			}
			outcomes = append(outcomes, PlayerOutcome{
				PlayerID: p.playerID, Platform: p.platform, PlayerName: p.name,
				IsTeammate: p.teamNum == localTeam, Won: localTeam == r.winnerTeam,
			})
		}
		if len(outcomes) > 0 {
			_ = s.RecordMatchResults(ctx, r.guid, r.playlist, outcomes, time.Unix(r.startTS, 0).UTC())
		}
	}
}

func (s *SQLiteStore) backfillMissingReplayPlayersAndTimestamps(ctx context.Context, localAccounts map[string]bool) {
	mjRows, err := s.db.QueryContext(ctx, `
		SELECT m.match_guid, m.record_start_timestamp, COALESCE(m.winner_team, -1), m.replay_stats_json,
		       (SELECT COUNT(*) FROM match_players mp WHERE mp.match_guid = m.match_guid),
		       (SELECT COUNT(*) FROM match_players mp JOIN players p ON mp.player_id = p.player_id WHERE mp.match_guid = m.match_guid AND mp.is_local = 0)
		FROM matches m WHERE m.replay_stats_json != '';
	`)
	if err != nil {
		return
	}
	type mJsonRow struct {
		guid, statsJSON                        string
		startTS                                int64
		winnerTeam, mpCount, nonLocalInPlayers int
	}
	var rows []mJsonRow
	for mjRows.Next() {
		var r mJsonRow
		if mjRows.Scan(&r.guid, &r.startTS, &r.winnerTeam, &r.statsJSON, &r.mpCount, &r.nonLocalInPlayers) == nil {
			rows = append(rows, r)
		}
	}
	_ = mjRows.Close()

	for _, r := range rows {
		var st ReplayMatchStats
		if json.Unmarshal([]byte(r.statsJSON), &st) != nil {
			continue
		}
		matchTS := r.startTS
		if st.Date != "" {
			if parsed, ok := ParseReplayDate(st.Date); ok && parsed.Unix() > 0 && (matchTS <= 0 || matchTS-parsed.Unix() > 86400) {
				matchTS = parsed.Unix()
				_, _ = s.db.ExecContext(ctx, "UPDATE matches SET record_start_timestamp = ? WHERE match_guid = ?;", matchTS, r.guid)
			}
		}
		if r.mpCount >= len(st.Players) && r.nonLocalInPlayers >= len(st.Players)-1 && len(st.Players) > 0 {
			continue
		}
		for _, p := range st.Players {
			pid := strings.TrimSpace(p.OnlineID)
			if (pid == "" || pid == "0") && strings.TrimSpace(p.Name) != "" {
				pid = strings.TrimSpace(p.Name)
			}
			if pid == "" {
				continue
			}
			plat := strings.TrimSpace(p.Platform)
			if plat == "" {
				plat = "Unknown"
			}
			canonicalID := s.resolvePlayerIDForMatch(ctx, r.guid, pid, plat, p.Name, p.Team, true)
			isLocal := localAccounts[strings.ToLower(pid)] || localAccounts[strings.ToLower(canonicalID)]
			if !isLocal {
				_, _ = s.db.ExecContext(ctx, `
					INSERT INTO players (player_id, platform, player_name, player_name_lower, ranks_json, first_seen_at, last_seen_at)
					VALUES (?, ?, ?, ?, '{}', ?, ?)
					ON CONFLICT(player_id) DO UPDATE SET
						first_seen_at = MIN(players.first_seen_at, excluded.first_seen_at),
						last_seen_at = MAX(players.last_seen_at, excluded.last_seen_at);
				`, canonicalID, plat, strings.TrimSpace(p.Name), strings.ToLower(strings.TrimSpace(p.Name)), matchTS, matchTS)
			}
			var wonVal *int
			if r.winnerTeam >= 0 {
				w := 0
				if r.winnerTeam == p.Team {
					w = 1
				}
				wonVal = &w
			}
			isLocInt := 0
			if isLocal {
				isLocInt = 1
			}
			_, _ = s.db.ExecContext(ctx, `
				INSERT INTO match_players (match_guid, player_id, team_num, is_local, won, score, goals, assists, saves, shots, demos, car_name, stats_json)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(match_guid, player_id) DO UPDATE SET
					stats_json = CASE WHEN match_players.stats_json = '' THEN excluded.stats_json ELSE match_players.stats_json END;
			`, r.guid, canonicalID, p.Team, isLocInt, wonVal, p.Score, p.Goals, p.Assists, p.Saves, p.Shots, p.Demos, strings.TrimSpace(p.CarName), marshalTelemetryIfPresent(p))
		}
	}
}

func (s *SQLiteStore) deduplicateConsoleAliases(ctx context.Context) {
	type pRow struct {
		id, platform, name string
		matches            int
	}
	if pRows, err := s.db.QueryContext(ctx, `
		SELECT p.player_id, p.platform, p.player_name, COALESCE(SUM(m.total_matches), 0)
		FROM players p LEFT JOIN player_matchups m ON p.player_id = m.player_id GROUP BY p.player_id;
	`); err == nil {
		byToken := make(map[string][]pRow)
		for pRows.Next() {
			var r pRow
			if pRows.Scan(&r.id, &r.platform, &r.name, &r.matches) == nil {
				if parts := strings.Split(r.id, "|"); len(parts) >= 2 {
					if tok := strings.ToLower(strings.TrimSpace(parts[1])); tok != "" && tok != "0" {
						byToken[tok] = append(byToken[tok], r)
					}
				}
			}
		}
		_ = pRows.Close()
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
					s.mergeDuplicatePlayerIDs(ctx, other.id, canon.id)
				}
			}
		}
	}

	if dupRows, err := s.db.QueryContext(ctx, `
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
	`); err == nil {
		type dupPair struct{ dupID, canonID string }
		var pairs []dupPair
		for dupRows.Next() {
			var cID, dID string
			if dupRows.Scan(&cID, &dID) == nil {
				pairs = append(pairs, dupPair{dupID: dID, canonID: cID})
			}
		}
		_ = dupRows.Close()
		for _, pair := range pairs {
			s.mergeDuplicatePlayerIDs(ctx, pair.dupID, pair.canonID)
		}
	}
}

func (s *SQLiteStore) mergeDuplicatePlayerIDs(ctx context.Context, dupID, canonID string) {
	if dupID == "" || canonID == "" || dupID == canonID {
		return
	}
	_, _ = s.db.ExecContext(ctx, `
		UPDATE match_players
		SET car_name = CASE WHEN match_players.car_name != '' THEN match_players.car_name ELSE COALESCE((SELECT d.car_name FROM match_players d WHERE d.match_guid = match_players.match_guid AND d.player_id = ?), '') END,
		    stats_json = CASE WHEN match_players.stats_json != '' THEN match_players.stats_json ELSE COALESCE((SELECT d.stats_json FROM match_players d WHERE d.match_guid = match_players.match_guid AND d.player_id = ?), '') END
		WHERE player_id = ?;
	`, dupID, dupID, canonID)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM match_players WHERE player_id = ? AND EXISTS (SELECT 1 FROM match_players c WHERE c.match_guid = match_players.match_guid AND c.player_id = ?);`, dupID, canonID)
	_, _ = s.db.ExecContext(ctx, `UPDATE match_players SET player_id = ? WHERE player_id = ?;`, canonID, dupID)
	_, _ = s.db.ExecContext(ctx, `UPDATE players SET ranks_json = CASE WHEN (players.ranks_json = '' OR players.ranks_json = '{}') THEN COALESCE((SELECT d.ranks_json FROM players d WHERE d.player_id = ?), '{}') ELSE players.ranks_json END WHERE player_id = ?;`, dupID, canonID)
	_, _ = s.db.ExecContext(ctx, "DELETE FROM player_matchups WHERE player_id = ?;", dupID)
	_, _ = s.db.ExecContext(ctx, "DELETE FROM players WHERE player_id = ?;", dupID)
}

func (s *SQLiteStore) rebuildMatchupsFromMatchPlayers(ctx context.Context) {
	var hasMatchPlayersWithWinner int
	_ = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM matches m
		WHERE m.winner_team >= 0 AND EXISTS (SELECT 1 FROM match_players lp WHERE lp.match_guid = m.match_guid AND lp.is_local = 1);
	`).Scan(&hasMatchPlayersWithWinner)
	if hasMatchPlayersWithWinner <= 0 {
		return
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()

	_, _ = tx.ExecContext(ctx, `
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
	`)
	_, _ = tx.ExecContext(ctx, "DELETE FROM player_matchups;")
	_, _ = tx.ExecContext(ctx, "DELETE FROM processed_match_outcomes;")
	_, _ = tx.ExecContext(ctx, `
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
	`)
	_, _ = tx.ExecContext(ctx, `
		INSERT INTO processed_match_outcomes (match_guid, playlist_id, processed_at)
		SELECT m.match_guid, m.playlist, m.record_start_timestamp FROM matches m
		WHERE m.winner_team >= 0
		  AND EXISTS (SELECT 1 FROM match_players lp WHERE lp.match_guid = m.match_guid AND lp.is_local = 1)
		  AND EXISTS (SELECT 1 FROM match_players mp WHERE mp.match_guid = m.match_guid AND mp.is_local = 0 AND mp.player_id NOT LIKE '%|0|0')
		ON CONFLICT(match_guid) DO NOTHING;
	`)
	_, _ = tx.ExecContext(ctx, `
		DELETE FROM players
		WHERE NOT EXISTS (SELECT 1 FROM player_matchups pm WHERE pm.player_id = players.player_id)
		  AND NOT EXISTS (SELECT 1 FROM match_players mp WHERE mp.player_id = players.player_id);
	`)
	_ = tx.Commit()
}

func (s *SQLiteStore) repairCorruptedRanksJSON(ctx context.Context, forceAll bool) {
	rows, err := s.db.QueryContext(ctx, `SELECT player_id, ranks_json FROM players WHERE ranks_json != '' AND ranks_json != '{}';`)
	if err != nil {
		return
	}
	type rankRow struct{ playerID, ranksJSON string }
	var toCheck []rankRow
	for rows.Next() {
		var r rankRow
		if rows.Scan(&r.playerID, &r.ranksJSON) == nil {
			toCheck = append(toCheck, r)
		}
	}
	_ = rows.Close()

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
			if updated, err := json.Marshal(ranks); err == nil {
				_, _ = s.db.ExecContext(ctx, `UPDATE players SET ranks_json = ? WHERE player_id = ?;`, string(updated), row.playerID)
			}
		}
	}
}

func (s *SQLiteStore) backfillMatchPlayerStatsJSON(ctx context.Context) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT mp.match_guid, mp.player_id, mp.team_num, COALESCE(p.player_name, ''), m.replay_stats_json
		FROM match_players mp JOIN matches m ON mp.match_guid = m.match_guid LEFT JOIN players p ON mp.player_id = p.player_id
		WHERE (mp.stats_json = '' OR mp.stats_json = '{}') AND m.replay_stats_json LIKE '%"boost":%';
	`)
	if err != nil {
		return
	}
	type mpBackfill struct{ guid, playerID, statsJSON string }
	var backfills []mpBackfill
	for rows.Next() {
		var guid, pid, pName, replayJSON string
		var teamNum int
		if rows.Scan(&guid, &pid, &teamNum, &pName, &replayJSON) == nil {
			if extracted := ExtractPlayerStatsJSONFromReplay("", replayJSON, pid, pName, teamNum); extracted != "" {
				backfills = append(backfills, mpBackfill{guid: guid, playerID: pid, statsJSON: extracted})
			}
		}
	}
	_ = rows.Close()
	for _, bf := range backfills {
		_, _ = s.db.ExecContext(ctx, `UPDATE match_players SET stats_json = ? WHERE match_guid = ? AND player_id = ?;`, bf.statsJSON, bf.guid, bf.playerID)
	}
}
