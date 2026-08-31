package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/rsned/spacemolt/internal/protocol"
	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/game/serverapi"
)

// lootTask is one loot_wreck call: a single cargo stack on a single wreck.
type lootTask struct {
	WreckID string
	ItemID  string
	Qty     float64
}

// lootErrKind is the loop's reaction to a failed loot.
type lootErrKind int

const (
	lootSkip  lootErrKind = iota // this stack is gone/invalid; move on
	lootRetry                    // an action is still pending; wait and retry
	lootStop                     // the hold is full; nothing further can land
)

// buildLootPlan turns a get_wrecks reply into the ordered loot_wreck calls:
// richest wreck first (jettison cannisters carry no salvage value and so sort
// last), every cargo stack at its full quantity. Wrecks with no cargo
// contribute nothing; a wreck towed by ANOTHER player is skipped and counted
// (its loot is in the tower's custody). Modules are never in the plan — they
// need salvage, not loot_wreck. onlyWreck narrows the plan to that wreck
// alone and overrides the towed skip (an explicit id means the operator
// knows better).
func buildLootPlan(resp serverapi.GetWrecksResponse, onlyWreck, selfID string) (plan []lootTask, skippedTowed int) {
	wrecks := slices.Clone(resp.Wrecks)
	slices.SortStableFunc(wrecks, func(a, b serverapi.Wreck) int {
		return b.SalvageValue - a.SalvageValue
	})
	for _, w := range wrecks {
		if onlyWreck != "" && w.ID != onlyWreck {
			continue
		}
		if len(w.Cargo) == 0 {
			continue
		}
		if onlyWreck == "" && w.TowedByPlayerID != "" && w.TowedByPlayerID != selfID {
			skippedTowed++
			continue
		}
		for _, c := range w.Cargo {
			if c.ItemID == "" || c.Quantity <= 0 {
				continue
			}
			plan = append(plan, lootTask{WreckID: w.ID, ItemID: c.ItemID, Qty: c.Quantity})
		}
	}
	return plan, skippedTowed
}

// classifyLootErr maps a loot_wreck failure onto the loop's reaction. The
// server's phrasings drift between versions, so this matches substrings of
// the two families that change behavior — a full hold ("cargo … full",
// "cargo space") and a still-queued action ("pending", "queued") — and skips
// everything else.
func classifyLootErr(err error) lootErrKind {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "cargo") && (strings.Contains(msg, "full") || strings.Contains(msg, "space")) {
		return lootStop
	}
	if strings.Contains(msg, "pending") || strings.Contains(msg, "queued") {
		return lootRetry
	}
	return lootSkip
}

// lootOutcome reads a loot's true result from the cached loot_wreck frame —
// the pending-ok is only an acknowledgement; the actual transfer lands a tick
// later as an action_result envelope {"command":"loot_wreck","result":{...}}
// (or flat on some paths). matched is false when the frame is missing,
// unparseable, or for a different item (stale cache) — the caller then
// reports the submit-level ok alone.
func lootOutcome(raw []byte, itemID string) (qty float64, wreckEmpty bool, xp map[string]float64, matched bool) {
	if len(raw) == 0 {
		return 0, false, nil, false
	}
	var resp struct {
		serverapi.LootWreckResponse
		XPGained map[string]float64 `json:"xp_gained"`
	}
	if err := json.Unmarshal(unwrapActionResult(raw), &resp); err != nil {
		return 0, false, nil, false
	}
	if !strings.EqualFold(resp.ItemID, itemID) {
		return 0, false, nil, false
	}
	return float64(resp.Quantity), resp.WreckEmpty, resp.XPGained, true
}

// lootRetryLimit bounds how often one stack is retried while a previous
// action drains; each retry waits SleepShort, so the worst case per stack is
// about one tick.
const lootRetryLimit = 3

