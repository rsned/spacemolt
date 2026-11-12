package worker

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/creditreq"
	"github.com/rsned/spacemolt/pkg/game"
)

// creditRequestThreshold is the credit balance below which a failed station
// refuel counts as "cannot pay" and is worth a request to the banker.
const creditRequestThreshold = 5000

// CreditFiler is the slice of creditreq.Queue the decorator needs.
type CreditFiler interface {
	File(creditreq.Record) (bool, error)
}

// CreditRequestingClient decorates a game.GameClient so that a station refuel
// that fails because the agent cannot pay files a credit request for the
// banker. Filing is best-effort and never changes what Refuel returns.
type CreditRequestingClient struct {
	game.GameClient
	Queue    CreditFiler
	History  *creditreq.History
	AgentID  string
	Username string
	Role     string // --role, recorded so the banker can size the payment
	Out      io.Writer
}

// Refuel calls the embedded Refuel and, when it failed for lack of credits
// while docked and broke, files a credit request. The original error is
// always returned unchanged.
func (c *CreditRequestingClient) Refuel(ctx context.Context) error {
	err := c.GameClient.Refuel(ctx)
	if err == nil {
		return nil
	}
	msg := err.Error()
	if !strings.Contains(msg, "no_fuel_source") || !strings.Contains(msg, "insufficient credits") {
		return err
	}
	c.fileRequest(ctx, msg)
	return err
}

func (c *CreditRequestingClient) fileRequest(ctx context.Context, reason string) {
	out := c.Out
	if out == nil {
		out = io.Discard
	}
	if st := c.GetState(); st == nil || !st.IsDocked() {
		return
	}
	if err := c.GetStatus(ctx); err != nil {
		return
	}
	st := c.GetState()
	if st == nil {
		return
	}
	st.Mu.Lock()
	credits, fuel, maxFuel := st.Credits, st.Fuel, st.MaxFuel
	systemID, baseID := st.CurrentSystem, st.Player.DockedAtBase
	st.Mu.Unlock()
	if credits >= creditRequestThreshold {
		return
	}
	now := time.Now().UTC()
	filed, ferr := c.Queue.File(creditreq.Record{
		AgentID:     c.AgentID,
		Username:    c.Username,
		Role:        c.Role,
		BaseID:      baseID,
		SystemID:    systemID,
		Reason:      reason,
		Credits:     credits,
		Fuel:        fuel,
		MaxFuel:     maxFuel,
		Status:      creditreq.StatusPending,
		RequestedAt: now,
	})
	if ferr != nil {
		fmt.Fprintf(out, "  credit request: file failed: %v\n", ferr) //nolint:errcheck
		return
	}
	if !filed {
		return
	}
	fmt.Fprintf(out, "  credit request filed (credits %.0f)\n", credits) //nolint:errcheck
	if c.History == nil {
		return
	}
	if herr := c.History.Append(creditreq.Entry{
		At:       now,
		AgentID:  c.AgentID,
		Username: c.Username,
		Event:    creditreq.EventFiled,
		Detail:   reason,
	}); herr != nil {
		fmt.Fprintf(out, "  credit request: history append failed: %v\n", herr) //nolint:errcheck
	}
}
