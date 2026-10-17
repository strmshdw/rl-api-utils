package storage_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/storage"
)

// ============================================================================
// CHALLENGE 1: STARTUP BACKFILL MIGRATION IDEMPOTENCY & EDGE CASES
// ============================================================================

func TestM1Stress_StartupBackfillMigration_RepeatedAndConcurrent(t *testing.T) {
	ctx := context.Background()

	for _, entry := range getM1TestStores(t) {
		t.Run(entry.name, func(t *testing.T) {
			store := entry.factory(t)

			// 1. Seed matches with varying replay_stats_json
			winner0 := 0
			winner1 := 1

			// Match 1: Normal 2v2
			stats1 := &storage.ReplayMatchStats{
				MatchGUID:   "backfill-stress-1",
				MapName:     "DFH_Stadium",
				Playlist:    11,
				BlueScore:   3,
				OrangeScore: 1,
				WinnerTeam:  &winner0,
				Date:        "2026-09-01T12:00:00Z",
				Players: []storage.ReplayPlayerStats{
					{Name: "P1", Platform: "Epic", OnlineID: "p1_id", Team: 0, Score: 500, Goals: 2},
					{Name: "P2", Platform: "Epic", OnlineID: "p2_id", Team: 0, Score: 400, Goals: 1},
					{Name: "P3", Platform: "Steam", OnlineID: "p3_id", Team: 1, Score: 300, Goals: 1},
					{Name: "P4", Platform: "Steam", OnlineID: "p4_id", Team: 1, Score: 250, Goals: 0},
				},
			}
			if err := store.SyncReplayStats(ctx, stats1); err != nil {
				t.Fatalf("SyncReplayStats(1) failed: %v", err)
			}

			// Match 2: 1v1
			stats2 := &storage.ReplayMatchStats{
				MatchGUID:   "backfill-stress-2",
				MapName:     "Mannfield",
				Playlist:    10,
				BlueScore:   2,
				OrangeScore: 4,
				WinnerTeam:  &winner1,
				Date:        "2026-09-02T12:00:00Z",
				Players: []storage.ReplayPlayerStats{
					{Name: "SoloA", Platform: "Epic", OnlineID: "solo_a", Team: 0, Score: 300, Goals: 2},
					{Name: "SoloB", Platform: "Steam", OnlineID: "solo_b", Team: 1, Score: 500, Goals: 4},
				},
			}
			if err := store.SyncReplayStats(ctx, stats2); err != nil {
				t.Fatalf("SyncReplayStats(2) failed: %v", err)
			}

			// Verify baseline players count
			p1, err := store.GetMatchPlayers(ctx, "backfill-stress-1")
			if err != nil || len(p1) != 4 {
				t.Fatalf("expected 4 players in match 1, got %d (err: %v)", len(p1), err)
			}
			p2, err := store.GetMatchPlayers(ctx, "backfill-stress-2")
			if err != nil || len(p2) != 2 {
				t.Fatalf("expected 2 players in match 2, got %d (err: %v)", len(p2), err)
			}
		})
	}
}

