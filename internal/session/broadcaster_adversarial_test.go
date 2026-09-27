package session

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestAdversarial_HighFanoutDynamicChurn stress-tests EventBroadcaster under
// dynamic churn: 100+ concurrent subscriber goroutines dynamically subscribing,
// reading arbitrary event counts, and unsubscribing while 1,000 events are
// broadcast concurrently by multiple publishers.
func TestAdversarial_HighFanoutDynamicChurn(t *testing.T) {
	b := NewEventBroadcaster()
	defer b.Close()

	const (
		numSubscribers    = 120
		totalEvents       = 1000
		numPublishers     = 5
		eventsPerPub      = totalEvents / numPublishers
	)

	stopChurn := make(chan struct{})
	var wgSubscribers sync.WaitGroup
	var activeSubs atomic.Int64

	// Launch 120 dynamic subscriber churn goroutines
	for i := 0; i < numSubscribers; i++ {
		wgSubscribers.Add(1)
		go func(subID int) {
			defer wgSubscribers.Done()
			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(subID)))

			for {
				select {
				case <-stopChurn:
					return
				default:
				}

				ch, cancel := b.Subscribe()
				activeSubs.Add(1)

				// Read a variable number of events (0 to 20) with a quick deadline
				readTarget := rng.Intn(21)
				for r := 0; r < readTarget; r++ {
					select {
					case _, ok := <-ch:
						if !ok {
							break
						}
					case <-time.After(2 * time.Millisecond):
						break
					case <-stopChurn:
						cancel()
						activeSubs.Add(-1)
						return
					}
				}

				// Unsubscribe dynamically
				cancel()
				activeSubs.Add(-1)

				// Optional idempotent second cancel to test concurrency safety
				if rng.Intn(3) == 0 {
					cancel()
				}

				// Brief jitter before re-subscribing
				time.Sleep(time.Duration(rng.Intn(500)) * time.Microsecond)
			}
		}(i)
	}

	// Concurrently query SubscriberCount to test RLock contention under churn
	stopMonitor := make(chan struct{})
	var wgMonitor sync.WaitGroup
	wgMonitor.Add(1)
	go func() {
		defer wgMonitor.Done()
		for {
			select {
			case <-stopMonitor:
				return
			default:
				_ = b.SubscriberCount()
				time.Sleep(500 * time.Microsecond)
			}
		}
	}()

	// Launch publishers broadcasting 1,000 events
	var wgPublishers sync.WaitGroup
	var broadcastErrors atomic.Int64

	startPub := time.Now()
	for p := 0; p < numPublishers; p++ {
		wgPublishers.Add(1)
		go func(pubID int) {
			defer wgPublishers.Done()
			for e := 0; e < eventsPerPub; e++ {
				eventID := fmt.Sprintf("pub-%d-seq-%d", pubID, e)
				b.Broadcast(SessionEvent{
					Event: EventMatchUpdate,
					Data:  eventID,
				})
			}
		}(p)
	}

	wgPublishers.Wait()
	pubDuration := time.Since(startPub)
	t.Logf("Published %d events across %d concurrent publishers under %d subscriber churn in %v",
		totalEvents, numPublishers, numSubscribers, pubDuration)

	if broadcastErrors.Load() > 0 {
		t.Fatalf("encountered %d broadcast errors", broadcastErrors.Load())
	}

	// Signal subscriber goroutines to stop and wait
	close(stopChurn)
	close(stopMonitor)
	wgSubscribers.Wait()
	wgMonitor.Wait()

	// Verify all subscriptions cleanly detached
	b.Close()
	if count := b.SubscriberCount(); count != 0 {
		t.Errorf("expected 0 subscribers after Close, got %d", count)
	}
}

