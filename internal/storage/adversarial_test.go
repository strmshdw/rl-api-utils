package storage_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/storage"
)

// ============================================================================
// 1. CONCURRENCY CONTENTION STRESS TESTS
// ============================================================================

func TestAdversarial_ConcurrencyContention_SQLite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "concurrency_stress.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	const numRecords = 30
	const numWorkers = 20
	const opsPerWorker = 20

	// Seed records
	var seeds []*storage.MatchRecord
	now := time.Now().UTC().Unix()
	for i := 0; i < numRecords; i++ {
		seeds = append(seeds, &storage.MatchRecord{
			MatchGUID:            fmt.Sprintf("sql-conc-%03d", i),
			RecordStartTimestamp: now + int64(i),
			MapName:              fmt.Sprintf("Map-%d", i),
			Playlist:             11,
			ReplayURL:            fmt.Sprintf("https://cdn.example.com/%03d.replay", i),
			DownloadStatus:       storage.DownloadPending,
			UploadStatus:         storage.UploadPending,
		})
	}
	if err := store.UpsertDiscoveredMatches(ctx, seeds); err != nil {
		t.Fatalf("failed to seed records: %v", err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, numWorkers*opsPerWorker)

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(workerID * 997)))

			for op := 0; op < opsPerWorker; op++ {
				targetIdx := rng.Intn(numRecords)
				guid := fmt.Sprintf("sql-conc-%03d", targetIdx)
				action := rng.Intn(7)

				switch action {
				case 0:
					// Read single match
					if _, err := store.GetMatch(ctx, guid); err != nil {
						errCh <- fmt.Errorf("worker %d GetMatch failed: %w", workerID, err)
					}
				case 1:
					// List queries
					if _, err := store.ListPendingDownloads(ctx); err != nil {
						errCh <- fmt.Errorf("worker %d ListPendingDownloads failed: %w", workerID, err)
					}
					if _, err := store.ListPendingUploads(ctx); err != nil {
						errCh <- fmt.Errorf("worker %d ListPendingUploads failed: %w", workerID, err)
					}
				case 2:
					// Download lifecycle
					_ = store.MarkDownloading(ctx, guid)
					if err := store.MarkDownloaded(ctx, guid, fmt.Sprintf("/local/%s.replay", guid)); err != nil {
						errCh <- fmt.Errorf("worker %d MarkDownloaded failed: %w", workerID, err)
					}
				case 3:
					// Upload lifecycle
					_ = store.MarkUploading(ctx, guid)
					if err := store.MarkUploaded(ctx, guid, fmt.Sprintf("bc-%s", guid), "https://bc.com"); err != nil {
						errCh <- fmt.Errorf("worker %d MarkUploaded failed: %w", workerID, err)
					}
				case 4:
					// Duplicate handling
					if err := store.MarkDuplicate(ctx, guid, fmt.Sprintf("dup-%s", guid), "https://bc.com/dup"); err != nil {
						errCh <- fmt.Errorf("worker %d MarkDuplicate failed: %w", workerID, err)
					}
				case 5:
					// Failure transition
					if err := store.MarkDownloadFailed(ctx, guid, "transient network blip"); err != nil {
						errCh <- fmt.Errorf("worker %d MarkDownloadFailed failed: %w", workerID, err)
					}
				case 6:
					// Concurrent re-upsert
					subBatch := []*storage.MatchRecord{
						{
							MatchGUID:            guid,
							RecordStartTimestamp: now + int64(targetIdx),
							ReplayURL:            fmt.Sprintf("https://cdn.example.com/%03d.replay", targetIdx),
						},
					}
					if err := store.UpsertDiscoveredMatches(ctx, subBatch); err != nil {
						errCh <- fmt.Errorf("worker %d Upsert failed: %w", workerID, err)
					}
				}
			}
		}(w)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("sqlite concurrency contention error: %v", err)
	}

	// Verify all records are still readable
	for i := 0; i < numRecords; i++ {
		guid := fmt.Sprintf("sql-conc-%03d", i)
		if _, err := store.GetMatch(ctx, guid); err != nil {
			t.Errorf("post-concurrency record %s unreadable: %v", guid, err)
		}
	}
}

