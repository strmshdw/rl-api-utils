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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dank/rlapi"
	"github.com/gorilla/websocket"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/psynet"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/syncer"
	"github.com/dank/rl-api-utils/internal/testutil"
)

// ----------------------------------------------------------------------------
// Mock BakkesMod Exporter (RFC 6455 WebSocket Server)
// ----------------------------------------------------------------------------

type MockBakkesModExporter struct {
	server    *httptest.Server
	upgrader  websocket.Upgrader
	mu        sync.Mutex
	conns     map[*websocket.Conn]struct{}
	connectCh chan struct{}
}

func NewMockBakkesModExporter() *MockBakkesModExporter {
	m := &MockBakkesModExporter{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		conns:     make(map[*websocket.Conn]struct{}),
		connectCh: make(chan struct{}, 20),
	}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := m.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		m.mu.Lock()
		m.conns[conn] = struct{}{}
		m.mu.Unlock()

		select {
		case m.connectCh <- struct{}{}:
		default:
		}

		go func() {
			defer func() {
				m.mu.Lock()
				delete(m.conns, conn)
				m.mu.Unlock()
				_ = conn.Close()
			}()
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()
	}))
	return m
}

func (m *MockBakkesModExporter) URL() string {
	return "ws" + strings.TrimPrefix(m.server.URL, "http")
}

func (m *MockBakkesModExporter) Address() string {
	return strings.TrimPrefix(m.server.URL, "http://")
}

func (m *MockBakkesModExporter) WaitForClient(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		count := len(m.conns)
		m.mu.Unlock()
		if count > 0 {
			return true
		}
		select {
		case <-m.connectCh:
			return true
		case <-time.After(10 * time.Millisecond):
		}
	}
	return false
}

func (m *MockBakkesModExporter) Broadcast(msg statsapi.EventMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return m.BroadcastRaw(data)
}

func (m *MockBakkesModExporter) BroadcastRaw(data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for conn := range m.conns {
		_ = conn.WriteMessage(websocket.TextMessage, data)
	}
	return nil
}

func (m *MockBakkesModExporter) SendUpdateState(matchGUID string, playlistID int, players []statsapi.StatsPlayer) error {
	return m.Broadcast(statsapi.EventMessage{
		Event: "UpdateState",
		Data: statsapi.EventData{
			MatchGuid: matchGUID,
			Playlist:  playlistID,
			Game: &statsapi.StatsGame{
				PlaylistId:  playlistID,
				TimeSeconds: 300,
			},
			Players: players,
		},
	})
}

func (m *MockBakkesModExporter) SendMatchEnded(matchGUID string, winnerTeamNum int) error {
	return m.Broadcast(statsapi.EventMessage{
		Event: "MatchEnded",
		Data: statsapi.EventData{
			MatchGuid:     matchGUID,
			WinnerTeamNum: &winnerTeamNum,
		},
	})
}

func (m *MockBakkesModExporter) SendMatchEndedNilWinner(matchGUID string) error {
	return m.Broadcast(statsapi.EventMessage{
		Event: "MatchEnded",
		Data: statsapi.EventData{
			MatchGuid: matchGUID,
		},
	})
}

func (m *MockBakkesModExporter) CloseClientConnections() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for conn := range m.conns {
		_ = conn.Close()
		delete(m.conns, conn)
	}
}

func (m *MockBakkesModExporter) Close() {
	m.CloseClientConnections()
	m.server.Close()
}

// ----------------------------------------------------------------------------
// PsyNet HTTP Transport Redirection
// ----------------------------------------------------------------------------

type redirectPsyNetTransport struct {
	targetURL string
	base      http.RoundTripper
}

func (t *redirectPsyNetTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.Host, "psynet.gg") {
		target, _ := url.Parse(t.targetURL)
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
	}
	return t.base.RoundTrip(req)
}

// ----------------------------------------------------------------------------
// In-Memory Test Syncer Stub
// ----------------------------------------------------------------------------

type testSyncerStub struct {
	mu     sync.Mutex
	cycles int
}

func (s *testSyncerStub) RunCycle(ctx context.Context) (*syncer.SyncStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cycles++
	return &syncer.SyncStats{}, nil
}

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
			Enabled:          true,
			Protocol:         "websocket",
			Address:          exporter.Address(),
			HTTPTriggerPort:  0,
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

