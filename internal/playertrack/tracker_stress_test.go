package playertrack_test

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rlapi"
)

// ============================================================================
// Stress Test Doubles
// ============================================================================

type stressSkillFetcher struct {
	mu           sync.Mutex
	totalCalls   atomic.Int64
	inFlight     atomic.Int64
	maxInFlight  atomic.Int64
	delay        time.Duration
	queriedPids  map[string]int
	closed       bool
	callNotifyCh chan struct{}
}

func newStressSkillFetcher(delay time.Duration) *stressSkillFetcher {
	return &stressSkillFetcher{
		delay:        delay,
		queriedPids:  make(map[string]int),
		callNotifyCh: make(chan struct{}, 1000),
	}
}

func (s *stressSkillFetcher) GetPlayersSkills(ctx context.Context, playerIDs []rlapi.PlayerID) ([]rlapi.PlayerWithSkills, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cur := s.inFlight.Add(1)
	defer s.inFlight.Add(-1)

	// Track max concurrent in-flight RPCs
	for {
		oldMax := s.maxInFlight.Load()
		if cur <= oldMax || s.maxInFlight.CompareAndSwap(oldMax, cur) {
			break
		}
	}

	s.totalCalls.Add(1)

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, playertrack.ErrRankClientClosed
	}
	for _, pid := range playerIDs {
		s.queriedPids[string(pid)]++
	}
	s.mu.Unlock()

	select {
	case s.callNotifyCh <- struct{}{}:
	default:
	}

	if s.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(s.delay):
		}
	}

	var results []rlapi.PlayerWithSkills
	for _, pid := range playerIDs {
		results = append(results, rlapi.PlayerWithSkills{
			PlayerID: pid,
			Skills: []rlapi.Skill{
				{Playlist: 11, Tier: 16, Division: 3, MMR: 1150.0},
				{Playlist: 13, Tier: 15, Division: 2, MMR: 1080.0},
			},
		})
	}
	return results, nil
}

func (s *stressSkillFetcher) IsEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed
}

func (s *stressSkillFetcher) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// ============================================================================
// Empirical Challenge 1: Burst 120Hz/240Hz Telemetry & Concurrent JSON Readers
// ============================================================================

