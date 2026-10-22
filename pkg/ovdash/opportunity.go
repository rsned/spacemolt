package ovdash

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/navigation"
)

// ErrOpportunityNotFound reports an id the scanner has pruned or never had.
// The drawer must say so rather than render a zero-valued record, which would
// look like a real opportunity worth 0 credits.
var ErrOpportunityNotFound = errors.New("opportunity not found")

// JumpsUnknown is the Jumps value when no route is known between the two
// endpoints -- a disconnected graph, or a station in a system we have never
// surveyed. It is deliberately NOT 0: zero reads as "same station, free run",
// which is the most expensive possible misreading of a haul.
const JumpsUnknown = -1

// Opportunity is one arbitrage row expanded with everything needed to judge it.
//
// The scanner's table carries prices and depth but no sense of COST: its
// `travel_ticks` and `fuel_cost` columns exist and are always 0, so a reader
// trusting them concludes every run is free. The fields below that the table
// does not hold -- UnitMargin, MarginPct, Jumps, ProfitPerJump -- are the ones
// that decide whether a trade is worth flying. A 17,874 cr profit is excellent
// over 2 jumps and poor over 21.
//
// Quantity vs SourceUnits is the other distinction worth drawing: SourceUnits
// is how deep the seller's book is, Quantity is how much the DESTINATION will
// absorb. A book 39,608 deep whose buyer wants 30 is a 30-unit trade.
type Opportunity struct {
	ID     int64  `json:"id"`
	ItemID string `json:"item_id"`
	Action string `json:"action_type,omitempty"`
	Status string `json:"status,omitempty"`

	BuyPrice   float64 `json:"buy_price"`
	SellPrice  float64 `json:"sell_price"`
	UnitMargin float64 `json:"unit_margin"`
	MarginPct  float64 `json:"margin_pct"`

	Quantity    float64 `json:"quantity"`
	SourceUnits float64 `json:"source_units"`
	GrossProfit float64 `json:"gross_profit"`

	FromStationID string `json:"from_station_id,omitempty"`
	FromName      string `json:"from_name,omitempty"`
	FromSystem    string `json:"from_system,omitempty"`
	ToStationID   string `json:"to_station_id,omitempty"`
	ToName        string `json:"to_name,omitempty"`
	ToSystem      string `json:"to_system,omitempty"`

	// Jumps is JumpsUnknown when unreachable; ProfitPerJump is then 0.
	Jumps         int     `json:"jumps"`
	ProfitPerJump float64 `json:"profit_per_jump"`

	ClaimedBy    string `json:"claimed_by,omitempty"`
	ClaimedAt    string `json:"claimed_at,omitempty"`
	DiscoveredAt string `json:"discovered_at,omitempty"`
	DiscoveredBy string `json:"discovered_by,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	Notes        string `json:"notes,omitempty"`
}

// LoadOpportunity reads one opportunity from the market DB and enriches it from
// the knowledge base: endpoint display names, their systems, and the jump
// distance between them.
func LoadOpportunity(ctx context.Context, marketPath, kbPath string, id int64) (Opportunity, error) {
	var o Opportunity
	db, err := sql.Open(sqliteDriver, "file:"+marketPath+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return o, fmt.Errorf("open market db: %w", err)
	}
	defer db.Close() //nolint:errcheck

	row := db.QueryRowContext(ctx, `
		SELECT id, COALESCE(item_id,''), COALESCE(action_type,''), COALESCE(status,''),
		       COALESCE(buy_price,0), COALESCE(sell_price,0), COALESCE(quantity,0),
		       COALESCE(source_units,0), COALESCE(gross_profit,0),
		       COALESCE(from_station_id,''), COALESCE(to_station_id,''),
		       COALESCE(claimed_by,''), COALESCE(claimed_at,''),
		       COALESCE(discovered_at,''), COALESCE(discovered_by,''),
		       COALESCE(expires_at,''), COALESCE(notes,'')
		FROM arbitrage_opportunities WHERE id = ?`, id)
	err = row.Scan(&o.ID, &o.ItemID, &o.Action, &o.Status, &o.BuyPrice, &o.SellPrice,
		&o.Quantity, &o.SourceUnits, &o.GrossProfit, &o.FromStationID, &o.ToStationID,
		&o.ClaimedBy, &o.ClaimedAt, &o.DiscoveredAt, &o.DiscoveredBy, &o.ExpiresAt, &o.Notes)
	if errors.Is(err, sql.ErrNoRows) {
		return o, fmt.Errorf("opportunity %d: %w", id, ErrOpportunityNotFound)
	}
	if err != nil {
		return o, fmt.Errorf("scan opportunity: %w", err)
	}

	o.UnitMargin = o.SellPrice - o.BuyPrice
	if o.BuyPrice > 0 {
		o.MarginPct = o.UnitMargin / o.BuyPrice * 100
	}
	o.Jumps = JumpsUnknown

	// Enrichment is best-effort: a missing or unreadable KB costs the drawer
	// its names and route, but must not fail the panel outright.
	if err := o.enrich(ctx, kbPath); err != nil {
		return o, nil //nolint:nilerr // names/route are optional; prices are not
	}
	return o, nil
}

// enrich fills endpoint names, systems, and the jump distance from the KB.
func (o *Opportunity) enrich(ctx context.Context, kbPath string) error {
	kb, err := sql.Open(sqliteDriver, "file:"+kbPath+"?mode=ro")
	if err != nil {
		return fmt.Errorf("open kb: %w", err)
	}
	defer kb.Close() //nolint:errcheck

	// Station ids arrive from two namespaces that are never comparable: POI ids
	// (from find_item) and BASE ids (from docked_at_base). Resolving only `pois`
	// leaves a base-id endpoint as a raw hex string with no system and so no
	// route -- exactly the detail the drawer exists to show. Bases are folded in
	// second so a base id maps through its poi_id to the same system.
	type place struct{ name, system string }
	loc := map[string]place{}
	sysName := map[string]string{}

	rows, err := kb.QueryContext(ctx, `SELECT id, COALESCE(name,'') FROM systems`)
	if err != nil {
		return fmt.Errorf("query systems: %w", err)
	}
	for rows.Next() {
		var id, name string
		if err = rows.Scan(&id, &name); err != nil {
			rows.Close() //nolint:errcheck,gosec
			return fmt.Errorf("scan system: %w", err)
		}
		sysName[id] = name
	}
	rows.Close() //nolint:errcheck,gosec

	prows, err := kb.QueryContext(ctx,
		`SELECT id, COALESCE(name,''), COALESCE(system_id,'') FROM pois`)
	if err != nil {
		return fmt.Errorf("query pois: %w", err)
	}
	poiSystem := map[string]string{}
	for prows.Next() {
		var id, name, sys string
		if err = prows.Scan(&id, &name, &sys); err != nil {
			prows.Close() //nolint:errcheck,gosec
			return fmt.Errorf("scan poi: %w", err)
		}
		loc[id] = place{name: name, system: sys}
		poiSystem[id] = sys
	}
	prows.Close() //nolint:errcheck,gosec

	brows, err := kb.QueryContext(ctx,
		`SELECT id, COALESCE(poi_id,''), COALESCE(name,'') FROM bases`)
	if err != nil {
		return fmt.Errorf("query bases: %w", err)
	}
	for brows.Next() {
		var id, poiID, name string
		if err = brows.Scan(&id, &poiID, &name); err != nil {
			brows.Close() //nolint:errcheck,gosec
			return fmt.Errorf("scan base: %w", err)
		}
		if _, taken := loc[id]; !taken {
			loc[id] = place{name: name, system: poiSystem[poiID]}
		}
	}
	brows.Close() //nolint:errcheck,gosec

	from, to := loc[o.FromStationID], loc[o.ToStationID]
	o.FromName, o.FromSystem = from.name, sysName[from.system]
	o.ToName, o.ToSystem = to.name, sysName[to.system]

	if from.system == "" || to.system == "" {
		return nil // unknown endpoint: Jumps stays JumpsUnknown
	}
	conns, err := loadConnections(ctx, kb)
	if err != nil {
		return err
	}
	graph := navigation.JumpGraphFromConnections(conns)
	d := navigation.BFSJumps(graph, from.system, []string{to.system})[to.system]
	if d >= navigation.RouteInf {
		return nil // unreachable: Jumps stays JumpsUnknown
	}
	o.Jumps = d
	if d > 0 {
		o.ProfitPerJump = o.GrossProfit / float64(d)
	}
	return nil
}

// loadConnections reads the jump graph, carrying the same one-way
// discriminator SQLiteKB.GetConnections uses so the drawer's jump count agrees
// with what routing actually believes. A lane whose stored distance equals the
// endpoints' spatial separation is an ordinary two-way link; one that differs
// is a Crimson wormhole, flyable in its stored direction alone. Reversing those
// is what once made a 14-jump run read as 5.
func loadConnections(ctx context.Context, kb *sql.DB) ([]knowledge.Connection, error) {
	rows, err := kb.QueryContext(ctx, `
		SELECT c.from_system, c.to_system, COALESCE(c.distance,0),
		       CASE WHEN c.distance > 0
		             AND f.position_x IS NOT NULL AND t.position_x IS NOT NULL
		             AND ABS(c.distance -
		                 SQRT((f.position_x - t.position_x) * (f.position_x - t.position_x) +
		                      (f.position_y - t.position_y) * (f.position_y - t.position_y))) >= 1.0
		            THEN 1 ELSE 0 END AS one_way
		FROM connections c
		LEFT JOIN systems f ON f.id = c.from_system
		LEFT JOIN systems t ON t.id = c.to_system`)
	if err != nil {
		return nil, fmt.Errorf("query connections: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []knowledge.Connection
	for rows.Next() {
		var (
			c      knowledge.Connection
			oneWay int
		)
		if err := rows.Scan(&c.FromSystem, &c.ToSystem, &c.Distance, &oneWay); err != nil {
			return nil, fmt.Errorf("scan connection: %w", err)
		}
		c.OneWay = oneWay != 0
		out = append(out, c)
	}
	return out, rows.Err()
}
