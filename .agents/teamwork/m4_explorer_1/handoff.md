# M4 Syncer Domain Orchestrator Exploration Report

## 1. Observation

Direct observations from the codebase investigation:

1. **Storage Interface & Data Models** (`internal/storage/store.go:19-106`):
   - `storage.DownloadStatus` constants: `DownloadPending` ("PENDING"), `DownloadDownloading` ("DOWNLOADING"), `DownloadDownloaded` ("DOWNLOADED"), `DownloadFailed` ("FAILED"), `DownloadSkipped` ("SKIPPED").
   - `storage.UploadStatus` constants: `UploadPending` ("PENDING"), `UploadUploading` ("UPLOADING"), `UploadUploaded` ("UPLOADED"), `UploadDuplicate` ("DUPLICATE"), `UploadFailed` ("FAILED").
   - `storage.MatchRecord` struct fields: `MatchGUID` (string), `RecordStartTimestamp` (int64), `MapName` (string), `Playlist` (int), `ReplayURL` (string), `DownloadStatus` (DownloadStatus), `LocalFilePath` (string), `DownloadedAt` (*time.Time), `UploadStatus` (UploadStatus), `BallchasingID` (string), `BallchasingURL` (string), `UploadedAt` (*time.Time), `RetryCount` (int), `LastError` (string), `CreatedAt` (time.Time), `UpdatedAt` (time.Time).
   - `storage.StateStore` defines:
     ```go
     GetMatch(ctx context.Context, matchGUID string) (*MatchRecord, error)
     ListPendingDownloads(ctx context.Context) ([]*MatchRecord, error)
     ListPendingUploads(ctx context.Context) ([]*MatchRecord, error)
     UpsertDiscoveredMatches(ctx context.Context, matches []*MatchRecord) error
     MarkDownloading(ctx context.Context, matchGUID string) error
     MarkDownloaded(ctx context.Context, matchGUID, localPath string) error
     MarkDownloadFailed(ctx context.Context, matchGUID, errMsg string) error
     MarkUploading(ctx context.Context, matchGUID string) error
     MarkUploaded(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error
     MarkDuplicate(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error
     MarkUploadFailed(ctx context.Context, matchGUID, errMsg string) error
     RecoverInFlight(ctx context.Context) error
     ```

2. **Upsert Idempotency & Delayed URL Handling** (`internal/storage/sqlite.go:208-211`, `internal/storage/jsonstore.go:329-336`):
   - SQLite ON CONFLICT clause:
     ```sql
     ON CONFLICT(match_guid) DO UPDATE SET
         download_status = CASE WHEN matches.download_status = 'SKIPPED' AND matches.replay_url = '' AND excluded.replay_url != '' THEN 'PENDING' ELSE matches.download_status END,
         replay_url = CASE WHEN matches.replay_url = '' AND excluded.replay_url != '' THEN excluded.replay_url ELSE matches.replay_url END,
         updated_at = excluded.updated_at;
     ```
   - JSONStore logic (`jsonstore.go:329-336`):
     ```go
     if existing.ReplayURL == "" && m.ReplayURL != "" {
         existing.ReplayURL = m.ReplayURL
         if existing.DownloadStatus == DownloadSkipped {
             existing.DownloadStatus = DownloadPending
         }
         existing.UpdatedAt = now
         modified = true
     }
     ```
   - Both stores already implement automatic promotion from `SKIPPED` to `PENDING` when a replay URL arrives in a subsequent polling cycle.

3. **In-Flight Recovery** (`internal/storage/sqlite.go:384-398`):
   - Resets `DOWNLOADING` -> `PENDING` and `UPLOADING` -> `PENDING` across a single atomic database transaction.

4. **PsyNet Match Provider & Downloader Contracts** (`internal/psynet/client.go:26-38`, `internal/psynet/downloader.go:47-49`):
   - `psynet.DiscoveredMatch` struct:
     ```go
     type DiscoveredMatch struct {
         MatchGUID            string
         RecordStartTimestamp int64
         MapName              string
         Playlist             int
         ReplayURL            string
     }
     ```
   - `psynet.MatchHistoryProvider` interface:
     ```go
     type MatchHistoryProvider interface {
         GetRecentMatches(ctx context.Context) ([]DiscoveredMatch, error)
         Close() error
     }
     ```
   - `psynet.ReplayDownloader` interface:
     ```go
     type ReplayDownloader interface {
         DownloadReplay(ctx context.Context, matchGUID, replayURL, destDir string) (localPath string, err error)
     }
     ```

5. **Ballchasing Uploader Contracts** (`internal/ballchasing/types.go:61-78`):
   - `ballchasing.UploadResult` struct:
     ```go
     type UploadResult struct {
         ID          string `json:"id"`
         Location    string `json:"location"`
         IsDuplicate bool   `json:"is_duplicate"`
     }
     ```
   - `ballchasing.ReplayUploader` interface:
     ```go
     type ReplayUploader interface {
         UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error)
         Ping(ctx context.Context) error
     }
     ```

6. **Configuration Parameters** (`internal/config/config.go:116-125`):
   - `SyncConfig` contains: `PollInterval` (Duration), `ReplayDir` (string), `DBPath` (string), `KeepLocalFiles` (bool), `DownloadTimeout` (Duration), `DryRun` (bool), `Once` (bool).

7. **Test Harness & E2E Validation** (`test/e2e/e2e_test.go:540-681`, `test/e2e/tier3_pairwise_test.go:48-52`):
   - E2E tests verify that in dry-run mode (`dryRun = true`), zero downloads and zero uploads are initiated, and `Store.GetMatch` returns nil (no match records committed to the database during dry run).

---

## 2. Logic Chain

