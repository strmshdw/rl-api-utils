package playertrack

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dank/rl-api-utils/internal/domain"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rlapi"
)

// OnUpdateState processes a real-time lobby state update from the Stats API Listener.
func (t *Tracker) OnUpdateState(ctx context.Context, matchGUID string, playlistID int, players []statsapi.StatsPlayer) error {
	return t.OnUpdateGameState(ctx, matchGUID, playlistID, players, nil)
}

// OnUpdateGameState processes a real-time lobby and match state update including clock telemetry.
func (t *Tracker) OnUpdateGameState(ctx context.Context, matchGUID string, playlistID int, players []statsapi.StatsPlayer, game *statsapi.StatsGame) error {
	trimmedGUID := strings.TrimSpace(matchGUID)
	if trimmedGUID == "" || len(players) == 0 {
		return nil
	}

	resolvedLocal, myTeamNum := t.resolveLocalPlayer(ctx, players)

	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return errors.New("tracker is closed")
	}

	now := time.Now().UTC()
	isSameMatch := t.currentMatch != nil && t.currentMatch.MatchGUID == trimmedGUID

	var (
		prevLocalPlayer *LobbyPlayer
		prevTeammates   []LobbyPlayer
		prevOpponents   []LobbyPlayer
		prevSpectators  []LobbyPlayer
	)

	if !isSameMatch {
		t.currentMatch = &CurrentMatchResponse{
			ActiveMatch:    true,
			MatchEnded:     false,
			MatchGUID:      trimmedGUID,
			PlaylistID:     playlistID,
			PlaylistName:   FormatPlaylist(playlistID),
			TimeRemaining:  game.GetTimeRemaining(),
			IsOvertime:     game.GetIsOvertime(),
			LocalPlayerIDs: t.getLocalPlayerIDsLocked(),
			Teammates:      make([]LobbyPlayer, 0),
			Opponents:      make([]LobbyPlayer, 0),
			Spectators:     make([]LobbyPlayer, 0),
			UpdatedAt:      now,
		}
		t.upsertedProfiles = make(map[string]string)
		t.matchupInFlight = make(map[string]time.Time)
	} else {
		prevLocalPlayer = t.currentMatch.LocalPlayer
		prevTeammates = t.currentMatch.Teammates
		prevOpponents = t.currentMatch.Opponents
		prevSpectators = t.currentMatch.Spectators

		if resolvedLocal == nil && t.currentMatch.LocalTeam != nil && (*t.currentMatch.LocalTeam == 0 || *t.currentMatch.LocalTeam == 1) {
			myTeamNum = *t.currentMatch.LocalTeam
		}
		if playlistID != 0 {
			t.currentMatch.PlaylistID = playlistID
			t.currentMatch.PlaylistName = FormatPlaylist(playlistID)
		}
		if game != nil {
			t.currentMatch.TimeRemaining = game.GetTimeRemaining()
			t.currentMatch.IsOvertime = game.GetIsOvertime()
		}
		if len(t.currentMatch.LocalPlayerIDs) == 0 {
			t.currentMatch.LocalPlayerIDs = t.getLocalPlayerIDsLocked()
		}
		t.currentMatch.UpdatedAt = now
		if !t.currentMatch.MatchEnded {
			t.currentMatch.ActiveMatch = true
		}
	}

	if myTeamNum == 0 || myTeamNum == 1 {
		val := myTeamNum
		t.currentMatch.LocalTeam = &val
	} else if !isSameMatch {
		t.currentMatch.LocalTeam = nil
	}

	var (
		teammates             []LobbyPlayer
		opponents             []LobbyPlayer
		spectators            []LobbyPlayer
		localPlayerLobby      *LobbyPlayer
		playersToUpsert       []statsapi.StatsPlayer
		playersToFetchRank    []rlapi.PlayerID
		playersToQueryMatchup []statsapi.StatsPlayer
	)

	seenThisFrame := make(map[string]bool, len(players))
	activePlaylistID := t.currentMatch.PlaylistID

	for _, p := range players {
		isBot := p.IsBot()
		normID := domain.NormalizeID(p.PrimaryId)
		if !isBot && normID != "" {
			if seenThisFrame[normID] {
				continue
			}
			seenThisFrame[normID] = true
		}

		isLocal := resolvedLocal != nil && strings.EqualFold(strings.TrimSpace(p.PrimaryId), strings.TrimSpace(resolvedLocal.PrimaryID))
		if !isBot && strings.TrimSpace(p.PrimaryId) != "" {
			if lastSeenName, exists := t.upsertedProfiles[p.PrimaryId]; !exists || lastSeenName != p.Name {
				playersToUpsert = append(playersToUpsert, p)
				t.upsertedProfiles[p.PrimaryId] = p.Name
			}
			if t.shouldQueueRankFetchLocked(p.PrimaryId, now) {
				t.inFlight[p.PrimaryId] = now
				playersToFetchRank = append(playersToFetchRank, rlapi.PlayerID(p.PrimaryId))
			}
		}

		var ranksSnapshot PlayerRanksSnapshot
		var currentRank *PlayerPlaylistRank
		if entry, ok := t.rankCache[p.PrimaryId]; ok {
			ranksSnapshot = entry.snapshot
			if r, found := ranksSnapshot.GetRank(activePlaylistID); found {
				currentRank = &r
			}
		}

		var matchupRecord *storage.PlayerMatchup
		if activePlaylistID != 0 && !isBot {
			mKey := fmt.Sprintf("%s:%d", p.PrimaryId, activePlaylistID)
			if m, ok := t.matchupCache[mKey]; ok {
				matchupRecord = m
			} else if !isLocal {
				if _, inFlight := t.matchupInFlight[mKey]; !inFlight {
					t.matchupInFlight[mKey] = now
					playersToQueryMatchup = append(playersToQueryMatchup, p)
				}
			}
		}

		lp := LobbyPlayer{
			PlayerID: p.PrimaryId,
			Platform: ParsePlayerPlatform(p.PrimaryId),
			Name:     p.Name,
			TeamNum:  p.TeamNum,
			IsLocal:  isLocal,
			IsBot:    isBot,
			Stats: PlayerStatsSummary{
				Score:   p.Score,
				Goals:   p.Goals,
				Assists: p.Assists,
				Saves:   p.Saves,
				Shots:   p.Shots,
				Demos:   p.Demos,
			},
			CurrentRank:   currentRank,
			Ranks:         ranksSnapshot,
			MatchupRecord: matchupRecord,
		}

		if isLocal {
			localPlayerLobby = &lp
		} else if (myTeamNum == 0 || myTeamNum == 1) && p.TeamNum == myTeamNum {
			teammates = append(teammates, lp)
		} else if (myTeamNum == 0 || myTeamNum == 1) && (p.TeamNum == 0 || p.TeamNum == 1) {
			opponents = append(opponents, lp)
		} else {
			spectators = append(spectators, lp)
		}
	}

	// Differential retention: preserve human players who disconnected mid-match.
	if isSameMatch {
		if localPlayerLobby == nil && prevLocalPlayer != nil {
			retainedLocal := prevLocalPlayer.DeepClone()
			retainedLocal.IsDisconnected = true
			localPlayerLobby = &retainedLocal
			if normID := domain.NormalizeID(retainedLocal.PlayerID); normID != "" {
				seenThisFrame[normID] = true
			}
		}
		teammates = append(teammates, retainDisconnectedRoster(prevTeammates, seenThisFrame)...)
		opponents = append(opponents, retainDisconnectedRoster(prevOpponents, seenThisFrame)...)
		spectators = append(spectators, retainDisconnectedRoster(prevSpectators, seenThisFrame)...)
	}

	t.currentMatch.LocalPlayer = localPlayerLobby
	t.currentMatch.Teammates = teammates
	t.currentMatch.Opponents = opponents
	t.currentMatch.Spectators = spectators

	matchClone := t.currentMatch.DeepClone()
	listener := t.listener
	t.mu.Unlock()

	if listener != nil && matchClone != nil {
		listener.OnActiveMatchUpdated(matchClone)
	}

	t.upsertNewPlayers(ctx, playersToUpsert, now)
	t.warmMatchupCache(ctx, trimmedGUID, activePlaylistID, playersToQueryMatchup)
	t.dispatchRankFetch(playersToFetchRank)

	return nil
}

