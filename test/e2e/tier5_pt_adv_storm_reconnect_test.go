package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
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
// SECTION 2: High-Frequency Event Storm (10,000 Frames @ 120Hz)
// ----------------------------------------------------------------------------

func TestTier5_PT_Adv2_HighFrequencyEventStorm_10kFrames120Hz(t *testing.T) {
	tempDir := t.TempDir()
	store, err := storage.NewSQLiteStore(filepath.Join(tempDir, "storm.db"))
	if err != nil {
		t.Fatalf("store init failed: %v", err)
	}
	defer store.Close()

	fetcher := newAdvSkillFetcher()
	fetcher.SetSkill("Steam|p1|0", []rlapi.Skill{{Playlist: 11, Tier: 16, Division: 2, MMR: 1040.0}})
	fetcher.SetSkill("Steam|p2|0", []rlapi.Skill{{Playlist: 11, Tier: 16, Division: 3, MMR: 1055.0}})
	fetcher.SetSkill("Epic|opp1|0", []rlapi.Skill{{Playlist: 11, Tier: 17, Division: 1, MMR: 1120.0}})
	fetcher.SetSkill("Epic|opp2|0", []rlapi.Skill{{Playlist: 11, Tier: 16, Division: 0, MMR: 1005.0}})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	trackerCfg := config.PlayerTrackingConfig{
		Enabled:        true,
		AutoFetchRanks: true,
		LocalPlayerID:  "Steam|p1|0",
	}
	authCfg := config.AuthConfig{
		Provider: "steam",
		Steam:    config.SteamConfig{SteamID64: "p1"},
	}
	tracker, err := playertrack.NewTracker(store, fetcher, trackerCfg, authCfg, playertrack.WithTrackerLogger(logger))
	if err != nil {
		t.Fatalf("tracker creation failed: %v", err)
	}
	defer tracker.Close()

	d, _ := daemon.New(&testSyncerStub{}, &config.Config{PlayerTracking: trackerCfg},
		daemon.WithPlayerTracker(tracker),
		daemon.WithStateStore(store),
		daemon.WithLogger(logger),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const totalFrames = 10000
	const numWriters = 4
	const framesPerWriter = totalFrames / numWriters
	const numReaders = 20

	startBarrier := make(chan struct{})
	var wg sync.WaitGroup
	var jsonErrors int64

	// Launch 4 concurrent telemetry writers
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			<-startBarrier
			for i := 0; i < framesPerWriter; i++ {
				scoreDelta := workerID*1000 + i
				players := []statsapi.StatsPlayer{
					{Name: "LocalP1", PrimaryId: "Steam|p1|0", TeamNum: 0, Score: 100 + scoreDelta},
					{Name: "TeammateP2", PrimaryId: "Steam|p2|0", TeamNum: 0, Score: 90 + scoreDelta},
					{Name: "Opponent1", PrimaryId: "Epic|opp1|0", TeamNum: 1, Score: 80 + scoreDelta},
					{Name: "Opponent2", PrimaryId: "Epic|opp2|0", TeamNum: 1, Score: 70 + scoreDelta},
				}
				_ = tracker.OnUpdateState(ctx, "storm-match-guid", 11, players)
			}
		}(w)
	}

	// Launch 20 concurrent HTTP readers
	stopReaders := make(chan struct{})
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			<-startBarrier
			endpoints := []string{"/current-match", "/players", "/players/Steam%7Cp2%7C0"}
			idx := 0
			for {
				select {
				case <-stopReaders:
					return
				default:
				}

				path := endpoints[idx%len(endpoints)]
				idx++
				req := httptest.NewRequest(http.MethodGet, path, nil)
				rec := httptest.NewRecorder()
				d.Handler(ctx).ServeHTTP(rec, req)

				if rec.Code == http.StatusOK {
					var raw json.RawMessage
					if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
						atomic.AddInt64(&jsonErrors, 1)
					}
				}
				runtime.Gosched()
			}
		}(r)
	}

	// Release all workers simultaneously
	close(startBarrier)

	// Wait for writers to complete 10,000 frames
	time.Sleep(300 * time.Millisecond)
	close(stopReaders)
	wg.Wait()

	if jsonErrors > 0 {
		t.Errorf("encountered %d JSON unmarshal errors during storm", jsonErrors)
	}

	// Conclude match and verify final outcome
	winnerTeam := 0
	_ = tracker.OnMatchEnded(ctx, "storm-match-guid", &winnerTeam)

	req := httptest.NewRequest(http.MethodGet, "/players/Steam%7Cp2%7C0", nil)
	rec := httptest.NewRecorder()
	d.Handler(ctx).ServeHTTP(rec, req)
	var detail daemon.PlayerDetailResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &detail)
	if len(detail.Matchups) != 1 || detail.Matchups[0].WinsAsTeammate != 1 {
		t.Errorf("storm outcome recorded incorrectly: %+v", detail)
	}
}

