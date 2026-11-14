package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SyncReplayStats enriches or upserts a match record with end-of-game replay statistics,
// keyed by MatchGUID, preserving existing Players when incoming Players is empty (017db99).
func (s *SQLiteStore) SyncReplayStats(ctx context.Context, stats *ReplayMatchStats) error {
	if err := ctx.Err(); err != nil {
		return wrapContextErr(err)
	}
	if stats == nil || strings.TrimSpace(stats.MatchGUID) == "" {
		return ErrInvalidGUID
	}

	st := *stats
	guid := s.resolveExistingMatchGUID(ctx, st.MatchGUID, st.BallchasingID)
	st.MatchGUID = guid
	stats.MatchGUID = guid
	if len(stats.Players) > 0 {
		playersCopy := make([]ReplayPlayerStats, len(stats.Players))
		copy(playersCopy, stats.Players)
		st.Players = playersCopy
	} else {
		var existingJSON string
		if s.db.QueryRowContext(ctx, "SELECT replay_stats_json FROM matches WHERE match_guid = ?;", guid).Scan(&existingJSON) == nil && existingJSON != "" {
			var existingSt ReplayMatchStats
			if json.Unmarshal([]byte(existingJSON), &existingSt) == nil && len(existingSt.Players) > 0 {
				st.Players = existingSt.Players
			}
		}
	}

	if st.BlueScore == 0 && st.OrangeScore == 0 && len(st.Players) > 0 {
		blueGoals, orangeGoals := 0, 0
		for _, p := range st.Players {
			if p.Team == 0 {
				blueGoals += p.Goals
			} else if p.Team == 1 {
				orangeGoals += p.Goals
			}
		}
		if blueGoals > 0 || orangeGoals > 0 {
			st.BlueScore, st.OrangeScore = blueGoals, orangeGoals
		}
	}
	if st.WinnerTeam == nil || *st.WinnerTeam < 0 {
		if st.BlueScore > st.OrangeScore {
			w := 0
			st.WinnerTeam = &w
		} else if st.OrangeScore > st.BlueScore {
			w := 1
			st.WinnerTeam = &w
		}
	}
	st.BallchasingURL = NormalizeBallchasingURL(st.BallchasingID, st.BallchasingURL)

	nowUnix := time.Now().UTC().Unix()
	winnerVal := int64(-1)
	if st.WinnerTeam != nil {
		winnerVal = int64(*st.WinnerTeam)
	}
	startTS := nowUnix
	if st.Date != "" {
		if t, ok := ParseReplayDate(st.Date); ok {
			startTS = t.Unix()
		}
	}
	if startTS == nowUnix {
		var existingTS int64
		if s.db.QueryRowContext(ctx, "SELECT record_start_timestamp FROM matches WHERE match_guid = ?;", guid).Scan(&existingTS) == nil && existingTS > 0 {
			startTS = existingTS
		}
	}

	matchPlayers := s.prepareSyncMatchPlayers(ctx, guid, &st, time.Unix(startTS, 0).UTC())
	statsJSON, err := json.Marshal(&st)
	if err != nil {
		return fmt.Errorf("failed to marshal replay stats: %w", err)
	}

	dlStatus := DownloadDownloaded
	upStatus := UploadPending
	statsSyncStatus := StatsSyncPending
	if st.BallchasingID != "" {
		upStatus = UploadUploaded
		statsSyncStatus = StatsSyncSynced
	}

	const query = `
	INSERT INTO matches (
		match_guid, record_start_timestamp, map_name, playlist,
		download_status, upload_status, ballchasing_id, ballchasing_url,
		blue_score, orange_score, winner_team, replay_stats_json, synced_at,
		stats_sync_status, stats_retry_count, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)
	ON CONFLICT(match_guid) DO UPDATE SET
		record_start_timestamp = CASE WHEN matches.record_start_timestamp > 0 THEN matches.record_start_timestamp ELSE excluded.record_start_timestamp END,
		map_name = CASE WHEN excluded.map_name != '' THEN excluded.map_name ELSE matches.map_name END,
		playlist = CASE WHEN excluded.playlist != 0 THEN excluded.playlist ELSE matches.playlist END,
		ballchasing_id = CASE WHEN excluded.ballchasing_id != '' THEN excluded.ballchasing_id ELSE matches.ballchasing_id END,
		ballchasing_url = CASE WHEN excluded.ballchasing_url != '' THEN excluded.ballchasing_url ELSE matches.ballchasing_url END,
		blue_score = CASE WHEN excluded.blue_score != 0 OR excluded.orange_score != 0 THEN excluded.blue_score ELSE matches.blue_score END,
		orange_score = CASE WHEN excluded.blue_score != 0 OR excluded.orange_score != 0 THEN excluded.orange_score ELSE matches.orange_score END,
		winner_team = CASE WHEN excluded.winner_team != -1 THEN excluded.winner_team ELSE matches.winner_team END,
		replay_stats_json = excluded.replay_stats_json,
		synced_at = excluded.synced_at,
		download_status = CASE WHEN matches.download_status = 'PENDING' THEN 'DOWNLOADED' ELSE matches.download_status END,
		upload_status = CASE WHEN excluded.ballchasing_id != '' AND (matches.upload_status = '' OR matches.upload_status = 'PENDING' OR matches.upload_status = 'FAILED') THEN 'UPLOADED' ELSE matches.upload_status END,
		stats_sync_status = CASE WHEN excluded.ballchasing_id != '' THEN 'SYNCED' ELSE matches.stats_sync_status END,
		updated_at = excluded.updated_at;`

	if _, err := s.db.ExecContext(ctx, query,
		guid, startTS, st.MapName, st.Playlist,
		dlStatus, upStatus, st.BallchasingID, st.BallchasingURL,
		st.BlueScore, st.OrangeScore, winnerVal, string(statsJSON), nowUnix,
		string(statsSyncStatus), nowUnix, nowUnix,
	); err != nil {
		return fmt.Errorf("failed to sync replay stats for %s: %w", guid, err)
	}

	if len(matchPlayers) > 0 {
		_ = s.SaveMatchPlayers(ctx, guid, matchPlayers)
	}
	s.hydrateRanksFromStats(ctx, &st)
	return nil
}

