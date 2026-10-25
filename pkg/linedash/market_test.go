package linedash

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/market"
)

type orderRow struct {
	station, item, side string
	price, qty, mine    float64
	source, captured    string
}

func seedMarket(t *testing.T, rows []orderRow) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "market.db")
	c, err := market.Open(market.Config{DBPath: path, WAL: true})
	if err != nil {
		t.Fatalf("market.Open: %v", err)
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close() //nolint:errcheck
	stmts := []string{
		`INSERT INTO stations VALUES ('st1','Station One','sys1','Sys One','2026-10-01T00:00:00Z','2026-10-01T00:00:00Z')`,
		`INSERT INTO stations VALUES ('st2','Station Two','sys1','Sys One','2026-10-01T00:00:00Z','2026-10-01T00:00:00Z')`,
		`INSERT INTO items VALUES ('moonshine','Moonshine','x','2026-10-01T00:00:00Z','2026-10-01T00:00:00Z')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("seed %q: %v", s, err)
		}
	}
	for _, r := range rows {
		var src any
		if r.source != "" {
			src = r.source
		}
		if _, err := db.Exec(`INSERT INTO market_orders(station_id,item_id,side,price_each,quantity,my_quantity,source,captured_at,bucket_utc)
VALUES (?,?,?,?,?,?,?,?,?)`, r.station, r.item, r.side, r.price, r.qty, r.mine, src, r.captured, r.captured); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	return path
}

func openTestMarket(t *testing.T, rows []orderRow, agentsDir string) *Market {
	t.Helper()
	m, err := OpenMarket(context.Background(), seedMarket(t, rows), agentsDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })

	return m
}

func TestDemandExcludesWorkerSource(t *testing.T) {
	ts := "2026-10-24T10:00:00Z"
	m := openTestMarket(t, []orderRow{
		{"st1", "moonshine", "buy", 1, 59126, 0, "", ts},
		{"st1", "moonshine", "buy", 1, 50000, 0, "worker", ts},
		{"st1", "moonshine", "buy", 9, 7, 0, "station", ts},
	}, t.TempDir())
	d, err := m.Demand(context.Background(), "st1", "moonshine")
	if err != nil {
		t.Fatal(err)
	}
	if d.BidUnits != 59133 || d.BestBid != 9 || d.Station != "st1" {
		t.Fatalf("got %+v", d)
	}
	if !d.CapturedAt.Equal(time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("CapturedAt %v", d.CapturedAt)
	}
}

func TestDemandUsesLatestCaptureOnly(t *testing.T) {
	m := openTestMarket(t, []orderRow{
		{"st1", "moonshine", "buy", 5, 100, 0, "", "2026-10-24T09:00:00Z"},
		{"st1", "moonshine", "buy", 2, 30, 0, "", "2026-10-24T10:00:00Z"},
		{"st1", "moonshine", "buy", 3, 40, 0, "", "2026-10-24T10:00:00Z"},
		// a newer worker-only capture must not blank genuine demand
		{"st1", "moonshine", "buy", 1, 999, 0, "worker", "2026-10-24T11:00:00Z"},
	}, t.TempDir())
	d, err := m.Demand(context.Background(), "st1", "moonshine")
	if err != nil {
		t.Fatal(err)
	}
	if d.BidUnits != 70 || d.BestBid != 3 {
		t.Fatalf("got %+v", d)
	}
}

func TestDemandNeverCaptured(t *testing.T) {
	m := openTestMarket(t, nil, t.TempDir())
	d, err := m.Demand(context.Background(), "st2", "moonshine")
	if err != nil {
		t.Fatal(err)
	}
	if d != (Demand{Station: "st2"}) {
		t.Fatalf("got %+v", d)
	}
}

const shopJSON = `{"Items":{"moonshine":{"Price":5500,"OrderID":"abc123","PostedAt":"2026-10-24T20:12:16.412131375-07:00","LastFillAt":"0001-01-01T00:00:00Z"}}}`

func writeShop(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a1", "shopkeep.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestMarketOurAskPrefersExchangeRow(t *testing.T) {
	m := openTestMarket(t, []orderRow{
		{"st1", "moonshine", "sell", 2900, 4, 10, "", "2026-10-24T09:00:00Z"},
		{"st1", "moonshine", "sell", 3000, 20, 15, "station", "2026-10-24T10:00:00Z"},
		{"st1", "moonshine", "sell", 100, 20, 0, "", "2026-10-24T11:00:00Z"},
	}, writeShop(t, shopJSON))
	a, ok, err := m.OurAsk(context.Background(), "a1", "st1", "moonshine")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if a.Price != 3000 || a.Units != 15 || a.OrderID != "abc123" {
		t.Fatalf("got %+v", a)
	}
	want := time.Date(2026, 10, 25, 3, 12, 16, 412131375, time.UTC)
	if !a.PostedAt.Equal(want) {
		t.Fatalf("PostedAt %v want %v", a.PostedAt, want)
	}
	if !a.SeenAt.Equal(time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("SeenAt %v", a.SeenAt)
	}
}

func TestOurAskJSONOnly(t *testing.T) {
	m := openTestMarket(t, nil, writeShop(t, shopJSON))
	a, ok, err := m.OurAsk(context.Background(), "a1", "st1", "moonshine")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if a.Price != 5500 || a.Units != 0 || a.OrderID != "abc123" || a.PostedAt.IsZero() {
		t.Fatalf("got %+v", a)
	}
}

func TestOurAskJSONWithoutOrderIsFalse(t *testing.T) {
	m := openTestMarket(t, nil, writeShop(t, `{"Items":{"moonshine":{"Price":5500,"OrderID":""}}}`))
	if _, ok, err := m.OurAsk(context.Background(), "a1", "st1", "moonshine"); err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestOurAskNoRowNoJsonIsFalse(t *testing.T) {
	for name, dir := range map[string]string{"missing": t.TempDir(), "corrupt": writeShop(t, "{not json")} {
		t.Run(name, func(t *testing.T) {
			m := openTestMarket(t, nil, dir)
			a, ok, err := m.OurAsk(context.Background(), "a1", "st1", "moonshine")
			if err != nil || ok || a != (OurAsk{Station: "st1"}) {
				t.Fatalf("a=%+v ok=%v err=%v", a, ok, err)
			}
		})
	}
}

// The shopkeeper saw the ask fill after the last capture that showed it: the
// row is a sold-out ask, not a standing one.
func TestOurAskDropsSoldOutRow(t *testing.T) {
	m := openTestMarket(t, []orderRow{
		{"st1", "moonshine", "sell", 3000, 20, 15, "", "2026-10-24T10:00:00Z"},
	}, writeShop(t, `{"Items":{"moonshine":{"Price":3000,"OrderID":"","LastFillAt":"2026-10-24T10:30:00Z"}}}`))
	a, ok, err := m.OurAsk(context.Background(), "a1", "st1", "moonshine")
	if err != nil || ok || a != (OurAsk{Station: "st1"}) {
		t.Fatalf("a=%+v ok=%v err=%v, want no ask", a, ok, err)
	}
}

// A fill older than the capture says nothing about the row: it is kept.
func TestOurAskKeepsRowSeenAfterLastFill(t *testing.T) {
	m := openTestMarket(t, []orderRow{
		{"st1", "moonshine", "sell", 3000, 20, 15, "", "2026-10-24T10:00:00Z"},
	}, writeShop(t, `{"Items":{"moonshine":{"Price":3000,"OrderID":"","LastFillAt":"2026-10-24T09:30:00Z"}}}`))
	a, ok, err := m.OurAsk(context.Background(), "a1", "st1", "moonshine")
	if err != nil || !ok || a.Price != 3000 || a.Units != 15 {
		t.Fatalf("a=%+v ok=%v err=%v, want the row", a, ok, err)
	}
}

func TestOurAskCorruptJSONStillUsesExchangeRow(t *testing.T) {
	m := openTestMarket(t, []orderRow{
		{"st1", "moonshine", "sell", 3000, 20, 15, "", "2026-10-24T10:00:00Z"},
	}, writeShop(t, "{not json"))
	a, ok, err := m.OurAsk(context.Background(), "a1", "st1", "moonshine")
	if err != nil || !ok || a.Price != 3000 || !a.PostedAt.IsZero() {
		t.Fatalf("a=%+v ok=%v err=%v", a, ok, err)
	}
}
