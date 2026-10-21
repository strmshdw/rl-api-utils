package storage

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dank/rl-api-utils/internal/ballchasing"
)

func TestSQLiteStore_RepairHistoricalMatchesAndOutcomes(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "repair.db")

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	// Simulate a historical match inserted when Ballchasing team goals were 0-0 and winner_team was -1,
	// with player goals present in replay_stats_json and is_local = 1 in match_players.
	rawStats := `{"match_guid":"GUID-REPAIR-1","map_name":"DFH Stadium","playlist":11,"blue_score":0,"orange_score":0,"duration":300,"overtime":false,"date":"2026-03-14T22:47:13Z","players":[{"name":"LocalPlayer","platform":"Steam","online_id":"76561198816594023","team":0,"score":650,"goals":3,"assists":1,"saves":2,"shots":5,"demos":1},{"name":"TeammateP","platform":"Steam","online_id":"76561198000000010","team":0,"score":420,"goals":1,"assists":2,"saves":1,"shots":3,"demos":0},{"name":"OpponentP1","platform":"Epic","online_id":"epic_opp_1","team":1,"score":380,"goals":2,"assists":0,"saves":1,"shots":4,"demos":0},{"name":"OpponentP2","platform":"Epic","online_id":"epic_opp_2","team":1,"score":210,"goals":0,"assists":1,"saves":0,"shots":2,"demos":0}],"ballchasing_id":"bc-repair-1","ballchasing_url":"https://ballchasing.com/api/replays/bc-repair-1"}`

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO matches (
			match_guid, record_start_timestamp, map_name, playlist,
			download_status, upload_status, ballchasing_id, ballchasing_url,
			blue_score, orange_score, winner_team, replay_stats_json,
			created_at, updated_at
		) VALUES (
			'GUID-REPAIR-1', 1773528433, 'DFH Stadium', 11,
			'DOWNLOADED', 'UPLOADED', 'bc-repair-1', 'https://ballchasing.com/api/replays/bc-repair-1',
			0, 0, -1, ?,
			1773528433, 1773528433
		)
	`, rawStats)
	if err != nil {
		_ = store.Close()
		t.Fatalf("insert match failed: %v", err)
	}

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO match_players (match_guid, player_id, team_num, is_local, won, score, goals, assists, saves, shots, demos)
		VALUES
			('GUID-REPAIR-1', 'Steam|76561198816594023|0', 0, 1, NULL, 650, 3, 1, 2, 5, 1),
			('GUID-REPAIR-1', 'Steam|76561198000000010|0', 0, 0, NULL, 420, 1, 2, 1, 3, 0),
			('GUID-REPAIR-1', 'Epic|epic_opp_1|0', 1, 0, NULL, 380, 2, 0, 1, 4, 0),
			('GUID-REPAIR-1', 'Epic|epic_opp_2|0', 1, 0, NULL, 210, 0, 1, 0, 2, 0)
	`)
	if err != nil {
		t.Fatalf("insert match_players failed: %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Re-open the store to trigger startup repairHistoricalMatchesAndOutcomes
	store, err = NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("re-open NewSQLiteStore failed: %v", err)
	}
	defer store.Close()

	m, err := store.GetMatch(ctx, "GUID-REPAIR-1")
	if err != nil {
		t.Fatalf("GetMatch failed: %v", err)
	}
	if m.BlueScore != 4 || m.OrangeScore != 2 {
		t.Errorf("repaired score = %d-%d; want 4-2", m.BlueScore, m.OrangeScore)
	}
	if m.WinnerTeam == nil || *m.WinnerTeam != 0 {
		t.Errorf("repaired WinnerTeam = %v; want 0", m.WinnerTeam)
	}
	if m.BallchasingURL != "https://ballchasing.com/replay/bc-repair-1" {
		t.Errorf("repaired BallchasingURL = %q; want https://ballchasing.com/replay/bc-repair-1", m.BallchasingURL)
	}

	// Verify HistoricalMatches outcome is victory
	hist, total, err := store.ListHistoricalMatches(ctx, MatchFilter{Limit: 10})
	if err != nil || total != 1 {
		t.Fatalf("ListHistoricalMatches failed: total=%d err=%v", total, err)
	}
	if hist[0].Result != "victory" {
		t.Errorf("hist[0].Result = %q; want victory", hist[0].Result)
	}
	if hist[0].Stats == nil || hist[0].Stats.BlueScore != 4 || hist[0].Stats.OrangeScore != 2 {
		t.Errorf("hist[0].Stats scores = %+v; want 4-2", hist[0].Stats)
	}

	// Verify player matchups were backfilled for teammate and opponents (excluding local player)
	tmMatchup, err := store.GetPlayerMatchup(ctx, "76561198000000010", 11) // also tests raw online_id resolution!
	if err != nil {
		t.Fatalf("GetPlayerMatchup teammate failed: %v", err)
	}
	if tmMatchup.WinsAsTeammate != 1 || tmMatchup.TotalMatches != 1 {
		t.Errorf("teammate matchup = %+v; want WinsAsTeammate=1, TotalMatches=1", tmMatchup)
	}

	oppMatchup, err := store.GetPlayerMatchup(ctx, "epic_opp_1", 11)
	if err != nil {
		t.Fatalf("GetPlayerMatchup opponent failed: %v", err)
	}
	if oppMatchup.WinsAsOpponent != 1 || oppMatchup.TotalMatches != 1 {
		t.Errorf("opponent matchup = %+v; want WinsAsOpponent=1, TotalMatches=1", oppMatchup)
	}
}

