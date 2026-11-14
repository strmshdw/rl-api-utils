package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func openTestSQLite(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := NewSQLiteStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSQLiteStore_FactoryPragmasAndResilience(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	if _, err := NewSQLiteStore(""); err == nil {
		t.Fatal("expected error for empty dbPath")
	}

	jsonPath := filepath.Join(tmpDir, "state.json")
	js, err := NewStore(jsonPath)
	if err != nil {
		t.Fatalf("NewStore(.json) failed: %v", err)
	}
	if _, ok := js.(*JSONStore); !ok {
		t.Fatalf("NewStore(.json) returned %T, want *JSONStore", js)
	}
	_ = js.Close()

	dbPath := filepath.Join(tmpDir, "sub", "app.db")
	st, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore(.db) failed: %v", err)
	}
	sqlStore, ok := st.(*SQLiteStore)
	if !ok {
		t.Fatalf("NewStore(.db) returned %T, want *SQLiteStore", st)
	}
	defer sqlStore.Close()

	var journalMode string
	if err := sqlStore.db.QueryRowContext(ctx, "PRAGMA journal_mode;").Scan(&journalMode); err != nil || journalMode != "wal" {
		t.Fatalf("journal_mode = %q, err=%v; want wal", journalMode, err)
	}
	var fk int
	if err := sqlStore.db.QueryRowContext(ctx, "PRAGMA foreign_keys;").Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign_keys = %d, err=%v; want 1", fk, err)
	}

	// Verify all 8 tables and 21 indexes exist.
	tables := []string{"matches", "auth_state", "players", "player_matchups", "processed_match_outcomes", "match_players", "mmr_history", "sessions"}
	for _, tbl := range tables {
		var cnt int
		if err := sqlStore.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?;", tbl).Scan(&cnt); err != nil || cnt != 1 {
			t.Fatalf("missing table %s (cnt=%d, err=%v)", tbl, cnt, err)
		}
	}
	var idxCount int
	if err := sqlStore.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name LIKE 'idx_%';").Scan(&idxCount); err != nil || idxCount != 21 {
		t.Fatalf("index count = %d, err=%v; want 21", idxCount, err)
	}

	// 0-byte database file should initialize cleanly.
	zeroPath := filepath.Join(tmpDir, "zero.db")
	if err := os.WriteFile(zeroPath, nil, 0644); err != nil {
		t.Fatal(err)
	}
	zs, err := NewSQLiteStore(zeroPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore on 0-byte file failed: %v", err)
	}
	_ = zs.Close()

	// Corrupt non-SQLite file must fail cleanly.
	corruptPath := filepath.Join(tmpDir, "corrupt.db")
	if err := os.WriteFile(corruptPath, []byte("not-a-sqlite-database"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSQLiteStore(corruptPath); err == nil {
		t.Fatal("expected error opening corrupt database file")
	}
}

func TestSQLiteStore_V1ToV4Migrations(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "legacy_v1.db")

	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	legacyDDL := `
	CREATE TABLE matches (
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
		replay_stats_json TEXT NOT NULL DEFAULT '',
		synced_at INTEGER,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);
	CREATE TABLE players (
		player_id TEXT PRIMARY KEY,
		platform TEXT NOT NULL DEFAULT '',
		player_name TEXT NOT NULL DEFAULT '',
		ranks_json TEXT NOT NULL DEFAULT '{}',
		first_seen_at INTEGER NOT NULL,
		last_seen_at INTEGER NOT NULL
	);
	CREATE TABLE sessions (
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
		local_player_ids TEXT NOT NULL DEFAULT '[]'
	);
	`
	if _, err := rawDB.Exec(legacyDDL); err != nil {
		_ = rawDB.Close()
		t.Fatalf("failed to create v1 schema: %v", err)
	}
	statsJSON, _ := json.Marshal(ReplayMatchStats{
		MatchGUID: "m-v1",
		Playlist:  11,
		Players: []ReplayPlayerStats{
			{Name: "Alpha", Platform: "Steam", OnlineID: "111", Team: 0, Score: 400, Goals: 3},
			{Name: "Beta", Platform: "Epic", OnlineID: "222", Team: 1, Score: 250, Goals: 1},
		},
	})
	now := time.Now().Unix()
	if _, err := rawDB.Exec(`INSERT INTO matches (match_guid, record_start_timestamp, playlist, download_status, upload_status, blue_score, orange_score, replay_stats_json, created_at, updated_at) VALUES ('m-v1', ?, 11, 'DOWNLOADED', 'UPLOADED', 0, 0, ?, ?, ?);`,
		now, string(statsJSON), now, now); err != nil {
		_ = rawDB.Close()
		t.Fatal(err)
	}
	if _, err := rawDB.Exec(`INSERT INTO players (player_id, platform, player_name, ranks_json, first_seen_at, last_seen_at) VALUES ('Steam|111|0', 'Steam', 'AlphaCase', '{}', ?, ?);`, now, now); err != nil {
		_ = rawDB.Close()
		t.Fatal(err)
	}
	_ = rawDB.Close()

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore migration failed: %v", err)
	}
	defer store.Close()

	// Verify backfilled columns + player_name_lower + zero-score repair + match_players backfill.
	m, err := store.GetMatch(ctx, "m-v1")
	if err != nil {
		t.Fatalf("GetMatch after migration failed: %v", err)
	}
	if m.BlueScore != 3 || m.OrangeScore != 1 || m.WinnerTeam == nil || *m.WinnerTeam != 0 {
		t.Fatalf("zero-score repair failed: got %d-%d winner=%v, want 3-1 winner=0", m.BlueScore, m.OrangeScore, m.WinnerTeam)
	}

	var nameLower string
	if err := store.db.QueryRowContext(ctx, "SELECT player_name_lower FROM players WHERE player_id='Steam|111|0';").Scan(&nameLower); err != nil || nameLower != "alphacase" {
		t.Fatalf("player_name_lower = %q, err=%v; want alphacase", nameLower, err)
	}

	mps, err := store.GetMatchPlayers(ctx, "m-v1")
	if err != nil || len(mps) != 2 {
		t.Fatalf("match_players backfill got len=%d, err=%v; want 2", len(mps), err)
	}
}

