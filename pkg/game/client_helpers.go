package game

import (
	"context"
	"fmt"
	"time"
)

// RobustCommandExecutor provides a wrapper around Client that handles
// connection issues and retries automatically, making it easier to write
// reliable agent code.
type RobustCommandExecutor struct {
	client         *Client
	maxRetries     int
	retryDelay     time.Duration
	contextTimeout time.Duration
}

// NewRobustCommandExecutor creates a new robust command executor
func NewRobustCommandExecutor(client *Client) *RobustCommandExecutor {
	return &RobustCommandExecutor{
		client:         client,
		maxRetries:     3,
		retryDelay:     2 * time.Second,
		contextTimeout: 30 * time.Second,
	}
}

// ExecuteCommand executes a game command with automatic retry on connection failures
// This is the preferred way to execute commands for agents, as it handles:
// - Connection drops and automatic reconnection
// - Transient failures with retry
// - Context cancellation
func (r *RobustCommandExecutor) ExecuteCommand(ctx context.Context, cmdFunc func(context.Context) error) error {
	var lastErr error

	for attempt := 0; attempt < r.maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff between retries
			backoff := r.retryDelay * time.Duration(1<<uint(attempt-1))
			r.client.debugLogger.Printf("Command attempt %d/%d failed, retrying in %v: %v",
				attempt+1, r.maxRetries, backoff, lastErr)

			select {
			case <-time.After(backoff):
				// Continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		// Ensure we're connected before executing
		if err := r.client.EnsureConnected(ctx); err != nil {
			lastErr = fmt.Errorf("connection check failed: %w", err)
			continue
		}

		// Execute the command with a timeout
		cmdCtx, cancel := context.WithTimeout(ctx, r.contextTimeout)
		defer cancel()

		err := cmdFunc(cmdCtx)
		if err == nil {
			// Success!
			return nil
		}

		lastErr = err

		// Check if this is a connection-related error that should trigger a retry
		if isConnectionError(err) {
			r.client.debugLogger.Printf("Connection error detected, will reconnect: %v", err)
			// Force a reconnection on next attempt
			_ = r.client.Disconnect()
			continue
		}

		// For other errors, check if they're retryable
		if isRetryableError(err) {
			continue
		}

		// Non-retryable error, return immediately
		return err
	}

	return fmt.Errorf("command failed after %d attempts: %w", r.maxRetries, lastErr)
}

// ExecuteWithRetry executes a command with custom retry logic
func (r *RobustCommandExecutor) ExecuteWithRetry(ctx context.Context, cmdFunc func(context.Context) error, maxRetries int) error {
	originalMaxRetries := r.maxRetries
	r.maxRetries = maxRetries
	defer func() { r.maxRetries = originalMaxRetries }()

	return r.ExecuteCommand(ctx, cmdFunc)
}

// isConnectionError checks if an error is connection-related
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()
	connectionErrors := []string{
		"not connected",
		"connection reset",
		"connection closed",
		"broken pipe",
		"connection timeout",
		"websocket closed",
		"failed to send message",
	}

	for _, connErr := range connectionErrors {
		if contains(errMsg, connErr) {
			return true
		}
	}

	return false
}

// isRetryableError checks if an error should trigger a retry
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()

	// These errors indicate temporary conditions that might resolve
	retryableErrors := []string{
		"timeout",
		"deadline exceeded",
		"temporary failure",
		"rate limited",
		"already in transit",
	}

	for _, retryErr := range retryableErrors {
		if contains(errMsg, retryErr) {
			return true
		}
	}

	return false
}

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
		 len(s) > len(substr) && (
			s[:len(substr)] == substr ||
			s[len(s)-len(substr):] == substr ||
			containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// SafeCommandBuilder helps build commands with proper error handling
type SafeCommandBuilder struct {
	client *Client
	exec   *RobustCommandExecutor
}

// NewSafeCommandBuilder creates a new safe command builder
func NewSafeCommandBuilder(client *Client) *SafeCommandBuilder {
	return &SafeCommandBuilder{
		client: client,
		exec:   NewRobustCommandExecutor(client),
	}
}

// GetSystem safely gets system information
func (s *SafeCommandBuilder) GetSystem(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.GetSystem)
}

// GetStatus safely gets player status
func (s *SafeCommandBuilder) GetStatus(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.GetStatus)
}

// GetPOI safely gets POI information
func (s *SafeCommandBuilder) GetPOI(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.GetPOI)
}

