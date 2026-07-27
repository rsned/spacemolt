package worker

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// boardEntry builds a deliver-shaped mission (real wire shape: no
// requirements block, single deliver_item objective): deliver qty of item to
// destBase (in destSystem), paying reward credits, expiring in expiry ticks
// (0 = never).
func boardEntry(id, item string, qty int, destBase, destSystem string, reward, expiry int) serverapi.MissionBoardEntry {
	return serverapi.MissionBoardEntry{
		MissionID: id, Type: "delivery", Title: "Deliver " + item,
		ExpiresInTicks: expiry,
		Rewards:        &serverapi.MissionRewards{Credits: reward},
		Objectives: []serverapi.MissionObjective{{
			Type: "deliver_item", ItemID: item, Quantity: qty, TargetBaseID: destBase, SystemID: destSystem,
		}},
	}
}

func TestBuildMissionCandidate(t *testing.T) {
	dist := map[string]int{"sol": 2, "haven": 0}
	ask := func(item string) (float64, bool) {
		if item == "steel" {
			return 20, true
		}
		return 0, false
	}
	noFuel := func(jumps int) float64 { return 0 }

	t.Run("deliver mission prices and routes", func(t *testing.T) {
		c, reason := buildMissionCandidate(boardEntry("m1", "steel", 20, "sol_station", "sol", 3000, 0), dist, ask, noFuel, false, 1)
		if reason != "" {
			t.Fatalf("rejected: %s", reason)
		}
		if c.ItemID != "steel" || c.Qty != 20 || c.DestSystem != "sol" || c.Jumps != 2 {
			t.Fatalf("candidate mismatch: %+v", c)
		}
		if c.ItemCost != 400 || c.Net != 3000-400 {
			t.Fatalf("economics mismatch: cost=%v net=%v", c.ItemCost, c.Net)
		}
	})

	t.Run("provided items reduce buy quantity", func(t *testing.T) {
		e := boardEntry("m2", "steel", 20, "sol_station", "sol", 3000, 0)
		e.ProvidedItems = map[string]int{"steel": 20}
		c, reason := buildMissionCandidate(e, dist, ask, noFuel, false, 1)
		if reason != "" {
			t.Fatalf("rejected: %s", reason)
		}
		if c.BuyQty != 0 || c.ItemCost != 0 {
			t.Fatalf("provided cargo should zero the buy: %+v", c)
		}
	})

	t.Run("non-deliver mission rejected", func(t *testing.T) {
		e := serverapi.MissionBoardEntry{
			MissionID: "m3", Type: "combat", Title: "Hunt",
			Rewards:    &serverapi.MissionRewards{Credits: 5000},
			Objectives: []serverapi.MissionObjective{{Type: "kill_creature", Quantity: 3}},
		}
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel, false, 1); reason == "" {
			t.Fatal("kill mission must be rejected")
		}
	})

	// Smuggling is deliver-shaped, so nothing about the objective distinguishes
	// it from ordinary freight — the type gate is the only thing standing
	// between a worker and a contraband run. It must stay closed by default and
	// open only for a worker whose operator allowlisted the category.
	t.Run("smuggling rejected unless the category is enabled", func(t *testing.T) {
		e := boardEntry("m3b", "steel", 20, "sol_station", "sol", 3000, 0)
		e.Type = "smuggling"
		// Real contraband couriers hand the goods over on accept; a smuggling
		// run we would have to source is separately (and correctly) refused,
		// so provide the items to keep this test about the TYPE gate.
		e.ProvidedItems = map[string]int{"steel": 20}
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel, false, 1); reason == "" {
			t.Fatal("smuggling must be rejected when the category is not enabled")
		}
		if c, reason := buildMissionCandidate(e, dist, ask, noFuel, true, 1); reason != "" {
			t.Fatalf("smuggling must be accepted once enabled, got %q (%+v)", reason, c)
		}
	})

	// Contraband board entries carry warnings (insurance voided, and so on).
	// Rejecting on warnings is right for ordinary freight — it is what keeps an
	// uninsured account off a contraband run it never opted into — but for
	// smuggling the warning IS the category, so enabling it is the opt-in.
	t.Run("warnings tolerated for smuggling only", func(t *testing.T) {
		e := boardEntry("m3d", "steel", 5, "haven_station", "haven", 800, 0)
		e.Type = "smuggling"
		e.ProvidedItems = map[string]int{"steel": 5} // couriers supply the goods
		e.Warnings = []string{"contraband cargo", "insurance voided"}
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel, true, 1); reason != "" {
			t.Fatalf("an enabled smuggling mission must tolerate its own warnings, got %q", reason)
		}
		d := boardEntry("m3e", "steel", 20, "sol_station", "sol", 3000, 0)
		d.Warnings = []string{"insurance voided"}
		if _, reason := buildMissionCandidate(d, dist, ask, noFuel, true, 1); reason == "" {
			t.Fatal("a DELIVERY mission with warnings must still be rejected even on a smuggling-enabled worker")
		}
	})

	t.Run("multi-leg chain rejected", func(t *testing.T) {
		e := serverapi.MissionBoardEntry{
			MissionID: "m3c", Type: "delivery", Title: "First Links",
			Rewards: &serverapi.MissionRewards{Credits: 3000},
			Objectives: []serverapi.MissionObjective{
				{Type: "deliver_item", ItemID: "steel", Quantity: 20, TargetBaseID: "sol_station", SystemID: "sol"},
				{Type: "deliver_item", ItemID: "steel", Quantity: 10, TargetBaseID: "haven_station", SystemID: "haven"},
			},
		}
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel, false, 1); reason == "" {
			t.Fatal("multi-leg (two deliver_item objectives) mission must be rejected")
		}
	})

	t.Run("module-gated mission rejected", func(t *testing.T) {
		e := boardEntry("m4", "steel", 20, "sol_station", "sol", 3000, 0)
		e.RequiredModules = []string{"smuggler_hold"}
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel, false, 1); reason == "" {
			t.Fatal("module-gated mission must be rejected")
		}
	})

	t.Run("tight expiry rejected", func(t *testing.T) {
		e := boardEntry("m5", "steel", 20, "sol_station", "sol", 3000, 30)
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel, false, 1); reason == "" {
			t.Fatal("30-tick expiry must be rejected (arbitrage-expiry lesson)")
		}
	})

	t.Run("unpriceable item rejected", func(t *testing.T) {
		e := boardEntry("m6", "unobtainium", 5, "sol_station", "sol", 3000, 0)
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel, false, 1); reason == "" {
			t.Fatal("no reference ask + buy needed must be rejected")
		}
	})

	t.Run("unreachable destination rejected", func(t *testing.T) {
		e := boardEntry("m7", "steel", 20, "far_station", "far_system", 3000, 0)
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel, false, 1); reason == "" {
			t.Fatal("destination missing from dist map must be rejected")
		}
	})

	t.Run("negative net rejected", func(t *testing.T) {
		e := boardEntry("m8", "steel", 100, "sol_station", "sol", 500, 0) // cost 2000 > reward 500
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel, false, 1); reason == "" {
			t.Fatal("negative-net mission must be rejected")
		}
	})

	t.Run("mission with warnings rejected", func(t *testing.T) {
		e := boardEntry("m9", "steel", 20, "sol_station", "sol", 3000, 0)
		e.Warnings = []string{"contraband: insurance voided"}
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel, false, 1); reason == "" {
			t.Fatal("deliver-shaped mission carrying warnings must be rejected")
		}
	})
}

