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

func TestCanonical23TierRankFormatting(t *testing.T) {
	expectedBase := map[int]string{
		0:  "Unranked",
		1:  "Bronze I",
		2:  "Bronze II",
		3:  "Bronze III",
		4:  "Silver I",
		5:  "Silver II",
		6:  "Silver III",
		7:  "Gold I",
		8:  "Gold II",
		9:  "Gold III",
		10: "Platinum I",
		11: "Platinum II",
		12: "Platinum III",
		13: "Diamond I",
		14: "Diamond II",
		15: "Diamond III",
		16: "Champion I",
		17: "Champion II",
		18: "Champion III",
		19: "Grand Champion I",
		20: "Grand Champion II",
		21: "Grand Champion III",
		22: "Supersonic Legend",
	}

	expectedShortBase := map[int]string{
		0:  "UNRANKED",
		1:  "BRZ I",
		2:  "BRZ II",
		3:  "BRZ III",
		4:  "SLV I",
		5:  "SLV II",
		6:  "SLV III",
		7:  "GOLD I",
		8:  "GOLD II",
		9:  "GOLD III",
		10: "PLAT I",
		11: "PLAT II",
		12: "PLAT III",
		13: "DIA I",
		14: "DIA II",
		15: "DIA III",
		16: "CHAMP I",
		17: "CHAMP II",
		18: "CHAMP III",
		19: "GC I",
		20: "GC II",
		21: "GC III",
		22: "SSL",
	}

	divSuffix := map[int]string{
		1: "Div I",
		2: "Div II",
		3: "Div III",
		4: "Div IV",
	}
	shortDivSuffix := map[int]string{
		1: "DIV I",
		2: "DIV II",
		3: "DIV III",
		4: "DIV IV",
	}

	for tier := 0; tier <= 22; tier++ {
		base := expectedBase[tier]
		shortBase := expectedShortBase[tier]

		// Division 0 (omitted)
		if got := FormatRankName(tier, 0); got != base {
			t.Errorf("FormatRankName(%d, 0) = %q; want %q", tier, got, base)
		}
		if gotShort := FormatShortRankName(tier, 0); gotShort != shortBase {
			t.Errorf("FormatShortRankName(%d, 0) = %q; want %q", tier, gotShort, shortBase)
		}

		// Divisions 1..4
		for div := 1; div <= 4; div++ {
			wantFull := base
			wantShort := shortBase
			if tier > 0 && tier < 22 {
				wantFull = base + " " + divSuffix[div]
				wantShort = shortBase + " · " + shortDivSuffix[div]
			}
			if got := FormatRankName(tier, div); got != wantFull {
				t.Errorf("FormatRankName(%d, %d) = %q; want %q", tier, div, got, wantFull)
			}
			if gotShort := FormatShortRankName(tier, div); gotShort != wantShort {
				t.Errorf("FormatShortRankName(%d, %d) = %q; want %q", tier, div, gotShort, wantShort)
			}
		}
	}

	// Spot-check examples from specification
	if got := FormatShortRankName(19, 2); got != "GC I · DIV II" {
		t.Errorf("FormatShortRankName(19, 2) = %q; want 'GC I · DIV II'", got)
	}
	if got := FormatShortRankName(22, 1); got != "SSL" {
		t.Errorf("FormatShortRankName(22, 1) = %q; want 'SSL'", got)
	}
	if got := FormatShortRankName(18, 4); got != "CHAMP III · DIV IV" {
		t.Errorf("FormatShortRankName(18, 4) = %q; want 'CHAMP III · DIV IV'", got)
	}
	if got := FormatShortRankName(15, 4); got != "DIA III · DIV IV" {
		t.Errorf("FormatShortRankName(15, 4) = %q; want 'DIA III · DIV IV'", got)
	}
	if got := FormatShortRankName(11, 1); got != "PLAT II · DIV I" {
		t.Errorf("FormatShortRankName(11, 1) = %q; want 'PLAT II · DIV I'", got)
	}
}

