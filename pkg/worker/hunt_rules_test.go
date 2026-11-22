package worker

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/huntdecide"
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
	s := huntdecide.State{Hull: 220, MaxHull: 220, Weapons: []huntdecide.Weapon{rulesAutocannon}}
	if c := testReturnChoice(s, huntOptContinue, huntOptRearm, huntOptRepair); c.OptionID != huntOptRearm {
		t.Errorf("OptionID = %q, want rearm", c.OptionID)
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
	dry := huntdecide.State{Hull: 50, MaxHull: 220, Weapons: []huntdecide.Weapon{rulesAutocannon}}
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
