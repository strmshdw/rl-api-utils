package storage

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/dank/rl-api-utils/internal/domain"
)

// ExtractPrimaryRankFromJSON extracts the best (tier, division, mmr) from a player's ranks_json.
func ExtractPrimaryRankFromJSON(ranksJSON string, playlistIDs []int) (tier int, div int, mmr float64) {
	if ranksJSON == "" || ranksJSON == "{}" {
		return 0, 0, 0
	}
	var parsed map[string]struct {
		PlaylistID int     `json:"playlist_id"`
		Tier       int     `json:"tier"`
		Division   int     `json:"division"`
		MMR        float64 `json:"mmr"`
		RankName   string  `json:"rank_name"`
	}
	if err := json.Unmarshal([]byte(ranksJSON), &parsed); err != nil || len(parsed) == 0 {
		return 0, 0, 0
	}
	for _, pid := range playlistIDs {
		if r, ok := parsed[strconv.Itoa(pid)]; ok && (r.MMR > 0 || r.Tier > 0) {
			return r.Tier, Resolve1BasedDivision(r.Division, r.RankName), r.MMR
		}
	}
	for _, pid := range []int{11, 13, 10, 12} {
		if r, ok := parsed[strconv.Itoa(pid)]; ok && (r.MMR > 0 || r.Tier > 0) {
			return r.Tier, Resolve1BasedDivision(r.Division, r.RankName), r.MMR
		}
	}
	for _, r := range parsed {
		rDiv := Resolve1BasedDivision(r.Division, r.RankName)
		if r.MMR > mmr || (r.MMR == mmr && r.Tier > tier) {
			tier, div, mmr = r.Tier, rDiv, r.MMR
		}
	}
	return tier, div, mmr
}

// ExtractPlayerStatsJSONFromReplay returns mpStatsJSON if non-empty, or extracts the matching player's ReplayPlayerStats JSON.
func ExtractPlayerStatsJSONFromReplay(mpStatsJSON, replayStatsJSON, playerID, playerName string, teamNum int) string {
	if s := strings.TrimSpace(mpStatsJSON); s != "" && s != "{}" {
		return s
	}
	if s := strings.TrimSpace(replayStatsJSON); s != "" && s != "{}" {
		var st ReplayMatchStats
		if err := json.Unmarshal([]byte(s), &st); err == nil {
			return ExtractPlayerStatsFromParsedReplay(&st, playerID, playerName, teamNum)
		}
	}
	return ""
}

func extractOnlineToken(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if parsed, err := domain.ParsePrimaryID(trimmed); err == nil {
		if parsed.AccountID != "" && parsed.AccountID != "0" {
			return parsed.AccountID
		}
		return ""
	}
	if strings.Contains(trimmed, "|") {
		parts := strings.Split(trimmed, "|")
		if len(parts) >= 2 {
			tok := strings.TrimSpace(parts[1])
			if tok != "" && tok != "0" {
				return tok
			}
		}
	}
	return ""
}

// ExtractPlayerStatsFromParsedReplay extracts a single player's ReplayPlayerStats JSON from parsed match stats.
func ExtractPlayerStatsFromParsedReplay(st *ReplayMatchStats, playerID, playerName string, teamNum int) string {
	if st == nil {
		return ""
	}
	lowerID := strings.ToLower(strings.TrimSpace(playerID))
	lowerName := strings.ToLower(strings.TrimSpace(playerName))
	idToken := extractOnlineToken(lowerID)
	for _, p := range st.Players {
		pID := strings.ToLower(strings.TrimSpace(p.OnlineID))
		pName := strings.ToLower(strings.TrimSpace(p.Name))
		if (pID != "" && pID != "0" && (pID == lowerID || pID == idToken || strings.Contains(lowerID, "|"+pID+"|") || strings.Contains(pID, "|"+idToken+"|"))) ||
			(lowerName != "" && pName == lowerName && (teamNum < 0 || p.Team == teamNum)) ||
			(idToken != "" && pName == idToken && (teamNum < 0 || p.Team == teamNum)) {
			if b, err := json.Marshal(p); err == nil {
				return string(b)
			}
		}
	}
	if teamNum >= 0 {
		var teamCandidate *ReplayPlayerStats
		teamCount := 0
		for i := range st.Players {
			if st.Players[i].Team == teamNum {
				teamCandidate = &st.Players[i]
				teamCount++
			}
		}
		if teamCount == 1 && teamCandidate != nil {
			if b, err := json.Marshal(teamCandidate); err == nil {
				return string(b)
			}
		}
	}
	return ""
}

