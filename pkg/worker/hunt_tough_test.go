package worker

import (
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/huntdecide"
)

// gauntletThreat is a Gauntlet's score sheet with the figures measured on
// 2026-11-27.
func gauntletThreat() huntThreat {
	return huntThreat{
		shipClass: "gauntlet", buffer: 285 + 85, hunterPerTick: huntDamagePerTickDefault,
		perTick: map[string]float64{
			"chlorophage": 2.8, "pilot_whale": 7.2, "cloudwhale": 60,
			"crusher_mantis": 45.3, "frost_lurker": 12.7, "ash_nibbler": 1, "hoarfrost_leviathan": 6,
		},
		shots: map[string]int{"frost_lurker": 40, "hoarfrost_leviathan": 6},
	}
}

func TestHuntDangerScoresTheFleetsQuarry(t *testing.T) {
	t.Parallel()
	th := gauntletThreat()
	for _, tc := range []struct {
		species string
		hull    int
		lo, hi  float64
	}{
		{"chlorophage", 55, 0.01, 0.04},
		{"pilot_whale", 220, 0.20, 0.28},
		{"cloudwhale", 3501, 30, 33},
	} {
		got, known := th.danger(tc.species, tc.hull)
		if !known || got < tc.lo || got > tc.hi {
			t.Errorf("%s: danger %.2f known=%v, want %.2f..%.2f", tc.species, got, known, tc.lo, tc.hi)
		}
	}
	if _, known := th.danger("mystery", 100); known {
		t.Error("a species with no recorded attacks has no score")
	}
	// Twice the damage halves the score: a stronger hull qualifies with no
	// list edited.
	strong := gauntletThreat()
	strong.hunterPerTick *= 2
	strong.buffer *= 2
	if tough, _ := th.tooTough("frost_lurker", 400); !tough {
		t.Error("a Gauntlet must not take a frost lurker (76%)")
	}
	if tough, why := strong.tooTough("frost_lurker", 400); tough {
		t.Errorf("a hull with twice the damage and twice the buffer takes a frost lurker (19%%): %s", why)
	}
}

// pirate-7's loss of 2026-11-26: a Cloudwhale is a grazer, so the role check
// let it through as quarry for a Lumen Oil need.
func TestHuntNeverEngagesWhatIsTooTough(t *testing.T) {
	t.Parallel()
	creatures := []serverapi.NearbyCreature{
		{CreatureID: "lev_1", Species: "cloudwhale", Role: "grazer", MaxHull: 3501},
		{CreatureID: "lev_2", Species: "cloudwhale", Role: "grazer"}, // hull unread
		{CreatureID: "crt_1", Species: "mystery_grazer", Role: "grazer", MaxHull: huntQuarryMaxHull + 1},
		{CreatureID: "crt_2", Species: "pilot_whale", Role: "grazer", MaxHull: 220},
		{CreatureID: "crt_3", Species: "mystery_grazer", Role: "grazer", MaxHull: huntQuarryMaxHull},
		// Measured and mild, so the hull cap does not apply to it.
		{CreatureID: "crt_4", Species: "ash_nibbler", Role: "grazer", MaxHull: 900},
	}
	for name, th := range map[string]huntThreat{"scored": gauntletThreat(), "unscored": {}} {
		var log strings.Builder
		var ids []string
		for _, c := range huntAdmissibleQuarry(creatures, true, "", th, &log) {
			ids = append(ids, c.CreatureID)
		}
		want := []string{"crt_2", "crt_3", "crt_4"}
		if name == "unscored" {
			want = []string{"crt_2", "crt_3"} // 900 hull and nothing measured
		}
		if !slices.Equal(ids, want) {
			t.Errorf("%s: quarry %v want %v\n%s", name, ids, want, log.String())
		}
	}
	if got := huntNeedQuarry(creatures, []string{"cloudwhale"}, gauntletThreat(), io.Discard); len(got) != 0 {
		t.Errorf("a need for cloudwhale has no quarry, got %+v", got)
	}
}

