package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/domain"
)

func TestParsePlaylistFilter_ExpandedModesAndCSV(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []int
	}{
		{name: "empty", raw: "", want: nil},
		{name: "all", raw: "all", want: nil},
		{name: "zero", raw: "0", want: nil},
		{name: "single_numeric", raw: "11", want: []int{11}},
		{name: "1v1", raw: "1v1", want: []int{10, 1}},
		{name: "2v2", raw: "2v2", want: []int{11, 2}},
		{name: "3v3", raw: "3v3", want: []int{13, 3, 12}},
		{name: "extra", raw: "extra", want: []int{27, 28, 29, 30, 34}},
		{name: "hoops", raw: "hoops", want: []int{27}},
		{name: "rumble", raw: "rumble", want: []int{28}},
		{name: "dropshot", raw: "dropshot", want: []int{29}},
		{name: "snowday", raw: "snowday", want: []int{30}},
		{name: "snow_day", raw: "snow_day", want: []int{30}},
		{name: "tournament", raw: "tournament", want: []int{34}},
		{name: "tournaments", raw: "tournaments", want: []int{34}},
		{name: "casual", raw: "casual", want: []int{1, 2, 3, 4, 15, 16, 17, 18, 23}},
		{name: "unranked", raw: " UNRANKED ", want: []int{1, 2, 3, 4, 15, 16, 17, 18, 23}},
		{name: "private", raw: "private", want: []int{6, 7, 8, 22, 24}},
		{name: "custom", raw: "Custom", want: []int{6, 7, 8, 22, 24}},
		{name: "ranked_1v1", raw: "ranked_1v1", want: []int{10}},
		{name: "ranked_2v2", raw: "ranked_2v2", want: []int{11}},
		{name: "ranked_3v3", raw: "ranked_3v3", want: []int{13, 12}},
		{name: "csv_extra_ranked", raw: "27,28,29,30", want: []int{27, 28, 29, 30}},
		{name: "csv_dedup_and_trim", raw: " 11 , 13 , 11 ", want: []int{11, 13}},
		{name: "csv_partial_valid", raw: "27,invalid,-3,0,28", want: []int{27, 28}},
		{name: "csv_all_invalid", raw: "invalid,-5,0,abc", want: nil},
		{name: "unknown_token", raw: "mystery_mode", want: nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParsePlaylistFilter(tc.raw)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParsePlaylistFilter(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

type scopedSpotlightBackend interface {
	StateStore
	GetScopedPlayerSpotlights(ctx context.Context, playlist string, fromUnix, toUnix int64) (*GlobalSpotlights, error)
	GetScopedPlayerMatchups(ctx context.Context, playerID string, fromUnix, toUnix int64) ([]*PlayerMatchup, error)
}

func openScopedTestBackends(t *testing.T) map[string]scopedSpotlightBackend {
	t.Helper()
	sqlStore := openTestSQLite(t)
	jsonStore, err := NewJSONStore(filepath.Join(t.TempDir(), "scoped_test.json"))
	if err != nil {
		t.Fatalf("NewJSONStore failed: %v", err)
	}
	t.Cleanup(func() { _ = jsonStore.Close() })

	return map[string]scopedSpotlightBackend{
		"sqlite": sqlStore,
		"json":   jsonStore,
	}
}

func seedMatchWithLocalAndPeer(
	t *testing.T,
	ctx context.Context,
	store StateStore,
	guid string,
	playlist int,
	ts int64,
	localID string,
	peerID, peerPlat, peerName string,
	isTeammate bool,
	localWon bool,
	peerBPM float64,
) {
	t.Helper()
	winnerTeam := 0
	blueScore, orangeScore := 3, 1
	if !localWon {
		winnerTeam = 1
		blueScore, orangeScore = 1, 3
	}
	peerTeam := 0
	if !isTeammate {
		peerTeam = 1
	}
	peerWon := (peerTeam == winnerTeam)

	statsJSON := ""
	if peerBPM > 0 {
		raw, _ := json.Marshal(ReplayPlayerStats{
			Name:     peerName,
			Platform: peerPlat,
			OnlineID: peerID,
			Team:     peerTeam,
			Score:    550,
			Goals:    2,
			Boost:    &domain.BoostStats{BPM: peerBPM, BCPM: peerBPM + 20, AvgAmount: 45},
		})
		statsJSON = string(raw)
	}

	if err := store.SyncReplayStats(ctx, &ReplayMatchStats{
		MatchGUID:   guid,
		MapName:     "DFH Stadium",
		Playlist:    playlist,
		BlueScore:   blueScore,
		OrangeScore: orangeScore,
		WinnerTeam:  &winnerTeam,
		Duration:    300,
		Date:        time.Unix(ts, 0).UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("SyncReplayStats(%s): %v", guid, err)
	}

	if err := store.SaveMatchPlayers(ctx, guid, []*MatchPlayerRecord{
		{
			MatchGUID:  guid,
			PlayerID:   localID,
			Platform:   "Steam",
			PlayerName: "LocalHero",
			TeamNum:    0,
			IsLocal:    true,
			Won:        &localWon,
			Score:      600,
			Goals:      2,
		},
		{
			MatchGUID:  guid,
			PlayerID:   peerID,
			Platform:   peerPlat,
			PlayerName: peerName,
			TeamNum:    peerTeam,
			IsLocal:    false,
			Won:        &peerWon,
			Score:      550,
			Goals:      2,
			StatsJSON:  statsJSON,
		},
	}); err != nil {
		t.Fatalf("SaveMatchPlayers(%s): %v", guid, err)
	}

	if err := store.RecordMatchResults(ctx, guid, playlist, []PlayerOutcome{
		{
			PlayerID:   peerID,
			Platform:   peerPlat,
			PlayerName: peerName,
			IsTeammate: isTeammate,
			Won:        localWon,
		},
	}, time.Unix(ts, 0).UTC()); err != nil {
		t.Fatalf("RecordMatchResults(%s): %v", guid, err)
	}
}

func TestGetScopedPlayerSpotlights_SQLiteAndJSON(t *testing.T) {
	ctx := context.Background()
	tEarly := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC).Unix()
	tLate := time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC).Unix()

	for backendName, store := range openScopedTestBackends(t) {
		t.Run(backendName, func(t *testing.T) {
			localID := "Steam|76561198000009999|0"
			ally2v2 := "Steam|76561198000001111|0"
			ally3v3 := "Steam|76561198000002222|0"
			rival2v2 := "Epic|rival_2v2_acc|0"
			liveOnly1v1 := "Epic|live_only_1v1|0"

			// Seed local account in players + player_matchups as if accidentally present; must be excluded!
			if err := store.SaveAuthState(ctx, "steam", "tok", localID, "LocalHero"); err != nil {
				t.Fatal(err)
			}
			if err := store.UpsertPlayer(ctx, &PlayerRecord{
				PlayerID: localID, Platform: "Steam", PlayerName: "LocalHero",
				FirstSeenAt: time.Unix(tEarly, 0).UTC(), LastSeenAt: time.Unix(tLate, 0).UTC(),
			}); err != nil {
				t.Fatal(err)
			}

			for _, p := range []*PlayerRecord{
				{PlayerID: ally2v2, Platform: "Steam", PlayerName: "Ally2v2", FirstSeenAt: time.Unix(tEarly, 0).UTC(), LastSeenAt: time.Unix(tLate, 0).UTC()},
				{PlayerID: ally3v3, Platform: "Steam", PlayerName: "Ally3v3", FirstSeenAt: time.Unix(tLate, 0).UTC(), LastSeenAt: time.Unix(tLate, 0).UTC()},
				{PlayerID: rival2v2, Platform: "Epic", PlayerName: "Rival2v2", FirstSeenAt: time.Unix(tEarly, 0).UTC(), LastSeenAt: time.Unix(tLate, 0).UTC()},
				{PlayerID: liveOnly1v1, Platform: "Epic", PlayerName: "LiveOnly1v1", FirstSeenAt: time.Unix(tEarly, 0).UTC(), LastSeenAt: time.Unix(tEarly, 0).UTC()},
			} {
				if err := store.UpsertPlayer(ctx, p); err != nil {
					t.Fatal(err)
				}
			}

			// Ally2v2: 3-0 together in Playlist 11 at tEarly (BPM=410), 0-3 together in Playlist 13 at tLate.
			for i := 0; i < 3; i++ {
				seedMatchWithLocalAndPeer(t, ctx, store, fmt.Sprintf("m-a2-11-%d", i), 11, tEarly+int64(i*60), localID, ally2v2, "Steam", "Ally2v2", true, true, 410)
				seedMatchWithLocalAndPeer(t, ctx, store, fmt.Sprintf("m-a2-13-%d", i), 13, tLate+int64(i*60), localID, ally2v2, "Steam", "Ally2v2", true, false, 300)
			}

			// Ally3v3: 4-0 together in Playlist 13 at tLate.
			for i := 0; i < 4; i++ {
				seedMatchWithLocalAndPeer(t, ctx, store, fmt.Sprintf("m-a3-13-%d", i), 13, tLate+int64(i*60), localID, ally3v3, "Steam", "Ally3v3", true, true, 450)
			}

			// Rival2v2: 0-3 against (local lost all 3) in Playlist 11 at tEarly; 3-0 against in Playlist 13 at tLate.
			for i := 0; i < 3; i++ {
				seedMatchWithLocalAndPeer(t, ctx, store, fmt.Sprintf("m-r2-11-%d", i), 11, tEarly+int64(i*60), localID, rival2v2, "Epic", "Rival2v2", false, false, 390)
				seedMatchWithLocalAndPeer(t, ctx, store, fmt.Sprintf("m-r2-13-%d", i), 13, tLate+int64(i*60), localID, rival2v2, "Epic", "Rival2v2", false, true, 390)
			}

			// LiveOnly1v1: 4-0 as opponent (local lost all 4) ONLY in player_matchups for Playlist 10 (no match_players rows).
			for i := 0; i < 4; i++ {
				if err := store.RecordMatchResults(ctx, fmt.Sprintf("live-10-%d", i), 10, []PlayerOutcome{
					{PlayerID: liveOnly1v1, Platform: "Epic", PlayerName: "LiveOnly1v1", IsTeammate: false, Won: false},
				}, time.Unix(tEarly+int64(i*30), 0).UTC()); err != nil {
					t.Fatal(err)
				}
			}

			// 1. Scoped to Playlist 11 (Ranked 2v2): Ally2v2 is 3-0 (100%) BestTeammate, Rival2v2 is 0-3 ToughestRival.
			sp11, err := store.GetScopedPlayerSpotlights(ctx, "ranked_2v2", 0, 0)
			if err != nil {
				t.Fatalf("GetScopedPlayerSpotlights(ranked_2v2) failed: %v", err)
			}
			if sp11.BestTeammate == nil || sp11.BestTeammate.Player == nil || sp11.BestTeammate.Player.PlayerID != ally2v2 {
				t.Fatalf("ranked_2v2 BestTeammate = %+v, want %s", sp11.BestTeammate, ally2v2)
			}
			if sp11.BestTeammate.Sub != "3-0 TOGETHER" {
				t.Fatalf("ranked_2v2 BestTeammate.Sub = %q, want 3-0 TOGETHER", sp11.BestTeammate.Sub)
			}
			if sp11.BestTeammate.Player.WithYouStats == nil || sp11.BestTeammate.Player.WithYouStats.AvgBPM != 410 {
				t.Fatalf("ranked_2v2 BestTeammate WithYouStats = %+v, want AvgBPM=410", sp11.BestTeammate.Player.WithYouStats)
			}
			if sp11.ToughestRival == nil || sp11.ToughestRival.Player == nil || sp11.ToughestRival.Player.PlayerID != rival2v2 {
				t.Fatalf("ranked_2v2 ToughestRival = %+v, want %s", sp11.ToughestRival, rival2v2)
			}
			if sp11.ToughestRival.Value != "0-3" || sp11.ToughestRival.Sub != "NEVER BEATEN" {
				t.Fatalf("ranked_2v2 ToughestRival = (%q, %q), want (0-3, NEVER BEATEN)", sp11.ToughestRival.Value, sp11.ToughestRival.Sub)
			}

			// 2. Scoped to Playlist 13 (Ranked 3v3): Ally3v3 is 4-0 BestTeammate (Ally2v2 is 0-3 in 3v3).
			sp13, err := store.GetScopedPlayerSpotlights(ctx, "13", 0, 0)
			if err != nil {
				t.Fatalf("GetScopedPlayerSpotlights(13) failed: %v", err)
			}
			if sp13.BestTeammate == nil || sp13.BestTeammate.Player == nil || sp13.BestTeammate.Player.PlayerID != ally3v3 {
				t.Fatalf("playlist 13 BestTeammate = %+v, want %s", sp13.BestTeammate, ally3v3)
			}

			// 3. Date-scoped to tEarly window [tEarly-60, tEarly+3600]: excludes tLate and excludes player_matchups-only fallback!
			spEarly, err := store.GetScopedPlayerSpotlights(ctx, "", tEarly-60, tEarly+3600)
			if err != nil {
				t.Fatalf("GetScopedPlayerSpotlights(early window) failed: %v", err)
			}
			if spEarly.BestTeammate == nil || spEarly.BestTeammate.Player == nil || spEarly.BestTeammate.Player.PlayerID != ally2v2 {
				t.Fatalf("early window BestTeammate = %+v, want %s", spEarly.BestTeammate, ally2v2)
			}
			if spEarly.ToughestRival == nil || spEarly.ToughestRival.Player == nil || spEarly.ToughestRival.Player.PlayerID != rival2v2 {
				t.Fatalf("early window ToughestRival = %+v, want %s (liveOnly1v1 has no match_players rows)", spEarly.ToughestRival, rival2v2)
			}

			// 4. All-time Playlist 10 (no date filter): falls back to player_matchups for liveOnly1v1!
			sp10, err := store.GetScopedPlayerSpotlights(ctx, "ranked_1v1", 0, 0)
			if err != nil {
				t.Fatalf("GetScopedPlayerSpotlights(ranked_1v1) failed: %v", err)
			}
			if sp10.ToughestRival == nil || sp10.ToughestRival.Player == nil || sp10.ToughestRival.Player.PlayerID != liveOnly1v1 {
				t.Fatalf("ranked_1v1 ToughestRival = %+v, want %s", sp10.ToughestRival, liveOnly1v1)
			}

			// 5. Verify local account never appears in global spotlights.
			spGlobal, err := store.GetPlayerSpotlights(ctx)
			if err != nil {
				t.Fatalf("GetPlayerSpotlights failed: %v", err)
			}
			for _, s := range []*SpotlightSummary{spGlobal.BestTeammate, spGlobal.MostFaced, spGlobal.ToughestRival} {
				if s != nil && s.Player != nil && s.Player.PlayerID == localID {
					t.Fatalf("local player %s leaked into spotlights: %+v", localID, s)
				}
			}
		})
	}
}

func TestGetScopedPlayerMatchups_SQLiteAndJSON(t *testing.T) {
	ctx := context.Background()
	tEarly := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC).Unix()
	tLate := time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC).Unix()

	for backendName, store := range openScopedTestBackends(t) {
		t.Run(backendName, func(t *testing.T) {
			localID := "Steam|76561198000009999|0"
			targetID := "Steam|76561198000005555|0"

			if err := store.UpsertPlayer(ctx, &PlayerRecord{
				PlayerID: targetID, Platform: "Steam", PlayerName: "TargetPeer",
				FirstSeenAt: time.Unix(tEarly, 0).UTC(), LastSeenAt: time.Unix(tLate, 0).UTC(),
			}); err != nil {
				t.Fatal(err)
			}

			// Early window: 2 wins together in Playlist 11.
			seedMatchWithLocalAndPeer(t, ctx, store, "sm-11-1", 11, tEarly+10, localID, targetID, "Steam", "TargetPeer", true, true, 400)
			seedMatchWithLocalAndPeer(t, ctx, store, "sm-11-2", 11, tEarly+20, localID, targetID, "Steam", "TargetPeer", true, true, 400)

			// Late window: 1 loss together in Playlist 11 + 3 wins against in Playlist 13.
			seedMatchWithLocalAndPeer(t, ctx, store, "sm-11-3", 11, tLate+10, localID, targetID, "Steam", "TargetPeer", true, false, 400)
			for i := 0; i < 3; i++ {
				seedMatchWithLocalAndPeer(t, ctx, store, fmt.Sprintf("sm-13-%d", i), 13, tLate+int64(100+i*10), localID, targetID, "Steam", "TargetPeer", false, true, 400)
			}

			// Live-only encounters in Playlist 27 (Hoops) present ONLY in player_matchups.
			if err := store.RecordMatchResults(ctx, "live-hoops-1", 27, []PlayerOutcome{
				{PlayerID: targetID, Platform: "Steam", PlayerName: "TargetPeer", IsTeammate: true, Won: true},
			}, time.Unix(tEarly+50, 0).UTC()); err != nil {
				t.Fatal(err)
			}

			// 1. Scoped to Late window [tLate, tLate+1000]:
			// Should have Playlist 13 (3 matches: 0-0 tm, 3-0 opp) and Playlist 11 (1 match: 0-1 tm), NO Playlist 27!
			scopedLate, err := store.GetScopedPlayerMatchups(ctx, targetID, tLate, tLate+1000)
			if err != nil {
				t.Fatalf("GetScopedPlayerMatchups(late) failed: %v", err)
			}
			if len(scopedLate) != 2 {
				t.Fatalf("len(scopedLate) = %d, want 2: %+v", len(scopedLate), scopedLate)
			}
			byPID := make(map[int]*PlayerMatchup)
			for _, m := range scopedLate {
				byPID[m.PlaylistID] = m
			}
			if m11 := byPID[11]; m11 == nil || m11.TotalMatches != 1 || m11.WinsAsTeammate != 0 || m11.LossesAsTeammate != 1 {
				t.Fatalf("late playlist 11 = %+v, want 0-1 tm (1 total)", m11)
			}
			if m13 := byPID[13]; m13 == nil || m13.TotalMatches != 3 || m13.WinsAsOpponent != 3 || m13.LossesAsOpponent != 0 {
				t.Fatalf("late playlist 13 = %+v, want 3-0 opp (3 total)", m13)
			}

			// 2. All Time (from=0, to=0): includes Playlist 11 (3 matches), Playlist 13 (3 matches), AND live-only Playlist 27 (1 match)!
			allTime, err := store.GetScopedPlayerMatchups(ctx, targetID, 0, 0)
			if err != nil {
				t.Fatalf("GetScopedPlayerMatchups(allTime) failed: %v", err)
			}
			if len(allTime) != 3 {
				t.Fatalf("len(allTime) = %d, want 3: %+v", len(allTime), allTime)
			}
			allByPID := make(map[int]*PlayerMatchup)
			for _, m := range allTime {
				allByPID[m.PlaylistID] = m
			}
			if m27 := allByPID[27]; m27 == nil || m27.TotalMatches != 1 || m27.WinsAsTeammate != 1 {
				t.Fatalf("allTime playlist 27 = %+v, want 1 live win", m27)
			}
			if m11 := allByPID[11]; m11 == nil || m11.TotalMatches != 3 || m11.WinsAsTeammate != 2 || m11.LossesAsTeammate != 1 {
				t.Fatalf("allTime playlist 11 = %+v, want 2-1 tm (3 total)", m11)
			}
		})
	}
}