// PopulatePlayerSummaryComputedFields sets EncountersCount, W-L role counts, and RelationshipTag on a PlayerSummary.
func PopulatePlayerSummaryComputedFields(sRec *PlayerSummary) {
	if sRec == nil {
		return
	}
	sRec.TeammateWins = sRec.TotalWinsAsTeammate
	sRec.TeammateLosses = sRec.TotalLossesAsTeammate
	sRec.OpponentWins = sRec.TotalWinsAsOpponent
	sRec.OpponentLosses = sRec.TotalLossesAsOpponent
	sRec.EncountersCount = sRec.TotalMatches
	sRec.RelationshipTag = ComputeRelationshipTag(
		sRec.TotalWinsAsTeammate,
		sRec.TotalLossesAsTeammate,
		sRec.TotalWinsAsOpponent,
		sRec.TotalLossesAsOpponent,
	)
}

// MatchesRoleFilter evaluates whether a PlayerSummary satisfies the role filter in PlayerDirectoryFilter.
func MatchesRoleFilter(sRec *PlayerSummary, filter PlayerDirectoryFilter) bool {
	if sRec == nil {
		return false
	}
	switch NormalizeRoleFilter(filter.Role) {
	case "frequent":
		minM := filter.MinMatches
		if minM <= 0 {
			minM = 3
		}
		return sRec.TotalMatches >= minM
	case "teammate":
		return (sRec.TotalWinsAsTeammate + sRec.TotalLossesAsTeammate) > 0
	case "opponent":
		return (sRec.TotalWinsAsOpponent + sRec.TotalLossesAsOpponent) > 0
	case "synergy":
		minGames := filter.MinSynergyGames
		if minGames <= 0 {
			minGames = 2
		}
		minWR := filter.MinSynergyWR
		if minWR <= 0 {
			minWR = 60.0
		}
		tmGames := sRec.TotalWinsAsTeammate + sRec.TotalLossesAsTeammate
		if tmGames < minGames {
			return false
		}
		wrPct := (float64(sRec.TotalWinsAsTeammate) / float64(tmGames)) * 100.0
		return wrPct+1e-9 >= minWR
	default:
		return true
	}
}

// MatchesPlaylistIDs returns true if playlistIDs is empty or contains playlist.
func MatchesPlaylistIDs(playlist int, playlistIDs []int) bool {
	if len(playlistIDs) == 0 {
		return true
	}
	for _, id := range playlistIDs {
		if id == playlist {
			return true
		}
	}
	return false
}

// MatchesStatusFilter evaluates whether a MatchRecord matches the given status filter.
func MatchesStatusFilter(m *MatchRecord, status string) bool {
	if m == nil {
		return false
	}
	s := strings.ToLower(strings.TrimSpace(status))
	if s == "" || s == "all" {
		return true
	}
	isUploaded := m.UploadStatus == UploadUploaded || m.UploadStatus == UploadDuplicate || m.StatsSyncStatus == StatsSyncSynced || strings.TrimSpace(m.BallchasingID) != ""
	isFailed := m.DownloadStatus == DownloadFailed || m.UploadStatus == UploadFailed || m.StatsSyncStatus == StatsSyncFailed
	isDownloaded := m.DownloadStatus == DownloadDownloaded || strings.TrimSpace(m.LocalFilePath) != ""
	switch s {
	case "uploaded", "synced", "ballchasing":
		return isUploaded
	case "local":
		return isDownloaded && !isUploaded
	case "downloaded":
		return isDownloaded
	case "failed", "error":
		return isFailed
	case "pending", "syncing", "queued":
		return !isUploaded && !isFailed
	default:
		return true
	}
}

