package main

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// slugifyTag converts a faction tag into a filesystem- and URL-safe slug,
// keeping only [A-Za-z0-9_-] and replacing any other rune with '_'. Returns
// "untagged" if the result is empty.
func slugifyTag(tag string) string {
	var b strings.Builder
	for _, r := range tag {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	s := b.String()
	if s == "" {
		return "untagged"
	}
	return s
}

// productionFacilityTypes are facility-type substrings that identify a
// production facility when the server omits the category field.
var productionFacilityTypes = []string{
	"forge", "refinery", "factory", "fabricator", "foundry",
	"mill", "plant", "assembler", "smelter", "shipyard",
}

// isProductionFacility reports whether a facility produces goods (e.g.
// darksteel_forge) as opposed to a faction support facility (e.g. faction_desk,
// faction_lockbox). The server's category enum is
// [infrastructure, service, production, faction]; production is the only
// producing category. When category is absent, fall back to a recipe binding or
// a production-style type name.
func isProductionFacility(f knowledge.FactionFacilityRow) bool {
	if f.Category != "" {
		return strings.EqualFold(f.Category, "production")
	}
	if f.RecipeID != "" {
		return true
	}
	t := strings.ToLower(f.FacilityType)
	for _, p := range productionFacilityTypes {
		if strings.Contains(t, p) {
			return true
		}
	}
	return false
}

// productionFacilities returns only the production facilities from fs.
func productionFacilities(fs []knowledge.FactionFacilityRow) []knowledge.FactionFacilityRow {
	out := make([]knowledge.FactionFacilityRow, 0, len(fs))
	for _, f := range fs {
		if isProductionFacility(f) {
			out = append(out, f)
		}
	}
	return out
}

// factionFacilities returns the non-production (faction support) facilities from fs.
func factionFacilities(fs []knowledge.FactionFacilityRow) []knowledge.FactionFacilityRow {
	out := make([]knowledge.FactionFacilityRow, 0, len(fs))
	for _, f := range fs {
		if !isProductionFacility(f) {
			out = append(out, f)
		}
	}
	return out
}

// renderFactionHTML renders one faction's dashboard page to a self-contained
// HTML string.
func renderFactionHTML(v *knowledge.FactionView) (string, error) {
	var buf bytes.Buffer
	if err := factionTemplate.Execute(&buf, v); err != nil {
		return "", fmt.Errorf("execute faction template: %w", err)
	}
	return buf.String(), nil
}
