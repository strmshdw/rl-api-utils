package invariants

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/domain"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/psynet"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/syncer"
	"github.com/dank/rl-api-utils/internal/testutil"
)

type noopDaemonSyncer struct{}

func (noopDaemonSyncer) RunCycle(context.Context) (*syncer.SyncStats, error) {
	return &syncer.SyncStats{}, nil
}

func TestInvariant_3744274_PollingSentinelExcludedFromLocalPlayerIDs(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "inv_3744274.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	// Seed primary local accounts alongside *_polling accounts that must be excluded.
	if err := store.SaveAuthState(ctx, "epic", "rt-primary", "epic-primary-acc", "MainDriver"); err != nil {
		t.Fatalf("SaveAuthState epic: %v", err)
	}
	if err := store.SaveAuthState(ctx, "epic_polling", "rt-poll", "Epic|_polling|0", "_polling"); err != nil {
		t.Fatalf("SaveAuthState epic_polling: %v", err)
	}
	if err := store.SaveAuthState(ctx, "steam_polling", "rt-spoll", "Steam|polling_acc|0", "PollGhost"); err != nil {
		t.Fatalf("SaveAuthState steam_polling: %v", err)
	}

	now := time.Now().UTC()
	win := 0
	for i, guid := range []string{"m-3744-1", "m-3744-2"} {
		_ = store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
			MatchGUID:   guid,
			Playlist:    11,
			BlueScore:   3,
			OrangeScore: 1,
			WinnerTeam:  &win,
			Date:        now.Add(time.Duration(i) * time.Minute).Format(time.RFC3339),
		})
		_ = store.SaveMatchPlayers(ctx, guid, []*storage.MatchPlayerRecord{
			{MatchGUID: guid, PlayerID: "Steam|76561198000000777|0", TeamNum: 0, IsLocal: true, Score: 500},
		})
	}

	ids, err := store.GetLocalPlayerIDs(ctx)
	if err != nil {
		t.Fatalf("GetLocalPlayerIDs: %v", err)
	}
	if len(ids) < 2 {
		t.Fatalf("expected primary steam & epic local IDs, got %v", ids)
	}
	for _, id := range ids {
		if id == "Epic|_polling|0" || id == "Steam|polling_acc|0" {
			t.Fatalf("3744274 regression: *_polling account %q leaked into GetLocalPlayerIDs: %v", id, ids)
		}
	}
	if ids[0] != "Steam|76561198000000777|0" {
		t.Fatalf("expected highest-frequency local match player first, got %v", ids)
	}
}

func TestInvariant_MeAndYouHTTPLocalPlayerResolution(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "inv_me_you.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	if err := store.SaveAuthState(ctx, "epic", "rt-me", "epic-local-99", "AceDriver"); err != nil {
		t.Fatalf("SaveAuthState: %v", err)
	}
	now := time.Now().UTC()
	win := 0
	if err := store.SyncReplayStats(ctx, &storage.ReplayMatchStats{
		MatchGUID:   "m-me-you-1",
		Playlist:    11,
		BlueScore:   4,
		OrangeScore: 2,
		WinnerTeam:  &win,
		Date:        now.Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("SyncReplayStats: %v", err)
	}
	if err := store.SaveMatchPlayers(ctx, "m-me-you-1", []*storage.MatchPlayerRecord{
		{MatchGUID: "m-me-you-1", PlayerID: "Epic|epic-local-99|0", TeamNum: 0, IsLocal: true, Score: 620, Goals: 2},
		{MatchGUID: "m-me-you-1", PlayerID: "Steam|76561198000000888|0", TeamNum: 1, IsLocal: false, Score: 310, Goals: 1},
	}); err != nil {
		t.Fatalf("SaveMatchPlayers: %v", err)
	}
	_ = store.RecordMMR(ctx, &storage.MMRHistoryRecord{
		RecordedAt: now,
		MatchGUID:  "m-me-you-1",
		PlaylistID: 11,
		MMR:        1512,
		Tier:       18,
		Division:   2,
	})

	cfg := config.NewDefaultConfig()
	cfg.Logging.Level = "error"
	d, err := daemon.New(
		noopDaemonSyncer{},
		cfg,
		daemon.WithStateStore(store),
		daemon.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	)
	if err != nil {
		t.Fatalf("daemon.New: %v", err)
	}
	handler := d.Handler(ctx)

	for _, alias := range []string{"me", "you"} {
		t.Run("GET_/api/players/"+alias, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/players/"+alias, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET /api/players/%s = %d: %s", alias, rec.Code, rec.Body.String())
			}
			var detail daemon.PlayerDetailResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
				t.Fatalf("unmarshal detail: %v", err)
			}
			if detail.PlayerID != "Epic|epic-local-99|0" {
				t.Fatalf("alias %q resolved PlayerID=%q, want Epic|epic-local-99|0", alias, detail.PlayerID)
			}
			if len(detail.Matchups) != 1 || detail.Matchups[0].WinsAsTeammate != 1 {
				t.Fatalf("expected 1 win in synthesized local dossier matchups, got %+v", detail.Matchups)
			}
		})
	}
}