func TestSQLiteStore_017db99ZeroPlayerAndRepairPreservation(t *testing.T) {
	ctx := context.Background()
	store := openTestSQLite(t)

	// 1. Seed a SYNCED match whose replay had 0 parsed players (valid Ballchasing replay, 017db99).
	// 2. Seed a DOWNLOADING, UPLOADING, and SYNCING match (should reset to PENDING).
	// 3. Seed a SYNCED match with players missing "boost": telemetry (should reset to PENDING in RepairHistoricalData).
	now := time.Now().UTC()
	matches := []*MatchRecord{
		{
			MatchGUID:            "synced-empty-players",
			RecordStartTimestamp: now.Unix(),
			DownloadStatus:       DownloadDownloaded,
			UploadStatus:         UploadUploaded,
			BallchasingID:        "bc-empty",
			StatsSyncStatus:      StatsSyncSynced,
			ReplayStatsJSON:      `{"match_guid":"synced-empty-players","players":[]}`,
		},
		{
			MatchGUID:            "synced-legacy-no-boost",
			RecordStartTimestamp: now.Add(-time.Minute).Unix(),
			DownloadStatus:       DownloadDownloaded,
			UploadStatus:         UploadUploaded,
			BallchasingID:        "bc-legacy",
			StatsSyncStatus:      StatsSyncSynced,
			ReplayStatsJSON:      `{"match_guid":"synced-legacy-no-boost","players":[{"name":"P1","score":300}]}`,
		},
		{
			MatchGUID:            "inflight-dl",
			RecordStartTimestamp: now.Add(-2 * time.Minute).Unix(),
			DownloadStatus:       DownloadDownloading,
			UploadStatus:         UploadPending,
			StatsSyncStatus:      StatsSyncPending,
		},
		{
			MatchGUID:            "inflight-ul",
			RecordStartTimestamp: now.Add(-3 * time.Minute).Unix(),
			DownloadStatus:       DownloadDownloaded,
			UploadStatus:         UploadUploading,
			StatsSyncStatus:      StatsSyncPending,
		},
		{
			MatchGUID:            "inflight-sync",
			RecordStartTimestamp: now.Add(-4 * time.Minute).Unix(),
			DownloadStatus:       DownloadDownloaded,
			UploadStatus:         UploadUploaded,
			BallchasingID:        "bc-syncing",
			StatsSyncStatus:      StatsSyncSyncing,
		},
	}
	if err := store.UpsertDiscoveredMatches(ctx, matches); err != nil {
		t.Fatal(err)
	}
	// Manually force inflight/synced states into DB rows cleanly and mark DB as pre-v4 for repair verification.
	_, _ = store.db.ExecContext(ctx, `UPDATE matches SET ballchasing_id='bc-empty', stats_sync_status='SYNCED', replay_stats_json=? WHERE match_guid='synced-empty-players';`, matches[0].ReplayStatsJSON)
	_, _ = store.db.ExecContext(ctx, `UPDATE matches SET ballchasing_id='bc-legacy', stats_sync_status='SYNCED', replay_stats_json=? WHERE match_guid='synced-legacy-no-boost';`, matches[1].ReplayStatsJSON)
	_, _ = store.db.ExecContext(ctx, `UPDATE matches SET download_status='DOWNLOADING' WHERE match_guid='inflight-dl';`)
	_, _ = store.db.ExecContext(ctx, `UPDATE matches SET upload_status='UPLOADING' WHERE match_guid='inflight-ul';`)
	_, _ = store.db.ExecContext(ctx, `UPDATE matches SET stats_sync_status='SYNCING' WHERE match_guid='inflight-sync';`)
	_, _ = store.db.ExecContext(ctx, `PRAGMA user_version = 3;`)

	if err := store.RecoverInFlight(ctx); err != nil {
		t.Fatalf("RecoverInFlight failed: %v", err)
	}

	checkStatus := func(guid string, wantDL DownloadStatus, wantUL UploadStatus, wantSync StatsSyncStatus) {
		t.Helper()
		got, err := store.GetMatch(ctx, guid)
		if err != nil {
			t.Fatalf("GetMatch(%s) failed: %v", guid, err)
		}
		if got.DownloadStatus != wantDL || got.UploadStatus != wantUL || got.StatsSyncStatus != wantSync {
			t.Fatalf("%s statuses = (%s, %s, %s), want (%s, %s, %s)", guid, got.DownloadStatus, got.UploadStatus, got.StatsSyncStatus, wantDL, wantUL, wantSync)
		}
	}

	checkStatus("inflight-dl", DownloadPending, UploadPending, StatsSyncPending)
	checkStatus("inflight-ul", DownloadDownloaded, UploadPending, StatsSyncPending)
	checkStatus("inflight-sync", DownloadDownloaded, UploadUploaded, StatsSyncPending)
	// CRITICAL 017db99: zero-player SYNCED match must NEVER revert to PENDING!
	checkStatus("synced-empty-players", DownloadDownloaded, UploadUploaded, StatsSyncSynced)

	if err := store.RepairHistoricalData(ctx); err != nil {
		t.Fatalf("RepairHistoricalData failed: %v", err)
	}
	// After RepairHistoricalData:
	// - synced-empty-players MUST remain SYNCED!
	// - synced-legacy-no-boost MUST be queued back to PENDING for re-sync!
	checkStatus("synced-empty-players", DownloadDownloaded, UploadUploaded, StatsSyncSynced)
	checkStatus("synced-legacy-no-boost", DownloadDownloaded, UploadUploaded, StatsSyncPending)
}

