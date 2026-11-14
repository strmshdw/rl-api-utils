package session

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAdversarial_ExtremeLifecycleChaos(t *testing.T) {
	for round := 0; round < 5; round++ {
		b := NewEventBroadcaster()
		stopCh := make(chan struct{})
		var wg sync.WaitGroup

		// 15 continuous publishers
		for p := 0; p < 15; p++ {
			wg.Add(1)
			go func(pubID int) {
				defer wg.Done()
				seq := 0
				for {
					select {
					case <-stopCh:
						return
					default:
						b.Broadcast(SessionEvent{
							Event: EventMatchUpdate,
							Data:  seq,
						})
						seq++
					}
				}
			}(p)
		}

		// 20 continuous subscribers & unsubscribers
		for s := 0; s < 20; s++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stopCh:
						return
					default:
						ch, cancel := b.Subscribe()
						// Non-blocking read attempts
						for r := 0; r < 5; r++ {
							select {
							case <-ch:
							default:
							}
						}
						cancel()
					}
				}
			}()
		}

		// 5 continuous count monitors
		for m := 0; m < 5; m++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stopCh:
						return
					default:
						_ = b.SubscriberCount()
					}
				}
			}()
		}

		// Let chaos run for 100ms
		time.Sleep(100 * time.Millisecond)

		// Abrupt Close() while publishers and subscribers are active
		b.Close()

		// Let it continue for another 50ms post-close
		time.Sleep(50 * time.Millisecond)

		close(stopCh)
		wg.Wait()
	}
}

