package worker

import (
	"context"
	"fmt"
	"io"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/rescue"
)

// GiftClient is the slice of game.GameClient the rescue-fee payment needs.
type GiftClient interface {
	GetState() *game.State
	SendGift(ctx context.Context, payload map[string]any) error
}

// DebtPayer pays a worker's outstanding rescue debts, one per docked pass, and
// announces a debt it cannot pay once per session instead of on every pass.
//
// The idle loop calls Pay every few seconds, so any per-pass logging here would
// bury the overmind log. Instead the payer remembers its last announcement and
// stays quiet until the situation actually changes — a worker that is broke at
// startup logs one line explaining why its debts are not clearing, and nothing
// more until it pays, is funded, or the shortfall changes.
//
// Not safe for concurrent use: RunStanding calls Pay from a single loop under
// ExecMu.
type DebtPayer struct {
	client    GiftClient
	out       io.Writer
	agentsDir string
	agentID   string

	// announced is the last line emitted about an unpayable debt. It suppresses
	// identical repeats and re-arms when the message would differ, so a relapse
	// into insolvency is reported rather than swallowed.
	announced string
}

// NewDebtPayer returns a payer for agentID's debts under agentsDir.
func NewDebtPayer(c GiftClient, out io.Writer, agentsDir, agentID string) *DebtPayer {
	return &DebtPayer{client: c, out: out, agentsDir: agentsDir, agentID: agentID}
}

// announce writes msg unless it is the announcement already showing.
func (p *DebtPayer) announce(msg string) {
	if p.announced == msg {
		return
	}
	p.announced = msg
	fmt.Fprintf(p.out, "%s\n", msg) //nolint:errcheck
}

// Pay settles the head outstanding rescue debt when the worker is docked
// (send_gift credits requires a base with storage) and can afford it. One debt
// per call respects the 1-gift-per-tick rate limit; the rest wait for the next
// docked pass. Best-effort: every failure leaves the debt in place.
func (p *DebtPayer) Pay(ctx context.Context) {
	debts, err := rescue.LoadDebts(p.agentsDir, p.agentID)
	if err != nil {
		p.announce(fmt.Sprintf("rescue-fee: load debts: %v", err))
		return
	}
	if len(debts) == 0 {
		p.announced = "" // nothing owed; a future debt announces afresh
		return
	}
	st := p.client.GetState()
	if st == nil {
		return
	}
	d := debts[0]
	// Solvency gate: an insolvent debtor (notably a broke assister rescued by
	// another assister) would otherwise fail send_gift and retry the same debt
	// every docked pass forever. Announce the stall once, then wait for funds.
	if st.Credits < float64(d.Credits) {
		p.announce(fmt.Sprintf(
			"rescue-fee: cannot pay: %d debt(s) totalling %d cr, holding %.0f cr, next fee %d cr to %s (waiting for funds)",
			len(debts), totalDebt(debts), st.Credits, d.Credits, d.Recipient))
		return
	}
	if !st.Doc {
		return // pay on a later pass once docked
	}
	payload := map[string]any{
		"recipient": d.Recipient,
		"credits":   d.Credits,
		"message":   "rescue fuel reimbursement",
	}
	if err := p.client.SendGift(ctx, payload); err != nil {
		p.announce(fmt.Sprintf("rescue-fee: gift %d to %s: %v (retrying next pass)", d.Credits, d.Recipient, err))
		return
	}
	p.announced = "" // a payment landed; re-arm for the next stall
	if err := rescue.RemoveHead(p.agentsDir, p.agentID); err != nil {
		fmt.Fprintf(p.out, "rescue-fee: clear paid debt: %v\n", err) //nolint:errcheck
	}
	fmt.Fprintf(p.out, "rescue-fee: paid %d cr to %s\n", d.Credits, d.Recipient) //nolint:errcheck
}

// totalDebt sums the credits owed across every outstanding debt.
func totalDebt(debts []rescue.Debt) int {
	var total int
	for _, d := range debts {
		total += d.Credits
	}
	return total
}
