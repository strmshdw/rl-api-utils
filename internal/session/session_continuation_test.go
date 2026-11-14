package session

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/storage"
)

func TestSessionTracker_PersistenceAndContinueAcrossRestart(t *testing.T) {
	backends := []struct {
		name     string
		newStore func(t *testing.T) storage.StateStore
	}{
		{
			name: "SQLiteStore",
			newStore: func(t *testing.T) storage.StateStore {
				st, err := storage.NewSQLiteStore(t.TempDir() + "/sessions.db")
				if err != nil {
					t.Fatalf("failed to create sqlite store: %v", err)
				}
				t.Cleanup(func() { _ = st.Close() })
				return st
			},
		},
		{
			name: "JSONStore",
			newStore: func(t *testing.T) storage.StateStore {
				st, err := storage.NewJSONStore(t.TempDir() + "/sessions.json")
				if err != nil {
					t.Fatalf("failed to create json store: %v", err)
				}
				t.Cleanup(func() { _ = st.Close() })
				return st
			},
		},
	}

	for _, b := range backends {
		t.Run(b.name, func(t *testing.T) {
			store := b.newStore(t)
			baseTime := time.Date(2026, 10, 17, 18, 0, 0, 0, time.UTC)
			var mu sync.Mutex
			currTime := baseTime
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

			// 1. Tracker 1 plays 2 matches in playlist 11
			t1 := NewSessionTracker(
				WithStateStore(store),
				WithContinueWindow(2*time.Hour),
				WithClock(clockFn),
			)

			m1 := createMockMatch("cont-m1", 11, "victory", 1200.0, 3, 1)
			t1.RecordActiveMatch(m1)
			advance(5 * time.Minute)
			m1End := createMockMatch("cont-m1", 11, "victory", 1209.5, 3, 1)
			m1End.MatchEnded = true
			t1.ConcludeMatch(m1End)

			advance(2 * time.Minute)
			m2 := createMockMatch("cont-m2", 11, "defeat", 1209.5, 1, 2)
			t1.RecordActiveMatch(m2)
			advance(5 * time.Minute)
			m2End := createMockMatch("cont-m2", 11, "defeat", 1200.5, 1, 2)
			m2End.MatchEnded = true
			t1.ConcludeMatch(m2End)

			s1Summary := t1.GetSessionSummary()
			session1ID := s1Summary.SessionID
			if s1Summary.TotalMatches != 2 || s1Summary.TotalWins != 1 || s1Summary.TotalLosses != 1 {
				t.Fatalf("unexpected s1 summary: %+v", s1Summary)
			}

			// Verify persisted in store
			persisted1, err := store.GetSession(context.Background(), session1ID)
			if err != nil {
				t.Fatalf("expected session 1 persisted in store: %v", err)
			}
			if persisted1.TotalMatches != 2 || persisted1.TotalWins != 1 {
				t.Fatalf("unexpected persisted session 1: %+v", persisted1)
			}

			// 2. Simulate daemon restart 30 minutes later (within 2h continue window)
			advance(30 * time.Minute)
			t2 := NewSessionTracker(
				WithStateStore(store),
				WithContinueWindow(2*time.Hour),
				WithClock(clockFn),
			)

			s2Initial := t2.GetSessionSummary()
			if s2Initial.ContinuableSession == nil {
				t.Fatalf("expected ContinuableSession to be detected on t2 within 2h window")
			}
			if s2Initial.ContinuableSession.SessionID != session1ID {
				t.Fatalf("expected ContinuableSession ID %s, got %s", session1ID, s2Initial.ContinuableSession.SessionID)
			}
			if s2Initial.ContinuableSession.TotalMatches != 2 {
				t.Fatalf("expected ContinuableSession TotalMatches=2, got %d", s2Initial.ContinuableSession.TotalMatches)
			}

			// 3. Play 1 match in t2 BEFORE clicking Continue, then call ContinueSession to verify merge
			m3 := createMockMatch("cont-m3", 11, "victory", 1200.5, 4, 0)
			t2.RecordActiveMatch(m3)
			advance(5 * time.Minute)
			m3End := createMockMatch("cont-m3", 11, "victory", 1210.0, 4, 0)
			m3End.MatchEnded = true
			t2.ConcludeMatch(m3End)

			tempSession2ID := t2.GetSessionSummary().SessionID
			merged, err := t2.ContinueSession("")
			if err != nil {
				t.Fatalf("ContinueSession failed: %v", err)
			}
			if merged.SessionID != session1ID {
				t.Fatalf("expected merged SessionID %s, got %s", session1ID, merged.SessionID)
			}
			if merged.TotalMatches != 3 || merged.TotalWins != 2 || merged.TotalLosses != 1 {
				t.Fatalf("expected merged 3 matches (2W-1L), got %d (%dW-%dL)", merged.TotalMatches, merged.TotalWins, merged.TotalLosses)
			}
			p11 := merged.Playlists["11"]
			if p11 == nil {
				t.Fatalf("expected playlist 11 stats in merged session")
			}
			if p11.InitialMMR != 1200.0 || p11.CurrentMMR != 1210.0 || p11.MMRDelta != 10.0 {
				t.Fatalf("expected InitialMMR=1200, CurrentMMR=1210, MMRDelta=10, got %+v", p11)
			}

			// Verify temporary session 2 was cleaned up from store
			if _, err := store.GetSession(context.Background(), tempSession2ID); err == nil {
				t.Errorf("expected temporary session %s to be deleted after merge", tempSession2ID)
			}

			// 4. Advance 3 hours (beyond 2h continue window) and start Tracker 3
			advance(3 * time.Hour)
			t3 := NewSessionTracker(
				WithStateStore(store),
				WithContinueWindow(2*time.Hour),
				WithClock(clockFn),
			)
			s3Summary := t3.GetSessionSummary()
			if s3Summary.ContinuableSession != nil {
				t.Fatalf("expected no ContinuableSession after 3h (> 2h window), got %+v", s3Summary.ContinuableSession)
			}
			if _, err := t3.ContinueSession(session1ID); err != ErrContinueWindowExpired {
				t.Fatalf("expected ErrContinueWindowExpired when continuing after 3h, got %v", err)
			}

			// Historical session should still be retrievable via ListSessions & GetHistoricalSession
			histList, total, err := t3.ListSessions(context.Background(), 10, 0)
			if err != nil || total != 1 || len(histList) != 1 {
				t.Fatalf("expected 1 historical session in ListSessions, got total=%d, err=%v", total, err)
			}
			if histList[0].CanContinue {
				t.Errorf("expected CanContinue=false on expired historical session")
			}
		})
	}
}