func TestRepairHistoricalData_RepairsCorruptedRanksJSON(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "ranks_repair.db")

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer store.Close()

	// Insert a player with corrupted legacy 19-tier rank names (e.g. tier 13 labeled "Champion I Div II", tier 20 labeled "Tier 20",
	// tier 13 Roman-numeral prefix collision labeled "Diamond II Div I", and tier 19 0-based PsyNet division 0 "Grand Champion I Division I")
	corruptedRanks := `{"10":{"playlist_id":10,"playlist_name":"Ranked Duel 1v1","tier":13,"division":1,"rank_name":"Diamond II Div I","mmr":910},"11":{"playlist_id":11,"playlist_name":"Ranked Doubles 2v2","tier":13,"division":2,"rank_name":"Champion I Div II","mmr":980},"13":{"playlist_id":13,"playlist_name":"Ranked Standard 3v3","tier":20,"division":3,"rank_name":"Tier 20","mmr":1580},"27":{"playlist_id":27,"playlist_name":"Ranked Hoops","tier":19,"division":0,"rank_name":"Grand Champion I Division I","mmr":1435}}`
	err = store.UpsertPlayer(ctx, &PlayerRecord{
		PlayerID:   "Steam|76561198000012345|0",
		Platform:   "Steam",
		PlayerName: "DiamondAndGCPlayer",
		RanksJSON:  corruptedRanks,
	})
	if err != nil {
		t.Fatalf("UpsertPlayer failed: %v", err)
	}

	if err := store.RepairHistoricalData(ctx); err != nil {
		t.Fatalf("RepairHistoricalData failed: %v", err)
	}

	p, err := store.GetPlayer(ctx, "Steam|76561198000012345|0")
	if err != nil {
		t.Fatalf("GetPlayer failed: %v", err)
	}

	// Verify tier 13 (playlist 10, previously "Diamond II Div I") is repaired to "Diamond I Div I" ("DIA I · DIV I")
	if !containsSubstr(p.RanksJSON, `"Diamond I Div I"`) || !containsSubstr(p.RanksJSON, `"DIA I · DIV I"`) {
		t.Errorf("expected repaired Diamond I Div I in RanksJSON, got %s", p.RanksJSON)
	}
	// Verify tier 13 (playlist 11) is now "Diamond I Div II" ("DIA I · DIV II") and tier 20 is "Grand Champion II Div III" ("GC II · DIV III")
	if !containsSubstr(p.RanksJSON, `"Diamond I Div II"`) || !containsSubstr(p.RanksJSON, `"DIA I · DIV II"`) {
		t.Errorf("expected repaired Diamond I Div II in RanksJSON, got %s", p.RanksJSON)
	}
	if !containsSubstr(p.RanksJSON, `"Grand Champion II Div III"`) || !containsSubstr(p.RanksJSON, `"GC II · DIV III"`) {
		t.Errorf("expected repaired Grand Champion II Div III in RanksJSON, got %s", p.RanksJSON)
	}
	// Verify tier 19 (playlist 27, division 0 "Division I") has short_rank "GC I · DIV I"
	if !containsSubstr(p.RanksJSON, `"GC I · DIV I"`) {
		t.Errorf("expected short_rank GC I · DIV I for 0-based PsyNet division 0, got %s", p.RanksJSON)
	}
}

func containsSubstr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || (len(s) > 0 && indexSubstr(s, sub) >= 0))
}