func TestSelectMissionSet(t *testing.T) {
	mk := func(id, destSystem string, net float64, jumps, buyQty, qty int) missionCandidate {
		return missionCandidate{
			Entry:  serverapi.MissionBoardEntry{MissionID: id},
			ItemID: "steel", Qty: qty, BuyQty: buyQty,
			ItemCost: float64(buyQty) * 20, DestSystem: destSystem,
			Net: net, Jumps: jumps, Reward: net + float64(buyQty)*20,
		}
	}
	const credits, cargoFree = 10000.0, 100.0

	t.Run("stacks same-destination missions best-net first", func(t *testing.T) {
		got := SelectMissionSet([]missionCandidate{
			mk("low", "sol", 1000, 2, 10, 10),
			mk("best", "sol", 3000, 2, 10, 10),
			mk("elsewhere", "krynn", 2000, 1, 10, 10),
		}, credits, cargoFree, 5)
		if len(got) != 2 || got[0].Entry.MissionID != "best" || got[1].Entry.MissionID != "low" {
			t.Fatalf("want [best low] (same destination as anchor), got %+v", got)
		}
	})

	t.Run("caps at MissionMaxStack", func(t *testing.T) {
		var cands []missionCandidate
		for i := range 8 {
			cands = append(cands, mk(string(rune('a'+i)), "sol", float64(1000+i), 2, 1, 1))
		}
		if got := SelectMissionSet(cands, credits, cargoFree, 5); len(got) != MissionMaxStack {
			t.Fatalf("got %d, want %d", len(got), MissionMaxStack)
		}
	})

	t.Run("respects buy budget", func(t *testing.T) {
		// Each costs 2000; budget = 3000*0.8 = 2400 -> only one fits.
		got := SelectMissionSet([]missionCandidate{
			mk("a", "sol", 3000, 2, 100, 100),
			mk("b", "sol", 2900, 2, 100, 100),
		}, 3000, 1000, 5)
		if len(got) != 1 {
			t.Fatalf("budget must cap the stack: got %d", len(got))
		}
	})

	t.Run("respects cargo space", func(t *testing.T) {
		got := SelectMissionSet([]missionCandidate{
			mk("a", "sol", 3000, 2, 60, 60),
			mk("b", "sol", 2900, 2, 60, 60),
		}, 100000, 100, 5)
		if len(got) != 1 {
			t.Fatalf("cargo must cap the stack: got %d", len(got))
		}
	})

	t.Run("drops anchors beyond an explicit maxJumps", func(t *testing.T) {
		got := SelectMissionSet([]missionCandidate{mk("far", "sol", 9000, 9, 1, 1)}, credits, cargoFree, 5)
		if len(got) != 0 {
			t.Fatalf("9-jump destination with maxJumps=5 must be dropped: %+v", got)
		}
	})

	// maxJumps <= 0 means NO distance cap. It used to fall back to a hardcoded
	// 5, which made every long-haul mission invisible to the whole fleet —
	// including the only route to smuggling level 2 (a 17-jump cross-border
	// run). Distance is now priced, not forbidden: the expiry gate scales with
	// jumps, fuel cost enters the net, and Autopilot tops the tank up before
	// departing.
	t.Run("no cap when maxJumps is unset", func(t *testing.T) {
		got := SelectMissionSet([]missionCandidate{mk("crossborder", "sol", 9000, 17, 1, 1)}, credits, cargoFree, 0)
		if len(got) != 1 {
			t.Fatalf("a 17-jump mission must be selectable with no cap configured, got %+v", got)
		}
	})
}

