package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// resolveCanonicalPlayerID resolves a raw OnlineID, alternate-platform composite ID,
// display name, or local alias ("me"/"you") to its canonical PlayerID.
func (s *SQLiteStore) resolveCanonicalPlayerID(ctx context.Context, playerID string) string {
	trimmed := strings.TrimSpace(playerID)
	if trimmed == "" {
		return playerID
	}
	var exact string
	if err := s.db.QueryRowContext(ctx, "SELECT player_id FROM players WHERE player_id = ? LIMIT 1;", trimmed).Scan(&exact); err == nil && exact != "" {
		return exact
	}

	lookupToken := trimmed
	if strings.Contains(trimmed, "|") {
		parts := strings.Split(trimmed, "|")
		if len(parts) >= 2 && strings.TrimSpace(parts[1]) != "" && strings.TrimSpace(parts[1]) != "0" {
			lookupToken = strings.TrimSpace(parts[1])
		} else {
			return playerID
		}
	}

	likePattern := "%|" + escapeLike(strings.ToLower(lookupToken)) + "|%"
	var resolved string
	if err := s.db.QueryRowContext(ctx, `
		SELECT p.player_id
		FROM players p
		LEFT JOIN player_matchups m ON p.player_id = m.player_id
		WHERE LOWER(p.player_id) LIKE ? ESCAPE '\'
		GROUP BY p.player_id
		ORDER BY COALESCE(SUM(m.total_matches), 0) DESC, p.last_seen_at DESC, p.player_id ASC
		LIMIT 1;
	`, likePattern).Scan(&resolved); err == nil && resolved != "" {
		return resolved
	}

	if !strings.Contains(trimmed, "|") {
		if err := s.db.QueryRowContext(ctx, `
			SELECT p.player_id
			FROM players p
			LEFT JOIN player_matchups m ON p.player_id = m.player_id
			WHERE p.player_name_lower = ?
			GROUP BY p.player_id
			ORDER BY COALESCE(SUM(m.total_matches), 0) DESC, p.last_seen_at DESC, p.player_id ASC
			LIMIT 1;
		`, strings.ToLower(trimmed)).Scan(&resolved); err == nil && resolved != "" {
			return resolved
		}
	}

	if strings.EqualFold(trimmed, "me") || strings.EqualFold(trimmed, "you") {
		if pid := s.primaryLocalPlayerID(ctx); pid != "" {
			return pid
		}
	}

	if err := s.db.QueryRowContext(ctx, `
		SELECT player_id
		FROM match_players
		WHERE LOWER(player_id) = ? OR LOWER(player_id) LIKE ? ESCAPE '\'
		GROUP BY player_id
		ORDER BY MAX(is_local) DESC, COUNT(*) DESC, player_id ASC
		LIMIT 1;
	`, strings.ToLower(trimmed), likePattern).Scan(&resolved); err == nil && resolved != "" {
		return resolved
	}

	if !strings.Contains(trimmed, "|") {
		var authAcc string
		if err := s.db.QueryRowContext(ctx, `
			SELECT account_id
			FROM auth_state
			WHERE LOWER(display_name) = ? AND account_id != '' AND LOWER(provider) NOT LIKE '%_polling'
			LIMIT 1;
		`, strings.ToLower(trimmed)).Scan(&authAcc); err == nil && authAcc != "" {
			if pid := s.primaryLocalPlayerID(ctx); pid != "" {
				return pid
			}
			return authAcc
		}
	}
	return playerID
}

// primaryLocalPlayerID returns the canonical primary account PlayerID for the local player,
// prioritizing match_players (is_local = 1) and falling back only to non-polling auth_state.
func (s *SQLiteStore) primaryLocalPlayerID(ctx context.Context) string {
	var pid string
	if err := s.db.QueryRowContext(ctx, `
		SELECT player_id
		FROM match_players
		WHERE is_local = 1 AND player_id != '' AND player_id != '0'
		GROUP BY player_id
		ORDER BY COUNT(*) DESC, player_id ASC
		LIMIT 1;
	`).Scan(&pid); err == nil && strings.TrimSpace(pid) != "" {
		return strings.TrimSpace(pid)
	}
	if err := s.db.QueryRowContext(ctx, `
		SELECT account_id
		FROM auth_state
		WHERE account_id != '' AND LOWER(provider) NOT LIKE '%_polling'
		ORDER BY updated_at DESC
		LIMIT 1;
	`).Scan(&pid); err == nil && strings.TrimSpace(pid) != "" {
		return strings.TrimSpace(pid)
	}
	return ""
}

