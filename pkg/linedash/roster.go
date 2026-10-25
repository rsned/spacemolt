package linedash

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
)

const fleetSuffix = "-fleet.yaml"

// RosterEntry is one agent's assignment, read from the fleet yamls.
type RosterEntry struct {
	AgentID, Fleet, Role, Station string
	Pipeline                      string   // raw "item,system,poi,to,recipient" or ""
	Consign                       []string // item:recipient:min[:keep]
	Shopkeep                      []string // "item:start[:floor]"
	FactionPull                   []string
	Removed                       bool   // in the fleet's overrides `removed`
	AwayFleet                     string // secondment away_fleet while the secondment is active, else ""
	Phase                         string // secondment phase while active, else ""
}

// LoadRoster reads every <fleet>-fleet.yaml in dir, its <fleet>-overrides.json
// if present, and secondments.json. An agent listed in two yamls keeps the
// entry whose fleet has it NOT removed; ties keep the first by file name.
// A missing dir yields an empty roster.
func LoadRoster(dir string) (map[string]RosterEntry, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*"+fleetSuffix))
	if err != nil {
		return nil, fmt.Errorf("glob fleet yamls: %w", err)
	}
	// Glob returns sorted names, which makes the tie-break deterministic.
	roster := map[string]RosterEntry{}
	for _, f := range files {
		fleet := strings.TrimSuffix(filepath.Base(f), fleetSuffix)
		specs, lerr := supervisor.LoadFleet(f)
		if lerr != nil {
			return nil, lerr
		}
		ov, oerr := supervisor.LoadOverrides(filepath.Join(dir, fleet+"-overrides.json"))
		if oerr != nil {
			return nil, oerr
		}
		for _, w := range specs {
			e := RosterEntry{
				AgentID:     w.AgentID,
				Fleet:       fleet,
				Role:        w.Role,
				Station:     w.Station,
				Pipeline:    w.Pipeline,
				Consign:     w.Consign,
				Shopkeep:    w.Shopkeep,
				FactionPull: w.FactionPull,
				Removed:     ov.IsRemoved(w.AgentID),
			}
			if old, ok := roster[w.AgentID]; ok && (!old.Removed || e.Removed) {
				continue
			}
			roster[w.AgentID] = e
		}
	}
	secs, err := supervisor.LoadSecondments(filepath.Join(dir, "secondments.json"))
	if err != nil {
		return nil, err
	}
	for id, e := range roster {
		if s, ok := secs.Active(id); ok {
			e.AwayFleet, e.Phase = s.AwayFleet, s.Phase
			roster[id] = e
		}
	}
	return roster, nil
}

// ShopFloor parses "item:start[:floor]" rules; floor defaults to start when
// absent. ok is false when no rule names item or the matching rule is malformed.
func ShopFloor(rules []string, item string) (start, floor int, ok bool) {
	for _, r := range rules {
		parts := strings.Split(r, ":")
		if parts[0] != item {
			continue
		}
		if len(parts) < 2 || len(parts) > 3 {
			return 0, 0, false
		}
		s, err := strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, false
		}
		f := s
		if len(parts) == 3 {
			if f, err = strconv.Atoi(parts[2]); err != nil {
				return 0, 0, false
			}
		}
		return s, f, true
	}
	return 0, 0, false
}
