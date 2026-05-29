package knowledge

import (
	"context"
	"testing"
)

func TestMigration36CreatesDemandTables(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	for _, table := range []string{"market_buy_demand", "market_buy_orders"} {
		var name string
		err := kb.db.QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil {
			t.Fatalf("table %q not found: %v", table, err)
		}
	}
}