// resolvePlayerIDForMatch maps an incoming player identifier onto an existing canonical PlayerID
// in match_players or players so PsyNet, .replay, and Ballchasing formats converge cleanly.
func (s *SQLiteStore) resolvePlayerIDForMatch(ctx context.Context, matchGUID, rawOnlineID, platform, playerName string, teamNum int, defaultComposite bool) string {
	pid := strings.TrimSpace(rawOnlineID)
	name := strings.TrimSpace(playerName)
	plat := strings.TrimSpace(platform)
	if plat == "" {
		plat = "Unknown"
	}
	if pid == "" || pid == "0" || strings.HasSuffix(pid, "|0|0") {
		if name == "" {
			return pid
		}
		pid = name
	}

	if matchGUID != "" {
		var rawMP string
		if err := s.db.QueryRowContext(ctx, "SELECT player_id FROM match_players WHERE match_guid = ? AND player_id = ? LIMIT 1;", matchGUID, pid).Scan(&rawMP); err == nil && rawMP != "" {
			return rawMP
		}
	}
	var rawP string
	if err := s.db.QueryRowContext(ctx, "SELECT player_id FROM players WHERE player_id = ? LIMIT 1;", pid).Scan(&rawP); err == nil && rawP != "" {
		return rawP
	}

	candidateID := pid
	onlineToken := pid
	if strings.Contains(pid, "|") {
		parts := strings.Split(pid, "|")
		if len(parts) >= 2 {
			if strings.TrimSpace(parts[0]) != "" && (plat == "" || plat == "Unknown") {
				plat = strings.TrimSpace(parts[0])
			}
			onlineToken = strings.TrimSpace(parts[1])
		}
	} else if defaultComposite {
		candidateID = fmt.Sprintf("%s|%s|0", plat, pid)
	}

	if candidateID != pid {
		if matchGUID != "" {
			var exactMP string
			if err := s.db.QueryRowContext(ctx, "SELECT player_id FROM match_players WHERE match_guid = ? AND player_id = ? LIMIT 1;", matchGUID, candidateID).Scan(&exactMP); err == nil && exactMP != "" {
				return exactMP
			}
		}
		var exactP string
		if err := s.db.QueryRowContext(ctx, "SELECT player_id FROM players WHERE player_id = ? LIMIT 1;", candidateID).Scan(&exactP); err == nil && exactP != "" {
			return exactP
		}
	}

	if onlineToken != "" && onlineToken != "0" {
		likePattern := "%|" + escapeLike(strings.ToLower(onlineToken)) + "|%"
		if matchGUID != "" {
			var mpMatch string
			if teamNum >= 0 {
				_ = s.db.QueryRowContext(ctx, `
					SELECT player_id FROM match_players
					WHERE match_guid = ? AND team_num = ? AND LOWER(player_id) LIKE ? ESCAPE '\'
					LIMIT 1;
				`, matchGUID, teamNum, likePattern).Scan(&mpMatch)
			} else {
				_ = s.db.QueryRowContext(ctx, `
					SELECT player_id FROM match_players
					WHERE match_guid = ? AND LOWER(player_id) LIKE ? ESCAPE '\'
					LIMIT 1;
				`, matchGUID, likePattern).Scan(&mpMatch)
			}
			if mpMatch != "" {
				return mpMatch
			}
		}
		var existingID, existingPlat string
		if err := s.db.QueryRowContext(ctx, `
			SELECT p.player_id, p.platform
			FROM players p
			LEFT JOIN player_matchups m ON p.player_id = m.player_id
			WHERE LOWER(p.player_id) LIKE ? ESCAPE '\'
			GROUP BY p.player_id
			ORDER BY COALESCE(SUM(m.total_matches), 0) DESC, p.last_seen_at DESC, p.player_id ASC
			LIMIT 1;
		`, likePattern).Scan(&existingID, &existingPlat); err == nil && existingID != "" && SamePlatformFamily(existingPlat, plat) {
			return existingID
		}
	}

	lookupName := name
	if lookupName == "" && onlineToken != "" {
		lookupName = onlineToken
	}
	if lookupName != "" && matchGUID != "" {
		var mpID, mpPlat string
		if teamNum >= 0 {
			_ = s.db.QueryRowContext(ctx, `
				SELECT mp.player_id, COALESCE(p.platform, '')
				FROM match_players mp
				JOIN players p ON mp.player_id = p.player_id
				WHERE mp.match_guid = ? AND mp.team_num = ? AND p.player_name_lower = ?
				LIMIT 1;
			`, matchGUID, teamNum, strings.ToLower(lookupName)).Scan(&mpID, &mpPlat)
		} else {
			_ = s.db.QueryRowContext(ctx, `
				SELECT mp.player_id, COALESCE(p.platform, '')
				FROM match_players mp
				JOIN players p ON mp.player_id = p.player_id
				WHERE mp.match_guid = ? AND p.player_name_lower = ?
				LIMIT 1;
			`, matchGUID, strings.ToLower(lookupName)).Scan(&mpID, &mpPlat)
		}
		if mpID != "" && SamePlatformFamily(mpPlat, plat) {
			return mpID
		}
	}

	if name != "" && strings.EqualFold(onlineToken, name) {
		var pID, pPlat string
		if err := s.db.QueryRowContext(ctx, `
			SELECT p.player_id, p.platform
			FROM players p
			LEFT JOIN player_matchups m ON p.player_id = m.player_id
			WHERE p.player_name_lower = ?
			GROUP BY p.player_id
			ORDER BY COALESCE(SUM(m.total_matches), 0) DESC, p.last_seen_at DESC, p.player_id ASC
			LIMIT 1;
		`, strings.ToLower(name)).Scan(&pID, &pPlat); err == nil && pID != "" && SamePlatformFamily(pPlat, plat) {
			return pID
		}
	}
	return candidateID
}

