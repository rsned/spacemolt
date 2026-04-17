package main

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/game"
)

const staleThresholdTicks int64 = 8640 // ~1 day (8640 ticks = 24 hours at 10s/tick)

// handleNearestCommand finds the nearest POIs of a given type.
// Usage: nearest <poi_type>
// Example: nearest station
func handleNearestCommand(ctx context.Context, client game.GameClient, args []string, format outputFormat) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: nearest <poi_type>\nExample: nearest station")
	}

	poiType := strings.ToLower(args[0])

	if globalKB == nil {
		return fmt.Errorf("knowledge base not available (--db-path required)")
	}
	if globalClock == nil {
		return fmt.Errorf("game clock not available")
	}

	state := client.GetState()
	if state == nil || state.System.ID == "" {
		return fmt.Errorf("current system unknown")
	}

	currentSystem := state.System.ID

	g, err := globalGraphCache.GetOrCreate(ctx)
	if err != nil {
		return fmt.Errorf("failed to get galaxy graph: %w", err)
	}

	results, err := galaxy.FindNearestByPOIType(ctx, globalKB, g, currentSystem, poiType, 3)
	if err != nil {
		return fmt.Errorf("find nearest %s: %w", poiType, err)
	}

	if len(results) == 0 {
		if format == formatStyled {
			fmt.Printf("No accessible %s found in the galaxy.\n", poiType)
		}
		return nil
	}

	currentTick := globalClock.Tick()
	for i := range results {
		age := currentTick - results[i].LastUpdated
		if age > staleThresholdTicks {
			results[i].StaleWarning = fmt.Sprintf("⚠ Data from %d ticks ago", age)
		}
	}

	if format == formatStyled {
		output := formatNearestResultsStyled(currentSystem, state.System.Name, poiType, results)
		fmt.Print(output)
	} else {
		output := formatNearestResultsRaw(currentSystem, state.System.Name, poiType, results)
		fmt.Print(output)
	}

	return nil
}

// formatNearestResultsStyled formats nearest results in human-readable styled output.
func formatNearestResultsStyled(fromSystem, fromSystemName, queryType string, results []galaxy.NearestResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\nNearest accessible %s from %s:\n\n", queryType, fromSystemName))

	if len(results) == 0 {
		sb.WriteString("No results found.\n")
		return sb.String()
	}

	for i, r := range results {
		suffix := ""
		if r.IsHomeBase {
			suffix = " (your home base)"
		}
		sb.WriteString(fmt.Sprintf("  %d. %s (%s) — %d hops%s\n", i+1, r.SystemName, r.SystemID, r.Hops, suffix))

		ageText := formatAge(globalClock.Tick() - r.LastUpdated)
		if r.StaleWarning != "" {
			sb.WriteString(fmt.Sprintf("     %s %s\n", r.StaleWarning, ageText))
		} else {
			sb.WriteString(fmt.Sprintf("     Last updated: %s\n", ageText))
		}
	}
	sb.WriteString("\n")
	return sb.String()
}

// formatNearestResultsRaw formats nearest results as JSON.
func formatNearestResultsRaw(fromSystem, fromSystemName, queryType string, results []galaxy.NearestResult) string {
	var sb strings.Builder
	sb.WriteString("{\n")
	sb.WriteString(fmt.Sprintf("  \"from_system\": \"%s\",\n", fromSystem))
	sb.WriteString(fmt.Sprintf("  \"from_system_name\": \"%s\",\n", fromSystemName))
	sb.WriteString(fmt.Sprintf("  \"query_type\": \"%s\",\n", queryType))
	sb.WriteString("  \"results\": [\n")

	for i, r := range results {
		sb.WriteString("    {")
		sb.WriteString(fmt.Sprintf("\"system_id\": \"%s\", ", r.SystemID))
		sb.WriteString(fmt.Sprintf("\"system_name\": \"%s\", ", r.SystemName))
		sb.WriteString(fmt.Sprintf("\"hops\": %d, ", r.Hops))
		sb.WriteString(fmt.Sprintf("\"is_home_base\": %t, ", r.IsHomeBase))
		sb.WriteString(fmt.Sprintf("\"last_updated_tick\": %d", r.LastUpdated))
		if r.StaleWarning != "" {
			sb.WriteString(fmt.Sprintf(", \"stale_warning\": \"%s\"", r.StaleWarning))
		}
		sb.WriteString("}")
		if i < len(results)-1 {
			sb.WriteString(",\n")
		} else {
			sb.WriteString("\n")
		}
	}
	sb.WriteString("  ]\n")
	sb.WriteString("}\n")
	return sb.String()
}

// formatAge converts tick age to human-readable time.
func formatAge(ticks int64) string {
	if ticks < 0 {
		return "unknown"
	}
	if ticks < 3600 {
		return fmt.Sprintf("%d ticks ago", ticks)
	}
	hours := math.Ceil(float64(ticks) / 360.0)
	if hours < 48 {
		return fmt.Sprintf("~%.0f hours ago", hours)
	}
	days := math.Ceil(hours / 24.0)
	return fmt.Sprintf("~%.0f days ago", days)
}
