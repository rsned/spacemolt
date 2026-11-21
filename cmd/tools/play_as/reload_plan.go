package main

import (
	"context"
	"fmt"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
	"github.com/rsned/spacemolt/pkg/reload"
)

// `reload` takes two ids the pilot has no way to produce mid-fight: the
// weapon's MODULE INSTANCE id from get_ship, and the item id of an ammo type
// that fits it. explorer-8 hit an empty magazine inside a creature battle and
// the only answer the REPL gave was the usage string.
//
// Both halves are already knowable. get_ship's modules carry id, ammo_type,
// magazine_size and current_ammo; the catalog pairs a weapon's ammo_type with
// an ammo item's effect.subtype (missile_launcher_i/ii -> missile ->
// standard_guided_missiles, railgun -> ferrous_slug_case, autocannon ->
// standard_rounds_box, and so on). This resolves the two against each other.

// The resolver lives in pkg/reload so the worker fleet shares it; these aliases
// keep the REPL's own names.
type (
	reloadAmmoCandidate = reload.Candidate
	reloadPlanEntry     = reload.Entry
	reloadTarget        = reload.Target
	ammoLookup          = reload.Lookup
)

func buildReloadPlan(modules []serverapi.ShipModule, cargo []serverapi.CargoItem, lookup ammoLookup) []reloadPlanEntry {
	return reload.BuildPlan(modules, cargo, lookup)
}

func reloadAllTargets(plan []reloadPlanEntry) []reloadTarget { return reload.Targets(plan) }

func kbAmmoLookup(ctx context.Context, kb knowledge.Base) ammoLookup { return reload.KBLookup(ctx, kb) }

// fetchReloadPlan refreshes get_ship and resolves its weapons against cargo.
func fetchReloadPlan(ctx context.Context, client game.GameClient) ([]reloadPlanEntry, error) {
	return reload.Fetch(ctx, client, globalKB)
}

// ammo-using weapon, its magazine state, and the exact command to fill it.
func formatReloadPlan(plan []reloadPlanEntry) []string {
	if len(plan) == 0 {
		return []string{"No fitted weapon takes ammo (energy weapons need no reload)."}
	}

	lines := []string{"Fitted weapons that take ammo (one reload per module, one tick each):"}
	for _, e := range plan {
		name := e.WeaponName
		if name == "" {
			name = e.WeaponTypeID
		}
		head := fmt.Sprintf("  %s (%s) — %d/%d %s", name, e.WeaponTypeID, e.CurrentAmmo, e.MagazineSize, e.AmmoType)
		switch {
		case e.Empty():
			head += "  EMPTY"
		case e.Full():
			head += "  full"
		}
		lines = append(lines, head)

		if len(e.Candidates) == 0 {
			lines = append(lines, fmt.Sprintf("    no %s ammo in cargo", e.AmmoType))
			continue
		}
		best := e.Candidates[0]
		label := best.Name
		if label == "" {
			label = best.ItemID
		}
		lines = append(lines, fmt.Sprintf("    reload %s %s   (%s %s in cargo)",
			e.WeaponID, best.ItemID, num(best.Quantity), label))
		for _, alt := range e.Candidates[1:] {
			altLabel := alt.Name
			if altLabel == "" {
				altLabel = alt.ItemID
			}
			lines = append(lines, fmt.Sprintf("    or: reload %s %s   (%s %s in cargo)",
				e.WeaponID, alt.ItemID, num(alt.Quantity), altLabel))
		}
	}
	lines = append(lines, "Run `reload all` to reload every empty weapon in sequence.")
	return lines
}

// showReloadPlan answers a bare `reload`.
func showReloadPlan(ctx context.Context, client game.GameClient) error {
	plan, err := fetchReloadPlan(ctx, client)
	if err != nil {
		return err
	}
	for _, line := range formatReloadPlan(plan) {
		fmt.Println(line)
	}
	if globalKB == nil {
		fmt.Println("(no knowledge base: run with --db-path to resolve ammo types from the catalog)")
	}
	return nil
}

// reloadAll reloads every dry weapon that has ammo aboard. The server charges
// a tick per module, so these are issued in sequence, not fired off together.
func reloadAll(ctx context.Context, client game.GameClient, format outputFormat) error {
	plan, err := fetchReloadPlan(ctx, client)
	if err != nil {
		return err
	}
	targets := reloadAllTargets(plan)
	if len(targets) == 0 {
		fmt.Println("Nothing to reload: no weapon is both empty and has matching ammo in cargo.")
		for _, line := range formatReloadPlan(plan) {
			fmt.Println(line)
		}
		return nil
	}

	var failed int
	for i, t := range targets {
		fmt.Printf("[%d/%d] reload %s (%s) ← %s\n", i+1, len(targets), t.WeaponName, t.WeaponID, t.AmmoItemID)
		cmd := fmt.Sprintf("reload %s %s", t.WeaponID, t.AmmoItemID)
		err := simpleCommand(client, func(ctx context.Context) error {
			return client.Reload(ctx, t.WeaponID, t.AmmoItemID)
		}, ctx, 3*time.Second, cmd, format)
		if err != nil {
			failed++
			fmt.Printf("   failed: %v\n", err)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d reloads failed", failed, len(targets))
	}
	return nil
}