func (s *SQLiteStore) getLocalAccountIDs(ctx context.Context) map[string]bool {
	accounts := make(map[string]bool)
	addAccount := func(acc string) {
		acc = strings.TrimSpace(acc)
		if acc == "" {
			return
		}
		accounts[strings.ToLower(acc)] = true
		if strings.Contains(acc, "|") {
			parts := strings.Split(acc, "|")
			if len(parts) >= 2 && strings.TrimSpace(parts[1]) != "" && strings.TrimSpace(parts[1]) != "0" {
				accounts[strings.ToLower(strings.TrimSpace(parts[1]))] = true
			}
		}
	}

	if rows, err := s.db.QueryContext(ctx, "SELECT account_id FROM auth_state WHERE account_id != '' AND LOWER(provider) NOT LIKE '%_polling';"); err == nil {
		for rows.Next() {
			var acc string
			if err := rows.Scan(&acc); err == nil {
				addAccount(acc)
			}
		}
		rows.Close()
	}

	if mpRows, err := s.db.QueryContext(ctx, "SELECT DISTINCT player_id FROM match_players WHERE is_local = 1;"); err == nil {
		for mpRows.Next() {
			var pid string
			if err := mpRows.Scan(&pid); err == nil {
				addAccount(pid)
			}
		}
		mpRows.Close()
	}
	return accounts
}

