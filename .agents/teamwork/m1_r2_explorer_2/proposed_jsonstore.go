package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// jsonStatePayload defines the serialized on-disk format for the structured JSON state store.
type jsonStatePayload struct {
	Version   int                     `json:"version"`
	UpdatedAt time.Time               `json:"updated_at"`
	Matches   map[string]*MatchRecord `json:"matches"`
	Auth      map[string]*AuthRecord  `json:"auth"`
}

// JSONStore provides an in-memory StateStore with atomic disk persistence via temporary files
// and atomic os.Rename. It is fully thread-safe via sync.RWMutex.
type JSONStore struct {
	mu       sync.RWMutex
	filePath string
	matches  map[string]*MatchRecord
	auth     map[string]*AuthRecord
	closed   bool
}

// Compile-time check ensuring JSONStore implements StateStore.
var _ StateStore = (*JSONStore)(nil)

// NewJSONStore creates or opens a JSONStore at the given file path.
// If the file does not exist, an empty store is created along with any missing parent directories.
// Any existing state is loaded and unmarshaled into memory on initialization.
func NewJSONStore(filePath string) (*JSONStore, error) {
	if filePath == "" {
		return nil, errors.New("filePath cannot be empty")
	}

	dir := filepath.Dir(filePath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory for json store %s: %w", dir, err)
		}
	}

	store := &JSONStore{
		filePath: filePath,
		matches:  make(map[string]*MatchRecord),
		auth:     make(map[string]*AuthRecord),
	}

	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		// Initialize empty file atomically
		if err := store.saveLocked(); err != nil {
			return nil, fmt.Errorf("failed to initialize empty json store %s: %w", filePath, err)
		}
		return store, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to stat json store %s: %w", filePath, err)
	}

	if info.Size() == 0 {
		// Empty file, persist initial structure
		if err := store.saveLocked(); err != nil {
			return nil, fmt.Errorf("failed to write initial structure to %s: %w", filePath, err)
		}
		return store, nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read json store %s: %w", filePath, err)
	}

	if len(strings.TrimSpace(string(data))) == 0 {
		if err := store.saveLocked(); err != nil {
			return nil, fmt.Errorf("failed to write initial structure to %s: %w", filePath, err)
		}
		return store, nil
	}

	var payload jsonStatePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal json store %s: %w", filePath, err)
	}

	if payload.Matches != nil {
		store.matches = payload.Matches
	}
	if payload.Auth != nil {
		store.auth = payload.Auth
	}

	// Clean up any stale temp files from prior abnormal crashes
	cleanupStaleTempFiles(dir)

	return store, nil
}

// saveLocked persists the in-memory state to disk atomically.
// Must be called with s.mu held for writing.
func (s *JSONStore) saveLocked() error {
	dir := filepath.Dir(s.filePath)
	if dir == "" {
		dir = "."
	}

	payload := jsonStatePayload{
		Version:   1,
		UpdatedAt: time.Now().UTC(),
		Matches:   s.matches,
		Auth:      s.auth,
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal json state: %w", err)
	}

	// Write to temporary file in the SAME directory to guarantee same filesystem volume
	tmpFile, err := os.CreateTemp(dir, ".rl-sync-state-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Ensure temp file is closed and cleaned up in case of error
	success := false
	defer func() {
		if !success {
			_ = tmpFile.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("failed to write data to temp file %s: %w", tmpPath, err)
	}

	// Fsync to ensure bytes are flushed to physical disk buffers
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("failed to fsync temp file %s: %w", tmpPath, err)
	}

	// Must close file before renaming on Windows!
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file %s: %w", tmpPath, err)
	}

	// Atomic replace via os.Rename with retry for Windows transient file locks
	if err := atomicRename(tmpPath, s.filePath); err != nil {
		return fmt.Errorf("failed to atomic rename %s to %s: %w", tmpPath, s.filePath, err)
	}

	success = true
	return nil
}

// atomicRename attempts atomic os.Rename with retries to withstand transient Windows locks
// (e.g. antivirus scanner or indexer momentarily holding open handles).
func atomicRename(oldPath, newPath string) error {
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		err = os.Rename(oldPath, newPath)
		if err == nil {
			return nil
		}
		time.Sleep(time.Duration((attempt+1)*5) * time.Millisecond)
	}
	return err
}

// cleanupStaleTempFiles scans the directory and removes orphaned .rl-sync-state-*.tmp files.
func cleanupStaleTempFiles(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), ".rl-sync-state-") && strings.HasSuffix(entry.Name(), ".tmp") {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}

// cloneMatchRecord creates a deep copy of MatchRecord to prevent data races.
func cloneMatchRecord(src *MatchRecord) *MatchRecord {
	if src == nil {
		return nil
	}
	dst := *src
	if src.DownloadedAt != nil {
		t := *src.DownloadedAt
		dst.DownloadedAt = &t
	}
	if src.UploadedAt != nil {
		t := *src.UploadedAt
		dst.UploadedAt = &t
	}
	return &dst
}