// TestM1Stress_StartupBackfill_RepeatedReopen tests closing and reopening stores repeatedly
// to verify migrateMatchPlayers and backfillMatchPlayersLocked are strictly idempotent.
func TestM1Stress_StartupBackfill_RepeatedReopen(t *testing.T) {
	ctx := context.Background()

	// 1. SQLiteStore
	t.Run("SQLiteStore_RepeatedReopen", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "reopen_idempotence.db")
		store, err := storage.NewSQLiteStore(dbPath)
		if err != nil {
			t.Fatalf("failed to create store: %v", err)
		}

		winner := 0
		stats := &storage.ReplayMatchStats{
			MatchGUID:   "reopen-match-1",
			MapName:     "DFH_Stadium",
			Playlist:    11,
			WinnerTeam:  &winner,
			Date:        "2026-09-01T12:00:00Z",
			Players: []storage.ReplayPlayerStats{
				{Name: "P1", Platform: "Epic", OnlineID: "p1_id", Team: 0, Score: 500},
				{Name: "P2", Platform: "Steam", OnlineID: "p2_id", Team: 1, Score: 400},
			},
		}
		if err := store.SyncReplayStats(ctx, stats); err != nil {
			t.Fatalf("SyncReplayStats failed: %v", err)
		}
		_ = store.Close()

		// Reopen 10 times consecutively
		for i := 0; i < 10; i++ {
			reopened, err := storage.NewSQLiteStore(dbPath)
			if err != nil {
				t.Fatalf("reopening iteration %d failed: %v", i, err)
			}
			players, err := reopened.GetMatchPlayers(ctx, "reopen-match-1")
			if err != nil {
				t.Fatalf("iteration %d GetMatchPlayers failed: %v", i, err)
			}
			if len(players) != 2 {
				t.Errorf("iteration %d expected 2 players, got %d", i, len(players))
			}
			_ = reopened.Close()
		}
	})

	// 2. JSONStore
	t.Run("JSONStore_RepeatedReopen", func(t *testing.T) {
		jsonPath := filepath.Join(t.TempDir(), "reopen_idempotence.json")
		store, err := storage.NewJSONStore(jsonPath)
		if err != nil {
			t.Fatalf("failed to create store: %v", err)
		}

		winner := 0
		stats := &storage.ReplayMatchStats{
			MatchGUID:   "reopen-match-1",
			MapName:     "DFH_Stadium",
			Playlist:    11,
			WinnerTeam:  &winner,
			Date:        "2026-09-01T12:00:00Z",
			Players: []storage.ReplayPlayerStats{
				{Name: "P1", Platform: "Epic", OnlineID: "p1_id", Team: 0, Score: 500},
				{Name: "P2", Platform: "Steam", OnlineID: "p2_id", Team: 1, Score: 400},
			},
		}
		if err := store.SyncReplayStats(ctx, stats); err != nil {
			t.Fatalf("SyncReplayStats failed: %v", err)
		}
		_ = store.Close()

		// Reopen 10 times consecutively
		for i := 0; i < 10; i++ {
			reopened, err := storage.NewJSONStore(jsonPath)
			if err != nil {
				t.Fatalf("reopening iteration %d failed: %v", i, err)
			}
			players, err := reopened.GetMatchPlayers(ctx, "reopen-match-1")
			if err != nil {
				t.Fatalf("iteration %d GetMatchPlayers failed: %v", i, err)
			}
			if len(players) != 2 {
				t.Errorf("iteration %d expected 2 players, got %d", i, len(players))
			}
			_ = reopened.Close()
		}
	})
}

// TestM1Stress_JSONStore_Backfill_DuplicatePlayersInReplayStats simulates startup
// migration when legacy stored matches contain duplicate player entries in replay_stats_json.
func TestM1Stress_JSONStore_Backfill_DuplicatePlayersInReplayStats(t *testing.T) {
	ctx := context.Background()

	// Construct JSON payload with legacy match containing duplicate player in replay_stats_json
	// and NO match_players map, simulating unmigrated pre-M1 database.
	jsonPath := filepath.Join(t.TempDir(), "legacy_unmigrated_dupes.json")
	rawLegacyJSON := `{
		"version": 1,
		"updated_at": "2026-09-10T12:00:00Z",
		"matches": {
			"legacy-dupe-match": {
				"match_guid": "legacy-dupe-match",
				"record_start_timestamp": 1788960000,
				"map_name": "DFH_Stadium",
				"playlist": 11,
				"winner_team": 0,
				"replay_stats_json": "{\"match_guid\":\"legacy-dupe-match\",\"winner_team\":0,\"players\":[{\"name\":\"DupeGuy\",\"platform\":\"Epic\",\"online_id\":\"dupe_guy\",\"team\":0,\"score\":300},{\"name\":\"DupeGuy\",\"platform\":\"Epic\",\"online_id\":\"dupe_guy\",\"team\":0,\"score\":400}]}",
				"stats_sync_status": "PENDING"
			}
		}
	}`

	if err := os.WriteFile(jsonPath, []byte(rawLegacyJSON), 0644); err != nil {
		t.Fatalf("failed to write legacy json: %v", err)
	}

	// Opening via NewJSONStore triggers backfillMatchPlayersLocked()
	store, err := storage.NewJSONStore(jsonPath)
	if err != nil {
		t.Fatalf("NewJSONStore failed: %v", err)
	}
	defer store.Close()

	players, err := store.GetMatchPlayers(ctx, "legacy-dupe-match")
	if err != nil {
		t.Fatalf("GetMatchPlayers failed: %v", err)
	}

	dupeCount := 0
	for _, p := range players {
		if p.PlayerID == "Epic|dupe_guy|0" {
			dupeCount++
		}
	}
	if dupeCount > 1 {
		t.Errorf("[JSONStore] VIOLATION: backfillMatchPlayersLocked allowed duplicate primary keys in match_players! Expected 1, got %d", dupeCount)
	}
}