// A predator is quarry only on a measured score within the limit. None is
// for a Gauntlet; the hull cap never clears one.
func TestHuntPredatorIsQuarryOnlyWhenTheScoreClearsIt(t *testing.T) {
	t.Parallel()
	creatures := []serverapi.NearbyCreature{
		{CreatureID: "p1", Species: "frost_lurker", Role: "predator", MaxHull: 400},
		{CreatureID: "p2", Species: "geiger_hound", Role: "predator", MaxHull: 300}, // never measured
		{CreatureID: "g1", Species: "pilot_whale", Role: "grazer", MaxHull: 220},
	}
	weak, strong := gauntletThreat(), gauntletThreat()
	strong.hunterPerTick *= 2
	strong.buffer *= 2

	if p, found := huntPredatorPresent(creatures, weak); !found || p.CreatureID != "p1" {
		t.Errorf("a Gauntlet leaves a ground with a frost lurker, got %v %v", p, found)
	}
	if p, found := huntPredatorPresent(creatures, strong); !found || p.CreatureID != "p2" {
		t.Errorf("an unmeasured predator still clears the ground for any hull, got %v %v", p, found)
	}
	got := huntAdmissibleQuarry(creatures[:1], true, "", strong, io.Discard)
	if len(got) != 1 {
		t.Errorf("the stronger hull takes the frost lurker as quarry, got %v", got)
	}
	if got := huntAdmissibleQuarry(creatures, true, "", weak, io.Discard); len(got) != 1 || got[0].CreatureID != "g1" {
		t.Errorf("a Gauntlet takes only the pilot whale, got %v", got)
	}

	// 29% on six shots: too little to trust against a predator.
	hatchling := []serverapi.NearbyCreature{{CreatureID: "lev_h", Species: "hoarfrost_leviathan", Role: "predator", MaxHull: 312}}
	if score, _ := weak.danger("hoarfrost_leviathan", 312); score > huntDangerMax {
		t.Fatalf("fixture: the hatchling must score under the limit, got %.2f", score)
	}
	if got := huntAdmissibleQuarry(hatchling, true, "", weak, io.Discard); len(got) != 0 {
		t.Errorf("a predator scored on %d shots is not quarry, got %v", weak.shots["hoarfrost_leviathan"], got)
	}

	roles := func(string) string { return "predator" }
	hulls := func(string) int { return 400 }
	need := huntNeed{Species: []string{"frost_lurker"}}
	if ok, _ := huntAdmissibleNeed(need, 1, roles, hulls, weak); ok {
		t.Error("a Gauntlet must refuse a frost lurker need")
	}
	if ok, why := huntAdmissibleNeed(need, 1, roles, hulls, strong); !ok {
		t.Errorf("the stronger hull takes a frost lurker need: %s", why)
	}
	if ok, _ := huntAdmissibleNeed(huntNeed{Species: []string{"geiger_hound"}}, 1, roles, func(string) int { return 300 }, strong); ok {
		t.Error("an unmeasured predator is never a need's quarry")
	}
}

func TestHuntAdmissibleNeedRefusesTooTough(t *testing.T) {
	t.Parallel()
	grazer := func(string) string { return "grazer" }
	ok, reason := huntAdmissibleNeed(huntNeed{Species: []string{"cloudwhale"}}, 1, grazer, nil, huntThreat{})
	if ok || !strings.Contains(reason, "too tough") {
		t.Errorf("ok=%v reason=%q, want a too-tough refusal", ok, reason)
	}
	// Another source the fleet can take keeps the need.
	if ok, reason := huntAdmissibleNeed(huntNeed{Species: []string{"cloudwhale", "pilot_whale"}}, 1, grazer, nil, huntThreat{}); !ok {
		t.Errorf("a need with a huntable source must stay: %s", reason)
	}
}