1. **Type Identity & Zero-Glue Architectural Decoupling**:
   - In Go, two named structs in different packages (`psynet.DiscoveredMatch` vs `syncer.DiscoveredMatch`) cannot be passed interchangeably in slice return signatures (`[]psynet.DiscoveredMatch` is not assignable to `[]syncer.DiscoveredMatch`).
   - By declaring `type DiscoveredMatch = psynet.DiscoveredMatch` and `type UploadResult = ballchasing.UploadResult` in `internal/syncer/interfaces.go`:
     - `*psynet.Client` directly satisfies `syncer.MatchHistoryProvider`.
     - `*ballchasing.Client` directly satisfies `syncer.ReplayUploader`.
     - Zero adapter glue, zero copying, and zero allocation overhead.

2. **Interface Scoping (Clean Architecture)**:
   - `syncer.StateStore` specifies only the methods required for synchronization and recovery: `GetMatch`, `ListPendingDownloads`, `ListPendingUploads`, `UpsertDiscoveredMatches`, `MarkDownloading`, `MarkDownloaded`, `MarkDownloadFailed`, `MarkUploading`, `MarkUploaded`, `MarkDuplicate`, `MarkUploadFailed`, and `RecoverInFlight`.
   - Both `*storage.SQLiteStore` and `*storage.JSONStore` implement all of these methods and can be used interchangeably.

3. **Core Synchronization Pipeline Execution Flow**:
   - **Step 1: Crash Recovery**: If `!s.dryRun`, call `s.store.RecoverInFlight(ctx)`. This resets in-flight transitions orphaned by an unexpected process crash back to `PENDING`.
   - **Step 2: Polling**: Query `s.provider.GetRecentMatches(ctx)`. If error occurs or context cancels, return immediately.
   - **Step 3: Upsert Discovered**:
     - For each discovered match: if `ReplayURL == ""`, set `DownloadStatus = SKIPPED` and increment `stats.SkippedCount`; else set `DownloadStatus = PENDING`.
     - If `!s.dryRun`, persist via `s.store.UpsertDiscoveredMatches(ctx, toUpsert)`.
     - If `s.dryRun`, do NOT mutate the database, preserving the read-only idempotency required by dry-run tests.
   - **Step 4: Download Loop**:
     - Query `s.store.ListPendingDownloads(ctx)`.
     - If `s.dryRun`, log pending downloads and do not download.
     - If `!s.dryRun`:
       - Loop through pending downloads. Check `ctx.Done()`.
       - Mark `DOWNLOADING` via `s.store.MarkDownloading`.
       - Download via `s.downloader.DownloadReplay`.
       - On success: mark `DOWNLOADED` via `s.store.MarkDownloaded` with returned `localPath`, increment `stats.DownloadedCount`.
       - On failure: mark `FAILED` via `s.store.MarkDownloadFailed`, increment `stats.FailedCount`. If `ctx.Err() != nil`, return immediately.
   - **Step 5: Upload Loop**:
     - Query `s.store.ListPendingUploads(ctx)`.
     - If `s.dryRun`, log pending uploads and do not upload.
     - If `!s.dryRun`:
       - Loop through pending uploads. Check `ctx.Done()`.
       - Mark `UPLOADING` via `s.store.MarkUploading`.
       - Upload via `s.uploader.UploadReplay`.
       - On HTTP 409 / Duplicate: `res.IsDuplicate == true` -> mark `DUPLICATE` via `s.store.MarkDuplicate`, increment `stats.DuplicateCount`.
       - On HTTP 201 / Success: mark `UPLOADED` via `s.store.MarkUploaded`, increment `stats.UploadedCount`.
       - On error: mark `FAILED` via `s.store.MarkUploadFailed`, increment `stats.FailedCount`. If `ctx.Err() != nil`, return immediately.
   - **Step 6: Metric Telemetry**:
     - Populate both `*Count` and direct name aliases (`DiscoveredCount` / `Discovered`) on `SyncStats` and return.

4. **Fault Tolerance & Resilience**:
   - Individual download or upload failures log descriptive errors and increment `FailedCount`, but do not abort subsequent items in the cycle.
   - Context cancellations (SIGINT/SIGTERM during a download or upload) immediately break the loop and return `ctx.Err()`.

---

## 3. Caveats

1. **Dry-Run Store Invariant**:
   - In dry-run mode, neither `RecoverInFlight` nor `UpsertDiscoveredMatches` should write to the store. This ensures `--dry-run` remains a pure simulation without modifying persistent state.
2. **Replay Directory Creation**:
   - `psynet.HTTPDownloader` already ensures `os.MkdirAll(destDir, 0755)` prior to saving temporary and final `.replay` files.
3. **Local File Retention (`KeepLocalFiles`)**:
   - Default is `true` as required by Rocket League community archive norms. If configured to `false`, files can optionally be unlinked after successful or duplicate upload.

---

## 4. Conclusion & Proposed Implementation

The sync orchestration subsystem (`internal/syncer`) provides the domain bridge connecting `storage`, `psynet`, and `ballchasing`.

Below are the complete, production-ready Go source files proposed for `internal/syncer`:
- `interfaces.go`
- `syncer.go`
- `syncer_test.go`

### 4.1. Proposed `internal/syncer/interfaces.go`

