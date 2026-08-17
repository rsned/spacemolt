package knowledge

import (
	"context"
	"fmt"
	"strings"
)

// SpeciesByDisplayName maps a creature's display name ("Rainbow Leviathan") to
// its species id ("rainbow_leviathan"), for species the guide already knows.
//
// It exists because a battle log names creatures but never types them: a
// creature participant carries a Username and an EMPTY ship_class. The normal
// path supplies the mapping from the get_nearby entry that named the quarry,
// but the battles that matter most for defence are the ones nobody chose — an
// ambush kills you without a get_nearby ever having run. Resolving through
// names already in wildlife_species keeps those battles usable while still
// refusing to invent a species: an unknown name simply does not resolve, and
// its shots are skipped rather than filed under a guess.
//
// Matching is case- and space-insensitive because the name is a display string;
// it is the only key the wire gives us and the server has already been seen
// varying case within a single reply ("PREDATOR" vs "predator").
func (kb *SQLiteKB) SpeciesByDisplayName(ctx context.Context) (map[string]string, error) {
	rows, err := kb.db.QueryContext(ctx,
		`SELECT species, name FROM wildlife_species WHERE name <> ''`)
	if err != nil {
		return nil, fmt.Errorf("species by display name: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string]string{}
	for rows.Next() {
		var species, name string
		if err := rows.Scan(&species, &name); err != nil {
			return nil, fmt.Errorf("scan species name: %w", err)
		}
		out[NormalizeCreatureName(name)] = species
	}

	return out, rows.Err()
}

// NormalizeCreatureName is the key form used by SpeciesByDisplayName.
func NormalizeCreatureName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
