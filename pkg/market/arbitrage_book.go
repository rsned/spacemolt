package market

import (
	"context"
	"database/sql"
	"fmt"
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

		// 2. Destinations already taken by active claimants (for fan-out).
		taken := map[string]bool{}
		rows, err := tx.QueryContext(ctx,
			`SELECT to_station_id FROM haul_book_claims
			 WHERE item_id=? AND from_station_id=? AND phase IN ('claimed','bought')
			   AND `+notExpiredSQL, book.ItemID, book.FromStationID)
		if err != nil {
			return fmt.Errorf("read taken dests: %w", err)
		}
		for rows.Next() {
			var d string
			if err := rows.Scan(&d); err != nil {
				_ = rows.Close()
				return fmt.Errorf("scan taken dest: %w", err)
			}
			taken[d] = true
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("iterate taken dests: %w", err)
		}
		_ = rows.Close()

		// 3. Prefer unassigned destinations (preserving rank), then fall back to taken
		//    ones so a fat book with fewer distinct dests than K is not stranded.
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

		// 4. Claim the first candidate whose opp row is still available (the per-row
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
			ins, err := tx.ExecContext(ctx,
				`INSERT INTO haul_book_claims
				   (item_id, from_station_id, opp_id, to_station_id, agent_id, phase,
				    bought_units, claimed_at, updated_at, expires_at)
				 VALUES (?,?,?,?,?, 'claimed', 0, ?,?,?)`,
				book.ItemID, book.FromStationID, cand.OppID, cand.ToStationID, agentID,
				nowStr, nowStr, expiresStr)
			if err != nil {
				return fmt.Errorf("insert book claim: %w", err)
			}
			id, _ := ins.LastInsertId()
			res = AdmitResult{OK: true, ClaimID: id, OppID: cand.OppID, ToStationID: cand.ToStationID}
			return nil
		}
		return nil // nothing claimable
	})
	return res, err
}
