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

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
)

// ----------------------------------------------------------------------------
// E2E Test Suite 3: Dual Storage Backend Parity (SQLiteStore vs JSONStore)
// ----------------------------------------------------------------------------

func TestE2E_PlayerTrack_DualBackendParity(t *testing.T) {
	backends := []struct {
		name   string
		initFn func(dir string) (storage.StateStore, error)
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
			Enabled:       true,
			LocalPlayerID: localPID,
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
			Enabled:       true,
			LocalPlayerID: localPID,
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
