package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteAndJSONStore_ReplayOrgPersistence(t *testing.T) {
	ctx := context.Background()

	runSuite := func(t *testing.T, reopen func() (StateStore, error)) {
		t.Helper()

		type orgStore interface {
			StateStore
			GetDailyCounter(ctx context.Context, counterType, dateKey string) (int, error)
			AllocateMatchReplayNaming(ctx context.Context, matchGUID, dateKey, gameMode, result string, minNextNum int) (*ReplayNamingAssignment, error)
			UpdateMatchReplayFile(ctx context.Context, matchGUID, localPath, replayName, gameMode string) error
			UpdateMatchGrouping(ctx context.Context, matchGUID, sessionID, gameMode, ballchasingGroupID string) error
			GetOrAllocateSessionSequence(ctx context.Context, sessionID, dateKey string) (*SessionSequenceRecord, error)
			GetBallchasingGroup(ctx context.Context, sessionID, gameMode string) (*BallchasingGroupRecord, error)
			SaveBallchasingGroup(ctx context.Context, rec *BallchasingGroupRecord) error
			ListBallchasingGroups(ctx context.Context) ([]*BallchasingGroupRecord, error)
			FindSessionForMatch(ctx context.Context, matchGUID string, matchTime time.Time) (*ResolvedMatchSession, error)
		}

		raw, err := reopen()
		if err != nil {
			t.Fatalf("open store: %v", err)
		}
		st := raw.(orgStore)

		if err := st.UpsertDiscoveredMatches(ctx, []*MatchRecord{
			{MatchGUID: "m-1", RecordStartTimestamp: 1795200000, Playlist: 11, ReplayURL: "https://cdn/m1.replay"},
			{MatchGUID: "m-2", RecordStartTimestamp: 1795200600, Playlist: 13, ReplayURL: "https://cdn/m2.replay"},
			{MatchGUID: "m-next-day", RecordStartTimestamp: 1795290000, Playlist: 27, ReplayURL: "https://cdn/m3.replay"},
		}); err != nil {
			t.Fatalf("UpsertDiscoveredMatches: %v", err)
		}

		// Allocate daily autonumbers
		a1, err := st.AllocateMatchReplayNaming(ctx, "m-1", "2026_11_21", "2v2", "WIN", 0)
		if err != nil {
			t.Fatalf("AllocateMatchReplayNaming m-1: %v", err)
		}
		if a1.DailyGameNum != 1 || a1.ReplayName != "2026_11_21_001_2v2_WIN.replay" || a1.Reused {
			t.Fatalf("unexpected assignment a1: %+v", a1)
		}

		// Idempotent re-allocation for same match must NOT burn counter
		a1Again, err := st.AllocateMatchReplayNaming(ctx, "m-1", "2026_11_21", "2v2", "WIN", 0)
		if err != nil {
			t.Fatalf("AllocateMatchReplayNaming m-1 retry: %v", err)
		}
		if !a1Again.Reused || a1Again.DailyGameNum != 1 || a1Again.ReplayName != "2026_11_21_001_2v2_WIN.replay" {
			t.Fatalf("expected reused #1, got %+v", a1Again)
		}

		a2, err := st.AllocateMatchReplayNaming(ctx, "m-2", "2026_11_21", "3v3", "LOSS", 0)
		if err != nil {
			t.Fatalf("AllocateMatchReplayNaming m-2: %v", err)
		}
		if a2.DailyGameNum != 2 || a2.ReplayName != "2026_11_21_002_3v3_LOSS.replay" {
			t.Fatalf("unexpected assignment a2: %+v", a2)
		}

		// Next day resets to 001
		a3, err := st.AllocateMatchReplayNaming(ctx, "m-next-day", "2026_11_22", "Extra", "WIN", 0)
		if err != nil {
			t.Fatalf("AllocateMatchReplayNaming m-next-day: %v", err)
		}
		if a3.DailyGameNum != 1 || a3.ReplayName != "2026_11_22_001_Extra_WIN.replay" {
			t.Fatalf("unexpected assignment a3: %+v", a3)
		}

		// Session sequences & Ballchasing groups
		s1, err := st.GetOrAllocateSessionSequence(ctx, "sess-alpha", "2026_11_21")
		if err != nil || s1.SessionNum != 1 {
			t.Fatalf("expected sess-alpha num 1, got %+v (err=%v)", s1, err)
		}
		s1Repeat, err := st.GetOrAllocateSessionSequence(ctx, "sess-alpha", "2026_11_21")
		if err != nil || s1Repeat.SessionNum != 1 {
			t.Fatalf("expected sess-alpha repeat num 1, got %+v (err=%v)", s1Repeat, err)
		}
		s2, err := st.GetOrAllocateSessionSequence(ctx, "sess-beta", "2026_11_21")
		if err != nil || s2.SessionNum != 2 {
			t.Fatalf("expected sess-beta num 2, got %+v (err=%v)", s2, err)
		}
		sNext, err := st.GetOrAllocateSessionSequence(ctx, "sess-gamma", "2026_11_22")
		if err != nil || sNext.SessionNum != 1 {
			t.Fatalf("expected sess-gamma num 1 on new day, got %+v (err=%v)", sNext, err)
		}

		if _, err := st.GetBallchasingGroup(ctx, "sess-alpha", "2v2"); !errors.Is(err, ErrGroupNotFound) {
			t.Fatalf("expected ErrGroupNotFound before save, got %v", err)
		}
		if err := st.SaveBallchasingGroup(ctx, &BallchasingGroupRecord{
			SessionID:           "sess-alpha",
			GameMode:            "2v2",
			DateKey:             "2026_11_21",
			SessionNum:          1,
			GroupName:           "2026_11_21_1_2v2",
			BallchasingGroupID:  "grp-abc-123",
			BallchasingGroupURL: "https://ballchasing.com/group/grp-abc-123",
		}); err != nil {
			t.Fatalf("SaveBallchasingGroup: %v", err)
		}

		if err := st.UpdateMatchGrouping(ctx, "m-1", "sess-alpha", "2v2", "grp-abc-123"); err != nil {
			t.Fatalf("UpdateMatchGrouping: %v", err)
		}

		// Persist a session containing m-1 and verify FindSessionForMatch (exact + time window)
		sessStart := time.Unix(1795199900, 0).UTC()
		sessLast := time.Unix(1795200900, 0).UTC()
		if err := st.SaveSession(ctx, &SessionRecord{
			SessionID:    "sess-alpha",
			StartedAt:    sessStart,
			LastMatchAt:  &sessLast,
			TotalMatches: 1,
			TotalWins:    1,
			MatchesJSON:  `[{"match_guid":"M-1","playlist_id":11,"result":"victory"}]`,
		}); err != nil {
			t.Fatalf("SaveSession: %v", err)
		}

		_ = st.Close()

		// Reopen to verify durability across restart
		raw2, err := reopen()
		if err != nil {
			t.Fatalf("reopen store: %v", err)
		}
		defer raw2.Close()
		st2 := raw2.(orgStore)

		if c, err := st2.GetDailyCounter(ctx, CounterTypeGame, "2026_11_21"); err != nil || c != 2 {
			t.Fatalf("expected persisted game counter=2, got %d (err=%v)", c, err)
		}
		if c, err := st2.GetDailyCounter(ctx, CounterTypeSession, "2026_11_21"); err != nil || c != 2 {
			t.Fatalf("expected persisted session counter=2, got %d (err=%v)", c, err)
		}

		m1, err := st2.GetMatch(ctx, "m-1")
		if err != nil {
			t.Fatalf("GetMatch m-1 after reopen: %v", err)
		}
		if m1.ReplayName != "2026_11_21_001_2v2_WIN.replay" || m1.DailyGameNum != 1 || m1.GameMode != "2v2" || m1.SessionID != "sess-alpha" || m1.BallchasingGroupID != "grp-abc-123" {
			t.Fatalf("unexpected m-1 after reopen: %+v", m1)
		}

		grp, err := st2.GetBallchasingGroup(ctx, "sess-alpha", "2v2")
		if err != nil {
			t.Fatalf("GetBallchasingGroup after reopen: %v", err)
		}
		if grp.GroupName != "2026_11_21_1_2v2" || grp.BallchasingGroupID != "grp-abc-123" {
			t.Fatalf("unexpected group after reopen: %+v", grp)
		}

		resolvedExact, err := st2.FindSessionForMatch(ctx, "m-1", time.Unix(1795200000, 0).UTC())
		if err != nil || resolvedExact == nil || !resolvedExact.ExactMatch || resolvedExact.SessionID != "sess-alpha" || resolvedExact.MatchResult != "WIN" {
			t.Fatalf("expected exact session match for m-1, got %+v (err=%v)", resolvedExact, err)
		}

		resolvedWindow, err := st2.FindSessionForMatch(ctx, "m-2", time.Unix(1795200600, 0).UTC())
		if err != nil || resolvedWindow == nil || resolvedWindow.ExactMatch || resolvedWindow.SessionID != "sess-alpha" {
			t.Fatalf("expected time-window session match for m-2, got %+v (err=%v)", resolvedWindow, err)
		}

		resolvedNone, err := st2.FindSessionForMatch(ctx, "m-next-day", time.Unix(1795290000, 0).UTC())
		if err != nil || resolvedNone != nil {
			t.Fatalf("expected nil session match for m-next-day, got %+v (err=%v)", resolvedNone, err)
		}
	}

	t.Run("SQLiteStore", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "state.db")
		runSuite(t, func() (StateStore, error) { return NewSQLiteStore(dbPath) })
	})

	t.Run("JSONStore", func(t *testing.T) {
		jsonPath := filepath.Join(t.TempDir(), "state.json")
		runSuite(t, func() (StateStore, error) { return NewJSONStore(jsonPath) })
	})
}
