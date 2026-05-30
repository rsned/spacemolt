package knowledge

import (
	"context"
	"testing"
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
