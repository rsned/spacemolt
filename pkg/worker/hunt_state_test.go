package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/huntdecide"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/reload"
)

func huntStateFixture(t *testing.T) (HuntDeps, *huntFake) {
	t.Helper()
	f := newHuntFake(t, huntFakeOpts{cargoCapacity: 100})
	f.state.Password = "hunter2-secret-value"
	f.state.CurrentTick = 1500
	f.state.Ship.CargoUsed = 30
	deps := huntDeps(f, &strings.Builder{})
	reach := 2
	if err := deps.KB.StoreItems(context.Background(), []knowledge.CatalogItem{{
		ID: "autocannon_ii", Name: "Autocannon II",
		Module: &knowledge.ItemModule{Type: "weapon", Weapon: &knowledge.ItemWeapon{
			DamageType: "kinetic", Reach: &reach, Cooldown: 3, AmmoType: "autocannon",
		}},
	}}); err != nil {
		t.Fatal(err)
	}
	deps.Version, deps.Commit = "v1.2.3", "abc123"
	return deps, f
}

func TestHuntDecisionStateProjectsShipAmmoAndSortie(t *testing.T) {
	deps, f := huntStateFixture(t)
	lo := reload.Loadout{
		Plan: []reload.Entry{{
			WeaponID: "mod1", WeaponTypeID: "autocannon_ii", AmmoType: "autocannon",
			CurrentAmmo: 12, MagazineSize: 20, LoadedAmmoID: "std_rounds",
			Candidates: []reload.Candidate{{ItemID: "std_rounds", Quantity: 7}},
		}},
		Free: []reload.FreeWeapon{{ModuleID: "mod2", TypeID: "pulse_laser_i"}},
	}
	sortie := &huntdecide.Sortie{
		StartTick: 1400, Fights: 3, Kills: 2, ReloadsSpent: 1,
		Grounds: []huntdecide.GroundVisit{{POI: "belt_a", Quarry: true}, {POI: "belt_b"}},
	}
	job := huntJob{boardID: "first_hunt", activeID: "hex-first_hunt", required: "belt_grazer", target: 5, baseline: 2}

	s := huntDecisionState(context.Background(), deps, job, sortie, lo)

	if s.AgentID != "pirate-6" || s.GameVersion == "" || s.WorkerCommit != "abc123" {
		t.Errorf("identity fields wrong: %+v", s)
	}
	if len(s.Weapons) != 2 {
		t.Fatalf("Weapons = %+v, want 2", s.Weapons)
	}
	w := s.Weapons[0]
	if w.ModuleID != "mod1" || w.DamageType != "kinetic" || w.Reach != 2 || w.Cooldown != 3 ||
		w.AmmoType != "autocannon" || w.Loaded != 12 || w.MagazineSize != 20 || w.LoadedAmmoID != "std_rounds" {
		t.Errorf("ammo-fed weapon = %+v", w)
	}
	if s.Weapons[1].ModuleID != "mod2" || s.Weapons[1].AmmoType != "" {
		t.Errorf("free weapon = %+v", s.Weapons[1])
	}
	if len(s.Ammo) != 1 || s.Ammo[0] != (huntdecide.Ammo{AmmoType: "autocannon", ItemID: "std_rounds", Aboard: 7}) {
		t.Errorf("Ammo = %+v", s.Ammo)
	}
	if s.FreeHold != 70 {
		t.Errorf("FreeHold = %d, want 70", s.FreeHold)
	}
	if s.Job.Kind != "mission" || s.Job.ID != "hex-first_hunt" || s.Job.Species != "belt_grazer" || s.Job.Target != 5 || s.Job.Done != 2 {
		t.Errorf("Job = %+v", s.Job)
	}
	if s.Tick != f.state.CurrentTick || s.Sortie.TicksOut != 100 {
		t.Errorf("Tick = %d, TicksOut = %d, want %d and 100", s.Tick, s.Sortie.TicksOut, f.state.CurrentTick)
	}
	if len(s.Sortie.Grounds) != 2 || s.Sortie.Fights != 3 || s.Sortie.Kills != 2 || s.Sortie.ReloadsSpent != 1 {
		t.Errorf("Sortie = %+v", s.Sortie)
	}
	// huntKB: test_system -> haven_station (repair-less, no services), no HomeStation set.
	if s.JumpsToHome != -1 {
		t.Errorf("JumpsToHome = %d, want -1 with no home station", s.JumpsToHome)
	}
}

func TestHuntDecisionStateJumpsAndNilKB(t *testing.T) {
	deps, f := huntStateFixture(t)
	f.state.System.ID = "pollux"
	f.state.CurrentSystem = "pollux"
	deps.HomeStation = "haven_station"
	s := huntDecisionState(context.Background(), deps, huntJob{home: true}, nil, reload.Loadout{})
	if s.JumpsToHome != 1 {
		t.Errorf("JumpsToHome = %d, want 1", s.JumpsToHome)
	}
	if s.Job.Kind != "home" || s.Job.ID != "home grounds" {
		t.Errorf("Job = %+v", s.Job)
	}
	if s.Sortie.TicksOut != 0 || s.Sortie.Grounds == nil {
		t.Errorf("nil sortie should give a zero value with empty grounds: %+v", s.Sortie)
	}

	deps.KB = nil
	s = huntDecisionState(context.Background(), deps, huntJob{}, nil, reload.Loadout{})
	if s.JumpsToHome != -1 || s.JumpsToStation != -1 {
		t.Errorf("jumps with no KB = %d, %d, want -1, -1", s.JumpsToHome, s.JumpsToStation)
	}
}

func TestHuntJobKind(t *testing.T) {
	for _, tc := range []struct {
		name     string
		job      huntJob
		wantKind string
		wantID   string
	}{
		{"home", huntJob{home: true}, "home", "home grounds"},
		{"need", huntJob{need: &huntNeed{MissionID: "need-9"}}, "need", "need-9"},
		{"mission active", huntJob{boardID: "b", activeID: "a"}, "mission", "a"},
		{"mission board only", huntJob{boardID: "b"}, "mission", "b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kind, id := huntJobKind(tc.job)
			if kind != tc.wantKind || id != tc.wantID {
				t.Errorf("got %q/%q, want %q/%q", kind, id, tc.wantKind, tc.wantID)
			}
		})
	}
}

func TestHuntDecisionStateHasNoPassword(t *testing.T) {
	deps, _ := huntStateFixture(t)
	s := huntDecisionState(context.Background(), deps, huntJob{}, huntdecide.NewSortie("pirate-6", time.Unix(0, 0)), reload.Loadout{})
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "assword") || strings.Contains(string(b), "hunter2-secret-value") {
		t.Errorf("decision state leaks the password: %s", b)
	}
}
