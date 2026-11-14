package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type jsonStatePayload struct {
	Version          int                               `json:"version"`
	UpdatedAt        time.Time                         `json:"updated_at"`
	Matches          map[string]*MatchRecord           `json:"matches"`
	Auth             map[string]*AuthRecord            `json:"auth"`
	Players          map[string]*PlayerRecord          `json:"players,omitempty"`
	PlayerMatchups   map[string]map[int]*PlayerMatchup `json:"player_matchups,omitempty"`
	ProcessedMatches map[string]int64                  `json:"processed_matches,omitempty"`
	MatchPlayers     map[string][]*MatchPlayerRecord   `json:"match_players,omitempty"`
	MMRHistory       []*MMRHistoryRecord               `json:"mmr_history,omitempty"`
	Sessions         map[string]*SessionRecord         `json:"sessions,omitempty"`
}

func writePayloadAtomically(filePath string, payload *jsonStatePayload) error {
	dir := filepath.Dir(filePath)
	if dir == "" {
		dir = "."
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal json state: %w", err)
	}
	tmpFile, err := os.CreateTemp(dir, ".rl-sync-state-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
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
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("failed to fsync temp file %s: %w", tmpPath, err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file %s: %w", tmpPath, err)
	}
	if err := atomicRename(tmpPath, filePath); err != nil {
		return fmt.Errorf("failed to atomic rename %s to %s: %w", tmpPath, filePath, err)
	}
	success = true
	return nil
}

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

func hydrateSQLiteFromPayload(ctx context.Context, s *SQLiteStore, p *jsonStatePayload) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin json hydrate tx: %w", err)
	}
	defer tx.Rollback()

	for key, m := range p.Matches {
		if m == nil {
			continue
		}
		guid := strings.TrimSpace(m.MatchGUID)
		if guid == "" {
			guid = strings.TrimSpace(key)
		}
		if guid == "" {
			continue
		}
		syncSt := m.StatsSyncStatus
		if syncSt == "" {
			syncSt = StatsSyncPending
		}
		dlSt := m.DownloadStatus
		if dlSt == "" {
			dlSt = DownloadPending
		}
		ulSt := m.UploadStatus
		if ulSt == "" {
			ulSt = UploadPending
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO matches (
				match_guid, record_start_timestamp, map_name, playlist, replay_url,
				download_status, local_file_path, downloaded_at, upload_status,
				ballchasing_id, ballchasing_url, uploaded_at, retry_count, last_error,
				blue_score, orange_score, winner_team, replay_stats_json, synced_at,
				stats_sync_status, stats_retry_count, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);`,
			guid, m.RecordStartTimestamp, m.MapName, m.Playlist, m.ReplayURL,
			string(dlSt), m.LocalFilePath, nullableTimeUnix(m.DownloadedAt), string(ulSt),
			m.BallchasingID, m.BallchasingURL, nullableTimeUnix(m.UploadedAt), m.RetryCount, m.LastError,
			m.BlueScore, m.OrangeScore, nullableIntPtr(m.WinnerTeam), m.ReplayStatsJSON, nullableTimeUnix(m.SyncedAt),
			string(syncSt), m.StatsRetryCount, m.CreatedAt.Unix(), m.UpdatedAt.Unix(),
		); err != nil {
			return fmt.Errorf("hydrate match %s: %w", guid, err)
		}
	}

	for key, a := range p.Auth {
		if a == nil {
			continue
		}
		prov := strings.TrimSpace(a.Provider)
		if prov == "" {
			prov = strings.TrimSpace(key)
		}
		if prov == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO auth_state (provider, refresh_token, account_id, display_name, updated_at)
			VALUES (?, ?, ?, ?, ?);`, prov, a.RefreshToken, a.AccountID, a.DisplayName, a.UpdatedAt.Unix()); err != nil {
			return fmt.Errorf("hydrate auth %s: %w", prov, err)
		}
	}

	for key, pl := range p.Players {
		if pl == nil {
			continue
		}
		pid := strings.TrimSpace(pl.PlayerID)
		if pid == "" {
			pid = strings.TrimSpace(key)
		}
		if pid == "" {
			continue
		}
		ranks := pl.RanksJSON
		if strings.TrimSpace(ranks) == "" {
			ranks = "{}"
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO players (player_id, platform, player_name, player_name_lower, ranks_json, notes, tags, first_seen_at, last_seen_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);`,
			pid, pl.Platform, pl.PlayerName, strings.ToLower(pl.PlayerName),
			ranks, pl.Notes, serializeTags(pl.Tags), pl.FirstSeenAt.Unix(), pl.LastSeenAt.Unix(),
		); err != nil {
			return fmt.Errorf("hydrate player %s: %w", pid, err)
		}
	}

	for pid, byPlaylist := range p.PlayerMatchups {
		if pid == "" || len(byPlaylist) == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO players (player_id, platform, player_name, player_name_lower, ranks_json, notes, tags, first_seen_at, last_seen_at) VALUES (?, '', '', '', '{}', '', '', 0, 0);`, pid); err != nil {
			return fmt.Errorf("hydrate placeholder player %s: %w", pid, err)
		}
		for plID, m := range byPlaylist {
			if m == nil {
				continue
			}
			effPL := plID
			if m.PlaylistID != 0 {
				effPL = m.PlaylistID
			}
			var lastPlayed int64
			if !m.LastPlayedAt.IsZero() {
				lastPlayed = m.LastPlayedAt.Unix()
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT OR REPLACE INTO player_matchups (
					player_id, playlist_id, wins_as_teammate, losses_as_teammate,
					wins_as_opponent, losses_as_opponent, total_matches, last_played_at
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?);`,
				pid, effPL, m.WinsAsTeammate, m.LossesAsTeammate,
				m.WinsAsOpponent, m.LossesAsOpponent, m.TotalMatches, lastPlayed,
			); err != nil {
				return fmt.Errorf("hydrate matchup %s/%d: %w", pid, effPL, err)
			}
		}
	}

	for guid, ts := range p.ProcessedMatches {
		if strings.TrimSpace(guid) == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO processed_match_outcomes (match_guid, playlist_id, processed_at) VALUES (?, 0, ?);`, guid, ts); err != nil {
			return fmt.Errorf("hydrate processed %s: %w", guid, err)
		}
	}

	for guid, list := range p.MatchPlayers {
		for _, mp := range list {
			if mp == nil || strings.TrimSpace(mp.PlayerID) == "" {
				continue
			}
			effGUID := strings.TrimSpace(mp.MatchGUID)
			if effGUID == "" {
				effGUID = strings.TrimSpace(guid)
			}
			var wonVal *int
			if mp.Won != nil {
				v := 0
				if *mp.Won {
					v = 1
				}
				wonVal = &v
			}
			isLoc := 0
			if mp.IsLocal {
				isLoc = 1
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT OR REPLACE INTO match_players (
					match_guid, player_id, team_num, is_local, won,
					score, goals, assists, saves, shots, demos, car_name, stats_json
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);`,
				effGUID, mp.PlayerID, mp.TeamNum, isLoc, wonVal,
				mp.Score, mp.Goals, mp.Assists, mp.Saves, mp.Shots, mp.Demos, mp.CarName, mp.StatsJSON,
			); err != nil {
				return fmt.Errorf("hydrate match_player %s/%s: %w", effGUID, mp.PlayerID, err)
			}
		}
	}

	for _, r := range p.MMRHistory {
		if r == nil {
			continue
		}
		if r.ID > 0 {
			if _, err := tx.ExecContext(ctx, `
				INSERT OR REPLACE INTO mmr_history (id, recorded_at, match_guid, playlist_id, mmr, mmr_change, tier, division)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?);`,
				r.ID, r.RecordedAt.Unix(), r.MatchGUID, r.PlaylistID, r.MMR, r.MMRChange, r.Tier, r.Division,
			); err != nil {
				return fmt.Errorf("hydrate mmr %d: %w", r.ID, err)
			}
		} else {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO mmr_history (recorded_at, match_guid, playlist_id, mmr, mmr_change, tier, division)
				VALUES (?, ?, ?, ?, ?, ?, ?);`,
				r.RecordedAt.Unix(), r.MatchGUID, r.PlaylistID, r.MMR, r.MMRChange, r.Tier, r.Division,
			); err != nil {
				return fmt.Errorf("hydrate mmr: %w", err)
			}
		}
	}

	for key, sess := range p.Sessions {
		if sess == nil {
			continue
		}
		sid := strings.TrimSpace(sess.SessionID)
		if sid == "" {
			sid = strings.TrimSpace(key)
		}
		if sid == "" {
			continue
		}
		var endedAtVal, lastMatchAtVal interface{}
		if sess.EndedAt != nil && !sess.EndedAt.IsZero() {
			endedAtVal = encodeSessionTime(*sess.EndedAt)
		}
		if sess.LastMatchAt != nil && !sess.LastMatchAt.IsZero() {
			lastMatchAtVal = encodeSessionTime(*sess.LastMatchAt)
		}
		localIDs := sess.LocalPlayerIDs
		if localIDs == nil {
			localIDs = []string{}
		}
		localIDsBytes, _ := json.Marshal(localIDs)
		dismissedInt := 0
		if sess.Dismissed {
			dismissedInt = 1
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO sessions (
				session_id, started_at, ended_at, last_match_at, updated_at,
				total_matches, total_wins, total_losses, win_rate,
				playlists_json, matches_json, local_player_ids, dismissed
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);`,
			sid, encodeSessionTime(sess.StartedAt), endedAtVal, lastMatchAtVal, encodeSessionTime(sess.UpdatedAt),
			sess.TotalMatches, sess.TotalWins, sess.TotalLosses, sess.WinRate,
			sess.PlaylistsJSON, sess.MatchesJSON, string(localIDsBytes), dismissedInt,
		); err != nil {
			return fmt.Errorf("hydrate session %s: %w", sid, err)
		}
	}
	return tx.Commit()
}

func dumpSQLiteToPayload(ctx context.Context, s *SQLiteStore) (*jsonStatePayload, error) {
	payload := &jsonStatePayload{
		Version:   1,
		UpdatedAt: time.Now().UTC(),
		Matches:   make(map[string]*MatchRecord),
		Auth:      make(map[string]*AuthRecord),
	}

	if rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT %s FROM matches;`, matchSelectCols)); err != nil {
		return nil, err
	} else {
		list, scanErr := scanMatchRecords(rows)
		_ = rows.Close()
		if scanErr != nil {
			return nil, scanErr
		}
		for _, m := range list {
			payload.Matches[m.MatchGUID] = m
		}
	}

	if rows, err := s.db.QueryContext(ctx, `SELECT provider, refresh_token, account_id, display_name, updated_at FROM auth_state;`); err != nil {
		return nil, err
	} else {
		for rows.Next() {
			var a AuthRecord
			var upd int64
			if err := rows.Scan(&a.Provider, &a.RefreshToken, &a.AccountID, &a.DisplayName, &upd); err != nil {
				_ = rows.Close()
				return nil, err
			}
			a.UpdatedAt = time.Unix(upd, 0).UTC()
			payload.Auth[a.Provider] = &a
		}
		_ = rows.Close()
	}

	if rows, err := s.db.QueryContext(ctx, `SELECT player_id, platform, player_name, ranks_json, notes, tags, first_seen_at, last_seen_at FROM players;`); err != nil {
		return nil, err
	} else {
		players := make(map[string]*PlayerRecord)
		for rows.Next() {
			pl, scanErr := scanPlayerRecordRow(rows)
			if scanErr != nil {
				_ = rows.Close()
				return nil, scanErr
			}
			players[pl.PlayerID] = pl
		}
		_ = rows.Close()
		if len(players) > 0 {
			payload.Players = players
		}
	}

	if rows, err := s.db.QueryContext(ctx, `SELECT player_id, playlist_id, wins_as_teammate, losses_as_teammate, wins_as_opponent, losses_as_opponent, total_matches, last_played_at FROM player_matchups;`); err != nil {
		return nil, err
	} else {
		matchups := make(map[string]map[int]*PlayerMatchup)
		for rows.Next() {
			m, scanErr := scanPlayerMatchupRow(rows)
			if scanErr != nil {
				_ = rows.Close()
				return nil, scanErr
			}
			if matchups[m.PlayerID] == nil {
				matchups[m.PlayerID] = make(map[int]*PlayerMatchup)
			}
			matchups[m.PlayerID][m.PlaylistID] = m
		}
		_ = rows.Close()
		if len(matchups) > 0 {
			payload.PlayerMatchups = matchups
		}
	}

	if rows, err := s.db.QueryContext(ctx, `SELECT match_guid, processed_at FROM processed_match_outcomes;`); err != nil {
		return nil, err
	} else {
		proc := make(map[string]int64)
		for rows.Next() {
			var g string
			var ts int64
			if err := rows.Scan(&g, &ts); err != nil {
				_ = rows.Close()
				return nil, err
			}
			proc[g] = ts
		}
		_ = rows.Close()
		if len(proc) > 0 {
			payload.ProcessedMatches = proc
		}
	}

	if rows, err := s.db.QueryContext(ctx, `SELECT match_guid, player_id, team_num, is_local, won, score, goals, assists, saves, shots, demos, car_name, stats_json FROM match_players ORDER BY match_guid ASC, team_num ASC, score DESC;`); err != nil {
		return nil, err
	} else {
		mps := make(map[string][]*MatchPlayerRecord)
		for rows.Next() {
			var mp MatchPlayerRecord
			var isLoc int
			var wonVal sql.NullInt64
			if err := rows.Scan(&mp.MatchGUID, &mp.PlayerID, &mp.TeamNum, &isLoc, &wonVal, &mp.Score, &mp.Goals, &mp.Assists, &mp.Saves, &mp.Shots, &mp.Demos, &mp.CarName, &mp.StatsJSON); err != nil {
				_ = rows.Close()
				return nil, err
			}
			mp.IsLocal = isLoc == 1
			if wonVal.Valid {
				w := wonVal.Int64 == 1
				mp.Won = &w
			}
			mps[mp.MatchGUID] = append(mps[mp.MatchGUID], &mp)
		}
		_ = rows.Close()
		if len(mps) > 0 {
			payload.MatchPlayers = mps
		}
	}

	if rows, err := s.db.QueryContext(ctx, `SELECT id, recorded_at, match_guid, playlist_id, mmr, mmr_change, tier, division FROM mmr_history ORDER BY id ASC;`); err != nil {
		return nil, err
	} else {
		var hist []*MMRHistoryRecord
		for rows.Next() {
			var r MMRHistoryRecord
			var recUnix int64
			if err := rows.Scan(&r.ID, &recUnix, &r.MatchGUID, &r.PlaylistID, &r.MMR, &r.MMRChange, &r.Tier, &r.Division); err != nil {
				_ = rows.Close()
				return nil, err
			}
			r.RecordedAt = time.Unix(recUnix, 0).UTC()
			hist = append(hist, &r)
		}
		_ = rows.Close()
		if len(hist) > 0 {
			payload.MMRHistory = hist
		}
	}

	if rows, err := s.db.QueryContext(ctx, `SELECT session_id, started_at, ended_at, last_match_at, updated_at, total_matches, total_wins, total_losses, win_rate, playlists_json, matches_json, local_player_ids, dismissed FROM sessions;`); err != nil {
		return nil, err
	} else {
		sessions := make(map[string]*SessionRecord)
		for rows.Next() {
			rec, scanErr := scanSessionRow(rows)
			if scanErr != nil {
				_ = rows.Close()
				return nil, scanErr
			}
			sessions[rec.SessionID] = rec
		}
		_ = rows.Close()
		if len(sessions) > 0 {
			payload.Sessions = sessions
		}
	}
	return payload, nil
}