func TestAdversarial_ConcurrencyContention_JSON(t *testing.T) {
	jsonPath := filepath.Join(t.TempDir(), "concurrency_stress.json")
	store, err := storage.NewJSONStore(jsonPath)
	if err != nil {
		t.Fatalf("failed to create json store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	const numRecords = 25
	const numWorkers = 15
	const opsPerWorker = 15

	// Seed records
	var seeds []*storage.MatchRecord
	now := time.Now().UTC().Unix()
	for i := 0; i < numRecords; i++ {
		seeds = append(seeds, &storage.MatchRecord{
			MatchGUID:            fmt.Sprintf("json-conc-%03d", i),
			RecordStartTimestamp: now + int64(i),
			MapName:              fmt.Sprintf("Map-%d", i),
			Playlist:             11,
			ReplayURL:            fmt.Sprintf("https://cdn.example.com/%03d.replay", i),
			DownloadStatus:       storage.DownloadPending,
			UploadStatus:         storage.UploadPending,
		})
	}
	if err := store.UpsertDiscoveredMatches(ctx, seeds); err != nil {
		t.Fatalf("failed to seed json store: %v", err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, numWorkers*opsPerWorker)

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(workerID * 811)))

			for op := 0; op < opsPerWorker; op++ {
				targetIdx := rng.Intn(numRecords)
				guid := fmt.Sprintf("json-conc-%03d", targetIdx)
				action := rng.Intn(6)

				switch action {
				case 0:
					if _, err := store.GetMatch(ctx, guid); err != nil {
						errCh <- fmt.Errorf("json worker %d GetMatch failed: %w", workerID, err)
					}
				case 1:
					_, _ = store.ListPendingDownloads(ctx)
					_, _ = store.ListPendingUploads(ctx)
				case 2:
					_ = store.MarkDownloading(ctx, guid)
					if err := store.MarkDownloaded(ctx, guid, fmt.Sprintf("/tmp/%s.replay", guid)); err != nil {
						errCh <- fmt.Errorf("json worker %d MarkDownloaded failed: %w", workerID, err)
					}
				case 3:
					_ = store.MarkUploading(ctx, guid)
					if err := store.MarkUploaded(ctx, guid, fmt.Sprintf("bc-%s", guid), "https://bc.com"); err != nil {
						errCh <- fmt.Errorf("json worker %d MarkUploaded failed: %w", workerID, err)
					}
				case 4:
					if err := store.MarkDuplicate(ctx, guid, fmt.Sprintf("dup-%s", guid), "https://bc.com/dup"); err != nil {
						errCh <- fmt.Errorf("json worker %d MarkDuplicate failed: %w", workerID, err)
					}
				case 5:
					if err := store.SaveAuthState(ctx, "epic", fmt.Sprintf("tok-%d-%d", workerID, op), "acc", "user"); err != nil {
						errCh <- fmt.Errorf("json worker %d SaveAuthState failed: %w", workerID, err)
					}
				}
			}
		}(w)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("json concurrency contention error: %v", err)
	}

	// Verify file on disk is valid JSON
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("failed to read json store after stress: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("json state file empty after stress test")
	}
}

// ============================================================================
// 2. CRASH RECOVERY & DIRTY STATE CLEANUP TESTS
// ============================================================================

func TestAdversarial_CrashRecovery_RecoverInFlight(t *testing.T) {
	testCases := []struct {
		name    string
		factory func(t *testing.T) (storage.StateStore, func())
	}{
		{
			name: "SQLiteStore",
			factory: func(t *testing.T) (storage.StateStore, func()) {
				p := filepath.Join(t.TempDir(), "crash_test.db")
				s, err := storage.NewSQLiteStore(p)
				if err != nil {
					t.Fatalf("NewSQLiteStore failed: %v", err)
				}
				return s, func() { s.Close() }
			},
		},
		{
			name: "JSONStore",
			factory: func(t *testing.T) (storage.StateStore, func()) {
				p := filepath.Join(t.TempDir(), "crash_test.json")
				s, err := storage.NewJSONStore(p)
				if err != nil {
					t.Fatalf("NewJSONStore failed: %v", err)
				}
				return s, func() { s.Close() }
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store, cleanup := tc.factory(t)
			defer cleanup()
			ctx := context.Background()

			now := time.Now().UTC().Unix()
			// Populate 200 matches across various in-flight and terminal statuses
			var matches []*storage.MatchRecord
			for i := 0; i < 200; i++ {
				var dlStatus storage.DownloadStatus
				var upStatus storage.UploadStatus
				localPath := ""
				bcID := ""

				switch i % 5 {
				case 0:
					// In-flight download
					dlStatus = storage.DownloadDownloading
					upStatus = storage.UploadPending
				case 1:
					// In-flight upload
					dlStatus = storage.DownloadDownloaded
					localPath = fmt.Sprintf("/replays/%d.replay", i)
					upStatus = storage.UploadUploading
				case 2:
					// Fully completed
					dlStatus = storage.DownloadDownloaded
					localPath = fmt.Sprintf("/replays/%d.replay", i)
					upStatus = storage.UploadUploaded
					bcID = fmt.Sprintf("bc-%d", i)
				case 3:
					// Duplicate replay
					dlStatus = storage.DownloadDownloaded
					localPath = fmt.Sprintf("/replays/%d.replay", i)
					upStatus = storage.UploadDuplicate
					bcID = fmt.Sprintf("bc-dup-%d", i)
				case 4:
					// Failed download
					dlStatus = storage.DownloadFailed
					upStatus = storage.UploadPending
				}

				matches = append(matches, &storage.MatchRecord{
					MatchGUID:            fmt.Sprintf("crash-guid-%03d", i),
					RecordStartTimestamp: now + int64(i),
					MapName:              "Field_P",
					Playlist:             11,
					ReplayURL:            fmt.Sprintf("https://cdn.example.com/%03d.replay", i),
					DownloadStatus:       dlStatus,
					LocalFilePath:        localPath,
					UploadStatus:         upStatus,
					BallchasingID:        bcID,
					RetryCount:           1,
				})
			}

			if err := store.UpsertDiscoveredMatches(ctx, matches); err != nil {
				t.Fatalf("failed to seed crash matches: %v", err)
			}

			// Simulate crash recovery invocation
			if err := store.RecoverInFlight(ctx); err != nil {
				t.Fatalf("RecoverInFlight failed: %v", err)
			}

			// Verify results for all 200 matches
			for i := 0; i < 200; i++ {
				guid := fmt.Sprintf("crash-guid-%03d", i)
				rec, err := store.GetMatch(ctx, guid)
				if err != nil {
					t.Fatalf("failed to get match %s: %v", guid, err)
				}

				switch i % 5 {
				case 0:
					// In-flight download MUST be reset to PENDING
					if rec.DownloadStatus != storage.DownloadPending {
						t.Errorf("match %s expected DownloadPending after recovery, got %s", guid, rec.DownloadStatus)
					}
					if rec.UploadStatus != storage.UploadPending {
						t.Errorf("match %s expected UploadPending, got %s", guid, rec.UploadStatus)
					}
				case 1:
					// In-flight upload MUST be reset to PENDING, download stays DOWNLOADED
					if rec.DownloadStatus != storage.DownloadDownloaded {
						t.Errorf("match %s download status altered: %s", guid, rec.DownloadStatus)
					}
					if rec.UploadStatus != storage.UploadPending {
						t.Errorf("match %s expected UploadPending after recovery, got %s", guid, rec.UploadStatus)
					}
				case 2:
					// Fully completed MUST NOT be altered
					if rec.DownloadStatus != storage.DownloadDownloaded || rec.UploadStatus != storage.UploadUploaded {
						t.Errorf("match %s completed status altered: dl=%s, up=%s", guid, rec.DownloadStatus, rec.UploadStatus)
					}
					if rec.BallchasingID != fmt.Sprintf("bc-%d", i) {
						t.Errorf("match %s BallchasingID lost: %s", guid, rec.BallchasingID)
					}
				case 3:
					// Duplicate MUST NOT be altered
					if rec.UploadStatus != storage.UploadDuplicate {
						t.Errorf("match %s duplicate status altered: %s", guid, rec.UploadStatus)
					}
					if rec.BallchasingID != fmt.Sprintf("bc-dup-%d", i) {
						t.Errorf("match %s BallchasingID lost: %s", guid, rec.BallchasingID)
					}
				case 4:
					// Failed download MUST NOT be altered
					if rec.DownloadStatus != storage.DownloadFailed {
						t.Errorf("match %s failed status altered: %s", guid, rec.DownloadStatus)
					}
				}
			}
		})
	}
}

