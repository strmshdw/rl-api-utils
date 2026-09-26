package statsapi_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/statsapi"
	"github.com/dank/rl-api-utils/internal/storage"
	"github.com/gorilla/websocket"
)

// mockStore implements MatchChecker in-memory for testing.
type mockStore struct {
	mu      sync.Mutex
	matches map[string]*storage.MatchRecord
}

func newMockStore() *mockStore {
	return &mockStore{
		matches: make(map[string]*storage.MatchRecord),
	}
}

func (m *mockStore) SaveMatch(match *storage.MatchRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.matches[match.MatchGUID] = match
}

func (m *mockStore) GetMatch(ctx context.Context, matchGUID string) (*storage.MatchRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, exists := m.matches[matchGUID]
	if !exists {
		return nil, storage.ErrMatchNotFound
	}
	return record, nil
}

func TestTracker_RecordMatch_Evaluation(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()

	// Seed one already downloaded match
	now := time.Now()
	store.SaveMatch(&storage.MatchRecord{
		MatchGUID:      "already-downloaded-guid",
		DownloadStatus: storage.DownloadDownloaded,
		DownloadedAt:   &now,
	})

	// Seed one already uploaded duplicate match
	store.SaveMatch(&storage.MatchRecord{
		MatchGUID:    "already-uploaded-guid",
		UploadStatus: storage.UploadDuplicate,
	})

	notifier := statsapi.NewNoopToastNotifier()
	cfg := statsapi.TrackerConfig{
		TriggerThreshold:   3,
		ForceSyncOnTrigger: false,
		EnableToast:        true,
	}

	tracker, err := statsapi.NewTracker(store, cfg, statsapi.WithToastNotifier(notifier))
	if err != nil {
		t.Fatalf("unexpected NewTracker error: %v", err)
	}

	// 1. Record already downloaded match -> should not be pending
	isNew, count, err := tracker.RecordMatch(ctx, "already-downloaded-guid")
	if err != nil {
		t.Fatalf("RecordMatch error: %v", err)
	}
	if isNew || count != 0 {
		t.Errorf("expected isNew=false, count=0 for already downloaded match, got isNew=%v, count=%d", isNew, count)
	}

	// 2. Record new match not in store -> should become pending
	isNew, count, err = tracker.RecordMatch(ctx, "new-match-1")
	if err != nil {
		t.Fatalf("RecordMatch error: %v", err)
	}
	if !isNew || count != 1 {
		t.Errorf("expected isNew=true, count=1, got isNew=%v, count=%d", isNew, count)
	}

	// 3. Record duplicate event for same match -> count stays 1, isNew=false
	isNew, count, err = tracker.RecordMatch(ctx, "new-match-1")
	if err != nil {
		t.Fatalf("RecordMatch error: %v", err)
	}
	if isNew || count != 1 {
		t.Errorf("expected isNew=false, count=1 for repeated event, got isNew=%v, count=%d", isNew, count)
	}

	// 4. Record second new match
	isNew, count, err = tracker.RecordMatch(ctx, "new-match-2")
	if err != nil {
		t.Fatalf("RecordMatch error: %v", err)
	}
	if !isNew || count != 2 {
		t.Errorf("expected isNew=true, count=2, got isNew=%v, count=%d", isNew, count)
	}

	// Notifier should not have triggered yet (< 3 threshold)
	if notifier.Count() != 0 {
		t.Errorf("expected 0 toast notifications before threshold, got %d", notifier.Count())
	}

	// 5. Record third new match -> hits threshold (3)
	isNew, count, err = tracker.RecordMatch(ctx, "new-match-3")
	if err != nil {
		t.Fatalf("RecordMatch error: %v", err)
	}
	if !isNew || count != 3 {
		t.Errorf("expected isNew=true, count=3, got isNew=%v, count=%d", isNew, count)
	}

	// Notifier should have triggered
	if notifier.Count() != 1 {
		t.Errorf("expected 1 toast notification when threshold reached, got %d", notifier.Count())
	}
	if !strings.Contains(notifier.Notifications[0].Message, "3 matches in queue") {
		t.Errorf("unexpected toast message: %s", notifier.Notifications[0].Message)
	}
}

