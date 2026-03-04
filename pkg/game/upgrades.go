package game

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
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
	ItemID        string  // Item ID to buy (e.g., "mining_laser_i", "pulse_laser_i")
	LogEmoji      string  // Emoji for logging
	Capacity      string  // Cargo/capability description for logging
	SuccessMsg    string  // Success message template
}

// UpgradeProgression defines a career path with upgrade tiers
type UpgradeProgression struct {
	CareerName string        // e.g., "Mining", "Combat", "Trading", "Exploration"
	Tiers      []UpgradeTier // Ordered list of upgrade tiers
}

// ShipDef is a minimal ship definition used for progression building.
// This avoids importing pkg/knowledge into pkg/game.
type ShipDef struct {
	ID            string
	Name          string
	Class         string
	Price         int
	WeaponSlots   int
	DefenseSlots  int
	UtilitySlots  int
	CargoCapacity int
}

// RoleCategories maps agent roles to their upgrade-eligible ship classes.
// Ships are queried from the knowledge DB filtered by these classes.
var RoleCategories = map[string][]string{
	"miner":    {"Mining", "Mining Capital"},
	"fighter":  {"Fighter", "Heavy Fighter", "Interceptor", "Cruiser"},
	"trader":   {"Freighter", "Bulk Hauler", "Armed Freighter", "Armored Transport"},
	"explorer": {"Explorer", "Expedition", "Scout", "Armed Explorer"},
	"pirate":   {"Raider", "Boarding Craft", "Assault", "Assault Cruiser"},
	"salvager": {"Salvager"},
	"engineer": {"Repair", "Construction", "Field Repair"},
	"craftsman": {"Refinery", "Gas Refinery", "Ice Refinery"},
}

// combatClasses identifies ship classes where weapon slots are the primary
// equipment metric (rather than utility slots).
var combatClasses = map[string]bool{
	"Fighter":         true,
	"Heavy Fighter":   true,
	"Interceptor":     true,
	"Raider":          true,
	"Assault":         true,
	"Assault Cruiser": true,
	"Assault Titan":   true,
	"Cruiser":         true,
	"Duelist":         true,
	"Boarding Craft":  true,
	"Battlecruiser":   true,
	"Dreadnought":     true,
	"Bombardment":     true,
	"Void Assault":    true,
	"Siege Titan":     true,
	"Convoy Escort":   true,
}

// roleEmojis maps agent roles to display emojis for upgrade log messages.
var roleEmojis = map[string]string{
	"miner":    "⛏️",
	"fighter":  "⚔️",
	"trader":   "💰",
	"explorer": "🔭",
	"pirate":   "🏴‍☠️",
	"salvager": "💎",
	"engineer": "🔧",
	"craftsman": "⚒️",
}

// roleEquipment maps agent roles to their default equipment item IDs.
var roleEquipment = map[string]string{
	"miner":    "mining_laser_i",
	"fighter":  "pulse_laser_i",
	"trader":   "pulse_laser_i",
	"explorer": "ship_scanner_i",
	"pirate":   "pulse_laser_i",
	"salvager": "basic_tow_rig",
	"engineer": "remote_armor_repairer_i",
	"craftsman": "cargo_expander_i",
}

// roleCareerNames maps agent roles to display career names.
var roleCareerNames = map[string]string{
	"miner":    "Mining",
	"fighter":  "Combat",
	"trader":   "Trading",
	"explorer": "Exploration",
	"pirate":   "Piracy",
	"salvager": "Salvage",
	"engineer": "Engineering",
	"craftsman": "Crafting",
}

// ShipMaxSlots returns the relevant max equipment slot count for a ship.
// Combat-oriented classes use weapon slots; others use utility slots.
func ShipMaxSlots(ship ShipDef) int {
	if combatClasses[ship.Class] {
		return ship.WeaponSlots
	}
	return ship.UtilitySlots
}

// FilterShipsByEmpire returns only ships whose ID starts with the given empire prefix.
// Empire values: "solarian", "crimson", "nebula", "outerrim", "voidborn".
func FilterShipsByEmpire(ships []ShipDef, empire string) []ShipDef {
	prefix := empire + "_"
	var result []ShipDef
	for _, s := range ships {
		if strings.HasPrefix(s.ID, prefix) {
			result = append(result, s)
		}
	}
	return result
}

// DefaultEquipment returns the item ID and count to install after buying a ship.
func DefaultEquipment(ship ShipDef, role string) (itemID string, count int) {
	itemID = roleEquipment[role]
	if itemID == "" {
		return "", 0
	}
	if combatClasses[ship.Class] {
		count = ship.WeaponSlots
	} else {
		count = ship.UtilitySlots
	}
	return itemID, count
}

