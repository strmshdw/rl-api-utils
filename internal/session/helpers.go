package session

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dank/rl-api-utils/internal/domain"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/storage"
)

func (s *SessionTracker) now() time.Time {
	if s.nowFunc != nil {
		return s.nowFunc().UTC()
	}
	return time.Now().UTC()
}

// SetClock updates the tracker's time source for deterministic testing or synchronized scheduling.
func (s *SessionTracker) SetClock(nowFn func() time.Time) {
	if nowFn == nil {
		return
	}
	s.mu.Lock()
	s.nowFunc = nowFn
	s.mu.Unlock()
}

// SetSummaryEnricher configures a callback applied to SessionResponse snapshots before
// returning or broadcasting session updates.
func (s *SessionTracker) SetSummaryEnricher(fn func(*SessionResponse) *SessionResponse) {
	s.mu.Lock()
	s.summaryEnricher = fn
	s.mu.Unlock()
}

func (s *SessionTracker) enrichedSummary() *SessionResponse {
	summary := s.GetSessionSummary()
	s.mu.RLock()
	enricher := s.summaryEnricher
	s.mu.RUnlock()
	if enricher != nil && summary != nil {
		return enricher(summary)
	}
	return summary
}

func (s *SessionTracker) broadcastSummaryUpdate() *SessionResponse {
	summary := s.enrichedSummary()
	if s.broadcaster != nil {
		s.broadcaster.Broadcast(SessionEvent{Event: EventSessionUpdate, Data: summary})
	}
	return summary
}

func formatUptime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
}

func resolvePlaylistName(playlistID int, existing string) string {
	if existing != "" {
		return existing
	}
	if playlistID != 0 {
		return domain.FormatPlaylist(playlistID)
	}
	return ""
}

func ensurePlaylistStatsLocked(
	playlists map[int]*PlaylistSessionStats,
	pid int,
	pName string,
	initialMMR, currentMMR float64,
) *PlaylistSessionStats {
	stats, ok := playlists[pid]
	if ok {
		if stats.PlaylistName == "" {
			stats.PlaylistName = resolvePlaylistName(pid, pName)
		}
		if stats.InitialMMR == 0 && initialMMR > 0 {
			stats.InitialMMR = initialMMR
		}
		return stats
	}
	stats = &PlaylistSessionStats{
		PlaylistID:   pid,
		PlaylistName: resolvePlaylistName(pid, pName),
		InitialMMR:   initialMMR,
		CurrentMMR:   currentMMR,
	}
	playlists[pid] = stats
	return stats
}

func applyPlaylistCurrentMMRLocked(stats *PlaylistSessionStats, currMMR float64) {
	if currMMR > 0 {
		stats.CurrentMMR = currMMR
		if stats.InitialMMR == 0 {
			stats.InitialMMR = currMMR
		}
	}
	stats.MMRDelta = domain.ComputeMMRDelta(stats.InitialMMR, stats.CurrentMMR)
}

func recordPlaylistOutcomeLocked(stats *PlaylistSessionStats, result string, endingMMR float64) {
	stats.MatchesPlayed++
	switch result {
	case "victory":
		stats.Wins++
	case "defeat":
		stats.Losses++
	}
	stats.WinRate = domain.ComputeWinRate(stats.Wins, stats.MatchesPlayed)
	applyPlaylistCurrentMMRLocked(stats, endingMMR)
}