func indexSubstr(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestSQLiteStore_SyncReplayStats_ResolvesExistingMatchAndCanonicalPlayers(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "syncreplays.db")

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer store.Close()

	// 1. Seed a match discovered via PsyNet with upload_status = 'PENDING' and a PS4 opponent
	err = store.UpsertDiscoveredMatches(ctx, []*MatchRecord{
		{
			MatchGUID:            "PSYNET-GUID-100",
			RecordStartTimestamp: 1773528433,
			MapName:              "DFH Stadium",
			Playlist:             10,
			DownloadStatus:       DownloadDownloaded,
			UploadStatus:         UploadPending,
			BallchasingID:        "bc-sync-100",
		},
	})
	if err != nil {
		t.Fatalf("UpsertDiscoveredMatches: %v", err)
	}

	err = store.UpsertPlayer(ctx, &PlayerRecord{
		PlayerID:   "PS4|7370223800633025836|0",
		Platform:   "PS4",
		PlayerName: "ConsoleHero",
	})
	if err != nil {
		t.Fatalf("UpsertPlayer: %v", err)
	}

	err = store.SaveMatchPlayers(ctx, "PSYNET-GUID-100", []*MatchPlayerRecord{
		{
			MatchGUID:  "PSYNET-GUID-100",
			PlayerID:   "Steam|76561198816594023|0",
			Platform:   "Steam",
			PlayerName: "LocalPilot",
			TeamNum:    0,
			IsLocal:    true,
			Score:      500,
			Goals:      3,
		},
		{
			MatchGUID:  "PSYNET-GUID-100",
			PlayerID:   "PS4|7370223800633025836|0",
			Platform:   "PS4",
			PlayerName: "ConsoleHero",
			TeamNum:    1,
			IsLocal:    false,
			Score:      320,
			Goals:      1,
		},
	})
	if err != nil {
		t.Fatalf("SaveMatchPlayers initial: %v", err)
	}

	// 2. Sync replay stats from Ballchasing where MatchGUID in replay is the ballchasing ID or lowercase GUID,
	// and opponent is reported as PlayStation|ConsoleHero|0
	st := &ReplayMatchStats{
		MatchGUID:      "bc-sync-100",
		MapName:        "DFH Stadium",
		Playlist:       10,
		BlueScore:      3,
		OrangeScore:    1,
		Duration:       300,
		Date:           "2026-03-14T22:47:13Z",
		BallchasingID:  "bc-sync-100",
		BallchasingURL: "https://ballchasing.com/replay/bc-sync-100",
		Players: []ReplayPlayerStats{
			{
				Name:     "LocalPilot",
				Platform: "Steam",
				OnlineID: "76561198816594023",
				Team:     0,
				IsLocal:  true,
				Score:    500,
				Goals:    3,
				Saves:    2,
				Shots:    5,
				Boost:    &ballchasing.BoostStats{BPM: 425, AvgAmount: 48},
			},
			{
				Name:     "ConsoleHero",
				Platform: "PlayStation",
				OnlineID: "ConsoleHero",
				Team:     1,
				IsLocal:  false,
				Score:    320,
				Goals:    1,
				Saves:    1,
				Shots:    3,
				Boost:    &ballchasing.BoostStats{BPM: 360, AvgAmount: 41},
			},
		},
	}

	if err := store.SyncReplayStats(ctx, st); err != nil {
		t.Fatalf("SyncReplayStats: %v", err)
	}
	if st.MatchGUID != "PSYNET-GUID-100" {
		t.Errorf("resolved st.MatchGUID = %q; want PSYNET-GUID-100", st.MatchGUID)
	}

	// Follow up with SaveMatchPlayers (as syncer does) with PlayStation|ConsoleHero|0
	err = store.SaveMatchPlayers(ctx, st.MatchGUID, []*MatchPlayerRecord{
		{
			MatchGUID:  st.MatchGUID,
			PlayerID:   "Steam|76561198816594023|0",
			Platform:   "Steam",
			PlayerName: "LocalPilot",
			TeamNum:    0,
			IsLocal:    true,
			Score:      500,
			Goals:      3,
		},
		{
			MatchGUID:  st.MatchGUID,
			PlayerID:   "PlayStation|ConsoleHero|0",
			Platform:   "PlayStation",
			PlayerName: "ConsoleHero",
			TeamNum:    1,
			IsLocal:    false,
			Score:      320,
			Goals:      1,
		},
	})
	if err != nil {
		t.Fatalf("SaveMatchPlayers follow-up: %v", err)
	}

	// Verify only 1 match exists and its upload_status is UPLOADED and stats_sync_status is SYNCED
	matches, total, err := store.ListHistoricalMatches(ctx, MatchFilter{Limit: 10})
	if err != nil || total != 1 {
		t.Fatalf("ListHistoricalMatches: total=%d err=%v", total, err)
	}
	if matches[0].UploadStatus != UploadUploaded || matches[0].StatsSyncStatus != StatsSyncSynced {
		t.Errorf("match status = upload:%s stats:%s; want UPLOADED / SYNCED", matches[0].UploadStatus, matches[0].StatsSyncStatus)
	}

	// Verify match_players has exactly 2 rows (no duplicate PlayStation|ConsoleHero|0 row) and both have Boost telemetry
	mps, err := store.GetMatchPlayers(ctx, "PSYNET-GUID-100")
	if err != nil {
		t.Fatalf("GetMatchPlayers: %v", err)
	}
	if len(mps) != 2 {
		t.Fatalf("len(mps) = %d; want 2 (%+v)", len(mps), mps)
	}
	for _, mp := range mps {
		if !containsSubstr(mp.StatsJSON, `"bpm":`) {
			t.Errorf("expected stats_json with bpm for player %s, got %q", mp.PlayerID, mp.StatsJSON)
		}
	}

	// Verify LocalPilot was NOT inserted into players table
	if _, err := store.GetPlayer(ctx, "Steam|76561198816594023|0"); err == nil {
		t.Errorf("expected local player not to be inserted into players directory")
	}

	// Verify ExtractPlayerStatsJSONFromReplay resolves ConsoleHero from replay_stats_json even if mpStatsJSON is empty
	extracted := ExtractPlayerStatsJSONFromReplay("", matches[0].ReplayStatsJSON, "PS4|7370223800633025836|0", "", 1)
	if !containsSubstr(extracted, `"bpm":360`) {
		t.Errorf("expected ExtractPlayerStatsJSONFromReplay to extract ConsoleHero bpm:360, got %q", extracted)
	}
}