func (t *Tracker) shouldQueueRankFetchLocked(pid string, now time.Time) bool {
	if !t.cfg.AutoFetchRanks || t.rankClient == nil || !t.rankClient.IsEnabled() {
		return false
	}
	if entry, ok := t.rankCache[pid]; ok && now.Sub(entry.fetchedAt) < t.rankCacheTTL {
		return false
	}
	if _, inFlight := t.inFlight[pid]; inFlight {
		return false
	}
	if failTime, failed := t.failBackoff[pid]; failed && now.Sub(failTime) < t.backoffTTL {
		return false
	}
	return true
}

func retainDisconnectedRoster(prev []LobbyPlayer, seenThisFrame map[string]bool) []LobbyPlayer {
	var retainedList []LobbyPlayer
	for _, oldP := range prev {
		if oldP.IsBot {
			continue
		}
		normID := domain.NormalizeID(oldP.PlayerID)
		if normID != "" && !seenThisFrame[normID] {
			retained := oldP.DeepClone()
			retained.IsDisconnected = true
			retainedList = append(retainedList, retained)
			seenThisFrame[normID] = true
		}
	}
	return retainedList
}

func (t *Tracker) upsertNewPlayers(ctx context.Context, players []statsapi.StatsPlayer, now time.Time) {
	for _, p := range players {
		rec := &storage.PlayerRecord{
			PlayerID:    p.PrimaryId,
			Platform:    ParsePlayerPlatform(p.PrimaryId),
			PlayerName:  p.Name,
			RanksJSON:   "{}",
			FirstSeenAt: now,
			LastSeenAt:  now,
		}
		if err := t.store.UpsertPlayer(ctx, rec); err != nil {
			t.logger.Warn("failed to upsert player profile",
				slog.String("player_id", p.PrimaryId),
				slog.Any("error", err),
			)
		}
	}
}

