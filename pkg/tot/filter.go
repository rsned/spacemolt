package tot

import "github.com/rsned/spacemolt/pkg/game"

// ValidActions returns the set of actions that are valid for the current game state.
// Actions are split into docked/space context and always-available query actions.
func ValidActions(state *game.State) []ActionOption {
	var actions []ActionOption
	if state.Doc {
		actions = dockedActions(state)
	} else {
		actions = spaceActions(state)
	}
	actions = append(actions, queryActions()...)
	return actions
}

// dockedActions returns actions available when the ship is docked at a station.
func dockedActions(state *game.State) []ActionOption {
	actions := []ActionOption{
		{Action: "undock", Description: "Undock and return to space"},
		{Action: "view_market", Description: "View the market listings at this station"},
		{Action: "buy", Description: "Buy items from the market"},
		{Action: "sell", Description: "Sell items to the market"},
		{Action: "sell_all", Description: "Sell all cargo to the market"},
		{Action: "view_storage", Description: "View items in station storage"},
		{Action: "deposit_credits", Description: "Deposit credits into station storage"},
		{Action: "withdraw_items", Description: "Withdraw items from station storage"},
		{Action: "list_ships", Description: "List all owned ships"},
		{Action: "switch_ship", Description: "Switch to a different owned ship"},
		{Action: "get_recipes", Description: "View available crafting recipes"},
		{Action: "craft", Description: "Craft items using available recipes"},
		{Action: "get_missions", Description: "View available missions"},
		{Action: "complete_mission", Description: "Complete an accepted mission"},
	}

	if state.Ship.Hull < state.Ship.MaxHull {
		actions = append(actions, ActionOption{
			Action:      "repair",
			Description: "Repair ship hull damage",
		})
	}

	if state.Ship.Fuel < state.Ship.MaxFuel {
		actions = append(actions, ActionOption{
			Action:      "refuel",
			Description: "Refuel the ship",
		})
	}

	return actions
}

// spaceActions returns actions available when the ship is in space (not docked).
func spaceActions(state *game.State) []ActionOption {
	actions := []ActionOption{
		{Action: "scan", Description: "Scan the current area for players and objects"},
	}

	// Travel to POIs in the current system.
	if len(state.System.POIs) > 0 {
		targets := make([]string, 0, len(state.System.POIs))
		for _, poi := range state.System.POIs {
			targets = append(targets, poi.ID)
		}
		actions = append(actions, ActionOption{
			Action:      "travel",
			Description: "Travel to a point of interest in the current system",
			Targets:     targets,
		})
	}

	// Dock at stations or outposts.
	for _, poi := range state.System.POIs {
		if poi.Type == "station" || poi.Type == "outpost" {
			actions = append(actions, ActionOption{
				Action:      "dock",
				Description: "Dock at a station or outpost",
			})
			break
		}
	}

	// Jump to connected systems.
	if len(state.System.Connections) > 0 {
		targets := make([]string, 0, len(state.System.Connections))
		for _, conn := range state.System.Connections {
			targets = append(targets, conn.SystemID)
		}
		actions = append(actions, ActionOption{
			Action:      "jump",
			Description: "Jump to a connected star system",
			Targets:     targets,
		})
	}

	// Mine at asteroid belts or asteroids.
	for _, poi := range state.System.POIs {
		if poi.Type == "asteroid_belt" || poi.Type == "asteroid" {
			actions = append(actions, ActionOption{
				Action:      "mine",
				Description: "Mine resources from an asteroid or asteroid belt",
			})
			break
		}
	}

	// Combat actions when in combat.
	if state.InCombat {
		actions = append(actions,
			ActionOption{Action: "battle_advance", Description: "Advance toward the enemy in battle"},
			ActionOption{Action: "battle_retreat", Description: "Retreat from the enemy in battle"},
			ActionOption{Action: "battle_stance", Description: "Change combat stance"},
			ActionOption{Action: "battle_target", Description: "Select a battle target"},
		)
	}

	return actions
}

// queryActions returns informational actions that are always available (no tick cost).
func queryActions() []ActionOption {
	return []ActionOption{
		{Action: "get_status", Description: "Get current player and ship status"},
		{Action: "get_system", Description: "Get information about the current system"},
		{Action: "get_cargo", Description: "View current cargo hold contents"},
		{Action: "get_skills", Description: "View player skills and XP progress"},
		{Action: "get_map", Description: "View the galaxy map"},
		{Action: "get_nearby", Description: "List nearby players and objects"},
	}
}