func TestTracker_Stress_BurstTelemetry_120Hz240Hz_ConcurrentReaders(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := newSQLiteTestStore(t)
	fetcher := newStressSkillFetcher(15 * time.Millisecond) // Realistic PsyNet latency

	cfg := config.PlayerTrackingConfig{
		Enabled:        true,
		LocalPlayerID:  "Steam|local|0",
		AutoFetchRanks: true,
	}
	tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
	if err != nil {
		t.Fatalf("NewTracker failed: %v", err)
	}
	defer func() { _ = tracker.Close() }()

	const totalEvents = 10000
	const numWriters = 4
	const eventsPerWriter = totalEvents / numWriters
	const numReaders = 12

	var wg sync.WaitGroup
	var completedWrites atomic.Int64
	var completedReads atomic.Int64
	var jsonCorruptions atomic.Int64

	startBarrier := make(chan struct{})

	// 12 Concurrent Readers continuously fetching and JSON-serializing
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			<-startBarrier

			for {
				if completedWrites.Load() >= totalEvents {
					break
				}

				snap := tracker.GetCurrentMatch()
				if snap == nil {
					jsonCorruptions.Add(1)
					continue
				}

				data, err := json.Marshal(snap)
				if err != nil {
					jsonCorruptions.Add(1)
					t.Errorf("reader %d failed to marshal CurrentMatchResponse: %v", readerID, err)
					continue
				}

				// Deep integrity check: Unmarshal back and verify no corrupted fields
				var verified playertrack.CurrentMatchResponse
				if err := json.Unmarshal(data, &verified); err != nil {
					jsonCorruptions.Add(1)
					t.Errorf("reader %d unmarshal failed (data corruption): %v", readerID, err)
					continue
				}

				if verified.MatchGUID != "" && verified.MatchGUID != "burst-telemetry-guid" {
					jsonCorruptions.Add(1)
					t.Errorf("reader %d got corrupted MatchGUID: %q", readerID, verified.MatchGUID)
				}

				completedReads.Add(1)
			}
		}(r)
	}

	// 4 Concurrent Writers blasting 10,000+ OnUpdateState events
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			<-startBarrier

			for i := 0; i < eventsPerWriter; i++ {
				score := (writerID * 100000) + i
				players := []statsapi.StatsPlayer{
					{Name: "LocalUser", PrimaryId: "Steam|local|0", TeamNum: 0, Score: score, Goals: i % 5},
					{Name: "Teammate1", PrimaryId: "Steam|tm1|0", TeamNum: 0, Score: score / 2, Assists: i % 3},
					{Name: "Teammate2", PrimaryId: "Steam|tm2|0", TeamNum: 0, Score: score / 3, Saves: i % 4},
					{Name: "Opponent1", PrimaryId: "Epic|opp1|0", TeamNum: 1, Score: score / 4, Shots: i % 6},
					{Name: "Opponent2", PrimaryId: "Epic|opp2|0", TeamNum: 1, Score: score / 5, Demos: i % 2},
					{Name: "Opponent3", PrimaryId: "Epic|opp3|0", TeamNum: 1, Score: score / 6},
				}

				if err := tracker.OnUpdateState(ctx, "burst-telemetry-guid", 11, players); err != nil {
					t.Errorf("writer %d OnUpdateState failed: %v", writerID, err)
					return
				}
				completedWrites.Add(1)
			}
		}(w)
	}

	// Release all workers simultaneously
	close(startBarrier)
	wg.Wait()

	if completedWrites.Load() != totalEvents {
		t.Errorf("expected %d completed writes; got %d", totalEvents, completedWrites.Load())
	}
	if completedReads.Load() < 100 {
		t.Errorf("expected at least 100 concurrent reads during burst; got %d", completedReads.Load())
	}
	if jsonCorruptions.Load() != 0 {
		t.Fatalf("encountered %d JSON corruptions or nil snapshots under burst load!", jsonCorruptions.Load())
	}

	// Wait for any in-flight rank query to finish
	time.Sleep(100 * time.Millisecond)

	// Verify exact single-flight RPC rank retrieval:
	// All 5 non-local players were part of the initial lobby.
	// Debounce and inFlight dedup must ensure exactly 1 batch RPC was dispatched!
	totalRPCCalls := fetcher.totalCalls.Load()
	if totalRPCCalls != 1 {
		t.Errorf("EXACT SINGLE-FLIGHT VIOLATION: expected exactly 1 batch RPC call for lobby, got %d calls!", totalRPCCalls)
	}

	// Verify all 5 non-local players were included in the single-flight query
	fetcher.mu.Lock()
	for _, pid := range []string{"Steam|tm1|0", "Steam|tm2|0", "Epic|opp1|0", "Epic|opp2|0", "Epic|opp3|0"} {
		if fetcher.queriedPids[pid] != 1 {
			t.Errorf("player %s queried %d times, expected exactly 1", pid, fetcher.queriedPids[pid])
		}
	}
	fetcher.mu.Unlock()

	// Verify that the snapshot correctly reflects populated ranks
	finalSnap := tracker.GetCurrentMatch()
	if len(finalSnap.Teammates) != 2 {
		t.Fatalf("expected 2 teammates, got %d", len(finalSnap.Teammates))
	}
	if len(finalSnap.Opponents) != 3 {
		t.Fatalf("expected 3 opponents, got %d", len(finalSnap.Opponents))
	}
	for _, tm := range finalSnap.Teammates {
		if tm.CurrentRank == nil {
			t.Errorf("teammate %s missing CurrentRank after fetch", tm.PlayerID)
		} else if tm.CurrentRank.RankName != "Champion I Division IV" {
			t.Errorf("teammate %s rank mismatch: %s", tm.PlayerID, tm.CurrentRank.RankName)
		}
	}
	for _, opp := range finalSnap.Opponents {
		if opp.CurrentRank == nil {
			t.Errorf("opponent %s missing CurrentRank after fetch", opp.PlayerID)
		} else if opp.CurrentRank.RankName != "Champion I Division IV" {
			t.Errorf("opponent %s rank mismatch: %s", opp.PlayerID, opp.CurrentRank.RankName)
		}
	}
}

// ============================================================================
// Empirical Challenge 2: Deep Clone Mutation Challenge
// ============================================================================

