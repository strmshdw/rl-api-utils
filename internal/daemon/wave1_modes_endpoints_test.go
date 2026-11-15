package daemon_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/storage"
)

func seedDaemonMatch(
	t *testing.T,
	ctx context.Context,
	store storage.StateStore,
	guid string,
	playlist int,
	ts int64,
	localID, peerID, peerPlat, peerName string,
	isTeammate, localWon bool,
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
	peerWon := peerTeam == winnerTeam

	if err := store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
		MatchGUID:   guid,
		MapName:     "Mannfield",
		Playlist:    playlist,
		BlueScore:   blueScore,
		OrangeScore: orangeScore,
		WinnerTeam:  &winnerTeam,
		Duration:    300,
		Date:        time.Unix(ts, 0).UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("SyncReplayStats(%s): %v", guid, err)
	}

	if err := store.SaveMatchPlayers(ctx, guid, []*storage.MatchPlayerRecord{
		{
			MatchGUID: guid, PlayerID: localID, Platform: "Steam", PlayerName: "Me",
			TeamNum: 0, IsLocal: true, Won: &localWon, Score: 500, Goals: 2,
		},
		{
			MatchGUID: guid, PlayerID: peerID, Platform: peerPlat, PlayerName: peerName,
			TeamNum: peerTeam, IsLocal: false, Won: &peerWon, Score: 450, Goals: 1,
		},
	}); err != nil {
		t.Fatalf("SaveMatchPlayers(%s): %v", guid, err)
	}

	if err := store.RecordMatchResults(ctx, guid, playlist, []storage.PlayerOutcome{
		{PlayerID: peerID, Platform: peerPlat, PlayerName: peerName, IsTeammate: isTeammate, Won: localWon},
	}, time.Unix(ts, 0).UTC()); err != nil {
		t.Fatalf("RecordMatchResults(%s): %v", guid, err)
	}
}

func TestHandleListPlayers_ScopedSpotlightsByPlaylistAndDate(t *testing.T) {
	ctx := context.Background()
	tJan := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC).Unix()
	tFeb := time.Date(2026, 2, 15, 12, 0, 0, 0, time.UTC).Unix()

	for _, backend := range allStorageBackends {
		t.Run(string(backend), func(t *testing.T) {
			store := newTestStore(t, backend)
			cfg := makeTestConfig(time.Minute, false)
			localID := "Steam|76561198000009999|0"
			cfg.PlayerTracking.LocalPlayerID = localID

			janAlly := "Steam|76561198000001001|0"
			febAlly := "Steam|76561198000002002|0"

			_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{PlayerID: janAlly, Platform: "Steam", PlayerName: "JanAlly2v2", FirstSeenAt: time.Unix(tJan, 0).UTC(), LastSeenAt: time.Unix(tJan, 0).UTC()})
			_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{PlayerID: febAlly, Platform: "Steam", PlayerName: "FebAlly3v3", FirstSeenAt: time.Unix(tFeb, 0).UTC(), LastSeenAt: time.Unix(tFeb, 0).UTC()})

			// JanAlly: 3-0 in Playlist 11 in Jan.
			for i := 0; i < 3; i++ {
				seedDaemonMatch(t, ctx, store, fmt.Sprintf("jan-11-%d", i), 11, tJan+int64(i*60), localID, janAlly, "Steam", "JanAlly2v2", true, true)
			}
			// FebAlly: 5-0 in Playlist 13 in Feb (dominates global unscoped BestTeammate by game count).
			for i := 0; i < 5; i++ {
				seedDaemonMatch(t, ctx, store, fmt.Sprintf("feb-13-%d", i), 13, tFeb+int64(i*60), localID, febAlly, "Steam", "FebAlly3v3", true, true)
			}

			d, err := daemon.New(newMockSyncer(), cfg, daemon.WithStateStore(store))
			if err != nil {
				t.Fatalf("daemon.New failed: %v", err)
			}

			rec := executeTestRequest(d, http.MethodGet, "/api/players?playlist=11&from=2026-01-01&to=2026-01-31", nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}

			var resp daemon.PlayerSearchResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}
			if resp.Spotlights == nil || resp.Spotlights.BestTeammate == nil || resp.Spotlights.BestTeammate.Player == nil {
				t.Fatalf("expected non-nil BestTeammate spotlight, got %+v", resp.Spotlights)
			}
			if resp.Spotlights.BestTeammate.Player.PlayerID != janAlly {
				t.Fatalf("BestTeammate = %s, want %s (scoped to playlist=11 & Jan)", resp.Spotlights.BestTeammate.Player.PlayerID, janAlly)
			}
		})
	}
}

