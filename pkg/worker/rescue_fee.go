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

// PayRescueDebt pays the head outstanding rescue debt for agentID when the
// worker is docked (send_gift credits requires a base with storage). One debt
// per call respects the 1-gift-per-tick rate limit; the rest wait for the next
// docked pass. Best-effort: every failure logs and leaves the debt in place.
func PayRescueDebt(ctx context.Context, c GiftClient, out io.Writer, agentsDir, agentID string) {
	debts, err := rescue.LoadDebts(agentsDir, agentID)
	if err != nil {
		fmt.Fprintf(out, "rescue-fee: load debts: %v\n", err) //nolint:errcheck
		return
	}
	if len(debts) == 0 {
		return
	}
	st := c.GetState()
	if st == nil || !st.Doc {
		return // pay on a later pass once docked
	}
	d := debts[0]
	payload := map[string]any{
		"recipient": d.Recipient,
		"credits":   d.Credits,
		"message":   "rescue fuel reimbursement",
	}
	if err := c.SendGift(ctx, payload); err != nil {
		fmt.Fprintf(out, "rescue-fee: gift %d to %s: %v (retrying next pass)\n", d.Credits, d.Recipient, err) //nolint:errcheck
		return
	}
	if err := rescue.RemoveHead(agentsDir, agentID); err != nil {
		fmt.Fprintf(out, "rescue-fee: clear paid debt: %v\n", err) //nolint:errcheck
	}
	fmt.Fprintf(out, "rescue-fee: paid %d cr to %s\n", d.Credits, d.Recipient) //nolint:errcheck
}
