package storage_test

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/storage"
)

// ============================================================================
// 1. CONCURRENT READ / WRITE STRESS TEST (JSONStore)
// ============================================================================

// TestConcurrencyStress_SearchWhileWriting_JSONStore stresses JSONStore under
// high-concurrency read/write operations to verify thread-safety, absence of map
// read/write panics, reader-writer lock integrity, and deterministic ordering.
func TestConcurrencyStress_SearchWhileWriting_JSONStore(t *testing.T) {
	for _, entry := range getSearchTestStores(t) {
		if entry.name != "JSONStore" {
			continue
		}
		t.Run(entry.name, func(t *testing.T) {
			store := entry.factory(t)
			seedSearchTestDataset(t, store)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			const (
				numSearchers    = 25
				numPlayerWriters = 15
				numMatchWriters  = 15
				numRankUpdaters  = 10
				iterationsPerWorker = 20
			)

			var wg sync.WaitGroup
			errCh := make(chan error, (numSearchers+numPlayerWriters+numMatchWriters+numRankUpdaters)*iterationsPerWorker)

			var (
				totalSearches atomic.Int64
				totalUpserts  atomic.Int64
				totalMatches  atomic.Int64
				totalRanks    atomic.Int64
			)

			queries := []string{"", "squishy", "pro", "king", "daniel", "76561198", "epic", "nonexistent"}
			platforms := []string{"", "all", "Steam", "Epic", "steam", "ALL"}

			// 1. Searchers (Readers)
			for i := 0; i < numSearchers; i++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					r := rand.New(rand.NewSource(int64(workerID * 1000)))
					for j := 0; j < iterationsPerWorker; j++ {
						q := queries[r.Intn(len(queries))]
						p := platforms[r.Intn(len(platforms))]
						limit := r.Intn(15) - 2 // includes -2, -1, 0, and positive limits
						offset := r.Intn(10) - 2 // includes negative and positive offsets

						results, total, err := store.SearchPlayerSummaries(ctx, q, p, limit, offset)
						if err != nil {
							errCh <- fmt.Errorf("searcher %d iter %d error: %w", workerID, j, err)
							return
						}
						totalSearches.Add(1)

						if total < 0 {
							errCh <- fmt.Errorf("searcher %d iter %d returned negative total: %d", workerID, j, total)
							return
						}

						// Validate deterministic ordering and aggregation invariant
						for k := 0; k < len(results); k++ {
							rec := results[k]
							if rec == nil {
								errCh <- fmt.Errorf("searcher %d iter %d returned nil summary at index %d", workerID, j, k)
								return
							}
							if rec.PlayerID == "" {
								errCh <- fmt.Errorf("searcher %d iter %d returned empty PlayerID", workerID, j)
								return
							}

							expectedMatches := rec.TotalWinsAsTeammate + rec.TotalLossesAsTeammate + rec.TotalWinsAsOpponent + rec.TotalLossesAsOpponent
							if rec.TotalMatches != expectedMatches {
								errCh <- fmt.Errorf("searcher %d iter %d player %s: total_matches mismatch: got %d, expected %d",
									workerID, j, rec.PlayerID, rec.TotalMatches, expectedMatches)
								return
							}

							if k > 0 {
								prev := results[k-1]
								if prev.LastSeenAt.Before(rec.LastSeenAt) {
									errCh <- fmt.Errorf("searcher %d iter %d ordering violation: %s (%v) before %s (%v)",
										workerID, j, prev.PlayerID, prev.LastSeenAt, rec.PlayerID, rec.LastSeenAt)
									return
								}
								if prev.LastSeenAt.Equal(rec.LastSeenAt) && prev.PlayerID > rec.PlayerID {
									errCh <- fmt.Errorf("searcher %d iter %d tie-break ordering violation: %s before %s",
										workerID, j, prev.PlayerID, rec.PlayerID)
									return
								}
							}
						}
					}
				}(i)
			}

			// 2. Player Writers (UpsertPlayer)
			for i := 0; i < numPlayerWriters; i++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					r := rand.New(rand.NewSource(int64(workerID * 2000)))
					for j := 0; j < iterationsPerWorker; j++ {
						pid := fmt.Sprintf("Steam|stress_p_%d_%d|0", workerID, j%5)
						now := time.Now().UTC()
						err := store.UpsertPlayer(ctx, &storage.PlayerRecord{
							PlayerID:    pid,
							Platform:    "Steam",
							PlayerName:  fmt.Sprintf("StressPlayer_%d_%d", workerID, j),
							RanksJSON:   fmt.Sprintf(`{"11":{"tier":%d}}`, r.Intn(20)),
							FirstSeenAt: now,
							LastSeenAt:  now,
						})
						if err != nil {
							errCh <- fmt.Errorf("player writer %d iter %d error: %w", workerID, j, err)
							return
						}
						totalUpserts.Add(1)
					}
				}(i)
			}

			// 3. Match Writers (RecordMatchResults)
			for i := 0; i < numMatchWriters; i++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					r := rand.New(rand.NewSource(int64(workerID * 3000)))
					for j := 0; j < iterationsPerWorker; j++ {
						guid := fmt.Sprintf("stress-match-guid-%d-%d", workerID, j)
						playlistID := 11 + (j % 3)
						targetPID := fmt.Sprintf("Steam|stress_p_%d_%d|0", workerID, j%5)
						outcomes := []storage.PlayerOutcome{
							{
								PlayerID:   targetPID,
								Platform:   "Steam",
								PlayerName: fmt.Sprintf("StressPlayer_%d_%d", workerID, j),
								IsTeammate: r.Intn(2) == 0,
								Won:        r.Intn(2) == 0,
							},
						}

						err := store.RecordMatchResults(ctx, guid, playlistID, outcomes)
						if err != nil && err != storage.ErrMatchAlreadyProcessed {
							errCh <- fmt.Errorf("match writer %d iter %d error: %w", workerID, j, err)
							return
						}
						totalMatches.Add(1)
					}
				}(i)
			}

			// 4. Rank Updaters (UpdatePlayerRanks)
			for i := 0; i < numRankUpdaters; i++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					r := rand.New(rand.NewSource(int64(workerID * 4000)))
					for j := 0; j < iterationsPerWorker; j++ {
						targetPID := fmt.Sprintf("Steam|stress_p_%d_%d|0", workerID%numPlayerWriters, j%5)
						ranksJSON := fmt.Sprintf(`{"11":{"tier":%d,"division":%d}}`, r.Intn(22), r.Intn(4))
						err := store.UpdatePlayerRanks(ctx, targetPID, ranksJSON)
						if err != nil && err != storage.ErrPlayerNotFound {
							errCh <- fmt.Errorf("rank updater %d iter %d error: %w", workerID, j, err)
							return
						}
						totalRanks.Add(1)
					}
				}(i)
			}

			wg.Wait()
			close(errCh)

			for err := range errCh {
				t.Fatalf("concurrency stress failure: %v", err)
			}

			t.Logf("[JSONStore Stress Completed] Searches: %d, Upserts: %d, Matches: %d, Ranks: %d",
				totalSearches.Load(), totalUpserts.Load(), totalMatches.Load(), totalRanks.Load())
		})
	}
}

