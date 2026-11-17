package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
	"github.com/rsned/spacemolt/pkg/knowledge"
)

// Surplus banking (operator, 2026-11-16): creature parts a hunter carries
// beyond its active Station Need feed the Dining/Leisure/Tourism recipes at
// Conjunction Junction. The hunter does not fly them there. When a pass finds
// it docked at its base station it banks them in personal storage, and once a
// batch has built up it gifts them -- straight out of storage -- to a collector
// hauler, whose collect_gifts run carries them to CJ. Docked at CJ itself (home
// hunting), the batch goes to haulDumpRecipient instead, whose
// faction_deposit_all sweep moves it into faction storage: the same path the
// haul leftovers take.
const (
	// huntGiftBatch is how many units of banked parts it takes to gift them.
	// Below it they wait in storage: a gift per kill would spend a mutation
	// tick on every pass for a handful of hides.
	huntGiftBatch = 50
)

// huntBankReserved are the operational consumables a hunter never banks: fuel
// and repair stock it burns itself. The rest -- ammunition, premium fuel
// cells, hull patches, sealants, shield charges -- is matched by catalog
// category (huntBankReservedCategories), because their ids follow no pattern.
var huntBankReserved = map[string]bool{
	"fuel":                true,
	"repair_kit":          true,
	"advanced_repair_kit": true,
	"repair_nanites":      true,
}

// huntBankReservedCategories are catalog categories that are never banked.
var huntBankReservedCategories = map[string]bool{"ammo": true, "fuel": true, "consumable": true}

// huntBankIsReserved reports whether itemID is a consumable the ship keeps:
// the haul-leftovers reserved list, huntBankReserved, or an ammo/fuel catalog
// or consumable catalog item. An item the catalog does not know is bankable.
func huntBankIsReserved(ctx context.Context, kb knowledge.Base, itemID string) bool {
	if haulReservedCargo[itemID] || huntBankReserved[itemID] {
		return true
	}
	if kb == nil {
		return false
	}
	it, err := kb.GetItem(ctx, itemID)
	return err == nil && it != nil && huntBankReservedCategories[it.Category]
}

// huntAtStation reports whether st is docked at station, which may be a POI id
// or a base id (the two are not textually related; see atPinnedStation).
func huntAtStation(ctx context.Context, kb knowledge.Base, st *game.State, station string) bool {
	if st == nil || !st.Doc || station == "" {
		return false
	}
	if st.CurrentPOI == station {
		return true
	}
	return atPinnedStation(ctx, MissionDeps{HomeStation: station, KB: kb}, st)
}

// huntUsername resolves an agent id to the in-game username send_gift needs.
func huntUsername(deps HuntDeps, agentID string) (string, error) {
	if deps.usernameFor != nil {
		return deps.usernameFor(agentID)
	}
	return UsernameFor(deps.AgentsDir, agentID)
}

// huntBankSurplus banks spare creature parts when the hunter is docked at its
// HomeStation, or at Conjunction Junction: every cargo item except the
// reserved consumables and up to keep[item] units of the active need goes to
// personal storage. When the bankable parts in storage there total at least
// huntGiftBatch, each is gifted from storage -- at CJ to haulDumpRecipient, at
// the base to the GiftTo collector (empty GiftTo banks only).
//
// Anywhere else it does nothing: a hunter never flies to base just to bank.
// Every refused deposit or gift is reported and the rest still go; the joined
// error is for the caller to log, never to stop the pass on.
func huntBankSurplus(ctx context.Context, deps HuntDeps, out io.Writer, keep map[string]int) error {
	if out == nil {
		out = io.Discard
	}
	st := deps.Client.GetState()
	atCJ := huntAtStation(ctx, deps.KB, st, haulDumpPOI)
	if !atCJ && (deps.HomeStation == "" || !huntAtStation(ctx, deps.KB, st, deps.HomeStation)) {
		return nil
	}
	where := deps.HomeStation
	if atCJ {
		where = haulDumpSystem
	}

	// Cached cargo can lag the server (an accept_mission grant carries no
	// cargo field), so read the hold before deciding what to move.
	if err := deps.Client.GetCargo(ctx); err != nil {
		fmt.Fprintf(out, "hunt: bank: get_cargo: %v; using cached hold\n", err) //nolint:errcheck
	}
	var failed []error
	if st = deps.Client.GetState(); st != nil {
		for _, c := range st.Ship.Cargo {
			qty := int(c.Quantity) - keep[c.ItemID]
			if qty <= 0 || huntBankIsReserved(ctx, deps.KB, c.ItemID) {
				continue
			}
			if err := deps.Client.DepositItemsPayload(ctx, map[string]any{
				"item_id": c.ItemID, "quantity": qty, "source": "cargo", "target": "self",
			}); err != nil {
				failed = append(failed, fmt.Errorf("deposit %s x%d: %w", c.ItemID, qty, err))
				continue
			}
			fmt.Fprintf(out, "hunt: banked %d %s at %s\n", qty, c.ItemID, where) //nolint:errcheck
		}
	}

	if err := deps.Client.ViewStorage(ctx); err != nil {
		return errors.Join(append(failed, fmt.Errorf("view_storage: %w", err))...)
	}
	held, ok := consignHeld(deps.Client.GetRawJSON("storage"))
	if !ok {
		return errors.Join(append(failed, errors.New("view_storage: no storage frame"))...)
	}
	parts, total := huntGiftableParts(ctx, deps.KB, held, keep)
	if total < huntGiftBatch {
		if total > 0 {
			fmt.Fprintf(out, "hunt: %d part(s) banked at %s; gifting at %d\n", total, where, huntGiftBatch) //nolint:errcheck
		}
		return errors.Join(failed...)
	}

	recipientID := deps.GiftTo
	if atCJ {
		recipientID = haulDumpRecipient
	}
	if recipientID == "" {
		fmt.Fprintf(out, "hunt: %d part(s) banked at %s; no collector configured, holding\n", total, where) //nolint:errcheck
		return errors.Join(failed...)
	}
	recipient, err := huntUsername(deps, recipientID)
	if err != nil {
		return errors.Join(append(failed, fmt.Errorf("gift recipient %s: %w", recipientID, err))...)
	}
	for _, id := range parts {
		if err := deps.Client.SendGift(ctx, map[string]any{
			"recipient": recipient, "item_id": id, "quantity": held[id], "source": "storage",
		}); err != nil {
			failed = append(failed, fmt.Errorf("gift %s x%d to %s: %w", id, held[id], recipientID, err))
			continue
		}
		fmt.Fprintf(out, "hunt: gave %d %s to %s at %s\n", held[id], id, recipientID, where) //nolint:errcheck
	}
	return errors.Join(failed...)
}

