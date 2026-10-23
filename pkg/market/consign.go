package market

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ConsignStockMaxAge is how long a collector's stock report stays offerable.
// The collector re-reports on every idle pass, so a row older than this means
// it has stopped running — and a hauler flown to a stronghold for goods that
// may have moved is the cost of trusting it.
const ConsignStockMaxAge = 2 * time.Hour

// ConsignStock is one collector's offer: Units of ItemID sitting in its
// personal storage at a station, available to any hauler that claims a
// consigned route for them. StationID is the market id the scanner pairs
// against (voss_redoubt); BaseID is the docked-base id the handoff queue and
// send_gift work in (voss_redoubt_station) — the two differ, and neither can
// be derived from the other here.
type ConsignStock struct {
	Holder    string
	StationID string
	BaseID    string
	ItemID    string
	Units     float64
	UpdatedAt time.Time
}

// UpsertConsignStock records the collector's current offer, replacing any
// earlier report for the same (holder, station, item). A report of zero units
// is kept rather than deleted, so "nothing to offer" is as fresh as any other
// reading.
func (c *Collector) UpsertConsignStock(ctx context.Context, s ConsignStock) error {
	return c.writeRetry(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO consign_stock (holder, station_id, base_id, item_id, units, updated_at)
			VALUES (?,?,?,?,?,?)
			ON CONFLICT(holder, station_id, item_id) DO UPDATE SET
			  base_id=excluded.base_id, units=excluded.units, updated_at=excluded.updated_at`,
			s.Holder, s.StationID, s.BaseID, s.ItemID, s.Units, s.UpdatedAt.UTC().Format(time.RFC3339)); err != nil {
			return fmt.Errorf("upsert consign stock: %w", err)
		}
		return nil
	})
}

// consignNotesPrefix tags a consigned row's notes. The opportunity reads do not
// select discovered_by, so the holder and base ride in notes, which they do.
const consignNotesPrefix = "consign "

func consignNotes(holder, baseID string) string {
	return consignNotesPrefix + "holder=" + holder + " base=" + baseID
}

// ConsignSource returns the collector holding a consigned route's goods and
// the base they sit at — what a claiming hauler needs to ask for the gift. ok
// is false for an ordinary route or a row whose notes do not carry both.
func ConsignSource(o ArbitrageOpportunity) (holder, baseID string, ok bool) {
	if !o.IsConsigned() || !strings.HasPrefix(o.Notes, consignNotesPrefix) {
		return "", "", false
	}
	for f := range strings.FieldsSeq(strings.TrimPrefix(o.Notes, consignNotesPrefix)) {
		switch k, v, _ := strings.Cut(f, "="); k {
		case "holder":
			holder = v
		case "base":
			baseID = v
		}
	}
	return holder, baseID, holder != "" && baseID != ""
}

// consignRoutesAccepted reports whether this database's arbitrage table admits
// withdraw_then_sell rows. Tables created before consignment carry a CHECK
// limited to the two buy-side types, and SQLite cannot widen a CHECK without
// rebuilding the table. One rejected insert would roll back the whole scan and
// empty every hauler's pool, so the scanner asks first and skips consignment
// on an old table instead.
func (c *Collector) consignRoutesAccepted(ctx context.Context) (bool, error) {
	var ddl string
	if err := c.db.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_master WHERE type='table' AND name='arbitrage_opportunities'`).Scan(&ddl); err != nil {
		return false, fmt.Errorf("read arbitrage table schema: %w", err)
	}
	return strings.Contains(ddl, "'"+ActionWithdrawThenSell+"'"), nil
}

// consignCandidates turns fresh collector stock into consigned routes, one per
// destination bid book. The source "ask" is the stock itself at zero cost, so
// OptimalArbitrage sizes each route to the smaller of stock and bid depth.
//
// Units already promised are netted out: a consigned claim made after the
// collector's last report has not yet been gifted out of its storage, so its
// quantity is still in the report. Claims from before the report are already
// reflected there and are not subtracted twice.
func (c *Collector) consignCandidates(ctx context.Context, opts ScanOptions, now time.Time) ([]arbCandidate, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT cs.holder, cs.station_id, cs.base_id, cs.item_id,
		       cs.units - COALESCE((
		         SELECT SUM(ao.quantity) FROM arbitrage_opportunities ao
		         WHERE ao.action_type = ? AND ao.status = 'claimed'
		           AND ao.from_station_id = cs.station_id AND ao.item_id = cs.item_id
		           AND ao.notes = ? || 'holder=' || cs.holder || ' base=' || cs.base_id
		           AND julianday(ao.claimed_at) > julianday(cs.updated_at)), 0)
		FROM consign_stock cs
		WHERE cs.units > 0 AND julianday(cs.updated_at) >= julianday(?)`,
		ActionWithdrawThenSell, consignNotesPrefix, now.Add(-ConsignStockMaxAge).Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("read consign stock: %w", err)
	}
	type offer struct {
		holder, station, base, item string
		units                       float64
	}
	var offers []offer
	for rows.Next() {
		var o offer
		if err := rows.Scan(&o.holder, &o.station, &o.base, &o.item, &o.units); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan consign stock: %w", err)
		}
		if o.units >= opts.MinQuantity {
			offers = append(offers, o)
		}
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close consign stock: %w", err)
	}

	var out []arbCandidate
	for _, o := range offers {
		prices, err := c.GetItemStationPrices(ctx, o.item)
		if err != nil {
			return nil, fmt.Errorf("consign prices %s: %w", o.item, err)
		}
		stock := []AskLevel{{PriceEach: 0, Quantity: o.units}}
		for _, dst := range prices {
			if dst.StationID == o.station || !dst.HasBuy || dst.BestBid < opts.MinPrice || dst.BidQty < opts.MinQuantity {
				continue
			}
			bids, err := c.GetBidLadder(ctx, o.item, dst.StationID)
			if err != nil {
				return nil, fmt.Errorf("consign bid ladder %s@%s: %w", o.item, dst.StationID, err)
			}
			qty, _, revenue, gross := OptimalArbitrage(stock, bids)
			if qty < opts.MinQuantity || gross < opts.MinProfit {
				continue
			}
			out = append(out, arbCandidate{
				fromStation: o.station,
				toStation:   dst.StationID,
				itemID:      o.item,
				sellPrice:   revenue / qty,
				qty:         qty,
				gross:       gross,
				sourceUnits: o.units,
				actionType:  ActionWithdrawThenSell,
				discovered:  "consign:" + o.holder,
				notes:       consignNotes(o.holder, o.base),
			})
		}
	}
	return out, nil
}
