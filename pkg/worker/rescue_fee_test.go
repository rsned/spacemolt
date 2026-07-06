package worker

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/rescue"
)

type fakeGiftClient struct {
	state *game.State
	gifts []map[string]any
	err   error
}

func (f *fakeGiftClient) GetState() *game.State { return f.state }
func (f *fakeGiftClient) SendGift(_ context.Context, payload map[string]any) error {
	if f.err != nil {
		return f.err
	}
	f.gifts = append(f.gifts, payload)
	return nil
}

func TestPayRescueDebt(t *testing.T) {
	ctx := context.Background()

	// Docked + one debt -> gift sent with recipient+credits, debt removed.
	t.Run("docked pays and clears", func(t *testing.T) {
		dir := t.TempDir()
		if err := rescue.AppendDebt(dir, "salvager-10", rescue.Debt{Recipient: "shipside_assist_haven", Credits: 1000}); err != nil {
			t.Fatal(err)
		}
		c := &fakeGiftClient{state: &game.State{Doc: true, Credits: 5000}}
		PayRescueDebt(ctx, c, io.Discard, dir, "salvager-10")
		if len(c.gifts) != 1 || c.gifts[0]["recipient"] != "shipside_assist_haven" || c.gifts[0]["credits"] != 1000 {
			t.Fatalf("gifts = %+v, want one gift of 1000 to shipside_assist_haven", c.gifts)
		}
		if debts, _ := rescue.LoadDebts(dir, "salvager-10"); len(debts) != 0 {
			t.Fatalf("debt must be cleared, got %+v", debts)
		}
	})

	// Undocked -> no gift, debt retained (send_gift credits needs a station).
	t.Run("undocked skips", func(t *testing.T) {
		dir := t.TempDir()
		_ = rescue.AppendDebt(dir, "salvager-10", rescue.Debt{Recipient: "shipside_assist_haven", Credits: 1000})
		c := &fakeGiftClient{state: &game.State{Doc: false}}
		PayRescueDebt(ctx, c, io.Discard, dir, "salvager-10")
		if len(c.gifts) != 0 {
			t.Fatalf("undocked must not gift, got %+v", c.gifts)
		}
		if debts, _ := rescue.LoadDebts(dir, "salvager-10"); len(debts) != 1 {
			t.Fatalf("undocked must retain debt, got %+v", debts)
		}
	})

	// No debts -> no-op.
	t.Run("no debts noop", func(t *testing.T) {
		dir := t.TempDir()
		c := &fakeGiftClient{state: &game.State{Doc: true}}
		PayRescueDebt(ctx, c, io.Discard, dir, "salvager-10")
		if len(c.gifts) != 0 {
			t.Fatalf("no debts must not gift, got %+v", c.gifts)
		}
	})

	// Gift error -> debt retained for next pass.
	t.Run("gift error retains debt", func(t *testing.T) {
		dir := t.TempDir()
		_ = rescue.AppendDebt(dir, "salvager-10", rescue.Debt{Recipient: "shipside_assist_haven", Credits: 1000})
		c := &fakeGiftClient{state: &game.State{Doc: true, Credits: 5000}, err: errors.New("not docked at storage")}
		PayRescueDebt(ctx, c, io.Discard, dir, "salvager-10")
		if len(c.gifts) != 0 {
			t.Fatalf("failed gift must not record a sent gift, got %+v", c.gifts)
		}
		if debts, _ := rescue.LoadDebts(dir, "salvager-10"); len(debts) != 1 {
			t.Fatalf("failed gift must retain debt, got %+v", debts)
		}
	})

	// Insolvent (credits < debt) -> no gift attempt, debt retained until the
	// worker can afford it. Guards the broke-assister infinite-retry loop.
	t.Run("insolvent skips and retains", func(t *testing.T) {
		dir := t.TempDir()
		_ = rescue.AppendDebt(dir, "salvager-10", rescue.Debt{Recipient: "shipside_assist_haven", Credits: 1000})
		c := &fakeGiftClient{state: &game.State{Doc: true, Credits: 300}}
		PayRescueDebt(ctx, c, io.Discard, dir, "salvager-10")
		if len(c.gifts) != 0 {
			t.Fatalf("insolvent worker must not attempt a gift, got %+v", c.gifts)
		}
		if debts, _ := rescue.LoadDebts(dir, "salvager-10"); len(debts) != 1 {
			t.Fatalf("insolvent worker must retain debt, got %+v", debts)
		}
	})
}