func (s *SQLiteStore) prepareSyncMatchPlayers(ctx context.Context, guid string, st *ReplayMatchStats, matchTime time.Time) []*MatchPlayerRecord {
	localAccounts := s.getLocalAccountIDs(ctx)
	matchPlayers := make([]*MatchPlayerRecord, 0, len(st.Players))
	for i := range st.Players {
		p := &st.Players[i]
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
		canonicalID := s.resolvePlayerIDForMatch(ctx, guid, pid, plat, p.Name, p.Team, true)
		isLocal := p.IsLocal || localAccounts[strings.ToLower(pid)] || localAccounts[strings.ToLower(canonicalID)]
		if isLocal {
			var existingLocalID string
			if s.db.QueryRowContext(ctx, "SELECT player_id FROM match_players WHERE match_guid = ? AND team_num = ? AND is_local = 1 LIMIT 1;", guid, p.Team).Scan(&existingLocalID) == nil && existingLocalID != "" {
				canonicalID = existingLocalID
			}
		}
		p.IsLocal = isLocal

		var won *bool
		if st.WinnerTeam != nil && *st.WinnerTeam >= 0 {
			w := (*st.WinnerTeam == p.Team)
			won = &w
		}
		matchPlayers = append(matchPlayers, &MatchPlayerRecord{
			MatchGUID: guid, PlayerID: canonicalID, Platform: plat, PlayerName: strings.TrimSpace(p.Name),
			TeamNum: p.Team, IsLocal: isLocal, Won: won, Score: p.Score, Goals: p.Goals,
			Assists: p.Assists, Saves: p.Saves, Shots: p.Shots, Demos: p.Demos,
			CarName: strings.TrimSpace(p.CarName), StatsJSON: marshalTelemetryIfPresent(*p),
		})

		if !isLocal {
			pRec, err := s.GetPlayer(ctx, canonicalID)
			if err != nil {
				_ = s.UpsertPlayer(ctx, &PlayerRecord{
					PlayerID: canonicalID, Platform: plat, PlayerName: strings.TrimSpace(p.Name),
					FirstSeenAt: matchTime, LastSeenAt: matchTime,
				})
			} else {
				if pRec.Platform == "" || pRec.Platform == "Unknown" {
					pRec.Platform = plat
				}
				if strings.TrimSpace(p.Name) != "" {
					pRec.PlayerName = strings.TrimSpace(p.Name)
				}
				if matchTime.Before(pRec.FirstSeenAt) {
					pRec.FirstSeenAt = matchTime
				}
				if matchTime.After(pRec.LastSeenAt) {
					pRec.LastSeenAt = matchTime
				}
				_ = s.UpsertPlayer(ctx, pRec)
			}
		}
	}
	return matchPlayers
}

