package knowledge

import (
	"context"
	"testing"
	"time"
)

// A ship_captured push is the only record of the new v0.572.0 loss mode: a
// hull taken intact rather than wrecked. Every one we witness — ours lost,
// ours taken, or a bystander's — is a row keyed on the boarding operation.
func TestRecordShipCapturesWritesRow(t *testing.T) {
	kb := newTestKB(t)
	at := time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)
	err := kb.RecordShipCaptures(context.Background(), []ShipCapture{{
		BattleID: "b1", Tick: 1800000, BoardingOperationID: "op1",
		CaptorID: "molten", CaptorUsername: "MoltenOne",
		FormerOwnerID: "h7", FormerOwnerUsername: "hauler-7",
		ShipID: "ship7", ShipClass: "congregation",
		ObserverID: "h7", SeenAt: at,
	}})
	if err != nil {
		t.Fatalf("RecordShipCaptures: %v", err)
	}
	var captor, owner, class, observer, seen string
	var tick int64
	if err := kb.db.QueryRow(`SELECT captor_id, former_owner_id, ship_class, observer_id, tick, seen_at_utc
		FROM ship_captures WHERE boarding_operation_id='op1'`).
		Scan(&captor, &owner, &class, &observer, &tick, &seen); err != nil {
		t.Fatalf("query: %v", err)
	}
	if captor != "molten" || owner != "h7" || class != "congregation" || observer != "h7" ||
		tick != 1800000 || seen != "2026-08-30T10:00:00Z" {
		t.Errorf("row = %q %q %q %q %d %q", captor, owner, class, observer, tick, seen)
	}
}

// Several of our agents in the same battle all receive the push; the
// operation is one event however many saw it.
func TestRecordShipCapturesDedupsPerOperation(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	c := ShipCapture{BattleID: "b1", Tick: 5, BoardingOperationID: "op1", ShipID: "s", ObserverID: "a", SeenAt: time.Now()}
	if err := kb.RecordShipCaptures(ctx, []ShipCapture{c}); err != nil {
		t.Fatal(err)
	}
	c.ObserverID = "b"
	if err := kb.RecordShipCaptures(ctx, []ShipCapture{c}); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, kb, "ship_captures"); n != 1 {
		t.Errorf("ship_captures rows = %d, want 1", n)
	}
}

// Intact prizes show up in get_nearby like players do; each read is one
// timeline row so a prize's drift toward its destination can be replayed.
func TestRecordPrizeSightingsWritesEventRow(t *testing.T) {
	kb := newTestKB(t)
	at := time.Date(2026, 8, 30, 10, 5, 0, 0, time.UTC)
	err := kb.RecordPrizeSightings(context.Background(), []SeenPrize{{
		PrizeID: "pz1", ShipID: "ship7", ShipClass: "congregation", ShipName: "Old Faithful",
		ActorID: "molten", Status: "in_transit", WaitReason: "",
		Hull: 100, MaxHull: 400, Shield: 0, MaxShield: 50, InCombat: false,
		SystemID: "zaniah", POIID: "zaniah_gate", Source: "get_nearby",
		Tick: 1800010, ObserverID: "mb_zaniah", SeenAt: at,
	}})
	if err != nil {
		t.Fatalf("RecordPrizeSightings: %v", err)
	}
	var actor, status, system, poi, observer string
	var hull, tick int64
	if err := kb.db.QueryRow(`SELECT actor_id, status, system_id, poi_id, observer_id, hull, tick
		FROM seen_prize_events WHERE prize_id='pz1'`).
		Scan(&actor, &status, &system, &poi, &observer, &hull, &tick); err != nil {
		t.Fatalf("query: %v", err)
	}
	if actor != "molten" || status != "in_transit" || system != "zaniah" || poi != "zaniah_gate" ||
		observer != "mb_zaniah" || hull != 100 || tick != 1800010 {
		t.Errorf("row = %q %q %q %q %q %d %d", actor, status, system, poi, observer, hull, tick)
	}
}

// The same observer reading the same prize at the same tick (get_nearby and
// get_system_agents back to back) is one observation.
func TestRecordPrizeSightingsDedupsSameObservation(t *testing.T) {
	kb := newTestKB(t)
	ctx := context.Background()
	p := SeenPrize{PrizeID: "pz1", SystemID: "s", Tick: 7, ObserverID: "a", Source: "get_nearby", SeenAt: time.Now()}
	for range 2 {
		if err := kb.RecordPrizeSightings(ctx, []SeenPrize{p}); err != nil {
			t.Fatal(err)
		}
	}
	if n := countRows(t, kb, "seen_prize_events"); n != 1 {
		t.Errorf("seen_prize_events rows = %d, want 1", n)
	}
	// A tick-less observation cannot be deduped and is kept as its own row.
	p.Tick = 0
	if err := kb.RecordPrizeSightings(ctx, []SeenPrize{p}); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, kb, "seen_prize_events"); n != 2 {
		t.Errorf("seen_prize_events rows after tick-less = %d, want 2", n)
	}
}
