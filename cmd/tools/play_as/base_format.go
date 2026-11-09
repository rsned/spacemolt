package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/assets"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/stationstate"
)

// baseBarWidth is the cell count of the hull/shield/power/fuel bars.
const baseBarWidth = 10

// baseFacilityRaw is the cached `facility list` reply the get_base view may
// use for its production and maintenance sections; nil for any other command.
func baseFacilityRaw(client game.GameClient, command string) []byte {
	switch strings.ToLower(command) {
	case "base", "get_base":
		return client.GetRawJSON("facility")
	}
	return nil
}

// severityColor maps a station severity to a statusline colour name.
func severityColor(s stationstate.Severity) string {
	switch s {
	case stationstate.OK:
		return "green"
	case stationstate.Strained:
		return "yellow"
	case stationstate.Critical:
		return "red"
	}
	return "gray"
}

// sevBar renders a fixed-width bar for pct, coloured by severity.
func sevBar(pct float64, s stationstate.Severity) string {
	filled := min(max(int(pct/100*baseBarWidth+0.5), 0), baseBarWidth)
	return colorize("["+strings.Repeat("█", filled)+strings.Repeat("░", baseBarWidth-filled)+"]", severityColor(s))
}

// barOf renders the named bar, or an empty grey track when the station has no
// denominator for it (the key is absent from bars).
func barOf(bars map[string]float64, key string, s stationstate.Severity) string {
	v, ok := bars[key]
	if !ok {
		return colorize("["+strings.Repeat("░", baseBarWidth)+"]", severityColor(stationstate.NoData))
	}
	return sevBar(v, s)
}

// barPctText is the bar's percentage text, "—" when absent.
func barPctText(bars map[string]float64, key string) string {
	if v, ok := bars[key]; ok {
		return fmt.Sprintf("%.0f%%", v)
	}
	return "—"
}

// formatBase renders a get_base reply as a sectioned station summary. The
// facility list (a cached `facility list` reply, possibly stale or nil) is used
// only when its base_id matches the station. It returns "" when the reply is
// not a base, which leaves the plain JSON view.
func formatBase(raw, facilityRaw []byte) string {
	snap, ok := assets.BaseSnapshotFrom(unwrapActionResult(raw))
	if !ok {
		return ""
	}
	var named struct {
		Base struct {
			Name string `json:"name"`
		} `json:"base"`
	}
	_ = json.Unmarshal(unwrapActionResult(raw), &named)
	name := named.Base.Name
	if name == "" {
		name = snap.BaseID
	}

	reading := assets.StationReading{Base: snap}
	if id, rows, ok := assets.FacilityRowsFrom(facilityRaw); ok && id == snap.BaseID {
		reading.Facilities = rows
	}
	now := time.Now()
	reading.CapturedAt = now
	st := stationstate.Evaluate(reading, true, now)

	zones := make(map[stationstate.Zone]stationstate.ZoneState, len(st.Zones))
	for _, z := range st.Zones {
		zones[z.Zone] = z
	}
	// why appends the zone's reasons that start with prefix as " ← reason".
	why := func(z stationstate.Zone, prefix string) string {
		var out []string
		for _, r := range zones[z].Reasons {
			if prefix == "" || strings.HasPrefix(r, prefix) {
				out = append(out, r)
			}
		}
		if len(out) == 0 {
			return ""
		}
		return colorize("  ← "+strings.Join(out, "; "), "yellow")
	}
	subSev := func(z stationstate.Zone, prefix string) stationstate.Severity {
		if prefix != "" && why(z, prefix) == "" {
			return stationstate.OK
		}
		return zones[z].Severity
	}

	var b strings.Builder
	cond := strings.ToUpper(snap.Condition)
	if cond == "" {
		cond = "UNKNOWN"
	}
	fmt.Fprintf(&b, "%s — %s (satisfaction %d%%)        %s\n", name, cond, snap.SatisfactionPct,
		colorize(st.Word, severityColor(st.Overall)))

	def := zones[stationstate.Defense]
	fmt.Fprintf(&b, "  Hull      %s %s / %s   armor %d%s\n", barOf(st.Bars, "hull", subSev(stationstate.Defense, "hull")),
		commaInt(snap.Hull), commaInt(snap.MaxHull), snap.Armor, why(stationstate.Defense, "hull"))
	shell := "shells"
	if st.ShieldShells == 1 {
		shell = "shell"
	}
	fmt.Fprintf(&b, "  Shield    %s %s / %s   %d %s%s\n", barOf(st.Bars, "shield", subSev(stationstate.Defense, "shield")),
		commaInt(snap.Shield), commaInt(snap.MaxShield), st.ShieldShells, shell, why(stationstate.Defense, "shield"))
	defLine := fmt.Sprintf("%d dps · reach %d", snap.WeaponDPS, snap.WeaponReach)
	if len(def.Facilities) > 0 {
		parts := make([]string, len(def.Facilities))
		for i, f := range def.Facilities {
			parts[i] = fmt.Sprintf("%s L%d", f.Type, f.Level)
		}
		defLine += " · " + strings.Join(parts, ", ")
	}
	fmt.Fprintf(&b, "  Defense   %s\n", defLine)

	battery := "none"
	if snap.BatteryCapacity > 0 {
		battery = fmt.Sprintf("%s/%s", commaInt(snap.BatteryStored), commaInt(snap.BatteryCapacity))
	}
	fmt.Fprintf(&b, "  Power     %s draw %s / %s · battery %s%s\n", barOf(st.Bars, "power", zones[stationstate.Core].Severity),
		commaInt(snap.PowerDraw), commaInt(snap.PowerSupply), battery, why(stationstate.Core, ""))
	fmt.Fprintf(&b, "  Life      %s / %s (%s)%s\n", commaInt(snap.LifeSupportSupply), commaInt(snap.LifeSupportDemand),
		barPctText(st.Bars, "life"), why(stationstate.Life, ""))
	fmt.Fprintf(&b, "  Fuel      %s %s / %s (%s)%s\n", barOf(st.Bars, "fuel", zones[stationstate.Fuel].Severity),
		commaInt(snap.FuelReserve), commaInt(snap.FuelCapacity), barPctText(st.Bars, "fuel"), why(stationstate.Fuel, ""))

	var tour []string
	if snap.TourismRevenue > 0 {
		tour = append(tour, commaInt(snap.TourismRevenue)+" cr lifetime")
	}
	if snap.DiningPoints > 0 {
		tour = append(tour, fmt.Sprintf("dining %d", snap.DiningPoints))
	}
	if snap.LeisurePoints > 0 {
		tour = append(tour, fmt.Sprintf("leisure %d", snap.LeisurePoints))
	}
	if len(tour) == 0 {
		tour = []string{"none"}
	}
	fmt.Fprintf(&b, "  Tourism   %s\n", strings.Join(tour, " · "))

	for _, f := range zones[stationstate.Production].Facilities {
		if f.BacklogTicks > 0 {
			fmt.Fprintf(&b, "  Production  %s: %d queued runs, %s ticks backlog\n", f.Name, f.QueuedRuns, commaInt(f.BacklogTicks))
		}
	}
	for _, f := range reading.Facilities {
		if f.MaintenanceSatisfied != nil && !*f.MaintenanceSatisfied {
			fmt.Fprintf(&b, "  Maintenance %s L%d: unmet\n", f.Type, f.Level)
		}
	}
	return b.String()
}