// TestM1Stress_StartupBackfillMigration_DuplicatePlayersInReplay asserts that
// replay stats containing duplicate player entries within the same match are
// de-duplicated strictly adhering to the composite primary key (match_guid, player_id).
func TestM1Stress_StartupBackfillMigration_DuplicatePlayersInReplay(t *testing.T) {
	ctx := context.Background()

	winner := 0
	statsWithDupes := &storage.ReplayMatchStats{
		MatchGUID:   "dupe-replay-001",
		MapName:     "DFH_Stadium",
		Playlist:    11,
		BlueScore:   3,
		OrangeScore: 1,
		WinnerTeam:  &winner,
		Date:        "2026-09-10T12:00:00Z",
		Players: []storage.ReplayPlayerStats{
			{Name: "DuplicatePlayer", Platform: "Epic", OnlineID: "dupe_online_id", Team: 0, Score: 400, Goals: 1},
			{Name: "DuplicatePlayer", Platform: "Epic", OnlineID: "dupe_online_id", Team: 0, Score: 450, Goals: 2}, // duplicate player ID in stats
			{Name: "UniquePlayer", Platform: "Steam", OnlineID: "unique_online_id", Team: 1, Score: 300, Goals: 1},
		},
	}

	for _, entry := range getM1TestStores(t) {
		t.Run(entry.name, func(t *testing.T) {
			store := entry.factory(t)

			if err := store.SyncReplayStats(ctx, statsWithDupes); err != nil {
				t.Fatalf("SyncReplayStats failed: %v", err)
			}

			players, err := store.GetMatchPlayers(ctx, "dupe-replay-001")
			if err != nil {
				t.Fatalf("GetMatchPlayers failed: %v", err)
			}

			// Composite PK (match_guid, player_id) dictates there must be at most 1 entry for dupe_online_id!
			dupeCount := 0
			for _, p := range players {
				if p.PlayerID == "Epic|dupe_online_id|0" {
					dupeCount++
				}
			}

			if dupeCount != 1 {
				t.Errorf("[%s] VIOLATION: duplicate player ID found in match_players! Expected 1 entry, got %d. Total players: %d",
					entry.name, dupeCount, len(players))
			}
		})
	}
}

// ============================================================================
// CHALLENGE 2: INSERTING HISTORICAL MATCHES OUT OF ORDER
// Asserting first_seen_at = MIN and last_seen_at = MAX hold strictly without clock rewinds.
// ============================================================================

