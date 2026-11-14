package session

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dank/rl-api-utils/internal/domain"
	"github.com/dank/rl-api-utils/internal/storage"
)

func recordRefTime(rec *storage.SessionRecord) time.Time {
	if rec.LastMatchAt != nil && !rec.LastMatchAt.IsZero() {
		return *rec.LastMatchAt
	}
	if rec.EndedAt != nil && !rec.EndedAt.IsZero() {
		return *rec.EndedAt
	}
	return rec.StartedAt
}

func recordSortTime(rec *storage.SessionRecord) time.Time {
	if rec.LastMatchAt != nil && !rec.LastMatchAt.IsZero() {
		return *rec.LastMatchAt
	}
	return rec.StartedAt
}

func sortSessionRecordsDesc(recs []*storage.SessionRecord) {
	sort.Slice(recs, func(i, j int) bool {
		ti := recordSortTime(recs[i])
		tj := recordSortTime(recs[j])
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return recs[i].StartedAt.After(recs[j].StartedAt)
	})
}

func isWithinContinueWindow(refTime, now, currentFirstMatch time.Time, cw time.Duration) bool {
	if cw <= 0 {
		return false
	}
	if now.Sub(refTime) <= cw {
		return true
	}
	return !currentFirstMatch.IsZero() && currentFirstMatch.After(refTime) && currentFirstMatch.Sub(refTime) <= cw
}

// checkAutoRotateLocked checks if the current session has been idle longer than continueWindow
// since its last match ended. If so, it finalizes the current session and rotates to a new epoch.
// Caller must hold s.mu.
func (s *SessionTracker) checkAutoRotateLocked(now time.Time) *storage.SessionRecord {
	if len(s.matches) == 0 || s.continueWindow <= 0 || s.lastMatchAt == nil || s.lastMatchAt.IsZero() {
		return nil
	}
	if now.Sub(*s.lastMatchAt) <= s.continueWindow {
		return nil
	}
	endedAt := *s.lastMatchAt
	rec := s.buildSessionRecordLocked(&endedAt)
	s.resetEpochLocked(now)
	return rec
}

// DismissContinuableSession dismisses the continuation banner for a specific previous session
// (or the currently offered continuable session if sessionID is empty), persisting the dismissal
// to StateStore so it survives daemon restarts.
func (s *SessionTracker) DismissContinuableSession(sessionID string) *SessionResponse {
	targetID := strings.TrimSpace(sessionID)
	if targetID == "" {
		currentSummary := s.GetSessionSummary()
		if currentSummary.ContinuableSession != nil {
			targetID = currentSummary.ContinuableSession.SessionID
		}
	}

	var recToPersist *storage.SessionRecord
	if targetID != "" {
		if rec, err := s.lookupSessionRecord(targetID); err == nil && rec != nil {
			recCopy := *rec
			recCopy.Dismissed = true
			recCopy.UpdatedAt = s.now()
			recToPersist = &recCopy
		}
	}

	s.mu.Lock()
	if s.dismissedContinueIDs == nil {
		s.dismissedContinueIDs = make(map[string]bool)
	}
	if targetID != "" {
		s.dismissedContinueIDs[targetID] = true
		if memRec, ok := s.archivedMemorySessions[targetID]; ok && memRec != nil {
			memRec.Dismissed = true
			memRec.UpdatedAt = s.now()
			if recToPersist == nil {
				recCopy := *memRec
				recToPersist = &recCopy
			}
		}
		if recToPersist != nil {
			if s.archivedMemorySessions == nil {
				s.archivedMemorySessions = make(map[string]*storage.SessionRecord)
			}
			s.archivedMemorySessions[targetID] = recToPersist
		}
	}
	s.mu.Unlock()

	if recToPersist != nil {
		s.saveRecordToStore(recToPersist)
	}
	return s.broadcastSummaryUpdate()
}

// ContinuePreviousSession is an alias for ContinueSession.
func (s *SessionTracker) ContinuePreviousSession(targetSessionID string) (*SessionResponse, error) {
	return s.ContinueSession(targetSessionID)
}

