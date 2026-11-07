package main

import "fmt"

// facilityHealth is the condition every facility row carries. Live rows have no
// "active" field, so a facility is working unless one of these says otherwise.
type facilityHealth struct {
	Damaged            bool  `json:"damaged"`
	RepairCompleteTick int64 `json:"repair_complete_tick"`
	UnderConstruction  bool  `json:"under_construction"`
	Dismantling        bool  `json:"dismantling"`
	PowerThrottled     bool  `json:"power_throttled"`
	MissedRentCycles   int   `json:"missed_rent_cycles"`
}

// statusOr names the facility's problem, or returns healthy when it has none.
// Missed rent is appended because a paused facility still owes what it missed.
func (h facilityHealth) statusOr(healthy string) string {
	status := healthy
	switch {
	case h.UnderConstruction:
		status = "building"
	case h.Dismantling:
		status = "dismantling"
	case h.Damaged:
		status = "damaged"
		if h.RepairCompleteTick > 0 {
			status = fmt.Sprintf("repairing (until tick %d)", h.RepairCompleteTick)
		}
	case h.PowerThrottled:
		status = "power-throttled"
	}
	if h.MissedRentCycles > 0 {
		status += fmt.Sprintf(" ⚠ %d missed cycle(s)", h.MissedRentCycles)
	}

	return status
}

// nameWithProblem appends a facility's problem to its name, for the production
// table, which has no status column.
func (h facilityHealth) nameWithProblem(name string) string {
	if problem := h.statusOr(""); problem != "" {
		return name + " ⚠ " + problem
	}

	return name
}

// ownedFacilityRow is one row of a `facility owned` response.
type ownedFacilityRow struct {
	facilityHealth
	FacilityID   string `json:"facility_id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	BaseName     string `json:"base_name"`
	RentPerCycle int    `json:"rent_per_cycle"`
	LaborPerRun  int    `json:"labor_per_run"`
	ArrearsOwed  int    `json:"arrears_owed"`
}

// statusText adds the facility's own arrears to its status.
func (f ownedFacilityRow) statusText() string {
	status := f.statusOr("active")
	if f.ArrearsOwed > 0 {
		status += fmt.Sprintf(" ⚠ owes %d", f.ArrearsOwed)
	}

	return status
}
