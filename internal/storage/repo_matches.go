package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const matchSelectCols = `match_guid, record_start_timestamp, map_name, playlist, replay_url,
       download_status, local_file_path, downloaded_at, upload_status,
       ballchasing_id, ballchasing_url, uploaded_at, retry_count,
       last_error, blue_score, orange_score, winner_team, replay_stats_json,
       synced_at, stats_sync_status, stats_retry_count, created_at, updated_at`

type scannable interface {
	Scan(dest ...interface{}) error
}

// GetMatch retrieves a single match record by GUID (falling back to case/hyphen-insensitive GUID or ballchasing_id).
func (s *SQLiteStore) GetMatch(ctx context.Context, matchGUID string) (*MatchRecord, error) {
	matchGUID = strings.TrimSpace(matchGUID)
	if matchGUID == "" {
		return nil, ErrInvalidGUID
	}

	query := fmt.Sprintf(`SELECT %s FROM matches WHERE match_guid = ?;`, matchSelectCols)
	rec, err := scanMatchRecord(s.db.QueryRowContext(ctx, query, matchGUID))
	if errors.Is(err, sql.ErrNoRows) {
		fbQuery := fmt.Sprintf(`
			SELECT %s FROM matches
			WHERE LOWER(REPLACE(match_guid, '-', '')) = LOWER(REPLACE(?, '-', '')) OR ballchasing_id = ?
			ORDER BY record_start_timestamp DESC LIMIT 1;`, matchSelectCols)
		fbRec, fbErr := scanMatchRecord(s.db.QueryRowContext(ctx, fbQuery, matchGUID, matchGUID))
		if errors.Is(fbErr, sql.ErrNoRows) {
			return nil, ErrMatchNotFound
		}
		if fbErr != nil {
			return nil, fmt.Errorf("failed to get match %s: %w", matchGUID, fbErr)
		}
		return fbRec, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get match %s: %w", matchGUID, err)
	}
	return rec, nil
}

