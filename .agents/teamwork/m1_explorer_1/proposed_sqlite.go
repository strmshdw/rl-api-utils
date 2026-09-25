package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const schemaDDL = `
CREATE TABLE IF NOT EXISTS matches (
    match_guid TEXT PRIMARY KEY,
    record_start_timestamp INTEGER NOT NULL,
    map_name TEXT NOT NULL DEFAULT '',
    playlist INTEGER NOT NULL DEFAULT 0,
    replay_url TEXT NOT NULL DEFAULT '',
    download_status TEXT NOT NULL DEFAULT 'PENDING',
    local_file_path TEXT NOT NULL DEFAULT '',
    downloaded_at INTEGER,
    upload_status TEXT NOT NULL DEFAULT 'PENDING',
    ballchasing_id TEXT NOT NULL DEFAULT '',
    ballchasing_url TEXT NOT NULL DEFAULT '',
    uploaded_at INTEGER,
    retry_count INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_matches_download_status ON matches(download_status);
CREATE INDEX IF NOT EXISTS idx_matches_upload_status ON matches(upload_status);
CREATE INDEX IF NOT EXISTS idx_matches_record_ts ON matches(record_start_timestamp DESC);

CREATE TABLE IF NOT EXISTS auth_state (
    provider TEXT PRIMARY KEY,
    refresh_token TEXT NOT NULL,
    account_id TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL
);
`

// SQLiteStore implements StateStore backed by pure Go modernc.org/sqlite.
type SQLiteStore struct {
	db     *sql.DB
	dbPath string
}

// Ensure SQLiteStore implements StateStore interface.
var _ StateStore = (*SQLiteStore)(nil)

// NewSQLiteStore creates and initializes a SQLiteStore at the given database path.
// If dbPath is ":memory:", an in-memory database with shared cache is created.
// Parent directories are automatically created if they do not exist.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	connStr := dbPath
	isMemory := dbPath == ":memory:" || strings.HasPrefix(dbPath, "file::memory:")

	if !isMemory {
		dir := filepath.Dir(dbPath)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, fmt.Errorf("failed to create sqlite directory %s: %w", dir, err)
			}
		}
	} else if dbPath == ":memory:" {
		// Use shared in-memory URI so multiple connections access the same dataset
		connStr = "file::memory:?cache=shared"
	}

	db, err := sql.Open("sqlite", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// SQLite connection pooling: single connection or busy_timeout ensures serialized writes
	// without database-locked contention across goroutines.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Apply critical PRAGMA configurations
	pragmas := []string{
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA foreign_keys = ON;",
	}
	if !isMemory {
		pragmas = append([]string{"PRAGMA journal_mode = WAL;"}, pragmas...)
	}

	for _, p := range pragmas {
		if _, err := db.ExecContext(ctx, p); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to execute %q: %w", p, err)
		}
	}

	// Execute schema migrations
	if _, err := db.ExecContext(ctx, schemaDDL); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return &SQLiteStore{
		db:     db,
		dbPath: dbPath,
	}, nil
}

// GetMatch retrieves a single match record by GUID.
func (s *SQLiteStore) GetMatch(ctx context.Context, matchGUID string) (*MatchRecord, error) {
	if matchGUID == "" {
		return nil, ErrInvalidGUID
	}

	const query = `
	SELECT match_guid, record_start_timestamp, map_name, playlist, replay_url,
	       download_status, local_file_path, downloaded_at, upload_status,
	       ballchasing_id, ballchasing_url, uploaded_at, retry_count,
	       last_error, created_at, updated_at
	FROM matches
	WHERE match_guid = ?;
	`

	row := s.db.QueryRowContext(ctx, query, matchGUID)
	rec, err := scanMatchRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMatchNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get match %s: %w", matchGUID, err)
	}
	return rec, nil
}

// ListPendingDownloads returns all matches with download_status = 'PENDING' and non-empty replay_url.
func (s *SQLiteStore) ListPendingDownloads(ctx context.Context) ([]*MatchRecord, error) {
	const query = `
	SELECT match_guid, record_start_timestamp, map_name, playlist, replay_url,
	       download_status, local_file_path, downloaded_at, upload_status,
	       ballchasing_id, ballchasing_url, uploaded_at, retry_count,
	       last_error, created_at, updated_at
	FROM matches
	WHERE download_status = 'PENDING' AND replay_url != ''
	ORDER BY record_start_timestamp ASC;
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list pending downloads: %w", err)
	}
	defer rows.Close()

	return scanMatchRecords(rows)
}

// ListPendingUploads returns all matches with download_status = 'DOWNLOADED' and upload_status = 'PENDING'.
func (s *SQLiteStore) ListPendingUploads(ctx context.Context) ([]*MatchRecord, error) {
	const query = `
	SELECT match_guid, record_start_timestamp, map_name, playlist, replay_url,
	       download_status, local_file_path, downloaded_at, upload_status,
	       ballchasing_id, ballchasing_url, uploaded_at, retry_count,
	       last_error, created_at, updated_at
	FROM matches
	WHERE download_status = 'DOWNLOADED' AND upload_status = 'PENDING' AND local_file_path != ''
	ORDER BY record_start_timestamp ASC;
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list pending uploads: %w", err)
	}
	defer rows.Close()

	return scanMatchRecords(rows)
}

