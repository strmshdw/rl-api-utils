package invariants

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/psynet"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/syncer"
	_ "modernc.org/sqlite"
)

func TestInvariant_SQLiteV1ToV4Migration(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "legacy_v1.db")

	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open raw sqlite: %v", err)
	}

	v1DDL := `
	PRAGMA user_version = 1;
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
	CREATE TABLE auth_state (
		provider TEXT PRIMARY KEY,
		refresh_token TEXT NOT NULL,
		account_id TEXT NOT NULL DEFAULT '',
		display_name TEXT NOT NULL DEFAULT '',
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
	);`
	if _, err := rawDB.Exec(v1DDL); err != nil {
		_ = rawDB.Close()
		t.Fatalf("create v1 schema: %v", err)
	}

	now := time.Now().UTC().Unix()
	statsJSON, _ := json.Marshal(storage.ReplayMatchStats{
		MatchGUID:     "m-v1-preserved",
		BallchasingID: "bc-v1",
		Playlist:      11,
		Players: []storage.ReplayPlayerStats{
			{Name: "DriverA", Platform: "Steam", OnlineID: "76561198000000111", Team: 0, Score: 500, Goals: 3},
			{Name: "DriverB", Platform: "Epic", OnlineID: "epic_opp_222", Team: 1, Score: 320, Goals: 1},
		},
	})
	if _, err := rawDB.Exec(
		`INSERT INTO matches (match_guid, record_start_timestamp, map_name, playlist, download_status, upload_status, ballchasing_id, blue_score, orange_score, replay_stats_json, synced_at, created_at, updated_at)
		 VALUES ('m-v1-preserved', ?, 'Mannfield', 11, 'DOWNLOADED', 'UPLOADED', 'bc-v1', 0, 0, ?, ?, ?, ?);`,
		now, string(statsJSON), now, now, now,
	); err != nil {
		_ = rawDB.Close()
		t.Fatalf("seed v1 match: %v", err)
	}
	if _, err := rawDB.Exec(
		`INSERT INTO auth_state (provider, refresh_token, account_id, display_name, updated_at)
		 VALUES ('epic', 'rt-v1-keep', 'epic-v1-local', 'V1Driver', ?);`,
		now,
	); err != nil {
		_ = rawDB.Close()
		t.Fatalf("seed v1 auth: %v", err)
	}
	if _, err := rawDB.Exec(
		`INSERT INTO players (player_id, platform, player_name, ranks_json, first_seen_at, last_seen_at)
		 VALUES ('Steam|76561198000000111|0', 'Steam', 'DriverA', '{}', ?, ?);`,
		now, now,
	); err != nil {
		_ = rawDB.Close()
		t.Fatalf("seed v1 player: %v", err)
	}
	_ = rawDB.Close()

	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore migration failed: %v", err)
	}
	defer store.Close()

	inspectDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("re-open sqlite for schema check: %v", err)
	}
	defer inspectDB.Close()

	var userVersion int
	if err := inspectDB.QueryRowContext(ctx, "PRAGMA user_version;").Scan(&userVersion); err != nil {
		t.Fatalf("read PRAGMA user_version: %v", err)
	}
	if userVersion != 4 {
		t.Fatalf("PRAGMA user_version = %d, want 4", userVersion)
	}

	expectedTables := []string{
		"matches",
		"auth_state",
		"players",
		"player_matchups",
		"processed_match_outcomes",
		"match_players",
		"mmr_history",
		"sessions",
	}
	for _, tbl := range expectedTables {
		var count int
		if err := inspectDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?;", tbl).Scan(&count); err != nil || count != 1 {
			t.Fatalf("expected migrated table %q to exist (count=%d, err=%v)", tbl, count, err)
		}
	}

	expectedIndexes := []string{
		"idx_matches_download_status",
		"idx_matches_upload_status",
		"idx_matches_record_ts",
		"idx_matches_playlist",
		"idx_matches_stats_sync",
		"idx_players_last_seen",
		"idx_players_last_seen_id",
		"idx_players_name",
		"idx_players_name_lower",
		"idx_players_platform",
		"idx_player_matchups_player",
		"idx_processed_matches_ts",
		"idx_match_players_player",
		"idx_match_players_player_won",
		"idx_match_players_local_team",
		"idx_match_players_local_player",
		"idx_mmr_history_playlist",
		"idx_mmr_history_recorded",
		"idx_mmr_history_match",
		"idx_sessions_started_at",
		"idx_sessions_last_match_at",
	}
	for _, idx := range expectedIndexes {
		var count int
		if err := inspectDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?;", idx).Scan(&count); err != nil || count != 1 {
			t.Fatalf("expected migrated index %q to exist (count=%d, err=%v)", idx, count, err)
		}
	}

	m, err := store.GetMatch(ctx, "m-v1-preserved")
	if err != nil {
		t.Fatalf("GetMatch after migration: %v", err)
	}
	if m.BallchasingID != "bc-v1" || m.BlueScore != 3 || m.OrangeScore != 1 || m.WinnerTeam == nil || *m.WinnerTeam != 0 {
		t.Fatalf("migrated match state unexpected: %+v", m)
	}

	rt, acc, disp, err := store.GetAuthState(ctx, "epic")
	if err != nil {
		t.Fatalf("GetAuthState after migration: %v", err)
	}
	if rt != "rt-v1-keep" || acc != "epic-v1-local" || disp != "V1Driver" {
		t.Fatalf("auth_state altered during migration: (%q, %q, %q)", rt, acc, disp)
	}

	mps, err := store.GetMatchPlayers(ctx, "m-v1-preserved")
	if err != nil || len(mps) != 2 {
		t.Fatalf("expected 2 backfilled match_players after v1->v4 migration, got len=%d err=%v", len(mps), err)
	}
}

