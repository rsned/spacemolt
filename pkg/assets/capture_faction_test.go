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
