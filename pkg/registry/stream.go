package registry

import (
	"sync"
	"time"
)

// Event represents an SSE event broadcast by the registry
type Event struct {
	Type      string      `json:"type"`
	Data      interface{} `json:"data"`
	Timestamp time.Time   `json:"timestamp"`
}

// StreamManager manages Server-Sent Events subscriptions for status updates
type StreamManager struct {
	mu          sync.RWMutex
	subscribers []chan Event
}

// NewStreamManager creates a new stream manager
func NewStreamManager() *StreamManager {
	return &StreamManager{
		subscribers: make([]chan Event, 0),
	}
}

// Subscribe creates a new subscription for status events
func (sm *StreamManager) Subscribe() <-chan Event {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Create buffered channel for events
	ch := make(chan Event, 100)
	sm.subscribers = append(sm.subscribers, ch)

	return ch
}

// Unsubscribe removes a subscription
func (sm *StreamManager) Unsubscribe(ch <-chan Event) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Find and remove the channel
	for i, subscriber := range sm.subscribers {
		if subscriber == ch {
			// Remove from slice
			sm.subscribers = append(sm.subscribers[:i], sm.subscribers[i+1:]...)
			close(subscriber)
			break
		}
	}
}

// Publish sends an event to all subscribers
func (sm *StreamManager) Publish(event Event) {
	// Set timestamp if not set
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Send to all subscribers (non-blocking)
	for _, ch := range sm.subscribers {
		select {
		case ch <- event:
			// Event sent successfully
		default:
			// Channel full, skip this subscriber
			// In production, might want to log this
		}
	}
}

// SubscriberCount returns the number of active subscribers
func (sm *StreamManager) SubscriberCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.subscribers)
}