func concludeMatchPlayers(match *playertrack.CurrentMatchResponse) ([]SessionMatchPlayer, int, int) {
	var blueScore, orangeScore int
	allPlayers := make([]playertrack.LobbyPlayer, 0, len(match.Teammates)+len(match.Opponents)+len(match.Spectators)+1)
	if match.LocalPlayer != nil {
		allPlayers = append(allPlayers, *match.LocalPlayer)
	}
	allPlayers = append(allPlayers, match.Teammates...)
	allPlayers = append(allPlayers, match.Opponents...)
	allPlayers = append(allPlayers, match.Spectators...)

	matchPlayers := make([]SessionMatchPlayer, 0, len(allPlayers))
	seenPlayerIDs := make(map[string]bool, len(allPlayers))

	for _, lp := range allPlayers {
		pid := strings.TrimSpace(lp.PlayerID)
		if pid != "" {
			if seenPlayerIDs[pid] {
				continue
			}
			seenPlayerIDs[pid] = true
		}

		if lp.TeamNum == 0 {
			blueScore += lp.Stats.Goals
		} else if lp.TeamNum == 1 {
			orangeScore += lp.Stats.Goals
		}

		var rankName, shortRank string
		var tier, div int
		var mmr float64
		if lp.CurrentRank != nil {
			rankName = lp.CurrentRank.RankName
			tier = lp.CurrentRank.Tier
			div = lp.CurrentRank.Division
			mmr = lp.CurrentRank.MMR
			shortRank = lp.CurrentRank.ShortRank
			if shortRank == "" && tier > 0 {
				effDiv := storage.Resolve1BasedDivision(div, rankName)
				shortRank = storage.FormatShortRankName(tier, effDiv)
			}
		}

		var won *bool
		if match.WinnerTeam != nil && (lp.TeamNum == 0 || lp.TeamNum == 1) {
			w := lp.TeamNum == *match.WinnerTeam
			won = &w
		}

		matchPlayers = append(matchPlayers, SessionMatchPlayer{
			PlayerID:       lp.PlayerID,
			Platform:       lp.Platform,
			Name:           lp.Name,
			TeamNum:        lp.TeamNum,
			IsLocal:        lp.IsLocal,
			IsBot:          lp.IsBot,
			IsDisconnected: lp.IsDisconnected,
			Won:            won,
			Stats:          lp.Stats,
			RankName:       rankName,
			ShortRank:      shortRank,
			Tier:           tier,
			Division:       div,
			MMR:            mmr,
			MatchupRecord:  lp.MatchupRecord,
		})
	}
	return matchPlayers, blueScore, orangeScore
}

func resolveMatchResult(match *playertrack.CurrentMatchResponse, blueScore, orangeScore int) string {
	if result := strings.ToLower(strings.TrimSpace(match.Result)); result != "" {
		return result
	}
	if match.WinnerTeam != nil && match.LocalTeam != nil {
		if *match.WinnerTeam == -1 {
			return "draw"
		}
		if *match.WinnerTeam == *match.LocalTeam {
			return "victory"
		}
		return "defeat"
	}
	if match.LocalTeam != nil {
		if *match.LocalTeam == 0 {
			if blueScore > orangeScore {
				return "victory"
			}
			if orangeScore > blueScore {
				return "defeat"
			}
			if blueScore > 0 || orangeScore > 0 {
				return "draw"
			}
			return "unknown"
		}
		if *match.LocalTeam == 1 {
			if orangeScore > blueScore {
				return "victory"
			}
			if blueScore > orangeScore {
				return "defeat"
			}
			if blueScore > 0 || orangeScore > 0 {
				return "draw"
			}
			return "unknown"
		}
	}
	if blueScore > orangeScore {
		return "victory"
	}
	if orangeScore > blueScore {
		return "defeat"
	}
	return "draw"
}

// SetContinueWindow updates the session continuation window duration.
func (s *SessionTracker) SetContinueWindow(d time.Duration) {
	if d < 0 {
		d = 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.continueWindow = d
}

// HasExplicitContinueWindow reports whether WithContinueWindow was explicitly supplied.
func (s *SessionTracker) HasExplicitContinueWindow() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.continueWindowExplicit
}

// ContinueWindow returns the configured session continuation window duration.
func (s *SessionTracker) ContinueWindow() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.continueWindow
}

// Broadcaster returns the underlying EventBroadcaster instance.
func (s *SessionTracker) Broadcaster() *EventBroadcaster {
	return s.broadcaster
}

// Subscribe registers a new listener channel for Server-Sent Events.
func (s *SessionTracker) Subscribe() (<-chan SessionEvent, func()) {
	return s.broadcaster.Subscribe()
}

// SetLocalPlayerIDs sets the local player IDs known to the daemon.
func (s *SessionTracker) SetLocalPlayerIDs(ids []string) {
	s.mu.Lock()
	s.localPlayerIDs = make([]string, len(ids))
	copy(s.localPlayerIDs, ids)
	rec := s.buildSessionRecordLocked(nil)
	s.mu.Unlock()
	s.saveRecordToStore(rec)
}

// GetLocalPlayerIDs returns the currently known local player IDs.
func (s *SessionTracker) GetLocalPlayerIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getLocalPlayerIDsLocked()
}

