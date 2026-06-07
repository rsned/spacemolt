package fitting

import "testing"

func TestLoadCatalog(t *testing.T) {
	cat, err := LoadCatalog("testdata")
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	// Ships loaded.
	s, ok := cat.Ship("cobble")
	if !ok {
		t.Fatal("cobble ship not found")
	}
	if s.CPUCapacity != 12 || s.UtilitySlots != 2 || s.Tier != 0 {
		t.Errorf("cobble = %+v, unexpected fields", s)
	}
	if len(cat.Ships()) != 2 {
		t.Errorf("Ships() len = %d, want 2", len(cat.Ships()))
	}

	// Modules loaded; non-module (steel_plate, no slot) skipped.
	m, ok := cat.Module("advanced_drone_bay")
	if !ok {
		t.Fatal("advanced_drone_bay module not found")
	}
	if m.Slot != "utility" || m.CPUUsage != 12 || m.PowerUsage != 15 {
		t.Errorf("drone bay = %+v, unexpected fields", m)
	}
	if _, ok := cat.Module("steel_plate"); ok {
		t.Error("steel_plate should be skipped (no slot)")
	}
	r, _ := cat.Module("nuclear_reactor_ii")
	if r.PowerBonus != 30 || r.RequiredSkills["mining"] != 8 {
		t.Errorf("reactor = %+v, unexpected fields", r)
	}

	// Engineering efficiency read from skills.
	eng := cat.Engineering(10)
	if eng.Level != 10 || eng.CPUEffPerLevel != 1 || eng.PowerEffPerLevel != 1 {
		t.Errorf("Engineering(10) = %+v, want level 10 / eff 1 / 1", eng)
	}
}

func TestLoadCatalog_MissingDir(t *testing.T) {
	if _, err := LoadCatalog("testdata/does-not-exist"); err == nil {
		t.Error("expected error for missing dir, got nil")
	}
}
