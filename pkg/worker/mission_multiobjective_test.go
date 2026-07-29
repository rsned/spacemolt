package worker

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/galaxy"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// anIntroduction is the live shape of the chain-2 entry mission, the one this
// whole feature exists for: TWO deliver_item objectives to the SAME pirate
// stronghold, with both goods handed over on accept. Completing it raises the
// pirate baseline -30 -> 10, which is permanent stronghold docking.
func anIntroduction() serverapi.MissionBoardEntry {
	return serverapi.MissionBoardEntry{
		MissionID: "an_introduction", TemplateID: "an_introduction",
		Type: "smuggling", Title: "An Introduction",
		ChainNext:      "supply_run",
		ExpiresInTicks: 20000,
		Rewards:        &serverapi.MissionRewards{Credits: 3000, SkillXP: map[string]int{"smuggling": 250}},
		ProvidedItems:  map[string]int{"starshine": 10, "nerve_burn": 5},
		Objectives: []serverapi.MissionObjective{
			{Type: "deliver_item", ItemID: "starshine", Quantity: 10, TargetBaseID: "voss_redoubt", SystemID: "alhena"},
			{Type: "deliver_item", ItemID: "nerve_burn", Quantity: 5, TargetBaseID: "voss_redoubt", SystemID: "alhena"},
		},
	}
}

// The headline case: before this change deliverShape took len(Objectives)==1
// only, so an_introduction was skipped as "not a plain deliver mission" every
// pass while sitting on the giver's board.
func TestChainMissionWithTwoObjectivesIsRunnable(t *testing.T) {
	dist := map[string]int{"alhena": 4}
	noAsk := func(string) (float64, bool) { return 0, false }
	noFuel := func(int) float64 { return 0 }

	c, reason := buildMissionCandidate(anIntroduction(), dist, noAsk, noFuel, true, 1, missionSmugglingXPFloor, 6)
	if reason != "" {
		t.Fatalf("an_introduction refused: %s", reason)
	}
	if len(c.Items) != 2 {
		t.Fatalf("Items = %d, want 2 (starshine + nerve_burn)", len(c.Items))
	}
	// The hold must be sized for BOTH objectives; charging only the first
	// would let a 15-unit mission board a ship with room for 10.
	if c.Qty != 15 {
		t.Errorf("Qty = %d, want 15 (10 starshine + 5 nerve_burn)", c.Qty)
	}
	// Both goods are provided on accept, so nothing is sourced or paid for.
	if c.BuyQty != 0 || c.ItemCost != 0 {
		t.Errorf("BuyQty/ItemCost = %d/%.0f, want 0/0 (mission provides both items)", c.BuyQty, c.ItemCost)
	}
	if c.DestBaseID != "voss_redoubt" || c.DestSystem != "alhena" {
		t.Errorf("destination = %s/%s, want voss_redoubt/alhena", c.DestBaseID, c.DestSystem)
	}
}

// A mission splitting its objectives across bases would strand half the cargo:
// the delivery executor routes to ONE destination. Refused with a reason that
// names the problem rather than the generic shape rejection.
func TestSplitDestinationDeliveryIsRefused(t *testing.T) {
	e := anIntroduction()
	e.Objectives[1].TargetBaseID = "sable_port"
	e.Objectives[1].SystemID = "kaus"

	_, reason := buildMissionCandidate(e, map[string]int{"alhena": 4, "kaus": 5}, func(string) (float64, bool) { return 0, false }, func(int) float64 { return 0 }, true, 1, missionSmugglingXPFloor, 6)
	if reason == "" {
		t.Fatal("a two-destination delivery was accepted; the executor can only reach one base")
	}
	if !contains(reason, "destination") {
		t.Errorf("reason = %q, want it to name the destination split", reason)
	}
}

