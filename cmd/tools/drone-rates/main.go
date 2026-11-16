// Command drone-rates estimates how fast each station's drones fill our
// marketbots' storage, from the storage history that capture_storage appends
// (assets.db storage_history) plus the action log's sales and station crafts:
//
//	mined/day = (last holding - first + sold - bought + craft inputs - craft outputs) / days
//
// It prints the per-station rates, the per-item totals, and the hauling it
// implies: holds per day at --hold units each, and haulers at --trips holds
// per hauler per day. Needs at least two captures per agent; marketbots
// capture storage twice daily.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/rsned/spacemolt/pkg/assets"
)

func main() {
	dbPath := flag.String("db", "data/assets.db", "Assets database path")
	recipesPath := flag.String("recipes", "data/game-api/latest/catalog_recipes.json", "Recipe catalog (for station crafts)")
	prefix := flag.String("agents", "marketbot", "Agent id prefix to measure")
	since := flag.Duration("since", 72*time.Hour, "How far back to measure")
	raw := flag.Bool("raw", true, "Only raw resources (*_ore, *_gas, *_ice)")
	hold := flag.Float64("hold", 1900, "Hauler hold size (units; raw resources are size 1)")
	trips := flag.Float64("trips", 3, "Holds one hauler delivers per day")
	top := flag.Int("top", 40, "Station/item rows to print (0 = all)")
	flag.Parse()

	if err := run(*dbPath, *recipesPath, *prefix, *since, *raw, *hold, *trips, *top); err != nil {
		fmt.Fprintln(os.Stderr, "drone-rates:", err)
		os.Exit(1)
	}
}

func loadRecipes(path string) (map[string]assets.RecipeIO, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cat struct {
		Items []struct {
			ID     string `json:"id"`
			Inputs []struct {
				ItemID   string  `json:"item_id"`
				Quantity float64 `json:"quantity"`
			} `json:"inputs"`
			Outputs []struct {
				ItemID   string  `json:"item_id"`
				Quantity float64 `json:"quantity"`
			} `json:"outputs"`
		} `json:"items"`
	}
	if err := json.Unmarshal(b, &cat); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	out := map[string]assets.RecipeIO{}
	for _, r := range cat.Items {
		io := assets.RecipeIO{Inputs: map[string]float64{}, Outputs: map[string]float64{}}
		for _, x := range r.Inputs {
			io.Inputs[x.ItemID] += x.Quantity
		}
		for _, x := range r.Outputs {
			io.Outputs[x.ItemID] += x.Quantity
		}
		out[r.ID] = io
	}

	return out, nil
}

func isRaw(item string) bool {
	return strings.HasSuffix(item, "_ore") || strings.HasSuffix(item, "_gas") || strings.HasSuffix(item, "_ice")
}

func run(dbPath, recipesPath, prefix string, since time.Duration, raw bool, hold, trips float64, top int) error {
	ctx := context.Background()
	recipes, err := loadRecipes(recipesPath)
	if err != nil {
		return err
	}
	cfg := assets.DefaultConfig()
	cfg.DBPath = dbPath
	st, err := assets.Open(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	agents, err := st.PlayersByAgentPrefix(ctx, prefix)
	if err != nil {
		return err
	}
	pids := make([]string, 0, len(agents))
	for pid := range agents {
		pids = append(pids, pid)
	}
	from := time.Now().Add(-since)
	snaps, err := st.LoadStorageHistory(ctx, from, pids)
	if err != nil {
		return err
	}
	flows, covered, err := st.LoadStorageFlows(ctx, from, pids, recipes)
	if err != nil {
		return err
	}
	rates := assets.StorageRates(snaps, flows, covered)

	measured := map[string]bool{}
	var minSpan, maxSpan time.Duration
	var kept []assets.StorageRate
	for _, r := range rates {
		span := r.To.Sub(r.From)
		if !measured[r.PlayerID] {
			measured[r.PlayerID] = true
			if minSpan == 0 || span < minSpan {
				minSpan = span
			}
			maxSpan = max(maxSpan, span)
		}
		if raw && !isRaw(r.ItemID) {
			continue
		}
		kept = append(kept, r)
	}
	fmt.Printf("%d %s* agents, %d measured (window %s..%s); %d without two captures inside action-log coverage yet\n\n",
		len(agents), prefix, len(measured), minSpan.Round(time.Minute), maxSpan.Round(time.Minute), len(agents)-len(measured))
	if len(kept) == 0 {
		fmt.Println("no rates yet: wait for a second storage capture (marketbots capture twice daily)")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
	_, _ = fmt.Fprintln(w, "station\titem\tmined/day\tchange\tsold/used\tagent\t")
	for i, r := range kept {
		if top > 0 && i >= top {
			break
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%.0f\t%.0f\t%.0f\t%s\t\n", r.BaseID, r.ItemID, r.PerDay, r.Delta, r.Outflow, agents[r.PlayerID])
	}
	_ = w.Flush()

	perItem := map[string]float64{}
	stations := map[string]bool{}
	total := 0.0
	for _, r := range kept {
		if r.PerDay <= 0 {
			continue
		}
		perItem[r.ItemID] += r.PerDay
		stations[r.BaseID] = true
		total += r.PerDay
	}
	items := make([]string, 0, len(perItem))
	for it := range perItem {
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return perItem[items[i]] > perItem[items[j]] })
	fmt.Println()
	w = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
	_, _ = fmt.Fprintln(w, "item\tmined/day (all stations)\t")
	for _, it := range items {
		_, _ = fmt.Fprintf(w, "%s\t%.0f\t\n", it, perItem[it])
	}
	_ = w.Flush()

	holds := total / hold
	fmt.Printf("\ntotal %.0f units/day across %d stations = %.1f holds/day at %.0f units = %d haulers at %.1f holds each per day\n",
		total, len(stations), holds, hold, int(math.Ceil(holds/trips)), trips)

	return nil
}
