package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// DefaultAgentsDir is WorkerDispatch's default location for agent credential
// files (data/agents/<agent-id>/credentials.json), used by Deliver to resolve
// a gift recipient's agent id to the in-game username send_gift requires.
const DefaultAgentsDir = "data/agents"

// UsernameFor resolves agentID to its in-game username by reading
// <agentsDir>/<agentID>/credentials.json (the same file every worker/agent
// process already carries its own login credentials in). agentsDir defaults
// to DefaultAgentsDir when empty.
func UsernameFor(agentsDir, agentID string) (string, error) {
	if agentsDir == "" {
		agentsDir = DefaultAgentsDir
	}
	path := filepath.Join(agentsDir, agentID, "credentials.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("UsernameFor(%s): %w", agentID, err)
	}
	var creds struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(raw, &creds); err != nil {
		return "", fmt.Errorf("UsernameFor(%s): parse %s: %w", agentID, path, err)
	}
	if creds.Username == "" {
		return "", fmt.Errorf("UsernameFor(%s): %s has no username field", agentID, path)
	}
	return creds.Username, nil
}

// resolveBase returns (systemID, poiID) for a base id, mirroring the two-step
// lookup in cmd/tools/play_as/source_sql.go SystemOf: bases.poi_id -> pois,
// falling back to treating stationID as a poi id directly (some callers pass
// one). Requires *knowledge.SQLiteKB; other Base implementations error, since
// there is no SQL to run against them.
func resolveBase(ctx context.Context, kb knowledge.Base, stationID string) (string, string, error) {
	if stationID == "" {
		return "", "", errors.New("resolveBase: empty station id")
	}
	sqliteKB, ok := kb.(*knowledge.SQLiteKB)
	if !ok || sqliteKB == nil {
		return "", "", fmt.Errorf("resolveBase(%s): requires *knowledge.SQLiteKB, got %T", stationID, kb)
	}
	var poiID, sys string
	err := sqliteKB.DB().QueryRowContext(ctx, `
		SELECT b.poi_id, p.system_id FROM bases b JOIN pois p ON p.id = b.poi_id WHERE b.id = ?`, stationID).Scan(&poiID, &sys)
	if errors.Is(err, sql.ErrNoRows) {
		poiID = stationID
		err = sqliteKB.DB().QueryRowContext(ctx, `SELECT system_id FROM pois WHERE id = ?`, stationID).Scan(&sys)
	}
	if err != nil {
		return "", "", fmt.Errorf("resolveBase(%s): %w", stationID, err)
	}
	return sys, poiID, nil
}

// cargoCount returns the quantity of itemID currently in ship cargo, 0 if the
// state is nil or the item isn't carried.
func cargoCount(state *game.State, itemID string) int {
	if state == nil {
		return 0
	}
	for _, item := range state.Ship.Cargo {
		if item.ItemID == itemID {
			return int(item.Quantity)
		}
	}
	return 0
}

// cargoFreeSpace returns the ship's remaining cargo capacity as a unit count
// (matching the rest of the package's cargoFree convention — see haul.go).
func cargoFreeSpace(state *game.State) int {
	if state == nil {
		return 0
	}
	free := int(state.Ship.CargoCapacity - state.Ship.CargoUsed)
	if free < 0 {
		return 0
	}
	return free
}

// isShortSupplyErr reports whether a withdraw error is the server telling us
// the source didn't have enough of the item (not a real failure) — these are
// non-fatal: the caller re-reads cargo to learn what it actually got.
func isShortSupplyErr(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "not enough") || strings.Contains(s, "no such item") || strings.Contains(s, "insufficient")
}

