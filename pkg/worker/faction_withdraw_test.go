package worker

import (
	"bytes"
	"context"
	"io"
	"testing"
)

const factionCellsReply = `{"base_id":"b1","items":[
 {"item_id":"fuel_cell","name":"Fuel Cell","quantity":356,"size":1}]}`

func TestFactionWithdrawMovesQtyAcrossHoldTrips(t *testing.T) {
	fc := newFactionPullFake(150, factionCellsReply)
	var out bytes.Buffer
	moved, err := FactionWithdraw(context.Background(), fc, "fuel_cell", 300, &out)
	if err != nil || moved != 300 {
		t.Fatalf("moved=%d err=%v, want 300 nil", moved, err)
	}
	if fc.withdrawn["fuel_cell"] != 300 || fc.deposited["fuel_cell"] != 300 {
		t.Errorf("withdrawn=%v deposited=%v, want 300 each", fc.withdrawn, fc.deposited)
	}
}

func TestFactionWithdrawStopsAtFactionStock(t *testing.T) {
	fc := newFactionPullFake(150, factionCellsReply)
	var out bytes.Buffer
	moved, err := FactionWithdraw(context.Background(), fc, "fuel_cell", 1000, &out)
	if err != nil || moved != 356 {
		t.Fatalf("moved=%d err=%v, want 356 nil", moved, err)
	}
}

func TestFactionWithdrawMissingItemMovesNothing(t *testing.T) {
	fc := newFactionPullFake(150, factionReply)
	var out bytes.Buffer
	moved, err := FactionWithdraw(context.Background(), fc, "fuel_cell", 10, &out)
	if err != nil || moved != 0 {
		t.Fatalf("moved=%d err=%v, want 0 nil", moved, err)
	}
	if len(fc.withdrawn) != 0 {
		t.Errorf("withdrew %v from an empty stock", fc.withdrawn)
	}
}

func TestDispatchFactionWithdraw(t *testing.T) {
	if !supported["faction_withdraw"] {
		t.Error(`"faction_withdraw" missing from the supported command set`)
	}
	fc := newFactionPullFake(150, factionCellsReply)
	d := &WorkerDispatch{Client: fc, Out: io.Discard}
	if err := d.Run(context.Background(), []string{"faction_withdraw", "fuel_cell", "200"}); err != nil {
		t.Fatal(err)
	}
	if fc.deposited["fuel_cell"] != 200 {
		t.Fatalf("deposited %v, want 200 fuel_cell", fc.deposited)
	}
	if err := d.Run(context.Background(), []string{"faction_withdraw", "fuel_cell"}); err == nil {
		t.Error("missing QTY accepted")
	}
}
