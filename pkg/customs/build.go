// Package customs derives where empire customs inspects ships from the
// server action-log events the fleet already captures (data/assets.db
// action_log_events), so contraband runs can route around those systems
// instead of following hand-kept rules.
//
// Inspections are tied to a SYSTEM, not to the border crossing: of 1,175
// inspections captured by 2026-11-23, all fell in 19 systems, and transit
// ships (traders, shuttles, explorers) were inspected there and gone within
// ten minutes. v0.734.1 also names the inspection system in every message.
package customs

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Class is a system's customs verdict.
type Class string

const (
	// Monitored: customs inspected here recently and the evidence has not
	// aged out.
	Monitored Class = "monitored"
	// Clear: enough arrivals and no inspection that still counts.
	Clear Class = "clear"
	// Unknown: too little traffic to say.
	Unknown Class = "unknown"
)

// SystemStat is the evidence and verdict for one system.
type SystemStat struct {
	Empire         string    `json:"empire,omitempty"`
	Class          Class     `json:"class"`
	Arrivals       int       `json:"arrivals"`
	Inspections    int       `json:"inspections"`
	Seizures       int       `json:"seizures"`
	ArrivalsSince  int       `json:"arrivals_since_last_inspection"`
	LastInspection time.Time `json:"last_inspection,omitzero"`
	SeizedItems    []string  `json:"seized_items,omitempty"`
	Reason         string    `json:"reason,omitempty"`
}

// Options tune the classification.
type Options struct {
	// Window bounds the evidence: events older than now-Window are ignored.
	Window time.Duration
	// ClearMinArrivals is how many arrivals a system needs before a system
	// with no counting inspection is called Clear rather than Unknown.
	ClearMinArrivals int
	// ClearAfterArrivals ages a Monitored system out: this many arrivals
	// after its last inspection with none since makes it Clear.
	ClearAfterArrivals int
	// EmpireOf names a system's empire when no inspection carried one. nil
	// leaves those blank.
	EmpireOf func(systemID string) string
}

// DefaultOptions are the operator-approved defaults (spec 2026-11-23).
func DefaultOptions() Options {
	return Options{Window: 14 * 24 * time.Hour, ClearMinArrivals: 10, ClearAfterArrivals: 20}
}

type event struct {
	typ  string
	at   time.Time
	data map[string]string
}

// Build classifies every system that had an arrival or an inspection inside
// the window.
func Build(ctx context.Context, db *sql.DB, now time.Time, o Options) (map[string]SystemStat, error) {
	cutoff := now.Add(-o.Window).UTC().Format(time.RFC3339)
	rows, err := db.QueryContext(ctx, `SELECT event_type, created_at, data_json FROM action_log_events
		WHERE event_type IN ('navigation.jumped','combat.customs_inspection','combat.customs_seizure')
		  AND created_at >= ? ORDER BY created_at`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("customs: query action log: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var events []event
	for rows.Next() {
		var typ, at, raw string
		if err := rows.Scan(&typ, &at, &raw); err != nil {
			return nil, fmt.Errorf("customs: scan: %w", err)
		}
		ts, err := time.Parse(time.RFC3339, at)
		if err != nil {
			continue
		}
		var data map[string]string
		if json.Unmarshal([]byte(raw), &data) != nil {
			continue
		}
		events = append(events, event{typ: typ, at: ts, data: data})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("customs: rows: %w", err)
	}

	stats := map[string]*SystemStat{}
	get := func(sys string) *SystemStat {
		s := stats[sys]
		if s == nil {
			s = &SystemStat{}
			stats[sys] = s
		}
		return s
	}
	for _, e := range events { // ordered by time
		switch e.typ {
		case "navigation.jumped":
			sys := strings.ToLower(e.data["to_system"])
			if sys == "" {
				continue
			}
			s := get(sys)
			s.Arrivals++
			s.ArrivalsSince++
		case "combat.customs_inspection":
			sys := strings.ToLower(e.data["system_id"])
			if sys == "" {
				continue
			}
			s := get(sys)
			s.Inspections++
			s.ArrivalsSince = 0
			s.LastInspection = e.at
			s.Empire = cmp.Or(e.data["empire_id"], s.Empire)
		case "combat.customs_seizure":
			sys := strings.ToLower(e.data["system_id"])
			if sys == "" {
				continue
			}
			s := get(sys)
			s.Seizures++
			s.Empire = cmp.Or(e.data["empire_id"], s.Empire)
			var items []string
			if json.Unmarshal([]byte(e.data["seized_items"]), &items) == nil {
				for _, it := range items {
					if !slices.Contains(s.SeizedItems, it) {
						s.SeizedItems = append(s.SeizedItems, it)
					}
				}
			}
		}
	}

	out := make(map[string]SystemStat, len(stats))
	for sys, s := range stats {
		if s.Empire == "" && o.EmpireOf != nil {
			s.Empire = o.EmpireOf(sys)
		}
		switch {
		case s.Inspections > 0 && s.ArrivalsSince < o.ClearAfterArrivals:
			s.Class = Monitored
		case s.Arrivals >= o.ClearMinArrivals:
			s.Class = Clear
		default:
			s.Class = Unknown
		}
		out[sys] = *s
	}
	return out, nil
}
