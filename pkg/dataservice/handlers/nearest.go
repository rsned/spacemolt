// Package handlers provides concrete Handler implementations for the
// dataservice query registry.
package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/rsned/spacemolt/pkg/dataservice"
	"github.com/rsned/spacemolt/pkg/galaxy"
)

// Nearest answers "find the N nearest accessible systems with a POI of type X".
type Nearest struct {
	// Limit is the max number of results to return. Defaults to 3 when 0.
	Limit int
}

// Name implements dataservice.Handler.
func (n *Nearest) Name() string { return "nearest" }

// ShortHelp implements dataservice.Handler.
func (n *Nearest) ShortHelp() string {
	return "Find nearest accessible POIs of a given type"
}

// PlaintextUsage implements dataservice.Handler.
func (n *Nearest) PlaintextUsage() string {
	return "nearest <poi_type> from <system_id>"
}

// JSONExample implements dataservice.Handler.
func (n *Nearest) JSONExample() map[string]any {
	return map[string]any{
		"query": "nearest",
		"params": map[string]any{
			"poi_type":    "station",
			"from_system": "sol-3",
		},
	}
}

func (n *Nearest) limit() int {
	if n.Limit > 0 {
		return n.Limit
	}
	return 3
}

// HandlePlaintext implements dataservice.Handler. Grammar:
//
//	nearest <poi_type> from <system_id>
func (n *Nearest) HandlePlaintext(ctx context.Context, deps dataservice.Deps, args []string) (string, error) {
	if len(args) < 3 {
		return "", dataservice.ErrParse(`usage: nearest <poi_type> from <system_id>`)
	}
	poiType := strings.ToLower(args[0])
	if strings.ToLower(args[1]) != "from" {
		return "", dataservice.ErrParse(`usage: nearest <poi_type> from <system_id>`)
	}
	fromSystem := args[2]

	results, err := galaxy.FindNearestByPOIType(ctx, deps.KB, deps.Graph, fromSystem, poiType, n.limit())
	if err != nil {
		return "", fmt.Errorf("nearest lookup: %w", err)
	}

	if len(results) == 0 {
		return fmt.Sprintf("No accessible %s found from %s.", poiType, fromSystem), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Nearest accessible %s from %s:\n", poiType, fromSystem)
	for i, r := range results {
		name := r.SystemName
		if name == "" {
			name = r.SystemID
		}
		hopWord := "hops"
		if r.Hops == 1 {
			hopWord = "hop"
		}
		fmt.Fprintf(&sb, "  %d. %s (%s) — %d %s", i+1, name, r.SystemID, r.Hops, hopWord)
		if age := ageText(deps, r.LastUpdated); age != "" {
			sb.WriteString(", updated ")
			sb.WriteString(age)
		}
		sb.WriteString("\n")
	}
	return dataservice.TruncateReply(sb.String()), nil
}

// HandleJSON implements dataservice.Handler.
func (n *Nearest) HandleJSON(ctx context.Context, deps dataservice.Deps, params map[string]any) (map[string]any, error) {
	poiType, _ := params["poi_type"].(string)
	fromSystem, _ := params["from_system"].(string)
	if poiType == "" {
		return nil, dataservice.ErrParse("missing required field: poi_type")
	}
	if fromSystem == "" {
		return nil, dataservice.ErrParse("missing required field: from_system")
	}

	results, err := galaxy.FindNearestByPOIType(ctx, deps.KB, deps.Graph, fromSystem, strings.ToLower(poiType), n.limit())
	if err != nil {
		return nil, fmt.Errorf("nearest lookup: %w", err)
	}

	out := make([]map[string]any, 0, len(results))
	for _, r := range results {
		out = append(out, map[string]any{
			"system_id":         r.SystemID,
			"system_name":       r.SystemName,
			"hops":              r.Hops,
			"last_updated_tick": r.LastUpdated,
		})
	}

	return map[string]any{
		"from_system": fromSystem,
		"poi_type":    poiType,
		"results":     out,
	}, nil
}

// ageText returns a short "~2h ago" / "~1d ago" suffix or empty string.
func ageText(deps dataservice.Deps, lastTick int64) string {
	if deps.Tick == nil || lastTick == 0 {
		return ""
	}
	now := deps.Tick()
	if now <= lastTick {
		return ""
	}
	ticks := now - lastTick
	if ticks < 360 {
		return fmt.Sprintf("%dt ago", ticks)
	}
	hours := ticks / 360
	if hours < 48 {
		return fmt.Sprintf("~%dh ago", hours)
	}
	days := hours / 24
	return fmt.Sprintf("~%dd ago", days)
}
