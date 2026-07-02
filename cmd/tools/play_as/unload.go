package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/rsned/spacemolt/pkg/game"
	"github.com/rsned/spacemolt/pkg/market"
)

const (
	// unloadMaxRungs bounds the per-item FindBestPrices query. No item's latest
	// buy book across every station approaches this; it only guards against an
	// unbounded scan.
	unloadMaxRungs = 10000
	// unloadDefaultTopN is how many destinations to show per item by default.
	unloadDefaultTopN = 10
)

// unloadOptions mirrors the flags accepted on the `unload` REPL command.
type unloadOptions struct {
	topN        int   // >0: cap destinations per item; <=0: show all
	noStorage   bool  // skip the view_storage read (cargo only)
	minProceeds int64 // hide destinations below this proceeds
}

// heldItem is one item the operator holds, split by where it sits.
type heldItem struct {
	ItemID  string
	Name    string
	Cargo   float64
	Storage float64
}

func (h heldItem) total() float64 { return h.Cargo + h.Storage }

// unloadRung is one price level of a station's latest buy book for an item. It
// decouples the pure builder from pkg/market so unload_test.go needs no DB.
type unloadRung struct {
	StationID   string
	StationName string
	SystemID    string
	SystemName  string
	Price       float64
	Qty         float64
	CapturedAt  time.Time
}

// unloadDest is one station's sell picture for a held item: what filling that
// station's latest buy book with the full held quantity would yield.
type unloadDest struct {
	StationID   string    `json:"station_id"`
	StationName string    `json:"station_name"`
	SystemID    string    `json:"system_id"`
	SystemName  string    `json:"system_name"`
	BestPrice   float64   `json:"best_price"`
	FillQty     float64   `json:"fill_qty"`
	Proceeds    float64   `json:"proceeds"`
	AvgPrice    float64   `json:"avg_price"`
	CapturedAt  time.Time `json:"captured_at"`
	AgeTicks    int64     `json:"age_ticks"`
}

// unloadItem is one held item's full sell-anywhere picture.
type unloadItem struct {
	ItemID  string       `json:"item_id"`
	Name    string       `json:"name"`
	Cargo   float64      `json:"cargo"`
	Storage float64      `json:"storage"`
	Held    float64      `json:"held"`
	Dests   []unloadDest `json:"dests"`
}

// bestProceeds is the top destination's proceeds, or 0 when no buyers.
func (u unloadItem) bestProceeds() float64 {
	if len(u.Dests) == 0 {
		return 0
	}
	return u.Dests[0].Proceeds
}

// unloadPlan is the rendered/serialized result of `unload`.
type unloadPlan struct {
	Generated time.Time    `json:"generated"`
	Items     []unloadItem `json:"items"`
}

// runUnload is the REPL entry point for `unload`. It gathers the operator's
// held items (cargo, plus storage unless suppressed), queries market.db for
// each item's latest buy book across every station, and prints a per-item board
// ranking where each item sells best right now.
func runUnload(client game.GameClient, ctx context.Context, opts unloadOptions, format outputFormat) error {
	if globalMarketCollector == nil {
		return fmt.Errorf("unload: market DB not configured (start play_as with --market-db)")
	}

	// 1. get_cargo — the mobile pool (works undocked).
	if err := client.GetCargo(ctx); err != nil {
		return fmt.Errorf("unload: get_cargo: %w", err)
	}
	var cargoResp struct {
		Cargo []storageItem `json:"cargo"`
	}
	if raw := client.GetRawJSON("cargo"); len(raw) > 0 {
		if err := json.Unmarshal(raw, &cargoResp); err != nil {
			return fmt.Errorf("unload: parse cargo: %w", err)
		}
	}

	// 2. view_storage — best-effort; only readable while docked and only covers
	// the current station's storage. A failure (e.g. undocked) is not fatal:
	// unload still works off cargo alone.
	var storageResp struct {
		Items []storageItem `json:"items"`
	}
	if !opts.noStorage {
		if err := client.ViewStorage(ctx); err == nil {
			if raw := client.GetRawJSON("storage"); len(raw) > 0 {
				_ = json.Unmarshal(raw, &storageResp)
			}
		}
	}

	held := mergeHeld(cargoResp.Cargo, storageResp.Items)
	if len(held) == 0 {
		fmt.Print("(nothing held to sell)\n")
		return nil
	}

	// 3. For each held item, pull its latest buy book across all stations.
	rungsByItem := make(map[string][]unloadRung, len(held))
	for _, h := range held {
		best, err := globalMarketCollector.FindBestPrices(ctx, h.ItemID, "buy", unloadMaxRungs)
		if err != nil {
			return fmt.Errorf("unload: market lookup for %s: %w", h.ItemID, err)
		}
		rungsByItem[h.ItemID] = toUnloadRungs(best)
	}

	plan := buildUnloadPlan(held, rungsByItem, time.Now(), opts.topN, opts.minProceeds)

	switch format {
	case formatStyled:
		fmt.Print(renderUnloadStyled(plan))
	default:
		fmt.Print(renderUnloadJSON(plan))
	}
	return nil
}

