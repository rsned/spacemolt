package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
)

// DefaultDroneScriptDir holds the DroneLang templates fit_drones uploads.
const DefaultDroneScriptDir = "data/scripts/drones"

// droneScriptMaxChars is the server's upload limit. Checked before upload so a
// too-long script fails locally with a clear message instead of consuming a
// mutation tick to earn a parse error.
const droneScriptMaxChars = 2000

// loadDroneScript reads <dir>/<name>.ds and substitutes $TOKEN$s from live
// state. Resolution happens HERE, not in the drone: DroneLang actions take a
// quoted string literal, so mem("STATION") cannot supply a MOVE target -- one
// template plus upload-time substitution is the only way to get a single
// maintained file across many stations.
//
// $STATION$ / $ASTEROID_BELT$ / $ICE_FIELD$ / $GAS_CLOUD$ resolve out of
// state.System.POIs and yield POI ids. (Since v0.598.3 DroneLang also accepts
// base ids, but a POI id is correct for every other command, so tokens stay the
// safer source.)
func loadDroneScript(dir, name string, state *game.State) (string, error) {
	if dir == "" {
		dir = DefaultDroneScriptDir
	}
	if strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("fit_drones: script name %q must not contain a path separator", name)
	}
	raw, err := os.ReadFile(filepath.Join(dir, name+".ds"))
	if err != nil {
		return "", fmt.Errorf("fit_drones: read script %q: %w", name, err)
	}
	out, err := ResolveTokens([]string{string(raw)}, state)
	if err != nil {
		return "", fmt.Errorf("fit_drones: resolve tokens in %q: %w", name, err)
	}
	script := out[0]
	if n := len(script); n > droneScriptMaxChars {
		return "", fmt.Errorf("fit_drones: script %q is %d chars after substitution, limit %d", name, n, droneScriptMaxChars)
	}
	if strings.Contains(script, "$") {
		return "", fmt.Errorf("fit_drones: script %q still has an unresolved $TOKEN$ after substitution", name)
	}
	return script, nil
}

// droneIDFromLoad pulls drone_id out of a load_drone reply (LoadDroneResponse).
func droneIDFromLoad(raw []byte) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("fit_drones: load_drone returned no payload")
	}
	var r struct {
		DroneID string `json:"drone_id"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", fmt.Errorf("fit_drones: decode load_drone reply: %w", err)
	}
	if r.DroneID == "" {
		return "", fmt.Errorf("fit_drones: load_drone reply carried no drone_id")
	}
	return r.DroneID, nil
}

// FitDrones turns the agent's active ship into a drone platform: withdraw bays
// and drones from THIS station's storage, install the bays, then load each
// drone and give it SCRIPT, and finally deploy if deploy is true.
//
// deploy=false stops after the scripts are uploaded, leaving the drones loaded
// and docked. That is the pre-travel case: a marketbot fitted at its origin must
// carry its drones to the destination and launch them THERE, because deploying
// here would put them to work in the wrong system moments before the ship jumps
// out. Pair it with a pre-rendered script -- $TOKEN$s resolve against the
// CURRENT system, which is not where the drones will fly.
//
// Deliberately NOT done: uninstalling an existing module to free a slot. The
// manual runbook does that, but uninstall_mod takes a module *instance* id and
// stripping the wrong one is silent and expensive, so a full utility rack is
// reported rather than cleared.
//
// Materials must already be in station storage -- withdraw_items only sees the
// local station, so this runs downstream of delivering bays and drones.
func (d *WorkerDispatch) FitDrones(ctx context.Context, script string, bays, drones int, bayItem, droneItem string, deploy bool) error {
	if bays < 1 || drones < 1 {
		return fmt.Errorf("fit_drones: bays and drones must be >= 1, got %d/%d", bays, drones)
	}
	if bayItem == "" {
		bayItem = "advanced_drone_bay"
	}
	if droneItem == "" {
		droneItem = "mining_drone"
	}

	// Resolve the script BEFORE spending any mutation: a bad token or an
	// over-length script should cost nothing.
	// "" -> DefaultDroneScriptDir. Deliberately NOT d.AgentsDir, which is
	// data/agents (credentials), not the script dir.
	body, err := loadDroneScript("", script, d.Client.GetState())
	if err != nil {
		return err
	}

	if err := d.Client.WithdrawItems(ctx, bayItem, float64(bays)); err != nil {
		return fmt.Errorf("fit_drones: withdraw %d %s: %w", bays, bayItem, err)
	}
	for i := range bays {
		if err := d.Client.InstallMod(ctx, bayItem); err != nil {
			return fmt.Errorf("fit_drones: install %s %d/%d (utility slot or CPU full?): %w", bayItem, i+1, bays, err)
		}
		time.Sleep(game.SleepQuick)
	}

	if err := d.Client.WithdrawItems(ctx, droneItem, float64(drones)); err != nil {
		return fmt.Errorf("fit_drones: withdraw %d %s: %w", drones, droneItem, err)
	}

	// load_drone and upload_drone_script are both PER DRONE: each load returns
	// the id the matching upload needs.
	loaded := 0
	for i := range drones {
		if err := d.Client.LoadDrone(ctx, droneItem); err != nil {
			return fmt.Errorf("fit_drones: load %s %d/%d: %w", droneItem, i+1, drones, err)
		}
		id, err := droneIDFromLoad(d.Client.GetRawJSON("_last"))
		if err != nil {
			return fmt.Errorf("fit_drones: drone %d/%d: %w", i+1, drones, err)
		}
		if err := d.Client.UploadDroneScript(ctx, id, body); err != nil {
			return fmt.Errorf("fit_drones: upload script to drone %s: %w", id, err)
		}
		loaded++
		time.Sleep(game.SleepQuick)
	}

	if !deploy {
		fmt.Fprintf(d.Out, "fit_drones: %d bay(s), %d drone(s) loaded and scripted %q, NOT deployed\n", bays, drones, script) //nolint:errcheck
		return nil
	}

	// Deploy needs open space; re-dock after so the agent resumes its role.
	if err := d.Client.Undock(ctx); err != nil {
		return fmt.Errorf("fit_drones: undock before deploy (%d drone(s) loaded, scripts uploaded): %w", loaded, err)
	}
	if err := d.Client.DeployDrone(ctx, "", true); err != nil {
		return fmt.Errorf("fit_drones: deploy: %w", err)
	}
	if err := d.Client.Dock(ctx); err != nil {
		return fmt.Errorf("fit_drones: re-dock after deploy (drones ARE deployed): %w", err)
	}
	fmt.Fprintf(d.Out, "fit_drones: %d bay(s), %d drone(s) deployed on script %q\n", bays, drones, script) //nolint:errcheck
	return nil
}
