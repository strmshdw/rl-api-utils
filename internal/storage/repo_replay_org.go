package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dank/rl-api-utils/internal/domain"
)

const (
	// CounterTypeGame identifies the daily game replay autonumber sequence.
	CounterTypeGame = "game"
	// CounterTypeSession identifies the daily session autonumber sequence.
	CounterTypeSession = "session"
)

type sessionMatchLookupEntry struct {
	MatchGUID  string `json:"match_guid"`
	PlaylistID int    `json:"playlist_id"`
	Result     string `json:"result"`
	LocalTeam  *int   `json:"local_team,omitempty"`
	WinnerTeam *int   `json:"winner_team,omitempty"`
}

func normalizeGUIDKey(guid string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(guid), "-", ""))
}

// GetDailyCounter returns the last allocated value for (counterType, dateKey), or 0 if none exists.
func (s *SQLiteStore) GetDailyCounter(ctx context.Context, counterType, dateKey string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, wrapContextErr(err)
	}
	var val int
	err := s.db.QueryRowContext(ctx,
		`SELECT last_value FROM daily_counters WHERE counter_type = ? AND date_key = ?;`,
		strings.TrimSpace(counterType), strings.TrimSpace(dateKey),
	).Scan(&val)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, wrapContextErr(fmt.Errorf("failed to get daily counter %s/%s: %w", counterType, dateKey, err))
	}
	return val, nil
}

func isSQLiteBusyErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "sqlite_busy") || strings.Contains(msg, "database is locked") || strings.Contains(msg, "database table is locked")
}

func runSQLiteBusyRetry(ctx context.Context, fn func() error) error {
	const maxAttempts = 12
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return wrapContextErr(ctxErr)
		}
		err = fn()
		if err == nil || !isSQLiteBusyErr(err) {
			return err
		}
		if attempt < maxAttempts-1 {
			timer := time.NewTimer(time.Duration((attempt+1)*10) * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return wrapContextErr(ctx.Err())
			case <-timer.C:
			}
		}
	}
	return err
}

// AllocateMatchReplayNaming atomically assigns (or reuses) a daily game autonumber and canonical
// replay filename YYYY_MM_DD_XXX_GameMode_Result.replay for matchGUID.
// Retries and re-downloads of the same matchGUID reuse its persisted autonumber without burning a new slot.
func (s *SQLiteStore) AllocateMatchReplayNaming(
	ctx context.Context,
	matchGUID, dateKey, gameMode, result string,
	minNextNum int,
) (*ReplayNamingAssignment, error) {
	var out *ReplayNamingAssignment
	err := runSQLiteBusyRetry(ctx, func() error {
		var innerErr error
		out, innerErr = s.allocateMatchReplayNamingOnce(ctx, matchGUID, dateKey, gameMode, result, minNextNum)
		return innerErr
	})
	return out, err
}

