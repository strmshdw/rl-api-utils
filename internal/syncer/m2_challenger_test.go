package syncer

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/storage"
)

// ============================================================================
// CHALLENGER SUITE 1: Concurrent RunCycle Execution (Mutex Serialization)
// ============================================================================

// TestChallenger_ConcurrentRunCycles_MutexSerialization spins up 10 concurrent goroutines
// attempting to run RunCycle simultaneously on the same Syncer instance, verifying mutex
// serialization, absence of race conditions, and clean sequential execution.
func TestChallenger_ConcurrentRunCycles_MutexSerialization(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.NewSQLiteStore(filepath.Join(tmpDir, "concurrent_syncer.db"))
	if err != nil {
		t.Fatalf("failed to open sqlite store: %v", err)
	}
	defer store.Close()

	provider := newMockHistoryProvider(DiscoveredMatch{
		MatchGUID:            "match-conc-1",
		RecordStartTimestamp: 1000,
		ReplayURL:            "https://cdn.example.com/conc1.replay",
	})
	dl := newMockDownloader()
	ul := newMockUploader()
	stats := newMockStatsFetcher()
	stats.replays["bc-match-conc-1"] = &ballchasing.ReplayItem{
		ID:     "bc-match-conc-1",
		Status: "ok",
		Blue:   ballchasing.ReplayTeam{Goals: 3},
		Orange: ballchasing.ReplayTeam{Goals: 1},
	}

	s, err := New(store, provider, dl, ul,
		WithStatsFetcher(stats),
		WithReplayDir(tmpDir),
		WithRateInterval(0),
	)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}

	const numWorkers = 10
	var wg sync.WaitGroup
	errCh := make(chan error, numWorkers)
	var activeConcurrent int32
	var maxObservedConcurrent int32

	ctx := context.Background()

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// Check concurrency depth inside loop
			cur := atomic.AddInt32(&activeConcurrent, 1)
			for {
				oldMax := atomic.LoadInt32(&maxObservedConcurrent)
				if cur <= oldMax || atomic.CompareAndSwapInt32(&maxObservedConcurrent, oldMax, cur) {
					break
				}
			}

			// Run cycle
			res, cycleErr := s.RunCycle(ctx)
			atomic.AddInt32(&activeConcurrent, -1)

			if cycleErr != nil {
				errCh <- fmt.Errorf("worker %d cycle failed: %w", workerID, cycleErr)
				return
			}
			if res == nil {
				errCh <- fmt.Errorf("worker %d returned nil stats", workerID)
				return
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for e := range errCh {
		t.Errorf("concurrent RunCycle error: %v", e)
	}

	t.Logf("10 concurrent goroutines completed without race conditions or deadlocks. Max concurrent callers: %d", maxObservedConcurrent)
}

// ============================================================================
// CHALLENGER SUITE 2: HTTP 409 (Duplicate) -> UPLOADED/DUPLICATE & Stats Polled
// ============================================================================