func TestTracker_ForceSyncTrigger(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()

	var triggerMu sync.Mutex
	triggerCount := 0
	triggerReason := ""

	triggerFn := func(ctx context.Context, reason string) error {
		triggerMu.Lock()
		defer triggerMu.Unlock()
		triggerCount++
		triggerReason = reason
		return nil
	}

	notifier := statsapi.NewNoopToastNotifier()
	cfg := statsapi.TrackerConfig{
		TriggerThreshold:   2,
		ForceSyncOnTrigger: true,
		EnableToast:        true,
	}

	tracker, err := statsapi.NewTracker(store, cfg,
		statsapi.WithToastNotifier(notifier),
		statsapi.WithSyncTrigger(triggerFn),
	)
	if err != nil {
		t.Fatalf("NewTracker error: %v", err)
	}

	tracker.RecordMatch(ctx, "guid-a")
	tracker.RecordMatch(ctx, "guid-b")

	// Allow goroutine to trigger
	time.Sleep(100 * time.Millisecond)

	triggerMu.Lock()
	count := triggerCount
	reason := triggerReason
	triggerMu.Unlock()

	if count != 1 {
		t.Errorf("expected 1 force-sync trigger invocation, got %d", count)
	}
	if reason != "threshold_2_reached" {
		t.Errorf("unexpected trigger reason: %q", reason)
	}

	// Clear matches
	tracker.ClearMatches([]string{"guid-a", "guid-b"})
	if tracker.PendingCount() != 0 {
		t.Errorf("expected 0 pending matches after ClearMatches, got %d", tracker.PendingCount())
	}
}

func TestListener_WebSocket(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Send MatchCreated event
		msg := statsapi.EventMessage{
			Event: "MatchCreated",
			Data: statsapi.EventData{
				MatchGuid: "ws-match-999",
			},
		}
		data, _ := json.Marshal(msg)
		conn.WriteMessage(websocket.TextMessage, data)

		// Wait briefly then close
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	wsURL := "ws://" + server.Listener.Addr().String()

	store := newMockStore()
	tracker, _ := statsapi.NewTracker(store, statsapi.TrackerConfig{TriggerThreshold: 10})

	listener, err := statsapi.NewListener(statsapi.ListenerConfig{
		Address:        wsURL,
		Protocol:       "websocket",
		ReconnectDelay: 50 * time.Millisecond,
	}, tracker, nil)
	if err != nil {
		t.Fatalf("NewListener error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go listener.Start(ctx)

	// Poll until match is recorded in tracker
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if tracker.PendingCount() > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if tracker.PendingCount() != 1 {
		t.Fatalf("expected 1 pending match recorded from WebSocket, got %d", tracker.PendingCount())
	}
	if guids := tracker.PendingGUIDs(); len(guids) != 1 || guids[0] != "ws-match-999" {
		t.Errorf("unexpected pending match GUIDs: %v", guids)
	}

	listener.Stop()
}

func TestListener_TCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on TCP: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Write MatchEnded event with trailing newline
		msg := statsapi.EventMessage{
			Event: "MatchEnded",
			Data: statsapi.EventData{
				MatchGuid: "tcp-match-888",
			},
		}
		data, _ := json.Marshal(msg)
		conn.Write(append(data, '\n'))
		time.Sleep(100 * time.Millisecond)
	}()

	store := newMockStore()
	tracker, _ := statsapi.NewTracker(store, statsapi.TrackerConfig{TriggerThreshold: 10})

	listener, err := statsapi.NewListener(statsapi.ListenerConfig{
		Address:        ln.Addr().String(),
		Protocol:       "tcp",
		ReconnectDelay: 50 * time.Millisecond,
	}, tracker, nil)
	if err != nil {
		t.Fatalf("NewListener error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go listener.Start(ctx)

	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if tracker.PendingCount() > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if tracker.PendingCount() != 1 {
		t.Fatalf("expected 1 pending match recorded from TCP, got %d", tracker.PendingCount())
	}
	if guids := tracker.PendingGUIDs(); len(guids) != 1 || guids[0] != "tcp-match-888" {
		t.Errorf("unexpected pending match GUIDs: %v", guids)
	}

	listener.Stop()
}