// mergeHeld unions cargo and storage by item_id, keeping the split so the board
// can show where each item sits. Zero-quantity entries are dropped.
func mergeHeld(cargo, storage []storageItem) []heldItem {
	type acc struct {
		cargo, storage float64
		name           string
	}
	inv := map[string]*acc{}
	get := func(id string) *acc {
		a, ok := inv[id]
		if !ok {
			a = &acc{}
			inv[id] = a
		}
		return a
	}
	for _, c := range cargo {
		a := get(c.ItemID)
		a.cargo += c.Quantity
		if a.name == "" {
			a.name = c.Name
		}
	}
	for _, s := range storage {
		a := get(s.ItemID)
		a.storage += s.Quantity
		if a.name == "" {
			a.name = s.Name
		}
	}
	out := make([]heldItem, 0, len(inv))
	for id, a := range inv {
		if a.cargo+a.storage <= 0 {
			continue
		}
		name := a.name
		if name == "" {
			name = id
		}
		out = append(out, heldItem{ItemID: id, Name: name, Cargo: a.cargo, Storage: a.storage})
	}
	return out
}

// toUnloadRungs projects market.BestPrice rows into the builder's rung type.
func toUnloadRungs(best []market.BestPrice) []unloadRung {
	out := make([]unloadRung, 0, len(best))
	for _, b := range best {
		out = append(out, unloadRung{
			StationID:   b.StationID,
			StationName: b.StationName,
			SystemID:    b.SystemID,
			SystemName:  b.SystemName,
			Price:       b.Price,
			Qty:         b.Quantity,
			CapturedAt:  b.CapturedAt,
		})
	}
	return out
}

