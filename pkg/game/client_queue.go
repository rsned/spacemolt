package game

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// QueuedCommand represents a command in the execution queue
type QueuedCommand struct {
	ID        string                    // Unique identifier for this command
	Message   protocol.Message          // The message to send
	Response  chan protocol.Response    // Channel to receive the response
	Error     chan error                // Channel to receive errors
	Timeout   time.Duration             // Timeout for this command
	Timestamp time.Time                 // When the command was queued
}

// CommandQueue manages a queue of commands to be executed sequentially
// This ensures that commands are sent one at a time and responses are properly matched
type CommandQueue struct {
	client    *Client
	queue     []*QueuedCommand
	mu        sync.Mutex
	active    *QueuedCommand         // Currently executing command
	activeMu  sync.RWMutex
	running   bool
	runnerCtx context.Context
	stopQueue chan struct{}
}

// NewCommandQueue creates a new command queue
func NewCommandQueue(client *Client) *CommandQueue {
	return &CommandQueue{
		client:    client,
		queue:     make([]*QueuedCommand, 0),
		stopQueue: make(chan struct{}),
	}
}

// Start begins processing the command queue
func (q *CommandQueue) Start(ctx context.Context) {
	q.mu.Lock()
	if q.running {
		q.mu.Unlock()
		return
	}
	q.running = true
	q.runnerCtx = ctx
	q.mu.Unlock()

	go q.processQueue()
}

// Stop stops processing the command queue
func (q *CommandQueue) Stop() {
	q.mu.Lock()
	if !q.running {
		q.mu.Unlock()
		return
	}
	q.running = false
	q.mu.Unlock()

	close(q.stopQueue)
}

// Enqueue adds a command to the queue and waits for it to complete.
// This blocks until the command is executed and a response is received.
//
// Deprecated: use Client.execMutation. CommandQueue will be removed once
// the last caller migrates.
func (q *CommandQueue) Enqueue(ctx context.Context, msg protocol.Message, timeout time.Duration) (protocol.Response, error) {
	// Start the queue if not already running
	q.mu.Lock()
	if !q.running {
		q.running = true
		q.runnerCtx = ctx
		go q.processQueue()
	}
	q.mu.Unlock()

	// Create the queued command
	cmd := &QueuedCommand{
		ID:        generateCommandID(msg),
		Message:   msg,
		Response:  make(chan protocol.Response, 1),
		Error:     make(chan error, 1),
		Timeout:   timeout,
		Timestamp: time.Now(),
	}

	// Add to queue
	q.mu.Lock()
	q.queue = append(q.queue, cmd)
	q.client.debugLogger.Printf("CommandQueue: Enqueued command %s (queue size: %d)", cmd.ID, len(q.queue))
	q.mu.Unlock()

	// Wait for response or error
	select {
	case resp := <-cmd.Response:
		return resp, nil
	case err := <-cmd.Error:
		return protocol.Response{}, err
	case <-time.After(timeout):
		return protocol.Response{}, fmt.Errorf("timeout waiting for command %s", cmd.ID)
	case <-ctx.Done():
		return protocol.Response{}, ctx.Err()
	}
}

// processQueue processes commands from the queue sequentially
func (q *CommandQueue) processQueue() {
	for {
		select {
		case <-q.runnerCtx.Done():
			q.client.debugLogger.Printf("CommandQueue: Context cancelled, stopping queue processor")
			return
		case <-q.stopQueue:
			q.client.debugLogger.Printf("CommandQueue: Stop signal received")
			return
		default:
		}

		// Get next command from queue
		q.mu.Lock()
		if len(q.queue) == 0 {
			q.mu.Unlock()
			time.Sleep(100 * time.Millisecond) // Small sleep to prevent busy-wait
			continue
		}

		cmd := q.queue[0]
		q.queue = q.queue[1:]
		q.mu.Unlock()

		// Mark as active
		q.activeMu.Lock()
		q.active = cmd
		q.activeMu.Unlock()

		// Execute the command
		q.client.debugLogger.Printf("CommandQueue: Executing command %s", cmd.ID)
		err := q.executeCommand(cmd)

		if err != nil {
			q.client.debugLogger.Printf("CommandQueue: Command %s failed: %v", cmd.ID, err)
			select {
			case cmd.Error <- err:
			default:
			}
		}

		// Clear active command
		q.activeMu.Lock()
		q.active = nil
		q.activeMu.Unlock()
	}
}

