package session_test

import (
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dank/rl-api-utils/internal/playertrack"
	"github.com/dank/rl-api-utils/internal/session"
)

// TestM3_SSEClockDelivery_RapidBurstsAndChurn tests high-frequency live game clock
// updates broadcast via SSE across 20 concurrent subscribers with churn, negative times,
// and overtime transitions without race conditions or dropped connections.
func TestM3_SSEClockDelivery_RapidBurstsAndChurn(t *testing.T) {
	tracker := session.NewSessionTracker()
	broadcaster := tracker.Broadcaster()
	defer broadcaster.Close()

	const totalUpdates = 500
	const concurrentSubscribers = 20

	var wg sync.WaitGroup
	var receivedCount int64
	var malformedCount int64
	stopCh := make(chan struct{})

	// Launch 20 concurrent subscribers consuming SSE frames
	for s := 0; s < concurrentSubscribers; s++ {
		wg.Add(1)
		go func(subID int) {
			defer wg.Done()
			ch, cancelSub := tracker.Subscribe()
			defer cancelSub()

			for {
				select {
				case <-stopCh:
					return
				case evt, ok := <-ch:
					if !ok {
						return
					}
					if evt.Event == session.EventMatchUpdate {
						atomic.AddInt64(&receivedCount, 1)

						// Validate SSE formatting under race detector
						formatted, err := evt.FormatSSE()
						if err != nil {
							atomic.AddInt64(&malformedCount, 1)
							continue
						}
						raw := string(formatted)
						if !strings.HasPrefix(raw, "event: match_update\ndata: ") || !strings.HasSuffix(raw, "\n\n") {
							atomic.AddInt64(&malformedCount, 1)
						}
					}
				}
			}
		}(s)
	}

	// Rapidly pump 500 match updates with dynamic clock states
	startTime := time.Now()
	for i := 0; i < totalUpdates; i++ {
		timeRemaining := 300 - i
		isOT := false
		if i >= 300 {
			timeRemaining = i - 300
			isOT = true
		} else if i < 10 {
			// Negative time during kickoff countdown
			timeRemaining = -1 * (i + 1)
		}

		team0 := 0
		match := &playertrack.CurrentMatchResponse{
			ActiveMatch:   true,
			MatchGUID:     "clock-stress-guid",
			PlaylistID:    13,
			PlaylistName:  "Ranked Standard",
			TimeRemaining: timeRemaining,
			IsOvertime:    isOT,
			LocalTeam:     &team0,
			LocalPlayerIDs: []string{"Steam|76561198000000001|0"},
			LocalPlayer: &playertrack.LobbyPlayer{
				PlayerID: "Steam|76561198000000001|0",
				Platform: "steam",
				Name:     "LivePlayer",
				TeamNum:  0,
				IsLocal:  true,
				Stats: playertrack.PlayerStatsSummary{
					Score: i * 5,
					Goals: i / 100,
				},
			},
			UpdatedAt: time.Now().UTC(),
		}

		tracker.OnActiveMatchUpdated(match)
		if i%50 == 0 {
			time.Sleep(1 * time.Millisecond)
		}
	}

	t.Logf("Dispatched %d match clock updates in %v", totalUpdates, time.Since(startTime))

	// Allow subscribers to drain
	time.Sleep(200 * time.Millisecond)
	close(stopCh)
	wg.Wait()

	totalRecv := atomic.LoadInt64(&receivedCount)
	malformed := atomic.LoadInt64(&malformedCount)

	if malformed > 0 {
		t.Fatalf("encountered %d malformed SSE frames", malformed)
	}

	if totalRecv < int64(totalUpdates) {
		t.Fatalf("expected at least %d events received across subscribers, got %d", totalUpdates, totalRecv)
	}

	t.Logf("Successfully received and formatted %d SSE frames with zero malformed frames", totalRecv)
}

// TestM3_SSEClockDelivery_JSONDecodability ensures that emitted match_update SSE payloads
// correctly decode into CurrentMatchResponse with time_remaining and is_overtime preserved.
func TestM3_SSEClockDelivery_JSONDecodability(t *testing.T) {
	tracker := session.NewSessionTracker()
	defer tracker.Broadcaster().Close()

	subCh, cancelSub := tracker.Subscribe()
	defer cancelSub()

	team0 := 0
	match := &playertrack.CurrentMatchResponse{
		ActiveMatch:   true,
		MatchGUID:     "clock-json-guid",
		PlaylistID:    13,
		TimeRemaining: -10, // uninitialized / countdown
		IsOvertime:    true,
		LocalTeam:     &team0,
		LocalPlayerIDs: []string{"Epic|test-epic-id|0"},
	}

	tracker.OnActiveMatchUpdated(match)

	select {
	case evt := <-subCh:
		if evt.Event != session.EventMatchUpdate {
			t.Fatalf("expected event match_update, got %s", evt.Event)
		}
		formatted, err := evt.FormatSSE()
		if err != nil {
			t.Fatalf("FormatSSE error: %v", err)
		}

		lines := strings.Split(strings.TrimSpace(string(formatted)), "\n")
		if len(lines) < 2 {
			t.Fatalf("unexpected line count: %d", len(lines))
		}
		dataJSON := strings.TrimPrefix(lines[1], "data: ")

		var decoded playertrack.CurrentMatchResponse
		if err := json.Unmarshal([]byte(dataJSON), &decoded); err != nil {
			t.Fatalf("failed to unmarshal SSE payload: %v", err)
		}

		if decoded.TimeRemaining != -10 {
			t.Errorf("expected TimeRemaining=-10, got %d", decoded.TimeRemaining)
		}
		if !decoded.IsOvertime {
			t.Errorf("expected IsOvertime=true, got %v", decoded.IsOvertime)
		}
		if len(decoded.LocalPlayerIDs) != 1 || decoded.LocalPlayerIDs[0] != "Epic|test-epic-id|0" {
			t.Errorf("expected LocalPlayerIDs, got %v", decoded.LocalPlayerIDs)
		}

	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for match_update event")
	}
}
