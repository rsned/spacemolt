package main

import "testing"

func TestValidateRenamesIsBijection(t *testing.T) {
	if err := validateRenames(explorerRenames); err != nil {
		t.Fatalf("explorerRenames invalid: %v", err)
	}
	if len(explorerRenames) != 10 {
		t.Fatalf("want 10 renames, got %d", len(explorerRenames))
	}
}

func TestRenameTargetsMatchExpectedEmpire(t *testing.T) {
	// Every target slot must have an expected-empire entry.
	for _, r := range explorerRenames {
		n, err := explorerNum(r.To)
		if err != nil {
			t.Fatalf("bad target %q: %v", r.To, err)
		}
		if _, ok := expectedEmpire[n]; !ok {
			t.Fatalf("target %q (slot %d) has no expectedEmpire entry", r.To, n)
		}
	}
	for _, id := range placeholderSlots {
		n, _ := explorerNum(id)
		if expectedEmpire[n] != "outerrim" {
			t.Fatalf("placeholder slot %d should be outerrim, got %q", n, expectedEmpire[n])
		}
	}
}

func TestExplorerNum(t *testing.T) {
	n, err := explorerNum("explorer-12")
	if err != nil || n != 12 {
		t.Fatalf("got (%d,%v), want (12,nil)", n, err)
	}
	if _, err := explorerNum("explorer-x"); err == nil {
		t.Fatal("want error for non-numeric suffix")
	}
}
