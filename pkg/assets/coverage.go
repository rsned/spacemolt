package assets

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
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
//
// faction_storage is keyed on faction_id rather than player_id, so its "agents"
// column counts FACTIONS. The panel labels it accordingly -- the alternative,
// leaving it out, would let a stalled faction capture go unnoticed, which is
// exactly the silence this query exists to break.
var coverageSources = []string{
	"agent_profile", "agent_carrier", "agent_hulls", "agent_skills",
	"agent_storage", "faction_storage",
}

// coverageKeyColumn is the identity column each source is counted by.
func coverageKeyColumn(table string) string {
	if strings.HasPrefix(table, "faction_") {
		return "faction_id"
	}

	return "player_id"
}

// CoverageCadence is how often each source is expected to refresh, keyed by
// table name. Coverage uses 2x a source's cadence as its stale cutoff -- this
// MUST match the alarm rule in the dashboard panel exactly (cadence * 2 in
// frontend/src/components/overmind/AssetCoveragePanel.tsx's CADENCE_HOURS),
// or the panel's red highlight and its "how many agents" stale count disagree
// about the same row. Keep the two maps in sync if either changes.
var CoverageCadence = map[string]time.Duration{
	"agent_profile":   time.Hour,
	"agent_carrier":   time.Hour,
	"agent_hulls":     time.Hour,
	"agent_skills":    time.Hour,
	"agent_storage":   24 * time.Hour,
	"faction_storage": 24 * time.Hour,
}

// Coverage reports how many agents each source knows about and how many of
// those are stale. A source listed in CoverageCadence is considered stale
// past 2x its cadence; any other source falls back to staleAfter.
//
// This exists because every previous unsupervised capture job here died
// silently. Capture rides the supervised worker schedule rather than a new
// daemon, and this query is how a stall becomes visible on a dashboard the
// operator already watches, instead of a cron whose silence means nothing.
func Coverage(ctx context.Context, db *sql.DB, now time.Time, staleAfter time.Duration) ([]CoverageRow, error) {
	if db == nil {
		return nil, nil
	}
	out := make([]CoverageRow, 0, len(coverageSources))
	for _, table := range coverageSources {
		row := CoverageRow{Source: table}
		var oldest sql.NullString
		key := coverageKeyColumn(table)

		staleFor := staleAfter
		if cadence, ok := CoverageCadence[table]; ok {
			staleFor = 2 * cadence
		}
		cutoff := rfc3339(now.Add(-staleFor))

		// #nosec G201 -- table and key come from the fixed coverageSources list
		// (via coverageKeyColumn), never user input.
		//
		// Stale counts DISTINCT agents, not rows: agent_skills and
		// agent_hulls carry many rows per agent, and COUNT(DISTINCT
		// CASE WHEN ... THEN key END) drops NULLs (COUNT(DISTINCT
		// ...) never counts a NULL), so a fresh agent's rows never
		// contribute. Getting this wrong (e.g. SUM of a per-row CASE)
		// overstates the alarm by however many rows the stale agent
		// happens to have.
		q := fmt.Sprintf(`SELECT COUNT(DISTINCT %[1]s), MIN(captured_at),
			COUNT(DISTINCT CASE WHEN captured_at < ? THEN %[1]s END) FROM %[2]s`, key, table)
		if err := db.QueryRowContext(ctx, q, cutoff).Scan(&row.Agents, &oldest, &row.Stale); err != nil {
			return nil, fmt.Errorf("assets: coverage %s: %w", table, err)
		}
		row.Oldest = oldest.String
		out = append(out, row)
	}

	return out, nil
}
