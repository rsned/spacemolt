package knowledge

import (
	"context"
	"fmt"
	"time"
)

// SystemSurveyReader is the slice of a knowledge base needed to answer "when
// did we last survey each system". Declared here rather than added to Base so
// callers that only need this one query need not grow the whole interface.
type SystemSurveyReader interface {
	SystemsLastSurveyed(ctx context.Context) (map[string]time.Time, error)
}

// SystemsLastSurveyed returns the most recent survey time per system, keyed by
// system id, for every system we have ever surveyed.
//
// The source is wildlife_surveys, NOT systems.last_visited_tick. That column
// reads non-zero for all 505 known systems and stamps twenty of them with a
// single identical tick, because get_map bulk-imports write it: it records
// having IMPORTED a system, not having been there. An explorer using it would
// conclude the entire galaxy was already visited.
//
// wildlife_surveys carries one row per actual survey_system call, with the
// system id and a real UTC timestamp, which is exactly the question being
// asked.
// A kb that does not implement SystemSurveyReader yields no survey history
// rather than an error, so every system reads as never surveyed -- the correct
// answer for a knowledge base that keeps no log.
func SystemsLastSurveyed(ctx context.Context, kb any) (map[string]time.Time, error) {
	r, ok := kb.(SystemSurveyReader)
	if !ok || r == nil {
		return nil, nil
	}

	return r.SystemsLastSurveyed(ctx)
}

// SystemsLastSurveyed implements SystemSurveyReader.
func (kb *SQLiteKB) SystemsLastSurveyed(ctx context.Context) (map[string]time.Time, error) {
	rows, err := kb.db.QueryContext(ctx, `
		SELECT system_id, MAX(observed_utc)
		FROM wildlife_surveys
		WHERE source = ? AND system_id != ''
		GROUP BY system_id`, WildlifeSourceSurvey)
	if err != nil {
		return nil, fmt.Errorf("systems last surveyed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]time.Time)
	for rows.Next() {
		var id, at string
		if err := rows.Scan(&id, &at); err != nil {
			return nil, fmt.Errorf("scan system survey: %w", err)
		}
		// A row whose timestamp will not parse is treated as never surveyed
		// rather than as an error: a malformed row must not stop an explorer.
		if t, perr := time.Parse(time.RFC3339, at); perr == nil {
			out[id] = t
		}
	}

	return out, rows.Err()
}

// SystemsLastSurveyed implements SystemSurveyReader for the in-memory KB,
// which keeps no survey log.
func (kb *MemoryKB) SystemsLastSurveyed(_ context.Context) (map[string]time.Time, error) {
	return nil, nil
}