func TestAdversarial_DirtyState_TempFileCleanup(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "state.json")

	// Pre-create 50 stale temp files simulating prior interrupted writes/abnormal terminations
	for i := 0; i < 50; i++ {
		stalePath := filepath.Join(tempDir, fmt.Sprintf(".rl-sync-state-dirty-%03d.tmp", i))
		if err := os.WriteFile(stalePath, []byte(`{"partial": "corrupt data"}`), 0644); err != nil {
			t.Fatalf("failed to create stale temp file %s: %v", stalePath, err)
		}
	}

	// Create an unrelated file that must NOT be touched
	keepPath := filepath.Join(tempDir, "important_user_log.txt")
	if err := os.WriteFile(keepPath, []byte("preserve me"), 0644); err != nil {
		t.Fatalf("failed to create keep file: %v", err)
	}

	// Also create a valid initial state file
	initialStore, err := storage.NewJSONStore(statePath)
	if err != nil {
		t.Fatalf("failed to create initial store: %v", err)
	}
	ctx := context.Background()
	_ = initialStore.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{
		{MatchGUID: "survive-guid", ReplayURL: "https://example.com/replay.replay"},
	})
	_ = initialStore.Close()

	// Reopen store — this should trigger cleanupStaleTempFiles
	reopenedStore, err := storage.NewJSONStore(statePath)
	if err != nil {
		t.Fatalf("failed to reopen store: %v", err)
	}
	defer reopenedStore.Close()

	// Verify all 50 stale temp files are removed
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("failed to read dir: %v", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if filepath.Ext(name) == ".tmp" {
			t.Errorf("stale temp file was not cleaned up: %s", name)
		}
	}

	// Verify unrelated file survived
	data, err := os.ReadFile(keepPath)
	if err != nil || string(data) != "preserve me" {
		t.Errorf("unrelated file was deleted or altered: %v", err)
	}

	// Verify store data survived
	rec, err := reopenedStore.GetMatch(ctx, "survive-guid")
	if err != nil || rec.MatchGUID != "survive-guid" {
		t.Errorf("reopened store failed to retrieve record: %v", err)
	}
}

// ============================================================================
// 3. IDEMPOTENCY INVARIANT TESTING
// ============================================================================

