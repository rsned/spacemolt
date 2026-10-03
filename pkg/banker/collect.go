package banker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/overmind/balances"
)

// Collect reads every "<fleet>-status.json" in dir and returns the agents whose
// credit reading can be trusted, plus the names of any fleets refused as stale.
//
// The status files are the only fleet-wide view of live credits. The asset
// ledger is no substitute: capture_profile runs hourly and capture_faction
// daily, so an agent that stranded ten minutes ago is invisible there.
//
// maxAge refuses a whole file whose CapturedAt is older than it. A stale status
// file reading as live is a documented failure here -- fleet-status.json once
// sat seventeen days dead -- and funding off one would pay agents that have
// since recovered while missing the ones that just broke. Refused fleets are
// RETURNED, never silently dropped: a sweep that quietly saw half the fleet is
// worse than one that says so.
//
// Usernames are not available from the status files and must be filled in by
// the caller from the asset ledger before planning.
func Collect(dir string, maxAge time.Duration, now time.Time) ([]Candidate, []string, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*-status.json"))
	if err != nil {
		return nil, nil, fmt.Errorf("banker: glob %s: %w", dir, err)
	}
	sort.Strings(paths)

	var (
		out   []Candidate
		stale []string
	)
	for _, p := range paths {
		fleet := strings.TrimSuffix(filepath.Base(p), "-status.json")
		b, rerr := os.ReadFile(p) //nolint:gosec // operator-supplied status directory
		if rerr != nil {
			return nil, nil, fmt.Errorf("banker: read %s: %w", p, rerr)
		}
		var sf balances.StatusFile
		if jerr := json.Unmarshal(b, &sf); jerr != nil {
			return nil, nil, fmt.Errorf("banker: parse %s: %w", p, jerr)
		}
		// An unparseable or absent timestamp counts as stale: without it there
		// is no evidence the file is current, and "no evidence" must not read
		// as "fresh" when the output is spending money.
		captured, terr := time.Parse(time.RFC3339, sf.CapturedAt)
		if terr != nil || now.Sub(captured) > maxAge {
			stale = append(stale, fleet)

			continue
		}
		for _, w := range sf.Workers {
			// An unseen worker's credits are not a reading at all -- the
			// heartbeat is known to report a stale 0, which would make a
			// solvent agent look broke and pull money it does not need.
			if !w.Seen {
				continue
			}
			out = append(out, Candidate{AgentID: w.AgentID, Fleet: fleet, Credits: w.Credits})
		}
	}

	return out, stale, nil
}

// Usernames is the agent_id -> in-game username map the gift path needs.
// send_gift addresses the username, which differs from the agent id for most of
// the fleet, so a sweep cannot run without this join.
type Usernames map[string]string

// Attach fills in each candidate's Username. A candidate with no known username
// keeps an empty one and is skipped by PlanTopUp with a stated reason, rather
// than silently falling back to the agent id -- which would address the wrong
// player or simply fail.
func Attach(cands []Candidate, names Usernames) []Candidate {
	out := make([]Candidate, len(cands))
	copy(out, cands)
	for i := range out {
		out[i].Username = names[out[i].AgentID]
	}

	return out
}
