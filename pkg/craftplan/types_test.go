package craftplan

import (
	"math"
	"testing"
)

func TestInventoryTotal(t *testing.T) {
	inv := Inventory{
		Cargo:   map[string]int{"iron_ore": 5},
		Storage: map[string]int{"iron_ore": 10, "copper_ore": 3},
		Faction: map[string]int{"iron_ore": 100},
	}
	if got := inv.total("iron_ore", false); got != 15 {
		t.Errorf("total iron_ore (no faction) = %d, want 15", got)
	}
	if got := inv.total("iron_ore", true); got != 115 {
		t.Errorf("total iron_ore (with faction) = %d, want 115", got)
	}
	if got := inv.total("copper_ore", true); got != 3 {
		t.Errorf("total copper_ore = %d, want 3", got)
	}
	if got := inv.total("missing", true); got != 0 {
		t.Errorf("total missing item = %d, want 0", got)
	}
}

func TestInfinityRepresentation(t *testing.T) {
	row := CraftableRow{CanMake: math.MaxInt}
	if row.CanMake <= 1_000_000_000 {
		t.Fatal("CanMake sentinel should be math.MaxInt, not a small number")
	}
}
