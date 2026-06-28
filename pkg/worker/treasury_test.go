package worker

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// fakeTreasuryClient implements the narrow treasuryClient surface for tests.
type fakeTreasuryClient struct {
	state       *game.State
	deposited   []float64
	withdrew    []float64
	withdrawErr error
}

func (f *fakeTreasuryClient) GetState() *game.State { return f.state }
func (f *fakeTreasuryClient) FactionDepositCredits(_ context.Context, amount float64) error {
	f.deposited = append(f.deposited, amount)
	return nil
}
func (f *fakeTreasuryClient) FactionWithdrawCredits(_ context.Context, amount float64) error {
	f.withdrew = append(f.withdrew, amount)
	return f.withdrawErr
}

func memberState(credits float64) *game.State {
	st := &game.State{Credits: credits}
	st.Player.FactionID = "fac-1"
	return st
}

func TestDepositProfitShare(t *testing.T) {
	ctx := context.Background()

	// In a faction with real profit: deposit floor(5%).
	c := &fakeTreasuryClient{state: memberState(50000)}
	depositProfitShare(ctx, c, io.Discard, 10000)
	if len(c.deposited) != 1 || c.deposited[0] != 500 {
		t.Fatalf("deposited = %v, want [500]", c.deposited)
	}

	// Profit too small to round to >=1 credit: no deposit.
	c = &fakeTreasuryClient{state: memberState(50000)}
	depositProfitShare(ctx, c, io.Discard, 10) // 5% = 0.5 -> floor 0
	if len(c.deposited) != 0 {
		t.Fatalf("tiny profit deposited %v, want none", c.deposited)
	}

	// Not in a faction: no deposit.
	c = &fakeTreasuryClient{state: &game.State{Credits: 50000}}
	depositProfitShare(ctx, c, io.Discard, 10000)
	if len(c.deposited) != 0 {
		t.Fatalf("non-member deposited %v, want none", c.deposited)
	}

	// Zero / negative profit: no deposit.
	c = &fakeTreasuryClient{state: memberState(50000)}
	depositProfitShare(ctx, c, io.Discard, 0)
	depositProfitShare(ctx, c, io.Discard, -500)
	if len(c.deposited) != 0 {
		t.Fatalf("non-positive profit deposited %v, want none", c.deposited)
	}
}

func TestTreasuryRescue(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(1_000_000, 0)

	// Broke faction member, cooldown clear: pulls one injection.
	c := &fakeTreasuryClient{state: memberState(400)}
	r := &treasuryRescue{}
	r.maybe(ctx, c, io.Discard, now)
	if len(c.withdrew) != 1 || c.withdrew[0] != treasuryRescueAmount {
		t.Fatalf("withdrew = %v, want [%v]", c.withdrew, treasuryRescueAmount)
	}

	// Second call within the cooldown window: no further withdrawal.
	r.maybe(ctx, c, io.Discard, now.Add(treasuryRescueCooldown-time.Minute))
	if len(c.withdrew) != 1 {
		t.Fatalf("withdrew within cooldown = %v, want still 1", c.withdrew)
	}

	// After the cooldown elapses: a fresh injection is allowed.
	r.maybe(ctx, c, io.Discard, now.Add(treasuryRescueCooldown+time.Second))
	if len(c.withdrew) != 2 {
		t.Fatalf("withdrew after cooldown = %v, want 2", c.withdrew)
	}

	// Above the floor: no rescue.
	c = &fakeTreasuryClient{state: memberState(treasuryRescueFloor + 1)}
	(&treasuryRescue{}).maybe(ctx, c, io.Discard, now)
	if len(c.withdrew) != 0 {
		t.Fatalf("solvent worker withdrew %v, want none", c.withdrew)
	}

	// Not in a faction: no rescue even when broke.
	c = &fakeTreasuryClient{state: &game.State{Credits: 100}}
	(&treasuryRescue{}).maybe(ctx, c, io.Discard, now)
	if len(c.withdrew) != 0 {
		t.Fatalf("non-member withdrew %v, want none", c.withdrew)
	}

	// Failed withdraw (e.g. missing manage_treasury) still advances the cooldown,
	// so the worker does not hammer the endpoint every idle pass.
	c = &fakeTreasuryClient{state: memberState(400), withdrawErr: errors.New("permission denied")}
	r = &treasuryRescue{}
	r.maybe(ctx, c, io.Discard, now)
	r.maybe(ctx, c, io.Discard, now.Add(time.Minute))
	if len(c.withdrew) != 1 {
		t.Fatalf("attempts after failed withdraw = %d, want 1 (cooldown holds)", len(c.withdrew))
	}
}
