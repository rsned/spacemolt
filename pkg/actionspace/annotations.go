package actionspace

// Annotation holds the preconditions, target generator, and metadata for a command.
type Annotation struct {
	Preconditions []Precondition
	Targets       func(*GameContext) []string

	// Category override (empty = use tag from OpenAPI spec).
	Category string
	// Summary override for virtual actions not in the spec.
	Summary string

	// IncludeNonMutation marks a command that lacks x-is-mutation in the spec
	// but should still be included in the action registry because it changes state.
	IncludeNonMutation bool
	// Virtual marks actions that don't exist as top-level OpenAPI paths
	// (e.g. battle_advance is a sub-action of the battle endpoint).
	Virtual bool
}

// CommandAnnotations returns the complete mapping of command name to annotation.
// This is the single source of truth for preconditions and target generators.
func CommandAnnotations() map[string]Annotation {
	return map[string]Annotation{
		// ===== Navigation =====
		"undock": {
			Preconditions: []Precondition{RequiresDocked},
		},
		"dock": {
			Preconditions: []Precondition{RequiresUndocked, RequiresNotInCombat},
			Targets:       stationPOIs,
		},
		"travel": {
			Preconditions: []Precondition{RequiresUndocked, RequiresFuel, RequiresNotInCombat, RequiresHasPOIs},
			Targets:       otherPOIs,
		},
		"jump": {
			Preconditions: []Precondition{RequiresUndocked, RequiresFuel, RequiresNotInCombat, RequiresHasConnections},
			Targets:       connectedSystems,
		},

		// ===== Mining =====
		"mine": {
			Preconditions: []Precondition{RequiresUndocked, RequiresCargoSpace, RequiresMiningPOI, RequiresNotInCombat},
		},

		// ===== Trading =====
		"buy": {
			Preconditions: []Precondition{RequiresDocked, RequiresCredits, RequiresCargoSpace},
		},
		"sell": {
			Preconditions: []Precondition{RequiresDocked, RequiresHasCargo},
		},
		"trade_offer": {
			Preconditions: []Precondition{RequiresNotInTransit, RequiresNearbyPlayers},
		},
		"trade_accept": {
			Preconditions: []Precondition{RequiresNotInTransit},
		},
		"trade_cancel": {
			IncludeNonMutation: true,
			Preconditions:      []Precondition{RequiresNotInTransit},
		},
		"trade_decline": {
			IncludeNonMutation: true,
			Preconditions:      []Precondition{RequiresNotInTransit},
		},

		// ===== Station Exchange =====
		"create_buy_order": {
			Preconditions: []Precondition{RequiresDocked, RequiresCredits},
		},
		"create_sell_order": {
			Preconditions: []Precondition{RequiresDocked, RequiresHasCargo},
		},
		"cancel_order": {
			Preconditions: []Precondition{RequiresDocked},
		},
		"modify_order": {
			Preconditions: []Precondition{RequiresDocked},
		},

		// ===== Combat =====
		"attack": {
			Preconditions: []Precondition{RequiresUndocked, RequiresNearbyPlayers},
		},
		"battle_advance": {
			Virtual:       true,
			Category:      "combat",
			Summary:       "Advance toward the enemy in battle",
			Preconditions: []Precondition{RequiresInCombat},
		},
		"battle_retreat": {
			Virtual:       true,
			Category:      "combat",
			Summary:       "Retreat from the enemy in battle",
			Preconditions: []Precondition{RequiresInCombat},
		},
		"cloak": {
			Preconditions: []Precondition{RequiresUndocked, RequiresNotInCombat},
		},
		"scan": {
			Preconditions: []Precondition{RequiresUndocked, RequiresNearbyPlayers},
		},
		"reload": {
			Preconditions: []Precondition{RequiresNotInTransit, RequiresHasCargo},
		},
		"self_destruct": {
			Preconditions: []Precondition{RequiresNotInTransit},
		},

		// ===== Salvage =====
		"loot_wreck": {
			Preconditions: []Precondition{RequiresUndocked, RequiresWrecks, RequiresCargoSpace},
		},
		"tow_wreck": {
			Preconditions: []Precondition{RequiresUndocked, RequiresWrecks},
		},
		"release_tow": {
			Preconditions: []Precondition{RequiresUndocked, RequiresTowingWreck},
		},
		"scrap_wreck": {
			Preconditions: []Precondition{RequiresUndocked, RequiresTowingWreck, RequiresCargoSpace},
		},
		"sell_wreck": {
			Preconditions: []Precondition{RequiresDocked, RequiresTowingWreck},
		},

		// ===== Ship Management =====
		"refuel": {
			Preconditions: []Precondition{RequiresNotInTransit, RequiresNotFullFuel},
		},
		"repair": {
			Preconditions: []Precondition{RequiresNotInTransit, RequiresDamaged},
		},
		"install_mod": {
			Preconditions: []Precondition{RequiresDocked, RequiresHasCargo},
		},
		"uninstall_mod": {
			Preconditions: []Precondition{RequiresDocked},
		},
		"switch_ship": {
			Preconditions: []Precondition{RequiresDocked, RequiresStoredShips},
		},
		"buy_listed_ship": {
			Preconditions: []Precondition{RequiresDocked, RequiresCredits},
		},
		// sell_ship retired server-side 2026-07-26 — see actions.go.
		"list_ship_for_sale": {
			Preconditions: []Precondition{RequiresDocked, RequiresStoredShips},
		},
		"cancel_ship_listing": {
			Preconditions: []Precondition{RequiresDocked},
		},
		"commission_ship": {
			Preconditions: []Precondition{RequiresDocked, RequiresCredits},
		},
		"cancel_commission": {
			Preconditions: []Precondition{RequiresDocked},
		},
		"supply_commission": {
			Preconditions: []Precondition{RequiresDocked, RequiresHasCargo},
		},
		"name_ship": {
			Preconditions: []Precondition{RequiresNotInTransit},
		},
		"use_item": {
			Preconditions: []Precondition{RequiresNotInTransit, RequiresHasCargo},
		},

		// ===== Cargo =====
		"jettison": {
			Preconditions: []Precondition{RequiresNotInTransit, RequiresHasCargo},
		},

		// ===== Storage =====
		"deposit_items": {
			Preconditions: []Precondition{RequiresDocked, RequiresHasCargo},
		},
		"withdraw_items": {
			Preconditions: []Precondition{RequiresDocked, RequiresCargoSpace},
		},
		"send_gift": {
			Preconditions: []Precondition{RequiresDocked},
		},

		// ===== Crafting =====
		"craft": {
			Preconditions: []Precondition{RequiresDocked, RequiresHasCargo},
		},

		// ===== Missions =====
		"accept_mission": {
			Preconditions: []Precondition{RequiresDocked},
		},
		"complete_mission": {
			Preconditions: []Precondition{RequiresDocked},
		},
		"abandon_mission": {
			IncludeNonMutation: true,
			Preconditions:      []Precondition{RequiresNotInTransit},
		},
		"distress_signal": {
			Preconditions: []Precondition{RequiresUndocked},
		},

		// ===== Faction =====
		"create_faction": {
			Preconditions: []Precondition{RequiresNotInTransit},
		},
		"join_faction": {
			Preconditions: []Precondition{RequiresNotInTransit},
		},
		"leave_faction": {
			Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction},
		},
		"faction_invite": {
			Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction},
		},
		"faction_kick": {
			Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction},
		},
		"faction_promote": {
			Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction},
		},
		"faction_deposit_credits": {
			Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction, RequiresCredits},
		},
		"faction_deposit_items": {
			Preconditions: []Precondition{RequiresDocked, RequiresFaction, RequiresHasCargo},
		},
		"faction_withdraw_credits": {
			Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction},
		},
		"faction_withdraw_items": {
			Preconditions: []Precondition{RequiresDocked, RequiresFaction, RequiresCargoSpace},
		},
		"faction_declare_war": {
			Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction},
		},
		"faction_propose_peace": {
			Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction},
		},
		"faction_accept_peace": {
			Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction},
		},
		"faction_propose_ally": {
			Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction},
		},
		"faction_accept_ally": {
			Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction},
		},
		"faction_remove_ally": {
			Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction},
		},
		"faction_set_enemy": {
			Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction},
		},
		"faction_submit_intel": {
			Preconditions: []Precondition{RequiresNotInTransit, RequiresFaction},
		},
		"faction_submit_trade_intel": {
			Preconditions: []Precondition{RequiresDocked, RequiresFaction},
		},
		"espionage": {
			Preconditions: []Precondition{RequiresDocked, RequiresFaction},
		},
		"faction_post_mission": {
			Preconditions: []Precondition{RequiresDocked, RequiresFaction, RequiresCredits},
		},
		"faction_cancel_mission": {
			Preconditions: []Precondition{RequiresDocked, RequiresFaction},
		},
		"faction_create_buy_order": {
			Preconditions: []Precondition{RequiresDocked, RequiresFaction},
		},
		"faction_create_sell_order": {
			Preconditions: []Precondition{RequiresDocked, RequiresFaction},
		},

		// ===== Social =====
		"fleet": {
			Preconditions: []Precondition{RequiresNotInTransit},
		},

		// ===== Insurance =====
		"buy_insurance": {
			Preconditions: []Precondition{RequiresDocked, RequiresCredits},
		},
		"set_home_base": {
			Preconditions: []Precondition{RequiresDocked},
		},

		// ===== Exploration =====
		"survey_system": {
			Preconditions: []Precondition{RequiresUndocked, RequiresNotInCombat},
		},
	}
}
