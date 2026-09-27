package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rlapi"

	"github.com/dank/rl-api-utils/internal/ballchasing"
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
// Thread-Safe Mock Skill Fetcher for Adversarial Tests
// ----------------------------------------------------------------------------

type advSkillFetcher struct {
	mu        sync.Mutex
	skillsMap map[rlapi.PlayerID][]rlapi.Skill
	err       error
	calls     [][]rlapi.PlayerID
	closed    bool
}

func newAdvSkillFetcher() *advSkillFetcher {
	return &advSkillFetcher{
		skillsMap: make(map[rlapi.PlayerID][]rlapi.Skill),
	}
}

func (f *advSkillFetcher) GetPlayersSkills(ctx context.Context, playerIDs []rlapi.PlayerID) ([]rlapi.PlayerWithSkills, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return nil, playertrack.ErrRankClientClosed
	}

	f.calls = append(f.calls, playerIDs)
	if f.err != nil {
		return nil, f.err
	}

	var res []rlapi.PlayerWithSkills
	for _, pid := range playerIDs {
		if skills, ok := f.skillsMap[pid]; ok {
			res = append(res, rlapi.PlayerWithSkills{
				PlayerID: pid,
				Skills:   skills,
			})
		}
	}
	return res, nil
}

func (f *advSkillFetcher) IsEnabled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.closed
}

func (f *advSkillFetcher) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *advSkillFetcher) GetCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *advSkillFetcher) SetError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *advSkillFetcher) SetSkill(pid rlapi.PlayerID, skills []rlapi.Skill) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.skillsMap[pid] = skills
}

func (f *advSkillFetcher) HasQueried(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, call := range f.calls {
		for _, pid := range call {
			if strings.HasPrefix(string(pid), prefix) {
				return true
			}
		}
	}
	return false
}

// ----------------------------------------------------------------------------
// SECTION 1: Error 67 Anti-Collision & Degradation Verification
// ----------------------------------------------------------------------------

