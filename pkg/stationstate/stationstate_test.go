package stationstate

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/assets"
)

var now = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

func cj(t *testing.T) assets.StationReading {
	t.Helper()
	fac, err := os.ReadFile(filepath.Join("..", "assets", "testdata", "cj_facility_list.json"))
	if err != nil {
		t.Fatal(err)
	}
	pax, err := os.ReadFile(filepath.Join("..", "assets", "testdata", "cj_station_passengers.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, rows, ok := assets.FacilityRowsFrom(fac)
	if !ok {
		t.Fatal("facility fixture unreadable")
	}
	p, ok := assets.PassengerSnapshotFrom(pax)
	if !ok {
		t.Fatal("passenger fixture unreadable")
	}
	return assets.StationReading{
		Base: assets.BaseSnapshot{
			Hull: 85000, MaxHull: 85000, Shield: 12000, MaxShield: 12000, Armor: 60, WeaponDPS: 30,
			FuelReserve: 21293, FuelCapacity: 55000, PowerSupply: 40,
			LifeSupportSupply: 12, LifeSupportDemand: 10, SatisfactionPct: 100,
		},
		Facilities: rows, Passengers: &p, CapturedAt: now,
	}
}

func zone(t *testing.T, st Status, z Zone) ZoneState {
	t.Helper()
	for _, zs := range st.Zones {
		if zs.Zone == z {
			return zs
		}
	}
	t.Fatalf("zone %s missing", z)
	return ZoneState{}
}

func TestEvaluate_CJTonight(t *testing.T) {
	r := cj(t)
	rate := 2455.0
	r.TourismPerHour = &rate
	st := Evaluate(r, true, now)
	if got := zone(t, st, Fuel).Severity; got != Strained {
		t.Errorf("fuel = %v", got)
	}
	if got := zone(t, st, Tourism); got.Severity != Strained || got.Brief != "discounted · 2,455 cr/h" {
		t.Errorf("tourism = %v %q", got.Severity, got.Brief)
	}
	if got := zone(t, st, Core).Severity; got != OK {
		t.Errorf("core = %v", got)
	}
	if st.Overall != Strained || st.Word != "STRAINED" {
		t.Errorf("overall = %v %s", st.Overall, st.Word)
	}
	if st.ShieldShells != 1 {
		t.Errorf("shells = %d", st.ShieldShells)
	}
	if math.Abs(st.Bars["fuel"]-38.715) > 0.01 {
		t.Errorf("fuel bar = %v", st.Bars["fuel"])
	}
	if got := zone(t, st, Fuel).Reasons; len(got) != 1 || got[0] != "fuel 39% < 40%" {
		t.Errorf("reasons = %v", got)
	}
}

func TestEvaluate_NoBatteryIsNotCritical(t *testing.T) {
	r := cj(t)
	if got := zone(t, Evaluate(r, true, now), Core).Severity; got != OK {
		t.Errorf("0/0 battery core = %v", got)
	}
	r.Base.BatteryCapacity = 100
	if got := zone(t, Evaluate(r, true, now), Core).Severity; got != Critical {
		t.Errorf("0/100 battery core = %v", got)
	}
}

func TestEvaluate_ZeroMaxes(t *testing.T) {
	r := assets.StationReading{CapturedAt: now}
	st := Evaluate(r, true, now)
	for k, v := range st.Bars {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Errorf("bar %s = %v", k, v)
		}
	}
	for _, k := range []string{"hull", "shield", "fuel", "power", "life"} {
		if _, has := st.Bars[k]; has {
			t.Errorf("bar %s present with zero denominator", k)
		}
	}
	for _, z := range st.Zones {
		if z.Severity != OK {
			t.Errorf("zone %s = %v, want OK", z.Zone, z.Severity)
		}
	}
}

func TestEvaluate_NoData(t *testing.T) {
	st := Evaluate(assets.StationReading{}, false, now)
	if st.Overall != NoData || st.Word != "NO DATA" {
		t.Errorf("got %v %s", st.Overall, st.Word)
	}
}

func TestEvaluate_Stale(t *testing.T) {
	r := cj(t)
	r.CapturedAt = now.Add(-3 * time.Hour)
	if !Evaluate(r, true, now).Stale {
		t.Error("3h old should be stale")
	}
	r.CapturedAt = now.Add(-time.Hour)
	if Evaluate(r, true, now).Stale {
		t.Error("1h old should not be stale")
	}
}

func TestEvaluate_UnknownTypeMaintenance(t *testing.T) {
	r := cj(t)
	no := false
	r.Facilities = append(r.Facilities, assets.FacilityRow{Type: "quantum_spa", Category: "service", MaintenanceSatisfied: &no})
	if got := ZoneOf("quantum_spa", "service"); got != Commerce {
		t.Errorf("ZoneOf = %v", got)
	}
	if got := zone(t, Evaluate(r, true, now), Commerce).Severity; got != Strained {
		t.Errorf("commerce = %v", got)
	}
}

func TestEvaluate_PowerCritical(t *testing.T) {
	r := cj(t)
	r.Base.PowerDraw = 38
	if got := zone(t, Evaluate(r, true, now), Core).Severity; got != Critical {
		t.Errorf("38/40 = %v", got)
	}
	r.Base.PowerDraw = 30
	if got := zone(t, Evaluate(r, true, now), Core).Severity; got != Strained {
		t.Errorf("30/40 = %v", got)
	}
}

func TestEvaluate_ProductionBacklog(t *testing.T) {
	r := cj(t)
	set := func(f func(i int, row *assets.FacilityRow)) assets.StationReading {
		out := r
		out.Facilities = append([]assets.FacilityRow(nil), r.Facilities...)
		for i := range out.Facilities {
			f(i, &out.Facilities[i])
		}
		return out
	}
	one := false
	oneBacklog := set(func(_ int, row *assets.FacilityRow) {
		if !one && ZoneOf(row.Type, row.Category) == Production {
			row.BacklogTicks, one = 50, true
		}
	})
	if got := zone(t, Evaluate(oneBacklog, true, now), Production).Severity; got != Strained {
		t.Errorf("one = %v", got)
	}
	all := set(func(_ int, row *assets.FacilityRow) {
		if ZoneOf(row.Type, row.Category) == Production {
			row.BacklogTicks = 50
		}
	})
	if got := zone(t, Evaluate(all, true, now), Production).Severity; got != Strained {
		t.Errorf("all = %v", got)
	}
	h2 := set(func(_ int, row *assets.FacilityRow) {
		if row.Type == "h2_fuel_combustor" {
			row.BacklogTicks = 50
		}
	})
	st := Evaluate(h2, true, now)
	if got := zone(t, st, Production).Severity; got != OK {
		t.Errorf("h2 only, production = %v", got)
	}
	if got := zone(t, st, Fuel).Severity; got != Strained {
		t.Errorf("h2 only, fuel = %v", got)
	}
}

func TestEvaluate_ShieldShells(t *testing.T) {
	r := cj(t)
	r.Facilities = append(r.Facilities,
		assets.FacilityRow{Type: "shield_projector", Category: "defense"},
		assets.FacilityRow{Type: "shield_projector", Category: "defense"})
	if got := Evaluate(r, true, now).ShieldShells; got != 3 {
		t.Errorf("shells = %d", got)
	}
}

func TestZoneOf(t *testing.T) {
	for _, tc := range []struct {
		typ, cat string
		want     Zone
	}{
		{"stellar_collector_grid", "infrastructure", Core}, {"fusion_reactor", "infrastructure", Core},
		{"battery_bank", "infrastructure", Core}, {"point_defense_battery", "defense", Defense},
		{"reinforced_plating", "defense", Defense}, {"atmospheric_recycler", "infrastructure", Life},
		{"hydroponics_bay", "infrastructure", Life}, {"refueling_dock", "service", Fuel},
		{"h2_fuel_combustor", "production", Fuel}, {"rec_lounge", "infrastructure", Tourism},
		{"transit_lounge", "faction", Tourism}, {"fabrication_yard", "service", Shipyard},
		{"heavy_defense_battery", "defense", Defense}, {"nimh_battery_line", "production", Production},
		{"reactor_core_production_line", "production", Production},
		{"dining_hall_fabrication_unit", "production", Production},
		{"iron_refinery", "production", Production}, {"faction_office", "faction", Commerce},
	} {
		if got := ZoneOf(tc.typ, tc.cat); got != tc.want {
			t.Errorf("%s = %v want %v", tc.typ, got, tc.want)
		}
	}
}

func TestStatusJSON(t *testing.T) {
	b, err := json.Marshal(Evaluate(cj(t), true, now))
	if err != nil || !strings.Contains(string(b), `"Overall":"strained"`) {
		t.Errorf("got %s %v", b, err)
	}
}

func TestSeverityJSON(t *testing.T) {
	b, err := Strained.MarshalText()
	if err != nil || string(b) != "strained" {
		t.Errorf("got %q %v", b, err)
	}
}

// The defense callout sits next to dps, so a bare shell count read as ammo
// (operator, 2026-11-09). It names the shield amount instead.
func TestEvaluate_DefenseBriefNamesShield(t *testing.T) {
	r := cj(t)
	st := Evaluate(r, true, r.CapturedAt)
	for _, z := range st.Zones {
		if z.Zone == Defense && z.Brief != "30 dps · shield 12k" {
			t.Fatalf("defense brief = %q", z.Brief)
		}
	}
}
