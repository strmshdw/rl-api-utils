package daemon_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/daemon"
	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/session"
	"github.com/dank/rl-api-utils/internal/storage"
)

type testHarnessM3 struct {
	d       *daemon.Daemon
	store   storage.StateStore
	tracker *playertrack.Tracker
	sess    *session.SessionTracker
	handler http.Handler
	ctx     context.Context
}

func setupM3DaemonTest(t *testing.T) *testHarnessM3 {
	t.Helper()
	store := newTestStore(t, backendSQLite)

	cfg := makeTestConfig(5*time.Minute, false)
	cfg.Web.Enabled = true
	cfg.Web.Port = 49125

	buf := &bytes.Buffer{}
	logger := daemon.NewLogger(cfg.Logging, buf)
	sess := session.NewSessionTracker(session.WithLogger(logger))
	fetcher := playertrack.NewMockSkillFetcher()
	tracker := newTestTracker(t, store, fetcher)

	syncerMock := newMockSyncer()

	d, err := daemon.New(syncerMock, cfg,
		daemon.WithStateStore(store),
		daemon.WithPlayerTracker(tracker),
		daemon.WithSessionTracker(sess),
		daemon.WithLogger(logger),
	)
	if err != nil {
		t.Fatalf("failed to create daemon: %v", err)
	}

	ctx := context.Background()
	handler := d.Handler(ctx)

	return &testHarnessM3{
		d:       d,
		store:   store,
		tracker: tracker,
		sess:    sess,
		handler: handler,
		ctx:     ctx,
	}
}

func TestM3_Daemon_RetryMatch_Endpoint(t *testing.T) {
	h := setupM3DaemonTest(t)

	// Seed a failed match
	matchGUID := "guid-retry-1"
	_ = h.store.UpsertDiscoveredMatches(h.ctx, []*storage.MatchRecord{
		{
			MatchGUID:      matchGUID,
			DownloadStatus: storage.DownloadFailed,
			UploadStatus:   storage.UploadFailed,
			LastError:      "network timeout",
			RetryCount:     3,
		},
	})

	t.Run("ValidRetry_AllowedOrigin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/matches/"+matchGUID+"/retry", nil)
		req.Header.Set("Origin", "http://localhost:49125")
		rec := httptest.NewRecorder()

		h.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: body=%s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("invalid json: %v", err)
		}
		if resp["status"] != "ok" || resp["match_guid"] != matchGUID {
			t.Errorf("unexpected response payload: %v", resp)
		}

		// Verify store state was reset
		m, err := h.store.GetMatch(h.ctx, matchGUID)
		if err != nil {
			t.Fatalf("failed to query match: %v", err)
		}
		if m.DownloadStatus != storage.DownloadPending || m.UploadStatus != storage.UploadPending || m.RetryCount != 0 {
			t.Errorf("match state was not reset: dl=%s, up=%s, retries=%d", m.DownloadStatus, m.UploadStatus, m.RetryCount)
		}
	})

	t.Run("DisallowedOrigin_CSRF_Rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/matches/"+matchGUID+"/retry", nil)
		req.Header.Set("Origin", "http://malicious-website.com")
		rec := httptest.NewRecorder()

		h.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for disallowed origin, got %d", rec.Code)
		}
	})

	t.Run("UnknownMatchGUID_404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/matches/nonexistent-guid-999/retry", nil)
		req.Header.Set("Origin", "http://localhost:49125")
		rec := httptest.NewRecorder()

		h.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d", rec.Code)
		}
	})
}