// GetMatchByBallchasingID retrieves a match record by its Ballchasing replay ID.
func (s *SQLiteStore) GetMatchByBallchasingID(ctx context.Context, ballchasingID string) (*MatchRecord, error) {
	ballchasingID = strings.TrimSpace(ballchasingID)
	if ballchasingID == "" {
		return nil, ErrMatchNotFound
	}
	query := fmt.Sprintf(`SELECT %s FROM matches WHERE ballchasing_id = ? ORDER BY record_start_timestamp DESC LIMIT 1;`, matchSelectCols)
	rec, err := scanMatchRecord(s.db.QueryRowContext(ctx, query, ballchasingID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMatchNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get match by ballchasing_id %s: %w", ballchasingID, err)
	}
	return rec, nil
}

func (s *SQLiteStore) resolveExistingMatchGUID(ctx context.Context, guid, ballchasingID string) string {
	guid = strings.TrimSpace(guid)
	ballchasingID = strings.TrimSpace(ballchasingID)
	if guid != "" {
		var exact string
		if s.db.QueryRowContext(ctx, "SELECT match_guid FROM matches WHERE match_guid = ? LIMIT 1;", guid).Scan(&exact) == nil && exact != "" {
			return exact
		}
	}
	if ballchasingID != "" {
		var byBC string
		if s.db.QueryRowContext(ctx, "SELECT match_guid FROM matches WHERE ballchasing_id = ? ORDER BY record_start_timestamp DESC LIMIT 1;", ballchasingID).Scan(&byBC) == nil && byBC != "" {
			return byBC
		}
	}
	if guid != "" {
		var byNorm string
		if s.db.QueryRowContext(ctx, "SELECT match_guid FROM matches WHERE LOWER(REPLACE(match_guid, '-', '')) = LOWER(REPLACE(?, '-', '')) ORDER BY record_start_timestamp DESC LIMIT 1;", guid).Scan(&byNorm) == nil && byNorm != "" {
			return byNorm
		}
	}
	return guid
}

// ListPendingDownloads returns matches ready for download (PENDING, or FAILED with retry_count < maxRetries and backoff elapsed).
func (s *SQLiteStore) ListPendingDownloads(ctx context.Context) ([]*MatchRecord, error) {
	maxRetries, baseSec, maxSec := s.retryConfig(0)
	nowUnix := time.Now().UTC().Unix()
	query := fmt.Sprintf(`
	SELECT %s FROM matches
	WHERE replay_url != ''
	  AND (
	    download_status = 'PENDING'
	    OR (
	      download_status = 'FAILED'
	      AND retry_count < ?
	      AND (? - updated_at) >= MIN(?, ? * (1 << (CASE WHEN retry_count > 30 THEN 30 WHEN retry_count > 0 THEN retry_count - 1 ELSE 0 END)))
	    )
	  )
	ORDER BY record_start_timestamp ASC;`, matchSelectCols)
	rows, err := s.db.QueryContext(ctx, query, maxRetries, nowUnix, maxSec, baseSec)
	if err != nil {
		return nil, fmt.Errorf("failed to list pending downloads: %w", err)
	}
	defer rows.Close()
	return scanMatchRecords(rows)
}

// ListPendingUploads returns matches ready for upload (DOWNLOADED with PENDING/FAILED within retry budget).
func (s *SQLiteStore) ListPendingUploads(ctx context.Context) ([]*MatchRecord, error) {
	maxRetries, baseSec, maxSec := s.retryConfig(0)
	nowUnix := time.Now().UTC().Unix()
	query := fmt.Sprintf(`
	SELECT %s FROM matches
	WHERE download_status = 'DOWNLOADED' AND local_file_path != ''
	  AND (
	    upload_status = 'PENDING'
	    OR (
	      upload_status = 'FAILED'
	      AND retry_count < ?
	      AND (? - updated_at) >= MIN(?, ? * (1 << (CASE WHEN retry_count > 30 THEN 30 WHEN retry_count > 0 THEN retry_count - 1 ELSE 0 END)))
	    )
	  )
	ORDER BY record_start_timestamp ASC;`, matchSelectCols)
	rows, err := s.db.QueryContext(ctx, query, maxRetries, nowUnix, maxSec, baseSec)
	if err != nil {
		return nil, fmt.Errorf("failed to list pending uploads: %w", err)
	}
	defer rows.Close()
	return scanMatchRecords(rows)
}

// ListPendingStatsSync returns matches requiring post-upload Ballchasing stats synchronization.
func (s *SQLiteStore) ListPendingStatsSync(ctx context.Context, maxRetries int) ([]*MatchRecord, error) {
	effMaxRetries, baseSec, maxSec := s.retryConfig(maxRetries)
	nowUnix := time.Now().UTC().Unix()
	query := fmt.Sprintf(`
	SELECT %s FROM matches
	WHERE ballchasing_id != ''
	  AND (
	    stats_sync_status = 'PENDING'
	    OR (
	      stats_sync_status = 'FAILED'
	      AND stats_retry_count < ?
	      AND (? - updated_at) >= MIN(?, ? * (1 << (CASE WHEN stats_retry_count > 30 THEN 30 WHEN stats_retry_count > 0 THEN stats_retry_count - 1 ELSE 0 END)))
	    )
	  )
	ORDER BY record_start_timestamp ASC;`, matchSelectCols)
	rows, err := s.db.QueryContext(ctx, query, effMaxRetries, nowUnix, maxSec, baseSec)
	if err != nil {
		return nil, fmt.Errorf("failed to list pending stats sync: %w", err)
	}
	defer rows.Close()
	return scanMatchRecords(rows)
}

// UpsertDiscoveredMatches inserts newly discovered matches without clobbering existing download or upload progress.
func (s *SQLiteStore) UpsertDiscoveredMatches(ctx context.Context, matches []*MatchRecord) error {
	if len(matches) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin upsert tx: %w", err)
	}
	defer tx.Rollback()

	const upsertSQL = `
	INSERT INTO matches (
	    match_guid, record_start_timestamp, map_name, playlist, replay_url,
	    download_status, local_file_path, downloaded_at, upload_status,
	    ballchasing_id, ballchasing_url, uploaded_at, retry_count,
	    last_error, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(match_guid) DO UPDATE SET
	    download_status = CASE WHEN matches.download_status = 'SKIPPED' AND matches.replay_url = '' AND excluded.replay_url != '' THEN 'PENDING' ELSE matches.download_status END,
	    replay_url = CASE WHEN matches.replay_url = '' AND excluded.replay_url != '' THEN excluded.replay_url ELSE matches.replay_url END,
	    updated_at = excluded.updated_at;`
	stmt, err := tx.PrepareContext(ctx, upsertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare upsert stmt: %w", err)
	}
	defer stmt.Close()

	nowEpoch := time.Now().UTC().Unix()
	for _, m := range matches {
		if m == nil || m.MatchGUID == "" {
			continue
		}
		dlStatus := m.DownloadStatus
		if dlStatus == "" {
			if m.ReplayURL == "" {
				dlStatus = DownloadSkipped
			} else {
				dlStatus = DownloadPending
			}
		}
		upStatus := m.UploadStatus
		if upStatus == "" {
			upStatus = UploadPending
		}
		createdEpoch := m.CreatedAt.Unix()
		if createdEpoch <= 0 {
			createdEpoch = nowEpoch
		}
		updatedEpoch := m.UpdatedAt.Unix()
		if updatedEpoch <= 0 {
			updatedEpoch = nowEpoch
		}
		var dlAt, upAt interface{}
		if m.DownloadedAt != nil {
			dlAt = m.DownloadedAt.Unix()
		}
		if m.UploadedAt != nil {
			upAt = m.UploadedAt.Unix()
		}
		if _, err := stmt.ExecContext(ctx,
			m.MatchGUID, m.RecordStartTimestamp, m.MapName, m.Playlist, m.ReplayURL,
			string(dlStatus), m.LocalFilePath, dlAt, string(upStatus),
			m.BallchasingID, m.BallchasingURL, upAt, m.RetryCount, m.LastError,
			createdEpoch, updatedEpoch,
		); err != nil {
			return fmt.Errorf("failed to upsert match %s: %w", m.MatchGUID, err)
		}
	}
	return tx.Commit()
}

