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
		c, reason := buildMissionCandidate(boardEntry("m1", "steel", 20, "sol_station", "sol", 3000, 0), dist, ask, noFuel, false)
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
		c, reason := buildMissionCandidate(e, dist, ask, noFuel, false)
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
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel, false); reason == "" {
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
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel, false); reason == "" {
			t.Fatal("smuggling must be rejected when the category is not enabled")
		}
		if c, reason := buildMissionCandidate(e, dist, ask, noFuel, true); reason != "" {
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
		e.Warnings = []string{"contraband cargo", "insurance voided"}
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel, true); reason != "" {
			t.Fatalf("an enabled smuggling mission must tolerate its own warnings, got %q", reason)
		}
		d := boardEntry("m3e", "steel", 20, "sol_station", "sol", 3000, 0)
		d.Warnings = []string{"insurance voided"}
		if _, reason := buildMissionCandidate(d, dist, ask, noFuel, true); reason == "" {
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
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel, false); reason == "" {
			t.Fatal("multi-leg (two deliver_item objectives) mission must be rejected")
		}
	})

	t.Run("module-gated mission rejected", func(t *testing.T) {
		e := boardEntry("m4", "steel", 20, "sol_station", "sol", 3000, 0)
		e.RequiredModules = []string{"smuggler_hold"}
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel, false); reason == "" {
			t.Fatal("module-gated mission must be rejected")
		}
	})

	t.Run("tight expiry rejected", func(t *testing.T) {
		e := boardEntry("m5", "steel", 20, "sol_station", "sol", 3000, 30)
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel, false); reason == "" {
			t.Fatal("30-tick expiry must be rejected (arbitrage-expiry lesson)")
		}
	})

	t.Run("unpriceable item rejected", func(t *testing.T) {
		e := boardEntry("m6", "unobtainium", 5, "sol_station", "sol", 3000, 0)
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel, false); reason == "" {
			t.Fatal("no reference ask + buy needed must be rejected")
		}
	})

	t.Run("unreachable destination rejected", func(t *testing.T) {
		e := boardEntry("m7", "steel", 20, "far_station", "far_system", 3000, 0)
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel, false); reason == "" {
			t.Fatal("destination missing from dist map must be rejected")
		}
	})

	t.Run("negative net rejected", func(t *testing.T) {
		e := boardEntry("m8", "steel", 100, "sol_station", "sol", 500, 0) // cost 2000 > reward 500
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel, false); reason == "" {
			t.Fatal("negative-net mission must be rejected")
		}
	})

	t.Run("mission with warnings rejected", func(t *testing.T) {
		e := boardEntry("m9", "steel", 20, "sol_station", "sol", 3000, 0)
		e.Warnings = []string{"contraband: insurance voided"}
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel, false); reason == "" {
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
