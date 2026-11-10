package worker

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// hazardState is an undocked ship at belt (system alzirr) with a station POI,
// a lethal black hole, and a clean planet in the same system.
func hazardState() *game.State {
	return &game.State{
		CurrentPOI: "alzirr_belt",
		System: game.SystemData{ID: "alzirr", POIs: []game.POI{
			{ID: "alzirr_belt", Name: "Alzirr Belt", Position: game.Position{X: 0, Y: 0},
				Hazard: &serverapi.POIHazard{Type: serverapi.HazardDebris, Severity: 2}},
			{ID: "the_maw", Name: "The Maw", Position: game.Position{X: 1, Y: 0},
				Hazard: &serverapi.POIHazard{Type: serverapi.HazardRadiation, Severity: 4}},
			{ID: "alzirr_i", Name: "Alzirr I", Position: game.Position{X: 2, Y: 0}},
			{ID: "relay", Name: "Event Horizon Relay", HasBase: true, BaseID: "b6007", Position: game.Position{X: 5, Y: 0}},
		}},
	}
}

func dose(poi string, sev, hull, maxHull int, at time.Time) game.HazardDose {
	return game.HazardDose{
		HazardWarning: serverapi.HazardWarning{POIID: poi, Hazard: &serverapi.POIHazard{Type: serverapi.HazardDebris, Severity: sev},
			Hull: hull, MaxHull: maxHull},
		At: at,
	}
}

type recRunner struct{ calls [][]string }

func (r *recRunner) run(_ context.Context, t []string) error {
	r.calls = append(r.calls, append([]string(nil), t...))
	return nil
}

func TestHazardShouldLeave(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		mut  func(*game.State)
		want bool
	}{
		{"no dose", func(*game.State) {}, false},
		{"moderate dose leaves", func(s *game.State) { s.LastHazardDose = dose("alzirr_belt", 2, 97, 100, now) }, true},
		{"low dose healthy hull stays", func(s *game.State) { s.LastHazardDose = dose("alzirr_belt", 1, 90, 100, now) }, false},
		{"low dose weak hull leaves", func(s *game.State) { s.LastHazardDose = dose("alzirr_belt", 1, 60, 100, now) }, true},
		{"stale dose", func(s *game.State) { s.LastHazardDose = dose("alzirr_belt", 4, 10, 100, now.Add(-time.Minute)) }, false},
		{"dose at a POI already left", func(s *game.State) { s.LastHazardDose = dose("the_maw", 4, 10, 100, now) }, false},
		{"docked", func(s *game.State) { s.LastHazardDose = dose("alzirr_belt", 4, 10, 100, now); s.Doc = true }, false},
		{"in battle", func(s *game.State) { s.LastHazardDose = dose("alzirr_belt", 4, 10, 100, now); s.InBattle = true }, false},
	}
	for _, c := range cases {
		st := hazardState()
		c.mut(st)
		if got, _ := hazardShouldLeave(st, now); got != c.want {
			t.Errorf("%s: leave=%v want %v", c.name, got, c.want)
		}
	}
}

func TestHazardRefuge(t *testing.T) {
	st := hazardState()
	if got := hazardRefuge(st); len(got) != 2 || got[0][0] != "travel" || got[0][1] != "relay" || got[1][0] != "dock" {
		t.Fatalf("refuge from belt = %v, want travel relay + dock", got)
	}
	st.CurrentPOI = "relay"
	if got := hazardRefuge(st); len(got) != 1 || got[0][0] != "dock" {
		t.Fatalf("refuge at station = %v, want dock", got)
	}
	st = hazardState()
	st.System.POIs = st.System.POIs[:3] // no station: nearest clean POI
	if got := hazardRefuge(st); len(got) != 1 || got[0][1] != "alzirr_i" {
		t.Fatalf("refuge without station = %v, want travel alzirr_i", got)
	}
}

func TestHazardGate_EscapesThenAbortsLoop(t *testing.T) {
	now := time.Now()
	st := hazardState()
	st.LastHazardDose = dose("alzirr_belt", 2, 80, 100, now)
	r := &recRunner{}
	err := hazardGate(context.Background(), io.Discard, st, now, []string{"mine"}, r.run)
	if !errors.Is(err, ErrHazardEscape) {
		t.Fatalf("err = %v, want ErrHazardEscape", err)
	}
	if len(r.calls) != 2 || strings.Join(r.calls[0], " ") != "travel relay" || r.calls[1][0] != "dock" {
		t.Fatalf("escape calls = %v", r.calls)
	}

	// The loop must stop even under -f: mining again is what kills the ship.
	runs := 0
	lerr := ExecuteLoop(context.Background(), io.Discard, 100, true, []Statement{{Raw: "mine", Tokens: []string{"mine"}}}, 0,
		func([]string) error { runs++; return err })
	if !errors.Is(lerr, ErrHazardEscape) || runs != 1 {
		t.Fatalf("loop: err=%v runs=%d, want ErrHazardEscape after 1", lerr, runs)
	}
}

func TestHazardGate_MovementIsNotPreempted(t *testing.T) {
	now := time.Now()
	st := hazardState()
	st.LastHazardDose = dose("alzirr_belt", 4, 30, 100, now)
	r := &recRunner{}
	if err := hazardGate(context.Background(), io.Discard, st, now, []string{"travel", "alzirr_i"}, r.run); err != nil || len(r.calls) != 0 {
		t.Fatalf("travel away: err=%v calls=%v, want pass-through", err, r.calls)
	}
}

func TestHazardGate_RefusesKnownDeadlyPOI(t *testing.T) {
	now := time.Now()
	st := hazardState()
	r := &recRunner{}
	if err := hazardGate(context.Background(), io.Discard, st, now, []string{"travel", "the_maw"}, r.run); !errors.Is(err, ErrHazardRefused) {
		t.Fatalf("travel the_maw: err=%v, want ErrHazardRefused", err)
	}
	if err := hazardGate(context.Background(), io.Discard, st, now, []string{"travel", "The Maw"}, r.run); !errors.Is(err, ErrHazardRefused) {
		t.Fatalf("travel by name: err=%v, want ErrHazardRefused", err)
	}
	// Severity 2 belt: allowed to enter and mine (the dose gate handles it).
	if err := hazardGate(context.Background(), io.Discard, st, now, []string{"mine"}, r.run); err != nil {
		t.Fatalf("mine at moderate belt: %v", err)
	}
	st.CurrentPOI = "the_maw"
	if err := hazardGate(context.Background(), io.Discard, st, now, []string{"mine"}, r.run); !errors.Is(err, ErrHazardRefused) {
		t.Fatalf("mine at the_maw: err=%v, want ErrHazardRefused", err)
	}
	if len(r.calls) != 0 {
		t.Fatalf("refusals must not move the ship: %v", r.calls)
	}
}
