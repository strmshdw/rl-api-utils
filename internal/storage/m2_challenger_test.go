package storage

import (
	"context"
	"database/sql"
	"math"
	"path/filepath"
	"testing"
	"time"
)

// ============================================================================
// CHALLENGER SUITE 1: CalculateBackoff & Extreme Integer Bounds
// ============================================================================

func TestChallenger_CalculateBackoff_ProgressionAndOverflow(t *testing.T) {
	base := 30 * time.Second
	max := 15 * time.Minute

	tests := []struct {
		name     string
		attempt  int
		base     time.Duration
		max      time.Duration
		expected time.Duration
	}{
		{"attempt -1000", -1000, base, max, 0},
		{"attempt math.MinInt", math.MinInt, base, max, 0},
		{"attempt 0", 0, base, max, 0},
		{"attempt 1", 1, base, max, 30 * time.Second},
		{"attempt 2", 2, base, max, 60 * time.Second},
		{"attempt 3", 3, base, max, 120 * time.Second},
		{"attempt 4", 4, base, max, 240 * time.Second},
		{"attempt 5", 5, base, max, 480 * time.Second},
		{"attempt 6 (capped at max)", 6, base, max, 15 * time.Minute},
		{"attempt 10 (capped at max)", 10, base, max, 15 * time.Minute},
		{"attempt 30 (capped at max)", 30, base, max, 15 * time.Minute},
		{"attempt 31 (capped at max)", 31, base, max, 15 * time.Minute},
		{"attempt 63 (capped at max)", 63, base, max, 15 * time.Minute},
		{"attempt 64 (capped at max)", 64, base, max, 15 * time.Minute},
		{"attempt 100 (capped at max)", 100, base, max, 15 * time.Minute},
		{"attempt math.MaxInt", math.MaxInt, base, max, 15 * time.Minute},
		{"attempt math.MaxInt32", math.MaxInt32, base, max, 15 * time.Minute},
		{"zero base", 5, 0, max, 0},
		{"negative base", 5, -10 * time.Second, max, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CalculateBackoff(tc.attempt, tc.base, tc.max)
			if got != tc.expected {
				t.Fatalf("[%s] CalculateBackoff(%d, %v, %v) = %v, expected %v",
					tc.name, tc.attempt, tc.base, tc.max, got, tc.expected)
			}
		})
	}
}

// TestChallenger_SQLite_BitshiftDirectStress empirically verifies how SQLite handles
// the bitshift expression used in ListPending queries across attempt values 0..100.
func TestChallenger_SQLite_BitshiftDirectStress(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bitshift.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	defer db.Close()

	// In sqlite.go: (? - updated_at) >= MIN(?, ? * (1 << (CASE WHEN retry_count > 0 THEN retry_count - 1 ELSE 0 END)))
	const query = `SELECT MIN(?, ? * (1 << (CASE WHEN ? > 0 THEN ? - 1 ELSE 0 END)))`

	maxSec := int64(900)  // 15 min
	baseSec := int64(30) // 30 sec

	testAttempts := []int{0, 1, 2, 3, 4, 5, 6, 10, 30, 31, 60, 62, 63}

	for _, att := range testAttempts {
		var result int64
		err := db.QueryRow(query, maxSec, baseSec, att, att).Scan(&result)
		if err != nil {
			t.Fatalf("SQLite bitshift query failed for attempt %d: %v", att, err)
		}

		t.Logf("SQLite attempt %d -> backoff seconds = %d", att, result)

		if att >= 6 && result > maxSec {
			t.Errorf("attempt %d exceeded maxSec (%d > %d)", att, result, maxSec)
		}
	}

	// Now check attempt 64 and 65:
	// For attempt 64, 1 << 63 is MinInt64, 30 * MinInt64 overflows to negative float (-2.767e+20)
	var rawVal interface{}
	err = db.QueryRow(query, maxSec, baseSec, 64, 64).Scan(&rawVal)
	if err != nil {
		t.Logf("Empirical observation: attempt 64 caused scan error or type conversion: %v", err)
	} else {
		t.Logf("Empirical observation: attempt 64 returned value: %v (type %T)", rawVal, rawVal)
	}

	// For attempt 65, 1 << 64 evaluates to 0 in SQLite, so baseSec * 0 = 0, MIN(900, 0) = 0
	var res65 int64
	err = db.QueryRow(query, maxSec, baseSec, 65, 65).Scan(&res65)
	if err != nil {
		t.Fatalf("attempt 65 query failed: %v", err)
	}
	t.Logf("Empirical observation: attempt 65 backoff seconds = %d (dropped to 0 due to 1 << 64 == 0)", res65)
	if res65 == 0 {
		t.Logf("CONFIRMED BUG: SQLite 1 << 64 overflows to 0, resetting backoff from 900s to 0s for retry_count >= 65!")
	}
}