func TestSQLiteStore_CanonicalRanksAuthAndClose(t *testing.T) {
	ctx := context.Background()
	store := openTestSQLite(t)

	if got := FormatRankName(22, 1); got != "Supersonic Legend" {
		t.Fatalf("tier 22 = %q, want Supersonic Legend", got)
	}
	if got := FormatRankName(19, 3); got != "Grand Champion I Div III" {
		t.Fatalf("tier 19 div 3 = %q", got)
	}
	if got := FormatShortRankName(19, 3); got != "GC I · DIV III" {
		t.Fatalf("short GC I div 3 = %q", got)
	}
	if got := FormatShortRankName(22, 0); got != "SSL" {
		t.Fatalf("short SSL = %q", got)
	}
	if got := FormatPlaylistName(11); got != "Ranked Doubles 2v2" {
		t.Fatalf("playlist 11 = %q", got)
	}
	if got := Resolve1BasedDivision(0, "Champion I Division IV"); got != 4 {
		t.Fatalf("Resolve1BasedDivision(Div IV) = %d, want 4", got)
	}

	// Test rank JSON repair when tier=22 was previously formatted with bogus label.
	badRanks := `{"11":{"playlist_id":11,"tier":22,"division":0,"rank_name":"Bronze I","short_rank":"B I","mmr":1905}}`
	if err := store.UpsertPlayer(ctx, &PlayerRecord{PlayerID: "Steam|r1|0", Platform: "Steam", PlayerName: "Legend", RanksJSON: badRanks}); err != nil {
		t.Fatal(err)
	}
	store.repairCorruptedRanksJSON(ctx, false)
	pl, err := store.GetPlayer(ctx, "Steam|r1|0")
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]map[string]interface{}
	if err := json.Unmarshal([]byte(pl.RanksJSON), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["11"]["rank_name"] != "Supersonic Legend" || parsed["11"]["short_rank"] != "SSL" {
		t.Fatalf("repaired ranks_json = %v, want Supersonic Legend / SSL", parsed["11"])
	}

	// Auth round-trip + update.
	if _, _, _, err := store.GetAuthState(ctx, "epic"); !errors.Is(err, ErrAuthStateNotFound) {
		t.Fatalf("expected ErrAuthStateNotFound, got %v", err)
	}
	if err := store.SaveAuthState(ctx, "epic", "tok-1", "acc-1", "DriverOne"); err != nil {
		t.Fatal(err)
	}
	tok, acc, name, err := store.GetAuthState(ctx, "epic")
	if err != nil || tok != "tok-1" || acc != "acc-1" || name != "DriverOne" {
		t.Fatalf("GetAuthState = (%q, %q, %q, %v)", tok, acc, name, err)
	}

	// Closing store and checking closed state.
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("second Close() failed: %v", err)
	}
	if _, err := store.GetMatch(ctx, "any"); err == nil {
		t.Fatal("expected error querying closed SQLiteStore")
	}
}
