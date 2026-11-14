package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func encodeSessionTime(t time.Time) int64 {
	return t.UTC().UnixNano()
}

func decodeSessionTime(val int64) time.Time {
	if val == 0 {
		return time.Time{}
	}
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

	var endedAtVal, lastMatchAtVal interface{}
	if session.EndedAt != nil && !session.EndedAt.IsZero() {
		endedAtVal = encodeSessionTime(*session.EndedAt)
	}
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
		sessionID, encodeSessionTime(startedAt), endedAtVal, lastMatchAtVal, encodeSessionTime(updatedAt),
		session.TotalMatches, session.TotalWins, session.TotalLosses, session.WinRate,
		playlistsJSON, matchesJSON, string(localIDsBytes), dismissedInt,
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
	FROM sessions WHERE session_id = ?;
	`
	rec, err := scanSessionRow(s.db.QueryRowContext(ctx, query, sessionID))
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

	records := make([]*SessionRecord, 0)
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
	if _, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE session_id = ?;", sessionID); err != nil {
		return wrapContextErr(fmt.Errorf("failed to delete session %s: %w", sessionID, err))
	}
	return nil
}

func scanSessionRow(scanner rowScanner) (*SessionRecord, error) {
	var (
		rec          SessionRecord
		startedAtRaw int64
		endedAtRaw   sql.NullInt64
		lastMatchRaw sql.NullInt64
		updatedAtRaw int64
		localIDsRaw  string
		dismissedInt int
	)
	if err := scanner.Scan(
		&rec.SessionID, &startedAtRaw, &endedAtRaw, &lastMatchRaw, &updatedAtRaw,
		&rec.TotalMatches, &rec.TotalWins, &rec.TotalLosses, &rec.WinRate,
		&rec.PlaylistsJSON, &rec.MatchesJSON, &localIDsRaw, &dismissedInt,
	); err != nil {
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
