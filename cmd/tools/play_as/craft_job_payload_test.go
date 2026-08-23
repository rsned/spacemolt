package main

import "testing"

// The bug this guards: `craft unpack_package --package_id=H --source=faction`
// sent {"recipe_id":"unpack_package","quantity":1} because the command never
// read package_id/source/target. partitionFlagsKV drops unknown flags in
// silence, so the fast path fired and the server answered "package_id is
// required when unpacking" — a client defect wearing a usage error's clothes.
func TestCraftJobPayload_PackageFlagsReachThePayload(t *testing.T) {
	payload, advanced, err := craftJobPayload("unpack_package", 1, map[string]string{
		"package_id": "6a71f551b0d6812376a8cd4a3396ef60",
		"source":     "faction",
		"target":     "faction",
	})
	if err != nil {
		t.Fatalf("craftJobPayload: %v", err)
	}
	if !advanced {
		t.Fatal("advanced = false; a package job cannot use the fast path — CraftWithOptions has nowhere to put package_id")
	}
	for k, want := range map[string]any{
		"recipe_id":  "unpack_package",
		"quantity":   1,
		"package_id": "6a71f551b0d6812376a8cd4a3396ef60",
		"source":     "faction",
		"target":     "faction",
	} {
		if got := payload[k]; got != want {
			t.Errorf("payload[%q] = %v, want %v", k, got, want)
		}
	}
}

func TestCraftJobPayload_PlainCraftStaysOnFastPath(t *testing.T) {
	payload, advanced, err := craftJobPayload("refine_steel", 12, map[string]string{"deliver_to": "storage"})
	if err != nil {
		t.Fatalf("craftJobPayload: %v", err)
	}
	if advanced {
		t.Error("advanced = true for a plain craft; deliver_to alone is carried by CraftWithOptions")
	}
	if payload["deliver_to"] != "storage" || payload["quantity"] != 12 {
		t.Errorf("payload = %v", payload)
	}
	for _, k := range []string{"package_id", "source", "target", "preset", "facility_id", "dry_run"} {
		if _, ok := payload[k]; ok {
			t.Errorf("payload carries %q for a plain craft", k)
		}
	}
}

func TestCraftJobPayload_Rejects(t *testing.T) {
	for name, flags := range map[string]map[string]string{
		"bad source":     {"package_id": "h", "source": "warehouse"},
		"bad target":     {"package_id": "h", "target": "elsewhere"},
		"bad deliver_to": {"deliver_to": "cargo"},
		"bad preset":     {"preset": "quick"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := craftJobPayload("unpack_package", 1, flags); err == nil {
				t.Fatalf("expected error for %v", flags)
			}
		})
	}
}

// Each advanced flag must independently force the generic path.
func TestCraftJobPayload_EachAdvancedFlagLeavesFastPath(t *testing.T) {
	for name, flags := range map[string]map[string]string{
		"package_id":  {"package_id": "h"},
		"source":      {"source": "cargo"},
		"target":      {"target": "storage"},
		"preset":      {"preset": "fast"},
		"facility_id": {"facility_id": "f1"},
		"dry_run":     {"dry_run": ""},
	} {
		t.Run(name, func(t *testing.T) {
			_, advanced, err := craftJobPayload("refine_steel", 1, flags)
			if err != nil {
				t.Fatalf("craftJobPayload: %v", err)
			}
			if !advanced {
				t.Errorf("%s did not force the advanced path", name)
			}
		})
	}
}
