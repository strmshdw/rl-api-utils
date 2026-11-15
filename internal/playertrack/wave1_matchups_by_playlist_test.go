package playertrack

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rlapi"
)

func TestLobbyPlayer_MatchupsByPlaylist_AndDeepClone(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "pt_modes.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer store.Close()

	localID := "Steam|76561198000000001|0"
	tmID := "Steam|76561198000000002|0"
	oppID := "Epic|opp_multi_mode|0"
	now := time.Date(2026, 4, 12, 15, 0, 0, 0, time.UTC)

	// Seed multi-playlist matchups across playlists 10 (1v1), 11 (2v2), and 13 (3v3).
	if err := store.RecordMatchResults(ctx, "g-10-1", 10, []storage.PlayerOutcome{
		{PlayerID: oppID, Platform: "Epic", PlayerName: "OppMulti", IsTeammate: false, Won: true},
	}, now.Add(-30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordMatchResults(ctx, "g-11-1", 11, []storage.PlayerOutcome{
		{PlayerID: tmID, Platform: "Steam", PlayerName: "BuddyMulti", IsTeammate: true, Won: true},
		{PlayerID: oppID, Platform: "Epic", PlayerName: "OppMulti", IsTeammate: false, Won: false},
	}, now.Add(-20*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordMatchResults(ctx, "g-13-1", 13, []storage.PlayerOutcome{
		{PlayerID: tmID, Platform: "Steam", PlayerName: "BuddyMulti", IsTeammate: true, Won: false},
		{PlayerID: oppID, Platform: "Epic", PlayerName: "OppMulti", IsTeammate: false, Won: true},
	}, now.Add(-10*time.Minute)); err != nil {
		t.Fatal(err)
	}

	tracker, err := NewTracker(
		store,
		NewNoOpRankClient(),
		config.PlayerTrackingConfig{Enabled: true, LocalPlayerID: localID},
		config.AuthConfig{},
	)
	if err != nil {
		t.Fatalf("NewTracker failed: %v", err)
	}
	defer tracker.Close()

	players := []statsapi.StatsPlayer{
		{PrimaryId: localID, Name: "Me", TeamNum: 0, Score: 400},
		{PrimaryId: tmID, Name: "BuddyMulti", TeamNum: 0, Score: 350},
		{PrimaryId: oppID, Name: "OppMulti", TeamNum: 1, Score: 500},
	}

	if err := tracker.OnUpdateGameState(ctx, "live-match-11", 11, players, &statsapi.StatsGame{TimeRemaining: 240}); err != nil {
		t.Fatalf("OnUpdateGameState failed: %v", err)
	}

	snap1 := tracker.GetCurrentMatch()
	if snap1.LocalPlayer == nil {
		t.Fatal("expected non-nil LocalPlayer")
	}
	if snap1.LocalPlayer.MatchupsByPlaylist != nil {
		t.Fatalf("expected LocalPlayer.MatchupsByPlaylist to be nil, got %+v", snap1.LocalPlayer.MatchupsByPlaylist)
	}

	if len(snap1.Teammates) != 1 || len(snap1.Opponents) != 1 {
		t.Fatalf("roster mismatch: tm=%d opp=%d", len(snap1.Teammates), len(snap1.Opponents))
	}

	tm := snap1.Teammates[0]
	if tm.MatchupRecord == nil || tm.MatchupRecord.PlaylistID != 11 || tm.MatchupRecord.WinsAsTeammate != 1 {
		t.Fatalf("tm MatchupRecord = %+v, want playlist 11 with 1 win", tm.MatchupRecord)
	}
	if len(tm.MatchupsByPlaylist) != 2 || tm.MatchupsByPlaylist["11"] == nil || tm.MatchupsByPlaylist["13"] == nil {
		t.Fatalf("tm MatchupsByPlaylist = %+v, want keys 11 and 13", tm.MatchupsByPlaylist)
	}
	if tm.MatchupsByPlaylist["13"].LossesAsTeammate != 1 {
		t.Fatalf("tm MatchupsByPlaylist[13] = %+v, want 1 loss as teammate", tm.MatchupsByPlaylist["13"])
	}

	opp := snap1.Opponents[0]
	if opp.MatchupRecord == nil || opp.MatchupRecord.PlaylistID != 11 || opp.MatchupRecord.LossesAsOpponent != 1 {
		t.Fatalf("opp MatchupRecord = %+v, want playlist 11 with 1 loss", opp.MatchupRecord)
	}
	if len(opp.MatchupsByPlaylist) != 3 || opp.MatchupsByPlaylist["10"] == nil || opp.MatchupsByPlaylist["11"] == nil || opp.MatchupsByPlaylist["13"] == nil {
		t.Fatalf("opp MatchupsByPlaylist = %+v, want keys 10, 11, 13", opp.MatchupsByPlaylist)
	}
	if opp.MatchupsByPlaylist["10"].WinsAsOpponent != 1 || opp.MatchupsByPlaylist["13"].WinsAsOpponent != 1 {
		t.Fatalf("opp MatchupsByPlaylist values = (%+v, %+v), want 1 win each", opp.MatchupsByPlaylist["10"], opp.MatchupsByPlaylist["13"])
	}

	// Verify DeepClone isolation: mutating snap1's map or struct pointers must NOT affect snap2 or subsequent frame updates!
	snap1.Opponents[0].MatchupsByPlaylist["13"].WinsAsOpponent = 99999
	snap1.Opponents[0].MatchupsByPlaylist["99"] = &storage.PlayerMatchup{PlaylistID: 99, TotalMatches: 42}

	snap2 := tracker.GetCurrentMatch()
	if snap2.Opponents[0].MatchupsByPlaylist["13"].WinsAsOpponent != 1 {
		t.Fatalf("DeepClone isolation broken: WinsAsOpponent mutated to %d", snap2.Opponents[0].MatchupsByPlaylist["13"].WinsAsOpponent)
	}
	if _, leaked := snap2.Opponents[0].MatchupsByPlaylist["99"]; leaked {
		t.Fatal("DeepClone isolation broken: key 99 leaked into tracker state")
	}

	// Also verify second OnUpdateGameState tick hits in-memory cache and preserves MatchupsByPlaylist.
	if err := tracker.OnUpdateGameState(ctx, "live-match-11", 11, players, &statsapi.StatsGame{TimeRemaining: 235}); err != nil {
		t.Fatalf("second OnUpdateGameState failed: %v", err)
	}
	snap3 := tracker.GetCurrentMatch()
	if len(snap3.Opponents[0].MatchupsByPlaylist) != 3 || snap3.Opponents[0].MatchupsByPlaylist["10"].WinsAsOpponent != 1 {
		t.Fatalf("cached tick MatchupsByPlaylist mismatch: %+v", snap3.Opponents[0].MatchupsByPlaylist)
	}
}

func TestPersistMMRSnapshot_ConvertsZeroBasedPsyNetDivisionToOneBased(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "pt_div.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer store.Close()

	localID := "Steam|76561198000007777|0"
	tracker, err := NewTracker(
		store,
		NewNoOpRankClient(),
		config.PlayerTrackingConfig{Enabled: true, LocalPlayerID: localID},
		config.AuthConfig{},
	)
	if err != nil {
		t.Fatalf("NewTracker failed: %v", err)
	}
	defer tracker.Close()
	now := time.Date(2026, 4, 1, 18, 0, 0, 0, time.UTC)

	// Match 1: PsyNet 0-based Division 0 == Platinum II Division I (Tier 11, 1-based division 1)
	skillDivI := rlapi.Skill{Playlist: 11, Tier: 11, Division: 0, MMR: 40.0, MatchesPlayed: 20}
	rankDivI := FromSkill(skillDivI)
	tracker.persistMMRSnapshot(ctx, "m-plat2-div1", 11, localID, 890, &rankDivI, []rlapi.Skill{skillDivI}, now, nil)

	// Match 2: PsyNet 0-based Division 1 == Platinum II Division II (Tier 11, 1-based division 2)
	skillDivII := rlapi.Skill{Playlist: 11, Tier: 11, Division: 1, MMR: 41.0, MatchesPlayed: 21}
	rankDivII := FromSkill(skillDivII)
	tracker.persistMMRSnapshot(ctx, "m-plat2-div2", 11, localID, 900, &rankDivII, []rlapi.Skill{skillDivII}, now.Add(10*time.Minute), nil)

	hist, err := store.ListMMRHistory(ctx, 11, 10)
	if err != nil || len(hist) != 2 {
		t.Fatalf("ListMMRHistory(11) = (%+v, %v), want 2 entries", hist, err)
	}
	// Newest first: m-plat2-div2 (Division 2), m-plat2-div1 (Division 1)
	if hist[0].Division != 2 {
		t.Fatalf("hist[0] (PsyNet Division=1 / Div II) stored Division = %d, want 2", hist[0].Division)
	}
	if hist[1].Division != 1 {
		t.Fatalf("hist[1] (PsyNet Division=0 / Div I) stored Division = %d, want 1", hist[1].Division)
	}

	// Simulate later promotion to Platinum III (Tier 12) in ranks_json and verify historical match enrichment preserves Div I & Div II!
	promotedSkill := rlapi.Skill{Playlist: 11, Tier: 12, Division: 2, MMR: 45.0, MatchesPlayed: 30}
	promotedJSON, _ := SerializeRanksJSON([]rlapi.Skill{promotedSkill})
	if err := store.UpsertPlayer(ctx, &storage.PlayerRecord{
		PlayerID: localID, Platform: "Steam", PlayerName: "Me", RanksJSON: promotedJSON,
		FirstSeenAt: now, LastSeenAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	winnerTeam := 0
	won := true
	for i, guid := range []string{"m-plat2-div1", "m-plat2-div2"} {
		ts := now.Add(time.Duration(i) * 10 * time.Minute)
		if err := store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
			MatchGUID: guid, MapName: "DFH Stadium", Playlist: 11,
			BlueScore: 3, OrangeScore: 1, WinnerTeam: &winnerTeam, Duration: 300,
			Date: ts.Format(time.RFC3339),
		}); err != nil {
			t.Fatal(err)
		}
		if err := store.SaveMatchPlayers(ctx, guid, []*storage.MatchPlayerRecord{
			{MatchGUID: guid, PlayerID: localID, Platform: "Steam", PlayerName: "Me", TeamNum: 0, IsLocal: true, Won: &won, Score: 450, Goals: 2},
		}); err != nil {
			t.Fatal(err)
		}
	}

	matches, _, err := store.ListHistoricalMatches(ctx, storage.MatchFilter{Limit: 10})
	if err != nil || len(matches) != 2 {
		t.Fatalf("ListHistoricalMatches = (%d, %v), want 2", len(matches), err)
	}
	// matches[0] is newest (m-plat2-div2), matches[1] is m-plat2-div1
	if got := matches[0].Stats.Players[0].ShortRank; got != "PLAT II · DIV II" {
		t.Fatalf("m-plat2-div2 enriched ShortRank = %q, want %q", got, "PLAT II · DIV II")
	}
	if got := matches[1].Stats.Players[0].ShortRank; got != "PLAT II · DIV I" {
		t.Fatalf("m-plat2-div1 enriched ShortRank = %q, want %q", got, "PLAT II · DIV I")
	}
}