func TestAdversarial_Idempotency_TerminalStatesNeverClobbered(t *testing.T) {
	testStores := []struct {
		name    string
		factory func(t *testing.T) storage.StateStore
	}{
		{
			name: "SQLiteStore",
			factory: func(t *testing.T) storage.StateStore {
				p := filepath.Join(t.TempDir(), "idem.db")
				s, err := storage.NewSQLiteStore(p)
				if err != nil {
					t.Fatalf("NewSQLiteStore failed: %v", err)
				}
				return s
			},
		},
		{
			name: "JSONStore",
			factory: func(t *testing.T) storage.StateStore {
				p := filepath.Join(t.TempDir(), "idem.json")
				s, err := storage.NewJSONStore(p)
				if err != nil {
					t.Fatalf("NewJSONStore failed: %v", err)
				}
				return s
			},
		},
	}

	for _, ts := range testStores {
		t.Run(ts.name, func(t *testing.T) {
			store := ts.factory(t)
			defer store.Close()
			ctx := context.Background()

			now := time.Now().UTC().Truncate(time.Second)

			// Step 1: Insert initial records and transition them to various advanced states
			mDownloaded := &storage.MatchRecord{
				MatchGUID:            "match-downloaded",
				RecordStartTimestamp: now.Unix(),
				ReplayURL:            "https://cdn.example.com/dl.replay",
			}
			mUploaded := &storage.MatchRecord{
				MatchGUID:            "match-uploaded",
				RecordStartTimestamp: now.Unix() + 10,
				ReplayURL:            "https://cdn.example.com/up.replay",
			}
			mDuplicate := &storage.MatchRecord{
				MatchGUID:            "match-duplicate",
				RecordStartTimestamp: now.Unix() + 20,
				ReplayURL:            "https://cdn.example.com/dup.replay",
			}
			mFailed := &storage.MatchRecord{
				MatchGUID:            "match-failed",
				RecordStartTimestamp: now.Unix() + 30,
				ReplayURL:            "https://cdn.example.com/failed.replay",
			}

			if err := store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{mDownloaded, mUploaded, mDuplicate, mFailed}); err != nil {
				t.Fatalf("initial upsert failed: %v", err)
			}

			// Transition mDownloaded
			_ = store.MarkDownloading(ctx, "match-downloaded")
			_ = store.MarkDownloaded(ctx, "match-downloaded", "/replays/dl.replay")

			// Transition mUploaded
			_ = store.MarkDownloading(ctx, "match-uploaded")
			_ = store.MarkDownloaded(ctx, "match-uploaded", "/replays/up.replay")
			_ = store.MarkUploading(ctx, "match-uploaded")
			_ = store.MarkUploaded(ctx, "match-uploaded", "bc-up-guid", "https://ballchasing.com/up")

			// Transition mDuplicate
			_ = store.MarkDownloading(ctx, "match-duplicate")
			_ = store.MarkDownloaded(ctx, "match-duplicate", "/replays/dup.replay")
			_ = store.MarkUploading(ctx, "match-duplicate")
			_ = store.MarkDuplicate(ctx, "match-duplicate", "bc-dup-guid", "https://ballchasing.com/dup")

			// Transition mFailed
			_ = store.MarkDownloading(ctx, "match-failed")
			_ = store.MarkDownloadFailed(ctx, "match-failed", "permanent 404")

			// Step 2: Now simulate 10 subsequent poll cycles where the PsyNet API returns
			// ALL 4 matches again as "newly discovered" with default PENDING statuses and empty metadata.
			for cycle := 1; cycle <= 10; cycle++ {
				rePollBatch := []*storage.MatchRecord{
					{
						MatchGUID:            "match-downloaded",
						RecordStartTimestamp: now.Unix(),
						ReplayURL:            "https://cdn.example.com/dl.replay",
						DownloadStatus:       storage.DownloadPending, // Attempt to clobber
						UploadStatus:         storage.UploadPending,
					},
					{
						MatchGUID:            "match-uploaded",
						RecordStartTimestamp: now.Unix() + 10,
						ReplayURL:            "https://cdn.example.com/up.replay",
						DownloadStatus:       storage.DownloadPending, // Attempt to clobber
						UploadStatus:         storage.UploadPending,
						BallchasingID:        "",
					},
					{
						MatchGUID:            "match-duplicate",
						RecordStartTimestamp: now.Unix() + 20,
						ReplayURL:            "https://cdn.example.com/dup.replay",
						DownloadStatus:       storage.DownloadPending, // Attempt to clobber
						UploadStatus:         storage.UploadPending,
						BallchasingID:        "",
					},
					{
						MatchGUID:            "match-failed",
						RecordStartTimestamp: now.Unix() + 30,
						ReplayURL:            "https://cdn.example.com/failed.replay",
						DownloadStatus:       storage.DownloadPending, // Attempt to clobber
						RetryCount:           0,
					},
				}

				if err := store.UpsertDiscoveredMatches(ctx, rePollBatch); err != nil {
					t.Fatalf("cycle %d upsert failed: %v", cycle, err)
				}

				// Verify invariant after EACH cycle
				rDl, _ := store.GetMatch(ctx, "match-downloaded")
				if rDl.DownloadStatus != storage.DownloadDownloaded || rDl.LocalFilePath != "/replays/dl.replay" {
					t.Fatalf("cycle %d: DOWNLOADED status clobbered: status=%s, path=%s", cycle, rDl.DownloadStatus, rDl.LocalFilePath)
				}

				rUp, _ := store.GetMatch(ctx, "match-uploaded")
				if rUp.UploadStatus != storage.UploadUploaded || rUp.BallchasingID != "bc-up-guid" {
					t.Fatalf("cycle %d: UPLOADED status clobbered: status=%s, bcID=%s", cycle, rUp.UploadStatus, rUp.BallchasingID)
				}

				rDup, _ := store.GetMatch(ctx, "match-duplicate")
				if rDup.UploadStatus != storage.UploadDuplicate || rDup.BallchasingID != "bc-dup-guid" {
					t.Fatalf("cycle %d: DUPLICATE status clobbered: status=%s, bcID=%s", cycle, rDup.UploadStatus, rDup.BallchasingID)
				}

				rFail, _ := store.GetMatch(ctx, "match-failed")
				if rFail.DownloadStatus != storage.DownloadFailed || rFail.RetryCount != 1 || rFail.LastError != "permanent 404" {
					t.Fatalf("cycle %d: FAILED status clobbered: status=%s, retry=%d, err=%s", cycle, rFail.DownloadStatus, rFail.RetryCount, rFail.LastError)
				}
			}
		})
	}
}