// executeCommand sends a command and waits for the matching response
func (q *CommandQueue) executeCommand(cmd *QueuedCommand) error {
	// Ensure we're connected
	if err := q.client.EnsureConnected(q.runnerCtx); err != nil {
		return fmt.Errorf("not connected: %w", err)
	}

	// Register waiter for this specific command
	responseChan := make(chan protocol.Response, 1)
	errorChan := make(chan protocol.Response, 1)

	// Register waiters for both OK and Error responses
	q.client.waiterMu.Lock()
	// Store the command ID with the waiters so we can match responses
	q.client.waiters[cmd.ID+":ok"] = responseChan
	q.client.waiters[cmd.ID+":error"] = errorChan
	q.client.waiters[cmd.ID+":any"] = responseChan // Fallback
	q.client.waiterMu.Unlock()

	// Clean up waiters when done
	defer func() {
		q.client.waiterMu.Lock()
		delete(q.client.waiters, cmd.ID+":ok")
		delete(q.client.waiters, cmd.ID+":error")
		delete(q.client.waiters, cmd.ID+":any")
		q.client.waiterMu.Unlock()
	}()

	// Send the command
	if err := q.client.Send(q.runnerCtx, cmd.Message); err != nil {
		return fmt.Errorf("failed to send command: %w", err)
	}

	// Wait for response with timeout
	timeoutChan := time.After(cmd.Timeout)
	select {
	case resp := <-responseChan:
		// Check if this is an error response
		if resp.Type == protocol.TypeError {
			if msg, ok := resp.Payload["message"].(string); ok {
				return fmt.Errorf("server error: %s", msg)
			}
			return fmt.Errorf("server error (no message)")
		}
		// Success - send response to caller
		select {
		case cmd.Response <- resp:
		default:
		}
		return nil

	case errResp := <-errorChan:
		// Error response received
		if msg, ok := errResp.Payload["message"].(string); ok {
			return fmt.Errorf("server error: %s", msg)
		}
		return fmt.Errorf("server error")

	case <-timeoutChan:
		return fmt.Errorf("timeout waiting for response to command %s", cmd.ID)

	case <-q.runnerCtx.Done():
		return q.runnerCtx.Err()
	}
}

// GetActiveCommand returns the currently executing command (if any)
func (q *CommandQueue) GetActiveCommand() *QueuedCommand {
	q.activeMu.RLock()
	defer q.activeMu.RUnlock()
	return q.active
}

// QueueSize returns the current size of the queue
func (q *CommandQueue) QueueSize() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.queue)
}

// generateCommandID creates a unique identifier for a command
func generateCommandID(msg protocol.Message) string {
	timestamp := time.Now().UnixNano()
	return fmt.Sprintf("%s_%d", msg.Type, timestamp)
}

// handleResponse is called by the client when a response is received
// It routes the response to the appropriate queued command
func (q *CommandQueue) handleResponse(resp protocol.Response) {
	q.activeMu.RLock()
	active := q.active
	q.activeMu.RUnlock()

	if active == nil {
		// No active command, this might be a server push message
		return
	}

	// Check if this response matches the active command
	// We match by response type (ok/error)
	responseType := resp.Type

	// Send response to the appropriate channel
	q.client.waiterMu.Lock()
	defer q.client.waiterMu.Unlock()

	// Try to match by response type first
	switch responseType {
	case protocol.TypeOK:
		if ch, ok := q.client.waiters[active.ID+":ok"]; ok {
			select {
			case ch <- resp:
			default:
			}
			return
		}
	case protocol.TypeError:
		if ch, ok := q.client.waiters[active.ID+":error"]; ok {
			select {
			case ch <- resp:
			default:
			}
			return
		}
	}

	// Fallback: try the any channel
	if ch, ok := q.client.waiters[active.ID+":any"]; ok {
		select {
		case ch <- resp:
		default:
		}
	}
}