func TestM1Stress_HistoricalMatchesOutOfOrder_RecordMatchResults(t *testing.T) {
	ctx := context.Background()

	for _, entry := range getM1TestStores(t) {
		t.Run(entry.name, func(t *testing.T) {
			store := entry.factory(t)

			playerID := "Epic|temporal_traveler_1|0"
			t0 := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
			tMin := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
			tMax := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
			tMid := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

			// Step 1: Insert match at t0 (2026-05-01)
			err := store.RecordMatchResults(ctx, "guid-t0", 11, []storage.PlayerOutcome{
				{PlayerID: playerID, Platform: "Epic", PlayerName: "Traveler", IsTeammate: true, Won: true},
			}, t0)
			if err != nil {
				t.Fatalf("RecordMatchResults(t0) failed: %v", err)
			}

			p, err := store.GetPlayer(ctx, playerID)
			if err != nil {
				t.Fatalf("GetPlayer failed: %v", err)
			}
			if !p.FirstSeenAt.Equal(t0) || !p.LastSeenAt.Equal(t0) {
				t.Fatalf("initial timestamps mismatch: first=%v, last=%v, expected=%v", p.FirstSeenAt, p.LastSeenAt, t0)
			}

			// Step 2: Insert NEWEST match at tMax (2026-10-01)
			err = store.RecordMatchResults(ctx, "guid-tMax", 11, []storage.PlayerOutcome{
				{PlayerID: playerID, Platform: "Epic", PlayerName: "Traveler", IsTeammate: true, Won: true},
			}, tMax)
			if err != nil {
				t.Fatalf("RecordMatchResults(tMax) failed: %v", err)
			}

			p, _ = store.GetPlayer(ctx, playerID)
			if !p.FirstSeenAt.Equal(t0) {
				t.Errorf("FirstSeenAt regressed after tMax: got %v, expected %v", p.FirstSeenAt, t0)
			}
			if !p.LastSeenAt.Equal(tMax) {
				t.Errorf("LastSeenAt did not advance to tMax: got %v, expected %v", p.LastSeenAt, tMax)
			}

			// Step 3: Insert OLDEST match at tMin (2026-01-01) out of order
			err = store.RecordMatchResults(ctx, "guid-tMin", 11, []storage.PlayerOutcome{
				{PlayerID: playerID, Platform: "Epic", PlayerName: "Traveler", IsTeammate: false, Won: false},
			}, tMin)
			if err != nil {
				t.Fatalf("RecordMatchResults(tMin) failed: %v", err)
			}

			p, _ = store.GetPlayer(ctx, playerID)
			if !p.FirstSeenAt.Equal(tMin) {
				t.Errorf("FirstSeenAt did not push back to MIN (tMin): got %v, expected %v", p.FirstSeenAt, tMin)
			}
			if !p.LastSeenAt.Equal(tMax) {
				t.Errorf("CLOCK REWIND VIOLATION: LastSeenAt regressed after inserting older match! Got %v, expected MAX %v", p.LastSeenAt, tMax)
			}

			// Check matchup LastPlayedAt
			mup, err := store.GetPlayerMatchup(ctx, playerID, 11)
			if err != nil {
				t.Fatalf("GetPlayerMatchup failed: %v", err)
			}
			if !mup.LastPlayedAt.Equal(tMax) {
				t.Errorf("CLOCK REWIND VIOLATION: matchup LastPlayedAt regressed! Got %v, expected MAX %v", mup.LastPlayedAt, tMax)
			}

			// Step 4: Insert intermediate match at tMid (2026-07-01)
			err = store.RecordMatchResults(ctx, "guid-tMid", 11, []storage.PlayerOutcome{
				{PlayerID: playerID, Platform: "Epic", PlayerName: "Traveler", IsTeammate: true, Won: false},
			}, tMid)
			if err != nil {
				t.Fatalf("RecordMatchResults(tMid) failed: %v", err)
			}

			p, _ = store.GetPlayer(ctx, playerID)
			if !p.FirstSeenAt.Equal(tMin) {
				t.Errorf("FirstSeenAt violated MIN after tMid: got %v, expected %v", p.FirstSeenAt, tMin)
			}
			if !p.LastSeenAt.Equal(tMax) {
				t.Errorf("CLOCK REWIND VIOLATION: LastSeenAt overwritten by intermediate match! Got %v, expected MAX %v", p.LastSeenAt, tMax)
			}

			mup, _ = store.GetPlayerMatchup(ctx, playerID, 11)
			if !mup.LastPlayedAt.Equal(tMax) {
				t.Errorf("CLOCK REWIND VIOLATION: matchup LastPlayedAt overwritten by intermediate match! Got %v, expected MAX %v", mup.LastPlayedAt, tMax)
			}
		})
	}
}