func TestTier5_PT_Adv1_Error67_EpicCollision_DegradesToNoOp_ReplaySyncUnimpeded(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "collision_epic.db")
	replayDir := filepath.Join(tempDir, "replays")

	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to init SQLiteStore: %v", err)
	}
	defer store.Close()

	// 1. Mock Servers
	psyServer := testutil.NewMockPsyNetServer()
	defer psyServer.Close()

	cdn := testutil.NewMockCDNServer()
	defer cdn.Close()

	bc := testutil.NewMockBallchasingServer()
	defer bc.Close()
	bc.SetExpectedToken("bc-valid-key")

	// Pre-seed a match in PsyNet to test replay sync
	matchGUID := "match-collision-001"
	replayURL := cdn.ReplayURL(matchGUID)
	psyServer.SetMatches([]testutil.MockMatchEntry{
		testutil.NewMockMatchEntry(matchGUID, replayURL, "DFHStadium_P", 11),
	})

	// Redirect psynet.gg requests to mock server
	origTransport := http.DefaultTransport
	http.DefaultTransport = &redirectPsyNetTransport{
		targetURL: psyServer.URL(),
		base:      origTransport,
	}
	defer func() { http.DefaultTransport = origTransport }()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// 2. Configure Colliding Credentials (identical Epic AccountID & RefreshToken)
	primaryAuth := config.AuthConfig{
		Provider: "epic",
		Epic: config.EpicConfig{
			AccountID:    "epic-main-player-111",
			RefreshToken: "tok-primary-secret",
		},
	}
	pollingAuth := config.PollingAuthConfig{
		Enabled:  true,
		Provider: "epic",
		Epic: config.EpicConfig{
			AccountID:    "epic-main-player-111",
			RefreshToken: "tok-primary-secret",
		},
	}

	// Verify collision detection function directly
	if err := playertrack.CheckCredentialCollision(primaryAuth, pollingAuth); err == nil {
		t.Fatalf("expected CheckCredentialCollision to detect collision, got nil")
	}

	// 3. NewRankClient must catch collision and degrade to NoOpRankClient without error
	rankClient, err := playertrack.NewRankClient(playertrack.RankClientConfig{
		PrimaryAuth: primaryAuth,
		PollingAuth: pollingAuth,
		Logger:      logger,
	})
	if err != nil {
		t.Fatalf("expected NewRankClient to degrade gracefully, got error: %v", err)
	}
	defer rankClient.Close()

	if rankClient.IsEnabled() {
		t.Errorf("expected NoOpRankClient to be disabled (IsEnabled=false)")
	}

	// 4. Initialize Tracker with degraded rank client
	trackerCfg := config.PlayerTrackingConfig{
		Enabled:        true,
		AutoFetchRanks: true,
		LocalPlayerID:  "Epic|epic-main-player-111|0",
	}
	tracker, err := playertrack.NewTracker(store, rankClient, trackerCfg, primaryAuth, playertrack.WithTrackerLogger(logger))
	if err != nil {
		t.Fatalf("failed to create Tracker: %v", err)
	}
	defer tracker.Close()

	// 5. Initialize Syncer Domain Engine with Primary Auth
	mockProvider := newStaticMatchProvider([]psynet.DiscoveredMatch{
		{
			MatchGUID:            matchGUID,
			RecordStartTimestamp: time.Now().Unix(),
			MapName:              "DFHStadium_P",
			Playlist:             11,
			ReplayURL:            replayURL,
		},
	})
	downloader := psynet.NewDownloader()
	uploader, err := ballchasing.NewClient(ballchasing.ClientConfig{
		BaseURL: bc.URL(),
		APIKey:  "bc-valid-key",
	})
	if err != nil {
		t.Fatalf("failed to create ballchasing client: %v", err)
	}

	syncerEngine, err := syncer.New(store, mockProvider, downloader, uploader,
		syncer.WithReplayDir(replayDir),
		syncer.WithLogger(logger),
	)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}

	// 6. Push match telemetry into Tracker
	players := []statsapi.StatsPlayer{
		{Name: "MainPlayer", PrimaryId: "Epic|epic-main-player-111|0", TeamNum: 0},
		{Name: "Teammate", PrimaryId: "Steam|76561198000000002|0", TeamNum: 0},
		{Name: "Opponent", PrimaryId: "Epic|opp_001|0", TeamNum: 1},
	}
	_ = tracker.OnUpdateState(context.Background(), matchGUID, 11, players)
	winnerTeam := 0
	_ = tracker.OnMatchEnded(context.Background(), matchGUID, &winnerTeam)

	// 7. Execute Replay Synchronization Cycle
	stats, err := syncerEngine.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("syncer RunCycle failed: %v", err)
	}
	if stats.Downloaded != 1 || stats.Uploaded != 1 {
		t.Fatalf("expected 1 downloaded and 1 uploaded match, got: %+v", stats)
	}

	// 8. Assertions:
	// a) MockPsyNetServer received 0 Skills RPC queries (Error 67 kick prevented)
	if psyServer.GetSkillsRequestCount() != 0 {
		t.Errorf("expected 0 Skills RPC queries to MockPsyNetServer, got %d", psyServer.GetSkillsRequestCount())
	}

	// b) Store record is marked UPLOADED
	mRec, err := store.GetMatch(context.Background(), matchGUID)
	if err != nil || mRec.UploadStatus != storage.UploadUploaded {
		t.Errorf("expected match status UPLOADED, got record: %+v, err: %v", mRec, err)
	}

	// c) Player matchup stats recorded
	pRec, err := store.GetPlayerMatchup(context.Background(), "Steam|76561198000000002|0", 11)
	if err != nil || pRec.WinsAsTeammate != 1 {
		t.Errorf("expected 1 win as teammate, got record: %+v, err: %v", pRec, err)
	}
}

