package assets

import (
	"context"
	"sort"
	"testing"
	"time"
)

// snapWith builds a snapshot with one active hull of the given cargo capacity
// proxy (cargo_used is not capacity; the active hull's presence is what the
// v1 rules key on) plus the given skills and standings.
//
// Standings carry one row per pirate stronghold, which is what the server
// actually sends. The retired generic "pirates" key must NOT appear here: this
// fixture used it, so every stronghold_access test passed against a lookup that
// returns nothing on live data.
func snapWith(smuggling int, pirateBaseline int, tier string, debt int64) AgentSnapshot {
	standings := make(map[string]StandingRow, len(pirateStrongholds))
	for _, name := range pirateStrongholds {
		standings[name] = StandingRow{Faction: name, Baseline: pirateBaseline}
	}

	return AgentSnapshot{
		Profile:      Profile{PlayerID: "abc123", Credits: 100000},
		Skills:       map[string]SkillRow{"smuggling": {Skill: "smuggling", Level: smuggling}},
		Standings:    standings,
		Carrier:      Carrier{Tier: tier, OutstandingDebt: debt},
		CarrierKnown: tier != "",
		Hulls:        []Hull{{ShipID: "s1", IsActive: true, FuelCurrent: 400, FuelMax: 400}},
	}
}

// pirateStrongholds is the nine keys the server sends, per the standings
// description in server_docs/openapi.json.
var pirateStrongholds = []string{
	"pirate_crix", "pirate_dross", "pirate_kael", "pirate_korr", "pirate_mera",
	"pirate_nyx", "pirate_sable", "pirate_thane", "pirate_voss",
}

// TestSmugglingEligibilityBoundary pins the L3 threshold, mirroring
// pkg/worker's smugglingXPExemptLevel. Level 3 unlocks the chain-2 reputation
// mission, which is the whole point of the climb.
func TestSmugglingEligibilityBoundary(t *testing.T) {
	for _, tc := range []struct {
		level int
		want  bool
	}{{0, false}, {2, false}, {3, true}, {7, true}} {
		caps := capsByName(Evaluate(snapWith(tc.level, -30, "licensed", 0)))
		got := caps["smuggling"]
		if got.Eligible != tc.want {
			t.Errorf("smuggling level %d: eligible = %v, want %v (reason %q)",
				tc.level, got.Eligible, tc.want, got.BlockingReason)
		}
		if !tc.want && got.BlockingReason == "" {
			t.Errorf("smuggling level %d: ineligible must carry a blocking reason", tc.level)
		}
	}
}

// TestStrongholdAccessUsesBaselineNotReputation pins that the gate reads
// baseline. Reputation floats above baseline and decays back, so gating on it
// would report eligible during the float and flip back later.
func TestStrongholdAccessUsesBaselineNotReputation(t *testing.T) {
	// Baseline 9 with a high floating reputation must still be ineligible.
	s := snapWith(3, 9, "licensed", 0)
	for _, name := range pirateStrongholds {
		st := s.Standings[name]
		st.Reputation = 95
		s.Standings[name] = st
	}
	if got := capsByName(Evaluate(s))["stronghold_access"]; got.Eligible {
		t.Error("baseline 9 with reputation 95 must be ineligible")
	}

	if got := capsByName(Evaluate(snapWith(3, 10, "licensed", 0)))["stronghold_access"]; !got.Eligible {
		t.Errorf("baseline 10 must be eligible, reason=%q", got.BlockingReason)
	}
}

// TestStrongholdAccessLiveStandings replays craftsman-1's real standings, the
// payload that exposed the bug: nine strongholds at baseline 10 / reputation
// 16-17, reported as "baseline 0, needs 10" because the rule looked up the
// retired generic "pirates" key. The operator runs faction production lines
// inside a stronghold, so ineligible was plainly wrong.
func TestStrongholdAccessLiveStandings(t *testing.T) {
	live := map[string]StandingRow{
		"crimson":  {Faction: "crimson", Baseline: 10, Reputation: 10},
		"nebula":   {Faction: "nebula", Baseline: 20, Reputation: 24},
		"outerrim": {Faction: "outerrim", Baseline: 10, Reputation: 10},
		"solarian": {Faction: "solarian", Baseline: 10, Reputation: 10},
		"voidborn": {Faction: "voidborn", Baseline: 10, Reputation: 10},
	}
	for i, name := range pirateStrongholds {
		live[name] = StandingRow{Faction: name, Baseline: 10, Reputation: 16 + i%2}
	}
	s := snapWith(3, 10, "licensed", 0)
	s.Standings = live

	got := capsByName(Evaluate(s))["stronghold_access"]
	if !got.Eligible {
		t.Errorf("live standings must be eligible, got reason %q", got.BlockingReason)
	}
}

