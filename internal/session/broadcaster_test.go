package session

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSessionEvent_FormatSSE(t *testing.T) {
	data := map[string]any{
		"session_id": "test-uuid-1234",
		"wins":       5,
		"rate":       83.3,
	}
	event := SessionEvent{
		Event: EventSessionUpdate,
		Data:  data,
	}

	formatted, err := event.FormatSSE()
	if err != nil {
		t.Fatalf("unexpected FormatSSE error: %v", err)
	}

	raw := string(formatted)
	if !strings.HasPrefix(raw, "event: session_update\ndata: ") {
		t.Fatalf("expected header 'event: session_update\\ndata: ', got:\n%s", raw)
	}
	if !strings.HasSuffix(raw, "\n\n") {
		t.Fatalf("expected trailing double newline '\\n\\n', got:\n%s", raw)
	}

	// Extract json portion
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d:\n%s", len(lines), raw)
	}
	jsonData := strings.TrimPrefix(lines[1], "data: ")

	var decoded map[string]any
	if err := json.Unmarshal([]byte(jsonData), &decoded); err != nil {
		t.Fatalf("failed to decode data payload: %v", err)
	}
	if decoded["session_id"] != "test-uuid-1234" {
		t.Errorf("expected session_id 'test-uuid-1234', got %v", decoded["session_id"])
	}
}

func TestSessionEvent_FormatSSE_MarshalError(t *testing.T) {
	// Channels cannot be serialized to JSON
	badEvent := SessionEvent{
		Event: "error_event",
		Data:  make(chan int),
	}
	_, err := badEvent.FormatSSE()
	if err == nil {
		t.Fatal("expected error formatting unserializable data, got nil")
	}
}

func TestBroadcaster_MultipleSubscribers(t *testing.T) {
	b := NewEventBroadcaster()
	defer b.Close()

	ch1, cancel1 := b.Subscribe()
	defer cancel1()
	ch2, cancel2 := b.Subscribe()
	defer cancel2()
	ch3, cancel3 := b.Subscribe()
	defer cancel3()

	if count := b.SubscriberCount(); count != 3 {
		t.Fatalf("expected 3 subscribers, got %d", count)
	}

	testPayload := "payload-multi-sub"
	b.Broadcast(SessionEvent{
		Event: EventMatchUpdate,
		Data:  testPayload,
	})

	for idx, ch := range []<-chan SessionEvent{ch1, ch2, ch3} {
		select {
		case msg, ok := <-ch:
			if !ok {
				t.Fatalf("sub %d channel closed prematurely", idx+1)
			}
			if msg.Event != EventMatchUpdate {
				t.Errorf("sub %d: expected event %s, got %s", idx+1, EventMatchUpdate, msg.Event)
			}
			if str, ok := msg.Data.(string); !ok || str != testPayload {
				t.Errorf("sub %d: expected payload %s, got %v", idx+1, testPayload, msg.Data)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("sub %d timed out waiting for event", idx+1)
		}
	}
}

func TestBroadcaster_SlowSubscriberNonBlocking(t *testing.T) {
	b := NewEventBroadcaster()
	defer b.Close()

	// Fast subscriber that continuously reads events
	fastCh, fastCancel := b.Subscribe()
	defer fastCancel()

	// Slow subscriber that NEVER reads from its channel
	slowCh, slowCancel := b.Subscribe()
	defer slowCancel()

	var fastReceived int
	var fastWg sync.WaitGroup
	fastWg.Add(1)

	totalEvents := 100 // Well beyond defaultSubscriberBufferSize = 64

	go func() {
		defer fastWg.Done()
		for range totalEvents {
			select {
			case <-fastCh:
				fastReceived++
			case <-time.After(1 * time.Second):
				return
			}
		}
	}()

	start := time.Now()
	for i := range totalEvents {
		b.Broadcast(SessionEvent{
			Event: EventMatchUpdate,
			Data:  i,
		})
		time.Sleep(100 * time.Microsecond)
	}
	broadcastDuration := time.Since(start)

	// Broadcast should be virtually instantaneous (< 50ms) even with a full channel
	if broadcastDuration > 100*time.Millisecond {
		t.Errorf("Broadcast took %v, expected non-blocking fast return (< 100ms)", broadcastDuration)
	}

	fastWg.Wait()
	if fastReceived != totalEvents {
		t.Errorf("fast subscriber received %d events, expected all %d", fastReceived, totalEvents)
	}

	// Slow channel should be saturated at capacity (64)
	if len(slowCh) != defaultSubscriberBufferSize {
		t.Errorf("expected slow channel to hold %d events, got %d", defaultSubscriberBufferSize, len(slowCh))
	}
}

func TestBroadcaster_SafeUnsubscribeAndClose(t *testing.T) {
	b := NewEventBroadcaster()

	ch1, cancel1 := b.Subscribe()
	ch2, cancel2 := b.Subscribe()

	if b.SubscriberCount() != 2 {
		t.Fatalf("expected 2 subscribers, got %d", b.SubscriberCount())
	}

	// Unsubscribe ch1 idempotently
	cancel1()
	cancel1() // second call must not panic
	cancel1() // third call must not panic

	if b.SubscriberCount() != 1 {
		t.Errorf("expected 1 subscriber after cancel, got %d", b.SubscriberCount())
	}

	// Verify ch1 is closed
	_, ok := <-ch1
	if ok {
		t.Error("expected ch1 to be closed")
	}

	// Broadcast to remaining ch2
	b.Broadcast(SessionEvent{Event: EventSessionUpdate, Data: "for-ch2"})
	select {
	case msg := <-ch2:
		if msg.Data != "for-ch2" {
			t.Errorf("expected 'for-ch2', got %v", msg.Data)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("ch2 timed out receiving event")
	}

	// Close broadcaster
	b.Close()
	b.Close() // idempotent close

	// Verify ch2 is now closed
	_, ok = <-ch2
	if ok {
		t.Error("expected ch2 to be closed after b.Close()")
	}

	// Calling cancel2 after Close must not panic
	cancel2()

	// Subscribing after Close should return immediately closed channel
	postCh, postCancel := b.Subscribe()
	defer postCancel()
	_, ok = <-postCh
	if ok {
		t.Error("expected channel from Subscribe on closed broadcaster to be closed")
	}

	// Broadcast on closed broadcaster is a no-op
	b.Broadcast(SessionEvent{Event: EventSessionUpdate, Data: "dropped"})
}

func TestBroadcaster_Concurrency(t *testing.T) {
	b := NewEventBroadcaster()
	defer b.Close()

	var wg sync.WaitGroup
	stopCh := make(chan struct{})

	// 10 Publisher goroutines
	for p := range 10 {
		wg.Add(1)
		go func(id int) {
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
					time.Sleep(1 * time.Millisecond)
				}
			}
		}(p)
	}

	// 10 Subscriber / Unsubscriber goroutines
	for s := range 10 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stopCh:
					return
				default:
					ch, cancel := b.Subscribe()
					// Read a few events
					for range 3 {
						select {
						case <-ch:
						case <-time.After(5 * time.Millisecond):
						}
					}
					cancel()
					time.Sleep(1 * time.Millisecond)
				}
			}
		}(s)
	}

	time.Sleep(200 * time.Millisecond)
	close(stopCh)
	wg.Wait()
}
