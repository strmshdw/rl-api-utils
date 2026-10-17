package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestCalculateBackoff(t *testing.T) {
	base := 30 * time.Second
	max := 15 * time.Minute

	cases := []struct {
		name     string
		attempt  int
		base     time.Duration
		max      time.Duration
		expected time.Duration
	}{
		{
			name:     "attempt 0 returns 0",
			attempt:  0,
			base:     base,
			max:      max,
			expected: 0,
		},
		{
			name:     "negative attempt returns 0",
			attempt:  -1,
			base:     base,
			max:      max,
			expected: 0,
		},
		{
			name:     "zero base backoff returns 0",
			attempt:  1,
			base:     0,
			max:      max,
			expected: 0,
		},
		{
			name:     "attempt 1 equals base",
			attempt:  1,
			base:     base,
			max:      max,
			expected: 30 * time.Second,
		},
		{
			name:     "attempt 2 doubles base",
			attempt:  2,
			base:     base,
			max:      max,
			expected: 60 * time.Second,
		},
		{
			name:     "attempt 3 quadruples base",
			attempt:  3,
			base:     base,
			max:      max,
			expected: 120 * time.Second,
		},
		{
			name:     "attempt 4 octuples base",
			attempt:  4,
			base:     base,
			max:      max,
			expected: 240 * time.Second,
		},
		{
			name:     "attempt 6 capped at maxBackoff",
			attempt:  6,
			base:     base,
			max:      max,
			expected: 15 * time.Minute, // 30s * 32 = 960s = 16min > 15min cap
		},
		{
			name:     "huge attempt does not overflow or panic",
			attempt:  100,
			base:     base,
			max:      max,
			expected: 15 * time.Minute,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actual := CalculateBackoff(tc.attempt, tc.base, tc.max)
			if actual != tc.expected {
				t.Errorf("CalculateBackoff(%d, %v, %v) = %v, expected %v",
					tc.attempt, tc.base, tc.max, actual, tc.expected)
			}
		})
	}
}

func setupTestStores(t *testing.T) map[string]StateStore {
	tmpDir := t.TempDir()

	sqlStore, err := NewSQLiteStore(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}

	jsonStore, err := NewJSONStore(filepath.Join(tmpDir, "test.json"))
	if err != nil {
		t.Fatalf("failed to create json store: %v", err)
	}

	t.Cleanup(func() {
		sqlStore.Close()
		jsonStore.Close()
	})

	return map[string]StateStore{
		"SQLiteStore": sqlStore,
		"JSONStore":   jsonStore,
	}
}

func TestStore_PendingDownloads_RetryAndBackoff(t *testing.T) {
	for name, store := range setupTestStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			// Configure 2-second base backoff for rapid deterministic testing
			if s, ok := store.(*SQLiteStore); ok {
				s.SetRetryPolicy(3, 2*time.Second, 10*time.Second)
			} else if j, ok := store.(*JSONStore); ok {
				j.SetRetryPolicy(3, 2*time.Second, 10*time.Second)
			}

			m1 := &MatchRecord{
				MatchGUID:            "pending-001",
				RecordStartTimestamp: 1000,
				ReplayURL:            "https://cdn.psynet.com/001.replay",
				DownloadStatus:       DownloadPending,
			}
			m2 := &MatchRecord{
				MatchGUID:            "failed-retry1",
				RecordStartTimestamp: 1001,
				ReplayURL:            "https://cdn.psynet.com/002.replay",
				DownloadStatus:       DownloadDownloading,
			}
			m3 := &MatchRecord{
				MatchGUID:            "failed-exhausted",
				RecordStartTimestamp: 1002,
				ReplayURL:            "https://cdn.psynet.com/003.replay",
				DownloadStatus:       DownloadDownloading,
			}

			if err := store.UpsertDiscoveredMatches(ctx, []*MatchRecord{m1, m2, m3}); err != nil {
				t.Fatalf("upsert failed: %v", err)
			}

			// Fail m2 once (retry_count = 1, backoff = 2s)
			if err := store.MarkDownloadFailed(ctx, "failed-retry1", "network timeout"); err != nil {
				t.Fatalf("mark failed: %v", err)
			}

			// Fail m3 3 times (retry_count = 3 >= maxRetries)
			for i := 0; i < 3; i++ {
				if err := store.MarkDownloadFailed(ctx, "failed-exhausted", "terminal 404"); err != nil {
					t.Fatalf("mark failed: %v", err)
				}
			}

			// Immediately query pending downloads: m2 should be waiting on backoff (2s), m3 exhausted
			pending, err := store.ListPendingDownloads(ctx)
			if err != nil {
				t.Fatalf("list pending downloads failed: %v", err)
			}
			if len(pending) != 1 || pending[0].MatchGUID != "pending-001" {
				t.Fatalf("expected only pending-001 immediately, got %d results: %+v", len(pending), pending)
			}

			// Wait for 2.1s to allow backoff to elapse
			time.Sleep(2100 * time.Millisecond)

			// Query again: now m1 and m2 should both be returned
			pendingAfter, err := store.ListPendingDownloads(ctx)
			if err != nil {
				t.Fatalf("list pending downloads failed: %v", err)
			}
			if len(pendingAfter) != 2 {
				t.Fatalf("expected 2 matches after backoff elapsed, got %d", len(pendingAfter))
			}
			if pendingAfter[0].MatchGUID != "pending-001" || pendingAfter[1].MatchGUID != "failed-retry1" {
				t.Errorf("unexpected match ordering or contents: %+v", pendingAfter)
			}

			// Mark m2 downloaded: verify retry_count is reset to 0
			if err := store.MarkDownloaded(ctx, "failed-retry1", "d:/replays/002.replay"); err != nil {
				t.Fatalf("mark downloaded failed: %v", err)
			}
			rec, err := store.GetMatch(ctx, "failed-retry1")
			if err != nil {
				t.Fatalf("get match failed: %v", err)
			}
			if rec.RetryCount != 0 {
				t.Errorf("expected RetryCount to reset to 0 upon download, got %d", rec.RetryCount)
			}
			if rec.DownloadStatus != DownloadDownloaded {
				t.Errorf("expected DownloadStatus = DOWNLOADED, got %s", rec.DownloadStatus)
			}
		})
	}
}

