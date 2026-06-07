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