func (s *SQLiteStore) isLocalPlayerID(ctx context.Context, playerID string) bool {
	trimmed := strings.TrimSpace(playerID)
	if trimmed == "" {
		return false
	}
	if strings.EqualFold(trimmed, "me") || strings.EqualFold(trimmed, "you") {
		return true
	}
	localAccounts := s.getLocalAccountIDs(ctx)
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
	var isLocCount int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM match_players WHERE is_local = 1 AND LOWER(player_id) = ? LIMIT 1;`, lower).Scan(&isLocCount)
	return isLocCount > 0
}

// GetLocalPlayerIDs returns all known local player account IDs ordered by local match frequency DESC
// while excluding background `*_polling` auth providers.
func (s *SQLiteStore) GetLocalPlayerIDs(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContextErr(err)
	}

	seen := make(map[string]bool)
	var ids []string
	addID := func(id string) {
		id = strings.TrimSpace(id)
		if id != "" && id != "0" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}

	if mpRows, err := s.db.QueryContext(ctx, `
		SELECT player_id
		FROM match_players
		WHERE is_local = 1 AND player_id != '' AND player_id != '0'
		GROUP BY player_id
		ORDER BY COUNT(*) DESC, player_id ASC;
	`); err == nil {
		defer mpRows.Close()
		for mpRows.Next() {
			var pid string
			if err := mpRows.Scan(&pid); err == nil {
				addID(pid)
			}
		}
	}

	if authRows, err := s.db.QueryContext(ctx, `
		SELECT account_id
		FROM auth_state
		WHERE account_id != '' AND LOWER(provider) NOT LIKE '%_polling'
		ORDER BY updated_at DESC;
	`); err == nil {
		defer authRows.Close()
		for authRows.Next() {
			var acc string
			if err := authRows.Scan(&acc); err == nil {
				addID(acc)
			}
		}
	}
	return ids, nil
}

type localPlaylistAgg struct {
	wins, losses, total int
	lastPlayed          int64
	tier, division      int
	rankName            string
}

// GetLocalPlayerProfile synthesizes a PlayerRecord and per-playlist PlayerMatchup slice
// for the local player from match_players, matches, mmr_history, and auth_state.
func (s *SQLiteStore) GetLocalPlayerProfile(ctx context.Context, playerID string) (*PlayerRecord, []*PlayerMatchup, error) {
	trimmed := strings.TrimSpace(playerID)
	if trimmed == "" {
		return nil, nil, ErrPlayerNotFound
	}
	resolvedID := s.resolveCanonicalPlayerID(ctx, trimmed)
	if !s.isLocalPlayerID(ctx, trimmed) && !s.isLocalPlayerID(ctx, resolvedID) {
		return nil, nil, ErrPlayerNotFound
	}

	canonicalID := s.primaryLocalPlayerID(ctx)
	if canonicalID == "" {
		if !strings.EqualFold(resolvedID, "me") && !strings.EqualFold(resolvedID, "you") && resolvedID != "" {
			canonicalID = resolvedID
		} else {
			return nil, nil, ErrPlayerNotFound
		}
	}

	displayName, platform := s.resolveLocalIdentityDetails(ctx, canonicalID)

	var firstSeenUnix, lastSeenUnix int64
	_ = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(MIN(m.record_start_timestamp), 0), COALESCE(MAX(m.record_start_timestamp), 0)
		FROM match_players mp
		JOIN matches m ON mp.match_guid = m.match_guid
		WHERE (mp.is_local = 1 OR mp.player_id = ?) AND m.record_start_timestamp > 0;
	`, canonicalID).Scan(&firstSeenUnix, &lastSeenUnix)
	now := time.Now().UTC()
	firstSeen, lastSeen := now, now
	if firstSeenUnix > 0 {
		firstSeen = time.Unix(firstSeenUnix, 0).UTC()
	}
	if lastSeenUnix > 0 {
		lastSeen = time.Unix(lastSeenUnix, 0).UTC()
	}

	byPlaylist := s.aggregateLocalPlaylists(ctx, canonicalID)
	matchups := make([]*PlayerMatchup, 0, len(byPlaylist))
	for pid, agg := range byPlaylist {
		if agg.total <= 0 {
			continue
		}
		lp := lastSeen
		if agg.lastPlayed > 0 {
			lp = time.Unix(agg.lastPlayed, 0).UTC()
		}
		matchups = append(matchups, &PlayerMatchup{
			PlayerID: canonicalID, PlaylistID: pid,
			WinsAsTeammate: agg.wins, LossesAsTeammate: agg.losses,
			TotalMatches: agg.total, LastPlayedAt: lp,
		})
	}
	sort.Slice(matchups, func(i, j int) bool {
		if matchups[i].TotalMatches != matchups[j].TotalMatches {
			return matchups[i].TotalMatches > matchups[j].TotalMatches
		}
		return matchups[i].PlaylistID < matchups[j].PlaylistID
	})

	rec := &PlayerRecord{
		PlayerID: canonicalID, Platform: platform, PlayerName: displayName,
		RanksJSON: s.buildLocalRanksJSON(ctx, byPlaylist), Notes: "", Tags: []string{},
		FirstSeenAt: firstSeen, LastSeenAt: lastSeen,
	}
	return rec, matchups, nil
}

