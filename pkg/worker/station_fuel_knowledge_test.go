package worker

import (
	"errors"
	"path/filepath"
	"testing"
)

// Access and fuel are independent. Hex Star admits everyone and sells no fuel,
// which is exactly how engineer-5 stranded: it docked without trouble and then
// had no way to fill up. A station proven open must therefore say NOTHING about
// its fuel desk until someone actually tries to buy.
func TestFuelKnowledgeIsIndependentOfAccess(t *testing.T) {
	a := LoadStationAccess(filepath.Join(t.TempDir(), "access.json"))
	a.RecordDock(hexStarBase, nil)

	if !a.Deliverable(hexStarBase) {
		t.Fatal("a successful dock must prove access")
	}
	if _, known := a.Fuel(hexStarBase); known {
		t.Error("docking must not imply anything about the station's fuel desk")
	}

	a.RecordRefuel(hexStarBase, errors.New("Error: no_fuel_source"))
	sells, known := a.Fuel(hexStarBase)
	if !known || sells {
		t.Errorf("Fuel() = (sells=%v, known=%v), want (false, true)", sells, known)
	}
	// ...and learning the station is dry must not retract its access.
	if !a.Deliverable(hexStarBase) {
		t.Error("a dry fuel desk must not mark the station inaccessible")
	}
}

// The two live "no fuel" errors mean opposite things and both appear in the
// fleet logs in the hundreds of thousands: no_fuel_source is the STATION having
// no fuel desk, while no_fuel_cells is the SHIP being out of cells to burn. Only
// the first says anything about the station.
func TestRecordRefuelDistinguishesStationDryFromShipDry(t *testing.T) {
	a := LoadStationAccess(filepath.Join(t.TempDir(), "access.json"))

	for _, err := range []error{
		errors.New("no fuel cells in cargo"),
		errors.New("no_fuel_cells"),
		errors.New("insufficient credits"),
		errors.New("dial tcp: connection reset by peer"),
	} {
		a.RecordRefuel("grand_exchange_station", err)
		if _, known := a.Fuel("grand_exchange_station"); known {
			t.Errorf("%v must teach nothing about the station's fuel desk", err)
		}
	}

	a.RecordRefuel("grand_exchange_station", nil)
	if sells, known := a.Fuel("grand_exchange_station"); !known || !sells {
		t.Errorf("a successful refuel must prove a fuel desk, got (sells=%v, known=%v)", sells, known)
	}
}

// Fuel status is recorded for NPC stations too: access is only ever in question
// at a player station, but a fuel desk is not guaranteed anywhere.
func TestFuelKnowledgeCoversNPCStations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.json")
	a := LoadStationAccess(path)
	a.RecordRefuel("deep_range_outpost", errors.New("no_fuel_source"))

	if sells, known := a.Fuel("deep_range_outpost"); !known || sells {
		t.Errorf("Fuel() = (%v,%v), want (false,true) for an NPC station", sells, known)
	}
	// An NPC station is always deliverable regardless of its fuel desk.
	if !a.Deliverable("deep_range_outpost") {
		t.Error("an NPC station must stay deliverable")
	}
	// The survey's whole point is that it outlives the run that made it.
	if sells, known := LoadStationAccess(path).Fuel("deep_range_outpost"); !known || sells {
		t.Errorf("after reload Fuel() = (%v,%v), want (false,true)", sells, known)
	}
}

// An owner can install or remove a fuel desk, so a later conclusive result wins.
func TestRecordRefuelLatestConclusiveResultWins(t *testing.T) {
	a := LoadStationAccess(filepath.Join(t.TempDir(), "access.json"))

	a.RecordRefuel(obsidianBase, errors.New("no_fuel_source"))
	a.RecordRefuel(obsidianBase, nil)
	if sells, known := a.Fuel(obsidianBase); !known || !sells {
		t.Error("a later successful refuel must clear a stale dry record")
	}
	a.RecordRefuel(obsidianBase, errors.New("no_fuel_source"))
	if sells, _ := a.Fuel(obsidianBase); sells {
		t.Error("a later no_fuel_source must clear a stale fuel record")
	}
}

// Unknown must stay distinguishable from a proven "no": a router may detour
// through an untried station on spec, but must never plan a leg around one that
// has already been proven dry.
func TestUnknownFuelIsNotTheSameAsProvenDry(t *testing.T) {
	a := LoadStationAccess(filepath.Join(t.TempDir(), "access.json"))
	if sells, known := a.Fuel("never_visited_station"); known || sells {
		t.Errorf("Fuel() = (%v,%v), want (false,false) for an untried station", sells, known)
	}
	var nilMap *StationAccess
	if sells, known := nilMap.Fuel(hexStarBase); known || sells {
		t.Error("a nil map must report nothing known")
	}
	nilMap.RecordRefuel(hexStarBase, nil) // must not panic
}
