package api

import (
	"sync"
	"time"
)

// StreamManager manages Server-Sent Events subscriptions for agents
type StreamManager struct {
	mu          sync.RWMutex
	subscribers map[string][]chan Event // agentID -> list of subscriber channels
}

// Event represents an SSE event
type Event struct {
	AgentID   string      `json:"agent_id,omitempty"`
	Type      string      `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// NewStreamManager creates a new stream manager
func NewStreamManager() *StreamManager {
	return &StreamManager{
		subscribers: make(map[string][]chan Event),
	}
}

// Subscribe creates a new subscription for an agent's events
func (sm *StreamManager) Subscribe(agentID string) <-chan Event {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Create buffered channel for events
	ch := make(chan Event, 100)

	// Add to subscribers list
	if _, exists := sm.subscribers[agentID]; !exists {
		sm.subscribers[agentID] = make([]chan Event, 0)
	}
	sm.subscribers[agentID] = append(sm.subscribers[agentID], ch)

	return ch
}

// Unsubscribe removes a subscription
func (sm *StreamManager) Unsubscribe(agentID string, ch <-chan Event) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	subs, exists := sm.subscribers[agentID]
	if !exists {
		return
	}

	// Find and remove the channel
	for i, subscriber := range subs {
		if subscriber == ch {
			// Remove from slice
			sm.subscribers[agentID] = append(subs[:i], subs[i+1:]...)
			close(subscriber)
			break
		}
	}

	// Clean up empty subscriber lists
	if len(sm.subscribers[agentID]) == 0 {
		delete(sm.subscribers, agentID)
	}
}

// Publish sends an event to all subscribers of an agent
func (sm *StreamManager) Publish(agentID string, event Event) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	subs, exists := sm.subscribers[agentID]
	if !exists {
		return
	}

	// Send to all subscribers (non-blocking)
	for _, ch := range subs {
		select {
		case ch <- event:
			// Event sent successfully
		default:
			// Channel full, skip this subscriber
			// In production, might want to log this
		}
	}
}

// PublishToAll sends an event to all subscribers across all agents
func (sm *StreamManager) PublishToAll(event Event) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	for agentID, subs := range sm.subscribers {
		// Set agent ID in event
		eventCopy := event
		eventCopy.AgentID = agentID

		for _, ch := range subs {
			select {
			case ch <- eventCopy:
				// Event sent successfully
			default:
				// Channel full, skip
			}
		}
	}
}

// SubscriberCount returns the number of active subscribers for an agent
func (sm *StreamManager) SubscriberCount(agentID string) int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	subs, exists := sm.subscribers[agentID]
	if !exists {
		return 0
	}
	return len(subs)
}
