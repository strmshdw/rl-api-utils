package syncer

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/storage"
)

func TestSyncer_PostUploadStatsPolling_Success(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	store.matches["m101"] = &storage.MatchRecord{
		MatchGUID:       "m101",
		Playlist:        11,
		UploadStatus:    storage.UploadUploaded,
		BallchasingID:   "bc-101",
		BallchasingURL:  "https://ballchasing.com/replay/bc-101",
		StatsSyncStatus: storage.StatsSyncPending,
	}

	fetcher := newMockStatsFetcher()
	fetcher.replays["bc-101"] = &ballchasing.ReplayItem{
		ID:         "bc-101",
		Status:     "ok",
		PlaylistID: "ranked-doubles",
		Duration:   300,
		Overtime:   false,
		Date:       "2026-10-16T18:00:00Z",
		Blue: ballchasing.ReplayTeam{
			Goals: 3,
			Players: []ballchasing.ReplayPlayer{
				{
					Name: "LocalHero",
					ID:   ballchasing.PlayerIdentity{Platform: "epic", ID: "epic-hero-id"},
					Score: 500, Goals: 2, Assists: 1, Saves: 1, Shots: 4,
					Demos: ballchasing.PlayerDemos{Inflicted: 1},
				},
				{
					Name: "Teammate1",
					ID:   ballchasing.PlayerIdentity{Platform: "steam", ID: "76561198000000002"},
					Score: 350, Goals: 1, Assists: 2, Saves: 2, Shots: 2,
				},
			},
		},
		Orange: ballchasing.ReplayTeam{
			Goals: 1,
			Players: []ballchasing.ReplayPlayer{
				{
					Name: "Opponent1",
					ID:   ballchasing.PlayerIdentity{Platform: "steam", ID: "76561198000000003"},
					Score: 400, Goals: 1, Assists: 0, Saves: 3, Shots: 3,
					Demos: ballchasing.PlayerDemos{Inflicted: 2},
				},
				{
					Name: "Opponent2",
					ID:   ballchasing.PlayerIdentity{Platform: "xbox", ID: "xbox-opp-id"},
					Score: 250, Goals: 0, Assists: 1, Saves: 1, Shots: 1,
				},
			},
		},
	}

	s, err := New(store, newMockHistoryProvider(), newMockDownloader(), newMockUploader(),
		WithStatsFetcher(fetcher),
		WithLocalPlayerName("LocalHero"),
		WithRateInterval(0),
	)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	stats, err := s.RunCycle(ctx)
	if err != nil || stats == nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	rec := store.matches["m101"]
	if rec.StatsSyncStatus != storage.StatsSyncSynced {
		t.Errorf("expected StatsSyncSynced, got %v", rec.StatsSyncStatus)
	}
	st, ok := store.syncedStats["m101"]
	if !ok || st.BlueScore != 3 || st.OrangeScore != 1 || *st.WinnerTeam != 0 {
		t.Fatalf("unexpected synced stats: %+v", st)
	}
	players := store.savedPlayers["m101"]
	if len(players) != 4 {
		t.Fatalf("expected 4 saved players, got %d", len(players))
	}
	var localPlayerFound bool
	for _, p := range players {
		if p.IsLocal {
			localPlayerFound = true
			if p.Won == nil || !*p.Won || p.Goals != 2 || p.Demos != 1 {
				t.Errorf("unexpected local player stats: %+v", p)
			}
		}
	}
	if !localPlayerFound {
		t.Errorf("expected local player flagged")
	}
	if outcomes := store.recordedOutcomes["m101"]; len(outcomes) != 3 {
		t.Fatalf("expected 3 recorded outcomes, got %d", len(outcomes))
	}
	expectedTime, _ := storage.ParseReplayDate("2026-10-16T18:00:00Z")
	if !store.matchTimes["m101"].Equal(expectedTime) {
		t.Errorf("expected matchTime %v, got %v", expectedTime, store.matchTimes["m101"])
	}
}

