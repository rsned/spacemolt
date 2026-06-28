package worker

import (
	"context"
	"fmt"
	"io"
	"math"
	"sync"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// Faction treasury policy. Mobile roles deposit a slice of each completed job's
// realized profit into their faction's shared treasury, building a buffer that
// rescues members who run themselves broke. Deposits are open to any member;
// withdrawals require the manage_treasury permission (the server rejects them
// otherwise, which we treat as a harmless no-op so un-permissioned agents simply
// idle until rescued by a human or granted the permission).
const (
	// TreasuryProfitCut is the share of a job's realized profit deposited into the
	// faction treasury on completion.
	TreasuryProfitCut = 0.05
	// treasuryRescueFloor is the wallet balance below which an idle, faction-member
	// worker pulls a one-shot injection from the treasury.
	treasuryRescueFloor = 1000.0
	// treasuryRescueAmount is the size of one rescue withdrawal.
	treasuryRescueAmount = 5000.0
	// treasuryRescueCooldown bounds how often a single worker may pull a rescue,
	// so a persistently-broke agent cannot drain the shared fund.
	treasuryRescueCooldown = time.Hour
)

// treasuryClient is the slice of game.GameClient the treasury helpers need. The
// narrow surface keeps the deposit/withdraw logic unit-testable without a full
// client mock; *game.Client (and game.GameClient) satisfy it directly.
type treasuryClient interface {
	GetState() *game.State
	FactionDepositCredits(ctx context.Context, amount float64) error
	FactionWithdrawCredits(ctx context.Context, amount float64) error
}

// depositProfitShare deposits TreasuryProfitCut of profit into the worker's
// faction treasury. It no-ops when the worker is not in a faction or the rounded
// cut is below 1 credit (the server's minimum). Best-effort: a deposit failure is
// logged, never fatal — the job already completed.
func depositProfitShare(ctx context.Context, c treasuryClient, out io.Writer, profit float64) {
	if out == nil {
		out = io.Discard
	}
	if profit <= 0 {
		return
	}
	st := c.GetState()
	if st == nil || st.Player.FactionID == "" {
		return
	}
	cut := math.Floor(profit * TreasuryProfitCut)
	if cut < 1 {
		return
	}
	if err := c.FactionDepositCredits(ctx, cut); err != nil {
		fmt.Fprintf(out, "treasury: deposit %.0f failed: %v\n", cut, err) //nolint:errcheck
		return
	}
	fmt.Fprintf(out, "treasury: deposited %.0f (%.0f%% of %.0f profit)\n", cut, TreasuryProfitCut*100, profit) //nolint:errcheck
}

// treasuryRescue rate-limits a worker's treasury withdrawals so a chronically
// broke agent cannot empty the shared fund. One instance is held per worker
// process (one agent), threaded into the mobile roles via their deps.
type treasuryRescue struct {
	mu   sync.Mutex
	last time.Time
}

// maybe pulls a fixed injection from the faction treasury when the worker is an
// idle faction member that has fallen below treasuryRescueFloor and the cooldown
// has elapsed. Call it only when the worker is between jobs with an empty hold —
// mid-trade a low balance is just capital tied up in cargo, not genuine fumes.
//
// The cooldown stamp advances on every attempt, success or failure, so a worker
// lacking manage_treasury (whose withdraw the server rejects) retries at most
// once per cooldown instead of hammering the endpoint each idle pass.
func (r *treasuryRescue) maybe(ctx context.Context, c treasuryClient, out io.Writer, now time.Time) {
	if r == nil {
		return
	}
	if out == nil {
		out = io.Discard
	}
	st := c.GetState()
	if st == nil || st.Player.FactionID == "" || st.Credits >= treasuryRescueFloor {
		return
	}
	r.mu.Lock()
	if !r.last.IsZero() && now.Sub(r.last) < treasuryRescueCooldown {
		r.mu.Unlock()
		return
	}
	r.last = now
	r.mu.Unlock()

	if err := c.FactionWithdrawCredits(ctx, treasuryRescueAmount); err != nil {
		fmt.Fprintf(out, "treasury: rescue withdraw %.0f failed (manage_treasury permission?): %v\n", treasuryRescueAmount, err) //nolint:errcheck
		return
	}
	fmt.Fprintf(out, "treasury: rescued %.0f credits (wallet was %.0f, below %.0f floor)\n", treasuryRescueAmount, st.Credits, treasuryRescueFloor) //nolint:errcheck
}
