package storage

import (
	"context"
	"encoding/json"
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
	return s.repairCorruptedRanksJSON(ctx, true)
}

func (s *SQLiteStore) execSQLQueries(ctx context.Context, queries []string, args ...any) error {
	for _, q := range queries {
		if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) repairHistoricalMatchesAndOutcomes(ctx context.Context) error {
	// 1. Normalize Ballchasing links and preserve SYNCED 0-player replays (017db99).
	if err := s.execSQLQueries(ctx, []string{
		`
		UPDATE matches
		SET ballchasing_url = 'https://ballchasing.com/replay/' || ballchasing_id
		WHERE ballchasing_id != '' AND (ballchasing_url LIKE '%/api/replays/%' OR ballchasing_url = '');
	`,
		`
		UPDATE matches
		SET upload_status = 'UPLOADED'
		WHERE ballchasing_id != '' AND (upload_status = '' OR upload_status = 'PENDING' OR upload_status = 'FAILED');
	`,
		`
		UPDATE matches
		SET stats_sync_status = 'SYNCED', stats_retry_count = 0
		WHERE ballchasing_id != ''
		  AND stats_sync_status = 'PENDING'
		  AND synced_at IS NOT NULL
		  AND synced_at > 0
		  AND replay_stats_json LIKE '%"ballchasing_id":%'
		  AND (replay_stats_json LIKE '%"players":null%' OR replay_stats_json LIKE '%"players":[]%' OR replay_stats_json LIKE '%"boost":%');
	`,
	}); err != nil {
		return err
	}

	// 2. Repair 0-0 match scores derived from player goals and backfill matchups.
	if err := s.repairZeroScoreMatches(ctx); err != nil {
		return err
	}

	// 3. Remove local accounts from players directory tables.
	localAccounts := s.getLocalAccountIDs(ctx)
	for accLower := range localAccounts {
		if err := s.execSQLQueries(ctx, []string{
			"UPDATE match_players SET is_local = 1 WHERE LOWER(player_id) = ?;",
			"DELETE FROM player_matchups WHERE LOWER(player_id) = ?;",
			"DELETE FROM players WHERE LOWER(player_id) = ?;",
		}, accLower); err != nil {
			return err
		}
	}

	// 4. One-time v4 migration: repair timestamps/players, deduplicate aliases, rebuild matchups, mark boost-less non-empty replays PENDING.
	var userVersion int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version;").Scan(&userVersion); err != nil {
		return err
	}
	if userVersion < 4 {
		if err := s.backfillMissingReplayPlayersAndTimestamps(ctx, localAccounts); err != nil {
			return err
		}
		if err := s.deduplicateConsoleAliases(ctx); err != nil {
			return err
		}
		if err := s.rebuildMatchupsFromMatchPlayers(ctx); err != nil {
			return err
		}
		if err := s.execSQLQueries(ctx, []string{
			`
			UPDATE matches
			SET stats_sync_status = 'PENDING', stats_retry_count = 0
			WHERE ballchasing_id != ''
			  AND stats_sync_status = 'SYNCED'
			  AND replay_stats_json LIKE '%"players":[{%'
			  AND replay_stats_json NOT LIKE '%"boost":%';
		`,
			"PRAGMA user_version = 4;",
		}); err != nil {
			return err
		}
	}

	// 5. Repair wall-clock player timestamps using match start timestamps.
	if _, err := s.db.ExecContext(ctx, `
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
	`); err != nil {
		return err
	}

	// 6. Repair corrupted legacy ranks_json values & backfill missing match_players.stats_json.
	if err := s.repairCorruptedRanksJSON(ctx, false); err != nil {
		return err
	}
	return s.backfillMatchPlayerStatsJSON(ctx)
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
		if scanErr := rows.Scan(&c.guid, &c.startTS, &c.playlist, &c.ballchasingID, &c.statsJSON); scanErr != nil {
			_ = rows.Close()
			return scanErr
		}
		cands = append(cands, c)
	}
	rowsErr := rows.Err()
	_ = rows.Close()
	if rowsErr != nil {
		return rowsErr
	}

	var toRepair []repairedMatchCandidate
	for _, c := range cands {
		var st ReplayMatchStats
		if json.Unmarshal([]byte(c.statsJSON), &st) != nil {
			continue
		}
		if len(st.Players) == 0 {
			loadedPlayers, loadErr := s.loadReplayPlayersFromMatchPlayers(ctx, c.guid)
			if loadErr != nil {
				return loadErr
			}
			st.Players = loadedPlayers
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
			return mErr
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
		if _, err := stmtMatch.ExecContext(ctx, r.blueScore, r.orangeScore, r.winnerTeam, r.statsJSON, r.guid); err != nil {
			return err
		}
		if r.winnerTeam >= 0 {
			if _, err := stmtMP.ExecContext(ctx, r.winnerTeam, r.guid); err != nil {
				return err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	return s.backfillOutcomesForRepairedMatches(ctx, toRepair)
}

func (s *SQLiteStore) loadReplayPlayersFromMatchPlayers(ctx context.Context, guid string) ([]ReplayPlayerStats, error) {
	mpRows, err := s.db.QueryContext(ctx, `
		SELECT mp.player_id, mp.team_num, mp.is_local, mp.score, mp.goals, mp.assists, mp.saves, mp.shots, mp.demos, mp.car_name, COALESCE(p.player_name, ''), COALESCE(p.platform, '')
		FROM match_players mp LEFT JOIN players p ON mp.player_id = p.player_id
		WHERE mp.match_guid = ? ORDER BY mp.team_num ASC, mp.score DESC;
	`, guid)
	if err != nil {
		return nil, err
	}
	defer mpRows.Close()

	var out []ReplayPlayerStats
	for mpRows.Next() {
		var (
			pid, carName, pName, plat        string
			team, isLoc, sc, g, a, sv, sh, d int
		)
		if scanErr := mpRows.Scan(&pid, &team, &isLoc, &sc, &g, &a, &sv, &sh, &d, &carName, &pName, &plat); scanErr != nil {
			return nil, scanErr
		}
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
	if rowsErr := mpRows.Err(); rowsErr != nil {
		return nil, rowsErr
	}
	return out, nil
}

func (s *SQLiteStore) backfillOutcomesForRepairedMatches(ctx context.Context, repaired []repairedMatchCandidate) error {
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
			return err
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
			if scanErr := mpRows.Scan(&info.playerID, &info.teamNum, &isLoc, &info.platform, &info.name); scanErr != nil {
				_ = mpRows.Close()
				return scanErr
			}
			info.isLocal = (isLoc == 1)
			if info.isLocal && localTeam == -1 {
				localTeam = info.teamNum
			}
			participants = append(participants, info)
		}
		rowsErr := mpRows.Err()
		_ = mpRows.Close()
		if rowsErr != nil {
			return rowsErr
		}
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
			if err := s.RecordMatchResults(ctx, r.guid, r.playlist, outcomes, time.Unix(r.startTS, 0).UTC()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *SQLiteStore) backfillMissingReplayPlayersAndTimestamps(ctx context.Context, localAccounts map[string]bool) error {
	mjRows, err := s.db.QueryContext(ctx, `
		SELECT m.match_guid, m.record_start_timestamp, COALESCE(m.winner_team, -1), m.replay_stats_json,
		       (SELECT COUNT(*) FROM match_players mp WHERE mp.match_guid = m.match_guid),
		       (SELECT COUNT(*) FROM match_players mp JOIN players p ON mp.player_id = p.player_id WHERE mp.match_guid = m.match_guid AND mp.is_local = 0)
		FROM matches m WHERE m.replay_stats_json != '';
	`)
	if err != nil {
		return err
	}
	type mJsonRow struct {
		guid, statsJSON                        string
		startTS                                int64
		winnerTeam, mpCount, nonLocalInPlayers int
	}
	var rows []mJsonRow
	for mjRows.Next() {
		var r mJsonRow
		if scanErr := mjRows.Scan(&r.guid, &r.startTS, &r.winnerTeam, &r.statsJSON, &r.mpCount, &r.nonLocalInPlayers); scanErr != nil {
			_ = mjRows.Close()
			return scanErr
		}
		rows = append(rows, r)
	}
	rowsErr := mjRows.Err()
	_ = mjRows.Close()
	if rowsErr != nil {
		return rowsErr
	}

	for _, r := range rows {
		var st ReplayMatchStats
		if json.Unmarshal([]byte(r.statsJSON), &st) != nil {
			continue
		}
		matchTS := r.startTS
		if st.Date != "" {
			if parsed, ok := ParseReplayDate(st.Date); ok && parsed.Unix() > 0 && (matchTS <= 0 || matchTS-parsed.Unix() > 86400) {
				matchTS = parsed.Unix()
				if _, err := s.db.ExecContext(ctx, "UPDATE matches SET record_start_timestamp = ? WHERE match_guid = ?;", matchTS, r.guid); err != nil {
					return err
				}
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
				if _, err := s.db.ExecContext(ctx, `
					INSERT INTO players (player_id, platform, player_name, player_name_lower, ranks_json, first_seen_at, last_seen_at)
					VALUES (?, ?, ?, ?, '{}', ?, ?)
					ON CONFLICT(player_id) DO UPDATE SET
						first_seen_at = MIN(players.first_seen_at, excluded.first_seen_at),
						last_seen_at = MAX(players.last_seen_at, excluded.last_seen_at);
				`, canonicalID, plat, strings.TrimSpace(p.Name), strings.ToLower(strings.TrimSpace(p.Name)), matchTS, matchTS); err != nil {
					return err
				}
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
			if _, err := s.db.ExecContext(ctx, `
				INSERT INTO match_players (match_guid, player_id, team_num, is_local, won, score, goals, assists, saves, shots, demos, car_name, stats_json)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(match_guid, player_id) DO UPDATE SET
					stats_json = CASE WHEN match_players.stats_json = '' THEN excluded.stats_json ELSE match_players.stats_json END;
			`, r.guid, canonicalID, p.Team, isLocInt, wonVal, p.Score, p.Goals, p.Assists, p.Saves, p.Shots, p.Demos, strings.TrimSpace(p.CarName), marshalTelemetryIfPresent(p)); err != nil {
				return err
			}
		}
	}
	return nil
}
