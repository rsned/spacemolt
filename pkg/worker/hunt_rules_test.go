package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/huntdecide"
	"github.com/rsned/spacemolt/pkg/reload"
)

func testOptions(ids ...string) []huntdecide.Option {
	var out []huntdecide.Option
	for _, id := range ids {
		out = append(out, huntdecide.Option{ID: id})
	}
	return out
}

func testReturnChoice(s huntdecide.State, ids ...string) huntdecide.Choice {
	return huntRules{}.Decide(context.Background(), huntdecide.Decision{
		Kind: huntdecide.KindReturn, State: s, Options: testOptions(ids...),
	})
}

var rulesAutocannon = huntdecide.Weapon{ModuleID: "m1", TypeID: "autocannon_ii", AmmoType: "autocannon", MagazineSize: 20, Loaded: 300}

func TestHuntRulesProvisionPrefersTwoReloads(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []int
		want string
	}{
		{"all buckets", []int{1, 2, 4}, "reloads_2"},
		{"only one", []int{1}, "reloads_1"},
		{"only four", []int{4}, "reloads_4"},
		{"one and four", []int{1, 4}, "reloads_1"},
		{"two and four", []int{2, 4}, "reloads_2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ids []string
			for _, n := range tc.opts {
				ids = append(ids, huntProvisionOptionID(n))
			}
			c := huntRules{}.Decide(context.Background(), huntdecide.Decision{
				Kind: huntdecide.KindProvision, Options: testOptions(ids...),
			})
			if c.OptionID != tc.want {
				t.Errorf("OptionID = %q, want %q", c.OptionID, tc.want)
			}
			if c.Reason == "" {
				t.Error("Reason is empty")
			}
		})
	}
}

func TestHuntRulesReturnRepairBelowHalfHull(t *testing.T) {
	s := huntdecide.State{Hull: 109, MaxHull: 220, Weapons: []huntdecide.Weapon{rulesAutocannon},
		Ammo: []huntdecide.Ammo{{AmmoType: "autocannon", ItemID: "r", Aboard: 40}}}
	if c := testReturnChoice(s, huntOptContinue, huntOptRearm, huntOptRepair); c.OptionID != huntOptRepair {
		t.Errorf("OptionID = %q, want repair", c.OptionID)
	}
}

func TestHuntRulesReturnRearmWithoutReserve(t *testing.T) {
	low := rulesAutocannon
	low.MagazineSize, low.Loaded = 650, 40
	s := huntdecide.State{Hull: 220, MaxHull: 220, Weapons: []huntdecide.Weapon{low}}
	if c := testReturnChoice(s, huntOptContinue, huntOptRearm, huntOptRepair); c.OptionID != huntOptRearm {
		t.Errorf("OptionID = %q, want rearm", c.OptionID)
	}
}

// Observed live 2026-11-22: pirate-5 flew home to rearm with 647/650 in both
// autocannons because it carried no spare box. A dry railgun beside them does
// not change that.
func TestHuntRulesReturnContinuesOnFullMagazinesWithoutReserve(t *testing.T) {
	full := huntdecide.Weapon{ModuleID: "m1", TypeID: "autocannon_ii", AmmoType: "autocannon", MagazineSize: 650, Loaded: 647}
	dryRail := huntdecide.Weapon{ModuleID: "m3", TypeID: "railgun_i", AmmoType: "railgun", MagazineSize: 5}
	s := huntdecide.State{Hull: 220, MaxHull: 220, Weapons: []huntdecide.Weapon{full, full, dryRail}}
	if c := testReturnChoice(s, huntOptContinue, huntOptRearm, huntOptRepair); c.OptionID != huntOptContinue {
		t.Errorf("OptionID = %q, want continue", c.OptionID)
	}
}