// Deliver moves ITEM xQTY from the worker's own storage at FROM to RECIPIENT
// at TO. recipient "self" (or the worker's own agent id) means deposit into
// own storage at TO instead of gifting. Gifts are cargo-only (live-verified,
// 2026-07-10): the verb withdraws into cargo at FROM, then travels to TO and
// gifts/deposits from cargo, repeating in cargo-capacity-sized batches until
// qty is satisfied. If FROM held less than qty, it delivers what existed and
// returns nil with a short-delivery note written to d.Out instead of looping
// forever.
//
// from == "" means the goods are already in cargo (e.g. just-mined ore
// handed off by MineQty): the withdraw leg AND the FROM travel leg are both
// skipped entirely — Deliver goes straight to TO with whatever is aboard. If
// cargo holds less than qty in this mode, that is a shortfall (mirroring the
// "source exhausted" case below, since there is no source to make a second
// withdraw pass against): Deliver delivers what's on hand and returns nil.
func (d *WorkerDispatch) Deliver(ctx context.Context, itemID string, qty int, from, to, recipient string) error {
	if qty < 1 {
		return fmt.Errorf("deliver: qty must be >= 1, got %d", qty)
	}
	// Resolve the recipient before any travel or withdraw so a bad recipient
	// (e.g. missing credentials.json) fails immediately instead of after
	// goods have already been pulled out of storage. The resolved username is
	// reused for every chunk below instead of re-reading credentials.json
	// each time.
	username, err := resolveRecipientUsername(d, recipient)
	if err != nil {
		return fmt.Errorf("deliver: %w", err)
	}
	var fromSys, fromPOI string
	if from != "" {
		fromSys, fromPOI, err = resolveBase(ctx, d.KB, from)
		if err != nil {
			return fmt.Errorf("deliver: resolve source %q: %w", from, err)
		}
	}
	toSys, toPOI, err := resolveBase(ctx, d.KB, to)
	if err != nil {
		return fmt.Errorf("deliver: resolve destination %q: %w", to, err)
	}

	remaining := qty
	for remaining > 0 {
		carrying := cargoCount(d.Client.GetState(), itemID)
		short := false
		if carrying < remaining {
			var cargoFull bool
			if from == "" {
				// Nothing to withdraw from — cargo is already everything
				// there is. Deliver it and stop; another pass would make no
				// progress.
				short = true
			} else {
				carrying, short, cargoFull, err = d.deliverWithdraw(ctx, itemID, remaining-carrying, fromSys, fromPOI, from)
				if err != nil {
					return err
				}
			}
			if carrying == 0 {
				switch {
				case from == "":
					fmt.Fprintf(d.Out, "deliver: %s not in cargo — nothing to deliver (requested %d)\n", itemID, qty) //nolint:errcheck
				case cargoFull:
					fmt.Fprintf(d.Out, "deliver: %s cargo full — no room to withdraw at %s, delivered %d of %d requested\n", //nolint:errcheck
						itemID, from, qty-remaining, qty)
				default:
					fmt.Fprintf(d.Out, "deliver: %s exhausted at %s — delivered %d of %d requested\n", //nolint:errcheck
						itemID, from, qty-remaining, qty)
				}
				return nil
			}
		}

		// Cap at remaining: carrying can exceed remaining when the ship
		// walked in already holding leftover cargo from a prior run (the
		// withdraw block above is skipped in that case). Any pre-existing
		// surplus above qty must stay in cargo, not get gifted/deposited.
		deliverQty := min(carrying, remaining)

		if err := d.autopilotAndDock(ctx, toSys, toPOI); err != nil {
			return fmt.Errorf("deliver: travel to destination %q: %w", to, err)
		}
		if err := giftOrDeposit(ctx, d, itemID, deliverQty, recipient, username); err != nil {
			return fmt.Errorf("deliver: %w", err)
		}
		time.Sleep(game.SleepQuick)
		remaining -= deliverQty

		if short && remaining > 0 {
			// The withdraw pass that fed this delivery came up short of what
			// we asked for — another round trip to FROM would make no
			// progress, so stop here instead of looping forever.
			fmt.Fprintf(d.Out, "deliver: %s short — delivered %d of %d, source exhausted\n", itemID, qty-remaining, qty) //nolint:errcheck
			return nil
		}
	}
	return nil
}

