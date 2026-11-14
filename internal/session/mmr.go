package session

import (
	"strings"

	"github.com/dank/rl-api-utils/internal/domain"
)

// UpdateLatestMatchMMR reconciles the most recently concluded match's EndingMMR and MMRChange
// once PsyNet commits the post-match skill rating update, updates the playlist session tally,
// persists the updated session record, and broadcasts a session_update SSE event.
func (s *SessionTracker) UpdateLatestMatchMMR(playlistID int, newMMR int, mmrDelta float64) {
	if newMMR <= 0 {
		return
	}

	s.mu.Lock()
	var targetMatch *SessionMatchDetail
	for i := len(s.matches) - 1; i >= 0; i-- {
		m := s.matches[i]
		if playlistID == 0 || m.PlaylistID == playlistID {
			targetMatch = m
			break
		}
	}
	if targetMatch == nil && len(s.matches) > 0 {
		targetMatch = s.matches[len(s.matches)-1]
	}

	effPlaylistID := playlistID
	newMMRFloat := float64(newMMR)

	if targetMatch != nil {
		if effPlaylistID == 0 {
			effPlaylistID = targetMatch.PlaylistID
		}
		targetMatch.EndingMMR = newMMRFloat
		if mmrDelta != 0 {
			targetMatch.MMRChange = domain.RoundFloat(mmrDelta, 2)
			if targetMatch.StartingMMR == 0 && newMMRFloat-mmrDelta > 0 {
				targetMatch.StartingMMR = domain.RoundFloat(newMMRFloat-mmrDelta, 2)
			}
		} else {
			targetMatch.MMRChange = domain.ComputeMMRDelta(targetMatch.StartingMMR, newMMRFloat)
		}
		for i := range targetMatch.Players {
			if targetMatch.Players[i].IsLocal {
				targetMatch.Players[i].MMR = newMMRFloat
			}
		}
	}

	if effPlaylistID != 0 {
		initMMR := newMMRFloat - mmrDelta
		if targetMatch != nil && targetMatch.StartingMMR > 0 {
			initMMR = targetMatch.StartingMMR
		} else if initMMR <= 0 {
			initMMR = newMMRFloat
		} else {
			initMMR = domain.RoundFloat(initMMR, 2)
		}
		stats := ensurePlaylistStatsLocked(s.playlists, effPlaylistID, "", initMMR, newMMRFloat)
		if stats.InitialMMR == 0 {
			stats.InitialMMR = initMMR
		}
		applyPlaylistCurrentMMRLocked(stats, newMMRFloat)
	}

	currentRec := s.buildSessionRecordLocked(nil)
	s.mu.Unlock()

	s.saveRecordToStore(currentRec)
	s.broadcastSummaryUpdate()
}

// UpdateMatchEndingMMR updates the ending MMR and recalculated MMR delta for a concluded match.
func (s *SessionTracker) UpdateMatchEndingMMR(matchGUID string, endingMMR float64) {
	if strings.TrimSpace(matchGUID) == "" || endingMMR <= 0 {
		return
	}

	s.mu.Lock()
	var targetMatch *SessionMatchDetail
	for _, m := range s.matches {
		if m.MatchGUID == matchGUID {
			targetMatch = m
			break
		}
	}
	if targetMatch == nil {
		s.mu.Unlock()
		return
	}

	targetMatch.EndingMMR = endingMMR
	targetMatch.MMRChange = domain.ComputeMMRDelta(targetMatch.StartingMMR, endingMMR)

	if stats, ok := s.playlists[targetMatch.PlaylistID]; ok {
		stats.CurrentMMR = endingMMR
		if stats.InitialMMR > 0 {
			stats.MMRDelta = domain.ComputeMMRDelta(stats.InitialMMR, endingMMR)
		}
	}
	rec := s.buildSessionRecordLocked(nil)
	s.mu.Unlock()

	s.saveRecordToStore(rec)
	s.broadcastSummaryUpdate()
}