// TestAdversarial_ExactBufferDropSemantics validates strict FIFO delivery and
// deterministic dropping once the 64-item channel capacity is reached.
func TestAdversarial_ExactBufferDropSemantics(t *testing.T) {
	b := NewEventBroadcaster()
	defer b.Close()

	ch, cancel := b.Subscribe()
	defer cancel()

	// 1. Broadcast exactly defaultSubscriberBufferSize (64) events
	for i := 1; i <= defaultSubscriberBufferSize; i++ {
		b.Broadcast(SessionEvent{Event: EventMatchUpdate, Data: i})
	}

	if len(ch) != defaultSubscriberBufferSize {
		t.Fatalf("expected buffer length %d, got %d", defaultSubscriberBufferSize, len(ch))
	}

	// 2. Broadcast 20 additional events (65..84) without reading
	// These must be dropped silently without blocking
	t0 := time.Now()
	for i := 65; i <= 84; i++ {
		b.Broadcast(SessionEvent{Event: EventMatchUpdate, Data: i})
	}
	if d := time.Since(t0); d > 10*time.Millisecond {
		t.Errorf("overflow broadcasts took %v, expected non-blocking immediate return", d)
	}

	// Buffer must still contain exactly 64 items
	if len(ch) != defaultSubscriberBufferSize {
		t.Fatalf("expected buffer length to remain %d, got %d", defaultSubscriberBufferSize, len(ch))
	}

	// 3. Drain and assert the first 64 events are preserved in exact order (1..64)
	for expected := 1; expected <= defaultSubscriberBufferSize; expected++ {
		select {
		case ev := <-ch:
			val, ok := ev.Data.(int)
			if !ok || val != expected {
				t.Fatalf("expected event %d, got %v", expected, ev.Data)
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("timed out waiting for event %d", expected)
		}
	}

	// Channel should now be empty
	if len(ch) != 0 {
		t.Fatalf("expected empty channel, got %d items remaining", len(ch))
	}

	// 4. Broadcast a new event (event 100) — must be accepted now that space is available
	b.Broadcast(SessionEvent{Event: EventMatchUpdate, Data: 100})
	if len(ch) != 1 {
		t.Fatalf("expected 1 event in buffer after freeing space, got %d", len(ch))
	}

	ev := <-ch
	if ev.Data.(int) != 100 {
		t.Fatalf("expected event 100, got %v", ev.Data)
	}
}

// TestAdversarial_MassiveScale_500Subscribers_5000Events stress-tests EventBroadcaster
// with 500 concurrent subscribers (250 stalled, 250 draining) receiving 5,000 broadcast
// events from 10 parallel publisher goroutines.
func TestAdversarial_MassiveScale_500Subscribers_5000Events(t *testing.T) {
	b := NewEventBroadcaster()
	defer b.Close()

	const (
		stalledSubs  = 250
		activeSubs   = 250
		numPubs      = 10
		totalEvents  = 5000
		eventsPerPub = totalEvents / numPubs
	)

	// 250 completely stalled subscribers
	stalledCancels := make([]func(), stalledSubs)
	for i := 0; i < stalledSubs; i++ {
		_, cancel := b.Subscribe()
		stalledCancels[i] = cancel
	}

	// 250 active draining subscribers
	activeCancels := make([]func(), activeSubs)
	var activeReceived atomic.Int64
	var wgActive sync.WaitGroup

	for i := 0; i < activeSubs; i++ {
		ch, cancel := b.Subscribe()
		activeCancels[i] = cancel
		wgActive.Add(1)
		go func(c <-chan SessionEvent) {
			defer wgActive.Done()
			for {
				select {
				case _, ok := <-c:
					if !ok {
						return
					}
					activeReceived.Add(1)
				case <-time.After(2 * time.Second):
					return
				}
			}
		}(ch)
	}

	if count := b.SubscriberCount(); count != stalledSubs+activeSubs {
		t.Fatalf("expected %d subscribers, got %d", stalledSubs+activeSubs, count)
	}

	// 10 concurrent publishers broadcasting 5,000 events total
	start := time.Now()
	var wgPubs sync.WaitGroup
	for p := 0; p < numPubs; p++ {
		wgPubs.Add(1)
		go func(pubID int) {
			defer wgPubs.Done()
			for e := 0; e < eventsPerPub; e++ {
				b.Broadcast(SessionEvent{
					Event: EventMatchUpdate,
					Data:  e,
				})
			}
		}(p)
	}

	wgPubs.Wait()
	duration := time.Since(start)

	t.Logf("Massive scale: Broadcast %d events to %d subscribers (250 stalled + 250 active) in %v (avg %v/event)",
		totalEvents, stalledSubs+activeSubs, duration, duration/totalEvents)

	// Total broadcast duration must be well under 5 seconds
	if duration > 5*time.Second {
		t.Errorf("Massive scale broadcast took %v, exceeding 5s threshold", duration)
	}

	// Clean up active subscribers
	for _, cancel := range activeCancels {
		cancel()
	}
	wgActive.Wait()

	// Clean up stalled subscribers
	for _, cancel := range stalledCancels {
		cancel()
	}

	if count := b.SubscriberCount(); count != 0 {
		t.Errorf("expected 0 subscribers after all cancels, got %d", count)
	}
}

// TestAdversarial_MemoryLeak_ChurnProfile verifies that rapid subscriber connect/disconnect
// cycles (5,000 subscriptions) cleanly release map entries and do not leak subscribers.
func TestAdversarial_MemoryLeak_ChurnProfile(t *testing.T) {
	b := NewEventBroadcaster()
	defer b.Close()

	const churnCycles = 5000
	var wg sync.WaitGroup

	for i := 0; i < churnCycles; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch, cancel := b.Subscribe()
			// Send an event
			b.Broadcast(SessionEvent{Event: EventSessionUpdate, Data: "ping"})
			select {
			case <-ch:
			default:
			}
			cancel()
		}()
	}

	wg.Wait()

	// Broadcaster must have exactly 0 subscribers remaining
	if count := b.SubscriberCount(); count != 0 {
		t.Fatalf("subscriber leak detected: %d subscribers remain in registry after %d churn cycles",
			count, churnCycles)
	}
}
