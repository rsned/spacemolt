package market

import (
	"context"
	"fmt"
	"time"
)

// HaulEfficiencyRow is one agent's haul aggregates over a time window.
type HaulEfficiencyRow struct {
	AgentID   string
	Hauls     int
	SumProfit float64 // Σ realized_profit
	SumJumps  int64   // Σ jumps_traveled
}

// HaulEfficiencySince returns per-agent aggregates over haul_results rows with
// sold_at >= since and jumps_traveled > 0, plus the summed fleet row (AgentID
// ""). A zero `since` (time.Time{}) means all-time. Agents with no qualifying
// rows are absent from perAgent. Rows with jumps_traveled = 0 are excluded
// (degenerate and a zero divisor for cr/jump).
func (c *Collector) HaulEfficiencySince(ctx context.Context, since time.Time) (perAgent []HaulEfficiencyRow, fleet HaulEfficiencyRow, err error) {
	rows, err := c.db.QueryContext(ctx, `
SELECT agent_id, COUNT(*), COALESCE(SUM(realized_profit),0), COALESCE(SUM(jumps_traveled),0)
  FROM haul_results
 WHERE sold_at >= ? AND jumps_traveled > 0
 GROUP BY agent_id`, since.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, HaulEfficiencyRow{}, fmt.Errorf("query haul efficiency: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var r HaulEfficiencyRow
		if err := rows.Scan(&r.AgentID, &r.Hauls, &r.SumProfit, &r.SumJumps); err != nil {
			return nil, HaulEfficiencyRow{}, fmt.Errorf("scan haul efficiency: %w", err)
		}
		perAgent = append(perAgent, r)
		fleet.Hauls += r.Hauls
		fleet.SumProfit += r.SumProfit
		fleet.SumJumps += r.SumJumps
	}
	if err := rows.Err(); err != nil {
		return nil, HaulEfficiencyRow{}, fmt.Errorf("iterate haul efficiency: %w", err)
	}
	return perAgent, fleet, nil
}