func TestTier5_PT_Adv2_SingleFlightRankDeduplication_UnderStorm(t *testing.T) {
	tempDir := t.TempDir()
	store, _ := storage.NewSQLiteStore(filepath.Join(tempDir, "dedup.db"))
	defer store.Close()

	fetcher := newAdvSkillFetcher()
	fetcher.SetSkill("Steam|p1|0", []rlapi.Skill{{Playlist: 11, Tier: 16, Division: 2}})
	fetcher.SetSkill("Steam|p2|0", []rlapi.Skill{{Playlist: 11, Tier: 16, Division: 3}})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	trackerCfg := config.PlayerTrackingConfig{
		Enabled:        true,
		AutoFetchRanks: true,
		LocalPlayerID:  "Steam|p1|0",
	}
	tracker, _ := playertrack.NewTracker(store, fetcher, trackerCfg, config.AuthConfig{}, playertrack.WithTrackerLogger(logger))
	defer tracker.Close()

	// Blast 1,000 frames for 2 players
	players := []statsapi.StatsPlayer{
		{Name: "P1", PrimaryId: "Steam|p1|0", TeamNum: 0},
		{Name: "P2", PrimaryId: "Steam|p2|0", TeamNum: 0},
	}
	for i := 0; i < 1000; i++ {
		_ = tracker.OnUpdateState(context.Background(), "dedup-match", 11, players)
	}

	time.Sleep(100 * time.Millisecond)

	// Single flight deduplication assertion:
	// Out of 1,000 frames, mock fetcher should be invoked at most 2 times (once per player ID)
	callCount := fetcher.GetCallCount()
	if callCount > 2 {
		t.Errorf("single flight deduplication failed: expected <= 2 calls for 1000 frames, got %d", callCount)
	}
}

func TestTier5_PT_Adv2_MemoryBoundedness_UnderEventStorm(t *testing.T) {
	tempDir := t.TempDir()
	store, _ := storage.NewSQLiteStore(filepath.Join(tempDir, "mem.db"))
	defer store.Close()

	trackerCfg := config.PlayerTrackingConfig{
		Enabled:        true,
		AutoFetchRanks: false,
		LocalPlayerID:  "Steam|local|0",
	}
	tracker, _ := playertrack.NewTracker(store, playertrack.NewNoOpRankClient(), trackerCfg, config.AuthConfig{})
	defer tracker.Close()

	runtime.GC()
	var m0 runtime.MemStats
	runtime.ReadMemStats(&m0)

	players := []statsapi.StatsPlayer{
		{Name: "Local", PrimaryId: "Steam|local|0", TeamNum: 0},
		{Name: "TM", PrimaryId: "Steam|tm|0", TeamNum: 0},
		{Name: "Opp1", PrimaryId: "Epic|opp1|0", TeamNum: 1},
		{Name: "Opp2", PrimaryId: "Epic|opp2|0", TeamNum: 1},
	}

	for i := 0; i < 10000; i++ {
		_ = tracker.OnUpdateState(context.Background(), "mem-match", 11, players)
	}

	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	var deltaMB float64
	if m1.Alloc > m0.Alloc {
		deltaMB = float64(m1.Alloc-m0.Alloc) / (1024 * 1024)
	}

	if deltaMB > 15.0 {
		t.Errorf("heap memory grew by %.2f MB under 10k frames (expected < 15.0 MB)", deltaMB)
	}
}