// MarkDownloading transitions download_status to DOWNLOADING.
func (s *SQLiteStore) MarkDownloading(ctx context.Context, matchGUID string) error {
	now := time.Now().UTC().Unix()
	res, err := s.db.ExecContext(ctx, `UPDATE matches SET download_status = ?, updated_at = ? WHERE match_guid = ?;`, string(DownloadDownloading), now, matchGUID)
	if err != nil {
		return fmt.Errorf("failed to mark downloading: %w", err)
	}
	return checkRowsAffected(res)
}

// MarkDownloaded transitions download_status to DOWNLOADED and resets retry_count.
func (s *SQLiteStore) MarkDownloaded(ctx context.Context, matchGUID, localPath string) error {
	now := time.Now().UTC().Unix()
	res, err := s.db.ExecContext(ctx, `UPDATE matches SET download_status = ?, local_file_path = ?, downloaded_at = ?, retry_count = 0, last_error = '', updated_at = ? WHERE match_guid = ?;`, string(DownloadDownloaded), localPath, now, now, matchGUID)
	if err != nil {
		return fmt.Errorf("failed to mark downloaded: %w", err)
	}
	return checkRowsAffected(res)
}

// MarkDownloadFailed transitions download_status to FAILED and increments retry_count.
func (s *SQLiteStore) MarkDownloadFailed(ctx context.Context, matchGUID, errMsg string) error {
	now := time.Now().UTC().Unix()
	res, err := s.db.ExecContext(ctx, `UPDATE matches SET download_status = ?, retry_count = retry_count + 1, last_error = ?, updated_at = ? WHERE match_guid = ?;`, string(DownloadFailed), errMsg, now, matchGUID)
	if err != nil {
		return fmt.Errorf("failed to mark download failed: %w", err)
	}
	return checkRowsAffected(res)
}

