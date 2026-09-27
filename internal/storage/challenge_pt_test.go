package storage_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/storage"
)

// helper to create a test store of the given type
func makeTestStores(t *testing.T) []struct {
	name    string
	factory func(t *testing.T) storage.StateStore
} {
	t.Helper()
	return []struct {
		name    string
		factory func(t *testing.T) storage.StateStore
	}{
		{
			name: "SQLiteStore",
			factory: func(t *testing.T) storage.StateStore {
				dbPath := filepath.Join(t.TempDir(), "test_sqlite.db")
				s, err := storage.NewSQLiteStore(dbPath)
				if err != nil {
					t.Fatalf("failed to create sqlite store: %v", err)
				}
				t.Cleanup(func() { _ = s.Close() })
				return s
			},
		},
		{
			name: "JSONStore",
			factory: func(t *testing.T) storage.StateStore {
				jsonPath := filepath.Join(t.TempDir(), "test_state.json")
				s, err := storage.NewJSONStore(jsonPath)
				if err != nil {
					t.Fatalf("failed to create json store: %v", err)
				}
				t.Cleanup(func() { _ = s.Close() })
				return s
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Challenge 1: High-Concurrency Duplicate Match GUID Submissions (Zero Double-Counting)
// ---------------------------------------------------------------------------

func TestAdversarial_ConcurrentDuplicateMatchGUIDs(t *testing.T) {
	for _, storeTest := range makeTestStores(t) {
		t.Run(storeTest.name, func(t *testing.T) {
			store := storeTest.factory(t)
			ctx := context.Background()

			const (
				matchGUID  = "guid-concurrency-dup-001"
				playlistID = 11
				numWorkers = 80
			)

			// 6 players in a 3v3 match: 3 teammates (won), 3 opponents (lost)
			outcomes := []storage.PlayerOutcome{
				{PlayerID: "Steam|76561198000000010|0", Platform: "Steam", PlayerName: "Teammate1", IsTeammate: true, Won: true},
				{PlayerID: "Epic|00000000000000000000000000000011|0", Platform: "Epic", PlayerName: "Teammate2", IsTeammate: true, Won: true},
				{PlayerID: "Xbox|Teammate3|0", Platform: "Xbox", PlayerName: "Teammate3", IsTeammate: true, Won: true},
				{PlayerID: "Steam|76561198000000020|0", Platform: "Steam", PlayerName: "Opponent1", IsTeammate: false, Won: true},
				{PlayerID: "Epic|00000000000000000000000000000021|0", Platform: "Epic", PlayerName: "Opponent2", IsTeammate: false, Won: true},
				{PlayerID: "PS4|Opponent3|0", Platform: "PS4", PlayerName: "Opponent3", IsTeammate: false, Won: true},
			}

			var (
				successCount   int32
				duplicateCount int32
				unexpectedErrs []error
				errMu          sync.Mutex
				wg             sync.WaitGroup
				startBarrier   = make(chan struct{})
			)

			for i := 0; i < numWorkers; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-startBarrier // synchronize all goroutines to launch simultaneously

					err := store.RecordMatchResults(ctx, matchGUID, playlistID, outcomes)
					if err == nil {
						atomic.AddInt32(&successCount, 1)
					} else if errors.Is(err, storage.ErrMatchAlreadyProcessed) {
						atomic.AddInt32(&duplicateCount, 1)
					} else {
						errMu.Lock()
						unexpectedErrs = append(unexpectedErrs, err)
						errMu.Unlock()
					}
				}()
			}

			// Release all workers simultaneously
			close(startBarrier)
			wg.Wait()

			if len(unexpectedErrs) > 0 {
				t.Fatalf("unexpected errors occurred during concurrent submissions: %v", unexpectedErrs)
			}

			// Verify invariant: Exactly 1 worker succeeded, exactly numWorkers-1 detected duplicate
			if successCount != 1 {
				t.Errorf("expected exactly 1 success, got %d", successCount)
			}
			if duplicateCount != int32(numWorkers-1) {
				t.Errorf("expected exactly %d duplicates, got %d", numWorkers-1, duplicateCount)
			}

			// EMPIRICAL VERIFICATION OF ZERO DOUBLE-COUNTING:
			// Check each teammate: WinsAsTeammate MUST be exactly 1, TotalMatches MUST be exactly 1
			for i := 0; i < 3; i++ {
				pid := outcomes[i].PlayerID
				m, err := store.GetPlayerMatchup(ctx, pid, playlistID)
				if err != nil {
					t.Fatalf("failed to get matchup for %s: %v", pid, err)
				}
				if m.WinsAsTeammate != 1 {
					t.Errorf("DOUBLE-COUNTING DETECTED for teammate %s: WinsAsTeammate = %d (expected 1)", pid, m.WinsAsTeammate)
				}
				if m.LossesAsTeammate != 0 || m.WinsAsOpponent != 0 || m.LossesAsOpponent != 0 {
					t.Errorf("unexpected non-zero counters for teammate %s: %+v", pid, m)
				}
				if m.TotalMatches != 1 {
					t.Errorf("DOUBLE-COUNTING DETECTED for teammate %s: TotalMatches = %d (expected 1)", pid, m.TotalMatches)
				}
			}

			// Check each opponent: WinsAsOpponent MUST be exactly 1, TotalMatches MUST be exactly 1
			for i := 3; i < 6; i++ {
				pid := outcomes[i].PlayerID
				m, err := store.GetPlayerMatchup(ctx, pid, playlistID)
				if err != nil {
					t.Fatalf("failed to get matchup for %s: %v", pid, err)
				}
				if m.WinsAsOpponent != 1 {
					t.Errorf("DOUBLE-COUNTING DETECTED for opponent %s: WinsAsOpponent = %d (expected 1)", pid, m.WinsAsOpponent)
				}
				if m.WinsAsTeammate != 0 || m.LossesAsTeammate != 0 || m.LossesAsOpponent != 0 {
					t.Errorf("unexpected non-zero counters for opponent %s: %+v", pid, m)
				}
				if m.TotalMatches != 1 {
					t.Errorf("DOUBLE-COUNTING DETECTED for opponent %s: TotalMatches = %d (expected 1)", pid, m.TotalMatches)
				}
			}

			// Post-concurrency repeat: submit again sequentially 10 times
			for i := 0; i < 10; i++ {
				err := store.RecordMatchResults(ctx, matchGUID, playlistID, outcomes)
				if !errors.Is(err, storage.ErrMatchAlreadyProcessed) {
					t.Errorf("post-race submission #%d expected ErrMatchAlreadyProcessed, got %v", i, err)
				}
			}

			// Re-verify counters after sequential retries
			m, err := store.GetPlayerMatchup(ctx, outcomes[0].PlayerID, playlistID)
			if err != nil {
				t.Fatalf("failed to get matchup: %v", err)
			}
			if m.WinsAsTeammate != 1 || m.TotalMatches != 1 {
				t.Errorf("counter drifted after post-race retries: %+v", m)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Challenge 2: Multi-Match Interleaved Concurrency (Multiple Matches x Duplicate Racers)
// ---------------------------------------------------------------------------

func TestAdversarial_ConcurrentInterleavedMatches(t *testing.T) {
	for _, storeTest := range makeTestStores(t) {
		t.Run(storeTest.name, func(t *testing.T) {
			store := storeTest.factory(t)
			ctx := context.Background()

			const (
				numDistinctMatches = 10
				racersPerMatch     = 6
				playlistID         = 13
				corePlayerID       = "Steam|76561198000000999|0"
			)

			// Pre-upsert core player
			err := store.UpsertPlayer(ctx, &storage.PlayerRecord{
				PlayerID:   corePlayerID,
				Platform:   "Steam",
				PlayerName: "CorePlayer",
			})
			if err != nil {
				t.Fatalf("failed to upsert core player: %v", err)
			}

			var (
				wg           sync.WaitGroup
				startBarrier = make(chan struct{})
				successCount int32
				dupCount     int32
				errCount     int32
			)

			// 10 distinct matches. Each match is won as teammate.
			for mIdx := 0; mIdx < numDistinctMatches; mIdx++ {
				guid := fmt.Sprintf("guid-interleaved-match-%02d", mIdx)
				outcomes := []storage.PlayerOutcome{
					{PlayerID: corePlayerID, Platform: "Steam", PlayerName: "CorePlayer", IsTeammate: true, Won: true},
					{PlayerID: fmt.Sprintf("Bot-%02d", mIdx), Platform: "Unknown", PlayerName: "Bot", IsTeammate: false, Won: true},
				}

				for r := 0; r < racersPerMatch; r++ {
					wg.Add(1)
					go func(g string, outs []storage.PlayerOutcome) {
						defer wg.Done()
						<-startBarrier

						err := store.RecordMatchResults(ctx, g, playlistID, outs)
						if err == nil {
							atomic.AddInt32(&successCount, 1)
						} else if errors.Is(err, storage.ErrMatchAlreadyProcessed) {
							atomic.AddInt32(&dupCount, 1)
						} else {
							atomic.AddInt32(&errCount, 1)
						}
					}(guid, outcomes)
				}
			}

			close(startBarrier)
			wg.Wait()

			if errCount != 0 {
				t.Fatalf("encountered %d unexpected errors during interleaved run", errCount)
			}

			// Invariant: exactly 10 successes (1 per match) and 50 duplicate rejections
			if successCount != int32(numDistinctMatches) {
				t.Errorf("expected %d successes, got %d", numDistinctMatches, successCount)
			}
			expectedDups := int32(numDistinctMatches * (racersPerMatch - 1))
			if dupCount != expectedDups {
				t.Errorf("expected %d duplicate rejections, got %d", expectedDups, dupCount)
			}

			// Verify core player stats: exactly 10 wins as teammate, 10 total matches
			m, err := store.GetPlayerMatchup(ctx, corePlayerID, playlistID)
			if err != nil {
				t.Fatalf("failed to get core player matchup: %v", err)
			}
			if m.WinsAsTeammate != numDistinctMatches {
				t.Errorf("expected %d WinsAsTeammate, got %d", numDistinctMatches, m.WinsAsTeammate)
			}
			if m.TotalMatches != numDistinctMatches {
				t.Errorf("expected %d TotalMatches, got %d", numDistinctMatches, m.TotalMatches)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Challenge 3: Cross-Store Parity for ListPlayers (Sorting, Pagination, Tie-Breaking)
// ---------------------------------------------------------------------------

func TestAdversarial_CrossStore_ListPlayers_Parity(t *testing.T) {
	sqlitePath := filepath.Join(t.TempDir(), "parity_sqlite.db")
	sqlStore, err := storage.NewSQLiteStore(sqlitePath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer sqlStore.Close()

	jsonPath := filepath.Join(t.TempDir(), "parity_state.json")
	jsonStore, err := storage.NewJSONStore(jsonPath)
	if err != nil {
		t.Fatalf("failed to create json store: %v", err)
	}
	defer jsonStore.Close()

	ctx := context.Background()

	// Seed 60 players identically in both stores.
	// We use exact second-truncated timestamps so both stores operate on identical time values.
	baseTime := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 60; i++ {
		// Group 1 (0-19): distinct timestamps, 1 minute apart
		// Group 2 (20-39): identical timestamp (tests tie-breaking by PlayerID ASC)
		// Group 3 (40-59): descending timestamps
		var lastSeen time.Time
		if i < 20 {
			lastSeen = baseTime.Add(time.Duration(i) * time.Minute)
		} else if i < 40 {
			lastSeen = baseTime.Add(100 * time.Minute) // identical for all 20 players
		} else {
			lastSeen = baseTime.Add(time.Duration(200-i) * time.Minute)
		}

		p := &storage.PlayerRecord{
			PlayerID:    fmt.Sprintf("Player_%03d", i),
			Platform:    "Steam",
			PlayerName:  fmt.Sprintf("User_%03d", i),
			RanksJSON:   fmt.Sprintf(`{"11":{"tier":%d,"division":1}}`, i%20),
			FirstSeenAt: baseTime,
			LastSeenAt:  lastSeen,
		}

		if err := sqlStore.UpsertPlayer(ctx, p); err != nil {
			t.Fatalf("sqlite UpsertPlayer failed for %s: %v", p.PlayerID, err)
		}
		if err := jsonStore.UpsertPlayer(ctx, p); err != nil {
			t.Fatalf("json UpsertPlayer failed for %s: %v", p.PlayerID, err)
		}
	}

	paginationCases := []struct {
		name   string
		limit  int
		offset int
	}{
		{"FirstPage_10", 10, 0},
		{"SecondPage_10", 10, 10},
		{"ThirdPage_15", 15, 20},
		{"FullScan_60", 60, 0},
		{"OverScan_100", 100, 0},
		{"OffsetAtEnd", 10, 60},
		{"OffsetBeyondEnd", 10, 100},
		{"ZeroLimit", 0, 0},
		{"NegativeLimit_All", -1, 0},
		{"NegativeLimit_WithOffset", -1, 15},
		{"NegativeOffset_Normalized", 10, -5},
	}

	for _, pc := range paginationCases {
		t.Run(pc.name, func(t *testing.T) {
			sqlList, err := sqlStore.ListPlayers(ctx, pc.limit, pc.offset)
			if err != nil {
				t.Fatalf("sqlite ListPlayers failed: %v", err)
			}

			jsonList, err := jsonStore.ListPlayers(ctx, pc.limit, pc.offset)
			if err != nil {
				t.Fatalf("json ListPlayers failed: %v", err)
			}

			// 1. Length must be identical
			if len(sqlList) != len(jsonList) {
				t.Fatalf("length mismatch: sqlite=%d, json=%d", len(sqlList), len(jsonList))
			}

			// 2. Every single element must match in order and values
			for idx := range sqlList {
				sp := sqlList[idx]
				jp := jsonList[idx]

				if sp.PlayerID != jp.PlayerID {
					t.Errorf("item %d PlayerID mismatch: sqlite=%s, json=%s", idx, sp.PlayerID, jp.PlayerID)
				}
				if sp.PlayerName != jp.PlayerName {
					t.Errorf("item %d PlayerName mismatch: sqlite=%s, json=%s", idx, sp.PlayerName, jp.PlayerName)
				}
				if sp.Platform != jp.Platform {
					t.Errorf("item %d Platform mismatch: sqlite=%s, json=%s", idx, sp.Platform, jp.Platform)
				}
				if sp.RanksJSON != jp.RanksJSON {
					t.Errorf("item %d RanksJSON mismatch: sqlite=%s, json=%s", idx, sp.RanksJSON, jp.RanksJSON)
				}
				if sp.LastSeenAt.Unix() != jp.LastSeenAt.Unix() {
					t.Errorf("item %d LastSeenAt mismatch: sqlite=%v, json=%v", idx, sp.LastSeenAt, jp.LastSeenAt)
				}
				if sp.FirstSeenAt.Unix() != jp.FirstSeenAt.Unix() {
					t.Errorf("item %d FirstSeenAt mismatch: sqlite=%v, json=%v", idx, sp.FirstSeenAt, jp.FirstSeenAt)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Challenge 4: Cross-Store Parity for ListPlayerSummaries (Aggregations & Order)
// ---------------------------------------------------------------------------

func TestAdversarial_CrossStore_ListPlayerSummaries_Parity(t *testing.T) {
	sqlitePath := filepath.Join(t.TempDir(), "summary_sqlite.db")
	sqlStore, err := storage.NewSQLiteStore(sqlitePath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer sqlStore.Close()

	jsonPath := filepath.Join(t.TempDir(), "summary_state.json")
	jsonStore, err := storage.NewJSONStore(jsonPath)
	if err != nil {
		t.Fatalf("failed to create json store: %v", err)
	}
	defer jsonStore.Close()

	ctx := context.Background()

	// Seed 4 distinct players
	players := []string{"Summary_Alpha", "Summary_Beta", "Summary_Gamma", "Summary_Delta"}
	for _, pid := range players {
		p := &storage.PlayerRecord{
			PlayerID:   pid,
			Platform:   "Steam",
			PlayerName: "Name_" + pid,
		}
		_ = sqlStore.UpsertPlayer(ctx, p)
		_ = jsonStore.UpsertPlayer(ctx, p)
	}

	// Record matches with 1-second sleeps between matches to guarantee distinct Unix seconds.
	// This tests whether sorting order and aggregate counters achieve 100% parity when timestamps
	// have distinct second values.
	matches := []struct {
		guid     string
		playlist int
		outcomes []storage.PlayerOutcome
	}{
		{
			guid:     "match-seq-1",
			playlist: 11,
			outcomes: []storage.PlayerOutcome{
				{PlayerID: "Summary_Alpha", Platform: "Steam", IsTeammate: true, Won: true},
				{PlayerID: "Summary_Beta", Platform: "Steam", IsTeammate: false, Won: true},
			},
		},
		{
			guid:     "match-seq-2",
			playlist: 13,
			outcomes: []storage.PlayerOutcome{
				{PlayerID: "Summary_Alpha", Platform: "Steam", IsTeammate: false, Won: false},
				{PlayerID: "Summary_Gamma", Platform: "Steam", IsTeammate: true, Won: false},
			},
		},
		{
			guid:     "match-seq-3",
			playlist: 11,
			outcomes: []storage.PlayerOutcome{
				{PlayerID: "Summary_Beta", Platform: "Steam", IsTeammate: true, Won: true},
				{PlayerID: "Summary_Delta", Platform: "Steam", IsTeammate: false, Won: true},
			},
		},
	}

	for _, m := range matches {
		time.Sleep(1050 * time.Millisecond) // ensure distinct second boundary
		if err := sqlStore.RecordMatchResults(ctx, m.guid, m.playlist, m.outcomes); err != nil {
			t.Fatalf("sql RecordMatchResults: %v", err)
		}
		if err := jsonStore.RecordMatchResults(ctx, m.guid, m.playlist, m.outcomes); err != nil {
			t.Fatalf("json RecordMatchResults: %v", err)
		}
	}

	paginationCases := []struct {
		name   string
		limit  int
		offset int
	}{
		{"Page1_2", 2, 0},
		{"Page2_2", 2, 2},
		{"FullScan_4", 4, 0},
		{"OverScan_10", 10, 0},
		{"OffsetBeyond", 10, 10},
		{"ZeroLimit", 0, 0},
		{"NegativeLimit", -1, 0},
	}

	for _, pc := range paginationCases {
		t.Run(pc.name, func(t *testing.T) {
			sqlSums, err := sqlStore.ListPlayerSummaries(ctx, pc.limit, pc.offset)
			if err != nil {
				t.Fatalf("sql ListPlayerSummaries failed: %v", err)
			}
			jsonSums, err := jsonStore.ListPlayerSummaries(ctx, pc.limit, pc.offset)
			if err != nil {
				t.Fatalf("json ListPlayerSummaries failed: %v", err)
			}

			if len(sqlSums) != len(jsonSums) {
				t.Fatalf("summary length mismatch: sql=%d, json=%d", len(sqlSums), len(jsonSums))
			}

			for idx := range sqlSums {
				ss := sqlSums[idx]
				js := jsonSums[idx]

				if ss.PlayerID != js.PlayerID {
					t.Errorf("item %d PlayerID mismatch: sql=%s, json=%s", idx, ss.PlayerID, js.PlayerID)
				}
				if ss.TotalWinsAsTeammate != js.TotalWinsAsTeammate {
					t.Errorf("item %d (%s) WinsTeammate mismatch: sql=%d, json=%d", idx, ss.PlayerID, ss.TotalWinsAsTeammate, js.TotalWinsAsTeammate)
				}
				if ss.TotalLossesAsTeammate != js.TotalLossesAsTeammate {
					t.Errorf("item %d (%s) LossesTeammate mismatch: sql=%d, json=%d", idx, ss.PlayerID, ss.TotalLossesAsTeammate, js.TotalLossesAsTeammate)
				}
				if ss.TotalWinsAsOpponent != js.TotalWinsAsOpponent {
					t.Errorf("item %d (%s) WinsOpponent mismatch: sql=%d, json=%d", idx, ss.PlayerID, ss.TotalWinsAsOpponent, js.TotalWinsAsOpponent)
				}
				if ss.TotalLossesAsOpponent != js.TotalLossesAsOpponent {
					t.Errorf("item %d (%s) LossesOpponent mismatch: sql=%d, json=%d", idx, ss.PlayerID, ss.TotalLossesAsOpponent, js.TotalLossesAsOpponent)
				}
				if ss.TotalMatches != js.TotalMatches {
					t.Errorf("item %d (%s) TotalMatches mismatch: sql=%d, json=%d", idx, ss.PlayerID, ss.TotalMatches, js.TotalMatches)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Challenge 5: Cross-Store Parity for GetPlayerMatchup & GetPlayerMatchups
// ---------------------------------------------------------------------------

func TestAdversarial_CrossStore_MatchupQueries_Parity(t *testing.T) {
	sqlitePath := filepath.Join(t.TempDir(), "matchup_sqlite.db")
	sqlStore, _ := storage.NewSQLiteStore(sqlitePath)
	defer sqlStore.Close()

	jsonPath := filepath.Join(t.TempDir(), "matchup_state.json")
	jsonStore, _ := storage.NewJSONStore(jsonPath)
	defer jsonStore.Close()

	ctx := context.Background()

	// 1. Missing record behavior: non-existent player
	sqlM, err := sqlStore.GetPlayerMatchup(ctx, "nonexistent-player", 11)
	if err != nil {
		t.Fatalf("sql GetPlayerMatchup for missing record returned error: %v", err)
	}
	jsonM, err := jsonStore.GetPlayerMatchup(ctx, "nonexistent-player", 11)
	if err != nil {
		t.Fatalf("json GetPlayerMatchup for missing record returned error: %v", err)
	}

	if sqlM.TotalMatches != 0 || jsonM.TotalMatches != 0 {
		t.Errorf("expected 0 TotalMatches on missing record: sql=%d, json=%d", sqlM.TotalMatches, jsonM.TotalMatches)
	}
	if sqlM.PlayerID != jsonM.PlayerID || sqlM.PlaylistID != jsonM.PlaylistID {
		t.Errorf("missing matchup struct parity failure: sql=%+v, json=%+v", sqlM, jsonM)
	}

	// 2. Missing matchups slice: non-existent player
	sqlSlice, err := sqlStore.GetPlayerMatchups(ctx, "nonexistent-player")
	if err != nil {
		t.Fatalf("sql GetPlayerMatchups returned error: %v", err)
	}
	jsonSlice, err := jsonStore.GetPlayerMatchups(ctx, "nonexistent-player")
	if err != nil {
		t.Fatalf("json GetPlayerMatchups returned error: %v", err)
	}
	if len(sqlSlice) != 0 || len(jsonSlice) != 0 {
		t.Errorf("expected empty slices for missing player: sql=%d, json=%d", len(sqlSlice), len(jsonSlice))
	}

	// 3. Record matchups across multiple playlists in descending order of playlist ID
	// (Tests that GetPlayerMatchups correctly returns them ordered by playlist_id ASC in both stores)
	playlists := []int{34, 13, 11, 2}
	for _, pl := range playlists {
		guid := fmt.Sprintf("multi-pl-match-%d", pl)
		outcomes := []storage.PlayerOutcome{
			{PlayerID: "MultiPlayer1", Platform: "Steam", PlayerName: "MultiUser", IsTeammate: true, Won: true},
		}
		_ = sqlStore.RecordMatchResults(ctx, guid, pl, outcomes)
		_ = jsonStore.RecordMatchResults(ctx, guid, pl, outcomes)
	}

	sqlList, err := sqlStore.GetPlayerMatchups(ctx, "MultiPlayer1")
	if err != nil {
		t.Fatalf("sql GetPlayerMatchups: %v", err)
	}
	jsonList, err := jsonStore.GetPlayerMatchups(ctx, "MultiPlayer1")
	if err != nil {
		t.Fatalf("json GetPlayerMatchups: %v", err)
	}

	if len(sqlList) != len(jsonList) || len(sqlList) != 4 {
		t.Fatalf("matchup count mismatch: sql=%d, json=%d (expected 4)", len(sqlList), len(jsonList))
	}

	expectedOrder := []int{2, 11, 13, 34} // ASC
	for idx := range expectedOrder {
		if sqlList[idx].PlaylistID != expectedOrder[idx] {
			t.Errorf("sql index %d playlist ID not ASC: got %d, expected %d", idx, sqlList[idx].PlaylistID, expectedOrder[idx])
		}
		if jsonList[idx].PlaylistID != expectedOrder[idx] {
			t.Errorf("json index %d playlist ID not ASC: got %d, expected %d", idx, jsonList[idx].PlaylistID, expectedOrder[idx])
		}
	}
}

// ---------------------------------------------------------------------------
// Challenge 6: Deep Clone Mutation Defense (Caller Mutating Pointers / Slices)
// ---------------------------------------------------------------------------

func TestAdversarial_DeepCloneMutationDefense(t *testing.T) {
	for _, storeTest := range makeTestStores(t) {
		t.Run(storeTest.name, func(t *testing.T) {
			store := storeTest.factory(t)
			ctx := context.Background()

			now := time.Now().UTC().Truncate(time.Second)
			pOriginal := &storage.PlayerRecord{
				PlayerID:    "ImmutablePlayer_01",
				Platform:    "Steam",
				PlayerName:  "OriginalName",
				RanksJSON:   `{"11":{"tier":10,"division":1}}`,
				FirstSeenAt: now,
				LastSeenAt:  now,
			}

			if err := store.UpsertPlayer(ctx, pOriginal); err != nil {
				t.Fatalf("UpsertPlayer failed: %v", err)
			}

			// Subtest 1: Mutating pOriginal struct locally after UpsertPlayer must NOT affect store
			pOriginal.PlayerName = "LocallyMutatedName"
			pOriginal.Platform = "LocallyMutatedPlatform"
			pOriginal.RanksJSON = `{"hacked":true}`

			got1, err := store.GetPlayer(ctx, "ImmutablePlayer_01")
			if err != nil {
				t.Fatalf("GetPlayer failed: %v", err)
			}
			if got1.PlayerName != "OriginalName" || got1.Platform != "Steam" {
				t.Errorf("STORE CORRUPTED BY CALLER'S INPUT STRUCT MUTATION: got %+v", got1)
			}

			// Subtest 2: Mutating the pointer returned by GetPlayer must NOT affect store
			got1.PlayerName = "MutatedFromGetPlayer"
			got1.Platform = "MutatedPlatform"
			got1.RanksJSON = `{"corrupted":true}`

			got2, err := store.GetPlayer(ctx, "ImmutablePlayer_01")
			if err != nil {
				t.Fatalf("GetPlayer second call failed: %v", err)
			}
			if got2.PlayerName != "OriginalName" || got2.Platform != "Steam" || got2.RanksJSON != `{"11":{"tier":10,"division":1}}` {
				t.Errorf("STORE CORRUPTED BY MUTATING RETURNED GETPLAYER STRUCT: got %+v", got2)
			}

			// Subtest 3: Mutating items in slice returned by ListPlayers must NOT affect store
			list, err := store.ListPlayers(ctx, 10, 0)
			if err != nil {
				t.Fatalf("ListPlayers failed: %v", err)
			}
			if len(list) == 0 {
				t.Fatal("ListPlayers returned empty slice")
			}
			list[0].PlayerName = "MutatedFromListPlayers"

			got3, err := store.GetPlayer(ctx, "ImmutablePlayer_01")
			if err != nil {
				t.Fatalf("GetPlayer third call failed: %v", err)
			}
			if got3.PlayerName != "OriginalName" {
				t.Errorf("STORE CORRUPTED BY MUTATING SLICE FROM LISTPLAYERS: got %s", got3.PlayerName)
			}

			// Subtest 4: Record match results and mutate outcome slice locally
			outcomes := []storage.PlayerOutcome{
				{PlayerID: "ImmutablePlayer_01", Platform: "Steam", PlayerName: "OriginalName", IsTeammate: true, Won: true},
			}
			if err := store.RecordMatchResults(ctx, "guid-mut-001", 11, outcomes); err != nil {
				t.Fatalf("RecordMatchResults failed: %v", err)
			}

			// Caller modifies local outcomes
			outcomes[0].PlayerName = "MutatedOutcomeName"
			outcomes[0].IsTeammate = false
			outcomes[0].Won = false

			m1, err := store.GetPlayerMatchup(ctx, "ImmutablePlayer_01", 11)
			if err != nil {
				t.Fatalf("GetPlayerMatchup failed: %v", err)
			}
			if m1.WinsAsTeammate != 1 {
				t.Errorf("Matchup corrupted by local outcome mutation: WinsAsTeammate = %d", m1.WinsAsTeammate)
			}

			// Subtest 5: Mutating returned PlayerMatchup must NOT affect store
			m1.WinsAsTeammate = 9999
			m1.TotalMatches = 9999

			m2, err := store.GetPlayerMatchup(ctx, "ImmutablePlayer_01", 11)
			if err != nil {
				t.Fatalf("GetPlayerMatchup second call failed: %v", err)
			}
			if m2.WinsAsTeammate != 1 || m2.TotalMatches != 1 {
				t.Errorf("STORE CORRUPTED BY MUTATING RETURNED PLAYERMATCHUP: got %+v", m2)
			}

			// Subtest 6: Mutating returned PlayerSummary from ListPlayerSummaries
			sums, err := store.ListPlayerSummaries(ctx, 10, 0)
			if err != nil {
				t.Fatalf("ListPlayerSummaries failed: %v", err)
			}
			if len(sums) == 0 {
				t.Fatal("ListPlayerSummaries returned 0 records")
			}
			sums[0].TotalWinsAsTeammate = 8888
			sums[0].PlayerName = "MutatedSummaryName"

			sums2, err := store.ListPlayerSummaries(ctx, 10, 0)
			if err != nil {
				t.Fatalf("ListPlayerSummaries second call failed: %v", err)
			}
			if sums2[0].TotalWinsAsTeammate == 8888 || sums2[0].PlayerName == "MutatedSummaryName" {
				t.Errorf("STORE CORRUPTED BY MUTATING RETURNED PLAYERSUMMARY: got %+v", sums2[0])
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Challenge 7: Boundary Conditions, Special Characters & SQL Injection Resilience
// ---------------------------------------------------------------------------

func TestAdversarial_SpecialCharactersAndBoundaryResilience(t *testing.T) {
	for _, storeTest := range makeTestStores(t) {
		t.Run(storeTest.name, func(t *testing.T) {
			store := storeTest.factory(t)
			ctx := context.Background()

			adversarialPlayerIDs := []string{
				"Steam|76561198000000001|0",
				"Epic|0123456789abcdef0123456789abcdef|0",
				"PS4|Player-With-Dashes|0",
				"Xbox|Player With Spaces|0",
				"Steam|Player'; DROP TABLE players; --|0",
				"Steam|🔥RocketKing👑|0",
				"Steam|日本語プレイヤー|0",
				"Steam|quotes'and\"double|0",
			}

			// 1. Upsert all adversarial players
			for _, pid := range adversarialPlayerIDs {
				err := store.UpsertPlayer(ctx, &storage.PlayerRecord{
					PlayerID:   pid,
					Platform:   "Special",
					PlayerName: "Name_" + pid,
				})
				if err != nil {
					t.Fatalf("[%s] failed to upsert player with ID %q: %v", storeTest.name, pid, err)
				}

				// Retrieve immediately and verify exact string preservation
				p, err := store.GetPlayer(ctx, pid)
				if err != nil {
					t.Fatalf("[%s] failed to retrieve player with ID %q: %v", storeTest.name, pid, err)
				}
				if p.PlayerID != pid {
					t.Errorf("[%s] player ID corrupted: got %q, expected %q", storeTest.name, p.PlayerID, pid)
				}
			}

			// 2. Record match outcomes involving all adversarial player IDs
			outcomes := make([]storage.PlayerOutcome, 0, len(adversarialPlayerIDs))
			for i, pid := range adversarialPlayerIDs {
				outcomes = append(outcomes, storage.PlayerOutcome{
					PlayerID:   pid,
					Platform:   "Special",
					PlayerName: "Name_" + pid,
					IsTeammate: i%2 == 0,
					Won:        true,
				})
			}

			err := store.RecordMatchResults(ctx, "guid-adversarial-chars-001", 11, outcomes)
			if err != nil {
				t.Fatalf("[%s] failed to record match results with adversarial IDs: %v", storeTest.name, err)
			}

			// Verify each player's matchup record
			for i, pid := range adversarialPlayerIDs {
				m, err := store.GetPlayerMatchup(ctx, pid, 11)
				if err != nil {
					t.Fatalf("[%s] failed to get matchup for %q: %v", storeTest.name, pid, err)
				}
				if m.TotalMatches != 1 {
					t.Errorf("[%s] unexpected TotalMatches for %q: got %d, expected 1", storeTest.name, pid, m.TotalMatches)
				}
				if i%2 == 0 {
					if m.WinsAsTeammate != 1 {
						t.Errorf("[%s] expected WinsAsTeammate=1 for %q, got %d", storeTest.name, pid, m.WinsAsTeammate)
					}
				} else {
					if m.WinsAsOpponent != 1 {
						t.Errorf("[%s] expected WinsAsOpponent=1 for %q, got %d", storeTest.name, pid, m.WinsAsOpponent)
					}
				}
			}

			// 3. Boundary check: Empty / whitespace match GUID must fail with ErrInvalidGUID
			emptyGUIDs := []string{"", "   ", "\t\n"}
			for _, eg := range emptyGUIDs {
				err := store.RecordMatchResults(ctx, eg, 11, outcomes)
				if !errors.Is(err, storage.ErrInvalidGUID) {
					t.Errorf("[%s] expected ErrInvalidGUID for empty matchGUID %q, got %v", storeTest.name, eg, err)
				}
			}

			// 4. Boundary check: Outcomes containing empty / whitespace PlayerIDs must be ignored
			mixedOutcomes := []storage.PlayerOutcome{
				{PlayerID: "", Platform: "Steam", Won: true},
				{PlayerID: "   ", Platform: "Steam", Won: true},
				{PlayerID: "ValidPlayerAfterEmpty", Platform: "Steam", Won: true},
			}
			err = store.RecordMatchResults(ctx, "guid-mixed-empty-001", 11, mixedOutcomes)
			if err != nil {
				t.Fatalf("[%s] failed to record mixed outcomes with empty IDs: %v", storeTest.name, err)
			}

			validM, err := store.GetPlayerMatchup(ctx, "ValidPlayerAfterEmpty", 11)
			if err != nil || validM.TotalMatches != 1 {
				t.Errorf("[%s] valid player in mixed outcomes was not recorded properly: %+v, err=%v", storeTest.name, validM, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Challenge 8: Empirical Discovery — Cross-Store Parity Divergence on Empty PlayerID
// ---------------------------------------------------------------------------

func TestAdversarial_ParityDiscrepancy_EmptyPlayerID(t *testing.T) {
	sqlitePath := filepath.Join(t.TempDir(), "discrepancy_sql.db")
	sqlStore, _ := storage.NewSQLiteStore(sqlitePath)
	defer sqlStore.Close()

	jsonPath := filepath.Join(t.TempDir(), "discrepancy_json.json")
	jsonStore, _ := storage.NewJSONStore(jsonPath)
	defer jsonStore.Close()

	ctx := context.Background()

	// In SQLite: GetPlayerMatchup with empty string returns an error ("player_id cannot be empty")
	_, sqlMatchupErr := sqlStore.GetPlayerMatchup(ctx, "", 11)

	// In JSONStore: GetPlayerMatchup with empty string returns (&PlayerMatchup{}, nil) -> NO error!
	_, jsonMatchupErr := jsonStore.GetPlayerMatchup(ctx, "", 11)

	t.Logf("[Empirical Divergence] GetPlayerMatchup(\"\"): SQLite err=%v, JSONStore err=%v", sqlMatchupErr, jsonMatchupErr)

	// In SQLite: GetPlayerMatchups with empty string returns an error ("player_id cannot be empty")
	_, sqlMatchupsErr := sqlStore.GetPlayerMatchups(ctx, "")

	// In JSONStore: GetPlayerMatchups with empty string returns ([]*PlayerMatchup{}, nil) -> NO error!
	_, jsonMatchupsErr := jsonStore.GetPlayerMatchups(ctx, "")

	t.Logf("[Empirical Divergence] GetPlayerMatchups(\"\"): SQLite err=%v, JSONStore err=%v", sqlMatchupsErr, jsonMatchupsErr)

	// In SQLite, empty string returns error
	if sqlMatchupErr == nil {
		t.Errorf("expected SQLite to reject empty player_id, but it succeeded")
	}
	if sqlMatchupsErr == nil {
		t.Errorf("expected SQLite to reject empty player_id, but it succeeded")
	}

	// Document this divergence: SQLite enforces error on empty string while JSONStore swallows it.
	if (sqlMatchupErr == nil) != (jsonMatchupErr == nil) {
		t.Logf("CONFIRMED CROSS-STORE BEHAVIORAL GAP: GetPlayerMatchup error contract divergence")
	}
	if (sqlMatchupsErr == nil) != (jsonMatchupsErr == nil) {
		t.Logf("CONFIRMED CROSS-STORE BEHAVIORAL GAP: GetPlayerMatchups error contract divergence")
	}
}

// ---------------------------------------------------------------------------
// Challenge 9: Empirical Discovery — Cross-Store Subsecond Timestamp Ordering Divergence
// ---------------------------------------------------------------------------

func TestAdversarial_ParityDiscrepancy_SubsecondTimestampOrdering(t *testing.T) {
	sqlitePath := filepath.Join(t.TempDir(), "subsec_sql.db")
	sqlStore, _ := storage.NewSQLiteStore(sqlitePath)
	defer sqlStore.Close()

	jsonPath := filepath.Join(t.TempDir(), "subsec_json.json")
	jsonStore, _ := storage.NewJSONStore(jsonPath)
	defer jsonStore.Close()

	ctx := context.Background()

	// Ingest two matches in rapid succession (< 1 second apart) with different players
	// Match 1: Player A
	// Match 2: Player Z
	// Both occur in the same second:
	// - SQLite: Both get identical integer unix timestamps -> ORDER BY last_seen_at DESC, player_id ASC
	//   Since last_seen_at is tied, tie-breaker orders Player A before Player Z: [Player_A, Player_Z]
	// - JSONStore: Match 2 (Player Z) has microsecond timestamp > Match 1 (Player A) ->
	//   JSONStore orders by LastSeenAt DESC: Player Z before Player A: [Player_Z, Player_A]
	_ = sqlStore.RecordMatchResults(ctx, "subsec-m1", 11, []storage.PlayerOutcome{
		{PlayerID: "Player_A", Platform: "Steam", IsTeammate: true, Won: true},
	})
	_ = jsonStore.RecordMatchResults(ctx, "subsec-m1", 11, []storage.PlayerOutcome{
		{PlayerID: "Player_A", Platform: "Steam", IsTeammate: true, Won: true},
	})

	_ = sqlStore.RecordMatchResults(ctx, "subsec-m2", 11, []storage.PlayerOutcome{
		{PlayerID: "Player_Z", Platform: "Steam", IsTeammate: true, Won: true},
	})
	_ = jsonStore.RecordMatchResults(ctx, "subsec-m2", 11, []storage.PlayerOutcome{
		{PlayerID: "Player_Z", Platform: "Steam", IsTeammate: true, Won: true},
	})

	sqlList, _ := sqlStore.ListPlayers(ctx, 10, 0)
	jsonList, _ := jsonStore.ListPlayers(ctx, 10, 0)

	t.Logf("[Subsecond Timing Gap] SQLite order: [%s, %s]", sqlList[0].PlayerID, sqlList[1].PlayerID)
	t.Logf("[Subsecond Timing Gap] JSONStore order: [%s, %s]", jsonList[0].PlayerID, jsonList[1].PlayerID)

	if sqlList[0].PlayerID != jsonList[0].PlayerID {
		t.Logf("CONFIRMED SUB-SECOND PARITY DIVERGENCE: SQLite tie-breaks same-second by player_id ASC ([%s, %s]) while JSONStore sorts by microsecond precision ([%s, %s])",
			sqlList[0].PlayerID, sqlList[1].PlayerID, jsonList[0].PlayerID, jsonList[1].PlayerID)
	}

}
