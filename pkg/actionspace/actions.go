package actionspace

// Target generator functions return dynamic target lists from context.

func stationPOIs(gc *GameContext) []string {
	var targets []string
	for _, poi := range gc.SystemPOIs {
		if poi.HasBase && poi.ID != gc.CurrentPOIID {
			targets = append(targets, poi.ID)
		}
	}
	return targets
}

func otherPOIs(gc *GameContext) []string {
	var targets []string
	for _, poi := range gc.SystemPOIs {
		if poi.ID != gc.CurrentPOIID {
			targets = append(targets, poi.ID)
		}
	}
	return targets
}

func connectedSystems(gc *GameContext) []string {
	targets := make([]string, len(gc.Connections))
	for i, conn := range gc.Connections {
		targets[i] = conn.SystemID
	}
	return targets
}

// AllActions is the complete registry of mutation commands.
// Action names MUST match the cases in pkg/agent/runner.go executeDecision().
var AllActions = []Action{
	// ===== Navigation =====
	{
		Name: "undock", Summary: "Undock from a base",
		Category: "navigation", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked},
	},
	{
		Name: "dock", Summary: "Dock at a base",
		Category: "navigation", IsMutation: true,
		Preconditions: []Precondition{RequiresUndocked, RequiresNotInCombat},
		Targets:       stationPOIs,
	},
	{
		Name: "travel", Summary: "Travel to a POI in current system",
		Category: "navigation", IsMutation: true,
		Preconditions: []Precondition{RequiresUndocked, RequiresFuel, RequiresNotInCombat, RequiresHasPOIs},
		Targets:       otherPOIs,
	},
	{
		Name: "jump", Summary: "Jump to an adjacent star system",
		Category: "navigation", IsMutation: true,
		Preconditions: []Precondition{RequiresUndocked, RequiresFuel, RequiresNotInCombat, RequiresHasConnections},
		Targets:       connectedSystems,
	},

	// ===== Mining =====
	{
		Name: "mine", Summary: "Mine resources from asteroids, ice fields, or gas clouds",
		Category: "mining", IsMutation: true,
		Preconditions: []Precondition{RequiresUndocked, RequiresCargoSpace, RequiresMiningPOI, RequiresNotInCombat},
	},

	// ===== Trading =====
	{
		Name: "buy", Summary: "Buy items at market price from the station exchange",
		Category: "trading", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked, RequiresCredits, RequiresCargoSpace},
	},
	{
		Name: "sell", Summary: "Sell items at market price on the station exchange",
		Category: "trading", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked, RequiresHasCargo},
	},
	{
		Name: "trade_offer", Summary: "Offer a trade to another player",
		Category: "trading", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit, RequiresNearbyPlayers},
	},
	{
		Name: "trade_accept", Summary: "Accept a trade offer",
		Category: "trading", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit},
	},
	{
		Name: "trade_cancel", Summary: "Cancel your trade offer",
		Category: "trading", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit},
	},
	{
		Name: "trade_decline", Summary: "Decline a trade offer",
		Category: "trading", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit},
	},

	// ===== Station Exchange =====
	{
		Name: "create_buy_order", Summary: "Place a buy offer on the station exchange",
		Category: "exchange", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked, RequiresCredits},
	},
	{
		Name: "create_sell_order", Summary: "List items for sale on the station exchange",
		Category: "exchange", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked, RequiresHasCargo},
	},
	{
		Name: "cancel_order", Summary: "Cancel an active order and return escrow",
		Category: "exchange", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked},
	},
	{
		Name: "modify_order", Summary: "Change the price on an existing order",
		Category: "exchange", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked},
	},

	// ===== Combat =====
	{
		Name: "attack", Summary: "Attack another player, pirate, or empire NPC",
		Category: "combat", IsMutation: true,
		Preconditions: []Precondition{RequiresUndocked, RequiresNearbyPlayers},
	},
	{
		Name: "battle_advance", Summary: "Advance toward the enemy in battle",
		Category: "combat", IsMutation: true,
		Preconditions: []Precondition{RequiresInCombat},
	},
	{
		Name: "battle_retreat", Summary: "Retreat from the enemy in battle",
		Category: "combat", IsMutation: true,
		Preconditions: []Precondition{RequiresInCombat},
	},
	{
		Name: "cloak", Summary: "Toggle cloaking device",
		Category: "combat", IsMutation: true,
		Preconditions: []Precondition{RequiresUndocked, RequiresNotInCombat},
	},
	{
		Name: "scan", Summary: "Scan another player or empire NPC",
		Category: "combat", IsMutation: true,
		Preconditions: []Precondition{RequiresUndocked, RequiresNearbyPlayers},
	},
	{
		Name: "reload", Summary: "Reload a weapon's magazine from ammo in cargo",
		Category: "combat", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit, RequiresHasCargo},
	},
	{
		Name: "self_destruct", Summary: "Destroy your own ship",
		Category: "combat", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit},
	},

	// ===== Salvage =====
	{
		Name: "loot_wreck", Summary: "Loot items from a wreck",
		Category: "salvage", IsMutation: true,
		Preconditions: []Precondition{RequiresUndocked, RequiresWrecks, RequiresCargoSpace},
	},
	{
		Name: "salvage_wreck", Summary: "Salvage a wreck for raw materials",
		Category: "salvage", IsMutation: true,
		Preconditions: []Precondition{RequiresUndocked, RequiresWrecks, RequiresCargoSpace},
	},
	{
		Name: "tow_wreck", Summary: "Attach a tow line to a wreck for hauling",
		Category: "salvage", IsMutation: true,
		Preconditions: []Precondition{RequiresUndocked, RequiresWrecks},
	},
	{
		Name: "release_tow", Summary: "Release a towed wreck at your current location",
		Category: "salvage", IsMutation: true,
		Preconditions: []Precondition{RequiresUndocked, RequiresTowingWreck},
	},
	{
		Name: "scrap_wreck", Summary: "Scrap a towed wreck for salvage materials",
		Category: "salvage", IsMutation: true,
		Preconditions: []Precondition{RequiresUndocked, RequiresTowingWreck, RequiresCargoSpace},
	},
	{
		Name: "sell_wreck", Summary: "Sell a towed wreck to the salvage yard for credits",
		Category: "salvage", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked, RequiresTowingWreck},
	},

	// ===== Ship Management =====
	{
		Name: "refuel", Summary: "Refuel your ship or transfer fuel to another ship",
		Category: "ship", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit, RequiresNotFullFuel},
	},
	{
		Name: "repair", Summary: "Repair hull at station or in space with repair kits",
		Category: "ship", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit, RequiresDamaged},
	},
	{
		Name: "repair_module", Summary: "Repair wear on a module using a Repair Kit",
		Category: "ship", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit, RequiresHasCargo},
	},
	{
		Name: "install_mod", Summary: "Install a module on your ship",
		Category: "ship", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked, RequiresHasCargo},
	},
	{
		Name: "uninstall_mod", Summary: "Uninstall a module from your ship",
		Category: "ship", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked},
	},
	{
		Name: "switch_ship", Summary: "Switch to a different ship stored at this station",
		Category: "ship", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked, RequiresStoredShips},
	},
	{
		Name: "buy_listed_ship", Summary: "Purchase a ship from the exchange",
		Category: "ship", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked, RequiresCredits},
	},
	{
		Name: "sell_ship", Summary: "Sell a stored ship at the current station",
		Category: "ship", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked, RequiresStoredShips},
	},
	{
		Name: "list_ship_for_sale", Summary: "List a stored ship for sale on the exchange",
		Category: "ship", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked, RequiresStoredShips},
	},
	{
		Name: "cancel_ship_listing", Summary: "Remove your ship listing from the exchange",
		Category: "ship", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked},
	},
	{
		Name: "commission_ship", Summary: "Commission a ship to be built at this shipyard",
		Category: "ship", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked, RequiresCredits},
	},
	{
		Name: "cancel_commission", Summary: "Cancel a pending or in-progress ship commission",
		Category: "ship", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked},
	},
	{
		Name: "supply_commission", Summary: "Donate materials to a commission",
		Category: "ship", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked, RequiresHasCargo},
	},
	{
		Name: "name_ship", Summary: "Set or clear a custom name for your active ship",
		Category: "ship", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit},
	},
	{
		Name: "use_item", Summary: "Use a consumable item from cargo",
		Category: "ship", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit, RequiresHasCargo},
	},

	// ===== Cargo =====
	{
		Name: "jettison", Summary: "Jettison items from cargo into space",
		Category: "cargo", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit, RequiresHasCargo},
	},

	// ===== Storage =====
	{
		Name: "deposit_items", Summary: "Move items from cargo to station storage",
		Category: "storage", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked, RequiresHasCargo},
	},
	{
		Name: "withdraw_items", Summary: "Move items from station storage to cargo",
		Category: "storage", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked, RequiresCargoSpace},
	},
	{
		Name: "send_gift", Summary: "Send items, credits, or a ship to another player",
		Category: "storage", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked},
	},

	// ===== Crafting =====
	{
		Name: "craft", Summary: "Craft an item (supports batch crafting)",
		Category: "crafting", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked, RequiresHasCargo},
	},

	// ===== Missions =====
	{
		Name: "accept_mission", Summary: "Accept a mission from the mission board",
		Category: "missions", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked},
	},
	{
		Name: "complete_mission", Summary: "Complete a mission and claim rewards",
		Category: "missions", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked},
	},
	{
		Name: "abandon_mission", Summary: "Abandon an active mission",
		Category: "missions", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit},
	},
	{
		Name: "distress_signal", Summary: "Broadcast a distress signal for emergency rescue",
		Category: "missions", IsMutation: true,
		Preconditions: []Precondition{RequiresUndocked},
	},

	// ===== Faction =====
	{
		Name: "create_faction", Summary: "Create a new faction",
		Category: "faction", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit},
	},
	{
		Name: "join_faction", Summary: "Join a faction via invitation",
		Category: "faction", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit},
	},
	{
		Name: "leave_faction", Summary: "Leave your faction",
		Category: "faction", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction},
	},
	{
		Name: "faction_invite", Summary: "Invite a player to your faction",
		Category: "faction", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction},
	},
	{
		Name: "faction_kick", Summary: "Kick a player from your faction",
		Category: "faction", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction},
	},
	{
		Name: "faction_promote", Summary: "Promote or demote a faction member",
		Category: "faction", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction},
	},
	{
		Name: "faction_deposit_credits", Summary: "Transfer credits to faction treasury",
		Category: "faction", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction, RequiresCredits},
	},
	{
		Name: "faction_deposit_items", Summary: "Move items from cargo to faction storage",
		Category: "faction", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked, RequiresFaction, RequiresHasCargo},
	},
	{
		Name: "faction_withdraw_credits", Summary: "Transfer credits from faction treasury",
		Category: "faction", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction},
	},
	{
		Name: "faction_withdraw_items", Summary: "Move items from faction storage to cargo",
		Category: "faction", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked, RequiresFaction, RequiresCargoSpace},
	},
	{
		Name: "faction_declare_war", Summary: "Declare war on another faction",
		Category: "faction", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction},
	},
	{
		Name: "faction_propose_peace", Summary: "Propose peace to a faction you're at war with",
		Category: "faction", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction},
	},
	{
		Name: "faction_accept_peace", Summary: "Accept a peace proposal",
		Category: "faction", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction},
	},
	{
		Name: "faction_propose_ally", Summary: "Propose a mutual alliance with another faction",
		Category: "faction", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction},
	},
	{
		Name: "faction_accept_ally", Summary: "Accept a pending alliance proposal",
		Category: "faction", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction},
	},
	{
		Name: "faction_remove_ally", Summary: "Dissolve an alliance with another faction",
		Category: "faction", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction},
	},
	{
		Name: "faction_set_enemy", Summary: "Mark another faction as enemy",
		Category: "faction", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction},
	},
	{
		Name: "faction_submit_intel", Summary: "Submit system intel to faction's shared map",
		Category: "faction", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction},
	},
	{
		Name: "faction_submit_trade_intel", Summary: "Submit market prices to faction ledger",
		Category: "faction", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked, RequiresFaction},
	},
	{
		Name: "faction_post_mission", Summary: "Post a mission on faction's mission board",
		Category: "faction", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked, RequiresFaction, RequiresCredits},
	},
	{
		Name: "faction_cancel_mission", Summary: "Cancel a posted faction mission",
		Category: "faction", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked, RequiresFaction},
	},
	{
		Name: "faction_create_buy_order", Summary: "Create a buy order for your faction",
		Category: "faction", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked, RequiresFaction},
	},
	{
		Name: "faction_create_sell_order", Summary: "Create a sell order for your faction",
		Category: "faction", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked, RequiresFaction},
	},

	// ===== Social =====
	{
		Name: "fleet", Summary: "Create and manage player fleets",
		Category: "social", IsMutation: true,
		Preconditions: []Precondition{RequiresNotInTransit},
	},

	// ===== Insurance =====
	{
		Name: "buy_insurance", Summary: "Purchase ship insurance",
		Category: "insurance", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked, RequiresCredits},
	},
	{
		Name: "set_home_base", Summary: "Set your home base for respawning",
		Category: "insurance", IsMutation: true,
		Preconditions: []Precondition{RequiresDocked},
	},

	// ===== Exploration =====
	{
		Name: "survey_system", Summary: "Scan for hidden deep core deposits",
		Category: "exploration", IsMutation: true,
		Preconditions: []Precondition{RequiresUndocked, RequiresNotInCombat},
	},
}