func TestInvariant_DomainIdentityNormalizationAndCrossID(t *testing.T) {
	ctx := context.Background()

	// Canonical domain parsing across platforms
	cases := []struct {
		raw       string
		wantPlat  string
		wantID    string
		wantSplit int
		wantErr   bool
	}{
		{"Steam|76561198012345678|0", "Steam", "76561198012345678", 0, false},
		{"epic|abcdef123456|0", "epic", "abcdef123456", 0, false},
		{"PS4|998877|1", "PS4", "998877", 1, false},
		{"Xbox|112233|0", "Xbox", "112233", 0, false},
		{"Switch|445566|0", "Switch", "445566", 0, false},
		{"invalid-no-pipes", "", "", 0, true},
		{"", "", "", 0, true},
	}
	for _, tc := range cases {
		parsed, err := domain.ParsePrimaryID(tc.raw)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParsePrimaryID(%q) expected error, got %+v", tc.raw, parsed)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParsePrimaryID(%q) unexpected error: %v", tc.raw, err)
			continue
		}
		if parsed.Platform != tc.wantPlat || parsed.AccountID != tc.wantID || parsed.Splitscreen != tc.wantSplit {
			t.Errorf("ParsePrimaryID(%q) = %+v, want (%s, %s, %d)", tc.raw, parsed, tc.wantPlat, tc.wantID, tc.wantSplit)
		}
	}

	if !domain.IsBotID("Unknown|0|0") || !domain.IsBotID("Unknown|0|1") {
		t.Errorf("expected IsBotID=true for bot identifiers")
	}
	if domain.IsBotID("Steam|76561198012345678|0") {
		t.Errorf("expected IsBotID=false for valid Steam player")
	}
	if !domain.CanonicalLocalAlias("me") || !domain.CanonicalLocalAlias("YOU") || domain.CanonicalLocalAlias("other") {
		t.Errorf("unexpected CanonicalLocalAlias behavior")
	}
	if got := domain.FormatCompositeID("Steam", "76561198012345678"); got != "Steam|76561198012345678|0" {
		t.Errorf("FormatCompositeID = %q, want Steam|76561198012345678|0", got)
	}
	if norm := domain.NormalizeID("  Epic|ABC123|0  "); norm != "epic|abc123|0" {
		t.Errorf("NormalizeID = %q, want epic|abc123|0", norm)
	}

	// Cross-ID resolution in storage across Steam/Epic accounts and raw online IDs
	store, err := storage.NewSQLiteStore(filepath.Join(t.TempDir(), "inv_cross_id.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	if err := store.UpsertPlayer(ctx, &storage.PlayerRecord{
		PlayerID:    "Steam|76561198012345678|0",
		Platform:    "Steam",
		PlayerName:  "CrossPlatformAce",
		FirstSeenAt: now,
		LastSeenAt:  now,
	}); err != nil {
		t.Fatalf("UpsertPlayer Steam: %v", err)
	}
	if err := store.UpsertPlayer(ctx, &storage.PlayerRecord{
		PlayerID:    "Epic|epic-hex-999|0",
		Platform:    "Epic",
		PlayerName:  "EpicRival",
		FirstSeenAt: now,
		LastSeenAt:  now,
	}); err != nil {
		t.Fatalf("UpsertPlayer Epic: %v", err)
	}

	byRawSteam, err := store.GetPlayer(ctx, "76561198012345678")
	if err != nil || byRawSteam.PlayerID != "Steam|76561198012345678|0" {
		t.Fatalf("GetPlayer by raw Steam64 = (%+v, %v)", byRawSteam, err)
	}
	byRawEpic, err := store.GetPlayer(ctx, "epic-hex-999")
	if err != nil || byRawEpic.PlayerID != "Epic|epic-hex-999|0" {
		t.Fatalf("GetPlayer by raw Epic ID = (%+v, %v)", byRawEpic, err)
	}
	byName, err := store.GetPlayer(ctx, "crossplatformace")
	if err != nil || byName.PlayerID != "Steam|76561198012345678|0" {
		t.Fatalf("GetPlayer by lowercase name = (%+v, %v)", byName, err)
	}
}

