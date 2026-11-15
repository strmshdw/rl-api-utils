package daemon_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/contracts"
	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
)

func TestStampingConfigEndpointsAndLiveScoutingPatch(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "stamping_api.db")
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	if err := os.WriteFile(cfgPath, []byte("auth:\n  provider: epic\nballchasing:\n  api_key: test-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	localID := "Epic|local_me_01|0"
	oppID := "Steam|opp_live_01|0"
	now := time.Now().UTC()
	_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
		PlayerID:    oppID,
		Platform:    "Steam",
		PlayerName:  "LiveOpp",
		RanksJSON:   "{}",
		FirstSeenAt: now,
		LastSeenAt:  now,
	})

	cfg := makeTestConfig(5*time.Minute, false)
	cfg.ConfigPath = cfgPath
	cfg.PlayerTracking.LocalPlayerID = localID

	tracker, err := playertrack.NewTracker(store, playertrack.NewMockSkillFetcher(), cfg.PlayerTracking, cfg.Auth)
	if err != nil {
		t.Fatal(err)
	}
	defer tracker.Close()

	_ = tracker.OnUpdateState(ctx, "live-guid-99", 11, []statsapi.StatsPlayer{
		{PrimaryId: localID, Name: "Me", TeamNum: 0, Score: 250},
		{PrimaryId: oppID, Name: "LiveOpp", TeamNum: 1, Score: 320},
	})

	d, err := daemon.New(newMockSyncer(), cfg,
		daemon.WithPlayerTracker(tracker),
		daemon.WithStateStore(store),
	)
	if err != nil {
		t.Fatal(err)
	}

	// 1. GET /api/stamping/config returns defaults
	recGet := executeTestRequest(d, http.MethodGet, "/api/stamping/config", nil)
	if recGet.Code != http.StatusOK {
		t.Fatalf("GET /api/stamping/config returned %d: %s", recGet.Code, recGet.Body.String())
	}
	var gotCfg contracts.StampingConfig
	if err := json.Unmarshal(recGet.Body.Bytes(), &gotCfg); err != nil {
		t.Fatalf("unmarshal stamping config failed: %v", err)
	}
	if gotCfg != config.DefaultStampingConfig() {
		t.Fatalf("GET /api/stamping/config = %+v, want defaults", gotCfg)
	}

	// 2. Reject cross-origin mutation (403)
	badReq := httptest.NewRequest(http.MethodPatch, "/api/stamping/config", bytes.NewReader([]byte(`{"carry_mmr_diff":150}`)))
	badReq.Header.Set("Origin", "https://evil.example.com")
	badRec := httptest.NewRecorder()
	d.Handler(ctx).ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-origin PATCH /api/stamping/config, got %d", badRec.Code)
	}

	// 3. PATCH /api/stamping/config updates runtime & persists to config.yaml
	patchBody := []byte(`{"carry_mmr_diff":135,"higher_mmr_diff":75,"relationship_min_games":4,"show_teammate_off_mode_peak":true}`)
	recPatch := executeTestRequest(d, http.MethodPatch, "/api/stamping/config", bytes.NewReader(patchBody))
	if recPatch.Code != http.StatusOK {
		t.Fatalf("PATCH /api/stamping/config returned %d: %s", recPatch.Code, recPatch.Body.String())
	}
	var patchedCfg contracts.StampingConfig
	if err := json.Unmarshal(recPatch.Body.Bytes(), &patchedCfg); err != nil {
		t.Fatal(err)
	}
	if patchedCfg.CarryMmrDiff != 135 || patchedCfg.HigherMmrDiff != 75 || patchedCfg.RelationshipMinGames != 4 || !patchedCfg.ShowTeammateOffModePeak {
		t.Fatalf("unexpected PATCH response: %+v", patchedCfg)
	}

	reloaded, err := config.Load(config.CLIFlags{ConfigPath: cfgPath})
	if err != nil {
		t.Fatalf("config.Load after PATCH failed: %v", err)
	}
	if reloaded.EffectiveStampingConfig().CarryMmrDiff != 135 || reloaded.EffectiveStampingConfig().RelationshipMinGames != 4 {
		t.Fatalf("stamping config did not persist to YAML: %+v", reloaded.EffectiveStampingConfig())
	}

	// 4. POST /api/stamping/config/reset restores defaults
	recReset := executeTestRequest(d, http.MethodPost, "/api/stamping/config/reset", nil)
	if recReset.Code != http.StatusOK {
		t.Fatalf("POST /api/stamping/config/reset returned %d: %s", recReset.Code, recReset.Body.String())
	}
	var resetCfg contracts.StampingConfig
	if err := json.Unmarshal(recReset.Body.Bytes(), &resetCfg); err != nil {
		t.Fatal(err)
	}
	if resetCfg != config.DefaultStampingConfig() {
		t.Fatalf("expected reset config to equal defaults, got %+v", resetCfg)
	}

	// 5. PATCH /api/players/{id} propagates Notes & Tags immediately to GET /api/current-match
	playerPatch := []byte(`{"notes":"Verified RLCS sub","tags":["verified","clutch"]}`)
	recPlayerPatch := executeTestRequest(d, http.MethodPatch, "/api/players/Steam%7Copp_live_01%7C0", bytes.NewReader(playerPatch))
	if recPlayerPatch.Code != http.StatusOK {
		t.Fatalf("PATCH /api/players/{id} returned %d: %s", recPlayerPatch.Code, recPlayerPatch.Body.String())
	}

	recLive := executeTestRequest(d, http.MethodGet, "/api/current-match", nil)
	if recLive.Code != http.StatusOK {
		t.Fatalf("GET /api/current-match returned %d", recLive.Code)
	}
	var liveSnap playertrack.CurrentMatchResponse
	if err := json.Unmarshal(recLive.Body.Bytes(), &liveSnap); err != nil {
		t.Fatal(err)
	}
	if len(liveSnap.Opponents) != 1 || liveSnap.Opponents[0].Notes != "Verified RLCS sub" ||
		len(liveSnap.Opponents[0].Tags) != 2 || liveSnap.Opponents[0].Tags[0] != "verified" {
		t.Fatalf("live match opponent did not receive immediate scouting update: %+v", liveSnap.Opponents)
	}
}

