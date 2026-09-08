package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// droneIDsFromRoster pulls the ids of every drone whose type matches itemID
// out of a get_drones reply.
//
// This replaces reading the id from load_drone's own reply, which cannot work:
// load_drone is action_result-wrapped and answers "pending" first, so the id is
// not there yet. Matching is a substring on type so "mining_drone" catches the
// server's variants, mirroring bulk_upload_drone_script's --type filter.
func droneIDsFromRoster(raw []byte, itemID string) ([]string, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("get_drones returned no payload")
	}
	var resp struct {
		Drones []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"drones"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode drone roster: %w", err)
	}
	want := strings.ToLower(itemID)
	ids := make([]string, 0, len(resp.Drones))
	seen := make([]string, 0, len(resp.Drones))
	for _, dr := range resp.Drones {
		if dr.ID == "" {
			continue
		}
		got := strings.ToLower(dr.Type)
		seen = append(seen, dr.Type)
		// Match EITHER direction. The roster's type and the storage item id are
		// not the same string -- an item "mining_drone" can list as type
		// "mining" -- and a one-way Contains silently matches nothing, which
		// reads as "no drones loaded" when five are sitting in the bay.
		if want != "" && !strings.Contains(got, want) && !strings.Contains(want, got) {
			continue
		}
		ids = append(ids, dr.ID)
	}
	if len(ids) == 0 && len(seen) > 0 {
		return nil, fmt.Errorf("roster has %d drone(s) but none match %q (types seen: %s)",
			len(seen), itemID, strings.Join(seen, ", "))
	}
	return ids, nil
}

// FitDrones turns the agent's active ship into a drone platform: withdraw bays
// installedModule is one fitted module as get_ship reports it: ID is the
// INSTANCE id uninstall_mod needs, TypeID the catalogue id worth logging.
type installedModule struct {
	ID     string `json:"id"`
	TypeID string `json:"type_id"`
	Name   string `json:"name"`
}

