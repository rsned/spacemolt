package main

import (
	"log"
	"os"
	"testing"
)

type fakeSide struct {
	name    string
	views   []BattleView // consumed one per View() call; last repeats
	i       int
	actions []string // "stance:fire", "advance", "retreat", "reload"
}

func (f *fakeSide) Name() string { return f.name }
func (f *fakeSide) Battle(action string, kv map[string]any) error {
	if action == "stance" {
		f.actions = append(f.actions, "stance:"+kv["stance"].(string))
	} else {
		f.actions = append(f.actions, action)
	}
	return nil
}
func (f *fakeSide) Reload() error {
	f.actions = append(f.actions, "reload")
	return nil
}
func (f *fakeSide) View() (BattleView, bool) {
	if len(f.views) == 0 {
		return BattleView{}, false
	}
	v := f.views[min(f.i, len(f.views)-1)]
	f.i++
	return v, true
}

func testLogger() *log.Logger { return log.New(os.Stderr, "", 0) }

func mkViews(n int, zone string, parts int) []BattleView {
	vs := make([]BattleView, 0, n+1)
	for t := 1; t <= n; t++ {
		vs = append(vs, BattleView{BattleID: "bx", Tick: t, MyZone: zone, ParticipantCount: parts})
	}
	vs = append(vs, BattleView{BattleID: "bx", Tick: n + 1, Ended: true, Outcome: "stalemate", ParticipantCount: parts})
	return vs
}

func ringPtr(r int) *int { return &r }

func TestRunDuelAppliesStancesAndHoldsRing(t *testing.T) {
	// Both start at outer (ring 3); script holds ring 2 → each side must
	// be ordered to advance until its zone reads mid, then hold.
	views := append(mkViews(3, "outer", 2)[:3], mkViews(3, "mid", 2)...)
	a := &fakeSide{name: "A", views: views}
	b := &fakeSide{name: "B", views: views}
	d := Duel{ID: "t", Attacker: "A", MaxTicks: 10,
		Script: []Phase{{FromTick: 1, StanceA: "fire", StanceB: "fire", HoldRing: ringPtr(2)}}}
	res, err := runDuel(a, b, d, func() {}, testLogger())
	if err != nil {
		t.Fatalf("runDuel: %v", err)
	}
	if res.Outcome != "stalemate" || res.BattleID != "bx" || res.Void {
		t.Errorf("result = %+v", res)
	}
	if a.actions[0] != "stance:fire" {
		t.Errorf("first action = %v, want stance:fire", a.actions)
	}
	advances := 0
	for _, act := range a.actions {
		if act == "advance" {
			advances++
		}
	}
	if advances == 0 {
		t.Errorf("at outer holding ring 2: expected advance orders, got %v", a.actions)
	}
	for i, act := range a.actions {
		_ = i
		if act == "retreat" {
			t.Errorf("holding ring 2 from outer must never retreat: %v", a.actions)
		}
	}
}

func TestRunDuelAppliesAsymmetricHoldRingIndependently(t *testing.T) {
	// A holds ring 1 (inner), B holds ring 2 (mid); both start at outer
	// (ring 3), so both must advance -- but each stops at its own target
	// ring, independently of the other side's hold.
	viewsA := append(mkViews(3, "outer", 2)[:3], mkViews(3, "inner", 2)...)
	viewsB := append(mkViews(3, "outer", 2)[:3], mkViews(3, "mid", 2)...)
	a := &fakeSide{name: "A", views: viewsA}
	b := &fakeSide{name: "B", views: viewsB}
	d := Duel{ID: "t", Attacker: "A", MaxTicks: 10,
		Script: []Phase{{FromTick: 1, StanceA: "fire", StanceB: "fire", HoldRingA: ringPtr(1), HoldRingB: ringPtr(2)}}}
	res, err := runDuel(a, b, d, func() {}, testLogger())
	if err != nil {
		t.Fatalf("runDuel: %v", err)
	}
	if res.Outcome != "stalemate" || res.BattleID != "bx" || res.Void {
		t.Errorf("result = %+v", res)
	}

	countAdvance := func(actions []string) int {
		n := 0
		for _, act := range actions {
			if act == "advance" {
				n++
			}
		}
		return n
	}
	if countAdvance(a.actions) == 0 {
		t.Errorf("A holding ring 1 from outer: expected advance orders, got %v", a.actions)
	}
	if countAdvance(b.actions) == 0 {
		t.Errorf("B holding ring 2 from outer: expected advance orders, got %v", b.actions)
	}
	for _, act := range a.actions {
		if act == "retreat" {
			t.Errorf("A holding ring 1 from outer must never retreat: %v", a.actions)
		}
	}
	for _, act := range b.actions {
		if act == "retreat" {
			t.Errorf("B holding ring 2 from outer must never retreat: %v", b.actions)
		}
	}
}