func (s *SessionTracker) getLocalPlayerIDsLocked() []string {
	if s.localPlayerIDs == nil {
		return []string{}
	}
	res := make([]string, len(s.localPlayerIDs))
	copy(res, s.localPlayerIDs)
	return res
}

// FilterSessionSummary scopes a SessionResponse to the requested playlist and date range.
func FilterSessionSummary(summary *SessionResponse, playlist string, fromUnix, toUnix int64, store storage.StateStore) *SessionResponse {
	if summary == nil {
		return nil
	}
	playlistIDs := storage.ParsePlaylistFilter(playlist)
	if len(playlistIDs) == 0 && fromUnix <= 0 && toUnix <= 0 {
		return summary
	}

	filteredMatches := make([]*SessionMatchDetail, 0, len(summary.Matches))
	filteredPlaylists := make(map[string]*PlaylistSessionStats)
	var wins, losses, totalMatches int

	for _, m := range summary.Matches {
		if m == nil || !storage.MatchesPlaylistIDs(m.PlaylistID, playlistIDs) {
			continue
		}
		matchTime := m.EndedAt
		if matchTime.IsZero() {
			matchTime = m.StartedAt
		}
		if !matchTime.IsZero() {
			ts := matchTime.Unix()
			if (fromUnix > 0 && ts < fromUnix) || (toUnix > 0 && ts > toUnix) {
				continue
			}
		}

		filteredMatches = append(filteredMatches, m)
		totalMatches++
		if m.Result == "victory" {
			wins++
		} else if m.Result == "defeat" {
			losses++
		}

		pKey := strconv.Itoa(m.PlaylistID)
		ps, ok := filteredPlaylists[pKey]
		if !ok {
			ps = &PlaylistSessionStats{
				PlaylistID:   m.PlaylistID,
				PlaylistName: resolvePlaylistName(m.PlaylistID, m.PlaylistName),
			}
			filteredPlaylists[pKey] = ps
		}
		ps.MatchesPlayed++
		if m.Result == "victory" {
			ps.Wins++
		} else if m.Result == "defeat" {
			ps.Losses++
		}
		ps.MMRDelta = domain.RoundFloat(ps.MMRDelta+m.MMRChange, 2)
		if m.EndingMMR > 0 {
			ps.CurrentMMR = m.EndingMMR
		}
	}

	cp := *summary
	cp.TotalMatches = totalMatches
	cp.TotalWins = wins
	cp.TotalLosses = losses
	cp.WinRate = domain.ComputeWinRate(wins, wins+losses)
	cp.Matches = filteredMatches
	cp.Playlists = filteredPlaylists
	cp.MostPlayedPlaylist, cp.ForStats, cp.AgainstStats = computeSessionDerivedStats(filteredPlaylists, filteredMatches, cp.LocalPlayerIDs, store)
	return &cp
}

func computeMostPlayedPlaylist(playlists map[string]*PlaylistSessionStats) *MostPlayedPlaylistSummary {
	var bestPS *PlaylistSessionStats
	for kStr, ps := range playlists {
		if ps == nil {
			continue
		}
		pid := ps.PlaylistID
		if pid == 0 {
			pid, _ = strconv.Atoi(kStr)
		}
		if bestPS == nil ||
			ps.MatchesPlayed > bestPS.MatchesPlayed ||
			(ps.MatchesPlayed == bestPS.MatchesPlayed && ps.Wins > bestPS.Wins) ||
			(ps.MatchesPlayed == bestPS.MatchesPlayed && ps.Wins == bestPS.Wins && pid > 0 && pid < bestPS.PlaylistID) {
			cp := *ps
			cp.PlaylistID = pid
			bestPS = &cp
		}
	}

	if bestPS == nil || (bestPS.MatchesPlayed <= 0 && bestPS.PlaylistID <= 0) {
		return nil
	}
	return &MostPlayedPlaylistSummary{
		ID:           bestPS.PlaylistID,
		Name:         resolvePlaylistName(bestPS.PlaylistID, bestPS.PlaylistName),
		NetMMR:       bestPS.MMRDelta,
		Wins:         bestPS.Wins,
		Losses:       bestPS.Losses,
		MatchesCount: bestPS.MatchesPlayed,
	}
}
