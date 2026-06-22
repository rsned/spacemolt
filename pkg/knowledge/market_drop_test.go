package knowledge

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMigration46DropsMarketTables(t *testing.T) {
	kb, err := NewSQLiteKB(Config{DBPath: filepath.Join(t.TempDir(), "k.db")})
	if err != nil {
		t.Fatalf("NewSQLiteKB: %v", err)
	}
	t.Cleanup(func() { _ = kb.Close() })

	for _, tbl := range []string{"market_snapshots", "market_listings", "market_analyses", "price_trends"} {
		var name string
		err := kb.db.QueryRowContext(context.Background(),
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&name)
		if err == nil {
			t.Errorf("table %s still exists after migration 46", tbl)
		}
	}
	// Tables that must remain.
	for _, tbl := range []string{"base_market", "market_buy_orders", "market_sell_orders"} {
		var name string
		if err := kb.db.QueryRowContext(context.Background(),
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&name); err != nil {
			t.Errorf("table %s should still exist: %v", tbl, err)
		}
	}
}
