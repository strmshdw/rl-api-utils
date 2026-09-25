package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/testutil"
)

// --- Domain Models & Interface Contracts (as defined in PROJECT.md) ---

type DownloadStatus string

const (
	DownloadPending     DownloadStatus = "PENDING"
	DownloadDownloading DownloadStatus = "DOWNLOADING"
	DownloadDownloaded  DownloadStatus = "DOWNLOADED"
	DownloadFailed      DownloadStatus = "FAILED"
	DownloadSkipped     DownloadStatus = "SKIPPED"
)

type UploadStatus string

const (
	UploadPending   UploadStatus = "PENDING"
	UploadUploading UploadStatus = "UPLOADING"
	UploadUploaded  UploadStatus = "UPLOADED"
	UploadDuplicate UploadStatus = "DUPLICATE"
	UploadFailed    UploadStatus = "FAILED"
)

type MatchRecord struct {
	MatchGUID            string
	RecordStartTimestamp int64
	MapName              string
	Playlist             int
	ReplayURL            string
	DownloadStatus       DownloadStatus
	LocalFilePath        string
	DownloadedAt         *time.Time
	UploadStatus         UploadStatus
	BallchasingID        string
	BallchasingURL       string
	UploadedAt           *time.Time
	RetryCount           int
	LastError            string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type StateStore interface {
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
	SaveAuthState(ctx context.Context, provider, refreshToken, accountID, displayName string) error
	GetAuthState(ctx context.Context, provider string) (refreshToken, accountID, displayName string, err error)
	Close() error
}

type DiscoveredMatch struct {
	MatchGUID            string
	RecordStartTimestamp int64
	MapName              string
	Playlist             int
	ReplayURL            string
}

type MatchHistoryProvider interface {
	GetRecentMatches(ctx context.Context) ([]DiscoveredMatch, error)
	Close() error
}

type ReplayDownloader interface {
	DownloadReplay(ctx context.Context, matchGUID, replayURL, destDir string) (localPath string, err error)
}

type UploadResult struct {
	ID          string
	Location    string
	IsDuplicate bool
}

type ReplayUploader interface {
	UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error)
}

// --- In-Memory StateStore Reference Implementation for E2E Tests ---

type MemoryStateStore struct {
	mu        sync.RWMutex
	matches   map[string]*MatchRecord
	authState map[string][3]string // provider -> [refresh_token, account_id, display_name]
	closed    bool
}

func NewMemoryStateStore() *MemoryStateStore {
	return &MemoryStateStore{
		matches:   make(map[string]*MatchRecord),
		authState: make(map[string][3]string),
	}
}

func (s *MemoryStateStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *MemoryStateStore) GetMatch(ctx context.Context, matchGUID string) (*MatchRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if rec, ok := s.matches[matchGUID]; ok {
		cp := *rec
		return &cp, nil
	}
	return nil, nil
}

func (s *MemoryStateStore) ListPendingDownloads(ctx context.Context) ([]*MatchRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var res []*MatchRecord
	for _, rec := range s.matches {
		if (rec.DownloadStatus == DownloadPending || rec.DownloadStatus == DownloadFailed) && rec.ReplayURL != "" {
			cp := *rec
			res = append(res, &cp)
		}
	}
	return res, nil
}

func (s *MemoryStateStore) ListPendingUploads(ctx context.Context) ([]*MatchRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var res []*MatchRecord
	for _, rec := range s.matches {
		if rec.DownloadStatus == DownloadDownloaded && rec.UploadStatus == UploadPending {
			cp := *rec
			res = append(res, &cp)
		}
	}
	return res, nil
}