// GetMatch retrieves a single match record by its GUID.
// Returns ErrMatchNotFound if the record does not exist.
func (s *JSONStore) GetMatch(ctx context.Context, matchGUID string) (*MatchRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if matchGUID == "" {
		return nil, ErrInvalidGUID
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	rec, ok := s.matches[matchGUID]
	if !ok {
		return nil, ErrMatchNotFound
	}

	return cloneMatchRecord(rec), nil
}

// ListPendingDownloads returns all matches ready for replay downloading
// (download_status = PENDING and non-empty replay_url), ordered by timestamp ASC.
func (s *JSONStore) ListPendingDownloads(ctx context.Context) ([]*MatchRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	var results []*MatchRecord
	for _, m := range s.matches {
		if m.DownloadStatus == DownloadPending && m.ReplayURL != "" {
			results = append(results, cloneMatchRecord(m))
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].RecordStartTimestamp != results[j].RecordStartTimestamp {
			return results[i].RecordStartTimestamp < results[j].RecordStartTimestamp
		}
		return results[i].MatchGUID < results[j].MatchGUID
	})

	return results, nil
}

// ListPendingUploads returns all matches ready for upload
// (download_status = DOWNLOADED and upload_status = PENDING and non-empty local path), ordered by timestamp ASC.
func (s *JSONStore) ListPendingUploads(ctx context.Context) ([]*MatchRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	var results []*MatchRecord
	for _, m := range s.matches {
		if m.DownloadStatus == DownloadDownloaded && m.UploadStatus == UploadPending && m.LocalFilePath != "" {
			results = append(results, cloneMatchRecord(m))
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].RecordStartTimestamp != results[j].RecordStartTimestamp {
			return results[i].RecordStartTimestamp < results[j].RecordStartTimestamp
		}
		return results[i].MatchGUID < results[j].MatchGUID
	})

	return results, nil
}

// UpsertDiscoveredMatches inserts newly discovered matches without clobbering
// existing download/upload statuses or metadata.
func (s *JSONStore) UpsertDiscoveredMatches(ctx context.Context, matches []*MatchRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(matches) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	if s.closed {
		return ErrStoreClosed
	}

	now := time.Now().UTC()
	modified := false

	for _, m := range matches {
		if m == nil || m.MatchGUID == "" {
			continue
		}

		existing, exists := s.matches[m.MatchGUID]
		if exists {
			// Idempotency guarantee: preserve existing progress and statuses
			if existing.ReplayURL == "" && m.ReplayURL != "" {
				existing.ReplayURL = m.ReplayURL
				if existing.DownloadStatus == DownloadSkipped {
					existing.DownloadStatus = DownloadPending
				}
				existing.UpdatedAt = now
				modified = true
			}
			continue
		}

		// New match insertion
		rec := cloneMatchRecord(m)
		if rec.DownloadStatus == "" {
			if rec.ReplayURL == "" {
				rec.DownloadStatus = DownloadSkipped
			} else {
				rec.DownloadStatus = DownloadPending
			}
		}
		if rec.UploadStatus == "" {
			rec.UploadStatus = UploadPending
		}
		if rec.CreatedAt.IsZero() {
			rec.CreatedAt = now
		}
		if rec.UpdatedAt.IsZero() {
			rec.UpdatedAt = now
		}

		s.matches[m.MatchGUID] = rec
		modified = true
	}

	if modified {
		if err := s.saveLocked(); err != nil {
			return fmt.Errorf("failed to persist upserted matches: %w", err)
		}
	}

	return nil
}