func (s *SQLiteStore) resolveLocalIdentityDetails(ctx context.Context, canonicalID string) (string, string) {
	platform := "Unknown"
	if strings.Contains(canonicalID, "|") {
		if parts := strings.Split(canonicalID, "|"); strings.TrimSpace(parts[0]) != "" {
			platform = strings.TrimSpace(parts[0])
		}
	}

	var displayName string
	if nameRows, err := s.db.QueryContext(ctx, `
		SELECT mp.stats_json, m.replay_stats_json
		FROM match_players mp
		JOIN matches m ON mp.match_guid = m.match_guid
		WHERE mp.player_id = ? OR mp.is_local = 1
		ORDER BY (CASE WHEN mp.player_id = ? THEN 1 ELSE 0 END) DESC, m.record_start_timestamp DESC
		LIMIT 25;
	`, canonicalID, canonicalID); err == nil {
		for nameRows.Next() {
			var stJSON, repJSON string
			if err := nameRows.Scan(&stJSON, &repJSON); err != nil {
				continue
			}
			if stJSON != "" {
				var rps ReplayPlayerStats
				if json.Unmarshal([]byte(stJSON), &rps) == nil {
					if cand := strings.TrimSpace(rps.Name); cand != "" && !strings.EqualFold(cand, "You") {
						displayName = cand
						if platform == "Unknown" && strings.TrimSpace(rps.Platform) != "" {
							platform = strings.TrimSpace(rps.Platform)
						}
						break
					}
				}
			}
			if repJSON != "" {
				var rms ReplayMatchStats
				if json.Unmarshal([]byte(repJSON), &rms) == nil {
					for _, p := range rms.Players {
						if p.IsLocal || strings.EqualFold(p.OnlineID, canonicalID) || (strings.Contains(canonicalID, "|") && strings.EqualFold(p.OnlineID, strings.Split(canonicalID, "|")[1])) {
							if cand := strings.TrimSpace(p.Name); cand != "" && !strings.EqualFold(cand, "You") {
								displayName = cand
								if platform == "Unknown" && strings.TrimSpace(p.Platform) != "" {
									platform = strings.TrimSpace(p.Platform)
								}
								break
							}
						}
					}
					if displayName != "" {
						break
					}
				}
			}
		}
		nameRows.Close()
	}

	if displayName == "" || platform == "Unknown" {
		var authName, provider string
		_ = s.db.QueryRowContext(ctx, `
			SELECT COALESCE(display_name, ''), COALESCE(provider, '')
			FROM auth_state
			WHERE (display_name != '' OR account_id != '') AND LOWER(provider) NOT LIKE '%_polling'
			ORDER BY updated_at DESC
			LIMIT 1;
		`).Scan(&authName, &provider)
		authName = strings.TrimSpace(authName)
		if displayName == "" && authName != "" {
			displayName = authName
		}
		if platform == "Unknown" {
			switch {
			case strings.EqualFold(provider, "epic"):
				platform = "Epic"
			case strings.EqualFold(provider, "steam"):
				platform = "Steam"
			}
		}
	}
	if displayName == "" {
		displayName = "You"
	}
	return displayName, platform
}

