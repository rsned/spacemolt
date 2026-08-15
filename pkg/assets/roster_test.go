package assets

import (
	"context"
	"testing"
	"time"
)

// rosterFixture seeds two agents: one fully captured (profile, active hull,
// capabilities, skills, standings, storage) and one identity-only — the
// roster must show both, because a capture gap that hides an agent is the
// exact failure the roster exists to surface.
func rosterFixture(t *testing.T) *Store {
	t.Helper()
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

	if err := st.UpsertIdentity(ctx, Identity{PlayerID: "p1", AgentID: "trader-1", Username: "Tessa"}, now); err != nil {
		t.Fatalf("identity p1: %v", err)
	}
	if err := st.UpsertIdentity(ctx, Identity{PlayerID: "p2", AgentID: "ghost-1", Username: "Gabe"}, now); err != nil {
		t.Fatalf("identity p2: %v", err)
	}
	if err := st.UpsertProfile(ctx, Profile{
		PlayerID: "p1", Username: "Tessa", Empire: "solarian", Credits: 12500,
		CurrentSystem: "sol", CurrentPOI: "sol_central", DockedAtBase: "sol_central",
		FactionID: "smt", FactionRank: "boss", Experience: 4200, CapturedAt: now,
	}); err != nil {
		t.Fatalf("profile: %v", err)
	}
	if err := st.ReplaceHulls(ctx, "p1", []Hull{
		{ShipID: "s-active", ClassID: "analysis", ClassName: "Analysis", IsActive: true,
			HullCurrent: 55, HullMax: 60, FuelCurrent: 91, FuelMax: 120, CargoUsed: 12},
		{ShipID: "s-spare", ClassID: "appraisal", ClassName: "Appraisal",
			HullCurrent: 10, HullMax: 40, Location: "sol_central"},
	}, now); err != nil {
		t.Fatalf("hulls: %v", err)
	}
	if err := st.ReplaceCapabilities(ctx, "p1", []Capability{
		{Capability: "haul", Eligible: true},
		{Capability: "stronghold_access", Eligible: false, BlockingReason: "pirate baseline 4 < 10"},
	}, now); err != nil {
		t.Fatalf("capabilities: %v", err)
	}
	if err := st.ReplaceSkills(ctx, "p1", []SkillRow{
		{Skill: "trading", Level: 7, XP: 8100}, {Skill: "piloting", Level: 3, XP: 900},
	}, now); err != nil {
		t.Fatalf("skills: %v", err)
	}
	if err := st.ReplaceStandings(ctx, "p1", []StandingRow{
		{Faction: "pirate_krynn", Reputation: 6, Baseline: 4},
	}, now); err != nil {
		t.Fatalf("standings: %v", err)
	}
	if err := st.ReplaceStorage(ctx, "p1", []StorageBase{
		{BaseID: "sol_central", Credits: 900, Items: []StorageItem{
			{ItemID: "steel_plate", Name: "Steel Plate", Quantity: 40},
			{ItemID: "power_cell", Name: "Power Cell", Quantity: 2},
		}},
	}, now); err != nil {
		t.Fatalf("storage: %v", err)
	}

	return st
}

func TestRosterIncludesIdentityOnlyAgents(t *testing.T) {
	st := rosterFixture(t)
	rows, err := Roster(context.Background(), st.DB())
	if err != nil {
		t.Fatalf("Roster: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("roster rows = %d, want 2", len(rows))
	}
	by := map[string]RosterRow{}
	for _, r := range rows {
		by[r.AgentID] = r
	}

	full := by["trader-1"]
	if full.Credits != 12500 || full.Empire != "solarian" || full.CapturedAt == "" {
		t.Errorf("trader-1 profile not attached: %+v", full)
	}
	if full.Ship == nil || full.Ship.ShipID != "s-active" || full.Ship.FuelMax != 120 {
		t.Errorf("trader-1 active hull wrong: %+v", full.Ship)
	}
	if c := full.Capabilities["stronghold_access"]; c.Eligible || c.Reason == "" {
		t.Errorf("stronghold_access must carry its blocking reason: %+v", c)
	}
	if !full.Capabilities["haul"].Eligible {
		t.Errorf("haul must be eligible")
	}

	ghost := by["ghost-1"]
	if ghost.PlayerID != "p2" || ghost.CapturedAt != "" || ghost.Ship != nil {
		t.Errorf("identity-only agent must appear with empty profile: %+v", ghost)
	}
}

func TestSheetForLoadsAllSections(t *testing.T) {
	st := rosterFixture(t)
	ctx := context.Background()

	sheet, ok, err := SheetFor(ctx, st.DB(), "trader-1")
	if err != nil || !ok {
		t.Fatalf("SheetFor = ok=%v err=%v", ok, err)
	}
	if len(sheet.Skills) != 2 || sheet.Skills[0].Skill != "trading" {
		t.Errorf("skills wrong (want trading first, level-desc): %+v", sheet.Skills)
	}
	if len(sheet.Standings) != 1 || sheet.Standings[0].Faction != "pirate_krynn" {
		t.Errorf("standings wrong: %+v", sheet.Standings)
	}
	if len(sheet.Hulls) != 2 || !sheet.Hulls[0].IsActive || sheet.Hulls[1].ShipID != "s-spare" {
		t.Errorf("hulls wrong (active first): %+v", sheet.Hulls)
	}
	if len(sheet.Storage) != 1 || sheet.Storage[0].Items != 2 || sheet.Storage[0].Units != 42 {
		t.Errorf("storage summary wrong: %+v", sheet.Storage)
	}

	if _, ok, err := SheetFor(ctx, st.DB(), "no-such-agent"); ok || err != nil {
		t.Errorf("unknown agent: ok=%v err=%v, want false/nil", ok, err)
	}
	// Player-id fallback keeps ledger-only identities reachable.
	if _, ok, err := SheetFor(ctx, st.DB(), "p2"); !ok || err != nil {
		t.Errorf("player-id fallback: ok=%v err=%v, want true/nil", ok, err)
	}
}
