package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// collectMinUnits is the fewest units of gifted parts worth a collection trip.
const collectMinUnits = 50

// CollectDeps wires the collect_gifts command.
type CollectDeps struct {
	Client game.GameClient
	KB     knowledge.Base
	Out    io.Writer // nil -> io.Discard
	// AgentsDir is where credential files live, for resolving the recipient's
	// username (empty -> DefaultAgentsDir).
	AgentsDir string
	// nav flies to (system, poi); nil -> Autopilot with the KB's route gates.
	nav func(ctx context.Context, system, poi string) error
	// usernameFor resolves an agent id to its username (nil -> UsernameFor).
	usernameFor func(agentID string) (string, error)
}

// CollectGifts is the collector hauler's run: hunters gift banked creature
// parts into its storage at their base stations (huntBankSurplus), and this
// carries them to Conjunction Junction and gifts them to haulDumpRecipient,
// whose faction_deposit_all sweep moves them into faction storage.
//
// stations are the hunter base stations to check (base or POI ids). Each is
// read with view_storage station_id, which needs no dock; the fullest one, if
// it holds at least collectMinUnits, is visited and emptied into cargo. The
// view_storage index form (item_count per location) is not used: it counts
// every item the collector holds anywhere, not just parts at hunter bases.
//
// Any non-reserved cargo already aboard -- a run whose CJ leg failed -- is
// delivered first, before collecting more. Fuel, repair and ammo stock is
// never collected or gifted (huntBankIsReserved).
func CollectGifts(ctx context.Context, deps CollectDeps, stations []string) error {
	if len(stations) == 0 {
		return errors.New("collect_gifts: want STATION..., got none")
	}
	out := deps.Out
	if out == nil {
		out = io.Discard
	}
	if deps.nav == nil {
		deps.nav = func(ctx context.Context, system, poi string) error {
			return Autopilot(ctx, AutopilotDeps{
				Client: deps.Client, Out: out, KB: deps.KB,
				OnWaypoint: func(ctx context.Context) error { return KBWaypointCapture(ctx, deps.Client, deps.KB) },
			}, system, poi)
		}
	}
	if err := deps.Client.GetStatus(ctx); err != nil {
		return fmt.Errorf("collect_gifts: get_status: %w", err)
	}
	if aboard := collectCargoParts(ctx, deps); len(aboard) > 0 {
		fmt.Fprintf(out, "collect_gifts: %d part type(s) already aboard; delivering first\n", len(aboard)) //nolint:errcheck
		return collectDeliver(ctx, deps, out)
	}

	best, bestUnits := "", 0
	var bestHeld map[string]int
	for _, station := range stations {
		if err := deps.Client.ViewStorageAt(ctx, station); err != nil {
			fmt.Fprintf(out, "collect_gifts: view_storage %s: %v; skipping\n", station, err) //nolint:errcheck
			continue
		}
		held, ok := consignHeld(deps.Client.GetRawJSON("storage"))
		if !ok {
			fmt.Fprintf(out, "collect_gifts: view_storage %s: no storage frame; skipping\n", station) //nolint:errcheck
			continue
		}
		parts, units := huntBankableParts(ctx, deps.KB, held, nil)
		if units > bestUnits {
			best, bestUnits = station, units
			bestHeld = make(map[string]int, len(parts))
			for _, id := range parts {
				bestHeld[id] = held[id]
			}
		}
	}
	if bestUnits < collectMinUnits {
		fmt.Fprintf(out, "collect_gifts: fullest station holds %d unit(s), under %d; nothing to collect\n", bestUnits, collectMinUnits) //nolint:errcheck
		return nil
	}

	fmt.Fprintf(out, "collect_gifts: collecting %d unit(s) at %s\n", bestUnits, best) //nolint:errcheck
	if err := collectGoTo(ctx, deps, best); err != nil {
		return fmt.Errorf("collect_gifts: to %s: %w", best, err)
	}
	ids := make([]string, 0, len(bestHeld))
	for id := range bestHeld {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	took := 0
	for _, id := range ids {
		// The server refuses what the hold cannot take; the rest waits for
		// the next run.
		if err := deps.Client.WithdrawItems(ctx, id, float64(bestHeld[id])); err != nil {
			fmt.Fprintf(out, "collect_gifts: withdraw %d %s: %v\n", bestHeld[id], id, err) //nolint:errcheck
			continue
		}
		took++
	}
	if took == 0 {
		return fmt.Errorf("collect_gifts: withdrew nothing at %s", best)
	}
	return collectDeliver(ctx, deps, out)
}

// collectCargoParts returns the non-reserved cargo aboard.
func collectCargoParts(ctx context.Context, deps CollectDeps) []game.CargoItem {
	if err := deps.Client.GetCargo(ctx); err != nil {
		return nil
	}
	st := deps.Client.GetState()
	if st == nil {
		return nil
	}
	var parts []game.CargoItem
	for _, c := range st.Ship.Cargo {
		if int(c.Quantity) > 0 && !huntBankIsReserved(ctx, deps.KB, c.ItemID) {
			parts = append(parts, c)
		}
	}
	return parts
}

// collectGoTo flies to station (a base or POI id) and docks there.
func collectGoTo(ctx context.Context, deps CollectDeps, station string) error {
	if st := deps.Client.GetState(); st != nil && huntAtStation(ctx, deps.KB, st, station) {
		return nil
	}
	system := ""
	if station == haulDumpPOI {
		system = haulDumpSystem
	} else {
		route, err := deps.Client.FindRoute(ctx, station)
		if err != nil {
			return fmt.Errorf("route: %w", err)
		}
		if len(route) > 0 {
			system = route[len(route)-1].SystemID
		}
		if system == "" {
			return errors.New("router returned no system")
		}
	}
	if st := deps.Client.GetState(); st != nil && st.Doc {
		if err := deps.Client.Undock(ctx); err != nil {
			return fmt.Errorf("undock: %w", err)
		}
	}
	if err := deps.nav(ctx, system, station); err != nil {
		return fmt.Errorf("transit: %w", err)
	}
	if err := deps.Client.Dock(ctx); err != nil && !strings.Contains(err.Error(), "Already docked") {
		return fmt.Errorf("dock: %w", err)
	}
	return nil
}

// collectDeliver flies the parts aboard to Conjunction Junction and gifts each
// to haulDumpRecipient. A refused gift is reported; the rest still go.
func collectDeliver(ctx context.Context, deps CollectDeps, out io.Writer) error {
	recipientOf := deps.usernameFor
	if recipientOf == nil {
		recipientOf = func(id string) (string, error) { return UsernameFor(deps.AgentsDir, id) }
	}
	recipient, err := recipientOf(haulDumpRecipient)
	if err != nil {
		return fmt.Errorf("collect_gifts: recipient: %w", err)
	}
	if err := collectGoTo(ctx, deps, haulDumpPOI); err != nil {
		return fmt.Errorf("collect_gifts: to %s: %w", haulDumpSystem, err)
	}
	var failed []error
	for _, c := range collectCargoParts(ctx, deps) {
		qty := int(c.Quantity)
		if err := deps.Client.SendGift(ctx, map[string]any{
			"recipient": recipient, "item_id": c.ItemID, "quantity": qty, "source": "cargo",
		}); err != nil {
			failed = append(failed, fmt.Errorf("gift %s x%d: %w", c.ItemID, qty, err))
			continue
		}
		fmt.Fprintf(out, "collect_gifts: gave %d %s to %s at %s\n", qty, c.ItemID, haulDumpRecipient, haulDumpSystem) //nolint:errcheck
	}
	if len(failed) > 0 {
		return fmt.Errorf("collect_gifts: %d gift(s) refused: %w", len(failed), errors.Join(failed...))
	}
	return nil
}