func TestTracker_Stress_DeepClone_MutationIsolation(t *testing.T) {
	ctx := context.Background()
	store := newSQLiteTestStore(t)
	fetcher := newTestSkillFetcher()
	fetcher.skillsMap["Steam|tm1|0"] = []rlapi.Skill{{Playlist: 11, Tier: 15, Division: 1, MMR: 1050.0}}
	fetcher.skillsMap["Epic|opp1|0"] = []rlapi.Skill{{Playlist: 11, Tier: 16, Division: 2, MMR: 1120.0}}

	// Pre-seed matchup history in store
	_ = store.RecordMatchResults(ctx, "preseed-guid", 11, []storage.PlayerOutcome{
		{PlayerID: "Steam|tm1|0", Platform: "Steam", PlayerName: "Teammate1", IsTeammate: true, Won: true},
		{PlayerID: "Epic|opp1|0", Platform: "Epic", PlayerName: "Opponent1", IsTeammate: false, Won: true},
	})

	cfg := config.PlayerTrackingConfig{
		Enabled:        true,
		LocalPlayerID:  "Steam|local|0",
		AutoFetchRanks: true,
	}
	tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
	if err != nil {
		t.Fatalf("NewTracker failed: %v", err)
	}
	defer func() { _ = tracker.Close() }()

	players := []statsapi.StatsPlayer{
		{Name: "LocalUser", PrimaryId: "Steam|local|0", TeamNum: 0, Score: 500, Goals: 2, Assists: 1, Saves: 3},
		{Name: "Teammate1", PrimaryId: "Steam|tm1|0", TeamNum: 0, Score: 300, Goals: 1, Assists: 2, Saves: 1},
		{Name: "Opponent1", PrimaryId: "Epic|opp1|0", TeamNum: 1, Score: 200, Goals: 0, Shots: 4, Demos: 1},
		{Name: "Spectator1", PrimaryId: "Steam|spec1|0", TeamNum: 255},
	}

	if err := tracker.OnUpdateState(ctx, "mutation-challenge-guid", 11, players); err != nil {
		t.Fatalf("OnUpdateState failed: %v", err)
	}

	// Wait for async rank queries to settle
	time.Sleep(50 * time.Millisecond)

	// Fetch snapshot 1
	snap1 := tracker.GetCurrentMatch()
	if snap1 == nil || snap1.LocalPlayer == nil || len(snap1.Teammates) == 0 || len(snap1.Opponents) == 0 {
		t.Fatalf("expected fully populated snap1, got %+v", snap1)
	}

	// Deeply and aggressively mutate EVERY field, pointer, slice, and map in snap1
	snap1.ActiveMatch = false
	snap1.MatchEnded = true
	snap1.MatchGUID = "MUTATED_GUID_EVIL"
	snap1.PlaylistID = 99999
	snap1.PlaylistName = "MUTATED_PLAYLIST_EVIL"
	snap1.Result = "MUTATED_VICTORY_EVIL"

	if snap1.LocalTeam != nil {
		*snap1.LocalTeam = 999
	}
	if snap1.WinnerTeam != nil {
		*snap1.WinnerTeam = 888
	}

	// Mutate snap1.LocalPlayer
	snap1.LocalPlayer.PlayerID = "MUTATED_LOCAL_ID"
	snap1.LocalPlayer.Platform = "MUTATED_PLATFORM"
	snap1.LocalPlayer.Name = "MUTATED_LOCAL_NAME"
	snap1.LocalPlayer.TeamNum = 999
	snap1.LocalPlayer.IsLocal = false
	snap1.LocalPlayer.IsBot = true
	snap1.LocalPlayer.Stats.Score = 999999
	snap1.LocalPlayer.Stats.Goals = 999
	if snap1.LocalPlayer.CurrentRank != nil {
		snap1.LocalPlayer.CurrentRank.RankName = "MUTATED_RANK_NAME"
		snap1.LocalPlayer.CurrentRank.MMR = 99999.0
	}
	if snap1.LocalPlayer.Ranks != nil {
		snap1.LocalPlayer.Ranks["11"] = playertrack.PlayerPlaylistRank{RankName: "MUTATED_IN_MAP"}
		snap1.LocalPlayer.Ranks["9999"] = playertrack.PlayerPlaylistRank{RankName: "INJECTED_MAP_KEY"}
	}
	if snap1.LocalPlayer.MatchupRecord != nil {
		snap1.LocalPlayer.MatchupRecord.WinsAsTeammate = 999999
	}

	// Mutate snap1.Teammates
	for i := range snap1.Teammates {
		snap1.Teammates[i].PlayerID = "MUTATED_TM_ID"
		snap1.Teammates[i].Name = "MUTATED_TM_NAME"
		snap1.Teammates[i].Stats.Score = 888888
		if snap1.Teammates[i].CurrentRank != nil {
			snap1.Teammates[i].CurrentRank.Tier = 999
			snap1.Teammates[i].CurrentRank.RankName = "MUTATED_TM_RANK"
		}
		if snap1.Teammates[i].Ranks != nil {
			snap1.Teammates[i].Ranks["11"] = playertrack.PlayerPlaylistRank{Tier: 999}
			snap1.Teammates[i].Ranks["8888"] = playertrack.PlayerPlaylistRank{Tier: 888}
		}
		if snap1.Teammates[i].MatchupRecord != nil {
			snap1.Teammates[i].MatchupRecord.WinsAsTeammate = 777777
		}
	}
	snap1.Teammates = append(snap1.Teammates, playertrack.LobbyPlayer{
		PlayerID: "INJECTED_EXTRA_TEAMMATE",
		Name:     "EvilHacker",
	})

	// Mutate snap1.Opponents
	for i := range snap1.Opponents {
		snap1.Opponents[i].PlayerID = "MUTATED_OPP_ID"
		snap1.Opponents[i].Stats.Score = 777777
		if snap1.Opponents[i].CurrentRank != nil {
			snap1.Opponents[i].CurrentRank.Tier = 999
		}
		if snap1.Opponents[i].Ranks != nil {
			snap1.Opponents[i].Ranks["11"] = playertrack.PlayerPlaylistRank{Tier: 999}
		}
		if snap1.Opponents[i].MatchupRecord != nil {
			snap1.Opponents[i].MatchupRecord.WinsAsOpponent = 666666
		}
	}
	snap1.Opponents = append(snap1.Opponents, playertrack.LobbyPlayer{
		PlayerID: "INJECTED_EXTRA_OPPONENT",
	})

	// Mutate snap1.Spectators
	snap1.Spectators = append(snap1.Spectators, playertrack.LobbyPlayer{
		PlayerID: "INJECTED_SPECTATOR",
	})

	// Now fetch snap2 from tracker: verify it is completely pristine and uncorrupted!
	snap2 := tracker.GetCurrentMatch()
	if snap2 == nil {
		t.Fatalf("snap2 is nil")
	}

	// Verify top-level scalars
	if !snap2.ActiveMatch {
		t.Errorf("MEMORY LEAK / CORRUPTION: snap2.ActiveMatch mutated to false")
	}
	if snap2.MatchEnded {
		t.Errorf("MEMORY LEAK / CORRUPTION: snap2.MatchEnded mutated to true")
	}
	if snap2.MatchGUID != "mutation-challenge-guid" {
		t.Errorf("MEMORY LEAK / CORRUPTION: snap2.MatchGUID mutated to %q", snap2.MatchGUID)
	}
	if snap2.PlaylistID != 11 {
		t.Errorf("MEMORY LEAK / CORRUPTION: snap2.PlaylistID mutated to %d", snap2.PlaylistID)
	}
	if snap2.PlaylistName != "Ranked Doubles (2v2)" {
		t.Errorf("MEMORY LEAK / CORRUPTION: snap2.PlaylistName mutated to %q", snap2.PlaylistName)
	}
	if snap2.LocalTeam == nil || *snap2.LocalTeam != 0 {
		t.Errorf("MEMORY LEAK / CORRUPTION: snap2.LocalTeam mutated to %v", snap2.LocalTeam)
	}

	// Verify LocalPlayer isolation
	if snap2.LocalPlayer == nil {
		t.Fatalf("snap2.LocalPlayer is nil")
	}
	if snap2.LocalPlayer.PlayerID != "Steam|local|0" {
		t.Errorf("MEMORY LEAK / CORRUPTION: LocalPlayer.PlayerID mutated to %q", snap2.LocalPlayer.PlayerID)
	}
	if snap2.LocalPlayer.Name != "LocalUser" {
		t.Errorf("MEMORY LEAK / CORRUPTION: LocalPlayer.Name mutated to %q", snap2.LocalPlayer.Name)
	}
	if snap2.LocalPlayer.TeamNum != 0 {
		t.Errorf("MEMORY LEAK / CORRUPTION: LocalPlayer.TeamNum mutated to %d", snap2.LocalPlayer.TeamNum)
	}
	if snap2.LocalPlayer.Stats.Score != 500 {
		t.Errorf("MEMORY LEAK / CORRUPTION: LocalPlayer.Stats.Score mutated to %d", snap2.LocalPlayer.Stats.Score)
	}

	// Verify Teammates slice & element isolation
	if len(snap2.Teammates) != 1 {
		t.Fatalf("MEMORY LEAK / CORRUPTION: snap2.Teammates length mutated to %d (slice shared!)", len(snap2.Teammates))
	}
	tm2 := snap2.Teammates[0]
	if tm2.PlayerID != "Steam|tm1|0" {
		t.Errorf("MEMORY LEAK / CORRUPTION: Teammate PlayerID mutated to %q", tm2.PlayerID)
	}
	if tm2.Stats.Score != 300 {
		t.Errorf("MEMORY LEAK / CORRUPTION: Teammate Stats.Score mutated to %d", tm2.Stats.Score)
	}
	if tm2.CurrentRank != nil && tm2.CurrentRank.RankName == "MUTATED_TM_RANK" {
		t.Errorf("MEMORY LEAK / CORRUPTION: Teammate CurrentRank mutated to %q", tm2.CurrentRank.RankName)
	}
	if tm2.Ranks != nil {
		if _, exists := tm2.Ranks["8888"]; exists {
			t.Errorf("MEMORY LEAK / CORRUPTION: Injected map key 8888 leaked into internal tracker ranks map!")
		}
	}
	if tm2.MatchupRecord != nil && tm2.MatchupRecord.WinsAsTeammate == 777777 {
		t.Errorf("MEMORY LEAK / CORRUPTION: Teammate MatchupRecord.WinsAsTeammate mutated to 777777")
	}

	// Verify Opponents slice & element isolation
	if len(snap2.Opponents) != 1 {
		t.Fatalf("MEMORY LEAK / CORRUPTION: snap2.Opponents length mutated to %d (slice shared!)", len(snap2.Opponents))
	}
	opp2 := snap2.Opponents[0]
	if opp2.PlayerID != "Epic|opp1|0" {
		t.Errorf("MEMORY LEAK / CORRUPTION: Opponent PlayerID mutated to %q", opp2.PlayerID)
	}
	if opp2.Stats.Score != 200 {
		t.Errorf("MEMORY LEAK / CORRUPTION: Opponent Stats.Score mutated to %d", opp2.Stats.Score)
	}

	// Verify Spectators slice isolation
	if len(snap2.Spectators) != 1 {
		t.Fatalf("MEMORY LEAK / CORRUPTION: snap2.Spectators length mutated to %d", len(snap2.Spectators))
	}
}

