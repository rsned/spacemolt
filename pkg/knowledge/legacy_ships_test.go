package knowledge

import (
	"context"
	"testing"
)

// TestCatalogRefreshKeepsLegacyClasses is the regression. StoreShipClasses replaces
// the table wholesale, so every catalog refresh erased the classes the server no
// longer publishes — which happen to be the four most-flown hulls in the fleet.
func TestCatalogRefreshKeepsLegacyClasses(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()

	// A refresh carrying only current classes, exactly as the live catalog returns.
	if err := kb.StoreShipClasses(ctx, []ShipClassDef{
		{ID: "sparrow", Name: "Sparrow", CargoCapacity: 35, Scale: 1, BaseSpeed: 3},
	}); err != nil {
		t.Fatalf("store: %v", err)
	}

	for _, id := range []string{"prospector", "drillship", "excavator", "deeprock_harvester"} {
		var n int
		if err := kb.db.QueryRow(`SELECT COUNT(*) FROM ships WHERE id=?`, id).Scan(&n); err != nil {
			t.Fatalf("query %s: %v", id, err)
		}
		if n != 1 {
			t.Fatalf("%s must survive a catalog refresh, found %d rows", id, n)
		}
	}
}

// TestLegacyClassesCarryTheFieldsThatMatter guards against a seed that exists but is
// hollow: scale and speed drive jump fuel (ceil(scale^1.5 x speed)) and jump time, so
// a zero there is worse than a missing row — it computes a confident wrong answer.
func TestLegacyClassesCarryTheFieldsThatMatter(t *testing.T) {
	classes, err := LegacyShipClasses()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(classes) == 0 {
		t.Fatal("no legacy classes recovered")
	}
	for _, c := range classes {
		if c.Scale <= 0 || c.BaseSpeed <= 0 || c.CargoCapacity <= 0 || c.BaseFuel <= 0 {
			t.Errorf("%s is missing load-bearing stats: scale=%d speed=%d cargo=%d fuel=%d",
				c.ID, c.Scale, c.BaseSpeed, c.CargoCapacity, c.BaseFuel)
		}
		if c.Name == "" {
			t.Errorf("%s has no name", c.ID)
		}
	}
}

// TestLiveCatalogWinsOverTheRecoveredCopy: if the server starts publishing a class
// again, its definition is authoritative — the recovered copy must not shadow it.
func TestLiveCatalogWinsOverTheRecoveredCopy(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	if err := kb.StoreShipClasses(ctx, []ShipClassDef{
		{ID: "drillship", Name: "Drillship Mk II", CargoCapacity: 999, Scale: 1, BaseSpeed: 2},
	}); err != nil {
		t.Fatalf("store: %v", err)
	}
	var name string
	var cargo int
	if err := kb.db.QueryRow(`SELECT name, cargo_capacity FROM ships WHERE id='drillship'`).Scan(&name, &cargo); err != nil {
		t.Fatalf("query: %v", err)
	}
	if name != "Drillship Mk II" || cargo != 999 {
		t.Fatalf("live definition must win, got %q cargo=%d", name, cargo)
	}
	var n int
	if err := kb.db.QueryRow(`SELECT COUNT(*) FROM ships WHERE id='drillship'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("a republished class must not be duplicated, got %d rows", n)
	}
}
