package assets

import (
	"context"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// factionFake serves canned faction_info and view_faction_storage frames,
// mimicking the client's raw-JSON cache: the frame for the most recent
// view_faction_storage call is what GetRawJSON("faction_storage") returns.
// Modeled on storageFake in capture_storage_test.go.
type factionFake struct {
	game.GameClient
	state            *game.State
	raw              map[string][]byte // "faction_info" -> raw body
	frames           map[string][]byte // station id ("" = current dock) -> raw frame
	lastFrame        []byte
	factionInfoCalls int
}

func (f *factionFake) GetStatus(context.Context) error { return nil }
func (f *factionFake) GetState() *game.State           { return f.state }

func (f *factionFake) FactionInfo(context.Context) error {
	f.factionInfoCalls++

	return nil
}

func (f *factionFake) ViewFactionStorage(ctx context.Context) error {
	return f.serve("")
}

func (f *factionFake) ViewFactionStorageAt(ctx context.Context, stationID string) error {
	return f.serve(stationID)
}

func (f *factionFake) serve(id string) error {
	f.lastFrame = f.frames[id]

	return nil
}

func (f *factionFake) GetRawJSON(key string) []byte {
	switch key {
	case "faction_info":
		return f.raw["faction_info"]
	case "faction_storage":
		return f.lastFrame
	default:
		return nil
	}
}

func newFactionFake(playerID, factionID, dockedAt string) *factionFake {
	return &factionFake{
		state: &game.State{Player: game.Player{
			ID: playerID, FactionID: factionID, DockedAtBase: dockedAt, HomeBase: dockedAt,
		}},
		raw:    map[string][]byte{},
		frames: map[string][]byte{},
	}
}

// TestIsFactionCaptorPicksLowestPlayerID pins the coordination-free election.
// Every member evaluates the same rule against the same data and exactly one
// concludes it is the captor -- no claim file, no lock.
func TestIsFactionCaptorPicksLowestPlayerID(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	for _, id := range []string{"ccc", "aaa", "bbb"} {
		if err := st.UpsertProfile(ctx, Profile{
			PlayerID: id, FactionID: "fac1", CapturedAt: now,
		}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	for _, tc := range []struct {
		playerID string
		want     bool
	}{{"aaa", true}, {"bbb", false}, {"ccc", false}} {
		got, err := st.IsFactionCaptor(ctx, tc.playerID, "fac1", now)
		if err != nil {
			t.Fatalf("IsFactionCaptor(%s): %v", tc.playerID, err)
		}
		if got != tc.want {
			t.Errorf("IsFactionCaptor(%s) = %v, want %v", tc.playerID, got, tc.want)
		}
	}
}

// TestIsFactionCaptorIgnoresStaleMembers pins that a member whose profile has
// gone stale cannot hold the designation hostage -- otherwise a dead worker
// with the lowest id would silently stop all faction capture.
func TestIsFactionCaptorIgnoresStaleMembers(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	if err := st.UpsertProfile(ctx, Profile{
		PlayerID: "aaa", FactionID: "fac1", CapturedAt: now.Add(-72 * time.Hour),
	}); err != nil {
		t.Fatalf("seed stale: %v", err)
	}
	if err := st.UpsertProfile(ctx, Profile{
		PlayerID: "bbb", FactionID: "fac1", CapturedAt: now,
	}); err != nil {
		t.Fatalf("seed fresh: %v", err)
	}

	got, err := st.IsFactionCaptor(ctx, "bbb", "fac1", now)
	if err != nil {
		t.Fatalf("IsFactionCaptor: %v", err)
	}
	if !got {
		t.Error("a stale lowest-id member must not block a fresh member from capturing")
	}
}

// TestIsFactionCaptorBootstrapsWhenLedgerIsCold pins that an empty ledger does
// not deadlock: with nothing to compare against, the caller captures.
func TestIsFactionCaptorBootstrapsWhenLedgerIsCold(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	got, err := st.IsFactionCaptor(ctx, "aaa", "fac1", now)
	if err != nil {
		t.Fatalf("IsFactionCaptor: %v", err)
	}
	if !got {
		t.Error("a cold ledger must bootstrap by capturing, not by waiting forever")
	}
}

// TestCaptureFactionKeepsSeedBaseAbsentFromHint pins the faction-side version of
// the seed-base data-loss path found in Task 7's review. A faction base holding
// only a FUEL BUNKER and no items is absent from an item-counting hint, so
// without the seed-base union its bunker row is deleted while the reserve sits
// in the seed response.
func TestCaptureFactionKeepsSeedBaseAbsentFromHint(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	if err := st.ReplaceFactionStorage(ctx, "fac1", []FactionStorageBase{
		{BaseID: "base_c", FuelReserve: 4200, FuelCapacity: 50000},
	}, now); err != nil {
		t.Fatalf("seed: %v", err)
	}

	f := newFactionFake("p1", "fac1", "base_c")
	f.raw["faction_info"] = []byte(`{"id":"fac1","name":"Iron Compact","treasury":1000}`)
	f.frames[""] = []byte(`{"faction_id":"fac1","base_id":"base_c","items":[],` +
		`"faction_fuel_reserve":4200,"faction_fuel_capacity":50000,` +
		`"hint":"9 items in storage at base_a"}`)
	f.frames["base_a"] = []byte(`{"faction_id":"fac1","base_id":"base_a",` +
		`"items":[{"item_id":"iron_ore","quantity":9}]}`)

	if err := CaptureFaction(ctx, f, st, "agent-x", now.Add(time.Hour)); err != nil {
		t.Fatalf("CaptureFaction: %v", err)
	}

	bases, _, err := st.LoadFactionStorage(ctx, "fac1", nil)
	if err != nil {
		t.Fatalf("LoadFactionStorage: %v", err)
	}
	for _, b := range bases {
		if b.BaseID == "base_c" {
			if b.FuelReserve != 4200 {
				t.Errorf("base_c fuel_reserve = %d, want 4200", b.FuelReserve)
			}

			return
		}
	}
	t.Fatalf("base_c was deleted despite its 4200-unit fuel bunker arriving in the seed response; bases = %v", bases)
}

// TestCaptureFactionSkipsNonMembers pins that an agent with no faction does
// nothing at all -- most of the fleet is unaffiliated and must not spend calls.
func TestCaptureFactionSkipsNonMembers(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	f := newFactionFake("p1", "", "") // no faction
	if err := CaptureFaction(ctx, f, st, "agent-x", now); err != nil {
		t.Fatalf("CaptureFaction: %v", err)
	}
	if f.factionInfoCalls != 0 {
		t.Errorf("faction_info called %d times for a non-member, want 0", f.factionInfoCalls)
	}
}

// An UNDOCKED captor seeds the storage sweep from its own HomeBase, and that is
// unrelated to where the faction keeps things. CRFT's elected captor is
// craftsman-8, homed at crimson_war_citadel -- a Crimson empire station where
// CRFT has no storage facility at all -- and it spends its days undocked at an
// ice belt. So:
//
//	docked_at_base is "" -> seed = HomeBase = crimson_war_citadel
//	view_faction_storage(crimson_war_citadel) -> no faction storage there
//	fetch fails -> return nil, whole capture abandoned SILENTLY
//
// Observed live: craftsman-1's daily capture_faction had been firing since at
// least 10-17 00:00 and writing nothing, while voss_redoubt_station's faction
// pocket sat frozen at 2026-10-13. Every holdings figure read off the personal
// table instead was wrong.
//
// station_id works undocked, so a failed seed must fall back to a base the
// FACTION is known to hold rather than giving up.
func TestCaptureFactionSeedsFromAKnownFactionBaseWhenHomeHasNone(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

	// What we already know: the faction holds storage at two bases.
	if err := st.ReplaceFactionStorage(ctx, "fac1", []FactionStorageBase{
		{BaseID: "voss_redoubt_station"},
		{BaseID: "grand_exchange_station"},
	}, now); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Undocked captor homed where the faction has nothing.
	f := newFactionFake("p1", "fac1", "")
	f.state.Player.HomeBase = "crimson_war_citadel"
	f.raw["faction_info"] = []byte(`{"id":"fac1","name":"Crafting Collective","treasury":8060546}`)
	// crimson_war_citadel deliberately has NO frame: the server refuses with
	// "Your faction does not have a storage facility at this station".
	f.frames["voss_redoubt_station"] = []byte(`{"faction_id":"fac1","base_id":"voss_redoubt_station",` +
		`"items":[{"item_id":"pharmaceutical_culture","quantity":815}],` +
		`"hint":"149653 items in faction storage at grand_exchange_station, voss_redoubt_station"}`)
	f.frames["grand_exchange_station"] = []byte(`{"faction_id":"fac1","base_id":"grand_exchange_station",` +
		`"items":[{"item_id":"energy_crystal","quantity":1408}]}`)

	if err := CaptureFaction(ctx, f, st, "craftsman-8", now.Add(time.Hour)); err != nil {
		t.Fatalf("CaptureFaction: %v", err)
	}

	bases, _, err := st.LoadFactionStorage(ctx, "fac1", nil)
	if err != nil {
		t.Fatalf("LoadFactionStorage: %v", err)
	}
	got := map[string]int{}
	for _, b := range bases {
		for _, it := range b.Items {
			got[b.BaseID+"/"+it.ItemID] = int(it.Quantity)
		}
	}
	if got["voss_redoubt_station/pharmaceutical_culture"] != 815 {
		t.Errorf("voss pharma = %d, want 815 (the seed fell back to a known faction base)",
			got["voss_redoubt_station/pharmaceutical_culture"])
	}
	// The sweep must still cover the OTHER base named in the hint, so fixing
	// Voss does not cost grand_exchange.
	if got["grand_exchange_station/energy_crystal"] != 1408 {
		t.Errorf("grand_exchange crystal = %d, want 1408", got["grand_exchange_station/energy_crystal"])
	}
}

// A captor with no faction storage anywhere and no known bases still must not
// error -- a cold ledger plus a homeless agent is a no-op, not a failure.
func TestCaptureFactionNoKnownBasesIsAQuietNoOp(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

	f := newFactionFake("p1", "fac1", "")
	f.state.Player.HomeBase = "crimson_war_citadel"
	f.raw["faction_info"] = []byte(`{"id":"fac1","name":"Crafting Collective","treasury":1}`)
	// No frames at all: every station refuses.

	if err := CaptureFaction(ctx, f, st, "craftsman-8", now); err != nil {
		t.Errorf("want a quiet no-op, got %v", err)
	}
}
