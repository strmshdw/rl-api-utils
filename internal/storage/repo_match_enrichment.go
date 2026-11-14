package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dank/rl-api-utils/internal/domain"
)

type enrichedMatchPlayer struct {
	playerID   string
	teamNum    int
	isLocal    bool
	score      int
	goals      int
	assists    int
	saves      int
	shots      int
	demos      int
	carName    string
	statsJSON  string
	playerName string
	platform   string
	ranksJSON  string
}

type localMMRInfo struct {
	mmr  float64
	tier int
	div  int
}

type enrichTarget struct {
	matchGUID string
	playlist  int
	mr        *MatchRecord
	stats     **ReplayMatchStats
}

// matchSearchMatchedMap executes at most one batched SQL query per 500 GUIDs to test participant search matches.
func (s *SQLiteStore) matchSearchMatchedMap(ctx context.Context, records []*MatchRecord, lowerQuery string) (map[string]bool, error) {
	matched := make(map[string]bool, len(records))
	if lowerQuery == "" || len(records) == 0 {
		for _, mr := range records {
			if mr != nil {
				matched[mr.MatchGUID] = true
			}
		}
		return matched, nil
	}

	var needPlayerLookup []string
	for _, mr := range records {
		if mr == nil {
			continue
		}
		if strings.Contains(strings.ToLower(mr.MatchGUID), lowerQuery) ||
			strings.Contains(strings.ToLower(mr.MapName), lowerQuery) ||
			strings.Contains(strings.ToLower(FormatPlaylistName(mr.Playlist)), lowerQuery) ||
			strings.Contains(strings.ToLower(mr.ReplayStatsJSON), lowerQuery) {
			matched[mr.MatchGUID] = true
			continue
		}
		needPlayerLookup = append(needPlayerLookup, mr.MatchGUID)
	}
	for _, chunk := range chunkStrings(needPlayerLookup, 500) {
		placeholders := make([]string, len(chunk))
		args := make([]interface{}, 0, len(chunk)+3)
		for i, g := range chunk {
			placeholders[i] = "?"
			args = append(args, g)
		}
		args = append(args, lowerQuery, lowerQuery, lowerQuery)
		query := fmt.Sprintf(`
			SELECT DISTINCT mp.match_guid
			FROM match_players mp LEFT JOIN players p ON mp.player_id = p.player_id
			WHERE mp.match_guid IN (%s)
			  AND (INSTR(LOWER(mp.player_id), ?) > 0 OR INSTR(COALESCE(p.player_name_lower, ''), ?) > 0 OR INSTR(LOWER(COALESCE(mp.car_name, '')), ?) > 0);
		`, strings.Join(placeholders, ","))
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, wrapContextErr(ctxErr)
			}
			continue
		}
		for rows.Next() {
			var g string
			if rows.Scan(&g) == nil {
				matched[g] = true
			}
		}
		_ = rows.Close()
	}
	return matched, nil
}

// EnrichMatchStatsWithPlayers enriches a single match's ReplayMatchStats with per-player telemetry and rank/MMR metadata.
func (s *SQLiteStore) EnrichMatchStatsWithPlayers(ctx context.Context, matchGUID string, playlist int, mr *MatchRecord, st *ReplayMatchStats) *ReplayMatchStats {
	s.enrichMatchStatsBatch(ctx, []enrichTarget{{matchGUID: matchGUID, playlist: playlist, mr: mr, stats: &st}})
	return st
}

