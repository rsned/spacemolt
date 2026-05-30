package knowledge

import (
	"context"
	"testing"
	"time"
)

func TestMigration38CreatesDemandHistoryTable(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	var name string
	if err := kb.db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, "market_demand_history").Scan(&name); err != nil {
		t.Fatalf("table market_demand_history not found: %v", err)
	}
}

func TestRecordDemandHistoryUpsertWithinBucket(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	bucket := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)

	rec := func(best, total float64, count int, capAt time.Time) {
		if err := kb.RecordDemandHistory(ctx, []DemandHistorySample{
			{StationID: "stn1", SystemID: "sys1", ItemID: "iron_ore", ItemName: "Iron Ore",
				BucketAt: bucket, CapturedAt: capAt,
				BestPrice: best, TotalQty: total, SMBestPrice: best, SMQty: total, OrderCount: count},
		}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	rec(10, 50, 2, bucket.Add(5*time.Minute))
	rec(12, 60, 3, bucket.Add(40*time.Minute)) // same bucket -> upsert in place

	var rows, count int
	var best, total float64
	if err := kb.db.QueryRowContext(ctx,
		`SELECT COUNT(*), MAX(best_price), MAX(total_qty), MAX(order_count)
		   FROM market_demand_history WHERE station_id=? AND item_id=? AND bucket_utc=?`,
		"stn1", "iron_ore", bucket.UTC().Format(time.RFC3339)).Scan(&rows, &best, &total, &count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if rows != 1 {
		t.Fatalf("want 1 row after same-bucket upsert, got %d", rows)
	}
	if best != 12 || total != 60 || count != 3 {
		t.Errorf("upsert did not take latest values: best=%v total=%v count=%v", best, total, count)
	}

	// A new bucket appends a second row rather than replacing.
	bucket2 := bucket.Add(time.Hour)
	if err := kb.RecordDemandHistory(ctx, []DemandHistorySample{
		{StationID: "stn1", ItemID: "iron_ore", BucketAt: bucket2, CapturedAt: bucket2, BestPrice: 9, TotalQty: 30, OrderCount: 1},
	}); err != nil {
		t.Fatalf("record new bucket: %v", err)
	}
	if err := kb.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM market_demand_history WHERE station_id=? AND item_id=?`,
		"stn1", "iron_ore").Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 2 {
		t.Errorf("want 2 rows across two buckets, got %d", rows)
	}
}

func TestLatestDemandCapture(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	// No data yet -> (zero, false, nil).
	if last, ok, err := kb.LatestDemandCapture(ctx, "stn1"); err != nil || ok || !last.IsZero() {
		t.Fatalf("empty: want (zero, false, nil), got last=%v ok=%v err=%v", last, ok, err)
	}

	t0 := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(10 * time.Minute)
	if err := kb.ReplaceStationBuyOrders(ctx, "stn1", []MarketBuyOrderRow{
		{StationID: "stn1", ItemID: "iron_ore", PriceEach: 10, Quantity: 50, Source: "station", CapturedAt: t0},
		{StationID: "stn1", ItemID: "copper", PriceEach: 8, Quantity: 20, Source: "station", CapturedAt: t1},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	last, ok, err := kb.LatestDemandCapture(ctx, "stn1")
	if err != nil || !ok {
		t.Fatalf("want (_, true, nil), got ok=%v err=%v", ok, err)
	}
	if !last.Equal(t1) {
		t.Errorf("want latest %v, got %v", t1, last)
	}
}

func TestLoadDemandHistoryFilterAndCap(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	base := time.Date(2026, 5, 30, 8, 0, 0, 0, time.UTC)

	var samples []DemandHistorySample
	for h := 0; h < 4; h++ {
		b := base.Add(time.Duration(h) * time.Hour)
		samples = append(samples,
			DemandHistorySample{StationID: "stnA", ItemID: "iron_ore", BucketAt: b, CapturedAt: b, BestPrice: float64(10 + h), TotalQty: 50, OrderCount: 1},
			DemandHistorySample{StationID: "stnB", ItemID: "iron_ore", BucketAt: b, CapturedAt: b, BestPrice: float64(20 + h), TotalQty: 30, OrderCount: 1},
			DemandHistorySample{StationID: "stnA", ItemID: "copper", BucketAt: b, CapturedAt: b, BestPrice: 5, TotalQty: 99, OrderCount: 1},
		)
	}
	if err := kb.RecordDemandHistory(ctx, samples); err != nil {
		t.Fatalf("record: %v", err)
	}

	// Filter by item only: iron_ore across both stations = 8 rows.
	got, err := kb.LoadDemandHistory(ctx, "iron_ore", "", 0)
	if err != nil {
		t.Fatalf("load all: %v", err)
	}
	if len(got) != 8 {
		t.Fatalf("iron_ore all stations: want 8, got %d", len(got))
	}

	// Narrow to one station.
	got, err = kb.LoadDemandHistory(ctx, "iron_ore", "stnB", 0)
	if err != nil {
		t.Fatalf("load station: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("iron_ore stnB: want 4, got %d", len(got))
	}
	for _, s := range got {
		if s.StationID != "stnB" {
			t.Errorf("station filter leaked: %+v", s)
		}
	}

	// Per-station cap = 2 keeps the 2 most recent buckets of each station, ascending.
	got, err = kb.LoadDemandHistory(ctx, "iron_ore", "", 2)
	if err != nil {
		t.Fatalf("load cap: %v", err)
	}
	if len(got) != 4 { // 2 per station x 2 stations
		t.Fatalf("cap=2: want 4, got %d", len(got))
	}
	// stnA sorts first; its two most recent buckets are hours 2 and 3 (prices 12,13), ascending.
	if got[0].StationID != "stnA" || got[0].BestPrice != 12 || got[1].BestPrice != 13 {
		t.Errorf("stnA cap window wrong: %+v %+v", got[0], got[1])
	}
}
