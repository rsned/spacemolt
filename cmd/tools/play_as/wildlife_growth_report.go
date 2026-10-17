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
//
// Growth is NOT tracked here: it spans stages, so an individual that matured
// belongs to two buckets and neither one can own the transition. It is
// collected per individual instead.
type growthBucket struct {
	species string
	stage   string // the creature name, which is what carries the stage
	hulls   []int  // one entry per distinct individual AT THIS STAGE
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
// Each INDIVIDUAL counts once PER STAGE. The explore loop looks twice per POI
// on purpose, so raw rows double every creature and a naive count reports a
// population twice its real size.
//
// Deduping per creature GLOBALLY was wrong, and silently so: an animal that
// grew from hatchling to juvenile collapsed into one entry in the juvenile
// bucket and its hatchling reading disappeared from the histogram. Over time
// every surviving animal migrates upward, leaving the young bands populated
// only by animals that died or stopped being sighted. The key is therefore
// (creature_id, name) and a maturing animal counts in BOTH bands, at the hull
// it had in each -- which is what a lifecycle histogram means.
func wildlifeGrowthReport(dir string) string {
	path := filepath.Join(dir, "observations.jsonl")
	f, err := os.Open(path)
	if err != nil {
		return fmt.Sprintf("no creature observations yet at %s\n"+
			"(collection is on by default; run an explore pass past a POI with creatures)\n", path)
	}
	defer f.Close() //nolint:errcheck

	// Per (individual, stage): the widest hull seen while it bore that name.
	type indivStage struct {
		species string
		stage   string
		best    int
	}
	perStage := map[string]*indivStage{}
	// Per individual, every distinct hull, so growth can be reported -- that
	// spans stages and so cannot be read off perStage.
	hullsSeen := map[string][]int{}
	individuals := map[string]struct{}{}
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
		if o.CreatureID == "" {
			continue
		}
		individuals[o.CreatureID] = struct{}{}

		key := o.CreatureID + "\x00" + o.Name
		cur, ok := perStage[key]
		if !ok {
			cur = &indivStage{species: o.Species, stage: o.Name}
			perStage[key] = cur
		}
		if o.MaxHull > cur.best {
			cur.best = o.MaxHull
		}
		if o.MaxHull > 0 && !slices.Contains(hullsSeen[o.CreatureID], o.MaxHull) {
			hullsSeen[o.CreatureID] = append(hullsSeen[o.CreatureID], o.MaxHull)
		}
	}

	buckets := map[string]*growthBucket{}
	for _, c := range perStage {
		if c.best == 0 {
			continue
		}
		k := c.species + "\x00" + c.stage
		b, ok := buckets[k]
		if !ok {
			b = &growthBucket{species: c.species, stage: c.stage}
			buckets[k] = b
		}
		b.hulls = append(b.hulls, c.best)
	}

	// Growth spans stages, so it is collected per individual rather than per
	// bucket. An empty list is the EXPECTED answer: as of 2026-10-16 none of
	// 1,971 tracked individuals had been observed growing, so this reporting
	// nothing is a fact about the animals, not a capture failure.
	var growth []string
	for id, hulls := range hullsSeen {
		if len(hulls) < 2 {
			continue
		}
		slices.Sort(hulls)
		growth = append(growth, fmt.Sprintf("%s %d->%d", id, hulls[0], hulls[len(hulls)-1]))
	}
	sort.Strings(growth)

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

	// Column widths are measured, not guessed: "the_experiment_research" class
	// species ids and names like "Hoarfrost Leviathan Hatchling" overflow a
	// fixed 14/22 and wrap the table into unreadability.
	spW, stW := len("SPECIES"), len("STAGE (name)")
	for _, k := range keys {
		spW = max(spW, len(buckets[k].species))
		stW = max(stW, len(buckets[k].stage))
	}

	var out strings.Builder
	fmt.Fprintf(&out, "Creature hull by stage — %d individuals from %d observations\n\n",
		len(individuals), rows)
	fmt.Fprintf(&out, "  %-*s  %-*s  %5s  %-14s %s\n",
		spW, "SPECIES", stW, "STAGE (name)", "n", "HULL RANGE", "MEDIAN")
	fmt.Fprintf(&out, "  %s\n", strings.Repeat("-", spW+stW+34))
	for _, k := range keys {
		b := buckets[k]
		slices.Sort(b.hulls)
		lo, hi := b.hulls[0], b.hulls[len(b.hulls)-1]
		rng := fmt.Sprintf("%d-%d HP", lo, hi)
		if lo == hi {
			rng = fmt.Sprintf("%d HP", lo)
		}
		stage := b.stage
		if stage == "" {
			stage = "(mature)"
		}
		fmt.Fprintf(&out, "  %-*s  %-*s  n=%-3d  %-14s %d\n",
			spW, b.species, stW, stage, len(b.hulls), rng, median(b.hulls))
	}

	// Observed growth is the headline finding, so it is called out rather than
	// left to be inferred from a widening range.
	if len(growth) > 0 {
		fmt.Fprintf(&out, "\n  Individuals observed GROWING (hull rose between sightings):\n")
		for _, g := range growth {
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
