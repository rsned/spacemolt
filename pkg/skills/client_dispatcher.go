package skills

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// ClientDispatcher adapts a game.Client into an ActionDispatcher for the
// skill executor. It maps action names to the corresponding game client
// methods and waits for server ticks between actions.
//
// After travel/jump actions it automatically refreshes system data so that
// POI-based conditions (current_poi_type, at_poi_type) can evaluate correctly.
type ClientDispatcher struct {
	Client    *game.Client
	Logger    *log.Logger
	TickDelay time.Duration // delay after tick-consuming actions; 0 = default (11s)
}

// NewClientDispatcher creates a dispatcher wrapping a connected game client.
func NewClientDispatcher(client *game.Client, logger *log.Logger) *ClientDispatcher {
	return &ClientDispatcher{
		Client:    client,
		Logger:    logger,
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
		if err := d.Client.Travel(ctx, target); err != nil {
			return err
		}
		d.waitForArrival(ctx, target)
		return nil
	case "jump":
		if target == "" {
			return fmt.Errorf("jump requires a target system")
		}
		if err := d.Client.Jump(ctx, target); err != nil {
			return err
		}
		d.waitForSystemChange(ctx)
		return nil

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

	// Crafting (compound — gracefully handles failures)
	case "craft_from_cargo":
		crafted, err := d.Client.CraftFromCargo(ctx, d.Logger, nil)
		if err != nil {
			d.Logger.Printf("warning: craft_from_cargo failed (non-fatal): %v", err)
			return nil
		}
		d.Logger.Printf("crafted %d items from cargo", crafted)
		return nil

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

const (
	arrivalPollInterval = time.Second
	arrivalTimeout      = game.SleepJump + game.SleepTick // 30s
)

// waitForArrival polls state.CurrentPOI until it matches the target POI,
// then refreshes system data. This handles the async nature of travel where
// the server sends "pending" immediately and "arrived" on the next tick.
func (d *ClientDispatcher) waitForArrival(ctx context.Context, targetPOI string) {
	deadline := time.After(arrivalTimeout)
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline:
			d.Logger.Printf("warning: timed out waiting for arrival at %s", targetPOI)
			d.fetchSystemData(ctx)
			return
		case <-time.After(arrivalPollInterval):
			state := d.Client.GetState()
			if state.CurrentPOI == targetPOI {
				d.Logger.Printf("arrived at %s", targetPOI)
				d.fetchSystemData(ctx)
				return
			}
		}
	}
}

// waitForSystemChange polls until state.Traveling becomes false after a jump,
// then refreshes system data.
func (d *ClientDispatcher) waitForSystemChange(ctx context.Context) {
	deadline := time.After(arrivalTimeout)
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline:
			d.Logger.Printf("warning: timed out waiting for jump completion")
			d.fetchSystemData(ctx)
			return
		case <-time.After(arrivalPollInterval):
			state := d.Client.GetState()
			if !state.Traveling {
				d.fetchSystemData(ctx)
				return
			}
		}
	}
}

// fetchSystemData calls GetSystem to populate POI data for condition evaluation.
func (d *ClientDispatcher) fetchSystemData(ctx context.Context) {
	if err := d.Client.GetSystem(ctx); err != nil {
		d.Logger.Printf("warning: failed to refresh system data: %v", err)
	}
	time.Sleep(game.SleepQuick)
}

// EnsureSystemData loads system POIs if they aren't populated yet.
// Call this before running a skill to make sure initial conditions can evaluate.
func (d *ClientDispatcher) EnsureSystemData(ctx context.Context) {
	state := d.Client.GetState()
	if len(state.System.POIs) == 0 {
		d.Logger.Printf("Fetching system data...")
		d.fetchSystemData(ctx)
	}
}

// isTickAction returns true if the action consumes a server tick.
func isTickAction(action string) bool {
	switch action {
	case "get_status", "get_system", "get_ship", "get_skills",
		"get_poi", "get_map", "get_version", "get_recipes",
		"get_wrecks", "get_notes", "get_listings", "get_trades",
		"wait", "help",
		"craft_from_cargo": // compound action, manages its own tick waits
		return false
	default:
		return true
	}
}