func TestStores_ResolveRawOnlineIDAndPreserveFullTelemetry(t *testing.T) {
	ctx := context.Background()
	sqliteStore, err := NewSQLiteStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer sqliteStore.Close()

	jsonStore, err := NewJSONStore(filepath.Join(t.TempDir(), "test.json"))
	if err != nil {
		t.Fatalf("NewJSONStore: %v", err)
	}
	defer jsonStore.Close()

	for name, st := range map[string]StateStore{"sqlite": sqliteStore, "json": jsonStore} {
		t.Run(name, func(t *testing.T) {
			stats := &ReplayMatchStats{
				MatchGUID:      "GUID-TELEM-1",
				MapName:        "Champions Field",
				Playlist:       13,
				BlueScore:      0, // Should auto-compute 2-1 from player goals
				OrangeScore:    0,
				Duration:       300,
				Date:           "2026-10-15T20:00:00Z",
				BallchasingID:  "bc-telem-1",
				BallchasingURL: "https://ballchasing.com/api/replays/bc-telem-1",
				Players: []ReplayPlayerStats{
					{
						Name:     "Mist",
						Platform: "Steam",
						OnlineID: "76561198245238200",
						Team:     0,
						Score:    650,
						Goals:    2,
						Assists:  0,
						Saves:    3,
						Shots:    4,
						Demos:    2,
						CarName:  "Octane",
						Rank: &ballchasing.PlayerRank{
							ID:       "grand-champion-2",
							Tier:     20,
							Division: 3,
							Name:     "Grand Champion II",
						},
						Boost: &ballchasing.BoostStats{
							BPM:       412.5,
							AvgAmount: 52.1,
						},
						Movement: &ballchasing.MovementStats{
							AvgSpeed:          1550.0,
							PercentSupersonic: 22.4,
						},
					},
					{
						Name:     "OpponentX",
						Platform: "Epic",
						OnlineID: "epic_oppx_99",
						Team:     1,
						Score:    310,
						Goals:    1,
						Assists:  0,
						Saves:    1,
						Shots:    2,
						Demos:    0,
						CarName:  "Fennec",
					},
				},
			}

			if err := st.SyncReplayStats(ctx, stats); err != nil {
				t.Fatalf("SyncReplayStats: %v", err)
			}

			// Lookup by raw OnlineID (without "Steam|" and "|0")
			p, err := st.GetPlayer(ctx, "76561198245238200")
			if err != nil {
				t.Fatalf("GetPlayer by raw online_id failed: %v", err)
			}
			if p.PlayerID != "Steam|76561198245238200|0" {
				t.Errorf("PlayerID = %q; want Steam|76561198245238200|0", p.PlayerID)
			}
			if p.RanksJSON == "" || p.RanksJSON == "{}" {
				t.Errorf("expected RanksJSON to be hydrated from ReplayPlayerStats.Rank, got %q", p.RanksJSON)
			}

			// ListPlayerMatches by raw OnlineID
			pMatches, total, err := st.ListPlayerMatches(ctx, "76561198245238200", PlayerMatchFilter{Limit: 10})
			if err != nil || total != 1 || len(pMatches) != 1 {
				t.Fatalf("ListPlayerMatches by raw online_id: total=%d len=%d err=%v", total, len(pMatches), err)
			}
			if pMatches[0].BlueScore != 2 || pMatches[0].OrangeScore != 1 {
				t.Errorf("score = %d-%d; want 2-1", pMatches[0].BlueScore, pMatches[0].OrangeScore)
			}
			if pMatches[0].Result != "victory" {
				t.Errorf("Result = %q; want victory", pMatches[0].Result)
			}
			if pMatches[0].Stats == nil || len(pMatches[0].Stats.Players) != 2 || pMatches[0].Stats.Players[0].Boost == nil {
				t.Errorf("expected full Boost telemetry in Stats.Players[0], got %+v", pMatches[0].Stats)
			}

			// UpdatePlayerNotesAndTags by raw OnlineID
			if err := st.UpdatePlayerNotesAndTags(ctx, "76561198245238200", "Great passer", []string{"cracked"}); err != nil {
				t.Fatalf("UpdatePlayerNotesAndTags by raw online_id failed: %v", err)
			}
			pAfter, _ := st.GetPlayer(ctx, "76561198245238200")
			if pAfter.Notes != "Great passer" {
				t.Errorf("Notes = %q; want Great passer", pAfter.Notes)
			}
		})
	}
}