// ----------------------------------------------------------------------------
// E2E Test Suite 2: Multi-Match Role Progression (Teammate -> Opponent -> Multi-Playlist)
// ----------------------------------------------------------------------------

func TestE2E_PlayerTrack_MultiMatchProgression(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "progression.db")

	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to init SQLiteStore: %v", err)
	}
	defer store.Close()

	exporter := NewMockBakkesModExporter()
	defer exporter.Close()

	localPID := "Epic|local_user_777|0"
	playerA := "Steam|76561198000000088|0"

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mockFetcher := playertrack.NewMockSkillFetcher()

	trackerCfg := config.PlayerTrackingConfig{
		Enabled:        true,
		AutoFetchRanks: true,
		LocalPlayerID:  localPID,
	}
	authCfg := config.AuthConfig{
		Provider: "epic",
		Epic:     config.EpicConfig{AccountID: "local_user_777"},
	}
	tracker, err := playertrack.NewTracker(store, mockFetcher, trackerCfg, authCfg, playertrack.WithTrackerLogger(logger))
	if err != nil {
		t.Fatalf("failed to create Tracker: %v", err)
	}
	defer tracker.Close()

	statsTracker, _ := statsapi.NewTracker(store, statsapi.TrackerConfig{}, statsapi.WithLogger(logger))
	listenerCfg := statsapi.ListenerConfig{
		Address:        exporter.Address(),
		Protocol:       "websocket",
		ReconnectDelay: 50 * time.Millisecond,
	}
	listener, _ := statsapi.NewListener(listenerCfg, statsTracker, logger, statsapi.WithPlayerEventHandler(tracker))

	cfg := &config.Config{
		StatsAPI: config.StatsAPIConfig{
			Enabled:  true,
			Protocol: "websocket",
			Address:  exporter.Address(),
		},
		PlayerTracking: trackerCfg,
		Auth:           authCfg,
	}

	d, _ := daemon.New(&testSyncerStub{}, cfg,
		daemon.WithPlayerTracker(tracker),
		daemon.WithStateStore(store),
		daemon.WithStatsTracker(statsTracker),
		daemon.WithStatsListener(listener),
		daemon.WithLogger(logger),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = d.Start(ctx) }()

	if !exporter.WaitForClient(3 * time.Second) {
		t.Fatalf("timed out waiting for Stats API listener")
	}

	execGet := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		d.Handler(ctx).ServeHTTP(rec, req)
		return rec
	}

	// MATCH 1: Playlist 11 (2v2) — Player A is Teammate (Team 0), Local is Team 0. Local Team Wins (Winner=0)
	m1Players := []statsapi.StatsPlayer{
		{Name: "LocalUser", PrimaryId: localPID, TeamNum: 0},
		{Name: "DynamicRival", PrimaryId: playerA, TeamNum: 0},
		{Name: "Opponent1", PrimaryId: "Epic|opp_1|0", TeamNum: 1},
		{Name: "Opponent2", PrimaryId: "Epic|opp_2|0", TeamNum: 1},
	}
	_ = exporter.SendUpdateState("match-prog-001", 11, m1Players)
	time.Sleep(50 * time.Millisecond)
	_ = exporter.SendMatchEnded("match-prog-001", 0)
	time.Sleep(100 * time.Millisecond)

	// Verify after Match 1: Player A has 1 Win as Teammate
	recM1 := execGet("/players/" + url.PathEscape(playerA))
	var detM1 daemon.PlayerDetailResponse
	_ = json.Unmarshal(recM1.Body.Bytes(), &detM1)
	if len(detM1.Matchups) != 1 || detM1.Matchups[0].WinsAsTeammate != 1 || detM1.Matchups[0].TotalMatches != 1 {
		t.Fatalf("after match 1: expected 1 win as teammate, got: %+v", detM1.Matchups)
	}

	// MATCH 2: Playlist 11 (2v2) — Player A is OPPONENT (Team 1), Local is Team 0. Opponent Wins (Winner=1 -> Local Lost)
	m2Players := []statsapi.StatsPlayer{
		{Name: "LocalUser", PrimaryId: localPID, TeamNum: 0},
		{Name: "NewTeammate", PrimaryId: "Epic|new_tm|0", TeamNum: 0},
		{Name: "DynamicRival", PrimaryId: playerA, TeamNum: 1},
		{Name: "Opponent2", PrimaryId: "Epic|opp_2|0", TeamNum: 1},
	}
	_ = exporter.SendUpdateState("match-prog-002", 11, m2Players)
	time.Sleep(50 * time.Millisecond)

	// In the middle of Match 2, check GET /current-match: Player A should be under opponents with historical matchup
	recMid := execGet("/current-match")
	var midResp playertrack.CurrentMatchResponse
	_ = json.Unmarshal(recMid.Body.Bytes(), &midResp)
	var oppA *playertrack.LobbyPlayer
	for i := range midResp.Opponents {
		if midResp.Opponents[i].PlayerID == playerA {
			oppA = &midResp.Opponents[i]
		}
	}
	if oppA == nil {
		t.Fatalf("expected Player A to be in opponents during Match 2")
	}
	if oppA.MatchupRecord == nil || oppA.MatchupRecord.WinsAsTeammate != 1 {
		t.Errorf("expected embedded matchup record in /current-match to reflect 1 win as teammate, got: %+v", oppA.MatchupRecord)
	}

	_ = exporter.SendMatchEnded("match-prog-002", 1) // Local team lost to Player A
	time.Sleep(100 * time.Millisecond)

	// Verify after Match 2: Player A in Playlist 11 has 1 Win as Teammate, 1 Loss as Opponent
	recM2 := execGet("/players/" + url.PathEscape(playerA))
	var detM2 daemon.PlayerDetailResponse
	_ = json.Unmarshal(recM2.Body.Bytes(), &detM2)
	if len(detM2.Matchups) != 1 {
		t.Fatalf("expected 1 matchup entry for playlist 11, got %d", len(detM2.Matchups))
	}
	m11 := detM2.Matchups[0]
	if m11.WinsAsTeammate != 1 || m11.LossesAsOpponent != 1 || m11.TotalMatches != 2 {
		t.Errorf("expected 1 win as teammate, 1 loss as opponent in playlist 11; got wins_tm=%d, loss_opp=%d, total=%d",
			m11.WinsAsTeammate, m11.LossesAsOpponent, m11.TotalMatches)
	}

	// MATCH 3: Playlist 13 (Standard 3v3) — Player A is OPPONENT (Team 1). Local Team Wins (Winner=0)
	m3Players := []statsapi.StatsPlayer{
		{Name: "LocalUser", PrimaryId: localPID, TeamNum: 0},
		{Name: "Teammate1", PrimaryId: "Epic|tm_1|0", TeamNum: 0},
		{Name: "Teammate2", PrimaryId: "Epic|tm_2|0", TeamNum: 0},
		{Name: "DynamicRival", PrimaryId: playerA, TeamNum: 1},
		{Name: "Opponent1", PrimaryId: "Epic|opp_1|0", TeamNum: 1},
		{Name: "Opponent2", PrimaryId: "Epic|opp_2|0", TeamNum: 1},
	}
	_ = exporter.SendUpdateState("match-prog-003", 13, m3Players)
	time.Sleep(50 * time.Millisecond)
	_ = exporter.SendMatchEnded("match-prog-003", 0) // Local won against Player A
	time.Sleep(100 * time.Millisecond)

	// Verify after Match 3: Multi-Playlist breakdown
	recM3 := execGet("/players/" + url.PathEscape(playerA))
	var detM3 daemon.PlayerDetailResponse
	_ = json.Unmarshal(recM3.Body.Bytes(), &detM3)
	if len(detM3.Matchups) != 2 {
		t.Fatalf("expected 2 playlist matchups (11 and 13), got %d", len(detM3.Matchups))
	}
	var p11Matchup, p13Matchup *storage.PlayerMatchup
	for _, m := range detM3.Matchups {
		if m.PlaylistID == 11 {
			p11Matchup = m
		} else if m.PlaylistID == 13 {
			p13Matchup = m
		}
	}
	if p11Matchup == nil || p13Matchup == nil {
		t.Fatalf("expected both playlist 11 and 13 matchups, got: %+v", detM3.Matchups)
	}
	if p11Matchup.WinsAsTeammate != 1 || p11Matchup.LossesAsOpponent != 1 || p11Matchup.TotalMatches != 2 {
		t.Errorf("playlist 11 mismatch: %+v", p11Matchup)
	}
	if p13Matchup.WinsAsOpponent != 1 || p13Matchup.TotalMatches != 1 {
		t.Errorf("playlist 13 mismatch: %+v", p13Matchup)
	}

	// Verify Aggregate Summary in GET /players
	recAll := execGet("/players")
	var summaries []*storage.PlayerSummary
	_ = json.Unmarshal(recAll.Body.Bytes(), &summaries)
	var sumA *storage.PlayerSummary
	for _, s := range summaries {
		if s.PlayerID == playerA {
			sumA = s
		}
	}
	if sumA == nil {
		t.Fatalf("Player A missing from /players aggregate")
	}
	if sumA.TotalWinsAsTeammate != 1 || sumA.TotalLossesAsTeammate != 0 ||
		sumA.TotalWinsAsOpponent != 1 || sumA.TotalLossesAsOpponent != 1 ||
		sumA.TotalMatches != 3 {
		t.Errorf("aggregate summary mismatch: wins_tm=%d, loss_tm=%d, win_opp=%d, loss_opp=%d, total=%d",
			sumA.TotalWinsAsTeammate, sumA.TotalLossesAsTeammate, sumA.TotalWinsAsOpponent, sumA.TotalLossesAsOpponent, sumA.TotalMatches)
	}
}