func TestTier5_PT_Adv1_Error67_SteamCollision_DegradesToNoOp(t *testing.T) {
	primaryAuth := config.AuthConfig{
		Provider: "steam",
		Steam: config.SteamConfig{
			SteamID64:     "76561198000000001",
			SessionTicket: "ticket-steam-main",
		},
	}
	pollingAuth := config.PollingAuthConfig{
		Enabled:  true,
		Provider: "steam",
		Steam: config.SteamConfig{
			SteamID64:     "76561198000000001",
			SessionTicket: "ticket-steam-main",
		},
	}

	err := playertrack.CheckCredentialCollision(primaryAuth, pollingAuth)
	if err == nil {
		t.Fatalf("expected Steam credential collision detection, got nil")
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	rankClient, err := playertrack.NewRankClient(playertrack.RankClientConfig{
		PrimaryAuth: primaryAuth,
		PollingAuth: pollingAuth,
		Logger:      logger,
	})
	if err != nil {
		t.Fatalf("expected graceful degradation to NoOpRankClient, got error: %v", err)
	}
	defer rankClient.Close()

	if rankClient.IsEnabled() {
		t.Errorf("expected NoOpRankClient to return IsEnabled=false")
	}

	// Verify querying skills on NoOpRankClient produces no network calls and returns empty/nil
	skills, err := rankClient.GetPlayersSkills(context.Background(), []rlapi.PlayerID{"Steam|76561198000000001|0"})
	if err != nil || skills != nil {
		t.Errorf("expected (nil, nil) from NoOpRankClient, got skills=%v, err=%v", skills, err)
	}
}

func TestTier5_PT_Adv1_Error67_NormalizationAndPaddingEdgeCases(t *testing.T) {
	cases := []struct {
		name        string
		primary     config.AuthConfig
		polling     config.PollingAuthConfig
		expectError bool
	}{
		{
			name: "Whitespace padding in Epic account ID",
			primary: config.AuthConfig{
				Provider: "epic",
				Epic:     config.EpicConfig{AccountID: "epic-user-001"},
			},
			polling: config.PollingAuthConfig{
				Enabled:  true,
				Provider: "epic",
				Epic:     config.EpicConfig{AccountID: "   epic-user-001   "},
			},
			expectError: true,
		},
		{
			name: "Case insensitivity in Epic account ID",
			primary: config.AuthConfig{
				Provider: "epic",
				Epic:     config.EpicConfig{AccountID: "EPIC-ACCOUNT-GUID-XYZ"},
			},
			polling: config.PollingAuthConfig{
				Enabled:  true,
				Provider: "epic",
				Epic:     config.EpicConfig{AccountID: "epic-account-guid-xyz"},
			},
			expectError: true,
		},
		{
			name: "Different providers (Steam primary, Epic polling) -> Valid",
			primary: config.AuthConfig{
				Provider: "steam",
				Steam:    config.SteamConfig{SteamID64: "76561198000000001"},
			},
			polling: config.PollingAuthConfig{
				Enabled:  true,
				Provider: "epic",
				Epic:     config.EpicConfig{AccountID: "epic-secondary-account"},
			},
			expectError: false,
		},
		{
			name: "Different account IDs in same provider -> Valid",
			primary: config.AuthConfig{
				Provider: "epic",
				Epic:     config.EpicConfig{AccountID: "epic-main-id"},
			},
			polling: config.PollingAuthConfig{
				Enabled:  true,
				Provider: "epic",
				Epic:     config.EpicConfig{AccountID: "epic-alt-id"},
			},
			expectError: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := playertrack.CheckCredentialCollision(tc.primary, tc.polling)
			if tc.expectError && err == nil {
				t.Errorf("expected collision error, got nil")
			} else if !tc.expectError && err != nil {
				t.Errorf("expected no collision error, got: %v", err)
			}
		})
	}
}

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
// SECTION 3: Bot & Corrupt Telemetry Immunity
// ----------------------------------------------------------------------------