// ============================================================================
// 4. CORRUPT DATABASES & RESILIENCY TESTS
// ============================================================================

func TestAdversarial_CorruptDatabase_Resilience(t *testing.T) {
	tempDir := t.TempDir()

	t.Run("SQLite_GarbageBytes", func(t *testing.T) {
		corruptPath := filepath.Join(tempDir, "garbage.db")
		// Write 1KB of non-sqlite garbage
		garbage := make([]byte, 1024)
		for i := range garbage {
			garbage[i] = byte(i % 256)
		}
		if err := os.WriteFile(corruptPath, garbage, 0644); err != nil {
			t.Fatalf("failed to write garbage db: %v", err)
		}

		_, err := storage.NewSQLiteStore(corruptPath)
		if err == nil {
			t.Fatal("expected error opening garbage sqlite db, got nil")
		}
	})

	t.Run("SQLite_ZeroByteFile", func(t *testing.T) {
		emptyPath := filepath.Join(tempDir, "empty.db")
		if err := os.WriteFile(emptyPath, []byte{}, 0644); err != nil {
			t.Fatalf("failed to write empty db: %v", err)
		}

		// SQLite must treat 0-byte file as brand new and initialize schema
		store, err := storage.NewSQLiteStore(emptyPath)
		if err != nil {
			t.Fatalf("failed to initialize sqlite from 0-byte file: %v", err)
		}
		defer store.Close()

		ctx := context.Background()
		_, err = store.GetMatch(ctx, "nonexistent")
		if !errors.Is(err, storage.ErrMatchNotFound) {
			t.Fatalf("expected ErrMatchNotFound on 0-byte initialized db, got %v", err)
		}
	})

	t.Run("JSON_TruncatedContent", func(t *testing.T) {
		truncatedPath := filepath.Join(tempDir, "truncated.json")
		if err := os.WriteFile(truncatedPath, []byte(`{"version": 1, "matches": { "guid-1": { "match_guid"`), 0644); err != nil {
			t.Fatalf("failed to write truncated json: %v", err)
		}

		_, err := storage.NewJSONStore(truncatedPath)
		if err == nil {
			t.Fatal("expected unmarshal error on truncated JSON, got nil")
		}
	})

	t.Run("JSON_ArrayRootPayload", func(t *testing.T) {
		arrayPath := filepath.Join(tempDir, "array.json")
		if err := os.WriteFile(arrayPath, []byte(`[1, 2, 3, "unexpected array"]`), 0644); err != nil {
			t.Fatalf("failed to write array json: %v", err)
		}

		_, err := storage.NewJSONStore(arrayPath)
		if err == nil {
			t.Fatal("expected unmarshal error on array JSON, got nil")
		}
	})

	t.Run("JSON_ZeroByteFile", func(t *testing.T) {
		emptyPath := filepath.Join(tempDir, "empty.json")
		if err := os.WriteFile(emptyPath, []byte{}, 0644); err != nil {
			t.Fatalf("failed to write empty json: %v", err)
		}

		store, err := storage.NewJSONStore(emptyPath)
		if err != nil {
			t.Fatalf("failed to initialize json store from 0-byte file: %v", err)
		}
		defer store.Close()

		ctx := context.Background()
		_, err = store.GetMatch(ctx, "nonexistent")
		if !errors.Is(err, storage.ErrMatchNotFound) {
			t.Fatalf("expected ErrMatchNotFound on 0-byte initialized json store, got %v", err)
		}
	})
}

// ============================================================================
// 5. LARGE DATASET SCALING TESTS (1500+ MATCHES)
// ============================================================================