// SortAndPaginatePlayerSummaries sorts matching PlayerSummary records and applies Limit/Offset pagination.
func SortAndPaginatePlayerSummaries(matching []*PlayerSummary, filter PlayerDirectoryFilter, playlistIDs []int) ([]*PlayerSummary, int) {
	total := len(matching)
	if total == 0 || filter.Limit == 0 {
		return make([]*PlayerSummary, 0), total
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return make([]*PlayerSummary, 0), total
	}

	cleanSort := strings.ToLower(strings.TrimSpace(filter.Sort))
	isAsc := strings.ToLower(strings.TrimSpace(filter.Order)) == "asc"

	cmpFloat := func(x, y float64) (bool, bool) {
		if x == y {
			return false, false
		}
		if isAsc {
			return x < y, true
		}
		return x > y, true
	}
	cmpInt := func(x, y int) (bool, bool) {
		if x == y {
			return false, false
		}
		if isAsc {
			return x < y, true
		}
		return x > y, true
	}
	winRatio := func(wins, losses int) float64 {
		tot := wins + losses
		if tot <= 0 {
			return 0
		}
		return float64(wins) / float64(tot)
	}

	sort.Slice(matching, func(i, j int) bool {
		a, b := matching[i], matching[j]
		switch cleanSort {
		case "games", "match_count", "matches", "encounters":
			if lt, ok := cmpInt(a.TotalMatches, b.TotalMatches); ok {
				return lt
			}
		case "with_wr", "win_rate":
			if lt, ok := cmpFloat(winRatio(a.TotalWinsAsTeammate, a.TotalLossesAsTeammate), winRatio(b.TotalWinsAsTeammate, b.TotalLossesAsTeammate)); ok {
				return lt
			}
			if lt, ok := cmpInt(a.TotalMatches, b.TotalMatches); ok {
				return lt
			}
		case "against_wr", "opponent_wr", "vs_win_rate":
			if lt, ok := cmpFloat(winRatio(a.TotalWinsAsOpponent, a.TotalLossesAsOpponent), winRatio(b.TotalWinsAsOpponent, b.TotalLossesAsOpponent)); ok {
				return lt
			}
			if lt, ok := cmpInt(a.TotalMatches, b.TotalMatches); ok {
				return lt
			}
		case "with_pts":
			ptsA, ptsB := 0.0, 0.0
			if a.WithYouStats != nil {
				ptsA = a.WithYouStats.AvgScore
			}
			if b.WithYouStats != nil {
				ptsB = b.WithYouStats.AvgScore
			}
			if lt, ok := cmpFloat(ptsA, ptsB); ok {
				return lt
			}
		case "against_pts", "vs_pts":
			ptsA, ptsB := 0.0, 0.0
			if a.AgainstYouStats != nil {
				ptsA = a.AgainstYouStats.AvgScore
			}
			if b.AgainstYouStats != nil {
				ptsB = b.AgainstYouStats.AvgScore
			}
			if lt, ok := cmpFloat(ptsA, ptsB); ok {
				return lt
			}
		case "mmr":
			_, _, mmrA := ExtractPrimaryRankFromJSON(a.RanksJSON, playlistIDs)
			_, _, mmrB := ExtractPrimaryRankFromJSON(b.RanksJSON, playlistIDs)
			if lt, ok := cmpFloat(mmrA, mmrB); ok {
				return lt
			}
		case "rank":
			tierA, divA, mmrA := ExtractPrimaryRankFromJSON(a.RanksJSON, playlistIDs)
			tierB, divB, mmrB := ExtractPrimaryRankFromJSON(b.RanksJSON, playlistIDs)
			if lt, ok := cmpInt(tierA*10+divA, tierB*10+divB); ok {
				return lt
			}
			if lt, ok := cmpFloat(mmrA, mmrB); ok {
				return lt
			}
		case "platform":
			pA, pB := strings.ToLower(a.Platform), strings.ToLower(b.Platform)
			if pA != pB {
				if isAsc {
					return pA < pB
				}
				return pA > pB
			}
		case "name":
			nA, nB := strings.ToLower(a.PlayerName), strings.ToLower(b.PlayerName)
			if nA != nB {
				if isAsc {
					return nA < nB
				}
				return nA > nB
			}
		}
		if !a.LastSeenAt.Equal(b.LastSeenAt) {
			if isAsc {
				return a.LastSeenAt.Before(b.LastSeenAt)
			}
			return a.LastSeenAt.After(b.LastSeenAt)
		}
		return a.PlayerID < b.PlayerID
	})

	end := total
	if filter.Limit > 0 {
		end = offset + filter.Limit
		if end > total {
			end = total
		}
	}
	results := make([]*PlayerSummary, end-offset)
	copy(results, matching[offset:end])
	return results, total
}