// shipModules decodes the fitted-module list from the last get_ship reply.
func shipModules(raw []byte) ([]installedModule, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("get_ship returned no payload")
	}
	var resp struct {
		Modules []installedModule `json:"modules"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode ship modules: %w", err)
	}
	return resp.Modules, nil
}

// freeModuleSlot removes the single fitted module blocking a bay install, and
// reports what it stripped.
//
// A fresh account's hull carries exactly ONE default module, and on a starter
// prospect there is not enough CPU or power to run it alongside an
// advanced_drone_bay -- so fitting a new account ALWAYS needs it gone. That is
// the entire justification for uninstalling here, and it is why the rule is
// "exactly one": with one module there is nothing to choose, the only candidate
// is the default. Two or more means somebody fitted this hull deliberately, we
// cannot tell which module is in the way, and stripping the wrong one is silent
// and expensive -- so that case refuses and changes nothing.
//
// Modules already of bayItem's type are not counted as blockers: re-running a
// partial fit must not strip the bay it just installed.
func (d *WorkerDispatch) freeModuleSlot(ctx context.Context, bayItem string) (installedModule, error) {
	var none installedModule
	if err := d.Client.GetShip(ctx); err != nil {
		return none, fmt.Errorf("get_ship: %w", err)
	}
	mods, err := shipModules(d.Client.GetRawJSON("ship"))
	if err != nil {
		return none, err
	}

	blockers := make([]installedModule, 0, len(mods))
	for _, m := range mods {
		if m.TypeID == bayItem {
			continue
		}
		blockers = append(blockers, m)
	}

	switch len(blockers) {
	case 0:
		return none, fmt.Errorf("no removable module found (fitted: %d); the install failed for another reason", len(mods))
	case 1:
		// fall through
	default:
		names := make([]string, 0, len(blockers))
		for _, m := range blockers {
			names = append(names, fmt.Sprintf("%s(%s)", m.TypeID, m.ID))
		}
		return none, fmt.Errorf("%d modules fitted, refusing to guess which blocks the bay: %s", len(blockers), strings.Join(names, " "))
	}

	victim := blockers[0]
	if err := d.Client.UninstallMod(ctx, victim.ID); err != nil {
		return none, fmt.Errorf("uninstall %s (%s): %w", victim.TypeID, victim.ID, err)
	}
	return victim, nil
}

// FitDronesOpts are the switches on a fit-out. They are a struct rather than
// trailing parameters because Deploy, Strip and Resume are three adjacent bools
// and a transposed pair would silently do the wrong thing on a live account.
type FitDronesOpts struct {
	// Deploy launches the drones at the end. False leaves them stowed for a
	// ship that still has to travel to the post it will mine.
	Deploy bool
	// Strip allows ONE blocking module to be uninstalled; see freeModuleSlot.
	Strip bool
	// Resume fits only what is missing, reading the ship and drone roster first.
	// A fit that failed partway leaves a hull with its bay installed and some
	// drones loaded, and re-running from the top would try to withdraw a second
	// bay that storage does not have. With Resume the verb is idempotent: run it
	// again and it completes the fit rather than restarting it.
	Resume bool
}

// installedBays counts fitted modules of bayItem's type. Requires a fresh
// get_ship.
func installedBays(raw []byte, bayItem string) (int, error) {
	mods, err := shipModules(raw)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, m := range mods {
		if m.TypeID == bayItem {
			n++
		}
	}
	return n, nil
}

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
// strip=true allows ONE blocking module to be uninstalled if the bay will not
// install; see freeModuleSlot. Without it a full rack is reported, never
// cleared -- which was the original behaviour and is still the default, because
// stripping the wrong module is silent and expensive.
//
// Materials must already be in station storage -- withdraw_items only sees the
// local station, so this runs downstream of delivering bays and drones.
func (d *WorkerDispatch) FitDrones(ctx context.Context, script string, bays, drones int, bayItem, droneItem string, opts FitDronesOpts) error {
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

	// Resume reads what is already fitted and subtracts it. Everything below
	// then works on the SHORTFALL, so a clean hull runs exactly as before
	// (nothing fitted, shortfall == the full ask) and a half-fitted one is
	// completed instead of restarted.
	baysNeeded, dronesNeeded := bays, drones
	alreadyLoaded := 0
	if opts.Resume {
		if err := d.Client.GetShip(ctx); err != nil {
			return fmt.Errorf("fit_drones: resume: get_ship: %w", err)
		}
		haveBays, err := installedBays(d.Client.GetRawJSON("ship"), bayItem)
		if err != nil {
			return fmt.Errorf("fit_drones: resume: %w", err)
		}
		baysNeeded = max(bays-haveBays, 0)

		if err := d.Client.GetDrones(ctx); err != nil {
			return fmt.Errorf("fit_drones: resume: get_drones: %w", err)
		}
		ids, err := droneIDsFromRoster(d.Client.GetRawJSON("drones"), droneItem)
		if err != nil && !strings.Contains(err.Error(), "none match") {
			return fmt.Errorf("fit_drones: resume: %w", err)
		}
		alreadyLoaded = len(ids)
		dronesNeeded = max(drones-alreadyLoaded, 0)
		msg := fmt.Sprintf("fit_drones: resume: %d/%d bay(s), %d/%d drone(s) already fitted; doing %d bay(s), %d drone(s)\n",
			haveBays, bays, alreadyLoaded, drones, baysNeeded, dronesNeeded)
		fmt.Fprint(d.Out, msg) //nolint:errcheck
	}

	if baysNeeded > 0 {
		if err := d.Client.WithdrawItems(ctx, bayItem, float64(baysNeeded)); err != nil {
			return fmt.Errorf("fit_drones: withdraw %d %s: %w", baysNeeded, bayItem, err)
		}
	}
	stripped := false
	for i := range baysNeeded {
		err := d.Client.InstallMod(ctx, bayItem)
		if err != nil && opts.Strip && !stripped {
			// A fresh hull's default module leaves too little CPU/power for the
			// bay. Strip once, then retry this same install -- not the whole
			// loop, so an earlier bay stays installed.
			stripped = true
			victim, ferr := d.freeModuleSlot(ctx, bayItem)
			if ferr != nil {
				return fmt.Errorf("fit_drones: install %s %d/%d failed (%v) and could not free a slot: %w", bayItem, i+1, baysNeeded, err, ferr)
			}
			fmt.Fprintf(d.Out, "fit_drones: uninstalled %s (%s) to free CPU/power for %s\n", victim.TypeID, victim.ID, bayItem) //nolint:errcheck
			err = d.Client.InstallMod(ctx, bayItem)
		}
		if err != nil {
			return fmt.Errorf("fit_drones: install %s %d/%d (utility slot or CPU full?): %w", bayItem, i+1, baysNeeded, err)
		}
		settle(ctx, game.SleepQuick)
	}

	// Withdraw only what cargo does not already hold. A run that died after its
	// withdraw left the drones in the hold, and asking storage for them a second
	// time fails against stock that is no longer there.
	toWithdraw := dronesNeeded
	if opts.Resume {
		toWithdraw = max(dronesNeeded-int(cargoQty(d.Client.GetState(), droneItem)), 0)
	}
	if toWithdraw > 0 {
		if err := d.Client.WithdrawItems(ctx, droneItem, float64(toWithdraw)); err != nil {
			return fmt.Errorf("fit_drones: withdraw %d %s: %w", toWithdraw, droneItem, err)
		}
	}

	// Load every drone FIRST, then read the roster for their ids.
	//
	// The obvious shape -- load one drone, take its id from the reply, script it
	// -- does not work: load_drone is action_result-wrapped, so the immediate
	// reply is only "Action 'load_drone' pending. Will execute on next tick" and
	// the drone_id lands a tick later. Reading it inline always finds nothing.
	// get_drones reports the settled roster, which is also how the manual
	// bulk_upload_drone_script path works.
	loaded := alreadyLoaded
	for i := range dronesNeeded {
		if err := d.Client.LoadDrone(ctx, droneItem); err != nil {
			return fmt.Errorf("fit_drones: load %s %d/%d: %w", droneItem, i+1, dronesNeeded, err)
		}
		loaded++
		settle(ctx, game.SleepQuick)
	}

	// Let the last load settle before asking for the roster.
	if dronesNeeded > 0 {
		settle(ctx, game.SleepTick)
	}
	if err := d.Client.GetDrones(ctx); err != nil {
		return fmt.Errorf("fit_drones: get_drones after loading %d: %w", loaded, err)
	}
	ids, err := droneIDsFromRoster(d.Client.GetRawJSON("drones"), droneItem)
	if err != nil {
		return fmt.Errorf("fit_drones: %d drone(s) loaded but unscripted: %w", loaded, err)
	}
	if len(ids) == 0 {
		return fmt.Errorf("fit_drones: %d %s loaded but the roster lists none", loaded, droneItem)
	}
	for _, id := range ids {
		if err := d.Client.UploadDroneScript(ctx, id, body); err != nil {
			return fmt.Errorf("fit_drones: upload script to drone %s: %w", id, err)
		}
		settle(ctx, game.SleepQuick)
	}

	if !opts.Deploy {
		fmt.Fprintf(d.Out, "fit_drones: %d bay(s), %d drone(s) loaded and scripted %q, NOT deployed\n", bays, drones, script) //nolint:errcheck
		return nil
	}

	if err := d.LaunchDrones(ctx, ""); err != nil {
		return fmt.Errorf("fit_drones: %d drone(s) loaded, scripts uploaded: %w", loaded, err)
	}
	fmt.Fprintf(d.Out, "fit_drones: %d bay(s), %d drone(s) deployed on script %q\n", bays, drones, script) //nolint:errcheck
	return nil
}

// LaunchDrones undocks, deploys, and re-docks. droneID empty deploys ALL loaded
// drones; naming one deploys just that drone.
//
// This is the other half of fit_drones deploy=false: an agent fitted at its
// origin arrives with its drones loaded and scripted but stowed, and this is
// what puts them to work once it is docked at the post they will actually mine.
//
// Re-docking matters beyond tidiness -- a resident's whole role is to sit docked
// capturing market data, so a launch that left it in open space would park it
// outside the station indefinitely. The re-dock failure is called out loudly
// because the drones ARE already deployed at that point: the launch succeeded
// and only the agent's own position needs fixing, which is the opposite of what
// a bare error here would suggest.
func (d *WorkerDispatch) LaunchDrones(ctx context.Context, droneID string) error {
	all := droneID == ""
	if err := d.Client.Undock(ctx); err != nil {
		return fmt.Errorf("launch_drones: undock before deploy: %w", err)
	}
	if err := d.Client.DeployDrone(ctx, droneID, all); err != nil {
		return fmt.Errorf("launch_drones: deploy: %w", err)
	}
	if err := d.Client.Dock(ctx); err != nil {
		return fmt.Errorf("launch_drones: re-dock after deploy (drones ARE deployed; agent is adrift): %w", err)
	}
	target := "all loaded drone(s)"
	if !all {
		target = droneID
	}
	fmt.Fprintf(d.Out, "launch_drones: deployed %s\n", target) //nolint:errcheck
	return nil
}