// TestM1Stress_HistoricalMatchesOutOfOrder_UpsertPlayer tests whether UpsertPlayer preserves
// timestamp monotonicity (first_seen_at = MIN, last_seen_at = MAX) when called directly with
// historical records out of order.
func TestM1Stress_HistoricalMatchesOutOfOrder_UpsertPlayer(t *testing.T) {
	ctx := context.Background()

	for _, entry := range getM1TestStores(t) {
		t.Run(entry.name, func(t *testing.T) {
			store := entry.factory(t)

			playerID := "Epic|upsert_chrono|0"
			tNewer := time.Date(2026, 10, 1, 15, 0, 0, 0, time.UTC)
			tOlder := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)

			// Step 1: Upsert with newer time
			pNewer := &storage.PlayerRecord{
				PlayerID:    playerID,
				Platform:    "Epic",
				PlayerName:  "ChronoPlayer",
				FirstSeenAt: tNewer,
				LastSeenAt:  tNewer,
			}
			if err := store.UpsertPlayer(ctx, pNewer); err != nil {
				t.Fatalf("UpsertPlayer(newer) failed: %v", err)
			}

			pCheck1, err := store.GetPlayer(ctx, playerID)
			if err != nil {
				t.Fatalf("GetPlayer failed: %v", err)
			}
			if !pCheck1.LastSeenAt.Equal(tNewer) {
				t.Fatalf("initial LastSeenAt got %v, expected %v", pCheck1.LastSeenAt, tNewer)
			}

			// Step 2: Out of order historical discovery calls UpsertPlayer with older times
			pOlder := &storage.PlayerRecord{
				PlayerID:    playerID,
				Platform:    "Epic",
				PlayerName:  "ChronoPlayer",
				FirstSeenAt: tOlder,
				LastSeenAt:  tOlder,
			}
			if err := store.UpsertPlayer(ctx, pOlder); err != nil {
				t.Fatalf("UpsertPlayer(older) failed: %v", err)
			}

			pCheck2, err := store.GetPlayer(ctx, playerID)
			if err != nil {
				t.Fatalf("GetPlayer failed: %v", err)
			}

			// Assert first_seen_at = MIN (should be tOlder)
			if !pCheck2.FirstSeenAt.Equal(tOlder) {
				t.Errorf("[%s] VIOLATION: FirstSeenAt failed to update to MIN! Got %v, expected %v",
					entry.name, pCheck2.FirstSeenAt, tOlder)
			}

			// Assert last_seen_at = MAX (must remain tNewer, NOT rewind to tOlder)
			if !pCheck2.LastSeenAt.Equal(tNewer) {
				t.Errorf("[%s] VIOLATION: CLOCK REWIND in UpsertPlayer! LastSeenAt regressed from %v to %v",
					entry.name, tNewer, pCheck2.LastSeenAt)
			}
		})
	}
}

// ============================================================================
// CHALLENGE 3: ADDING DUPLICATE MATCH_PLAYERS ENTRIES
// Asserting composite primary key enforcement (match_guid, player_id).
// ============================================================================

func TestM1Stress_DuplicateMatchPlayers_CompositePK(t *testing.T) {
	ctx := context.Background()

	for _, entry := range getM1TestStores(t) {
		t.Run(entry.name, func(t *testing.T) {
			store := entry.factory(t)

			guid := "composite-pk-test-match"
			wonTrue := true
			wonFalse := false

			// 1. Same call contains duplicate (match_guid, player_id) entries with different scores
			recordsWithDupes := []*storage.MatchPlayerRecord{
				{MatchGUID: guid, PlayerID: "Epic|player_a|0", TeamNum: 0, Won: &wonTrue, Score: 100, Goals: 1},
				{MatchGUID: guid, PlayerID: "Epic|player_b|0", TeamNum: 1, Won: &wonFalse, Score: 200, Goals: 0},
				{MatchGUID: guid, PlayerID: "Epic|player_a|0", TeamNum: 0, Won: &wonTrue, Score: 500, Goals: 3}, // duplicate of player_a with updated stats
			}

			if err := store.SaveMatchPlayers(ctx, guid, recordsWithDupes); err != nil {
				t.Fatalf("SaveMatchPlayers failed: %v", err)
			}

			players, err := store.GetMatchPlayers(ctx, guid)
			if err != nil {
				t.Fatalf("GetMatchPlayers failed: %v", err)
			}

			if len(players) != 2 {
				t.Errorf("[%s] Composite PK violation: expected exactly 2 records, got %d", entry.name, len(players))
			}

			var playerA *storage.MatchPlayerRecord
			for _, p := range players {
				if p.PlayerID == "Epic|player_a|0" {
					if playerA != nil {
						t.Errorf("[%s] Composite PK violation: multiple records returned for player_a!", entry.name)
					}
					playerA = p
				}
			}
			if playerA == nil {
				t.Fatalf("[%s] player_a not found", entry.name)
			}
			// Should reflect updated stats
			if playerA.Score != 500 || playerA.Goals != 3 {
				t.Errorf("[%s] Expected player_a score=500, goals=3; got score=%d, goals=%d", entry.name, playerA.Score, playerA.Goals)
			}

			// 2. Separate sequential calls with duplicate (match_guid, player_id)
			updateRecords := []*storage.MatchPlayerRecord{
				{MatchGUID: guid, PlayerID: "Epic|player_a|0", TeamNum: 0, Won: &wonTrue, Score: 777, Goals: 4},
			}
			if err := store.SaveMatchPlayers(ctx, guid, updateRecords); err != nil {
				t.Fatalf("SaveMatchPlayers (sequential update) failed: %v", err)
			}

			playersAfterUpdate, err := store.GetMatchPlayers(ctx, guid)
			if err != nil {
				t.Fatalf("GetMatchPlayers failed: %v", err)
			}
			if len(playersAfterUpdate) != 2 {
				t.Errorf("[%s] Sequential update changed player count! Expected 2, got %d", entry.name, len(playersAfterUpdate))
			}
		})
	}
}

