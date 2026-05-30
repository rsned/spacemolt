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
