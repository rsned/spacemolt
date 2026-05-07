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

// Nearest answers "find the N nearest accessible systems" queries, keyed by
// either a POI type or a resource id.
type Nearest struct {
	// Limit is the max number of results to return. Defaults to 3 when 0.
	Limit int
}

// Name implements dataservice.Handler.
func (n *Nearest) Name() string { return "nearest" }

// ShortHelp implements dataservice.Handler.
func (n *Nearest) ShortHelp() string {
	return "Find nearest accessible POIs of a given type or with a given resource"
}

// PlaintextUsages implements dataservice.Handler. Each entry documents one
// supported grammar form. The shorthand "nearest <term> from <X>" tries
// POI-type first, then resource-id, so "nearest station from sol-3" and
// "nearest tungsten_ore from sol-3" both work without a leading keyword.
func (n *Nearest) PlaintextUsages() []string {
	return []string{
		"nearest poi <poi_type> from <system_id>",
		"nearest ore <resource_id> from <system_id>",
		"nearest <poi_type|resource_id> from <system_id>  # shorthand",
	}
}

// JSONExamples implements dataservice.Handler. Exactly one of poi_type or
// resource_id must be supplied alongside from_system.
func (n *Nearest) JSONExamples() []map[string]any {
	return []map[string]any{
		{
			"query":  "nearest",
			"params": map[string]any{"poi_type": "station", "from_system": "sol-3"},
		},
		{
			"query":  "nearest",
			"params": map[string]any{"resource_id": "legacy_ore", "from_system": "sol-3"},
		},
	}
}

func (n *Nearest) limit() int {
	if n.Limit > 0 {
		return n.Limit
	}
	return 3
}

const nearestUsage = `usage: nearest (poi <poi_type>|ore <resource_id>|<poi_type>) from <system_id>`

// HandlePlaintext implements dataservice.Handler. Grammar:
//
//	nearest <poi_type> from <system_id>
//	nearest poi <poi_type> from <system_id>
//	nearest ore <resource_id> from <system_id>
func (n *Nearest) HandlePlaintext(ctx context.Context, deps dataservice.Deps, args []string) (string, error) {
	kind, key, fromSystem, err := parseNearestArgs(args)
	if err != nil {
		return "", err
	}

	results, label, err := n.runQuery(ctx, deps, kind, key, fromSystem)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return fmt.Sprintf("No accessible %s found from %s.", label, fromSystem), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Nearest accessible %s from %s:\n", label, fromSystem)
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

// HandleJSON implements dataservice.Handler. One of {poi_type, resource_id}
// must be provided along with from_system.
func (n *Nearest) HandleJSON(ctx context.Context, deps dataservice.Deps, params map[string]any) (map[string]any, error) {
	poiType, _ := params["poi_type"].(string)
	resourceID, _ := params["resource_id"].(string)
	fromSystem, _ := params["from_system"].(string)

	if fromSystem == "" {
		return nil, dataservice.ErrParse("missing required field: from_system")
	}
	if poiType == "" && resourceID == "" {
		return nil, dataservice.ErrParse("missing required field: poi_type or resource_id")
	}
	if poiType != "" && resourceID != "" {
		return nil, dataservice.ErrParse("only one of poi_type or resource_id may be set")
	}

	var kind, key string
	if resourceID != "" {
		kind = "ore"
		key = resourceID
	} else {
		kind = "poi"
		key = strings.ToLower(poiType)
	}

	results, _, err := n.runQuery(ctx, deps, kind, key, fromSystem)
	if err != nil {
		return nil, err
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

	reply := map[string]any{
		"from_system": fromSystem,
		"results":     out,
	}
	if kind == "ore" {
		reply["resource_id"] = key
	} else {
		reply["poi_type"] = key
	}
	return reply, nil
}

// parseNearestArgs extracts (kind, key, fromSystem) from the plaintext tokens
// following the "nearest" keyword. Kind is "ore", "poi", or "auto" — the
// shorthand form (no leading keyword) returns "auto" so runQuery can try
// both POI-type and resource-id lookups; users typing "nearest tungsten_ore
// from X" don't need to know which catalog the term belongs to.
func parseNearestArgs(args []string) (kind, key, fromSystem string, err error) {
	if len(args) < 3 {
		return "", "", "", dataservice.ErrParse(nearestUsage)
	}

	head := strings.ToLower(args[0])
	switch head {
	case "ore":
		if len(args) < 4 || strings.ToLower(args[2]) != "from" {
			return "", "", "", dataservice.ErrParse(nearestUsage)
		}
		return "ore", args[1], args[3], nil
	case "poi":
		if len(args) < 4 || strings.ToLower(args[2]) != "from" {
			return "", "", "", dataservice.ErrParse(nearestUsage)
		}
		return "poi", strings.ToLower(args[1]), args[3], nil
	default:
		if strings.ToLower(args[1]) != "from" {
			return "", "", "", dataservice.ErrParse(nearestUsage)
		}
		return "auto", head, args[2], nil
	}
}

// runQuery dispatches to the appropriate galaxy lookup and returns results
// along with a human-readable label for the queried category. Kind "auto"
// tries POI-type first, then resource-id, picking whichever returns hits;
// the no-results label keeps the bare term so the failure message reads
// naturally.
func (n *Nearest) runQuery(ctx context.Context, deps dataservice.Deps, kind, key, fromSystem string) ([]galaxy.NearestResult, string, error) {
	debugf := func(format string, args ...any) {
		if deps.Logger != nil {
			deps.Logger.Printf("nearest: "+format, args...)
		}
	}
	switch kind {
	case "ore":
		results, err := galaxy.FindNearestByResource(ctx, deps.KB, deps.Graph, fromSystem, key, n.limit())
		if err != nil {
			return nil, "", fmt.Errorf("nearest lookup: %w", err)
		}
		debugf("ore %q from %q -> %d results", key, fromSystem, len(results))
		return results, "ore " + key, nil
	case "poi":
		results, err := galaxy.FindNearestByPOIType(ctx, deps.KB, deps.Graph, fromSystem, key, n.limit())
		if err != nil {
			return nil, "", fmt.Errorf("nearest lookup: %w", err)
		}
		debugf("poi %q from %q -> %d results", key, fromSystem, len(results))
		return results, key, nil
	case "auto":
		poiResults, err := galaxy.FindNearestByPOIType(ctx, deps.KB, deps.Graph, fromSystem, key, n.limit())
		if err != nil {
			return nil, "", fmt.Errorf("nearest lookup: %w", err)
		}
		if len(poiResults) > 0 {
			debugf("auto %q from %q -> poi=%d (matched POI type)", key, fromSystem, len(poiResults))
			return poiResults, key, nil
		}
		oreResults, err := galaxy.FindNearestByResource(ctx, deps.KB, deps.Graph, fromSystem, key, n.limit())
		if err != nil {
			return nil, "", fmt.Errorf("nearest lookup: %w", err)
		}
		debugf("auto %q from %q -> poi=0 ore=%d", key, fromSystem, len(oreResults))
		if len(oreResults) > 0 {
			return oreResults, "ore " + key, nil
		}
		return nil, key, nil
	default:
		return nil, "", dataservice.ErrParse(nearestUsage)
	}
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