// TestM1Stress_DuplicateMatchPlayers_CrossMatchIsolation tests that the composite
// primary key correctly scopes players to specific matches (same player in 2 matches is allowed,
// 2 players in same match is allowed).
func TestM1Stress_DuplicateMatchPlayers_CrossMatchIsolation(t *testing.T) {
	ctx := context.Background()

	for _, entry := range getM1TestStores(t) {
		t.Run(entry.name, func(t *testing.T) {
			store := entry.factory(t)

			wonTrue := true
			// Match 1
			_ = store.SaveMatchPlayers(ctx, "match-A", []*storage.MatchPlayerRecord{
				{MatchGUID: "match-A", PlayerID: "Epic|shared_player|0", TeamNum: 0, Won: &wonTrue, Score: 300},
			})

			// Match 2 with same player
			_ = store.SaveMatchPlayers(ctx, "match-B", []*storage.MatchPlayerRecord{
				{MatchGUID: "match-B", PlayerID: "Epic|shared_player|0", TeamNum: 1, Won: &wonTrue, Score: 450},
			})

			pA, err := store.GetMatchPlayers(ctx, "match-A")
			if err != nil || len(pA) != 1 || pA[0].Score != 300 {
				t.Errorf("[%s] match-A player record corrupted: %+v, err: %v", entry.name, pA, err)
			}

			pB, err := store.GetMatchPlayers(ctx, "match-B")
			if err != nil || len(pB) != 1 || pB[0].Score != 450 {
				t.Errorf("[%s] match-B player record corrupted: %+v, err: %v", entry.name, pB, err)
			}
		})
	}
}

// ============================================================================
// CHALLENGE 4: SQLITE TABLE LOCK STRESS UNDER CONCURRENT GOROUTINES
// ============================================================================