func TestResolveActiveMissionIDs(t *testing.T) {
	t.Run("primary match by TemplateID", func(t *testing.T) {
		accepted := []missionCandidate{{Entry: serverapi.MissionBoardEntry{MissionID: "m1", Title: "Deliver steel"}}}
		actives := []serverapi.ActiveMission{{MissionID: "hex-1", TemplateID: "m1"}}
		got := resolveActiveMissionIDs(accepted, actives)
		if len(got) != 1 || got[0].ActiveID != "hex-1" {
			t.Fatalf("want ActiveID hex-1, got %+v", got)
		}
	})

	t.Run("fallback match by title + objective when TemplateID missing", func(t *testing.T) {
		accepted := []missionCandidate{{
			Entry:  serverapi.MissionBoardEntry{MissionID: "m1", Title: "Deliver steel"},
			ItemID: "steel", Qty: 20, DestBaseID: "sol_station",
		}}
		actives := []serverapi.ActiveMission{{
			MissionID: "hex-1", Title: "Deliver steel",
			Objectives: []serverapi.ActiveMissionObjective{{ItemID: "steel", Required: 20, TargetBase: "sol_station"}},
		}}
		got := resolveActiveMissionIDs(accepted, actives)
		if len(got) != 1 || got[0].ActiveID != "hex-1" {
			t.Fatalf("want ActiveID hex-1 via fallback match, got %+v", got)
		}
	})

	t.Run("unresolved candidate gets empty ActiveID", func(t *testing.T) {
		accepted := []missionCandidate{{Entry: serverapi.MissionBoardEntry{MissionID: "m1", Title: "Deliver steel"}}}
		got := resolveActiveMissionIDs(accepted, nil)
		if len(got) != 1 || got[0].ActiveID != "" {
			t.Fatalf("want empty ActiveID when nothing matches, got %+v", got)
		}
	})

	t.Run("each active consumed by at most one candidate", func(t *testing.T) {
		// Two board entries share the same template id (shouldn't happen live,
		// but the resolver must not double-assign the one matching active).
		accepted := []missionCandidate{
			{Entry: serverapi.MissionBoardEntry{MissionID: "m1", Title: "Deliver steel"}},
			{Entry: serverapi.MissionBoardEntry{MissionID: "m1", Title: "Deliver steel"}},
		}
		actives := []serverapi.ActiveMission{{MissionID: "hex-1", TemplateID: "m1"}}
		got := resolveActiveMissionIDs(accepted, actives)
		if len(got) != 2 {
			t.Fatalf("want 2 candidates, got %d", len(got))
		}
		resolvedCount := 0
		for _, c := range got {
			if c.ActiveID != "" {
				resolvedCount++
			}
		}
		if resolvedCount != 1 {
			t.Fatalf("only one candidate should claim the single matching active, got %d resolved: %+v", resolvedCount, got)
		}
	})
}

