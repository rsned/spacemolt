package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// runWhereFacility implements: where_facility <recipe_id>
// Lists public production facilities galaxy-wide that craft the recipe.
func runWhereFacility(client game.GameClient, ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: where_facility <recipe_id>")
	}
	recipeID := args[0]
	if globalKB == nil {
		return fmt.Errorf("where_facility: knowledge DB not available (run with --db-path)")
	}
	sk, ok := globalKB.(*knowledge.SQLiteKB)
	if !ok {
		return fmt.Errorf("where_facility: knowledge DB does not support facility queries")
	}
	rows, err := sk.FacilitiesForRecipe(ctx, recipeID)
	if err != nil {
		return err
	}
	var currentTick int64
	if globalClock != nil {
		currentTick = globalClock.Tick()
	}
	fmt.Print(formatWhereFacility(recipeID, rows, currentTick))
	return nil
}

// formatWhereFacility renders the facility table. currentTick<=0 means unknown
// (age shown as "?").
func formatWhereFacility(recipeID string, rows []knowledge.PublicFacility, currentTick int64) string {
	if len(rows) == 0 {
		return fmt.Sprintf("No public facility known that crafts %s.\n(Coverage comes from marketbot facility sweeps — the recipe may be craftable somewhere untracked.)\n", recipeID)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Public facilities crafting %s (%d):\n", recipeID, len(rows))
	fmt.Fprintf(&b, "  %-24s  %-4s  %-9s  %-8s  %s\n", "STATION", "TIER", "FEE/RUN", "OWNER", "AGE(ticks)")
	for _, f := range rows {
		// Output-rate multiplier from level: output × 3^(level-1).
		mult := 1
		for i := 1; i < f.Level; i++ {
			mult *= 3
		}
		owner := f.OwnerFaction
		if owner == "" {
			owner = "—"
		}
		age := "?"
		if currentTick > 0 {
			age = fmt.Sprintf("%d", currentTick-int64(f.LastSeenTick))
		}
		fmt.Fprintf(&b, "  %-24s  T%-3d  %-9d  %-8s  %s  (×%d)\n",
			f.StationID, f.Level, f.RentalFeePerRun, owner, age, mult)
	}
	return b.String()
}