func TestStore_PendingUploads_RetryAndBackoff(t *testing.T) {
	for name, store := range setupTestStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			if s, ok := store.(*SQLiteStore); ok {
				s.SetRetryPolicy(3, 2*time.Second, 10*time.Second)
			} else if j, ok := store.(*JSONStore); ok {
				j.SetRetryPolicy(3, 2*time.Second, 10*time.Second)
			}

			m1 := &MatchRecord{
				MatchGUID:            "upload-pending-001",
				RecordStartTimestamp: 2000,
				DownloadStatus:       DownloadDownloaded,
				LocalFilePath:        "d:/replays/001.replay",
				UploadStatus:         UploadPending,
			}
			m2 := &MatchRecord{
				MatchGUID:            "upload-failed-retry1",
				RecordStartTimestamp: 2001,
				DownloadStatus:       DownloadDownloaded,
				LocalFilePath:        "d:/replays/002.replay",
				UploadStatus:         UploadUploading,
			}
			m3 := &MatchRecord{
				MatchGUID:            "upload-exhausted",
				RecordStartTimestamp: 2002,
				DownloadStatus:       DownloadDownloaded,
				LocalFilePath:        "d:/replays/003.replay",
				UploadStatus:         UploadUploading,
			}

			if err := store.UpsertDiscoveredMatches(ctx, []*MatchRecord{m1, m2, m3}); err != nil {
				t.Fatalf("upsert failed: %v", err)
			}

			// Fail m2 once
			if err := store.MarkUploadFailed(ctx, "upload-failed-retry1", "rate limited"); err != nil {
				t.Fatalf("mark upload failed: %v", err)
			}

			// Fail m3 3 times
			for i := 0; i < 3; i++ {
				if err := store.MarkUploadFailed(ctx, "upload-exhausted", "unauthorized"); err != nil {
					t.Fatalf("mark upload failed: %v", err)
				}
			}

			// Immediately query: only upload-pending-001
			pending, err := store.ListPendingUploads(ctx)
			if err != nil {
				t.Fatalf("list pending uploads failed: %v", err)
			}
			if len(pending) != 1 || pending[0].MatchGUID != "upload-pending-001" {
				t.Fatalf("expected only upload-pending-001, got %d results: %+v", len(pending), pending)
			}

			// Wait for backoff
			time.Sleep(2100 * time.Millisecond)

			// Query after backoff: m1 and m2 returned, m3 excluded
			pendingAfter, err := store.ListPendingUploads(ctx)
			if err != nil {
				t.Fatalf("list pending uploads failed: %v", err)
			}
			if len(pendingAfter) != 2 {
				t.Fatalf("expected 2 matches after backoff, got %d", len(pendingAfter))
			}
			if pendingAfter[0].MatchGUID != "upload-pending-001" || pendingAfter[1].MatchGUID != "upload-failed-retry1" {
				t.Errorf("unexpected matches: %+v", pendingAfter)
			}
		})
	}
}