func (t *Tracker) warmMatchupCache(ctx context.Context, matchGUID string, playlistID int, players []statsapi.StatsPlayer) {
	if len(players) == 0 || playlistID == 0 {
		return
	}
	var updatedSnapshot *CurrentMatchResponse
	for _, p := range players {
		mKey := fmt.Sprintf("%s:%d", p.PrimaryId, playlistID)
		matchup, err := t.store.GetPlayerMatchup(ctx, p.PrimaryId, playlistID)

		t.mu.Lock()
		delete(t.matchupInFlight, mKey)
		if err == nil && matchup != nil {
			t.matchupCache[mKey] = matchup
			t.boundMatchupCacheLocked()
			if t.currentMatch != nil && t.currentMatch.MatchGUID == matchGUID {
				t.updateLobbyPlayerMatchupLocked(p.PrimaryId, matchup)
				updatedSnapshot = t.currentMatch.DeepClone()
			}
		}
		t.mu.Unlock()
	}
	if updatedSnapshot != nil {
		t.mu.RLock()
		l := t.listener
		t.mu.RUnlock()
		if l != nil {
			l.OnActiveMatchUpdated(updatedSnapshot)
		}
	}
}

func (t *Tracker) dispatchRankFetch(players []rlapi.PlayerID) {
	if len(players) == 0 {
		return
	}
	t.mu.Lock()
	if !t.closed {
		t.wg.Add(1)
		go func(pids []rlapi.PlayerID) {
			defer t.wg.Done()
			t.fetchRanksBatch(t.ctx, pids)
		}(players)
	} else {
		for _, pid := range players {
			delete(t.inFlight, string(pid))
		}
	}
	t.mu.Unlock()
}

func visitMatchPlayers(match *CurrentMatchResponse, playerID string, fn func(*LobbyPlayer)) {
	if match == nil {
		return
	}
	target := strings.TrimSpace(playerID)
	apply := func(p *LobbyPlayer) {
		if p != nil && strings.EqualFold(strings.TrimSpace(p.PlayerID), target) {
			fn(p)
		}
	}
	apply(match.LocalPlayer)
	for i := range match.Teammates {
		apply(&match.Teammates[i])
	}
	for i := range match.Opponents {
		apply(&match.Opponents[i])
	}
	for i := range match.Spectators {
		apply(&match.Spectators[i])
	}
}

func (t *Tracker) updateLobbyPlayerRanksLocked(playerID string, snapshot PlayerRanksSnapshot) {
	if t.currentMatch == nil {
		return
	}
	pid := t.currentMatch.PlaylistID
	var currentRank *PlayerPlaylistRank
	if r, ok := snapshot.GetRank(pid); ok {
		currentRank = &r
	}
	visitMatchPlayers(t.currentMatch, playerID, func(p *LobbyPlayer) {
		p.Ranks = snapshot
		p.CurrentRank = currentRank
	})
}

func (t *Tracker) updateLobbyPlayerMatchupLocked(playerID string, matchup *storage.PlayerMatchup) {
	visitMatchPlayers(t.currentMatch, playerID, func(p *LobbyPlayer) {
		p.MatchupRecord = matchup
	})
}
