package serverapi

// Environmental hazards (v0.687.0). A POI may carry a hazard that damages
// ships: steady hazards dose every tick an undocked ship stays at the POI,
// mining hazards dose on each mining action. Shields absorb a dose first and
// resistance to the hazard's damage type reduces it; an emptied hull is a
// lost ship. Docked, offline and in-battle pilots are not dosed.

// Hazard types (POIHazard.Type).
const (
	HazardRadiation    = "radiation"    // thermal, steady
	HazardTidalShear   = "tidal_shear"  // kinetic, steady and on mining
	HazardChargedSeam  = "charged_seam" // em, on mining; lowers scan power
	HazardDebris       = "debris"       // kinetic, on mining
	HazardScarDust     = "scar_dust"    // steady; lowers scan power
	HazardIonStorm     = "ion_storm"    // lowers scan power
	HazardSeverityLow  = 1              // 1% of max hull per dose
	HazardSeverityHigh = 3              // 8% of max hull per dose
	HazardSeverityMax  = 4              // lethal: 25% of max hull per dose
)

// POIHazard is the hazard at a POI: its type and severity (1 low, 2 moderate,
// 3 high, 4 lethal).
type POIHazard struct {
	Type     string `json:"type"`
	Severity int    `json:"severity"`
}

// DosePct is the share of maximum hull (percent) one dose takes before shields
// and resistance: 1, 3, 8 or 25 for severity 1-4. Zero for an unknown severity.
func (h *POIHazard) DosePct() int {
	if h == nil {
		return 0
	}
	switch h.Severity {
	case 1:
		return 1
	case 2:
		return 3
	case 3:
		return 8
	case 4:
		return 25
	}
	return 0
}

// Steady reports whether the hazard doses every tick an undocked ship stays
// at the POI (as opposed to only on mining actions or only on scans).
func (h *POIHazard) Steady() bool {
	if h == nil {
		return false
	}
	switch h.Type {
	case HazardRadiation, HazardTidalShear, HazardScarDust:
		return true
	}
	return false
}

// DosesOnMining reports whether each mining action at the POI takes a dose.
func (h *POIHazard) DosesOnMining() bool {
	if h == nil {
		return false
	}
	switch h.Type {
	case HazardTidalShear, HazardChargedSeam, HazardDebris:
		return true
	}
	return false
}

// Damaging reports whether the hazard can hurt the hull at all (ion_storm
// only lowers scan power).
func (h *POIHazard) Damaging() bool {
	return h.Steady() || h.DosesOnMining()
}

// HazardWarning is the hazard_warning push: a hazard at poi_id has just
// dosed the ship, leaving Hull of MaxHull. The ship is lost if its hull
// reaches 0 there.
type HazardWarning struct {
	POIID   string     `json:"poi_id"`
	POIName string     `json:"poi_name"`
	Hazard  *POIHazard `json:"hazard"`
	Hull    int        `json:"hull"`
	MaxHull int        `json:"max_hull"`
	DosePct int        `json:"dose_pct"`
	Message string     `json:"message"`
}
