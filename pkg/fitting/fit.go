package fitting

import (
	"fmt"
	"sort"
)

// skillWarnings renders a module's required_skills as informational strings,
// sorted by skill name for deterministic output.
func skillWarnings(m Module) []string {
	if len(m.RequiredSkills) == 0 {
		return nil
	}
	names := make([]string, 0, len(m.RequiredSkills))
	for k := range m.RequiredSkills {
		names = append(names, k)
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, fmt.Sprintf("requires %s %d", n, m.RequiredSkills[n]))
	}
	return out
}

// MaxFit reports how many copies of module m fit on ship s at the given
// Engineering level. It adds copies one at a time so that capacity-adding
// modules (cpu_bonus / power_bonus) correctly raise the budget as they are
// fitted. Iteration is bounded by the relevant slot count.
func MaxFit(s Ship, m Module, eng Engineering) FitResult {
	slotCap := slotCapacity(s, m.Slot)
	effCPU := effUsage(m.CPUUsage, eng.Level, eng.CPUEffPerLevel)
	effPower := effUsage(m.PowerUsage, eng.Level, eng.PowerEffPerLevel)

	count := 0
	binding := ""
	for {
		next := count + 1
		if next > slotCap {
			binding = slotLabel(m.Slot)
			break
		}
		cpuUsed := next * effCPU
		powerUsed := next * effPower
		cpuCap := s.CPUCapacity + next*m.CPUBonus
		powerCap := s.PowerCapacity + next*m.PowerBonus
		if cpuUsed > cpuCap {
			binding = "CPU"
			break
		}
		if powerUsed > powerCap {
			binding = "power"
			break
		}
		count = next
	}

	return FitResult{
		Fits:              count >= 1,
		MaxCount:          count,
		SlotType:          m.Slot,
		SlotsUsed:         count,
		SlotsAvail:        slotCap,
		CPUUsed:           count * effCPU,
		CPUAvail:          s.CPUCapacity + count*m.CPUBonus,
		PowerUsed:         count * effPower,
		PowerAvail:        s.PowerCapacity + count*m.PowerBonus,
		BindingConstraint: binding,
		SkillWarnings:     skillWarnings(m),
	}
}

// ShipFit pairs a ship with its MaxFit result for the queried module.
type ShipFit struct {
	Ship   Ship
	Result FitResult
}

// ShipsThatFit returns every ship that can fit at least `count` copies of module
// m, sorted by tier ascending then name. Each entry carries the per-ship MaxFit
// result.
func ShipsThatFit(ships []Ship, m Module, count int, eng Engineering) []ShipFit {
	var out []ShipFit
	for _, s := range ships {
		r := MaxFit(s, m, eng)
		if r.MaxCount >= count {
			out = append(out, ShipFit{Ship: s, Result: r})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ship.Tier != out[j].Ship.Tier {
			return out[i].Ship.Tier < out[j].Ship.Tier
		}
		return out[i].Ship.Name < out[j].Ship.Name
	})
	return out
}

// CheckFit reports whether an arbitrary loadout fits on ship s at the given
// Engineering level. It checks slot counts per type plus aggregate CPU and
// power, accounting for capacity-adding modules. The first violated constraint
// is reported in BindingConstraint.
func CheckFit(s Ship, loadout []ModuleCount, eng Engineering) FitResult {
	slotsUsed := map[string]int{}
	cpuUsed, powerUsed := 0, 0
	cpuCap, powerCap := s.CPUCapacity, s.PowerCapacity
	var warnings []string
	seenWarning := map[string]bool{}

	for _, mc := range loadout {
		slotsUsed[mc.Module.Slot] += mc.Count
		cpuUsed += mc.Count * effUsage(mc.Module.CPUUsage, eng.Level, eng.CPUEffPerLevel)
		powerUsed += mc.Count * effUsage(mc.Module.PowerUsage, eng.Level, eng.PowerEffPerLevel)
		cpuCap += mc.Count * mc.Module.CPUBonus
		powerCap += mc.Count * mc.Module.PowerBonus
		for _, w := range skillWarnings(mc.Module) {
			if !seenWarning[w] {
				seenWarning[w] = true
				warnings = append(warnings, w)
			}
		}
	}

	binding := ""
	fits := true
	// Slots first (deterministic order: weapon, defense, utility).
	for _, slot := range []string{"weapon", "defense", "utility"} {
		if slotsUsed[slot] > slotCapacity(s, slot) {
			binding = slotLabel(slot)
			fits = false
			break
		}
	}
	if fits && cpuUsed > cpuCap {
		binding = "CPU"
		fits = false
	}
	if fits && powerUsed > powerCap {
		binding = "power"
		fits = false
	}

	slotType := "mixed"
	totalSlots := 0
	if len(slotsUsed) == 1 {
		for k := range slotsUsed {
			slotType = k
			totalSlots = slotsUsed[k]
		}
	}

	res := FitResult{
		Fits:              fits,
		SlotType:          slotType,
		CPUUsed:           cpuUsed,
		CPUAvail:          cpuCap,
		PowerUsed:         powerUsed,
		PowerAvail:        powerCap,
		BindingConstraint: binding,
		SkillWarnings:     warnings,
	}
	if slotType != "mixed" {
		res.SlotsUsed = totalSlots
		res.SlotsAvail = slotCapacity(s, slotType)
	}
	return res
}