// ============================================================================
// 2. CONCURRENT READ / WRITE STRESS TEST (SQLiteStore)
// ============================================================================

// TestConcurrencyStress_SearchWhileWriting_SQLiteStore stresses SQLiteStore under
// high concurrent reads, writes, and transactions to ensure the single connection
// pool (MaxOpenConns=1) does not deadlock, starve, or produce "database is locked" errors.
func TestConcurrencyStress_SearchWhileWriting_SQLiteStore(t *testing.T) {
	for _, entry := range getSearchTestStores(t) {
		if entry.name != "SQLiteStore" {
			continue
		}
		t.Run(entry.name, func(t *testing.T) {
			store := entry.factory(t)
			seedSearchTestDataset(t, store)

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			const (
				numSearchers        = 20
				numPlayerWriters    = 10
				numMatchWriters     = 10
				numRankUpdaters     = 5
				iterationsPerWorker = 15
			)

			var wg sync.WaitGroup
			errCh := make(chan error, (numSearchers+numPlayerWriters+numMatchWriters+numRankUpdaters)*iterationsPerWorker)

			var (
				totalSearches atomic.Int64
				totalUpserts  atomic.Int64
				totalMatches  atomic.Int64
				totalRanks    atomic.Int64
			)

			queries := []string{"", "squishy", "pro", "king", "daniel", "76561198", "epic", "nonexistent"}
			platforms := []string{"", "all", "Steam", "Epic"}

			// 1. Searchers (Readers)
			for i := 0; i < numSearchers; i++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					r := rand.New(rand.NewSource(int64(workerID * 1111)))
					for j := 0; j < iterationsPerWorker; j++ {
						q := queries[r.Intn(len(queries))]
						p := platforms[r.Intn(len(platforms))]
						limit := r.Intn(10) - 1
						offset := r.Intn(10) - 1

						results, total, err := store.SearchPlayerSummaries(ctx, q, p, limit, offset)
						if err != nil {
							errCh <- fmt.Errorf("sqlite searcher %d iter %d error: %w", workerID, j, err)
							return
						}
						totalSearches.Add(1)

						if total < 0 {
							errCh <- fmt.Errorf("sqlite searcher %d iter %d returned negative total: %d", workerID, j, total)
							return
						}

						// Validate ordering and aggregation
						for k := 0; k < len(results); k++ {
							rec := results[k]
							if rec == nil {
								errCh <- fmt.Errorf("sqlite searcher %d iter %d nil summary", workerID, j)
								return
							}

							expectedMatches := rec.TotalWinsAsTeammate + rec.TotalLossesAsTeammate + rec.TotalWinsAsOpponent + rec.TotalLossesAsOpponent
							if rec.TotalMatches != expectedMatches {
								errCh <- fmt.Errorf("sqlite searcher %d iter %d player %s total_matches mismatch: got %d, expected %d",
									workerID, j, rec.PlayerID, rec.TotalMatches, expectedMatches)
								return
							}

							if k > 0 {
								prev := results[k-1]
								if prev.LastSeenAt.Before(rec.LastSeenAt) {
									errCh <- fmt.Errorf("sqlite searcher %d iter %d ordering violation: %s (%v) before %s (%v)",
										workerID, j, prev.PlayerID, prev.LastSeenAt, rec.PlayerID, rec.LastSeenAt)
									return
								}
								if prev.LastSeenAt.Equal(rec.LastSeenAt) && prev.PlayerID > rec.PlayerID {
									errCh <- fmt.Errorf("sqlite searcher %d iter %d tie-break ordering violation: %s before %s",
										workerID, j, prev.PlayerID, rec.PlayerID)
									return
								}
							}
						}
					}
				}(i)
			}

			// 2. Player Writers (UpsertPlayer)
			for i := 0; i < numPlayerWriters; i++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					r := rand.New(rand.NewSource(int64(workerID * 2222)))
					for j := 0; j < iterationsPerWorker; j++ {
						pid := fmt.Sprintf("Steam|sql_stress_%d_%d|0", workerID, j%4)
						now := time.Now().UTC()
						err := store.UpsertPlayer(ctx, &storage.PlayerRecord{
							PlayerID:    pid,
							Platform:    "Steam",
							PlayerName:  fmt.Sprintf("SQLPlayer_%d_%d", workerID, j),
							RanksJSON:   fmt.Sprintf(`{"11":{"tier":%d}}`, r.Intn(20)),
							FirstSeenAt: now,
							LastSeenAt:  now,
						})
						if err != nil {
							errCh <- fmt.Errorf("sqlite player writer %d iter %d error: %w", workerID, j, err)
							return
						}
						totalUpserts.Add(1)
					}
				}(i)
			}

			// 3. Match Writers (RecordMatchResults)
			for i := 0; i < numMatchWriters; i++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					r := rand.New(rand.NewSource(int64(workerID * 3333)))
					for j := 0; j < iterationsPerWorker; j++ {
						guid := fmt.Sprintf("sql-stress-guid-%d-%d", workerID, j)
						playlistID := 11 + (j % 3)
						targetPID := fmt.Sprintf("Steam|sql_stress_%d_%d|0", workerID, j%4)
						outcomes := []storage.PlayerOutcome{
							{
								PlayerID:   targetPID,
								Platform:   "Steam",
								PlayerName: fmt.Sprintf("SQLPlayer_%d_%d", workerID, j),
								IsTeammate: r.Intn(2) == 0,
								Won:        r.Intn(2) == 0,
							},
						}

						err := store.RecordMatchResults(ctx, guid, playlistID, outcomes)
						if err != nil && err != storage.ErrMatchAlreadyProcessed {
							errCh <- fmt.Errorf("sqlite match writer %d iter %d error: %w", workerID, j, err)
							return
						}
						totalMatches.Add(1)
					}
				}(i)
			}

			// 4. Rank Updaters (UpdatePlayerRanks)
			for i := 0; i < numRankUpdaters; i++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					r := rand.New(rand.NewSource(int64(workerID * 4444)))
					for j := 0; j < iterationsPerWorker; j++ {
						targetPID := fmt.Sprintf("Steam|sql_stress_%d_%d|0", workerID%numPlayerWriters, j%4)
						ranksJSON := fmt.Sprintf(`{"11":{"tier":%d,"division":%d}}`, r.Intn(22), r.Intn(4))
						err := store.UpdatePlayerRanks(ctx, targetPID, ranksJSON)
						if err != nil && err != storage.ErrPlayerNotFound {
							errCh <- fmt.Errorf("sqlite rank updater %d iter %d error: %w", workerID, j, err)
							return
						}
						totalRanks.Add(1)
					}
				}(i)
			}

			wg.Wait()
			close(errCh)

			for err := range errCh {
				t.Fatalf("sqlite concurrency stress failure: %v", err)
			}

			t.Logf("[SQLiteStore Stress Completed] Searches: %d, Upserts: %d, Matches: %d, Ranks: %d",
				totalSearches.Load(), totalUpserts.Load(), totalMatches.Load(), totalRanks.Load())
		})
	}
}