func (s *SQLiteStore) aggregateLocalPlaylists(ctx context.Context, canonicalID string) map[int]*localPlaylistAgg {
	byPlaylist := make(map[int]*localPlaylistAgg)
	mRows, err := s.db.QueryContext(ctx, `
		SELECT m.playlist, m.record_start_timestamp, m.blue_score, m.orange_score, m.winner_team,
		       mp.team_num, mp.won, mp.stats_json
		FROM match_players mp
		JOIN matches m ON mp.match_guid = m.match_guid
		WHERE mp.is_local = 1 OR mp.player_id = ?
		ORDER BY m.record_start_timestamp DESC;
	`, canonicalID)
	if err != nil {
		return byPlaylist
	}
	defer mRows.Close()

	for mRows.Next() {
		var (
			playlist, teamNum               int
			startTS                         int64
			blueScore, orangeSc, winnerTeam sql.NullInt64
			wonVal                          sql.NullInt64
			statsJSON                       string
		)
		if mRows.Scan(&playlist, &startTS, &blueScore, &orangeSc, &winnerTeam, &teamNum, &wonVal, &statsJSON) != nil || playlist <= 0 {
			continue
		}
		agg, ok := byPlaylist[playlist]
		if !ok {
			agg = &localPlaylistAgg{}
			byPlaylist[playlist] = agg
		}
		if startTS > agg.lastPlayed {
			agg.lastPlayed = startTS
		}
		var won, lost bool
		switch {
		case wonVal.Valid:
			won = wonVal.Int64 == 1
			lost = !won
		case winnerTeam.Valid && winnerTeam.Int64 >= 0:
			won = int(winnerTeam.Int64) == teamNum
			lost = !won
		case blueScore.Int64 != orangeSc.Int64:
			won = (teamNum == 0 && blueScore.Int64 > orangeSc.Int64) || (teamNum == 1 && orangeSc.Int64 > blueScore.Int64)
			lost = !won
		}
		if won {
			agg.wins++
			agg.total++
		} else if lost {
			agg.losses++
			agg.total++
		}
		if agg.tier == 0 && statsJSON != "" {
			var rps ReplayPlayerStats
			if json.Unmarshal([]byte(statsJSON), &rps) == nil && rps.Rank != nil && rps.Rank.Tier > 0 {
				agg.tier = rps.Rank.Tier
				agg.division = rps.Rank.Division
				agg.rankName = rps.Rank.Name
			}
		}
	}
	return byPlaylist
}

func (s *SQLiteStore) buildLocalRanksJSON(ctx context.Context, byPlaylist map[int]*localPlaylistAgg) string {
	ranksMap := make(map[string]map[string]interface{})
	if mmrRows, err := s.db.QueryContext(ctx, `
		SELECT playlist_id, mmr, tier, division
		FROM mmr_history
		WHERE id IN (SELECT MAX(id) FROM mmr_history GROUP BY playlist_id);
	`); err == nil {
		for mmrRows.Next() {
			var pid, mmr, tier, div int
			if mmrRows.Scan(&pid, &mmr, &tier, &div) == nil && pid > 0 {
				effDiv := Resolve1BasedDivision(div, "")
				games := 0
				if agg, ok := byPlaylist[pid]; ok {
					games = agg.total
					if tier == 0 && agg.tier > 0 {
						tier = agg.tier
						effDiv = Resolve1BasedDivision(agg.division, agg.rankName)
					}
				}
				ranksMap[strconv.Itoa(pid)] = map[string]interface{}{
					"playlist_id": pid, "playlist_name": FormatPlaylistName(pid),
					"mmr": mmr, "tier": tier, "division": effDiv,
					"rank_name": FormatRankName(tier, effDiv), "short_rank": FormatShortRankName(tier, effDiv),
					"matches_played": games,
				}
			}
		}
		mmrRows.Close()
	}
	for pid, agg := range byPlaylist {
		pKey := strconv.Itoa(pid)
		if _, exists := ranksMap[pKey]; !exists && agg.tier > 0 {
			effDiv := Resolve1BasedDivision(agg.division, agg.rankName)
			ranksMap[pKey] = map[string]interface{}{
				"playlist_id": pid, "playlist_name": FormatPlaylistName(pid),
				"tier": agg.tier, "division": effDiv,
				"rank_name": FormatRankName(agg.tier, effDiv), "short_rank": FormatShortRankName(agg.tier, effDiv),
				"matches_played": agg.total,
			}
		}
	}
	if len(ranksMap) == 0 {
		return "{}"
	}
	b, err := json.Marshal(ranksMap)
	if err != nil {
		return "{}"
	}
	return string(b)
}
