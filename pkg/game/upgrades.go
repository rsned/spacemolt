package game

import (
	"context"
	"log"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
)

// UpgradeTier defines a ship/equipment upgrade configuration
type UpgradeTier struct {
	Name          string  // Human-readable name
	Threshold     float64 // Credits required
	FromShipClass string  // Current ship class to upgrade from
	ToShipClass   string  // Ship class to buy
	NumItems      int     // Number of items to buy and install (e.g., mining lasers, weapons)
	ItemID        string  // Item ID to buy (e.g., "mining_laser_1", "weapon_laser_1")
	LogEmoji      string  // Emoji for logging
	Capacity      string  // Cargo/capability description for logging
	SuccessMsg    string  // Success message template
}

// UpgradeProgression defines a career path with upgrade tiers
type UpgradeProgression struct {
	CareerName string        // e.g., "Mining", "Combat", "Trading", "Exploration"
	Tiers      []UpgradeTier // Ordered list of upgrade tiers
}

// Career upgrade configurations for each agent type
var (
	// MiningProgression defines the mining ship upgrade path
	// Uses actual ship IDs from server_docs/sol_ships.mining.json
	MiningProgression = UpgradeProgression{
		CareerName: "Mining",
		Tiers: []UpgradeTier{
			{
				Name:          "Improved Mining Ship",
				Threshold:     500.0,
				FromShipClass: "starter_mining",
				ToShipClass:   "mining_improved",
				NumItems:      2,
				ItemID:        "mining_laser_1",
				LogEmoji:      "⛏️",
				Capacity:      "75 cargo, 2 utility slots for mining lasers!",
				SuccessMsg:    "DIGGER!",
			},
			{
				Name:          "Gas Harvester",
				Threshold:     1800.0,
				FromShipClass: "mining_improved",
				ToShipClass:   "mining_gas",
				NumItems:      0,
				ItemID:        "",
				LogEmoji:      "💨",
				Capacity:      "80 cargo, 3 utility slots for gas harvesting!",
				SuccessMsg:    "SIPHON!",
			},
			{
				Name:          "Drillship",
				Threshold:     2000.0,
				FromShipClass: "mining_gas",
				ToShipClass:   "mining_enhanced",
				NumItems:      3,
				ItemID:        "mining_laser_1",
				LogEmoji:      "⛏️",
				Capacity:      "100 cargo, 3 utility slots for mining lasers!",
				SuccessMsg:    "DRILLSHIP!",
			},
			{
				Name:          "Excavator",
				Threshold:     5000.0,
				FromShipClass: "mining_enhanced",
				ToShipClass:   "mining_barge",
				NumItems:      4,
				ItemID:        "mining_laser_1",
				LogEmoji:      "⛏️⛏️",
				Capacity:      "150 cargo, 4 utility slots for mining lasers!",
				SuccessMsg:    "EXCAVATOR!",
			},
			{
				Name:          "Deeprock Harvester",
				Threshold:     25000.0,
				FromShipClass: "mining_barge",
				ToShipClass:   "mining_cruiser",
				NumItems:      6,
				ItemID:        "mining_laser_1",
				LogEmoji:      "⛏️⛏️⛏️",
				Capacity:      "400 cargo, 6 utility slots for mining lasers!",
				SuccessMsg:    "DEEPROCK HARVESTER!",
			},
			{
				Name:          "Titan Excavator",
				Threshold:     95000.0,
				FromShipClass: "mining_cruiser",
				ToShipClass:   "mining_capital",
				NumItems:      8,
				ItemID:        "mining_laser_1",
				LogEmoji:      "⛏️⛏️⛏️⛏️",
				Capacity:      "1200 cargo, 8 utility slots - capital mining vessel!",
				SuccessMsg:    "TITAN!",
			},
		},
	}

	// CombatProgression defines the fighter ship upgrade path
	// Uses actual ship IDs from the game server (sol_ships.fighter.json)
	CombatProgression = UpgradeProgression{
		CareerName: "Combat",
		Tiers: []UpgradeTier{
			{
				Name:          "Light Fighter",
				Threshold:     2500.0,
				FromShipClass: "fighter_scout",
				ToShipClass:   "fighter_light",
				NumItems:      2,
				ItemID:        "weapon_laser_1",
				LogEmoji:      "⚔️",
				Capacity:      "Fast & agile, 2 weapon slots!",
				SuccessMsg:    "VIPER!",
			},
			{
				Name:          "Medium Fighter",
				Threshold:     8000.0,
				FromShipClass: "fighter_light",
				ToShipClass:   "fighter_medium",
				NumItems:      3,
				ItemID:        "weapon_laser_1",
				LogEmoji:      "⚔️⚔️",
				Capacity:      "Balanced combat ship, 3 weapon slots!",
				SuccessMsg:    "TALON!",
			},
			{
				Name:          "Heavy Fighter",
				Threshold:     22000.0,
				FromShipClass: "fighter_medium",
				ToShipClass:   "fighter_heavy",
				NumItems:      4,
				ItemID:        "weapon_laser_1",
				LogEmoji:      "⚔️⚔️⚔️",
				Capacity:      "Heavy assault, 4 weapon slots!",
				SuccessMsg:    "WARHAWK!",
			},
			{
				Name:          "Elite Fighter",
				Threshold:     26000.0,
				FromShipClass: "fighter_heavy",
				ToShipClass:   "crimson_berserker",
				NumItems:      5,
				ItemID:        "weapon_laser_1",
				LogEmoji:      "⚔️⚔️⚔️⚔️",
				Capacity:      "Crimson Fleet elite, 5 weapon slots!",
				SuccessMsg:    "BLOOD BERSERKER!",
			},
			{
				Name:          "Ultimate Fighter",
				Threshold:     35000.0,
				FromShipClass: "crimson_berserker",
				ToShipClass:   "crimson_executioner",
				NumItems:      6,
				ItemID:        "weapon_laser_1",
				LogEmoji:      "⚔️⚔️⚔️⚔️⚔️",
				Capacity:      "Heavy weapons platform, 6 weapon slots!",
				SuccessMsg:    "EXECUTIONER!",
			},
		},
	}

	// TradingProgression defines the trader ship upgrade path
	// Uses actual ship IDs from server_docs/sol_ships.freighter.json
	TradingProgression = UpgradeProgression{
		CareerName: "Trading",
		Tiers: []UpgradeTier{
			{
				Name:          "Armed Hauler",
				Threshold:     600.0,
				FromShipClass: "starter_trading",
				ToShipClass:   "freighter_small",
				NumItems:      1,
				ItemID:        "weapon_laser_1",
				LogEmoji:      "💰",
				Capacity:      "200 cargo, 1 weapon slot for defense!",
				SuccessMsg:    "HAULER!",
			},
			{
				Name:          "Hauler",
				Threshold:     3000.0,
				FromShipClass: "freighter_small",
				ToShipClass:   "freighter_medium",
				NumItems:      2,
				ItemID:        "weapon_laser_1",
				LogEmoji:      "💰💰",
				Capacity:      "450 cargo, 2 weapon slots, 3 utility slots!",
				SuccessMsg:    "MERCHANTMAN!",
			},
			{
				Name:          "Defender",
				Threshold:     12000.0,
				FromShipClass: "freighter_medium",
				ToShipClass:   "freighter_armed",
				NumItems:      3,
				ItemID:        "weapon_laser_1",
				LogEmoji:      "💰💰💰",
				Capacity:      "350 cargo, 3 weapon slots for dangerous routes!",
				SuccessMsg:    "DEFENDER!",
			},
			{
				Name:          "Bulk Carrier",
				Threshold:     20000.0,
				FromShipClass: "freighter_armed",
				ToShipClass:   "freighter_large",
				NumItems:      2,
				ItemID:        "weapon_laser_1",
				LogEmoji:      "💰💰💰💰",
				Capacity:      "1000 cargo, 2 weapon slots, 4 utility slots!",
				SuccessMsg:    "BULK CARRIER!",
			},
			{
				Name:          "Leviathan",
				Threshold:     45000.0,
				FromShipClass: "freighter_large",
				ToShipClass:   "superfreighter",
				NumItems:      3,
				ItemID:        "weapon_laser_1",
				LogEmoji:      "💰💰💰💰💰",
				Capacity:      "2000 cargo - enormous trade capacity!",
				SuccessMsg:    "LEVIATHAN!",
			},
		},
	}

	// ExplorationProgression defines the explorer ship upgrade path
	// Uses actual ship IDs from server_docs/sol_ships.explorer.json
	ExplorationProgression = UpgradeProgression{
		CareerName: "Exploration",
		Tiers: []UpgradeTier{
			{
				Name:          "Pathfinder",
				Threshold:     15000.0,
				FromShipClass: "starter_exploration",
				ToShipClass:   "explorer",
				NumItems:      1,
				ItemID:        "scanner_advanced_1",
				LogEmoji:      "🔭",
				Capacity:      "Long-range exploration, 100 cargo, 4 utility slots!",
				SuccessMsg:    "PATHFINDER!",
			},
			{
				Name:          "Frontier Ranger",
				Threshold:     24000.0,
				FromShipClass: "explorer",
				ToShipClass:   "frontier_ranger",
				NumItems:      1,
				ItemID:        "scanner_advanced_2",
				LogEmoji:      "🔭🔭",
				Capacity:      "Combat-ready explorer, 120 cargo, 4 utility slots!",
				SuccessMsg:    "RANGER!",
			},
			{
				Name:          "Wasteland Survivor",
				Threshold:     32000.0,
				FromShipClass: "frontier_ranger",
				ToShipClass:   "frontier_survivor",
				NumItems:      1,
				ItemID:        "scanner_advanced_2",
				LogEmoji:      "🔭🔭🔭",
				Capacity:      "Self-sufficient, 200 cargo, 5 utility slots!",
				SuccessMsg:    "SURVIVOR!",
			},
			{
				Name:          "Trailblazer",
				Threshold:     38000.0,
				FromShipClass: "frontier_survivor",
				ToShipClass:   "expedition_ship",
				NumItems:      2,
				ItemID:        "scanner_advanced_2",
				LogEmoji:      "🔭🔭🔭🔭",
				Capacity:      "Deep space expedition, 180 cargo, 5 utility slots!",
				SuccessMsg:    "TRAILBLAZER!",
			},
			{
				Name:          "Horizon",
				Threshold:     68000.0,
				FromShipClass: "expedition_ship",
				ToShipClass:   "deep_space_explorer",
				NumItems:      3,
				ItemID:        "scanner_advanced_2",
				LogEmoji:      "🔭🔭🔭🔭🔭",
				Capacity:      "Ultimate explorer, 280 cargo, 6 utility slots!",
				SuccessMsg:    "HORIZON!",
			},
			{
				Name:          "Rim Corsair",
				Threshold:     75000.0,
				FromShipClass: "deep_space_explorer",
				ToShipClass:   "frontier_corsair",
				NumItems:      4,
				ItemID:        "weapon_laser_1",
				LogEmoji:      "🔭🔭🔭🔭🔭🔭",
				Capacity:      "Combat-explorer, 250 cargo, 5 utility slots, 4 weapon slots!",
				SuccessMsg:    "CORSAR!",
			},
		},
	}

	// PirateProgression defines the pirate/salvager ship upgrade path
	// Uses actual ship IDs from server_docs/sol_ships.raider.json and sol_ships.assault.json
	PirateProgression = UpgradeProgression{
		CareerName: "Piracy",
		Tiers: []UpgradeTier{
			{
				Name:          "Raiding Skiff",
				Threshold:     7000.0,
				FromShipClass: "starter_pirate",
				ToShipClass:   "raider",
				NumItems:      3,
				ItemID:        "weapon_laser_1",
				LogEmoji:      "🏴‍☠️",
				Capacity:      "Fast raider, 3 weapon slots, 40 cargo!",
				SuccessMsg:    "BLACKTHORN!",
			},
			{
				Name:          "Interception Ship",
				Threshold:     18000.0,
				FromShipClass: "raider",
				ToShipClass:   "interceptor",
				NumItems:      2,
				ItemID:        "weapon_laser_2",
				LogEmoji:      "🏴‍☠️🏴‍☠️",
				Capacity:      "Ultra-fast pursuit, 2 weapon slots, 20 cargo!",
				SuccessMsg:    "STILETTO!",
			},
			{
				Name:          "Heavy Ravager",
				Threshold:     28000.0,
				FromShipClass: "interceptor",
				ToShipClass:   "crimson_ravager",
				NumItems:      4,
				ItemID:        "weapon_laser_2",
				LogEmoji:      "🏴‍☠️🏴‍☠️🏴‍☠️",
				Capacity:      "Sustained assault, 4 weapon slots, 60 cargo!",
				SuccessMsg:    "RAVAGER!",
			},
			{
				Name:          "Death Reaper",
				Threshold:     72000.0,
				FromShipClass: "crimson_ravager",
				ToShipClass:   "crimson_reaper",
				NumItems:      5,
				ItemID:        "weapon_laser_3",
				LogEmoji:      "🏴‍☠️🏴‍☠️🏴‍☠️🏴‍☠️",
				Capacity:      "Freighter hunter, 5 weapon slots, 180 cargo!",
				SuccessMsg:    "REAPER!",
			},
			{
				Name:          "Boarding Frigate",
				Threshold:     42000.0,
				FromShipClass: "crimson_reaper",
				ToShipClass:   "assault_frigate",
				NumItems:      2,
				ItemID:        "weapon_laser_2",
				LogEmoji:      "🏴‍☠️🏴‍☠️🏴‍☠️🏴‍☠️🏴‍☠️",
				Capacity:      "Boarding specialist, 2 weapon slots, 40 cargo!",
				SuccessMsg:    "PIKE!",
			},
			{
				Name:          "Assault Cruiser",
				Threshold:     85000.0,
				FromShipClass: "assault_frigate",
				ToShipClass:   "assault_cruiser",
				NumItems:      3,
				ItemID:        "weapon_laser_3",
				LogEmoji:      "🏴‍☠️🏴‍☠️🏴‍☠️🏴‍☠️🏴‍☠️🏴‍☠️",
				Capacity:      "Station boarding, 3 weapon slots, 80 cargo!",
				SuccessMsg:    "BREACH!",
			},
		},
	}

	// SalvagerProgression defines the salvager ship upgrade path
	// Uses actual ship IDs from server_docs/sol_ships.industrial.json
	SalvagerProgression = UpgradeProgression{
		CareerName: "Salvage",
		Tiers: []UpgradeTier{
			{
				Name:          "Salvager",
				Threshold:     12000.0,
				FromShipClass: "starter_salvager",
				ToShipClass:   "salvager",
				NumItems:      1,
				ItemID:        "salvage_laser_1",
				LogEmoji:      "💎",
				Capacity:      "Wreck salvage, 250 cargo, 4 utility slots!",
				SuccessMsg:    "VULTURE!",
			},
			{
				Name:          "Research Vessel",
				Threshold:     32000.0,
				FromShipClass: "salvager",
				ToShipClass:   "scientific_vessel",
				NumItems:      2,
				ItemID:        "scanner_advanced_1",
				LogEmoji:      "💎💎",
				Capacity:      "Science & salvage, 150 cargo, 6 utility slots!",
				SuccessMsg:    "DISCOVERY!",
			},
			{
				Name:          "Mobile Refinery",
				Threshold:     35000.0,
				FromShipClass: "scientific_vessel",
				ToShipClass:   "refinery_ship",
				NumItems:      1,
				ItemID:        "refinery_module_1",
				LogEmoji:      "💎💎💎",
				Capacity:      "Process ore in space, 500 cargo, 6 utility slots!",
				SuccessMsg:    "ALCHEMIST!",
			},
			{
				Name:          "Drone Controller",
				Threshold:     45000.0,
				FromShipClass: "refinery_ship",
				ToShipClass:   "mining_drone_controller",
				NumItems:      2,
				ItemID:        "drone_bay_1",
				LogEmoji:      "💎💎💎💎",
				Capacity:      "Control drone fleets, 300 cargo, 6 utility slots!",
				SuccessMsg:    "OVERSEER!",
			},
			{
				Name:          "Mobile Shipyard",
				Threshold:     380000.0,
				FromShipClass: "mining_drone_controller",
				ToShipClass:   "mobile_shipyard",
				NumItems:      4,
				ItemID:        "drone_bay_2",
				LogEmoji:      "💎💎💎💎💎",
				Capacity:      "Build ships in space, 2000 cargo, 10 utility slots!",
				SuccessMsg:    "FORGE ETERNAL!",
			},
		},
	}

	// EngineerProgression defines the engineer ship upgrade path
	// Uses actual ship IDs from server_docs/sol_ships.support.json
	EngineerProgression = UpgradeProgression{
		CareerName: "Engineering",
		Tiers: []UpgradeTier{
			{
				Name:          "Repair Ship",
				Threshold:     5000.0,
				FromShipClass: "starter_engineer",
				ToShipClass:   "support_small",
				NumItems:      1,
				ItemID:        "repair_toolkit_1",
				LogEmoji:      "🔧",
				Capacity:      "Field repairs, 100 cargo, 3 utility slots!",
				SuccessMsg:    "MECHANIC!",
			},
			{
				Name:          "Construction Vessel",
				Threshold:     15000.0,
				FromShipClass: "support_small",
				ToShipClass:   "support_medium",
				NumItems:      2,
				ItemID:        "construction_module_1",
				LogEmoji:      "🔧🔧",
				Capacity:      "Build bases, 150 cargo, 6 utility slots!",
				SuccessMsg:    "BUILDER!",
			},
			{
				Name:          "Heavy Constructor",
				Threshold:     35000.0,
				FromShipClass: "support_medium",
				ToShipClass:   "support_large",
				NumItems:      3,
				ItemID:        "construction_module_2",
				LogEmoji:      "🔧🔧🔧",
				Capacity:      "Major construction, 300 cargo, 6 utility slots!",
				SuccessMsg:    "ARCHITECT!",
			},
		},
	}

	// CraftsmanProgression defines the production craftsman ship upgrade path
	// Uses actual ship IDs from server_docs/sol_ships.industrial.json
	CraftsmanProgression = UpgradeProgression{
		CareerName: "Crafting",
		Tiers: []UpgradeTier{
			{
				Name:          "Mobile Factory",
				Threshold:     12000.0,
				FromShipClass: "starter_craftsman",
				ToShipClass:   "factory_ship",
				NumItems:      2,
				ItemID:        "fabricator_1",
				LogEmoji:      "⚒️",
				Capacity:      "On-site production, 200 cargo, 4 utility slots!",
				SuccessMsg:    "FACTORY!",
			},
			{
				Name:          "Industrial Platform",
				Threshold:     32000.0,
				FromShipClass: "factory_ship",
				ToShipClass:   "industrial_platform",
				NumItems:      3,
				ItemID:        "fabricator_2",
				LogEmoji:      "⚒️⚒️",
				Capacity:      "Mass production, 500 cargo, 6 utility slots!",
				SuccessMsg:    "PLATFORM!",
			},
			{
				Name:          "Production Hub",
				Threshold:     75000.0,
				FromShipClass: "industrial_platform",
				ToShipClass:   "manufacturing_hub",
				NumItems:      4,
				ItemID:        "fabricator_3",
				LogEmoji:      "⚒️⚒️⚒️",
				Capacity:      "Industrial complex, 1500 cargo, 8 utility slots!",
				SuccessMsg:    "HUB!",
			},
		},
	}
)

