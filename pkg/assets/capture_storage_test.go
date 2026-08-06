package assets

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// storageFake serves canned view_storage frames per station id, mimicking the
// client's raw-JSON cache: the frame for the most recent call is what
// GetRawJSON("storage") returns.
type storageFake struct {
	game.GameClient
	state     *game.State
	frames    map[string][]byte // station id ("" = current dock) -> raw frame
	failFor   map[string]error  // station id -> error to return
	lastRaw   []byte
	calls     []string
	statusErr error
}

func (f *storageFake) GetStatus(context.Context) error { return f.statusErr }
func (f *storageFake) GetState() *game.State           { return f.state }

func (f *storageFake) ViewStorage(ctx context.Context) error {
	return f.serve("")
}

func (f *storageFake) ViewStorageAt(ctx context.Context, stationID string) error {
	return f.serve(stationID)
}

func (f *storageFake) serve(id string) error {
	f.calls = append(f.calls, id)
	if err, ok := f.failFor[id]; ok {
		f.lastRaw = nil

		return err
	}
	f.lastRaw = f.frames[id]

	return nil
}

func (f *storageFake) GetRawJSON(key string) []byte {
	if key != "storage" {
		return nil
	}

	return f.lastRaw
}

func newStorageFake(playerID, dockedAt string) *storageFake {
	return &storageFake{
		state: &game.State{Player: game.Player{
			ID: playerID, DockedAtBase: dockedAt, HomeBase: dockedAt,
		}},
		frames:  map[string][]byte{},
		failFor: map[string]error{},
	}
}

// TestCaptureStorageSweepsHintBases pins the core flow: one seed call yields
// the agent-global hint, and every base it names gets swept.
func TestCaptureStorageSweepsHintBases(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	f := newStorageFake("p1", "base_a")
	f.frames[""] = []byte(`{"base_id":"base_a","hint":"15 items in storage at base_a, base_b",` +
		`"items":[{"item_id":"x","quantity":5}]}`)
	f.frames["base_b"] = []byte(`{"base_id":"base_b","hint":"15 items in storage at base_a, base_b",` +
		`"items":[{"item_id":"y","quantity":10}]}`)

	if err := CaptureStorage(ctx, f, st, "agent-x", now); err != nil {
		t.Fatalf("CaptureStorage: %v", err)
	}

	bases, _, err := st.LoadStorage(ctx, "p1", nil)
	if err != nil {
		t.Fatalf("LoadStorage: %v", err)
	}
	if len(bases) != 2 {
		t.Fatalf("bases = %d, want 2", len(bases))
	}
	// The seed response must be reused, not re-fetched: base_a is already in hand.
	for _, c := range f.calls {
		if c == "base_a" {
			t.Errorf("base_a was re-fetched; the seed response should be reused")
		}
	}
}

// TestCaptureStorageUnparseableHintDeletesNothing pins the most dangerous
// failure. An unparseable hint must fall back to the known base set and skip
// the base-deletion invariant -- an empty sweep is indistinguishable from "sold
// everything" and would erase real holdings.
func TestCaptureStorageUnparseableHintDeletesNothing(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	seed := []StorageBase{{BaseID: "base_a", Items: []StorageItem{{ItemID: "x", Quantity: 5}}}}
	if err := st.ReplaceStorage(ctx, "p1", seed, now); err != nil {
		t.Fatalf("seed: %v", err)
	}

	f := newStorageFake("p1", "base_a")
	f.frames[""] = []byte(`{"base_id":"base_a","hint":"storage subsystem offline","items":[]}`)

	if err := CaptureStorage(ctx, f, st, "agent-x", now.Add(time.Hour)); err != nil {
		t.Fatalf("CaptureStorage: %v", err)
	}

	bases, _, err := st.LoadStorage(ctx, "p1", nil)
	if err != nil {
		t.Fatalf("LoadStorage: %v", err)
	}
	if len(bases) != 1 {
		t.Fatalf("bases = %d, want 1 -- an unparseable hint must never delete", len(bases))
	}
}

// TestCaptureStorageEmptySentinelClears pins the other side of the same coin:
// the server's explicit "holds nothing" sentinel is authoritative and DOES
// clear, because zero storage is genuinely reachable.
func TestCaptureStorageEmptySentinelClears(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	if err := st.ReplaceStorage(ctx, "p1", []StorageBase{
		{BaseID: "base_a", Items: []StorageItem{{ItemID: "x", Quantity: 5}}},
	}, now); err != nil {
		t.Fatalf("seed: %v", err)
	}

	f := newStorageFake("p1", "base_a")
	f.frames[""] = []byte(`{"base_id":"base_a","hint":"No items in storage at any station.","items":[]}`)

	if err := CaptureStorage(ctx, f, st, "agent-x", now.Add(time.Hour)); err != nil {
		t.Fatalf("CaptureStorage: %v", err)
	}

	bases, _, err := st.LoadStorage(ctx, "p1", nil)
	if err != nil {
		t.Fatalf("LoadStorage: %v", err)
	}
	if len(bases) != 0 {
		t.Errorf("bases = %d, want 0 -- the empty sentinel is authoritative", len(bases))
	}
}

