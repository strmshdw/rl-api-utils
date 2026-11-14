package e2e

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/ballchasing"
	"github.com/dank/rl-api-utils/internal/psynet"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/dank/rl-api-utils/internal/syncer"
	"github.com/dank/rl-api-utils/internal/testutil"
	"github.com/dank/rlapi"
)

// ============================================================================
// SECTION 2: Database Contention & Transaction Rollback Verification
// ============================================================================

// TestTier5_Adv2_SQLiteStore_HighConcurrencyContention spawns 20 concurrent goroutines
// executing simultaneous upserts, status transitions, queries, and recovery operations
// against a single on-disk SQLite database file.
func TestTier5_Adv2_SQLiteStore_HighConcurrencyContention(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "contention.db")

	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Seed initial matches
	initial := []*storage.MatchRecord{
		{MatchGUID: "seed-1", ReplayURL: "http://cdn/seed-1.replay", RecordStartTimestamp: 1000},
		{MatchGUID: "seed-2", ReplayURL: "http://cdn/seed-2.replay", RecordStartTimestamp: 2000},
		{MatchGUID: "seed-3", ReplayURL: "http://cdn/seed-3.replay", RecordStartTimestamp: 3000},
	}
	if err := store.UpsertDiscoveredMatches(ctx, initial); err != nil {
		t.Fatalf("initial upsert failed: %v", err)
	}

	const goroutines = 20
	const iterations = 15

	var wg sync.WaitGroup
	wg.Add(goroutines)

	var errCount int64

	for i := 0; i < goroutines; i++ {
		workerID := i
		go func() {
			defer wg.Done()
			for iter := 0; iter < iterations; iter++ {
				guid := fmt.Sprintf("worker-%d-match-%d", workerID, iter)

				// Operation A: Upsert new match
				err := store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{
					{
						MatchGUID:            guid,
						ReplayURL:            fmt.Sprintf("http://cdn/%s.replay", guid),
						RecordStartTimestamp: time.Now().Unix(),
					},
				})
				if err != nil {
					atomic.AddInt64(&errCount, 1)
					continue
				}

				// Operation B: Transition to Downloading then Downloaded
				_ = store.MarkDownloading(ctx, guid)
				_ = store.MarkDownloaded(ctx, guid, fmt.Sprintf("/tmp/%s.replay", guid))

				// Operation C: Transition to Uploading then Uploaded
				_ = store.MarkUploading(ctx, guid)
				_ = store.MarkUploaded(ctx, guid, fmt.Sprintf("bc-%s", guid), "http://loc")

				// Operation D: Concurrent read queries
				_, _ = store.GetMatch(ctx, guid)
				_, _ = store.ListPendingDownloads(ctx)
				_, _ = store.ListPendingUploads(ctx)

				// Operation E: Periodic recovery attempt
				if iter%5 == 0 {
					_ = store.RecoverInFlight(ctx)
				}
			}
		}()
	}

	wg.Wait()

	if errCount > 0 {
		t.Fatalf("encountered %d unhandled errors under SQLite database contention", errCount)
	}

	// Verify database integrity by reading back matches
	sample, err := store.GetMatch(ctx, "worker-0-match-0")
	if err != nil {
		t.Fatalf("failed to retrieve sample match: %v", err)
	}
	if sample.UploadStatus != storage.UploadUploaded {
		t.Fatalf("expected sample to be UPLOADED, got %s", sample.UploadStatus)
	}
}

// TestTier5_Adv2_SQLiteStore_TransactionRollback_OnContextCancel verifies that
// when an upsert transaction encounters a pre-canceled context, it rolls back
// cleanly without leaving partially committed records.
func TestTier5_Adv2_SQLiteStore_TransactionRollback_OnContextCancel(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "rollback.db")

	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite store: %v", err)
	}
	defer store.Close()

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel() // Immediately canceled

	matches := []*storage.MatchRecord{
		{MatchGUID: "rollback-1", ReplayURL: "http://cdn/1.replay", RecordStartTimestamp: 100},
		{MatchGUID: "rollback-2", ReplayURL: "http://cdn/2.replay", RecordStartTimestamp: 200},
	}

	err = store.UpsertDiscoveredMatches(canceledCtx, matches)
	if err == nil {
		t.Fatal("expected error on canceled context upsert, got nil")
	}

	// Verify rollback: none of the matches should exist in the database
	liveCtx := context.Background()
	m1, err := store.GetMatch(liveCtx, "rollback-1")
	if !errors.Is(err, storage.ErrMatchNotFound) && m1 != nil {
		t.Fatalf("expected rollback-1 to not exist after rollback, got: %+v (err: %v)", m1, err)
	}

	m2, err := store.GetMatch(liveCtx, "rollback-2")
	if !errors.Is(err, storage.ErrMatchNotFound) && m2 != nil {
		t.Fatalf("expected rollback-2 to not exist after rollback, got: %+v (err: %v)", m2, err)
	}
}