// GetShipClassMaxSlots returns maximum number of equipment slots for a ship class
// For fighters/pirates: returns weapon_slots
// For miners/salvagers/traders/engineers: returns utility slots
// For explorers/craftsmen: returns utility slots
// Uses actual ship data from game server
func GetShipClassMaxSlots(shipClass string) int {
	// TODO: Load from server data instead of hardcoding
	// Data from server_docs/sol_ships.*.json
	switch shipClass {
	// Mining ships - utility slots (from sol_ships.mining.json)
	case "starter_mining":
		return 1 // Prospector: 2 utility slots
	case "mining_improved":
		return 2 // Digger: 2 utility slots
	case "mining_gas":
		return 3 // Siphon: 3 utility slots
	case "mining_enhanced":
		return 3 // Drillship: 3 utility slots
	case "mining_barge":
		return 4 // Excavator: 4 utility slots
	case "mining_cruiser":
		return 6 // Deeprock Harvester: 6 utility slots
	case "mining_capital":
		return 8 // Titan Excavator: 8 utility slots
	// Fighter ships - weapon slots (from sol_ships.fighter.json)
	case "fighter_scout":
		return 2 // Sparrow: 2 weapon slots
	case "fighter_light":
		return 2 // Viper: 2 weapon slots
	case "fighter_medium":
		return 3 // Talon: 3 weapon slots
	case "fighter_heavy":
		return 4 // Warhawk: 4 weapon slots
	case "crimson_berserker":
		return 5 // Blood Berserker: 5 weapon slots
	case "crimson_executioner":
		return 6 // Executioner: 6 weapon slots
	case "solarian_champion":
		return 4 // Sunfire Champion: 4 weapon slots
	// Freighter ships - weapon/defense slots (from sol_ships.freighter.json)
	case "freighter_small":
		return 1 // Mule: 1 weapon slot
	case "freighter_medium":
		return 2 // Hauler: 2 weapon slots
	case "freighter_armed":
		return 3 // Defender: 3 weapon slots
	case "freighter_large":
		return 2 // Bulk Carrier: 2 weapon slots
	case "superfreighter":
		return 3 // Leviathan: 3 weapon slots
	// Explorer ships - utility slots (from sol_ships.explorer.json)
	case "explorer":
		return 4 // Pathfinder: 4 utility slots
	case "frontier_ranger":
		return 4 // Frontier Ranger: 4 utility slots
	case "frontier_survivor":
		return 5 // Wasteland Survivor: 5 utility slots
	case "expedition_ship":
		return 5 // Trailblazer: 5 utility slots
	case "deep_space_explorer":
		return 6 // Horizon: 6 utility slots
	case "frontier_corsair":
		return 5 // Rim Corsair: 5 utility slots
	// Raider ships - weapon slots (from sol_ships.raider.json)
	case "raider":
		return 3 // Blackthorn: 3 weapon slots
	case "interceptor":
		return 2 // Stiletto: 2 weapon slots
	case "crimson_ravager":
		return 4 // Crimson Ravager: 4 weapon slots
	case "crimson_reaper":
		return 5 // Death Reaper: 5 weapon slots
	// Assault ships - weapon slots (from sol_ships.assault.json)
	case "assault_frigate":
		return 2 // Boarding Pike: 2 weapon slots
	case "assault_cruiser":
		return 3 // Breach: 3 weapon slots
	case "solarian_templar":
		return 4 // Templar: 4 weapon slots
	case "assault_carrier":
		return 4 // Storm Bringer: 4 weapon slots
	// Industrial ships - utility slots (from sol_ships.industrial.json)
	case "salvager":
		return 4 // Vulture: 4 utility slots
	case "scientific_vessel":
		return 6 // Discovery: 6 utility slots
	case "refinery_ship":
		return 6 // Alchemist: 6 utility slots
	case "mining_drone_controller":
		return 6 // Overseer Platform: 6 utility slots
	case "mobile_shipyard":
		return 10 // Forge Eternal: 10 utility slots
	// Stealth ships - utility slots (from sol_ships.stealth.json)
	case "voidborn_specter":
		return 4 // Void Specter: 4 utility slots
	case "stealth_transport":
		return 4 // Whisper: 4 utility slots
	case "stealth_ship":
		return 4 // Phantom: 4 utility slots
	case "stealth_bomber":
		return 5 // Specter: 5 utility slots
	// Starter ships - default slots
	case "starter_pirate":
		return 2 // Default pirate starter: 2 weapon slots
	case "starter_salvager":
		return 2 // Default salvager: 2 utility slots
	case "starter_engineer":
		return 2 // Default engineer: 2 utility slots
	case "starter_craftsman":
		return 2 // Default craftsman: 2 utility slots
	default:
		return 2 // Most ships have at least 2 slots
	}
}