// ----------------------------------------------------------------------------
// E2E Test Suite 3: Dual Storage Backend Parity (SQLiteStore vs JSONStore)
// ----------------------------------------------------------------------------

func TestE2E_PlayerTrack_DualBackendParity(t *testing.T) {
	backends := []struct {
		name    string
		initFn  func(dir string) (storage.StateStore, error)
	}{
		{
			name: "SQLiteStore",
			initFn: func(dir string) (storage.StateStore, error) {
				return storage.NewSQLiteStore(filepath.Join(dir, "state.db"))
			},
		},
		{
			name: "JSONStore",
			initFn: func(dir string) (storage.StateStore, error) {
				return storage.NewJSONStore(filepath.Join(dir, "state.json"))
			},
		},
	}

	for _, b := range backends {
		t.Run(b.name, func(t *testing.T) {
			tempDir := t.TempDir()
			store, err := b.initFn(tempDir)
			if err != nil {
				t.Fatalf("backend init failed: %v", err)
			}
			defer store.Close()

			exporter := NewMockBakkesModExporter()
			defer exporter.Close()

			localPID := "Steam|76561198000000001|0"
			teammatePID := "Steam|76561198000000002|0"
			oppPID := "Epic|opp_001|0"

			mockFetcher := playertrack.NewMockSkillFetcher()
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))

			trackerCfg := config.PlayerTrackingConfig{
				Enabled:        true,
				AutoFetchRanks: true,
				LocalPlayerID:  localPID,
			}
			authCfg := config.AuthConfig{
				Provider: "steam",
				Steam:    config.SteamConfig{SteamID64: "76561198000000001"},
			}
			tracker, err := playertrack.NewTracker(store, mockFetcher, trackerCfg, authCfg, playertrack.WithTrackerLogger(logger))
			if err != nil {
				t.Fatalf("failed to create Tracker: %v", err)
			}
			defer tracker.Close()

			statsTracker, _ := statsapi.NewTracker(store, statsapi.TrackerConfig{}, statsapi.WithLogger(logger))
			listenerCfg := statsapi.ListenerConfig{
				Address:        exporter.Address(),
				Protocol:       "websocket",
				ReconnectDelay: 50 * time.Millisecond,
			}
			listener, _ := statsapi.NewListener(listenerCfg, statsTracker, logger, statsapi.WithPlayerEventHandler(tracker))

			cfg := &config.Config{
				StatsAPI: config.StatsAPIConfig{
					Enabled:  true,
					Protocol: "websocket",
					Address:  exporter.Address(),
				},
				PlayerTracking: trackerCfg,
				Auth:           authCfg,
			}

			d, _ := daemon.New(&testSyncerStub{}, cfg,
				daemon.WithPlayerTracker(tracker),
				daemon.WithStateStore(store),
				daemon.WithStatsTracker(statsTracker),
				daemon.WithStatsListener(listener),
				daemon.WithLogger(logger),
			)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() { _ = d.Start(ctx) }()

			if !exporter.WaitForClient(3 * time.Second) {
				t.Fatalf("timed out waiting for Stats API listener")
			}

			execGet := func(path string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodGet, path, nil)
				rec := httptest.NewRecorder()
				d.Handler(ctx).ServeHTTP(rec, req)
				return rec
			}

			// Broadcast Match 1: Local team wins
			players := []statsapi.StatsPlayer{
				{Name: "LocalHero", PrimaryId: localPID, TeamNum: 0},
				{Name: "TeammateTim", PrimaryId: teammatePID, TeamNum: 0},
				{Name: "Opponent1", PrimaryId: oppPID, TeamNum: 1},
			}
			_ = exporter.SendUpdateState("parity-match-1", 11, players)
			time.Sleep(50 * time.Millisecond)
			_ = exporter.SendMatchEnded("parity-match-1", 0)
			time.Sleep(100 * time.Millisecond)

			// Assert GET /players
			recPlayers := execGet("/players")
			if recPlayers.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", recPlayers.Code)
			}
			var summaries []*storage.PlayerSummary
			_ = json.Unmarshal(recPlayers.Body.Bytes(), &summaries)
			if len(summaries) < 3 {
				t.Fatalf("expected at least 3 players in summary, got %d", len(summaries))
			}

			// Assert GET /players/{id}
			recDetail := execGet("/players/" + url.PathEscape(teammatePID))
			if recDetail.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", recDetail.Code)
			}
			var detail daemon.PlayerDetailResponse
			_ = json.Unmarshal(recDetail.Body.Bytes(), &detail)
			if detail.PlayerID != teammatePID || len(detail.Matchups) != 1 || detail.Matchups[0].WinsAsTeammate != 1 {
				t.Errorf("backend %s parity assertion failed on player detail: %+v", b.name, detail)
			}

			// Assert duplicate MatchEnded idempotency
			_ = exporter.SendMatchEnded("parity-match-1", 0)
			time.Sleep(50 * time.Millisecond)
			recDetailDup := execGet("/players/" + url.PathEscape(teammatePID))
			var detailDup daemon.PlayerDetailResponse
			_ = json.Unmarshal(recDetailDup.Body.Bytes(), &detailDup)
			if detailDup.Matchups[0].WinsAsTeammate != 1 {
				t.Errorf("backend %s failed idempotency: wins=%d", b.name, detailDup.Matchups[0].WinsAsTeammate)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// E2E Test Suite 4: Offline Degradation (Missing or Disabled Polling Auth)
// ----------------------------------------------------------------------------

func TestE2E_PlayerTrack_OfflineDegradation(t *testing.T) {
	tempDir := t.TempDir()
	store, err := storage.NewSQLiteStore(filepath.Join(tempDir, "degrade.db"))
	if err != nil {
		t.Fatalf("store init failed: %v", err)
	}
	defer store.Close()

	exporter := NewMockBakkesModExporter()
	defer exporter.Close()

	localPID := "Epic|main_epic_acc|0"
	tmPID := "Steam|76561198000000002|0"
	oppPID := "Epic|opp_acc_001|0"

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// NoOpRankClient simulates disabled polling_auth or network failure
	noOpClient := playertrack.NewNoOpRankClient()

	trackerCfg := config.PlayerTrackingConfig{
		Enabled:        true,
		AutoFetchRanks: false, // Disabled rank queries
		LocalPlayerID:  localPID,
	}
	authCfg := config.AuthConfig{
		Provider: "epic",
		Epic:     config.EpicConfig{AccountID: "main_epic_acc"},
	}
	tracker, err := playertrack.NewTracker(store, noOpClient, trackerCfg, authCfg, playertrack.WithTrackerLogger(logger))
	if err != nil {
		t.Fatalf("tracker creation failed: %v", err)
	}
	defer tracker.Close()

	statsTracker, _ := statsapi.NewTracker(store, statsapi.TrackerConfig{}, statsapi.WithLogger(logger))
	listenerCfg := statsapi.ListenerConfig{
		Address:        exporter.Address(),
		Protocol:       "websocket",
		ReconnectDelay: 50 * time.Millisecond,
	}
	listener, _ := statsapi.NewListener(listenerCfg, statsTracker, logger, statsapi.WithPlayerEventHandler(tracker))

	cfg := &config.Config{
		StatsAPI: config.StatsAPIConfig{
			Enabled:  true,
			Protocol: "websocket",
			Address:  exporter.Address(),
		},
		PlayerTracking: trackerCfg,
		Auth:           authCfg,
	}

	d, _ := daemon.New(&testSyncerStub{}, cfg,
		daemon.WithPlayerTracker(tracker),
		daemon.WithStateStore(store),
		daemon.WithStatsTracker(statsTracker),
		daemon.WithStatsListener(listener),
		daemon.WithLogger(logger),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = d.Start(ctx) }()

	if !exporter.WaitForClient(3 * time.Second) {
		t.Fatalf("timed out waiting for Stats API listener")
	}

	execGet := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		d.Handler(ctx).ServeHTTP(rec, req)
		return rec
	}

	// Send match telemetry
	players := []statsapi.StatsPlayer{
		{Name: "MainPlayer", PrimaryId: localPID, TeamNum: 0},
		{Name: "Teammate", PrimaryId: tmPID, TeamNum: 0},
		{Name: "Opponent", PrimaryId: oppPID, TeamNum: 1},
	}
	_ = exporter.SendUpdateState("degrade-match-01", 11, players)
	time.Sleep(50 * time.Millisecond)

	// GET /current-match: Should succeed without ranks
	recCur := execGet("/current-match")
	if recCur.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recCur.Code)
	}
	var curResp playertrack.CurrentMatchResponse
	_ = json.Unmarshal(recCur.Body.Bytes(), &curResp)
	if !curResp.ActiveMatch {
		t.Errorf("expected active_match = true")
	}
	if len(curResp.Teammates) != 1 || curResp.Teammates[0].CurrentRank != nil {
		t.Errorf("expected teammate with nil CurrentRank, got %v", curResp.Teammates)
	}

	// Conclude match: Local team lost (Winner = 1)
	_ = exporter.SendMatchEnded("degrade-match-01", 1)
	time.Sleep(100 * time.Millisecond)

	// Verify matchup tracking works completely unimpeded
	recDetail := execGet("/players/" + url.PathEscape(tmPID))
	var det daemon.PlayerDetailResponse
	_ = json.Unmarshal(recDetail.Body.Bytes(), &det)
	if len(det.Matchups) != 1 || det.Matchups[0].LossesAsTeammate != 1 {
		t.Errorf("expected 1 loss as teammate, got: %+v", det.Matchups)
	}
}