// Provided items are granted as a POOL keyed by item id. A mission listing the
// same good twice must subtract that pool once — subtracting per objective
// double-counts the grant and invents a purchase that isn't needed (and, for
// smuggling, a hard "must source it itself" rejection of a runnable mission).
func TestRepeatedItemObjectivesMergeBeforeSubtractingProvided(t *testing.T) {
	e := anIntroduction()
	e.Objectives = []serverapi.MissionObjective{
		{Type: "deliver_item", ItemID: "starshine", Quantity: 6, TargetBaseID: "voss_redoubt", SystemID: "alhena"},
		{Type: "deliver_item", ItemID: "starshine", Quantity: 4, TargetBaseID: "voss_redoubt", SystemID: "alhena"},
	}
	e.ProvidedItems = map[string]int{"starshine": 10}

	c, reason := buildMissionCandidate(e, map[string]int{"alhena": 4}, func(string) (float64, bool) { return 0, false }, func(int) float64 { return 0 }, true, 1, missionSmugglingXPFloor, 6)
	if reason != "" {
		t.Fatalf("refused a fully provided mission: %s", reason)
	}
	if len(c.Items) != 1 {
		t.Fatalf("Items = %d, want 1 (the two starshine objectives merge)", len(c.Items))
	}
	if c.Qty != 10 || c.BuyQty != 0 {
		t.Errorf("Qty/BuyQty = %d/%d, want 10/0 — the 10 provided cover both objectives", c.Qty, c.BuyQty)
	}
}

// Multi-item SOURCING is deliberately out of scope: the acquisition path prices
// one ladder per candidate and divides ItemCost by a single BuyQty, so a
// multi-item buy would mis-price. Refused explicitly rather than half-supported.
func TestMultiItemMissionNeedingAPurchaseIsRefused(t *testing.T) {
	e := anIntroduction()
	e.Type = "delivery" // sidestep the smuggling contraband rule to reach the sourcing rule
	e.ProvidedItems = map[string]int{"starshine": 10} // nerve_burn NOT provided

	_, reason := buildMissionCandidate(e, map[string]int{"alhena": 4}, func(string) (float64, bool) { return 5, true }, func(int) float64 { return 0 }, true, 1, missionMinNet, 6)
	if reason == "" {
		t.Fatal("a multi-item mission requiring a purchase was accepted; sourcing is single-item only")
	}
	if !contains(reason, "source") {
		t.Errorf("reason = %q, want it to name the sourcing limit", reason)
	}
}

// Single-objective missions must come out bit-identical: this feature must not
// move the economics of the fleet's ordinary deliveries.
func TestSingleObjectiveCandidateIsUnchanged(t *testing.T) {
	e := boardEntry("d1", "steel", 4, "sol_central", "sol", 1000, 400)
	c, reason := buildMissionCandidate(e, map[string]int{"sol": 2}, func(string) (float64, bool) { return 20, true }, func(int) float64 { return 10 }, false, 1, missionMinNet, 6)
	if reason != "" {
		t.Fatalf("plain delivery refused: %s", reason)
	}
	if c.ItemID != "steel" || c.Qty != 4 || c.BuyQty != 4 {
		t.Errorf("ItemID/Qty/BuyQty = %s/%d/%d, want steel/4/4", c.ItemID, c.Qty, c.BuyQty)
	}
	if c.ItemCost != 80 || c.Net != 1000-80-10 {
		t.Errorf("ItemCost/Net = %.0f/%.0f, want 80/910", c.ItemCost, c.Net)
	}
	if len(c.Items) != 1 {
		t.Errorf("Items = %d, want 1", len(c.Items))
	}
}

// The stronghold guard exists because a worker was destroyed flying to one, so
// it stays on for procedural couriers. But an_introduction's OWN destination is
// a stronghold and the mission carries one-time passage — a blanket refusal
// permanently blocks the single mission that grants stronghold access.
func TestStrongholdEndpointLegalOnlyForAChainMission(t *testing.T) {
	strongholds := map[string]bool{"alhena": true}

	chain := missionCandidate{Entry: anIntroduction(), DestSystem: "alhena"}
	if hold, ok := missionStrongholdHop(strongholds, nil, "haven", chain); !ok {
		t.Errorf("chain mission refused at %s; it carries its own passage", hold)
	}

	// Same destination, no chain marker: still refused.
	courier := anIntroduction()
	courier.ChainNext = ""
	courier.MissionID = "smuggling_courier_x~deadbeef"
	if _, ok := missionStrongholdHop(strongholds, nil, "haven", missionCandidate{Entry: courier, DestSystem: "alhena"}); ok {
		t.Error("a procedural courier was allowed into a stronghold; the guard must still refuse it")
	}
}