func TestSyncer_PostUploadStatsPolling_Duplicate(t *testing.T) {
	store := newMockStore()
	store.matches["m-dup"] = &storage.MatchRecord{
		MatchGUID:       "m-dup",
		Playlist:        11,
		UploadStatus:    storage.UploadDuplicate,
		BallchasingID:   "bc-dup",
		BallchasingURL:  "https://ballchasing.com/replay/bc-dup",
		StatsSyncStatus: storage.StatsSyncPending,
	}

	fetcher := newMockStatsFetcher()
	fetcher.replays["bc-dup"] = &ballchasing.ReplayItem{
		ID:     "bc-dup",
		Status: "ok",
		Date:   "2026-10-16T17:00:00Z",
		Blue: ballchasing.ReplayTeam{
			Goals:   1,
			Players: []ballchasing.ReplayPlayer{{Name: "P1", ID: ballchasing.PlayerIdentity{Platform: "epic", ID: "id1"}}},
		},
		Orange: ballchasing.ReplayTeam{
			Goals:   0,
			Players: []ballchasing.ReplayPlayer{{Name: "P2", ID: ballchasing.PlayerIdentity{Platform: "steam", ID: "id2"}}},
		},
	}

	s, _ := New(store, newMockHistoryProvider(), newMockDownloader(), newMockUploader(),
		WithStatsFetcher(fetcher),
		WithRateInterval(0),
	)

	if _, err := s.RunCycle(context.Background()); err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}
	if store.matches["m-dup"].StatsSyncStatus != storage.StatsSyncSynced {
		t.Errorf("expected duplicate replay to transition to StatsSyncSynced, got %v", store.matches["m-dup"].StatsSyncStatus)
	}
}

func TestSyncer_PostUploadStatsPolling_StatusPendingAndFailed(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	store.matches["m-pend"] = &storage.MatchRecord{
		MatchGUID:       "m-pend",
		Playlist:        11,
		UploadStatus:    storage.UploadUploaded,
		BallchasingID:   "bc-pend",
		StatsSyncStatus: storage.StatsSyncPending,
	}
	store.matches["m-fail"] = &storage.MatchRecord{
		MatchGUID:       "m-fail",
		Playlist:        11,
		UploadStatus:    storage.UploadUploaded,
		BallchasingID:   "bc-fail",
		StatsSyncStatus: storage.StatsSyncPending,
	}

	fetcher := newMockStatsFetcher()
	fetcher.replays["bc-pend"] = &ballchasing.ReplayItem{ID: "bc-pend", Status: "pending"}
	fetcher.replays["bc-fail"] = &ballchasing.ReplayItem{ID: "bc-fail", Status: "failed"}

	s, _ := New(store, newMockHistoryProvider(), newMockDownloader(), newMockUploader(),
		WithStatsFetcher(fetcher),
		WithRateInterval(0),
	)

	if _, err := s.RunCycle(ctx); err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}
	if store.matches["m-pend"].StatsSyncStatus != storage.StatsSyncPending || store.matches["m-pend"].StatsRetryCount != 0 {
		t.Errorf("pending should stay PENDING with 0 retries, got %+v", store.matches["m-pend"])
	}
	if store.matches["m-fail"].StatsSyncStatus != storage.StatsSyncFailed || store.matches["m-fail"].StatsRetryCount != 1 {
		t.Errorf("failed should transition to FAILED with 1 retry, got %+v", store.matches["m-fail"])
	}

	fetcher.replays["bc-pend"].Status = "ok"
	if _, err := s.RunCycle(ctx); err != nil {
		t.Fatalf("cycle 2 failed: %v", err)
	}
	if store.matches["m-pend"].StatsSyncStatus != storage.StatsSyncSynced {
		t.Errorf("expected m-pend to transition to SYNCED, got %v", store.matches["m-pend"].StatsSyncStatus)
	}
}