func TestHandleGetPlayer_DateScopedPlaylistBreakdownAndFilteredMatchups(t *testing.T) {
	ctx := context.Background()
	tJan := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC).Unix()
	tFeb := time.Date(2026, 2, 15, 12, 0, 0, 0, time.UTC).Unix()

	for _, backend := range allStorageBackends {
		t.Run(string(backend), func(t *testing.T) {
			store := newTestStore(t, backend)
			cfg := makeTestConfig(time.Minute, false)
			localID := "Steam|76561198000009999|0"
			cfg.PlayerTracking.LocalPlayerID = localID
			peerID := "Steam|76561198000003333|0"

			_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
				PlayerID: peerID, Platform: "Steam", PlayerName: "DossierPeer",
				FirstSeenAt: time.Unix(tJan, 0).UTC(), LastSeenAt: time.Unix(tFeb, 0).UTC(),
			})

			// Jan: 2 matches in Playlist 11 (out of Feb window).
			for i := 0; i < 2; i++ {
				seedDaemonMatch(t, ctx, store, fmt.Sprintf("dos-jan-11-%d", i), 11, tJan+int64(i*60), localID, peerID, "Steam", "DossierPeer", true, true)
			}
			// Feb: 2 matches in Playlist 11 (1-1) AND 4 matches in Playlist 13 (3-1).
			seedDaemonMatch(t, ctx, store, "dos-feb-11-0", 11, tFeb+10, localID, peerID, "Steam", "DossierPeer", true, true)
			seedDaemonMatch(t, ctx, store, "dos-feb-11-1", 11, tFeb+20, localID, peerID, "Steam", "DossierPeer", true, false)
			for i := 0; i < 4; i++ {
				seedDaemonMatch(t, ctx, store, fmt.Sprintf("dos-feb-13-%d", i), 13, tFeb+int64(100+i*10), localID, peerID, "Steam", "DossierPeer", false, i < 3)
			}
			// Live-only match in Playlist 27 (Hoops) in player_matchups only.
			_ = store.RecordMatchResults(ctx, "dos-live-27", 27, []storage.PlayerOutcome{
				{PlayerID: peerID, Platform: "Steam", PlayerName: "DossierPeer", IsTeammate: true, Won: true},
			}, time.Unix(tJan+500, 0).UTC())

			d, err := daemon.New(newMockSyncer(), cfg, daemon.WithStateStore(store))
			if err != nil {
				t.Fatalf("daemon.New failed: %v", err)
			}

			escapedID := url.PathEscape(peerID)
			// 1. Date-scoped + playlist-filtered: ?playlist=11&from=2026-02-01&to=2026-02-28
			rec := executeTestRequest(d, http.MethodGet, fmt.Sprintf("/api/players/%s?playlist=11&from=2026-02-01&to=2026-02-28", escapedID), nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}

			var detail daemon.PlayerDetailResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
				t.Fatalf("unmarshal detail: %v", err)
			}

			// EncountersCount & Matchups are filtered to Playlist 11 in Feb (2 matches: 1-1).
			if detail.EncountersCount != 2 || detail.TeammateWins != 1 || detail.TeammateLosses != 1 {
				t.Fatalf("scoped detail summary = (enc=%d, tm=%d-%d), want (2, 1-1)", detail.EncountersCount, detail.TeammateWins, detail.TeammateLosses)
			}
			if len(detail.Matchups) != 1 || detail.Matchups[0].PlaylistID != 11 || detail.Matchups[0].TotalMatches != 2 {
				t.Fatalf("scoped detail.Matchups = %+v, want [playlist 11 with 2 matches]", detail.Matchups)
			}

			// PlaylistBreakdown has ALL playlists in Feb (Playlist 13 with 4 matches first, Playlist 11 with 2 matches second), sorted by encounters DESC!
			if len(detail.PlaylistBreakdown) != 2 {
				t.Fatalf("len(detail.PlaylistBreakdown) = %d, want 2: %+v", len(detail.PlaylistBreakdown), detail.PlaylistBreakdown)
			}
			if detail.PlaylistBreakdown[0].PlaylistID != 13 || detail.PlaylistBreakdown[0].TotalMatches != 4 {
				t.Fatalf("PlaylistBreakdown[0] = %+v, want playlist 13 with 4 matches", detail.PlaylistBreakdown[0])
			}
			if detail.PlaylistBreakdown[1].PlaylistID != 11 || detail.PlaylistBreakdown[1].TotalMatches != 2 {
				t.Fatalf("PlaylistBreakdown[1] = %+v, want playlist 11 with 2 matches", detail.PlaylistBreakdown[1])
			}

			// 2. All-time (unscoped date): preserves live-only Playlist 27 in PlaylistBreakdown!
			recAll := executeTestRequest(d, http.MethodGet, fmt.Sprintf("/api/players/%s", escapedID), nil)
			if recAll.Code != http.StatusOK {
				t.Fatalf("all-time status = %d", recAll.Code)
			}
			var detailAll daemon.PlayerDetailResponse
			if err := json.Unmarshal(recAll.Body.Bytes(), &detailAll); err != nil {
				t.Fatalf("unmarshal all-time detail: %v", err)
			}
			if len(detailAll.PlaylistBreakdown) != 3 {
				t.Fatalf("all-time len(PlaylistBreakdown) = %d, want 3 (11, 13, 27): %+v", len(detailAll.PlaylistBreakdown), detailAll.PlaylistBreakdown)
			}
		})
	}
}