// Passage covers the DESTINATION, not the road. A chain mission routed through
// some OTHER stronghold is still refused.
func TestChainPassageDoesNotCoverStrongholdsOnTheWay(t *testing.T) {
	strongholds := map[string]bool{"alhena": true, "sable": true}
	chain := missionCandidate{Entry: anIntroduction(), DestSystem: "alhena"}

	pathVia := func(from, to string, _ bool) (galaxy.Route, error) {
		return galaxy.Route{Path: []string{from, "sable", to}}, nil
	}
	if missionRouteClear(pathVia, strongholds, "haven", "alhena", true) {
		t.Error("route through the sable stronghold was cleared; passage covers the destination only")
	}
	// The destination itself appearing in the path must NOT trip the check.
	direct := func(from, to string, _ bool) (galaxy.Route, error) {
		return galaxy.Route{Path: []string{from, to}}, nil
	}
	if !missionRouteClear(direct, strongholds, "haven", "alhena", true) {
		t.Error("a direct route was refused because the destination is a stronghold; that is what passage is for")
	}
	// Without passage the same direct route stays refused.
	if missionRouteClear(direct, strongholds, "haven", "alhena", false) {
		t.Error("a stronghold destination was cleared for a mission carrying no passage")
	}
	_ = chain
}

// A restart mid-run must not lose a multi-objective mission: the resume path
// gated on len(Objectives)==1 too, so an accepted an_introduction would have
// been skipped as unrecognised and left to rot.
func TestHeldMultiObjectiveDeliveryResumes(t *testing.T) {
	m := serverapi.ActiveMission{
		MissionID: "abc123", Type: "smuggling", Title: "An Introduction", ChainNext: "supply_run",
		Objectives: []serverapi.ActiveMissionObjective{
			{Type: "deliver_item", ItemID: "starshine", Required: 10, TargetBase: "voss_redoubt", SystemID: "alhena"},
			{Type: "deliver_item", ItemID: "nerve_burn", Required: 5, TargetBase: "voss_redoubt", SystemID: "alhena"},
		},
	}
	hold := map[string]float64{"starshine": 10, "nerve_burn": 5}

	h, ok := heldDeliveryShape(m, func(id string) float64 { return hold[id] })
	if !ok {
		t.Fatal("a two-objective active delivery was not recognised")
	}
	if !h.Covered {
		t.Errorf("Covered = false (short %s %.0f/%d), want true — the hold carries both", h.ShortItem, h.ShortAboard, h.ShortNeed)
	}
	if h.TotalRemaining != 15 || h.DestBase != "voss_redoubt" {
		t.Errorf("remaining/dest = %d/%s, want 15/voss_redoubt", h.TotalRemaining, h.DestBase)
	}

	// Short one item: abandoned, and the message must name the item actually missing.
	hold["nerve_burn"] = 2
	h2, _ := heldDeliveryShape(m, func(id string) float64 { return hold[id] })
	if h2.Covered {
		t.Fatal("Covered = true while 3 nerve_burn are missing")
	}
	if h2.ShortItem != "nerve_burn" || h2.ShortNeed != 5 {
		t.Errorf("short = %s %d, want nerve_burn 5", h2.ShortItem, h2.ShortNeed)
	}
}

// Coverage must be judged per item AFTER merging. Checking each objective
// against the hold separately passes 6+4 starshine on a hold of only 6, because
// the same units answer both objectives.
func TestHeldCoverageIsJudgedPerItemNotPerObjective(t *testing.T) {
	m := serverapi.ActiveMission{
		MissionID: "abc123", Type: "smuggling", Title: "Double",
		Objectives: []serverapi.ActiveMissionObjective{
			{Type: "deliver_item", ItemID: "starshine", Required: 6, TargetBase: "voss_redoubt", SystemID: "alhena"},
			{Type: "deliver_item", ItemID: "starshine", Required: 4, TargetBase: "voss_redoubt", SystemID: "alhena"},
		},
	}
	h, ok := heldDeliveryShape(m, func(string) float64 { return 6 })
	if !ok {
		t.Fatal("shape rejected")
	}
	if h.Covered {
		t.Error("6 units were counted as covering two objectives totalling 10")
	}
	if h.TotalRemaining != 10 {
		t.Errorf("TotalRemaining = %d, want 10", h.TotalRemaining)
	}
}

