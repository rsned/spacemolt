package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

func TestPoiToIntelMap_Fields(t *testing.T) {
	poi := serverapi.POI{
		ID:          "garden_world",
		SystemID:    "traders_rest",
		Type:        "planet",
		Class:       "terran",
		Name:        "Garden World",
		Description: "Agricultural luxury.",
		Position:    serverapi.Position{X: 3, Y: -0.3},
	}
	m := poiToIntelMap(poi, nil)

	if m["id"] != "garden_world" || m["type"] != "planet" || m["name"] != "Garden World" {
		t.Errorf("core fields wrong: %+v", m)
	}
	if m["class"] != "terran" || m["description"] != "Agricultural luxury." {
		t.Errorf("optional fields wrong: %+v", m)
	}
	pos, ok := m["position"].(map[string]any)
	if !ok || pos["x"] != float64(3) || pos["y"] != float64(-0.3) {
		t.Errorf("position wrong: %+v", m["position"])
	}
	if _, ok := m["resources"]; ok {
		t.Errorf("expected no resources key when none present, got %+v", m["resources"])
	}
}

func TestPoiToIntelMap_ResourcesFromDisplay(t *testing.T) {
	poi := serverapi.POI{ID: "vein", Type: "gas_cloud", Name: "Vein", SystemID: "ross_128"}
	res := []serverapi.ResourceDisplay{
		{ResourceID: "prismatic_nebulite", Richness: 28, Remaining: 5500},
	}
	m := poiToIntelMap(poi, res)
	rs, ok := m["resources"].([]map[string]any)
	if !ok || len(rs) != 1 {
		t.Fatalf("resources not mapped: %+v", m["resources"])
	}
	if rs[0]["resource_id"] != "prismatic_nebulite" || rs[0]["richness"] != float64(28) || rs[0]["remaining"] != float64(5500) {
		t.Errorf("resource fields wrong: %+v", rs[0])
	}
}

func TestPoiToIntelMap_ResourcesFallbackToPOI(t *testing.T) {
	poi := serverapi.POI{
		ID: "belt", Type: "asteroid_belt", Name: "Belt", SystemID: "sol",
		Resources: []serverapi.POIResource{{ResourceID: "iron_ore", Richness: 85, Remaining: 50000}},
	}
	m := poiToIntelMap(poi, nil)
	rs, ok := m["resources"].([]map[string]any)
	if !ok || len(rs) != 1 || rs[0]["resource_id"] != "iron_ore" {
		t.Fatalf("expected fallback to poi.Resources, got %+v", m["resources"])
	}
}

func TestBuildIntelSystems_GroupsBySystem(t *testing.T) {
	dir := t.TempDir()
	// Two POIs in traders_rest, one in ross_128.
	writeJSON(t, filepath.Join(dir, "traders_rest___garden_world.json"),
		`{"poi":{"id":"garden_world","system_id":"traders_rest","type":"planet","name":"Garden World"}}`)
	writeJSON(t, filepath.Join(dir, "traders_rest___factory.json"),
		`{"poi":{"id":"factory","system_id":"traders_rest","type":"station","name":"Factory"}}`)
	writeJSON(t, filepath.Join(dir, "ross_128___vein.json"),
		`{"poi":{"id":"vein","system_id":"ross_128","type":"gas_cloud","name":"Vein"}}`)

	files, err := collectIntelFiles(dir)
	if err != nil {
		t.Fatalf("collectIntelFiles: %v", err)
	}
	systems, poiCount, warnings, err := buildIntelSystems(context.Background(), files)
	if err != nil {
		t.Fatalf("buildIntelSystems: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if poiCount != 3 {
		t.Errorf("poiCount = %d, want 3", poiCount)
	}
	if len(systems) != 2 {
		t.Fatalf("systems = %d, want 2", len(systems))
	}

	bySys := map[string]map[string]any{}
	for _, s := range systems {
		bySys[s["system_id"].(string)] = s
	}
	tr := bySys["traders_rest"]
	if tr == nil {
		t.Fatal("missing traders_rest system")
	}
	if tr["name"] != "traders_rest" { // KB nil -> fallback to system id
		t.Errorf("name fallback = %v, want traders_rest", tr["name"])
	}
	if pois, _ := tr["pois"].([]map[string]any); len(pois) != 2 {
		t.Errorf("traders_rest pois = %d, want 2", len(pois))
	}
}

func TestBuildIntelSystems_Passthrough(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "preformatted.json"),
		`{"systems":[{"system_id":"alpha","name":"Alpha","pois":[{"id":"p1","type":"planet","name":"P1"}]}]}`)

	files, _ := collectIntelFiles(dir)
	systems, poiCount, warnings, err := buildIntelSystems(context.Background(), files)
	if err != nil {
		t.Fatalf("buildIntelSystems: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if len(systems) != 1 || systems[0]["system_id"] != "alpha" || poiCount != 1 {
		t.Fatalf("passthrough mismatch: systems=%+v count=%d", systems, poiCount)
	}
}

func TestBuildIntelSystems_SkipsBadFiles(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "good.json"),
		`{"poi":{"id":"p","system_id":"sol","type":"planet","name":"P"}}`)
	writeJSON(t, filepath.Join(dir, "garbage.json"), `not json at all`)
	writeJSON(t, filepath.Join(dir, "nosystem.json"),
		`{"poi":{"id":"q","type":"planet","name":"Q"}}`)

	files, _ := collectIntelFiles(dir)
	systems, poiCount, warnings, err := buildIntelSystems(context.Background(), files)
	if err != nil {
		t.Fatalf("buildIntelSystems: %v", err)
	}
	if poiCount != 1 || len(systems) != 1 {
		t.Errorf("expected 1 good POI, got count=%d systems=%d", poiCount, len(systems))
	}
	if len(warnings) != 2 {
		t.Errorf("expected 2 warnings (garbage + missing system_id), got %v", warnings)
	}
}

func TestResolveIntelPath_RelativeToIntelDir(t *testing.T) {
	dir := t.TempDir()
	orig := globalIntelDir
	globalIntelDir = dir
	t.Cleanup(func() { globalIntelDir = orig })

	sub := filepath.Join(dir, "sol")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(sub, "sol___earth.json"), `{"poi":{"id":"earth"}}`)

	// Relative path resolves under globalIntelDir.
	got, err := resolveIntelPath("sol/sol___earth.json")
	if err != nil {
		t.Fatalf("resolveIntelPath: %v", err)
	}
	if got != filepath.Join(dir, "sol", "sol___earth.json") {
		t.Errorf("resolved = %q", got)
	}

	if _, err := resolveIntelPath("does/not/exist.json"); err == nil {
		t.Error("expected error for missing path")
	}
}

func writeJSON(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