// MarkDownloading transitions download_status to DOWNLOADING.
func (s *JSONStore) MarkDownloading(ctx context.Context, matchGUID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if matchGUID == "" {
		return ErrInvalidGUID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	if s.closed {
		return ErrStoreClosed
	}

	rec, ok := s.matches[matchGUID]
	if !ok {
		return ErrMatchNotFound
	}

	rec.DownloadStatus = DownloadDownloading
	rec.UpdatedAt = time.Now().UTC()

	return s.saveLocked()
}

// MarkDownloaded transitions download_status to DOWNLOADED, setting local_file_path and downloaded_at.
func (s *JSONStore) MarkDownloaded(ctx context.Context, matchGUID, localPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if matchGUID == "" {
		return ErrInvalidGUID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	if s.closed {
		return ErrStoreClosed
	}

	rec, ok := s.matches[matchGUID]
	if !ok {
		return ErrMatchNotFound
	}

	now := time.Now().UTC()
	rec.DownloadStatus = DownloadDownloaded
	rec.LocalFilePath = localPath
	rec.DownloadedAt = &now
	rec.LastError = ""
	rec.UpdatedAt = now

	return s.saveLocked()
}

// MarkDownloadFailed transitions download_status to FAILED, increments retry_count, and records last_error.
func (s *JSONStore) MarkDownloadFailed(ctx context.Context, matchGUID, errMsg string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if matchGUID == "" {
		return ErrInvalidGUID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	if s.closed {
		return ErrStoreClosed
	}

	rec, ok := s.matches[matchGUID]
	if !ok {
		return ErrMatchNotFound
	}

	now := time.Now().UTC()
	rec.DownloadStatus = DownloadFailed
	rec.LastError = errMsg
	rec.RetryCount++
	rec.UpdatedAt = now

	return s.saveLocked()
}

// MarkUploading transitions upload_status to UPLOADING.
func (s *JSONStore) MarkUploading(ctx context.Context, matchGUID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if matchGUID == "" {
		return ErrInvalidGUID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	if s.closed {
		return ErrStoreClosed
	}

	rec, ok := s.matches[matchGUID]
	if !ok {
		return ErrMatchNotFound
	}

	rec.UploadStatus = UploadUploading
	rec.UpdatedAt = time.Now().UTC()

	return s.saveLocked()
}

// MarkUploaded transitions upload_status to UPLOADED, recording Ballchasing ID and URL.
func (s *JSONStore) MarkUploaded(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if matchGUID == "" {
		return ErrInvalidGUID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	if s.closed {
		return ErrStoreClosed
	}

	rec, ok := s.matches[matchGUID]
	if !ok {
		return ErrMatchNotFound
	}

	now := time.Now().UTC()
	rec.UploadStatus = UploadUploaded
	rec.BallchasingID = ballchasingID
	rec.BallchasingURL = ballchasingURL
	rec.UploadedAt = &now
	rec.LastError = ""
	rec.UpdatedAt = now

	return s.saveLocked()
}

// MarkDuplicate transitions upload_status to DUPLICATE, recording Ballchasing ID and URL.
func (s *JSONStore) MarkDuplicate(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if matchGUID == "" {
		return ErrInvalidGUID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	if s.closed {
		return ErrStoreClosed
	}

	rec, ok := s.matches[matchGUID]
	if !ok {
		return ErrMatchNotFound
	}

	now := time.Now().UTC()
	rec.UploadStatus = UploadDuplicate
	rec.BallchasingID = ballchasingID
	rec.BallchasingURL = ballchasingURL
	rec.UploadedAt = &now
	rec.LastError = ""
	rec.UpdatedAt = now

	return s.saveLocked()
}

// MarkUploadFailed transitions upload_status to FAILED, increments retry_count, and records last_error.
func (s *JSONStore) MarkUploadFailed(ctx context.Context, matchGUID, errMsg string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if matchGUID == "" {
		return ErrInvalidGUID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	if s.closed {
		return ErrStoreClosed
	}

	rec, ok := s.matches[matchGUID]
	if !ok {
		return ErrMatchNotFound
	}

	now := time.Now().UTC()
	rec.UploadStatus = UploadFailed
	rec.LastError = errMsg
	rec.RetryCount++
	rec.UpdatedAt = now

	return s.saveLocked()
}

// RecoverInFlight resets DOWNLOADING -> PENDING and UPLOADING -> PENDING after an abnormal termination.
func (s *JSONStore) RecoverInFlight(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	if s.closed {
		return ErrStoreClosed
	}

	now := time.Now().UTC()
	modified := false

	for _, m := range s.matches {
		if m.DownloadStatus == DownloadDownloading {
			m.DownloadStatus = DownloadPending
			m.UpdatedAt = now
			modified = true
		}
		if m.UploadStatus == UploadUploading {
			m.UploadStatus = UploadPending
			m.UpdatedAt = now
			modified = true
		}
	}

	if modified {
		if err := s.saveLocked(); err != nil {
			return fmt.Errorf("failed to persist recovered in-flight states: %w", err)
		}
	}

	return nil
}

// SaveAuthState upserts authentication tokens and metadata for a provider.
func (s *JSONStore) SaveAuthState(ctx context.Context, provider, refreshToken, accountID, displayName string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if provider == "" {
		return errors.New("provider cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	if s.closed {
		return ErrStoreClosed
	}

	s.auth[provider] = &AuthRecord{
		Provider:     provider,
		RefreshToken: refreshToken,
		AccountID:    accountID,
		DisplayName:  displayName,
		UpdatedAt:    time.Now().UTC(),
	}

	return s.saveLocked()
}

// GetAuthState retrieves persisted authentication metadata for a given provider.
func (s *JSONStore) GetAuthState(ctx context.Context, provider string) (refreshToken, accountID, displayName string, err error) {
	if err := ctx.Err(); err != nil {
		return "", "", "", err
	}
	if provider == "" {
		return "", "", "", errors.New("provider cannot be empty")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return "", "", "", ErrStoreClosed
	}

	rec, ok := s.auth[provider]
	if !ok {
		return "", "", "", ErrAuthStateNotFound
	}

	return rec.RefreshToken, rec.AccountID, rec.DisplayName, nil
}

// Close closes the JSONStore. Subsequent calls to Store methods will return ErrStoreClosed.
func (s *JSONStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true
	return nil
}