// huntCreatureDropKB is the KB query that says which species drop an item
// (wildlife_kill_drops); it lives on *knowledge.SQLiteKB.
type huntCreatureDropKB interface {
	SpeciesDroppingItem(ctx context.Context, itemID string) ([]string, error)
}

// huntIsCreatureDrop reports whether the KB records itemID as a creature drop.
// A KB without the query, or a failed read, says no: an item with no recorded
// creature source is never gifted.
func huntIsCreatureDrop(ctx context.Context, kb knowledge.Base, itemID string) bool {
	dk, ok := kb.(huntCreatureDropKB)
	if !ok {
		return false
	}
	sp, err := dk.SpeciesDroppingItem(ctx, itemID)
	return err == nil && len(sp) > 0
}

// huntGiftableParts lists, sorted, the stored items that may be gifted --
// recorded creature drops that are neither reserved nor kept for a held
// mission -- and their total units. Anything else banked (old ore, modules)
// stays in storage.
func huntGiftableParts(ctx context.Context, kb knowledge.Base, held map[string]int, keep map[string]int) ([]string, int) {
	var parts []string
	total := 0
	for id, q := range held {
		if q <= 0 || keep[id] > 0 || huntBankIsReserved(ctx, kb, id) || !huntIsCreatureDrop(ctx, kb, id) {
			continue
		}
		parts = append(parts, id)
		total += q
	}
	sort.Strings(parts)
	return parts, total
}

// huntHeldMissionKeep returns item -> units for every unfinished item
// objective of the held missions in an active_missions frame, so banking
// never strips the parts a held mission needs -- including a Station Need the
// pass declined to resume. ok is false when the frame cannot be read; with no
// frame at all there is nothing held to protect.
func huntHeldMissionKeep(raw []byte) (keep map[string]int, ok bool) {
	if len(raw) == 0 {
		return nil, true
	}
	var resp serverapi.GetActiveMissionsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, false
	}
	for _, m := range resp.Missions {
		for _, o := range m.Objectives {
			if o.ItemID == "" || o.Completed {
				continue
			}
			if keep == nil {
				keep = map[string]int{}
			}
			keep[o.ItemID] += max(o.Required, 1)
		}
	}
	return keep, true
}

// huntBankAtDock is the pass's banking step: a failure is logged and the pass
// goes on.
//
// keep starts from every held mission's item objectives in the last
// active_missions read (no new query), plus any extra the caller names.
func huntBankAtDock(ctx context.Context, deps HuntDeps, out io.Writer, who string, extra map[string]int) {
	keep, ok := huntHeldMissionKeep(deps.Client.GetRawJSON("active_missions"))
	if !ok {
		fmt.Fprintf(out, "hunt[%s]: banking skipped: held missions unreadable\n", who) //nolint:errcheck
		return
	}
	for id, q := range extra {
		if keep == nil {
			keep = map[string]int{}
		}
		keep[id] = max(keep[id], q)
	}
	if err := huntBankSurplus(ctx, deps, out, keep); err != nil {
		fmt.Fprintf(out, "hunt[%s]: banking: %v; hunting on\n", who, err) //nolint:errcheck
	}
}
