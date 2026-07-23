package supervisor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOverridesRoundTripAndSubtract(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x-overrides.json")

	// Missing file -> empty, no error.
	o, err := LoadOverrides(path)
	if err != nil || len(o.Removed) != 0 {
		t.Fatalf("missing file: o=%+v err=%v, want empty/nil", o, err)
	}

	o.Add("craftsman-1")
	o.Add("craftsman-1") // idempotent
	if err := SaveOverrides(path, o); err != nil {
		t.Fatalf("save: %v", err)
	}
	back, err := LoadOverrides(path)
	if err != nil || !back.IsRemoved("craftsman-1") || len(back.Removed) != 1 {
		t.Fatalf("reload: %+v err=%v", back, err)
	}

	specs := []WorkerSpec{{AgentID: "craftsman-1"}, {AgentID: "fighter-4"}}
	eff := SubtractOverrides(specs, back)
	if len(eff) != 1 || eff[0].AgentID != "fighter-4" {
		t.Fatalf("subtract: %+v, want only fighter-4", eff)
	}

	back.Delete("craftsman-1")
	if back.IsRemoved("craftsman-1") {
		t.Fatal("delete did not remove")
	}

	// Corrupt file -> empty + error (caller logs and continues).
	if err := os.WriteFile(path, []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	o2, err := LoadOverrides(path)
	if err == nil || len(o2.Removed) != 0 {
		t.Fatalf("corrupt: o=%+v err=%v, want empty + error", o2, err)
	}
}
