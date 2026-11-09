// Package stationstate turns a station reading into per-zone severities. It is
// the single home of the alert thresholds: the play_as formatter and the
// console server both call Evaluate, and the browser only renders the result.
package stationstate

import (
	"fmt"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/assets"
)

// Severity ranks how worried to be about a zone.
type Severity int

// Severities in increasing order of concern; NoData means no reading.
const (
	OK Severity = iota
	Strained
	Critical
	NoData
)

// String is the lowercase severity name.
func (s Severity) String() string {
	switch s {
	case OK:
		return "ok"
	case Strained:
		return "strained"
	case Critical:
		return "critical"
	case NoData:
		return "nodata"
	}
	return "unknown"
}

// MarshalText makes Severity serialize as its string in JSON.
func (s Severity) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// Thresholds. These are the only place alert levels are defined.
const (
	powerStrainedPct, powerCriticalPct   = 70.0, 90.0
	lifeStrainedPct, lifeCriticalPct     = 110.0, 100.0
	fuelStrainedPct, fuelCriticalPct     = 40.0, 15.0
	shieldStrainedPct, shieldCriticalPct = 60.0, 25.0
	hullStrainedPct, hullCriticalPct     = 75.0, 40.0
	staleAfter                           = 2 * time.Hour
)

// ZoneState is the evaluated state of one zone.
type ZoneState struct {
	Zone       Zone
	Label      string
	Severity   Severity
	Brief      string
	Reasons    []string
	Facilities []assets.FacilityRow
}

// Status is the evaluated state of the whole station.
type Status struct {
	Overall      Severity
	Word         string
	Zones        []ZoneState
	ShieldShells int
	Bars         map[string]float64
	CapturedAt   time.Time
	Stale        bool
}

func (z *ZoneState) raise(s Severity, format string, args ...any) {
	if s > z.Severity {
		z.Severity = s
	}
	z.Reasons = append(z.Reasons, fmt.Sprintf(format, args...))
}

// pct is num/den as a percentage, 0 when den is not positive.
func pct(num, den int64) float64 {
	if den <= 0 {
		return 0
	}
	return float64(num) / float64(den) * 100
}