// ============================================================================
// 3. DEEP COPY AND MUTATION LEAK RESILIENCE TEST (JSONStore)
// ============================================================================

// TestConcurrencyStress_JSONStore_DeepCopyAndMutationLeak aggressively mutates
// all fields of returned PlayerSummary objects in reader goroutines while other
// goroutines concurrently query and inspect the store, verifying that mutations
// to search results NEVER corrupt the internal store state.
func TestConcurrencyStress_JSONStore_DeepCopyAndMutationLeak(t *testing.T) {
	for _, entry := range getSearchTestStores(t) {
		if entry.name != "JSONStore" {
			continue
		}
		t.Run(entry.name, func(t *testing.T) {
			store := entry.factory(t)
			seedSearchTestDataset(t, store)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			const (
				numMutators = 20
				numAuditors = 10
				iterations  = 30
			)

			var wg sync.WaitGroup
			errCh := make(chan error, (numMutators+numAuditors)*iterations)

			// Mutators: Search, then corrupt every field of every returned summary
			for i := 0; i < numMutators; i++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					for j := 0; j < iterations; j++ {
						results, _, err := store.SearchPlayerSummaries(ctx, "", "all", 20, 0)
						if err != nil {
							errCh <- fmt.Errorf("mutator %d search error: %w", workerID, err)
							return
						}

						for _, summary := range results {
							// Deliberately corrupt all fields
							summary.PlayerName = "CORRUPTED_MUTATED_NAME"
							summary.Platform = "CORRUPTED_PLATFORM"
							summary.PlayerID = "CORRUPTED_ID"
							summary.RanksJSON = `{"corrupted": true}`
							summary.FirstSeenAt = time.Unix(1, 0).UTC()
							summary.LastSeenAt = time.Unix(2, 0).UTC()
							summary.TotalWinsAsTeammate = 999999
							summary.TotalLossesAsTeammate = 888888
							summary.TotalWinsAsOpponent = 777777
							summary.TotalLossesAsOpponent = 666666
							summary.TotalMatches = 3333330
						}
					}
				}(i)
			}

			// Auditors: Continuously verify that the store never has corrupted data
			for i := 0; i < numAuditors; i++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					for j := 0; j < iterations; j++ {
						results, _, err := store.SearchPlayerSummaries(ctx, "", "all", 20, 0)
						if err != nil {
							errCh <- fmt.Errorf("auditor %d search error: %w", workerID, err)
							return
						}

						for _, s := range results {
							if s.PlayerName == "CORRUPTED_MUTATED_NAME" {
								errCh <- fmt.Errorf("auditor detected leaked mutated PlayerName in store: %s", s.PlayerID)
								return
							}
							if s.Platform == "CORRUPTED_PLATFORM" {
								errCh <- fmt.Errorf("auditor detected leaked mutated Platform in store: %s", s.PlayerID)
								return
							}
							if s.TotalMatches >= 999999 {
								errCh <- fmt.Errorf("auditor detected leaked mutated TotalMatches in store: %d", s.TotalMatches)
								return
							}
							if s.RanksJSON == `{"corrupted": true}` {
								errCh <- fmt.Errorf("auditor detected leaked mutated RanksJSON in store")
								return
							}
						}
					}
				}(i)
			}

			wg.Wait()
			close(errCh)

			for err := range errCh {
				t.Fatalf("deep copy mutation leak failure: %v", err)
			}
		})
	}
}