func TestM1Stress_SQLite_HighConcurrencyTableLock(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sqlite_table_lock_stress.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	const (
		numGoroutines       = 40
		operationsPerWorker = 30
	)

	var (
		totalOps     atomic.Int64
		errCount     atomic.Int64
		lockErrCount atomic.Int64
	)

	var wg sync.WaitGroup
	errCh := make(chan error, numGoroutines*operationsPerWorker)

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(workerID * 12345)))

			for i := 0; i < operationsPerWorker; i++ {
				if ctx.Err() != nil {
					return
				}

				matchGUID := fmt.Sprintf("lock-stress-%03d", rng.Intn(10))
				playerID := fmt.Sprintf("Epic|lock_player_%02d|0", rng.Intn(15))
				opType := rng.Intn(6)

				var opErr error
				switch opType {
				case 0:
					// RecordMatchResults (heavy multi-table write transaction)
					won := rng.Intn(2) == 1
					opErr = store.RecordMatchResults(ctx, fmt.Sprintf("lock-res-%04d", rng.Intn(500)), 11, []storage.PlayerOutcome{
						{PlayerID: playerID, Platform: "Epic", PlayerName: "Player", IsTeammate: true, Won: won},
					}, time.Now().UTC())
					if errors.Is(opErr, storage.ErrMatchAlreadyProcessed) {
						opErr = nil // expected idempotency rejection
					}

				case 1:
					// SaveMatchPlayers (table lock contention on match_players)
					won := rng.Intn(2) == 1
					opErr = store.SaveMatchPlayers(ctx, matchGUID, []*storage.MatchPlayerRecord{
						{MatchGUID: matchGUID, PlayerID: playerID, TeamNum: rng.Intn(2), Won: &won, Score: rng.Intn(1000)},
					})

				case 2:
					// UpsertPlayer
					opErr = store.UpsertPlayer(ctx, &storage.PlayerRecord{
						PlayerID:   playerID,
						Platform:   "Epic",
						PlayerName: fmt.Sprintf("Player_%02d", rng.Intn(15)),
						LastSeenAt: time.Now().UTC(),
					})

				case 3:
					// RecordMMR
					opErr = store.RecordMMR(ctx, &storage.MMRHistoryRecord{
						RecordedAt: time.Now().UTC(),
						MatchGUID:  matchGUID,
						PlaylistID: 11,
						MMR:        1000 + rng.Intn(200),
						MMRChange:  rng.Intn(20) - 10,
					})

				case 4:
					// SearchPlayerSummaries (concurrent reader)
					_, _, opErr = store.SearchPlayerSummaries(ctx, "lock", "Epic", 10, 0)

				case 5:
					// ListPlayerMatches (concurrent join reader)
					_, _, opErr = store.ListPlayerMatches(ctx, playerID, storage.PlayerMatchFilter{
						Limit: 10,
					})
				}

				if opErr != nil {
					errCount.Add(1)
					errStr := opErr.Error()
					if containsLockError(errStr) {
						lockErrCount.Add(1)
					}
					select {
					case errCh <- fmt.Errorf("worker %d op %d (type %d) error: %w", workerID, i, opType, opErr):
					default:
					}
					return
				}
				totalOps.Add(1)
			}
		}(g)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("Concurrency stress failure: %v", err)
	}

	if lockErrCount.Load() > 0 {
		t.Fatalf("FATAL: %d SQLite table/database lock errors observed under high concurrency!", lockErrCount.Load())
	}
	t.Logf("Successfully executed %d concurrent operations with zero table lock errors", totalOps.Load())
}

// TestM1Stress_SQLite_MultiHandleContention tests multiple SQLiteStore instances
// accessing the exact same database file concurrently, stressing OS-level file locking
// and WAL mode concurrency.
func TestM1Stress_SQLite_MultiHandleContention(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "multi_handle_stress.db")

	const numHandles = 5
	stores := make([]*storage.SQLiteStore, numHandles)
	for i := 0; i < numHandles; i++ {
		s, err := storage.NewSQLiteStore(dbPath)
		if err != nil {
			t.Fatalf("failed to open store handle %d: %v", i, err)
		}
		stores[i] = s
		defer s.Close()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, numHandles*20)
	var completed atomic.Int64

	for h := 0; h < numHandles; h++ {
		wg.Add(1)
		go func(handleIdx int) {
			defer wg.Done()
			s := stores[handleIdx]
			for j := 0; j < 20; j++ {
				guid := fmt.Sprintf("multi-handle-%d-%d", handleIdx, j)
				playerID := fmt.Sprintf("Epic|multi_p_%d|0", handleIdx)
				won := true

				err := s.SaveMatchPlayers(ctx, guid, []*storage.MatchPlayerRecord{
					{MatchGUID: guid, PlayerID: playerID, TeamNum: 0, Won: &won, Score: 300},
				})
				if err != nil {
					errCh <- fmt.Errorf("handle %d write error: %w", handleIdx, err)
					return
				}

				_, err = s.GetMatchPlayers(ctx, guid)
				if err != nil {
					errCh <- fmt.Errorf("handle %d read error: %w", handleIdx, err)
					return
				}
				completed.Add(1)
			}
		}(h)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("Multi-handle contention error: %v", err)
	}
	t.Logf("Completed %d operations across %d independent SQLiteStore handles", completed.Load(), numHandles)
}

func containsLockError(msg string) bool {
	return msg == "database is locked" ||
		msg == "table is locked" ||
		msg == "busy" ||
		msg == "database table is locked"
}
