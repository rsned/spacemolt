package market

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"
)

// BookKey identifies a haul "book": one item at one source station.
type BookKey struct {
	ItemID        string
	FromStationID string
}

// BookCandidate is one destination row of a book, offered to AdmitBookClaim in
// ranked (best-first) order. OppID is the arbitrage_opportunities row id.
type BookCandidate struct {
	OppID       int
	ToStationID string
}

// AdmitResult reports the outcome of AdmitBookClaim.
type AdmitResult struct {
	OK          bool   // false = book at capacity, or no candidate still claimable
	ClaimID     int64  // haul_book_claims.claim_id (valid when OK)
	OppID       int    // admitted opportunity row (valid when OK)
	ToStationID string // its destination (valid when OK)
	// Units is what a consigned claim reserved. 0 from AdmitBookClaim, which
	// does not reserve at claim time.
	Units float64
}

// AdmitBookClaim atomically admits agentID onto a book if it is under capacity,
// assigning the best still-available destination not already held by another active
// claimant (fan-out; degrades to reusing the best destination when all are taken).
// The whole decision — cap count, taken-destination read, per-row claim, and roster
// insert — runs in one IMMEDIATE transaction, so a losing racer observes the winner's
// committed state and never re-collides. Returns OK=false (no error) when the book is
// at capacity or every candidate row was claimed by someone else first.
func (c *Collector) AdmitBookClaim(ctx context.Context, book BookKey, candidates []BookCandidate, agentID string, capN int, expiresIn time.Duration) (AdmitResult, error) {
	var res AdmitResult
	if capN < 1 || len(candidates) == 0 {
		return res, nil
	}
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	expiresStr := now.Add(expiresIn).Format(time.RFC3339)

	err := c.writeRetry(ctx, func(tx *sql.Tx) error {
		// 1. Capacity: how many haulers are already active on this book?
		var active int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM haul_book_claims
			 WHERE item_id=? AND from_station_id=? AND phase IN ('claimed','bought')
			   AND `+notExpiredSQL, book.ItemID, book.FromStationID).Scan(&active); err != nil {
			return fmt.Errorf("count book claimants: %w", err)
		}
		if active >= capN {
			return nil // res.OK stays false
		}

		// 2. Prefer unassigned destinations, then taken ones.
		ordered, err := fanOutOrder(ctx, tx, book, candidates)
		if err != nil {
			return err
		}

		// 3. Claim the first candidate whose opp row is still available (the per-row
		//    destination lock), then record the roster row.
		for _, cand := range ordered {
			r, err := tx.ExecContext(ctx,
				`UPDATE arbitrage_opportunities SET status='claimed', claimed_by=?, claimed_at=?
				 WHERE id=? AND status='available' AND `+notExpiredSQL,
				agentID, nowStr, cand.OppID)
			if err != nil {
				return fmt.Errorf("claim opp row: %w", err)
			}
			n, _ := r.RowsAffected()
			if n == 0 {
				continue // claimed by another hauler between scan and now; try next
			}
			id, err := insertBookClaim(ctx, tx, book, cand, agentID, nowStr, expiresStr)
			if err != nil {
				return err
			}
			res = AdmitResult{OK: true, ClaimID: id, OppID: cand.OppID, ToStationID: cand.ToStationID}
			return nil
		}
		return nil // nothing claimable
	})
	return res, err
}

// ConsignMinClaimValue is the smallest consigned claim admitted, in sale value.
// Matches the scanner's default profit floor: without it the tail of a book
// sends a hauler across the map for a handful of units.
const ConsignMinClaimValue = 5000

// AdmitConsignClaim admits agentID onto a consigned book by units, not by
// head count. Each route was sized to the whole stock when scanned, so two
// claims could promise the same goods: on 2026-11-05 a hold of 1,900 and a hold
// of 100 each passed the head-count cap and claimed 2,144 of about 1,700.
//
// wantUnits is what the hauler can load. The reservation is the smallest of
// that, the route's quantity and the holder's unspoken-for stock, and it
// replaces the row's quantity, so the gift request, the sale and the scanner's
// netting all read the true figure. Stock nets out the same way the scanner
// nets it: only claims made after the holder's last report, since that report
// already excludes earlier ones. Returns OK=false when nothing is left, or
// when what is left is worth less than ConsignMinClaimValue.
func (c *Collector) AdmitConsignClaim(ctx context.Context, book BookKey, candidates []BookCandidate, agentID string, wantUnits float64, expiresIn time.Duration) (AdmitResult, error) {
	var res AdmitResult
	if wantUnits < 1 || len(candidates) == 0 {
		return res, nil
	}
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	expiresStr := now.Add(expiresIn).Format(time.RFC3339)
	freshSince := now.Add(-ConsignStockMaxAge).Format(time.RFC3339)

	err := c.writeRetry(ctx, func(tx *sql.Tx) error {
		ordered, err := fanOutOrder(ctx, tx, book, candidates)
		if err != nil {
			return err
		}
		for _, cand := range ordered {
			var qty, sell float64
			var notes string
			err := tx.QueryRowContext(ctx,
				`SELECT quantity, sell_price, notes FROM arbitrage_opportunities
				 WHERE id=? AND action_type=? AND status='available' AND `+notExpiredSQL,
				cand.OppID, ActionWithdrawThenSell).Scan(&qty, &sell, &notes)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return fmt.Errorf("read consigned route: %w", err)
			}
			holder, _, ok := ConsignSource(ArbitrageOpportunity{ActionType: ActionWithdrawThenSell, Notes: notes})
			if !ok {
				continue
			}
			var stock float64
			err = tx.QueryRowContext(ctx, `
				SELECT cs.units - COALESCE((
				  SELECT SUM(ao.quantity) FROM arbitrage_opportunities ao
				  WHERE ao.action_type = ? AND ao.status = 'claimed'
				    AND ao.from_station_id = cs.station_id AND ao.item_id = cs.item_id
				    AND ao.notes = ? || 'holder=' || cs.holder || ' base=' || cs.base_id
				    AND julianday(ao.claimed_at) > julianday(cs.updated_at)), 0)
				FROM consign_stock cs
				WHERE cs.holder = ? AND cs.station_id = ? AND cs.item_id = ?
				  AND julianday(cs.updated_at) >= julianday(?)`,
				ActionWithdrawThenSell, consignNotesPrefix, holder, book.FromStationID, book.ItemID, freshSince).Scan(&stock)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return fmt.Errorf("read consign stock: %w", err)
			}
			take := math.Floor(min(wantUnits, qty, stock))
			if take < 1 || take*sell < ConsignMinClaimValue {
				continue
			}
			r, err := tx.ExecContext(ctx,
				`UPDATE arbitrage_opportunities
				 SET status='claimed', claimed_by=?, claimed_at=?, quantity=?, cargo_required=?, gross_profit=?
				 WHERE id=? AND status='available'`,
				agentID, nowStr, take, take, take*sell, cand.OppID)
			if err != nil {
				return fmt.Errorf("claim consigned route: %w", err)
			}
			if n, _ := r.RowsAffected(); n == 0 {
				continue
			}
			id, err := insertBookClaim(ctx, tx, book, cand, agentID, nowStr, expiresStr)
			if err != nil {
				return err
			}
			res = AdmitResult{OK: true, ClaimID: id, OppID: cand.OppID, ToStationID: cand.ToStationID, Units: take}
			return nil
		}
		return nil
	})
	return res, err
}

// fanOutOrder ranks candidates so destinations no active claimant holds come
// first, keeping rank within each group. Taken ones stay in, so a fat book with
// few distinct destinations is not stranded.
func fanOutOrder(ctx context.Context, tx *sql.Tx, book BookKey, candidates []BookCandidate) ([]BookCandidate, error) {
	taken := map[string]bool{}
	rows, err := tx.QueryContext(ctx,
		`SELECT to_station_id FROM haul_book_claims
		 WHERE item_id=? AND from_station_id=? AND phase IN ('claimed','bought')
		   AND `+notExpiredSQL, book.ItemID, book.FromStationID)
	if err != nil {
		return nil, fmt.Errorf("read taken dests: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, fmt.Errorf("scan taken dest: %w", err)
		}
		taken[d] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate taken dests: %w", err)
	}
	ordered := make([]BookCandidate, 0, len(candidates))
	for _, cand := range candidates {
		if !taken[cand.ToStationID] {
			ordered = append(ordered, cand)
		}
	}
	for _, cand := range candidates {
		if taken[cand.ToStationID] {
			ordered = append(ordered, cand)
		}
	}
	return ordered, nil
}

// insertBookClaim records agentID on the book's roster.
func insertBookClaim(ctx context.Context, tx *sql.Tx, book BookKey, cand BookCandidate, agentID, nowStr, expiresStr string) (int64, error) {
	ins, err := tx.ExecContext(ctx,
		`INSERT INTO haul_book_claims
		   (item_id, from_station_id, opp_id, to_station_id, agent_id, phase,
		    bought_units, claimed_at, updated_at, expires_at)
		 VALUES (?,?,?,?,?, 'claimed', 0, ?,?,?)`,
		book.ItemID, book.FromStationID, cand.OppID, cand.ToStationID, agentID,
		nowStr, nowStr, expiresStr)
	if err != nil {
		return 0, fmt.Errorf("insert book claim: %w", err)
	}
	id, _ := ins.LastInsertId()
	return id, nil
}

// SettleBookClaim records the actual bought quantity on a claim (settle-only: this is
// the "subtract actual amount used" write). Idempotent no-op if the claim is not in
// 'claimed' phase or not owned by agentID.
func (c *Collector) SettleBookClaim(ctx context.Context, claimID int64, agentID string, boughtUnits float64) error {
	return c.writeRetry(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`UPDATE haul_book_claims SET phase='bought', bought_units=?, updated_at=?
			 WHERE claim_id=? AND agent_id=? AND phase='claimed'`,
			boughtUnits, time.Now().UTC().Format(time.RFC3339), claimID, agentID)
		if err != nil {
			return fmt.Errorf("settle book claim: %w", err)
		}
		return nil
	})
}

// CompleteBookClaim marks a claim done after its sell leg completes, freeing its cap
// slot. Owner-scoped; no-op otherwise.
func (c *Collector) CompleteBookClaim(ctx context.Context, claimID int64, agentID string) error {
	return c.writeRetry(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`UPDATE haul_book_claims SET phase='done', updated_at=?
			 WHERE claim_id=? AND agent_id=? AND phase IN ('claimed','bought')`,
			time.Now().UTC().Format(time.RFC3339), claimID, agentID)
		if err != nil {
			return fmt.Errorf("complete book claim: %w", err)
		}
		return nil
	})
}

// ReleaseBookClaim returns a claim's cap slot on a pre-buy abandon. Owner-scoped.
func (c *Collector) ReleaseBookClaim(ctx context.Context, claimID int64, agentID string) error {
	return c.writeRetry(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`UPDATE haul_book_claims SET phase='released', updated_at=?
			 WHERE claim_id=? AND agent_id=? AND phase IN ('claimed','bought')`,
			time.Now().UTC().Format(time.RFC3339), claimID, agentID)
		if err != nil {
			return fmt.Errorf("release book claim: %w", err)
		}
		return nil
	})
}

// InvalidateBook clears a collapsed book so no further hauler is drawn in: it expires
// the book's still-available opportunity rows and the CALLING agent's own claimed row
// (so its abandon does not republish it), while leaving other agents' claimed rows
// untouched — they confirm-and-invalidate on their own arrival. Call only when a live
// recapture confirmed the source is gone.
func (c *Collector) InvalidateBook(ctx context.Context, itemID, fromStation, agentID, reason string) error {
	return c.writeRetry(ctx, func(tx *sql.Tx) error {
		note := fmt.Sprintf("source collapsed: %s (by %s)", reason, agentID)
		_, err := tx.ExecContext(ctx,
			`UPDATE arbitrage_opportunities SET status='expired', notes=?
			 WHERE item_id=? AND from_station_id=?
			   AND (status='available' OR (status='claimed' AND claimed_by=?))`,
			note, itemID, fromStation, agentID)
		if err != nil {
			return fmt.Errorf("invalidate book opps: %w", err)
		}
		return nil
	})
}

// GetActiveBookClaim returns the claim_id of agentID's active (claimed/bought) claim on
// a book, if any. The partial unique index idx_bookclaims_active_agent guarantees at
// most one such row. Used to recover the claim id when resuming a haul (e.g. across a
// process restart) so the resumed completion still frees the cap slot.
func (c *Collector) GetActiveBookClaim(ctx context.Context, itemID, fromStation, agentID string) (int64, bool, error) {
	var id int64
	err := c.db.QueryRowContext(ctx,
		`SELECT claim_id FROM haul_book_claims
		 WHERE item_id=? AND from_station_id=? AND agent_id=? AND phase IN ('claimed','bought')
		 ORDER BY claim_id DESC LIMIT 1`,
		itemID, fromStation, agentID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("get active book claim: %w", err)
	}
	return id, true, nil
}

// ReapExpiredBookClaims releases roster rows whose TTL passed while still active
// (e.g. a hauler killed between claim and settle). The cap count already ignores
// expired rows; this is cleanup so the roster does not grow unbounded. Returns the
// number reaped.
func (c *Collector) ReapExpiredBookClaims(ctx context.Context) (int, error) {
	n := 0
	err := c.writeRetry(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE haul_book_claims SET phase='released', updated_at=?
			 WHERE phase IN ('claimed','bought') AND julianday(expires_at) <= julianday('now')`,
			time.Now().UTC().Format(time.RFC3339))
		if err != nil {
			return fmt.Errorf("reap book claims: %w", err)
		}
		if k, e := res.RowsAffected(); e == nil {
			n = int(k)
		}
		return nil
	})
	return n, err
}