type noopHistoryProvider struct{}

func (p *noopHistoryProvider) GetRecentMatches(context.Context) ([]psynet.DiscoveredMatch, error) {
	return nil, nil
}
func (p *noopHistoryProvider) Close() error { return nil }

type noopReplayDownloader struct{}

func (d *noopReplayDownloader) DownloadReplay(context.Context, string, string, string) (string, error) {
	return "", nil
}

type noopReplayUploader struct{}

func (u *noopReplayUploader) UploadReplay(context.Context, string, string) (*ballchasing.UploadResult, error) {
	return &ballchasing.UploadResult{}, nil
}

type stubStatsFetcher struct {
	item *ballchasing.ReplayItem
}

func (f *stubStatsFetcher) GetReplay(context.Context, string) (*ballchasing.ReplayItem, error) {
	return f.item, nil
}

func TestInvariant_017db99_ZeroPlayerReplaySyncAndRecoverInFlight(t *testing.T) {
	ctx := context.Background()

	t.Run("Store_SyncReplayStats_ZeroPlayers_NotResetByRecoverInFlight", func(t *testing.T) {
		store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "inv_017db99_store.db"))
		if err != nil {
			t.Fatalf("NewSQLiteStore: %v", err)
		}
		defer store.Close()

		now := time.Now().UTC().Unix()
		if err := store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{
			{
				MatchGUID:            "m-zero-p",
				RecordStartTimestamp: now,
				DownloadStatus:       storage.DownloadDownloaded,
				UploadStatus:         storage.UploadUploaded,
				BallchasingID:        "bc-zero-p",
				StatsSyncStatus:      storage.StatsSyncPending,
			},
			{
				MatchGUID:            "m-syncing-inflight",
				RecordStartTimestamp: now - 10,
				DownloadStatus:       storage.DownloadDownloaded,
				UploadStatus:         storage.UploadUploaded,
				BallchasingID:        "bc-syncing",
				StatsSyncStatus:      storage.StatsSyncSyncing,
			},
		}); err != nil {
			t.Fatalf("seed matches: %v", err)
		}
		if err := store.SaveMatchPlayers(ctx, "m-zero-p", []*storage.MatchPlayerRecord{
			{MatchGUID: "m-zero-p", PlayerID: "Steam|kept1|0", TeamNum: 0, Score: 300},
		}); err != nil {
			t.Fatalf("seed match_players: %v", err)
		}

		if err := store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
			MatchGUID:     "m-zero-p",
			BallchasingID: "bc-zero-p",
			Playlist:      11,
			Players:       nil,
		}); err != nil {
			t.Fatalf("SyncReplayStats with nil players: %v", err)
		}

		rec, err := store.GetMatch(ctx, "m-zero-p")
		if err != nil {
			t.Fatalf("GetMatch: %v", err)
		}
		if rec.StatsSyncStatus != storage.StatsSyncSynced {
			t.Fatalf("StatsSyncStatus = %q, want %q", rec.StatsSyncStatus, storage.StatsSyncSynced)
		}

		mps, err := store.GetMatchPlayers(ctx, "m-zero-p")
		if err != nil || len(mps) != 1 {
			t.Fatalf("expected existing match_players to be preserved on 0-player summary, got len=%d err=%v", len(mps), err)
		}

		pending, err := store.ListPendingStatsSync(ctx, 5)
		if err != nil {
			t.Fatalf("ListPendingStatsSync: %v", err)
		}
		for _, p := range pending {
			if p.MatchGUID == "m-zero-p" {
				t.Fatalf("0-player SYNCED match returned in ListPendingStatsSync")
			}
		}

		if err := store.RecoverInFlight(ctx); err != nil {
			t.Fatalf("RecoverInFlight: %v", err)
		}

		afterRec, err := store.GetMatch(ctx, "m-zero-p")
		if err != nil {
			t.Fatalf("GetMatch after RecoverInFlight: %v", err)
		}
		if afterRec.StatsSyncStatus != storage.StatsSyncSynced {
			t.Fatalf("017db99 regression: RecoverInFlight reset 0-player match to %q (want %q)", afterRec.StatsSyncStatus, storage.StatsSyncSynced)
		}

		syncingRec, err := store.GetMatch(ctx, "m-syncing-inflight")
		if err != nil || syncingRec.StatsSyncStatus != storage.StatsSyncPending {
			t.Fatalf("expected SYNCING match to reset to PENDING, got %q (err=%v)", syncingRec.StatsSyncStatus, err)
		}
	})

	t.Run("Syncer_PollAndSyncPendingStats_ZeroPlayers", func(t *testing.T) {
		store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "inv_017db99_syncer.db"))
		if err != nil {
			t.Fatalf("NewSQLiteStore: %v", err)
		}
		defer store.Close()

		now := time.Now().UTC().Unix()
		if err := store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{
			{
				MatchGUID:            "m-syncer-zero",
				RecordStartTimestamp: now,
				DownloadStatus:       storage.DownloadDownloaded,
				UploadStatus:         storage.UploadUploaded,
				BallchasingID:        "bc-syncer-zero",
				BallchasingURL:       "https://ballchasing.com/replay/bc-syncer-zero",
				StatsSyncStatus:      storage.StatsSyncPending,
			},
		}); err != nil {
			t.Fatalf("seed match: %v", err)
		}

		fetcher := &stubStatsFetcher{
			item: &ballchasing.ReplayItem{
				ID:     "bc-syncer-zero",
				Link:   "https://ballchasing.com/replay/bc-syncer-zero",
				Status: "ok",
				Blue:   ballchasing.ReplayTeam{Color: "blue", Players: nil},
				Orange: ballchasing.ReplayTeam{Color: "orange", Players: []ballchasing.ReplayPlayer{}},
			},
		}
		s, err := syncer.New(
			store,
			&noopHistoryProvider{},
			&noopReplayDownloader{},
			&noopReplayUploader{},
			syncer.WithStatsFetcher(fetcher),
			syncer.WithRateInterval(0),
		)
		if err != nil {
			t.Fatalf("syncer.New: %v", err)
		}

		if err := s.PollAndSyncPendingStats(ctx); err != nil {
			t.Fatalf("PollAndSyncPendingStats: %v", err)
		}

		rec, err := store.GetMatch(ctx, "m-syncer-zero")
		if err != nil || rec.StatsSyncStatus != storage.StatsSyncSynced {
			t.Fatalf("after 0-player Ballchasing poll: StatsSyncStatus=%q err=%v, want SYNCED", rec.StatsSyncStatus, err)
		}
		pending, err := store.ListPendingStatsSync(ctx, 5)
		if err != nil || len(pending) != 0 {
			t.Fatalf("expected 0 pending stats sync after 0-player sync, got %d (err=%v)", len(pending), err)
		}
		if err := store.RecoverInFlight(ctx); err != nil {
			t.Fatalf("RecoverInFlight: %v", err)
		}
		after, err := store.GetMatch(ctx, "m-syncer-zero")
		if err != nil || after.StatsSyncStatus != storage.StatsSyncSynced {
			t.Fatalf("RecoverInFlight mutated 0-player SYNCED status to %q", after.StatsSyncStatus)
		}
	})
}
