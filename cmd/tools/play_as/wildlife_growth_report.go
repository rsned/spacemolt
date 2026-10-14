package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// growthBucket is one species-and-stage population.
type growthBucket struct {
	species string
	stage   string // the creature name, which is what carries the stage
	hulls   []int  // one entry per distinct individual
	grew    []string
}

// wildlifeGrowthReport renders the per-stage hull histogram from the temporary
// observation dump.
//
// Buckets are keyed by the creature NAME, not by a stage vocabulary: nobody
// knows the stage names yet (cloudwhale was the first variable-hull species
// ever sighted, on 2026-10-14), and the name is what encodes the stage --
// "Young Cloudwhale" against a mature "Cloudwhale". A stage nobody has seen
// becomes its own bucket the first time it appears, with no code change.
//
// Each INDIVIDUAL counts once. The explore loop looks twice per POI on purpose,
// so raw rows double every creature and a naive count reports a population
// twice its real size. Per individual the widest hull seen is kept, because
// hull only grows -- and an individual observed at two different hulls is
// reported separately, since that is the growth the collection exists to find.
func wildlifeGrowthReport(dir string) string {
	path := filepath.Join(dir, "observations.jsonl")
	f, err := os.Open(path)
	if err != nil {
		return fmt.Sprintf("no creature observations yet at %s\n"+
			"(collection is on by default; run an explore pass past a POI with creatures)\n", path)
	}
	defer f.Close() //nolint:errcheck

	// Per individual: widest hull seen, its stage name, and every distinct hull
	// so growth can be reported.
	type indiv struct {
		species string
		stage   string
		best    int
		seen    []int
	}
	byCreature := map[string]*indiv{}
	rows := 0

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var o creatureObservation
		if err := json.Unmarshal([]byte(line), &o); err != nil {
			continue
		}
		rows++
		key := o.CreatureID
		if key == "" {
			continue
		}
		cur, ok := byCreature[key]
		if !ok {
			cur = &indiv{species: o.Species, stage: o.Name}
			byCreature[key] = cur
		}
		if o.MaxHull > cur.best {
			cur.best = o.MaxHull
			// A later, larger reading means it matured into this name.
			if o.Name != "" {
				cur.stage = o.Name
			}
		}
		if o.MaxHull > 0 && !slices.Contains(cur.seen, o.MaxHull) {
			cur.seen = append(cur.seen, o.MaxHull)
		}
	}

	buckets := map[string]*growthBucket{}
	for id, c := range byCreature {
		k := c.species + "\x00" + c.stage
		b, ok := buckets[k]
		if !ok {
			b = &growthBucket{species: c.species, stage: c.stage}
			buckets[k] = b
		}
		b.hulls = append(b.hulls, c.best)
		if len(c.seen) > 1 {
			slices.Sort(c.seen)
			b.grew = append(b.grew, fmt.Sprintf("%s %d->%d", id, c.seen[0], c.seen[len(c.seen)-1]))
		}
	}

	keys := make([]string, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := buckets[keys[i]], buckets[keys[j]]
		if a.species != b.species {
			return a.species < b.species
		}
		return median(a.hulls) < median(b.hulls) // youngest stage first
	})

	var out strings.Builder
	fmt.Fprintf(&out, "Creature hull by stage — %d individuals from %d observations\n\n", len(byCreature), rows)
	fmt.Fprintf(&out, "  %-14s %-22s %5s  %-15s %s\n", "SPECIES", "STAGE (name)", "n", "HULL RANGE", "MEDIAN")
	fmt.Fprintf(&out, "  %s\n", strings.Repeat("-", 72))
	for _, k := range keys {
		b := buckets[k]
		slices.Sort(b.hulls)
		lo, hi := b.hulls[0], b.hulls[len(b.hulls)-1]
		rng := fmt.Sprintf("%d-%d HP", lo, hi)
		if lo == hi {
			rng = fmt.Sprintf("%d HP", lo)
		}
		fmt.Fprintf(&out, "  %-14s %-22s n=%-3d %-15s %d\n",
			b.species, b.stage, len(b.hulls), rng, median(b.hulls))
	}

	// Observed growth is the headline finding, so it is called out rather than
	// left to be inferred from a widening range.
	var grew []string
	for _, k := range keys {
		grew = append(grew, buckets[k].grew...)
	}
	if len(grew) > 0 {
		fmt.Fprintf(&out, "\n  Individuals observed to have GREW (hull rose between sightings):\n")
		for _, g := range grew {
			fmt.Fprintf(&out, "    %s\n", g)
		}
	} else {
		fmt.Fprintf(&out, "\n  No individual has yet been seen at two different hulls.\n")
	}
	return out.String()
}

func median(v []int) int {
	if len(v) == 0 {
		return 0
	}
	s := slices.Clone(v)
	slices.Sort(s)
	return s[len(s)/2]
}
