package actionspace

// Docking state preconditions.
var (
	RequiresDocked = Precondition{
		Name:        "requires_docked",
		Description: "Ship must be docked at a station",
		Check:       func(gc *GameContext) bool { return gc.Docked },
	}
	RequiresUndocked = Precondition{
		Name:        "requires_undocked",
		Description: "Ship must be undocked in space",
		Check:       func(gc *GameContext) bool { return !gc.Docked && !gc.InTransit },
	}
	RequiresNotInTransit = Precondition{
		Name:        "requires_not_in_transit",
		Description: "Ship must not be traveling",
		Check:       func(gc *GameContext) bool { return !gc.InTransit },
	}
)

// Resource preconditions.
var (
	RequiresFuel = Precondition{
		Name:        "requires_fuel",
		Description: "Ship needs fuel",
		Check:       func(gc *GameContext) bool { return gc.Fuel > 0 },
	}
	RequiresCargoSpace = Precondition{
		Name:        "requires_cargo_space",
		Description: "Cargo hold has room",
		Check:       func(gc *GameContext) bool { return gc.CargoUsed < gc.CargoCapacity },
	}
	RequiresCredits = Precondition{
		Name:        "requires_credits",
		Description: "Must have credits",
		Check:       func(gc *GameContext) bool { return gc.Credits > 0 },
	}
	RequiresDamaged = Precondition{
		Name:        "requires_damaged",
		Description: "Hull is below maximum",
		Check:       func(gc *GameContext) bool { return gc.Hull < gc.MaxHull },
	}
	RequiresNotFullFuel = Precondition{
		Name:        "requires_not_full_fuel",
		Description: "Fuel tank is not full",
		Check:       func(gc *GameContext) bool { return gc.Fuel < gc.MaxFuel },
	}
	RequiresHasCargo = Precondition{
		Name:        "requires_has_cargo",
		Description: "Must have items in cargo",
		Check:       func(gc *GameContext) bool { return gc.CargoItemCount > 0 },
	}
)

// Combat preconditions.
var (
	RequiresInCombat = Precondition{
		Name:        "requires_in_combat",
		Description: "Must be in combat",
		Check:       func(gc *GameContext) bool { return gc.InCombat || gc.InBattle },
	}
	RequiresNotInCombat = Precondition{
		Name:        "requires_not_in_combat",
		Description: "Must not be in combat",
		Check:       func(gc *GameContext) bool { return !gc.InCombat && !gc.InBattle },
	}
)

// Location preconditions.
var (
	RequiresMiningPOI = Precondition{
		Name:        "requires_mining_poi",
		Description: "Must be at a mineable location (asteroid belt, ice field, or gas cloud)",
		Check: func(gc *GameContext) bool {
			switch gc.CurrentPOIType {
			case "asteroid_belt", "asteroid_field", "asteroid", "ice_field", "gas_cloud":
				return true
			default:
				return false
			}
		},
	}
	RequiresHasConnections = Precondition{
		Name:        "requires_has_connections",
		Description: "System must have jump connections",
		Check:       func(gc *GameContext) bool { return len(gc.Connections) > 0 },
	}
	RequiresHasPOIs = Precondition{
		Name:        "requires_has_pois",
		Description: "System must have other POIs to travel to",
		Check:       func(gc *GameContext) bool { return len(gc.SystemPOIs) > 1 },
	}
)

// Player status preconditions.
var (
	RequiresFaction = Precondition{
		Name:        "requires_faction",
		Description: "Must be in a faction",
		Check:       func(gc *GameContext) bool { return gc.HasFaction },
	}
	RequiresNearbyPlayers = Precondition{
		Name:        "requires_nearby_players",
		Description: "Other players must be at this POI",
		Check:       func(gc *GameContext) bool { return gc.NearbyPlayerCount > 0 },
	}
	RequiresWrecks = Precondition{
		Name:        "requires_wrecks",
		Description: "Wrecks must be at current POI",
		Check:       func(gc *GameContext) bool { return gc.WreckCount > 0 },
	}
	RequiresTowingWreck = Precondition{
		Name:        "requires_towing_wreck",
		Description: "Must be towing a wreck",
		Check:       func(gc *GameContext) bool { return gc.TowingWreck },
	}
	RequiresStoredShips = Precondition{
		Name:        "requires_stored_ships",
		Description: "Must have ships stored at this station",
		Check:       func(gc *GameContext) bool { return gc.StoredShipCount > 0 },
	}
)
