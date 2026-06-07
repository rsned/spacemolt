// Package fitting computes ship-module fitting against the game's catalog data.
// It answers two questions offline: how many of a module fit on a hull, and
// which hulls can fit a given count of a module. Slots, CPU, power, capacity-
// adding modules, and the Engineering skill are all accounted for.
package fitting

import "math"

// Ship is the fitting-relevant subset of a catalog hull.
type Ship struct {
	ID             string
	Name           string
	CPUCapacity    int
	PowerCapacity  int
	WeaponSlots    int
	DefenseSlots   int
	UtilitySlots   int
	Tier           int
	DefaultModules []string // parsed but ignored by the fit math (bare-hull assumption)
}

// Module is the fitting-relevant subset of a catalog module.
type Module struct {
	ID             string
	Name           string
	Type           string         // "weapon" | "defense" | "mining" | "utility"
	Slot           string         // "weapon" | "defense" | "utility"
	CPUUsage       int
	PowerUsage     int
	CPUBonus       int            // adds to CPU capacity (e.g. tow rig)
	PowerBonus     int            // adds to power capacity (e.g. reactor)
	RequiredSkills map[string]int // informational only
}

// Engineering carries the assumed Engineering skill level and the data-driven
// per-level efficiency values read from the skills catalog (both 1 by default).
type Engineering struct {
	Level            int
	CPUEffPerLevel   float64
	PowerEffPerLevel float64
}

// DefaultEngineering returns an Engineering at the given level using the default
// 1%-per-level efficiency for both CPU and power. Used when no catalog skill
// data overrides it.
func DefaultEngineering(level int) Engineering {
	return Engineering{Level: level, CPUEffPerLevel: 1, PowerEffPerLevel: 1}
}

// ModuleCount pairs a module with how many copies are being fitted.
type ModuleCount struct {
	Module Module
	Count  int
}

// FitResult reports the outcome of a fit calculation.
type FitResult struct {
	Fits              bool
	MaxCount          int    // populated by MaxFit: how many copies fit
	SlotType          string // module slot type ("" or "mixed" for multi-slot loadouts)
	SlotsUsed         int
	SlotsAvail        int
	CPUUsed           int
	CPUAvail          int
	PowerUsed         int
	PowerAvail        int
	BindingConstraint string // "weapon slots" | "defense slots" | "utility slots" | "CPU" | "power" | ""
	SkillWarnings     []string
}

// effUsage applies the Engineering reduction to a base usage value and rounds up.
// effPerLevel is the per-level efficiency (percentage points) from the skill data.
func effUsage(base, level int, effPerLevel float64) int {
	reduced := float64(base) * (1 - 0.01*float64(level)*effPerLevel)
	if reduced < 0 {
		reduced = 0
	}
	return int(math.Ceil(reduced))
}

// slotCapacity returns the number of slots of the given type on the ship.
func slotCapacity(s Ship, slot string) int {
	switch slot {
	case "weapon":
		return s.WeaponSlots
	case "defense":
		return s.DefenseSlots
	case "utility":
		return s.UtilitySlots
	default:
		return 0
	}
}

// slotLabel returns the human label for a slot type used in BindingConstraint.
func slotLabel(slot string) string {
	switch slot {
	case "weapon":
		return "weapon slots"
	case "defense":
		return "defense slots"
	case "utility":
		return "utility slots"
	default:
		return slot + " slots"
	}
}
