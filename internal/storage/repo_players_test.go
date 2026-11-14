package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRepoPlayers_3744274LocalIDsAndProfile(t *testing.T) {
	ctx := context.Background()
	store := openTestSQLite(t)

	// Seed primary auth + background polling auth (3744274: polling MUST be excluded!).
	if err := store.SaveAuthState(ctx, "epic", "r1", "Epic|main_acc|0", "MainDriver"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAuthState(ctx, "epic_polling", "r2", "Epic|polling_only_acc|0", "PollingGhost"); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	wtBlue := 0
	for i, guid := range []string{"loc-m1", "loc-m2", "loc-m3"} {
		ts := now.Add(time.Duration(i) * time.Minute)
		_ = store.SyncReplayStats(ctx, &ReplayMatchStats{
			MatchGUID: guid, Playlist: 11, BlueScore: 3, OrangeScore: 1, WinnerTeam: &wtBlue, Date: ts.Format(time.RFC3339),
		})
	}
	// Steam|high|0 appears in 2 local matches, Epic|low|0 in 1 local match -> Steam|high|0 must come FIRST!
	_ = store.SaveMatchPlayers(ctx, "loc-m1", []*MatchPlayerRecord{{MatchGUID: "loc-m1", PlayerID: "Steam|high|0", TeamNum: 0, IsLocal: true, Score: 600}})
	_ = store.SaveMatchPlayers(ctx, "loc-m2", []*MatchPlayerRecord{{MatchGUID: "loc-m2", PlayerID: "Steam|high|0", TeamNum: 0, IsLocal: true, Score: 550}})
	_ = store.SaveMatchPlayers(ctx, "loc-m3", []*MatchPlayerRecord{{MatchGUID: "loc-m3", PlayerID: "Epic|low|0", TeamNum: 0, IsLocal: true, Score: 400}})
	_ = store.RecordMMR(ctx, &MMRHistoryRecord{RecordedAt: now, MatchGUID: "loc-m1", PlaylistID: 11, MMR: 1480, Tier: 17, Division: 2})

	ids, err := store.GetLocalPlayerIDs(ctx)
	if err != nil {
		t.Fatalf("GetLocalPlayerIDs failed: %v", err)
	}
	if len(ids) < 2 || ids[0] != "Steam|high|0" || ids[1] != "Epic|low|0" {
		t.Fatalf("GetLocalPlayerIDs order = %v, want [Steam|high|0, Epic|low|0, ...]", ids)
	}
	for _, id := range ids {
		if id == "Epic|polling_only_acc|0" {
			t.Fatalf("3744274 regression: *_polling provider account %q leaked into GetLocalPlayerIDs!", id)
		}
	}

	// GetLocalPlayerProfile resolves "me" alias and synthesizes playlist matchups + MMR ranks.
	prof, matchups, err := store.GetLocalPlayerProfile(ctx, "me")
	if err != nil {
		t.Fatalf("GetLocalPlayerProfile(me) failed: %v", err)
	}
	if prof.PlayerID != "Steam|high|0" || len(matchups) != 1 || matchups[0].WinsAsTeammate != 3 {
		t.Fatalf("local profile = %+v, matchups = %+v", prof, matchups)
	}
	tier, _, mmr := ExtractPrimaryRankFromJSON(prof.RanksJSON, []int{11})
	if tier != 17 || mmr != 1480 {
		t.Fatalf("local profile ranks = (tier=%d, mmr=%.0f), want (17, 1480)", tier, mmr)
	}
	if _, _, err := store.GetLocalPlayerProfile(ctx, "nonexistent-outsider"); !errors.Is(err, ErrPlayerNotFound) {
		t.Fatalf("expected ErrPlayerNotFound for non-local ID, got %v", err)
	}
}

func TestRepoPlayers_CRUDMatchupsNotesMMRAndPlayerMatches(t *testing.T) {
	ctx := context.Background()
	store := openTestSQLite(t)
	_ = store.SaveAuthState(ctx, "steam", "rt", "Steam|local_1|0", "Me")

	t1 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	if err := store.UpsertPlayer(ctx, &PlayerRecord{
		PlayerID: "Steam|76561198000|0", Platform: "Steam", PlayerName: "Buddy",
		RanksJSON: `{"11":{"playlist_id":11,"tier":16,"division":3,"mmr":1350}}`,
		Notes:     "good passer", Tags: []string{"duo", "chill"}, FirstSeenAt: t1, LastSeenAt: t1,
	}); err != nil {
		t.Fatal(err)
	}

	// Raw online ID & display name canonical resolution.
	got, err := store.GetPlayer(ctx, "76561198000")
	if err != nil || got.PlayerID != "Steam|76561198000|0" {
		t.Fatalf("GetPlayer by raw OnlineID = (%+v, %v)", got, err)
	}
	if gotByName, err := store.GetPlayer(ctx, "buddy"); err != nil || gotByName.PlayerID != "Steam|76561198000|0" {
		t.Fatalf("GetPlayer by name = (%+v, %v)", gotByName, err)
	}
	if err := store.UpdatePlayerNotesAndTags(ctx, "76561198000", "updated note", []string{"com", "fast"}); err != nil {
		t.Fatal(err)
	}
	got, _ = store.GetPlayer(ctx, "Steam|76561198000|0")
	if got.Notes != "updated note" || len(got.Tags) != 2 {
		t.Fatalf("notes/tags mismatch: %+v", got)
	}

	// RecordMatchResults 4-way matrix + local exclusion + idempotency.
	outcomes := []PlayerOutcome{
		{PlayerID: "Steam|local_1|0", Platform: "Steam", PlayerName: "Me", IsTeammate: true, Won: true},
		{PlayerID: "Steam|76561198000|0", Platform: "Steam", PlayerName: "Buddy", IsTeammate: true, Won: true},
		{PlayerID: "Epic|foe_1|0", Platform: "Epic", PlayerName: "Foe", IsTeammate: false, Won: true},
	}
	if err := store.RecordMatchResults(ctx, "pm-1", 11, outcomes, t1); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordMatchResults(ctx, "pm-1", 11, outcomes, t1); !errors.Is(err, ErrMatchAlreadyProcessed) {
		t.Fatalf("duplicate RecordMatchResults = %v, want ErrMatchAlreadyProcessed", err)
	}

	muBuddy, err := store.GetPlayerMatchup(ctx, "76561198000", 11)
	if err != nil || muBuddy.WinsAsTeammate != 1 || muBuddy.TotalMatches != 1 {
		t.Fatalf("Buddy matchup = %+v, err=%v", muBuddy, err)
	}
	muFoe, err := store.GetPlayerMatchup(ctx, "Epic|foe_1|0", 11)
	if err != nil || muFoe.WinsAsOpponent != 1 || muFoe.TotalMatches != 1 {
		t.Fatalf("Foe matchup = %+v, err=%v", muFoe, err)
	}

	// ListPlayerMatches with batched search + role + perspective filtering.
	wt0, wt1 := 0, 1
	_ = store.SyncReplayStats(ctx, &ReplayMatchStats{
		MatchGUID: "pm-1", MapName: "Champion's Field", Playlist: 11, BlueScore: 3, OrangeScore: 1, WinnerTeam: &wt0, Date: t1.Format(time.RFC3339),
	})
	_ = store.SyncReplayStats(ctx, &ReplayMatchStats{
		MatchGUID: "pm-2", MapName: "Salty Shores", Playlist: 13, BlueScore: 2, OrangeScore: 5, WinnerTeam: &wt1, Date: t1.Add(time.Hour).Format(time.RFC3339),
	})
	_ = store.SaveMatchPlayers(ctx, "pm-1", []*MatchPlayerRecord{
		{MatchGUID: "pm-1", PlayerID: "Steam|local_1|0", TeamNum: 0, IsLocal: true, Score: 500},
		{MatchGUID: "pm-1", PlayerID: "Epic|foe_1|0", TeamNum: 1, IsLocal: false, Score: 350, CarName: "Takumi"},
	})
	_ = store.SaveMatchPlayers(ctx, "pm-2", []*MatchPlayerRecord{
		{MatchGUID: "pm-2", PlayerID: "Steam|local_1|0", TeamNum: 0, IsLocal: true, Score: 400},
		{MatchGUID: "pm-2", PlayerID: "Epic|foe_1|0", TeamNum: 0, IsLocal: false, Score: 650, CarName: "Octane"},
	})

	oppDetails, total, err := store.ListPlayerMatches(ctx, "Epic|foe_1|0", PlayerMatchFilter{Role: "opponent", Search: "takumi"})
	if err != nil || total != 1 || len(oppDetails) != 1 || oppDetails[0].MatchGUID != "pm-1" {
		t.Fatalf("ListPlayerMatches opponent+search = (%v, %d, %v)", oppDetails, total, err)
	}
	if oppDetails[0].Result != "defeat" || oppDetails[0].LocalResult != "victory" {
		t.Fatalf("unexpected perspective results: Result=%s LocalResult=%s", oppDetails[0].Result, oppDetails[0].LocalResult)
	}
	allPersp, _, _ := store.ListPlayerMatches(ctx, "Epic|foe_1|0", PlayerMatchFilter{Result: "W", LocalPerspective: true})
	if len(allPersp) != 1 || allPersp[0].MatchGUID != "pm-1" {
		t.Fatalf("local_perspective win filter = %v, want [pm-1]", allPersp)
	}

	// MMR history recording + filtering.
	_ = store.RecordMMR(ctx, &MMRHistoryRecord{RecordedAt: t1, MatchGUID: "pm-1", PlaylistID: 11, MMR: 1410, MMRChange: 9, Tier: 16, Division: 4})
	_ = store.RecordMMR(ctx, &MMRHistoryRecord{RecordedAt: t1.Add(time.Minute), MatchGUID: "pm-2", PlaylistID: 13, MMR: 1520, MMRChange: 10, Tier: 18, Division: 1})
	mmrList, err := store.ListMMRHistory(ctx, 11, 10)
	if err != nil || len(mmrList) != 1 || mmrList[0].MMR != 1410 {
		t.Fatalf("ListMMRHistory(11) = %+v, err=%v", mmrList, err)
	}
}