func TestRunDuelVoidsOnThirdParticipant(t *testing.T) {
	views := []BattleView{
		{BattleID: "bx", Tick: 1, MyZone: "outer", ParticipantCount: 2},
		{BattleID: "bx", Tick: 2, MyZone: "outer", ParticipantCount: 3}, // pirate joins
		{BattleID: "bx", Tick: 3, Ended: true, Outcome: "interference", ParticipantCount: 3},
	}
	a := &fakeSide{name: "A", views: views}
	b := &fakeSide{name: "B", views: views}
	d := Duel{ID: "t", Attacker: "A", MaxTicks: 10,
		Script: []Phase{{FromTick: 1, StanceA: "fire", StanceB: "fire"}}}
	res, err := runDuel(a, b, d, func() {}, testLogger())
	if err != nil {
		t.Fatalf("runDuel: %v", err)
	}
	if !res.Void {
		t.Errorf("third participant must void the duel: %+v", res)
	}
	// After the join both sides must have been ordered to flee out.
	last := a.actions[len(a.actions)-1]
	if last != "stance:flee" {
		t.Errorf("void must order flee, actions = %v", a.actions)
	}
}

func TestRunDuelMaxTicksOrdersFleeOut(t *testing.T) {
	// Battle never ends on its own within MaxTicks: the loop must switch
	// both sides to flee and keep going until the battle ends.
	views := append(mkViews(30, "outer", 2)[:5], // 5 live ticks
		BattleView{BattleID: "bx", Tick: 6, MyZone: "outer", ParticipantCount: 2},
		BattleView{BattleID: "bx", Tick: 7, Ended: true, Outcome: "escape", ParticipantCount: 2})
	a := &fakeSide{name: "A", views: views}
	b := &fakeSide{name: "B", views: views}
	d := Duel{ID: "t", Attacker: "A", MaxTicks: 4,
		Script: []Phase{{FromTick: 1, StanceA: "fire", StanceB: "fire"}}}
	res, err := runDuel(a, b, d, func() {}, testLogger())
	if err != nil {
		t.Fatalf("runDuel: %v", err)
	}
	if res.Outcome != "escape" {
		t.Errorf("outcome = %q", res.Outcome)
	}
	sawFlee := false
	for _, act := range a.actions {
		if act == "stance:flee" {
			sawFlee = true
		}
	}
	if !sawFlee {
		t.Errorf("past MaxTicks the loop must flee out: %v", a.actions)
	}
}

func TestRunDuelReloadEveryTickTrigger(t *testing.T) {
	// ReloadEvery=2 produces reload actions at ticks 2, 4 (multiples of 2
	// while in fire stance), then no more after script switches to flee at tick 5.
	views := append(mkViews(4, "outer", 2)[:4],
		BattleView{BattleID: "bx", Tick: 5, MyZone: "outer", ParticipantCount: 2},
		BattleView{BattleID: "bx", Tick: 6, MyZone: "outer", ParticipantCount: 2},
		BattleView{BattleID: "bx", Tick: 7, Ended: true, Outcome: "stalemate", ParticipantCount: 2})
	a := &fakeSide{name: "A", views: views}
	b := &fakeSide{name: "B", views: views}
	d := Duel{ID: "t", Attacker: "A", MaxTicks: 10, ReloadEvery: 2,
		Script: []Phase{
			{FromTick: 1, StanceA: "fire", StanceB: "fire"},
			{FromTick: 5, StanceA: "flee", StanceB: "flee"},
		}}
	res, err := runDuel(a, b, d, func() {}, testLogger())
	if err != nil {
		t.Fatalf("runDuel: %v", err)
	}
	if res.Outcome != "stalemate" || res.Void {
		t.Errorf("result = %+v", res)
	}

	// Count reloads before and after the flee stance.
	reloadBeforeFlee := 0
	reloadAfterFlee := false
	sawFlee := false
	for _, act := range a.actions {
		if act == "stance:flee" {
			sawFlee = true
		}
		if act == "reload" {
			if sawFlee {
				reloadAfterFlee = true
			} else {
				reloadBeforeFlee++
			}
		}
	}

	// Expect 2 reloads in fire stance (ticks 2, 4).
	if reloadBeforeFlee != 2 {
		t.Errorf("reloads while firing = %d, want 2: %v", reloadBeforeFlee, a.actions)
	}
	// No reloads should occur after the flee stance.
	if reloadAfterFlee {
		t.Errorf("no reloads should occur after flee stance: %v", a.actions)
	}
}