func (s *MemoryStateStore) UpsertDiscoveredMatches(ctx context.Context, matches []*MatchRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for _, m := range matches {
		if existing, exists := s.matches[m.MatchGUID]; exists {
			// Do not regress status
			if existing.ReplayURL == "" && m.ReplayURL != "" {
				existing.ReplayURL = m.ReplayURL
			}
			existing.UpdatedAt = now
			continue
		}
		newRec := *m
		if newRec.CreatedAt.IsZero() {
			newRec.CreatedAt = now
		}
		newRec.UpdatedAt = now
		if newRec.DownloadStatus == "" {
			if newRec.ReplayURL == "" {
				newRec.DownloadStatus = DownloadSkipped
			} else {
				newRec.DownloadStatus = DownloadPending
			}
		}
		if newRec.UploadStatus == "" {
			newRec.UploadStatus = UploadPending
		}
		s.matches[m.MatchGUID] = &newRec
	}
	return nil
}

func (s *MemoryStateStore) MarkDownloading(ctx context.Context, matchGUID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec, exists := s.matches[matchGUID]; exists {
		rec.DownloadStatus = DownloadDownloading
		rec.UpdatedAt = time.Now()
		return nil
	}
	return fmt.Errorf("match not found: %s", matchGUID)
}

func (s *MemoryStateStore) MarkDownloaded(ctx context.Context, matchGUID, localPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec, exists := s.matches[matchGUID]; exists {
		now := time.Now()
		rec.DownloadStatus = DownloadDownloaded
		rec.LocalFilePath = localPath
		rec.DownloadedAt = &now
		rec.UpdatedAt = now
		return nil
	}
	return fmt.Errorf("match not found: %s", matchGUID)
}

func (s *MemoryStateStore) MarkDownloadFailed(ctx context.Context, matchGUID, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec, exists := s.matches[matchGUID]; exists {
		rec.DownloadStatus = DownloadFailed
		rec.LastError = errMsg
		rec.RetryCount++
		rec.UpdatedAt = time.Now()
		return nil
	}
	return fmt.Errorf("match not found: %s", matchGUID)
}

func (s *MemoryStateStore) MarkUploading(ctx context.Context, matchGUID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec, exists := s.matches[matchGUID]; exists {
		rec.UploadStatus = UploadUploading
		rec.UpdatedAt = time.Now()
		return nil
	}
	return fmt.Errorf("match not found: %s", matchGUID)
}

func (s *MemoryStateStore) MarkUploaded(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec, exists := s.matches[matchGUID]; exists {
		now := time.Now()
		rec.UploadStatus = UploadUploaded
		rec.BallchasingID = ballchasingID
		rec.BallchasingURL = ballchasingURL
		rec.UploadedAt = &now
		rec.UpdatedAt = now
		return nil
	}
	return fmt.Errorf("match not found: %s", matchGUID)
}

func (s *MemoryStateStore) MarkDuplicate(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec, exists := s.matches[matchGUID]; exists {
		now := time.Now()
		rec.UploadStatus = UploadDuplicate
		rec.BallchasingID = ballchasingID
		rec.BallchasingURL = ballchasingURL
		rec.UploadedAt = &now
		rec.UpdatedAt = now
		return nil
	}
	return fmt.Errorf("match not found: %s", matchGUID)
}

func (s *MemoryStateStore) MarkUploadFailed(ctx context.Context, matchGUID, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec, exists := s.matches[matchGUID]; exists {
		rec.UploadStatus = UploadFailed
		rec.LastError = errMsg
		rec.RetryCount++
		rec.UpdatedAt = time.Now()
		return nil
	}
	return fmt.Errorf("match not found: %s", matchGUID)
}

func (s *MemoryStateStore) RecoverInFlight(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for _, rec := range s.matches {
		if rec.DownloadStatus == DownloadDownloading {
			rec.DownloadStatus = DownloadPending
			rec.UpdatedAt = now
		}
		if rec.UploadStatus == UploadUploading {
			rec.UploadStatus = UploadPending
			rec.UpdatedAt = now
		}
	}
	return nil
}

func (s *MemoryStateStore) SaveAuthState(ctx context.Context, provider, refreshToken, accountID, displayName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authState[provider] = [3]string{refreshToken, accountID, displayName}
	return nil
}