// A completed objective owes nothing even with an empty hold: storage at the
// target base counts toward delivery and the wire reports it as in_storage.
func TestHeldCompletedObjectiveNeedsNoCargo(t *testing.T) {
	m := serverapi.ActiveMission{
		MissionID: "abc123", Type: "delivery", Title: "Done",
		Objectives: []serverapi.ActiveMissionObjective{
			{Type: "deliver_item", ItemID: "steel", Required: 4, Current: 4, Completed: true, TargetBase: "sol_central", SystemID: "sol"},
		},
	}
	h, ok := heldDeliveryShape(m, func(string) float64 { return 0 })
	if !ok || !h.Covered {
		t.Errorf("ok/Covered = %v/%v, want true/true for a completed objective", ok, h.Covered)
	}
}

// a_word_in_private is the ONLY smuggling XP a level-0 agent can earn: every
// smuggling-typed mission needs level 1 already, so this delivery-typed visit
// is the whole bootstrap. Its single objective is dock_at_base with no item,
// which deliverShape refused — leaving a fresh agent unable to start the chain
// at all. Offered at 13 bases including treasure_cache_trading_post.
func TestDockAtBaseBootstrapIsRunnable(t *testing.T) {
	e := serverapi.MissionBoardEntry{
		MissionID: "a_word_in_private", TemplateID: "a_word_in_private",
		Type: "delivery", Title: "A Word in Private",
		ExpiresInTicks: 5000,
		Rewards:        &serverapi.MissionRewards{Credits: 500, SkillXP: map[string]int{"smuggling": 50}},
		Objectives: []serverapi.MissionObjective{{
			Type: "dock_at_base", TargetBaseID: "treasure_cache_trading_post", SystemID: "treasure_cache",
		}},
	}

	c, reason := buildMissionCandidate(e, map[string]int{"treasure_cache": 3}, func(string) (float64, bool) { return 0, false }, func(int) float64 { return 0 }, false, 1, missionMinNet, 6)
	if reason != "" {
		t.Fatalf("the level-0 bootstrap mission was refused: %s", reason)
	}
	// Nothing is carried, so it must not consume cargo budget or claim to.
	if len(c.Items) != 0 || c.Qty != 0 || c.BuyQty != 0 || c.ItemCost != 0 {
		t.Errorf("items/qty/buy/cost = %d/%d/%d/%.0f, want all zero for a visit", len(c.Items), c.Qty, c.BuyQty, c.ItemCost)
	}
	if c.DestBaseID != "treasure_cache_trading_post" || c.DestSystem != "treasure_cache" {
		t.Errorf("destination = %s/%s, want treasure_cache_trading_post/treasure_cache", c.DestBaseID, c.DestSystem)
	}
}

// A held visit owes nothing, so it is deliverable the moment it is accepted —
// it must never be abandoned as "cargo_lost" for carrying no cargo.
func TestHeldDockAtBaseOwesNothing(t *testing.T) {
	m := serverapi.ActiveMission{
		MissionID: "abc", Type: "delivery", Title: "A Word in Private",
		Objectives: []serverapi.ActiveMissionObjective{{
			Type: "dock_at_base", TargetBase: "treasure_cache_trading_post", SystemID: "treasure_cache",
		}},
	}
	h, ok := heldDeliveryShape(m, func(string) float64 { return 0 })
	if !ok {
		t.Fatal("a held dock_at_base mission was not recognised")
	}
	if !h.Covered {
		t.Errorf("Covered = false for a visit that owes nothing (short %s)", h.ShortItem)
	}
	if h.DestBase != "treasure_cache_trading_post" {
		t.Errorf("DestBase = %s, want treasure_cache_trading_post", h.DestBase)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