func TestTier5_PT_Adv2_HTTPLatency_UnderStorm(t *testing.T) {
	tempDir := t.TempDir()
	store, _ := storage.NewSQLiteStore(filepath.Join(tempDir, "latency.db"))
	defer store.Close()

	trackerCfg := config.PlayerTrackingConfig{
		Enabled:        true,
		AutoFetchRanks: false,
		LocalPlayerID:  "Steam|local|0",
	}
	tracker, _ := playertrack.NewTracker(store, playertrack.NewNoOpRankClient(), trackerCfg, config.AuthConfig{})
	defer tracker.Close()

	d, _ := daemon.New(&testSyncerStub{}, &config.Config{PlayerTracking: trackerCfg},
		daemon.WithPlayerTracker(tracker),
		daemon.WithStateStore(store),
	)

	// Concurrent writer continuously streaming frames
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		players := []statsapi.StatsPlayer{
			{Name: "Local", PrimaryId: "Steam|local|0", TeamNum: 0},
			{Name: "TM", PrimaryId: "Steam|tm|0", TeamNum: 0},
		}
		for {
			select {
			case <-ctx.Done():
				return
			default:
				_ = tracker.OnUpdateState(ctx, "latency-match", 11, players)
			}
		}
	}()

	// Measure read latency
	for i := 0; i < 50; i++ {
		start := time.Now()
		req := httptest.NewRequest(http.MethodGet, "/current-match", nil)
		rec := httptest.NewRecorder()
		d.Handler(ctx).ServeHTTP(rec, req)
		duration := time.Since(start)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if duration > 50*time.Millisecond {
			t.Errorf("iteration %d: read latency exceeded 50ms: %v", i, duration)
		}
	}
}

// ----------------------------------------------------------------------------
// SECTION 4: Network Disconnect & Reconnect Resilience
// ----------------------------------------------------------------------------

func TestTier5_PT_Adv4_StatsAPI_DropAndReconnect_Resilience(t *testing.T) {
	tempDir := t.TempDir()
	store, _ := storage.NewSQLiteStore(filepath.Join(tempDir, "reconnect.db"))
	defer store.Close()

	exporter := NewMockBakkesModExporter()
	defer exporter.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	trackerCfg := config.PlayerTrackingConfig{
		Enabled:       true,
		LocalPlayerID: "Steam|local|0",
	}
	tracker, _ := playertrack.NewTracker(store, playertrack.NewNoOpRankClient(), trackerCfg, config.AuthConfig{}, playertrack.WithTrackerLogger(logger))
	defer tracker.Close()

	statsTracker, _ := statsapi.NewTracker(store, statsapi.TrackerConfig{}, statsapi.WithLogger(logger))
	listenerCfg := statsapi.ListenerConfig{
		Address:        exporter.Address(),
		Protocol:       "websocket",
		ReconnectDelay: 25 * time.Millisecond,
	}
	listener, _ := statsapi.NewListener(listenerCfg, statsTracker, logger, statsapi.WithPlayerEventHandler(tracker))

	d, _ := daemon.New(&testSyncerStub{}, &config.Config{StatsAPI: config.StatsAPIConfig{Enabled: true, Address: exporter.Address()}},
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
		t.Fatalf("listener did not connect initially")
	}

	// Send Match 1 UpdateState
	players := []statsapi.StatsPlayer{
		{Name: "Local", PrimaryId: "Steam|local|0", TeamNum: 0},
		{Name: "TM", PrimaryId: "Steam|tm|0", TeamNum: 0},
	}
	_ = exporter.SendUpdateState("drop-match-1", 11, players)
	time.Sleep(50 * time.Millisecond)

	// Simulate socket drop
	exporter.CloseClientConnections()
	time.Sleep(50 * time.Millisecond)

	// Await automatic reconnection
	if !exporter.WaitForClient(3 * time.Second) {
		t.Fatalf("listener failed to automatically reconnect after drop")
	}

	// Send MatchEnded on re-established connection
	_ = exporter.SendMatchEnded("drop-match-1", 0)
	time.Sleep(100 * time.Millisecond)

	// Verify match ended was successfully ingested after reconnect
	matchup, err := store.GetPlayerMatchup(context.Background(), "Steam|tm|0", 11)
	if err != nil || matchup.WinsAsTeammate != 1 {
		t.Errorf("match outcome not recorded after reconnect: %+v, err: %v", matchup, err)
	}
}

