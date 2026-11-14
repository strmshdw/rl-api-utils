package e2e

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dank/rlapi"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/config"
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
