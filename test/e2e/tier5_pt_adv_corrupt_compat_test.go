package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
		Enabled:       true,
		LocalPlayerID: "Steam|valid_local|0",
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
		Enabled:       true,
		LocalPlayerID: "Epic|valid-local|0",
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
		name   string
		initFn func(dir string) (storage.StateStore, error)
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