func TestTier5_PT_Adv3_BotImmunity_50BotsNoStoragePollution(t *testing.T) {
	tempDir := t.TempDir()
	store, _ := storage.NewSQLiteStore(filepath.Join(tempDir, "bots.db"))
	defer store.Close()

	fetcher := newAdvSkillFetcher()
	fetcher.SetSkill("Epic|human-player|0", []rlapi.Skill{{Playlist: 11, Tier: 16, Division: 2}})

	trackerCfg := config.PlayerTrackingConfig{
		Enabled:        true,
		AutoFetchRanks: true,
		LocalPlayerID:  "Epic|human-player|0",
	}
	tracker, _ := playertrack.NewTracker(store, fetcher, trackerCfg, config.AuthConfig{})
	defer tracker.Close()

	// Generate 1 human + 50 AI bots
	players := make([]statsapi.StatsPlayer, 0, 51)
	players = append(players, statsapi.StatsPlayer{
		Name:      "HumanHero",
		PrimaryId: "Epic|human-player|0",
		TeamNum:   0,
	})
	botNames := []string{"Maverick", "Goose", "Iceman", "Viper", "Slider", "Jester", "Hollywood", "Wolfman"}
	for i := 0; i < 50; i++ {
		players = append(players, statsapi.StatsPlayer{
			Name:      botNames[i%len(botNames)],
			PrimaryId: fmt.Sprintf("Unknown|0|%d", i),
			TeamNum:   1, // Opposing bots
		})
	}

	matchGUID := "bot-storm-guid"
	_ = tracker.OnUpdateState(context.Background(), matchGUID, 11, players)
	winnerTeam := 0
	_ = tracker.OnMatchEnded(context.Background(), matchGUID, &winnerTeam)

	// Verify current match identifies bots
	resp := tracker.GetCurrentMatch()
	if len(resp.Opponents) != 50 {
		t.Fatalf("expected 50 opponents, got %d", len(resp.Opponents))
	}
	for _, opp := range resp.Opponents {
		if !opp.IsBot {
			t.Errorf("expected bot flag true for %s", opp.PlayerID)
		}
	}

	// Verify fetcher received 0 queries for bots (only human)
	if fetcher.HasQueried("Unknown|0|") {
		t.Errorf("bot was queried for skill")
	}

	// Verify SQLite database has 0 bot rows in players table and player_matchups
	summaries, err := store.ListPlayerSummaries(context.Background(), 100, 0)
	if err != nil {
		t.Fatalf("ListPlayerSummaries failed: %v", err)
	}
	for _, s := range summaries {
		if strings.HasPrefix(s.PlayerID, "Unknown|0|") {
			t.Errorf("bot found in persistent storage: %s", s.PlayerID)
		}
	}
}

func TestTier5_PT_Adv3_CorruptTelemetry_SQLInjectionAndOversizedPayloads(t *testing.T) {
	tempDir := t.TempDir()
	store, _ := storage.NewSQLiteStore(filepath.Join(tempDir, "corrupt.db"))
	defer store.Close()

	trackerCfg := config.PlayerTrackingConfig{
		Enabled:        true,
		LocalPlayerID:  "Steam|valid_local|0",
	}
	tracker, _ := playertrack.NewTracker(store, playertrack.NewNoOpRankClient(), trackerCfg, config.AuthConfig{})
	defer tracker.Close()

	adversarialCases := []struct {
		name      string
		matchGUID string
		players   []statsapi.StatsPlayer
	}{
		{
			name:      "SQL Injection in GUID",
			matchGUID: "guid-'; DROP TABLE players; DROP TABLE player_matchups; --",
			players: []statsapi.StatsPlayer{
				{Name: "Player1", PrimaryId: "Steam|valid_local|0", TeamNum: 0},
				{Name: "Player2", PrimaryId: "Epic|opponent|0", TeamNum: 1},
			},
		},
		{
			name:      "Oversized 65KB GUID",
			matchGUID: strings.Repeat("A", 65536),
			players: []statsapi.StatsPlayer{
				{Name: "Player1", PrimaryId: "Steam|valid_local|0", TeamNum: 0},
			},
		},
		{
			name:      "Empty and Whitespace GUID",
			matchGUID: "     ",
			players: []statsapi.StatsPlayer{
				{Name: "Player1", PrimaryId: "Steam|valid_local|0", TeamNum: 0},
			},
		},
		{
			name:      "Corrupted and Binary Primary IDs",
			matchGUID: "corrupt-ids-01",
			players: []statsapi.StatsPlayer{
				{Name: "EmptyID", PrimaryId: "", TeamNum: 0},
				{Name: "SinglePipe", PrimaryId: "|||", TeamNum: 0},
				{Name: "NoPipes", PrimaryId: "Steam", TeamNum: 0},
				{Name: "BinaryNoise", PrimaryId: "\x00\x01\x02\xff", TeamNum: 1},
				{Name: "InvalidSplitscreen", PrimaryId: "Steam|acc|not_a_number", TeamNum: 1},
			},
		},
		{
			name:      "Out-of-bounds teams",
			matchGUID: "oob-teams-01",
			players: []statsapi.StatsPlayer{
				{Name: "NegTeam", PrimaryId: "Steam|neg|0", TeamNum: -1},
				{Name: "HugeTeam", PrimaryId: "Steam|huge|0", TeamNum: 9999},
			},
		},
	}

	for _, tc := range adversarialCases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic handling corrupt telemetry: %v", r)
				}
			}()

			_ = tracker.OnUpdateState(context.Background(), tc.matchGUID, 11, tc.players)
			winner := 0
			_ = tracker.OnMatchEnded(context.Background(), tc.matchGUID, &winner)
		})
	}

	// Verify database is completely intact and healthy after all adversarial payloads
	validGUID := "valid-aftermath-match"
	validPlayers := []statsapi.StatsPlayer{
		{Name: "ValidLocal", PrimaryId: "Steam|valid_local|0", TeamNum: 0},
		{Name: "ValidTeammate", PrimaryId: "Steam|valid_tm|0", TeamNum: 0},
	}
	_ = tracker.OnUpdateState(context.Background(), validGUID, 11, validPlayers)
	winner := 0
	_ = tracker.OnMatchEnded(context.Background(), validGUID, &winner)

	pMatchup, err := store.GetPlayerMatchup(context.Background(), "Steam|valid_tm|0", 11)
	if err != nil || pMatchup.WinsAsTeammate != 1 {
		t.Fatalf("database corrupted by prior payloads; unable to track valid match: %v, matchup: %+v", err, pMatchup)
	}
}

