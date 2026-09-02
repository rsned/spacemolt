package main

import (
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestComputeFitActions(t *testing.T) {
	cur := []string{"mining_laser_i", "pulse_laser_i", "pulse_laser_i"}
	want := []string{"pulse_laser_i", "missile_launcher_i"}
	rem, inst := computeFitActions(cur, want)
	sort.Strings(rem)
	sort.Strings(inst)
	if !reflect.DeepEqual(rem, []string{"mining_laser_i", "pulse_laser_i"}) {
		t.Errorf("remove = %v", rem)
	}
	if !reflect.DeepEqual(inst, []string{"missile_launcher_i"}) {
		t.Errorf("install = %v", inst)
	}
	// Duplicates count: two pulse lasers wanted, one present → install one.
	rem2, inst2 := computeFitActions([]string{"pulse_laser_i"}, []string{"pulse_laser_i", "pulse_laser_i"})
	if len(rem2) != 0 || !reflect.DeepEqual(inst2, []string{"pulse_laser_i"}) {
		t.Errorf("dup case: rem=%v inst=%v", rem2, inst2)
	}
}

func TestBattleEnded(t *testing.T) {
	cases := []struct {
		name       string
		seenBattle bool
		inBattle   bool
		want       bool
	}{
		{"never seen a battle, not in battle: no battle at all, not ended", false, false, false},
		{"never seen a battle, in battle: first tick of a fresh battle", false, true, false},
		{"seen and still in battle: mid-fight", true, true, false},
		{"seen and no longer in battle: this battle ended", true, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := battleEnded(c.seenBattle, c.inBattle); got != c.want {
				t.Errorf("battleEnded(%v, %v) = %v, want %v", c.seenBattle, c.inBattle, got, c.want)
			}
		})
	}
}

// TestResetBattleTrackingClearsStaleEnded reproduces the cross-duel bug: a
// Bot that saw duel 1 end (seenBattle=true) must NOT report duel 2 as
// instantly ended just because InBattle hasn't flipped true yet -- unless
// ResetBattleTracking is called between duels, in which case the stale
// seenBattle=true no longer causes a false "ended".
func TestResetBattleTrackingClearsStaleEnded(t *testing.T) {
	b := &Bot{seenBattle: true, battleFirstSeen: time.Now().Add(-time.Hour)}

	// Without a reset: the bug. State.InBattle is still false right after
	// duel 2's Attack() is issued (the push hasn't landed yet), and the
	// stale seenBattle=true from duel 1 makes battleEnded report true.
	if !battleEnded(b.seenBattle, false) {
		t.Fatalf("setup: expected the stale-seenBattle bug to reproduce before reset")
	}

	b.ResetBattleTracking()

	if b.seenBattle {
		t.Errorf("seenBattle not cleared by ResetBattleTracking")
	}
	if !b.battleFirstSeen.IsZero() {
		t.Errorf("battleFirstSeen not cleared by ResetBattleTracking")
	}
	if battleEnded(b.seenBattle, false) {
		t.Errorf("after ResetBattleTracking, a not-yet-in-battle poll must not read as ended")
	}
}

func TestParseAvailable(t *testing.T) {
	cases := []struct {
		msg  string
		want int
	}{
		{"battle_bot2 withdraw_items: insufficient_storage: Storage only has 4 x standard_guided_missiles. Use 'view_storage' to check.", 4},
		{"Storage only has 20 x standard_rounds_box.", 20},
		{"some unrelated error with no count", 0},
		{"", 0},
	}
	for _, c := range cases {
		if got := parseAvailable(c.msg); got != c.want {
			t.Errorf("parseAvailable(%q) = %d, want %d", c.msg, got, c.want)
		}
	}
}
