package rescue

import (
	"testing"
)

func TestDebtRoundTrip(t *testing.T) {
	dir := t.TempDir()
	// Missing file -> empty, no error.
	got, err := LoadDebts(dir, "salvager-10")
	if err != nil || len(got) != 0 {
		t.Fatalf("LoadDebts on missing = (%v, %v), want (nil, nil)", got, err)
	}
	// Append accumulates in order.
	if err := AppendDebt(dir, "salvager-10", Debt{Recipient: "shipside_assist_haven", Credits: 1000}); err != nil {
		t.Fatal(err)
	}
	if err := AppendDebt(dir, "salvager-10", Debt{Recipient: "shipside_assist_sol", Credits: 1000}); err != nil {
		t.Fatal(err)
	}
	got, err = LoadDebts(dir, "salvager-10")
	if err != nil || len(got) != 2 || got[0].Recipient != "shipside_assist_haven" || got[1].Recipient != "shipside_assist_sol" {
		t.Fatalf("after 2 appends = %+v (err %v)", got, err)
	}
	// RemoveHead pops the first.
	if err := RemoveHead(dir, "salvager-10"); err != nil {
		t.Fatal(err)
	}
	got, _ = LoadDebts(dir, "salvager-10")
	if len(got) != 1 || got[0].Recipient != "shipside_assist_sol" {
		t.Fatalf("after RemoveHead = %+v, want [sol]", got)
	}
	// RemoveHead on the last entry removes the file (LoadDebts -> empty).
	if err := RemoveHead(dir, "salvager-10"); err != nil {
		t.Fatal(err)
	}
	if got, _ = LoadDebts(dir, "salvager-10"); len(got) != 0 {
		t.Fatalf("after draining = %+v, want empty", got)
	}
	// RemoveHead on an already-empty/missing file is a no-op, no error.
	if err := RemoveHead(dir, "salvager-10"); err != nil {
		t.Fatalf("RemoveHead on missing = %v, want nil", err)
	}
}
