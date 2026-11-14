package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
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
CREATE INDEX IF NOT EXISTS idx_match_players_local_team ON match_players(match_guid, is_local, team_num);
CREATE INDEX IF NOT EXISTS idx_match_players_local_player ON match_players(is_local, player_id);

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

func runSchemaMigrations(ctx context.Context, db *sql.DB) error {
	if err := migrateMatchesTable(ctx, db); err != nil {
		return fmt.Errorf("failed to migrate matches schema: %w", err)
	}
	if err := migratePlayersTable(ctx, db); err != nil {
		return fmt.Errorf("failed to migrate schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, schemaDDL); err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}
	if err := migrateSessionsTable(ctx, db); err != nil {
		return fmt.Errorf("failed to migrate sessions schema: %w", err)
	}
	if err := migrateMatchPlayers(ctx, db); err != nil {
		return fmt.Errorf("failed to backfill match_players: %w", err)
	}
	return nil
}

func tableColumns(ctx context.Context, db *sql.DB, tableName string) (map[string]bool, bool, error) {
	var tableCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?;", tableName).Scan(&tableCount); err != nil {
		return nil, false, fmt.Errorf("failed to check %s table existence: %w", tableName, err)
	}
	if tableCount == 0 {
		return nil, false, nil
	}

	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s);", tableName))
	if err != nil {
		return nil, true, fmt.Errorf("failed to read %s table_info: %w", tableName, err)
	}
	defer rows.Close()

	cols := make(map[string]bool)
	for rows.Next() {
		var cid, notnull, pk int
		var name, colType string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notnull, &dflt, &pk); err != nil {
			return nil, true, fmt.Errorf("failed to scan %s table_info: %w", tableName, err)
		}
		cols[strings.ToLower(name)] = true
	}
	if err := rows.Err(); err != nil {
		return nil, true, fmt.Errorf("%s table_info iteration error: %w", tableName, err)
	}
	return cols, true, nil
}

func migratePlayersTable(ctx context.Context, db *sql.DB) error {
	cols, exists, err := tableColumns(ctx, db, "players")
	if err != nil || !exists {
		return err
	}

	if !cols["player_name_lower"] {
		if _, err := db.ExecContext(ctx, `ALTER TABLE players ADD COLUMN player_name_lower TEXT NOT NULL DEFAULT '';`); err != nil {
			return fmt.Errorf("failed to alter table players: %w", err)
		}
		if err := backfillPlayerNameLower(ctx, db); err != nil {
			return err
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

func backfillPlayerNameLower(ctx context.Context, db *sql.DB) error {
	pRows, err := db.QueryContext(ctx, `SELECT player_id, player_name FROM players WHERE player_name != '';`)
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
			_ = pRows.Close()
			return fmt.Errorf("failed to scan player for backfill: %w", err)
		}
		toUpdate = append(toUpdate, pe)
	}
	_ = pRows.Close()
	if len(toUpdate) == 0 {
		return nil
	}

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
	return tx.Commit()
}

func migrateMatchesTable(ctx context.Context, db *sql.DB) error {
	cols, exists, err := tableColumns(ctx, db, "matches")
	if err != nil || !exists {
		return err
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

func migrateSessionsTable(ctx context.Context, db *sql.DB) error {
	cols, exists, err := tableColumns(ctx, db, "sessions")
	if err != nil || !exists {
		return err
	}
	if !cols["dismissed"] {
		if _, err := db.ExecContext(ctx, "ALTER TABLE sessions ADD COLUMN dismissed INTEGER NOT NULL DEFAULT 0;"); err != nil {
			return fmt.Errorf("failed to add dismissed column to sessions: %w", err)
		}
	}
	return nil
}

func migrateMatchPlayers(ctx context.Context, db *sql.DB) error {
	if _, hasMatches, _ := tableColumns(ctx, db, "matches"); !hasMatches {
		return nil
	}
	if _, hasMP, _ := tableColumns(ctx, db, "match_players"); !hasMP {
		return nil
	}

	rows, err := db.QueryContext(ctx, `
		SELECT m.match_guid, m.winner_team, m.replay_stats_json
		FROM matches m
		WHERE m.replay_stats_json != ''
		  AND NOT EXISTS (SELECT 1 FROM match_players mp WHERE mp.match_guid = m.match_guid);
	`)
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
			score, goals, assists, saves, shots, demos, car_name, stats_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(match_guid, player_id) DO NOTHING;
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare insert match_players stmt: %w", err)
	}
	defer stmt.Close()

	for _, m := range matches {
		var st ReplayMatchStats
		if json.Unmarshal([]byte(m.statsJSON), &st) != nil {
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
			if m.winnerTeam.Valid && m.winnerTeam.Int64 >= 0 {
				w := 0
				if int(m.winnerTeam.Int64) == p.Team {
					w = 1
				}
				wonVal = &w
			}
			if _, err := stmt.ExecContext(ctx,
				m.guid, canonicalID, p.Team, 0, wonVal,
				p.Score, p.Goals, p.Assists, p.Saves, p.Shots, p.Demos,
				strings.TrimSpace(p.CarName), marshalTelemetryIfPresent(p),
			); err != nil {
				return fmt.Errorf("failed to backfill match_player %s in %s: %w", canonicalID, m.guid, err)
			}
		}
	}
	return tx.Commit()
}

func marshalTelemetryIfPresent(p ReplayPlayerStats) string {
	if p.Core != nil || p.Boost != nil || p.Movement != nil || p.Positioning != nil || p.Demo != nil || p.Ball != nil || p.Camera != nil || p.Rank != nil {
		if b, err := json.Marshal(p); err == nil {
			return string(b)
		}
	}
	return ""
}

// RecoverInFlight resets DOWNLOADING, UPLOADING, and SYNCING rows to PENDING after abnormal exit.
// Preserves terminal SYNCED states including 0-player Ballchasing replays (017db99).
func (s *SQLiteStore) RecoverInFlight(ctx context.Context) error {
	now := time.Now().UTC().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin recovery tx: %w", err)
	}
	defer tx.Rollback()

	if _, err = tx.ExecContext(ctx,
		`UPDATE matches SET download_status = ?, updated_at = ? WHERE download_status = ?;`,
		string(DownloadPending), now, string(DownloadDownloading),
	); err != nil {
		return fmt.Errorf("failed to reset in-flight downloads: %w", err)
	}
	if _, err = tx.ExecContext(ctx,
		`UPDATE matches SET upload_status = ?, updated_at = ? WHERE upload_status = ?;`,
		string(UploadPending), now, string(UploadUploading),
	); err != nil {
		return fmt.Errorf("failed to reset in-flight uploads: %w", err)
	}
	if _, err = tx.ExecContext(ctx,
		`UPDATE matches SET stats_sync_status = ?, updated_at = ? WHERE stats_sync_status = ?;`,
		string(StatsSyncPending), now, string(StatsSyncSyncing),
	); err != nil {
		return fmt.Errorf("failed to reset in-flight stats sync: %w", err)
	}
	return tx.Commit()
}