// TestStrongholdAccessAnyStrongholdCounts pins the per-crew semantics: each
// stronghold keeps its own books, so one crew that will have you IS access,
// even when the other eight will not.
func TestStrongholdAccessAnyStrongholdCounts(t *testing.T) {
	s := snapWith(3, 2, "licensed", 0) // all nine hostile
	if got := capsByName(Evaluate(s))["stronghold_access"]; got.Eligible {
		t.Error("baseline 2 everywhere must be ineligible")
	} else if got.BlockingReason == "" {
		t.Error("ineligible must carry a blocking reason naming the best stronghold")
	}

	s.Standings["pirate_voss"] = StandingRow{Faction: "pirate_voss", Baseline: strongholdBase}
	if got := capsByName(Evaluate(s))["stronghold_access"]; !got.Eligible {
		t.Errorf("one welcoming stronghold is access, reason=%q", got.BlockingReason)
	}
}

// TestStrongholdAccessNoStandings distinguishes "nobody will have you" from
// "we have never seen a pirate standing", which are different problems.
func TestStrongholdAccessNoStandings(t *testing.T) {
	s := snapWith(3, 10, "licensed", 0)
	s.Standings = map[string]StandingRow{"nebula": {Faction: "nebula", Baseline: 20}}
	got := capsByName(Evaluate(s))["stronghold_access"]
	if got.Eligible {
		t.Error("no pirate standings must not read as access")
	}
	if got.BlockingReason != "no pirate stronghold standings" {
		t.Errorf("reason = %q, want it to say the standings are missing", got.BlockingReason)
	}
}

// TestFreightBlockedByDebt pins that outstanding debt blocks freight and says
// so, since debt is the thing that silently stops contract acceptance.
func TestFreightBlockedByDebt(t *testing.T) {
	got := capsByName(Evaluate(snapWith(3, 10, "licensed", 4200)))["freight"]
	if got.Eligible {
		t.Error("outstanding debt must block freight")
	}
	if got.BlockingReason == "" {
		t.Error("debt block must carry a reason")
	}
}

// TestUnknownCarrierIsIneligibleNotEligible pins the safe direction: an agent
// whose carrier profile has never been captured is NOT freight-eligible. The
// screening filter must not invent capability from missing data.
func TestUnknownCarrierIsIneligibleNotEligible(t *testing.T) {
	s := snapWith(3, 10, "", 0)
	if got := capsByName(Evaluate(s))["freight"]; got.Eligible {
		t.Error("uncaptured carrier profile must not read as freight-eligible")
	}
}

// TestEvaluateCoversEveryRegisteredRule pins that Evaluate emits one row per
// registered capability, so a new rule cannot silently produce no output.
func TestEvaluateCoversEveryRegisteredRule(t *testing.T) {
	caps := Evaluate(snapWith(3, 10, "licensed", 0))
	if len(caps) != len(Rules()) {
		t.Fatalf("Evaluate returned %d rows, want %d (one per rule)", len(caps), len(Rules()))
	}
}

// TestEvaluateReturnsSortedOrder pins that Evaluate's output is sorted by
// capability name. Every other assertion in this file goes through the
// order-insensitive capsByName helper, so without this test a regression
// dropping the sort in Evaluate would pass silently.
func TestEvaluateReturnsSortedOrder(t *testing.T) {
	caps := Evaluate(snapWith(3, 10, "licensed", 0))
	names := make([]string, len(caps))
	for i, c := range caps {
		names[i] = c.Capability
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("Evaluate returned unsorted capability names: %v", names)
	}
}

// TestReplaceCapabilitiesIsAWholeSetSwap pins that a capability that stops
// being emitted disappears from the table.
func TestReplaceCapabilitiesIsAWholeSetSwap(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	first := []Capability{{Capability: "haul", Eligible: true}, {Capability: "freight", Eligible: false, BlockingReason: "debt"}}
	if err := st.ReplaceCapabilities(ctx, "abc123", first, now); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := st.ReplaceCapabilities(ctx, "abc123", first[:1], now.Add(time.Hour)); err != nil {
		t.Fatalf("second: %v", err)
	}

	var n int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_capability WHERE player_id = ?`, "abc123").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("agent_capability rows = %d, want 1", n)
	}
}

// capsByName indexes an Evaluate result for assertion.
func capsByName(caps []Capability) map[string]Capability {
	m := make(map[string]Capability, len(caps))
	for _, c := range caps {
		m[c.Capability] = c
	}

	return m
}