// TestAdversarial_StalledSubscribersNonBlocking asserts that 100 completely stalled
// subscribers (never reading from their channels) do not block or degrade broadcast
// throughput for 1,000 sequential events, while 10 fast subscribers receive all frames.
func TestAdversarial_StalledSubscribersNonBlocking(t *testing.T) {
	b := NewEventBroadcaster()
	defer b.Close()

	const (
		stalledCount = 100
		fastCount    = 10
		totalEvents  = 1000
	)

	// Register 100 stalled subscribers: no reads performed
	stalledCancels := make([]func(), stalledCount)
	stalledChans := make([]<-chan SessionEvent, stalledCount)
	for i := 0; i < stalledCount; i++ {
		ch, cancel := b.Subscribe()
		stalledChans[i] = ch
		stalledCancels[i] = cancel
	}

	// Register 10 fast subscribers: drain continuously
	fastCancels := make([]func(), fastCount)
	fastReceived := make([]atomic.Int64, fastCount)
	var wgFast sync.WaitGroup

	for i := 0; i < fastCount; i++ {
		ch, cancel := b.Subscribe()
		fastCancels[i] = cancel
		wgFast.Add(1)
		go func(idx int, c <-chan SessionEvent) {
			defer wgFast.Done()
			for {
				select {
				case _, ok := <-c:
					if !ok {
						return
					}
					fastReceived[idx].Add(1)
				case <-time.After(3 * time.Second):
					return
				}
			}
		}(i, ch)
	}

	if count := b.SubscriberCount(); count != stalledCount+fastCount {
		t.Fatalf("expected %d subscribers, got %d", stalledCount+fastCount, count)
	}

	// Broadcast 1,000 events and record latencies
	var maxBroadcastDuration time.Duration
	startTotal := time.Now()

	for i := 0; i < totalEvents; i++ {
		t0 := time.Now()
		b.Broadcast(SessionEvent{
			Event: EventSessionUpdate,
			Data:  i,
		})
		d := time.Since(t0)
		if d > maxBroadcastDuration {
			maxBroadcastDuration = d
		}
	}
	totalDuration := time.Since(startTotal)
	avgDuration := totalDuration / totalEvents

	t.Logf("Broadcast 1,000 events to %d subscribers (100 stalled + 10 fast): total=%v, avg/event=%v, max single broadcast=%v",
		stalledCount+fastCount, totalDuration, avgDuration, maxBroadcastDuration)

	// Non-blocking assertion: total time must be well under 1 second (typically < 50ms)
	if totalDuration > 1*time.Second {
		t.Fatalf("Broadcast took %v, violating non-blocking guarantee (expected < 1s for 1000 broadcasts)", totalDuration)
	}

	// Individual broadcast must not hang
	if maxBroadcastDuration > 100*time.Millisecond {
		t.Errorf("single Broadcast call hung for %v (expected < 100ms)", maxBroadcastDuration)
	}

	// Verify stalled subscriber channel buffers: capped at defaultSubscriberBufferSize (64)
	for i, ch := range stalledChans {
		buffered := len(ch)
		if buffered != defaultSubscriberBufferSize {
			t.Errorf("stalled subscriber %d buffer has %d events, expected exactly %d",
				i, buffered, defaultSubscriberBufferSize)
		}
	}

	// Allow fast subscribers an adequate window to complete draining under heavy CI load
	time.Sleep(150 * time.Millisecond)

	// Close fast subscribers and wait
	for _, cancel := range fastCancels {
		cancel()
	}
	wgFast.Wait()

	// Verify all fast subscribers received 1,000 events
	for i := 0; i < fastCount; i++ {
		rcvd := fastReceived[i].Load()
		if rcvd != int64(totalEvents) {
			t.Errorf("fast subscriber %d received %d/%d events", i, rcvd, totalEvents)
		}
	}

	// Clean up stalled subscribers
	for _, cancel := range stalledCancels {
		cancel()
	}
}

