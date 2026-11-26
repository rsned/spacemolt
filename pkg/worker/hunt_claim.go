package worker

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Ground claims keep hunters off each other's grounds. Every hunter reads the
// same board and the same sightings, so two of them picked the same best
// ground at the same moment and the second arrived to an empty belt
// (2026-11-25: Caustic Sac and Krypton Bladder grounds, 21 needs abandoned in
// a day). A hunter setting out for a ground writes its claim; the others pass
// that ground over while the claim is live.
//
// Each hunter owns one file, <agentsDir>/<agentID>/hunt-claim.json, replaced
// whole, so no two processes ever write the same file.
const (
	huntClaimFile = "hunt-claim.json"
	// huntClaimFor is how long a claim stands without being renewed. A hunter
	// at its ground renews it every pass; a stranded or stopped one lets go
	// of the ground this long after its last pass.
	huntClaimFor = time.Hour
)

// huntClaim is one hunter's word that it is working a ground.
type huntClaim struct {
	Agent   string   `json:"agent"`
	System  string   `json:"system"`
	POI     string   `json:"poi"`
	Species []string `json:"species,omitempty"`
	AtUTC   string   `json:"at_utc"`
}

func huntClaimPath(agentsDir, agentID string) string {
	if agentsDir == "" {
		agentsDir = DefaultAgentsDir
	}
	return filepath.Join(agentsDir, agentID, huntClaimFile)
}

// huntClaimGround records that this hunter is working poi for species. It
// replaces the hunter's earlier claim, and renews one on the same ground.
func huntClaimGround(deps HuntDeps, out io.Writer, system, poi string, species []string) {
	if deps.AgentID == "" || poi == "" {
		return
	}
	c := huntClaim{Agent: deps.AgentID, System: system, POI: poi, Species: species, AtUTC: rfc(huntNow(deps))}
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	path := huntClaimPath(deps.AgentsDir, deps.AgentID)
	err = os.MkdirAll(filepath.Dir(path), 0o750)
	if err == nil {
		tmp := path + ".tmp"
		if err = os.WriteFile(tmp, data, 0o600); err == nil {
			err = os.Rename(tmp, path)
		}
	}
	if err != nil {
		fmt.Fprintf(out, "hunt: claim on %s not written: %v\n", poi, err) //nolint:errcheck
	}
}

// huntReleaseClaim drops this hunter's claim, if it holds one.
func huntReleaseClaim(deps HuntDeps) {
	if deps.AgentID == "" {
		return
	}
	_ = os.Remove(huntClaimPath(deps.AgentsDir, deps.AgentID))
}

// huntClaimedGrounds lists the grounds other hunters hold a live claim on for
// any of species. A claim with no species, or a caller with none, conflicts
// with everything at that ground.
func huntClaimedGrounds(deps HuntDeps, species []string) []huntClaim {
	agentsDir := deps.AgentsDir
	if agentsDir == "" {
		agentsDir = DefaultAgentsDir
	}
	paths, err := filepath.Glob(filepath.Join(agentsDir, "*", huntClaimFile))
	if err != nil {
		return nil
	}
	now := huntNow(deps)
	var out []huntClaim
	for _, p := range paths {
		data, rerr := os.ReadFile(p) //nolint:gosec // fleet state file under the agents dir
		if rerr != nil {
			continue
		}
		var c huntClaim
		if json.Unmarshal(data, &c) != nil || c.POI == "" || c.Agent == deps.AgentID {
			continue
		}
		at, ok := huntParseUTC(c.AtUTC)
		if !ok || now.Sub(at) >= huntClaimFor || at.After(now.Add(huntClaimFor)) {
			continue
		}
		if len(species) > 0 && len(c.Species) > 0 &&
			!slices.ContainsFunc(species, func(s string) bool { return slices.Contains(c.Species, s) }) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// huntClaimedPOIs is huntClaimedGrounds as POI ids.
func huntClaimedPOIs(deps HuntDeps, species []string) []string {
	claims := huntClaimedGrounds(deps, species)
	pois := make([]string, 0, len(claims))
	for _, c := range claims {
		pois = append(pois, c.POI)
	}
	return pois
}
