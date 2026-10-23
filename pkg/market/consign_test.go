package market

import (
	"context"
	"strings"
	"testing"
	"time"
)

// seedConsignBook writes a capture for the collector's own station (no orders,
// so it resolves to a name) and a buy book for item at a second station.
func seedConsignBook(t *testing.T, c *Collector, item string, bids ...Order) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "voss_redoubt", StationName: "Voss Redoubt", SystemID: "alhena", SystemName: "Alhena", CapturedAt: now,
	}); err != nil {
		t.Fatalf("WriteSnapshot voss: %v", err)
	}
	for i := range bids {
		bids[i].StationID, bids[i].ItemID, bids[i].Side, bids[i].CapturedAt = "factory_belt", item, "buy", now
	}
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "factory_belt", StationName: "Factory Belt", SystemID: "sysF", SystemName: "Fabrica", CapturedAt: now,
		Orders: bids,
	}); err != nil {
		t.Fatalf("WriteSnapshot factory: %v", err)
	}
}

func alhenaStock(units float64, at time.Time) ConsignStock {
	return ConsignStock{
		Holder: "marketbot_alhena", StationID: "voss_redoubt", BaseID: "voss_redoubt_station",
		ItemID: "adamantite_ore", Units: units, UpdatedAt: at,
	}
}

var consignScanOpts = ScanOptions{MinProfit: 1, MinPrice: 1, MinQuantity: 1, ExpiresIn: time.Hour}

func consignedRows(t *testing.T, c *Collector) []ArbitrageOpportunity {
	t.Helper()
	opps, err := c.GetOpportunities(context.Background(), "available", 100)
	if err != nil {
		t.Fatalf("GetOpportunities: %v", err)
	}
	var out []ArbitrageOpportunity
	for _, o := range opps {
		if o.IsConsigned() {
			out = append(out, o)
		}
	}
	return out
}

// A collector's reported stock becomes a withdraw_then_sell route against the
// best bids elsewhere, sized to the smaller of stock and bid depth, carrying
// the holder and base the hauler needs to request the gift.
func TestScanArbitrageEmitsConsignedRoute(t *testing.T) {
	c := openArbDB(t)
	ctx := context.Background()
	seedConsignBook(t, c, "adamantite_ore",
		Order{PriceEach: 1600, Quantity: 1000},
		Order{PriceEach: 1500, Quantity: 5000})
	if err := c.UpsertConsignStock(ctx, alhenaStock(2838, time.Now().UTC())); err != nil {
		t.Fatalf("UpsertConsignStock: %v", err)
	}

	if _, err := c.ScanArbitrage(ctx, consignScanOpts); err != nil {
		t.Fatalf("ScanArbitrage: %v", err)
	}

	rows := consignedRows(t, c)
	if len(rows) != 1 {
		t.Fatalf("consigned rows = %+v, want 1", rows)
	}
	o := rows[0]
	if o.FromStationID != "voss_redoubt" || o.ToStationID != "factory_belt" || o.ItemID != "adamantite_ore" {
		t.Fatalf("route = %s -> %s %s", o.FromStationID, o.ToStationID, o.ItemID)
	}
	wantRevenue := 1600*1000 + 1500*1838.0
	if o.BuyPrice != 0 || o.Quantity != 2838 || o.SourceUnits != 2838 || o.GrossProfit != wantRevenue {
		t.Fatalf("row = buy %v qty %v source %v gross %v, want 0/2838/2838/%v", o.BuyPrice, o.Quantity, o.SourceUnits, o.GrossProfit, wantRevenue)
	}
	holder, base, ok := ConsignSource(o)
	if !ok || holder != "marketbot_alhena" || base != "voss_redoubt_station" {
		t.Fatalf("ConsignSource = %q %q %v", holder, base, ok)
	}
}

