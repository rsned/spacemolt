package worker

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
)

// An advanced_drone_bay takes one utility slot, cpu 12 and power 15.
const (
	bayCPU   = 12
	bayPower = 15
)

// FitReport is what FitCheck learned about installing one bay on this hull.
type FitReport struct {
	Modules    []installedModule
	CPUFree    int
	PowerFree  int
	BaysFitted int
	// MustStrip is the fewest modules whose removal lets a bay in.
	MustStrip []installedModule
	Fits      bool
}

// shipBudget is the fitting budget from a get_ship reply.
type shipBudget struct {
	CPUUsed       float64 `json:"cpu_used"`
	CPUCapacity   float64 `json:"cpu_capacity"`
	PowerUsed     float64 `json:"power_used"`
	PowerCapacity float64 `json:"power_capacity"`
	UtilitySlots  int     `json:"utility_slots"`
}

// FitCheck reports what fitting one bay would take, and changes nothing. No
// fitted-module data is captured anywhere else: the assets ledger holds counts
// and the knowledge base's ship_modules table is empty.
func (d *WorkerDispatch) FitCheck(ctx context.Context, bayItem string) (FitReport, error) {
	var rep FitReport
	if bayItem == "" {
		bayItem = "advanced_drone_bay"
	}
	if err := d.Client.GetShip(ctx); err != nil {
		return rep, fmt.Errorf("fit_check: get_ship: %w", err)
	}
	raw := d.Client.GetRawJSON("ship")
	mods, err := shipModules(raw)
	if err != nil {
		return rep, fmt.Errorf("fit_check: %w", err)
	}
	var resp struct {
		Ship shipBudget `json:"ship"`
	}
	if err = json.Unmarshal(raw, &resp); err != nil {
		return rep, fmt.Errorf("fit_check: decode ship: %w", err)
	}

	rep = planBayFit(mods, resp.Ship, bayItem)
	for _, m := range mods {
		fmt.Fprintf(d.Out, "fit_check: module %s slot=%s cpu=%d power=%d\n", m.TypeID, m.Slot, m.CPUUsage, m.PowerUsage) //nolint:errcheck
	}
	strip := make([]string, 0, len(rep.MustStrip))
	for _, m := range rep.MustStrip {
		strip = append(strip, m.TypeID)
	}
	fmt.Fprintf(d.Out, "fit_check: fits=%t bays=%d cpu_free=%d power_free=%d must_strip=%v\n", //nolint:errcheck
		rep.Fits, rep.BaysFitted, rep.CPUFree, rep.PowerFree, strip)

	return rep, nil
}

// planBayFit picks the strip list: a utility module first if every slot is
// taken, then the largest CPU users until cpu and power both clear the bay.
func planBayFit(mods []installedModule, ship shipBudget, bayItem string) FitReport {
	rep := FitReport{
		Modules:   mods,
		CPUFree:   int(ship.CPUCapacity - ship.CPUUsed),
		PowerFree: int(ship.PowerCapacity - ship.PowerUsed),
	}
	slotsFree := ship.UtilitySlots
	candidates := make([]installedModule, 0, len(mods))
	for _, m := range mods {
		if m.Slot == "utility" {
			slotsFree--
		}
		if m.TypeID == bayItem {
			rep.BaysFitted++
			continue
		}
		candidates = append(candidates, m)
	}
	if rep.BaysFitted > 0 {
		rep.Fits = true
		return rep
	}
	if ship.UtilitySlots < 1 || ship.CPUCapacity < bayCPU || ship.PowerCapacity < bayPower {
		return rep
	}

	slices.SortStableFunc(candidates, func(a, b installedModule) int { return cmp.Compare(b.CPUUsage, a.CPUUsage) })
	cpu, power := rep.CPUFree, rep.PowerFree
	take := func(m installedModule) {
		rep.MustStrip = append(rep.MustStrip, m)
		cpu += m.CPUUsage
		power += m.PowerUsage
		if m.Slot == "utility" {
			slotsFree++
		}
	}
	if slotsFree < 1 {
		i := slices.IndexFunc(candidates, func(m installedModule) bool { return m.Slot == "utility" })
		if i < 0 {
			return rep
		}
		take(candidates[i])
		candidates = slices.Delete(candidates, i, i+1)
	}
	for _, m := range candidates {
		if cpu >= bayCPU && power >= bayPower {
			break
		}
		take(m)
	}
	rep.Fits = cpu >= bayCPU && power >= bayPower

	return rep
}
