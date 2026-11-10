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

	"github.com/dank/rl-api-utils/internal/ballchasing"
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

CREATE TABLE IF NOT EXISTS sessions (
    session_id TEXT PRIMARY KEY,
    started_at INTEGER NOT NULL,
    ended_at INTEGER,
    last_match_at INTEGER,
    updated_at INTEGER NOT NULL,
    total_matches INTEGER NOT NULL DEFAULT 0,
    total_wins INTEGER NOT NULL DEFAULT 0,
    total_losses INTEGER NOT NULL DEFAULT 0,
    win_rate REAL NOT NULL DEFAULT 0,
    playlists_json TEXT NOT NULL DEFAULT '{}',
    matches_json TEXT NOT NULL DEFAULT '[]',
    local_player_ids TEXT NOT NULL DEFAULT '[]',
    dismissed INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_sessions_started_at ON sessions(started_at DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_last_match_at ON sessions(last_match_at DESC);
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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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

	if err := migrateSessionsTable(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate sessions schema: %w", err)
	}

	// Run startup backfill migration for match_players
	if err := migrateMatchPlayers(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to backfill match_players: %w", err)
	}

	store := &SQLiteStore{
		db:          db,
		dbPath:      dbPath,
		maxRetries:  DefaultMaxRetries,
		baseBackoff: DefaultBaseBackoff,
		maxBackoff:  DefaultMaxBackoff,
	}

	_ = store.repairHistoricalMatchesAndOutcomes(ctx)

	return store, nil
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

// GetMatch retrieves a single match record by GUID (falling back to case/hyphen-insensitive GUID or ballchasing_id).
func (s *SQLiteStore) GetMatch(ctx context.Context, matchGUID string) (*MatchRecord, error) {
	matchGUID = strings.TrimSpace(matchGUID)
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
		const fallbackQuery = `
		SELECT match_guid, record_start_timestamp, map_name, playlist, replay_url,
		       download_status, local_file_path, downloaded_at, upload_status,
		       ballchasing_id, ballchasing_url, uploaded_at, retry_count,
		       last_error, blue_score, orange_score, winner_team, replay_stats_json,
		       synced_at, stats_sync_status, stats_retry_count, created_at, updated_at
		FROM matches
		WHERE LOWER(REPLACE(match_guid, '-', '')) = LOWER(REPLACE(?, '-', ''))
		   OR ballchasing_id = ?
		ORDER BY record_start_timestamp DESC
		LIMIT 1;
		`
		fbRow := s.db.QueryRowContext(ctx, fallbackQuery, matchGUID, matchGUID)
		fbRec, fbErr := scanMatchRecord(fbRow)
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

	const query = `
	SELECT match_guid, record_start_timestamp, map_name, playlist, replay_url,
	       download_status, local_file_path, downloaded_at, upload_status,
	       ballchasing_id, ballchasing_url, uploaded_at, retry_count,
	       last_error, blue_score, orange_score, winner_team, replay_stats_json,
	       synced_at, stats_sync_status, stats_retry_count, created_at, updated_at
	FROM matches
	WHERE ballchasing_id = ?
	ORDER BY record_start_timestamp DESC
	LIMIT 1;
	`
	row := s.db.QueryRowContext(ctx, query, ballchasingID)
	rec, err := scanMatchRecord(row)
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
		if err := s.db.QueryRowContext(ctx, "SELECT match_guid FROM matches WHERE match_guid = ? LIMIT 1;", guid).Scan(&exact); err == nil && exact != "" {
			return exact
		}
	}
	if ballchasingID != "" {
		var byBC string
		if err := s.db.QueryRowContext(ctx, "SELECT match_guid FROM matches WHERE ballchasing_id = ? ORDER BY record_start_timestamp DESC LIMIT 1;", ballchasingID).Scan(&byBC); err == nil && byBC != "" {
			return byBC
		}
	}
	if guid != "" {
		var byNorm string
		if err := s.db.QueryRowContext(ctx, "SELECT match_guid FROM matches WHERE LOWER(REPLACE(match_guid, '-', '')) = LOWER(REPLACE(?, '-', '')) ORDER BY record_start_timestamp DESC LIMIT 1;", guid).Scan(&byNorm); err == nil && byNorm != "" {
			return byNorm
		}
	}
	return guid
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
	ballchasingURL = NormalizeBallchasingURL(ballchasingID, ballchasingURL)
	res, err := s.db.ExecContext(ctx,
		`UPDATE matches SET upload_status = ?, ballchasing_id = ?, ballchasing_url = ?, uploaded_at = ?, last_error = '', stats_sync_status = CASE WHEN replay_stats_json LIKE '%"boost":%' THEN stats_sync_status ELSE 'PENDING' END, stats_retry_count = 0, updated_at = ? WHERE match_guid = ?;`,
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
	ballchasingURL = NormalizeBallchasingURL(ballchasingID, ballchasingURL)
	res, err := s.db.ExecContext(ctx,
		`UPDATE matches SET upload_status = ?, ballchasing_id = ?, ballchasing_url = ?, uploaded_at = ?, last_error = '', stats_sync_status = CASE WHEN replay_stats_json LIKE '%"boost":%' THEN stats_sync_status ELSE 'PENDING' END, stats_retry_count = 0, updated_at = ? WHERE match_guid = ?;`,
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

// resolveCanonicalPlayerID resolves a raw OnlineID (e.g. "76561198816594023" or "epic_p2"),
// a composite ID with an alternate platform prefix (e.g. "PlayStation|7370223800633025836|0"),
// or a player display name to its canonical PlayerID in the players table.
// If no match is found, returns playerID unchanged.
func (s *SQLiteStore) resolveCanonicalPlayerID(ctx context.Context, playerID string) string {
	trimmed := strings.TrimSpace(playerID)
	if trimmed == "" {
		return playerID
	}
	var exact string
	if err := s.db.QueryRowContext(ctx, "SELECT player_id FROM players WHERE player_id = ? LIMIT 1;", trimmed).Scan(&exact); err == nil && exact != "" {
		return exact
	}
	lookupToken := trimmed
	if strings.Contains(trimmed, "|") {
		parts := strings.Split(trimmed, "|")
		if len(parts) >= 2 && strings.TrimSpace(parts[1]) != "" && strings.TrimSpace(parts[1]) != "0" {
			lookupToken = strings.TrimSpace(parts[1])
		} else {
			return playerID
		}
	}
	likePattern := "%|" + escapeLike(strings.ToLower(lookupToken)) + "|%"
	var resolved string
	if err := s.db.QueryRowContext(ctx, `
		SELECT p.player_id
		FROM players p
		LEFT JOIN player_matchups m ON p.player_id = m.player_id
		WHERE LOWER(p.player_id) LIKE ? ESCAPE '\'
		GROUP BY p.player_id
		ORDER BY COALESCE(SUM(m.total_matches), 0) DESC, p.last_seen_at DESC, p.player_id ASC
		LIMIT 1;
	`, likePattern).Scan(&resolved); err == nil && resolved != "" {
		return resolved
	}
	if !strings.Contains(trimmed, "|") {
		if err := s.db.QueryRowContext(ctx, `
			SELECT p.player_id
			FROM players p
			LEFT JOIN player_matchups m ON p.player_id = m.player_id
			WHERE p.player_name_lower = ?
			GROUP BY p.player_id
			ORDER BY COALESCE(SUM(m.total_matches), 0) DESC, p.last_seen_at DESC, p.player_id ASC
			LIMIT 1;
		`, strings.ToLower(trimmed)).Scan(&resolved); err == nil && resolved != "" {
			return resolved
		}
	}
	// Fallback: resolve local player aliases ("me", "you", raw OnlineID, or local display name) against match_players and auth_state.
	if strings.EqualFold(trimmed, "me") || strings.EqualFold(trimmed, "you") {
		if err := s.db.QueryRowContext(ctx, `
			SELECT player_id
			FROM match_players
			WHERE is_local = 1 AND player_id != '' AND player_id != '0'
			GROUP BY player_id
			ORDER BY COUNT(*) DESC, player_id ASC
			LIMIT 1;
		`).Scan(&resolved); err == nil && resolved != "" {
			return resolved
		}
		if err := s.db.QueryRowContext(ctx, `
			SELECT account_id
			FROM auth_state
			WHERE account_id != ''
			LIMIT 1;
		`).Scan(&resolved); err == nil && resolved != "" {
			return resolved
		}
	}
	if err := s.db.QueryRowContext(ctx, `
		SELECT player_id
		FROM match_players
		WHERE LOWER(player_id) = ? OR LOWER(player_id) LIKE ? ESCAPE '\'
		GROUP BY player_id
		ORDER BY MAX(is_local) DESC, COUNT(*) DESC, player_id ASC
		LIMIT 1;
	`, strings.ToLower(trimmed), likePattern).Scan(&resolved); err == nil && resolved != "" {
		return resolved
	}
	if !strings.Contains(trimmed, "|") {
		var authAcc string
		if err := s.db.QueryRowContext(ctx, `
			SELECT account_id
			FROM auth_state
			WHERE LOWER(display_name) = ? AND account_id != ''
			LIMIT 1;
		`, strings.ToLower(trimmed)).Scan(&authAcc); err == nil && authAcc != "" {
			if err := s.db.QueryRowContext(ctx, `
				SELECT player_id
				FROM match_players
				WHERE is_local = 1 AND player_id != '' AND player_id != '0'
				GROUP BY player_id
				ORDER BY COUNT(*) DESC, player_id ASC
				LIMIT 1;
			`).Scan(&resolved); err == nil && resolved != "" {
				return resolved
			}
			return authAcc
		}
	}
	return playerID
}

// resolvePlayerIDForMatch resolves an incoming player identifier to an existing canonical PlayerID
// in match_players (for matchGUID) or players so that PsyNet ("PS4|<id>|0"), .replay ("PlayStation|<id>|0"),
// and Ballchasing ("PlayStation|<name>|0") converge on a single PlayerID.
func (s *SQLiteStore) resolvePlayerIDForMatch(ctx context.Context, matchGUID, rawOnlineID, platform, playerName string, teamNum int, defaultComposite bool) string {
	pid := strings.TrimSpace(rawOnlineID)
	name := strings.TrimSpace(playerName)
	plat := strings.TrimSpace(platform)
	if plat == "" {
		plat = "Unknown"
	}
	if pid == "" || pid == "0" || strings.HasSuffix(pid, "|0|0") {
		if name == "" {
			return pid
		}
		pid = name
	}

	// Check exact match for raw pid first
	if matchGUID != "" {
		var rawMP string
		if err := s.db.QueryRowContext(ctx, "SELECT player_id FROM match_players WHERE match_guid = ? AND player_id = ? LIMIT 1;", matchGUID, pid).Scan(&rawMP); err == nil && rawMP != "" {
			return rawMP
		}
	}
	var rawP string
	if err := s.db.QueryRowContext(ctx, "SELECT player_id FROM players WHERE player_id = ? LIMIT 1;", pid).Scan(&rawP); err == nil && rawP != "" {
		return rawP
	}

	candidateID := pid
	onlineToken := pid
	if strings.Contains(pid, "|") {
		parts := strings.Split(pid, "|")
		if len(parts) >= 2 {
			if strings.TrimSpace(parts[0]) != "" && (plat == "" || plat == "Unknown") {
				plat = strings.TrimSpace(parts[0])
			}
			onlineToken = strings.TrimSpace(parts[1])
		}
	} else if defaultComposite {
		candidateID = fmt.Sprintf("%s|%s|0", plat, pid)
	}

	// 1. Exact match in match_players for this match or in players table
	if candidateID != pid {
		if matchGUID != "" {
			var exactMP string
			if err := s.db.QueryRowContext(ctx, "SELECT player_id FROM match_players WHERE match_guid = ? AND player_id = ? LIMIT 1;", matchGUID, candidateID).Scan(&exactMP); err == nil && exactMP != "" {
				return exactMP
			}
		}
		var exactP string
		if err := s.db.QueryRowContext(ctx, "SELECT player_id FROM players WHERE player_id = ? LIMIT 1;", candidateID).Scan(&exactP); err == nil && exactP != "" {
			return exactP
		}
	}

	// 2. Same OnlineID middle segment in match_players for this match or in players table
	if onlineToken != "" && onlineToken != "0" {
		likePattern := "%|" + escapeLike(strings.ToLower(onlineToken)) + "|%"
		if matchGUID != "" {
			var mpMatch string
			if teamNum >= 0 {
				_ = s.db.QueryRowContext(ctx, `
					SELECT player_id FROM match_players
					WHERE match_guid = ? AND team_num = ? AND LOWER(player_id) LIKE ? ESCAPE '\'
					LIMIT 1;
				`, matchGUID, teamNum, likePattern).Scan(&mpMatch)
			} else {
				_ = s.db.QueryRowContext(ctx, `
					SELECT player_id FROM match_players
					WHERE match_guid = ? AND LOWER(player_id) LIKE ? ESCAPE '\'
					LIMIT 1;
				`, matchGUID, likePattern).Scan(&mpMatch)
			}
			if mpMatch != "" {
				return mpMatch
			}
		}
		var existingID, existingPlat string
		if err := s.db.QueryRowContext(ctx, `
			SELECT p.player_id, p.platform
			FROM players p
			LEFT JOIN player_matchups m ON p.player_id = m.player_id
			WHERE LOWER(p.player_id) LIKE ? ESCAPE '\'
			GROUP BY p.player_id
			ORDER BY COALESCE(SUM(m.total_matches), 0) DESC, p.last_seen_at DESC, p.player_id ASC
			LIMIT 1;
		`, likePattern).Scan(&existingID, &existingPlat); err == nil && existingID != "" {
			if SamePlatformFamily(existingPlat, plat) {
				return existingID
			}
		}
	}

	// 3. Same player name on same team in this match (e.g. Ballchasing PlayStation|<name>|0 vs PS4|<numeric>|0)
	lookupName := name
	if lookupName == "" && onlineToken != "" {
		lookupName = onlineToken
	}
	if lookupName != "" && matchGUID != "" {
		var mpID, mpPlat string
		if teamNum >= 0 {
			_ = s.db.QueryRowContext(ctx, `
				SELECT mp.player_id, COALESCE(p.platform, '')
				FROM match_players mp
				JOIN players p ON mp.player_id = p.player_id
				WHERE mp.match_guid = ? AND mp.team_num = ? AND p.player_name_lower = ?
				LIMIT 1;
			`, matchGUID, teamNum, strings.ToLower(lookupName)).Scan(&mpID, &mpPlat)
		} else {
			_ = s.db.QueryRowContext(ctx, `
				SELECT mp.player_id, COALESCE(p.platform, '')
				FROM match_players mp
				JOIN players p ON mp.player_id = p.player_id
				WHERE mp.match_guid = ? AND p.player_name_lower = ?
				LIMIT 1;
			`, matchGUID, strings.ToLower(lookupName)).Scan(&mpID, &mpPlat)
		}
		if mpID != "" && SamePlatformFamily(mpPlat, plat) {
			return mpID
		}
	}

	// 4. Console player whose onlineToken is just their display name: resolve against existing same-platform-family player by name
	if name != "" && strings.EqualFold(onlineToken, name) {
		var pID, pPlat string
		if err := s.db.QueryRowContext(ctx, `
			SELECT p.player_id, p.platform
			FROM players p
			LEFT JOIN player_matchups m ON p.player_id = m.player_id
			WHERE p.player_name_lower = ?
			GROUP BY p.player_id
			ORDER BY COALESCE(SUM(m.total_matches), 0) DESC, p.last_seen_at DESC, p.player_id ASC
			LIMIT 1;
		`, strings.ToLower(name)).Scan(&pID, &pPlat); err == nil && pID != "" {
			if SamePlatformFamily(pPlat, plat) {
				return pID
			}
		}
	}

	return candidateID
}

// GetPlayer retrieves a single player profile by PlayerID. Returns ErrPlayerNotFound if not found.
func (s *SQLiteStore) GetPlayer(ctx context.Context, playerID string) (*PlayerRecord, error) {
	if strings.TrimSpace(playerID) == "" {
		return nil, ErrPlayerNotFound
	}
	playerID = s.resolveCanonicalPlayerID(ctx, playerID)

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

// isLocalPlayerID checks whether playerID belongs to the local player (via match_players.is_local or auth_state).
func (s *SQLiteStore) isLocalPlayerID(ctx context.Context, playerID string) bool {
	trimmed := strings.TrimSpace(playerID)
	if trimmed == "" {
		return false
	}
	if strings.EqualFold(trimmed, "me") || strings.EqualFold(trimmed, "you") {
		return true
	}
	localAccounts := s.getLocalAccountIDs(ctx)
	lower := strings.ToLower(trimmed)
	if localAccounts[lower] {
		return true
	}
	if strings.Contains(lower, "|") {
		parts := strings.Split(lower, "|")
		if len(parts) >= 2 && strings.TrimSpace(parts[1]) != "" && localAccounts[strings.TrimSpace(parts[1])] {
			return true
		}
	}
	var isLocCount int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM match_players WHERE is_local = 1 AND LOWER(player_id) = ? LIMIT 1;`, lower).Scan(&isLocCount)
	return isLocCount > 0
}

// GetLocalPlayerProfile synthesizes a PlayerRecord and per-playlist PlayerMatchup slice for the local player
// from match_players, matches, mmr_history, and auth_state without adding the local player to the players directory table.
func (s *SQLiteStore) GetLocalPlayerProfile(ctx context.Context, playerID string) (*PlayerRecord, []*PlayerMatchup, error) {
	trimmed := strings.TrimSpace(playerID)
	if trimmed == "" {
		return nil, nil, ErrPlayerNotFound
	}
	resolvedID := s.resolveCanonicalPlayerID(ctx, trimmed)
	if !s.isLocalPlayerID(ctx, trimmed) && !s.isLocalPlayerID(ctx, resolvedID) {
		return nil, nil, ErrPlayerNotFound
	}

	canonicalID := resolvedID
	if strings.EqualFold(canonicalID, "me") || strings.EqualFold(canonicalID, "you") || canonicalID == "" {
		var mpID string
		if err := s.db.QueryRowContext(ctx, `
			SELECT player_id
			FROM match_players
			WHERE is_local = 1 AND player_id != '' AND player_id != '0'
			GROUP BY player_id
			ORDER BY COUNT(*) DESC, player_id ASC
			LIMIT 1;
		`).Scan(&mpID); err == nil && mpID != "" {
			canonicalID = mpID
		} else if err := s.db.QueryRowContext(ctx, `
			SELECT account_id
			FROM auth_state
			WHERE account_id != ''
			LIMIT 1;
		`).Scan(&mpID); err == nil && mpID != "" {
			canonicalID = mpID
		} else {
			return nil, nil, ErrPlayerNotFound
		}
	}

	platform := "Unknown"
	if strings.Contains(canonicalID, "|") {
		parts := strings.Split(canonicalID, "|")
		if strings.TrimSpace(parts[0]) != "" {
			platform = strings.TrimSpace(parts[0])
		}
	}

	var displayName, provider string
	_ = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(display_name, ''), COALESCE(provider, '')
		FROM auth_state
		WHERE display_name != '' OR account_id != ''
		ORDER BY updated_at DESC
		LIMIT 1;
	`).Scan(&displayName, &provider)
	displayName = strings.TrimSpace(displayName)
	if platform == "Unknown" && provider != "" {
		if strings.EqualFold(provider, "epic") {
			platform = "Epic"
		} else if strings.EqualFold(provider, "steam") {
			platform = "Steam"
		}
	}

	if displayName == "" {
		// Look for local player's display name in recent match_players.stats_json or matches.replay_stats_json
		nameRows, err := s.db.QueryContext(ctx, `
			SELECT mp.stats_json, m.replay_stats_json
			FROM match_players mp
			JOIN matches m ON mp.match_guid = m.match_guid
			WHERE mp.is_local = 1 OR mp.player_id = ?
			ORDER BY m.record_start_timestamp DESC
			LIMIT 25;
		`, canonicalID)
		if err == nil {
			for nameRows.Next() {
				var stJSON, repJSON string
				if err := nameRows.Scan(&stJSON, &repJSON); err != nil {
					continue
				}
				if stJSON != "" {
					var rps ReplayPlayerStats
					if json.Unmarshal([]byte(stJSON), &rps) == nil {
						cand := strings.TrimSpace(rps.Name)
						if cand != "" && !strings.EqualFold(cand, "You") {
							displayName = cand
							if platform == "Unknown" && strings.TrimSpace(rps.Platform) != "" {
								platform = strings.TrimSpace(rps.Platform)
							}
							break
						}
					}
				}
				if repJSON != "" {
					var rms ReplayMatchStats
					if json.Unmarshal([]byte(repJSON), &rms) == nil {
						for _, p := range rms.Players {
							if p.IsLocal || strings.EqualFold(p.OnlineID, canonicalID) || (strings.Contains(canonicalID, "|") && strings.EqualFold(p.OnlineID, strings.Split(canonicalID, "|")[1])) {
								cand := strings.TrimSpace(p.Name)
								if cand != "" && !strings.EqualFold(cand, "You") {
									displayName = cand
									if platform == "Unknown" && strings.TrimSpace(p.Platform) != "" {
										platform = strings.TrimSpace(p.Platform)
									}
									break
								}
							}
						}
						if displayName != "" {
							break
						}
					}
				}
			}
			nameRows.Close()
		}
	}
	if displayName == "" {
		displayName = "You"
	}

	var firstSeenUnix, lastSeenUnix int64
	_ = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(MIN(m.record_start_timestamp), 0), COALESCE(MAX(m.record_start_timestamp), 0)
		FROM match_players mp
		JOIN matches m ON mp.match_guid = m.match_guid
		WHERE (mp.is_local = 1 OR mp.player_id = ?) AND m.record_start_timestamp > 0;
	`, canonicalID).Scan(&firstSeenUnix, &lastSeenUnix)
	now := time.Now().UTC()
	firstSeen := now
	lastSeen := now
	if firstSeenUnix > 0 {
		firstSeen = time.Unix(firstSeenUnix, 0).UTC()
	}
	if lastSeenUnix > 0 {
		lastSeen = time.Unix(lastSeenUnix, 0).UTC()
	}

	// Aggregate per-playlist matchups and fallback ranks from match_players + matches
	type plAgg struct {
		wins       int
		losses     int
		total      int
		lastPlayed int64
		tier       int
		division   int
		rankName   string
	}
	byPlaylist := make(map[int]*plAgg)

	mRows, err := s.db.QueryContext(ctx, `
		SELECT m.playlist, m.record_start_timestamp, m.blue_score, m.orange_score, m.winner_team,
		       mp.team_num, mp.won, mp.stats_json
		FROM match_players mp
		JOIN matches m ON mp.match_guid = m.match_guid
		WHERE mp.is_local = 1 OR mp.player_id = ?
		ORDER BY m.record_start_timestamp DESC;
	`, canonicalID)
	if err == nil {
		for mRows.Next() {
			var (
				playlist   int
				startTS    int64
				blueScore  sql.NullInt64
				orangeSc   sql.NullInt64
				winnerTeam sql.NullInt64
				teamNum    int
				wonVal     sql.NullInt64
				statsJSON  string
			)
			if err := mRows.Scan(&playlist, &startTS, &blueScore, &orangeSc, &winnerTeam, &teamNum, &wonVal, &statsJSON); err != nil {
				continue
			}
			if playlist <= 0 {
				continue
			}
			agg, ok := byPlaylist[playlist]
			if !ok {
				agg = &plAgg{}
				byPlaylist[playlist] = agg
			}
			if startTS > agg.lastPlayed {
				agg.lastPlayed = startTS
			}
			var won, lost bool
			if wonVal.Valid {
				if wonVal.Int64 == 1 {
					won = true
				} else {
					lost = true
				}
			} else if winnerTeam.Valid && winnerTeam.Int64 >= 0 {
				if int(winnerTeam.Int64) == teamNum {
					won = true
				} else {
					lost = true
				}
			} else if blueScore.Int64 != orangeSc.Int64 {
				if (teamNum == 0 && blueScore.Int64 > orangeSc.Int64) || (teamNum == 1 && orangeSc.Int64 > blueScore.Int64) {
					won = true
				} else {
					lost = true
				}
			}
			if won {
				agg.wins++
				agg.total++
			} else if lost {
				agg.losses++
				agg.total++
			}
			if agg.tier == 0 && statsJSON != "" {
				var rps ReplayPlayerStats
				if json.Unmarshal([]byte(statsJSON), &rps) == nil && rps.Rank != nil && rps.Rank.Tier > 0 {
					agg.tier = rps.Rank.Tier
					agg.division = rps.Rank.Division
					agg.rankName = rps.Rank.Name
				}
			}
		}
		mRows.Close()
	}

	matchups := make([]*PlayerMatchup, 0, len(byPlaylist))
	for pid, agg := range byPlaylist {
		if agg.total <= 0 {
			continue
		}
		lp := lastSeen
		if agg.lastPlayed > 0 {
			lp = time.Unix(agg.lastPlayed, 0).UTC()
		}
		matchups = append(matchups, &PlayerMatchup{
			PlayerID:         canonicalID,
			PlaylistID:       pid,
			WinsAsTeammate:   agg.wins,
			LossesAsTeammate: agg.losses,
			WinsAsOpponent:   0,
			LossesAsOpponent: 0,
			TotalMatches:     agg.total,
			LastPlayedAt:     lp,
		})
	}
	sort.Slice(matchups, func(i, j int) bool {
		if matchups[i].TotalMatches != matchups[j].TotalMatches {
			return matchups[i].TotalMatches > matchups[j].TotalMatches
		}
		return matchups[i].PlaylistID < matchups[j].PlaylistID
	})

	// Build RanksJSON from mmr_history + per-playlist fallback ranks
	ranksMap := make(map[string]map[string]interface{})
	mmrRows, err := s.db.QueryContext(ctx, `
		SELECT playlist_id, mmr, tier, division
		FROM mmr_history
		WHERE id IN (
			SELECT MAX(id) FROM mmr_history GROUP BY playlist_id
		);
	`)
	if err == nil {
		for mmrRows.Next() {
			var pid, mmr, tier, div int
			if err := mmrRows.Scan(&pid, &mmr, &tier, &div); err == nil && pid > 0 {
				effDiv := Resolve1BasedDivision(div, "")
				games := 0
				if agg, ok := byPlaylist[pid]; ok {
					games = agg.total
					if tier == 0 && agg.tier > 0 {
						tier = agg.tier
						effDiv = Resolve1BasedDivision(agg.division, agg.rankName)
					}
				}
				ranksMap[strconv.Itoa(pid)] = map[string]interface{}{
					"playlist_id":    pid,
					"playlist_name":  FormatPlaylistName(pid),
					"mmr":            mmr,
					"tier":           tier,
					"division":       effDiv,
					"rank_name":      FormatRankName(tier, effDiv),
					"short_rank":     FormatShortRankName(tier, effDiv),
					"matches_played": games,
				}
			}
		}
		mmrRows.Close()
	}
	for pid, agg := range byPlaylist {
		pKey := strconv.Itoa(pid)
		if _, exists := ranksMap[pKey]; !exists && agg.tier > 0 {
			effDiv := Resolve1BasedDivision(agg.division, agg.rankName)
			ranksMap[pKey] = map[string]interface{}{
				"playlist_id":    pid,
				"playlist_name":  FormatPlaylistName(pid),
				"tier":           agg.tier,
				"division":       effDiv,
				"rank_name":      FormatRankName(agg.tier, effDiv),
				"short_rank":     FormatShortRankName(agg.tier, effDiv),
				"matches_played": agg.total,
			}
		}
	}
	ranksJSON := "{}"
	if len(ranksMap) > 0 {
		if b, err := json.Marshal(ranksMap); err == nil {
			ranksJSON = string(b)
		}
	}

	rec := &PlayerRecord{
		PlayerID:    canonicalID,
		Platform:    platform,
		PlayerName:  displayName,
		RanksJSON:   ranksJSON,
		Notes:       "",
		Tags:        []string{},
		FirstSeenAt: firstSeen,
		LastSeenAt:  lastSeen,
	}
	return rec, matchups, nil
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

// ComputePlayerSplitStats computes WITH YOU (teammate) and AGAINST YOU (opponent, local player win/loss perspective)
// Core + Ballchasing Mechanics stats for a player, optionally scoped to playlistIDs and [fromUnix, toUnix].
func (s *SQLiteStore) ComputePlayerSplitStats(
	ctx context.Context,
	playerID, playerName string,
	playlistIDs []int,
	fromUnix, toUnix int64,
	fbTMWins, fbTMLosses, fbOppWins, fbOppLosses int,
) (*PlayerRoleStatsSummary, *PlayerRoleStatsSummary) {
	resolvedID := s.resolveCanonicalPlayerID(ctx, playerID)
	if resolvedID == "" {
		resolvedID = strings.TrimSpace(playerID)
	}

	if fromUnix > 0 || toUnix > 0 {
		var hasAnyMatchPlayer int
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM match_players WHERE player_id = ? LIMIT 1;", resolvedID).Scan(&hasAnyMatchPlayer); err == nil && hasAnyMatchPlayer > 0 {
			fbTMWins, fbTMLosses, fbOppWins, fbOppLosses = 0, 0, 0, 0
		}
	}

	var whereClauses []string
	var args []interface{}
	if s.isLocalPlayerID(ctx, resolvedID) {
		whereClauses = append(whereClauses, "(mp.player_id = ? OR mp.is_local = 1)")
		args = append(args, resolvedID)
	} else {
		whereClauses = append(whereClauses, "mp.player_id = ?")
		args = append(args, resolvedID)
	}

	if len(playlistIDs) > 0 {
		placeholders := make([]string, len(playlistIDs))
		for i, pid := range playlistIDs {
			placeholders[i] = "?"
			args = append(args, pid)
		}
		whereClauses = append(whereClauses, fmt.Sprintf("m.playlist IN (%s)", strings.Join(placeholders, ",")))
	}
	if fromUnix > 0 {
		whereClauses = append(whereClauses, "m.record_start_timestamp >= ?")
		args = append(args, fromUnix)
	}
	if toUnix > 0 {
		whereClauses = append(whereClauses, "m.record_start_timestamp <= ?")
		args = append(args, toUnix)
	}

	querySQL := fmt.Sprintf(`
	SELECT mp.team_num, mp.is_local, mp.won, mp.score, mp.goals, mp.assists, mp.saves, mp.shots, mp.demos, mp.stats_json,
	       m.blue_score, m.orange_score, m.winner_team, m.replay_stats_json,
	       COALESCE((SELECT lp.team_num FROM match_players lp WHERE lp.match_guid = m.match_guid AND lp.is_local = 1 LIMIT 1), -1) AS local_team
	FROM match_players mp
	JOIN matches m ON mp.match_guid = m.match_guid
	WHERE %s
	ORDER BY m.record_start_timestamp DESC;
	`, strings.Join(whereClauses, " AND "))

	var withAcc, againstAcc RoleStatsAccumulator
	rows, err := s.db.QueryContext(ctx, querySQL, args...)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var (
				teamNum     int
				isLocal     int
				wonVal      sql.NullInt64
				score       int
				goals       int
				assists     int
				saves       int
				shots       int
				demos       int
				statsJSON   string
				blueScore   sql.NullInt64
				orangeScore sql.NullInt64
				winnerTeam  sql.NullInt64
				replayStats sql.NullString
				localTeam   int
			)
			if scanErr := rows.Scan(
				&teamNum, &isLocal, &wonVal, &score, &goals, &assists, &saves, &shots, &demos, &statsJSON,
				&blueScore, &orangeScore, &winnerTeam, &replayStats, &localTeam,
			); scanErr != nil {
				continue
			}

			if isLocal == 1 {
				localTeam = teamNum
			}
			if localTeam < 0 {
				localTeam = 0
			}
			isTeammate := (teamNum == localTeam)

			var localWon bool
			isDraw := false
			if winnerTeam.Valid && winnerTeam.Int64 >= 0 {
				localWon = (int(winnerTeam.Int64) == localTeam)
			} else if blueScore.Int64 != orangeScore.Int64 {
				localWon = (localTeam == 0 && blueScore.Int64 > orangeScore.Int64) ||
					(localTeam == 1 && orangeScore.Int64 > blueScore.Int64)
			} else if wonVal.Valid {
				if isTeammate {
					localWon = (wonVal.Int64 == 1)
				} else {
					localWon = (wonVal.Int64 == 0)
				}
			} else {
				isDraw = true
			}

			resolvedStatsJSON := ExtractPlayerStatsJSONFromReplay(statsJSON, replayStats.String, resolvedID, playerName, teamNum)
			if isTeammate {
				withAcc.AddMatchOutcome(localWon, isDraw)
				withAcc.AddPlayerBoxAndTelemetry(score, goals, assists, saves, shots, demos, resolvedStatsJSON)
			} else {
				againstAcc.AddMatchOutcome(localWon, isDraw)
				againstAcc.AddPlayerBoxAndTelemetry(score, goals, assists, saves, shots, demos, resolvedStatsJSON)
			}
		}
	}

	return withAcc.Finalize(fbTMWins, fbTMLosses), againstAcc.Finalize(fbOppWins, fbOppLosses)
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
		PopulatePlayerSummaryComputedFields(&sRec)
		summaries = append(summaries, &sRec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error in list player summaries: %w", err)
	}
	rows.Close()

	for _, sRec := range summaries {
		withSt, againstSt := s.ComputePlayerSplitStats(
			ctx, sRec.PlayerID, sRec.PlayerName, nil, 0, 0,
			sRec.TotalWinsAsTeammate, sRec.TotalLossesAsTeammate,
			sRec.TotalWinsAsOpponent, sRec.TotalLossesAsOpponent,
		)
		sRec.WithYouStats = withSt
		sRec.AgainstYouStats = againstSt
		if withSt.Games+againstSt.Games > sRec.TotalMatches {
			sRec.TotalWinsAsTeammate = withSt.Wins
			sRec.TotalLossesAsTeammate = withSt.Losses
			sRec.TotalWinsAsOpponent = againstSt.Wins
			sRec.TotalLossesAsOpponent = againstSt.Losses
			sRec.TotalMatches = withSt.Games + againstSt.Games
		}
		PopulatePlayerSummaryComputedFields(sRec)
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
	playlistIDs := ParsePlaylistFilter(filter.Playlist)
	hasScopeFilter := len(playlistIDs) > 0 || filter.FromUnix > 0 || filter.ToUnix > 0

	var pWhereClauses []string
	var pWhereArgs []interface{}
	pWhereClauses = append(pWhereClauses, "NOT EXISTS (SELECT 1 FROM match_players mp_loc WHERE mp_loc.player_id = p.player_id AND mp_loc.is_local = 1)")

	if cleanPlatform != "" && cleanPlatform != "all" {
		switch cleanPlatform {
		case "playstation", "ps4", "psn", "psynet":
			pWhereClauses = append(pWhereClauses, "LOWER(p.platform) IN ('playstation', 'ps4', 'psn', 'psynet')")
		case "xbox", "xboxone":
			pWhereClauses = append(pWhereClauses, "LOWER(p.platform) IN ('xbox', 'xboxone')")
		case "switch", "nintendo":
			pWhereClauses = append(pWhereClauses, "LOWER(p.platform) IN ('switch', 'nintendo')")
		default:
			pWhereClauses = append(pWhereClauses, "LOWER(p.platform) = ?")
			pWhereArgs = append(pWhereArgs, cleanPlatform)
		}
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

	var mJoinClauses []string
	var mJoinArgs []interface{}
	mJoinClauses = append(mJoinClauses, "p.player_id = m.player_id")
	if len(playlistIDs) > 0 {
		placeholders := make([]string, len(playlistIDs))
		for i, pid := range playlistIDs {
			placeholders[i] = "?"
			mJoinArgs = append(mJoinArgs, pid)
		}
		mJoinClauses = append(mJoinClauses, fmt.Sprintf("m.playlist_id IN (%s)", strings.Join(placeholders, ",")))
	}
	if filter.FromUnix > 0 {
		mJoinClauses = append(mJoinClauses, "m.last_played_at >= ?")
		mJoinArgs = append(mJoinArgs, filter.FromUnix)
	}
	if filter.ToUnix > 0 {
		mJoinClauses = append(mJoinClauses, "m.last_played_at <= ?")
		mJoinArgs = append(mJoinArgs, filter.ToUnix)
	}

	baseSQL := fmt.Sprintf(`
	SELECT p.player_id, p.platform, p.player_name, p.ranks_json, p.notes, p.tags, p.first_seen_at, p.last_seen_at,
	       COALESCE(SUM(m.wins_as_teammate), 0) AS total_wins_teammate,
	       COALESCE(SUM(m.losses_as_teammate), 0) AS total_losses_teammate,
	       COALESCE(SUM(m.wins_as_opponent), 0) AS total_wins_opponent,
	       COALESCE(SUM(m.losses_as_opponent), 0) AS total_losses_opponent,
	       COALESCE(SUM(m.total_matches), 0) AS total_matches
	FROM players p
	LEFT JOIN player_matchups m ON %s
	%s
	GROUP BY p.player_id;
	`, strings.Join(mJoinClauses, " AND "), pWhereSQL)

	allArgs := append(mJoinArgs, pWhereArgs...)
	rows, err := s.db.QueryContext(ctx, baseSQL, allArgs...)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, 0, wrapContextErr(ctxErr)
		}
		return nil, 0, fmt.Errorf("failed to query player summaries: %w", err)
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
				return nil, 0, wrapContextErr(ctxErr)
			}
			return nil, 0, fmt.Errorf("failed to scan player summary row: %w", err)
		}
		sRec.Notes = notes.String
		sRec.Tags = parseTags(tagsStr.String)
		sRec.FirstSeenAt = time.Unix(firstSeen, 0).UTC()
		sRec.LastSeenAt = time.Unix(lastSeen, 0).UTC()
		candidates = append(candidates, &sRec)
	}
	if err := rows.Err(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, 0, wrapContextErr(ctxErr)
		}
		return nil, 0, fmt.Errorf("rows iteration error in filter player summaries: %w", err)
	}
	rows.Close()

	matching := make([]*PlayerSummary, 0, len(candidates))
	for _, sRec := range candidates {
		withSt, againstSt := s.ComputePlayerSplitStats(
			ctx, sRec.PlayerID, sRec.PlayerName, playlistIDs, filter.FromUnix, filter.ToUnix,
			sRec.TotalWinsAsTeammate, sRec.TotalLossesAsTeammate,
			sRec.TotalWinsAsOpponent, sRec.TotalLossesAsOpponent,
		)
		sRec.WithYouStats = withSt
		sRec.AgainstYouStats = againstSt
		if hasScopeFilter || withSt.Games+againstSt.Games > sRec.TotalMatches {
			sRec.TotalWinsAsTeammate = withSt.Wins
			sRec.TotalLossesAsTeammate = withSt.Losses
			sRec.TotalWinsAsOpponent = againstSt.Wins
			sRec.TotalLossesAsOpponent = againstSt.Losses
			if withSt.Games+againstSt.Games > 0 || filter.FromUnix > 0 || filter.ToUnix > 0 || withSt.Games+againstSt.Games > sRec.TotalMatches {
				sRec.TotalMatches = withSt.Games + againstSt.Games
			}
		}

		if hasScopeFilter && sRec.TotalMatches <= 0 {
			continue
		}
		if !MatchesRoleFilter(sRec, filter) {
			continue
		}
		PopulatePlayerSummaryComputedFields(sRec)
		matching = append(matching, sRec)
	}

	results, total := SortAndPaginatePlayerSummaries(matching, filter, playlistIDs)

	return results, total, nil
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
	playerID = s.resolveCanonicalPlayerID(ctx, playerID)
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

	localAccounts := s.getLocalAccountIDs(ctx)
	resolvedOutcomes := make([]PlayerOutcome, 0, len(outcomes))
	for _, outcome := range outcomes {
		rawPID := strings.TrimSpace(outcome.PlayerID)
		if rawPID == "" || rawPID == "0" || strings.HasSuffix(rawPID, "|0|0") {
			continue
		}
		resolvedPID := s.resolvePlayerIDForMatch(ctx, matchGUID, rawPID, outcome.Platform, outcome.PlayerName, -1, false)
		if localAccounts[strings.ToLower(rawPID)] || localAccounts[strings.ToLower(resolvedPID)] {
			continue
		}
		outcome.PlayerID = resolvedPID
		resolvedOutcomes = append(resolvedOutcomes, outcome)
	}
	outcomes = resolvedOutcomes

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
		platform = CASE WHEN players.platform != '' AND players.platform != 'Unknown' THEN players.platform WHEN excluded.platform != '' THEN excluded.platform ELSE players.platform END,
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
	resolvedID := s.resolveCanonicalPlayerID(ctx, playerID)

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

	err := s.db.QueryRowContext(ctx, query, resolvedID, playlistID).Scan(
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
		// Fallback to computing matchup directly from match_players + matches if not in player_matchups
		const fallbackQuery = `
		SELECT
			COALESCE(SUM(CASE WHEN mp.team_num = lp.team_num AND lp.team_num = m.winner_team THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN mp.team_num = lp.team_num AND lp.team_num != m.winner_team THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN mp.team_num != lp.team_num AND lp.team_num = m.winner_team THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN mp.team_num != lp.team_num AND lp.team_num != m.winner_team THEN 1 ELSE 0 END), 0),
			COUNT(*),
			COALESCE(MAX(m.record_start_timestamp), 0)
		FROM match_players mp
		JOIN matches m ON mp.match_guid = m.match_guid
		JOIN (
			SELECT match_guid, MIN(team_num) AS team_num
			FROM match_players
			WHERE is_local = 1
			GROUP BY match_guid
		) lp ON lp.match_guid = m.match_guid
		WHERE mp.player_id = ?
		  AND m.playlist = ?
		  AND m.winner_team >= 0
		  AND mp.is_local = 0;
		`
		var fb PlayerMatchup
		var fbLast int64
		if fbErr := s.db.QueryRowContext(ctx, fallbackQuery, resolvedID, playlistID).Scan(
			&fb.WinsAsTeammate,
			&fb.LossesAsTeammate,
			&fb.WinsAsOpponent,
			&fb.LossesAsOpponent,
			&fb.TotalMatches,
			&fbLast,
		); fbErr == nil && fb.TotalMatches > 0 {
			fb.PlayerID = resolvedID
			fb.PlaylistID = playlistID
			if fbLast > 0 {
				fb.LastPlayedAt = time.Unix(fbLast, 0).UTC()
			}
			return &fb, nil
		}

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
	playerID = s.resolveCanonicalPlayerID(ctx, playerID)

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

	if len(matchups) == 0 {
		const fallbackQuery = `
		SELECT
			mp.player_id,
			m.playlist,
			COALESCE(SUM(CASE WHEN mp.team_num = lp.team_num AND lp.team_num = m.winner_team THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN mp.team_num = lp.team_num AND lp.team_num != m.winner_team THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN mp.team_num != lp.team_num AND lp.team_num = m.winner_team THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN mp.team_num != lp.team_num AND lp.team_num != m.winner_team THEN 1 ELSE 0 END), 0),
			COUNT(*),
			COALESCE(MAX(m.record_start_timestamp), 0)
		FROM match_players mp
		JOIN matches m ON mp.match_guid = m.match_guid
		JOIN (
			SELECT match_guid, MIN(team_num) AS team_num
			FROM match_players
			WHERE is_local = 1
			GROUP BY match_guid
		) lp ON lp.match_guid = m.match_guid
		WHERE mp.player_id = ?
		  AND m.winner_team >= 0
		  AND mp.is_local = 0
		GROUP BY mp.player_id, m.playlist
		ORDER BY m.playlist ASC;
		`
		fbRows, fbErr := s.db.QueryContext(ctx, fallbackQuery, playerID)
		if fbErr == nil {
			defer fbRows.Close()
			for fbRows.Next() {
				var (
					m          PlayerMatchup
					lastPlayed int64
				)
				if err := fbRows.Scan(
					&m.PlayerID,
					&m.PlaylistID,
					&m.WinsAsTeammate,
					&m.LossesAsTeammate,
					&m.WinsAsOpponent,
					&m.LossesAsOpponent,
					&m.TotalMatches,
					&lastPlayed,
				); err == nil {
					if lastPlayed > 0 {
						m.LastPlayedAt = time.Unix(lastPlayed, 0).UTC()
					}
					matchups = append(matchups, &m)
				}
			}
		}
	}

	return matchups, nil
}

// SyncReplayStats enriches or upserts a match record with end-of-game replay statistics,
// keyed by MatchGUID, and populates player records and match_players.
func (s *SQLiteStore) SyncReplayStats(ctx context.Context, stats *ReplayMatchStats) error {
	if stats == nil || strings.TrimSpace(stats.MatchGUID) == "" {
		return ErrInvalidGUID
	}

	st := *stats
	guid := s.resolveExistingMatchGUID(ctx, st.MatchGUID, st.BallchasingID)
	st.MatchGUID = guid
	stats.MatchGUID = guid
	if len(stats.Players) > 0 {
		playersCopy := make([]ReplayPlayerStats, len(stats.Players))
		copy(playersCopy, stats.Players)
		st.Players = playersCopy
	}

	// Fallback score calculation from player goals if top-level team scores are 0-0
	if st.BlueScore == 0 && st.OrangeScore == 0 && len(st.Players) > 0 {
		blueGoals, orangeGoals := 0, 0
		for _, p := range st.Players {
			if p.Team == 0 {
				blueGoals += p.Goals
			} else if p.Team == 1 {
				orangeGoals += p.Goals
			}
		}
		if blueGoals > 0 || orangeGoals > 0 {
			st.BlueScore = blueGoals
			st.OrangeScore = orangeGoals
		}
	}
	if st.WinnerTeam == nil || *st.WinnerTeam < 0 {
		if st.BlueScore > st.OrangeScore {
			w := 0
			st.WinnerTeam = &w
		} else if st.OrangeScore > st.BlueScore {
			w := 1
			st.WinnerTeam = &w
		}
	}
	st.BallchasingURL = NormalizeBallchasingURL(st.BallchasingID, st.BallchasingURL)

	now := time.Now().UTC()
	nowUnix := now.Unix()

	winnerVal := int64(-1)
	if st.WinnerTeam != nil {
		winnerVal = int64(*st.WinnerTeam)
	}

	startTS := nowUnix
	if st.Date != "" {
		if t, ok := ParseReplayDate(st.Date); ok {
			startTS = t.Unix()
		}
	}
	if startTS == nowUnix {
		var existingTS int64
		if err := s.db.QueryRowContext(ctx, "SELECT record_start_timestamp FROM matches WHERE match_guid = ?;", guid).Scan(&existingTS); err == nil && existingTS > 0 {
			startTS = existingTS
		}
	}

	// Resolve all players and local status before marshaling replay_stats_json
	localAccounts := s.getLocalAccountIDs(ctx)
	matchPlayers := make([]*MatchPlayerRecord, 0, len(st.Players))
	matchTime := time.Unix(startTS, 0).UTC()

	for i := range st.Players {
		p := &st.Players[i]
		pid := strings.TrimSpace(p.OnlineID)
		if (pid == "" || pid == "0") && strings.TrimSpace(p.Name) != "" {
			pid = strings.TrimSpace(p.Name)
		}
		if pid == "" {
			continue
		}
		plat := strings.TrimSpace(p.Platform)
		if plat == "" {
			plat = "Unknown"
		}
		canonicalID := s.resolvePlayerIDForMatch(ctx, guid, pid, plat, p.Name, p.Team, true)

		isLocal := p.IsLocal || localAccounts[strings.ToLower(pid)] || localAccounts[strings.ToLower(canonicalID)]
		if isLocal {
			var existingLocalID string
			if err := s.db.QueryRowContext(ctx, "SELECT player_id FROM match_players WHERE match_guid = ? AND team_num = ? AND is_local = 1 LIMIT 1;", guid, p.Team).Scan(&existingLocalID); err == nil && existingLocalID != "" {
				canonicalID = existingLocalID
			}
		}
		p.IsLocal = isLocal

		var won *bool
		if st.WinnerTeam != nil && *st.WinnerTeam >= 0 {
			w := (*st.WinnerTeam == p.Team)
			won = &w
		}

		var statsJSONStr string
		if p.Core != nil || p.Boost != nil || p.Movement != nil || p.Positioning != nil || p.Demo != nil || p.Ball != nil || p.Camera != nil || p.Rank != nil {
			if b, mErr := json.Marshal(p); mErr == nil {
				statsJSONStr = string(b)
			}
		}

		mp := &MatchPlayerRecord{
			MatchGUID:  guid,
			PlayerID:   canonicalID,
			Platform:   plat,
			PlayerName: strings.TrimSpace(p.Name),
			TeamNum:    p.Team,
			IsLocal:    isLocal,
			Won:        won,
			Score:      p.Score,
			Goals:      p.Goals,
			Assists:    p.Assists,
			Saves:      p.Saves,
			Shots:      p.Shots,
			Demos:      p.Demos,
			CarName:    strings.TrimSpace(p.CarName),
			StatsJSON:  statsJSONStr,
		}
		matchPlayers = append(matchPlayers, mp)

		if !isLocal {
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
				if pRec.Platform == "" || pRec.Platform == "Unknown" {
					pRec.Platform = plat
				}
				if strings.TrimSpace(p.Name) != "" {
					pRec.PlayerName = strings.TrimSpace(p.Name)
				}
				if matchTime.Before(pRec.FirstSeenAt) {
					pRec.FirstSeenAt = matchTime
				}
				if matchTime.After(pRec.LastSeenAt) {
					pRec.LastSeenAt = matchTime
				}
				_ = s.UpsertPlayer(ctx, pRec)
			}
		}
	}

	statsJSON, err := json.Marshal(&st)
	if err != nil {
		return fmt.Errorf("failed to marshal replay stats: %w", err)
	}

	dlStatus := DownloadDownloaded
	upStatus := UploadPending
	statsSyncStatus := StatsSyncPending
	if st.BallchasingID != "" {
		upStatus = UploadUploaded
		statsSyncStatus = StatsSyncSynced
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
		blue_score = CASE WHEN excluded.blue_score != 0 OR excluded.orange_score != 0 THEN excluded.blue_score ELSE matches.blue_score END,
		orange_score = CASE WHEN excluded.blue_score != 0 OR excluded.orange_score != 0 THEN excluded.orange_score ELSE matches.orange_score END,
		winner_team = CASE WHEN excluded.winner_team != -1 THEN excluded.winner_team ELSE matches.winner_team END,
		replay_stats_json = excluded.replay_stats_json,
		synced_at = excluded.synced_at,
		download_status = CASE WHEN matches.download_status = 'PENDING' THEN 'DOWNLOADED' ELSE matches.download_status END,
		upload_status = CASE WHEN excluded.ballchasing_id != '' AND (matches.upload_status = '' OR matches.upload_status = 'PENDING' OR matches.upload_status = 'FAILED') THEN 'UPLOADED' ELSE matches.upload_status END,
		stats_sync_status = CASE WHEN excluded.ballchasing_id != '' THEN 'SYNCED' ELSE matches.stats_sync_status END,
		updated_at = excluded.updated_at;
	`

	_, err = s.db.ExecContext(ctx, query,
		guid, startTS, st.MapName, st.Playlist,
		dlStatus, upStatus, st.BallchasingID, st.BallchasingURL,
		st.BlueScore, st.OrangeScore, winnerVal, string(statsJSON), nowUnix,
		string(statsSyncStatus), nowUnix, nowUnix,
	)
	if err != nil {
		return fmt.Errorf("failed to sync replay stats for %s: %w", guid, err)
	}

	if len(matchPlayers) > 0 {
		_ = s.SaveMatchPlayers(ctx, guid, matchPlayers)
	}

	s.hydrateRanksFromStats(ctx, &st)

	return nil
}

// ListHistoricalMatches returns paginated historical matches matching the filter,
// ordered by record_start_timestamp DESC, match_guid ASC.
// matchMatchesSearchQuery evaluates whether a match record (or any of its participants) matches a search query string.
func (s *SQLiteStore) matchMatchesSearchQuery(ctx context.Context, mr *MatchRecord, lowerQuery string) bool {
	if lowerQuery == "" {
		return true
	}
	if strings.Contains(strings.ToLower(mr.MatchGUID), lowerQuery) ||
		strings.Contains(strings.ToLower(mr.MapName), lowerQuery) ||
		strings.Contains(strings.ToLower(FormatPlaylistName(mr.Playlist)), lowerQuery) ||
		strings.Contains(strings.ToLower(mr.ReplayStatsJSON), lowerQuery) {
		return true
	}
	const pQuery = `
	SELECT COUNT(*)
	FROM match_players mp
	LEFT JOIN players p ON mp.player_id = p.player_id
	WHERE mp.match_guid = ?
	  AND (INSTR(LOWER(mp.player_id), ?) > 0 OR INSTR(COALESCE(p.player_name_lower, ''), ?) > 0 OR INSTR(LOWER(COALESCE(mp.car_name, '')), ?) > 0);
	`
	var cnt int
	if err := s.db.QueryRowContext(ctx, pQuery, mr.MatchGUID, lowerQuery, lowerQuery, lowerQuery).Scan(&cnt); err == nil && cnt > 0 {
		return true
	}
	return false
}

// EnrichMatchStatsWithPlayers enriches a match's ReplayMatchStats with per-player Ballchasing telemetry
// from match_players.stats_json and rank/MMR metadata from players.ranks_json and mmr_history.
func (s *SQLiteStore) EnrichMatchStatsWithPlayers(ctx context.Context, matchGUID string, playlist int, mr *MatchRecord, st *ReplayMatchStats) *ReplayMatchStats {
	if st == nil && mr != nil && strings.TrimSpace(mr.ReplayStatsJSON) != "" {
		var parsed ReplayMatchStats
		if err := json.Unmarshal([]byte(mr.ReplayStatsJSON), &parsed); err == nil {
			st = &parsed
		}
	}

	const mpQuery = `
	SELECT mp.player_id, mp.team_num, mp.is_local, mp.score, mp.goals, mp.assists,
	       mp.saves, mp.shots, mp.demos, mp.car_name, mp.stats_json,
	       COALESCE(p.player_name, ''), COALESCE(p.platform, ''), COALESCE(p.ranks_json, '{}')
	FROM match_players mp
	LEFT JOIN players p ON mp.player_id = p.player_id
	WHERE mp.match_guid = ?
	ORDER BY mp.team_num ASC, mp.score DESC;
	`
	rows, err := s.db.QueryContext(ctx, mpQuery, matchGUID)
	if err != nil {
		return st
	}
	defer rows.Close()

	type enrichedMP struct {
		playerID   string
		teamNum    int
		isLocal    bool
		score      int
		goals      int
		assists    int
		saves      int
		shots      int
		demos      int
		carName    string
		statsJSON  string
		playerName string
		platform   string
		ranksJSON  string
	}
	var mps []enrichedMP
	for rows.Next() {
		var item enrichedMP
		var isLoc int
		if err := rows.Scan(
			&item.playerID, &item.teamNum, &isLoc, &item.score, &item.goals, &item.assists,
			&item.saves, &item.shots, &item.demos, &item.carName, &item.statsJSON,
			&item.playerName, &item.platform, &item.ranksJSON,
		); err == nil {
			item.isLocal = (isLoc == 1)
			mps = append(mps, item)
		}
	}
	rows.Close()

	if st == nil && len(mps) > 0 {
		st = &ReplayMatchStats{
			MatchGUID: matchGUID,
			Playlist:  playlist,
		}
		if mr != nil {
			st.MapName = mr.MapName
			st.BlueScore = mr.BlueScore
			st.OrangeScore = mr.OrangeScore
			st.WinnerTeam = mr.WinnerTeam
			st.BallchasingID = mr.BallchasingID
			st.BallchasingURL = mr.BallchasingURL
			if mr.RecordStartTimestamp > 0 {
				st.Date = time.Unix(mr.RecordStartTimestamp, 0).UTC().Format(time.RFC3339)
			}
		}
	}
	if st == nil {
		return nil
	}

	var localMMR float64
	var localTier, localDiv int
	_ = s.db.QueryRowContext(ctx,
		"SELECT mmr, tier, division FROM mmr_history WHERE match_guid = ? ORDER BY recorded_at DESC LIMIT 1;",
		matchGUID,
	).Scan(&localMMR, &localTier, &localDiv)

	matchedMP := make([]bool, len(mps))
	for i := range st.Players {
		sp := &st.Players[i]
		spOnlineLower := strings.ToLower(strings.TrimSpace(sp.OnlineID))
		spNameLower := strings.ToLower(strings.TrimSpace(sp.Name))

		var matched *enrichedMP
		for j := range mps {
			if matchedMP[j] {
				continue
			}
			mpIDLower := strings.ToLower(strings.TrimSpace(mps[j].playerID))
			mpIDToken := ""
			if parts := strings.Split(mpIDLower, "|"); len(parts) >= 2 {
				mpIDToken = strings.TrimSpace(parts[1])
			}
			if (spOnlineLower != "" && spOnlineLower != "0" && (mpIDLower == spOnlineLower || mpIDToken == spOnlineLower || strings.Contains(mpIDLower, "|"+spOnlineLower+"|") || strings.Contains(spOnlineLower, "|"+mpIDToken+"|"))) ||
				(sp.IsLocal && mps[j].isLocal && mps[j].teamNum == sp.Team) ||
				(spNameLower != "" && (strings.EqualFold(mps[j].playerName, spNameLower) || mpIDToken == spNameLower) && mps[j].teamNum == sp.Team) ||
				(mps[j].teamNum == sp.Team && mps[j].score == sp.Score && mps[j].goals == sp.Goals) {
				matched = &mps[j]
				matchedMP[j] = true
				break
			}
		}

		if matched != nil {
			if matched.isLocal {
				sp.IsLocal = true
			}
			if strings.TrimSpace(matched.statsJSON) != "" && matched.statsJSON != "{}" {
				var parsedRPS ReplayPlayerStats
				if err := json.Unmarshal([]byte(matched.statsJSON), &parsedRPS); err == nil {
					if sp.Core == nil && parsedRPS.Core != nil {
						sp.Core = parsedRPS.Core
					}
					if sp.Boost == nil && parsedRPS.Boost != nil {
						sp.Boost = parsedRPS.Boost
					}
					if sp.Movement == nil && parsedRPS.Movement != nil {
						sp.Movement = parsedRPS.Movement
					}
					if sp.Positioning == nil && parsedRPS.Positioning != nil {
						sp.Positioning = parsedRPS.Positioning
					}
					if sp.Demo == nil && parsedRPS.Demo != nil {
						sp.Demo = parsedRPS.Demo
					}
					if sp.Ball == nil && parsedRPS.Ball != nil {
						sp.Ball = parsedRPS.Ball
					}
					if sp.Camera == nil && parsedRPS.Camera != nil {
						sp.Camera = parsedRPS.Camera
					}
					if sp.Rank == nil && parsedRPS.Rank != nil {
						sp.Rank = parsedRPS.Rank
					}
				}
			}

			tier, div, mmr := ExtractPrimaryRankFromJSON(matched.ranksJSON, []int{playlist})
			if matched.isLocal && localMMR > 0 {
				mmr = localMMR
				if localTier > 0 && (tier != localTier || div == 0) {
					tier = localTier
					div = localDiv
				}
			}
			if sp.Rank != nil && sp.Rank.Tier > 0 {
				tier = sp.Rank.Tier
				div = sp.Rank.Division
			}
			if tier > 0 {
				rName := FormatRankName(tier, div)
				sName := FormatShortRankName(tier, div)
				if sp.Rank == nil {
					sp.Rank = &ballchasing.PlayerRank{
						Tier:      tier,
						Division:  div,
						Name:      rName,
						ShortRank: sName,
					}
				} else {
					sp.Rank.Name = rName
					sp.Rank.ShortRank = sName
				}
				sp.RankName = rName
				sp.ShortRank = sName
			}
			if mmr > 0 && sp.MMR == 0 {
				sp.MMR = mmr
			}
		} else if sp.Rank != nil && sp.Rank.Tier > 0 {
			rName := FormatRankName(sp.Rank.Tier, sp.Rank.Division)
			sName := FormatShortRankName(sp.Rank.Tier, sp.Rank.Division)
			sp.Rank.Name = rName
			sp.Rank.ShortRank = sName
			sp.RankName = rName
			sp.ShortRank = sName
		}
	}

	if len(st.Players) == 0 && len(mps) > 0 {
		for _, mp := range mps {
			var sp ReplayPlayerStats
			if strings.TrimSpace(mp.statsJSON) != "" && mp.statsJSON != "{}" {
				_ = json.Unmarshal([]byte(mp.statsJSON), &sp)
			}
			if sp.Name == "" {
				sp.Name = mp.playerName
				if sp.Name == "" {
					sp.Name = mp.playerID
				}
			}
			if sp.Platform == "" {
				sp.Platform = mp.platform
			}
			if sp.OnlineID == "" {
				sp.OnlineID = mp.playerID
			}
			sp.Team = mp.teamNum
			sp.IsLocal = mp.isLocal
			sp.Score = mp.score
			sp.Goals = mp.goals
			sp.Assists = mp.assists
			sp.Saves = mp.saves
			sp.Shots = mp.shots
			sp.Demos = mp.demos
			if sp.CarName == "" {
				sp.CarName = mp.carName
			}
			tier, div, mmr := ExtractPrimaryRankFromJSON(mp.ranksJSON, []int{playlist})
			if mp.isLocal && localMMR > 0 {
				mmr = localMMR
				if localTier > 0 && (tier != localTier || div == 0) {
					tier = localTier
					div = localDiv
				}
			}
			if sp.Rank != nil && sp.Rank.Tier > 0 {
				tier = sp.Rank.Tier
				div = sp.Rank.Division
			}
			if tier > 0 {
				rName := FormatRankName(tier, div)
				sName := FormatShortRankName(tier, div)
				sp.Rank = &ballchasing.PlayerRank{
					Tier:      tier,
					Division:  div,
					Name:      rName,
					ShortRank: sName,
				}
				sp.RankName = rName
				sp.ShortRank = sName
			}
			if mmr > 0 {
				sp.MMR = mmr
			}
			st.Players = append(st.Players, sp)
		}
	}

	return st
}

// ListHistoricalMatches returns paginated historical matches matching the filter,
// ordered by record_start_timestamp DESC, match_guid ASC (or filter.SortBy / filter.SortDir).
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

	playlistIDs := ParsePlaylistFilter(filter.PlaylistFilter)
	if len(playlistIDs) == 0 && filter.Playlist > 0 {
		playlistIDs = []int{filter.Playlist}
	}
	if len(playlistIDs) > 0 {
		placeholders := make([]string, len(playlistIDs))
		for i, pid := range playlistIDs {
			placeholders[i] = "?"
			args = append(args, pid)
		}
		whereClauses = append(whereClauses, fmt.Sprintf("playlist IN (%s)", strings.Join(placeholders, ",")))
	}
	if filter.FromUnix > 0 {
		whereClauses = append(whereClauses, "record_start_timestamp >= ?")
		args = append(args, filter.FromUnix)
	}
	if filter.ToUnix > 0 {
		whereClauses = append(whereClauses, "record_start_timestamp <= ?")
		args = append(args, filter.ToUnix)
	}

	cleanResult := NormalizeResultFilter(filter.Result)
	if cleanResult != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("(%s) = ?", outcomeExpr))
		args = append(args, cleanResult)
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	searchTerm := strings.TrimSpace(filter.Search)
	if searchTerm == "" {
		searchTerm = strings.TrimSpace(filter.Query)
	}
	lowerSearch := strings.ToLower(searchTerm)
	cleanStatus := strings.ToLower(strings.TrimSpace(filter.Status))
	cleanSortBy := strings.ToLower(strings.TrimSpace(filter.SortBy))
	cleanSortDir := strings.ToLower(strings.TrimSpace(filter.SortDir))

	needsInMemoryFilterOrSort := lowerSearch != "" ||
		(cleanStatus != "" && cleanStatus != "all") ||
		(cleanSortBy != "" && cleanSortBy != "date" && cleanSortBy != "time" && cleanSortBy != "timestamp") ||
		cleanSortDir == "asc"

	if !needsInMemoryFilterOrSort {
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

		records, err := scanHistoricalMatchRows(ctx, rows)
		if err != nil {
			return nil, 0, err
		}
		for _, rec := range records {
			rec.Stats = s.EnrichMatchStatsWithPlayers(ctx, rec.MatchGUID, rec.Playlist, &rec.MatchRecord, rec.Stats)
		}
		return records, total, nil
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
	ORDER BY record_start_timestamp DESC, match_guid ASC;
	`, outcomeExpr, whereSQL)

	rows, err := s.db.QueryContext(ctx, querySQL, args...)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, 0, wrapContextErr(ctxErr)
		}
		return nil, 0, fmt.Errorf("failed to query historical matches: %w", err)
	}
	defer rows.Close()

	allRecords, err := scanHistoricalMatchRows(ctx, rows)
	if err != nil {
		return nil, 0, err
	}

	matching := make([]*HistoricalMatchRecord, 0, len(allRecords))
	for _, rec := range allRecords {
		if !MatchesStatusFilter(&rec.MatchRecord, cleanStatus) {
			continue
		}
		if lowerSearch != "" && !s.matchMatchesSearchQuery(ctx, &rec.MatchRecord, lowerSearch) {
			continue
		}
		matching = append(matching, rec)
	}

	total := len(matching)
	if total == 0 || offset >= total {
		return make([]*HistoricalMatchRecord, 0), total, nil
	}

	var localStatsByGUID map[string]*MatchPlayerRecord
	if cleanSortBy != "" && cleanSortBy != "date" && cleanSortBy != "time" && cleanSortBy != "timestamp" && cleanSortBy != "duration" {
		localStatsByGUID = make(map[string]*MatchPlayerRecord)
		lpRows, lpErr := s.db.QueryContext(ctx, "SELECT match_guid, score, goals, assists, saves, shots, demos FROM match_players WHERE is_local = 1;")
		if lpErr == nil {
			defer lpRows.Close()
			for lpRows.Next() {
				var mp MatchPlayerRecord
				if err := lpRows.Scan(&mp.MatchGUID, &mp.Score, &mp.Goals, &mp.Assists, &mp.Saves, &mp.Shots, &mp.Demos); err == nil {
					localStatsByGUID[mp.MatchGUID] = &mp
				}
			}
		}
	}

	SortHistoricalMatches(matching, cleanSortBy, cleanSortDir, localStatsByGUID)

	end := offset + limit
	if end > total {
		end = total
	}
	page := make([]*HistoricalMatchRecord, end-offset)
	copy(page, matching[offset:end])
	for _, rec := range page {
		rec.Stats = s.EnrichMatchStatsWithPlayers(ctx, rec.MatchGUID, rec.Playlist, &rec.MatchRecord, rec.Stats)
	}
	return page, total, nil
}

func scanHistoricalMatchRows(ctx context.Context, rows *sql.Rows) ([]*HistoricalMatchRecord, error) {
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
				return nil, wrapContextErr(ctxErr)
			}
			return nil, fmt.Errorf("failed to scan match: %w", err)
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
			return nil, wrapContextErr(ctxErr)
		}
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return records, nil
}

// getLocalAccountIDs returns a set of known local account IDs from auth_state and match_players.
func (s *SQLiteStore) getLocalAccountIDs(ctx context.Context) map[string]bool {
	accounts := make(map[string]bool)
	addAccount := func(acc string) {
		acc = strings.TrimSpace(acc)
		if acc == "" {
			return
		}
		accounts[strings.ToLower(acc)] = true
		if strings.Contains(acc, "|") {
			parts := strings.Split(acc, "|")
			if len(parts) >= 2 && strings.TrimSpace(parts[1]) != "" && strings.TrimSpace(parts[1]) != "0" {
				accounts[strings.ToLower(strings.TrimSpace(parts[1]))] = true
			}
		}
	}

	if rows, err := s.db.QueryContext(ctx, "SELECT account_id FROM auth_state WHERE account_id != '';"); err == nil {
		for rows.Next() {
			var acc string
			if err := rows.Scan(&acc); err == nil {
				addAccount(acc)
			}
		}
		rows.Close()
	}

	if mpRows, err := s.db.QueryContext(ctx, "SELECT DISTINCT player_id FROM match_players WHERE is_local = 1;"); err == nil {
		for mpRows.Next() {
			var pid string
			if err := mpRows.Scan(&pid); err == nil {
				addAccount(pid)
			}
		}
		mpRows.Close()
	}

	return accounts
}

// FormatPlaylistName returns a standard display name for Rocket League playlist IDs.
func FormatPlaylistName(id int) string {
	return formatPlaylistName(id)
}

// formatPlaylistName returns a standard display name for Rocket League playlist IDs.
func formatPlaylistName(id int) string {
	switch id {
	case 1:
		return "Casual Duel 1v1"
	case 2:
		return "Casual Doubles 2v2"
	case 3:
		return "Casual Standard 3v3"
	case 4:
		return "Casual Chaos 4v4"
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

var canonical23TierNames = [23]string{
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
	13: "Diamond I",
	14: "Diamond II",
	15: "Diamond III",
	16: "Champion I",
	17: "Champion II",
	18: "Champion III",
	19: "Grand Champion I",
	20: "Grand Champion II",
	21: "Grand Champion III",
	22: "Supersonic Legend",
}

var canonical23ShortTierNames = [23]string{
	0:  "UNRANKED",
	1:  "BRZ I",
	2:  "BRZ II",
	3:  "BRZ III",
	4:  "SLV I",
	5:  "SLV II",
	6:  "SLV III",
	7:  "GOLD I",
	8:  "GOLD II",
	9:  "GOLD III",
	10: "PLAT I",
	11: "PLAT II",
	12: "PLAT III",
	13: "DIA I",
	14: "DIA II",
	15: "DIA III",
	16: "CHAMP I",
	17: "CHAMP II",
	18: "CHAMP III",
	19: "GC I",
	20: "GC II",
	21: "GC III",
	22: "SSL",
}

// FormatRankName formats tier (0..22) and division (1..4) into canonical 23-tier Rocket League rank names.
func FormatRankName(tier, division int) string {
	return formatRankName(tier, division)
}

// formatRankName formats tier (0..22) and division (1..4) into canonical 23-tier Rocket League rank names.
func formatRankName(tier, division int) string {
	if tier < 0 || tier >= len(canonical23TierNames) {
		return fmt.Sprintf("Tier %d", tier)
	}
	name := canonical23TierNames[tier]
	if tier == 0 || tier >= 22 {
		return name
	}
	divNames := [4]string{"Div I", "Div II", "Div III", "Div IV"}
	if division >= 1 && division <= 4 {
		return fmt.Sprintf("%s %s", name, divNames[division-1])
	}
	return name
}

// FormatShortRankName formats tier (0..22) and division (1..4) into abbreviated broadcast labels
// (e.g. "GC I · DIV II", "SSL", "CHAMP III · DIV IV", "DIA III · DIV IV", "PLAT II · DIV I").
func FormatShortRankName(tier, division int) string {
	if tier < 0 || tier >= len(canonical23ShortTierNames) {
		return fmt.Sprintf("T%d", tier)
	}
	shortBase := canonical23ShortTierNames[tier]
	if tier == 0 || tier >= 22 {
		return shortBase
	}
	shortDivs := [4]string{"DIV I", "DIV II", "DIV III", "DIV IV"}
	if division >= 1 && division <= 4 {
		return fmt.Sprintf("%s · %s", shortBase, shortDivs[division-1])
	}
	return shortBase
}

// hydrateRanksFromStats attempts to parse rank metadata from ReplayPlayerStats or replay raw JSON and update player ranks.
func (s *SQLiteStore) hydrateRanksFromStats(ctx context.Context, stats *ReplayMatchStats) {
	if stats == nil || stats.Playlist <= 0 {
		return
	}

	localAccounts := s.getLocalAccountIDs(ctx)

	applyRank := func(idStr string, tier, division int, rankID, rankName string) {
		if idStr == "" || (tier == 0 && (rankID == "" || rankID == "0")) {
			return
		}
		if localAccounts[strings.ToLower(idStr)] {
			return
		}
		pRec, err := s.GetPlayer(ctx, idStr)
		if err != nil || localAccounts[strings.ToLower(pRec.PlayerID)] {
			return
		}

		ranks := make(map[string]map[string]interface{})
		if pRec.RanksJSON != "" && pRec.RanksJSON != "{}" {
			_ = json.Unmarshal([]byte(pRec.RanksJSON), &ranks)
		}

		rName := formatRankName(tier, division)
		if tier == 0 && rankName != "" {
			rName = rankName
		}

		pKey := strconv.Itoa(stats.Playlist)
		mmr := 0.0
		matchesPlayed := 0
		if prev, ok := ranks[pKey]; ok {
			if mVal, ok := prev["mmr"].(float64); ok && mVal > 0 {
				mmr = mVal
			}
			if mpVal, ok := prev["matches_played"].(float64); ok && mpVal > 0 {
				matchesPlayed = int(mpVal)
			}
		}

		ranks[pKey] = map[string]interface{}{
			"playlist_id":    stats.Playlist,
			"playlist_name":  formatPlaylistName(stats.Playlist),
			"tier":           tier,
			"division":       division,
			"rank_name":      rName,
			"short_rank":     FormatShortRankName(tier, division),
			"mmr":            mmr,
			"matches_played": matchesPlayed,
		}

		if updatedData, err := json.Marshal(ranks); err == nil {
			_, _ = s.db.ExecContext(ctx, "UPDATE players SET ranks_json = ? WHERE player_id = ?;", string(updatedData), pRec.PlayerID)
		}
	}

	// 1. Hydrate directly from structured ReplayPlayerStats.Rank
	for _, p := range stats.Players {
		if p.Rank == nil {
			continue
		}
		pid := strings.TrimSpace(p.OnlineID)
		if (pid == "" || pid == "0") && strings.TrimSpace(p.Name) != "" {
			pid = strings.TrimSpace(p.Name)
		}
		if pid == "" {
			continue
		}
		plat := strings.TrimSpace(p.Platform)
		if plat == "" {
			plat = "Unknown"
		}
		canonicalID := s.resolvePlayerIDForMatch(ctx, stats.MatchGUID, pid, plat, p.Name, p.Team, true)
		applyRank(canonicalID, p.Rank.Tier, p.Rank.Division, p.Rank.ID, p.Rank.Name)
	}

	// 2. Fallback: hydrate from RawJSON if provided
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
		var idStr string
		switch v := bp.ID.(type) {
		case string:
			idStr = v
		case map[string]interface{}:
			if idVal, ok := v["id"].(string); ok {
				idStr = idVal
			}
		}
		rankIDStr := ""
		if bp.Rank.ID != nil {
			rankIDStr = fmt.Sprintf("%v", bp.Rank.ID)
		}
		applyRank(idStr, bp.Rank.Tier, bp.Rank.Division, rankIDStr, bp.Rank.Name)
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

	matchGUID = s.resolveExistingMatchGUID(ctx, matchGUID, "")
	resolvedPlayers := make([]*MatchPlayerRecord, 0, len(players))
	for _, p := range players {
		if p == nil || strings.TrimSpace(p.PlayerID) == "" {
			continue
		}
		cp := *p
		cp.MatchGUID = matchGUID
		plat := strings.TrimSpace(p.Platform)
		pName := strings.TrimSpace(p.PlayerName)
		if plat == "" {
			if parts := strings.SplitN(p.PlayerID, "|", 3); len(parts) == 3 {
				plat = strings.TrimSpace(parts[0])
			}
		}
		if pName == "" && strings.TrimSpace(p.StatsJSON) != "" {
			var rps ReplayPlayerStats
			if err := json.Unmarshal([]byte(p.StatsJSON), &rps); err == nil {
				if strings.TrimSpace(rps.Name) != "" {
					pName = strings.TrimSpace(rps.Name)
				}
				if plat == "" && strings.TrimSpace(rps.Platform) != "" {
					plat = strings.TrimSpace(rps.Platform)
				}
			}
		}
		resolvedID := s.resolvePlayerIDForMatch(ctx, matchGUID, p.PlayerID, plat, pName, p.TeamNum, false)
		if p.IsLocal {
			var existingLocalID string
			if err := s.db.QueryRowContext(ctx, "SELECT player_id FROM match_players WHERE match_guid = ? AND team_num = ? AND is_local = 1 LIMIT 1;", matchGUID, p.TeamNum).Scan(&existingLocalID); err == nil && existingLocalID != "" {
				resolvedID = existingLocalID
			}
		}
		cp.PlayerID = resolvedID
		resolvedPlayers = append(resolvedPlayers, &cp)
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

	for _, p := range resolvedPlayers {
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
	playerID = s.resolveCanonicalPlayerID(ctx, playerID)

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

	isLocalTarget := 0
	if s.isLocalPlayerID(ctx, playerID) {
		isLocalTarget = 1
	}

	const querySQL = `
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
	WHERE (mp.player_id = ? OR (? = 1 AND mp.is_local = 1))
	ORDER BY m.record_start_timestamp DESC, m.match_guid ASC;
	`

	rows, err := s.db.QueryContext(ctx, querySQL, playerID, isLocalTarget)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query player matches: %w", err)
	}
	defer rows.Close()

	playlistIDs := ParsePlaylistFilter(filter.PlaylistFilter)
	if len(playlistIDs) == 0 && filter.Playlist > 0 {
		playlistIDs = []int{filter.Playlist}
	}
	rawResultLower := strings.ToLower(strings.TrimSpace(filter.Result))
	useLocalPerspective := filter.LocalPerspective ||
		rawResultLower == "w" || rawResultLower == "l" ||
		rawResultLower == "win" || rawResultLower == "loss" ||
		rawResultLower == "wins" || rawResultLower == "losses"
	normResult := NormalizeResultFilter(filter.Result)
	normRole := NormalizeRoleFilter(filter.Role)
	lowerSearch := strings.ToLower(strings.TrimSpace(filter.Search))

	var allDetails []*PlayerMatchDetail
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

		playerResult := "unknown"
		if mp.Won != nil {
			if *mp.Won {
				playerResult = "victory"
			} else {
				playerResult = "defeat"
			}
		} else if mr.WinnerTeam != nil && *mr.WinnerTeam >= 0 {
			if *mr.WinnerTeam == mp.TeamNum {
				playerResult = "victory"
			} else {
				playerResult = "defeat"
			}
		} else if (mp.TeamNum == 0 && mr.BlueScore > mr.OrangeScore) || (mp.TeamNum == 1 && mr.OrangeScore > mr.BlueScore) {
			playerResult = "victory"
		} else if (mp.TeamNum == 0 && mr.OrangeScore > mr.BlueScore) || (mp.TeamNum == 1 && mr.BlueScore > mr.OrangeScore) {
			playerResult = "defeat"
		} else if (mr.WinnerTeam != nil && *mr.WinnerTeam == -1) || (mr.BlueScore == mr.OrangeScore && (mr.BlueScore > 0 || mr.OrangeScore > 0)) {
			playerResult = "draw"
		}

		effLocalTeam := localTeam
		if effLocalTeam < 0 {
			if mp.IsLocal {
				effLocalTeam = mp.TeamNum
			} else {
				effLocalTeam = 0
			}
		}
		localResult := "unknown"
		if mr.WinnerTeam != nil && *mr.WinnerTeam >= 0 {
			if *mr.WinnerTeam == effLocalTeam {
				localResult = "victory"
			} else {
				localResult = "defeat"
			}
		} else if mr.BlueScore != mr.OrangeScore {
			if (effLocalTeam == 0 && mr.BlueScore > mr.OrangeScore) || (effLocalTeam == 1 && mr.OrangeScore > mr.BlueScore) {
				localResult = "victory"
			} else {
				localResult = "defeat"
			}
		} else if mp.Won != nil {
			if role == "teammate" {
				if *mp.Won {
					localResult = "victory"
				} else {
					localResult = "defeat"
				}
			} else {
				if *mp.Won {
					localResult = "defeat"
				} else {
					localResult = "victory"
				}
			}
		} else if (mr.WinnerTeam != nil && *mr.WinnerTeam == -1) || (mr.BlueScore == mr.OrangeScore && (mr.BlueScore > 0 || mr.OrangeScore > 0)) {
			localResult = "draw"
		}

		effectiveResult := playerResult
		if useLocalPerspective {
			effectiveResult = localResult
		}

		if !MatchesPlaylistIDs(mr.Playlist, playlistIDs) {
			continue
		}
		if filter.FromUnix > 0 && mr.RecordStartTimestamp < filter.FromUnix {
			continue
		}
		if filter.ToUnix > 0 && mr.RecordStartTimestamp > filter.ToUnix {
			continue
		}
		if !MatchesStatusFilter(&mr, filter.Status) {
			continue
		}
		if normRole == "teammate" && role != "teammate" {
			continue
		}
		if normRole == "opponent" && role != "opponent" {
			continue
		}
		if normResult != "" && effectiveResult != normResult {
			continue
		}

		var st *ReplayMatchStats
		if mr.ReplayStatsJSON != "" {
			var parsed ReplayMatchStats
			if err := json.Unmarshal([]byte(mr.ReplayStatsJSON), &parsed); err == nil {
				st = &parsed
			}
		}

		allDetails = append(allDetails, &PlayerMatchDetail{
			MatchRecord: mr,
			PlayerStats: mp,
			Role:        role,
			Result:      effectiveResult,
			LocalResult: localResult,
			Stats:       st,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("rows error in list player matches: %w", err)
	}

	matching := make([]*PlayerMatchDetail, 0, len(allDetails))
	for _, d := range allDetails {
		if lowerSearch != "" && !s.matchMatchesSearchQuery(ctx, &d.MatchRecord, lowerSearch) {
			continue
		}
		matching = append(matching, d)
	}

	total := len(matching)
	if total == 0 || offset >= total {
		return make([]*PlayerMatchDetail, 0), total, nil
	}

	SortPlayerMatchDetails(matching, filter.SortBy, filter.SortDir)

	end := offset + limit
	if end > total {
		end = total
	}
	page := make([]*PlayerMatchDetail, end-offset)
	copy(page, matching[offset:end])
	for _, d := range page {
		d.Stats = s.EnrichMatchStatsWithPlayers(ctx, d.MatchGUID, d.Playlist, &d.MatchRecord, d.Stats)
	}
	return page, total, nil
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
	playerID = s.resolveCanonicalPlayerID(ctx, playerID)

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
	WHERE ballchasing_id != ''
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

func encodeSessionTime(t time.Time) int64 {
	return t.UTC().UnixNano()
}

func decodeSessionTime(val int64) time.Time {
	if val == 0 {
		return time.Time{}
	}
	// Support Unix seconds, milliseconds, and nanoseconds seamlessly.
	if val > -1e11 && val < 1e11 {
		return time.Unix(val, 0).UTC()
	}
	if val > -1e14 && val < 1e14 {
		return time.UnixMilli(val).UTC()
	}
	return time.Unix(0, val).UTC()
}

// SaveSession inserts or updates a persistent play session record.
func (s *SQLiteStore) SaveSession(ctx context.Context, session *SessionRecord) error {
	if err := ctx.Err(); err != nil {
		return wrapContextErr(err)
	}
	if session == nil || strings.TrimSpace(session.SessionID) == "" {
		return errors.New("session ID cannot be empty")
	}

	sessionID := strings.TrimSpace(session.SessionID)
	now := time.Now().UTC()
	updatedAt := session.UpdatedAt.UTC()
	if updatedAt.IsZero() {
		updatedAt = now
	}
	startedAt := session.StartedAt.UTC()
	if startedAt.IsZero() {
		startedAt = updatedAt
	}

	var endedAtVal interface{}
	if session.EndedAt != nil && !session.EndedAt.IsZero() {
		endedAtVal = encodeSessionTime(*session.EndedAt)
	}

	var lastMatchAtVal interface{}
	if session.LastMatchAt != nil && !session.LastMatchAt.IsZero() {
		lastMatchAtVal = encodeSessionTime(*session.LastMatchAt)
	}

	playlistsJSON := session.PlaylistsJSON
	if strings.TrimSpace(playlistsJSON) == "" {
		playlistsJSON = "{}"
	}
	matchesJSON := session.MatchesJSON
	if strings.TrimSpace(matchesJSON) == "" {
		matchesJSON = "[]"
	}
	localIDs := session.LocalPlayerIDs
	if localIDs == nil {
		localIDs = []string{}
	}
	localIDsBytes, err := json.Marshal(localIDs)
	if err != nil {
		localIDsBytes = []byte("[]")
	}

	dismissedInt := 0
	if session.Dismissed {
		dismissedInt = 1
	}

	const query = `
	INSERT INTO sessions (
		session_id, started_at, ended_at, last_match_at, updated_at,
		total_matches, total_wins, total_losses, win_rate,
		playlists_json, matches_json, local_player_ids, dismissed
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(session_id) DO UPDATE SET
		started_at = excluded.started_at,
		ended_at = excluded.ended_at,
		last_match_at = excluded.last_match_at,
		updated_at = excluded.updated_at,
		total_matches = excluded.total_matches,
		total_wins = excluded.total_wins,
		total_losses = excluded.total_losses,
		win_rate = excluded.win_rate,
		playlists_json = excluded.playlists_json,
		matches_json = excluded.matches_json,
		local_player_ids = excluded.local_player_ids,
		dismissed = excluded.dismissed;
	`

	_, err = s.db.ExecContext(ctx, query,
		sessionID,
		encodeSessionTime(startedAt),
		endedAtVal,
		lastMatchAtVal,
		encodeSessionTime(updatedAt),
		session.TotalMatches,
		session.TotalWins,
		session.TotalLosses,
		session.WinRate,
		playlistsJSON,
		matchesJSON,
		string(localIDsBytes),
		dismissedInt,
	)
	if err != nil {
		return wrapContextErr(fmt.Errorf("failed to save session %s: %w", sessionID, err))
	}
	return nil
}

// GetSession retrieves a persistent play session by its ID.
func (s *SQLiteStore) GetSession(ctx context.Context, sessionID string) (*SessionRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContextErr(err)
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, ErrSessionNotFound
	}

	const query = `
	SELECT session_id, started_at, ended_at, last_match_at, updated_at,
	       total_matches, total_wins, total_losses, win_rate,
	       playlists_json, matches_json, local_player_ids, dismissed
	FROM sessions
	WHERE session_id = ?;
	`

	row := s.db.QueryRowContext(ctx, query, sessionID)
	rec, err := scanSessionRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, wrapContextErr(fmt.Errorf("failed to get session %s: %w", sessionID, err))
	}
	return rec, nil
}

// ListSessions returns a paginated list of persisted play sessions ordered most recent first.
func (s *SQLiteStore) ListSessions(ctx context.Context, limit, offset int) ([]*SessionRecord, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, wrapContextErr(err)
	}
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions;").Scan(&total); err != nil {
		return nil, 0, wrapContextErr(fmt.Errorf("failed to count sessions: %w", err))
	}

	const query = `
	SELECT session_id, started_at, ended_at, last_match_at, updated_at,
	       total_matches, total_wins, total_losses, win_rate,
	       playlists_json, matches_json, local_player_ids, dismissed
	FROM sessions
	ORDER BY COALESCE(last_match_at, started_at) DESC, started_at DESC, session_id ASC
	LIMIT ? OFFSET ?;
	`

	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, wrapContextErr(fmt.Errorf("failed to list sessions: %w", err))
	}
	defer rows.Close()

	var records []*SessionRecord
	for rows.Next() {
		rec, scanErr := scanSessionRow(rows)
		if scanErr != nil {
			return nil, 0, wrapContextErr(fmt.Errorf("failed to scan session row: %w", scanErr))
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, wrapContextErr(fmt.Errorf("rows error in list sessions: %w", err))
	}
	if records == nil {
		records = make([]*SessionRecord, 0)
	}
	return records, total, nil
}

// DeleteSession deletes a play session record by ID.
func (s *SQLiteStore) DeleteSession(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return wrapContextErr(err)
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}

	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE session_id = ?;", sessionID)
	if err != nil {
		return wrapContextErr(fmt.Errorf("failed to delete session %s: %w", sessionID, err))
	}
	return nil
}

type sessionRowScanner interface {
	Scan(dest ...interface{}) error
}

func scanSessionRow(scanner sessionRowScanner) (*SessionRecord, error) {
	var (
		rec          SessionRecord
		startedAtRaw int64
		endedAtRaw   sql.NullInt64
		lastMatchRaw sql.NullInt64
		updatedAtRaw int64
		localIDsRaw  string
		dismissedInt int
	)
	err := scanner.Scan(
		&rec.SessionID,
		&startedAtRaw,
		&endedAtRaw,
		&lastMatchRaw,
		&updatedAtRaw,
		&rec.TotalMatches,
		&rec.TotalWins,
		&rec.TotalLosses,
		&rec.WinRate,
		&rec.PlaylistsJSON,
		&rec.MatchesJSON,
		&localIDsRaw,
		&dismissedInt,
	)
	if err != nil {
		return nil, err
	}

	rec.StartedAt = decodeSessionTime(startedAtRaw)
	if endedAtRaw.Valid && endedAtRaw.Int64 != 0 {
		t := decodeSessionTime(endedAtRaw.Int64)
		rec.EndedAt = &t
	}
	if lastMatchRaw.Valid && lastMatchRaw.Int64 != 0 {
		t := decodeSessionTime(lastMatchRaw.Int64)
		rec.LastMatchAt = &t
	}
	rec.UpdatedAt = decodeSessionTime(updatedAtRaw)
	rec.Dismissed = dismissedInt != 0

	if strings.TrimSpace(localIDsRaw) != "" {
		_ = json.Unmarshal([]byte(localIDsRaw), &rec.LocalPlayerIDs)
	}
	if rec.LocalPlayerIDs == nil {
		rec.LocalPlayerIDs = []string{}
	}
	return &rec, nil
}

// migrateSessionsTable ensures the sessions table has the dismissed column.
func migrateSessionsTable(ctx context.Context, db *sql.DB) error {
	var tableCount int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='sessions';").Scan(&tableCount)
	if err != nil || tableCount == 0 {
		return nil
	}

	rows, err := db.QueryContext(ctx, "PRAGMA table_info(sessions);")
	if err != nil {
		return fmt.Errorf("failed to read sessions table_info: %w", err)
	}
	defer rows.Close()

	cols := make(map[string]bool)
	for rows.Next() {
		var cid, notnull, pk int
		var name, colType string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("failed to scan sessions table_info: %w", err)
		}
		cols[strings.ToLower(name)] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("sessions table_info iteration error: %w", err)
	}

	if !cols["dismissed"] {
		if _, err := db.ExecContext(ctx, "ALTER TABLE sessions ADD COLUMN dismissed INTEGER NOT NULL DEFAULT 0;"); err != nil {
			return fmt.Errorf("failed to add dismissed column to sessions: %w", err)
		}
	}
	return nil
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

			var statsJSONStr string
			if p.Core != nil || p.Boost != nil || p.Movement != nil || p.Positioning != nil || p.Demo != nil || p.Ball != nil || p.Camera != nil || p.Rank != nil {
				if b, mErr := json.Marshal(p); mErr == nil {
					statsJSONStr = string(b)
				}
			}

			if _, err := stmt.ExecContext(ctx,
				m.guid, canonicalID, p.Team, 0, wonVal,
				p.Score, p.Goals, p.Assists, p.Saves, p.Shots, p.Demos,
				strings.TrimSpace(p.CarName), statsJSONStr,
			); err != nil {
				return fmt.Errorf("failed to backfill match_player %s in %s: %w", canonicalID, m.guid, err)
			}
		}
	}

	return tx.Commit()
}

// mergeDuplicatePlayerIDs merges a duplicate player ID into a canonical player ID across match_players and players.
func (s *SQLiteStore) mergeDuplicatePlayerIDs(ctx context.Context, dupID, canonID string) {
	if dupID == "" || canonID == "" || dupID == canonID {
		return
	}
	_, _ = s.db.ExecContext(ctx, `
		UPDATE match_players
		SET car_name = CASE WHEN match_players.car_name != '' THEN match_players.car_name ELSE COALESCE((SELECT d.car_name FROM match_players d WHERE d.match_guid = match_players.match_guid AND d.player_id = ?), '') END,
		    stats_json = CASE WHEN match_players.stats_json != '' THEN match_players.stats_json ELSE COALESCE((SELECT d.stats_json FROM match_players d WHERE d.match_guid = match_players.match_guid AND d.player_id = ?), '') END
		WHERE player_id = ?;
	`, dupID, dupID, canonID)

	_, _ = s.db.ExecContext(ctx, `
		DELETE FROM match_players
		WHERE player_id = ?
		  AND EXISTS (SELECT 1 FROM match_players c WHERE c.match_guid = match_players.match_guid AND c.player_id = ?);
	`, dupID, canonID)

	_, _ = s.db.ExecContext(ctx, `
		UPDATE match_players SET player_id = ? WHERE player_id = ?;
	`, canonID, dupID)

	_, _ = s.db.ExecContext(ctx, `
		UPDATE players
		SET ranks_json = CASE WHEN (players.ranks_json = '' OR players.ranks_json = '{}') THEN COALESCE((SELECT d.ranks_json FROM players d WHERE d.player_id = ?), '{}') ELSE players.ranks_json END
		WHERE player_id = ?;
	`, dupID, canonID)

	_, _ = s.db.ExecContext(ctx, "DELETE FROM player_matchups WHERE player_id = ?;", dupID)
	_, _ = s.db.ExecContext(ctx, "DELETE FROM players WHERE player_id = ?;", dupID)
}

// repairHistoricalMatchesAndOutcomes repairs historical matches where blue_score and orange_score
// were synced as 0-0 despite non-zero player goals in replay_stats_json, updates match_players.won,
// normalizes API links in ballchasing_url, deduplicates player aliases, repairs timestamps,
// and rebuilds head-to-head player_matchups with local-perspective win/loss outcomes.
func (s *SQLiteStore) repairHistoricalMatchesAndOutcomes(ctx context.Context) error {
	// 1. Normalize any API links in ballchasing_url to web links and repair upload_status / stats_sync_status
	_, _ = s.db.ExecContext(ctx, `
		UPDATE matches
		SET ballchasing_url = 'https://ballchasing.com/replay/' || ballchasing_id
		WHERE ballchasing_id != '' AND (ballchasing_url LIKE '%/api/replays/%' OR ballchasing_url = '');
	`)
	_, _ = s.db.ExecContext(ctx, `
		UPDATE matches
		SET upload_status = 'UPLOADED'
		WHERE ballchasing_id != '' AND (upload_status = '' OR upload_status = 'PENDING' OR upload_status = 'FAILED');
	`)
	_, _ = s.db.ExecContext(ctx, `
		UPDATE matches
		SET stats_sync_status = 'PENDING', stats_retry_count = 0
		WHERE ballchasing_id != ''
		  AND stats_sync_status = 'SYNCED'
		  AND replay_stats_json NOT LIKE '%"boost":%';
	`)

	// 2. Find matches with 0-0 scores and non-empty replay_stats_json
	rows, err := s.db.QueryContext(ctx, `
		SELECT match_guid, record_start_timestamp, playlist, ballchasing_id, replay_stats_json
		FROM matches
		WHERE blue_score = 0 AND orange_score = 0 AND replay_stats_json != '';
	`)
	if err != nil {
		return err
	}

	type rawCandidate struct {
		guid          string
		startTS       int64
		playlist      int
		ballchasingID string
		statsJSON     string
	}
	var candidates []rawCandidate
	for rows.Next() {
		var c rawCandidate
		if err := rows.Scan(&c.guid, &c.startTS, &c.playlist, &c.ballchasingID, &c.statsJSON); err == nil {
			candidates = append(candidates, c)
		}
	}
	rows.Close()

	type repairedMatch struct {
		guid          string
		startTS       int64
		playlist      int
		ballchasingID string
		blueScore     int
		orangeScore   int
		winnerTeam    int
		statsJSON     string
	}
	var toRepair []repairedMatch

	for _, c := range candidates {
		var st ReplayMatchStats
		if err := json.Unmarshal([]byte(c.statsJSON), &st); err != nil || len(st.Players) == 0 {
			continue
		}
		bGoals, oGoals := 0, 0
		for _, p := range st.Players {
			if p.Team == 0 {
				bGoals += p.Goals
			} else if p.Team == 1 {
				oGoals += p.Goals
			}
		}
		if bGoals == 0 && oGoals == 0 {
			continue
		}
		st.BlueScore = bGoals
		st.OrangeScore = oGoals
		winner := -1
		if bGoals > oGoals {
			winner = 0
			st.WinnerTeam = &winner
		} else if oGoals > bGoals {
			winner = 1
			st.WinnerTeam = &winner
		}
		st.BallchasingURL = NormalizeBallchasingURL(st.BallchasingID, st.BallchasingURL)
		updatedJSON, err := json.Marshal(&st)
		if err != nil {
			continue
		}
		toRepair = append(toRepair, repairedMatch{
			guid:          c.guid,
			startTS:       c.startTS,
			playlist:      c.playlist,
			ballchasingID: c.ballchasingID,
			blueScore:     bGoals,
			orangeScore:   oGoals,
			winnerTeam:    winner,
			statsJSON:     string(updatedJSON),
		})
	}

	if len(toRepair) > 0 {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		stmtMatch, err := tx.PrepareContext(ctx, `
			UPDATE matches
			SET blue_score = ?, orange_score = ?, winner_team = ?, replay_stats_json = ?
			WHERE match_guid = ?;
		`)
		if err != nil {
			return err
		}
		defer stmtMatch.Close()

		stmtMP, err := tx.PrepareContext(ctx, `
			UPDATE match_players
			SET won = CASE WHEN team_num = ? THEN 1 ELSE 0 END
			WHERE match_guid = ?;
		`)
		if err != nil {
			return err
		}
		defer stmtMP.Close()

		for _, r := range toRepair {
			_, _ = stmtMatch.ExecContext(ctx, r.blueScore, r.orangeScore, r.winnerTeam, r.statsJSON, r.guid)
			if r.winnerTeam >= 0 {
				_, _ = stmtMP.ExecContext(ctx, r.winnerTeam, r.guid)
			}
		}

		if err := tx.Commit(); err != nil {
			return err
		}

		// Backfill head-to-head player_matchups for newly repaired matches
		for _, r := range toRepair {
			if r.winnerTeam < 0 || r.playlist <= 0 || r.ballchasingID == "" {
				continue
			}

			mpRows, err := s.db.QueryContext(ctx, `
				SELECT mp.player_id, mp.team_num, mp.is_local, COALESCE(p.platform, ''), COALESCE(p.player_name, '')
				FROM match_players mp
				LEFT JOIN players p ON mp.player_id = p.player_id
				WHERE mp.match_guid = ?;
			`, r.guid)
			if err != nil {
				continue
			}

			type mpInfo struct {
				playerID string
				teamNum  int
				isLocal  bool
				platform string
				name     string
			}
			var participants []mpInfo
			localTeam := -1
			for mpRows.Next() {
				var info mpInfo
				var isLoc int
				if err := mpRows.Scan(&info.playerID, &info.teamNum, &isLoc, &info.platform, &info.name); err == nil {
					info.isLocal = (isLoc == 1)
					if info.isLocal && localTeam == -1 {
						localTeam = info.teamNum
					}
					participants = append(participants, info)
				}
			}
			mpRows.Close()

			if localTeam < 0 {
				continue
			}

			var outcomes []PlayerOutcome
			for _, p := range participants {
				if p.isLocal || strings.HasSuffix(p.playerID, "|0|0") {
					continue
				}
				outcomes = append(outcomes, PlayerOutcome{
					PlayerID:   p.playerID,
					Platform:   p.platform,
					PlayerName: p.name,
					IsTeammate: p.teamNum == localTeam,
					Won:        localTeam == r.winnerTeam,
				})
			}

			if len(outcomes) > 0 {
				matchTime := time.Unix(r.startTS, 0).UTC()
				_ = s.RecordMatchResults(ctx, r.guid, r.playlist, outcomes, matchTime)
			}
		}
	}

	// 3. Remove local player accounts from players and player_matchups
	localAccounts := s.getLocalAccountIDs(ctx)
	for accLower := range localAccounts {
		_, _ = s.db.ExecContext(ctx, "UPDATE match_players SET is_local = 1 WHERE LOWER(player_id) = ?;", accLower)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM player_matchups WHERE LOWER(player_id) = ?;", accLower)
		_, _ = s.db.ExecContext(ctx, "DELETE FROM players WHERE LOWER(player_id) = ?;", accLower)
	}

	// 4. One-time migration (PRAGMA user_version < 4) to deduplicate console aliases,
	// ensure all replay_stats_json players exist in players/match_players,
	// rebuild head-to-head player_matchups atomically, and mark matches lacking full boost telemetry as PENDING.
	var userVersion int
	_ = s.db.QueryRowContext(ctx, "PRAGMA user_version;").Scan(&userVersion)
	if userVersion < 4 {
		// 4.0 Ensure all non-local players in matches.replay_stats_json are present in players and match_players,
		// and repair matches.record_start_timestamp if it was set to wall-clock sync time instead of st.Date.
		type mJsonRow struct {
			guid                  string
			startTS               int64
			winnerTeam            int
			statsJSON             string
			mpCount               int
			nonLocalInPlayersCnt  int
		}
		var mJsonRows []mJsonRow
		mjRows, err := s.db.QueryContext(ctx, `
			SELECT m.match_guid,
			       m.record_start_timestamp,
			       COALESCE(m.winner_team, -1),
			       m.replay_stats_json,
			       (SELECT COUNT(*) FROM match_players mp WHERE mp.match_guid = m.match_guid),
			       (SELECT COUNT(*) FROM match_players mp JOIN players p ON mp.player_id = p.player_id WHERE mp.match_guid = m.match_guid AND mp.is_local = 0)
			FROM matches m
			WHERE m.replay_stats_json != '';
		`)
		if err == nil {
			for mjRows.Next() {
				var r mJsonRow
				if err := mjRows.Scan(&r.guid, &r.startTS, &r.winnerTeam, &r.statsJSON, &r.mpCount, &r.nonLocalInPlayersCnt); err == nil {
					mJsonRows = append(mJsonRows, r)
				}
			}
			mjRows.Close()
		}

		for _, r := range mJsonRows {
			var st ReplayMatchStats
			if err := json.Unmarshal([]byte(r.statsJSON), &st); err != nil {
				continue
			}
			matchTS := r.startTS
			if st.Date != "" {
				if parsed, ok := ParseReplayDate(st.Date); ok && parsed.Unix() > 0 {
					if matchTS <= 0 || matchTS-parsed.Unix() > 86400 {
						matchTS = parsed.Unix()
						_, _ = s.db.ExecContext(ctx, "UPDATE matches SET record_start_timestamp = ? WHERE match_guid = ?;", matchTS, r.guid)
					}
				}
			}
			// Fast-path skip if match_players and players are already populated for this match
			if r.mpCount >= len(st.Players) && r.nonLocalInPlayersCnt >= len(st.Players)-1 && len(st.Players) > 0 {
				continue
			}
			for _, p := range st.Players {
				pid := strings.TrimSpace(p.OnlineID)
				if (pid == "" || pid == "0") && strings.TrimSpace(p.Name) != "" {
					pid = strings.TrimSpace(p.Name)
				}
				if pid == "" {
					continue
				}
				plat := strings.TrimSpace(p.Platform)
				if plat == "" {
					plat = "Unknown"
				}
				canonicalID := s.resolvePlayerIDForMatch(ctx, r.guid, pid, plat, p.Name, p.Team, true)
				isLocal := localAccounts[strings.ToLower(pid)] || localAccounts[strings.ToLower(canonicalID)]
				if !isLocal {
					_, _ = s.db.ExecContext(ctx, `
						INSERT INTO players (player_id, platform, player_name, player_name_lower, ranks_json, first_seen_at, last_seen_at)
						VALUES (?, ?, ?, ?, '{}', ?, ?)
						ON CONFLICT(player_id) DO UPDATE SET
							first_seen_at = MIN(players.first_seen_at, excluded.first_seen_at),
							last_seen_at = MAX(players.last_seen_at, excluded.last_seen_at);
					`, canonicalID, plat, strings.TrimSpace(p.Name), strings.ToLower(strings.TrimSpace(p.Name)), matchTS, matchTS)
				}
				var wonVal *int
				if r.winnerTeam >= 0 {
					w := 0
					if r.winnerTeam == p.Team {
						w = 1
					}
					wonVal = &w
				}
				isLocInt := 0
				if isLocal {
					isLocInt = 1
				}
				var statsJSONStr string
				if p.Core != nil || p.Boost != nil || p.Movement != nil || p.Positioning != nil || p.Demo != nil || p.Ball != nil || p.Camera != nil || p.Rank != nil {
					if b, mErr := json.Marshal(p); mErr == nil {
						statsJSONStr = string(b)
					}
				}
				_, _ = s.db.ExecContext(ctx, `
					INSERT INTO match_players (match_guid, player_id, team_num, is_local, won, score, goals, assists, saves, shots, demos, car_name, stats_json)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
					ON CONFLICT(match_guid, player_id) DO UPDATE SET
						stats_json = CASE WHEN match_players.stats_json = '' THEN excluded.stats_json ELSE match_players.stats_json END;
				`, r.guid, canonicalID, p.Team, isLocInt, wonVal, p.Score, p.Goals, p.Assists, p.Saves, p.Shots, p.Demos, strings.TrimSpace(p.CarName), statsJSONStr)
			}
		}

		// 4a. Merge players sharing the same middle OnlineID segment across platform family aliases (e.g. PS4|<id>|0 vs PlayStation|<id>|0)
		type pRow struct {
			id       string
			platform string
			name     string
			matches  int
		}
		pRows, err := s.db.QueryContext(ctx, `
			SELECT p.player_id, p.platform, p.player_name, COALESCE(SUM(m.total_matches), 0)
			FROM players p
			LEFT JOIN player_matchups m ON p.player_id = m.player_id
			GROUP BY p.player_id;
		`)
		if err == nil {
			byToken := make(map[string][]pRow)
			for pRows.Next() {
				var r pRow
				if err := pRows.Scan(&r.id, &r.platform, &r.name, &r.matches); err == nil {
					parts := strings.Split(r.id, "|")
					if len(parts) >= 2 {
						tok := strings.ToLower(strings.TrimSpace(parts[1]))
						if tok != "" && tok != "0" {
							byToken[tok] = append(byToken[tok], r)
						}
					}
				}
			}
			pRows.Close()

			for _, group := range byToken {
				if len(group) < 2 {
					continue
				}
				canonIdx := 0
				for i := 1; i < len(group); i++ {
					if group[i].matches > group[canonIdx].matches ||
						(group[i].matches == group[canonIdx].matches && (group[i].platform == "PS4" || group[i].platform == "XboxOne")) {
						canonIdx = i
					}
				}
				canon := group[canonIdx]
				for i, other := range group {
					if i != canonIdx && SamePlatformFamily(canon.platform, other.platform) {
						s.mergeDuplicatePlayerIDs(ctx, other.id, canon.id)
					}
				}
			}
		}

		// 4b. Merge same-match, same-team, same-name duplicate console entries in match_players
		// (e.g. PS4|7370223800633025836|0 and PlayStation|hungry-panda88|0 in the same match)
		type dupPair struct {
			dupID   string
			canonID string
		}
		var pairs []dupPair
		dupRows, err := s.db.QueryContext(ctx, `
			SELECT mp1.player_id, mp2.player_id
			FROM match_players mp1
			JOIN match_players mp2
			  ON mp1.match_guid = mp2.match_guid
			 AND mp1.team_num = mp2.team_num
			 AND mp1.player_id != mp2.player_id
			 AND mp1.is_local = 0 AND mp2.is_local = 0
			 AND mp1.score = mp2.score
			 AND mp1.goals = mp2.goals
			 AND mp1.assists = mp2.assists
			 AND mp1.saves = mp2.saves
			 AND mp1.shots = mp2.shots
			JOIN players p1 ON mp1.player_id = p1.player_id
			JOIN players p2 ON mp2.player_id = p2.player_id
			WHERE p1.player_name_lower != ''
			  AND p1.player_name_lower = p2.player_name_lower
			  AND (mp1.player_id LIKE 'PS4|%' OR mp1.player_id LIKE 'XboxOne|%')
			  AND (mp2.player_id LIKE 'PlayStation|%' OR mp2.player_id LIKE 'Xbox|%')
			GROUP BY mp1.player_id, mp2.player_id;
		`)
		if err == nil {
			for dupRows.Next() {
				var cID, dID string
				if err := dupRows.Scan(&cID, &dID); err == nil {
					pairs = append(pairs, dupPair{dupID: dID, canonID: cID})
				}
			}
			dupRows.Close()
			for _, pair := range pairs {
				s.mergeDuplicatePlayerIDs(ctx, pair.dupID, pair.canonID)
			}
		}

		// 4c. Rebuild player_matchups and processed_match_outcomes atomically from matches + match_players
		// so all head-to-head records across all playlists are complete and accurate from the local perspective.
		var hasMatchPlayersWithWinner int
		_ = s.db.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM matches m
			WHERE m.winner_team >= 0
			  AND EXISTS (SELECT 1 FROM match_players lp WHERE lp.match_guid = m.match_guid AND lp.is_local = 1);
		`).Scan(&hasMatchPlayersWithWinner)

		if hasMatchPlayersWithWinner > 0 {
			if tx, err := s.db.BeginTx(ctx, nil); err == nil {
				// Ensure every non-local participant in match_players exists in players to satisfy foreign key constraints
				_, _ = tx.ExecContext(ctx, `
					INSERT INTO players (player_id, platform, player_name, player_name_lower, ranks_json, first_seen_at, last_seen_at)
					SELECT
						mp.player_id,
						CASE WHEN INSTR(mp.player_id, '|') > 1 THEN SUBSTR(mp.player_id, 1, INSTR(mp.player_id, '|') - 1) ELSE 'Unknown' END,
						CASE WHEN INSTR(mp.player_id, '|') > 1 THEN SUBSTR(mp.player_id, INSTR(mp.player_id, '|') + 1, LENGTH(mp.player_id) - INSTR(mp.player_id, '|') - 2) ELSE mp.player_id END,
						LOWER(CASE WHEN INSTR(mp.player_id, '|') > 1 THEN SUBSTR(mp.player_id, INSTR(mp.player_id, '|') + 1, LENGTH(mp.player_id) - INSTR(mp.player_id, '|') - 2) ELSE mp.player_id END),
						'{}',
						COALESCE(MIN(m.record_start_timestamp), 0),
						COALESCE(MAX(m.record_start_timestamp), 0)
					FROM match_players mp
					JOIN matches m ON mp.match_guid = m.match_guid
					WHERE mp.is_local = 0
					  AND mp.player_id != ''
					  AND mp.player_id != '0'
					  AND mp.player_id NOT LIKE '%|0|0'
					  AND NOT EXISTS (SELECT 1 FROM players p WHERE p.player_id = mp.player_id)
					GROUP BY mp.player_id;
				`)

				_, _ = tx.ExecContext(ctx, "DELETE FROM player_matchups;")
				_, _ = tx.ExecContext(ctx, "DELETE FROM processed_match_outcomes;")

				_, _ = tx.ExecContext(ctx, `
					INSERT INTO player_matchups (
						player_id, playlist_id,
						wins_as_teammate, losses_as_teammate,
						wins_as_opponent, losses_as_opponent,
						total_matches, last_played_at
					)
					SELECT
						mp.player_id,
						m.playlist,
						COALESCE(SUM(CASE WHEN mp.team_num = lp.team_num AND lp.team_num = m.winner_team THEN 1 ELSE 0 END), 0),
						COALESCE(SUM(CASE WHEN mp.team_num = lp.team_num AND lp.team_num != m.winner_team THEN 1 ELSE 0 END), 0),
						COALESCE(SUM(CASE WHEN mp.team_num != lp.team_num AND lp.team_num = m.winner_team THEN 1 ELSE 0 END), 0),
						COALESCE(SUM(CASE WHEN mp.team_num != lp.team_num AND lp.team_num != m.winner_team THEN 1 ELSE 0 END), 0),
						COUNT(*),
						COALESCE(MAX(m.record_start_timestamp), 0)
					FROM match_players mp
					JOIN matches m ON mp.match_guid = m.match_guid
					JOIN (
						SELECT match_guid, MIN(team_num) AS team_num
						FROM match_players
						WHERE is_local = 1
						GROUP BY match_guid
					) lp ON lp.match_guid = m.match_guid
					JOIN players p ON p.player_id = mp.player_id
					WHERE m.winner_team >= 0
					  AND mp.is_local = 0
					  AND mp.player_id NOT LIKE '%|0|0'
					GROUP BY mp.player_id, m.playlist;
				`)

				_, _ = tx.ExecContext(ctx, `
					INSERT INTO processed_match_outcomes (match_guid, playlist_id, processed_at)
					SELECT
						m.match_guid,
						m.playlist,
						m.record_start_timestamp
					FROM matches m
					WHERE m.winner_team >= 0
					  AND EXISTS (SELECT 1 FROM match_players lp WHERE lp.match_guid = m.match_guid AND lp.is_local = 1)
					  AND EXISTS (SELECT 1 FROM match_players mp WHERE mp.match_guid = m.match_guid AND mp.is_local = 0 AND mp.player_id NOT LIKE '%|0|0')
					ON CONFLICT(match_guid) DO NOTHING;
				`)

				// Remove orphan player records absent from both player_matchups and match_players
				_, _ = tx.ExecContext(ctx, `
					DELETE FROM players
					WHERE NOT EXISTS (SELECT 1 FROM player_matchups pm WHERE pm.player_id = players.player_id)
					  AND NOT EXISTS (SELECT 1 FROM match_players mp WHERE mp.player_id = players.player_id);
				`)

				_ = tx.Commit()
			}
		}

		// 4d. Mark Ballchasing matches whose replay_stats_json lacks full boost telemetry as PENDING
		_, _ = s.db.ExecContext(ctx, `
			UPDATE matches
			SET stats_sync_status = 'PENDING', stats_retry_count = 0
			WHERE ballchasing_id != ''
			  AND stats_sync_status = 'SYNCED'
			  AND replay_stats_json NOT LIKE '%"boost":%';
		`)

		_, _ = s.db.ExecContext(ctx, "PRAGMA user_version = 4;")
	}

	// 5. Repair player first_seen_at and last_seen_at timestamps that were set to wall-clock sync time
	// instead of historical match record_start_timestamp.
	_, _ = s.db.ExecContext(ctx, `
		UPDATE players
		SET first_seen_at = (
				SELECT MIN(m.record_start_timestamp)
				FROM match_players mp
				JOIN matches m ON mp.match_guid = m.match_guid
				WHERE mp.player_id = players.player_id AND m.record_start_timestamp > 0
			),
			last_seen_at = (
				SELECT MAX(m.record_start_timestamp)
				FROM match_players mp
				JOIN matches m ON mp.match_guid = m.match_guid
				WHERE mp.player_id = players.player_id AND m.record_start_timestamp > 0
			)
		WHERE EXISTS (
			SELECT 1
			FROM match_players mp
			JOIN matches m ON mp.match_guid = m.match_guid
			WHERE mp.player_id = players.player_id AND m.record_start_timestamp > 0
			GROUP BY mp.player_id
			HAVING players.last_seen_at > MAX(m.record_start_timestamp) + 3600
		);
	`)

	// 6. Repair any corrupted rank_name values in players.ranks_json caused by the legacy 19-tier rank table
	s.repairCorruptedRanksJSON(ctx, false)

	// 7. Backfill match_players.stats_json from matches.replay_stats_json for any rows missing per-player telemetry
	if mpMissingRows, err := s.db.QueryContext(ctx, `
		SELECT mp.match_guid, mp.player_id, mp.team_num, COALESCE(p.player_name, ''), m.replay_stats_json
		FROM match_players mp
		JOIN matches m ON mp.match_guid = m.match_guid
		LEFT JOIN players p ON mp.player_id = p.player_id
		WHERE (mp.stats_json = '' OR mp.stats_json = '{}')
		  AND m.replay_stats_json LIKE '%"boost":%';
	`); err == nil {
		type mpBackfill struct {
			guid      string
			playerID  string
			statsJSON string
		}
		var backfills []mpBackfill
		for mpMissingRows.Next() {
			var guid, pid, pName, replayJSON string
			var teamNum int
			if err := mpMissingRows.Scan(&guid, &pid, &teamNum, &pName, &replayJSON); err == nil {
				if extracted := ExtractPlayerStatsJSONFromReplay("", replayJSON, pid, pName, teamNum); extracted != "" {
					backfills = append(backfills, mpBackfill{guid: guid, playerID: pid, statsJSON: extracted})
				}
			}
		}
		mpMissingRows.Close()
		for _, bf := range backfills {
			_, _ = s.db.ExecContext(ctx, `UPDATE match_players SET stats_json = ? WHERE match_guid = ? AND player_id = ?;`, bf.statsJSON, bf.guid, bf.playerID)
		}
	}

	return nil
}

// RepairHistoricalData runs full historical match, matchup, and ranks_json repair on the SQLite database.
func (s *SQLiteStore) RepairHistoricalData(ctx context.Context) error {
	if err := s.repairHistoricalMatchesAndOutcomes(ctx); err != nil {
		return err
	}
	s.repairCorruptedRanksJSON(ctx, true)
	return nil
}

// repairCorruptedRanksJSON scans players.ranks_json and fixes rank_name / short_rank entries using the canonical 23-tier table.
// When forceAll is true, it also populates rank_name and short_rank on entries where rank_name was omitted.
func (s *SQLiteStore) repairCorruptedRanksJSON(ctx context.Context, forceAll bool) {
	rows, err := s.db.QueryContext(ctx, `SELECT player_id, ranks_json FROM players WHERE ranks_json != '' AND ranks_json != '{}';`)
	if err != nil {
		return
	}
	type playerRankRow struct {
		playerID  string
		ranksJSON string
	}
	var toCheck []playerRankRow
	for rows.Next() {
		var r playerRankRow
		if err := rows.Scan(&r.playerID, &r.ranksJSON); err == nil {
			toCheck = append(toCheck, r)
		}
	}
	rows.Close()

	for _, row := range toCheck {
		var ranks map[string]map[string]interface{}
		if err := json.Unmarshal([]byte(row.ranksJSON), &ranks); err != nil || len(ranks) == 0 {
			continue
		}
		changed := false
		for pKey, entry := range ranks {
			if entry == nil {
				continue
			}
			tier := 0
			if tVal, ok := entry["tier"].(float64); ok {
				tier = int(tVal)
			}
			division := 0
			if dVal, ok := entry["division"].(float64); ok {
				division = int(dVal)
			}
			if tier < 0 || tier > 22 {
				continue
			}
			rawName, hasRankName := entry["rank_name"].(string)
			if !hasRankName && !forceAll {
				continue
			}
			effDiv := Resolve1BasedDivision(division, rawName)
			expectedBase := FormatRankName(tier, 0)
			expectedFull := FormatRankName(tier, effDiv)
			expectedShort := FormatShortRankName(tier, effDiv)

			rawBase := strings.TrimSpace(rawName)
			if idx := strings.Index(rawBase, " Div "); idx >= 0 {
				rawBase = strings.TrimSpace(rawBase[:idx])
			} else if idx := strings.Index(rawBase, " Division "); idx >= 0 {
				rawBase = strings.TrimSpace(rawBase[:idx])
			}

			// Repair if rank_name is missing, "Tier X", or has the wrong tier base name (e.g. tier 13 labeled "Champion I" or "Diamond II")
			if !hasRankName || rawName == "" || strings.HasPrefix(rawName, "Tier ") || rawBase != expectedBase {
				entry["rank_name"] = expectedFull
				entry["short_rank"] = expectedShort
				if _, hasPID := entry["playlist_id"]; !hasPID {
					if pid, pErr := strconv.Atoi(pKey); pErr == nil && pid > 0 {
						entry["playlist_id"] = pid
						entry["playlist_name"] = FormatPlaylistName(pid)
					}
				}
				changed = true
			} else if curShort, ok := entry["short_rank"].(string); !ok || curShort == "" || curShort != expectedShort {
				if !ok || curShort == "" || forceAll || curShort != expectedShort {
					entry["short_rank"] = expectedShort
					changed = true
				}
			}
		}
		if changed {
			if updated, err := json.Marshal(ranks); err == nil {
				_, _ = s.db.ExecContext(ctx, `UPDATE players SET ranks_json = ? WHERE player_id = ?;`, string(updated), row.playerID)
			}
		}
	}
}

// PruneOldestMatches removes the oldest `count` match records (ordered by record_start_timestamp ASC, created_at ASC)
// from SQLite and returns the number of pruned matches.
func (s *SQLiteStore) PruneOldestMatches(ctx context.Context, count int) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, wrapContextErr(err)
	}
	if count <= 0 {
		return 0, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT match_guid, local_file_path
		FROM matches
		ORDER BY record_start_timestamp ASC, created_at ASC, match_guid ASC
		LIMIT ?;
	`, count)
	if err != nil {
		return 0, fmt.Errorf("failed to query oldest matches for pruning: %w", err)
	}
	defer rows.Close()

	type pruneTarget struct {
		guid      string
		localPath string
	}
	var targets []pruneTarget
	for rows.Next() {
		var t pruneTarget
		var lp sql.NullString
		if err := rows.Scan(&t.guid, &lp); err == nil {
			t.localPath = strings.TrimSpace(lp.String)
			targets = append(targets, t)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	rows.Close()
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

