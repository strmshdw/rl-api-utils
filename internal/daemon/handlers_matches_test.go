package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/config"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/syncer"
)

type dummySyncer struct{}

func (d *dummySyncer) RunCycle(ctx context.Context) (*syncer.SyncStats, error) {
	return &syncer.SyncStats{}, nil
}

func TestDaemon_MatchesEndpoints(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_matches.db")
	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Seed matches
	stats1 := &storage.ReplayMatchStats{
		MatchGUID:   "guid-alpha",
		MapName:     "DFH Stadium",
		Playlist:    11,
		BlueScore:   3,
		OrangeScore: 2,
		Date:        "2026-10-12T12:00:00Z",
		Players: []storage.ReplayPlayerStats{
			{Name: "PlayerOne", Platform: "Epic", OnlineID: "p1", Team: 0, Score: 500, Goals: 2},
			{Name: "PlayerTwo", Platform: "Steam", OnlineID: "p2", Team: 1, Score: 300, Goals: 1},
		},
	}
	if err := store.SyncReplayStats(ctx, stats1); err != nil {
		t.Fatalf("failed to sync replay stats: %v", err)
	}

	stats2 := &storage.ReplayMatchStats{
		MatchGUID:   "guid-beta",
		MapName:     "Utopia Coliseum",
		Playlist:    13,
		BlueScore:   1,
		OrangeScore: 4,
		Date:        "2026-10-12T13:00:00Z",
		Players: []storage.ReplayPlayerStats{
			{Name: "PlayerOne", Platform: "Epic", OnlineID: "p1", Team: 1, Score: 400, Goals: 3},
		},
	}
	if err := store.SyncReplayStats(ctx, stats2); err != nil {
		t.Fatalf("failed to sync replay stats 2: %v", err)
	}

	cfg := &config.Config{
		Sync: config.SyncConfig{
			Once: false,
		},
	}

	d, err := New(&dummySyncer{}, cfg, WithStateStore(store))
	if err != nil {
		t.Fatalf("failed to create daemon: %v", err)
	}

	handler := d.Handler(ctx)

	// 1. List all matches
	req := httptest.NewRequest(http.MethodGet, "/api/matches", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/matches status = %d, want 200", rec.Code)
	}

	var resp MatchesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal MatchesResponse failed: %v", err)
	}
	if resp.Total != 2 {
		t.Errorf("expected total 2, got %d", resp.Total)
	}
	if len(resp.Matches) != 2 {
		t.Errorf("expected 2 matches, got %d", len(resp.Matches))
	}

	// 2. Filter by playlist
	req = httptest.NewRequest(http.MethodGet, "/api/matches?playlist=11", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal MatchesResponse failed: %v", err)
	}
	if resp.Total != 1 || len(resp.Matches) != 1 {
		t.Errorf("expected 1 match for playlist 11, got %d (total %d)", len(resp.Matches), resp.Total)
	}

	// 3. Search query
	req = httptest.NewRequest(http.MethodGet, "/api/matches?query=Utopia", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal MatchesResponse failed: %v", err)
	}
	if resp.Total != 1 || resp.Matches[0].Stats.MapName != "Utopia Coliseum" {
		t.Errorf("expected Utopia match, got %+v", resp.Matches)
	}

	// 4. Get specific match
	req = httptest.NewRequest(http.MethodGet, "/api/matches/guid-alpha", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/matches/guid-alpha status = %d, want 200", rec.Code)
	}
	var matchRec storage.HistoricalMatchRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &matchRec); err != nil {
		t.Fatalf("unmarshal HistoricalMatchRecord failed: %v", err)
	}
	if matchRec.MatchGUID != "guid-alpha" {
		t.Errorf("expected match guid-alpha, got %s", matchRec.MatchGUID)
	}

	// 5. Get non-existent match
	req = httptest.NewRequest(http.MethodGet, "/api/matches/non-existent", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-existent match, got %d", rec.Code)
	}
}

func TestDaemon_BallchasingSyncEndpoints(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{
		Sync: config.SyncConfig{Once: false},
	}

	// Test endpoints without history syncer
	dNoSync, err := New(&dummySyncer{}, cfg)
	if err != nil {
		t.Fatalf("New daemon failed: %v", err)
	}
	hNoSync := dNoSync.Handler(ctx)

	// Status when not configured
	req := httptest.NewRequest(http.MethodGet, "/api/sync/ballchasing/status", nil)
	rec := httptest.NewRecorder()
	hNoSync.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for status, got %d", rec.Code)
	}
	var prog syncer.BallchasingHistorySyncProgress
	if err := json.Unmarshal(rec.Body.Bytes(), &prog); err != nil {
		t.Fatalf("unmarshal progress failed: %v", err)
	}
	if prog.Running {
		t.Errorf("expected running to be false")
	}

	// POST trigger when not configured -> 400
	req = httptest.NewRequest(http.MethodPost, "/api/sync/ballchasing", nil)
	rec = httptest.NewRecorder()
	hNoSync.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when history syncer not configured, got %d", rec.Code)
	}

	// Now configure mock ballchasing client and history syncer
	bcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/" {
			_ = json.NewEncoder(w).Encode(ballchasing.PingResponse{Name: "Tester"})
			return
		}
		_ = json.NewEncoder(w).Encode(ballchasing.ReplayListResponse{
			Count: 0,
			List:  []ballchasing.ReplayItem{},
			Next:  "",
		})
	}))
	defer bcSrv.Close()

	bcClient, err := ballchasing.NewClient(ballchasing.ClientConfig{
		BaseURL: bcSrv.URL,
		APIKey:  "dummy-key",
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	tempDir := t.TempDir()
	store, _ := storage.NewSQLiteStore(filepath.Join(tempDir, "test.db"))
	defer store.Close()

	historySyncer := syncer.NewBallchasingHistorySyncer(bcClient, store, "Epic|test|0", "Tester", nil)
	historySyncer.SetRateInterval(time.Millisecond)

	dWithSync, err := New(&dummySyncer{}, cfg, WithHistorySyncer(historySyncer), WithStateStore(store))
	if err != nil {
		t.Fatalf("New daemon failed: %v", err)
	}
	hWithSync := dWithSync.Handler(ctx)

	// POST trigger -> 202 Accepted
	req = httptest.NewRequest(http.MethodPost, "/api/sync/ballchasing", nil)
	rec = httptest.NewRecorder()
	hWithSync.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d: %s", rec.Code, rec.Body.String())
	}

	// Check status
	time.Sleep(50 * time.Millisecond)
	req = httptest.NewRequest(http.MethodGet, "/api/sync/ballchasing/status", nil)
	rec = httptest.NewRecorder()
	hWithSync.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