// MarkUploading transitions upload_status to UPLOADING.
func (s *SQLiteStore) MarkUploading(ctx context.Context, matchGUID string) error {
	now := time.Now().UTC().Unix()
	res, err := s.db.ExecContext(ctx, `UPDATE matches SET upload_status = ?, updated_at = ? WHERE match_guid = ?;`, string(UploadUploading), now, matchGUID)
	if err != nil {
		return fmt.Errorf("failed to mark uploading: %w", err)
	}
	return checkRowsAffected(res)
}

func (s *SQLiteStore) markUploadCompleted(ctx context.Context, status UploadStatus, matchGUID, ballchasingID, ballchasingURL string) error {
	now := time.Now().UTC().Unix()
	ballchasingURL = NormalizeBallchasingURL(ballchasingID, ballchasingURL)
	res, err := s.db.ExecContext(ctx,
		`UPDATE matches SET upload_status = ?, ballchasing_id = ?, ballchasing_url = ?, uploaded_at = ?, last_error = '', stats_sync_status = CASE WHEN replay_stats_json LIKE '%"boost":%' THEN stats_sync_status ELSE 'PENDING' END, stats_retry_count = 0, updated_at = ? WHERE match_guid = ?;`,
		string(status), ballchasingID, ballchasingURL, now, now, matchGUID,
	)
	if err != nil {
		return fmt.Errorf("failed to mark %s: %w", strings.ToLower(string(status)), err)
	}
	return checkRowsAffected(res)
}