func TestM3_Daemon_PlayerDirectory_And_Detail_Endpoints(t *testing.T) {
	h := setupM3DaemonTest(t)

	p1ID := "Steam|76561198000000001|0"
	p2ID := "Epic|epic_rival_2|0"

	_ = h.store.UpsertPlayer(h.ctx, &storage.PlayerRecord{
		PlayerID:   p1ID,
		Platform:   "Steam",
		PlayerName: "GreatTeammate",
		Notes:      "Good rotation",
		Tags:       []string{"reliable"},
	})
	_ = h.store.UpsertPlayer(h.ctx, &storage.PlayerRecord{
		PlayerID:   p2ID,
		Platform:   "Epic",
		PlayerName: "ChallengerRival",
	})

	// Seed matches and outcomes
	winnerBlue := 0
	_ = h.store.SyncReplayStats(h.ctx, &storage.ReplayMatchStats{
		MatchGUID:   "match-p1-1",
		Playlist:    11,
		BlueScore:   3,
		OrangeScore: 1,
		WinnerTeam:  &winnerBlue,
		Date:        time.Now().UTC().Format(time.RFC3339),
	})
	wonTrue := true
	_ = h.store.SaveMatchPlayers(h.ctx, "match-p1-1", []*storage.MatchPlayerRecord{
		{MatchGUID: "match-p1-1", PlayerID: "Steam|local|0", TeamNum: 0, IsLocal: true, Won: &wonTrue, Score: 400},
		{MatchGUID: "match-p1-1", PlayerID: p1ID, TeamNum: 0, IsLocal: false, Won: &wonTrue, Score: 500, Goals: 2},
	})
	_ = h.store.RecordMatchResults(h.ctx, "match-p1-1", 11, []storage.PlayerOutcome{
		{PlayerID: p1ID, Platform: "Steam", PlayerName: "GreatTeammate", IsTeammate: true, Won: true},
	})

	_ = h.store.SyncReplayStats(h.ctx, &storage.ReplayMatchStats{
		MatchGUID:   "match-p1-2",
		Playlist:    11,
		BlueScore:   4,
		OrangeScore: 2,
		WinnerTeam:  &winnerBlue,
		Date:        time.Now().UTC().Format(time.RFC3339),
	})
	_ = h.store.SaveMatchPlayers(h.ctx, "match-p1-2", []*storage.MatchPlayerRecord{
		{MatchGUID: "match-p1-2", PlayerID: "Steam|local|0", TeamNum: 0, IsLocal: true, Won: &wonTrue, Score: 350},
		{MatchGUID: "match-p1-2", PlayerID: p1ID, TeamNum: 0, IsLocal: false, Won: &wonTrue, Score: 450, Goals: 1},
	})
	_ = h.store.RecordMatchResults(h.ctx, "match-p1-2", 11, []storage.PlayerOutcome{
		{PlayerID: p1ID, Platform: "Steam", PlayerName: "GreatTeammate", IsTeammate: true, Won: true},
	})

	t.Run("GET_api_players_WithSpotlightsAndRoleFilter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/players?role=synergy&sort=win_rate&order=desc", nil)
		rec := httptest.NewRecorder()

		h.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: body=%s", rec.Code, rec.Body.String())
		}

		var resp daemon.PlayerSearchResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("invalid json: %v", err)
		}
		if resp.Total != 1 || len(resp.Players) != 1 {
			t.Fatalf("expected 1 synergy player, got total=%d, len=%d", resp.Total, len(resp.Players))
		}
		if resp.Players[0].PlayerID != p1ID {
			t.Errorf("expected %s, got %s", p1ID, resp.Players[0].PlayerID)
		}
		if resp.Spotlights == nil || resp.Spotlights.BestTeammate == nil || resp.Spotlights.BestTeammate.Player == nil {
			t.Errorf("expected BestTeammate spotlight in response")
		}
	})

	t.Run("GET_api_players_id_IncludesRecentMatches", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/players/"+p1ID, nil)
		rec := httptest.NewRecorder()

		h.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: body=%s", rec.Code, rec.Body.String())
		}

		var resp daemon.PlayerDetailResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("invalid json: %v", err)
		}
		if resp.PlayerRecord == nil || resp.PlayerRecord.PlayerID != p1ID {
			t.Fatalf("expected player %s, got %v", p1ID, resp.PlayerRecord)
		}
		if len(resp.RecentMatches) != 2 {
			t.Fatalf("expected 2 recent matches, got %d", len(resp.RecentMatches))
		}
		if resp.RecentMatches[0].Role != "teammate" {
			t.Errorf("expected role 'teammate', got '%s'", resp.RecentMatches[0].Role)
		}
	})

	t.Run("GET_api_players_id_matches_Filtering", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/players/"+p1ID+"/matches?role=teammate&result=victory", nil)
		rec := httptest.NewRecorder()

		h.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: body=%s", rec.Code, rec.Body.String())
		}

		var resp daemon.PlayerMatchesResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("invalid json: %v", err)
		}
		if resp.Total != 2 || len(resp.Matches) != 2 {
			t.Fatalf("expected 2 matches, got total=%d, len=%d", resp.Total, len(resp.Matches))
		}
	})

	t.Run("PATCH_api_players_id_NotesAndTags", func(t *testing.T) {
		body := bytes.NewBufferString(`{"notes":"Updated clutch player","tags":["clutch","consistent"]}`)
		req := httptest.NewRequest(http.MethodPatch, "/api/players/"+p1ID, body)
		req.Header.Set("Origin", "http://localhost:49125")
		rec := httptest.NewRecorder()

		h.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: body=%s", rec.Code, rec.Body.String())
		}

		var result map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatalf("invalid json: %v", err)
		}
		if result["status"] != "ok" {
			t.Fatalf("expected status ok, got %v", result["status"])
		}

		// Verify persisted state
		p, err := h.store.GetPlayer(h.ctx, p1ID)
		if err != nil {
			t.Fatalf("failed to query player: %v", err)
		}
		if p.Notes != "Updated clutch player" {
			t.Errorf("notes not updated: %s", p.Notes)
		}
		if len(p.Tags) != 2 || p.Tags[0] != "clutch" || p.Tags[1] != "consistent" {
			t.Errorf("tags not updated: %v", p.Tags)
		}
	})
}