// CanUpgradeAnyShip checks if any ship upgrade is affordable based on current ship class
func CanUpgradeAnyShip(currentShipClass string, availableCredits float64, tiers []UpgradeTier) bool {
	for _, tier := range tiers {
		if currentShipClass == tier.FromShipClass && availableCredits >= tier.Threshold {
			return true
		}
	}
	return false
}

// PerformShipUpgrade handles the complete ship upgrade process:
// 1. Sell all cargo
// 2. Uninstall all modules
// 3. Buy new ship
// 4. Buy equipment items
// 5. Install equipment items
func PerformShipUpgrade(client *Client, logger *log.Logger, ctx context.Context, tier UpgradeTier, availableCredits float64) bool {
	state := client.GetState()

	// Check if we can upgrade based on current ship and credits
	if state.Ship.ClassID != tier.FromShipClass || availableCredits < tier.Threshold {
		return false
	}

	logger.Printf("%s UPGRADE TIME! You have %.2f credits - upgrading to %s!", tier.LogEmoji, availableCredits, tier.Name)

	// CRITICAL: Sell all cargo first (it will be lost when switching ships!)
	if len(state.Ship.Cargo) > 0 {
		logger.Printf("📦 Selling all cargo before ship upgrade...")
		if err := client.SellAll(ctx); err != nil {
			logger.Printf("Failed to sell cargo: %v", err)
		} else {
			logger.Printf("✅ Cargo sold!")
			time.Sleep(3 * time.Second)
		}
	}

	// CRITICAL: Uninstall all utility slot modules first!
	if len(state.Ship.Modules) > 0 {
		logger.Printf("🔧 Uninstalling modules before ship upgrade...")
		for _, moduleID := range state.Ship.Modules {
			uninstallMsg := protocol.Message{
				Type: "uninstall_mod",
				Payload: map[string]any{
					"module_id": moduleID,
				},
			}
			if err := client.Send(ctx, uninstallMsg); err != nil {
				logger.Printf("Failed to uninstall module %s: %v", moduleID, err)
			} else {
				logger.Printf("✅ Uninstalled module: %s", moduleID)
			}
			time.Sleep(10 * time.Second) // Wait between uninstalls to respect game tick rate
		}
	}

	logger.Printf("🚀 Purchasing %s ship (%s)...", tier.ToShipClass, tier.Name)

	// Buy new ship using direct protocol message
	buyShipMsg := protocol.Message{
		Type: "buy_ship",
		Payload: map[string]any{
			"ship_class": tier.ToShipClass,
		},
	}
	if err := client.Send(ctx, buyShipMsg); err != nil {
		logger.Printf("Failed to buy ship: %v", err)
		return false
	}

	logger.Printf("✅ SHIP UPGRADED TO %s", tier.SuccessMsg)
	logger.Printf("✅ New capacity: %s", tier.Capacity)
	time.Sleep(5 * time.Second) // Wait longer for ship upgrade to process

	// If tier includes equipment items, buy and install them
	if tier.NumItems > 0 && tier.ItemID != "" {
		state = client.GetState()

		logger.Printf("⛏️  Now purchasing %d %s...", tier.NumItems, tier.ItemID)
		if err := client.Buy(ctx, tier.ItemID, float64(tier.NumItems)); err != nil {
			logger.Printf("Failed to buy items: %v", err)
			return true // Ship upgraded, equipment failed
		}

		logger.Printf("✅ Purchased %d %s!", tier.NumItems, tier.ItemID)
		time.Sleep(3 * time.Second)

		// Install each item
		for i := 1; i <= tier.NumItems; i++ {
			if err := client.Install(ctx, tier.ItemID); err != nil {
				logger.Printf("Failed to install %s #%d: %v", tier.ItemID, i, err)
			} else {
				logger.Printf("✅ %s #%d installed!", tier.ItemID, i)
			}
			time.Sleep(10 * time.Second) // Wait between installs to respect game tick rate
		}
	}

	// Generate success message with appropriate emoji
	itemEmoji := ""
	switch tier.NumItems {
	case 3:
		itemEmoji = "✅✅✅"
	case 4:
		itemEmoji = "✅✅✅✅"
	case 6:
		itemEmoji = "✅✅✅✅✅✅"
	}
	if itemEmoji != "" {
		logger.Printf("%s %d ITEM SETUP COMPLETE! Power increased!", itemEmoji, tier.NumItems)
	}

	return true
}

