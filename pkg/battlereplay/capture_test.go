package battlereplay

import (
	"context"
	"errors"
	"testing"

	"github.com/rsned/spacemolt/pkg/knowledge"
)

// fakeRecorder stands in for the SQLite KB. It records what was written so the
// resolution rules can be asserted without a database.
type fakeRecorder struct {
	byName  map[string]string
	nameErr error
	written []knowledge.WildlifeAttack
	calls   int
}

func (f *fakeRecorder) UpsertWildlifeAttacks(_ context.Context, rows []knowledge.WildlifeAttack) error {
	f.written = append(f.written, rows...)

	return nil
}

func (f *fakeRecorder) SpeciesByDisplayName(_ context.Context) (map[string]string, error) {
	f.calls++
	if f.nameErr != nil {
		return nil, f.nameErr
	}

	return f.byName, nil
}

// TestResolveCreatureSpecies_FallsBackToTheDisplayName is the ambush case, and
// the reason the fallback exists. salvager-7 was killed without ever running
// get_nearby, so nothing typed the creature; all the battle carries is the
// display name "Rainbow Leviathan".
func TestResolveCreatureSpecies_FallsBackToTheDisplayName(t *testing.T) {
	m := leviathanModel(t)
	rec := &fakeRecorder{byName: map[string]string{"rainbow leviathan": "rainbow_leviathan"}}

	got, err := ResolveCreatureSpecies(context.Background(), rec, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got[leviathanID] != "rainbow_leviathan" {
		t.Fatalf("resolved = %v, want the leviathan named from the guide", got)
	}
	// The player participant must not be resolved as wildlife.
	if len(got) != 1 {
		t.Errorf("resolved %d participants, want only the creature: %v", len(got), got)
	}
}

// TestResolveCreatureSpecies_CallerMappingWins: a get_nearby typed the creature
// directly, so the guide is not consulted at all.
func TestResolveCreatureSpecies_CallerMappingWins(t *testing.T) {
	m := leviathanModel(t)
	rec := &fakeRecorder{byName: map[string]string{"rainbow leviathan": "wrong_species"}}

	got, err := ResolveCreatureSpecies(context.Background(), rec, m,
		map[string]string{leviathanID: "rainbow_leviathan"})
	if err != nil {
		t.Fatal(err)
	}
	if got[leviathanID] != "rainbow_leviathan" {
		t.Errorf("resolved = %v", got)
	}
	if rec.calls != 0 {
		t.Errorf("consulted the name index %d times when every creature was already typed", rec.calls)
	}
}

// TestResolveCreatureSpecies_UnknownNameIsSkipped: an unrecognised creature is
// dropped rather than filed under a slug invented from its display name, which
// would create a permanent phantom species.
func TestResolveCreatureSpecies_UnknownNameIsSkipped(t *testing.T) {
	m := leviathanModel(t)
	rec := &fakeRecorder{byName: map[string]string{"slag-tortoise": "slag_tortoise"}}

	got, err := ResolveCreatureSpecies(context.Background(), rec, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("resolved %v from a name the guide does not hold", got)
	}
}

func TestResolveCreatureSpecies_LookupFailurePropagates(t *testing.T) {
	m := leviathanModel(t)
	rec := &fakeRecorder{nameErr: errors.New("db down")}

	if _, err := ResolveCreatureSpecies(context.Background(), rec, m, nil); err == nil {
		t.Fatal("a failed species lookup was swallowed; the battle would silently record nothing")
	}
}

// TestCaptureWildlifeAttacks_NoKBOrBattleIsANoOp: capture is opportunistic
// everywhere else in wildlife, and a session without a SQLite KB must still be
// able to fight.
func TestCaptureWildlifeAttacks_NoKBOrBattleIsANoOp(t *testing.T) {
	n, err := CaptureWildlifeAttacks(context.Background(), nil, nil, "", nil, nil)
	if err != nil || n != 0 {
		t.Errorf("n=%d err=%v, want a silent no-op", n, err)
	}
}
