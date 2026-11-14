package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dank/rl-api-utils/internal/storage"
)

const historicalMatchBatchPageSize = 200

func cloneReplayMatchStats(stats *storage.ReplayMatchStats) *storage.ReplayMatchStats {
	if stats == nil {
		return nil
	}
	cp := *stats
	if stats.WinnerTeam != nil {
		wt := *stats.WinnerTeam
		cp.WinnerTeam = &wt
	}
	if stats.Players != nil {
		cp.Players = make([]storage.ReplayPlayerStats, len(stats.Players))
		copy(cp.Players, stats.Players)
	}
	return &cp
}

func (s *SessionTracker) cloneReplayTelemetryLocked() map[string]*storage.ReplayMatchStats {
	if len(s.replayTelemetry) == 0 {
		return nil
	}
	out := make(map[string]*storage.ReplayMatchStats, len(s.replayTelemetry))
	for k, v := range s.replayTelemetry {
		if v != nil {
			out[k] = cloneReplayMatchStats(v)
		}
	}
	return out
}

// AttachReplayTelemetry registers replay telemetry for a match GUID so derived For/Against
// session metrics hydrate immediately without requiring extra per-match storage round-trips.
func (s *SessionTracker) AttachReplayTelemetry(matchGUID string, stats *storage.ReplayMatchStats) bool {
	if stats == nil {
		return false
	}
	guid := strings.TrimSpace(matchGUID)
	if guid == "" {
		guid = strings.TrimSpace(stats.MatchGUID)
	}
	if guid == "" {
		return false
	}

	cloned := cloneReplayMatchStats(stats)
	if cloned.MatchGUID == "" {
		cloned.MatchGUID = guid
	}

	s.mu.Lock()
	if s.replayTelemetry == nil {
		s.replayTelemetry = make(map[string]*storage.ReplayMatchStats)
	}
	s.replayTelemetry[guid] = cloned
	s.replayTelemetry[strings.ToLower(guid)] = cloned

	for _, m := range s.matches {
		if m != nil && strings.EqualFold(strings.TrimSpace(m.MatchGUID), guid) && len(m.Players) == 0 && len(cloned.Players) > 0 {
			m.Players = sessionPlayersFromReplay(cloned, s.getLocalPlayerIDsLocked())
		}
	}
	rec := s.buildSessionRecordLocked(nil)
	s.mu.Unlock()

	s.saveRecordToStore(rec)
	s.broadcastSummaryUpdate()
	return true
}

func sessionPlayersFromReplay(st *storage.ReplayMatchStats, localIDs []string) []SessionMatchPlayer {
	localSet := buildLocalIDSet(localIDs, nil, nil)
	out := make([]SessionMatchPlayer, 0, len(st.Players))
	for _, rp := range st.Players {
		isLocal := rp.IsLocal || matchesLocalID(localSet, rp.Platform, rp.OnlineID)
		var won *bool
		if st.WinnerTeam != nil && (rp.Team == 0 || rp.Team == 1) {
			w := rp.Team == *st.WinnerTeam
			won = &w
		}
		pid := strings.TrimSpace(rp.OnlineID)
		if pid != "" && rp.Platform != "" && !strings.Contains(pid, "|") {
			pid = fmt.Sprintf("%s|%s|0", rp.Platform, pid)
		}
		p := SessionMatchPlayer{
			PlayerID:  pid,
			Platform:  rp.Platform,
			Name:      rp.Name,
			TeamNum:   rp.Team,
			IsLocal:   isLocal,
			IsBot:     rp.IsBot,
			Won:       won,
			RankName:  rp.RankName,
			ShortRank: rp.ShortRank,
			MMR:       rp.MMR,
		}
		p.Stats.Score = rp.Score
		p.Stats.Goals = rp.Goals
		p.Stats.Assists = rp.Assists
		p.Stats.Saves = rp.Saves
		p.Stats.Shots = rp.Shots
		p.Stats.Demos = rp.Demos
		if rp.Rank != nil {
			p.Tier = rp.Rank.Tier
			p.Division = rp.Rank.Division
		}
		out = append(out, p)
	}
	return out
}

func computeSessionDerivedStats(
	playlists map[string]*PlaylistSessionStats,
	matches []*SessionMatchDetail,
	localIDs []string,
	store storage.StateStore,
) (*MostPlayedPlaylistSummary, *storage.PlayerRoleStatsSummary, *storage.PlayerRoleStatsSummary) {
	return computeSessionDerivedStatsWithCache(playlists, matches, localIDs, store, nil, nil, nil)
}