// TestTier5_Adv2_SQLiteStore_BatchUpsert_DuplicateGUIDsInSameBatch tests handling
// when an incoming slice contains duplicate GUIDs in the same upsert batch.
func TestTier5_Adv2_SQLiteStore_BatchUpsert_DuplicateGUIDsInSameBatch(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "dup_batch.db")

	store, err := storage.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	batch := []*storage.MatchRecord{
		{MatchGUID: "dup-guid-1", ReplayURL: "", RecordStartTimestamp: 1000},
		{MatchGUID: "dup-guid-1", ReplayURL: "http://cdn/dup-1.replay", RecordStartTimestamp: 1000}, // second has replay url
	}

	err = store.UpsertDiscoveredMatches(ctx, batch)
	if err != nil {
		t.Fatalf("batch with internal duplicates should succeed with upsert ON CONFLICT, got: %v", err)
	}

	rec, err := store.GetMatch(ctx, "dup-guid-1")
	if err != nil {
		t.Fatalf("failed to retrieve match: %v", err)
	}
	// The replay URL should have been updated by the second item
	if rec.ReplayURL != "http://cdn/dup-1.replay" {
		t.Fatalf("expected updated replay URL http://cdn/dup-1.replay, got %q", rec.ReplayURL)
	}
}

// TestTier5_Adv2_JSONStore_ConcurrentReadWriteContention tests thread safety and
// atomic persistence of JSONStore under heavy concurrent access from 25 goroutines.
func TestTier5_Adv2_JSONStore_ConcurrentReadWriteContention(t *testing.T) {
	tempDir := t.TempDir()
	jsonPath := filepath.Join(tempDir, "concurrent_state.json")

	store, err := storage.NewJSONStore(jsonPath)
	if err != nil {
		t.Fatalf("NewJSONStore failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	const workers = 25
	const iterations = 10

	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		workerID := i
		go func() {
			defer wg.Done()
			for iter := 0; iter < iterations; iter++ {
				guid := fmt.Sprintf("json-worker-%d-%d", workerID, iter)

				_ = store.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{
					{
						MatchGUID:            guid,
						ReplayURL:            fmt.Sprintf("http://cdn/%s.replay", guid),
						RecordStartTimestamp: time.Now().Unix(),
					},
				})

				_ = store.MarkDownloading(ctx, guid)
				_ = store.MarkDownloaded(ctx, guid, fmt.Sprintf("/tmp/%s.replay", guid))
				_ = store.MarkUploading(ctx, guid)
				_ = store.MarkUploaded(ctx, guid, fmt.Sprintf("bc-%s", guid), "http://loc")

				_, _ = store.GetMatch(ctx, guid)
				_, _ = store.ListPendingDownloads(ctx)
				_, _ = store.ListPendingUploads(ctx)
			}
		}()
	}

	wg.Wait()

	// Verify that the JSON file on disk is valid and parseable
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("failed to read persisted json file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("persisted json file is empty")
	}

	// Verify clean reload into a new JSONStore instance
	reloaded, err := storage.NewJSONStore(jsonPath)
	if err != nil {
		t.Fatalf("failed to reload persisted JSONStore: %v", err)
	}
	defer reloaded.Close()

	rec, err := reloaded.GetMatch(ctx, "json-worker-0-0")
	if err != nil || rec == nil {
		t.Fatalf("failed to retrieve match from reloaded JSONStore: %v", err)
	}
	if rec.UploadStatus != storage.UploadUploaded {
		t.Fatalf("expected UPLOADED status, got %s", rec.UploadStatus)
	}
}

