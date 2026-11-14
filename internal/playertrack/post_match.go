package playertrack

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rlapi"
)

// OnMatchEnded processes a match completion event and atomically records outcomes.
func (t *Tracker) OnMatchEnded(ctx context.Context, matchGUID string, winnerTeamNum *int) error {
	trimmedGUID := strings.TrimSpace(matchGUID)
	if trimmedGUID == "" {
		return nil
	}

	if winnerTeamNum == nil || (*winnerTeamNum != 0 && *winnerTeamNum != 1) {
		t.logger.Info("match ended without valid winner; skipping outcome compilation",
			slog.String("match_guid", trimmedGUID),
		)
		t.concludeMatchWithoutOutcomes(trimmedGUID, winnerTeamNum)
		return nil
	}

	t.mu.Lock()
	if t.currentMatch == nil || t.currentMatch.MatchGUID != trimmedGUID {
		t.mu.Unlock()
		t.logger.Warn("match ended but no active match state cached; skipping outcome recording",
			slog.String("match_guid", trimmedGUID),
		)
		return nil
	}

	matchState := t.currentMatch
	matchState.MatchEnded = true
	matchState.ActiveMatch = false
	matchState.WinnerTeam = winnerTeamNum

	localPlayer := matchState.LocalPlayer
	localTeam := matchState.LocalTeam
	playlistID := matchState.PlaylistID

	if localPlayer == nil || localTeam == nil || (*localTeam != 0 && *localTeam != 1) {
		matchClone := matchState.DeepClone()
		listener := t.listener
		t.mu.Unlock()
		t.logger.Warn("match ended but local player not playing on a valid team; skipping matchup outcomes",
			slog.String("match_guid", trimmedGUID),
		)
		if listener != nil && matchClone != nil {
			listener.OnMatchConcluded(matchClone)
		}
		return nil
	}

	myTeamNum := *localTeam
	myTeamWon := *winnerTeamNum == myTeamNum
	if myTeamWon {
		matchState.Result = "victory"
	} else {
		matchState.Result = "defeat"
	}

	var (
		localPID             string
		preMatchMMR          float64
		preMatchesPlayed     int
		needBackgroundPoller bool
		immediateNewRank     *PlayerPlaylistRank
		immediateSkills      []rlapi.Skill
		matchEndStartedAt    = time.Now().UTC()
		participantIDs       []rlapi.PlayerID
	)

	localPID = strings.TrimSpace(localPlayer.PlayerID)
	preMatchMMR, preMatchesPlayed = t.capturePreMatchMMR(localPID, playlistID, localPlayer)
	participantIDs = collectParticipantIDs(localPID, matchState)

	delete(t.rankCache, localPID)
	delete(t.inFlight, localPID)
	delete(t.failBackoff, localPID)

	if t.cfg.AutoFetchRanks && t.rankClient != nil && t.rankClient.IsEnabled() {
		t.mu.Unlock()
		fetchCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		skills, err := t.rankClient.GetPlayersSkills(fetchCtx, []rlapi.PlayerID{rlapi.PlayerID(localPID)})
		cancel()
		t.mu.Lock()
		if err == nil && len(skills) > 0 {
			immediateSkills = skills[0].Skills
			snapshot := BuildRanksSnapshot(immediateSkills)
			var immediateUpdated bool
			if r, ok := snapshot.GetRank(playlistID); ok {
				if matchState.LocalPlayer != nil {
					rCopy := r
					matchState.LocalPlayer.CurrentRank = &rCopy
					matchState.LocalPlayer.Ranks = snapshot
				}
				if hasPostMatchMMRUpdated(r, preMatchMMR, preMatchesPlayed) {
					immediateUpdated = true
					rCopy := r
					immediateNewRank = &rCopy
				}
			}
			if immediateUpdated {
				t.rankCache[localPID] = cachedRankEntry{
					snapshot:  snapshot,
					fetchedAt: time.Now().UTC(),
				}
				t.boundRankCacheLocked()
				t.mmrPollerState = "synced"
				t.lastMMRSyncLatencyMs = time.Since(matchEndStartedAt).Milliseconds()
			} else {
				t.mmrPollerState = "polling"
				needBackgroundPoller = true
			}
		} else {
			t.mmrPollerState = "polling"
			needBackgroundPoller = true
		}
	}

	concludedSnapshot := matchState.DeepClone()
	listener := t.listener
	outcomes := compileMatchOutcomes(matchState, localPlayer.PlayerID, myTeamNum, myTeamWon)
	t.mu.Unlock()

	if err := t.store.RecordMatchResults(ctx, trimmedGUID, playlistID, outcomes); err != nil {
		if errors.Is(err, storage.ErrMatchAlreadyProcessed) {
			t.logger.Debug("match already processed, ignoring duplicate MatchEnded event",
				slog.String("match_guid", trimmedGUID),
			)
			if listener != nil && concludedSnapshot != nil {
				listener.OnMatchConcluded(concludedSnapshot)
			}
			return nil
		}
		t.logger.Error("failed to record match results in store",
			slog.String("match_guid", trimmedGUID),
			slog.Any("error", err),
		)
		return err
	}

	if immediateNewRank != nil && playlistID > 0 && immediateNewRank.MMR > 0 {
		t.persistMMRSnapshot(ctx, trimmedGUID, playlistID, localPID, preMatchMMR, immediateNewRank, immediateSkills, time.Now().UTC(), nil)
	}

	t.mu.Lock()
	for _, o := range outcomes {
		delete(t.matchupCache, fmt.Sprintf("%s:%d", o.PlayerID, playlistID))
	}
	t.mu.Unlock()

	t.logger.Info("successfully recorded match results",
		slog.String("match_guid", trimmedGUID),
		slog.Int("playlist_id", playlistID),
		slog.Bool("my_team_won", myTeamWon),
		slog.Int("players_tracked", len(outcomes)),
	)

	if listener != nil && concludedSnapshot != nil {
		listener.OnMatchConcluded(concludedSnapshot)
	}

	if needBackgroundPoller && localPID != "" {
		t.spawnPostMatchMMRPoller(trimmedGUID, playlistID, localPID, preMatchMMR, preMatchesPlayed, participantIDs, matchEndStartedAt)
	}

	return nil
}