// MarkUploaded transitions upload_status to UPLOADED, recording Ballchasing ID and URL.
func (s *SQLiteStore) MarkUploaded(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error {
	return s.markUploadCompleted(ctx, UploadUploaded, matchGUID, ballchasingID, ballchasingURL)
}

// MarkDuplicate transitions upload_status to DUPLICATE, recording Ballchasing ID and URL.
func (s *SQLiteStore) MarkDuplicate(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error {
	return s.markUploadCompleted(ctx, UploadDuplicate, matchGUID, ballchasingID, ballchasingURL)
}

// MarkUploadFailed transitions upload_status to FAILED and increments retry_count.
func (s *SQLiteStore) MarkUploadFailed(ctx context.Context, matchGUID, errMsg string) error {
	now := time.Now().UTC().Unix()
	res, err := s.db.ExecContext(ctx, `UPDATE matches SET upload_status = ?, retry_count = retry_count + 1, last_error = ?, updated_at = ? WHERE match_guid = ?;`, string(UploadFailed), errMsg, now, matchGUID)
	if err != nil {
		return fmt.Errorf("failed to mark upload failed: %w", err)
	}
	return checkRowsAffected(res)
}

// MarkStatsSyncing transitions a match's stats sync status to SYNCING.
func (s *SQLiteStore) MarkStatsSyncing(ctx context.Context, matchGUID string) error {
	if strings.TrimSpace(matchGUID) == "" {
		return ErrInvalidGUID
	}
	now := time.Now().UTC().Unix()
	res, err := s.db.ExecContext(ctx, `UPDATE matches SET stats_sync_status = ?, updated_at = ? WHERE match_guid = ?;`, string(StatsSyncSyncing), now, matchGUID)
	if err != nil {
		return fmt.Errorf("failed to mark stats syncing for %s: %w", matchGUID, err)
	}
	return checkRowsAffected(res)
}

// MarkStatsSynced transitions a match's stats sync status to SYNCED.
func (s *SQLiteStore) MarkStatsSynced(ctx context.Context, matchGUID string) error {
	if strings.TrimSpace(matchGUID) == "" {
		return ErrInvalidGUID
	}
	now := time.Now().UTC().Unix()
	res, err := s.db.ExecContext(ctx, `UPDATE matches SET stats_sync_status = ?, synced_at = ?, stats_retry_count = 0, updated_at = ? WHERE match_guid = ?;`, string(StatsSyncSynced), now, now, matchGUID)
	if err != nil {
		return fmt.Errorf("failed to mark stats synced for %s: %w", matchGUID, err)
	}
	return checkRowsAffected(res)
}

// MarkStatsSyncFailed increments stats_retry_count and sets stats_sync_status to FAILED.
func (s *SQLiteStore) MarkStatsSyncFailed(ctx context.Context, matchGUID, errMsg string) error {
	if strings.TrimSpace(matchGUID) == "" {
		return ErrInvalidGUID
	}
	now := time.Now().UTC().Unix()
	res, err := s.db.ExecContext(ctx, `UPDATE matches SET stats_sync_status = ?, stats_retry_count = stats_retry_count + 1, last_error = ?, updated_at = ? WHERE match_guid = ?;`, string(StatsSyncFailed), errMsg, now, matchGUID)
	if err != nil {
		return fmt.Errorf("failed to mark stats sync failed for %s: %w", matchGUID, err)
	}
	return checkRowsAffected(res)
}

// ResetMatchRetry resets error counters and resets failed download/upload/sync states to PENDING.
func (s *SQLiteStore) ResetMatchRetry(ctx context.Context, matchGUID string) error {
	if strings.TrimSpace(matchGUID) == "" {
		return ErrInvalidGUID
	}
	now := time.Now().UTC().Unix()
	const query = `
	UPDATE matches
	SET retry_count = 0, stats_retry_count = 0, last_error = '',
	    download_status = CASE WHEN download_status = 'FAILED' THEN 'PENDING' ELSE download_status END,
	    upload_status = CASE WHEN upload_status = 'FAILED' THEN 'PENDING' ELSE upload_status END,
	    stats_sync_status = CASE WHEN stats_sync_status = 'FAILED' OR stats_sync_status = 'SYNCING' THEN 'PENDING' ELSE stats_sync_status END,
	    updated_at = ?
	WHERE match_guid = ?;`
	res, err := s.db.ExecContext(ctx, query, now, matchGUID)
	if err != nil {
		return fmt.Errorf("failed to reset match retry for %s: %w", matchGUID, err)
	}
	return checkRowsAffected(res)
}

// PruneOldestMatches removes the oldest `count` match records ordered by record_start_timestamp ASC, created_at ASC.
func (s *SQLiteStore) PruneOldestMatches(ctx context.Context, count int) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, wrapContextErr(err)
	}
	if count <= 0 {
		return 0, nil
	}

	rows, err := s.db.QueryContext(ctx, `SELECT match_guid, local_file_path FROM matches ORDER BY record_start_timestamp ASC, created_at ASC, match_guid ASC LIMIT ?;`, count)
	if err != nil {
		return 0, fmt.Errorf("failed to query oldest matches for pruning: %w", err)
	}
	type pruneTarget struct{ guid, localPath string }
	var targets []pruneTarget
	for rows.Next() {
		var t pruneTarget
		var lp sql.NullString
		if rows.Scan(&t.guid, &lp) == nil {
			t.localPath = strings.TrimSpace(lp.String)
			targets = append(targets, t)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()
	if len(targets) == 0 {
		return 0, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	pruned := 0
	for _, t := range targets {
		_, _ = tx.ExecContext(ctx, `DELETE FROM match_players WHERE match_guid = ?;`, t.guid)
		_, _ = tx.ExecContext(ctx, `DELETE FROM processed_match_outcomes WHERE match_guid = ?;`, t.guid)
		if res, err := tx.ExecContext(ctx, `DELETE FROM matches WHERE match_guid = ?;`, t.guid); err == nil {
			if n, _ := res.RowsAffected(); n > 0 {
				pruned += int(n)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	for _, t := range targets {
		if t.localPath != "" {
			_ = os.Remove(t.localPath)
		}
	}
	return pruned, nil
}

func scanMatchRecord(s scannable) (*MatchRecord, error) {
	var (
		r               MatchRecord
		downloadedAt    sql.NullInt64
		uploadedAt      sql.NullInt64
		winnerTeam      sql.NullInt64
		syncedAt        sql.NullInt64
		blueScore       sql.NullInt64
		orangeScore     sql.NullInt64
		replayStatsJSON sql.NullString
		statsSyncStatus sql.NullString
		statsRetryCount sql.NullInt64
		createdAt       int64
		updatedAt       int64
	)
	err := s.Scan(
		&r.MatchGUID, &r.RecordStartTimestamp, &r.MapName, &r.Playlist, &r.ReplayURL,
		&r.DownloadStatus, &r.LocalFilePath, &downloadedAt, &r.UploadStatus,
		&r.BallchasingID, &r.BallchasingURL, &uploadedAt, &r.RetryCount,
		&r.LastError, &blueScore, &orangeScore, &winnerTeam, &replayStatsJSON,
		&syncedAt, &statsSyncStatus, &statsRetryCount, &createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}
	hydrateScannedMatchRecord(&r, downloadedAt, uploadedAt, winnerTeam, syncedAt, blueScore, orangeScore, replayStatsJSON, statsSyncStatus, statsRetryCount, createdAt, updatedAt)
	return &r, nil
}

func hydrateScannedMatchRecord(
	r *MatchRecord,
	downloadedAt, uploadedAt, winnerTeam, syncedAt, blueScore, orangeScore sql.NullInt64,
	replayStatsJSON, statsSyncStatus sql.NullString,
	statsRetryCount sql.NullInt64,
	createdAt, updatedAt int64,
) {
	if downloadedAt.Valid {
		t := time.Unix(downloadedAt.Int64, 0).UTC()
		r.DownloadedAt = &t
	}
	if uploadedAt.Valid {
		t := time.Unix(uploadedAt.Int64, 0).UTC()
		r.UploadedAt = &t
	}
	if blueScore.Valid {
		r.BlueScore = int(blueScore.Int64)
	}
	if orangeScore.Valid {
		r.OrangeScore = int(orangeScore.Int64)
	}
	if winnerTeam.Valid && winnerTeam.Int64 >= 0 {
		wt := int(winnerTeam.Int64)
		r.WinnerTeam = &wt
	}
	if replayStatsJSON.Valid {
		r.ReplayStatsJSON = replayStatsJSON.String
	}
	if statsSyncStatus.Valid && statsSyncStatus.String != "" {
		r.StatsSyncStatus = StatsSyncStatus(statsSyncStatus.String)
	} else {
		r.StatsSyncStatus = StatsSyncPending
	}
	if statsRetryCount.Valid {
		r.StatsRetryCount = int(statsRetryCount.Int64)
	}
	if syncedAt.Valid {
		t := time.Unix(syncedAt.Int64, 0).UTC()
		r.SyncedAt = &t
	}
	r.CreatedAt = time.Unix(createdAt, 0).UTC()
	r.UpdatedAt = time.Unix(updatedAt, 0).UTC()
}

func scanMatchRecords(rows *sql.Rows) ([]*MatchRecord, error) {
	records := make([]*MatchRecord, 0)
	for rows.Next() {
		rec, err := scanMatchRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan match row: %w", err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}
	return records, nil
}

func checkRowsAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrMatchNotFound
	}
	return nil
}