func TestTier5_PT_Adv3_MixedLobby_ValidPlayerPreservation(t *testing.T) {
	tempDir := t.TempDir()
	store, _ := storage.NewSQLiteStore(filepath.Join(tempDir, "mixed.db"))
	defer store.Close()

	trackerCfg := config.PlayerTrackingConfig{
		Enabled:        true,
		LocalPlayerID:  "Epic|valid-local|0",
	}
	tracker, _ := playertrack.NewTracker(store, playertrack.NewNoOpRankClient(), trackerCfg, config.AuthConfig{})
	defer tracker.Close()

	// 1 valid local, 1 corrupt player, 1 valid teammate, 1 bot, 1 valid opponent
	players := []statsapi.StatsPlayer{
		{Name: "ValidLocal", PrimaryId: "Epic|valid-local|0", TeamNum: 0},
		{Name: "Corrupted", PrimaryId: "|||", TeamNum: -1},
		{Name: "ValidTeammate", PrimaryId: "Steam|76561198000000002|0", TeamNum: 0},
		{Name: "BotOpponent", PrimaryId: "Unknown|0|0", TeamNum: 1},
		{Name: "ValidOpponent", PrimaryId: "Epic|valid-opp|0", TeamNum: 1},
	}

	_ = tracker.OnUpdateState(context.Background(), "mixed-match-1", 11, players)
	winner := 0
	_ = tracker.OnMatchEnded(context.Background(), "mixed-match-1", &winner)

	// Teammate check
	tmMatchup, err := store.GetPlayerMatchup(context.Background(), "Steam|76561198000000002|0", 11)
	if err != nil || tmMatchup.WinsAsTeammate != 1 {
		t.Errorf("valid teammate was not recorded: %+v, err: %v", tmMatchup, err)
	}

	// Opponent check
	oppMatchup, err := store.GetPlayerMatchup(context.Background(), "Epic|valid-opp|0", 11)
	if err != nil || oppMatchup.WinsAsOpponent != 1 {
		t.Errorf("valid opponent was not recorded: %+v, err: %v", oppMatchup, err)
	}
}

