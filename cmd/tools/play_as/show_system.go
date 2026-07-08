package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// suggestSystems returns up to three system ids that plausibly match an unknown
// query, for a "did you mean" hint. Candidates are ranked by substring match
// first (id or name, case-insensitive), then by Levenshtein distance ≤ 2. It is
// pure — no I/O — so it is directly testable.
func suggestSystems(query string, systems []knowledge.System) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	type cand struct {
		id   string
		rank int // 0 = substring, 1 = fuzzy
		dist int
	}
	var cands []cand
	for _, s := range systems {
		id := strings.ToLower(s.ID)
		name := strings.ToLower(s.Name)
		switch {
		case strings.Contains(id, q) || (name != "" && strings.Contains(name, q)):
			cands = append(cands, cand{s.ID, 0, 0})
		default:
			d := levenshtein(q, id)
			if name != "" {
				if dn := levenshtein(q, name); dn < d {
					d = dn
				}
			}
			if d <= 2 {
				cands = append(cands, cand{s.ID, 1, d})
			}
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].rank != cands[j].rank {
			return cands[i].rank < cands[j].rank
		}
		return cands[i].dist < cands[j].dist
	})
	var out []string
	for _, c := range cands {
		out = append(out, c.id)
		if len(out) == 3 {
			break
		}
	}
	return out
}

// levenshtein returns the edit distance between two strings.
func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, min(cur[j-1]+1, prev[j-1]+cost))
		}
		prev = cur
	}
	return prev[len(rb)]
}

// renderSystem renders the knowledge base's view of a system in a layout that
// mirrors get_system, enriched with data get_system does not carry: per-POI
// resources/services, star/planet class, hidden flag, and a freshness line.
// It is pure: all inputs are passed in, so it is directly testable. nameByID
// resolves connection ids to display names (id shown when absent). nowTick is
// the current game tick for the age suffix (0 omits the age).
func renderSystem(sys *knowledge.System, pois []knowledge.POI, poisErr error, nameByID map[string]string, nowTick int64) string {
	var b strings.Builder

	// Header: Name (id) | Empire
	empire := sys.Empire
	if empire != "" {
		empire = strings.ToUpper(empire[:1]) + empire[1:]
	} else {
		empire = "Unknown"
	}
	fmt.Fprintf(&b, "%s (%s) | %s\n", sys.Name, sys.ID, empire)

	// Security + freshness line.
	sec := fmt.Sprintf("Security: %d - %s", sys.PoliceLevel, sys.SecurityStatus)
	if sys.Visited() {
		fresh := fmt.Sprintf("Visited (tick %d", sys.LastVisitedTick)
		if nowTick > 0 {
			fresh += ", " + formatAge(nowTick-sys.LastVisitedTick)
		}
		fresh += ")"
		fmt.Fprintf(&b, "%s   | %s\n", sec, fresh)
	} else {
		fmt.Fprintf(&b, "%s (untrusted)   | Unexplored (map-import only)\n", sec)
	}
	if sys.Description != "" {
		fmt.Fprintf(&b, "%s\n", sys.Description)
	}

	// Connections.
	b.WriteString("\nConnections:\n")
	if len(sys.Connections) == 0 {
		b.WriteString("  (none)\n")
	} else {
		nameW := 0
		labels := make([]string, len(sys.Connections))
		for i, c := range sys.Connections {
			label := c.SystemID
			if n := nameByID[c.SystemID]; n != "" {
				label = n
			}
			labels[i] = label
			nameW = max(nameW, len(label))
		}
		for i, c := range sys.Connections {
			fmt.Fprintf(&b, "  %-*s | %-12s | %d LY\n", nameW, labels[i], c.SystemID, c.Distance)
		}
	}

	// POIs.
	b.WriteString("\nPOIs:\n")
	if poisErr != nil {
		fmt.Fprintf(&b, "  (unavailable: %v)\n", poisErr)
		return b.String()
	}
	if len(pois) == 0 {
		b.WriteString("  (none)\n")
		return b.String()
	}
	type row struct{ name, id, typ, class, detail string }
	rows := make([]row, 0, len(pois))
	nameW, idW, typeW, classW := len("Name"), len("ID"), len("Type"), len("Class")
	for _, p := range pois {
		name := p.Name
		if p.Hidden {
			name += " (hidden)"
		}
		detail := poiDetail(p)
		r := row{name, p.ID, p.Type, p.Class, detail}
		rows = append(rows, r)
		nameW = max(nameW, len(r.name))
		idW = max(idW, len(r.id))
		typeW = max(typeW, len(r.typ))
		classW = max(classW, len(r.class))
	}
	fmt.Fprintf(&b, "%-*s | %-*s | %-*s | %-*s | Resources / Services\n",
		nameW, "Name", idW, "ID", typeW, "Type", classW, "Class")
	b.WriteString(strings.Repeat("-", nameW+idW+typeW+classW+32) + "\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "%-*s | %-*s | %-*s | %-*s | %s\n",
			nameW, r.name, idW, r.id, typeW, r.typ, classW, r.class, r.detail)
	}
	return b.String()
}

// poiDetail renders a POI's last column: resources (id(richness), comma-joined)
// when it has any, otherwise its services. Empty when it has neither.
func poiDetail(p knowledge.POI) string {
	if len(p.Resources) > 0 {
		parts := make([]string, 0, len(p.Resources))
		for _, r := range p.Resources {
			parts = append(parts, fmt.Sprintf("%s(%s)", r.ResourceID, trimFloat(r.Richness)))
		}
		return strings.Join(parts, ", ")
	}
	return strings.Join(p.Services, ", ")
}

// runShowSystem implements the show_system REPL command:
//
//	show_system <id>
//
// It prints what the knowledge base knows about a remote system, enriched
// beyond the live get_system output. Pure KB read: no server call, no tick cost.
func runShowSystem(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: show_system <id>")
	}
	if globalKB == nil {
		return fmt.Errorf("show_system: knowledge base not available (run with --db-path)")
	}
	id := args[0]

	sys, err := globalKB.GetSystem(ctx, id)
	if err != nil || sys == nil {
		fmt.Printf("System %q not found in knowledge base.\n", id)
		if systems, serr := globalKB.GetSystems(ctx); serr == nil {
			if s := suggestSystems(id, systems); len(s) > 0 {
				fmt.Printf("Did you mean: %s?\n", strings.Join(s, ", "))
			}
		}
		return nil
	}

	// id -> name map for connection labels (also powers not-found suggestions).
	nameByID := map[string]string{}
	if systems, serr := globalKB.GetSystems(ctx); serr == nil {
		for _, s := range systems {
			nameByID[s.ID] = s.Name
		}
	}

	pois, perr := globalKB.GetPOIs(ctx, id)

	var nowTick int64
	if globalClock != nil {
		nowTick = globalClock.Tick()
	}

	fmt.Print(renderSystem(sys, pois, perr, nameByID, nowTick))
	return nil
}