// Stock not re-reported recently is not offered: the collector may be gone or
// the goods already moved, and a hauler flown to Voss for nothing is the cost.
func TestScanArbitrageSkipsStaleConsignStock(t *testing.T) {
	c := openArbDB(t)
	ctx := context.Background()
	seedConsignBook(t, c, "adamantite_ore", Order{PriceEach: 1600, Quantity: 1000})
	if err := c.UpsertConsignStock(ctx, alhenaStock(500, time.Now().UTC().Add(-ConsignStockMaxAge-time.Minute))); err != nil {
		t.Fatalf("UpsertConsignStock: %v", err)
	}
	if _, err := c.ScanArbitrage(ctx, consignScanOpts); err != nil {
		t.Fatalf("ScanArbitrage: %v", err)
	}
	if rows := consignedRows(t, c); len(rows) != 0 {
		t.Fatalf("stale stock offered: %+v", rows)
	}
}

// A consigned claim made after the collector's last report is not yet
// reflected in its storage, so the scanner must not offer those units again.
// A claim from before the report is already reflected and must not be
// subtracted twice.
func TestScanArbitrageNetsUnreportedConsignClaims(t *testing.T) {
	c := openArbDB(t)
	ctx := context.Background()
	seedConsignBook(t, c, "adamantite_ore", Order{PriceEach: 1600, Quantity: 10000})
	report := time.Now().UTC().Add(-10 * time.Minute)
	if err := c.UpsertConsignStock(ctx, alhenaStock(1000, report)); err != nil {
		t.Fatalf("UpsertConsignStock: %v", err)
	}
	if _, err := c.ScanArbitrage(ctx, consignScanOpts); err != nil {
		t.Fatalf("ScanArbitrage: %v", err)
	}
	first := consignedRows(t, c)[0]
	if ok, err := c.ClaimOpportunity(ctx, first.ID, "hauler-1"); err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	// The claim landed after the report; its quantity is the whole 1000, so
	// nothing is left to offer.
	if _, err := c.ScanArbitrage(ctx, consignScanOpts); err != nil {
		t.Fatalf("ScanArbitrage 2: %v", err)
	}
	if rows := consignedRows(t, c); len(rows) != 0 {
		t.Fatalf("units promised to hauler-1 offered again: %+v", rows)
	}

	// The collector reports again after gifting: storage now shows 300 left,
	// and the earlier claim predates this report, so all 300 are offered.
	if err := c.UpsertConsignStock(ctx, alhenaStock(300, time.Now().UTC().Add(time.Second))); err != nil {
		t.Fatalf("UpsertConsignStock 2: %v", err)
	}
	if _, err := c.ScanArbitrage(ctx, consignScanOpts); err != nil {
		t.Fatalf("ScanArbitrage 3: %v", err)
	}
	rows := consignedRows(t, c)
	if len(rows) != 1 || rows[0].Quantity != 300 {
		t.Fatalf("after re-report rows = %+v, want one of 300", rows)
	}
}

// Consigned routes are few and cost nothing to source, so the Limit cap on
// ordinary candidates must never crowd them out.
func TestScanArbitrageLimitKeepsConsignedRoutes(t *testing.T) {
	c := openArbDB(t)
	ctx := context.Background()
	seedConsignBook(t, c, "adamantite_ore", Order{PriceEach: 1600, Quantity: 1000})
	now := time.Now().UTC()
	// An ordinary spread that outranks nothing in particular but fills Limit=1.
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stnA", StationName: "Alpha", SystemID: "sysA", SystemName: "Sol", CapturedAt: now,
		Orders: []Order{{StationID: "stnA", ItemID: "iron_ore", Side: "sell", PriceEach: 5, Quantity: 10, CapturedAt: now}},
	}); err != nil {
		t.Fatalf("WriteSnapshot stnA: %v", err)
	}
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stnB", StationName: "Beta", SystemID: "sysB", SystemName: "Sirius", CapturedAt: now,
		Orders: []Order{{StationID: "stnB", ItemID: "iron_ore", Side: "buy", PriceEach: 8, Quantity: 5, CapturedAt: now}},
	}); err != nil {
		t.Fatalf("WriteSnapshot stnB: %v", err)
	}
	if err := c.UpsertConsignStock(ctx, alhenaStock(50, now)); err != nil {
		t.Fatalf("UpsertConsignStock: %v", err)
	}
	opts := consignScanOpts
	opts.Limit = 1
	if _, err := c.ScanArbitrage(ctx, opts); err != nil {
		t.Fatalf("ScanArbitrage: %v", err)
	}
	if rows := consignedRows(t, c); len(rows) != 1 {
		t.Fatalf("consigned rows = %+v, want 1 despite Limit=1", rows)
	}
}

