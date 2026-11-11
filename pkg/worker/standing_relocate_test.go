package worker

import (
	"context"
	"io"
	"slices"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// After a death the client knows no system; the next command must re-read
// the location first (salvager-5, 2026-11-11).
func TestRelocateIfUnknownRefreshesAfterRespawn(t *testing.T) {
	var ran []string
	run := func(_ context.Context, tok []string) error { ran = append(ran, tok[0]); return nil }

	if !relocateIfUnknown(context.Background(), io.Discard, &game.State{Died: true}, []string{"haul"}, run) {
		t.Fatal("want a refresh when the system is unknown")
	}
	if !slices.Equal(ran, []string{"get_status", "get_system"}) {
		t.Errorf("ran %v", ran)
	}
}

func TestRelocateIfUnknownSkips(t *testing.T) {
	run := func(context.Context, []string) error { t.Error("unexpected refresh"); return nil }
	known := &game.State{Died: true, System: game.SystemData{ID: "vega"}}
	for name, tc := range map[string]struct {
		st  *game.State
		tok []string
	}{
		"system known":    {known, []string{"haul"}},
		"never died":      {&game.State{}, []string{"haul"}},
		"name only known": {&game.State{Died: true, CurrentSystem: "Vega"}, []string{"haul"}},
		"is the refresh":  {&game.State{Died: true}, []string{"get_system"}},
		"no state":        {nil, []string{"haul"}},
	} {
		if relocateIfUnknown(context.Background(), io.Discard, tc.st, tc.tok, run) {
			t.Errorf("%s: refreshed", name)
		}
	}
}
