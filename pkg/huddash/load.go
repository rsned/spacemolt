package huddash

import (
	"context"
	"sort"
	"time"

	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/overmind/balances"
)

// Loader is the subset of *market.Collector this package needs (eases testing).
type Loader interface {
	GetHaulResults(ctx context.Context, agentID string, limit int) ([]market.HaulResult, error)
	GetFleetSnapshots(ctx context.Context, agentID string, limit int) ([]market.FleetSnapshot, error)
}

// LoadInput assembles the render model: haul_results + fleet_timeseries from the
// market DB (window-filtered) joined with the live fleet-status.json. Agents are
// the haulers in the status file plus any agent that has hauls in the window.
func LoadInput(ctx context.Context, ld Loader, statusPath string, p Period, window time.Duration, now time.Time) (Input, error) {
	cutoff := now.Add(-window)

	allHauls, err := ld.GetHaulResults(ctx, "", 10000)
	if err != nil {
		return Input{}, err
	}
	allSeries, err := ld.GetFleetSnapshots(ctx, "", 20000)
	if err != nil {
		return Input{}, err
	}
	status, err := balances.ReadStatus(statusPath)
	if err != nil {
		return Input{}, err
	}

	agents := map[string]*AgentData{}
	get := func(id string) *AgentData {
		a := agents[id]
		if a == nil {
			a = &AgentData{AgentID: id}
			agents[id] = a
		}
		return a
	}
	for _, w := range status.Workers {
		if w.Role == "hauler" {
			a := get(w.AgentID)
			a.Status = w
			a.HasStat = true
		}
	}
	for _, h := range allHauls {
		t, perr := time.Parse(time.RFC3339, h.SoldAt)
		if perr != nil || t.Before(cutoff) {
			continue
		}
		a := get(h.AgentID)
		a.Hauls = append(a.Hauls, h)
	}
	for _, s := range allSeries {
		if a, ok := agents[s.AgentID]; ok {
			t, perr := time.Parse(time.RFC3339, s.TS)
			if perr != nil || t.Before(cutoff) {
				continue
			}
			a.Series = append(a.Series, s)
		}
	}

	out := Input{GeneratedAt: now, Period: p, Window: window}
	for _, a := range agents {
		sort.Slice(a.Hauls, func(i, j int) bool { return a.Hauls[i].SoldAt < a.Hauls[j].SoldAt })
		sort.Slice(a.Series, func(i, j int) bool { return a.Series[i].TS < a.Series[j].TS })
		out.Agents = append(out.Agents, *a)
	}
	sort.Slice(out.Agents, func(i, j int) bool { return out.Agents[i].AgentID < out.Agents[j].AgentID })
	return out, nil
}
