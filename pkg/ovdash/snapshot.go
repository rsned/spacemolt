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
}

// Snapshot is the merged live view across every fleet.
type Snapshot struct {
	CapturedAt  map[string]string   `json:"captured_at"`       // fleet label -> RFC3339
	Agents      []AgentState        `json:"agents"`            // system resolved
	OffMap      []AgentState        `json:"off_map"`           // unresolvable system names
	StaleFleets []string            `json:"stale_fleets"`      // labels; missing/old/corrupt
	Removed     map[string][]string `json:"removed,omitempty"` // fleet label -> override-removed agent ids
}

// ReadSnapshot reads every fleet status file under dir and merges them.
// A missing, corrupt, or older-than-staleAfter file marks that fleet stale;
// its last-good agents (if parseable) still appear — greying out is UI policy,
// data completeness is ours.
func ReadSnapshot(dir string, g *Galaxy, now time.Time, staleAfter time.Duration) (*Snapshot, error) {
	s := &Snapshot{CapturedAt: map[string]string{}}
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
		if ts, err := time.Parse(time.RFC3339, sf.CapturedAt); err != nil || now.Sub(ts) > staleAfter {
			s.StaleFleets = append(s.StaleFleets, f.Label)
		}
		for _, w := range sf.Workers {
			a := AgentState{
				Fleet: f.Label, AgentID: w.AgentID, Role: w.Role,
				SystemName: w.System, POI: w.POI, Docked: w.Docked,
				Credits: w.Credits, Hull: w.Hull, MaxHull: w.MaxHull,
				Fuel: w.Fuel, MaxFuel: w.MaxFuel,
				CargoUsed: w.CargoUsed, CargoCap: w.CargoCapacity,
				Activity: w.Activity, Healthy: w.Healthy, Seen: w.Seen,
				Restarts: w.Restarts, LastSeen: w.LastSeen,
				Leaving: w.Leaving,
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
	if len(s.Agents) == 0 && len(s.OffMap) == 0 && len(s.StaleFleets) == len(Fleets) {
		return s, fmt.Errorf("no readable status files in %s", dir)
	}
	return s, nil
}