// enrichMatchStatsBatch enriches a slice of matches in O(chunks) batched SQL queries (zero N+1).
func (s *SQLiteStore) enrichMatchStatsBatch(ctx context.Context, targets []enrichTarget) {
	if len(targets) == 0 {
		return
	}
	guidSet := make(map[string]struct{}, len(targets))
	guids := make([]string, 0, len(targets))
	for _, t := range targets {
		if t.matchGUID != "" {
			if _, ok := guidSet[t.matchGUID]; !ok {
				guidSet[t.matchGUID] = struct{}{}
				guids = append(guids, t.matchGUID)
			}
		}
	}
	mpsByGUID := make(map[string][]enrichedMatchPlayer, len(guids))
	mmrByGUID := make(map[string]localMMRInfo, len(guids))

	for _, chunk := range chunkStrings(guids, 500) {
		placeholders := make([]string, len(chunk))
		args := make([]interface{}, len(chunk))
		for i, g := range chunk {
			placeholders[i] = "?"
			args[i] = g
		}
		phStr := strings.Join(placeholders, ",")

		mpSQL := fmt.Sprintf(`
			SELECT mp.match_guid, mp.player_id, mp.team_num, mp.is_local, mp.score, mp.goals, mp.assists,
			       mp.saves, mp.shots, mp.demos, mp.car_name, mp.stats_json,
			       COALESCE(p.player_name, ''), COALESCE(p.platform, ''), COALESCE(p.ranks_json, '{}')
			FROM match_players mp LEFT JOIN players p ON mp.player_id = p.player_id
			WHERE mp.match_guid IN (%s)
			ORDER BY mp.match_guid ASC, mp.team_num ASC, mp.score DESC;`, phStr)
		if rows, err := s.db.QueryContext(ctx, mpSQL, args...); err == nil {
			for rows.Next() {
				var g string
				var item enrichedMatchPlayer
				var isLoc int
				if rows.Scan(&g, &item.playerID, &item.teamNum, &isLoc, &item.score, &item.goals, &item.assists, &item.saves, &item.shots, &item.demos, &item.carName, &item.statsJSON, &item.playerName, &item.platform, &item.ranksJSON) == nil {
					item.isLocal = (isLoc == 1)
					mpsByGUID[g] = append(mpsByGUID[g], item)
				}
			}
			_ = rows.Close()
		}

		mmrSQL := fmt.Sprintf(`SELECT match_guid, mmr, tier, division FROM mmr_history WHERE match_guid IN (%s) ORDER BY recorded_at DESC, id DESC;`, phStr)
		if rows, err := s.db.QueryContext(ctx, mmrSQL, args...); err == nil {
			for rows.Next() {
				var g string
				var info localMMRInfo
				if rows.Scan(&g, &info.mmr, &info.tier, &info.div) == nil {
					if _, exists := mmrByGUID[g]; !exists {
						mmrByGUID[g] = info
					}
				}
			}
			_ = rows.Close()
		}
	}

	for _, t := range targets {
		*t.stats = applyMatchEnrichment(t.matchGUID, t.playlist, t.mr, *t.stats, mpsByGUID[t.matchGUID], mmrByGUID[t.matchGUID])
	}
}

