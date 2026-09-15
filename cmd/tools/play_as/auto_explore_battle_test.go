package main

import (
	"errors"
	"testing"
)

// explorer-8's last minutes, 2026-09-14: it was in a fight it was losing and
// auto-explore kept trying to fly. Every attempt came back
// "in_battle: cannot perform this action while in combat" and the tour's error
// path printed "(continuing)" and tried the next one — travel to a POI, then a
// jump to taygeta — until the pirates finished it.
//
// The tour has no combat code and should not grow any: the correct response is
// to stop and hand the decision back, because fight-or-flee is exactly the
// judgement an automated survey walk cannot make.
func TestIsInBattle(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"live server text", errors.New(
			"in_battle: cannot perform this action while in combat. Use the 'battle' command to fight or flee."), true},
		{"wrapped", errors.New(
			"jump to taygeta failed: in_battle: cannot perform this action while in combat."), true},
		{"unrelated", errors.New("no POIs in system Fuyue"), false},
		{"docking error", errors.New(
			"You must be docked at a station to perform this action."), false},
		{"nil", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isInBattle(tc.err); got != tc.want {
				t.Errorf("isInBattle(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
