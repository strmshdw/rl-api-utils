package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// UpsertPlayer creates or updates a player's profile metadata.
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
		strings.ToLower(player.PlayerName),
		ranksJSON,
		player.Notes,
		serializeTags(player.Tags),
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
	playerID = s.resolveCanonicalPlayerID(ctx, playerID)

	const query = `
	SELECT player_id, platform, player_name, ranks_json, notes, tags, first_seen_at, last_seen_at
	FROM players
	WHERE player_id = ?;
	`
	p, err := scanPlayerRecordRow(s.db.QueryRowContext(ctx, query, playerID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPlayerNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get player %s: %w", playerID, err)
	}
	return p, nil
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
		p, scanErr := scanPlayerRecordRow(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("failed to scan player row: %w", scanErr)
		}
		records = append(records, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error in list players: %w", err)
	}
	return records, nil
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

	const query = `
	UPDATE players
	SET ranks_json = ?, last_seen_at = MAX(last_seen_at, ?)
	WHERE player_id = ?;
	`
	res, err := s.db.ExecContext(ctx, query, ranksJSON, time.Now().UTC().Unix(), playerID)
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

// UpdatePlayerNotesAndTags updates notes and custom tags for a player profile.
func (s *SQLiteStore) UpdatePlayerNotesAndTags(ctx context.Context, playerID string, notes string, tags []string) error {
	if strings.TrimSpace(playerID) == "" {
		return ErrPlayerNotFound
	}
	playerID = s.resolveCanonicalPlayerID(ctx, playerID)

	const query = `
	UPDATE players
	SET notes = ?, tags = ?
	WHERE player_id = ?;
	`
	res, err := s.db.ExecContext(ctx, query, notes, serializeTags(tags), playerID)
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

// RecordMatchResults atomically records all player outcomes for a concluded match.
// If matchGUID was already processed, rolls back and returns ErrMatchAlreadyProcessed.
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

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapContextErr(fmt.Errorf("failed to begin match results tx: %w", err))
	}
	defer tx.Rollback()

	const insertLedgerSQL = `INSERT INTO processed_match_outcomes (match_guid, playlist_id, processed_at) VALUES (?, ?, ?);`
	if _, err = tx.ExecContext(ctx, insertLedgerSQL, matchGUID, playlistID, matchTS); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return wrapContextErr(ctxErr)
		}
		if strings.Contains(err.Error(), "constraint failed") {
			return ErrMatchAlreadyProcessed
		}
		return fmt.Errorf("failed to record processed match outcome: %w", err)
	}

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
		return wrapContextErr(fmt.Errorf("failed to prepare upsert player stmt: %w", err))
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
		return wrapContextErr(fmt.Errorf("failed to prepare upsert matchup stmt: %w", err))
	}
	defer stmtMatchup.Close()

	for _, outcome := range resolvedOutcomes {
		if strings.TrimSpace(outcome.PlayerID) == "" {
			continue
		}
		if _, err := stmtPlayer.ExecContext(ctx,
			outcome.PlayerID, outcome.Platform, outcome.PlayerName, strings.ToLower(outcome.PlayerName),
			matchTS, matchTS, hasExplicitTime,
		); err != nil {
			return wrapContextErr(fmt.Errorf("failed to ensure player %s exists: %w", outcome.PlayerID, err))
		}

		var winTM, lossTM, winOpp, lossOpp int
		switch {
		case outcome.IsTeammate && outcome.Won:
			winTM = 1
		case outcome.IsTeammate:
			lossTM = 1
		case outcome.Won:
			winOpp = 1
		default:
			lossOpp = 1
		}
		if _, err := stmtMatchup.ExecContext(ctx, outcome.PlayerID, playlistID, winTM, lossTM, winOpp, lossOpp, matchTS); err != nil {
			return wrapContextErr(fmt.Errorf("failed to upsert matchup for player %s in playlist %d: %w", outcome.PlayerID, playlistID, err))
		}
	}

	if err := tx.Commit(); err != nil {
		return wrapContextErr(err)
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
	m, err := scanPlayerMatchupRow(s.db.QueryRowContext(ctx, query, resolvedID, playlistID))
	if err == nil {
		return m, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed to get player matchup for %s (playlist %d): %w", playerID, playlistID, err)
	}

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
	  AND m.playlist = ?
	  AND m.winner_team >= 0
	  AND mp.is_local = 0
	GROUP BY mp.player_id, m.playlist;
	`
	if fb, fbErr := scanPlayerMatchupRow(s.db.QueryRowContext(ctx, fallbackQuery, resolvedID, playlistID)); fbErr == nil && fb.TotalMatches > 0 {
		fb.PlayerID = resolvedID
		fb.PlaylistID = playlistID
		return fb, nil
	}
	return &PlayerMatchup{PlayerID: playerID, PlaylistID: playlistID}, nil
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
		m, scanErr := scanPlayerMatchupRow(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("failed to scan player matchup row: %w", scanErr)
		}
		matchups = append(matchups, m)
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
		if fbRows, fbErr := s.db.QueryContext(ctx, fallbackQuery, playerID); fbErr == nil {
			defer fbRows.Close()
			for fbRows.Next() {
				if m, scanErr := scanPlayerMatchupRow(fbRows); scanErr == nil {
					matchups = append(matchups, m)
				}
			}
		}
	}
	return matchups, nil
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
		recTime.Unix(), record.MatchGUID, record.PlaylistID, record.MMR, record.MMRChange, record.Tier, record.Division,
	)
	if err != nil {
		return fmt.Errorf("failed to record mmr: %w", err)
	}
	if id, err := res.LastInsertId(); err == nil {
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
		rows, err = s.db.QueryContext(ctx, `
		SELECT id, recorded_at, match_guid, playlist_id, mmr, mmr_change, tier, division
		FROM mmr_history WHERE playlist_id = ? ORDER BY recorded_at DESC, id DESC LIMIT ?;`, playlistID, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
		SELECT id, recorded_at, match_guid, playlist_id, mmr, mmr_change, tier, division
		FROM mmr_history ORDER BY recorded_at DESC, id DESC LIMIT ?;`, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query mmr history: %w", err)
	}
	defer rows.Close()

	records := make([]*MMRHistoryRecord, 0)
	for rows.Next() {
		var r MMRHistoryRecord
		var recUnix int64
		if err := rows.Scan(&r.ID, &recUnix, &r.MatchGUID, &r.PlaylistID, &r.MMR, &r.MMRChange, &r.Tier, &r.Division); err != nil {
			return nil, fmt.Errorf("failed to scan mmr history row: %w", err)
		}
		r.RecordedAt = time.Unix(recUnix, 0).UTC()
		records = append(records, &r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error in list mmr history: %w", err)
	}
	return records, nil
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanPlayerRecordRow(scanner rowScanner) (*PlayerRecord, error) {
	var (
		p         PlayerRecord
		notes     sql.NullString
		tagsStr   sql.NullString
		firstSeen int64
		lastSeen  int64
	)
	if err := scanner.Scan(&p.PlayerID, &p.Platform, &p.PlayerName, &p.RanksJSON, &notes, &tagsStr, &firstSeen, &lastSeen); err != nil {
		return nil, err
	}
	p.Notes = notes.String
	p.Tags = parseTags(tagsStr.String)
	p.FirstSeenAt = time.Unix(firstSeen, 0).UTC()
	p.LastSeenAt = time.Unix(lastSeen, 0).UTC()
	return &p, nil
}

func scanPlayerMatchupRow(scanner rowScanner) (*PlayerMatchup, error) {
	var m PlayerMatchup
	var lastPlayed int64
	if err := scanner.Scan(
		&m.PlayerID, &m.PlaylistID,
		&m.WinsAsTeammate, &m.LossesAsTeammate,
		&m.WinsAsOpponent, &m.LossesAsOpponent,
		&m.TotalMatches, &lastPlayed,
	); err != nil {
		return nil, err
	}
	if lastPlayed > 0 {
		m.LastPlayedAt = time.Unix(lastPlayed, 0).UTC()
	}
	return &m, nil
}
