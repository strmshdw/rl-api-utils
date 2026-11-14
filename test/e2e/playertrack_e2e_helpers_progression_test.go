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

	"github.com/gorilla/websocket"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/syncer"
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