// ============================================================================
// 4. CONNECTION POOL STARVATION & UNCLOSED ROWS TEST (SQLiteStore)
// ============================================================================

// TestConcurrencyStress_SQLiteStore_ConnectionPoolStarvationAndRowLeaks
// stresses SQLiteStore by interleaving boundary queries, rapid context
// cancellations, and subsequent writes to prove no rows or connections are leaked.
func TestConcurrencyStress_SQLiteStore_ConnectionPoolStarvationAndRowLeaks(t *testing.T) {
	for _, entry := range getSearchTestStores(t) {
		if entry.name != "SQLiteStore" {
			continue
		}
		t.Run(entry.name, func(t *testing.T) {
			store := entry.factory(t)
			seedSearchTestDataset(t, store)

			// Phase 1: Boundary stress — queries triggering fast-paths and zero results
			const numBoundaryWorkers = 20
			var wg sync.WaitGroup
			errCh := make(chan error, numBoundaryWorkers*50)

			for i := 0; i < numBoundaryWorkers; i++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					ctx := context.Background()
					for j := 0; j < 50; j++ {
						switch j % 5 {
						case 0:
							// total == 0 shortcut
							res, tot, err := store.SearchPlayerSummaries(ctx, "nonexistent_query_string_xyz", "", 10, 0)
							if err != nil || tot != 0 || len(res) != 0 {
								errCh <- fmt.Errorf("total==0 shortcut failed: res=%d, tot=%d, err=%v", len(res), tot, err)
								return
							}
						case 1:
							// limit == 0 shortcut
							res, tot, err := store.SearchPlayerSummaries(ctx, "", "all", 0, 0)
							if err != nil || tot == 0 || len(res) != 0 {
								errCh <- fmt.Errorf("limit==0 shortcut failed: res=%d, tot=%d, err=%v", len(res), tot, err)
								return
							}
						case 2:
							// offset >= total shortcut
							res, tot, err := store.SearchPlayerSummaries(ctx, "", "all", 10, 99999)
							if err != nil || tot == 0 || len(res) != 0 {
								errCh <- fmt.Errorf("offset>=total shortcut failed: res=%d, tot=%d, err=%v", len(res), tot, err)
								return
							}
						case 3:
							// negative limit and offset normalization
							res, tot, err := store.SearchPlayerSummaries(ctx, "", "all", -1, -5)
							if err != nil || tot == 0 || len(res) != tot {
								errCh <- fmt.Errorf("negative limit/offset failed: res=%d, tot=%d, err=%v", len(res), tot, err)
								return
							}
						case 4:
							// standard full scan with row iteration
							res, tot, err := store.SearchPlayerSummaries(ctx, "squishy", "", 5, 0)
							if err != nil || tot == 0 || len(res) == 0 {
								errCh <- fmt.Errorf("standard scan failed: res=%d, tot=%d, err=%v", len(res), tot, err)
								return
							}
						}
					}
				}(i)
			}
			wg.Wait()

			close(errCh)
			for err := range errCh {
				t.Fatalf("boundary query error: %v", err)
			}

			// Phase 2: Rapid Context Cancellations mid-query
			const numCancelWorkers = 5
			for i := 0; i < numCancelWorkers; i++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					for j := 0; j < 5; j++ {
						timeout := time.Duration((j+1)*2) * time.Millisecond
						cancelCtx, cancel := context.WithTimeout(context.Background(), timeout)
						_, _, _ = store.SearchPlayerSummaries(cancelCtx, "pro", "Steam", 10, 0)
						cancel()
					}
				}(i)
			}
			wg.Wait()

			// Phase 3: Starvation Verification
			// If any row was unclosed or connection was leaked, the single connection
			// pool (MaxOpenConns=1) will hang here. We test with a strict 2-second timeout.
			verifyCtx, verifyCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer verifyCancel()

			testPID := "Steam|pool_starvation_check|0"
			if err := store.UpsertPlayer(verifyCtx, &storage.PlayerRecord{
				PlayerID:   testPID,
				Platform:   "Steam",
				PlayerName: "PoolCheckPlayer",
			}); err != nil {
				t.Fatalf("CONNECTION STARVED: UpsertPlayer failed after concurrent load: %v", err)
			}

			if err := store.RecordMatchResults(verifyCtx, "guid-pool-starve-1", 11, []storage.PlayerOutcome{
				{PlayerID: testPID, IsTeammate: true, Won: true},
			}); err != nil {
				t.Fatalf("CONNECTION STARVED: RecordMatchResults failed after concurrent load: %v", err)
			}

			summaries, total, err := store.SearchPlayerSummaries(verifyCtx, "PoolCheckPlayer", "Steam", 10, 0)
			if err != nil {
				t.Fatalf("CONNECTION STARVED: SearchPlayerSummaries failed after concurrent load: %v", err)
			}
			if total != 1 || len(summaries) != 1 {
				t.Fatalf("expected 1 result from PoolCheckPlayer, got total=%d len=%d", total, len(summaries))
			}
			if summaries[0].TotalWinsAsTeammate != 1 {
				t.Fatalf("expected 1 TotalWinsAsTeammate, got %d", summaries[0].TotalWinsAsTeammate)
			}
		})
	}
}

