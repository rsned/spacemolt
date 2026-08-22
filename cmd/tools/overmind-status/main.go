// Command overmind-status serves a live HTML status page for one or more
// overminds. It re-reads each overmind's status JSON file on every request and
// renders an alphabetical worker table per overmind (name, credits, fleet role,
// current task/status, last position), with a table of contents across all
// overminds at the top.
//
// The page auto-refreshes on a configurable interval (default 5 minutes) and a
// "Refresh now" button reloads immediately, so a manual refresh always reflects
// the newest snapshot the overminds have written (~every 30s).
//
//	overmind-status --addr :8087 --refresh 300 \
//	  --overmind "Haul=data/overmind/haul-status.json" \
//	  --overmind "Marketbots=data/overmind/mb-status.json" \
//	  --overmind "Assist=data/overmind/assist-status.json" \
//	  --overmind "Craft=data/overmind/craft-status.json" \
//	  --overmind "Missions=data/overmind/mission-learn-status.json" \
//	  --overmind "Hunt=data/overmind/hunt-status.json" \
//	  --overmind "Unlock=data/overmind/unlock-status.json" \
//	  --overmind "Shuttle=data/overmind/shuttle-status.json" \
//	  --overmind "Mining=data/overmind/mining-status.json"
//
// With no --overmind flags the seven defaults above are used.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/market"
	"github.com/rsned/spacemolt/pkg/ovstatus"
)

// sourceList is a repeatable "Name=path" flag accumulating overmind sources.
type sourceList []ovstatus.Source

func (s *sourceList) String() string {
	parts := make([]string, 0, len(*s))
	for _, src := range *s {
		parts = append(parts, src.Name+"="+src.Path)
	}
	return strings.Join(parts, ",")
}

func (s *sourceList) Set(v string) error {
	name, path, ok := strings.Cut(v, "=")
	name, path = strings.TrimSpace(name), strings.TrimSpace(path)
	if !ok || name == "" || path == "" {
		return errBadSource
	}
	*s = append(*s, ovstatus.Source{Name: name, Path: path})
	return nil
}

var errBadSource = flagError("overmind must be in the form Name=path")

type flagError string

func (e flagError) Error() string { return string(e) }

func defaultSources() []ovstatus.Source {
	return []ovstatus.Source{
		{Name: "Haul", Path: "data/overmind/haul-status.json"},
		{Name: "Marketbots", Path: "data/overmind/mb-status.json"},
		{Name: "Assist", Path: "data/overmind/assist-status.json"},
		{Name: "Craft", Path: "data/overmind/craft-status.json"},
		{Name: "Missions", Path: "data/overmind/mission-learn-status.json"},
		{Name: "Hunt", Path: "data/overmind/hunt-status.json"},
		{Name: "Unlock", Path: "data/overmind/unlock-status.json"},
		{Name: "Shuttle", Path: "data/overmind/shuttle-status.json"},
		{Name: "Mining", Path: "data/overmind/mining-status.json"},
	}
}

// windowLabel renders a duration as a compact "48h" when it is a whole number
// of hours, else the standard Duration string.
func windowLabel(d time.Duration) string {
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return d.String()
}