// A report of zero units (everything gifted or kept) replaces the old row, so
// nothing is offered.
func TestUpsertConsignStockReplacesUnits(t *testing.T) {
	c := openArbDB(t)
	ctx := context.Background()
	seedConsignBook(t, c, "adamantite_ore", Order{PriceEach: 1600, Quantity: 1000})
	now := time.Now().UTC()
	if err := c.UpsertConsignStock(ctx, alhenaStock(400, now)); err != nil {
		t.Fatalf("UpsertConsignStock: %v", err)
	}
	if err := c.UpsertConsignStock(ctx, alhenaStock(0, now.Add(time.Second))); err != nil {
		t.Fatalf("UpsertConsignStock 0: %v", err)
	}
	if _, err := c.ScanArbitrage(ctx, consignScanOpts); err != nil {
		t.Fatalf("ScanArbitrage: %v", err)
	}
	if rows := consignedRows(t, c); len(rows) != 0 {
		t.Fatalf("zero stock offered: %+v", rows)
	}
}

// On a table created before consignment (CHECK limited to the buy-side
// types), the scanner must skip consigned routes rather than attempt an
// insert the CHECK rejects — that would roll back the whole scan and leave
// every hauler with an empty pool. Ordinary routes still land.
func TestScanArbitrageSkipsConsignOnPreConsignTable(t *testing.T) {
	c := openArbDB(t)
	ctx := context.Background()
	var ddl string
	if err := c.db.QueryRow(`SELECT sql FROM sqlite_master WHERE name='arbitrage_opportunities'`).Scan(&ddl); err != nil {
		t.Fatalf("read ddl: %v", err)
	}
	old := strings.Replace(ddl, ", 'withdraw_then_sell'", "", 1)
	if old == ddl {
		t.Fatalf("fresh schema does not admit withdraw_then_sell: %s", ddl)
	}
	if _, err := c.db.Exec(`DROP TABLE arbitrage_opportunities`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := c.db.Exec(old); err != nil {
		t.Fatalf("recreate old table: %v", err)
	}

	seedConsignBook(t, c, "adamantite_ore", Order{PriceEach: 1600, Quantity: 1000})
	now := time.Now().UTC()
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stnA", StationName: "Alpha", SystemID: "sysA", SystemName: "Sol", CapturedAt: now,
		Orders: []Order{{StationID: "stnA", ItemID: "iron_ore", Side: "sell", PriceEach: 5, Quantity: 10, CapturedAt: now}},
	}); err != nil {
		t.Fatalf("WriteSnapshot stnA: %v", err)
	}
	if err := c.WriteSnapshot(ctx, MarketSnapshot{
		StationID: "stnB", StationName: "Beta", SystemID: "sysB", SystemName: "Sirius", CapturedAt: now,
		Orders: []Order{{StationID: "stnB", ItemID: "iron_ore", Side: "buy", PriceEach: 8, Quantity: 5, CapturedAt: now}},
	}); err != nil {
		t.Fatalf("WriteSnapshot stnB: %v", err)
	}
	if err := c.UpsertConsignStock(ctx, alhenaStock(500, now)); err != nil {
		t.Fatalf("UpsertConsignStock: %v", err)
	}

	res, err := c.ScanArbitrage(ctx, consignScanOpts)
	if err != nil {
		t.Fatalf("ScanArbitrage on old table: %v", err)
	}
	if res.Inserted != 1 {
		t.Fatalf("inserted = %d, want the 1 ordinary route", res.Inserted)
	}
	if rows := consignedRows(t, c); len(rows) != 0 {
		t.Fatalf("consigned rows on old table: %+v", rows)
	}
}
