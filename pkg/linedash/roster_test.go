package linedash

import "testing"

func TestLoadRosterReadsPipelineFields(t *testing.T) {
	r, err := LoadRoster("testdata/fleet")
	if err != nil {
		t.Fatal(err)
	}
	e := r["miningbot_0000"]
	if e.Fleet != "crft-mining" || e.Role != "pipeline_miner" || e.Station != "sol_central" {
		t.Errorf("entry = %+v", e)
	}
	if e.Pipeline != "pirate_moonshine_ingredient,sol,belt_1,crft_hub,craftsman-8" {
		t.Errorf("pipeline = %q", e.Pipeline)
	}
	c := r["craftsman-8"]
	if len(c.Consign) != 1 || len(c.Shopkeep) != 1 || len(c.FactionPull) != 1 {
		t.Errorf("crafter lists = %+v", c)
	}
	if e.AwayFleet != "" || e.Phase != "" {
		t.Errorf("home-phase secondment must not show: %+v", e)
	}
}

func TestRosterSecondedAndRemoved(t *testing.T) {
	r, err := LoadRoster("testdata/fleet")
	if err != nil {
		t.Fatal(err)
	}
	e := r["miningbot_0004"]
	if e.Fleet != "unlock" || e.Removed || e.AwayFleet != "unlock" || e.Phase != "seconded" {
		t.Errorf("0004 = %+v", e)
	}
	if e := r["miningbot_0026"]; !e.Removed {
		t.Errorf("0026 should be removed: %+v", e)
	}
}

func TestLoadRosterMissingDirIsEmptyNotError(t *testing.T) {
	r, err := LoadRoster("testdata/nope")
	if err != nil || len(r) != 0 {
		t.Errorf("got %v, %v", r, err)
	}
}

func TestShopFloor(t *testing.T) {
	rules := []string{"x:200", "pirate_moonshine:3000:1500", "bad:abc"}
	if s, f, ok := ShopFloor(rules, "pirate_moonshine"); !ok || s != 3000 || f != 1500 {
		t.Errorf("moonshine = %d %d %v", s, f, ok)
	}
	if s, f, ok := ShopFloor(rules, "x"); !ok || s != 200 || f != 200 {
		t.Errorf("x = %d %d %v", s, f, ok)
	}
	if _, _, ok := ShopFloor(rules, "bad"); ok {
		t.Error("malformed rule must be !ok")
	}
	if _, _, ok := ShopFloor(rules, "absent"); ok {
		t.Error("absent item must be !ok")
	}
}