// BuildProgression generates an UpgradeProgression from ship definitions.
// It sorts ships by price, finds the agent's current position, and chains
// them into upgrade tiers.
//
// Parameters:
//   - ships: all eligible ships (pre-filtered by role categories + empire)
//   - currentShipID: the agent's current ship class ID
//   - role: agent role (for equipment selection and career name)
//
// Returns nil if no upgrades are available.
func BuildProgression(ships []ShipDef, currentShipID string, role string) *UpgradeProgression {
	if len(ships) == 0 {
		return nil
	}

	// Sort by price ascending, then by name for stable ordering
	sort.Slice(ships, func(i, j int) bool {
		if ships[i].Price != ships[j].Price {
			return ships[i].Price < ships[j].Price
		}
		return ships[i].ID < ships[j].ID
	})

	// Deduplicate by price tier: keep only the best ship at each price point.
	// When multiple ships share the same price, keep the one with the most
	// relevant slots (weapon slots for combat, utility slots otherwise).
	deduped := deduplicateByPrice(ships)

	// Find current ship index (-1 if not in the list, e.g. starter ship)
	currentIdx := -1
	for i, s := range deduped {
		if s.ID == currentShipID {
			currentIdx = i
			break
		}
	}

	// Build tiers for ships after the current one
	emoji := roleEmojis[role]
	if emoji == "" {
		emoji = "🚀"
	}
	careerName := roleCareerNames[role]
	if careerName == "" {
		// Capitalize first letter as fallback
		careerName = strings.ToUpper(role[:1]) + role[1:]
	}

	var tiers []UpgradeTier
	for i := range deduped {
		if i <= currentIdx {
			continue
		}
		// Skip free ships (price 0) unless it's the very first upgrade
		if deduped[i].Price == 0 && i > 0 {
			continue
		}

		fromShip := currentShipID
		if len(tiers) > 0 {
			fromShip = tiers[len(tiers)-1].ToShipClass
		}

		itemID, numItems := DefaultEquipment(deduped[i], role)

		tier := UpgradeTier{
			Name:          deduped[i].Name,
			Threshold:     float64(deduped[i].Price),
			FromShipClass: fromShip,
			ToShipClass:   deduped[i].ID,
			NumItems:      numItems,
			ItemID:        itemID,
			LogEmoji:      emoji,
			Capacity:      formatCapacity(deduped[i]),
			SuccessMsg:    strings.ToUpper(deduped[i].Name) + "!",
		}
		tiers = append(tiers, tier)
	}

	if len(tiers) == 0 {
		return nil
	}

	return &UpgradeProgression{
		CareerName: careerName,
		Tiers:      tiers,
	}
}

// deduplicateByPrice keeps only the best ship at each price point.
func deduplicateByPrice(ships []ShipDef) []ShipDef {
	if len(ships) == 0 {
		return nil
	}

	var result []ShipDef
	lastPrice := -1
	for _, s := range ships {
		if s.Price == lastPrice && len(result) > 0 {
			// Same price as previous - keep the one with more relevant slots
			prev := &result[len(result)-1]
			if betterShip(s, *prev) {
				*prev = s
			}
			continue
		}
		result = append(result, s)
		lastPrice = s.Price
	}
	return result
}

// betterShip returns true if a is better than b.
func betterShip(a, b ShipDef) bool {
	isCombat := combatClasses[a.Class] || combatClasses[b.Class]
	if isCombat {
		if a.WeaponSlots != b.WeaponSlots {
			return a.WeaponSlots > b.WeaponSlots
		}
	} else {
		if a.UtilitySlots != b.UtilitySlots {
			return a.UtilitySlots > b.UtilitySlots
		}
	}
	// Tiebreak: more cargo
	return a.CargoCapacity > b.CargoCapacity
}

// formatCapacity builds a human-readable capacity description for a ship.
func formatCapacity(ship ShipDef) string {
	parts := []string{fmt.Sprintf("%d cargo", ship.CargoCapacity)}
	if ship.WeaponSlots > 0 {
		parts = append(parts, fmt.Sprintf("%d weapon slots", ship.WeaponSlots))
	}
	if ship.UtilitySlots > 0 {
		parts = append(parts, fmt.Sprintf("%d utility slots", ship.UtilitySlots))
	}
	return strings.Join(parts, ", ")
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
		"gas_",       // Gases
		"crystal_",   // Crystals
		"salvage_",   // Salvage materials
		"scrap_",     // Scrap materials
		"component_", // Crafted components (component_electronics, etc.)
	}

	// New naming convention uses suffixes for ores and ice
	oreAndResourceSuffixes := []string{
		"_ore", // All ores (iron_ore, copper_ore, etc.)
		"_ice", // All ice resources (water_ice, deuterium_ice, etc.)
	}

	for _, prefix := range oreAndResourcePrefixes {
		if len(itemID) >= len(prefix) && itemID[:len(prefix)] == prefix {
			return true
		}
	}

	for _, suffix := range oreAndResourceSuffixes {
		if len(itemID) >= len(suffix) && itemID[len(itemID)-len(suffix):] == suffix {
			return true
		}
	}

	return false
}

// TryInstallAndSellExtras attempts to install modules from cargo and sells any that can't be installed.
// maxSlots is the relevant slot count for the current ship (use ShipMaxSlots to compute it).
// This is useful for all agent types that collect loot or buy equipment.
func TryInstallAndSellExtras(client *Client, logger *log.Logger, ctx context.Context, maxSlots int) {
	state := client.GetState()

	// Find all equipment items in cargo (not ores/resources)
	for _, item := range state.Ship.Cargo {
		// Skip ores and resources
		if IsOreOrResource(item.ItemID) {
			continue
		}

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
				}
				logger.Printf("✅ Installed %s!", item.ItemID)
				installed++
				time.Sleep(10 * time.Second) // Wait between installs to respect game tick rate
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
