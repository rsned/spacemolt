package worker

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/rsned/spacemolt/pkg/game"
)

func havenState() *game.State {
	return &game.State{System: game.SystemData{
		ID: "haven", Name: "Haven",
		POIs: []game.POI{
			{ID: "grand_exchange", Type: "station"},
			{ID: "commerce_fields", Type: "asteroid_belt"},
		},
	}}
}

// The shipped templates must resolve to fully literal DroneLang: actions take a
// quoted string literal, so any surviving $TOKEN$ would be uploaded verbatim
// and the drone would try to MOVE to "$STATION$".
func TestLoadDroneScriptResolvesTemplates(t *testing.T) {
	dir := filepath.Join("..", "..", "data", "scripts", "drones")
	got, err := loadDroneScript(dir, "mine_asteroid", havenState())
	if err != nil {
		t.Fatalf("loadDroneScript: %v", err)
	}
	for _, want := range []string{`at("grand_exchange")`, `MOVE "commerce_fields"`, `MINE`, `DEPOSIT`} {
		if !strings.Contains(got, want) {
			t.Errorf("resolved script missing %q\n%s", want, got)
		}
	}
	if strings.Contains(got, "$") {
		t.Errorf("unresolved token remains:\n%s", got)
	}
}

// A token with no matching POI in-system must fail BEFORE any mutation is
// spent, not upload a broken script.
func TestLoadDroneScriptFailsOnMissingPOI(t *testing.T) {
	dir := filepath.Join("..", "..", "data", "scripts", "drones")
	// Haven has no ice field, so mine_ice cannot resolve there.
	if _, err := loadDroneScript(dir, "mine_ice", havenState()); err == nil {
		t.Error("mine_ice in a system with no ice_field: want error, got nil")
	}
}

func TestLoadDroneScriptRejectsPathTraversal(t *testing.T) {
	if _, err := loadDroneScript("", "../../etc/passwd", havenState()); err == nil {
		t.Error("path separator in script name: want error, got nil")
	}
}

// Every shipped template must fit the server's 2000-char upload limit once
// substituted, and must not smuggle in a base id.
func TestShippedTemplatesAreValid(t *testing.T) {
	dir := filepath.Join("..", "..", "data", "scripts", "drones")
	st := &game.State{System: game.SystemData{ID: "s", Name: "S", POIs: []game.POI{
		{ID: "st", Type: "station"}, {ID: "belt", Type: "asteroid_belt"},
		{ID: "ice", Type: "ice_field"}, {ID: "gas", Type: "gas_cloud"},
	}}}
	for _, name := range []string{"mine_asteroid", "mine_ice", "mine_gas"} {
		got, err := loadDroneScript(dir, name, st)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(got) > droneScriptMaxChars {
			t.Errorf("%s: %d chars exceeds %d", name, len(got), droneScriptMaxChars)
		}
	}
}

func TestDroneIDFromLoad(t *testing.T) {
	id, err := droneIDFromLoad([]byte(`{"drone_id":"0fc88ae2","drone_type":"mining","bay_count":1}`))
	if err != nil || id != "0fc88ae2" {
		t.Fatalf("got %q, %v; want 0fc88ae2", id, err)
	}
	if _, err := droneIDFromLoad([]byte(`{"bay_count":1}`)); err == nil {
		t.Error("reply with no drone_id: want error, got nil")
	}
	if _, err := droneIDFromLoad(nil); err == nil {
		t.Error("empty payload: want error, got nil")
	}
}