func (s *SQLiteStore) hydrateRanksFromStats(ctx context.Context, stats *ReplayMatchStats) {
	if stats == nil || stats.Playlist <= 0 {
		return
	}
	localAccounts := s.getLocalAccountIDs(ctx)
	applyRank := func(idStr string, tier, division int, rankID, rankName string) {
		if idStr == "" || (tier == 0 && (rankID == "" || rankID == "0")) || localAccounts[strings.ToLower(idStr)] {
			return
		}
		pRec, err := s.GetPlayer(ctx, idStr)
		if err != nil || localAccounts[strings.ToLower(pRec.PlayerID)] {
			return
		}
		ranks := make(map[string]map[string]interface{})
		if pRec.RanksJSON != "" && pRec.RanksJSON != "{}" {
			_ = json.Unmarshal([]byte(pRec.RanksJSON), &ranks)
		}
		rName := FormatRankName(tier, division)
		if tier == 0 && rankName != "" {
			rName = rankName
		}
		pKey := strconv.Itoa(stats.Playlist)
		mmr, matchesPlayed := 0.0, 0
		if prev, ok := ranks[pKey]; ok {
			if mVal, ok := prev["mmr"].(float64); ok && mVal > 0 {
				mmr = mVal
			}
			if mpVal, ok := prev["matches_played"].(float64); ok && mpVal > 0 {
				matchesPlayed = int(mpVal)
			}
		}
		ranks[pKey] = map[string]interface{}{
			"playlist_id": stats.Playlist, "playlist_name": FormatPlaylistName(stats.Playlist),
			"tier": tier, "division": division, "rank_name": rName, "short_rank": FormatShortRankName(tier, division),
			"mmr": mmr, "matches_played": matchesPlayed,
		}
		if updatedData, err := json.Marshal(ranks); err == nil {
			_, _ = s.db.ExecContext(ctx, "UPDATE players SET ranks_json = ? WHERE player_id = ?;", string(updatedData), pRec.PlayerID)
		}
	}

	for _, p := range stats.Players {
		if p.Rank == nil {
			continue
		}
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
		canonicalID := s.resolvePlayerIDForMatch(ctx, stats.MatchGUID, pid, plat, p.Name, p.Team, true)
		applyRank(canonicalID, p.Rank.Tier, p.Rank.Division, p.Rank.ID, p.Rank.Name)
	}

	if stats.RawJSON == "" {
		return
	}
	var detail struct {
		Blue, Orange struct {
			Players []struct {
				ID   interface{} `json:"id"`
				Rank *struct {
					ID       interface{} `json:"id"`
					Tier     int         `json:"tier"`
					Division int         `json:"division"`
					Name     string      `json:"name"`
				} `json:"rank"`
			} `json:"players"`
		}
	}
	if json.Unmarshal([]byte(stats.RawJSON), &detail) != nil {
		return
	}
	for _, bp := range append(detail.Blue.Players, detail.Orange.Players...) {
		if bp.Rank == nil {
			continue
		}
		var idStr, rankIDStr string
		switch v := bp.ID.(type) {
		case string:
			idStr = v
		case map[string]interface{}:
			if idVal, ok := v["id"].(string); ok {
				idStr = idVal
			}
		}
		if bp.Rank.ID != nil {
			rankIDStr = fmt.Sprintf("%v", bp.Rank.ID)
		}
		applyRank(idStr, bp.Rank.Tier, bp.Rank.Division, rankIDStr, bp.Rank.Name)
	}
}