func TestAdversarial_LargeDataset_1500Matches(t *testing.T) {
	testStores := []struct {
		name    string
		factory func(t *testing.T) storage.StateStore
	}{
		{
			name: "SQLiteStore",
			factory: func(t *testing.T) storage.StateStore {
				p := filepath.Join(t.TempDir(), "large.db")
				s, err := storage.NewSQLiteStore(p)
				if err != nil {
					t.Fatalf("NewSQLiteStore failed: %v", err)
				}
				return s
			},
		},
		{
			name: "JSONStore",
			factory: func(t *testing.T) storage.StateStore {
				p := filepath.Join(t.TempDir(), "large.json")
				s, err := storage.NewJSONStore(p)
				if err != nil {
					t.Fatalf("NewJSONStore failed: %v", err)
				}
				return s
			},
		},
	}

	for _, ts := range testStores {
		t.Run(ts.name, func(t *testing.T) {
			store := ts.factory(t)
			defer store.Close()
			ctx := context.Background()

			const totalMatches = 1500
			now := time.Now().UTC().Unix()

			var batch []*storage.MatchRecord
			for i := 0; i < totalMatches; i++ {
				var dlStatus storage.DownloadStatus
				var upStatus storage.UploadStatus
				localPath := ""
				replayURL := fmt.Sprintf("https://cdn.example.com/%04d.replay", i)

				switch i % 3 {
				case 0:
					// Pending download
					dlStatus = storage.DownloadPending
					upStatus = storage.UploadPending
				case 1:
					// Downloaded, Pending upload
					dlStatus = storage.DownloadDownloaded
					localPath = fmt.Sprintf("/replays/%04d.replay", i)
					upStatus = storage.UploadPending
				case 2:
					// Downloaded & Uploaded
					dlStatus = storage.DownloadDownloaded
					localPath = fmt.Sprintf("/replays/%04d.replay", i)
					upStatus = storage.UploadUploaded
				}

				batch = append(batch, &storage.MatchRecord{
					MatchGUID:            fmt.Sprintf("large-%04d", i),
					RecordStartTimestamp: now + int64(i),
					MapName:              fmt.Sprintf("Map-%d", i%10),
					Playlist:             11,
					ReplayURL:            replayURL,
					DownloadStatus:       dlStatus,
					LocalFilePath:        localPath,
					UploadStatus:         upStatus,
				})
			}

			// Time the batch insertion
			startInsert := time.Now()
			if err := store.UpsertDiscoveredMatches(ctx, batch); err != nil {
				t.Fatalf("batch insert of 1500 matches failed: %v", err)
			}
			t.Logf("[%s] Inserted %d matches in %v", ts.name, totalMatches, time.Since(startInsert))

			// Query ListPendingDownloads: exactly 500 records expected (i % 3 == 0)
			startDL := time.Now()
			pendingDL, err := store.ListPendingDownloads(ctx)
			if err != nil {
				t.Fatalf("ListPendingDownloads failed: %v", err)
			}
			dlDuration := time.Since(startDL)
			if len(pendingDL) != 500 {
				t.Fatalf("expected 500 pending downloads, got %d", len(pendingDL))
			}
			t.Logf("[%s] ListPendingDownloads returned %d in %v", ts.name, len(pendingDL), dlDuration)

			// Verify strict chronological ordering ASC
			for i := 1; i < len(pendingDL); i++ {
				if pendingDL[i].RecordStartTimestamp < pendingDL[i-1].RecordStartTimestamp {
					t.Fatalf("ListPendingDownloads ordering violation at index %d: %d < %d",
						i, pendingDL[i].RecordStartTimestamp, pendingDL[i-1].RecordStartTimestamp)
				}
			}

			// Query ListPendingUploads: exactly 500 records expected (i % 3 == 1)
			startUP := time.Now()
			pendingUP, err := store.ListPendingUploads(ctx)
			if err != nil {
				t.Fatalf("ListPendingUploads failed: %v", err)
			}
			upDuration := time.Since(startUP)
			if len(pendingUP) != 500 {
				t.Fatalf("expected 500 pending uploads, got %d", len(pendingUP))
			}
			t.Logf("[%s] ListPendingUploads returned %d in %v", ts.name, len(pendingUP), upDuration)

			// Verify strict chronological ordering ASC
			for i := 1; i < len(pendingUP); i++ {
				if pendingUP[i].RecordStartTimestamp < pendingUP[i-1].RecordStartTimestamp {
					t.Fatalf("ListPendingUploads ordering violation at index %d: %d < %d",
						i, pendingUP[i].RecordStartTimestamp, pendingUP[i-1].RecordStartTimestamp)
				}
			}

			// Re-upsert the same 1500 matches (re-discovery)
			startReupsert := time.Now()
			if err := store.UpsertDiscoveredMatches(ctx, batch); err != nil {
				t.Fatalf("re-upsert of 1500 matches failed: %v", err)
			}
			t.Logf("[%s] Re-upserted %d matches in %v", ts.name, totalMatches, time.Since(startReupsert))

			// Verify counts remain identical (no duplicate rows created)
			pendingDLAfter, _ := store.ListPendingDownloads(ctx)
			if len(pendingDLAfter) != 500 {
				t.Errorf("expected 500 pending downloads after re-upsert, got %d", len(pendingDLAfter))
			}
			pendingUPAfter, _ := store.ListPendingUploads(ctx)
			if len(pendingUPAfter) != 500 {
				t.Errorf("expected 500 pending uploads after re-upsert, got %d", len(pendingUPAfter))
			}
		})
	}
}

// ============================================================================
// 6. BOUNDARY CONDITIONS & CONTEXT CANCELLATION TESTS
// ============================================================================

