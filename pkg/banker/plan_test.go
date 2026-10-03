package banker

import "testing"

func cand(id, user string, credits float64) Candidate {
	return Candidate{AgentID: id, Username: user, Fleet: "haul", Credits: credits}
}

// The core rule: an agent below the floor is topped up TO the floor, not by it.
// 94 of 149 agents sat under 10,000 on 2026-10-03 while the weekly tax trickle
// drained them toward debt jail; the whole shortfall was 792,328 credits.
func TestPlanTopsUpToTheFloor(t *testing.T) {
	p := PlanTopUp([]Candidate{
		cand("assist-krynn", "Krynn Keeper", 0),
		cand("explorer-4", "Nova Nash", 2500),
	}, 10000, 0, nil)

	if len(p.Grants) != 2 {
		t.Fatalf("want 2 grants, got %+v", p.Grants)
	}
	if p.Grants[0].Amount != 10000 || p.Grants[1].Amount != 7500 {
		t.Fatalf("top-up must reach the floor, not add it: %+v", p.Grants)
	}
	if p.Total != 17500 {
		t.Fatalf("total = %v, want 17500", p.Total)
	}
}

// An agent already at or above the floor is left alone. Funding it would be
// money moved for no reason, and the banker must be boring and predictable.
func TestPlanSkipsAgentsAtOrAboveTheFloor(t *testing.T) {
	p := PlanTopUp([]Candidate{
		cand("trader-10", "Rich Trader", 36_000_000),
		cand("edge", "Edge Case", 10000),
	}, 10000, 0, nil)

	if len(p.Grants) != 0 {
		t.Fatalf("nobody at or above the floor may be funded: %+v", p.Grants)
	}
}

// send_gift addresses a USERNAME, and 106 of 172 agents have one that differs
// from their agent id. An agent whose username we do not know cannot be paid,
// and guessing agent_id would silently pay the wrong player or fail.
func TestPlanSkipsAnAgentWithNoUsername(t *testing.T) {
	p := PlanTopUp([]Candidate{cand("ghost", "", 0)}, 10000, 0, nil)

	if len(p.Grants) != 0 {
		t.Fatalf("an unaddressable agent must never be granted: %+v", p.Grants)
	}
	if len(p.Skips) != 1 || p.Skips[0].Reason == "" {
		t.Fatalf("the skip must be recorded with a reason: %+v", p.Skips)
	}
}

// The funding agent must never pay itself.
func TestPlanExcludesTheFunder(t *testing.T) {
	p := PlanTopUp([]Candidate{
		cand("trader-10", "Rich Trader", 0),
		cand("miner-7", "Mining Mike", 2),
	}, 10000, 0, map[string]bool{"trader-10": true})

	if len(p.Grants) != 1 || p.Grants[0].AgentID != "miner-7" {
		t.Fatalf("the funder must be excluded from its own sweep: %+v", p.Grants)
	}
}

// maxTotal is a hard spend cap. Candidates are offered the money neediest
// first, and a grant that does not fit is skipped WHOLE rather than trimmed --
// a partial top-up leaves an agent short of the floor, which is the state the
// floor exists to prevent.
//
// The sweep then keeps going rather than stopping at the first unaffordable
// grant, so leftover headroom still helps someone. That is deliberate and has a
// cost worth naming: a less needy agent can be funded while a needier one is
// skipped. Here a cap of 11,000 covers broke (10,000) but not middling (5,000),
// and the remaining 1,000 reaches rich-ish instead. Helping a second agent
// beats stranding the headroom, and anyone skipped is named in the output.
func TestPlanSpendCapSkipsWholeGrantsAndKeepsFilling(t *testing.T) {
	p := PlanTopUp([]Candidate{
		cand("rich-ish", "Nearly Fine", 9000), // needs 1000
		cand("broke", "Dead Broke", 0),        // needs 10000
		cand("middling", "Half There", 5000),  // needs 5000
	}, 10000, 11000, nil)

	if p.Total > 11000 {
		t.Fatalf("spend cap breached: total=%v", p.Total)
	}
	if len(p.Grants) != 2 || p.Grants[0].AgentID != "broke" || p.Grants[1].AgentID != "rich-ish" {
		t.Fatalf("neediest first, then fill the headroom: %+v", p.Grants)
	}
	if p.Total != 11000 {
		t.Fatalf("headroom should be used: total=%v want 11000", p.Total)
	}
	if len(p.Skips) != 1 || p.Skips[0].AgentID != "middling" {
		t.Fatalf("the unaffordable grant must be skipped whole, not trimmed: %+v", p.Skips)
	}
}

// A zero or negative floor funds nobody. Guards against an empty flag wiring
// itself into "pay everyone nothing" or, worse, a negative top-up.
func TestPlanWithNoFloorFundsNobody(t *testing.T) {
	if p := PlanTopUp([]Candidate{cand("a", "A", 0)}, 0, 0, nil); len(p.Grants) != 0 {
		t.Fatalf("a zero floor must fund nobody: %+v", p.Grants)
	}
}