func TestSyncer_PostUploadStatsPolling_RetryBounded(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	store.matches["m-err"] = &storage.MatchRecord{
		MatchGUID:       "m-err",
		Playlist:        11,
		UploadStatus:    storage.UploadUploaded,
		BallchasingID:   "bc-err",
		StatsSyncStatus: storage.StatsSyncPending,
	}

	fetcher := newMockStatsFetcher()
	fetcher.failForID["bc-err"] = fmt.Errorf("500 internal server error")

	s, _ := New(store, newMockHistoryProvider(), newMockDownloader(), newMockUploader(),
		WithStatsFetcher(fetcher),
		WithMaxRetries(2),
		WithRateInterval(0),
	)

	_, _ = s.RunCycle(ctx)
	if store.matches["m-err"].StatsRetryCount != 1 {
		t.Fatalf("expected count 1, got %d", store.matches["m-err"].StatsRetryCount)
	}
	_, _ = s.RunCycle(ctx)
	if store.matches["m-err"].StatsRetryCount != 2 {
		t.Fatalf("expected count 2, got %d", store.matches["m-err"].StatsRetryCount)
	}
	callsBefore := fetcher.calls["bc-err"]
	_, _ = s.RunCycle(ctx)
	if fetcher.calls["bc-err"] != callsBefore {
		t.Errorf("expected no additional calls after reaching maxRetries")
	}
}

func TestSyncer_PostUploadStatsPolling_ErrNotFoundMarksExpired(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	store.matches["m-404"] = &storage.MatchRecord{
		MatchGUID:       "m-404",
		Playlist:        11,
		UploadStatus:    storage.UploadUploaded,
		BallchasingID:   "bc-404",
		StatsSyncStatus: storage.StatsSyncPending,
	}

	fetcher := newMockStatsFetcher()
	fetcher.failForID["bc-404"] = fmt.Errorf("ballchasing get replay failed: %w", ballchasing.ErrNotFound)

	s, _ := New(store, newMockHistoryProvider(), newMockDownloader(), newMockUploader(),
		WithStatsFetcher(fetcher),
		WithMaxRetries(3),
		WithRateInterval(0),
	)

	if err := s.PollAndSyncPendingStats(ctx); err != nil {
		t.Fatalf("PollAndSyncPendingStats failed: %v", err)
	}
	rec := store.matches["m-404"]
	if rec.StatsSyncStatus != storage.StatsSyncFailed || !strings.HasPrefix(rec.LastError, expiredStatsErrorPrefix) {
		t.Fatalf("expected StatsSyncFailed with expired prefix, got status=%s err=%q", rec.StatsSyncStatus, rec.LastError)
	}
	if fetcher.calls["bc-404"] != 1 {
		t.Fatalf("expected 1 fetch call, got %d", fetcher.calls["bc-404"])
	}

	// Second and third polls must NOT retry expired replay repeatedly.
	_ = s.PollAndSyncPendingStats(ctx)
	_ = s.PollAndSyncPendingStats(ctx)
	if fetcher.calls["bc-404"] != 1 {
		t.Fatalf("expected expired replay not to be retried repeatedly, got %d calls", fetcher.calls["bc-404"])
	}
}

