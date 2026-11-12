package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/creditreq"
	"github.com/rsned/spacemolt/pkg/game"
)

// Banker-side limits for pay_credit_requests.
const (
	// creditGiftAmount is the standard (non-hauler) payment. Raised from
	// 20,000 when empire-station fuel ran 10-20 cr/unit and 20,000 covered
	// only a few tanks (operator, 2026-11-12).
	creditGiftAmount = 50000
	// creditHaulerAmount is what a hauler gets: arbitrage is capital
	// intensive, and 20,000 bought loads too small to clear the haul profit
	// floor, so paid truckers sat broke again within hours (operator,
	// 2026-11-11). Same 24h rule and daily cap.
	creditHaulerAmount  = 100000
	creditBankerFloor   = 1_000_000.0
	creditDailyCap      = 1_000_000
	creditPerAgentSpan  = 24 * time.Hour
	creditMaxSendErrors = 3
)

// historyFailedMarker is in the HeldReason of a record whose gift went out but
// whose history entry could not be written. The hold and the run-level halt
// check share it.
const historyFailedMarker = "history write failed"

// ErrPaymentUnrecorded marks a gift that went out but could be recorded
// neither in the history nor as a held queue record, so nothing stops a repeat.
var ErrPaymentUnrecorded = errors.New("pay_credit_requests: a payment is unrecorded")

// ErrPayHalted is returned by a run that refused to pay because an earlier
// payment is unrecorded (a held marker record or the halt file).
var ErrPayHalted = errors.New("pay_credit_requests: halted")

// GiftSender is the slice of game.GameClient the banker needs.
type GiftSender interface {
	SendGift(ctx context.Context, payload map[string]any) error
}

// PayDeps is everything PayCreditRequests touches; it uses no globals.
type PayDeps struct {
	Queue      *creditreq.Queue
	History    *creditreq.History
	Sender     GiftSender
	OwnCredits func() float64
	Usernames  func(agentID string) string
	Now        func() time.Time
	Out        io.Writer
	// HaltPath is a durable sentinel written on an unrecorded payment; while
	// it exists no run pays. Empty disables it.
	HaltPath string
}