// ContinueSession resumes a previous session (or merges the current session's matches into it)
// if the target session's last game played is within the configured continuation window.
// If targetSessionID is empty, the most recent eligible previous session is continued.
func (s *SessionTracker) ContinueSession(targetSessionID string) (*SessionResponse, error) {
	targetSessionID = strings.TrimSpace(targetSessionID)

	s.mu.RLock()
	currentID := s.id
	cw := s.continueWindow
	now := s.now()
	s.mu.RUnlock()

	if targetSessionID != "" && targetSessionID == currentID {
		return s.GetSessionSummary(), nil
	}

	targetRec, err := s.resolveContinuationTarget(targetSessionID, currentID, cw, now)
	if err != nil {
		return nil, err
	}

	refTime := recordRefTime(targetRec)

	s.mu.Lock()
	var currentFirstMatchStart time.Time
	if len(s.matches) > 0 {
		currentFirstMatchStart = s.matches[0].StartedAt
	}
	if !isWithinContinueWindow(refTime, now, currentFirstMatchStart, cw) {
		s.mu.Unlock()
		return nil, ErrContinueWindowExpired
	}

	var prevMatches []*SessionMatchDetail
	if strings.TrimSpace(targetRec.MatchesJSON) != "" {
		_ = json.Unmarshal([]byte(targetRec.MatchesJSON), &prevMatches)
	}
	var prevPlaylists map[string]*PlaylistSessionStats
	if strings.TrimSpace(targetRec.PlaylistsJSON) != "" {
		_ = json.Unmarshal([]byte(targetRec.PlaylistsJSON), &prevPlaylists)
	}

	oldSessionID := s.id
	hadCurrentMatches := len(s.matches) > 0
	combinedMatches, seenGUIDs := mergeChronologicalMatches(prevMatches, s.matches)
	newPlaylists, totalWins, totalLosses, latestEnd := rebuildSessionTallies(combinedMatches, prevPlaylists, s.activeMatch)

	s.id = targetRec.SessionID
	if !targetRec.StartedAt.IsZero() && (targetRec.StartedAt.Before(s.startedAt) || !hadCurrentMatches) {
		s.startedAt = targetRec.StartedAt.UTC()
	}
	if !latestEnd.IsZero() {
		t := latestEnd.UTC()
		s.lastMatchAt = &t
	} else if targetRec.LastMatchAt != nil && !targetRec.LastMatchAt.IsZero() {
		t := targetRec.LastMatchAt.UTC()
		s.lastMatchAt = &t
	}
	s.totalWins = totalWins
	s.totalLosses = totalLosses
	s.playlists = newPlaylists
	s.matches = combinedMatches
	s.completedGUIDs = seenGUIDs
	if len(s.localPlayerIDs) == 0 && len(targetRec.LocalPlayerIDs) > 0 {
		s.localPlayerIDs = make([]string, len(targetRec.LocalPlayerIDs))
		copy(s.localPlayerIDs, targetRec.LocalPlayerIDs)
	}
	delete(s.dismissedContinueIDs, s.id)
	if hadCurrentMatches && oldSessionID != s.id {
		delete(s.archivedMemorySessions, oldSessionID)
	}

	updatedRec := s.buildSessionRecordLocked(nil)
	store := s.store
	s.mu.Unlock()

	if hadCurrentMatches && oldSessionID != targetRec.SessionID && store != nil {
		_ = store.DeleteSession(context.Background(), oldSessionID)
	}
	s.saveRecordToStore(updatedRec)
	return s.broadcastSummaryUpdate(), nil
}

func (s *SessionTracker) resolveContinuationTarget(targetSessionID, currentID string, cw time.Duration, now time.Time) (*storage.SessionRecord, error) {
	if targetSessionID != "" {
		rec, err := s.lookupSessionRecord(targetSessionID)
		if err != nil {
			return nil, err
		}
		if rec.TotalMatches <= 0 {
			return nil, ErrNoContinuableSession
		}
		return rec, nil
	}

	rec := s.findContinuableRecord(currentID, cw, now, true)
	if rec == nil {
		if (s.hasAnyPreviousSession(currentID) && cw <= 0) || s.hasExpiredPreviousSession(currentID, cw, now) {
			return nil, ErrContinueWindowExpired
		}
		return nil, ErrNoContinuableSession
	}
	if rec.TotalMatches <= 0 {
		return nil, ErrNoContinuableSession
	}
	return rec, nil
}

func appendUniqueMatchClones(dst []*SessionMatchDetail, seen map[string]struct{}, src []*SessionMatchDetail) []*SessionMatchDetail {
	for _, m := range src {
		if m == nil {
			continue
		}
		g := strings.TrimSpace(m.MatchGUID)
		if g == "" {
			continue
		}
		if _, exists := seen[g]; exists {
			continue
		}
		seen[g] = struct{}{}
		dst = append(dst, m.DeepClone())
	}
	return dst
}

func mergeChronologicalMatches(prevMatches, curMatches []*SessionMatchDetail) ([]*SessionMatchDetail, map[string]struct{}) {
	capHint := len(prevMatches) + len(curMatches)
	combined := make([]*SessionMatchDetail, 0, capHint)
	seenGUIDs := make(map[string]struct{}, capHint)
	combined = appendUniqueMatchClones(combined, seenGUIDs, prevMatches)
	combined = appendUniqueMatchClones(combined, seenGUIDs, curMatches)

	sort.SliceStable(combined, func(i, j int) bool {
		if !combined[i].EndedAt.Equal(combined[j].EndedAt) {
			return combined[i].EndedAt.Before(combined[j].EndedAt)
		}
		return combined[i].StartedAt.Before(combined[j].StartedAt)
	})
	return combined, seenGUIDs
}