func TestSyncer_PostUploadStatsPolling_ZeroPlayersPreservesExisting(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	store.matches["m-empty-ok"] = &storage.MatchRecord{
		MatchGUID:       "m-empty-ok",
		Playlist:        11,
		UploadStatus:    storage.UploadUploaded,
		BallchasingID:   "bc-empty-ok",
		StatsSyncStatus: storage.StatsSyncPending,
	}
	existingPlayers := []*storage.MatchPlayerRecord{
		{MatchGUID: "m-empty-ok", PlayerID: "Epic|keep|0", PlayerName: "KeepMe", Goals: 2},
	}
	store.savedPlayers["m-empty-ok"] = existingPlayers
	store.syncedStats["m-empty-ok"] = &storage.ReplayMatchStats{
		MatchGUID: "m-empty-ok",
		Players:   []storage.ReplayPlayerStats{{Name: "KeepMe", Goals: 2}},
	}

	fetcher := newMockStatsFetcher()
	fetcher.replays["bc-empty-ok"] = &ballchasing.ReplayItem{
		ID:     "bc-empty-ok",
		Status: "ok",
		Date:   "2026-10-16T18:00:00Z",
		Blue:   ballchasing.ReplayTeam{Goals: 2},
		Orange: ballchasing.ReplayTeam{Goals: 1},
	}

	s, _ := New(store, newMockHistoryProvider(), newMockDownloader(), newMockUploader(),
		WithStatsFetcher(fetcher),
		WithRateInterval(0),
	)
	if err := s.PollAndSyncPendingStats(ctx); err != nil {
		t.Fatalf("PollAndSyncPendingStats failed: %v", err)
	}

	if store.matches["m-empty-ok"].StatsSyncStatus != storage.StatsSyncSynced {
		t.Fatalf("expected SYNCED on 0-player ok replay, got %s", store.matches["m-empty-ok"].StatsSyncStatus)
	}
	if len(store.savedPlayers["m-empty-ok"]) != 1 || store.savedPlayers["m-empty-ok"][0].PlayerName != "KeepMe" {
		t.Fatalf("existing saved players must be preserved, got %+v", store.savedPlayers["m-empty-ok"])
	}
	if len(store.syncedStats["m-empty-ok"].Players) != 1 {
		t.Fatalf("existing syncedStats players must be preserved, got %+v", store.syncedStats["m-empty-ok"].Players)
	}
}

func TestSyncer_PostUploadStatsPolling_InFlightRecoveryAndSharedRateLimiter(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	store.matches["m-recov"] = &storage.MatchRecord{
		MatchGUID:       "m-recov",
		Playlist:        11,
		UploadStatus:    storage.UploadUploaded,
		BallchasingID:   "bc-recov",
		StatsSyncStatus: storage.StatsSyncSyncing,
	}
	store.matches["m-recov-2"] = &storage.MatchRecord{
		MatchGUID:       "m-recov-2",
		Playlist:        11,
		UploadStatus:    storage.UploadUploaded,
		BallchasingID:   "bc-recov-2",
		StatsSyncStatus: storage.StatsSyncPending,
	}

	fetcher := newMockStatsFetcher()
	fetcher.replays["bc-recov"] = &ballchasing.ReplayItem{ID: "bc-recov", Status: "ok", Date: "2026-10-16T18:00:00Z"}
	fetcher.replays["bc-recov-2"] = &ballchasing.ReplayItem{ID: "bc-recov-2", Status: "ok", Date: "2026-10-16T18:05:00Z"}

	sharedRL := ballchasing.NewRateLimiter(15 * time.Millisecond)
	s, err := New(store, newMockHistoryProvider(), newMockDownloader(), newMockUploader(),
		WithStatsFetcher(fetcher),
		WithRateLimiter(sharedRL),
	)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if s.RateLimiter() != sharedRL {
		t.Fatalf("expected Syncer.RateLimiter() to return configured shared limiter")
	}

	start := time.Now()
	if _, err := s.RunCycle(ctx); err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 12*time.Millisecond {
		t.Fatalf("expected shared RateLimiter pacing across 2 queries, took %v", elapsed)
	}
	if store.matches["m-recov"].StatsSyncStatus != storage.StatsSyncSynced {
		t.Errorf("expected recovered in-flight record SYNCED, got %v", store.matches["m-recov"].StatsSyncStatus)
	}
}