func nullableTimeUnix(t *time.Time) interface{} {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.UTC().Unix()
}

func nullableIntPtr(v *int) interface{} {
	if v == nil {
		return nil
	}
	return *v
}

func loadJSONStoreEngine(filePath string) (*JSONStore, error) {
	if filePath == "" {
		return nil, fmt.Errorf("filePath cannot be empty")
	}
	dir := filepath.Dir(filePath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory for json store %s: %w", dir, err)
		}
	}

	inner, err := newIsolatedMemorySQLiteStore(false)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize backing engine for json store: %w", err)
	}
	s := &JSONStore{filePath: filePath, inner: inner}

	info, statErr := os.Stat(filePath)
	if os.IsNotExist(statErr) {
		if err := s.saveLocked(); err != nil {
			_ = inner.Close()
			return nil, fmt.Errorf("failed to initialize empty json store %s: %w", filePath, err)
		}
		return s, nil
	} else if statErr != nil {
		_ = inner.Close()
		return nil, fmt.Errorf("failed to stat json store %s: %w", filePath, statErr)
	}

	if info.Size() == 0 {
		if err := s.saveLocked(); err != nil {
			_ = inner.Close()
			return nil, fmt.Errorf("failed to write initial structure to %s: %w", filePath, err)
		}
		return s, nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		_ = inner.Close()
		return nil, fmt.Errorf("failed to read json store %s: %w", filePath, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		if err := s.saveLocked(); err != nil {
			_ = inner.Close()
			return nil, fmt.Errorf("failed to write initial structure to %s: %w", filePath, err)
		}
		return s, nil
	}

	var payload jsonStatePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		_ = inner.Close()
		return nil, fmt.Errorf("failed to unmarshal json store %s: %w", filePath, err)
	}

	ctx := context.Background()
	if err := hydrateSQLiteFromPayload(ctx, inner, &payload); err != nil {
		_ = inner.Close()
		return nil, fmt.Errorf("failed to load json store %s: %w", filePath, err)
	}
	_ = inner.repairZeroScoreMatches(ctx)
	_ = migrateMatchPlayers(ctx, inner.db)
	cleanupStaleTempFiles(dir)
	return s, nil
}

func withRead[T any](s *JSONStore, fn func() (T, error)) (T, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		var zero T
		return zero, ErrStoreClosed
	}
	return fn()
}

func withRead2[A, B any](s *JSONStore, fn func() (A, B, error)) (A, B, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		var zeroA A
		var zeroB B
		return zeroA, zeroB, ErrStoreClosed
	}
	return fn()
}

func (s *JSONStore) withWrite(fn func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrStoreClosed
	}
	if err := fn(); err != nil {
		return err
	}
	return s.saveLocked()
}
