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

// assistCreditFloor is the balance an assist agent is kept at: below it the
// assist files a request and the banker tops it back up to the floor. Assists
// buy fuel for strandees and must never be the next ship to strand
// (operator, 2026-11-12).
const assistCreditFloor = 50000

// creditThresholdFor is the balance below which a worker of role may file.
func creditThresholdFor(role string) float64 {
	if role == "assist" {
		return assistCreditFloor
	}
	return creditRequestThreshold
}

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
	if credits >= creditThresholdFor(c.Role) {
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

// CreditRequester is implemented by a client that can file a credit request
// on demand (CreditRequestingClient). Roles assert for it, so a client without
// the credit-request channel simply never files.
type CreditRequester interface {
	RequestCredits(ctx context.Context, reason string)
}

// RequestCredits files a request when the agent is docked and below
// creditRequestThreshold, under the same rules as the refuel-failure trigger
// (at most one open record per agent). For a role that is broke but has no
// failed refuel to report: a hauler idling docked with a full tank.
func (c *CreditRequestingClient) RequestCredits(ctx context.Context, reason string) {
	c.fileRequest(ctx, reason)
}