func TestHuntWorthReloading(t *testing.T) {
	box := []reload.Candidate{{ItemID: "standard_rounds_box", Quantity: 2}}
	for _, tc := range []struct {
		name string
		e    reload.Entry
		want bool
	}{
		{"dry", reload.Entry{MagazineSize: 650, Candidates: box}, true},
		{"nearly full", reload.Entry{MagazineSize: 650, CurrentAmmo: 648, LoadedAmmoID: "standard_rounds_box", Candidates: box}, false},
		{"below half", reload.Entry{MagazineSize: 5, CurrentAmmo: 2, LoadedAmmoID: "standard_rounds_box", Candidates: box}, true},
		{"below half, other ammo loaded", reload.Entry{MagazineSize: 5, CurrentAmmo: 2, LoadedAmmoID: "other", Candidates: box}, false},
		{"full", reload.Entry{MagazineSize: 5, CurrentAmmo: 5, LoadedAmmoID: "standard_rounds_box", Candidates: box}, false},
		{"dry, nothing aboard", reload.Entry{MagazineSize: 5}, false},
	} {
		if got := huntWorthReloading(tc.e); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestHuntRulesReturnContinueWithFreeWeapon(t *testing.T) {
	pulse := huntdecide.Weapon{ModuleID: "m2", TypeID: "pulse_laser_i"}
	s := huntdecide.State{Hull: 220, MaxHull: 220, Weapons: []huntdecide.Weapon{rulesAutocannon, pulse}}
	if c := testReturnChoice(s, huntOptContinue, huntOptRearm, huntOptRepair); c.OptionID != huntOptContinue {
		t.Errorf("OptionID = %q, want continue", c.OptionID)
	}
}

func TestHuntRulesReturnContinueWithReserve(t *testing.T) {
	s := huntdecide.State{Hull: 220, MaxHull: 220, Weapons: []huntdecide.Weapon{rulesAutocannon},
		Ammo: []huntdecide.Ammo{{AmmoType: "autocannon", ItemID: "r", Aboard: 1}}}
	if c := testReturnChoice(s, huntOptContinue, huntOptRearm, huntOptRepair); c.OptionID != huntOptContinue {
		t.Errorf("OptionID = %q, want continue", c.OptionID)
	}
}

func TestHuntRulesEnergyOnlyNeverRearms(t *testing.T) {
	s := huntdecide.State{Hull: 220, MaxHull: 220, Weapons: []huntdecide.Weapon{{ModuleID: "m2", TypeID: "pulse_laser_i"}}}
	if c := testReturnChoice(s, huntOptContinue, huntOptRearm, huntOptRepair); c.OptionID != huntOptContinue {
		t.Errorf("OptionID = %q, want continue", c.OptionID)
	}
}

func TestHuntRulesReturnPicksOnlyOfferedOptions(t *testing.T) {
	low := rulesAutocannon
	low.MagazineSize, low.Loaded = 650, 40
	dry := huntdecide.State{Hull: 50, MaxHull: 220, Weapons: []huntdecide.Weapon{low}}
	if c := testReturnChoice(dry, huntOptContinue, huntOptRearm); c.OptionID != huntOptRearm {
		t.Errorf("no repair offered, dry: OptionID = %q, want rearm", c.OptionID)
	}
	if c := testReturnChoice(dry, huntOptContinue); c.OptionID != huntOptContinue {
		t.Errorf("only continue offered: OptionID = %q, want continue", c.OptionID)
	}
}

func TestHuntCanFireAndReserve(t *testing.T) {
	loadedDry := huntdecide.Weapon{AmmoType: "autocannon"}
	aboard := huntdecide.Ammo{AmmoType: "autocannon", Aboard: 3}
	wrong := huntdecide.Ammo{AmmoType: "railgun", Aboard: 3}
	for _, tc := range []struct {
		name         string
		s            huntdecide.State
		fire, resrve bool
	}{
		{"loaded no reserve", huntdecide.State{Weapons: []huntdecide.Weapon{rulesAutocannon}}, true, false},
		{"dry with stack", huntdecide.State{Weapons: []huntdecide.Weapon{loadedDry}, Ammo: []huntdecide.Ammo{aboard}}, true, true},
		{"dry with wrong stack", huntdecide.State{Weapons: []huntdecide.Weapon{loadedDry}, Ammo: []huntdecide.Ammo{wrong}}, false, false},
		{"no weapons", huntdecide.State{}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := huntCanFire(tc.s); got != tc.fire {
				t.Errorf("huntCanFire = %v, want %v", got, tc.fire)
			}
			if got := huntHasReserve(tc.s); got != tc.resrve {
				t.Errorf("huntHasReserve = %v, want %v", got, tc.resrve)
			}
		})
	}
}

func TestHuntDeciderDefault(t *testing.T) {
	if _, ok := huntDecider(HuntDeps{}).(huntRules); !ok {
		t.Error("nil Decider should resolve to huntRules")
	}
}

// The objective's own wording names the quarry and outranks the curated table.
func TestHuntRequiredSpeciesReadsTheObjectiveText(t *testing.T) {
	t.Parallel()
	names := map[string]string{"pressblister": "pressblister", "slag-tortoise": "slag_tortoise", "leviathan": "leviathan", "molt leviathan": "molt_leviathan"}
	entry := func(desc string) serverapi.MissionBoardEntry {
		return serverapi.MissionBoardEntry{
			MissionID:  "cracking_the_shell",
			Objectives: []serverapi.MissionObjective{{Type: objectiveKillCreature, Description: desc}},
		}
	}
	for desc, want := range map[string][2]string{
		"Hunt 3 Pressblisters at an ice field":      {"pressblister", "objective text"},
		"Hunt 3 Slag-Tortoises at an asteroid belt": {"slag_tortoise", "objective text"},
		"Slay a Molt Leviathan":                     {"molt_leviathan", "objective text"},
		"Hunt 3 things somewhere":                   {"slag_tortoise", "curated mission table"},
	} {
		got, src := huntRequiredSpecies(entry(desc), names)
		if got != want[0] || src != want[1] {
			t.Errorf("%q: species %q (%s), want %q (%s)", desc, got, src, want[0], want[1])
		}
	}
	if got, src := huntRequiredSpecies(entry("Hunt 3 Pressblisters at an ice field"), nil); got != "slag_tortoise" || src != "curated mission table" {
		t.Errorf("no guide: species %q (%s), want the curated fallback", got, src)
	}
}

func TestHuntNoMissionServicesIsADryBoard(t *testing.T) {
	if !huntNoMissionServices(errors.New("Hive Sulafat does not offer mission services. Try a larger base or station.")) {
		t.Fatal("a base without mission services must read as a dry board")
	}
	if huntNoMissionServices(errors.New("rate limited")) || huntNoMissionServices(nil) {
		t.Fatal("other errors are failed reads, not dry boards")
	}
}
