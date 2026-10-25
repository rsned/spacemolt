package worker

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// craftFake refuses any craft above `affordable` output units with the
// server's cannot_craft wording and records every quantity attempted.
type craftFake struct {
	*fakeClient
	affordable int
	qtys       []int
	presets    []string
}

func (f *craftFake) CraftWithQuantity(_ context.Context, _ string, qty int) error {
	f.qtys = append(f.qtys, qty)
	if qty > f.affordable {
		return errors.New("cannot_craft: Not enough materials in your station storage to craft this. Deposit the inputs into station storage first (crafting no longer pulls from cargo).")
	}
	return nil
}

func (f *craftFake) CraftWithPreset(ctx context.Context, recipe string, qty int, preset string) error {
	f.presets = append(f.presets, preset)
	return f.CraftWithQuantity(ctx, recipe, qty)
}

func newCraftDispatch(f *craftFake) (*WorkerDispatch, *bytes.Buffer) {
	var out bytes.Buffer
	d := NewWorkerDispatch(f, nil, nil, &out)
	return d, &out
}

// TestCraftShrinksToWhatInputsAllow: `craft distill_moonshine 60` with inputs
// for 12 units must not fail outright — live 2026-10-25 both crafters refused
// every ten-minute job for hours while faction storage held enough for small
// batches. The verb halves the quantity down to craftMinQuantity (10).
func TestCraftShrinksToWhatInputsAllow(t *testing.T) {
	f := &craftFake{fakeClient: &fakeClient{state: &game.State{}}, affordable: 12}
	d, out := newCraftDispatch(f)
	if err := d.Run(context.Background(), []string{"craft", "distill_moonshine", "60"}); err != nil {
		t.Fatalf("craft should succeed at a smaller batch, got: %v", err)
	}
	want := []int{60, 30, 15, 10}
	if len(f.qtys) != len(want) {
		t.Fatalf("attempted quantities = %v, want %v", f.qtys, want)
	}
	for i := range want {
		if f.qtys[i] != want[i] {
			t.Fatalf("attempted quantities = %v, want %v", f.qtys, want)
		}
	}
	if !bytes.Contains(out.Bytes(), []byte("craft: distill_moonshine 60 refused (not enough materials); crafted 10")) {
		t.Errorf("shrink was not reported: %q", out.String())
	}
}

func TestCraftBelowFloorStillFails(t *testing.T) {
	f := &craftFake{fakeClient: &fakeClient{state: &game.State{}}, affordable: 4}
	d, _ := newCraftDispatch(f)
	err := d.Run(context.Background(), []string{"craft", "distill_moonshine", "60"})
	if err == nil {
		t.Fatal("craft with inputs below the floor must still fail")
	}
	if f.qtys[len(f.qtys)-1] != craftMinQuantity {
		t.Fatalf("last attempt = %d, want the floor %d (attempts %v)", f.qtys[len(f.qtys)-1], craftMinQuantity, f.qtys)
	}
}

func TestCraftInvalidPresetMakesNoAttempt(t *testing.T) {
	f := &craftFake{fakeClient: &fakeClient{state: &game.State{}}, affordable: 1000}
	d, _ := newCraftDispatch(f)
	if err := d.Run(context.Background(), []string{"craft", "distill_moonshine", "60", "bogus"}); err == nil {
		t.Fatal("invalid preset must fail")
	}
	if len(f.qtys) != 0 {
		t.Fatalf("no craft attempt expected on an invalid preset, got %v", f.qtys)
	}
}

func TestCraftRequestAtOrBelowFloorIsSingleAttempt(t *testing.T) {
	f := &craftFake{fakeClient: &fakeClient{state: &game.State{}}, affordable: 0}
	d, _ := newCraftDispatch(f)
	if err := d.Run(context.Background(), []string{"craft", "distill_moonshine", "8", "cheap"}); err == nil {
		t.Fatal("expected failure")
	}
	if len(f.qtys) != 1 || f.qtys[0] != 8 || f.presets[0] != "cheap" {
		t.Fatalf("attempts = %v presets = %v, want one attempt of 8 with preset cheap", f.qtys, f.presets)
	}
}
