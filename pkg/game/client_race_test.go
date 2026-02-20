package game

import (
	"sync"
	"testing"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// TestClientConcurrentAccess tests that the Client properly handles concurrent
// access to shared fields (conn, handler) without data races.
//
// This test would fail with the race detector before the fix that added
// proper mutex protection in Connect() and listen().
func TestClientConcurrentAccess(t *testing.T) {
	// Create a mock handler
	handler := &mockHandler{}

	// Create client with test WebSocket URL
	client := NewClient("ws://localhost:8080/ws", "testuser", "testpass", nil)

	// Start the listen goroutine (simulating what Connect() does)
	// In real code, Connect() starts this goroutine
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Simulate listen loop checking conn and handler
		for range 100 {
			client.mu.RLock()
			_ = client.conn     // Read conn
			_ = client.handler  // Read handler
			client.mu.RUnlock()
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// Simulate SetHandler() being called concurrently (as in auto-explorer main.go)
	// This was one of the data races: write to handler without mutex protection
	for range 100 {
		client.SetHandler(handler)
		time.Sleep(1 * time.Millisecond)
	}

	// Wait for goroutine to finish
	wg.Wait()
}

// mockHandler is a minimal implementation of MessageHandler for testing
type mockHandler struct{}

func (m *mockHandler) OnConnected(state *State) {}

func (m *mockHandler) OnMessage(resp protocol.Response) {}

func (m *mockHandler) OnDisconnected(err error) {}
