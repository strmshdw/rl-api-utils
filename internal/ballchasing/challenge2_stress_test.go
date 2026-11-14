package ballchasing

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/testutil"
)

// ----------------------------------------------------------------------------
// Challenge 4: Concurrency Stress Harness (Same Client Instance)
// ----------------------------------------------------------------------------

func TestChallenge2_Concurrency_StressHarness(t *testing.T) {
	for _, streamMode := range []bool{false, true} {
		t.Run(fmt.Sprintf("streaming=%v", streamMode), func(t *testing.T) {
			srv := testutil.NewMockBallchasingServer()
			defer srv.Close()

			// Configure client with shared instance
			client, err := NewClient(ClientConfig{
				BaseURL:      srv.URL(),
				APIKey:       defaultTestToken,
				Visibility:   "public",
				MaxRetries:   3,
				BaseBackoff:  2 * time.Millisecond,
				MaxBackoff:   20 * time.Millisecond,
				StreamUpload: streamMode,
				RateLimiter:  NewRateLimiter(time.Nanosecond),
			})
			if err != nil {
				t.Fatalf("NewClient failed: %v", err)
			}

			// Pre-configure duplicates in mock server
			const dupCount = 10
			for i := 0; i < dupCount; i++ {
				dupGUID := fmt.Sprintf("concur-dup-%d", i)
				existingID := fmt.Sprintf("existing-concur-id-%d", i)
				srv.SetDuplicateGUID(dupGUID, existingID)
			}

			// 50 total goroutines:
			// - 20 fresh uploads -> 201 Created
			// - 10 duplicate uploads -> 409 Conflict (IsDuplicate: true, err == nil)
			// - 10 transient 429 uploads -> retry and succeed
			// - 5 0-byte file uploads -> ErrEmptyFile
			// - 5 non-existent file uploads -> ErrFileNotFound
			const totalGoroutines = 50
			type resultRecord struct {
				index    int
				category string
				guid     string
				res      *UploadResult
				err      error
			}

			results := make([]resultRecord, totalGoroutines)
			var wg sync.WaitGroup
			startGate := make(chan struct{}) // Barrier to release all goroutines simultaneously

			for i := 0; i < totalGoroutines; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					<-startGate // Wait for simultaneous release

					var cat, guid, filePath string

					switch {
					case idx < 20:
						// Normal fresh uploads (201 Created)
						cat = "fresh"
						guid = fmt.Sprintf("concur-fresh-%v-%d", streamMode, idx)
						filePath = helperCreateReplayFile(t, guid, 1024+(idx*128))

					case idx < 30:
						// Duplicate uploads (409 Conflict)
						dupIdx := idx - 20
						cat = "duplicate"
						guid = fmt.Sprintf("concur-dup-%d", dupIdx)
						filePath = helperCreateReplayFile(t, guid, 1024)

					case idx < 40:
						// Rate-limited uploads
						cat = "ratelimit"
						guid = fmt.Sprintf("concur-ratelimit-%v-%d", streamMode, idx)
						filePath = helperCreateReplayFile(t, guid, 1024)

					case idx < 45:
						// 0-byte file
						cat = "0byte"
						guid = fmt.Sprintf("concur-0byte-%d", idx)
						filePath = filepath.Join(t.TempDir(), fmt.Sprintf("%s.replay", guid))
						_ = os.WriteFile(filePath, []byte{}, 0644)

					default:
						// Non-existent file
						cat = "nonexistent"
						guid = fmt.Sprintf("concur-missing-%d", idx)
						filePath = filepath.Join(t.TempDir(), fmt.Sprintf("%s.replay", guid))
					}

					res, err := client.UploadReplay(context.Background(), guid, filePath)
					results[idx] = resultRecord{
						index:    idx,
						category: cat,
						guid:     guid,
						res:      res,
						err:      err,
					}
				}(i)
			}

			// Release all goroutines simultaneously
			close(startGate)
			wg.Wait()

			// Verify results for all 50 concurrent goroutines
			for _, r := range results {
				switch r.category {
				case "fresh":
					if r.err != nil {
						t.Errorf("[fresh %d] unexpected error: %v", r.index, r.err)
					}
					if r.res == nil || r.res.ID == "" {
						t.Errorf("[fresh %d] expected valid UploadResult, got: %+v", r.index, r.res)
					} else if r.res.IsDuplicate {
						t.Errorf("[fresh %d] expected IsDuplicate == false", r.index)
					}

				case "duplicate":
					if r.err != nil {
						t.Errorf("[duplicate %d] expected err == nil on 409, got: %v", r.index, r.err)
					}
					if r.res == nil {
						t.Errorf("[duplicate %d] expected non-nil result", r.index)
					} else {
						if !r.res.IsDuplicate {
							t.Errorf("[duplicate %d] expected IsDuplicate == true", r.index)
						}
						dupIdx := r.index - 20
						expectedID := fmt.Sprintf("existing-concur-id-%d", dupIdx)
						if r.res.ID != expectedID {
							t.Errorf("[duplicate %d] expected ID %s, got %s", r.index, expectedID, r.res.ID)
						}
					}

				case "ratelimit":
					if r.err != nil {
						t.Errorf("[ratelimit %d] unexpected error: %v", r.index, r.err)
					}
					if r.res == nil || r.res.ID == "" {
						t.Errorf("[ratelimit %d] expected valid UploadResult, got %+v", r.index, r.res)
					}

				case "0byte":
					if r.err == nil {
						t.Errorf("[0byte %d] expected error for 0-byte file, got nil", r.index)
					}
					if !errors.Is(r.err, ErrEmptyFile) {
						t.Errorf("[0byte %d] expected ErrEmptyFile, got: %v", r.index, r.err)
					}
					if r.res != nil {
						t.Errorf("[0byte %d] expected nil result, got %+v", r.index, r.res)
					}

				case "nonexistent":
					if r.err == nil {
						t.Errorf("[nonexistent %d] expected error for missing file, got nil", r.index)
					}
					if !errors.Is(r.err, ErrFileNotFound) {
						t.Errorf("[nonexistent %d] expected ErrFileNotFound, got: %v", r.index, r.err)
					}
					if r.res != nil {
						t.Errorf("[nonexistent %d] expected nil result, got %+v", r.index, r.res)
					}
				}
			}

			// Verify payload integrity for uploaded replays:
			// Each fresh upload recorded on mock server must match its matchGUID and TAGAME magic bytes
			for i := 0; i < 20; i++ {
				guid := fmt.Sprintf("concur-fresh-%v-%d", streamMode, i)
				rec := srv.GetUpload(guid)
				if rec == nil {
					t.Errorf("mock server missing upload record for %s", guid)
					continue
				}
				expectedSize := int64(1024 + (i * 128))
				if rec.FileSize != expectedSize {
					t.Errorf("[%s] expected size %d, got %d", guid, expectedSize, rec.FileSize)
				}
				if !bytes.HasPrefix(rec.FileBytes, testutil.ReplayMagicBytes) {
					t.Errorf("[%s] payload corrupted, missing TAGAME prefix", guid)
				}
			}
		})
	}
}
