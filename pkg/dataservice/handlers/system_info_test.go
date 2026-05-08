package handlers

import (
	"context"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/dataservice"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

func newSystemInfoDeps(t *testing.T) (dataservice.Deps, func()) {
	t.Helper()
	ctx := context.Background()

	kb := knowledge.NewMemoryKB()

	if err := kb.RememberSystem(ctx, knowledge.System{
		ID:              "nova_terra",
		Name:            "Nova Terra",
		Empire:          "terran",
		SecurityStatus:  "high_sec",
		PoliceLevel:     3,
		Position:        game.Position{X: 12, Y: -3},
		LastUpdatedTick: 50,
		LastVisitedTick: 50,
	}); err != nil {
		t.Fatalf("remember system: %v", err)
	}
	pois := []knowledge.POI{
		{ID: "nova_terra_central", SystemID: "nova_terra", Type: "station", Name: "Nova Terra Central"},
		{ID: "nova_terra_industrial_belt", SystemID: "nova_terra", Type: "asteroid_belt", Name: "Nova Terra Industrial Belt"},
		{ID: "nova_terra_star", SystemID: "nova_terra", Type: "sun", Name: "Nova Terra Star"},
		{ID: "nova_terra_i", SystemID: "nova_terra", Type: "planet", Name: "Nova Terra I"},
	}
	for _, p := range pois {
		if err := kb.RememberPOI(ctx, p); err != nil {
			t.Fatalf("remember poi %s: %v", p.ID, err)
		}
	}

	return dataservice.Deps{
		KB:   kb,
		Tick: func() int64 { return 100 },
	}, func() { _ = kb.Close() }
}

func TestSystemInfo_PlaintextHappy(t *testing.T) {
	deps, cleanup := newSystemInfoDeps(t)
	defer cleanup()

	h := SystemInfo{}
	reply, err := h.HandlePlaintext(context.Background(), deps, []string{"nova_terra"})
	if err != nil {
		t.Fatalf("HandlePlaintext: %v", err)
	}
	for _, want := range []string{
		"System: Nova Terra (nova_terra)",
		"Empire: terran",
		"high_sec (police 3)",
		"POIs (4)",
		"nova_terra_industrial_belt",
		"asteroid_belt",
		"nova_terra_central",
		"station",
	} {
		if !strings.Contains(reply, want) {
			t.Errorf("reply missing %q\n%s", want, reply)
		}
	}
	if strings.Contains(reply, "Nova Terra Central") {
		t.Errorf("reply should not include POI human name, only id+type:\n%s", reply)
	}
	if strings.Contains(strings.ToLower(reply), "connections:") {
		t.Errorf("reply should not include connections summary:\n%s", reply)
	}
}

func TestSystemInfo_PlaintextOrderedByTypeThenID(t *testing.T) {
	deps, cleanup := newSystemInfoDeps(t)
	defer cleanup()

	h := SystemInfo{}
	reply, err := h.HandlePlaintext(context.Background(), deps, []string{"nova_terra"})
	if err != nil {
		t.Fatalf("HandlePlaintext: %v", err)
	}
	idxBelt := strings.Index(reply, "nova_terra_industrial_belt")
	idxPlanet := strings.Index(reply, "nova_terra_i ")
	idxStation := strings.Index(reply, "nova_terra_central")
	idxStar := strings.Index(reply, "nova_terra_star")
	if idxBelt < 0 || idxPlanet < 0 || idxStation < 0 || idxStar < 0 {
		t.Fatalf("missing one of the POIs:\n%s", reply)
	}
	if idxBelt >= idxPlanet || idxPlanet >= idxStation || idxStation >= idxStar {
		t.Errorf("POIs not sorted by type (asteroid_belt, planet, station, sun):\n%s", reply)
	}
}

func TestSystemInfo_PlaintextNotFound(t *testing.T) {
	deps, cleanup := newSystemInfoDeps(t)
	defer cleanup()

	h := SystemInfo{}
	reply, err := h.HandlePlaintext(context.Background(), deps, []string{"unknown_system"})
	if err != nil {
		t.Fatalf("HandlePlaintext: %v", err)
	}
	if !strings.Contains(reply, "not found") {
		t.Errorf("expected not-found message: %q", reply)
	}
}

func TestSystemInfo_PlaintextMissingArg(t *testing.T) {
	deps, cleanup := newSystemInfoDeps(t)
	defer cleanup()

	h := SystemInfo{}
	if _, err := h.HandlePlaintext(context.Background(), deps, nil); err == nil {
		t.Fatal("expected parse error on missing system_id")
	}
}

func TestSystemInfo_JSONHappy(t *testing.T) {
	deps, cleanup := newSystemInfoDeps(t)
	defer cleanup()

	h := SystemInfo{}
	out, err := h.HandleJSON(context.Background(), deps, map[string]any{"system_id": "nova_terra"})
	if err != nil {
		t.Fatalf("HandleJSON: %v", err)
	}
	if out["found"] != true {
		t.Errorf("found: got %v", out["found"])
	}
	if out["system_name"] != "Nova Terra" {
		t.Errorf("system_name: got %v", out["system_name"])
	}
	if out["police_level"] != 3 {
		t.Errorf("police_level: got %v", out["police_level"])
	}
	pois, ok := out["pois"].([]map[string]any)
	if !ok {
		t.Fatalf("pois not slice of maps, got %T", out["pois"])
	}
	if len(pois) != 4 {
		t.Fatalf("expected 4 pois, got %d", len(pois))
	}
	for _, p := range pois {
		if _, ok := p["name"]; ok {
			t.Errorf("pois entries should not include name: %v", p)
		}
		if p["id"] == nil || p["type"] == nil {
			t.Errorf("poi entry missing id or type: %v", p)
		}
	}
}

func TestSystemInfo_JSONNotFound(t *testing.T) {
	deps, cleanup := newSystemInfoDeps(t)
	defer cleanup()

	h := SystemInfo{}
	out, err := h.HandleJSON(context.Background(), deps, map[string]any{"system_id": "unknown_system"})
	if err != nil {
		t.Fatalf("HandleJSON: %v", err)
	}
	if out["found"] != false {
		t.Errorf("expected found=false, got %v", out["found"])
	}
}

func TestSystemInfo_JSONMissingField(t *testing.T) {
	deps, cleanup := newSystemInfoDeps(t)
	defer cleanup()

	h := SystemInfo{}
	if _, err := h.HandleJSON(context.Background(), deps, nil); err == nil {
		t.Fatal("expected error for missing system_id")
	}
}

func TestSystemInfo_HelpFitsBudget(t *testing.T) {
	r := dataservice.NewRegistry(dataservice.Deps{})
	r.Register(SystemInfo{})

	pt, err := r.Dispatch(context.Background(), "help")
	if err != nil {
		t.Fatalf("dispatch help: %v", err)
	}
	if len(pt) > dataservice.MaxReplyChars {
		t.Errorf("help over budget: %d > %d", len(pt), dataservice.MaxReplyChars)
	}
}