func TestSQLiteStore_MigrationDeduplicatesConsoleAliasesAndFixesInvertedMatchups(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "migrate.db")

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	// Reset user_version to 0 to simulate pre-migration database state
	_, _ = store.db.ExecContext(ctx, "PRAGMA user_version = 0;")
	_ = store.SaveAuthState(ctx, "steam", "tok", "Steam|76561198816594023|0", "strmshdw")

	rawStats := `{"match_guid":"GUID-MIG-1","map_name":"Mannfield","playlist":11,"blue_score":3,"orange_score":1,"winner_team":0,"duration":300,"date":"2026-03-14T22:47:13Z","players":[{"name":"strmshdw","platform":"Steam","online_id":"76561198816594023","team":0,"score":500,"goals":3},{"name":"hungry-panda88","platform":"PlayStation","online_id":"hungry-panda88","team":1,"score":250,"goals":1}],"ballchasing_id":"bc-mig-1","ballchasing_url":"https://ballchasing.com/replay/bc-mig-1"}`

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO matches (
			match_guid, record_start_timestamp, map_name, playlist,
			download_status, upload_status, ballchasing_id, ballchasing_url,
			blue_score, orange_score, winner_team, replay_stats_json,
			stats_sync_status, created_at, updated_at
		) VALUES (
			'GUID-MIG-1', 1773528433, 'Mannfield', 11,
			'DOWNLOADED', 'UPLOADED', 'bc-mig-1', 'https://ballchasing.com/replay/bc-mig-1',
			3, 1, 0, ?,
			'SYNCED', 1773528433, 1773528433
		);
	`, rawStats)
	if err != nil {
		t.Fatalf("insert match: %v", err)
	}

	// Simulate local player in players + 3 duplicate console rows (PS4|<id>|0, PlayStation|<id>|0, PlayStation|<name>|0) with wall-clock last_seen_at
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO players (player_id, platform, player_name, player_name_lower, ranks_json, first_seen_at, last_seen_at)
		VALUES
			('Steam|76561198816594023|0', 'Steam', 'strmshdw', 'strmshdw', '{}', 1792141737, 1792141737),
			('PS4|7370223800633025836|0', 'PS4', 'hungry-panda88', 'hungry-panda88', '{}', 1773528433, 1792141737),
			('PlayStation|7370223800633025836|0', 'PlayStation', 'hungry-panda88', 'hungry-panda88', '{}', 1792141737, 1792141737),
			('PlayStation|hungry-panda88|0', 'PlayStation', 'hungry-panda88', 'hungry-panda88', '{"11":{"tier":13}}', 1792141737, 1792141737);
	`)
	if err != nil {
		t.Fatalf("insert players: %v", err)
	}

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO match_players (match_guid, player_id, team_num, is_local, won, score, goals, assists, saves, shots, demos, car_name)
		VALUES
			('GUID-MIG-1', 'Steam|76561198816594023|0', 0, 1, 1, 500, 3, 0, 0, 4, 0, 'Octane'),
			('GUID-MIG-1', 'PS4|7370223800633025836|0', 1, 0, 0, 250, 1, 0, 1, 2, 0, ''),
			('GUID-MIG-1', 'PlayStation|7370223800633025836|0', 1, 0, 0, 250, 1, 0, 1, 2, 0, ''),
			('GUID-MIG-1', 'PlayStation|hungry-panda88|0', 1, 0, 0, 250, 1, 0, 1, 2, 0, 'Fennec');
	`)
	if err != nil {
		t.Fatalf("insert match_players: %v", err)
	}

	// Simulate inverted outcome (LossesAsOpponent = 1 instead of WinsAsOpponent = 1)
	_, _ = store.db.ExecContext(ctx, `INSERT INTO processed_match_outcomes (match_guid, playlist_id, processed_at) VALUES ('GUID-MIG-1', 11, 1792141737);`)
	_, _ = store.db.ExecContext(ctx, `INSERT INTO player_matchups (player_id, playlist_id, wins_as_teammate, losses_as_teammate, wins_as_opponent, losses_as_opponent, total_matches, last_played_at) VALUES ('PS4|7370223800633025836|0', 11, 0, 0, 0, 1, 1, 1792141737);`)

	_ = store.Close()

	// Re-open to trigger migration
	store, err = NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("re-open NewSQLiteStore: %v", err)
	}
	defer store.Close()

	// 1. Local player must not be in players table
	if _, err := store.GetPlayer(ctx, "Steam|76561198816594023|0"); err == nil {
		t.Errorf("expected local player to be removed from players directory")
	}

	// 2. All 3 console aliases should resolve to PS4|7370223800633025836|0 with merged car_name and ranks_json
	p, err := store.GetPlayer(ctx, "hungry-panda88")
	if err != nil {
		t.Fatalf("GetPlayer by name failed: %v", err)
	}
	if p.PlayerID != "PS4|7370223800633025836|0" {
		t.Errorf("canonical PlayerID = %q; want PS4|7370223800633025836|0", p.PlayerID)
	}
	if p.LastSeenAt.Unix() != 1773528433 {
		t.Errorf("repaired LastSeenAt = %d; want 1773528433", p.LastSeenAt.Unix())
	}

	// 3. Rebuilt matchup should have WinsAsOpponent = 1, LossesAsOpponent = 0
	mu, err := store.GetPlayerMatchup(ctx, "PS4|7370223800633025836|0", 11)
	if err != nil {
		t.Fatalf("GetPlayerMatchup: %v", err)
	}
	if mu.WinsAsOpponent != 1 || mu.LossesAsOpponent != 0 {
		t.Errorf("matchup = %+v; want WinsAsOpponent=1, LossesAsOpponent=0", mu)
	}

	// 4. Match lacking "boost": telemetry should be marked PENDING so PollAndSyncPendingStats hydrates it
	m, err := store.GetMatch(ctx, "GUID-MIG-1")
	if err != nil {
		t.Fatalf("GetMatch: %v", err)
	}
	if m.StatsSyncStatus != StatsSyncPending {
		t.Errorf("StatsSyncStatus = %q; want PENDING", m.StatsSyncStatus)
	}

	// 5. FilterPlayerSummaries with Platform="playstation" matches PS4 player
	summaries, total, err := store.FilterPlayerSummaries(ctx, PlayerDirectoryFilter{Platform: "playstation", Limit: 10})
	if err != nil || total != 1 || len(summaries) != 1 {
		t.Fatalf("FilterPlayerSummaries(playstation): total=%d len=%d err=%v", total, len(summaries), err)
	}

	// 6. Even if player_matchups row is missing, GetPlayerMatchups and GetPlayerMatchup fall back to match_players + matches
	_, _ = store.db.ExecContext(ctx, "DELETE FROM player_matchups WHERE player_id = 'PS4|7370223800633025836|0';")
	fbMatchups, err := store.GetPlayerMatchups(ctx, "hungry-panda88")
	if err != nil || len(fbMatchups) != 1 {
		t.Fatalf("GetPlayerMatchups fallback: len=%d err=%v", len(fbMatchups), err)
	}
	if fbMatchups[0].WinsAsOpponent != 1 || fbMatchups[0].TotalMatches != 1 {
		t.Errorf("GetPlayerMatchups fallback = %+v; want WinsAsOpponent=1, TotalMatches=1", fbMatchups[0])
	}
	fbSingle, err := store.GetPlayerMatchup(ctx, "hungry-panda88", 11)
	if err != nil || fbSingle.WinsAsOpponent != 1 || fbSingle.TotalMatches != 1 {
		t.Errorf("GetPlayerMatchup fallback = %+v err=%v; want WinsAsOpponent=1, TotalMatches=1", fbSingle, err)
	}
}



