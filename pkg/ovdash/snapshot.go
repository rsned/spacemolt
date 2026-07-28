package ovdash

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	{File: "fleet", Label: "haul", Color: "#d4a017", Socket: "haul.sock"},
	{File: "mission-learn", Label: "mission", Color: "#22d3ee", Socket: "mission-learn.sock"},
	{File: "craft", Label: "craft", Color: "#34d399", Socket: "craft.sock"},
	{File: "mb", Label: "mb", Color: "#a78bfa", Socket: "mb.sock"},
	{File: "assist", Label: "assist", Color: "#fb923c", Socket: "assist.sock"},
	{File: "shuttle", Label: "shuttle", Color: "#f472b6", Socket: "shuttle.sock"},
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