func TestTier5_PT_Adv3_MalformedPrimaryIDsAndNegativeTeams(t *testing.T) {
	tempDir := t.TempDir()
	store, _ := storage.NewSQLiteStore(filepath.Join(tempDir, "malformed.db"))
	defer store.Close()

	trackerCfg := config.PlayerTrackingConfig{
		Enabled:       true,
		LocalPlayerID: "Steam|local|0",
	}
	tracker, _ := playertrack.NewTracker(store, playertrack.NewNoOpRankClient(), trackerCfg, config.AuthConfig{})
	defer tracker.Close()

	players := []statsapi.StatsPlayer{
		{Name: "Local", PrimaryId: "Steam|local|0", TeamNum: 0},
		{Name: "NegTeam", PrimaryId: "Epic|neg|0", TeamNum: -1},
		{Name: "HugeTeam", PrimaryId: "Epic|huge|0", TeamNum: 255},
	}
	_ = tracker.OnUpdateState(context.Background(), "malformed-match", 11, players)

	resp := tracker.GetCurrentMatch()
	// Negative team and team 255 must be assigned to spectators, never teammates or opponents
	if len(resp.Teammates) != 0 || len(resp.Opponents) != 0 {
		t.Errorf("malformed teams leaked into teammates or opponents: %+v", resp)
	}
	if len(resp.Spectators) != 2 {
		t.Errorf("expected 2 spectators for non-0/1 teams, got %d", len(resp.Spectators))
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
		PrimaryAuth: config.AuthConfig{Provider: "epic", Epic: config.EpicConfig{AccountID: "main-1"}},
		PollingAuth: config.PollingAuthConfig{Enabled: true, Provider: "epic", Epic: config.EpicConfig{AccountID: "polling-1"}},
		CredentialsSupplier: creds,
		Logger:      logger,
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

// ----------------------------------------------------------------------------
// SECTION 5: Backward Compatibility Verification for 385+ Replay-Sync Tests
// ----------------------------------------------------------------------------

func TestTier5_PT_Adv5_BackwardCompatibility_AllReplaySyncPipelines(t *testing.T) {
	tempDir := t.TempDir()
	store, _ := storage.NewSQLiteStore(filepath.Join(tempDir, "compat_sync.db"))
	defer store.Close()

	cdn := testutil.NewMockCDNServer()
	defer cdn.Close()

	bc := testutil.NewMockBallchasingServer()
	defer bc.Close()
	bc.SetExpectedToken("bc-compat-token")

	// Match 1: 201 Created
	m1 := "compat-match-001"
	// Match 2: 409 Duplicate
	m2 := "compat-match-002"
	bc.SetDuplicateGUID(m2, "bc-dup-id-123")
	// Match 3: 429 Rate Limit (1 failure with retry-after 0)
	m3 := "compat-match-003"
	bc.SimulateRateLimit(1, 0)

	mockProvider := newStaticMatchProvider([]psynet.DiscoveredMatch{
		{
			MatchGUID:            m1,
			RecordStartTimestamp: time.Now().Unix(),
			MapName:              "DFHStadium_P",
			Playlist:             11,
			ReplayURL:            cdn.ReplayURL(m1),
		},
		{
			MatchGUID:            m2,
			RecordStartTimestamp: time.Now().Unix(),
			MapName:              "Wasteland_P",
			Playlist:             11,
			ReplayURL:            cdn.ReplayURL(m2),
		},
		{
			MatchGUID:            m3,
			RecordStartTimestamp: time.Now().Unix(),
			MapName:              "Utopia_P",
			Playlist:             11,
			ReplayURL:            cdn.ReplayURL(m3),
		},
	})
	downloader := psynet.NewDownloader()
	uploader, _ := ballchasing.NewClient(ballchasing.ClientConfig{
		BaseURL: bc.URL(),
		APIKey:  "bc-compat-token",
	},
		ballchasing.WithMaxRetries(3),
		ballchasing.WithBaseBackoff(10*time.Millisecond),
	)

	replayDir := filepath.Join(tempDir, "replays")
	syncerEngine, err := syncer.New(store, mockProvider, downloader, uploader,
		syncer.WithReplayDir(replayDir),
	)
	if err != nil {
		t.Fatalf("failed to create syncer: %v", err)
	}

	// Execute synchronization cycle
	stats, err := syncerEngine.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("syncer RunCycle failed: %v", err)
	}

	if stats.Discovered != 3 || stats.Downloaded != 3 {
		t.Errorf("expected 3 discovered and downloaded, got %+v", stats)
	}

	// Verify statuses in store:
	// m1: UPLOADED
	r1, _ := store.GetMatch(context.Background(), m1)
	if r1.UploadStatus != storage.UploadUploaded {
		t.Errorf("m1 expected UPLOADED, got %s", r1.UploadStatus)
	}

	// m2: DUPLICATE
	r2, _ := store.GetMatch(context.Background(), m2)
	if r2.UploadStatus != storage.UploadDuplicate {
		t.Errorf("m2 expected DUPLICATE, got %s", r2.UploadStatus)
	}

	// m3: UPLOADED after retry
	r3, _ := store.GetMatch(context.Background(), m3)
	if r3.UploadStatus != storage.UploadUploaded {
		t.Errorf("m3 expected UPLOADED, got %s", r3.UploadStatus)
	}
}

func TestTier5_PT_Adv5_BackwardCompatibility_CLIFlagsAndHTTPTrigger(t *testing.T) {
	tempDir := t.TempDir()
	store, _ := storage.NewSQLiteStore(filepath.Join(tempDir, "flags.db"))
	defer store.Close()

	testSyncer := &testSyncerStub{}
	cfg := &config.Config{
		Sync: config.SyncConfig{
			Once:   true,
			DryRun: true,
		},
		StatsAPI: config.StatsAPIConfig{
			Enabled:         true,
			HTTPTriggerPort: 0,
		},
	}

	d, err := daemon.New(testSyncer, cfg, daemon.WithStateStore(store))
	if err != nil {
		t.Fatalf("daemon.New failed: %v", err)
	}

	// 1. Single-Run Mode (--once)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = d.Start(ctx)
	if err != nil {
		t.Fatalf("daemon.Start in --once mode failed: %v", err)
	}
	if testSyncer.cycles != 1 {
		t.Errorf("expected exactly 1 syncer cycle in --once mode, got %d", testSyncer.cycles)
	}

	// 2. HTTP /sync trigger endpoint
	req := httptest.NewRequest(http.MethodPost, "/sync", nil)
	rec := httptest.NewRecorder()
	d.Handler(context.Background()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from /sync, got %d", rec.Code)
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != "sync_triggered" {
		t.Errorf("expected status 'sync_triggered', got %q", resp["status"])
	}
}

func TestTier5_PT_Adv5_DualBackendParity_SQLiteAndJSON(t *testing.T) {
	backends := []struct {
		name    string
		initFn  func(dir string) (storage.StateStore, error)
	}{
		{
			name: "SQLiteStore",
			initFn: func(dir string) (storage.StateStore, error) {
				return storage.NewSQLiteStore(filepath.Join(dir, "parity.db"))
			},
		},
		{
			name: "JSONStore",
			initFn: func(dir string) (storage.StateStore, error) {
				return storage.NewJSONStore(filepath.Join(dir, "parity.json"))
			},
		},
	}

	for _, b := range backends {
		t.Run(b.name, func(t *testing.T) {
			tempDir := t.TempDir()
			store, err := b.initFn(tempDir)
			if err != nil {
				t.Fatalf("failed to init %s: %v", b.name, err)
			}
			defer store.Close()

			cdn := testutil.NewMockCDNServer()
			defer cdn.Close()

			bc := testutil.NewMockBallchasingServer()
			defer bc.Close()
			bc.SetExpectedToken("bc-key")

			matchGUID := "parity-match-101"
			replayURL := cdn.ReplayURL(matchGUID)

			mockProvider := newStaticMatchProvider([]psynet.DiscoveredMatch{
				{
					MatchGUID:            matchGUID,
					RecordStartTimestamp: time.Now().Unix(),
					MapName:              "DFHStadium_P",
					Playlist:             11,
					ReplayURL:            replayURL,
				},
			})
			downloader := psynet.NewDownloader()
			uploader, _ := ballchasing.NewClient(ballchasing.ClientConfig{
				BaseURL: bc.URL(),
				APIKey:  "bc-key",
			})

			engine, err := syncer.New(store, mockProvider, downloader, uploader,
				syncer.WithReplayDir(filepath.Join(tempDir, "replays")),
			)
			if err != nil {
				t.Fatalf("syncer creation failed on %s: %v", b.name, err)
			}

			stats, err := engine.RunCycle(context.Background())
			if err != nil || stats.Uploaded != 1 {
				t.Errorf("%s failed RunCycle: stats=%+v, err=%v", b.name, stats, err)
			}

			rec, err := store.GetMatch(context.Background(), matchGUID)
			if err != nil || rec.UploadStatus != storage.UploadUploaded {
				t.Errorf("%s match state mismatch: %+v, err: %v", b.name, rec, err)
			}
		})
	}
}
