package knowledge

import (
	"context"
	"fmt"
	"time"
)

// ShipyardQuote is one shipyard quote or commission reply. Royalty is nil
// when the reply carried no royalty text, which is distinct from zero.
type ShipyardQuote struct {
	ShipClass, StationID, FactionID, AgentID string
	Mode                                     string // "quote" | "commission"

	Labor, MaterialCost, ProvideMaterialsTotal, YardMargin, ShipyardTierHere int

	Royalty *int
	Tick    int64
	SeenAt  time.Time
	Raw     []byte
}

// ShipLicense is a ship design license bought for a faction.
type ShipLicense struct {
	FactionID, ShipClass, Empire, AgentID string
	CostPaid                              int
	Tick                                  int64
	SeenAt                                time.Time
}

// ShipyardRecorder is the subset of the KB that stores shipyard replies.
// SQLiteKB implements it; callers narrow a Base to it and no-op otherwise.
type ShipyardRecorder interface {
	RecordShipyardQuote(ctx context.Context, q ShipyardQuote) error
	RecordShipLicense(ctx context.Context, l ShipLicense) error
}

// RecordShipyardQuote appends one quote/commission row.
func (kb *SQLiteKB) RecordShipyardQuote(ctx context.Context, q ShipyardQuote) error {
	var royalty any
	if q.Royalty != nil {
		royalty = *q.Royalty
	}
	if _, err := kb.db.ExecContext(ctx, `
INSERT INTO shipyard_quotes
	(ship_class, station_id, faction_id, agent_id, mode, labor, material_cost,
	 provide_materials_total, yard_margin, shipyard_tier_here, royalty, tick, seen_at, raw)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		q.ShipClass, q.StationID, q.FactionID, q.AgentID, q.Mode, q.Labor, q.MaterialCost,
		q.ProvideMaterialsTotal, q.YardMargin, q.ShipyardTierHere, royalty, q.Tick,
		q.SeenAt.UTC().Format(time.RFC3339), string(q.Raw),
	); err != nil {
		return fmt.Errorf("knowledge: insert shipyard_quotes: %w", err)
	}

	return nil
}

// RecordShipLicense stores a license purchase, replacing any earlier row for
// the same faction and design so the latest purchase wins.
func (kb *SQLiteKB) RecordShipLicense(ctx context.Context, l ShipLicense) error {
	if _, err := kb.db.ExecContext(ctx, `
INSERT OR REPLACE INTO ship_licenses
	(faction_id, ship_class, empire, agent_id, cost_paid, tick, seen_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		l.FactionID, l.ShipClass, l.Empire, l.AgentID, l.CostPaid, l.Tick,
		l.SeenAt.UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("knowledge: insert ship_licenses: %w", err)
	}

	return nil
}

var _ ShipyardRecorder = (*SQLiteKB)(nil)
