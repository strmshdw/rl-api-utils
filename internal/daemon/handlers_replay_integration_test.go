package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/session"
	"github.com/dank/rl-api-utils/internal/storage"
)

func TestDaemon_SQLiteTeammateFallback_WithoutReplayStatsJSON(t *testing.T) {
	ctx := context.Background()
	matchTime := time.Date(2026, 11, 25, 20, 0, 0, 0, time.UTC)
	replayPayload := append([]byte{0x10, 0x04, 0x00, 0x00}, []byte("TEAMMATE_SQLITE_REPLAY_PAYLOAD")...)

	var sawUploaderMe atomic.Bool
	var sawMateQuery atomic.Bool
	var sawLocalFallbackQuery atomic.Bool

	bcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/replays":
			w.Header().Set("Content-Type", "application/json")
			q := r.URL.Query()
			if q.Get("uploader") == "me" {
				sawUploaderMe.Store(true)
				_ = json.NewEncoder(w).Encode(ballchasing.ReplayListResponse{Count: 0, List: []ballchasing.ReplayItem{}})
				return
			}
			if q.Get("player-name") == "MatePro" && q.Get("uploader") == "" {
				sawMateQuery.Store(true)
				_ = json.NewEncoder(w).Encode(ballchasing.ReplayListResponse{
					Count: 1,
					List: []ballchasing.ReplayItem{{
						ID:         "bc-mate-sql-1",
						MatchGUID:  "sql-teammate-guid-1",
						Link:       "https://ballchasing.com/replay/bc-mate-sql-1",
						Date:       matchTime.Add(15 * time.Second).Format(time.RFC3339),
						PlaylistID: "ranked-doubles",
						Blue:       ballchasing.ReplayTeam{Goals: 3, Players: []ballchasing.ReplayPlayer{{Name: "MatePro"}}},
						Orange:     ballchasing.ReplayTeam{Goals: 2},
					}},
				})
				return
			}
			if q.Get("player-name") == "LocalPilot" && q.Get("uploader") == "" {
				sawLocalFallbackQuery.Store(true)
				_ = json.NewEncoder(w).Encode(ballchasing.ReplayListResponse{
					Count: 1,
					List: []ballchasing.ReplayItem{{
						ID:         "bc-localname-sql-2",
						MatchGUID:  "sql-localname-guid-2",
						Link:       "https://ballchasing.com/replay/bc-localname-sql-2",
						Date:       matchTime.Add(10 * time.Second).Format(time.RFC3339),
						PlaylistID: "ranked-doubles",
					}},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(ballchasing.ReplayListResponse{Count: 0, List: []ballchasing.ReplayItem{}})
		case "/replays/bc-mate-sql-1/file":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(replayPayload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer bcSrv.Close()

	bcClient, err := ballchasing.NewClient(ballchasing.ClientConfig{
		BaseURL:     bcSrv.URL,
		APIKey:      "test-key",
		MaxRetries:  0,
		RateLimiter: ballchasing.NewRateLimiter(time.Millisecond),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	tmpDir := t.TempDir()
	replayDir := filepath.Join(tmpDir, "replays")
	if err := os.MkdirAll(replayDir, 0o755); err != nil {
		t.Fatalf("mkdir replayDir: %v", err)
	}
	store, err := storage.NewSQLiteStore(filepath.Join(tmpDir, "teammate.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	// Seed match with NO ballchasing_id and NO ReplayStatsJSON — only players + match_players rows.
	if err := store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{
		{
			MatchGUID:            "sql-teammate-guid-1",
			RecordStartTimestamp: matchTime.Unix(),
			Playlist:             11,
			DownloadStatus:       storage.DownloadSkipped,
			UploadStatus:         storage.UploadPending,
		},
		{
			MatchGUID:            "sql-localname-guid-2",
			RecordStartTimestamp: matchTime.Unix(),
			Playlist:             11,
			DownloadStatus:       storage.DownloadSkipped,
			UploadStatus:         storage.UploadPending,
		},
	}); err != nil {
		t.Fatalf("UpsertDiscoveredMatches: %v", err)
	}
	if err := store.UpsertPlayer(ctx, &storage.PlayerRecord{
		PlayerID:    "Steam|mate123|0",
		Platform:    "Steam",
		PlayerName:  "MatePro",
		FirstSeenAt: matchTime,
		LastSeenAt:  matchTime,
	}); err != nil {
		t.Fatalf("UpsertPlayer: %v", err)
	}
	if err := store.SaveMatchPlayers(ctx, "sql-teammate-guid-1", []*storage.MatchPlayerRecord{{
		MatchGUID: "sql-teammate-guid-1",
		PlayerID:  "Steam|mate123|0",
		TeamNum:   0,
		Score:     420,
		Goals:     2,
	}}); err != nil {
		t.Fatalf("SaveMatchPlayers: %v", err)
	}

	// Verify SQLiteStore.GetMatchPlayers now populates PlayerName via LEFT JOIN players.
	mps, err := store.GetMatchPlayers(ctx, "sql-teammate-guid-1")
	if err != nil || len(mps) != 1 || mps[0].PlayerName != "MatePro" || mps[0].Platform != "Steam" {
		t.Fatalf("expected joined PlayerName=MatePro Platform=Steam, got err=%v rows=%+v", err, mps)
	}

	cfg := config.NewDefaultConfig()
	cfg.Sync.ReplayDir = replayDir
	cfg.PlayerTracking.LocalPlayerName = "LocalPilot"
	d, err := New(&dummySyncer{}, cfg, WithStateStore(store), WithBallchasingClient(bcClient))
	if err != nil {
		t.Fatalf("New daemon: %v", err)
	}
	h := d.Handler(ctx)

	// 1. Lookup + download for teammate-uploaded match using SQLite joined PlayerName.
	recStatus := httptest.NewRecorder()
	h.ServeHTTP(recStatus, httptest.NewRequest(http.MethodGet, "/api/matches/sql-teammate-guid-1/replay/status?lookup=true", nil))
	if recStatus.Code != http.StatusOK {
		t.Fatalf("status?lookup=true = %d: %s", recStatus.Code, recStatus.Body.String())
	}
	var st ReplayAvailabilityResponse
	_ = json.Unmarshal(recStatus.Body.Bytes(), &st)
	if !st.Available || st.Source != "ballchasing" || st.BallchasingID != "bc-mate-sql-1" {
		t.Fatalf("unexpected teammate lookup status: %+v", st)
	}
	if !sawUploaderMe.Load() || !sawMateQuery.Load() {
		t.Fatalf("expected uploader=me (%v) and player-name=MatePro (%v)", sawUploaderMe.Load(), sawMateQuery.Load())
	}

	recGet := httptest.NewRecorder()
	h.ServeHTTP(recGet, httptest.NewRequest(http.MethodGet, "/api/matches/sql-teammate-guid-1/replay", nil))
	if recGet.Code != http.StatusOK || !bytes.Equal(recGet.Body.Bytes(), replayPayload) {
		t.Fatalf("GET replay failed: code=%d", recGet.Code)
	}

	// 2. Match with zero match_players rows falls back to cfg.PlayerTracking.LocalPlayerName.
	recLocalSt := httptest.NewRecorder()
	h.ServeHTTP(recLocalSt, httptest.NewRequest(http.MethodGet, "/api/matches/sql-localname-guid-2/replay/status?lookup=true", nil))
	if recLocalSt.Code != http.StatusOK {
		t.Fatalf("local-name fallback status = %d: %s", recLocalSt.Code, recLocalSt.Body.String())
	}
	var stLocal ReplayAvailabilityResponse
	_ = json.Unmarshal(recLocalSt.Body.Bytes(), &stLocal)
	if !stLocal.Available || stLocal.BallchasingID != "bc-localname-sql-2" || !sawLocalFallbackQuery.Load() {
		t.Fatalf("expected LocalPlayerName fallback hit, got status=%+v sawLocal=%v", stLocal, sawLocalFallbackQuery.Load())
	}
}

func TestDaemon_SessionOnlyMatch_LinkCacheAndRestartDiskCache(t *testing.T) {
	ctx := context.Background()
	payload := append([]byte{0x10, 0x04, 0x00, 0x00}, []byte("SESSION_CACHED_REPLAY_BINARY")...)

	fetcher := &stubReplayFetcher{
		findFn: func(ctx context.Context, q ballchasing.ReplaySearchQuery) (*ballchasing.ReplayItem, error) {
			return &ballchasing.ReplayItem{
				ID:   "bc-sess-cached-1",
				Link: "https://ballchasing.com/replay/bc-sess-cached-1",
			}, nil
		},
		downloadFn: func(ctx context.Context, replayID, destPath string) (int64, error) {
			if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
				return 0, err
			}
			if err := os.WriteFile(destPath, payload, 0o644); err != nil {
				return 0, err
			}
			return int64(len(payload)), nil
		},
	}

	d, store, replayDir := setupReplayTestDaemon(t, fetcher)
	guid := "sess-cache-guid-01"
	sessMatchesJSON, _ := json.Marshal([]*session.SessionMatchDetail{{
		MatchGUID:   guid,
		PlaylistID:  11,
		StartedAt:   time.Now().UTC().Add(-3 * time.Minute),
		BlueScore:   2,
		OrangeScore: 1,
	}})
	if err := store.SaveSession(ctx, &storage.SessionRecord{
		SessionID:    "sess-cache-1",
		StartedAt:    time.Now().UTC().Add(-10 * time.Minute),
		TotalMatches: 1,
		MatchesJSON:  string(sessMatchesJSON),
	}); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	h := d.Handler(ctx)

	// Open 1: ?lookup=true (1 search, 0 downloads) -> GET /replay (0 extra searches, 1 download).
	recSt1 := httptest.NewRecorder()
	h.ServeHTTP(recSt1, httptest.NewRequest(http.MethodGet, "/api/matches/"+guid+"/replay/status?lookup=true", nil))
	if recSt1.Code != http.StatusOK {
		t.Fatalf("open1 status = %d: %s", recSt1.Code, recSt1.Body.String())
	}
	var st1 ReplayAvailabilityResponse
	_ = json.Unmarshal(recSt1.Body.Bytes(), &st1)
	if !st1.Available || st1.Source != "ballchasing" || st1.BallchasingID != "bc-sess-cached-1" {
		t.Fatalf("open1 unexpected status: %+v", st1)
	}
	if fetcher.findCalls.Load() != 1 || fetcher.dlCalls.Load() != 0 {
		t.Fatalf("after open1 status: findCalls=%d dlCalls=%d, want 1/0", fetcher.findCalls.Load(), fetcher.dlCalls.Load())
	}

	recGet1 := httptest.NewRecorder()
	h.ServeHTTP(recGet1, httptest.NewRequest(http.MethodGet, "/api/matches/"+guid+"/replay", nil))
	if recGet1.Code != http.StatusOK || !bytes.Equal(recGet1.Body.Bytes(), payload) {
		t.Fatalf("open1 GET failed: code=%d", recGet1.Code)
	}
	if recGet1.Header().Get("X-Replay-Source") != "ballchasing" || recGet1.Header().Get("Cache-Control") != "private, no-store" {
		t.Errorf("open1 headers: source=%q cc=%q", recGet1.Header().Get("X-Replay-Source"), recGet1.Header().Get("Cache-Control"))
	}
	if fetcher.findCalls.Load() != 1 || fetcher.dlCalls.Load() != 1 {
		t.Fatalf("after open1 GET: findCalls=%d dlCalls=%d, want 1/1", fetcher.findCalls.Load(), fetcher.dlCalls.Load())
	}

	// Open 2 (same daemon): status & GET both hit cache with 0 Ballchasing calls.
	recSt2 := httptest.NewRecorder()
	h.ServeHTTP(recSt2, httptest.NewRequest(http.MethodGet, "/api/matches/"+guid+"/replay/status", nil))
	var st2 ReplayAvailabilityResponse
	_ = json.Unmarshal(recSt2.Body.Bytes(), &st2)
	if !st2.Available || st2.Source != "cache" || !st2.LocalAvailable {
		t.Fatalf("open2 status expected cache hit, got %+v", st2)
	}

	recGet2 := httptest.NewRecorder()
	h.ServeHTTP(recGet2, httptest.NewRequest(http.MethodGet, "/api/matches/"+guid+"/replay", nil))
	if recGet2.Code != http.StatusOK || recGet2.Header().Get("X-Replay-Source") != "cache" || !bytes.Equal(recGet2.Body.Bytes(), payload) {
		t.Fatalf("open2 GET expected cache hit: code=%d source=%q", recGet2.Code, recGet2.Header().Get("X-Replay-Source"))
	}
	if fetcher.findCalls.Load() != 1 || fetcher.dlCalls.Load() != 1 {
		t.Fatalf("open2 triggered unexpected Ballchasing calls: find=%d dl=%d", fetcher.findCalls.Load(), fetcher.dlCalls.Load())
	}

	// Restarted daemon (fresh instance, empty in-memory link cache) still hits disk cache by canonicalGUID.
	cfg2 := config.NewDefaultConfig()
	cfg2.Sync.ReplayDir = replayDir
	restarted, err := New(&dummySyncer{}, cfg2, WithStateStore(store), WithBallchasingClient(fetcher))
	if err != nil {
		t.Fatalf("New restarted daemon: %v", err)
	}
	h2 := restarted.Handler(ctx)

	recRestartSt := httptest.NewRecorder()
	h2.ServeHTTP(recRestartSt, httptest.NewRequest(http.MethodGet, "/api/matches/"+guid+"/replay/status", nil))
	var stRestart ReplayAvailabilityResponse
	_ = json.Unmarshal(recRestartSt.Body.Bytes(), &stRestart)
	if !stRestart.Available || stRestart.Source != "cache" || !stRestart.LocalAvailable {
		t.Fatalf("restarted daemon status expected disk cache hit, got %+v", stRestart)
	}

	recRestartGet := httptest.NewRecorder()
	h2.ServeHTTP(recRestartGet, httptest.NewRequest(http.MethodGet, "/api/matches/"+guid+"/replay", nil))
	if recRestartGet.Code != http.StatusOK || recRestartGet.Header().Get("X-Replay-Source") != "cache" || !bytes.Equal(recRestartGet.Body.Bytes(), payload) {
		t.Fatalf("restarted daemon GET expected disk cache hit: code=%d source=%q", recRestartGet.Code, recRestartGet.Header().Get("X-Replay-Source"))
	}
	if fetcher.findCalls.Load() != 1 || fetcher.dlCalls.Load() != 1 {
		t.Fatalf("restarted daemon made unexpected Ballchasing calls: find=%d dl=%d", fetcher.findCalls.Load(), fetcher.dlCalls.Load())
	}
}