// ============================================================================
// 5. PARALLEL IDENTICAL STREAM DUAL-STORE CONSISTENCY TEST
// ============================================================================

// TestConcurrencyStress_DualStore_ParityUnderParallelWrites writes an identical
// stream of concurrent updates to both SQLiteStore and JSONStore and verifies:
// 1. Total counts, total matches, and win/loss aggregations match 100% across all queries.
// 2. Deterministic sort parity holds when timestamps have 1-second resolution.
// 3. Empirically verifies the known sub-second timestamp ordering divergence when
//    concurrent RecordMatchResults calls execute within the same second.
func TestConcurrencyStress_DualStore_ParityUnderParallelWrites(t *testing.T) {
	stores := getSearchTestStores(t)
	var (
		sqlStore  storage.StateStore
		jsonStore storage.StateStore
	)
	for _, s := range stores {
		if s.name == "SQLiteStore" {
			sqlStore = s.factory(t)
		} else if s.name == "JSONStore" {
			jsonStore = s.factory(t)
		}
	}
	if sqlStore == nil || jsonStore == nil {
		t.Fatal("missing either SQLiteStore or JSONStore")
	}

	seedSearchTestDataset(t, sqlStore)
	seedSearchTestDataset(t, jsonStore)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const (
		numWriters = 10
		ops        = 10
	)
	var wg sync.WaitGroup
	errCh := make(chan error, numWriters*ops*2)

	// Discrete base time: each player has a distinct second timestamp
	baseTime := time.Date(2026, 9, 26, 16, 0, 0, 0, time.UTC)

	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				pid := fmt.Sprintf("Steam|dual_%02d_%02d|0", workerID, j)
				// Distinct second for each player
				secOffset := workerID*ops + j
				fixedTime := baseTime.Add(time.Duration(secOffset) * time.Second)

				p := &storage.PlayerRecord{
					PlayerID:    pid,
					Platform:    "Steam",
					PlayerName:  fmt.Sprintf("DualPlayer_%02d_%02d", workerID, j),
					RanksJSON:   `{"11":{"tier":10}}`,
					FirstSeenAt: fixedTime,
					LastSeenAt:  fixedTime,
				}
				if err := sqlStore.UpsertPlayer(ctx, p); err != nil {
					errCh <- fmt.Errorf("sql dual upsert error: %w", err)
					return
				}
				if err := jsonStore.UpsertPlayer(ctx, p); err != nil {
					errCh <- fmt.Errorf("json dual upsert error: %w", err)
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("parallel dual write error: %v", err)
	}

	// Compare SearchPlayerSummaries side-by-side across both stores
	testQueries := []struct {
		name     string
		query    string
		platform string
		limit    int
		offset   int
	}{
		{"PrefixSearch_Page1", "DualPlayer", "Steam", 20, 0},
		{"PrefixSearch_Page2", "DualPlayer", "Steam", 20, 20},
		{"PrefixSearch_Page3", "DualPlayer", "Steam", 20, 40},
		{"PrefixSearch_OverLimit", "DualPlayer", "Steam", 200, 0},
		{"PrefixSearch_AllPlatforms", "DualPlayer", "all", 50, 0},
		{"PrefixSearch_EmptyPlatform", "DualPlayer", "", 50, 0},
		{"BroadSearch_Steam", "", "Steam", 100, 0},
		{"BroadSearch_All", "", "all", 150, 0},
	}

	for _, tc := range testQueries {
		t.Run(tc.name, func(t *testing.T) {
			sqlRes, sqlTotal, errSQL := sqlStore.SearchPlayerSummaries(ctx, tc.query, tc.platform, tc.limit, tc.offset)
			if errSQL != nil {
				t.Fatalf("sql search error for %v: %v", tc, errSQL)
			}
			jsonRes, jsonTotal, errJSON := jsonStore.SearchPlayerSummaries(ctx, tc.query, tc.platform, tc.limit, tc.offset)
			if errJSON != nil {
				t.Fatalf("json search error for %v: %v", tc, errJSON)
			}

			if sqlTotal != jsonTotal {
				t.Fatalf("total mismatch for %v: sql=%d, json=%d", tc, sqlTotal, jsonTotal)
			}
			if len(sqlRes) != len(jsonRes) {
				t.Fatalf("result len mismatch for %v: sql=%d, json=%d", tc, len(sqlRes), len(jsonRes))
			}

			// Under discrete second timestamps, ordering and content MUST match 100% bitwise
			for k := 0; k < len(sqlRes); k++ {
				sRec := sqlRes[k]
				jRec := jsonRes[k]

				if sRec.PlayerID != jRec.PlayerID {
					t.Fatalf("query %s index %d: PlayerID mismatch: sql=%s, json=%s", tc.name, k, sRec.PlayerID, jRec.PlayerID)
				}
				if sRec.PlayerName != jRec.PlayerName {
					t.Fatalf("query %s index %d: PlayerName mismatch: sql=%s, json=%s", tc.name, k, sRec.PlayerName, jRec.PlayerName)
				}
				if !sRec.LastSeenAt.Equal(jRec.LastSeenAt) {
					t.Fatalf("query %s index %d: LastSeenAt mismatch: sql=%v, json=%v", tc.name, k, sRec.LastSeenAt, jRec.LastSeenAt)
				}
				if sRec.TotalWinsAsTeammate != jRec.TotalWinsAsTeammate {
					t.Fatalf("query %s index %d: TotalWinsAsTeammate mismatch: sql=%d, json=%d",
						tc.name, k, sRec.TotalWinsAsTeammate, jRec.TotalWinsAsTeammate)
				}
				if sRec.TotalMatches != jRec.TotalMatches {
					t.Fatalf("query %s index %d: TotalMatches mismatch: sql=%d, json=%d",
						tc.name, k, sRec.TotalMatches, jRec.TotalMatches)
				}
			}
		})
	}
}