// TestCaptureStorageKeepsBasesThatFailedMidSweep pins per-base degradation: one
// failed ViewStorageAt must not delete that base's holdings.
func TestCaptureStorageKeepsBasesThatFailedMidSweep(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	if err := st.ReplaceStorage(ctx, "p1", []StorageBase{
		{BaseID: "base_b", Items: []StorageItem{{ItemID: "y", Quantity: 10}}},
	}, now); err != nil {
		t.Fatalf("seed: %v", err)
	}

	f := newStorageFake("p1", "base_a")
	f.frames[""] = []byte(`{"base_id":"base_a","hint":"15 items in storage at base_a, base_b",` +
		`"items":[{"item_id":"x","quantity":5}]}`)
	f.failFor["base_b"] = errors.New("timeout")

	if err := CaptureStorage(ctx, f, st, "agent-x", now.Add(time.Hour)); err != nil {
		t.Fatalf("CaptureStorage: %v", err)
	}

	bases, _, err := st.LoadStorage(ctx, "p1", nil)
	if err != nil {
		t.Fatalf("LoadStorage: %v", err)
	}
	if len(bases) != 2 {
		t.Fatalf("bases = %d, want 2 (base_b carried forward)", len(bases))
	}
	for _, b := range bases {
		if b.BaseID == "base_b" && len(b.Items) != 1 {
			t.Errorf("base_b items = %d, want 1 carried forward", len(b.Items))
		}
	}
}

// TestCaptureStorageCarryForwardKeyedConsistently pins the fix for the I2
// review finding: observed must be keyed the same way on every path through
// the sweep loop, or a carried-forward base and a duplicate hint entry for it
// can either vanish (carry-forward lookup misses on the next pass) or get
// appended twice (violating the agent_storage primary key and failing the
// whole capture). The hint here names base_b TWICE and its query fails both
// times it would be attempted -- the fix must fetch it only once, carry the
// stored rows forward under the same id it looked storedByBase up with, and
// must not re-attempt or re-append it on the second hint entry.
func TestCaptureStorageCarryForwardKeyedConsistently(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	if err := st.ReplaceStorage(ctx, "p1", []StorageBase{
		{BaseID: "base_b", Items: []StorageItem{{ItemID: "y", Quantity: 10}}},
	}, now); err != nil {
		t.Fatalf("seed: %v", err)
	}

	f := newStorageFake("p1", "base_a")
	f.frames[""] = []byte(`{"base_id":"base_a","hint":"15 items in storage at base_a, base_b, base_b",` +
		`"items":[{"item_id":"x","quantity":5}]}`)
	f.failFor["base_b"] = errors.New("timeout")

	if err := CaptureStorage(ctx, f, st, "agent-x", now.Add(time.Hour)); err != nil {
		t.Fatalf("CaptureStorage: %v", err)
	}

	baseBFetches := 0
	for _, c := range f.calls {
		if c == "base_b" {
			baseBFetches++
		}
	}
	if baseBFetches != 1 {
		t.Errorf("base_b was fetched %d times, want 1 -- the second hint entry must be skipped as already observed",
			baseBFetches)
	}

	bases, _, err := st.LoadStorage(ctx, "p1", nil)
	if err != nil {
		t.Fatalf("LoadStorage: %v", err)
	}
	if len(bases) != 2 {
		t.Fatalf("bases = %d, want 2 (base_a + base_b carried forward exactly once): %+v", len(bases), bases)
	}
	for _, b := range bases {
		if b.BaseID == "base_b" && len(b.Items) != 1 {
			t.Errorf("base_b items = %d, want 1 carried forward", len(b.Items))
		}
	}
}

// TestCaptureStorageKeepsSeedBaseAbsentFromHint pins the data-loss path found in
// review. The hint enumerates bases with ITEMS, so a base holding only credits is
// legitimately absent from it. Without the seed-base union the agent's docked
// base is never swept, never observed, and gets DELETED -- while its decoded
// contents sit in the seed response we already have in hand.
func TestCaptureStorageKeepsSeedBaseAbsentFromHint(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	if err := st.ReplaceStorage(ctx, "p1", []StorageBase{{BaseID: "base_c", Credits: 500}}, now); err != nil {
		t.Fatalf("seed: %v", err)
	}

	f := newStorageFake("p1", "base_c")
	// Docked at a credits-only base; the hint names only the item-holding bases.
	f.frames[""] = []byte(`{"base_id":"base_c","credits":500,"items":[],` +
		`"hint":"15 items in storage at base_a, base_b"}`)
	f.frames["base_a"] = []byte(`{"base_id":"base_a","items":[{"item_id":"x","quantity":5}]}`)
	f.frames["base_b"] = []byte(`{"base_id":"base_b","items":[{"item_id":"y","quantity":10}]}`)

	if err := CaptureStorage(ctx, f, st, "agent-x", now.Add(time.Hour)); err != nil {
		t.Fatalf("CaptureStorage: %v", err)
	}

	bases, _, err := st.LoadStorage(ctx, "p1", nil)
	if err != nil {
		t.Fatalf("LoadStorage: %v", err)
	}
	var found *StorageBase
	for i := range bases {
		if bases[i].BaseID == "base_c" {
			found = &bases[i]
		}
	}
	if found == nil {
		t.Fatalf("base_c was deleted despite its 500 credits arriving in the seed response; bases = %v", bases)
	}
	if found.Credits != 500 {
		t.Errorf("base_c credits = %d, want 500", found.Credits)
	}
}