// deliverWithdraw travels to (system, poi), docks, and withdraws up to want
// units of itemID (capped by free cargo space) into cargo. A short-supply
// withdraw error is not fatal: it re-reads cargo to learn the actual amount
// received. Returns the total now carried, whether the pass came up short of
// want (the caller's progress guard for the round-trip loop), and — when
// nothing at all was withdrawn — whether that was because the cargo hold had
// zero free space (cargoFull) rather than the source being out of stock, so
// the caller can log an accurate reason.
func (d *WorkerDispatch) deliverWithdraw(ctx context.Context, itemID string, want int, system, poi, baseLabel string) (carrying int, short bool, cargoFull bool, err error) {
	if err := d.autopilotAndDock(ctx, system, poi); err != nil {
		return 0, false, false, fmt.Errorf("deliver: travel to source %q: %w", baseLabel, err)
	}
	before := cargoCount(d.Client.GetState(), itemID)
	free := cargoFreeSpace(d.Client.GetState())
	if want > free {
		want = free
	}
	if want <= 0 {
		return before, false, true, nil
	}
	werr := d.Client.WithdrawItems(ctx, itemID, float64(want))
	if werr != nil && !isShortSupplyErr(werr) {
		return 0, false, false, fmt.Errorf("deliver: withdraw %s at %s: %w", itemID, baseLabel, werr)
	}
	// The live client's parseActionResult has no case for "withdraw_items"
	// (unlike "deposit_items"), so a successful or short-supply withdraw does
	// NOT update state.Ship.Cargo on its own — an explicit refresh is required
	// before re-reading cargo below, or the short-source progress guard
	// under-counts what was actually withdrawn.
	if cerr := d.Client.GetCargo(ctx); cerr != nil {
		return 0, false, false, fmt.Errorf("deliver: refresh cargo after withdraw %s at %s: %w", itemID, baseLabel, cerr)
	}
	time.Sleep(game.SleepQuick)
	carrying = cargoCount(d.Client.GetState(), itemID)
	short = carrying-before < want
	if werr != nil {
		fmt.Fprintf(d.Out, "deliver: withdraw %s at %s came up short: %v\n", itemID, baseLabel, werr) //nolint:errcheck
	}
	return carrying, short, false, nil
}

// resolveRecipientUsername resolves recipient to the in-game username
// SendGift requires, up front — before any travel, withdraw, or buy — so a
// bad recipient (e.g. a missing/broken credentials.json) fails fast instead
// of after goods have already been moved or credits already spent. recipient
// "self" (or d's own agent id) needs no resolution and returns "". The
// returned username is reused for every chunk of the caller's delivery loop
// instead of re-resolving credentials.json on each iteration.
func resolveRecipientUsername(d *WorkerDispatch, recipient string) (string, error) {
	if recipient == "self" || recipient == d.AgentID {
		return "", nil
	}
	username, err := UsernameFor(d.agentsDir(), recipient)
	if err != nil {
		return "", fmt.Errorf("resolve recipient %q: %w", recipient, err)
	}
	return username, nil
}

// giftOrDeposit hands qty of itemID to recipient from the ship's current
// cargo. recipient "self" (or d's own agent id) deposits into the dispatch's
// own storage at the current (already-docked) location; any other recipient
// is sent a gift instead, addressed to username (resolved once up front by
// the caller via resolveRecipientUsername — ignored when recipient is
// self/own agent id). Cargo-only — the caller must already be docked at the
// handoff location before calling this. Shared by Deliver and BuyDirected,
// the two verbs that hand cargo off to a recipient.
func giftOrDeposit(ctx context.Context, d *WorkerDispatch, itemID string, qty int, recipient, username string) error {
	if recipient == "self" || recipient == d.AgentID {
		if err := d.Client.DepositItems(ctx, itemID, float64(qty)); err != nil {
			return fmt.Errorf("deposit %s: %w", itemID, err)
		}
	} else {
		if err := d.Client.SendGift(ctx, map[string]any{
			"recipient": username,
			"item_id":   itemID,
			"quantity":  float64(qty),
		}); err != nil {
			return fmt.Errorf("gift %s to %s: %w", itemID, recipient, err)
		}
	}
	// The live client's parseActionResult has no "send_gift" case at all, and
	// its "deposit_items" case only updates CargoCapacity (not the cargo item
	// list), so a successful hand-off does NOT remove the delivered/gifted
	// units from state.Ship.Cargo on its own — an explicit refresh is
	// required before the caller's chunk loop re-reads cargo, or the next
	// iteration sees phantom stock that is no longer actually aboard (same
	// remedy already applied to withdraw and buy).
	if err := d.Client.GetCargo(ctx); err != nil {
		return fmt.Errorf("refresh cargo after handoff of %s: %w", itemID, err)
	}
	return nil
}

// autopilotAndDock routes to (system, poi) via Autopilot and docks — the
// shared "get to a base and dock" step both the source and destination hops
// of Deliver use.
func (d *WorkerDispatch) autopilotAndDock(ctx context.Context, system, poi string) error {
	if err := Autopilot(ctx, AutopilotDeps{Client: d.Client, Out: d.Out}, system, poi); err != nil {
		return err
	}
	return d.Client.Dock(ctx)
}

// agentsDir returns the configured agent-credentials directory, defaulting to
// DefaultAgentsDir when unset.
func (d *WorkerDispatch) agentsDir() string {
	if d.AgentsDir != "" {
		return d.AgentsDir
	}
	return DefaultAgentsDir
}
