package session_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/contracts"
	"github.com/dank/rl-api-utils/internal/domain"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/session"
	"github.com/dank/rl-api-utils/internal/storage"
)

type countingStateStore struct {
	storage.StateStore
	getMatchCalls            atomic.Int64
	getMatchPlayersCalls     atomic.Int64
	listHistoricalMatchCalls atomic.Int64
}

func (c *countingStateStore) GetMatch(ctx context.Context, matchGUID string) (*storage.MatchRecord, error) {
	c.getMatchCalls.Add(1)
	return c.StateStore.GetMatch(ctx, matchGUID)
}

func (c *countingStateStore) GetMatchPlayers(ctx context.Context, matchGUID string) ([]*storage.MatchPlayerRecord, error) {
	c.getMatchPlayersCalls.Add(1)
	return c.StateStore.GetMatchPlayers(ctx, matchGUID)
}

func (c *countingStateStore) ListHistoricalMatches(ctx context.Context, filter storage.MatchFilter) ([]*storage.HistoricalMatchRecord, int, error) {
	c.listHistoricalMatchCalls.Add(1)
	return c.StateStore.ListHistoricalMatches(ctx, filter)
}

func (c *countingStateStore) resetCounters() {
	c.getMatchCalls.Store(0)
	c.getMatchPlayersCalls.Store(0)
	c.listHistoricalMatchCalls.Store(0)
}

func TestTelemetry_BatchHydrationEliminatesNPlusOne(t *testing.T) {
	ctx := context.Background()
	rawStore, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "session_telemetry.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer func() { _ = rawStore.Close() }()

	store := &countingStateStore{StateStore: rawStore}
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	now := base

	tracker := session.NewSessionTracker(
		session.WithStateStore(store),
		session.WithNowFunc(func() time.Time { return now }),
	)
	localID := "Steam|76561198000000001|0"
	tracker.SetLocalPlayerIDs([]string{localID})

	win0 := 0
	for i := 0; i < 6; i++ {
		guid := fmt.Sprintf("batch-guid-%d", i)
		ts := base.Add(time.Duration(i) * time.Minute)
		now = ts
		if err := store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
			MatchGUID:   guid,
			MapName:     "DFH Stadium",
			Playlist:    11,
			BlueScore:   3,
			OrangeScore: 1,
			WinnerTeam:  &win0,
			Date:        ts.Format(time.RFC3339),
			Players: []storage.ReplayPlayerStats{
				{
					Name: "Local", Platform: "Steam", OnlineID: "76561198000000001", Team: 0, IsLocal: true,
					Score: 500, Goals: 2, Assists: 1, Saves: 2, Shots: 5, Demos: 1,
					Boost:       &domain.BoostStats{BPM: 420, AvgAmount: 48},
					Movement:    &domain.MovementStats{PercentSupersonic: 18, PercentGround: 55},
					Positioning: &domain.PositioningStats{PercentBehindBall: 66},
				},
				{
					Name: "Opponent", Platform: "Epic", OnlineID: "opp_1", Team: 1,
					Score: 300, Goals: 1, Assists: 0, Saves: 1, Shots: 3, Demos: 0,
					Boost:       &domain.BoostStats{BPM: 360, AvgAmount: 42},
					Movement:    &domain.MovementStats{PercentSupersonic: 12, PercentGround: 60},
					Positioning: &domain.PositioningStats{PercentBehindBall: 60},
				},
			},
		}); err != nil {
			t.Fatalf("SyncReplayStats(%s) failed: %v", guid, err)
		}

		tracker.OnMatchStateUpdated(&playertrack.CurrentMatchResponse{
			MatchGUID:   guid,
			PlaylistID:  11,
			ActiveMatch: true,
			LocalTeam:   &win0,
			LocalPlayer: &playertrack.LobbyPlayer{
				PlayerID: localID, Platform: "Steam", Name: "Local", TeamNum: 0, IsLocal: true,
				CurrentRank: &playertrack.PlayerPlaylistRank{MMR: 1200 + float64(i*10)},
			},
		})
		now = ts.Add(5 * time.Minute)
		tracker.OnMatchEnded(&playertrack.CurrentMatchResponse{
			MatchGUID:  guid,
			PlaylistID: 11,
			MatchEnded: true,
			LocalTeam:  &win0,
			WinnerTeam: &win0,
			Result:     "victory",
			LocalPlayer: &playertrack.LobbyPlayer{
				PlayerID: localID, Platform: "Steam", Name: "Local", TeamNum: 0, IsLocal: true,
				CurrentRank: &playertrack.PlayerPlaylistRank{MMR: 1210 + float64(i*10)},
			},
		})
		if i == 2 {
			now = now.Add(time.Minute)
			tracker.Reset()
		}
	}

	store.resetCounters()
	summary := tracker.GetSessionSummary()
	if summary.ForStats == nil || summary.AgainstStats == nil {
		t.Fatalf("expected non-nil ForStats and AgainstStats")
	}
	if summary.ForStats.AvgBPM != 420 || summary.AgainstStats.AvgBPM != 360 {
		t.Fatalf("unexpected hydrated BPM: for=%v against=%v", summary.ForStats.AvgBPM, summary.AgainstStats.AvgBPM)
	}
	if store.getMatchCalls.Load() != 0 || store.getMatchPlayersCalls.Load() != 0 {
		t.Fatalf("expected zero per-match queries, got GetMatch=%d GetMatchPlayers=%d",
			store.getMatchCalls.Load(), store.getMatchPlayersCalls.Load())
	}
	if store.listHistoricalMatchCalls.Load() != 1 {
		t.Fatalf("expected 1 batched ListHistoricalMatches call for current session, got %d", store.listHistoricalMatchCalls.Load())
	}

	store.resetCounters()
	histSessions, total, err := tracker.ListHistoricalSessions(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ListHistoricalSessions failed: %v", err)
	}
	if total < 2 || len(histSessions) < 2 {
		t.Fatalf("expected >= 2 historical sessions, got total=%d len=%d", total, len(histSessions))
	}
	for _, hs := range histSessions {
		if hs.ForStats == nil || hs.ForStats.AvgBPM != 420 {
			t.Fatalf("historical session %s missing hydrated AvgBPM=420: %+v", hs.SessionID, hs.ForStats)
		}
	}
	if store.getMatchCalls.Load() != 0 || store.getMatchPlayersCalls.Load() != 0 {
		t.Fatalf("expected zero per-match queries in ListHistoricalSessions, got GetMatch=%d GetMatchPlayers=%d",
			store.getMatchCalls.Load(), store.getMatchPlayersCalls.Load())
	}
	if store.listHistoricalMatchCalls.Load() != 1 {
		t.Fatalf("expected single shared ListHistoricalMatches batch across all sessions, got %d", store.listHistoricalMatchCalls.Load())
	}
}

