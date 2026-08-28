package knowledge

import (
	"context"
	"fmt"
	"strings"
)

// WildlifeReader is the read side of the wildlife store. Narrowed separately
// from WildlifeRecorder so a caller that only reports never gains write access.
type WildlifeReader interface {
	UnscannedWildlifeSpecies(ctx context.Context, species []string) ([]string, error)
}

// UnscannedSpecies returns which of species have never been scanned, preserving
// nothing about order.
//
// A species is scanned once a scan has stamped danger_scanned_utc; until then
// its danger bracket and hull are unknown. That is the difference between a
// harmless grazer and a Rainbow Leviathan doing 130 energy/tick, which kills a
// starter hull in two ticks -- so the answer is what an operator needs before
// deciding whether a manual scan is safe.
//
// A nil KB, a KB that cannot read wildlife (in-memory, a mock), or an empty
// list all yield no results and no error: reporting is opportunistic and must
// never break the caller.
func UnscannedSpecies(ctx context.Context, kb Base, species []string) ([]string, error) {
	if kb == nil || len(species) == 0 {
		return nil, nil
	}
	r, ok := kb.(WildlifeReader)
	if !ok {
		return nil, nil
	}
	return r.UnscannedWildlifeSpecies(ctx, species)
}

// UnscannedWildlifeSpecies implements WildlifeReader.
//
// A species absent from the table is NOT reported: the caller has just upserted
// everything it saw, so an absent row means the species was never observed
// here, not that it is unscanned.
func (k *SQLiteKB) UnscannedWildlifeSpecies(ctx context.Context, species []string) ([]string, error) {
	if k == nil || k.db == nil || len(species) == 0 {
		return nil, nil
	}
	args := make([]any, len(species))
	for i, s := range species {
		args[i] = s
	}
	q := fmt.Sprintf(`
		SELECT species FROM wildlife_species
		WHERE species IN (%s) AND COALESCE(danger_scanned_utc, '') = ''`,
		strings.TrimSuffix(strings.Repeat("?,", len(species)), ","))

	rows, err := k.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query unscanned wildlife species: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, fmt.Errorf("scan unscanned wildlife species: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate unscanned wildlife species: %w", err)
	}
	return out, nil
}
