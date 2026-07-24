package worker

import (
	"path/filepath"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func TestSaveLoadHeldFreightRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := freightHeldPath(dir, "engineer-3")
	want := []*serverapi.ShipmentContract{
		{ID: "c1", PackageID: "h1", DestinationBaseID: "sol_central", DeadlineTick: 1380},
		{ID: "c2", PackageID: "h2", DestinationBaseID: "nova_terra", DeadlineTick: 1450},
	}
	if err := saveHeldFreight(path, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := loadHeldFreight(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 2 || got[0].ID != "c1" || got[1].DestinationBaseID != "nova_terra" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestLoadHeldFreightMissingFileIsEmpty(t *testing.T) {
	got, err := loadHeldFreight(filepath.Join(t.TempDir(), "nope", "freight-held.json"))
	if err != nil {
		t.Fatalf("a missing file must not error, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("a missing file must load empty, got %+v", got)
	}
}

// addHeldFreight / removeHeldFreight must drive persistHeld with the post-mutation
// set so the file always reflects what the worker is carrying.
func TestHeldFreightMutationsPersist(t *testing.T) {
	var last []*serverapi.ShipmentContract
	s := &missionRunState{persistHeld: func(cs []*serverapi.ShipmentContract) { last = cs }}

	s.addHeldFreight(&serverapi.ShipmentContract{ID: "c1"})
	if len(last) != 1 || last[0].ID != "c1" {
		t.Fatalf("add must persist the set, got %+v", last)
	}
	s.addHeldFreight(&serverapi.ShipmentContract{ID: "c2"})
	if len(last) != 2 {
		t.Fatalf("second add must persist both, got %+v", last)
	}
	s.removeHeldFreight("c1")
	if len(last) != 1 || last[0].ID != "c2" {
		t.Fatalf("remove must persist the survivor, got %+v", last)
	}
}

// ensureFreightPersistence loads any persisted set into mission state and wires the
// callback, but ONLY for a freight worker with an AgentID. A second call is a no-op.
func TestEnsureFreightPersistenceLoadsAndWires(t *testing.T) {
	dir := t.TempDir()
	seed := []*serverapi.ShipmentContract{{ID: "c1", DestinationBaseID: "sol_central", DeadlineTick: 1380}}
	if err := saveHeldFreight(freightHeldPath(dir, "engineer-3"), seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	d := &WorkerDispatch{AgentID: "engineer-3", AgentsDir: dir, EnableFreight: true, mission: &missionRunState{}}

	d.ensureFreightPersistence()

	if d.mission.heldFreightCount() != 1 {
		t.Fatalf("must load the persisted contract, count = %d", d.mission.heldFreightCount())
	}
	if d.mission.persistHeld == nil {
		t.Fatal("must wire the persist callback")
	}
	// A live mutation now writes through to the same file.
	d.mission.removeHeldFreight("c1")
	got, err := loadHeldFreight(freightHeldPath(dir, "engineer-3"))
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("the removal must have persisted, file still holds %+v", got)
	}
}

// A non-freight worker (or one without an AgentID) must not load or wire anything —
// no file churn for the general pool.
func TestEnsureFreightPersistenceInertWhenDisabled(t *testing.T) {
	d := &WorkerDispatch{AgentID: "explorer-1", AgentsDir: t.TempDir(), EnableFreight: false, mission: &missionRunState{}}
	d.ensureFreightPersistence()
	if d.mission.persistHeld != nil {
		t.Fatal("persistence must stay inert when freight is disabled")
	}
}