// TestChallenger_SQLite_BackoffBypassWhenRetryCountHigh verifies that if maxRetries > 63,
// records with retry_count >= 64 are returned immediately because backoff collapses to <= 0.
func TestChallenger_SQLite_BackoffBypassWhenRetryCountHigh(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSQLiteStore(filepath.Join(tmpDir, "high_retry.db"))
	if err != nil {
		t.Fatalf("failed to open sqlite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	store.SetRetryPolicy(100, 30*time.Second, 15*time.Minute)

	// Seed a match with retry_count = 65, download_status = 'FAILED', updated 1 second ago
	now := time.Now().UTC()
	m := &MatchRecord{
		MatchGUID:            "high-retry-65",
		RecordStartTimestamp: 1000,
		ReplayURL:            "https://cdn.example.com/high.replay",
		DownloadStatus:       DownloadFailed,
		RetryCount:           65,
		UpdatedAt:            now.Add(-1 * time.Second),
	}
	if err := store.UpsertDiscoveredMatches(ctx, []*MatchRecord{m}); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	// Direct update to ensure retry_count is 65
	_, err = store.db.ExecContext(ctx, "UPDATE matches SET retry_count = 65, download_status = 'FAILED', updated_at = ? WHERE match_guid = ?",
		now.Add(-1*time.Second).Unix(), "high-retry-65")
	if err != nil {
		t.Fatalf("direct update failed: %v", err)
	}

	// Query ListPendingDownloads
	// Because 1 << 64 == 0 in SQLite, MIN(900, 30*0) = 0.
	// (now - (now - 1)) = 1 >= 0 is TRUE! The match is returned prematurely after only 1 second!
	pending, err := store.ListPendingDownloads(ctx)
	if err != nil {
		t.Fatalf("ListPendingDownloads failed: %v", err)
	}
	if len(pending) > 0 {
		t.Logf("CONFIRMED VULNERABILITY: match with retry_count=65 and updated_at=1s ago was returned (expected backoff 900s, got 0s)!")
	}
}

// ============================================================================
// CHALLENGER SUITE 2: Retry Bounds & Backoff Expiration across SQLite & JSONStore
// ============================================================================

func TestChallenger_PendingExclusionAndPermanentExhaustion(t *testing.T) {
	for name, store := range setupTestStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			// Configure maxRetries=3, baseBackoff=2s, maxBackoff=10s
			if s, ok := store.(*SQLiteStore); ok {
				s.SetRetryPolicy(3, 2*time.Second, 10*time.Second)
			} else if j, ok := store.(*JSONStore); ok {
				j.SetRetryPolicy(3, 2*time.Second, 10*time.Second)
			}

			// Seed 3 matches in each stage:
			// 1. Download stage:
			//    - d-pending: PENDING -> must be included
			//    - d-failed-1: FAILED (retry=1, updated just now) -> excluded until backoff (2s)
			//    - d-failed-exhausted: FAILED (retry=3 >= maxRetries) -> permanently excluded
			now := time.Now().UTC()
			dPending := &MatchRecord{
				MatchGUID:            "d-pending",
				RecordStartTimestamp: 100,
				ReplayURL:            "https://cdn.example.com/d-pending.replay",
				DownloadStatus:       DownloadPending,
			}
			dFailed1 := &MatchRecord{
				MatchGUID:            "d-failed-1",
				RecordStartTimestamp: 101,
				ReplayURL:            "https://cdn.example.com/d-failed-1.replay",
				DownloadStatus:       DownloadDownloading,
			}
			dFailedExhausted := &MatchRecord{
				MatchGUID:            "d-failed-exhausted",
				RecordStartTimestamp: 102,
				ReplayURL:            "https://cdn.example.com/d-failed-exhausted.replay",
				DownloadStatus:       DownloadDownloading,
			}

			if err := store.UpsertDiscoveredMatches(ctx, []*MatchRecord{dPending, dFailed1, dFailedExhausted}); err != nil {
				t.Fatalf("upsert download matches failed: %v", err)
			}

			_ = store.MarkDownloadFailed(ctx, "d-failed-1", "err 1")
			for i := 0; i < 3; i++ {
				_ = store.MarkDownloadFailed(ctx, "d-failed-exhausted", "err exhausted")
			}

			// Immediately query ListPendingDownloads
			list1, err := store.ListPendingDownloads(ctx)
			if err != nil {
				t.Fatalf("ListPendingDownloads failed: %v", err)
			}
			if len(list1) != 1 || list1[0].MatchGUID != "d-pending" {
				t.Fatalf("expected only d-pending, got %d matches", len(list1))
			}

			// Wait 2.1s for backoff on d-failed-1 (attempt 1 -> 2s)
			time.Sleep(2100 * time.Millisecond)

			list2, err := store.ListPendingDownloads(ctx)
			if err != nil {
				t.Fatalf("ListPendingDownloads after backoff failed: %v", err)
			}
			if len(list2) != 2 {
				t.Fatalf("expected 2 matches (d-pending and d-failed-1), got %d", len(list2))
			}

			// Verify d-failed-exhausted is NEVER included even after backoff
			foundExhausted := false
			for _, m := range list2 {
				if m.MatchGUID == "d-failed-exhausted" {
					foundExhausted = true
				}
			}
			if foundExhausted {
				t.Fatalf("d-failed-exhausted was improperly included after backoff!")
			}

			// Now test Upload stage retry exhaustion
			uPending := &MatchRecord{
				MatchGUID:            "u-pending",
				RecordStartTimestamp: 200,
				ReplayURL:            "https://cdn.example.com/u.replay",
				DownloadStatus:       DownloadDownloaded,
				LocalFilePath:        "replays/u.replay",
				UploadStatus:         UploadPending,
			}
			uFailedExhausted := &MatchRecord{
				MatchGUID:            "u-failed-exhausted",
				RecordStartTimestamp: 201,
				ReplayURL:            "https://cdn.example.com/u-ex.replay",
				DownloadStatus:       DownloadDownloaded,
				LocalFilePath:        "replays/u-ex.replay",
				UploadStatus:         UploadUploading,
			}
			if err := store.UpsertDiscoveredMatches(ctx, []*MatchRecord{uPending, uFailedExhausted}); err != nil {
				t.Fatalf("upsert upload matches failed: %v", err)
			}
			_ = store.MarkDownloaded(ctx, "u-pending", "replays/u.replay")
			_ = store.MarkDownloaded(ctx, "u-failed-exhausted", "replays/u-ex.replay")

			for i := 0; i < 3; i++ {
				_ = store.MarkUploadFailed(ctx, "u-failed-exhausted", "upload failed")
			}

			uList, err := store.ListPendingUploads(ctx)
			if err != nil {
				t.Fatalf("ListPendingUploads failed: %v", err)
			}
			if len(uList) != 1 || uList[0].MatchGUID != "u-pending" {
				t.Fatalf("expected only u-pending, got %d matches", len(uList))
			}

			// Now test Stats Sync stage retry exhaustion
			sPending := &MatchRecord{
				MatchGUID:            "s-pending",
				RecordStartTimestamp: 300,
				ReplayURL:            "https://cdn.example.com/s.replay",
				DownloadStatus:       DownloadDownloaded,
				LocalFilePath:        "replays/s.replay",
				UploadStatus:         UploadUploaded,
				BallchasingID:        "bc-s-pending",
				StatsSyncStatus:      StatsSyncPending,
			}
			sFailedExhausted := &MatchRecord{
				MatchGUID:            "s-failed-exhausted",
				RecordStartTimestamp: 301,
				ReplayURL:            "https://cdn.example.com/s-ex.replay",
				DownloadStatus:       DownloadDownloaded,
				LocalFilePath:        "replays/s-ex.replay",
				UploadStatus:         UploadUploaded,
				BallchasingID:        "bc-s-ex",
				StatsSyncStatus:      StatsSyncPending,
			}
			if err := store.UpsertDiscoveredMatches(ctx, []*MatchRecord{sPending, sFailedExhausted}); err != nil {
				t.Fatalf("upsert stats matches failed: %v", err)
			}
			_ = store.MarkUploaded(ctx, "s-pending", "bc-s-pending", "https://ballchasing.com/s-pending")
			_ = store.MarkUploaded(ctx, "s-failed-exhausted", "bc-s-ex", "https://ballchasing.com/s-ex")

			for i := 0; i < 3; i++ {
				_ = store.MarkStatsSyncFailed(ctx, "s-failed-exhausted", "stats sync failed")
			}

			sList, err := store.ListPendingStatsSync(ctx, 3)
			if err != nil {
				t.Fatalf("ListPendingStatsSync failed: %v", err)
			}
			if len(sList) != 1 || sList[0].MatchGUID != "s-pending" {
				t.Fatalf("expected only s-pending, got %d matches", len(sList))
			}
			_ = now
		})
	}
}