// CountModulesInstalled counts how many of a specific module type are installed
func CountModulesInstalled(state *State, itemID string) int {
	count := 0
	for _, module := range state.Ship.Modules {
		if module == itemID {
			count++
		}
	}
	return count
}

// CountModulesInCargo counts how many of a specific module type are in cargo
func CountModulesInCargo(state *State, itemID string) int {
	count := 0
	for _, item := range state.Ship.Cargo {
		if item.ItemID == itemID && item.Quantity > 0 {
			count += int(item.Quantity)
		}
	}
	return count
}

// IsOreOrResource returns true if item is ore or a resource (should be sold, not installed)
func IsOreOrResource(itemID string) bool {
	// Ores and resources to sell (not installable modules)
	oreAndResourcePrefixes := []string{
		"ore_",       // All ores (ore_iron, ore_copper, etc.)
		"gas_",       // Gases
		"crystal_",   // Crystals
		"salvage_",   // Salvage materials
		"scrap_",     // Scrap materials
		"refined_",   // Refined materials (refined_steel, refined_copper, etc.)
		"component_", // Crafted components (component_electronics, etc.)
	}

	for _, prefix := range oreAndResourcePrefixes {
		if len(itemID) >= len(prefix) && itemID[:len(prefix)] == prefix {
			return true
		}
	}

	return false
}

