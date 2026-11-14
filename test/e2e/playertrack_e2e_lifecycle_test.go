package e2e

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/dank/rlapi"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/psynet"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/testutil"
)

// ----------------------------------------------------------------------------
// E2E Test Suite 1: Full Match Lifecycle Simulation
// ----------------------------------------------------------------------------

func TestE2E_PlayerTrack_FullMatchLifecycle(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state.db")

	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to init SQLiteStore: %v", err)
	}
	defer store.Close()

	// 1. Start Mock PsyNet Server with pre-configured player skills
	psyServer := testutil.NewMockPsyNetServer()
	defer psyServer.Close()

	localPID := "Steam|76561198000000001|0"
	teammatePID := "Steam|76561198000000002|0"
	oppAlphaPID := "Epic|opp_alpha_001|0"
	oppBetaPID := "Epic|opp_beta_002|0"

	psyServer.SetPlayerSkills(localPID, []rlapi.Skill{
		testutil.NewMockSkill(11, 16, 2, 1040.0, 50), // Champion II Division III
	})
	psyServer.SetPlayerSkills(teammatePID, []rlapi.Skill{
		testutil.NewMockSkill(11, 16, 3, 1055.0, 80), // Champion II Division IV
	})
	psyServer.SetPlayerSkills(oppAlphaPID, []rlapi.Skill{
		testutil.NewMockSkill(11, 17, 1, 1120.0, 120), // Champion III Division II
	})
	psyServer.SetPlayerSkills(oppBetaPID, []rlapi.Skill{
		testutil.NewMockSkill(11, 16, 0, 1005.0, 30), // Champion II Division I
	})

	// Redirect psynet.gg to mock server
	origTransport := http.DefaultTransport
	http.DefaultTransport = &redirectPsyNetTransport{
		targetURL: psyServer.URL(),
		base:      origTransport,
	}
	defer func() { http.DefaultTransport = origTransport }()

	// 2. Start Mock BakkesMod Exporter
	exporter := NewMockBakkesModExporter()
	defer exporter.Close()

	// 3. Initialize RankClient
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	creds := psynet.StaticCredentials{
		Platform:    "Epic",
		AuthToken:   "mock-polling-token",
		AccountID:   "polling-account-999",
		DisplayName: "RankQueryBot",
	}
	rankClient, err := playertrack.NewRankClient(playertrack.RankClientConfig{
		PrimaryAuth: config.AuthConfig{
			Provider: "steam",
			Steam:    config.SteamConfig{SteamID64: "76561198000000001"},
		},
		PollingAuth: config.PollingAuthConfig{
			Enabled:  true,
			Provider: "epic",
			Epic:     config.EpicConfig{AccountID: "polling-account-999"},
		},
		CredentialsSupplier: creds,
		Logger:              logger,
	})
	if err != nil {
		t.Fatalf("failed to create RankClient: %v", err)
	}
	defer rankClient.Close()

	// 4. Initialize Tracker
	trackerCfg := config.PlayerTrackingConfig{
		Enabled:        true,
		AutoFetchRanks: true,
		LocalPlayerID:  localPID,
	}
	authCfg := config.AuthConfig{
		Provider: "steam",
		Steam:    config.SteamConfig{SteamID64: "76561198000000001"},
	}
	tracker, err := playertrack.NewTracker(store, rankClient, trackerCfg, authCfg, playertrack.WithTrackerLogger(logger))
	if err != nil {
		t.Fatalf("failed to create Tracker: %v", err)
	}
	defer tracker.Close()

	// 5. Initialize Stats API Listener & Tracker
	statsTracker, err := statsapi.NewTracker(store, statsapi.TrackerConfig{}, statsapi.WithLogger(logger))
	if err != nil {
		t.Fatalf("failed to create StatsTracker: %v", err)
	}

	listenerCfg := statsapi.ListenerConfig{
		Address:        exporter.Address(),
		Protocol:       "websocket",
		ReconnectDelay: 50 * time.Millisecond,
	}
	listener, err := statsapi.NewListener(listenerCfg, statsTracker, logger, statsapi.WithPlayerEventHandler(tracker))
	if err != nil {
		t.Fatalf("failed to create Listener: %v", err)
	}

	// 6. Initialize Daemon
	cfg := &config.Config{
		Sync: config.SyncConfig{
			PollInterval: config.Duration(1 * time.Hour),
		},
		StatsAPI: config.StatsAPIConfig{
			Enabled:         true,
			Protocol:        "websocket",
			Address:         exporter.Address(),
			HTTPTriggerPort: 0,
		},
		PlayerTracking: trackerCfg,
		Auth:           authCfg,
	}

	d, err := daemon.New(&testSyncerStub{}, cfg,
		daemon.WithPlayerTracker(tracker),
		daemon.WithStateStore(store),
		daemon.WithStatsTracker(statsTracker),
		daemon.WithStatsListener(listener),
		daemon.WithLogger(logger),
	)
	if err != nil {
		t.Fatalf("failed to create Daemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = d.Start(ctx)
	}()

	// Await connection from listener to exporter
	if !exporter.WaitForClient(3 * time.Second) {
		t.Fatalf("timed out waiting for Stats API listener to connect to mock exporter")
	}

	// Handler helper for testing local API endpoints
	execGet := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		d.Handler(ctx).ServeHTTP(rec, req)
		return rec
	}

	// 7. Verify Initial Idle State on GET /current-match
	recIdle := execGet("/current-match")
	if recIdle.Code != http.StatusOK {
		t.Fatalf("expected 200 for idle /current-match, got %d", recIdle.Code)
	}
	var idleResp playertrack.CurrentMatchResponse
	if err := json.Unmarshal(recIdle.Body.Bytes(), &idleResp); err != nil {
		t.Fatalf("failed to decode idle /current-match: %v", err)
	}
	if idleResp.ActiveMatch {
		t.Fatalf("expected active_match = false in idle state")
	}
	if idleResp.Teammates == nil || idleResp.Opponents == nil {
		t.Fatalf("expected non-nil slices for teammates and opponents")
	}

	// 8. Broadcast UpdateState Event with 6 players
	players := []statsapi.StatsPlayer{
		{Name: "LocalHero", PrimaryId: localPID, TeamNum: 0, Score: 250, Goals: 1, Saves: 1},
		{Name: "TeammateTim", PrimaryId: teammatePID, TeamNum: 0, Score: 310, Goals: 1, Assists: 1},
		{Name: "OpponentAlice", PrimaryId: oppAlphaPID, TeamNum: 1, Score: 180, Saves: 2},
		{Name: "OpponentBob", PrimaryId: oppBetaPID, TeamNum: 1, Score: 150, Shots: 2},
		{Name: "CoachSpectator", PrimaryId: "Steam|76561198000000099|0", TeamNum: 255, Score: 0},
		{Name: "Merlin", PrimaryId: "Unknown|0|0", TeamNum: 0, Score: 50},
	}
	matchGUID := "e2e-match-101"
	if err := exporter.SendUpdateState(matchGUID, 11, players); err != nil {
		t.Fatalf("failed to send UpdateState: %v", err)
	}

	// Wait for ranks to be fetched asynchronously
	var curMatch playertrack.CurrentMatchResponse
	deadline := time.Now().Add(4 * time.Second)
	foundRanks := false
	for time.Now().Before(deadline) {
		rec := execGet("/current-match")
		if rec.Code == http.StatusOK {
			if err := json.Unmarshal(rec.Body.Bytes(), &curMatch); err == nil {
				if curMatch.ActiveMatch && curMatch.LocalPlayer != nil && curMatch.LocalPlayer.CurrentRank != nil {
					foundRanks = true
					break
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !foundRanks {
		t.Fatalf("timed out waiting for rank population in /current-match: %+v", curMatch)
	}

	// 9. Assert Current Match State
	if curMatch.MatchGUID != matchGUID {
		t.Errorf("expected match_guid %q, got %q", matchGUID, curMatch.MatchGUID)
	}
	if curMatch.PlaylistID != 11 {
		t.Errorf("expected playlist_id 11, got %d", curMatch.PlaylistID)
	}
	if curMatch.PlaylistName != "Ranked Doubles (2v2)" {
		t.Errorf("expected playlist_name 'Ranked Doubles (2v2)', got %q", curMatch.PlaylistName)
	}
	if curMatch.LocalTeam == nil || *curMatch.LocalTeam != 0 {
		t.Errorf("expected local_team = 0, got %v", curMatch.LocalTeam)
	}
	if curMatch.LocalPlayer.CurrentRank.RankName != "Champion I Division III" {
		t.Errorf("expected local player rank 'Champion I Division III', got %q", curMatch.LocalPlayer.CurrentRank.RankName)
	}
	if curMatch.LocalPlayer.CurrentRank.MMR != 1040.0 {
		t.Errorf("expected local player MMR 1040.0, got %f", curMatch.LocalPlayer.CurrentRank.MMR)
	}

	// Check Teammates
	if len(curMatch.Teammates) != 2 {
		t.Fatalf("expected 2 teammates, got %d", len(curMatch.Teammates))
	}
	var tmHuman, tmBot *playertrack.LobbyPlayer
	for i := range curMatch.Teammates {
		if curMatch.Teammates[i].PlayerID == teammatePID {
			tmHuman = &curMatch.Teammates[i]
		} else if curMatch.Teammates[i].IsBot {
			tmBot = &curMatch.Teammates[i]
		}
	}
	if tmHuman == nil || tmBot == nil {
		t.Fatalf("expected human teammate and bot teammate, got: %+v", curMatch.Teammates)
	}
	if tmHuman.CurrentRank == nil || tmHuman.CurrentRank.RankName != "Champion I Division IV" {
		t.Errorf("expected TeammateTim rank 'Champion I Division IV', got %v", tmHuman.CurrentRank)
	}
	if tmHuman.CurrentRank.MMR != 1055.0 {
		t.Errorf("expected TeammateTim MMR 1055.0, got %f", tmHuman.CurrentRank.MMR)
	}
	if tmBot.CurrentRank != nil {
		t.Errorf("expected bot to have nil CurrentRank, got %v", tmBot.CurrentRank)
	}

	// Check Opponents
	if len(curMatch.Opponents) != 2 {
		t.Fatalf("expected 2 opponents, got %d", len(curMatch.Opponents))
	}
	var oppAlpha, oppBeta *playertrack.LobbyPlayer
	for i := range curMatch.Opponents {
		if curMatch.Opponents[i].PlayerID == oppAlphaPID {
			oppAlpha = &curMatch.Opponents[i]
		} else if curMatch.Opponents[i].PlayerID == oppBetaPID {
			oppBeta = &curMatch.Opponents[i]
		}
	}
	if oppAlpha == nil || oppAlpha.CurrentRank == nil || oppAlpha.CurrentRank.RankName != "Champion II Division II" {
		t.Errorf("expected OpponentAlice rank 'Champion II Division II', got %v", oppAlpha)
	}
	if oppBeta == nil || oppBeta.CurrentRank == nil || oppBeta.CurrentRank.RankName != "Champion I Division I" {
		t.Errorf("expected OpponentBob rank 'Champion I Division I', got %v", oppBeta)
	}

	// Check Spectators
	if len(curMatch.Spectators) != 1 {
		t.Fatalf("expected 1 spectator, got %d", len(curMatch.Spectators))
	}
	if curMatch.Spectators[0].TeamNum != 255 {
		t.Errorf("expected spectator team 255, got %d", curMatch.Spectators[0].TeamNum)
	}

	// 10. Broadcast MatchEnded (Winner = Team 0 -> Local Player Team Won)
	if err := exporter.SendMatchEnded(matchGUID, 0); err != nil {
		t.Fatalf("failed to send MatchEnded: %v", err)
	}

	// Await processing
	var endedMatch playertrack.CurrentMatchResponse
	time.Sleep(200 * time.Millisecond)
	recEnded := execGet("/current-match")
	if recEnded.Code != http.StatusOK {
		t.Fatalf("expected 200 for ended /current-match, got %d", recEnded.Code)
	}
	_ = json.Unmarshal(recEnded.Body.Bytes(), &endedMatch)
	if endedMatch.ActiveMatch {
		t.Errorf("expected active_match = false after match end")
	}
	if !endedMatch.MatchEnded {
		t.Errorf("expected match_ended = true after match end")
	}
	if endedMatch.Result != "victory" {
		t.Errorf("expected result = 'victory', got %q", endedMatch.Result)
	}

	// 11. Query Persistent Storage via HTTP GET /players
	recPlayers := execGet("/players")
	if recPlayers.Code != http.StatusOK {
		t.Fatalf("expected 200 for GET /players, got %d", recPlayers.Code)
	}
	var summaries []*storage.PlayerSummary
	if err := json.Unmarshal(recPlayers.Body.Bytes(), &summaries); err != nil {
		t.Fatalf("failed to decode /players response: %v", err)
	}

	summaryMap := make(map[string]*storage.PlayerSummary)
	for _, s := range summaries {
		summaryMap[s.PlayerID] = s
	}

	// Teammate check
	tmSummary, ok := summaryMap[teammatePID]
	if !ok {
		t.Fatalf("expected teammate %q in /players", teammatePID)
	}
	if tmSummary.TotalWinsAsTeammate != 1 || tmSummary.TotalLossesAsTeammate != 0 || tmSummary.TotalMatches != 1 {
		t.Errorf("expected teammate 1 win as teammate, 0 losses, got wins=%d, losses=%d, total=%d",
			tmSummary.TotalWinsAsTeammate, tmSummary.TotalLossesAsTeammate, tmSummary.TotalMatches)
	}

	// Opponent checks
	oppASummary, ok := summaryMap[oppAlphaPID]
	if !ok {
		t.Fatalf("expected opponent %q in /players", oppAlphaPID)
	}
	if oppASummary.TotalWinsAsOpponent != 1 || oppASummary.TotalLossesAsOpponent != 0 || oppASummary.TotalMatches != 1 {
		t.Errorf("expected opponent 1 win as opponent, got wins=%d, losses=%d, total=%d",
			oppASummary.TotalWinsAsOpponent, oppASummary.TotalLossesAsOpponent, oppASummary.TotalMatches)
	}

	oppBSummary, ok := summaryMap[oppBetaPID]
	if !ok {
		t.Fatalf("expected opponent %q in /players", oppBetaPID)
	}
	if oppBSummary.TotalWinsAsOpponent != 1 || oppBSummary.TotalLossesAsOpponent != 0 || oppBSummary.TotalMatches != 1 {
		t.Errorf("expected opponent 1 win as opponent, got wins=%d, losses=%d, total=%d",
			oppBSummary.TotalWinsAsOpponent, oppBSummary.TotalLossesAsOpponent, oppBSummary.TotalMatches)
	}

	// Bots and spectators must NOT be recorded in player_matchups
	if _, botFound := summaryMap["Unknown|0|0"]; botFound {
		t.Errorf("bot Unknown|0|0 should NOT exist in player summaries")
	}

	// 12. Query Specific Player Breakdown via GET /players/{id}
	recDetail := execGet("/players/" + url.PathEscape(teammatePID))
	if recDetail.Code != http.StatusOK {
		t.Fatalf("expected 200 for GET /players/{id}, got %d: %s", recDetail.Code, recDetail.Body.String())
	}
	var detail daemon.PlayerDetailResponse
	if err := json.Unmarshal(recDetail.Body.Bytes(), &detail); err != nil {
		t.Fatalf("failed to decode detail response: %v", err)
	}
	if detail.PlayerID != teammatePID {
		t.Errorf("expected player_id %q, got %q", teammatePID, detail.PlayerID)
	}
	if len(detail.Matchups) != 1 {
		t.Fatalf("expected 1 matchup entry for playlist 11, got %d", len(detail.Matchups))
	}
	if detail.Matchups[0].PlaylistID != 11 {
		t.Errorf("expected matchup playlist 11, got %d", detail.Matchups[0].PlaylistID)
	}
	if detail.Matchups[0].WinsAsTeammate != 1 || detail.Matchups[0].TotalMatches != 1 {
		t.Errorf("expected 1 win as teammate, got wins=%d, total=%d",
			detail.Matchups[0].WinsAsTeammate, detail.Matchups[0].TotalMatches)
	}

	// 13. Idempotency Check: Broadcast Duplicate MatchEnded
	if err := exporter.SendMatchEnded(matchGUID, 0); err != nil {
		t.Fatalf("failed to resend duplicate MatchEnded: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	recDetailDup := execGet("/players/" + url.PathEscape(teammatePID))
	var detailDup daemon.PlayerDetailResponse
	_ = json.Unmarshal(recDetailDup.Body.Bytes(), &detailDup)
	if detailDup.Matchups[0].WinsAsTeammate != 1 || detailDup.Matchups[0].TotalMatches != 1 {
		t.Errorf("duplicate MatchEnded doubled the counts: wins=%d, total=%d",
			detailDup.Matchups[0].WinsAsTeammate, detailDup.Matchups[0].TotalMatches)
	}
}
