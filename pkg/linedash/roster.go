package linedash

import (
	"errors"
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
// A missing dir yields an empty roster and a nil error.
//
// A non-nil error together with a non-nil map means "partial": the named files
// were skipped (a corrupt overrides or secondments file counts as "no removals"
// or "no secondments"). The caller should log the error and render the map.
func LoadRoster(dir string) (map[string]RosterEntry, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*"+fleetSuffix))
	if err != nil {
		return nil, fmt.Errorf("glob fleet yamls: %w", err)
	}
	var errs []error
	// Glob returns sorted names, which makes the tie-break deterministic.
	roster := map[string]RosterEntry{}
	for _, f := range files {
		fleet := strings.TrimSuffix(filepath.Base(f), fleetSuffix)
		specs, lerr := supervisor.LoadFleet(f)
		if lerr != nil {
			errs = append(errs, fmt.Errorf("fleet %s: %w", filepath.Base(f), lerr))
			continue
		}
		ovPath := filepath.Join(dir, fleet+"-overrides.json")
		ov, oerr := supervisor.LoadOverrides(ovPath)
		if oerr != nil {
			// A bad sidecar means "no removals" for this fleet.
			errs = append(errs, fmt.Errorf("overrides %s: %w", filepath.Base(ovPath), oerr))
			ov = supervisor.Overrides{}
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
	secs, serr := supervisor.LoadSecondments(filepath.Join(dir, "secondments.json"))
	if serr != nil {
		errs = append(errs, fmt.Errorf("secondments.json: %w", serr))
		secs = supervisor.Secondments{}
	}
	for id, e := range roster {
		if s, ok := secs.Active(id); ok {
			e.AwayFleet, e.Phase = s.AwayFleet, s.Phase
			roster[id] = e
		}
	}
	return roster, errors.Join(errs...)
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