// TryInstallAndSellExtras attempts to install modules from cargo and sells any that can't be installed
// This is useful for all agent types that collect loot or buy equipment
func TryInstallAndSellExtras(client *Client, logger *log.Logger, ctx context.Context) {
	state := client.GetState()

	// Find all equipment items in cargo (not ores/resources)
	for _, item := range state.Ship.Cargo {
		// Skip ores and resources
		if IsOreOrResource(item.ItemID) {
			continue
		}

		// Get max slots for current ship
		maxSlots := GetShipClassMaxSlots(state.Ship.ClassID)

		// Special handling for equipment items - keep only what we can use
		installed := CountModulesInstalled(state, item.ItemID)
		inCargo := CountModulesInCargo(state, item.ItemID)
		total := installed + inCargo

		// If we have less than max, try to install
		if total < maxSlots && inCargo > 0 {
			for i := 0; i < int(item.Quantity) && (installed+i) < maxSlots; i++ {
				logger.Printf("🔧 Installing %s from cargo...", item.ItemID)
				if err := client.Install(ctx, item.ItemID); err != nil {
					logger.Printf("⚠️  Cannot install %s: %v", item.ItemID, err)
					break
				} else {
					logger.Printf("✅ Installed %s!", item.ItemID)
					installed++
					time.Sleep(10 * time.Second) // Wait between installs to respect game tick rate
				}
			}
		}

		// If we have MORE than max, sell the excess to free up cargo space
		if total > maxSlots && inCargo > 0 {
			excess := total - maxSlots
			logger.Printf("⚠️  Too many %s! Have %d, can only use %d - selling %d excess",
				item.ItemID, total, maxSlots, excess)
			if err := client.Sell(ctx, item.ItemID, float64(excess)); err != nil {
				logger.Printf("Failed to sell excess: %v", err)
			} else {
				logger.Printf("✅ Sold %d excess %s(s) - freed up cargo space!", excess, item.ItemID)
				time.Sleep(10 * time.Second) // Wait after selling to respect game tick rate
			}
			continue
		}

		// This is other equipment - try to install it
		if item.Quantity > 0 {
			logger.Printf("🔧 Attempting to install %s from cargo...", item.ItemID)
			if err := client.Install(ctx, item.ItemID); err != nil {
				// Installation failed - probably no slots, CPU, or power available
				logger.Printf("⚠️  Cannot install %s: %v - selling it", item.ItemID, err)
				time.Sleep(10 * time.Second) // Wait after failed install to respect game tick rate

				// Sell the item since we can't use it
				if err := client.Sell(ctx, item.ItemID, item.Quantity); err != nil {
					logger.Printf("Failed to sell %s: %v", item.ItemID, err)
				} else {
					logger.Printf("✅ Sold extra %s (%.0f units)", item.ItemID, item.Quantity)
				}
				time.Sleep(10 * time.Second) // Wait after selling to respect game tick rate
			} else {
				logger.Printf("✅ Installed %s successfully!", item.ItemID)
				time.Sleep(10 * time.Second) // Wait after install to respect game tick rate
			}
		}
	}
}

