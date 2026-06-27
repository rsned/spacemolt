package market

import (
	"context"
	"database/sql"
	"fmt"
)

// RecordHaulResult writes one completed-haul outcome row (real, cargo-capped prices and
// per-leg timing), the source of the dashboard's true realized-profit and response-time.
func (c *Collector) RecordHaulResult(ctx context.Context, r HaulResult) error {
	return c.writeRetry(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
INSERT INTO haul_results
 (opp_id, agent_id, item_id, qty, buy_price_paid, sell_price_got, realized_profit,
  jumps_traveled, claimed_at, arrived_src_at, bought_at, arrived_dst_at, sold_at,
  claimed_tick, arrived_src_tick, bought_tick, arrived_dst_tick, sold_tick, created_at)
 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			r.OppID, r.AgentID, r.ItemID, r.Qty, r.BuyPricePaid, r.SellPriceGot, r.RealizedProfit,
			r.JumpsTraveled, r.ClaimedAt, r.ArrivedSrcAt, r.BoughtAt, r.ArrivedDstAt, r.SoldAt,
			r.ClaimedTick, r.ArrivedSrcTick, r.BoughtTick, r.ArrivedDstTick, r.SoldTick, r.CreatedAt)
		return err
	})
}

// GetHaulResults returns the most recent haul results for agentID (all agents if empty),
// newest sold first, capped at limit (<=0 -> 500).
func (c *Collector) GetHaulResults(ctx context.Context, agentID string, limit int) ([]HaulResult, error) {
	if limit <= 0 {
		limit = 500
	}
	q := `SELECT id, opp_id, agent_id, item_id, qty, buy_price_paid, sell_price_got,
 realized_profit, jumps_traveled, claimed_at, arrived_src_at, bought_at, arrived_dst_at,
 sold_at, claimed_tick, arrived_src_tick, bought_tick, arrived_dst_tick, sold_tick, created_at
 FROM haul_results`
	args := []any{}
	if agentID != "" {
		q += ` WHERE agent_id = ?`
		args = append(args, agentID)
	}
	q += ` ORDER BY sold_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := c.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("get haul results: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var out []HaulResult
	for rows.Next() {
		var r HaulResult
		if err := rows.Scan(&r.ID, &r.OppID, &r.AgentID, &r.ItemID, &r.Qty, &r.BuyPricePaid,
			&r.SellPriceGot, &r.RealizedProfit, &r.JumpsTraveled, &r.ClaimedAt, &r.ArrivedSrcAt,
			&r.BoughtAt, &r.ArrivedDstAt, &r.SoldAt, &r.ClaimedTick, &r.ArrivedSrcTick,
			&r.BoughtTick, &r.ArrivedDstTick, &r.SoldTick, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan haul result: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RecordFleetSnapshot writes a batch of per-hauler balance/fuel/cargo snapshots in one
// transaction (the quarter-hourly fleet_timeseries feed for the credit-balance line).
func (c *Collector) RecordFleetSnapshot(ctx context.Context, rows []FleetSnapshot) error {
	if len(rows) == 0 {
		return nil
	}
	return c.writeRetry(ctx, func(tx *sql.Tx) error {
		for _, r := range rows {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO fleet_timeseries
 (ts, agent_id, role, system, docked, credits, fuel, max_fuel, cargo_used, cargo_capacity)
 VALUES (?,?,?,?,?,?,?,?,?,?)`,
				r.TS, r.AgentID, r.Role, r.System, boolToInt(r.Docked), r.Credits, r.Fuel,
				r.MaxFuel, r.CargoUsed, r.CargoCapacity); err != nil {
				return err
			}
		}
		return nil
	})
}

// GetFleetSnapshots returns snapshots for agentID (all if empty), newest first, capped at
// limit (<=0 -> 5000).
func (c *Collector) GetFleetSnapshots(ctx context.Context, agentID string, limit int) ([]FleetSnapshot, error) {
	if limit <= 0 {
		limit = 5000
	}
	q := `SELECT id, ts, agent_id, role, system, docked, credits, fuel, max_fuel, cargo_used, cargo_capacity FROM fleet_timeseries`
	args := []any{}
	if agentID != "" {
		q += ` WHERE agent_id = ?`
		args = append(args, agentID)
	}
	q += ` ORDER BY ts DESC LIMIT ?`
	args = append(args, limit)
	rows, err := c.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("get fleet snapshots: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var out []FleetSnapshot
	for rows.Next() {
		var r FleetSnapshot
		var docked int
		if err := rows.Scan(&r.ID, &r.TS, &r.AgentID, &r.Role, &r.System, &docked, &r.Credits,
			&r.Fuel, &r.MaxFuel, &r.CargoUsed, &r.CargoCapacity); err != nil {
			return nil, fmt.Errorf("scan fleet snapshot: %w", err)
		}
		r.Docked = docked != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// HaulTotals is an agent's recorded-haul roll-up: how many completed hauls and
// the summed real realized profit across them (losses included).
type HaulTotals struct {
	Hauls          int
	RealizedProfit float64
}

// HaulResultTotals aggregates haul_results per agent — count and summed
// realized_profit — in a single grouped query. This is the truthful basis for
// fleet-report's realized-profit + haul-count columns (the actual-fill recorder
// writes haul_results), replacing the old arbitrage-opportunity gross estimate.
func (c *Collector) HaulResultTotals(ctx context.Context) (map[string]HaulTotals, error) {
	rows, err := c.db.QueryContext(ctx,
		`SELECT agent_id, COUNT(*), COALESCE(SUM(realized_profit), 0)
		   FROM haul_results GROUP BY agent_id`)
	if err != nil {
		return nil, fmt.Errorf("query haul_result totals: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	out := map[string]HaulTotals{}
	for rows.Next() {
		var agent string
		var t HaulTotals
		if err := rows.Scan(&agent, &t.Hauls, &t.RealizedProfit); err != nil {
			return nil, fmt.Errorf("scan haul_result totals: %w", err)
		}
		out[agent] = t
	}
	return out, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
