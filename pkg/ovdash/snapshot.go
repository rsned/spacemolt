package ovdash

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/assets"
	"github.com/rsned/spacemolt/pkg/overmind/balances"
	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
)

// FleetDef names one fleet's status file and how the UI labels/colors it.
type FleetDef struct {
	File   string // status file prefix: data/overmind/<File>-status.json
	Label  string
	Color  string
	Socket string // basename of the fleet's control-channel unix socket
}

// Fleets is the fixed fleet registry. Order is display order.
var Fleets = []FleetDef{
	// File is "haul", not "fleet": the haul overmind writes haul-status.json.
	// This read fleet-status.json — the name from before the fleet was renamed —
	// and nothing had written that file for 17 hours by 2026-08-13, so the panel
	// showed every haul agent at a dead position and flagged the fleet stale
	// forever while the live file sat unread beside it.
	{File: "haul", Label: "haul", Color: "#d4a017", Socket: "haul.sock"},
	{File: "mission-learn", Label: "mission", Color: "#22d3ee", Socket: "mission-learn.sock"},
	{File: "craft", Label: "craft", Color: "#34d399", Socket: "craft.sock"},
	{File: "mb", Label: "mb", Color: "#a78bfa", Socket: "mb.sock"},
	{File: "assist", Label: "assist", Color: "#fb923c", Socket: "assist.sock"},
	// shuttle retired 2026-08-13: johnny_cab moved to the unlock fleet, and
	// shuttle-status.json stopped updating — listing it only showed a stale panel.
	{File: "hunt", Label: "hunt", Color: "#ef4444", Socket: "hunt.sock"},
	{File: "unlock", Label: "unlock", Color: "#60a5fa", Socket: "unlock.sock"},
}

// AgentState is one worker in one snapshot, system resolved to a map id.
type AgentState struct {
	Fleet      string  `json:"fleet"`
	AgentID    string  `json:"agent_id"`
	Role       string  `json:"role"`
	SystemID   string  `json:"system_id"`
	SystemName string  `json:"system_name"`
	POI        string  `json:"poi"`
	Docked     bool    `json:"docked"`
	Credits    float64 `json:"credits"`
	Hull       float64 `json:"hull"`
	MaxHull    float64 `json:"max_hull"`
	Fuel       float64 `json:"fuel"`
	MaxFuel    float64 `json:"max_fuel"`
	CargoUsed  float64 `json:"cargo_used"`
	CargoCap   float64 `json:"cargo_capacity"`
	Activity   string  `json:"activity,omitempty"`
	Healthy    bool    `json:"healthy"`
	Seen       bool    `json:"seen"`
	Restarts   int     `json:"restarts"`
	LastSeen   string  `json:"last_seen"`
	Leaving    bool    `json:"leaving,omitempty"`
	// Build identity (from the worker's Hello, via the status file). Tier is
	// this build's color vs the current (newest) fleet build. Modified is the
	// cosmetic raw vcs.modified flag; CodeDirty drives the tier.
	Version   string `json:"version,omitempty"`
	Commit    string `json:"commit,omitempty"`
	BuiltAt   string `json:"built_at,omitempty"`
	CodeDirty bool   `json:"code_dirty,omitempty"`
	Modified  bool   `json:"modified,omitempty"`
	Tier      Tier   `json:"tier,omitempty"`
}

// OvermindInfo is one fleet's overmind build identity plus the rolled-up
// worst tier across that overmind and all its workers.
type OvermindInfo struct {
	Version   string `json:"version,omitempty"`
	Commit    string `json:"commit,omitempty"`
	BuiltAt   string `json:"built_at,omitempty"`
	CodeDirty bool   `json:"code_dirty,omitempty"`
	Modified  bool   `json:"modified,omitempty"`
	Tier      Tier   `json:"tier,omitempty"`       // the overmind's own tier
	FleetTier Tier   `json:"fleet_tier,omitempty"` // worst tier in the fleet
}