func TestTelemetry_AttachReplayTelemetry(t *testing.T) {
	tracker := session.NewTracker()
	if tracker.AttachReplayTelemetry("", nil) {
		t.Fatal("expected false for empty guid / nil telemetry")
	}

	ch, cancel := tracker.Subscribe()
	defer cancel()

	win0 := 0
	tracker.ConcludeMatch(&playertrack.CurrentMatchResponse{
		MatchGUID:  "attach-guid-1",
		PlaylistID: 13,
		MatchEnded: true,
		LocalTeam:  &win0,
		WinnerTeam: &win0,
		Result:     "victory",
	})

	ok := tracker.AttachReplayTelemetry("attach-guid-1", &storage.ReplayMatchStats{
		MatchGUID:  "attach-guid-1",
		WinnerTeam: &win0,
		Players: []storage.ReplayPlayerStats{
			{
				Name: "Local", Platform: "Steam", OnlineID: "12345", Team: 0, IsLocal: true,
				Score: 600, Goals: 3, Assists: 1, Saves: 2, Shots: 6,
				Boost: &domain.BoostStats{BPM: 444, AvgAmount: 51},
			},
			{
				Name: "Opp", Platform: "Epic", OnlineID: "999", Team: 1,
				Score: 220, Goals: 0, Assists: 0, Saves: 1, Shots: 2,
				Boost: &domain.BoostStats{BPM: 310, AvgAmount: 35},
			},
		},
	})
	if !ok {
		t.Fatal("expected AttachReplayTelemetry to return true")
	}

	summary := tracker.GetSessionSummary()
	if summary.ForStats == nil || summary.ForStats.AvgBPM != 444 {
		t.Fatalf("expected ForStats AvgBPM=444, got %+v", summary.ForStats)
	}
	if summary.AgainstStats == nil || summary.AgainstStats.AvgBPM != 310 {
		t.Fatalf("expected AgainstStats AvgBPM=310, got %+v", summary.AgainstStats)
	}
	if len(summary.Matches[0].Players) != 2 {
		t.Fatalf("expected match players hydrated from telemetry, got %d", len(summary.Matches[0].Players))
	}

	sawSessionUpdate := false
	for i := 0; i < 4; i++ {
		select {
		case ev := <-ch:
			if ev.Event == session.EventSessionUpdate {
				sawSessionUpdate = true
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !sawSessionUpdate {
		t.Fatal("expected EventSessionUpdate broadcast after AttachReplayTelemetry")
	}
}

func TestSessionTracker_ClockRestoreAndAliases(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "restore.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	t1 := session.NewTracker(session.WithStateStore(store), session.WithNowFunc(func() time.Time { return now }))
	ch, cancel := t1.Subscribe()
	defer cancel()

	t1.OnMatchStateUpdated(&playertrack.CurrentMatchResponse{MatchGUID: "clock-g1", PlaylistID: 11, ActiveMatch: true})
	t1.OnClockUpdated(&contracts.MatchClock{SecondsRemaining: 142, IsOvertime: false})

	var sawClock bool
	for i := 0; i < 3; i++ {
		select {
		case ev := <-ch:
			if ev.Event == session.EventClockUpdated {
				clk, ok := ev.Data.(contracts.MatchClock)
				if !ok || clk.SecondsRemaining != 142 || clk.IsOvertime {
					t.Fatalf("unexpected clock payload: %#v", ev.Data)
				}
				sawClock = true
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !sawClock {
		t.Fatal("expected EventClockUpdated event")
	}

	if snap := t1.GetSessionSummary(); snap.ActiveMatch == nil || snap.ActiveMatch.TimeRemaining != 142 {
		t.Fatalf("expected active match clock updated to 142, got %+v", snap.ActiveMatch)
	}

	win0 := 0
	t1.OnMatchEnded(&playertrack.CurrentMatchResponse{
		MatchGUID: "clock-g1", PlaylistID: 11, MatchEnded: true, LocalTeam: &win0, WinnerTeam: &win0, Result: "victory",
	})
	firstID := t1.GetSessionSummary().SessionID
	now = now.Add(10 * time.Minute)
	t1.Reset()

	t2 := session.NewSessionTracker(session.WithStateStore(store))
	t2.SetClock(func() time.Time { return now })
	if err := t2.RestoreFromStore(ctx); err != nil {
		t.Fatalf("RestoreFromStore failed: %v", err)
	}

	cont, err := t2.ContinuePreviousSession("")
	if err != nil {
		t.Fatalf("ContinuePreviousSession failed: %v", err)
	}
	if cont.SessionID != firstID || cont.TotalMatches != 1 {
		t.Fatalf("expected continued session %s with 1 match, got %s (%d)", firstID, cont.SessionID, cont.TotalMatches)
	}
}

func TestFilterSessionSummary_ScopingAndMath(t *testing.T) {
	if session.FilterSessionSummary(nil, "11", 0, 0, nil) != nil {
		t.Fatal("expected nil for nil summary")
	}

	t0 := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	summary := &session.SessionResponse{
		Matches: []*session.SessionMatchDetail{
			{MatchGUID: "m1", PlaylistID: 11, Result: "victory", MMRChange: 9.5, EndingMMR: 1209.5, StartedAt: t0, EndedAt: t0.Add(5 * time.Minute)},
			{MatchGUID: "m2", PlaylistID: 13, Result: "defeat", MMRChange: -8.2, EndingMMR: 1091.8, StartedAt: t0.Add(10 * time.Minute), EndedAt: t0.Add(15 * time.Minute)},
			{MatchGUID: "m3", PlaylistID: 11, Result: "defeat", MMRChange: -9.0, EndingMMR: 1200.5, StartedAt: t0.Add(20 * time.Minute), EndedAt: t0.Add(25 * time.Minute)},
		},
	}

	filtered := session.FilterSessionSummary(summary, "11", t0.Unix(), t0.Add(19*time.Minute).Unix(), nil)
	if filtered.TotalMatches != 1 || filtered.TotalWins != 1 || filtered.WinRate != 100.0 {
		t.Fatalf("unexpected filtered counts: %+v", filtered)
	}
	if filtered.MostPlayedPlaylist == nil || filtered.MostPlayedPlaylist.ID != 11 || filtered.MostPlayedPlaylist.NetMMR != 9.5 {
		t.Fatalf("unexpected filtered most played playlist: %+v", filtered.MostPlayedPlaylist)
	}
}
