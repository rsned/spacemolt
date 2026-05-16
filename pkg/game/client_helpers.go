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
		_, err := s.client.Travel(ctx, targetPOI)
		return err
	})
}

// Jump safely jumps to another system
func (s *SafeCommandBuilder) Jump(ctx context.Context, targetSystem string) error {
	return s.exec.ExecuteCommand(ctx, func(ctx context.Context) error {
		_, err := s.client.Jump(ctx, targetSystem)
		return err
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

// ===== QUEUED COMMAND METHODS (LEGACY NAMES) =====
// These methods preserve the *Queued public names for external callers but
// now delegate to the non-Queued client methods, which internally use Submit
// for request_id-based correlation. The underlying CommandQueue is gone.

// DockQueued docks at a station.
func (s *SafeCommandBuilder) DockQueued(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.Dock)
}

// UndockQueued undocks from a station.
func (s *SafeCommandBuilder) UndockQueued(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.Undock)
}

// TravelQueued travels to a POI.
func (s *SafeCommandBuilder) TravelQueued(ctx context.Context, targetPOI string) error {
	return s.exec.ExecuteCommand(ctx, func(ctx context.Context) error {
		_, err := s.client.Travel(ctx, targetPOI)
		return err
	})
}

// JumpQueued jumps to another system.
func (s *SafeCommandBuilder) JumpQueued(ctx context.Context, targetSystem string) error {
	return s.exec.ExecuteCommand(ctx, func(ctx context.Context) error {
		_, err := s.client.Jump(ctx, targetSystem)
		return err
	})
}

// MineQueued mines resources.
func (s *SafeCommandBuilder) MineQueued(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.Mine)
}

// RefuelQueued refuels the ship.
func (s *SafeCommandBuilder) RefuelQueued(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.Refuel)
}

// RepairQueued repairs the ship.
func (s *SafeCommandBuilder) RepairQueued(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.Repair)
}

// BuyQueued buys items.
func (s *SafeCommandBuilder) BuyQueued(ctx context.Context, itemID string, quantity float64) error {
	return s.exec.ExecuteCommand(ctx, func(ctx context.Context) error {
		return s.client.Buy(ctx, itemID, quantity)
	})
}

// SellQueued sells items.
func (s *SafeCommandBuilder) SellQueued(ctx context.Context, itemID string, quantity float64) error {
	return s.exec.ExecuteCommand(ctx, func(ctx context.Context) error {
		return s.client.Sell(ctx, itemID, quantity)
	})
}

// GetSystemQueued gets system info.
func (s *SafeCommandBuilder) GetSystemQueued(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.GetSystem)
}

// GetStatusQueued gets status.
func (s *SafeCommandBuilder) GetStatusQueued(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.GetStatus)
}

// GetPOIQueued gets POI info.
func (s *SafeCommandBuilder) GetPOIQueued(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.GetPOI)
}

// GetListingsQueued gets market listings.
func (s *SafeCommandBuilder) GetListingsQueued(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.GetListings)
}

// CraftQueued crafts an item.
func (s *SafeCommandBuilder) CraftQueued(ctx context.Context, recipeID string, quantity int) error {
	return s.exec.ExecuteCommand(ctx, func(ctx context.Context) error {
		return s.client.CraftWithQuantity(ctx, recipeID, quantity)
	})
}

// GetCargoQueued gets cargo contents.
func (s *SafeCommandBuilder) GetCargoQueued(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.GetCargo)
}

// GetBaseQueued gets base info.
func (s *SafeCommandBuilder) GetBaseQueued(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.GetBase)
}

// GetShipQueued gets ship info.
func (s *SafeCommandBuilder) GetShipQueued(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.GetShip)
}

// GetNearbyQueued gets nearby players.
func (s *SafeCommandBuilder) GetNearbyQueued(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.GetNearby)
}

// ViewStorageQueued views station storage.
func (s *SafeCommandBuilder) ViewStorageQueued(ctx context.Context) error {
	return s.exec.ExecuteCommand(ctx, s.client.ViewStorage)
}

// WithdrawItemsQueued withdraws items from storage.
func (s *SafeCommandBuilder) WithdrawItemsQueued(ctx context.Context, itemID string, quantity float64) error {
	return s.exec.ExecuteCommand(ctx, func(ctx context.Context) error {
		return s.client.WithdrawItems(ctx, itemID, quantity)
	})
}

// AcceptMissionQueued accepts a mission.
func (s *SafeCommandBuilder) AcceptMissionQueued(ctx context.Context, missionID string) error {
	return s.exec.ExecuteCommand(ctx, func(ctx context.Context) error {
		return s.client.AcceptMission(ctx, missionID)
	})
}

// CompleteMissionQueued completes a mission.
func (s *SafeCommandBuilder) CompleteMissionQueued(ctx context.Context, missionID string) error {
	return s.exec.ExecuteCommand(ctx, func(ctx context.Context) error {
		return s.client.CompleteMission(ctx, missionID)
	})
}

