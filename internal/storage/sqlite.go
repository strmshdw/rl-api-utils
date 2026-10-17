package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
    blue_score INTEGER NOT NULL DEFAULT 0,
    orange_score INTEGER NOT NULL DEFAULT 0,
    winner_team INTEGER NOT NULL DEFAULT -1,
    replay_stats_json TEXT NOT NULL DEFAULT '',
    synced_at INTEGER,
    stats_sync_status TEXT NOT NULL DEFAULT 'PENDING',
    stats_retry_count INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_matches_download_status ON matches(download_status);
CREATE INDEX IF NOT EXISTS idx_matches_upload_status ON matches(upload_status);
CREATE INDEX IF NOT EXISTS idx_matches_record_ts ON matches(record_start_timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_matches_playlist ON matches(playlist);
CREATE INDEX IF NOT EXISTS idx_matches_stats_sync ON matches(stats_sync_status);

CREATE TABLE IF NOT EXISTS auth_state (
    provider TEXT PRIMARY KEY,
    refresh_token TEXT NOT NULL,
    account_id TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS players (
    player_id TEXT PRIMARY KEY,
    platform TEXT NOT NULL DEFAULT '',
    player_name TEXT NOT NULL DEFAULT '',
    player_name_lower TEXT NOT NULL DEFAULT '',
    ranks_json TEXT NOT NULL DEFAULT '{}',
    notes TEXT NOT NULL DEFAULT '',
    tags TEXT NOT NULL DEFAULT '',
    first_seen_at INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_players_last_seen ON players(last_seen_at DESC);
CREATE INDEX IF NOT EXISTS idx_players_last_seen_id ON players(last_seen_at DESC, player_id ASC);
CREATE INDEX IF NOT EXISTS idx_players_name ON players(player_name);
CREATE INDEX IF NOT EXISTS idx_players_name_lower ON players(player_name_lower);
CREATE INDEX IF NOT EXISTS idx_players_platform ON players(platform);

CREATE TABLE IF NOT EXISTS player_matchups (
    player_id TEXT NOT NULL,
    playlist_id INTEGER NOT NULL,
    wins_as_teammate INTEGER NOT NULL DEFAULT 0,
    losses_as_teammate INTEGER NOT NULL DEFAULT 0,
    wins_as_opponent INTEGER NOT NULL DEFAULT 0,
    losses_as_opponent INTEGER NOT NULL DEFAULT 0,
    total_matches INTEGER NOT NULL DEFAULT 0,
    last_played_at INTEGER NOT NULL,
    PRIMARY KEY (player_id, playlist_id),
    FOREIGN KEY (player_id) REFERENCES players(player_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_player_matchups_player ON player_matchups(player_id);

CREATE TABLE IF NOT EXISTS processed_match_outcomes (
    match_guid TEXT PRIMARY KEY,
    playlist_id INTEGER NOT NULL DEFAULT 0,
    processed_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_processed_matches_ts ON processed_match_outcomes(processed_at DESC);

CREATE TABLE IF NOT EXISTS match_players (
    match_guid TEXT NOT NULL,
    player_id TEXT NOT NULL,
    team_num INTEGER NOT NULL,
    is_local INTEGER NOT NULL DEFAULT 0,
    won INTEGER,
    score INTEGER NOT NULL DEFAULT 0,
    goals INTEGER NOT NULL DEFAULT 0,
    assists INTEGER NOT NULL DEFAULT 0,
    saves INTEGER NOT NULL DEFAULT 0,
    shots INTEGER NOT NULL DEFAULT 0,
    demos INTEGER NOT NULL DEFAULT 0,
    car_name TEXT NOT NULL DEFAULT '',
    stats_json TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (match_guid, player_id)
);

CREATE INDEX IF NOT EXISTS idx_match_players_player ON match_players(player_id);
CREATE INDEX IF NOT EXISTS idx_match_players_player_won ON match_players(player_id, won);

CREATE TABLE IF NOT EXISTS mmr_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    recorded_at INTEGER NOT NULL,
    match_guid TEXT NOT NULL DEFAULT '',
    playlist_id INTEGER NOT NULL,
    mmr INTEGER NOT NULL,
    mmr_change INTEGER NOT NULL DEFAULT 0,
    tier INTEGER NOT NULL DEFAULT 0,
    division INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_mmr_history_playlist ON mmr_history(playlist_id, recorded_at DESC);
CREATE INDEX IF NOT EXISTS idx_mmr_history_recorded ON mmr_history(recorded_at DESC);
CREATE INDEX IF NOT EXISTS idx_mmr_history_match ON mmr_history(match_guid);
`

// SQLiteStore implements StateStore backed by pure Go modernc.org/sqlite.
type SQLiteStore struct {
	db          *sql.DB
	dbPath      string
	maxRetries  int
	baseBackoff time.Duration
	maxBackoff  time.Duration
}

// Ensure SQLiteStore implements StateStore interface.
var _ StateStore = (*SQLiteStore)(nil)

// SetRetryPolicy configures the maximum retries and backoff limits for pending queries.
func (s *SQLiteStore) SetRetryPolicy(maxRetries int, baseBackoff, maxBackoff time.Duration) {
	if maxRetries > 0 {
		s.maxRetries = maxRetries
	}
	s.baseBackoff = baseBackoff
	s.maxBackoff = maxBackoff
}

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

	// SQLite connection pooling: single connection ensures serialized writes
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

	// Run schema migrations for existing tables before schemaDDL
	if err := migrateMatchesTable(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate matches schema: %w", err)
	}
	if err := migratePlayersTable(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate schema: %w", err)
	}

	// Execute schema migrations
	if _, err := db.ExecContext(ctx, schemaDDL); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Run startup backfill migration for match_players
	if err := migrateMatchPlayers(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to backfill match_players: %w", err)
	}

	return &SQLiteStore{
		db:          db,
		dbPath:      dbPath,
		maxRetries:  DefaultMaxRetries,
		baseBackoff: DefaultBaseBackoff,
		maxBackoff:  DefaultMaxBackoff,
	}, nil
}

// migratePlayersTable ensures the players table has player_name_lower, notes, and tags columns and backfills existing rows.
func migratePlayersTable(ctx context.Context, db *sql.DB) error {
	var tableCount int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='players';").Scan(&tableCount)
	if err != nil {
		return fmt.Errorf("failed to check players table existence: %w", err)
	}
	if tableCount == 0 {
		return nil
	}

	rows, err := db.QueryContext(ctx, "PRAGMA table_info(players);")
	if err != nil {
		return fmt.Errorf("failed to read table_info: %w", err)
	}
	defer rows.Close()

	cols := make(map[string]bool)
	for rows.Next() {
		var cid, notnull, pk int
		var name, colType string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("failed to scan table_info row: %w", err)
		}
		cols[strings.ToLower(name)] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("table_info iteration error: %w", err)
	}

	if !cols["player_name_lower"] {
		alterSQL := `ALTER TABLE players ADD COLUMN player_name_lower TEXT NOT NULL DEFAULT '';`
		if _, err := db.ExecContext(ctx, alterSQL); err != nil {
			return fmt.Errorf("failed to alter table players: %w", err)
		}

		selectSQL := `SELECT player_id, player_name FROM players WHERE player_name != '';`
		pRows, err := db.QueryContext(ctx, selectSQL)
		if err != nil {
			return fmt.Errorf("failed to select players for backfill: %w", err)
		}
		type playerEntry struct {
			id   string
			name string
		}
		var toUpdate []playerEntry
		for pRows.Next() {
			var pe playerEntry
			if err := pRows.Scan(&pe.id, &pe.name); err != nil {
				pRows.Close()
				return fmt.Errorf("failed to scan player for backfill: %w", err)
			}
			toUpdate = append(toUpdate, pe)
		}
		pRows.Close()

		if len(toUpdate) > 0 {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				return fmt.Errorf("failed to begin backfill tx: %w", err)
			}
			defer tx.Rollback()

			stmt, err := tx.PrepareContext(ctx, "UPDATE players SET player_name_lower = ? WHERE player_id = ?;")
			if err != nil {
				return fmt.Errorf("failed to prepare backfill stmt: %w", err)
			}
			defer stmt.Close()

			for _, pe := range toUpdate {
				if _, err := stmt.ExecContext(ctx, strings.ToLower(pe.name), pe.id); err != nil {
					return fmt.Errorf("failed to backfill player %s: %w", pe.id, err)
				}
			}

			if err := tx.Commit(); err != nil {
				return fmt.Errorf("failed to commit backfill tx: %w", err)
			}
		}
	}

	if !cols["notes"] {
		if _, err := db.ExecContext(ctx, `ALTER TABLE players ADD COLUMN notes TEXT NOT NULL DEFAULT '';`); err != nil {
			return fmt.Errorf("failed to add notes column to players: %w", err)
		}
	}

	if !cols["tags"] {
		if _, err := db.ExecContext(ctx, `ALTER TABLE players ADD COLUMN tags TEXT NOT NULL DEFAULT '';`); err != nil {
			return fmt.Errorf("failed to add tags column to players: %w", err)
		}
	}

	return nil
}

// migrateMatchesTable ensures the matches table has end of game replay stats and sync status columns.
func migrateMatchesTable(ctx context.Context, db *sql.DB) error {
	var tableCount int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='matches';").Scan(&tableCount)
	if err != nil {
		return fmt.Errorf("failed to check matches table existence: %w", err)
	}
	if tableCount == 0 {
		return nil
	}

	rows, err := db.QueryContext(ctx, "PRAGMA table_info(matches);")
	if err != nil {
		return fmt.Errorf("failed to read table_info: %w", err)
	}
	defer rows.Close()

	cols := make(map[string]bool)
	for rows.Next() {
		var cid, notnull, pk int
		var name, colType string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("failed to scan table_info row: %w", err)
		}
		cols[strings.ToLower(name)] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("table_info iteration error: %w", err)
	}

	alterStmts := []struct {
		col string
		sql string
	}{
		{"blue_score", "ALTER TABLE matches ADD COLUMN blue_score INTEGER NOT NULL DEFAULT 0;"},
		{"orange_score", "ALTER TABLE matches ADD COLUMN orange_score INTEGER NOT NULL DEFAULT 0;"},
		{"winner_team", "ALTER TABLE matches ADD COLUMN winner_team INTEGER NOT NULL DEFAULT -1;"},
		{"replay_stats_json", "ALTER TABLE matches ADD COLUMN replay_stats_json TEXT NOT NULL DEFAULT '';"},
		{"synced_at", "ALTER TABLE matches ADD COLUMN synced_at INTEGER;"},
		{"stats_sync_status", "ALTER TABLE matches ADD COLUMN stats_sync_status TEXT NOT NULL DEFAULT 'PENDING';"},
		{"stats_retry_count", "ALTER TABLE matches ADD COLUMN stats_retry_count INTEGER NOT NULL DEFAULT 0;"},
	}

	for _, stmt := range alterStmts {
		if !cols[stmt.col] {
			if _, err := db.ExecContext(ctx, stmt.sql); err != nil {
				return fmt.Errorf("failed to add column %s to matches: %w", stmt.col, err)
			}
		}
	}
	return nil
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
	       last_error, blue_score, orange_score, winner_team, replay_stats_json,
	       synced_at, stats_sync_status, stats_retry_count, created_at, updated_at
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

// ListPendingDownloads returns all matches ready for download (PENDING, or FAILED with retry_count < maxRetries and backoff elapsed).
func (s *SQLiteStore) ListPendingDownloads(ctx context.Context) ([]*MatchRecord, error) {
	maxRetries := s.maxRetries
	if maxRetries <= 0 {
		maxRetries = DefaultMaxRetries
	}
	baseSec := int64(s.baseBackoff / time.Second)
	if s.baseBackoff > 0 && baseSec == 0 {
		baseSec = 1
	}
	maxSec := int64(s.maxBackoff / time.Second)
	if maxSec <= 0 {
		maxSec = 86400
	}
	nowUnix := time.Now().UTC().Unix()

	const query = `
	SELECT match_guid, record_start_timestamp, map_name, playlist, replay_url,
	       download_status, local_file_path, downloaded_at, upload_status,
	       ballchasing_id, ballchasing_url, uploaded_at, retry_count,
	       last_error, blue_score, orange_score, winner_team, replay_stats_json,
	       synced_at, stats_sync_status, stats_retry_count, created_at, updated_at
	FROM matches
	WHERE replay_url != ''
	  AND (
	    download_status = 'PENDING'
	    OR (
	      download_status = 'FAILED'
	      AND retry_count < ?
	      AND (? - updated_at) >= MIN(?, ? * (1 << (CASE WHEN retry_count > 0 THEN retry_count - 1 ELSE 0 END)))
	    )
	  )
	ORDER BY record_start_timestamp ASC;
	`

	rows, err := s.db.QueryContext(ctx, query, maxRetries, nowUnix, maxSec, baseSec)
	if err != nil {
		return nil, fmt.Errorf("failed to list pending downloads: %w", err)
	}
	defer rows.Close()

	return scanMatchRecords(rows)
}

// ListPendingUploads returns all matches ready for upload (DOWNLOADED with PENDING, or FAILED with retry_count < maxRetries and backoff elapsed).
func (s *SQLiteStore) ListPendingUploads(ctx context.Context) ([]*MatchRecord, error) {
	maxRetries := s.maxRetries
	if maxRetries <= 0 {
		maxRetries = DefaultMaxRetries
	}
	baseSec := int64(s.baseBackoff / time.Second)
	if s.baseBackoff > 0 && baseSec == 0 {
		baseSec = 1
	}
	maxSec := int64(s.maxBackoff / time.Second)
	if maxSec <= 0 {
		maxSec = 86400
	}
	nowUnix := time.Now().UTC().Unix()

	const query = `
	SELECT match_guid, record_start_timestamp, map_name, playlist, replay_url,
	       download_status, local_file_path, downloaded_at, upload_status,
	       ballchasing_id, ballchasing_url, uploaded_at, retry_count,
	       last_error, blue_score, orange_score, winner_team, replay_stats_json,
	       synced_at, stats_sync_status, stats_retry_count, created_at, updated_at
	FROM matches
	WHERE download_status = 'DOWNLOADED' AND local_file_path != ''
	  AND (
	    upload_status = 'PENDING'
	    OR (
	      upload_status = 'FAILED'
	      AND retry_count < ?
	      AND (? - updated_at) >= MIN(?, ? * (1 << (CASE WHEN retry_count > 0 THEN retry_count - 1 ELSE 0 END)))
	    )
	  )
	ORDER BY record_start_timestamp ASC;
	`

	rows, err := s.db.QueryContext(ctx, query, maxRetries, nowUnix, maxSec, baseSec)
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
	    download_status = CASE WHEN matches.download_status = 'SKIPPED' AND matches.replay_url = '' AND excluded.replay_url != '' THEN 'PENDING' ELSE matches.download_status END,
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

// MarkDownloaded transitions download_status to DOWNLOADED, setting local_file_path and downloaded_at,
// and resets retry_count = 0 so that download retries do not starve upload retries.
func (s *SQLiteStore) MarkDownloaded(ctx context.Context, matchGUID, localPath string) error {
	now := time.Now().UTC().Unix()
	res, err := s.db.ExecContext(ctx,
		`UPDATE matches SET download_status = ?, local_file_path = ?, downloaded_at = ?, retry_count = 0, last_error = '', updated_at = ? WHERE match_guid = ?;`,
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

	_, err = tx.ExecContext(ctx,
		`UPDATE matches SET stats_sync_status = ?, updated_at = ? WHERE stats_sync_status = ?;`,
		string(StatsSyncPending), now, string(StatsSyncSyncing),
	)
	if err != nil {
		return fmt.Errorf("failed to reset in-flight stats sync: %w", err)
	}

	return tx.Commit()
}

// SaveAuthState upserts authentication tokens and metadata for a provider.
func (s *SQLiteStore) SaveAuthState(ctx context.Context, provider, refreshToken, accountID, displayName string) error {
	if provider == "" {
		return errors.New("provider cannot be empty")
	}

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
	if provider == "" {
		return "", "", "", errors.New("provider cannot be empty")
	}

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
		&blueScore,
		&orangeScore,
		&winnerTeam,
		&replayStatsJSON,
		&syncedAt,
		&statsSyncStatus,
		&statsRetryCount,
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
	if syncedAt.Valid {
		t := time.Unix(syncedAt.Int64, 0).UTC()
		r.SyncedAt = &t
	}
	if statsSyncStatus.Valid && statsSyncStatus.String != "" {
		r.StatsSyncStatus = StatsSyncStatus(statsSyncStatus.String)
	} else {
		r.StatsSyncStatus = StatsSyncPending
	}
	if statsRetryCount.Valid {
		r.StatsRetryCount = int(statsRetryCount.Int64)
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

func serializeTags(tags []string) string {
	if len(tags) == 0 {
		return "[]"
	}
	clean := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t != "" {
			clean = append(clean, t)
		}
	}
	if len(clean) == 0 {
		return "[]"
	}
	data, err := json.Marshal(clean)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func parseTags(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return []string{}
	}
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err == nil && tags != nil {
		return tags
	}
	parts := strings.Split(raw, ",")
	tags = make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			tags = append(tags, p)
		}
	}
	return tags
}

// UpsertPlayer inserts a new player or updates an existing player's platform, name, notes, tags, and last_seen_at.
// Preserves first_seen_at and existing ranks_json on update unless provided. Monotonically updates timestamps.
func (s *SQLiteStore) UpsertPlayer(ctx context.Context, player *PlayerRecord) error {
	if err := ctx.Err(); err != nil {
		return wrapContextErr(err)
	}
	if player == nil {
		return errors.New("player cannot be nil")
	}
	if strings.TrimSpace(player.PlayerID) == "" {
		return errors.New("player_id cannot be empty")
	}

	now := time.Now().UTC()
	firstSeenEpoch := player.FirstSeenAt.Unix()
	lastSeenEpoch := player.LastSeenAt.Unix()
	if firstSeenEpoch <= 0 && lastSeenEpoch <= 0 {
		firstSeenEpoch = now.Unix()
		lastSeenEpoch = now.Unix()
	} else if firstSeenEpoch <= 0 {
		firstSeenEpoch = lastSeenEpoch
	} else if lastSeenEpoch <= 0 {
		lastSeenEpoch = firstSeenEpoch
	}
	if firstSeenEpoch > lastSeenEpoch {
		firstSeenEpoch = lastSeenEpoch
	}

	ranksJSON := player.RanksJSON
	if strings.TrimSpace(ranksJSON) == "" {
		ranksJSON = "{}"
	}

	playerNameLower := strings.ToLower(player.PlayerName)
	tagsJSON := serializeTags(player.Tags)

	const query = `
	INSERT INTO players (player_id, platform, player_name, player_name_lower, ranks_json, notes, tags, first_seen_at, last_seen_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(player_id) DO UPDATE SET
		platform = CASE WHEN excluded.platform != '' THEN excluded.platform ELSE players.platform END,
		player_name = CASE WHEN excluded.player_name != '' THEN excluded.player_name ELSE players.player_name END,
		player_name_lower = CASE WHEN excluded.player_name != '' THEN excluded.player_name_lower ELSE players.player_name_lower END,
		ranks_json = CASE WHEN excluded.ranks_json != '' AND excluded.ranks_json != '{}' THEN excluded.ranks_json ELSE players.ranks_json END,
		notes = CASE WHEN excluded.notes != '' THEN excluded.notes ELSE players.notes END,
		tags = CASE WHEN excluded.tags != '' AND excluded.tags != '[]' THEN excluded.tags ELSE players.tags END,
		first_seen_at = MIN(players.first_seen_at, excluded.first_seen_at),
		last_seen_at = MAX(players.last_seen_at, excluded.last_seen_at);
	`

	_, err := s.db.ExecContext(ctx, query,
		player.PlayerID,
		player.Platform,
		player.PlayerName,
		playerNameLower,
		ranksJSON,
		player.Notes,
		tagsJSON,
		firstSeenEpoch,
		lastSeenEpoch,
	)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return wrapContextErr(ctxErr)
		}
		return fmt.Errorf("failed to upsert player %s: %w", player.PlayerID, err)
	}
	return nil
}

// GetPlayer retrieves a single player profile by PlayerID. Returns ErrPlayerNotFound if not found.
func (s *SQLiteStore) GetPlayer(ctx context.Context, playerID string) (*PlayerRecord, error) {
	if strings.TrimSpace(playerID) == "" {
		return nil, ErrPlayerNotFound
	}

	const query = `
	SELECT player_id, platform, player_name, ranks_json, notes, tags, first_seen_at, last_seen_at
	FROM players
	WHERE player_id = ?;
	`

	var (
		p         PlayerRecord
		notes     sql.NullString
		tagsStr   sql.NullString
		firstSeen int64
		lastSeen  int64
	)

	err := s.db.QueryRowContext(ctx, query, playerID).Scan(
		&p.PlayerID,
		&p.Platform,
		&p.PlayerName,
		&p.RanksJSON,
		&notes,
		&tagsStr,
		&firstSeen,
		&lastSeen,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPlayerNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get player %s: %w", playerID, err)
	}

	p.Notes = notes.String
	p.Tags = parseTags(tagsStr.String)
	p.FirstSeenAt = time.Unix(firstSeen, 0).UTC()
	p.LastSeenAt = time.Unix(lastSeen, 0).UTC()
	return &p, nil
}

// ListPlayers returns paginated players ordered by last_seen_at DESC, player_id ASC.
func (s *SQLiteStore) ListPlayers(ctx context.Context, limit, offset int) ([]*PlayerRecord, error) {
	if limit == 0 {
		return []*PlayerRecord{}, nil
	}
	if limit < 0 {
		limit = -1
	}
	if offset < 0 {
		offset = 0
	}

	const query = `
	SELECT player_id, platform, player_name, ranks_json, notes, tags, first_seen_at, last_seen_at
	FROM players
	ORDER BY last_seen_at DESC, player_id ASC
	LIMIT ? OFFSET ?;
	`

	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list players: %w", err)
	}
	defer rows.Close()

	records := make([]*PlayerRecord, 0)
	for rows.Next() {
		var (
			p         PlayerRecord
			notes     sql.NullString
			tagsStr   sql.NullString
			firstSeen int64
			lastSeen  int64
		)
		if err := rows.Scan(
			&p.PlayerID,
			&p.Platform,
			&p.PlayerName,
			&p.RanksJSON,
			&notes,
			&tagsStr,
			&firstSeen,
			&lastSeen,
		); err != nil {
			return nil, fmt.Errorf("failed to scan player row: %w", err)
		}
		p.Notes = notes.String
		p.Tags = parseTags(tagsStr.String)
		p.FirstSeenAt = time.Unix(firstSeen, 0).UTC()
		p.LastSeenAt = time.Unix(lastSeen, 0).UTC()
		records = append(records, &p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error in list players: %w", err)
	}

	return records, nil
}

// ListPlayerSummaries returns paginated players with aggregate win/loss statistics across all playlists.
func (s *SQLiteStore) ListPlayerSummaries(ctx context.Context, limit, offset int) ([]*PlayerSummary, error) {
	if limit == 0 {
		return []*PlayerSummary{}, nil
	}
	if limit < 0 {
		limit = -1
	}
	if offset < 0 {
		offset = 0
	}

	const query = `
	SELECT p.player_id, p.platform, p.player_name, p.ranks_json, p.notes, p.tags, p.first_seen_at, p.last_seen_at,
	       COALESCE(SUM(m.wins_as_teammate), 0) AS total_wins_teammate,
	       COALESCE(SUM(m.losses_as_teammate), 0) AS total_losses_teammate,
	       COALESCE(SUM(m.wins_as_opponent), 0) AS total_wins_opponent,
	       COALESCE(SUM(m.losses_as_opponent), 0) AS total_losses_opponent,
	       COALESCE(SUM(m.total_matches), 0) AS total_matches
	FROM (
		SELECT player_id, platform, player_name, ranks_json, notes, tags, first_seen_at, last_seen_at
		FROM players p
		ORDER BY p.last_seen_at DESC, p.player_id ASC
		LIMIT ? OFFSET ?
	) p
	LEFT JOIN player_matchups m ON p.player_id = m.player_id
	GROUP BY p.player_id
	ORDER BY p.last_seen_at DESC, p.player_id ASC;
	`

	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list player summaries: %w", err)
	}
	defer rows.Close()

	summaries := make([]*PlayerSummary, 0)
	for rows.Next() {
		var (
			sRec      PlayerSummary
			notes     sql.NullString
			tagsStr   sql.NullString
			firstSeen int64
			lastSeen  int64
		)
		if err := rows.Scan(
			&sRec.PlayerID,
			&sRec.Platform,
			&sRec.PlayerName,
			&sRec.RanksJSON,
			&notes,
			&tagsStr,
			&firstSeen,
			&lastSeen,
			&sRec.TotalWinsAsTeammate,
			&sRec.TotalLossesAsTeammate,
			&sRec.TotalWinsAsOpponent,
			&sRec.TotalLossesAsOpponent,
			&sRec.TotalMatches,
		); err != nil {
			return nil, fmt.Errorf("failed to scan player summary row: %w", err)
		}
		sRec.Notes = notes.String
		sRec.Tags = parseTags(tagsStr.String)
		sRec.FirstSeenAt = time.Unix(firstSeen, 0).UTC()
		sRec.LastSeenAt = time.Unix(lastSeen, 0).UTC()
		summaries = append(summaries, &sRec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error in list player summaries: %w", err)
	}

	return summaries, nil
}

// escapeLike escapes SQL LIKE wildcard characters '%', '_', and the escape character '\'
// to ensure literal substring matching identical to strings.Contains.
func escapeLike(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '%', '_', '\\':
			b.WriteRune('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// FilterPlayerSummaries searches, filters, and sorts players across the entire dataset.
func (s *SQLiteStore) FilterPlayerSummaries(ctx context.Context, filter PlayerDirectoryFilter) ([]*PlayerSummary, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, wrapContextErr(err)
	}

	cleanQuery := strings.TrimSpace(filter.Query)
	cleanPlatform := strings.TrimSpace(strings.ToLower(filter.Platform))
	cleanRole := strings.TrimSpace(strings.ToLower(filter.Role))
	cleanSort := strings.TrimSpace(strings.ToLower(filter.Sort))
	isAsc := strings.TrimSpace(strings.ToLower(filter.Order)) == "asc"

	var pWhereClauses []string
	var pWhereArgs []interface{}

	if cleanPlatform != "" && cleanPlatform != "all" {
		pWhereClauses = append(pWhereClauses, "LOWER(p.platform) = ?")
		pWhereArgs = append(pWhereArgs, cleanPlatform)
	}

	if cleanQuery != "" {
		lowerQuery := strings.ToLower(cleanQuery)
		if strings.ContainsRune(lowerQuery, '\x00') {
			pWhereClauses = append(pWhereClauses, "(INSTR(p.player_name_lower, ?) > 0 OR INSTR(LOWER(p.player_id), ?) > 0)")
			pWhereArgs = append(pWhereArgs, lowerQuery, lowerQuery)
		} else {
			pWhereClauses = append(pWhereClauses, "(p.player_name_lower LIKE ? ESCAPE '\\' OR LOWER(p.player_id) LIKE ? ESCAPE '\\')")
			escaped := "%" + escapeLike(lowerQuery) + "%"
			pWhereArgs = append(pWhereArgs, escaped, escaped)
		}
	}

	pWhereSQL := ""
	if len(pWhereClauses) > 0 {
		pWhereSQL = " WHERE " + strings.Join(pWhereClauses, " AND ")
	}

	var roleWhereClauses []string
	switch cleanRole {
	case "frequent":
		roleWhereClauses = append(roleWhereClauses, "total_matches >= 3")
	case "teammate":
		roleWhereClauses = append(roleWhereClauses, "(total_wins_teammate + total_losses_teammate) > 0")
	case "opponent":
		roleWhereClauses = append(roleWhereClauses, "(total_wins_opponent + total_losses_opponent) > 0")
	case "synergy":
		roleWhereClauses = append(roleWhereClauses, "(total_wins_teammate + total_losses_teammate) >= 2 AND (1.0 * total_wins_teammate / (total_wins_teammate + total_losses_teammate)) >= 0.60")
	}

	roleWhereSQL := ""
	if len(roleWhereClauses) > 0 {
		roleWhereSQL = " WHERE " + strings.Join(roleWhereClauses, " AND ")
	}

	countSQL := fmt.Sprintf(`
	WITH player_agg AS (
		SELECT p.player_id, p.platform, p.player_name, p.player_name_lower, p.ranks_json, p.notes, p.tags, p.first_seen_at, p.last_seen_at,
		       COALESCE(SUM(m.wins_as_teammate), 0) AS total_wins_teammate,
		       COALESCE(SUM(m.losses_as_teammate), 0) AS total_losses_teammate,
		       COALESCE(SUM(m.wins_as_opponent), 0) AS total_wins_opponent,
		       COALESCE(SUM(m.losses_as_opponent), 0) AS total_losses_opponent,
		       COALESCE(SUM(m.total_matches), 0) AS total_matches
		FROM players p
		LEFT JOIN player_matchups m ON p.player_id = m.player_id
		%s
		GROUP BY p.player_id
	)
	SELECT COUNT(*) FROM player_agg%s;
	`, pWhereSQL, roleWhereSQL)

	var total int
	if err := s.db.QueryRowContext(ctx, countSQL, pWhereArgs...).Scan(&total); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, 0, wrapContextErr(ctxErr)
		}
		return nil, 0, fmt.Errorf("failed to count matching players: %w", err)
	}

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

	limitArg := filter.Limit
	if limitArg < 0 {
		limitArg = -1
	}

	var orderClause string
	switch cleanSort {
	case "match_count", "matches":
		if isAsc {
			orderClause = "total_matches ASC, last_seen_at ASC, player_id ASC"
		} else {
			orderClause = "total_matches DESC, last_seen_at DESC, player_id ASC"
		}
	case "win_rate":
		if isAsc {
			orderClause = "COALESCE((1.0 * total_wins_teammate / NULLIF(total_wins_teammate + total_losses_teammate, 0)), 0) ASC, total_matches ASC, last_seen_at ASC, player_id ASC"
		} else {
			orderClause = "COALESCE((1.0 * total_wins_teammate / NULLIF(total_wins_teammate + total_losses_teammate, 0)), 0) DESC, total_matches DESC, last_seen_at DESC, player_id ASC"
		}
	case "name":
		if isAsc {
			orderClause = "LOWER(player_name) ASC, player_id ASC"
		} else {
			orderClause = "LOWER(player_name) DESC, player_id ASC"
		}
	default:
		if isAsc {
			orderClause = "last_seen_at ASC, player_id ASC"
		} else {
			orderClause = "last_seen_at DESC, player_id ASC"
		}
	}

	querySQL := fmt.Sprintf(`
	WITH player_agg AS (
		SELECT p.player_id, p.platform, p.player_name, p.player_name_lower, p.ranks_json, p.notes, p.tags, p.first_seen_at, p.last_seen_at,
		       COALESCE(SUM(m.wins_as_teammate), 0) AS total_wins_teammate,
		       COALESCE(SUM(m.losses_as_teammate), 0) AS total_losses_teammate,
		       COALESCE(SUM(m.wins_as_opponent), 0) AS total_wins_opponent,
		       COALESCE(SUM(m.losses_as_opponent), 0) AS total_losses_opponent,
		       COALESCE(SUM(m.total_matches), 0) AS total_matches
		FROM players p
		LEFT JOIN player_matchups m ON p.player_id = m.player_id
		%s
		GROUP BY p.player_id
	)
	SELECT player_id, platform, player_name, ranks_json, notes, tags, first_seen_at, last_seen_at,
	       total_wins_teammate, total_losses_teammate, total_wins_opponent, total_losses_opponent, total_matches
	FROM player_agg%s
	ORDER BY %s
	LIMIT ? OFFSET ?;
	`, pWhereSQL, roleWhereSQL, orderClause)

	queryArgs := make([]interface{}, len(pWhereArgs)+2)
	copy(queryArgs, pWhereArgs)
	queryArgs[len(pWhereArgs)] = limitArg
	queryArgs[len(pWhereArgs)+1] = offset

	rows, err := s.db.QueryContext(ctx, querySQL, queryArgs...)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, 0, wrapContextErr(ctxErr)
		}
		return nil, 0, fmt.Errorf("failed to filter player summaries: %w", err)
	}
	defer rows.Close()

	summaries := make([]*PlayerSummary, 0)
	for rows.Next() {
		var (
			sRec      PlayerSummary
			notes     sql.NullString
			tagsStr   sql.NullString
			firstSeen int64
			lastSeen  int64
		)
		if err := rows.Scan(
			&sRec.PlayerID,
			&sRec.Platform,
			&sRec.PlayerName,
			&sRec.RanksJSON,
			&notes,
			&tagsStr,
			&firstSeen,
			&lastSeen,
			&sRec.TotalWinsAsTeammate,
			&sRec.TotalLossesAsTeammate,
			&sRec.TotalWinsAsOpponent,
			&sRec.TotalLossesAsOpponent,
			&sRec.TotalMatches,
		); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, 0, wrapContextErr(ctxErr)
			}
			return nil, 0, fmt.Errorf("failed to scan player summary row: %w", err)
		}
		sRec.Notes = notes.String
		sRec.Tags = parseTags(tagsStr.String)
		sRec.FirstSeenAt = time.Unix(firstSeen, 0).UTC()
		sRec.LastSeenAt = time.Unix(lastSeen, 0).UTC()
		summaries = append(summaries, &sRec)
	}
	if err := rows.Err(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, 0, wrapContextErr(ctxErr)
		}
		return nil, 0, fmt.Errorf("rows iteration error in filter player summaries: %w", err)
	}

	return summaries, total, nil
}

// SearchPlayerSummaries searches players matching a query string across player_name and player_id
// (case-insensitive substring match), optionally filtered by platform (case-insensitive; "all" or "" means
// all platforms), returning paginated PlayerSummary records and the total count of matching records across all pages.
// Results are ordered by last_seen_at DESC, player_id ASC.
func (s *SQLiteStore) SearchPlayerSummaries(ctx context.Context, query, platform string, limit, offset int) ([]*PlayerSummary, int, error) {
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

// GetPlayerSpotlights computes dataset-wide global spotlights (Best Teammate, Most Faced, Toughest Rival).
func (s *SQLiteStore) GetPlayerSpotlights(ctx context.Context) (*GlobalSpotlights, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContextErr(err)
	}

	const query = `
	SELECT p.player_id, p.platform, p.player_name, p.ranks_json, p.notes, p.tags, p.first_seen_at, p.last_seen_at,
	       COALESCE(SUM(m.wins_as_teammate), 0) AS total_wins_teammate,
	       COALESCE(SUM(m.losses_as_teammate), 0) AS total_losses_teammate,
	       COALESCE(SUM(m.wins_as_opponent), 0) AS total_wins_opponent,
	       COALESCE(SUM(m.losses_as_opponent), 0) AS total_losses_opponent,
	       COALESCE(SUM(m.total_matches), 0) AS total_matches
	FROM players p
	JOIN player_matchups m ON p.player_id = m.player_id
	GROUP BY p.player_id
	HAVING total_matches > 0
	ORDER BY p.player_id ASC;
	`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, wrapContextErr(ctxErr)
		}
		return nil, fmt.Errorf("failed to query spotlight candidates: %w", err)
	}
	defer rows.Close()

	candidates := make([]*PlayerSummary, 0)
	for rows.Next() {
		var (
			sRec      PlayerSummary
			notes     sql.NullString
			tagsStr   sql.NullString
			firstSeen int64
			lastSeen  int64
		)
		if err := rows.Scan(
			&sRec.PlayerID,
			&sRec.Platform,
			&sRec.PlayerName,
			&sRec.RanksJSON,
			&notes,
			&tagsStr,
			&firstSeen,
			&lastSeen,
			&sRec.TotalWinsAsTeammate,
			&sRec.TotalLossesAsTeammate,
			&sRec.TotalWinsAsOpponent,
			&sRec.TotalLossesAsOpponent,
			&sRec.TotalMatches,
		); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, wrapContextErr(ctxErr)
			}
			return nil, fmt.Errorf("failed to scan spotlight candidate: %w", err)
		}
		sRec.Notes = notes.String
		sRec.Tags = parseTags(tagsStr.String)
		sRec.FirstSeenAt = time.Unix(firstSeen, 0).UTC()
		sRec.LastSeenAt = time.Unix(lastSeen, 0).UTC()
		candidates = append(candidates, &sRec)
	}
	if err := rows.Err(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, wrapContextErr(ctxErr)
		}
		return nil, fmt.Errorf("spotlight candidate iteration error: %w", err)
	}

	return ComputeGlobalSpotlights(candidates), nil
}

// GetLocalPlayerIDs returns all known local player account and platform IDs from auth and match records.
func (s *SQLiteStore) GetLocalPlayerIDs(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContextErr(err)
	}

	idMap := make(map[string]bool)

	// 1. From auth_state
	authRows, err := s.db.QueryContext(ctx, "SELECT account_id FROM auth_state WHERE account_id != '';")
	if err == nil {
		defer authRows.Close()
		for authRows.Next() {
			var acc string
			if err := authRows.Scan(&acc); err == nil && strings.TrimSpace(acc) != "" {
				idMap[strings.TrimSpace(acc)] = true
			}
		}
	}

	// 2. From match_players
	mpRows, err := s.db.QueryContext(ctx, "SELECT DISTINCT player_id FROM match_players WHERE is_local = 1;")
	if err == nil {
		defer mpRows.Close()
		for mpRows.Next() {
			var pid string
			if err := mpRows.Scan(&pid); err == nil && strings.TrimSpace(pid) != "" {
				idMap[strings.TrimSpace(pid)] = true
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

// UpdatePlayerRanks updates ranks_json and last_seen_at for a player.
func (s *SQLiteStore) UpdatePlayerRanks(ctx context.Context, playerID string, ranksJSON string) error {
	if strings.TrimSpace(playerID) == "" {
		return ErrPlayerNotFound
	}
	if strings.TrimSpace(ranksJSON) == "" {
		ranksJSON = "{}"
	}

	now := time.Now().UTC().Unix()
	const query = `
	UPDATE players
	SET ranks_json = ?, last_seen_at = MAX(last_seen_at, ?)
	WHERE player_id = ?;
	`

	res, err := s.db.ExecContext(ctx, query, ranksJSON, now, playerID)
	if err != nil {
		return fmt.Errorf("failed to update player ranks for %s: %w", playerID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrPlayerNotFound
	}
	return nil
}

// RecordMatchResults atomically records all player outcomes for a concluded match.
// Executes inside a single transaction. If matchGUID was already processed, rolls back
// immediately and returns ErrMatchAlreadyProcessed without modifying any counter.
// Optional matchTime preserves the original replay timestamp rather than wall-clock execution time.
func (s *SQLiteStore) RecordMatchResults(ctx context.Context, matchGUID string, playlistID int, outcomes []PlayerOutcome, matchTime ...time.Time) error {
	if err := ctx.Err(); err != nil {
		return wrapContextErr(err)
	}
	if strings.TrimSpace(matchGUID) == "" {
		return ErrInvalidGUID
	}

	matchTS := time.Now().UTC().Unix()
	hasExplicitTime := false
	if len(matchTime) > 0 && !matchTime[0].IsZero() {
		matchTS = matchTime[0].UTC().Unix()
		hasExplicitTime = true
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return wrapContextErr(ctxErr)
		}
		return fmt.Errorf("failed to begin match results tx: %w", err)
	}
	defer tx.Rollback()

	// 1. Atomic insertion into processed_match_outcomes ledger.
	// If matchGUID already exists, UNIQUE constraint fails -> return ErrMatchAlreadyProcessed.
	const insertLedgerSQL = `
	INSERT INTO processed_match_outcomes (match_guid, playlist_id, processed_at)
	VALUES (?, ?, ?);
	`
	_, err = tx.ExecContext(ctx, insertLedgerSQL, matchGUID, playlistID, matchTS)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return wrapContextErr(ctxErr)
		}
		errStr := err.Error()
		if strings.Contains(errStr, "UNIQUE constraint failed") || strings.Contains(errStr, "constraint failed") {
			return ErrMatchAlreadyProcessed
		}
		return fmt.Errorf("failed to record processed match outcome: %w", err)
	}

	// 2. Prepare statements for player upsert and matchup upsert
	const upsertPlayerSQL = `
	INSERT INTO players (player_id, platform, player_name, player_name_lower, ranks_json, first_seen_at, last_seen_at)
	VALUES (?, ?, ?, ?, '{}', ?, ?)
	ON CONFLICT(player_id) DO UPDATE SET
		platform = CASE WHEN excluded.platform != '' THEN excluded.platform ELSE players.platform END,
		player_name = CASE WHEN excluded.player_name != '' THEN excluded.player_name ELSE players.player_name END,
		player_name_lower = CASE WHEN excluded.player_name != '' THEN excluded.player_name_lower ELSE players.player_name_lower END,
		first_seen_at = MIN(players.first_seen_at, excluded.first_seen_at),
		last_seen_at = CASE WHEN ? THEN MAX(players.last_seen_at, excluded.last_seen_at) ELSE players.last_seen_at END;
	`
	stmtPlayer, err := tx.PrepareContext(ctx, upsertPlayerSQL)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return wrapContextErr(ctxErr)
		}
		return fmt.Errorf("failed to prepare upsert player stmt: %w", err)
	}
	defer stmtPlayer.Close()

	const upsertMatchupSQL = `
	INSERT INTO player_matchups (
		player_id, playlist_id,
		wins_as_teammate, losses_as_teammate,
		wins_as_opponent, losses_as_opponent,
		total_matches, last_played_at
	) VALUES (?, ?, ?, ?, ?, ?, 1, ?)
	ON CONFLICT(player_id, playlist_id) DO UPDATE SET
		wins_as_teammate = player_matchups.wins_as_teammate + excluded.wins_as_teammate,
		losses_as_teammate = player_matchups.losses_as_teammate + excluded.losses_as_teammate,
		wins_as_opponent = player_matchups.wins_as_opponent + excluded.wins_as_opponent,
		losses_as_opponent = player_matchups.losses_as_opponent + excluded.losses_as_opponent,
		total_matches = player_matchups.total_matches + 1,
		last_played_at = MAX(player_matchups.last_played_at, excluded.last_played_at);
	`
	stmtMatchup, err := tx.PrepareContext(ctx, upsertMatchupSQL)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return wrapContextErr(ctxErr)
		}
		return fmt.Errorf("failed to prepare upsert matchup stmt: %w", err)
	}
	defer stmtMatchup.Close()

	// 3. Process each outcome
	for _, outcome := range outcomes {
		if strings.TrimSpace(outcome.PlayerID) == "" {
			continue
		}

		// Ensure player exists in players table to satisfy foreign key constraint
		if _, err := stmtPlayer.ExecContext(ctx,
			outcome.PlayerID,
			outcome.Platform,
			outcome.PlayerName,
			strings.ToLower(outcome.PlayerName),
			matchTS,
			matchTS,
			hasExplicitTime,
		); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return wrapContextErr(ctxErr)
			}
			return fmt.Errorf("failed to ensure player %s exists: %w", outcome.PlayerID, err)
		}

		// 4-way win/loss matrix calculation:
		// outcome.Won represents whether the local player's team won the match.
		var (
			winTeammate  int
			lossTeammate int
			winOpponent  int
			lossOpponent int
		)

		if outcome.IsTeammate {
			if outcome.Won {
				winTeammate = 1
			} else {
				lossTeammate = 1
			}
		} else {
			if outcome.Won {
				winOpponent = 1
			} else {
				lossOpponent = 1
			}
		}

		if _, err := stmtMatchup.ExecContext(ctx,
			outcome.PlayerID,
			playlistID,
			winTeammate,
			lossTeammate,
			winOpponent,
			lossOpponent,
			matchTS,
		); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return wrapContextErr(ctxErr)
			}
			return fmt.Errorf("failed to upsert matchup for player %s in playlist %d: %w", outcome.PlayerID, playlistID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return wrapContextErr(ctxErr)
		}
		return err
	}
	return nil
}

// GetPlayerMatchup retrieves head-to-head record for a player in a specific playlist.
// If no record exists, returns zeroed PlayerMatchup with TotalMatches=0 and nil error.
func (s *SQLiteStore) GetPlayerMatchup(ctx context.Context, playerID string, playlistID int) (*PlayerMatchup, error) {
	if strings.TrimSpace(playerID) == "" {
		return nil, errors.New("player_id cannot be empty")
	}

	const query = `
	SELECT player_id, playlist_id,
	       wins_as_teammate, losses_as_teammate,
	       wins_as_opponent, losses_as_opponent,
	       total_matches, last_played_at
	FROM player_matchups
	WHERE player_id = ? AND playlist_id = ?;
	`

	var (
		m          PlayerMatchup
		lastPlayed int64
	)

	err := s.db.QueryRowContext(ctx, query, playerID, playlistID).Scan(
		&m.PlayerID,
		&m.PlaylistID,
		&m.WinsAsTeammate,
		&m.LossesAsTeammate,
		&m.WinsAsOpponent,
		&m.LossesAsOpponent,
		&m.TotalMatches,
		&lastPlayed,
	)
	if errors.Is(err, sql.ErrNoRows) {
		// When no matchup record exists, return zeroed record with TotalMatches=0 and nil error
		return &PlayerMatchup{
			PlayerID:   playerID,
			PlaylistID: playlistID,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get player matchup for %s (playlist %d): %w", playerID, playlistID, err)
	}

	if lastPlayed > 0 {
		m.LastPlayedAt = time.Unix(lastPlayed, 0).UTC()
	}
	return &m, nil
}

// GetPlayerMatchups retrieves all playlist matchups for a player ordered by playlist_id ASC.
func (s *SQLiteStore) GetPlayerMatchups(ctx context.Context, playerID string) ([]*PlayerMatchup, error) {
	if strings.TrimSpace(playerID) == "" {
		return nil, errors.New("player_id cannot be empty")
	}

	const query = `
	SELECT player_id, playlist_id,
	       wins_as_teammate, losses_as_teammate,
	       wins_as_opponent, losses_as_opponent,
	       total_matches, last_played_at
	FROM player_matchups
	WHERE player_id = ?
	ORDER BY playlist_id ASC;
	`

	rows, err := s.db.QueryContext(ctx, query, playerID)
	if err != nil {
		return nil, fmt.Errorf("failed to get player matchups for %s: %w", playerID, err)
	}
	defer rows.Close()

	matchups := make([]*PlayerMatchup, 0)
	for rows.Next() {
		var (
			m          PlayerMatchup
			lastPlayed int64
		)
		if err := rows.Scan(
			&m.PlayerID,
			&m.PlaylistID,
			&m.WinsAsTeammate,
			&m.LossesAsTeammate,
			&m.WinsAsOpponent,
			&m.LossesAsOpponent,
			&m.TotalMatches,
			&lastPlayed,
		); err != nil {
			return nil, fmt.Errorf("failed to scan player matchup row: %w", err)
		}
		if lastPlayed > 0 {
			m.LastPlayedAt = time.Unix(lastPlayed, 0).UTC()
		}
		matchups = append(matchups, &m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error in get player matchups: %w", err)
	}

	return matchups, nil
}

// SyncReplayStats enriches or upserts a match record with end-of-game replay statistics,
// keyed by MatchGUID, and populates player records and match_players.
func (s *SQLiteStore) SyncReplayStats(ctx context.Context, stats *ReplayMatchStats) error {
	if stats == nil || strings.TrimSpace(stats.MatchGUID) == "" {
		return ErrInvalidGUID
	}

	guid := strings.TrimSpace(stats.MatchGUID)
	statsJSON, err := json.Marshal(stats)
	if err != nil {
		return fmt.Errorf("failed to marshal replay stats: %w", err)
	}

	now := time.Now().UTC()
	nowUnix := now.Unix()

	winnerVal := int64(-1)
	if stats.WinnerTeam != nil {
		winnerVal = int64(*stats.WinnerTeam)
	}

	startTS := nowUnix
	if stats.Date != "" {
		if t, ok := ParseReplayDate(stats.Date); ok {
			startTS = t.Unix()
		}
	}

	dlStatus := DownloadDownloaded
	upStatus := UploadPending
	if stats.BallchasingID != "" {
		upStatus = UploadUploaded
	}

	const query = `
	INSERT INTO matches (
		match_guid, record_start_timestamp, map_name, playlist,
		download_status, upload_status, ballchasing_id, ballchasing_url,
		blue_score, orange_score, winner_team, replay_stats_json, synced_at,
		stats_sync_status, stats_retry_count, created_at, updated_at
	) VALUES (
		?, ?, ?, ?,
		?, ?, ?, ?,
		?, ?, ?, ?, ?,
		?, 0, ?, ?
	)
	ON CONFLICT(match_guid) DO UPDATE SET
		record_start_timestamp = CASE WHEN matches.record_start_timestamp > 0 THEN matches.record_start_timestamp ELSE excluded.record_start_timestamp END,
		map_name = CASE WHEN excluded.map_name != '' THEN excluded.map_name ELSE matches.map_name END,
		playlist = CASE WHEN excluded.playlist != 0 THEN excluded.playlist ELSE matches.playlist END,
		ballchasing_id = CASE WHEN excluded.ballchasing_id != '' THEN excluded.ballchasing_id ELSE matches.ballchasing_id END,
		ballchasing_url = CASE WHEN excluded.ballchasing_url != '' THEN excluded.ballchasing_url ELSE matches.ballchasing_url END,
		blue_score = excluded.blue_score,
		orange_score = excluded.orange_score,
		winner_team = excluded.winner_team,
		replay_stats_json = excluded.replay_stats_json,
		synced_at = excluded.synced_at,
		download_status = CASE WHEN matches.download_status = 'PENDING' THEN 'DOWNLOADED' ELSE matches.download_status END,
		stats_sync_status = CASE WHEN excluded.ballchasing_id != '' THEN 'SYNCED' ELSE matches.stats_sync_status END,
		updated_at = excluded.updated_at;
	`

	_, err = s.db.ExecContext(ctx, query,
		guid, startTS, stats.MapName, stats.Playlist,
		dlStatus, upStatus, stats.BallchasingID, stats.BallchasingURL,
		stats.BlueScore, stats.OrangeScore, winnerVal, string(statsJSON), nowUnix,
		string(StatsSyncSynced), nowUnix, nowUnix,
	)
	if err != nil {
		return fmt.Errorf("failed to sync replay stats for %s: %w", guid, err)
	}

	// Upsert all players in this replay into players table and match_players
	localAccounts := s.getLocalAccountIDs(ctx)
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
		}
		matchPlayers = append(matchPlayers, mp)

		pRec, err := s.GetPlayer(ctx, canonicalID)
		if err != nil {
			pRec = &PlayerRecord{
				PlayerID:    canonicalID,
				Platform:    plat,
				PlayerName:  strings.TrimSpace(p.Name),
				FirstSeenAt: matchTime,
				LastSeenAt:  matchTime,
			}
			_ = s.UpsertPlayer(ctx, pRec)
		} else {
			pRec.Platform = plat
			pRec.PlayerName = strings.TrimSpace(p.Name)
			if matchTime.Before(pRec.FirstSeenAt) {
				pRec.FirstSeenAt = matchTime
			}
			if matchTime.After(pRec.LastSeenAt) {
				pRec.LastSeenAt = matchTime
			}
			_ = s.UpsertPlayer(ctx, pRec)
		}
	}

	if len(matchPlayers) > 0 {
		_ = s.SaveMatchPlayers(ctx, guid, matchPlayers)
	}

	s.hydrateRanksFromStats(ctx, stats)

	return nil
}

// ListHistoricalMatches returns paginated historical matches matching the filter,
// ordered by record_start_timestamp DESC, match_guid ASC.
func (s *SQLiteStore) ListHistoricalMatches(ctx context.Context, filter MatchFilter) ([]*HistoricalMatchRecord, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, wrapContextErr(err)
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

	outcomeExpr := `
	CASE
		WHEN (SELECT lp.team_num FROM match_players lp WHERE lp.match_guid = matches.match_guid AND lp.is_local = 1 LIMIT 1) = 0 THEN
			CASE
				WHEN blue_score > orange_score THEN 'victory'
				WHEN orange_score > blue_score THEN 'defeat'
				WHEN winner_team != -1 THEN
					CASE WHEN winner_team = 0 THEN 'victory' ELSE 'defeat' END
				WHEN blue_score = orange_score AND (blue_score > 0 OR orange_score > 0) THEN 'draw'
				ELSE 'draw'
			END
		WHEN (SELECT lp.team_num FROM match_players lp WHERE lp.match_guid = matches.match_guid AND lp.is_local = 1 LIMIT 1) = 1 THEN
			CASE
				WHEN orange_score > blue_score THEN 'victory'
				WHEN blue_score > orange_score THEN 'defeat'
				WHEN winner_team != -1 THEN
					CASE WHEN winner_team = 1 THEN 'victory' ELSE 'defeat' END
				WHEN blue_score = orange_score AND (blue_score > 0 OR orange_score > 0) THEN 'draw'
				ELSE 'draw'
			END
		ELSE
			CASE
				WHEN blue_score > orange_score THEN 'victory'
				WHEN orange_score > blue_score THEN 'defeat'
				WHEN winner_team != -1 THEN
					CASE WHEN winner_team = 0 THEN 'victory' ELSE 'defeat' END
				WHEN blue_score = orange_score AND (blue_score > 0 OR orange_score > 0) THEN 'draw'
				ELSE 'draw'
			END
	END`

	var whereClauses []string
	var args []interface{}

	if filter.Playlist > 0 {
		whereClauses = append(whereClauses, "playlist = ?")
		args = append(args, filter.Playlist)
	}

	if q := strings.TrimSpace(filter.Query); q != "" {
		likePattern := "%" + q + "%"
		whereClauses = append(whereClauses, "(match_guid LIKE ? OR map_name LIKE ? OR replay_stats_json LIKE ?)")
		args = append(args, likePattern, likePattern, likePattern)
	}

	cleanResult := strings.ToLower(strings.TrimSpace(filter.Result))
	if cleanResult != "" && cleanResult != "all" {
		whereClauses = append(whereClauses, fmt.Sprintf("(%s) = ?", outcomeExpr))
		args = append(args, cleanResult)
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM matches %s;", whereSQL)
	var total int
	if err := s.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, 0, wrapContextErr(ctxErr)
		}
		return nil, 0, fmt.Errorf("failed to count historical matches: %w", err)
	}

	if total == 0 || offset >= total {
		return make([]*HistoricalMatchRecord, 0), total, nil
	}

	querySQL := fmt.Sprintf(`
	SELECT match_guid, record_start_timestamp, map_name, playlist, replay_url,
	       download_status, local_file_path, downloaded_at, upload_status,
	       ballchasing_id, ballchasing_url, uploaded_at, retry_count,
	       last_error, blue_score, orange_score, winner_team, replay_stats_json,
	       synced_at, stats_sync_status, stats_retry_count, created_at, updated_at,
	       (%s) AS outcome
	FROM matches
	%s
	ORDER BY record_start_timestamp DESC, match_guid ASC
	LIMIT ? OFFSET ?;
	`, outcomeExpr, whereSQL)

	queryArgs := append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, querySQL, queryArgs...)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, 0, wrapContextErr(ctxErr)
		}
		return nil, 0, fmt.Errorf("failed to query historical matches: %w", err)
	}
	defer rows.Close()

	records := make([]*HistoricalMatchRecord, 0)
	for rows.Next() {
		var (
			mr           MatchRecord
			downloadedAt sql.NullInt64
			uploadedAt   sql.NullInt64
			winnerTeam   sql.NullInt64
			syncedAt     sql.NullInt64
			blueScore    sql.NullInt64
			orangeScore  sql.NullInt64
			replayStats  sql.NullString
			statsSync    sql.NullString
			statsRetry   sql.NullInt64
			createdAt    int64
			updatedAt    int64
			outcome      string
		)

		if err := rows.Scan(
			&mr.MatchGUID, &mr.RecordStartTimestamp, &mr.MapName, &mr.Playlist, &mr.ReplayURL,
			&mr.DownloadStatus, &mr.LocalFilePath, &downloadedAt, &mr.UploadStatus,
			&mr.BallchasingID, &mr.BallchasingURL, &uploadedAt, &mr.RetryCount,
			&mr.LastError, &blueScore, &orangeScore, &winnerTeam, &replayStats,
			&syncedAt, &statsSync, &statsRetry, &createdAt, &updatedAt,
			&outcome,
		); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, 0, wrapContextErr(ctxErr)
			}
			return nil, 0, fmt.Errorf("failed to scan match: %w", err)
		}

		mr.BlueScore = int(blueScore.Int64)
		mr.OrangeScore = int(orangeScore.Int64)
		if winnerTeam.Valid {
			wt := int(winnerTeam.Int64)
			mr.WinnerTeam = &wt
		}
		if downloadedAt.Valid {
			t := time.Unix(downloadedAt.Int64, 0).UTC()
			mr.DownloadedAt = &t
		}
		if uploadedAt.Valid {
			t := time.Unix(uploadedAt.Int64, 0).UTC()
			mr.UploadedAt = &t
		}
		if syncedAt.Valid {
			t := time.Unix(syncedAt.Int64, 0).UTC()
			mr.SyncedAt = &t
		}
		mr.ReplayStatsJSON = replayStats.String
		mr.StatsSyncStatus = StatsSyncStatus(statsSync.String)
		if mr.StatsSyncStatus == "" {
			mr.StatsSyncStatus = StatsSyncPending
		}
		mr.StatsRetryCount = int(statsRetry.Int64)
		mr.CreatedAt = time.Unix(createdAt, 0).UTC()
		mr.UpdatedAt = time.Unix(updatedAt, 0).UTC()

		hist := &HistoricalMatchRecord{
			MatchRecord: mr,
			Result:      outcome,
		}
		if mr.ReplayStatsJSON != "" {
			var st ReplayMatchStats
			if err := json.Unmarshal([]byte(mr.ReplayStatsJSON), &st); err == nil {
				hist.Stats = &st
			}
		}
		records = append(records, hist)
	}
	if err := rows.Err(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, 0, wrapContextErr(ctxErr)
		}
		return nil, 0, fmt.Errorf("rows error: %w", err)
	}

	return records, total, nil
}

// getLocalAccountIDs returns a set of known local account IDs from auth_state.
func (s *SQLiteStore) getLocalAccountIDs(ctx context.Context) map[string]bool {
	accounts := make(map[string]bool)
	rows, err := s.db.QueryContext(ctx, "SELECT account_id FROM auth_state WHERE account_id != '';")
	if err != nil {
		return accounts
	}
	defer rows.Close()
	for rows.Next() {
		var acc string
		if err := rows.Scan(&acc); err == nil && acc != "" {
			accounts[strings.ToLower(acc)] = true
		}
	}
	return accounts
}

// formatPlaylistName returns a standard display name for Rocket League playlist IDs.
func formatPlaylistName(id int) string {
	switch id {
	case 1, 2, 3, 4:
		return "Casual"
	case 10:
		return "Ranked Duel 1v1"
	case 11:
		return "Ranked Doubles 2v2"
	case 12:
		return "Ranked Solo Standard 3v3"
	case 13:
		return "Ranked Standard 3v3"
	case 27:
		return "Hoops"
	case 28:
		return "Rumble"
	case 29:
		return "Dropshot"
	case 30:
		return "Snow Day"
	case 34:
		return "Tournament"
	default:
		return fmt.Sprintf("Playlist %d", id)
	}
}

// formatRankName formats tier and division into standard Rocket League rank names.
func formatRankName(tier, division int) string {
	tierNames := map[int]string{
		0:  "Unranked",
		1:  "Bronze I",
		2:  "Bronze II",
		3:  "Bronze III",
		4:  "Silver I",
		5:  "Silver II",
		6:  "Silver III",
		7:  "Gold I",
		8:  "Gold II",
		9:  "Gold III",
		10: "Platinum I",
		11: "Platinum II",
		12: "Platinum III",
		13: "Champion I",
		14: "Champion II",
		15: "Champion III",
		16: "Grand Champion I",
		17: "Grand Champion II",
		18: "Grand Champion III",
		19: "Supersonic Legend",
	}
	name, ok := tierNames[tier]
	if !ok || tier == 0 || tier >= 19 {
		if !ok {
			return fmt.Sprintf("Tier %d", tier)
		}
		return name
	}
	divNames := []string{"Div I", "Div II", "Div III", "Div IV"}
	if division >= 0 && division < len(divNames) {
		return fmt.Sprintf("%s %s", name, divNames[division])
	}
	return name
}

// hydrateRanksFromStats attempts to parse rank metadata from replay raw JSON and update player ranks.
func (s *SQLiteStore) hydrateRanksFromStats(ctx context.Context, stats *ReplayMatchStats) {
	if stats == nil || stats.RawJSON == "" || stats.Playlist <= 0 {
		return
	}

	type bcPlayerRank struct {
		ID       int    `json:"id"`
		Tier     int    `json:"tier"`
		Division int    `json:"division"`
		Name     string `json:"name"`
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
		if bp.Rank == nil || (bp.Rank.Tier == 0 && bp.Rank.ID == 0) {
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

		pRec, err := s.GetPlayer(ctx, idStr)
		if err != nil {
			continue
		}

		ranks := make(map[string]map[string]interface{})
		if pRec.RanksJSON != "" && pRec.RanksJSON != "{}" {
			_ = json.Unmarshal([]byte(pRec.RanksJSON), &ranks)
		}

		rName := bp.Rank.Name
		if rName == "" {
			rName = formatRankName(bp.Rank.Tier, bp.Rank.Division)
		}

		pKey := strconv.Itoa(stats.Playlist)
		ranks[pKey] = map[string]interface{}{
			"playlist_id":    stats.Playlist,
			"playlist_name":  formatPlaylistName(stats.Playlist),
			"tier":           bp.Rank.Tier,
			"division":       bp.Rank.Division,
			"rank_name":      rName,
			"mmr":            0.0,
			"matches_played": 0,
		}

		if updatedData, err := json.Marshal(ranks); err == nil {
			_ = s.UpdatePlayerRanks(ctx, pRec.PlayerID, string(updatedData))
		}
	}
}

// SaveMatchPlayers bulk upserts player match participation records for a match.
func (s *SQLiteStore) SaveMatchPlayers(ctx context.Context, matchGUID string, players []*MatchPlayerRecord) error {
	if strings.TrimSpace(matchGUID) == "" {
		return ErrInvalidGUID
	}
	if len(players) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin save match players tx: %w", err)
	}
	defer tx.Rollback()

	const query = `
	INSERT INTO match_players (
		match_guid, player_id, team_num, is_local, won,
		score, goals, assists, saves, shots, demos,
		car_name, stats_json
	) VALUES (
		?, ?, ?, ?, ?,
		?, ?, ?, ?, ?, ?,
		?, ?
	)
	ON CONFLICT(match_guid, player_id) DO UPDATE SET
		team_num = excluded.team_num,
		is_local = excluded.is_local,
		won = excluded.won,
		score = excluded.score,
		goals = excluded.goals,
		assists = excluded.assists,
		saves = excluded.saves,
		shots = excluded.shots,
		demos = excluded.demos,
		car_name = CASE WHEN excluded.car_name != '' THEN excluded.car_name ELSE match_players.car_name END,
		stats_json = CASE WHEN excluded.stats_json != '' THEN excluded.stats_json ELSE match_players.stats_json END;
	`

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to prepare save match player stmt: %w", err)
	}
	defer stmt.Close()

	for _, p := range players {
		if p == nil || strings.TrimSpace(p.PlayerID) == "" {
			continue
		}

		var wonVal *int
		if p.Won != nil {
			w := 0
			if *p.Won {
				w = 1
			}
			wonVal = &w
		}

		isLocal := 0
		if p.IsLocal {
			isLocal = 1
		}

		if _, err := stmt.ExecContext(ctx,
			matchGUID,
			p.PlayerID,
			p.TeamNum,
			isLocal,
			wonVal,
			p.Score,
			p.Goals,
			p.Assists,
			p.Saves,
			p.Shots,
			p.Demos,
			p.CarName,
			p.StatsJSON,
		); err != nil {
			return fmt.Errorf("failed to save match player %s in %s: %w", p.PlayerID, matchGUID, err)
		}
	}

	return tx.Commit()
}

// GetMatchPlayers retrieves all player records for a match GUID ordered by team_num ASC, score DESC.
func (s *SQLiteStore) GetMatchPlayers(ctx context.Context, matchGUID string) ([]*MatchPlayerRecord, error) {
	if strings.TrimSpace(matchGUID) == "" {
		return nil, ErrInvalidGUID
	}

	const query = `
	SELECT match_guid, player_id, team_num, is_local, won,
	       score, goals, assists, saves, shots, demos,
	       car_name, stats_json
	FROM match_players
	WHERE match_guid = ?
	ORDER BY team_num ASC, score DESC;
	`

	rows, err := s.db.QueryContext(ctx, query, matchGUID)
	if err != nil {
		return nil, fmt.Errorf("failed to query match players for %s: %w", matchGUID, err)
	}
	defer rows.Close()

	var records []*MatchPlayerRecord
	for rows.Next() {
		var (
			mp      MatchPlayerRecord
			isLocal int
			wonVal  sql.NullInt64
		)
		if err := rows.Scan(
			&mp.MatchGUID,
			&mp.PlayerID,
			&mp.TeamNum,
			&isLocal,
			&wonVal,
			&mp.Score,
			&mp.Goals,
			&mp.Assists,
			&mp.Saves,
			&mp.Shots,
			&mp.Demos,
			&mp.CarName,
			&mp.StatsJSON,
		); err != nil {
			return nil, fmt.Errorf("failed to scan match player row: %w", err)
		}
		mp.IsLocal = (isLocal == 1)
		if wonVal.Valid {
			w := (wonVal.Int64 == 1)
			mp.Won = &w
		}
		records = append(records, &mp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error in get match players: %w", err)
	}

	if records == nil {
		records = make([]*MatchPlayerRecord, 0)
	}
	return records, nil
}

// ListPlayerMatches returns paginated match details for a specific player with role and result calculations.
func (s *SQLiteStore) ListPlayerMatches(ctx context.Context, playerID string, filter PlayerMatchFilter) ([]*PlayerMatchDetail, int, error) {
	if strings.TrimSpace(playerID) == "" {
		return nil, 0, ErrPlayerNotFound
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

	var whereClauses []string
	var args []interface{}

	whereClauses = append(whereClauses, "mp.player_id = ?")
	args = append(args, playerID)

	if filter.Playlist > 0 {
		whereClauses = append(whereClauses, "m.playlist = ?")
		args = append(args, filter.Playlist)
	}

	cleanResult := strings.ToLower(strings.TrimSpace(filter.Result))
	switch cleanResult {
	case "victory":
		whereClauses = append(whereClauses, "(mp.won = 1 OR (mp.won IS NULL AND m.winner_team = mp.team_num))")
	case "defeat":
		whereClauses = append(whereClauses, "(mp.won = 0 OR (mp.won IS NULL AND m.winner_team >= 0 AND m.winner_team != mp.team_num))")
	case "draw":
		whereClauses = append(whereClauses, "((mp.won IS NULL AND m.winner_team = -1) OR (m.blue_score = m.orange_score AND (m.blue_score > 0 OR m.orange_score > 0)))")
	}

	cleanRole := strings.ToLower(strings.TrimSpace(filter.Role))
	switch cleanRole {
	case "teammate":
		whereClauses = append(whereClauses, `(
			EXISTS (SELECT 1 FROM match_players lp WHERE lp.match_guid = m.match_guid AND lp.is_local = 1 AND lp.team_num = mp.team_num AND lp.player_id != mp.player_id)
			OR mp.is_local = 1
		)`)
	case "opponent":
		whereClauses = append(whereClauses, `EXISTS (SELECT 1 FROM match_players lp WHERE lp.match_guid = m.match_guid AND lp.is_local = 1 AND lp.team_num != mp.team_num)`)
	}

	whereSQL := "WHERE " + strings.Join(whereClauses, " AND ")

	countSQL := fmt.Sprintf(`
	SELECT COUNT(*)
	FROM match_players mp
	JOIN matches m ON mp.match_guid = m.match_guid
	%s;
	`, whereSQL)

	var total int
	if err := s.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count player matches: %w", err)
	}

	if total == 0 {
		return make([]*PlayerMatchDetail, 0), 0, nil
	}

	querySQL := fmt.Sprintf(`
	SELECT m.match_guid, m.record_start_timestamp, m.map_name, m.playlist, m.replay_url,
	       m.download_status, m.local_file_path, m.downloaded_at, m.upload_status,
	       m.ballchasing_id, m.ballchasing_url, m.uploaded_at, m.retry_count,
	       m.last_error, m.blue_score, m.orange_score, m.winner_team, m.replay_stats_json,
	       m.synced_at, m.stats_sync_status, m.stats_retry_count, m.created_at, m.updated_at,
	       mp.player_id, mp.team_num, mp.is_local, mp.won, mp.score, mp.goals, mp.assists,
	       mp.saves, mp.shots, mp.demos, mp.car_name, mp.stats_json,
	       COALESCE((SELECT lp.team_num FROM match_players lp WHERE lp.match_guid = m.match_guid AND lp.is_local = 1 LIMIT 1), -1) AS local_team
	FROM match_players mp
	JOIN matches m ON mp.match_guid = m.match_guid
	%s
	ORDER BY m.record_start_timestamp DESC, m.match_guid ASC
	LIMIT ? OFFSET ?;
	`, whereSQL)

	queryArgs := append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, querySQL, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query player matches: %w", err)
	}
	defer rows.Close()

	var details []*PlayerMatchDetail
	for rows.Next() {
		var (
			mr           MatchRecord
			mp           MatchPlayerRecord
			downloadedAt sql.NullInt64
			uploadedAt   sql.NullInt64
			winnerTeam   sql.NullInt64
			syncedAt     sql.NullInt64
			blueScore    sql.NullInt64
			orangeScore  sql.NullInt64
			replayStats  sql.NullString
			statsSync    sql.NullString
			statsRetry   sql.NullInt64
			createdAt    int64
			updatedAt    int64
			isLocal      int
			wonVal       sql.NullInt64
			localTeam    int
		)

		if err := rows.Scan(
			&mr.MatchGUID, &mr.RecordStartTimestamp, &mr.MapName, &mr.Playlist, &mr.ReplayURL,
			&mr.DownloadStatus, &mr.LocalFilePath, &downloadedAt, &mr.UploadStatus,
			&mr.BallchasingID, &mr.BallchasingURL, &uploadedAt, &mr.RetryCount,
			&mr.LastError, &blueScore, &orangeScore, &winnerTeam, &replayStats,
			&syncedAt, &statsSync, &statsRetry, &createdAt, &updatedAt,
			&mp.PlayerID, &mp.TeamNum, &isLocal, &wonVal, &mp.Score, &mp.Goals, &mp.Assists,
			&mp.Saves, &mp.Shots, &mp.Demos, &mp.CarName, &mp.StatsJSON,
			&localTeam,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan player match detail: %w", err)
		}

		mr.BlueScore = int(blueScore.Int64)
		mr.OrangeScore = int(orangeScore.Int64)
		if winnerTeam.Valid {
			wt := int(winnerTeam.Int64)
			mr.WinnerTeam = &wt
		}
		if downloadedAt.Valid {
			t := time.Unix(downloadedAt.Int64, 0).UTC()
			mr.DownloadedAt = &t
		}
		if uploadedAt.Valid {
			t := time.Unix(uploadedAt.Int64, 0).UTC()
			mr.UploadedAt = &t
		}
		if syncedAt.Valid {
			t := time.Unix(syncedAt.Int64, 0).UTC()
			mr.SyncedAt = &t
		}
		mr.ReplayStatsJSON = replayStats.String
		mr.StatsSyncStatus = StatsSyncStatus(statsSync.String)
		if mr.StatsSyncStatus == "" {
			mr.StatsSyncStatus = StatsSyncPending
		}
		mr.StatsRetryCount = int(statsRetry.Int64)
		mr.CreatedAt = time.Unix(createdAt, 0).UTC()
		mr.UpdatedAt = time.Unix(updatedAt, 0).UTC()

		mp.MatchGUID = mr.MatchGUID
		mp.IsLocal = (isLocal == 1)
		if wonVal.Valid {
			w := (wonVal.Int64 == 1)
			mp.Won = &w
		}

		role := "teammate"
		if localTeam >= 0 {
			if mp.TeamNum == localTeam {
				role = "teammate"
			} else {
				role = "opponent"
			}
		}

		result := "unknown"
		if mp.Won != nil {
			if *mp.Won {
				result = "victory"
			} else {
				result = "defeat"
			}
		} else if mr.WinnerTeam != nil {
			if *mr.WinnerTeam == -1 {
				result = "draw"
			} else if *mr.WinnerTeam == mp.TeamNum {
				result = "victory"
			} else {
				result = "defeat"
			}
		} else if mr.BlueScore == mr.OrangeScore && (mr.BlueScore > 0 || mr.OrangeScore > 0) {
			result = "draw"
		} else if (mp.TeamNum == 0 && mr.BlueScore > mr.OrangeScore) || (mp.TeamNum == 1 && mr.OrangeScore > mr.BlueScore) {
			result = "victory"
		} else if (mp.TeamNum == 0 && mr.OrangeScore > mr.BlueScore) || (mp.TeamNum == 1 && mr.BlueScore > mr.OrangeScore) {
			result = "defeat"
		}

		var st *ReplayMatchStats
		if mr.ReplayStatsJSON != "" {
			var parsed ReplayMatchStats
			if err := json.Unmarshal([]byte(mr.ReplayStatsJSON), &parsed); err == nil {
				st = &parsed
			}
		}

		details = append(details, &PlayerMatchDetail{
			MatchRecord: mr,
			PlayerStats: mp,
			Role:        role,
			Result:      result,
			Stats:       st,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("rows error in list player matches: %w", err)
	}

	return details, total, nil
}

// RecordMMR inserts a point-in-time competitive MMR history record.
func (s *SQLiteStore) RecordMMR(ctx context.Context, record *MMRHistoryRecord) error {
	if record == nil {
		return errors.New("record cannot be nil")
	}

	recTime := record.RecordedAt
	if recTime.IsZero() {
		recTime = time.Now().UTC()
	}

	const query = `
	INSERT INTO mmr_history (recorded_at, match_guid, playlist_id, mmr, mmr_change, tier, division)
	VALUES (?, ?, ?, ?, ?, ?, ?);
	`

	res, err := s.db.ExecContext(ctx, query,
		recTime.Unix(),
		record.MatchGUID,
		record.PlaylistID,
		record.MMR,
		record.MMRChange,
		record.Tier,
		record.Division,
	)
	if err != nil {
		return fmt.Errorf("failed to record mmr: %w", err)
	}

	id, err := res.LastInsertId()
	if err == nil {
		record.ID = id
	}
	record.RecordedAt = recTime
	return nil
}

// ListMMRHistory returns competitive MMR history for a playlist ordered by recorded_at DESC, id DESC.
func (s *SQLiteStore) ListMMRHistory(ctx context.Context, playlistID int, limit int) ([]*MMRHistoryRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	var (
		rows *sql.Rows
		err  error
	)

	if playlistID > 0 {
		const query = `
		SELECT id, recorded_at, match_guid, playlist_id, mmr, mmr_change, tier, division
		FROM mmr_history
		WHERE playlist_id = ?
		ORDER BY recorded_at DESC, id DESC
		LIMIT ?;
		`
		rows, err = s.db.QueryContext(ctx, query, playlistID, limit)
	} else {
		const query = `
		SELECT id, recorded_at, match_guid, playlist_id, mmr, mmr_change, tier, division
		FROM mmr_history
		ORDER BY recorded_at DESC, id DESC
		LIMIT ?;
		`
		rows, err = s.db.QueryContext(ctx, query, limit)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to query mmr history: %w", err)
	}
	defer rows.Close()

	var records []*MMRHistoryRecord
	for rows.Next() {
		var (
			r       MMRHistoryRecord
			recUnix int64
		)
		if err := rows.Scan(
			&r.ID,
			&recUnix,
			&r.MatchGUID,
			&r.PlaylistID,
			&r.MMR,
			&r.MMRChange,
			&r.Tier,
			&r.Division,
		); err != nil {
			return nil, fmt.Errorf("failed to scan mmr history row: %w", err)
		}
		r.RecordedAt = time.Unix(recUnix, 0).UTC()
		records = append(records, &r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error in list mmr history: %w", err)
	}

	if records == nil {
		records = make([]*MMRHistoryRecord, 0)
	}
	return records, nil
}

// UpdatePlayerNotesAndTags updates notes and custom tags for a player profile.
func (s *SQLiteStore) UpdatePlayerNotesAndTags(ctx context.Context, playerID string, notes string, tags []string) error {
	if strings.TrimSpace(playerID) == "" {
		return ErrPlayerNotFound
	}

	tagsJSON := serializeTags(tags)
	const query = `
	UPDATE players
	SET notes = ?, tags = ?
	WHERE player_id = ?;
	`

	res, err := s.db.ExecContext(ctx, query, notes, tagsJSON, playerID)
	if err != nil {
		return fmt.Errorf("failed to update player notes and tags for %s: %w", playerID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrPlayerNotFound
	}
	return nil
}

// ListPendingStatsSync returns matches requiring post-upload Ballchasing stats synchronization.
func (s *SQLiteStore) ListPendingStatsSync(ctx context.Context, maxRetries int) ([]*MatchRecord, error) {
	effectiveMaxRetries := maxRetries
	if effectiveMaxRetries <= 0 {
		effectiveMaxRetries = s.maxRetries
		if effectiveMaxRetries <= 0 {
			effectiveMaxRetries = DefaultMaxRetries
		}
	}
	baseSec := int64(s.baseBackoff / time.Second)
	if s.baseBackoff > 0 && baseSec == 0 {
		baseSec = 1
	}
	maxSec := int64(s.maxBackoff / time.Second)
	if maxSec <= 0 {
		maxSec = 86400
	}
	nowUnix := time.Now().UTC().Unix()

	const query = `
	SELECT match_guid, record_start_timestamp, map_name, playlist, replay_url,
	       download_status, local_file_path, downloaded_at, upload_status,
	       ballchasing_id, ballchasing_url, uploaded_at, retry_count,
	       last_error, blue_score, orange_score, winner_team, replay_stats_json,
	       synced_at, stats_sync_status, stats_retry_count, created_at, updated_at
	FROM matches
	WHERE (upload_status = 'UPLOADED' OR upload_status = 'DUPLICATE')
	  AND ballchasing_id != ''
	  AND (
	    stats_sync_status = 'PENDING'
	    OR (
	      stats_sync_status = 'FAILED'
	      AND stats_retry_count < ?
	      AND (? - updated_at) >= MIN(?, ? * (1 << (CASE WHEN stats_retry_count > 0 THEN stats_retry_count - 1 ELSE 0 END)))
	    )
	  )
	ORDER BY record_start_timestamp ASC;
	`

	rows, err := s.db.QueryContext(ctx, query, effectiveMaxRetries, nowUnix, maxSec, baseSec)
	if err != nil {
		return nil, fmt.Errorf("failed to list pending stats sync: %w", err)
	}
	defer rows.Close()

	return scanMatchRecords(rows)
}

// MarkStatsSyncing transitions a match's stats sync status to SYNCING.
func (s *SQLiteStore) MarkStatsSyncing(ctx context.Context, matchGUID string) error {
	if strings.TrimSpace(matchGUID) == "" {
		return ErrInvalidGUID
	}

	now := time.Now().UTC().Unix()
	const query = `
	UPDATE matches
	SET stats_sync_status = ?, updated_at = ?
	WHERE match_guid = ?;
	`
	res, err := s.db.ExecContext(ctx, query, string(StatsSyncSyncing), now, matchGUID)
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
	const query = `
	UPDATE matches
	SET stats_sync_status = ?, synced_at = ?, stats_retry_count = 0, updated_at = ?
	WHERE match_guid = ?;
	`
	res, err := s.db.ExecContext(ctx, query, string(StatsSyncSynced), now, now, matchGUID)
	if err != nil {
		return fmt.Errorf("failed to mark stats synced for %s: %w", matchGUID, err)
	}
	return checkRowsAffected(res)
}

// MarkStatsSyncFailed increments retry count and sets stats_sync_status to FAILED.
func (s *SQLiteStore) MarkStatsSyncFailed(ctx context.Context, matchGUID, errMsg string) error {
	if strings.TrimSpace(matchGUID) == "" {
		return ErrInvalidGUID
	}

	now := time.Now().UTC().Unix()
	const query = `
	UPDATE matches
	SET stats_sync_status = ?, stats_retry_count = stats_retry_count + 1, last_error = ?, updated_at = ?
	WHERE match_guid = ?;
	`
	res, err := s.db.ExecContext(ctx, query, string(StatsSyncFailed), errMsg, now, matchGUID)
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
	SET retry_count = 0,
	    stats_retry_count = 0,
	    last_error = '',
	    download_status = CASE WHEN download_status = 'FAILED' THEN 'PENDING' ELSE download_status END,
	    upload_status = CASE WHEN upload_status = 'FAILED' THEN 'PENDING' ELSE upload_status END,
	    stats_sync_status = CASE WHEN stats_sync_status = 'FAILED' OR stats_sync_status = 'SYNCING' THEN 'PENDING' ELSE stats_sync_status END,
	    updated_at = ?
	WHERE match_guid = ?;
	`
	res, err := s.db.ExecContext(ctx, query, now, matchGUID)
	if err != nil {
		return fmt.Errorf("failed to reset match retry for %s: %w", matchGUID, err)
	}
	return checkRowsAffected(res)
}

// migrateMatchPlayers backfills match_players from existing matches with replay_stats_json on startup.
func migrateMatchPlayers(ctx context.Context, db *sql.DB) error {
	var tableCount int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='matches';").Scan(&tableCount)
	if err != nil || tableCount == 0 {
		return nil
	}

	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='match_players';").Scan(&tableCount)
	if err != nil || tableCount == 0 {
		return nil
	}

	const query = `
	SELECT m.match_guid, m.winner_team, m.replay_stats_json
	FROM matches m
	WHERE m.replay_stats_json != ''
	  AND NOT EXISTS (SELECT 1 FROM match_players mp WHERE mp.match_guid = m.match_guid);
	`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query matches for match_players backfill: %w", err)
	}
	defer rows.Close()

	type matchToBackfill struct {
		guid       string
		winnerTeam sql.NullInt64
		statsJSON  string
	}
	var matches []matchToBackfill
	for rows.Next() {
		var m matchToBackfill
		if err := rows.Scan(&m.guid, &m.winnerTeam, &m.statsJSON); err != nil {
			return fmt.Errorf("failed to scan match for backfill: %w", err)
		}
		matches = append(matches, m)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("row iteration error in match_players backfill: %w", err)
	}

	if len(matches) == 0 {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin backfill tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
	INSERT INTO match_players (
		match_guid, player_id, team_num, is_local, won,
		score, goals, assists, saves, shots, demos,
		car_name, stats_json
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(match_guid, player_id) DO NOTHING;
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare insert match_players stmt: %w", err)
	}
	defer stmt.Close()

	for _, m := range matches {
		var st ReplayMatchStats
		if err := json.Unmarshal([]byte(m.statsJSON), &st); err != nil {
			continue
		}
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

			var wonVal *int
			if m.winnerTeam.Valid {
				wt := int(m.winnerTeam.Int64)
				if wt >= 0 {
					w := 0
					if wt == p.Team {
						w = 1
					}
					wonVal = &w
				}
			}

			if _, err := stmt.ExecContext(ctx,
				m.guid, canonicalID, p.Team, 0, wonVal,
				p.Score, p.Goals, p.Assists, p.Saves, p.Shots, p.Demos,
				"", "",
			); err != nil {
				return fmt.Errorf("failed to backfill match_player %s in %s: %w", canonicalID, m.guid, err)
			}
		}
	}

	return tx.Commit()
}