// Evaluate computes the station status. ok false means no reading exists.
func Evaluate(r assets.StationReading, ok bool, now time.Time) Status {
	if !ok {
		st := Status{Overall: NoData, Word: "NO DATA", Bars: map[string]float64{}}
		for _, z := range Zones {
			st.Zones = append(st.Zones, ZoneState{Zone: z, Label: z.Label(), Severity: NoData, Brief: "no data"})
		}
		return st
	}
	b := r.Base
	zs := make(map[Zone]*ZoneState, len(Zones))
	for _, z := range Zones {
		zs[z] = &ZoneState{Zone: z, Label: z.Label()}
	}
	shells := 1
	var prodLines, prodBacklogged, fuelBacklogged int
	for _, f := range r.Facilities {
		z := ZoneOf(f.Type, f.Category)
		zs[z].Facilities = append(zs[z].Facilities, f)
		if f.MaintenanceSatisfied != nil && !*f.MaintenanceSatisfied {
			zs[z].raise(Strained, "%s maintenance unsatisfied", f.Type)
		}
		switch {
		case f.Type == "shield_projector":
			shells++
		case z == Production:
			prodLines++
			if f.BacklogTicks > 0 {
				prodBacklogged++
			}
		case f.Type == h2Combustor && f.BacklogTicks > 0:
			fuelBacklogged++
		}
	}

	// Core: power and battery.
	core := zs[Core]
	powerPct := pct(b.PowerDraw, b.PowerSupply)
	switch {
	case b.PowerSupply <= 0:
	case powerPct >= powerCriticalPct:
		core.raise(Critical, "power draw %.0f%% >= %.0f%%", powerPct, powerCriticalPct)
	case powerPct >= powerStrainedPct:
		core.raise(Strained, "power draw %.0f%% >= %.0f%%", powerPct, powerStrainedPct)
	}
	if b.BatteryCapacity > 0 && b.BatteryStored == 0 {
		core.raise(Critical, "battery empty (0/%d)", b.BatteryCapacity)
	}
	batt := "none"
	if b.BatteryCapacity > 0 {
		batt = fmt.Sprintf("%d/%d", b.BatteryStored, b.BatteryCapacity)
	}
	core.Brief = fmt.Sprintf("draw %d/%d · batt %s", b.PowerDraw, b.PowerSupply, batt)

	// Life support.
	life := zs[Life]
	lifePct := pct(b.LifeSupportSupply, b.LifeSupportDemand)
	if b.LifeSupportDemand > 0 {
		switch {
		case lifePct < lifeCriticalPct:
			life.raise(Critical, "life support %.0f%% < %.0f%%", lifePct, lifeCriticalPct)
		case lifePct < lifeStrainedPct:
			life.raise(Strained, "life support %.0f%% < %.0f%%", lifePct, lifeStrainedPct)
		}
	}
	life.Brief = fmt.Sprintf("supply %d / demand %d", b.LifeSupportSupply, b.LifeSupportDemand)

	// Fuel.
	fuel := zs[Fuel]
	fuelPct := pct(b.FuelReserve, b.FuelCapacity)
	if b.FuelCapacity > 0 {
		switch {
		case fuelPct < fuelCriticalPct:
			fuel.raise(Critical, "fuel %.0f%% < %.0f%%", fuelPct, fuelCriticalPct)
		case fuelPct < fuelStrainedPct:
			fuel.raise(Strained, "fuel %.0f%% < %.0f%%", fuelPct, fuelStrainedPct)
		}
	}
	if fuelBacklogged > 0 {
		fuel.raise(Strained, "%s backlogged", h2Combustor)
	}
	fuel.Brief = fmt.Sprintf("%.0f%%", fuelPct)

	// Production.
	prod := zs[Production]
	switch {
	case prodLines > 0 && prodBacklogged == prodLines:
		prod.raise(Critical, "all %d lines backlogged", prodLines)
	case prodBacklogged > 0:
		prod.raise(Strained, "%d of %d lines backlogged", prodBacklogged, prodLines)
	}
	prod.Brief = fmt.Sprintf("%d backlogged / %d lines", prodBacklogged, prodLines)

	// Tourism.
	tour := zs[Tourism]
	tour.Brief = "no passenger data"
	if p := r.Passengers; p != nil {
		if p.DemandLevel != "" && p.DemandLevel != "normal" {
			tour.raise(Strained, "demand %s", p.DemandLevel)
		}
		if strings.Contains(strings.ToLower(p.MarketConditions), "struggling") {
			tour.raise(Strained, "economy struggling")
		}
		level := p.DemandLevel
		if level == "" {
			level = "unknown"
		}
		tour.Brief = level
		if r.TourismPerHour != nil {
			tour.Brief += " · " + commas(int64(*r.TourismPerHour+0.5)) + " cr/h"
		}
	}

	// Defense.
	def := zs[Defense]
	shieldPct := pct(b.Shield, b.MaxShield)
	hullPct := pct(b.Hull, b.MaxHull)
	if b.MaxShield > 0 {
		switch {
		case shieldPct < shieldCriticalPct:
			def.raise(Critical, "shield %.0f%% < %.0f%%", shieldPct, shieldCriticalPct)
		case shieldPct < shieldStrainedPct:
			def.raise(Strained, "shield %.0f%% < %.0f%%", shieldPct, shieldStrainedPct)
		}
	}
	if b.MaxHull > 0 {
		switch {
		case hullPct < hullCriticalPct:
			def.raise(Critical, "hull %.0f%% < %.0f%%", hullPct, hullCriticalPct)
		case hullPct < hullStrainedPct:
			def.raise(Strained, "hull %.0f%% < %.0f%%", hullPct, hullStrainedPct)
		}
	}
	plural := "s"
	if shells == 1 {
		plural = ""
	}
	def.Brief = fmt.Sprintf("%d dps · %d shell%s", b.WeaponDPS, shells, plural)

	zs[Shipyard].Brief = facilityBrief(zs[Shipyard])
	zs[Commerce].Brief = facilityBrief(zs[Commerce])

	st := Status{
		ShieldShells: shells,
		CapturedAt:   r.CapturedAt,
		Stale:        now.Sub(r.CapturedAt) > staleAfter,
		Bars: map[string]float64{
			"hull": hullPct, "shield": shieldPct, "fuel": fuelPct, "power": powerPct,
			"life": lifePct, "satisfaction": float64(b.SatisfactionPct),
		},
	}
	for _, z := range Zones {
		st.Zones = append(st.Zones, *zs[z])
		st.Overall = max(st.Overall, zs[z].Severity)
	}
	st.Word = map[Severity]string{OK: "THRIVING", Strained: "STRAINED", Critical: "CRITICAL"}[st.Overall]
	return st
}

func facilityBrief(z *ZoneState) string {
	if len(z.Facilities) == 1 {
		return "1 facility"
	}
	return fmt.Sprintf("%d facilities", len(z.Facilities))
}

// commas formats n with thousands separators.
func commas(n int64) string {
	s := fmt.Sprintf("%d", n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		s = "-" + s
	}
	return s
}
