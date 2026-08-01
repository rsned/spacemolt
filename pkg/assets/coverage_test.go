package assets

import (
	"context"
	"testing"
	"time"
)

// TestCoverageCountsStalePerSource pins the anti-rot surface. Every previous
// unsupervised capture job in this codebase died silently — daily-summary for
// 25 days, market-prune until the DB hit 62GB, the arbitrage scanner mimicking
// "no opportunities". Coverage is how this one gets noticed instead.
func TestCoverageCountsStalePerSource(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-10 * time.Minute)
	old := now.Add(-72 * time.Hour)

	if err := st.UpsertProfile(ctx, Profile{PlayerID: "fresh-1", CapturedAt: fresh}); err != nil {
		t.Fatalf("fresh profile: %v", err)
	}
	if err := st.UpsertProfile(ctx, Profile{PlayerID: "stale-1", CapturedAt: old}); err != nil {
		t.Fatalf("stale profile: %v", err)
	}
	if err := st.UpsertCarrier(ctx, "fresh-1", Carrier{Tier: "licensed"}, fresh); err != nil {
		t.Fatalf("carrier: %v", err)
	}

	rows, err := Coverage(ctx, st.DB(), now, 24*time.Hour)
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	by := map[string]CoverageRow{}
	for _, r := range rows {
		by[r.Source] = r
	}

	if got := by["agent_profile"]; got.Agents != 2 || got.Stale != 1 {
		t.Errorf("agent_profile = %d agents / %d stale, want 2 / 1", got.Agents, got.Stale)
	}
	if got := by["agent_carrier"]; got.Agents != 1 || got.Stale != 0 {
		t.Errorf("agent_carrier = %d agents / %d stale, want 1 / 0", got.Agents, got.Stale)
	}
}

// TestCoverageOnEmptyDBIsNotAnError pins that a brand-new ledger reports zeroes
// rather than failing the dashboard that reads it. It asserts the EXACT set of
// sources, not just a non-zero length: a query that silently dropped one
// source would still pass a bare len(rows) != 0 check, which is precisely the
// "an absent row reads as no problem here" failure this task exists to catch.
//
// The expected source list is hardcoded here rather than read from the
// package's own coverageSources var: comparing against that var would be
// self-referential and would not catch a bug that shrinks coverageSources
// itself, since the "expected" side would shrink right along with it.
func TestCoverageOnEmptyDBIsNotAnError(t *testing.T) {
	wantSources := []string{"agent_profile", "agent_carrier", "agent_hulls", "agent_skills"}

	st := openTestStore(t)
	rows, err := Coverage(context.Background(), st.DB(),
		time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC), 24*time.Hour)
	if err != nil {
		t.Fatalf("Coverage on empty DB: %v", err)
	}
	if len(rows) != len(wantSources) {
		t.Fatalf("got %d rows, want %d (one per source): %+v", len(rows), len(wantSources), rows)
	}
	got := map[string]bool{}
	for _, r := range rows {
		got[r.Source] = true
		if r.Agents != 0 || r.Stale != 0 {
			t.Errorf("%s: got %d agents / %d stale on an empty DB", r.Source, r.Agents, r.Stale)
		}
	}
	for _, want := range wantSources {
		if !got[want] {
			t.Errorf("missing source %q in Coverage output", want)
		}
	}
}

// TestCoverageCountsDistinctAgentsNotRows pins two things at once, using the
// only fixture that can distinguish them: agent_skills and agent_hulls carry
// MANY rows per agent (unlike agent_profile/agent_carrier, keyed one-row-per-
// agent, where COUNT(*) and COUNT(DISTINCT player_id) are indistinguishable).
//
//  1. Agents must count distinct agents, not rows: an agent with several
//     skills/hulls is still one agent.
//  2. Stale must count distinct STALE agents, not stale ROWS: an agent whose
//     captured_at is old contributes one to Stale even though ReplaceHulls/
//     ReplaceSkills wrote several rows at that same old timestamp. Getting
//     this wrong overstates the alarm by however many rows the stale agent
//     has — exactly the failure that makes an operator learn to ignore it.
func TestCoverageCountsDistinctAgentsNotRows(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-10 * time.Minute)
	old := now.Add(-72 * time.Hour)

	// multi-1 is one agent with three skills and three hulls, all captured
	// stale.
	if err := st.ReplaceSkills(ctx, "multi-1", []SkillRow{
		{Skill: "mining", Level: 4},
		{Skill: "piloting", Level: 2},
		{Skill: "trading", Level: 7},
	}, old); err != nil {
		t.Fatalf("stale skills: %v", err)
	}
	if err := st.ReplaceHulls(ctx, "multi-1", []Hull{
		{ShipID: "ship-a"},
		{ShipID: "ship-b"},
		{ShipID: "ship-c"},
	}, old); err != nil {
		t.Fatalf("stale hulls: %v", err)
	}

	// single-1 is a second agent, one row each, captured fresh.
	if err := st.ReplaceSkills(ctx, "single-1", []SkillRow{
		{Skill: "mining", Level: 1},
	}, fresh); err != nil {
		t.Fatalf("fresh skills: %v", err)
	}
	if err := st.ReplaceHulls(ctx, "single-1", []Hull{
		{ShipID: "ship-x"},
	}, fresh); err != nil {
		t.Fatalf("fresh hulls: %v", err)
	}

	rows, err := Coverage(ctx, st.DB(), now, 24*time.Hour)
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	by := map[string]CoverageRow{}
	for _, r := range rows {
		by[r.Source] = r
	}

	if got := by["agent_skills"]; got.Agents != 2 || got.Stale != 1 {
		t.Errorf("agent_skills = %d agents (want 2, i.e. distinct, not the 4 rows written) / "+
			"%d stale (want 1 agent, not the 3 stale rows from multi-1)", got.Agents, got.Stale)
	}
	if got := by["agent_hulls"]; got.Agents != 2 || got.Stale != 1 {
		t.Errorf("agent_hulls = %d agents (want 2, i.e. distinct, not the 4 rows written) / "+
			"%d stale (want 1 agent, not the 3 stale rows from multi-1)", got.Agents, got.Stale)
	}
}