func (t *Tracker) concludeMatchWithoutOutcomes(matchGUID string, winnerTeamNum *int) {
	t.mu.Lock()
	var matchClone *CurrentMatchResponse
	if t.currentMatch != nil && t.currentMatch.MatchGUID == matchGUID {
		t.currentMatch.MatchEnded = true
		t.currentMatch.ActiveMatch = false
		t.currentMatch.WinnerTeam = winnerTeamNum
		matchClone = t.currentMatch.DeepClone()
	}
	listener := t.listener
	t.mu.Unlock()

	if listener != nil && matchClone != nil {
		listener.OnMatchConcluded(matchClone)
	}
}

func (t *Tracker) capturePreMatchMMR(localPID string, playlistID int, localPlayer *LobbyPlayer) (float64, int) {
	if localPlayer.CurrentRank != nil && localPlayer.CurrentRank.PlaylistID == playlistID {
		return localPlayer.CurrentRank.MMR, localPlayer.CurrentRank.MatchesPlayed
	}
	if cached, ok := t.rankCache[localPID]; ok {
		if r, ok := cached.snapshot.GetRank(playlistID); ok {
			return r.MMR, r.MatchesPlayed
		}
	}
	return 0, 0
}

func collectParticipantIDs(localPID string, matchState *CurrentMatchResponse) []rlapi.PlayerID {
	var participantIDs []rlapi.PlayerID
	seen := map[string]bool{}
	if localPID != "" {
		participantIDs = append(participantIDs, rlapi.PlayerID(localPID))
		seen[localPID] = true
	}
	for _, roster := range [][]LobbyPlayer{matchState.Teammates, matchState.Opponents} {
		for _, p := range roster {
			pID := strings.TrimSpace(p.PlayerID)
			if !p.IsBot && pID != "" && !seen[pID] {
				seen[pID] = true
				participantIDs = append(participantIDs, rlapi.PlayerID(pID))
			}
		}
	}
	return participantIDs
}

func hasPostMatchMMRUpdated(r PlayerPlaylistRank, preMatchMMR float64, preMatchesPlayed int) bool {
	return (preMatchMMR > 0 && r.MMR != preMatchMMR) ||
		(preMatchesPlayed > 0 && r.MatchesPlayed > preMatchesPlayed) ||
		(preMatchMMR == 0 && preMatchesPlayed == 0 && r.MMR > 0)
}

func compileMatchOutcomes(matchState *CurrentMatchResponse, localPlayerID string, myTeamNum int, myTeamWon bool) []storage.PlayerOutcome {
	roster := make([]LobbyPlayer, 0, len(matchState.Teammates)+len(matchState.Opponents))
	roster = append(roster, matchState.Teammates...)
	roster = append(roster, matchState.Opponents...)

	outcomes := make([]storage.PlayerOutcome, 0, len(roster))
	seen := make(map[string]bool)
	normLocal := strings.TrimSpace(localPlayerID)

	for _, p := range roster {
		if p.IsBot || p.IsLocal || strings.EqualFold(strings.TrimSpace(p.PlayerID), normLocal) || seen[p.PlayerID] {
			continue
		}
		seen[p.PlayerID] = true
		outcomes = append(outcomes, storage.PlayerOutcome{
			PlayerID:   p.PlayerID,
			Platform:   p.Platform,
			PlayerName: p.Name,
			IsTeammate: p.TeamNum == myTeamNum,
			Won:        myTeamWon,
		})
	}
	return outcomes
}