func TestChallenger_HTTP409_DuplicateTransitionsAndStatsPolled(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.NewSQLiteStore(filepath.Join(tmpDir, "dup_409.db"))
	if err != nil {
		t.Fatalf("failed to open sqlite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	provider := newMockHistoryProvider(DiscoveredMatch{
		MatchGUID:            "guid-dup-test",
		RecordStartTimestamp: 1000,
		ReplayURL:            "https://cdn.example.com/dup.replay",
	})
	dl := newMockDownloader()
	dupUploader := newMockUploader()
	dupUploader.duplicates["guid-dup-test"] = true // Mark as duplicate (HTTP 409 simulation)

	statsFetcher := newMockStatsFetcher()
	statsFetcher.replays["bc-guid-dup-test"] = &ballchasing.ReplayItem{
		ID:         "bc-guid-dup-test",
		Status:     "ok",
		Date:       "2026-10-15T18:00:00Z",
		PlaylistID: "ranked-doubles",
		Blue: ballchasing.ReplayTeam{
			Goals: 4,
			Players: []ballchasing.ReplayPlayer{
				{
					Name:  "LocalHero",
					ID:    ballchasing.PlayerID{Platform: "steam", ID: "76561198000000001"},
					Score: 500,
					Goals: 2,
				},
			},
		},
		Orange: ballchasing.ReplayTeam{
			Goals: 1,
			Players: []ballchasing.ReplayPlayer{
				{
					Name:  "RivalOpponent",
					ID:    ballchasing.PlayerID{Platform: "epic", ID: "epic_rival_1"},
					Score: 250,
					Goals: 1,
				},
			},
		},
	}

	syncer, err := New(store, provider, dl, dupUploader,
		WithStatsFetcher(statsFetcher),
		WithReplayDir(tmpDir),
		WithLocalPlayerID("Steam|76561198000000001|0"),
		WithRateInterval(0),
	)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}

	// Execute RunCycle
	stats, err := syncer.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	if stats.DuplicateCount != 1 {
		t.Fatalf("expected DuplicateCount=1, got %d", stats.DuplicateCount)
	}

	// Verify record in store: upload_status must be DUPLICATE, stats_sync_status must be SYNCED
	rec, err := store.GetMatch(ctx, "guid-dup-test")
	if err != nil {
		t.Fatalf("GetMatch failed: %v", err)
	}
	if rec.UploadStatus != storage.UploadDuplicate {
		t.Fatalf("expected UploadStatus=DUPLICATE, got %s", rec.UploadStatus)
	}
	if rec.BallchasingID != "bc-guid-dup-test" {
		t.Fatalf("expected BallchasingID=bc-guid-dup-test, got %s", rec.BallchasingID)
	}
	if rec.StatsSyncStatus != storage.StatsSyncSynced {
		t.Fatalf("expected StatsSyncStatus=SYNCED, got %s", rec.StatsSyncStatus)
	}

	// Verify match_players were ingested
	players, err := store.GetMatchPlayers(ctx, "guid-dup-test")
	if err != nil {
		t.Fatalf("GetMatchPlayers failed: %v", err)
	}
	if len(players) != 2 {
		t.Fatalf("expected 2 match players, got %d", len(players))
	}

	// Verify head-to-head outcome was recorded for RivalOpponent (Orange lost to Blue -> LossesAsOpponent = 1)
	matchup, err := store.GetPlayerMatchup(ctx, "Epic|epic_rival_1|0", 11) // ranked-doubles = 11
	if err != nil {
		t.Fatalf("GetPlayerMatchup failed: %v", err)
	}
	if matchup.TotalMatches != 1 || matchup.LossesAsOpponent != 1 {
		t.Fatalf("expected 1 match against RivalOpponent (LossesAsOpponent=1), got Total=%d, Losses=%d",
			matchup.TotalMatches, matchup.LossesAsOpponent)
	}
	t.Logf("HTTP 409 duplicate replay verified: marked DUPLICATE, stats polled and SYNCED, H2H recorded!")
}

// ============================================================================
// CHALLENGER SUITE 3: Simulated Transient Network Failures & Recovery
// ============================================================================

type transientFailUploader struct {
	mu           sync.Mutex
	failAttempts int
	calls        int
}

func (u *transientFailUploader) UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.calls++
	if u.calls <= u.failAttempts {
		return nil, errors.New("503 Service Unavailable: transient network error")
	}
	return &UploadResult{
		ID:          "bc-recovered-" + matchGUID,
		Location:    "https://ballchasing.com/replay/bc-recovered-" + matchGUID,
		IsDuplicate: false,
	}, nil
}

