package worker

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

// v0.721.0: an armed non-combat ship opens a fight it did not start at fire
// unless its default stance is flee. Set it once at worker start.
func TestEnsureDefaultStanceFleeSetsItOnce(t *testing.T) {
	f := &fleeFake{}
	ship := &game.Ship{} // omitted = auto
	if err := ensureDefaultStanceFlee(context.Background(), io.Discard, ship, f); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 || f.calls[0] != "default_stance flee" {
		t.Fatalf("calls = %v, want one default_stance flee", f.calls)
	}
	if ship.DefaultStance != "flee" {
		t.Errorf("ship default stance = %q, want flee recorded", ship.DefaultStance)
	}
}

func TestEnsureDefaultStanceFleeSkips(t *testing.T) {
	f := &fleeFake{}
	if err := ensureDefaultStanceFlee(context.Background(), io.Discard, &game.Ship{DefaultStance: "flee"}, f); err != nil || len(f.calls) != 0 {
		t.Errorf("already flee: err %v, calls %v; want nothing sent", err, f.calls)
	}
	// A nil client (the caller opted out) sends nothing.
	if err := ensureDefaultStanceFlee(context.Background(), io.Discard, &game.Ship{}, nil); err != nil || len(f.calls) != 0 {
		t.Errorf("nil client: err %v, calls %v; want nothing sent", err, f.calls)
	}
}

func TestEnsureDefaultStanceFleeReportsFailure(t *testing.T) {
	f := &fleeFake{err: errors.New("boom")}
	if err := ensureDefaultStanceFlee(context.Background(), io.Discard, &game.Ship{}, f); err == nil {
		t.Error("want the refusal returned")
	}
}