func (s *MemoryStateStore) GetAuthState(ctx context.Context, provider string) (string, string, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if data, ok := s.authState[provider]; ok {
		return data[0], data[1], data[2], nil
	}
	return "", "", "", nil
}

// --- Concrete Clean Architecture Implementations for Pipeline Testing ---

// HTTPReplayDownloader implements ReplayDownloader with atomic .tmp file staging.
type HTTPReplayDownloader struct {
	client *http.Client
}

func NewHTTPReplayDownloader(timeout time.Duration) *HTTPReplayDownloader {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &HTTPReplayDownloader{
		client: &http.Client{Timeout: timeout},
	}
}

func (d *HTTPReplayDownloader) DownloadReplay(ctx context.Context, matchGUID, replayURL, destDir string) (string, error) {
	if replayURL == "" {
		return "", errors.New("empty replay URL")
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create destination dir: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, replayURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http get failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned non-200 status: %d", resp.StatusCode)
	}

	tmpPath := filepath.Join(destDir, fmt.Sprintf(".tmp-%s-%d.replay", matchGUID, time.Now().UnixNano()))
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}

	success := false
	defer func() {
		tmpFile.Close()
		if !success {
			_ = os.Remove(tmpPath)
		}
	}()

	written, err := io.Copy(tmpFile, resp.Body)
	if err != nil {
		return "", fmt.Errorf("streaming failed: %w", err)
	}

	if written < 1024 {
		return "", fmt.Errorf("downloaded replay is too small (%d bytes), minimum is 1024", written)
	}

	_ = tmpFile.Sync()
	_ = tmpFile.Close()

	finalPath := filepath.Join(destDir, fmt.Sprintf("%s.replay", matchGUID))
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return "", fmt.Errorf("atomic rename failed: %w", err)
	}

	success = true
	return finalPath, nil
}

// HTTPBallchasingUploader implements ReplayUploader with multipart POST, raw token, 201/409/429 handling.
type HTTPBallchasingUploader struct {
	client     *http.Client
	baseURL    string
	apiKey     string
	visibility string
	group      string
	maxRetries int
}

func NewHTTPBallchasingUploader(baseURL, apiKey, visibility, group string, maxRetries int) *HTTPBallchasingUploader {
	if baseURL == "" {
		baseURL = "https://ballchasing.com/api"
	}
	if visibility == "" {
		visibility = "public"
	}
	if maxRetries < 0 {
		maxRetries = 3
	}
	return &HTTPBallchasingUploader{
		client:     &http.Client{Timeout: 60 * time.Second},
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		apiKey:     apiKey,
		visibility: visibility,
		group:      group,
		maxRetries: maxRetries,
	}
}