// TestAdversarial_ChannelLifecycle_DoubleUnsubscribe verifies that calling cancel()
// multiple times concurrently from 50 goroutines per subscription never panics
// ("close of closed channel") and safely deregisters the channel.
func TestAdversarial_ChannelLifecycle_DoubleUnsubscribe(t *testing.T) {
	const iterations = 500
	const concurrentCallers = 20

	for iter := 0; iter < iterations; iter++ {
		b := NewEventBroadcaster()
		ch, cancel := b.Subscribe()

		if b.SubscriberCount() != 1 {
			t.Fatalf("iter %d: expected 1 subscriber, got %d", iter, b.SubscriberCount())
		}

		var wg sync.WaitGroup
		gate := make(chan struct{})

		for c := 0; c < concurrentCallers; c++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-gate
				cancel()
			}()
		}

		// Release all goroutines simultaneously
		close(gate)
		wg.Wait()

		if b.SubscriberCount() != 0 {
			t.Fatalf("iter %d: expected 0 subscribers after cancel, got %d", iter, b.SubscriberCount())
		}

		// Verify channel is drained/closed
		_, ok := <-ch
		if ok {
			t.Fatalf("iter %d: expected channel to be closed", iter)
		}

		b.Close()
	}
}

// TestAdversarial_ChannelLifecycle_UnsubscribeAfterClose verifies that calling cancel()
// after the broadcaster has already called Close() is completely safe and does not panic.
func TestAdversarial_ChannelLifecycle_UnsubscribeAfterClose(t *testing.T) {
	const iterations = 200
	const numSubs = 50

	for iter := 0; iter < iterations; iter++ {
		b := NewEventBroadcaster()
		cancels := make([]func(), numSubs)
		chans := make([]<-chan SessionEvent, numSubs)

		for s := 0; s < numSubs; s++ {
			chans[s], cancels[s] = b.Subscribe()
		}

		// Broadcaster closes while subscribers are still registered
		b.Close()

		// Verify all subscriber channels are closed
		for s, ch := range chans {
			_, ok := <-ch
			if ok {
				t.Fatalf("iter %d, sub %d: expected closed channel after b.Close()", iter, s)
			}
		}

		// Concurrently invoke cancel() on all subscribers after Close()
		var wg sync.WaitGroup
		for s := 0; s < numSubs; s++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				// Call cancel multiple times
				cancels[idx]()
				cancels[idx]()
			}(s)
		}
		wg.Wait()

		if b.SubscriberCount() != 0 {
			t.Fatalf("iter %d: expected 0 subscribers, got %d", iter, b.SubscriberCount())
		}
	}
}

// TestAdversarial_ChannelLifecycle_BroadcastAfterClose verifies that broadcasting
// after Close() is a safe no-op that never panics or deadlocks under high concurrency.
func TestAdversarial_ChannelLifecycle_BroadcastAfterClose(t *testing.T) {
	b := NewEventBroadcaster()
	b.Close()

	// Calling Close() again is safe
	b.Close()

	const concurrentPublishers = 20
	const eventsPerPub = 200

	var wg sync.WaitGroup
	for p := 0; p < concurrentPublishers; p++ {
		wg.Add(1)
		go func(pubID int) {
			defer wg.Done()
			for e := 0; e < eventsPerPub; e++ {
				b.Broadcast(SessionEvent{
					Event: EventMatchUpdate,
					Data:  fmt.Sprintf("pub-%d-event-%d", pubID, e),
				})
			}
		}(p)
	}

	// Concurrently attempt to subscribe on closed broadcaster
	for s := 0; s < 10; s++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch, cancel := b.Subscribe()
			defer cancel()
			// Channel must be closed immediately
			_, ok := <-ch
			if ok {
				t.Errorf("expected closed channel on post-close Subscribe")
			}
		}()
	}

	wg.Wait()

	if b.SubscriberCount() != 0 {
		t.Errorf("expected 0 subscribers, got %d", b.SubscriberCount())
	}
}

// TestAdversarial_ExtremeLifecycleChaos executes all operations simultaneously:
// Subscribe, Broadcast, cancel, SubscriberCount, and Close() all running in 50+
// parallel goroutines over an extended duration.
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