// buildHaulStats maps windowed + lifetime haul aggregates into the ovstatus
// bundle: the windowed fleet + per-agent rows become the net-of-fuel panel
// (ranked by net cr/jump desc), and the lifetime rows become per-worker gross
// stat lines.
func buildHaulStats(windowedAgents []market.HaulEfficiencyRow, windowedFleet market.HaulEfficiencyRow,
	lifetimeAgents []market.HaulEfficiencyRow, label string, fuelPerJump, fuelCrPerUnit float64) *ovstatus.HaulStats {
	gross, fuelCr, net := ovstatus.PerJumpMetrics(windowedFleet.SumProfit, windowedFleet.SumJumps, fuelPerJump, fuelCrPerUnit)
	panel := &ovstatus.EffPanel{
		WindowLabel: label, Hauls: windowedFleet.Hauls,
		GrossPerJump: gross, FuelPerJump: fuelCr, NetPerJump: net,
	}
	for _, a := range windowedAgents {
		_, _, an := ovstatus.PerJumpMetrics(a.SumProfit, a.SumJumps, fuelPerJump, fuelCrPerUnit)
		panel.Agents = append(panel.Agents, ovstatus.PanelAgent{AgentID: a.AgentID, Hauls: a.Hauls, NetPerJump: an})
	}
	sort.Slice(panel.Agents, func(i, j int) bool { return panel.Agents[i].NetPerJump > panel.Agents[j].NetPerJump })

	lifetime := make(map[string]ovstatus.AgentLifetime, len(lifetimeAgents))
	for _, a := range lifetimeAgents {
		avg := 0.0
		if a.SumJumps > 0 {
			avg = a.SumProfit / float64(a.SumJumps)
		}
		lifetime[a.AgentID] = ovstatus.AgentLifetime{Hauls: a.Hauls, Jumps: a.SumJumps, AvgPerJump: avg}
	}
	return &ovstatus.HaulStats{Panel: panel, Lifetime: lifetime}
}

func main() {
	var sources sourceList
	addr := flag.String("addr", ":8087", "Listen address for the status page")
	refresh := flag.Int("refresh", 300, "Default auto-refresh interval in seconds (0 disables)")
	marketDBPath := flag.String("market-db-path", "data/market.db", "Path to market.db for the haul-efficiency panel ('' disables it)")
	effWindow := flag.Duration("eff-window", 48*time.Hour, "Efficiency panel window")
	fuelPerJump := flag.Float64("fuel-per-jump", 9, "Estimated fuel units burned per jump (panel fuel term)")
	fuelCrPerUnit := flag.Float64("fuel-cr-per-unit", 5, "Estimated credits per fuel unit (panel fuel term)")
	flag.Var(&sources, "overmind", "Overmind source as Name=path (repeatable); defaults to Haul/Marketbots/Shuttle")
	flag.Parse()

	if len(sources) == 0 {
		sources = defaultSources()
	}

	logger := log.New(log.Writer(), "[overmind-status] ", log.LstdFlags)

	var col *market.Collector
	if *marketDBPath != "" {
		c, err := market.Open(market.Config{DBPath: *marketDBPath, WAL: true})
		if err != nil {
			logger.Printf("efficiency panel disabled: open %s: %v", *marketDBPath, err)
		} else {
			col = c
			defer col.Close() //nolint:errcheck
		}
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		// A ?refresh=N query overrides the default so the operator can watch
		// faster (or pause auto-refresh with ?refresh=0) without a restart.
		rf := *refresh
		if q := r.URL.Query().Get("refresh"); q != "" {
			if n, err := strconv.Atoi(q); err == nil && n >= 0 {
				rf = n
			}
		}
		var hs *ovstatus.HaulStats
		if col != nil {
			now := time.Now()
			wAgents, wFleet, err1 := col.HaulEfficiencySince(r.Context(), now.Add(-*effWindow))
			lAgents, _, err2 := col.HaulEfficiencySince(r.Context(), time.Time{})
			if err1 != nil || err2 != nil {
				logger.Printf("efficiency query: %v / %v", err1, err2)
			} else {
				hs = buildHaulStats(wAgents, wFleet, lAgents, windowLabel(*effWindow), *fuelPerJump, *fuelCrPerUnit)
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if _, err := w.Write([]byte(ovstatus.Render(sources, hs, rf, time.Now()))); err != nil {
			logger.Printf("write response: %v", err)
		}
	})

	names := make([]string, 0, len(sources))
	for _, s := range sources {
		names = append(names, s.Name)
	}
	logger.Printf("serving status for [%s] on %s (refresh %ds)", strings.Join(names, ", "), *addr, *refresh)

	srv := &http.Server{
		Addr:              *addr,
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		logger.Fatalf("server: %v", err)
	}
}
