package daemon_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/storage"
)

func TestDaemon_HTTP_Phase2_PlayersMatchesSessionAndPrune(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, backendSQLite)
	mockFetcher := playertrack.NewMockSkillFetcher()
	tracker := newTestTracker(t, store, mockFetcher)

	localID := "Epic|my_epic_acc_001|0"
	tmID := "Steam|76561198000000101|0"
	oppID := "Epic|nemesis_opp_202|0"

	now := time.Now().UTC()
	day1 := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC).Unix()
	day2 := time.Date(2026, 3, 15, 18, 0, 0, 0, time.UTC).Unix()

	// Seed player profiles with canonical 23-tier ranks
	_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
		PlayerID:    localID,
		Platform:    "Epic",
		PlayerName:  "Me",
		RanksJSON:   `{"11":{"playlist_id":11,"tier":15,"division":3,"mmr":1045.0,"rank_name":"Diamond III Div IV","short_rank":"DIA III · DIV IV"}}`,
		FirstSeenAt: now,
		LastSeenAt:  now,
	})
	_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
		PlayerID:    tmID,
		Platform:    "Steam",
		PlayerName:  "PrimeMate",
		RanksJSON:   `{"11":{"playlist_id":11,"tier":16,"division":2,"mmr":1090.5,"rank_name":"Champion I Div II","short_rank":"CHAMP I · DIV II"}}`,
		FirstSeenAt: now,
		LastSeenAt:  now,
	})
	_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
		PlayerID:    oppID,
		Platform:    "Epic",
		PlayerName:  "FierceNemesis",
		RanksJSON:   `{"11":{"playlist_id":11,"tier":19,"division":0,"mmr":1435.0,"rank_name":"Grand Champion I Div I","short_rank":"GC I · DIV I"}}`,
		FirstSeenAt: now,
		LastSeenAt:  now,
	})

	win0 := 0
	win1 := 1
	wonTrue := true
	wonFalse := false

	// Match 1 (Day 1, 2v2, Local + PrimeMate WIN 3-1 vs FierceNemesis on DFH Stadium, synced)
	_ = store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{
		{
			MatchGUID:            "guid-2v2-win-1",
			RecordStartTimestamp: day1,
			MapName:              "DFH Stadium",
			Playlist:             11,
		},
		{
			MatchGUID:            "guid-2v2-loss-2",
			RecordStartTimestamp: day2,
			MapName:              "Mannfield (Night)",
			Playlist:             11,
		},
	})
	_ = store.MarkDownloaded(ctx, "guid-2v2-win-1", "C:/replays/guid-2v2-win-1.replay")
	_ = store.MarkUploaded(ctx, "guid-2v2-win-1", "bc-1", "https://ballchasing.com/replay/bc-1")
	_ = store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
		MatchGUID:      "guid-2v2-win-1",
		MapName:        "DFH Stadium",
		Playlist:       11,
		BlueScore:      3,
		OrangeScore:    1,
		WinnerTeam:     &win0,
		Duration:       300,
		BallchasingID:  "bc-1",
		BallchasingURL: "https://ballchasing.com/replay/bc-1",
		Players: []storage.ReplayPlayerStats{
			{Name: "Me", Platform: "Epic", OnlineID: "my_epic_acc_001", Team: 0, Score: 520, Goals: 2, Assists: 1, Saves: 2, Shots: 5, Demos: 1},
			{Name: "PrimeMate", Platform: "Steam", OnlineID: "76561198000000101", Team: 0, Score: 440, Goals: 1, Assists: 2, Saves: 1, Shots: 4, Demos: 2},
			{Name: "FierceNemesis", Platform: "Epic", OnlineID: "nemesis_opp_202", Team: 1, Score: 380, Goals: 1, Assists: 0, Saves: 3, Shots: 4, Demos: 0},
		},
	})
	_ = store.MarkStatsSynced(ctx, "guid-2v2-win-1")
	_ = store.SaveMatchPlayers(ctx, "guid-2v2-win-1", []*storage.MatchPlayerRecord{
		{MatchGUID: "guid-2v2-win-1", PlayerID: localID, TeamNum: 0, Score: 520, Goals: 2, Assists: 1, Saves: 2, Shots: 5, Demos: 1, Won: &wonTrue, IsLocal: true, StatsJSON: `{"boost":{"bpm":410,"avg_amount":48},"movement":{"percent_supersonic_speed":18.5,"percent_ground":55},"positioning":{"percent_behind_ball":68}}`},
		{MatchGUID: "guid-2v2-win-1", PlayerID: tmID, TeamNum: 0, Score: 440, Goals: 1, Assists: 2, Saves: 1, Shots: 4, Demos: 2, Won: &wonTrue, IsLocal: false, StatsJSON: `{"boost":{"bpm":390,"avg_amount":45},"movement":{"percent_supersonic_speed":16.0,"percent_ground":58},"positioning":{"percent_behind_ball":64}}`},
		{MatchGUID: "guid-2v2-win-1", PlayerID: oppID, TeamNum: 1, Score: 380, Goals: 1, Assists: 0, Saves: 3, Shots: 4, Demos: 0, Won: &wonFalse, IsLocal: false, StatsJSON: `{"boost":{"bpm":425,"avg_amount":50},"movement":{"percent_supersonic_speed":20.0,"percent_ground":52},"positioning":{"percent_behind_ball":60}}`},
	})
	_ = store.RecordMatchResults(ctx, "guid-2v2-win-1", 11, []storage.PlayerOutcome{
		{PlayerID: tmID, Platform: "Steam", PlayerName: "PrimeMate", IsTeammate: true, Won: true},
		{PlayerID: oppID, Platform: "Epic", PlayerName: "FierceNemesis", IsTeammate: false, Won: false},
	})

	// Match 2 (Day 2, 2v2, Local + PrimeMate LOSE 1-4 vs FierceNemesis on Mannfield, pending upload)
	_ = store.MarkDownloaded(ctx, "guid-2v2-loss-2", "C:/replays/guid-2v2-loss-2.replay")
	_ = store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
		MatchGUID:   "guid-2v2-loss-2",
		MapName:     "Mannfield (Night)",
		Playlist:    11,
		BlueScore:   1,
		OrangeScore: 4,
		WinnerTeam:  &win1,
		Duration:    300,
	})
	_ = store.SaveMatchPlayers(ctx, "guid-2v2-loss-2", []*storage.MatchPlayerRecord{
		{MatchGUID: "guid-2v2-loss-2", PlayerID: localID, TeamNum: 0, Score: 250, Goals: 1, Assists: 0, Saves: 1, Shots: 2, Demos: 0, Won: &wonFalse, IsLocal: true, StatsJSON: `{"boost":{"bpm":350,"avg_amount":40},"movement":{"percent_supersonic_speed":14.0}}`},
		{MatchGUID: "guid-2v2-loss-2", PlayerID: tmID, TeamNum: 0, Score: 210, Goals: 0, Assists: 1, Saves: 0, Shots: 2, Demos: 1, Won: &wonFalse, IsLocal: false, StatsJSON: `{"boost":{"bpm":330,"avg_amount":38},"movement":{"percent_supersonic_speed":12.0}}`},
		{MatchGUID: "guid-2v2-loss-2", PlayerID: oppID, TeamNum: 1, Score: 650, Goals: 3, Assists: 1, Saves: 2, Shots: 6, Demos: 2, Won: &wonTrue, IsLocal: false, StatsJSON: `{"boost":{"bpm":465,"avg_amount":54},"movement":{"percent_supersonic_speed":24.0}}`},
	})
	_ = store.RecordMatchResults(ctx, "guid-2v2-loss-2", 11, []storage.PlayerOutcome{
		{PlayerID: tmID, Platform: "Steam", PlayerName: "PrimeMate", IsTeammate: true, Won: false},
		{PlayerID: oppID, Platform: "Epic", PlayerName: "FierceNemesis", IsTeammate: false, Won: true},
	})

	cfg := makeTestConfig(5*time.Minute, false)
	cfg.Sync.ReplayLimit = 2
	d, err := daemon.New(newMockSyncer(), cfg,
		daemon.WithPlayerTracker(tracker),
		daemon.WithStateStore(store),
	)
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	// 1. Verify GET /api/players with playlist=2v2 & split stats
	recPlayers := executeTestRequest(d, http.MethodGet, "/api/players?playlist=2v2&sort_by=games&sort_dir=desc", nil)
	if recPlayers.Code != http.StatusOK {
		t.Fatalf("GET /api/players returned %d: %s", recPlayers.Code, recPlayers.Body.String())
	}
	var playersResp daemon.PlayerSearchResponse
	if err := json.Unmarshal(recPlayers.Body.Bytes(), &playersResp); err != nil {
		t.Fatalf("unmarshal /api/players failed: %v", err)
	}
	if len(playersResp.Players) != 2 {
		t.Fatalf("expected 2 non-local players in 2v2, got %d", len(playersResp.Players))
	}

	// 2. Verify GET /api/players/{id} for opponent (against_you_stats wins/losses from local perspective)
	recOpp := executeTestRequest(d, http.MethodGet, "/api/players/Epic%7Cnemesis_opp_202%7C0?playlist=2v2&from=2026-03-12&to=2026-03-20", nil)
	if recOpp.Code != http.StatusOK {
		t.Fatalf("GET /api/players/{id} returned %d: %s", recOpp.Code, recOpp.Body.String())
	}
	var oppDetail daemon.PlayerDetailResponse
	if err := json.Unmarshal(recOpp.Body.Bytes(), &oppDetail); err != nil {
		t.Fatalf("unmarshal /api/players/{id} failed: %v", err)
	}
	if oppDetail.AgainstYouStats == nil {
		t.Fatal("expected non-nil against_you_stats")
	}
	// On Day 2 (2026-03-15), local player LOST to FierceNemesis -> Wins=0, Losses=1 from local perspective!
	if oppDetail.AgainstYouStats.Games != 1 || oppDetail.AgainstYouStats.Wins != 0 || oppDetail.AgainstYouStats.Losses != 1 {
		t.Errorf("expected against_you_stats on Day 2 to have Games=1, Wins=0, Losses=1 (local perspective), got %+v", oppDetail.AgainstYouStats)
	}
	if oppDetail.AgainstYouStats.AvgBPM != 465.0 {
		t.Errorf("expected against_you_stats AvgBPM=465.0, got %v", oppDetail.AgainstYouStats.AvgBPM)
	}
	if oppDetail.RelationshipTag != "NEMESIS" {
		t.Errorf("expected relationship_tag=NEMESIS on Day 2 loss, got %q", oppDetail.RelationshipTag)
	}

	// 3. Verify GET /api/players/{id}/matches?result=defeat evaluates defeat from local ("US") perspective
	recOppMatches := executeTestRequest(d, http.MethodGet, "/api/players/Epic%7Cnemesis_opp_202%7C0/matches?result=defeat&role=opponent", nil)
	if recOppMatches.Code != http.StatusOK {
		t.Fatalf("GET /api/players/{id}/matches returned %d: %s", recOppMatches.Code, recOppMatches.Body.String())
	}
	var oppMatchesResp daemon.PlayerMatchesResponse
	if err := json.Unmarshal(recOppMatches.Body.Bytes(), &oppMatchesResp); err != nil {
		t.Fatalf("unmarshal /api/players/{id}/matches failed: %v", err)
	}
	if len(oppMatchesResp.Matches) != 1 || oppMatchesResp.Matches[0].MatchGUID != "guid-2v2-loss-2" {
		t.Fatalf("expected 1 defeat match (guid-2v2-loss-2) from local perspective, got %+v", oppMatchesResp.Matches)
	}

	// 3b. Verify GET /api/players/{localID} and GET /api/players/me return HTTP 200 for the local player's own profile
	for _, selfEndpoint := range []string{"/api/players/Epic%7Cmy_epic_acc_001%7C0", "/api/players/me"} {
		recSelf := executeTestRequest(d, http.MethodGet, selfEndpoint, nil)
		if recSelf.Code != http.StatusOK {
			t.Fatalf("GET %s returned %d: %s", selfEndpoint, recSelf.Code, recSelf.Body.String())
		}
		var selfDetail daemon.PlayerDetailResponse
		if err := json.Unmarshal(recSelf.Body.Bytes(), &selfDetail); err != nil {
			t.Fatalf("unmarshal %s failed: %v", selfEndpoint, err)
		}
		if selfDetail.RelationshipTag != "YOU" {
			t.Errorf("%s RelationshipTag = %q; want YOU", selfEndpoint, selfDetail.RelationshipTag)
		}
		if selfDetail.WithYouStats == nil || selfDetail.WithYouStats.Games != 2 || selfDetail.WithYouStats.Wins != 1 || selfDetail.WithYouStats.Losses != 1 {
			t.Errorf("%s WithYouStats = %+v; want Games=2 Wins=1 Losses=1", selfEndpoint, selfDetail.WithYouStats)
		}
	}
	recSelfMatches := executeTestRequest(d, http.MethodGet, "/api/players/me/matches", nil)
	if recSelfMatches.Code != http.StatusOK {
		t.Fatalf("GET /api/players/me/matches returned %d: %s", recSelfMatches.Code, recSelfMatches.Body.String())
	}
	var selfMatchesResp daemon.PlayerMatchesResponse
	if err := json.Unmarshal(recSelfMatches.Body.Bytes(), &selfMatchesResp); err != nil {
		t.Fatalf("unmarshal /api/players/me/matches failed: %v", err)
	}
	if len(selfMatchesResp.Matches) != 2 {
		t.Errorf("expected 2 matches for local player, got %d", len(selfMatchesResp.Matches))
	}

	// 4. Verify GET /api/matches/{guid} returns enriched player rank & Ballchasing telemetry
	recMatchDetail := executeTestRequest(d, http.MethodGet, "/api/matches/guid-2v2-win-1", nil)
	if recMatchDetail.Code != http.StatusOK {
		t.Fatalf("GET /api/matches/guid-2v2-win-1 returned %d: %s", recMatchDetail.Code, recMatchDetail.Body.String())
	}
	var matchRec storage.HistoricalMatchRecord
	if err := json.Unmarshal(recMatchDetail.Body.Bytes(), &matchRec); err != nil {
		t.Fatalf("unmarshal /api/matches/{guid} failed: %v", err)
	}
	if matchRec.Stats == nil || len(matchRec.Stats.Players) != 3 {
		t.Fatalf("expected 3 enriched players in match stats, got %+v", matchRec.Stats)
	}
	var foundMe, foundTM, foundOpp bool
	for _, p := range matchRec.Stats.Players {
		switch p.Name {
		case "Me":
			foundMe = true
			if p.RankName != "Diamond III Div IV" || p.ShortRank != "DIA III · DIV IV" || p.MMR != 1045.0 {
				t.Errorf("expected Me rank Diamond III Div IV / DIA III · DIV IV / 1045.0, got %q / %q / %v", p.RankName, p.ShortRank, p.MMR)
			}
		case "PrimeMate":
			foundTM = true
			if p.RankName != "Champion I Div II" || p.ShortRank != "CHAMP I · DIV II" || p.MMR != 1090.5 {
				t.Errorf("expected PrimeMate rank Champion I Div II / CHAMP I · DIV II / 1090.5, got %q / %q / %v", p.RankName, p.ShortRank, p.MMR)
			}
			if p.Boost == nil || p.Boost.BPM != 390 {
				t.Errorf("expected PrimeMate Boost.BPM=390, got %+v", p.Boost)
			}
		case "FierceNemesis":
			foundOpp = true
			if p.RankName != "Grand Champion I Div I" || p.ShortRank != "GC I · DIV I" || p.MMR != 1435.0 {
				t.Errorf("expected FierceNemesis rank Grand Champion I Div I / GC I · DIV I / 1435.0, got %q / %q / %v", p.RankName, p.ShortRank, p.MMR)
			}
		}
	}
	if !foundMe || !foundTM || !foundOpp {
		t.Errorf("missing enriched match players: foundMe=%v foundTM=%v foundOpp=%v", foundMe, foundTM, foundOpp)
	}

	// 5. Verify GET /status and GET /api/session report replay quota exceeded (2/2 = 100%)
	recStatus := executeTestRequest(d, http.MethodGet, "/status", nil)
	if recStatus.Code != http.StatusOK {
		t.Fatalf("GET /status returned %d", recStatus.Code)
	}
	var statusResp daemon.DaemonStatusResponse
	if err := json.Unmarshal(recStatus.Body.Bytes(), &statusResp); err != nil {
		t.Fatalf("unmarshal /status failed: %v", err)
	}
	if statusResp.ReplayCount != 2 || statusResp.ReplayLimit != 2 || statusResp.ReplayLimitStatus != "exceeded" {
		t.Errorf("expected replay quota 2/2 (exceeded), got count=%d limit=%d status=%q", statusResp.ReplayCount, statusResp.ReplayLimit, statusResp.ReplayLimitStatus)
	}
	if statusResp.MMRPollerState != "idle" {
		t.Errorf("expected mmr_poller_state=idle, got %q", statusResp.MMRPollerState)
	}

	// 6. Verify POST /api/matches/prune CSRF check & pruning
	recPruneForbidden := executeTestRequestWithOrigin(d, http.MethodPost, "/api/matches/prune", "https://evil.example.com", strings.NewReader(`{"count":1}`))
	if recPruneForbidden.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for cross-origin POST /api/matches/prune, got %d", recPruneForbidden.Code)
	}
	recPruneOK := executeTestRequestWithOrigin(d, http.MethodPost, "/api/matches/prune", "http://127.0.0.1:49125", strings.NewReader(`{"count":1,"new_limit":10}`))
	if recPruneOK.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for valid POST /api/matches/prune, got %d: %s", recPruneOK.Code, recPruneOK.Body.String())
	}
	var pruneResp map[string]interface{}
	if err := json.Unmarshal(recPruneOK.Body.Bytes(), &pruneResp); err != nil {
		t.Fatalf("unmarshal prune response failed: %v", err)
	}
	if int(pruneResp["pruned_count"].(float64)) != 1 || int(pruneResp["replay_count"].(float64)) != 1 || int(pruneResp["replay_limit"].(float64)) != 10 {
		t.Errorf("unexpected prune response: %+v", pruneResp)
	}

	// 7. Verify POST /api/sync/ballchasing CSRF check
	recSyncForbidden := executeTestRequestWithOrigin(d, http.MethodPost, "/api/sync/ballchasing", "https://evil.example.com", nil)
	if recSyncForbidden.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for cross-origin POST /api/sync/ballchasing, got %d", recSyncForbidden.Code)
	}
}