// Snapshot is the merged live view across every fleet.
type Snapshot struct {
	CapturedAt  map[string]string       `json:"captured_at"`         // fleet label -> RFC3339
	Agents      []AgentState            `json:"agents"`              // system resolved
	OffMap      []AgentState            `json:"off_map"`             // unresolvable system names
	StaleFleets []string                `json:"stale_fleets"`        // labels; missing/old/corrupt
	Removed     map[string][]string     `json:"removed,omitempty"`   // fleet label -> override-removed agent ids
	Overminds   map[string]OvermindInfo `json:"overminds,omitempty"` // fleet label -> overmind build
	// CurrentOvermind/CurrentWorker are the newest build seen for each binary,
	// picked by build time. Reported separately because they roll out
	// independently — a fleet can be running a current overmind and stale
	// workers, and a single merged "current" hides exactly that.
	CurrentOvermind string `json:"current_overmind,omitempty"`
	CurrentWorker   string `json:"current_worker,omitempty"`
	// AssetCoverage is per-source freshness of the agent asset ledger. Empty
	// when the ledger is not deployed.
	AssetCoverage []assets.CoverageRow `json:"asset_coverage,omitempty"`
}

// dedupeByFreshestFleet keeps one entry per agent id, preferring the fleet whose
// status file was captured most recently.
//
// Keeping a stale fleet's agents is deliberate — greying out is UI policy, data
// completeness is ours — but that argument only holds while the stale copy is the
// ONLY word on an agent. When a live fleet reports the same id, the two positions
// contradict each other and the older one is simply wrong.
//
// Leaving both in place is not a cosmetic duplicate. Diff keys by agent id, so it
// finds that id at two systems on EVERY poll and emits a Moved event every time:
// live 2026-08-13 the dashboard drew trader-10 flying HR 8832 -> Gudja about once
// a second, forever, while the real worker sat still — and neither endpoint was
// anywhere an agent actually was. An agent mid-secondment can appear in two LIVE
// fleets for a poll or two as well, so this guard is not only about dead files.
//
// Relative order within each slice is preserved so display order stays the
// registry's.
func dedupeByFreshestFleet(agents, offMap []AgentState, capturedAt map[string]string) ([]AgentState, []AgentState) {
	freshness := func(fleet string) time.Time {
		ts, err := time.Parse(time.RFC3339, capturedAt[fleet])
		if err != nil {
			return time.Time{} // unparseable loses to any real timestamp
		}
		return ts
	}
	best := map[string]time.Time{}
	for _, a := range agents {
		if t := freshness(a.Fleet); t.After(best[a.AgentID]) || best[a.AgentID].IsZero() {
			best[a.AgentID] = t
		}
	}
	for _, a := range offMap {
		if t := freshness(a.Fleet); t.After(best[a.AgentID]) || best[a.AgentID].IsZero() {
			best[a.AgentID] = t
		}
	}
	taken := map[string]bool{}
	keep := func(in []AgentState) []AgentState {
		out := in[:0:0]
		for _, a := range in {
			if taken[a.AgentID] || !freshness(a.Fleet).Equal(best[a.AgentID]) {
				continue
			}
			taken[a.AgentID] = true
			out = append(out, a)
		}
		return out
	}
	return keep(agents), keep(offMap)
}

