package worker

import (
	"context"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

func TestHuntCreatureWorkHull(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	kb := knowledge.NewMemoryKB()
	if err := kb.StoreShipClasses(ctx, []knowledge.ShipClassDef{
		{ID: "gauntlet", Category: "Combat", Tier: 2},
		{ID: "warship", Category: "Combat", Tier: 4},
		{ID: "skirmisher", Category: "Combat", Tier: 1},
		{ID: "hauler", Category: "Industrial", Tier: 2},
		{ID: "escort", Category: "Combat Support", Tier: 3},
		{ID: "drillship", Tier: 1},
	}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		classID string
		want    bool
	}{
		{"T2 combat", "gauntlet", true},
		{"T4 combat", "warship", true},
		{"T1 combat", "skirmisher", false},
		{"T2 industrial", "hauler", false},
		{"combat support is not combat", "escort", false},
		{"legacy hull without category", "drillship", false},
		{"missing from catalog", "prospector", false},
		{"empty class id", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := huntCreatureWorkHull(ctx, kb, tc.classID); got != tc.want {
				t.Errorf("huntCreatureWorkHull(%q) = %v, want %v", tc.classID, got, tc.want)
			}
		})
	}
	if huntCreatureWorkHull(ctx, nil, "gauntlet") {
		t.Error("nil kb must return false")
	}
}