func computeSessionDerivedStatsWithCache(
	playlists map[string]*PlaylistSessionStats,
	matches []*SessionMatchDetail,
	localIDs []string,
	store storage.StateStore,
	preloadedLocalIDs []string,
	histByGUID map[string]*storage.HistoricalMatchRecord,
	memTelemetry map[string]*storage.ReplayMatchStats,
) (*MostPlayedPlaylistSummary, *storage.PlayerRoleStatsSummary, *storage.PlayerRoleStatsSummary) {
	mostPlayed := computeMostPlayedPlaylist(playlists)
	localSet := buildLocalIDSet(localIDs, preloadedLocalIDs, store)

	if histByGUID == nil && store != nil && len(matches) > 0 {
		histByGUID = batchLoadHistoricalMatches(context.Background(), store, matches)
	}

	var forAcc, againstAcc storage.RoleStatsAccumulator
	for _, m := range matches {
		if m == nil {
			continue
		}
		youWon := m.Result == "victory"
		isDraw := m.Result != "victory" && m.Result != "defeat"
		localTeam := resolveLocalTeam(m, localSet)

		forAcc.AddMatchOutcome(youWon, isDraw)
		againstAcc.AddMatchOutcome(youWon, isDraw)

		var telemetry *storage.ReplayMatchStats
		guid := strings.TrimSpace(m.MatchGUID)
		if guid != "" && len(memTelemetry) > 0 {
			if st, ok := memTelemetry[guid]; ok && st != nil {
				telemetry = st
			} else if st, ok := memTelemetry[strings.ToLower(guid)]; ok && st != nil {
				telemetry = st
			}
		}
		if telemetry == nil && guid != "" && len(histByGUID) > 0 {
			if hm := lookupHistoricalMatch(histByGUID, guid); hm != nil {
				telemetry = extractReplayStatsFromHistorical(hm)
			}
		}

		if telemetry != nil && len(telemetry.Players) > 0 {
			accumulateReplayTelemetry(&forAcc, &againstAcc, telemetry, m, localTeam, localSet)
			continue
		}
		accumulateMatchPlayersFallback(&forAcc, &againstAcc, m, localTeam, localSet)
	}

	return mostPlayed, forAcc.Finalize(0, 0), againstAcc.Finalize(0, 0)
}

func buildLocalIDSet(localIDs, preloadedLocalIDs []string, store storage.StateStore) map[string]bool {
	localSet := make(map[string]bool, len(localIDs)+len(preloadedLocalIDs))
	addIDs := func(ids []string) {
		for _, id := range ids {
			if trimmed := strings.TrimSpace(id); trimmed != "" {
				localSet[trimmed] = true
				localSet[strings.ToLower(trimmed)] = true
			}
		}
	}
	addIDs(localIDs)
	if len(localSet) == 0 {
		addIDs(preloadedLocalIDs)
	}
	if len(localSet) == 0 && store != nil {
		if ids, err := store.GetLocalPlayerIDs(context.Background()); err == nil {
			addIDs(ids)
		}
	}
	return localSet
}

// batchLoadHistoricalMatches loads enriched replay/player telemetry for a set of session matches
// using paginated ListHistoricalMatches batch queries (eliminating O(N) per-match GetMatch + GetMatchPlayers scans).
func batchLoadHistoricalMatches(ctx context.Context, store storage.StateStore, matches []*SessionMatchDetail) map[string]*storage.HistoricalMatchRecord {
	if store == nil || len(matches) == 0 {
		return nil
	}
	needed := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		if m != nil {
			if g := strings.ToLower(strings.TrimSpace(m.MatchGUID)); g != "" {
				needed[g] = struct{}{}
			}
		}
	}
	if len(needed) == 0 {
		return nil
	}

	result := make(map[string]*storage.HistoricalMatchRecord, len(needed)*2)
	remaining := len(needed)
	for offset := 0; remaining > 0; {
		page, total, err := store.ListHistoricalMatches(ctx, storage.MatchFilter{
			Limit:  historicalMatchBatchPageSize,
			Offset: offset,
		})
		if err != nil || len(page) == 0 {
			break
		}
		for _, hm := range page {
			if hm == nil {
				continue
			}
			guid := strings.TrimSpace(hm.MatchGUID)
			if guid == "" {
				continue
			}
			lower := strings.ToLower(guid)
			if _, ok := needed[lower]; ok {
				if _, already := result[lower]; !already {
					remaining--
				}
				result[guid] = hm
				result[lower] = hm
			}
		}
		offset += len(page)
		if offset >= total {
			break
		}
	}
	return result
}

func lookupHistoricalMatch(histByGUID map[string]*storage.HistoricalMatchRecord, guid string) *storage.HistoricalMatchRecord {
	if hm, ok := histByGUID[guid]; ok {
		return hm
	}
	return histByGUID[strings.ToLower(guid)]
}

func extractReplayStatsFromHistorical(hm *storage.HistoricalMatchRecord) *storage.ReplayMatchStats {
	if hm == nil {
		return nil
	}
	if hm.Stats != nil && len(hm.Stats.Players) > 0 {
		return hm.Stats
	}
	if strings.TrimSpace(hm.ReplayStatsJSON) != "" {
		var st storage.ReplayMatchStats
		if json.Unmarshal([]byte(hm.ReplayStatsJSON), &st) == nil && len(st.Players) > 0 {
			return &st
		}
	}
	return hm.Stats
}

