package assets

import (
	"context"
	"testing"
	"time"
)

// TestRecordStatsAppendsOnlyOnChange is the whole design in one test. The
// capture runs hourly for 160 agents; if it wrote a row every pass the table
// would grow by ~3,800 rows a day to say nothing happened. It must write only
// when a counter moves — and it must ALWAYS write when one does, because that
// row is the only evidence a death occurred.
func TestRecordStatsAppendsOnlyOnChange(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)

	base := Stats{ShipsLost: 1, DeathsByPirate: 1}
	wrote, err := st.RecordStats(ctx, "p1", base, t0)
	if err != nil {
		t.Fatal(err)
	}
	if !wrote {
		t.Fatal("first capture wrote nothing; a never-seen agent must be recorded")
	}

	// Two unchanged captures an hour apart.
	for i, at := range []time.Time{t0.Add(time.Hour), t0.Add(2 * time.Hour)} {
		wrote, err = st.RecordStats(ctx, "p1", base, at)
		if err != nil {
			t.Fatal(err)
		}
		if wrote {
			t.Errorf("unchanged capture %d wrote a row", i)
		}
	}

	// A death by wildlife lands.
	died := base
	died.DeathsByWildlife = 1
	died.ShipsLost = 2
	wrote, err = st.RecordStats(ctx, "p1", died, t0.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !wrote {
		t.Fatal("a changed counter was not recorded — the death would be invisible")
	}

	hist, err := st.StatsHistory(ctx, "p1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 {
		t.Fatalf("want 2 rows (first capture + the change), got %d", len(hist))
	}
	// Newest first: the death, then the baseline. The pair brackets when it
	// happened — after 09:00 and by 12:00.
	if hist[0].DeathsByWildlife != 1 || hist[0].CapturedAt != "2026-08-17T12:00:00Z" {
		t.Errorf("newest row = %+v, want the wildlife death at 12:00", hist[0])
	}
	if hist[1].DeathsByWildlife != 0 {
		t.Errorf("older row = %+v, want the pre-death baseline", hist[1])
	}
}

// TestLatestStatsNilWhenNeverCaptured keeps "never observed" distinguishable
// from "has never died". A zeroed row would assert the agent is unscathed.
func TestLatestStatsNilWhenNeverCaptured(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	got, err := st.LatestStats(ctx, "nobody")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("want nil for an uncaptured agent, got %+v", got)
	}
}

// TestStatsDeathsIsNotShipsLost pins the live discrepancy so nobody later
// "fixes" one into the other: explorer-7 really does report two deaths against
// one ship lost.
func TestStatsDeathsIsNotShipsLost(t *testing.T) {
	explorer7 := Stats{ShipsLost: 1, DeathsByPirate: 1, DeathsByPlayer: 1}

	if explorer7.Deaths() != 2 {
		t.Errorf("Deaths() = %d, want 2", explorer7.Deaths())
	}
	if explorer7.Deaths() == explorer7.ShipsLost {
		t.Error("Deaths() and ShipsLost must stay independent facts")
	}
}

// TestFleetDeathsRanksByDeaths covers the fleet-wide readout: latest row per
// agent, most-died first, with the agent id joined on where identity is known.
func TestFleetDeathsRanksByDeaths(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)

	if err := st.UpsertIdentity(ctx, Identity{PlayerID: "p1", AgentID: "salvager-7", Username: "Gadget"}, t0); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertIdentity(ctx, Identity{PlayerID: "p2", AgentID: "explorer-7", Username: "Nova"}, t0); err != nil {
		t.Fatal(err)
	}

	if _, err := st.RecordStats(ctx, "p1", Stats{ShipsLost: 1, DeathsByPirate: 1}, t0); err != nil {
		t.Fatal(err)
	}
	// A later row for p1 must supersede the earlier one, not add to it.
	if _, err := st.RecordStats(ctx, "p1", Stats{ShipsLost: 2, DeathsByPirate: 1, DeathsByWildlife: 2}, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RecordStats(ctx, "p2", Stats{ShipsLost: 1, DeathsByPlayer: 1}, t0); err != nil {
		t.Fatal(err)
	}

	got, err := st.FleetDeaths(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want one row per agent, got %d: %+v", len(got), got)
	}
	if got[0].AgentID != "salvager-7" || got[0].Stats.Deaths() != 3 {
		t.Errorf("first = %+v, want salvager-7 with 3 deaths (latest row only)", got[0])
	}
	if got[1].AgentID != "explorer-7" || got[1].Stats.Deaths() != 1 {
		t.Errorf("second = %+v, want explorer-7 with 1 death", got[1])
	}
}