func TestSessionTracker_AutoRotateOnIdleAndGameDisconnect(t *testing.T) {
	store, err := storage.NewSQLiteStore(t.TempDir() + "/rotate.db")
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	currTime := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
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

	st := NewSessionTracker(
		WithStateStore(store),
		WithContinueWindow(1*time.Hour),
		WithClock(clockFn),
	)

	// Play 1 match
	m1 := createMockMatch("rot-m1", 13, "victory", 1300.0, 2, 1)
	st.RecordActiveMatch(m1)

	// Mid-match disconnect should NOT rotate the session while activeMatch != nil
	st.OnGameDisconnected()
	if st.GetSessionSummary().ActiveMatch == nil {
		t.Fatalf("expected ActiveMatch to remain intact on mid-match disconnect")
	}

	advance(5 * time.Minute)
	m1.MatchEnded = true
	st.ConcludeMatch(m1)
	firstSessionID := st.GetSessionSummary().SessionID

	// Now disconnect between matches: should archive firstSessionID and rotate to a new session with ContinuableSession
	advance(10 * time.Minute)
	st.OnGameDisconnected()

	afterDisc := st.GetSessionSummary()
	if afterDisc.SessionID == firstSessionID {
		t.Fatalf("expected new session ID after game disconnect")
	}
	if afterDisc.TotalMatches != 0 {
		t.Fatalf("expected 0 matches in new session after disconnect, got %d", afterDisc.TotalMatches)
	}
	if afterDisc.ContinuableSession == nil || afterDisc.ContinuableSession.SessionID != firstSessionID {
		t.Fatalf("expected ContinuableSession pointing to %s, got %+v", firstSessionID, afterDisc.ContinuableSession)
	}

	// Dismiss continuable session
	afterDismiss := st.DismissContinuableSession("")
	if afterDismiss.ContinuableSession != nil {
		t.Fatalf("expected ContinuableSession to be nil after dismiss")
	}

	// Simulate daemon restart: dismissed state must persist in StateStore
	advance(5 * time.Minute)
	stRestarted := NewSessionTracker(
		WithStateStore(store),
		WithContinueWindow(1*time.Hour),
		WithClock(clockFn),
	)
	restartedSummary := stRestarted.GetSessionSummary()
	if restartedSummary.ContinuableSession != nil {
		t.Fatalf("expected dismissed ContinuableSession to remain nil after daemon restart, got %+v", restartedSummary.ContinuableSession)
	}

	// Explicit ContinueSession by ID from Session History should still succeed while within window
	continued, err := stRestarted.ContinueSession(firstSessionID)
	if err != nil {
		t.Fatalf("expected explicit ContinueSession(%s) to succeed after dismiss, got %v", firstSessionID, err)
	}
	if continued.SessionID != firstSessionID || continued.TotalMatches != 1 {
		t.Fatalf("unexpected continued session: %+v", continued)
	}
}