func (u *HTTPBallchasingUploader) UploadReplay(ctx context.Context, matchGUID, filePath string) (*UploadResult, error) {
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read replay file: %w", err)
	}

	endpoint := fmt.Sprintf("%s/v2/upload?visibility=%s", u.baseURL, u.visibility)
	if u.group != "" {
		endpoint += "&group=" + u.group
	}

	for attempt := 0; attempt <= u.maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, err := writer.CreateFormFile("file", filepath.Base(filePath))
		if err != nil {
			return nil, err
		}
		if _, err := part.Write(fileData); err != nil {
			return nil, err
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", u.apiKey)
		req.Header.Set("Content-Type", writer.FormDataContentType())

		resp, err := u.client.Do(req)
		if err != nil {
			if attempt < u.maxRetries {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			return nil, err
		}

		respBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		switch resp.StatusCode {
		case http.StatusCreated:
			var res struct {
				ID       string `json:"id"`
				Location string `json:"location"`
			}
			_ = json.Unmarshal(respBytes, &res)
			return &UploadResult{
				ID:          res.ID,
				Location:    res.Location,
				IsDuplicate: false,
			}, nil

		case http.StatusConflict:
			var res struct {
				ID       string `json:"id"`
				Location string `json:"location"`
			}
			_ = json.Unmarshal(respBytes, &res)
			return &UploadResult{
				ID:          res.ID,
				Location:    res.Location,
				IsDuplicate: true,
			}, nil

		case http.StatusTooManyRequests:
			if attempt >= u.maxRetries {
				return nil, errors.New("rate limit retries exhausted")
			}
			retryWait := 100 * time.Millisecond
			retryAfterHeader := resp.Header.Get("Retry-After")
			if secs, err := strconv.Atoi(retryAfterHeader); err == nil && secs > 0 {
				retryWait = time.Duration(secs) * 100 * time.Millisecond // scaled for fast tests
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryWait):
			}
			continue

		case http.StatusUnauthorized:
			return nil, errors.New("permanent auth failure: HTTP 401 Unauthorized")

		case http.StatusBadRequest:
			return nil, fmt.Errorf("permanent client error: HTTP 400 Bad Request: %s", string(respBytes))

		default:
			if resp.StatusCode >= 500 && attempt < u.maxRetries {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			return nil, fmt.Errorf("unexpected http status %d: %s", resp.StatusCode, string(respBytes))
		}
	}

	return nil, errors.New("upload failed after max attempts")
}

// SyncerEngine executes the domain synchronization loop: poll -> diff -> download -> upload -> commit.
type SyncerEngine struct {
	store      StateStore
	provider   MatchHistoryProvider
	downloader ReplayDownloader
	uploader   ReplayUploader
	replayDir  string
	dryRun     bool
}

type SyncStats struct {
	DiscoveredCount int
	DownloadedCount int
	UploadedCount   int
	DuplicateCount  int
	SkippedCount    int
	FailedCount     int
}

func NewSyncerEngine(store StateStore, provider MatchHistoryProvider, downloader ReplayDownloader, uploader ReplayUploader, replayDir string, dryRun bool) *SyncerEngine {
	return &SyncerEngine{
		store:      store,
		provider:   provider,
		downloader: downloader,
		uploader:   uploader,
		replayDir:  replayDir,
		dryRun:     dryRun,
	}
}

func (s *SyncerEngine) RunCycle(ctx context.Context) (*SyncStats, error) {
	stats := &SyncStats{}

	// 1. Recover in-flight items from previous crashes
	if err := s.store.RecoverInFlight(ctx); err != nil {
		return nil, fmt.Errorf("recovery failed: %w", err)
	}

	// 2. Poll matches
	discovered, err := s.provider.GetRecentMatches(ctx)
	if err != nil {
		return nil, fmt.Errorf("polling matches failed: %w", err)
	}
	stats.DiscoveredCount = len(discovered)

	if s.dryRun {
		return stats, nil
	}

	// 3. Upsert into store
	var toUpsert []*MatchRecord
	for _, d := range discovered {
		if d.ReplayURL == "" {
			stats.SkippedCount++
		}
		rec := &MatchRecord{
			MatchGUID:            d.MatchGUID,
			RecordStartTimestamp: d.RecordStartTimestamp,
			MapName:              d.MapName,
			Playlist:             d.Playlist,
			ReplayURL:            d.ReplayURL,
		}
		toUpsert = append(toUpsert, rec)
	}
	if err := s.store.UpsertDiscoveredMatches(ctx, toUpsert); err != nil {
		return nil, fmt.Errorf("upsert failed: %w", err)
	}

	// 4. Download pending
	pendingDownloads, err := s.store.ListPendingDownloads(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pending downloads failed: %w", err)
	}

	for _, rec := range pendingDownloads {
		select {
		case <-ctx.Done():
			return stats, ctx.Err()
		default:
		}

		if rec.ReplayURL == "" {
			stats.SkippedCount++
			continue
		}

		if err := s.store.MarkDownloading(ctx, rec.MatchGUID); err != nil {
			continue
		}

		localPath, err := s.downloader.DownloadReplay(ctx, rec.MatchGUID, rec.ReplayURL, s.replayDir)
		if err != nil {
			if ctx.Err() != nil {
				return stats, ctx.Err()
			}
			_ = s.store.MarkDownloadFailed(ctx, rec.MatchGUID, err.Error())
			stats.FailedCount++
			continue
		}

		_ = s.store.MarkDownloaded(ctx, rec.MatchGUID, localPath)
		stats.DownloadedCount++
	}

	// 5. Upload pending
	pendingUploads, err := s.store.ListPendingUploads(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pending uploads failed: %w", err)
	}

	for _, rec := range pendingUploads {
		select {
		case <-ctx.Done():
			return stats, ctx.Err()
		default:
		}

		if err := s.store.MarkUploading(ctx, rec.MatchGUID); err != nil {
			continue
		}

		res, err := s.uploader.UploadReplay(ctx, rec.MatchGUID, rec.LocalFilePath)
		if err != nil {
			if ctx.Err() != nil {
				return stats, ctx.Err()
			}
			_ = s.store.MarkUploadFailed(ctx, rec.MatchGUID, err.Error())
			stats.FailedCount++
			continue
		}

		if res.IsDuplicate {
			_ = s.store.MarkDuplicate(ctx, rec.MatchGUID, res.ID, res.Location)
			stats.DuplicateCount++
		} else {
			_ = s.store.MarkUploaded(ctx, rec.MatchGUID, res.ID, res.Location)
			stats.UploadedCount++
		}
	}

	return stats, nil
}

