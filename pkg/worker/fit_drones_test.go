package worker

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"slices"
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

// fitDronesDispatch wires a WorkerDispatch onto the shared fakeClient, docked at
// Haven, for the deploy-flag tests.
func fitDronesDispatch(t *testing.T) (*WorkerDispatch, *fakeClient) {
	t.Helper()
	// FitDrones resolves scripts from DefaultDroneScriptDir, a repo-relative
	// path, so the shipped templates are only visible from the repo root.
	t.Chdir(filepath.Join("..", ".."))
	st := havenState()
	st.Doc = true
	fc := &fakeClient{state: st}
	return &WorkerDispatch{Client: fc, Out: io.Discard}, fc
}

// deploy=false is the pre-travel case: a marketbot is fitted at its ORIGIN and
// must carry the drones to its post. Undocking and launching them at the origin
// would put them to work in the wrong system moments before the ship jumps out,
// so the run has to stop cleanly after the last upload.
func TestFitDronesNoDeployStopsAfterUpload(t *testing.T) {
	d, fc := fitDronesDispatch(t)

	if err := d.FitDrones(context.Background(), "mine_asteroid", 1, 3, "", "", false); err != nil {
		t.Fatalf("FitDrones: %v", err)
	}

	got := strings.Join(fc.calls, ",")
	for _, forbidden := range []string{"undock", "deploy_drone", "dock"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("deploy=false issued %q; calls: %v", forbidden, fc.calls)
		}
	}
	// The fitting itself must still have happened in full.
	for _, want := range []string{
		"withdraw:advanced_drone_bay:1", "install_mod:advanced_drone_bay",
		"withdraw:mining_drone:3",
		"load_drone:mining_drone", "upload_script:drone-1", "upload_script:drone-3",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q; calls: %v", want, fc.calls)
		}
	}
	if len(fc.uploadedScripts) != 3 {
		t.Errorf("uploaded %d scripts, want 3", len(fc.uploadedScripts))
	}
}

// deploy=true must keep behaving exactly as before the flag existed, since every
// existing caller relies on the trailing undock/deploy/dock.
func TestFitDronesDeployTrueStillLaunches(t *testing.T) {
	d, fc := fitDronesDispatch(t)

	if err := d.FitDrones(context.Background(), "mine_asteroid", 1, 2, "", "", true); err != nil {
		t.Fatalf("FitDrones: %v", err)
	}

	got := strings.Join(fc.calls, ",")
	for _, want := range []string{"undock", "deploy_drone:all=true", "dock"} {
		if !strings.Contains(got, want) {
			t.Errorf("deploy=true missing %q; calls: %v", want, fc.calls)
		}
	}
	// Deploy must come after every upload, never interleaved.
	if di, ui := strings.Index(got, "deploy_drone"), strings.LastIndex(got, "upload_script"); di < ui {
		t.Errorf("deployed before the last upload; calls: %v", fc.calls)
	}
}

// Each drone must get its OWN id from its own load reply. Scripting drone-1
// three times would leave two drones idle and look like success.
func TestFitDronesScriptsEachDroneOnce(t *testing.T) {
	d, fc := fitDronesDispatch(t)

	if err := d.FitDrones(context.Background(), "mine_asteroid", 1, 4, "", "", false); err != nil {
		t.Fatalf("FitDrones: %v", err)
	}
	for _, want := range []string{"drone-1", "drone-2", "drone-3", "drone-4"} {
		if !strings.Contains(strings.Join(fc.calls, ","), "upload_script:"+want) {
			t.Errorf("no upload to %s; calls: %v", want, fc.calls)
		}
	}
}

// launch_drones is the arrival half of fit_drones deploy=false. Order is the
// whole contract: deploy must happen between the undock and the re-dock.
func TestLaunchDronesUndocksDeploysRedocks(t *testing.T) {
	d, fc := fitDronesDispatch(t)

	if err := d.LaunchDrones(context.Background(), ""); err != nil {
		t.Fatalf("LaunchDrones: %v", err)
	}
	if got, want := fc.calls, []string{"undock", "deploy_drone:all=true", "dock"}; !slices.Equal(got, want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
	if !fc.state.Doc {
		t.Error("agent left undocked; a resident must end docked to keep capturing")
	}
}

// A named drone deploys alone -- all=false -- so one drone can be relaunched
// without disturbing the rest of the bay.
func TestLaunchDronesSingleDroneIsNotAll(t *testing.T) {
	d, fc := fitDronesDispatch(t)

	if err := d.LaunchDrones(context.Background(), "drone-2"); err != nil {
		t.Fatalf("LaunchDrones: %v", err)
	}
	if got := strings.Join(fc.calls, ","); !strings.Contains(got, "deploy_drone:all=false") {
		t.Errorf("named drone deployed with all=true; calls: %v", fc.calls)
	}
}

// A failed re-dock must NOT read as a failed launch: the drones are already
// deployed and only the agent's position is wrong.
func TestLaunchDronesRedockFailureSaysDronesAreDeployed(t *testing.T) {
	d, fc := fitDronesDispatch(t)
	fc.dockErr = errors.New("docking bay full")

	err := d.LaunchDrones(context.Background(), "")
	if err == nil {
		t.Fatal("expected an error when the re-dock fails")
	}
	if !strings.Contains(err.Error(), "drones ARE deployed") {
		t.Errorf("error does not flag that the launch succeeded: %v", err)
	}
}