func rebuildSessionTallies(
	combinedMatches []*SessionMatchDetail,
	prevPlaylists map[string]*PlaylistSessionStats,
	activeMatch *ActiveMatchRecord,
) (map[int]*PlaylistSessionStats, int, int, time.Time) {
	newPlaylists := make(map[int]*PlaylistSessionStats)
	for kStr, ps := range prevPlaylists {
		if ps == nil {
			continue
		}
		pid := ps.PlaylistID
		if pid == 0 {
			pid, _ = strconv.Atoi(kStr)
		}
		if pid != 0 && ps.InitialMMR > 0 {
			newPlaylists[pid] = &PlaylistSessionStats{
				PlaylistID:   pid,
				PlaylistName: resolvePlaylistName(pid, ps.PlaylistName),
				InitialMMR:   ps.InitialMMR,
				CurrentMMR:   ps.InitialMMR,
			}
		}
	}

	var totalWins, totalLosses int
	var latestEnd time.Time
	for _, m := range combinedMatches {
		if m.Result == "victory" {
			totalWins++
		} else if m.Result == "defeat" {
			totalLosses++
		}
		if m.EndedAt.After(latestEnd) {
			latestEnd = m.EndedAt
		}
		if m.PlaylistID == 0 {
			continue
		}
		st, ok := newPlaylists[m.PlaylistID]
		if !ok {
			st = &PlaylistSessionStats{
				PlaylistID:   m.PlaylistID,
				PlaylistName: resolvePlaylistName(m.PlaylistID, m.PlaylistName),
				InitialMMR:   m.StartingMMR,
				CurrentMMR:   m.StartingMMR,
			}
			newPlaylists[m.PlaylistID] = st
		} else {
			if m.StartingMMR == 0 && st.CurrentMMR > 0 {
				m.StartingMMR = st.CurrentMMR
			}
			if st.InitialMMR == 0 && m.StartingMMR > 0 {
				st.InitialMMR = m.StartingMMR
			}
		}
		if m.StartingMMR > 0 && m.EndingMMR > 0 {
			m.MMRChange = domain.ComputeMMRDelta(m.StartingMMR, m.EndingMMR)
		}
		effMMR := m.EndingMMR
		if effMMR <= 0 {
			effMMR = m.StartingMMR
		}
		recordPlaylistOutcomeLocked(st, m.Result, effMMR)
	}

	if activeMatch != nil && activeMatch.PlaylistID != 0 {
		pid := activeMatch.PlaylistID
		st := ensurePlaylistStatsLocked(newPlaylists, pid, "", activeMatch.StartingMMR, activeMatch.StartingMMR)
		if activeMatch.StartingMMR > 0 {
			applyPlaylistCurrentMMRLocked(st, activeMatch.StartingMMR)
		}
	}
	return newPlaylists, totalWins, totalLosses, latestEnd
}

func (s *SessionTracker) collectCandidateRecords(excludeSessionID string) []*storage.SessionRecord {
	s.mu.RLock()
	store := s.store
	memMap := make(map[string]*storage.SessionRecord, len(s.archivedMemorySessions))
	for k, v := range s.archivedMemorySessions {
		if k != excludeSessionID && v != nil {
			memMap[k] = v
		}
	}
	s.mu.RUnlock()

	if store != nil {
		if recs, _, err := store.ListSessions(context.Background(), 25, 0); err == nil {
			for _, r := range recs {
				if r != nil && r.SessionID != excludeSessionID {
					memMap[r.SessionID] = r
				}
			}
		}
	}

	candidates := make([]*storage.SessionRecord, 0, len(memMap))
	for _, r := range memMap {
		if r.TotalMatches > 0 {
			candidates = append(candidates, r)
		}
	}
	sortSessionRecordsDesc(candidates)
	return candidates
}

func (s *SessionTracker) findContinuableRecord(currentSessionID string, cw time.Duration, now time.Time, ignoreDismissed bool) *storage.SessionRecord {
	if cw <= 0 {
		return nil
	}
	candidates := s.collectCandidateRecords(currentSessionID)

	s.mu.RLock()
	dismissed := make(map[string]bool, len(s.dismissedContinueIDs))
	for k, v := range s.dismissedContinueIDs {
		dismissed[k] = v
	}
	var currentFirstMatch time.Time
	if len(s.matches) > 0 {
		currentFirstMatch = s.matches[0].StartedAt
	}
	s.mu.RUnlock()

	for _, rec := range candidates {
		if !ignoreDismissed && (dismissed[rec.SessionID] || rec.Dismissed) {
			continue
		}
		if isWithinContinueWindow(recordRefTime(rec), now, currentFirstMatch, cw) {
			return rec
		}
	}
	return nil
}

func (s *SessionTracker) hasAnyPreviousSession(currentSessionID string) bool {
	return len(s.collectCandidateRecords(currentSessionID)) > 0
}

func (s *SessionTracker) hasExpiredPreviousSession(currentSessionID string, cw time.Duration, now time.Time) bool {
	candidates := s.collectCandidateRecords(currentSessionID)
	s.mu.RLock()
	var currentFirstMatch time.Time
	if len(s.matches) > 0 {
		currentFirstMatch = s.matches[0].StartedAt
	}
	s.mu.RUnlock()

	for _, rec := range candidates {
		if !isWithinContinueWindow(recordRefTime(rec), now, currentFirstMatch, cw) {
			return true
		}
	}
	return false
}