func (s *SQLiteStore) allocateMatchReplayNamingOnce(
	ctx context.Context,
	matchGUID, dateKey, gameMode, result string,
	minNextNum int,
) (*ReplayNamingAssignment, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContextErr(err)
	}
	matchGUID = strings.TrimSpace(matchGUID)
	if matchGUID == "" {
		return nil, ErrInvalidGUID
	}
	dateKey = strings.TrimSpace(dateKey)
	if dateKey == "" {
		dateKey = time.Now().Format("2006_01_02")
	}
	mode := domain.NormalizeGameModeCategory(gameMode)
	res := domain.NormalizeReplayResult(result)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, wrapContextErr(fmt.Errorf("failed to begin replay naming tx: %w", err))
	}
	defer tx.Rollback()

	var (
		existingName    sql.NullString
		existingDateKey sql.NullString
		existingNum     sql.NullInt64
		existingMode    sql.NullString
	)
	err = tx.QueryRowContext(ctx,
		`SELECT replay_name, replay_date_key, daily_game_num, game_mode FROM matches WHERE match_guid = ?;`,
		matchGUID,
	).Scan(&existingName, &existingDateKey, &existingNum, &existingMode)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMatchNotFound
	}
	if err != nil {
		return nil, wrapContextErr(fmt.Errorf("failed to inspect match %s naming: %w", matchGUID, err))
	}

	nowUnix := time.Now().UTC().Unix()

	if existingNum.Valid && existingNum.Int64 > 0 && strings.TrimSpace(existingDateKey.String) != "" {
		dk := strings.TrimSpace(existingDateKey.String)
		num := int(existingNum.Int64)
		effMode := strings.TrimSpace(existingMode.String)
		if effMode == "" {
			effMode = mode
		}
		effName := strings.TrimSpace(existingName.String)
		needsUpdate := false
		if effName == "" || (strings.HasSuffix(effName, "_"+domain.ReplayResultUnknown+".replay") && res != domain.ReplayResultUnknown) {
			effName = domain.FormatReplayFileName(dk, num, effMode, res)
			needsUpdate = true
		}
		if strings.TrimSpace(existingMode.String) == "" && effMode != "" {
			needsUpdate = true
		}
		if needsUpdate {
			if _, err := tx.ExecContext(ctx,
				`UPDATE matches SET replay_name = ?, game_mode = ?, updated_at = ? WHERE match_guid = ?;`,
				effName, effMode, nowUnix, matchGUID,
			); err != nil {
				return nil, wrapContextErr(fmt.Errorf("failed to update reused replay naming for %s: %w", matchGUID, err))
			}
		}
		if err := tx.Commit(); err != nil {
			return nil, wrapContextErr(err)
		}
		return &ReplayNamingAssignment{
			MatchGUID:    matchGUID,
			DateKey:      dk,
			DailyGameNum: num,
			GameMode:     effMode,
			Result:       res,
			ReplayName:   effName,
			Reused:       true,
		}, nil
	}

	var lastVal int
	if scanErr := tx.QueryRowContext(ctx,
		`SELECT last_value FROM daily_counters WHERE counter_type = ? AND date_key = ?;`,
		CounterTypeGame, dateKey,
	).Scan(&lastVal); scanErr != nil && !errors.Is(scanErr, sql.ErrNoRows) {
		return nil, wrapContextErr(fmt.Errorf("failed to query daily game counter for %s: %w", dateKey, scanErr))
	}

	nextNum := lastVal + 1
	if minNextNum > nextNum {
		nextNum = minNextNum
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO daily_counters (counter_type, date_key, last_value, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(counter_type, date_key) DO UPDATE SET
			last_value = excluded.last_value,
			updated_at = excluded.updated_at;`,
		CounterTypeGame, dateKey, nextNum, nowUnix,
	); err != nil {
		return nil, wrapContextErr(fmt.Errorf("failed to increment daily game counter for %s: %w", dateKey, err))
	}

	replayName := domain.FormatReplayFileName(dateKey, nextNum, mode, res)
	if _, err := tx.ExecContext(ctx, `
		UPDATE matches
		SET replay_name = ?, replay_date_key = ?, daily_game_num = ?, game_mode = ?, updated_at = ?
		WHERE match_guid = ?;`,
		replayName, dateKey, nextNum, mode, nowUnix, matchGUID,
	); err != nil {
		return nil, wrapContextErr(fmt.Errorf("failed to persist replay naming for %s: %w", matchGUID, err))
	}

	if err := tx.Commit(); err != nil {
		return nil, wrapContextErr(err)
	}
	return &ReplayNamingAssignment{
		MatchGUID:    matchGUID,
		DateKey:      dateKey,
		DailyGameNum: nextNum,
		GameMode:     mode,
		Result:       res,
		ReplayName:   replayName,
		Reused:       false,
	}, nil
}

// UpdateMatchReplayFile updates a match's local file path and replay naming metadata.
func (s *SQLiteStore) UpdateMatchReplayFile(ctx context.Context, matchGUID, localPath, replayName, gameMode string) error {
	if err := ctx.Err(); err != nil {
		return wrapContextErr(err)
	}
	matchGUID = strings.TrimSpace(matchGUID)
	if matchGUID == "" {
		return ErrInvalidGUID
	}
	now := time.Now().UTC().Unix()
	res, err := s.db.ExecContext(ctx, `
		UPDATE matches
		SET local_file_path = CASE WHEN ? != '' THEN ? ELSE local_file_path END,
		    replay_name = CASE WHEN ? != '' THEN ? ELSE replay_name END,
		    game_mode = CASE WHEN ? != '' THEN ? ELSE game_mode END,
		    updated_at = ?
		WHERE match_guid = ?;`,
		localPath, localPath, replayName, replayName, gameMode, gameMode, now, matchGUID,
	)
	if err != nil {
		return wrapContextErr(fmt.Errorf("failed to update match replay file for %s: %w", matchGUID, err))
	}
	return checkRowsAffected(res)
}

// UpdateMatchGrouping persists the resolved session ID, game mode, and Ballchasing group ID for a match.
func (s *SQLiteStore) UpdateMatchGrouping(ctx context.Context, matchGUID, sessionID, gameMode, ballchasingGroupID string) error {
	if err := ctx.Err(); err != nil {
		return wrapContextErr(err)
	}
	matchGUID = strings.TrimSpace(matchGUID)
	if matchGUID == "" {
		return ErrInvalidGUID
	}
	now := time.Now().UTC().Unix()
	res, err := s.db.ExecContext(ctx, `
		UPDATE matches
		SET session_id = CASE WHEN ? != '' THEN ? ELSE session_id END,
		    game_mode = CASE WHEN ? != '' THEN ? ELSE game_mode END,
		    ballchasing_group_id = CASE WHEN ? != '' THEN ? ELSE ballchasing_group_id END,
		    updated_at = ?
		WHERE match_guid = ?;`,
		sessionID, sessionID, gameMode, gameMode, ballchasingGroupID, ballchasingGroupID, now, matchGUID,
	)
	if err != nil {
		return wrapContextErr(fmt.Errorf("failed to update match grouping for %s: %w", matchGUID, err))
	}
	return checkRowsAffected(res)
}

// GetOrAllocateSessionSequence atomically returns the existing daily SessionNum for sessionID,
// or allocates the next monotonic SessionNum for dateKey and persists it.
func (s *SQLiteStore) GetOrAllocateSessionSequence(ctx context.Context, sessionID, dateKey string) (*SessionSequenceRecord, error) {
	var out *SessionSequenceRecord
	err := runSQLiteBusyRetry(ctx, func() error {
		var innerErr error
		out, innerErr = s.getOrAllocateSessionSequenceOnce(ctx, sessionID, dateKey)
		return innerErr
	})
	return out, err
}

func (s *SQLiteStore) getOrAllocateSessionSequenceOnce(ctx context.Context, sessionID, dateKey string) (*SessionSequenceRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContextErr(err)
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("session ID cannot be empty")
	}
	dateKey = strings.TrimSpace(dateKey)
	if dateKey == "" {
		dateKey = time.Now().Format("2006_01_02")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, wrapContextErr(fmt.Errorf("failed to begin session sequence tx: %w", err))
	}
	defer tx.Rollback()

	var (
		rec       SessionSequenceRecord
		createdAt int64
	)
	err = tx.QueryRowContext(ctx,
		`SELECT session_id, date_key, session_num, created_at FROM session_sequences WHERE session_id = ?;`,
		sessionID,
	).Scan(&rec.SessionID, &rec.DateKey, &rec.SessionNum, &createdAt)
	if err == nil {
		rec.CreatedAt = time.Unix(createdAt, 0).UTC()
		if commitErr := tx.Commit(); commitErr != nil {
			return nil, wrapContextErr(commitErr)
		}
		return &rec, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, wrapContextErr(fmt.Errorf("failed to lookup session sequence %s: %w", sessionID, err))
	}

	var lastVal int
	if scanErr := tx.QueryRowContext(ctx,
		`SELECT last_value FROM daily_counters WHERE counter_type = ? AND date_key = ?;`,
		CounterTypeSession, dateKey,
	).Scan(&lastVal); scanErr != nil && !errors.Is(scanErr, sql.ErrNoRows) {
		return nil, wrapContextErr(fmt.Errorf("failed to query daily session counter for %s: %w", dateKey, scanErr))
	}

	nextNum := lastVal + 1
	now := time.Now().UTC()
	nowUnix := now.Unix()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO daily_counters (counter_type, date_key, last_value, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(counter_type, date_key) DO UPDATE SET
			last_value = excluded.last_value,
			updated_at = excluded.updated_at;`,
		CounterTypeSession, dateKey, nextNum, nowUnix,
	); err != nil {
		return nil, wrapContextErr(fmt.Errorf("failed to increment daily session counter for %s: %w", dateKey, err))
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO session_sequences (session_id, date_key, session_num, created_at)
		VALUES (?, ?, ?, ?);`,
		sessionID, dateKey, nextNum, nowUnix,
	); err != nil {
		return nil, wrapContextErr(fmt.Errorf("failed to insert session sequence %s: %w", sessionID, err))
	}

	if err := tx.Commit(); err != nil {
		return nil, wrapContextErr(err)
	}
	return &SessionSequenceRecord{
		SessionID:  sessionID,
		DateKey:    dateKey,
		SessionNum: nextNum,
		CreatedAt:  now,
	}, nil
}

// GetBallchasingGroup retrieves a persisted Ballchasing group for (sessionID, gameMode).
// Returns ErrGroupNotFound if no group has been created yet.
func (s *SQLiteStore) GetBallchasingGroup(ctx context.Context, sessionID, gameMode string) (*BallchasingGroupRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContextErr(err)
	}
	sessionID = strings.TrimSpace(sessionID)
	mode := domain.NormalizeGameModeCategory(gameMode)
	if sessionID == "" {
		return nil, ErrGroupNotFound
	}

	var (
		rec       BallchasingGroupRecord
		createdAt int64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT session_id, game_mode, date_key, session_num, group_name, ballchasing_group_id, ballchasing_group_url, created_at
		FROM ballchasing_groups
		WHERE session_id = ? AND game_mode = ?;`,
		sessionID, mode,
	).Scan(&rec.SessionID, &rec.GameMode, &rec.DateKey, &rec.SessionNum, &rec.GroupName, &rec.BallchasingGroupID, &rec.BallchasingGroupURL, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrGroupNotFound
	}
	if err != nil {
		return nil, wrapContextErr(fmt.Errorf("failed to get ballchasing group %s/%s: %w", sessionID, mode, err))
	}
	rec.CreatedAt = time.Unix(createdAt, 0).UTC()
	return &rec, nil
}