func (t *Tracker) persistMMRSnapshot(
	ctx context.Context,
	matchGUID string,
	playlistID int,
	localPID string,
	preMatchMMR float64,
	newRank *PlayerPlaylistRank,
	skills []rlapi.Skill,
	recordedAt time.Time,
	listener MatchStateListener,
) {
	if localPID != "" && len(skills) > 0 {
		if ranksJSON, err := SerializeRanksJSON(skills); err == nil {
			_ = t.store.UpdatePlayerRanks(ctx, localPID, ranksJSON)
		}
	}
	if playlistID <= 0 || newRank == nil || newRank.MMR <= 0 {
		return
	}
	mmrDelta := 0.0
	if preMatchMMR > 0 {
		mmrDelta = math.Round((newRank.MMR-preMatchMMR)*100) / 100
	}
	newMMRInt := int(math.Round(newRank.MMR))
	_ = t.store.RecordMMR(ctx, &storage.MMRHistoryRecord{
		MatchGUID:  matchGUID,
		PlaylistID: playlistID,
		MMR:        newMMRInt,
		MMRChange:  int(math.Round(mmrDelta)),
		Tier:       newRank.Tier,
		Division:   newRank.Division,
		RecordedAt: recordedAt,
	})
	if reconciler, ok := listener.(PostMatchMMRReconciler); ok {
		reconciler.UpdateLatestMatchMMR(playlistID, newMMRInt, mmrDelta)
	}
}

// spawnPostMatchMMRPoller launches a tracked background goroutine that polls PsyNet with stepped backoff
// (+2s, +5s, +9s, +15s, +25s) until post-match MMR or MatchesPlayed updates.
func (t *Tracker) spawnPostMatchMMRPoller(
	matchGUID string,
	playlistID int,
	localPID string,
	preMatchMMR float64,
	preMatchesPlayed int,
	participantIDs []rlapi.PlayerID,
	startedAt time.Time,
) {
	t.mu.Lock()
	if t.closed || t.rankClient == nil || !t.rankClient.IsEnabled() {
		t.mmrPollerState = "idle"
		t.mu.Unlock()
		return
	}
	if t.pollerCancel != nil {
		t.pollerCancel()
	}
	pollerCtx, pollerCancel := context.WithCancel(t.ctx)
	t.pollerCancel = pollerCancel
	schedule := append([]time.Duration(nil), t.postMatchPollSchedule...)
	if len(schedule) == 0 {
		schedule = append([]time.Duration(nil), defaultPostMatchPollSchedule...)
	}
	t.mmrPollerState = "polling"
	t.wg.Add(1)
	t.mu.Unlock()

	go func() {
		defer t.wg.Done()
		defer pollerCancel()

		var prevOffset time.Duration
		for idx, targetOffset := range schedule {
			waitDur := targetOffset - prevOffset
			if waitDur <= 0 {
				waitDur = targetOffset
			}
			prevOffset = targetOffset

			timer := time.NewTimer(waitDur)
			select {
			case <-pollerCtx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}

			fetchCtx, cancel := context.WithTimeout(pollerCtx, 4*time.Second)
			skillsList, err := t.rankClient.GetPlayersSkills(fetchCtx, participantIDs)
			cancel()
			if err != nil || len(skillsList) == 0 {
				continue
			}

			var (
				localSnapshot PlayerRanksSnapshot
				localRank     *PlayerPlaylistRank
				localSkills   []rlapi.Skill
			)

			now := time.Now().UTC()
			for _, entry := range skillsList {
				pid := strings.TrimSpace(string(entry.PlayerID))
				if pid == "" {
					continue
				}
				snap := BuildRanksSnapshot(entry.Skills)
				if strings.EqualFold(pid, localPID) {
					localSnapshot = snap
					localSkills = entry.Skills
					if r, ok := snap.GetRank(playlistID); ok {
						rCopy := r
						localRank = &rCopy
					}
				} else {
					t.mu.Lock()
					t.rankCache[pid] = cachedRankEntry{
						snapshot:  snap,
						fetchedAt: now,
					}
					t.boundRankCacheLocked()
					t.mu.Unlock()
					if ranksJSON, sErr := SerializeRanksJSON(entry.Skills); sErr == nil {
						_ = t.store.UpdatePlayerRanks(pollerCtx, pid, ranksJSON)
					}
				}
			}

			if localRank == nil {
				continue
			}

			updated := (localRank.MMR > 0 && localRank.MMR != preMatchMMR) ||
				(localRank.MatchesPlayed > preMatchesPlayed)
			isFinal := idx == len(schedule)-1

			if updated || isFinal {
				t.mu.Lock()
				t.rankCache[localPID] = cachedRankEntry{
					snapshot:  localSnapshot,
					fetchedAt: now,
				}
				t.boundRankCacheLocked()
				if t.currentMatch != nil && t.currentMatch.MatchGUID == matchGUID && t.currentMatch.LocalPlayer != nil {
					rCopy := *localRank
					t.currentMatch.LocalPlayer.CurrentRank = &rCopy
					t.currentMatch.LocalPlayer.Ranks = localSnapshot
				}
				listener := t.listener
				t.mu.Unlock()

				t.persistMMRSnapshot(pollerCtx, matchGUID, playlistID, localPID, preMatchMMR, localRank, localSkills, now, listener)

				t.mu.Lock()
				t.mmrPollerState = "synced"
				t.lastMMRSyncLatencyMs = time.Since(startedAt).Milliseconds()
				t.mu.Unlock()
				return
			}
		}

		t.mu.Lock()
		if t.mmrPollerState == "polling" {
			t.mmrPollerState = "idle"
		}
		t.mu.Unlock()
	}()
}
