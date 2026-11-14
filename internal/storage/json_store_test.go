package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestJSONStore_LifecycleValidationLegacyBackfillAndClosed(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	if _, err := NewJSONStore(""); err == nil {
		t.Fatal("expected error for empty filePath")
	}

	// Corrupt JSON must fail cleanly.
	corruptPath := filepath.Join(tmpDir, "corrupt.json")
	if err := os.WriteFile(corruptPath, []byte(`{"broken":`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewJSONStore(corruptPath); err == nil {
		t.Fatal("expected error for corrupt json file")
	}

	// Empty 0-byte file initializes cleanly and removes stale temp files.
	nestedDir := filepath.Join(tmpDir, "nested", "state")
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatal(err)
	}
	staleTmp := filepath.Join(nestedDir, ".rl-sync-state-stale.tmp")
	if err := os.WriteFile(staleTmp, []byte("partial"), 0644); err != nil {
		t.Fatal(err)
	}
	zeroPath := filepath.Join(nestedDir, "store.json")
	if err := os.WriteFile(zeroPath, nil, 0644); err != nil {
		t.Fatal(err)
	}
	js, err := NewJSONStore(zeroPath)
	if err != nil {
		t.Fatalf("NewJSONStore(0-byte) failed: %v", err)
	}
	_ = js.Close()

	// Legacy JSON payload with stale duplicate match_players and 0-0 score must hydrate + deduplicate + repair.
	legacyStats, _ := json.Marshal(ReplayMatchStats{
		MatchGUID: "legacy-1",
		Playlist:  11,
		Players: []ReplayPlayerStats{
			{Name: "One", Platform: "Steam", OnlineID: "111", Team: 0, Score: 300, Goals: 2},
			{Name: "Two", Platform: "Epic", OnlineID: "222", Team: 1, Score: 200, Goals: 0},
		},
	})
	legacyJSON, _ := json.Marshal(jsonStatePayload{
		Version: 1,
		Matches: map[string]*MatchRecord{
			"legacy-1": {
				MatchGUID:            "legacy-1",
				RecordStartTimestamp: time.Now().UTC().Unix(),
				Playlist:             11,
				DownloadStatus:       DownloadDownloaded,
				UploadStatus:         UploadUploaded,
				ReplayStatsJSON:      string(legacyStats),
			},
		},
		Auth: map[string]*AuthRecord{
			"epic": {Provider: "epic", RefreshToken: "rt-old", AccountID: "Epic|1|0", DisplayName: "Me"},
		},
		MatchPlayers: map[string][]*MatchPlayerRecord{
			"legacy-1": {
				{MatchGUID: "legacy-1", PlayerID: "Steam|111|0", TeamNum: 0, Score: 300, Goals: 2},
				{MatchGUID: "legacy-1", PlayerID: "Steam|111|0", TeamNum: 0, Score: 300, Goals: 2},
			},
		},
	})
	legacyPath := filepath.Join(nestedDir, "legacy.json")
	if err := os.WriteFile(legacyPath, legacyJSON, 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := NewJSONStore(legacyPath)
	if err != nil {
		t.Fatalf("NewJSONStore(legacy) failed: %v", err)
	}
	if _, statErr := os.Stat(staleTmp); !os.IsNotExist(statErr) {
		t.Fatalf("expected stale temp file %s to be cleaned up", staleTmp)
	}
	mGot, err := loaded.GetMatch(ctx, "legacy-1")
	if err != nil || mGot.BlueScore != 2 || mGot.OrangeScore != 0 {
		t.Fatalf("zero-score repair in JSONStore failed: m=%+v, err=%v", mGot, err)
	}
	// Deep-copy mutation defense: modifying returned MatchRecord must not persist in store.
	mGot.MapName = "MUTATED_BY_CALLER"
	mAgain, _ := loaded.GetMatch(ctx, "legacy-1")
	if mAgain.MapName == "MUTATED_BY_CALLER" {
		t.Fatal("JSONStore leaked internal mutable reference on GetMatch")
	}

	// Idempotent Close() + ErrStoreClosed contract.
	if err := loaded.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if err := loaded.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
	if _, err := loaded.GetMatch(ctx, "legacy-1"); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("closed GetMatch = %v, want ErrStoreClosed", err)
	}
	if err := loaded.SaveSession(ctx, &SessionRecord{SessionID: "s"}); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("closed SaveSession = %v, want ErrStoreClosed", err)
	}
}