// LootWreck loots all items from a wreck
// Useful for fighters and pirates who defeat NPCs
func LootWreck(client *Client, logger *log.Logger, ctx context.Context, wreckID string) error {
	state := client.GetState()

	logger.Printf("💎 Looting wreck %s...", wreckID)

	// Check current cargo capacity
	cargoSpaceRemaining := state.Ship.CargoCapacity - state.Ship.CargoUsed

	// Try to loot each item in the wreck
	// TODO: Get wreck contents first, then selectively loot
	// For now, this is a placeholder that can be expanded later
	_ = cargoSpaceRemaining

	logger.Printf("✅ Wreck looted!")
	return nil
}

// SellAllLoot sells all loot items in cargo
// Useful for fighters and pirates after combat
func SellAllLoot(client *Client, logger *log.Logger, ctx context.Context) error {
	state := client.GetState()

	if len(state.Ship.Cargo) == 0 {
		logger.Printf("No cargo to sell")
		return nil
	}

	logger.Printf("💰 Selling all loot (%d items)...", len(state.Ship.Cargo))

	// List what we're selling
	for _, item := range state.Ship.Cargo {
		logger.Printf("   - %s x%.0f", item.ItemID, item.Quantity)
	}

	if err := client.SellAll(ctx); err != nil {
		logger.Printf("Sell error: %v", err)
		return err
	}

	// Wait longer for state update
	time.Sleep(5 * time.Second)
	state = client.GetState()
	logger.Printf("✅ Loot sold!")

	return nil
}

// ShouldUpgrade checks if it's time to check for upgrades based on credits
func ShouldUpgrade(credits float64, tiers []UpgradeTier, tier1Threshold float64) bool {
	// Check when approaching any upgrade threshold
	for _, tier := range tiers {
		if credits >= tier.Threshold {
			return true
		}
	}

	// Also check basic tier threshold
	if credits >= tier1Threshold {
		return true
	}

	return false
}
