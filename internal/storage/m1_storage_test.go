package storage_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/storage"
)

func getM1TestStores(t *testing.T) []searchTestStoreEntry {
	t.Helper()
	return []searchTestStoreEntry{
		{
			name: "SQLiteStore",
			factory: func(t *testing.T) storage.StateStore {
				dbPath := filepath.Join(t.TempDir(), "m1_test.db")
				s, err := storage.NewSQLiteStore(dbPath)
				if err != nil {
					t.Fatalf("failed to create sqlite store: %v", err)
				}
				t.Cleanup(func() { _ = s.Close() })
				return s
			},
		},
		{
			name: "JSONStore",
			factory: func(t *testing.T) storage.StateStore {
				jsonPath := filepath.Join(t.TempDir(), "m1_test.json")
				s, err := storage.NewJSONStore(jsonPath)
				if err != nil {
					t.Fatalf("failed to create json store: %v", err)
				}
				t.Cleanup(func() { _ = s.Close() })
				return s
			},
		},
	}
}

// TestM1_SchemaMigrationsAndSafePRAGMAs tests safe column additions and idempotency on SQLite.
func TestM1_SchemaMigrationsAndSafePRAGMAs(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "migration_legacy.db")

	// 1. Create a bare legacy schema database without M1 columns or tables
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open raw sqlite db: %v", err)
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
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);
	CREATE TABLE players (
		player_id TEXT PRIMARY KEY,
		platform TEXT NOT NULL DEFAULT '',
		player_name TEXT NOT NULL DEFAULT '',
		player_name_lower TEXT NOT NULL DEFAULT '',
		ranks_json TEXT NOT NULL DEFAULT '{}',
		first_seen_at INTEGER NOT NULL,
		last_seen_at INTEGER NOT NULL
	);
	`
	if _, err := rawDB.ExecContext(ctx, legacyDDL); err != nil {
		t.Fatalf("failed to create legacy schema: %v", err)
	}
	_ = rawDB.Close()

	// 2. Open via NewSQLiteStore, which must perform safe PRAGMA-based migrations
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to open store with legacy schema: %v", err)
	}

	// Verify columns were added by inserting a player with notes/tags and match with stats sync status
	player := &storage.PlayerRecord{
		PlayerID:    "Epic|legacy_test_01|0",
		Platform:    "Epic",
		PlayerName:  "LegacyPlayer",
		Notes:       "Special smurf note",
		Tags:        []string{"smurf", "pro"},
		FirstSeenAt: time.Now().UTC(),
		LastSeenAt:  time.Now().UTC(),
	}
	if err := store.UpsertPlayer(ctx, player); err != nil {
		t.Fatalf("UpsertPlayer failed on migrated schema: %v", err)
	}

	p, err := store.GetPlayer(ctx, "Epic|legacy_test_01|0")
	if err != nil {
		t.Fatalf("GetPlayer failed: %v", err)
	}
	if p.Notes != "Special smurf note" || len(p.Tags) != 2 || p.Tags[0] != "smurf" || p.Tags[1] != "pro" {
		t.Errorf("notes/tags mismatch: got notes=%q, tags=%v", p.Notes, p.Tags)
	}

	// 3. Re-open multiple times to ensure idempotency
	_ = store.Close()
	for i := 0; i < 3; i++ {
		reopened, err := storage.NewSQLiteStore(dbPath)
		if err != nil {
			t.Fatalf("reopening store iteration %d failed: %v", i, err)
		}
		_ = reopened.Close()
	}
}

// TestM1_StartupBackfillMigration tests startup backfilling of match_players from existing matches.
func TestM1_StartupBackfillMigration(t *testing.T) {
	ctx := context.Background()

	winner := 0
	stats := &storage.ReplayMatchStats{
		MatchGUID:   "backfill-match-1",
		MapName:     "DFH_Stadium",
		Playlist:    11,
		BlueScore:   4,
		OrangeScore: 2,
		WinnerTeam:  &winner,
		Duration:    300,
		Date:        "2026-09-15T12:00:00Z",
		Players: []storage.ReplayPlayerStats{
			{
				Name:     "PlayerA",
				Platform: "Epic",
				OnlineID: "backfill_p1",
				Team:     0,
				Score:    650,
				Goals:    3,
				Assists:  1,
				Saves:    2,
				Shots:    5,
				Demos:    1,
			},
			{
				Name:     "PlayerB",
				Platform: "Steam",
				OnlineID: "76561198000000999",
				Team:     1,
				Score:    400,
				Goals:    1,
				Assists:  0,
				Saves:    3,
				Shots:    2,
				Demos:    0,
			},
		},
		BallchasingID:  "bc-backfill-1",
		BallchasingURL: "https://ballchasing.com/replay/bc-backfill-1",
	}

	for _, entry := range getM1TestStores(t) {
		t.Run(entry.name, func(t *testing.T) {
			store := entry.factory(t)

			// Sync replay stats (which populates match and match_players)
			if err := store.SyncReplayStats(ctx, stats); err != nil {
				t.Fatalf("SyncReplayStats failed: %v", err)
			}

			// Query match players
			players, err := store.GetMatchPlayers(ctx, "backfill-match-1")
			if err != nil {
				t.Fatalf("GetMatchPlayers failed: %v", err)
			}
			if len(players) != 2 {
				t.Fatalf("expected 2 match players, got %d", len(players))
			}

			// PlayerA (team 0, score 650) should be first
			pA := players[0]
			if pA.PlayerID != "Epic|backfill_p1|0" || pA.Score != 650 || pA.Goals != 3 {
				t.Errorf("unexpected player A record: %+v", pA)
			}
			if pA.Won == nil || !*pA.Won {
				t.Errorf("expected player A won = true, got %v", pA.Won)
			}

			// PlayerB (team 1, score 400) should be second
			pB := players[1]
			if pB.PlayerID != "Steam|76561198000000999|0" || pB.Score != 400 {
				t.Errorf("unexpected player B record: %+v", pB)
			}
			if pB.Won == nil || *pB.Won {
				t.Errorf("expected player B won = false, got %v", pB.Won)
			}
		})
	}
}

// TestM1_MatchPlayersCRUDAndRoleCalculations tests match player persistence, role assignment, and results.
func TestM1_MatchPlayersCRUDAndRoleCalculations(t *testing.T) {
	ctx := context.Background()

	for _, entry := range getM1TestStores(t) {
		t.Run(entry.name, func(t *testing.T) {
			store := entry.factory(t)

			// 1. Create a match record
			m := &storage.MatchRecord{
				MatchGUID:            "guid-m1-test",
				RecordStartTimestamp: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC).Unix(),
				MapName:              "Mannfield",
				Playlist:             11,
				DownloadStatus:       storage.DownloadDownloaded,
				UploadStatus:         storage.UploadUploaded,
				BallchasingID:        "bc-test-1",
				BlueScore:            3,
				OrangeScore:          1,
				WinnerTeam:           func() *int { w := 0; return &w }(),
				CreatedAt:            time.Now().UTC(),
				UpdatedAt:            time.Now().UTC(),
			}
			if err := store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{m}); err != nil {
				t.Fatalf("UpsertDiscoveredMatches failed: %v", err)
			}

			// 2. Save match players with 1 local player on team 0, 1 teammate on team 0, and 2 opponents on team 1
			wonTrue := true
			wonFalse := false
			players := []*storage.MatchPlayerRecord{
				{
					MatchGUID: "guid-m1-test",
					PlayerID:  "Epic|local_hero|0",
					TeamNum:   0,
					IsLocal:   true,
					Won:       &wonTrue,
					Score:     500,
					Goals:     2,
					Assists:   1,
					Saves:     1,
					Shots:     3,
					CarName:   "Octane",
				},
				{
					MatchGUID: "guid-m1-test",
					PlayerID:  "Epic|teammate_buddy|0",
					TeamNum:   0,
					IsLocal:   false,
					Won:       &wonTrue,
					Score:     400,
					Goals:     1,
					Assists:   2,
					Saves:     2,
					Shots:     2,
					CarName:   "Fennec",
				},
				{
					MatchGUID: "guid-m1-test",
					PlayerID:  "Steam|opponent_alpha|0",
					TeamNum:   1,
					IsLocal:   false,
					Won:       &wonFalse,
					Score:     350,
					Goals:     1,
					Assists:   0,
					Saves:     1,
					Shots:     2,
					CarName:   "Dominus",
				},
			}

			if err := store.SaveMatchPlayers(ctx, "guid-m1-test", players); err != nil {
				t.Fatalf("SaveMatchPlayers failed: %v", err)
			}

			// Verify GetMatchPlayers
			retrieved, err := store.GetMatchPlayers(ctx, "guid-m1-test")
			if err != nil {
				t.Fatalf("GetMatchPlayers failed: %v", err)
			}
			if len(retrieved) != 3 {
				t.Fatalf("expected 3 retrieved match players, got %d", len(retrieved))
			}

			// Verify ListPlayerMatches for teammate: should have role="teammate" and result="victory"
			details, total, err := store.ListPlayerMatches(ctx, "Epic|teammate_buddy|0", storage.PlayerMatchFilter{
				Role:     "teammate",
				Result:   "victory",
				Playlist: 11,
			})
			if err != nil {
				t.Fatalf("ListPlayerMatches teammate failed: %v", err)
			}
			if total != 1 || len(details) != 1 {
				t.Fatalf("expected 1 match detail, got total=%d, len=%d", total, len(details))
			}
			if details[0].Role != "teammate" {
				t.Errorf("expected role teammate, got %s", details[0].Role)
			}
			if details[0].Result != "victory" {
				t.Errorf("expected result victory, got %s", details[0].Result)
			}
			if details[0].PlayerStats.CarName != "Fennec" {
				t.Errorf("expected car Fennec, got %s", details[0].PlayerStats.CarName)
			}

			// Verify ListPlayerMatches for opponent: should have role="opponent" and result="defeat"
			oppDetails, oppTotal, err := store.ListPlayerMatches(ctx, "Steam|opponent_alpha|0", storage.PlayerMatchFilter{
				Role:   "opponent",
				Result: "defeat",
			})
			if err != nil {
				t.Fatalf("ListPlayerMatches opponent failed: %v", err)
			}
			if oppTotal != 1 || len(oppDetails) != 1 {
				t.Fatalf("expected 1 opponent match detail, got total=%d, len=%d", oppTotal, len(oppDetails))
			}
			if oppDetails[0].Role != "opponent" {
				t.Errorf("expected role opponent, got %s", oppDetails[0].Role)
			}
			if oppDetails[0].Result != "defeat" {
				t.Errorf("expected result defeat, got %s", oppDetails[0].Result)
			}
		})
	}
}

// TestM1_MMRHistoryCRUD tests competitive MMR tracking, ordering, and playlist isolation.
func TestM1_MMRHistoryCRUD(t *testing.T) {
	ctx := context.Background()

	for _, entry := range getM1TestStores(t) {
		t.Run(entry.name, func(t *testing.T) {
			store := entry.factory(t)

			base := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)

			// Record MMR entries across playlists 11 and 13
			entries := []*storage.MMRHistoryRecord{
				{
					RecordedAt: base.Add(10 * time.Minute),
					MatchGUID:  "guid-mmr-1",
					PlaylistID: 11,
					MMR:        1050,
					MMRChange:  9,
					Tier:       14,
					Division:   1,
				},
				{
					RecordedAt: base.Add(20 * time.Minute),
					MatchGUID:  "guid-mmr-2",
					PlaylistID: 11,
					MMR:        1059,
					MMRChange:  9,
					Tier:       14,
					Division:   2,
				},
				{
					RecordedAt: base.Add(30 * time.Minute),
					MatchGUID:  "guid-mmr-3",
					PlaylistID: 13,
					MMR:        1120,
					MMRChange:  -8,
					Tier:       15,
					Division:   1,
				},
			}

			for _, e := range entries {
				if err := store.RecordMMR(ctx, e); err != nil {
					t.Fatalf("RecordMMR failed: %v", err)
				}
				if e.ID <= 0 {
					t.Fatalf("expected positive ID set on recorded MMR entry, got %d", e.ID)
				}
			}

			// Query history for playlist 11 (should return newest first)
			hist11, err := store.ListMMRHistory(ctx, 11, 10)
			if err != nil {
				t.Fatalf("ListMMRHistory(11) failed: %v", err)
			}
			if len(hist11) != 2 {
				t.Fatalf("expected 2 records for playlist 11, got %d", len(hist11))
			}
			if hist11[0].MatchGUID != "guid-mmr-2" || hist11[1].MatchGUID != "guid-mmr-1" {
				t.Errorf("unexpected ordering for playlist 11: %+v", hist11)
			}

			// Query history for playlist 13
			hist13, err := store.ListMMRHistory(ctx, 13, 10)
			if err != nil {
				t.Fatalf("ListMMRHistory(13) failed: %v", err)
			}
			if len(hist13) != 1 || hist13[0].MMR != 1120 {
				t.Errorf("unexpected playlist 13 record: %+v", hist13)
			}

			// Query all playlists (playlistID = 0)
			allHist, err := store.ListMMRHistory(ctx, 0, 10)
			if err != nil {
				t.Fatalf("ListMMRHistory(all) failed: %v", err)
			}
			if len(allHist) != 3 {
				t.Fatalf("expected 3 total records, got %d", len(allHist))
			}
			if allHist[0].MatchGUID != "guid-mmr-3" {
				t.Errorf("expected newest entry first (guid-mmr-3), got %s", allHist[0].MatchGUID)
			}
		})
	}
}

// TestM1_PlayerNotesAndTags tests updating and retrieving custom notes and tags.
func TestM1_PlayerNotesAndTags(t *testing.T) {
	ctx := context.Background()

	for _, entry := range getM1TestStores(t) {
		t.Run(entry.name, func(t *testing.T) {
			store := entry.factory(t)

			p := &storage.PlayerRecord{
				PlayerID:    "Epic|tag_player_01|0",
				Platform:    "Epic",
				PlayerName:  "TagTester",
				FirstSeenAt: time.Now().UTC(),
				LastSeenAt:  time.Now().UTC(),
			}
			if err := store.UpsertPlayer(ctx, p); err != nil {
				t.Fatalf("UpsertPlayer failed: %v", err)
			}

			// Update notes and tags
			if err := store.UpdatePlayerNotesAndTags(ctx, p.PlayerID, "Suspected alt account", []string{"alt", "freestyler"}); err != nil {
				t.Fatalf("UpdatePlayerNotesAndTags failed: %v", err)
			}

			retrieved, err := store.GetPlayer(ctx, p.PlayerID)
			if err != nil {
				t.Fatalf("GetPlayer failed: %v", err)
			}
			if retrieved.Notes != "Suspected alt account" {
				t.Errorf("expected notes %q, got %q", "Suspected alt account", retrieved.Notes)
			}
			if len(retrieved.Tags) != 2 || retrieved.Tags[0] != "alt" || retrieved.Tags[1] != "freestyler" {
				t.Errorf("expected tags [alt freestyler], got %v", retrieved.Tags)
			}
		})
	}
}

// TestM1_MatchStatsSyncAndRetryStatus tests stats sync lifecycle and retry resets.
func TestM1_MatchStatsSyncAndRetryStatus(t *testing.T) {
	ctx := context.Background()

	for _, entry := range getM1TestStores(t) {
		t.Run(entry.name, func(t *testing.T) {
			store := entry.factory(t)

			m := &storage.MatchRecord{
				MatchGUID:            "sync-match-01",
				RecordStartTimestamp: time.Now().UTC().Unix(),
				MapName:              "Utopia",
				Playlist:             11,
				DownloadStatus:       storage.DownloadDownloaded,
				UploadStatus:         storage.UploadUploaded,
				BallchasingID:        "bc-sync-01",
				StatsSyncStatus:      storage.StatsSyncPending,
				CreatedAt:            time.Now().UTC(),
				UpdatedAt:            time.Now().UTC(),
			}
			if err := store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{m}); err != nil {
				t.Fatalf("UpsertDiscoveredMatches failed: %v", err)
			}

			// 1. Verify match appears in ListPendingStatsSync
			pending, err := store.ListPendingStatsSync(ctx, 3)
			if err != nil {
				t.Fatalf("ListPendingStatsSync failed: %v", err)
			}
			if len(pending) != 1 || pending[0].MatchGUID != "sync-match-01" {
				t.Fatalf("expected sync-match-01 pending, got %+v", pending)
			}

			// 2. Mark syncing
			if err := store.MarkStatsSyncing(ctx, "sync-match-01"); err != nil {
				t.Fatalf("MarkStatsSyncing failed: %v", err)
			}
			mCheck, _ := store.GetMatch(ctx, "sync-match-01")
			if mCheck.StatsSyncStatus != storage.StatsSyncSyncing {
				t.Errorf("expected stats status SYNCING, got %s", mCheck.StatsSyncStatus)
			}

			// 3. Mark failed with error
			if err := store.MarkStatsSyncFailed(ctx, "sync-match-01", "Ballchasing rate limit 429"); err != nil {
				t.Fatalf("MarkStatsSyncFailed failed: %v", err)
			}
			mCheck, _ = store.GetMatch(ctx, "sync-match-01")
			if mCheck.StatsSyncStatus != storage.StatsSyncFailed || mCheck.StatsRetryCount != 1 {
				t.Errorf("expected FAILED with retry 1, got %s, retry %d", mCheck.StatsSyncStatus, mCheck.StatsRetryCount)
			}

			// 4. Reset match retry
			if err := store.ResetMatchRetry(ctx, "sync-match-01"); err != nil {
				t.Fatalf("ResetMatchRetry failed: %v", err)
			}
			mCheck, _ = store.GetMatch(ctx, "sync-match-01")
			if mCheck.StatsSyncStatus != storage.StatsSyncPending || mCheck.StatsRetryCount != 0 {
				t.Errorf("expected PENDING with retry 0 after reset, got %s, retry %d", mCheck.StatsSyncStatus, mCheck.StatsRetryCount)
			}

			// 5. Mark synced
			if err := store.MarkStatsSynced(ctx, "sync-match-01"); err != nil {
				t.Fatalf("MarkStatsSynced failed: %v", err)
			}
			mCheck, _ = store.GetMatch(ctx, "sync-match-01")
			if mCheck.StatsSyncStatus != storage.StatsSyncSynced || mCheck.SyncedAt == nil {
				t.Errorf("expected SYNCED with non-nil SyncedAt, got %s", mCheck.StatsSyncStatus)
			}
		})
	}
}

// TestM1_TimestampPreservationAndMonotonicity verifies out-of-order replay sync does not regress player timestamps.
func TestM1_TimestampPreservationAndMonotonicity(t *testing.T) {
	ctx := context.Background()

	for _, entry := range getM1TestStores(t) {
		t.Run(entry.name, func(t *testing.T) {
			store := entry.factory(t)

			olderMatchTime := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
			newerMatchTime := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

			// First, process the NEWER match
			err := store.RecordMatchResults(ctx, "match-newer", 11, []storage.PlayerOutcome{
				{PlayerID: "Epic|chrono_player|0", Platform: "Epic", PlayerName: "Chrono", IsTeammate: true, Won: true},
			}, newerMatchTime)
			if err != nil {
				t.Fatalf("RecordMatchResults (newer) failed: %v", err)
			}

			pAfterNewer, _ := store.GetPlayer(ctx, "Epic|chrono_player|0")
			if !pAfterNewer.FirstSeenAt.Equal(newerMatchTime) || !pAfterNewer.LastSeenAt.Equal(newerMatchTime) {
				t.Fatalf("unexpected timestamps after newer match: first=%v, last=%v", pAfterNewer.FirstSeenAt, pAfterNewer.LastSeenAt)
			}

			// Next, process the OLDER match (out of order sync)
			err = store.RecordMatchResults(ctx, "match-older", 11, []storage.PlayerOutcome{
				{PlayerID: "Epic|chrono_player|0", Platform: "Epic", PlayerName: "Chrono", IsTeammate: true, Won: false},
			}, olderMatchTime)
			if err != nil {
				t.Fatalf("RecordMatchResults (older) failed: %v", err)
			}

			pAfterOlder, _ := store.GetPlayer(ctx, "Epic|chrono_player|0")
			// FirstSeenAt should expand back to olderMatchTime (MIN)
			if !pAfterOlder.FirstSeenAt.Equal(olderMatchTime) {
				t.Errorf("expected FirstSeenAt to expand to %v, got %v", olderMatchTime, pAfterOlder.FirstSeenAt)
			}
			// LastSeenAt MUST NOT regress to olderMatchTime; it must stay at newerMatchTime (MAX)
			if !pAfterOlder.LastSeenAt.Equal(newerMatchTime) {
				t.Errorf("expected LastSeenAt to preserve %v, regressed to %v", newerMatchTime, pAfterOlder.LastSeenAt)
			}

			// Head-to-head matchup LastPlayedAt must also preserve newerMatchTime (MAX)
			matchup, err := store.GetPlayerMatchup(ctx, "Epic|chrono_player|0", 11)
			if err != nil {
				t.Fatalf("GetPlayerMatchup failed: %v", err)
			}
			if !matchup.LastPlayedAt.Equal(newerMatchTime) {
				t.Errorf("expected matchup LastPlayedAt to preserve %v, regressed to %v", newerMatchTime, matchup.LastPlayedAt)
			}
			if matchup.TotalMatches != 2 || matchup.WinsAsTeammate != 1 || matchup.LossesAsTeammate != 1 {
				t.Errorf("unexpected matchup statistics: %+v", matchup)
			}
		})
	}
}

// TestM1_RankHydrationFromReplay verifies player rank hydration from Ballchasing raw JSON.
func TestM1_RankHydrationFromReplay(t *testing.T) {
	ctx := context.Background()

	for _, entry := range getM1TestStores(t) {
		t.Run(entry.name, func(t *testing.T) {
			store := entry.factory(t)

			rawBCJSON := `{
				"blue": {
					"players": [
						{
							"id": {"id": "Epic|ranked_star|0"},
							"rank": {
								"id": 14,
								"tier": 14,
								"division": 3,
								"name": "Champion II Div IV"
							}
						}
					]
				},
				"orange": {
					"players": []
				}
			}`

			stats := &storage.ReplayMatchStats{
				MatchGUID: "rank-hydrate-match",
				Playlist:  11,
				Date:      "2026-09-25T14:30:00Z",
				RawJSON:   rawBCJSON,
				Players: []storage.ReplayPlayerStats{
					{
						Name:     "RankedStar",
						Platform: "Epic",
						OnlineID: "ranked_star",
						Team:     0,
					},
				},
			}

			if err := store.SyncReplayStats(ctx, stats); err != nil {
				t.Fatalf("SyncReplayStats failed: %v", err)
			}

			p, err := store.GetPlayer(ctx, "Epic|ranked_star|0")
			if err != nil {
				t.Fatalf("GetPlayer failed: %v", err)
			}

			var ranks map[string]interface{}
			if err := json.Unmarshal([]byte(p.RanksJSON), &ranks); err != nil {
				t.Fatalf("failed to unmarshal ranks JSON: %v, raw=%s", err, p.RanksJSON)
			}
			if _, ok := ranks["11"]; !ok {
				t.Fatalf("expected playlist 11 hydrated in ranks JSON, got %s", p.RanksJSON)
			}
		})
	}
}