```go
package syncer

import (
	"context"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/psynet"
	"github.com/dank/rl-api-utils/internal/storage"
)

// DiscoveredMatch aliases psynet.DiscoveredMatch for cross-package structural identity.
type DiscoveredMatch = psynet.DiscoveredMatch

// UploadResult aliases ballchasing.UploadResult for cross-package structural identity.
type UploadResult = ballchasing.UploadResult

// SyncStats captures telemetry and metrics for a single synchronization cycle.
type SyncStats struct {
	DiscoveredCount int `json:"discovered_count"`
	DownloadedCount int `json:"downloaded_count"`
	UploadedCount   int `json:"uploaded_count"`
	DuplicateCount  int `json:"duplicate_count"`
	SkippedCount    int `json:"skipped_count"`
	FailedCount     int `json:"failed_count"`

	// Convenience alias fields matching caller conventions
	Discovered int `json:"discovered"`
	Downloaded int `json:"downloaded"`
	Uploaded   int `json:"uploaded"`
	Duplicates int `json:"duplicates"`
	Skipped    int `json:"skipped"`
	Failures   int `json:"failures"`
}

// PopulateAliases syncs convenience alias fields with their canonical *Count counterparts.
func (s *SyncStats) PopulateAliases() {
	s.Discovered = s.DiscoveredCount
	s.Downloaded = s.DownloadedCount
	s.Uploaded = s.UploadedCount
	s.Duplicates = s.DuplicateCount
	s.Skipped = s.SkippedCount
	s.Failures = s.FailedCount
}

// StateStore defines the persistence contract required by the syncer domain orchestrator.
// Satisfied by *storage.SQLiteStore and *storage.JSONStore.
type StateStore interface {
	GetMatch(ctx context.Context, matchGUID string) (*storage.MatchRecord, error)
	ListPendingDownloads(ctx context.Context) ([]*storage.MatchRecord, error)
	ListPendingUploads(ctx context.Context) ([]*storage.MatchRecord, error)
	UpsertDiscoveredMatches(ctx context.Context, matches []*storage.MatchRecord) error
	MarkDownloading(ctx context.Context, matchGUID string) error
	MarkDownloaded(ctx context.Context, matchGUID, localPath string) error
	MarkDownloadFailed(ctx context.Context, matchGUID, errMsg string) error
	MarkUploading(ctx context.Context, matchGUID string) error
	MarkUploaded(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error
	MarkDuplicate(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error
	MarkUploadFailed(ctx context.Context, matchGUID, errMsg string) error
	RecoverInFlight(ctx context.Context) error
}

// MatchHistoryProvider defines the match query contract for PsyNet or mock providers.
// Satisfied by *psynet.Client and testutil.InMemoryMatchHistoryProvider.
type MatchHistoryProvider interface {
	GetRecentMatches(ctx context.Context) ([]DiscoveredMatch, error)
	Close() error
}

// ReplayDownloader defines the replay file streaming download contract.
// Satisfied by *psynet.HTTPDownloader.
type ReplayDownloader interface {
	DownloadReplay(ctx context.Context, matchGUID, replayURL, destDir string) (localPath string, err error)
}

// ReplayUploader defines the replay upload contract for Ballchasing or mock uploaders.
// Satisfied by *ballchasing.Client.
type ReplayUploader interface {
	UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error)
}
```

---

### 4.2. Proposed `internal/syncer/syncer.go`