func TestTracker_Stress_ConcurrentMutations_NoCrossPollution(t *testing.T) {
	ctx := context.Background()
	store := newSQLiteTestStore(t)
	fetcher := newTestSkillFetcher()
	fetcher.skillsMap["Steam|p1|0"] = []rlapi.Skill{{Playlist: 11, Tier: 16, Division: 3, MMR: 1150.0}}

	cfg := config.PlayerTrackingConfig{
		Enabled:        true,
		LocalPlayerID:  "Steam|local|0",
		AutoFetchRanks: true,
	}
	tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
	if err != nil {
		t.Fatalf("NewTracker failed: %v", err)
	}
	defer func() { _ = tracker.Close() }()

	players := []statsapi.StatsPlayer{
		{Name: "LocalUser", PrimaryId: "Steam|local|0", TeamNum: 0, Score: 100},
		{Name: "Player1", PrimaryId: "Steam|p1|0", TeamNum: 1, Score: 200},
	}
	_ = tracker.OnUpdateState(ctx, "concurrent-mutation-guid", 11, players)
	time.Sleep(30 * time.Millisecond)

	const numGoroutines = 10
	const iterations = 500

	var wg sync.WaitGroup
	var corruptions atomic.Int64

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				snap := tracker.GetCurrentMatch()
				if snap == nil {
					corruptions.Add(1)
					continue
				}

				// Check that it's uncorrupted
				if snap.MatchGUID != "concurrent-mutation-guid" {
					corruptions.Add(1)
				}
				if len(snap.Opponents) != 1 || snap.Opponents[0].PlayerID != "Steam|p1|0" {
					corruptions.Add(1)
				}

				// Aggressively mutate returned snapshot
				snap.MatchGUID = fmt.Sprintf("corrupt-%d-%d", gid, i)
				snap.PlaylistID = gid * 1000
				snap.Opponents[0].PlayerID = fmt.Sprintf("corrupt-opp-%d", gid)
				snap.Opponents[0].Stats.Score = 999999
				if snap.Opponents[0].CurrentRank != nil {
					snap.Opponents[0].CurrentRank.Tier = 99
				}
				snap.Opponents = append(snap.Opponents, playertrack.LobbyPlayer{PlayerID: "evil"})
			}
		}(g)
	}

	wg.Wait()

	if corruptions.Load() != 0 {
		t.Fatalf("detected %d cross-goroutine memory mutations / corruptions!", corruptions.Load())
	}
}

