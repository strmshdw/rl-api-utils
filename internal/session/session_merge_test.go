package session

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/storage"
)

func TestSessionTracker_MultiPlaylistMultiMatchMergeAndCarryOverWindow(t *testing.T) {
	store, err := storage.NewJSONStore(t.TempDir() + "/merge.json")
	if err != nil {
		t.Fatalf("failed to create json store: %v", err)
	}
	defer store.Close()

	currTime := time.Date(2026, 10, 17, 16, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clockFn := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return currTime
	}
	advance := func(d time.Duration) {
		mu.Lock()
		currTime = currTime.Add(d)
		mu.Unlock()
	}

	t1 := NewSessionTracker(
		WithStateStore(store),
		WithContinueWindow(1*time.Hour),
		WithClock(clockFn),
	)

	// Session 1: 1 match in Playlist 11 (1000 -> 1010) and 1 match in Playlist 13 (1100 -> 1090)
	s1m1 := createMockMatch("s1-m1", 11, "victory", 1000.0, 3, 1)
	t1.RecordActiveMatch(s1m1)
	advance(5 * time.Minute)
	s1m1End := createMockMatch("s1-m1", 11, "victory", 1010.0, 3, 1)
	s1m1End.MatchEnded = true
	t1.ConcludeMatch(s1m1End)

	advance(2 * time.Minute)
	s1m2 := createMockMatch("s1-m2", 13, "defeat", 1100.0, 1, 2)
	t1.RecordActiveMatch(s1m2)
	advance(5 * time.Minute)
	s1m2End := createMockMatch("s1-m2", 13, "defeat", 1090.0, 1, 2)
	s1m2End.MatchEnded = true
	t1.ConcludeMatch(s1m2End)

	session1ID := t1.GetSessionSummary().SessionID

	// Restart 45 minutes after Session 1's last match (within 1h window)
	advance(45 * time.Minute)
	t2 := NewSessionTracker(
		WithStateStore(store),
		WithContinueWindow(1*time.Hour),
		WithClock(clockFn),
	)

	// Play Match 1 in Session 2 (Playlist 11) where StartingMMR was 0 (rank fetch arrived only at match end = 1020.0)
	s2m1 := createMockMatch("s2-m1", 11, "victory", 0.0, 2, 0)
	t2.RecordActiveMatch(s2m1)
	advance(6 * time.Minute)
	s2m1End := createMockMatch("s2-m1", 11, "victory", 1020.0, 2, 0)
	s2m1End.MatchEnded = true
	t2.ConcludeMatch(s2m1End)

	// Play Match 2 in Session 2 (Playlist 13: 1090 -> 1105) + advance past 1h total since Session 1 ended
	// (45m + 6m + 15m = 66m > 60m since Session 1's LastMatchAt, but Session 2's first match started at +45m <= 60m!)
	advance(9 * time.Minute)
	s2m2 := createMockMatch("s2-m2", 13, "victory", 1090.0, 4, 2)
	t2.RecordActiveMatch(s2m2)
	advance(6 * time.Minute)
	s2m2End := createMockMatch("s2-m2", 13, "victory", 1105.0, 4, 2)
	s2m2End.MatchEnded = true
	t2.ConcludeMatch(s2m2End)

	// Verify CanContinue is true in both GetSessionSummary().ContinuableSession and ListSessions
	preMerge := t2.GetSessionSummary()
	if preMerge.ContinuableSession == nil || preMerge.ContinuableSession.SessionID != session1ID {
		t.Fatalf("expected ContinuableSession %s via carry-over window, got %+v", session1ID, preMerge.ContinuableSession)
	}
	if !preMerge.ContinuableSession.CanContinue {
		t.Fatalf("expected ContinuableSession.CanContinue=true via carry-over window")
	}

	listed, _, err := t2.ListSessions(context.Background(), 10, 0)
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	var foundS1 *SessionResponse
	for _, item := range listed {
		if item.SessionID == session1ID {
			foundS1 = item
			break
		}
	}
	if foundS1 == nil || !foundS1.CanContinue {
		t.Fatalf("expected Session 1 in ListSessions to have CanContinue=true via carry-over window, got %+v", foundS1)
	}

	// Merge Session 2 (2 matches across playlists 11 & 13) into Session 1
	merged, err := t2.ContinueSession("")
	if err != nil {
		t.Fatalf("ContinueSession failed: %v", err)
	}
	if merged.TotalMatches != 4 || merged.TotalWins != 3 || merged.TotalLosses != 1 {
		t.Fatalf("expected 4 matches (3W-1L), got %d (%dW-%dL)", merged.TotalMatches, merged.TotalWins, merged.TotalLosses)
	}
	if merged.Playlists["11"].InitialMMR != 1000.0 || merged.Playlists["11"].CurrentMMR != 1020.0 || merged.Playlists["11"].MMRDelta != 20.0 {
		t.Fatalf("unexpected Playlist 11 stats after merge: %+v", merged.Playlists["11"])
	}
	if merged.Playlists["13"].InitialMMR != 1100.0 || merged.Playlists["13"].CurrentMMR != 1105.0 || merged.Playlists["13"].MMRDelta != 5.0 {
		t.Fatalf("unexpected Playlist 13 stats after merge: %+v", merged.Playlists["13"])
	}

	// Verify s2-m1 had its StartingMMR (1010.0) and MMRChange (+10.0) backfilled from Session 1's CurrentMMR
	var mergedS2M1 *SessionMatchDetail
	for _, m := range merged.Matches {
		if m.MatchGUID == "s2-m1" {
			mergedS2M1 = m
			break
		}
	}
	if mergedS2M1 == nil {
		t.Fatalf("expected s2-m1 in merged matches")
	}
	if mergedS2M1.StartingMMR != 1010.0 || mergedS2M1.MMRChange != 10.0 {
		t.Fatalf("expected s2-m1 StartingMMR=1010.0 and MMRChange=10.0 after backfill, got start=%f change=%f",
			mergedS2M1.StartingMMR, mergedS2M1.MMRChange)
	}
}

func TestSessionTracker_SetStateStoreFlushesInMemorySessions(t *testing.T) {
	st := NewSessionTracker()
	m1 := createMockMatch("flush-m1", 11, "victory", 1000.0, 2, 1)
	st.RecordActiveMatch(m1)
	m1.MatchEnded = true
	st.ConcludeMatch(m1)
	firstID := st.GetSessionSummary().SessionID

	st.Reset()

	m2 := createMockMatch("flush-m2", 13, "defeat", 1100.0, 1, 3)
	st.RecordActiveMatch(m2)
	m2.MatchEnded = true
	st.ConcludeMatch(m2)
	secondID := st.GetSessionSummary().SessionID

	store, err := storage.NewSQLiteStore(t.TempDir() + "/late_attach.db")
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	st.SetStateStore(store)

	sessions, total, err := st.ListSessions(context.Background(), 10, 0)
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if total != 2 || len(sessions) != 2 {
		t.Fatalf("expected 2 flushed sessions in store after SetStateStore, got total=%d len=%d", total, len(sessions))
	}
	seen := map[string]bool{sessions[0].SessionID: true, sessions[1].SessionID: true}
	if !seen[firstID] || !seen[secondID] {
		t.Fatalf("expected both %s and %s in store, got %v", firstID, secondID, seen)
	}
}