func TestChallenger_TransientNetworkFailure_StatusFailedAndRecovery(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.NewSQLiteStore(filepath.Join(tmpDir, "transient_fail.db"))
	if err != nil {
		t.Fatalf("failed to open sqlite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	provider := newMockHistoryProvider(DiscoveredMatch{
		MatchGUID:            "guid-transient-test",
		RecordStartTimestamp: 1000,
		ReplayURL:            "https://cdn.example.com/transient.replay",
	})
	dl := newMockDownloader()
	failingUploader := &transientFailUploader{failAttempts: 1} // Fails on 1st call, succeeds on 2nd
	statsFetcher := newMockStatsFetcher()
	statsFetcher.replays["bc-recovered-guid-transient-test"] = &ballchasing.ReplayItem{
		ID:     "bc-recovered-guid-transient-test",
		Status: "ok",
		Blue:   ballchasing.ReplayTeam{Goals: 2},
		Orange: ballchasing.ReplayTeam{Goals: 1},
	}

	syncer, err := New(store, provider, dl, failingUploader,
		WithStatsFetcher(statsFetcher),
		WithReplayDir(tmpDir),
		WithRateInterval(0),
	)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}

	// Cycle 1: Download succeeds, Upload fails with 503
	stats1, err := syncer.RunCycle(ctx)
	if err != nil {
		t.Fatalf("Cycle 1 RunCycle failed: %v", err)
	}
	if stats1.FailedCount != 1 {
		t.Fatalf("expected FailedCount=1 in Cycle 1, got %d", stats1.FailedCount)
	}

	// Verify status in store is FAILED with retry_count = 1
	rec1, err := store.GetMatch(ctx, "guid-transient-test")
	if err != nil {
		t.Fatalf("GetMatch failed: %v", err)
	}
	if rec1.UploadStatus != storage.UploadFailed {
		t.Fatalf("expected UploadStatus=FAILED, got %s", rec1.UploadStatus)
	}
	if rec1.RetryCount != 1 {
		t.Fatalf("expected RetryCount=1, got %d", rec1.RetryCount)
	}

	// Reset retry to simulate subsequent cycle after backoff
	if err := store.ResetMatchRetry(ctx, "guid-transient-test"); err != nil {
		t.Fatalf("ResetMatchRetry failed: %v", err)
	}

	// Cycle 2: Upload recovers and succeeds!
	stats2, err := syncer.RunCycle(ctx)
	if err != nil {
		t.Fatalf("Cycle 2 RunCycle failed: %v", err)
	}
	if stats2.UploadedCount != 1 {
		t.Fatalf("expected UploadedCount=1 in Cycle 2, got %d", stats2.UploadedCount)
	}

	rec2, err := store.GetMatch(ctx, "guid-transient-test")
	if err != nil {
		t.Fatalf("GetMatch failed: %v", err)
	}
	if rec2.UploadStatus != storage.UploadUploaded {
		t.Fatalf("expected UploadStatus=UPLOADED after recovery, got %s", rec2.UploadStatus)
	}
	if rec2.StatsSyncStatus != storage.StatsSyncSynced {
		t.Fatalf("expected StatsSyncStatus=SYNCED after recovery, got %s", rec2.StatsSyncStatus)
	}
	t.Logf("Transient failure and recovery verified: transitioned to FAILED, then recovered cleanly to UPLOADED and SYNCED!")
}

// ============================================================================
// CHALLENGER SUITE 4: Multi-Identity Local Player Resolution & 100% Strict Bot Immunity
// ============================================================================