// ============================================================================
// Empirical Challenge 3: Lifecycle Drain & Close Under Load
// ============================================================================

func TestTracker_Stress_LifecycleDrain_CloseUnderLoad(t *testing.T) {
	initialGoroutines := runtime.NumGoroutine()

	store := newSQLiteTestStore(t)
	fetcher := newStressSkillFetcher(25 * time.Millisecond) // In-flight latency

	cfg := config.PlayerTrackingConfig{
		Enabled:        true,
		LocalPlayerID:  "Steam|local|0",
		AutoFetchRanks: true,
	}
	tracker, err := playertrack.NewTracker(store, fetcher, cfg, config.AuthConfig{})
	if err != nil {
		t.Fatalf("NewTracker failed: %v", err)
	}

	const writers = 8
	const readers = 6

	var wg sync.WaitGroup
	ctx := context.Background()
	stopCh := make(chan struct{})

	var writeAttempts atomic.Int64
	var writeErrorsAfterClose atomic.Int64
	var readCount atomic.Int64

	// Launch writers hammering state updates
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			i := 0
			for {
				select {
				case <-stopCh:
					return
				default:
					i++
					writeAttempts.Add(1)
					players := []statsapi.StatsPlayer{
						{Name: "LocalUser", PrimaryId: "Steam|local|0", TeamNum: 0, Score: i},
						{Name: fmt.Sprintf("Opp_%d_%d", workerID, i%10), PrimaryId: fmt.Sprintf("Steam|opp_%d_%d|0", workerID, i%10), TeamNum: 1, Score: i / 2},
					}
					err := tracker.OnUpdateState(ctx, fmt.Sprintf("drain-guid-%d", workerID), 11, players)
					if err != nil {
						writeErrorsAfterClose.Add(1)
					}
					time.Sleep(1 * time.Millisecond)
				}
			}
		}(w)
	}

	// Launch readers hammering GetCurrentMatch & JSON serialization
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stopCh:
					return
				default:
					snap := tracker.GetCurrentMatch()
					if snap != nil {
						_, _ = json.Marshal(snap)
					}
					readCount.Add(1)
					time.Sleep(1 * time.Millisecond)
				}
			}
		}()
	}

	// Let the system run with heavy load and multiple in-flight RPCs
	time.Sleep(150 * time.Millisecond)

	// Trigger Tracker.Close() WHILE calls and RPCs are active!
	closeDone := make(chan error, 1)
	go func() {
		closeDone <- tracker.Close()
	}()

	// Tracker.Close() MUST return cleanly without deadlock within 5 seconds
	select {
	case err := <-closeDone:
		if err != nil {
			t.Errorf("tracker.Close() returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("DEADLOCK DETECTED: tracker.Close() failed to return within 5 seconds!")
	}

	// Stop workers
	close(stopCh)
	wg.Wait()

	// Verify idempotency: calling Close a second time must be a clean no-op
	if err := tracker.Close(); err != nil {
		t.Errorf("second Close() returned error: %v", err)
	}

	// Verify post-close behavior: OnUpdateState must cleanly reject new events
	postCloseErr := tracker.OnUpdateState(ctx, "post-close-guid", 11, []statsapi.StatsPlayer{
		{Name: "LocalUser", PrimaryId: "Steam|local|0", TeamNum: 0},
	})
	if postCloseErr == nil || !strings.Contains(postCloseErr.Error(), "closed") {
		t.Errorf("expected 'tracker is closed' error after Close(), got: %v", postCloseErr)
	}

	// Verify GetCurrentMatch still returns a valid (non-nil) snapshot post-close
	postSnap := tracker.GetCurrentMatch()
	if postSnap == nil {
		t.Errorf("GetCurrentMatch() returned nil after Close()")
	}

	// Await stabilization and verify no goroutine leaks
	var finalGoroutines int
	for attempt := 0; attempt < 20; attempt++ {
		time.Sleep(25 * time.Millisecond)
		finalGoroutines = runtime.NumGoroutine()
		if finalGoroutines <= initialGoroutines+2 {
			break
		}
	}

	// Check for goroutine leak
	if delta := finalGoroutines - initialGoroutines; delta > 4 {
		t.Errorf("GOROUTINE LEAK DETECTED: %d leaked goroutines (initial=%d, final=%d)", delta, initialGoroutines, finalGoroutines)
	}
}