// A sortie that closes in another hull lost its ship, whatever result it was
// heading home with.
func TestHuntSortieClosingInAnotherHullIsDestroyed(t *testing.T) {
	t.Parallel()
	st := &game.State{}
	st.Ship.ClassID = "cobble"
	deps := HuntDeps{AgentID: "pirate-7", Client: &huntStateClient{st: st}}
	s := &huntdecide.Sortie{ID: "s1", ShipClass: "gauntlet", LastEngaged: "cloudwhale"}
	var log strings.Builder
	if got := huntSortieLostShip(deps, &log, s, huntdecide.ResultReturnedDamaged); got != huntdecide.ResultDestroyed {
		t.Errorf("got %q want destroyed", got)
	}
	if !strings.Contains(log.String(), "SHIP DESTROYED") || !strings.Contains(log.String(), "cloudwhale") {
		t.Errorf("log must name the loss and what was engaged: %s", log.String())
	}
	st.Ship.ClassID = "gauntlet"
	if got := huntSortieLostShip(deps, io.Discard, s, huntdecide.ResultCompleted); got != huntdecide.ResultCompleted {
		t.Errorf("same hull keeps its result, got %q", got)
	}
	// A sortie file written before the field existed says nothing.
	st.Ship.ClassID = "cobble"
	if got := huntSortieLostShip(deps, io.Discard, &huntdecide.Sortie{ID: "old"}, huntdecide.ResultNoQuarry); got != huntdecide.ResultNoQuarry {
		t.Errorf("no recorded hull keeps its result, got %q", got)
	}
}

// huntStateClient is a GameClient that only knows its state.
type huntStateClient struct {
	game.GameClient
	st *game.State
}

func (c *huntStateClient) GetState() *game.State { return c.st }

// Every creature at the ground is an option: the quarry, and the ones left
// alone with why. The rule picks as huntPickQuarry does.
func TestHuntTargetOptionsCarryWhatWasRefused(t *testing.T) {
	t.Parallel()
	all := []serverapi.NearbyCreature{
		{CreatureID: "lev_1", Species: "cloudwhale", Role: "grazer", Hull: 3501, MaxHull: 3501},
		{CreatureID: "crt_1", Species: "pilot_whale", Role: "grazer", Hull: 220, MaxHull: 220},
		{CreatureID: "crt_2", Species: "pilot_whale", Role: "grazer", Hull: 90, MaxHull: 220},
		{CreatureID: "crt_3", Species: "drift_ray", Role: "grazer", Hull: 40, MaxHull: 40},
		{CreatureID: "crt_4", Species: "pilot_whale", Role: "grazer", Hull: 10, MaxHull: 220, InCombat: true},
	}
	quarry := huntNeedQuarry(all, []string{"cloudwhale", "pilot_whale"}, gauntletThreat(), io.Discard)
	opts := huntTargetOptions(quarry, all, gauntletThreat())
	if len(opts) != len(all) {
		t.Fatalf("want one option per creature, got %d", len(opts))
	}
	refused := map[string]string{}
	for _, o := range opts {
		if ok, _ := o.Features["admissible"].(bool); !ok {
			refused[o.ID], _ = o.Features["refused"].(string)
			if !o.NoExplore {
				t.Errorf("%s is refused and must never be explored", o.ID)
			}
		}
	}
	want := map[string]string{"lev_1": huntRefusedTooTough, "crt_3": huntRefusedNotQuarry, "crt_4": huntRefusedInCombat}
	if len(refused) != len(want) {
		t.Fatalf("refused = %v want %v", refused, want)
	}
	for id, why := range want {
		if refused[id] != why {
			t.Errorf("%s refused as %q want %q", id, refused[id], why)
		}
	}
	got := huntRules{}.Decide(t.Context(), huntdecide.Decision{Kind: huntdecide.KindTarget, Options: opts})
	if _, idx := huntPickQuarry(quarry); got.OptionID != "crt_2" || quarry[idx].CreatureID != got.OptionID {
		t.Errorf("rule picked %q, huntPickQuarry %q; want crt_2 from both", got.OptionID, quarry[idx].CreatureID)
	}
}