// --- E2E Test Harness Context & Fixtures ---

type E2ETestHarness struct {
	T         *testing.T
	TempDir   string
	ReplayDir string
	Store     StateStore
	PsyNet    *testutil.MockPsyNetServer
	BC        *testutil.MockBallchasingServer
	CDN       *testutil.MockCDNServer
}

func SetupE2EHarness(t *testing.T) *E2ETestHarness {
	tempDir, err := os.MkdirTemp("", "rl-e2e-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	replayDir := filepath.Join(tempDir, "replays")
	_ = os.MkdirAll(replayDir, 0755)

	store := NewMemoryStateStore()
	psy := testutil.NewMockPsyNetServer()
	bc := testutil.NewMockBallchasingServer()
	cdn := testutil.NewMockCDNServer()

	h := &E2ETestHarness{
		T:         t,
		TempDir:   tempDir,
		ReplayDir: replayDir,
		Store:     store,
		PsyNet:    psy,
		BC:        bc,
		CDN:       cdn,
	}

	t.Cleanup(func() {
		h.Teardown()
	})

	return h
}

func (h *E2ETestHarness) Teardown() {
	if h.Store != nil {
		_ = h.Store.Close()
	}
	if h.PsyNet != nil {
		h.PsyNet.Close()
	}
	if h.BC != nil {
		h.BC.Close()
	}
	if h.CDN != nil {
		h.CDN.Close()
	}
	_ = os.RemoveAll(h.TempDir)
}

// Adapter converting testutil.MockMatchEntry to syncer.DiscoveredMatch
type MockProviderAdapter struct {
	psy *testutil.MockPsyNetServer
}

func NewMockProviderAdapter(psy *testutil.MockPsyNetServer) *MockProviderAdapter {
	return &MockProviderAdapter{psy: psy}
}

func (p *MockProviderAdapter) GetRecentMatches(ctx context.Context) ([]DiscoveredMatch, error) {
	entries := p.psy.GetMatches()
	var res []DiscoveredMatch
	for _, e := range entries {
		res = append(res, DiscoveredMatch{
			MatchGUID:            e.Match.MatchGUID,
			RecordStartTimestamp: e.Match.RecordStartTimestamp,
			MapName:              e.Match.MapName,
			Playlist:             e.Match.Playlist,
			ReplayURL:            e.ReplayURL,
		})
	}
	return res, nil
}

func (p *MockProviderAdapter) Close() error {
	return nil
}
