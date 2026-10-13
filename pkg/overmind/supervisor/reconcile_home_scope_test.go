package supervisor

import (
	"os"
	"path/filepath"
	"testing"
)

// A sweep is configured for ONE home fleet, but the ledger holds entries for
// every fleet that rotates. ReconcileSecondments iterated all of them and
// applied the configured home to each, so a craft->unlock entry processed by a
// haul-homed sweep edited HAUL's overrides: the agent was removed from a fleet
// that never rostered it and left running in BOTH craft and unlock. Six
// craftsmen hit exactly that on 2026-10-13, and a double-run is the
// session_replaced thrash that kills worker sessions.
//
// A sweep must touch only the entries whose HomeFleet is the one it was given.
func TestReconcileSkipsEntriesForAnotherHomeFleet(t *testing.T) {
	dir := t.TempDir()
	ledger := filepath.Join(dir, "secondments.json")

	homeOv := filepath.Join(dir, "haul-overrides.json")
	awayOv := filepath.Join(dir, "unlock-overrides.json")
	for _, p := range []string{homeOv, awayOv} {
		if err := os.WriteFile(p, []byte(`{"removed":[]}`), 0o600); err != nil {
			t.Fatalf("seed %s: %v", p, err)
		}
	}

	led := &Secondments{Entries: []Secondment{{
		AgentID: "craftsman-3", HomeFleet: "craft", AwayFleet: "unlock", Phase: PhaseNominated,
	}}}
	if err := SaveSecondments(ledger, *led); err != nil {
		t.Fatalf("SaveSecondments: %v", err)
	}

	home := FleetSide{Name: "haul", OverridesPath: homeOv}
	away := FleetSide{Name: "unlock", OverridesPath: awayOv}

	log, err := ReconcileSecondments(ledger, home, away, ReconcileOptions{MaxInFlight: 9})
	if err != nil {
		t.Fatalf("ReconcileSecondments: %v", err)
	}
	if len(log) != 0 {
		t.Errorf("log = %v, want nothing done for a craft-homed entry under a haul sweep", log)
	}

	// The decisive assertion: haul's removed-set must be untouched. Writing
	// craftsman-3 there is what produced the double-run.
	ov, err := LoadOverrides(homeOv)
	if err != nil {
		t.Fatalf("LoadOverrides: %v", err)
	}
	if ov.IsRemoved("craftsman-3") {
		t.Error("haul overrides gained craftsman-3; a haul sweep must not touch a craft-homed entry")
	}

	after, err := LoadSecondments(ledger)
	if err != nil {
		t.Fatalf("LoadSecondments: %v", err)
	}
	if after.Entries[0].Phase != PhaseNominated {
		t.Errorf("phase = %q, want it left %q for the craft-homed sweep to take", after.Entries[0].Phase, PhaseNominated)
	}
}