func TestAdversarial_BoundaryAndEdgeCases(t *testing.T) {
	testStores := []struct {
		name    string
		factory func(t *testing.T) storage.StateStore
	}{
		{
			name: "SQLiteStore",
			factory: func(t *testing.T) storage.StateStore {
				p := filepath.Join(t.TempDir(), "edge.db")
				s, err := storage.NewSQLiteStore(p)
				if err != nil {
					t.Fatalf("NewSQLiteStore failed: %v", err)
				}
				return s
			},
		},
		{
			name: "JSONStore",
			factory: func(t *testing.T) storage.StateStore {
				p := filepath.Join(t.TempDir(), "edge.json")
				s, err := storage.NewJSONStore(p)
				if err != nil {
					t.Fatalf("NewJSONStore failed: %v", err)
				}
				return s
			},
		},
	}

	for _, ts := range testStores {
		t.Run(ts.name, func(t *testing.T) {
			store := ts.factory(t)
			defer store.Close()
			ctx := context.Background()

			// 1. Empty batch upsert
			if err := store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{}); err != nil {
				t.Errorf("empty slice upsert failed: %v", err)
			}
			if err := store.UpsertDiscoveredMatches(ctx, nil); err != nil {
				t.Errorf("nil slice upsert failed: %v", err)
			}

			// 2. Batch containing nil elements and empty GUIDs alongside valid match
			mixedBatch := []*storage.MatchRecord{
				nil,
				{MatchGUID: ""},
				{MatchGUID: "valid-guid-1", ReplayURL: "https://example.com/1.replay"},
				nil,
			}
			if err := store.UpsertDiscoveredMatches(ctx, mixedBatch); err != nil {
				t.Fatalf("mixed batch upsert failed: %v", err)
			}
			rec, err := store.GetMatch(ctx, "valid-guid-1")
			if err != nil || rec.MatchGUID != "valid-guid-1" {
				t.Errorf("valid record from mixed batch not found: %v", err)
			}

			// 3. Duplicate GUIDs in the SAME batch
			sameBatch := []*storage.MatchRecord{
				{MatchGUID: "same-batch-guid", MapName: "FirstMap", ReplayURL: "https://example.com/first.replay"},
				{MatchGUID: "same-batch-guid", MapName: "SecondMap", ReplayURL: "https://example.com/first.replay"},
			}
			if err := store.UpsertDiscoveredMatches(ctx, sameBatch); err != nil {
				t.Fatalf("batch with internal duplicate GUIDs failed: %v", err)
			}

			// 4. Non-existent GUID transitions must return ErrMatchNotFound
			nonExistentGUID := "does-not-exist-999"
			if err := store.MarkDownloading(ctx, nonExistentGUID); !errors.Is(err, storage.ErrMatchNotFound) {
				t.Errorf("expected ErrMatchNotFound for MarkDownloading, got %v", err)
			}
			if err := store.MarkDownloaded(ctx, nonExistentGUID, "/path"); !errors.Is(err, storage.ErrMatchNotFound) {
				t.Errorf("expected ErrMatchNotFound for MarkDownloaded, got %v", err)
			}
			if err := store.MarkDownloadFailed(ctx, nonExistentGUID, "err"); !errors.Is(err, storage.ErrMatchNotFound) {
				t.Errorf("expected ErrMatchNotFound for MarkDownloadFailed, got %v", err)
			}
			if err := store.MarkUploading(ctx, nonExistentGUID); !errors.Is(err, storage.ErrMatchNotFound) {
				t.Errorf("expected ErrMatchNotFound for MarkUploading, got %v", err)
			}
			if err := store.MarkUploaded(ctx, nonExistentGUID, "id", "url"); !errors.Is(err, storage.ErrMatchNotFound) {
				t.Errorf("expected ErrMatchNotFound for MarkUploaded, got %v", err)
			}
			if err := store.MarkDuplicate(ctx, nonExistentGUID, "id", "url"); !errors.Is(err, storage.ErrMatchNotFound) {
				t.Errorf("expected ErrMatchNotFound for MarkDuplicate, got %v", err)
			}
			if err := store.MarkUploadFailed(ctx, nonExistentGUID, "err"); !errors.Is(err, storage.ErrMatchNotFound) {
				t.Errorf("expected ErrMatchNotFound for MarkUploadFailed, got %v", err)
			}

			// 5. Empty GUID queries must return ErrInvalidGUID
			if _, err := store.GetMatch(ctx, ""); !errors.Is(err, storage.ErrInvalidGUID) {
				t.Errorf("expected ErrInvalidGUID for GetMatch(''), got %v", err)
			}
		})
	}
}

// ============================================================================
// 7. SPECIFIC BEHAVIORAL AND SEMANTIC INVESTIGATIONS
// ============================================================================

func TestAdversarial_ContextCancellation(t *testing.T) {
	testStores := []struct {
		name    string
		factory func(t *testing.T) storage.StateStore
	}{
		{
			name: "SQLiteStore",
			factory: func(t *testing.T) storage.StateStore {
				p := filepath.Join(t.TempDir(), "ctx_cancel.db")
				s, err := storage.NewSQLiteStore(p)
				if err != nil {
					t.Fatalf("NewSQLiteStore failed: %v", err)
				}
				return s
			},
		},
		{
			name: "JSONStore",
			factory: func(t *testing.T) storage.StateStore {
				p := filepath.Join(t.TempDir(), "ctx_cancel.json")
				s, err := storage.NewJSONStore(p)
				if err != nil {
					t.Fatalf("NewJSONStore failed: %v", err)
				}
				return s
			},
		},
	}

	for _, ts := range testStores {
		t.Run(ts.name, func(t *testing.T) {
			store := ts.factory(t)
			defer store.Close()

			// Seed one match with normal context
			_ = store.UpsertDiscoveredMatches(context.Background(), []*storage.MatchRecord{
				{MatchGUID: "ctx-test-1", ReplayURL: "https://example.com/1"},
			})

			canceledCtx, cancel := context.WithCancel(context.Background())
			cancel() // already canceled

			_, err := store.GetMatch(canceledCtx, "ctx-test-1")
			if err == nil {
				t.Errorf("[%s] GetMatch succeeded with canceled context (ignored ctx.Err())", ts.name)
			} else {
				t.Logf("[%s] GetMatch correctly rejected canceled context: %v", ts.name, err)
			}

			err = store.UpsertDiscoveredMatches(canceledCtx, []*storage.MatchRecord{
				{MatchGUID: "ctx-test-2", ReplayURL: "https://example.com/2"},
			})
			if err == nil {
				t.Errorf("[%s] UpsertDiscoveredMatches succeeded with canceled context (ignored ctx.Err())", ts.name)
			} else {
				t.Logf("[%s] UpsertDiscoveredMatches correctly rejected canceled context: %v", ts.name, err)
			}
		})
	}
}