// ----------------------------------------------------------------------------
// E2E Test Suite 5: State Persistence Across Daemon Restarts
// ----------------------------------------------------------------------------

func TestE2E_PlayerTrack_StatePersistenceAcrossRestarts(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "restart.db")

	localPID := "Steam|76561198000000001|0"
	tmPID := "Steam|76561198000000002|0"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Phase 1: Run Daemon 1 and record a match
	{
		store1, err := storage.NewSQLiteStore(dbPath)
		if err != nil {
			t.Fatalf("store 1 init failed: %v", err)
		}

		exporter1 := NewMockBakkesModExporter()
		trackerCfg := config.PlayerTrackingConfig{
			Enabled:        true,
			LocalPlayerID:  localPID,
		}
		authCfg := config.AuthConfig{
			Provider: "steam",
			Steam:    config.SteamConfig{SteamID64: "76561198000000001"},
		}
		tracker1, _ := playertrack.NewTracker(store1, playertrack.NewNoOpRankClient(), trackerCfg, authCfg, playertrack.WithTrackerLogger(logger))
		statsTracker1, _ := statsapi.NewTracker(store1, statsapi.TrackerConfig{}, statsapi.WithLogger(logger))
		listener1, _ := statsapi.NewListener(statsapi.ListenerConfig{Address: exporter1.Address(), Protocol: "websocket"}, statsTracker1, logger, statsapi.WithPlayerEventHandler(tracker1))

		d1, _ := daemon.New(&testSyncerStub{}, &config.Config{StatsAPI: config.StatsAPIConfig{Enabled: true, Address: exporter1.Address()}},
			daemon.WithPlayerTracker(tracker1),
			daemon.WithStateStore(store1),
			daemon.WithStatsTracker(statsTracker1),
			daemon.WithStatsListener(listener1),
			daemon.WithLogger(logger),
		)

		ctx1, cancel1 := context.WithCancel(context.Background())
		go func() { _ = d1.Start(ctx1) }()
		_ = exporter1.WaitForClient(3 * time.Second)

		players := []statsapi.StatsPlayer{
			{Name: "LocalHero", PrimaryId: localPID, TeamNum: 0},
			{Name: "TeammateTim", PrimaryId: tmPID, TeamNum: 0},
			{Name: "Opponent", PrimaryId: "Epic|opp_001|0", TeamNum: 1},
		}
		_ = exporter1.SendUpdateState("restart-match-1", 11, players)
		time.Sleep(50 * time.Millisecond)
		_ = exporter1.SendMatchEnded("restart-match-1", 0)
		time.Sleep(100 * time.Millisecond)

		// Terminate Daemon 1
		cancel1()
		exporter1.Close()
		tracker1.Close()
		store1.Close()
		time.Sleep(100 * time.Millisecond)
	}

	// Phase 2: Instantiate Daemon 2 on the same database file
	{
		store2, err := storage.NewSQLiteStore(dbPath)
		if err != nil {
			t.Fatalf("store 2 init failed: %v", err)
		}
		defer store2.Close()

		exporter2 := NewMockBakkesModExporter()
		defer exporter2.Close()

		trackerCfg := config.PlayerTrackingConfig{
			Enabled:        true,
			LocalPlayerID:  localPID,
		}
		authCfg := config.AuthConfig{
			Provider: "steam",
			Steam:    config.SteamConfig{SteamID64: "76561198000000001"},
		}
		tracker2, _ := playertrack.NewTracker(store2, playertrack.NewNoOpRankClient(), trackerCfg, authCfg, playertrack.WithTrackerLogger(logger))
		defer tracker2.Close()

		statsTracker2, _ := statsapi.NewTracker(store2, statsapi.TrackerConfig{}, statsapi.WithLogger(logger))
		listener2, _ := statsapi.NewListener(statsapi.ListenerConfig{Address: exporter2.Address(), Protocol: "websocket"}, statsTracker2, logger, statsapi.WithPlayerEventHandler(tracker2))

		d2, _ := daemon.New(&testSyncerStub{}, &config.Config{StatsAPI: config.StatsAPIConfig{Enabled: true, Address: exporter2.Address()}},
			daemon.WithPlayerTracker(tracker2),
			daemon.WithStateStore(store2),
			daemon.WithStatsTracker(statsTracker2),
			daemon.WithStatsListener(listener2),
			daemon.WithLogger(logger),
		)

		ctx2, cancel2 := context.WithCancel(context.Background())
		defer cancel2()
		go func() { _ = d2.Start(ctx2) }()
		_ = exporter2.WaitForClient(3 * time.Second)

		// Query Daemon 2: stats must be fully intact
		req := httptest.NewRequest(http.MethodGet, "/players/"+url.PathEscape(tmPID), nil)
		rec := httptest.NewRecorder()
		d2.Handler(ctx2).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 from Daemon 2, got %d", rec.Code)
		}
		var detail daemon.PlayerDetailResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &detail)
		if len(detail.Matchups) != 1 || detail.Matchups[0].WinsAsTeammate != 1 {
			t.Errorf("stats lost across restart: %+v", detail)
		}

		// Re-sending MatchEnded for the match processed in Daemon 1 must be rejected by Daemon 2
		_ = exporter2.SendMatchEnded("restart-match-1", 0)
		time.Sleep(50 * time.Millisecond)

		recDup := httptest.NewRecorder()
		d2.Handler(ctx2).ServeHTTP(recDup, req)
		var detailDup daemon.PlayerDetailResponse
		_ = json.Unmarshal(recDup.Body.Bytes(), &detailDup)
		if detailDup.Matchups[0].WinsAsTeammate != 1 {
			t.Errorf("idempotency violated across restarts: wins=%d", detailDup.Matchups[0].WinsAsTeammate)
		}
	}
}
