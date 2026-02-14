package gateway

import (
	"sync"
	"time"
)

// EventType classifies a mesh event for WebSocket clients.
type EventType string

const (
	EventMessage        EventType = "message"
	EventNodeUpdate     EventType = "node_update"
	EventPositionUpdate EventType = "position_update"
	EventTelemetry      EventType = "telemetry"
	EventStatus         EventType = "status"
)

// Event is the JSON-serialisable envelope broadcast to WebSocket clients.
type Event struct {
	Type      EventType   `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// subscriber holds a buffered channel for one WebSocket connection.
type subscriber struct {
	ch chan Event
}

// EventBus fans mesh events out to all registered WebSocket clients.
// Matches the spec structure:
//
//	type EventBus struct {
//	    clients     map[string]*websocket.Conn
//	    broadcast   chan Event
//	    register    chan *websocket.Conn
//	    unregister  chan *websocket.Conn
//	}
//
// We use channel-based subscribers instead of raw *websocket.Conn to keep
// the bus transport-agnostic and fully testable without a real WebSocket.
type EventBus struct {
	mu   sync.RWMutex
	subs map[*subscriber]struct{}
}

// NewEventBus constructs a ready EventBus.
func NewEventBus() *EventBus {
	return &EventBus{subs: make(map[*subscriber]struct{})}
}

// Subscribe registers a new WebSocket client.
// Returns a receive channel and an unsubscribe function that must be
// called when the client disconnects (it closes the channel).
func (b *EventBus) Subscribe() (<-chan Event, func()) {
	s := &subscriber{ch: make(chan Event, 64)}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()

	unsub := func() {
		b.mu.Lock()
		delete(b.subs, s)
		b.mu.Unlock()
		close(s.ch)
	}
	return s.ch, unsub
}

// Publish sends an Event to all current subscribers.
// Slow consumers are skipped (their buffer is full) to avoid stalling
// the ingest loop. They can catch up via the REST history endpoint.
func (b *EventBus) Publish(e Event) {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for s := range b.subs {
		select {
		case s.ch <- e:
		default:
			// Slow consumer – drop silently.
		}
	}
}

// PublishMessage is a convenience wrapper for EventMessage events.
func (b *EventBus) PublishMessage(data interface{}) {
	b.Publish(Event{Type: EventMessage, Data: data})
}

// PublishNodeUpdate is a convenience wrapper for EventNodeUpdate events.
func (b *EventBus) PublishNodeUpdate(data interface{}) {
	b.Publish(Event{Type: EventNodeUpdate, Data: data})
}

// PublishPosition is a convenience wrapper for EventPositionUpdate events.
func (b *EventBus) PublishPosition(data interface{}) {
	b.Publish(Event{Type: EventPositionUpdate, Data: data})
}

// PublishTelemetry is a convenience wrapper for EventTelemetry events.
func (b *EventBus) PublishTelemetry(data interface{}) {
	b.Publish(Event{Type: EventTelemetry, Data: data})
}

// Len returns the current subscriber count (useful for metrics/tests).
func (b *EventBus) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}