// UpsertDiscoveredMatches inserts newly discovered matches. If a match already exists,
// its download and upload progress are preserved (not clobbered).
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
	    replay_url = CASE WHEN matches.replay_url = '' AND excluded.replay_url != '' THEN excluded.replay_url ELSE matches.replay_url END,
	    updated_at = excluded.updated_at;
	`

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

		_, err = stmt.ExecContext(ctx,
			m.MatchGUID,
			m.RecordStartTimestamp,
			m.MapName,
			m.Playlist,
			m.ReplayURL,
			string(dlStatus),
			m.LocalFilePath,
			dlAt,
			string(upStatus),
			m.BallchasingID,
			m.BallchasingURL,
			upAt,
			m.RetryCount,
			m.LastError,
			createdEpoch,
			updatedEpoch,
		)
		if err != nil {
			return fmt.Errorf("failed to upsert match %s: %w", m.MatchGUID, err)
		}
	}

	return tx.Commit()
}

// MarkDownloading transitions download_status to DOWNLOADING.
func (s *SQLiteStore) MarkDownloading(ctx context.Context, matchGUID string) error {
	now := time.Now().UTC().Unix()
	res, err := s.db.ExecContext(ctx,
		`UPDATE matches SET download_status = ?, updated_at = ? WHERE match_guid = ?;`,
		string(DownloadDownloading), now, matchGUID,
	)
	if err != nil {
		return fmt.Errorf("failed to mark downloading: %w", err)
	}
	return checkRowsAffected(res)
}

// MarkDownloaded transitions download_status to DOWNLOADED, setting local_file_path and downloaded_at.
func (s *SQLiteStore) MarkDownloaded(ctx context.Context, matchGUID, localPath string) error {
	now := time.Now().UTC().Unix()
	res, err := s.db.ExecContext(ctx,
		`UPDATE matches SET download_status = ?, local_file_path = ?, downloaded_at = ?, last_error = '', updated_at = ? WHERE match_guid = ?;`,
		string(DownloadDownloaded), localPath, now, now, matchGUID,
	)
	if err != nil {
		return fmt.Errorf("failed to mark downloaded: %w", err)
	}
	return checkRowsAffected(res)
}

// MarkDownloadFailed transitions download_status to FAILED, increments retry_count, and records last_error.
func (s *SQLiteStore) MarkDownloadFailed(ctx context.Context, matchGUID, errMsg string) error {
	now := time.Now().UTC().Unix()
	res, err := s.db.ExecContext(ctx,
		`UPDATE matches SET download_status = ?, retry_count = retry_count + 1, last_error = ?, updated_at = ? WHERE match_guid = ?;`,
		string(DownloadFailed), errMsg, now, matchGUID,
	)
	if err != nil {
		return fmt.Errorf("failed to mark download failed: %w", err)
	}
	return checkRowsAffected(res)
}

// MarkUploading transitions upload_status to UPLOADING.
func (s *SQLiteStore) MarkUploading(ctx context.Context, matchGUID string) error {
	now := time.Now().UTC().Unix()
	res, err := s.db.ExecContext(ctx,
		`UPDATE matches SET upload_status = ?, updated_at = ? WHERE match_guid = ?;`,
		string(UploadUploading), now, matchGUID,
	)
	if err != nil {
		return fmt.Errorf("failed to mark uploading: %w", err)
	}
	return checkRowsAffected(res)
}

// MarkUploaded transitions upload_status to UPLOADED, recording Ballchasing ID and URL.
func (s *SQLiteStore) MarkUploaded(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error {
	now := time.Now().UTC().Unix()
	res, err := s.db.ExecContext(ctx,
		`UPDATE matches SET upload_status = ?, ballchasing_id = ?, ballchasing_url = ?, uploaded_at = ?, last_error = '', updated_at = ? WHERE match_guid = ?;`,
		string(UploadUploaded), ballchasingID, ballchasingURL, now, now, matchGUID,
	)
	if err != nil {
		return fmt.Errorf("failed to mark uploaded: %w", err)
	}
	return checkRowsAffected(res)
}

// MarkDuplicate transitions upload_status to DUPLICATE, recording Ballchasing ID and URL.
func (s *SQLiteStore) MarkDuplicate(ctx context.Context, matchGUID, ballchasingID, ballchasingURL string) error {
	now := time.Now().UTC().Unix()
	res, err := s.db.ExecContext(ctx,
		`UPDATE matches SET upload_status = ?, ballchasing_id = ?, ballchasing_url = ?, uploaded_at = ?, last_error = '', updated_at = ? WHERE match_guid = ?;`,
		string(UploadDuplicate), ballchasingID, ballchasingURL, now, now, matchGUID,
	)
	if err != nil {
		return fmt.Errorf("failed to mark duplicate: %w", err)
	}
	return checkRowsAffected(res)
}

// MarkUploadFailed transitions upload_status to FAILED, increments retry_count, and records last_error.
func (s *SQLiteStore) MarkUploadFailed(ctx context.Context, matchGUID, errMsg string) error {
	now := time.Now().UTC().Unix()
	res, err := s.db.ExecContext(ctx,
		`UPDATE matches SET upload_status = ?, retry_count = retry_count + 1, last_error = ?, updated_at = ? WHERE match_guid = ?;`,
		string(UploadFailed), errMsg, now, matchGUID,
	)
	if err != nil {
		return fmt.Errorf("failed to mark upload failed: %w", err)
	}
	return checkRowsAffected(res)
}

// RecoverInFlight resets DOWNLOADING -> PENDING and UPLOADING -> PENDING across an atomic transaction.
func (s *SQLiteStore) RecoverInFlight(ctx context.Context) error {
	now := time.Now().UTC().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin recovery tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		`UPDATE matches SET download_status = ?, updated_at = ? WHERE download_status = ?;`,
		string(DownloadPending), now, string(DownloadDownloading),
	)
	if err != nil {
		return fmt.Errorf("failed to reset in-flight downloads: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		`UPDATE matches SET upload_status = ?, updated_at = ? WHERE upload_status = ?;`,
		string(UploadPending), now, string(UploadUploading),
	)
	if err != nil {
		return fmt.Errorf("failed to reset in-flight uploads: %w", err)
	}

	return tx.Commit()
}

// SaveAuthState upserts authentication tokens and metadata for a provider.
func (s *SQLiteStore) SaveAuthState(ctx context.Context, provider, refreshToken, accountID, displayName string) error {
	now := time.Now().UTC().Unix()
	const query = `
	INSERT INTO auth_state (provider, refresh_token, account_id, display_name, updated_at)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(provider) DO UPDATE SET
	    refresh_token = excluded.refresh_token,
	    account_id = excluded.account_id,
	    display_name = excluded.display_name,
	    updated_at = excluded.updated_at;
	`
	_, err := s.db.ExecContext(ctx, query, provider, refreshToken, accountID, displayName, now)
	if err != nil {
		return fmt.Errorf("failed to save auth state for %s: %w", provider, err)
	}
	return nil
}

// GetAuthState retrieves persisted authentication metadata for a given provider.
func (s *SQLiteStore) GetAuthState(ctx context.Context, provider string) (refreshToken, accountID, displayName string, err error) {
	const query = `
	SELECT refresh_token, account_id, display_name
	FROM auth_state
	WHERE provider = ?;
	`
	err = s.db.QueryRowContext(ctx, query, provider).Scan(&refreshToken, &accountID, &displayName)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", "", ErrAuthStateNotFound
	}
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get auth state for %s: %w", provider, err)
	}
	return refreshToken, accountID, displayName, nil
}

// Close closes the SQLite database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// Row scanner abstraction for QueryRow and Rows.
type scannable interface {
	Scan(dest ...interface{}) error
}

func scanMatchRecord(s scannable) (*MatchRecord, error) {
	var (
		r            MatchRecord
		downloadedAt sql.NullInt64
		uploadedAt   sql.NullInt64
		createdAt    int64
		updatedAt    int64
	)

	err := s.Scan(
		&r.MatchGUID,
		&r.RecordStartTimestamp,
		&r.MapName,
		&r.Playlist,
		&r.ReplayURL,
		&r.DownloadStatus,
		&r.LocalFilePath,
		&downloadedAt,
		&r.UploadStatus,
		&r.BallchasingID,
		&r.BallchasingURL,
		&uploadedAt,
		&r.RetryCount,
		&r.LastError,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return nil, err
	}

	if downloadedAt.Valid {
		t := time.Unix(downloadedAt.Int64, 0).UTC()
		r.DownloadedAt = &t
	}
	if uploadedAt.Valid {
		t := time.Unix(uploadedAt.Int64, 0).UTC()
		r.UploadedAt = &t
	}
	r.CreatedAt = time.Unix(createdAt, 0).UTC()
	r.UpdatedAt = time.Unix(updatedAt, 0).UTC()

	return &r, nil
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