type invCollidingProvider struct {
	matches []psynet.DiscoveredMatch
}

func (p *invCollidingProvider) GetRecentMatches(context.Context) ([]psynet.DiscoveredMatch, error) {
	return p.matches, nil
}
func (p *invCollidingProvider) Close() error { return nil }

func TestInvariant_Error67_CheckCredentialCollisionAndDegradation(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	primaryEpic := config.AuthConfig{
		Provider: "epic",
		Epic:     config.EpicConfig{AccountID: "epic-acct-1", RefreshToken: "rt-shared-secret"},
	}
	collidingPolling := config.PollingAuthConfig{
		Enabled:  true,
		Provider: "epic",
		Epic:     config.EpicConfig{AccountID: "  EPIC-ACCT-1 ", RefreshToken: "rt-other"},
	}
	if err := playertrack.CheckCredentialCollision(primaryEpic, collidingPolling); !errors.Is(err, playertrack.ErrCredentialCollision) {
		t.Fatalf("expected ErrCredentialCollision for Epic collision, got %v", err)
	}

	steamPrimary := config.AuthConfig{
		Provider: "steam",
		Steam:    config.SteamConfig{SteamID64: "76561198000000123", SessionTicket: "tkt-1"},
	}
	steamPolling := config.PollingAuthConfig{
		Enabled:  true,
		Provider: "steam",
		Steam:    config.SteamConfig{SteamID64: "76561198000000123", SessionTicket: "tkt-2"},
	}
	if err := playertrack.CheckCredentialCollision(steamPrimary, steamPolling); !errors.Is(err, playertrack.ErrCredentialCollision) {
		t.Fatalf("expected ErrCredentialCollision for Steam collision, got %v", err)
	}

	// NewRankClient catches Error 67 collision and degrades cleanly to NoOpRankClient.
	rankClient, err := playertrack.NewRankClient(playertrack.RankClientConfig{
		PrimaryAuth: primaryEpic,
		PollingAuth: collidingPolling,
		Logger:      logger,
	})
	if err != nil {
		t.Fatalf("NewRankClient returned error on collision instead of degrading: %v", err)
	}
	defer rankClient.Close()
	if rankClient.IsEnabled() {
		t.Fatalf("expected degraded NoOpRankClient.IsEnabled() == false")
	}

	// Tracker + Syncer replay pipeline continue operating cleanly with degraded rank client.
	tempDir := t.TempDir()
	store, err := storage.NewSQLiteStore(filepath.Join(tempDir, "inv_e67.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	tracker, err := playertrack.NewTracker(
		store,
		rankClient,
		config.PlayerTrackingConfig{Enabled: true, AutoFetchRanks: true, LocalPlayerID: "Epic|epic-acct-1|0"},
		primaryEpic,
		playertrack.WithTrackerLogger(logger),
	)
	if err != nil {
		t.Fatalf("NewTracker: %v", err)
	}
	defer tracker.Close()

	cdn := testutil.NewMockCDNServer()
	defer cdn.Close()
	bc := testutil.NewMockBallchasingServer()
	defer bc.Close()
	bc.SetExpectedToken("bc-test-key")

	guid := "m-e67-sync-1"
	provider := &invCollidingProvider{
		matches: []psynet.DiscoveredMatch{{MatchGUID: guid, ReplayURL: cdn.ReplayURL(guid), Playlist: 11, RecordStartTimestamp: time.Now().UTC().Unix()}},
	}
	downloader := psynet.NewDownloader()
	uploader, err := ballchasing.NewClient(ballchasing.ClientConfig{APIKey: "bc-test-key", BaseURL: bc.URL()})
	if err != nil {
		t.Fatalf("ballchasing.NewClient: %v", err)
	}
	s, err := syncer.New(store, provider, downloader, uploader, syncer.WithReplayDir(filepath.Join(tempDir, "replays")), syncer.WithRateInterval(0))
	if err != nil {
		t.Fatalf("syncer.New: %v", err)
	}
	stats, err := s.RunCycle(ctx)
	if err != nil {
		t.Fatalf("RunCycle with Error 67 degraded tracker failed: %v", err)
	}
	if stats.UploadedCount != 1 {
		t.Fatalf("UploadedCount = %d, want 1", stats.UploadedCount)
	}
	rec, err := store.GetMatch(ctx, guid)
	if err != nil || rec.UploadStatus != storage.UploadUploaded {
		t.Fatalf("expected UPLOADED match despite Error 67 rank degradation, got %+v (err=%v)", rec, err)
	}
}
