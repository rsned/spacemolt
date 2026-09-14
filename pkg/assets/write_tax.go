package assets

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// UpsertTax records an agent's forward-looking tax position.
func (s *Store) UpsertTax(ctx context.Context, playerID string, t TaxEstimate, now time.Time) error {
	if playerID == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_tax (player_id, assessed_property_value, property_tax_total,
			income_tax_total, taxable_income_to_date, taxable_market_income,
			market_sales_to_date, market_cogs_deducted, market_loss_carryforward,
			tax_prepaid, next_assessment_seconds, collection_active, note,
			sales_tax_rates, outstanding_bounty_total, inactivity_exempt, captured_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(player_id) DO UPDATE SET
			assessed_property_value  = excluded.assessed_property_value,
			property_tax_total       = excluded.property_tax_total,
			income_tax_total         = excluded.income_tax_total,
			taxable_income_to_date   = excluded.taxable_income_to_date,
			taxable_market_income    = excluded.taxable_market_income,
			market_sales_to_date     = excluded.market_sales_to_date,
			market_cogs_deducted     = excluded.market_cogs_deducted,
			market_loss_carryforward = excluded.market_loss_carryforward,
			tax_prepaid              = excluded.tax_prepaid,
			next_assessment_seconds  = excluded.next_assessment_seconds,
			collection_active        = excluded.collection_active,
			note                     = excluded.note,
			sales_tax_rates          = excluded.sales_tax_rates,
			outstanding_bounty_total = excluded.outstanding_bounty_total,
			inactivity_exempt        = excluded.inactivity_exempt,
			captured_at              = excluded.captured_at`,
		playerID, t.AssessedPropertyValue, t.PropertyTaxTotal, t.IncomeTaxTotal,
		t.TaxableIncomeToDate, t.TaxableMarketIncome, t.MarketSalesToDate,
		t.MarketCOGSDeducted, t.MarketLossCarryforward, t.TaxPrepaid,
		t.NextAssessmentSeconds, boolToInt(t.CollectionActive), t.Note,
		t.SalesTaxRates, t.OutstandingBountyTotal, boolToInt(t.InactivityExempt),
		rfc3339(now))
	if err != nil {
		return fmt.Errorf("assets: upsert tax %s: %w", playerID, err)
	}

	return nil
}

// ReplaceTaxShips swaps in the per-hull assessed values. Ships absent from rows
// are deleted: a hull that has been sold must stop showing up as taxable value.
func (s *Store) ReplaceTaxShips(ctx context.Context, playerID string, rows []TaxShipValue, now time.Time) error {
	ts := rfc3339(now)

	return s.replaceSet(ctx, `DELETE FROM agent_tax_ships WHERE player_id = ?`, playerID, func(tx *sql.Tx) error {
		for _, r := range rows {
			if r.ShipID == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO agent_tax_ships (player_id, ship_id, value, captured_at)
				VALUES (?,?,?,?)`, playerID, r.ShipID, r.Value, ts); err != nil {
				return fmt.Errorf("assets: insert tax ship %s/%s: %w", playerID, r.ShipID, err)
			}
		}

		return nil
	})
}

// ReplaceTaxBounties swaps in the per-empire outstanding debt. Empires absent
// from rows are deleted: a bounty that has been paid must stop showing up, or
// we chase debts that are already settled.
func (s *Store) ReplaceTaxBounties(ctx context.Context, playerID string, rows []TaxBounty, now time.Time) error {
	ts := rfc3339(now)

	return s.replaceSet(ctx, `DELETE FROM agent_tax_bounties WHERE player_id = ?`, playerID, func(tx *sql.Tx) error {
		for _, r := range rows {
			if r.Empire == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO agent_tax_bounties (player_id, empire, bounty, captured_at)
				VALUES (?,?,?,?)`, playerID, r.Empire, r.Bounty, ts); err != nil {
				return fmt.Errorf("assets: insert tax bounty %s/%s: %w", playerID, r.Empire, err)
			}
		}

		return nil
	})
}

// TaxBounties returns one agent's outstanding per-empire debt, largest first.
func (s *Store) TaxBounties(ctx context.Context, playerID string) ([]TaxBounty, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT empire, bounty FROM agent_tax_bounties
		WHERE player_id = ? ORDER BY bounty DESC, empire ASC`, playerID)
	if err != nil {
		return nil, fmt.Errorf("assets: tax bounties %s: %w", playerID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []TaxBounty
	for rows.Next() {
		var b TaxBounty
		if err := rows.Scan(&b.Empire, &b.Bounty); err != nil {
			return nil, fmt.Errorf("assets: scan tax bounty %s: %w", playerID, err)
		}
		out = append(out, b)
	}

	return out, rows.Err()
}

// TaxExposure is one agent's ability to pay its next levy.
type TaxExposure struct {
	PlayerID    string
	AgentID     string
	Empire      string
	Credits     float64
	PropertyTax int64
	IncomeTax   int64
	// OutstandingBounty is debt already owed. Unlike the two levies above it
	// is due NOW and is what actually gets the pilot detained.
	OutstandingBounty int64
	TotalDue          int64
	Shortfall         int64
	CapturedAt        string
}

// TaxShortfalls lists agents whose credits will not cover what they owe, worst
// first.
//
// This is the whole point of capturing the estimate: an unpaid levy does not
// bounce, it becomes an outstanding bounty with that empire and the agent is
// detained in their territory until it is paid — and since the bounty then
// skims income, an agent at zero cannot earn its way out.
//
// "What they owe" is the next levy PLUS any bounty already outstanding. A rich
// agent can clear next week's bill and still be carrying debt from an earlier
// one — explorer-8 did exactly that on 2026-09-14 — so counting only the levy
// reports such an agent as healthy.
func (s *Store) TaxShortfalls(ctx context.Context) ([]TaxExposure, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.player_id, COALESCE(i.agent_id,''), COALESCE(p.empire,''),
		       COALESCE(p.credits,0), t.property_tax_total, t.income_tax_total,
		       t.outstanding_bounty_total, t.captured_at
		FROM agent_tax t
		LEFT JOIN agents i ON i.player_id = t.player_id
		LEFT JOIN agent_profile p ON p.player_id = t.player_id
		WHERE (t.property_tax_total + t.income_tax_total + t.outstanding_bounty_total) > COALESCE(p.credits,0)
		  AND (t.property_tax_total + t.income_tax_total + t.outstanding_bounty_total) > 0
		ORDER BY (t.property_tax_total + t.income_tax_total + t.outstanding_bounty_total) - COALESCE(p.credits,0) DESC`)
	if err != nil {
		return nil, fmt.Errorf("assets: tax shortfalls: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []TaxExposure
	for rows.Next() {
		var e TaxExposure
		if err := rows.Scan(&e.PlayerID, &e.AgentID, &e.Empire, &e.Credits,
			&e.PropertyTax, &e.IncomeTax, &e.OutstandingBounty, &e.CapturedAt); err != nil {
			return nil, fmt.Errorf("assets: scan tax shortfall: %w", err)
		}
		e.TotalDue = e.PropertyTax + e.IncomeTax + e.OutstandingBounty
		e.Shortfall = e.TotalDue - int64(e.Credits)
		out = append(out, e)
	}

	return out, rows.Err()
}
