package worker

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

func TestCraftAllowance(t *testing.T) {
	for _, tc := range []struct{ held, qty, want int }{
		{0, 500, 500},
		{99400, 500, 500},
		{99700, 500, 300}, // trimmed to the room left
		{99995, 500, 0},   // room below the minimum batch
		{104210, 500, 0},  // marketbot_010's steel on 2026-11-18
		{100000, 500, 0},
	} {
		if got := craftAllowance(tc.held, tc.qty); got != tc.want {
			t.Errorf("craftAllowance(%d, %d) = %d, want %d", tc.held, tc.qty, got, tc.want)
		}
	}
}

func newHoldDispatch(t *testing.T, held int) (*WorkerDispatch, *craftFake, *bytes.Buffer) {
	t.Helper()
	f := &craftFake{fakeClient: &fakeClient{state: &game.State{}}, affordable: 1 << 30}
	f.raw = map[string][]byte{"storage": fmt.Appendf(nil, `{"items":[{"item_id":"widget","quantity":%d}]}`, held)}
	var out bytes.Buffer
	d := NewWorkerDispatch(f, newCraftTestKB(t), nil, &out)

	return d, f, &out
}

// A scheduled craft whose output already fills storage to the hold must not
// run: live 2026-11-18 the hourly smelting on marketbot_010 had piled up
// 104,210 steel_plate with nothing hauling it away.
func TestCraftHeldAtOutputHold(t *testing.T) {
	d, f, out := newHoldDispatch(t, 104210)
	if err := d.Run(context.Background(), []string{"craft", "make_widget", "500"}); err != nil {
		t.Fatalf("a held craft is not an error, got: %v", err)
	}
	if len(f.qtys) != 0 {
		t.Fatalf("craft ran at the hold: attempts %v", f.qtys)
	}
	if !bytes.Contains(out.Bytes(), []byte("craft: make_widget held: 104210 widget in storage")) {
		t.Errorf("hold was not reported: %q", out.String())
	}
}

func TestCraftTrimmedToRoomUnderHold(t *testing.T) {
	d, f, _ := newHoldDispatch(t, 99700)
	if err := d.Run(context.Background(), []string{"craft", "make_widget", "500"}); err != nil {
		t.Fatalf("craft: %v", err)
	}
	if len(f.qtys) != 1 || f.qtys[0] != 300 {
		t.Fatalf("attempts = %v, want [300]", f.qtys)
	}
}

func TestCraftBelowHoldRunsAsAsked(t *testing.T) {
	d, f, _ := newHoldDispatch(t, 1200)
	if err := d.Run(context.Background(), []string{"craft", "make_widget", "500"}); err != nil {
		t.Fatalf("craft: %v", err)
	}
	if len(f.qtys) != 1 || f.qtys[0] != 500 {
		t.Fatalf("attempts = %v, want [500]", f.qtys)
	}
}