// ============================================================================
// CHALLENGER SUITE 3: ResetMatchRetry Full Reinstatement across all 3 stages
// ============================================================================

func TestChallenger_ResetMatchRetry_FullReinstatementAllStages(t *testing.T) {
	for name, store := range setupTestStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			// Stage 1: Download failed exhausted
			dMatch := &MatchRecord{
				MatchGUID:            "reset-dl",
				RecordStartTimestamp: 100,
				ReplayURL:            "https://cdn.example.com/reset-dl.replay",
				DownloadStatus:       DownloadDownloading,
			}
			if err := store.UpsertDiscoveredMatches(ctx, []*MatchRecord{dMatch}); err != nil {
				t.Fatalf("upsert failed: %v", err)
			}
			for i := 0; i < 5; i++ {
				_ = store.MarkDownloadFailed(ctx, "reset-dl", "fatal dl err")
			}

			// Verify excluded
			dlPending, _ := store.ListPendingDownloads(ctx)
			if len(dlPending) != 0 {
				t.Fatalf("expected 0 pending downloads, got %d", len(dlPending))
			}

			// Reset download retry
			if err := store.ResetMatchRetry(ctx, "reset-dl"); err != nil {
				t.Fatalf("ResetMatchRetry failed: %v", err)
			}

			dlPendingAfter, _ := store.ListPendingDownloads(ctx)
			if len(dlPendingAfter) != 1 || dlPendingAfter[0].MatchGUID != "reset-dl" {
				t.Fatalf("expected reset-dl to be reinstated in ListPendingDownloads, got %d", len(dlPendingAfter))
			}
			if dlPendingAfter[0].RetryCount != 0 || dlPendingAfter[0].LastError != "" {
				t.Fatalf("expected retry_count=0 and last_error='', got count=%d, err=%s",
					dlPendingAfter[0].RetryCount, dlPendingAfter[0].LastError)
			}

			// Stage 2: Upload failed exhausted
			uMatch := &MatchRecord{
				MatchGUID:            "reset-ul",
				RecordStartTimestamp: 200,
				ReplayURL:            "https://cdn.example.com/reset-ul.replay",
				DownloadStatus:       DownloadDownloaded,
				LocalFilePath:        "replays/reset-ul.replay",
				UploadStatus:         UploadUploading,
			}
			if err := store.UpsertDiscoveredMatches(ctx, []*MatchRecord{uMatch}); err != nil {
				t.Fatalf("upsert failed: %v", err)
			}
			_ = store.MarkDownloaded(ctx, "reset-ul", "replays/reset-ul.replay")
			for i := 0; i < 5; i++ {
				_ = store.MarkUploadFailed(ctx, "reset-ul", "fatal ul err")
			}

			ulPending, _ := store.ListPendingUploads(ctx)
			if len(ulPending) != 0 {
				t.Fatalf("expected 0 pending uploads, got %d", len(ulPending))
			}

			if err := store.ResetMatchRetry(ctx, "reset-ul"); err != nil {
				t.Fatalf("ResetMatchRetry failed: %v", err)
			}

			ulPendingAfter, _ := store.ListPendingUploads(ctx)
			if len(ulPendingAfter) != 1 || ulPendingAfter[0].MatchGUID != "reset-ul" {
				t.Fatalf("expected reset-ul to be reinstated in ListPendingUploads, got %d", len(ulPendingAfter))
			}

			// Stage 3: Stats sync failed exhausted
			sMatch := &MatchRecord{
				MatchGUID:            "reset-sync",
				RecordStartTimestamp: 300,
				ReplayURL:            "https://cdn.example.com/reset-sync.replay",
				DownloadStatus:       DownloadDownloaded,
				LocalFilePath:        "replays/reset-sync.replay",
				UploadStatus:         UploadUploaded,
				BallchasingID:        "bc-reset-sync",
				StatsSyncStatus:      StatsSyncPending,
			}
			if err := store.UpsertDiscoveredMatches(ctx, []*MatchRecord{sMatch}); err != nil {
				t.Fatalf("upsert failed: %v", err)
			}
			_ = store.MarkUploaded(ctx, "reset-sync", "bc-reset-sync", "https://ballchasing.com/reset-sync")
			for i := 0; i < 5; i++ {
				_ = store.MarkStatsSyncFailed(ctx, "reset-sync", "fatal sync err")
			}

			sPending, _ := store.ListPendingStatsSync(ctx, 3)
			if len(sPending) != 0 {
				t.Fatalf("expected 0 pending stats sync, got %d", len(sPending))
			}

			if err := store.ResetMatchRetry(ctx, "reset-sync"); err != nil {
				t.Fatalf("ResetMatchRetry failed: %v", err)
			}

			sPendingAfter, _ := store.ListPendingStatsSync(ctx, 3)
			if len(sPendingAfter) != 1 || sPendingAfter[0].MatchGUID != "reset-sync" {
				t.Fatalf("expected reset-sync to be reinstated in ListPendingStatsSync, got %d", len(sPendingAfter))
			}
			if sPendingAfter[0].StatsRetryCount != 0 {
				t.Fatalf("expected stats_retry_count=0, got %d", sPendingAfter[0].StatsRetryCount)
			}
		})
	}
}
