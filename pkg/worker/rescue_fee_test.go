package worker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
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
		NewDebtPayer(c, io.Discard, dir, "salvager-10").Pay(ctx)
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
		c := &fakeGiftClient{state: &game.State{Doc: false, Credits: 5000}}
		NewDebtPayer(c, io.Discard, dir, "salvager-10").Pay(ctx)
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
		NewDebtPayer(c, io.Discard, dir, "salvager-10").Pay(ctx)
		if len(c.gifts) != 0 {
			t.Fatalf("no debts must not gift, got %+v", c.gifts)
		}
	})

	// Gift error -> debt retained for next pass.
	t.Run("gift error retains debt", func(t *testing.T) {
		dir := t.TempDir()
		_ = rescue.AppendDebt(dir, "salvager-10", rescue.Debt{Recipient: "shipside_assist_haven", Credits: 1000})
		c := &fakeGiftClient{state: &game.State{Doc: true, Credits: 5000}, err: errors.New("not docked at storage")}
		NewDebtPayer(c, io.Discard, dir, "salvager-10").Pay(ctx)
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
		NewDebtPayer(c, io.Discard, dir, "salvager-10").Pay(ctx)
		if len(c.gifts) != 0 {
			t.Fatalf("insolvent worker must not attempt a gift, got %+v", c.gifts)
		}
		if debts, _ := rescue.LoadDebts(dir, "salvager-10"); len(debts) != 1 {
			t.Fatalf("insolvent worker must retain debt, got %+v", debts)
		}
	})
}

func TestDebtPayerAnnouncesOncePerSession(t *testing.T) {
	ctx := context.Background()

	// The whole point: the idle loop calls Pay every few seconds, so an
	// unpayable debt must produce exactly one line, not one per pass.
	t.Run("insolvent announces once across many passes", func(t *testing.T) {
		dir := t.TempDir()
		_ = rescue.AppendDebt(dir, "salvager-10", rescue.Debt{Recipient: "shipside_assist_haven", Credits: 1000})
		_ = rescue.AppendDebt(dir, "salvager-10", rescue.Debt{Recipient: "shipside_assist_sol", Credits: 500})
		var buf bytes.Buffer
		c := &fakeGiftClient{state: &game.State{Doc: true, Credits: 300}}
		p := NewDebtPayer(c, &buf, dir, "salvager-10")
		for range 20 {
			p.Pay(ctx)
		}
		got := strings.Count(buf.String(), "rescue-fee: cannot pay")
		if got != 1 {
			t.Fatalf("announcements = %d, want exactly 1\nlog:\n%s", got, buf.String())
		}
		// The one line must carry enough to act on: debt count, total owed,
		// credits on hand, and the fee that is blocked.
		for _, want := range []string{"2 debt(s)", "1500 cr", "holding 300 cr", "next fee 1000 cr", "shipside_assist_haven"} {
			if !strings.Contains(buf.String(), want) {
				t.Errorf("announcement missing %q, got:\n%s", want, buf.String())
			}
		}
	})

	// An undocked insolvent worker still announces, so a worker that starts
	// broke in transit explains itself without waiting to dock.
	t.Run("announces while undocked", func(t *testing.T) {
		dir := t.TempDir()
		_ = rescue.AppendDebt(dir, "salvager-10", rescue.Debt{Recipient: "shipside_assist_haven", Credits: 1000})
		var buf bytes.Buffer
		c := &fakeGiftClient{state: &game.State{Doc: false, Credits: 300}}
		p := NewDebtPayer(c, &buf, dir, "salvager-10")
		p.Pay(ctx)
		p.Pay(ctx)
		if got := strings.Count(buf.String(), "rescue-fee: cannot pay"); got != 1 {
			t.Fatalf("announcements = %d, want exactly 1\nlog:\n%s", got, buf.String())
		}
	})

	// A solvent worker in transit says nothing — it will simply pay on docking.
	t.Run("solvent undocked stays quiet", func(t *testing.T) {
		dir := t.TempDir()
		_ = rescue.AppendDebt(dir, "salvager-10", rescue.Debt{Recipient: "shipside_assist_haven", Credits: 1000})
		var buf bytes.Buffer
		c := &fakeGiftClient{state: &game.State{Doc: false, Credits: 5000}}
		p := NewDebtPayer(c, &buf, dir, "salvager-10")
		p.Pay(ctx)
		p.Pay(ctx)
		if buf.Len() != 0 {
			t.Fatalf("solvent worker in transit must stay quiet, got:\n%s", buf.String())
		}
	})

	// Repeated send failures collapse to one line too — same log-spam problem,
	// same suppression.
	t.Run("repeated gift errors announce once", func(t *testing.T) {
		dir := t.TempDir()
		_ = rescue.AppendDebt(dir, "salvager-10", rescue.Debt{Recipient: "shipside_assist_haven", Credits: 1000})
		var buf bytes.Buffer
		c := &fakeGiftClient{state: &game.State{Doc: true, Credits: 5000}, err: errors.New("not docked at storage")}
		p := NewDebtPayer(c, &buf, dir, "salvager-10")
		for range 10 {
			p.Pay(ctx)
		}
		if got := strings.Count(buf.String(), "rescue-fee: gift"); got != 1 {
			t.Fatalf("gift-error lines = %d, want exactly 1\nlog:\n%s", got, buf.String())
		}
	})

	// Suppression must not be permanent: once funded the worker pays, and a
	// later relapse into insolvency announces again.
	t.Run("re-arms after paying", func(t *testing.T) {
		dir := t.TempDir()
		_ = rescue.AppendDebt(dir, "salvager-10", rescue.Debt{Recipient: "shipside_assist_haven", Credits: 1000})
		var buf bytes.Buffer
		c := &fakeGiftClient{state: &game.State{Doc: true, Credits: 300}}
		p := NewDebtPayer(c, &buf, dir, "salvager-10")
		p.Pay(ctx) // announces the stall
		c.state.Credits = 5000
		p.Pay(ctx) // funded: pays and clears
		if len(c.gifts) != 1 {
			t.Fatalf("funded worker must pay, gifts = %+v", c.gifts)
		}
		// New debt, broke again -> a second announcement, not silence.
		_ = rescue.AppendDebt(dir, "salvager-10", rescue.Debt{Recipient: "shipside_assist_sol", Credits: 2000})
		c.state.Credits = 100
		p.Pay(ctx)
		if got := strings.Count(buf.String(), "rescue-fee: cannot pay"); got != 2 {
			t.Fatalf("announcements = %d, want 2 (stall, pay, relapse)\nlog:\n%s", got, buf.String())
		}
	})
}