func TestTier5_PT_Adv4_PsyNet_ServerErrors500_NegativeCacheBackoff(t *testing.T) {
	tempDir := t.TempDir()
	store, _ := storage.NewSQLiteStore(filepath.Join(tempDir, "backoff.db"))
	defer store.Close()

	fetcher := newAdvSkillFetcher()
	fetcher.SetError(errors.New("HTTP 500: PsyNet database offline"))

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	trackerCfg := config.PlayerTrackingConfig{
		Enabled:        true,
		AutoFetchRanks: true,
		LocalPlayerID:  "Epic|local|0",
	}
	tracker, _ := playertrack.NewTracker(store, fetcher, trackerCfg, config.AuthConfig{},
		playertrack.WithTrackerLogger(logger),
		playertrack.WithBackoffTTL(50*time.Millisecond),
	)
	defer tracker.Close()

	playerPID := "Epic|player-500|0"
	players := []statsapi.StatsPlayer{
		{Name: "Local", PrimaryId: "Epic|local|0", TeamNum: 0},
		{Name: "P500", PrimaryId: playerPID, TeamNum: 0},
	}

	// Frame 1: Fails, populates failBackoff
	_ = tracker.OnUpdateState(context.Background(), "backoff-match", 11, players)
	time.Sleep(20 * time.Millisecond)

	// Blast 50 frames during backoff window
	for i := 0; i < 50; i++ {
		_ = tracker.OnUpdateState(context.Background(), "backoff-match", 11, players)
	}
	time.Sleep(20 * time.Millisecond)

	// Assert fetcher was invoked only 1 time (suppressed by backoff)
	count := fetcher.GetCallCount()
	if count != 1 {
		t.Fatalf("expected exactly 1 call during backoff window, got %d", count)
	}

	// Heal mock fetcher and wait for backoff window to expire
	fetcher.SetError(nil)
	fetcher.SetSkill(rlapi.PlayerID(playerPID), []rlapi.Skill{{Playlist: 11, Tier: 16, Division: 2}})
	time.Sleep(60 * time.Millisecond)

	// Frame 52: After backoff expiry, query should be dispatched and succeed
	_ = tracker.OnUpdateState(context.Background(), "backoff-match", 11, players)
	time.Sleep(50 * time.Millisecond)

	count2 := fetcher.GetCallCount()
	if count2 < 2 {
		t.Fatalf("expected second query after backoff TTL expired, got %d", count2)
	}
}

func TestTier5_PT_Adv4_PsyNet_AbruptDropAndSelfHealing(t *testing.T) {
	tempDir := t.TempDir()
	store, _ := storage.NewSQLiteStore(filepath.Join(tempDir, "psy_drop.db"))
	defer store.Close()

	psyServer := testutil.NewMockPsyNetServer()
	defer psyServer.Close()

	targetPID := "Epic|test-reconnect-player|0"
	psyServer.SetPlayerSkills(targetPID, []rlapi.Skill{
		testutil.NewMockSkill(11, 16, 2, 1040.0, 50),
	})

	origTransport := http.DefaultTransport
	http.DefaultTransport = &redirectPsyNetTransport{
		targetURL: psyServer.URL(),
		base:      origTransport,
	}
	defer func() { http.DefaultTransport = origTransport }()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	creds := psynet.StaticCredentials{
		Platform:    "Epic",
		AuthToken:   "tok-1",
		AccountID:   "polling-1",
		DisplayName: "Bot",
	}

	rankClient, err := playertrack.NewRankClient(playertrack.RankClientConfig{
		PrimaryAuth:         config.AuthConfig{Provider: "epic", Epic: config.EpicConfig{AccountID: "main-1"}},
		PollingAuth:         config.PollingAuthConfig{Enabled: true, Provider: "epic", Epic: config.EpicConfig{AccountID: "polling-1"}},
		CredentialsSupplier: creds,
		Logger:              logger,
	})
	if err != nil {
		t.Fatalf("failed to create rank client: %v", err)
	}
	defer rankClient.Close()

	// Invalidate next skills query with connection drop
	psyServer.SetDisconnectNextSkills(1)

	// PsyNetRankClient has transparent reconnect logic: it should catch EOF/closed, reconnect, and retry
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	skills, err := rankClient.GetPlayersSkills(ctx, []rlapi.PlayerID{rlapi.PlayerID(targetPID)})
	if err != nil {
		t.Fatalf("transparent reconnect failed on connection drop: %v", err)
	}
	if len(skills) != 1 || len(skills[0].Skills) != 1 {
		t.Errorf("expected 1 skill result after reconnect retry, got: %+v", skills)
	}
	if psyServer.GetSkillsRequestCount() < 2 {
		t.Errorf("expected at least 2 requests (initial drop + retry), got %d", psyServer.GetSkillsRequestCount())
	}
}
