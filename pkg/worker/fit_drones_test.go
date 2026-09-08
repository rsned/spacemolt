package worker

import (
	"context"
	"errors"
	"fmt"
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

	if err := d.FitDrones(context.Background(), "mine_asteroid", 1, 3, "", "", FitDronesOpts{Deploy: false, Strip: false}); err != nil {
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

	if err := d.FitDrones(context.Background(), "mine_asteroid", 1, 2, "", "", FitDronesOpts{Deploy: true, Strip: false}); err != nil {
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

	if err := d.FitDrones(context.Background(), "mine_asteroid", 1, 4, "", "", FitDronesOpts{Deploy: false, Strip: false}); err != nil {
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

// shipJSON builds a get_ship reply carrying the given fitted modules.
func shipJSON(mods ...[2]string) []byte {
	parts := make([]string, 0, len(mods))
	for _, m := range mods {
		parts = append(parts, fmt.Sprintf(`{"id":%q,"type_id":%q,"name":"m"}`, m[0], m[1]))
	}
	return []byte(`{"modules":[` + strings.Join(parts, ",") + `]}`)
}

// The fresh-account case: a starter hull's ONE default module leaves too little
// CPU/power for the bay, so the install fails until it is stripped.
func TestFitDronesStripRemovesTheSingleDefaultModule(t *testing.T) {
	d, fc := fitDronesDispatch(t)
	fc.installModErrUntilStrip = errors.New("insufficient CPU")
	fc.raw = map[string][]byte{"ship": shipJSON([2]string{"inst-1", "survey_scanner_i"})}

	if err := d.FitDrones(context.Background(), "mine_asteroid", 1, 2, "", "", FitDronesOpts{Deploy: false, Strip: true}); err != nil {
		t.Fatalf("FitDrones with strip: %v", err)
	}
	got := strings.Join(fc.calls, ",")
	// It must uninstall the INSTANCE id, then retry the same install.
	if !strings.Contains(got, "uninstall_mod:inst-1") {
		t.Errorf("did not strip the default module; calls: %v", fc.calls)
	}
	if n := strings.Count(got, "install_mod:advanced_drone_bay"); n != 2 {
		t.Errorf("install attempted %d times, want 2 (fail, strip, retry); calls: %v", n, fc.calls)
	}
	if !strings.Contains(got, "load_drone") {
		t.Error("fitting did not continue past the install")
	}
}

// Without the flag the original behaviour stands: report the full rack, strip
// nothing.
func TestFitDronesWithoutStripNeverUninstalls(t *testing.T) {
	d, fc := fitDronesDispatch(t)
	fc.installModErrUntilStrip = errors.New("insufficient CPU")
	fc.raw = map[string][]byte{"ship": shipJSON([2]string{"inst-1", "survey_scanner_i"})}

	err := d.FitDrones(context.Background(), "mine_asteroid", 1, 2, "", "", FitDronesOpts{Deploy: false, Strip: false})
	if err == nil {
		t.Fatal("expected the install to fail")
	}
	if strings.Contains(strings.Join(fc.calls, ","), "uninstall_mod") {
		t.Errorf("stripped a module without the flag; calls: %v", fc.calls)
	}
}

// Two or more fitted modules is a deliberately fitted hull: we cannot tell which
// one blocks the bay, and stripping the wrong one is silent and expensive.
func TestFitDronesStripRefusesWhenSeveralModulesFitted(t *testing.T) {
	d, fc := fitDronesDispatch(t)
	fc.installModErrUntilStrip = errors.New("insufficient CPU")
	fc.raw = map[string][]byte{"ship": shipJSON(
		[2]string{"inst-1", "survey_scanner_i"},
		[2]string{"inst-2", "cargo_expander_iii"},
	)}

	err := d.FitDrones(context.Background(), "mine_asteroid", 1, 2, "", "", FitDronesOpts{Deploy: false, Strip: true})
	if err == nil {
		t.Fatal("expected a refusal when several modules are fitted")
	}
	if !strings.Contains(err.Error(), "refusing to guess") {
		t.Errorf("error should say it refused to guess: %v", err)
	}
	if strings.Contains(strings.Join(fc.calls, ","), "uninstall_mod") {
		t.Errorf("stripped a module despite ambiguity; calls: %v", fc.calls)
	}
}

// A bay already installed by an earlier partial run must not be counted as the
// blocker and stripped.
func TestFitDronesStripIgnoresAlreadyInstalledBays(t *testing.T) {
	d, fc := fitDronesDispatch(t)
	fc.installModErrUntilStrip = errors.New("insufficient CPU")
	fc.raw = map[string][]byte{"ship": shipJSON(
		[2]string{"bay-1", "advanced_drone_bay"},
		[2]string{"inst-1", "survey_scanner_i"},
	)}

	if err := d.FitDrones(context.Background(), "mine_asteroid", 1, 1, "", "", FitDronesOpts{Deploy: false, Strip: true}); err != nil {
		t.Fatalf("FitDrones: %v", err)
	}
	got := strings.Join(fc.calls, ",")
	if strings.Contains(got, "uninstall_mod:bay-1") {
		t.Errorf("stripped the drone bay it had installed; calls: %v", fc.calls)
	}
	if !strings.Contains(got, "uninstall_mod:inst-1") {
		t.Errorf("did not strip the actual blocker; calls: %v", fc.calls)
	}
}

// Regression, found live 2026-09-08: load_drone is action_result-wrapped, so its
// immediate reply is "pending" and carries NO drone_id. Reading the id inline
// failed every fit at drone 1/5. Ids must come from the get_drones roster, read
// after ALL loads.
func TestFitDronesReadsDroneIDsFromRosterNotLoadReply(t *testing.T) {
	d, fc := fitDronesDispatch(t)

	if err := d.FitDrones(context.Background(), "mine_asteroid", 1, 3, "", "", FitDronesOpts{Deploy: false, Strip: false}); err != nil {
		t.Fatalf("FitDrones: %v", err)
	}

	got := strings.Join(fc.calls, ",")
	if !strings.Contains(got, "get_drones") {
		t.Fatalf("never read the roster; calls: %v", fc.calls)
	}
	// Every load must precede the roster read, and every upload follow it.
	roster := strings.Index(got, "get_drones")
	if last := strings.LastIndex(got, "load_drone"); last > roster {
		t.Errorf("loaded a drone after reading the roster; calls: %v", fc.calls)
	}
	if first := strings.Index(got, "upload_script"); first < roster {
		t.Errorf("uploaded before reading the roster; calls: %v", fc.calls)
	}
	if n := strings.Count(got, "upload_script"); n != 3 {
		t.Errorf("uploaded %d scripts, want 3; calls: %v", n, fc.calls)
	}
}

// A roster that comes back empty must fail loudly: the drones are loaded but
// scriptless, which would otherwise look like a clean fit and mine nothing.
func TestFitDronesFailsWhenRosterIsEmpty(t *testing.T) {
	d, fc := fitDronesDispatch(t)
	fc.raw = map[string][]byte{"drones": []byte(`{"drones":[]}`)}
	fc.suppressRoster = true

	err := d.FitDrones(context.Background(), "mine_asteroid", 1, 2, "", "", FitDronesOpts{Deploy: false, Strip: false})
	if err == nil {
		t.Fatal("expected an error when the roster lists no drones")
	}
	if !strings.Contains(err.Error(), "roster lists none") {
		t.Errorf("unclear error: %v", err)
	}
}

// Resume completes a half-finished fit instead of restarting it. The live
// failures on 2026-09-08 left hulls with the bay installed and some drones
// loaded; re-running from the top asked storage for a second bay it did not
// have.
func TestFitDronesResumeSkipsWhatIsAlreadyFitted(t *testing.T) {
	d, fc := fitDronesDispatch(t)
	// One bay fitted, two of the five drones already loaded.
	fc.raw = map[string][]byte{"ship": shipJSON([2]string{"bay-1", "advanced_drone_bay"})}
	fc.droneSeq = 2

	err := d.FitDrones(context.Background(), "mine_asteroid", 1, 5, "", "",
		FitDronesOpts{Resume: true})
	if err != nil {
		t.Fatalf("FitDrones resume: %v", err)
	}

	got := strings.Join(fc.calls, ",")
	if strings.Contains(got, "withdraw:advanced_drone_bay") {
		t.Errorf("withdrew a second bay; calls: %v", fc.calls)
	}
	if strings.Contains(got, "install_mod") {
		t.Errorf("re-installed an existing bay; calls: %v", fc.calls)
	}
	// Only the 3 missing drones get loaded, but all 5 get scripted.
	if n := strings.Count(got, "load_drone"); n != 3 {
		t.Errorf("loaded %d drones, want 3 (5 asked, 2 present); calls: %v", n, fc.calls)
	}
	if n := strings.Count(got, "upload_script"); n != 5 {
		t.Errorf("scripted %d drones, want all 5; calls: %v", n, fc.calls)
	}
}

// Drones a failed run already pulled into cargo must not be withdrawn twice --
// storage no longer holds them.
func TestFitDronesResumeDoesNotRewithdrawCargo(t *testing.T) {
	d, fc := fitDronesDispatch(t)
	fc.raw = map[string][]byte{"ship": shipJSON([2]string{"bay-1", "advanced_drone_bay"})}
	fc.droneSeq = 1
	fc.state.Ship.Cargo = []game.CargoItem{{ItemID: "mining_drone", Quantity: 4}}

	if err := d.FitDrones(context.Background(), "mine_asteroid", 1, 5, "", "",
		FitDronesOpts{Resume: true}); err != nil {
		t.Fatalf("FitDrones resume: %v", err)
	}
	if strings.Contains(strings.Join(fc.calls, ","), "withdraw:mining_drone") {
		t.Errorf("re-withdrew drones already in cargo; calls: %v", fc.calls)
	}
}

// Without Resume the behaviour is unchanged: a full fit from scratch.
func TestFitDronesWithoutResumeFitsFromScratch(t *testing.T) {
	d, fc := fitDronesDispatch(t)

	if err := d.FitDrones(context.Background(), "mine_asteroid", 1, 2, "", "",
		FitDronesOpts{}); err != nil {
		t.Fatalf("FitDrones: %v", err)
	}
	got := strings.Join(fc.calls, ",")
	if !strings.Contains(got, "withdraw:advanced_drone_bay:1") || !strings.Contains(got, "install_mod") {
		t.Errorf("did not fit from scratch; calls: %v", fc.calls)
	}
}
