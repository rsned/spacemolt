package assets

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// CoverageRow is one source's freshness across the fleet.
type CoverageRow struct {
	Source string `json:"source"`
	Agents int    `json:"agents"`
	Oldest string `json:"oldest"`
	Stale  int    `json:"stale"`
}

// coverageSources are the tables Coverage reports on, in display order.
var coverageSources = []string{"agent_profile", "agent_carrier", "agent_hulls", "agent_skills"}

// Coverage reports how many agents each source knows about and how many of
// those are older than staleAfter.
//
// This exists because every previous unsupervised capture job here died
// silently. Capture rides the supervised worker schedule rather than a new
// daemon, and this query is how a stall becomes visible on a dashboard the
// operator already watches, instead of a cron whose silence means nothing.
func Coverage(ctx context.Context, db *sql.DB, now time.Time, staleAfter time.Duration) ([]CoverageRow, error) {
	if db == nil {
		return nil, nil
	}
	cutoff := rfc3339(now.Add(-staleAfter))
	out := make([]CoverageRow, 0, len(coverageSources))
	for _, table := range coverageSources {
		row := CoverageRow{Source: table}
		var oldest sql.NullString
		// #nosec G201 -- table comes from the fixed coverageSources list, never user input.
		q := fmt.Sprintf(`SELECT COUNT(DISTINCT player_id), MIN(captured_at),
			COALESCE(SUM(CASE WHEN captured_at < ? THEN 1 ELSE 0 END), 0) FROM %s`, table)
		if err := db.QueryRowContext(ctx, q, cutoff).Scan(&row.Agents, &oldest, &row.Stale); err != nil {
			return nil, fmt.Errorf("assets: coverage %s: %w", table, err)
		}
		row.Oldest = oldest.String
		out = append(out, row)
	}

	return out, nil
}