func sortMatchItems[T any](items []T, sortBy, sortDir string, metricFn func(T, string) float64, tsFn func(T) int64, guidFn func(T) string) {
	s := strings.ToLower(strings.TrimSpace(sortBy))
	isAsc := strings.ToLower(strings.TrimSpace(sortDir)) == "asc"
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		tsA, tsB := tsFn(a), tsFn(b)
		if s == "" || s == "date" || s == "time" || s == "timestamp" {
			if tsA != tsB {
				if isAsc {
					return tsA < tsB
				}
				return tsA > tsB
			}
			return guidFn(a) < guidFn(b)
		}
		mA, mB := metricFn(a, s), metricFn(b, s)
		if mA != mB {
			if isAsc {
				return mA < mB
			}
			return mA > mB
		}
		if tsA != tsB {
			return tsA > tsB
		}
		return guidFn(a) < guidFn(b)
	})
}

func playerStatMetric(score, goals, assists, saves, shots, demos int, st *ReplayMatchStats, ts int64, sortKey string) float64 {
	switch sortKey {
	case "score", "pts", "points":
		return float64(score)
	case "goals":
		return float64(goals)
	case "assists":
		return float64(assists)
	case "saves":
		return float64(saves)
	case "shots":
		return float64(shots)
	case "demos", "demos_inflicted":
		return float64(demos)
	case "duration":
		if st != nil {
			return float64(st.Duration)
		}
		return 0
	default:
		return float64(ts)
	}
}

// SortHistoricalMatches sorts HistoricalMatchRecord slice by sortBy and sortDir.
func SortHistoricalMatches(records []*HistoricalMatchRecord, sortBy, sortDir string, localStatsByGUID map[string]*MatchPlayerRecord) {
	sortMatchItems(records, sortBy, sortDir,
		func(r *HistoricalMatchRecord, key string) float64 {
			if r == nil {
				return 0
			}
			if lp := localStatsByGUID[r.MatchGUID]; lp != nil {
				return playerStatMetric(lp.Score, lp.Goals, lp.Assists, lp.Saves, lp.Shots, lp.Demos, r.Stats, r.RecordStartTimestamp, key)
			}
			if r.Stats != nil && len(r.Stats.Players) > 0 {
				p := r.Stats.Players[0]
				return playerStatMetric(p.Score, p.Goals, p.Assists, p.Saves, p.Shots, p.Demos, r.Stats, r.RecordStartTimestamp, key)
			}
			return playerStatMetric(0, 0, 0, 0, 0, 0, r.Stats, r.RecordStartTimestamp, key)
		},
		func(r *HistoricalMatchRecord) int64 { return r.RecordStartTimestamp },
		func(r *HistoricalMatchRecord) string { return r.MatchGUID },
	)
}

// SortPlayerMatchDetails sorts PlayerMatchDetail slice by sortBy and sortDir.
func SortPlayerMatchDetails(details []*PlayerMatchDetail, sortBy, sortDir string) {
	sortMatchItems(details, sortBy, sortDir,
		func(d *PlayerMatchDetail, key string) float64 {
			if d == nil {
				return 0
			}
			return playerStatMetric(d.PlayerStats.Score, d.PlayerStats.Goals, d.PlayerStats.Assists, d.PlayerStats.Saves, d.PlayerStats.Shots, d.PlayerStats.Demos, d.Stats, d.RecordStartTimestamp, key)
		},
		func(d *PlayerMatchDetail) int64 { return d.RecordStartTimestamp },
		func(d *PlayerMatchDetail) string { return d.MatchGUID },
	)
}