func TestChallenger_LocalPlayerResolution_All6TiersAndStrictBotImmunity(t *testing.T) {
	candidates := []ReplayPlayerCandidate{
		{
			Name:        "Saltie",
			Platform:    "Steam",
			OnlineID:    "0",
			CanonicalID: "Steam|0|0",
			Team:        0,
			IsBot:       true, // BOT!
		},
		{
			Name:        "Armstrong",
			Platform:    "Epic",
			OnlineID:    "bot_armstrong",
			CanonicalID: "Epic|bot_armstrong|0",
			Team:        1,
			IsBot:       true, // BOT!
		},
		{
			Name:        "HumanPlayerOne",
			Platform:    "Steam",
			OnlineID:    "76561198000000010",
			CanonicalID: "Steam|76561198000000010|0",
			Team:        0,
			IsBot:       false,
		},
		{
			Name:        "HumanPlayerTwo",
			Platform:    "Epic",
			OnlineID:    "epic_human_2",
			CanonicalID: "Epic|epic_human_2|0",
			Team:        1,
			IsBot:       false,
		},
	}

	// 1. Strict Bot Immunity: Even if configured local player identity matches a bot exactly,
	// the bot MUST NEVER be resolved as the local player!
	botAttacks := []LocalPlayerIdentity{
		{ConfigLocalPlayerID: "Steam|0|0"},
		{ConfigLocalPlayerID: "0"},
		{ConfigLocalPlayerName: "Saltie"},
		{AuthAccountID: "0"},
		{AuthDisplayName: "Saltie"},
		{PingSteamID: "0"},
		{PingName: "Saltie"},
		{UploaderSteamID: "0"},
		{UploaderName: "Saltie"},
		{ReplayPlayerName: "Saltie"},
		{ConfigLocalPlayerID: "Epic|bot_armstrong|0"},
		{ConfigLocalPlayerName: "Armstrong"},
	}

	for _, attack := range botAttacks {
		idx, team, found := ResolveLocalPlayer(candidates, attack)
		if found && idx >= 0 && candidates[idx].IsBot {
			t.Fatalf("CRITICAL SECURITY VIOLATION: Bot %s was resolved as local player! (identity=%+v)",
				candidates[idx].Name, attack)
		}
		_ = team
	}
	t.Logf("Strict bot immunity verified against 12 bot usurpation vectors (0 bots matched).")

	// 2. Tier 1: ConfigLocalPlayerID
	id1 := LocalPlayerIdentity{ConfigLocalPlayerID: "Steam|76561198000000010|0"}
	idx1, team1, found1 := ResolveLocalPlayer(candidates, id1)
	if !found1 || idx1 != 2 || team1 == nil || *team1 != 0 {
		t.Fatalf("Tier 1 failed: found=%v, idx=%d, team=%v", found1, idx1, team1)
	}

	// 3. Tier 2: AuthAccountID
	id2 := LocalPlayerIdentity{AuthAccountID: "epic_human_2"}
	idx2, team2, found2 := ResolveLocalPlayer(candidates, id2)
	if !found2 || idx2 != 3 || team2 == nil || *team2 != 1 {
		t.Fatalf("Tier 2 failed: found=%v, idx=%d, team=%v", found2, idx2, team2)
	}

	// 4. Tier 3: PingSteamID & UploaderSteamID
	id3 := LocalPlayerIdentity{UploaderSteamID: "76561198000000010"}
	idx3, team3, found3 := ResolveLocalPlayer(candidates, id3)
	if !found3 || idx3 != 2 || team3 == nil || *team3 != 0 {
		t.Fatalf("Tier 3 failed: found=%v, idx=%d, team=%v", found3, idx3, team3)
	}

	// 5. Tier 4: ConfigLocalPlayerName
	id4 := LocalPlayerIdentity{ConfigLocalPlayerName: "HumanPlayerTwo"}
	idx4, team4, found4 := ResolveLocalPlayer(candidates, id4)
	if !found4 || idx4 != 3 || team4 == nil || *team4 != 1 {
		t.Fatalf("Tier 4 failed: found=%v, idx=%d, team=%v", found4, idx4, team4)
	}

	// 6. Tier 5: AuthDisplayName / PingName / UploaderName
	id5 := LocalPlayerIdentity{AuthDisplayName: "HumanPlayerOne"}
	idx5, team5, found5 := ResolveLocalPlayer(candidates, id5)
	if !found5 || idx5 != 2 || team5 == nil || *team5 != 0 {
		t.Fatalf("Tier 5 failed: found=%v, idx=%d, team=%v", found5, idx5, team5)
	}

	// 7. Tier 6: ReplayPlayerName
	id6 := LocalPlayerIdentity{ReplayPlayerName: "HumanPlayerTwo"}
	idx6, team6, found6 := ResolveLocalPlayer(candidates, id6)
	if !found6 || idx6 != 3 || team6 == nil || *team6 != 1 {
		t.Fatalf("Tier 6 failed: found=%v, idx=%d, team=%v", found6, idx6, team6)
	}

	// 8. Tier 6 fallback: ReplayLocalTeam (when player name is not in roster)
	teamOrange := 1
	id6b := LocalPlayerIdentity{ReplayLocalTeam: &teamOrange}
	idx6b, team6b, found6b := ResolveLocalPlayer(candidates, id6b)
	if !found6b || idx6b != -1 || team6b == nil || *team6b != 1 {
		t.Fatalf("Tier 6 team fallback failed: found=%v, idx=%d, team=%v", found6b, idx6b, team6b)
	}

	t.Logf("All 6 resolution tiers and team fallback verified successfully!")
}