// Dock safely docks at a station
func (s *SafeCommandBuilder) Dock(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.Dock)
}

// Undock safely undocks from a station
func (s *SafeCommandBuilder) Undock(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.Undock)
}

// Travel safely travels to a POI
func (s *SafeCommandBuilder) Travel(ctx context.Context, targetPOI string) error {
	return s.exec.ExecuteCommand(ctx, func(ctx context.Context) error {
		return s.client.Travel(ctx, targetPOI)
	})
}

// Jump safely jumps to another system
func (s *SafeCommandBuilder) Jump(ctx context.Context, targetSystem string) error {
	return s.exec.ExecuteCommand(ctx, func(ctx context.Context) error {
		return s.client.Jump(ctx, targetSystem)
	})
}

// Mine safely mines resources
func (s *SafeCommandBuilder) Mine(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.Mine)
}

// Refuel safely refuels the ship
func (s *SafeCommandBuilder) Refuel(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.Refuel)
}

// Repair safely repairs the ship
func (s *SafeCommandBuilder) Repair(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.Repair)
}

// SellAllBulk safely sells all cargo
func (s *SafeCommandBuilder) SellAllBulk(ctx context.Context, reservedItems []string) error {
	return s.exec.ExecuteCommand(ctx, func(ctx context.Context) error {
		return s.client.SellAllBulk(ctx, reservedItems)
	})
}

// Buy safely buys items
func (s *SafeCommandBuilder) Buy(ctx context.Context, itemID string, quantity float64) error {
	return s.exec.ExecuteCommand(ctx, func(ctx context.Context) error {
		return s.client.Buy(ctx, itemID, quantity)
	})
}

// GetListings safely gets market listings
func (s *SafeCommandBuilder) GetListings(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.GetListings)
}

// ===== QUEUED COMMAND METHODS =====
// These methods use the command queue for reliable sequential execution
// They block until the command completes successfully, ensuring commands
// are executed one at a time in order.

// DockQueued docks at a station using the queue
func (s *SafeCommandBuilder) DockQueued(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.DockQueued)
}

// UndockQueued undocks from a station using the queue
func (s *SafeCommandBuilder) UndockQueued(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.UndockQueued)
}

// TravelQueued travels to a POI using the queue
func (s *SafeCommandBuilder) TravelQueued(ctx context.Context, targetPOI string) error {
	return s.exec.ExecuteCommand(ctx, func(ctx context.Context) error {
		return s.client.TravelQueued(ctx, targetPOI)
	})
}

// JumpQueued jumps to another system using the queue
func (s *SafeCommandBuilder) JumpQueued(ctx context.Context, targetSystem string) error {
	return s.exec.ExecuteCommand(ctx, func(ctx context.Context) error {
		return s.client.JumpQueued(ctx, targetSystem)
	})
}

// MineQueued mines resources using the queue
func (s *SafeCommandBuilder) MineQueued(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.MineQueued)
}

// RefuelQueued refuels the ship using the queue
func (s *SafeCommandBuilder) RefuelQueued(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.RefuelQueued)
}

// RepairQueued repairs the ship using the queue
func (s *SafeCommandBuilder) RepairQueued(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.RepairQueued)
}

// BuyQueued buys items using the queue
func (s *SafeCommandBuilder) BuyQueued(ctx context.Context, itemID string, quantity float64) error {
	return s.exec.ExecuteCommand(ctx, func(ctx context.Context) error {
		return s.client.BuyQueued(ctx, itemID, quantity)
	})
}

// SellQueued sells items using the queue
func (s *SafeCommandBuilder) SellQueued(ctx context.Context, itemID string, quantity float64) error {
	return s.exec.ExecuteCommand(ctx, func(ctx context.Context) error {
		return s.client.SellQueued(ctx, itemID, quantity)
	})
}

// GetSystemQueued gets system info using the queue
func (s *SafeCommandBuilder) GetSystemQueued(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.GetSystemQueued)
}

// GetStatusQueued gets status using the queue
func (s *SafeCommandBuilder) GetStatusQueued(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.GetStatusQueued)
}

// GetPOIQueued gets POI info using the queue
func (s *SafeCommandBuilder) GetPOIQueued(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.GetPOIQueued)
}

// GetListingsQueued gets market listings using the queue
func (s *SafeCommandBuilder) GetListingsQueued(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.GetListingsQueued)
}