func TestMatchesRoleFilter_DefaultThresholdsAndLegacyZeroBasedMMRHistory(t *testing.T) {
	twoGames := &PlayerSummary{PlayerID: "p1", TotalMatches: 2, TotalWinsAsTeammate: 1, TotalLossesAsTeammate: 1}
	oneGame := &PlayerSummary{PlayerID: "p2", TotalMatches: 1, TotalWinsAsTeammate: 1, TotalLossesAsTeammate: 0}
	coldMate := &PlayerSummary{PlayerID: "p3", TotalMatches: 3, TotalWinsAsTeammate: 1, TotalLossesAsTeammate: 2}

	if !MatchesRoleFilter(twoGames, PlayerDirectoryFilter{Role: "frequent"}) {
		t.Fatal("expected frequent default threshold >= 2 to include 2-game player")
	}
	if MatchesRoleFilter(oneGame, PlayerDirectoryFilter{Role: "frequent"}) {
		t.Fatal("expected frequent default threshold >= 2 to exclude 1-game player")
	}
	if !MatchesRoleFilter(twoGames, PlayerDirectoryFilter{Role: "synergy"}) {
		t.Fatal("expected synergy default threshold >= 1 game @ 50% WR to include 1-1 (50%) teammate")
	}
	if !MatchesRoleFilter(oneGame, PlayerDirectoryFilter{Role: "synergy"}) {
		t.Fatal("expected synergy default threshold >= 1 game @ 50% WR to include 1-0 (100%) teammate")
	}
	if MatchesRoleFilter(coldMate, PlayerDirectoryFilter{Role: "synergy"}) {
		t.Fatal("expected synergy default threshold >= 50% WR to exclude 1-2 (33.3%) teammate")
	}

	// Legacy mmr_history row with division=0 on Tier 10 (Platinum II) repairs to DIV I on enrichment after promotion
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "legacy_div.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	localID := "Steam|76561198000008888|0"
	winnerTeam := 0
	won := true
	_ = store.UpsertPlayer(ctx, &PlayerRecord{
		PlayerID:    localID,
		Platform:    "Steam",
		PlayerName:  "Me",
		RanksJSON:   `{"11":{"playlist_id":11,"tier":12,"division":2,"rank_name":"Platinum III Division III","mmr":980}}`,
		FirstSeenAt: now,
		LastSeenAt:  now,
	})
	_ = store.SyncReplayStats(ctx, &ReplayMatchStats{
		MatchGUID: "legacy-div0", MapName: "DFH Stadium", Playlist: 11,
		BlueScore: 2, OrangeScore: 1, WinnerTeam: &winnerTeam, Duration: 300,
		Date: now.Format(time.RFC3339),
	})
	_ = store.SaveMatchPlayers(ctx, "legacy-div0", []*MatchPlayerRecord{
		{MatchGUID: "legacy-div0", PlayerID: localID, Platform: "Steam", PlayerName: "Me", TeamNum: 0, IsLocal: true, Won: &won, Score: 400, Goals: 2},
	})
	_ = store.RecordMMR(ctx, &MMRHistoryRecord{
		MatchGUID: "legacy-div0", PlaylistID: 11, MMR: 900, Tier: 11, Division: 0, RecordedAt: now,
	})

	matches, _, err := store.ListHistoricalMatches(ctx, MatchFilter{Limit: 10})
	if err != nil || len(matches) != 1 {
		t.Fatalf("ListHistoricalMatches = (%d, %v), want 1", len(matches), err)
	}
	if got := matches[0].Stats.Players[0].ShortRank; got != "PLAT II · DIV I" {
		t.Fatalf("legacy division=0 ShortRank = %q, want %q", got, "PLAT II · DIV I")
	}
}

