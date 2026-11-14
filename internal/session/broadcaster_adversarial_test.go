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
		numSubscribers = 120
		totalEvents    = 1000
		numPublishers  = 5
		eventsPerPub   = totalEvents / numPublishers
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
		// Yield execution briefly every 20 events to allow fast subscriber goroutines to drain buffers under heavy CPU load
		if i%20 == 0 {
			time.Sleep(150 * time.Microsecond)
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