// ReadSnapshot reads every fleet status file under dir and merges them.
// A missing, corrupt, or older-than-staleAfter file marks that fleet stale;
// its last-good agents (if parseable) still appear — greying out is UI policy,
// data completeness is ours.
func ReadSnapshot(dir string, g *Galaxy, now time.Time, staleAfter time.Duration) (*Snapshot, error) {
	s := &Snapshot{CapturedAt: map[string]string{}, Overminds: map[string]OvermindInfo{}}
	var samples, ovSamples, wSamples []buildSample
	for _, f := range Fleets {
		path := filepath.Join(dir, f.File+"-status.json")
		b, err := os.ReadFile(path)
		if err != nil {
			s.StaleFleets = append(s.StaleFleets, f.Label)
			continue
		}
		var sf balances.StatusFile
		if err := json.Unmarshal(b, &sf); err != nil {
			s.StaleFleets = append(s.StaleFleets, f.Label)
			continue
		}
		s.CapturedAt[f.Label] = sf.CapturedAt
		s.Overminds[f.Label] = OvermindInfo{
			Version: sf.OvermindVersion, Commit: sf.OvermindCommit, BuiltAt: sf.OvermindBuiltAt,
			CodeDirty: sf.OvermindCodeDirty, Modified: sf.OvermindModified,
		}
		samples = append(samples, buildSample{Version: sf.OvermindVersion, BuiltAt: sf.OvermindBuiltAt})
		ovSamples = append(ovSamples, buildSample{Version: sf.OvermindVersion, BuiltAt: sf.OvermindBuiltAt})
		if ts, err := time.Parse(time.RFC3339, sf.CapturedAt); err != nil || now.Sub(ts) > staleAfter {
			s.StaleFleets = append(s.StaleFleets, f.Label)
		}
		for _, w := range sf.Workers {
			samples = append(samples, buildSample{Version: w.Version, BuiltAt: w.BuiltAt})
			wSamples = append(wSamples, buildSample{Version: w.Version, BuiltAt: w.BuiltAt})
			a := AgentState{
				Fleet: f.Label, AgentID: w.AgentID, Role: w.Role,
				SystemName: w.System, POI: w.POI, Docked: w.Docked,
				Credits: w.Credits, Hull: w.Hull, MaxHull: w.MaxHull,
				Fuel: w.Fuel, MaxFuel: w.MaxFuel,
				CargoUsed: w.CargoUsed, CargoCap: w.CargoCapacity,
				Activity: w.Activity, Healthy: w.Healthy, Seen: w.Seen,
				Restarts: w.Restarts, LastSeen: w.LastSeen,
				Leaving: w.Leaving,
				Version: w.Version, Commit: w.Commit, BuiltAt: w.BuiltAt,
				CodeDirty: w.CodeDirty, Modified: w.Modified,
			}
			if id, ok := g.ResolveName(w.System); ok {
				a.SystemID = id
				s.Agents = append(s.Agents, a)
			} else {
				s.OffMap = append(s.OffMap, a)
			}
		}

		ovPath := filepath.Join(dir, strings.TrimSuffix(f.Socket, ".sock")+"-overrides.json")
		if ov, err := supervisor.LoadOverrides(ovPath); err == nil && len(ov.Removed) > 0 {
			if s.Removed == nil {
				s.Removed = map[string][]string{}
			}
			s.Removed[f.Label] = ov.Removed
		}
	}
	s.Agents, s.OffMap = dedupeByFreshestFleet(s.Agents, s.OffMap, s.CapturedAt)
	current := currentVersion(samples)
	s.CurrentOvermind = currentVersion(ovSamples)
	s.CurrentWorker = currentVersion(wSamples)
	for i := range s.Agents {
		s.Agents[i].Tier = Classify(s.Agents[i].Version, s.Agents[i].CodeDirty, current)
	}
	for i := range s.OffMap {
		s.OffMap[i].Tier = Classify(s.OffMap[i].Version, s.OffMap[i].CodeDirty, current)
	}
	fleetTiers := map[string][]Tier{}
	for _, a := range s.Agents {
		fleetTiers[a.Fleet] = append(fleetTiers[a.Fleet], a.Tier)
	}
	for _, a := range s.OffMap {
		fleetTiers[a.Fleet] = append(fleetTiers[a.Fleet], a.Tier)
	}
	for label, oi := range s.Overminds {
		oi.Tier = Classify(oi.Version, oi.CodeDirty, current)
		oi.FleetTier = worstTier(append(fleetTiers[label], oi.Tier)...)
		s.Overminds[label] = oi
	}
	if len(s.Agents) == 0 && len(s.OffMap) == 0 && len(s.StaleFleets) == len(Fleets) {
		return s, fmt.Errorf("no readable status files in %s", dir)
	}
	return s, nil
}