// ListHistoricalMatches returns paginated historical matches matching the filter using batched enrichment (zero N+1).
func (s *SQLiteStore) ListHistoricalMatches(ctx context.Context, filter MatchFilter) ([]*HistoricalMatchRecord, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, wrapContextErr(err)
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	} else if limit > 200 {
		limit = 200
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	const outcomeExpr = `
	CASE
		WHEN (SELECT lp.team_num FROM match_players lp WHERE lp.match_guid = matches.match_guid AND lp.is_local = 1 LIMIT 1) = 1 THEN
			CASE WHEN orange_score > blue_score THEN 'victory' WHEN blue_score > orange_score THEN 'defeat'
			     WHEN winner_team != -1 THEN CASE WHEN winner_team = 1 THEN 'victory' ELSE 'defeat' END ELSE 'draw' END
		ELSE
			CASE WHEN blue_score > orange_score THEN 'victory' WHEN orange_score > blue_score THEN 'defeat'
			     WHEN winner_team != -1 THEN CASE WHEN winner_team = 0 THEN 'victory' ELSE 'defeat' END ELSE 'draw' END
	END`

	var whereClauses []string
	var args []interface{}
	playlistIDs := ParsePlaylistFilter(filter.PlaylistFilter)
	if len(playlistIDs) == 0 && filter.Playlist > 0 {
		playlistIDs = []int{filter.Playlist}
	}
	if len(playlistIDs) > 0 {
		placeholders := make([]string, len(playlistIDs))
		for i, pid := range playlistIDs {
			placeholders[i] = "?"
			args = append(args, pid)
		}
		whereClauses = append(whereClauses, fmt.Sprintf("playlist IN (%s)", strings.Join(placeholders, ",")))
	}
	if filter.FromUnix > 0 {
		whereClauses = append(whereClauses, "record_start_timestamp >= ?")
		args = append(args, filter.FromUnix)
	}
	if filter.ToUnix > 0 {
		whereClauses = append(whereClauses, "record_start_timestamp <= ?")
		args = append(args, filter.ToUnix)
	}
	if cleanResult := NormalizeResultFilter(filter.Result); cleanResult != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("(%s) = ?", outcomeExpr))
		args = append(args, cleanResult)
	}
	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	searchTerm := strings.TrimSpace(filter.Search)
	if searchTerm == "" {
		searchTerm = strings.TrimSpace(filter.Query)
	}
	lowerSearch := strings.ToLower(searchTerm)
	cleanStatus := strings.ToLower(strings.TrimSpace(filter.Status))
	cleanSortBy := strings.ToLower(strings.TrimSpace(filter.SortBy))
	cleanSortDir := strings.ToLower(strings.TrimSpace(filter.SortDir))
	needsInMem := lowerSearch != "" || (cleanStatus != "" && cleanStatus != "all") ||
		(cleanSortBy != "" && cleanSortBy != "date" && cleanSortBy != "time" && cleanSortBy != "timestamp") || cleanSortDir == "asc"

	if !needsInMem {
		var total int
		if err := s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM matches %s;", whereSQL), args...).Scan(&total); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, 0, wrapContextErr(ctxErr)
			}
			return nil, 0, fmt.Errorf("failed to count historical matches: %w", err)
		}
		if total == 0 || offset >= total {
			return make([]*HistoricalMatchRecord, 0), total, nil
		}
		querySQL := fmt.Sprintf(`SELECT %s, (%s) AS outcome FROM matches %s ORDER BY record_start_timestamp DESC, match_guid ASC LIMIT ? OFFSET ?;`, matchSelectCols, outcomeExpr, whereSQL)
		rows, err := s.db.QueryContext(ctx, querySQL, append(args, limit, offset)...)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, 0, wrapContextErr(ctxErr)
			}
			return nil, 0, fmt.Errorf("failed to query historical matches: %w", err)
		}
		defer rows.Close()
		records, err := scanHistoricalMatchRows(ctx, rows)
		if err != nil {
			return nil, 0, err
		}
		enrichTargets := make([]enrichTarget, len(records))
		for i, rec := range records {
			enrichTargets[i] = enrichTarget{matchGUID: rec.MatchGUID, playlist: rec.Playlist, mr: &rec.MatchRecord, stats: &rec.Stats}
		}
		s.enrichMatchStatsBatch(ctx, enrichTargets)
		return records, total, nil
	}

	querySQL := fmt.Sprintf(`SELECT %s, (%s) AS outcome FROM matches %s ORDER BY record_start_timestamp DESC, match_guid ASC;`, matchSelectCols, outcomeExpr, whereSQL)
	rows, err := s.db.QueryContext(ctx, querySQL, args...)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, 0, wrapContextErr(ctxErr)
		}
		return nil, 0, fmt.Errorf("failed to query historical matches: %w", err)
	}
	defer rows.Close()
	allRecords, err := scanHistoricalMatchRows(ctx, rows)
	if err != nil {
		return nil, 0, err
	}
	_ = rows.Close()

	statusFiltered := make([]*HistoricalMatchRecord, 0, len(allRecords))
	searchMatchInputs := make([]*MatchRecord, 0, len(allRecords))
	for _, rec := range allRecords {
		if MatchesStatusFilter(&rec.MatchRecord, cleanStatus) {
			statusFiltered = append(statusFiltered, rec)
			searchMatchInputs = append(searchMatchInputs, &rec.MatchRecord)
		}
	}
	searchMatched, err := s.matchSearchMatchedMap(ctx, searchMatchInputs, lowerSearch)
	if err != nil {
		return nil, 0, err
	}
	matching := make([]*HistoricalMatchRecord, 0, len(statusFiltered))
	for _, rec := range statusFiltered {
		if lowerSearch == "" || searchMatched[rec.MatchGUID] {
			matching = append(matching, rec)
		}
	}

	total := len(matching)
	if total == 0 || offset >= total {
		return make([]*HistoricalMatchRecord, 0), total, nil
	}
	var localStatsByGUID map[string]*MatchPlayerRecord
	if cleanSortBy != "" && cleanSortBy != "date" && cleanSortBy != "time" && cleanSortBy != "timestamp" && cleanSortBy != "duration" {
		localStatsByGUID = make(map[string]*MatchPlayerRecord)
		if lpRows, lpErr := s.db.QueryContext(ctx, "SELECT match_guid, score, goals, assists, saves, shots, demos FROM match_players WHERE is_local = 1;"); lpErr == nil {
			for lpRows.Next() {
				var mp MatchPlayerRecord
				if lpRows.Scan(&mp.MatchGUID, &mp.Score, &mp.Goals, &mp.Assists, &mp.Saves, &mp.Shots, &mp.Demos) == nil {
					localStatsByGUID[mp.MatchGUID] = &mp
				}
			}
			_ = lpRows.Close()
		}
	}
	SortHistoricalMatches(matching, cleanSortBy, cleanSortDir, localStatsByGUID)

	end := offset + limit
	if end > total {
		end = total
	}
	page := make([]*HistoricalMatchRecord, end-offset)
	copy(page, matching[offset:end])
	enrichTargets := make([]enrichTarget, len(page))
	for i, rec := range page {
		enrichTargets[i] = enrichTarget{matchGUID: rec.MatchGUID, playlist: rec.Playlist, mr: &rec.MatchRecord, stats: &rec.Stats}
	}
	s.enrichMatchStatsBatch(ctx, enrichTargets)
	return page, total, nil
}