// SaveBallchasingGroup upserts a persisted Ballchasing group record for (session_id, game_mode).
func (s *SQLiteStore) SaveBallchasingGroup(ctx context.Context, rec *BallchasingGroupRecord) error {
	if err := ctx.Err(); err != nil {
		return wrapContextErr(err)
	}
	if rec == nil || strings.TrimSpace(rec.SessionID) == "" || strings.TrimSpace(rec.BallchasingGroupID) == "" {
		return errors.New("ballchasing group record requires session_id and ballchasing_group_id")
	}
	mode := domain.NormalizeGameModeCategory(rec.GameMode)
	createdAt := rec.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO ballchasing_groups (
			session_id, game_mode, date_key, session_num, group_name,
			ballchasing_group_id, ballchasing_group_url, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id, game_mode) DO UPDATE SET
			date_key = excluded.date_key,
			session_num = excluded.session_num,
			group_name = excluded.group_name,
			ballchasing_group_id = excluded.ballchasing_group_id,
			ballchasing_group_url = excluded.ballchasing_group_url;`,
		strings.TrimSpace(rec.SessionID), mode, strings.TrimSpace(rec.DateKey), rec.SessionNum,
		strings.TrimSpace(rec.GroupName), strings.TrimSpace(rec.BallchasingGroupID),
		strings.TrimSpace(rec.BallchasingGroupURL), createdAt.Unix(),
	)
	if err != nil {
		return wrapContextErr(fmt.Errorf("failed to save ballchasing group %s/%s: %w", rec.SessionID, mode, err))
	}
	return nil
}

// ListBallchasingGroups returns all persisted Ballchasing group mappings ordered by date and session number.
func (s *SQLiteStore) ListBallchasingGroups(ctx context.Context) ([]*BallchasingGroupRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContextErr(err)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT session_id, game_mode, date_key, session_num, group_name, ballchasing_group_id, ballchasing_group_url, created_at
		FROM ballchasing_groups
		ORDER BY date_key ASC, session_num ASC, game_mode ASC;`,
	)
	if err != nil {
		return nil, wrapContextErr(fmt.Errorf("failed to list ballchasing groups: %w", err))
	}
	defer rows.Close()

	out := make([]*BallchasingGroupRecord, 0)
	for rows.Next() {
		var (
			rec       BallchasingGroupRecord
			createdAt int64
		)
		if err := rows.Scan(&rec.SessionID, &rec.GameMode, &rec.DateKey, &rec.SessionNum, &rec.GroupName, &rec.BallchasingGroupID, &rec.BallchasingGroupURL, &createdAt); err != nil {
			return nil, wrapContextErr(fmt.Errorf("failed to scan ballchasing group: %w", err))
		}
		rec.CreatedAt = time.Unix(createdAt, 0).UTC()
		out = append(out, &rec)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapContextErr(err)
	}
	return out, nil
}