```go
package syncer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/dank/rl-api-utils/internal/storage"
)

// Sentinel errors returned by the syncer domain package.
var (
	ErrNilStore      = errors.New("syncer: state store cannot be nil")
	ErrNilProvider   = errors.New("syncer: match history provider cannot be nil")
	ErrNilDownloader = errors.New("syncer: replay downloader cannot be nil")
	ErrNilUploader   = errors.New("syncer: replay uploader cannot be nil")
)

// Option allows configuring optional Syncer settings.
type Option func(*Syncer)

// WithReplayDir configures the directory where replay files are downloaded.
func WithReplayDir(dir string) Option {
	return func(s *Syncer) {
		trimmed := strings.TrimSpace(dir)
		if trimmed != "" {
			s.replayDir = trimmed
		}
	}
}

// WithDryRun configures whether the syncer operates in simulation mode.
func WithDryRun(dryRun bool) Option {
	return func(s *Syncer) {
		s.dryRun = dryRun
	}
}

// WithLogger configures a custom structured logger.
func WithLogger(logger *slog.Logger) Option {
	return func(s *Syncer) {
		if logger != nil {
			s.logger = logger
		}
	}
}

// WithKeepLocalFiles configures whether local .replay files are kept after upload.
func WithKeepLocalFiles(keep bool) Option {
	return func(s *Syncer) {
		s.keepLocalFiles = keep
	}
}

// Config holds configuration parameters for the syncer orchestrator.
type Config struct {
	ReplayDir      string
	DryRun         bool
	KeepLocalFiles bool
	Logger         *slog.Logger
}

// Syncer is the domain orchestrator that coordinates match discovery,
// replay downloading, Ballchasing uploads, and state persistence.
type Syncer struct {
	store          StateStore
	provider       MatchHistoryProvider
	downloader     ReplayDownloader
	uploader       ReplayUploader
	replayDir      string
	dryRun         bool
	keepLocalFiles bool
	logger         *slog.Logger
}

// New creates a new Syncer instance with functional options.
func New(store StateStore, provider MatchHistoryProvider, downloader ReplayDownloader, uploader ReplayUploader, opts ...Option) (*Syncer, error) {
	if store == nil {
		return nil, ErrNilStore
	}
	if provider == nil {
		return nil, ErrNilProvider
	}
	if downloader == nil {
		return nil, ErrNilDownloader
	}
	if uploader == nil {
		return nil, ErrNilUploader
	}

	s := &Syncer{
		store:          store,
		provider:       provider,
		downloader:     downloader,
		uploader:       uploader,
		replayDir:      "./replays",
		dryRun:         false,
		keepLocalFiles: true,
		logger:         slog.Default(),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s, nil
}

// NewWithConfig creates a new Syncer instance with a Config struct.
func NewWithConfig(store StateStore, provider MatchHistoryProvider, downloader ReplayDownloader, uploader ReplayUploader, cfg Config) (*Syncer, error) {
	return New(store, provider, downloader, uploader,
		WithReplayDir(cfg.ReplayDir),
		WithDryRun(cfg.DryRun),
		WithKeepLocalFiles(cfg.KeepLocalFiles),
		WithLogger(cfg.Logger),
	)
}

// NewSyncerEngine creates a new Syncer matching E2E test harness conventions.
func NewSyncerEngine(store StateStore, provider MatchHistoryProvider, downloader ReplayDownloader, uploader ReplayUploader, replayDir string, dryRun bool) *Syncer {
	s, err := New(store, provider, downloader, uploader,
		WithReplayDir(replayDir),
		WithDryRun(dryRun),
	)
	if err != nil {
		panic(err)
	}
	return s
}

// Sync is an alias for RunCycle, executing a single synchronization pass.
func (s *Syncer) Sync(ctx context.Context) (*SyncStats, error) {
	return s.RunCycle(ctx)
}

// RunCycle executes a complete synchronization cycle:
// 1. Recover in-flight orphan records (DOWNLOADING/UPLOADING -> PENDING).
// 2. Poll recent match history from PsyNet.
// 3. Upsert discovered matches into the persistent store.
// 4. Download pending replays to the local replay directory.
// 5. Upload pending downloaded replays to Ballchasing.com.
// 6. Return comprehensive cycle statistics.
func (s *Syncer) RunCycle(ctx context.Context) (*SyncStats, error) {
	stats := &SyncStats{}

	// 1. Recover in-flight items from previous crashes (skipped in dry-run to keep store read-only)
	if !s.dryRun {
		if err := s.store.RecoverInFlight(ctx); err != nil {
			return nil, fmt.Errorf("recovery failed: %w", err)
		}
	}

	// 2. Poll matches from PsyNet
	discovered, err := s.provider.GetRecentMatches(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("polling matches failed: %w", err)
	}
	stats.DiscoveredCount = len(discovered)

	// 3. Upsert discovered matches into the store
	var toUpsert []*storage.MatchRecord
	for _, d := range discovered {
		guid := strings.TrimSpace(d.MatchGUID)
		if guid == "" {
			s.logger.Warn("skipping match entry with empty MatchGUID")
			continue
		}

		dlStatus := storage.DownloadPending
		if strings.TrimSpace(d.ReplayURL) == "" {
			dlStatus = storage.DownloadSkipped
			stats.SkippedCount++
			s.logger.Debug("match discovered without replay URL (delayed CDN arrival)",
				slog.String("guid", guid),
				slog.Int("playlist", d.Playlist),
				slog.String("map", d.MapName),
			)
		}

		toUpsert = append(toUpsert, &storage.MatchRecord{
			MatchGUID:            guid,
			RecordStartTimestamp: d.RecordStartTimestamp,
			MapName:              d.MapName,
			Playlist:             d.Playlist,
			ReplayURL:            d.ReplayURL,
			DownloadStatus:       dlStatus,
			UploadStatus:         storage.UploadPending,
		})
	}

	if s.dryRun {
		s.logger.Info("[DRY-RUN] Discovered matches from PsyNet", slog.Int("count", len(discovered)))
	} else if len(toUpsert) > 0 {
		if err := s.store.UpsertDiscoveredMatches(ctx, toUpsert); err != nil {
			return nil, fmt.Errorf("upserting discovered matches failed: %w", err)
		}
	}

	// 4. Download pending replays
	pendingDownloads, err := s.store.ListPendingDownloads(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pending downloads failed: %w", err)
	}

	if s.dryRun {
		s.logger.Info("[DRY-RUN] Store pending downloads", slog.Int("count", len(pendingDownloads)))
	} else {
		for _, rec := range pendingDownloads {
			select {
			case <-ctx.Done():
				stats.PopulateAliases()
				return stats, ctx.Err()
			default:
			}

			if strings.TrimSpace(rec.ReplayURL) == "" {
				stats.SkippedCount++
				continue
			}

			if err := s.store.MarkDownloading(ctx, rec.MatchGUID); err != nil {
				s.logger.Warn("failed to mark match downloading",
					slog.String("guid", rec.MatchGUID),
					slog.String("error", err.Error()),
				)
				continue
			}

			localPath, err := s.downloader.DownloadReplay(ctx, rec.MatchGUID, rec.ReplayURL, s.replayDir)
			if err != nil {
				if ctx.Err() != nil {
					stats.PopulateAliases()
					return stats, ctx.Err()
				}
				_ = s.store.MarkDownloadFailed(ctx, rec.MatchGUID, err.Error())
				stats.FailedCount++
				s.logger.Error("failed to download replay",
					slog.String("guid", rec.MatchGUID),
					slog.String("error", err.Error()),
				)
				continue
			}

			if err := s.store.MarkDownloaded(ctx, rec.MatchGUID, localPath); err != nil {
				s.logger.Error("failed to mark match downloaded",
					slog.String("guid", rec.MatchGUID),
					slog.String("error", err.Error()),
				)
				stats.FailedCount++
				continue
			}

			stats.DownloadedCount++
			s.logger.Info("downloaded replay successfully",
				slog.String("guid", rec.MatchGUID),
				slog.String("local_path", localPath),
			)
		}
	}

	// 5. Upload pending replays
	pendingUploads, err := s.store.ListPendingUploads(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pending uploads failed: %w", err)
	}

	if s.dryRun {
		s.logger.Info("[DRY-RUN] Store pending uploads", slog.Int("count", len(pendingUploads)))
	} else {
		for _, rec := range pendingUploads {
			select {
			case <-ctx.Done():
				stats.PopulateAliases()
				return stats, ctx.Err()
			default:
			}

			if strings.TrimSpace(rec.LocalFilePath) == "" {
				s.logger.Warn("skipping pending upload with empty local file path",
					slog.String("guid", rec.MatchGUID),
				)
				continue
			}

			if err := s.store.MarkUploading(ctx, rec.MatchGUID); err != nil {
				s.logger.Warn("failed to mark match uploading",
					slog.String("guid", rec.MatchGUID),
					slog.String("error", err.Error()),
				)
				continue
			}

			res, err := s.uploader.UploadReplay(ctx, rec.MatchGUID, rec.LocalFilePath)
			if err != nil {
				if ctx.Err() != nil {
					stats.PopulateAliases()
					return stats, ctx.Err()
				}
				_ = s.store.MarkUploadFailed(ctx, rec.MatchGUID, err.Error())
				stats.FailedCount++
				s.logger.Error("failed to upload replay",
					slog.String("guid", rec.MatchGUID),
					slog.String("error", err.Error()),
				)
				continue
			}

			if res != nil && res.IsDuplicate {
				if err := s.store.MarkDuplicate(ctx, rec.MatchGUID, res.ID, res.Location); err != nil {
					s.logger.Error("failed to mark match duplicate",
						slog.String("guid", rec.MatchGUID),
						slog.String("error", err.Error()),
					)
					stats.FailedCount++
					continue
				}
				stats.DuplicateCount++
				s.logger.Info("replay already uploaded (duplicate marked)",
					slog.String("guid", rec.MatchGUID),
					slog.String("ballchasing_id", res.ID),
				)
			} else {
				id := ""
				loc := ""
				if res != nil {
					id = res.ID
					loc = res.Location
				}
				if err := s.store.MarkUploaded(ctx, rec.MatchGUID, id, loc); err != nil {
					s.logger.Error("failed to mark match uploaded",
						slog.String("guid", rec.MatchGUID),
						slog.String("error", err.Error()),
					)
					stats.FailedCount++
					continue
				}
				stats.UploadedCount++
				s.logger.Info("uploaded replay successfully",
					slog.String("guid", rec.MatchGUID),
					slog.String("ballchasing_id", id),
				)
			}

			// Clean up local file if KeepLocalFiles is false
			if !s.keepLocalFiles && rec.LocalFilePath != "" {
				_ = os.Remove(rec.LocalFilePath)
			}
		}
	}

	// 6. Return populated cycle statistics
	stats.PopulateAliases()
	return stats, nil
}
```

