package skills

import (
	"context"
	"fmt"
	"log"
	"strings"
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
	Client      game.GameClient
	Logger      *log.Logger
	TickDelay   time.Duration // delay after tick-consuming actions; 0 = default (11s)
	Route       *RouteProgress // Active route for multi-system travel
	baseDir     string         // Base directory for agent data (e.g., "data")
	agentID     string         // Agent identifier (e.g., "miner-1")
	FoundPOI    string         // ID of most recently found POI
	SkillParams map[string]string // Skill parameters for current execution
}

// NewClientDispatcher creates a dispatcher wrapping a connected game client.
func NewClientDispatcher(client game.GameClient, baseDir, agentID string, logger *log.Logger) *ClientDispatcher {
	return &ClientDispatcher{
		Client:      client,
		Logger:      logger,
		TickDelay:   game.SleepTick + time.Second, // 11s — wait for next tick
		baseDir:     baseDir,
		agentID:     agentID,
		SkillParams: make(map[string]string),
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
		// Check if target is a parameter reference
		jumpTarget := target
		if strings.HasPrefix(target, "$") {
			paramName := strings.TrimPrefix(target, "$")
			if val, ok := d.SkillParams[paramName]; ok {
				jumpTarget = val
			}
		}
		if err := d.Client.Jump(ctx, jumpTarget); err != nil {
			return err
		}
		d.waitForSystemChange(ctx)
		return nil
	case "find_route":
		if target == "" {
			return fmt.Errorf("find_route requires a target system")
		}
		// Check if target is a parameter reference
		routeTarget := target
		if strings.HasPrefix(target, "$") {
			paramName := strings.TrimPrefix(target, "$")
			if val, ok := d.SkillParams[paramName]; ok {
				routeTarget = val
			}
		}
		return d.doFindRoute(ctx, routeTarget)

	// Route persistence
	case "store_route_progress":
		return d.doStoreRouteProgress()
	case "load_route_progress":
		return d.doLoadRouteProgress()
	case "clear_route_progress":
		return d.doClearRouteProgress()

	// POI discovery
	case "find_poi_in_system":
		if target == "" {
			return fmt.Errorf("find_poi_in_system requires a target POI name")
		}
		// Check if target is a parameter reference
		poiTarget := target
		if strings.HasPrefix(target, "$") {
			paramName := strings.TrimPrefix(target, "$")
			if val, ok := d.SkillParams[paramName]; ok {
				poiTarget = val
			}
		}
		return d.doFindPOIInSystem(poiTarget)

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
		// Cast to *game.Client to access CraftFromCargo which is not in GameClient interface
		if client, ok := d.Client.(*game.Client); ok {
			crafted, err := client.CraftFromCargo(ctx, d.Logger, nil)
			if err != nil {
				d.Logger.Printf("warning: craft_from_cargo failed (non-fatal): %v", err)
				return nil
			}
			d.Logger.Printf("crafted %d items from cargo", crafted)
		}
		return nil

	case "craft_from_storage":
		if client, ok := d.Client.(*game.Client); ok {
			crafted, err := client.CraftFromStorage(ctx, d.Logger, nil)
			if err != nil {
				d.Logger.Printf("warning: craft_from_storage failed (non-fatal): %v", err)
				return nil
			}
			d.Logger.Printf("crafted %d items from storage", crafted)
		}
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

// doFindRoute calls the game client's FindRoute API and stores the result.
func (d *ClientDispatcher) doFindRoute(ctx context.Context, targetSystem string) error {
	route, err := d.Client.FindRoute(ctx, targetSystem)
	if err != nil {
		return fmt.Errorf("find route to %s: %w", targetSystem, err)
	}

	// Convert to RouteStep format
	steps := make([]RouteStep, len(route))
	for i, r := range route {
		steps[i] = RouteStep{
			SystemID: r.SystemID,
			Name:     r.Name,
			Jumps:    r.Jumps,
		}
	}

	d.Route = &RouteProgress{
		DestinationSystem: targetSystem,
		Route:             steps,
		CurrentStep:       0,
		Timestamp:         time.Now(),
	}

	d.Logger.Printf("[route] Found route to %s (%d steps)", targetSystem, len(steps))
	return nil
}

// doStoreRouteProgress saves the current route to a JSON file.
func (d *ClientDispatcher) doStoreRouteProgress() error {
	if d.Route == nil {
		return fmt.Errorf("no route to store")
	}
	if err := SaveRouteProgress(d.baseDir, d.agentID, d.Route); err != nil {
		return fmt.Errorf("save route progress: %w", err)
	}
	d.Logger.Printf("[route] Saved progress (step %d/%d)",
		d.Route.CurrentStep, len(d.Route.Route))
	return nil
}

// doLoadRouteProgress loads route from JSON file and validates against current position.
func (d *ClientDispatcher) doLoadRouteProgress() error {
	route, err := LoadRouteProgress(d.baseDir, d.agentID)
	if err != nil {
		return fmt.Errorf("load route progress: %w", err)
	}

	// Validate route against current position
	state := d.Client.GetState()
	if len(route.Route) > 0 && route.CurrentStep < len(route.Route) {
		expectedSystem := route.Route[route.CurrentStep].SystemID
		if state.System.ID != expectedSystem {
			// Try to find our position in the route
			found := false
			for i, step := range route.Route {
				if step.SystemID == state.System.ID {
					route.CurrentStep = i
					found = true
					d.Logger.Printf("[route] Adjusted step to %d based on current position", i)
					break
				}
			}
			if !found {
				return fmt.Errorf("current system %s not in saved route", state.System.ID)
			}
		}
	}

	d.Route = route
	d.Logger.Printf("[route] Loaded progress (step %d/%d to %s)",
		route.CurrentStep, len(route.Route), route.DestinationSystem)
	return nil
}

// doClearRouteProgress removes the route file and clears in-memory route.
func (d *ClientDispatcher) doClearRouteProgress() error {
	if err := ClearRouteProgress(d.baseDir, d.agentID); err != nil {
		return fmt.Errorf("clear route progress: %w", err)
	}
	d.Route = nil
	d.Logger.Printf("[route] Cleared progress")
	return nil
}

// doFindPOIInSystem searches for a POI by name in the current system.
func (d *ClientDispatcher) doFindPOIInSystem(poiName string) error {
	state := d.Client.GetState()

	for _, poi := range state.System.POIs {
		if strings.EqualFold(poi.Name, poiName) {
			d.FoundPOI = poi.ID
			d.Logger.Printf("[poi] Found POI: %s (%s)", poi.Name, poi.ID)
			return nil
		}
	}

	return fmt.Errorf("POI %q not found in system %s", poiName, state.System.Name)
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
		"wait", "help", "find_route", "store_route_progress",
		"load_route_progress", "clear_route_progress", "find_poi_in_system",
		"craft_from_cargo",  // compound action, manages its own tick waits
		"craft_from_storage": // compound action, manages its own tick waits
		return false
	default:
		return true
	}
}