func scanHistoricalMatchRows(ctx context.Context, rows *sql.Rows) ([]*HistoricalMatchRecord, error) {
	records := make([]*HistoricalMatchRecord, 0)
	for rows.Next() {
		var (
			mr                       MatchRecord
			downloadedAt, uploadedAt sql.NullInt64
			winnerTeam, syncedAt     sql.NullInt64
			blueScore, orangeScore   sql.NullInt64
			replayStats, statsSync   sql.NullString
			statsRetry               sql.NullInt64
			createdAt, updatedAt     int64
			outcome                  string
		)
		if err := rows.Scan(
			&mr.MatchGUID, &mr.RecordStartTimestamp, &mr.MapName, &mr.Playlist, &mr.ReplayURL,
			&mr.DownloadStatus, &mr.LocalFilePath, &downloadedAt, &mr.UploadStatus,
			&mr.BallchasingID, &mr.BallchasingURL, &uploadedAt, &mr.RetryCount,
			&mr.LastError, &blueScore, &orangeScore, &winnerTeam, &replayStats,
			&syncedAt, &statsSync, &statsRetry, &createdAt, &updatedAt, &outcome,
		); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, wrapContextErr(ctxErr)
			}
			return nil, fmt.Errorf("failed to scan match: %w", err)
		}
		hydrateScannedMatchRecord(&mr, downloadedAt, uploadedAt, winnerTeam, syncedAt, blueScore, orangeScore, replayStats, statsSync, statsRetry, createdAt, updatedAt)
		hist := &HistoricalMatchRecord{MatchRecord: mr, Result: outcome}
		if mr.ReplayStatsJSON != "" {
			var st ReplayMatchStats
			if json.Unmarshal([]byte(mr.ReplayStatsJSON), &st) == nil {
				hist.Stats = &st
			}
		}
		records = append(records, hist)
	}
	if err := rows.Err(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, wrapContextErr(ctxErr)
		}
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return records, nil
}