// PayCreditRequests pays pending credit requests oldest first, subject to the
// banker floor, the one-payment-per-agent-per-24h rule and the fleet daily cap.
// A payment is written to the history BEFORE the record leaves the queue, so a
// crash between the two can never cause a second gift (the 24h rule sees it).
func PayCreditRequests(ctx context.Context, d PayDeps) (paid, held int, err error) {
	if d.HaltPath != "" {
		if _, serr := os.Stat(d.HaltPath); serr == nil {
			msg := fmt.Sprintf("halted — %s exists; add the missing paid line to credit-requests-history.jsonl, reconcile and delete it", d.HaltPath)
			if d.Out != nil {
				fmt.Fprintf(d.Out, "pay_credit_requests: %s\n", msg) //nolint:errcheck
			}
			return 0, 0, fmt.Errorf("%w: %s", ErrPayHalted, msg)
		}
	}
	recs, err := d.Queue.List()
	if err != nil {
		return 0, 0, err
	}
	var pending []creditreq.Record
	for _, r := range recs {
		if r.Status == creditreq.StatusHeld && strings.Contains(r.HeldReason, historyFailedMarker) {
			if d.Out != nil {
				fmt.Fprintf(d.Out, "pay_credit_requests: halted — a payment is unrecorded (agent %s); fix the history file, add the missing paid line to credit-requests-history.jsonl, then clear the hold\n", r.AgentID) //nolint:errcheck
			}
			return 0, 0, fmt.Errorf("%w: a payment is unrecorded (agent %s)", ErrPayHalted, r.AgentID)
		}
		if r.Status == creditreq.StatusPending {
			pending = append(pending, r)
		}
	}
	if err := releaseExpiredHolds(d, recs); err != nil {
		return 0, 0, err
	}
	sort.SliceStable(pending, func(i, j int) bool { return pending[i].RequestedAt.Before(pending[j].RequestedAt) })

	logf := func(format string, a ...any) {
		if d.Out != nil {
			fmt.Fprintf(d.Out, format+"\n", a...) //nolint:errcheck
		}
	}
	hold := func(r creditreq.Record, username, reason string) error {
		now := d.Now().UTC()
		if _, err := d.Queue.Update(r.AgentID, func(rec *creditreq.Record) {
			rec.Status = creditreq.StatusHeld
			rec.HeldReason = reason
		}); err != nil {
			return err
		}
		held++
		logf("pay_credit_requests: holding %s: %s", r.AgentID, reason)
		return d.History.Append(creditreq.Entry{At: now, AgentID: r.AgentID, Username: username, Event: creditreq.EventHeld, Detail: reason})
	}

	spent := 0.0 // credits gifted this run; the client's balance may lag
	for _, r := range pending {
		if err := ctx.Err(); err != nil {
			return paid, held, err
		}
		if own := d.OwnCredits() - spent; own < creditBankerFloor {
			logf("pay_credit_requests: banker has %.0f credits, below the %.0f floor; paying nothing", own, creditBankerFloor)
			return paid, held, nil
		}
		username := r.Username
		if username == "" && d.Usernames != nil {
			username = d.Usernames(r.AgentID)
		}
		if username == "" {
			if err := hold(r, "", "no username"); err != nil {
				return paid, held, err
			}
			continue
		}

		amount := creditAmountFor(r)
		now := d.Now().UTC()
		midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		// The 24h window always reaches back past UTC midnight, so one read
		// serves both the per-agent rule and the daily cap.
		entries, err := d.History.Since(now.Add(-creditPerAgentSpan))
		if err != nil {
			return paid, held, err
		}
		if !r.Override && creditreq.PaidSince(entries, r.AgentID, now.Add(-creditPerAgentSpan)) >= 1 {
			var last time.Time
			for _, e := range entries {
				if e.Event == creditreq.EventPaid && e.AgentID == r.AgentID && e.At.After(last) {
					last = e.At
				}
			}
			if err := hold(r, username, fmt.Sprintf("paid %s already", last.UTC().Format(time.RFC3339))); err != nil {
				return paid, held, err
			}
			continue
		}
		if creditreq.TotalPaidSince(entries, midnight)+amount > creditDailyCap {
			if err := hold(r, username, "daily cap"); err != nil {
				return paid, held, err
			}
			continue
		}

		gerr := d.Sender.SendGift(ctx, map[string]any{"recipient": username, "credits": int(amount)})
		if gerr != nil {
			var se *game.ServerError
			if !errors.As(gerr, &se) {
				// Timeout, cancel, disconnect, or anything unrecognised: the
				// server may have processed the gift. Never retry on doubt.
				if err := hold(r, username, "send outcome unknown: "+gerr.Error()); err != nil {
					return paid, held, err
				}
				return paid, held, fmt.Errorf("pay_credit_requests: send outcome unknown for %s: %w", r.AgentID, gerr)
			}
			attempts := r.Attempts + 1
			if _, err := d.Queue.Update(r.AgentID, func(rec *creditreq.Record) { rec.Attempts = attempts }); err != nil {
				return paid, held, err
			}
			if err := d.History.Append(creditreq.Entry{At: now, AgentID: r.AgentID, Username: username, Event: creditreq.EventSendFailed, Detail: gerr.Error()}); err != nil {
				return paid, held, err
			}
			logf("pay_credit_requests: gift to %s failed (%d/%d): %v", username, attempts, creditMaxSendErrors, gerr)
			if attempts >= creditMaxSendErrors {
				if err := hold(r, username, "send failed: "+gerr.Error()); err != nil {
					return paid, held, err
				}
			}
			continue
		}

		spent += float64(amount)
		paid++
		// History first: if the Remove below fails, the 24h rule still stops a repeat.
		if herr := d.History.Append(creditreq.Entry{At: now, AgentID: r.AgentID, Username: username, Event: creditreq.EventPaid, Amount: amount}); herr != nil {
			// The gift is out and cannot be undone. Park the record as held so
			// no later run pays it again, and stop: with the history unwritable
			// the 24h and daily-cap checks can no longer be trusted.
			reason := fmt.Sprintf("paid %s but %s: %v — add the missing paid line to credit-requests-history.jsonl before clearing this hold", now.Format(time.RFC3339), historyFailedMarker, herr)
			_, uerr := d.Queue.Update(r.AgentID, func(rec *creditreq.Record) {
				rec.Status = creditreq.StatusHeld
				rec.HeldReason = reason
				rec.Amount = amount
				rec.PaidAt = now
			})
			logf("pay_credit_requests: %s (%s): %s", r.AgentID, username, reason)
			if uerr != nil {
				logf("pay_credit_requests: %s (%s): could not hold the record either: %v", r.AgentID, username, uerr)
				d.writeHaltFile(logf, r.AgentID, username, now)
				return paid, held, fmt.Errorf("%w: %w", ErrPaymentUnrecorded, errors.Join(herr, uerr))
			}
			held++
			return paid, held, herr
		}
		if err := d.Queue.Remove(r.AgentID); err != nil {
			return paid, held, err
		}
		logf("pay_credit_requests: paid %d credits to %s (%s)", amount, username, r.AgentID)
	}
	return paid, held, nil
}

