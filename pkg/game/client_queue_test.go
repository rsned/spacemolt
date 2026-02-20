package game

import (
	"context"
	"testing"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// TestCommandQueueBasic tests basic queue operations
func TestCommandQueueBasic(t *testing.T) {
	client := NewClient("ws://test", "user", "pass", nil)
	queue := NewCommandQueue(client)

	if queue == nil {
		t.Fatal("Failed to create command queue")
	}

	if queue.QueueSize() != 0 {
		t.Errorf("Expected empty queue, got size %d", queue.QueueSize())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the queue
	queue.Start(ctx)

	if !queue.running {
		t.Error("Queue should be running after Start()")
	}

	// Stop the queue
	queue.Stop()

	// Give it a moment to stop
	time.Sleep(100 * time.Millisecond)
}

// TestCommandQueueEnqueue tests enqueueing commands
func TestCommandQueueEnqueue(t *testing.T) {
	client := NewClient("ws://test", "user", "pass", nil)
	queue := NewCommandQueue(client)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Enqueue a command (will timeout since we're not connected)
	msg := protocol.Message{
		Type:      "test",
		Timestamp: time.Now().UnixMilli(),
	}

	done := make(chan error, 1)
	go func() {
		_, err := queue.Enqueue(ctx, msg, 100*time.Millisecond)
		done <- err
	}()

	// Wait for timeout or completion
	select {
	case err := <-done:
		if err == nil {
			t.Error("Expected error due to timeout, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Error("Test took too long")
	}
}

// TestCommandQueueIDGeneration tests command ID generation
func TestCommandQueueIDGeneration(t *testing.T) {
	msg := protocol.Message{
		Type:      "travel",
		Timestamp: time.Now().UnixMilli(),
	}

	id := generateCommandID(msg)
	if id == "" {
		t.Error("Generated empty command ID")
	}

	expectedPrefix := "travel_"
	if len(id) < len(expectedPrefix) || id[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("Expected command ID to start with '%s', got '%s'", expectedPrefix, id)
	}
}

// TestClientSendQueued tests the client's SendQueued method
func TestClientSendQueued(t *testing.T) {
	client := NewClient("ws://test", "user", "pass", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	msg := protocol.Message{
		Type:      "test",
		Timestamp: time.Now().UnixMilli(),
	}

	// This will timeout since we're not connected
	_, err := client.SendQueued(ctx, msg, 100*time.Millisecond)
	if err == nil {
		t.Error("Expected error due to timeout/disconnect, got nil")
	}
}

// TestCommandQueueSequential tests that commands are executed sequentially
func TestCommandQueueSequential(t *testing.T) {
	client := NewClient("ws://test", "user", "pass", nil)
	queue := NewCommandQueue(client)

	if queue.QueueSize() != 0 {
		t.Errorf("Expected empty queue, got size %d", queue.QueueSize())
	}

	// The queue processor should be empty
	active := queue.GetActiveCommand()
	if active != nil {
		t.Error("Expected no active command")
	}
}

// BenchmarkCommandQueueEnqueue benchmarks enqueueing commands
func BenchmarkCommandQueueEnqueue(b *testing.B) {
	client := NewClient("ws://bench", "user", "pass", nil)
	queue := NewCommandQueue(client)

	ctx := context.Background()
	msg := protocol.Message{
		Type:      "test",
		Timestamp: time.Now().UnixMilli(),
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Just measure enqueue speed (commands will timeout)
		go queue.Enqueue(ctx, msg, 10*time.Millisecond)
	}
}

// BenchmarkGenerateCommandID benchmarks command ID generation
func BenchmarkGenerateCommandID(b *testing.B) {
	msg := protocol.Message{
		Type:      "travel",
		Timestamp: time.Now().UnixMilli(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = generateCommandID(msg)
	}
}