func TestNonLoopbackWebHost_AllowsSameOriginLANAndHostname_BlocksCSRFAndDNSRebinding(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "lan_origin.db")
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	if err := os.WriteFile(cfgPath, []byte("auth:\n  provider: epic\nballchasing:\n  api_key: test-key\nweb:\n  host: \"0.0.0.0\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	playerID := "Steam|lan_test_player|0"
	now := time.Now().UTC()
	_ = store.UpsertPlayer(ctx, &storage.PlayerRecord{
		PlayerID:    playerID,
		Platform:    "Steam",
		PlayerName:  "LANPlayer",
		RanksJSON:   "{}",
		FirstSeenAt: now,
		LastSeenAt:  now,
	})

	cfg := makeTestConfig(5*time.Minute, false)
	cfg.ConfigPath = cfgPath
	cfg.Web.Host = "0.0.0.0"

	d, err := daemon.New(newMockSyncer(), cfg, daemon.WithStateStore(store))
	if err != nil {
		t.Fatal(err)
	}
	handler := d.Handler(ctx)

	sendReq := func(method, path, host, origin, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "http://"+host+path, bytes.NewReader([]byte(body)))
		req.Host = host
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	// 1. Same-origin LAN IPv4 saves succeed across mutating endpoints when web.host == "0.0.0.0"
	lanEndpoints := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPatch, "/api/stamping/config", `{"carry_mmr_diff":125}`},
		{http.MethodPost, "/api/stamping/config/reset", `{}`},
		{http.MethodPatch, "/api/players/Steam%7Clan_test_player%7C0", `{"notes":"LAN save ok","tags":["clutch"]}`},
		{http.MethodPost, "/api/sync/config", `{"trigger_threshold":6}`},
		{http.MethodPost, "/api/session/reset", `{}`},
	}
	for _, ep := range lanEndpoints {
		rec := sendReq(ep.method, ep.path, "192.168.1.42:49125", "http://192.168.1.42:49125", ep.body)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for same-origin LAN %s %s, got %d: %s", ep.method, ep.path, rec.Code, rec.Body.String())
		}
	}

	// 2. Same-origin Tailscale IPv4 & IPv6 literals succeed
	for _, tc := range []struct {
		host   string
		origin string
	}{
		{"100.88.12.5:49125", "http://100.88.12.5:49125"},
		{"[fd7a:115c:a1e0::1]:49125", "http://[fd7a:115c:a1e0::1]:49125"},
	} {
		rec := sendReq(http.MethodPatch, "/api/stamping/config", tc.host, tc.origin, `{"higher_mmr_diff":80}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for same-origin IP literal %s, got %d: %s", tc.origin, rec.Code, rec.Body.String())
		}
	}

	// 3. Same-origin local machine hostname and .local mDNS hostname succeed
	if hostName, hErr := os.Hostname(); hErr == nil && hostName != "" {
		for _, h := range []string{hostName + ":49125", hostName + ".local:49125"} {
			rec := sendReq(http.MethodPatch, "/api/stamping/config", h, "http://"+h, `{"higher_mmr_diff":70}`)
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200 OK for same-origin machine hostname %s, got %d: %s", h, rec.Code, rec.Body.String())
			}
		}
	}

	// 4. Vite dev proxy over local interface IP succeeds when web.host is non-loopback
	if lanIP, ipErr := daemon.DiscoverLANIPv4(); ipErr == nil && lanIP != "" {
		rec := sendReq(http.MethodPatch, "/api/stamping/config", "localhost:49125", "http://"+lanIP+":5173", `{"higher_mmr_diff":65}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for local interface dev-proxy Origin %s, got %d: %s", lanIP, rec.Code, rec.Body.String())
		}
	}

	// 5. Cross-origin CSRF and DNS-rebinding attacks still return 403 Forbidden
	blocked := []struct {
		name   string
		host   string
		origin string
	}{
		{"cross-origin external site", "192.168.1.42:49125", "https://evil.example"},
		{"cross-origin neighbor LAN IP", "192.168.1.42:49125", "http://192.168.1.250:49125"},
		{"null sandboxed origin", "192.168.1.42:49125", "null"},
		{"DNS rebinding public domain", "rebind.attacker.example:49125", "http://rebind.attacker.example:49125"},
		{"DNS rebinding subdomain spoof", "my-pc.attacker.example:49125", "http://my-pc.attacker.example:49125"},
	}
	for _, tc := range blocked {
		rec := sendReq(http.MethodPatch, "/api/stamping/config", tc.host, tc.origin, `{"carry_mmr_diff":200}`)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s: expected 403 Forbidden for Host=%q Origin=%q, got %d: %s", tc.name, tc.host, tc.origin, rec.Code, rec.Body.String())
		}
	}
}
