package worker

import (
	"path/filepath"
	"testing"

	"github.com/rsned/spacemolt/pkg/overmind/supervisor"
)

// TestUnlockFleetYAMLParses reads the checked-in roster rather than a fixture,
// for the same reason the hunt-fleet test does: the failure worth catching is a
// mistyped key in the file the operator will actually launch, which a temp-file
// test by construction cannot see.
//
// The unlock pool's own hazard is the STATION PIN. `station:` is a standing
// order to travel there now, and the autopilot tops off only at the origin and
// thereafter burns cargo fuel cells alone (autopilotRefuelIfNeeded, <10%) — it
// never docks to buy fuel between hops. engineer-5 stranded exactly this way on
// 2026-07-29: a 16-jump pin, 135 fuel against its own 128 estimate, dry at a
// star with no station. So a pin is only ever safe for a worker already close
// to the giver, and the roster comment carries the measured distance for every
// unpinned member. This test pins the invariant that survives a roster edit:
// every pinned worker is pinned at the ONE base that sells the chain.
func TestUnlockFleetYAMLParses(t *testing.T) {
	specs, err := supervisor.LoadFleet(filepath.Join("..", "..", "data", "overmind", "unlock-fleet.yaml"))
	if err != nil {
		t.Fatalf("LoadFleet: %v", err)
	}
	if len(specs) == 0 {
		t.Fatal("unlock fleet is empty")
	}
	roles, err := LoadRoles(filepath.Join("..", "..", "data", "overmind", "roles.yaml"))
	if err != nil {
		t.Fatalf("LoadRoles: %v", err)
	}
	// `across_the_line` and `supply_run` sit here and nowhere else, and
	// `an_introduction` — the mission that raises the pirate baseline from the
	// -30 hostile default to 10 — is offered only by its giver at this base.
	const chainGiver = "treasure_cache_trading_post"
	pinned := 0
	for _, s := range specs {
		if s.Role != "unlock" {
			t.Errorf("%s: role = %q, want unlock", s.AgentID, s.Role)
		}
		if _, ok := roles[s.Role]; !ok {
			t.Errorf("%s: role %q is not defined in roles.yaml", s.AgentID, s.Role)
		}
		if s.Station != "" {
			pinned++
			if s.Station != chainGiver {
				t.Errorf("%s: pinned at %q; the chain is sold only at %s", s.AgentID, s.Station, chainGiver)
			}
		}
		// Smuggling is the category that carries the chain. A member without it
		// runs ordinary deliveries forever and never advances toward the unlock,
		// which is the whole point of the pool.
		if !containsCategory(s.MissionCategories, "smuggling") {
			t.Errorf("%s: mission_categories %v omits smuggling", s.AgentID, s.MissionCategories)
		}
	}
	if pinned == 0 {
		t.Error("no worker is pinned at the chain giver, so nobody can reach the unlock mission")
	}
	// The role must run missions; a schedule-only role would capture the ledger
	// faithfully and never fly, which is how the idle pool this replaced behaved.
	if roles["unlock"].Idle != "missions" {
		t.Errorf("unlock role idle = %q, want missions", roles["unlock"].Idle)
	}
	// capture_faction is the completion signal: it writes agent_standings, which
	// is where the baseline flip from -30 to 10 becomes visible.
	found := false
	for _, e := range roles["unlock"].Schedule {
		if e.Command == "capture_faction" {
			found = true
		}
	}
	if !found {
		t.Error("unlock role has no capture_faction entry; the baseline flip would be invisible to the ledger")
	}
}

func containsCategory(cats []string, want string) bool {
	for _, c := range cats {
		if c == want {
			return true
		}
	}

	return false
}