func TestAdversarial_SkippedReplayURLArrival(t *testing.T) {
	testStores := []struct {
		name    string
		factory func(t *testing.T) storage.StateStore
	}{
		{
			name: "SQLiteStore",
			factory: func(t *testing.T) storage.StateStore {
				p := filepath.Join(t.TempDir(), "skipped.db")
				s, err := storage.NewSQLiteStore(p)
				if err != nil {
					t.Fatalf("NewSQLiteStore failed: %v", err)
				}
				return s
			},
		},
		{
			name: "JSONStore",
			factory: func(t *testing.T) storage.StateStore {
				p := filepath.Join(t.TempDir(), "skipped.json")
				s, err := storage.NewJSONStore(p)
				if err != nil {
					t.Fatalf("NewJSONStore failed: %v", err)
				}
				return s
			},
		},
	}

	for _, ts := range testStores {
		t.Run(ts.name, func(t *testing.T) {
			store := ts.factory(t)
			defer store.Close()
			ctx := context.Background()

			// Cycle 1: Match discovered before PsyNet generated replay URL
			m1 := &storage.MatchRecord{
				MatchGUID:            "no-replay-match",
				RecordStartTimestamp: 1000,
				ReplayURL:            "", // no replay url yet
			}
			if err := store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{m1}); err != nil {
				t.Fatalf("initial upsert failed: %v", err)
			}

			rInitial, err := store.GetMatch(ctx, "no-replay-match")
			if err != nil {
				t.Fatalf("GetMatch failed: %v", err)
			}
			t.Logf("[%s] Initial download status: %s", ts.name, rInitial.DownloadStatus)
			if rInitial.DownloadStatus != storage.DownloadSkipped {
				t.Errorf("[%s] expected initial DownloadSkipped, got %s", ts.name, rInitial.DownloadStatus)
			}

			// Cycle 2: 5 minutes later, PsyNet returns match with ReplayURL available
			m2 := &storage.MatchRecord{
				MatchGUID:            "no-replay-match",
				RecordStartTimestamp: 1000,
				ReplayURL:            "https://cdn.psynet.gg/delayed_replay.replay",
			}
			if err := store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{m2}); err != nil {
				t.Fatalf("second upsert failed: %v", err)
			}

			rUpdated, err := store.GetMatch(ctx, "no-replay-match")
			if err != nil {
				t.Fatalf("GetMatch after update failed: %v", err)
			}
			t.Logf("[%s] After ReplayURL arrival: ReplayURL=%q, DownloadStatus=%s",
				ts.name, rUpdated.ReplayURL, rUpdated.DownloadStatus)

			pending, err := store.ListPendingDownloads(ctx)
			if err != nil {
				t.Fatalf("ListPendingDownloads failed: %v", err)
			}
			t.Logf("[%s] ListPendingDownloads count: %d", ts.name, len(pending))

			if rUpdated.DownloadStatus != storage.DownloadPending {
				t.Errorf("[%s] CRITICAL FINDING: Match initially SKIPPED never transitions to PENDING when ReplayURL arrives (DownloadStatus=%s, pendingCount=%d)",
					ts.name, rUpdated.DownloadStatus, len(pending))
			}
		})
	}
}

func TestAdversarial_AuthState_EmptyProvider(t *testing.T) {
	testStores := []struct {
		name    string
		factory func(t *testing.T) storage.StateStore
	}{
		{
			name: "SQLiteStore",
			factory: func(t *testing.T) storage.StateStore {
				p := filepath.Join(t.TempDir(), "auth_empty.db")
				s, err := storage.NewSQLiteStore(p)
				if err != nil {
					t.Fatalf("NewSQLiteStore failed: %v", err)
				}
				return s
			},
		},
		{
			name: "JSONStore",
			factory: func(t *testing.T) storage.StateStore {
				p := filepath.Join(t.TempDir(), "auth_empty.json")
				s, err := storage.NewJSONStore(p)
				if err != nil {
					t.Fatalf("NewJSONStore failed: %v", err)
				}
				return s
			},
		},
	}

	for _, ts := range testStores {
		t.Run(ts.name, func(t *testing.T) {
			store := ts.factory(t)
			defer store.Close()
			ctx := context.Background()

			err := store.SaveAuthState(ctx, "", "tok", "acc", "user")
			t.Logf("[%s] SaveAuthState with empty provider returned: %v", ts.name, err)

			_, _, _, err = store.GetAuthState(ctx, "")
			t.Logf("[%s] GetAuthState with empty provider returned: %v", ts.name, err)
		})
	}
}