// TestSmugglingGate covers the smuggling-specific economics. These runs are
// taken for SKILL XP, not credits: the ladder to chain 2 (free pirate-stronghold
// docking) and chain 3 (Crimson wormholes) is gated on smuggling level, and the
// couriers that supply that XP pay 300-1400 cr against real fuel. Judged on
// credits alone every one of them is a loss, which is exactly what stranded
// engineer-1 at level 2 with 88/340 XP and zero accepts.
func TestSmugglingGate(t *testing.T) {
	dist := map[string]int{"sol": 2, "haven": 0, "far": 10}
	ask := func(item string) (float64, bool) {
		if item == "steel" {
			return 20, true
		}
		return 0, false // contraband is not sold anywhere
	}
	noFuel := func(jumps int) float64 { return 0 }
	costlyFuel := func(jumps int) float64 { return float64(jumps) * 300 }

	// smugglingRun builds a provided-items contraband courier: nothing to buy,
	// paying credits plus smuggling XP.
	smugglingRun := func(id string, qty, reward, expiry, xp int, destSystem string) serverapi.MissionBoardEntry {
		e := boardEntry(id, "starshine", qty, destSystem+"_station", destSystem, reward, expiry)
		e.Type = "smuggling"
		e.ProvidedItems = map[string]int{"starshine": qty}
		e.Rewards.SkillXP = map[string]int{"smuggling": xp}
		return e
	}

	t.Run("XP carries a run whose credit net is negative", func(t *testing.T) {
		// 10 jumps of fuel against a 1400 cr reward: -1600 on credits alone.
		// 175 XP is most of the 252 this worker needs for the chain-2 unlock.
		e := smugglingRun("s1", 5, 1400, 900, 175, "far")
		c, reason := buildMissionCandidate(e, dist, ask, costlyFuel, true, 1)
		if reason != "" {
			t.Fatalf("XP-rich courier rejected: %s", reason)
		}
		if c.Net >= 0 {
			t.Fatalf("expected a negative credit net, got %.0f", c.Net)
		}
	})

	t.Run("XP does not excuse an arbitrarily bad trade", func(t *testing.T) {
		// Same 10-jump cost, but only 5 XP: not worth 3000 credits of fuel.
		e := smugglingRun("s2", 5, 1400, 900, 5, "far")
		if _, reason := buildMissionCandidate(e, dist, ask, costlyFuel, true, 1); reason == "" {
			t.Fatal("an XP-poor, fuel-expensive courier must still be rejected")
		}
	})

	t.Run("a mission we must source contraband for is rejected", func(t *testing.T) {
		// The operator's second smuggling shape: "source the goods yourself".
		// Contraband has no sell orders anywhere, so this can never be
		// completed and must never be accepted. ProvidedItems is short.
		e := smugglingRun("s3", 5, 5000, 900, 175, "haven")
		e.ProvidedItems = map[string]int{"starshine": 2} // 3 short
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel, true, 1); reason == "" {
			t.Fatal("a smuggling mission requiring us to buy contraband must be rejected")
		}
	})

	t.Run("sourcing rejection holds even when the item happens to be priceable", func(t *testing.T) {
		// Do not lean on the ref-ask lookup failing: a stale ask would let an
		// uncompletable run through. Steel is priceable in this fixture.
		e := smugglingRun("s4", 5, 5000, 900, 175, "haven")
		e.Objectives[0].ItemID = "steel"
		e.ProvidedItems = map[string]int{"steel": 0}
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel, true, 1); reason == "" {
			t.Fatal("smuggling must reject a buy-it-yourself run even when priced")
		}
	})

	t.Run("short expiry allowed when no travel is required", func(t *testing.T) {
		// Black-market jobs board AT the current station (0 jumps). The 180-tick
		// base margin refused six of them for runway they never needed.
		e := smugglingRun("s5", 5, 3000, 140, 60, "haven") // haven = 0 jumps
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel, true, 1); reason != "" {
			t.Fatalf("0-jump smuggling run rejected on expiry: %s", reason)
		}
		// The same short window on ordinary freight is still refused.
		d := boardEntry("s6", "steel", 5, "haven_station", "haven", 3000, 140)
		if _, reason := buildMissionCandidate(d, dist, ask, noFuel, true, 1); reason == "" {
			t.Fatal("delivery must keep the conservative expiry margin")
		}
	})

	t.Run("delivery economics are unchanged", func(t *testing.T) {
		// XP valuation must not leak into ordinary freight: a delivery that
		// loses money stays rejected even if it grants XP.
		d := boardEntry("s7", "steel", 5, "far_station", "far", 100, 900)
		d.Rewards.SkillXP = map[string]int{"smuggling": 175}
		if _, reason := buildMissionCandidate(d, dist, ask, costlyFuel, true, 1); reason == "" {
			t.Fatal("XP must not rescue a loss-making DELIVERY mission")
		}
	})
}