// runLootAll is the `loot_all [wreck-id]` REPL command: refresh get_wrecks,
// record any wildlife carcasses (before looting changes them), then walk
// every cargo stack richest-wreck-first until done or the hold is full. Loot
// executes on the next server tick, so successive stacks are paced a tick
// apart; a still-pending action is retried after a short wait.
func runLootAll(ctx context.Context, client game.GameClient, onlyWreck string) error {
	var sink protocol.Response
	cctx := game.WithResultSink(ctx, &sink)
	if err := client.GetWrecks(cctx); err != nil {
		return fmt.Errorf("get_wrecks: %w", err)
	}
	raw := chooseResponseJSON(sink, client, "get_wrecks")
	if len(raw) == 0 {
		return fmt.Errorf("get_wrecks returned no data")
	}
	var resp serverapi.GetWrecksResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("parse get_wrecks: %w", err)
	}

	captureCarcasses(ctx, client, globalKB, globalAgentID, capturedCarcasses, os.Stdout)

	selfID := ""
	if st := client.GetState(); st != nil {
		selfID = st.Player.ID
	}
	plan, skippedTowed := buildLootPlan(resp, onlyWreck, selfID)
	if len(plan) == 0 {
		if skippedTowed > 0 {
			fmt.Printf("Nothing lootable: %d wreck(s) towed by another player.\n", skippedTowed)
		} else {
			fmt.Println("Nothing lootable here.")
		}
		return nil
	}
	fmt.Printf("Looting %d stack(s) across the field (richest wreck first; one loot per tick)...\n", len(plan))
	if skippedTowed > 0 {
		fmt.Printf("  (skipping %d wreck(s) towed by another player)\n", skippedTowed)
	}

	looted, failed := 0, 0
	emptied := map[string]bool{}
	for i, task := range plan {
		if emptied[task.WreckID] {
			continue
		}
		var err error
		for attempt := 0; ; attempt++ {
			var lootSink protocol.Response
			lctx := game.WithResultSink(ctx, &lootSink)
			err = client.LootWreck(lctx, task.WreckID, task.ItemID, task.Qty)
			if err == nil || classifyLootErr(err) != lootRetry || attempt >= lootRetryLimit {
				break
			}
			time.Sleep(game.SleepShort)
		}
		short := task.WreckID
		if len(short) > 8 {
			short = short[:8]
		}
		if err != nil {
			if classifyLootErr(err) == lootStop {
				fmt.Printf("  ✗ %s x%s from %s: %v\n", task.ItemID, formatFloat(task.Qty), short, err)
				fmt.Printf("Cargo hold full — stopping with %d stack(s) unlooted. (sell/deposit, then loot_all again)\n", len(plan)-i-1)
				break
			}
			failed++
			fmt.Printf("  ✗ %s x%s from %s: %v\n", task.ItemID, formatFloat(task.Qty), short, err)
			continue
		}
		// The pending-ok above is only an acknowledgement; the transfer
		// happens on the next tick and its action_result carries the real
		// quantity. Wait the tick out, then read it.
		time.Sleep(game.SleepTick)
		qty, wreckEmpty, xp, matched := lootOutcome(client.GetRawJSON("loot_wreck"), task.ItemID)
		switch {
		case !matched:
			looted++
			fmt.Printf("  ✓ %s x%s from %s (submitted; result frame not seen)\n", task.ItemID, formatFloat(task.Qty), short)
		case qty <= 0:
			fmt.Printf("  ✗ %s from %s: 0 looted — hold full or stack gone; stopping with %d stack(s) unlooted.\n", task.ItemID, short, len(plan)-i-1)
			fmt.Printf("Looted %d stack(s), %d failed.\n", looted, failed)
			return nil
		default:
			looted++
			line := fmt.Sprintf("  ✓ %s x%s from %s", task.ItemID, formatFloat(qty), short)
			for skill, gain := range xp {
				line += fmt.Sprintf(" (+%s %s xp)", formatFloat(gain), skill)
			}
			fmt.Println(line)
			if qty < task.Qty {
				fmt.Printf("Partial loot (%s of %s) — hold is nearly full; stopping with %d stack(s) unlooted.\n", formatFloat(qty), formatFloat(task.Qty), len(plan)-i-1)
				fmt.Printf("Looted %d stack(s), %d failed.\n", looted, failed)
				return nil
			}
			if wreckEmpty {
				emptied[task.WreckID] = true
			}
		}
	}
	fmt.Printf("Looted %d stack(s), %d failed.\n", looted, failed)
	return nil
}
