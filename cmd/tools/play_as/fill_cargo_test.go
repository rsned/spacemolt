package main

import (
	"reflect"
	"testing"
)

func fillCats(m map[string]string) func(string) string {
	return func(id string) string {
		if c, ok := m[id]; ok {
			return c
		}
		return "unknown"
	}
}

func TestBuildFillPlan_AlphabeticalUntilFull(t *testing.T) {
	items := []storageItem{
		{ItemID: "zeta_rations", Quantity: 100, Size: 1},
		{ItemID: "algae_wafer", Quantity: 30, Size: 2},
		{ItemID: "iron_ore", Quantity: 500, Size: 1},
		{ItemID: "biogas_fizz", Quantity: 95, Size: 1},
	}
	cats := fillCats(map[string]string{
		"zeta_rations": "consumable", "algae_wafer": "consumable",
		"biogas_fizz": "consumable", "iron_ore": "ore",
	})

	got := buildFillPlan(items, 200, "consumable", cats)

	want := []fillStep{
		{ItemID: "algae_wafer", Quantity: 30, Space: 60},
		{ItemID: "biogas_fizz", Quantity: 95, Space: 95},
		{ItemID: "zeta_rations", Quantity: 45, Space: 45},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("plan = %+v, want %+v", got, want)
	}
}

func TestBuildFillPlan_SkipsTooBigKeepsSmallerLater(t *testing.T) {
	items := []storageItem{
		{ItemID: "a_big", Quantity: 10, Size: 50},
		{ItemID: "b_small", Quantity: 10, Size: 1},
	}
	cats := fillCats(map[string]string{"a_big": "component", "b_small": "component"})

	got := buildFillPlan(items, 20, "Component", cats)

	want := []fillStep{{ItemID: "b_small", Quantity: 10, Space: 10}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("plan = %+v, want %+v", got, want)
	}
}

func TestBuildFillPlan_NoSpaceOrNoMatch(t *testing.T) {
	items := []storageItem{{ItemID: "x", Quantity: 5, Size: 1}}
	cats := fillCats(map[string]string{"x": "ore"})
	if got := buildFillPlan(items, 0, "ore", cats); len(got) != 0 {
		t.Fatalf("zero space: plan = %+v, want empty", got)
	}
	if got := buildFillPlan(items, 100, "ammo", cats); len(got) != 0 {
		t.Fatalf("no match: plan = %+v, want empty", got)
	}
}

func TestBuildFillPlan_ZeroSizeTreatedAsOne(t *testing.T) {
	items := []storageItem{{ItemID: "x", Quantity: 5, Size: 0}}
	cats := fillCats(map[string]string{"x": "misc"})
	got := buildFillPlan(items, 3, "misc", cats)
	want := []fillStep{{ItemID: "x", Quantity: 3, Space: 3}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("plan = %+v, want %+v", got, want)
	}
}

func TestParseFillCargoArgs(t *testing.T) {
	opts, err := parseFillCargoArgs([]string{"--category=consumable", "--dry-run"})
	if err != nil || opts.category != "consumable" || !opts.dryRun {
		t.Fatalf("got %+v, %v", opts, err)
	}
	opts, err = parseFillCargoArgs([]string{"ore"})
	if err != nil || opts.category != "ore" || opts.dryRun {
		t.Fatalf("positional: got %+v, %v", opts, err)
	}
	if _, err := parseFillCargoArgs(nil); err == nil {
		t.Fatal("missing category: want error")
	}
	if _, err := parseFillCargoArgs([]string{"--bogus"}); err == nil {
		t.Fatal("unknown flag: want error")
	}
}
