package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/rsned/spacemolt/pkg/creditreq"
)

// Banker-side limits for pay_credit_requests.
const (
	creditGiftAmount    = 20000
	creditBankerFloor   = 1_000_000.0
	creditDailyCap      = 1_000_000
	creditPerAgentSpan  = 24 * time.Hour
	creditMaxSendErrors = 3
)

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
}

// PayCreditRequests pays pending credit requests oldest first, subject to the
// banker floor, the one-payment-per-agent-per-24h rule and the fleet daily cap.
// A payment is written to the history BEFORE the record leaves the queue, so a
// crash between the two can never cause a second gift (the 24h rule sees it).
func PayCreditRequests(ctx context.Context, d PayDeps) (paid, held int, err error) {
	recs, err := d.Queue.List()
	if err != nil {
		return 0, 0, err
	}
	var pending []creditreq.Record
	for _, r := range recs {
		if r.Status == creditreq.StatusPending {
			pending = append(pending, r)
		}
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

	for _, r := range pending {
		if err := ctx.Err(); err != nil {
			return paid, held, err
		}
		if own := d.OwnCredits(); own < creditBankerFloor {
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

		now := d.Now().UTC()
		midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		since := now.Add(-creditPerAgentSpan)
		if midnight.Before(since) {
			since = midnight
		}
		entries, err := d.History.Since(since)
		if err != nil {
			return paid, held, err
		}
		if creditreq.PaidSince(entries, r.AgentID, now.Add(-creditPerAgentSpan)) >= 1 {
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
		if creditreq.TotalPaidSince(entries, midnight)+creditGiftAmount > creditDailyCap {
			if err := hold(r, username, "daily cap"); err != nil {
				return paid, held, err
			}
			continue
		}

		gerr := d.Sender.SendGift(ctx, map[string]any{"recipient": username, "credits": creditGiftAmount})
		if gerr != nil {
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

		// History first: if the Remove below fails, the 24h rule still stops a repeat.
		if err := d.History.Append(creditreq.Entry{At: now, AgentID: r.AgentID, Username: username, Event: creditreq.EventPaid, Amount: creditGiftAmount}); err != nil {
			return paid, held, err
		}
		paid++
		if err := d.Queue.Remove(r.AgentID); err != nil {
			return paid, held, err
		}
		logf("pay_credit_requests: paid %d credits to %s (%s)", creditGiftAmount, username, r.AgentID)
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
	names := d.loadCreditUsernames(ctx)
	paid, held, err := PayCreditRequests(ctx, PayDeps{
		Queue:   d.CreditQueue,
		History: d.CreditHistory,
		Sender:  d.Client,
		OwnCredits: func() float64 {
			if st := d.Client.GetState(); st != nil {
				return st.Player.Credits
			}
			return 0
		},
		Usernames: func(id string) string { return names[id] },
		Now:       time.Now,
		Out:       d.Out,
	})
	fmt.Fprintf(d.Out, "pay_credit_requests: paid %d, held %d\n", paid, held) //nolint:errcheck
	return err
}

// loadCreditUsernames reads agent_id -> username from the assets ledger. A nil
// store or a failed query yields an empty map, so each record is held
// "no username" rather than the run failing.
func (d *WorkerDispatch) loadCreditUsernames(ctx context.Context) map[string]string {
	out := map[string]string{}
	if d.Assets == nil {
		return out
	}
	rows, err := d.Assets.DB().QueryContext(ctx, `SELECT agent_id, username FROM agents WHERE username != ''`)
	if err != nil {
		return out
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var id, user string
		if rows.Scan(&id, &user) == nil {
			out[id] = user
		}
	}
	return out
}