func TestStore_PendingStatsSync_DuplicateAndRetry(t *testing.T) {
	for name, store := range setupTestStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			if s, ok := store.(*SQLiteStore); ok {
				s.SetRetryPolicy(3, 2*time.Second, 10*time.Second)
			} else if j, ok := store.(*JSONStore); ok {
				j.SetRetryPolicy(3, 2*time.Second, 10*time.Second)
			}

			mUploaded := &MatchRecord{
				MatchGUID:            "sync-uploaded-001",
				RecordStartTimestamp: 3000,
				DownloadStatus:       DownloadDownloaded,
				UploadStatus:         UploadUploaded,
				BallchasingID:        "bc-001",
				StatsSyncStatus:      StatsSyncPending,
			}
			mDuplicate := &MatchRecord{
				MatchGUID:            "sync-duplicate-002",
				RecordStartTimestamp: 3001,
				DownloadStatus:       DownloadDownloaded,
				UploadStatus:         UploadDuplicate,
				BallchasingID:        "bc-002",
				StatsSyncStatus:      StatsSyncPending,
			}
			mFailed := &MatchRecord{
				MatchGUID:            "sync-failed-003",
				RecordStartTimestamp: 3002,
				DownloadStatus:       DownloadDownloaded,
				UploadStatus:         UploadUploaded,
				BallchasingID:        "bc-003",
				StatsSyncStatus:      StatsSyncSyncing,
			}

			if err := store.UpsertDiscoveredMatches(ctx, []*MatchRecord{mUploaded, mDuplicate, mFailed}); err != nil {
				t.Fatalf("upsert failed: %v", err)
			}

			// Fail mFailed once
			if err := store.MarkStatsSyncFailed(ctx, "sync-failed-003", "processing pending"); err != nil {
				t.Fatalf("mark stats sync failed: %v", err)
			}

			// Immediately query: DUPLICATE and UPLOADED pending matches must both be returned!
			pending, err := store.ListPendingStatsSync(ctx, 3)
			if err != nil {
				t.Fatalf("list pending stats sync failed: %v", err)
			}
			if len(pending) != 2 {
				t.Fatalf("expected 2 matches (uploaded + duplicate), got %d: %+v", len(pending), pending)
			}
			if pending[0].MatchGUID != "sync-uploaded-001" || pending[1].MatchGUID != "sync-duplicate-002" {
				t.Errorf("unexpected pending items: %+v", pending)
			}

			// Wait for backoff
			time.Sleep(2100 * time.Millisecond)

			// Query after backoff: all 3 should now be returned
			pendingAfter, err := store.ListPendingStatsSync(ctx, 3)
			if err != nil {
				t.Fatalf("list pending stats sync after backoff failed: %v", err)
			}
			if len(pendingAfter) != 3 {
				t.Fatalf("expected all 3 matches after backoff, got %d", len(pendingAfter))
			}
		})
	}
}

func TestStore_ResetMatchRetry_FullReset(t *testing.T) {
	for name, store := range setupTestStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			m := &MatchRecord{
				MatchGUID:            "reset-all-stages",
				RecordStartTimestamp: 4000,
				ReplayURL:            "https://cdn.psynet.com/reset.replay",
				DownloadStatus:       DownloadFailed,
				UploadStatus:         UploadFailed,
				StatsSyncStatus:      StatsSyncFailed,
				RetryCount:           3,
				StatsRetryCount:      3,
				LastError:            "fatal error",
			}

			if err := store.UpsertDiscoveredMatches(ctx, []*MatchRecord{m}); err != nil {
				t.Fatalf("upsert failed: %v", err)
			}

			if err := store.ResetMatchRetry(ctx, "reset-all-stages"); err != nil {
				t.Fatalf("reset match retry failed: %v", err)
			}

			rec, err := store.GetMatch(ctx, "reset-all-stages")
			if err != nil {
				t.Fatalf("get match failed: %v", err)
			}

			if rec.DownloadStatus != DownloadPending {
				t.Errorf("expected DownloadStatus = PENDING, got %s", rec.DownloadStatus)
			}
			if rec.UploadStatus != UploadPending {
				t.Errorf("expected UploadStatus = PENDING, got %s", rec.UploadStatus)
			}
			if rec.StatsSyncStatus != StatsSyncPending {
				t.Errorf("expected StatsSyncStatus = PENDING, got %s", rec.StatsSyncStatus)
			}
			if rec.RetryCount != 0 {
				t.Errorf("expected RetryCount = 0, got %d", rec.RetryCount)
			}
			if rec.StatsRetryCount != 0 {
				t.Errorf("expected StatsRetryCount = 0, got %d", rec.StatsRetryCount)
			}
			if rec.LastError != "" {
				t.Errorf("expected LastError = '', got %q", rec.LastError)
			}
		})
	}
}
