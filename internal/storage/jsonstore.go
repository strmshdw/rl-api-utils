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
	Version          int                               `json:"version"`
	UpdatedAt        time.Time                         `json:"updated_at"`
	Matches          map[string]*MatchRecord           `json:"matches"`
	Auth             map[string]*AuthRecord            `json:"auth"`
	Players          map[string]*PlayerRecord          `json:"players,omitempty"`
	PlayerMatchups   map[string]map[int]*PlayerMatchup `json:"player_matchups,omitempty"`
	ProcessedMatches map[string]int64                  `json:"processed_matches,omitempty"`
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
	closed           bool
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
		filePath:         filePath,
		matches:          make(map[string]*MatchRecord),
		auth:             make(map[string]*AuthRecord),
		players:          make(map[string]*PlayerRecord),
		playerMatchups:   make(map[string]map[int]*PlayerMatchup),
		processedMatches: make(map[string]int64),
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
	if payload.Players != nil {
		store.players = payload.Players
	}
	if payload.PlayerMatchups != nil {
		store.playerMatchups = payload.PlayerMatchups
	}
	if payload.ProcessedMatches != nil {
		store.processedMatches = payload.ProcessedMatches
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
		Version:          1,
		UpdatedAt:        time.Now().UTC(),
		Matches:          s.matches,
		Auth:             s.auth,
		Players:          s.players,
		PlayerMatchups:   s.playerMatchups,
		ProcessedMatches: s.processedMatches,
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

// clonePlayerRecord creates a deep copy of PlayerRecord to prevent data races.
func clonePlayerRecord(src *PlayerRecord) *PlayerRecord {
	if src == nil {
		return nil
	}
	dst := *src
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
	return &dst
}

// UpsertPlayer inserts or updates a player record, updating player_name, platform, and last_seen_at.
// Preserves ranks_json and first_seen_at if existing.
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

	now := time.Now().UTC()
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
		if !player.LastSeenAt.IsZero() {
			existing.LastSeenAt = player.LastSeenAt
		} else {
			existing.LastSeenAt = now
		}
	} else {
		rec := clonePlayerRecord(player)
		if rec.FirstSeenAt.IsZero() {
			rec.FirstSeenAt = now
		}
		if rec.LastSeenAt.IsZero() {
			rec.LastSeenAt = now
		}
		if rec.RanksJSON == "" {
			rec.RanksJSON = "{}"
		}
		s.players[player.PlayerID] = rec
	}

	return s.saveLocked()
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

	rec, ok := s.players[playerID]
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

// SearchPlayerSummaries searches players matching query across player_name and player_id
// (case-insensitive substring match), optionally filtered by platform (case-insensitive; "all" or "" means
// all platforms), returning paginated PlayerSummary records and the total count of matching records across all pages.
// Results are ordered by last_seen_at DESC, player_id ASC.
func (s *JSONStore) SearchPlayerSummaries(ctx context.Context, query, platform string, limit, offset int) ([]*PlayerSummary, int, error) {
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

	cleanQuery := strings.TrimSpace(strings.ToLower(query))
	cleanPlatform := strings.TrimSpace(strings.ToLower(platform))
	hasQuery := cleanQuery != ""
	hasPlatform := cleanPlatform != "" && cleanPlatform != "all"

	// 1. In-memory filter
	matching := make([]*PlayerRecord, 0)
	for _, p := range s.players {
		if hasPlatform && strings.ToLower(p.Platform) != cleanPlatform {
			continue
		}
		if hasQuery {
			nameMatch := strings.Contains(strings.ToLower(p.PlayerName), cleanQuery)
			idMatch := strings.Contains(strings.ToLower(p.PlayerID), cleanQuery)
			if !nameMatch && !idMatch {
				continue
			}
		}
		matching = append(matching, p)
	}

	total := len(matching)

	// Boundary shortcuts: total 0 or limit 0 return empty slice with accurate total
	if total == 0 || limit == 0 {
		return make([]*PlayerSummary, 0), total, nil
	}

	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return make([]*PlayerSummary, 0), total, nil
	}

	// 2. Deterministic sort: last_seen_at DESC, player_id ASC
	sort.Slice(matching, func(i, j int) bool {
		if !matching[i].LastSeenAt.Equal(matching[j].LastSeenAt) {
			return matching[i].LastSeenAt.After(matching[j].LastSeenAt)
		}
		return matching[i].PlayerID < matching[j].PlayerID
	})

	end := total
	if limit > 0 {
		end = offset + limit
		if end > total {
			end = total
		}
	}

	// 3. Slice and aggregate matchups
	results := make([]*PlayerSummary, 0, end-offset)
	for _, p := range matching[offset:end] {
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
		results = append(results, sum)
	}

	return results, total, nil
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

	rec, ok := s.players[playerID]
	if !ok {
		return ErrPlayerNotFound
	}

	if strings.TrimSpace(ranksJSON) == "" {
		ranksJSON = "{}"
	}
	rec.RanksJSON = ranksJSON
	rec.LastSeenAt = time.Now().UTC()

	return s.saveLocked()
}

// RecordMatchResults atomically records all player outcomes for a match and registers the match GUID
// in processedMatches. If matchGUID was already processed, returns ErrMatchAlreadyProcessed.
func (s *JSONStore) RecordMatchResults(ctx context.Context, matchGUID string, playlistID int, outcomes []PlayerOutcome) error {
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

	now := time.Now().UTC()

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
				FirstSeenAt: now,
				LastSeenAt:  now,
			}
			s.players[outcome.PlayerID] = player
		} else {
			player.LastSeenAt = now
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
				LastPlayedAt: now,
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
		m.LastPlayedAt = now
	}

	// Record match in processed ledger
	s.processedMatches[matchGUID] = now.Unix()

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

	pMap, ok := s.playerMatchups[playerID]
	if !ok {
		return &PlayerMatchup{
			PlayerID:   playerID,
			PlaylistID: playlistID,
		}, nil
	}

	m, ok := pMap[playlistID]
	if !ok {
		return &PlayerMatchup{
			PlayerID:   playerID,
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

	pMap, ok := s.playerMatchups[playerID]
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

