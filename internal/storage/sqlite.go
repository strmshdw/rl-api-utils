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

CREATE TABLE IF NOT EXISTS players (
    player_id TEXT PRIMARY KEY,
    platform TEXT NOT NULL DEFAULT '',
    player_name TEXT NOT NULL DEFAULT '',
    player_name_lower TEXT NOT NULL DEFAULT '',
    ranks_json TEXT NOT NULL DEFAULT '{}',
    first_seen_at INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_players_last_seen ON players(last_seen_at DESC);
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

	// Run table migration before schemaDDL to guarantee player_name_lower exists before index creation
	if err := migratePlayersTable(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate schema: %w", err)
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

// migratePlayersTable ensures the players table has player_name_lower column and backfills existing rows.
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

	hasCol := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, colType string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("failed to scan table_info row: %w", err)
		}
		if strings.EqualFold(name, "player_name_lower") {
			hasCol = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("table_info iteration error: %w", err)
	}

	if !hasCol {
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

// UpsertPlayer inserts a new player or updates an existing player's platform, name, and last_seen_at.
// Preserves first_seen_at and existing ranks_json on update.
func (s *SQLiteStore) UpsertPlayer(ctx context.Context, player *PlayerRecord) error {
	if player == nil {
		return errors.New("player cannot be nil")
	}
	if strings.TrimSpace(player.PlayerID) == "" {
		return errors.New("player_id cannot be empty")
	}

	now := time.Now().UTC()
	firstSeenEpoch := player.FirstSeenAt.Unix()
	if firstSeenEpoch <= 0 {
		firstSeenEpoch = now.Unix()
	}
	lastSeenEpoch := player.LastSeenAt.Unix()
	if lastSeenEpoch <= 0 {
		lastSeenEpoch = now.Unix()
	}

	ranksJSON := player.RanksJSON
	if strings.TrimSpace(ranksJSON) == "" {
		ranksJSON = "{}"
	}

	playerNameLower := strings.ToLower(player.PlayerName)

	const query = `
	INSERT INTO players (player_id, platform, player_name, player_name_lower, ranks_json, first_seen_at, last_seen_at)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(player_id) DO UPDATE SET
		platform = CASE WHEN excluded.platform != '' THEN excluded.platform ELSE players.platform END,
		player_name = CASE WHEN excluded.player_name != '' THEN excluded.player_name ELSE players.player_name END,
		player_name_lower = CASE WHEN excluded.player_name != '' THEN excluded.player_name_lower ELSE players.player_name_lower END,
		ranks_json = CASE WHEN excluded.ranks_json != '' AND excluded.ranks_json != '{}' THEN excluded.ranks_json ELSE players.ranks_json END,
		last_seen_at = excluded.last_seen_at;
	`

	_, err := s.db.ExecContext(ctx, query,
		player.PlayerID,
		player.Platform,
		player.PlayerName,
		playerNameLower,
		ranksJSON,
		firstSeenEpoch,
		lastSeenEpoch,
	)
	if err != nil {
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
	SELECT player_id, platform, player_name, ranks_json, first_seen_at, last_seen_at
	FROM players
	WHERE player_id = ?;
	`

	var (
		p         PlayerRecord
		firstSeen int64
		lastSeen  int64
	)

	err := s.db.QueryRowContext(ctx, query, playerID).Scan(
		&p.PlayerID,
		&p.Platform,
		&p.PlayerName,
		&p.RanksJSON,
		&firstSeen,
		&lastSeen,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPlayerNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get player %s: %w", playerID, err)
	}

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
	SELECT player_id, platform, player_name, ranks_json, first_seen_at, last_seen_at
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
			firstSeen int64
			lastSeen  int64
		)
		if err := rows.Scan(
			&p.PlayerID,
			&p.Platform,
			&p.PlayerName,
			&p.RanksJSON,
			&firstSeen,
			&lastSeen,
		); err != nil {
			return nil, fmt.Errorf("failed to scan player row: %w", err)
		}
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
	SELECT p.player_id, p.platform, p.player_name, p.ranks_json, p.first_seen_at, p.last_seen_at,
	       COALESCE(SUM(m.wins_as_teammate), 0) AS total_wins_teammate,
	       COALESCE(SUM(m.losses_as_teammate), 0) AS total_losses_teammate,
	       COALESCE(SUM(m.wins_as_opponent), 0) AS total_wins_opponent,
	       COALESCE(SUM(m.losses_as_opponent), 0) AS total_losses_opponent,
	       COALESCE(SUM(m.total_matches), 0) AS total_matches
	FROM players p
	LEFT JOIN player_matchups m ON p.player_id = m.player_id
	GROUP BY p.player_id
	ORDER BY p.last_seen_at DESC, p.player_id ASC
	LIMIT ? OFFSET ?;
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
			firstSeen int64
			lastSeen  int64
		)
		if err := rows.Scan(
			&sRec.PlayerID,
			&sRec.Platform,
			&sRec.PlayerName,
			&sRec.RanksJSON,
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

// SearchPlayerSummaries searches players matching a query string across player_name and player_id
// (case-insensitive substring match), optionally filtered by platform (case-insensitive; "all" or "" means
// all platforms), returning paginated PlayerSummary records and the total count of matching records across all pages.
// Results are ordered by last_seen_at DESC, player_id ASC.
func (s *SQLiteStore) SearchPlayerSummaries(ctx context.Context, query, platform string, limit, offset int) ([]*PlayerSummary, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}

	cleanQuery := strings.TrimSpace(query)
	cleanPlatform := strings.TrimSpace(strings.ToLower(platform))

	var whereClauses []string
	var whereArgs []interface{}

	if cleanPlatform != "" && cleanPlatform != "all" {
		whereClauses = append(whereClauses, "LOWER(p.platform) = ?")
		whereArgs = append(whereArgs, cleanPlatform)
	}

	if cleanQuery != "" {
		lowerQuery := strings.ToLower(cleanQuery)
		if strings.ContainsRune(lowerQuery, '\x00') {
			whereClauses = append(whereClauses, "(INSTR(p.player_name_lower, ?) > 0 OR INSTR(LOWER(p.player_id), ?) > 0)")
			whereArgs = append(whereArgs, lowerQuery, lowerQuery)
		} else {
			whereClauses = append(whereClauses, "(p.player_name_lower LIKE ? ESCAPE '\\' OR LOWER(p.player_id) LIKE ? ESCAPE '\\')")
			escaped := "%" + escapeLike(lowerQuery) + "%"
			whereArgs = append(whereArgs, escaped, escaped)
		}
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = " WHERE " + strings.Join(whereClauses, " AND ")
	}

	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM players p%s;", whereSQL)
	var total int
	if err := s.db.QueryRowContext(ctx, countSQL, whereArgs...).Scan(&total); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, 0, ctxErr
		}
		return nil, 0, fmt.Errorf("failed to count matching players: %w", err)
	}

	// Boundary shortcuts: empty dataset or count-only request
	if total == 0 || limit == 0 {
		return make([]*PlayerSummary, 0), total, nil
	}

	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return make([]*PlayerSummary, 0), total, nil
	}

	limitArg := limit
	if limitArg < 0 {
		limitArg = -1
	}

	querySQL := fmt.Sprintf(`
	SELECT p.player_id, p.platform, p.player_name, p.ranks_json, p.first_seen_at, p.last_seen_at,
	       COALESCE(SUM(m.wins_as_teammate), 0) AS total_wins_teammate,
	       COALESCE(SUM(m.losses_as_teammate), 0) AS total_losses_teammate,
	       COALESCE(SUM(m.wins_as_opponent), 0) AS total_wins_opponent,
	       COALESCE(SUM(m.losses_as_opponent), 0) AS total_losses_opponent,
	       COALESCE(SUM(m.total_matches), 0) AS total_matches
	FROM players p
	LEFT JOIN player_matchups m ON p.player_id = m.player_id%s
	GROUP BY p.player_id
	ORDER BY p.last_seen_at DESC, p.player_id ASC
	LIMIT ? OFFSET ?;
	`, whereSQL)

	queryArgs := make([]interface{}, len(whereArgs)+2)
	copy(queryArgs, whereArgs)
	queryArgs[len(whereArgs)] = limitArg
	queryArgs[len(whereArgs)+1] = offset

	rows, err := s.db.QueryContext(ctx, querySQL, queryArgs...)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, 0, ctxErr
		}
		return nil, 0, fmt.Errorf("failed to search player summaries: %w", err)
	}
	defer rows.Close()

	summaries := make([]*PlayerSummary, 0)
	for rows.Next() {
		var (
			sRec      PlayerSummary
			firstSeen int64
			lastSeen  int64
		)
		if err := rows.Scan(
			&sRec.PlayerID,
			&sRec.Platform,
			&sRec.PlayerName,
			&sRec.RanksJSON,
			&firstSeen,
			&lastSeen,
			&sRec.TotalWinsAsTeammate,
			&sRec.TotalLossesAsTeammate,
			&sRec.TotalWinsAsOpponent,
			&sRec.TotalLossesAsOpponent,
			&sRec.TotalMatches,
		); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, 0, ctxErr
			}
			return nil, 0, fmt.Errorf("failed to scan player summary row: %w", err)
		}
		sRec.FirstSeenAt = time.Unix(firstSeen, 0).UTC()
		sRec.LastSeenAt = time.Unix(lastSeen, 0).UTC()
		summaries = append(summaries, &sRec)
	}
	if err := rows.Err(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, 0, ctxErr
		}
		return nil, 0, fmt.Errorf("rows iteration error in search player summaries: %w", err)
	}

	return summaries, total, nil
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
	SET ranks_json = ?, last_seen_at = ?
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
func (s *SQLiteStore) RecordMatchResults(ctx context.Context, matchGUID string, playlistID int, outcomes []PlayerOutcome) error {
	if strings.TrimSpace(matchGUID) == "" {
		return ErrInvalidGUID
	}

	now := time.Now().UTC().Unix()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin match results tx: %w", err)
	}
	defer tx.Rollback()

	// 1. Atomic insertion into processed_match_outcomes ledger.
	// If matchGUID already exists, UNIQUE constraint fails -> return ErrMatchAlreadyProcessed.
	const insertLedgerSQL = `
	INSERT INTO processed_match_outcomes (match_guid, playlist_id, processed_at)
	VALUES (?, ?, ?);
	`
	_, err = tx.ExecContext(ctx, insertLedgerSQL, matchGUID, playlistID, now)
	if err != nil {
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
		last_seen_at = excluded.last_seen_at;
	`
	stmtPlayer, err := tx.PrepareContext(ctx, upsertPlayerSQL)
	if err != nil {
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
		last_played_at = excluded.last_played_at;
	`
	stmtMatchup, err := tx.PrepareContext(ctx, upsertMatchupSQL)
	if err != nil {
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
			now,
			now,
		); err != nil {
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
			now,
		); err != nil {
			return fmt.Errorf("failed to upsert matchup for player %s in playlist %d: %w", outcome.PlayerID, playlistID, err)
		}
	}

	return tx.Commit()
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