func TestM3_Daemon_MMRHistory_And_SyncSecurity(t *testing.T) {
	h := setupM3DaemonTest(t)

	now := time.Now().UTC()
	// Record MMR history snapshots
	_ = h.store.RecordMMR(h.ctx, &storage.MMRHistoryRecord{
		RecordedAt: now.Add(-30 * time.Minute),
		PlaylistID: 11,
		MMR:        1000,
		MMRChange:  0,
		Tier:       10,
		Division:   1,
	})
	_ = h.store.RecordMMR(h.ctx, &storage.MMRHistoryRecord{
		RecordedAt: now.Add(-15 * time.Minute),
		PlaylistID: 11,
		MMR:        1015,
		MMRChange:  15,
		Tier:       10,
		Division:   2,
	})
	_ = h.store.RecordMMR(h.ctx, &storage.MMRHistoryRecord{
		RecordedAt: now,
		PlaylistID: 11,
		MMR:        1024,
		MMRChange:  9,
		Tier:       10,
		Division:   2,
	})

	t.Run("GET_api_mmr_history_Chronological", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/mmr-history?playlist=11&order=asc", nil)
		rec := httptest.NewRecorder()

		h.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: body=%s", rec.Code, rec.Body.String())
		}

		var resp daemon.MMRHistoryResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("invalid json: %v", err)
		}
		if len(resp.History) != 3 {
			t.Fatalf("expected 3 mmr history records, got %d", len(resp.History))
		}
		if resp.History[0].MMR != 1000 || resp.History[2].MMR != 1024 {
			t.Errorf("expected chronological order oldest first (1000 -> 1024), got %d -> %d", resp.History[0].MMR, resp.History[2].MMR)
		}
	})

	t.Run("POST_sync_RejectsGET_AndEnforcesCSRF", func(t *testing.T) {
		// 1. GET /sync should be rejected with 405 Method Not Allowed
		getReq := httptest.NewRequest(http.MethodGet, "/sync", nil)
		getRec := httptest.NewRecorder()
		h.handler.ServeHTTP(getRec, getReq)
		if getRec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405 for GET /sync, got %d", getRec.Code)
		}

		// 2. POST /sync with disallowed origin should return 403 Forbidden
		badPost := httptest.NewRequest(http.MethodPost, "/sync", nil)
		badPost.Header.Set("Origin", "http://untrusted-site.com")
		badRec := httptest.NewRecorder()
		h.handler.ServeHTTP(badRec, badPost)
		if badRec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for POST /sync with bad origin, got %d", badRec.Code)
		}

		// 3. POST /sync with allowed origin should return 200 OK
		goodPost := httptest.NewRequest(http.MethodPost, "/sync", nil)
		goodPost.Header.Set("Origin", "http://localhost:49125")
		goodRec := httptest.NewRecorder()
		h.handler.ServeHTTP(goodRec, goodPost)
		if goodRec.Code != http.StatusOK {
			t.Fatalf("expected 200 for POST /sync with localhost origin, got %d", goodRec.Code)
		}
	})
}