func TestJSONStore_PersistenceParityWithSQLiteAndConcurrency(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, "parity.json")
	dbPath := filepath.Join(tmpDir, "parity.db")

	js, err := NewJSONStore(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	ss, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()

	now := time.Now().UTC().Truncate(time.Second)
	seedOperations := func(st StateStore) {
		t.Helper()
		_ = st.SaveAuthState(ctx, "steam", "tok", "Steam|local|0", "LocalAce")
		_ = st.UpsertDiscoveredMatches(ctx, []*MatchRecord{
			{MatchGUID: "p-1", RecordStartTimestamp: now.Unix(), MapName: "DFH", Playlist: 11, BlueScore: 3, OrangeScore: 2, DownloadStatus: DownloadDownloaded, UploadStatus: UploadUploaded, StatsSyncStatus: StatsSyncSynced},
			{MatchGUID: "p-2", RecordStartTimestamp: now.Add(time.Minute).Unix(), MapName: "Aquadome", Playlist: 11, BlueScore: 1, OrangeScore: 4, DownloadStatus: DownloadDownloaded, UploadStatus: UploadUploaded, StatsSyncStatus: StatsSyncSynced},
		})
		_ = st.SaveMatchPlayers(ctx, "p-1", []*MatchPlayerRecord{
			{MatchGUID: "p-1", PlayerID: "Steam|local|0", TeamNum: 0, IsLocal: true, Score: 500, Goals: 2},
			{MatchGUID: "p-1", PlayerID: "Epic|ally|0", TeamNum: 0, IsLocal: false, Score: 420, Goals: 1},
		})
		_ = st.RecordMatchResults(ctx, "p-1", 11, []PlayerOutcome{{PlayerID: "Epic|ally|0", Platform: "Epic", PlayerName: "Ally", IsTeammate: true, Won: true}}, now)
		_ = st.RecordMMR(ctx, &MMRHistoryRecord{RecordedAt: now, MatchGUID: "p-1", PlaylistID: 11, MMR: 1450, Tier: 17, Division: 1})
		_ = st.SaveSession(ctx, &SessionRecord{SessionID: "s-1", StartedAt: now, UpdatedAt: now, TotalMatches: 2, TotalWins: 1, TotalLosses: 1})
	}
	seedOperations(js)
	seedOperations(ss)

	// Reopen JSONStore to verify full round-trip serialization across all tables.
	_ = js.Close()
	js2, err := NewJSONStore(jsonPath)
	if err != nil {
		t.Fatalf("reopen JSONStore failed: %v", err)
	}
	defer js2.Close()

	jHist, jTotal, err := js2.ListHistoricalMatches(ctx, MatchFilter{Limit: 10})
	sHist, sTotal, sErr := ss.ListHistoricalMatches(ctx, MatchFilter{Limit: 10})
	if err != nil || sErr != nil || jTotal != sTotal || len(jHist) != len(sHist) {
		t.Fatalf("historical parity mismatch: json=(%d,%v) sqlite=(%d,%v)", jTotal, err, sTotal, sErr)
	}

	jSum, jSTotal, err := js2.FilterPlayerSummaries(ctx, PlayerDirectoryFilter{Limit: 10})
	sSum, sSTotal, sErr := ss.FilterPlayerSummaries(ctx, PlayerDirectoryFilter{Limit: 10})
	if err != nil || sErr != nil || jSTotal != sSTotal || len(jSum) != len(sSum) {
		t.Fatalf("player summary parity mismatch: json=(%d,%v) sqlite=(%d,%v)", jSTotal, err, sSTotal, sErr)
	}
	if jSum[0].WithYouStats.Wins != sSum[0].WithYouStats.Wins || jSum[0].WithYouStats.AvgScore != sSum[0].WithYouStats.AvgScore {
		t.Fatalf("split stats parity mismatch: json=%+v sqlite=%+v", jSum[0].WithYouStats, sSum[0].WithYouStats)
	}

	// Concurrent reads and writes must not race or error on JSONStore.
	var wg sync.WaitGroup
	for w := 0; w < 6; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for step := 0; step < 5; step++ {
				g := fmt.Sprintf("conc-%d-%d", id, step)
				_ = js2.UpsertDiscoveredMatches(ctx, []*MatchRecord{{MatchGUID: g, RecordStartTimestamp: now.Unix(), Playlist: 11}})
				_, _, _ = js2.ListHistoricalMatches(ctx, MatchFilter{Limit: 5})
			}
		}(w)
	}
	wg.Wait()
}
