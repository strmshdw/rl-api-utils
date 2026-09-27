package session

import (
	"sync"
)

const defaultSubscriberBufferSize = 64

// EventBroadcaster coordinates thread-safe distribution of SessionEvent notifications
// to connected clients (HTTP REST/SSE endpoints, WebSocket bridges, and internal listeners).
type EventBroadcaster struct {
	mu          sync.RWMutex
	subscribers map[chan SessionEvent]struct{}
	closed      bool
}

// Broadcaster is an alias for EventBroadcaster for flexible naming.
type Broadcaster = EventBroadcaster

// NewEventBroadcaster constructs an initialized EventBroadcaster.
func NewEventBroadcaster() *EventBroadcaster {
	return &EventBroadcaster{
		subscribers: make(map[chan SessionEvent]struct{}),
	}
}

// NewBroadcaster constructs an initialized Broadcaster.
func NewBroadcaster() *Broadcaster {
	return NewEventBroadcaster()
}

// Subscribe registers a new subscriber channel and returns:
// 1. A receive-only channel of SessionEvent (buffered to 64 items)
// 2. An idempotent cancel function to deregister and close the channel.
func (b *EventBroadcaster) Subscribe() (<-chan SessionEvent, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan SessionEvent, defaultSubscriberBufferSize)
	if b.closed {
		close(ch)
		return ch, func() {}
	}

	b.subscribers[ch] = struct{}{}

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if _, exists := b.subscribers[ch]; exists {
				delete(b.subscribers, ch)
				close(ch)
			}
		})
	}

	return ch, cancel
}

// Broadcast sends an event to all registered subscribers using non-blocking writes.
// If a subscriber's channel buffer is full, the event frame is dropped for that
// specific subscriber to prevent slow consumers from blocking the daemon pipeline.
func (b *EventBroadcaster) Broadcast(event SessionEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return
	}

	for ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			// Buffer full: drop frame for slow consumer to prevent backpressure
		}
	}
}

// SubscriberCount returns the current number of active subscribers.
func (b *EventBroadcaster) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}

// Close closes all subscriber channels and marks the broadcaster closed.
// Subsequent Subscribe() calls return an already-closed channel.
func (b *EventBroadcaster) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true

	for ch := range b.subscribers {
		close(ch)
	}
	b.subscribers = make(map[chan SessionEvent]struct{})
}
