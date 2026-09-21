package ovdash

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// ClaimedCargo is the capital one agent currently has converted into goods: the
// opportunity it holds, what it spent, and what it expects back.
//
// The fleet total on the dashboard sums WALLET credits, so every credit a
// hauler turns into cargo disappears from that number until the sell leg lands.
// With sixteen haulers holding two thirds of the fleet's money and a ~17-minute
// cycle, that is millions in flight at any instant, and a total read without it
// looks like a slow bleed. CostBasis is what left the wallet; ExpectedProceeds
// is what the claim says should come back.
type ClaimedCargo struct {
	OppID            int     `json:"opp_id"`
	ItemID           string  `json:"item_id"`
	Quantity         float64 `json:"quantity"`
	CostBasis        float64 `json:"cost_basis"`
	ExpectedProceeds float64 `json:"expected_proceeds"`
	FromStationID    string  `json:"from_station_id,omitempty"`
	ToStationID      string  `json:"to_station_id,omitempty"`
}

// LoadClaimedCargo returns the open claim per agent, keyed by agent id.
//
// Only status='claimed' rows count: a completed opportunity is already back in
// the wallet, and an unclaimed one belongs to nobody. An absent market DB
// yields an empty map and no error -- the panel is an aid, and losing it must
// not take the dashboard down.
//
// The figures are the claim's own prices, not the fill actually paid. A hauler
// taking a prefix of the book does better than this, never worse, so read
// CostBasis as an upper bound on what is committed.
func LoadClaimedCargo(ctx context.Context, dbPath string) (map[string]ClaimedCargo, error) {
	out := map[string]ClaimedCargo{}
	if _, err := os.Stat(dbPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return out, nil
		}
		return out, fmt.Errorf("stat market db: %w", err)
	}
	db, err := sql.Open(sqliteDriver, "file:"+dbPath+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return out, fmt.Errorf("open market db: %w", err)
	}
	defer db.Close() //nolint:errcheck

	rows, err := db.QueryContext(ctx, `
		SELECT id, COALESCE(claimed_by,''), COALESCE(item_id,''), COALESCE(quantity,0),
		       COALESCE(buy_price,0), COALESCE(sell_price,0),
		       COALESCE(from_station_id,''), COALESCE(to_station_id,'')
		FROM arbitrage_opportunities
		WHERE status = 'claimed' AND claimed_by IS NOT NULL AND claimed_by != ''`)
	if err != nil {
		return out, fmt.Errorf("claimed cargo: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var c ClaimedCargo
		var agent string
		var buy, sell float64
		if err := rows.Scan(&c.OppID, &agent, &c.ItemID, &c.Quantity, &buy, &sell,
			&c.FromStationID, &c.ToStationID); err != nil {
			return out, fmt.Errorf("scan claimed cargo: %w", err)
		}
		c.CostBasis = c.Quantity * buy
		c.ExpectedProceeds = c.Quantity * sell
		// An agent should hold one claim; if the table disagrees, keep the
		// largest so the panel understates nothing.
		if prev, ok := out[agent]; ok && prev.CostBasis >= c.CostBasis {
			continue
		}
		out[agent] = c
	}
	return out, rows.Err()
}

// AttachClaimedCargo stamps each agent row with the value of the goods it is
// carrying and sets the fleet-wide total.
//
// The total counts every open claim, including those whose agent is missing
// from the status files: a worker being rolled is removed from its fleet for
// minutes at a time but still owns what it bought, and dropping it is what
// makes the dashboard total lurch downward for reasons that have nothing to do
// with the fleet losing money.
func (s *Snapshot) AttachClaimedCargo(cargo map[string]ClaimedCargo) {
	if s == nil {
		return
	}
	stamp := func(list []AgentState) {
		for i := range list {
			c, ok := cargo[list[i].AgentID]
			if !ok {
				continue
			}
			list[i].CargoValue = c.CostBasis
			list[i].CargoProceeds = c.ExpectedProceeds
			list[i].CargoItem = c.ItemID
			list[i].CargoOppID = c.OppID
		}
	}
	stamp(s.Agents)
	stamp(s.OffMap)
	s.CargoValueTotal = 0
	for _, c := range cargo {
		s.CargoValueTotal += c.CostBasis
	}
}