func TestSyncer_ResolveLocalPlayer_AllTiersAndBotImmunity(t *testing.T) {
	roster := []ReplayPlayerCandidate{
		{Name: "PlayerTier1", Platform: "Epic", OnlineID: "epic-p1", CanonicalID: "Epic|epic-p1|0", Team: 0},
		{Name: "PlayerTier2", Platform: "Steam", OnlineID: "76561198000000002", CanonicalID: "Steam|76561198000000002|0", Team: 1},
		{Name: "PlayerTier3", Platform: "Steam", OnlineID: "76561198000000003", CanonicalID: "Steam|76561198000000003|0", Team: 0},
		{Name: "PlayerTier4", Platform: "Xbox", OnlineID: "xbox-p4", CanonicalID: "Xbox|xbox-p4|0", Team: 1},
		{Name: "PlayerTier5", Platform: "PlayStation", OnlineID: "ps-p5", CanonicalID: "PlayStation|ps-p5|0", Team: 0},
		{Name: "PlayerTier6", Platform: "Switch", OnlineID: "sw-p6", CanonicalID: "Switch|sw-p6|0", Team: 1},
	}

	cases := []struct {
		id       LocalPlayerIdentity
		wantIdx  int
		wantTeam int
		wantOK   bool
	}{
		{LocalPlayerIdentity{ConfigLocalPlayerID: "Epic|epic-p1|0"}, 0, 0, true},
		{LocalPlayerIdentity{AuthAccountID: "76561198000000002"}, 1, 1, true},
		{LocalPlayerIdentity{PingSteamID: "76561198000000003"}, 2, 0, true},
		{LocalPlayerIdentity{ConfigLocalPlayerName: "PlayerTier4"}, 3, 1, true},
		{LocalPlayerIdentity{AuthDisplayName: "PlayerTier5"}, 4, 0, true},
		{LocalPlayerIdentity{ReplayPlayerName: "PlayerTier6"}, 5, 1, true},
		{LocalPlayerIdentity{ConfigLocalPlayerName: "NoSuchPlayer"}, -1, 0, false},
	}
	for i, tc := range cases {
		idx, team, found := ResolveLocalPlayer(roster, tc.id)
		if found != tc.wantOK || idx != tc.wantIdx || (found && *team != tc.wantTeam) {
			t.Errorf("case %d failed: idx=%d team=%v found=%v", i, idx, team, found)
		}
	}

	localTeamNum := 0
	idx, team, found := ResolveLocalPlayer(roster, LocalPlayerIdentity{ReplayLocalTeam: &localTeamNum})
	if !found || idx != -1 || *team != 0 {
		t.Errorf("Tier 6 team fallback failed: idx=%d, team=%v, found=%v", idx, team, found)
	}

	botRoster := []ReplayPlayerCandidate{
		{Name: "Saltie", Platform: "Unknown", OnlineID: "bot-1", CanonicalID: "bot-1", Team: 0, IsBot: true},
		{Name: "RealPlayer", Platform: "Epic", OnlineID: "real-id", CanonicalID: "Epic|real-id|0", Team: 1, IsBot: false},
	}
	if idx, _, found := ResolveLocalPlayer(botRoster, LocalPlayerIdentity{ConfigLocalPlayerName: "Saltie"}); found || idx != -1 {
		t.Errorf("bot immunity violated by name")
	}
	if idx, _, found := ResolveLocalPlayer(botRoster, LocalPlayerIdentity{ConfigLocalPlayerID: "bot-1"}); found || idx != -1 {
		t.Errorf("bot immunity violated by ID")
	}
}

func TestSyncer_SyncDownloadedReplayStats_Idempotent(t *testing.T) {
	files, err := filepath.Glob("../../replays/*.replay")
	if err != nil || len(files) == 0 {
		t.Skip("no replay files found")
	}

	store := newMockStore()
	guid := "test-idempotent-guid"
	store.matches[guid] = &storage.MatchRecord{MatchGUID: guid, Playlist: 11}
	s, err := New(store, newMockHistoryProvider(), newMockDownloader(), newMockUploader())
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	ctx := context.Background()
	if err := s.SyncDownloadedReplayStats(ctx, guid, files[0]); err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if err := s.SyncDownloadedReplayStats(ctx, guid, files[0]); err != nil {
		t.Fatalf("second call should ignore ErrMatchAlreadyProcessed, got: %v", err)
	}
}
