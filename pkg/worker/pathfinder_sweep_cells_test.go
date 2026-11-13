package worker

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/handoff"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/sweep"
)

func (c *sweepFakeClient) WithdrawItems(_ context.Context, itemID string, qty float64) error {
	c.calls = append(c.calls, fmt.Sprintf("withdraw %s %.0f", itemID, qty))
	return nil
}

func doneSweepAtDepot(t *testing.T, rec handoff.Record) (*sweepFakeClient, *WorkerDispatch, *handoff.Queue) {
	t.Helper()
	c := &sweepFakeClient{state: &game.State{
		Doc:    true,
		Player: game.Player{DockedAtBase: "ehr-base"},
		Ship:   game.Ship{CargoCapacity: 165, CargoUsed: 20},
	}}
	s := sweep.NewState(7900, 148, 45)
	s.Phase = sweep.PhaseDone
	d, _ := newSweepDispatch(t, c, knowledge.NewMemoryKB(), s)
	q := handoff.NewQueue(filepath.Join(t.TempDir(), "q.json"))
	if _, err := q.Enqueue(rec); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Transition(rec.ID, handoff.StatusPending, handoff.StatusDone, func(r *handoff.Record) { r.MovedQty = rec.Qty }); err != nil {
		t.Fatal(err)
	}
	d.Handoff = q
	return c, d, q
}

func TestSweepDoneCollectsGiftedCells(t *testing.T) {
	rec := handoff.Record{ID: "sweepcells/explorer-3/x", Holder: "marketbot_010", Station: "ehr-base", ItemID: "fuel_cell", Qty: 40, Recipient: "explorer-3"}
	c, d, q := doneSweepAtDepot(t, rec)
	if err := d.PathfinderSweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(c.calls, "withdraw fuel_cell 40") {
		t.Fatalf("calls %v, want withdraw fuel_cell 40", c.calls)
	}
	recs, _ := q.List()
	if recs[0].Status != handoff.StatusCollected {
		t.Errorf("record status %q, want collected", recs[0].Status)
	}
}

func TestSweepDoneIgnoresOtherGifts(t *testing.T) {
	for name, rec := range map[string]handoff.Record{
		"other recipient": {ID: "sweepcells/explorer-9/x", Station: "ehr-base", ItemID: "fuel_cell", Qty: 40, Recipient: "explorer-9"},
		"other station":   {ID: "sweepcells/explorer-3/x", Station: "cj-base", ItemID: "fuel_cell", Qty: 40, Recipient: "explorer-3"},
		"not ours":        {ID: "consign/1/explorer-3", Station: "ehr-base", ItemID: "fuel_cell", Qty: 40, Recipient: "explorer-3"},
	} {
		t.Run(name, func(t *testing.T) {
			c, d, _ := doneSweepAtDepot(t, rec)
			if err := d.PathfinderSweep(context.Background()); err != nil {
				t.Fatal(err)
			}
			if slices.ContainsFunc(c.calls, func(s string) bool { return len(s) > 8 && s[:8] == "withdraw" }) {
				t.Fatalf("calls %v, want no withdraw", c.calls)
			}
		})
	}
}
