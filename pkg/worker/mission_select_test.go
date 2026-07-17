package worker

import (
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// boardEntry builds a deliver-shaped mission: deliver qty of item to destBase
// (in destSystem), paying reward credits, expiring in expiry ticks (0 = never).
func boardEntry(id, item string, qty int, destBase, destSystem string, reward, expiry int) serverapi.MissionBoardEntry {
	return serverapi.MissionBoardEntry{
		MissionID: id, Type: "delivery", Title: "Deliver " + item,
		ExpiresInTicks: expiry,
		Requirements: &serverapi.MissionRequirements{
			DeliverItemID: item, DeliverQuantity: qty, DeliverToBaseID: destBase,
		},
		Rewards:    &serverapi.MissionRewards{Credits: reward},
		Objectives: []serverapi.MissionObjective{{Type: "deliver", TargetBaseID: destBase, SystemID: destSystem}},
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
		c, reason := buildMissionCandidate(boardEntry("m1", "steel", 20, "sol_station", "sol", 3000, 0), dist, ask, noFuel)
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
		c, reason := buildMissionCandidate(e, dist, ask, noFuel)
		if reason != "" {
			t.Fatalf("rejected: %s", reason)
		}
		if c.BuyQty != 0 || c.ItemCost != 0 {
			t.Fatalf("provided cargo should zero the buy: %+v", c)
		}
	})

	t.Run("non-deliver mission rejected", func(t *testing.T) {
		e := boardEntry("m3", "", 0, "", "", 5000, 0)
		e.Requirements = &serverapi.MissionRequirements{KillCount: 3}
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel); reason == "" {
			t.Fatal("kill mission must be rejected")
		}
	})

	t.Run("module-gated mission rejected", func(t *testing.T) {
		e := boardEntry("m4", "steel", 20, "sol_station", "sol", 3000, 0)
		e.RequiredModules = []string{"smuggler_hold"}
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel); reason == "" {
			t.Fatal("module-gated mission must be rejected")
		}
	})

	t.Run("tight expiry rejected", func(t *testing.T) {
		e := boardEntry("m5", "steel", 20, "sol_station", "sol", 3000, 30)
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel); reason == "" {
			t.Fatal("30-tick expiry must be rejected (arbitrage-expiry lesson)")
		}
	})

	t.Run("unpriceable item rejected", func(t *testing.T) {
		e := boardEntry("m6", "unobtainium", 5, "sol_station", "sol", 3000, 0)
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel); reason == "" {
			t.Fatal("no reference ask + buy needed must be rejected")
		}
	})

	t.Run("unreachable destination rejected", func(t *testing.T) {
		e := boardEntry("m7", "steel", 20, "far_station", "far_system", 3000, 0)
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel); reason == "" {
			t.Fatal("destination missing from dist map must be rejected")
		}
	})

	t.Run("negative net rejected", func(t *testing.T) {
		e := boardEntry("m8", "steel", 100, "sol_station", "sol", 500, 0) // cost 2000 > reward 500
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel); reason == "" {
			t.Fatal("negative-net mission must be rejected")
		}
	})

	t.Run("mission with warnings rejected", func(t *testing.T) {
		e := boardEntry("m9", "steel", 20, "sol_station", "sol", 3000, 0)
		e.Warnings = []string{"contraband: insurance voided"}
		if _, reason := buildMissionCandidate(e, dist, ask, noFuel); reason == "" {
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

	t.Run("drops anchors beyond maxJumps", func(t *testing.T) {
		got := SelectMissionSet([]missionCandidate{mk("far", "sol", 9000, 9, 1, 1)}, credits, cargoFree, 5)
		if len(got) != 0 {
			t.Fatalf("9-jump destination with maxJumps=5 must be dropped: %+v", got)
		}
	})
}
