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
	// A pin has a second legitimate destination once an agent has BANKED the
	// unlock: the stronghold it is named for. That is the campaign's whole
	// purpose -- the `unlock` role runs update_market hourly, so a graduate
	// pinned at a stronghold turns a dark market into a reporting one with no
	// further change. Nine strongholds, one per marketbot; POI ids here, since
	// the station registry's base ids carry a `_station` suffix these do not.
	//
	// The set is enumerated rather than opened up, because the property worth
	// protecting is unchanged: a pin must be a place we MEANT to send someone.
	// An arbitrary far station is how engineer-5 died.
	//
	// BASE ids, with the `_station` suffix -- NOT the POI ids. The arrival check
	// compares st.Player.DockedAtBase against this field, so a POI-id pin never
	// matches at a dual-named station: the worker reads "not arrived" while
	// standing on the dock and re-travels every pass. That drained two 130-unit
	// tanks to zero on 2026-08-12 before it was caught.
	// Seven of the nine are named by BASE id. sable_port and korr_fortress are
	// named by POI id because they are two of the four warlord bases that have
	// NEVER BEEN SCANNED: `bases` holds no row for them, so no base id exists to
	// write, and the `_station` suffix the other seven carry would be a guess.
	// atPinnedStation matches those two on st.Player.CurrentPOI instead, which
	// needs no KB row — see TestPinMatchesTheCurrentPOIWithNoBasesRow. Replace
	// each with its real base id once a bot has docked there and scanned it.
	strongholds := map[string]bool{
		"voss_redoubt_station": true, "sable_port": true,
		"crix_stronghold_station": true, "kael_arsenal_station": true,
		"dross_citadel_station": true, "korr_fortress": true,
		"nyx_nexus_station": true, "thane_keep_station": true,
		"mera_sanctum_station": true,
	}
	pinned, atGiver := 0, 0
	for _, s := range specs {
		if s.Role != "unlock" {
			t.Errorf("%s: role = %q, want unlock", s.AgentID, s.Role)
		}
		if _, ok := roles[s.Role]; !ok {
			t.Errorf("%s: role %q is not defined in roles.yaml", s.AgentID, s.Role)
		}
		if s.Station != "" {
			pinned++
			switch {
			case s.Station == chainGiver:
				atGiver++
			case strongholds[s.Station]:
				// A deployed graduate. Nothing to check here that this file can
				// see: whether it actually holds the unlock is runtime state in
				// assets.db, not roster state.
			default:
				t.Errorf("%s: pinned at %q; a pin must be the chain giver (%s) or a stronghold", s.AgentID, s.Station, chainGiver)
			}
		}
		// Smuggling is the category that carries the chain. A member without it
		// runs ordinary deliveries forever and never advances toward the unlock,
		// which is the whole point of the pool.
		if !containsCategory(s.MissionCategories, "smuggling") {
			t.Errorf("%s: mission_categories %v omits smuggling", s.AgentID, s.MissionCategories)
		}
	}
	if atGiver == 0 {
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