// TestSmugglingGateAgainstLiveBoard uses a real board trio the operator
// captured (Grand Exchange Station -> Frontier Station, 2026-07-27): three
// couriers, each +300 cr and +50 smuggling xp, goods PROVIDED ("slides you 5
// units of raw gutter flux"), all to the same destination so they stack into
// one trip. Fuel is the measured rate from engineer-1's across_the_line run:
// 952 credits over 17 jumps, ~56/jump. Priced on credits alone each of these
// is a ~430 credit loss and the floor refuses it; they are worth taking for
// the 150 xp toward the chain-2 unlock.
func TestSmugglingGateAgainstLiveBoard(t *testing.T) {
	dist := map[string]int{"frontier": 13}
	ask := func(string) (float64, bool) { return 0, false }
	measuredFuel := func(jumps int) float64 { return float64(jumps) * 56 }

	courier := func(id, item string, qty int) serverapi.MissionBoardEntry {
		e := boardEntry(id, item, qty, "frontier_station", "frontier", 300, 900)
		e.Type = "smuggling"
		e.ProvidedItems = map[string]int{item: qty}
		e.Rewards.SkillXP = map[string]int{"smuggling": 50}
		return e
	}

	for _, tc := range []struct{ id, item string; qty int }{
		{"gutter", "gutter_flux", 5},
		{"nerve", "nerve_burn", 3},
		{"star", "starshine", 3},
	} {
		c, reason := buildMissionCandidate(courier(tc.id, tc.item, tc.qty), dist, ask, measuredFuel, true, 1)
		if reason != "" {
			t.Fatalf("live courier %s rejected: %s", tc.id, reason)
		}
		if c.Net >= 0 {
			t.Fatalf("%s: expected a credit loss, got net %.0f", tc.id, c.Net)
		}
		if c.DestSystem != "frontier" {
			t.Fatalf("%s: destination %q breaks stacking", tc.id, c.DestSystem)
		}
	}
}