// TestTier5_Adv2_Stores_ClosedStore_RejectsOperations verifies that once Close()
// is called on JSONStore and SQLiteStore, subsequent operations return appropriate errors.
func TestTier5_Adv2_Stores_ClosedStore_RejectsOperations(t *testing.T) {
	tempDir := t.TempDir()

	// 1. JSONStore closed behavior
	jsonStore, _ := storage.NewJSONStore(filepath.Join(tempDir, "closed.json"))
	_ = jsonStore.Close()

	ctx := context.Background()
	_, err := jsonStore.GetMatch(ctx, "any")
	if !errors.Is(err, storage.ErrStoreClosed) {
		t.Fatalf("expected ErrStoreClosed for JSONStore.GetMatch, got: %v", err)
	}
	err = jsonStore.UpsertDiscoveredMatches(ctx, []*storage.MatchRecord{{MatchGUID: "m1"}})
	if !errors.Is(err, storage.ErrStoreClosed) {
		t.Fatalf("expected ErrStoreClosed for JSONStore.UpsertDiscoveredMatches, got: %v", err)
	}

	// 2. SQLiteStore closed behavior
	sqliteStore, _ := storage.NewSQLiteStore(filepath.Join(tempDir, "closed.db"))
	_ = sqliteStore.Close()

	_, err = sqliteStore.GetMatch(ctx, "any")
	if err == nil {
		t.Fatal("expected error on closed SQLiteStore.GetMatch, got nil")
	}
}

// ============================================================================
// SECTION 3: Transparent Reconnects on Connection Drops During Long Sync Cycles
// ============================================================================

// adversarialMockRPC implements psynet.RPCClient with fault injection for reconnect testing.
type adversarialMockRPC struct {
	mu           sync.Mutex
	entries      []rlapi.MatchEntry
	failNextCall error
	connected    bool
	callCount    int
	closed       bool
}

func (m *adversarialMockRPC) GetMatchHistory(ctx context.Context) ([]rlapi.MatchEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++

	if m.failNextCall != nil {
		err := m.failNextCall
		m.failNextCall = nil // Clear after one trigger
		m.connected = false  // Drop connection
		return nil, err
	}

	return m.entries, nil
}

func (m *adversarialMockRPC) IsConnected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected && !m.closed
}

func (m *adversarialMockRPC) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.connected = false
	return nil
}