// FindSessionForMatch resolves which persisted play session owns matchGUID.
// Priority: (1) exact GUID match inside sessions.matches_json (case/hyphen insensitive),
// (2) session time-window containing matchTime ([started_at - 10m, end_ref + 15m]).
// Returns (nil, nil) if no persisted session matches.
func (s *SQLiteStore) FindSessionForMatch(ctx context.Context, matchGUID string, matchTime time.Time) (*ResolvedMatchSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContextErr(err)
	}
	normTarget := normalizeGUIDKey(matchGUID)

	rows, err := s.db.QueryContext(ctx, `
		SELECT session_id, started_at, ended_at, last_match_at, matches_json
		FROM sessions
		ORDER BY started_at ASC, session_id ASC;`,
	)
	if err != nil {
		return nil, wrapContextErr(fmt.Errorf("failed to query sessions for match %s: %w", matchGUID, err))
	}
	defer rows.Close()

	type sessCandidate struct {
		id        string
		startedAt time.Time
		endRef    time.Time
		matches   string
	}
	var candidates []sessCandidate
	for rows.Next() {
		var (
			sid       string
			started   int64
			ended     sql.NullInt64
			lastMatch sql.NullInt64
			matchesJS string
		)
		if err := rows.Scan(&sid, &started, &ended, &lastMatch, &matchesJS); err != nil {
			return nil, wrapContextErr(fmt.Errorf("failed to scan session candidate: %w", err))
		}
		st := decodeSessionTime(started)
		endRef := st
		if lastMatch.Valid && lastMatch.Int64 != 0 {
			if t := decodeSessionTime(lastMatch.Int64); t.After(endRef) {
				endRef = t
			}
		}
		if ended.Valid && ended.Int64 != 0 {
			if t := decodeSessionTime(ended.Int64); t.After(endRef) {
				endRef = t
			}
		}
		candidates = append(candidates, sessCandidate{
			id:        sid,
			startedAt: st,
			endRef:    endRef,
			matches:   matchesJS,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, wrapContextErr(err)
	}

	if normTarget != "" {
		for _, c := range candidates {
			if strings.TrimSpace(c.matches) == "" || c.matches == "[]" {
				continue
			}
			var entries []sessionMatchLookupEntry
			if json.Unmarshal([]byte(c.matches), &entries) != nil {
				continue
			}
			for _, e := range entries {
				if normalizeGUIDKey(e.MatchGUID) == normTarget {
					return &ResolvedMatchSession{
						SessionID:   c.id,
						StartedAt:   c.startedAt,
						MatchResult: resolveSessionEntryResult(e),
						PlaylistID:  e.PlaylistID,
						ExactMatch:  true,
					}, nil
				}
			}
		}
	}

	if !matchTime.IsZero() {
		const (
			preMargin  = 10 * time.Minute
			postMargin = 15 * time.Minute
		)
		var best *ResolvedMatchSession
		var bestDist time.Duration
		mt := matchTime.UTC()
		for _, c := range candidates {
			if c.startedAt.IsZero() {
				continue
			}
			winStart := c.startedAt.Add(-preMargin)
			winEnd := c.endRef.Add(postMargin)
			if mt.Before(winStart) || mt.After(winEnd) {
				continue
			}
			var dist time.Duration
			if mt.Before(c.startedAt) {
				dist = c.startedAt.Sub(mt)
			} else if mt.After(c.endRef) {
				dist = mt.Sub(c.endRef)
			}
			if best == nil || dist < bestDist {
				bestDist = dist
				best = &ResolvedMatchSession{
					SessionID:  c.id,
					StartedAt:  c.startedAt,
					ExactMatch: false,
				}
			}
		}
		if best != nil {
			return best, nil
		}
	}

	return nil, nil
}

func resolveSessionEntryResult(e sessionMatchLookupEntry) string {
	switch strings.ToLower(strings.TrimSpace(e.Result)) {
	case "victory", "win", "w":
		return domain.ReplayResultWin
	case "defeat", "loss", "l":
		return domain.ReplayResultLoss
	}
	if e.LocalTeam != nil && e.WinnerTeam != nil {
		if *e.LocalTeam == *e.WinnerTeam {
			return domain.ReplayResultWin
		}
		return domain.ReplayResultLoss
	}
	return ""
}