// TestSmugglingBatchFuelSharing covers the batching bonus. Smuggling boards
// commonly offer 2-3 couriers to the SAME destination, and SelectMissionSet
// already stacks by DestSystem — but pricing each candidate against the FULL
// trip fuel rejects them individually before stacking can combine them.
//
// Fuel here is the rate observed live rather than a guess: engineer-1 saw
// "net -1988 below floor 500" on a 300 cr provided-items courier 13 jumps out,
// which puts fuelCostFor(13) at ~2288, i.e. ~176/jump.
func TestSmugglingBatchFuelSharing(t *testing.T) {
	dist := map[string]int{"frontier": 13}
	ask := func(string) (float64, bool) { return 0, false }
	liveFuel := func(jumps int) float64 { return float64(jumps) * 176 }

	courier := func(id string) serverapi.MissionBoardEntry {
		e := boardEntry(id, "ghost_rounds", 3, "frontier_station", "frontier", 300, 900)
		e.Type = "smuggling"
		e.ProvidedItems = map[string]int{"ghost_rounds": 3}
		e.Rewards.SkillXP = map[string]int{"smuggling": 50}
		return e
	}

	t.Run("alone it is a bad trade and stays rejected", func(t *testing.T) {
		if _, reason := buildMissionCandidate(courier("b1"), dist, ask, liveFuel, true, 1); reason == "" {
			t.Fatal("a lone courier paying 50 xp for ~2288 of fuel must be rejected")
		}
	})

	t.Run("three sharing one trip are accepted", func(t *testing.T) {
		c, reason := buildMissionCandidate(courier("b2"), dist, ask, liveFuel, true, 3)
		if reason != "" {
			t.Fatalf("batched courier rejected: %s", reason)
		}
		// Reporting must stay honest: the recorded economics are the REAL
		// full-trip cost, not the shared figure the gate used to admit it.
		if c.FuelCost != 13*176 {
			t.Fatalf("FuelCost must record the full trip, got %.0f", c.FuelCost)
		}
		if c.Net != 300-13*176 {
			t.Fatalf("Net must record the true credit loss, got %.0f", c.Net)
		}
	})

	t.Run("sharing cannot exceed the stack cap", func(t *testing.T) {
		// A board offering 50 identical couriers does not make fuel free: at
		// most MissionMaxStack of them ever ride one trip.
		big, reasonBig := buildMissionCandidate(courier("b3"), dist, ask, liveFuel, true, 50)
		capped, reasonCap := buildMissionCandidate(courier("b4"), dist, ask, liveFuel, true, MissionMaxStack)
		if reasonBig != reasonCap {
			t.Fatalf("share beyond the stack cap changed the verdict: %q vs %q", reasonBig, reasonCap)
		}
		if big.Net != capped.Net {
			t.Fatalf("share beyond the stack cap changed economics: %.0f vs %.0f", big.Net, capped.Net)
		}
	})

	t.Run("delivery missions never share fuel", func(t *testing.T) {
		// Ordinary freight is not batched by this rule; a loss-making delivery
		// stays rejected however many siblings share its destination.
		d := boardEntry("b5", "steel", 3, "frontier_station", "frontier", 300, 900)
		d.ProvidedItems = map[string]int{"steel": 3}
		if _, reason := buildMissionCandidate(d, dist, ask, liveFuel, true, 3); reason == "" {
			t.Fatal("delivery must not get the smuggling batch discount")
		}
	})
}
