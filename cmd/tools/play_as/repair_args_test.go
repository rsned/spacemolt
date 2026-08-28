package main

import "testing"

func TestRepairArgs(t *testing.T) {
	tests := []struct {
		name  string
		parts []string
		want  map[string]any
	}{
		// The bug this function exists for: `repair fleet` was swallowed whole by
		// parseFlagArgs (bare tokens hit a `continue`), so an EMPTY payload went
		// out as a plain hull repair — which spends credits at a station.
		{"bare target", []string{"repair", "fleet"}, map[string]any{"target": "fleet"}},
		{"bare player name", []string{"repair", "ThomasEdison"}, map[string]any{"target": "ThomasEdison"}},

		{"flag target", []string{"repair", "--target", "fleet"}, map[string]any{"target": "fleet"}},
		{"flag target equals", []string{"repair", "--target=fleet"}, map[string]any{"target": "fleet"}},

		// item_id is lowercased; quantity is coerced to an int.
		{
			"kit and quantity",
			[]string{"repair", "--item_id=Repair_Kit", "--quantity=3"},
			map[string]any{"item_id": "repair_kit", "quantity": 3},
		},
		{
			"bare target with flags",
			[]string{"repair", "fleet", "--quantity=2"},
			map[string]any{"target": "fleet", "quantity": 2},
		},

		// No args at all: nil payload, so the caller uses the plain Repair path.
		{"no args", []string{"repair"}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repairArgs(tt.parts)
			if err != nil {
				t.Fatalf("repairArgs(%q) unexpected error: %v", tt.parts, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("repairArgs(%q) = %#v, want %#v", tt.parts, got, tt.want)
			}
			for k, w := range tt.want {
				if got[k] != w {
					t.Errorf("repairArgs(%q)[%q] = %#v, want %#v", tt.parts, k, got[k], w)
				}
			}
		})
	}
}

// A case-sensitive player id must survive: lowercasing target the way item_id is
// lowercased would make `repair ThomasEdison` unrepairable.
func TestRepairArgsPreservesTargetCase(t *testing.T) {
	got, err := repairArgs([]string{"repair", "ThomasEdison"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["target"] != "ThomasEdison" {
		t.Errorf("target = %#v, want %q (case must be preserved)", got["target"], "ThomasEdison")
	}
}

func TestRepairArgsRejectsUnknownFlag(t *testing.T) {
	if _, err := repairArgs([]string{"repair", "--targt=fleet"}); err == nil {
		t.Fatal("repairArgs accepted unknown flag --targt; want an error naming it")
	}
}

// A single-dash long flag must not be read as a positional target: `repair
// -target fleet` would otherwise silently become target="-target".
func TestRepairArgsRejectsSingleDashFlag(t *testing.T) {
	if _, err := repairArgs([]string{"repair", "-target", "fleet"}); err == nil {
		t.Fatal("repairArgs accepted single-dash flag -target; want an error")
	}
}