// buildUnloadPlan is the pure core: for each held item it groups the item's buy
// rungs by station, fills the full held quantity against each station's book
// (best price first), and ranks stations by proceeds. Items are ordered by
// their best destination's proceeds so the most lucrative cargo leads.
//
// topN>0 caps destinations per item; minProceeds hides destinations below the
// threshold. now drives the per-destination age (now-captured over one tick).
func buildUnloadPlan(held []heldItem, rungsByItem map[string][]unloadRung, now time.Time, topN int, minProceeds int64) unloadPlan {
	plan := unloadPlan{Generated: now, Items: []unloadItem{}}
	for _, h := range held {
		if h.total() <= 0 {
			continue
		}
		item := unloadItem{ItemID: h.ItemID, Name: h.Name, Cargo: h.Cargo, Storage: h.Storage, Held: h.total(), Dests: []unloadDest{}}

		byStation := map[string][]unloadRung{}
		order := []string{}
		for _, r := range rungsByItem[h.ItemID] {
			if _, ok := byStation[r.StationID]; !ok {
				order = append(order, r.StationID)
			}
			byStation[r.StationID] = append(byStation[r.StationID], r)
		}

		for _, st := range order {
			rungs := byStation[st]
			ors := make([]orderRung, 0, len(rungs))
			var best float64
			var captured time.Time
			meta := rungs[0]
			for _, r := range rungs {
				ors = append(ors, orderRung{Price: r.Price, Qty: r.Qty})
				if r.Price > best {
					best = r.Price
				}
				if r.CapturedAt.After(captured) {
					captured = r.CapturedAt
				}
			}
			qty, proceeds, avg, _ := fillOrderBook(h.total(), ors)
			if qty <= 0 {
				continue
			}
			if minProceeds > 0 && int64(proceeds) < minProceeds {
				continue
			}
			age := int64(0)
			if !captured.IsZero() {
				if d := now.Sub(captured); d > 0 {
					age = int64(d / game.SleepTick)
				}
			}
			item.Dests = append(item.Dests, unloadDest{
				StationID:   st,
				StationName: meta.StationName,
				SystemID:    meta.SystemID,
				SystemName:  meta.SystemName,
				BestPrice:   best,
				FillQty:     qty,
				Proceeds:    proceeds,
				AvgPrice:    avg,
				CapturedAt:  captured,
				AgeTicks:    age,
			})
		}

		slices.SortStableFunc(item.Dests, func(a, b unloadDest) int {
			switch {
			case a.Proceeds > b.Proceeds:
				return -1
			case a.Proceeds < b.Proceeds:
				return 1
			default:
				return 0
			}
		})
		if topN > 0 && len(item.Dests) > topN {
			item.Dests = item.Dests[:topN]
		}
		plan.Items = append(plan.Items, item)
	}

	slices.SortStableFunc(plan.Items, func(a, b unloadItem) int {
		ap, bp := a.bestProceeds(), b.bestProceeds()
		switch {
		case ap > bp:
			return -1
		case ap < bp:
			return 1
		default:
			return strings.Compare(a.ItemID, b.ItemID)
		}
	})
	return plan
}

// renderUnloadStyled formats an unloadPlan as a per-item destination board.
func renderUnloadStyled(plan unloadPlan) string {
	if len(plan.Items) == 0 {
		return "(nothing held to sell)\n"
	}
	var b strings.Builder
	var bestTotal float64
	for _, it := range plan.Items {
		bestTotal += it.bestProceeds()
	}
	fmt.Fprintf(&b, "Unload board — %d item(s) held, best-market total %s cr (source: market.db latest captures)\n",
		len(plan.Items), formatCredits(bestTotal))

	for _, it := range plan.Items {
		held := formatFloat(it.Held)
		switch {
		case it.Cargo > 0 && it.Storage > 0:
			held += fmt.Sprintf(" (%s cargo + %s storage)", formatFloat(it.Cargo), formatFloat(it.Storage))
		case it.Storage > 0:
			held += fmt.Sprintf(" (%s storage)", formatFloat(it.Storage))
		default:
			held += fmt.Sprintf(" (%s cargo)", formatFloat(it.Cargo))
		}
		fmt.Fprintf(&b, "\n%s (%s) — %s held\n", it.ItemID, it.Name, held)
		if len(it.Dests) == 0 {
			b.WriteString("  (no buyers in market.db)\n")
			continue
		}
		fmt.Fprintf(&b, "  %-32s %-16s %8s %6s %10s %7s\n",
			"STATION", "SYSTEM", "BEST", "FILL", "PROCEEDS", "AGE")
		for _, d := range it.Dests {
			fmt.Fprintf(&b, "  %-32s %-16s %8s %6s %10s %6dt\n",
				truncateName(d.StationID, 32), truncateName(d.SystemName, 16),
				formatPrice(d.BestPrice), formatFloat(d.FillQty),
				formatCredits(d.Proceeds), d.AgeTicks)
		}
	}
	return b.String()
}

// renderUnloadJSON serializes an unloadPlan as pretty-printed JSON.
func renderUnloadJSON(plan unloadPlan) string {
	out, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return ""
	}
	return string(out) + "\n"
}
