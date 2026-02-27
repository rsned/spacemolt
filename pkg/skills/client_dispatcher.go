package skills

import (
	"context"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// ClientDispatcher adapts a game.Client into an ActionDispatcher for the
// skill executor. It maps action names to the corresponding game client
// methods and waits for server ticks between actions.
type ClientDispatcher struct {
	Client    *game.Client
	TickDelay time.Duration // delay after tick-consuming actions; 0 = default (11s)
}

// NewClientDispatcher creates a dispatcher wrapping a connected game client.
func NewClientDispatcher(client *game.Client) *ClientDispatcher {
	return &ClientDispatcher{
		Client:    client,
		TickDelay: game.SleepTick + time.Second, // 11s — wait for next tick
	}
}

// GetState returns the current game state.
func (d *ClientDispatcher) GetState() *game.State {
	return d.Client.GetState()
}

// Dispatch executes a single game action and waits for the tick if needed.
func (d *ClientDispatcher) Dispatch(ctx context.Context, action, target string) error {
	err := d.dispatch(ctx, action, target)
	if err != nil {
		return err
	}

	// Wait for the server tick after actions that consume one.
	if isTickAction(action) {
		delay := d.TickDelay
		if delay == 0 {
			delay = game.SleepTick + time.Second
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

func (d *ClientDispatcher) dispatch(ctx context.Context, action, target string) error {
	switch action {
	// Navigation
	case "undock":
		return d.Client.Undock(ctx)
	case "dock":
		return d.Client.Dock(ctx)
	case "travel":
		if target == "" {
			return fmt.Errorf("travel requires a target POI")
		}
		return d.Client.Travel(ctx, target)
	case "jump":
		if target == "" {
			return fmt.Errorf("jump requires a target system")
		}
		return d.Client.Jump(ctx, target)

	// Mining & scanning
	case "mine":
		return d.Client.Mine(ctx)
	case "scan":
		return d.Client.Scan(ctx)

	// Commerce
	case "sell":
		return d.Client.SellAllBulk(ctx, nil)
	case "buy":
		if target == "" {
			return fmt.Errorf("buy requires a target item ID")
		}
		return d.Client.Buy(ctx, target, 1)

	// Ship maintenance
	case "refuel":
		return d.Client.Refuel(ctx)
	case "repair":
		return d.Client.Repair(ctx)

	// Crafting
	case "craft":
		if target == "" {
			return fmt.Errorf("craft requires a target recipe ID")
		}
		return d.Client.CraftWithQuantity(ctx, target, 1)

	// Storage
	case "deposit_all_items":
		return d.Client.DepositAllItems(ctx)

	// Queries (no tick cost, no wait)
	case "get_status":
		return d.Client.GetStatus(ctx)
	case "get_system":
		return d.Client.GetSystem(ctx)
	case "get_ship":
		return d.Client.GetShip(ctx)
	case "get_skills":
		return d.Client.GetSkills(ctx)

	// No-op
	case "wait":
		return nil

	default:
		return fmt.Errorf("unsupported action: %q", action)
	}
}

// isTickAction returns true if the action consumes a server tick.
func isTickAction(action string) bool {
	switch action {
	case "get_status", "get_system", "get_ship", "get_skills",
		"get_poi", "get_map", "get_version", "get_recipes",
		"get_wrecks", "get_notes", "get_listings", "get_trades",
		"wait", "help":
		return false
	default:
		return true
	}
}