// TestTier5_Adv3_PsyNet_TransparentReconnect_OnConnectionDrop verifies that when
// PsyNet WebSocket RPC experiences an unexpected connection drop (ErrConnectionClosed or EOF),
// psynet.Client transparently reconnects via connectLocked and retries the query successfully.
func TestTier5_Adv3_PsyNet_TransparentReconnect_OnConnectionDrop(t *testing.T) {
	reconnectAttempts := 0

	// Factory that returns a failing RPC on attempt 1, and a working RPC on attempt 2 (reconnect)
	factory := func(ctx context.Context, creds *psynet.Credentials) (psynet.RPCClient, error) {
		reconnectAttempts++
		if reconnectAttempts == 1 {
			// First connection drops with ErrConnectionClosed
			return &adversarialMockRPC{
				failNextCall: rlapi.ErrConnectionClosed,
				connected:    true,
			}, nil
		}
		// Second connection (reconnect) succeeds with match entries
		return &adversarialMockRPC{
			connected: true,
			entries: []rlapi.MatchEntry{
				{
					Match: rlapi.Match{
						MatchGUID:            "reconnect-guid-1",
						RecordStartTimestamp: 1700000000,
						MapName:              "Beckwith_P",
						Playlist:             2,
					},
					ReplayUrl: "http://cdn/rec-1.replay",
				},
			},
		}, nil
	}

	client, err := psynet.NewClient(psynet.ClientConfig{
		Credentials: &psynet.Credentials{
			Platform:  "Epic",
			AuthToken: "token",
			AccountID: "acc",
		},
		RPCFactory: factory,
		Logger:     slog.Default(),
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer client.Close()

	matches, err := client.GetRecentMatches(context.Background())
	if err != nil {
		t.Fatalf("GetRecentMatches should transparently reconnect and succeed, got error: %v", err)
	}

	if len(matches) != 1 || matches[0].MatchGUID != "reconnect-guid-1" {
		t.Fatalf("unexpected matches after reconnect: %+v", matches)
	}
	if reconnectAttempts != 2 {
		t.Fatalf("expected exactly 2 connections (initial + reconnect), got %d", reconnectAttempts)
	}
}

// TestTier5_Adv3_PsyNet_ReconnectFailure_PropagatesDescriptiveError verifies that
// if the connection drops AND the reconnect attempt also fails, psynet.Client
// returns a descriptive compounded error without leaking state or panicking.
func TestTier5_Adv3_PsyNet_ReconnectFailure_PropagatesDescriptiveError(t *testing.T) {
	reconnectAttempts := 0

	factory := func(ctx context.Context, creds *psynet.Credentials) (psynet.RPCClient, error) {
		reconnectAttempts++
		if reconnectAttempts == 1 {
			return &adversarialMockRPC{
				failNextCall: rlapi.ErrConnectionClosed,
				connected:    true,
			}, nil
		}
		// Reconnect fails with network error
		return nil, errors.New("dial tcp 127.0.0.1: connection refused")
	}

	client, err := psynet.NewClient(psynet.ClientConfig{
		Credentials: &psynet.Credentials{
			Platform:  "Epic",
			AuthToken: "token",
			AccountID: "acc",
		},
		RPCFactory: factory,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer client.Close()

	_, err = client.GetRecentMatches(context.Background())
	if err == nil {
		t.Fatal("expected error when reconnect fails, got nil")
	}
	if !strings.Contains(err.Error(), "reconnect failed") {
		t.Fatalf("expected error message to mention 'reconnect failed', got: %v", err)
	}
}

// TestTier5_Adv3_PsyNet_ClientClosedDuringReconnect_ReturnsErrClientClosed verifies
// that if Close() is called while a reconnect is in flight, ErrClientClosed is returned.
func TestTier5_Adv3_PsyNet_ClientClosedDuringReconnect_ReturnsErrClientClosed(t *testing.T) {
	client, _ := psynet.NewClient(psynet.ClientConfig{
		Credentials: &psynet.Credentials{Platform: "Epic", AuthToken: "t", AccountID: "a"},
		RPCFactory: func(ctx context.Context, creds *psynet.Credentials) (psynet.RPCClient, error) {
			return &adversarialMockRPC{connected: true}, nil
		},
	})

	_ = client.Close()

	_, err := client.GetRecentMatches(context.Background())
	if !errors.Is(err, psynet.ErrClientClosed) {
		t.Fatalf("expected ErrClientClosed on closed client, got: %v", err)
	}
}

// TestTier5_Adv3_Syncer_MultiCycleSelfHealing_AfterPsyNetDrop tests the syncer orchestrator
// recovering across cycles: Cycle 1 encounters a PsyNet drop and fails; Cycle 2 heals and completes.
func TestTier5_Adv3_Syncer_MultiCycleSelfHealing_AfterPsyNetDrop(t *testing.T) {
	tempDir := t.TempDir()
	store, _ := storage.NewStore(filepath.Join(tempDir, "state.json"))
	defer store.Close()

	cdn := testutil.NewMockCDNServer()
	defer cdn.Close()
	bc := testutil.NewMockBallchasingServer()
	defer bc.Close()

	providerFail := true
	mockProvider := &dynamicMockProvider{
		fn: func(ctx context.Context) ([]syncer.DiscoveredMatch, error) {
			if providerFail {
				return nil, errors.New("psynet rpc connection reset")
			}
			return []syncer.DiscoveredMatch{
				{
					MatchGUID:            "self-heal-1",
					RecordStartTimestamp: time.Now().Unix(),
					MapName:              "Park_P",
					Playlist:             2,
					ReplayURL:            cdn.ReplayURL("self-heal-1"),
				},
			}, nil
		},
	}

	downloader := psynet.NewDownloader()
	uploader, _ := ballchasing.NewClient(ballchasing.ClientConfig{
		BaseURL: bc.URL(),
		APIKey:  "test-ballchasing-token",
	})

	s, err := syncer.New(store, mockProvider, downloader, uploader,
		syncer.WithReplayDir(filepath.Join(tempDir, "replays")),
	)
	if err != nil {
		t.Fatalf("syncer.New failed: %v", err)
	}

	// Cycle 1: PsyNet fails -> Cycle returns error
	_, err = s.RunCycle(context.Background())
	if err == nil {
		t.Fatal("expected Cycle 1 to fail due to psynet drop, got nil error")
	}

	// Self-heal: PsyNet reconnects for Cycle 2
	providerFail = false

	stats2, err := s.RunCycle(context.Background())
	if err != nil {
		t.Fatalf("Cycle 2 should have healed and succeeded, got: %v", err)
	}
	if stats2.UploadedCount != 1 {
		t.Fatalf("expected 1 upload in healed Cycle 2, got %d", stats2.UploadedCount)
	}
}

type dynamicMockProvider struct {
	fn func(ctx context.Context) ([]syncer.DiscoveredMatch, error)
}

func (p *dynamicMockProvider) GetRecentMatches(ctx context.Context) ([]syncer.DiscoveredMatch, error) {
	return p.fn(ctx)
}

func (p *dynamicMockProvider) Close() error {
	return nil
}
