package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dank/rl-api-utils/internal/ballchasing"
)

// jsonStatePayload defines the serialized on-disk format for the structured JSON state store.
type jsonStatePayload struct {
	Version          int                               `json:"version"`
	UpdatedAt        time.Time                         `json:"updated_at"`
	Matches          map[string]*MatchRecord           `json:"matches"`
	Auth             map[string]*AuthRecord            `json:"auth"`
	Players          map[string]*PlayerRecord          `json:"players,omitempty"`
	PlayerMatchups   map[string]map[int]*PlayerMatchup `json:"player_matchups,omitempty"`
	ProcessedMatches map[string]int64                  `json:"processed_matches,omitempty"`
	MatchPlayers     map[string][]*MatchPlayerRecord   `json:"match_players,omitempty"`
	MMRHistory       []*MMRHistoryRecord               `json:"mmr_history,omitempty"`
	Sessions         map[string]*SessionRecord         `json:"sessions,omitempty"`
}

// JSONStore provides an in-memory StateStore with atomic disk persistence via temporary files
// and atomic os.Rename. It is fully thread-safe via sync.RWMutex.
type JSONStore struct {
	mu               sync.RWMutex
	filePath         string
	matches          map[string]*MatchRecord
	auth             map[string]*AuthRecord
	players          map[string]*PlayerRecord
	playerMatchups   map[string]map[int]*PlayerMatchup
	processedMatches map[string]int64
	matchPlayers     map[string][]*MatchPlayerRecord
	mmrHistory       []*MMRHistoryRecord
	sessions         map[string]*SessionRecord
	closed           bool
	maxRetries       int
	baseBackoff      time.Duration
	maxBackoff       time.Duration
}

// Compile-time check ensuring JSONStore implements StateStore.
var _ StateStore = (*JSONStore)(nil)

// SetRetryPolicy configures the maximum retries and backoff limits for pending queries.
func (s *JSONStore) SetRetryPolicy(maxRetries int, baseBackoff, maxBackoff time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if maxRetries > 0 {
		s.maxRetries = maxRetries
	}
	s.baseBackoff = baseBackoff
	s.maxBackoff = maxBackoff
}

// CalculateBackoff computes exponential backoff: baseBackoff * 2^(attempt - 1), capped at maxBackoff.
// Returns 0 if attempt <= 0 or baseBackoff <= 0.
func CalculateBackoff(attempt int, baseBackoff, maxBackoff time.Duration) time.Duration {
	if attempt <= 0 || baseBackoff <= 0 {
		return 0
	}
	shift := attempt - 1
	if shift > 30 {
		shift = 30
	}
	multiplier := time.Duration(1 << shift)
	if maxBackoff > 0 && multiplier > maxBackoff/baseBackoff {
		return maxBackoff
	}
	delay := baseBackoff * multiplier
	if delay < 0 || (maxBackoff > 0 && delay > maxBackoff) {
		if maxBackoff > 0 {
			return maxBackoff
		}
		return time.Duration(1<<63 - 1)
	}
	return delay
}

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
		filePath:         filePath,
		matches:          make(map[string]*MatchRecord),
		auth:             make(map[string]*AuthRecord),
		players:          make(map[string]*PlayerRecord),
		playerMatchups:   make(map[string]map[int]*PlayerMatchup),
		processedMatches: make(map[string]int64),
		matchPlayers:     make(map[string][]*MatchPlayerRecord),
		mmrHistory:       make([]*MMRHistoryRecord, 0),
		sessions:         make(map[string]*SessionRecord),
		maxRetries:       DefaultMaxRetries,
		baseBackoff:      DefaultBaseBackoff,
		maxBackoff:       DefaultMaxBackoff,
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
		for _, m := range store.matches {
			if m.StatsSyncStatus == "" {
				m.StatsSyncStatus = StatsSyncPending
			}
		}
	}
	if payload.Auth != nil {
		store.auth = payload.Auth
	}
	if payload.Players != nil {
		store.players = payload.Players
		for _, p := range store.players {
			if p.Tags == nil {
				p.Tags = []string{}
			}
		}
	}
	if payload.PlayerMatchups != nil {
		store.playerMatchups = payload.PlayerMatchups
	}
	if payload.ProcessedMatches != nil {
		store.processedMatches = payload.ProcessedMatches
	}
	if payload.MatchPlayers != nil {
		store.matchPlayers = payload.MatchPlayers
	}
	if payload.MMRHistory != nil {
		store.mmrHistory = payload.MMRHistory
	}
	if payload.Sessions != nil {
		store.sessions = payload.Sessions
		for _, sess := range store.sessions {
			if sess.LocalPlayerIDs == nil {
				sess.LocalPlayerIDs = []string{}
			}
		}
	}

	// Startup backfill: populate matchPlayers from existing matches with replay_stats_json if missing
	store.backfillMatchPlayersLocked()

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
		Version:          1,
		UpdatedAt:        time.Now().UTC(),
		Matches:          s.matches,
		Auth:             s.auth,
		Players:          s.players,
		PlayerMatchups:   s.playerMatchups,
		ProcessedMatches: s.processedMatches,
		MatchPlayers:     s.matchPlayers,
		MMRHistory:       s.mmrHistory,
		Sessions:         s.sessions,
	}

	data, err := json.Marshal(payload)
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
	if src.WinnerTeam != nil {
		wt := *src.WinnerTeam
		dst.WinnerTeam = &wt
	}
	if src.SyncedAt != nil {
		t := *src.SyncedAt
		dst.SyncedAt = &t
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


// ListPendingDownloads returns all matches ready for download (PENDING, or FAILED with retry_count < maxRetries and backoff elapsed).
func (s *JSONStore) ListPendingDownloads(ctx context.Context) ([]*MatchRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	now := time.Now().UTC()
	maxRetries := s.maxRetries
	if maxRetries <= 0 {
		maxRetries = DefaultMaxRetries
	}

	var results []*MatchRecord
	for _, m := range s.matches {
		if m.ReplayURL == "" {
			continue
		}
		if m.DownloadStatus == DownloadPending {
			results = append(results, cloneMatchRecord(m))
		} else if m.DownloadStatus == DownloadFailed && m.RetryCount < maxRetries {
			delay := CalculateBackoff(m.RetryCount, s.baseBackoff, s.maxBackoff)
			if now.Sub(m.UpdatedAt) >= delay {
				results = append(results, cloneMatchRecord(m))
			}
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].RecordStartTimestamp != results[j].RecordStartTimestamp {
			return results[i].RecordStartTimestamp < results[j].RecordStartTimestamp
		}
		return results[i].MatchGUID < results[j].MatchGUID
	})

	if results == nil {
		results = make([]*MatchRecord, 0)
	}
	return results, nil
}