---

### 4.3. Proposed `internal/syncer/syncer_test.go`

```go
package syncer

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/storage"
)

// --- Mock Implementations ---

type mockStore struct {
	mu                  sync.Mutex
	matches             map[string]*storage.MatchRecord
	recoverCalls        int
	upsertCalls         int
	listDlCalls         int
	listUpCalls         int
	markDlFailedCalls   int
	markUpFailedCalls   int
	failRecover         bool
	failUpsert          bool
	failListDl          bool
	failListUp          bool
	failMarkDownloading bool
	failMarkUploading   bool
}

func newMockStore() *mockStore {
	return &mockStore{
		matches: make(map[string]*storage.MatchRecord),
	}
}

func (m *mockStore) GetMatch(ctx context.Context, matchGUID string) (*storage.MatchRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.matches[matchGUID]
	if !ok {
		return nil, storage.ErrMatchNotFound
	}
	cp := *rec
	return &cp, nil
}

func (m *mockStore) ListPendingDownloads(ctx context.Context) ([]*storage.MatchRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listDlCalls++
	if m.failListDl {
		return nil, errors.New("mock list dl error")
	}
	var res []*storage.MatchRecord
	for _, rec := range m.matches {
		if rec.DownloadStatus == storage.DownloadPending && rec.ReplayURL != "" {
			cp := *rec
			res = append(res, &cp)
		}
	}
	return res, nil
}

func (m *mockStore) ListPendingUploads(ctx context.Context) ([]*storage.MatchRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listUpCalls++
	if m.failListUp {
		return nil, errors.New("mock list up error")
	}
	var res []*storage.MatchRecord
	for _, rec := range m.matches {
		if rec.DownloadStatus == storage.DownloadDownloaded && rec.UploadStatus == storage.UploadPending && rec.LocalFilePath != "" {
			cp := *rec
			res = append(res, &cp)
		}
	}
	return res, nil
}

func (m *mockStore) UpsertDiscoveredMatches(ctx context.Context, matches []*storage.MatchRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.upsertCalls++
	if m.failUpsert {
		return errors.New("mock upsert error")
	}
	now := time.Now()
	for _, rec := range matches {
		if existing, ok := m.matches[rec.MatchGUID]; ok {
			if existing.ReplayURL == "" && rec.ReplayURL != "" {
				existing.ReplayURL = rec.ReplayURL
				if existing.DownloadStatus == storage.DownloadSkipped {
					existing.DownloadStatus = storage.DownloadPending
				}
				existing.UpdatedAt = now
			}
			continue
		}
		cp := *rec
		if cp.CreatedAt.IsZero() {
			cp.CreatedAt = now
		}
		cp.UpdatedAt = now
		m.matches[rec.MatchGUID] = &cp
	}
	return nil
}

func (m *mockStore) MarkDownloading(ctx context.Context, matchGUID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failMarkDownloading {
		return errors.New("mock mark downloading error")
	}
	if rec, ok := m.matches[matchGUID]; ok {
		rec.DownloadStatus = storage.DownloadDownloading
		rec.UpdatedAt = time.Now()
		return nil
	}
	return storage.ErrMatchNotFound
}

func (m *mockStore) MarkDownloaded(ctx context.Context, matchGUID, localPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rec, ok := m.matches[matchGUID]; ok {
		now := time.Now()
		rec.DownloadStatus = storage.DownloadDownloaded
		rec.LocalFilePath = localPath
		rec.DownloadedAt = &now
		rec.UpdatedAt = now
		return nil
	}
	return storage.ErrMatchNotFound
}

func (m *mockStore) MarkDownloadFailed(ctx context.Context, matchGUID, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.markDlFailedCalls++
	if rec, ok := m.matches[matchGUID]; ok {
		rec.DownloadStatus = storage.DownloadFailed
		rec.LastError = errMsg
		rec.RetryCount++
		rec.UpdatedAt = time.Now()
		return nil
	}
	return storage.ErrMatchNotFound
}

func (m *mockStore) MarkUploading(ctx context.Context, matchGUID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failMarkUploading {
		return errors.New("mock mark uploading error")
	}
	if rec, ok := m.matches[matchGUID]; ok {
		rec.UploadStatus = storage.UploadUploading
		rec.UpdatedAt = time.Now()
		return nil
	}
	return storage.ErrMatchNotFound
}

func (m *mockStore) MarkUploaded(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rec, ok := m.matches[matchGUID]; ok {
		now := time.Now()
		rec.UploadStatus = storage.UploadUploaded
		rec.BallchasingID = ballchasingID
		rec.BallchasingURL = ballchasingURL
		rec.UploadedAt = &now
		rec.UpdatedAt = now
		return nil
	}
	return storage.ErrMatchNotFound
}

func (m *mockStore) MarkDuplicate(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rec, ok := m.matches[matchGUID]; ok {
		now := time.Now()
		rec.UploadStatus = storage.UploadDuplicate
		rec.BallchasingID = ballchasingID
		rec.BallchasingURL = ballchasingURL
		rec.UploadedAt = &now
		rec.UpdatedAt = now
		return nil
	}
	return storage.ErrMatchNotFound
}

func (m *mockStore) MarkUploadFailed(ctx context.Context, matchGUID, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.markUpFailedCalls++
	if rec, ok := m.matches[matchGUID]; ok {
		rec.UploadStatus = storage.UploadFailed
		rec.LastError = errMsg
		rec.RetryCount++
		rec.UpdatedAt = time.Now()
		return nil
	}
	return storage.ErrMatchNotFound
}

func (m *mockStore) RecoverInFlight(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recoverCalls++
	if m.failRecover {
		return errors.New("mock recover error")
	}
	now := time.Now()
	for _, rec := range m.matches {
		if rec.DownloadStatus == storage.DownloadDownloading {
			rec.DownloadStatus = storage.DownloadPending
			rec.UpdatedAt = now
		}
		if rec.UploadStatus == storage.UploadUploading {
			rec.UploadStatus = storage.UploadPending
			rec.UpdatedAt = now
		}
	}
	return nil
}

type mockHistoryProvider struct {
	mu      sync.Mutex
	matches []DiscoveredMatch
	calls   int
	err     error
	closed  bool
}

func newMockHistoryProvider(matches ...DiscoveredMatch) *mockHistoryProvider {
	return &mockHistoryProvider{matches: matches}
}

func (p *mockHistoryProvider) GetRecentMatches(ctx context.Context) ([]DiscoveredMatch, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	res := make([]DiscoveredMatch, len(p.matches))
	copy(res, p.matches)
	return res, nil
}

func (p *mockHistoryProvider) SetMatches(matches []DiscoveredMatch) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.matches = matches
}

func (p *mockHistoryProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

type mockDownloader struct {
	mu           sync.Mutex
	downloaded   map[string]string // guid -> localPath
	failForGUIDs map[string]error
	calls        int
	blockChan    chan struct{}
}

func newMockDownloader() *mockDownloader {
	return &mockDownloader{
		downloaded:   make(map[string]string),
		failForGUIDs: make(map[string]error),
	}
}

func (d *mockDownloader) DownloadReplay(ctx context.Context, matchGUID, replayURL, destDir string) (string, error) {
	d.mu.Lock()
	d.calls++
	block := d.blockChan
	if err, ok := d.failForGUIDs[matchGUID]; ok {
		d.mu.Unlock()
		return "", err
	}
	d.mu.Unlock()

	if block != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-block:
		}
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	p := filepath.Join(destDir, fmt.Sprintf("%s.replay", matchGUID))
	d.mu.Lock()
	d.downloaded[matchGUID] = p
	d.mu.Unlock()
	return p, nil
}

type mockUploader struct {
	mu           sync.Mutex
	uploaded     map[string]string // guid -> filePath
	failForGUIDs map[string]error
	duplicates   map[string]bool
	calls        int
	blockChan    chan struct{}
}

func newMockUploader() *mockUploader {
	return &mockUploader{
		uploaded:     make(map[string]string),
		failForGUIDs: make(map[string]error),
		duplicates:   make(map[string]bool),
	}
}

func (u *mockUploader) UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error) {
	u.mu.Lock()
	u.calls++
	block := u.blockChan
	if err, ok := u.failForGUIDs[matchGUID]; ok {
		u.mu.Unlock()
		return nil, err
	}
	isDup := u.duplicates[matchGUID]
	u.mu.Unlock()

	if block != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-block:
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	u.mu.Lock()
	u.uploaded[matchGUID] = filePath
	u.mu.Unlock()

	return &UploadResult{
		ID:          fmt.Sprintf("bc-%s", matchGUID),
		Location:    fmt.Sprintf("https://ballchasing.com/replay/bc-%s", matchGUID),
		IsDuplicate: isDup,
	}, nil
}

// --- Unit Tests ---

func TestSyncer_ConstructorValidation(t *testing.T) {
	store := newMockStore()
	provider := newMockHistoryProvider()
	downloader := newMockDownloader()
	uploader := newMockUploader()

	if _, err := New(nil, provider, downloader, uploader); !errors.Is(err, ErrNilStore) {
		t.Fatalf("expected ErrNilStore, got %v", err)
	}
	if _, err := New(store, nil, downloader, uploader); !errors.Is(err, ErrNilProvider) {
		t.Fatalf("expected ErrNilProvider, got %v", err)
	}
	if _, err := New(store, provider, nil, uploader); !errors.Is(err, ErrNilDownloader) {
		t.Fatalf("expected ErrNilDownloader, got %v", err)
	}
	if _, err := New(store, provider, downloader, nil); !errors.Is(err, ErrNilUploader) {
		t.Fatalf("expected ErrNilUploader, got %v", err)
	}

	s, err := New(store, provider, downloader, uploader,
		WithReplayDir("/tmp/replays"),
		WithDryRun(true),
		WithKeepLocalFiles(false),
	)
	if err != nil {
		t.Fatalf("unexpected New error: %v", err)
	}
	if s.replayDir != "/tmp/replays" || !s.dryRun || s.keepLocalFiles {
		t.Fatalf("options not applied correctly: %+v", s)
	}
}

func TestSyncer_HappyPath_FullPipeline(t *testing.T) {
	store := newMockStore()
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m1", ReplayURL: "https://cdn.example.com/m1.replay", MapName: "DFHStadium"},
		DiscoveredMatch{MatchGUID: "m2", ReplayURL: "https://cdn.example.com/m2.replay", MapName: "Mannfield"},
	)
	downloader := newMockDownloader()
	uploader := newMockUploader()

	s, err := New(store, provider, downloader, uploader, WithReplayDir("/tmp/test-replays"))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	stats, err := s.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	if stats.DiscoveredCount != 2 || stats.DownloadedCount != 2 || stats.UploadedCount != 2 || stats.FailedCount != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if stats.Discovered != 2 || stats.Downloaded != 2 || stats.Uploaded != 2 {
		t.Fatalf("alias stats mismatch: %+v", stats)
	}

	rec1, _ := store.GetMatch(context.Background(), "m1")
	if rec1.DownloadStatus != storage.DownloadDownloaded || rec1.UploadStatus != storage.UploadUploaded {
		t.Fatalf("m1 status incorrect: %+v", rec1)
	}
}

func TestSyncer_MultiCycle_Progression(t *testing.T) {
	store := newMockStore()
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m1", ReplayURL: "https://cdn.example.com/m1.replay"},
	)
	downloader := newMockDownloader()
	uploader := newMockUploader()

	s, _ := New(store, provider, downloader, uploader)

	// Cycle 1
	stats1, err := s.RunCycle(context.Background())
	if err != nil || stats1.DownloadedCount != 1 || stats1.UploadedCount != 1 {
		t.Fatalf("cycle 1 failed: %v, stats: %+v", err, stats1)
	}

	// Cycle 2: add m2, keep m1
	provider.SetMatches([]DiscoveredMatch{
		{MatchGUID: "m1", ReplayURL: "https://cdn.example.com/m1.replay"},
		{MatchGUID: "m2", ReplayURL: "https://cdn.example.com/m2.replay"},
	})

	stats2, err := s.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("cycle 2 failed: %v", err)
	}

	if stats2.DiscoveredCount != 2 {
		t.Fatalf("expected 2 discovered in cycle 2, got %d", stats2.DiscoveredCount)
	}
	if stats2.DownloadedCount != 1 || stats2.UploadedCount != 1 {
		t.Fatalf("only new match m2 should be processed in cycle 2: %+v", stats2)
	}
}

func TestSyncer_DelayedReplayURL_TwoCycles(t *testing.T) {
	store := newMockStore()
	// Cycle 1: ReplayURL is empty (delayed CDN generation)
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m-delayed", ReplayURL: ""},
	)
	downloader := newMockDownloader()
	uploader := newMockUploader()

	s, _ := New(store, provider, downloader, uploader)

	stats1, err := s.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("cycle 1 failed: %v", err)
	}
	if stats1.SkippedCount != 1 || stats1.DownloadedCount != 0 {
		t.Fatalf("expected 1 skipped, 0 downloaded in cycle 1: %+v", stats1)
	}

	rec, _ := store.GetMatch(context.Background(), "m-delayed")
	if rec.DownloadStatus != storage.DownloadSkipped {
		t.Fatalf("expected SKIPPED status, got %s", rec.DownloadStatus)
	}

	// Cycle 2: CDN populates ReplayURL
	provider.SetMatches([]DiscoveredMatch{
		{MatchGUID: "m-delayed", ReplayURL: "https://cdn.example.com/m-delayed.replay"},
	})

	stats2, err := s.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("cycle 2 failed: %v", err)
	}
	if stats2.DownloadedCount != 1 || stats2.UploadedCount != 1 {
		t.Fatalf("delayed match should be downloaded and uploaded in cycle 2: %+v", stats2)
	}
}

func TestSyncer_DuplicateHandling_HTTP409(t *testing.T) {
	store := newMockStore()
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m-dup", ReplayURL: "https://cdn.example.com/m-dup.replay"},
	)
	downloader := newMockDownloader()
	uploader := newMockUploader()
	uploader.duplicates["m-dup"] = true

	s, _ := New(store, provider, downloader, uploader)

	stats, err := s.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}
	if stats.DuplicateCount != 1 || stats.UploadedCount != 0 || stats.FailedCount != 0 {
		t.Fatalf("expected 1 duplicate, 0 uploaded, 0 failed: %+v", stats)
	}

	rec, _ := store.GetMatch(context.Background(), "m-dup")
	if rec.UploadStatus != storage.UploadDuplicate {
		t.Fatalf("expected DUPLICATE upload status, got %s", rec.UploadStatus)
	}
}

func TestSyncer_DryRun_NoNetworkOrDBMutations(t *testing.T) {
	store := newMockStore()
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m-dry", ReplayURL: "https://cdn.example.com/m-dry.replay"},
	)
	downloader := newMockDownloader()
	uploader := newMockUploader()

	s, _ := New(store, provider, downloader, uploader, WithDryRun(true))

	stats, err := s.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("dry run failed: %v", err)
	}

	if stats.DiscoveredCount != 1 {
		t.Fatalf("expected 1 discovered in dry run, got %d", stats.DiscoveredCount)
	}
	if stats.DownloadedCount != 0 || stats.UploadedCount != 0 {
		t.Fatalf("dry run must not download or upload: %+v", stats)
	}
	if downloader.calls != 0 || uploader.calls != 0 {
		t.Fatalf("downloader or uploader called in dry run: dl=%d, up=%d", downloader.calls, uploader.calls)
	}

	// Verify store was not mutated
	_, err = store.GetMatch(context.Background(), "m-dry")
	if !errors.Is(err, storage.ErrMatchNotFound) {
		t.Fatalf("dry run must not insert match into store, got %v", err)
	}
}

func TestSyncer_CrashRecovery_ResetsInFlight(t *testing.T) {
	store := newMockStore()
	// Pre-seed orphaned in-flight states from simulated crash
	store.matches["m-crash-dl"] = &storage.MatchRecord{
		MatchGUID:      "m-crash-dl",
		ReplayURL:      "https://cdn.example.com/crash-dl.replay",
		DownloadStatus: storage.DownloadDownloading,
		UploadStatus:   storage.UploadPending,
	}
	store.matches["m-crash-up"] = &storage.MatchRecord{
		MatchGUID:      "m-crash-up",
		ReplayURL:      "https://cdn.example.com/crash-up.replay",
		DownloadStatus: storage.DownloadDownloaded,
		LocalFilePath:  "/tmp/crash-up.replay",
		UploadStatus:   storage.UploadUploading,
	}

	provider := newMockHistoryProvider() // no new discovered matches
	downloader := newMockDownloader()
	uploader := newMockUploader()

	s, _ := New(store, provider, downloader, uploader)

	stats, err := s.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	if store.recoverCalls != 1 {
		t.Fatalf("expected RecoverInFlight to be called once, got %d", store.recoverCalls)
	}
	if stats.DownloadedCount != 1 || stats.UploadedCount != 2 {
		t.Fatalf("expected 1 download and 2 uploads after crash recovery: %+v", stats)
	}
}

func TestSyncer_ContextCancellation_DuringDownload(t *testing.T) {
	store := newMockStore()
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m-cancel", ReplayURL: "https://cdn.example.com/cancel.replay"},
	)
	downloader := newMockDownloader()
	downloader.blockChan = make(chan struct{})
	uploader := newMockUploader()

	s, _ := New(store, provider, downloader, uploader)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := s.RunCycle(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestSyncer_ContextCancellation_DuringUpload(t *testing.T) {
	store := newMockStore()
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m-cancel-up", ReplayURL: "https://cdn.example.com/cancel-up.replay"},
	)
	downloader := newMockDownloader()
	uploader := newMockUploader()
	uploader.blockChan = make(chan struct{})

	s, _ := New(store, provider, downloader, uploader)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := s.RunCycle(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestSyncer_PartialDownloadFailure_ContinuesToNext(t *testing.T) {
	store := newMockStore()
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m1", ReplayURL: "https://cdn.example.com/m1.replay"},
		DiscoveredMatch{MatchGUID: "m2", ReplayURL: "https://cdn.example.com/m2.replay"},
		DiscoveredMatch{MatchGUID: "m3", ReplayURL: "https://cdn.example.com/m3.replay"},
	)
	downloader := newMockDownloader()
	downloader.failForGUIDs["m2"] = errors.New("CDN 404 Not Found")
	uploader := newMockUploader()

	s, _ := New(store, provider, downloader, uploader)

	stats, err := s.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	if stats.DownloadedCount != 2 || stats.FailedCount != 1 || stats.UploadedCount != 2 {
		t.Fatalf("unexpected stats on partial failure: %+v", stats)
	}

	rec2, _ := store.GetMatch(context.Background(), "m2")
	if rec2.DownloadStatus != storage.DownloadFailed {
		t.Fatalf("m2 should be marked FAILED, got %s", rec2.DownloadStatus)
	}
}

func TestSyncer_PartialUploadFailure_ContinuesToNext(t *testing.T) {
	store := newMockStore()
	provider := newMockHistoryProvider(
		DiscoveredMatch{MatchGUID: "m1", ReplayURL: "https://cdn.example.com/m1.replay"},
		DiscoveredMatch{MatchGUID: "m2", ReplayURL: "https://cdn.example.com/m2.replay"},
	)
	downloader := newMockDownloader()
	uploader := newMockUploader()
	uploader.failForGUIDs["m1"] = errors.New("Ballchasing 500 Internal Server Error")

	s, _ := New(store, provider, downloader, uploader)

	stats, err := s.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("RunCycle failed: %v", err)
	}

	if stats.DownloadedCount != 2 || stats.UploadedCount != 1 || stats.FailedCount != 1 {
		t.Fatalf("unexpected stats on partial upload failure: %+v", stats)
	}

	rec1, _ := store.GetMatch(context.Background(), "m1")
	if rec1.UploadStatus != storage.UploadFailed {
		t.Fatalf("m1 should be marked FAILED, got %s", rec1.UploadStatus)
	}
	rec2, _ := store.GetMatch(context.Background(), "m2")
	if rec2.UploadStatus != storage.UploadUploaded {
		t.Fatalf("m2 should be marked UPLOADED, got %s", rec2.UploadStatus)
	}
}

func TestSyncer_ProviderError_AbortsCycle(t *testing.T) {
	store := newMockStore()
	provider := newMockHistoryProvider()
	provider.err = errors.New("psynet rpc connection failed")
	downloader := newMockDownloader()
	uploader := newMockUploader()

	s, _ := New(store, provider, downloader, uploader)

	_, err := s.RunCycle(context.Background())
	if err == nil || !strings.Contains(err.Error(), "polling matches failed") {
		t.Fatalf("expected polling error, got: %v", err)
	}
}
```

---

## 5. Verification Method

Once implemented by the worker:

1. **Unit Test Execution**:
   ```powershell
   & "C:\Users\strms\AppData\Local\go\go\bin\go.exe" test -v -count=1 ./internal/syncer/...
   ```
   *Expected*: PASS for all 12 test scenarios (100% pass rate).

2. **Code Hygiene & Static Analysis**:
   ```powershell
   & "C:\Users\strms\AppData\Local\go\go\bin\go.exe" vet ./internal/syncer/...
   ```
   *Expected*: Clean with no warnings.

3. **Full Repository Regression**:
   ```powershell
   & "C:\Users\strms\AppData\Local\go\go\bin\go.exe" test ./...
   ```
   *Expected*: All existing packages (`internal/auth`, `internal/ballchasing`, `internal/config`, `internal/psynet`, `internal/storage`, `internal/testutil`, `test/e2e`) continue passing without regressions.

4. **Invalidation Conditions**:
   - Any failure to reset in-flight records on restart.
   - Any network download/upload initiated during dry-run.
   - Any regression from `SKIPPED` to failure when a match replay URL is delayed.
   - Any duplicate upload HTTP 409 being reported as a failure or triggering retry thrashing.
