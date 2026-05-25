package spar

import (
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

func TestFormatTickRow(t *testing.T) {
	p := game.BattleParticipant{Username: "Me", Zone: "mid", Stance: "fire", HullPct: 80, ShieldPct: 50}
	row := formatTickRow(917153, p)
	for _, want := range []string{"917153", "Me", "mid", "fire", "80", "50"} {
		if !strings.Contains(row, want) {
			t.Fatalf("row %q missing %q", row, want)
		}
	}
}

func TestFormatSummary(t *testing.T) {
	st := &game.State{}
	st.Hull = 40
	st.MaxHull = 100
	s := formatSummary("Me", st)
	if !strings.Contains(s, "Me") || !strings.Contains(s, "40") {
		t.Fatalf("summary %q missing name or hull", s)
	}
}

func TestFormatSummary_Destroyed(t *testing.T) {
	st := &game.State{}
	st.Hull = 0
	st.MaxHull = 100
	s := formatSummary("Goner", st)
	if !strings.Contains(s, "DESTROYED") {
		t.Fatalf("summary %q should mark a 0-hull ship DESTROYED", s)
	}
}
