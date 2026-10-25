package linedash

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // database/sql driver for market.db
)

// Demand is the standing buy-side demand for one item at one station.
type Demand struct {
	Station    string
	BidUnits   float64   // sum of quantity over the latest non-worker buy capture
	BestBid    float64   // max price_each among those rows
	CapturedAt time.Time // zero when the station was never captured
}

// OurAsk is our own standing sell order for one item at one station.
type OurAsk struct {
	Station  string
	Price    float64   // from the exchange row when one exists
	Units    float64   // my_quantity of the exchange row; 0 when only shopkeep.json knows
	SeenAt   time.Time // captured_at of the exchange row
	PostedAt time.Time // from shopkeep.json; zero when unknown
	OrderID  string    // from shopkeep.json
}

// shopState mirrors worker.ShopState (the shopkeep.json shape). pkg/worker is
// not imported because it pulls in game, knowledge, navigation, rescue and
// other heavy packages the dashboard does not need.
type shopState struct {
	Items map[string]shopItemState
}

// shopItemState mirrors worker.ShopItemState (only the fields read here).
type shopItemState struct {
	Price    int
	OrderID  string
	PostedAt time.Time
}

// Market reads order books from market.db, read-only.
type Market struct {
	db        *sql.DB
	agentsDir string
}

// OpenMarket opens market.db read-only. agentsDir holds <agent>/shopkeep.json.
func OpenMarket(_ context.Context, marketPath, agentsDir string) (*Market, error) {
	db, err := openReadOnly(marketPath)
	if err != nil {
		return nil, err
	}

	return &Market{db: db, agentsDir: agentsDir}, nil
}

// Close releases the database handle.
func (m *Market) Close() error { return m.db.Close() }

// Demand sums the buy-side quantity of the latest capture for (station, item),
// ignoring our own workers' bids. A never-captured station yields zeros.
func (m *Market) Demand(ctx context.Context, station, item string) (Demand, error) {
	d := Demand{Station: station}
	const q = `
SELECT COALESCE(SUM(quantity), 0), COALESCE(MAX(price_each), 0), MAX(captured_at)
FROM market_orders
WHERE station_id = ? AND item_id = ? AND side = 'buy' AND COALESCE(source, '') <> 'worker'
  AND captured_at = (SELECT MAX(captured_at) FROM market_orders
                     WHERE station_id = ? AND item_id = ? AND side = 'buy' AND COALESCE(source, '') <> 'worker')`
	var captured sql.NullString
	if err := m.db.QueryRowContext(ctx, q, station, item, station, item).
		Scan(&d.BidUnits, &d.BestBid, &captured); err != nil {
		return d, fmt.Errorf("demand %s/%s: %w", station, item, err)
	}
	if captured.Valid {
		d.CapturedAt = parseTime(captured.String)
	}

	return d, nil
}

// OurAsk returns our standing ask for (station, item). The exchange row is the
// truth for price and units; shopkeep.json supplies the order id and posted
// time. ok is false when neither source knows an ask.
func (m *Market) OurAsk(ctx context.Context, agent, station, item string) (OurAsk, bool, error) {
	a := OurAsk{Station: station}
	const q = `
SELECT price_each, my_quantity, captured_at FROM market_orders
WHERE station_id = ? AND item_id = ? AND side = 'sell' AND my_quantity > 0
ORDER BY captured_at DESC, id DESC LIMIT 1`
	var captured string
	haveRow := true
	err := m.db.QueryRowContext(ctx, q, station, item).Scan(&a.Price, &a.Units, &captured)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		haveRow = false
	case err != nil:
		return OurAsk{Station: station}, false, fmt.Errorf("our ask %s/%s: %w", station, item, err)
	default:
		a.SeenAt = parseTime(captured)
	}

	st, haveJSON := m.shopItem(agent, item)
	if haveJSON {
		a.PostedAt = st.PostedAt
		a.OrderID = st.OrderID
	}
	if haveRow {
		return a, true, nil
	}
	if haveJSON && st.OrderID != "" {
		a.Price = float64(st.Price)

		return a, true, nil
	}

	return OurAsk{Station: station}, false, nil
}

// shopItem reads one item from the agent's shopkeep.json. A missing or
// unparsable file means nothing is known.
func (m *Market) shopItem(agent, item string) (shopItemState, bool) {
	raw, err := os.ReadFile(filepath.Join(m.agentsDir, agent, "shopkeep.json"))
	if err != nil {
		return shopItemState{}, false
	}
	var s shopState
	if err := json.Unmarshal(raw, &s); err != nil {
		return shopItemState{}, false
	}
	st, ok := s.Items[item]

	return st, ok
}