func applyMatchEnrichment(matchGUID string, playlist int, mr *MatchRecord, st *ReplayMatchStats, mps []enrichedMatchPlayer, localMMR localMMRInfo) *ReplayMatchStats {
	if st == nil && mr != nil && strings.TrimSpace(mr.ReplayStatsJSON) != "" {
		var parsed ReplayMatchStats
		if json.Unmarshal([]byte(mr.ReplayStatsJSON), &parsed) == nil {
			st = &parsed
		}
	}
	if st == nil && len(mps) > 0 {
		st = &ReplayMatchStats{MatchGUID: matchGUID, Playlist: playlist}
		if mr != nil {
			st.MapName, st.BlueScore, st.OrangeScore, st.WinnerTeam = mr.MapName, mr.BlueScore, mr.OrangeScore, mr.WinnerTeam
			st.BallchasingID, st.BallchasingURL = mr.BallchasingID, mr.BallchasingURL
			if mr.RecordStartTimestamp > 0 {
				st.Date = time.Unix(mr.RecordStartTimestamp, 0).UTC().Format(time.RFC3339)
			}
		}
	}
	if st == nil {
		return nil
	}

	matchedMP := make([]bool, len(mps))
	for i := range st.Players {
		sp := &st.Players[i]
		spOnlineLower := strings.ToLower(strings.TrimSpace(sp.OnlineID))
		spNameLower := strings.ToLower(strings.TrimSpace(sp.Name))
		var matched *enrichedMatchPlayer
		for j := range mps {
			if matchedMP[j] {
				continue
			}
			mpIDLower := strings.ToLower(strings.TrimSpace(mps[j].playerID))
			mpIDToken := extractOnlineToken(mpIDLower)
			if (spOnlineLower != "" && spOnlineLower != "0" && (mpIDLower == spOnlineLower || mpIDToken == spOnlineLower || strings.Contains(mpIDLower, "|"+spOnlineLower+"|") || strings.Contains(spOnlineLower, "|"+mpIDToken+"|"))) ||
				(sp.IsLocal && mps[j].isLocal && mps[j].teamNum == sp.Team) ||
				(spNameLower != "" && (strings.EqualFold(mps[j].playerName, spNameLower) || mpIDToken == spNameLower) && mps[j].teamNum == sp.Team) ||
				(mps[j].teamNum == sp.Team && mps[j].score == sp.Score && mps[j].goals == sp.Goals) {
				matched = &mps[j]
				matchedMP[j] = true
				break
			}
		}
		if matched != nil {
			if matched.isLocal {
				sp.IsLocal = true
			}
			mergePlayerStatsJSON(sp, matched.statsJSON)
			tier, div, mmr := ExtractPrimaryRankFromJSON(matched.ranksJSON, []int{playlist})
			if matched.isLocal && localMMR.mmr > 0 {
				mmr = localMMR.mmr
				if localMMR.tier > 0 && (tier != localMMR.tier || div == 0) {
					tier, div = localMMR.tier, localMMR.div
				}
			}
			applyRankToReplayPlayer(sp, tier, div, mmr)
		} else if sp.Rank != nil && sp.Rank.Tier > 0 {
			rName, sName := FormatRankName(sp.Rank.Tier, sp.Rank.Division), FormatShortRankName(sp.Rank.Tier, sp.Rank.Division)
			sp.Rank.Name, sp.Rank.ShortRank, sp.RankName, sp.ShortRank = rName, sName, rName, sName
		}
	}

	if len(st.Players) == 0 && len(mps) > 0 {
		for _, mp := range mps {
			var sp ReplayPlayerStats
			if strings.TrimSpace(mp.statsJSON) != "" && mp.statsJSON != "{}" {
				_ = json.Unmarshal([]byte(mp.statsJSON), &sp)
			}
			if sp.Name == "" {
				if mp.playerName != "" {
					sp.Name = mp.playerName
				} else {
					sp.Name = mp.playerID
				}
			}
			if sp.Platform == "" {
				sp.Platform = mp.platform
			}
			if sp.OnlineID == "" {
				sp.OnlineID = mp.playerID
			}
			sp.Team, sp.IsLocal, sp.Score, sp.Goals, sp.Assists, sp.Saves, sp.Shots, sp.Demos = mp.teamNum, mp.isLocal, mp.score, mp.goals, mp.assists, mp.saves, mp.shots, mp.demos
			if sp.CarName == "" {
				sp.CarName = mp.carName
			}
			tier, div, mmr := ExtractPrimaryRankFromJSON(mp.ranksJSON, []int{playlist})
			if mp.isLocal && localMMR.mmr > 0 {
				mmr = localMMR.mmr
				if localMMR.tier > 0 && (tier != localMMR.tier || div == 0) {
					tier, div = localMMR.tier, localMMR.div
				}
			}
			applyRankToReplayPlayer(&sp, tier, div, mmr)
			st.Players = append(st.Players, sp)
		}
	}
	return st
}

func mergePlayerStatsJSON(sp *ReplayPlayerStats, statsJSON string) {
	if strings.TrimSpace(statsJSON) == "" || statsJSON == "{}" {
		return
	}
	var parsed ReplayPlayerStats
	if json.Unmarshal([]byte(statsJSON), &parsed) != nil {
		return
	}
	if sp.Core == nil {
		sp.Core = parsed.Core
	}
	if sp.Boost == nil {
		sp.Boost = parsed.Boost
	}
	if sp.Movement == nil {
		sp.Movement = parsed.Movement
	}
	if sp.Positioning == nil {
		sp.Positioning = parsed.Positioning
	}
	if sp.Demo == nil {
		sp.Demo = parsed.Demo
	}
	if sp.Ball == nil {
		sp.Ball = parsed.Ball
	}
	if sp.Camera == nil {
		sp.Camera = parsed.Camera
	}
	if sp.Rank == nil {
		sp.Rank = parsed.Rank
	}
}

func applyRankToReplayPlayer(sp *ReplayPlayerStats, tier, div int, mmr float64) {
	if sp.Rank != nil && sp.Rank.Tier > 0 {
		tier, div = sp.Rank.Tier, sp.Rank.Division
	}
	if tier > 0 {
		rName, sName := FormatRankName(tier, div), FormatShortRankName(tier, div)
		if sp.Rank == nil {
			sp.Rank = &domain.PlayerRank{Tier: tier, Division: div, Name: rName, ShortRank: sName}
		} else {
			sp.Rank.Name, sp.Rank.ShortRank = rName, sName
		}
		sp.RankName, sp.ShortRank = rName, sName
	}
	if mmr > 0 && sp.MMR == 0 {
		sp.MMR = mmr
	}
}
