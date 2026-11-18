package sweep

import (
	"fmt"
	"slices"
)

// The frontier-run fit: a pathfinder drive and two Survey Scanner IIs (survey
// power 90), all three in utility slots.
const (
	FitDrive       = "pathfinder_drive"
	FitScanner     = "survey_scanner_ii"
	FitScannerWant = 2
)

// Module slot kinds, as the item catalog names them.
const (
	SlotUtility = "utility"
	SlotWeapon  = "weapon"
	SlotDefense = "defense"
)

// FitModule is one module fitted to a ship.
type FitModule struct {
	ID, TypeID, Name, Slot string
	CPU, Power             int
}

// FitPlan is how to bring a ship to the frontier-run fit.
type FitPlan struct {
	Remove []FitModule // in removal order
	Add    []string    // type ids to fit
	// The fit after the plan, against the hull's capacity.
	CPUAfter, CPUCap, PowerAfter, PowerCap int
	OK                                     bool
	Why                                    string // when !OK
}

// PlanFit keeps the pathfinder drive, brings the ship to two Survey Scanner
// IIs, and removes what has to go to make room: other utility modules first
// (for the slots, then for CPU and power), then weapons, then defence
// modules, the hungriest of each kind first. Nothing is removed that does not
// have to be. scannerCPU and scannerPower are what one scanner will cost this
// pilot.
func PlanFit(fitted []FitModule, utilitySlots, cpuCap, powerCap, scannerCPU, scannerPower int) FitPlan {
	p := FitPlan{CPUCap: cpuCap, PowerCap: powerCap}
	hasDrive, scanners, utilityUsed := false, 0, 0
	var removable []FitModule
	for _, m := range fitted {
		p.CPUAfter += m.CPU
		p.PowerAfter += m.Power
		if m.Slot == SlotUtility {
			utilityUsed++
		}
		switch m.TypeID {
		case FitDrive:
			hasDrive = true
		case FitScanner:
			scanners++
		default:
			removable = append(removable, m)
		}
	}
	if !hasDrive {
		p.Why = "no " + FitDrive + " fitted"
		return p
	}
	need := max(FitScannerWant-scanners, 0)
	for range need {
		p.Add = append(p.Add, FitScanner)
	}
	p.CPUAfter += need * scannerCPU
	p.PowerAfter += need * scannerPower
	utilityUsed += need

	// Removal order: utility, weapon, defence; within a kind, most CPU first.
	rank := map[string]int{SlotUtility: 0, SlotWeapon: 1, SlotDefense: 2}
	slices.SortStableFunc(removable, func(a, b FitModule) int {
		if ra, rb := slotRank(rank, a.Slot), slotRank(rank, b.Slot); ra != rb {
			return ra - rb
		}
		return b.CPU - a.CPU
	})
	for _, m := range removable {
		overSlots := utilityUsed > utilitySlots && m.Slot == SlotUtility
		overBudget := p.CPUAfter > cpuCap || p.PowerAfter > powerCap
		if !overSlots && !overBudget {
			continue
		}
		p.Remove = append(p.Remove, m)
		p.CPUAfter -= m.CPU
		p.PowerAfter -= m.Power
		if m.Slot == SlotUtility {
			utilityUsed--
		}
	}

	switch {
	case utilityUsed > utilitySlots:
		p.Why = fmt.Sprintf("needs %d utility slots, the hull has %d", utilityUsed, utilitySlots)
	case p.CPUAfter > cpuCap:
		p.Why = fmt.Sprintf("needs %d CPU, the hull has %d", p.CPUAfter, cpuCap)
	case p.PowerAfter > powerCap:
		p.Why = fmt.Sprintf("needs %d power, the hull has %d", p.PowerAfter, powerCap)
	default:
		p.OK = true
	}
	return p
}

// slotRank orders a slot kind for removal; an unknown kind goes last.
func slotRank(rank map[string]int, slot string) int {
	if r, ok := rank[slot]; ok {
		return r
	}
	return len(rank)
}