// TestCaptureStorageClearsEmptiedSeedBase pins the other side: a seed base that
// genuinely holds nothing must still be deletable, or "I sold everything at my
// home station" would never converge to zero.
func TestCaptureStorageClearsEmptiedSeedBase(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	if err := st.ReplaceStorage(ctx, "p1", []StorageBase{
		{BaseID: "base_c", Items: []StorageItem{{ItemID: "x", Quantity: 5}}},
	}, now); err != nil {
		t.Fatalf("seed: %v", err)
	}

	f := newStorageFake("p1", "base_c")
	f.frames[""] = []byte(`{"base_id":"base_c","credits":0,"items":[],` +
		`"hint":"10 items in storage at base_a"}`)
	f.frames["base_a"] = []byte(`{"base_id":"base_a","items":[{"item_id":"y","quantity":10}]}`)

	if err := CaptureStorage(ctx, f, st, "agent-x", now.Add(time.Hour)); err != nil {
		t.Fatalf("CaptureStorage: %v", err)
	}

	bases, _, err := st.LoadStorage(ctx, "p1", nil)
	if err != nil {
		t.Fatalf("LoadStorage: %v", err)
	}
	for _, b := range bases {
		if b.BaseID == "base_c" {
			t.Errorf("an emptied seed base must still be deleted, got %+v", b)
		}
	}
}

// TestCaptureStorageUndockedSeedsWithHomeBase pins that an undocked agent still
// captures. view_storage without a station_id returns not_docked, so the seed
// call must supply one.
func TestCaptureStorageUndockedSeedsWithHomeBase(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	f := newStorageFake("p1", "")
	f.state.Player.HomeBase = "base_a"
	f.frames["base_a"] = []byte(`{"base_id":"base_a","hint":"5 items in storage at base_a",` +
		`"items":[{"item_id":"x","quantity":5}]}`)

	if err := CaptureStorage(ctx, f, st, "agent-x", now); err != nil {
		t.Fatalf("CaptureStorage: %v", err)
	}
	if len(f.calls) == 0 || f.calls[0] != "base_a" {
		t.Errorf("seed call = %v, want the first call to target base_a", f.calls)
	}
}

// TestCaptureStorageNilStoreOrClientIsANoOp pins that an unconfigured store or
// client disables capture rather than erroring — assets must never be a new
// way for a worker pass to fail. Mirrors CaptureProfile's precedent.
func TestCaptureStorageNilStoreOrClientIsANoOp(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	t.Run("nil store", func(t *testing.T) {
		f := newStorageFake("p1", "base_a")
		if err := CaptureStorage(ctx, f, nil, "agent-x", now); err != nil {
			t.Errorf("nil store must be a no-op, got %v", err)
		}
	})

	t.Run("nil client", func(t *testing.T) {
		st := openTestStore(t)
		if err := CaptureStorage(ctx, nil, st, "agent-x", now); err != nil {
			t.Errorf("nil client must be a no-op, got %v", err)
		}
	})
}

// TestCaptureStorageFailedGetStatusWritesNothing pins that a GetStatus error
// leaves the store untouched rather than writing under a fresh captured_at on
// data of arbitrary age. Mirrors CaptureProfile's precedent.
func TestCaptureStorageFailedGetStatusWritesNothing(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	seed := []StorageBase{{BaseID: "base_a", Items: []StorageItem{{ItemID: "x", Quantity: 5}}}}
	if err := st.ReplaceStorage(ctx, "p1", seed, now); err != nil {
		t.Fatalf("seed: %v", err)
	}

	f := newStorageFake("p1", "base_a")
	f.statusErr = errors.New("boom")
	f.frames[""] = []byte(`{"base_id":"base_a","hint":"15 items in storage at base_a, base_b",` +
		`"items":[{"item_id":"z","quantity":99}]}`)

	if err := CaptureStorage(ctx, f, st, "agent-x", now.Add(time.Hour)); err != nil {
		t.Fatalf("CaptureStorage must not fail on a GetStatus error: %v", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("calls = %v, want none -- a GetStatus failure must stop before any view_storage call", f.calls)
	}

	bases, _, err := st.LoadStorage(ctx, "p1", nil)
	if err != nil {
		t.Fatalf("LoadStorage: %v", err)
	}
	if len(bases) != 1 || len(bases[0].Items) != 1 || bases[0].Items[0].ItemID != "x" {
		t.Errorf("bases = %+v, want the original seed untouched", bases)
	}
}