func resolveLocalTeam(m *SessionMatchDetail, localSet map[string]bool) int {
	if m.LocalTeam != nil && *m.LocalTeam >= 0 {
		return *m.LocalTeam
	}
	for _, p := range m.Players {
		if p.IsLocal || matchesLocalID(localSet, p.Platform, p.PlayerID) {
			if p.TeamNum == 0 || p.TeamNum == 1 {
				return p.TeamNum
			}
		}
	}
	return 0
}

func matchesLocalID(localSet map[string]bool, platform, rawID string) bool {
	if len(localSet) == 0 {
		return false
	}
	trimmed := strings.TrimSpace(rawID)
	if trimmed != "" {
		if localSet[trimmed] || localSet[strings.ToLower(trimmed)] {
			return true
		}
		if platform != "" {
			full := fmt.Sprintf("%s|%s|0", platform, trimmed)
			if localSet[full] || localSet[strings.ToLower(full)] {
				return true
			}
		}
	}
	return false
}

func accumulateReplayTelemetry(
	forAcc, againstAcc *storage.RoleStatsAccumulator,
	st *storage.ReplayMatchStats,
	m *SessionMatchDetail,
	localTeam int,
	localSet map[string]bool,
) {
	var localRP *storage.ReplayPlayerStats
	var localLocked bool
	var oppRPs []storage.ReplayPlayerStats

	for i := range st.Players {
		rp := &st.Players[i]
		isLocal := rp.IsLocal || matchesLocalID(localSet, rp.Platform, rp.OnlineID)
		if isLocal {
			localRP = rp
			localLocked = true
		} else if rp.Team == localTeam && !localLocked && localRP == nil {
			localRP = rp
		} else if rp.Team != localTeam && (rp.Team == 0 || rp.Team == 1) {
			oppRPs = append(oppRPs, *rp)
		}
	}

	if localRP != nil {
		b, _ := json.Marshal(localRP)
		forAcc.AddPlayerBoxAndTelemetry(localRP.Score, localRP.Goals, localRP.Assists, localRP.Saves, localRP.Shots, localRP.Demos, string(b))
	} else {
		forAcc.AddPlayerBoxAndTelemetry(0, 0, 0, 0, 0, 0, "")
	}

	if len(oppRPs) > 0 {
		for _, op := range oppRPs {
			b, _ := json.Marshal(op)
			againstAcc.AddPlayerBoxAndTelemetry(op.Score, op.Goals, op.Assists, op.Saves, op.Shots, op.Demos, string(b))
		}
	} else {
		oppGoals := m.OrangeScore
		if localTeam == 1 {
			oppGoals = m.BlueScore
		}
		againstAcc.AddPlayerBoxAndTelemetry(0, oppGoals, 0, 0, 0, 0, "")
	}
}

func accumulateMatchPlayersFallback(
	forAcc, againstAcc *storage.RoleStatsAccumulator,
	m *SessionMatchDetail,
	localTeam int,
	localSet map[string]bool,
) {
	var localPlayer *SessionMatchPlayer
	var localLocked bool
	var oppScore, oppGoals, oppAssists, oppSaves, oppShots, oppDemos, oppCount int

	for i := range m.Players {
		p := &m.Players[i]
		if p.IsLocal || matchesLocalID(localSet, p.Platform, p.PlayerID) {
			localPlayer = p
			localLocked = true
		} else if p.TeamNum == localTeam && !localLocked && localPlayer == nil {
			localPlayer = p
		} else if p.TeamNum != localTeam && (p.TeamNum == 0 || p.TeamNum == 1) {
			oppCount++
			oppScore += p.Stats.Score
			oppGoals += p.Stats.Goals
			oppAssists += p.Stats.Assists
			oppSaves += p.Stats.Saves
			oppShots += p.Stats.Shots
			oppDemos += p.Stats.Demos
		}
	}

	if localPlayer != nil {
		forAcc.AddPlayerBoxAndTelemetry(
			localPlayer.Stats.Score,
			localPlayer.Stats.Goals,
			localPlayer.Stats.Assists,
			localPlayer.Stats.Saves,
			localPlayer.Stats.Shots,
			localPlayer.Stats.Demos,
			"",
		)
	} else {
		myGoals := m.BlueScore
		if localTeam == 1 {
			myGoals = m.OrangeScore
		}
		forAcc.AddPlayerBoxAndTelemetry(0, myGoals, 0, 0, 0, 0, "")
	}

	if oppCount == 0 {
		oppGoals = m.OrangeScore
		if localTeam == 1 {
			oppGoals = m.BlueScore
		}
	}
	againstAcc.AddPlayerBoxAndTelemetry(oppScore, oppGoals, oppAssists, oppSaves, oppShots, oppDemos, "")
}