// payCreditRequests is the dispatch glue: the banker's own client pays the
// shared queue, usernames come from the assets ledger's agents table (the same
// agent_id -> username map cmd/tools/banker reads).
func (d *WorkerDispatch) payCreditRequests(ctx context.Context) error {
	if d.CreditQueue == nil || d.CreditHistory == nil {
		return errors.New("pay_credit_requests: credit requests are not enabled (--credit-requests)")
	}
	if d.creditPayHalted {
		fmt.Fprintln(d.Out, "pay_credit_requests: refusing — an earlier payment is unrecorded; restart the worker after reconciling") //nolint:errcheck
		return fmt.Errorf("%w: latched in this process", ErrPayHalted)
	}
	names, nerr := d.loadCreditUsernames(ctx)
	if nerr != nil {
		fmt.Fprintf(d.Out, "pay_credit_requests: username lookup failed: %v\n", nerr) //nolint:errcheck
	}
	paid, held, err := PayCreditRequests(ctx, PayDeps{
		Queue:   d.CreditQueue,
		History: d.CreditHistory,
		Sender:  d.Client,
		OwnCredits: func() float64 {
			if st := d.Client.GetState(); st != nil {
				return st.GetCredits()
			}
			return 0
		},
		Usernames: func(id string) string { return names[id] },
		Now:       time.Now,
		Out:       d.Out,
		HaltPath:  d.CreditQueue.Path() + ".halt",
	})
	if errors.Is(err, ErrPaymentUnrecorded) {
		d.creditPayHalted = true
	}
	fmt.Fprintf(d.Out, "pay_credit_requests: paid %d, held %d\n", paid, held) //nolint:errcheck
	return err
}

// loadCreditUsernames reads agent_id -> username from the assets ledger. A nil
// store or a failed query yields a partial map plus the error, so each record is held
// "no username" rather than the run failing.
func (d *WorkerDispatch) loadCreditUsernames(ctx context.Context) (map[string]string, error) {
	out := map[string]string{}
	if d.Assets == nil {
		return out, nil
	}
	rows, err := d.Assets.DB().QueryContext(ctx, `SELECT agent_id, username FROM agents WHERE username != ''`)
	if err != nil {
		return out, err
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var id, user string
		if err := rows.Scan(&id, &user); err != nil {
			return out, err
		}
		out[id] = user
	}
	return out, rows.Err()
}

// writeHaltFile best-effort records an unrecorded payment in the sentinel file.
func (d PayDeps) writeHaltFile(logf func(string, ...any), agentID, username string, at time.Time) {
	if d.HaltPath == "" {
		return
	}
	body := fmt.Sprintf("agent_id=%s\nusername=%s\nat=%s\n", agentID, username, at.Format(time.RFC3339))
	if err := os.WriteFile(d.HaltPath, []byte(body), 0o644); err != nil {
		logf("pay_credit_requests: could not write halt file %s: %v", d.HaltPath, err)
	}
}

// heldPaidPrefix starts the HeldReason the 24 h rule writes ("paid <RFC3339>
// already").
const heldPaidPrefix = "paid "

// releaseExpiredHolds drops "paid … already" holds whose 24 h window has
// passed. Such a hold was terminal: trucker-07 and 13 others sat held from
// 2026-11-10 with no way to be paid again. The stale record is removed, not
// re-paid -- it describes the agent a day ago -- so an agent still broke files
// a fresh request from where it is now, and only that one is paid.
func releaseExpiredHolds(d PayDeps, recs []creditreq.Record) error {
	now := d.Now().UTC()
	for _, r := range recs {
		if r.Status != creditreq.StatusHeld || !strings.HasPrefix(r.HeldReason, heldPaidPrefix) {
			continue
		}
		ts := strings.TrimSuffix(strings.TrimPrefix(r.HeldReason, heldPaidPrefix), " already")
		paidAt, err := time.Parse(time.RFC3339, ts)
		if err != nil || now.Sub(paidAt) < creditPerAgentSpan {
			continue
		}
		if err := d.Queue.Remove(r.AgentID); err != nil {
			return err
		}
		if d.Out != nil {
			fmt.Fprintf(d.Out, "pay_credit_requests: released %s's hold (last paid %s, over 24h ago); it may file again\n", r.AgentID, ts) //nolint:errcheck
		}
		if err := d.History.Append(creditreq.Entry{At: now, AgentID: r.AgentID, Username: r.Username,
			Event: creditreq.EventReleased, Detail: r.HeldReason}); err != nil {
			return err
		}
	}

	return nil
}

// creditAmountFor is the payment for request r: an operator override pays its
// own Amount, a hauler gets creditHaulerAmount, an assist is topped up to
// assistCreditFloor from the balance it filed with, anyone else gets the
// standard amount.
func creditAmountFor(r creditreq.Record) int64 {
	if r.Override && r.Amount > 0 {
		return r.Amount
	}
	switch r.Role {
	case "hauler":
		return creditHaulerAmount
	case "assist":
		return max(int64(assistCreditFloor-r.Credits), creditGiftAmount/10)
	}

	return creditGiftAmount
}
