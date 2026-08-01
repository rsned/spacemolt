package assets

import (
	"context"
	"testing"
	"time"
)

// TestReplaceSkillsDropsVanishedRows pins the replacement invariant: a skill
// absent from a later capture must be DELETED, not left behind. Stale rows are
// phantom capability — the ledger would report an agent as eligible on the
// strength of data the server no longer reports.
func TestReplaceSkillsDropsVanishedRows(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	first := []SkillRow{{Skill: "smuggling", Level: 2, XP: 40}, {Skill: "trading", Level: 5, XP: 10}}
	if err := st.ReplaceSkills(ctx, "abc123", first, now); err != nil {
		t.Fatalf("first ReplaceSkills: %v", err)
	}
	second := []SkillRow{{Skill: "smuggling", Level: 3, XP: 0}}
	if err := st.ReplaceSkills(ctx, "abc123", second, now.Add(time.Hour)); err != nil {
		t.Fatalf("second ReplaceSkills: %v", err)
	}

	var n int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_skills WHERE player_id = ?`, "abc123").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("agent_skills rows = %d, want 1 (trading must be deleted)", n)
	}
	var level int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT level FROM agent_skills WHERE player_id = ? AND skill = ?`,
		"abc123", "smuggling").Scan(&level); err != nil {
		t.Fatalf("read smuggling: %v", err)
	}
	if level != 3 {
		t.Errorf("smuggling level = %d, want 3", level)
	}
}

// TestReplaceSkillsIsScopedToOneAgent pins that replacing one agent's skills
// leaves every other agent alone. A DELETE missing its player_id predicate
// would wipe the fleet on the next capture.
func TestReplaceSkillsIsScopedToOneAgent(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	if err := st.ReplaceSkills(ctx, "agent-a", []SkillRow{{Skill: "trading", Level: 4}}, now); err != nil {
		t.Fatalf("seed a: %v", err)
	}
	if err := st.ReplaceSkills(ctx, "agent-b", []SkillRow{{Skill: "mining", Level: 1}}, now); err != nil {
		t.Fatalf("seed b: %v", err)
	}
	if err := st.ReplaceSkills(ctx, "agent-b", []SkillRow{{Skill: "mining", Level: 2}}, now); err != nil {
		t.Fatalf("replace b: %v", err)
	}

	var n int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_skills WHERE player_id = ?`, "agent-a").Scan(&n); err != nil {
		t.Fatalf("count a: %v", err)
	}
	if n != 1 {
		t.Errorf("agent-a rows = %d, want 1 (untouched)", n)
	}
}

// TestReplaceStandingsKeepsBaseline pins that baseline survives the round trip.
// Baseline, not reputation, is the durable signal: reputation floats above it
// from missions and decays back toward it, so stronghold access is
// pirates.baseline >= 10 and gating on reputation would flip back later.
func TestReplaceStandingsKeepsBaseline(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	rows := []StandingRow{{Faction: "pirates", Reputation: 42, Baseline: 10, OutstandingBounty: 0}}
	if err := st.ReplaceStandings(ctx, "abc123", rows, now); err != nil {
		t.Fatalf("ReplaceStandings: %v", err)
	}

	var rep, base int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT reputation, baseline FROM agent_standings WHERE player_id = ? AND faction = ?`,
		"abc123", "pirates").Scan(&rep, &base); err != nil {
		t.Fatalf("read: %v", err)
	}
	if rep != 42 || base != 10 {
		t.Errorf("got reputation=%d baseline=%d, want 42/10", rep, base)
	}
}

// TestUpsertProfileRoundTrip pins the scalar columns and that a second capture
// updates rather than duplicating.
func TestUpsertProfileRoundTrip(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	p := Profile{
		PlayerID: "abc123", Username: "Tester", Empire: "haven", Credits: 15135,
		HomeBase: "grand_exchange_station", DockedAtBase: "treasure_cache_trading_post",
		CurrentSystem: "voss", CurrentPOI: "treasure_cache_trading_post",
		ActiveShipID: "ship-1", FactionID: "databot", FactionRank: "leader",
		Experience: 900, CapturedAt: now,
	}
	if err := st.UpsertProfile(ctx, p); err != nil {
		t.Fatalf("first: %v", err)
	}
	p.Credits = 22000
	p.CapturedAt = now.Add(time.Hour)
	if err := st.UpsertProfile(ctx, p); err != nil {
		t.Fatalf("second: %v", err)
	}

	var (
		n       int
		credits float64
		base    string
	)
	if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_profile`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("agent_profile rows = %d, want 1", n)
	}
	if err := st.DB().QueryRowContext(ctx,
		`SELECT credits, home_base FROM agent_profile WHERE player_id = ?`, "abc123").
		Scan(&credits, &base); err != nil {
		t.Fatalf("read: %v", err)
	}
	if credits != 22000 || base != "grand_exchange_station" {
		t.Errorf("got credits=%v home_base=%q, want 22000/grand_exchange_station", credits, base)
	}
}
