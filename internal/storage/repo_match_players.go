package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// SaveMatchPlayers persists per-player box scores for a match inside a single transaction.
func (s *SQLiteStore) SaveMatchPlayers(ctx context.Context, matchGUID string, players []*MatchPlayerRecord) error {
	if err := ctx.Err(); err != nil {
		return wrapContextErr(err)
	}
	if strings.TrimSpace(matchGUID) == "" {
		return ErrInvalidGUID
	}
	if len(players) == 0 {
		return nil
	}

	matchGUID = s.resolveExistingMatchGUID(ctx, matchGUID, "")
	resolvedPlayers := make([]*MatchPlayerRecord, 0, len(players))
	for _, p := range players {
		if p == nil || strings.TrimSpace(p.PlayerID) == "" {
			continue
		}
		cp := *p
		cp.MatchGUID = matchGUID
		plat := strings.TrimSpace(p.Platform)
		pName := strings.TrimSpace(p.PlayerName)
		if plat == "" {
			if parts := strings.SplitN(p.PlayerID, "|", 3); len(parts) == 3 {
				plat = strings.TrimSpace(parts[0])
			}
		}
		if pName == "" && strings.TrimSpace(p.StatsJSON) != "" {
			var rps ReplayPlayerStats
			if json.Unmarshal([]byte(p.StatsJSON), &rps) == nil {
				if strings.TrimSpace(rps.Name) != "" {
					pName = strings.TrimSpace(rps.Name)
				}
				if plat == "" && strings.TrimSpace(rps.Platform) != "" {
					plat = strings.TrimSpace(rps.Platform)
				}
			}
		}
		resolvedID := s.resolvePlayerIDForMatch(ctx, matchGUID, p.PlayerID, plat, pName, p.TeamNum, false)
		if p.IsLocal {
			var existingLocalID string
			if err := s.db.QueryRowContext(ctx, "SELECT player_id FROM match_players WHERE match_guid = ? AND team_num = ? AND is_local = 1 LIMIT 1;", matchGUID, p.TeamNum).Scan(&existingLocalID); err == nil && existingLocalID != "" {
				resolvedID = existingLocalID
			}
		}
		cp.PlayerID = resolvedID
		resolvedPlayers = append(resolvedPlayers, &cp)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin save match players tx: %w", err)
	}
	defer tx.Rollback()

	const query = `
	INSERT INTO match_players (
		match_guid, player_id, team_num, is_local, won,
		score, goals, assists, saves, shots, demos,
		car_name, stats_json
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(match_guid, player_id) DO UPDATE SET
		team_num = excluded.team_num,
		is_local = excluded.is_local,
		won = excluded.won,
		score = excluded.score,
		goals = excluded.goals,
		assists = excluded.assists,
		saves = excluded.saves,
		shots = excluded.shots,
		demos = excluded.demos,
		car_name = CASE WHEN excluded.car_name != '' THEN excluded.car_name ELSE match_players.car_name END,
		stats_json = CASE WHEN excluded.stats_json != '' THEN excluded.stats_json ELSE match_players.stats_json END;
	`
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to prepare save match player stmt: %w", err)
	}
	defer stmt.Close()

	for _, p := range resolvedPlayers {
		if p == nil || strings.TrimSpace(p.PlayerID) == "" {
			continue
		}
		var wonVal *int
		if p.Won != nil {
			w := 0
			if *p.Won {
				w = 1
			}
			wonVal = &w
		}
		isLocal := 0
		if p.IsLocal {
			isLocal = 1
		}
		if _, err := stmt.ExecContext(ctx,
			matchGUID, p.PlayerID, p.TeamNum, isLocal, wonVal,
			p.Score, p.Goals, p.Assists, p.Saves, p.Shots, p.Demos,
			p.CarName, p.StatsJSON,
		); err != nil {
			return fmt.Errorf("failed to save match player %s in %s: %w", p.PlayerID, matchGUID, err)
		}
	}
	return tx.Commit()
}

