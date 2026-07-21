package ovdash

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SourceEarnings is one earnings stream's trailing-window aggregate.
type SourceEarnings struct {
	Total   float64 `json:"total"`
	PerHour float64 `json:"per_hour"`
	Count   int     `json:"count"`
}

// Accounting is the top-strip payload.
type Accounting struct {
	TotalCredits    float64        `json:"total_credits"`
	Agents          int            `json:"agents"`
	Healthy         int            `json:"healthy"`
	Unseen          int            `json:"unseen"`
	Restarts        int            `json:"restarts"`
	Haul            SourceEarnings `json:"haul"`
	Freight         SourceEarnings `json:"freight"`
	Missions        SourceEarnings `json:"missions"`
	CombinedPerHour float64        `json:"combined_per_hour"`
	PerAgentPerHour float64        `json:"per_agent_per_hour"`
	OldestCapture   string         `json:"oldest_capture"`
}

func earnQuery(ctx context.Context, db *sql.DB, q, since string, hours float64) (SourceEarnings, error) {
	var e SourceEarnings
	if err := db.QueryRowContext(ctx, q, since).Scan(&e.Total, &e.Count); err != nil {
		return e, err
	}
	e.PerHour = e.Total / hours
	return e, nil
}

// LoadEarnings computes the three trailing-window earnings streams from the
// market DB (read-only).
func LoadEarnings(ctx context.Context, dbPath string, now time.Time, window time.Duration) (haul, freight, missions SourceEarnings, err error) {
	db, err := sql.Open(sqliteDriver, "file:"+dbPath+"?mode=ro&_busy_timeout=5000")
	if err != nil {
		return haul, freight, missions, fmt.Errorf("open market db: %w", err)
	}
	defer db.Close() //nolint:errcheck
	since := now.Add(-window).UTC().Format(time.RFC3339)
	hours := window.Hours()

	if haul, err = earnQuery(ctx, db,
		`SELECT COALESCE(SUM(realized_profit),0), COUNT(*) FROM haul_results WHERE sold_at >= ?`,
		since, hours); err != nil {
		return haul, freight, missions, fmt.Errorf("haul earnings: %w", err)
	}
	if freight, err = earnQuery(ctx, db,
		`SELECT COALESCE(SUM(carrier_payout - fuel_cost),0), COUNT(*) FROM freight_results
		 WHERE outcome='delivered' AND finished_at >= ?`,
		since, hours); err != nil {
		return haul, freight, missions, fmt.Errorf("freight earnings: %w", err)
	}
	if missions, err = earnQuery(ctx, db,
		`SELECT COALESCE(SUM(credits_earned - item_cost - fuel_cost),0), COUNT(*) FROM mission_results
		 WHERE finished_at >= ?`,
		since, hours); err != nil {
		return haul, freight, missions, fmt.Errorf("mission earnings: %w", err)
	}
	return haul, freight, missions, nil
}

// BuildAccounting merges live snapshot totals with the earnings streams.
func BuildAccounting(s *Snapshot, haul, freight, missions SourceEarnings, window time.Duration) Accounting {
	a := Accounting{Haul: haul, Freight: freight, Missions: missions}
	active := 0
	all := append(append([]AgentState{}, s.Agents...), s.OffMap...)
	for _, w := range all {
		a.Agents++
		a.TotalCredits += w.Credits
		a.Restarts += w.Restarts
		if w.Healthy {
			a.Healthy++
		}
		if !w.Seen {
			a.Unseen++
		}
		if w.Healthy && w.Seen {
			active++
		}
	}
	a.CombinedPerHour = haul.PerHour + freight.PerHour + missions.PerHour
	if active > 0 {
		a.PerAgentPerHour = a.CombinedPerHour / float64(active)
	}
	for _, ts := range s.CapturedAt {
		if a.OldestCapture == "" || ts < a.OldestCapture {
			a.OldestCapture = ts
		}
	}
	return a
}
