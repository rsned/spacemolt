package handlers

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/rsned/spacemolt/pkg/dataservice"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// SystemInfo answers "system_info <system_id>" queries: name, empire,
// security/police level, and the POI list known to the shared KB. Reads
// only the knowledge base — no graph traversal — so it's cheap to call
// for any system that's been recorded by an explorer agent.
type SystemInfo struct{}

// Name implements dataservice.Handler.
func (SystemInfo) Name() string { return "system_info" }

// ShortHelp implements dataservice.Handler.
func (SystemInfo) ShortHelp() string {
	return "Look up a system's name, security level, and recorded POIs"
}

// PlaintextUsages implements dataservice.Handler.
func (SystemInfo) PlaintextUsages() []string {
	return []string{"system_info <system_id>"}
}

// JSONExamples implements dataservice.Handler.
func (SystemInfo) JSONExamples() []map[string]any {
	return []map[string]any{
		{
			"query":  "system_info",
			"params": map[string]any{"system_id": "sol-3"},
		},
	}
}

const systemInfoUsage = "usage: system_info <system_id>"

// HandlePlaintext implements dataservice.Handler.
func (h SystemInfo) HandlePlaintext(ctx context.Context, deps dataservice.Deps, args []string) (string, error) {
	if len(args) != 1 || args[0] == "" {
		return "", dataservice.ErrParse(systemInfoUsage)
	}
	systemID := args[0]

	sys, pois, err := loadSystemInfo(ctx, deps.KB, systemID)
	if err != nil {
		return "", err
	}
	if sys == nil {
		return fmt.Sprintf("System %s not found in knowledge base.", systemID), nil
	}

	var sb strings.Builder
	name := sys.Name
	if name == "" {
		name = sys.ID
	}
	fmt.Fprintf(&sb, "System: %s (%s)\n", name, sys.ID)

	empire := sys.Empire
	if empire == "" {
		empire = "unknown"
	}
	stronghold := ""
	if sys.IsStronghold {
		stronghold = " [STRONGHOLD]"
	}
	security := formatSecurity(sys)
	fmt.Fprintf(&sb, "Empire: %s%s    Security: %s\n", empire, stronghold, security)
	if age := ageText(deps, sys.LastUpdatedTick); age != "" {
		fmt.Fprintf(&sb, "Updated: %s\n", age)
	}

	sb.WriteString("\n")
	if len(pois) == 0 {
		sb.WriteString("POIs: none recorded (system may not be fully explored)\n")
	} else {
		fmt.Fprintf(&sb, "POIs (%d):\n", len(pois))
		idWidth := 0
		for _, p := range pois {
			if w := len(p.ID); w > idWidth {
				idWidth = w
			}
		}
		for _, p := range pois {
			ptype := p.Type
			if ptype == "" {
				ptype = "unknown"
			}
			fmt.Fprintf(&sb, "  %-*s  %s\n", idWidth, p.ID, ptype)
		}
	}

	return dataservice.TruncateReply(sb.String()), nil
}

// HandleJSON implements dataservice.Handler.
func (h SystemInfo) HandleJSON(ctx context.Context, deps dataservice.Deps, params map[string]any) (map[string]any, error) {
	systemID, _ := params["system_id"].(string)
	if systemID == "" {
		return nil, dataservice.ErrParse("missing required field: system_id")
	}

	sys, pois, err := loadSystemInfo(ctx, deps.KB, systemID)
	if err != nil {
		return nil, err
	}
	if sys == nil {
		return map[string]any{
			"system_id": systemID,
			"found":     false,
		}, nil
	}

	poiList := make([]map[string]any, 0, len(pois))
	for _, p := range pois {
		poiList = append(poiList, map[string]any{
			"id":   p.ID,
			"type": p.Type,
		})
	}

	return map[string]any{
		"system_id":         sys.ID,
		"system_name":       sys.Name,
		"empire":            sys.Empire,
		"police_level":      sys.PoliceLevel,
		"security_status":   sys.SecurityStatus,
		"is_stronghold":     sys.IsStronghold,
		"last_updated_tick": sys.LastUpdatedTick,
		"visited":           sys.Visited(),
		"found":             true,
		"pois":              poiList,
	}, nil
}

// loadSystemInfo fetches the system and its POIs in one place; sorts POIs
// by (type, name) for stable rendering. Returns (nil, nil, nil) when the
// system is absent — callers render their own not-found message.
func loadSystemInfo(ctx context.Context, kb knowledge.Base, systemID string) (*knowledge.System, []knowledge.POI, error) {
	sys, err := kb.GetSystem(ctx, systemID)
	if err != nil {
		return nil, nil, fmt.Errorf("get system: %w", err)
	}
	if sys == nil {
		return nil, nil, nil
	}
	pois, err := kb.GetPOIs(ctx, systemID)
	if err != nil {
		return nil, nil, fmt.Errorf("get pois: %w", err)
	}
	slices.SortFunc(pois, func(a, b knowledge.POI) int {
		if a.Type != b.Type {
			return strings.Compare(a.Type, b.Type)
		}
		return strings.Compare(a.Name, b.Name)
	})
	return sys, pois, nil
}

// formatSecurity renders a system's security as "<status> (police N)" with
// an "(unvisited)" suffix when the police level is the map-import default.
func formatSecurity(sys *knowledge.System) string {
	status := sys.SecurityStatus
	if status == "" {
		status = "unknown"
	}
	suffix := ""
	if !sys.Visited() {
		suffix = " (unvisited)"
	}
	return fmt.Sprintf("%s (police %d)%s", status, sys.PoliceLevel, suffix)
}