// ListPendingUploads returns all matches ready for upload
// (download_status = DOWNLOADED and upload_status = PENDING or FAILED with retry_count < maxRetries and backoff elapsed, and non-empty local path), ordered by timestamp ASC.
func (s *JSONStore) ListPendingUploads(ctx context.Context) ([]*MatchRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	now := time.Now().UTC()
	maxRetries := s.maxRetries
	if maxRetries <= 0 {
		maxRetries = DefaultMaxRetries
	}

	var results []*MatchRecord
	for _, m := range s.matches {
		if m.DownloadStatus != DownloadDownloaded || m.LocalFilePath == "" {
			continue
		}
		if m.UploadStatus == UploadPending {
			results = append(results, cloneMatchRecord(m))
		} else if m.UploadStatus == UploadFailed && m.RetryCount < maxRetries {
			delay := CalculateBackoff(m.RetryCount, s.baseBackoff, s.maxBackoff)
			if now.Sub(m.UpdatedAt) >= delay {
				results = append(results, cloneMatchRecord(m))
			}
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].RecordStartTimestamp != results[j].RecordStartTimestamp {
			return results[i].RecordStartTimestamp < results[j].RecordStartTimestamp
		}
		return results[i].MatchGUID < results[j].MatchGUID
	})

	if results == nil {
		results = make([]*MatchRecord, 0)
	}
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
	rec.RetryCount = 0
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
	rec.BallchasingURL = NormalizeBallchasingURL(ballchasingID, ballchasingURL)
	rec.UploadedAt = &now
	rec.LastError = ""
	if ballchasingID != "" && !strings.Contains(rec.ReplayStatsJSON, `"boost":`) {
		rec.StatsSyncStatus = StatsSyncPending
		rec.StatsRetryCount = 0
	}
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
	rec.BallchasingURL = NormalizeBallchasingURL(ballchasingID, ballchasingURL)
	rec.UploadedAt = &now
	rec.LastError = ""
	if ballchasingID != "" && !strings.Contains(rec.ReplayStatsJSON, `"boost":`) {
		rec.StatsSyncStatus = StatsSyncPending
		rec.StatsRetryCount = 0
	}
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
		if m.StatsSyncStatus == StatsSyncSyncing {
			m.StatsSyncStatus = StatsSyncPending
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

// clonePlayerRecord creates a deep copy of PlayerRecord to prevent data races.
func clonePlayerRecord(src *PlayerRecord) *PlayerRecord {
	if src == nil {
		return nil
	}
	dst := *src
	if src.Tags != nil {
		dst.Tags = make([]string, len(src.Tags))
		copy(dst.Tags, src.Tags)
	} else {
		dst.Tags = []string{}
	}
	return &dst
}

// clonePlayerMatchup creates a deep copy of PlayerMatchup to prevent data races.
func clonePlayerMatchup(src *PlayerMatchup) *PlayerMatchup {
	if src == nil {
		return nil
	}
	dst := *src
	return &dst
}

// clonePlayerSummary creates a deep copy of PlayerSummary to prevent data races.
func clonePlayerSummary(src *PlayerSummary) *PlayerSummary {
	if src == nil {
		return nil
	}
	dst := *src
	if src.Tags != nil {
		dst.Tags = make([]string, len(src.Tags))
		copy(dst.Tags, src.Tags)
	} else {
		dst.Tags = []string{}
	}
	return &dst
}

// UpsertPlayer inserts or updates a player record, updating player_name, platform, notes, tags, and last_seen_at.
// Preserves ranks_json and first_seen_at if existing. Monotonically updates timestamps.
func (s *JSONStore) UpsertPlayer(ctx context.Context, player *PlayerRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if player == nil || strings.TrimSpace(player.PlayerID) == "" {
		return errors.New("player or player_id cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return ErrStoreClosed
	}

	now := time.Now().UTC().Truncate(time.Second)
	existing, exists := s.players[player.PlayerID]
	if exists {
		if player.Platform != "" {
			existing.Platform = player.Platform
		}
		if player.PlayerName != "" {
			existing.PlayerName = player.PlayerName
		}
		if player.RanksJSON != "" && player.RanksJSON != "{}" {
			existing.RanksJSON = player.RanksJSON
		}
		if player.Notes != "" {
			existing.Notes = player.Notes
		}
		if len(player.Tags) > 0 {
			existing.Tags = make([]string, len(player.Tags))
			copy(existing.Tags, player.Tags)
		}
		// Monotonic timestamps: first_seen moves earlier, last_seen moves later
		if !player.FirstSeenAt.IsZero() {
			inFirst := player.FirstSeenAt.Truncate(time.Second)
			if existing.FirstSeenAt.IsZero() || inFirst.Before(existing.FirstSeenAt) {
				existing.FirstSeenAt = inFirst
			}
		}
		incomingLastSeen := player.LastSeenAt
		if incomingLastSeen.IsZero() {
			incomingLastSeen = now
		} else {
			incomingLastSeen = incomingLastSeen.Truncate(time.Second)
		}
		if existing.LastSeenAt.IsZero() || incomingLastSeen.After(existing.LastSeenAt) {
			existing.LastSeenAt = incomingLastSeen
		}
	} else {
		rec := clonePlayerRecord(player)
		if !rec.FirstSeenAt.IsZero() {
			rec.FirstSeenAt = rec.FirstSeenAt.Truncate(time.Second)
		}
		if !rec.LastSeenAt.IsZero() {
			rec.LastSeenAt = rec.LastSeenAt.Truncate(time.Second)
		}
		if rec.FirstSeenAt.IsZero() && rec.LastSeenAt.IsZero() {
			rec.FirstSeenAt = now
			rec.LastSeenAt = now
		} else if rec.FirstSeenAt.IsZero() {
			rec.FirstSeenAt = rec.LastSeenAt
		} else if rec.LastSeenAt.IsZero() {
			rec.LastSeenAt = rec.FirstSeenAt
		}
		if rec.FirstSeenAt.After(rec.LastSeenAt) {
			rec.FirstSeenAt = rec.LastSeenAt
		}
		if rec.RanksJSON == "" {
			rec.RanksJSON = "{}"
		}
		if rec.Tags == nil {
			rec.Tags = []string{}
		}
		s.players[player.PlayerID] = rec
	}

	return s.saveLocked()
}

// resolveCanonicalPlayerIDLocked resolves a raw online_id or case-variant player ID to the stored canonical key.
// Must be called with s.mu held (RLock or Lock).
func (s *JSONStore) resolveCanonicalPlayerIDLocked(playerID string) string {
	playerID = strings.TrimSpace(playerID)
	if playerID == "" {
		return ""
	}
	if _, ok := s.players[playerID]; ok {
		return playerID
	}
	if _, ok := s.playerMatchups[playerID]; ok {
		return playerID
	}
	lowerTarget := strings.ToLower(playerID)
	lookupSeg := lowerTarget
	if strings.Contains(lowerTarget, "|") {
		parts := strings.Split(lowerTarget, "|")
		if len(parts) >= 2 && strings.TrimSpace(parts[1]) != "" {
			lookupSeg = strings.TrimSpace(parts[1])
		}
	}
	suffixPipe0 := "|" + lookupSeg + "|0"
	suffixPipe := "|" + lookupSeg
	var bestID string
	var bestSeen time.Time
	for id, p := range s.players {
		lowerID := strings.ToLower(id)
		if lowerID == lowerTarget || (lookupSeg != "0" && (strings.HasSuffix(lowerID, suffixPipe0) || strings.HasSuffix(lowerID, suffixPipe))) {
			if bestID == "" || p.LastSeenAt.After(bestSeen) {
				bestID = id
				bestSeen = p.LastSeenAt
			}
		}
	}
	if bestID != "" {
		return bestID
	}
	if !strings.Contains(playerID, "|") {
		for id, p := range s.players {
			if strings.EqualFold(strings.TrimSpace(p.PlayerName), playerID) {
				if bestID == "" || p.LastSeenAt.After(bestSeen) {
					bestID = id
					bestSeen = p.LastSeenAt
				}
			}
		}
		if bestID != "" {
			return bestID
		}
	}
	for _, mps := range s.matchPlayers {
		for _, mp := range mps {
			if mp == nil {
				continue
			}
			lowerID := strings.ToLower(mp.PlayerID)
			if lowerID == lowerTarget || (lookupSeg != "0" && (strings.HasSuffix(lowerID, suffixPipe0) || strings.HasSuffix(lowerID, suffixPipe))) {
				return mp.PlayerID
			}
		}
	}
	return playerID
}

// GetPlayer retrieves a single player profile by PlayerID. Returns ErrPlayerNotFound if missing.
func (s *JSONStore) GetPlayer(ctx context.Context, playerID string) (*PlayerRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(playerID) == "" {
		return nil, ErrPlayerNotFound
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	resolvedID := s.resolveCanonicalPlayerIDLocked(playerID)
	rec, ok := s.players[resolvedID]
	if !ok {
		return nil, ErrPlayerNotFound
	}

	return clonePlayerRecord(rec), nil
}

// ListPlayers returns a paginated slice of players ordered by last_seen_at DESC, player_id ASC.
func (s *JSONStore) ListPlayers(ctx context.Context, limit, offset int) ([]*PlayerRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	all := make([]*PlayerRecord, 0, len(s.players))
	for _, p := range s.players {
		all = append(all, p)
	}

	sort.Slice(all, func(i, j int) bool {
		if !all[i].LastSeenAt.Equal(all[j].LastSeenAt) {
			return all[i].LastSeenAt.After(all[j].LastSeenAt)
		}
		return all[i].PlayerID < all[j].PlayerID
	})

	total := len(all)
	if offset < 0 {
		offset = 0
	}
	if offset >= total || limit == 0 {
		return []*PlayerRecord{}, nil
	}

	end := total
	if limit > 0 {
		end = offset + limit
		if end > total {
			end = total
		}
	}

	results := make([]*PlayerRecord, 0, end-offset)
	for _, p := range all[offset:end] {
		results = append(results, clonePlayerRecord(p))
	}

	return results, nil
}

// ListPlayerSummaries returns paginated players along with aggregate win/loss statistics.
func (s *JSONStore) ListPlayerSummaries(ctx context.Context, limit, offset int) ([]*PlayerSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	all := make([]*PlayerRecord, 0, len(s.players))
	for _, p := range s.players {
		all = append(all, p)
	}

	sort.Slice(all, func(i, j int) bool {
		if !all[i].LastSeenAt.Equal(all[j].LastSeenAt) {
			return all[i].LastSeenAt.After(all[j].LastSeenAt)
		}
		return all[i].PlayerID < all[j].PlayerID
	})

	total := len(all)
	if offset < 0 {
		offset = 0
	}
	if offset >= total || limit == 0 {
		return []*PlayerSummary{}, nil
	}

	end := total
	if limit > 0 {
		end = offset + limit
		if end > total {
			end = total
		}
	}

	results := make([]*PlayerSummary, 0, end-offset)
	for _, p := range all[offset:end] {
		sum := &PlayerSummary{
			PlayerRecord: *clonePlayerRecord(p),
		}
		if matchups, ok := s.playerMatchups[p.PlayerID]; ok {
			for _, m := range matchups {
				sum.TotalWinsAsTeammate += m.WinsAsTeammate
				sum.TotalLossesAsTeammate += m.LossesAsTeammate
				sum.TotalWinsAsOpponent += m.WinsAsOpponent
				sum.TotalLossesAsOpponent += m.LossesAsOpponent
				sum.TotalMatches += m.TotalMatches
			}
		}
		results = append(results, sum)
	}

	return results, nil
}

// FilterPlayerSummaries searches, filters, and sorts players across the entire dataset.
func (s *JSONStore) FilterPlayerSummaries(ctx context.Context, filter PlayerDirectoryFilter) ([]*PlayerSummary, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if s.closed {
		return nil, 0, ErrStoreClosed
	}

	cleanQuery := strings.TrimSpace(strings.ToLower(filter.Query))
	cleanPlatform := strings.TrimSpace(strings.ToLower(filter.Platform))
	cleanRole := strings.TrimSpace(strings.ToLower(filter.Role))
	cleanSort := strings.TrimSpace(strings.ToLower(filter.Sort))
	isAsc := strings.TrimSpace(strings.ToLower(filter.Order)) == "asc"

	hasQuery := cleanQuery != ""
	hasPlatform := cleanPlatform != "" && cleanPlatform != "all"

	// 1. In-memory filter with full matchup aggregations
	matching := make([]*PlayerSummary, 0)
	for _, p := range s.players {
		if hasPlatform && !MatchesPlatformFilter(p.Platform, cleanPlatform) {
			continue
		}
		if hasQuery {
			nameMatch := strings.Contains(strings.ToLower(p.PlayerName), cleanQuery)
			idMatch := strings.Contains(strings.ToLower(p.PlayerID), cleanQuery)
			if !nameMatch && !idMatch {
				continue
			}
		}

		sum := &PlayerSummary{
			PlayerRecord: *clonePlayerRecord(p),
		}
		if matchups, ok := s.playerMatchups[p.PlayerID]; ok {
			for _, m := range matchups {
				if m == nil {
					continue
				}
				sum.TotalWinsAsTeammate += m.WinsAsTeammate
				sum.TotalLossesAsTeammate += m.LossesAsTeammate
				sum.TotalWinsAsOpponent += m.WinsAsOpponent
				sum.TotalLossesAsOpponent += m.LossesAsOpponent
				sum.TotalMatches += m.TotalMatches
			}
		}

		// Role filter
		switch cleanRole {
		case "frequent":
			if sum.TotalMatches < 3 {
				continue
			}
		case "teammate":
			if (sum.TotalWinsAsTeammate + sum.TotalLossesAsTeammate) <= 0 {
				continue
			}
		case "opponent":
			if (sum.TotalWinsAsOpponent + sum.TotalLossesAsOpponent) <= 0 {
				continue
			}
		case "synergy":
			tmGames := sum.TotalWinsAsTeammate + sum.TotalLossesAsTeammate
			if tmGames < 2 || (float64(sum.TotalWinsAsTeammate)/float64(tmGames)) < 0.60 {
				continue
			}
		}

		matching = append(matching, sum)
	}

	total := len(matching)

	if total == 0 || filter.Limit == 0 {
		return make([]*PlayerSummary, 0), total, nil
	}

	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return make([]*PlayerSummary, 0), total, nil
	}

	// 2. Sorting
	sort.Slice(matching, func(i, j int) bool {
		switch cleanSort {
		case "match_count", "matches":
			if matching[i].TotalMatches != matching[j].TotalMatches {
				if isAsc {
					return matching[i].TotalMatches < matching[j].TotalMatches
				}
				return matching[i].TotalMatches > matching[j].TotalMatches
			}
		case "win_rate":
			tmGamesI := matching[i].TotalWinsAsTeammate + matching[i].TotalLossesAsTeammate
			tmGamesJ := matching[j].TotalWinsAsTeammate + matching[j].TotalLossesAsTeammate
			rateI := 0.0
			if tmGamesI > 0 {
				rateI = float64(matching[i].TotalWinsAsTeammate) / float64(tmGamesI)
			}
			rateJ := 0.0
			if tmGamesJ > 0 {
				rateJ = float64(matching[j].TotalWinsAsTeammate) / float64(tmGamesJ)
			}
			if rateI != rateJ {
				if isAsc {
					return rateI < rateJ
				}
				return rateI > rateJ
			}
			if matching[i].TotalMatches != matching[j].TotalMatches {
				if isAsc {
					return matching[i].TotalMatches < matching[j].TotalMatches
				}
				return matching[i].TotalMatches > matching[j].TotalMatches
			}
		case "name":
			nameI := strings.ToLower(matching[i].PlayerName)
			nameJ := strings.ToLower(matching[j].PlayerName)
			if nameI != nameJ {
				if isAsc {
					return nameI < nameJ
				}
				return nameI > nameJ
			}
		default: // "last_seen"
			if !matching[i].LastSeenAt.Equal(matching[j].LastSeenAt) {
				if isAsc {
					return matching[i].LastSeenAt.Before(matching[j].LastSeenAt)
				}
				return matching[i].LastSeenAt.After(matching[j].LastSeenAt)
			}
		}

		// Fallback tie-breaker: last_seen_at (respecting isAsc), player_id ASC
		if !matching[i].LastSeenAt.Equal(matching[j].LastSeenAt) {
			if isAsc {
				return matching[i].LastSeenAt.Before(matching[j].LastSeenAt)
			}
			return matching[i].LastSeenAt.After(matching[j].LastSeenAt)
		}
		return matching[i].PlayerID < matching[j].PlayerID
	})

	end := total
	if filter.Limit > 0 {
		end = offset + filter.Limit
		if end > total {
			end = total
		}
	}

	results := make([]*PlayerSummary, end-offset)
	copy(results, matching[offset:end])
	return results, total, nil
}

// SearchPlayerSummaries searches players matching query across player_name and player_id
// (case-insensitive substring match), optionally filtered by platform (case-insensitive; "all" or "" means
// all platforms), returning paginated PlayerSummary records and the total count of matching records across all pages.
// Results are ordered by last_seen_at DESC, player_id ASC.
func (s *JSONStore) SearchPlayerSummaries(ctx context.Context, query, platform string, limit, offset int) ([]*PlayerSummary, int, error) {
	return s.FilterPlayerSummaries(ctx, PlayerDirectoryFilter{
		Query:    query,
		Platform: platform,
		Role:     "all",
		Sort:     "last_seen",
		Order:    "desc",
		Limit:    limit,
		Offset:   offset,
	})
}

// GetPlayerSpotlights computes dataset-wide global spotlights.
func (s *JSONStore) GetPlayerSpotlights(ctx context.Context) (*GlobalSpotlights, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	candidates := make([]*PlayerSummary, 0)
	for _, p := range s.players {
		sum := &PlayerSummary{
			PlayerRecord: *clonePlayerRecord(p),
		}
		if matchups, ok := s.playerMatchups[p.PlayerID]; ok {
			for _, m := range matchups {
				if m == nil {
					continue
				}
				sum.TotalWinsAsTeammate += m.WinsAsTeammate
				sum.TotalLossesAsTeammate += m.LossesAsTeammate
				sum.TotalWinsAsOpponent += m.WinsAsOpponent
				sum.TotalLossesAsOpponent += m.LossesAsOpponent
				sum.TotalMatches += m.TotalMatches
			}
		}
		if sum.TotalMatches > 0 {
			candidates = append(candidates, sum)
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].PlayerID < candidates[j].PlayerID
	})

	return ComputeGlobalSpotlights(candidates), nil
}

// GetLocalPlayerIDs returns all known local player account and platform IDs from auth and match records.
func (s *JSONStore) GetLocalPlayerIDs(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	idMap := make(map[string]bool)
	for _, a := range s.auth {
		if strings.TrimSpace(a.AccountID) != "" {
			idMap[strings.TrimSpace(a.AccountID)] = true
		}
	}

	for _, mps := range s.matchPlayers {
		for _, mp := range mps {
			if mp.IsLocal && strings.TrimSpace(mp.PlayerID) != "" {
				idMap[strings.TrimSpace(mp.PlayerID)] = true
			}
		}
	}

	ids := make([]string, 0, len(idMap))
	for id := range idMap {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

// UpdatePlayerRanks updates the ranks_json payload and last_seen_at timestamp for a player.
func (s *JSONStore) UpdatePlayerRanks(ctx context.Context, playerID string, ranksJSON string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(playerID) == "" {
		return ErrPlayerNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return ErrStoreClosed
	}

	resolvedID := s.resolveCanonicalPlayerIDLocked(playerID)
	rec, ok := s.players[resolvedID]
	if !ok {
		return ErrPlayerNotFound
	}

	if strings.TrimSpace(ranksJSON) == "" {
		ranksJSON = "{}"
	}
	rec.RanksJSON = ranksJSON
	now := time.Now().UTC()
	if rec.LastSeenAt.IsZero() || now.After(rec.LastSeenAt) {
		rec.LastSeenAt = now
	}

	return s.saveLocked()
}

// RecordMatchResults atomically records all player outcomes for a match and registers the match GUID
// in processedMatches. If matchGUID was already processed, returns ErrMatchAlreadyProcessed.
// Optional matchTime preserves the original replay timestamp rather than wall-clock execution time.
func (s *JSONStore) RecordMatchResults(ctx context.Context, matchGUID string, playlistID int, outcomes []PlayerOutcome, matchTime ...time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(matchGUID) == "" {
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

	// Idempotency check: if match was already processed, return sentinel error
	if _, exists := s.processedMatches[matchGUID]; exists {
		return ErrMatchAlreadyProcessed
	}

	matchTS := time.Now().UTC().Truncate(time.Second)
	hasExplicitTime := false
	if len(matchTime) > 0 && !matchTime[0].IsZero() {
		matchTS = matchTime[0].UTC().Truncate(time.Second)
		hasExplicitTime = true
	}

	for _, outcome := range outcomes {
		if strings.TrimSpace(outcome.PlayerID) == "" {
			continue
		}

		// Ensure player record exists (referential integrity)
		player, exists := s.players[outcome.PlayerID]
		if !exists {
			player = &PlayerRecord{
				PlayerID:    outcome.PlayerID,
				Platform:    outcome.Platform,
				PlayerName:  outcome.PlayerName,
				RanksJSON:   "{}",
				Tags:        []string{},
				FirstSeenAt: matchTS,
				LastSeenAt:  matchTS,
			}
			s.players[outcome.PlayerID] = player
		} else {
			if hasExplicitTime {
				if matchTS.Before(player.FirstSeenAt) {
					player.FirstSeenAt = matchTS
				}
				if matchTS.After(player.LastSeenAt) {
					player.LastSeenAt = matchTS
				}
			}
			if outcome.Platform != "" {
				player.Platform = outcome.Platform
			}
			if outcome.PlayerName != "" {
				player.PlayerName = outcome.PlayerName
			}
		}

		// Ensure matchup map exists for player
		pMap, ok := s.playerMatchups[outcome.PlayerID]
		if !ok {
			pMap = make(map[int]*PlayerMatchup)
			s.playerMatchups[outcome.PlayerID] = pMap
		}

		// Get or create matchup for playlist
		m, ok := pMap[playlistID]
		if !ok {
			m = &PlayerMatchup{
				PlayerID:     outcome.PlayerID,
				PlaylistID:   playlistID,
				LastPlayedAt: matchTS,
			}
			pMap[playlistID] = m
		}

		// Update 4-way win/loss matrix
		if outcome.IsTeammate {
			if outcome.Won {
				m.WinsAsTeammate++
			} else {
				m.LossesAsTeammate++
			}
		} else {
			if outcome.Won {
				m.WinsAsOpponent++
			} else {
				m.LossesAsOpponent++
			}
		}
		m.TotalMatches++
		if matchTS.After(m.LastPlayedAt) {
			m.LastPlayedAt = matchTS
		}
	}

	// Record match in processed ledger
	s.processedMatches[matchGUID] = matchTS.Unix()

	return s.saveLocked()
}

// GetPlayerMatchup retrieves the head-to-head record for a player in a specific playlist.
// If no record exists, returns zeroed PlayerMatchup with TotalMatches=0 and nil error.
func (s *JSONStore) GetPlayerMatchup(ctx context.Context, playerID string, playlistID int) (*PlayerMatchup, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(playerID) == "" {
		return &PlayerMatchup{PlayerID: playerID, PlaylistID: playlistID}, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	resolvedID := s.resolveCanonicalPlayerIDLocked(playerID)
	pMap, ok := s.playerMatchups[resolvedID]
	if !ok {
		return &PlayerMatchup{
			PlayerID:   resolvedID,
			PlaylistID: playlistID,
		}, nil
	}

	m, ok := pMap[playlistID]
	if !ok {
		return &PlayerMatchup{
			PlayerID:   resolvedID,
			PlaylistID: playlistID,
		}, nil
	}

	return clonePlayerMatchup(m), nil
}

// GetPlayerMatchups retrieves all playlist matchups for a player ordered by playlist_id ASC.
func (s *JSONStore) GetPlayerMatchups(ctx context.Context, playerID string) ([]*PlayerMatchup, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(playerID) == "" {
		return []*PlayerMatchup{}, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	resolvedID := s.resolveCanonicalPlayerIDLocked(playerID)
	pMap, ok := s.playerMatchups[resolvedID]
	if !ok || len(pMap) == 0 {
		return []*PlayerMatchup{}, nil
	}

	results := make([]*PlayerMatchup, 0, len(pMap))
	for _, m := range pMap {
		results = append(results, clonePlayerMatchup(m))
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].PlaylistID < results[j].PlaylistID
	})

	return results, nil
}

// cloneMatchPlayerRecord creates a deep copy of MatchPlayerRecord to prevent data races.
func cloneMatchPlayerRecord(src *MatchPlayerRecord) *MatchPlayerRecord {
	if src == nil {
		return nil
	}
	dst := *src
	if src.Won != nil {
		w := *src.Won
		dst.Won = &w
	}
	return &dst
}

// cloneMMRHistoryRecord creates a deep copy of MMRHistoryRecord to prevent data races.
func cloneMMRHistoryRecord(src *MMRHistoryRecord) *MMRHistoryRecord {
	if src == nil {
		return nil
	}
	dst := *src
	return &dst
}

// SyncReplayStats enriches or upserts a match record with end-of-game replay statistics,
// keyed by MatchGUID, and populates player records and match_players.
func (s *JSONStore) SyncReplayStats(ctx context.Context, stats *ReplayMatchStats) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if stats == nil || strings.TrimSpace(stats.MatchGUID) == "" {
		return ErrInvalidGUID
	}

	guid := strings.TrimSpace(stats.MatchGUID)

	// If team scores are 0-0, compute fallback scores from player goal counts
	if stats.BlueScore == 0 && stats.OrangeScore == 0 && len(stats.Players) > 0 {
		bGoals, oGoals := 0, 0
		for _, p := range stats.Players {
			if p.Team == 0 {
				bGoals += p.Goals
			} else if p.Team == 1 {
				oGoals += p.Goals
			}
		}
		if bGoals > 0 || oGoals > 0 {
			stats.BlueScore = bGoals
			stats.OrangeScore = oGoals
		}
	}

	// Ensure WinnerTeam is consistent with scores when unset or previously -1 on a non-tied game
	if (stats.WinnerTeam == nil || *stats.WinnerTeam == -1) && stats.BlueScore != stats.OrangeScore {
		wt := 0
		if stats.OrangeScore > stats.BlueScore {
			wt = 1
		}
		stats.WinnerTeam = &wt
	}

	if strings.Contains(stats.BallchasingURL, "/api/replays/") && stats.BallchasingID != "" {
		stats.BallchasingURL = "https://ballchasing.com/replay/" + stats.BallchasingID
	}

	statsJSON, err := json.Marshal(stats)
	if err != nil {
		return fmt.Errorf("failed to marshal replay stats: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreClosed
	}

	now := time.Now().UTC()
	startTS := now.Unix()
	if stats.Date != "" {
		if t, ok := ParseReplayDate(stats.Date); ok {
			startTS = t.Unix()
		}
	}

	rec, exists := s.matches[guid]
	if !exists {
		upStatus := UploadPending
		syncStatus := StatsSyncPending
		if stats.BallchasingID != "" {
			upStatus = UploadUploaded
			syncStatus = StatsSyncSynced
		}
		rec = &MatchRecord{
			MatchGUID:            guid,
			RecordStartTimestamp: startTS,
			MapName:              stats.MapName,
			Playlist:             stats.Playlist,
			DownloadStatus:       DownloadDownloaded,
			UploadStatus:         upStatus,
			BallchasingID:        stats.BallchasingID,
			BallchasingURL:       stats.BallchasingURL,
			BlueScore:            stats.BlueScore,
			OrangeScore:          stats.OrangeScore,
			WinnerTeam:           stats.WinnerTeam,
			ReplayStatsJSON:      string(statsJSON),
			SyncedAt:             &now,
			StatsSyncStatus:      syncStatus,
			StatsRetryCount:      0,
			CreatedAt:            now,
			UpdatedAt:            now,
		}
		s.matches[guid] = rec
	} else {
		if rec.RecordStartTimestamp <= 0 {
			rec.RecordStartTimestamp = startTS
		}
		if stats.MapName != "" {
			rec.MapName = stats.MapName
		}
		if stats.Playlist != 0 {
			rec.Playlist = stats.Playlist
		}
		if stats.BallchasingID != "" {
			rec.BallchasingID = stats.BallchasingID
			rec.UploadStatus = UploadUploaded
			rec.StatsSyncStatus = StatsSyncSynced
		}
		if stats.BallchasingURL != "" {
			rec.BallchasingURL = stats.BallchasingURL
		}
		rec.BlueScore = stats.BlueScore
		rec.OrangeScore = stats.OrangeScore
		rec.WinnerTeam = stats.WinnerTeam
		rec.ReplayStatsJSON = string(statsJSON)
		rec.SyncedAt = &now
		rec.UpdatedAt = now
		if rec.DownloadStatus == DownloadPending {
			rec.DownloadStatus = DownloadDownloaded
		}
	}

	// Upsert players and match_players
	localAccounts := make(map[string]bool)
	for _, a := range s.auth {
		if a.AccountID != "" {
			localAccounts[strings.ToLower(a.AccountID)] = true
		}
	}

	matchPlayers := make([]*MatchPlayerRecord, 0, len(stats.Players))
	matchTime := time.Unix(startTS, 0).UTC()

	for _, p := range stats.Players {
		pid := strings.TrimSpace(p.OnlineID)
		if pid == "" {
			continue
		}
		plat := strings.TrimSpace(p.Platform)
		if plat == "" {
			plat = "Unknown"
		}
		canonicalID := pid
		if !strings.Contains(canonicalID, "|") {
			canonicalID = fmt.Sprintf("%s|%s|0", plat, pid)
		}

		isLocal := localAccounts[strings.ToLower(pid)] || localAccounts[strings.ToLower(canonicalID)]

		var won *bool
		if stats.WinnerTeam != nil && *stats.WinnerTeam >= 0 {
			w := (*stats.WinnerTeam == p.Team)
			won = &w
		}

		var pStatsJSON string
		if p.Core != nil || p.Boost != nil || p.Movement != nil || p.Positioning != nil || p.Demo != nil || p.Camera != nil || p.Rank != nil {
			if b, err := json.Marshal(p); err == nil {
				pStatsJSON = string(b)
			}
		}

		mp := &MatchPlayerRecord{
			MatchGUID: guid,
			PlayerID:  canonicalID,
			TeamNum:   p.Team,
			IsLocal:   isLocal,
			Won:       won,
			Score:     p.Score,
			Goals:     p.Goals,
			Assists:   p.Assists,
			Saves:     p.Saves,
			Shots:     p.Shots,
			Demos:     p.Demos,
			CarName:   p.CarName,
			StatsJSON: pStatsJSON,
		}
		matchPlayers = append(matchPlayers, mp)

		if !isLocal {
			pRec, pExists := s.players[canonicalID]
			if !pExists {
				s.players[canonicalID] = &PlayerRecord{
					PlayerID:    canonicalID,
					Platform:    plat,
					PlayerName:  strings.TrimSpace(p.Name),
					RanksJSON:   "{}",
					Tags:        []string{},
					FirstSeenAt: matchTime,
					LastSeenAt:  matchTime,
				}
			} else {
				pRec.Platform = plat
				pRec.PlayerName = strings.TrimSpace(p.Name)
				if matchTime.Before(pRec.FirstSeenAt) {
					pRec.FirstSeenAt = matchTime
				}
				if matchTime.After(pRec.LastSeenAt) {
					pRec.LastSeenAt = matchTime
				}
			}
		}
	}

	if len(matchPlayers) > 0 {
		s.saveMatchPlayersLocked(guid, matchPlayers)
	}

	s.hydrateRanksFromStatsLocked(stats)

	return s.saveLocked()
}

// ListHistoricalMatches returns paginated historical matches matching the filter.
func (s *JSONStore) ListHistoricalMatches(ctx context.Context, filter MatchFilter) ([]*HistoricalMatchRecord, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, 0, ErrStoreClosed
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	cleanResult := strings.ToLower(strings.TrimSpace(filter.Result))
	q := strings.ToLower(strings.TrimSpace(filter.Query))

	var filtered []*HistoricalMatchRecord

	for _, m := range s.matches {
		if filter.Playlist > 0 && m.Playlist != filter.Playlist {
			continue
		}
		if q != "" {
			if !strings.Contains(strings.ToLower(m.MatchGUID), q) &&
				!strings.Contains(strings.ToLower(m.MapName), q) &&
				!strings.Contains(strings.ToLower(m.ReplayStatsJSON), q) {
				continue
			}
		}

		localTeam := -1
		if mps, ok := s.matchPlayers[m.MatchGUID]; ok {
			for _, mp := range mps {
				if mp.IsLocal {
					localTeam = mp.TeamNum
					break
				}
			}
		}

		outcome := "unknown"
		if localTeam >= 0 {
			if localTeam == 0 {
				if m.BlueScore > m.OrangeScore {
					outcome = "victory"
				} else if m.OrangeScore > m.BlueScore {
					outcome = "defeat"
				} else if m.WinnerTeam != nil && *m.WinnerTeam != -1 {
					if *m.WinnerTeam == 0 {
						outcome = "victory"
					} else {
						outcome = "defeat"
					}
				} else if (m.BlueScore == m.OrangeScore && (m.BlueScore > 0 || m.OrangeScore > 0)) || (m.WinnerTeam != nil && *m.WinnerTeam == -1) {
					outcome = "draw"
				} else {
					outcome = "draw"
				}
			} else if localTeam == 1 {
				if m.OrangeScore > m.BlueScore {
					outcome = "victory"
				} else if m.BlueScore > m.OrangeScore {
					outcome = "defeat"
				} else if m.WinnerTeam != nil && *m.WinnerTeam != -1 {
					if *m.WinnerTeam == 1 {
						outcome = "victory"
					} else {
						outcome = "defeat"
					}
				} else if (m.BlueScore == m.OrangeScore && (m.BlueScore > 0 || m.OrangeScore > 0)) || (m.WinnerTeam != nil && *m.WinnerTeam == -1) {
					outcome = "draw"
				} else {
					outcome = "draw"
				}
			}
		} else {
			if m.BlueScore > m.OrangeScore {
				outcome = "victory"
			} else if m.OrangeScore > m.BlueScore {
				outcome = "defeat"
			} else if m.WinnerTeam != nil && *m.WinnerTeam != -1 {
				if *m.WinnerTeam == 0 {
					outcome = "victory"
				} else {
					outcome = "defeat"
				}
			} else if (m.BlueScore == m.OrangeScore && (m.BlueScore > 0 || m.OrangeScore > 0)) || (m.WinnerTeam != nil && *m.WinnerTeam == -1) {
				outcome = "draw"
			} else {
				outcome = "draw"
			}
		}

		if cleanResult != "" && cleanResult != "all" && cleanResult != outcome {
			continue
		}

		hist := &HistoricalMatchRecord{
			MatchRecord: *cloneMatchRecord(m),
			Result:      outcome,
		}
		if m.ReplayStatsJSON != "" {
			var st ReplayMatchStats
			if err := json.Unmarshal([]byte(m.ReplayStatsJSON), &st); err == nil {
				hist.Stats = &st
			}
		}
		filtered = append(filtered, hist)
	}

	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].RecordStartTimestamp != filtered[j].RecordStartTimestamp {
			return filtered[i].RecordStartTimestamp > filtered[j].RecordStartTimestamp
		}
		return filtered[i].MatchGUID < filtered[j].MatchGUID
	})

	total := len(filtered)
	if offset >= total {
		return make([]*HistoricalMatchRecord, 0), total, nil
	}

	end := offset + limit
	if end > total {
		end = total
	}

	results := make([]*HistoricalMatchRecord, end-offset)
	copy(results, filtered[offset:end])
	return results, total, nil
}

// hydrateRanksFromStatsLocked attempts to parse rank metadata from replay stats or raw JSON and update player ranks.
// Must be called with s.mu held for writing.
func (s *JSONStore) hydrateRanksFromStatsLocked(stats *ReplayMatchStats) {
	if stats == nil || stats.Playlist <= 0 {
		return
	}

	applyRank := func(idStr string, r *ballchasing.PlayerRank) {
		if idStr == "" || r == nil || (r.Tier == 0 && (r.ID == "" || r.ID == "0")) {
			return
		}
		resolvedID := s.resolveCanonicalPlayerIDLocked(idStr)
		pRec, ok := s.players[resolvedID]
		if !ok {
			return
		}

		ranks := make(map[string]map[string]interface{})
		if pRec.RanksJSON != "" && pRec.RanksJSON != "{}" {
			_ = json.Unmarshal([]byte(pRec.RanksJSON), &ranks)
		}

		rName := r.Name
		if rName == "" {
			rName = formatRankName(r.Tier, r.Division)
		}

		pKey := strconv.Itoa(stats.Playlist)
		existingMMR := 0.0
		existingMatches := 0
		if existing, ok := ranks[pKey]; ok && existing != nil {
			if mmrVal, ok := existing["mmr"].(float64); ok && mmrVal > 0 {
				existingMMR = mmrVal
			}
			if mpVal, ok := existing["matches_played"].(float64); ok && mpVal > 0 {
				existingMatches = int(mpVal)
			}
		}

		ranks[pKey] = map[string]interface{}{
			"playlist_id":    stats.Playlist,
			"playlist_name":  formatPlaylistName(stats.Playlist),
			"tier":           r.Tier,
			"division":       r.Division,
			"rank_name":      rName,
			"mmr":            existingMMR,
			"matches_played": existingMatches,
		}

		if updatedData, err := json.Marshal(ranks); err == nil {
			pRec.RanksJSON = string(updatedData)
		}
	}

	for _, p := range stats.Players {
		if p.Rank == nil {
			continue
		}
		pid := strings.TrimSpace(p.OnlineID)
		plat := strings.TrimSpace(p.Platform)
		if plat == "" {
			plat = "Unknown"
		}
		canonicalID := pid
		if pid != "" && !strings.Contains(canonicalID, "|") {
			canonicalID = fmt.Sprintf("%s|%s|0", plat, pid)
		}
		applyRank(canonicalID, p.Rank)
	}

	if stats.RawJSON == "" {
		return
	}

	type bcPlayerRank struct {
		ID       interface{} `json:"id"`
		Tier     int         `json:"tier"`
		Division int         `json:"division"`
		Name     string      `json:"name"`
	}
	type bcPlayer struct {
		ID   interface{}   `json:"id"`
		Rank *bcPlayerRank `json:"rank"`
	}
	type bcTeam struct {
		Players []bcPlayer `json:"players"`
	}
	type bcDetail struct {
		Blue   bcTeam `json:"blue"`
		Orange bcTeam `json:"orange"`
	}

	var detail bcDetail
	if err := json.Unmarshal([]byte(stats.RawJSON), &detail); err != nil {
		return
	}

	allPlayers := append(detail.Blue.Players, detail.Orange.Players...)
	for _, bp := range allPlayers {
		if bp.Rank == nil {
			continue
		}
		idStrVal := fmt.Sprintf("%v", bp.Rank.ID)
		if bp.Rank.Tier == 0 && (bp.Rank.ID == nil || idStrVal == "" || idStrVal == "0") {
			continue
		}

		var idStr string
		switch v := bp.ID.(type) {
		case string:
			idStr = v
		case map[string]interface{}:
			if idVal, ok := v["id"].(string); ok {
				idStr = idVal
			}
		}
		if idStr == "" {
			continue
		}

		applyRank(idStr, &ballchasing.PlayerRank{
			ID:       idStrVal,
			Tier:     bp.Rank.Tier,
			Division: bp.Rank.Division,
			Name:     bp.Rank.Name,
		})
	}
}

// saveMatchPlayersLocked bulk upserts player match participation records for a match.
// It strictly enforces composite primary key uniqueness on (match_guid, player_id):
// - Any duplicates within incoming records or against existing records are merged by player_id.
// - Later incoming records update earlier records for stats, won, team_num, and is_local.
// - Existing non-empty car_name and stats_json are preserved if incoming values are empty
//   (matching SQLiteStore's CASE WHEN excluded.car_name != '' THEN ... END).
// - The resulting slice is ordered deterministically by team_num ASC, score DESC, player_id ASC.
// Caller MUST hold s.mu for writing.
func (s *JSONStore) saveMatchPlayersLocked(matchGUID string, players []*MatchPlayerRecord) {
	if s.matchPlayers == nil {
		s.matchPlayers = make(map[string][]*MatchPlayerRecord)
	}

	existing := s.matchPlayers[matchGUID]
	byPID := make(map[string]*MatchPlayerRecord, len(existing)+len(players))
	for _, mp := range existing {
		if mp != nil && strings.TrimSpace(mp.PlayerID) != "" {
			byPID[mp.PlayerID] = cloneMatchPlayerRecord(mp)
		}
	}

	for _, p := range players {
		if p == nil || strings.TrimSpace(p.PlayerID) == "" {
			continue
		}
		canonicalID := strings.TrimSpace(p.PlayerID)
		prev, exists := byPID[canonicalID]
		if !exists {
			cp := cloneMatchPlayerRecord(p)
			cp.MatchGUID = matchGUID
			cp.PlayerID = canonicalID
			byPID[canonicalID] = cp
		} else {
			// Update existing record, preserving car_name and stats_json if empty in incoming record
			prev.TeamNum = p.TeamNum
			prev.IsLocal = p.IsLocal
			if p.Won != nil {
				w := *p.Won
				prev.Won = &w
			} else {
				prev.Won = nil
			}
			prev.Score = p.Score
			prev.Goals = p.Goals
			prev.Assists = p.Assists
			prev.Saves = p.Saves
			prev.Shots = p.Shots
			prev.Demos = p.Demos
			if p.CarName != "" {
				prev.CarName = p.CarName
			}
			if p.StatsJSON != "" {
				prev.StatsJSON = p.StatsJSON
			}
		}
	}

	newList := make([]*MatchPlayerRecord, 0, len(byPID))
	for _, mp := range byPID {
		newList = append(newList, mp)
	}
	sort.Slice(newList, func(i, j int) bool {
		if newList[i].TeamNum != newList[j].TeamNum {
			return newList[i].TeamNum < newList[j].TeamNum
		}
		if newList[i].Score != newList[j].Score {
			return newList[i].Score > newList[j].Score
		}
		return newList[i].PlayerID < newList[j].PlayerID
	})

	s.matchPlayers[matchGUID] = newList
}

// backfillMatchPlayersLocked populates matchPlayers from existing matches with replay_stats_json if missing.
// Must be called with s.mu held for writing.
func (s *JSONStore) backfillMatchPlayersLocked() {
	if s.matchPlayers == nil {
		s.matchPlayers = make(map[string][]*MatchPlayerRecord)
	}

	// Sanitize any existing in-memory/disk-loaded records to guarantee composite PK uniqueness
	for guid, mps := range s.matchPlayers {
		s.saveMatchPlayersLocked(guid, mps)
	}

	for guid, m := range s.matches {
		if m == nil || m.ReplayStatsJSON == "" {
			continue
		}

		var st ReplayMatchStats
		if err := json.Unmarshal([]byte(m.ReplayStatsJSON), &st); err != nil {
			continue
		}

		// Repair 0-0 score if player goals exist
		if m.BlueScore == 0 && m.OrangeScore == 0 && len(st.Players) > 0 {
			bGoals, oGoals := 0, 0
			for _, p := range st.Players {
				if p.Team == 0 {
					bGoals += p.Goals
				} else if p.Team == 1 {
					oGoals += p.Goals
				}
			}
			if bGoals > 0 || oGoals > 0 {
				m.BlueScore = bGoals
				m.OrangeScore = oGoals
				st.BlueScore = bGoals
				st.OrangeScore = oGoals
				wt := -1
				if bGoals > oGoals {
					wt = 0
				} else if oGoals > bGoals {
					wt = 1
				}
				m.WinnerTeam = &wt
				st.WinnerTeam = &wt
				if b, err := json.Marshal(&st); err == nil {
					m.ReplayStatsJSON = string(b)
				}
			}
		}

		if _, ok := s.matchPlayers[guid]; ok && len(s.matchPlayers[guid]) > 0 {
			continue
		}

		var players []*MatchPlayerRecord
		for _, p := range st.Players {
			pid := strings.TrimSpace(p.OnlineID)
			if pid == "" {
				continue
			}
			plat := strings.TrimSpace(p.Platform)
			if plat == "" {
				plat = "Unknown"
			}
			canonicalID := pid
			if !strings.Contains(canonicalID, "|") {
				canonicalID = fmt.Sprintf("%s|%s|0", plat, pid)
			}

			var won *bool
			if m.WinnerTeam != nil && *m.WinnerTeam >= 0 {
				w := (*m.WinnerTeam == p.Team)
				won = &w
			}

			players = append(players, &MatchPlayerRecord{
				MatchGUID: guid,
				PlayerID:  canonicalID,
				TeamNum:   p.Team,
				IsLocal:   false,
				Won:       won,
				Score:     p.Score,
				Goals:     p.Goals,
				Assists:   p.Assists,
				Saves:     p.Saves,
				Shots:     p.Shots,
				Demos:     p.Demos,
				CarName:   p.CarName,
			})
		}
		if len(players) > 0 {
			s.saveMatchPlayersLocked(guid, players)
		}
	}
}

// SaveMatchPlayers bulk upserts player match participation records for a match.
func (s *JSONStore) SaveMatchPlayers(ctx context.Context, matchGUID string, players []*MatchPlayerRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(matchGUID) == "" {
		return ErrInvalidGUID
	}
	if len(players) == 0 {
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

	s.saveMatchPlayersLocked(matchGUID, players)
	return s.saveLocked()
}

// GetMatchPlayers retrieves all player records for a match GUID ordered by team_num ASC, score DESC.
func (s *JSONStore) GetMatchPlayers(ctx context.Context, matchGUID string) ([]*MatchPlayerRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(matchGUID) == "" {
		return nil, ErrInvalidGUID
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	list, ok := s.matchPlayers[matchGUID]
	if !ok || len(list) == 0 {
		return []*MatchPlayerRecord{}, nil
	}

	results := make([]*MatchPlayerRecord, len(list))
	for i, mp := range list {
		results[i] = cloneMatchPlayerRecord(mp)
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].TeamNum != results[j].TeamNum {
			return results[i].TeamNum < results[j].TeamNum
		}
		return results[i].Score > results[j].Score
	})

	return results, nil
}

// ListPlayerMatches returns paginated match details for a player with role and result calculations.
func (s *JSONStore) ListPlayerMatches(ctx context.Context, playerID string, filter PlayerMatchFilter) ([]*PlayerMatchDetail, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if strings.TrimSpace(playerID) == "" {
		return nil, 0, ErrPlayerNotFound
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, 0, ErrStoreClosed
	}

	resolvedID := s.resolveCanonicalPlayerIDLocked(playerID)

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	cleanResult := strings.ToLower(strings.TrimSpace(filter.Result))
	cleanRole := strings.ToLower(strings.TrimSpace(filter.Role))

	var matchesList []*PlayerMatchDetail

	for matchGUID, mPlayers := range s.matchPlayers {
		m, ok := s.matches[matchGUID]
		if !ok {
			continue
		}

		if filter.Playlist > 0 && m.Playlist != filter.Playlist {
			continue
		}

		var targetMP *MatchPlayerRecord
		localTeam := -1
		for _, mp := range mPlayers {
			if mp.IsLocal {
				localTeam = mp.TeamNum
			}
			if mp.PlayerID == resolvedID {
				targetMP = mp
			}
		}

		if targetMP == nil {
			continue
		}

		role := "teammate"
		if localTeam >= 0 {
			if targetMP.TeamNum == localTeam {
				role = "teammate"
			} else {
				role = "opponent"
			}
		}

		if cleanRole != "" && cleanRole != "all" {
			if cleanRole != role {
				continue
			}
		}

		result := "unknown"
		if targetMP.Won != nil {
			if *targetMP.Won {
				result = "victory"
			} else {
				result = "defeat"
			}
		} else if m.WinnerTeam != nil {
			if *m.WinnerTeam == -1 {
				result = "draw"
			} else if *m.WinnerTeam == targetMP.TeamNum {
				result = "victory"
			} else {
				result = "defeat"
			}
		} else if m.BlueScore == m.OrangeScore && (m.BlueScore > 0 || m.OrangeScore > 0) {
			result = "draw"
		} else if (targetMP.TeamNum == 0 && m.BlueScore > m.OrangeScore) || (targetMP.TeamNum == 1 && m.OrangeScore > m.BlueScore) {
			result = "victory"
		} else if (targetMP.TeamNum == 0 && m.OrangeScore > m.BlueScore) || (targetMP.TeamNum == 1 && m.BlueScore > m.OrangeScore) {
			result = "defeat"
		}

		if cleanResult != "" && cleanResult != "all" {
			if cleanResult != result {
				continue
			}
		}

		var st *ReplayMatchStats
		if m.ReplayStatsJSON != "" {
			var parsed ReplayMatchStats
			if err := json.Unmarshal([]byte(m.ReplayStatsJSON), &parsed); err == nil {
				st = &parsed
			}
		}

		detail := &PlayerMatchDetail{
			MatchRecord: *cloneMatchRecord(m),
			PlayerStats: *cloneMatchPlayerRecord(targetMP),
			Role:        role,
			Result:      result,
			Stats:       st,
		}
		matchesList = append(matchesList, detail)
	}

	sort.Slice(matchesList, func(i, j int) bool {
		if matchesList[i].RecordStartTimestamp != matchesList[j].RecordStartTimestamp {
			return matchesList[i].RecordStartTimestamp > matchesList[j].RecordStartTimestamp
		}
		return matchesList[i].MatchGUID < matchesList[j].MatchGUID
	})

	total := len(matchesList)
	if offset >= total {
		return []*PlayerMatchDetail{}, total, nil
	}

	end := offset + limit
	if end > total {
		end = total
	}

	return matchesList[offset:end], total, nil
}

// RecordMMR inserts a point-in-time competitive MMR history record.
func (s *JSONStore) RecordMMR(ctx context.Context, record *MMRHistoryRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if record == nil {
		return errors.New("record cannot be nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return ErrStoreClosed
	}

	rec := cloneMMRHistoryRecord(record)
	if rec.RecordedAt.IsZero() {
		rec.RecordedAt = time.Now().UTC()
	}
	rec.ID = int64(len(s.mmrHistory) + 1)
	record.ID = rec.ID
	record.RecordedAt = rec.RecordedAt

	s.mmrHistory = append(s.mmrHistory, rec)
	return s.saveLocked()
}

// ListMMRHistory returns competitive MMR history for a playlist ordered by recorded_at DESC, id DESC.
func (s *JSONStore) ListMMRHistory(ctx context.Context, playlistID int, limit int) ([]*MMRHistoryRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	var filtered []*MMRHistoryRecord
	for _, r := range s.mmrHistory {
		if playlistID > 0 && r.PlaylistID != playlistID {
			continue
		}
		filtered = append(filtered, cloneMMRHistoryRecord(r))
	}

	sort.Slice(filtered, func(i, j int) bool {
		if !filtered[i].RecordedAt.Equal(filtered[j].RecordedAt) {
			return filtered[i].RecordedAt.After(filtered[j].RecordedAt)
		}
		return filtered[i].ID > filtered[j].ID
	})

	if len(filtered) > limit {
		filtered = filtered[:limit]
	}

	if filtered == nil {
		filtered = make([]*MMRHistoryRecord, 0)
	}
	return filtered, nil
}

// UpdatePlayerNotesAndTags updates notes and custom tags for a player profile.
func (s *JSONStore) UpdatePlayerNotesAndTags(ctx context.Context, playerID string, notes string, tags []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(playerID) == "" {
		return ErrPlayerNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return ErrStoreClosed
	}

	resolvedID := s.resolveCanonicalPlayerIDLocked(playerID)
	p, ok := s.players[resolvedID]
	if !ok {
		return ErrPlayerNotFound
	}

	p.Notes = notes
	if tags != nil {
		p.Tags = make([]string, len(tags))
		copy(p.Tags, tags)
	} else {
		p.Tags = []string{}
	}

	return s.saveLocked()
}

// ListPendingStatsSync returns matches requiring post-upload Ballchasing stats synchronization.
func (s *JSONStore) ListPendingStatsSync(ctx context.Context, maxRetries int) ([]*MatchRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	now := time.Now().UTC()
	effectiveMaxRetries := maxRetries
	if effectiveMaxRetries <= 0 {
		effectiveMaxRetries = s.maxRetries
		if effectiveMaxRetries <= 0 {
			effectiveMaxRetries = DefaultMaxRetries
		}
	}

	var results []*MatchRecord
	for _, m := range s.matches {
		if (m.UploadStatus == UploadUploaded || m.UploadStatus == UploadDuplicate) && m.BallchasingID != "" {
			if m.StatsSyncStatus == StatsSyncPending {
				results = append(results, cloneMatchRecord(m))
			} else if m.StatsSyncStatus == StatsSyncFailed && m.StatsRetryCount < effectiveMaxRetries {
				delay := CalculateBackoff(m.StatsRetryCount, s.baseBackoff, s.maxBackoff)
				if now.Sub(m.UpdatedAt) >= delay {
					results = append(results, cloneMatchRecord(m))
				}
			}
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].RecordStartTimestamp < results[j].RecordStartTimestamp
	})

	if results == nil {
		results = make([]*MatchRecord, 0)
	}
	return results, nil
}

// MarkStatsSyncing transitions a match's stats sync status to SYNCING.
func (s *JSONStore) MarkStatsSyncing(ctx context.Context, matchGUID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(matchGUID) == "" {
		return ErrInvalidGUID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreClosed
	}

	m, ok := s.matches[matchGUID]
	if !ok {
		return ErrMatchNotFound
	}

	m.StatsSyncStatus = StatsSyncSyncing
	m.UpdatedAt = time.Now().UTC()
	return s.saveLocked()
}

// MarkStatsSynced transitions a match's stats sync status to SYNCED.
func (s *JSONStore) MarkStatsSynced(ctx context.Context, matchGUID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(matchGUID) == "" {
		return ErrInvalidGUID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreClosed
	}

	m, ok := s.matches[matchGUID]
	if !ok {
		return ErrMatchNotFound
	}

	now := time.Now().UTC()
	m.StatsSyncStatus = StatsSyncSynced
	m.SyncedAt = &now
	m.StatsRetryCount = 0
	m.UpdatedAt = now
	return s.saveLocked()
}

// MarkStatsSyncFailed increments retry count and sets stats_sync_status to FAILED.
func (s *JSONStore) MarkStatsSyncFailed(ctx context.Context, matchGUID, errMsg string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(matchGUID) == "" {
		return ErrInvalidGUID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreClosed
	}

	m, ok := s.matches[matchGUID]
	if !ok {
		return ErrMatchNotFound
	}

	now := time.Now().UTC()
	m.StatsSyncStatus = StatsSyncFailed
	m.StatsRetryCount++
	m.LastError = errMsg
	m.UpdatedAt = now
	return s.saveLocked()
}

// ResetMatchRetry resets error counters and resets failed download/upload/sync states to PENDING.
func (s *JSONStore) ResetMatchRetry(ctx context.Context, matchGUID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(matchGUID) == "" {
		return ErrInvalidGUID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreClosed
	}

	m, ok := s.matches[matchGUID]
	if !ok {
		return ErrMatchNotFound
	}

	now := time.Now().UTC()
	m.RetryCount = 0
	m.StatsRetryCount = 0
	m.LastError = ""
	if m.DownloadStatus == DownloadFailed {
		m.DownloadStatus = DownloadPending
	}
	if m.UploadStatus == UploadFailed {
		m.UploadStatus = UploadPending
	}
	if m.StatsSyncStatus == StatsSyncFailed || m.StatsSyncStatus == StatsSyncSyncing {
		m.StatsSyncStatus = StatsSyncPending
	}
	m.UpdatedAt = now
	return s.saveLocked()
}

func copySessionRecord(src *SessionRecord) *SessionRecord {
	if src == nil {
		return nil
	}
	cp := *src
	if src.EndedAt != nil {
		t := src.EndedAt.UTC()
		cp.EndedAt = &t
	}
	if src.LastMatchAt != nil {
		t := src.LastMatchAt.UTC()
		cp.LastMatchAt = &t
	}
	if len(src.LocalPlayerIDs) > 0 {
		cp.LocalPlayerIDs = make([]string, len(src.LocalPlayerIDs))
		copy(cp.LocalPlayerIDs, src.LocalPlayerIDs)
	} else {
		cp.LocalPlayerIDs = []string{}
	}
	return &cp
}

// SaveSession inserts or updates a persistent play session record.
func (s *JSONStore) SaveSession(ctx context.Context, session *SessionRecord) error {
	if err := ctx.Err(); err != nil {
		return wrapContextErr(err)
	}
	if session == nil || strings.TrimSpace(session.SessionID) == "" {
		return errors.New("session ID cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreClosed
	}

	cp := copySessionRecord(session)
	cp.SessionID = strings.TrimSpace(cp.SessionID)
	now := time.Now().UTC()
	if cp.UpdatedAt.IsZero() {
		cp.UpdatedAt = now
	} else {
		cp.UpdatedAt = cp.UpdatedAt.UTC()
	}
	if cp.StartedAt.IsZero() {
		cp.StartedAt = cp.UpdatedAt
	} else {
		cp.StartedAt = cp.StartedAt.UTC()
	}
	if strings.TrimSpace(cp.PlaylistsJSON) == "" {
		cp.PlaylistsJSON = "{}"
	}
	if strings.TrimSpace(cp.MatchesJSON) == "" {
		cp.MatchesJSON = "[]"
	}

	if s.sessions == nil {
		s.sessions = make(map[string]*SessionRecord)
	}
	s.sessions[cp.SessionID] = cp
	return s.saveLocked()
}

// GetSession retrieves a persistent play session by its ID.
func (s *JSONStore) GetSession(ctx context.Context, sessionID string) (*SessionRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContextErr(err)
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, ErrSessionNotFound
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreClosed
	}

	rec, ok := s.sessions[sessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return copySessionRecord(rec), nil
}

// ListSessions returns a paginated list of persisted play sessions ordered most recent first.
func (s *JSONStore) ListSessions(ctx context.Context, limit, offset int) ([]*SessionRecord, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, wrapContextErr(err)
	}
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, 0, ErrStoreClosed
	}

	all := make([]*SessionRecord, 0, len(s.sessions))
	for _, rec := range s.sessions {
		all = append(all, copySessionRecord(rec))
	}

	sort.Slice(all, func(i, j int) bool {
		ti := all[i].StartedAt
		if all[i].LastMatchAt != nil && !all[i].LastMatchAt.IsZero() {
			ti = *all[i].LastMatchAt
		}
		tj := all[j].StartedAt
		if all[j].LastMatchAt != nil && !all[j].LastMatchAt.IsZero() {
			tj = *all[j].LastMatchAt
		}
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		if !all[i].StartedAt.Equal(all[j].StartedAt) {
			return all[i].StartedAt.After(all[j].StartedAt)
		}
		return all[i].SessionID < all[j].SessionID
	})

	total := len(all)
	if offset >= total {
		return []*SessionRecord{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return all[offset:end], total, nil
}

// DeleteSession deletes a play session record by ID.
func (s *JSONStore) DeleteSession(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return wrapContextErr(err)
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreClosed
	}

	if _, ok := s.sessions[sessionID]; !ok {
		return nil
	}
	delete(s.sessions, sessionID)
	return s.saveLocked()
}