// GetMatchPlayers retrieves all player records for a match GUID ordered by team_num ASC, score DESC.
func (s *SQLiteStore) GetMatchPlayers(ctx context.Context, matchGUID string) ([]*MatchPlayerRecord, error) {
	if strings.TrimSpace(matchGUID) == "" {
		return nil, ErrInvalidGUID
	}

	const query = `
	SELECT match_guid, player_id, team_num, is_local, won,
	       score, goals, assists, saves, shots, demos,
	       car_name, stats_json
	FROM match_players
	WHERE match_guid = ?
	ORDER BY team_num ASC, score DESC;
	`
	rows, err := s.db.QueryContext(ctx, query, matchGUID)
	if err != nil {
		return nil, fmt.Errorf("failed to query match players for %s: %w", matchGUID, err)
	}
	defer rows.Close()

	records := make([]*MatchPlayerRecord, 0)
	for rows.Next() {
		var (
			mp      MatchPlayerRecord
			isLocal int
			wonVal  sql.NullInt64
		)
		if err := rows.Scan(
			&mp.MatchGUID, &mp.PlayerID, &mp.TeamNum, &isLocal, &wonVal,
			&mp.Score, &mp.Goals, &mp.Assists, &mp.Saves, &mp.Shots, &mp.Demos,
			&mp.CarName, &mp.StatsJSON,
		); err != nil {
			return nil, fmt.Errorf("failed to scan match player row: %w", err)
		}
		mp.IsLocal = isLocal == 1
		if wonVal.Valid {
			w := wonVal.Int64 == 1
			mp.Won = &w
		}
		records = append(records, &mp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error in get match players: %w", err)
	}
	return records, nil
}

// ListPlayerMatches returns paginated match details for a specific player using batched search and enrichment (zero N+1).
func (s *SQLiteStore) ListPlayerMatches(ctx context.Context, playerID string, filter PlayerMatchFilter) ([]*PlayerMatchDetail, int, error) {
	if strings.TrimSpace(playerID) == "" {
		return nil, 0, ErrPlayerNotFound
	}
	playerID = s.resolveCanonicalPlayerID(ctx, playerID)

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	isLocalTarget := 0
	if s.isLocalPlayerID(ctx, playerID) {
		isLocalTarget = 1
		if primaryID := s.primaryLocalPlayerID(ctx); primaryID != "" {
			playerID = primaryID
		}
	}

	const querySQL = `
	SELECT m.match_guid, m.record_start_timestamp, m.map_name, m.playlist, m.replay_url,
	       m.download_status, m.local_file_path, m.downloaded_at, m.upload_status,
	       m.ballchasing_id, m.ballchasing_url, m.uploaded_at, m.retry_count,
	       m.last_error, m.blue_score, m.orange_score, m.winner_team, m.replay_stats_json,
	       m.synced_at, m.stats_sync_status, m.stats_retry_count, m.created_at, m.updated_at,
	       mp.player_id, mp.team_num, mp.is_local, mp.won, mp.score, mp.goals, mp.assists,
	       mp.saves, mp.shots, mp.demos, mp.car_name, mp.stats_json,
	       COALESCE((SELECT lp.team_num FROM match_players lp WHERE lp.match_guid = m.match_guid AND lp.is_local = 1 LIMIT 1), -1) AS local_team
	FROM match_players mp
	JOIN matches m ON mp.match_guid = m.match_guid
	WHERE (mp.player_id = ? OR (? = 1 AND mp.is_local = 1))
	ORDER BY m.record_start_timestamp DESC, m.match_guid ASC;
	`
	rows, err := s.db.QueryContext(ctx, querySQL, playerID, isLocalTarget)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query player matches: %w", err)
	}
	defer rows.Close()

	playlistIDs := ParsePlaylistFilter(filter.PlaylistFilter)
	if len(playlistIDs) == 0 && filter.Playlist > 0 {
		playlistIDs = []int{filter.Playlist}
	}
	rawResultLower := strings.ToLower(strings.TrimSpace(filter.Result))
	useLocalPerspective := filter.LocalPerspective ||
		rawResultLower == "w" || rawResultLower == "l" ||
		rawResultLower == "win" || rawResultLower == "loss" ||
		rawResultLower == "wins" || rawResultLower == "losses"
	normResult := NormalizeResultFilter(filter.Result)
	normRole := NormalizeRoleFilter(filter.Role)
	lowerSearch := strings.ToLower(strings.TrimSpace(filter.Search))

	var allDetails []*PlayerMatchDetail
	for rows.Next() {
		var (
			mr           MatchRecord
			mp           MatchPlayerRecord
			downloadedAt sql.NullInt64
			uploadedAt   sql.NullInt64
			winnerTeam   sql.NullInt64
			syncedAt     sql.NullInt64
			blueScore    sql.NullInt64
			orangeScore  sql.NullInt64
			replayStats  sql.NullString
			statsSync    sql.NullString
			statsRetry   sql.NullInt64
			createdAt    int64
			updatedAt    int64
			isLocal      int
			wonVal       sql.NullInt64
			localTeam    int
		)
		if err := rows.Scan(
			&mr.MatchGUID, &mr.RecordStartTimestamp, &mr.MapName, &mr.Playlist, &mr.ReplayURL,
			&mr.DownloadStatus, &mr.LocalFilePath, &downloadedAt, &mr.UploadStatus,
			&mr.BallchasingID, &mr.BallchasingURL, &uploadedAt, &mr.RetryCount,
			&mr.LastError, &blueScore, &orangeScore, &winnerTeam, &replayStats,
			&syncedAt, &statsSync, &statsRetry, &createdAt, &updatedAt,
			&mp.PlayerID, &mp.TeamNum, &isLocal, &wonVal, &mp.Score, &mp.Goals, &mp.Assists,
			&mp.Saves, &mp.Shots, &mp.Demos, &mp.CarName, &mp.StatsJSON,
			&localTeam,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan player match detail: %w", err)
		}
		hydrateScannedMatchRecord(&mr, downloadedAt, uploadedAt, winnerTeam, syncedAt, blueScore, orangeScore, replayStats, statsSync, statsRetry, createdAt, updatedAt)
		mp.MatchGUID = mr.MatchGUID
		mp.IsLocal = isLocal == 1
		if wonVal.Valid {
			w := wonVal.Int64 == 1
			mp.Won = &w
		}

		role, effectiveResult, localResult := resolvePlayerMatchOutcome(&mr, &mp, localTeam, useLocalPerspective)
		if !MatchesPlaylistIDs(mr.Playlist, playlistIDs) {
			continue
		}
		if filter.FromUnix > 0 && mr.RecordStartTimestamp < filter.FromUnix {
			continue
		}
		if filter.ToUnix > 0 && mr.RecordStartTimestamp > filter.ToUnix {
			continue
		}
		if !MatchesStatusFilter(&mr, filter.Status) {
			continue
		}
		if (normRole == "teammate" && role != "teammate") || (normRole == "opponent" && role != "opponent") {
			continue
		}
		if normResult != "" && effectiveResult != normResult {
			continue
		}

		var st *ReplayMatchStats
		if mr.ReplayStatsJSON != "" {
			var parsed ReplayMatchStats
			if json.Unmarshal([]byte(mr.ReplayStatsJSON), &parsed) == nil {
				st = &parsed
			}
		}
		allDetails = append(allDetails, &PlayerMatchDetail{
			MatchRecord: mr,
			PlayerStats: mp,
			Role:        role,
			Result:      effectiveResult,
			LocalResult: localResult,
			Stats:       st,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("rows error in list player matches: %w", err)
	}

	matching := allDetails
	if lowerSearch != "" {
		records := make([]*MatchRecord, len(allDetails))
		for i, d := range allDetails {
			records[i] = &d.MatchRecord
		}
		matchedByGUID, searchErr := s.matchSearchMatchedMap(ctx, records, lowerSearch)
		if searchErr != nil {
			return nil, 0, searchErr
		}
		matching = make([]*PlayerMatchDetail, 0, len(allDetails))
		for _, d := range allDetails {
			if matchedByGUID[d.MatchGUID] {
				matching = append(matching, d)
			}
		}
	}

	total := len(matching)
	if total == 0 || offset >= total {
		return make([]*PlayerMatchDetail, 0), total, nil
	}

	SortPlayerMatchDetails(matching, filter.SortBy, filter.SortDir)
	end := offset + limit
	if end > total {
		end = total
	}
	page := make([]*PlayerMatchDetail, end-offset)
	copy(page, matching[offset:end])

	targets := make([]enrichTarget, len(page))
	for i, d := range page {
		targets[i] = enrichTarget{matchGUID: d.MatchGUID, playlist: d.Playlist, mr: &d.MatchRecord, stats: &d.Stats}
	}
	s.enrichMatchStatsBatch(ctx, targets)
	return page, total, nil
}

func resolvePlayerMatchOutcome(mr *MatchRecord, mp *MatchPlayerRecord, localTeam int, useLocalPerspective bool) (role, effectiveResult, localResult string) {
	role = "teammate"
	if localTeam >= 0 && mp.TeamNum != localTeam {
		role = "opponent"
	}

	playerResult := "unknown"
	switch {
	case mp.Won != nil && *mp.Won:
		playerResult = "victory"
	case mp.Won != nil:
		playerResult = "defeat"
	case mr.WinnerTeam != nil && *mr.WinnerTeam >= 0 && *mr.WinnerTeam == mp.TeamNum:
		playerResult = "victory"
	case mr.WinnerTeam != nil && *mr.WinnerTeam >= 0:
		playerResult = "defeat"
	case (mp.TeamNum == 0 && mr.BlueScore > mr.OrangeScore) || (mp.TeamNum == 1 && mr.OrangeScore > mr.BlueScore):
		playerResult = "victory"
	case (mp.TeamNum == 0 && mr.OrangeScore > mr.BlueScore) || (mp.TeamNum == 1 && mr.BlueScore > mr.OrangeScore):
		playerResult = "defeat"
	case (mr.WinnerTeam != nil && *mr.WinnerTeam == -1) || (mr.BlueScore == mr.OrangeScore && (mr.BlueScore > 0 || mr.OrangeScore > 0)):
		playerResult = "draw"
	}

	effLocalTeam := localTeam
	if effLocalTeam < 0 {
		if mp.IsLocal {
			effLocalTeam = mp.TeamNum
		} else {
			effLocalTeam = 0
		}
	}
	localResult = "unknown"
	switch {
	case mr.WinnerTeam != nil && *mr.WinnerTeam >= 0 && *mr.WinnerTeam == effLocalTeam:
		localResult = "victory"
	case mr.WinnerTeam != nil && *mr.WinnerTeam >= 0:
		localResult = "defeat"
	case mr.BlueScore != mr.OrangeScore && ((effLocalTeam == 0 && mr.BlueScore > mr.OrangeScore) || (effLocalTeam == 1 && mr.OrangeScore > mr.BlueScore)):
		localResult = "victory"
	case mr.BlueScore != mr.OrangeScore:
		localResult = "defeat"
	case mp.Won != nil && role == "teammate" && *mp.Won, mp.Won != nil && role != "teammate" && !*mp.Won:
		localResult = "victory"
	case mp.Won != nil:
		localResult = "defeat"
	case (mr.WinnerTeam != nil && *mr.WinnerTeam == -1) || (mr.BlueScore == mr.OrangeScore && (mr.BlueScore > 0 || mr.OrangeScore > 0)):
		localResult = "draw"
	}

	effectiveResult = playerResult
	if useLocalPerspective {
		effectiveResult = localResult
	}
	return role, effectiveResult, localResult
}