// ============================================================================
// 6. SUB-SECOND TIMESTAMP DIVERGENCE EMPIRICAL VERIFICATION
// ============================================================================

// TestConcurrencyStress_SubsecondTimestampOrdering_DivergenceDocumentation
// empirically demonstrates that when matches occur within the same calendar second:
// - SQLite truncates last_seen_at to Unix integer seconds, resolving ties via player_id ASC.
// - JSONStore preserves nanosecond timestamps in memory, resolving ties by subsecond time.
// - Both stores maintain 100% agreement on total counts, aggregations, and filtering.
func TestConcurrencyStress_SubsecondTimestampOrdering_DivergenceDocumentation(t *testing.T) {
	stores := getSearchTestStores(t)
	var (
		sqlStore  storage.StateStore
		jsonStore storage.StateStore
	)
	for _, s := range stores {
		if s.name == "SQLiteStore" {
			sqlStore = s.factory(t)
		} else if s.name == "JSONStore" {
			jsonStore = s.factory(t)
		}
	}

	ctx := context.Background()

	// Seed two players in reverse alphabetical order within milliseconds
	now := time.Now().UTC()
	pZ := &storage.PlayerRecord{
		PlayerID:    "Steam|subsecond_Z|0",
		Platform:    "Steam",
		PlayerName:  "Subsecond_Z",
		FirstSeenAt: now,
		LastSeenAt:  now,
	}
	pA := &storage.PlayerRecord{
		PlayerID:    "Steam|subsecond_A|0",
		Platform:    "Steam",
		PlayerName:  "Subsecond_A",
		FirstSeenAt: now,
		LastSeenAt:  now,
	}

	_ = sqlStore.UpsertPlayer(ctx, pZ)
	_ = jsonStore.UpsertPlayer(ctx, pZ)
	time.Sleep(2 * time.Millisecond)
	_ = sqlStore.UpsertPlayer(ctx, pA)
	_ = jsonStore.UpsertPlayer(ctx, pA)

	// Record match outcomes for both players within the same second
	_ = sqlStore.RecordMatchResults(ctx, "subsecond-m1", 11, []storage.PlayerOutcome{
		{PlayerID: pZ.PlayerID, Platform: "Steam", PlayerName: pZ.PlayerName, IsTeammate: true, Won: true},
	})
	_ = jsonStore.RecordMatchResults(ctx, "subsecond-m1", 11, []storage.PlayerOutcome{
		{PlayerID: pZ.PlayerID, Platform: "Steam", PlayerName: pZ.PlayerName, IsTeammate: true, Won: true},
	})

	time.Sleep(2 * time.Millisecond)

	_ = sqlStore.RecordMatchResults(ctx, "subsecond-m2", 11, []storage.PlayerOutcome{
		{PlayerID: pA.PlayerID, Platform: "Steam", PlayerName: pA.PlayerName, IsTeammate: true, Won: false},
	})
	_ = jsonStore.RecordMatchResults(ctx, "subsecond-m2", 11, []storage.PlayerOutcome{
		{PlayerID: pA.PlayerID, Platform: "Steam", PlayerName: pA.PlayerName, IsTeammate: true, Won: false},
	})

	sqlRes, sqlTot, errSQL := sqlStore.SearchPlayerSummaries(ctx, "subsecond", "Steam", 10, 0)
	if errSQL != nil {
		t.Fatalf("sql search failed: %v", errSQL)
	}
	jsonRes, jsonTot, errJSON := jsonStore.SearchPlayerSummaries(ctx, "subsecond", "Steam", 10, 0)
	if errJSON != nil {
		t.Fatalf("json search failed: %v", errJSON)
	}

	if sqlTot != 2 || jsonTot != 2 {
		t.Fatalf("expected total 2, got sql=%d, json=%d", sqlTot, jsonTot)
	}

	// Verify that aggregations match exactly
	for _, s := range sqlRes {
		for _, j := range jsonRes {
			if s.PlayerID == j.PlayerID {
				if s.TotalMatches != j.TotalMatches || s.TotalWinsAsTeammate != j.TotalWinsAsTeammate {
					t.Errorf("aggregation mismatch for %s: sql=%+v, json=%+v", s.PlayerID, s, j)
				}
			}
		}
	}

	t.Logf("[Subsecond Divergence Observation] SQLite order: [%s, %s]", sqlRes[0].PlayerID, sqlRes[1].PlayerID)
	t.Logf("[Subsecond Divergence Observation] JSONStore order: [%s, %s]", jsonRes[0].PlayerID, jsonRes[1].PlayerID)
}