func TestSessionTracker_AutoRotateAfterAbandonedMatchAndSSEBroadcast(t *testing.T) {
	store, err := storage.NewSQLiteStore(t.TempDir() + "/abandoned.db")
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	currTime := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
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

	st := NewSessionTracker(
		WithStateStore(store),
		WithContinueWindow(1*time.Hour),
		WithClock(clockFn),
	)

	// 1. Conclude Match 1 in Playlist 11
	m1 := createMockMatch("m1", 11, "victory", 1200.0, 3, 1)
	st.RecordActiveMatch(m1)
	advance(5 * time.Minute)
	m1.MatchEnded = true
	st.ConcludeMatch(m1)
	s1ID := st.GetSessionSummary().SessionID

	// 2. Enter Match 2 in Playlist 13 and abandon it mid-game (no ConcludeMatch)
	advance(2 * time.Minute)
	m2Abandoned := createMockMatch("m2-abandoned", 13, "", 1350.0, 0, 1)
	st.RecordActiveMatch(m2Abandoned)

	// 3. Subscribe to SSE and advance 2 hours (> 1h continueWindow)
	ch, cancel := st.Subscribe()
	defer cancel()
	advance(2 * time.Hour)

	// 4. Start Match 3 in Playlist 11 -> must auto-rotate despite m2-abandoned having left activeMatch != nil,
	// must NOT seed m2-abandoned's Playlist 13 into the new session, and must broadcast EventSessionUpdate!
	m3 := createMockMatch("m3", 11, "", 1210.0, 1, 0)
	st.RecordActiveMatch(m3)

	afterM3 := st.GetSessionSummary()
	if afterM3.SessionID == s1ID {
		t.Fatalf("expected auto-rotation to a new session after 2h idle following abandoned match")
	}
	if afterM3.TotalMatches != 0 || afterM3.TotalWins != 0 {
		t.Fatalf("expected new session to have 0 completed matches, got %d", afterM3.TotalMatches)
	}
	if _, hasP13 := afterM3.Playlists["13"]; hasP13 {
		t.Fatalf("expected abandoned match playlist 13 not to leak into new rotated session")
	}

	var gotSessionUpdate, gotMatchUpdate bool
	for range 2 {
		select {
		case ev := <-ch:
			if ev.Event == EventSessionUpdate {
				gotSessionUpdate = true
			}
			if ev.Event == EventMatchUpdate {
				gotMatchUpdate = true
			}
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("timed out waiting for SSE events on auto-rotate (gotSessionUpdate=%v, gotMatchUpdate=%v)", gotSessionUpdate, gotMatchUpdate)
		}
	}
	if !gotSessionUpdate || !gotMatchUpdate {
		t.Fatalf("expected both EventSessionUpdate and EventMatchUpdate, got session=%v match=%v", gotSessionUpdate, gotMatchUpdate)
	}

	// 5. Conclude Match 3, then start Match 4 and quit Rocket League mid-Match-4 (OnGameDisconnected)
	advance(5 * time.Minute)
	m3.MatchEnded = true
	m3.Result = "victory"
	st.ConcludeMatch(m3)
	s2ID := st.GetSessionSummary().SessionID

	advance(1 * time.Minute)
	m4Abandoned := createMockMatch("m4-abandoned", 13, "", 1350.0, 0, 0)
	st.RecordActiveMatch(m4Abandoned)

	advance(2 * time.Minute)
	st.OnGameDisconnected()

	afterQuit := st.GetSessionSummary()
	if afterQuit.SessionID == s2ID {
		t.Fatalf("expected OnGameDisconnected to rotate completed session even when a subsequent match was abandoned on exit")
	}
	if afterQuit.ActiveMatch != nil {
		t.Fatalf("expected ActiveMatch to be nil after OnGameDisconnected rotation")
	}
	if afterQuit.ContinuableSession == nil || afterQuit.ContinuableSession.SessionID != s2ID {
		t.Fatalf("expected ContinuableSession to offer %s, got %+v", s2ID, afterQuit.ContinuableSession)
	}
}
